package haproxy

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// scriptedExec is a stateful, in-memory fake Exec used only by the B3 driver
// tests. It NEVER runs real sudo, systemctl, launchctl, kill or haproxy and
// NEVER touches the real filesystem. It records every invocation (argv + stdin)
// so a test can assert the exact commands a driver issued, and it interprets a
// small command vocabulary (tee/mv/cat/rm/find/chmod/chown, systemctl,
// launchctl, kill, haproxy) against an in-memory file map so that a write can
// be read back through the interface (the conformance test relies on this).
type scriptedExec struct {
	mu    sync.Mutex
	calls []scriptedInvocation

	files  map[string][]byte
	mtimes map[string]int64
	clock  int64
	// dirs are the directories that exist; tee into a missing one fails like a
	// real write would. seedFile registers its parent.
	dirs map[string]bool

	// selinuxMode / selinuxBool are what getenforce / getsebool report
	// (defaults Enforcing / off).
	selinuxMode string
	selinuxBool string

	// Error injection for the failure-propagation paths.
	validateFail bool
	reloadFail   bool
	restartFail  bool
	startFail    bool
	removeFail   bool

	// serviceActive is what `systemctl is-active` / `launchctl print` report.
	serviceActive bool
	// pid is returned by `cat <pidfile>` when a path looks like a pidfile and
	// has no stored content (lets a SIGUSR2 reload test seed a master pid).
	pid string
}

type scriptedInvocation struct {
	name  string
	args  []string
	stdin []byte
}

func newScriptedExec() *scriptedExec {
	return &scriptedExec{
		files:         map[string][]byte{},
		mtimes:        map[string]int64{},
		dirs:          map[string]bool{},
		serviceActive: true,
	}
}

// seedFile places content at a path as if it were already installed, for read
// and pidfile tests.
func (s *scriptedExec) seedFile(path string, data []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clock++
	s.files[path] = append([]byte(nil), data...)
	s.mtimes[path] = 1_700_000_000 + s.clock
	s.dirs[filepath.Dir(path)] = true
}

func (s *scriptedExec) Run(ctx context.Context, stdin []byte, name string, args ...string) (stdout, stderr []byte, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, scriptedInvocation{name: name, args: append([]string(nil), args...), stdin: append([]byte(nil), stdin...)})

	cmd := name
	rest := args
	if name == "sudo" {
		if len(args) == 0 {
			return nil, nil, fmt.Errorf("scripted: sudo with no command")
		}
		cmd = args[0]
		rest = args[1:]
	}

	switch cmd {
	case "tee":
		path := rest[len(rest)-1]
		if !s.dirs[filepath.Dir(path)] {
			return nil, []byte("No such file or directory"), fmt.Errorf("scripted: tee %s: no such directory", path)
		}
		s.clock++
		s.files[path] = append([]byte(nil), stdin...)
		s.mtimes[path] = 1_700_000_000 + s.clock
		return stdin, nil, nil
	case "chmod", "chown":
		return nil, nil, nil
	case "test":
		if len(rest) == 2 && rest[0] == "-d" && s.dirs[rest[1]] {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("scripted: test failed")
	case "mkdir":
		s.dirs[rest[len(rest)-1]] = true
		return nil, nil, nil
	case "mv":
		var pos []string
		for _, a := range rest {
			if strings.HasPrefix(a, "-") {
				continue
			}
			pos = append(pos, a)
		}
		if len(pos) < 2 {
			return nil, nil, fmt.Errorf("scripted: mv needs src dst")
		}
		src, dst := pos[0], pos[1]
		s.files[dst] = s.files[src]
		s.mtimes[dst] = s.mtimes[src]
		delete(s.files, src)
		delete(s.mtimes, src)
		return nil, nil, nil
	case "cat":
		path := rest[len(rest)-1]
		if b, ok := s.files[path]; ok {
			return b, nil, nil
		}
		if s.pid != "" && strings.Contains(path, "pid") {
			return []byte(s.pid + "\n"), nil, nil
		}
		return nil, []byte("No such file"), fmt.Errorf("scripted: cat %s: not found", path)
	case "rm":
		if s.removeFail {
			return nil, []byte("rm failed"), fmt.Errorf("scripted: rm failed")
		}
		path := rest[len(rest)-1]
		delete(s.files, path)
		delete(s.mtimes, path)
		return nil, nil, nil
	case "find":
		dir := ""
		for _, a := range rest {
			if !strings.HasPrefix(a, "-") {
				dir = a
				break
			}
		}
		var b strings.Builder
		for p, data := range s.files {
			if filepath.Dir(p) == dir {
				fmt.Fprintf(&b, "%s\t%d\t%d\n", p, len(data), s.mtimes[p])
			}
		}
		return []byte(b.String()), nil, nil
	case "haproxy":
		if hasArg(rest, "-c") {
			if s.validateFail {
				return nil, []byte("[ALERT] config invalid"), fmt.Errorf("scripted: haproxy -c failed")
			}
			return []byte("Configuration file is valid\n"), nil, nil
		}
		if hasArg(rest, "-v") {
			return []byte("HAProxy version 3.4.6 scripted\n"), nil, nil
		}
		return nil, nil, nil
	case "systemctl":
		action := rest[0]
		switch action {
		case "reload":
			if s.reloadFail {
				return nil, []byte("reload failed"), fmt.Errorf("scripted: reload failed")
			}
		case "restart":
			if s.restartFail {
				return nil, []byte("restart failed"), fmt.Errorf("scripted: restart failed")
			}
		case "start":
			if s.startFail {
				return nil, []byte("start failed"), fmt.Errorf("scripted: start failed")
			}
		case "is-active":
			if s.serviceActive {
				return []byte("active\n"), nil, nil
			}
			return []byte("inactive\n"), nil, fmt.Errorf("scripted: inactive exit 3")
		}
		return nil, nil, nil
	case "launchctl":
		action := rest[0]
		switch action {
		case "kickstart":
			if s.restartFail {
				return nil, []byte("kickstart failed"), fmt.Errorf("scripted: kickstart failed")
			}
		case "bootstrap":
			if s.startFail {
				return nil, []byte("bootstrap failed"), fmt.Errorf("scripted: bootstrap failed")
			}
		case "print":
			if s.serviceActive {
				return []byte("state = running\n"), nil, nil
			}
			return nil, []byte("could not find service"), fmt.Errorf("scripted: not running")
		}
		return nil, nil, nil
	case "kill":
		if s.reloadFail {
			return nil, []byte("kill failed"), fmt.Errorf("scripted: kill failed")
		}
		return nil, nil, nil
	case "getenforce":
		if s.selinuxMode != "" {
			return []byte(s.selinuxMode + "\n"), nil, nil
		}
		return []byte("Enforcing\n"), nil, nil
	case "getsebool":
		if s.selinuxBool != "" {
			return []byte("haproxy_connect_any --> " + s.selinuxBool + "\n"), nil, nil
		}
		return []byte("haproxy_connect_any --> off\n"), nil, nil
	default:
		return nil, nil, nil
	}
}

func hasArg(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}

// argvStrings returns each recorded invocation as a single "name arg arg..."
// string, for exact-command assertions.
func (s *scriptedExec) argvStrings() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.calls))
	for _, c := range s.calls {
		out = append(out, strings.TrimSpace(c.name+" "+strings.Join(c.args, " ")))
	}
	return out
}

// issued reports whether any recorded invocation's "name arg arg..." contains
// the given substring.
func (s *scriptedExec) issued(sub string) bool {
	for _, a := range s.argvStrings() {
		if strings.Contains(a, sub) {
			return true
		}
	}
	return false
}

// argvContains reports whether the given substring appears in ANY argv token of
// ANY recorded invocation (never consulting stdin).
func (s *scriptedExec) argvContains(sub string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.calls {
		if strings.Contains(c.name, sub) {
			return true
		}
		for _, a := range c.args {
			if strings.Contains(a, sub) {
				return true
			}
		}
	}
	return false
}

// stdinContains reports whether any invocation carried the substring on stdin.
func (s *scriptedExec) stdinContains(sub string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.calls {
		if strings.Contains(string(c.stdin), sub) {
			return true
		}
	}
	return false
}

func (s *scriptedExec) reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = nil
}

// atoiOK is a tiny helper so tests can assert a parsed pid round-trips.
func atoiOK(s string) bool {
	_, err := strconv.Atoi(strings.TrimSpace(s))
	return err == nil
}

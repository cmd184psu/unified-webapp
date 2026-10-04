package haproxy

// The staged test (FRD §7 staging). The candidate config is written to the data
// directory with every listening port shifted, so a throwaway HAProxy can run
// beside the live one. It is started, probed over TLS, and stopped again; the
// live files are never involved. Every port p maps to p+10000 (443 -> 10443), so
// a hand-written frontend on 8443 or the 1936 stats page still gets tested
// without colliding with the live proxy.

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// StagePort is where the staged copy of the :443 frontend listens.
const StagePort = 10443

// ProbeResult is what one host answered through the staged proxy.
type ProbeResult struct {
	Host    string `json:"host"`
	Trusted bool   `json:"trusted"` // TLS handshake succeeded against a trusted chain for this name
	Status  int    `json:"status"`  // HTTP status through the staged proxy; 0 when none
	Detail  string `json:"detail"`
}

// StageTester starts the staged config, probes the hosts and stops it again.
// An error means the staged proxy could not be started or tested at all.
type StageTester func(ctx context.Context, cfgPath string, hosts []string) ([]ProbeResult, error)

var (
	stageBindRe   = regexp.MustCompile(`(?m)^(\s*bind\s+)(\S+)`)
	stageSocketRe = regexp.MustCompile(`(?m)^[ \t]*stats socket\b.*\n?`)
)

func stagePort(p int) int {
	if p+10000 <= 65535 {
		return p + 10000
	}
	return p
}

// StageRewrite returns cfg with each bind port shifted (see StagePort) and the
// stats socket line removed: the staged proxy is only probed over TLS, and a
// second process must not fight the live one over the socket.
func StageRewrite(cfg string) string {
	cfg = stageBindRe.ReplaceAllStringFunc(cfg, func(line string) string {
		m := stageBindRe.FindStringSubmatch(line)
		addrs := strings.Split(m[2], ",")
		for i, a := range addrs {
			if strings.HasPrefix(a, "unix@") || strings.HasPrefix(a, "/") {
				continue
			}
			if c := strings.LastIndex(a, ":"); c >= 0 {
				if p, err := strconv.Atoi(a[c+1:]); err == nil {
					addrs[i] = a[:c+1] + strconv.Itoa(stagePort(p))
				}
			}
		}
		return m[1] + strings.Join(addrs, ",")
	})
	return stageSocketRe.ReplaceAllString(cfg, "")
}

// probeHost turns a certificate name into a name that can be asked for over SNI.
func probeHost(h string) string {
	if strings.HasPrefix(h, "*.") {
		return "www." + h[2:]
	}
	return h
}

// NewStageTester returns the real tester: it runs haproxy through sudo, like
// the rest of the module. caFile adds a private root to the system trust store.
func NewStageTester(caFile string) StageTester {
	return func(ctx context.Context, cfgPath string, hosts []string) ([]ProbeResult, error) {
		if _, err := StageTLSConfig(caFile, ""); err != nil {
			return nil, err
		}
		stop, err := startStaged(ctx, cfgPath)
		if err != nil {
			return nil, err
		}
		defer stop()
		return probeStaged(ctx, caFile, hosts), nil
	}
}

func portFree(port int) bool {
	ln, err := net.Listen("tcp", ":"+strconv.Itoa(port))
	if err != nil {
		return false
	}
	ln.Close()
	return true
}

func startStaged(ctx context.Context, cfgPath string) (func(), error) {
	if !portFree(StagePort) {
		return nil, fmt.Errorf("port %d is already in use, so the staged test cannot run (is an earlier test still going?)", StagePort)
	}
	pidFile := filepath.Join(filepath.Dir(cfgPath), "haproxy.pid")
	_ = os.Remove(pidFile)
	if out, err := exec.CommandContext(ctx, "sudo", "-n", "haproxy", "-f", cfgPath, "-p", pidFile, "-D").CombinedOutput(); err != nil {
		return nil, fmt.Errorf("the staged HAProxy would not start: %s", firstAlert(string(out), err))
	}
	stop := func() {
		c, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if pids, err := exec.CommandContext(c, "sudo", "-n", "cat", pidFile).Output(); err == nil {
			args := append([]string{"-n", "kill", "-TERM"}, strings.Fields(string(pids))...)
			if len(args) > 3 {
				_ = exec.CommandContext(c, "sudo", args...).Run()
			}
		}
		for i := 0; i < 50 && !portFree(StagePort); i++ {
			time.Sleep(100 * time.Millisecond)
		}
	}
	for i := 0; i < 50; i++ {
		if conn, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(StagePort), 200*time.Millisecond); err == nil {
			conn.Close()
			return stop, nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	stop()
	return nil, fmt.Errorf("the staged HAProxy started but never listened on port %d", StagePort)
}

func probeStaged(ctx context.Context, caFile string, hosts []string) []ProbeResult {
	out := make([]ProbeResult, 0, len(hosts))
	for _, h := range hosts {
		name := probeHost(h)
		tlsCfg, _ := StageTLSConfig(caFile, name) // the CA file was validated before the proxy started
		client := &http.Client{
			Timeout: 8 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: tlsCfg,
				DialContext: func(c context.Context, network, _ string) (net.Conn, error) {
					return (&net.Dialer{}).DialContext(c, network, "127.0.0.1:"+strconv.Itoa(StagePort))
				},
				DisableKeepAlives: true,
			},
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		}
		r := ProbeResult{Host: h}
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+name+"/", nil)
		resp, err := client.Do(req)
		if err != nil {
			msg := err.Error()
			switch {
			case strings.Contains(msg, "unknown authority"):
				r.Detail = "NOT TRUSTED: the certificate is not signed by an authority this machine trusts"
			case strings.Contains(msg, "valid for") || strings.Contains(msg, "not valid for"):
				r.Detail = "the certificate served does not cover this name"
			case strings.Contains(msg, "expired"):
				r.Detail = "the certificate has expired"
			default:
				r.Detail = "no secure answer: " + msg
			}
			out = append(out, r)
			continue
		}
		resp.Body.Close()
		r.Trusted, r.Status = true, resp.StatusCode
		if resp.StatusCode >= 502 && resp.StatusCode <= 504 {
			r.Detail = "served securely, but whatever sits behind it is not answering"
		}
		out = append(out, r)
	}
	return out
}

func firstAlert(out string, err error) string {
	var alerts []string
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "[ALERT]") {
			alerts = append(alerts, strings.TrimSpace(l[strings.Index(l, "]")+1:]))
		}
	}
	if len(alerts) > 0 {
		return strings.Join(alerts, " ")
	}
	return err.Error()
}

// untrusted names the hosts that failed the TLS trust check.
func untrusted(ps []ProbeResult) []string {
	var bad []string
	for _, p := range ps {
		if !p.Trusted {
			bad = append(bad, p.Host+": "+p.Detail)
		}
	}
	return bad
}

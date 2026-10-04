package haproxy

import (
	"bufio"
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeStatsServer is a real local unix-socket server speaking the HAProxy
// stats protocol: one command line per connection, reply, close. It records
// every command so tests can prove the module only ever sends read-only ones.
type fakeStatsServer struct {
	Path string
	mu   sync.Mutex
	cmds []string
}

func (f *fakeStatsServer) Commands() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.cmds...)
}

// startFakeStats serves replies keyed by command. When hang is true it accepts
// and reads but never answers (a timeout test).
func startFakeStats(t *testing.T, replies map[string]string, hang bool) *fakeStatsServer {
	t.Helper()
	dir, err := os.MkdirTemp("", "hs")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	f := &fakeStatsServer{Path: filepath.Join(dir, "s.sock")}
	ln, err := net.Listen("unix", f.Path)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	done := make(chan struct{})
	t.Cleanup(func() { close(done); ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				line, _ := bufio.NewReader(c).ReadString('\n')
				cmd := strings.TrimSpace(line)
				f.mu.Lock()
				f.cmds = append(f.cmds, cmd)
				f.mu.Unlock()
				if hang {
					<-done
					return
				}
				c.Write([]byte(replies[cmd]))
			}(c)
		}
	}()
	return f
}

func readTestdata(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestParseStatCSVRecordedSample(t *testing.T) {
	rows, err := ParseStatCSV(readTestdata(t, "show_stat.csv"))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 6 {
		t.Fatalf("rows = %d, want 6: %+v", len(rows), rows)
	}
	by := map[string]StatRow{}
	for _, r := range rows {
		by[r.Proxy+"/"+r.Server+"/"+r.Kind] = r
	}
	fe := by["fe_https//frontend"]
	if fe.Status != "OPEN" || fe.SessionsCur != 12 || fe.SessionRate != 3 || fe.BytesIn != 1048576 || fe.BytesOut != 5242880 {
		t.Errorf("frontend row wrong: %+v", fe)
	}
	if by["be_app//backend"].Status != "UP" {
		t.Errorf("backend row wrong: %+v", by["be_app//backend"])
	}
	up := by["be_app/app1/server"]
	if up.Status != "UP" || up.CheckStatus != "L7OK" || up.LastCheck != "OK" || up.SessionsCur != 5 {
		t.Errorf("UP server wrong: %+v", up)
	}
	down := by["be_app/app2/server"]
	if down.Status != "DOWN" || down.CheckStatus != "L4CON" || down.LastCheck != "Layer4 connection problem" {
		t.Errorf("DOWN server wrong: %+v", down)
	}
	nc := by["be_app/app3/server"]
	if nc.Status != "no check" || nc.CheckStatus != "" {
		t.Errorf("no-check server wrong: %+v", nc)
	}
	if _, ok := by["fe_https/sock-1/listener"]; !ok {
		t.Errorf("listener row missing: %+v", rows)
	}
}

func TestParseStatCSVUsesColumnNamesNotOffsets(t *testing.T) {
	// Columns reordered and extra ones added: names must drive the mapping.
	csv := "# svname,extra,pxname,type,status,scur\nweb1,zz,be,2,UP,4\n\n"
	rows, err := ParseStatCSV(csv)
	if err != nil || len(rows) != 1 {
		t.Fatalf("rows=%v err=%v", rows, err)
	}
	if r := rows[0]; r.Proxy != "be" || r.Server != "web1" || r.Kind != "server" || r.SessionsCur != 4 {
		t.Errorf("row = %+v", r)
	}
}

func TestParseStatCSVNoHeaderIsError(t *testing.T) {
	if _, err := ParseStatCSV("garbage\n"); err == nil {
		t.Error("want error for output without a # header")
	}
}

func TestParseStatInfoRecordedSample(t *testing.T) {
	info := ParseStatInfo(readTestdata(t, "show_info.txt"))
	if info.Version != "3.0.5-8e879a5" || info.UptimeSec != 184361 || info.CurrConns != 7 || info.Pid != 28417 {
		t.Errorf("info = %+v", info)
	}
}

func TestStatsFetchOverUnixSocket(t *testing.T) {
	f := startFakeStats(t, map[string]string{
		"show stat": readTestdata(t, "show_stat.csv"),
		"show info": readTestdata(t, "show_info.txt"),
	}, false)
	info, rows, err := FetchStats(context.Background(), f.Path, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if info.Version == "" || len(rows) != 6 {
		t.Errorf("info=%+v rows=%d", info, len(rows))
	}
	for _, c := range f.Commands() {
		if c != "show stat" && c != "show info" {
			t.Errorf("sent a non-read-only command %q", c)
		}
	}
}

func TestStatsQueryRefusesOtherCommands(t *testing.T) {
	f := startFakeStats(t, nil, false)
	if _, err := statsQuery(context.Background(), f.Path, time.Second, "disable server be/app1"); err == nil {
		t.Error("statsQuery must refuse commands other than show stat / show info")
	}
	if len(f.Commands()) != 0 {
		t.Errorf("nothing may reach the socket: %v", f.Commands())
	}
}

func TestStatsUnavailableMissingRefusedTimeout(t *testing.T) {
	dir := t.TempDir()
	// Missing socket file.
	_, _, err := FetchStats(context.Background(), filepath.Join(dir, "none.sock"), 200*time.Millisecond)
	var ue *ErrStatsUnavailable
	if !errors.As(err, &ue) || ue.Error() == "" {
		t.Errorf("missing socket: err = %v, want ErrStatsUnavailable", err)
	}
	// Refused: a socket file with nobody listening.
	p := filepath.Join(dir, "dead.sock")
	ln, lerr := net.Listen("unix", p)
	if lerr != nil {
		t.Fatal(lerr)
	}
	ln.(*net.UnixListener).SetUnlinkOnClose(false)
	ln.Close()
	_, _, err = FetchStats(context.Background(), p, 200*time.Millisecond)
	if !errors.As(err, &ue) {
		t.Errorf("refused: err = %v, want ErrStatsUnavailable", err)
	}
	// Timeout: accepts but never answers.
	f := startFakeStats(t, nil, true)
	start := time.Now()
	_, _, err = FetchStats(context.Background(), f.Path, 200*time.Millisecond)
	if !errors.As(err, &ue) {
		t.Errorf("timeout: err = %v, want ErrStatsUnavailable", err)
	}
	if time.Since(start) > 3*time.Second {
		t.Errorf("timeout took %v", time.Since(start))
	}
}

// ---- StatsVerifier ---------------------------------------------------------

func newVerifier(drv *fakeDriver, path string) (StatsVerifier, *OpLog) {
	drv.StatsPath = path
	log := NewOpLog(50)
	return StatsVerifier{Driver: drv, Log: log, Timeout: 300 * time.Millisecond}, log
}

func logText(l *OpLog) string {
	var b strings.Builder
	for _, e := range l.Snapshot() {
		b.WriteString(e.Message + "\n")
	}
	return b.String()
}

func TestStatsVerifierSocketAnswersOK(t *testing.T) {
	f := startFakeStats(t, map[string]string{"show info": readTestdata(t, "show_info.txt")}, false)
	v, _ := newVerifier(newFakeDriver(), f.Path)
	if err := v.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestStatsVerifierFallbackLogged(t *testing.T) {
	v, log := newVerifier(newFakeDriver(), filepath.Join(t.TempDir(), "none.sock"))
	if err := v.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logText(log), "stats socket not available") {
		t.Errorf("fallback not logged: %q", logText(log))
	}
}

func TestStatsVerifierServiceInactiveErrors(t *testing.T) {
	drv := newFakeDriver()
	drv.Active = false
	v, _ := newVerifier(drv, filepath.Join(t.TempDir(), "none.sock"))
	if err := v.Verify(context.Background()); err == nil {
		t.Error("inactive service with no socket must fail")
	}
}

func TestStatsVerifierSocketAnswersButServiceInactiveErrors(t *testing.T) {
	f := startFakeStats(t, map[string]string{"show info": readTestdata(t, "show_info.txt")}, false)
	drv := newFakeDriver()
	drv.Active = false
	v, _ := newVerifier(drv, f.Path)
	if err := v.Verify(context.Background()); err == nil {
		t.Error("inactive service must fail even when the socket answers")
	}
}

func TestStatsVerifierSocketWithoutVersionErrors(t *testing.T) {
	f := startFakeStats(t, map[string]string{"show info": "Name: HAProxy\n"}, false)
	v, _ := newVerifier(newFakeDriver(), f.Path)
	if err := v.Verify(context.Background()); err == nil {
		t.Error("a socket that reports no version must fail")
	}
}

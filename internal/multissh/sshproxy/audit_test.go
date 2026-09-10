package sshproxy

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// captureLog redirects the standard logger for the duration of a test and
// returns a func that reads back everything written to it.
func captureLog(t *testing.T) func() string {
	t.Helper()
	var mu sync.Mutex
	var buf strings.Builder
	prevFlags := log.Flags()
	prevOut := log.Writer()
	log.SetFlags(0)
	log.SetOutput(writerFunc(func(p []byte) (int, error) {
		mu.Lock()
		defer mu.Unlock()
		buf.Write(p)
		return len(p), nil
	}))
	t.Cleanup(func() {
		log.SetOutput(prevOut)
		log.SetFlags(prevFlags)
	})
	return func() string {
		mu.Lock()
		defer mu.Unlock()
		return buf.String()
	}
}

type writerFunc func(p []byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

func waitForLog(t *testing.T, logged func() string, want string) {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(logged(), want) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %q in the log:\n%s", want, logged())
}

// A password session must be auditable end to end -- which host, which user,
// which auth method, and when it ended -- with the password itself absent.
// The frame is hand-built rather than sent through clientMsg because Secret
// marshals redacted, which would make the test pass for the wrong reason.
func TestAuditLinesCarryNoCredential(t *testing.T) {
	const secret = "hunter2-do-not-leak"
	logged := captureLog(t)

	dialer := &fakeDialer{}
	srv, _ := newTestServer(t, dialer)
	ws := dialWS(t, srv)

	frame := fmt.Sprintf(
		`{"type":"connect","host":"pw.example","port":2222,"user":"ops","key":"","password":%q,"cols":80,"rows":24}`,
		secret,
	)
	if err := ws.WriteMessage(websocket.TextMessage, []byte(frame)); err != nil {
		t.Fatalf("write connect: %v", err)
	}
	if _, data := readFrame(t, ws); !strings.Contains(string(data), `"state":"connected"`) {
		t.Fatalf("expected connected status, got %s", data)
	}

	// The password really did reach the dialer -- so its absence from the log
	// is redaction, not a frame that never carried a credential.
	dialer.mu.Lock()
	reached := dialer.last.Password.Reveal()
	dialer.mu.Unlock()
	if reached != secret {
		t.Fatalf("dialer did not receive the password, got %q", reached)
	}

	waitForLog(t, logged, `connect host="pw.example" port=2222 user="ops" auth=password outcome=ok`)

	if err := ws.WriteJSON(clientMsg{Type: "disconnect"}); err != nil {
		t.Fatalf("write disconnect: %v", err)
	}
	waitForLog(t, logged, `disconnect host="pw.example" user="ops" reason=client`)

	if out := logged(); strings.Contains(out, secret) {
		t.Fatalf("audit output leaked the password:\n%s", out)
	}
}

// A key session is labelled auth=key, and the key path -- which names a file on
// the server -- stays out of the audit line.
func TestAuditLabelsKeySessionsWithoutTheKeyPath(t *testing.T) {
	logged := captureLog(t)

	dialer := &fakeDialer{}
	srv, dir := newTestServer(t, dialer)
	ws := dialWS(t, srv)

	if err := ws.WriteJSON(clientMsg{Type: "connect", Host: "k.example", Port: 22, User: "ops", Key: "id", Cols: 80, Rows: 24}); err != nil {
		t.Fatalf("write connect: %v", err)
	}
	if _, data := readFrame(t, ws); !strings.Contains(string(data), `"state":"connected"`) {
		t.Fatalf("expected connected status, got %s", data)
	}

	waitForLog(t, logged, `connect host="k.example" port=22 user="ops" auth=key outcome=ok`)
	if out := logged(); strings.Contains(out, dir) {
		t.Fatalf("audit output leaked the key path %q:\n%s", dir, out)
	}
}

// The rejected-upgrade line is the only diagnosis an operator gets when a proxy
// rewrites Host, so it must name both sides of the comparison.
func TestAuditRecordsRejectedUpgrades(t *testing.T) {
	logged := captureLog(t)

	srv, _ := newTestServer(t, &fakeDialer{})
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/"
	_, _, err := websocket.DefaultDialer.Dial(url, map[string][]string{
		"Origin": {"http://evil.example"},
	})
	if err == nil {
		t.Fatal("expected a foreign Origin to be rejected")
	}

	waitForLog(t, logged, `ws upgrade rejected origin="http://evil.example"`)
}

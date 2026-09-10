package sshproxy

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// fakeConn echoes everything written to it back through Output and records the
// last resize, so the bridge protocol can be tested without a real SSH server.
type fakeConn struct {
	pr *io.PipeReader
	pw *io.PipeWriter

	mu        sync.Mutex
	cols      int
	rows      int
	done      chan struct{}
	closeOnce sync.Once
}

func newFakeConn() *fakeConn {
	pr, pw := io.Pipe()
	return &fakeConn{pr: pr, pw: pw, done: make(chan struct{})}
}

func (c *fakeConn) Write(p []byte) (int, error) { return c.pw.Write(p) }
func (c *fakeConn) Output() io.Reader           { return c.pr }

func (c *fakeConn) Resize(cols, rows int) error {
	c.mu.Lock()
	c.cols, c.rows = cols, rows
	c.mu.Unlock()
	return nil
}

func (c *fakeConn) Wait() error {
	<-c.done
	return nil
}

func (c *fakeConn) Close() error {
	c.closeOnce.Do(func() {
		close(c.done)
		_ = c.pw.Close()
		_ = c.pr.Close()
	})
	return nil
}

type fakeDialer struct {
	err     error
	mu      sync.Mutex
	last    ConnectParams
	created *fakeConn
}

func (d *fakeDialer) Dial(p ConnectParams) (Conn, error) {
	d.mu.Lock()
	d.last = p
	d.mu.Unlock()
	if d.err != nil {
		return nil, d.err
	}
	c := newFakeConn()
	d.mu.Lock()
	d.created = c
	d.mu.Unlock()
	return c, nil
}

func newTestServer(t *testing.T, dialer Dialer) (*httptest.Server, string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "id"), []byte("KEY"), 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	h := NewHandler(dialer, dir)
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv, dir
}

func dialWS(t *testing.T, srv *httptest.Server) *websocket.Conn {
	t.Helper()
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/"
	ws, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("ws dial: %v", err)
	}
	t.Cleanup(func() { _ = ws.Close() })
	return ws
}

func readFrame(t *testing.T, ws *websocket.Conn) (int, []byte) {
	t.Helper()
	_ = ws.SetReadDeadline(time.Now().Add(3 * time.Second))
	mt, data, err := ws.ReadMessage()
	if err != nil {
		t.Fatalf("read frame: %v", err)
	}
	return mt, data
}

func TestBridge_ConnectEchoResizeDisconnect(t *testing.T) {
	dialer := &fakeDialer{}
	srv, _ := newTestServer(t, dialer)
	ws := dialWS(t, srv)

	if err := ws.WriteJSON(clientMsg{Type: "connect", Host: "h", User: "u", Key: "id", Cols: 100, Rows: 40}); err != nil {
		t.Fatalf("write connect: %v", err)
	}

	mt, data := readFrame(t, ws)
	if mt != websocket.TextMessage || !strings.Contains(string(data), `"state":"connected"`) {
		t.Fatalf("expected connected status, got mt=%d data=%s", mt, data)
	}

	dialer.mu.Lock()
	if dialer.last.Host != "h" || dialer.last.User != "u" || !strings.HasSuffix(dialer.last.KeyPath, "/id") {
		t.Fatalf("dialer got unexpected params: %+v", dialer.last)
	}
	dialer.mu.Unlock()

	// Echo: binary in -> binary out.
	if err := ws.WriteMessage(websocket.BinaryMessage, []byte("hello")); err != nil {
		t.Fatalf("write input: %v", err)
	}
	mt, data = readFrame(t, ws)
	if mt != websocket.BinaryMessage || string(data) != "hello" {
		t.Fatalf("expected echoed 'hello', got mt=%d data=%s", mt, data)
	}

	// Resize is forwarded to the connection.
	if err := ws.WriteJSON(clientMsg{Type: "resize", Cols: 123, Rows: 45}); err != nil {
		t.Fatalf("write resize: %v", err)
	}
	waitFor(t, func() bool {
		dialer.mu.Lock()
		c := dialer.created
		dialer.mu.Unlock()
		if c == nil {
			return false
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		return c.cols == 123 && c.rows == 45
	})

	// Disconnect yields a disconnected status.
	if err := ws.WriteJSON(clientMsg{Type: "disconnect"}); err != nil {
		t.Fatalf("write disconnect: %v", err)
	}
	mt, data = readFrame(t, ws)
	if mt != websocket.TextMessage || !strings.Contains(string(data), `"state":"disconnected"`) {
		t.Fatalf("expected disconnected status, got mt=%d data=%s", mt, data)
	}
}

func TestBridge_InvalidKeyReportsError(t *testing.T) {
	srv, _ := newTestServer(t, &fakeDialer{})
	ws := dialWS(t, srv)

	if err := ws.WriteJSON(clientMsg{Type: "connect", Host: "h", User: "u", Key: "../escape"}); err != nil {
		t.Fatalf("write connect: %v", err)
	}
	mt, data := readFrame(t, ws)
	if mt != websocket.TextMessage || !strings.Contains(string(data), `"type":"error"`) {
		t.Fatalf("expected error frame, got mt=%d data=%s", mt, data)
	}
}

func TestBridge_DialFailureReportsError(t *testing.T) {
	srv, _ := newTestServer(t, &fakeDialer{err: errors.New("boom")})
	ws := dialWS(t, srv)

	if err := ws.WriteJSON(clientMsg{Type: "connect", Host: "h", User: "u", Key: "id"}); err != nil {
		t.Fatalf("write connect: %v", err)
	}
	mt, data := readFrame(t, ws)
	if mt != websocket.TextMessage || !strings.Contains(string(data), `"type":"error"`) {
		t.Fatalf("expected error frame, got mt=%d data=%s", mt, data)
	}
}

func TestBridge_RemoteExitReportsDisconnect(t *testing.T) {
	dialer := &fakeDialer{}
	srv, _ := newTestServer(t, dialer)
	ws := dialWS(t, srv)

	if err := ws.WriteJSON(clientMsg{Type: "connect", Host: "h", User: "u", Key: "id"}); err != nil {
		t.Fatalf("write connect: %v", err)
	}
	if mt, data := readFrame(t, ws); mt != websocket.TextMessage || !strings.Contains(string(data), "connected") {
		t.Fatalf("expected connected, got %s", data)
	}

	// Simulate the remote shell exiting.
	dialer.mu.Lock()
	c := dialer.created
	dialer.mu.Unlock()
	c.Close()

	mt, data := readFrame(t, ws)
	if mt != websocket.TextMessage || !strings.Contains(string(data), `"state":"disconnected"`) {
		t.Fatalf("expected disconnected status, got mt=%d data=%s", mt, data)
	}
}

func TestSameOrigin(t *testing.T) {
	mk := func(host, origin string) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "http://"+host+"/api/ssh/ws", nil)
		r.Host = host
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		return r
	}
	if !sameOrigin(mk("example.com", "")) {
		t.Fatalf("empty origin should be allowed")
	}
	if !sameOrigin(mk("example.com", "http://example.com")) {
		t.Fatalf("matching origin should be allowed")
	}
	if sameOrigin(mk("example.com", "http://evil.com")) {
		t.Fatalf("mismatched origin should be rejected")
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("condition not met within timeout")
}

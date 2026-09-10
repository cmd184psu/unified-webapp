package multissh

import (
	"errors"
	"log"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
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

// A broadcast to a password-authenticated target must leave an audit trail of
// which host was reached, and must not leave the password anywhere in it.
func TestBroadcastAuditLinesRecordTheHostAndNotTheCredential(t *testing.T) {
	const secret = "hunter2-do-not-leak"
	logged := captureLog(t)

	fake := &fakeTransferrer{}
	srv := New(Options{
		SSHKeyDir:      t.TempDir(),
		UploadDir:      t.TempDir(),
		MaxUploadBytes: 1024,
		MaxSessions:    3,
		Transferrer:    fake,
	})
	uploadID := stageUploadForBroadcastTest(t, srv, "bundle.tar", []byte("12345678"))

	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	startBroadcastJob(t, httpSrv.URL, map[string]any{
		"uploadId": uploadID,
		"targets": []map[string]any{
			{"host": "pw.example", "port": 22, "user": "ops", "password": secret, "remoteDir": "/opt/a"},
		},
	})

	waitForLog(t, logged, "outcome=ok")

	out := logged()
	for _, want := range []string{
		`multissh: audit broadcast host="pw.example"`,
		`user="ops"`,
		`remote="/opt/a/bundle.tar"`,
		"outcome=start",
		"outcome=ok",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("audit output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, secret) {
		t.Fatalf("audit output leaked the password:\n%s", out)
	}
}

// A failed target is audited too -- an operator reading the log must be able to
// tell "never attempted" from "attempted and failed".
func TestBroadcastAuditRecordsFailedTargets(t *testing.T) {
	logged := captureLog(t)

	fake := &fakeTransferrer{errorsBy: map[string]error{"bad.example": errTransferForAudit}}
	srv := New(Options{
		SSHKeyDir:      t.TempDir(),
		UploadDir:      t.TempDir(),
		MaxUploadBytes: 1024,
		MaxSessions:    3,
		Transferrer:    fake,
	})
	uploadID := stageUploadForBroadcastTest(t, srv, "bundle.tar", []byte("12345678"))

	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	startBroadcastJob(t, httpSrv.URL, map[string]any{
		"uploadId": uploadID,
		"targets": []map[string]any{
			{"host": "bad.example", "port": 22, "user": "ops", "password": "pw", "remoteDir": "/opt/b"},
		},
	})

	waitForLog(t, logged, "outcome=failed")
	if out := logged(); !strings.Contains(out, `audit broadcast host="bad.example"`) {
		t.Fatalf("expected an audited failure for bad.example:\n%s", out)
	}
}

// The rejected-upgrade line is the only diagnosis an operator gets when a proxy
// rewrites Host (R1), so it must name both sides of the comparison.
func TestBroadcastOriginRejectionIsAudited(t *testing.T) {
	logged := captureLog(t)

	req := httptest.NewRequest("GET", "/api/broadcast/ws?job=x", nil)
	req.Header.Set("Origin", "http://evil.example")
	req.Host = "multissh.example"

	if sameOrigin(req) {
		t.Fatal("expected a foreign Origin to be rejected")
	}
	out := logged()
	if !strings.Contains(out, `origin="http://evil.example"`) || !strings.Contains(out, `host="multissh.example"`) {
		t.Fatalf("rejection log must name both Origin and Host:\n%s", out)
	}
}

var errTransferForAudit = errors.New("transfer failed")

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

// NFR-4: the upload handler must stream. ParseMultipartForm buffers to memory
// up to its bound and spills the rest to temp files, which is exactly what the
// requirement forbids -- and no behavioral assertion distinguishes the two, so
// the guard is structural.
func TestUploadHandlerStreamsAndNeverParsesTheWholeForm(t *testing.T) {
	src, err := os.ReadFile("upload.go")
	if err != nil {
		t.Fatalf("read upload.go: %v", err)
	}
	body := string(src)
	if !strings.Contains(body, "r.MultipartReader()") {
		t.Fatal("upload.go must consume the request through r.MultipartReader()")
	}
	if strings.Contains(body, "ParseMultipartForm") {
		t.Fatal("upload.go must not call ParseMultipartForm: it buffers the upload")
	}
	if !strings.Contains(body, "io.Copy(") {
		t.Fatal("upload.go must stream the part with io.Copy")
	}
}

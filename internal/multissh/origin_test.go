package multissh

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// TestBroadcastOriginRejectionUsesBroadcastPrefix asserts the broadcast
// upgrade path's rejection line is distinguishable from the terminal
// bridge's -- an operator grepping logs needs to tell the two apart.
func TestBroadcastOriginRejectionUsesBroadcastPrefix(t *testing.T) {
	logged := captureLog(t)

	req := httptest.NewRequest("GET", "/api/broadcast/ws?job=x", nil)
	req.Header.Set("Origin", "http://evil.example")
	req.Host = "multissh.example"

	if sameOrigin(req) {
		t.Fatal("expected a foreign Origin to be rejected")
	}
	out := logged()
	if !strings.Contains(out, "broadcast ws upgrade rejected") {
		t.Fatalf("expected the broadcast-prefixed rejection line, got:\n%s", out)
	}
}

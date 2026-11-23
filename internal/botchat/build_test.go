package botchat

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"cmd184psu/unified-webapp/internal/botchat/config"
)

func TestBuildServesAuthMode(t *testing.T) {
	dir := t.TempDir()
	h, err := Build(config.Config{
		DataDir: dir,
		Auth:    config.AuthConfig{Mode: "none"},
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	t.Cleanup(func() {
		if c, ok := h.(io.Closer); ok {
			_ = c.Close()
		}
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/auth/mode", nil)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"mode":"none"`) {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

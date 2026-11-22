package botchat

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAF15_LargeAnswerAndCursorRequest(t *testing.T) {
	body := strings.Repeat("x", 2<<20)
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.RequestURI()
		_ = json.NewEncoder(w).Encode(map[string]any{"messages": []map[string]any{
			{"seq": 11, "from": "a", "content": body},
			{"seq": 9, "from": "old", "content": "before the cursor"},
		}})
	}))
	defer srv.Close()
	key := filepath.Join(t.TempDir(), "key")
	if err := os.WriteFile(key, []byte("k\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := &Client{BaseURL: srv.URL, KeyFile: key}
	ms, err := c.MessagesAfter(context.Background(), "room-1", 10, 25)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotPath, "after=10") || !strings.Contains(gotPath, "limit=25") {
		t.Fatalf("request = %s", gotPath)
	}
	if len(ms) != 1 || ms[0].Seq != 11 || len(ms[0].Content) != len(body) {
		t.Fatalf("messages = %d, first seq %d, content %d", len(ms), ms[0].Seq, len(ms[0].Content))
	}
}

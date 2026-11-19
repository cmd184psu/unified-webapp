package notify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time      { return c.t }
func (c *clock) add(d time.Duration) { c.t = c.t.Add(d) }

func TestDebounceAndExactText(t *testing.T) {
	var keys []string
	var sent []string
	c := &clock{t: time.Unix(1_000_000, 0)}
	w := &Watcher{
		Keys:  func(context.Context) ([]string, error) { return keys, nil },
		Send:  func(_ context.Context, _ Config, text string) error { sent = append(sent, text); return nil },
		Clock: c.now,
	}
	check := func(want bool, why string) {
		t.Helper()
		got, err := w.Check(context.Background())
		if err != nil || got != want {
			t.Fatalf("%s: sent=%v err=%v, want %v", why, got, err, want)
		}
	}
	keys = []string{"task_halted|p|1|T1|0"}
	check(false, "the first look only learns what is already there")
	keys = append(keys, "seat_question|p|1|T2|9")
	check(true, "a new item")
	c.add(time.Minute)
	keys = append(keys, "seat_silent|p|1|T3|10")
	check(false, "inside the 10 minute gap")
	c.add(5 * time.Minute)
	check(false, "still inside the gap")
	c.add(5 * time.Minute)
	check(true, "the item that came during the gap is announced when the gap ends")
	c.add(11 * time.Minute)
	check(false, "nothing new: no repeat")
	keys = nil
	check(false, "cleared")
	keys = []string{"task_halted|p|1|T9|0"}
	check(true, "new again after the gap")
	for _, s := range sent {
		if s != "Agentflow needs attention" {
			t.Fatalf("sent %q; the text must be exactly the fixed one", s)
		}
	}
	if len(sent) != 3 {
		t.Fatalf("sent %d", len(sent))
	}
}

func TestAnItemThatClearsDuringTheGapIsNotAnnounced(t *testing.T) {
	var keys []string
	n := 0
	c := &clock{t: time.Unix(1_000_000, 0)}
	w := &Watcher{Keys: func(context.Context) ([]string, error) { return keys, nil },
		Send: func(context.Context, Config, string) error { n++; return nil }, Clock: c.now}
	_, _ = w.Check(context.Background())
	keys = []string{"a"}
	_, _ = w.Check(context.Background())
	keys = []string{"a", "b"}
	c.add(time.Minute)
	_, _ = w.Check(context.Background())
	keys = nil
	c.add(time.Minute)
	_, _ = w.Check(context.Background())
	c.add(20 * time.Minute)
	_, _ = w.Check(context.Background())
	if n != 1 {
		t.Fatalf("sent %d, want 1", n)
	}
}

func TestDeliverCommandAndURL(t *testing.T) {
	var mu sync.Mutex
	var body map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		_ = json.NewDecoder(r.Body).Decode(&body)
	}))
	defer srv.Close()
	out := filepath.Join(t.TempDir(), "out")
	err := Deliver(context.Background(), Config{Command: "printf '%s' >" + out, URL: srv.URL}, Text)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(out)
	if string(got) != Text {
		t.Fatalf("command got %q", got)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(body) != 1 || body["text"] != Text {
		t.Fatalf("url got %v", body)
	}
	if !strings.HasPrefix(Text, "Agentflow") {
		t.Fatal("text changed")
	}
}

package jobs

import (
	"encoding/json"
	"strconv"
	"sync"
	"testing"
)

// TestConcurrentUpdateAndAll exercises the exact access pattern that raced in
// the reference code: one goroutine mutating a job while another marshals
// All() and reads Status/Error. It only compiles against the Option D Queue
// (the reference Queue has neither Update nor Get).
func TestConcurrentUpdateAndAll(t *testing.T) {
	q := New(10)
	q.Enqueue(&Job{ID: "j1", URL: "http://example.invalid", Status: Queued})
	<-q.ch // drain: keeps the job out of any worker's hands; test is self-contained

	const iters = 1000
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		for i := 0; i < iters; i++ {
			if i%2 == 0 {
				// Compound update: readers must never see Failed with an
				// empty Error.
				q.Update("j1", func(j *Job) {
					j.Error = "boom"
					j.Status = Failed
				})
			} else {
				q.Update("j1", func(j *Job) {
					j.Error = ""
					j.Status = Running
					j.Progress = "download " + strconv.Itoa(i)
				})
			}
		}
	}()

	go func() {
		defer wg.Done()
		for i := 0; i < iters; i++ {
			if _, err := json.Marshal(q.All()); err != nil {
				t.Errorf("marshal: %v", err)
				return
			}
			j, ok := q.Get("j1")
			if !ok {
				t.Error("Get lost the enqueued job")
				return
			}
			if j.Status == Failed && j.Error == "" {
				t.Error("observed Failed with empty Error: compound Update is not atomic")
				return
			}
		}
	}()

	wg.Wait()

	if q.Update("no-such-id", func(*Job) {}) {
		t.Error("Update on unknown ID returned true, want false")
	}
}

func TestJobJSONKeysUnchanged(t *testing.T) {
	q := New(1)
	q.Enqueue(&Job{
		ID: "k1", URL: "u", ShowName: "s", EpisodeTitle: "e",
		Season: 1, Episode: 2, Mode: "video",
		Status: Queued, Progress: "p", OutputFile: "o", Error: "x",
	})
	<-q.ch

	data, err := json.Marshal(q.All())
	if err != nil {
		t.Fatal(err)
	}
	var decoded []map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded) != 1 {
		t.Fatalf("decoded %d jobs, want 1", len(decoded))
	}
	want := []string{
		"ID", "URL", "ShowName", "EpisodeTitle", "Season", "Episode",
		"Mode", "Status", "Progress", "OutputFile", "Error",
	}
	if len(decoded[0]) != len(want) {
		t.Errorf("wire format has %d keys, want %d: %v", len(decoded[0]), len(want), decoded[0])
	}
	for _, k := range want {
		if _, ok := decoded[0][k]; !ok {
			t.Errorf("missing key %q", k)
		}
	}
}

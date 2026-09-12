package jobs

import "testing"

func TestQueueInsertionOrder(t *testing.T) {
	q := New(10)
	ids := []string{"a", "b", "c", "d", "e"}
	for _, id := range ids {
		q.Enqueue(&Job{ID: id, Status: Queued})
		<-q.ch // drain so Enqueue doesn't block
	}

	all := q.All()
	if len(all) != len(ids) {
		t.Fatalf("len: got %d, want %d", len(all), len(ids))
	}
	for i, j := range all {
		if j.ID != ids[i] {
			t.Errorf("position %d: got %q, want %q", i, j.ID, ids[i])
		}
	}
}

func TestAllReturnsCopy(t *testing.T) {
	q := New(10)
	q.Enqueue(&Job{ID: "x", Status: Queued})
	<-q.ch

	a := q.All()
	b := q.All()
	// mutating the slice from All() must not affect the queue's internal order
	a[0] = &Job{ID: "mutated"}
	if q.All()[0].ID != "x" {
		t.Error("All() returned a reference to internal slice")
	}
	_ = b
}

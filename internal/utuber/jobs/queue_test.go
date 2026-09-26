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
	a[0] = Job{ID: "mutated"}
	if q.All()[0].ID != "x" {
		t.Error("All() returned a reference to internal slice")
	}
	_ = b
}

func TestQueueRemove(t *testing.T) {
	q := New(10)
	for _, id := range []string{"a", "b", "c"} {
		q.Enqueue(&Job{ID: id, Status: Queued})
		<-q.ch
	}
	q.Update("b", func(j *Job) { j.Status = Running })
	q.Update("c", func(j *Job) { j.Status = Completed })

	if err := q.Remove("a"); err != nil {
		t.Fatalf("remove queued: %v", err)
	}
	if err := q.Remove("b"); err != ErrRunning {
		t.Errorf("remove running: got %v, want ErrRunning", err)
	}
	if err := q.Remove("c"); err != nil {
		t.Errorf("remove completed: %v", err)
	}
	if err := q.Remove("zzz"); err != ErrNotFound {
		t.Errorf("remove unknown: got %v, want ErrNotFound", err)
	}
	all := q.All()
	if len(all) != 1 || all[0].ID != "b" {
		t.Errorf("only the running job should remain, got %+v", all)
	}
}

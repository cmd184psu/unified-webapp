package sshproxy

import (
	"bytes"
	"context"
	"errors"
	"testing"
)

func TestProgressWriter_ReportsCumulativeBytesAcrossWrites(t *testing.T) {
	var out bytes.Buffer
	got := make([]int64, 0, 2)
	w := &progressWriter{
		ctx: context.Background(),
		w:   &out,
		progress: func(v int64) {
			got = append(got, v)
		},
	}

	if _, err := w.Write([]byte("abc")); err != nil {
		t.Fatalf("first write: %v", err)
	}
	if _, err := w.Write([]byte("de")); err != nil {
		t.Fatalf("second write: %v", err)
	}

	if out.String() != "abcde" {
		t.Fatalf("unexpected output: %q", out.String())
	}
	if len(got) != 2 || got[0] != 3 || got[1] != 5 {
		t.Fatalf("unexpected progress updates: %#v", got)
	}
}

func TestProgressWriter_ContextCancellationStopsWrite(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	w := &progressWriter{
		ctx:      ctx,
		w:        &bytes.Buffer{},
		progress: func(int64) {},
	}

	_, err := w.Write([]byte("x"))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context canceled, got %v", err)
	}
}

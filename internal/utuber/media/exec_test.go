package media

import (
	"context"
	"testing"
)

type fakeExec struct {
	lines []string
}

func (f fakeExec) Run(
	ctx context.Context,
	name string,
	args []string,
	onLine func(string),
) error {
	for _, l := range f.lines {
		onLine(l)
	}
	return nil
}

func TestProgressCapture(t *testing.T) {
	exec := fakeExec{lines: []string{"10%", "42%", "100%"}}

	var seen []string
	exec.Run(context.Background(), "x", nil, func(s string) {
		seen = append(seen, s)
	})

	if len(seen) != 3 {
		t.Fatal("expected progress lines")
	}
}

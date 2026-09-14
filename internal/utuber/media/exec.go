package media

import (
	"bufio"
	"context"
	"os/exec"
)

type Executor interface {
	Run(ctx context.Context, name string, args []string, onLine func(string)) error
}

type OSExecutor struct{}

func (OSExecutor) Run(ctx context.Context, name string, args []string, onLine func(string)) error {
	cmd := exec.CommandContext(ctx, name, args...)
	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()

	if err := cmd.Start(); err != nil {
		return err
	}

	scan := func(r *bufio.Scanner) {
		for r.Scan() {
			onLine(r.Text())
		}
	}

	go scan(bufio.NewScanner(stdout))
	go scan(bufio.NewScanner(stderr))

	return cmd.Wait()
}

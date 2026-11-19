package dockervm

// The macOS driver: Colima. The VM is started on demand and stopped after the
// idle period; it is never started at login.

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// colimaDriver runs a Colima VM through the colima CLI and checks readiness
// with the docker CLI.
type colimaDriver struct {
	docker, colima string
	timeout        time.Duration
}

func (d *colimaDriver) Name() string { return "colima" }

// Start brings the VM up. It first asks docker whether the runtime already
// answers, so a running VM costs one `docker info` and no `colima start`.
func (d *colimaDriver) Start(ctx context.Context) error {
	if d.info(ctx) == nil {
		return nil
	}
	startCtx, cancel := context.WithTimeout(ctx, d.timeout)
	defer cancel()
	out, err := exec.CommandContext(startCtx, d.colima, "start").CombinedOutput()
	if err != nil {
		return fmt.Errorf("colima start: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return d.wait(startCtx)
}

// wait polls docker info until the daemon answers or the deadline passes.
func (d *colimaDriver) wait(ctx context.Context) error {
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	var last error
	for {
		if err := d.info(ctx); err == nil {
			return nil
		} else {
			last = err
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("docker did not answer after colima start: %w (last: %v)", ctx.Err(), last)
		case <-t.C:
		}
	}
}

func (d *colimaDriver) Stop(ctx context.Context) error {
	out, err := exec.CommandContext(ctx, d.colima, "stop").CombinedOutput()
	if err != nil {
		return fmt.Errorf("colima stop: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (d *colimaDriver) Running(ctx context.Context) (int, error) {
	return countContainers(ctx, d.docker)
}

func (d *colimaDriver) Status(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, d.colima, "status").CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// info is `docker info` with the timeout Start uses.
func (d *colimaDriver) info(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, d.docker, "info", "--format", "{{.ServerVersion}}")
	return cmd.Run()
}

// countContainers is the number of this instance's running containers.
func countContainers(ctx context.Context, docker string) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, docker, "ps", "--filter", "label=agentflow=1", "--format", "{{.Names}}").Output()
	if err != nil {
		return 0, err
	}
	n := 0
	for _, l := range strings.Split(string(out), "\n") {
		if strings.TrimSpace(l) != "" {
			n++
		}
	}
	return n, nil
}

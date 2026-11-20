package dockervm

// The macOS driver: Colima. The VM is started on demand and stopped after the
// idle period; it is never started at login.

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// colimaDriver runs a Colima VM through the colima CLI and checks readiness
// with the docker CLI.
type colimaDriver struct {
	docker, colima string
	timeout        time.Duration
	// config overrides the colima.yaml read by Shared (tests).
	config string
	// startedHere: this process ran `colima start`. A VM someone else started is theirs:
	// the idle stop leaves it running. The manager serializes Start and Stop.
	startedHere bool
}

// Owned says agentflow started the VM, so the idle stop may stop it.
func (d *colimaDriver) Owned() bool { return d.startedHere }

func (d *colimaDriver) Name() string { return "colima" }

// Shared lists the host directories the VM mounts, from the default profile's
// colima.yaml ($COLIMA_HOME, else ~/.colima). Colima shares the home directory and
// /tmp/colima when the file names no mounts. Nil when the file cannot be read: the
// sandbox then cannot check and lets docker decide.
func (d *colimaDriver) Shared() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	dir := os.Getenv("COLIMA_HOME")
	if dir == "" {
		dir = filepath.Join(home, ".colima")
	}
	path := d.config
	if path == "" {
		path = filepath.Join(dir, "default", "colima.yaml")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return colimaMounts(string(b), home)
}

// colimaMounts reads the mounts list of a colima.yaml: the "location:" of each entry under
// the top-level "mounts:" key. An empty or absent list is Colima's default share.
func colimaMounts(yaml, home string) []string {
	var out []string
	in := false
	for _, line := range strings.Split(yaml, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "-") {
			in = strings.HasPrefix(line, "mounts:")
			continue
		}
		if !in {
			continue
		}
		trimmed = strings.TrimPrefix(trimmed, "- ")
		if loc, ok := strings.CutPrefix(trimmed, "location:"); ok {
			loc = strings.Trim(strings.TrimSpace(loc), `"'`)
			if strings.HasPrefix(loc, "~/") {
				loc = filepath.Join(home, loc[2:])
			} else if loc == "~" {
				loc = home
			}
			if filepath.IsAbs(loc) {
				out = append(out, filepath.Clean(loc))
			}
		}
	}
	if len(out) == 0 {
		return []string{home, "/tmp/colima"}
	}
	return out
}

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
	d.startedHere = true
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
	d.startedHere = false
	return nil
}

// Running counts every running container in the VM, not only agentflow's: stopping the VM
// would end them all, so any one of them keeps it up.
func (d *colimaDriver) Running(ctx context.Context) (int, error) {
	return countContainers(ctx, d.docker, false)
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

// countContainers is the number of running containers: agentflow's only (label
// agentflow=1), or all of them.
func countContainers(ctx context.Context, docker string, agentflowOnly bool) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	args := []string{"ps", "--format", "{{.Names}}"}
	if agentflowOnly {
		args = []string{"ps", "--filter", "label=agentflow=1", "--format", "{{.Names}}"}
	}
	out, err := exec.CommandContext(ctx, docker, args...).Output()
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

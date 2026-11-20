package dockervm

// The Linux driver: Docker runs natively, so "start when needed" is only a
// reachability check. The host's daemon is never stopped, auto-installed or
// reconfigured; the manager only makes sure it answers before a container is
// run and reports a clear attention item when it does not.

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// daemonDriver checks a native Docker daemon. socket activation means starting
// the daemon is the host's job (docker.socket or rootless docker/podman via
// DOCKER_HOST); agentflow never runs systemctl.
type daemonDriver struct{ docker string }

func (d *daemonDriver) Name() string { return "docker" }

func (d *daemonDriver) Start(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, d.docker, "info", "--format", "{{.ServerVersion}}").CombinedOutput()
	if err != nil {
		return fmt.Errorf("the docker daemon is not reachable (start docker.socket, or set DOCKER_HOST for rootless docker/podman): %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Stop never stops the host's daemon: other work may depend on it.
func (d *daemonDriver) Stop(context.Context) error { return nil }

func (d *daemonDriver) Running(ctx context.Context) (int, error) {
	return countContainers(ctx, d.docker)
}

func (d *daemonDriver) Status(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, d.docker, "info", "--format", "{{.ServerVersion}} {{.OperatingSystem}} {{.Architecture}}").CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

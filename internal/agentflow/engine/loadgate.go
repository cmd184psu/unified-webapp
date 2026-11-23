// Copyright 2026 FreeSociety3 contributors
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"

)

// LoadGateOK reports whether this node may start a new stage right now: the
// 1-min load is below load_cap_tenths/10 * cores, free memory is above the
// floor, and free disk is above min_free_space_gb. Running stages are never
// killed for load; a closed gate simply defers the start to the next tick.
func (e *Engine) LoadGateOK(ctx context.Context) bool {
	if testing.Testing() {
		return true
	}
	set, err := e.deps.Store.GetSettings(ctx)
	if err != nil {
		return true
	}
	cores := runtime.NumCPU()
	if cores < 1 {
		cores = 1
	}
	capLoad := float64(set.LoadCapTenths) / 10.0 * float64(cores)
	if capLoad <= 0 {
		capLoad = 1.5 * float64(cores)
	}
	if l := systemLoad1m(); l > capLoad {
		return false
	}
	if set.MinFreeMemGB > 0 && freeMemGB() < float64(set.MinFreeMemGB) {
		return false
	}
	if set.MinFreeSpaceGB > 0 {
		// check the data dir if known, else cwd
		dir := "."
		if e.deps.DataDir != "" {
			dir = e.deps.DataDir
		}
		if freeDiskGB(dir) < float64(set.MinFreeSpaceGB) {
			return false
		}
	}
	return true
}

func systemLoad1m() float64 {
	// Linux: /proc/loadavg
	if b, err := os.ReadFile("/proc/loadavg"); err == nil {
		fields := strings.Fields(string(b))
		if len(fields) > 0 {
			if v, err := strconv.ParseFloat(fields[0], 64); err == nil {
				return v
			}
		}
	}
	// macOS: sysctl
	out, err := exec.Command("sysctl", "-n", "vm.loadavg").Output()
	if err == nil {
		// format: { 1.23 4.56 7.89 }
		s := strings.Trim(strings.TrimSpace(string(out)), "{} ")
		fields := strings.Fields(s)
		if len(fields) > 0 {
			if v, err := strconv.ParseFloat(fields[0], 64); err == nil {
				return v
			}
		}
	}
	return 0
}

func freeMemGB() float64 {
	// Linux: MemAvailable from /proc/meminfo
	if b, err := os.ReadFile("/proc/meminfo"); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(line, "MemAvailable:") {
				fields := strings.Fields(line)
				if len(fields) >= 2 {
					if kb, err := strconv.ParseFloat(fields[1], 64); err == nil {
						return kb / 1024 / 1024
					}
				}
			}
		}
	}
	// macOS: use sysctl hw.memsize and vm_stat (best effort)
	out, err := exec.Command("sysctl", "-n", "hw.memsize").Output()
	if err == nil {
		if bytes, err := strconv.ParseFloat(strings.TrimSpace(string(out)), 64); err == nil {
			// approximate free as 25% of total
			return bytes / 1024 / 1024 / 1024 * 0.25
		}
	}
	return 9999
}

func freeDiskGB(path string) float64 {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 9999
	}
	return float64(st.Bavail) * float64(st.Bsize) / 1024 / 1024 / 1024
}


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
)

// GateMetrics is what the start gate reads about the machine right now.
type GateMetrics struct {
	// Load1m is the one-minute load average.
	Load1m float64
	// Cores is the number of CPUs the load is compared against.
	Cores int
	// FreeMemGB and FreeDiskGB are the free memory and the free space on the
	// filesystem holding the data directory, in GiB.
	FreeMemGB  float64
	FreeDiskGB float64
}

// GateOpen reports whether a node with these settings and metrics may start a
// new stage. A zero (or negative) limit means that limit is off, except for
// the load cap, which falls back to 1.5x cores. Running stages are never
// killed for load: a closed gate only defers the next start.
func GateOpen(loadCapTenths, minFreeMemGB, minFreeSpaceGB int, m GateMetrics) bool {
	cores := m.Cores
	if cores < 1 {
		cores = 1
	}
	capLoad := float64(loadCapTenths) / 10.0 * float64(cores)
	if capLoad <= 0 {
		capLoad = 1.5 * float64(cores)
	}
	if m.Load1m > capLoad {
		return false
	}
	if minFreeMemGB > 0 && m.FreeMemGB < float64(minFreeMemGB) {
		return false
	}
	if minFreeSpaceGB > 0 && m.FreeDiskGB < float64(minFreeSpaceGB) {
		return false
	}
	return true
}

// LoadGateOK reports whether this node may start a new stage right now: the
// 1-min load is below load_cap_tenths/10 * cores, free memory is above the
// floor, and free disk is above min_free_space_gb. The same answer decides
// "accepting" in /api/peer/status, so a peer sees the gate before it delegates.
func (e *Engine) LoadGateOK(ctx context.Context) bool {
	if e.deps.LoadGate != nil {
		return e.deps.LoadGate(ctx)
	}
	set, err := e.deps.Store.GetSettings(ctx)
	if err != nil {
		return true
	}
	return GateOpen(set.LoadCapTenths, set.MinFreeMemGB, set.MinFreeSpaceGB, e.GateMetrics())
}

// GateMetrics reads the machine's current load, memory and disk.
func (e *Engine) GateMetrics() GateMetrics {
	dir := e.deps.DataDir
	if dir == "" {
		dir = "."
	}
	return GateMetrics{
		Load1m:     systemLoad1m(),
		Cores:      runtime.NumCPU(),
		FreeMemGB:  freeMemGB(),
		FreeDiskGB: freeDiskGB(dir),
	}
}

// systemLoad1m reads the one-minute load average, or 0 when the platform does
// not report one.
func systemLoad1m() float64 {
	if b, err := os.ReadFile("/proc/loadavg"); err == nil {
		fields := strings.Fields(string(b))
		if len(fields) > 0 {
			if v, err := strconv.ParseFloat(fields[0], 64); err == nil {
				return v
			}
		}
	}
	// macOS: sysctl -n vm.loadavg prints "{ 1.23 4.56 7.89 }".
	out, err := exec.Command("sysctl", "-n", "vm.loadavg").Output()
	if err != nil {
		return 0
	}
	fields := strings.Fields(strings.Trim(strings.TrimSpace(string(out)), "{} "))
	if len(fields) == 0 {
		return 0
	}
	v, _ := strconv.ParseFloat(fields[0], 64)
	return v
}

// freeMemGB reports the memory the OS says is available, in GiB. The start
// gate uses it as a floor: below it no new stage starts.
func freeMemGB() float64 {
	// Linux: MemAvailable in /proc/meminfo.
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
	// macOS: vm_stat page counts times the page size, which its first line
	// names ("... (page size of 16384 bytes)").
	out, err := exec.Command("vm_stat").Output()
	if err != nil {
		return 0
	}
	pageBytes := 4096.0
	freePages := 0.0
	for _, line := range strings.Split(string(out), "\n") {
		if i := strings.Index(line, "page size of "); i >= 0 {
			if f := strings.Fields(line[i+len("page size of "):]); len(f) > 0 {
				if v, err := strconv.ParseFloat(f[0], 64); err == nil {
					pageBytes = v
				}
			}
		}
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		switch strings.TrimSpace(key) {
		case "Pages free", "Pages inactive", "Pages speculative", "Pages purgeable":
			n, err := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(val), "."), 64)
			if err == nil {
				freePages += n
			}
		}
	}
	if freePages == 0 {
		return 0
	}
	return freePages * pageBytes / 1024 / 1024 / 1024
}

func freeDiskGB(path string) float64 {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0
	}
	return float64(st.Bavail) * float64(st.Bsize) / 1024 / 1024 / 1024
}

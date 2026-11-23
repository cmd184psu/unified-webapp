// Copyright 2026 FreeSociety3 contributors
// SPDX-License-Identifier: Apache-2.0

//go:build darwin

package api

import "golang.org/x/sys/unix"

// load1m reports the host's one-minute load average, or 0 when unavailable.
func load1m() float64 {
	raw, err := unix.SysctlRaw("vm.loadavg")
	if err != nil {
		return 0
	}
	return parseLoadavg(raw)
}

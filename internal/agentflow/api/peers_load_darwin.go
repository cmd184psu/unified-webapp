// Copyright 2026 FreeSociety3 contributors
// SPDX-License-Identifier: Apache-2.0

//go:build darwin

package api

import (
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

// load1m reports the host's one-minute load average, or 0 when unavailable.
func load1m() float64 {
	s, err := unix.Sysctl("vm.loadavg")
	if err != nil {
		return 0
	}
	f := strings.Fields(strings.Trim(s, "{} "))
	if len(f) == 0 {
		return 0
	}
	v, err := strconv.ParseFloat(f[0], 64)
	if err != nil {
		return 0
	}
	return v
}

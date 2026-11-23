// Copyright 2026 FreeSociety3 contributors
// SPDX-License-Identifier: Apache-2.0

//go:build linux

package api

import (
	"os"
	"strconv"
	"strings"
)

// load1m reports the host's one-minute load average, or 0 when unavailable.
func load1m() float64 {
	b, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return 0
	}
	f := strings.Fields(string(b))
	if len(f) == 0 {
		return 0
	}
	v, err := strconv.ParseFloat(f[0], 64)
	if err != nil {
		return 0
	}
	return v
}

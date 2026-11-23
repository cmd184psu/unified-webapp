// Copyright 2026 FreeSociety3 contributors
// SPDX-License-Identifier: Apache-2.0

//go:build darwin

package api

import (
	"encoding/binary"

	"golang.org/x/sys/unix"
)

// struct loadavg on 64-bit darwin is three 32-bit fixpt_t values followed by
// padding to long's 8-byte alignment, then fscale: 24 bytes in all.
const (
	darwinLoadavgSize  = 24
	darwinFscaleOffset = 16
)

// load1m reports the host's one-minute load average, or 0 when unavailable.
func load1m() float64 {
	raw, err := unix.SysctlRaw("vm.loadavg")
	if err != nil {
		return 0
	}
	return parseLoadavg(raw)
}

// parseLoadavg decodes struct loadavg. Sysctl("vm.loadavg") reads the binary
// struct as text and always yields 0; the averages are fixed point, so the
// first one must be divided by fscale.
func parseLoadavg(raw []byte) float64 {
	if len(raw) < darwinLoadavgSize {
		return 0
	}
	scale := int64(binary.LittleEndian.Uint64(raw[darwinFscaleOffset:darwinLoadavgSize]))
	if scale <= 0 {
		return 0
	}
	return float64(binary.LittleEndian.Uint32(raw[0:4])) / float64(scale)
}

// Copyright 2026 FreeSociety3 contributors
// SPDX-License-Identifier: Apache-2.0

package api

import "encoding/binary"

// parseLoadavg decodes darwin's `struct loadavg { fixpt_t ldavg[3]; long fscale; }`
// as SysctlRaw returns it: three little-endian uint32 fixed-point values, four
// bytes of padding, then the int64 scale. The text form of vm.loadavg is binary
// too, which is why reading it as a string always yielded zero.
func parseLoadavg(raw []byte) float64 {
	if len(raw) < 24 {
		return 0
	}
	scale := binary.LittleEndian.Uint64(raw[16:])
	if scale == 0 {
		return 0
	}
	return float64(binary.LittleEndian.Uint32(raw[0:])) / float64(scale)
}

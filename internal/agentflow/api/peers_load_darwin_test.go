// Copyright 2026 FreeSociety3 contributors
// SPDX-License-Identifier: Apache-2.0

//go:build darwin

package api

import (
	"encoding/binary"
	"testing"

	"golang.org/x/sys/unix"
)

func TestParseLoadavgFixedPoint(t *testing.T) {
	raw := make([]byte, darwinLoadavgSize)
	binary.LittleEndian.PutUint32(raw[0:4], 3072)
	binary.LittleEndian.PutUint64(raw[darwinFscaleOffset:], 2048)
	if got := parseLoadavg(raw); got != 1.5 {
		t.Fatalf("parseLoadavg = %v, want 1.5", got)
	}
	if got := parseLoadavg(raw[:darwinLoadavgSize-1]); got != 0 {
		t.Fatalf("short buffer = %v, want 0", got)
	}
	binary.LittleEndian.PutUint64(raw[darwinFscaleOffset:], 0)
	if got := parseLoadavg(raw); got != 0 {
		t.Fatalf("zero fscale = %v, want 0", got)
	}
}

func TestLoadavgSysctlLayout(t *testing.T) {
	raw, err := unix.SysctlRaw("vm.loadavg")
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) != darwinLoadavgSize {
		t.Fatalf("vm.loadavg returned %d bytes, want %d", len(raw), darwinLoadavgSize)
	}
	if got := parseLoadavg(raw); got < 0 {
		t.Fatalf("load1m = %v, want >= 0", got)
	}
}

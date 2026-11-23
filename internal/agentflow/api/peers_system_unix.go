// Copyright 2026 FreeSociety3 contributors
// SPDX-License-Identifier: Apache-2.0

//go:build unix

package api

import "golang.org/x/sys/unix"

// freeGB reports the free space available to the data directory in GiB.
func freeGB(dir string) float64 {
	if dir == "" {
		return 0
	}
	var st unix.Statfs_t
	if err := unix.Statfs(dir, &st); err != nil {
		return 0
	}
	return float64(st.Bavail) * float64(st.Bsize) / (1 << 30)
}

// Copyright 2026 FreeSociety3 contributors
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	"testing"

	"cmd184psu/unified-webapp/internal/agentflow/store"
)

func TestGateOpen(t *testing.T) {
	cases := []struct {
		name  string
		cap   int
		mem   int
		disk  int
		m     GateMetrics
		opens bool
	}{
		{"idle", 15, 2, 20, GateMetrics{Load1m: 1, Cores: 8, FreeMemGB: 8, FreeDiskGB: 100}, true},
		{"load over 1.5x cores", 15, 2, 20, GateMetrics{Load1m: 12.1, Cores: 8, FreeMemGB: 8, FreeDiskGB: 100}, false},
		{"load at the cap", 15, 2, 20, GateMetrics{Load1m: 12, Cores: 8, FreeMemGB: 8, FreeDiskGB: 100}, true},
		{"custom cap", 20, 2, 20, GateMetrics{Load1m: 15, Cores: 8, FreeMemGB: 8, FreeDiskGB: 100}, true},
		{"memory floor", 15, 4, 20, GateMetrics{Load1m: 1, Cores: 8, FreeMemGB: 3.9, FreeDiskGB: 100}, false},
		{"disk floor", 15, 2, 20, GateMetrics{Load1m: 1, Cores: 8, FreeMemGB: 8, FreeDiskGB: 19}, false},
		{"zero limits off", 0, 0, 0, GateMetrics{Load1m: 1, Cores: 8, FreeMemGB: 0.1, FreeDiskGB: 0.1}, true},
		{"zero cap falls back to 1.5x", 0, 0, 0, GateMetrics{Load1m: 13, Cores: 8, FreeMemGB: 8, FreeDiskGB: 100}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := GateOpen(c.cap, c.mem, c.disk, c.m); got != c.opens {
				t.Fatalf("GateOpen = %v, want %v", got, c.opens)
			}
		})
	}
}

// TestSubmitRefusesClosedGate is the wiring: a closed gate refuses a stage
// before a stage row or lane job exists, so nothing half-starts.
func TestSubmitRefusesClosedGate(t *testing.T) {
	h := newHarness(t)
	h.tweak = func(d *Deps) {
		d.LoadGate = func(context.Context) bool { return false }
	}
	h.start()
	defer h.closeAll()
	h.backend("gpu", 1)
	if _, _, err := h.eng.Submit(context.Background(), "gpu-1",
		store.StageSpec{Role: "worker", Backend: "gpu", Model: "m1"},
		StagePayload{}); err == nil {
		t.Fatal("Submit started a stage with the gate closed")
	}
	open, err := h.st.OpenStages(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 0 {
		t.Fatalf("stages = %+v, want none", open)
	}
}

func TestLoadGateReadsTheSettings(t *testing.T) {
	h := newHarness(t)
	h.tweak = func(d *Deps) {
		d.LoadGate = nil // the real gate
	}
	h.start()
	defer h.closeAll()
	ctx := context.Background()
	// Generous limits open the gate on any machine this test runs on; a memory
	// floor far above the host must close it, which proves the settings are
	// read rather than hardcoded.
	if err := h.st.UpdateSettings(ctx, "tester", map[string]string{
		"load_cap_tenths":   "100",
		"min_free_mem_gb":   "1",
		"min_free_space_gb": "1",
	}); err != nil {
		t.Fatal(err)
	}
	if !h.eng.LoadGateOK(ctx) {
		t.Fatalf("gate closed with generous limits: %+v", h.eng.GateMetrics())
	}
	if err := h.st.UpdateSettings(ctx, "tester", map[string]string{"min_free_mem_gb": "10000"}); err != nil {
		t.Fatal(err)
	}
	if h.eng.LoadGateOK(ctx) {
		t.Fatalf("gate open with a 10000 GiB memory floor: %+v", h.eng.GateMetrics())
	}
}

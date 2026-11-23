// Copyright 2026 FreeSociety3 contributors
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestGateAllowsOnlyPeerPairUnauthenticated(t *testing.T) {
	svc := newGateService(t, time.Now(), &Policy{Modules: map[string]ModulePolicy{
		"agentflow": {}, "grocery": {},
	}})

	reached := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	gate := svc.Gate("agentflow", reached)

	post := func(path string) int {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, path, nil)
		gate.ServeHTTP(rec, req)
		return rec.Code
	}
	if got := post("/api/peer/pair"); got != http.StatusNoContent {
		t.Errorf("POST /api/peer/pair = %d, want it to reach the handler", got)
	}
	for _, path := range []string{"/api/peer/unpair", "/api/peer/rotate", "/api/peers/invite"} {
		if got := post(path); got != http.StatusUnauthorized {
			t.Errorf("POST %s = %d, want 401", path, got)
		}
	}

	// The allowlist is scoped to agentflow: the same path on another module is
	// still gated.
	other := svc.Gate("grocery", reached)
	rec := httptest.NewRecorder()
	other.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/peer/pair", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("grocery /api/peer/pair = %d, want 401", rec.Code)
	}
}

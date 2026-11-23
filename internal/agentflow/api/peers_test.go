// Copyright 2026 FreeSociety3 contributors
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func enablePeers(t *testing.T, e *env) {
	t.Helper()
	err := e.st.UpdateSettings(t.Context(), "test", map[string]string{"peers_enabled": "on"})
	if err != nil {
		t.Fatal(err)
	}
}

func postJSON(url string, v any) (*http.Response, error) {
	b, _ := json.Marshal(v)
	return http.Post(url, "application/json", bytes.NewReader(b))
}

func TestPeersHandshake(t *testing.T) {
	ea := newEnv(t, envOpt{})
	eb := newEnv(t, envOpt{})
	enablePeers(t, ea)
	enablePeers(t, eb)

	srvA := httptest.NewServer(ea.mux)
	defer srvA.Close()
	srvB := httptest.NewServer(eb.mux)
	defer srvB.Close()

	resp, err := postJSON(srvA.URL+"/api/peers/invite", map[string]any{"name": "builder"})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var inv struct {
		Code string `json:"code"`
	}
	json.NewDecoder(resp.Body).Decode(&inv)
	if inv.Code == "" {
		t.Fatal("no code")
	}

	accBody := map[string]any{"a_url": srvA.URL, "code": inv.Code, "name": "laptop", "b_url": srvB.URL}
	resp2, _ := postJSON(srvB.URL+"/api/peers/accept", accBody)
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("accept status %d", resp2.StatusCode)
	}

	keyA, err := ea.api.readPeerKey("builder")
	if err != nil {
		t.Fatal(err)
	}
	keyB, err := eb.api.readPeerKey("laptop")
	if err != nil {
		t.Fatal(err)
	}

	req, _ := http.NewRequest(http.MethodGet, srvB.URL+"/api/peer/status", nil)
	req.Header.Set("Authorization", "Bearer "+keyA)
	r, _ := http.DefaultClient.Do(req)
	r.Body.Close()
	if r.StatusCode != http.StatusOK {
		t.Fatalf("A->B status %d", r.StatusCode)
	}

	req2, _ := http.NewRequest(http.MethodGet, srvA.URL+"/api/peer/status", nil)
	req2.Header.Set("Authorization", "Bearer "+keyB)
	r2, _ := http.DefaultClient.Do(req2)
	r2.Body.Close()
	if r2.StatusCode != http.StatusOK {
		t.Fatalf("B->A status %d", r2.StatusCode)
	}
}

func TestPeerCodeSingleUse(t *testing.T) {
	ea := newEnv(t, envOpt{})
	eb := newEnv(t, envOpt{})
	enablePeers(t, ea)
	enablePeers(t, eb)
	srvA := httptest.NewServer(ea.mux)
	defer srvA.Close()
	srvB := httptest.NewServer(eb.mux)
	defer srvB.Close()

	resp, _ := postJSON(srvA.URL+"/api/peers/invite", map[string]any{"name": "builder"})
	var inv struct {
		Code string `json:"code"`
	}
	json.NewDecoder(resp.Body).Decode(&inv)
	resp.Body.Close()

	// first accept ok
	r1, _ := postJSON(srvB.URL+"/api/peers/accept", map[string]any{"a_url": srvA.URL, "code": inv.Code, "name": "laptop", "b_url": srvB.URL})
	r1.Body.Close()

	// second accept with same code should fail
	eb2 := newEnv(t, envOpt{})
	enablePeers(t, eb2)
	srvB2 := httptest.NewServer(eb2.mux)
	defer srvB2.Close()
	r2, _ := postJSON(srvB2.URL+"/api/peers/accept", map[string]any{"a_url": srvA.URL, "code": inv.Code, "name": "laptop2", "b_url": srvB2.URL})
	defer r2.Body.Close()
	if r2.StatusCode == http.StatusOK {
		t.Fatal("expected failure on reused code")
	}
}

func TestPeerRevoke(t *testing.T) {
	ea := newEnv(t, envOpt{})
	eb := newEnv(t, envOpt{})
	enablePeers(t, ea)
	enablePeers(t, eb)
	srvA := httptest.NewServer(ea.mux)
	defer srvA.Close()
	srvB := httptest.NewServer(eb.mux)
	defer srvB.Close()

	resp, _ := postJSON(srvA.URL+"/api/peers/invite", map[string]any{"name": "builder"})
	var inv struct {
		Code string `json:"code"`
	}
	json.NewDecoder(resp.Body).Decode(&inv)
	resp.Body.Close()

	r1, _ := postJSON(srvB.URL+"/api/peers/accept", map[string]any{"a_url": srvA.URL, "code": inv.Code, "name": "laptop", "b_url": srvB.URL})
	r1.Body.Close()

	// revoke on A
	req, _ := http.NewRequest(http.MethodDelete, srvA.URL+"/api/peers/builder", nil)
	r, _ := http.DefaultClient.Do(req)
	r.Body.Close()
	if r.StatusCode != http.StatusOK {
		t.Fatalf("revoke status %d", r.StatusCode)
	}

	// peer gone
	if _, err := ea.st.GetPeer(t.Context(), "builder"); err == nil {
		t.Fatal("peer still exists")
	}
	// inbound key gone
	if _, err := ea.st.InboundKeyHash(t.Context(), "builder"); err == nil {
		t.Fatal("inbound key still exists")
	}
	// outbound file gone
	if _, err := ea.api.readPeerKey("builder"); err == nil {
		t.Fatal("key file still exists")
	}
}

func TestNoKeyInOutput(t *testing.T) {
	ea := newEnv(t, envOpt{})
	eb := newEnv(t, envOpt{})
	enablePeers(t, ea)
	enablePeers(t, eb)
	srvA := httptest.NewServer(ea.mux)
	defer srvA.Close()
	srvB := httptest.NewServer(eb.mux)
	defer srvB.Close()

	resp, _ := postJSON(srvA.URL+"/api/peers/invite", map[string]any{"name": "builder"})
	var inv struct {
		Code string `json:"code"`
	}
	json.NewDecoder(resp.Body).Decode(&inv)
	resp.Body.Close()

	r1, _ := postJSON(srvB.URL+"/api/peers/accept", map[string]any{"a_url": srvA.URL, "code": inv.Code, "name": "laptop", "b_url": srvB.URL})
	r1.Body.Close()

	keyA, _ := ea.api.readPeerKey("builder")
	// capture all output: list peers, status, logs not accessible; check responses
	respL, _ := http.Get(srvA.URL + "/api/peers")
	body := readAll(respL)
	respL.Body.Close()
	if bytes.Contains(body, []byte(keyA)) {
		t.Fatal("key leaked in peers list")
	}
	// status response
	keyB, _ := eb.api.readPeerKey("laptop")
	req, _ := http.NewRequest(http.MethodGet, srvA.URL+"/api/peer/status", nil)
	req.Header.Set("Authorization", "Bearer "+keyB)
	r, _ := http.DefaultClient.Do(req)
	b := readAll(r)
	r.Body.Close()
	if bytes.Contains(b, []byte(keyB)) {
		t.Fatal("key leaked in status")
	}
	_ = keyA
}

func readAll(r *http.Response) []byte {
	b := make([]byte, 0, 4096)
	buf := make([]byte, 4096)
	for {
		n, err := r.Body.Read(buf)
		if n > 0 {
			b = append(b, buf[:n]...)
		}
		if err != nil {
			break
		}
	}
	return b
}

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

// pairPeers runs the invite/accept handshake and returns the two servers and
// the keys each side uses to call the other.
func pairPeers(t *testing.T) (ea, eb *env, srvA, srvB *httptest.Server, keyAtoB, keyBtoA string) {
	t.Helper()
	ea = newEnv(t, envOpt{})
	eb = newEnv(t, envOpt{})
	enablePeers(t, ea)
	enablePeers(t, eb)
	srvA = httptest.NewServer(ea.mux)
	t.Cleanup(srvA.Close)
	srvB = httptest.NewServer(eb.mux)
	t.Cleanup(srvB.Close)
	resp, err := postJSON(srvA.URL+"/api/peers/invite", map[string]any{"name": "builder"})
	if err != nil {
		t.Fatal(err)
	}
	var inv struct {
		Code string `json:"code"`
	}
	json.NewDecoder(resp.Body).Decode(&inv)
	resp.Body.Close()
	r, err := postJSON(srvB.URL+"/api/peers/accept", map[string]any{
		"a_url": srvA.URL, "code": inv.Code, "name": "laptop", "b_url": srvB.URL, "host_header": "b.example",
	})
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if r.StatusCode != http.StatusOK {
		t.Fatalf("accept status %d", r.StatusCode)
	}
	keyAtoB, err = ea.api.readPeerKey("builder")
	if err != nil {
		t.Fatal(err)
	}
	keyBtoA, err = eb.api.readPeerKey("laptop")
	if err != nil {
		t.Fatal(err)
	}
	return
}

func peerStatus(t *testing.T, srv *httptest.Server, key string) (*http.Response, []byte) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/peer/status", nil)
	req.Header.Set("Authorization", "Bearer "+key)
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b := readAll(r)
	r.Body.Close()
	return r, b
}

func TestPeerRotateBothHalves(t *testing.T) {
	ea, eb, srvA, srvB, keyAtoB, keyBtoA := pairPeers(t)
	oldBtoA := keyBtoA

	r, err := postJSON(srvA.URL+"/api/peers/builder/rotate", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	body := readAll(r)
	r.Body.Close()
	if r.StatusCode != http.StatusOK {
		t.Fatalf("rotate status %d: %s", r.StatusCode, body)
	}
	newKeyAtoB, err := ea.api.readPeerKey("builder")
	if err != nil {
		t.Fatal(err)
	}
	newKeyBtoA, err := eb.api.readPeerKey("laptop")
	if err != nil {
		t.Fatal(err)
	}
	if newKeyAtoB == keyAtoB || newKeyBtoA == oldBtoA {
		t.Fatal("rotate kept an old key")
	}
	if bytes.Contains(body, []byte(newKeyAtoB)) || bytes.Contains(body, []byte(newKeyBtoA)) || bytes.Contains(body, []byte(keyAtoB)) {
		t.Fatal("rotate response leaks a key")
	}

	if resp, _ := peerStatus(t, srvB, keyAtoB); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("old A->B key still works: %d", resp.StatusCode)
	}
	if resp, _ := peerStatus(t, srvA, oldBtoA); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("old B->A key still works: %d", resp.StatusCode)
	}
	if resp, body := peerStatus(t, srvB, newKeyAtoB); resp.StatusCode != http.StatusOK {
		t.Errorf("new A->B key rejected: %d %s", resp.StatusCode, body)
	}
	if resp, body := peerStatus(t, srvA, newKeyBtoA); resp.StatusCode != http.StatusOK {
		t.Errorf("new B->A key rejected: %d %s", resp.StatusCode, body)
	}
}

func TestPeerUnpairRequiresKey(t *testing.T) {
	_, eb, srvA, srvB, keyAtoB, _ := pairPeers(t)
	_ = srvA

	r, _ := postJSON(srvB.URL+"/api/peer/unpair", map[string]any{})
	r.Body.Close()
	if r.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated unpair status %d, want 401", r.StatusCode)
	}
	if _, err := eb.st.GetPeer(t.Context(), "laptop"); err != nil {
		t.Fatal("peer removed by an unauthenticated unpair")
	}

	req, _ := http.NewRequest(http.MethodPost, srvB.URL+"/api/peer/unpair", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Authorization", "Bearer "+keyAtoB)
	r2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	r2.Body.Close()
	if r2.StatusCode != http.StatusOK {
		t.Fatalf("authenticated unpair status %d", r2.StatusCode)
	}
	if _, err := eb.st.GetPeer(t.Context(), "laptop"); err == nil {
		t.Fatal("peer still exists after authenticated unpair")
	}
}

func TestPeersAcceptSendsHostHeader(t *testing.T) {
	ea, _, _, _, _, _ := pairPeers(t)
	p, err := ea.st.GetPeer(t.Context(), "builder")
	if err != nil {
		t.Fatal(err)
	}
	if p.HostHeader != "b.example" {
		t.Fatalf("A stored host_header %q, want b.example", p.HostHeader)
	}
}

func TestPeerStatusReportsRealLoad(t *testing.T) {
	_, _, _, srvB, keyAtoB, _ := pairPeers(t)
	resp, body := peerStatus(t, srvB, keyAtoB)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}
	var st struct {
		Protocol int `json:"protocol"`
		Load     struct {
			OneMinute float64 `json:"1m"`
			Cores     int     `json:"cores"`
			FreeGB    float64 `json:"free_gb"`
		} `json:"load"`
		Backends []map[string]any `json:"backends"`
	}
	if err := json.Unmarshal(body, &st); err != nil {
		t.Fatal(err)
	}
	if st.Protocol != 1 {
		t.Errorf("protocol = %d", st.Protocol)
	}
	if st.Load.Cores <= 0 {
		t.Errorf("cores = %d", st.Load.Cores)
	}
	if st.Load.FreeGB <= 0 {
		t.Errorf("free_gb = %v, want a real reading", st.Load.FreeGB)
	}
	if st.Backends == nil {
		t.Error("backends is null, want a list")
	}
}

func TestPeerInviteRejectsPathName(t *testing.T) {
	ea := newEnv(t, envOpt{})
	enablePeers(t, ea)
	srv := httptest.NewServer(ea.mux)
	defer srv.Close()
	for _, name := range []string{"../escape", "a/b", ".", ""} {
		r, err := postJSON(srv.URL+"/api/peers/invite", map[string]any{"name": name})
		if err != nil {
			t.Fatal(err)
		}
		r.Body.Close()
		if r.StatusCode != http.StatusBadRequest {
			t.Errorf("name %q: status %d, want 400", name, r.StatusCode)
		}
	}
}

func TestPeerKeySinkMirrorsInbound(t *testing.T) {
	ea := newEnv(t, envOpt{})
	eb := newEnv(t, envOpt{})
	enablePeers(t, ea)
	enablePeers(t, eb)
	type call struct{ name, hash string }
	var aCalls, bCalls []call
	ea.api.deps.PeerKeySink = func(name, hash string) error { aCalls = append(aCalls, call{name, hash}); return nil }
	eb.api.deps.PeerKeySink = func(name, hash string) error { bCalls = append(bCalls, call{name, hash}); return nil }

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
	r, _ := postJSON(srvB.URL+"/api/peers/accept", map[string]any{"a_url": srvA.URL, "code": inv.Code, "name": "laptop", "b_url": srvB.URL})
	r.Body.Close()

	for who, calls := range map[string][]call{"A": aCalls, "B": bCalls} {
		if len(calls) != 1 {
			t.Fatalf("%s sink calls = %d, want 1", who, len(calls))
		}
		if calls[0].name == "" || !bytes.HasPrefix([]byte(calls[0].hash), []byte("sha256:")) {
			t.Errorf("%s sink got name=%q hash=%q", who, calls[0].name, calls[0].hash)
		}
	}
}

func TestNoKeyInAnyPeerOutput(t *testing.T) {
	ea, eb, srvA, srvB, keyAtoB, keyBtoA := pairPeers(t)
	keys := [][]byte{[]byte(keyAtoB), []byte(keyBtoA)}

	check := func(label string, body []byte, allow ...[]byte) {
		for _, k := range keys {
			leak := true
			for _, a := range allow {
				if bytes.Equal(k, a) {
					leak = false
				}
			}
			if leak && bytes.Contains(body, k) {
				t.Errorf("%s leaks a key", label)
			}
		}
	}

	respL, _ := http.Get(srvA.URL + "/api/peers")
	check("peers list", readAll(respL))
	respL.Body.Close()
	_, status := peerStatus(t, srvB, keyAtoB)
	check("peer status", status)

	r, _ := postJSON(srvA.URL+"/api/peers/builder/rotate", map[string]any{})
	check("rotate", readAll(r))
	r.Body.Close()

	// Revoke's best-effort unpair body comes from A; B's delete response is
	// empty of keys as well.
	req, _ := http.NewRequest(http.MethodDelete, srvA.URL+"/api/peers/builder", nil)
	rd, _ := http.DefaultClient.Do(req)
	check("revoke", readAll(rd))
	rd.Body.Close()

	// The pairing exchange may hand B its key exactly once: the response must
	// carry no other key. The handshake already completed, so this only checks
	// the local stores did not echo anything.
	p, _ := ea.st.GetPeer(t.Context(), "builder")
	if bytes.Contains([]byte(p.LastError), []byte(keyAtoB)) {
		t.Error("peer row leaks a key")
	}
	_ = eb
}

func TestPeerPollerUpdatesLastSeen(t *testing.T) {
	ea, _, _, _, _, _ := pairPeers(t)
	ea.api.PollPeersOnce(t.Context())
	p, err := ea.st.GetPeer(t.Context(), "builder")
	if err != nil {
		t.Fatal(err)
	}
	if p.LastSeen == 0 || p.LastOK == 0 {
		t.Fatalf("poller did not update peer: last_seen=%d last_ok=%d err=%q", p.LastSeen, p.LastOK, p.LastError)
	}
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

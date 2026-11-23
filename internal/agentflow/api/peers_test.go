// Copyright 2026 FreeSociety3 contributors
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"log"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"cmd184psu/unified-webapp/internal/agentflow/store"
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

// AF-24 row 1: B keeps A's own Host header, so B can reach A through a proxy
// instead of sending its own authority.
func TestPeersAcceptStoresPeerHostHeader(t *testing.T) {
	_, eb, srvA, srvB, _, _ := pairPeers(t)
	p, err := eb.st.GetPeer(t.Context(), "laptop")
	if err != nil {
		t.Fatal(err)
	}
	want := hostOf(srvA.URL)
	if p.HostHeader != want {
		t.Fatalf("B stored host_header %q for A, want %q", p.HostHeader, want)
	}
	if p.HostHeader == hostOf(srvB.URL) {
		t.Fatal("B stored its own host header for A")
	}
}

// AF-24 row 2: darwin's vm.loadavg is binary fixed point, not text.
func TestParseLoadavg(t *testing.T) {
	raw := make([]byte, 24)
	binary.LittleEndian.PutUint32(raw[0:], 1200)
	binary.LittleEndian.PutUint32(raw[4:], 600)
	binary.LittleEndian.PutUint32(raw[8:], 300)
	binary.LittleEndian.PutUint64(raw[16:], 2048)
	if got := parseLoadavg(raw); math.Abs(got-1200.0/2048.0) > 1e-9 {
		t.Fatalf("parseLoadavg = %v, want %v", got, 1200.0/2048.0)
	}
	if got := parseLoadavg(raw[:12]); got != 0 {
		t.Fatalf("short buffer = %v, want 0", got)
	}
	zero := make([]byte, 24)
	binary.LittleEndian.PutUint32(zero[0:], 500)
	if got := parseLoadavg(zero); got != 0 {
		t.Fatalf("zero scale = %v, want 0", got)
	}
}

// AF-24 row 3: a project with no open effort must not empty the whole list.
func TestPeerStatusListsProjectWithoutEffort(t *testing.T) {
	_, eb, _, srvB, keyAtoB, _ := pairPeers(t)
	eb.addProject("solo")
	resp, body := peerStatus(t, srvB, keyAtoB)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}
	var st struct {
		Projects []struct {
			Slug         string `json:"slug"`
			OpenEfforts  int    `json:"open_efforts"`
			RunningStage int    `json:"running_stages"`
		} `json:"projects"`
		Delegable []string `json:"delegable"`
	}
	if err := json.Unmarshal(body, &st); err != nil {
		t.Fatal(err)
	}
	if len(st.Projects) != 1 || st.Projects[0].Slug != "solo" {
		t.Fatalf("projects = %+v, want the solo row", st.Projects)
	}
	if len(st.Delegable) != 1 || st.Delegable[0] != "solo" {
		t.Fatalf("delegable = %v, want [solo]", st.Delegable)
	}
}

// AF-24 rows 4 and 10: the status carries this node's build and schema, a
// backend with nothing resting still has a list, the poller stores both and
// the last status stays in memory for GET /api/peers.
func TestPeerStatusReportsBuildSchemaAndRestList(t *testing.T) {
	ea, eb, srvA, srvB, keyAtoB, _ := pairPeers(t)
	if err := eb.st.CreateBackend(t.Context(), "t", store.Backend{Name: "gpu", Host: "http://127.0.0.1:1", APIType: "ollama", Slots: 1}); err != nil {
		t.Fatal(err)
	}
	resp, body := peerStatus(t, srvB, keyAtoB)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}
	var st struct {
		Build    string `json:"build"`
		Schema   int    `json:"schema"`
		Node     string `json:"node"`
		Backends []struct {
			Name      string  `json:"name"`
			RestUntil []int64 `json:"rest_until"`
		} `json:"backends"`
	}
	if err := json.Unmarshal(body, &st); err != nil {
		t.Fatal(err)
	}
	if st.Build == "" {
		t.Error("build is empty")
	}
	if st.Schema <= 0 {
		t.Errorf("schema = %d, want this node's schema version", st.Schema)
	}
	if st.Node == "" {
		t.Error("node name is empty")
	}
	if len(st.Backends) != 1 {
		t.Fatalf("backends = %+v", st.Backends)
	}
	if st.Backends[0].RestUntil == nil {
		t.Error("rest_until is null, want [] for a backend with nothing resting")
	}

	ea.api.PollPeersOnce(t.Context())
	p, err := ea.st.GetPeer(t.Context(), "builder")
	if err != nil {
		t.Fatal(err)
	}
	if p.RemoteSchema <= 0 || p.RemoteBuild == "" {
		t.Fatalf("poller stored build=%q schema=%d, want both", p.RemoteBuild, p.RemoteSchema)
	}
	respL, _ := http.Get(srvA.URL + "/api/peers")
	var list struct {
		Peers []struct {
			Name        string          `json:"name"`
			RemoteBuild string          `json:"remote_build"`
			RemoteSch   int             `json:"remote_schema"`
			Status      json.RawMessage `json:"status"`
		} `json:"peers"`
	}
	b := readAll(respL)
	respL.Body.Close()
	if err := json.Unmarshal(b, &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Peers) != 1 || len(list.Peers[0].Status) == 0 {
		t.Fatalf("peers list = %s, want the last status in memory", b)
	}
}

// AF-24 row 11: /api/peer/status obeys the master switch.
func TestPeerStatusGatedWhenPeersDisabled(t *testing.T) {
	_, eb, _, srvB, keyAtoB, _ := pairPeers(t)
	if err := eb.st.UpdateSettings(t.Context(), "t", map[string]string{"peers_enabled": "off"}); err != nil {
		t.Fatal(err)
	}
	resp, body := peerStatus(t, srvB, keyAtoB)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status with peers off = %d (%s), want 404", resp.StatusCode, body)
	}
}

// AF-24 row 4: accepting is real, not a constant: a held node takes no work.
func TestPeerStatusAcceptingFollowsHold(t *testing.T) {
	_, eb, _, srvB, keyAtoB, _ := pairPeers(t)
	_, body := peerStatus(t, srvB, keyAtoB)
	if !bytes.Contains(body, []byte(`"accepting":true`)) {
		t.Fatalf("status = %s, want accepting true", body)
	}
	if _, err := eb.st.HoldService(t.Context(), "t", "test", []string{}, false); err != nil {
		t.Fatal(err)
	}
	_, body = peerStatus(t, srvB, keyAtoB)
	if !bytes.Contains(body, []byte(`"accepting":false`)) {
		t.Fatalf("held node status = %s, want accepting false", body)
	}
}

// The pairing code is only burned once the target name is accepted, so a
// refused pairing leaves the code usable.
func TestPeerPairDoesNotBurnCodeWhenNameTaken(t *testing.T) {
	ea := newEnv(t, envOpt{})
	enablePeers(t, ea)
	srvA := httptest.NewServer(ea.mux)
	defer srvA.Close()
	resp, _ := postJSON(srvA.URL+"/api/peers/invite", map[string]any{"name": "builder"})
	var inv struct {
		Code string `json:"code"`
	}
	json.NewDecoder(resp.Body).Decode(&inv)
	resp.Body.Close()

	if err := ea.st.UpsertPeer(t.Context(), store.Peer{Name: "builder", BaseURL: "http://127.0.0.1:1", Enabled: true, Protocol: 1, Delegable: []string{}}); err != nil {
		t.Fatal(err)
	}
	body := map[string]any{"code": inv.Code, "node_name": "laptop", "base_url": "http://127.0.0.1:2", "key": "K", "protocol": 1, "build": "test"}
	r, _ := postJSON(srvA.URL+"/api/peer/pair", body)
	r.Body.Close()
	if r.StatusCode != http.StatusForbidden {
		t.Fatalf("pair with a taken name = %d, want 403", r.StatusCode)
	}

	if err := ea.st.DeletePeer(t.Context(), "builder"); err != nil {
		t.Fatal(err)
	}
	r2, _ := postJSON(srvA.URL+"/api/peer/pair", body)
	r2.Body.Close()
	if r2.StatusCode != http.StatusOK {
		t.Fatalf("code was burned by the refused pairing: status %d", r2.StatusCode)
	}
	r3, _ := postJSON(srvA.URL+"/api/peer/pair", body)
	r3.Body.Close()
	if r3.StatusCode == http.StatusOK {
		t.Fatal("the code paired twice")
	}
}

// AF-24: behind a TLS-terminating proxy the invite URL must be https.
func TestPeersInviteUsesForwardedProto(t *testing.T) {
	ea := newEnv(t, envOpt{})
	enablePeers(t, ea)
	srv := httptest.NewServer(ea.mux)
	defer srv.Close()
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/peers/invite", strings.NewReader(`{"name":"builder"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Host", "laptop.example")
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var inv struct {
		AURL string `json:"a_url"`
	}
	json.NewDecoder(r.Body).Decode(&inv)
	r.Body.Close()
	if inv.AURL != "https://laptop.example" {
		t.Fatalf("a_url = %q, want https://laptop.example", inv.AURL)
	}
}

// AF-24 row 7: the lockout is per host, not per connection.
func TestClientIPIgnoresPort(t *testing.T) {
	r1 := &http.Request{RemoteAddr: "198.51.100.7:41000"}
	r2 := &http.Request{RemoteAddr: "198.51.100.7:41001"}
	if clientIP(r1) != "198.51.100.7" || clientIP(r2) != "198.51.100.7" {
		t.Fatalf("clientIP = %q, %q", clientIP(r1), clientIP(r2))
	}
	peerLimiter.reset(clientIP(r1))
	defer peerLimiter.reset(clientIP(r1))
	for i := 0; i < 5; i++ {
		peerLimiter.recordFailure(clientIP(r1))
	}
	if peerLimiter.allow(clientIP(r2)) {
		t.Fatal("five failures on one port did not lock out the host")
	}
}

// AF-24 row 6: the poll wait is jittered, so paired nodes drift apart.
func TestPollWaitJitter(t *testing.T) {
	seen := map[time.Duration]bool{}
	for i := 0; i < 50; i++ {
		d := pollWait(60)
		if d < 48*time.Second || d > 72*time.Second {
			t.Fatalf("pollWait(60) = %v, want 48s..72s", d)
		}
		seen[d] = true
	}
	if len(seen) < 2 {
		t.Fatal("pollWait never varies")
	}
}

// AF-24 row 8: a rotation the peer refuses leaves this side exactly as it was.
func TestPeerRotateRollsBackWhenPeerUnreachable(t *testing.T) {
	ea, _, srvA, srvB, keyAtoB, keyBtoA := pairPeers(t)
	oldHash, err := ea.st.InboundKeyHash(t.Context(), "builder")
	if err != nil {
		t.Fatal(err)
	}
	srvB.Close()
	r, _ := postJSON(srvA.URL+"/api/peers/builder/rotate", map[string]any{})
	r.Body.Close()
	if r.StatusCode != http.StatusBadGateway {
		t.Fatalf("rotate with an unreachable peer = %d, want 502", r.StatusCode)
	}
	if got, err := ea.api.readPeerKey("builder"); err != nil || got != keyAtoB {
		t.Fatalf("outbound key after failed rotate = %q (err %v), want the old one", got, err)
	}
	hash, err := ea.st.InboundKeyHash(t.Context(), "builder")
	if err != nil || hash != oldHash {
		t.Fatalf("inbound hash after failed rotate changed (err %v)", err)
	}
	// The old keys still work: the peer never swapped, so B still presents K_A.
	srvA.Close()
	srvA = httptest.NewServer(ea.mux)
	defer srvA.Close()
	if resp, body := peerStatus(t, srvA, keyBtoA); resp.StatusCode != http.StatusOK {
		t.Fatalf("old A inbound key after failed rotate: %d %s", resp.StatusCode, body)
	}
}

// AF-24 row 12: a peer that has never answered counts as unreachable from
// pairing time instead of staying silent for ever.
func TestPeerAttentionFlagsNeverReachedPeer(t *testing.T) {
	e := newEnv(t, envOpt{})
	now := time.Now().Unix()
	if err := e.st.UpsertPeer(t.Context(), store.Peer{Name: "ghost", BaseURL: "http://127.0.0.1:1", Enabled: true, Protocol: 1, PairedAt: now - 1200, Delegable: []string{}}); err != nil {
		t.Fatal(err)
	}
	if err := e.st.UpsertPeer(t.Context(), store.Peer{Name: "fresh", BaseURL: "http://127.0.0.1:1", Enabled: true, Protocol: 1, PairedAt: now - 30, Delegable: []string{}}); err != nil {
		t.Fatal(err)
	}
	items, err := e.eng.Attention(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, it := range items {
		if it.Kind == "peer_unreachable" {
			seen[it.Message] = true
		}
	}
	if !seen["peer ghost unreachable"] {
		t.Fatalf("no attention item for a peer that never answered: %+v", items)
	}
	if seen["peer fresh unreachable"] {
		t.Fatal("a peer paired 30 s ago is already flagged")
	}
}

// AF-24 item 6 / row 9: no key reaches a log line, an event, an error field or
// any body the UI reads. The pairing exchange is the one documented exception.
func TestNoKeyInLogsEventsOrUIBound(t *testing.T) {
	var logs bytes.Buffer
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

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
	r, _ := postJSON(srvB.URL+"/api/peers/accept", map[string]any{"a_url": srvA.URL, "code": inv.Code, "name": "laptop", "b_url": srvB.URL})
	r.Body.Close()

	keys := [][]byte{}
	grab := func() {
		if k, err := ea.api.readPeerKey("builder"); err == nil {
			keys = append(keys, []byte(k))
		}
		if k, err := eb.api.readPeerKey("laptop"); err == nil {
			keys = append(keys, []byte(k))
		}
	}
	grab()

	bodies := map[string][]byte{}
	get := func(label, url string, key string) {
		req, _ := http.NewRequest(http.MethodGet, url, nil)
		if key != "" {
			req.Header.Set("Authorization", "Bearer "+key)
		}
		rr, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		bodies[label] = readAll(rr)
		rr.Body.Close()
	}
	get("A peers", srvA.URL+"/api/peers", "")
	get("B peers", srvB.URL+"/api/peers", "")
	get("A attention", srvA.URL+"/api/attention", "")
	get("A digest", srvA.URL+"/api/digest", "")
	get("B attention", srvB.URL+"/api/attention", "")
	get("B digest", srvB.URL+"/api/digest", "")
	get("B status", srvB.URL+"/api/peer/status", string(keys[len(keys)-2]))

	ea.api.PollPeersOnce(t.Context())
	if k, err := ea.api.readPeerKey("builder"); err == nil {
		keys = append(keys, []byte(k))
	}
	if k, err := eb.api.readPeerKey("laptop"); err == nil {
		keys = append(keys, []byte(k))
	}

	rr, _ := postJSON(srvA.URL+"/api/peers/builder/rotate", map[string]any{})
	bodies["rotate"] = readAll(rr)
	rr.Body.Close()
	grab()
	rt, _ := postJSON(srvA.URL+"/api/peers/builder/status", map[string]any{})
	bodies["test"] = readAll(rt)
	rt.Body.Close()

	req, _ := http.NewRequest(http.MethodDelete, srvA.URL+"/api/peers/builder", nil)
	rd, _ := http.DefaultClient.Do(req)
	bodies["revoke"] = readAll(rd)
	rd.Body.Close()

	for label, body := range bodies {
		for _, k := range keys {
			if bytes.Contains(body, k) {
				t.Errorf("%s response leaks a key", label)
			}
		}
		// The UI's only peer source is /api/peers; it must not even name a
		// key field, so a later change cannot start rendering one.
		if label == "A peers" || label == "B peers" {
			var v any
			if err := json.Unmarshal(body, &v); err == nil {
				var walk func(any)
				walk = func(x any) {
					switch m := x.(type) {
					case map[string]any:
						for k, val := range m {
							lk := strings.ToLower(k)
							if strings.Contains(lk, "key") {
								t.Errorf("%s exposes a %q field to the UI", label, k)
							}
							walk(val)
						}
					case []any:
						for _, val := range m {
							walk(val)
						}
					}
				}
				walk(v)
			}
		}
	}

	for _, k := range keys {
		if bytes.Contains(logs.Bytes(), k) {
			t.Error("a key was logged")
		}
	}
	for _, e := range []*env{ea, eb} {
		peers, err := e.st.ListPeers(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range peers {
			for _, k := range keys {
				if bytes.Contains([]byte(p.LastError), k) {
					t.Errorf("peer %q last_error leaks a key", p.Name)
				}
			}
		}
		evs, err := e.st.ListEvents(t.Context(), "", "", 500)
		if err != nil {
			t.Fatal(err)
		}
		for _, ev := range evs {
			blob := []byte(ev.Action + ev.DetailJSON + ev.Actor)
			for _, k := range keys {
				if bytes.Contains(blob, k) {
					t.Errorf("event %d (%s) leaks a key", ev.ID, ev.Action)
				}
			}
		}
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

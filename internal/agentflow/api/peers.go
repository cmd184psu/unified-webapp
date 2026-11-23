// Copyright 2026 FreeSociety3 contributors
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"cmd184psu/unified-webapp/internal/agentflow/engine"
	"cmd184psu/unified-webapp/internal/agentflow/model"
	"cmd184psu/unified-webapp/internal/agentflow/peerclient"
	"cmd184psu/unified-webapp/internal/agentflow/store"
)

type peerRateLimiter struct {
	mu       sync.Mutex
	failures map[string][]time.Time
}

var peerLimiter = &peerRateLimiter{failures: map[string][]time.Time{}}

func (r *peerRateLimiter) allow(ip string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	cutoff := now.Add(-10 * time.Minute)
	var recent []time.Time
	for _, t := range r.failures[ip] {
		if t.After(cutoff) {
			recent = append(recent, t)
		}
	}
	r.failures[ip] = recent
	if len(recent) >= 5 {
		return false
	}
	return true
}

func (r *peerRateLimiter) recordFailure(ip string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.failures[ip] = append(r.failures[ip], time.Now())
}

func (r *peerRateLimiter) reset(ip string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.failures, ip)
}

func (a *API) registerPeers(mux muxer) {
	// management (needs peers_enabled? allow listing even if off? require on)
	mux.HandleFunc("GET /api/peers", a.peersList)
	mux.HandleFunc("POST /api/peers/invite", a.peersInvite)
	mux.HandleFunc("POST /api/peers/accept", a.peersAccept)
	mux.HandleFunc("GET /api/peers/{name}/status", a.peerTest) // test connection
	mux.HandleFunc("POST /api/peers/{name}/rotate", a.peersRotate)
	mux.HandleFunc("DELETE /api/peers/{name}", a.peersRevoke)
	a.registerDelegations(mux)
	// unauthenticated peer routes
	mux.HandleFunc("POST /api/peer/pair", a.peerPair)
	mux.HandleFunc("POST /api/peer/unpair", a.peerUnpair)
	mux.HandleFunc("GET /api/peer/status", a.peerStatus)
}

func (a *API) peersEnabled() bool {
	if a.deps.Store == nil {
		return false
	}
	s, err := a.deps.Store.GetSettings(context.Background())
	if err != nil {
		return false
	}
	return s.PeersEnabled
}

func (a *API) nodeName() string {
	if a.deps.Store != nil {
		if s, err := a.deps.Store.GetSettings(context.Background()); err == nil && s.NodeName != "" {
			return s.NodeName
		}
	}
	h, _ := os.Hostname()
	return h
}

func (a *API) dataDir() string {
	if a.deps.DataDir != "" {
		return a.deps.DataDir
	}
	if a.deps.Store != nil {
		return a.deps.Store.DataDir()
	}
	return ""
}

func (a *API) peerKeyPath(name string) string {
	return filepath.Join(a.dataDir(), "peers", name+".key")
}

func (a *API) writePeerKey(name, key string) error {
	dir := filepath.Join(a.dataDir(), "peers")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(a.peerKeyPath(name), []byte(key), 0o600)
}

func (a *API) readPeerKey(name string) (string, error) {
	b, err := os.ReadFile(a.peerKeyPath(name))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

func (a *API) deletePeerKey(name string) {
	os.Remove(a.peerKeyPath(name))
}

func (a *API) peersList(w http.ResponseWriter, r *http.Request) {
	if !a.peersEnabled() {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "peers disabled"})
		return
	}
	peers, err := a.deps.Store.ListPeers(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	// never key material
	out := []map[string]any{}
	for _, p := range peers {
		out = append(out, map[string]any{
			"name":                 p.Name,
			"base_url":             p.BaseURL,
			"host_header":          p.HostHeader,
			"enabled":              p.Enabled,
			"protocol":             p.Protocol,
			"remote_build":         p.RemoteBuild,
			"paired_at":            p.PairedAt,
			"last_seen":            p.LastSeen,
			"last_ok":              p.LastOK,
			"last_error":           p.LastError,
			"insecure_skip_verify": p.InsecureSkipVerify,
			"delegable":            p.Delegable,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"peers": out})
}

func (a *API) peersInvite(w http.ResponseWriter, r *http.Request) {
	if !a.peersEnabled() {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "peers disabled"})
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Name) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name required"})
		return
	}
	name := strings.TrimSpace(req.Name)
	// name taken?
	if _, err := a.deps.Store.GetPeer(r.Context(), name); err == nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}
	// also inbound taken?
	if _, err := a.deps.Store.InboundKeyHash(r.Context(), name); err == nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}
	// generate code
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal"})
		return
	}
	code := base64.RawURLEncoding.EncodeToString(buf)
	hash := sha256Hex(code)
	expires := time.Now().Add(10 * time.Minute).Unix()
	if err := a.deps.Store.CreatePairingCode(r.Context(), name, hash, expires); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	aURL := scheme + "://" + r.Host
	writeJSON(w, http.StatusOK, map[string]any{"code": code, "a_url": aURL})
}

func (a *API) peersAccept(w http.ResponseWriter, r *http.Request) {
	if !a.peersEnabled() {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "peers disabled"})
		return
	}
	ip := r.RemoteAddr
	if !peerLimiter.allow(ip) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}
	var req struct {
		AURL string `json:"a_url"`
		Code string `json:"code"`
		Name string `json:"name"` // local name for A
		BURL string `json:"b_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad request"})
		return
	}
	if strings.TrimSpace(req.AURL) == "" || strings.TrimSpace(req.Code) == "" || strings.TrimSpace(req.Name) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing fields"})
		return
	}
	localName := strings.TrimSpace(req.Name)
	// name taken?
	if _, err := a.deps.Store.GetPeer(r.Context(), localName); err == nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		peerLimiter.recordFailure(ip)
		return
	}
	bURL := strings.TrimSpace(req.BURL)
	if bURL == "" {
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		bURL = scheme + "://" + r.Host
	}
	// mint K_B
	kb, err := genKey()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal"})
		return
	}
	// inbound key on B for A calling B = K_B
	if err := a.deps.Store.StoreInboundKey(r.Context(), localName, sha256Hex(kb)); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	// call A's pair endpoint
	client := &peerclient.Client{Timeout: 30 * time.Second, InsecureSkipVerify: false}
	var resp struct {
		Key string `json:"key"`
	}
	body := map[string]any{
		"code":      req.Code,
		"node_name": a.nodeName(),
		"base_url":  bURL,
		"key":       kb,
		"protocol":  1,
		"build":     "dev",
	}
	pairURL := strings.TrimRight(req.AURL, "/") + "/api/peer/pair"
	if err := client.Do(r.Context(), http.MethodPost, pairURL, "", "", body, &resp); err != nil {
		peerLimiter.recordFailure(ip)
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}
	if resp.Key == "" {
		peerLimiter.recordFailure(ip)
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}
	// store outbound key for A = resp.Key
	if err := a.writePeerKey(localName, resp.Key); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	// create peer row for A
	now := time.Now().Unix()
	peer := store.Peer{
		Name:      localName,
		BaseURL:   strings.TrimRight(req.AURL, "/"),
		Enabled:   true,
		Protocol:  1,
		PairedAt:  now,
		Delegable: []string{},
	}
	if err := a.deps.Store.UpsertPeer(r.Context(), peer); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	peerLimiter.reset(ip)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *API) peerPair(w http.ResponseWriter, r *http.Request) {
	if !a.peersEnabled() {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	ip := r.RemoteAddr
	if !peerLimiter.allow(ip) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}
	var req struct {
		Code     string `json:"code"`
		NodeName string `json:"node_name"`
		BaseURL  string `json:"base_url"`
		HostHdr  string `json:"host_header"`
		Key      string `json:"key"`
		Protocol int    `json:"protocol"`
		Build    string `json:"build"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad request"})
		return
	}
	if strings.TrimSpace(req.Code) == "" || strings.TrimSpace(req.Key) == "" || strings.TrimSpace(req.BaseURL) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing fields"})
		return
	}
	name, err := a.deps.Store.ConsumePairingCode(r.Context(), sha256Hex(req.Code))
	if err != nil {
		peerLimiter.recordFailure(ip)
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}
	// name taken? (peer exists)
	if _, err := a.deps.Store.GetPeer(r.Context(), name); err == nil {
		peerLimiter.recordFailure(ip)
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}
	// store outbound key = req.Key
	if err := a.writePeerKey(name, req.Key); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	// mint K_A
	ka, err := genKey()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal"})
		return
	}
	// store inbound hash for name
	if err := a.deps.Store.StoreInboundKey(r.Context(), name, sha256Hex(ka)); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	now := time.Now().Unix()
	peer := store.Peer{
		Name:               name,
		BaseURL:            strings.TrimRight(req.BaseURL, "/"),
		HostHeader:         req.HostHdr,
		Enabled:            true,
		Protocol:           req.Protocol,
		RemoteBuild:        req.Build,
		PairedAt:           now,
		InsecureSkipVerify: false,
		Delegable:          []string{},
	}
	if peer.Protocol == 0 {
		peer.Protocol = 1
	}
	if err := a.deps.Store.UpsertPeer(r.Context(), peer); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	peerLimiter.reset(ip)
	writeJSON(w, http.StatusOK, map[string]any{"key": ka, "protocol": 1})
}

func (a *API) peerUnpair(w http.ResponseWriter, r *http.Request) {
	var req struct {
		BaseURL string `json:"base_url"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	// find peer by base_url and remove
	if peers, err := a.deps.Store.ListPeers(r.Context()); err == nil {
		for _, pp := range peers {
			if strings.TrimRight(pp.BaseURL, "/") == strings.TrimRight(req.BaseURL, "/") {
				a.deletePeerKey(pp.Name)
				_ = a.deps.Store.DeletePeer(r.Context(), pp.Name)
				_ = a.deps.Store.DeleteInboundKey(r.Context(), pp.Name)
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *API) peersRevoke(w http.ResponseWriter, r *http.Request) {
	if !a.peersEnabled() {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	name := r.PathValue("name")
	if strings.TrimSpace(name) == "" {
		// fallback parse
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if len(parts) > 0 {
			name = parts[len(parts)-1]
		}
	}
	// tell other side best-effort
	if p, err := a.deps.Store.GetPeer(r.Context(), name); err == nil {
		client := &peerclient.Client{Timeout: 10 * time.Second, InsecureSkipVerify: p.InsecureSkipVerify}
		url := strings.TrimRight(p.BaseURL, "/") + "/api/peer/unpair"
		key, _ := a.readPeerKey(name)
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		body := map[string]any{"base_url": scheme + "://" + r.Host}
		_ = client.Do(r.Context(), http.MethodPost, url, key, p.HostHeader, body, nil)
	}
	// remove local
	a.deletePeerKey(name)
	_ = a.deps.Store.DeletePeer(r.Context(), name)
	_ = a.deps.Store.DeleteInboundKey(r.Context(), name)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *API) peersRotate(w http.ResponseWriter, r *http.Request) {
	if !a.peersEnabled() {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	name := r.PathValue("name")
	if name == "" {
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if len(parts) > 0 {
			name = parts[len(parts)-1]
		}
	}
	// mint new key each way: revoke + re-pair not possible; instead rotate outbound/inbound locally and notify?
	// Simplified: generate new inbound key, store; old stops working. Outbound unchanged (remote rotates its own).
	newKey, err := genKey()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal"})
		return
	}
	_ = a.deps.Store.StoreInboundKey(r.Context(), name, sha256Hex(newKey))
	writeJSON(w, http.StatusOK, map[string]any{"key": newKey})
}

func (a *API) peerTest(w http.ResponseWriter, r *http.Request) {
	if !a.peersEnabled() {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	name := r.PathValue("name")
	if name == "" {
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if len(parts) >= 2 {
			name = parts[len(parts)-2]
		}
	}
	p, err := a.deps.Store.GetPeer(r.Context(), name)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	key, _ := a.readPeerKey(name)
	client := &peerclient.Client{Timeout: 10 * time.Second, InsecureSkipVerify: p.InsecureSkipVerify}
	var st peerclient.PeerStatus
	url := strings.TrimRight(p.BaseURL, "/") + "/api/peer/status"
	if err := client.Do(r.Context(), http.MethodGet, url, key, p.HostHeader, nil, &st); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": st})
}

func (a *API) peerStatus(w http.ResponseWriter, r *http.Request) {
	// needs peer key; validate via inbound table OR platform gate. We validate here.
	presented := extractBearer(r)
	peerName, ok := a.deps.Store.ValidateInboundKey(r.Context(), presented)
	if !ok {
		// maybe platform gate already validated; if no inbound table, accept any? Require ok.
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	_ = peerName
	// gather status
	m := engine.GateMetrics{}
	if a.deps.Engine != nil {
		m = a.deps.Engine.GateMetrics()
	}
	st := map[string]any{
		"protocol": 1,
		"node":     a.nodeName(),
		"build":    "dev",
		"schema":   schemaVersion(a.deps.Store),
		"time":     time.Now().Unix(),
		"accepting": func() bool {
			if a.deps.Engine != nil {
				return a.deps.Engine.LoadGateOK(r.Context())
			}
			return true
		}(),
		"load":       map[string]any{"1m": m.Load1m, "cores": m.Cores, "free_gb": m.FreeDiskGB, "free_mem_gb": m.FreeMemGB},
		"backends":   []map[string]any{},
		"projects":   []map[string]any{},
		"delegable":  []string{},
		"rest_until": map[string]int64{},
	}
	// backends (rest_until shared so a peer's 429s propagate)
	rests := map[string][]map[string]any{}
	restMap := map[string]int64{}
	if rs, err := a.deps.Store.ModelRests(r.Context()); err == nil {
		for _, r := range rs {
			rests[r.Backend] = append(rests[r.Backend], map[string]any{"model": r.Model, "until": r.Until})
			if r.Until > time.Now().UnixMilli() {
				restMap[r.Backend+"\x00"+r.Model] = r.Until
			}
		}
	}
	st["rest_until"] = restMap
	stages, _ := a.deps.Store.OpenStages(r.Context())
	perBackend := map[string]int{}
	perProject := map[int64]int{}
	for _, st := range stages {
		perBackend[st.Backend]++
		perProject[st.Effort]++
	}
	if bs, err := a.deps.Store.ListBackends(r.Context()); err == nil {
		blist := []map[string]any{}
		for _, b := range bs {
			ru := rests[b.Name]
			if ru == nil {
				ru = []map[string]any{}
			}
			blist = append(blist, map[string]any{
				"name":       b.Name,
				"slots":      b.Slots,
				"running":    perBackend[b.Name],
				"rest_until": ru,
			})
		}
		st["backends"] = blist
	}
	// projects
	if ps, err := a.deps.Store.ListProjects(r.Context()); err == nil {
		plist := []map[string]any{}
		delegable := []string{}
		for _, p := range ps {
			state, running, open := "stopped", 0, 0
			if a.deps.Engine != nil {
				state = a.deps.Engine.Runs().State(p.Slug)
			}
			if efs, err := a.deps.Store.ListEfforts(r.Context(), p.Slug); err == nil {
				for _, ef := range efs {
					running += perProject[ef.ID]
					if ef.Status != model.EffortComplete && ef.Status != model.EffortAbandoned {
						open++
					}
				}
			}
			plist = append(plist, map[string]any{
				"slug":           p.Slug,
				"run_state":      state,
				"running_stages": running,
				"open_efforts":   open,
			})
			delegable = append(delegable, p.Slug)
		}
		st["projects"] = plist
		st["delegable"] = delegable
	}
	writeJSON(w, http.StatusOK, st)
}

func extractBearer(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if strings.HasPrefix(auth, prefix) {
		return strings.TrimPrefix(auth, prefix)
	}
	return r.Header.Get("X-API-Key")
}

func genKey() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func schemaVersion(s *store.Store) int {
	if s == nil {
		return 0
	}
	// store doesn't expose; query
	var v int
	_ = s.DB().QueryRow(`SELECT COALESCE(MAX(version),0) FROM schema_version`).Scan(&v)
	return v
}

func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func freeGB() float64 {
	return 0
}

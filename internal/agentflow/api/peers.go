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
	mrand "math/rand"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

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
	return len(recent) < 5
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

// clientIP is the address the lockout accumulates on. RemoteAddr carries a
// fresh port on every connection, so keying on it would never lock anything
// out; the host part is stable for a node.
func clientIP(r *http.Request) string {
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

// requestBaseURL is how this node is reachable as the caller sees it. Behind a
// TLS-terminating proxy r.TLS is nil, so X-Forwarded-Proto decides the scheme
// and X-Forwarded-Host the authority.
func requestBaseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if p := firstHeaderValue(r.Header.Get("X-Forwarded-Proto")); p == "http" || p == "https" {
		scheme = p
	}
	host := r.Host
	if h := firstHeaderValue(r.Header.Get("X-Forwarded-Host")); h != "" {
		host = h
	}
	return scheme + "://" + host
}

func firstHeaderValue(v string) string {
	return strings.TrimSpace(strings.Split(v, ",")[0])
}

func hostOf(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return u.Host
}

// peerNameRE is the only accepted peer name: it becomes a file name and a
// config key suffix, so a separator or a dot-only name never gets through.
var peerNameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

func validPeerName(name string) bool { return peerNameRE.MatchString(name) }

// protocolVersion is the only peer protocol this build speaks.
const protocolVersion = 1

// BuildTime identifies this node's build in /api/peer/status. The Makefile
// stamps it with -ldflags -X; a go run/test build keeps the "dev" default.
var BuildTime = "dev"

func (a *API) registerPeers(mux muxer) {
	// Management (platform gate, module agentflow).
	mux.HandleFunc("GET /api/peers", a.peersList)
	mux.HandleFunc("POST /api/peers/invite", a.peersInvite)
	mux.HandleFunc("POST /api/peers/accept", a.peersAccept)
	mux.HandleFunc("GET /api/peers/{name}/status", a.peerTest)
	mux.HandleFunc("POST /api/peers/{name}/rotate", a.peersRotate)
	mux.HandleFunc("DELETE /api/peers/{name}", a.peersRevoke)
	// Peer routes: pair is the only one allow-listed at the gate; unpair,
	// rotate and status require the presenting peer's inbound key, which is
	// also mirrored into auth.api_keys so the gate accepts it.
	mux.HandleFunc("POST /api/peer/pair", a.peerPair)
	mux.HandleFunc("POST /api/peer/unpair", a.peerUnpair)
	mux.HandleFunc("POST /api/peer/rotate", a.peerRotate)
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
	// validPeerName already rules out separators; Base is belt and braces.
	return filepath.Join(a.dataDir(), "peers", filepath.Base(name)+".key")
}

func (a *API) writePeerKey(name, key string) error {
	if !validPeerName(name) {
		return os.ErrInvalid
	}
	dir := filepath.Join(a.dataDir(), "peers")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	path := a.peerKeyPath(name)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(key); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
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

// mirrorPeerKey records an inbound key hash in auth.api_keys so the platform
// gate accepts it. The local peer_inbound table remains the authority; a nil
// sink (tests, an unprotected deployment) just skips the mirror.
func (a *API) mirrorPeerKey(name, key string) error {
	if a.deps.PeerKeySink == nil {
		return nil
	}
	return a.deps.PeerKeySink(name, "sha256:"+sha256Hex(key))
}

func (a *API) forgetPeerKey(name string) {
	if a.deps.PeerKeyDelete != nil {
		_ = a.deps.PeerKeyDelete(name)
	}
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
	// Never key material.
	out := []map[string]any{}
	for _, p := range peers {
		row := map[string]any{
			"name":                 p.Name,
			"base_url":             p.BaseURL,
			"host_header":          p.HostHeader,
			"enabled":              p.Enabled,
			"protocol":             p.Protocol,
			"remote_build":         p.RemoteBuild,
			"remote_schema":        p.RemoteSchema,
			"paired_at":            p.PairedAt,
			"last_seen":            p.LastSeen,
			"last_ok":              p.LastOK,
			"last_error":           p.LastError,
			"insecure_skip_verify": p.InsecureSkipVerify,
			"delegable":            p.Delegable,
		}
		if st, ok := a.lastPeerStatus(p.Name); ok {
			row["status"] = st
		}
		out = append(out, row)
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
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name required"})
		return
	}
	name := strings.TrimSpace(req.Name)
	if !validPeerName(name) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid name"})
		return
	}
	if _, err := a.deps.Store.GetPeer(r.Context(), name); err == nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}
	if _, err := a.deps.Store.InboundKeyHash(r.Context(), name); err == nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal"})
		return
	}
	code := base64.RawURLEncoding.EncodeToString(buf)
	expires := time.Now().Add(10 * time.Minute).Unix()
	if err := a.deps.Store.CreatePairingCode(r.Context(), name, sha256Hex(code), expires); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	aURL := requestBaseURL(r)
	writeJSON(w, http.StatusOK, map[string]any{"code": code, "a_url": aURL})
}

func (a *API) peersAccept(w http.ResponseWriter, r *http.Request) {
	if !a.peersEnabled() {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "peers disabled"})
		return
	}
	ip := clientIP(r)
	if !peerLimiter.allow(ip) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}
	var req struct {
		AURL     string `json:"a_url"`
		Code     string `json:"code"`
		Name     string `json:"name"`
		BURL     string `json:"b_url"`
		HostHdr  string `json:"host_header"`
		Protocol int    `json:"protocol"`
		Build    string `json:"build"`
		NodeName string `json:"node_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad request"})
		return
	}
	localName := strings.TrimSpace(req.Name)
	if strings.TrimSpace(req.AURL) == "" || strings.TrimSpace(req.Code) == "" || !validPeerName(localName) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing or invalid fields"})
		return
	}
	if _, err := a.deps.Store.GetPeer(r.Context(), localName); err == nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		peerLimiter.recordFailure(ip)
		return
	}
	bURL := strings.TrimSpace(req.BURL)
	if bURL == "" {
		bURL = requestBaseURL(r)
	}
	// B's inbound key: A presents K_B when it calls B.
	kb, err := genKey()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal"})
		return
	}
	if err := a.deps.Store.StoreInboundKey(r.Context(), localName, sha256Hex(kb)); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	cleanup := func() {
		_ = a.deps.Store.DeleteInboundKey(r.Context(), localName)
		a.forgetPeerKey(localName)
	}
	if err := a.mirrorPeerKey(localName, kb); err != nil {
		cleanup()
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	hostHeader := strings.TrimSpace(req.HostHdr)
	if hostHeader == "" {
		hostHeader = r.Host
	}
	protocol := req.Protocol
	if protocol == 0 {
		protocol = protocolVersion
	}
	build := req.Build
	if build == "" {
		build = BuildTime
	}
	// A stores B's host header from this body; it was omitted before, so A
	// could never reach B through a proxy.
	client := &peerclient.Client{Peer: "pair:" + localName, Timeout: 30 * time.Second}
	var resp struct {
		Key        string `json:"key"`
		HostHeader string `json:"host_header"`
	}
	body := map[string]any{
		"code":        req.Code,
		"node_name":   a.nodeName(),
		"base_url":    bURL,
		"host_header": hostHeader,
		"key":         kb,
		"protocol":    protocol,
		"build":       build,
	}
	pairURL := strings.TrimRight(req.AURL, "/") + "/api/peer/pair"
	if err := client.Do(r.Context(), http.MethodPost, pairURL, "", "", body, &resp); err != nil {
		cleanup()
		peerLimiter.recordFailure(ip)
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}
	if resp.Key == "" {
		cleanup()
		peerLimiter.recordFailure(ip)
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}
	if err := a.writePeerKey(localName, resp.Key); err != nil {
		cleanup()
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	// The row for A uses the Host header A reported: B's own header is useless
	// for reaching A through a proxy.
	aHostHeader := strings.TrimSpace(resp.HostHeader)
	if aHostHeader == "" {
		aHostHeader = hostOf(req.AURL)
	}
	now := time.Now().Unix()
	peer := store.Peer{
		Name:        localName,
		BaseURL:     strings.TrimRight(req.AURL, "/"),
		HostHeader:  aHostHeader,
		Enabled:     true,
		Protocol:    protocol,
		RemoteBuild: build,
		PairedAt:    now,
		Delegable:   []string{},
	}
	if err := a.deps.Store.UpsertPeer(r.Context(), peer); err != nil {
		cleanup()
		a.deletePeerKey(localName)
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
	ip := clientIP(r)
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
	// Validate the code and the target name before burning the code, so a
	// refused pairing does not use it up; ConsumePairingCode still burns it
	// atomically, so a race between two pairs leaves one winner.
	name, err := a.deps.Store.CheckPairingCode(r.Context(), sha256Hex(req.Code))
	if err != nil {
		peerLimiter.recordFailure(ip)
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}
	if !validPeerName(name) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}
	if _, err := a.deps.Store.GetPeer(r.Context(), name); err == nil {
		peerLimiter.recordFailure(ip)
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}
	if name, err = a.deps.Store.ConsumePairingCode(r.Context(), sha256Hex(req.Code)); err != nil {
		peerLimiter.recordFailure(ip)
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}
	if err := a.writePeerKey(name, req.Key); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	ka, err := genKey()
	if err != nil {
		a.deletePeerKey(name)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal"})
		return
	}
	if err := a.deps.Store.StoreInboundKey(r.Context(), name, sha256Hex(ka)); err != nil {
		a.deletePeerKey(name)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if err := a.mirrorPeerKey(name, ka); err != nil {
		a.deletePeerKey(name)
		_ = a.deps.Store.DeleteInboundKey(r.Context(), name)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	now := time.Now().Unix()
	protocol := req.Protocol
	if protocol == 0 {
		protocol = protocolVersion
	}
	peer := store.Peer{
		Name:        name,
		BaseURL:     strings.TrimRight(req.BaseURL, "/"),
		HostHeader:  req.HostHdr,
		Enabled:     true,
		Protocol:    protocol,
		RemoteBuild: req.Build,
		PairedAt:    now,
		Delegable:   []string{},
	}
	if err := a.deps.Store.UpsertPeer(r.Context(), peer); err != nil {
		a.deletePeerKey(name)
		_ = a.deps.Store.DeleteInboundKey(r.Context(), name)
		a.forgetPeerKey(name)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	peerLimiter.reset(ip)
	// The pairing exchange is the one place a key travels in a response: B
	// needs K_A to call A. It never appears anywhere else. host_header is A's
	// own authority so B can reach it through a proxy.
	writeJSON(w, http.StatusOK, map[string]any{"key": ka, "protocol": protocolVersion, "host_header": r.Host})
}

// peerUnpair is authenticated by the presenting peer key; it never matches by
// base_url (an unauthenticated caller could otherwise remove a peer).
func (a *API) peerUnpair(w http.ResponseWriter, r *http.Request) {
	if !a.peersEnabled() {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	presented := extractBearer(r)
	name, ok := a.deps.Store.ValidateInboundKey(r.Context(), presented)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	a.deletePeerKey(name)
	_ = a.deps.Store.DeletePeer(r.Context(), name)
	_ = a.deps.Store.DeleteInboundKey(r.Context(), name)
	a.forgetPeerKey(name)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// peerRotate swaps both halves with the peer. The caller (the peer) presents
// its current outbound key; this side stores the new inbound hash for that key
// and the new outbound key it should use for the caller. No key is echoed.
func (a *API) peerRotate(w http.ResponseWriter, r *http.Request) {
	if !a.peersEnabled() {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	name, ok := a.deps.Store.ValidateInboundKey(r.Context(), extractBearer(r))
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	var req struct {
		Inbound  string `json:"inbound_key"`
		Outbound string `json:"outbound_key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad request"})
		return
	}
	if strings.TrimSpace(req.Inbound) == "" || strings.TrimSpace(req.Outbound) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing fields"})
		return
	}
	oldHash, oldErr := a.deps.Store.InboundKeyHash(r.Context(), name)
	hadOld := oldErr == nil
	oldOut, _ := a.readPeerKey(name)
	rollback := func() { a.restoreInboundKey(name, oldHash, hadOld) }
	// Mirror the new inbound key before the row that trusts it, so the gate
	// and peer_inbound never disagree about which key is current.
	if err := a.mirrorPeerKey(name, req.Inbound); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if err := a.deps.Store.StoreInboundKey(r.Context(), name, sha256Hex(req.Inbound)); err != nil {
		rollback()
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if err := a.writePeerKey(name, req.Outbound); err != nil {
		rollback()
		if oldOut != "" {
			_ = a.writePeerKey(name, oldOut)
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *API) peersRevoke(w http.ResponseWriter, r *http.Request) {
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
	if !validPeerName(name) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	// Tell the other side best-effort. Its unpair validates the key we send.
	if p, err := a.deps.Store.GetPeer(r.Context(), name); err == nil {
		client := &peerclient.Client{Peer: name, Timeout: 10 * time.Second, InsecureSkipVerify: p.InsecureSkipVerify}
		url := strings.TrimRight(p.BaseURL, "/") + "/api/peer/unpair"
		key, _ := a.readPeerKey(name)
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		body := map[string]any{"base_url": scheme + "://" + r.Host}
		_ = client.Do(r.Context(), http.MethodPost, url, key, p.HostHeader, body, nil)
	}
	a.deletePeerKey(name)
	_ = a.deps.Store.DeletePeer(r.Context(), name)
	_ = a.deps.Store.DeleteInboundKey(r.Context(), name)
	a.forgetPeerKey(name)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// peersRotate is the management rotate: both halves change, the peer is
// notified, and the response carries no key.
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
	if !validPeerName(name) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	p, err := a.deps.Store.GetPeer(r.Context(), name)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	oldOut, err := a.readPeerKey(name)
	if err != nil || oldOut == "" {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "no outbound key for peer"})
		return
	}
	newOut, err := genKey()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal"})
		return
	}
	newIn, err := genKey()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal"})
		return
	}
	oldHash, oldErr := a.deps.Store.InboundKeyHash(r.Context(), name)
	hadOld := oldErr == nil
	rollback := func() {
		// Both halves come back exactly as they were; the peer never swapped.
		_ = a.writePeerKey(name, oldOut)
		a.restoreInboundKey(name, oldHash, hadOld)
	}
	// Swap this side first: oldOut still authenticates at the peer, so the
	// call below completes the pair; a failure rolls the local half back.
	if err := a.writePeerKey(name, newOut); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if err := a.deps.Store.StoreInboundKey(r.Context(), name, sha256Hex(newIn)); err != nil {
		rollback()
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if err := a.mirrorPeerKey(name, newIn); err != nil {
		rollback()
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	client := &peerclient.Client{Peer: name, Timeout: 15 * time.Second, InsecureSkipVerify: p.InsecureSkipVerify}
	url := strings.TrimRight(p.BaseURL, "/") + "/api/peer/rotate"
	body := map[string]any{"inbound_key": newOut, "outbound_key": newIn}
	if err := client.Do(r.Context(), http.MethodPost, url, oldOut, p.HostHeader, body, nil); err != nil {
		rollback()
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "peer did not accept the rotation: " + err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
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
	client := &peerclient.Client{Peer: name, Timeout: 10 * time.Second, InsecureSkipVerify: p.InsecureSkipVerify}
	var st peerclient.PeerStatus
	url := strings.TrimRight(p.BaseURL, "/") + "/api/peer/status"
	if err := client.Do(r.Context(), http.MethodGet, url, key, p.HostHeader, nil, &st); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if st.Protocol != protocolVersion {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":    false,
			"error": "peer speaks protocol " + strconv.Itoa(st.Protocol) + "; upgrade the older node",
		})
		return
	}
	a.rememberPeerStatus(name, st)
	_ = a.deps.Store.UpdatePeerStatus(r.Context(), name, time.Now().Unix(), time.Now().Unix(), "", st.Build, st.Schema, st.Protocol, st.Delegable)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": st})
}

// PollPeersOnce refreshes last_seen/last_ok/last_error for every enabled peer.
func (a *API) PollPeersOnce(ctx context.Context) {
	if !a.peersEnabled() {
		return
	}
	peers, err := a.deps.Store.ListPeers(ctx)
	if err != nil {
		return
	}
	for _, p := range peers {
		if !p.Enabled {
			continue
		}
		key, _ := a.readPeerKey(p.Name)
		if key == "" {
			continue
		}
		client := &peerclient.Client{Peer: p.Name, Timeout: 10 * time.Second, InsecureSkipVerify: p.InsecureSkipVerify}
		var st peerclient.PeerStatus
		url := strings.TrimRight(p.BaseURL, "/") + "/api/peer/status"
		now := time.Now().Unix()
		if err := client.Do(ctx, http.MethodGet, url, key, p.HostHeader, nil, &st); err != nil {
			_ = a.deps.Store.UpdatePeerStatus(ctx, p.Name, now, p.LastOK, err.Error(), p.RemoteBuild, p.RemoteSchema, p.Protocol, p.Delegable)
			continue
		}
		if st.Protocol != protocolVersion {
			_ = a.deps.Store.UpdatePeerStatus(ctx, p.Name, now, p.LastOK,
				"peer speaks protocol "+strconv.Itoa(st.Protocol)+"; upgrade the older node", st.Build, st.Schema, st.Protocol, st.Delegable)
			continue
		}
		a.rememberPeerStatus(p.Name, st)
		_ = a.deps.Store.UpdatePeerStatus(ctx, p.Name, now, now, "", st.Build, st.Schema, st.Protocol, st.Delegable)
	}
}

// RunPeerPoller polls peers at the configured interval until ctx ends. Each
// wait is jittered by up to 20% so nodes that were paired together do not poll
// each other in lockstep for ever.
func (a *API) RunPeerPoller(ctx context.Context) {
	for {
		secs := 60
		if a.deps.Store != nil {
			if s, err := a.deps.Store.GetSettings(ctx); err == nil && s.PeerPollSeconds > 0 {
				secs = s.PeerPollSeconds
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(pollWait(secs)):
			a.PollPeersOnce(ctx)
		}
	}
}

// pollWait is the poll interval jittered by up to 20% either way, so two nodes
// paired together drift apart instead of polling in lockstep.
func pollWait(secs int) time.Duration {
	return time.Duration(float64(secs) * (0.8 + 0.4*mrand.Float64()) * float64(time.Second))
}

func (a *API) peerStatus(w http.ResponseWriter, r *http.Request) {
	// The master switch gates this route too: an old inbound key must not keep
	// answering after pairing is turned off.
	if !a.peersEnabled() {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	presented := extractBearer(r)
	if _, ok := a.deps.Store.ValidateInboundKey(r.Context(), presented); !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	// A held service starts nothing, so it accepts no delegated work either.
	accepting := true
	if h, err := a.deps.Store.ServiceHold(r.Context()); err == nil && h.Active {
		accepting = false
	}
	st := map[string]any{
		"protocol":  protocolVersion,
		"node":      a.nodeName(),
		"build":     BuildTime,
		"schema":    schemaVersion(a.deps.Store),
		"time":      time.Now().Unix(),
		"accepting": accepting,
		"load": map[string]any{
			"1m":      load1m(),
			"cores":   numCPU(),
			"free_gb": freeGB(a.dataDir()),
		},
		"backends":  []map[string]any{},
		"projects":  []map[string]any{},
		"delegable": []string{},
	}
	running, rest := a.peerBackendStatus(r.Context())
	if bs, err := a.deps.Store.ListBackends(r.Context()); err == nil {
		blist := []map[string]any{}
		for _, b := range bs {
			// A backend with no resting model still reports a list, never null.
			until := rest[b.Name]
			if until == nil {
				until = []int64{}
			}
			blist = append(blist, map[string]any{
				"name":       b.Name,
				"slots":      b.Slots,
				"running":    running[b.Name],
				"rest_until": until,
			})
		}
		st["backends"] = blist
	}
	if plist, delegable, err := a.peerProjectStatus(r.Context()); err == nil {
		st["projects"] = plist
		st["delegable"] = delegable
	}
	writeJSON(w, http.StatusOK, st)
}

// peerBackendStatus counts running stages per backend and lists rest ends.
func (a *API) peerBackendStatus(ctx context.Context) (map[string]int, map[string][]int64) {
	running := map[string]int{}
	rest := map[string][]int64{}
	db := a.deps.Store.DB()
	if db == nil {
		return running, rest
	}
	if rows, err := db.QueryContext(ctx, `SELECT backend, COUNT(*) FROM stages WHERE state = 'running' AND backend <> '' GROUP BY backend`); err == nil {
		defer rows.Close()
		for rows.Next() {
			var name string
			var n int
			if rows.Scan(&name, &n) == nil {
				running[name] = n
			}
		}
	}
	if rows, err := db.QueryContext(ctx, `SELECT backend, until FROM model_rest WHERE until > ? ORDER BY until`, time.Now().UnixMilli()); err == nil {
		defer rows.Close()
		for rows.Next() {
			var name string
			var until int64
			if rows.Scan(&name, &until) == nil {
				rest[name] = append(rest[name], until)
			}
		}
	}
	return running, rest
}

// peerProjectStatus reports each project's active effort state and open work.
func (a *API) peerProjectStatus(ctx context.Context) ([]map[string]any, []string, error) {
	db := a.deps.Store.DB()
	if db == nil {
		return []map[string]any{}, []string{}, nil
	}
	rows, err := db.QueryContext(ctx, `
		SELECT p.slug, COALESCE(e.run_state, 'stopped'), COALESCE(e.id, 0),
		       (SELECT COUNT(*) FROM stages s WHERE s.effort = e.id AND s.state = 'running'),
		       (SELECT COUNT(*) FROM tasks t JOIN queues q ON q.id = t.queue WHERE q.effort = e.id)
		FROM projects p
		LEFT JOIN efforts e ON e.project = p.slug AND e.status NOT IN ('complete', 'abandoned')
		ORDER BY p.slug`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	delegable := []string{}
	for rows.Next() {
		var slug, state string
		var effort int64
		var runningStages, openTasks int
		if err := rows.Scan(&slug, &state, &effort, &runningStages, &openTasks); err != nil {
			return nil, nil, err
		}
		openEfforts := 0
		if effort > 0 {
			openEfforts = 1
		}
		out = append(out, map[string]any{
			"slug":           slug,
			"run_state":      state,
			"running_stages": runningStages,
			"open_efforts":   openEfforts,
			"open_tasks":     openTasks,
		})
		delegable = append(delegable, slug)
	}
	return out, delegable, rows.Err()
}

func extractBearer(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if strings.HasPrefix(auth, prefix) {
		return strings.TrimPrefix(auth, prefix)
	}
	return r.Header.Get("X-API-Key")
}

// restoreInboundKey puts the peer's inbound hash (agentflow row and the auth
// mirror) back after a failed rotation.
func (a *API) restoreInboundKey(name, hash string, existed bool) {
	ctx := context.Background()
	if !existed {
		_ = a.deps.Store.DeleteInboundKey(ctx, name)
		a.forgetPeerKey(name)
		return
	}
	_ = a.deps.Store.StoreInboundKey(ctx, name, hash)
	if a.deps.PeerKeySink != nil {
		_ = a.deps.PeerKeySink(name, "sha256:"+hash)
	}
}

func genKey() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// rememberPeerStatus keeps the last status a peer reported, for the peers page
// and the Test button (AF-24: the last status stays in memory).
func (a *API) rememberPeerStatus(name string, st peerclient.PeerStatus) {
	a.peerMu.Lock()
	defer a.peerMu.Unlock()
	if a.peerLast == nil {
		a.peerLast = map[string]peerclient.PeerStatus{}
	}
	a.peerLast[name] = st
}

func (a *API) lastPeerStatus(name string) (peerclient.PeerStatus, bool) {
	a.peerMu.Lock()
	defer a.peerMu.Unlock()
	st, ok := a.peerLast[name]
	return st, ok
}

func schemaVersion(s *store.Store) int {
	if s == nil {
		return 0
	}
	var v int
	_ = s.DB().QueryRow(`SELECT COALESCE(MAX(version),0) FROM schema_version`).Scan(&v)
	return v
}

func numCPU() int { return runtime.NumCPU() }

func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

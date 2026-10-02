package haproxy

// A test-only fake CertMachine. It replays the exact byte-level download
// vectors of the real CertMachine API (internal/certmachine/handler.go's
// writeVerifiedDownload and handler_extension_test.go): a strong
// ETag: "sha256-<hex of body>", X-Cert-Id, X-Cert-Fingerprint, If-None-Match
// -> 304, and HEAD. Tamper knobs let the integrity tests drive each rejection
// path. Nothing here parses a certificate; the "haproxy.pem" body is opaque
// bytes (it even carries a private-key marker on purpose, so the leakage test
// can prove the marker never escapes into an error, log or JSON the package
// produces).

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// fakeCMPrivateKeyMarker is embedded in every fake haproxy.pem body so the
// leakage tests have a real private-key marker to look for.
const fakeCMPrivateKeyMarker = "-----BEGIN PRIVATE KEY-----"

// fakeCMEntry is one cert the fake serves: its list/detail metadata plus the
// opaque bytes of its haproxy.pem.
type fakeCMEntry struct {
	cert CertMachineCert
	body []byte
}

// fakeCertMachine is an httptest-backed stand-in for CertMachine. It is served
// over plain HTTP on loopback (127.0.0.1), which the client's transport policy
// allows, so the tests need no TLS material.
type fakeCertMachine struct {
	entries map[int64]*fakeCMEntry
	// expectBearer, when set, is the API key every request must carry.
	expectBearer string
	// tamper knobs for the download route.
	tamperBody       bool
	corruptETag      bool
	missingETag      bool
	wrongID          bool
	wrongFingerprint bool
	// refusal / error status knobs for the download route.
	status401 bool
	status500 bool
	refuse409 string // non-empty: respond 409 with this message as {"error": ...}
}

func newFakeCertMachine() *fakeCertMachine {
	return &fakeCertMachine{entries: map[int64]*fakeCMEntry{}}
}

// addCert registers a cert with the given id/fqdn/fingerprint/status/sans and a
// haproxy.pem body that carries the private-key marker.
func (f *fakeCertMachine) addCert(id int64, fqdn, fingerprint, status string, dns []string) *fakeCMEntry {
	fp := fingerprint
	body := []byte("cert-for-" + fqdn + "\n" + fakeCMPrivateKeyMarker + "\nsecret-key-bytes-" + strconv.FormatInt(id, 10) + "\n-----END PRIVATE KEY-----\n")
	e := &fakeCMEntry{
		cert: CertMachineCert{
			ID:          id,
			FQDN:        fqdn,
			SANs:        CertMachineSANs{DNS: dns},
			Fingerprint: &fp,
			Status:      status,
		},
		body: body,
	}
	f.entries[id] = e
	return e
}

func (f *fakeCertMachine) start(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(f.handler())
	t.Cleanup(srv.Close)
	return srv
}

func (f *fakeCertMachine) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/certs", f.handleList)
	mux.HandleFunc("GET /api/certs/{id}", f.handleGet)
	mux.HandleFunc("GET /api/certs/{id}/files/haproxy.pem", f.handleDownload)
	mux.HandleFunc("HEAD /api/certs/{id}/files/haproxy.pem", f.handleDownload)
	return mux
}

func (f *fakeCertMachine) authOK(r *http.Request) bool {
	if f.expectBearer == "" {
		return true
	}
	return r.Header.Get("Authorization") == "Bearer "+f.expectBearer
}

func (f *fakeCertMachine) handleList(w http.ResponseWriter, r *http.Request) {
	if !f.authOK(r) {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	q := r.URL.Query()
	fqdn, status := q.Get("fqdn"), q.Get("status")
	switch status {
	case "", "active", "archived", "quarantined":
	default:
		http.Error(w, `{"error":"status must be one of active, archived, quarantined"}`, http.StatusBadRequest)
		return
	}
	out := []CertMachineCert{}
	for _, e := range f.entries {
		if fqdn != "" && !strings.EqualFold(e.cert.FQDN, fqdn) {
			continue
		}
		if status != "" && e.cert.Status != status {
			continue
		}
		out = append(out, e.cert)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"certs": out})
}

func (f *fakeCertMachine) handleGet(w http.ResponseWriter, r *http.Request) {
	if !f.authOK(r) {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	e, ok := f.entries[id]
	if !ok {
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(e.cert)
}

func (f *fakeCertMachine) handleDownload(w http.ResponseWriter, r *http.Request) {
	if !f.authOK(r) {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	if f.status401 {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	if f.status500 {
		http.Error(w, `{"error":"boom"}`, http.StatusInternalServerError)
		return
	}
	if f.refuse409 != "" {
		http.Error(w, `{"error":"`+f.refuse409+`"}`, http.StatusConflict)
		return
	}
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	e, ok := f.entries[id]
	if !ok {
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
		return
	}

	sum := sha256.Sum256(e.body)
	etag := `"sha256-` + hex.EncodeToString(sum[:]) + `"`
	if f.corruptETag {
		etag = `"sha256-0000000000000000000000000000000000000000000000000000000000000000"`
	}
	if !f.missingETag {
		w.Header().Set("ETag", etag)
	}
	certID := strconv.FormatInt(e.cert.ID, 10)
	if f.wrongID {
		certID = strconv.FormatInt(e.cert.ID+1, 10)
	}
	w.Header().Set("X-Cert-Id", certID)
	fp := ""
	if e.cert.Fingerprint != nil {
		fp = *e.cert.Fingerprint
	}
	if f.wrongFingerprint {
		fp = "deadbeef-not-the-one"
	}
	w.Header().Set("X-Cert-Fingerprint", fp)
	w.Header().Set("Content-Type", "application/x-pem-file")
	w.Header().Set("Cache-Control", "no-store, max-age=0")

	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	body := e.body
	if f.tamperBody {
		body = append(append([]byte(nil), e.body...), []byte("TAMPERED")...)
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

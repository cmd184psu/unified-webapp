package certmachine

// Tests for the haproxy-editor extension of the CertMachine API
// (docs/frd/FRD-haproxy-editor.md section 13, FR-C1 to FR-C5): list filters,
// a strong ETag plus identity headers on file downloads, conditional
// requests and HEAD. All additive; the refusals stay exactly as they were.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

func etagFor(body []byte) string {
	sum := sha256.Sum256(body)
	return `"sha256-` + hex.EncodeToString(sum[:]) + `"`
}

func listCertsAt(t *testing.T, url string) (int, []Cert) {
	t.Helper()
	res := doJSON(t, http.MethodGet, url, "")
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return res.StatusCode, nil
	}
	var body struct {
		Certs []Cert `json:"certs"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	return res.StatusCode, body.Certs
}

// FR-C1: fqdn and status filters on GET /api/certs.
func TestListCertsFilters(t *testing.T) {
	srv, httpSrv := newHandlerTestHTTPServer(t)
	if err := srv.db.InitCA(context.Background(), ""); err != nil {
		t.Fatalf("InitCA: %v", err)
	}
	a := generateTestCert(t, srv.db, "a.example.local")
	generateTestCert(t, srv.db, "b.example.local")
	// Renewing a archives the first row and activates a new one.
	renewRes := doJSON(t, http.MethodPost, httpSrv.URL+"/api/certs/"+strconv.FormatInt(a.Cert.ID, 10)+"/renew", "")
	var renewed issueResponse
	if err := json.NewDecoder(renewRes.Body).Decode(&renewed); err != nil {
		t.Fatalf("decode renew: %v", err)
	}
	renewRes.Body.Close()
	insertQuarantinedCert(t, srv.db, "q.example.local", "mismatch")

	all, err := srv.db.ListCerts(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	// No filter: exactly today's list. ListCerts orders by not_after only, so
	// ties are not deterministic: compare as a set keyed by id, but
	// shape-exact (the full JSON of each cert).
	unfiltered := doJSON(t, http.MethodGet, httpSrv.URL+"/api/certs", "")
	var rawBody struct {
		Certs []json.RawMessage `json:"certs"`
	}
	if err := json.NewDecoder(unfiltered.Body).Decode(&rawBody); err != nil {
		t.Fatalf("decode unfiltered: %v", err)
	}
	unfiltered.Body.Close()
	if unfiltered.StatusCode != 200 || len(rawBody.Certs) != len(all) {
		t.Fatalf("unfiltered list = %d with %d certs, want 200 with %d", unfiltered.StatusCode, len(rawBody.Certs), len(all))
	}
	want := map[int64]string{}
	for _, c := range all {
		b, err := json.Marshal(c)
		if err != nil {
			t.Fatal(err)
		}
		want[c.ID] = string(b)
	}
	for _, raw := range rawBody.Certs {
		var c Cert
		if err := json.Unmarshal(raw, &c); err != nil {
			t.Fatal(err)
		}
		if want[c.ID] != string(raw) {
			t.Errorf("unfiltered cert %d JSON = %s, want %s", c.ID, raw, want[c.ID])
		}
		delete(want, c.ID)
	}
	if len(want) != 0 {
		t.Errorf("unfiltered list missing certs: %v", want)
	}

	// fqdn alone: exact and case-insensitive, both rows for that name.
	code, certs := listCertsAt(t, httpSrv.URL+"/api/certs?fqdn=A.Example.LOCAL")
	if code != 200 || len(certs) != 2 {
		t.Fatalf("fqdn filter = %d with %d certs, want 200 with 2 (archived + active)", code, len(certs))
	}
	for _, c := range certs {
		if !strings.EqualFold(c.FQDN, "a.example.local") {
			t.Errorf("fqdn filter returned %q", c.FQDN)
		}
	}

	// fqdn + status=active: exactly the renewed row.
	code, certs = listCertsAt(t, httpSrv.URL+"/api/certs?fqdn=a.example.local&status=active")
	if code != 200 || len(certs) != 1 || certs[0].ID != renewed.Cert.ID || certs[0].Status != "active" {
		t.Fatalf("fqdn+status=active = %d %+v, want the one active row id %d", code, certs, renewed.Cert.ID)
	}

	// status alone.
	if _, certs := listCertsAt(t, httpSrv.URL+"/api/certs?status=quarantined"); len(certs) != 1 || certs[0].FQDN != "q.example.local" {
		t.Errorf("status=quarantined = %+v, want only q.example.local", certs)
	}
	if _, certs := listCertsAt(t, httpSrv.URL+"/api/certs?status=archived"); len(certs) != 1 || certs[0].ID != a.Cert.ID {
		t.Errorf("status=archived = %+v, want only the original a row", certs)
	}

	// No match is an empty JSON array, not null and not an error.
	res := doJSON(t, http.MethodGet, httpSrv.URL+"/api/certs?fqdn=nope.example.local", "")
	raw := string(mustReadAll(t, res.Body))
	res.Body.Close()
	if res.StatusCode != 200 || !strings.Contains(raw, `"certs":[]`) {
		t.Errorf("no match = %d %s, want 200 with an empty array", res.StatusCode, raw)
	}

	// An unknown status is a 400.
	bad := doJSON(t, http.MethodGet, httpSrv.URL+"/api/certs?status=bogus", "")
	bad.Body.Close()
	if bad.StatusCode != http.StatusBadRequest {
		t.Errorf("status=bogus = %d, want 400", bad.StatusCode)
	}
}

// FR-C2, FR-C3: every file download carries a strong ETag of its exact body
// and the identity headers.
func TestFileDownloadsCarryETagAndIdentityHeaders(t *testing.T) {
	srv, httpSrv := newHandlerTestHTTPServer(t)
	if err := srv.db.InitCA(context.Background(), ""); err != nil {
		t.Fatalf("InitCA: %v", err)
	}
	result := generateTestCert(t, srv.db, "etag.example.local")
	id := strconv.FormatInt(result.Cert.ID, 10)

	for _, name := range []string{"cert.pem", "key.pem", "haproxy.pem"} {
		res := doJSON(t, http.MethodGet, httpSrv.URL+"/api/certs/"+id+"/files/"+name, "")
		body := mustReadAll(t, res.Body)
		res.Body.Close()
		if res.StatusCode != 200 {
			t.Fatalf("%s = %d, want 200", name, res.StatusCode)
		}
		if got, want := res.Header.Get("ETag"), etagFor(body); got != want {
			t.Errorf("%s ETag = %q, want %q (sha256 of the exact body)", name, got, want)
		}
		if got := res.Header.Get("X-Cert-Id"); got != id {
			t.Errorf("%s X-Cert-Id = %q, want %q", name, got, id)
		}
		if result.Cert.Fingerprint == nil {
			t.Fatal("fixture cert has no fingerprint")
		}
		if got := res.Header.Get("X-Cert-Fingerprint"); got != *result.Cert.Fingerprint {
			t.Errorf("%s X-Cert-Fingerprint = %q, want the list's %q", name, got, *result.Cert.Fingerprint)
		}
		// FR-C3: it is the fingerprint exactly as GET /api/certs reports it.
		_, listed := listCertsAt(t, httpSrv.URL+"/api/certs?fqdn=etag.example.local&status=active")
		if len(listed) != 1 || listed[0].Fingerprint == nil || res.Header.Get("X-Cert-Fingerprint") != *listed[0].Fingerprint {
			t.Errorf("%s X-Cert-Fingerprint %q does not match GET /api/certs: %+v", name, res.Header.Get("X-Cert-Fingerprint"), listed)
		}
		// Content-Disposition and no-store are untouched.
		if cc := res.Header.Get("Cache-Control"); cc != "no-store, max-age=0" {
			t.Errorf("%s Cache-Control = %q, want unchanged", name, cc)
		}
	}

	// The bytes of a given cert id never change, so neither does the ETag.
	first := doJSON(t, http.MethodGet, httpSrv.URL+"/api/certs/"+id+"/files/haproxy.pem", "")
	first.Body.Close()
	second := doJSON(t, http.MethodGet, httpSrv.URL+"/api/certs/"+id+"/files/haproxy.pem", "")
	second.Body.Close()
	if first.Header.Get("ETag") == "" || first.Header.Get("ETag") != second.Header.Get("ETag") {
		t.Errorf("haproxy.pem ETag not stable across requests: %q vs %q", first.Header.Get("ETag"), second.Header.Get("ETag"))
	}
}

// FR-C4: If-None-Match gives 304 with no body; HEAD gives headers only.
func TestFileDownloadsConditionalAndHead(t *testing.T) {
	srv, httpSrv := newHandlerTestHTTPServer(t)
	if err := srv.db.InitCA(context.Background(), ""); err != nil {
		t.Fatalf("InitCA: %v", err)
	}
	result := generateTestCert(t, srv.db, "cond.example.local")
	id := strconv.FormatInt(result.Cert.ID, 10)
	url := httpSrv.URL + "/api/certs/" + id + "/files/haproxy.pem"

	plain := doJSON(t, http.MethodGet, url, "")
	body := mustReadAll(t, plain.Body)
	plain.Body.Close()
	etag := plain.Header.Get("ETag")
	if etag == "" || len(body) == 0 {
		t.Fatalf("setup: ETag=%q body=%d bytes", etag, len(body))
	}

	conditional := func(inm string) *http.Response {
		req, _ := http.NewRequest(http.MethodGet, url, nil)
		req.Header.Set("If-None-Match", inm)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}

	for name, inm := range map[string]string{"exact": etag, "weak": "W/" + etag, "list": `"nope", ` + etag, "star": "*"} {
		res := conditional(inm)
		got := mustReadAll(t, res.Body)
		res.Body.Close()
		if res.StatusCode != http.StatusNotModified || len(got) != 0 {
			t.Errorf("If-None-Match %s = %d with %d body bytes, want 304 and none", name, res.StatusCode, len(got))
		}
		if res.Header.Get("ETag") != etag || res.Header.Get("X-Cert-Id") != id {
			t.Errorf("304 (%s) lost ETag/identity headers: %v", name, res.Header)
		}
	}

	wrong := conditional(`"sha256-0000"`)
	wrongBody := mustReadAll(t, wrong.Body)
	wrong.Body.Close()
	if wrong.StatusCode != 200 || len(wrongBody) == 0 {
		t.Errorf("non-matching If-None-Match = %d with %d bytes, want 200 with the body", wrong.StatusCode, len(wrongBody))
	}

	head := doJSON(t, http.MethodHead, url, "")
	headBody := mustReadAll(t, head.Body)
	head.Body.Close()
	if head.StatusCode != 200 || len(headBody) != 0 || head.Header.Get("ETag") != etag || head.Header.Get("X-Cert-Fingerprint") == "" {
		t.Errorf("HEAD = %d, %d body bytes, ETag %q, want 200, none, %q and an identity header", head.StatusCode, len(headBody), head.Header.Get("ETag"), etag)
	}
}

// Unchanged on purpose: refusals keep their status and message and carry no
// ETag or identity headers (nothing was exported).
func TestRefusedAndMissingDownloadsCarryNoETag(t *testing.T) {
	srv, httpSrv := newHandlerTestHTTPServer(t)
	if err := srv.db.InitCA(context.Background(), ""); err != nil {
		t.Fatalf("InitCA: %v", err)
	}
	qid := strconv.FormatInt(insertQuarantinedCert(t, srv.db, "broken.example.local", "key mismatch"), 10)

	quarantined := doJSON(t, http.MethodGet, httpSrv.URL+"/api/certs/"+qid+"/files/haproxy.pem", "")
	qBody := decodeErrorBody(t, quarantined)
	if quarantined.StatusCode != http.StatusConflict || !strings.Contains(qBody["error"], "quarantined") {
		t.Errorf("quarantined haproxy.pem = %d %v, want 409 naming the quarantine", quarantined.StatusCode, qBody)
	}
	for _, h := range []string{"ETag", "X-Cert-Id", "X-Cert-Fingerprint"} {
		if v := quarantined.Header.Get(h); v != "" {
			t.Errorf("refused download carries %s = %q, want none", h, v)
		}
	}

	missing := doJSON(t, http.MethodGet, httpSrv.URL+"/api/certs/999999/files/haproxy.pem", "")
	missing.Body.Close()
	if missing.StatusCode != http.StatusNotFound || missing.Header.Get("ETag") != "" {
		t.Errorf("unknown id = %d ETag %q, want 404 and no ETag", missing.StatusCode, missing.Header.Get("ETag"))
	}
}

// The shared downloads (CA root, tgz bundle) must not gain cert identity
// headers: the root has no cert id and the bundle embeds time.Now().
func TestSharedDownloadsCarryNoCertHeaders(t *testing.T) {
	srv, httpSrv := newHandlerTestHTTPServer(t)
	if err := srv.db.InitCA(context.Background(), ""); err != nil {
		t.Fatalf("InitCA: %v", err)
	}
	result := generateTestCert(t, srv.db, "shared.example.local")
	if result.Cert.Fingerprint == nil {
		t.Fatal("fixture cert has no fingerprint")
	}
	id := strconv.FormatInt(result.Cert.ID, 10)

	for _, path := range []string{"/api/ca/root.crt", "/api/certs/" + id + "/bundle"} {
		res := doJSON(t, http.MethodGet, httpSrv.URL+path, "")
		body := mustReadAll(t, res.Body)
		res.Body.Close()
		if res.StatusCode != 200 || len(body) == 0 {
			t.Errorf("%s = %d with %d bytes, want 200 with a body", path, res.StatusCode, len(body))
		}
		if cc := res.Header.Get("Cache-Control"); cc != "no-store, max-age=0" {
			t.Errorf("%s Cache-Control = %q, want unchanged", path, cc)
		}
		if res.Header.Get("Content-Disposition") == "" {
			t.Errorf("%s lost Content-Disposition", path)
		}
		for _, h := range []string{"ETag", "X-Cert-Id", "X-Cert-Fingerprint"} {
			if v := res.Header.Get(h); v != "" {
				t.Errorf("%s carries %s = %q, want none", path, h, v)
			}
		}
	}
}

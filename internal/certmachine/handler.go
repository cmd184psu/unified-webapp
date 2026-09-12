// handler.go wires the certmachine HTTP API onto Server.mux: the twelve
// routes in the slice 8 doc, their method-less 405 fallthroughs, and the
// single sentinel-to-status mapping every handler funnels errors through.
//
// Route shape follows Option B (plan §"API route shape"): each mutating path
// gets a method-prefixed handler ("POST /api/certs") plus a method-less
// fallthrough registered on the same pattern, which is the only mechanism
// measured to produce a real 405 + Allow header on this mux (Appendix C-2) --
// without it, the "/"-registered static handler's JSON 404 wins every time,
// because bare "/" always matches. Read-only paths deliberately get no
// fallthrough: a wrong method on them answers the static handler's JSON 404,
// not a 405 -- documented asymmetry, not an oversight (see mountRoutes).
package certmachine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"time"

	"cmd184psu/unified-webapp/internal/platform/response"
)

// mountRoutes registers every /api/ route plus its 405 fallthrough (for
// mutating paths only) on s.mux, and logs the one boot line the slice 8 doc
// requires. Called once, from New (build.go), before the "/" static handler
// is mounted.
func (s *Server) mountRoutes() {
	s.mux.HandleFunc("GET /api/config", s.handleConfigGet)

	s.mux.HandleFunc("GET /api/ca", s.handleCAGet)
	s.mux.HandleFunc("POST /api/ca/init", s.handleCAInit)
	s.mux.HandleFunc("GET /api/ca/root.crt", s.handleCARootGet)

	s.mux.HandleFunc("GET /api/certs", s.handleCertsGet)
	s.mux.HandleFunc("POST /api/certs", s.handleCertsPost)
	s.mux.HandleFunc("GET /api/certs/{id}", s.handleCertGet)
	s.mux.HandleFunc("DELETE /api/certs/{id}", s.handleCertDelete)
	s.mux.HandleFunc("POST /api/certs/{id}/renew", s.handleCertRenew)
	s.mux.HandleFunc("GET /api/certs/{id}/files/{name}", s.handleCertFileGet)
	s.mux.HandleFunc("GET /api/certs/{id}/bundle", s.handleCertBundleGet)

	s.mux.HandleFunc("GET /api/import/preview", s.handleImportPreview)
	s.mux.HandleFunc("POST /api/import", s.handleImportExecute)

	// Method-less fallthroughs: the only paths carrying a mutating method.
	// Each Allow string is hand-maintained and asserted against these exact
	// registrations by TestAllowHeadersMatchRegisteredRoutes.
	s.mux.HandleFunc("/api/ca/init", methodNotAllowed("POST"))
	s.mux.HandleFunc("/api/certs", methodNotAllowed("GET, POST"))
	s.mux.HandleFunc("/api/certs/{id}", methodNotAllowed("GET, DELETE"))
	s.mux.HandleFunc("/api/certs/{id}/renew", methodNotAllowed("POST"))
	s.mux.HandleFunc("/api/import", methodNotAllowed("POST"))

	s.logBoot()
}

// logBoot emits the one boot log line the slice 8 doc specifies, once, at
// build time. Values that could not be determined (no CA yet, legacy import
// not configured) render as plain, non-alarming placeholders rather than
// error text.
func (s *Server) logBoot() {
	ctx := context.Background()
	count, err := s.db.CountCerts(ctx)
	if err != nil {
		count = -1
	}
	caStatus := "absent"
	if _, err := s.db.GetCA(ctx); err == nil {
		caStatus = "present"
	}
	legacyDir := s.opts.LegacyImportDir
	legacyStatus := "ok"
	if legacyDir == "" {
		legacyDir = "none"
		legacyStatus = "n/a"
	} else if s.legacyImportReason != "" {
		legacyStatus = s.legacyImportReason
	}
	log.Printf("certmachine: db=%s schema=v%d certs=%d ca=%s legacy_import=%s(%s)",
		s.opts.DBPath, schemaVersion, count, caStatus, legacyDir, legacyStatus)
}

// methodNotAllowed answers every request with a 405 and the given Allow
// header -- the fallthrough mountRoutes registers on every mutating path's
// bare pattern, alongside its method-prefixed sibling(s).
func methodNotAllowed(allow string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Allow", allow)
		response.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// writeStoreError is the single sentinel-to-status mapping every handler in
// this file funnels its store/pki errors through (plan: "in the spirit of
// internal/grocery/handler.go:335-352"). ErrNotFound is a missing resource;
// ErrValidation and ErrConfirmMismatch are client input errors; every other
// sentinel this package declares describes a well-formed request that
// conflicts with current state, so it is a 409 -- one rule instead of a
// per-sentinel table that a new sentinel could be added to without updating
// this switch. Anything unrecognized is logged with its detail and answered
// with a generic 500 body: err.Error() never reaches the client past this
// point, so SQL text, file paths, and PEM material can never leak through a
// 500 (FRD §7).
func writeStoreError(w http.ResponseWriter, err error) {
	status, msg := storeErrorStatus(err)
	response.WriteError(w, status, msg)
}

// storeErrorStatus is writeStoreError's mapping, split out so
// handleImportExecute can reuse the identical status and message while
// writing a body that carries more than the error envelope (its partial
// ImportReport). Everything writeStoreError's doc comment says applies here:
// the 500 branch is the only one that logs, and the only one whose returned
// message is not err.Error().
func storeErrorStatus(err error) (int, string) {
	switch {
	case errors.Is(err, ErrNotFound):
		return http.StatusNotFound, err.Error()
	case errors.Is(err, ErrValidation), errors.Is(err, ErrConfirmMismatch):
		return http.StatusBadRequest, err.Error()
	case errors.Is(err, ErrDuplicateActive),
		errors.Is(err, ErrDuplicateImport),
		errors.Is(err, ErrCAExists),
		errors.Is(err, ErrCANotFound),
		errors.Is(err, ErrQuarantined),
		errors.Is(err, ErrImportPending),
		errors.Is(err, ErrCAExpiringSoon),
		errors.Is(err, ErrNewerActiveExists),
		errors.Is(err, ErrImportConfirmRequired),
		errors.Is(err, ErrQuarantinedDownload),
		errors.Is(err, ErrRootMismatch),
		// Both CA-reconciliation refusals (importer.go) are conflicts with
		// current state, not internal failures. Omitting them cost the
		// operator the one message that carries caReplacementRemedy -- the
		// only documented way out of a fingerprint mismatch -- by burying it
		// behind a generic 500.
		errors.Is(err, ErrCAKeyMismatch),
		errors.Is(err, ErrCAFingerprintMismatch):
		return http.StatusConflict, err.Error()
	default:
		log.Printf("certmachine: internal error: %v", err)
		return http.StatusInternalServerError, "internal error"
	}
}

// parseCertID extracts the {id} path value as an int64. A non-numeric value
// can never name a row, so it is reported the same way an absent one is --
// ErrNotFound, through the same writeStoreError path every other handler
// uses, rather than a second ad hoc 404.
func parseCertID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		return 0, ErrNotFound
	}
	return id, nil
}

// maxRequestBodyBytes bounds every JSON request body this package reads. The
// POST routes are unauthenticated, and the largest legitimate body is a
// 64-entry SAN list -- a few kilobytes. Without a bound, a multi-gigabyte
// dnsSans array is fully materialized in memory before ValidateRequest gets a
// chance to reject it for exceeding maxSANCount.
const maxRequestBodyBytes = 1 << 20

// decodeOptionalJSON decodes r's body into v, reading at most
// maxRequestBodyBytes. An empty body (io.EOF on the first token) is not an
// error -- v is left at its zero value -- because POST /api/import's
// confirmNonEmpty is optional and callers may send no body at all rather than
// "{}". w is needed only by http.MaxBytesReader, which uses it to stop
// reading the connection once the cap is hit.
func decodeOptionalJSON(w http.ResponseWriter, r *http.Request, v any) error {
	defer r.Body.Close()
	body := http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	if err := json.NewDecoder(body).Decode(v); err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return err
	}
	return nil
}

// writeBodyError answers a decodeOptionalJSON failure. An over-limit body is
// reported as 413 naming the cap rather than folded into the generic 400:
// "your JSON is malformed" would send the caller looking for a syntax error
// that isn't there.
func writeBodyError(w http.ResponseWriter, err error) {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		response.WriteError(w, http.StatusRequestEntityTooLarge,
			fmt.Sprintf("request body exceeds the %d-byte limit", maxRequestBodyBytes))
		return
	}
	response.WriteError(w, http.StatusBadRequest, "invalid request body")
}

// configResponse is GET /api/config's body.
type configResponse struct {
	DefaultValidityDays   int    `json:"defaultValidityDays"`
	ExpiryWarnDays        int    `json:"expiryWarnDays"`
	CertCount             int    `json:"certCount"`
	LegacyImportAvailable bool   `json:"legacyImportAvailable"`
	LegacyImportDir       string `json:"legacyImportDir"`
	LegacyImportReason    string `json:"legacyImportReason"`
}

func (s *Server) handleConfigGet(w http.ResponseWriter, r *http.Request) {
	count, err := s.db.CountCerts(r.Context())
	if err != nil {
		writeStoreError(w, err)
		return
	}
	response.WriteJSON(w, http.StatusOK, configResponse{
		DefaultValidityDays:   s.opts.DefaultValidityDays,
		ExpiryWarnDays:        s.opts.ExpiryWarnDays,
		CertCount:             count,
		LegacyImportAvailable: s.opts.LegacyImportDir != "" && s.legacyImportReason == "",
		LegacyImportDir:       s.opts.LegacyImportDir,
		LegacyImportReason:    s.legacyImportReason,
	})
}

// caResponse is GET /api/ca and POST /api/ca/init's body. Exists is always
// present; the rest are omitted (an absent CA has no subject/serial/etc. to
// report) when Exists is false.
type caResponse struct {
	Exists       bool    `json:"exists"`
	Subject      string  `json:"subject,omitempty"`
	Serial       string  `json:"serial,omitempty"`
	NotBefore    string  `json:"notBefore,omitempty"`
	NotAfter     string  `json:"notAfter,omitempty"`
	Fingerprint  string  `json:"fingerprint,omitempty"`
	ImportedFrom *string `json:"importedFrom,omitempty"`
}

func caResponseFrom(ca *CA) caResponse {
	return caResponse{
		Exists:       true,
		Subject:      ca.Subject,
		Serial:       ca.Serial,
		NotBefore:    ca.NotBefore,
		NotAfter:     ca.NotAfter,
		Fingerprint:  ca.Fingerprint,
		ImportedFrom: ca.ImportedFrom,
	}
}

func (s *Server) handleCAGet(w http.ResponseWriter, r *http.Request) {
	ca, err := s.db.GetCA(r.Context())
	if err != nil {
		if errors.Is(err, ErrCANotFound) {
			response.WriteJSON(w, http.StatusOK, caResponse{Exists: false})
			return
		}
		writeStoreError(w, err)
		return
	}
	response.WriteJSON(w, http.StatusOK, caResponseFrom(ca))
}

func (s *Server) handleCAInit(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := s.db.InitCA(ctx, s.opts.LegacyImportDir); err != nil {
		writeStoreError(w, err)
		return
	}
	ca, err := s.db.GetCA(ctx)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	response.WriteJSON(w, http.StatusCreated, caResponseFrom(ca))
}

func (s *Server) handleCARootGet(w http.ResponseWriter, r *http.Request) {
	ca, err := s.db.GetCA(r.Context())
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeDownload(w, "rootCA.crt", pemContentType, []byte(ca.CertPEM))
}

func (s *Server) handleCertsGet(w http.ResponseWriter, r *http.Request) {
	certs, err := s.db.ListCerts(r.Context())
	if err != nil {
		writeStoreError(w, err)
		return
	}
	response.WriteJSON(w, http.StatusOK, map[string]any{"certs": certs})
}

// generateRequest is POST /api/certs's body.
type generateRequest struct {
	FQDN    string   `json:"fqdn"`
	DNSSans []string `json:"dnsSans"`
	IPSans  []string `json:"ipSans"`
}

// issueResponse is the shared body for POST /api/certs and
// POST /api/certs/{id}/renew: the newly issued row plus the FR-4 validity
// clamp fact. RequestedNotAfter is only present when ValidityClamped is true
// -- when it isn't, it would just repeat Cert.NotAfter.
type issueResponse struct {
	Cert              Cert   `json:"cert"`
	ValidityClamped   bool   `json:"validityClamped"`
	RequestedNotAfter string `json:"requestedNotAfter,omitempty"`
}

func issueResponseFrom(result IssueResult) issueResponse {
	resp := issueResponse{Cert: result.Cert, ValidityClamped: result.Clamped}
	if result.Clamped {
		resp.RequestedNotAfter = result.RequestedNotAfter.UTC().Format(time.RFC3339)
	}
	return resp
}

func (s *Server) handleCertsPost(w http.ResponseWriter, r *http.Request) {
	var body generateRequest
	if err := decodeOptionalJSON(w, r, &body); err != nil {
		writeBodyError(w, err)
		return
	}
	req, err := ValidateRequest(body.FQDN, body.DNSSans, body.IPSans)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	result, err := s.db.Generate(r.Context(), req, s.opts.DefaultValidityDays, s.opts.ExpiryWarnDays)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	response.WriteJSON(w, http.StatusCreated, issueResponseFrom(result))
}

func (s *Server) handleCertGet(w http.ResponseWriter, r *http.Request) {
	id, err := parseCertID(r)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	c, err := s.db.GetCert(r.Context(), id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	response.WriteJSON(w, http.StatusOK, c)
}

func (s *Server) handleCertDelete(w http.ResponseWriter, r *http.Request) {
	id, err := parseCertID(r)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	confirm := r.URL.Query().Get("confirm")
	if confirm == "" {
		response.WriteError(w, http.StatusBadRequest, "confirm query parameter is required")
		return
	}
	if err := s.db.Delete(r.Context(), id, confirm); err != nil {
		writeStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleCertRenew(w http.ResponseWriter, r *http.Request) {
	id, err := parseCertID(r)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	result, err := s.db.Renew(r.Context(), id, s.opts.DefaultValidityDays, s.opts.ExpiryWarnDays)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	response.WriteJSON(w, http.StatusCreated, issueResponseFrom(result))
}

// pemContentType and tgzContentType are the two Content-Type values every
// download route uses.
const (
	pemContentType = "application/x-pem-file"
	tgzContentType = "application/gzip"
)

// writeDownload writes data as an attachment named filename. It is the one
// place any of the download routes touches the response body, so the
// Content-Disposition convention (FR-7: always derived from the row's fqdn
// via SafeFilename, or the literal "rootCA.crt") lives in exactly one spot.
func writeDownload(w http.ResponseWriter, filename, contentType string, data []byte) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	// Every route through here serves private key material (key.pem,
	// haproxy.pem, the bundle) or the root certificate. None of it belongs in
	// a shared proxy cache or a browser's on-disk cache after a renew has
	// superseded it.
	w.Header().Set("Cache-Control", "no-store, max-age=0")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// handleCertFileGet serves GET /api/certs/{id}/files/{name}. {name} is a
// closed three-entry lookup (cert.pem, key.pem, haproxy.pem) -- anything
// else, including a traversal attempt or "rootCA.key", is a 404: this is a
// validation gate, not a structural guarantee (Principle 2), and it is what
// makes rootCA.key permanently unservable through this route.
func (s *Server) handleCertFileGet(w http.ResponseWriter, r *http.Request) {
	id, err := parseCertID(r)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	name := r.PathValue("name")
	ctx := r.Context()

	switch name {
	case "cert.pem":
		c, err := s.db.GetCert(ctx, id)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		if c.CertPEM == nil {
			response.WriteError(w, http.StatusNotFound, "not found")
			return
		}
		writeDownload(w, SafeFilename(c.FQDN)+".cert.pem", pemContentType, []byte(*c.CertPEM))
	case "key.pem":
		c, err := s.db.getCertWithKey(ctx, id)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		if c.KeyPEM == nil {
			response.WriteError(w, http.StatusNotFound, "not found")
			return
		}
		writeDownload(w, SafeFilename(c.FQDN)+".key.pem", pemContentType, []byte(*c.KeyPEM))
	case "haproxy.pem":
		c, ca, err := s.certAndCA(ctx, id)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		data, err := AssembleHAProxyPEM(*c, []byte(ca.CertPEM))
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeDownload(w, SafeFilename(c.FQDN)+".haproxy.pem", pemContentType, data)
	default:
		response.WriteError(w, http.StatusNotFound, "not found")
	}
}

func (s *Server) handleCertBundleGet(w http.ResponseWriter, r *http.Request) {
	id, err := parseCertID(r)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	c, ca, err := s.certAndCA(r.Context(), id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	data, err := BundleTGZ(*c, []byte(ca.CertPEM))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeDownload(w, SafeFilename(c.FQDN)+".tgz", tgzContentType, data)
}

// certAndCA fetches both the cert (with its key) and the stored CA -- the
// pair haproxy.pem and bundle downloads both need before they can call
// AssembleHAProxyPEM/BundleTGZ.
func (s *Server) certAndCA(ctx context.Context, id int64) (*Cert, *CA, error) {
	c, err := s.db.getCertWithKey(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	ca, err := s.db.GetCA(ctx)
	if err != nil {
		return nil, nil, err
	}
	return c, ca, nil
}

// legacyImportUnavailable returns the reason the two import routes cannot
// run, or "" when they can. Both routes gate on this before touching the
// store: with the shipped default (legacy_import_dir: ""), Preview's
// os.ReadFile("rootCA.crt") resolves against the *process working directory*,
// so an unconfigured deployment answered a generic 500 whose body deliberately
// hides the cause -- indistinguishable from a real internal failure. The
// configured-but-unusable case (legacyImportReason, set at build time by
// checkLegacyImportDir) is the same class of answer and gets the same 409.
func (s *Server) legacyImportUnavailable() string {
	if s.legacyImportReason != "" {
		return s.legacyImportReason
	}
	if s.opts.LegacyImportDir == "" {
		return "legacy_import_dir is not configured"
	}
	return ""
}

func (s *Server) handleImportPreview(w http.ResponseWriter, r *http.Request) {
	if reason := s.legacyImportUnavailable(); reason != "" {
		response.WriteError(w, http.StatusConflict, "legacy import is unavailable: "+reason)
		return
	}
	report, err := s.db.Preview(r.Context(), s.opts.LegacyImportDir)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	response.WriteJSON(w, http.StatusOK, report)
}

// importRequest is POST /api/import's body. confirmNonEmpty is optional and
// defaults to false -- an empty body is treated identically to `{}`.
type importRequest struct {
	ConfirmNonEmpty bool `json:"confirmNonEmpty"`
}

// importErrorResponse is POST /api/import's failure body. Execute
// deliberately returns its partial ImportReport alongside an error (see its
// doc comment), and that report is the only thing that names which of
// (possibly) 200 legacy directories was the problem -- dropping it left the
// operator with a bare sentence and no way to find the bad one. Report is
// omitted when Execute failed before classifying anything (a missing root, an
// unconfirmed re-run).
type importErrorResponse struct {
	Error  string        `json:"error"`
	Report *ImportReport `json:"report,omitempty"`
}

func (s *Server) handleImportExecute(w http.ResponseWriter, r *http.Request) {
	if reason := s.legacyImportUnavailable(); reason != "" {
		response.WriteError(w, http.StatusConflict, "legacy import is unavailable: "+reason)
		return
	}
	var body importRequest
	if err := decodeOptionalJSON(w, r, &body); err != nil {
		writeBodyError(w, err)
		return
	}
	report, err := s.db.Execute(r.Context(), s.opts.LegacyImportDir, body.ConfirmNonEmpty)
	if err != nil {
		status, msg := storeErrorStatus(err)
		response.WriteJSON(w, status, importErrorResponse{Error: msg, Report: report})
		return
	}
	response.WriteJSON(w, http.StatusOK, report)
}

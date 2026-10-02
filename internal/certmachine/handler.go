// handler.go wires the certmachine HTTP API onto Server.mux: the slice 8
// doc's routes (plus POST /api/ca/trust, the device-trust button), their
// method-less 405 fallthroughs, and the single sentinel-to-status mapping
// every handler funnels errors through.
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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"cmd184psu/unified-webapp/internal/platform/response"
	"cmd184psu/unified-webapp/internal/platform/sshclient"
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
	s.mux.HandleFunc("POST /api/ca/trust", s.handleCATrust)
	s.mux.HandleFunc("POST /api/ca/trust/remote", s.handleCATrustRemote)
	s.mux.HandleFunc("POST /api/ca/replace", s.handleCAReplace)
	s.mux.HandleFunc("POST /api/ca/switch-back", s.handleCASwitchBack)
	s.mux.HandleFunc("GET /api/ssh/keys", s.handleSSHKeys)

	s.mux.HandleFunc("GET /api/certs", s.handleCertsGet)
	s.mux.HandleFunc("POST /api/certs", s.handleCertsPost)
	s.mux.HandleFunc("GET /api/certs/{id}", s.handleCertGet)
	s.mux.HandleFunc("DELETE /api/certs/{id}", s.handleCertDelete)
	s.mux.HandleFunc("POST /api/certs/{id}/renew", s.handleCertRenew)
	s.mux.HandleFunc("POST /api/certs/{id}/edit", s.handleCertEdit)
	s.mux.HandleFunc("GET /api/certs/{id}/files/{name}", s.handleCertFileGet)
	s.mux.HandleFunc("GET /api/certs/{id}/bundle", s.handleCertBundleGet)

	s.mux.HandleFunc("GET /api/import/preview", s.handleImportPreview)
	s.mux.HandleFunc("POST /api/import", s.handleImportExecute)

	// Method-less fallthroughs: the only paths carrying a mutating method.
	// Each Allow string is hand-maintained and asserted against these exact
	// registrations by TestAllowHeadersMatchRegisteredRoutes.
	s.mux.HandleFunc("/api/ca/init", methodNotAllowed("POST"))
	s.mux.HandleFunc("/api/ca/trust", methodNotAllowed("POST"))
	s.mux.HandleFunc("/api/ca/trust/remote", methodNotAllowed("POST"))
	s.mux.HandleFunc("/api/ca/replace", methodNotAllowed("POST"))
	s.mux.HandleFunc("/api/ca/switch-back", methodNotAllowed("POST"))
	s.mux.HandleFunc("/api/certs", methodNotAllowed("GET, POST"))
	s.mux.HandleFunc("/api/certs/{id}", methodNotAllowed("GET, DELETE"))
	s.mux.HandleFunc("/api/certs/{id}/renew", methodNotAllowed("POST"))
	s.mux.HandleFunc("/api/certs/{id}/edit", methodNotAllowed("POST"))
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
	if _, err := s.db.GetCurrentCA(ctx); err == nil {
		caStatus = "present"
	}
	previousStatus := "absent"
	if previous, err := s.db.GetPreviousCA(ctx); err == nil && previous != nil {
		previousStatus = "present"
	}
	legacyDir := s.opts.LegacyImportDir
	legacyStatus := "ok"
	if legacyDir == "" {
		legacyDir = "none"
		legacyStatus = "n/a"
	} else if s.legacyImportReason != "" {
		legacyStatus = s.legacyImportReason
	}
	log.Printf("certmachine: db=%s schema=v%d certs=%d ca=%s previous_ca=%s legacy_import=%s(%s)",
		s.opts.DBPath, schemaVersion, count, caStatus, previousStatus, legacyDir, legacyStatus)
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
		errors.Is(err, ErrCAFingerprintMismatch),
		// certAndCA's FR-R3 refusal (a NULL ca_id, "unknown signer"): a
		// well-formed request against a row that exists but cannot be
		// resolved to a signer, same class as the two mismatches above.
		errors.Is(err, ErrUnknownSigner),
		// Replace/SwitchBack's own conflicts (the CA-replacement plan,
		// US-004): each is a well-formed request against a state it cannot
		// proceed against, not an internal failure.
		errors.Is(err, ErrNoPreviousCA),
		errors.Is(err, ErrPreviousStaleChoiceRequired),
		errors.Is(err, ErrConcurrentChange):
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
	TrustDeviceAvailable  bool   `json:"trustDeviceAvailable"`
	TrustPlatform         string `json:"trustPlatform,omitempty"`
	// TrustRemoteAvailable: the CA can be trusted on another machine over SSH.
	TrustRemoteAvailable bool   `json:"trustRemoteAvailable"`
	TrustRemoteReason    string `json:"trustRemoteReason,omitempty"`
}

func (s *Server) handleConfigGet(w http.ResponseWriter, r *http.Request) {
	count, err := s.db.CountCerts(r.Context())
	if err != nil {
		writeStoreError(w, err)
		return
	}
	// The platform is only reported when the feature is enabled: an operator
	// who left trust_device_enabled at its default false has no reason to
	// see this box's OS reflected back at them from every module's config
	// response, and DetectTrustPlatform runs an /etc/os-release read for it.
	var trustPlatform string
	if s.opts.TrustDeviceEnabled {
		if platform, err := DetectTrustPlatform(); err == nil {
			trustPlatform = string(platform)
		}
	}
	response.WriteJSON(w, http.StatusOK, configResponse{
		DefaultValidityDays:   s.opts.DefaultValidityDays,
		ExpiryWarnDays:        s.opts.ExpiryWarnDays,
		CertCount:             count,
		LegacyImportAvailable: s.opts.LegacyImportDir != "" && s.legacyImportReason == "",
		LegacyImportDir:       s.opts.LegacyImportDir,
		LegacyImportReason:    s.legacyImportReason,
		TrustDeviceAvailable:  s.opts.TrustDeviceEnabled && trustPlatform != "",
		TrustPlatform:         trustPlatform,
		TrustRemoteAvailable:  s.opts.SSH != nil,
		TrustRemoteReason:     s.opts.SSHReason,
	})
}

// caResponse is GET /api/ca and POST /api/ca/init's body. Exists is always
// present; the rest are omitted (an absent CA has no subject/serial/etc. to
// report) when Exists is false. Id, Previous and UnknownSignerActiveCount
// are the CA-replacement plan's additions (§4, US-005): Previous is omitted
// entirely when there is no previous CA, and UnknownSignerActiveCount always
// reports (even 0), since the Replace dialog needs it unconditionally.
type caResponse struct {
	Exists                   bool                `json:"exists"`
	ID                       int64               `json:"id,omitempty"`
	Subject                  string              `json:"subject,omitempty"`
	Serial                   string              `json:"serial,omitempty"`
	NotBefore                string              `json:"notBefore,omitempty"`
	NotAfter                 string              `json:"notAfter,omitempty"`
	Fingerprint              string              `json:"fingerprint,omitempty"`
	ImportedFrom             *string             `json:"importedFrom,omitempty"`
	Previous                 *previousCAResponse `json:"previous,omitempty"`
	UnknownSignerActiveCount int                 `json:"unknownSignerActiveCount"`
}

// previousCAResponse is caResponse's "previous" field: the previous CA's
// identity plus how many active rows still depend on it (FR-R7, D7) -- the
// count that drives both the panel's display and POST /api/ca/replace's
// ErrPreviousStaleChoiceRequired 409.
type previousCAResponse struct {
	ID          int64  `json:"id"`
	Subject     string `json:"subject"`
	NotBefore   string `json:"notBefore"`
	NotAfter    string `json:"notAfter"`
	Fingerprint string `json:"fingerprint"`
	ActiveCount int    `json:"activeCount"`
}

func caResponseFrom(ca *CA) caResponse {
	return caResponse{
		Exists:       true,
		ID:           ca.ID,
		Subject:      ca.Subject,
		Serial:       ca.Serial,
		NotBefore:    ca.NotBefore,
		NotAfter:     ca.NotAfter,
		Fingerprint:  ca.Fingerprint,
		ImportedFrom: ca.ImportedFrom,
	}
}

// buildCAResponse assembles GET /api/ca's full body: the current CA (already
// known to exist -- callers check ErrCANotFound first), plus the previous CA
// block (omitted when there is none) and the top-level unknown-signer count.
func (s *Server) buildCAResponse(ctx context.Context, ca *CA) (caResponse, error) {
	resp := caResponseFrom(ca)
	unknownCount, err := s.db.countActiveUnknownSigner(ctx)
	if err != nil {
		return caResponse{}, err
	}
	resp.UnknownSignerActiveCount = unknownCount

	previous, err := s.db.GetPreviousCA(ctx)
	if err != nil {
		return caResponse{}, err
	}
	if previous != nil {
		activeCount, err := s.db.countActiveByCA(ctx, previous.ID)
		if err != nil {
			return caResponse{}, err
		}
		resp.Previous = &previousCAResponse{
			ID:          previous.ID,
			Subject:     previous.Subject,
			NotBefore:   previous.NotBefore,
			NotAfter:    previous.NotAfter,
			Fingerprint: previous.Fingerprint,
			ActiveCount: activeCount,
		}
	}
	return resp, nil
}

func (s *Server) handleCAGet(w http.ResponseWriter, r *http.Request) {
	ca, err := s.db.GetCurrentCA(r.Context())
	if err != nil {
		if errors.Is(err, ErrCANotFound) {
			response.WriteJSON(w, http.StatusOK, caResponse{Exists: false})
			return
		}
		writeStoreError(w, err)
		return
	}
	resp, err := s.buildCAResponse(r.Context(), ca)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	response.WriteJSON(w, http.StatusOK, resp)
}

// caReplaceRequest is POST /api/ca/replace's body (§4). PreviousStale is a
// pointer -- decoded as *string, never defaulted to "" -- because P4
// distinguishes "omitted" (ErrPreviousStaleChoiceRequired when the outgoing
// previous CA still signs an active row) from any string value.
type caReplaceRequest struct {
	Name          string  `json:"name"`
	Existing      string  `json:"existing"`
	PreviousStale *string `json:"previousStale"`
}

// caReplaceResponse is POST /api/ca/replace's 200 body: the post-replace CA
// (GET /api/ca shape) plus ReplaceResult's counts.
type caReplaceResponse struct {
	CA              caResponse `json:"ca"`
	Reissued        int        `json:"reissued"`
	Deleted         int        `json:"deleted"`
	Kept            int        `json:"kept"`
	Clamped         int        `json:"clamped"`
	PreviousDropped bool       `json:"previousDropped"`
}

// ndjsonAccept is the Accept header value that switches POST /api/ca/replace
// from its default JSON response into the streaming NDJSON progress mode (§
// "real progress bar" -- owner feedback that a blanket re-issue of dozens of
// certs gave no sign anything was happening).
const ndjsonAccept = "application/x-ndjson"

func (s *Server) handleCAReplace(w http.ResponseWriter, r *http.Request) {
	var body caReplaceRequest
	if err := decodeOptionalJSON(w, r, &body); err != nil {
		writeBodyError(w, err)
		return
	}
	if strings.Contains(r.Header.Get("Accept"), ndjsonAccept) {
		s.handleCAReplaceStream(w, r, body)
		return
	}
	result, err := s.db.ReplaceCA(r.Context(), body.Name, body.Existing, body.PreviousStale, s.opts.DefaultValidityDays, s.opts.ExpiryWarnDays)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	ca, err := s.db.GetCurrentCA(r.Context())
	if err != nil {
		writeStoreError(w, err)
		return
	}
	caResp, err := s.buildCAResponse(r.Context(), ca)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	response.WriteJSON(w, http.StatusOK, caReplaceResponse{
		CA:              caResp,
		Reissued:        result.Reissued,
		Deleted:         result.Deleted,
		Kept:            result.Kept,
		Clamped:         result.Clamped,
		PreviousDropped: result.PreviousDropped,
	})
}

// ndjsonProgressLine is one "progress" line of the NDJSON stream.
type ndjsonProgressLine struct {
	Type  string `json:"type"`
	Phase string `json:"phase"`
	Done  int    `json:"done,omitempty"`
	Total int    `json:"total,omitempty"`
}

// ndjsonResultLine is the stream's final line on success: caReplaceResponse's
// same fields, plus the "result" discriminator.
type ndjsonResultLine struct {
	Type            string     `json:"type"`
	CA              caResponse `json:"ca"`
	Reissued        int        `json:"reissued"`
	Deleted         int        `json:"deleted"`
	Kept            int        `json:"kept"`
	Clamped         int        `json:"clamped"`
	PreviousDropped bool       `json:"previousDropped"`
}

// ndjsonErrorLine is the stream's final line on failure, once streaming has
// already started (an earlier failure -- 400 validation, 409
// previousStale-required, etc. -- is answered exactly as the non-streaming
// path does, via writeStoreError, since no line has been written yet). Error
// is the same client-safe message writeStoreError would send: it goes
// through storeErrorStatus, never err.Error() directly, so internal detail
// (SQL text, file paths, PEM material) never leaks through a mid-stream
// failure any more than it does through a 500.
type ndjsonErrorLine struct {
	Type   string `json:"type"`
	Status int    `json:"status"`
	Error  string `json:"error"`
}

// handleCAReplaceStream is handleCAReplace's NDJSON path: headers (200,
// Content-Type application/x-ndjson, Cache-Control no-store) are written
// lazily on the first line this writes, via ndjsonWriter.write, so a failure
// before any progress event (validation, P4's 409, etc.) still reaches the
// client as a normal writeStoreError response. Every write is flushed
// immediately through http.ResponseController -- certmachine's own handler
// chain (stripCORS, and the platform's Gate/BodyLimit/CORS middleware
// upstream of it in cmd/server) never wraps the ResponseWriter, so the
// underlying *http.response's Flusher reaches this call unobstructed.
func (s *Server) handleCAReplaceStream(w http.ResponseWriter, r *http.Request, body caReplaceRequest) {
	ctx := r.Context()
	nw := &ndjsonWriter{w: w, rc: http.NewResponseController(w)}

	progress := func(ev ReplaceProgress) {
		nw.write(ndjsonProgressLine{Type: "progress", Phase: string(ev.Phase), Done: ev.Done, Total: ev.Total})
	}

	result, err := s.db.ReplaceCA(ctx, body.Name, body.Existing, body.PreviousStale,
		s.opts.DefaultValidityDays, s.opts.ExpiryWarnDays, WithReplaceProgress(progress))
	if err != nil {
		nw.writeError(w, err)
		return
	}

	ca, err := s.db.GetCurrentCA(ctx)
	if err != nil {
		nw.writeError(w, err)
		return
	}
	caResp, err := s.buildCAResponse(ctx, ca)
	if err != nil {
		nw.writeError(w, err)
		return
	}

	nw.write(ndjsonResultLine{
		Type:            "result",
		CA:              caResp,
		Reissued:        result.Reissued,
		Deleted:         result.Deleted,
		Kept:            result.Kept,
		Clamped:         result.Clamped,
		PreviousDropped: result.PreviousDropped,
	})
}

// ndjsonWriter lazily commits the streaming response's headers on its first
// line, then writes and flushes every subsequent line immediately.
type ndjsonWriter struct {
	w       http.ResponseWriter
	rc      *http.ResponseController
	started bool
}

func (nw *ndjsonWriter) write(v any) {
	if !nw.started {
		nw.w.Header().Set("Content-Type", ndjsonAccept)
		nw.w.Header().Set("Cache-Control", "no-store")
		// nginx buffers proxied responses by default (docs/multissh.md:79-85
		// notes the same nginx-fronts-this-app deployment), which would hold
		// every line until the buffer fills or the response ends -- defeating
		// the whole point of streaming progress. This tells nginx to pass
		// each write straight through.
		nw.w.Header().Set("X-Accel-Buffering", "no")
		nw.w.WriteHeader(http.StatusOK)
		nw.started = true
	}
	_ = json.NewEncoder(nw.w).Encode(v)
	_ = nw.rc.Flush()
}

// writeError answers err either as a normal (non-streamed) writeStoreError
// response, when nothing has been written to w yet, or as the stream's final
// "error" line once streaming has already started.
func (nw *ndjsonWriter) writeError(w http.ResponseWriter, err error) {
	if !nw.started {
		writeStoreError(w, err)
		return
	}
	status, msg := storeErrorStatus(err)
	nw.write(ndjsonErrorLine{Type: "error", Status: status, Error: msg})
}

// caSwitchBackResponse is POST /api/ca/switch-back's 200 body.
type caSwitchBackResponse struct {
	CA caResponse `json:"ca"`
}

func (s *Server) handleCASwitchBack(w http.ResponseWriter, r *http.Request) {
	if _, err := s.db.SwitchBack(r.Context(), s.opts.ExpiryWarnDays); err != nil {
		writeStoreError(w, err)
		return
	}
	ca, err := s.db.GetCurrentCA(r.Context())
	if err != nil {
		writeStoreError(w, err)
		return
	}
	caResp, err := s.buildCAResponse(r.Context(), ca)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	response.WriteJSON(w, http.StatusOK, caSwitchBackResponse{CA: caResp})
}

// handleCAInit creates the root CA. An optional JSON body {"name": "..."}
// sets its name (Common Name); without one it's DefaultCAName.
func (s *Server) handleCAInit(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req struct {
		Name string `json:"name"`
	}
	if r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			response.WriteError(w, http.StatusBadRequest, "invalid request body")
			return
		}
	}
	name, err := NormalizeCAName(req.Name)
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.db.InitNamedCA(ctx, s.opts.LegacyImportDir, name); err != nil {
		writeStoreError(w, err)
		return
	}
	ca, err := s.db.GetCurrentCA(ctx)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	response.WriteJSON(w, http.StatusCreated, caResponseFrom(ca))
}

func (s *Server) handleCARootGet(w http.ResponseWriter, r *http.Request) {
	ca, err := s.db.GetCurrentCA(r.Context())
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeDownload(w, CAFileName(ca.Subject), pemContentType, []byte(ca.CertPEM))
}

// trustResponse is POST /api/ca/trust's body, on both success and failure:
// Output always carries the ran commands' combined stdout+stderr (empty only
// when the request never got as far as running one, e.g. the feature is
// disabled or no CA exists yet) so a sudo refusal is visible to whoever
// clicked the button, not just the server log.
type trustResponse struct {
	Platform string `json:"platform,omitempty"`
	Output   string `json:"output,omitempty"`
	Error    string `json:"error,omitempty"`
}

// handleCATrust runs InstallTrust against the current root CA -- the server
// side of the "Trust this CA on this device" button. See trust.go's package
// doc for exactly what runs and why every command is `sudo -n`.
func (s *Server) handleCATrust(w http.ResponseWriter, r *http.Request) {
	if !s.opts.TrustDeviceEnabled {
		response.WriteJSON(w, http.StatusConflict, trustResponse{
			Error: "device trust install is unavailable: certmachine.trust_device_enabled is not set",
		})
		return
	}
	ca, err := s.db.GetCurrentCA(r.Context())
	if err != nil {
		writeStoreError(w, err)
		return
	}
	platform, platErr := DetectTrustPlatform()
	output, err := InstallTrust(r.Context(), []byte(ca.CertPEM), trustAnchorName(ca.Subject))
	if err != nil {
		if platErr != nil {
			log.Printf("certmachine: device trust install unsupported: %v", err)
		} else {
			log.Printf("certmachine: device trust install failed: %v", err)
		}
		response.WriteJSON(w, http.StatusConflict, trustResponse{
			Platform: string(platform),
			Output:   output,
			Error:    err.Error(),
		})
		return
	}
	response.WriteJSON(w, http.StatusOK, trustResponse{
		Platform: string(platform),
		Output:   output,
	})
}

// trustRemoteRequest is POST /api/ca/trust/remote's body. Key is a file name
// picked from the server's SSH key folder (never a path); exactly one of Key
// and Password is used.
type trustRemoteRequest struct {
	Host     string           `json:"host"`
	Port     int              `json:"port"`
	User     string           `json:"user"`
	Key      string           `json:"key"`
	Password sshclient.Secret `json:"password"`
}

// handleCATrustRemote installs the root CA into another machine's trust store
// over SSH (see remotetrust.go). Every attempt is logged with its target and
// outcome, never its credential.
func (s *Server) handleCATrustRemote(w http.ResponseWriter, r *http.Request) {
	if s.opts.SSH == nil {
		response.WriteJSON(w, http.StatusConflict, trustResponse{Error: "remote trust is unavailable: " + s.opts.SSHReason})
		return
	}
	var req trustRemoteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteJSON(w, http.StatusBadRequest, trustResponse{Error: "invalid request body"})
		return
	}
	defer req.Password.Zero()
	req.Host = strings.TrimSpace(req.Host)
	req.User = strings.TrimSpace(req.User)
	if req.Host == "" || req.User == "" {
		response.WriteJSON(w, http.StatusBadRequest, trustResponse{Error: "host and user are required"})
		return
	}
	creds := sshclient.Credentials{Host: req.Host, Port: req.Port, User: req.User, Password: req.Password}
	if strings.TrimSpace(req.Key) != "" {
		keyPath, err := sshclient.ResolveKeyPath(s.opts.SSH.SSHDir, req.Key)
		if err != nil {
			response.WriteJSON(w, http.StatusBadRequest, trustResponse{Error: "unknown SSH key"})
			return
		}
		creds.KeyPath = keyPath
	}
	if _, err := creds.AuthMethods(); errors.Is(err, sshclient.ErrCredential) {
		response.WriteJSON(w, http.StatusBadRequest, trustResponse{Error: "choose an SSH key or enter a password (not both)"})
		return
	}

	ca, err := s.db.GetCurrentCA(r.Context())
	if err != nil {
		writeStoreError(w, err)
		return
	}
	platform, output, err := InstallTrustRemote(r.Context(), creds, s.opts.SSH.HostKeyCallback, []byte(ca.CertPEM), trustAnchorName(ca.Subject))
	target := fmt.Sprintf("%s@%s", req.User, creds.Addr())
	switch {
	case err == nil:
		log.Printf("certmachine: remote trust %s platform=%s outcome=ok", target, platform)
		response.WriteJSON(w, http.StatusOK, trustResponse{Platform: string(platform), Output: output})
	case errors.Is(err, ErrTrustPlatformUnsupported):
		log.Printf("certmachine: remote trust %s outcome=unsupported: %v", target, err)
		response.WriteJSON(w, http.StatusUnprocessableEntity, trustResponse{
			Output: output,
			Error:  "not a supported operating system (macOS, Windows, Rocky/RHEL, Ubuntu/Debian); nothing was installed. " + err.Error(),
		})
	case output == "" && platform == "":
		// Nothing ran: the SSH connection itself failed.
		log.Printf("certmachine: remote trust %s outcome=connect-failed: %v", target, err)
		response.WriteJSON(w, http.StatusBadGateway, trustResponse{Error: err.Error()})
	default:
		log.Printf("certmachine: remote trust %s platform=%s outcome=failed: %v", target, platform, err)
		response.WriteJSON(w, http.StatusConflict, trustResponse{Platform: string(platform), Output: output, Error: err.Error()})
	}
}

// handleSSHKeys lists the server's SSH key folder for the remote-trust
// dialog's key picker (names only; paths never leave the server).
func (s *Server) handleSSHKeys(w http.ResponseWriter, r *http.Request) {
	if s.opts.SSH == nil {
		response.WriteJSON(w, http.StatusOK, map[string]any{"keys": []sshclient.KeyFile{}})
		return
	}
	keys, err := sshclient.ListKeys(s.opts.SSH.SSHDir)
	if err != nil {
		keys = []sshclient.KeyFile{}
	}
	response.WriteJSON(w, http.StatusOK, map[string]any{"keys": keys})
}

func (s *Server) handleCertsGet(w http.ResponseWriter, r *http.Request) {
	certs, err := s.db.ListCerts(r.Context())
	if err != nil {
		writeStoreError(w, err)
		return
	}
	q := r.URL.Query()
	fqdn, status := q.Get("fqdn"), q.Get("status")
	switch status {
	case "", "active", "archived", "quarantined":
	default:
		response.WriteError(w, http.StatusBadRequest, "status must be one of active, archived, quarantined")
		return
	}
	if fqdn != "" || status != "" {
		filtered := make([]Cert, 0, len(certs))
		for _, c := range certs {
			if fqdn != "" && !strings.EqualFold(c.FQDN, fqdn) {
				continue
			}
			if status != "" && c.Status != status {
				continue
			}
			filtered = append(filtered, c)
		}
		certs = filtered
	}
	response.WriteJSON(w, http.StatusOK, map[string]any{"certs": certs})
}

// generateRequest is POST /api/certs's body.
type generateRequest struct {
	FQDN    string   `json:"fqdn"`
	DNSSans []string `json:"dnsSans"`
	IPSans  []string `json:"ipSans"`
}

// issueResponse is the shared body for POST /api/certs,
// POST /api/certs/{id}/renew, and POST /api/certs/{id}/edit: the newly
// issued row plus the FR-4 validity clamp fact. RequestedNotAfter is only
// present when ValidityClamped is true -- when it isn't, it would just
// repeat Cert.NotAfter. PreviousDropped (the CA-replacement plan, §4) is
// always present: Renew, Edit and Delete can all trigger dropPreviousIfUnusedTx,
// so a caller cannot infer it from the route alone.
type issueResponse struct {
	Cert              Cert   `json:"cert"`
	ValidityClamped   bool   `json:"validityClamped"`
	RequestedNotAfter string `json:"requestedNotAfter,omitempty"`
	PreviousDropped   bool   `json:"previousDropped"`
}

func issueResponseFrom(result IssueResult) issueResponse {
	resp := issueResponse{Cert: result.Cert, ValidityClamped: result.Clamped, PreviousDropped: result.PreviousDropped}
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

// deleteResponse is DELETE /api/certs/{id}'s 200 body (the CA-replacement
// plan, §4: 204 with no body becomes 200 carrying previousDropped, since
// Delete can trigger dropPreviousIfUnusedTx same as Renew and Edit).
type deleteResponse struct {
	PreviousDropped bool `json:"previousDropped"`
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
	dropped, err := s.db.Delete(r.Context(), id, confirm)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	response.WriteJSON(w, http.StatusOK, deleteResponse{PreviousDropped: dropped})
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

// editRequest is POST /api/certs/{id}/edit's body (§4, P5): the same shape
// generateRequest carries plus ValidityDays, the one field Edit accepts that
// no other issuance route does (the FR-4 narrowing).
type editRequest struct {
	FQDN         string   `json:"fqdn"`
	DNSSans      []string `json:"dnsSans"`
	IPSans       []string `json:"ipSans"`
	ValidityDays int      `json:"validityDays"`
}

func (s *Server) handleCertEdit(w http.ResponseWriter, r *http.Request) {
	id, err := parseCertID(r)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	var body editRequest
	if err := decodeOptionalJSON(w, r, &body); err != nil {
		writeBodyError(w, err)
		return
	}
	req, err := ValidateRequest(body.FQDN, body.DNSSans, body.IPSans)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	result, err := s.db.Edit(r.Context(), id, req, body.ValidityDays, s.opts.ExpiryWarnDays)
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

// writeVerifiedDownload is writeDownload plus the integrity contract of the
// three per-cert file downloads: a strong ETag of the exact body and the
// X-Cert-Id / X-Cert-Fingerprint identity headers. It is local to
// handleCertFileGet on purpose -- the shared writeDownload also serves the
// CA root (no cert id) and the tgz bundle (embeds time.Now, so no stable
// ETag).
func (s *Server) writeVerifiedDownload(w http.ResponseWriter, r *http.Request, c *Cert, filename string, data []byte) {
	if c.Fingerprint == nil || *c.Fingerprint == "" {
		log.Printf("certmachine: cert %d has no fingerprint; refusing download", c.ID)
		response.WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}
	sum := sha256.Sum256(data)
	etag := `"sha256-` + hex.EncodeToString(sum[:]) + `"`
	w.Header().Set("ETag", etag)
	w.Header().Set("X-Cert-Id", strconv.FormatInt(c.ID, 10))
	w.Header().Set("X-Cert-Fingerprint", *c.Fingerprint)
	if ifNoneMatchHits(r.Header.Get("If-None-Match"), etag) {
		w.Header().Set("Cache-Control", "no-store, max-age=0")
		w.WriteHeader(http.StatusNotModified)
		return
	}
	writeDownload(w, filename, pemContentType, data)
}

// ifNoneMatchHits reports whether an If-None-Match header value matches
// etag: "*", or any entry of a comma-separated list, compared weakly (a
// W/ prefix is ignored, RFC 9110 13.1.2).
func ifNoneMatchHits(header, etag string) bool {
	for _, part := range strings.Split(header, ",") {
		part = strings.TrimSpace(part)
		if part == "*" || strings.TrimPrefix(part, "W/") == etag {
			return true
		}
	}
	return false
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
		s.writeVerifiedDownload(w, r, c, SafeFilename(c.FQDN)+".cert.pem", []byte(*c.CertPEM))
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
		s.writeVerifiedDownload(w, r, c, SafeFilename(c.FQDN)+".key.pem", []byte(*c.KeyPEM))
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
		s.writeVerifiedDownload(w, r, c, SafeFilename(c.FQDN)+".haproxy.pem", data)
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
	data, err := BundleTGZ(*c, []byte(ca.CertPEM), CAFileName(ca.Subject))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeDownload(w, SafeFilename(c.FQDN)+".tgz", tgzContentType, data)
}

// certAndCA fetches both the cert (with its key) and its recorded signer --
// the pair haproxy.pem and bundle downloads both need before they can call
// AssembleHAProxyPEM/BundleTGZ. It resolves the signer by the cert's own
// ca_id (GetCAByID), never GetCurrentCA (FR-R3): a stale cert's haproxy.pem
// and bundle must embed the CA that actually signed it, not whichever CA is
// current now. A NULL ca_id (P1, "unknown signer") is ErrUnknownSigner --
// except on a quarantined row, whose ca_id is never meaningful (P1) and
// whose real refusal is checkDownloadable's ErrQuarantinedDownload, naming
// the stored quarantine reason. That check runs inside
// AssembleHAProxyPEM/BundleTGZ, before either PEM argument is ever read, so
// the placeholder *CA returned here is never dereferenced.
func (s *Server) certAndCA(ctx context.Context, id int64) (*Cert, *CA, error) {
	c, err := s.db.getCertWithKey(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	if c.Status == StatusQuarantined {
		return c, &CA{}, nil
	}
	if c.CAID == nil {
		return nil, nil, ErrUnknownSigner
	}
	ca, err := s.db.GetCAByID(ctx, *c.CAID)
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

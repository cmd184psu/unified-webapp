package admin

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"

	"github.com/go-ldap/ldap/v3"

	"cmd184psu/unified-webapp/internal/platform/auth"
	"cmd184psu/unified-webapp/internal/platform/config"
	"cmd184psu/unified-webapp/internal/platform/response"
)

// Handler serves all admin module HTTP routes. T5.1 was a scaffold with no
// routes; T5.4 adds the FR-M1/FR-M5 data API below -- module matrix, API-key,
// LDAP-test, and session settings -- every mutating route funneling through
// applyAuth (T5.3: validate -> splice the config file -> swap the live
// Policy). No route here does any authentication or authorization of the
// request itself: the dispatcher wraps this Handler in svc.Gate("admin",
// ...) before it ever sees a request (cmd/server/main.go buildDispatcher),
// so every route below is reached only once the caller is already an
// authenticated admin. Passkey management is not duplicated here -- the
// gate's own FR-A8 routes (/api/auth/passkey/..., /api/auth/passkeys...)
// already cover register/list/delete for every protected module, admin
// included.
//
// authConfig/mu: applyAuth's inputs (a config.AuthConfig) and its swap
// target (the live auth.Service) are stateless from this Handler's point of
// view -- applyAuth always needs the *current* full auth config to build a
// candidate from (e.g. adding one API key must not silently drop every
// other key, module entry, and LDAP/session setting already configured), so
// the Handler tracks its own copy, seeded at construction from the config
// file's "auth" section (NewHandler's authConfig parameter) and updated only
// after a mutation's applyAuth call actually succeeds. mu serializes the
// entire read-current -> build-candidate -> applyAuth -> update-stored-copy
// sequence for every mutating route (mutateAuth, below), so two concurrent
// admin requests (e.g. two POST /api/keys for different names) can never
// race a lost update against each other -- without it, both could read the
// same starting APIKeys slice, and whichever applyAuth/store finished last
// would silently discard the other's addition.
type Handler struct {
	cfg  config.AdminConfig
	deps Deps

	mu         sync.Mutex
	authConfig config.AuthConfig
}

// NewHandler constructs a Handler. authConfig is the current auth.* config
// (normally cfg.Auth from the same *config.Config Build was given) --
// Handler's own copy of it, mutated only through applyAuth via mutateAuth.
func NewHandler(cfg config.AdminConfig, authConfig config.AuthConfig, deps Deps) *Handler {
	return &Handler{cfg: cfg, deps: deps, authConfig: authConfig}
}

// Register wires admin's API routes onto mux. Every route is under /api/,
// gate-protected and origin-checked and body-limited exactly like every
// other module's routes (the dispatcher applies all three uniformly in
// buildDispatcher) -- nothing here adds auth logic of its own.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/config/auth", h.handleGetConfigAuth)
	mux.HandleFunc("PUT /api/config/modules", h.handlePutConfigModules)
	mux.HandleFunc("POST /api/keys", h.handlePostKey)
	mux.HandleFunc("DELETE /api/keys/{name}", h.handleDeleteKey)
	mux.HandleFunc("POST /api/ldap/test", h.handlePostLDAPTest)
	mux.HandleFunc("PUT /api/config/session", h.handlePutConfigSession)
	mux.HandleFunc("GET /api/config/pin-files", h.handleGetPinFiles)
	mux.HandleFunc("POST /api/config/pin-files", h.handlePostPinFile)
	mux.HandleFunc("PUT /api/config/ldap", h.handlePutConfigLdap)
}

// mutateAuth is the single path every mutating route below uses. It holds
// h.mu for the entire sequence: mutate is called with the current
// h.authConfig and returns the candidate config to apply plus whether a
// change should happen at all (false lets a handler answer "not found" --
// e.g. deleting a key that isn't there -- without splicing an
// unchanged config back into the file). When ok is true, the candidate is
// passed to applyAuth (T5.3: ValidatePolicy -> splice -> SwapPolicy); a
// rejection there is returned unchanged and h.authConfig is left untouched
// (matching applyAuth's own "reject changes nothing" contract). Only when
// applyAuth succeeds does h.authConfig become the candidate, so the very
// next request -- whether GET /api/config/auth or another mutation --
// observes it.
func (h *Handler) mutateAuth(mutate func(config.AuthConfig) (config.AuthConfig, bool)) (applied bool, err error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	candidate, ok := mutate(h.authConfig)
	if !ok {
		return false, nil
	}
	expandedAuth, err := h.applyAuth(candidate)
	if err != nil {
		return false, err
	}
	// Store applyAuth's return value, not candidate: it carries every
	// pin_file expanded relative to the config file's directory, matching
	// what was actually validated, persisted, and swapped in -- so the very
	// next GET /api/config/auth reports the same absolute paths boot would.
	h.authConfig = expandedAuth
	return true, nil
}

// --- GET /api/config/auth ---

// authConfigView is the GET /api/config/auth response shape: a redacted
// mirror of config.AuthConfig safe to hand to a browser. Every credential is
// either elided to the literal string "(set)" (bcrypt/API-key hashes,
// LDAP.bind_password) or reduced to a defined_by indicator that never
// carries the secret itself (the operator PIN: "config" for an inline hash,
// "file" plus the file's path -- the path is not a secret -- for the file
// form, "none" when neither is configured). This shape is deliberately
// stable and documented here rather than reusing config.AuthConfig
// verbatim, so a future field added to config.AuthConfig does not
// accidentally leak through this endpoint by default.
type authConfigView struct {
	Modules      map[string]config.ModuleAuthConfig `json:"modules"`
	KnownModules []string                           `json:"known_modules"`
	APIKeys      []namedHashView                    `json:"api_keys"`
	AdminPIN     adminPINView                       `json:"admin_pin"`
	LDAP         ldapConfigView                     `json:"ldap"`
	Passkey      config.PasskeyConfig               `json:"passkey"`
	Session      sessionConfigView                  `json:"session"`
	CookieSecure bool                               `json:"cookie_secure"`
	CookieDomain string                             `json:"cookie_domain"`
}

// namedHashView mirrors config.NamedHash with Hash always redacted to
// "(set)" -- every entry in the underlying table has a hash by construction
// (upsertNamedHash/handlePostKey never store an empty one), so
// there is no "unset" case to distinguish here.
type namedHashView struct {
	Name string `json:"name"`
	Hash string `json:"hash"`
}

// adminPINView reports how the operator PIN is configured without ever
// carrying its value: DefinedBy is "config" (inline bcrypt hash), "file"
// (Path names the 0400 PIN file -- the path is not a secret), or "none".
type adminPINView struct {
	DefinedBy string `json:"defined_by"`
	Path      string `json:"path,omitempty"`
}

// ldapConfigView mirrors config.LDAPConfig with BindPassword redacted to
// "(set)" when non-empty, "" (still absent) when it is.
type ldapConfigView struct {
	URL            string   `json:"url"`
	StartTLS       bool     `json:"start_tls"`
	InsecureTLS    bool     `json:"insecure_tls"`
	BindDN         string   `json:"bind_dn"`
	BindPassword   string   `json:"bind_password"`
	BaseDN         string   `json:"base_dn"`
	UserFilter     string   `json:"user_filter"`
	RequiredGroups []string `json:"required_groups"`
	TimeoutSeconds int      `json:"timeout_seconds"`
}

// sessionConfigView mirrors config.SessionConfig verbatim -- neither field
// is a secret.
type sessionConfigView struct {
	TTLHours             int     `json:"ttl_hours"`
	RefreshAfterFraction float64 `json:"refresh_after_fraction"`
}

// redactAuthConfig builds the GET /api/config/auth response from a, eliding
// every credential per authConfigView's doc comment.
func redactAuthConfig(a config.AuthConfig) authConfigView {
	keys := make([]namedHashView, len(a.APIKeys))
	for i, k := range a.APIKeys {
		keys[i] = namedHashView{Name: k.Name, Hash: "(set)"}
	}

	adminPIN := adminPINView{DefinedBy: "none"}
	switch {
	case a.AdminPIN != "":
		adminPIN.DefinedBy = "config"
	case a.AdminPINFile != "":
		adminPIN.DefinedBy = "file"
		adminPIN.Path = a.AdminPINFile
	}

	bindPassword := ""
	if a.LDAP.BindPassword != "" {
		bindPassword = "(set)"
	}

	modules := a.Modules
	if modules == nil {
		modules = map[string]config.ModuleAuthConfig{}
	}

	return authConfigView{
		Modules:  modules,
		APIKeys:  keys,
		AdminPIN: adminPIN,
		LDAP: ldapConfigView{
			URL:            a.LDAP.URL,
			StartTLS:       a.LDAP.StartTLS,
			InsecureTLS:    a.LDAP.InsecureTLS,
			BindDN:         a.LDAP.BindDN,
			BindPassword:   bindPassword,
			BaseDN:         a.LDAP.BaseDN,
			UserFilter:     a.LDAP.UserFilter,
			RequiredGroups: a.LDAP.RequiredGroups,
			TimeoutSeconds: a.LDAP.TimeoutSeconds,
		},
		Passkey:      a.Passkey,
		Session:      sessionConfigView{TTLHours: a.Session.TTLHours, RefreshAfterFraction: a.Session.RefreshAfterFraction},
		CookieSecure: a.CookieSecure,
		CookieDomain: a.CookieDomain,
	}
}

func (h *Handler) handleGetConfigAuth(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	cur := h.authConfig
	h.mu.Unlock()
	view := redactAuthConfig(cur)
	// The matrix editor needs every routable module -- not just the ones
	// already protected -- so protection can be turned ON from the UI.
	view.KnownModules = append([]string(nil), h.deps.KnownModules...)
	response.WriteJSON(w, http.StatusOK, view)
}

// --- PUT /api/config/modules ---

// handlePutConfigModules replaces the module auth table wholesale: the
// request body is exactly the map[string]config.ModuleAuthConfig
// auth.ValidatePolicy consumes (module name, or the reserved "admin"
// pseudo-module, -> its two-state auth config). An unknown module (e.g. a
// typo) is rejected by applyAuth's ValidatePolicy step and reported as 400
// with its exact error message; nothing is written to the config file and
// the live policy is untouched.
func (h *Handler) handlePutConfigModules(w http.ResponseWriter, r *http.Request) {
	var matrix map[string]config.ModuleAuthConfig
	if err := json.NewDecoder(r.Body).Decode(&matrix); err != nil {
		response.WriteDecodeError(w, err)
		return
	}

	_, err := h.mutateAuth(func(cur config.AuthConfig) (config.AuthConfig, bool) {
		cur.Modules = matrix
		return cur, true
	})
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	// Report what was actually persisted (h.authConfig.Modules), not the
	// submitted matrix verbatim: applyAuth expands each pin_file relative to
	// the config file's directory before validating and splicing it in, so a
	// relative path the operator typed is echoed back here as the same
	// absolute path GET /api/config/auth would report.
	h.mu.Lock()
	persisted := h.authConfig.Modules
	h.mu.Unlock()
	response.WriteJSON(w, http.StatusOK, map[string]any{"modules": persisted})
}

// --- POST /api/keys, DELETE /api/keys/{name} ---

// postKeyRequest is the POST /api/keys body.
type postKeyRequest struct {
	Name string `json:"name"`
}

// handlePostKey generates a fresh API key server-side, mirroring
// cmd/server/main.go's genAPIKeyAndExit (-gen-api-key) exactly -- 32 random
// bytes, base64.RawURLEncoding-encoded as the plaintext key, hashed with
// SHA-256 into the same "sha256:<hex>" form auth.checkAPIKey compares
// against -- so a key minted here and one minted with -gen-api-key are
// byte-for-byte interchangeable. Only the hash is ever stored; the
// plaintext key is returned in this response and this response alone (AC-15)
// -- it is never logged, never re-derivable from the stored hash, and never
// appears in any other endpoint's output.
func (h *Handler) handlePostKey(w http.ResponseWriter, r *http.Request) {
	var req postKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteDecodeError(w, err)
		return
	}
	if req.Name == "" {
		response.WriteError(w, http.StatusBadRequest, "name is required")
		return
	}

	key, hash, err := generateAPIKey()
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "generating key")
		return
	}

	_, err = h.mutateAuth(func(cur config.AuthConfig) (config.AuthConfig, bool) {
		cur.APIKeys = upsertNamedHash(cur.APIKeys, req.Name, hash)
		return cur, true
	})
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	response.WriteJSON(w, http.StatusOK, map[string]string{"name": req.Name, "key": key})
}

// generateAPIKey mirrors cmd/server/main.go's genAPIKeyAndExit precisely:
// 32 cryptographically random bytes, base64.RawURLEncoding-encoded as the
// plaintext key a client presents (Authorization: Bearer or X-API-Key,
// per apikey.go's extractAPIKey), hashed with SHA-256 into the
// "sha256:<hex>" string stored in config.NamedHash.Hash.
func generateAPIKey() (key, hash string, err error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", err
	}
	key = base64.RawURLEncoding.EncodeToString(raw)
	sum := sha256.Sum256([]byte(key))
	hash = "sha256:" + hex.EncodeToString(sum[:])
	return key, hash, nil
}

// handleDeleteKey removes the named API-key entry, if present, via
// applyAuth. Revoking a key this way takes effect on the very next request
// (SwapPolicy) -- the deleted key stops authenticating immediately, no
// restart.
func (h *Handler) handleDeleteKey(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	applied, err := h.mutateAuth(func(cur config.AuthConfig) (config.AuthConfig, bool) {
		keys, ok := removeNamedHash(cur.APIKeys, name)
		if !ok {
			return cur, false
		}
		cur.APIKeys = keys
		return cur, true
	})
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !applied {
		response.WriteError(w, http.StatusNotFound, "key not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- shared NamedHash table helpers ---

// upsertNamedHash returns a copy of list with name's entry set to hash --
// replacing an existing entry with that name, or appending a new one. list
// itself is never mutated.
func upsertNamedHash(list []config.NamedHash, name, hash string) []config.NamedHash {
	out := make([]config.NamedHash, len(list))
	copy(out, list)
	for i, e := range out {
		if e.Name == name {
			out[i].Hash = hash
			return out
		}
	}
	return append(out, config.NamedHash{Name: name, Hash: hash})
}

// removeNamedHash returns a copy of list with name's entry (if any) removed,
// and whether an entry was actually found and removed. list itself is never
// mutated.
func removeNamedHash(list []config.NamedHash, name string) ([]config.NamedHash, bool) {
	out := make([]config.NamedHash, 0, len(list))
	removed := false
	for _, e := range list {
		if e.Name == name {
			removed = true
			continue
		}
		out = append(out, e)
	}
	return out, removed
}

// --- POST /api/ldap/test ---

// ldapTestRequest is the POST /api/ldap/test body: credentials to attempt a
// bind with. Neither field is ever echoed back or logged.
type ldapTestRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// handlePostLDAPTest attempts an LDAP bind server-side against the
// *currently saved* auth.ldap config (not a posted override -- the point is
// to test the configuration an admin has already put in place) using the
// posted username/password, and reports only the outcome class: "ok",
// "bad_credentials", "unreachable", or "tls_error" (classifyLDAPTestError).
// The request body's credentials are read once, passed straight to
// auth.LDAPClient.Authenticate, and never appear in the response, a log
// line, or an error message -- Authenticate's own contract already
// guarantees its returned error never carries the password.
func (h *Handler) handlePostLDAPTest(w http.ResponseWriter, r *http.Request) {
	var req ldapTestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteDecodeError(w, err)
		return
	}

	h.mu.Lock()
	ldapCfg := h.authConfig.LDAP
	h.mu.Unlock()

	client := auth.NewLDAPClient(ldapCfg)
	_, bindErr := client.Authenticate(r.Context(), req.Username, req.Password)

	response.WriteJSON(w, http.StatusOK, map[string]string{"result": classifyLDAPTestError(bindErr)})
}

// classifyLDAPTestError maps an error from auth.LDAPClient.Authenticate to
// one of the four outcome classes POST /api/ldap/test reports:
//
//   - nil                                       -> "ok"
//   - auth.ErrLDAPAuth / auth.ErrLDAPForbidden   -> "bad_credentials"
//     (ldap.go's Authenticate deliberately folds "no such user", "wrong
//     password", and "authenticated but not in a required group" into these
//     two sentinels, precisely so a probe here can never be used to
//     enumerate valid usernames by timing or by a distinguishable error)
//   - a TLS handshake/certificate-verification failure (isLDAPTLSError)
//     -> "tls_error"
//   - a *ldap.Error reporting a bind-level rejection (e.g. a misconfigured
//     service auth.ldap.bind_dn/bind_password -- LDAPResultInvalidCredentials
//     and the two closest siblings) -> "bad_credentials", the closest of
//     the four classes even though the rejected identity here is the
//     service account's, not the tested user's
//   - anything else that reaches Authenticate's returned error can only
//     have originated in LDAPClient.connect's dial (network unreachable,
//     connection refused, DNS failure, timeout) -- none of those are a TLS
//     or bind-rejection shape, so "unreachable" is both the correct answer
//     for that case and the closest of the four classes for any residual
//     unclassified failure, so it is this function's default.
func classifyLDAPTestError(err error) string {
	if err == nil {
		return "ok"
	}
	if errors.Is(err, auth.ErrLDAPAuth) || errors.Is(err, auth.ErrLDAPForbidden) {
		return "bad_credentials"
	}
	if isLDAPTLSError(err) {
		return "tls_error"
	}
	var ldapErr *ldap.Error
	if errors.As(err, &ldapErr) {
		switch ldapErr.ResultCode {
		case ldap.LDAPResultInvalidCredentials, ldap.LDAPResultInappropriateAuthentication, ldap.LDAPResultInsufficientAccessRights:
			return "bad_credentials"
		}
	}
	return "unreachable"
}

// isLDAPTLSError reports whether err represents a TLS handshake or
// certificate-verification failure, as opposed to a plain dial/connection
// failure. The crypto/x509 error types are checked directly; many
// crypto/tls handshake failures surface as a plain *errors.errorString
// (e.g. "tls: failed to verify certificate: ...") with no distinguishing
// type at all, so a message naming "tls", "certificate", or "x509" is
// treated as the same class.
func isLDAPTLSError(err error) bool {
	var certErr x509.CertificateInvalidError
	var hostErr x509.HostnameError
	var authorityErr x509.UnknownAuthorityError
	var recordErr tls.RecordHeaderError
	if errors.As(err, &certErr) || errors.As(err, &hostErr) || errors.As(err, &authorityErr) || errors.As(err, &recordErr) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "tls:") || strings.Contains(msg, "certificate") || strings.Contains(msg, "x509")
}

// --- PUT /api/config/ldap ---

// putLDAPRequest mirrors config.LDAPConfig's JSON shape exactly except
// BindPassword, which is a *string here so the three "leave it alone"
// spellings (omitted, blank after strings.TrimSpace, and the redacted
// placeholder "(set)" GET /api/config/auth echoes back for an
// already-configured password) can be told apart from an actual new value
// -- see handlePutConfigLdap.
type putLDAPRequest struct {
	URL            string   `json:"url"`
	StartTLS       bool     `json:"start_tls"`
	InsecureTLS    bool     `json:"insecure_tls"`
	BindDN         string   `json:"bind_dn"`
	BindPassword   *string  `json:"bind_password"`
	BaseDN         string   `json:"base_dn"`
	UserFilter     string   `json:"user_filter"`
	RequiredGroups []string `json:"required_groups"`
	TimeoutSeconds int      `json:"timeout_seconds"`
}

// handlePutConfigLdap replaces auth.ldap wholesale via applyAuth, making the
// admin console's LDAP panel an editor rather than a read-only display.
// Every field replaces the currently configured value except BindPassword:
// nil, blank after strings.TrimSpace, or the literal "(set)" all mean "keep
// the currently configured bind password" -- anything else replaces it.
// This is what lets the admin UI round-trip a GET's redacted view straight
// back through a PUT without the displayed placeholder ever overwriting the
// real password.
//
// ValidatePolicy (via applyAuth) rejects clearing auth.ldap.url while a
// protected non-admin module still exists; that rejection surfaces here as
// a 400 with its exact message, same as every other mutateAuth caller. On
// success the response is the same redacted LDAP view redactAuthConfig
// produces for GET /api/config/auth's "ldap" field.
func (h *Handler) handlePutConfigLdap(w http.ResponseWriter, r *http.Request) {
	var req putLDAPRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteDecodeError(w, err)
		return
	}

	_, err := h.mutateAuth(func(cur config.AuthConfig) (config.AuthConfig, bool) {
		bindPassword := cur.LDAP.BindPassword
		if req.BindPassword != nil {
			trimmed := strings.TrimSpace(*req.BindPassword)
			if trimmed != "" && trimmed != "(set)" {
				bindPassword = *req.BindPassword
			}
		}
		cur.LDAP = config.LDAPConfig{
			URL:            req.URL,
			StartTLS:       req.StartTLS,
			InsecureTLS:    req.InsecureTLS,
			BindDN:         req.BindDN,
			BindPassword:   bindPassword,
			BaseDN:         req.BaseDN,
			UserFilter:     req.UserFilter,
			RequiredGroups: req.RequiredGroups,
			TimeoutSeconds: req.TimeoutSeconds,
		}
		return cur, true
	})
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	h.mu.Lock()
	view := redactAuthConfig(h.authConfig)
	h.mu.Unlock()
	response.WriteJSON(w, http.StatusOK, view.LDAP)
}

// --- PUT /api/config/session ---

// putSessionRequest is the PUT /api/config/session body.
type putSessionRequest struct {
	TTLHours     int    `json:"ttl_hours"`
	CookieDomain string `json:"cookie_domain"`
	CookieSecure bool   `json:"cookie_secure"`
}

// handlePutConfigSession replaces the session TTL and cookie attributes via
// applyAuth. Setting TTLHours to 0 restores the default (720h) -- the same
// zero-means-default rule session.go's sessionTTL applies everywhere else.
func (h *Handler) handlePutConfigSession(w http.ResponseWriter, r *http.Request) {
	var req putSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteDecodeError(w, err)
		return
	}

	_, err := h.mutateAuth(func(cur config.AuthConfig) (config.AuthConfig, bool) {
		cur.Session.TTLHours = req.TTLHours
		cur.CookieDomain = req.CookieDomain
		cur.CookieSecure = req.CookieSecure
		return cur, true
	})
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	response.WriteJSON(w, http.StatusOK, map[string]any{
		"ttl_hours":     req.TTLHours,
		"cookie_domain": req.CookieDomain,
		"cookie_secure": req.CookieSecure,
	})
}

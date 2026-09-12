// policy.go defines the immutable, hot-swappable snapshot of auth state
// (Policy) and the long-lived Service that holds an atomic pointer to the
// current snapshot alongside the non-swappable singletons (the HMAC session
// key and the global login throttle).
//
// The design (FR-A11 / AC-13) is deliberate: BuildPolicy is the *only*
// constructor of a Policy, and it captures everything a live admin edit can
// change -- the two-state module table, the API-key table, the admin-PIN
// source, LDAP settings, passkey RP config, and session/cookie settings --
// not just the module table. A later task (admin live-apply) validates a new
// config.AuthConfig, builds a fresh Policy, and swaps it into the Service
// with a single atomic pointer store. Because every per-request read goes
// through Service.policy(), that swap takes effect on the very next request
// with no half-applied state and no restart, for every field a Policy
// carries -- not only the ones a matrix-only snapshot would have covered.
package auth

import (
	"net/http"
	"sync/atomic"
	"time"

	"cmd184psu/unified-webapp/internal/platform/config"
)

// Policy is an immutable snapshot of every piece of auth state that can be
// hot-swapped by a live admin edit. Once built, a Policy's fields are never
// mutated -- a swap replaces the whole snapshot, never a field within it, so
// concurrent readers always observe an internally-consistent set of values.
type Policy struct {
	// Modules is a copy of config.AuthConfig.Modules: module name (or the
	// reserved "admin" pseudo-module) -> its two-state auth config. Present
	// (even a zero ModulePolicy) means the module is protected; absent means
	// open (auth bypassed entirely). This replaces the old accepted-method
	// matrix: what a protected module offers on its login page is computed
	// by OfferedMethods below, never read off this map directly.
	Modules map[string]ModulePolicy

	// APIKeys is a copy of the configured named-hash API-key table. Keys are
	// orthogonal service authentication: valid on any protected non-admin
	// module, never on admin, never surfaced on a login page.
	APIKeys []config.NamedHash

	// AdminPIN and AdminPINFile mirror config.AuthConfig's admin operator
	// PIN source. ValidatePolicy guarantees at most one of the three
	// configured spellings (auth.admin_pin, auth.admin_pin_file,
	// auth.modules.admin.pin_file) is ever set; BuildPolicy folds the third
	// spelling into AdminPINFile, so by the time a Policy exists there are
	// only ever these two fields to consult, exactly one non-empty or both
	// empty when no operator PIN is configured.
	AdminPIN     string
	AdminPINFile string

	// LDAP is a copy of the configured LDAP settings. A Service constructs
	// an *LDAPClient from this per policy (NewLDAPClient is cheap -- it does
	// nothing beyond filling in a filter default -- so there is no need to
	// cache one across swaps).
	LDAP config.LDAPConfig

	// Passkey is a copy of the configured passkey RP settings.
	Passkey config.PasskeyConfig

	// passkeys is the constructed passkey ceremony service for this
	// snapshot, or nil when no protected non-admin module is configured (or
	// Passkey.RPID is unset). It is built once per Policy (newPasskeyService
	// touches the filesystem), never rebuilt per request.
	passkeys *passkeyService

	// SessionTTL and RefreshFraction are the resolved (default-applied)
	// session lifetime and sliding-refresh fraction -- see session.go's
	// sessionTTL/refreshFraction.
	SessionTTL      time.Duration
	RefreshFraction float64

	// CookieSecure and CookieDomain mirror config.AuthConfig's cookie
	// attributes.
	CookieSecure bool
	CookieDomain string
}

// ModulePolicy is the resolved per-module auth state carried in a Policy
// snapshot.
type ModulePolicy struct {
	// PinFile is the absolute path to the module's door-code file (S1's
	// config loading, and the admin live-apply pipeline, both expand it
	// relative to the config file's directory before it ever reaches here),
	// or "" when the module has no door code (LDAP/passkey only).
	PinFile string
}

// OfferedMethods returns the auth methods module should present on
// /api/auth/mode and accept from a login attempt (S3 wires both) -- it
// drives login *surface* only, never request authorization (that is
// grantsAllow's job, session.go):
//
//   - module == "admin": exactly ["admin_pin"], regardless of whether admin
//     has its own Modules entry. The operator PIN is the only way in.
//   - a protected non-admin module: ["ldap"], plus "passkey" when this
//     policy's passkey service is configured, plus "pin" when the module's
//     ModulePolicy has a PinFile. API keys ("key") never appear here -- they
//     are orthogonal service auth with no login-page surface.
//   - an open (unconfigured) module: nil.
func (p *Policy) OfferedMethods(module string) []string {
	if module == "admin" {
		return []string{adminPINMethod}
	}
	m, ok := p.Modules[module]
	if !ok {
		return nil
	}
	methods := []string{"ldap"}
	if p.passkeys != nil {
		methods = append(methods, "passkey")
	}
	if m.PinFile != "" {
		methods = append(methods, pinMethod)
	}
	return methods
}

// BuildPolicy constructs an immutable Policy snapshot from a. It is the one
// constructor of a Policy -- boot (FromConfig) and a later admin live-apply
// both call it, then hand the result to Service.SwapPolicy. BuildPolicy does
// not validate a (that is ValidatePolicy's job, and callers are expected to
// have already called it); it only copies/resolves fields and, when needed,
// constructs the passkey ceremony service.
//
// The passkeyService is only constructed when passkeys are actually usable:
// a.Passkey.RPID must be set AND at least one protected non-admin module is
// configured -- passkey is offered on every protected non-admin module once
// globally configured (OfferedMethods above), never gated per-module the way
// the old method-list matrix did, so admin-only configs never need it.
// newPasskeyService touches the filesystem (it creates dataDir and
// opens/creates the passkeys.json store), so building it unconditionally
// would mean every config -- even one with no protected non-admin module at
// all -- pays that cost and, on an unwritable data dir, fails to boot. Any
// error from newPasskeyService is propagated to the caller.
func BuildPolicy(a config.AuthConfig) (*Policy, error) {
	adminPINFile := a.AdminPINFile
	if adminPINFile == "" {
		if m, ok := a.Modules["admin"]; ok {
			adminPINFile = m.PinFile
		}
	}

	p := &Policy{
		Modules:         copyModules(a.Modules),
		APIKeys:         append([]config.NamedHash(nil), a.APIKeys...),
		AdminPIN:        a.AdminPIN,
		AdminPINFile:    adminPINFile,
		LDAP:            a.LDAP,
		Passkey:         a.Passkey,
		SessionTTL:      sessionTTL(a.Session),
		RefreshFraction: refreshFraction(a.Session),
		CookieSecure:    a.CookieSecure,
		CookieDomain:    a.CookieDomain,
	}

	if a.Passkey.RPID != "" && hasProtectedNonAdminModule(a.Modules) {
		svc, err := newPasskeyService(a.Passkey, a.DataDir, nil)
		if err != nil {
			return nil, err
		}
		p.passkeys = svc
	}

	return p, nil
}

// copyModules returns a fresh top-level copy of m, so a caller mutating a's
// Modules (or a Policy's) after BuildPolicy returns can never affect the
// snapshot.
func copyModules(m map[string]config.ModuleAuthConfig) map[string]ModulePolicy {
	if m == nil {
		return nil
	}
	out := make(map[string]ModulePolicy, len(m))
	for k, v := range m {
		out[k] = ModulePolicy{PinFile: v.PinFile}
	}
	return out
}

// hasProtectedNonAdminModule reports whether modules has at least one entry
// other than "admin". BuildPolicy's passkey-construction condition and
// validate.go's per-module LDAP requirement both key off this notion of "a
// real protected module exists".
func hasProtectedNonAdminModule(modules map[string]config.ModuleAuthConfig) bool {
	for k := range modules {
		if k != "admin" {
			return true
		}
	}
	return false
}

// Service is the long-lived auth entry point held by the dispatcher: an
// atomically-swappable Policy snapshot plus the singletons that are
// deliberately *not* part of that snapshot because they are keyed to
// on-disk state that a config edit cannot rotate in place:
//
//   - the HMAC session-signing key is loaded once from auth.data_dir at
//     boot (loadOrCreateKey) and never regenerated by a live-apply -- an
//     admin edit changes what a session may authorize, not the key that
//     signs it; rotating the key file itself is an operator action (FR-A2)
//     that requires a restart, not a policy swap.
//   - the login throttle (FR-A7) is a single global failure counter across
//     every method and module; swapping policies must not reset it, or an
//     admin edit could be used to bypass an in-progress backoff.
type Service struct {
	policyPtr atomic.Pointer[Policy]
	key       []byte
	throttle  *throttle
	now       func() time.Time

	// ldapDialer overrides the dialer used by the *LDAPClient the login
	// handler (handlers.go) builds per request from the current policy's
	// LDAP config. It is nil in production (ldapClient's NewLDAPClient call
	// keeps the real go-ldap dialer); tests set it to a fake so a login
	// handler test can exercise the ldap method without a network,
	// mirroring ldap_test.go's clientWithConn seam.
	ldapDialer ldapDialer
}

// ldapClient builds an *LDAPClient from cfg, the current policy's LDAP
// config, applying s.ldapDialer as a test override when set.
func (s *Service) ldapClient(cfg config.LDAPConfig) *LDAPClient {
	c := NewLDAPClient(cfg)
	if s.ldapDialer != nil {
		c.dialer = s.ldapDialer
	}
	return c
}

// policy returns the current Policy snapshot. Called once per request (or
// once per check within a request) so every authenticator and the gate
// itself always observe a single, internally-consistent snapshot even if a
// SwapPolicy happens concurrently.
func (s *Service) policy() *Policy {
	return s.policyPtr.Load()
}

// SwapPolicy atomically replaces the current Policy snapshot with p. This is
// the single mechanism by which a live admin edit (a later task) takes
// effect: build a new Policy with BuildPolicy, then call SwapPolicy. There
// is no partial-update path -- every field changes together, in one atomic
// pointer store.
func (s *Service) SwapPolicy(p *Policy) {
	s.policyPtr.Store(p)
}

// FromConfig builds a Service from a, the module names buildDispatcher
// knows about, and whether the admin module is routed at all. It validates
// a (ValidatePolicy, with the same knownModules/adminRouted boot uses),
// builds the initial Policy (BuildPolicy), loads or creates the HMAC session
// key, and starts a fresh throttle.
//
// Lazy key creation: when a has no modules configured at all and no admin
// operator PIN (the zero-value AuthConfig, or any config that protects
// nothing), FromConfig skips loadOrCreateKey entirely and returns a working
// Service with an empty Policy -- every gate becomes pass-through. This
// keeps a fully-unprotected config's boot free of any filesystem side
// effect under auth.data_dir (no directory is created, no key file is
// written), matching today's behavior for a config with no "auth" section
// at all (FR-A14). config.go/validate.go establish no default for
// data_dir when it is left empty, so this task does not invent one for the
// protected case either -- a protected config with an empty data_dir keys
// its session file relative to the process's working directory, exactly as
// loadOrCreateKey already does.
func FromConfig(a config.AuthConfig, knownModules []string, adminRouted bool) (*Service, error) {
	if err := ValidatePolicy(a, knownModules, adminRouted); err != nil {
		return nil, err
	}

	p, err := BuildPolicy(a)
	if err != nil {
		return nil, err
	}

	s := &Service{now: time.Now}
	s.throttle = newThrottle(s.now)

	if len(a.Modules) > 0 || a.AdminPIN != "" || a.AdminPINFile != "" {
		key, err := loadOrCreateKey(a.DataDir)
		if err != nil {
			return nil, err
		}
		s.key = key
	}

	s.policyPtr.Store(p)
	return s, nil
}

// checkAdminPIN validates pin against the current policy's operator PIN
// source, delegating to pin.go's package-level checkAdminPIN.
func (s *Service) checkAdminPIN(pin string) (ok bool, err error) {
	p := s.policy()
	return checkAdminPIN(p.AdminPIN, p.AdminPINFile, pin)
}

// checkAPIKey validates a presented API key from r against the current
// policy's API-key table, delegating to apikey.go's package-level
// checkAPIKey.
func (s *Service) checkAPIKey(r *http.Request) (name string, ok bool) {
	return checkAPIKey(s.policy().APIKeys, r)
}

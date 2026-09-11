// policy.go defines the immutable, hot-swappable snapshot of auth state
// (Policy) and the long-lived Service that holds an atomic pointer to the
// current snapshot alongside the non-swappable singletons (the HMAC session
// key and the global login throttle).
//
// The design (FR-A11 / AC-13) is deliberate: BuildPolicy is the *only*
// constructor of a Policy, and it captures everything a live admin edit can
// change -- the module method matrix, the PIN/API-key tables, the admin-PIN
// source, LDAP settings, passkey RP config, and session/cookie settings --
// not just the matrix. A later task (admin live-apply) validates a new
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
	// reserved "admin" pseudo-module) -> its configured accepted-method
	// list. This is the raw matrix as configured -- it does not include the
	// admin_pin token that admin's *effective* accepted set always carries;
	// EffectiveMethods below folds that in, and the gate/mode reporting
	// (T4.2) is expected to call it for admin rather than reading Modules
	// directly.
	Modules map[string][]string

	// PINs and APIKeys are copies of the configured named-hash tables.
	PINs    []config.NamedHash
	APIKeys []config.NamedHash

	// AdminPIN and AdminPINFile mirror config.AuthConfig's admin operator
	// PIN source -- exactly one is non-empty, or both are empty when no
	// operator PIN is configured (ValidatePolicy enforces this before a
	// Policy is ever built from the config).
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
	// snapshot, or nil when passkeys are not configured/used by any module.
	// It is built once per Policy (newPasskeyService touches the
	// filesystem), never rebuilt per request.
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

// EffectiveMethods returns module's accepted-method list, folding in the
// reserved "admin_pin" token when module is the "admin" pseudo-module (the
// operator PIN is always acceptable for admin, regardless of whether "pin"
// appears in its matrix entry, and regardless of whether admin has a matrix
// entry at all). For every other module it is exactly the configured list
// (nil/empty when the module is unprotected).
func (p *Policy) EffectiveMethods(module string) []string {
	methods := p.Modules[module]
	if module != "admin" {
		return methods
	}
	out := make([]string, 0, len(methods)+1)
	out = append(out, methods...)
	for _, m := range out {
		if m == adminPINMethod {
			return out
		}
	}
	return append(out, adminPINMethod)
}

// BuildPolicy constructs an immutable Policy snapshot from a. It is the one
// constructor of a Policy -- boot (FromConfig) and a later admin live-apply
// both call it, then hand the result to Service.SwapPolicy. BuildPolicy does
// not validate a (that is ValidatePolicy's job, and callers are expected to
// have already called it); it only copies/resolves fields and, when needed,
// constructs the passkey ceremony service.
//
// The passkeyService is only constructed when passkeys are actually
// configured and used: a.Passkey.RPID must be set AND "passkey" must appear
// in at least one module's method list (including admin's, since admin's
// matrix entry is a plain module entry like any other). newPasskeyService
// touches the filesystem (it creates dataDir and opens/creates the
// passkeys.json store), so building it unconditionally would mean every
// config -- even one with no passkey module at all -- pays that cost and, on
// an unwritable data dir, fails to boot. Any error from newPasskeyService is
// propagated to the caller.
func BuildPolicy(a config.AuthConfig) (*Policy, error) {
	p := &Policy{
		Modules:         copyModules(a.Modules),
		PINs:            append([]config.NamedHash(nil), a.PINs...),
		APIKeys:         append([]config.NamedHash(nil), a.APIKeys...),
		AdminPIN:        a.AdminPIN,
		AdminPINFile:    a.AdminPINFile,
		LDAP:            a.LDAP,
		Passkey:         a.Passkey,
		SessionTTL:      sessionTTL(a.Session),
		RefreshFraction: refreshFraction(a.Session),
		CookieSecure:    a.CookieSecure,
		CookieDomain:    a.CookieDomain,
	}

	if a.Passkey.RPID != "" && modulesUseMethod(a.Modules, "passkey") {
		svc, err := newPasskeyService(a.Passkey, a.DataDir, nil)
		if err != nil {
			return nil, err
		}
		p.passkeys = svc
	}

	return p, nil
}

// copyModules returns a deep-enough copy of m: a fresh top-level map with
// each value slice also copied, so a caller mutating a's Modules (or a
// Policy's) after BuildPolicy returns can never affect the snapshot.
func copyModules(m map[string][]string) map[string][]string {
	if m == nil {
		return nil
	}
	out := make(map[string][]string, len(m))
	for k, v := range m {
		out[k] = append([]string(nil), v...)
	}
	return out
}

// modulesUseMethod reports whether method appears in any module's list in
// modules.
func modulesUseMethod(modules map[string][]string, method string) bool {
	for _, methods := range modules {
		for _, m := range methods {
			if m == method {
				return true
			}
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

// checkPIN validates pin against the current policy's named PIN table,
// delegating to pin.go's package-level checkPIN.
func (s *Service) checkPIN(pin string) (name string, ok bool) {
	return checkPIN(s.policy().PINs, pin)
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

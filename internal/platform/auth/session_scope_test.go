package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"cmd184psu/unified-webapp/internal/platform/config"
)

// scopeHarness is a Service with a movable clock and a one-cookie "browser",
// as with a shared cookie_domain: every module sees the same uw_session.
type scopeHarness struct {
	t      *testing.T
	svc    *Service
	now    time.Time
	cookie string
}

func newScopeHarness(t *testing.T, p *Policy) *scopeHarness {
	t.Helper()
	h := &scopeHarness{t: t, now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	nowFn := func() time.Time { return h.now }
	h.svc = &Service{key: gateTestKey(), now: nowFn, throttle: newThrottle(nowFn)}
	h.svc.SwapPolicy(p)
	return h
}

// do sends a request to module and keeps whatever cookie the answer sets.
func (h *scopeHarness) do(module, method, path string, body *http.Request) *httptest.ResponseRecorder {
	h.t.Helper()
	req := body
	if req == nil {
		req = httptest.NewRequest(method, path, nil)
	}
	if h.cookie != "" {
		req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: h.cookie})
	}
	rec := httptest.NewRecorder()
	h.svc.Gate(module, echoHandler()).ServeHTTP(rec, req)
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookieName {
			h.cookie = c.Value
			if c.MaxAge < 0 {
				h.cookie = ""
			}
		}
	}
	return rec
}

func (h *scopeHarness) login(module, method, pin, user, pass string) {
	h.t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", loginBody(h.t, method, pin, user, pass))
	if rec := h.do(module, "", "", req); rec.Code != http.StatusOK {
		h.t.Fatalf("%s login to %s: %d %s", method, module, rec.Code, rec.Body.String())
	}
}

// allowed reports whether a page request to module gets through the gate.
func (h *scopeHarness) allowed(module string) bool {
	h.t.Helper()
	rec := h.do(module, http.MethodGet, "/api/items", nil)
	return rec.Header().Get("X-Reached-Next") == "yes"
}

// use reports real use of module, as the shared page code does on clicks
// and typing (POST /api/auth/activity), and says whether it was accepted.
func (h *scopeHarness) use(module string) bool {
	h.t.Helper()
	return h.do(module, http.MethodPost, "/api/auth/activity", nil).Code == http.StatusOK
}

func (h *scopeHarness) logout(module string) {
	h.t.Helper()
	h.do(module, http.MethodPost, "/api/auth/logout", nil)
}

// newLDAPScopeHarness protects todo and grocery, with LDAP accepting carol.
func newLDAPScopeHarness(t *testing.T) *scopeHarness {
	t.Helper()
	p := hourPolicy(map[string]ModulePolicy{"todo": {}, "grocery": {}})
	p.LDAP = config.LDAPConfig{URL: "ldap://fake", BaseDN: "dc=example,dc=com"}
	h := newScopeHarness(t, p)
	h.svc.ldapDialer = func(ctx context.Context, opts ldapDialOptions) (ldapConn, error) {
		return scriptedUserConn("uid=carol,ou=people,dc=example,dc=com", []string{"users"}, nil), nil
	}
	return h
}

func rewritePinFile(t *testing.T, path, pin string) {
	t.Helper()
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(pin), 0400); err != nil {
		t.Fatal(err)
	}
	// Move the mtime on explicitly: the fingerprint cache is keyed on it.
	later := time.Now().Add(time.Minute)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}
}

// hourPolicy protects modules with the default 60-minute idle limit and a
// 30-day maximum session length.
func hourPolicy(modules map[string]ModulePolicy) *Policy {
	return &Policy{Modules: modules, SessionTTL: 720 * time.Hour}
}

func TestPINChangeEndsSessions(t *testing.T) {
	shared := writePinFile(t, "1234")
	other := writePinFile(t, "9999")
	h := newScopeHarness(t, hourPolicy(map[string]ModulePolicy{
		"slideshow": {PinFile: shared}, "todo": {PinFile: shared}, "grocery": {PinFile: other},
	}))
	h.login("slideshow", "pin", "1234", "", "")
	h.login("todo", "pin", "1234", "", "")
	h.login("grocery", "pin", "9999", "", "")

	rewritePinFile(t, shared, "56789")
	if h.allowed("slideshow") || h.allowed("todo") {
		t.Fatal("a PIN change must end the sessions made with the old PIN")
	}
	if !h.allowed("grocery") {
		t.Fatal("a module with a different PIN file must stay signed in")
	}
	if rec := h.do("slideshow", http.MethodGet, "/api/auth/session", nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("session check after a PIN change: %d, want 401", rec.Code)
	}

	// Logging in with the new PIN works, and replaces the stale grant.
	h.login("slideshow", "pin", "56789", "", "")
	if !h.allowed("slideshow") {
		t.Fatal("the new PIN should sign in")
	}
	claims, err := parseToken(gateTestKey(), h.cookie, h.now)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, g := range claims.Grants {
		if len(g) > len("pin:slideshow:") && g[:len("pin:slideshow:")] == "pin:slideshow:" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("want exactly one slideshow pin grant, got %v", claims.Grants)
	}
}

func TestSamePINOtherModuleNeedsItsOwnLogin(t *testing.T) {
	shared := writePinFile(t, "1234")
	h := newScopeHarness(t, hourPolicy(map[string]ModulePolicy{
		"slideshow": {PinFile: shared}, "todo": {PinFile: shared},
	}))
	h.login("slideshow", "pin", "1234", "", "")
	if h.allowed("todo") {
		t.Fatal("sharing a PIN file must not share the login")
	}
}

func TestPreFingerprintTokenDenied(t *testing.T) {
	pin := writePinFile(t, "1234")
	h := newScopeHarness(t, hourPolicy(map[string]ModulePolicy{"todo": {PinFile: pin}}))
	h.cookie = sessionCookieToken(t, "", []string{pinGrant("todo")}, time.Hour, h.now)
	if h.allowed("todo") {
		t.Fatal("a bare pin grant (issued before fingerprints) must be refused")
	}
}

func TestModulesHaveTheirOwnIdleClock(t *testing.T) {
	pin := writePinFile(t, "1234")
	h := newScopeHarness(t, hourPolicy(map[string]ModulePolicy{
		"slideshow": {PinFile: pin}, "todo": {PinFile: pin},
	}))
	h.login("slideshow", "pin", "1234", "", "")
	h.login("todo", "pin", "1234", "", "")

	// Keep using slideshow; leave todo idle.
	for i := 0; i < 4; i++ {
		h.now = h.now.Add(35 * time.Minute)
		if !h.use("slideshow") {
			t.Fatalf("slideshow, in use, should stay signed in (step %d)", i)
		}
	}
	if h.allowed("todo") {
		t.Fatal("todo was idle past its limit; slideshow's use must not keep it alive")
	}
}

func TestIdentityIdleClockIsPerModule(t *testing.T) {
	h := newLDAPScopeHarness(t)
	h.login("todo", "ldap", "", "carol", "correct-horse")
	if !h.allowed("grocery") {
		t.Fatal("an LDAP login reaches every protected module")
	}
	for i := 0; i < 4; i++ {
		h.now = h.now.Add(35 * time.Minute)
		if !h.use("todo") {
			t.Fatalf("todo, in use, should stay signed in (step %d)", i)
		}
	}
	if h.allowed("grocery") {
		t.Fatal("grocery was idle past its limit and should ask again")
	}
}

func TestLogoutSignsOutOfThisModuleOnly(t *testing.T) {
	pin := writePinFile(t, "1234")
	h := newScopeHarness(t, hourPolicy(map[string]ModulePolicy{
		"slideshow": {PinFile: pin}, "todo": {PinFile: pin},
	}))
	h.login("slideshow", "pin", "1234", "", "")
	h.login("todo", "pin", "1234", "", "")

	h.logout("slideshow")
	if h.allowed("slideshow") {
		t.Fatal("slideshow should be signed out")
	}
	if !h.allowed("todo") {
		t.Fatal("signing out of slideshow must not sign out of todo")
	}
	h.logout("todo")
	if h.cookie != "" {
		t.Error("with nothing left, the cookie should be cleared")
	}
}

func TestLogoutOfIdentityClearsEverything(t *testing.T) {
	h := newLDAPScopeHarness(t)
	h.login("todo", "ldap", "", "carol", "correct-horse")
	h.logout("todo")
	if h.cookie != "" || h.allowed("grocery") {
		t.Fatal("signing out of an LDAP identity clears the whole session")
	}
}

func TestLoginClaimsKeepOtherClocks(t *testing.T) {
	now := time.Unix(10_000, 0)
	existing := &sessionClaims{
		Grants:     []string{"pin:todo:aa", "pin:grocery:bb"},
		GrantTimes: map[string]int64{"pin:todo:aa": 100, "pin:grocery:bb": 200, "pin:gone:cc": 50},
		ModuleSeen: map[string]int64{"todo": 300},
	}
	c := loginClaims(existing, "", []string{"pin:todo:aa", "pin:grocery:bb", "pin:slideshow:dd"}, "pin:slideshow:dd", "slideshow", now)
	if c.GrantTimes["pin:todo:aa"] != 100 || c.GrantTimes["pin:grocery:bb"] != 200 || c.GrantTimes["pin:slideshow:dd"] != now.Unix() {
		t.Errorf("grant times %v", c.GrantTimes)
	}
	if _, ok := c.GrantTimes["pin:gone:cc"]; ok {
		t.Error("a grant no longer held should drop its clock")
	}
	if c.ModuleSeen["todo"] != 300 || c.ModuleSeen["slideshow"] != now.Unix() {
		t.Errorf("module clocks %v", c.ModuleSeen)
	}
}

func TestBackgroundPollingDoesNotKeepASessionAlive(t *testing.T) {
	pin := writePinFile(t, "1234")
	h := newScopeHarness(t, hourPolicy(map[string]ModulePolicy{"taskmaster": {PinFile: pin}}))
	h.login("taskmaster", "pin", "1234", "", "")
	for i := 0; i < 5; i++ {
		h.now = h.now.Add(10 * time.Minute)
		if !h.allowed("taskmaster") {
			t.Fatalf("polling within the idle limit still works (step %d)", i)
		}
	}
	h.now = h.now.Add(11 * time.Minute) // 61 minutes with no real use
	if h.allowed("taskmaster") {
		t.Fatal("polling alone must not keep an untouched tab signed in")
	}
}

func TestIdleLimitIsPerModuleConfig(t *testing.T) {
	pin := writePinFile(t, "1234")
	h := newScopeHarness(t, hourPolicy(map[string]ModulePolicy{
		"todo":    {PinFile: pin, Idle: 10 * time.Minute},
		"grocery": {PinFile: pin}, // unset: the 60-minute default
	}))
	h.login("todo", "pin", "1234", "", "")
	h.login("grocery", "pin", "1234", "", "")
	h.now = h.now.Add(15 * time.Minute)
	if h.allowed("todo") {
		t.Error("todo's 10-minute idle limit has passed")
	}
	if !h.allowed("grocery") {
		t.Error("grocery's default 60-minute limit has not")
	}
	h.now = h.now.Add(46 * time.Minute)
	if h.allowed("grocery") {
		t.Error("grocery's 60 minutes have passed")
	}
}

func TestMaximumSessionLengthEndsEvenActiveSessions(t *testing.T) {
	pin := writePinFile(t, "1234")
	p := hourPolicy(map[string]ModulePolicy{"todo": {PinFile: pin}})
	p.SessionTTL = 2 * time.Hour
	h := newScopeHarness(t, p)
	h.login("todo", "pin", "1234", "", "")
	for i := 0; i < 3; i++ {
		h.now = h.now.Add(30 * time.Minute)
		if !h.use("todo") {
			t.Fatalf("active and within the maximum length (step %d)", i)
		}
	}
	h.now = h.now.Add(31 * time.Minute) // 2h01m since login
	if h.use("todo") || h.allowed("todo") {
		t.Fatal("the maximum session length has passed; sign in again")
	}
}

func TestSessionCheckReportsIdleAndRenewsNothing(t *testing.T) {
	pin := writePinFile(t, "1234")
	h := newScopeHarness(t, hourPolicy(map[string]ModulePolicy{"todo": {PinFile: pin, Idle: 30 * time.Minute}}))
	h.login("todo", "pin", "1234", "", "")
	h.now = h.now.Add(20 * time.Minute)
	rec := h.do("todo", http.MethodGet, "/api/auth/session", nil)
	if rec.Code != http.StatusOK || decodeJSON(t, rec)["idleSeconds"] != float64(10*60) {
		t.Fatalf("session check: %d %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Set-Cookie") != "" {
		t.Error("checking the session is not use")
	}
	h.now = h.now.Add(11 * time.Minute)
	if rec := h.do("todo", http.MethodGet, "/api/auth/session", nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("after the idle limit the check should 401, got %d", rec.Code)
	}
}

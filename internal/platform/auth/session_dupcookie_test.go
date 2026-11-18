package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"cmd184psu/unified-webapp/internal/platform/config"
)

// A browser can hold two uw_session cookies (one set for a parent domain by an
// older configuration, one host-only) and sends both, the older first. The
// gate must take the valid one, whichever comes first; reading only the first
// left a signed-in user bounced to the login page with no message.
func TestGateAcceptsAValidSessionCookieAfterAStaleOne(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	p := &Policy{
		Modules:    map[string]ModulePolicy{"todo": {PinFile: writePinFile(t, "1111")}},
		LDAP:       config.LDAPConfig{URL: "ldap://fake"},
		SessionTTL: time.Hour,
	}
	svc := newGateService(t, now, p)
	grants := []string{pinGrantFP("todo", pinFingerprint(gateTestKey(), "1111"))}
	good := sessionCookieToken(t, "", grants, time.Hour, now)
	expired := sessionCookieToken(t, "", grants, time.Hour, now.Add(-3*time.Hour))
	forOtherModule := sessionCookieToken(t, "", []string{pinGrantFP("obsidianoid", pinFingerprint(gateTestKey(), "2222"))}, time.Hour, now)

	status := func(values ...string) int {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/items", nil)
		for _, v := range values {
			req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: v})
		}
		svc.Gate("todo", echoHandler()).ServeHTTP(rec, req)
		return rec.Code
	}
	cases := []struct {
		name   string
		values []string
		want   int
	}{
		{"valid only", []string{good}, 200},
		{"garbage first, valid second", []string{"garbage", good}, 200},
		{"expired first, valid second", []string{expired, good}, 200},
		{"valid first, garbage second", []string{good, "garbage"}, 200},
		{"only stale ones", []string{"garbage", expired}, 401},
		{"valid but for another module, then garbage", []string{forOtherModule, "garbage"}, 401},
	}
	for _, c := range cases {
		if got := status(c.values...); got != c.want {
			t.Errorf("%s: status %d, want %d", c.name, got, c.want)
		}
	}
}

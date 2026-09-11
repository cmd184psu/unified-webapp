package auth

// login_page_test.go covers T4.4: the embedded login page. gate_test.go
// already exercises the step-6 routing heuristic (HTML vs JSON, /api/ never
// masked); these tests focus on the page content itself -- stable form/button
// ids, the mode fetch, and the admin_pin/passkey membership checks in the
// inline JS -- via plain byte/string assertions on the embedded page, no
// headless browser.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestLoginPageServedOnUnauthenticatedHTMLGet(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	svc := newGateService(t, now, &Policy{Modules: map[string][]string{"grocery": {"pin"}}})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/some/page.html", nil)
	req.Header.Set("Accept", "text/html")
	svc.Gate("grocery", echoHandler()).ServeHTTP(rec, req)
	mustNotReached(t, rec)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("Content-Type = %q, want text/html", ct)
	}
	body := rec.Body.String()
	for _, marker := range []string{`id="pin-form"`, `id="ldap-form"`, `id="passkey-btn"`} {
		if !contains(body, marker) {
			t.Fatalf("body missing stable marker %q; body=%q", marker, body)
		}
	}
}

func TestLoginPageJSONOnUnauthenticatedJSONAccept(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	svc := newGateService(t, now, &Policy{Modules: map[string][]string{"grocery": {"pin"}}})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/some/page.html", nil)
	req.Header.Set("Accept", "application/json")
	svc.Gate("grocery", echoHandler()).ServeHTTP(rec, req)
	mustNotReached(t, rec)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	ct := rec.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}
	body := rec.Body.String()
	if contains(body, "pin-form") || contains(body, "<!DOCTYPE") {
		t.Fatalf("body = %q, must not be the HTML login page", body)
	}
}

func TestLoginPageJSONOnPostEvenWithHTMLAccept(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	svc := newGateService(t, now, &Policy{Modules: map[string][]string{"grocery": {"pin"}}})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/some/page.html", nil)
	req.Header.Set("Accept", "text/html")
	svc.Gate("grocery", echoHandler()).ServeHTTP(rec, req)
	mustNotReached(t, rec)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	ct := rec.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json (only GET gets the HTML page)", ct)
	}
	body := rec.Body.String()
	if contains(body, "pin-form") || contains(body, "<!DOCTYPE") {
		t.Fatalf("body = %q, must not be the HTML login page", body)
	}
}

func TestLoginPageNeverServedOnAPIPath(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	svc := newGateService(t, now, &Policy{Modules: map[string][]string{"admin": {}}})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/settings", nil)
	req.Header.Set("Accept", "text/html")
	svc.Gate("admin", echoHandler()).ServeHTTP(rec, req)
	mustNotReached(t, rec)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	ct := rec.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json (T4.2 step 6: /api/ never gets the login page)", ct)
	}
	body := rec.Body.String()
	if !strings.HasPrefix(strings.TrimSpace(body), "{") {
		t.Fatalf("body = %q, want the JSON envelope", body)
	}
	if contains(body, "pin-form") || contains(body, "<!DOCTYPE") {
		t.Fatalf("body = %q, must never be the HTML login page on an /api/ path", body)
	}
}

// TestLoginPageContainsModeFetchAndMembershipChecks is a cheap smoke test on
// the embedded page bytes: the JS must fetch /api/auth/mode and gate its
// forms on the same method vocabulary the gate/mode response use --
// including "admin_pin" (L6: admin's break-glass path) and "passkey".
func TestLoginPageContainsModeFetchAndMembershipChecks(t *testing.T) {
	body := string(loginPageHTML)

	if !contains(body, "/api/auth/mode") {
		t.Fatalf("embedded page does not fetch /api/auth/mode")
	}
	if !contains(body, "admin_pin") {
		t.Fatalf("embedded page JS does not reference \"admin_pin\" (PIN form must show for admin's break-glass method)")
	}
	if !contains(body, "passkey") {
		t.Fatalf("embedded page JS does not reference \"passkey\"")
	}
	if !contains(body, "/api/auth/login") {
		t.Fatalf("embedded page does not post to /api/auth/login")
	}
	if !contains(body, "/api/auth/passkey/login/begin") || !contains(body, "/api/auth/passkey/login/finish") {
		t.Fatalf("embedded page does not drive the passkey login ceremony")
	}
}

// TestLoginPageAdminEmptyMatrixShowsPINForm proves the mode-driven form
// selection logic in the embedded JS classifies methods=["admin_pin"] (the
// admin empty-matrix case, T4.2 step 2) as "show the PIN form": the JS must
// check membership of both "pin" and "admin_pin" when deciding to reveal
// #pin-form, so an admin whose matrix has no "pin" entry still gets a form.
func TestLoginPageAdminEmptyMatrixShowsPINForm(t *testing.T) {
	body := string(loginPageHTML)

	pinFormIdx := strings.Index(body, `id="pin-form"`)
	if pinFormIdx < 0 {
		t.Fatalf("embedded page missing id=\"pin-form\"")
	}

	hasPinIdx := strings.Index(body, `hasPin`)
	if hasPinIdx < 0 {
		t.Fatalf("embedded page missing the hasPin membership check")
	}
	// The hasPin check must consult both "pin" and "admin_pin" so the
	// empty-matrix admin page (mode == ["admin_pin"]) still shows the form.
	windowEnd := hasPinIdx + 400
	if windowEnd > len(body) {
		windowEnd = len(body)
	}
	checkWindow := body[hasPinIdx:windowEnd]
	if !contains(checkWindow, `"pin"`) || !contains(checkWindow, `"admin_pin"`) {
		t.Fatalf("hasPin check does not test both \"pin\" and \"admin_pin\": %q", checkWindow)
	}

	// And end-to-end via the gate: admin's mode response for an empty
	// matrix includes "admin_pin" (already covered in gate_test.go's
	// TestGateModeReportsAcceptedMethods; re-asserted here as the
	// companion half of the JS check above).
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	svc := newGateService(t, now, &Policy{Modules: map[string][]string{}})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/auth/mode", nil)
	svc.Gate("admin", echoHandler()).ServeHTTP(rec, req)
	if !contains(rec.Body.String(), "admin_pin") {
		t.Fatalf("admin empty-matrix mode body = %q, want it to contain admin_pin", rec.Body.String())
	}
}

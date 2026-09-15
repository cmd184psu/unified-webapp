package actor

import (
	"net/http/httptest"
	"path/filepath"
	"testing"

	"cmd184psu/unified-webapp/internal/issuetracker/db"
	"cmd184psu/unified-webapp/internal/issuetracker/store"
	"cmd184psu/unified-webapp/internal/platform/auth"
)

func testStore(t *testing.T) *store.Store {
	t.Helper()
	database, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	return store.New(database)
}

func TestResolveOpenModeDefaultUser(t *testing.T) {
	st := testStore(t)
	r := httptest.NewRequest("GET", "/", nil) // no principal on context
	u, err := Resolve(r, st, "Nobody", "nobody@example.com")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if u.Username != "nobody@example.com" || u.Name != "Nobody" {
		t.Errorf("open-mode actor = %+v, want username=nobody@example.com name=Nobody", u)
	}
}

func TestResolveAPIKey(t *testing.T) {
	st := testStore(t)
	r := httptest.NewRequest("POST", "/graphql", nil)
	r = r.WithContext(auth.WithPrincipal(r.Context(), auth.Principal{Method: "apikey", Subject: "svc-key"}))
	u, err := Resolve(r, st, "Nobody", "nobody@example.com")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if u.Username != "api" {
		t.Errorf("api-key actor username = %q, want api", u.Username)
	}
}

func TestResolveLDAPUser(t *testing.T) {
	st := testStore(t)
	r := httptest.NewRequest("GET", "/api/issues", nil)
	r = r.WithContext(auth.WithPrincipal(r.Context(), auth.Principal{Method: "ldap", Subject: "alice"}))
	u, err := Resolve(r, st, "Nobody", "nobody@example.com")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if u.Username != "alice" {
		t.Errorf("ldap actor username = %q, want alice", u.Username)
	}
	// Same subject resolves to the same row (find-or-create is stable).
	u2, _ := Resolve(r, st, "Nobody", "nobody@example.com")
	if u2.ID != u.ID {
		t.Errorf("ldap actor not stable: %q != %q", u2.ID, u.ID)
	}
}

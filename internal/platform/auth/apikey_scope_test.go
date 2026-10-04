package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"cmd184psu/unified-webapp/internal/platform/config"
)

// An API key works only on the modules it is scoped to; "*" (or a legacy key
// with no scope at all) means every protected module. Same isolation principle
// as the session tests: one module's credential grants nothing on another.
func TestAPIKeyScopedToItsModules(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	mods := map[string]ModulePolicy{}
	for _, m := range isolationModules {
		mods[m] = ModulePolicy{}
	}
	p := &Policy{
		Modules: mods,
		APIKeys: []config.NamedHash{
			{Name: "only-todo", Hash: hashKey("k-todo"), Modules: []string{"todo"}},
			{Name: "two", Hash: hashKey("k-two"), Modules: []string{"todo", "grocery"}},
			{Name: "everything", Hash: hashKey("k-all"), Modules: []string{"*"}},
			{Name: "legacy", Hash: hashKey("k-legacy")}, // no scope: treated as all
		},
	}
	svc := newGateService(t, now, p)

	allowed := map[string]map[string]bool{
		"k-todo":   {"todo": true},
		"k-two":    {"todo": true, "grocery": true},
		"k-all":    {"todo": true, "grocery": true, "smbedit": true, "multissh": true},
		"k-legacy": {"todo": true, "grocery": true, "smbedit": true, "multissh": true},
	}
	for key, want := range allowed {
		for _, mod := range isolationModules {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/api/items", nil)
			req.Header.Set("Authorization", "Bearer "+key)
			svc.Gate(mod, echoHandler()).ServeHTTP(rec, req)
			if want[mod] {
				if rec.Code != http.StatusOK {
					t.Errorf("key %s on %s = %d, want 200", key, mod, rec.Code)
				}
			} else if rec.Code != http.StatusUnauthorized {
				t.Errorf("key %s on %s = %d, want 401 (not in its scope)", key, mod, rec.Code)
			}
		}
	}
}

func TestValidatePolicyRejectsKeyScopeNamingAnUnknownModuleOrAdmin(t *testing.T) {
	known := []string{"todo", "grocery"}
	ok := config.AuthConfig{APIKeys: []config.NamedHash{{Name: "a", Hash: "x", Modules: []string{"todo", "*"}}}}
	if err := ValidatePolicy(ok, known, false); err != nil {
		t.Fatalf("valid scope rejected: %v", err)
	}
	for _, bad := range []string{"nope", "admin"} {
		a := config.AuthConfig{APIKeys: []config.NamedHash{{Name: "a", Hash: "x", Modules: []string{bad}}}}
		if err := ValidatePolicy(a, known, false); err == nil {
			t.Errorf("scope %q accepted", bad)
		}
	}
}

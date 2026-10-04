package admin

import (
	"encoding/json"
	"net/http"
	"reflect"
	"testing"

	"cmd184psu/unified-webapp/internal/platform/config"
)

func TestNormalizeKeyScope(t *testing.T) {
	known := []string{"todo", "grocery", "smbedit"}
	good := []struct {
		in   []string
		want []string
	}{
		{[]string{"todo"}, []string{"todo"}},
		{[]string{" smbedit ", "todo", "todo"}, []string{"smbedit", "todo"}},
		{[]string{"*"}, []string{"*"}},
		{[]string{"todo", "*"}, []string{"*"}},
	}
	for _, c := range good {
		got, err := NormalizeKeyScope(c.in, known)
		if err != nil || !reflect.DeepEqual(got, c.want) {
			t.Errorf("NormalizeKeyScope(%v) = %v, %v; want %v", c.in, got, err, c.want)
		}
	}
	for _, in := range [][]string{nil, {}, {""}, {"admin"}, {"nope"}} {
		if got, err := NormalizeKeyScope(in, known); err == nil {
			t.Errorf("NormalizeKeyScope(%v) = %v, want an error (a key is never unscoped)", in, got)
		}
	}
}

func TestKeyScopeViewShowsLegacyAsAll(t *testing.T) {
	if got := keyScopeView(nil); !reflect.DeepEqual(got, []string{"*"}) {
		t.Errorf("legacy view = %v", got)
	}
	if got := keyScopeView([]string{"todo"}); !reflect.DeepEqual(got, []string{"todo"}) {
		t.Errorf("scoped view = %v", got)
	}
}

func TestKeyRoutesRequireAndUpdateScope(t *testing.T) {
	_, mux, _ := newAdminTestHandler(t, config.AuthConfig{}, []string{"todo", "grocery"}, false)

	for _, body := range []map[string]any{
		{"name": "k"},                                    // no scope: not allowed
		{"name": "k", "modules": []string{}},             // empty
		{"name": "k", "modules": []string{"admin"}},      // admin never takes a key
		{"name": "k", "modules": []string{"no-module"}},  // not a module
	} {
		if rec := doAdmin(t, mux, http.MethodPost, "/api/keys", body); rec.Code != http.StatusBadRequest {
			t.Errorf("POST %v = %d, want 400", body, rec.Code)
		}
	}

	rec := doAdmin(t, mux, http.MethodPost, "/api/keys", map[string]any{"name": "k", "modules": []string{"todo"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("POST scoped = %d: %s", rec.Code, rec.Body.String())
	}
	if rec := doAdmin(t, mux, http.MethodPut, "/api/keys/k", map[string]any{"modules": []string{"*"}}); rec.Code != http.StatusOK {
		t.Fatalf("PUT scope = %d: %s", rec.Code, rec.Body.String())
	}
	if rec := doAdmin(t, mux, http.MethodPut, "/api/keys/k", map[string]any{"modules": []string{}}); rec.Code != http.StatusBadRequest {
		t.Errorf("PUT empty scope = %d, want 400", rec.Code)
	}
	if rec := doAdmin(t, mux, http.MethodPut, "/api/keys/missing", map[string]any{"modules": []string{"todo"}}); rec.Code != http.StatusNotFound {
		t.Errorf("PUT unknown key = %d, want 404", rec.Code)
	}

	get := doAdmin(t, mux, http.MethodGet, "/api/config/auth", nil)
	var view struct {
		APIKeys []struct {
			Name    string   `json:"name"`
			Modules []string `json:"modules"`
		} `json:"api_keys"`
	}
	if err := json.Unmarshal(get.Body.Bytes(), &view); err != nil || len(view.APIKeys) != 1 || !reflect.DeepEqual(view.APIKeys[0].Modules, []string{"*"}) {
		t.Fatalf("view = %+v, err %v; body %s", view, err, get.Body.String())
	}
}

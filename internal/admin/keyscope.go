package admin

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"cmd184psu/unified-webapp/internal/platform/config"
)

// NormalizeKeyScope turns a requested API-key scope into its stored form: the
// sorted, de-duplicated module names, or just ["*"] when the key works on every
// module. A key is never unscoped, so an empty request is an error. admin never
// accepts an API key, and every name must be a real module.
func NormalizeKeyScope(requested, known []string) ([]string, error) {
	isKnown := make(map[string]bool, len(known))
	for _, m := range known {
		isKnown[m] = true
	}
	seen := map[string]bool{}
	var out []string
	for _, m := range requested {
		m = strings.TrimSpace(m)
		switch {
		case m == "":
			continue
		case m == config.AllModules:
			return []string{config.AllModules}, nil
		case m == "admin":
			return nil, errors.New("admin never accepts an API key")
		case len(known) > 0 && !isKnown[m]:
			return nil, fmt.Errorf("%q is not a module", m)
		}
		if !seen[m] {
			seen[m] = true
			out = append(out, m)
		}
	}
	if len(out) == 0 {
		return nil, errors.New("choose the modules this key works on, or all modules")
	}
	sort.Strings(out)
	return out, nil
}

// upsertKey is upsertNamedHash for API keys: it also sets the key's scope.
func upsertKey(list []config.NamedHash, name, hash string, modules []string) []config.NamedHash {
	out := make([]config.NamedHash, len(list))
	copy(out, list)
	for i, e := range out {
		if e.Name == name {
			out[i].Hash, out[i].Modules = hash, modules
			return out
		}
	}
	return append(out, config.NamedHash{Name: name, Hash: hash, Modules: modules})
}

// keyScopeView is how a key's scope is shown: a legacy key with no scope works
// everywhere, so it is shown (and from the next save, stored) as "*".
func keyScopeView(modules []string) []string {
	if len(modules) == 0 {
		return []string{config.AllModules}
	}
	return append([]string(nil), modules...)
}

// Package auth defines the shared authentication/authorization policy
// surface used across modules. It has no knowledge of and no dependency on
// any specific module package (grocery, todo, etc.) — it only understands
// the generic shape of module names and auth methods.
package auth

import (
	"fmt"
	"os"
	"sort"

	"cmd184psu/unified-webapp/internal/platform/config"
)

// validMethods is the set of auth methods a module may list.
var validMethods = map[string]bool{
	"ldap":    true,
	"pin":     true,
	"passkey": true,
	"key":     true,
}

// ValidatePolicy checks a for internal consistency: every module referenced
// in a.Modules must be known (or the reserved "admin" pseudo-module), every
// listed method must be recognized and have its corresponding authenticator
// configured, and the admin operator PIN must be configured exactly once if
// the admin module is routed through auth. It performs no authentication and
// has no side effects beyond stat-ing AdminPINFile when set.
func ValidatePolicy(a config.AuthConfig, knownModules []string, adminRouted bool) error {
	// Fast path: nothing configured and admin isn't routed through auth, so
	// there is nothing to validate. This also covers a zero-value AuthConfig
	// (no "auth" key at all in the config file).
	if len(a.Modules) == 0 && a.AdminPIN == "" && a.AdminPINFile == "" && !adminRouted {
		return nil
	}

	known := make(map[string]bool, len(knownModules))
	for _, m := range knownModules {
		known[m] = true
	}

	// Sort module names for deterministic error messages.
	modNames := make([]string, 0, len(a.Modules))
	for m := range a.Modules {
		modNames = append(modNames, m)
	}
	sort.Strings(modNames)

	usedMethods := make(map[string]bool)
	for _, mod := range modNames {
		if mod != "admin" && !known[mod] {
			return fmt.Errorf("auth.modules: unknown module %q", mod)
		}
		for _, method := range a.Modules[mod] {
			if method == "admin_pin" {
				return fmt.Errorf("auth.modules: module %q lists %q, which is a reserved token and can never be configured as a method", mod, method)
			}
			if !validMethods[method] {
				return fmt.Errorf("auth.modules: module %q lists unknown auth method %q", mod, method)
			}
			usedMethods[method] = true
		}
	}

	if usedMethods["ldap"] && a.LDAP.URL == "" {
		return fmt.Errorf("auth: method %q is configured for a module but auth.ldap.url is not set", "ldap")
	}
	if usedMethods["pin"] && len(a.PINs) == 0 {
		return fmt.Errorf("auth: method %q is configured for a module but auth.pins is empty", "pin")
	}
	if usedMethods["key"] && len(a.APIKeys) == 0 {
		return fmt.Errorf("auth: method %q is configured for a module but auth.api_keys is empty", "key")
	}
	if usedMethods["passkey"] && a.Passkey.RPID == "" {
		return fmt.Errorf("auth: method %q is configured for a module but auth.passkey.rp_id is not set", "passkey")
	}

	if adminRouted && a.AdminPIN == "" && a.AdminPINFile == "" {
		return fmt.Errorf("auth: the admin module requires an operator PIN (set auth.admin_pin or auth.admin_pin_file)")
	}
	if a.AdminPIN != "" && a.AdminPINFile != "" {
		return fmt.Errorf("auth: exactly one of auth.admin_pin or auth.admin_pin_file may be set, not both")
	}
	if a.AdminPINFile != "" {
		info, err := os.Stat(a.AdminPINFile)
		if err != nil {
			return fmt.Errorf("auth.admin_pin_file: %w", err)
		}
		if info.Mode().Perm()&0077 != 0 {
			return fmt.Errorf("auth.admin_pin_file: permissions %#o are too open; chmod 0400 %s", info.Mode().Perm(), a.AdminPINFile)
		}
	}

	return nil
}

// Package auth defines the shared authentication/authorization policy
// surface used across modules. It has no knowledge of and no dependency on
// any specific module package (grocery, todo, etc.) — it only understands
// the generic shape of module names and the two-state auth model.
package auth

import (
	"fmt"
	"sort"

	"cmd184psu/unified-webapp/internal/platform/config"
)

// ValidatePolicy checks a for internal consistency under the two-state auth
// model: every module referenced in a.Modules must be known (or the reserved
// "admin" pseudo-module); any protected non-admin module requires LDAP to be
// configured (LDAP is the identity backbone every protected module relies
// on); every configured pin_file -- a module door code, or, via
// modules.admin.pin_file, one of the admin PIN sources -- must exist and be
// readable by the process only; and the admin operator PIN must be
// configured through exactly one of its three sources if the admin module is
// routed through auth. api_keys may be empty and the passkey config is
// entirely optional -- neither has a required-when-used rule any more. It
// performs no authentication and has no side effects beyond stat-ing pin
// files.
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

	for _, mod := range modNames {
		if mod != "admin" && !known[mod] {
			return fmt.Errorf("auth.modules: unknown module %q", mod)
		}
	}

	for _, mod := range modNames {
		if mod == "admin" {
			continue
		}
		if a.LDAP.URL == "" {
			return fmt.Errorf("auth.modules: module %q is protected but auth.ldap.url is not set", mod)
		}
	}

	for _, mod := range modNames {
		pinFile := a.Modules[mod].PinFile
		if pinFile == "" {
			continue
		}
		if err := statPINFile(pinFile); err != nil {
			return fmt.Errorf("auth.modules.%s.pin_file: %w", mod, err)
		}
	}

	adminModPinFile := ""
	if m, ok := a.Modules["admin"]; ok {
		adminModPinFile = m.PinFile
	}

	adminSources := 0
	if a.AdminPIN != "" {
		adminSources++
	}
	if a.AdminPINFile != "" {
		adminSources++
	}
	if adminModPinFile != "" {
		adminSources++
	}
	if adminSources > 1 {
		return fmt.Errorf("auth: exactly one of auth.admin_pin, auth.admin_pin_file, or auth.modules.admin.pin_file may be set")
	}
	if adminRouted && adminSources == 0 {
		return fmt.Errorf("auth: the admin module requires an operator PIN (set auth.admin_pin, auth.admin_pin_file, or auth.modules.admin.pin_file)")
	}

	if a.AdminPINFile != "" {
		if err := statPINFile(a.AdminPINFile); err != nil {
			return fmt.Errorf("auth.admin_pin_file: %w", err)
		}
	}

	return nil
}

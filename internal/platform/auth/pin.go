package auth

import (
	"crypto/subtle"
	"fmt"
	"os"
	"strings"

	"golang.org/x/crypto/bcrypt"

	"cmd184psu/unified-webapp/internal/platform/config"
)

// Reserved operator identity and token methods (L6): the operator PIN
// authenticates as the fixed identity adminIdentity via the distinct token
// method adminPINMethod, which validate.go refuses to accept as a
// configurable auth.modules entry so a named PIN can never impersonate it.
// A named-PIN login instead records pinMethod.
const (
	adminIdentity  = "admin"
	adminPINMethod = "admin_pin"
	pinMethod      = "pin"
)

// checkPIN compares pin against every entry in pins (bcrypt), returning the
// name of the first match. The plaintext pin is never logged or included in
// any returned value or error.
func checkPIN(pins []config.NamedHash, pin string) (name string, ok bool) {
	for _, entry := range pins {
		if bcrypt.CompareHashAndPassword([]byte(entry.Hash), []byte(pin)) == nil {
			return entry.Name, true
		}
	}
	return "", false
}

// checkAdminPIN validates pin against the operator PIN, configured exactly
// one of two ways: an inline bcrypt hash (adminPIN) or a plaintext PIN file
// (adminPINFile). When neither is configured it returns (false, nil) --
// there is no operator PIN to check.
//
// The file form is re-read on every call (deliberately -- an operator
// editing the file takes effect on the very next login attempt, with no
// caching) and its permission mode is re-checked on every read, refusing
// with an error naming the fix when the mode is too open. No error message
// generated here ever includes the attempted pin or the file's contents.
func checkAdminPIN(adminPIN, adminPINFile, pin string) (ok bool, err error) {
	if adminPIN != "" {
		return bcrypt.CompareHashAndPassword([]byte(adminPIN), []byte(pin)) == nil, nil
	}
	if adminPINFile != "" {
		info, err := os.Stat(adminPINFile)
		if err != nil {
			return false, fmt.Errorf("auth.admin_pin_file: %w", err)
		}
		if info.Mode().Perm()&0077 != 0 {
			return false, fmt.Errorf("auth.admin_pin_file: permissions %#o are too open; chmod 0400 %s", info.Mode().Perm(), adminPINFile)
		}
		data, err := os.ReadFile(adminPINFile)
		if err != nil {
			return false, fmt.Errorf("auth.admin_pin_file: %w", err)
		}
		filePIN := strings.TrimSpace(string(data))
		return subtle.ConstantTimeCompare([]byte(filePIN), []byte(pin)) == 1, nil
	}
	return false, nil
}

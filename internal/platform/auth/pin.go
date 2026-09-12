package auth

import (
	"crypto/subtle"
	"fmt"
	"os"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

// Reserved operator identity and grant vocabulary tokens (L6): the operator
// PIN authenticates as the fixed identity adminIdentity via the distinct
// grant adminPINMethod. pinMethod is the login-surface token for a module's
// own door code (OfferedMethods); the actual grant a door-code login
// produces is scoped per module (session.go's pinGrant), never the bare
// pinMethod string.
const (
	adminIdentity  = "admin"
	adminPINMethod = "admin_pin"
	pinMethod      = "pin"
)

// statPINFile verifies that path exists and is not group/other readable
// (permissions & 0077 == 0) -- the permission rule the operator PIN file has
// always enforced, now shared by every configured pin_file. It is the single
// source of truth for that check: checkPINFile below calls it on every login
// attempt, and validate.go's ValidatePolicy calls it once per configured
// pin_file at boot (and on every live-apply).
func statPINFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if info.Mode().Perm()&0077 != 0 {
		return fmt.Errorf("%s: permissions %#o are too open; chmod 0400 %s", path, info.Mode().Perm(), path)
	}
	return nil
}

// checkPINFile validates pin against the plaintext PIN stored in the file at
// path -- a per-module door code, or (via checkAdminPIN) the operator PIN
// file. The file is re-read and its permissions re-checked on every call,
// deliberately uncached, so an operator editing or rotating it on disk takes
// effect on the very next login attempt. Comparison is constant-time; no
// error returned here ever includes the attempted pin or the file's
// contents.
func checkPINFile(path, pin string) (ok bool, err error) {
	if err := statPINFile(path); err != nil {
		return false, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false, fmt.Errorf("%s: %w", path, err)
	}
	filePIN := strings.TrimSpace(string(data))
	return subtle.ConstantTimeCompare([]byte(filePIN), []byte(pin)) == 1, nil
}

// checkAdminPIN validates pin against the operator PIN, configured exactly
// one of two ways: an inline bcrypt hash (adminPIN) or a plaintext PIN file
// (adminPINFile, delegating the file branch to checkPINFile). When neither
// is configured it returns (false, nil) -- there is no operator PIN to
// check. (A third spelling, modules.admin.pin_file, is folded into
// adminPINFile by policy.go's BuildPolicy before this function ever sees
// it.)
func checkAdminPIN(adminPIN, adminPINFile, pin string) (ok bool, err error) {
	if adminPIN != "" {
		return bcrypt.CompareHashAndPassword([]byte(adminPIN), []byte(pin)) == nil, nil
	}
	if adminPINFile != "" {
		ok, err := checkPINFile(adminPINFile, pin)
		if err != nil {
			return false, fmt.Errorf("auth.admin_pin_file: %w", err)
		}
		return ok, nil
	}
	return false, nil
}

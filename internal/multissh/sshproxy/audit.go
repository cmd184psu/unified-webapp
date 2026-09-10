package sshproxy

import "log"

// Auditf writes one operator-facing audit line to the standard logger, which
// on a systemd install lands in journald (`journalctl -u unified`).
//
// Every call site passes host, user and outcome and nothing else. No
// credential -- not a Secret, not a key path, not a ConnectParams -- is ever
// an argument here; audit lines are greppable evidence of what was reached,
// not of what it was reached with. TestAuditLinesCarryNoCredential pins that.
func Auditf(format string, args ...any) {
	log.Printf("multissh: audit "+format, args...)
}

// authLabel names the credential kind for an audit line without revealing it.
func authLabel(p ConnectParams) string {
	if !p.Password.IsZero() {
		return "password"
	}
	return "key"
}

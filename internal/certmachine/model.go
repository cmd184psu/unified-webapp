package certmachine

// Cert status values. "expired" is never stored -- expiry is a property
// clients derive from notAfter at read time, never persisted (FR-2).
const (
	StatusActive      = "active"
	StatusArchived    = "archived"
	StatusQuarantined = "quarantined"
)

// SANs is the certs.sans column: the subject alternative names parsed from a
// certificate, stored as JSON. It is never derived from meta.json (§2.1
// Principle 1) -- always reparsed from cert_pem.
type SANs struct {
	DNS []string `json:"dns"`
	IP  []string `json:"ip"`
}

// CA is a certificate authority row. Since schema v2 (the CA-replacement
// plan) ca can hold up to two rows, distinguished by Role ("current" or
// "previous"); FR-3's original singleton is now "at most one current". All
// fields but Role are derived by parsing cert_pem at insert time; nothing
// here is read back from a legacy meta.json.
type CA struct {
	ID           int64   `json:"id"`
	Role         string  `json:"role"`
	CertPEM      string  `json:"certPem"`
	KeyPEM       string  `json:"-"`
	Subject      string  `json:"subject"`
	Serial       string  `json:"serial"`
	NotBefore    string  `json:"notBefore"`
	NotAfter     string  `json:"notAfter"`
	Fingerprint  string  `json:"fingerprint"`
	ImportedFrom *string `json:"importedFrom,omitempty"`
	Created      string  `json:"created"`
}

// Cert is one leaf certificate row.
//
// Serial, NotBefore, NotAfter and Fingerprint are pointers because they are
// NULL -- never "" -- when the stored certificate did not parse (the
// certs_serial binding decision in the schema doc). KeyPEM is never
// serialized to JSON; CertPEM is included only where a slice-later handler
// explicitly opts in (GetCert), which is why it carries omitempty rather than
// being unconditionally present.
//
// CAID, CASubject and Stale are the CA-replacement plan's additions (§3.2).
// CAID is nil when the signer is unknown (P1); CASubject is filled only
// where a caller joins against ca; Stale holds exactly when CAID is non-nil
// and differs from the current CA's id (§3.3, invariant 5).
type Cert struct {
	ID               int64   `json:"id"`
	FQDN             string  `json:"fqdn"`
	Serial           *string `json:"serial"`
	NotBefore        *string `json:"notBefore"`
	NotAfter         *string `json:"notAfter"`
	SANs             SANs    `json:"sans"`
	Fingerprint      *string `json:"fingerprint"`
	Status           string  `json:"status"`
	CertPEM          *string `json:"certPem,omitempty"`
	KeyPEM           *string `json:"-"`
	ImportedFrom     *string `json:"importedFrom,omitempty"`
	QuarantineReason *string `json:"quarantineReason,omitempty"`
	ImportWarning    *string `json:"importWarning,omitempty"`
	CAID             *int64  `json:"caId"`
	CASubject        *string `json:"caSubject,omitempty"`
	Stale            bool    `json:"stale"`
	Created          string  `json:"created"`
}

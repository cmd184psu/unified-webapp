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

// CA is the singleton certificate authority row (ca.id = 1, FR-3). All fields
// are derived by parsing cert_pem at insert time; nothing here is read back
// from a legacy meta.json.
type CA struct {
	ID           int64   `json:"id"`
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
	Created          string  `json:"created"`
}

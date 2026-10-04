package haproxy

// Cert details and expiry state, both derived ONLY from what CertMachine
// supplies (D15, FR-H11, FR-H15). Nothing here parses a certificate, computes a
// fingerprint, or reads a PEM; it reshapes CertMachine's own data and compares
// its notAfter against the expiry_warn_days setting.

import (
	"time"
)

// CertExpiryState is FR-H15's three-way expiry classification.
type CertExpiryState string

const (
	CertExpiryOK      CertExpiryState = "ok"
	CertExpirySoon    CertExpiryState = "soon"
	CertExpiryExpired CertExpiryState = "expired"
)

// CertDetails is the per-cert information the UI shows (FR-H11), surfaced from
// CertMachine. Issuer is present only when CertMachine supplies it (its signer
// subject); it is never guessed or parsed locally.
type CertDetails struct {
	FQDN      string          `json:"fqdn"`
	SANsDNS   []string        `json:"sansDns"`
	SANsIP    []string        `json:"sansIp"`
	NotBefore string          `json:"notBefore,omitempty"`
	NotAfter  string          `json:"notAfter,omitempty"`
	Status    string          `json:"status"`
	Issuer    string          `json:"issuer,omitempty"`
	Expiry    CertExpiryState `json:"expiry,omitempty"`
}

// CertDetailsFrom reshapes a CertMachine cert into the UI's detail view and
// computes its expiry state against warnDays. now is passed in so the result
// is deterministic in tests.
func CertDetailsFrom(c CertMachineCert, now time.Time, warnDays int) CertDetails {
	d := CertDetails{
		FQDN:    c.FQDN,
		SANsDNS: c.SANs.DNS,
		SANsIP:  c.SANs.IP,
		Status:  c.Status,
	}
	if c.CASubject != nil {
		d.Issuer = *c.CASubject
	}
	if c.NotBefore != nil {
		d.NotBefore = *c.NotBefore
	}
	if c.NotAfter != nil {
		d.NotAfter = *c.NotAfter
		if state, err := CertExpiryStateFor(*c.NotAfter, now, warnDays); err == nil {
			d.Expiry = state
		}
	}
	return d
}

// CertExpiryStateFor classifies a notAfter (CertMachine's RFC3339 string)
// against now and warnDays: expired once now is at or past it, soon once now is
// within warnDays of it, otherwise ok. An unparseable value is an error.
func CertExpiryStateFor(notAfter string, now time.Time, warnDays int) (CertExpiryState, error) {
	na, err := time.Parse(time.RFC3339, notAfter)
	if err != nil {
		return "", err
	}
	if !now.Before(na) {
		return CertExpiryExpired, nil
	}
	warnThreshold := na.Add(-time.Duration(warnDays) * 24 * time.Hour)
	if !now.Before(warnThreshold) {
		return CertExpirySoon, nil
	}
	return CertExpiryOK, nil
}

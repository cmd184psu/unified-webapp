// lifecycle.go implements the certificate authority and leaf lifecycle
// operations: InitCA, Generate, Renew, Delete (FR-3, FR-4, FR-5, FR-6). It is
// the only place expiry_warn_days and default_validity_days -- both
// operator-configured, never hardcoded -- become inputs to a decision, and
// the only place "crypto happens before the transaction opens" is enforced
// for leaf issuance: GenerateLeaf runs to completion, and only then does
// WithTx run the insert(s), so an RSA failure never leaves a partial row.
package certmachine

import (
	"context"
	"crypto/x509"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Sentinel errors for the lifecycle operations in this file. ErrQuarantined
// and ErrConfirmMismatch are declared in store.go (per the slice 2 binding
// decision) but are raised here, by Renew and Delete respectively.
var (
	// ErrImportPending means InitCA was called against an empty database
	// while the configured legacy_import_dir still holds a parseable
	// rootCA.crt. Initializing a new CA now would occupy the ca singleton
	// and permanently orphan the legacy root -- FR-6 provides no CA
	// rotation or deletion route through the API, so the wizard's later
	// ImportCA would hit a fingerprint mismatch with no clean way back.
	// Import must run first; never the reverse.
	ErrImportPending = errors.New("certmachine: cannot initialize a certificate authority while a legacy import is pending")

	// ErrCAExpiringSoon means the stored CA has fewer than expiry_warn_days
	// of life left. Generate and Renew both refuse rather than mint a
	// certificate that a client would soon distrust along with its issuer.
	ErrCAExpiringSoon = errors.New("certmachine: certificate authority is too close to expiry to issue or renew certificates")

	// ErrNewerActiveExists means Renew was called on an archived row while
	// a newer active row already exists for the same fqdn. Renewing the
	// archived row would resurrect a stale SAN set and silently discard
	// the active row's -- the operator renews the active row instead.
	ErrNewerActiveExists = errors.New("certmachine: a newer active certificate already exists for this fqdn")
)

// IssueResult is what Generate and Renew return for the newly issued leaf.
// Cert.NotAfter already carries the actual (possibly clamped) expiry;
// Clamped and RequestedNotAfter surface GenerateLeaf's validity clamp
// (LeafResult, pki.go) so a caller can report FR-4's validityClamped fact
// rather than silently issuing a shorter certificate.
type IssueResult struct {
	Cert              Cert
	Clamped           bool
	RequestedNotAfter time.Time
}

// InitCA creates a new root CA (RSA-4096, ~10-year, CN "CertMachine Root
// CA") and stores it as the ca singleton. There is no force flag: a CA can
// be created at most once through this method, ever -- InsertCA's own
// pre-check makes ErrCAExists deterministic on any later call.
//
// InitCA also refuses with ErrImportPending when the database holds no
// certs yet and legacyImportDir contains a parseable rootCA.crt: initializing
// first would occupy the ca singleton with a new, unrelated root and leave
// the legacy one unimportable (see ErrImportPending). Running the import
// first, then InitCA (a no-op at that point, since ImportCA will already
// have stored the CA), is the only supported order.
func (s *Store) InitCA(ctx context.Context, legacyImportDir string) error {
	switch _, err := s.GetCA(ctx); {
	case err == nil:
		return ErrCAExists
	case !errors.Is(err, ErrCANotFound):
		return err
	}

	n, err := s.CountCerts(ctx)
	if err != nil {
		return err
	}
	if n == 0 && legacyRootPending(legacyImportDir) {
		return fmt.Errorf("%w: %s contains a parseable %s; import it before initializing a new certificate authority -- initializing first would orphan the legacy root with no route back except: %s",
			ErrImportPending, legacyImportDir, legacyRootCertFilename, caReplacementRemedy)
	}

	certPEM, keyPEM, err := GenerateCA()
	if err != nil {
		return err
	}
	cert, err := ParseCert(certPEM)
	if err != nil {
		return fmt.Errorf("certmachine: parse generated ca cert: %w", err)
	}

	ca := CA{
		CertPEM:     string(certPEM),
		KeyPEM:      string(keyPEM),
		Subject:     cert.Subject.CommonName,
		Serial:      SerialString(cert.SerialNumber),
		NotBefore:   cert.NotBefore.UTC().Format(time.RFC3339),
		NotAfter:    cert.NotAfter.UTC().Format(time.RFC3339),
		Fingerprint: Fingerprint(cert.Raw),
		Created:     time.Now().UTC().Format(time.RFC3339),
	}
	return s.WithTx(ctx, func(tx *sql.Tx) error {
		return s.InsertCA(ctx, tx, ca)
	})
}

// legacyRootPending reports whether dir (the configured legacy_import_dir)
// contains a parseable rootCA.crt -- the condition under which InitCA must
// refuse (see ErrImportPending). Only the certificate is checked, not the
// key: InitCA cares whether an import is available to run, not whether it
// would ultimately succeed -- ImportCA itself verifies the key pair before
// storing anything. An empty dir (legacy import not configured) is never
// pending.
func legacyRootPending(dir string) bool {
	if dir == "" {
		return false
	}
	certPEM, err := os.ReadFile(filepath.Join(dir, legacyRootCertFilename))
	if err != nil {
		return false
	}
	_, err = ParseCert(certPEM)
	return err == nil
}

// checkCAExpiry refuses (ErrCAExpiringSoon) when caCert has fewer than
// expiryWarnDays of life left. Generate and Renew share this refusal and its
// threshold -- expiry_warn_days, not a hardcoded 30 -- so the deployment has
// exactly one notion of "about to expire", the same number the UI paints
// badges with. The message names the manual remedy: FR-6 provides no CA
// rotation route, so "rotate the CA" would name a button that does not
// exist.
func checkCAExpiry(caCert *x509.Certificate, expiryWarnDays int) error {
	warn := time.Duration(expiryWarnDays) * 24 * time.Hour
	if time.Until(caCert.NotAfter) < warn {
		return fmt.Errorf("%w: the certificate authority expires %s, within the %d-day warning threshold -- %s",
			ErrCAExpiringSoon, caCert.NotAfter.UTC().Format(time.RFC3339), expiryWarnDays, caReplacementRemedy)
	}
	return nil
}

// issueLeaf generates a fresh leaf certificate for fqdn/req under the stored
// CA. It performs no database writes: Generate and Renew each decide how the
// resulting Cert is persisted (a plain insert vs. an archive-then-insert),
// and per the "crypto happens before the transaction opens" binding
// decision, this is exactly the validation/crypto step that must complete
// before either opens WithTx. Fails with ErrCANotFound when no CA exists, or
// ErrCAExpiringSoon when the CA has fewer than expiryWarnDays left.
func (s *Store) issueLeaf(ctx context.Context, fqdn string, req CertRequest, defaultValidityDays, expiryWarnDays int) (Cert, LeafResult, error) {
	ca, err := s.GetCA(ctx)
	if err != nil {
		return Cert{}, LeafResult{}, err
	}
	caCert, err := ParseCert([]byte(ca.CertPEM))
	if err != nil {
		return Cert{}, LeafResult{}, fmt.Errorf("certmachine: parse stored ca cert: %w", err)
	}
	if err := checkCAExpiry(caCert, expiryWarnDays); err != nil {
		return Cert{}, LeafResult{}, err
	}
	caKey, err := ParseKey([]byte(ca.KeyPEM))
	if err != nil {
		return Cert{}, LeafResult{}, fmt.Errorf("certmachine: parse stored ca key: %w", err)
	}

	leaf, err := GenerateLeaf(caCert, caKey, req, defaultValidityDays)
	if err != nil {
		return Cert{}, LeafResult{}, err
	}
	leafCert, err := ParseCert(leaf.CertPEM)
	if err != nil {
		return Cert{}, LeafResult{}, fmt.Errorf("certmachine: parse generated leaf cert: %w", err)
	}

	return certFromLeaf(fqdn, leaf, leafCert), leaf, nil
}

// certFromLeaf builds the Cert row for a freshly issued leaf. Its metadata
// is derived from the parsed leafCert, not from req -- the parsed
// certificate is the only source of truth (Principle 1), and leafCert's
// DNSNames already reflect GenerateLeaf's own FQDN-first dedupe.
func certFromLeaf(fqdn string, leaf LeafResult, leafCert *x509.Certificate) Cert {
	serial := SerialString(leafCert.SerialNumber)
	fingerprint := Fingerprint(leafCert.Raw)
	notBefore := leaf.NotBefore.UTC().Format(time.RFC3339)
	notAfter := leaf.NotAfter.UTC().Format(time.RFC3339)
	certPEM := string(leaf.CertPEM)
	keyPEM := string(leaf.KeyPEM)

	return Cert{
		FQDN:        fqdn,
		Serial:      &serial,
		NotBefore:   &notBefore,
		NotAfter:    &notAfter,
		SANs:        sansFromCert(leafCert),
		Fingerprint: &fingerprint,
		Status:      StatusActive,
		CertPEM:     &certPEM,
		KeyPEM:      &keyPEM,
		Created:     time.Now().UTC().Format(time.RFC3339),
	}
}

// sansFromCert reads a certificate's DNS and IP SANs into the certs.sans
// shape, always as non-nil slices so the column serializes as
// {"dns":[...],"ip":[...]} rather than {"dns":null,...}.
func sansFromCert(cert *x509.Certificate) SANs {
	dns := make([]string, len(cert.DNSNames))
	copy(dns, cert.DNSNames)
	ips := make([]string, 0, len(cert.IPAddresses))
	for _, ip := range cert.IPAddresses {
		ips = append(ips, ip.String())
	}
	return SANs{DNS: dns, IP: ips}
}

// parseStoredIPs re-parses a cert row's stored IP SANs (each already a
// valid net.IP.String() value from a prior sansFromCert) back into net.IP,
// for handing to GenerateLeaf during Renew. A parse failure here means the
// stored data is corrupt, not that a caller supplied bad input -- it is
// reported plainly, not as ErrValidation.
func parseStoredIPs(certID int64, raw []string) ([]net.IP, error) {
	ips := make([]net.IP, 0, len(raw))
	for _, s := range raw {
		ip := net.ParseIP(s)
		if ip == nil {
			return nil, fmt.Errorf("certmachine: stored sans for cert %d contains invalid ip %q", certID, s)
		}
		ips = append(ips, ip)
	}
	return ips, nil
}

// Generate validates and issues a brand-new leaf certificate (FR-4). req
// must already be normalized and validated (ValidateRequest, pki.go) --
// Generate itself performs no input validation. The duplicate-active-fqdn
// conflict (ErrDuplicateActive) is InsertCert's own pre-check, raised
// naturally by the insert below rather than duplicated here.
func (s *Store) Generate(ctx context.Context, req CertRequest, defaultValidityDays, expiryWarnDays int) (IssueResult, error) {
	c, leaf, err := s.issueLeaf(ctx, req.FQDN, req, defaultValidityDays, expiryWarnDays)
	if err != nil {
		return IssueResult{}, err
	}

	var id int64
	if err := s.WithTx(ctx, func(tx *sql.Tx) error {
		var err error
		id, err = s.InsertCert(ctx, tx, c)
		return err
	}); err != nil {
		return IssueResult{}, err
	}
	c.ID = id

	return IssueResult{Cert: c, Clamped: leaf.Clamped, RequestedNotAfter: leaf.RequestedNotAfter}, nil
}

// activeCertForFQDN returns the active row for fqdn, or nil (with a nil
// error) when none exists. Used by Renew to detect the "archived row with a
// newer active row present" conflict (ErrNewerActiveExists).
//
// The comparison is EqualFold, matching Delete's confirmation check and the
// COLLATE NOCASE the certs_one_active_per_fqdn index and ArchiveAllForFQDN
// use: an imported legacy CN keeps its original case (FR-3), so a
// case-sensitive scan here would miss the very active row that makes renewing
// an archived sibling the wrong move.
func (s *Store) activeCertForFQDN(ctx context.Context, fqdn string) (*Cert, error) {
	certs, err := s.ListCerts(ctx)
	if err != nil {
		return nil, err
	}
	for i := range certs {
		if certs[i].Status == StatusActive && strings.EqualFold(certs[i].FQDN, fqdn) {
			return &certs[i], nil
		}
	}
	return nil, nil
}

// Renew re-issues the certificate identified by id: it copies the source
// row's fqdn and full SAN set, generates a fresh key and leaf under the
// stored CA, then archives every active row for that fqdn and inserts the
// new one in one transaction (WithTx) -- keeping
// certs_one_active_per_fqdn satisfied throughout, including when id itself
// is the active row being renewed.
//
// Renew accepts any non-quarantined row -- active, archived, or expired (an
// active row past its own not_after; "expired" is never a stored status).
// Two refusals: a quarantined row has no parseable cert to copy a SAN set
// from (ErrQuarantined), and an archived row is refused when a newer active
// row already exists for the fqdn (ErrNewerActiveExists) -- renewing it
// would resurrect a stale SAN set and discard the newer cert's.
func (s *Store) Renew(ctx context.Context, id int64, defaultValidityDays, expiryWarnDays int) (IssueResult, error) {
	source, err := s.GetCert(ctx, id)
	if err != nil {
		return IssueResult{}, err
	}
	if source.Status == StatusQuarantined {
		return IssueResult{}, ErrQuarantined
	}
	if source.Status == StatusArchived {
		active, err := s.activeCertForFQDN(ctx, source.FQDN)
		if err != nil {
			return IssueResult{}, err
		}
		if active != nil {
			return IssueResult{}, fmt.Errorf("%w: certificate %d is active for %s; renew that one instead", ErrNewerActiveExists, active.ID, source.FQDN)
		}
	}

	ips, err := parseStoredIPs(source.ID, source.SANs.IP)
	if err != nil {
		return IssueResult{}, err
	}
	req := CertRequest{FQDN: source.FQDN, DNSSans: source.SANs.DNS, IPSans: ips}

	c, leaf, err := s.issueLeaf(ctx, source.FQDN, req, defaultValidityDays, expiryWarnDays)
	if err != nil {
		return IssueResult{}, err
	}

	var newID int64
	if err := s.WithTx(ctx, func(tx *sql.Tx) error {
		if err := s.ArchiveAllForFQDN(ctx, tx, source.FQDN); err != nil {
			return err
		}
		var err error
		newID, err = s.InsertCert(ctx, tx, c)
		return err
	}); err != nil {
		return IssueResult{}, err
	}
	c.ID = newID

	return IssueResult{Cert: c, Clamped: leaf.Clamped, RequestedNotAfter: leaf.RequestedNotAfter}, nil
}

// Delete permanently removes the cert row with id (FR-6: a hard delete,
// with no soft-delete or undo). confirmFQDN must equal the row's fqdn
// case-insensitively, or Delete returns ErrConfirmMismatch and removes
// nothing -- FR-6's "explicit confirmation naming the FQDN" is enforced
// here, server-side, because a browser-only confirm() leaves the
// destructive route one stray curl from firing. active, archived, and
// quarantined rows are all deletable this way. There is deliberately no
// function anywhere in this package that deletes the ca row: FR-6 makes the
// CA non-deletable through any code path.
func (s *Store) Delete(ctx context.Context, id int64, confirmFQDN string) error {
	c, err := s.GetCert(ctx, id)
	if err != nil {
		return err
	}
	if !strings.EqualFold(c.FQDN, confirmFQDN) {
		return ErrConfirmMismatch
	}
	return s.DeleteCert(ctx, id)
}

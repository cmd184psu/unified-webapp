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
	// rootCA.crt. Initializing a new CA now would occupy the current-CA role
	// with a new, unrelated root and leave the legacy one orphaned -- the
	// wizard's later ImportCA would hit a fingerprint mismatch with no clean
	// way back short of the CA-replacement plan's Replace route (a later
	// story), which is a bigger hammer than simply importing first. Import
	// must run first; never the reverse.
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

// IssueResult is what Generate, Renew and Edit return for the newly issued
// leaf. Cert.NotAfter already carries the actual (possibly clamped) expiry;
// Clamped and RequestedNotAfter surface GenerateLeaf's validity clamp
// (LeafResult, pki.go) so a caller can report FR-4's validityClamped fact
// rather than silently issuing a shorter certificate.
//
// PreviousDropped (the CA-replacement plan, P2) reports whether this call's
// transaction also dropped the previous CA -- Renew, Edit and Delete can all
// leave the previous CA signing nothing active, which triggers
// dropPreviousIfUnusedTx. It is always false for Generate, which never
// touches an existing row.
type IssueResult struct {
	Cert              Cert
	Clamped           bool
	RequestedNotAfter time.Time
	PreviousDropped   bool
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
	return s.InitNamedCA(ctx, legacyImportDir, DefaultCAName)
}

// InitNamedCA is InitCA with the root CA's name (its Common Name) chosen by
// the caller; name must already be normalized with NormalizeCAName.
func (s *Store) InitNamedCA(ctx context.Context, legacyImportDir, name string) error {
	switch _, err := s.GetCurrentCA(ctx); {
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

	certPEM, keyPEM, err := GenerateNamedCA(name)
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
// badges with. The message names the remedy: "Replace CA..." (the
// CA-replacement plan's rotation route, a later story), not a manual
// database edit.
func checkCAExpiry(caCert *x509.Certificate, expiryWarnDays int) error {
	warn := time.Duration(expiryWarnDays) * 24 * time.Hour
	if time.Until(caCert.NotAfter) < warn {
		return fmt.Errorf("%w: the certificate authority expires %s, within the %d-day warning threshold -- %s",
			ErrCAExpiringSoon, caCert.NotAfter.UTC().Format(time.RFC3339), expiryWarnDays, caReplacementRemedy)
	}
	return nil
}

// issueLeaf generates a fresh leaf certificate for fqdn/req under the current
// stored CA: GetCurrentCA followed by issueLeafWith. Fails with
// ErrCANotFound when no CA exists.
func (s *Store) issueLeaf(ctx context.Context, fqdn string, req CertRequest, defaultValidityDays, expiryWarnDays int) (Cert, LeafResult, error) {
	ca, err := s.GetCurrentCA(ctx)
	if err != nil {
		return Cert{}, LeafResult{}, err
	}
	return issueLeafWith(ca, fqdn, req, defaultValidityDays, expiryWarnDays)
}

// issueLeafWith generates a fresh leaf certificate for fqdn/req under ca. It
// performs no database access at all -- not even a read -- so every caller
// (Generate via issueLeaf, Renew, Edit) can call it, then open WithTx,
// satisfying the "crypto happens before the transaction opens" binding
// decision (R2) regardless of which CA row (current, in every case here) is
// doing the signing. Fails with ErrCAExpiringSoon when ca has fewer than
// expiryWarnDays left.
func issueLeafWith(ca *CA, fqdn string, req CertRequest, validityDays, expiryWarnDays int) (Cert, LeafResult, error) {
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

	leaf, err := GenerateLeaf(caCert, caKey, req, validityDays)
	if err != nil {
		return Cert{}, LeafResult{}, err
	}
	leafCert, err := ParseCert(leaf.CertPEM)
	if err != nil {
		return Cert{}, LeafResult{}, fmt.Errorf("certmachine: parse generated leaf cert: %w", err)
	}

	c := certFromLeaf(fqdn, leaf, leafCert)
	// A leaf issued here is, by construction, freshly signed by ca -- record
	// that signer now (P1/§3.4) rather than leaving ca_id NULL until a later
	// re-issue. certAndCA (handler.go, FR-R3) resolves a cert's downloads by
	// this field, and Generate's certAndCA callers must keep working
	// unchanged for a brand-new certificate.
	c.CAID = &ca.ID
	return c, leaf, nil
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
		if c.CAID != nil {
			if err := requireCurrentCATx(ctx, tx, *c.CAID); err != nil {
				return err
			}
		}
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
//
// Renew always uses defaultValidityDays (D8): it is FR-R5's per-certificate
// "re-issue under the current CA", not a validity override -- Edit is the
// one place an operator chooses validityDays. The new row's ca_id is set to
// the current CA by issueLeaf/issueLeafWith; dropPreviousIfUnusedTx (P2) runs
// in the same transaction, so a Renew that leaves the previous CA signing
// nothing active drops it (and its archived rows) immediately, which
// IssueResult.PreviousDropped reports.
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
	var dropped bool
	if err := s.WithTx(ctx, func(tx *sql.Tx) error {
		if err := s.ArchiveAllForFQDN(ctx, tx, source.FQDN); err != nil {
			return err
		}
		if c.CAID != nil {
			if err := requireCurrentCATx(ctx, tx, *c.CAID); err != nil {
				return err
			}
		}
		var err error
		newID, err = s.InsertCert(ctx, tx, c)
		if err != nil {
			return err
		}
		if err := s.runHook("write"); err != nil {
			return err
		}
		d, err := s.dropPreviousIfUnusedAndHookTx(ctx, tx)
		if err != nil {
			return err
		}
		dropped = d
		return nil
	}); err != nil {
		return IssueResult{}, err
	}
	c.ID = newID

	return IssueResult{Cert: c, Clamped: leaf.Clamped, RequestedNotAfter: leaf.RequestedNotAfter, PreviousDropped: dropped}, nil
}

// Delete permanently removes the cert row with id (FR-6: a hard delete,
// with no soft-delete or undo). confirmFQDN must equal the row's fqdn
// case-insensitively, or Delete returns ErrConfirmMismatch and removes
// nothing -- FR-6's "explicit confirmation naming the FQDN" is enforced
// here, server-side, because a browser-only confirm() leaves the
// destructive route one stray curl from firing. active, archived, and
// quarantined rows are all deletable this way.
//
// FR-6's "the CA is non-deletable" is narrowed by the CA-replacement plan
// (R4, Principle 2): the *current* CA is never deletable through any code
// path -- dropPreviousCATx is the only function in this package that
// deletes the ca row, and it refuses anything but role='previous'. A
// *previous* CA is dropped automatically, in the same transaction, once
// nothing active still uses it: the read, the confirmation check, the
// delete and dropPreviousIfUnusedTx (P2) all run inside one WithTx, so a
// concurrent change or a hook failure leaves the row (and the previous CA)
// untouched. The returned bool is IssueResult.PreviousDropped's twin for a
// call that issues nothing new.
func (s *Store) Delete(ctx context.Context, id int64, confirmFQDN string) (bool, error) {
	var dropped bool
	err := s.WithTx(ctx, func(tx *sql.Tx) error {
		c, err := getCertTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if !strings.EqualFold(c.FQDN, confirmFQDN) {
			return ErrConfirmMismatch
		}
		if err := deleteCertTx(ctx, tx, id); err != nil {
			return err
		}
		if err := s.runHook("write"); err != nil {
			return err
		}
		d, err := s.dropPreviousIfUnusedAndHookTx(ctx, tx)
		if err != nil {
			return err
		}
		dropped = d
		return nil
	})
	if err != nil {
		return false, err
	}
	return dropped, nil
}

// minEditValidityDays and maxEditValidityDays bound Edit's validityDays
// (FR-4's one narrowing, P5): GenerateLeaf still clamps to the CA's
// NotAfter regardless, but Edit is the one caller that accepts an
// operator-chosen validity at all, so it is the one place that must reject
// a nonsensical value up front rather than let a 0- or 4000-day request
// through to the clamp.
const (
	minEditValidityDays = 1
	maxEditValidityDays = 3650
)

// Edit re-issues the certificate identified by id under a new request and
// validity (FR-R6, P5) -- the one path through which an operator can change
// an existing certificate's fqdn, SANs, or validity without going through
// Renew's "copy the source row verbatim" contract. req must already be
// normalized and validated (ValidateRequest); Edit itself validates only
// validityDays, which ValidateRequest knows nothing about.
//
// Every branch below is pinned by P5:
//   - validityDays outside [1, 3650] -> ErrValidation, checked first (no
//     crypto, no reads) so a bad request never touches the store.
//   - the source row is quarantined -> ErrQuarantined (no parseable cert to
//     build on).
//   - the fqdn is unchanged and the source is archived while a newer active
//     row exists for that fqdn -> ErrNewerActiveExists, the same refusal
//     Renew raises for the same reason: editing it would resurrect a stale
//     SAN set and discard the newer cert's.
//   - the fqdn changes and another row is already active for the new one
//     -> ErrDuplicateActive, raised here as an early, friendly check; the
//     real guard is InsertCert's own in-transaction duplicate check, so a
//     concurrent collision still surfaces as ErrDuplicateActive (409).
//
// The new leaf is generated (issueLeafWith) before WithTx opens (R2). Inside
// the transaction: an unchanged fqdn is handled by ArchiveAllForFQDN alone
// (it already covers the source row when active); a changed fqdn archives
// only the source row, and only if it is active -- the old fqdn is then left
// with no active row, which is the point of a rename. InsertCert then
// inserts the new row with ca_id set to the current CA (issueLeafWith set
// it), and dropPreviousIfUnusedTx (P2) runs last, same as Renew and Delete.
func (s *Store) Edit(ctx context.Context, id int64, req CertRequest, validityDays, expiryWarnDays int) (IssueResult, error) {
	if validityDays < minEditValidityDays || validityDays > maxEditValidityDays {
		return IssueResult{}, fmt.Errorf("%w: validityDays must be between %d and %d, got %d",
			ErrValidation, minEditValidityDays, maxEditValidityDays, validityDays)
	}

	source, err := s.GetCert(ctx, id)
	if err != nil {
		return IssueResult{}, err
	}
	if source.Status == StatusQuarantined {
		return IssueResult{}, ErrQuarantined
	}

	fqdnUnchanged := strings.EqualFold(source.FQDN, req.FQDN)
	if fqdnUnchanged && source.Status == StatusArchived {
		active, err := s.activeCertForFQDN(ctx, source.FQDN)
		if err != nil {
			return IssueResult{}, err
		}
		if active != nil {
			return IssueResult{}, fmt.Errorf("%w: certificate %d is active for %s; edit that one instead", ErrNewerActiveExists, active.ID, source.FQDN)
		}
	}
	if !fqdnUnchanged {
		active, err := s.activeCertForFQDN(ctx, req.FQDN)
		if err != nil {
			return IssueResult{}, err
		}
		if active != nil && active.ID != source.ID {
			return IssueResult{}, fmt.Errorf("%w: %s", ErrDuplicateActive, req.FQDN)
		}
	}

	current, err := s.GetCurrentCA(ctx)
	if err != nil {
		return IssueResult{}, err
	}
	c, leaf, err := issueLeafWith(current, req.FQDN, req, validityDays, expiryWarnDays)
	if err != nil {
		return IssueResult{}, err
	}

	var newID int64
	var dropped bool
	if err := s.WithTx(ctx, func(tx *sql.Tx) error {
		switch {
		case fqdnUnchanged:
			if err := s.ArchiveAllForFQDN(ctx, tx, source.FQDN); err != nil {
				return err
			}
		case source.Status == StatusActive:
			if err := archiveCertTx(ctx, tx, source.ID); err != nil {
				return err
			}
		}

		if c.CAID != nil {
			if err := requireCurrentCATx(ctx, tx, *c.CAID); err != nil {
				return err
			}
		}

		var err error
		newID, err = s.InsertCert(ctx, tx, c)
		if err != nil {
			return err
		}
		if err := s.runHook("write"); err != nil {
			return err
		}
		d, err := s.dropPreviousIfUnusedAndHookTx(ctx, tx)
		if err != nil {
			return err
		}
		dropped = d
		return nil
	}); err != nil {
		return IssueResult{}, err
	}
	c.ID = newID

	return IssueResult{Cert: c, Clamped: leaf.Clamped, RequestedNotAfter: leaf.RequestedNotAfter, PreviousDropped: dropped}, nil
}

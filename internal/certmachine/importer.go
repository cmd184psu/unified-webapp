// importer.go reconciles the store's PKI state with a legacy certmachine
// installation. ImportCA (slice 3) handles the CA half; Preview/Execute
// (slice 7) handle every leaf under <legacyDir>/certs/ -- scan/classify,
// quarantine, and FR-8's idempotency guarantee. Neither this file nor any
// other in this package ever opens a meta.json (Principle 1): every
// metadata field below is derived by parsing cert.pem/key.pem directly, and
// the one declared carve-out (an unparseable cert's fqdn falling back to its
// legacy directory name) is exactly that -- a directory name, not anything
// read from meta.json.
package certmachine

import (
	"context"
	"crypto/x509"
	"database/sql"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// caReplacementRemedy is the manual procedure quoted by every message that
// needs an escape from "no CA rotation or deletion route exists" (FR-6):
// ImportCA's fingerprint-mismatch refusal here, and (in later slices)
// InitCA's ErrImportPending and the expiring-CA 409 from Generate/Renew.
// docs/certmachine.md (slice 11) documents the same procedure in full.
const caReplacementRemedy = `stop the binary, delete the CA row with sqlite3 <db> "DELETE FROM ca;", restart, re-initialize the CA, and re-issue every certificate -- existing leaves will no longer chain and must be regenerated`

// Sentinel errors for CA import/reconciliation (FR-3). Both are wrapped with
// fmt.Errorf so the message can name the dynamic detail (the offending path,
// or the two fingerprints) while callers still discriminate with errors.Is.
var (
	// ErrCAKeyMismatch means the legacy rootCA.key's public half does not
	// match rootCA.crt. ImportCA refuses the whole import and stores
	// neither: the root is the one input whose failure is unrecoverable
	// (every subsequent GenerateLeaf would produce a leaf no client can
	// verify), unlike a leaf-level mismatch, which slice 7 quarantines.
	ErrCAKeyMismatch = errors.New("certmachine: legacy root private key does not match its certificate")

	// ErrCAFingerprintMismatch means the store already holds a CA row whose
	// fingerprint differs from the legacy root's. ImportCA refuses rather
	// than silently continuing, which would leave every imported leaf
	// chained to a root the DB does not hold.
	ErrCAFingerprintMismatch = errors.New("certmachine: legacy root does not match the stored certificate authority")
)

// ImportCA reconciles the store's singleton CA row with the legacy root
// found under dir (rootCA.crt / rootCA.key -- see ReadLegacyRoot). It never
// re-encodes what it reads, preserving FR-3's byte-for-byte guarantee, and it
// verifies the key pairs with the certificate before storing either.
//
// Three outcomes (CA precedence, FR-3 + pre-mortem 3.1 ordering variant):
//
//   - no ca row -> the legacy root is inserted verbatim; imported is true.
//   - a ca row whose fingerprint equals the legacy root's -> skipped, this is
//     a re-run after a partial import; imported is false, err is nil.
//   - a ca row whose fingerprint differs -> the whole import is refused with
//     ErrCAFingerprintMismatch naming both fingerprints and the remedy;
//     nothing is changed.
func (s *Store) ImportCA(ctx context.Context, dir string) (imported bool, err error) {
	certPEM, keyPEM, err := ReadLegacyRoot(dir)
	if err != nil {
		return false, err
	}
	cert, err := ParseCert(certPEM)
	if err != nil {
		return false, fmt.Errorf("certmachine: parse legacy root cert in %s: %w", dir, err)
	}
	key, err := ParseKey(keyPEM)
	if err != nil {
		return false, fmt.Errorf("certmachine: parse legacy root key in %s: %w", dir, err)
	}
	if !keysMatch(cert, key) {
		return false, fmt.Errorf("%w: %s does not match %s", ErrCAKeyMismatch, legacyRootKeyFilename, legacyRootCertFilename)
	}

	legacyFP := Fingerprint(cert.Raw)

	existing, err := s.GetCA(ctx)
	switch {
	case err == nil:
		if existing.Fingerprint == legacyFP {
			return false, nil
		}
		return false, fmt.Errorf("%w: stored=%s legacy=%s; %s", ErrCAFingerprintMismatch, existing.Fingerprint, legacyFP, caReplacementRemedy)
	case !errors.Is(err, ErrCANotFound):
		return false, err
	}

	importedFrom := legacyRootCertFilename
	ca := CA{
		CertPEM:      string(certPEM),
		KeyPEM:       string(keyPEM),
		Subject:      cert.Subject.CommonName,
		Serial:       SerialString(cert.SerialNumber),
		NotBefore:    cert.NotBefore.UTC().Format(time.RFC3339),
		NotAfter:     cert.NotAfter.UTC().Format(time.RFC3339),
		Fingerprint:  legacyFP,
		ImportedFrom: &importedFrom,
		Created:      time.Now().UTC().Format(time.RFC3339),
	}
	if err := s.WithTx(ctx, func(tx *sql.Tx) error {
		return s.InsertCA(ctx, tx, ca)
	}); err != nil {
		return false, err
	}
	return true, nil
}

// ErrImportConfirmRequired means Execute was called against a database that
// already holds certs (CountCerts() > 0) without confirmNonEmpty. FR-8's
// idempotency guarantee (imported_from) makes a re-run safe, but a caller
// still has to say so explicitly -- the wizard "never sends a manifest ...
// only confirmNonEmpty" (plan §4 slice 10), and this is the sentinel that
// enforces it server-side.
var ErrImportConfirmRequired = errors.New("certmachine: the database already holds certificates; pass confirmNonEmpty to re-run the import")

// Legacy leaf filenames inside each <legacyDir>/certs/<name>/ directory
// (reference/certmachine/main.go:117-118,140).
const (
	legacyLeafCertFilename = "cert.pem"
	legacyLeafKeyFilename  = "key.pem"
)

// Import outcome values for ImportItem.Status. These are report-only
// vocabulary, distinct from the stored Cert.Status values: "expired" and
// "skipped" are never persisted (FR-2's "expired is never stored", FR-8's
// skip-is-not-a-status), they only describe what scan()/Preview/Execute
// found.
const (
	ImportOutcomeImportable  = "importable"
	ImportOutcomeExpired     = "expired"
	ImportOutcomeQuarantined = "quarantined"
	ImportOutcomeSkipped     = "skipped"
)

// ImportItem is one legacy source directory as classified by scan(),
// independent of whether Execute actually wrote a row for it.
type ImportItem struct {
	// Path is the source directory relative to legacyDir, e.g.
	// "certs/foo.local" -- the same value stored in certs.imported_from for
	// a row this item produces.
	Path string `json:"path"`
	// FQDN is the certificate's CN, or (Principle 1's declared carve-out)
	// the legacy directory name when the certificate did not parse at all.
	FQDN string `json:"fqdn"`
	// Status is one of the ImportOutcome* constants above.
	Status string `json:"status"`
	// Reason is populated for Quarantined (the quarantine reason) and
	// Skipped (why: already imported, or a duplicate serial) items; empty
	// for Importable/Expired.
	Reason string `json:"reason,omitempty"`
}

// ImportReport is what both Preview and Execute return: the classification
// scan() reached (identical for both, for the same tree and DB state --
// binding decision, slice 7 doc), plus (Execute only) whatever was actually
// written. StrayFiles names files found in the legacy root that are not
// certificates this tool manages (reference/certmachine/main.go's own
// server.crt/server.key listener cert) -- reported, never silently ignored
// (Principle 4).
type ImportReport struct {
	Importable int          `json:"importable"`
	Expired    int          `json:"expired"`
	Broken     int          `json:"broken"`
	Skipped    int          `json:"skipped"`
	Items      []ImportItem `json:"items"`
	StrayFiles []string     `json:"strayFiles"`
}

// legacyLeaf is scan()'s internal working record for one
// <legacyDir>/certs/<dirName> directory. It carries everything classifyLeaf
// discovers from disk, plus the skip/duplicate-CN decisions layered on top
// by determineSkips and resolveDuplicateCNs -- the two steps Preview and
// Execute both run, in that order (binding decision: skip lookups run
// first, duplicate-CN resolution only applies to survivors).
type legacyLeaf struct {
	dirName string
	relPath string // "certs/<dirName>", forward-slash always (matches the plan's literal example and stays deterministic across host OSes)

	quarantined bool
	reason      string // quarantine_reason text; empty when not quarantined

	fqdn          string // CN once parsed; falls back to dirName until/unless it is
	certPEM       []byte // raw bytes, present whenever cert.pem could be read at all
	keyPEM        []byte // raw bytes, present whenever key.pem could be read at all
	cert          *x509.Certificate
	serial        *string // nil exactly when the certificate did not parse (schema binding decision: NULL, never "")
	importWarning *string // set when the cert parses, keys pair, but it does not chain to the legacy root

	skipped    bool
	skipReason string

	// finalStatus is StatusActive or StatusArchived, decided by
	// resolveDuplicateCNs for every surviving (non-quarantined,
	// non-skipped) leaf. Zero value until that step runs.
	finalStatus string
}

// scanStrayFiles lists the files directly under dir (the legacy root) that
// are neither the legacy root cert/key nor the "certs" directory itself --
// reference/certmachine/main.go's own server.crt/server.key listener cert is
// the motivating case. Reported by the caller, never treated as an error.
func scanStrayFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("certmachine: read legacy root %s: %w", dir, err)
	}
	var stray []string
	for _, e := range entries {
		if e.IsDir() {
			continue // "certs" is the only directory the legacy app ever creates here
		}
		switch e.Name() {
		case legacyRootCertFilename, legacyRootKeyFilename:
			continue
		}
		stray = append(stray, e.Name())
	}
	return stray, nil
}

// classifyLeaf implements the scan procedure's classification table
// (slice 7 doc) for one <legacyDir>/certs/<dirName> directory. rootCert (may
// be nil, though scanLegacyTree never passes nil) is used only for a
// CheckSignatureFrom chain check, never Verify -- an already-expired leaf
// must never be conflated with a chain problem, and CheckSignatureFrom
// ignores validity periods entirely.
func classifyLeaf(certsDir, dirName string, rootCert *x509.Certificate) *legacyLeaf {
	leaf := &legacyLeaf{
		dirName: dirName,
		relPath: path.Join("certs", dirName),
		fqdn:    dirName, // carve-out default; overwritten the instant a CN parses
	}
	leafDir := filepath.Join(certsDir, dirName)

	certPEM, err := os.ReadFile(filepath.Join(leafDir, legacyLeafCertFilename))
	if err != nil {
		leaf.quarantined = true
		leaf.reason = "cert.pem missing or not valid PEM"
		return leaf
	}
	block, _ := pem.Decode(certPEM)
	if block == nil {
		leaf.quarantined = true
		leaf.reason = "cert.pem missing or not valid PEM"
		return leaf
	}
	if block.Type != pemTypeCertificate {
		// A file named cert.pem holding something else -- classically a key,
		// from a legacy tree someone repaired by hand. Its bytes are NOT
		// retained: cert_pem is projected into every /api/certs/{id} response
		// and served verbatim from the public files/cert.pem route, so
		// retaining a mislabelled key here would publish it. The reason text
		// deliberately does not echo block.Type, for the same reason.
		leaf.quarantined = true
		leaf.reason = "cert.pem does not contain a CERTIFICATE block"
		return leaf
	}
	// Re-encode only the decoded CERTIFICATE block, discarding whatever else
	// certPEM's raw bytes contained after it. A cert.pem holding a
	// CERTIFICATE block followed by a trailing PRIVATE KEY block (a
	// plausible operator mis-copy of this module's own haproxy.pem format --
	// bundle.go's HAProxyPEM) would otherwise pass the type check above and
	// publish that trailing key verbatim via cert_pem's projections (GET
	// /api/certs/{id} and the public files/cert.pem route). pem.EncodeToMemory
	// of a block produced by pem.Decode is byte-identical to the legacy
	// pem.Encode output for a well-formed single-block file (FR-3
	// byte-for-byte preservation; TestByteForByteLeafImport /
	// TestAssembledHAProxyPEMMatchesLegacyFile pin this), so this is safe
	// even though the DER below turns out to be unparseable.
	leaf.certPEM = pem.EncodeToMemory(block)
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		leaf.quarantined = true
		leaf.reason = fmt.Sprintf("certificate could not be parsed: %v", err)
		return leaf
	}
	leaf.cert = cert
	leaf.fqdn = cert.Subject.CommonName
	serial := SerialString(cert.SerialNumber)
	leaf.serial = &serial

	keyPEM, err := os.ReadFile(filepath.Join(leafDir, legacyLeafKeyFilename))
	if err != nil {
		leaf.quarantined = true
		leaf.reason = "private key file missing"
		return leaf
	}
	leaf.keyPEM = keyPEM
	key, err := ParseKey(keyPEM)
	if err != nil {
		leaf.quarantined = true
		leaf.reason = fmt.Sprintf("private key could not be parsed: %v", err)
		return leaf
	}
	if !keysMatch(cert, key) {
		leaf.quarantined = true
		leaf.reason = "private key does not match certificate"
		return leaf
	}

	if rootCert != nil {
		if err := cert.CheckSignatureFrom(rootCert); err != nil {
			warning := fmt.Sprintf("does not chain to the imported root: %v", err)
			leaf.importWarning = &warning
		}
	}

	return leaf
}

// scanLeaves classifies every directory under <dir>/certs/. A missing certs
// directory is not an error -- a legacy install with a root but no leaves
// yet -- it simply yields zero leaves.
func scanLeaves(dir string, rootCert *x509.Certificate) ([]*legacyLeaf, error) {
	certsDir := filepath.Join(dir, "certs")
	entries, err := os.ReadDir(certsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("certmachine: read legacy certs dir %s: %w", certsDir, err)
	}

	leaves := make([]*legacyLeaf, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		leaves = append(leaves, classifyLeaf(certsDir, e.Name(), rootCert))
	}
	return leaves, nil
}

// scanLegacyTree is scan() (slice 7 doc): the disk-only classification
// shared verbatim by Preview and Execute. It never touches the database and
// never writes anything under dir -- legacy_import_dir is read-only, and
// every file access here is os.ReadFile/os.ReadDir. rootCert is read fresh
// from dir's own rootCA.crt (not from the store's ca row, which Preview must
// never assume exists) purely so classifyLeaf can run its chain check; a
// root that cannot be read or parsed fails the whole scan, matching "a
// missing root fails the whole import with a clear error" for Preview as
// much as Execute.
func scanLegacyTree(dir string) (rootCert *x509.Certificate, leaves []*legacyLeaf, strayFiles []string, err error) {
	// Defensive: an empty dir makes every filepath.Join below relative, so
	// the scan would quietly read rootCA.crt out of the process working
	// directory. The handlers refuse an unconfigured legacy_import_dir with a
	// 409 before getting here (handler.go), but nothing about this function's
	// signature says so.
	if strings.TrimSpace(dir) == "" {
		return nil, nil, nil, errors.New("certmachine: legacy_import_dir is not configured")
	}
	rootPEM, err := os.ReadFile(filepath.Join(dir, legacyRootCertFilename))
	if err != nil {
		return nil, nil, nil, fmt.Errorf("certmachine: read legacy root cert: %w", err)
	}
	rootCert, err = ParseCert(rootPEM)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("certmachine: parse legacy root cert in %s: %w", dir, err)
	}

	strayFiles, err = scanStrayFiles(dir)
	if err != nil {
		return rootCert, nil, nil, err
	}

	leaves, err = scanLeaves(dir, rootCert)
	if err != nil {
		return rootCert, nil, strayFiles, err
	}
	return rootCert, leaves, strayFiles, nil
}

// determineSkips is the skip-determination step the slice 7 doc requires
// "for both callers": Preview and Execute must agree on which leaves are
// skipped, for the same tree and the same DB state. A leaf is skipped when
// its relPath (imported_from) or its parsed serial already exists -- either
// already committed in the store, or already seen earlier in this same scan
// pass (the copied-directory case: two legacy dirs sharing one UnixNano
// serial import as one row plus one skipped, never an aborted transaction).
// Leaves are processed in scan order (os.ReadDir's sorted order), so the
// alphabetically-first directory of a duplicate pair always wins.
func (s *Store) determineSkips(ctx context.Context, leaves []*legacyLeaf) error {
	existing, err := s.ListCerts(ctx)
	if err != nil {
		return err
	}
	seenPaths := make(map[string]bool, len(existing))
	seenSerials := make(map[string]bool, len(existing))
	for _, c := range existing {
		if c.ImportedFrom != nil {
			seenPaths[*c.ImportedFrom] = true
		}
		if c.Serial != nil {
			seenSerials[*c.Serial] = true
		}
	}

	for _, l := range leaves {
		switch {
		case seenPaths[l.relPath]:
			l.skipped = true
			l.skipReason = fmt.Sprintf("already imported from %s", l.relPath)
		case l.serial != nil && seenSerials[*l.serial]:
			l.skipped = true
			l.skipReason = fmt.Sprintf("duplicate certificate serial %s", *l.serial)
		default:
			seenPaths[l.relPath] = true
			if l.serial != nil {
				seenSerials[*l.serial] = true
			}
		}
	}
	return nil
}

// resolveDuplicateCNs applies the scan procedure's step 4: among leaves that
// survived determineSkips (skipped and quarantined leaves are left alone --
// quarantine rows never compete for certs_one_active_per_fqdn, and a skipped
// leaf produces no row at all), leaves sharing a case-insensitively equal CN
// have the newest not_after win StatusActive; the rest become
// StatusArchived. Running this only on survivors -- never before
// determineSkips -- is load-bearing: doing it first would let a fresh
// directory "resolve" against an already-imported, already-skipped one and
// archive the row that is actually active in the database.
func resolveDuplicateCNs(leaves []*legacyLeaf) {
	groups := make(map[string][]*legacyLeaf)
	for _, l := range leaves {
		if l.quarantined || l.skipped {
			continue
		}
		l.finalStatus = StatusActive
		key := strings.ToLower(l.fqdn)
		groups[key] = append(groups[key], l)
	}
	for _, group := range groups {
		if len(group) < 2 {
			continue
		}
		sort.SliceStable(group, func(i, j int) bool {
			return group[i].cert.NotAfter.After(group[j].cert.NotAfter)
		})
		for i := 1; i < len(group); i++ {
			group[i].finalStatus = StatusArchived
		}
	}
}

// buildReport turns a classified/skip-resolved/duplicate-resolved leaf list
// into the ImportReport both Preview and Execute return. "Expired" is
// computed here for the report only -- it is never what gets stored
// (FR-2/FR-8: an expired legacy cert still lands as a normal, StatusActive
// or StatusArchived row; expiry is derived by callers from not_after, same
// as everywhere else in this package).
func buildReport(leaves []*legacyLeaf, strayFiles []string) *ImportReport {
	if strayFiles == nil {
		strayFiles = []string{}
	}
	report := &ImportReport{Items: []ImportItem{}, StrayFiles: strayFiles}
	for _, l := range leaves {
		item := ImportItem{Path: l.relPath, FQDN: l.fqdn}
		switch {
		case l.skipped:
			item.Status = ImportOutcomeSkipped
			item.Reason = l.skipReason
			report.Skipped++
		case l.quarantined:
			item.Status = ImportOutcomeQuarantined
			item.Reason = l.reason
			report.Broken++
		case l.cert.NotAfter.Before(time.Now()):
			item.Status = ImportOutcomeExpired
			report.Expired++
		default:
			item.Status = ImportOutcomeImportable
			report.Importable++
		}
		report.Items = append(report.Items, item)
	}
	return report
}

// certFromLeafScan builds the Cert row a surviving (non-skipped) leaf
// produces. Quarantined leaves keep whatever was readable (Principle 1's
// carve-out and the scan procedure's binding decisions): cert_pem holds the
// canonical re-encoding of the single decoded CERTIFICATE block, retained
// only once cert.pem has been read, pem.Decode succeeded, and the block's
// type checked out as a certificate (even if the DER itself did not go on to
// parse) -- never the raw file bytes, and never anything past that first
// block. key_pem is NULL when key.pem could not be read, and every
// field that a successfully parsed certificate makes available (fingerprint,
// not_before/after, sans) is still populated even on a quarantined row --
// the parsed certificate remains the source of truth (Principle 1) for
// whatever quarantine cause left it readable.
func certFromLeafScan(l *legacyLeaf) Cert {
	relPath := l.relPath
	c := Cert{
		FQDN:         l.fqdn,
		ImportedFrom: &relPath,
		SANs:         SANs{DNS: []string{}, IP: []string{}},
		Created:      time.Now().UTC().Format(time.RFC3339),
	}
	if len(l.certPEM) > 0 {
		certStr := string(l.certPEM)
		c.CertPEM = &certStr
	}
	if len(l.keyPEM) > 0 {
		keyStr := string(l.keyPEM)
		c.KeyPEM = &keyStr
	}
	if l.cert != nil {
		c.Serial = l.serial
		notBefore := l.cert.NotBefore.UTC().Format(time.RFC3339)
		notAfter := l.cert.NotAfter.UTC().Format(time.RFC3339)
		c.NotBefore = &notBefore
		c.NotAfter = &notAfter
		fingerprint := Fingerprint(l.cert.Raw)
		c.Fingerprint = &fingerprint
		c.SANs = sansFromCert(l.cert)
	}

	if l.quarantined {
		c.Status = StatusQuarantined
		reason := l.reason
		c.QuarantineReason = &reason
		return c
	}

	c.Status = l.finalStatus
	c.ImportWarning = l.importWarning
	return c
}

// Preview performs a read-only scan and classification of legacyDir: it
// writes nothing to the database (no ImportCA, no cert inserts) and nothing
// to legacyDir. It runs the identical scan() and skip-determination steps
// Execute runs, so its counts (Importable/Expired/Broken/Skipped) and Items
// are exactly what Execute would produce against the same tree and the same
// current database state (binding decision, slice 7 doc) -- including a
// non-zero Skipped count on a database that already holds part of the tree,
// which is what makes a re-run's step-1 preview trustworthy.
func (s *Store) Preview(ctx context.Context, legacyDir string) (*ImportReport, error) {
	_, leaves, strayFiles, err := scanLegacyTree(legacyDir)
	if err != nil {
		return nil, err
	}
	if err := s.determineSkips(ctx, leaves); err != nil {
		return nil, err
	}
	resolveDuplicateCNs(leaves)
	return buildReport(leaves, strayFiles), nil
}

// Execute imports legacyDir for real: the CA first (ImportCA, including its
// fingerprint reconciliation -- a CA row whose fingerprint matches the
// legacy root is skipped, not re-inserted, so a partial re-run's second call
// costs nothing there), then every surviving leaf in exactly one transaction
// (WithTx). That single transaction is what makes the leaf half of the
// import genuinely all-or-nothing: any insert failing rolls back every leaf
// insert attempted in this call, leaving the leaf set exactly as it was
// before Execute was called (fix the named problem and re-run -- a CA
// already imported by an earlier, successful call is deliberately left in
// place, since ImportCA's own fingerprint check makes that state safe and
// idempotent to re-encounter). Execute refuses outright, before touching
// anything, when the database already holds certs and confirmNonEmpty is
// false (ErrImportConfirmRequired) -- with it set, every already-present
// leaf is simply Skipped, never re-inserted or duplicated.
func (s *Store) Execute(ctx context.Context, legacyDir string, confirmNonEmpty bool) (*ImportReport, error) {
	n, err := s.CountCerts(ctx)
	if err != nil {
		return nil, err
	}
	if n > 0 && !confirmNonEmpty {
		return nil, ErrImportConfirmRequired
	}

	if _, err := s.ImportCA(ctx, legacyDir); err != nil {
		return nil, err
	}

	_, leaves, strayFiles, err := scanLegacyTree(legacyDir)
	if err != nil {
		return nil, err
	}
	if err := s.determineSkips(ctx, leaves); err != nil {
		return nil, err
	}
	resolveDuplicateCNs(leaves)
	report := buildReport(leaves, strayFiles)

	toInsert := make([]Cert, 0, len(leaves))
	for _, l := range leaves {
		if l.skipped {
			continue
		}
		toInsert = append(toInsert, certFromLeafScan(l))
	}

	if len(toInsert) > 0 {
		if err := s.WithTx(ctx, func(tx *sql.Tx) error {
			for _, c := range toInsert {
				if _, err := s.InsertCert(ctx, tx, c); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			return report, err
		}
	}

	return report, nil
}

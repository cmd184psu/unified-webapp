package certmachine

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"database/sql"
	"errors"
	"fmt"
	"math/big"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

// newV1FixtureDB opens a fresh SQLite file at dbPath, applies ddlV1 and sets
// user_version = 1, and returns the open *sql.DB for the caller to insert
// fixture rows into. The caller closes it (or hands it to migrate/Open).
func newV1FixtureDB(t *testing.T, dbPath string) *sql.DB {
	t.Helper()
	db, err := sql.Open(sqlDriver, dsn(dbPath))
	if err != nil {
		t.Fatalf("open v1 fixture db: %v", err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(ddlV1); err != nil {
		t.Fatalf("apply ddlV1: %v", err)
	}
	if _, err := db.Exec("PRAGMA user_version = 1"); err != nil {
		t.Fatalf("set user_version = 1: %v", err)
	}
	return db
}

// insertV1CA inserts the singleton v1 ca row (no role column yet) and
// returns the parsed cert plus key for signing fixture leaves.
func insertV1CA(t *testing.T, db *sql.DB, notAfter time.Time) (*x509.Certificate, *rsa.PrivateKey) {
	t.Helper()
	caCert, caKey, caCertPEM := newThrowawayCA(t, notAfter)
	_, err := db.Exec(`INSERT INTO ca (id, cert_pem, key_pem, subject, serial, not_before, not_after, fingerprint, created)
		VALUES (1, ?, ?, ?, ?, ?, ?, ?, ?)`,
		string(caCertPEM), string(encodeKeyPEM(caKey)), caCert.Subject.CommonName, SerialString(caCert.SerialNumber),
		caCert.NotBefore.UTC().Format(time.RFC3339), caCert.NotAfter.UTC().Format(time.RFC3339),
		Fingerprint(caCert.Raw), time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		t.Fatalf("insert v1 ca: %v", err)
	}
	return caCert, caKey
}

// v1CertFixture describes one row to insert into a v1 certs table (no ca_id
// column yet -- that is added by the migration under test).
type v1CertFixture struct {
	fqdn             string
	status           string
	certPEM          []byte
	importWarning    *string
	quarantineReason *string
}

func insertV1Cert(t *testing.T, db *sql.DB, f v1CertFixture) int64 {
	t.Helper()
	var certPEM any
	if f.certPEM != nil {
		certPEM = string(f.certPEM)
	}
	res, err := db.Exec(`INSERT INTO certs (fqdn, sans, status, cert_pem, quarantine_reason, import_warning, created)
		VALUES (?, '{"dns":[],"ip":[]}', ?, ?, ?, ?, ?)`,
		f.fqdn, f.status, certPEM, f.quarantineReason, f.importWarning, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		t.Fatalf("insert v1 cert %s: %v", f.fqdn, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("insert v1 cert %s: last insert id: %v", f.fqdn, err)
	}
	return id
}

func TestMigrateV1toV2_Populated(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	dbPath := filepath.Join(t.TempDir(), "certmachine.db")
	db := newV1FixtureDB(t, dbPath)

	farFuture := time.Now().AddDate(10, 0, 0)
	caCert, caKey := insertV1CA(t, db, farFuture)
	otherCert, otherKey, _ := newThrowawayCA(t, farFuture)

	activeLeaf, err := GenerateLeaf(caCert, caKey, CertRequest{FQDN: "active.example.local"}, 365)
	if err != nil {
		t.Fatalf("generate active leaf: %v", err)
	}
	archivedLeaf, err := GenerateLeaf(caCert, caKey, CertRequest{FQDN: "archived.example.local"}, 365)
	if err != nil {
		t.Fatalf("generate archived leaf: %v", err)
	}
	// Signed by a different CA -- this is the "does not chain" import_warning row.
	orphanLeaf, err := GenerateLeaf(otherCert, otherKey, CertRequest{FQDN: "orphan.example.local"}, 365)
	if err != nil {
		t.Fatalf("generate orphan leaf: %v", err)
	}
	// A mixed-case legacy FQDN, chaining fine to the real CA.
	mixedCaseLeaf, err := GenerateLeaf(caCert, caKey, CertRequest{FQDN: "MixedCase.Example.Local"}, 365)
	if err != nil {
		t.Fatalf("generate mixed-case leaf: %v", err)
	}

	doesNotChain := "does not chain to the stored CA"
	quarantineReason := "key mismatch"

	activeID := insertV1Cert(t, db, v1CertFixture{fqdn: "active.example.local", status: StatusActive, certPEM: activeLeaf.CertPEM})
	archivedID := insertV1Cert(t, db, v1CertFixture{fqdn: "archived.example.local", status: StatusArchived, certPEM: archivedLeaf.CertPEM})
	quarantinedID := insertV1Cert(t, db, v1CertFixture{fqdn: "quarantined.example.local", status: StatusQuarantined, quarantineReason: &quarantineReason})
	orphanID := insertV1Cert(t, db, v1CertFixture{fqdn: "orphan.example.local", status: StatusActive, certPEM: orphanLeaf.CertPEM, importWarning: &doesNotChain})
	mixedCaseID := insertV1Cert(t, db, v1CertFixture{fqdn: "MixedCase.Example.Local", status: StatusActive, certPEM: mixedCaseLeaf.CertPEM})

	if err := migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	defer db.Close()

	var version int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatalf("read user_version: %v", err)
	}
	if version != schemaVersion {
		t.Errorf("user_version = %d, want %d", version, schemaVersion)
	}

	var caID int64
	var role string
	if err := db.QueryRow("SELECT id, role FROM ca").Scan(&caID, &role); err != nil {
		t.Fatalf("read migrated ca: %v", err)
	}
	if caID != 1 || role != "current" {
		t.Errorf("migrated ca = (id=%d, role=%q), want (1, current)", caID, role)
	}

	// P1: chaining rows get ca_id = 1; quarantined and non-chaining rows
	// stay NULL.
	wantCAID := map[int64]any{
		activeID:      int64(1),
		archivedID:    int64(1),
		quarantinedID: nil,
		orphanID:      nil,
		mixedCaseID:   int64(1),
	}
	for id, want := range wantCAID {
		var got sql.NullInt64
		if err := db.QueryRow("SELECT ca_id FROM certs WHERE id = ?", id).Scan(&got); err != nil {
			t.Fatalf("read ca_id for cert %d: %v", id, err)
		}
		if want == nil {
			if got.Valid {
				t.Errorf("cert %d ca_id = %d, want NULL", id, got.Int64)
			}
			continue
		}
		if !got.Valid || got.Int64 != want.(int64) {
			t.Errorf("cert %d ca_id = %v (valid=%v), want %v", id, got.Int64, got.Valid, want)
		}
	}

	// All 5 old indexes plus the 2 new ones.
	wantIndexes := []string{
		"certs_one_active_per_fqdn", "certs_imported_from", "certs_serial", "certs_not_after", "certs_fqdn",
		"ca_one_per_role", "certs_ca_id",
	}
	gotIndexes := indexNames(t, db)
	for _, name := range wantIndexes {
		if !gotIndexes[name] {
			t.Errorf("index %q missing after migration", name)
		}
	}

	s := &Store{db: db}
	if err := s.checkStructuralInvariants(ctx); err != nil {
		t.Errorf("checkStructuralInvariants after migration: %v", err)
	}

	// A second ca row with role='previous' inserts OK...
	prevCert, prevKey, prevCertPEM := newThrowawayCA(t, farFuture)
	if _, err := db.Exec(`INSERT INTO ca (role, cert_pem, key_pem, subject, serial, not_before, not_after, fingerprint, created)
		VALUES ('previous', ?, ?, ?, ?, ?, ?, ?, ?)`,
		string(prevCertPEM), string(encodeKeyPEM(prevKey)), prevCert.Subject.CommonName, SerialString(prevCert.SerialNumber),
		prevCert.NotBefore.UTC().Format(time.RFC3339), prevCert.NotAfter.UTC().Format(time.RFC3339),
		Fingerprint(prevCert.Raw), time.Now().UTC().Format(time.RFC3339)); err != nil {
		t.Errorf("insert second ca row with role=previous: %v", err)
	}
	// ...but a second 'current' fails the partial unique index.
	thirdCert, thirdKey, thirdCertPEM := newThrowawayCA(t, farFuture)
	if _, err := db.Exec(`INSERT INTO ca (role, cert_pem, key_pem, subject, serial, not_before, not_after, fingerprint, created)
		VALUES ('current', ?, ?, ?, ?, ?, ?, ?, ?)`,
		string(thirdCertPEM), string(encodeKeyPEM(thirdKey)), thirdCert.Subject.CommonName, SerialString(thirdCert.SerialNumber),
		thirdCert.NotBefore.UTC().Format(time.RFC3339), thirdCert.NotAfter.UTC().Format(time.RFC3339),
		Fingerprint(thirdCert.Raw), time.Now().UTC().Format(time.RFC3339)); err == nil {
		t.Error("insert second ca row with role=current: want error from ca_one_per_role, got nil")
	}
}

func indexNames(t *testing.T, db *sql.DB) map[string]bool {
	t.Helper()
	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type = 'index' AND name NOT LIKE 'sqlite_%'`)
	if err != nil {
		t.Fatalf("list indexes: %v", err)
	}
	defer rows.Close()
	names := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan index name: %v", err)
		}
		names[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("list indexes: %v", err)
	}
	return names
}

func TestMigrate_FreshIsV2(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "certmachine.db")
	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	var version int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatalf("read user_version: %v", err)
	}
	if version != 2 {
		t.Errorf("user_version = %d, want 2", version)
	}

	cols := tableCols(t, s.db, "ca")
	if _, ok := cols["role"]; !ok {
		t.Error("fresh v2 ca table has no role column")
	}
	certCols := tableCols(t, s.db, "certs")
	if _, ok := certCols["ca_id"]; !ok {
		t.Error("fresh v2 certs table has no ca_id column")
	}
}

func TestMigrate_Idempotent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	dbPath := filepath.Join(t.TempDir(), "certmachine.db")
	db := newV1FixtureDB(t, dbPath)
	insertV1CA(t, db, time.Now().AddDate(10, 0, 0))
	if err := migrate(db); err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Reopening must not re-run any migration step: no "table ca_new
	// already exists" or "duplicate column" error, and the data survives.
	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s.Close()

	var version int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatalf("read user_version: %v", err)
	}
	if version != schemaVersion {
		t.Errorf("user_version after reopen = %d, want %d", version, schemaVersion)
	}
	if err := s.checkStructuralInvariants(ctx); err != nil {
		t.Errorf("checkStructuralInvariants after reopen: %v", err)
	}
}

func TestMigrateV1toV2_FailureRollsBack(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	dbPath := filepath.Join(t.TempDir(), "certmachine.db")
	db := newV1FixtureDB(t, dbPath)
	defer db.Close()
	caCert, caKey := insertV1CA(t, db, time.Now().AddDate(10, 0, 0))
	leaf, err := GenerateLeaf(caCert, caKey, CertRequest{FQDN: "svc.example.local"}, 365)
	if err != nil {
		t.Fatalf("generate leaf: %v", err)
	}
	insertV1Cert(t, db, v1CertFixture{fqdn: "svc.example.local", status: StatusActive, certPEM: leaf.CertPEM})

	beforeCA := dumpTable(t, db, "ca")
	beforeCerts := dumpTable(t, db, "certs")

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	failAt3 := func(step int) error {
		if step == 3 {
			return errors.New("injected failure at step 3")
		}
		return nil
	}
	migrateErr := migrateV1toV2(tx, failAt3)
	if migrateErr == nil {
		t.Fatal("migrateV1toV2 with a step-3 hook error: want error, got nil")
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("rollback: %v", err)
	}

	var version int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatalf("read user_version: %v", err)
	}
	if version != 1 {
		t.Errorf("user_version after rollback = %d, want 1", version)
	}

	afterCA := dumpTable(t, db, "ca")
	afterCerts := dumpTable(t, db, "certs")
	if !reflect.DeepEqual(beforeCA, afterCA) {
		t.Errorf("ca dump changed after rollback:\nbefore=%v\nafter=%v", beforeCA, afterCA)
	}
	if !reflect.DeepEqual(beforeCerts, afterCerts) {
		t.Errorf("certs dump changed after rollback:\nbefore=%v\nafter=%v", beforeCerts, afterCerts)
	}
}

// dumpTable returns every row of table, every column, as a stable string
// representation ordered by id -- used to assert "nothing changed" without
// hardcoding the column list (R1/R7's atomicity test pattern).
func dumpTable(t *testing.T, db *sql.DB, table string) []string {
	t.Helper()
	rows, err := db.Query("SELECT * FROM " + table + " ORDER BY id")
	if err != nil {
		t.Fatalf("dump %s: %v", table, err)
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		t.Fatalf("dump %s: columns: %v", table, err)
	}
	var dump []string
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			t.Fatalf("dump %s: scan: %v", table, err)
		}
		dump = append(dump, fmt.Sprintf("%v", vals))
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("dump %s: %v", table, err)
	}
	return dump
}

// colInfo is one PRAGMA table_info row, minus cid: ADD COLUMN always
// appends, so cid differs between a migrated table and one built by ddlV2
// directly, even when the columns are otherwise identical.
type colInfo struct {
	typ     string
	notNull int
	dflt    sql.NullString
	pk      int
}

func tableCols(t *testing.T, db *sql.DB, table string) map[string]colInfo {
	t.Helper()
	rows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		t.Fatalf("table_info(%s): %v", table, err)
	}
	defer rows.Close()
	cols := map[string]colInfo{}
	for rows.Next() {
		var cid int
		var name, typ string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			t.Fatalf("table_info(%s): scan: %v", table, err)
		}
		cols[name] = colInfo{typ: typ, notNull: notnull, dflt: dflt, pk: pk}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("table_info(%s): %v", table, err)
	}
	return cols
}

// indexInfo captures everything TestMigrate_FreshEqualsMigrated compares for
// one index: whether it is unique/partial (index_list), its key columns and
// their collation (index_xinfo, key=1 rows only), and its own CREATE INDEX
// text with whitespace collapsed (raw sqlite_master.sql cannot be compared
// verbatim across a rebuilt vs. a freshly-created table, but each index's
// own SQL is stored per statement and is unaffected by that).
type indexInfo struct {
	unique  int
	partial int
	keyCols []string // "name/collation" pairs, in key order
	sql     string
}

func indexDetails(t *testing.T, db *sql.DB, table string) map[string]indexInfo {
	t.Helper()
	rows, err := db.Query(fmt.Sprintf("PRAGMA index_list(%s)", table))
	if err != nil {
		t.Fatalf("index_list(%s): %v", table, err)
	}
	type listRow struct {
		name    string
		unique  int
		partial int
	}
	var list []listRow
	for rows.Next() {
		var seq int
		var name, origin string
		var unique, partial int
		if err := rows.Scan(&seq, &name, &unique, &origin, &partial); err != nil {
			rows.Close()
			t.Fatalf("index_list(%s): scan: %v", table, err)
		}
		list = append(list, listRow{name: name, unique: unique, partial: partial})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		t.Fatalf("index_list(%s): %v", table, err)
	}
	rows.Close()

	details := map[string]indexInfo{}
	for _, l := range list {
		if strings.HasPrefix(l.name, "sqlite_autoindex_") {
			continue
		}
		xrows, err := db.Query(fmt.Sprintf("PRAGMA index_xinfo(%s)", l.name))
		if err != nil {
			t.Fatalf("index_xinfo(%s): %v", l.name, err)
		}
		var keyCols []string
		for xrows.Next() {
			var seqno, cid, key int
			var name, coll sql.NullString
			var desc int
			if err := xrows.Scan(&seqno, &cid, &name, &desc, &coll, &key); err != nil {
				xrows.Close()
				t.Fatalf("index_xinfo(%s): scan: %v", l.name, err)
			}
			if key == 1 {
				keyCols = append(keyCols, fmt.Sprintf("%s/%s", name.String, coll.String))
			}
		}
		if err := xrows.Err(); err != nil {
			xrows.Close()
			t.Fatalf("index_xinfo(%s): %v", l.name, err)
		}
		xrows.Close()

		var sqlText sql.NullString
		if err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type = 'index' AND name = ?`, l.name).Scan(&sqlText); err != nil {
			t.Fatalf("read index sql for %s: %v", l.name, err)
		}
		details[l.name] = indexInfo{
			unique:  l.unique,
			partial: l.partial,
			keyCols: keyCols,
			sql:     strings.Join(strings.Fields(sqlText.String), " "),
		}
	}
	return details
}

// insertGuardCA inserts a minimal, valid current CA row directly (bypassing
// crypto) so the AUTOINCREMENT id-reuse guard has a row 1 to start from on
// both a fresh and a migrated database.
func insertGuardCA(t *testing.T, db *sql.DB, role string) int64 {
	t.Helper()
	res, err := db.Exec(`INSERT INTO ca (role, cert_pem, key_pem, subject, serial, not_before, not_after, fingerprint, created)
		VALUES (?, 'CERT', 'KEY', 'CN=guard', '1', '2024-01-01T00:00:00Z', '2034-01-01T00:00:00Z', 'FP', '2024-01-01T00:00:00Z')`, role)
	if err != nil {
		t.Fatalf("insert guard ca (role=%s): %v", role, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("insert guard ca: last insert id: %v", err)
	}
	return id
}

// assertAutoincrementIDReuseGuard is the real AUTOINCREMENT check the plan
// calls for: table_info can't reveal AUTOINCREMENT, and sqlite_sequence
// already exists in v1 because of certs, so neither is meaningful on its
// own. Deleting row 2 and inserting again must yield id 3, never a reused 2.
// db must start with exactly one 'current' ca row at id 1.
func assertAutoincrementIDReuseGuard(t *testing.T, db *sql.DB) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	id2 := insertGuardCA(t, db, "previous")
	if id2 != 2 {
		t.Fatalf("first inserted previous ca id = %d, want 2", id2)
	}
	// dropPreviousCATx (the real drop path, R4) is now available; exercise
	// it here instead of a raw DELETE, since it is what production code
	// (and this AUTOINCREMENT guard) must both agree deletes row 2.
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	if err := dropPreviousCATx(ctx, tx, id2); err != nil {
		_ = tx.Rollback()
		t.Fatalf("dropPreviousCATx(%d): %v", id2, err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	id3 := insertGuardCA(t, db, "previous")
	if id3 != 3 {
		t.Errorf("second inserted previous ca id = %d, want 3 (not a reused 2)", id3)
	}

	tx, err = db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin cleanup tx: %v", err)
	}
	if err := dropPreviousCATx(ctx, tx, id3); err != nil {
		_ = tx.Rollback()
		t.Fatalf("cleanup dropPreviousCATx(%d): %v", id3, err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit cleanup: %v", err)
	}
}

func TestMigrate_FreshEqualsMigrated(t *testing.T) {
	freshPath := filepath.Join(t.TempDir(), "fresh.db")
	freshStore, err := Open(freshPath)
	if err != nil {
		t.Fatalf("Open(fresh): %v", err)
	}
	defer freshStore.Close()
	insertGuardCA(t, freshStore.db, "current") // id 1, matches the migrated fixture below

	migratedPath := filepath.Join(t.TempDir(), "migrated.db")
	migratedDB := newV1FixtureDB(t, migratedPath)
	insertV1CA(t, migratedDB, time.Now().AddDate(10, 0, 0))
	if err := migrate(migratedDB); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	defer migratedDB.Close()

	// Table-name set.
	freshTables := tableNames(t, freshStore.db)
	migratedTables := tableNames(t, migratedDB)
	if !reflect.DeepEqual(freshTables, migratedTables) {
		t.Errorf("table sets differ:\nfresh=%v\nmigrated=%v", freshTables, migratedTables)
	}

	for _, table := range []string{"ca", "certs"} {
		freshCols := tableCols(t, freshStore.db, table)
		migratedCols := tableCols(t, migratedDB, table)
		if !reflect.DeepEqual(freshCols, migratedCols) {
			t.Errorf("%s table_info differs:\nfresh=%+v\nmigrated=%+v", table, freshCols, migratedCols)
		}

		freshIdx := indexDetails(t, freshStore.db, table)
		migratedIdx := indexDetails(t, migratedDB, table)
		if !reflect.DeepEqual(freshIdx, migratedIdx) {
			t.Errorf("%s index details differ:\nfresh=%+v\nmigrated=%+v", table, freshIdx, migratedIdx)
		}
	}

	assertAutoincrementIDReuseGuard(t, freshStore.db)
	assertAutoincrementIDReuseGuard(t, migratedDB)
}

func tableNames(t *testing.T, db *sql.DB) []string {
	t.Helper()
	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'`)
	if err != nil {
		t.Fatalf("list tables: %v", err)
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan table name: %v", err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("list tables: %v", err)
	}
	sort.Strings(names)
	return names
}

// TestOpen_ExpiredRowsStillBoot is R6's boot-safety check: an expired leaf
// and a leaf with no ServerAuth EKU must never block migration or Open, and
// the signature-only backfill (CheckSignatureFrom, never Verify) must still
// attribute both to the migrated CA.
func TestOpen_ExpiredRowsStillBoot(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	dbPath := filepath.Join(t.TempDir(), "certmachine.db")
	db := newV1FixtureDB(t, dbPath)
	caCert, caKey := insertV1CA(t, db, time.Now().AddDate(10, 0, 0))

	past := time.Now().Add(-30 * 24 * time.Hour)
	expiredLeaf, expiredPEM := newV1Leaf(t, caCert, caKey, "expired.example.local", past.Add(-24*time.Hour), past, []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth})
	_ = expiredLeaf
	noEKULeaf, noEKUPEM := newV1Leaf(t, caCert, caKey, "no-eku.example.local", time.Now().Add(-time.Hour), time.Now().AddDate(1, 0, 0), nil)
	_ = noEKULeaf

	expiredID := insertV1Cert(t, db, v1CertFixture{fqdn: "expired.example.local", status: StatusActive, certPEM: expiredPEM})
	noEKUID := insertV1Cert(t, db, v1CertFixture{fqdn: "no-eku.example.local", status: StatusActive, certPEM: noEKUPEM})
	if err := db.Close(); err != nil {
		t.Fatalf("close fixture db: %v", err)
	}

	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open (migrate + boot) with expired/no-EKU rows: %v", err)
	}
	defer s.Close()

	if err := s.checkStructuralInvariants(ctx); err != nil {
		t.Errorf("checkStructuralInvariants: %v", err)
	}

	for _, id := range []int64{expiredID, noEKUID} {
		var caID sql.NullInt64
		if err := s.db.QueryRowContext(ctx, `SELECT ca_id FROM certs WHERE id = ?`, id).Scan(&caID); err != nil {
			t.Fatalf("read ca_id for cert %d: %v", id, err)
		}
		if !caID.Valid || caID.Int64 != 1 {
			t.Errorf("cert %d ca_id = %v (valid=%v), want 1", id, caID.Int64, caID.Valid)
		}
	}
}

// newV1Leaf builds a leaf certificate signed by caCert/caKey without going
// through GenerateLeaf, so NotBefore/NotAfter and ExtKeyUsage can be set
// freely -- GenerateLeaf clamps to the CA's remaining lifetime and always
// adds ServerAuth, neither of which this test wants.
func newV1Leaf(t *testing.T, caCert *x509.Certificate, caKey *rsa.PrivateKey, cn string, notBefore, notAfter time.Time, ekus []x509.ExtKeyUsage) (*x509.Certificate, []byte) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate leaf key: %v", err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		t.Fatalf("generate leaf serial: %v", err)
	}
	tpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    notBefore,
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  ekus,
		DNSNames:     []string{cn},
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, caCert, &key.PublicKey, caKey)
	if err != nil {
		t.Fatalf("create leaf cert for %s: %v", cn, err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse leaf cert for %s: %v", cn, err)
	}
	return cert, encodeCertPEM(der)
}

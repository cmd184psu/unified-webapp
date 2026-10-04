package haproxy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func certTestStore(t *testing.T) (*CertStore, *fakeDriver, string) {
	t.Helper()
	dir := t.TempDir()
	drv := newFakeDriver()
	return CertNewStore(dir, drv), drv, dir
}

// A matching download is written byte for byte under the convention name with
// CertMode, and recorded in certs.json; nothing about the cert's details is
// stored.
func TestCertPullAndStageWritesVerifiedBytes(t *testing.T) {
	store, drv, dataDir := certTestStore(t)
	cm := newFakeCertMachine()
	entry := cm.addCert(12, "brandx.cmdhome.net", "fp-12", "active", []string{"brandx.cmdhome.net"})
	srv := cm.start(t)
	client := mustClient(t, CertMachineSettings{URL: srv.URL, APIKey: "topsecretkey"})

	res, err := store.PullAndStage(context.Background(), drv, client, entry.cert, "a note")
	if err != nil {
		t.Fatalf("PullAndStage: %v", err)
	}
	wantName := NamingFileName(entry.cert.FQDN, entry.body)
	if res.Name != wantName {
		t.Errorf("staged name = %q, want %q", res.Name, wantName)
	}
	wantPath := filepath.Join(drv.CertsDir(), wantName)
	if res.Path != wantPath {
		t.Errorf("staged path = %q, want %q", res.Path, wantPath)
	}
	if got := drv.Files[wantPath]; string(got) != string(entry.body) {
		t.Errorf("written bytes differ from the verified body")
	}
	if got := drv.Modes[wantPath]; got != drv.Ownership().CertMode {
		t.Errorf("written mode = %v, want CertMode %v", got, drv.Ownership().CertMode)
	}
	sum := sha256.Sum256(entry.body)
	if res.SHA256 != hex.EncodeToString(sum[:]) {
		t.Errorf("recorded sha256 = %q, want the full body hash", res.SHA256)
	}

	// The tracking row exists, enabled, with the source but no detail.
	listing, err := store.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(listing.Certs) != 1 || listing.Certs[0].Name != wantName || !listing.Certs[0].Enabled {
		t.Fatalf("listing = %+v, want one enabled row %q", listing, wantName)
	}
	if listing.Certs[0].CertMachine.ID != 12 || listing.Certs[0].CertMachine.FQDN != "brandx.cmdhome.net" {
		t.Errorf("source = %+v, want id 12 / brandx", listing.Certs[0].CertMachine)
	}

	// certs.json never carries a cert detail or any key material.
	raw, err := os.ReadFile(filepath.Join(dataDir, "certs.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), fakeCMPrivateKeyMarker) || strings.Contains(string(raw), "PRIVATE KEY") {
		t.Error("certs.json leaked key material")
	}
	for _, forbidden := range []string{"subject", "issuer", "notAfter", "notBefore", "sans", "serial", "fingerprint", "status"} {
		if strings.Contains(string(raw), forbidden) {
			t.Errorf("certs.json contains a cert detail field %q", forbidden)
		}
	}
}

// certs.json entries carry only name/enabled/note/certmachine{id,fqdn}/sha256.
func TestCertEntryHasNoDetailFields(t *testing.T) {
	e := CertEntry{
		Name: "brandx.cmdhome.net-3fa91c07b2de.pem", Enabled: true, Note: "x",
		CertMachine: CertSource{ID: 12, FQDN: "brandx.cmdhome.net"}, SHA256: "3fa91c07b2de",
	}
	b, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	wantKeys := map[string]bool{"name": true, "enabled": true, "note": true, "certmachine": true, "sha256": true}
	for k := range m {
		if !wantKeys[k] {
			t.Errorf("CertEntry has unexpected field %q (no cert detail may be stored)", k)
		}
	}
	for k := range wantKeys {
		if _, ok := m[k]; !ok {
			t.Errorf("CertEntry missing field %q", k)
		}
	}
	var sub map[string]json.RawMessage
	if err := json.Unmarshal(m["certmachine"], &sub); err != nil {
		t.Fatal(err)
	}
	if len(sub) != 2 || sub["id"] == nil || sub["fqdn"] == nil {
		t.Errorf("certmachine sub-object = %v, want exactly {id,fqdn}", sub)
	}
}

// Each failed verification writes nothing: no bytes land and no write is
// recorded against the driver.
func TestCertPullAndStageWritesNothingOnBadVerification(t *testing.T) {
	tamper := map[string]func(*fakeCertMachine){
		"tampered body":     func(f *fakeCertMachine) { f.tamperBody = true },
		"mismatched ETag":   func(f *fakeCertMachine) { f.corruptETag = true },
		"wrong X-Cert-Id":   func(f *fakeCertMachine) { f.wrongID = true },
		"wrong fingerprint": func(f *fakeCertMachine) { f.wrongFingerprint = true },
		"missing ETag":      func(f *fakeCertMachine) { f.missingETag = true },
	}
	for name, apply := range tamper {
		t.Run(name, func(t *testing.T) {
			store, drv, _ := certTestStore(t)
			cm := newFakeCertMachine()
			entry := cm.addCert(7, "svc.cmdhome.net", "fp-7", "active", nil)
			apply(cm)
			srv := cm.start(t)
			client := mustClient(t, CertMachineSettings{URL: srv.URL, APIKey: "k"})

			if _, err := store.PullAndStage(context.Background(), drv, client, entry.cert, ""); err == nil {
				t.Fatalf("%s: PullAndStage succeeded, want rejection", name)
			}
			if len(drv.Files) != 0 {
				t.Errorf("%s: driver has %d files, want none written", name, len(drv.Files))
			}
			for _, c := range drv.Calls {
				if strings.HasPrefix(c, "write ") {
					t.Errorf("%s: a write reached the driver: %q", name, c)
				}
			}
			listing, _ := store.List(context.Background())
			if len(listing.Certs) != 0 {
				t.Errorf("%s: a tracking row was recorded for a rejected pull", name)
			}
		})
	}
}

// Disable removes a cert from the crt-list but keeps its file and row; enable
// restores it. Unmanaged files are never touched or counted as managed.
func TestCertEnableDisableAndUnmanaged(t *testing.T) {
	store, drv, _ := certTestStore(t)
	cm := newFakeCertMachine()
	entry := cm.addCert(12, "brandx.cmdhome.net", "fp-12", "active", nil)
	srv := cm.start(t)
	client := mustClient(t, CertMachineSettings{URL: srv.URL, APIKey: "k"})
	res, err := store.PullAndStage(context.Background(), drv, client, entry.cert, "")
	if err != nil {
		t.Fatal(err)
	}

	// A hand-placed, non-convention file in the certs dir.
	unmanagedPath := filepath.Join(drv.CertsDir(), "hand-placed.crt")
	_ = drv.PrivilegedWrite(context.Background(), unmanagedPath, []byte("not ours"), 0o644)

	listing, err := store.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if listing.Unmanaged != 1 {
		t.Errorf("Unmanaged = %d, want 1 (the hand-placed file)", listing.Unmanaged)
	}
	if len(listing.Certs) != 1 {
		t.Errorf("managed = %d, want only our convention file", len(listing.Certs))
	}

	// crt-list has the enabled cert's line.
	crt, err := store.CrtList()
	if err != nil {
		t.Fatal(err)
	}
	wantLine := filepath.Join(drv.CertsDir(), res.Name)
	if !strings.Contains(crt, wantLine) {
		t.Errorf("crt-list = %q, want it to include %q", crt, wantLine)
	}

	// Disable: line gone, file and row remain.
	if err := store.SetEnabled(res.Name, false); err != nil {
		t.Fatal(err)
	}
	crt, _ = store.CrtList()
	if strings.Contains(crt, wantLine) {
		t.Errorf("disabled cert still in crt-list: %q", crt)
	}
	if _, ok := drv.Files[wantLine]; !ok {
		t.Error("disabling removed the cert file; it must stay on disk")
	}
	listing, _ = store.List(context.Background())
	if len(listing.Certs) != 1 || listing.Certs[0].Enabled {
		t.Errorf("row gone or still enabled after disable: %+v", listing.Certs)
	}

	// Enable restores the line.
	if err := store.SetEnabled(res.Name, true); err != nil {
		t.Fatal(err)
	}
	crt, _ = store.CrtList()
	if !strings.Contains(crt, wantLine) {
		t.Errorf("enable did not restore the crt-list line: %q", crt)
	}

	// The unmanaged file was never written to or removed by us.
	if string(drv.Files[unmanagedPath]) != "not ours" {
		t.Error("unmanaged file was modified")
	}
	for _, c := range drv.Calls {
		if c == "remove "+unmanagedPath {
			t.Error("unmanaged file was removed")
		}
	}
}

// A missing file for a tracked row is marked missing (not removed from the row).
func TestCertMissingFileMarkedMissing(t *testing.T) {
	store, drv, _ := certTestStore(t)
	cm := newFakeCertMachine()
	entry := cm.addCert(12, "brandx.cmdhome.net", "fp-12", "active", nil)
	srv := cm.start(t)
	client := mustClient(t, CertMachineSettings{URL: srv.URL, APIKey: "k"})
	res, err := store.PullAndStage(context.Background(), drv, client, entry.cert, "")
	if err != nil {
		t.Fatal(err)
	}
	delete(drv.Files, filepath.Join(drv.CertsDir(), res.Name)) // file disappears under us

	listing, err := store.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(listing.Certs) != 1 || !listing.Certs[0].Missing {
		t.Errorf("listing = %+v, want the row kept and marked missing", listing.Certs)
	}
}

// Remove deletes the file and row, but is refused while a service uses it.
func TestCertRemoveRefusedWhileUsed(t *testing.T) {
	store, drv, _ := certTestStore(t)
	cm := newFakeCertMachine()
	entry := cm.addCert(12, "brandx.cmdhome.net", "fp-12", "active", nil)
	srv := cm.start(t)
	client := mustClient(t, CertMachineSettings{URL: srv.URL, APIKey: "k"})
	res, err := store.PullAndStage(context.Background(), drv, client, entry.cert, "")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(drv.CertsDir(), res.Name)

	// Used: refused, nothing deleted.
	used := func(name string) bool { return name == res.Name }
	if err := store.Remove(context.Background(), drv, res.Name, used); err == nil {
		t.Fatal("Remove succeeded while the cert was in use")
	}
	if _, ok := drv.Files[path]; !ok {
		t.Error("refused Remove still deleted the file")
	}
	if listing, _ := store.List(context.Background()); len(listing.Certs) != 1 {
		t.Error("refused Remove still dropped the row")
	}

	// Unused: file and row both go.
	if err := store.Remove(context.Background(), drv, res.Name, func(string) bool { return false }); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, ok := drv.Files[path]; ok {
		t.Error("Remove did not delete the file")
	}
	if listing, _ := store.List(context.Background()); len(listing.Certs) != 0 {
		t.Error("Remove did not drop the row")
	}
}

// New bytes => new file; the old file is kept until an explicit post-Apply
// cleanup of the superseded files.
func TestCertSupersededFilesListedNotRemovedUntilCleanup(t *testing.T) {
	store, drv, _ := certTestStore(t)
	cm := newFakeCertMachine()
	first := cm.addCert(12, "brandx.cmdhome.net", "fp-12", "active", nil)
	srv := cm.start(t)
	client := mustClient(t, CertMachineSettings{URL: srv.URL, APIKey: "k"})

	r1, err := store.PullAndStage(context.Background(), drv, client, first.cert, "")
	if err != nil {
		t.Fatal(err)
	}
	// A re-issue: new id, same FQDN, new bytes => new name.
	second := cm.addCert(13, "brandx.cmdhome.net", "fp-13", "active", nil)
	r2, err := store.PullAndStage(context.Background(), drv, client, second.cert, "")
	if err != nil {
		t.Fatal(err)
	}
	if r2.Name == r1.Name {
		t.Fatalf("re-issue reused the old name %q", r1.Name)
	}
	if len(r2.Superseded) != 1 || r2.Superseded[0] != r1.Name {
		t.Fatalf("superseded = %v, want [%q]", r2.Superseded, r1.Name)
	}
	oldPath := filepath.Join(drv.CertsDir(), r1.Name)
	if _, ok := drv.Files[oldPath]; !ok {
		t.Error("PullAndStage removed the superseded file; it must survive until Apply")
	}

	// The post-Apply cleanup removes the superseded file and row.
	if err := store.RemoveSuperseded(context.Background(), drv, r2.Superseded); err != nil {
		t.Fatal(err)
	}
	if _, ok := drv.Files[oldPath]; ok {
		t.Error("RemoveSuperseded left the old file")
	}
	listing, _ := store.List(context.Background())
	if len(listing.Certs) != 1 || listing.Certs[0].Name != r2.Name {
		t.Errorf("after cleanup listing = %+v, want only %q", listing.Certs, r2.Name)
	}
}

// Re-enabling a superseded row clears the marker, so cleanup leaves it alone.
func TestCertReEnableClearsSupersededMarker(t *testing.T) {
	store, drv, _ := certTestStore(t)
	cm := newFakeCertMachine()
	srv := cm.start(t)
	r1 := pullStaged(t, store, drv, cm, srv.URL, 12, "x.example")
	r2 := pullStaged(t, store, drv, cm, srv.URL, 13, "x.example")
	if len(r2.Superseded) != 1 {
		t.Fatalf("superseded = %v", r2.Superseded)
	}
	if err := store.SetEnabled(r1.Name, true); err != nil {
		t.Fatal(err)
	}
	if err := store.RemoveSuperseded(context.Background(), drv, r2.Superseded); err != nil {
		t.Fatal(err)
	}
	if _, ok := drv.Files[filepath.Join(drv.CertsDir(), r1.Name)]; !ok {
		t.Error("re-enabled row's file was removed")
	}
	listing, _ := store.List(context.Background())
	if len(listing.Certs) != 2 {
		t.Errorf("listing = %+v, want both rows", listing.Certs)
	}
}

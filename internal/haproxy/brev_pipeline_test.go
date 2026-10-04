package haproxy

// B-REV focused-review regression tests. Each test is written RED first against
// the four defects the Apply/rollback review returned:
//
//	Defect 1 (OUTAGE): cert Update must SWAP the new cert into the crt-list
//	  (superseded same-FQDN rows disabled immediately), never leave both the old
//	  and new rows enabled, which danglingly references a file RemoveSuperseded
//	  then deletes and leaves certs.json in perpetual "pending changes".
//	Defect 2: Reload/Restart/Start must serialize with Apply on the same lock.
//	Defect 3: the Go error return is reserved for indeterminate state; every
//	  outcome fully described by ApplyResult returns a nil error with an Outcome.
//	Defect 4: staging must not corrupt a crt-list path that appears inside an
//	  unrelated longer path (Apply renders the staged config directly; Restore
//	  replaces only the exact `crt-list <path>` token).

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// --- test doubles for these defects -----------------------------------------

// blockingWriteDriver wraps a fakeDriver and blocks the write to blockPath until
// release is closed, signalling reached once it is at the gate. It lets a test
// hold Apply mid-install (crt-list written, config not yet) and fire a
// concurrent Reload. It does NOT modify driver_fake.go.
type blockingWriteDriver struct {
	*fakeDriver
	blockPath string
	reached   chan struct{}
	release   chan struct{}
	once      sync.Once
}

func (d *blockingWriteDriver) PrivilegedWrite(ctx context.Context, path string, data []byte, mode os.FileMode) error {
	if path == d.blockPath {
		d.once.Do(func() { close(d.reached) })
		<-d.release
	}
	return d.fakeDriver.PrivilegedWrite(ctx, path, data, mode)
}

// captureValidate records the staged config bytes exactly as they are handed to
// the validator, so a test can prove what Apply/Restore actually stage.
type captureValidate struct {
	*fakeDriver
	stagedCfg string
}

func (d *captureValidate) Validate(ctx context.Context, configPath string) error {
	b, _ := os.ReadFile(configPath)
	d.stagedCfg = string(b)
	return d.fakeDriver.Validate(ctx, configPath)
}

// reloadFailNth fails exactly the failOn-th reload (1-based), so the install
// reload can succeed while the rollback reload fails (indeterminate state).
type reloadFailNth struct {
	*fakeDriver
	failOn int
	n      int
}

func (d *reloadFailNth) Reload(ctx context.Context) error {
	_ = d.fakeDriver.Reload(ctx) // record the reload
	d.n++
	if d.n == d.failOn {
		return errors.New("reload failed")
	}
	return nil
}

// pullStaged pulls and stages a cert for fqdn through a fake CertMachine into
// the given store/driver, returning the stage result.
func pullStaged(t *testing.T, store *CertStore, drv *fakeDriver, cm *fakeCertMachine, srvURL string, id int64, fqdn string) CertStageResult {
	t.Helper()
	entry := cm.addCert(id, fqdn, "fp-"+fqdn+"-"+strconv.FormatInt(id, 10), "active", []string{fqdn})
	client := mustClient(t, CertMachineSettings{URL: srvURL, APIKey: "k"})
	res, err := store.PullAndStage(context.Background(), drv, client, entry.cert, "")
	if err != nil {
		t.Fatalf("PullAndStage(%d): %v", id, err)
	}
	return res
}

// --- Defect 1: cert Update swaps the crt-list, never dangles ----------------

// After staging a re-issued bundle for the same FQDN, the rendered crt-list
// lists ONLY the new cert; the superseded row is disabled (not deleted) and its
// file survives until RemoveSuperseded.
func TestCertUpdateSwapsCrtListToNewOnly(t *testing.T) {
	store, drv, _ := certTestStore(t)
	cm := newFakeCertMachine()
	srv := cm.start(t)
	r1 := pullStaged(t, store, drv, cm, srv.URL, 12, "brandx.cmdhome.net")
	r2 := pullStaged(t, store, drv, cm, srv.URL, 13, "brandx.cmdhome.net")
	if r2.Name == r1.Name {
		t.Fatalf("re-issue reused the old name %q", r1.Name)
	}

	crt, err := store.CrtList()
	if err != nil {
		t.Fatal(err)
	}
	newLine := filepath.Join(drv.CertsDir(), r2.Name)
	oldLine := filepath.Join(drv.CertsDir(), r1.Name)
	if !strings.Contains(crt, newLine) {
		t.Errorf("crt-list missing the new cert %q:\n%s", newLine, crt)
	}
	if strings.Contains(crt, oldLine) {
		t.Errorf("crt-list still lists the superseded cert (dangling reference):\n%s", crt)
	}

	// Old file survives on disk until the post-Apply cleanup.
	if _, ok := drv.Files[oldLine]; !ok {
		t.Error("superseded cert file removed too early; it must survive until RemoveSuperseded")
	}
	// Superseded row kept but disabled; new row enabled.
	listing, err := store.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var oldRow, newRow *CertManaged
	for i := range listing.Certs {
		switch listing.Certs[i].Name {
		case r1.Name:
			oldRow = &listing.Certs[i]
		case r2.Name:
			newRow = &listing.Certs[i]
		}
	}
	if oldRow == nil || newRow == nil {
		t.Fatalf("expected both rows kept, got %+v", listing.Certs)
	}
	if oldRow.Enabled {
		t.Error("superseded row must be disabled immediately (swapped out of the crt-list)")
	}
	if !newRow.Enabled {
		t.Error("new row must be enabled")
	}
}

// A full Update: apply the first cert, stage a re-issue, apply again with the
// superseded cleanup wired. The installed crt-list then references only files
// that exist, and Pending reports NO changes (no perpetual pending).
func TestCertUpdateAppliedThenCleanupNoPerpetualPending(t *testing.T) {
	drv := newFakeDriver()
	ap, _, certs, _ := newTestApplier(t, drv, ApplyOptions{BackupKeep: 5, Verifier: &fakeVerifier{}})
	cm := newFakeCertMachine()
	srv := cm.start(t)

	var toClean []string
	ap.opts.AfterSuccess = func(ctx context.Context) error {
		if len(toClean) == 0 {
			return nil
		}
		err := certs.RemoveSuperseded(ctx, drv, toClean)
		toClean = nil
		return err
	}

	r1 := pullStaged(t, certs, drv, cm, srv.URL, 12, "x.example")
	res, err := ap.Apply(context.Background())
	if err != nil {
		t.Fatalf("first apply: %v (res=%+v)", err, res)
	}
	if !res.Applied {
		t.Fatalf("first apply did not apply: %+v", res)
	}

	r2 := pullStaged(t, certs, drv, cm, srv.URL, 13, "x.example")
	toClean = r2.Superseded
	res, err = ap.Apply(context.Background())
	if err != nil {
		t.Fatalf("update apply: %v (res=%+v)", err, res)
	}
	if !res.Applied {
		t.Fatalf("update apply did not apply: %+v", res)
	}

	// The installed (live) crt-list references only files that exist.
	liveCrt := string(drv.Files[drv.CrtListPath()])
	for _, line := range strings.Split(strings.TrimSpace(liveCrt), "\n") {
		if line == "" {
			continue
		}
		if _, ok := drv.Files[line]; !ok {
			t.Errorf("installed crt-list references a deleted file %q:\n%s", line, liveCrt)
		}
	}
	oldPath := filepath.Join(drv.CertsDir(), r1.Name)
	newPath := filepath.Join(drv.CertsDir(), r2.Name)
	if _, ok := drv.Files[oldPath]; ok {
		t.Error("superseded file should have been removed by the post-Apply cleanup")
	}
	if _, ok := drv.Files[newPath]; !ok {
		t.Error("new cert file must exist after the update")
	}

	// No perpetual pending: candidate == live.
	ch, err := ap.Pending(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if ch.HasChanges {
		t.Errorf("perpetual pending after Update+cleanup: %+v", ch)
	}
}

// A failed Update apply (rolled back) leaves the OLD crt-list live, both files
// present, and the superseded row disabled (not deleted) so a retry works.
func TestCertUpdateFailedApplyLeavesOldLiveAndRetryWorks(t *testing.T) {
	drv := newFakeDriver()
	ap, _, certs, _ := newTestApplier(t, drv, ApplyOptions{BackupKeep: 5, Verifier: &fakeVerifier{}})
	cm := newFakeCertMachine()
	srv := cm.start(t)

	r1 := pullStaged(t, certs, drv, cm, srv.URL, 12, "x.example")
	if res, err := ap.Apply(context.Background()); err != nil || !res.Applied {
		t.Fatalf("seed apply: res=%+v err=%v", res, err)
	}
	oldCrt := string(drv.Files[drv.CrtListPath()])
	oldPath := filepath.Join(drv.CertsDir(), r1.Name)

	r2 := pullStaged(t, certs, drv, cm, srv.URL, 13, "x.example")
	newPath := filepath.Join(drv.CertsDir(), r2.Name)

	// Make the update apply fail at verify so it rolls back.
	ap.verifier = &fakeVerifier{err: errors.New("new worker not serving")}
	res, _ := ap.Apply(context.Background())
	if !res.RolledBack {
		t.Fatalf("expected RolledBack=true on verify failure, got %+v", res)
	}
	if string(drv.Files[drv.CrtListPath()]) != oldCrt {
		t.Error("rolled-back update must leave the OLD crt-list live")
	}
	if _, ok := drv.Files[oldPath]; !ok {
		t.Error("old cert file must remain after a rolled-back update")
	}
	if _, ok := drv.Files[newPath]; !ok {
		t.Error("new cert file must remain after a rolled-back update (never deleted by rollback)")
	}
	// The superseded row is disabled, not deleted, so a retry still renders the
	// new cert only.
	listing, _ := certs.List(context.Background())
	var oldRow *CertManaged
	for i := range listing.Certs {
		if listing.Certs[i].Name == r1.Name {
			oldRow = &listing.Certs[i]
		}
	}
	if oldRow == nil {
		t.Fatal("superseded row must not be deleted on a failed apply")
	}
	if oldRow.Enabled {
		t.Error("superseded row must stay disabled after a failed apply")
	}

	// Retry succeeds and installs the new cert.
	ap.verifier = &fakeVerifier{}
	res, err := ap.Apply(context.Background())
	if err != nil || !res.Applied {
		t.Fatalf("retry apply: res=%+v err=%v", res, err)
	}
	liveCrt := string(drv.Files[drv.CrtListPath()])
	if !strings.Contains(liveCrt, newPath) || strings.Contains(liveCrt, oldPath) {
		t.Errorf("retry must install the new crt-list only:\n%s", liveCrt)
	}
}

// --- Defect 2: Reload serializes with Apply (run with -race) ----------------

func TestReloadSerializedWithApply(t *testing.T) {
	base := newFakeDriver()
	drv := &blockingWriteDriver{
		fakeDriver: base, blockPath: base.ConfigPath(),
		reached: make(chan struct{}), release: make(chan struct{}),
	}
	ap, models, _, _ := newTestApplier(t, drv, ApplyOptions{BackupKeep: 5, Verifier: &fakeVerifier{}})
	addEnabledService(t, models) // first run: there are changes to apply

	applyDone := make(chan ApplyResult, 1)
	go func() {
		r, _ := ap.Apply(context.Background())
		applyDone <- r
	}()
	<-drv.reached // crt-list installed; Apply is blocked before the config write

	reloadDone := make(chan error, 1)
	go func() { reloadDone <- ap.Reload(context.Background()) }()
	time.Sleep(100 * time.Millisecond) // give a non-serialized Reload time to (wrongly) run

	// With serialization, no standalone reload may have run while Apply holds the
	// lock: there must be no reload after the crt-list install yet.
	cs := calls(base)
	crt := firstIndex(cs, "write "+base.CrtListPath())
	if crt < 0 {
		t.Fatalf("crt-list was not installed before the gate: %v", cs)
	}
	for i := crt + 1; i < len(cs); i++ {
		if cs[i] == "reload" {
			t.Fatalf("a reload ran between the crt-list install and the config install (Reload not serialized with Apply): %v", cs)
		}
	}

	close(drv.release)
	<-applyDone
	if err := <-reloadDone; err != nil {
		t.Fatalf("reload: %v", err)
	}

	cs = calls(base)
	wc := firstIndex(cs, "write "+base.ConfigPath())
	fr := firstIndex(cs, "reload")
	if wc < 0 || fr < 0 || fr < wc {
		t.Errorf("Apply's reload must follow the config install: config=%d reload=%d\n%v", wc, fr, cs)
	}
	if n := countCall(cs, "reload"); n != 2 {
		t.Errorf("expected 2 reloads (Apply + the serialized standalone), got %d: %v", n, cs)
	}
}

// --- Defect 3: the Outcome contract -----------------------------------------

func TestApplyOutcomeNoChanges(t *testing.T) {
	drv := newFakeDriver()
	ap, _, _, _ := newTestApplier(t, drv, ApplyOptions{})
	syncLive(t, ap, drv)
	res, err := ap.Apply(context.Background())
	if err != nil {
		t.Fatalf("no-changes must not error: %v", err)
	}
	if res.Outcome != OutcomeNoChanges || res.Applied {
		t.Errorf("want OutcomeNoChanges/Applied=false, got %+v", res)
	}
}

func TestApplyOutcomeApplied(t *testing.T) {
	drv := newFakeDriver()
	ap, models, _, _ := newTestApplier(t, drv, ApplyOptions{BackupKeep: 5, Verifier: &fakeVerifier{}})
	syncLive(t, ap, drv)
	addEnabledService(t, models)
	res, err := ap.Apply(context.Background())
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if res.Outcome != OutcomeApplied || !res.Applied {
		t.Errorf("want OutcomeApplied/Applied=true, got %+v", res)
	}
}

func TestApplyOutcomeValidationFailed(t *testing.T) {
	drv := newFakeDriver()
	drv.ValidateErr = errors.New("line 5: unknown keyword")
	ap, models, _, _ := newTestApplier(t, drv, ApplyOptions{})
	syncLive(t, ap, drv)
	addEnabledService(t, models)
	res, err := ap.Apply(context.Background())
	if err != nil {
		t.Fatalf("validation failure must not error: %v", err)
	}
	if res.Outcome != OutcomeValidationFailed || res.Applied {
		t.Errorf("want OutcomeValidationFailed/Applied=false, got %+v", res)
	}
}

func TestApplyOutcomeRolledBackReturnsNilError(t *testing.T) {
	drv := newFakeDriver()
	ap, models, _, _ := newTestApplier(t, drv, ApplyOptions{BackupKeep: 5, Verifier: &fakeVerifier{err: errors.New("not serving")}})
	syncLive(t, ap, drv)
	addEnabledService(t, models)
	res, err := ap.Apply(context.Background())
	if err != nil {
		t.Fatalf("a cleanly rolled-back apply must return a nil error, got %v", err)
	}
	if res.Outcome != OutcomeRolledBack || !res.RolledBack {
		t.Errorf("want OutcomeRolledBack/RolledBack=true, got %+v", res)
	}
	if !strings.Contains(res.Message, "previous config is live") {
		t.Errorf("message should state the previous config is live, got %q", res.Message)
	}
}

func TestApplyOutcomeRollbackFailedReturnsError(t *testing.T) {
	base := newFakeDriver()
	// Verify fails -> rollback; the rollback reload (2nd reload) fails too.
	drv := &reloadFailNth{fakeDriver: base, failOn: 2}
	ap, models, _, _ := newTestApplier(t, drv, ApplyOptions{BackupKeep: 5, Verifier: &fakeVerifier{err: errors.New("not serving")}})
	syncLive(t, ap, base)
	addEnabledService(t, models)
	res, err := ap.Apply(context.Background())
	if err == nil {
		t.Fatal("a failed rollback is indeterminate and must return a non-nil error")
	}
	if res.Outcome != OutcomeRollbackFailed || res.RolledBack {
		t.Errorf("want OutcomeRollbackFailed/RolledBack=false, got %+v", res)
	}
}

func TestRestoreOutcomes(t *testing.T) {
	// Applied.
	drv := newFakeDriver()
	ap, models, _, _ := newTestApplier(t, drv, ApplyOptions{BackupKeep: 5, Verifier: &fakeVerifier{}})
	syncLive(t, ap, drv)
	applyChange(t, ap, models, "svc1", 443)
	var cfgBackup string
	backups, err := ap.ListBackups(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range backups {
		if b.Kind == backupKindCfg && !b.Orig {
			cfgBackup = b.Name
		}
	}
	if cfgBackup == "" {
		t.Fatal("no cfg backup to restore")
	}
	res, err := ap.Restore(context.Background(), cfgBackup)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if res.Outcome != OutcomeApplied || !res.Applied {
		t.Errorf("restore success: want OutcomeApplied, got %+v", res)
	}

	// Validation failed.
	drv.ValidateErr = errors.New("rejected")
	res, err = ap.Restore(context.Background(), cfgBackup)
	if err != nil {
		t.Fatalf("restore validation failure must not error: %v", err)
	}
	if res.Outcome != OutcomeValidationFailed || res.Applied {
		t.Errorf("restore validation: want OutcomeValidationFailed, got %+v", res)
	}
}

func TestRestoreOutcomeRolledBackReturnsNilError(t *testing.T) {
	base := newFakeDriver()
	drv := &reloadFailNth{fakeDriver: base, failOn: 0} // no failure yet
	ap, models, _, _ := newTestApplier(t, drv, ApplyOptions{BackupKeep: 5, Verifier: &fakeVerifier{}})
	syncLive(t, ap, base)
	applyChange(t, ap, models, "svc1", 443)
	liveNow := string(base.Files[base.ConfigPath()])

	var cfgBackup string
	backups, _ := ap.ListBackups(context.Background())
	for _, b := range backups {
		if b.Kind == backupKindCfg && !b.Orig {
			cfgBackup = b.Name
		}
	}
	// Fail only the restore's install reload (1st reload of this Restore).
	drv.n, drv.failOn = 0, 1
	res, err := ap.Restore(context.Background(), cfgBackup)
	if err != nil {
		t.Fatalf("a cleanly rolled-back restore must return a nil error, got %v", err)
	}
	if res.Outcome != OutcomeRolledBack || !res.RolledBack {
		t.Errorf("want OutcomeRolledBack/RolledBack=true, got %+v", res)
	}
	if string(base.Files[base.ConfigPath()]) != liveNow {
		t.Error("restore rollback must leave the config that was live before the restore")
	}
}

// --- Defect 4: staging must not corrupt an unrelated longer path ------------

// A service whose `extra` directive embeds the crt-list path as part of a longer
// path validates in staging with that directive byte-identical; only the bind's
// crt-list is repointed at the staged file.
func TestApplyStagingDoesNotCorruptExtraPath(t *testing.T) {
	base := newFakeDriver()
	drv := &captureValidate{fakeDriver: base}
	ap, models, _, _ := newTestApplier(t, drv, ApplyOptions{Verifier: &fakeVerifier{}})
	crtPath := base.CrtListPath()
	extra := "http-request set-var(txn.p) str(" + crtPath + ".d/unrelated)"
	if _, err := models.Update(func(m *Model) {
		m.Services = append(m.Services, Service{
			ID: "s1", Name: "svc1", Enabled: true, FQDNs: []string{"a.example"},
			ExposedPort: 443, Upstream: Upstream{Host: "127.0.0.1", Port: 9001}, Check: true,
			Extra: []string{extra},
		})
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := ap.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(drv.stagedCfg, "    "+extra+"\n") {
		t.Errorf("the extra directive was corrupted in the staged config:\n%s", drv.stagedCfg)
	}
	// The bind must reference the staged crt-list, not the live path.
	if strings.Contains(drv.stagedCfg, "crt-list "+crtPath+"\n") {
		t.Errorf("staged config still binds the live crt-list path:\n%s", drv.stagedCfg)
	}
}

// Restore stages a literal backup config: only the exact `crt-list <path>` token
// is repointed; a longer path that merely contains the crt-list path is intact.
func TestRestoreReplacesOnlyCrtListToken(t *testing.T) {
	base := newFakeDriver()
	drv := &captureValidate{fakeDriver: base}
	ap, _, _, _ := newTestApplier(t, drv, ApplyOptions{Verifier: &fakeVerifier{}})
	crtPath := base.CrtListPath()
	backupCfg := "global\n    maxconn 2000\n\n" +
		"frontend fe_443\n    bind *:443 ssl crt-list " + crtPath + "\n" +
		"    http-request set-header X-Old " + crtPath + ".bak\n"
	name := backupCfgPrefix + "20260101T000000.000000000Z"
	base.Files[filepath.Join(base.BackupDir(), name)] = []byte(backupCfg)

	res, err := ap.Restore(context.Background(), name)
	if err != nil {
		t.Fatalf("restore: %v (res=%+v)", err, res)
	}
	// The bind's crt-list token was repointed (live path no longer the bind arg).
	if strings.Contains(drv.stagedCfg, "crt-list "+crtPath+"\n") {
		t.Errorf("the bind's crt-list token was not repointed:\n%s", drv.stagedCfg)
	}
	// The unrelated longer path is byte-identical.
	if !strings.Contains(drv.stagedCfg, "X-Old "+crtPath+".bak\n") {
		t.Errorf("an unrelated longer path was corrupted by substitution:\n%s", drv.stagedCfg)
	}
}

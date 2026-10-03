package haproxy

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// --- test doubles -----------------------------------------------------------

// fakeVerifier is an injectable Verifier: it records that it ran and returns a
// chosen error, so verify-failure is deterministic without the service status.
type fakeVerifier struct {
	err   error
	calls int
}

func (v *fakeVerifier) Verify(ctx context.Context) error {
	v.calls++
	return v.err
}

// reloadFailN wraps a fakeDriver and makes the first n Reload calls fail, while
// still recording every reload in the fake's Calls log. It lets a test fail the
// install reload but let the rollback reload succeed.
type reloadFailN struct {
	*fakeDriver
	remaining int
}

func (d *reloadFailN) Reload(ctx context.Context) error {
	_ = d.fakeDriver.Reload(ctx) // records "reload"; inner ReloadErr stays nil
	if d.remaining > 0 {
		d.remaining--
		return errors.New("reload failed")
	}
	return nil
}

// --- helpers ----------------------------------------------------------------

func newTestApplier(t *testing.T, drv Driver, opts ApplyOptions) (*Applier, *ModelStore, *CertStore, string) {
	t.Helper()
	dataDir := t.TempDir()
	models, err := NewModelStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	certs := CertNewStore(dataDir, drv)
	if opts.StagingDir == "" {
		opts.StagingDir = t.TempDir()
	}
	ap := NewApplier(drv, models, certs, NewOpLog(200), opts)
	return ap, models, certs, dataDir
}

// syncLive writes the current candidate into the driver's live files and clears
// the Calls log, so the next operation starts from an in-sync, no-changes state.
func syncLive(t *testing.T, ap *Applier, drv *fakeDriver) {
	t.Helper()
	_, cfg, crt, err := ap.renderCandidate()
	if err != nil {
		t.Fatal(err)
	}
	drv.mu.Lock()
	drv.Files[drv.ConfigPath()] = []byte(cfg)
	drv.Files[drv.CrtListPath()] = []byte(crt)
	drv.Calls = nil
	drv.mu.Unlock()
}

// seedCert writes a tracking row and an on-disk cert file (content-addressed
// name, with key-like bytes) so crt-list rendering has something to include and
// so rollback can be shown never to delete it.
func seedCert(t *testing.T, dataDir string, drv *fakeDriver, name string, enabled bool) string {
	t.Helper()
	data := certStoreData{
		Version: certStoreVersion,
		Dir:     drv.CertsDir(),
		Certs:   []CertEntry{{Name: name, Enabled: enabled, CertMachine: CertSource{ID: 1, FQDN: "x.example"}, SHA256: "deadbeef"}},
	}
	raw, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "certs.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	certPath := filepath.Join(drv.CertsDir(), name)
	drv.mu.Lock()
	drv.Files[certPath] = []byte("cert bytes\n-----BEGIN PRIVATE KEY-----\nsecret\n-----END PRIVATE KEY-----\n")
	drv.mu.Unlock()
	return certPath
}

func addEnabledService(t *testing.T, models *ModelStore) {
	t.Helper()
	if _, err := models.Update(func(m *Model) {
		m.Services = append(m.Services, Service{
			ID: "s1", Name: "svc1", Enabled: true, FQDNs: []string{"a.example"},
			ExposedPort: 443, Upstream: Upstream{Host: "127.0.0.1", Port: 9001}, Check: true,
		})
	}); err != nil {
		t.Fatal(err)
	}
}

func calls(drv *fakeDriver) []string {
	drv.mu.Lock()
	defer drv.mu.Unlock()
	return append([]string(nil), drv.Calls...)
}

func firstIndex(cs []string, s string) int {
	for i, c := range cs {
		if c == s {
			return i
		}
	}
	return -1
}

func lastPrefixIndex(cs []string, prefix string) int {
	idx := -1
	for i, c := range cs {
		if strings.HasPrefix(c, prefix) {
			idx = i
		}
	}
	return idx
}

func countCall(cs []string, s string) int {
	n := 0
	for _, c := range cs {
		if c == s {
			n++
		}
	}
	return n
}

func stagingEmpty(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	// The staged copy is kept on purpose so the owner can read what was tested;
	// anything else left behind would be a leak.
	for _, e := range entries {
		if e.Name() != "haproxy.cfg" && e.Name() != "crt-list.txt" {
			t.Errorf("unexpected file left in the staging dir: %s", e.Name())
		}
	}
}

// --- Pending / change detection --------------------------------------------

func TestPendingUneditedReportsNoChanges(t *testing.T) {
	drv := newFakeDriver()
	ap, _, _, _ := newTestApplier(t, drv, ApplyOptions{})
	syncLive(t, ap, drv)

	ch, err := ap.Pending(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if ch.HasChanges {
		t.Errorf("expected no changes, got %+v", ch)
	}
	if ch.ConfigDiff != "" || ch.CrtListDiff != "" {
		t.Errorf("expected empty diffs, got cfg=%q crt=%q", ch.ConfigDiff, ch.CrtListDiff)
	}
}

func TestApplyUneditedWritesNothing(t *testing.T) {
	drv := newFakeDriver()
	ap, _, _, _ := newTestApplier(t, drv, ApplyOptions{})
	syncLive(t, ap, drv)

	res, err := ap.Apply(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Applied || res.Outcome != OutcomeNoChanges || !strings.Contains(res.Message, "No changes") {
		t.Errorf("expected Applied=false/OutcomeNoChanges, got %+v", res)
	}
	for _, c := range calls(drv) {
		if strings.HasPrefix(c, "write ") || c == "reload" || strings.HasPrefix(c, "validate ") || strings.HasPrefix(c, "list ") {
			t.Errorf("unedited Apply must do nothing, but called %q; all: %v", c, calls(drv))
		}
	}
}

func TestPendingEditReportsChange(t *testing.T) {
	drv := newFakeDriver()
	ap, models, _, _ := newTestApplier(t, drv, ApplyOptions{})
	syncLive(t, ap, drv)
	addEnabledService(t, models)

	ch, err := ap.Pending(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !ch.HasChanges || ch.ConfigDiff == "" {
		t.Errorf("expected a config change with a diff, got %+v", ch)
	}
}

func TestPendingServiceToggleReportsChange(t *testing.T) {
	drv := newFakeDriver()
	ap, models, _, _ := newTestApplier(t, drv, ApplyOptions{})
	addEnabledService(t, models)
	syncLive(t, ap, drv)

	// Disable the service: the config should now differ.
	if _, err := models.Update(func(m *Model) { m.Services[0].Enabled = false }); err != nil {
		t.Fatal(err)
	}
	ch, err := ap.Pending(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !ch.HasChanges || ch.ConfigDiff == "" {
		t.Errorf("expected a config change after a service toggle, got %+v", ch)
	}
}

func TestPendingCertToggleReportsCrtListChange(t *testing.T) {
	drv := newFakeDriver()
	ap, _, certs, dataDir := newTestApplier(t, drv, ApplyOptions{})
	seedCert(t, dataDir, drv, "x.example-0123456789ab.pem", false)
	syncLive(t, ap, drv)

	if err := certs.SetEnabled("x.example-0123456789ab.pem", true); err != nil {
		t.Fatal(err)
	}
	ch, err := ap.Pending(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !ch.HasChanges || ch.CrtListDiff == "" {
		t.Errorf("expected a crt-list change after a cert enable, got %+v", ch)
	}
}

// --- Apply success & ordering ----------------------------------------------

func TestApplySuccessCallsInOrder(t *testing.T) {
	drv := newFakeDriver()
	afterCalled := false
	ap, models, _, _ := newTestApplier(t, drv, ApplyOptions{
		BackupKeep:   5,
		Verifier:     &fakeVerifier{},
		AfterSuccess: func(ctx context.Context) error { afterCalled = true; return nil },
	})
	syncLive(t, ap, drv)
	addEnabledService(t, models)

	res, err := ap.Apply(context.Background())
	if err != nil {
		t.Fatalf("apply: %v (res=%+v)", err, res)
	}
	if !res.Applied || res.Outcome != OutcomeApplied {
		t.Fatalf("expected Applied=true/OutcomeApplied, got %+v", res)
	}
	if !afterCalled {
		t.Error("AfterSuccess not called on success")
	}

	cs := calls(drv)
	lastBackupWrite := lastPrefixIndex(cs, "write "+drv.BackupDir())
	installCrt := firstIndex(cs, "write "+drv.CrtListPath())
	installCfg := firstIndex(cs, "write "+drv.ConfigPath())
	reload := firstIndex(cs, "reload")

	if lastBackupWrite < 0 || installCrt < 0 || installCfg < 0 || reload < 0 {
		t.Fatalf("missing expected calls in %v", cs)
	}
	if !(lastBackupWrite < installCrt && installCrt < installCfg && installCfg < reload) {
		t.Errorf("wrong order: backup=%d crtlist=%d cfg=%d reload=%d\n%v",
			lastBackupWrite, installCrt, installCfg, reload, cs)
	}
}

// --- validate failure -------------------------------------------------------

func TestApplyValidateFailureWritesNothing(t *testing.T) {
	drv := newFakeDriver()
	drv.ValidateErr = errors.New("line 5: unknown keyword 'frontendd'")
	ap, models, _, _ := newTestApplier(t, drv, ApplyOptions{})
	syncLive(t, ap, drv)
	addEnabledService(t, models)

	res, err := ap.Apply(context.Background())
	if err != nil {
		t.Fatalf("validate failure should not be a hard error: %v", err)
	}
	if res.Applied || res.Outcome != OutcomeValidationFailed {
		t.Errorf("expected Applied=false/OutcomeValidationFailed, got %+v", res)
	}
	if !strings.Contains(res.Message, "unknown keyword") {
		t.Errorf("expected the validator message, got %q", res.Message)
	}
	for _, c := range calls(drv) {
		if strings.HasPrefix(c, "write ") || c == "reload" {
			t.Errorf("validation failure must write nothing live, but called %q", c)
		}
	}
	stagingEmpty(t, ap.opts.StagingDir)
}

// --- post-install failure & rollback ---------------------------------------

func TestApplyConfigWriteFailureRestoresCrtList(t *testing.T) {
	drv := newFakeDriver()
	ap, models, certs, dataDir := newTestApplier(t, drv, ApplyOptions{BackupKeep: 5, Verifier: &fakeVerifier{}})
	// Seed a disabled cert and sync, so the live crt-list is empty; enabling it
	// makes the candidate crt-list genuinely differ and be overwritten on
	// install, so the rollback restore is actually exercised.
	seedCert(t, dataDir, drv, "x.example-0123456789ab.pem", false)
	syncLive(t, ap, drv)
	liveCrt := append([]byte(nil), drv.Files[drv.CrtListPath()]...)
	if err := certs.SetEnabled("x.example-0123456789ab.pem", true); err != nil {
		t.Fatal(err)
	}
	addEnabledService(t, models)

	// Guard: the candidate crt-list must actually differ from live, or the
	// restore assertion below would be vacuous.
	if _, _, candCrt, rerr := ap.renderCandidate(); rerr != nil {
		t.Fatal(rerr)
	} else if candCrt == string(liveCrt) {
		t.Fatal("test setup is vacuous: candidate crt-list equals live")
	}
	drv.WriteErrFor[drv.ConfigPath()] = errors.New("disk full")

	res, err := ap.Apply(context.Background())
	if err != nil {
		t.Fatalf("a cleanly rolled-back config-write failure must return a nil error, got %v", err)
	}
	if res.Applied || !res.RolledBack || res.Outcome != OutcomeRolledBack {
		t.Errorf("expected Applied=false/RolledBack=true/OutcomeRolledBack, got %+v", res)
	}
	if string(drv.Files[drv.CrtListPath()]) != string(liveCrt) {
		t.Errorf("crt-list not restored to its pre-apply content")
	}
	if countCall(calls(drv), "reload") < 1 {
		t.Errorf("expected a reload during rollback, calls=%v", calls(drv))
	}
}

func TestApplyReloadFailureRollsBackBothFiles(t *testing.T) {
	base := newFakeDriver()
	drv := &reloadFailN{fakeDriver: base, remaining: 1}
	ap, models, _, _ := newTestApplier(t, drv, ApplyOptions{BackupKeep: 5, Verifier: &fakeVerifier{}})
	syncLive(t, ap, base)
	liveCfg := append([]byte(nil), base.Files[base.ConfigPath()]...)
	liveCrt := append([]byte(nil), base.Files[base.CrtListPath()]...)
	addEnabledService(t, models)

	res, err := ap.Apply(context.Background())
	if err != nil {
		t.Fatalf("a cleanly rolled-back reload failure must return a nil error, got %v", err)
	}
	if !res.RolledBack || res.Outcome != OutcomeRolledBack {
		t.Errorf("expected RolledBack=true/OutcomeRolledBack, got %+v", res)
	}
	if !strings.Contains(res.Message, "previous config is live") {
		t.Errorf("message should state the previous config is live, got %q", res.Message)
	}
	if string(base.Files[base.ConfigPath()]) != string(liveCfg) {
		t.Error("config not restored")
	}
	if string(base.Files[base.CrtListPath()]) != string(liveCrt) {
		t.Error("crt-list not restored")
	}
	if n := countCall(calls(base), "reload"); n != 2 {
		t.Errorf("expected 2 reloads (install + rollback), got %d: %v", n, calls(base))
	}
}

func TestApplyVerifyFailureRollsBack(t *testing.T) {
	drv := newFakeDriver()
	ap, models, _, _ := newTestApplier(t, drv, ApplyOptions{BackupKeep: 5, Verifier: &fakeVerifier{err: errors.New("new worker not serving")}})
	syncLive(t, ap, drv)
	liveCfg := append([]byte(nil), drv.Files[drv.ConfigPath()]...)
	addEnabledService(t, models)

	res, err := ap.Apply(context.Background())
	if err != nil {
		t.Fatalf("a cleanly rolled-back verify failure must return a nil error, got %v", err)
	}
	if !res.RolledBack || res.Outcome != OutcomeRolledBack || !strings.Contains(res.Message, "previous config is live") {
		t.Errorf("expected rollback stating previous config is live, got res=%+v", res)
	}
	if string(drv.Files[drv.ConfigPath()]) != string(liveCfg) {
		t.Error("config not restored after verify failure")
	}
	if n := countCall(calls(drv), "reload"); n != 2 {
		t.Errorf("expected 2 reloads (install + rollback), got %d", n)
	}
}

// --- first-run failure removes the new files, never a cert ------------------

func TestApplyFirstRunFailureRemovesNewFilesKeepsCert(t *testing.T) {
	drv := newFakeDriver()
	ap, _, certs, dataDir := newTestApplier(t, drv, ApplyOptions{BackupKeep: 5, Verifier: &fakeVerifier{err: errors.New("not serving")}})
	certPath := seedCert(t, dataDir, drv, "x.example-0123456789ab.pem", true)
	if err := certs.SetEnabled("x.example-0123456789ab.pem", true); err != nil {
		t.Fatal(err)
	}
	// No live cfg/crt-list: this is a first run.

	res, err := ap.Apply(context.Background())
	if err != nil {
		t.Fatalf("a cleanly rolled-back first-run verify failure must return a nil error, got %v", err)
	}
	if !res.RolledBack || res.Outcome != OutcomeRolledBack {
		t.Errorf("expected RolledBack=true/OutcomeRolledBack, got %+v", res)
	}
	if _, ok := drv.Files[drv.ConfigPath()]; ok {
		t.Error("newly written config should have been removed on first-run rollback")
	}
	if _, ok := drv.Files[drv.CrtListPath()]; ok {
		t.Error("newly written crt-list should have been removed on first-run rollback")
	}
	if _, ok := drv.Files[certPath]; !ok {
		t.Error("rollback must never delete a cert file")
	}
	// No private-key marker in the op log, diffs or the error.
	assertNoPrivateKey(t, ap, err)
}

func assertNoPrivateKey(t *testing.T, ap *Applier, err error) {
	t.Helper()
	for _, e := range ap.log.Snapshot() {
		if strings.Contains(e.Message, "PRIVATE KEY") {
			t.Errorf("op log leaked a private-key marker: %q", e.Message)
		}
	}
	ch, perr := ap.Pending(context.Background())
	if perr == nil {
		if strings.Contains(ch.ConfigDiff, "PRIVATE KEY") || strings.Contains(ch.CrtListDiff, "PRIVATE KEY") {
			t.Error("a diff leaked a private-key marker")
		}
	}
	if err != nil && strings.Contains(err.Error(), "PRIVATE KEY") {
		t.Error("an error leaked a private-key marker")
	}
}

// --- AfterSuccess (superseded cleanup) only on success ----------------------

func TestAfterSuccessNotCalledOnFailure(t *testing.T) {
	drv := newFakeDriver()
	drv.ValidateErr = errors.New("bad config")
	called := false
	ap, models, _, _ := newTestApplier(t, drv, ApplyOptions{
		AfterSuccess: func(ctx context.Context) error { called = true; return nil },
	})
	syncLive(t, ap, drv)
	addEnabledService(t, models)

	if _, err := ap.Apply(context.Background()); err != nil {
		t.Fatalf("validate failure is not a hard error: %v", err)
	}
	if called {
		t.Error("AfterSuccess must not run when Apply did not fully succeed")
	}
}

// --- Check ------------------------------------------------------------------

func TestCheckNeverTouchesLiveAndCleansStaging(t *testing.T) {
	drv := newFakeDriver()
	ap, models, _, _ := newTestApplier(t, drv, ApplyOptions{})
	syncLive(t, ap, drv)
	addEnabledService(t, models)

	cr, err := ap.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !cr.OK {
		t.Errorf("expected OK check, got %+v", cr)
	}
	for _, c := range calls(drv) {
		if strings.HasPrefix(c, "write ") || c == "reload" {
			t.Errorf("Check must not touch live files, but called %q", c)
		}
	}
	stagingEmpty(t, ap.opts.StagingDir)
}

// --- concurrency ------------------------------------------------------------

func TestApplyConcurrentSerialized(t *testing.T) {
	drv := newFakeDriver()
	ap, models, _, _ := newTestApplier(t, drv, ApplyOptions{BackupKeep: 5, Verifier: &fakeVerifier{}})
	addEnabledService(t, models)

	var wg sync.WaitGroup
	results := make([]ApplyResult, 2)
	errs := make([]error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = ap.Apply(context.Background())
		}(i)
	}
	wg.Wait()

	applied := 0
	for i := 0; i < 2; i++ {
		if errs[i] != nil {
			t.Fatalf("apply %d errored: %v", i, errs[i])
		}
		if results[i].Applied {
			applied++
		}
	}
	if applied != 1 {
		t.Errorf("expected exactly one of two concurrent applies to apply changes, got %d", applied)
	}
}

package haproxy

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// applyOnce drives one successful Apply after mutating the model so there is a
// change to apply. It returns after the config is installed.
func applyChange(t *testing.T, ap *Applier, models *ModelStore, name string, port int) {
	t.Helper()
	if _, err := models.Update(func(m *Model) {
		m.Services = append(m.Services, Service{
			ID: name, Name: name, Enabled: true, FQDNs: []string{name + ".example"},
			ExposedPort: port, Upstream: Upstream{Host: "127.0.0.1", Port: port}, Check: true,
		})
	}); err != nil {
		t.Fatal(err)
	}
	res, err := ap.Apply(context.Background())
	if err != nil {
		t.Fatalf("apply: %v (res=%+v)", err, res)
	}
	if !res.Applied {
		t.Fatalf("expected a change to apply, got %+v", res)
	}
}

func TestBackupFirstAdoptionOrigWrittenOnceAndSurvivesPrune(t *testing.T) {
	drv := newFakeDriver()
	ap, models, _, _ := newTestApplier(t, drv, ApplyOptions{BackupKeep: 1, Verifier: &fakeVerifier{}})
	// Pre-existing live config so the first Apply adopts it.
	syncLive(t, ap, drv)

	// Three applies, each changing the model, with keep-1.
	applyChange(t, ap, models, "svc1", 443)
	applyChange(t, ap, models, "svc2", 8443)
	applyChange(t, ap, models, "svc3", 9443)

	backups, err := ap.ListBackups(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var origCfg, origCrt, normalCfg, normalCrt int
	for _, b := range backups {
		switch {
		case b.Orig && b.Kind == backupKindCfg:
			origCfg++
		case b.Orig && b.Kind == backupKindCrtList:
			origCrt++
		case !b.Orig && b.Kind == backupKindCfg:
			normalCfg++
		case !b.Orig && b.Kind == backupKindCrtList:
			normalCrt++
		}
	}
	if origCfg != 1 {
		t.Errorf("expected exactly one .orig cfg backup, got %d", origCfg)
	}
	if origCrt != 1 {
		t.Errorf("expected exactly one .orig crt-list backup, got %d", origCrt)
	}
	if normalCfg != 1 {
		t.Errorf("keep-1 should leave exactly one normal cfg backup, got %d", normalCfg)
	}
	if normalCrt != 1 {
		t.Errorf("keep-1 should leave exactly one normal crt-list backup, got %d", normalCrt)
	}
}

func TestListAndViewBackup(t *testing.T) {
	drv := newFakeDriver()
	ap, models, _, _ := newTestApplier(t, drv, ApplyOptions{BackupKeep: 5, Verifier: &fakeVerifier{}})
	syncLive(t, ap, drv)
	liveCfg := string(drv.Files[drv.ConfigPath()])
	applyChange(t, ap, models, "svc1", 443)

	backups, err := ap.ListBackups(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var cfgBackup string
	for _, b := range backups {
		if b.Kind == backupKindCfg && !b.Orig {
			cfgBackup = b.Name
		}
	}
	if cfgBackup == "" {
		t.Fatal("no normal cfg backup listed")
	}
	got, err := ap.ViewBackup(context.Background(), cfgBackup)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != liveCfg {
		t.Errorf("backup contents = %q, want the pre-apply live config %q", got, liveCfg)
	}

	if _, err := ap.ViewBackup(context.Background(), "../etc/passwd"); err == nil {
		t.Error("expected ViewBackup to reject a traversal name")
	}
	if _, err := ap.ViewBackup(context.Background(), "not-a-backup.txt"); err == nil {
		t.Error("expected ViewBackup to reject a non-backup name")
	}
}

func TestRestoreValidatesBeforeInstalling(t *testing.T) {
	drv := newFakeDriver()
	ap, models, _, _ := newTestApplier(t, drv, ApplyOptions{BackupKeep: 5, Verifier: &fakeVerifier{}})
	syncLive(t, ap, drv)
	origCfg := string(drv.Files[drv.ConfigPath()])
	applyChange(t, ap, models, "svc1", 443)

	backups, err := ap.ListBackups(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var cfgBackup string
	for _, b := range backups {
		if b.Kind == backupKindCfg && !b.Orig {
			cfgBackup = b.Name
		}
	}

	// Now make validation fail: a Restore must validate in staging and write
	// nothing live.
	drv.ValidateErr = errors.New("staged config rejected")
	liveBefore := string(drv.Files[drv.ConfigPath()])

	res, err := ap.Restore(context.Background(), cfgBackup)
	if err != nil {
		t.Fatalf("validation failure is not a hard error: %v", err)
	}
	if res.Applied || res.Outcome != OutcomeValidationFailed {
		t.Errorf("expected Applied=false/OutcomeValidationFailed when restore validation fails, got %+v", res)
	}
	if string(drv.Files[drv.ConfigPath()]) != liveBefore {
		t.Error("a failed-validation restore must not change the live config")
	}
	_ = origCfg
}

func TestRestoreRollsBackOnReloadFailure(t *testing.T) {
	base := newFakeDriver()
	drv := &reloadFailN{fakeDriver: base, remaining: 0}
	ap, models, _, _ := newTestApplier(t, drv, ApplyOptions{BackupKeep: 5, Verifier: &fakeVerifier{}})
	syncLive(t, ap, base)
	applyChange(t, ap, models, "svc1", 443)
	liveNow := string(base.Files[base.ConfigPath()])

	// Arm a single reload failure for the Restore's install reload only.
	drv.remaining = 1

	backups, err := ap.ListBackups(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var cfgBackup string
	for _, b := range backups {
		if b.Kind == backupKindCfg && !b.Orig {
			cfgBackup = b.Name
		}
	}

	res, err := ap.Restore(context.Background(), cfgBackup)
	if err != nil {
		t.Fatalf("a cleanly rolled-back restore must return a nil error, got %v", err)
	}
	if !res.RolledBack || res.Outcome != OutcomeRolledBack || !strings.Contains(res.Message, "previous config is live") {
		t.Errorf("expected rollback stating previous config is live, got res=%+v", res)
	}
	if string(base.Files[base.ConfigPath()]) != liveNow {
		t.Error("restore rollback must leave the config that was live before the restore")
	}
}

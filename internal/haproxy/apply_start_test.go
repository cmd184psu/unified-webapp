package haproxy

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// startFlips wraps a fakeDriver so a successful Start makes Status report active,
// like a real service that came up.
type startFlips struct{ *fakeDriver }

func (d *startFlips) Start(ctx context.Context) error {
	err := d.fakeDriver.Start(ctx)
	if err == nil {
		d.mu.Lock()
		d.Active = true
		d.mu.Unlock()
	}
	return err
}

// statusErr makes Status fail, so the service state is unknown.
type statusErr struct{ *fakeDriver }

func (d *statusErr) Status(ctx context.Context) (ServiceStatus, error) {
	return ServiceStatus{}, errors.New("systemctl unavailable")
}

func inactiveDriver() *fakeDriver {
	d := newFakeDriver()
	d.Active = false
	return d
}

func TestApplyInactiveStartsInsteadOfReload(t *testing.T) {
	drv := inactiveDriver()
	ap, models, _, _ := newTestApplier(t, &startFlips{drv}, ApplyOptions{BackupKeep: 5, Verifier: &fakeVerifier{}})
	syncLive(t, ap, drv)
	addEnabledService(t, models)

	res, err := ap.Apply(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	cs := calls(drv)
	if countCall(cs, "start") != 1 || countCall(cs, "reload") != 0 {
		t.Errorf("want one start and no reload, got %v", cs)
	}
	if !res.Applied || !res.Started || res.Outcome != OutcomeApplied || !strings.Contains(res.Message, "not running, so it was started") {
		t.Errorf("result = %+v", res)
	}
}

func TestApplyInactiveFirstRunThenReloadsOnceActive(t *testing.T) {
	drv := inactiveDriver()
	ap, models, _, _ := newTestApplier(t, &startFlips{drv}, ApplyOptions{BackupKeep: 5, Verifier: &fakeVerifier{}})
	addEnabledService(t, models) // no live files: first run

	res, err := ap.Apply(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !res.Applied || !res.Started {
		t.Fatalf("first run = %+v", res)
	}
	if _, ok := drv.Files[drv.ConfigPath()]; !ok {
		t.Error("config not installed")
	}
	if _, ok := drv.Files[drv.CrtListPath()]; !ok {
		t.Error("crt-list not installed")
	}
	if countCall(calls(drv), "reload") != 0 {
		t.Errorf("no reload expected on first run: %v", calls(drv))
	}

	if _, err := models.Update(func(m *Model) { m.Services[0].ExposedPort = 8443 }); err != nil {
		t.Fatal(err)
	}
	res, err = ap.Apply(context.Background())
	if err != nil || !res.Applied || res.Started {
		t.Fatalf("second apply = %+v, %v", res, err)
	}
	if countCall(calls(drv), "reload") != 1 || countCall(calls(drv), "start") != 1 {
		t.Errorf("second apply must reload, not start: %v", calls(drv))
	}
}

func TestApplyInactiveStartErrorRollsBackWithoutReload(t *testing.T) {
	drv := inactiveDriver()
	drv.StartErr = errors.New("unit failed")
	ap, models, _, _ := newTestApplier(t, drv, ApplyOptions{BackupKeep: 5, Verifier: &fakeVerifier{}})
	syncLive(t, ap, drv)
	liveCfg := string(drv.Files[drv.ConfigPath()])
	addEnabledService(t, models)

	res, err := ap.Apply(context.Background())
	if err != nil {
		t.Fatalf("clean rollback must be a nil error, got %v", err)
	}
	if !res.RolledBack || res.Outcome != OutcomeRolledBack || res.Started || !strings.Contains(res.Message, "previous config is live") {
		t.Errorf("result = %+v", res)
	}
	if countCall(calls(drv), "reload") != 0 {
		t.Errorf("must not reload a stopped service: %v", calls(drv))
	}
	if string(drv.Files[drv.ConfigPath()]) != liveCfg {
		t.Error("config not restored")
	}
}

func TestApplyInactiveFirstRunStartErrorRemovesFiles(t *testing.T) {
	drv := inactiveDriver()
	drv.StartErr = errors.New("unit failed")
	ap, models, _, _ := newTestApplier(t, drv, ApplyOptions{BackupKeep: 5, Verifier: &fakeVerifier{}})
	addEnabledService(t, models)

	res, err := ap.Apply(context.Background())
	if err != nil || res.Outcome != OutcomeRolledBack {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if _, ok := drv.Files[drv.ConfigPath()]; ok {
		t.Error("new config should be removed")
	}
	if _, ok := drv.Files[drv.CrtListPath()]; ok {
		t.Error("new crt-list should be removed")
	}
	if countCall(calls(drv), "reload") != 0 {
		t.Errorf("must not reload: %v", calls(drv))
	}
}

func TestApplyInactiveStartOkVerifyFailsReloadsRestoredFiles(t *testing.T) {
	drv := inactiveDriver()
	ap, models, _, _ := newTestApplier(t, &startFlips{drv}, ApplyOptions{BackupKeep: 5, Verifier: &fakeVerifier{err: errors.New("not serving")}})
	syncLive(t, ap, drv)
	liveCfg := string(drv.Files[drv.ConfigPath()])
	addEnabledService(t, models)

	res, err := ap.Apply(context.Background())
	if err != nil || !res.RolledBack || res.Outcome != OutcomeRolledBack {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if string(drv.Files[drv.ConfigPath()]) != liveCfg {
		t.Error("config not restored")
	}
	cs := calls(drv)
	if countCall(cs, "start") != 1 || countCall(cs, "reload") != 1 {
		t.Errorf("want one start and one reload: %v", cs)
	}
	if i, j := firstIndex(cs, "start"), firstIndex(cs, "reload"); j < i {
		t.Errorf("reload must follow start: %v", cs)
	}
}

func TestApplyInactiveStartOkVerifyFailsFirstRunNoReload(t *testing.T) {
	drv := inactiveDriver()
	ap, models, _, _ := newTestApplier(t, &startFlips{drv}, ApplyOptions{BackupKeep: 5, Verifier: &fakeVerifier{err: errors.New("not serving")}})
	addEnabledService(t, models)

	res, err := ap.Apply(context.Background())
	if err != nil || res.Outcome != OutcomeRolledBack {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if _, ok := drv.Files[drv.ConfigPath()]; ok {
		t.Error("new config should be removed")
	}
	if countCall(calls(drv), "reload") != 0 {
		t.Errorf("no restored files exist, so no reload: %v", calls(drv))
	}
}

func TestApplyStatusErrorKeepsReload(t *testing.T) {
	drv := newFakeDriver()
	ap, models, _, _ := newTestApplier(t, &statusErr{drv}, ApplyOptions{BackupKeep: 5, Verifier: &fakeVerifier{}})
	syncLive(t, ap, drv)
	addEnabledService(t, models)

	res, err := ap.Apply(context.Background())
	if err != nil || !res.Applied || res.Started {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	cs := calls(drv)
	if countCall(cs, "reload") != 1 || countCall(cs, "start") != 0 {
		t.Errorf("unknown state must reload: %v", cs)
	}
}

func TestRestoreInactiveStartsInsteadOfReload(t *testing.T) {
	drv := newFakeDriver()
	ap, models, _, _ := newTestApplier(t, &startFlips{drv}, ApplyOptions{BackupKeep: 5, Verifier: &fakeVerifier{}})
	syncLive(t, ap, drv)
	applyChange(t, ap, models, "svc1", 443)
	backups, err := ap.ListBackups(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var name string
	for _, b := range backups {
		if b.Kind == backupKindCfg && !b.Orig {
			name = b.Name
		}
	}
	drv.mu.Lock()
	drv.Active = false
	drv.Calls = nil
	drv.mu.Unlock()

	res, err := ap.Restore(context.Background(), name)
	if err != nil {
		t.Fatal(err)
	}
	cs := calls(drv)
	if !res.Applied || !res.Started || countCall(cs, "start") != 1 || countCall(cs, "reload") != 0 {
		t.Errorf("res=%+v calls=%v", res, cs)
	}
}

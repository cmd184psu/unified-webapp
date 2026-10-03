package haproxy

// The Apply pipeline (FRD §7, D4, D6; FR-H20..H24). This is the outage-class
// path: a wrong haproxy.cfg on the proxy fronting every module is an outage or
// a silently un-served host, so the rules here are strict and every failure
// after the first install rolls the live files back to exactly what was there.
//
// Nothing is live until Apply, and Apply is a no-op when nothing changed (D4).
// Apply = render candidate -> validate in a staging dir with the real binary
// (D6) -> back up the live files -> install crt-list then config atomically
// through the driver -> reload -> verify -> roll back on any post-install
// failure. Cert files are written into CertsDir BEFORE Apply (B5a) and the
// crt-list only references them once Apply installs it; a failed Apply must
// therefore never delete a cert file. Superseded cert files are removed only
// after a fully successful Apply, via the AfterSuccess hook (B4b wires it to
// CertStore.RemoveSuperseded).

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Verifier decides whether HAProxy is healthy after a reload. The default
// implementation reports healthy when the service is active; build.go wires the
// StatsVerifier (stats.go), which also needs the stats socket to answer.
type Verifier interface {
	Verify(ctx context.Context) error
}

// statusVerifier is the default Verifier: the service must report active.
type statusVerifier struct{ driver Driver }

func (v statusVerifier) Verify(ctx context.Context) error {
	st, err := v.driver.Status(ctx)
	if err != nil {
		return fmt.Errorf("status query failed: %w", err)
	}
	if !st.Active {
		return fmt.Errorf("haproxy is not active after reload: %s", st.Detail)
	}
	return nil
}

// ApplyOptions configures an Applier.
type ApplyOptions struct {
	// BackupKeep is how many timestamped backups per kind to retain; <= 0 keeps
	// all. The ".orig-" first-adoption backup is never pruned regardless.
	BackupKeep int
	// StagingDir is the directory candidate configs are validated in; "" uses
	// the system temp dir. Each Check/Apply makes and removes its own subdir.
	StagingDir string
	// Verifier checks health after reload; nil uses the active-service check.
	Verifier Verifier
	// AfterSuccess runs only after a fully successful Apply (B4b wires it to
	// CertStore.RemoveSuperseded). Its error does not fail the apply.
	AfterSuccess func(ctx context.Context) error
}

// Applier runs the Apply pipeline against a Driver, the model and cert stores,
// and an op log. Apply, Restore and the Reload/Restart/Start actions are all
// serialized by mu, so no action loads an inconsistent config/crt-list pair
// mid-Apply. Status is read-only and stays lock-free.
type Applier struct {
	driver   Driver
	models   *ModelStore
	certs    *CertStore
	log      *OpLog
	opts     ApplyOptions
	verifier Verifier

	mu sync.Mutex // serializes Apply, Restore, Reload, Restart and Start (Status stays lock-free)
}

// NewApplier builds an Applier. A nil op log is replaced with a tiny one so
// logging never panics.
func NewApplier(driver Driver, models *ModelStore, certs *CertStore, log *OpLog, opts ApplyOptions) *Applier {
	if log == nil {
		log = NewOpLog(1)
	}
	v := opts.Verifier
	if v == nil {
		v = statusVerifier{driver: driver}
	}
	return &Applier{driver: driver, models: models, certs: certs, log: log, opts: opts, verifier: v}
}

// Changes is the pending-changes report (FR-H20): whether the candidate differs
// from the live files, and the unified diffs for the Review-changes view.
type Changes struct {
	HasChanges  bool   `json:"hasChanges"`
	ConfigDiff  string `json:"configDiff"`
	CrtListDiff string `json:"crtListDiff"`
	Summary     string `json:"summary"`
}

// CheckResult is the outcome of Check (FR-H22): whether the candidate passed
// `haproxy -c` in staging, and the validator's message when it did not.
type CheckResult struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

// Outcome names exactly what an Apply or Restore did. It is the machine-readable
// companion to Message: the Go error return is reserved for INDETERMINATE state
// (a failed rollback, or backup/staging infrastructure errors). Every outcome
// fully described by ApplyResult carries a nil error, so an HTTP layer renders a
// cleanly rolled-back apply as a normal result, never a 500.
type Outcome string

const (
	// OutcomeApplied: the candidate is installed, reloaded and verified.
	OutcomeApplied Outcome = "applied"
	// OutcomeNoChanges: nothing differed from the live files; nothing was done.
	OutcomeNoChanges Outcome = "no_changes"
	// OutcomeValidationFailed: `haproxy -c` rejected the staged candidate; the
	// live files were never touched.
	OutcomeValidationFailed Outcome = "validation_failed"
	// OutcomeRolledBack: a post-validation failure was fully rolled back and the
	// previous config is live. Accompanied by a nil error.
	OutcomeRolledBack Outcome = "rolled_back"
	// OutcomeRollbackFailed: the rollback itself failed, so the live state may be
	// inconsistent. This is the one failure outcome accompanied by a non-nil
	// error (the indeterminate case).
	OutcomeRollbackFailed Outcome = "rollback_failed"
)

// ApplyResult is the outcome of Apply/Restore. Outcome names what happened; the
// error return is used ONLY for indeterminate state (OutcomeRollbackFailed, or
// an infrastructure error with an empty Outcome).
type ApplyResult struct {
	Applied    bool    `json:"applied"`
	RolledBack bool    `json:"rolledBack"`
	Outcome    Outcome `json:"outcome"`
	Message    string  `json:"message"`
	// Started is true when HAProxy was not running and Apply/Restore started it
	// (loading the new config) instead of reloading.
	Started bool `json:"started,omitempty"`
}

// StatusInfo is the service-status panel data (FR-H23): the driver's status
// plus HAProxy's version.
type StatusInfo struct {
	ServiceStatus
	Version string `json:"version"`
}

// liveState is the pre-apply content of the live files, used for backup and for
// rollback.
type liveState struct {
	cfg           []byte
	cfgExists     bool
	crtList       []byte
	crtListExists bool
}

// renderCandidate renders the candidate config and crt-list from the current
// model and cert store, with the driver's real (final) paths. It also returns
// the model snapshot it rendered from, so the caller can re-render the SAME
// model into staging with the staged crt-list path (D6) without a textual
// substitution.
func (a *Applier) renderCandidate() (model *Model, cfgText, crtListText string, err error) {
	model = a.models.Snapshot()
	cfgText = Render(model, a.driver.BaselineGlobal(), a.driver.StatsSocketPath(), a.driver.StatsSocketOwner(), a.driver.CrtListPath())
	crtListText, err = a.certs.CrtList()
	if err != nil {
		return nil, "", "", err
	}
	return model, cfgText, crtListText, nil
}

// renderStagedCfg renders model into a config whose TLS binds reference the
// staged crt-list path, so staging validates the candidate crt-list and certs
// directly (D6) rather than textually rewriting the final config.
func (a *Applier) renderStagedCfg(model *Model, stagedCrtListPath string) string {
	return Render(model, a.driver.BaselineGlobal(), a.driver.StatsSocketPath(), a.driver.StatsSocketOwner(), stagedCrtListPath)
}

// replaceCrtListToken rewrites each exact `crt-list <oldPath>` token the
// generator emits to `crt-list <newPath>`, matching only where oldPath is the
// whole crt-list argument (bounded by whitespace or end of input). A longer path
// that merely contains oldPath (a service's extra directive, a raw section) is
// left untouched. It is used for Restore, whose config text is a literal backup.
func replaceCrtListToken(cfgText, oldPath, newPath string) string {
	token := "crt-list " + oldPath
	repl := "crt-list " + newPath
	var b strings.Builder
	for {
		i := strings.Index(cfgText, token)
		if i < 0 {
			b.WriteString(cfgText)
			break
		}
		end := i + len(token)
		b.WriteString(cfgText[:i])
		if end >= len(cfgText) || isConfigSpace(cfgText[end]) {
			b.WriteString(repl)
		} else {
			b.WriteString(token)
		}
		cfgText = cfgText[end:]
	}
	return b.String()
}

func isConfigSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}

// readLive reads a live file through the driver, reporting a missing file as
// (nil, false, nil) rather than an error.
func (a *Applier) readLive(ctx context.Context, path string) ([]byte, bool, error) {
	b, err := a.driver.PrivilegedRead(ctx, path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return b, true, nil
}

// compare reads the live files and compares them with the candidate text,
// producing the pending-changes report and the live state for backup/rollback.
// A missing live file counts as changed.
func (a *Applier) compare(ctx context.Context, cfgText, crtListText string) (liveState, Changes, error) {
	liveCfg, cfgExists, err := a.readLive(ctx, a.driver.ConfigPath())
	if err != nil {
		return liveState{}, Changes{}, err
	}
	liveCrt, crtExists, err := a.readLive(ctx, a.driver.CrtListPath())
	if err != nil {
		return liveState{}, Changes{}, err
	}

	cfgChanged := !cfgExists || string(liveCfg) != cfgText
	crtChanged := !crtExists || string(liveCrt) != crtListText

	changes := Changes{
		HasChanges:  cfgChanged || crtChanged,
		ConfigDiff:  UnifiedDiff("live/haproxy.cfg", "candidate/haproxy.cfg", string(liveCfg), cfgText),
		CrtListDiff: UnifiedDiff("live/crt-list.txt", "candidate/crt-list.txt", string(liveCrt), crtListText),
		Summary:     summarize(cfgChanged, crtChanged),
	}
	live := liveState{cfg: liveCfg, cfgExists: cfgExists, crtList: liveCrt, crtListExists: crtExists}
	return live, changes, nil
}

func summarize(cfgChanged, crtChanged bool) string {
	switch {
	case cfgChanged && crtChanged:
		return "config and crt-list have pending changes"
	case cfgChanged:
		return "config has pending changes"
	case crtChanged:
		return "crt-list has pending changes"
	default:
		return "No changes"
	}
}

// Pending reports whether the candidate config and crt-list differ from the
// live files (FR-H20). It writes nothing.
func (a *Applier) Pending(ctx context.Context) (Changes, error) {
	_, cfgText, crtListText, err := a.renderCandidate()
	if err != nil {
		return Changes{}, err
	}
	_, changes, err := a.compare(ctx, cfgText, crtListText)
	return changes, err
}

// Check renders the candidate, stages it and runs `haproxy -c` on it (FR-H22,
// steps 1-2 of §7). It never touches the live files and always removes the
// staging dir.
func (a *Applier) Check(ctx context.Context) (CheckResult, error) {
	model, _, crtListText, err := a.renderCandidate()
	if err != nil {
		return CheckResult{}, err
	}
	valid, msg, err := a.stageAndValidate(ctx, crtListText, func(stagedCrtListPath string) string {
		return a.renderStagedCfg(model, stagedCrtListPath)
	})
	if err != nil {
		return CheckResult{}, err
	}
	return CheckResult{OK: valid, Message: msg}, nil
}

// stageAndValidate writes the candidate crt-list and config into a fresh temp
// dir under StagingDir (ordinary file I/O, never the live files), runs the
// driver's validator on the staged config, and removes the dir. The staged
// config is produced by buildStagedCfg from the path the staged crt-list was
// written to, so validation sees the candidate crt-list and certs (D6) without
// any textual substitution against the final config. A non-nil error is an
// infrastructure failure (temp dir / write); the bool and string report the
// validator's verdict.
func (a *Applier) stageAndValidate(ctx context.Context, crtListText string, buildStagedCfg func(stagedCrtListPath string) string) (bool, string, error) {
	dir, err := os.MkdirTemp(a.opts.StagingDir, "haproxy-stage-*")
	if err != nil {
		return false, "", fmt.Errorf("haproxy: create staging dir: %w", err)
	}
	defer os.RemoveAll(dir)

	stagedCrtList := filepath.Join(dir, "crt-list.txt")
	stagedCfg := filepath.Join(dir, "haproxy.cfg")
	cfgForStage := buildStagedCfg(stagedCrtList)

	if err := os.WriteFile(stagedCrtList, []byte(crtListText), 0o600); err != nil {
		return false, "", fmt.Errorf("haproxy: write staged crt-list: %w", err)
	}
	if err := os.WriteFile(stagedCfg, []byte(cfgForStage), 0o600); err != nil {
		return false, "", fmt.Errorf("haproxy: write staged config: %w", err)
	}

	if err := a.driver.Validate(ctx, stagedCfg); err != nil {
		return false, err.Error(), nil
	}
	return true, "", nil
}

// Apply runs the full pipeline (FR-H21, §7). It serializes with other Apply and
// Restore calls. When nothing changed it does nothing and says so (D4).
func (a *Applier) Apply(ctx context.Context) (ApplyResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	model, cfgText, crtListText, err := a.renderCandidate()
	if err != nil {
		return ApplyResult{}, err
	}
	live, changes, err := a.compare(ctx, cfgText, crtListText)
	if err != nil {
		return ApplyResult{}, err
	}
	if !changes.HasChanges {
		a.log.Add("apply: no changes to apply")
		return ApplyResult{Applied: false, Outcome: OutcomeNoChanges, Message: "No changes to apply"}, nil
	}

	// The staged config is rendered directly from the same model with the staged
	// crt-list path (D6, Defect 4) — never a textual rewrite of cfgText.
	buildStaged := func(stagedCrtListPath string) string {
		return a.renderStagedCfg(model, stagedCrtListPath)
	}
	res, err := a.run(ctx, cfgText, crtListText, buildStaged, live, "apply")
	if err != nil {
		return res, err
	}
	if res.Applied && a.opts.AfterSuccess != nil {
		if e := a.opts.AfterSuccess(ctx); e != nil {
			a.log.Addf("apply: post-apply cleanup warning: %v", e)
			res.Message += " (cleanup warning: " + e.Error() + ")"
		}
	}
	return res, nil
}

// run is the shared install core for Apply and Restore: validate in staging,
// back up, install crt-list then config, reload, verify, roll back on any
// failure after the first install. It assumes the caller holds a.mu.
func (a *Applier) run(ctx context.Context, cfgText, crtListText string, buildStagedCfg func(stagedCrtListPath string) string, live liveState, op string) (ApplyResult, error) {
	a.log.Addf("%s: validating candidate in staging", op)
	valid, vmsg, err := a.stageAndValidate(ctx, crtListText, buildStagedCfg)
	if err != nil {
		return ApplyResult{}, err
	}
	if !valid {
		a.log.Addf("%s: validation failed: %s", op, vmsg)
		return ApplyResult{Applied: false, Outcome: OutcomeValidationFailed, Message: "validation failed: " + vmsg}, nil
	}

	if err := a.backup(ctx, live); err != nil {
		return ApplyResult{}, fmt.Errorf("haproxy: %s: backup failed: %w", op, err)
	}
	if err := a.prune(ctx); err != nil {
		// Pruning is best-effort; a failure never fails the apply.
		a.log.Addf("%s: prune backups: %v", op, err)
	}

	mode := a.driver.Ownership().ConfigMode
	crtListInstalled := false
	cfgInstalled := false

	// `systemctl reload` fails on a stopped service, so record whether HAProxy is
	// running before touching anything. A Status error leaves the state unknown,
	// and unknown keeps the reload behaviour.
	inactive := false
	if st, serr := a.driver.Status(ctx); serr != nil {
		a.log.Addf("%s: could not read the service status (%v); assuming it is running", op, serr)
	} else if !st.Active {
		inactive = true
	}

	// Install the crt-list first (the config's TLS binds reference it). A failure
	// here changes nothing live, so the previous config is still loaded: a clean,
	// fully-described outcome (nil error), not an indeterminate one.
	if err := a.driver.PrivilegedWrite(ctx, a.driver.CrtListPath(), []byte(crtListText), mode); err != nil {
		msg := fmt.Sprintf("installing crt-list failed: %v; nothing was changed, the previous config is live", err)
		a.log.Addf("%s: %s", op, msg)
		return ApplyResult{Applied: false, RolledBack: true, Outcome: OutcomeRolledBack, Message: msg}, nil
	}
	crtListInstalled = true
	a.log.Addf("%s: installed crt-list", op)

	// Then the config.
	if err := a.driver.PrivilegedWrite(ctx, a.driver.ConfigPath(), []byte(cfgText), mode); err != nil {
		a.log.Addf("%s: installing config failed: %v", op, err)
		// Nothing was reloaded or started, so the service state is untouched.
		rbErr := a.rollback(ctx, crtListInstalled, cfgInstalled, live, !inactive)
		return a.rollbackResult(op, "installing config", err, rbErr)
	}
	cfgInstalled = true
	a.log.Addf("%s: installed config", op)

	started := false
	if inactive {
		if err := a.driver.Start(ctx); err != nil {
			a.log.Addf("%s: HAProxy was not running and starting it failed: %v", op, err)
			// Nothing is running, so there is nothing to reload after the restore.
			rbErr := a.rollback(ctx, crtListInstalled, cfgInstalled, live, false)
			return a.rollbackResult(op, "start", err, rbErr)
		}
		started = true
		a.log.Addf("%s: HAProxy was not running, so it was started with the new configuration", op)
	} else {
		if err := a.driver.Reload(ctx); err != nil {
			a.log.Addf("%s: reload failed: %v", op, err)
			rbErr := a.rollback(ctx, crtListInstalled, cfgInstalled, live, true)
			return a.rollbackResult(op, "reload", err, rbErr)
		}
		a.log.Addf("%s: reloaded", op)
	}
	// After a rollback, reload when the service was running before; when we
	// started it ourselves, reload only if restored files exist to load.
	reloadOnRollback := !inactive || live.cfgExists

	if err := a.verifier.Verify(ctx); err != nil {
		a.log.Addf("%s: verify failed: %v", op, err)
		rbErr := a.rollback(ctx, crtListInstalled, cfgInstalled, live, reloadOnRollback)
		return a.rollbackResult(op, "verify", err, rbErr)
	}
	a.log.Addf("%s: verified; succeeded", op)
	if started {
		lead := "Applied."
		if op == "restore" {
			lead = "Restored."
		}
		return ApplyResult{Applied: true, Started: true, Outcome: OutcomeApplied, Message: lead + " HAProxy was not running, so it was started."}, nil
	}
	return ApplyResult{Applied: true, Outcome: OutcomeApplied, Message: op + " succeeded"}, nil
}

// rollback restores the live files to exactly their pre-apply state and, when reload
// is true, reloads again (never on a service known to be stopped). It restores only files this run actually installed, and NEVER removes
// a cert file (it touches only the config and crt-list paths).
func (a *Applier) rollback(ctx context.Context, crtListInstalled, cfgInstalled bool, live liveState, reload bool) error {
	mode := a.driver.Ownership().ConfigMode
	var errs []string

	restore := func(installed, existed bool, path string, content []byte) {
		if !installed {
			return
		}
		if existed {
			if err := a.driver.PrivilegedWrite(ctx, path, content, mode); err != nil {
				errs = append(errs, fmt.Sprintf("restore %s: %v", path, err))
			}
		} else {
			if err := a.driver.PrivilegedRemove(ctx, path); err != nil {
				errs = append(errs, fmt.Sprintf("remove %s: %v", path, err))
			}
		}
	}

	restore(cfgInstalled, live.cfgExists, a.driver.ConfigPath(), live.cfg)
	restore(crtListInstalled, live.crtListExists, a.driver.CrtListPath(), live.crtList)

	if reload {
		if err := a.driver.Reload(ctx); err != nil {
			errs = append(errs, fmt.Sprintf("reload after rollback: %v", err))
		}
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

// rollbackResult builds the result after a rollback. A successful rollback is a
// fully-described, clean outcome (OutcomeRolledBack, nil error): the previous
// config is live. A FAILED rollback is indeterminate (OutcomeRollbackFailed) and
// is the one failure outcome that carries a non-nil error.
func (a *Applier) rollbackResult(op, stage string, cause, rbErr error) (ApplyResult, error) {
	if rbErr != nil {
		msg := fmt.Sprintf("%s failed: %v; ROLLBACK ALSO FAILED: %v; the live config may be inconsistent", stage, cause, rbErr)
		a.log.Addf("%s: %s", op, msg)
		return ApplyResult{Applied: false, RolledBack: false, Outcome: OutcomeRollbackFailed, Message: msg},
			fmt.Errorf("haproxy: %s: %s", op, msg)
	}
	msg := fmt.Sprintf("%s failed: %v; rolled back, the previous config is live", stage, cause)
	a.log.Addf("%s: %s", op, msg)
	return ApplyResult{Applied: false, RolledBack: true, Outcome: OutcomeRolledBack, Message: msg}, nil
}

// WithLock runs fn while holding the Applier's lock, so it cannot interleave
// with Apply, Restore or a service action.
func (a *Applier) WithLock(fn func() error) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return fn()
}

// Reload performs a logged graceful reload. It serializes with Apply and Restore
// on the same lock, so a reload can never fire mid-Apply (crt-list installed,
// config not yet) and load an inconsistent pair.
func (a *Applier) Reload(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.log.Add("reload requested")
	if err := a.driver.Reload(ctx); err != nil {
		a.log.Addf("reload failed: %v", err)
		return err
	}
	a.log.Add("reloaded")
	return nil
}

// Restart performs a logged full restart. It serializes with Apply and Restore
// on the same lock.
func (a *Applier) Restart(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.log.Add("restart requested")
	if err := a.driver.Restart(ctx); err != nil {
		a.log.Addf("restart failed: %v", err)
		return err
	}
	a.log.Add("restarted")
	return nil
}

// Start performs a logged service start. It serializes with Apply and Restore on
// the same lock.
func (a *Applier) Start(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.log.Add("start requested")
	if err := a.driver.Start(ctx); err != nil {
		a.log.Addf("start failed: %v", err)
		return err
	}
	a.log.Add("started")
	return nil
}

// Status reports the service status plus HAProxy's version (FR-H23).
func (a *Applier) Status(ctx context.Context) (StatusInfo, error) {
	st, err := a.driver.Status(ctx)
	if err != nil {
		return StatusInfo{}, err
	}
	ver, verr := a.driver.Version(ctx)
	if verr != nil {
		// A missing version is not fatal to the status panel.
		ver = ""
	}
	return StatusInfo{ServiceStatus: st, Version: ver}, nil
}

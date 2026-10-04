package haproxy

// Timestamped backups of the live config and crt-list (FRD §7 step 3, FR-H23).
// Before every install Apply backs up whatever it is about to overwrite, keeps
// the newest N per kind, and — the very first time it adopts a config that was
// already live — keeps one ".orig-" backup that is never pruned, so the
// operator's original is always recoverable. Restore puts a backup back through
// the same validate / backup / install / rollback pipeline as Apply.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	backupCfgPrefix     = "haproxy.cfg."
	backupCrtListPrefix = "crt-list.txt."
	backupOrigInfix     = "orig-"
	// backupTSLayout is UTC and fixed-width so lexical order is chronological.
	backupTSLayout = "20060102T150405.000000000Z"
)

const (
	backupKindCfg     = "cfg"
	backupKindCrtList = "crtlist"
)

// Backup is one backup file: its kind, the timestamp it was taken at, its file
// name and whether it is the never-pruned first-adoption original.
type Backup struct {
	Name      string `json:"name"`
	Kind      string `json:"kind"` // backupKindCfg | backupKindCrtList
	Timestamp string `json:"timestamp"`
	Orig      bool   `json:"orig"`
}

// classifyBackup recognizes a backup file name and extracts its kind, timestamp
// and whether it is an ".orig-" original. ok is false for an unrelated name.
func classifyBackup(name string) (kind, ts string, orig, ok bool) {
	var rest string
	switch {
	case strings.HasPrefix(name, backupCfgPrefix):
		kind = backupKindCfg
		rest = name[len(backupCfgPrefix):]
	case strings.HasPrefix(name, backupCrtListPrefix):
		kind = backupKindCrtList
		rest = name[len(backupCrtListPrefix):]
	default:
		return "", "", false, false
	}
	if rest == "" {
		return "", "", false, false
	}
	if strings.HasPrefix(rest, backupOrigInfix) {
		return kind, rest[len(backupOrigInfix):], true, true
	}
	return kind, rest, false, true
}

// validateBackupName rejects an empty name or one with a path separator or
// traversal, so a name can never escape the backup directory.
func validateBackupName(name string) error {
	if name == "" {
		return fmt.Errorf("haproxy: empty backup name")
	}
	if strings.ContainsAny(name, `/\`) || strings.Contains(name, "..") {
		return fmt.Errorf("haproxy: unsafe backup name %q", name)
	}
	return nil
}

// backup writes a timestamped backup of each live file that exists, and — the
// first time any backup is taken while a live config exists — also writes a
// never-pruned ".orig-" copy (FRD §7 step 3).
func (a *Applier) backup(ctx context.Context, live liveState) error {
	dir := a.driver.BackupDir()
	existing, err := a.driver.PrivilegedList(ctx, dir)
	if err != nil {
		return err
	}
	firstAdoption := len(existing) == 0
	mode := a.driver.Ownership().ConfigMode
	ts := time.Now().UTC().Format(backupTSLayout)

	write := func(prefix string, content []byte) error {
		if err := a.driver.PrivilegedWrite(ctx, filepath.Join(dir, prefix+ts), content, mode); err != nil {
			return err
		}
		if firstAdoption {
			if err := a.driver.PrivilegedWrite(ctx, filepath.Join(dir, prefix+backupOrigInfix+ts), content, mode); err != nil {
				return err
			}
		}
		return nil
	}

	if live.cfgExists {
		if err := write(backupCfgPrefix, live.cfg); err != nil {
			return err
		}
	}
	if live.crtListExists {
		if err := write(backupCrtListPrefix, live.crtList); err != nil {
			return err
		}
	}
	a.log.Addf("backed up live files (ts=%s, firstAdoption=%v)", ts, firstAdoption)
	return nil
}

// prune removes all but the newest BackupKeep backups of each kind, never
// touching an ".orig-" original. BackupKeep <= 0 keeps everything.
func (a *Applier) prune(ctx context.Context) error {
	if a.opts.BackupKeep <= 0 {
		return nil
	}
	dir := a.driver.BackupDir()
	list, err := a.driver.PrivilegedList(ctx, dir)
	if err != nil {
		return err
	}
	byKind := map[string][]string{}
	for _, f := range list {
		kind, _, orig, ok := classifyBackup(f.Name)
		if !ok || orig {
			continue
		}
		byKind[kind] = append(byKind[kind], f.Name)
	}
	for _, names := range byKind {
		// Newest first: within a kind the prefix is identical, so name order is
		// timestamp order.
		sort.Sort(sort.Reverse(sort.StringSlice(names)))
		for i := a.opts.BackupKeep; i < len(names); i++ {
			if err := a.driver.PrivilegedRemove(ctx, filepath.Join(dir, names[i])); err != nil {
				return err
			}
		}
	}
	return nil
}

// ListBackups returns the backups in the driver's backup dir, newest first
// (FR-H23).
func (a *Applier) ListBackups(ctx context.Context) ([]Backup, error) {
	list, err := a.driver.PrivilegedList(ctx, a.driver.BackupDir())
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err // a missing folder just means nothing has been backed up yet
	}
	out := []Backup{}
	for _, f := range list {
		kind, ts, orig, ok := classifyBackup(f.Name)
		if !ok {
			continue
		}
		out = append(out, Backup{Name: f.Name, Kind: kind, Timestamp: ts, Orig: orig})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Timestamp != out[j].Timestamp {
			return out[i].Timestamp > out[j].Timestamp
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// ViewBackup returns the contents of one backup file (FR-H23).
func (a *Applier) ViewBackup(ctx context.Context, name string) ([]byte, error) {
	if err := validateBackupName(name); err != nil {
		return nil, err
	}
	if _, _, _, ok := classifyBackup(name); !ok {
		return nil, fmt.Errorf("haproxy: %q is not a backup file", name)
	}
	return a.driver.PrivilegedRead(ctx, filepath.Join(a.driver.BackupDir(), name))
}

// Restore puts a backup back (FR-H23): it stages the backup's contents,
// validates them, then installs and reloads with the same backup-before and
// rollback-on-failure semantics as Apply. Restoring a config backup also
// restores the crt-list backup of the same timestamp when one is present;
// otherwise the live crt-list is kept. It serializes with Apply.
func (a *Applier) Restore(ctx context.Context, name string) (ApplyResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if err := validateBackupName(name); err != nil {
		return ApplyResult{}, err
	}
	kind, _, _, ok := classifyBackup(name)
	if !ok {
		return ApplyResult{}, fmt.Errorf("haproxy: %q is not a backup file", name)
	}
	dir := a.driver.BackupDir()
	content, err := a.driver.PrivilegedRead(ctx, filepath.Join(dir, name))
	if err != nil {
		return ApplyResult{}, fmt.Errorf("haproxy: read backup %q: %w", name, err)
	}

	liveCfg, cfgExists, err := a.readLive(ctx, a.driver.ConfigPath())
	if err != nil {
		return ApplyResult{}, err
	}
	liveCrt, crtExists, err := a.readLive(ctx, a.driver.CrtListPath())
	if err != nil {
		return ApplyResult{}, err
	}

	var cfgText, crtListText string
	switch kind {
	case backupKindCfg:
		cfgText = string(content)
		// Restore the matching crt-list backup if present, else keep live.
		mate := backupCrtListPrefix + strings.TrimPrefix(name, backupCfgPrefix)
		if mb, merr := a.driver.PrivilegedRead(ctx, filepath.Join(dir, mate)); merr == nil {
			crtListText = string(mb)
		} else {
			crtListText = string(liveCrt)
		}
	case backupKindCrtList:
		crtListText = string(content)
		cfgText = string(liveCfg)
	}

	live := liveState{cfg: liveCfg, cfgExists: cfgExists, crtList: liveCrt, crtListExists: crtExists}
	a.log.Addf("restore: restoring backup %q", name)
	// The backup config is a literal; for staging, repoint ONLY the exact
	// `crt-list <CrtListPath>` token the generator emits (Defect 4), never a bare
	// substring that a longer path might contain.
	buildStaged := func(stagedCrtListPath string) string {
		return replaceCrtListToken(cfgText, a.driver.CrtListPath(), stagedCrtListPath)
	}
	return a.run(ctx, cfgText, crtListText, buildStaged, live, "restore")
}

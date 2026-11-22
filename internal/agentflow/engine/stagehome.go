// Copyright 2026 FreeSociety3 contributors
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"cmd184psu/unified-webapp/internal/agentflow/model"
	"cmd184psu/unified-webapp/internal/agentflow/store"
)

// StageHomeDir is the per-stage directory that holds the agent's private HOME.
func (e *Engine) StageHomeDir(stage int64) string {
	return filepath.Join(e.StageDir(stage), "home")
}

// FreeSpaceBytes reports the free bytes available on the filesystem holding the
// agentflow data directory.
func (e *Engine) FreeSpaceBytes() (int64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(e.deps.DataDir, &st); err != nil {
		return 0, err
	}
	if runtime.GOOS == "windows" {
		return int64(st.Bavail) * int64(st.Bsize), nil
	}
	return int64(st.Bavail) * int64(st.Bsize), nil
}

// MinFreeSpaceBytes is the configured free-space threshold.
func (e *Engine) MinFreeSpaceBytes(ctx context.Context) (int64, error) {
	s, err := e.deps.Store.GetSettings(ctx)
	if err != nil {
		return 0, err
	}
	return int64(s.MinFreeSpaceGB) * 1024 * 1024 * 1024, nil
}

// DiskLow reports whether free space is below the configured threshold.
func (e *Engine) DiskLow(ctx context.Context) bool {
	free, err := e.FreeSpaceBytes()
	if err != nil {
		return false
	}
	threshold, err := e.MinFreeSpaceBytes(ctx)
	if err != nil {
		return false
	}
	return free < threshold
}

// LowDisk is an alias for DiskLow.
func (e *Engine) LowDisk(ctx context.Context) bool { return e.DiskLow(ctx) }

// validateStageHomePath ensures p is exactly <data>/stages/<id>/home, with no
// symlink component and no escape from the data directory.
func (e *Engine) validateStageHomePath(p string) (int64, error) {
	return validateStageHomePath(e.deps.DataDir, p)
}

func validateStageHomePath(dataDir, p string) (int64, error) {
	dataDir = filepath.Clean(dataDir)
	p = filepath.Clean(p)
	if dataDir == "" {
		return 0, fmt.Errorf("stage home: empty data dir")
	}
	rel, err := filepath.Rel(dataDir, p)
	if err != nil {
		return 0, err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return 0, fmt.Errorf("stage home: path outside data dir")
	}
	parts := strings.Split(rel, string(os.PathSeparator))
	if len(parts) != 3 || parts[0] != "stages" || parts[2] != "home" {
		return 0, fmt.Errorf("stage home: not of form <data>/stages/<id>/home")
	}
	id, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("stage home: invalid stage id")
	}
	// Refuse any symlink in the path and require every component to be a directory.
	cur := dataDir
	fi, err := os.Lstat(cur)
	if err != nil {
		return 0, err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return 0, fmt.Errorf("stage home: symlinked data dir")
	}
	for _, part := range parts {
		cur = filepath.Join(cur, part)
		fi, err := os.Lstat(cur)
		if err != nil {
			return 0, err
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			return 0, fmt.Errorf("stage home: symlinked component")
		}
		if !fi.IsDir() {
			return 0, fmt.Errorf("stage home: not a directory")
		}
	}
	return id, nil
}

// PruneStageHomePath deletes one stage home directory after validating its path.
// isRunning, when non-nil, is consulted to make sure a running stage is never
// touched.
func PruneStageHomePath(dataDir, p string, isRunning func(int64) bool) error {
	id, err := validateStageHomePath(dataDir, p)
	if err != nil {
		return err
	}
	if isRunning != nil && isRunning(id) {
		return fmt.Errorf("stage %d is running", id)
	}
	// Keep events.jsonl/prompt.md: remove only home/.
	if err := os.RemoveAll(p); err != nil {
		return err
	}
	return nil
}

// PruneStageHome deletes a single stage's home directory, refusing running stages.
func (e *Engine) PruneStageHome(ctx context.Context, stage int64) error {
	st, err := e.deps.Store.GetStage(ctx, stage)
	if err == nil && (st.State == model.StageQueued || st.State == model.StageRunning) {
		return fmt.Errorf("stage %d is %s", stage, st.State)
	}
	p := e.StageHomeDir(stage)
	id, err := e.validateStageHomePath(p)
	if err != nil {
		return err
	}
	if id != stage {
		return fmt.Errorf("stage home: id mismatch")
	}
	return os.RemoveAll(p)
}

// PruneStageHomeNow is an alias used by the UI/API.
func (e *Engine) PruneStageHomesNow(ctx context.Context) (int, error) {
	return e.PruneExpiredStageHomes(ctx, true)
}

// PruneExpiredStageHomes deletes completed stage homes older than the keep
// window. When force is true, every completed stage home is deleted.
func (e *Engine) PruneExpiredStageHomes(ctx context.Context, force bool) (int, error) {
	keepDays, err := e.stageHomeKeepDays(ctx)
	if err != nil {
		return 0, err
	}
	cutoff := time.Now().Add(-time.Duration(keepDays) * 24 * time.Hour)
	if force {
		cutoff = time.Now().Add(24 * time.Hour) // everything finished is eligible
	}
	stagesRoot := filepath.Join(e.deps.DataDir, "stages")
	entries, err := os.ReadDir(stagesRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	count := 0
	for _, ent := range entries {
		if !ent.IsDir() {
			continue
		}
		id, err := strconv.ParseInt(ent.Name(), 10, 64)
		if err != nil || id <= 0 {
			continue
		}
		st, err := e.deps.Store.GetStage(ctx, id)
		if err != nil {
			continue
		}
		if st.State == model.StageQueued || st.State == model.StageRunning {
			continue
		}
		// Respect the keep window unless forced.
		if !force && st.Finished > 0 && time.UnixMilli(st.Finished).After(cutoff) {
			continue
		}
		p := filepath.Join(stagesRoot, ent.Name(), "home")
		if _, err := e.validateStageHomePath(p); err != nil {
			continue
		}
		if err := os.RemoveAll(p); err != nil {
			continue
		}
		count++
	}
	return count, nil
}

func (e *Engine) stageHomeKeepDays(ctx context.Context) (int, error) {
	s, err := e.deps.Store.GetSettings(ctx)
	if err != nil {
		return 1, err
	}
	if s.StageHomeKeepDays < 0 {
		return 1, nil
	}
	return s.StageHomeKeepDays, nil
}

// StageHomeKeepDays exposes the setting.
func (e *Engine) StageHomeKeepDays(ctx context.Context) (int, error) {
	return e.stageHomeKeepDays(ctx)
}

// pruneAfterStage deletes the home directory when the keep window is zero.
func (e *Engine) pruneAfterStage(ctx context.Context, stage int64) {
	set, err := e.deps.Store.GetSettings(ctx)
	if err != nil || set.StageHomeKeepDays != 0 {
		return
	}
	st, err := e.deps.Store.GetStage(ctx, stage)
	if err != nil || st.State == model.StageQueued || st.State == model.StageRunning {
		return
	}
	p := e.StageHomeDir(stage)
	if _, err := e.validateStageHomePath(p); err != nil {
		return
	}
	_ = os.RemoveAll(p)
}

// Attention: disk low item. Called by Attention() in attention.go? We'll add separate method.
func (e *Engine) diskLowAttention(ctx context.Context) []AttentionItem {
	if !e.DiskLow(ctx) {
		return nil
	}
	free, _ := e.FreeSpaceBytes()
	msg := fmt.Sprintf("Disk low: %.1f GB free, below the configured threshold", float64(free)/(1024*1024*1024))
	return []AttentionItem{{Kind: "disk_low", Message: msg, Since: e.deps.Clock().UnixMilli()}}
}

var _ = store.Settings{}
var _ = model.Stage{}
var _ = time.Second

// Compatibility aliases for AF-15b tests and callers.
func (e *Engine) PruneStageHomes(ctx context.Context) (int, error) {
	return e.PruneStageHomesNow(ctx)
}

func (e *Engine) PruneStageHomeDir(ctx context.Context, dir string) error {
	id, err := validateStageHomePath(e.deps.DataDir, dir)
	if err != nil {
		return err
	}
	st, err := e.deps.Store.GetStage(ctx, id)
	if err == nil && (st.State == model.StageQueued || st.State == model.StageRunning) {
		return fmt.Errorf("stage %d is %s", id, st.State)
	}
	return os.RemoveAll(dir)
}

func (e *Engine) PruneStageHomePath(ctx context.Context, path string) error {
	return e.PruneStageHomeDir(ctx, path)
}

func (e *Engine) IsDiskLow(ctx context.Context) bool    { return e.DiskLow(ctx) }
func (e *Engine) LowDiskSpace(ctx context.Context) bool { return e.DiskLow(ctx) }
func (e *Engine) FreeDiskSpace() (int64, error)         { return e.FreeSpaceBytes() }

// Package-level helpers used by path-focused tests.
func PruneStageHome(dataDir, stagePath string, isRunning func(int64) bool) error {
	return PruneStageHomePath(dataDir, stagePath, isRunning)
}

func PruneStageHomeDir(dataDir, stagePath string, isRunning func(int64) bool) error {
	return PruneStageHomePath(dataDir, stagePath, isRunning)
}

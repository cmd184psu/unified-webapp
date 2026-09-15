package config

import (
	"path/filepath"
	"testing"
)

func TestDefaultConfigIssueTracker(t *testing.T) {
	c := DefaultConfig()
	if got := c.IssueTracker.StaticDir; got != "./web/issuetracker" {
		t.Errorf("StaticDir = %q, want ./web/issuetracker", got)
	}
	if got := c.IssueTracker.DBPath; got != "./data/issuetracker/issues.db" {
		t.Errorf("DBPath = %q, want ./data/issuetracker/issues.db", got)
	}
	if got := c.IssueTracker.DefaultUser.Name; got != "Unassigned" {
		t.Errorf("DefaultUser.Name = %q, want Unassigned", got)
	}
	if got := c.IssueTracker.DefaultUser.Email; got != "unassigned@localhost" {
		t.Errorf("DefaultUser.Email = %q, want unassigned@localhost", got)
	}
}

func TestExpandIssueTrackerPaths(t *testing.T) {
	cfg := IssueTrackerConfig{StaticDir: "~/web/it", DBPath: "~/data/it.db"}
	if err := expandIssueTrackerPaths(&cfg); err != nil {
		t.Fatalf("expandIssueTrackerPaths: %v", err)
	}
	if filepath.IsAbs(cfg.StaticDir) == false || cfg.StaticDir[0] == '~' {
		t.Errorf("StaticDir not expanded: %q", cfg.StaticDir)
	}
	if cfg.DBPath[0] == '~' {
		t.Errorf("DBPath not expanded: %q", cfg.DBPath)
	}
}

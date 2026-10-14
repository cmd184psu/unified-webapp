package store

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"cmd184psu/unified-webapp/internal/agentflow/runner"
)

var ctx = context.Background()

func mkRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func wantField(t *testing.T, err error, field string) {
	t.Helper()
	var fe *FieldError
	if !errors.Is(err, ErrInvalid) || !errors.As(err, &fe) || fe.Field != field {
		t.Fatalf("err = %v, want ErrInvalid naming %q", err, field)
	}
}

func goodBackend(name string) Backend {
	return Backend{Name: name, Host: "http://localhost:11434", APIType: "ollama", Slots: 2,
		Models: []BackendModel{{Model: "free-model"}, {Model: "pricey-model", Paid: true}}}
}

func countRows(t *testing.T, s *Store, table string) int {
	t.Helper()
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestMigrationVersionsStrictlyIncreasingFromOne(t *testing.T) {
	for i, m := range migrations {
		if m.version != i+1 {
			t.Fatalf("migrations[%d].version = %d, want %d", i, m.version, i+1)
		}
	}
}

func TestFreshDBHasNoBackendsOrProjects(t *testing.T) {
	s, _ := openTemp(t)
	for _, tbl := range []string{"backends", "backend_models", "role_candidates", "projects", "project_limits"} {
		if n := countRows(t, s, tbl); n != 0 {
			t.Errorf("%s has %d rows on a fresh DB", tbl, n)
		}
	}
	snap, err := s.Snapshot(ctx)
	if err != nil || len(snap.Backends) != 0 || len(snap.Candidates) != 0 {
		t.Fatalf("snapshot = %+v, %v", snap, err)
	}
}

func TestSettingsDefaults(t *testing.T) {
	s, _ := openTemp(t)
	got, err := s.GetSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got.MaxAttempts != 5 || got.StuckThreshold != 3 || got.MaxInfraRetries != 5 || got.PlanMaxRounds != 5 ||
		got.OpencodePath != "opencode" || got.VerifySlots != 2 ||
		got.WorkerTimeout <= 0 || got.ReviewTimeout <= 0 || got.PlanTimeout <= 0 || got.VerifyTimeout <= 0 {
		t.Fatalf("defaults = %+v", got)
	}
}

// AC-CF1 (store half): a changed limit survives close and reopen.
func TestSettingsSurviveReopen(t *testing.T) {
	s, path := openTemp(t)
	if err := s.UpdateSettings(ctx, "tester", map[string]string{"max_attempts": "7"}); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	got, err := s2.GetSettings(ctx)
	if err != nil || got.MaxAttempts != 7 {
		t.Fatalf("after reopen MaxAttempts = %d, %v; want 7", got.MaxAttempts, err)
	}
	evs, _ := s2.ListEvents(ctx, "settings", "global", 0)
	if len(evs) != 1 || evs[0].Action != "settings.update" || evs[0].Actor != "tester" {
		t.Fatalf("events = %+v", evs)
	}
}

// SV1: every bound, each naming its field; an invalid write changes nothing.
func TestSettingsValidationBounds(t *testing.T) {
	s, _ := openTemp(t)
	cases := []struct {
		name  string
		patch map[string]string
		field string
	}{
		{"max_attempts zero", map[string]string{"max_attempts": "0"}, "max_attempts"},
		{"max_attempts one (floor is 2)", map[string]string{"max_attempts": "1"}, "max_attempts"},
		{"max_attempts high", map[string]string{"max_attempts": "51"}, "max_attempts"},
		{"max_attempts text", map[string]string{"max_attempts": "lots"}, "max_attempts"},
		{"stuck low", map[string]string{"stuck_threshold": "1"}, "stuck_threshold"},
		{"stuck above max_attempts", map[string]string{"stuck_threshold": "6"}, "stuck_threshold"},
		{"max_attempts below stuck", map[string]string{"max_attempts": "2"}, "stuck_threshold"},
		{"infra negative", map[string]string{"max_infra_retries": "-1"}, "max_infra_retries"},
		{"plan rounds zero", map[string]string{"plan_max_rounds": "0"}, "plan_max_rounds"},
		{"worker timeout short", map[string]string{"worker_timeout": "29s"}, "worker_timeout"},
		{"review timeout long", map[string]string{"review_timeout": "4h1s"}, "review_timeout"},
		{"plan timeout junk", map[string]string{"plan_timeout": "soon"}, "plan_timeout"},
		{"verify timeout zero", map[string]string{"verify_timeout": "0s"}, "verify_timeout"},
		{"opencode blank", map[string]string{"opencode_path": "  "}, "opencode_path"},
		{"verify slots zero", map[string]string{"verify_slots": "0"}, "verify_slots"},
		{"unknown key", map[string]string{"nope": "1"}, "nope"},
		{"empty patch", map[string]string{}, "settings"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			wantField(t, s.UpdateSettings(ctx, "t", c.patch), c.field)
		})
	}
	// Boundaries are inclusive.
	ok := map[string]string{"max_attempts": "50", "stuck_threshold": "2", "worker_timeout": "30s", "verify_timeout": "4h"}
	if err := s.UpdateSettings(ctx, "t", ok); err != nil {
		t.Fatalf("boundary values rejected: %v", err)
	}
	// A rejected write changed nothing, and wrote no event.
	if err := s.UpdateSettings(ctx, "t", map[string]string{"plan_max_rounds": "9", "verify_slots": "0"}); err == nil {
		t.Fatal("expected error")
	}
	got, _ := s.GetSettings(ctx)
	if got.PlanMaxRounds != 5 {
		t.Fatalf("partial write leaked: plan_max_rounds = %d", got.PlanMaxRounds)
	}
	if evs, _ := s.ListEvents(ctx, "settings", "", 0); len(evs) != 1 {
		t.Fatalf("events = %d, want 1 (only the successful write)", len(evs))
	}
}

func TestBackendValidation(t *testing.T) {
	s, _ := openTemp(t)
	cases := []struct {
		name   string
		mutate func(*Backend)
		field  string
	}{
		{"uppercase name", func(b *Backend) { b.Name = "Bad" }, "name"},
		{"empty name", func(b *Backend) { b.Name = "" }, "name"},
		{"leading dash", func(b *Backend) { b.Name = "-x" }, "name"},
		{"23 chars", func(b *Backend) { b.Name = strings.Repeat("a", 23) }, "name"},
		{"reserved verify", func(b *Backend) { b.Name = "verify" }, "name"},
		{"reserved merge", func(b *Backend) { b.Name = "merge" }, "name"},
		{"ftp host", func(b *Backend) { b.Host = "ftp://x:1" }, "host"},
		{"no scheme", func(b *Backend) { b.Host = "localhost:11434" }, "host"},
		{"no host", func(b *Backend) { b.Host = "http://" }, "host"},
		{"url credentials", func(b *Backend) { b.Host = "http://user:pw@localhost:1" }, "host"},
		{"padded model", func(b *Backend) { b.Models = []BackendModel{{Model: " m"}} }, "models"},
		{"bad api", func(b *Backend) { b.APIType = "grpc" }, "api_type"},
		{"zero slots", func(b *Backend) { b.Slots = 0 }, "slots"},
		{"blank model", func(b *Backend) { b.Models = []BackendModel{{Model: " "}} }, "models"},
		{"dup model", func(b *Backend) { b.Models = []BackendModel{{Model: "a"}, {Model: "a"}} }, "models"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b := goodBackend("local")
			c.mutate(&b)
			wantField(t, s.CreateBackend(ctx, "t", b), c.field)
		})
	}
	// BK6 boundary: 22 characters is allowed, so "agentflow-"+name is 32.
	if err := s.CreateBackend(ctx, "t", goodBackend(strings.Repeat("a", 22))); err != nil {
		t.Fatalf("22-char name rejected: %v", err)
	}
	if n := countRows(t, s, "backends"); n != 1 {
		t.Fatalf("rejected creates leaked rows: %d backends", n)
	}
}

func TestBackendLifecycle(t *testing.T) {
	s, _ := openTemp(t)
	if err := s.CreateBackend(ctx, "t", goodBackend("local")); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateBackend(ctx, "t", goodBackend("local")); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate = %v, want ErrConflict", err)
	}
	b, err := s.GetBackend(ctx, "local")
	if err != nil || len(b.Models) != 2 || b.Models[1].Model != "pricey-model" || !b.Models[1].Paid {
		t.Fatalf("get = %+v, %v", b, err)
	}
	b.Slots = 4
	b.Host = "https://example.test"
	b.Models = b.Models[:1]
	if err := s.UpdateBackend(ctx, "t", b); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetBackend(ctx, "local")
	if got.Slots != 4 || got.Host != "https://example.test" || len(got.Models) != 1 {
		t.Fatalf("after update = %+v", got)
	}
	if err := s.UpdateBackend(ctx, "t", goodBackend("ghost")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("update unknown = %v", err)
	}
	if _, err := s.GetBackend(ctx, "ghost"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get unknown = %v", err)
	}
	if err := s.RetireBackend(ctx, "t", "local"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetBackend(ctx, "local"); !got.Retired {
		t.Fatal("not retired")
	}
	if err := s.RetireBackend(ctx, "t", "ghost"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("retire unknown = %v", err)
	}
	evs, _ := s.ListEvents(ctx, "backend", "local", 0)
	if len(evs) != 3 {
		t.Fatalf("backend events = %d, want create+update+retire", len(evs))
	}
}

// MB5: a paid model is a candidate only when listed explicitly; MB4 ordering.
func TestPaidModelNotCandidateUnlessListed(t *testing.T) {
	s, _ := openTemp(t)
	if err := s.CreateBackend(ctx, "t", goodBackend("local")); err != nil {
		t.Fatal(err)
	}
	if err := s.SetRoleCandidates(ctx, "t", "global", "", "worker", []CandidateRef{{"local", "free-model"}}); err != nil {
		t.Fatal(err)
	}
	snap, err := s.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got := snap.CandidatesFor("worker", "")
	if len(got) != 1 || got[0].Model != "free-model" || got[0].Paid {
		t.Fatalf("candidates = %+v", got)
	}
	for _, c := range snap.Candidates {
		if c.Model == "pricey-model" {
			t.Fatal("paid model became a candidate without being listed")
		}
	}
	if got := snap.CandidatesFor("reviewer", ""); len(got) != 0 {
		t.Fatalf("unlisted role has candidates: %+v", got)
	}
	// Listing it explicitly makes it a candidate, flagged paid, in order.
	if err := s.SetRoleCandidates(ctx, "t", "global", "", "worker",
		[]CandidateRef{{"local", "pricey-model"}, {"local", "free-model"}}); err != nil {
		t.Fatal(err)
	}
	snap, _ = s.Snapshot(ctx)
	got = snap.CandidatesFor("worker", "")
	if len(got) != 2 || got[0].Model != "pricey-model" || !got[0].Paid || got[1].Paid {
		t.Fatalf("explicit list = %+v", got)
	}
}

func TestRoleCandidatesValidationAndProjectOverride(t *testing.T) {
	s, _ := openTemp(t)
	s.CreateBackend(ctx, "t", goodBackend("local"))
	s.CreateBackend(ctx, "t", goodBackend("other"))
	repo := mkRepo(t)
	if err := s.CreateProject(ctx, "t", Project{Slug: "p", RepoPath: repo, ModelScope: "local"}); err != nil {
		t.Fatal(err)
	}
	wantField(t, s.SetRoleCandidates(ctx, "t", "global", "", "worker", nil), "candidates")
	wantField(t, s.SetRoleCandidates(ctx, "t", "global", "", "janitor", []CandidateRef{{"local", "free-model"}}), "role")
	wantField(t, s.SetRoleCandidates(ctx, "t", "global", "", "worker", []CandidateRef{{"local", "nope"}}), "candidates[0]")
	wantField(t, s.SetRoleCandidates(ctx, "t", "global", "x", "worker", []CandidateRef{{"local", "free-model"}}), "project_id")
	wantField(t, s.SetRoleCandidates(ctx, "t", "team", "", "worker", []CandidateRef{{"local", "free-model"}}), "scope")
	if err := s.SetRoleCandidates(ctx, "t", "project", "ghost", "worker", []CandidateRef{{"local", "free-model"}}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown project = %v", err)
	}
	s.SetRoleCandidates(ctx, "t", "global", "", "worker", []CandidateRef{{"local", "free-model"}})
	s.SetRoleCandidates(ctx, "t", "project", "p", "worker", []CandidateRef{{"other", "free-model"}})
	snap, _ := s.Snapshot(ctx)
	if got := snap.CandidatesFor("worker", "p"); len(got) != 1 || got[0].Backend != "other" {
		t.Fatalf("project list = %+v", got)
	}
	if got := snap.CandidatesFor("worker", "q"); len(got) != 1 || got[0].Backend != "local" {
		t.Fatalf("other project falls back to global: %+v", got)
	}
	// A model in use cannot be dropped from its backend.
	b, _ := s.GetBackend(ctx, "other")
	b.Models = []BackendModel{{Model: "pricey-model"}}
	if err := s.UpdateBackend(ctx, "t", b); !errors.Is(err, ErrConflict) {
		t.Fatalf("dropping used model = %v, want ErrConflict", err)
	}
	if b2, _ := s.GetBackend(ctx, "other"); len(b2.Models) != 2 {
		t.Fatal("conflicting update was not rolled back")
	}
	// Retired backend entries drop out of the effective list.
	s.RetireBackend(ctx, "t", "other")
	snap, _ = s.Snapshot(ctx)
	if got := snap.CandidatesFor("worker", "p"); len(got) != 0 {
		t.Fatalf("all-retired project list must not fall back: %+v", got)
	}
	wantField(t, s.SetRoleCandidates(ctx, "t", "global", "", "worker", []CandidateRef{{"other", "free-model"}}), "candidates[0]")
	if err := s.ClearProjectRoleCandidates(ctx, "t", "p", "worker"); err != nil {
		t.Fatal(err)
	}
}

func TestProjectValidationAndUniqueness(t *testing.T) {
	s, _ := openTemp(t)
	repo := mkRepo(t)
	notRepo := t.TempDir()
	cases := []struct {
		name  string
		p     Project
		field string
	}{
		{"bad slug", Project{Slug: "Bad Slug", RepoPath: repo, ModelScope: "any"}, "slug"},
		{"bad scope", Project{Slug: "p", RepoPath: repo, ModelScope: "cloud"}, "model_scope"},
		{"relative path", Project{Slug: "p", RepoPath: "repo", ModelScope: "any"}, "repo_path"},
		{"missing path", Project{Slug: "p", RepoPath: filepath.Join(repo, "nope"), ModelScope: "any"}, "repo_path"},
		{"not a git repo", Project{Slug: "p", RepoPath: notRepo, ModelScope: "any"}, "repo_path"},
		{"blank feedback", Project{Slug: "p", RepoPath: repo, ModelScope: "any", FeedbackCmds: []string{"go vet", " "}}, "feedback_cmds[1]"},
		{"unknown limit", Project{Slug: "p", RepoPath: repo, ModelScope: "any", Limits: map[string]string{"verify_slots": "3"}}, "limits.verify_slots"},
		{"limit out of range", Project{Slug: "p", RepoPath: repo, ModelScope: "any", Limits: map[string]string{"max_attempts": "99"}}, "limits.max_attempts"},
		{"limit cross rule", Project{Slug: "p", RepoPath: repo, ModelScope: "any", Limits: map[string]string{"max_attempts": "2"}}, "limits.stuck_threshold"},
		{"limit bad timeout", Project{Slug: "p", RepoPath: repo, ModelScope: "any", Limits: map[string]string{"worker_timeout": "5s"}}, "limits.worker_timeout"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { wantField(t, s.CreateProject(ctx, "t", c.p), c.field) })
	}
	if n := countRows(t, s, "projects"); n != 0 {
		t.Fatalf("rejected creates leaked %d rows", n)
	}
	// PJ4: one repo_path per project, also through a symlink alias.
	if err := s.CreateProject(ctx, "t", Project{Slug: "a", RepoPath: repo, ModelScope: "local", FeedbackCmds: []string{"go vet ./..."}}); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateProject(ctx, "t", Project{Slug: "b", RepoPath: repo, ModelScope: "local"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate repo_path = %v, want ErrConflict", err)
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(repo, alias); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateProject(ctx, "t", Project{Slug: "b", RepoPath: alias, ModelScope: "local"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("aliased repo_path = %v, want ErrConflict", err)
	}
	if err := s.CreateProject(ctx, "t", Project{Slug: "a", RepoPath: mkRepo(t), ModelScope: "local"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate slug = %v, want ErrConflict", err)
	}
	// A second repo is fine; moving it onto the first one is not.
	repo2 := mkRepo(t)
	if err := s.CreateProject(ctx, "t", Project{Slug: "b", RepoPath: repo2, ModelScope: "any"}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateProject(ctx, "t", Project{Slug: "b", RepoPath: repo, ModelScope: "any"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("update onto taken repo_path = %v", err)
	}
	if err := s.UpdateProject(ctx, "t", Project{Slug: "ghost", RepoPath: repo2, ModelScope: "any"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("update unknown = %v", err)
	}
	// Updating a project keeping its own repo_path is not a conflict.
	if err := s.UpdateProject(ctx, "t", Project{Slug: "a", RepoPath: repo, ModelScope: "any", FeedbackCmds: []string{"make test"}}); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetProject(ctx, "a")
	if err != nil || got.ModelScope != "any" || len(got.FeedbackCmds) != 1 || got.FeedbackCmds[0] != "make test" {
		t.Fatalf("get = %+v, %v", got, err)
	}
	if _, err := s.GetProject(ctx, "ghost"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get unknown = %v", err)
	}
	if ps, _ := s.ListProjects(ctx); len(ps) != 2 || ps[0].Slug != "a" {
		t.Fatalf("list = %+v", ps)
	}
}

// AC-WC2 / WC3: the working directory, anything inside it, anything that
// contains it (the repo root in particular) and data_dir are refused, also
// via a symlink alias.
func TestProjectRejectsCwdAndDataDir(t *testing.T) {
	cwd := mkRepo(t) // a git repo, so only the overlap rule can reject it
	t.Chdir(cwd)
	inside := filepath.Join(cwd, "sub")
	if err := os.MkdirAll(filepath.Join(inside, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	parent := filepath.Dir(cwd)
	if err := os.Mkdir(filepath.Join(parent, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(cwd, alias); err != nil {
		t.Fatal(err)
	}
	insideAlias := filepath.Join(alias, "sub")

	s, _ := openTemp(t)
	dataDir := s.dataDir
	if err := os.Mkdir(filepath.Join(dataDir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	dataInside := filepath.Join(dataDir, "nested")
	if err := os.MkdirAll(filepath.Join(dataInside, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	dataAlias := filepath.Join(t.TempDir(), "dataalias")
	if err := os.Symlink(dataDir, dataAlias); err != nil {
		t.Fatal(err)
	}
	wantLabel := map[string]string{
		"cwd": "working directory", "inside cwd": "working directory", "contains cwd": "working directory",
		"symlink to cwd": "working directory", "inside via symlink": "working directory",
		"data_dir": "data directory", "inside data_dir": "data directory", "symlink to data_dir": "data directory",
	}
	for name, path := range map[string]string{
		"cwd": cwd, "inside cwd": inside, "contains cwd": parent, "symlink to cwd": alias,
		"inside via symlink": insideAlias, "data_dir": dataDir, "inside data_dir": dataInside,
		"symlink to data_dir": dataAlias,
	} {
		t.Run(name, func(t *testing.T) {
			err := s.CreateProject(ctx, "t", Project{Slug: "p", RepoPath: path, ModelScope: "local"})
			wantField(t, err, "repo_path")
			var fe *FieldError
			if !errors.As(err, &fe) || !strings.Contains(fe.Msg, wantLabel[name]) {
				t.Fatalf("rejected for the wrong reason (want %q): %v", wantLabel[name], err)
			}
		})
	}
	if n := countRows(t, s, "projects"); n != 0 {
		t.Fatalf("%d projects registered", n)
	}
}

// AC-WC2 against the real server layout: the module root (which contains the
// test's real working directory) and the working directory itself.
func TestProjectRejectsRealRepoRoot(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := cwd
	for {
		if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
			break
		}
		next := filepath.Dir(root)
		if next == root {
			t.Fatal("go.mod not found above " + cwd)
		}
		root = next
	}
	s, _ := openTemp(t)
	for name, p := range map[string]string{"module root": root, "cwd": cwd, "internal dir": filepath.Join(root, "internal")} {
		t.Run(name, func(t *testing.T) {
			wantField(t, s.CreateProject(ctx, "t", Project{Slug: "p", RepoPath: p, ModelScope: "local"}), "repo_path")
		})
	}
}

// CF2: blank override falls back to global; set overrides apply.
func TestProjectOverridesFallBackToGlobal(t *testing.T) {
	s, _ := openTemp(t)
	repo := mkRepo(t)
	err := s.CreateProject(ctx, "t", Project{Slug: "p", RepoPath: repo, ModelScope: "local",
		Limits: map[string]string{"max_attempts": "9", "worker_timeout": "45m", "plan_max_rounds": ""}})
	if err != nil {
		t.Fatal(err)
	}
	eff, err := s.EffectiveSettings(ctx, "p")
	if err != nil {
		t.Fatal(err)
	}
	if eff.MaxAttempts != 9 || eff.WorkerTimeout != 45*time.Minute {
		t.Fatalf("overrides not applied: %+v", eff)
	}
	if eff.PlanMaxRounds != 5 || eff.StuckThreshold != 3 {
		t.Fatalf("blank/absent did not fall back: %+v", eff)
	}
	if p, _ := s.GetProject(ctx, "p"); len(p.Limits) != 2 {
		t.Fatalf("blank override stored: %+v", p.Limits)
	}
	// A global change flows through to keys the project does not override.
	if err := s.UpdateSettings(ctx, "t", map[string]string{"plan_max_rounds": "8"}); err != nil {
		t.Fatal(err)
	}
	eff, _ = s.EffectiveSettings(ctx, "p")
	if eff.PlanMaxRounds != 8 || eff.MaxAttempts != 9 {
		t.Fatalf("after global change: %+v", eff)
	}
	snap, _ := s.Snapshot(ctx)
	if !reflect.DeepEqual(snap.Effective["p"], eff) || snap.Settings.MaxAttempts != 5 {
		t.Fatalf("snapshot effective = %+v", snap.Effective["p"])
	}
	// Clearing an override (blank) restores the global value.
	err = s.UpdateProject(ctx, "t", Project{Slug: "p", RepoPath: repo, ModelScope: "local", Limits: map[string]string{"max_attempts": ""}})
	if err != nil {
		t.Fatal(err)
	}
	if eff, _ = s.EffectiveSettings(ctx, "p"); eff.MaxAttempts != 5 {
		t.Fatalf("cleared override: %+v", eff)
	}
	if _, err := s.EffectiveSettings(ctx, "ghost"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown project = %v", err)
	}
}

// A global change that would invalidate a project's overrides is refused and
// leaves the setting unchanged.
func TestGlobalUpdateChecksProjectOverrides(t *testing.T) {
	s, _ := openTemp(t)
	err := s.CreateProject(ctx, "t", Project{Slug: "p", RepoPath: mkRepo(t), ModelScope: "local",
		Limits: map[string]string{"stuck_threshold": "4"}})
	if err != nil {
		t.Fatal(err)
	}
	// Globally valid (stuck 3 <= 3) but project p would have stuck 4 > 3.
	err = s.UpdateSettings(ctx, "t", map[string]string{"max_attempts": "3"})
	wantField(t, err, "projects.p.stuck_threshold")
	if got, _ := s.GetSettings(ctx); got.MaxAttempts != 5 {
		t.Fatalf("max_attempts = %d after refused update", got.MaxAttempts)
	}
}

// SV2: writes and their audit events commit together; the snapshot is one
// read transaction and does not see a later write.
func TestSnapshotIsImmutableAndConsistent(t *testing.T) {
	s, _ := openTemp(t)
	s.CreateBackend(ctx, "t", goodBackend("local"))
	snap, err := s.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	s.CreateBackend(ctx, "t", goodBackend("later"))
	s.UpdateSettings(ctx, "t", map[string]string{"max_attempts": "9"})
	if len(snap.Backends) != 1 || snap.Settings.MaxAttempts != 5 {
		t.Fatalf("snapshot changed after the fact: %+v", snap)
	}
}

func TestConfigWritesAppendAuditEventsInSameTx(t *testing.T) {
	s, _ := openTemp(t)
	repo := mkRepo(t)
	s.CreateBackend(ctx, "alice", goodBackend("local"))
	s.CreateProject(ctx, "alice", Project{Slug: "p", RepoPath: repo, ModelScope: "any"})
	s.SetRoleCandidates(ctx, "alice", "global", "", "worker", []CandidateRef{{"local", "free-model"}})
	evs, err := s.ListEvents(ctx, "", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	var actions []string
	for _, e := range evs {
		if e.Actor != "alice" {
			t.Fatalf("actor = %q", e.Actor)
		}
		actions = append(actions, e.Action)
	}
	if got := strings.Join(actions, ","); got != "backend.create,project.create,candidates.set" {
		t.Fatalf("actions = %s", got)
	}
	// A failed write leaves no event behind.
	before := countRows(t, s, "events")
	s.CreateProject(ctx, "alice", Project{Slug: "q", RepoPath: repo, ModelScope: "any"})
	if countRows(t, s, "events") != before {
		t.Fatal("failed write appended an event")
	}
}

func TestCandidateDuplicateRejected(t *testing.T) {
	s, _ := openTemp(t)
	s.CreateBackend(ctx, "t", goodBackend("local"))
	err := s.SetRoleCandidates(ctx, "t", "global", "", "worker",
		[]CandidateRef{{"local", "free-model"}, {"local", "free-model"}})
	wantField(t, err, "candidates[1]")
}

// An all-retired project list yields nothing; it does not fall back to global.
func TestAllRetiredProjectListDoesNotFallBack(t *testing.T) {
	s, _ := openTemp(t)
	s.CreateBackend(ctx, "t", goodBackend("local"))
	s.CreateBackend(ctx, "t", goodBackend("other"))
	if err := s.CreateProject(ctx, "t", Project{Slug: "p", RepoPath: mkRepo(t), ModelScope: "local"}); err != nil {
		t.Fatal(err)
	}
	s.SetRoleCandidates(ctx, "t", "global", "", "worker", []CandidateRef{{"local", "free-model"}})
	s.SetRoleCandidates(ctx, "t", "project", "p", "worker", []CandidateRef{{"other", "free-model"}})
	s.RetireBackend(ctx, "t", "other")
	snap, _ := s.Snapshot(ctx)
	if got := snap.CandidatesFor("worker", "p"); len(got) != 0 {
		t.Fatalf("fell back to global: %+v", got)
	}
	if got := snap.CandidatesFor("worker", ""); len(got) != 1 {
		t.Fatalf("global = %+v", got)
	}
}

// The migration's CHECK and FK constraints reject invalid rows written with
// raw SQL, bypassing the Go validators.
func TestMigrationConstraintsRejectInvalidRows(t *testing.T) {
	s, _ := openTemp(t)
	s.CreateBackend(ctx, "t", goodBackend("local"))
	s.CreateProject(ctx, "t", Project{Slug: "p", RepoPath: mkRepo(t), ModelScope: "local"})
	bad := map[string]string{
		"api_type":      `INSERT INTO backends (name, host, api_type, slots, created, updated) VALUES ('x','http://h','grpc',1,0,0)`,
		"slots":         `INSERT INTO backends (name, host, api_type, slots, created, updated) VALUES ('x','http://h','ollama',0,0,0)`,
		"hosted":        `INSERT INTO backends (name, host, api_type, hosted, slots, created, updated) VALUES ('x','http://h','ollama',2,1,0,0)`,
		"model_scope":   `INSERT INTO projects (slug, repo_path, model_scope, created, updated) VALUES ('x','/x','cloud',0,0)`,
		"scope":         `INSERT INTO role_candidates (scope, role, position, backend, model) VALUES ('team','worker',0,'local','free-model')`,
		"global+proj":   `INSERT INTO role_candidates (scope, project_id, role, position, backend, model) VALUES ('global','p','worker',0,'local','free-model')`,
		"project-none":  `INSERT INTO role_candidates (scope, role, position, backend, model) VALUES ('project','worker',0,'local','free-model')`,
		"role":          `INSERT INTO role_candidates (scope, role, position, backend, model) VALUES ('global','janitor',0,'local','free-model')`,
		"position":      `INSERT INTO role_candidates (scope, role, position, backend, model) VALUES ('global','worker',-1,'local','free-model')`,
		"unknown model": `INSERT INTO role_candidates (scope, role, position, backend, model) VALUES ('global','worker',0,'local','ghost')`,
	}
	for name, q := range bad {
		t.Run(name, func(t *testing.T) {
			if _, err := s.db.Exec(q); err == nil {
				t.Fatal("invalid row accepted")
			}
		})
	}
	if _, err := s.db.Exec(`INSERT INTO role_candidates (scope, role, position, backend, model) VALUES ('global','worker',0,'local','free-model')`); err != nil {
		t.Fatalf("valid row rejected: %v", err)
	}
}

func TestUpgradeV1DatabaseToV2(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agentflow.db")
	s1, err := openWith(path, migrations[:1])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := AppendEventTx(t, s1); err != nil {
		t.Fatal(err)
	}
	if v := schemaVersion(t, s1.db); v != 1 {
		t.Fatalf("v1 db at version %d", v)
	}
	s1.Close()
	s2, err := Open(path)
	if err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	defer s2.Close()
	if v := schemaVersion(t, s2.db); v != migrations[len(migrations)-1].version {
		t.Fatalf("version = %d", v)
	}
	if evs, _ := s2.ListEvents(ctx, "", "", 0); len(evs) != 1 {
		t.Fatalf("v1 events lost: %d", len(evs))
	}
	if got, err := s2.GetSettings(ctx); err != nil || got.MaxAttempts != 5 {
		t.Fatalf("v2 defaults after upgrade = %+v, %v", got, err)
	}
}

func AppendEventTx(t *testing.T, s *Store) (int64, error) {
	t.Helper()
	var id int64
	err := s.WithTx(ctx, func(tx *sql.Tx) (err error) {
		id, err = AppendEvent(tx, "t", "seed", "thing", "1", nil)
		return err
	})
	return id, err
}

func TestRecheckRepoPath(t *testing.T) {
	s, _ := openTemp(t)
	repo := mkRepo(t)
	if err := s.CreateProject(ctx, "t", Project{Slug: "p", RepoPath: repo, ModelScope: "local"}); err != nil {
		t.Fatal(err)
	}
	if err := s.RecheckRepoPath(ctx, "p"); err != nil {
		t.Fatalf("healthy recheck: %v", err)
	}
	if err := s.RecheckRepoPath(ctx, "ghost"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown = %v", err)
	}
	// Replace the repo directory with a symlink to another repo.
	other := mkRepo(t)
	if err := os.RemoveAll(repo); err != nil {
		t.Fatal(err)
	}
	err := s.RecheckRepoPath(ctx, "p")
	wantField(t, err, "repo_path") // gone
	if err := os.Symlink(other, repo); err != nil {
		t.Fatal(err)
	}
	err = s.RecheckRepoPath(ctx, "p")
	wantField(t, err, "repo_path")
	var fe *FieldError
	if errors.As(err, &fe) && !strings.Contains(fe.Msg, "symlink") {
		t.Fatalf("msg = %q", fe.Msg)
	}
	if _, err := s.ValidateRepoPath(other); err != nil {
		t.Fatalf("ValidateRepoPath: %v", err)
	}
}

func TestRepoPathMissingVersusUnreadable(t *testing.T) {
	s, _ := openTemp(t)
	base := t.TempDir()
	file := filepath.Join(base, "file")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	// A path below a regular file fails with ENOTDIR, not "does not exist".
	_, errMissing := s.ValidateRepoPath(filepath.Join(base, "nope"))
	_, errBlocked := s.ValidateRepoPath(filepath.Join(file, "repo"))
	var m, l *FieldError
	if !errors.As(errMissing, &m) || !errors.As(errBlocked, &l) {
		t.Fatalf("errors: %v / %v", errMissing, errBlocked)
	}
	if m.Msg != "does not exist" || !strings.Contains(l.Msg, "cannot be accessed") {
		t.Fatalf("missing = %q, blocked = %q", m.Msg, l.Msg)
	}
}

func TestPX5_MaxConcurrentRoundTripsAndRejectsNegative(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	b := goodBackend("local")
	b.Models = []BackendModel{{Model: "x", MaxConcurrent: 1}, {Model: "y"}}
	if err := s.CreateBackend(ctx, "t", b); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetBackend(ctx, "local")
	if err != nil {
		t.Fatal(err)
	}
	if got.Models[0].MaxConcurrent != 1 || got.Models[1].MaxConcurrent != 0 {
		t.Fatalf("models = %+v, want x limited to 1 and y unlimited", got.Models)
	}
	b.Models[1].MaxConcurrent = 3
	if err := s.UpdateBackend(ctx, "t", b); err != nil {
		t.Fatal(err)
	}
	if got, _ = s.GetBackend(ctx, "local"); got.Models[1].MaxConcurrent != 3 {
		t.Fatalf("update lost max_concurrent: %+v", got.Models)
	}
	b.Models[0].MaxConcurrent = -1
	wantField(t, s.UpdateBackend(ctx, "t", b), "models")
}

// F2: the stages rebuild keeps every row and the rows that point at them, and
// afterwards a stage can have the cleanup role.
func TestMigration6RebuildsStagesKeepingRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agentflow.db")
	s1, err := openWith(path, migrations[:5])
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO projects (slug, repo_path, model_scope, created, updated) VALUES ('p', '/r', 'local', 1, 1)`,
		`INSERT INTO efforts (project, title, status, created, updated) VALUES ('p', 'e', 'running', 1, 1)`,
		`INSERT INTO queues (effort, revision, hash, status) VALUES (1, 1, 'h', 'approved')`,
		`INSERT INTO tasks (queue, task_key, title, prompt, acceptance_json, files_touched_json) VALUES (1, 'T1', 't', 'p', '[]', '[]')`,
		`INSERT INTO attempts (task, n, kind, started) VALUES (1, 1, 'normal', 1)`,
		`INSERT INTO stages (attempt, role, backend, model, state, outcome, raw_output) VALUES (1, 'worker', 'b', 'm', 'done', 'pass', 'raw text')`,
		`INSERT INTO infra_retries (task, stage, reason, at) VALUES (1, 1, 'r', 1)`,
	} {
		if _, err := s1.db.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	s1.Close()
	s2, err := Open(path)
	if err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	defer s2.Close()
	var raw string
	var stage sql.NullInt64
	if err := s2.db.QueryRow(`SELECT raw_output FROM stages WHERE id = 1`).Scan(&raw); err != nil || raw != "raw text" {
		t.Fatalf("stage row lost: %q %v", raw, err)
	}
	if err := s2.db.QueryRow(`SELECT stage FROM infra_retries`).Scan(&stage); err != nil || !stage.Valid || stage.Int64 != 1 {
		t.Fatalf("infra_retries.stage = %+v (%v), want 1", stage, err)
	}
	var fk int
	if err := s2.db.QueryRow(`PRAGMA foreign_keys`).Scan(&fk); err != nil || fk != 1 {
		t.Fatalf("foreign_keys = %d (%v), want on again", fk, err)
	}
	if _, err := s2.CreateStage(ctx, StageSpec{Attempt: 1, Role: "cleanup", Backend: "b", Model: "m"}); err != nil {
		t.Fatalf("cleanup stage rejected: %v", err)
	}
	var autoinc int
	if err := s2.db.QueryRow(`SELECT id FROM stages WHERE role = 'cleanup'`).Scan(&autoinc); err != nil || autoinc != 2 {
		t.Fatalf("new stage id = %d (%v), want 2 (sequence kept)", autoinc, err)
	}
}

// F2: cleanup_enabled round-trips through the store.
func TestAT11_ProjectCleanupFlagRoundTrips(t *testing.T) {
	s, _ := openTemp(t)
	p := Project{Slug: "p", RepoPath: mkRepo(t), ModelScope: "local", CleanupEnabled: true}
	if err := s.CreateProject(ctx, "t", p); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetProject(ctx, "p")
	if err != nil || !got.CleanupEnabled {
		t.Fatalf("created project = %+v (%v)", got, err)
	}
	got.CleanupEnabled = false
	if err := s.UpdateProject(ctx, "t", got); err != nil {
		t.Fatal(err)
	}
	if got, _ = s.GetProject(ctx, "p"); got.CleanupEnabled {
		t.Fatal("flag not cleared")
	}
}

// F3: EffortUsage sums tokens and stage seconds over the effort's own stages
// and those of its attempts; a running stage counts up to now.
func TestAT12_EffortUsageSumsTokensAndSeconds(t *testing.T) {
	s, _ := openTemp(t)
	if err := s.CreateProject(ctx, "t", Project{Slug: "p", RepoPath: mkRepo(t), ModelScope: "local"}); err != nil {
		t.Fatal(err)
	}
	ef, err := s.CreateEffort(ctx, "t", "p", "e", false)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO queues (effort, revision, hash, status) VALUES (1, 1, 'h', 'approved')`,
		`INSERT INTO tasks (queue, task_key, title, prompt, acceptance_json, files_touched_json) VALUES (1, 'T1', 't', 'p', '[]', '[]')`,
		`INSERT INTO attempts (task, n, kind, started) VALUES (1, 1, 'normal', 1)`,
		`INSERT INTO stages (attempt, role, state, started, finished, tokens) VALUES (1, 'worker', 'done', 1000, 11000, 300)`,
		`INSERT INTO stages (effort, role, state, started, finished, tokens) VALUES (1, 'planner', 'done', 20000, 25000, 200)`,
		`INSERT INTO stages (attempt, role, state, started) VALUES (1, 'reviewer', 'running', 30000)`,
		`INSERT INTO stages (attempt, role, state) VALUES (1, 'verify', 'queued')`,
	} {
		if _, err := s.db.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	tokens, seconds, err := s.EffortUsage(ctx, ef.ID, 34000)
	if err != nil || tokens != 500 || seconds != 19 { // 10 + 5 + 4 running
		t.Fatalf("usage = %d tokens, %d s (%v), want 500 and 19", tokens, seconds, err)
	}
	if err := s.RecordStageTokens(ctx, 1, 400); err != nil {
		t.Fatal(err)
	}
	if tokens, _, _ = s.EffortUsage(ctx, ef.ID, 34000); tokens != 600 {
		t.Fatalf("tokens after rewrite = %d, want 600", tokens)
	}
	if err := s.RecordStageTokens(ctx, 99, 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown stage = %v", err)
	}
	neg := int64(-1)
	if _, err := s.UpdateEffort(ctx, "t", ef.ID, EffortPatch{BudgetSeconds: &neg}); err == nil {
		t.Fatal("negative budget accepted")
	}
}

// AC-OA3: a project's plugins must be exact versions; the rest are refused
// with the entry named, and a project with none stores none.
func TestOA3_ProjectPluginsArePinnedAndPerProject(t *testing.T) {
	s, _ := openTemp(t)
	good := []runner.Plugin{{Name: "@acme/notify", Version: "1.2.3"}, {Name: "beta", Version: "0.1.0-rc.1"}}
	if err := s.CreateProject(ctx, "t", Project{Slug: "a", RepoPath: mkRepo(t), ModelScope: "local", Plugins: good}); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateProject(ctx, "t", Project{Slug: "b", RepoPath: mkRepo(t), ModelScope: "local"}); err != nil {
		t.Fatal(err)
	}
	a, _ := s.GetProject(ctx, "a")
	b, _ := s.GetProject(ctx, "b")
	if len(a.Plugins) != 2 || a.Plugins[0].Name != "@acme/notify" || len(b.Plugins) != 0 {
		t.Fatalf("a = %v, b = %v", a.Plugins, b.Plugins)
	}
	bad := []struct {
		name string
		p    []runner.Plugin
		want string
	}{
		{"latest", []runner.Plugin{{Name: "x", Version: "latest"}}, "plugins[0].version"},
		{"empty", []runner.Plugin{{Name: "x", Version: ""}}, "plugins[0].version"},
		{"caret", []runner.Plugin{{Name: "ok", Version: "1.0.0"}, {Name: "x", Version: "^1.2.3"}}, "plugins[1].version"},
		{"tilde", []runner.Plugin{{Name: "x", Version: "~1.2.3"}}, "plugins[0].version"},
		{"partial", []runner.Plugin{{Name: "x", Version: "1.2"}}, "plugins[0].version"},
		{"@latest in name", []runner.Plugin{{Name: "x@latest", Version: "1.0.0"}}, "plugins[0].name"},
		{"duplicate", []runner.Plugin{{Name: "x", Version: "1.0.0"}, {Name: "x", Version: "1.0.1"}}, "plugins[1].name"},
	}
	for _, c := range bad {
		a.Plugins = c.p
		wantField(t, s.UpdateProject(ctx, "t", a), c.want)
	}
	a, _ = s.GetProject(ctx, "a")
	if len(a.Plugins) != 2 {
		t.Fatalf("a refused update changed the plugins: %v", a.Plugins)
	}
	a.Plugins = nil
	if err := s.UpdateProject(ctx, "t", a); err != nil {
		t.Fatal(err)
	}
	if a, _ = s.GetProject(ctx, "a"); len(a.Plugins) != 0 {
		t.Fatalf("opt-out kept plugins: %v", a.Plugins)
	}
	if n := countRows(t, s, "project_plugins"); n != 0 {
		t.Fatalf("project_plugins rows = %d", n)
	}
}

func TestBrowseRootsDefaultAndValidation(t *testing.T) {
	s, _ := openTemp(t)
	defer s.Close()
	home, _ := os.UserHomeDir()
	got, err := s.GetSettings(ctx)
	if err != nil || len(got.BrowseRoots) != 1 || got.BrowseRoots[0] != home {
		t.Fatalf("default roots = %v (%v), want the home directory", got.BrowseRoots, err)
	}
	if err := s.UpdateSettings(ctx, "t", map[string]string{"browse_roots": `["/srv/a","/srv/b/"]`}); err != nil {
		t.Fatal(err)
	}
	if got, _ = s.GetSettings(ctx); len(got.BrowseRoots) != 2 || got.BrowseRoots[1] != "/srv/b" {
		t.Fatalf("roots = %v", got.BrowseRoots)
	}
	for _, bad := range []string{`["rel"]`, `nonsense`, `[1]`} {
		var fe *FieldError
		if err := s.UpdateSettings(ctx, "t", map[string]string{"browse_roots": bad}); !errors.As(err, &fe) || fe.Field != "browse_roots" {
			t.Errorf("browse_roots %s = %v, want a field error", bad, err)
		}
	}
}

func TestBackendKeyStoredApartAndNeverInEvents(t *testing.T) {
	s, _ := openTemp(t)
	defer s.Close()
	b := Backend{Name: "hosted", Host: "http://127.0.0.1:1", APIType: "openai_compatible", Hosted: true, Slots: 1,
		Models: []BackendModel{{Model: "m"}}}
	if err := s.CreateBackend(ctx, "t", b); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetBackend(ctx, "hosted"); got.HasKey {
		t.Fatal("new backend claims a key")
	}
	if err := s.SetBackendKey(ctx, "t", "hosted", "sk-very-secret"); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetBackend(ctx, "hosted")
	if !got.HasKey {
		t.Fatal("HasKey is false after SetBackendKey")
	}
	// An ordinary backend update leaves the key alone.
	b.Slots = 2
	if err := s.UpdateBackend(ctx, "t", b); err != nil {
		t.Fatal(err)
	}
	if k, _ := s.BackendKey(ctx, "hosted"); k != "sk-very-secret" {
		t.Fatalf("key after UpdateBackend = %q", k)
	}
	evs, _ := s.ListEvents(ctx, "", "", 0)
	for _, ev := range evs {
		if strings.Contains(ev.DetailJSON, "sk-very-secret") {
			t.Fatalf("event %s holds the key", ev.Action)
		}
	}
	if err := s.SetBackendKey(ctx, "t", "hosted", ""); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetBackend(ctx, "hosted"); got.HasKey {
		t.Fatal("HasKey stays true after clearing")
	}
	if err := s.SetBackendKey(ctx, "t", "nope", "k"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown backend = %v", err)
	}
}

package store

import (
	"path/filepath"
	"testing"
)

// TestProjectToolchainSettingsRoundTrip: toolchains, feedback_env and
// verify_in_copy are stored, read back and updated (AF-6).
func TestProjectToolchainSettingsRoundTrip(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "agentflow.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	repo := mkRepo(t)
	p := Project{Slug: "cs", RepoPath: repo, ModelScope: "local",
		Toolchains: []string{"dotnet"}, FeedbackEnv: map[string]string{"CIV3_PATH": "/civ3"}, VerifyInCopy: true}
	if err := s.CreateProject(ctx, "t", p); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetProject(ctx, "cs")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Toolchains) != 1 || got.Toolchains[0] != "dotnet" || got.FeedbackEnv["CIV3_PATH"] != "/civ3" || !got.VerifyInCopy {
		t.Fatalf("after create: %+v", got)
	}
	got.Toolchains, got.FeedbackEnv, got.VerifyInCopy = nil, nil, false
	if err := s.UpdateProject(ctx, "t", got); err != nil {
		t.Fatal(err)
	}
	got, err = s.GetProject(ctx, "cs")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Toolchains) != 0 || len(got.FeedbackEnv) != 0 || got.VerifyInCopy {
		t.Fatalf("after clearing: %+v", got)
	}
}

// TestProjectToolchainSettingsValidation: an unknown or repeated toolchain, a
// malformed or blank variable, and HOME or PATH are refused naming the field.
func TestProjectToolchainSettingsValidation(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "agentflow.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	repo := mkRepo(t)
	for _, c := range []struct {
		name  string
		p     Project
		field string
	}{
		{"unknown", Project{Toolchains: []string{"ruby"}}, "toolchains[0]"},
		{"twice", Project{Toolchains: []string{"dotnet", "dotnet"}}, "toolchains[1]"},
		{"bad name", Project{FeedbackEnv: map[string]string{"CIV3 PATH": "/c"}}, "feedback_env.CIV3 PATH"},
		{"blank", Project{FeedbackEnv: map[string]string{"CIV3_PATH": "  "}}, "feedback_env.CIV3_PATH"},
		{"home", Project{FeedbackEnv: map[string]string{"HOME": "/h"}}, "feedback_env.HOME"},
		{"path", Project{FeedbackEnv: map[string]string{"PATH": "/bin"}}, "feedback_env.PATH"},
	} {
		t.Run(c.name, func(t *testing.T) {
			c.p.Slug, c.p.RepoPath, c.p.ModelScope = "p-"+c.name, repo, "local"
			c.p.Slug = "p" + c.name[:1]
			wantField(t, s.CreateProject(ctx, "t", c.p), c.field)
		})
	}
}

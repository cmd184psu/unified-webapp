package prompts

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestLoadWithNoFilesOnDisk(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, role := range Roles {
		s, err := Load(role)
		if err != nil || strings.TrimSpace(s) == "" {
			t.Fatalf("%s: %q %v", role, s, err)
		}
	}
	if _, err := Load("nope"); err == nil {
		t.Fatal("unknown role must error")
	}
}

func TestCompilerPromptHasNoVerifyField(t *testing.T) {
	s, _ := Load("compiler")
	if strings.Contains(strings.ToLower(s), "verify") {
		t.Fatal("compiler prompt must not mention a verify field")
	}
	for _, pitfall := range []string{"grep", "curl", "pytest", "&&", "||"} {
		if strings.Contains(s, pitfall) {
			t.Fatalf("compiler prompt still carries shell-pitfall text %q", pitfall)
		}
	}
}

func TestEmbeddedMatchesDirectory(t *testing.T) {
	ents, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, e := range ents {
		if strings.HasSuffix(e.Name(), ".md") {
			n++
		}
	}
	if n != len(Roles) {
		t.Fatalf("%d md files, %d roles", n, len(Roles))
	}
	for _, role := range Roles {
		if _, err := Load(role); err != nil {
			t.Fatalf("role %s: %v", role, err)
		}
	}
}

func TestPromptsNeverInstructGitMutation(t *testing.T) {
	forbidden := regexp.MustCompile(`(?i)\bgit\s+(add|commit|push|branch|stash|reset|checkout)\b|\b(commit|push|stash)\s+(the|your|any)\b`)
	for _, role := range Roles {
		s, err := Load(role)
		if err != nil {
			t.Fatal(err)
		}
		if m := forbidden.FindString(s); m != "" {
			t.Errorf("%s prompt contains git-mutation text %q", role, m)
		}
	}
}

// A person-facing interface must reach what the programmatic one offers (UI/API parity):
// the drafter writes it into the FRD or flags the gap, and the acceptance check looks for it.
func TestParityRulesArePresent(t *testing.T) {
	for role, phrase := range map[string]string{
		"drafter":    "reach every capability",
		"acceptance": "enter or trigger",
	} {
		s, err := Load(role)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(s, phrase) {
			t.Fatalf("%s prompt lacks the UI/API parity rule (%q)", role, phrase)
		}
	}
}

// The compiler must not chain every task: independent tasks run side by side.
func TestCompilerDoesNotChainEverything(t *testing.T) {
	s, err := Load("compiler")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(s, "side by side") || strings.Contains(s, "Use sequential dependencies") {
		t.Fatal("the compiler prompt must ask for dependencies only where needed, not a chain")
	}
}

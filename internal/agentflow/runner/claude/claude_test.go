package claude

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cmd184psu/unified-webapp/internal/agentflow/runner"
)

func TestArgvFollowsTheRole(t *testing.T) {
	a := &Adapter{}
	w := strings.Join(a.Argv("claude", runner.Request{Role: "worker", Model: "claude-haiku-5-5", Workdir: "/w"}), "\n")
	for _, want := range []string{"-p", "stream-json", "claude-haiku-5-5", "--setting-sources\nlocal", "--strict-mcp-config", "Bash,Read,Edit,Write,Glob,Grep", "acceptEdits"} {
		if !strings.Contains(w, want) {
			t.Errorf("worker argv lacks %q:\n%s", want, w)
		}
	}
	r := strings.Join(a.Argv("claude", runner.Request{Role: "reviewer", Model: "m", Workdir: "/w", SessionID: "s-1"}), "\n")
	if strings.Contains(r, "Edit") || !strings.Contains(r, "dontAsk") || !strings.Contains(r, "--resume\ns-1") {
		t.Errorf("reviewer argv wrong:\n%s", r)
	}
}

func TestSettingsSandboxDeniesSecretsAndReviewerWrites(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.Mkdir(filepath.Join(home, ".ssh"), 0o700); err != nil {
		t.Fatal(err)
	}
	for role, wantWriteDeny := range map[string]bool{"worker": false, "reviewer": true} {
		var s struct {
			Sandbox struct {
				Enabled    bool `json:"enabled"`
				Unsandbox  bool `json:"allowUnsandboxedCommands"`
				FailIfNone bool `json:"failIfUnavailable"`
				Filesystem struct {
					DenyRead  []string `json:"denyRead"`
					DenyWrite []string `json:"denyWrite"`
				} `json:"filesystem"`
			} `json:"sandbox"`
			Permissions struct {
				Deny []string `json:"deny"`
			} `json:"permissions"`
		}
		if err := json.Unmarshal([]byte(settingsJSON(role, "/work")), &s); err != nil {
			t.Fatal(err)
		}
		if !s.Sandbox.Enabled || s.Sandbox.Unsandbox || !s.Sandbox.FailIfNone {
			t.Errorf("%s: sandbox must be on, unescapable and required: %+v", role, s.Sandbox)
		}
		if len(s.Sandbox.Filesystem.DenyRead) != 1 || s.Sandbox.Filesystem.DenyRead[0] != filepath.Join(home, ".ssh") {
			t.Errorf("%s: denyRead = %v, want only the existing ~/.ssh", role, s.Sandbox.Filesystem.DenyRead)
		}
		if got := len(s.Sandbox.Filesystem.DenyWrite) == 1; got != wantWriteDeny {
			t.Errorf("%s: denyWrite = %v, want a write deny %v", role, s.Sandbox.Filesystem.DenyWrite, wantWriteDeny)
		}
		if !strings.Contains(strings.Join(s.Permissions.Deny, " "), "Read(**/.env)") {
			t.Errorf("%s: Read tool rules lack .env: %v", role, s.Permissions.Deny)
		}
	}
}

func TestParseStreamTakesTheResultAndUsage(t *testing.T) {
	in := `{"type":"system","subtype":"init","session_id":"s-9"}
{"type":"assistant","message":{"content":[{"type":"text","text":"working"},{"type":"tool_use"}]}}
{"type":"assistant","message":{"content":[{"type":"text","text":"{\"ok\":true}"}]}}
{"type":"result","subtype":"success","is_error":false,"result":"{\"ok\":true}","session_id":"s-9","usage":{"input_tokens":2,"cache_creation_input_tokens":10,"cache_read_input_tokens":3,"output_tokens":5}}
`
	out, err := parseStream(strings.NewReader(in), io.Discard, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.final() != `{"ok":true}` || out.sessionID != "s-9" || !out.completed || out.errMsg != "" {
		t.Fatalf("out = %+v", out)
	}
	if out.usage == nil || out.usage.InputTokens != 15 || out.usage.TotalTokens != 20 {
		t.Fatalf("usage = %+v", out.usage)
	}
}

func TestParseStreamErrorResultIsTheFailure(t *testing.T) {
	in := `{"type":"result","subtype":"error_during_execution","is_error":true,"result":"API Error: 429 rate limit exceeded"}` + "\n"
	out, _ := parseStream(strings.NewReader(in), io.Discard, nil)
	if out.completed || !strings.Contains(out.errMsg, "429") {
		t.Fatalf("out = %+v", out)
	}
	if runner.ClassifyMessage(out.errMsg) != runner.ErrRateLimit {
		t.Fatalf("class = %v, want rate limit", runner.ClassifyMessage(out.errMsg))
	}
}

func TestParseVersion(t *testing.T) {
	if v, err := parseVersion("2.1.326 (Claude Code)\n"); err != nil || v != "2.1.326" {
		t.Fatalf("%q %v", v, err)
	}
}

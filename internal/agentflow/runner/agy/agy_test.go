package agy

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cmd184psu/unified-webapp/internal/agentflow/runner"
)

func TestArgvAndPromptHandling(t *testing.T) {
	a := &Adapter{}
	argv := strings.Join(a.Argv("agy", runner.Request{Role: "worker", Model: "gemini-3.8-flash-low", Workdir: "/w", SessionID: "c-1"}, "do it", `{"x":1}`), "\n")
	for _, want := range []string{"--output-format\nstream-json", "--model\ngemini-3.8-flash-low", "--sandbox", "--settings\n{\"x\":1}", "--conversation\nc-1", "-p=do it"} {
		if !strings.Contains(argv, want) {
			t.Errorf("argv lacks %q:\n%s", want, argv)
		}
	}
	dir := t.TempDir()
	small, big := filepath.Join(dir, "s"), filepath.Join(dir, "b")
	_ = os.WriteFile(small, []byte("short"), 0o600)
	_ = os.WriteFile(big, []byte(strings.Repeat("x", maxInlinePrompt+1)), 0o600)
	if txt, rd, err := prompt(small); err != nil || txt != "short" || rd != "" {
		t.Fatalf("small prompt = %q %q %v", txt, rd, err)
	}
	if txt, rd, err := prompt(big); err != nil || rd != big || !strings.Contains(txt, big) || len(txt) > 1000 {
		t.Fatalf("big prompt must be read from its file: %q %q %v", txt, rd, err)
	}
}

func TestSettingsDenySecretsEnvFilesAndReviewerWrites(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.Mkdir(filepath.Join(home, ".ssh"), 0o700); err != nil {
		t.Fatal(err)
	}
	ws := t.TempDir()
	_ = os.MkdirAll(filepath.Join(ws, "sub", ".git"), 0o755)
	_ = os.WriteFile(filepath.Join(ws, ".env"), []byte("x"), 0o600)
	_ = os.WriteFile(filepath.Join(ws, "sub", ".env.local"), []byte("x"), 0o600)
	_ = os.WriteFile(filepath.Join(ws, "sub", ".git", ".env"), []byte("x"), 0o600) // inside .git: skipped
	for role, wantWriteDeny := range map[string]bool{"worker": false, "reviewer": true} {
		var s struct {
			Sandbox bool   `json:"enableTerminalSandbox"`
			Tool    string `json:"toolPermission"`
			Perms   struct {
				Deny  []string `json:"deny"`
				Allow []string `json:"allow"`
			} `json:"permissions"`
		}
		if err := json.Unmarshal([]byte(settingsJSON(role, ws, "")), &s); err != nil {
			t.Fatal(err)
		}
		all := strings.Join(s.Perms.Deny, "\n")
		for _, want := range []string{"read_file(" + filepath.Join(home, ".ssh") + ")", "read_file(" + filepath.Join(ws, ".env") + ")", "read_file(" + filepath.Join(ws, "sub", ".env.local") + ")"} {
			if !strings.Contains(all, want) {
				t.Errorf("%s: deny lacks %q:\n%s", role, want, all)
			}
		}
		if strings.Contains(all, ".git/.env") {
			t.Errorf("%s: .git must be skipped", role)
		}
		if got := strings.Contains(all, "write_file("+ws+")"); got != wantWriteDeny {
			t.Errorf("%s: write deny = %v, want %v", role, got, wantWriteDeny)
		}
		if !s.Sandbox || s.Tool != "proceed-in-sandbox" {
			t.Errorf("%s: sandbox must be on: %+v", role, s)
		}
	}
	if !strings.Contains(settingsJSON("worker", ws, "/stage/prompt.md"), "read_file(/stage/prompt.md)") {
		t.Error("a file-read prompt must be allowed")
	}
}

func TestParseStreamAndModels(t *testing.T) {
	in := `{"event":"init","init":{"conversation_id":"c-9"}}
{"event":"step_update","step_update":{"step_type":"agent_response","text_delta":"po"}}
{"event":"result","result":{"conversation_id":"c-9","status":"SUCCESS","response":"pong\n","usage":{"input_tokens":10,"output_tokens":2,"cache_read_tokens":5}}}
`
	out, err := parseStream(strings.NewReader(in), io.Discard, nil)
	if err != nil || out.final() != "pong" || out.sessionID != "c-9" || !out.completed {
		t.Fatalf("out = %+v err %v", out, err)
	}
	if out.usage == nil || out.usage.InputTokens != 15 || out.usage.TotalTokens != 17 {
		t.Fatalf("usage = %+v", out.usage)
	}
	bad, _ := parseStream(strings.NewReader(`{"event":"result","result":{"status":"ERROR","error":"quota exceeded: rate limit"}}`+"\n"), io.Discard, nil)
	if bad.completed || runner.ClassifyMessage(bad.errMsg) != runner.ErrRateLimit {
		t.Fatalf("bad = %+v class %v", bad, runner.ClassifyMessage(bad.errMsg))
	}
	got := parseModels([]byte("Fetching available models...\ngemini-3.8-flash-low\tGemini 3.8 Flash (Low)\nclaude-sonnet-4-6\tClaude Sonnet 4.6\n"))
	if len(got) != 2 || got[0] != "gemini-3.8-flash-low" {
		t.Fatalf("models = %v", got)
	}
	if v, err := parseVersion("1.5.10\n"); err != nil || v != "1.5.10" {
		t.Fatalf("%q %v", v, err)
	}
}

package opencode

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"cmd184psu/unified-webapp/internal/agentflow/runner"
)

var update = flag.Bool("update", false, "rewrite golden files")

func specs() []BackendSpec {
	return []BackendSpec{
		{Backend: runner.Backend{Name: "local", Host: "http://localhost:11434/", APIType: "ollama"}, Models: []string{"qwen3:32b"}},
		{Backend: runner.Backend{Name: "hosted", Host: "https://api.example.com/v1", APIType: "openai_compatible"}, Models: []string{"big"}},
		{Backend: runner.Backend{Name: "claude", Host: "https://api.anthropic.com/v1", APIType: "anthropic"}, Models: []string{"sonnet"}},
	}
}

func decode(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestGoldenConfig(t *testing.T) {
	got, err := RenderConfig(specs())
	if err != nil {
		t.Fatal(err)
	}
	golden := filepath.Join("testdata", "opencode.golden.json")
	if *update {
		if err := os.WriteFile(golden, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("generated config differs from %s (run with -update)", golden)
	}
}

func TestConfigHasNoPluginsOrHomePaths(t *testing.T) {
	data, err := RenderConfig(specs())
	if err != nil {
		t.Fatal(err)
	}
	m := decode(t, data)
	for _, k := range []string{"plugin", "plugins", "mcp", "instructions"} {
		if _, ok := m[k]; ok {
			t.Errorf("config has %q key", k)
		}
	}
	if strings.Contains(string(data), "plugin") {
		t.Error("config text mentions plugin")
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" && home != "/" && strings.Contains(string(data), home) {
		t.Errorf("config contains real home path %s", home)
	}
	if regexp.MustCompile(`"/[a-zA-Z]`).Match(data) {
		t.Error("config contains an absolute filesystem path value")
	}
}

func TestConfigHoldsNoSecretValues(t *testing.T) {
	t.Setenv(KeyEnv("hosted"), "sk-live-SECRETVALUE123456")
	t.Setenv(KeyEnv("claude"), "sk-ant-SECRETVALUE123456")
	data, err := RenderConfig(specs())
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	for _, bad := range []string{"SECRETVALUE", "sk-live", "sk-ant"} {
		if strings.Contains(s, bad) {
			t.Errorf("config contains %q", bad)
		}
	}
	prov := decode(t, data)["provider"].(map[string]any)
	for _, name := range []string{"hosted", "claude"} {
		key := prov[name].(map[string]any)["options"].(map[string]any)["apiKey"].(string)
		if key != "{env:"+KeyEnv(name)+"}" {
			t.Errorf("%s apiKey = %q, want env reference", name, key)
		}
	}
	// Every apiKey is either an env reference or the fixed ollama placeholder.
	for name, p := range prov {
		key := p.(map[string]any)["options"].(map[string]any)["apiKey"].(string)
		if !strings.HasPrefix(key, "{env:") && key != "ollama" {
			t.Errorf("provider %s has literal-looking apiKey %q", name, key)
		}
	}
}

func TestOllamaIsOpenAICompatible(t *testing.T) {
	data, _ := RenderConfig(specs())
	p := decode(t, data)["provider"].(map[string]any)["local"].(map[string]any)
	if p["npm"] != "@ai-sdk/openai-compatible" {
		t.Errorf("npm = %v", p["npm"])
	}
	if p["options"].(map[string]any)["baseURL"] != "http://localhost:11434/v1" {
		t.Errorf("baseURL = %v", p["options"])
	}
}

func TestBadBackends(t *testing.T) {
	for name, b := range map[string]BackendSpec{
		"bad id":  {Backend: runner.Backend{Name: "Bad Name", Host: "http://x", APIType: "ollama"}},
		"bad api": {Backend: runner.Backend{Name: "ok", Host: "http://x", APIType: "grpc"}},
	} {
		if _, err := RenderConfig([]BackendSpec{b}); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
	clash := []BackendSpec{
		{Backend: runner.Backend{Name: "a-b", Host: "http://x", APIType: "openai_compatible"}},
		{Backend: runner.Backend{Name: "a_b", Host: "http://x", APIType: "openai_compatible"}},
	}
	if _, err := RenderConfig(clash); err == nil {
		t.Error("key variable clash: want error")
	}
	dup := []BackendSpec{specs()[0], specs()[0]}
	if _, err := RenderConfig(dup); err == nil {
		t.Error("duplicate backend: want error")
	}
}

func agentPerm(t *testing.T, role string) map[string]any {
	t.Helper()
	data, _ := RenderConfig(specs())
	a := decode(t, data)["agent"].(map[string]any)[AgentName(role)]
	if a == nil {
		t.Fatalf("no agent for %s", role)
	}
	return a.(map[string]any)["permission"].(map[string]any)
}

func TestEveryRoleHasAnAgent(t *testing.T) {
	data, _ := RenderConfig(specs())
	agents := decode(t, data)["agent"].(map[string]any)
	if len(agents) != len(Roles) {
		t.Fatalf("got %d agents, want %d", len(agents), len(Roles))
	}
	for _, r := range Roles {
		a := agents[AgentName(r)].(map[string]any)
		if a["mode"] != "primary" || strings.TrimSpace(a["prompt"].(string)) == "" {
			t.Errorf("%s: bad agent %v", r, a)
		}
	}
}

func TestEditPermission(t *testing.T) {
	for _, r := range Roles {
		want := "deny"
		if writesWorktree(r) {
			want = "allow"
		}
		if got := agentPerm(t, r)["edit"]; got != want {
			t.Errorf("%s edit = %v, want %s", r, got, want)
		}
		if got := agentPerm(t, r)["external_directory"]; got != "deny" {
			t.Errorf("%s external_directory = %v", r, got)
		}
	}
}

// ordered returns the bash rules in marshalled order.
func ordered(t *testing.T, role string) []rule {
	t.Helper()
	data, err := json.Marshal(permissionFor(role).Bash)
	if err != nil {
		t.Fatal(err)
	}
	dec := json.NewDecoder(strings.NewReader(string(data)))
	if _, err := dec.Token(); err != nil {
		t.Fatal(err)
	}
	var out []rule
	for dec.More() {
		k, _ := dec.Token()
		v, _ := dec.Token()
		out = append(out, rule{k.(string), v.(string)})
	}
	return out
}

func indexOf(rs []rule, pattern string) int {
	for i, r := range rs {
		if r.Pattern == pattern {
			return i
		}
	}
	return -1
}

func TestWorkerDeniesEveryGitWriteVerb(t *testing.T) {
	rs := ordered(t, "worker")
	for _, v := range []string{"add", "commit", "branch -d", "branch -m", "branch -c", "tag", "stash", "push", "reset", "checkout", "switch", "merge", "rebase", "cherry-pick", "worktree", "config"} {
		i := indexOf(rs, "git "+v+"*")
		if i < 0 || rs[i].Action != "deny" {
			t.Errorf("worker has no deny for git %s", v)
		}
	}
	for _, p := range []string{"gh *", "glab *", "ssh *"} {
		if i := indexOf(rs, p); i < 0 || rs[i].Action != "deny" {
			t.Errorf("worker has no deny for %s", p)
		}
	}
}

// TestRuleOrder pins last-match-wins ordering (S2): the catch-all comes first,
// "git *" deny after it, and every read-only git allow after the deny.
func TestRuleOrder(t *testing.T) {
	for _, r := range Roles {
		rs := ordered(t, r)
		star := indexOf(rs, "*")
		gitDeny := indexOf(rs, "git *")
		if star != 0 {
			t.Errorf("%s: catch-all at %d, want 0", r, star)
		}
		if gitDeny < 0 || rs[gitDeny].Action != "deny" {
			t.Fatalf("%s: no git * deny", r)
		}
		for _, p := range gitReadOnly {
			i := indexOf(rs, p)
			if i < 0 || rs[i].Action != "allow" || i < gitDeny {
				t.Errorf("%s: %q must be an allow after \"git *\" deny (at %d)", r, p, i)
			}
		}
		for _, p := range gitWriteDenies {
			if i := indexOf(rs, p); i < gitDeny {
				t.Errorf("%s: %q must come after \"git *\"", r, p)
			}
		}
		if writesWorktree(r) != (rs[0].Action == "allow") {
			t.Errorf("%s: catch-all action %s", r, rs[0].Action)
		}
	}
}

func TestReadOnlyRolesBashIsReadOnly(t *testing.T) {
	for _, r := range Roles {
		if writesWorktree(r) {
			continue
		}
		rs := ordered(t, r)
		if rs[0].Action != "deny" {
			t.Errorf("%s: catch-all must deny", r)
		}
		last := rs[len(rs)-1]
		if last.Pattern != "*>*" || last.Action != "deny" {
			t.Errorf("%s: last rule = %v, want redirection deny", r, last)
		}
		for _, w := range []string{"rm", "mv", "cp", "sed", "tee", "go", "npm", "sh", "bash", "env"} {
			for _, x := range rs {
				if x.Action == "allow" && strings.HasPrefix(x.Pattern, w+" ") {
					t.Errorf("%s allows %q", r, x.Pattern)
				}
			}
		}
	}
}

func TestPermissionJSONKeepsOrder(t *testing.T) {
	data, _ := RenderConfig(specs())
	s := string(data)
	s = s[strings.Index(s, `"af-worker"`):]
	a := strings.Index(s, `"*": "allow"`)
	b := strings.Index(s, `"git *": "deny"`)
	c := strings.Index(s, `"git status*": "allow"`)
	if !(a >= 0 && a < b && b < c) {
		t.Fatalf("worker bash order wrong in JSON: %d %d %d", a, b, c)
	}
	if !strings.Contains(string(data), `"*>*": "deny"`) {
		t.Error("redirection pattern was HTML-escaped or missing")
	}
}

func TestGenerateConfigFrozenPerStage(t *testing.T) {
	root := t.TempDir()
	stageA, stageB := filepath.Join(root, "a"), filepath.Join(root, "b")
	pathA, err := GenerateConfig(specs(), stageA)
	if err != nil {
		t.Fatal(err)
	}
	if pathA != filepath.Join(stageA, "xdg", "opencode", "opencode.json") {
		t.Errorf("path = %s", pathA)
	}
	before, _ := os.ReadFile(pathA)

	edited := specs()
	edited[0].Host = "http://other:11434"
	edited[0].Models = append(edited[0].Models, "new-model")
	pathB, err := GenerateConfig(edited, stageB)
	if err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(pathA)
	if string(before) != string(after) {
		t.Fatal("stage A config changed after generating stage B")
	}
	b, _ := os.ReadFile(pathB)
	if string(b) == string(before) || !strings.Contains(string(b), "other:11434") {
		t.Fatal("stage B did not pick up the backend edit")
	}
	// A retry with the same input is fine; different input is refused.
	if _, err := GenerateConfig(specs(), stageA); err != nil {
		t.Errorf("idempotent regenerate: %v", err)
	}
	if _, err := GenerateConfig(edited, stageA); err != ErrFrozen {
		t.Errorf("changed regenerate: err = %v, want ErrFrozen", err)
	}
	if st, err := os.Stat(HomeDir(stageA)); err != nil || !st.IsDir() {
		t.Error("stage home not created")
	}
}

func TestNoDuplicatePermissionPatternsAndNoAsk(t *testing.T) {
	for _, r := range Roles {
		p := permissionFor(r)
		if err := p.Bash.check(); err != nil {
			t.Errorf("%s: %v", r, err)
		}
		if err := p.Read.check(); err != nil {
			t.Errorf("%s: %v", r, err)
		}
	}
	data, _ := RenderConfig(specs())
	if strings.Contains(string(data), `"ask"`) {
		t.Error("config contains an ask action")
	}
	if err := (rules{{"a", "allow"}, {"a", "deny"}}).check(); err == nil {
		t.Error("duplicate not detected")
	}
	for _, r := range Roles {
		p := permissionFor(r)
		if p.Task != "deny" || p.WebFetch != "deny" || p.WebSearch != "deny" {
			t.Errorf("%s: task/web not denied", r)
		}
	}
}

func TestIsolationEnv(t *testing.T) {
	parent := []string{
		"PATH=/usr/bin", "LANG=C", "HOME=/home/real",
		"OPENCODE_CONFIG_DIR=/home/real/.config/opencode", "OPENCODE_CONFIG=/x",
		"XDG_CONFIG_HOME=/home/real/.config", "OPENCODE_DISABLE_PROJECT_CONFIG=0",
		"AWS_SECRET_ACCESS_KEY=zzz", KeyEnv("hosted") + "=sk-1", "AGENTFLOW_KEY_UNLISTED=sk-2",
	}
	stage := "/data/stages/s1"
	env := IsolationEnv(stage, parent, []string{KeyEnv("hosted"), KeyEnv("missing"), "AWS_SECRET_ACCESS_KEY"})
	got := map[string]string{}
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		if _, dup := got[k]; dup {
			t.Errorf("duplicate %s", k)
		}
		got[k] = v
	}
	want := map[string]string{
		"HOME":                             stage + "/home",
		"XDG_CONFIG_HOME":                  stage + "/xdg",
		"XDG_DATA_HOME":                    stage + "/home/.local/share",
		"XDG_STATE_HOME":                   stage + "/home/.local/state",
		"XDG_CACHE_HOME":                   stage + "/home/.cache",
		"OPENCODE_PURE":                    "1",
		"OPENCODE_DISABLE_PROJECT_CONFIG":  "1",
		"OPENCODE_DISABLE_CLAUDE_CODE":     "1",
		"OPENCODE_DISABLE_EXTERNAL_SKILLS": "1",
		"OPENCODE_DISABLE_AUTOUPDATE":      "1",
		"PATH":                             "/usr/bin",
		"LANG":                             "C",
		KeyEnv("hosted"):                   "sk-1",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
	for k := range got {
		if _, ok := want[k]; !ok {
			t.Errorf("unexpected env %s", k)
		}
	}
	for _, k := range []string{"OPENCODE_CONFIG_DIR", "OPENCODE_CONFIG"} {
		if _, ok := got[k]; ok {
			t.Errorf("%s must never be set", k)
		}
	}
}

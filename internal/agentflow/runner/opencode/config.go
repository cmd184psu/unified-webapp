package opencode

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"cmd184psu/unified-webapp/internal/agentflow/runner"
)

// ErrFrozen is returned when a stage's config already exists with different
// content: a stage keeps the configuration it started with (BK5).
var ErrFrozen = errors.New("opencode: stage config already generated with different content")

// BackendSpec is a backend as the adapter renders it: the runner-neutral
// backend plus the model names the stage may use.
type BackendSpec struct {
	runner.Backend
	Models []string
}

var providerID = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

// ConfigPath is where the stage's generated config lives. opencode reads
// $XDG_CONFIG_HOME/opencode/opencode.json and XDG_CONFIG_HOME is <stage>/xdg
// (spike S3); OPENCODE_CONFIG is additive and is not used.
func ConfigPath(stageDir string) string {
	return filepath.Join(stageDir, "xdg", "opencode", "opencode.json")
}

// HomeDir is the stage's private HOME.
func HomeDir(stageDir string) string { return filepath.Join(stageDir, "home") }

// KeyEnv is the environment variable that carries the API key for a backend.
// The generated config references it as {env:NAME} and never holds the value;
// the engine must pass it to the child through IsolationEnv.
func KeyEnv(backend string) string {
	return "AGENTFLOW_KEY_" + strings.ToUpper(strings.NewReplacer("-", "_").Replace(backend))
}

// ModelRef is the --model value for a backend's model.
func ModelRef(backend, model string) string { return backend + "/" + model }

type providerDef struct {
	NPM     string                `json:"npm"`
	Name    string                `json:"name"`
	Options map[string]string     `json:"options"`
	Models  map[string]modelEntry `json:"models"`
}

type modelEntry struct {
	Name string `json:"name"`
}

type configDoc struct {
	Schema     string                 `json:"$schema"`
	AutoUpdate bool                   `json:"autoupdate"`
	Share      string                 `json:"share"`
	Plugin     []string               `json:"plugin,omitempty"`
	Provider   map[string]providerDef `json:"provider"`
	Agent      map[string]agentDef    `json:"agent"`
}

func renderProvider(b BackendSpec) (providerDef, error) {
	if !providerID.MatchString(b.Name) {
		return providerDef{}, fmt.Errorf("opencode: backend name %q is not a valid provider id", b.Name)
	}
	host := strings.TrimRight(b.Host, "/")
	p := providerDef{Name: b.Name, Options: map[string]string{}, Models: map[string]modelEntry{}}
	switch b.APIType {
	case "ollama":
		// Ollama is rendered as its OpenAI-compatible provider (MB2). It needs
		// no key, but the SDK wants a non-empty one; this is a fixed
		// placeholder, not a secret.
		p.NPM = "@ai-sdk/openai-compatible"
		p.Options["baseURL"] = host + "/v1"
		p.Options["apiKey"] = "ollama"
	case "openai_compatible":
		p.NPM = "@ai-sdk/openai-compatible"
		p.Options["baseURL"] = host
		p.Options["apiKey"] = "{env:" + KeyEnv(b.Name) + "}"
	case "anthropic":
		p.NPM = "@ai-sdk/anthropic"
		p.Options["baseURL"] = host
		p.Options["apiKey"] = "{env:" + KeyEnv(b.Name) + "}"
	default:
		return providerDef{}, fmt.Errorf("opencode: backend %q has unsupported api type %q", b.Name, b.APIType)
	}
	for _, m := range b.Models {
		p.Models[m] = modelEntry{Name: m}
	}
	return p, nil
}

// RenderConfig returns the opencode.json bytes for backends. It contains no
// filesystem path and no secret value, and no plugin entry unless the project
// opted into plugins (OA3): then exactly those, as pinned name@version, sorted
// by name. A plugin that is not an exact version is refused here as well as
// where it is stored.
func RenderConfig(backends []BackendSpec, plugins ...runner.Plugin) ([]byte, error) {
	doc := configDoc{
		Schema:   "https://opencode.ai/config.json",
		Share:    "disabled",
		Provider: map[string]providerDef{},
		Agent:    map[string]agentDef{},
	}
	keys := map[string]string{}
	for _, b := range backends {
		if other, clash := keys[KeyEnv(b.Name)]; clash {
			return nil, fmt.Errorf("opencode: backends %q and %q map to the same key variable %s", other, b.Name, KeyEnv(b.Name))
		}
		keys[KeyEnv(b.Name)] = b.Name
		if _, dup := doc.Provider[b.Name]; dup {
			return nil, fmt.Errorf("opencode: duplicate backend %q", b.Name)
		}
		p, err := renderProvider(b)
		if err != nil {
			return nil, err
		}
		doc.Provider[b.Name] = p
	}
	names := map[string]bool{}
	for _, pl := range plugins {
		if err := pl.Validate(); err != nil {
			return nil, fmt.Errorf("opencode: %w", err)
		}
		if names[pl.Name] {
			return nil, fmt.Errorf("opencode: duplicate plugin %q", pl.Name)
		}
		names[pl.Name] = true
		doc.Plugin = append(doc.Plugin, pl.Spec())
	}
	slices.Sort(doc.Plugin)
	for _, role := range Roles {
		a, err := agentFor(role)
		if err != nil {
			return nil, err
		}
		doc.Agent[AgentName(role)] = a
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// GenerateConfig renders backends and freezes the result for the stage (BK5).
// It creates the stage's xdg config dir and private home. If the stage already
// has a config, identical content is accepted (a retry) and different content
// returns ErrFrozen; a later backend edit therefore never changes a stage that
// already started. It returns the config path.
func GenerateConfig(backends []BackendSpec, stageDir string, plugins ...runner.Plugin) (string, error) {
	data, err := RenderConfig(backends, plugins...)
	if err != nil {
		return "", err
	}
	path := ConfigPath(stageDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	if err := os.MkdirAll(HomeDir(stageDir), 0o700); err != nil {
		return "", err
	}
	// Write a temp file in the same directory and link it into place, so a
	// crash never leaves a partial config that would freeze the stage.
	tmp, err := os.CreateTemp(filepath.Dir(path), ".opencode-*.tmp")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	if err := os.Link(tmp.Name(), path); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return "", err
		}
		old, rerr := os.ReadFile(path)
		if rerr != nil {
			return "", rerr
		}
		if !bytes.Equal(old, data) {
			return "", ErrFrozen
		}
	}
	if d, err := os.Open(filepath.Dir(path)); err == nil {
		d.Sync()
		d.Close()
	}
	return path, nil
}

// IsolationEnv builds the child environment for a stage from an allowlist
// (spike S3): the parent's PATH and LANG, a fresh HOME and XDG dirs under the
// stage, project config and external skills disabled, plus the named key
// variables copied from parent. Nothing else is inherited, so a parent's
// OPENCODE_CONFIG_DIR, OPENCODE_CONFIG or XDG_* can never leak the user's
// configuration in. The adapter also passes --pure on the command line (B7).
// B7 note: a fresh HOME makes the first run bootstrap node_modules and fetch
// the models list; consider OPENCODE_DISABLE_MODELS_FETCH and allow for the
// start-up time.
func IsolationEnv(stageDir string, parent []string, keyEnvs []string) []string {
	lookup := func(name string) (string, bool) {
		for i := len(parent) - 1; i >= 0; i-- {
			if k, v, ok := strings.Cut(parent[i], "="); ok && k == name {
				return v, true
			}
		}
		return "", false
	}
	home := HomeDir(stageDir)
	env := []string{
		"HOME=" + home,
		"XDG_CONFIG_HOME=" + filepath.Join(stageDir, "xdg"),
		"XDG_DATA_HOME=" + filepath.Join(home, ".local", "share"),
		"XDG_STATE_HOME=" + filepath.Join(home, ".local", "state"),
		"XDG_CACHE_HOME=" + filepath.Join(home, ".cache"),
		"OPENCODE_PURE=1", // backup to --pure; B7 must still pass --pure
		"OPENCODE_DISABLE_PROJECT_CONFIG=1",
		"OPENCODE_DISABLE_CLAUDE_CODE=1",
		"OPENCODE_DISABLE_EXTERNAL_SKILLS=1",
		"OPENCODE_DISABLE_AUTOUPDATE=1",
	}
	for _, name := range append([]string{"PATH", "LANG"}, keyEnvs...) {
		if !strings.HasPrefix(name, "AGENTFLOW_KEY_") && name != "PATH" && name != "LANG" {
			continue
		}
		if v, ok := lookup(name); ok {
			env = append(env, name+"="+v)
		}
	}
	return env
}

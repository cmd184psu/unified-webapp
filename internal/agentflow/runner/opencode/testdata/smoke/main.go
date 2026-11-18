// Command smoke is the operator-run live check behind scripts/agentflow-smoke.sh.
// It runs ONE real opencode stage through the production adapter and proves
//
//	AC-OC1: a prompt of at least AGENTFLOW_SMOKE_PROMPT_KB (default 500) KB
//	        reaches the model intact (the last line survives), and
//	AC-OC2: a marker plugin in the operator-style global opencode config of a
//	        throwaway HOME is not loaded by the adapter's run.
//
// It lives under testdata so `go build ./...` and `go test ./...` never see it.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"cmd184psu/unified-webapp/internal/agentflow/runner"
	"cmd184psu/unified-webapp/internal/agentflow/runner/opencode"
)

func env(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}

func die(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "SMOKE FAIL: "+format+"\n", a...)
	os.Exit(1)
}

func main() {
	bin := env("AGENTFLOW_SMOKE_OPENCODE", filepath.Join(os.Getenv("HOME"), ".opencode", "bin", "opencode"))
	host := env("AGENTFLOW_SMOKE_HOST", "")
	model := env("AGENTFLOW_SMOKE_MODEL", "")
	apiType := env("AGENTFLOW_SMOKE_API_TYPE", "ollama")
	stubLog := env("AGENTFLOW_SMOKE_STUB_LOG", "") // set by the script when it runs the local stub
	kb, err := strconv.Atoi(env("AGENTFLOW_SMOKE_PROMPT_KB", "500"))
	if err != nil || kb <= 0 {
		die("AGENTFLOW_SMOKE_PROMPT_KB must be a positive integer")
	}
	if host == "" || model == "" {
		die("AGENTFLOW_SMOKE_HOST and AGENTFLOW_SMOKE_MODEL are required")
	}
	work, err := os.MkdirTemp("", "agentflow-smoke-")
	if err != nil {
		die("%v", err)
	}
	defer os.RemoveAll(work)

	// A throwaway HOME that looks like an operator's: a global opencode config
	// with a plugin that records when it is loaded.
	parentHome := filepath.Join(work, "home")
	marker := filepath.Join(work, "plugin-loaded.marker")
	plugDir := filepath.Join(parentHome, ".config", "opencode", "plugins")
	must(os.MkdirAll(plugDir, 0o755))
	plugin := fmt.Sprintf("import { writeFileSync } from \"node:fs\"\nexport const Marker = async () => {\n  writeFileSync(%q, \"loaded\")\n  return {}\n}\n", marker)
	must(os.WriteFile(filepath.Join(plugDir, "marker.js"), []byte(plugin), 0o644))

	parentEnv := []string{"HOME=" + parentHome}
	var keys []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if k == "HOME" {
			continue
		}
		parentEnv = append(parentEnv, kv)
		if strings.HasPrefix(k, "AGENTFLOW_KEY_") {
			keys = append(keys, k)
		}
	}

	// Control: with the throwaway HOME as-is, opencode does load the plugin, so
	// "absent after the adapter run" below means something.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	ctl := exec.CommandContext(ctx, bin, "agent", "list")
	ctl.Env = parentEnv
	ctl.Dir = work
	_ = ctl.Run()
	controlLoaded := fileExists(marker)
	if controlLoaded {
		must(os.Remove(marker))
	} else {
		fmt.Println("SMOKE WARN: control run did not load the marker plugin; AC-OC2 below is not conclusive")
	}

	// The prompt: kb KB of filler, then the end marker and the instruction.
	nonce := fmt.Sprintf("SMOKE-%d", time.Now().UnixNano())
	var sb strings.Builder
	for i := 0; sb.Len() < kb<<10; i++ {
		fmt.Fprintf(&sb, "filler line %06d: agentflow smoke test padding, ignore it.\n", i)
	}
	sb.WriteString("END-OF-PROMPT-MARKER\nIgnore all text above. Reply with exactly " + nonce + " and nothing else.\n")
	stage := filepath.Join(work, "stage")
	wt := filepath.Join(work, "wt")
	must(os.MkdirAll(wt, 0o755))
	b := runner.Backend{Name: "smoke", Host: host, APIType: apiType}
	if _, err := opencode.GenerateConfig([]opencode.BackendSpec{{Backend: b, Models: []string{model}}}, stage); err != nil {
		die("generate config: %v", err)
	}
	pp := filepath.Join(stage, "prompt.md")
	must(os.WriteFile(pp, []byte(sb.String()), 0o644))

	a := &opencode.Adapter{Path: bin, ParentEnv: parentEnv, KeyEnvs: keys}
	res, err := a.Run(ctx, runner.Request{Role: "reviewer", Backend: b, Model: model, Workdir: wt, StageDir: stage, PromptPath: pp})
	if err != nil {
		die("opencode run: %v (%+v)", err, res)
	}
	fmt.Printf("final reply: %.200q\n", res.Final)

	// AC-OC1.
	if stubLog != "" {
		if !stubSawWholePrompt(stubLog, kb<<10) {
			die("AC-OC1: the stub never received a %d KB request ending with the marker (%s)", kb, stubLog)
		}
	} else if !strings.Contains(res.Final, nonce) {
		die("AC-OC1: the reply does not echo %s, so the end of the %d KB prompt did not arrive", nonce, kb)
	}
	fmt.Printf("AC-OC1 ok: a %d KB prompt arrived intact\n", kb)

	// AC-OC2.
	if fileExists(marker) {
		die("AC-OC2: the marker plugin in the throwaway HOME's global config was loaded")
	}
	fmt.Printf("AC-OC2 ok: the marker plugin was not loaded (control loaded it: %v)\n", controlLoaded)
}

func stubSawWholePrompt(path string, min int) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(data), "\n") {
		var e struct {
			BodyBytes int  `json:"bodyBytes"`
			Marker    bool `json:"containsMarkerEnd"`
		}
		if json.Unmarshal([]byte(line), &e) == nil && e.BodyBytes >= min && e.Marker {
			return true
		}
	}
	return false
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

func must(err error) {
	if err != nil {
		die("%v", err)
	}
}

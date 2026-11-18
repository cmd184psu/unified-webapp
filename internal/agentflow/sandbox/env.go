package sandbox

import "sort"

// SandboxHome is HOME inside the sandbox. The host /home is hidden, so this
// path never exists outside it; Spec.StageHome is bound here.
const SandboxHome = "/home/agent"

// baseEnv returns the allowlisted environment (WC1). Nothing is inherited from
// the server: callers add runner-specific variables through Spec.Env, which may
// override these.
func baseEnv(path string) map[string]string {
	h := SandboxHome
	return map[string]string{
		"PATH":                path,
		"HOME":                h,
		"LANG":                "C.UTF-8",
		"TERM":                "dumb",
		"GIT_TERMINAL_PROMPT": "0",
		"GIT_CONFIG_GLOBAL":   "/dev/null",
		"GIT_CONFIG_NOSYSTEM": "1",
		"GIT_OPTIONAL_LOCKS":  "0",
		"XDG_CONFIG_HOME":     h + "/.config",
		"XDG_CACHE_HOME":      h + "/.cache",
		"XDG_DATA_HOME":       h + "/.local/share",
		"XDG_STATE_HOME":      h + "/.local/state",
		"GOCACHE":             h + "/.cache/go-build",
		"GOMODCACHE":          h + "/go/pkg/mod",
		"GOPATH":              h + "/go",
		"GOTOOLCHAIN":         "local",
	}
}

// setenvArgs renders env as `--setenv K V` pairs in sorted key order. The
// caller must have emitted --clearenv first, which drops earlier --setenv.
func setenvArgs(env map[string]string) []string {
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	args := make([]string, 0, 3*len(keys))
	for _, k := range keys {
		args = append(args, "--setenv", k, env[k])
	}
	return args
}

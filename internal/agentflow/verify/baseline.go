package verify

import (
	"context"
	"fmt"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

// CheckFeedbackPrefix starts the name of each feedback command's check.
const CheckFeedbackPrefix = "feedback: "

// Failure is one normalised failure signature (V5, mirroring ralph verify).
// Sig is stable across runs: no durations, addresses or timestamps. File or
// Dir, relative to the worktree, say where it points so a baseline failure can
// be tied to a touched file.
type Failure struct {
	Sig  string `json:"sig"`
	File string `json:"file,omitempty"`
	Dir  string `json:"dir,omitempty"`
	Text string `json:"text,omitempty"`
}

var (
	reTestFail = regexp.MustCompile(`^\s*--- FAIL: (\S+)`)
	rePkgFail  = regexp.MustCompile(`^FAIL\t(\S+)`)
	reFileLine = regexp.MustCompile(`^(?:vet: )?(?:\./)?([^\s:]+\.go):(\d+):(?:\d+:)?\s*(.*\S)`)
	reDuration = regexp.MustCompile(`\b\d+(\.\d+)?(ms|s)\b`)
	reAddr     = regexp.MustCompile(`0x[0-9a-fA-F]+`)
	reErrWord  = regexp.MustCompile(`(?i)error|fail|panic|assert`)
)

func normalise(s string) string {
	s = reDuration.ReplaceAllString(s, "<t>")
	s = reAddr.ReplaceAllString(s, "<addr>")
	return strings.Join(strings.Fields(s), " ")
}

// fingerprint derives failure signatures from a finished command. A command
// that failed always yields at least one, so a failure is never lost.
func fingerprint(r CommandResult, module string) []Failure {
	if !r.Failed() {
		return nil
	}
	name := r.Argv.String()
	if r.TimedOut {
		return []Failure{{Sig: name + "|timeout"}}
	}
	var fs []Failure
	if len(r.Argv) >= 2 && filepath.Base(r.Argv[0]) == "go" {
		switch r.Argv[1] {
		case "test", "vet", "build":
			fs = parseGo("go "+r.Argv[1], r.Output, module)
		}
	}
	if len(fs) == 0 {
		fs = []Failure{genericFailure(name, r)}
	}
	seen := map[string]bool{}
	out := fs[:0]
	for _, f := range fs {
		if !seen[f.Sig] {
			seen[f.Sig] = true
			out = append(out, f)
		}
	}
	return out
}

// parseGo reads go build/vet/test output: file:line:msg for compiler and vet
// diagnostics, package and test name for go test.
func parseGo(kind, output, module string) []Failure {
	var fs []Failure
	var tests []string
	// What each failing test printed (its assertion message, a panic), so the
	// worker that has to fix it can read why it failed, not only its name.
	said := map[string][]string{}
	cur := ""
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimRight(line, "\r")
		if m := reTestFail.FindStringSubmatch(line); m != nil {
			tests = append(tests, m[1])
			cur = m[1]
		} else if cur != "" && (strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") || strings.HasPrefix(line, "panic:")) && strings.TrimSpace(line) != "" {
			said[cur] = append(said[cur], strings.TrimSpace(line))
		} else if m := rePkgFail.FindStringSubmatch(line); m != nil {
			dir := pkgDir(m[1], module)
			if len(tests) == 0 {
				fs = append(fs, Failure{Sig: kind + "|" + m[1] + "|FAIL", Dir: dir})
			}
			for _, t := range tests {
				fs = append(fs, Failure{Sig: kind + "|" + m[1] + "|" + t, Dir: dir, Text: evidenceText(said[t])})
			}
			tests, cur = nil, ""
			said = map[string][]string{}
		} else if m := reFileLine.FindStringSubmatch(line); m != nil {
			file := path.Clean(m[1])
			fs = append(fs, Failure{Sig: fmt.Sprintf("%s|%s:%s: %s", kind, file, m[2], normalise(m[3])), File: file})
		}
	}
	return fs
}

// evidenceText joins the lines a failing test printed, capped so one noisy test
// cannot swamp the repair prompt.
func evidenceText(lines []string) string {
	const maxLines, maxBytes = 8, 800
	if len(lines) > maxLines {
		lines = lines[:maxLines]
	}
	out := strings.Join(lines, "\n")
	if len(out) > maxBytes {
		out = out[:maxBytes] + " ..."
	}
	return out
}

// pkgDir maps an import path to its directory relative to the module root.
func pkgDir(pkg, module string) string {
	switch {
	case module != "" && pkg == module:
		return "."
	case module != "" && strings.HasPrefix(pkg, module+"/"):
		return strings.TrimPrefix(pkg, module+"/")
	}
	return pkg
}

func genericFailure(name string, r CommandResult) Failure {
	var first, firstErr string
	for _, line := range strings.Split(r.Output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if first == "" {
			first = line
		}
		if reErrWord.MatchString(line) {
			firstErr = line
			break
		}
	}
	text := firstErr
	if text == "" {
		text = first
	}
	if text == "" {
		text = fmt.Sprintf("exit %d", r.ExitCode)
	}
	return Failure{Sig: name + "|" + normalise(text), Text: text}
}

// touches reports whether the failure points at one of the touched files. An
// entry covers itself and, as a directory, everything beneath it; "." covers
// everything.
func (f Failure) touches(touched []string) bool {
	for _, t := range touched {
		t = path.Clean(filepath.ToSlash(t))
		switch {
		case t == ".":
			return true
		case f.File != "" && (f.File == t || strings.HasPrefix(f.File, t+"/")):
			return true
		case f.Dir != "" && (path.Dir(t) == f.Dir || f.Dir == t || strings.HasPrefix(f.Dir, t+"/")):
			return true
		case f.File == "" && f.Dir == "" && f.Text != "" && strings.Contains(f.Text, t):
			return true
		}
	}
	return false
}

// Baseline is the set of feedback failures present on the base commit, before
// any attempt touched anything (V5).
type Baseline struct {
	Commit   string    `json:"commit"`
	Failures []Failure `json:"failures"`
}

func (b Baseline) has(sig string) bool {
	for _, f := range b.Failures {
		if f.Sig == sig {
			return true
		}
	}
	return false
}

// RecordBaseline runs the feedback commands in cfg.Spec's worktree, which the
// caller has checked out at commit, and records every failure. The engine calls
// it at run start and again after each merged pass.
func RecordBaseline(ctx context.Context, cfg FeedbackConfig, cmds []Argv, commit string) (Baseline, error) {
	results, err := RunFeedback(ctx, cfg, cmds)
	if err != nil {
		return Baseline{}, err
	}
	b := Baseline{Commit: commit, Failures: []Failure{}}
	for _, r := range results {
		b.Failures = append(b.Failures, r.Failures...)
	}
	return b, nil
}

// Compare adds the feedback results to rep and finalizes it. A failure absent
// from the baseline is a new failure; one present in the baseline fails only
// when it points at a touched file, and is otherwise a warning. Each command
// gets a check that passes unless it produced a failing failure. Call it once
// per report: it appends to rep, so a second call would duplicate entries.
func Compare(rep *VerifyReport, results []CommandResult, base Baseline, touched []string) {
	for _, r := range results {
		check := Check{Name: CheckFeedbackPrefix + r.Argv.String(), Pass: true}
		for _, f := range r.Failures {
			switch {
			case !base.has(f.Sig):
				rep.NewFailures = append(rep.NewFailures, f.Sig)
				check.Pass = false
				check.Details = append(check.Details, f.Sig)
				if f.Text != "" {
					rep.Evidence = append(rep.Evidence, Evidence{Sig: f.Sig, Text: f.Text})
				}
			case f.touches(touched):
				rep.NewFailures = append(rep.NewFailures, f.Sig)
				rep.BaselineFailures = append(rep.BaselineFailures, f.Sig)
				check.Pass = false
				check.Details = append(check.Details, "baseline failure in a touched file: "+f.Sig)
			default:
				rep.BaselineFailures = append(rep.BaselineFailures, f.Sig)
				rep.Warnings = append(rep.Warnings, "baseline failure (existed before this attempt): "+f.Sig)
			}
		}
		rep.Checks = append(rep.Checks, check)
	}
	rep.Finalize()
}

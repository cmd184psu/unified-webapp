package verify

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"cmd184psu/unified-webapp/internal/agentflow/sandbox"
)

type fb struct {
	repo, gitDir, root string
	n                  int
}

func newFB(t *testing.T) *fb {
	t.Helper()
	if _, err := exec.LookPath("bwrap"); err != nil && runtime.GOOS != "darwin" {
		t.Fatalf("bwrap not installed; it is a fixed host dependency (D16): %v", err)
	}
	root := t.TempDir()
	f := &fb{repo: filepath.Join(root, "repo"), root: root}
	f.gitDir = filepath.Join(f.repo, ".git")
	for name, body := range map[string]string{
		"go.mod":       "module example.com/m\n\ngo 1.21\n",
		"a/a.go":       "package a\n\nfunc One() int { return 1 }\n",
		"a/a_test.go":  "package a\n\nimport \"testing\"\n\nfunc TestOld(t *testing.T) { t.Fatal(\"old failure\") }\n\nfunc TestFine(t *testing.T) {}\n",
		"b/b.go":       "package b\n\nfunc Two() int { return 2 }\n",
		"b/b_test.go":  "package b\n\nimport \"testing\"\n\nfunc TestTwo(t *testing.T) { if Two() != 2 { t.Fatal(\"bad\") } }\n",
		"docs/note.md": "x\n",
	} {
		write(t, f.repo, name, body)
	}
	run(t, f.repo, "init", "-q", "-b", "main")
	run(t, f.repo, "add", "-A")
	run(t, f.repo, "commit", "-q", "-m", "base")
	return f
}

// worktree adds a fresh worktree at HEAD and returns a config for it.
func (f *fb) worktree(t *testing.T, timeout time.Duration) FeedbackConfig {
	t.Helper()
	f.n++
	name := "wt" + string(rune('0'+f.n))
	wt := filepath.Join(f.root, name)
	home := filepath.Join(f.root, name+"-home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	run(t, f.repo, "worktree", "add", "-q", "-b", name, wt)
	return FeedbackConfig{Timeout: timeout, Spec: sandbox.Spec{
		Worktree: wt, RepoGitDir: f.gitDir, WorktreeGitDir: filepath.Join(f.gitDir, "worktrees", name),
		StageHome: home, Net: false, Env: map[string]string{"GOPROXY": "off"},
	}}
}

func goTest() []Argv { return []Argv{{"go", "test", "./..."}} }

// AC-V4: a failure that existed before the run does not fail an unrelated task.
func TestBaselineFailureDoesNotFailUnrelatedTask(t *testing.T) {
	f := newFB(t)
	ctx := context.Background()
	base, err := RecordBaseline(ctx, f.worktree(t, time.Minute), goTest(), "base")
	if err != nil {
		t.Fatal(err)
	}
	if len(base.Failures) != 1 || base.Failures[0].Sig != "go test|example.com/m/a|TestOld" {
		t.Fatalf("baseline failures: %+v", base.Failures)
	}

	cfg := f.worktree(t, time.Minute)
	write(t, cfg.Spec.Worktree, "b/b.go", "package b\n\nfunc Two() int { return 2 }\n\n// edited\n")
	results, err := RunFeedback(ctx, cfg, goTest())
	if err != nil {
		t.Fatal(err)
	}
	rep := structuralReport(t, cfg, "b/b.go")
	Compare(&rep, results, base, []string{"b/b.go"})
	if !rep.Pass || len(rep.NewFailures) != 0 {
		t.Fatalf("unrelated task must pass: %+v", rep)
	}
	if len(rep.BaselineFailures) != 1 || len(rep.Warnings) != 1 || !strings.Contains(rep.Warnings[0], "TestOld") {
		t.Fatalf("want one baseline warning naming TestOld: %+v", rep)
	}
}

func TestBaselineFailureInTouchedFileFails(t *testing.T) {
	f := newFB(t)
	ctx := context.Background()
	base, err := RecordBaseline(ctx, f.worktree(t, time.Minute), goTest(), "base")
	if err != nil {
		t.Fatal(err)
	}
	cfg := f.worktree(t, time.Minute)
	write(t, cfg.Spec.Worktree, "a/a.go", "package a\n\nfunc One() int { return 11 }\n")
	results, err := RunFeedback(ctx, cfg, goTest())
	if err != nil {
		t.Fatal(err)
	}
	rep := structuralReport(t, cfg, "a/a.go")
	Compare(&rep, results, base, []string{"a/a.go"})
	if rep.Pass || len(rep.NewFailures) != 1 || rep.NewFailures[0] != "go test|example.com/m/a|TestOld" {
		t.Fatalf("touched-file baseline failure must fail: %+v", rep)
	}
}

func TestNewFailureFailsAndCompileErrorHasFileLine(t *testing.T) {
	f := newFB(t)
	ctx := context.Background()
	base, err := RecordBaseline(ctx, f.worktree(t, time.Minute), goTest(), "base")
	if err != nil {
		t.Fatal(err)
	}
	cfg := f.worktree(t, time.Minute)
	write(t, cfg.Spec.Worktree, "b/b.go", "package b\n\nfunc Two() int { return 3 }\n")
	results, err := RunFeedback(ctx, cfg, goTest())
	if err != nil {
		t.Fatal(err)
	}
	rep := structuralReport(t, cfg, "b/b.go")
	Compare(&rep, results, base, []string{"b/b.go"})
	if rep.Pass || !reflect.DeepEqual(rep.NewFailures, []string{"go test|example.com/m/b|TestTwo"}) {
		t.Fatalf("want new TestTwo failure: %+v", rep)
	}

	write(t, cfg.Spec.Worktree, "b/b.go", "package b\n\nfunc Two() int { return undefinedName }\n")
	results, err = RunFeedback(ctx, FeedbackConfig{Spec: cfg.Spec}, []Argv{{"go", "build", "./..."}})
	if err != nil {
		t.Fatal(err)
	}
	if len(results[0].Failures) != 1 || results[0].Failures[0].Sig != "go build|b/b.go:3: undefined: undefinedName" {
		t.Fatalf("build failures: %+v", results[0].Failures)
	}
}

func structuralReport(t *testing.T, cfg FeedbackConfig, touched ...string) VerifyReport {
	t.Helper()
	rep, err := Structural(context.Background(), Input{Worktree: cfg.Spec.Worktree,
		GitDir: cfg.Spec.WorktreeGitDir, BaseCommit: "HEAD", FilesTouched: touched})
	if err != nil {
		t.Fatal(err)
	}
	return rep
}

func TestRealTrueFalseAndReadOnlyWorktreeAndStageHomeCache(t *testing.T) {
	f := newFB(t)
	cfg := f.worktree(t, time.Minute)
	res, err := RunFeedback(context.Background(), cfg, []Argv{
		{"true"}, {"false"}, {"touch", "x.txt"}, {"go", "build", "./..."},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res[0].Failed() || !res[1].Failed() || len(res[1].Failures) != 1 {
		t.Fatalf("true/false: %+v %+v", res[0], res[1])
	}
	if !res[2].Failed() {
		t.Fatalf("worktree must be read-only: %+v", res[2])
	}
	if _, err := os.Stat(filepath.Join(cfg.Spec.Worktree, "x.txt")); err == nil {
		t.Fatal("feedback command wrote into the worktree")
	}
	if res[3].Failed() {
		t.Fatalf("go build: %s", res[3].Output)
	}
	if _, err := os.Stat(filepath.Join(cfg.Spec.StageHome, ".cache", "go-build")); err != nil {
		t.Fatalf("go cache must live in the stage home: %v", err)
	}
}

func TestTimeoutKillsCommandAndOutputIsBounded(t *testing.T) {
	f := newFB(t)
	cfg := f.worktree(t, 2*time.Second)
	start := time.Now()
	res, err := RunFeedback(context.Background(), cfg, []Argv{{"sleep", "300"}, {"yes", "padding padding padding"}})
	if err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > 20*time.Second {
		t.Fatalf("timeout not enforced, took %v", d)
	}
	if !res[0].TimedOut || !res[1].TimedOut {
		t.Fatalf("want both timed out: %+v", res)
	}
	if res[0].Failures[0].Sig != "sleep 300|timeout" {
		t.Fatalf("timeout sig: %+v", res[0].Failures)
	}
	if !res[1].Truncated || len(res[1].Output) > maxOutputBytes+64 {
		t.Fatalf("output must be capped near %d: truncated=%v len=%d", maxOutputBytes, res[1].Truncated, len(res[1].Output))
	}
}

func TestCommandsRunConcurrently(t *testing.T) {
	f := newFB(t)
	cfg := f.worktree(t, time.Minute)
	start := time.Now()
	res, err := RunFeedback(context.Background(), cfg, []Argv{{"sleep", "2"}, {"sleep", "2"}, {"sleep", "2"}})
	if err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > 3500*time.Millisecond {
		t.Fatalf("commands ran serially: %v", d)
	}
	for _, r := range res {
		if r.Failed() {
			t.Fatalf("sleep failed: %+v", r)
		}
	}
}

func TestParentCancelIsAnError(t *testing.T) {
	f := newFB(t)
	cfg := f.worktree(t, time.Minute)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := RunFeedback(ctx, cfg, []Argv{{"sleep", "300"}}); err == nil {
		t.Fatal("cancelled parent context must be an error, not a failure result")
	}
}

func TestFingerprintGoOutputIsNormalised(t *testing.T) {
	out := "--- FAIL: TestA (0.01s)\n    a_test.go:9: boom\n--- FAIL: TestA/sub (0.00s)\nFAIL\nFAIL\texample.com/m/a\t0.013s\n" +
		"# example.com/m/c [example.com/m/c.test]\nc/c.go:4:9: undefined: x\nFAIL\texample.com/m/c [build failed]\nFAIL\n"
	r := CommandResult{Argv: Argv{"go", "test", "./..."}, ExitCode: 1, Output: out}
	var sigs []string
	for _, f := range fingerprint(r, "example.com/m") {
		sigs = append(sigs, f.Sig)
	}
	want := []string{"go test|example.com/m/a|TestA", "go test|example.com/m/a|TestA/sub",
		"go test|c/c.go:4: undefined: x", "go test|example.com/m/c|FAIL"}
	if !reflect.DeepEqual(sigs, want) {
		t.Fatalf("sigs\n got %q\nwant %q", sigs, want)
	}
	r.Output = strings.ReplaceAll(out, "0.013s", "7.5s")
	r.Output = strings.ReplaceAll(r.Output, "(0.01s)", "(3.2s)")
	again := fingerprint(r, "example.com/m")
	for i, f := range again {
		if f.Sig != want[i] {
			t.Fatalf("signature changed with timings: %q", f.Sig)
		}
	}
}

func TestFingerprintVetAndGeneric(t *testing.T) {
	vet := CommandResult{Argv: Argv{"go", "vet", "./..."}, ExitCode: 1,
		Output: "# example.com/m/a\na/a.go:7:2: unreachable code\nvet: a/b.go:3:1: undefined: q\n"}
	fs := fingerprint(vet, "example.com/m")
	if len(fs) != 2 || fs[0].Sig != "go vet|a/a.go:7: unreachable code" || fs[0].File != "a/a.go" || fs[1].File != "a/b.go" {
		t.Fatalf("vet: %+v", fs)
	}
	gen := CommandResult{Argv: Argv{"python", "-m", "pytest", "-q"}, ExitCode: 1,
		Output: "collected 3 items\n\nFAILED tests/test_x.py::test_a - assert 1 == 2 in 0.12s\n"}
	fs = fingerprint(gen, "")
	if len(fs) != 1 || fs[0].Sig != "python -m pytest -q|FAILED tests/test_x.py::test_a - assert 1 == 2 in <t>" {
		t.Fatalf("generic: %+v", fs)
	}
	if !fs[0].touches([]string{"tests/test_x.py"}) || fs[0].touches([]string{"src/other.py"}) {
		t.Fatal("generic failure must match touched files by name in its line")
	}
	if got := fingerprint(CommandResult{Argv: Argv{"false"}, ExitCode: 1}, ""); len(got) != 1 || got[0].Sig != "false|exit 1" {
		t.Fatalf("silent failure: %+v", got)
	}
	if got := fingerprint(CommandResult{Argv: Argv{"true"}}, ""); got != nil {
		t.Fatalf("success has failures: %+v", got)
	}
}

func TestCompareBaselineSemantics(t *testing.T) {
	root := Failure{Sig: "go test|example.com/m|TestRoot", Dir: "."}
	sub := Failure{Sig: "go test|example.com/m/a|TestOld", Dir: "a"}
	file := Failure{Sig: "go vet|a/x.go:1: m", File: "a/x.go"}
	cases := []struct {
		name    string
		touched []string
		f       Failure
		fails   bool
	}{
		{"root pkg, root file touched", []string{"main.go"}, root, true},
		{"root pkg, subdir file touched", []string{"a/a.go"}, root, false},
		{"sub pkg, file in it", []string{"a/a.go"}, sub, true},
		{"sub pkg, directory entry above", []string{"a"}, sub, true},
		{"sub pkg, unrelated", []string{"b/b.go"}, sub, false},
		{"file diag, same file", []string{"a/x.go"}, file, true},
		{"file diag, other file", []string{"a/y.go"}, file, false},
		{"dot covers all", []string{"."}, sub, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rep := VerifyReport{Checks: []Check{{Name: "x", Pass: true}}}
			res := []CommandResult{{Argv: Argv{"go", "test"}, ExitCode: 1, Failures: []Failure{c.f}}}
			Compare(&rep, res, Baseline{Failures: []Failure{c.f}}, c.touched)
			if rep.Pass == c.fails {
				t.Fatalf("pass=%v want fails=%v: %+v", rep.Pass, c.fails, rep)
			}
		})
	}
}

func TestParseArgvAndDetect(t *testing.T) {
	a, err := ParseArgv("  go   test ./... ")
	if err != nil || !reflect.DeepEqual(a, Argv{"go", "test", "./..."}) {
		t.Fatalf("ParseArgv: %v %v", a, err)
	}
	if _, err := ParseArgv("   "); err == nil {
		t.Fatal("blank command must be rejected")
	}
	cases := []struct {
		name  string
		files map[string]string
		want  []Argv
	}{
		{"go", map[string]string{"go.mod": "module x\n"},
			[]Argv{{"go", "build", "-o", "/dev/null", "./..."}, {"go", "vet", "./..."}, {"go", "test", "./..."}}},
		{"python", map[string]string{"pyproject.toml": "[project]\n"}, []Argv{{"python", "-m", "pytest", "-q"}}},
		{"npm with test script", map[string]string{"package.json": `{"scripts":{"test":"jest"}}`}, []Argv{{"npm", "test", "--silent"}}},
		{"npm without test script", map[string]string{"package.json": `{"scripts":{"build":"tsc"}}`}, nil},
		{"npm invalid json", map[string]string{"package.json": `{`}, nil},
		{"go and npm", map[string]string{"go.mod": "module x\n", "package.json": `{"scripts":{"test":"t"}}`},
			[]Argv{{"go", "build", "-o", "/dev/null", "./..."}, {"go", "vet", "./..."}, {"go", "test", "./..."}, {"npm", "test", "--silent"}}},
		{"nothing", map[string]string{"README.md": "x"}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			for n, b := range c.files {
				write(t, dir, n, b)
			}
			if got := Detect(dir); !reflect.DeepEqual(got, c.want) {
				t.Fatalf("Detect = %v want %v", got, c.want)
			}
		})
	}
	// Stored commands win over detection.
	dir := t.TempDir()
	write(t, dir, "go.mod", "module x\n")
	got, err := Commands([]string{"make check"}, dir)
	if err != nil || !reflect.DeepEqual(got, []Argv{{"make", "check"}}) {
		t.Fatalf("Commands: %v %v", got, err)
	}
	if got, _ := Commands(nil, dir); len(got) != 3 {
		t.Fatalf("unset must auto-detect: %v", got)
	}
}

// AC-V2: nothing in verify invokes a shell, and only the sandbox builds
// processes (the one exception is the structural stage's fixed `git` argv).
func TestNoShellInVerify(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi os.FileInfo) bool { return !strings.HasSuffix(fi.Name(), "_test.go") }, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, pkg := range pkgs {
		for name, file := range pkg.Files {
			// structural.go passes `-c user.name=...` to git, which is git config,
			// not a shell flag; its exec calls are still checked below.
			shellLits := name != "structural.go"
			ast.Inspect(file, func(n ast.Node) bool {
				switch x := n.(type) {
				case *ast.BasicLit:
					if x.Kind == token.STRING && shellLits {
						switch strings.Trim(x.Value, "\"`") {
						case "bash", "sh", "-c", "/bin/sh", "/bin/bash":
							t.Errorf("%s: shell literal %s", fset.Position(x.Pos()), x.Value)
						}
					}
				case *ast.CallExpr:
					sel, ok := x.Fun.(*ast.SelectorExpr)
					if !ok {
						return true
					}
					if id, ok := sel.X.(*ast.Ident); ok && id.Name == "exec" && strings.HasPrefix(sel.Sel.Name, "Command") {
						if lit, ok := x.Args[boolInt(sel.Sel.Name == "CommandContext")].(*ast.BasicLit); !ok || lit.Value != `"git"` {
							t.Errorf("%s: exec.%s outside sandbox with a non-git program", fset.Position(x.Pos()), sel.Sel.Name)
						}
					}
				}
				return true
			})
		}
	}
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// A failure whose lines are only in the tail of a huge output must still be
// seen, or it would be demoted to a baseline warning.
func TestFailureOnlyInOutputTailIsKept(t *testing.T) {
	b := &limitedBuffer{max: maxOutputBytes}
	filler := strings.Repeat("log line padding padding padding\n", 20000)
	b.Write([]byte(filler))
	b.Write([]byte("--- FAIL: TestLate (0.01s)\nFAIL\nFAIL\texample.com/m/z\t1.0s\n"))
	out := b.String()
	if !b.truncated || !strings.Contains(out, "bytes elided") || len(out) > maxOutputBytes+64 {
		t.Fatalf("want bounded head+tail output, truncated=%v len=%d", b.truncated, len(out))
	}
	fs := fingerprint(CommandResult{Argv: Argv{"go", "test", "./..."}, ExitCode: 1, Output: out}, "example.com/m")
	if len(fs) != 1 || fs[0].Sig != "go test|example.com/m/z|TestLate" {
		t.Fatalf("tail failure lost: %+v", fs)
	}
	rep := VerifyReport{Checks: []Check{{Name: "x", Pass: true}}}
	Compare(&rep, []CommandResult{{Argv: Argv{"go", "test"}, ExitCode: 1, Failures: fs}}, Baseline{}, nil)
	if rep.Pass {
		t.Fatal("new tail failure must fail verify")
	}
}

// H11: a failing test's own output (the assertion message) travels with the
// failure as evidence, and never changes its signature, so fingerprints stay
// stable and the worker that repairs it can read why it failed.
func TestEvidenceCarriesWhatAFailingTestSaid(t *testing.T) {
	out := "--- FAIL: TestHistory (0.00s)\n    server_test.go:80: expected 2 history entries got 3\n    server_test.go:81: and a second line\n" +
		"--- FAIL: TestOther (0.00s)\npanic: runtime error: index out of range [0]\nFAIL\nFAIL\texample.com/m\t0.005s\n"
	r := CommandResult{Argv: Argv{"go", "test", "./..."}, ExitCode: 1, Output: out}
	fs := fingerprint(r, "example.com/m")
	if len(fs) != 2 || fs[0].Sig != "go test|example.com/m|TestHistory" || fs[1].Sig != "go test|example.com/m|TestOther" {
		t.Fatalf("failures = %+v", fs)
	}
	if !strings.Contains(fs[0].Text, "expected 2 history entries got 3") || !strings.Contains(fs[0].Text, "and a second line") {
		t.Fatalf("test text = %q", fs[0].Text)
	}
	if !strings.Contains(fs[1].Text, "index out of range") {
		t.Fatalf("panic text = %q", fs[1].Text)
	}
	r.Failures = fs
	var rep VerifyReport
	Compare(&rep, []CommandResult{r}, Baseline{}, nil)
	if len(rep.Evidence) != 2 || rep.Evidence[0].Sig != fs[0].Sig || !strings.Contains(rep.Evidence[0].Text, "got 3") {
		t.Fatalf("evidence = %+v", rep.Evidence)
	}
	for _, c := range rep.Checks {
		for _, d := range c.Details {
			if strings.Contains(d, "got 3") {
				t.Fatalf("the message leaked into a check detail, which feeds the fingerprint: %q", d)
			}
		}
	}
	long := make([]string, 30)
	for i := range long {
		long[i] = strings.Repeat("x", 200)
	}
	if got := evidenceText(long); len(got) > 810 || strings.Count(got, "\n") > 7 {
		t.Fatalf("evidence not capped: %d bytes", len(got))
	}
}

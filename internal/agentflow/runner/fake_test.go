package runner

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func writePrompt(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "prompt.md")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestFakeRecordsRequestsAndPromptContents(t *testing.T) {
	big := strings.Repeat("x", 600*1024)
	p := writePrompt(t, big)
	f := NewFake().Script("worker", Step{Reply: "done", Usage: &Usage{TotalTokens: 7}, SessionID: "s1"})
	req := Request{Role: "worker", Backend: Backend{Name: "a", Host: "h", APIType: "openai"}, Model: "m", Workdir: "/w", StageDir: "/s", PromptPath: p, Message: "go"}
	res, err := f.Run(context.Background(), req)
	if err != nil || res.Final != "done" || res.Usage.TotalTokens != 7 || res.SessionID != "s1" {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	calls := f.Calls()
	if len(calls) != 1 || calls[0].Prompt != big || calls[0].PromptErr != nil {
		t.Fatalf("prompt not recorded: %d calls", len(calls))
	}
	if calls[0].Request.Backend.Host != "h" || calls[0].Request.Model != "m" || calls[0].Request.Workdir != "/w" {
		t.Fatalf("request not recorded: %+v", calls[0].Request)
	}
	if v, _ := f.Version(context.Background()); v != "fake" {
		t.Fatalf("version %q", v)
	}
}

func TestFakeScriptPerRoleAndCall(t *testing.T) {
	f := NewFake().
		Script("worker", Step{Reply: "w1"}, Step{Reply: "w2"}).
		Script("reviewer", Step{Reply: "r1"})
	for _, tc := range []struct{ role, want string }{{"worker", "w1"}, {"reviewer", "r1"}, {"worker", "w2"}} {
		res, err := f.Run(context.Background(), Request{Role: tc.role})
		if err != nil || res.Final != tc.want {
			t.Fatalf("%s: %+v %v", tc.role, res, err)
		}
	}
	if _, err := f.Run(context.Background(), Request{Role: "worker"}); err == nil {
		t.Fatal("exhausted script must fail")
	}
	f.Fallback(Step{Reply: "fb"})
	if res, _ := f.Run(context.Background(), Request{Role: "worker"}); res.Final != "fb" {
		t.Fatalf("fallback: %+v", res)
	}
	if n := len(f.CallsFor("worker")); n != 4 {
		t.Fatalf("worker calls %d", n)
	}
}

func TestFakeErrorClasses(t *testing.T) {
	for _, c := range []ErrClass{ErrRateLimit, ErrUnreachable, ErrCrash, ErrTimeout} {
		f := NewFake().Script("w", Step{ErrClass: c, Reply: "partial"})
		res, err := f.Run(context.Background(), Request{Role: "w"})
		var re *Error
		if !errors.As(err, &re) || re.Class != c || res.ErrClass != c || res.Final != "partial" {
			t.Fatalf("%s: res=%+v err=%v", c, res, err)
		}
		if !c.Valid() {
			t.Fatalf("%s not valid", c)
		}
	}
	if ErrClass("bogus").Valid() {
		t.Fatal("bogus class valid")
	}
}

func TestFakeIncompleteAndSessionContinuation(t *testing.T) {
	f := NewFake().Script("rev",
		Step{Incomplete: true, SessionID: "sess"},
		Step{Reply: "fixed"})
	r1, err := f.Run(context.Background(), Request{Role: "rev"})
	if err != nil || !r1.Incomplete || r1.Final != "" || r1.SessionID != "sess" {
		t.Fatalf("%+v %v", r1, err)
	}
	if _, err := f.Run(context.Background(), Request{Role: "rev", SessionID: r1.SessionID}); err != nil {
		t.Fatal(err)
	}
	if got := f.Calls()[1].Request.SessionID; got != "sess" {
		t.Fatalf("session id %q", got)
	}
}

func TestFakeBlockUntilCtxCancel(t *testing.T) {
	f := NewFake().Script("w", Step{Block: true})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := f.Run(ctx, Request{Role: "w"}); done <- err }()
	select {
	case <-done:
		t.Fatal("returned before cancel")
	case <-time.After(50 * time.Millisecond):
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("did not unblock")
	}
}

func TestFakeBlockDeadlineIsTimeoutClass(t *testing.T) {
	f := NewFake().Script("w", Step{Block: true})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	res, err := f.Run(ctx, Request{Role: "w"})
	var re *Error
	if !errors.As(err, &re) || re.Class != ErrTimeout || res.ErrClass != ErrTimeout {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

func TestFakeDelayRespectsCtx(t *testing.T) {
	f := NewFake().Script("w", Step{Delay: time.Hour}, Step{Delay: 10 * time.Millisecond, Reply: "ok"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := f.Run(ctx, Request{Role: "w"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("err %v", err)
	}
	if res, err := f.Run(context.Background(), Request{Role: "w"}); err != nil || res.Final != "ok" {
		t.Fatalf("%+v %v", res, err)
	}
}

func TestFakePanicAndEdit(t *testing.T) {
	dir := t.TempDir()
	f := NewFake().Script("w",
		Step{Edit: func(r Request) error { return os.WriteFile(filepath.Join(r.Workdir, "a.txt"), []byte("hi"), 0o600) }, Reply: "edited"},
		Step{Panic: "boom"})
	if res, err := f.Run(context.Background(), Request{Role: "w", Workdir: dir}); err != nil || res.Final != "edited" {
		t.Fatalf("%+v %v", res, err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "a.txt")); string(b) != "hi" {
		t.Fatalf("edit not applied: %q", b)
	}
	defer func() {
		if r := recover(); r != "boom" {
			t.Fatalf("recover %v", r)
		}
		if n := len(f.Calls()); n != 2 {
			t.Fatalf("panicking call not recorded: %d", n)
		}
	}()
	_, _ = f.Run(context.Background(), Request{Role: "w"})
}

func TestFakeStreamsEventsToOnEvent(t *testing.T) {
	evs := []Event{{Type: "text", Text: "a"}, {Type: "text", Text: "b"}}
	f := NewFake().Script("w", Step{Events: evs, Reply: "b"})
	var got []string
	res, _ := f.Run(context.Background(), Request{Role: "w", OnEvent: func(e Event) { got = append(got, e.Text) }})
	if strings.Join(got, "") != "ab" || len(res.Events) != 2 {
		t.Fatalf("got %v res %+v", got, res)
	}
}

func TestFakeConcurrentUse(t *testing.T) {
	f := NewFake()
	steps := make([]Step, 50)
	for i := range steps {
		steps[i] = Step{Reply: strconv.Itoa(i)}
	}
	f.Script("w", steps...)
	var mu sync.Mutex
	seen := map[string]int{}
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := f.Run(context.Background(), Request{Role: "w"})
			if err != nil {
				t.Error(err)
				return
			}
			mu.Lock()
			seen[res.Final]++
			mu.Unlock()
			_ = f.Calls()
		}()
	}
	wg.Wait()
	if len(seen) != 50 {
		t.Fatalf("distinct %d", len(seen))
	}
	for k, n := range seen {
		if n != 1 {
			t.Fatalf("step %s consumed %d times", k, n)
		}
	}
	if n := len(f.Calls()); n != 50 {
		t.Fatalf("calls %d", n)
	}
}

func TestFakeMatchScriptsPerTask(t *testing.T) {
	isTask := func(id string) func(Request) bool {
		return func(r Request) bool { return r.Workdir == id }
	}
	f := NewFake().Script("w",
		Step{Reply: "plain"},
		Step{Reply: "A", Match: isTask("a")},
		Step{Reply: "B", Match: isTask("b")})
	for _, tc := range []struct{ wd, want string }{{"b", "B"}, {"a", "A"}, {"c", "plain"}} {
		res, err := f.Run(context.Background(), Request{Role: "w", Workdir: tc.wd})
		if err != nil || res.Final != tc.want {
			t.Fatalf("%s: %+v %v", tc.wd, res, err)
		}
	}
	if _, err := f.Run(context.Background(), Request{Role: "w", Workdir: "a"}); err == nil {
		t.Fatal("matched step must be consumed once")
	}
}

func TestFakePartialOnCancel(t *testing.T) {
	f := NewFake().Script("w", Step{Partial: true, Reply: "half", Events: []Event{{Type: "text", Text: "half"}}, Block: true})
	ctx, cancel := context.WithCancel(context.Background())
	var streamed int
	done := make(chan Result, 1)
	go func() {
		res, _ := f.Run(ctx, Request{Role: "w", OnEvent: func(Event) { streamed++ }})
		done <- res
	}()
	time.Sleep(30 * time.Millisecond)
	cancel()
	res := <-done
	if res.Final != "half" || len(res.Events) != 1 || streamed != 1 {
		t.Fatalf("res=%+v streamed=%d", res, streamed)
	}
}

func TestFakeEditBeforeBlockLeavesDirtyWorkdir(t *testing.T) {
	dir := t.TempDir()
	f := NewFake().Script("w", Step{
		Block:           true,
		EditBeforeBlock: func(r Request) error { return os.WriteFile(filepath.Join(r.Workdir, "dirty"), []byte("x"), 0o600) },
	})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err := f.Run(ctx, Request{Role: "w", Workdir: dir})
	var re *Error
	if !errors.As(err, &re) || re.Class != ErrTimeout {
		t.Fatalf("err %v", err)
	}
	if _, serr := os.Stat(filepath.Join(dir, "dirty")); serr != nil {
		t.Fatal("workdir not dirty")
	}
}

func TestFakeEditFailureIsCrash(t *testing.T) {
	cause := errors.New("disk")
	f := NewFake().Script("w", Step{Edit: func(Request) error { return cause }})
	res, err := f.Run(context.Background(), Request{Role: "w"})
	var re *Error
	if !errors.As(err, &re) || re.Class != ErrCrash || res.ErrClass != ErrCrash || !errors.Is(err, cause) {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

func TestFakeErrWithoutClassReturnedAsIs(t *testing.T) {
	want := errors.New("plain")
	f := NewFake().Script("w", Step{Err: want})
	_, err := f.Run(context.Background(), Request{Role: "w"})
	var re *Error
	if err != want || errors.As(err, &re) {
		t.Fatalf("err %v", err)
	}
}

func TestErrorUnwrapsCause(t *testing.T) {
	cause := errors.New("429 body")
	f := NewFake().Script("w", Step{ErrClass: ErrRateLimit, Err: cause})
	_, err := f.Run(context.Background(), Request{Role: "w"})
	if !errors.Is(err, cause) {
		t.Fatalf("cause lost: %v", err)
	}
}

func TestFakeCallsDoNotAlias(t *testing.T) {
	env := []string{"A=1"}
	f := NewFake().Fallback(Step{Events: []Event{{Text: "e"}}})
	res, _ := f.Run(context.Background(), Request{Role: "w", Env: env})
	env[0] = "A=2"
	res.Events[0].Text = "mutated"
	f.Calls()[0].Request.Env[0] = "A=3"
	if got := f.Calls()[0].Request.Env[0]; got != "A=1" {
		t.Fatalf("env aliased: %s", got)
	}
	res2, _ := f.Run(context.Background(), Request{Role: "w"})
	if res2.Events[0].Text != "e" {
		t.Fatal("events aliased")
	}
}

// TestNoOllamaHostInAgentflowPackages asserts MB1: no engine, api, store,
// verify or gitsvc package references the OLLAMA_HOST string.
func TestNoOllamaHostInAgentflowPackages(t *testing.T) {
	needle := "OLLAMA" + "_HOST"
	scanned := 0
	for _, pkg := range []string{"engine", "api", "store", "verify", "gitsvc"} {
		dir := filepath.Join("..", pkg)
		if _, err := os.Stat(dir); err != nil {
			continue
		}
		err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
				return err
			}
			f, perr := parser.ParseFile(token.NewFileSet(), path, nil, 0)
			if perr != nil {
				return perr
			}
			scanned++
			ast.Inspect(f, func(n ast.Node) bool {
				if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING && strings.Contains(lit.Value, needle) {
					t.Errorf("%s references %s", path, needle)
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if scanned == 0 {
		t.Fatal("scanned no files")
	}
}

// TestInterfaceIsRunnerAgnostic asserts the package names no concrete agent.
func TestInterfaceIsRunnerAgnostic(t *testing.T) {
	for _, name := range []string{"runner.go", "fake.go"} {
		b, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(strings.ToLower(string(b)), "opencode") {
			t.Errorf("%s mentions opencode", name)
		}
	}
}

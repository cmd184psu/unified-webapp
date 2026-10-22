package engine

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"cmd184psu/unified-webapp/internal/agentflow/runner"
)

// evalProbe is a Go test, written next to a case's files, that says whether
// the case behaves as its variant claims.
var evalProbes = map[string]string{
	"power-not-finite": `package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProbe(t *testing.T) {
	rec := httptest.NewRecorder()
	powerHandler(rec, httptest.NewRequest("POST", "/api/power", strings.NewReader(` + "`{\"a\":0,\"b\":-1}`" + `)))
	want := http.StatusOK
	if wantFixed {
		want = http.StatusBadRequest
	}
	if rec.Code != want {
		t.Fatalf("0^-1 = %d %s", rec.Code, rec.Body)
	}
}
`,
	"juxtaposed-numbers": `package main

import "testing"

func TestProbe(t *testing.T) {
	_, err := Evaluate("2 3")
	if (err != nil) != wantFixed {
		t.Fatalf("Evaluate(\"2 3\") error = %v", err)
	}
	if v, err := Evaluate("2 + 3 - 1"); err != nil || v != 4 {
		t.Fatalf("2 + 3 - 1 = %v, %v", v, err)
	}
	if _, err := Evaluate("2 +"); err == nil {
		t.Fatal("2 + must be invalid")
	}
}
`,
	"fractional-count": `package main

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProbe(t *testing.T) {
	_, err := readCount(httptest.NewRequest("POST", "/api/primes", strings.NewReader(` + "`{\"count\":2.5}`" + `)))
	if err == nil {
		t.Fatal("2.5 must be an error")
	}
	if (err.Error() == "domain error") != wantFixed {
		t.Fatalf("2.5 gives %q", err)
	}
	if n, err := readCount(httptest.NewRequest("POST", "/api/primes", strings.NewReader(` + "`{\"count\":3}`" + `))); err != nil || n != 3 {
		t.Fatalf("3 gives %d, %v", n, err)
	}
}
`,
}

// Every case builds in both variants, and its defect is a defect: the defect
// variant misbehaves as described and the fixed variant does not.
func TestH32_EvalCasesBuildAndTheDefectIsReal(t *testing.T) {
	for _, c := range evalCases {
		for _, variant := range []string{"defect", "fixed"} {
			t.Run(c.ID+"/"+variant, func(t *testing.T) {
				files := c.Defect
				if variant == "fixed" {
					files = c.Fixed
				}
				dir := t.TempDir()
				for _, f := range files {
					writeFile(t, filepath.Join(dir, f.Path), f.Content)
				}
				if needle, isPage := map[string]string{"page-through-api": "fetch('/api/add'", "page-wrong-route": "fetch('/api/evaluate'"}[c.ID]; isPage {
					html := ""
					for _, f := range files {
						if f.Path == "index.html" {
							html = f.Content
						}
					}
					if got := strings.Contains(html, needle); got != (variant == "fixed") {
						t.Fatalf("page calls the API = %v", got)
					}
				}
				probe, ok := evalProbes[c.ID]
				if ok {
					writeFile(t, filepath.Join(dir, "probe_test.go"), probe+"\nconst wantFixed = "+map[bool]string{true: "true", false: "false"}[variant == "fixed"]+"\n")
				}
				cmd := exec.Command("go", "vet", "./...")
				cmd.Dir = dir
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("go vet: %v\n%s", err, out)
				}
				if ok {
					cmd := exec.Command("go", "test", "./...")
					cmd.Dir = dir
					if out, err := cmd.CombinedOutput(); err != nil {
						t.Fatalf("go test: %v\n%s", err, out)
					}
				}
			})
		}
	}
}

func evalRun(t *testing.T, c *chainRun, opt EvalOptions) EvalView {
	t.Helper()
	if err := c.eng.CheckReviewers(context.Background(), "t", "gpu", opt); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "reviewer check ends", func() bool {
		v, err := c.eng.EvalStatus(context.Background(), "gpu")
		return err == nil && v.State == "idle" && len(v.Rows) > 0
	})
	v, _ := c.eng.EvalStatus(context.Background(), "gpu")
	return v
}

// H32: a reviewer that flags the defect and passes the fixed code scores every
// case; one that flags everything also scores a false alarm on every fixed
// variant.
func TestH32_AcceptanceLayerScoresCaughtDefectsAndFalseAlarms(t *testing.T) {
	c := newChainRun(t, nil, chainDefs()[:1])
	if err := c.eng.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	for range evalCases {
		c.fake.Script("reviewer", runner.Step{Reply: acceptGap}, runner.Step{Reply: acceptOK})
	}
	v := evalRun(t, c, EvalOptions{Models: []string{"m2"}})
	if len(v.Summary) != 1 || v.Summary[0].Caught != len(evalCases) || v.Summary[0].Defects != len(evalCases) || v.Summary[0].FalseAlarms != 0 || v.Summary[0].Fixed != len(evalCases) {
		t.Fatalf("summary = %+v", v.Summary)
	}

	for range evalCases {
		c.fake.Script("reviewer", runner.Step{Reply: acceptGap}, runner.Step{Reply: acceptGap})
	}
	v = evalRun(t, c, EvalOptions{Models: []string{"m2"}})
	if s := v.Summary[0]; s.Caught != len(evalCases) || s.FalseAlarms != len(evalCases) {
		t.Fatalf("summary = %+v", s)
	}
}

// H32: the task layer reads the verdict; an unreadable reply is counted
// apart from a miss.
func TestH32_TaskLayerReadsTheVerdict(t *testing.T) {
	c := newChainRun(t, nil, chainDefs()[:1])
	if err := c.eng.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	repair := `{"verdict":"repair","summary":"x","issues":[{"description":"the page does the sums","severity":"major"}],"repair_prompt":"call the API"}`
	c.fake.Script("reviewer", runner.Step{Reply: repair}, runner.Step{Reply: reviewPass}, runner.Step{Reply: reviewPass}, runner.Step{Reply: reviewPass}, runner.Step{Reply: reviewBad}, runner.Step{Reply: reviewBad}, runner.Step{Reply: reviewPass})
	v := evalRun(t, c, EvalOptions{Models: []string{"m2"}, Layers: []string{LayerTask}, Cases: []string{"page-through-api", "power-not-finite", "fractional-count"}})
	got := map[string]string{}
	for _, r := range v.Rows {
		got[r.Case+"/"+r.Variant] = r.Outcome
	}
	if got["page-through-api/defect"] != evalCaught || got["page-through-api/fixed"] != evalClean || got["power-not-finite/defect"] != evalMissed {
		t.Fatalf("rows = %v", got)
	}
	if got["power-not-finite/fixed"] != evalClean || got["fractional-count/defect"] != evalUnparseable {
		t.Fatalf("rows = %v", got)
	}
}

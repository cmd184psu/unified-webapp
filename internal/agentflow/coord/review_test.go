package coord

import (
	"context"
	"strings"
	"testing"
)

type fakeReviews struct {
	req ReviewRequest
	res ReviewResult
}

func (f *fakeReviews) ProjectForRepo(context.Context, string) (string, error) { return "fs3", nil }
func (f *fakeReviews) CreateReview(_ context.Context, req ReviewRequest) (ReviewResult, error) {
	f.req = req
	return f.res, nil
}

func TestAgentflowReviewRecordsAPassWithoutAChatTask(t *testing.T) {
	cfg := testConfig(t)
	cfg.ReviewMode = "agentflow"
	bus := &fakeBus{}
	reviews := &fakeReviews{res: ReviewResult{ID: 7, Verdict: "PASS", Findings: "1. No findings."}}
	c := &Coordinator{Cfg: cfg, Bus: bus, Git: &fakeGit{}, Reviews: reviews}
	r := &run{c: c, cfg: cfg, bus: bus, st: &State{}, rep: &Report{}}
	r.st.init()
	task := Task{ID: "TASK-9", Title: "work", Objective: "do it", AcceptanceCriteria: "works", Exclusions: "none",
		Assignee: "deepseek4", State: "done"}
	d := map[string]*string{}
	r.agentflowReview(context.Background(), task, d, []Msg{{Callsign: "deepseek4", Content: "DONE TASK-9 feature/x@abcdef1"}})
	if get(d, "pass") != "AF7" || get(d, "review") != "AF7" {
		t.Fatalf("delivery = %v", d)
	}
	if reviews.req.Project != "fs3" || reviews.req.Branch != "feature/x" || reviews.req.AuthorFamily != "deepseek" {
		t.Fatalf("request = %+v", reviews.req)
	}
	if len(bus.writes) != 0 {
		t.Fatalf("agentflow review created a BotChat task: %v", bus.writes)
	}
}

func TestAgentflowReviewRecordsChangesRequested(t *testing.T) {
	cfg := testConfig(t)
	cfg.ReviewMode = "agentflow"
	bus := &fakeBus{}
	reviews := &fakeReviews{res: ReviewResult{ID: 8, Verdict: "CHANGES-REQUESTED", Findings: "1. off by one"}}
	c := &Coordinator{Cfg: cfg, Bus: bus, Git: &fakeGit{}, Reviews: reviews}
	r := &run{c: c, cfg: cfg, bus: bus, st: &State{}, rep: &Report{}}
	r.st.init()
	task := Task{ID: "TASK-10", Title: "work", Assignee: "deepseek4", State: "done"}
	d := map[string]*string{}
	r.agentflowReview(context.Background(), task, d, []Msg{{Callsign: "deepseek4", Content: "DONE TASK-10 feature/y@abcdef1"}})
	if get(d, "cr") != "AF8" || get(d, "status") != "rework" {
		t.Fatalf("delivery = %v", d)
	}
	if len(bus.writes) == 0 || !strings.Contains(strings.Join(bus.writes, " "), "transition TASK-10 rework") {
		t.Fatalf("writes = %v", bus.writes)
	}
}

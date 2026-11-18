package engine

import (
	"context"
	"errors"
	"os"
	"strconv"
	"testing"

	"cmd184psu/unified-webapp/internal/agentflow/model"
	"cmd184psu/unified-webapp/internal/agentflow/runner"
	"cmd184psu/unified-webapp/internal/agentflow/store"
)

// haltedRun builds a run whose first task is stuck: it changes nothing twice,
// so verify fails with the same fingerprint at stuck_threshold 2.
func haltedRun(t *testing.T, defs []model.TaskDef) *chainRun {
	t.Helper()
	c := newChainRun(t, nil, defs)
	c.setLimits(map[string]string{"max_attempts": "2", "stuck_threshold": "2"})
	c.fake.Script("worker", runner.Step{Reply: workerOK}, runner.Step{Reply: workerOK})
	if got := c.runAttempts(2); got[1] != model.StatusStuck {
		t.Fatalf("setup: status = %s, want stuck", got[1])
	}
	return c
}

func (c *chainRun) event(subjectType string, id int64, action string) (store.Event, bool) {
	c.t.Helper()
	evs, err := c.st.ListEvents(context.Background(), subjectType, strconv.FormatInt(id, 10), 100)
	if err != nil {
		c.t.Fatal(err)
	}
	for _, ev := range evs {
		if ev.Action == action {
			return ev, true
		}
	}
	return store.Event{}, false
}

func (c *chainRun) exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func (c *chainRun) branchExists(name string) bool {
	return gitT(c.t, c.repo, "branch", "--list", name) != ""
}

// AT9, ST4, AC-QA2 (edit half): every operator action on halted work has its
// status effect and writes an events row naming the actor.
func TestC9_ActionsChangeStatusAndAreAudited(t *testing.T) {
	ctx := context.Background()
	taskBranch := func(c *chainRun) string { return "agentflow-task/" + effortName(c.effort) + "/T1" }
	rows := []struct {
		name                     string
		defs                     []model.TaskDef
		halt                     bool
		act                      func(c *chainRun) error
		subject, action          string
		subjectID                func(c *chainRun) int64
		check                    func(t *testing.T, c *chainRun)
		worktreeKept, branchKept bool
	}{
		{
			name: "retry keeps the worktree", defs: chainDefs()[:1], halt: true,
			act:     func(c *chainRun) error { return c.eng.Retry(ctx, "alice", c.tasks[0].ID, false) },
			subject: "task", action: "task.reset", subjectID: func(c *chainRun) int64 { return c.tasks[0].ID },
			check: func(t *testing.T, c *chainRun) {
				if st := c.taskStatus(); st != model.StatusReady {
					t.Fatalf("status = %s, want ready", st)
				}
				if r := c.task0().WaitingReason; r != "" {
					t.Fatalf("waiting reason = %q", r)
				}
			},
			worktreeKept: true, branchKept: true,
		},
		{
			name: "clean retry removes worktree, branch and base commit", defs: chainDefs()[:1], halt: true,
			act:     func(c *chainRun) error { return c.eng.Retry(ctx, "alice", c.tasks[0].ID, true) },
			subject: "task", action: "task.reset", subjectID: func(c *chainRun) int64 { return c.tasks[0].ID },
			check: func(t *testing.T, c *chainRun) {
				if st := c.taskStatus(); st != model.StatusReady {
					t.Fatalf("status = %s, want ready", st)
				}
				if c.exists(c.eng.baseCommitFile(c.tasks[0].ID)) {
					t.Fatal("base_commit survived a clean retry")
				}
				// The first attempt after a clean retry is a normal one again.
				c.fake.Script("worker", runner.Step{Reply: workerOK, Edit: writeWork})
				c.fake.Script("reviewer", runner.Step{Reply: reviewPass})
				if err := c.eng.Runs().RunOne(ctx, "t", c.slug); err != nil {
					t.Fatal(err)
				}
				waitFor(t, "task passed", func() bool { return c.taskStatus() == model.StatusPassed })
				if as := c.attempts(0); len(as) != 3 || as[2].Kind != model.AttemptNormal {
					t.Fatalf("attempts = %+v", as)
				}
			},
		},
		{
			name: "override unblocks the dependent", defs: chainDefs()[:2], halt: true,
			act:     func(c *chainRun) error { return c.eng.Override(ctx, "alice", c.tasks[0].ID, "fixed by hand") },
			subject: "task", action: "task.override", subjectID: func(c *chainRun) int64 { return c.tasks[0].ID },
			check: func(t *testing.T, c *chainRun) {
				set, _ := c.st.EffectiveSettings(ctx, c.slug)
				st, err := c.st.TaskStatuses(ctx, c.effort, c.eng.limitsFor(set))
				if err != nil || st["T1"] != model.StatusOverridden || st["T2"] != model.StatusReady {
					t.Fatalf("statuses = %v (%v)", st, err)
				}
			},
		},
		{
			name: "override of the last open task completes the effort", defs: chainDefs()[:1], halt: true,
			act:     func(c *chainRun) error { return c.eng.Override(ctx, "alice", c.tasks[0].ID, "fixed by hand") },
			subject: "task", action: "task.override", subjectID: func(c *chainRun) int64 { return c.tasks[0].ID },
			check: func(t *testing.T, c *chainRun) {
				if ef, err := c.st.GetEffort(ctx, c.effort); err != nil || ef.Status != model.EffortComplete {
					t.Fatalf("effort = %+v (%v)", ef, err)
				}
			},
		},
		{
			name: "edit in an approved queue returns it to draft", defs: chainDefs()[:1],
			act: func(c *chainRun) error {
				def := c.tasks[0].TaskDef
				def.Prompt = "do it differently"
				q, err := c.eng.EditTask(ctx, "alice", c.effort, def)
				if err == nil && q.Status != model.QueueDraft {
					err = errors.New("queue is " + q.Status)
				}
				return err
			},
			subject: "effort", action: "queue.edit_task", subjectID: func(c *chainRun) int64 { return c.effort },
			check: func(t *testing.T, c *chainRun) {
				if _, err := c.st.ApprovedQueueHash(ctx, c.effort); !errors.Is(err, store.ErrNotFound) {
					t.Fatalf("approved hash after edit: %v", err)
				}
			},
		},
		{
			name: "abandon removes the effort's worktrees and branches", defs: chainDefs()[:1], halt: true,
			act:     func(c *chainRun) error { return c.eng.AbandonEffort(ctx, "alice", c.effort) },
			subject: "effort", action: "effort.abandon", subjectID: func(c *chainRun) int64 { return c.effort },
			check: func(t *testing.T, c *chainRun) {
				if ef, err := c.st.GetEffort(ctx, c.effort); err != nil || ef.Status != model.EffortAbandoned {
					t.Fatalf("effort = %+v (%v)", ef, err)
				}
				if _, err := c.st.ActiveEffort(ctx, c.slug); !errors.Is(err, store.ErrNotFound) {
					t.Fatalf("active effort after abandon: %v", err)
				}
				if st := c.eng.Runs().State(c.slug); st != model.RunStopped {
					t.Fatalf("run state = %s", st)
				}
				if c.branchExists("agentflow/" + effortName(c.effort)) {
					t.Fatal("integration branch survived")
				}
				if c.exists(c.eng.deps.Git.WorktreeRoot() + "/" + c.slug + "/" + effortName(c.effort) + "/_integration") {
					t.Fatal("integration worktree survived")
				}
			},
		},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			var c *chainRun
			if r.halt {
				c = haltedRun(t, r.defs)
			} else {
				c = newChainRun(t, nil, r.defs)
			}
			wt := ""
			if r.halt {
				wt = c.attempts(0)[0].Worktree
				if !c.exists(wt) || !c.branchExists(taskBranch(c)) {
					t.Fatal("setup: halted task has no worktree or branch")
				}
			}
			if err := r.act(c); err != nil {
				t.Fatal(err)
			}
			ev, ok := c.event(r.subject, r.subjectID(c), r.action)
			if !ok || ev.Actor != "alice" {
				t.Fatalf("%s event = %+v, found %v", r.action, ev, ok)
			}
			if r.halt && r.action != "effort.abandon" && r.worktreeKept != (c.exists(wt) && c.branchExists(taskBranch(c))) {
				t.Fatalf("worktree kept = %v, want %v", c.exists(wt), r.worktreeKept)
			}
			if r.halt && r.action == "effort.abandon" && (c.exists(wt) || c.branchExists(taskBranch(c))) {
				t.Fatal("task worktree or branch survived an abandon")
			}
			r.check(t, c)
		})
	}
}

// AT9: only halted tasks can be retried or overridden.
func TestC9_ActionsRefuseTasksThatAreNotHalted(t *testing.T) {
	c := newChainRun(t, nil, chainDefs()[:1])
	ctx := context.Background()
	if err := c.eng.Retry(ctx, "alice", c.tasks[0].ID, false); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("Retry = %v, want ErrConflict", err)
	}
	if err := c.eng.Override(ctx, "alice", c.tasks[0].ID, "x"); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("Override = %v, want ErrConflict", err)
	}
}

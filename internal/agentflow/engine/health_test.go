package engine

import (
	"context"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"cmd184psu/unified-webapp/internal/agentflow/model"
	"cmd184psu/unified-webapp/internal/agentflow/runner"
)

func (c *chainRun) attemptStages(task int) []model.Stage {
	c.t.Helper()
	as := c.attempts(task)
	if len(as) == 0 {
		return nil
	}
	ss, err := c.st.ListAttemptStages(context.Background(), as[0].ID)
	if err != nil {
		c.t.Fatal(err)
	}
	return ss
}

func (c *chainRun) infraStrikes(task int) int {
	c.t.Helper()
	ins, err := c.st.TaskInputs(context.Background(), c.effort)
	if err != nil {
		c.t.Fatal(err)
	}
	return len(ins[task].InfraIDs)
}

// AC-MB1 (MB7): alpha reports itself unreachable, so it is marked down for the
// cooldown and the retried worker stage runs on beta, which the dispatcher and
// the infra retry both honor. No attempt is counted for the outage; after the
// cooldown alpha is usable again.
func TestC8_ACMB1_UnreachableBackendIsSkippedForTheCooldown(t *testing.T) {
	c := newChainRun(t, nil, chainDefs()[:1])
	c.addBackend("alpha", 1, false, false, "m1")
	c.addBackend("beta", 1, false, false, "m3")
	c.candidates("global", "", "worker", "alpha/m1", "beta/m3")
	var skew atomic.Int64
	now := func() time.Time { return time.Now().Add(time.Duration(skew.Load())) }
	c.eng.deps.Clock = now
	c.fake.Script("worker",
		runner.Step{ErrClass: runner.ErrUnreachable, Err: os.ErrDeadlineExceeded},
		runner.Step{Reply: workerOK, Edit: writeWork})
	c.fake.Script("reviewer", runner.Step{Reply: reviewPass})
	c.run()
	ctx := context.Background()
	waitFor(t, "alpha marked down", func() bool { _, down := c.eng.DownBackends()["alpha"]; return down })

	skew.Store(int64(11 * time.Second)) // past the infra backoff, inside the cooldown
	if err := c.eng.Advance(ctx); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "task passed", func() bool { return c.taskStatus() == model.StatusPassed })
	ss := c.attemptStages(0)
	if ss[0].Backend != "alpha" || ss[0].Outcome != OutcomeInfraUnreachable || ss[1].Role != "worker" || ss[1].Backend != "beta" {
		t.Fatalf("stages = %+v", ss)
	}
	if as := c.attempts(0); len(as) != 1 || as[0].N != 1 || as[0].Outcome != model.OutcomePassed {
		t.Fatalf("attempts = %+v", as)
	}
	set, err := c.st.GetSettings(ctx)
	if err != nil || set.BackendCooldown != 2*time.Minute {
		t.Fatalf("cooldown = %v (%v)", set.BackendCooldown, err)
	}
	skew.Store(int64(3 * time.Minute))
	if down := c.eng.DownBackends(); len(down) != 0 {
		t.Fatalf("still down after the cooldown: %v", down)
	}
}

// AC-BK2 (BK4): two stages are queued on alpha (its lane is registered at
// width 0). Retiring alpha takes them off its lane, closes them
// infra:rerouted and submits the same roles on beta; the attempts stay open
// (a reconcile pass does not abandon them) and nothing is counted.
func TestC8_ACBK2_RetireReroutesQueuedStages(t *testing.T) {
	c := newChainRun(t, nil, []model.TaskDef{chainDefs()[0], chainDefs()[2]})
	c.eng.deps.TestWidthZero = true // alpha and beta never start a job
	c.addBackend("alpha", 2, false, false, "m1")
	c.addBackend("beta", 2, false, false, "m3")
	c.candidates("global", "", "worker", "alpha/m1", "beta/m3")
	c.run()
	waitFor(t, "two stages queued on alpha", func() bool {
		n := 0
		for _, st := range c.openStages() {
			if st.Backend == "alpha" {
				n++
			}
		}
		return n == 2
	})
	ctx := context.Background()
	rep, err := c.eng.RetireBackend(ctx, "t", "alpha", RetireOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Rerouted) != 2 || len(rep.Warnings) != 0 {
		t.Fatalf("report = %+v", rep)
	}
	if _, err := c.eng.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		ss := c.attemptStages(i)
		if len(ss) != 2 || ss[0].Backend != "alpha" || ss[0].Outcome != OutcomeInfraRerouted ||
			ss[1].Backend != "beta" || ss[1].Role != "worker" || ss[1].State != model.StageQueued {
			t.Fatalf("task %d stages = %+v", i, ss)
		}
		if as := c.attempts(i); len(as) != 1 || as[0].Outcome != "" {
			t.Fatalf("task %d attempts = %+v", i, as)
		}
		if n := c.infraStrikes(i); n != 0 {
			t.Fatalf("task %d infra strikes = %d", i, n)
		}
	}
	if jobs, err := c.lane("agentflow-alpha").List(); err != nil || len(jobs) != 0 {
		t.Fatalf("alpha jobs = %v (%v)", jobs, err)
	}
	if jobs, err := c.lane("agentflow-beta").List(); err != nil || len(jobs) != 2 {
		t.Fatalf("beta jobs = %v (%v)", jobs, err)
	}
}

// BK4, SV3: retiring with cancel_running cancels the running stage, which ends
// infra:cancel and counts no infra strike; retiring the only worker backend
// warns, naming the running project.
func TestC8_RetireCancelRunningAndLastBackendWarning(t *testing.T) {
	c := newChainRun(t, nil, chainDefs()[:1])
	c.addBackend("alpha", 1, false, false, "m1")
	c.candidates("global", "", "worker", "alpha/m1")
	c.fake.Script("worker", runner.Step{Block: true})
	c.run()
	waitFor(t, "worker running on alpha", func() bool { return len(c.fake.CallsFor("worker")) == 1 })

	rep, err := c.eng.RetireBackend(context.Background(), "t", "alpha", RetireOptions{CancelRunning: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Canceled) != 1 || len(rep.Warnings) != 1 ||
		!strings.Contains(rep.Warnings[0], "role worker") || !strings.Contains(rep.Warnings[0], c.slug) {
		t.Fatalf("report = %+v", rep)
	}
	waitFor(t, "stage infra:cancel", func() bool {
		ss := c.attemptStages(0)
		return len(ss) == 1 && ss[0].Outcome == OutcomeInfraCancel
	})
	if n := c.infraStrikes(0); n != 0 {
		t.Fatalf("infra strikes = %d", n)
	}
}

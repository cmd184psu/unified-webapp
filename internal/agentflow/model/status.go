package model

// Task statuses (state_model.task_statuses). They are derived, never stored.
type Status string

const (
	StatusBlocked        Status = "blocked"
	StatusReady          Status = "ready"
	StatusRunning        Status = "running"
	StatusRepair         Status = "repair"
	StatusPassed         Status = "passed"
	StatusOverridden     Status = "overridden"
	StatusStuck          Status = "stuck"
	StatusExhausted      Status = "exhausted"
	StatusNeedsInput     Status = "needs_input"
	StatusNeedsAttention Status = "needs_attention"
)

// Halted reports whether s is a halted status (a human must act).
func (s Status) Halted() bool {
	switch s {
	case StatusStuck, StatusExhausted, StatusNeedsInput, StatusNeedsAttention:
		return true
	}
	return false
}

// Done reports whether s satisfies a dependency.
func (s Status) Done() bool { return s == StatusPassed || s == StatusOverridden }

// Limits are the effective limits for a project. MaxPolicyViolations <= 0
// disables the policy-violation halt.
type Limits struct {
	MaxAttempts         int
	StuckThreshold      int
	MaxInfraRetries     int
	MaxPolicyViolations int
}

// Reset is a task_resets row. It marks an epoch: only attempts and infra
// retries with an ID greater than the cutoffs count after it.
type Reset struct {
	AfterAttemptID int64
	AfterInfraID   int64
}

// TaskInput is everything DeriveTaskStatus needs about one task.
type TaskInput struct {
	Key        string
	Deps       []string
	Attempts   []Attempt // any order
	Stages     []Stage   // stages of this task's attempts
	Overridden bool
	Resets     []Reset
	InfraIDs   []int64 // IDs of this task's infra_retries rows
}

// DeriveTaskStatus is pure. depStatuses are the statuses of in.Deps. A halted
// task blocks only its dependents (AT8): a halted dependency is not Done, so
// the dependent is blocked, while unrelated tasks are unaffected.
func DeriveTaskStatus(in TaskInput, depStatuses []Status, lim Limits) Status {
	if in.Overridden {
		return StatusOverridden
	}
	var cutA, cutI int64
	for _, r := range in.Resets {
		if r.AfterAttemptID > cutA {
			cutA = r.AfterAttemptID
		}
		if r.AfterInfraID > cutI {
			cutI = r.AfterInfraID
		}
	}
	var att []Attempt // ascending by N
	for _, a := range in.Attempts {
		if a.ID > cutA && a.Outcome != OutcomeAbandoned {
			att = append(att, a)
		}
	}
	sortAttempts(att)
	infra := 0
	for _, id := range in.InfraIDs {
		if id > cutI {
			infra++
		}
	}
	var last *Attempt
	if len(att) > 0 {
		last = &att[len(att)-1]
	}
	if last != nil && last.Outcome == OutcomePassed {
		return StatusPassed
	}
	if last != nil && last.Outcome == OutcomeBlocked {
		return StatusNeedsInput
	}
	if lim.MaxInfraRetries > 0 && infra >= lim.MaxInfraRetries {
		return StatusNeedsAttention
	}
	if lim.MaxPolicyViolations > 0 {
		n := 0
		for _, a := range att {
			if a.Outcome == OutcomePolicyViolation {
				n++
			}
		}
		if n >= lim.MaxPolicyViolations {
			return StatusNeedsAttention
		}
	}
	if last != nil && last.Outcome != "" {
		if stuck(att, lim.StuckThreshold) {
			return StatusStuck
		}
		if lim.MaxAttempts > 0 && len(att) >= lim.MaxAttempts {
			return StatusExhausted
		}
	}
	if last != nil && last.Outcome == "" {
		return StatusRunning
	}
	active := map[int64]bool{}
	for _, a := range att {
		active[a.ID] = true
	}
	for _, s := range in.Stages {
		if active[s.Attempt] && (s.State == StageQueued || s.State == StageRunning) {
			return StatusRunning
		}
	}
	for _, d := range depStatuses {
		if !d.Done() {
			return StatusBlocked
		}
	}
	if last != nil {
		return StatusRepair
	}
	return StatusReady
}

func sortAttempts(a []Attempt) {
	for i := 1; i < len(a); i++ {
		for j := i; j > 0 && a[j].N < a[j-1].N; j-- {
			a[j], a[j-1] = a[j-1], a[j]
		}
	}
}

// stuck reports whether the last threshold attempts all ended failed with the
// same non-empty fingerprint.
func stuck(att []Attempt, threshold int) bool {
	if threshold < 2 || len(att) < threshold {
		return false
	}
	tail := att[len(att)-threshold:]
	for _, a := range tail {
		if a.Outcome != OutcomeFailed || a.Fingerprint == "" || a.Fingerprint != tail[0].Fingerprint {
			return false
		}
	}
	return true
}

// DeriveAll derives every task's status. Tasks must form a DAG (ValidateTasks);
// a dependency missing from tasks counts as not done.
func DeriveAll(tasks []TaskInput, lim Limits) map[string]Status {
	byKey := map[string]TaskInput{}
	for _, t := range tasks {
		byKey[t.Key] = t
	}
	out := map[string]Status{}
	visiting := map[string]bool{}
	var get func(k string) Status
	get = func(k string) Status {
		if s, ok := out[k]; ok {
			return s
		}
		if visiting[k] { // dependency cycle: never done
			return StatusBlocked
		}
		visiting[k] = true
		defer delete(visiting, k)
		t, ok := byKey[k]
		if !ok {
			return StatusBlocked
		}
		ds := make([]Status, len(t.Deps))
		for i, d := range t.Deps {
			ds[i] = get(d)
		}
		s := DeriveTaskStatus(t, ds, lim)
		out[k] = s
		return s
	}
	for _, t := range tasks {
		get(t.Key)
	}
	return out
}

// AllDone reports whether every task is passed or overridden (AT8).
func AllDone(statuses map[string]Status) bool {
	if len(statuses) == 0 {
		return false
	}
	for _, s := range statuses {
		if !s.Done() {
			return false
		}
	}
	return true
}

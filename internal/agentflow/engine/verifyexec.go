package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"cmd184psu/unified-webapp/internal/agentflow/model"
	"cmd184psu/unified-webapp/internal/agentflow/sandbox"
	"cmd184psu/unified-webapp/internal/agentflow/store"
	"cmd184psu/unified-webapp/internal/agentflow/verify"
	"cmd184psu/unified-webapp/internal/taskmaster/golane"
)

// verifyExecutor is the engine-owned verify stage (V1 to V6): the structural
// checks, then the project's feedback commands compared with the baseline.
type verifyExecutor struct{ e *Engine }

// loadChain gathers the project, effort, task, attempt and stages a payload
// names.
func (e *Engine) loadChain(ctx context.Context, p StagePayload) (*chain, error) {
	s := e.deps.Store
	c := &chain{}
	var err error
	if c.proj, err = s.GetProject(ctx, p.Project); err != nil {
		return nil, err
	}
	if c.eff, err = s.GetEffort(ctx, p.Effort); err != nil {
		return nil, err
	}
	if c.att, err = s.GetAttempt(ctx, p.Attempt); err != nil {
		return nil, err
	}
	if c.task, err = s.GetTask(ctx, c.att.Task); err != nil {
		return nil, err
	}
	c.stages, err = s.ListAttemptStages(ctx, c.att.ID)
	return c, err
}

// Execute implements Executor. A failing verify is the stage outcome repair
// with the report attached; the report is also the reviewer's evidence.
func (x verifyExecutor) Execute(ctx context.Context, rc golane.RunContext, st model.Stage, p StagePayload) (StageResult, error) {
	e := x.e
	c, err := e.loadChain(ctx, p)
	if err != nil {
		return StageResult{}, err
	}
	set, err := e.deps.Store.EffectiveSettings(ctx, p.Project)
	if err != nil {
		return StageResult{}, err
	}
	ctx, cancel := withTimeout(ctx, set.VerifyTimeout)
	defer cancel()
	admin, err := e.deps.Git.AdminGitDir(ctx, c.proj.RepoPath, c.att.Worktree)
	if err != nil {
		return StageResult{}, err
	}
	base, err := os.ReadFile(e.baseCommitFile(c.task.ID))
	if err != nil {
		return StageResult{}, fmt.Errorf("task base commit: %w", err)
	}
	others, err := e.ownedByOthers(ctx, c, e.limitsFor(set))
	if err != nil {
		return StageResult{}, err
	}
	rep, err := verify.Structural(ctx, verify.Input{
		Worktree: c.att.Worktree, GitDir: admin, BaseCommit: strings.TrimSpace(string(base)), FilesTouched: c.task.FilesTouched,
		OwnedByOthers: others, NoChangesReason: c.noChangesReason(),
	})
	if err != nil {
		return StageResult{}, err
	}
	if rep.Pass {
		if _, err := e.feedback(ctx, c, &rep, c.att.Worktree, admin, filepath.Join(e.StageDir(st.ID), "home"), st.ID, set, c.touchedFiles()); err != nil {
			return StageResult{}, err
		}
	}
	return reportResult(rep)
}

// withTimeout applies verify_timeout; zero means none.
func withTimeout(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	if d <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, d)
}

// reportResult turns a report into the stage result: pass when green, repair
// with the failing checks listed otherwise. The report is the parsed output.
func reportResult(rep verify.VerifyReport) (StageResult, error) {
	b, err := json.Marshal(rep)
	if err != nil {
		return StageResult{}, err
	}
	res := StageResult{Outcome: OutcomePass, Raw: "verify passed", Parsed: b}
	if !reportOK(rep) {
		res.Outcome = OutcomeRepair
		res.Raw = "verify failed: " + failingChecks(rep)
	}
	return res, nil
}

// reportOK is the report's verdict; a report with no checks at all (no
// feedback commands, nothing to compare) is green.
func reportOK(rep verify.VerifyReport) bool {
	return len(rep.Checks) == 0 || rep.Pass
}

func failingChecks(rep verify.VerifyReport) string {
	var out []string
	for _, ch := range rep.Checks {
		if !ch.Pass {
			out = append(out, ch.Name+" "+strings.Join(ch.Details, "; "))
		}
	}
	return strings.Join(out, " | ")
}

// dockerSpec is the container a project's feedback commands run in, or nil for
// a host project. It checks spec's mounts first (a path the VM does not share is
// refused without booting it), then starts the runtime, so a Docker that cannot
// come up fails with the reason rather than at the first command (AF-12).
//
// The host sandbox template's extra binds and PATH dirs (the host's Go module cache, host
// toolchains) are dropped: a container has the image's toolchains, and the project's
// docker_mounts are its only extra binds. A host path there would be a Mac binary or a
// directory the VM does not share.
func (e *Engine) dockerSpec(ctx context.Context, p store.Project, name string, spec *sandbox.Spec) error {
	if p.Sandbox != "docker" {
		return nil
	}
	d, err := sandbox.NewDocker(p.DockerImage, p.DockerNetwork, p.DockerSELinux, e.deps.DockerProxy, name, sandbox.HostUser(), p.DockerMounts)
	if err != nil {
		return fmt.Errorf("project %s: %w", p.Slug, err)
	}
	if e.deps.Containers == nil {
		return fmt.Errorf("project %s: docker sandbox is not configured", p.Slug)
	}
	d.Shared = e.deps.Containers.SharedPaths()
	d.Instance = e.deps.Containers.Instance()
	spec.Docker, spec.ExtraROBinds, spec.PathDirs = d, nil, nil
	if err := sandbox.CheckDockerMounts(*spec); err != nil {
		spec.Docker = nil
		return fmt.Errorf("project %s: %w", p.Slug, err)
	}
	if err := e.deps.Containers.Ensure(ctx); err != nil {
		spec.Docker = nil
		return err
	}
	return nil
}

// feedbackSpec is the sandbox for feedback commands run on wt: the configured
// template plus this worktree, its trusted git dirs, and the project's
// toolchains and feedback environment (AF-6). The worktree is read-only unless
// the caller runs in a copy; caches go to the stage home.
func (e *Engine) feedbackSpec(p store.Project, wt, admin, home string) (sandbox.Spec, error) {
	if err := os.MkdirAll(home, 0o700); err != nil {
		return sandbox.Spec{}, err
	}
	s := e.deps.Sandbox
	s.Worktree, s.ReadOnlyWorktree = wt, true
	s.WorktreeGitDir = admin
	s.RepoGitDir = filepath.Dir(filepath.Dir(admin))
	s.StageHome = home
	if err := sandbox.ApplyToolchains(&s, p.Toolchains); err != nil {
		return sandbox.Spec{}, err
	}
	for k, v := range p.FeedbackEnv {
		if s.Env == nil {
			s.Env = map[string]string{}
		}
		s.Env[k] = v
	}
	return s, nil
}

// verifyWorktree is the directory feedback commands run in: wt itself, or a
// copy of it when the project sets verify_in_copy. A feedback command may not
// write to the worktree it verifies, and a toolchain such as .NET writes bin/
// and obj/ next to the sources, so that project is verified on a copy the
// stage home holds and the next run replaces (AF-6).
func verifyWorktree(p store.Project, wt, home string) (string, bool, error) {
	if !p.VerifyInCopy {
		return wt, false, nil
	}
	dst := filepath.Join(home, "verify-copy")
	if err := verify.CopyWorktree(dst, wt); err != nil {
		return "", false, err
	}
	return dst, true, nil
}

// feedback runs the project's feedback commands on wt and compares them with
// the effort's baseline into rep (V5). It returns the raw results so a merge
// can refresh the baseline from them.
func (e *Engine) feedback(ctx context.Context, c *chain, rep *verify.VerifyReport, wt, admin, home string, stage int64, set store.Settings, touched []string) ([]verify.CommandResult, error) {
	wt, writable, err := verifyWorktree(c.proj, wt, home)
	if err != nil {
		return nil, err
	}
	cmds, err := verify.Commands(c.proj.FeedbackCmds, wt)
	if err != nil {
		return nil, err
	}
	if len(cmds) == 0 {
		return nil, nil
	}
	spec, err := e.feedbackSpec(c.proj, wt, admin, home)
	if err != nil {
		return nil, err
	}
	if err = e.dockerSpec(ctx, c.proj, sandbox.ContainerName(c.proj.Slug, c.eff.ID, c.task.ID, strconv.FormatInt(stage, 10)), &spec); err != nil {
		return nil, err
	}
	if spec.Docker != nil {
		defer e.deps.Containers.Release()
	}
	spec.ReadOnlyWorktree = !writable
	results, err := verify.RunFeedback(ctx, verify.FeedbackConfig{Spec: spec, Timeout: set.VerifyTimeout, WritableWorktree: writable}, cmds)
	if err != nil {
		return nil, err
	}
	base, err := e.loadBaseline(ctx, c.eff.ID)
	if err != nil {
		return nil, err
	}
	verify.Compare(rep, results, base, touched)
	return results, nil
}

func (e *Engine) loadBaseline(ctx context.Context, effort int64) (verify.Baseline, error) {
	_, js, err := e.deps.Store.LatestBaseline(ctx, effort)
	if errors.Is(err, store.ErrNotFound) {
		return verify.Baseline{}, nil
	}
	if err != nil {
		return verify.Baseline{}, err
	}
	var b verify.Baseline
	return b, json.Unmarshal([]byte(js), &b)
}

// baselineFrom is the baseline a feedback run implies at commit: every
// failure it saw.
func baselineFrom(commit string, results []verify.CommandResult) verify.Baseline {
	b := verify.Baseline{Commit: commit, Failures: []verify.Failure{}}
	for _, r := range results {
		b.Failures = append(b.Failures, r.Failures...)
	}
	return b
}

func (e *Engine) saveBaseline(ctx context.Context, effort int64, b verify.Baseline) error {
	js, err := json.Marshal(b)
	if err != nil {
		return err
	}
	return e.deps.Store.SaveBaseline(ctx, effort, b.Commit, string(js))
}

// ensureBaseline records the feedback baseline on the integration worktree if
// the effort has none (V5). It runs before the first work stage touches
// anything, so the baseline is the base commit as the user left it.
func (e *Engine) ensureBaseline(ctx context.Context, proj store.Project, effort int64, intWT string) error {
	if _, _, err := e.deps.Store.LatestBaseline(ctx, effort); err == nil {
		return nil
	} else if !errors.Is(err, store.ErrNotFound) {
		return err
	}
	head, err := e.deps.Git.Head(ctx, intWT)
	if err != nil {
		return err
	}
	set, err := e.deps.Store.EffectiveSettings(ctx, proj.Slug)
	if err != nil {
		return err
	}
	cmds, err := verify.Commands(proj.FeedbackCmds, intWT)
	if err != nil {
		return err
	}
	base := verify.Baseline{Commit: head.Commit, Failures: []verify.Failure{}}
	if len(cmds) > 0 {
		admin, err := e.deps.Git.AdminGitDir(ctx, proj.RepoPath, intWT)
		if err != nil {
			return err
		}
		home := filepath.Join(e.deps.DataDir, "baselines", strconv.FormatInt(effort, 10))
		wt, writable, err := verifyWorktree(proj, intWT, home)
		if err != nil {
			return err
		}
		if cmds, err = verify.Commands(proj.FeedbackCmds, wt); err != nil {
			return err
		}
		spec, err := e.feedbackSpec(proj, wt, admin, home)
		if err != nil {
			return err
		}
		if err = e.dockerSpec(ctx, proj, sandbox.ContainerName(proj.Slug, effort, 0, "baseline"), &spec); err != nil {
			return err
		}
		if spec.Docker != nil {
			defer e.deps.Containers.Release()
		}
		spec.ReadOnlyWorktree = !writable
		bctx, cancel := withTimeout(ctx, set.VerifyTimeout)
		defer cancel()
		if base, err = verify.RecordBaseline(bctx, verify.FeedbackConfig{Spec: spec, Timeout: set.VerifyTimeout, WritableWorktree: writable}, cmds, head.Commit); err != nil {
			return fmt.Errorf("record baseline: %w", err)
		}
	}
	return e.saveBaseline(ctx, effort, base)
}

// reviewEvidence is the section of the reviewer's prompt that carries the
// attempt's VerifyReport (V6).
func (e *Engine) reviewEvidence(ctx context.Context, attempt int64) (string, error) {
	stages, err := e.deps.Store.ListAttemptStages(ctx, attempt)
	if err != nil {
		return "", err
	}
	for i := len(stages) - 1; i >= 0; i-- {
		if st := stages[i]; st.Role == "verify" && len(st.Parsed) > 0 {
			return "\n\n## Verify report (engine evidence)\n\n```json\n" + string(st.Parsed) + "\n```\n", nil
		}
	}
	return "", nil
}

// ownedByOthers are the files that the effort's other tasks are to write and
// that this task is not built on: every task that is neither this one nor one
// of its dependencies, direct or indirect. A dependency's files are already in
// this task's tree, so changing them cannot conflict at merge; a file of an
// unrelated task can (H11). A task already passed (merged) is no longer "to
// write" anything: its files are in the effort branch this task started from, and
// a later task that finds a defect in one of them may fix it (the reviewer sees the
// change as outside files_touched).
func (e *Engine) ownedByOthers(ctx context.Context, c *chain, lim model.Limits) ([]string, error) {
	tasks, err := e.deps.Store.ListTasks(ctx, c.eff.ID)
	if err != nil {
		return nil, err
	}
	status, err := e.deps.Store.TaskStatuses(ctx, c.eff.ID, lim)
	if err != nil {
		return nil, err
	}
	byKey := map[string]model.Task{}
	for _, t := range tasks {
		byKey[t.Key] = t
	}
	upstream := map[string]bool{c.task.Key: true}
	var walk func(key string)
	walk = func(key string) {
		for _, d := range byKey[key].DependsOn {
			if !upstream[d] {
				upstream[d] = true
				walk(d)
			}
		}
	}
	walk(c.task.Key)
	var out []string
	for _, t := range tasks {
		if !upstream[t.Key] && !status[t.Key].Done() {
			out = append(out, t.FilesTouched...)
		}
	}
	return out, nil
}

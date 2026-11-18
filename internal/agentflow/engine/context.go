package engine

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"cmd184psu/unified-webapp/internal/agentflow/model"
	"cmd184psu/unified-webapp/internal/agentflow/prompts"
	"cmd184psu/unified-webapp/internal/agentflow/runner"
	"cmd184psu/unified-webapp/internal/agentflow/store"
)

// Window is the context window a model is assumed to have (H15): the window it
// is served with when that is known, otherwise the floor, and never more than
// the model supports. The source says which: "set" (the owner's value),
// "observed" (a run was cut off at that size), "host", "model max" (the model
// supports less than the floor) or "floor" (nothing known).
func Window(m store.BackendModel, floor int) (tokens int, source string) {
	if m.WindowTokens > 0 {
		src := m.WindowSource
		if src == "" {
			src = "set"
		}
		return m.WindowTokens, src
	}
	if m.ContextTokens > 0 && m.ContextTokens < floor {
		return m.ContextTokens, "model max"
	}
	return floor, "floor"
}

// learnWindow records the size at which a host cut a run off as the model's
// served window (H15). The owner's own value is never replaced, and a learned
// one only moves down: a cut-off proves the window is no larger than that.
func (e *Engine) learnWindow(ctx context.Context, st model.Stage, err error) {
	var re *runner.Error
	if !errors.As(err, &re) || re.Tokens <= 0 || st.Backend == "" || st.Model == "" {
		return
	}
	b, gerr := e.deps.Store.GetBackend(ctx, st.Backend)
	if gerr != nil {
		return
	}
	for _, m := range b.Models {
		if m.Model != st.Model {
			continue
		}
		if m.WindowSource == "set" && m.WindowTokens > 0 {
			return
		}
		if m.WindowTokens > 0 && m.WindowTokens <= int(re.Tokens) {
			return
		}
		_ = e.deps.Store.SetModelWindow(ctx, st.Backend, st.Model, int(re.Tokens), "observed")
		return
	}
}

// Context estimate for a worker task (H15). The prompt is small; what fills a
// window is the work: a repair attempt starts with the previous diff, which
// holds every file written, and then writes them again. Measured on the first
// local trial: a three-file task peaked at 7.9K tokens on its first attempt
// (prompt 4.6K), and its repair attempts started at 7.5K to 9.7K and were cut
// off at 16K. The numbers are rough on purpose, and the engine learns the real
// windows from cut-off runs, so a wrong guess costs a reroute, not a failed task.
const (
	codeFileTokens  = 1500 // a typical source file
	pageFileTokens  = 2500 // html, css or js for a page
	workTurns       = 2    // a file is written, then carried again in a repair attempt's diff
	toolOutputSlack = 2000 // build and test output, directory listings
	agentOverhead   = 3000 // the agent program's own system prompt and tool definitions (measured: a first step costs 4.6K)
	runtimeSections = 800  // requirement text and definition of done added at run time
	reasoningNum    = 11   // room for thinking models: 11/10
	reasoningDen    = 10
)

// fileTokens is the assumed size of a file a task will write.
func fileTokens(path string) int {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".html", ".htm", ".css", ".js", ".ts", ".tsx", ".jsx":
		return pageFileTokens
	}
	return codeFileTokens
}

// TaskNeed estimates the context a worker needs for a task whose prompt takes
// promptTokens and which writes the given files.
func TaskNeed(promptTokens int, files []string) int {
	work := toolOutputSlack
	for _, f := range files {
		work += workTurns * fileTokens(f)
	}
	return (promptTokens + work) * reasoningNum / reasoningDen
}

// defPromptTokens is the first step of a worker for a compiled task: the
// agent's own overhead, the role's instructions, the task, its criteria and
// room for the requirement text and definition of done added at run time.
func defPromptTokens(def model.TaskDef) int {
	return promptTokens(def.Title, def.Prompt, def.Acceptance)
}

func promptTokens(title, prompt string, acceptance []string) int {
	text, _ := prompts.Load("worker")
	n := len(text) + len(title) + len(prompt) + (agentOverhead+runtimeSections)*4
	for _, a := range acceptance {
		n += len(a)
	}
	return n / 4
}

// needFor sets the context a task needs on the view, so the dispatcher passes
// over worker candidates whose window is smaller (H15). It does so only when
// some usable candidate can hold the task: a task too big for all of them is
// not left waiting for a model that will never come, it runs on the largest
// and the cut-off, if it happens, is reported.
func (e *Engine) needFor(v *view, p store.Project, t model.Task) {
	need := TaskNeed(promptTokens(t.Title, t.Prompt, t.Acceptance), t.FilesTouched)
	v.need = 0
	for _, c := range v.snap.CandidatesFor("worker", p.Slug) {
		if skipReason(v, p, c, false) == "" && windowOf(v, c) >= need {
			v.need = need
			return
		}
	}
}

// QueueContextProblems finds tasks that need more context than any worker
// candidate has (H15): the compiler is asked to split them. windows are the
// context windows of the project's worker candidates; with none known, nothing
// is flagged.
func QueueContextProblems(defs []model.TaskDef, windows []int) []string {
	best := 0
	for _, w := range windows {
		best = max(best, w)
	}
	if best == 0 {
		return nil
	}
	var out []string
	for _, d := range defs {
		need := TaskNeed(defPromptTokens(d), d.FilesTouched)
		if need > best {
			out = append(out, fmt.Sprintf("task %s needs about %d tokens of context (it writes %d file(s)) but the largest worker context window is %d: split it into smaller tasks that each write fewer files", d.Key, need, len(d.FilesTouched), best))
		}
	}
	return out
}

// modelOf is the backend's entry for a model, empty when it has none.
func modelOf(snap store.Snapshot, backend, name string) store.BackendModel {
	for _, b := range snap.Backends {
		if b.Name != backend {
			continue
		}
		for _, m := range b.Models {
			if m.Model == name {
				return m
			}
		}
	}
	return store.BackendModel{Model: name}
}

// windowOf is the context window of a candidate.
func windowOf(v *view, c store.Candidate) int {
	w, _ := Window(modelOf(v.snap, c.Backend, c.Model), v.snap.Settings.ContextFloor)
	return w
}

// workerWindows are the context windows of the project's usable worker candidates.
func workerWindows(v *view, p store.Project) []int {
	var out []int
	for _, c := range v.snap.CandidatesFor("worker", p.Slug) {
		if skipReason(v, p, c, false) == "" {
			out = append(out, windowOf(v, c))
		}
	}
	return out
}

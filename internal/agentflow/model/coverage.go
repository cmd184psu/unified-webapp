package model

import (
	"fmt"
	"path"
	"regexp"
	"strings"
)

var (
	reIDToken = regexp.MustCompile(`\b[A-Z]{1,3}\d+\b`)
	reIDLabel = regexp.MustCompile(`^\s*([A-Z]{1,3})(\d+)\s*:`)
	reIDParts = regexp.MustCompile(`^([A-Z]{1,3})\d+$`)
)

// QueueCoverageProblems checks a compiled queue against the FRD it came from
// (H14). A criterion label that looks like an FRD id (same letters, such as R
// or AC) must be one, so the compiler's own numbering cannot collide with the
// FRD's and pull in the wrong requirement text. Every requirement and every
// acceptance criterion must be cited by some task (a requirement also counts
// when a cited criterion belongs to it), so a requirement no task builds is
// found at compile time. A Go test file may not sit in a tests/ or test/
// directory: it cannot reach the package main code it tests. The result is
// empty when the queue is sound or the FRD carries no ids.
func QueueCoverageProblems(f FRD, defs []TaskDef) []string {
	ids := map[string]bool{}
	prefixes := map[string]bool{}
	add := func(id string) {
		if id == "" {
			return
		}
		ids[id] = true
		if m := reIDParts.FindStringSubmatch(id); m != nil {
			prefixes[m[1]] = true
		}
	}
	for _, r := range f.Requirements {
		add(r.ID)
	}
	for _, ac := range f.AcceptanceCriteria {
		add(ac.ID)
	}
	var problems []string
	cited := map[string]bool{}
	seenLabel := map[string]bool{}
	for _, t := range defs {
		for _, a := range t.Acceptance {
			if m := reIDLabel.FindStringSubmatch(a); m != nil && prefixes[m[1]] && !ids[m[1]+m[2]] && !seenLabel[m[1]+m[2]] {
				seenLabel[m[1]+m[2]] = true
				problems = append(problems, fmt.Sprintf("task %s cites %s%s, which is not an FRD id: cite only FRD ids and number your own steps differently (for example T3.1)", t.Key, m[1], m[2]))
			}
			for _, tok := range reIDToken.FindAllString(a, -1) {
				if ids[tok] {
					cited[tok] = true
				}
			}
		}
		for _, p := range t.FilesTouched {
			if isMisplacedGoTest(p) {
				problems = append(problems, fmt.Sprintf("task %s puts a Go test at %s: tests sit beside the code they test, in the same directory and package, never in a tests/ directory", t.Key, p))
			}
		}
	}
	problems = append(problems, mainComesLate(defs)...)
	if len(ids) == 0 {
		return problems
	}
	var uncited []string
	for _, ac := range f.AcceptanceCriteria {
		if ac.ID != "" && !cited[ac.ID] {
			uncited = append(uncited, ac.ID)
		}
	}
	linked := map[string]bool{}
	for _, ac := range f.AcceptanceCriteria {
		if cited[ac.ID] {
			for _, r := range ac.Requirements {
				linked[r] = true
			}
		}
	}
	for _, r := range f.Requirements {
		if r.ID != "" && !cited[r.ID] && !linked[r.ID] {
			uncited = append([]string{r.ID}, uncited...)
		}
	}
	if len(uncited) > 0 {
		problems = append(problems, "no task cites "+strings.Join(uncited, ", ")+": every FRD requirement and acceptance criterion must appear as the label of at least one task's acceptance criterion")
	}
	return problems
}

func isMisplacedGoTest(p string) bool {
	if !strings.HasSuffix(p, "_test.go") {
		return false
	}
	switch path.Base(path.Dir(p)) {
	case "tests", "test":
		return true
	}
	return false
}

// mainComesLate finds a Go queue whose first task writes code in the module
// root while main.go belongs to a later task. The build after that first task
// fails for want of a main function, and the worker that adds one is stopped
// by the scope check, so no model can pass the task.
func mainComesLate(defs []TaskDef) []string {
	first := -1
	for i, t := range defs {
		for _, p := range t.FilesTouched {
			if strings.HasSuffix(p, ".go") && !strings.HasSuffix(p, "_test.go") && !strings.Contains(p, "/") {
				first = i
			}
		}
		if first >= 0 {
			break
		}
	}
	if first < 0 {
		return nil
	}
	has := func(t TaskDef) bool {
		for _, p := range t.FilesTouched {
			if p == "main.go" {
				return true
			}
		}
		return false
	}
	if has(defs[first]) {
		return nil
	}
	for _, t := range defs[first+1:] {
		if has(t) {
			return []string{fmt.Sprintf("task %s is the first to write Go code in the module root but main.go comes later in %s: the build fails until a main function exists, so put main.go (with a minimal main function) in %s and let later tasks extend it", defs[first].Key, t.Key, defs[first].Key)}
		}
	}
	return nil
}

// RepairCoverage adds, to the task that builds the requirement, a criterion
// for each acceptance criterion no task cites (H33). A compiler that keeps the
// requirements straight but forgets one of their criteria would otherwise fail
// the coverage rule three times and halt the queue; the criterion's own wording
// from the FRD is exact, so the engine can write the line itself. It adds
// nothing for a criterion whose requirements no task cites, which is a real
// omission only the compiler can fix. defs is not changed; added names each
// criterion and the task that took it.
func RepairCoverage(f FRD, defs []TaskDef) (out []TaskDef, added []string) {
	out = make([]TaskDef, len(defs))
	copy(out, defs)
	cites := func(t TaskDef, id string) bool {
		for _, a := range t.Acceptance {
			for _, tok := range reIDToken.FindAllString(a, -1) {
				if tok == id {
					return true
				}
			}
		}
		return false
	}
	for _, ac := range f.AcceptanceCriteria {
		if ac.ID == "" || ac.Criterion == "" {
			continue
		}
		done := false
		for _, t := range out {
			done = done || cites(t, ac.ID)
		}
		for _, rid := range ac.Requirements {
			for i := range out {
				if done || !cites(out[i], rid) {
					continue
				}
				out[i].Acceptance = append(append([]string{}, out[i].Acceptance...), ac.ID+": "+ac.Criterion)
				added = append(added, ac.ID+" to "+out[i].Key)
				done = true
			}
		}
	}
	return out, added
}

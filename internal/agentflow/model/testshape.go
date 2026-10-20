package model

import (
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"
)

// QueueTestShapeProblems finds tests-only tasks that cover code an earlier task
// wrote in the same directory. Such a task finds the code only when it is already
// done, so a defect in that code turns up too late, in a task whose scope does
// not allow fixing it (the second local webcalc trial: T008 wrote tests, found
// the evaluator T004 wrote rejected every expression, and could not touch it). The
// tests belong in the task that writes the code; a tests-only task is for tests
// that span directories.
func QueueTestShapeProblems(defs []TaskDef) []string {
	byKey := map[string]TaskDef{}
	for _, d := range defs {
		byKey[d.Key] = d
	}
	upstream := func(d TaskDef) map[string]bool {
		seen := map[string]bool{}
		var walk func(k string)
		walk = func(k string) {
			for _, dep := range byKey[k].DependsOn {
				if !seen[dep] {
					seen[dep] = true
					walk(dep)
				}
			}
		}
		walk(d.Key)
		return seen
	}
	var out []string
	for _, d := range defs {
		if len(d.FilesTouched) == 0 || !allTestFiles(d.FilesTouched) {
			continue
		}
		dirs := map[string]bool{}
		for _, f := range d.FilesTouched {
			dirs[path.Dir(f)] = true
		}
		covered := map[string]bool{}
		for k := range upstream(d) {
			for _, f := range byKey[k].FilesTouched {
				if strings.HasSuffix(f, ".go") && !strings.HasSuffix(f, "_test.go") && dirs[path.Dir(f)] {
					covered[f+" ("+k+")"] = true
				}
			}
		}
		if len(covered) == 0 {
			continue
		}
		var code []string
		for c := range covered {
			code = append(code, c)
		}
		sort.Strings(code)
		out = append(out, fmt.Sprintf("task %s only writes tests (%s) for code that earlier tasks write in the same directory (%s): put each test in the task that writes the code it covers, naming the test file in that task's files_touched and prompt, and drop this task or give it work no other task does",
			d.Key, strings.Join(d.FilesTouched, ", "), strings.Join(code, ", ")))
	}
	return out
}

func allTestFiles(files []string) bool {
	for _, f := range files {
		if !strings.HasSuffix(f, "_test.go") {
			return false
		}
	}
	return true
}

var rePromptPath = regexp.MustCompile("[A-Za-z0-9_.-]+(?:/[A-Za-z0-9_.-]+)+\\.[A-Za-z0-9]{1,5}")

// QueueFileNameProblems finds a task whose files_touched lists a file under a
// name that is a path from its prompt with the slashes turned into other
// characters (the third local webcalc trial: the prompt said history/store.go
// and files_touched said history.store.go). The worker writes the file the
// prompt names, the checks and the reviewer look for the one listed, and the
// task stops with a file that "does not exist".
func QueueFileNameProblems(defs []TaskDef) []string {
	var out []string
	for _, d := range defs {
		listed := map[string]bool{}
		for _, f := range d.FilesTouched {
			listed[f] = true
		}
		for _, p := range rePromptPath.FindAllString(d.Prompt, -1) {
			if listed[p] {
				continue
			}
			for _, sep := range []string{".", "_", "-"} {
				if alias := strings.ReplaceAll(p, "/", sep); alias != p && listed[alias] {
					out = append(out, fmt.Sprintf("task %s lists %s in files_touched but its prompt names the file %s: use the path from the prompt", d.Key, alias, p))
				}
			}
		}
	}
	return out
}

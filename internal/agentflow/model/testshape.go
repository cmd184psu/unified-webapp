package model

import (
	"fmt"
	"path"
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

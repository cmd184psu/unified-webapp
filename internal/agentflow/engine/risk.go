package engine

import (
	"context"
	"fmt"
	"path"
	"regexp"
	"strings"

	"cmd184psu/unified-webapp/internal/agentflow/model"
	"cmd184psu/unified-webapp/internal/agentflow/store"
)

// AF-26: a task is flagged risky without a person saying so when it touches
// code that needs a second look, runs as the merger, or talks about a risky
// subject. The rules are settings (auto_risk, risky_paths, risky_words) so an
// owner can widen them or switch them off; the defaults cover the migrations,
// the auth package, the config loader and the deploy code. A task a caller
// already marked risky keeps its own reason: the rule never overwrites it.

// migrationName matches a file named like a migration, such as 0057_risk.sql.
var migrationName = regexp.MustCompile(`^\d+[_-][^/]*$`)

// AutoRisk reports whether a task should be flagged risky on its own and, when
// it should, the rule that fired plus the file or word that tripped it. It is
// pure: the task's own risk, the role it runs as and the settings are all it
// reads.
func AutoRisk(def model.TaskDef, role string, set store.Settings) (bool, string) {
	if def.Risk == model.RiskRisky { // a hand-set risk keeps its own reason
		return true, def.RiskReason
	}
	if !set.AutoRisk {
		return false, ""
	}
	if role == "merger" {
		return true, "runs as the merger: it resolves a conflicted merge"
	}
	for _, f := range def.FilesTouched {
		file := strings.TrimPrefix(strings.TrimSpace(f), "./")
		if isMigrationFile(file) {
			return true, fmt.Sprintf("touches a migration: %s", file)
		}
		if _, ok := riskyPath(file, set.RiskyPaths); ok {
			return true, fmt.Sprintf("touches a risky path: %s", file)
		}
	}
	for _, w := range set.RiskyWords {
		if mentions(def.Title, w) {
			return true, fmt.Sprintf("the title mentions %q", w)
		}
		if mentions(def.Prompt, w) {
			return true, fmt.Sprintf("the prompt mentions %q", w)
		}
	}
	return false, ""
}

// isMigrationFile reports whether a path is a new migration: a migrations/
// directory or a file named like one (0007_add_keys.sql).
func isMigrationFile(file string) bool {
	dir, base := path.Split(file)
	if strings.Contains(dir, "migrations/") {
		return true
	}
	return migrationName.MatchString(base)
}

// riskyPath reports whether file matches one of the settings' risky paths. An
// entry is a file, a directory (a trailing slash, or a path prefix) or a
// prefix ending in *.
func riskyPath(file string, entries []string) (string, bool) {
	for _, e := range entries {
		e = strings.TrimSpace(e)
		if e == "" {
			continue
		}
		switch {
		case strings.HasSuffix(e, "*"):
			if strings.HasPrefix(file, strings.TrimSuffix(e, "*")) {
				return e, true
			}
		case strings.HasSuffix(e, "/"):
			if strings.HasPrefix(file, e) || file == strings.TrimSuffix(e, "/") {
				return e, true
			}
		case file == e, strings.HasPrefix(file, e+"/"):
			return e, true
		}
	}
	return "", false
}

// mentions reports whether text contains word as a whole word, ignoring case:
// auth matches "the auth package" but not "authentication".
func mentions(text, word string) bool {
	word = strings.TrimSpace(word)
	if word == "" {
		return false
	}
	re := regexp.MustCompile(`(?i)(^|[^a-z0-9_])` + regexp.QuoteMeta(word) + `([^a-z0-9_]|$)`)
	return re.MatchString(text)
}

// AutoRiskTasks applies AutoRisk to the tasks that arrive without a risk, for
// the callers that put tasks in the store (the compiler, the acceptance check,
// the resolver, queue seeding and import). A settings read that fails leaves
// the tasks as they are: auto-risk never stops a queue from being written.
func (e *Engine) AutoRiskTasks(ctx context.Context, defs []model.TaskDef) []model.TaskDef {
	set, err := e.deps.Store.GetSettings(ctx)
	if err != nil {
		return defs
	}
	for i := range defs {
		if risky, reason := AutoRisk(defs[i], "", set); risky {
			defs[i].Risk = model.RiskRisky
			defs[i].RiskReason = reason
		}
	}
	return defs
}

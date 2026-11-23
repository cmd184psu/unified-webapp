package engine

import (
	"strings"
	"testing"

	"cmd184psu/unified-webapp/internal/agentflow/model"
	"cmd184psu/unified-webapp/internal/agentflow/store"
)

// riskSettings is the default settings every case here starts from.
func riskSettings() store.Settings {
	return store.Settings{AutoRisk: true, RiskyPaths: store.DefaultRiskyPaths, RiskyWords: store.DefaultRiskyWords}
}

func TestAutoRiskMigrationPath(t *testing.T) {
	risky, reason := AutoRisk(model.TaskDef{Title: "Add a column", FilesTouched: []string{"internal/agentflow/store/migrate.go"}}, "", riskSettings())
	if !risky || !strings.Contains(reason, "internal/agentflow/store/migrate.go") {
		t.Fatalf("a migration-touching task must be risky naming the file, got %v %q", risky, reason)
	}
	for _, f := range []string{"db/migrations/0007_add_keys.sql", "0058_risk_flags.sql"} {
		if risky, reason := AutoRisk(model.TaskDef{FilesTouched: []string{f}}, "", riskSettings()); !risky || !strings.Contains(reason, f) {
			t.Errorf("a new migration file %s must be risky, got %v %q", f, risky, reason)
		}
	}
}

func TestAutoRiskRiskyPaths(t *testing.T) {
	for _, f := range []string{"internal/platform/auth/key.go", "internal/platform/config/config.go", "deploy/promote.sh"} {
		if risky, reason := AutoRisk(model.TaskDef{FilesTouched: []string{"README.md", f}}, "", riskSettings()); !risky || !strings.Contains(reason, f) {
			t.Errorf("a task touching %s must be risky naming the file, got %v %q", f, risky, reason)
		}
	}
}

func TestAutoRiskDocsOnlyIsNormal(t *testing.T) {
	def := model.TaskDef{Title: "Explain the queue", Prompt: "Write the documentation for the task queue.", FilesTouched: []string{"docs/queue.md"}}
	if risky, reason := AutoRisk(def, "", riskSettings()); risky {
		t.Fatalf("a docs-only task must not be risky, got %q", reason)
	}
}

func TestAutoRiskMergerRole(t *testing.T) {
	if risky, reason := AutoRisk(model.TaskDef{Title: "Resolve the merge"}, "merger", riskSettings()); !risky || !strings.Contains(reason, "merger") {
		t.Fatalf("a merger task must be risky, got %v %q", risky, reason)
	}
	if risky, _ := AutoRisk(model.TaskDef{Title: "Resolve the merge"}, "worker", riskSettings()); risky {
		t.Fatalf("the same task as a worker must not be risky")
	}
}

func TestAutoRiskTitleWord(t *testing.T) {
	risky, reason := AutoRisk(model.TaskDef{Title: "Rotate the credential", Prompt: "do it"}, "", riskSettings())
	if !risky || !strings.Contains(reason, "credential") {
		t.Fatalf("a title naming a credential must be risky, got %v %q", risky, reason)
	}
	if risky, _ := AutoRisk(model.TaskDef{Title: "Add authentication docs"}, "", riskSettings()); risky {
		t.Fatalf("'authentication' is not the word 'auth'")
	}
	if risky, reason := AutoRisk(model.TaskDef{Prompt: "Store the API key in settings"}, "", riskSettings()); !risky || !strings.Contains(reason, "key") {
		t.Fatalf("a prompt naming a key must be risky, got %v %q", risky, reason)
	}
}

func TestAutoRiskOff(t *testing.T) {
	set := riskSettings()
	set.AutoRisk = false
	def := model.TaskDef{Title: "Rotate the credential", FilesTouched: []string{"internal/agentflow/store/migrate.go"}}
	if risky, reason := AutoRisk(def, "merger", set); risky {
		t.Fatalf("auto_risk off must flag nothing, got %q", reason)
	}
}

func TestAutoRiskKeepsHandSetRisk(t *testing.T) {
	set := riskSettings()
	set.AutoRisk = false
	def := model.TaskDef{Risk: model.RiskRisky, RiskReason: "the owner said so"}
	if risky, reason := AutoRisk(def, "", set); !risky || reason != "the owner said so" {
		t.Fatalf("a hand-set risk keeps its own reason, got %v %q", risky, reason)
	}
}

func TestAutoRiskEmptyRules(t *testing.T) {
	set := store.Settings{AutoRisk: true}
	def := model.TaskDef{Title: "Rotate the key", FilesTouched: []string{"internal/platform/auth/key.go"}}
	if risky, reason := AutoRisk(def, "", set); risky {
		t.Fatalf("empty path and word lists must flag nothing, got %q", reason)
	}
}

package agentflow

import "testing"

func TestNotifiableKindSkipsAdvisoryKinds(t *testing.T) {
	for _, k := range []string{"conflicting_guidance", "conflicting_guidance_summary", "started_from_backlog"} {
		if notifiableKind(k) {
			t.Fatalf("%s must not notify", k)
		}
	}
	for _, k := range []string{"task_halted", "stalled", "task_needs_files", "disk_low", "coordinator_question"} {
		if !notifiableKind(k) {
			t.Fatalf("%s must notify", k)
		}
	}
}

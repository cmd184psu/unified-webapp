package opencode

import (
	"testing"

	"cmd184psu/unified-webapp/internal/agentflow/runner"
)

func TestRefForKeepsProviderPrefixedModels(t *testing.T) {
	b := runner.Backend{Name: "opencode-go", APIType: "opencode"}
	if got := RefFor(b, "opencode-go/muse-spark-1.3-contributor"); got != "opencode-go/muse-spark-1.3-contributor" {
		t.Fatalf("got %q", got)
	}
	if got := RefFor(b, "big-pickle"); got != "opencode/big-pickle" {
		t.Fatalf("got %q", got)
	}
}

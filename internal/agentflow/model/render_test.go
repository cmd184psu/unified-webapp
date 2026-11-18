package model

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestEscNeutralisesMarkupAndLineMarkers(t *testing.T) {
	if got := esc("<b>a</b> & `c` *d* [e](f)"); got != `&lt;b&gt;a&lt;/b&gt; &amp; \`+"`"+`c\`+"`"+` \*d\* \[e\](f)` {
		t.Errorf("esc = %q", got)
	}
	if got := escBlock("- a\n+ b\n1. c\n2) d\nplain\n# h"); got != "\\- a\n\\+ b\n1\\. c\n2\\) d\nplain\n\\# h" {
		t.Errorf("escBlock = %q", got)
	}
}

func TestRenderFRDAndPlanHaveNoRawHTML(t *testing.T) {
	f := FRD{ID: "F", Title: "<script>x</script>", Objective: "o", Requirements: []FRDRequirement{{ID: "R1", Description: "<img src=x onerror=1>"}},
		StateModel: json.RawMessage(`{"<a>":["<b>"]}`)}
	md := RenderFRD(f)
	if strings.ContainsAny(md, "<>") || !strings.Contains(md, "# F: &lt;script&gt;x&lt;/script&gt;") {
		t.Errorf("FRD markdown:\n%s", md)
	}
	plan := RenderPlan(json.RawMessage(`{"phases":[{"name":"<i>"}],"principles":["<u>"],"zeta":"<s>"}`))
	if strings.ContainsAny(plan, "<>") || strings.Index(plan, "Principles") > strings.Index(plan, "Phases") || strings.Index(plan, "Phases") > strings.Index(plan, "Zeta") {
		t.Errorf("plan markdown:\n%s", plan)
	}
}

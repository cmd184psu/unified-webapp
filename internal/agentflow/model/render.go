package model

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Markdown rendering of FRDs, plans and efforts (DBS6). Every piece of text
// that came from a document is escaped: HTML characters become entities and
// markdown syntax characters get a backslash, so no input can add markup, raw
// HTML included. The renderer itself only emits markdown syntax.

// mdSpecials are escaped wherever they occur in text.
const mdSpecials = "\\`*_[]#|~!"

// esc escapes text for one line of markdown. Line breaks and tabs become
// spaces.
func esc(s string) string {
	var b strings.Builder
	for _, r := range strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ", "\t", " ").Replace(s) {
		switch {
		case r == '<':
			b.WriteString("&lt;")
		case r == '>':
			b.WriteString("&gt;")
		case r == '&':
			b.WriteString("&amp;")
		case strings.ContainsRune(mdSpecials, r):
			b.WriteByte('\\')
			b.WriteRune(r)
		default:
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}

// escBlock escapes multi-line text, keeping its lines, and neutralises the
// markers that would turn a line into a list item.
func escBlock(s string) string {
	lines := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	for i, l := range lines {
		l = esc(l)
		switch {
		case strings.HasPrefix(l, "-"), strings.HasPrefix(l, "+"), strings.HasPrefix(l, "="):
			l = "\\" + l
		default:
			if n := leadingDigits(l); n > 0 && n < len(l) && (l[n] == '.' || l[n] == ')') {
				l = l[:n] + "\\" + l[n:]
			}
		}
		lines[i] = l
	}
	return strings.Join(lines, "\n")
}

func leadingDigits(s string) int {
	n := 0
	for n < len(s) && s[n] >= '0' && s[n] <= '9' {
		n++
	}
	return n
}

// heading writes a heading of the given level.
func heading(b *strings.Builder, level int, text string) {
	if level > 6 {
		level = 6
	}
	fmt.Fprintf(b, "%s %s\n\n", strings.Repeat("#", level), esc(text))
}

func para(b *strings.Builder, s string) {
	if strings.TrimSpace(s) != "" {
		b.WriteString(escBlock(s))
		b.WriteString("\n\n")
	}
}

func bullets(b *strings.Builder, items []string) {
	for _, it := range items {
		fmt.Fprintf(b, "- %s\n", esc(it))
	}
	if len(items) > 0 {
		b.WriteString("\n")
	}
}

// RenderFRD renders an FRD as markdown.
func RenderFRD(f FRD) string {
	var b strings.Builder
	writeFRD(&b, f, 1)
	return b.String()
}

func writeFRD(b *strings.Builder, f FRD, level int) {
	heading(b, level, f.ID+": "+f.Title)
	var meta []string
	for _, kv := range [][2]string{{"Status", f.Status}, {"Created", f.Created}, {"Approved", f.ApprovedDate}} {
		if kv[1] != "" {
			meta = append(meta, kv[0]+": "+kv[1])
		}
	}
	bullets(b, meta)
	if len(f.Toolchains) > 0 {
		fmt.Fprintf(b, "Toolchains: %s\n\n", esc(strings.Join(f.Toolchains, ", ")))
	}
	heading(b, level+1, "Objective")
	para(b, f.Objective)
	if f.Context != "" {
		heading(b, level+1, "Context")
		para(b, f.Context)
	}
	if len(f.Constraints) > 0 {
		heading(b, level+1, "Constraints")
		bullets(b, f.Constraints)
	}
	if len(f.Groups) > 0 {
		heading(b, level+1, "Groups")
		for _, g := range f.Groups {
			fmt.Fprintf(b, "- **%s** %s", esc(g.ID), esc(g.Title))
			if g.Summary != "" {
				fmt.Fprintf(b, ": %s", esc(g.Summary))
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	writeRaw(b, level+1, "State model", f.StateModel)
	heading(b, level+1, "Requirements")
	for _, r := range f.Requirements {
		var tags []string
		if r.Priority != "" {
			tags = append(tags, r.Priority)
		}
		if r.Group != "" {
			tags = append(tags, "group "+r.Group)
		}
		tag := ""
		if len(tags) > 0 {
			tag = " (" + esc(strings.Join(tags, ", ")) + ")"
		}
		fmt.Fprintf(b, "- **%s**%s: %s\n", esc(r.ID), tag, esc(r.Description))
	}
	b.WriteString("\n")
	heading(b, level+1, "Acceptance criteria")
	for _, ac := range f.AcceptanceCriteria {
		refs := ""
		if len(ac.Requirements) > 0 {
			refs = " (" + esc(strings.Join(ac.Requirements, ", ")) + ")"
		}
		fmt.Fprintf(b, "- **%s**%s: %s\n", esc(ac.ID), refs, esc(ac.Criterion))
	}
	b.WriteString("\n")
	if len(f.Decisions) > 0 {
		heading(b, level+1, "Decisions")
		for _, d := range f.Decisions {
			fmt.Fprintf(b, "- **%s**: %s", esc(d.ID), esc(d.Decision))
			if d.Rationale != "" {
				fmt.Fprintf(b, " (%s)", esc(d.Rationale))
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	writeRaw(b, level+1, "Findings", f.Findings)
	if len(f.OutOfScope) > 0 {
		heading(b, level+1, "Out of scope")
		bullets(b, f.OutOfScope)
	}
	writeRaw(b, level+1, "Planning spikes", f.PlanningSpikes)
}

// planKeyOrder is the order the plan's known sections come in; other keys
// follow alphabetically.
var planKeyOrder = []string{"principles", "drivers", "options", "phases", "adr"}

// RenderPlan renders a plan document (a JSON object) as markdown.
func RenderPlan(raw json.RawMessage) string {
	var b strings.Builder
	heading(&b, 1, "Plan")
	writePlan(&b, raw, 2)
	return b.String()
}

func writePlan(b *strings.Builder, raw json.RawMessage, level int) {
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil {
		para(b, string(raw))
		return
	}
	keys := make([]string, 0, len(m))
	for _, k := range planKeyOrder {
		if _, ok := m[k]; ok {
			keys = append(keys, k)
		}
	}
	var rest []string
	for k := range m {
		if !contains(planKeyOrder, k) {
			rest = append(rest, k)
		}
	}
	sort.Strings(rest)
	for _, k := range append(keys, rest...) {
		writeRaw(b, level, sectionTitle(k), m[k])
	}
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

// sectionTitle turns a JSON key into a heading: "files_touched" becomes
// "Files touched".
func sectionTitle(k string) string {
	k = strings.ReplaceAll(k, "_", " ")
	if k == "" {
		return k
	}
	return strings.ToUpper(k[:1]) + k[1:]
}

// writeRaw renders an arbitrary JSON value under a heading; empty and null
// values write nothing.
func writeRaw(b *strings.Builder, level int, title string, raw json.RawMessage) {
	if len(bytes.TrimSpace(raw)) == 0 || string(bytes.TrimSpace(raw)) == "null" {
		return
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return
	}
	heading(b, level, title)
	if s, ok := v.(string); ok {
		para(b, s)
		return
	}
	writeValue(b, v, "")
	b.WriteString("\n")
}

// writeValue renders a decoded JSON value as nested bullet lists.
func writeValue(b *strings.Builder, v any, indent string) {
	switch x := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if isScalar(x[k]) {
				fmt.Fprintf(b, "%s- **%s**: %s\n", indent, esc(k), scalarText(x[k]))
				continue
			}
			fmt.Fprintf(b, "%s- **%s**:\n", indent, esc(k))
			writeValue(b, x[k], indent+"  ")
		}
	case []any:
		for _, it := range x {
			if isScalar(it) {
				fmt.Fprintf(b, "%s- %s\n", indent, scalarText(it))
				continue
			}
			fmt.Fprintf(b, "%s-\n", indent)
			writeValue(b, it, indent+"  ")
		}
	default:
		fmt.Fprintf(b, "%s%s\n", indent, scalarText(v))
	}
}

func isScalar(v any) bool {
	switch v.(type) {
	case map[string]any, []any:
		return false
	}
	return true
}

func scalarText(v any) string {
	switch x := v.(type) {
	case nil:
		return "none"
	case string:
		return esc(x)
	case json.Number:
		return esc(x.String())
	default:
		return esc(fmt.Sprint(x))
	}
}

// EffortReport is everything an effort export carries. Nil or empty parts are
// left out of the markdown.
type EffortReport struct {
	Effort       Effort
	FRD          *FRD
	FRDRevision  int
	Plan         json.RawMessage
	PlanRevision int
	Queue        *Queue
	Statuses     map[string]Status
}

// RenderEffort renders an effort with its FRD, plan and queue as markdown.
func RenderEffort(r EffortReport) string {
	var b strings.Builder
	e := r.Effort
	heading(&b, 1, fmt.Sprintf("Effort %d: %s", e.ID, e.Title))
	meta := []string{"Project: " + e.Project, "Status: " + e.Status, "Run state: " + e.RunState}
	if e.IntegrationBranch != "" {
		meta = append(meta, "Integration branch: "+e.IntegrationBranch)
	}
	if e.BaseCommit != "" {
		meta = append(meta, "Base commit: "+e.BaseCommit)
	}
	if e.YOLO {
		meta = append(meta, "YOLO: on")
	}
	bullets(&b, meta)
	if r.FRD != nil {
		writeFRD(&b, *r.FRD, 2)
		fmt.Fprintf(&b, "_FRD revision %d._\n\n", r.FRDRevision)
	}
	if len(r.Plan) > 0 {
		heading(&b, 2, fmt.Sprintf("Plan (revision %d)", r.PlanRevision))
		writePlan(&b, r.Plan, 3)
	}
	if r.Queue != nil {
		heading(&b, 2, "Queue")
		bullets(&b, []string{
			fmt.Sprintf("Revision: %d", r.Queue.Revision), "Hash: " + r.Queue.Hash, "Status: " + r.Queue.Status,
		})
		for _, t := range r.Queue.Tasks {
			title := t.Key + ": " + t.Title
			if s, ok := r.Statuses[t.Key]; ok {
				title += " [" + string(s) + "]"
			}
			heading(&b, 3, title)
			para(&b, t.Prompt)
			if len(t.Acceptance) > 0 {
				b.WriteString("Acceptance:\n\n")
				bullets(&b, t.Acceptance)
			}
			if len(t.FilesTouched) > 0 {
				b.WriteString("Files touched:\n\n")
				bullets(&b, t.FilesTouched)
			}
			if len(t.DependsOn) > 0 {
				b.WriteString("Depends on: " + esc(strings.Join(t.DependsOn, ", ")) + "\n\n")
			}
		}
	}
	return b.String()
}

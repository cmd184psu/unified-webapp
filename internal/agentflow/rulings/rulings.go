// Package rulings reads a project's RULINGS.md (the oracle) from a worktree and
// selects the rulings a task should see: those whose areas intersect the task's
// files_touched (via a path-prefix -> area tag map) plus every ruling tagged
// "process". It also detects when a reviewer repair contradicts a matched ruling.
package rulings

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// A Ruling is one row of RULINGS.md. The areas field is a comma-separated list
// of area tags (e.g. "corruption,engine"); "process" rulings are always
// included regardless of the task's files.
type Ruling struct {
	ID        string
	Topic     string
	Ruling    string
	Source    string
	Authority string
	Areas     []string
	Date      string
}

// RulingFile is the parsed oracle for one worktree: the file's preamble (the
// authority rules above the table) and the parsed rulings.
type RulingFile struct {
	// Preamble is the markdown above the table: the authority rules the agent
	// should read before the selected rulings.
	Preamble string
	Rulings  []Ruling
}

// AreaMap maps path prefixes (glob-like, "*" only as a trailing suffix) to area
// tags. "src/rules/corruption*" -> "corruption" means a file whose path relative
// to the repo root starts with "src/rules/corruption" is in area "corruption".
type AreaMap map[string]string

// ParseAreaMap reads the stored rulings_file_area_map value (a JSON object whose
// keys are path prefixes ending in "*" and values are area tags). An empty or
// unparseable value is an empty map (no path->area matches; process rulings
// still apply).
func ParseAreaMap(v string) AreaMap {
	m := AreaMap{}
	if v == "" {
		return m
	}
	raw := map[string]string{}
	if jsonUnmarshal([]byte(v), &raw) != nil {
		return m
	}
	for k, a := range raw {
		k = strings.TrimSpace(k)
		a = strings.TrimSpace(a)
		if k == "" || a == "" {
			continue
		}
		if !strings.HasSuffix(k, "*") {
			k = k + "*"
		}
		m[k] = a
	}
	return m
}

// AreaOf returns the area tag for a file path relative to the repo root, or ""
// when no prefix matches. The path is cleaned first.
func (m AreaMap) AreaOf(path string) string {
	rel := strings.TrimSpace(filepath.ToSlash(path))
	if rel == "" || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return ""
	}
	for prefix, area := range m {
		name := strings.TrimSuffix(prefix, "*")
		if strings.HasPrefix(rel, name) {
			return area
		}
	}
	return ""
}

// Load reads and parses the rulings file at the given absolute path. It returns
// the parsed file (preamble + rulings) or an error. Load does not care whether
// the setting is on or off; the caller decides that.
func Load(path string) (RulingFile, error) {
	path = filepath.Clean(path)
	data, err := os.ReadFile(path)
	if err != nil {
		return RulingFile{}, err
	}
	return Parse(string(data)), nil
}

// Parse parses a RULINGS.md body (as read from disk) into preamble + rulings.
// It is lenient: an unparseable body returns an empty RulingFile with the whole
// body as preamble and no rulings, never an error.
func Parse(body string) RulingFile {
	body = strings.ReplaceAll(body, "\r\n", "\n")
	idx := strings.Index(body, "\n| ")
	if idx == -1 {
		return RulingFile{Preamble: strings.TrimSpace(body)}
	}
	// Walk back to the start of the line that contains the table header.
	lineStart := strings.LastIndex(body[:idx], "\n") + 1
	preamble := strings.TrimSpace(body[:lineStart])
	rest := body[idx:]
	// Find the header row and the separator row.
	lines := strings.Split(rest, "\n")
	if len(lines) < 2 {
		return RulingFile{Preamble: preamble}
	}
	// skip header + separator
	start := 0
	for start < len(lines) && !strings.HasPrefix(strings.TrimSpace(lines[start]), "| id") {
		start++
	}
	start++ // header line
	if start < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[start]), "|---|") {
		start++ // separator
	}
	var out []Ruling
	for _, ln := range lines[start:] {
		ln = strings.TrimSpace(ln)
		if ln == "" || !strings.HasPrefix(ln, "|") {
			continue
		}
		r, ok := parseRow(ln)
		if ok {
			out = append(out, r)
		}
	}
	return RulingFile{Preamble: preamble, Rulings: out}
}

func parseRow(ln string) (Ruling, bool) {
	// Split on "|", drop the leading/trailing empty tokens from the leading/| trailing "|".
	parts := strings.Split(ln, "|")
	if len(parts) < 8 {
		return Ruling{}, false
	}
	// parts[0] is "" (before first |), parts[len-1] is "" (after last |).
	vals := parts[1 : len(parts)-1]
	if len(vals) < 7 {
		return Ruling{}, false
	}
	for i := range vals {
		vals[i] = strings.TrimSpace(vals[i])
	}
	areas := strings.Split(vals[5], ",")
	for i := range areas {
		areas[i] = strings.TrimSpace(areas[i])
	}
	return Ruling{
		ID:        vals[0],
		Topic:     vals[1],
		Ruling:    vals[2],
		Source:    vals[3],
		Authority: vals[4],
		Areas:     areas,
		Date:      vals[6],
	}, true
}

// Selected returns the rulings selected for a task's files: those whose areas
// intersect the task's files_touched (via the area map) plus every ruling tagged
// "process" (always included regardless of files).
func Selected(rf RulingFile, files []string, am AreaMap) []Ruling {
	areaSet := map[string]bool{}
	for _, f := range files {
		if a := am.AreaOf(f); a != "" {
			areaSet[a] = true
		}
	}
	var out []Ruling
	for _, r := range rf.Rulings {
		if sliceContains(r.Areas, "process") {
			out = append(out, r)
			continue
		}
		for _, a := range r.Areas {
			if areaSet[a] {
				out = append(out, r)
				break
			}
		}
	}
	return out
}

// Header builds the ruling header injected into a stage prompt. root is the
// absolute path to the checkout root (worktree for attempt stages, repo path for
// effort stages); rulingsFile is the setting value (relative to root, may be
// empty = off). When the setting is empty or the file does not exist, Header
// returns "". Otherwise it returns preamble + the selected rulings (id, topic,
// ruling) + the full file path the agent can read for the rest.
func Header(root, rulingsFile string, files []string, am AreaMap) string {
	if rulingsFile == "" {
		return ""
	}
	full := filepath.Join(root, filepath.Clean(rulingsFile))
	if !cheapExists(full) {
		return ""
	}
	rf, err := Load(full)
	if err != nil || len(rf.Rulings) == 0 && rf.Preamble == "" {
		return ""
	}
	sel := Selected(rf, files, am)
	var b strings.Builder
	b.WriteString("## Rulings (the oracle) injected for this task\n\n")
	b.WriteString(rf.Preamble)
	b.WriteString("\n\nThe rulings below apply to this task; the full file is at ")
	b.WriteString(full)
	b.WriteString(" — read it for anything not listed.\n\n")
	if len(sel) == 0 {
		b.WriteString("_No ruling matches this task's files; only the process rulings above apply._\n\n")
		return b.String()
	}
	b.WriteString("| id | topic | ruling |\n|---|---|---|\n")
	for _, r := range sel {
		b.WriteString(fmt.Sprintf("| %s | %s | %s |\n", r.ID, r.Topic, r.Ruling))
	}
	b.WriteString("\n")
	return b.String()
}

// CheapExists reports whether path exists without reading it; used by the engine
// and by Header/Injected to decide whether to load the file.
func CheapExists(path string) bool {
	return cheapExists(path)
}

// cheapExists reports whether path exists without reading it; used by Header and
// Injected to decide whether to load the file.
func cheapExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// InjectedRuling is a selection-ready ruling for the per-effort dashboard/export.
type InjectedRuling struct {
	ID     string
	Topic  string
	Ruling string
	Areas  string
	Date   string
}

// Injected returns the rulings shown on the effort's dashboard/export: the
// selected rulings (by area + process) for the given files, as InjectedRuling
// structs, plus the relative file path. If the setting is off or the file is
// missing, it returns an empty list and an empty path.
func Injected(root, rulingsFile string, files []string, am AreaMap) (path string, out []InjectedRuling) {
	if rulingsFile == "" {
		return "", nil
	}
	full := filepath.Join(root, filepath.Clean(rulingsFile))
	if !cheapExists(full) {
		return "", nil
	}
	rf, err := Load(full)
	if err != nil {
		return full, nil
	}
	sel := Selected(rf, files, am)
	path = filepath.ToSlash(filepath.Clean(rulingsFile))
	for _, r := range sel {
		out = append(out, InjectedRuling{
			ID:     r.ID,
			Topic:  r.Topic,
			Ruling: r.Ruling,
			Areas:  strings.Join(r.Areas, ", "),
			Date:   r.Date,
		})
	}
	return path, out
}

// ContradictsRuling reports whether a reviewer repair (its issues + repair
// prompt) contradicts any of the given rulings. It is a lightweight, deliberate
// heuristic: a repair contradicts a ruling when a ruling clause and a repair
// clause share a substantive phrase and express opposite polarity (one denies
// what the other asserts). It is deterministic and testable; it is a backstop for
// the reviewer instruction that already forbids raising such a repair.
func ContradictsRuling(review Issues, rulings []Ruling) bool {
	if len(rulings) == 0 {
		return false
	}
	repairText := normalizedClauseText(review)
	if repairText == "" {
		return false
	}
	for _, r := range rulings {
		for _, clause := range splitClauses(r.Ruling) {
			if clausePolarityClash(clause, repairText) {
				return true
			}
		}
	}
	return false
}

// Issues is the reviewer's proposed repair, as the engine sees it.
type Issues struct {
	Issues       []SchemaIssue
	RepairPrompt string
}

// SchemaIssue is a minimal view of a reviewer issue.
type SchemaIssue struct {
	Description string
	File        string
	Line        int
}

func normalizedClauseText(issues Issues) string {
	var parts []string
	for _, is := range issues.Issues {
		if is.Description != "" {
			parts = append(parts, is.Description)
		}
	}
	if issues.RepairPrompt != "" {
		parts = append(parts, issues.RepairPrompt)
	}
	return normalizePhrase(strings.Join(parts, " "))
}

func splitClauses(text string) []string {
	out := strings.Split(text, ". ")
	for i, c := range out {
		out[i] = strings.TrimSpace(c)
	}
	if strings.TrimSpace(out[len(out)-1]) == "" {
		out = out[:len(out)-1]
	}
	return out
}

func normalizePhrase(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\t", " ")
	for _, p := range []string{"`", "\"", "'", ",", ";", ":", "(", ")", "[", "]", "{", "}", "-", "—", "–"} {
		s = strings.ReplaceAll(s, p, " ")
	}
	return strings.Join(strings.Fields(s), " ")
}

// clausePolarityClash reports whether `clause` (a ruling clause) and `repair`
// (normalized repair text) conflict: they share a substantive phrase about the
// same subject and express opposite polarity (one keeps/stays false, the other
// asserts true/set). It is deterministic and testable; it is a backstop for the
// reviewer instruction that already forbids raising such a repair.
func clausePolarityClash(clause, repair string) bool {
	cNorm := normalizePhrase(clause)
	if cNorm == "" || len(strings.Fields(cNorm)) < 3 {
		return false
	}
	// Look for a shared substantive phrase (at least 3 content words) that
	// appears in both the clause and the repair, then check the polarity around
	// the key value word in the repair (true/false, enabled/disabled, must be X
	// vs stays X).
	share := sharedPhrase(cNorm, repair)
	if share == "" {
		return false
	}
	return oppositePolarityAround(share, cNorm, repair)
}

// sharedPhrase returns the longest substring (>= 3 words) that appears in both
// normalized strings, or "" if there is none. It compares word runs, not byte
// offsets, so it costs O(words(a)*words(b)) instead of cubic byte scans; the
// attention scan calls it once per note pair, where a cubic scan stalls the
// endpoint on long histories.
func sharedPhrase(a, b string) string {
	if len(a) > len(b) {
		a, b = b, a
	}
	aw, bw := strings.Fields(a), strings.Fields(b)
	if len(aw) < 3 || len(bw) < 3 {
		return ""
	}
	// Compare words with attached punctuation trimmed (the normalizer keeps
	// periods, so "calculation." must still match "calculation"); the
	// returned span uses the original words of a.
	key := func(w string) string { return strings.Trim(w, ".,;:!?()[]{}\"'") }
	ak, bk := make([]string, len(aw)), make([]string, len(bw))
	for i, w := range aw {
		ak[i] = key(w)
	}
	for j, w := range bw {
		bk[j] = key(w)
	}
	// Longest common contiguous word run of at least 3 words.
	prev := make([]int, len(bw)+1)
	bestLen, bestEnd := 0, 0 // bestEnd is the end index in aw
	for i := 1; i <= len(aw); i++ {
		cur := make([]int, len(bw)+1)
		for j := 1; j <= len(bw); j++ {
			if ak[i-1] != "" && ak[i-1] == bk[j-1] {
				cur[j] = prev[j-1] + 1
				if cur[j] > bestLen {
					bestLen, bestEnd = cur[j], i
				}
			}
		}
		prev = cur
	}
	if bestLen < 3 {
		return ""
	}
	return strings.Join(aw[bestEnd-bestLen:bestEnd], " ")
}

// oppositePolarityAround reports whether the repair text expresses the opposite
// polarity of the clause within the shared phrase region.
func oppositePolarityAround(share, clause, repair string) bool {
	// Find the value-bearing word in the shared phrase for each side.
	// Typical pair: "keeps ... false" vs "must be set to true".
	clauseVal := valueWord(clause)
	repairVal := valueWord(repair)
	if clauseVal == "" || repairVal == "" {
		return false
	}
	if oppositeValue(clauseVal, repairVal) {
		return true
	}
	// Also handle the negation-cue flip: the ruling says "does NOT X" and the
	// repair says "must X" around the same subject.
	if hasNegation(clause) && !hasNegation(repair) && strings.Contains(repair, share) {
		return true
	}
	if !hasNegation(clause) && hasNegation(repair) && strings.Contains(repair, share) {
		return true
	}
	return false
}

func valueWord(s string) string {
	lower := strings.ToLower(s)
	for _, w := range []string{"true", "false", "enabled", "disabled", "on", "off", "yes", "no"} {
		if strings.Contains(lower, w) {
			return w
		}
	}
	return ""
}

func oppositeValue(a, b string) bool {
	pair := map[string]string{"true": "false", "false": "true", "enabled": "disabled",
		"disabled": "enabled", "on": "off", "off": "on", "yes": "no", "no": "yes"}
	return pair[a] == b
}

// negationCues are words/phrases that flip a clause's polarity.
var negationCues = []string{"not", "no ", "never", "don't", "doesn't", "won't", "shouldn't",
	"must not", "can't", "couldn't", "wouldn't", "isn't", "aren't", "wasn't", "weren't",
	"false", "disabled", "off", "without", "instead of"}

// hasNegation reports whether the normalized phrase contains a negation cue as a
// token or leading substring.
func hasNegation(s string) bool {
	for _, c := range negationCues {
		if strings.Contains(s, c) {
			return true
		}
	}
	return false
}

func phraseIn(phrase, haystack string) bool {
	if phrase == "" {
		return false
	}
	return strings.Contains(haystack, phrase)
}

func sliceContains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

// ContradictIssues reports whether any issue in `a` contradicts any issue in `b`
// (two notes on one task that clash). It uses the same clause-polarity heuristic
// on the issue descriptions. It is used for the "two notes contradict each other"
// attention case.
func ContradictIssues(a, b Issues) bool {
	if len(a.Issues) == 0 || len(b.Issues) == 0 {
		return false
	}
	ta := normalizedClauseText(a)
	tb := normalizedClauseText(b)
	if ta == "" || tb == "" {
		return false
	}
	// Two notes clash when each contains a clause whose opposite appears in the
	// other, i.e. they assert and deny the same thing.
	for _, ca := range splitClauses(reverseJoin(a.Issues)) {
		if clausePolarityClash(ca, tb) {
			return true
		}
	}
	for _, cb := range splitClauses(reverseJoin(b.Issues)) {
		if clausePolarityClash(cb, ta) {
			return true
		}
	}
	return false
}

func reverseJoin(issues []SchemaIssue) string {
	var b strings.Builder
	for i := len(issues) - 1; i >= 0; i-- {
		if issues[i].Description != "" {
			if b.Len() > 0 {
				b.WriteByte(' ')
			}
			b.WriteString(issues[i].Description)
		}
	}
	return b.String()
}

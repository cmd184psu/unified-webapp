package model

import (
	"regexp"
	"strings"
)

// Literal facts (phase H1). A person's request often names exact things a
// weak model paraphrases away: a port, a URL path, a status code, a quoted
// message. Extraction is deterministic and conservative, because a literal
// that is wrongly required would stall a run; the checks built on it only ask
// that each one survives, verbatim, from the request into the FRD and from the
// FRD into the task queue.

const maxLiterals = 40

var (
	reLitPort   = regexp.MustCompile(`(?i)\bport\s+(\d{2,5})\b|(?:localhost|127\.0\.0\.1|0\.0\.0\.0):(\d{2,5})\b|\s:(\d{4,5})\b`)
	reLitStatus = regexp.MustCompile(`(?i)\b(?:http|status(?: code)?)\s+([1-5]\d\d)\b`)
	reLitPath   = regexp.MustCompile("(?:^|[\\s(\"'`\\[,;])((?:/[A-Za-z0-9_.{}:-]+)+)")
	reLitTick   = regexp.MustCompile("`([^`\\n]{2,60})`")
	reLitQuote  = regexp.MustCompile(`"([^"\n]*)"`)
)

// ExtractLiterals returns the exact facts in text, each once, in the order
// they first appear: ports, URL paths, HTTP status codes, backticked tokens
// and double-quoted phrases.
func ExtractLiterals(text string) []string {
	type hit struct {
		at  int
		lit string
	}
	var hits []hit
	add := func(at int, lit string, trim bool) {
		lit = strings.TrimSpace(lit)
		if trim {
			lit = strings.TrimRight(lit, ".,;:")
		}
		if lit != "" && (!strings.HasPrefix(lit, "/") || strings.ContainsAny(lit, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")) {
			hits = append(hits, hit{at, lit})
		}
	}
	for _, m := range reLitPort.FindAllStringSubmatchIndex(text, -1) {
		for g := 1; g <= 3; g++ {
			if m[2*g] >= 0 {
				add(m[2*g], text[m[2*g]:m[2*g+1]], true)
			}
		}
	}
	for _, m := range reLitStatus.FindAllStringSubmatchIndex(text, -1) {
		add(m[2], text[m[2]:m[3]], true)
	}
	for _, m := range reLitPath.FindAllStringSubmatchIndex(text, -1) {
		add(m[2], text[m[2]:m[3]], true)
	}
	for _, m := range reLitTick.FindAllStringSubmatchIndex(text, -1) {
		add(m[2], text[m[2]:m[3]], false)
	}
	// Quotes pair up in order, so the gap between two quoted words is never
	// read as a phrase; only a phrase that starts and ends on a letter or
	// digit counts.
	for _, m := range reLitQuote.FindAllStringSubmatchIndex(text, -1) {
		q := text[m[2]:m[3]]
		if len(q) >= 3 && len(q) <= 60 && isAlnum(q[0]) && isAlnum(q[len(q)-1]) {
			add(m[2], q, false)
		}
	}
	// order of appearance
	for i := 1; i < len(hits); i++ {
		for j := i; j > 0 && hits[j].at < hits[j-1].at; j-- {
			hits[j], hits[j-1] = hits[j-1], hits[j]
		}
	}
	seen := map[string]bool{}
	var out []string
	for _, h := range hits {
		k := strings.ToLower(h.lit)
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, h.lit)
		if len(out) == maxLiterals {
			break
		}
	}
	return out
}

func isAlnum(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

func normLiteralText(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(s)), " ")
}

// MissingLiterals returns the literals that do not occur in text (compared
// case-insensitively with whitespace collapsed).
func MissingLiterals(lits []string, text string) []string {
	hay := normLiteralText(text)
	var out []string
	for _, l := range lits {
		if !strings.Contains(hay, normLiteralText(l)) {
			out = append(out, l)
		}
	}
	return out
}

// FRDText is the plain text of the parts of an FRD a reader acts on.
func FRDText(f FRD) string {
	var b strings.Builder
	b.WriteString(f.Title + "\n" + f.Objective + "\n" + f.Context + "\n")
	for _, c := range f.Constraints {
		b.WriteString(c + "\n")
	}
	for _, r := range f.Requirements {
		b.WriteString(r.Description + "\n")
	}
	for _, a := range f.AcceptanceCriteria {
		b.WriteString(a.Criterion + "\n")
	}
	for _, d := range f.Decisions {
		b.WriteString(d.Decision + "\n")
	}
	return b.String()
}

// TasksText is the plain text of a queue a worker acts on.
func TasksText(defs []TaskDef) string {
	var b strings.Builder
	for _, t := range defs {
		b.WriteString(t.Title + "\n" + t.Prompt + "\n")
		for _, a := range t.Acceptance {
			b.WriteString(a + "\n")
		}
		for _, f := range t.FilesTouched {
			b.WriteString(f + "\n")
		}
	}
	return b.String()
}

// LiteralsNote is the prompt section that tells a model which exact strings
// must survive into its output.
func LiteralsNote(lits []string, where string) string {
	if len(lits) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\n## Exact strings that must appear\n\nThese exact strings come from the person's request. Copy each one verbatim into " + where + ", never paraphrase, round or replace them:\n\n")
	for _, l := range lits {
		b.WriteString("- `" + l + "`\n")
	}
	return b.String()
}

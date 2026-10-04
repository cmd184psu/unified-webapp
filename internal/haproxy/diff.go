package haproxy

// A small, self-contained unified line diff for the Review-changes view
// (FR-H20): it shows exactly how the candidate config and crt-list differ from
// what is live. It has no external dependency and is used only on the editor's
// own generated text (haproxy.cfg and the crt-list) — never on a cert file, so
// no key material ever reaches it.

import (
	"fmt"
	"strings"
)

// diffContext is how many unchanged lines of context surround each change in a
// hunk, as in a standard unified diff.
const diffContext = 3

// UnifiedDiff returns a readable unified line diff of oldText vs newText,
// labelled with oldName/newName. It is empty when the two are equal.
func UnifiedDiff(oldName, newName, oldText, newText string) string {
	if oldText == newText {
		return ""
	}
	a := splitDiffLines(oldText)
	b := splitDiffLines(newText)
	ops := diffOps(a, b)

	hunks := groupHunks(ops)
	if len(hunks) == 0 {
		return ""
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "--- %s\n", oldName)
	fmt.Fprintf(&sb, "+++ %s\n", newName)
	for _, h := range hunks {
		fmt.Fprintf(&sb, "@@ -%s +%s @@\n", rangeString(h.aStart, h.aCount), rangeString(h.bStart, h.bCount))
		for _, ln := range h.lines {
			sb.WriteByte(ln.op)
			sb.WriteString(ln.text)
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}

// splitDiffLines splits text into lines, dropping a single trailing newline so
// a normally newline-terminated file does not gain a phantom empty final line.
func splitDiffLines(text string) []string {
	if text == "" {
		return nil
	}
	text = strings.TrimSuffix(text, "\n")
	return strings.Split(text, "\n")
}

// diffLine is one line of the edit script: op is ' ' (common), '-' (removed) or
// '+' (added); aLine/bLine are the 1-based line numbers in old/new (0 when the
// line does not exist on that side).
type diffLine struct {
	op           byte
	text         string
	aLine, bLine int
}

// diffOps computes an LCS-based edit script turning a into b. The inputs are
// small generated files, so the O(n*m) table is fine.
func diffOps(a, b []string) []diffLine {
	n, m := len(a), len(b)
	// c[i][j] = length of the LCS of a[i:] and b[j:].
	c := make([][]int, n+1)
	for i := range c {
		c[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				c[i][j] = c[i+1][j+1] + 1
			} else if c[i+1][j] >= c[i][j+1] {
				c[i][j] = c[i+1][j]
			} else {
				c[i][j] = c[i][j+1]
			}
		}
	}

	var out []diffLine
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case a[i] == b[j]:
			out = append(out, diffLine{op: ' ', text: a[i], aLine: i + 1, bLine: j + 1})
			i++
			j++
		case c[i+1][j] >= c[i][j+1]:
			out = append(out, diffLine{op: '-', text: a[i], aLine: i + 1})
			i++
		default:
			out = append(out, diffLine{op: '+', text: b[j], bLine: j + 1})
			j++
		}
	}
	for ; i < n; i++ {
		out = append(out, diffLine{op: '-', text: a[i], aLine: i + 1})
	}
	for ; j < m; j++ {
		out = append(out, diffLine{op: '+', text: b[j], bLine: j + 1})
	}
	return out
}

// hunk is a contiguous block of the diff with its old/new line ranges.
type hunk struct {
	aStart, aCount int
	bStart, bCount int
	lines          []diffLine
}

// groupHunks turns the full edit script into unified-diff hunks: each run of
// changes is surrounded by up to diffContext unchanged lines, and runs closer
// than that are merged into one hunk.
func groupHunks(ops []diffLine) []hunk {
	// Find indices of changed lines.
	var changed []int
	for i, o := range ops {
		if o.op != ' ' {
			changed = append(changed, i)
		}
	}
	if len(changed) == 0 {
		return nil
	}

	// Build [lo,hi] op-index windows around change runs, merging near ones.
	type window struct{ lo, hi int }
	var windows []window
	for _, idx := range changed {
		lo := idx - diffContext
		if lo < 0 {
			lo = 0
		}
		hi := idx + diffContext
		if hi > len(ops)-1 {
			hi = len(ops) - 1
		}
		if n := len(windows); n > 0 && lo <= windows[n-1].hi+1 {
			if hi > windows[n-1].hi {
				windows[n-1].hi = hi
			}
			continue
		}
		windows = append(windows, window{lo, hi})
	}

	var hunks []hunk
	for _, w := range windows {
		h := hunk{}
		for k := w.lo; k <= w.hi; k++ {
			o := ops[k]
			h.lines = append(h.lines, o)
			if o.op == ' ' || o.op == '-' {
				if h.aCount == 0 {
					h.aStart = o.aLine
				}
				h.aCount++
			}
			if o.op == ' ' || o.op == '+' {
				if h.bCount == 0 {
					h.bStart = o.bLine
				}
				h.bCount++
			}
		}
		hunks = append(hunks, h)
	}
	return hunks
}

// rangeString formats a unified-diff range: "start,count", or "start" when the
// count is 1, and "0,0" for an empty side.
func rangeString(start, count int) string {
	if count == 0 {
		return "0,0"
	}
	if count == 1 {
		return fmt.Sprintf("%d", start)
	}
	return fmt.Sprintf("%d,%d", start, count)
}

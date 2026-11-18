package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"slices"
	"strings"

	"cmd184psu/unified-webapp/internal/agentflow/schema"
	"cmd184psu/unified-webapp/internal/agentflow/verify"
)

var (
	pathRE   = regexp.MustCompile(`(?:[A-Za-z0-9_.~-]*/)+[A-Za-z0-9_.~-]*`)
	hexRE    = regexp.MustCompile(`\b(?:0[xX][0-9a-fA-F]+|[0-9a-fA-F]{7,})\b`)
	digitsRE = regexp.MustCompile(`\d+`)
	spaceRE  = regexp.MustCompile(`\s+`)
	// fileRE splits "path/to/file.go:12:3: message" into file and message.
	fileRE = regexp.MustCompile(`^([\w.+-]*(?:/[\w.+-]+)+|[\w+-]+\.[A-Za-z0-9]+)(?::\d+)*:?\s+(.*)$`)
)

// normalize makes a failure message comparable across attempts: paths, hex
// ids (commit ids, addresses) and numbers (lines, counts, durations) are
// replaced by placeholders, case and spacing are folded.
func normalize(s string) string {
	s = pathRE.ReplaceAllString(s, "<path>")
	s = hexRE.ReplaceAllString(s, "<hex>")
	s = digitsRE.ReplaceAllString(s, "<n>")
	return spaceRE.ReplaceAllString(strings.ToLower(strings.TrimSpace(s)), " ")
}

// splitDetail separates the file a check detail names, if it leads with one,
// from the rest of the message.
func splitDetail(d string) (file, msg string) {
	if m := fileRE.FindStringSubmatch(strings.TrimSpace(d)); m != nil {
		return m[1], m[2]
	}
	return "", d
}

// hashSet fingerprints a set of tuples: sorted, de-duplicated, hashed.
func hashSet(prefix string, tuples []string) string {
	slices.Sort(tuples)
	tuples = slices.Compact(tuples)
	sum := sha256.Sum256([]byte(strings.Join(tuples, "\n")))
	return prefix + hex.EncodeToString(sum[:8])
}

// verifyFingerprint is the set of (check, file, normalised message) of every
// failing check and every new feedback failure (AT4).
func verifyFingerprint(rep verify.VerifyReport) string {
	var t []string
	tuple := func(check, detail string) {
		file, msg := splitDetail(detail)
		t = append(t, check+"\x00"+file+"\x00"+normalize(msg))
	}
	for _, ch := range rep.Checks {
		if ch.Pass {
			continue
		}
		if len(ch.Details) == 0 {
			tuple(ch.Name, "")
		}
		for _, d := range ch.Details {
			tuple(ch.Name, d)
		}
	}
	for _, f := range rep.NewFailures {
		tuple("new_failure", f)
	}
	return hashSet("v:", t)
}

// reviewFingerprint is the set of (file, normalised description) of the
// reviewer's issues; a repair without issues falls back to its repair prompt.
func reviewFingerprint(r schema.ReviewOut) string {
	var t []string
	for _, is := range r.Issues {
		t = append(t, is.File+"\x00"+normalize(is.Description))
	}
	if len(t) == 0 {
		t = append(t, "\x00"+normalize(r.RepairPrompt))
	}
	return hashSet("r:", t)
}

// fingerprintOf is the attempt's failure fingerprint. Failures without
// structure to compare (a timeout, a commit that was refused) fingerprint as
// their kind, so repeating one is stuck as well.
func fingerprintOf(f Failure) string {
	switch f.Kind {
	case FailVerify:
		var rep verify.VerifyReport
		if json.Unmarshal(f.Stage.Parsed, &rep) == nil {
			return verifyFingerprint(rep)
		}
	case FailReview:
		if r, err := schema.Parse[schema.ReviewOut](f.Stage.Parsed); err == nil {
			return reviewFingerprint(r)
		}
	case FailMerge:
		var m mergeReport
		if json.Unmarshal(f.Stage.Parsed, &m) == nil {
			if m.Report != nil {
				return "m" + verifyFingerprint(*m.Report)
			}
			return hashSet("m:", append([]string{m.Kind}, m.Files...))
		}
	}
	return string(f.Kind)
}

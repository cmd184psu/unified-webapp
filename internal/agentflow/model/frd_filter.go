package model

import (
	"regexp"
	"strings"
)

// The engine builds, vets and tests every task and holds it to the project's
// definition of done, so a drafted FRD must not carry criteria that only
// restate that ("go test passes", "the build succeeds", "new code has
// tests"). DropRestatedCriteria removes them after parsing; the drafter prompt
// asks the model not to write them, this makes it certain.
var (
	restateSubject = `(?:(?:the |all |any |every |existing |new |unit |integration |full |whole |entire |project |go |python |npm )*` +
		`(?:go test|go build|go vet|gofmt|pytest|npm test|tests?|test suite|builds?|lint(?:ing)?|vet|compilation|static analysis|type ?check(?:s|ing)?)` +
		`(?: -[a-z]+(?:=\S+)?)*(?: \./\.\.\.)?(?: suite)?|(?:the )?(?:project|code ?base|code|repo(?:sitory)?|module|package))`
	restateVerb = `(?:all |still |both |should |must |will |continues? to )*(?:pass(?:es|ing)?|succeed(?:s)?|compile(?:s)?|build(?:s)?|run(?:s)? (?:clean(?:ly)?|successfully|without errors?)|` +
		`(?:is|are) (?:green|clean|successful|passing)|(?:complete|finish)(?:es)? (?:successfully|without errors?)|exit(?:s)? (?:with )?(?:code )?0|` +
		`(?:report(?:s)? )?no (?:errors?|failures?|warnings?)|without (?:errors?|failures?))` +
		`(?: (?:with no (?:errors?|failures?)|without (?:errors?|failures?)|successfully|cleanly))*`
	restateClause = restateSubject + `(?: and | & |, | ?, and )?(?:` + restateSubject + `(?: and | & |, | ?, and )?)* ` + restateVerb
	restatePhrase = regexp.MustCompile(`^(?:(?:run|running|ensure|make sure|verify|confirm)(?: that)? )?` +
		restateClause + `(?:(?: and |, | ?, and )` + restateClause + `)*$`)
	restateNoFailures = regexp.MustCompile(`^(?:there (?:are|is) )?no (?:(?:test|build|compile|compilation|vet|lint) )?(?:failures?|errors?|warnings?)(?: (?:in|from) (?:the )?(?:tests?|build|vet|lint))?$`)
	restateFixed      = []*regexp.Regexp{
		regexp.MustCompile(`^no unrelated files (?:were |are )?(?:changed|modified|touched)$`),
		regexp.MustCompile(`^no files (?:outside|beyond|other than)(?: of)? the tasks?(?: declared)?(?: scope| files) (?:were |are )?(?:changed|modified|touched)$`),
		regexp.MustCompile(`^gofmt(?: -[a-z]+)* (?:reports|shows|finds|produces) no (?:diff|differences|changes|output|issues|files)$`),
		regexp.MustCompile(`^(?:the |all )?(?:code|project|repo(?:sitory)?|changed files|files) (?:is|are) gofmt[- ]clean$`),
		regexp.MustCompile(`^new (?:code|functionality|behaviou?r) (?:has|have|is covered by|includes?|is tested by|comes with) (?:unit |automated )?tests?$`),
		regexp.MustCompile(`^no (?:new )?(?:secrets?|credentials?)(?: or credentials?)?(?: (?:are |is )?(?:added|committed|introduced|leaked))?$`),
	}
	restateSpaces = regexp.MustCompile(`\s+`)
)

// normalizeCriterion lower-cases a criterion and strips quoting, trailing
// punctuation and repeated spaces, so matching sees only the words.
func normalizeCriterion(s string) string {
	s = strings.ToLower(strings.NewReplacer("`", "", "\"", "", "'", "").Replace(s))
	s = restateSpaces.ReplaceAllString(strings.TrimSpace(s), " ")
	return strings.TrimRight(s, " .;:!")
}

// restatesDoD reports whether the criterion says nothing the definition of
// done does not: a verification-only phrase, a fixed line, or one of the
// definition's own items (norm is the normalized item list).
func restatesDoD(criterion string, norm map[string]bool) bool {
	c := normalizeCriterion(criterion)
	if c == "" {
		return false
	}
	if norm[c] || restatePhrase.MatchString(c) || restateNoFailures.MatchString(c) {
		return true
	}
	for _, re := range restateFixed {
		if re.MatchString(c) {
			return true
		}
	}
	return false
}

// DropRestatedCriteria removes the acceptance criteria that restate the
// definition of done (dod are its items) and returns the dropped texts. It
// never removes every criterion: when all would go they are kept, nothing is
// returned as dropped and keptAll is true, so the caller can record it.
func DropRestatedCriteria(f *FRD, dod []string) (dropped []string, keptAll bool) {
	norm := map[string]bool{}
	for _, it := range dod {
		norm[normalizeCriterion(it)] = true
	}
	kept := f.AcceptanceCriteria[:0:0]
	for _, ac := range f.AcceptanceCriteria {
		if restatesDoD(ac.Criterion, norm) {
			dropped = append(dropped, ac.Criterion)
			continue
		}
		kept = append(kept, ac)
	}
	if len(kept) == 0 && len(dropped) > 0 {
		return nil, true
	}
	f.AcceptanceCriteria = kept
	return dropped, false
}

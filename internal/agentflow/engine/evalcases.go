package engine

import "strings"

// Seeded-defect cases for "Check reviewers" (H32). Each case is a small
// project with an FRD of two requirements: Target, which the Defect variant of
// the files breaks and the Fixed variant meets, and Other, which both meet. A
// reviewer that is asked about the Defect variant should find the Target gap;
// asked about the Fixed variant it should find nothing. The first four cases
// are the four defects the first hybrid trial (effort 16) let through.

type evalFile struct{ Path, Content string }

type evalCase struct {
	ID     string
	Title  string
	Target string // the requirement id the defect breaks
	// FRD is the compact FRD JSON the reviewer is shown.
	FRD string
	// Task is what a per-task review is asked to judge.
	TaskTitle  string
	TaskPrompt string
	Acceptance []string
	Defect     []evalFile
	Fixed      []evalFile
}

// swap returns files with the one at path replaced by content.
func swap(files []evalFile, path, content string) []evalFile {
	out := make([]evalFile, len(files))
	copy(out, files)
	for i := range out {
		if out[i].Path == path {
			out[i].Content = content
		}
	}
	return out
}

const evalGoMod = "module calc\n\ngo 1.22\n"

const evalHelpers = `package main

import (
	"encoding/json"
	"net/http"
)

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, msg string) {
	writeJSON(w, http.StatusBadRequest, map[string]string{"error": msg})
}
`

var evalCases = []evalCase{
	pageCase(),
	powerCase(),
	juxtaposedCase(),
	primesCase(),
	wrongRouteCase(),
}

func pageCase() evalCase {
	main := `package main

import (
	"encoding/json"
	"net/http"
)

type pair struct {
	A *float64 ` + "`json:\"a\"`" + `
	B *float64 ` + "`json:\"b\"`" + `
}

func addHandler(w http.ResponseWriter, r *http.Request) {
	var p pair
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil || p.A == nil || p.B == nil {
		fail(w, "a and b must be numbers")
		return
	}
	writeJSON(w, http.StatusOK, map[string]float64{"result": *p.A + *p.B})
}

func main() {
	http.HandleFunc("POST /api/add", addHandler)
	http.Handle("GET /", http.FileServer(http.Dir(".")))
	_ = http.ListenAndServe(":7070", nil)
}
`
	bad := `<!doctype html>
<html><body>
<input id="display" value="" readonly>
<button onclick="press('1')">1</button><button onclick="press('2')">2</button>
<button onclick="press('+')">+</button><button onclick="equals()">=</button>
<script>
const display = document.getElementById('display');
function press(c) { display.value += c; }
function equals() {
  const parts = display.value.split('+');
  display.value = String(Number(parts[0]) + Number(parts[1]));
}
</script>
</body></html>
`
	good := `<!doctype html>
<html><body>
<input id="display" value="" readonly>
<button onclick="press('1')">1</button><button onclick="press('2')">2</button>
<button onclick="press('+')">+</button><button onclick="equals()">=</button>
<script>
const display = document.getElementById('display');
function press(c) { display.value += c; }
async function equals() {
  const parts = display.value.split('+');
  const r = await fetch('/api/add', {method: 'POST', headers: {'Content-Type': 'application/json'},
    body: JSON.stringify({a: Number(parts[0]), b: Number(parts[1])})});
  const body = await r.json();
  display.value = r.ok ? String(body.result) : body.error;
}
</script>
</body></html>
`
	base := []evalFile{{"go.mod", evalGoMod}, {"helpers.go", evalHelpers}, {"main.go", main}, {"index.html", bad}}
	return evalCase{
		ID: "page-through-api", Title: "the page does its own arithmetic", Target: "R1",
		FRD: `{"title":"Calculator page","objective":"A calculator page backed by a Go server.","requirements":[` +
			`{"id":"R1","description":"GET / serves a page with digit buttons, a plus button and an equals button. All arithmetic is performed by the Go API calls (POST /api/add) and never in JavaScript: the page script only sends the operands and shows the answer.","priority":"must"},` +
			`{"id":"R2","description":"POST /api/add takes JSON {\"a\": number, \"b\": number} and returns {\"result\": number}; a missing or non-numeric field returns HTTP 400 with {\"error\": message}.","priority":"must"}],` +
			`"acceptance_criteria":[{"id":"AC1","requirements":["R1"],"criterion":"pressing 1 + 2 = shows 3 using the API"}]}`,
		TaskTitle:  "Calculator page",
		TaskPrompt: "Write index.html: digit buttons, plus and equals. Equals must send the two operands to POST /api/add and show the result.",
		Acceptance: []string{"R1: all arithmetic is performed by the Go API calls and never in JavaScript", "AC1: pressing 1 + 2 = shows 3 using the API"},
		Defect:     base,
		Fixed:      swap(base, "index.html", good),
	}
}

func powerCase() evalCase {
	head := `package main

import (
	"encoding/json"
	"math"
	"net/http"
)

type pair struct {
	A *float64 ` + "`json:\"a\"`" + `
	B *float64 ` + "`json:\"b\"`" + `
}

func readPair(w http.ResponseWriter, r *http.Request) (pair, bool) {
	var p pair
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil || p.A == nil || p.B == nil {
		fail(w, "a and b must be numbers")
		return p, false
	}
	return p, true
}

func divideHandler(w http.ResponseWriter, r *http.Request) {
	p, ok := readPair(w, r)
	if !ok {
		return
	}
	if *p.B == 0 {
		fail(w, "division by zero")
		return
	}
	writeJSON(w, http.StatusOK, map[string]float64{"result": *p.A / *p.B})
}

`
	tail := `
func main() {
	http.HandleFunc("POST /api/divide", divideHandler)
	http.HandleFunc("POST /api/power", powerHandler)
	_ = http.ListenAndServe(":7070", nil)
}
`
	bad := head + `func powerHandler(w http.ResponseWriter, r *http.Request) {
	p, ok := readPair(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]float64{"result": math.Pow(*p.A, *p.B)})
}
` + tail
	good := head + `func powerHandler(w http.ResponseWriter, r *http.Request) {
	p, ok := readPair(w, r)
	if !ok {
		return
	}
	res := math.Pow(*p.A, *p.B)
	if math.IsInf(res, 0) || math.IsNaN(res) {
		fail(w, "domain error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]float64{"result": res})
}
` + tail
	base := []evalFile{{"go.mod", evalGoMod}, {"helpers.go", evalHelpers}, {"main.go", bad}}
	return evalCase{
		ID: "power-not-finite", Title: "a power that is not finite is answered with 200", Target: "R1",
		FRD: `{"title":"Power","objective":"Arithmetic endpoints.","requirements":[` +
			`{"id":"R1","description":"POST /api/power takes JSON {\"a\": number, \"b\": number} and returns {\"result\": a to the power b}; a power whose result is not a finite number (such as 0 to the power -1, or 10 to the power 1000) returns HTTP 400 with {\"error\": \"domain error\"}.","priority":"must"},` +
			`{"id":"R2","description":"POST /api/divide takes {\"a\": number, \"b\": number}; dividing by zero returns HTTP 400 with {\"error\": \"division by zero\"}.","priority":"must"}],` +
			`"acceptance_criteria":[{"id":"AC1","requirements":["R1"],"criterion":"power of 0 and -1 gives a domain error"}]}`,
		TaskTitle:  "Power endpoint",
		TaskPrompt: "Add POST /api/power in main.go, answering the errors the requirement names.",
		Acceptance: []string{"R1: a power whose result is not a finite number returns HTTP 400 with {\"error\": \"domain error\"}", "AC1: power of 0 and -1 gives a domain error"},
		Defect:     base,
		Fixed:      swap(base, "main.go", good),
	}
}

func juxtaposedCase() evalCase {
	evalDefect := `package main

import (
	"errors"
	"strconv"
	"strings"
)

var errInvalid = errors.New("invalid expression")

// Evaluate computes a sum or difference of decimal numbers such as "2 + 3 - 1".
func Evaluate(expr string) (float64, error) {
	expr = strings.ReplaceAll(expr, " ", "")
	if expr == "" {
		return 0, errInvalid
	}
	total, sign, start := 0.0, 1.0, 0
	for i := 0; i <= len(expr); i++ {
		if i < len(expr) && expr[i] != '+' && expr[i] != '-' {
			continue
		}
		if i == start {
			return 0, errInvalid
		}
		n, err := strconv.ParseFloat(expr[start:i], 64)
		if err != nil {
			return 0, errInvalid
		}
		total += sign * n
		if i < len(expr) {
			sign = 1
			if expr[i] == '-' {
				sign = -1
			}
		}
		start = i + 1
	}
	return total, nil
}
`
	evalFixed := `package main

import (
	"errors"
	"strconv"
	"strings"
)

var errInvalid = errors.New("invalid expression")

// Evaluate computes a sum or difference of decimal numbers such as "2 + 3 - 1".
func Evaluate(expr string) (float64, error) {
	fields := strings.Fields(strings.NewReplacer("+", " + ", "-", " - ").Replace(expr))
	if len(fields) == 0 {
		return 0, errInvalid
	}
	total, sign, wantNumber := 0.0, 1.0, true
	for _, f := range fields {
		if wantNumber {
			n, err := strconv.ParseFloat(f, 64)
			if err != nil {
				return 0, errInvalid
			}
			total += sign * n
			wantNumber = false
			continue
		}
		switch f {
		case "+":
			sign = 1
		case "-":
			sign = -1
		default:
			return 0, errInvalid // two numbers with no operator between them
		}
		wantNumber = true
	}
	if wantNumber {
		return 0, errInvalid
	}
	return total, nil
}
`
	handler := `package main

import (
	"encoding/json"
	"net/http"
)

func evaluateHandler(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Expression *string ` + "`json:\"expression\"`" + `
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Expression == nil {
		fail(w, "expression is required")
		return
	}
	res, err := Evaluate(*in.Expression)
	if err != nil {
		fail(w, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]float64{"result": res})
}

func main() {
	http.HandleFunc("POST /api/evaluate", evaluateHandler)
	_ = http.ListenAndServe(":7070", nil)
}
`
	base := []evalFile{{"go.mod", evalGoMod}, {"helpers.go", evalHelpers}, {"main.go", handler}, {"evaluator.go", evalDefect}}
	return evalCase{
		ID: "juxtaposed-numbers", Title: "two numbers with no operator read as one", Target: "R1",
		FRD: `{"title":"Evaluate","objective":"Evaluate a typed expression.","requirements":[` +
			`{"id":"R1","description":"POST /api/evaluate takes {\"expression\": string} and returns {\"result\": number} for decimal numbers joined by + and -, with optional spaces. Two numbers with no operator between them, such as \"2 3\", are invalid and return HTTP 400 with {\"error\": \"invalid expression\"}.","priority":"must"},` +
			`{"id":"R2","description":"A missing expression returns HTTP 400 with {\"error\": message}; an expression that ends in an operator (\"2 +\") returns HTTP 400 with {\"error\": \"invalid expression\"}.","priority":"must"}],` +
			`"acceptance_criteria":[{"id":"AC1","requirements":["R1"],"criterion":"\"2 3\" is rejected"}]}`,
		TaskTitle:  "Expression evaluator",
		TaskPrompt: "Write Evaluate in evaluator.go for sums and differences, rejecting every invalid shape the requirement names.",
		Acceptance: []string{"R1: two numbers with no operator between them are invalid and return 400 {\"error\": \"invalid expression\"}", "AC1: \"2 3\" is rejected"},
		Defect:     base,
		Fixed:      swap(base, "evaluator.go", evalFixed),
	}
}

func primesCase() evalCase {
	head := `package main

import (
	"encoding/json"
	"errors"
	"math"
	"net/http"
)

func firstPrimes(n int) []int {
	out := []int{}
	for c := 2; len(out) < n; c++ {
		isPrime := true
		for d := 2; d*d <= c; d++ {
			if c%d == 0 {
				isPrime = false
				break
			}
		}
		if isPrime {
			out = append(out, c)
		}
	}
	return out
}

`
	tail := `
func primesHandler(w http.ResponseWriter, r *http.Request) {
	n, err := readCount(r)
	if err != nil {
		fail(w, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string][]int{"result": firstPrimes(n)})
}

func main() {
	http.HandleFunc("POST /api/primes", primesHandler)
	_ = http.ListenAndServe(":7070", nil)
}
`
	bad := strings.Replace(head, "\t\"math\"\n", "", 1) + `func readCount(r *http.Request) (int, error) {
	var in struct {
		Count *int ` + "`json:\"count\"`" + `
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		return 0, err
	}
	if in.Count == nil {
		return 0, errors.New("count is required")
	}
	if *in.Count < 1 || *in.Count > 1000 {
		return 0, errors.New("domain error")
	}
	return *in.Count, nil
}
` + tail
	good := head + `func readCount(r *http.Request) (int, error) {
	var in struct {
		Count *float64 ` + "`json:\"count\"`" + `
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		return 0, errors.New("body must be JSON")
	}
	if in.Count == nil {
		return 0, errors.New("count is required")
	}
	if *in.Count != math.Trunc(*in.Count) || *in.Count < 1 || *in.Count > 1000 {
		return 0, errors.New("domain error")
	}
	return int(*in.Count), nil
}
` + tail
	base := []evalFile{{"go.mod", evalGoMod}, {"helpers.go", evalHelpers}, {"main.go", bad}}
	return evalCase{
		ID: "fractional-count", Title: "a fractional count gets the wrong error", Target: "R1",
		FRD: `{"title":"Primes","objective":"List primes.","requirements":[` +
			`{"id":"R1","description":"POST /api/primes takes {\"count\": integer from 1 to 1000} and returns {\"result\": [the first count prime numbers]}. Any count outside that range, or not a whole number (such as 2.5), returns HTTP 400 with {\"error\": \"domain error\"}.","priority":"must"},` +
			`{"id":"R2","description":"A missing count returns HTTP 400 with {\"error\": message}.","priority":"must"}],` +
			`"acceptance_criteria":[{"id":"AC1","requirements":["R1"],"criterion":"count 2.5 gives a domain error"}]}`,
		TaskTitle:  "Primes endpoint",
		TaskPrompt: "Add POST /api/primes in main.go with the validation the requirement names.",
		Acceptance: []string{"R1: a count that is out of range or not a whole number returns 400 {\"error\": \"domain error\"}", "AC1: count 2.5 gives a domain error"},
		Defect:     base,
		Fixed:      swap(base, "main.go", good),
	}
}

func wrongRouteCase() evalCase {
	main := `package main

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

func evaluateHandler(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Expression *string ` + "`json:\"expression\"`" + `
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Expression == nil {
		fail(w, "expression is required")
		return
	}
	n, err := strconv.ParseFloat(strings.TrimSpace(*in.Expression), 64)
	if err != nil {
		fail(w, "invalid expression")
		return
	}
	writeJSON(w, http.StatusOK, map[string]float64{"result": n})
}

func main() {
	http.HandleFunc("POST /api/evaluate", evaluateHandler)
	http.Handle("GET /", http.FileServer(http.Dir(".")))
	_ = http.ListenAndServe(":7070", nil)
}
`
	page := func(call string) string {
		return `<!doctype html>
<html><body>
<input id="display" value="">
<button onclick="equals()">=</button>
<ul id="history"></ul>
<script>
const display = document.getElementById('display');
async function equals() {
  ` + call + `
  const body = await r.json();
  display.value = r.ok ? String(body.result) : body.error;
  const li = document.createElement('li');
  li.textContent = display.value;
  document.getElementById('history').prepend(li);
}
</script>
</body></html>
`
	}
	bad := page("const r = await fetch('/?expression=' + encodeURIComponent(display.value));")
	good := page("const r = await fetch('/api/evaluate', {method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify({expression: display.value})});")
	base := []evalFile{{"go.mod", evalGoMod}, {"helpers.go", evalHelpers}, {"main.go", main}, {"index.html", bad}}
	return evalCase{
		ID: "page-wrong-route", Title: "the page calls a route the server does not have", Target: "R1",
		FRD: `{"title":"Calculator page","objective":"A page over the evaluate endpoint.","requirements":[` +
			`{"id":"R1","description":"GET / serves a page with an equals button. Pressing it sends the typed expression to POST /api/evaluate as {\"expression\": string} and shows the result or the error; the page calls no other URL and does no arithmetic itself.","priority":"must"},` +
			`{"id":"R2","description":"POST /api/evaluate returns {\"result\": number}, or HTTP 400 with {\"error\": message} when the expression field is missing.","priority":"must"}],` +
			`"acceptance_criteria":[{"id":"AC1","requirements":["R1"],"criterion":"pressing equals shows the server's answer"}]}`,
		TaskTitle:  "Calculator page",
		TaskPrompt: "Write index.html with an equals button that evaluates the typed expression through the server and lists the answers.",
		Acceptance: []string{"R1: pressing equals sends the typed expression to POST /api/evaluate and shows the result; the page calls no other URL", "AC1: pressing equals shows the server's answer"},
		Defect:     base,
		Fixed:      swap(base, "index.html", good),
	}
}

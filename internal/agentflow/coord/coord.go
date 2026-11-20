package coord

import (
	"context"
	"fmt"
	"log"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// Decision is one thing a run decided to do. In a dry run it is all that happens.
type Decision struct {
	Kind   string `json:"kind"` // review_recorded, rework, review_created, merge, merge_blocked, accept, alert, finished, fed, queue_empty, deliver, transition
	Task   string `json:"task,omitempty"`
	Seat   string `json:"seat,omitempty"`
	Detail string `json:"detail"`
}

// Report is one run.
type Report struct {
	Started   time.Time  `json:"started"`
	Finished  time.Time  `json:"finished"`
	Dry       bool       `json:"dry"`
	Decisions []Decision `json:"decisions"`
	// Writes are the BotChat writes a dry run held back; Queues the queue files it would have
	// written, by callsign.
	Writes []string          `json:"writes,omitempty"`
	Queues map[string]string `json:"queues,omitempty"`
	Error  string            `json:"error,omitempty"`
}

// Git is what the coordinator does with the repositories.
type Git interface {
	// RemoteHas reports whether origin has the branch; Merged whether it is already in base.
	RemoteHas(ctx context.Context, r Repo, branch string) bool
	Merged(ctx context.Context, r Repo, branch, base string) bool
	// Merge merges origin/branch into base in a scratch worktree, runs the gates and pushes
	// base. It returns why it did not, leaving origin untouched.
	Merge(ctx context.Context, r Repo, base, branch, tid string, approved bool) error
	// Contains lists the remote branches of any repository whose history has sha.
	Contains(ctx context.Context, sha string) []string
}

// Coordinator runs the coordinator chores.
type Coordinator struct {
	Cfg *Config
	Bus Bus
	Git Git
	Now func() time.Time

	mu   sync.Mutex // one run at a time
	last *Report
}

// Last is the newest run's report.
func (c *Coordinator) Last() *Report {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.last
}

type run struct {
	c       *Coordinator
	cfg     *Config
	bus     Bus
	dry     bool
	st      *State
	rep     *Report
	tasks   []Task
	byID    map[string]Task
	rooms   map[string][]Msg
	reviews []Msg
}

func (r *run) decide(kind, task, seat, format string, a ...any) {
	d := Decision{Kind: kind, Task: task, Seat: seat, Detail: fmt.Sprintf(format, a...)}
	r.rep.Decisions = append(r.rep.Decisions, d)
	r.c.logLine(kind+" "+strings.TrimSpace(task+" "+seat)+": "+d.Detail, r.dry)
}

func (r *run) alert(task, format string, a ...any) {
	msg := fmt.Sprintf(format, a...)
	r.decide("alert", task, "", "%s", msg)
	if !r.dry && r.cfg.AlertFile != "" {
		appendLine(r.cfg.AlertFile, r.c.now().Format("01-02 15:04")+" "+msg)
	}
}

func (c *Coordinator) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func (c *Coordinator) logLine(s string, dry bool) {
	if dry {
		s = "DRY " + s
	}
	log.Printf("coord: %s", s)
	if c.Cfg.LogFile != "" && !dry {
		appendLine(c.Cfg.LogFile, c.now().Format("01-02 15:04:05")+" [agentflow] "+s)
	}
}

func appendLine(path, line string) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		log.Printf("coord: %s: %v", path, err)
		return
	}
	defer f.Close()
	_, _ = f.WriteString(line + "\n")
}

// Run does one coordinator pass. dry (or Config.DryRun) decides without doing: no BotChat
// write, no merge, no queue file or state written.
func (c *Coordinator) Run(ctx context.Context, dry bool) *Report {
	c.mu.Lock()
	defer c.mu.Unlock()
	dry = dry || c.Cfg.DryRun
	rep := &Report{Started: c.now(), Dry: dry}
	bus := c.Bus
	var db *DryBus
	if dry {
		db = &DryBus{Bus: c.Bus}
		bus = db
	}
	r := &run{c: c, cfg: c.Cfg, bus: bus, dry: dry, rep: rep}
	if err := r.do(ctx); err != nil {
		rep.Error = err.Error()
		r.alert("", "coordinator run failed: %v", err)
	}
	if db != nil {
		rep.Writes = db.Writes()
	}
	rep.Finished = c.now()
	c.last = rep
	return rep
}

func (r *run) do(ctx context.Context) error {
	st, err := LoadState(r.cfg.StateFile)
	if err != nil {
		return err
	}
	r.st = st
	if r.tasks, err = r.bus.Tasks(ctx); err != nil {
		return err
	}
	r.byID = map[string]Task{}
	for _, t := range r.tasks {
		r.byID[t.ID] = t
	}
	if r.reviews, err = r.bus.Messages(ctx, r.cfg.ReviewRoom); err != nil {
		return err
	}
	r.rooms = map[string][]Msg{}
	for _, room := range r.cfg.Rooms {
		if r.rooms[room], err = r.bus.Messages(ctx, room); err != nil {
			return err
		}
	}
	open := map[string]int{}
	for _, t := range r.tasks {
		if t.PlanID == r.cfg.ReviewPlan && in(t.State, "ready", "working", "acknowledged", "assigned", "rework") {
			open[t.Assignee]++
		}
	}
	reworked := r.recordReviews(ctx)
	r.devTasks(ctx, reworked, open)
	tasks, err := r.bus.Tasks(ctx) // the reviews and merges above changed the board
	if err != nil {
		return err
	}
	if !r.dry {
		r.tasks = tasks
	}
	if err := r.feed(ctx); err != nil {
		r.alert("", "feed failed: %v", err)
	}
	if r.dry {
		return nil
	}
	return r.st.Save(r.cfg.StateFile)
}

func in(s string, set ...string) bool {
	for _, x := range set {
		if s == x {
			return true
		}
	}
	return false
}

var reTaskNum = regexp.MustCompile(`TASK-(\d+)`)

// verdictOf finds the reviewer's verdict for a review task: only an explicit verdict line
// counts (avoids matching words like "mutation pass").
func verdictOf(review Task, msgs []Msg) (kind, text string) {
	rnum := strings.TrimPrefix(review.ID, "TASK-")
	dev := reTaskNum.FindStringSubmatch(review.Title + " " + review.Objective)
	reR := regexp.MustCompile(`\bR` + regexp.QuoteMeta(rnum) + `\b`)
	for _, m := range msgs {
		if m.Sender() != review.Assignee {
			continue
		}
		head := clipStr(m.Content, 220)
		if !(strings.Contains(head, "TASK-"+rnum) || reR.MatchString(head) || (dev != nil && strings.Contains(head, "TASK-"+dev[1]))) {
			continue
		}
		if mm := reVerdict.FindStringSubmatch(clipStr(m.Content, 400)); mm != nil {
			kind, text = mm[1], m.Content
		} else if mm := reVerdictLine.FindStringSubmatch(clipStr(m.Content, 1500)); mm != nil {
			kind, text = mm[1], m.Content
		}
	}
	return kind, text
}

var (
	reVerdict     = regexp.MustCompile(`REVIEW\s+(?:TASK-\d+|CAL-\d+|R\d+)[^:\n]{0,40}:\s*(PASS|CHANGES-REQUESTED)\b`)
	reVerdictLine = regexp.MustCompile(`VERDICT:\s*(PASS|CHANGES-REQUESTED)\b`)
)

func clipStr(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// expand reads a message that is only "@/tmp/file" (seats sometimes post that instead of the
// text), only under /tmp.
func expand(m Msg) Msg {
	c := strings.TrimSpace(m.Content)
	if strings.HasPrefix(c, "@/tmp/") && !strings.Contains(c, "\n") {
		if b, err := os.ReadFile(c[1:]); err == nil {
			if real, err := realPath(c[1:]); err == nil && (strings.HasPrefix(real, "/tmp/") || strings.HasPrefix(real, "/private/tmp/")) {
				m.Content = clipStr(string(b), 20000)
			}
		}
	}
	return m
}

// doneMessage is the newest DONE message by the task's assignee naming the task.
func doneMessage(t Task, msgs []Msg) string {
	out := ""
	for _, m := range msgs {
		m = expand(m)
		head := clipStr(m.Content, 80)
		if m.Sender() == t.Assignee && strings.Contains(head, t.ID) && strings.Contains(head, "DONE") {
			out = m.Content
		}
	}
	return out
}

// transition applies an action as the coordinator, and on behalf of the seat when the board
// refuses the coordinator (the owner approved acting for the seats).
func (r *run) transition(ctx context.Context, id, action, note, onBehalf string) bool {
	for _, from := range append([]string{r.cfg.Me}, nonEmpty(onBehalf)...) {
		n := note
		if from != r.cfg.Me {
			n = "Recorded by " + r.cfg.Me + " on behalf of " + from + " (owner-approved): " + note
		}
		if err := r.bus.Transition(ctx, id, from, action, clipStr(n, 1500)); err != nil {
			r.c.logLine(fmt.Sprintf("transition %s %s as %s failed: %v", id, action, from, err), r.dry)
			continue
		}
		return true
	}
	return false
}

func nonEmpty(s string) []string {
	if s == "" {
		return nil
	}
	return []string{s}
}

// recordReviews closes finished review tasks with their verdict; CHANGES-REQUESTED sends the
// dev task back to rework. It returns the dev tasks sent back.
func (r *run) recordReviews(ctx context.Context) map[string]bool {
	reworked := map[string]bool{}
	for _, t := range r.tasks {
		if t.PlanID != r.cfg.ReviewPlan || t.State != "done" {
			continue
		}
		kind, text := verdictOf(t, r.reviews)
		if kind == "" {
			r.c.logLine("review "+t.ID+" done but no verdict found", r.dry)
			continue
		}
		did := ""
		if m := reTaskNum.FindStringSubmatch(t.Title + " " + t.Objective); m != nil {
			did = "TASK-" + m[1]
		}
		if kind == "PASS" {
			if did != "" {
				set(r.st.dev(did), "pass", t.ID)
			}
		} else if did != "" {
			d := r.st.dev(did)
			set(d, "cr", t.ID)
			set(d, "status", "rework")
			reworked[did] = true
			if dev := r.byID[did]; dev.State == "done" {
				who := dev.ClaimedBy
				if who == "" {
					who = dev.Assignee
				}
				r.decide("rework", did, who, "review %s asked for changes", t.ID)
				r.transition(ctx, did, "rework", "Review "+t.ID+" CHANGES-REQUESTED (read it in review-desk): "+strings.ReplaceAll(clipStr(text, 900), "\n", " "), who)
			}
		}
		r.decide("review_recorded", t.ID, t.Assignee, "%s for %s", kind, did)
		r.transition(ctx, t.ID, "accept", "Verdict recorded: "+kind, "")
		r.transition(ctx, t.ID, "close", "Recorded.", "")
	}
	return reworked
}

// devTasks creates reviews for new deliveries and merges passed ones.
func (r *run) devTasks(ctx context.Context, reworked map[string]bool, open map[string]int) {
	merges := 0
	var all []Msg
	for _, room := range r.cfg.Rooms {
		all = append(all, r.rooms[room]...)
	}
	for _, t := range r.tasks {
		if t.PlanID == r.cfg.ReviewPlan || t.State != "done" || !in(t.PlanID, r.cfg.OurPlans...) || reworked[t.ID] {
			continue
		}
		d := r.st.dev(t.ID)
		if get(d, "status") == "blocked" {
			continue // PASS but the merge was blocked: resolved by hand, never re-reviewed
		}
		if get(d, "status") != "done" {
			set(d, "status", "done")
			for _, k := range []string{"pass", "cr", "review"} {
				d[k] = nil
			}
		}
		if !in(t.Assignee, r.cfg.Authors...) {
			continue
		}
		switch {
		case get(d, "pass") != "" && merges < r.cfg.maxMerges():
			if r.merge(ctx, t, d, all) {
				merges++
			}
		case get(d, "review") == "" && get(d, "status") == "done":
			if rv := r.pickReviewer(t, open); rv != "" {
				id := r.createReview(ctx, t, rv)
				set(d, "review", id)
				open[rv]++
				r.decide("review_created", t.ID, rv, "review %s", id)
			}
		}
	}
}

func (r *run) merge(ctx context.Context, t Task, d map[string]*string, all []Msg) bool {
	pass := get(d, "pass")
	fallback := ""
	if rv, ok := r.byID[pass]; ok {
		fallback = rv.Title + "\n" + rv.Objective
	}
	refs := BranchesIn(doneMessage(t, all), r.cfg.Base, func(sha string) []string { return r.c.Git.Contains(ctx, sha) }, fallback)
	if len(refs) == 0 {
		r.alert(t.ID, "%s has a PASS review (%s) but no branch found in its DONE message: MERGE BY HAND", t.ID, pass)
		set(d, "status", "blocked")
		return false
	}
	approved := false
	for _, m := range r.reviews {
		if strings.Contains(clipStr(m.Content, 200), t.ID) && m.Sender() != r.cfg.Me && strings.Contains(strings.ToLower(m.Content), "approv") {
			approved = true
		}
	}
	var did []string
	for _, ref := range refs {
		for _, repo := range r.cfg.Repos {
			base := repo.base(r.cfg)
			if !r.c.Git.RemoteHas(ctx, repo, ref.Branch) || r.c.Git.Merged(ctx, repo, ref.Branch, base) {
				continue
			}
			r.decide("merge", t.ID, "", "%s into %s in %s", ref.Branch, base, repo.Name)
			if !r.dry {
				if err := r.c.Git.Merge(ctx, repo, base, ref.Branch, t.ID, approved); err != nil {
					r.alert(t.ID, "%s merge blocked: %v", t.ID, err)
					set(d, "pass", "")
					set(d, "status", "blocked")
					return false
				}
			}
			did = append(did, ref.Branch)
		}
	}
	if len(did) == 0 {
		var names []string
		for _, ref := range refs {
			names = append(names, ref.Branch)
		}
		r.alert(t.ID, "%s has a PASS review but nothing to merge (already merged or branches missing: %s)", t.ID, strings.Join(names, ", "))
		set(d, "status", "merged?")
		return false
	}
	r.decide("accept", t.ID, t.ClaimedBy, "PASS review %s; merged %s", pass, strings.Join(did, ", "))
	r.transition(ctx, t.ID, "accept", "PASS review "+pass+"; merged: "+strings.Join(did, ", "), t.ClaimedBy)
	r.transition(ctx, t.ID, "close", "Merged.", "")
	set(d, "status", "merged")
	return true
}

// pickReviewer is a reviewer of another model family than the author, the least loaded first.
func (r *run) pickReviewer(dev Task, open map[string]int) string {
	var cands []string
	for _, rv := range r.cfg.Reviewers {
		if rv != dev.Assignee && r.cfg.family(rv) != r.cfg.family(dev.Assignee) {
			cands = append(cands, rv)
		}
	}
	engine := dev.PlanID == r.cfg.EnginePlan
	if r.cfg.EngineTitles != "" {
		if re, err := regexp.Compile(r.cfg.EngineTitles); err == nil && re.MatchString(dev.Title) {
			engine = true
		}
	}
	if engine && len(r.cfg.StrongReviewers) > 0 {
		kept := cands[:0]
		for _, c := range cands {
			if in(c, r.cfg.StrongReviewers...) {
				kept = append(kept, c)
			}
		}
		cands = kept
	}
	idx := func(s string) int {
		for i, x := range r.cfg.Reviewers {
			if x == s {
				return i
			}
		}
		return len(r.cfg.Reviewers)
	}
	sort.SliceStable(cands, func(i, j int) bool {
		if open[cands[i]] != open[cands[j]] {
			return open[cands[i]] < open[cands[j]]
		}
		return idx(cands[i]) < idx(cands[j])
	})
	if len(cands) == 0 {
		return ""
	}
	return cands[0]
}

func (r *run) createReview(ctx context.Context, dev Task, reviewer string) string {
	n := dev.ID
	body := map[string]any{"from": r.cfg.Me, "roomId": r.cfg.ReviewRoom, "planId": r.cfg.ReviewPlan, "title": "Review " + n + " " + clipStr(dev.Title, 60),
		"assignee": reviewer, "capabilities": []string{"review"},
		"objective": fmt.Sprintf("Independent review of %s by %s. Read GET /tasks/%s for the DONE report and branches (web FreeSociety3 and/or C# OpenCiv3). Checklist: MINIMAL DIFF "+
			"(scripts/diffhygiene.py, uilint.sh, uidiff.sh; UIDIFF_APPROVED only for values the task directs), node --check, gofmt, go vet, go test, C# dotnet build+test "+
			"when touched, a behavioural check on your own ports (never 8765/8080/8081/8090-8099), no Civ3 art committed, every value cited to OpenCiv3 source or the original. "+
			"Work in your own worktree, never edit /opt/FreeSociety3. Post 'REVIEW %s: PASS' or 'REVIEW %s: CHANGES-REQUESTED' as the first words of your verdict in review-desk, "+
			"then short numbered findings.", n, dev.Assignee, n, n, n),
		"acceptanceCriteria": "PASS or CHANGES-REQUESTED with evidence.", "exclusions": "Do not edit the branch. Never touch the real game."}
	id, err := r.bus.CreateTask(ctx, body)
	if err != nil {
		r.alert(n, "could not create the review of %s: %v", n, err)
		return ""
	}
	if s, ok := r.cfg.seat(reviewer); !ok || !s.Feed {
		r.transition(ctx, id, "promote", "review for "+reviewer, "")
	}
	return id
}

// MergeTask merges one dev task's branches now (the "merge by hand" button for a task whose
// DONE message the run could not read, or whose merge was blocked): the branches its DONE
// message or review names, or branches given explicitly. dry decides without merging.
func (c *Coordinator) MergeTask(ctx context.Context, id string, branches []string, dry bool) *Report {
	c.mu.Lock()
	defer c.mu.Unlock()
	dry = dry || c.Cfg.DryRun
	rep := &Report{Started: c.now(), Dry: dry}
	bus := c.Bus
	var db *DryBus
	if dry {
		db = &DryBus{Bus: c.Bus}
		bus = db
	}
	r := &run{c: c, cfg: c.Cfg, bus: bus, dry: dry, rep: rep}
	err := func() error {
		st, err := LoadState(c.Cfg.StateFile)
		if err != nil {
			return err
		}
		r.st = st
		t, err := bus.Task(ctx, id)
		if err != nil {
			return err
		}
		r.byID = map[string]Task{t.ID: t}
		if r.reviews, err = bus.Messages(ctx, c.Cfg.ReviewRoom); err != nil {
			return err
		}
		var all []Msg
		for _, room := range c.Cfg.Rooms {
			m, err := bus.Messages(ctx, room)
			if err != nil {
				return err
			}
			all = append(all, m...)
		}
		if len(branches) > 0 {
			text := "DONE " + t.ID + " " + strings.Join(branches, " ")
			all = append(all, Msg{ID: "api", Callsign: t.Assignee, Content: text})
		}
		d := r.st.dev(id)
		if get(d, "pass") == "" {
			set(d, "pass", "by hand")
		}
		r.merge(ctx, t, d, all)
		if dry {
			return nil
		}
		return r.st.Save(c.Cfg.StateFile)
	}()
	if err != nil {
		rep.Error = err.Error()
	}
	if db != nil {
		rep.Writes = db.Writes()
	}
	rep.Finished = c.now()
	return rep
}

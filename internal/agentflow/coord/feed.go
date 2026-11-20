package coord

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var activeStates = []string{"ready", "assigned", "acknowledged", "working", "rework"}

// finishActions are the transitions that take a seat's task from its state to done, so a run
// never sends one the board refuses (AF-13 bug iii: "claim" answered 409 every run because the
// task was already claimed).
func finishActions(state string) []string {
	switch state {
	case "ready", "assigned":
		return []string{"claim", "start", "done"}
	case "acknowledged", "rework":
		return []string{"start", "done"}
	case "working":
		return []string{"done"}
	}
	return nil
}

// feed gives each fed seat exactly one task through its queue file: it records a finished
// task (DONE or a verdict posted since it was fed), picks the next one (pending reviews, then
// seat_order, then rework, then (AF-13 bug ii) the next open task of the seat's own plans), and
// rewrites the queue file.
func (r *run) feed(ctx context.Context) error {
	var msgs []Msg
	for _, room := range r.cfg.Rooms {
		msgs = append(msgs, r.rooms[room]...)
	}
	if _, ok := r.rooms[r.cfg.ReviewRoom]; !ok {
		msgs = append(msgs, r.reviews...)
	}
	if err := os.MkdirAll(r.cfg.QueueDir, 0o755); err != nil && !r.dry {
		return err
	}
	skip := map[string]bool{}
	for _, id := range r.st.FeedSkip {
		skip[id] = true
	}
	for _, seat := range r.cfg.Seats {
		if !seat.Feed {
			continue
		}
		cs := seat.Callsign
		var mine []Task
		for _, t := range r.tasks {
			if t.Assignee == cs {
				mine = append(mine, t)
			}
		}
		var cur *Task
		for i := range mine {
			if in(mine[i].State, activeStates...) && mine[i].ID == r.st.Fed[cs] {
				cur = &mine[i]
			}
		}
		if cur != nil && r.finished(*cur, cs, msgs) {
			for _, act := range finishActions(cur.State) {
				// The seat's own task: act as the seat at once (owner-approved), not first as
				// the coordinator, which the board refuses for a claimed task.
				if err := r.bus.Transition(ctx, cur.ID, cs, act, "Recorded by "+r.cfg.Me+" on behalf of "+cs+" (owner-approved): seat reported completion on BotChat"); err != nil {
					r.c.logLine(fmt.Sprintf("transition %s %s as %s failed: %v", cur.ID, act, cs, err), r.dry)
				}
			}
			r.decide("finished", cur.ID, cs, "the seat reported it on BotChat")
			cur = nil
		}
		held := map[string]Task{}
		for _, t := range mine {
			if in(t.State, "draft", "deferred", "rework") && (cur == nil || t.ID != cur.ID) && !skip[t.ID] {
				held[t.ID] = t
			}
		}
		queue := r.queueOrder(cs, held)
		if cur == nil && len(queue) > 0 {
			nxt := held[queue[0]]
			queue = queue[1:]
			r.decide("fed", nxt.ID, cs, "%s", clipStr(nxt.Title, 90))
			if nxt.State != "rework" {
				act := "reinstate"
				if nxt.State == "draft" {
					act = "promote"
				}
				r.transition(ctx, nxt.ID, act, "spoon-fed to "+cs, "")
			}
			r.st.Fed[cs] = nxt.ID
			cur = &nxt
			var seen []string
			for _, m := range msgs {
				if m.Sender() == cs {
					seen = append(seen, m.ID)
				}
			}
			r.st.FedSeen[cs] = seen
			_ = r.bus.Post(ctx, r.cfg.EngineRoom, r.cfg.Me, cs, fmt.Sprintf("@%s new task in your queue file %s (%s). Read that file and do only that task.", cs, r.queuePath(cs), nxt.ID))
		}
		if cur == nil && len(queue) == 0 && !r.st.FedEmpty[cs] {
			r.alert("", "SPOON: %s has an empty queue and no open task in its plans", cs)
		}
		r.st.FedEmpty[cs] = cur == nil && len(queue) == 0
		text, err := r.queueText(ctx, cs, cur, queue, held)
		if err != nil {
			return err
		}
		if r.dry {
			if r.rep.Queues == nil {
				r.rep.Queues = map[string]string{}
			}
			r.rep.Queues[cs] = text
			continue
		}
		if err := os.WriteFile(r.queuePath(cs), []byte(text), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func (r *run) queuePath(cs string) string { return filepath.Join(r.cfg.QueueDir, cs+".md") }

// finished: the seat posted DONE for its task (or the verdict of a review task) since the task
// was fed; reports that existed then (a reworked task's old DONE) do not count.
func (r *run) finished(cur Task, cs string, msgs []Msg) bool {
	seen := map[string]bool{}
	for _, id := range r.st.FedSeen[cs] {
		seen[id] = true
	}
	var fresh []Msg
	for _, m := range msgs {
		if !seen[m.ID] {
			fresh = append(fresh, m)
		}
	}
	if cur.PlanID != r.cfg.ReviewPlan {
		return doneMessage(cur, fresh) != ""
	}
	f := strings.Fields(cur.Title)
	if len(f) < 2 {
		return false
	}
	for _, m := range fresh {
		if m.Sender() == cs && strings.HasPrefix(expand(m).Content, "REVIEW "+f[1]) {
			return true
		}
	}
	return false
}

// queueOrder is the seat's queue: its pending reviews, then state.json seat_order (reworks
// included), then other reworks, then the rest of the seat's held tasks in the plans it works
// in, by number (bug ii: an empty seat_order no longer leaves the seat idle until a human
// edits state.json).
func (r *run) queueOrder(cs string, held map[string]Task) []string {
	var q []string
	has := map[string]bool{}
	add := func(id string) {
		if !has[id] {
			has[id] = true
			q = append(q, id)
		}
	}
	byNum := func(f func(Task) bool) {
		var ids []string
		for id, t := range held {
			if f(t) && !has[id] {
				ids = append(ids, id)
			}
		}
		sort.Slice(ids, func(i, j int) bool { return held[ids[i]].Num < held[ids[j]].Num })
		for _, id := range ids {
			add(id)
		}
	}
	byNum(func(t Task) bool { return t.PlanID == r.cfg.ReviewPlan })
	for _, id := range r.st.SeatOrder[cs] {
		if _, ok := held[id]; ok {
			add(id)
		}
	}
	byNum(func(t Task) bool { return t.State == "rework" })
	plans := map[string]bool{}
	for _, id := range append(append([]string{}, r.st.SeatOrder[cs]...), r.st.Fed[cs]) {
		if t, ok := r.byID[id]; ok && t.PlanID != "" && t.PlanID != r.cfg.ReviewPlan {
			plans[t.PlanID] = true
		}
	}
	byNum(func(t Task) bool { return plans[t.PlanID] && t.Creator != "af-coord" })
	return q
}

var (
	reDraftTitle = regexp.MustCompile(`\s*-\s*DRAFT\s*$`)
	reDraftExcl  = regexp.MustCompile(`DRAFT: do not start until coord promotes it\.\s*`)
)

// queueText is the queue file, the same text coordinator.py writes.
func (r *run) queueText(ctx context.Context, cs string, cur *Task, queue []string, held map[string]Task) (string, error) {
	start := r.cfg.StartHere
	if start == "" {
		start = filepath.Join(r.cfg.QueueDir, "START-HERE.md")
	}
	lines := []string{"# Queue for " + cs + " (written by " + r.cfg.Me + "; do not edit)", "",
		"New or restarted? First read " + tilde(start) + ": who you are, how this queue, " +
			"BotChat DONE and reviews work, and how to find work you already did before a restart.", ""}
	if cur != nil {
		var report string
		if cur.PlanID == r.cfg.ReviewPlan {
			id := field(cur.Title, 1)
			report = fmt.Sprintf("Post your verdict in BotChat room review-desk (%s) starting with exactly `REVIEW %s: PASS` or `REVIEW %s: CHANGES-REQUESTED`, then numbered findings.", r.cfg.ReviewRoom, id, id)
		} else {
			report = fmt.Sprintf("Push your branch to origin (git.cmdhome.net), then post in BotChat room engine-design (%s): `DONE %s <branch>@<sha>` plus a short report (what changed, gates run, limitations).", r.cfg.EngineRoom, cur.ID)
		}
		title := reDraftTitle.ReplaceAllString(cur.Title, "")
		excl := reDraftExcl.ReplaceAllString(cur.Exclusions, "")
		if cur.State == "rework" {
			rid := "?"
			best := -1
			for _, t := range r.tasks {
				if t.PlanID == r.cfg.ReviewPlan && strings.HasPrefix(t.Title, "Review "+cur.ID+" ") && t.Num > best {
					best, rid = t.Num, t.ID
				}
			}
			title += "  (REWORK)"
			vfile := filepath.Join(r.cfg.QueueDir, "review-"+cur.ID+".md")
			var last string
			for _, m := range r.reviews {
				if strings.HasPrefix(m.Content, "REVIEW "+cur.ID) {
					last = m.Content
				}
			}
			if last != "" && !r.dry {
				body := fmt.Sprintf("# Full review verdict for %s (review %s, copied from review-desk by %s)\n\n%s\n", cur.ID, rid, r.cfg.Me, last)
				if err := os.WriteFile(vfile, []byte(body), 0o644); err != nil {
					return "", err
				}
			}
			lines = append(lines, "## REWORK: a review asked for changes. The full verdict is in `"+vfile+"`. Fix EVERY numbered finding there "+
				"on the same branch, then report DONE again as below.", "")
		}
		lines = append(lines, "## CURRENT TASK: "+cur.ID+"  "+title, "", "### Objective", cur.Objective, "",
			"### Acceptance", cur.AcceptanceCriteria, "", "### Exclusions", excl, "",
			"### When finished", report, "",
			"Rules: work on this task only. Do NOT claim, accept, close or transition any BotChat task ("+r.cfg.Me+" records everything for you). "+
				"Never touch BotChat tasks that agentflow mirrors automatically (creator af-coord, titles like T001/A2-3); "+
				"an AF-n task in THIS file (work ON agentflow itself) is normal work for you. Never touch the live game on port 8765. Never push main. "+
				"This file is rewritten with your next task after you report.", "")
	} else {
		lines = append(lines, "## No task right now. Post `IDLE` in engine-design and wait.", "")
	}
	lines = append(lines, "## Up next (do not start these)")
	for _, id := range queue {
		lines = append(lines, "- "+id+" "+clipStr(held[id].Title, 90))
	}
	return strings.Join(lines, "\n") + "\n", nil
}

func field(s string, i int) string {
	f := strings.Fields(s)
	if i < len(f) {
		return f[i]
	}
	return ""
}

func tilde(p string) string {
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(p, home+"/") {
		return "~" + p[len(home):]
	}
	return p
}

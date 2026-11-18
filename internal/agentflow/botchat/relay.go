package botchat

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"cmd184psu/unified-webapp/internal/agentflow/store"
)

// Coordinator is the callsign agentflow's own code posts under.
const Coordinator = "af-coord"

// Relay posts what happens in agentflow into BotChat: one room per effort, a message for every
// stage that ends (under that stage's own callsign) and for the events that matter (run started,
// queue approved, halted, stalled, accepted). It reads the database, so it adds nothing to the
// engine's own paths and a restart picks up where it stopped.
type Relay struct {
	Store *store.Store
	// Every is the polling interval; zero means 5 seconds.
	Every time.Duration
	// HTTP overrides the client's HTTP client in tests.
	Client func(url, keyFile string) *Client

	mu    sync.Mutex // guards rooms: the dispatcher shares the relay's rooms
	rooms map[string]string
}

const (
	keyStageTS    = "stage_ts"
	keyStageID    = "stage_id"
	keyEventID    = "event_id"
	keyStartTS    = "start_ts"
	keyStartID    = "start_id"
	keyAttTS      = "att_ts"
	keyAttID      = "att_id"
	batch         = 100
	maxText       = 1500
	globalRoom    = "global"
	roomNamePrefx = "agentflow-effort-"
)

// Run polls until ctx ends. A failure is logged and retried on the next tick.
func (r *Relay) Run(ctx context.Context) {
	every := r.Every
	if every <= 0 {
		every = 5 * time.Second
	}
	t := time.NewTicker(every)
	defer t.Stop()
	var lastErr string
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if err := r.Step(ctx); err != nil {
			if msg := err.Error(); msg != lastErr {
				log.Printf("agentflow: botchat relay: %v", err)
				lastErr = msg
			}
		} else {
			lastErr = ""
		}
	}
}

func (r *Relay) client(set store.Settings) *Client {
	if r.Client != nil {
		return r.Client(set.BotchatURL, set.BotchatKeyFile)
	}
	return &Client{BaseURL: set.BotchatURL, KeyFile: set.BotchatKeyFile}
}

// Step posts everything new once. It does nothing when the settings leave the relay off.
func (r *Relay) Step(ctx context.Context) error {
	set, err := r.Store.GetSettings(ctx)
	if err != nil {
		return err
	}
	if set.BotchatURL == "" || set.BotchatKeyFile == "" {
		return nil
	}
	c := r.client(set)
	cur, err := r.cursors(ctx)
	if err != nil {
		return err
	}
	started, err := r.Store.StartedStagesAfter(ctx, cur.sts, cur.sid, batch)
	if err != nil {
		return err
	}
	for _, s := range started {
		if err := r.send(ctx, c, s.Effort, StageCallsign(s), StartText(s)); err != nil {
			return err
		}
		r.note("task board", r.mirrorWorkerStart(ctx, c, s))
		cur.sts, cur.sid = s.Started, s.ID
		if err := r.Store.SetBotchatState(ctx, keyStartTS, store.Itoa64(cur.sts)); err != nil {
			return err
		}
		if err := r.Store.SetBotchatState(ctx, keyStartID, store.Itoa64(cur.sid)); err != nil {
			return err
		}
	}
	stages, err := r.Store.FinishedStagesAfter(ctx, cur.ts, cur.stage, batch)
	if err != nil {
		return err
	}
	for _, s := range stages {
		if err := r.send(ctx, c, s.Effort, StageCallsign(s), StageText(s)); err != nil {
			return err
		}
		cur.ts, cur.stage = s.Finished, s.ID
		if err := r.Store.SetBotchatState(ctx, keyStageTS, store.Itoa64(cur.ts)); err != nil {
			return err
		}
		if err := r.Store.SetBotchatState(ctx, keyStageID, store.Itoa64(cur.stage)); err != nil {
			return err
		}
	}
	attempts, err := r.Store.AttemptsEndedAfter(ctx, cur.ats, cur.aid, batch)
	if err != nil {
		return err
	}
	for _, a := range attempts {
		r.note("task board", r.mirrorAttemptEnd(ctx, c, a))
		cur.ats, cur.aid = a.Finished, a.ID
		if err := r.Store.SetBotchatState(ctx, keyAttTS, store.Itoa64(cur.ats)); err != nil {
			return err
		}
		if err := r.Store.SetBotchatState(ctx, keyAttID, store.Itoa64(cur.aid)); err != nil {
			return err
		}
	}
	events, err := r.Store.EventsAfter(ctx, cur.event, batch)
	if err != nil {
		return err
	}
	for _, e := range events {
		if effort, text, ok := EventText(e); ok {
			switch e.Action {
			case "queue.approve":
				r.note("task board", func() error { _, err := r.mirrorEffort(ctx, c, effort); return err }())
			case "run.complete":
				r.mirrorEffortDone(ctx, c, effort)
			}
			if err := r.send(ctx, c, effort, Coordinator, text); err != nil {
				return err
			}
		}
		cur.event = e.ID
		if err := r.Store.SetBotchatState(ctx, keyEventID, store.Itoa64(cur.event)); err != nil {
			return err
		}
	}
	return nil
}

type cursors struct{ ts, stage, event, sts, sid, ats, aid int64 }

// cursors reads how far the relay has posted; the first time, it starts at the present so
// switching the relay on does not replay history.
func (r *Relay) cursors(ctx context.Context) (cursors, error) {
	var c cursors
	get := func(key string) (int64, bool, error) {
		v, err := r.Store.BotchatState(ctx, key)
		if err != nil || v == "" {
			return 0, false, err
		}
		n, err := strconv.ParseInt(v, 10, 64)
		return n, err == nil, nil
	}
	var okTS, okID, okEv bool
	var err error
	if c.ts, okTS, err = get(keyStageTS); err != nil {
		return c, err
	}
	if c.stage, okID, err = get(keyStageID); err != nil {
		return c, err
	}
	if c.event, okEv, err = get(keyEventID); err != nil {
		return c, err
	}
	var okS1, okS2 bool
	if c.sts, okS1, err = get(keyStartTS); err != nil {
		return c, err
	}
	if c.sid, okS2, err = get(keyStartID); err != nil {
		return c, err
	}
	var okA1, okA2 bool
	if c.ats, okA1, err = get(keyAttTS); err != nil {
		return c, err
	}
	if c.aid, okA2, err = get(keyAttID); err != nil {
		return c, err
	}
	if okTS && okID && okEv && okS1 && okS2 && okA1 && okA2 {
		return c, nil
	}
	ts, stage, event, err := r.Store.RelayHead(ctx)
	if err != nil {
		return c, err
	}
	sts, sid, err := r.Store.StartHead(ctx)
	if err != nil {
		return c, err
	}
	ats, aid, err := r.Store.AttemptHead(ctx)
	if err != nil {
		return c, err
	}
	c = cursors{ts: ts, stage: stage, event: event, sts: sts, sid: sid, ats: ats, aid: aid}
	for k, v := range map[string]int64{keyStageTS: ts, keyStageID: stage, keyEventID: event, keyStartTS: sts, keyStartID: sid, keyAttTS: ats, keyAttID: aid} {
		if err := r.Store.SetBotchatState(ctx, k, store.Itoa64(v)); err != nil {
			return c, err
		}
	}
	return c, nil
}

// note logs a task board failure without stopping the relay: the board is a mirror, so a
// missed move is worth a line in the log, not a stuck run of chat messages.
func (r *Relay) note(what string, err error) {
	if err != nil {
		log.Printf("agentflow: botchat %s: %v", what, err)
	}
}

// roomFor is the id of the effort's room (the shared `global` room when there is no effort),
// made on first use.
func (r *Relay) roomFor(ctx context.Context, c *Client, effort int64) (string, error) {
	name, wave := globalRoom, ""
	if effort > 0 {
		name, wave = roomNamePrefx+strconv.FormatInt(effort, 10), "af-"+strconv.FormatInt(effort, 10)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.rooms == nil {
		r.rooms = map[string]string{}
	}
	if id, ok := r.rooms[name]; ok {
		return id, nil
	}
	id := globalRoom
	if name != globalRoom {
		var err error
		if id, err = c.Room(ctx, name, wave, Coordinator); err != nil {
			return "", err
		}
	}
	r.rooms[name] = id
	return id, nil
}

// send posts to the room of the effort.
func (r *Relay) send(ctx context.Context, c *Client, effort int64, from, text string) error {
	id, err := r.roomFor(ctx, c, effort)
	if err != nil {
		return err
	}
	if r := []rune(text); len(r) > maxText {
		text = string(r[:maxText]) + "…"
	}
	err = c.Post(ctx, id, from, "everyone", text)
	if err == ErrNotFound { // the room was deleted: make it again next time
		r.mu.Lock()
		delete(r.rooms, roomNamePrefx+strconv.FormatInt(effort, 10))
		r.mu.Unlock()
	}
	return err
}

// StageCallsign is the identity a stage speaks under: af-<effort>-<role>[-<task>].
func StageCallsign(s store.FinishedStage) string {
	cs := "af-" + strconv.FormatInt(s.Effort, 10) + "-" + s.Role
	if s.Task != "" {
		cs += "-" + s.Task
	}
	return cs
}

// StageText is the message for a stage that ended.
func StageText(s store.FinishedStage) string {
	var b strings.Builder
	if s.Task != "" {
		fmt.Fprintf(&b, "%s attempt %d · ", s.Task, s.Attempt)
	}
	b.WriteString(s.Role)
	if s.Model != "" {
		fmt.Fprintf(&b, " %s", s.Model)
		if s.Backend != "" {
			fmt.Fprintf(&b, " (%s)", s.Backend)
		}
	}
	out := s.Outcome
	if out == "" {
		out = "done"
	}
	fmt.Fprintf(&b, ": %s", out)
	if s.Finished > s.Started && s.Started > 0 {
		fmt.Fprintf(&b, " · %.1f min", float64(s.Finished-s.Started)/60000)
	}
	if s.Tokens > 0 {
		fmt.Fprintf(&b, " · %s tokens", compact(s.Tokens))
	}
	if d := stageDetail(s); d != "" {
		b.WriteString("\n" + d)
	}
	return b.String()
}

// StartText is the message for a stage that was just handed to its model.
func StartText(s store.FinishedStage) string {
	var b strings.Builder
	if s.Task != "" {
		fmt.Fprintf(&b, "%s attempt %d · ", s.Task, s.Attempt)
	}
	switch {
	case s.Model != "" && s.Backend != "":
		fmt.Fprintf(&b, "assigned to %s %s (%s)", s.Role, s.Model, s.Backend)
	case s.Model != "":
		fmt.Fprintf(&b, "assigned to %s %s", s.Role, s.Model)
	default:
		fmt.Fprintf(&b, "%s started", s.Role)
	}
	return b.String()
}

// stageDetail is what the stage had to say, short enough for a chat: a reviewer's verdict and
// issues, a worker's summary or the reason it is blocked, a verify or merge stage's own words.
func stageDetail(s store.FinishedStage) string {
	var p struct {
		Verdict string `json:"verdict"`
		Summary string `json:"summary"`
		Blocked string `json:"blocked"`
		Issues  []struct {
			Description string `json:"description"`
			Severity    string `json:"severity"`
			File        string `json:"file"`
		} `json:"issues"`
		Files []string `json:"files_changed"`
	}
	if s.Parsed != "" && json.Unmarshal([]byte(s.Parsed), &p) == nil {
		var lines []string
		switch s.Role {
		case "reviewer", "architect", "critic":
			if p.Verdict != "" || p.Summary != "" {
				lines = append(lines, strings.TrimSpace("verdict "+p.Verdict+": "+p.Summary))
			}
			for i, is := range p.Issues {
				if i == 8 {
					lines = append(lines, fmt.Sprintf("… and %d more issues", len(p.Issues)-8))
					break
				}
				where := ""
				if is.File != "" {
					where = " (" + is.File + ")"
				}
				lines = append(lines, fmt.Sprintf("- [%s] %s%s", is.Severity, clip(is.Description, 300), where))
			}
		default:
			switch {
			case p.Blocked != "":
				lines = append(lines, "blocked: "+clip(p.Blocked, 400))
			case p.Summary != "":
				lines = append(lines, clip(p.Summary, 400))
			}
			if len(p.Files) > 0 {
				lines = append(lines, fmt.Sprintf("%d file(s) changed", len(p.Files)))
			}
		}
		return strings.Join(lines, "\n")
	}
	if (s.Role == "verify" || s.Role == "merge") && strings.TrimSpace(s.RawHead) != "" {
		return clip(strings.TrimSpace(s.RawHead), 500)
	}
	return ""
}

func clip(t string, n int) string {
	if r := []rune(strings.TrimSpace(t)); len(r) > n {
		return string(r[:n]) + "…"
	}
	return strings.TrimSpace(t)
}

func compact(n int64) string {
	if n >= 1000 {
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	}
	return strconv.FormatInt(n, 10)
}

// EventText turns an event worth telling into (effort, message); ok is false for the rest.
func EventText(e store.RelayEvent) (effort int64, text string, ok bool) {
	var d map[string]any
	_ = json.Unmarshal([]byte(e.DetailJSON), &d)
	str := func(k string) string { s, _ := d[k].(string); return s }
	num := func(k string) int64 { f, _ := d[k].(float64); return int64(f) }
	switch e.SubjectType {
	case "effort":
		effort, _ = strconv.ParseInt(e.SubjectID, 10, 64)
	default:
		effort = num("effort")
	}
	project := e.SubjectID
	if e.SubjectType != "project" {
		project = str("project")
	}
	switch e.Action {
	case "effort.create":
		return effort, fmt.Sprintf("effort %d created on project %s", effort, project), true
	case "run.start":
		return effort, "run started on " + project, true
	case "run.pause", "run.paused":
		return effort, "run paused on " + project, true
	case "run.stop":
		return effort, "run stopped on " + project, true
	case "run.complete":
		return effort, "run complete on " + project, true
	case "queue.approve":
		return effort, "queue approved", true
	case "queue.edit_task":
		return effort, "task " + str("task") + " edited", true
	case "compile.halted":
		return effort, "compile halted: " + str("reason"), true
	case "plan.halted":
		return effort, "planning halted: " + str("reason"), true
	case "stall.detected":
		return effort, fmt.Sprintf("stalled: nothing in flight for %d min", num("minutes")), true
	case "acceptance.passed":
		return effort, "acceptance review passed: " + str("summary"), true
	case "effort.abandon":
		return effort, "effort abandoned", true
	}
	return 0, "", false
}

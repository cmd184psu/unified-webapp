// Copyright (c) 2026 cmd184psu
// SPDX-License-Identifier: MIT

package coord

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"cmd184psu/unified-webapp/internal/agentflow/store"
)

// FeedSeat is one seat native feeding may fill.
type FeedSeat struct {
	Callsign  string
	Kind      SeatKind
	QueueFile string
}

// SeatFeeder renders queue files from the seat-task backlog and, when asked,
// feeds empty seats. Both are no-ops while seat_feed_native is false, so
// coord/feed.go keeps writing the queue files.
type SeatFeeder struct {
	Store    *store.Store
	QueueDir string
	Seats    []FeedSeat
	// Post sends the board pointer. Nil discards it.
	Post func(ctx context.Context, callsign, message string) error
	Now  func() int64
}

// SeatFeedNative reports whether native rendering and feeding are on.
// A missing store or a missing key is off.
func SeatFeedNative(ctx context.Context, st *store.Store) (bool, error) {
	if st == nil {
		return false, nil
	}
	v, err := st.Setting(ctx, "seat_feed_native")
	if err != nil {
		return false, err
	}
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "on", "yes":
		return true, nil
	default:
		return false, nil
	}
}

func (f *SeatFeeder) seat(callsign string) FeedSeat {
	for _, s := range f.Seats {
		if s.Callsign == callsign {
			return s
		}
	}
	return FeedSeat{Callsign: callsign}
}

func (f *SeatFeeder) queuePath(callsign string) string {
	s := f.seat(callsign)
	if s.QueueFile != "" {
		return s.QueueFile
	}
	return filepath.Join(f.QueueDir, callsign+".md")
}

func headlessKind(k SeatKind) bool {
	switch k {
	case KindKilo, KindOpencode, SeatKind("headless"):
		return true
	default:
		return false
	}
}

// researchTask is work that only a headless seat may take: a RE- title, or an
// explicit "research" kind.
func researchTask(t store.SeatTask) bool {
	title := strings.TrimSpace(t.Title)
	if strings.HasPrefix(strings.ToUpper(title), "RE-") {
		return true
	}
	for _, k := range t.EligibleKinds {
		if strings.EqualFold(k, "research") {
			return true
		}
	}
	return false
}

func taskEligible(t store.SeatTask, seat FeedSeat) bool {
	if researchTask(t) && !headlessKind(seat.Kind) {
		return false
	}
	for _, cs := range t.EligibleSeats {
		if cs == seat.Callsign {
			return true
		}
	}
	if seat.Kind == "" {
		return false
	}
	for _, k := range t.EligibleKinds {
		if strings.EqualFold(k, string(seat.Kind)) {
			return true
		}
		if headlessKind(seat.Kind) && strings.EqualFold(k, "headless") {
			return true
		}
	}
	return false
}

func taskRef(t store.SeatTask) string {
	return fmt.Sprintf("ST-%d", t.ID)
}

// renderQueueText is the queue file body. An empty current task says
// NO CURRENT TASK. Up next lists other ready tasks and does not start them.
func renderQueueText(callsign string, cur *store.SeatTask, up []store.SeatTask) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Queue for %s\n\n", callsign)
	if cur == nil {
		b.WriteString("## NO CURRENT TASK\n")
		b.WriteString("## No task right now.\n")
	} else {
		fmt.Fprintf(&b, "## CURRENT TASK: %s  %s\n\n", taskRef(*cur), cur.Title)
		fmt.Fprintf(&b, "### Objective\n%s\n\n", cur.Objective)
		fmt.Fprintf(&b, "### Acceptance\n%s\n\n", cur.Acceptance)
		fmt.Fprintf(&b, "### Exclusions\n%s\n\n", cur.Exclusions)
	}
	b.WriteString("## Up next (do not start these)\n")
	for _, t := range up {
		fmt.Fprintf(&b, "- %s %s\n", taskRef(t), t.Title)
	}
	return b.String()
}

// RenderSeatQueue writes <queue>/<callsign>.md from the backlog and returns
// that same text. While seat_feed_native is false it writes nothing and
// returns an empty string.
func (f *SeatFeeder) RenderSeatQueue(ctx context.Context, callsign string) (string, error) {
	on, err := SeatFeedNative(ctx, f.Store)
	if err != nil || !on {
		return "", err
	}
	if f.Store == nil {
		return "", nil
	}
	tasks, err := f.Store.ListSeatTasks(ctx, "", "")
	if err != nil {
		return "", err
	}
	seat := f.seat(callsign)
	var cur *store.SeatTask
	var up []store.SeatTask
	for i := range tasks {
		t := tasks[i]
		if t.State == "fed" && t.FedTo == callsign {
			if cur == nil || t.FedAt >= cur.FedAt {
				cp := t
				cur = &cp
			}
			continue
		}
		if t.State == "ready" && taskEligible(t, seat) {
			up = append(up, t)
		}
	}
	text := renderQueueText(callsign, cur, up)
	path := f.queuePath(callsign)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		return "", err
	}
	return text, nil
}

func (f *SeatFeeder) now() int64 {
	if f.Now != nil {
		return f.Now()
	}
	return time.Now().Unix()
}

func seatHasWork(tasks []store.SeatTask, seat FeedSeat, path string) bool {
	for _, t := range tasks {
		if t.State == "fed" && t.FedTo == seat.Callsign {
			return true
		}
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return strings.Contains(string(b), "## CURRENT TASK:")
}

func pickReady(tasks []store.SeatTask, seat FeedSeat) *store.SeatTask {
	var best *store.SeatTask
	for i := range tasks {
		t := tasks[i]
		if t.State != "ready" || !taskEligible(t, seat) {
			continue
		}
		if best == nil || t.Priority > best.Priority || (t.Priority == best.Priority && t.ID < best.ID) {
			cp := t
			best = &cp
		}
	}
	return best
}

// FeedSeats gives each empty-queue seat its highest-priority ready task.
// Research tasks go only to headless seats. A seat that already has a fed
// task, or a queue file that already has a CURRENT TASK, is left alone.
// While seat_feed_native is false this is a no-op.
func (f *SeatFeeder) FeedSeats(ctx context.Context) error {
	on, err := SeatFeedNative(ctx, f.Store)
	if err != nil || !on || f.Store == nil {
		return err
	}
	tasks, err := f.Store.ListSeatTasks(ctx, "", "")
	if err != nil {
		return err
	}
	seats := append([]FeedSeat(nil), f.Seats...)
	sort.Slice(seats, func(i, j int) bool { return seats[i].Callsign < seats[j].Callsign })
	for _, seat := range seats {
		if seat.Callsign == "" || seatHasWork(tasks, seat, f.queuePath(seat.Callsign)) {
			continue
		}
		next := pickReady(tasks, seat)
		if next == nil {
			continue
		}
		fed, err := f.Store.MarkFed(ctx, next.ID, seat.Callsign, f.now())
		if err != nil {
			return err
		}
		for i := range tasks {
			if tasks[i].ID == fed.ID {
				tasks[i] = fed
			}
		}
		if _, err := f.RenderSeatQueue(ctx, seat.Callsign); err != nil {
			return err
		}
		if f.Post != nil {
			msg := fmt.Sprintf("@%s new task in your queue file %s (%s). Read that file and do only that task.", seat.Callsign, f.queuePath(seat.Callsign), taskRef(fed))
			if err := f.Post(ctx, seat.Callsign, msg); err != nil {
				return err
			}
		}
	}
	return nil
}

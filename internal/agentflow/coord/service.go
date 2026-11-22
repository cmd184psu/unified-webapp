package coord

import (
	"context"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Service is the module as agentflow runs it: the coordinator pass on Every, the seat watch on
// WatchEvery, and the calls the API makes.
type Service struct {
	Cfg   *Config
	Coord *Coordinator
	Watch *Watcher
	// Dir is agentflow's data directory, where seats.json and seat-nudges.jsonl live.
	Dir string

	// watchLast is the newest seat-watch decisions.
	watchLast []Decision
}

// Start runs the loops until ctx ends, when Config.Enabled. The first pass waits one interval,
// so a restart never runs a pass twice in a row with the Python coordinator.
func (s *Service) Start(ctx context.Context) {
	if s == nil || !s.Cfg.Enabled {
		return
	}
	go loop(ctx, s.Cfg.Every.or(15*time.Minute), func() { s.Coord.Run(ctx, false) })
	go loop(ctx, s.Cfg.WatchEvery.or(time.Minute), func() { s.watchLast = s.Watch.Pass(ctx, false) })
}

func loop(ctx context.Context, every time.Duration, f func()) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			func() {
				defer func() {
					if r := recover(); r != nil {
						log.Printf("coord: pass panicked: %v", r)
					}
				}()
				f()
			}()
		}
	}
}

// ExternalSeat is one hand-run seat for GET /api/seats/external: its live screen state, the
// registry entry the owner edits (kind, where, nudge policy), and the paste card the Seats
// page shows.
type ExternalSeat struct {
	SeatState
	Kind    SeatKind    `json:"kind"`
	Where   string      `json:"where"`
	Feed    bool        `json:"feed"`
	Nudge   bool        `json:"nudge"`
	Policy  NudgePolicy `json:"policy"`
	Current string      `json:"current,omitempty"` // the task fed to it (state.json fed)
	Queue   string      `json:"queue_file"`
	// Suggested is the exact message to paste, by the stopgap's rules (Suggestion).
	Suggested Suggestion `json:"suggested"`
	// NudgeLog is the seat's newest nudges, oldest first.
	NudgeLog []NudgeEntry `json:"nudge_log,omitempty"`
}

// Seats lists the hand-run seats from the owner's registry.
func (s *Service) Seats() ([]ExternalSeat, error) {
	reg, err := s.Registry()
	if err != nil {
		return nil, err
	}
	st, err := LoadState(s.Cfg.StateFile)
	if err != nil {
		return nil, err
	}
	var out []ExternalSeat
	watched := map[string]SeatState{}
	for _, w := range s.Watch.Seats() {
		watched[w.Callsign] = w
	}
	for _, c := range reg.Seats {
		ws := watched[c.Callsign]
		if ws.Callsign == "" {
			ws = SeatState{Callsign: c.Callsign, Session: c.Where}
		}
		qp := s.seatQueuePath(c)
		text, rerr := os.ReadFile(qp)
		e := ExternalSeat{
			SeatState: ws, Kind: c.Kind, Where: c.Where, Feed: true,
			Nudge: c.Policy.Enabled && !c.Policy.Paused, Policy: c.Policy,
			Current: st.Fed[c.Callsign], Queue: qp,
			Suggested: c.Suggest(string(text), rerr == nil, MessageOptions{QueueFile: qp, StartHere: s.Cfg.StartHere}),
		}
		if cfgSeat, ok := s.Cfg.seat(c.Callsign); ok {
			e.Feed = cfgSeat.Feed
		}
		e.NudgeLog = s.nudgeLog(c.Callsign, 5)
		out = append(out, e)
	}
	return out, nil
}

// queuePath keeps AF-13's path for a callsign.
func (s *Service) queuePath(cs string) string {
	return strings.TrimRight(s.Cfg.QueueDir, "/") + "/" + cs + ".md"
}

// seatQueuePath is the registered seat's queue file.
func (s *Service) seatQueuePath(c RegistrySeat) string {
	if c.QueueFile != "" {
		return c.QueueFile
	}
	return s.queuePath(c.Callsign)
}

// QueueFile is the seat's queue file.
func (s *Service) QueueFile(callsign string) (string, error) {
	reg, err := s.Registry()
	if err != nil {
		return "", err
	}
	c, ok := reg.seat(callsign)
	if !ok {
		return "", ErrUnknownSeat
	}
	b, err := os.ReadFile(s.seatQueuePath(c))
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	return string(b), err
}

// registryPath is agentflow's seats.json; empty Dir keeps the registry in memory (tests).
func (s *Service) registryPath() string {
	if s == nil || s.Dir == "" {
		return ""
	}
	return filepath.Join(s.Dir, RegistryFile)
}

// Registry reads the owner's registry; a missing file gives the import defaults.
func (s *Service) Registry() (*Registry, error) {
	p := s.registryPath()
	if p == "" {
		return DefaultRegistry(), nil
	}
	return LoadRegistry(p)
}

// Seat returns one registered seat.
func (s *Service) Seat(callsign string) (RegistrySeat, bool, error) {
	reg, err := s.Registry()
	if err != nil {
		return RegistrySeat{}, false, err
	}
	c, ok := reg.seat(callsign)
	return c, ok, nil
}

// SaveSeat adds or replaces a seat and persists the registry.
func (s *Service) SaveSeat(seat RegistrySeat) (RegistrySeat, error) {
	reg, err := s.Registry()
	if err != nil {
		return RegistrySeat{}, err
	}
	if err := reg.Upsert(seat); err != nil {
		return RegistrySeat{}, err
	}
	if p := s.registryPath(); p != "" {
		if err := reg.Save(p); err != nil {
			return RegistrySeat{}, err
		}
	}
	return seat, nil
}

// DeleteSeat removes a seat from the registry.
func (s *Service) DeleteSeat(callsign string) error {
	reg, err := s.Registry()
	if err != nil {
		return err
	}
	if err := reg.Delete(callsign); err != nil {
		return err
	}
	if p := s.registryPath(); p != "" {
		return reg.Save(p)
	}
	return nil
}

// PauseSeat pauses or resumes a seat's nudges without touching its Enabled choice.
func (s *Service) PauseSeat(callsign string, paused bool) (RegistrySeat, error) {
	reg, err := s.Registry()
	if err != nil {
		return RegistrySeat{}, err
	}
	c, ok := reg.seat(callsign)
	if !ok {
		return RegistrySeat{}, ErrUnknownSeat
	}
	c.Policy.Paused = paused
	if _, err := s.SaveSeat(c); err != nil {
		return RegistrySeat{}, err
	}
	return c, nil
}

// LogNudge records a nudge on the card and in seat-nudges.jsonl.
func (s *Service) LogNudge(e NudgeEntry) error {
	p := s.registryPath()
	if p == "" {
		return nil
	}
	return AppendNudgeLog(filepath.Join(filepath.Dir(p), NudgeLogFile), e)
}

func (s *Service) nudgeLog(callsign string, limit int) []NudgeEntry {
	p := s.registryPath()
	if p == "" {
		return nil
	}
	var out []NudgeEntry
	for _, e := range ReadNudgeLog(filepath.Join(filepath.Dir(p), NudgeLogFile), 0) {
		if e.Seat == callsign {
			out = append(out, e)
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out
}

// WatchLast is the newest seat-watch decisions.
func (s *Service) WatchLast() []Decision { return s.watchLast }

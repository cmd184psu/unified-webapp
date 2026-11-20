package coord

import (
	"context"
	"errors"
	"log"
	"os"
	"strings"
	"time"
)

// Service is the module as agentflow runs it: the coordinator pass on Every, the seat watch on
// WatchEvery, and the calls the API makes.
type Service struct {
	Cfg   *Config
	Coord *Coordinator
	Watch *Watcher

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

// ExternalSeat is one hand-run seat for GET /api/seats/external.
type ExternalSeat struct {
	SeatState
	Feed    bool   `json:"feed"`
	Nudge   bool   `json:"nudge"`
	Current string `json:"current,omitempty"` // the task fed to it (state.json fed)
	Queue   string `json:"queue_file"`
}

// Seats lists the hand-run seats.
func (s *Service) Seats() ([]ExternalSeat, error) {
	st, err := LoadState(s.Cfg.StateFile)
	if err != nil {
		return nil, err
	}
	var out []ExternalSeat
	watched := map[string]SeatState{}
	for _, w := range s.Watch.Seats() {
		watched[w.Callsign] = w
	}
	for _, c := range s.Cfg.Seats {
		ws := watched[c.Callsign]
		if ws.Callsign == "" {
			ws = SeatState{Callsign: c.Callsign, Session: c.Session}
		}
		out = append(out, ExternalSeat{SeatState: ws, Feed: c.Feed, Nudge: c.Nudge, Current: st.Fed[c.Callsign], Queue: s.queuePath(c.Callsign)})
	}
	return out, nil
}

func (s *Service) queuePath(cs string) string {
	return strings.TrimRight(s.Cfg.QueueDir, "/") + "/" + cs + ".md"
}

// QueueFile is the seat's queue file.
func (s *Service) QueueFile(callsign string) (string, error) {
	if _, ok := s.Cfg.seat(callsign); !ok {
		return "", ErrUnknownSeat
	}
	b, err := os.ReadFile(s.queuePath(callsign))
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	return string(b), err
}

// WatchLast is the newest seat-watch decisions.
func (s *Service) WatchLast() []Decision { return s.watchLast }

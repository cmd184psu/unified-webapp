package coord

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// State is coordinator.py's state.json. Keys this port does not use are kept as they were, so
// the Python coordinator can run again from a state this one wrote.
type State struct {
	// Dev is each dev task's delivery: status (done, rework, blocked, merged, merged?) and the
	// review ids (pass, cr, review). A value may be null.
	Dev       map[string]map[string]*string `json:"dev"`
	SeatOrder map[string][]string           `json:"seat_order"`
	Fed       map[string]string             `json:"fed"`
	FedSeen   map[string][]string           `json:"fed_seen"`
	FedEmpty  map[string]bool               `json:"fed_empty"`
	FeedSkip  []string                      `json:"feed_skip"`
	// RoomSeqs is the newest message seq read per room, so a pass asks only for
	// what is new (AF-15).
	RoomSeqs map[string]int64 `json:"room_seqs"`
	// AFDelivered maps a delivered agentflow effort id to merged, blocked or "merge-task TASK-n".
	AFDelivered map[string]string `json:"af_delivered"`

	extra map[string]json.RawMessage
}

var stateKeys = []string{"dev", "seat_order", "fed", "fed_seen", "fed_empty", "feed_skip", "af_delivered", "room_seqs"}

func (s *State) UnmarshalJSON(b []byte) error {
	type plain State
	var p plain
	if err := json.Unmarshal(b, &p); err != nil {
		return err
	}
	if err := json.Unmarshal(b, &p.extra); err != nil {
		return err
	}
	for _, k := range stateKeys {
		delete(p.extra, k)
	}
	*s = State(p)
	s.init()
	return nil
}

func (s State) MarshalJSON() ([]byte, error) {
	type plain State
	b, err := json.Marshal(plain(s))
	if err != nil || len(s.extra) == 0 {
		return b, err
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	for k, v := range s.extra {
		m[k] = v
	}
	return json.Marshal(m)
}

func (s *State) init() {
	if s.Dev == nil {
		s.Dev = map[string]map[string]*string{}
	}
	if s.SeatOrder == nil {
		s.SeatOrder = map[string][]string{}
	}
	if s.Fed == nil {
		s.Fed = map[string]string{}
	}
	if s.FedSeen == nil {
		s.FedSeen = map[string][]string{}
	}
	if s.FedEmpty == nil {
		s.FedEmpty = map[string]bool{}
	}
	if s.AFDelivered == nil {
		s.AFDelivered = map[string]string{}
	}
	if s.RoomSeqs == nil {
		s.RoomSeqs = map[string]int64{}
	}
}

// dev returns the task's delivery record, creating it.
func (s *State) dev(id string) map[string]*string {
	d, ok := s.Dev[id]
	if !ok || d == nil {
		d = map[string]*string{}
		s.Dev[id] = d
	}
	return d
}

func get(d map[string]*string, k string) string {
	if v := d[k]; v != nil {
		return *v
	}
	return ""
}

func set(d map[string]*string, k, v string) {
	if v == "" {
		d[k] = nil
		return
	}
	d[k] = &v
}

// LoadState reads state.json; a missing file is an empty state.
func LoadState(path string) (*State, error) {
	s := &State{}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		s.init()
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	return s, json.Unmarshal(b, s)
}

// Save writes state.json atomically.
func (s *State) Save(path string) error {
	b, err := json.MarshalIndent(s, "", " ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".state-*.json")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(append(b, '\n')); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), path)
}

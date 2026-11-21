// Package coord is the coordinator for the seats the owner runs by hand (AF-13), ported from
// ~/work/coordinator/coordinator.py and seatwatch.sh into agentflow.
//
// The seats (deepseek4, fb-beam) work from a queue file each and report on BotChat: a
// "DONE TASK-n branch@sha" message for work, a "REVIEW TASK-n: PASS|CHANGES-REQUESTED" verdict
// for a review. Each run the coordinator reads the BotChat rooms and the task board and:
//
//  1. records finished reviews: PASS marks the dev task for merging, CHANGES-REQUESTED sends it
//     back to its author (rework) and copies the verdict next to the queue;
//  2. creates one review for each newly finished dev task, from a seat of another model family;
//  3. merges a passed dev task's branches into the base branch of each repository that has them,
//     in a scratch worktree, after the repository's gates, at most MaxMerges per run;
//  4. feeds each seat exactly one task through its queue file;
//
// and, every minute, watches the seats' terminals: a seat whose screen has stood still is
// nudged with the owner-approved text (never while a halt text shows) or the owner is told.
//
// Every BotChat write and every merge goes through one place, so a dry run (Config.DryRun, or
// a run asked for with dry=true) decides everything and does nothing: its decisions are the
// report. It reads and writes coordinator.py's state.json, so either can run from the other's
// state during the switch-over.
package coord

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ConfigFile is the module's configuration in agentflow's data directory. Without it the
// module is off.
const ConfigFile = "coord.json"

// Repo is a repository a passed task's branch is merged into.
type Repo struct {
	Name string `json:"name"` // web, cs
	Path string `json:"path"`
	// Base is the branch merges go into (default Config.Base).
	Base string `json:"base,omitempty"`
	// Gates are shell commands run in the merge worktree after the merge; any failure aborts
	// the merge. They run with Env added to the environment.
	Gates []string          `json:"gates"`
	Env   map[string]string `json:"env,omitempty"`
	// UnionFiles are files whose conflicts are resolved by keeping both sides' lines (MANIFEST.md).
	UnionFiles []string `json:"union_files,omitempty"`
	// ApprovedEnv is set to the task id for a merge whose review approved a visual change
	// (UIDIFF_APPROVED).
	ApprovedEnv string `json:"approved_env,omitempty"`
	// Push lets the coordinator push a gated merge to origin's base branch, as
	// coordinator.py does. It is the one place agentflow pushes, so it is off unless the
	// owner sets it; without it a passed merge stops with an alert after its gates.
	Push bool `json:"push,omitempty"`
}

// Seat is a hand-run seat: a BotChat callsign working in the owner's tmux session.
type Seat struct {
	Callsign string `json:"callsign"`
	Session  string `json:"session"`
	// Feed: the seat gets exactly one task at a time through its queue file.
	Feed bool `json:"feed"`
	// Nudge: when its screen stands still the watcher types the profile's nudge text.
	Nudge bool `json:"nudge"`
	// Profile is the seat profile (AF-11 seat_profiles) whose quiet_lines, halt_texts and
	// nudge_text the watcher uses; empty uses the built-in freebuff profile.
	Profile string `json:"profile,omitempty"`
	// Ready is a text the screen shows when the seat waits at its input box; the watcher
	// types only then.
	Ready []string `json:"ready,omitempty"`
}

// Config is coord.json.
type Config struct {
	// Enabled starts the run loop; the API works either way.
	Enabled bool `json:"enabled"`
	// DryRun decides and reports but writes nothing to BotChat, merges nothing, types nothing
	// and keeps state in memory.
	DryRun bool `json:"dry_run"`
	// Every is the coordinator run interval (default 15m); WatchEvery the seat watch (1m).
	Every      Duration `json:"every,omitempty"`
	WatchEvery Duration `json:"watch_every,omitempty"`

	BotChatURL string `json:"botchat_url"`
	KeyFile    string `json:"key_file"`
	Me         string `json:"me"` // coord-fs3

	ReviewPlan string   `json:"review_plan"`
	ReviewRoom string   `json:"review_room"`
	EngineRoom string   `json:"engine_room"`
	EnginePlan string   `json:"engine_plan"`
	Rooms      []string `json:"rooms"`
	// OurPlans are the plans whose finished tasks this coordinator reviews and merges.
	OurPlans []string `json:"our_plans"`
	// Reviewers in preference order; Family maps a callsign to its model family (a reviewer
	// never shares the author's).
	Reviewers []string          `json:"reviewers"`
	Family    map[string]string `json:"family"`
	// ReviewMode chooses how seat work is reviewed: "seat" (the default) creates a BotChat
	// review task, "agentflow" asks the review API at AgentflowURL instead.
	ReviewMode   string `json:"review_mode,omitempty"`
	AgentflowURL string `json:"agentflow_url,omitempty"`
	// StrongReviewers may review engine work (EnginePlan, or titles matching EngineTitles).
	StrongReviewers []string `json:"strong_reviewers,omitempty"`
	EngineTitles    string   `json:"engine_titles,omitempty"`
	// Authors are the callsigns whose finished tasks are taken (others are ignored).
	Authors []string `json:"authors"`

	Base      string `json:"base"`
	Repos     []Repo `json:"repos"`
	MaxMerges int    `json:"max_merges,omitempty"`
	// MergeDir holds the scratch worktrees merges run in.
	MergeDir string `json:"merge_dir,omitempty"`

	Seats []Seat `json:"seats"`
	// QueueDir holds <callsign>.md and review-TASK-n.md; StateFile is state.json; AlertFile and
	// LogFile are appended to.
	QueueDir  string `json:"queue_dir"`
	StateFile string `json:"state_file"`
	AlertFile string `json:"alert_file,omitempty"`
	LogFile   string `json:"log_file,omitempty"`
	// Start is the line the queue file points a restarted seat at.
	StartHere string `json:"start_here,omitempty"`
	// IdleAfter is how long a seat's screen may stand still before it is nudged (10m);
	// RenotifyAfter is the gap between nudges or notices while it stays still (30m).
	IdleAfter     Duration `json:"idle_after,omitempty"`
	RenotifyAfter Duration `json:"renotify_after,omitempty"`
	// DeadText is a regexp for a seat whose session ended (notified, never nudged).
	DeadText string `json:"dead_text,omitempty"`
}

// Duration is a time.Duration written as "15m" in JSON.
type Duration time.Duration

func (d *Duration) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	v, err := time.ParseDuration(s)
	*d = Duration(v)
	return err
}

func (d Duration) MarshalJSON() ([]byte, error) { return json.Marshal(time.Duration(d).String()) }

func (d Duration) or(def time.Duration) time.Duration {
	if d > 0 {
		return time.Duration(d)
	}
	return def
}

// Load reads dir/coord.json. A missing file is (nil, nil): the module is off.
func Load(dir string) (*Config, error) {
	b, err := os.ReadFile(filepath.Join(dir, ConfigFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var c Config
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("coord: %s: %w", ConfigFile, err)
	}
	return &c, c.validate()
}

func (c *Config) validate() error {
	var miss []string
	for name, v := range map[string]string{"botchat_url": c.BotChatURL, "key_file": c.KeyFile, "me": c.Me, "review_plan": c.ReviewPlan,
		"review_room": c.ReviewRoom, "engine_room": c.EngineRoom, "base": c.Base, "queue_dir": c.QueueDir, "state_file": c.StateFile} {
		if strings.TrimSpace(v) == "" {
			miss = append(miss, name)
		}
	}
	if len(miss) > 0 {
		return fmt.Errorf("coord: %s needs %s", ConfigFile, strings.Join(miss, ", "))
	}
	for _, s := range c.Seats {
		if s.Callsign == "" || (s.Nudge && s.Session == "") {
			return fmt.Errorf("coord: a seat needs a callsign, and a session to be nudged: %+v", s)
		}
	}
	return nil
}

func (c *Config) seat(callsign string) (Seat, bool) {
	for _, s := range c.Seats {
		if s.Callsign == callsign {
			return s, true
		}
	}
	return Seat{}, false
}

func (c *Config) family(cs string) string {
	if f, ok := c.Family[cs]; ok {
		return f
	}
	return cs
}

func (c *Config) maxMerges() int {
	if c.MaxMerges > 0 {
		return c.MaxMerges
	}
	return 2
}

func (r Repo) base(c *Config) string {
	if r.Base != "" {
		return r.Base
	}
	return c.Base
}

package agentflow

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"

	"cmd184psu/unified-webapp/internal/agentflow/engine"
	"cmd184psu/unified-webapp/internal/agentflow/gitsvc"
	"cmd184psu/unified-webapp/internal/agentflow/notify"
	"cmd184psu/unified-webapp/internal/agentflow/runner"
	"cmd184psu/unified-webapp/internal/agentflow/runner/botchatseat"
	"cmd184psu/unified-webapp/internal/agentflow/runner/mux"
	"cmd184psu/unified-webapp/internal/agentflow/runner/seat"
	"cmd184psu/unified-webapp/internal/agentflow/sandbox"
	"cmd184psu/unified-webapp/internal/agentflow/store"
)

// seats are the two seat runners and the board they report to. One board serves both, so the
// engine lists one set of seat notices.
type seats struct {
	board *runner.SeatBoard
	term  *seat.Adapter
	chat  *botchatseat.Adapter
}

var _ engine.SeatControl = (*seats)(nil)

func newSeats(dataDir string, git *gitsvc.Service, st *store.Store) *seats {
	board := &runner.SeatBoard{}
	return &seats{
		board: board,
		term: &seat.Adapter{
			Mux: func(ctx context.Context) (mux.Multiplexer, error) {
				set, err := st.GetSettings(ctx)
				if err != nil {
					return mux.New("")
				}
				return mux.New(set.SeatMux)
			},
			Profiles: func(ctx context.Context) map[string]seat.Profile {
				set, err := st.GetSettings(ctx)
				if err != nil {
					return nil
				}
				return seatProfiles(set.SeatProfiles)
			},
			Sandbox:  seatSandbox(gitDirs(git, st)),
			Instance: seat.InstanceID(dataDir),
			Board:    board,
		},
		chat: &botchatseat.Adapter{
			Settings: func(ctx context.Context) (string, string) {
				set, err := st.GetSettings(ctx)
				if err != nil {
					return "", ""
				}
				return set.BotchatURL, set.BotchatKeyFile
			},
			Board: board,
		},
	}
}

// seatProfiles reads the seat_profiles setting. A profile named like a built-in one is laid
// over it, so {"freebuff": {"config_dir": "..."}} changes only that field.
func seatProfiles(text string) map[string]seat.Profile {
	out := map[string]seat.Profile{"freebuff": seat.Freebuff()}
	if text == "" {
		return out
	}
	var raw map[string]json.RawMessage
	if json.Unmarshal([]byte(text), &raw) != nil {
		return out
	}
	for name, r := range raw {
		p := seat.Profile{}
		if base, ok := out[name]; ok {
			p = base
		}
		if json.Unmarshal(r, &p) == nil {
			out[name] = p
		}
	}
	return out
}

// seatBinDst is where a seat program's binary is bound read-only inside a bwrap sandbox.
const seatBinDst = "/opt/agentflow-seat"

// seatSandbox wraps a seat program in the stage sandbox, like every other agent: the stage
// home as HOME, credentials hidden, the worktree writable only for a writing role, git admin
// dirs read-only. The program then runs in a terminal session, not as agentflow's child.
func seatSandbox(dirs func(runner.Request) (string, string, error)) seat.Sandboxer {
	return func(req runner.Request, o seat.SandboxOpts) (seat.Launch, error) {
		argv, env, net := o.Argv, o.Env, o.Net
		repo, wt, err := dirs(req)
		if err != nil {
			return seat.Launch{}, err
		}
		home := seat.HomeDir(req.StageDir)
		if err := os.MkdirAll(home, 0o700); err != nil {
			return seat.Launch{}, err
		}
		spec := sandbox.Spec{
			Worktree:         req.Workdir,
			ReadOnlyWorktree: req.Role != "worker" && req.Role != "cleanup",
			RepoGitDir:       repo,
			WorktreeGitDir:   wt,
			StageHome:        home,
			Net:              net,
			Env:              env,
			Argv:             argv,
			HideDirs:         o.HideDirs,
			ReadFiles:        o.ReadFiles,
		}
		if runtime.GOOS == "linux" && len(argv) > 0 && filepath.IsAbs(argv[0]) {
			// bwrap shows only what is bound: the program's binary goes in read-only.
			dst := filepath.Join(seatBinDst, filepath.Base(argv[0]))
			spec.ExtraROBinds = append(spec.ExtraROBinds, sandbox.Bind{Src: argv[0], Dst: dst})
			spec.Argv = append([]string{dst}, argv[1:]...)
		}
		cmd, err := sandbox.Command(context.Background(), spec) // never started: only its argv and env are used
		if err != nil {
			return seat.Launch{}, err
		}
		seen := sandbox.SandboxHome
		if runtime.GOOS != "linux" {
			if seen, err = filepath.EvalSymlinks(home); err != nil {
				return seat.Launch{}, err
			}
		}
		argvOut := append([]string{cmd.Path}, cmd.Args[1:]...)
		return seat.Launch{Argv: argvOut, Env: cmd.Env, Dir: cmd.Dir, Home: seen}, nil
	}
}

func (s *seats) IsSeat(apiType string) bool {
	return apiType == seat.APIType || apiType == botchatseat.APIType
}

func (s *seats) Alive(ctx context.Context, apiType string, stage int64, stageDir string) bool {
	switch apiType {
	case seat.APIType:
		return s.term.Alive(ctx, stage, stageDir)
	case botchatseat.APIType:
		return s.chat.Alive(ctx, stageDir)
	}
	return false
}

func (s *seats) Cancel(ctx context.Context, apiType string, stage int64, stageDir string) error {
	switch apiType {
	case seat.APIType:
		return s.term.Kill(ctx, stage, stageDir)
	case botchatseat.APIType:
		return s.chat.Cancel(ctx, stage, stageDir)
	}
	return nil
}

func (s *seats) Answer(ctx context.Context, apiType string, stage int64, stageDir, text string) error {
	switch apiType {
	case seat.APIType:
		return s.term.Answer(ctx, stage, stageDir, text)
	case botchatseat.APIType:
		return s.chat.Answer(ctx, stage, stageDir, text)
	}
	return fmt.Errorf("api type %s is not a seat", apiType)
}

func (s *seats) Attach(ctx context.Context, apiType string, stage int64, stageDir string) string {
	if apiType == seat.APIType {
		return s.term.Attach(ctx, stage, stageDir)
	}
	return ""
}

func (s *seats) Sweep(ctx context.Context, open func(int64) bool) error {
	_, err := s.term.Sweep(ctx, open)
	return err
}

func (s *seats) Notices() []runner.SeatNotice { return s.board.List() }

// notifiableKind is false for advisory attention kinds that never need the owner: a same-task
// conflicting-guidance note is advice for the agents and, capped to a top ten, it moves in and out of the
// list and used to re-fire the same notification over and over (owner 11-24).
func notifiableKind(kind string) bool {
	switch kind {
	case "conflicting_guidance", "conflicting_guidance_summary", "started_from_backlog":
		return false
	}
	return true
}

// attentionWatcher sends the one notification text when the attention list gains an item.
func attentionWatcher(eng *engine.Engine, st *store.Store) *notify.Watcher {
	return &notify.Watcher{
		Keys: func(ctx context.Context) ([]string, error) {
			items, err := eng.Attention(ctx)
			if err != nil {
				return nil, err
			}
			keys := make([]string, 0, len(items))
			for _, it := range items {
				if !notifiableKind(it.Kind) {
					continue
				}
				keys = append(keys, notify.Key(it.Kind, it.Project, strconv.FormatInt(it.Effort, 10), it.Task, strconv.FormatInt(it.Stage, 10)))
			}
			return keys, nil
		},
		Config: func(ctx context.Context) notify.Config {
			set, err := st.GetSettings(ctx)
			if err != nil {
				return notify.Config{}
			}
			return notify.Config{MacOS: set.NotifyMacOS, Command: set.NotifyCommand, URL: set.NotifyURL}
		},
	}
}

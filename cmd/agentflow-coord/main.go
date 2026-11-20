// Command agentflow-coord runs one pass of agentflow's coordinator module (AF-13) from the
// command line, against a coord.json, and prints its decisions. With -dry (the default) it
// writes nothing: no BotChat write, no merge, no queue file, no state. coord-fs3 uses it to
// compare the module with coordinator.py --dry before the switch-over.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"cmd184psu/unified-webapp/internal/agentflow/botchat"
	"cmd184psu/unified-webapp/internal/agentflow/coord"
	"cmd184psu/unified-webapp/internal/agentflow/runner/mux"
	"cmd184psu/unified-webapp/internal/agentflow/runner/seat"
)

func main() {
	cfgPath := flag.String("config", "", "coord.json (default: ./coord.json)")
	dry := flag.Bool("dry", true, "decide only; -dry=false runs for real")
	asJSON := flag.Bool("json", false, "print the report as JSON")
	queuesOut := flag.String("queues-out", "", "a dry run writes the queue files it would have written into this directory, to compare")
	watch := flag.Bool("watch", false, "instead of a pass, look at each seat's screen once (read only: never types) and print what the watcher sees")
	flag.Parse()
	dir := "."
	if *cfgPath != "" {
		dir = filepath.Dir(*cfgPath)
		if filepath.Base(*cfgPath) != coord.ConfigFile {
			fmt.Fprintln(os.Stderr, "agentflow-coord: the file must be named", coord.ConfigFile)
			os.Exit(2)
		}
	}
	cfg, err := coord.Load(dir)
	if err != nil || cfg == nil {
		fmt.Fprintln(os.Stderr, "agentflow-coord: no usable coord.json:", err)
		os.Exit(2)
	}
	if *watch {
		w := &coord.Watcher{Cfg: cfg, Screens: &seat.Adapter{Mux: func(context.Context) (mux.Multiplexer, error) { return mux.New("") }}}
		cfg.DryRun = true // never types, never notifies
		w.Pass(context.Background(), true)
		for _, st := range w.Seats() {
			fmt.Printf("%-10s session %-14s running=%v ready=%v dead=%v halt=%q\n", st.Callsign, st.Session, st.Running, st.Ready, st.Dead, st.Halt)
		}
		return
	}
	c := &coord.Coordinator{Cfg: cfg, Bus: coord.HTTPBus{C: &botchat.Client{BaseURL: cfg.BotChatURL, KeyFile: cfg.KeyFile}},
		Git: coord.ExecGit{Repos: cfg.Repos, MergeDir: cfg.MergeDir}}
	rep := c.Run(context.Background(), *dry)
	if *queuesOut != "" {
		for cs, text := range rep.Queues {
			if err := os.WriteFile(filepath.Join(*queuesOut, cs+".md"), []byte(text), 0o644); err != nil {
				fmt.Fprintln(os.Stderr, "agentflow-coord:", err)
			}
		}
	}
	if *asJSON {
		_ = json.NewEncoder(os.Stdout).Encode(rep)
	} else {
		for _, d := range rep.Decisions {
			fmt.Printf("%-16s %-9s %-10s %s\n", d.Kind, d.Task, d.Seat, d.Detail)
		}
		for _, w := range rep.Writes {
			fmt.Println("held back:", w)
		}
	}
	if rep.Error != "" {
		fmt.Fprintln(os.Stderr, "agentflow-coord:", rep.Error)
		os.Exit(1)
	}
}

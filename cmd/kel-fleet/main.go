// kel-fleet prints the kelsier fleet document as JSON.
//
// It is what `kel _fleet` runs: bin/kel's fleet_json() execs it, and since
// v0.10 there is no bash fallback — see internal/fleet.
//
//	kel-fleet [--dirty] [--land] [--pr] [--panes] [--here] [--worktrees]
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"github.com/tharpep/kelsier/internal/fleet"
)

func detachedRefresh(string) {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	cmd := exec.Command(exe, "--refresh-claude-agents")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if cmd.Start() == nil {
		go cmd.Wait()
	}
}

func main() {
	fleet.StartClaudeRefresh = detachedRefresh
	var opt fleet.Options
	for _, a := range os.Args[1:] {
		switch a {
		case "--dirty":
			opt.Dirty = true
		case "--land":
			opt.Land = true
		case "--pr":
			opt.PR = true
		case "--panes":
			opt.Panes = true
		case "--here":
			opt.Here = true
		case "--worktrees":
			opt.Worktrees = true
		case "--refresh-claude-agents":
			fleet.RefreshClaudeAgents(fleet.StateDir())
			return
		case "-h", "--help":
			fmt.Fprintln(os.Stderr, "usage: kel-fleet [--dirty] [--land] [--pr] [--panes] [--here] [--worktrees]")
			return
		default:
			fmt.Fprintf(os.Stderr, "kel-fleet: unknown flag %s\n", a)
			os.Exit(2)
		}
	}

	enc := json.NewEncoder(os.Stdout)
	if err := enc.Encode(fleet.Load(opt)); err != nil {
		fmt.Fprintf(os.Stderr, "kel-fleet: %v\n", err)
		os.Exit(1)
	}
}

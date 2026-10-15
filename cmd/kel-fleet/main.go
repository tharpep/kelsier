// kel-fleet prints the kelsier fleet document as JSON.
//
// It is what `kel _fleet` runs: bin/kel's fleet_json() execs it, and since
// v0.10 there is no bash fallback — see internal/fleet.
//
//	kel-fleet [--dirty] [--land] [--pr] [--panes] [--here]
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/tharpep/kelsier/internal/fleet"
)

func main() {
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
		case "-h", "--help":
			fmt.Fprintln(os.Stderr, "usage: kel-fleet [--dirty] [--land] [--pr] [--panes] [--here]")
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

// kel-fleet prints the kelsier fleet document as JSON.
//
// It is a drop-in replacement for `kel _fleet` in bin/kel: bin/kel's
// fleet_json() runs this when $KEL_FLEET_BIN exists and falls back to the bash
// implementation otherwise. The two must agree — see internal/fleet.
//
//	kel-fleet [--dirty]
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
		case "-h", "--help":
			fmt.Fprintln(os.Stderr, "usage: kel-fleet [--dirty] [--land] [--pr]")
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

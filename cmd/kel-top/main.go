// kel-top is the sidebar: the pane on the left of every kel window
// (sidebar.go, sbview.go).
//
//	kel-top --sidebar                      the live pane
//	kel-top --sidebar --once               render one frame to stdout and exit
//	kel-top --sidebar --fixture FILE|DIR   render saved fleet documents, for
//	        [--width N] [--height N]       the golden frames (spec.md §5e)
//
// It was a dashboard as well, `kel top`, until the sidebar replaced it: one
// surface (invariant 6), so the board and its own key scheme went.
package main

import (
	"fmt"
	"os"
	"strings"
)

// triageRank puts the row you must deal with first at the top.
func triageRank(state *string) int {
	if state == nil {
		return 5
	}
	switch *state {
	case "waiting":
		return 0
	case "working":
		return 1
	case "throttled":
		return 2
	case "done":
		return 3
	case "dead":
		return 6
	default: // idle
		return 5
	}
}

func f(p *float64) float64 {
	if p == nil {
		return 0
	}
	return *p
}

const (
	reset  = "\x1b[0m"
	dim    = "\x1b[2m"
	bold   = "\x1b[1m"
	yellow = "\x1b[33m"
	red    = "\x1b[31m"
	cyan   = "\x1b[36m"
	green  = "\x1b[32m"
	purple = "\x1b[35m"
)

// humanizeSecs matches bin/kel's helper: 45s, 6m, 1h05m.
func humanizeSecs(s int64) string {
	switch {
	case s < 0:
		return "-"
	case s < 60:
		return fmt.Sprintf("%ds", s)
	case s < 3600:
		return fmt.Sprintf("%dm", s/60)
	default:
		return fmt.Sprintf("%dh%02dm", s/3600, (s%3600)/60)
	}
}

func money(v float64) string { return fmt.Sprintf("$%.2f", v) }

func truncate(s string, n int) string {
	if n <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n == 1 {
		return "…"
	}
	return string(r[:n-1]) + "…"
}

func pad(s string, n int) string {
	if n <= 0 {
		return "" // a squeezed pane asks for negative widths; slicing them panics
	}
	r := []rune(s)
	if len(r) >= n {
		return string(r[:n])
	}
	return s + strings.Repeat(" ", n-len(r))
}

func main() {
	once, sidebar := false, false
	fixture, widths, height := "", []int{28, 32, 40}, 0
	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch a {
		case "--fixture", "--width", "--height":
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "kel-top: %s needs a value\n", a)
				os.Exit(2)
			}
			i++
			switch a {
			case "--fixture":
				fixture = args[i]
			case "--width":
				var w int
				fmt.Sscanf(args[i], "%d", &w)
				widths = []int{w}
			default:
				fmt.Sscanf(args[i], "%d", &height)
			}
		case "--once":
			once = true
		case "--sidebar":
			sidebar = true
		case "-h", "--help":
			fmt.Fprintln(os.Stderr, "usage: kel-top --sidebar [--once] [--fixture FILE|DIR [--width N] [--height N]]")
			return
		default:
			fmt.Fprintf(os.Stderr, "kel-top: unknown flag %s\n", a)
			os.Exit(2)
		}
	}
	if !sidebar {
		fmt.Fprintln(os.Stderr, "kel-top: --sidebar is the only mode; `kel top` was replaced by the sidebar")
		os.Exit(2)
	}
	if fixture != "" {
		runFixtures(fixture, widths, height)
		return
	}
	runSidebar(once)
}

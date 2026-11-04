// kel-sidebar is the pane on the left of every kel window (sidebar.go,
// sbview.go).
//
//	kel-sidebar                      the live pane
//	kel-sidebar --once               render one frame to stdout and exit
//	kel-sidebar --fixture FILE|DIR   render saved fleet documents, for
//	    [--width N] [--height N]     the golden frames (spec.md §5e)
package main

import (
	"fmt"
	"os"
	"strings"
)

// triageRank orders states by urgency: a space shows its most urgent agent's.
func triageRank(state *string) int {
	if state == nil {
		return 5
	}
	switch *state {
	case "waiting", "error":
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

// kel's own theme (spec.md §5d), the same on every machine. The surface the
// sidebar sits on is the pane's tmux style (KEL_SIDEBAR_STYLE in bin/kel);
// these must stay readable on it.
const (
	reset  = "\x1b[0m"
	bold   = "\x1b[1m"
	dim    = "\x1b[38;2;107;107;116m"
	mid    = "\x1b[38;2;161;161;170m"
	faint  = "\x1b[38;2;85;85;94m"
	yellow = "\x1b[38;2;245;167;66m"
	red    = "\x1b[38;2;242;114;127m"
	cyan   = "\x1b[38;2;86;212;221m"
	green  = "\x1b[38;2;95;211;141m"
	purple = "\x1b[38;2;180;140;242m"

	raised   = "\x1b[48;2;29;29;34m"
	yellowBg = "\x1b[48;2;46;34;16m"
	redBg    = "\x1b[48;2;49;21;25m"
	greenBg  = "\x1b[48;2;18;41;27m"
	purpleBg = "\x1b[48;2;36;27;54m"
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
	once := false
	fixture, widths, height := "", []int{28, 32, 40}, 0
	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch a {
		case "--fixture", "--width", "--height":
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "kel-sidebar: %s needs a value\n", a)
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
		case "-h", "--help":
			fmt.Fprintln(os.Stderr, "usage: kel-sidebar [--once] [--fixture FILE|DIR [--width N] [--height N]]")
			return
		default:
			fmt.Fprintf(os.Stderr, "kel-sidebar: unknown flag %s\n", a)
			os.Exit(2)
		}
	}
	if fixture != "" {
		runFixtures(fixture, widths, height)
		return
	}
	runSidebar(once)
}

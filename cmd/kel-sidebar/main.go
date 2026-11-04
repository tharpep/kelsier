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

const (
	reset = "\x1b[0m"
	bold  = "\x1b[1m"
)

// The state colours are kel's, the same on every theme.
const (
	yellow = "\x1b[38;2;245;167;66m"
	red    = "\x1b[38;2;242;114;127m"
	cyan   = "\x1b[38;2;86;212;221m"
	green  = "\x1b[38;2;95;211;141m"
	purple = "\x1b[38;2;180;140;242m"
)

// The surfaces and text greys are built from the terminal's own background
// and foreground (applyPalette, spec.md §5d), so the sidebar wears the
// theme's hue. These are the values until the terminal answers, and what the
// golden frames are drawn with: kel's fixed dark, measured against a black
// terminal.
var (
	baseBg   = rgb{0x13, 0x13, 0x16}
	baseFg   = rgb{0xe6, 0xe6, 0xe9}
	base     = baseFg.fg() + baseBg.bg()
	dim      = rgb{107, 107, 116}.fg()
	mid      = rgb{161, 161, 170}.fg()
	faint    = rgb{85, 85, 94}.fg()
	lifted   = rgb{29, 29, 34}
	raised   = lifted.bg()
	yellowBg = rgb{46, 34, 16}
	redBg    = rgb{49, 21, 25}
	greenBg  = rgb{18, 41, 27}
	purpleBg = rgb{36, 27, 54}
)

type rgb struct{ r, g, b uint8 }

func (c rgb) fg() string  { return fmt.Sprintf("\x1b[38;2;%d;%d;%dm", c.r, c.g, c.b) }
func (c rgb) bg() string  { return fmt.Sprintf("\x1b[48;2;%d;%d;%dm", c.r, c.g, c.b) }
func (c rgb) hex() string { return fmt.Sprintf("#%02x%02x%02x", c.r, c.g, c.b) }

// mix is c moved toward o by t (0 = c, 1 = o).
func (c rgb) mix(o rgb, t float64) rgb {
	m := func(a, b uint8) uint8 { return uint8(float64(a) + (float64(b)-float64(a))*t + 0.5) }
	return rgb{m(c.r, o.r), m(c.g, o.g), m(c.b, o.b)}
}

func (c rgb) dark() bool { return 0.2126*float64(c.r)+0.7152*float64(c.g)+0.0722*float64(c.b) < 128 }

func parseHex(s string) (rgb, bool) {
	var c rgb
	if len(s) != 7 || s[0] != '#' {
		return c, false
	}
	_, err := fmt.Sscanf(s[1:], "%02x%02x%02x", &c.r, &c.g, &c.b)
	return c, err == nil
}

// applyPalette builds every surface and grey from the terminal's background
// and foreground: the sidebar a shade darker than the panes beside it, the
// selection a shade lighter on a dark theme (darker on a light one), the
// greys between the text and the surface.
func applyPalette(term, text rgb) {
	black, white := rgb{}, rgb{255, 255, 255}
	surface, lift := term.mix(black, 0.22), term.mix(white, 0.08)
	if !term.dark() {
		surface, lift = term.mix(black, 0.04), term.mix(black, 0.10)
	}
	baseBg, baseFg = surface, text
	base = text.fg() + surface.bg()
	mid = text.mix(surface, 0.30).fg()
	dim = text.mix(surface, 0.50).fg()
	faint = text.mix(surface, 0.64).fg()
	lifted, raised = lift, lift.bg()
	chip := func(c rgb) rgb { return c.mix(surface, 0.80) }
	yellowBg, redBg = chip(rgb{245, 167, 66}), chip(rgb{242, 114, 127})
	greenBg, purpleBg = chip(rgb{95, 211, 141}), chip(rgb{180, 140, 242})
}

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

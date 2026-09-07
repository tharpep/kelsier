package main

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
)

// keys is kel-top's own navigation bindings. Update() matches against these
// same values (key.Matches) and the footer's help.Model renders help text
// from these same values — one struct, so a key cannot go undocumented
// (#27) or documented-but-unhandled without this file changing too.
//
// This is a DIFFERENT list from keymap.tsv (keymap.go): that file is
// per-agent and fleet-wide actions Phase 3/4 wire up, read by both bin/kel
// and kel-top. Navigation has no bash-side counterpart to stay in sync
// with, so it lives here instead of duplicating a file that would only ever
// have one reader. The one exception is Jump: its key is "enter", already
// reserved in keymap.tsv's agent/jump row, so its HELP TEXT is sourced from
// there (interface.md §6: "help renders from the file") rather than
// hand-written a second time — Jump itself stays a no-op this phase (#3);
// only Phase 4 gives it the real action.
type keys struct {
	Up, Down, Top, Bottom              key.Binding
	Sort, Filter, Reload               key.Binding
	Jump, ToggleHelp, Quit             key.Binding
	FilterDone, FilterBack, FilterQuit key.Binding
}

func newKeys(jumpHelp string) keys {
	if jumpHelp == "" {
		jumpHelp = "jump to it" // keymap.tsv's own text for this row; belt and suspenders
	}
	return keys{
		Up:         key.NewBinding(key.WithKeys("k", "up"), key.WithHelp("k/↑", "up")),
		Down:       key.NewBinding(key.WithKeys("j", "down"), key.WithHelp("j/↓", "down")),
		Top:        key.NewBinding(key.WithKeys("g", "home"), key.WithHelp("g", "top")),
		Bottom:     key.NewBinding(key.WithKeys("G", "end"), key.WithHelp("G", "bottom")),
		Sort:       key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "sort")),
		Filter:     key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
		Reload:     key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "reload")),
		Jump:       key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", jumpHelp)),
		ToggleHelp: key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		Quit:       key.NewBinding(key.WithKeys("q", "esc", "ctrl+c"), key.WithHelp("q/esc/ctrl+c", "quit")),
		FilterDone: key.NewBinding(key.WithKeys("enter", "esc"), key.WithHelp("enter/esc", "done")),
		FilterBack: key.NewBinding(key.WithKeys("backspace"), key.WithHelp("backspace", "delete")),
		FilterQuit: key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("ctrl+c", "quit")),
	}
}

// normalHelp and filterHelp are the two help.KeyMap views over the same
// bindings — which one renders depends on m.filtOn, not on two different
// key sets existing.
type normalHelp keys

func (k normalHelp) ShortHelp() []key.Binding {
	// ShortHelpView adds items left to right until the width runs out, with
	// no reordering of its own -- so whatever is listed last is what a
	// narrow terminal loses first. Quit goes early on purpose: it is the one
	// thing a stuck user most needs to still see, and Jump goes last since
	// it is this phase's reserved no-op, the one entry safest to drop.
	return []key.Binding{k.Quit, k.Filter, k.Sort, k.Down, k.Up, k.Top, k.Bottom, k.Reload, k.ToggleHelp, k.Jump}
}

func (k normalHelp) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Top, k.Bottom},
		{k.Sort, k.Filter, k.Reload, k.Jump},
		{k.ToggleHelp, k.Quit},
	}
}

type filterHelp keys

func (k filterHelp) ShortHelp() []key.Binding {
	return []key.Binding{k.FilterDone, k.FilterBack, k.FilterQuit}
}

func (k filterHelp) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.FilterDone, k.FilterBack, k.FilterQuit}}
}

// helpTextFor looks up one keymap.tsv row's help column. Used only for Jump,
// which is the one navigation key that already has an entry there.
func helpTextFor(rows []keyBinding, scope, action string) string {
	for _, r := range rows {
		if r.Scope == scope && r.Action == action {
			return r.Help
		}
	}
	return ""
}

// dumpHelpKeys is --dump-help-keys' payload: one line per literal key
// ("k/up" expands to two lines), scope-prefixed by mode since Jump's "enter"
// and FilterDone's "enter" are different bindings in different modes. This
// is the bidirectional guard's data half — test/kel-test.sh asserts it
// against a hand-kept expected set, the same shape as the keymap.tsv parity
// guard in keymap.go.
func dumpHelpKeys(k keys) string {
	var b strings.Builder
	add := func(mode string, bnd key.Binding) {
		for _, lit := range bnd.Keys() {
			fmt.Fprintf(&b, "%s\t%s\t%s\n", mode, lit, bnd.Help().Desc)
		}
	}
	add("normal", k.Up)
	add("normal", k.Down)
	add("normal", k.Top)
	add("normal", k.Bottom)
	add("normal", k.Sort)
	add("normal", k.Filter)
	add("normal", k.Reload)
	add("normal", k.Jump)
	add("normal", k.ToggleHelp)
	add("normal", k.Quit)
	add("filter", k.FilterDone)
	add("filter", k.FilterBack)
	add("filter", k.FilterQuit)
	return b.String()
}

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
type keys struct {
	Up, Down, Top, Bottom              key.Binding
	Sort, Filter, Reload               key.Binding
	Jump, ToggleHelp, Quit             key.Binding
	FilterDone, FilterBack, FilterQuit key.Binding
}

func newKeys() keys {
	return keys{
		Up:         key.NewBinding(key.WithKeys("k", "up"), key.WithHelp("k/↑", "up")),
		Down:       key.NewBinding(key.WithKeys("j", "down"), key.WithHelp("j/↓", "down")),
		Top:        key.NewBinding(key.WithKeys("g", "home"), key.WithHelp("g", "top")),
		Bottom:     key.NewBinding(key.WithKeys("G", "end"), key.WithHelp("G", "bottom")),
		Sort:       key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "sort")),
		Filter:     key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
		Reload:     key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "reload")),
		Jump:       key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "jump to it")),
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

// dumpHelpKeys is --dump-help-keys' payload: one line per literal key
// ("k/up" expands to two lines), scope-prefixed by mode since Jump's "enter"
// and FilterDone's "enter" are different bindings in different modes. This
// is the guard's data half — test/kel-test.sh asserts it against a
// hand-kept expected set.
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

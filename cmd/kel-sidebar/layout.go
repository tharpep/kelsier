package main

import (
	"os"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// sbLayout is one of `kel layout`'s named layouts, as the menu draws it.
// name, min and max are kel's (layout_table in bin/kel, pinned by
// TestLayoutsMatchKel); the words and the thumbnail are the menu's.
type sbLayout struct {
	name     string
	min, max int
	title    string
	sub      string
	thumb    [5]string
}

var sbLayouts = []sbLayout{
	{"below", 2, 9, "agent + shell", "shell below", [5]string{
		"┌───────┐", "│●      │", "├───────┤", "│$      │", "└───────┘"}},
	{"editor", 2, 9, "agent + editor", "editor right 40%", [5]string{
		"┌────┬──┐", "│●   │▪ │", "├────┤  │", "│$   │  │", "└────┴──┘"}},
	{"side", 2, 2, "side by side", "half and half", [5]string{
		"┌───┬───┐", "│●  │$  │", "│   │   │", "│   │   │", "└───┴───┘"}},
	{"main", 3, 9, "main + stack", "stack on the right", [5]string{
		"┌────┬──┐", "│●   │$ │", "│    ├──┤", "│    │$ │", "└────┴──┘"}},
	{"grid", 4, 4, "grid", "two by two", [5]string{
		"┌───┬───┐", "│●  │$  │", "├───┼───┤", "│$  │▪  │", "└───┴───┘"}},
	{"columns", 3, 9, "even columns", "one column each", [5]string{
		"┌──┬─┬──┐", "│● │$│$ │", "│  │ │  │", "│  │ │  │", "└──┴─┴──┘"}},
}

// layoutsFor are the layouts that fit n panes, in the menu's order.
func layoutsFor(n int) []sbLayout {
	var out []sbLayout
	for _, l := range sbLayouts {
		if n >= l.min && n <= l.max {
			out = append(out, l)
		}
	}
	return out
}

func (l sbLayout) fits() string {
	if l.min == l.max {
		return "fits " + strconv.Itoa(l.min) + " panes"
	}
	return "fits " + strconv.Itoa(l.min) + "+ panes"
}

// sbLayoutMenu is the layout menu, open over the inspector for one window.
type sbLayoutMenu struct {
	win   string
	title string
	sel   int
}

// layoutWindow is the window the layout menu would arrange: the selected
// agent's or pane's, else the sidebar's own.
func (m sbModel) layoutWindow() (wid, name string) {
	v := m.view()
	if a, ok := m.agentOf(v.sel); ok {
		wid = a.wid
	} else if p, ok := m.paneOf(v.sel); ok {
		wid = p.WindowID
	} else {
		wid = m.ui.myWindow
	}
	for _, a := range v.agents {
		if a.wid == wid {
			return wid, a.agent.Name + "'s window"
		}
	}
	return wid, "this window"
}

func (m *sbModel) openLayouts() {
	if m.board || m.mode != modeNormal {
		return
	}
	wid, name := m.layoutWindow()
	if wid == "" {
		return
	}
	if len(layoutsFor(m.view().windowPanes(wid))) == 0 {
		m.msg, m.msgBad = "split the window first: a layout needs 2 panes", true
		return
	}
	m.menu = nil
	m.layouts = &sbLayoutMenu{win: wid, title: "Layout · " + name}
}

func layoutUndoable(wid string) bool {
	_, err := os.Stat(sbStatePath(wid + ".layout"))
	return err == nil
}

func (m sbModel) applyLayout(name string) (tea.Model, tea.Cmd) {
	win := m.layouts.win
	m.layouts = nil
	if name == "undo" {
		if !layoutUndoable(win) {
			return m, nil
		}
		return m, runKel("", nil, "layout", "undo", win)
	}
	return m, runKel("", nil, "layout", name, win)
}

func (m sbModel) layoutKey(k string) (tea.Model, tea.Cmd) {
	opts := layoutsFor(m.view().windowPanes(m.layouts.win))
	switch k {
	case "up", "k":
		m.layouts.sel = (m.layouts.sel + len(opts) - 1) % max(1, len(opts))
	case "down", "j":
		m.layouts.sel = (m.layouts.sel + 1) % max(1, len(opts))
	case "enter":
		if m.layouts.sel < len(opts) {
			return m.applyLayout(opts[m.layouts.sel].name)
		}
	case "u":
		return m.applyLayout("undo")
	case "1", "2", "3", "4", "5", "6", "7", "8", "9":
		if n := int(k[0] - '1'); n < len(opts) {
			return m.applyLayout(opts[n].name)
		}
	case "esc", "left", "q", "L", "shift+l":
		m.layouts = nil
	}
	return m, nil
}

// layoutLines draws the layout menu: a thumbnail per layout that fits the
// window, its words and its key, then how to answer. Too short a sidebar
// gets one line per layout.
func (v sbView) layoutLines(w, h int) []line {
	mn := v.layouts
	opts := layoutsFor(v.windowPanes(mn.win))
	if w < 20 || len(opts) == 0 {
		return nil
	}
	row := func(s string, id string) line {
		return line{fill(s, w, raised), lineRef{id: id, kind: "menuitem"}}
	}
	out := []line{row(spread(" "+bold+truncate(mn.title, w-7)+reset, dim+"esc"+reset, w), "lo:esc"), row("", "")}
	full := 3+len(opts)*5 <= h || h <= 0
	for i, l := range opts {
		id := "lo:" + l.name
		bar, tc := " ", ""
		if i == mn.sel {
			bar, tc = accent+"▌"+reset, bold
		}
		key := dim + strconv.Itoa(i+1) + reset
		if !full {
			out = append(out, row(spread(bar+key+" "+tc+truncate(l.title+" · "+l.sub, w-5)+reset, "", w), id))
			continue
		}
		tw := w - 12
		words := []string{"", tc + truncate(l.title, tw-2) + reset, dim + truncate(l.sub, tw) + reset, faint + truncate(l.fits(), tw) + reset, ""}
		for j, t := range l.thumb {
			s := bar + faint + t + reset + " " + words[j]
			if j == 1 {
				s = spread(s, key, w)
			}
			out = append(out, row(s, id))
		}
	}
	hint := "↵ apply · 1-" + strconv.Itoa(len(opts))
	if layoutUndoable(mn.win) {
		hint += " · u undo"
	}
	hint += " · esc"
	foot := row(" "+dim+hint+reset, "lo:hint")
	foot.ref.text = hint
	return append(out, foot)
}

// layoutClick: a layout clicked is applied, a word of the hint line is its
// key, anything else closes the menu.
func (m sbModel) layoutClick(ref lineRef, x int) (tea.Model, tea.Cmd) {
	name, ok := strings.CutPrefix(ref.id, "lo:")
	switch {
	case !ok || name == "esc":
		m.layouts = nil
		return m, nil
	case name == "hint":
		pos := 1
		for _, seg := range strings.Split(ref.text, " · ") {
			n := len([]rune(seg))
			if x < pos+n+3 {
				switch strings.Fields(seg)[0] {
				case "↵":
					return m.layoutKey("enter")
				case "u", "esc":
					return m.layoutKey(strings.Fields(seg)[0])
				}
				return m, nil
			}
			pos += n + 3
		}
		return m, nil
	}
	return m.applyLayout(name)
}

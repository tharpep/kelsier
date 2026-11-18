package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/tharpep/kelsier/internal/fleet"
)

// atModel is the fifteen-agent fixture at 32x40 with sel selected.
func atModel(t *testing.T, sel string) sbModel {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	return sbModel{doc: loadFixture(t, "fifteen-three-repos"), width: 32, height: 40, visible: true, ui: uiState{sel: sel}}
}

func press(m sbModel, k string) sbModel {
	var msg tea.KeyPressMsg
	switch k {
	case "right":
		msg = tea.KeyPressMsg{Code: tea.KeyRight}
	case "left":
		msg = tea.KeyPressMsg{Code: tea.KeyLeft}
	case "esc":
		msg = tea.KeyPressMsg{Code: tea.KeyEscape}
	case "enter":
		msg = tea.KeyPressMsg{Code: tea.KeyEnter}
	case "down":
		msg = tea.KeyPressMsg{Code: tea.KeyDown}
	case "up":
		msg = tea.KeyPressMsg{Code: tea.KeyUp}
	case "space":
		msg = tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	default:
		msg = tea.KeyPressMsg{Code: rune(k[0]), Text: k}
	}
	nm, _ := m.Update(msg)
	return nm.(sbModel)
}

func click(m sbModel, y int, b tea.MouseButton) sbModel {
	nm, _ := m.Update(tea.MouseClickMsg{X: 4, Y: y, Button: b})
	return nm.(sbModel)
}

// inspectorHit is the row and column of the inspector button pressing key.
func inspectorHit(t *testing.T, m sbModel, key string) (x, y int) {
	t.Helper()
	_, refs := renderView(m.view(), m.width, m.height, 0)
	for i, r := range refs {
		if r.kind != "inspector" {
			continue
		}
		for _, h := range r.hits {
			if h.key == key {
				return h.x0, i
			}
		}
	}
	t.Fatalf("no inspector button %q", key)
	return 0, 0
}

// The entry menu opens on a right-click, on the inspector's ⋯ and on →, and
// a hover opens nothing.
func TestEntryMenuOpens(t *testing.T) {
	m := atModel(t, "a:@1")
	if got := click(m, rowOf(t, m, "a:@2"), tea.MouseRight); got.menu == nil || got.menu.entry != "a:@2" || got.ui.sel != "a:@2" {
		t.Errorf("right-click: sel %q menu %+v", got.ui.sel, got.menu)
	}
	x, y := inspectorHit(t, m, "menu")
	nm, _ := m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	if got := nm.(sbModel); got.menu == nil || got.menu.entry != "a:@1" {
		t.Errorf("⋯: %+v", got.menu)
	}
	if got := press(m, "right"); got.menu == nil || got.menu.entry != "a:@1" {
		t.Errorf("right arrow: %+v", got.menu)
	}
	nm, _ = m.Update(tea.MouseMotionMsg{X: 4, Y: rowOf(t, m, "a:@2")})
	if nm.(sbModel).menu != nil {
		t.Error("hover opened a menu")
	}
	m.mode = modeNew
	if got := click(m, rowOf(t, m, "a:@1"), tea.MouseRight); got.menu != nil {
		t.Errorf("a right-click during a prompt opened %+v", got.menu)
	}
}

// The menu draws over the inspector and the rows above it, its foot on the
// inspector's last row: no list row moves, and the rows it does not cover
// are as they were.
func TestEntryMenuDrawsOverTheInspector(t *testing.T) {
	for _, h := range []int{0, 30, 40} {
		m := atModel(t, "a:@2")
		m.height = h
		before, bref := hoverFrame(m)
		open := press(m, "right")
		after, aref := hoverFrame(open)
		if len(after) != len(before) {
			t.Fatalf("h=%d: %d lines became %d", h, len(before), len(after))
		}
		last := -1
		for i, r := range bref {
			if r.kind == "inspector" {
				last = i
			}
		}
		top := -1
		for i := range aref {
			if aref[i].kind == "menuitem" {
				if top < 0 {
					top = i
				}
				continue
			}
			if after[i] != before[i] {
				t.Errorf("h=%d: row %d changed outside the menu:\n was %q\n now %q", h, i, before[i], after[i])
			}
		}
		if top < 0 || aref[last].kind != "menuitem" || (last+1 < len(aref) && aref[last+1].kind == "menuitem") {
			t.Errorf("h=%d: the menu's foot is not on the inspector's last row (%d)", h, last)
		}
	}
}

// ← esc, enter on an item, a click outside, the wheel and an entry that
// leaves the fleet each close it.
func TestEntryMenuCloses(t *testing.T) {
	m := atModel(t, "a:@2")
	m = press(m, "right")
	if m.menu == nil {
		t.Fatal("did not open")
	}
	for _, k := range []string{"left", "esc", "enter"} {
		if got := press(m, k); got.menu != nil {
			t.Errorf("%s left the menu open", k)
		}
	}
	if got := click(m, 0, tea.MouseLeft); got.menu != nil {
		t.Error("a click outside left the menu open")
	}
	nm, _ := m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	if got := nm.(sbModel); got.menu != nil {
		t.Error("the wheel left the menu open")
	}
	var rest []fleet.Agent
	for _, a := range m.doc.Agents {
		if a.WindowID == nil || *a.WindowID != "@2" {
			rest = append(rest, a)
		}
	}
	doc := m.doc
	doc.Agents = rest
	nm, _ = m.Update(sbLoaded{doc})
	if got := nm.(sbModel); got.menu != nil {
		t.Error("the entry left the fleet and the menu stayed")
	}
}

// ↑ ↓ j k move the cursor, a listed key runs its item and closes, other keys
// do nothing, and a click on an item runs it.
func TestEntryMenuKeys(t *testing.T) {
	m := press(atModel(t, "a:@2"), "right")
	n := len(m.menu.items)
	if m = press(m, "up"); m.menu.sel != n-1 {
		t.Errorf("up from the first item: %d", m.menu.sel)
	}
	m = press(press(m, "down"), "j")
	if m.menu.sel != 1 {
		t.Errorf("down, j: %d", m.menu.sel)
	}
	if m = press(m, "k"); m.menu.sel != 0 {
		t.Errorf("k: %d", m.menu.sel)
	}
	for _, k := range []string{"q", "n", "/", "w"} {
		got := press(m, k)
		if got.menu == nil || got.mode != modeNormal {
			t.Errorf("%q was not ignored: menu %+v mode %v", k, got.menu, got.mode)
		}
	}
	got := press(m, "x")
	if got.menu != nil || got.mode != modeKill || got.target != "a:@2" {
		t.Errorf("x: menu %+v mode %v target %q", got.menu, got.mode, got.target)
	}
	nm, _ := m.Update(tea.MouseClickMsg{X: 4, Y: rowOf(t, m, "mi:x"), Button: tea.MouseLeft})
	if got := nm.(sbModel); got.mode != modeKill || got.target != "a:@2" || got.menu != nil {
		t.Errorf("Kill clicked: mode %v target %q menu %+v", got.mode, got.target, got.menu)
	}
}

// A verb list that changes under an open menu closes it; an unchanged one
// keeps it.
func TestEntryMenuClosesWhenItemsGoStale(t *testing.T) {
	m := press(atModel(t, "a:@2"), "right")
	doc := m.doc
	doc.Agents = append([]fleet.Agent(nil), m.doc.Agents...)
	for i := range doc.Agents {
		if doc.Agents[i].WindowID != nil && *doc.Agents[i].WindowID == "@2" {
			dead := "dead"
			doc.Agents[i].State = &dead
		}
	}
	nm, _ := m.Update(sbLoaded{doc})
	if nm.(sbModel).menu != nil {
		t.Error("relaunch appeared and the menu kept its old items")
	}
	nm, _ = m.Update(sbLoaded{m.doc})
	if nm.(sbModel).menu == nil {
		t.Error("an unchanged fleet closed the menu")
	}
}

// A repo heading's menu, from a right-click (→ opens the fold), holds its
// checkout's verbs, and no Go to it: enter folds a heading.
func TestGroupMenu(t *testing.T) {
	m := atModel(t, "a:@1")
	m = click(m, rowOf(t, m, "g:kelsier"), tea.MouseRight)
	if m.menu == nil || m.ui.sel != "g:kelsier" {
		t.Fatal("no menu on a repo heading")
	}
	keys := ""
	for _, it := range m.menu.items {
		keys += it.key + " "
	}
	if keys != "f g d e t c " {
		t.Errorf("repo menu keys %q", keys)
	}
}

// The menu open over the inspector, and a repo folded by hand, held like
// every other frame — the brief's 32x36.
func TestMenuGolden(t *testing.T) {
	keepPalette(t)
	t.Setenv("HOME", "/home/me")
	doc := loadFixture(t, "fifteen-three-repos")
	now := int64(0)
	if tm, err := time.Parse(time.RFC3339, doc.GeneratedAt); err == nil {
		now = tm.Unix()
	}
	folded := map[string]bool{groupKey("api-gw"): false}
	for _, c := range []struct {
		name       string
		sel        string
		menu, fold bool
		h          int
	}{
		{"folded", "a:@1", false, true, 36},
		{"menu-kelsier", "a:@1", true, true, 36},
		{"menu-usage-charts", "a:@7", true, true, 30},
		{"menu-repo", "g:kelsier", true, true, 36},
		{"pane-in-agent-window", "%7", false, false, 36},
	} {
		t.Setenv("XDG_STATE_HOME", t.TempDir())
		m := sbModel{doc: doc, width: 32, height: c.h, visible: true, ui: uiState{sel: c.sel, focused: true, now: now}}
		if c.fold {
			m.ui.expanded = folded
		}
		if c.menu {
			m.openMenu()
			if m.menu == nil {
				t.Fatalf("%s: no menu", c.name)
			}
		}
		got, _ := renderView(m.view(), 32, c.h, now)
		gold := filepath.Join(fixtures, "fifteen-three-repos."+c.name+".32x"+itoa(c.h)+".golden")
		if *update {
			if err := os.WriteFile(gold, []byte(got+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		want, err := os.ReadFile(gold)
		if err != nil {
			t.Fatalf("%s: %v", gold, err)
		}
		if got+"\n" != string(want) {
			t.Errorf("%s changed:\n--- got\n%s\n--- want\n%s", gold, plain(got), plain(string(want)))
		}
	}
}

// A pane with no row of its own — one in an agent's window — is a click in
// the agent's inspector; selected, its agent's row keeps the bar and the
// inspector shows the pane.
func TestInnerPaneSelectsFromTheInspector(t *testing.T) {
	m := atModel(t, "a:@4")
	x, y := inspectorHit(t, m, "sel:%7")
	nm, _ := m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	m = nm.(sbModel)
	if m.ui.sel != "%7" || m.view().sel != "%7" {
		t.Fatalf("sel %q, view %q", m.ui.sel, m.view().sel)
	}
	lines, _ := hoverFrame(m)
	if row := lines[rowOf(t, m, "a:@4")]; !strings.HasPrefix(row, "▌") {
		t.Errorf("the owner's row lost its bar: %q", row)
	}
	if !strings.Contains(strings.Join(lines, "\n"), "python3 :8000") {
		t.Error("the inspector does not show the pane")
	}
	acts := entryMenu(m.view())
	has := map[string]bool{}
	for _, a := range acts {
		has[a.key] = true
	}
	if !has["m"] || !has["M"] || !has["S"] {
		t.Errorf("the pane's menu lacks its pane verbs: %+v", acts)
	}
	if got := press(m, "j"); got.ui.sel != "a:@5" {
		t.Errorf("j from an inner pane went to %q", got.ui.sel)
	}
}

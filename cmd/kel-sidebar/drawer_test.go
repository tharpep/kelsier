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

func drawerModel(t *testing.T, sel string) sbModel {
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

// The drawer opens on a right-click, on ⋯ and on →, and nothing else opens it.
func TestDrawerOpens(t *testing.T) {
	m := drawerModel(t, "a:@1")
	if got := click(m, rowOf(t, m, "a:@2"), tea.MouseRight); got.drawer == nil || got.drawer.id != "a:@2" {
		t.Errorf("right-click: %+v", got.drawer)
	}
	y := rowOf(t, m, "a:@2")
	nm, _ := m.Update(tea.MouseMotionMsg{X: 4, Y: y})
	hov := nm.(sbModel)
	_, refs := renderView(hov.view(), 32, 40, 0)
	nm, _ = hov.Update(tea.MouseClickMsg{X: refs[y].hits[len(refs[y].hits)-1].x0, Y: y, Button: tea.MouseLeft})
	if got := nm.(sbModel); got.drawer == nil || got.drawer.id != "a:@2" {
		t.Errorf("⋯ button: %+v", got.drawer)
	}
	if got := press(m, "right"); got.drawer == nil || got.drawer.id != "a:@1" {
		t.Errorf("right arrow: %+v", got.drawer)
	}
	if hov.drawer != nil {
		t.Error("hover opened a drawer")
	}
}

// ← esc, enter on an item, a left click outside, a selection that moves and
// an entry that leaves the fleet each close it.
func TestDrawerCloses(t *testing.T) {
	m := drawerModel(t, "a:@2")
	m.height = 50
	m = press(m, "right")
	if m.drawer == nil {
		t.Fatal("did not open")
	}
	for _, k := range []string{"left", "esc", "enter"} {
		if got := press(m, k); got.drawer != nil {
			t.Errorf("%s left the drawer open", k)
		}
	}
	if got := click(m, 0, tea.MouseLeft); got.drawer != nil {
		t.Error("a click outside left the drawer open")
	}
	nm, _ := m.Update(tea.MouseClickMsg{X: 4, Y: rowOf(t, m, "a:@4"), Button: tea.MouseLeft})
	if got := nm.(sbModel); got.drawer != nil || got.ui.sel != "a:@4" {
		t.Errorf("selecting another entry: sel %q drawer %+v", got.ui.sel, got.drawer)
	}
	nm, _ = m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	if got := nm.(sbModel); got.drawer != nil {
		t.Error("the selection moved and the drawer stayed")
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
	if got := nm.(sbModel); got.drawer != nil {
		t.Error("the entry left the fleet and the drawer stayed")
	}
}

// ↑ ↓ j k move the cursor, a letter runs its item and closes, other keys do
// nothing, and a click on an item runs it.
func TestDrawerKeys(t *testing.T) {
	m := press(drawerModel(t, "a:@2"), "right")
	n := len(m.drawer.items)
	if m = press(m, "up"); m.drawer.sel != n-1 {
		t.Errorf("up from the first item: %d", m.drawer.sel)
	}
	m = press(press(m, "down"), "j")
	if m.drawer.sel != 1 {
		t.Errorf("down, j: %d", m.drawer.sel)
	}
	if m = press(m, "k"); m.drawer.sel != 0 {
		t.Errorf("k: %d", m.drawer.sel)
	}
	for _, k := range []string{"q", "n", "/", "w"} {
		got := press(m, k)
		if got.drawer == nil || got.mode != modeNormal {
			t.Errorf("%q was not ignored: drawer %+v mode %v", k, got.drawer, got.mode)
		}
	}
	got := press(m, "x")
	if got.drawer != nil || got.mode != modeKill || got.target != "a:@2" {
		t.Errorf("x: drawer %+v mode %v target %q", got.drawer, got.mode, got.target)
	}
	if got := press(press(m, "up"), "enter"); got.drawer != nil {
		t.Error("enter on an item left the drawer open")
	}
}

// Only what the document holds is drawn: the git position when known.
func TestDrawerFactsOmitUnknowns(t *testing.T) {
	text := func(git *fleet.Git) string {
		d := &sbDrawer{items: []sbAct{{"enter", "Go to it"}}}
		var ss []string
		for _, l := range drawerLines(d, git, "/x", "", 32, " ") {
			ss = append(ss, strings.TrimRight(plain(l.s), " "))
		}
		return strings.Join(ss, "\n")
	}
	branch, ahead, dirty := "main", 2, 3
	item := "   ›Go to it                  ↵"
	if got := text(nil); got != "\n"+item {
		t.Errorf("nothing known:\n%s", got)
	}
	g := &fleet.Git{Branch: &branch, Ahead: &ahead, Dirty: &dirty}
	if got := text(g); got != "\n    main ↑2 ●3\n\n"+item {
		t.Errorf("git only:\n%s", got)
	}
}

// The cursor stays on screen when the block and its drawer outgrow the pane.
func TestDrawerCursorStaysInView(t *testing.T) {
	m := drawerModel(t, "a:@7")
	m.height = 16
	m = press(m, "right")
	for step := 0; step < 12; step++ {
		_, refs := renderView(m.view(), 32, 16, 0)
		want := "dr:" + m.drawer.items[m.drawer.sel].key
		found := false
		for _, r := range refs {
			found = found || r.id == want
		}
		if !found {
			t.Fatalf("step %d: the cursor row %s is off screen", step, want)
		}
		m = press(m, "down")
	}
}

// The drawer rows end in the last item, whole, when the pane is tall enough.
func TestDrawerScrollsIntoView(t *testing.T) {
	m := drawerModel(t, "a:@7")
	m.height = 30
	m = press(m, "right")
	out, refs := renderView(m.view(), 32, 30, 0)
	lines := strings.Split(plain(out), "\n")
	var drawer []string
	for i, r := range refs {
		if r.kind == "drawer" {
			drawer = append(drawer, lines[i])
		}
	}
	if len(lines) != 30 || len(drawer) == 0 || !strings.Contains(drawer[len(drawer)-1], "Kill") {
		t.Errorf("%d lines, drawer rows %q", len(lines), drawer)
	}
	if refs[rowOf(t, m, "a:@7")].kind != "agent" {
		t.Error("the card's first row is not shown")
	}
}

// An action list that changes under an open drawer closes it.
func TestDrawerClosesWhenItemsGoStale(t *testing.T) {
	m := press(drawerModel(t, "a:@2"), "right")
	doc := m.doc
	doc.Agents = append([]fleet.Agent(nil), m.doc.Agents...)
	for i := range doc.Agents {
		if doc.Agents[i].WindowID != nil && *doc.Agents[i].WindowID == "@2" {
			dead := "dead"
			doc.Agents[i].State = &dead
		}
	}
	nm, _ := m.Update(sbLoaded{doc})
	if nm.(sbModel).drawer != nil {
		t.Error("relaunch appeared and the drawer kept its old items")
	}
	nm, _ = m.Update(sbLoaded{m.doc})
	if nm.(sbModel).drawer == nil {
		t.Error("an unchanged fleet closed the drawer")
	}
}

// A pointer into a Needs you pointer, or a prompt in progress, opens nothing.
func TestDrawerNotForPointersOrPrompts(t *testing.T) {
	m := drawerModel(t, needID("@2"))
	if got := press(m, "right"); got.drawer != nil {
		t.Errorf("→ on a Needs you pointer opened %+v", got.drawer)
	}
	m = drawerModel(t, "a:@1")
	m.mode = modeNew
	if got := click(m, rowOf(t, m, "a:@1"), tea.MouseRight); got.drawer != nil {
		t.Errorf("a right-click during a prompt opened %+v", got.drawer)
	}
}

// The drawer open on an agent, at 32 columns and at 32x30, held like every
// other frame.
func TestDrawerGolden(t *testing.T) {
	keepPalette(t)
	t.Setenv("HOME", "/home/me")
	doc := loadFixture(t, "fifteen-three-repos")
	now := int64(0)
	if tm, err := time.Parse(time.RFC3339, doc.GeneratedAt); err == nil {
		now = tm.Unix()
	}
	for _, c := range []struct {
		id   string
		name string
		h    int
	}{{"a:@1", "kelsier", 0}, {"a:@1", "kelsier", 30}, {"a:@7", "usage-charts", 0}, {"a:@7", "usage-charts", 30}} {
		m := sbModel{doc: doc, width: 32, height: c.h, visible: true, ui: uiState{sel: c.id, focused: true}}
		m = press(m, "right")
		if m.drawer == nil {
			t.Fatalf("%s: no drawer", c.name)
		}
		got, _ := renderView(m.view(), 32, c.h, now)
		gold := filepath.Join(fixtures, "fifteen-three-repos.drawer-"+c.name+".32.golden")
		if c.h > 0 {
			gold = filepath.Join(fixtures, "fifteen-three-repos.drawer-"+c.name+".32x30.golden")
		}
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

package main

import (
	"math/rand"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func hoverFrame(m sbModel) ([]string, []lineRef) {
	out, refs := renderView(m.view(), m.width, m.height, 0)
	return strings.Split(plain(out), "\n"), refs
}

func tail1(s string) string {
	r := []rune(s)
	if len(r) == 0 {
		return ""
	}
	return strings.TrimRight(string(r[1:]), " ")
}

// After any pointer move, every row keeps its entry and its text past the bar
// column, the rows the pointer left and entered aside, and the frame keeps
// its length — with an entry's menu open too.
func TestHoverNeverMovesAnyRow(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	files, _ := filepath.Glob(filepath.Join(fixtures, "*.json"))
	for _, f := range files {
		name := strings.TrimSuffix(filepath.Base(f), ".json")
		doc := loadFixture(t, name)
		variants := []uiState{{}}
		if name == "density" {
			open := map[string]bool{shellsKey: true, bgKey: true}
			for _, g := range []string{"api-gw", "kelsier", "misc", "web-dashboard"} {
				open[quietKey(g)] = true
			}
			variants = append(variants, uiState{now: 1792018800}, uiState{now: 1792018800, expanded: open})
		}
		for vi := 0; vi < len(variants)*4; vi++ {
			ui, h, menu := variants[vi/4], []int{16, 40}[vi%2], vi%4 >= 2
			rng := rand.New(rand.NewSource(int64(h)*7919 + int64(len(name)) + int64(vi)))
			m := sbModel{doc: doc, width: 32, height: h, visible: true, ui: ui}
			if menu {
				m.openMenu()
			}
			y := rng.Intn(h)
			for step := 0; step < 60; step++ {
				bl, br := hoverFrame(m)
				ny := rng.Intn(len(br))
				nm, _ := m.Update(tea.MouseMotionMsg{X: rng.Intn(32), Y: ny})
				m = nm.(sbModel)
				al, ar := hoverFrame(m)
				if len(al) != len(bl) {
					t.Fatalf("%s h=%d step %d: %d lines became %d moving %d to %d", name, h, step, len(bl), len(al), y, ny)
				}
				for i := range al {
					if ar[i].id != br[i].id || ar[i].kind != br[i].kind {
						t.Fatalf("%s h=%d step %d: moving %d to %d changed row %d from %q to %q", name, h, step, y, ny, i, br[i].id, ar[i].id)
					}
					free := i == y || i == ny || br[i].kind == "menuitem" || br[i].kind == "foot" || br[i].kind == "menu"
					if !free && tail1(al[i]) != tail1(bl[i]) {
						t.Fatalf("%s h=%d step %d: moving %d to %d changed row %d:\n was %q\n now %q", name, h, step, y, ny, i, bl[i], al[i])
					}
				}
				y = ny
			}
		}
	}
}

// Hiding the sidebar drops its hover, as losing focus does.
func TestHiddenSidebarKeepsNoHover(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	probe := probeVisible
	t.Cleanup(func() { probeVisible = probe })
	for _, msg := range []tea.Msg{sbTick{}, sbWake{}} {
		probeVisible = func(string) (bool, bool) { return true, true }
		m := sbModel{doc: loadFixture(t, "fifteen-three-repos"), width: 32, height: 40, myPane: "%1"}
		m.checkVisible()
		nm, _ := m.Update(tea.MouseMotionMsg{X: 4, Y: rowOf(t, m, "a:@2")})
		m = nm.(sbModel)
		if !m.visible || m.ui.hover == 0 {
			t.Fatalf("%T: setup: visible %v hover %d", msg, m.visible, m.ui.hover)
		}
		probeVisible = func(string) (bool, bool) { return false, false }
		nm, _ = m.Update(msg)
		m = nm.(sbModel)
		if m.visible || m.ui.hover != 0 || m.ui.hoverX != 0 {
			t.Errorf("%T: hidden but hover %d,%d", msg, m.ui.hover, m.ui.hoverX)
		}
	}
}

// The pointer is read from the frame View drew, not from a fresh render: a
// menu item under it is the one lit.
func TestMotionReadsTheLastFrame(t *testing.T) {
	m := sbModel{width: 32, height: 40, visible: true, frame: &[]lineRef{{}, {id: "mi:x", kind: "menuitem"}},
		menu: &sbMenu{items: []sbAct{{"d", "Diff"}, {"x", "Kill"}}}}
	nm, _ := m.Update(tea.MouseMotionMsg{X: 4, Y: 1})
	if got := nm.(sbModel).menu.sel; got != 1 {
		t.Errorf("menu.sel %d", got)
	}
	m.menu = nil
	m.View()
	if len(*m.frame) < 2 {
		t.Errorf("View left %d refs", len(*m.frame))
	}
}

// The entry under the pointer lights, a dim bar where the selection's would
// be; the selection and the inspector do not.
func TestHoverLightsTheRow(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	m := sbModel{doc: loadFixture(t, "fifteen-three-repos"), width: 32, height: 40, visible: true, ui: uiState{sel: "a:@1"}}
	y := rowOf(t, m, "a:@2")
	nm, _ := m.Update(tea.MouseMotionMsg{X: 4, Y: y})
	if l, _ := hoverFrame(nm.(sbModel)); !strings.HasPrefix(l[y], "▏") {
		t.Errorf("hovered row %q", l[y])
	}
	sel := rowOf(t, m, "a:@1")
	nm, _ = m.Update(tea.MouseMotionMsg{X: 4, Y: sel})
	if l, _ := hoverFrame(nm.(sbModel)); !strings.HasPrefix(l[sel], "▌") {
		t.Errorf("hovered selection %q", l[sel])
	}
}

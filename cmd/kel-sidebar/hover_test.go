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

func nameRow(refs []lineRef, id string) int {
	for i, r := range refs {
		if id != "" && r.id == id {
			return i
		}
	}
	return -1
}

func tail1(s string) string {
	r := []rune(s)
	if len(r) == 0 {
		return ""
	}
	return strings.TrimRight(string(r[1:]), " ")
}

// After any pointer move, every row keeps its entry and its text (past the
// bar column) except the name rows and pointer rows of the entries the pointer
// left and entered, and the frame keeps its length.
func TestHoverNeverMovesAnyRow(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	files, _ := filepath.Glob(filepath.Join(fixtures, "*.json"))
	for _, f := range files {
		name := strings.TrimSuffix(filepath.Base(f), ".json")
		doc := loadFixture(t, name)
		variants := []uiState{{}}
		if name == "density" {
			open := map[string]bool{reposKey: true}
			for _, g := range []string{"api-gw", "kelsier", "misc", "web-dashboard"} {
				open[quietKey(g)] = true
			}
			variants = append(variants, uiState{now: 1792018800}, uiState{now: 1792018800, expanded: open})
		}
		for vi, h := range []int{16, 40, 16, 40, 16, 40}[:2*len(variants)] {
			ui := variants[vi/2]
			rng := rand.New(rand.NewSource(int64(h)*7919 + int64(len(name)) + int64(vi)))
			m := sbModel{doc: doc, width: 32, height: h, visible: true, ui: ui}
			y := rng.Intn(h)
			for step := 0; step < 60; step++ {
				bl, br := hoverFrame(m)
				oldID := m.ui.hoverID
				ny := rng.Intn(len(br))
				nm, _ := m.Update(tea.MouseMotionMsg{X: rng.Intn(32), Y: ny})
				m = nm.(sbModel)
				al, ar := hoverFrame(m)
				if len(al) != len(bl) {
					t.Fatalf("%s h=%d step %d: %d lines became %d moving %d to %d", name, h, step, len(bl), len(al), y, ny)
				}
				free := map[int]bool{y: true, ny: true, nameRow(br, oldID): true, nameRow(ar, m.ui.hoverID): true}
				for i := range al {
					if ar[i].id != br[i].id || ar[i].kind != br[i].kind {
						t.Fatalf("%s h=%d step %d: moving %d to %d changed row %d from %q to %q", name, h, step, y, ny, i, br[i].id, ar[i].id)
					}
					if !free[i] && tail1(al[i]) != tail1(bl[i]) {
						t.Fatalf("%s h=%d step %d: moving %d to %d changed row %d:\n was %q\n now %q", name, h, step, y, ny, i, bl[i], al[i])
					}
				}
				y = ny
			}
		}
	}
}

// A hovered agent's row trades its age for Diff, Files and ⋯, and its name
// gives way to them.
func TestHoverButtonsReplaceDetail(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	for _, id := range []string{"a:@2", "a:@4"} {
		m := sbModel{doc: loadFixture(t, "fifteen-three-repos"), width: 28, height: 40, visible: true}
		y := rowOf(t, m, id)
		rest, _ := hoverFrame(m)
		nm, _ := m.Update(tea.MouseMotionMsg{X: 4, Y: y})
		m = nm.(sbModel)
		hov, _ := hoverFrame(m)
		row := hov[y]
		if !strings.HasSuffix(strings.TrimRight(row, " "), "▐Diff▌▐Files▌▐⋯▌") {
			t.Errorf("%s: buttons are not the row's right side: %q", id, row)
		}
		f := strings.Fields(rest[y])
		if detail := f[len(f)-1]; strings.Contains(row, detail) {
			t.Errorf("%s: the detail %q is still on the hovered row %q", id, detail, row)
		}
		if len([]rune(f[1])) > 6 && !strings.Contains(row, "…") {
			t.Errorf("%s: name %q was not truncated: %q", id, f[1], row)
		}
	}
}

// The selected entry's block carries its buttons with no pointer anywhere.
func TestSelectedBlockCarriesButtons(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	m := sbModel{doc: loadFixture(t, "fifteen-three-repos"), width: 32, height: 40, visible: true, ui: uiState{focused: true, sel: "a:@2"}}
	lines, refs := hoverFrame(m)
	foot := -1
	for i, r := range refs {
		if r.id == "a:@2" && len(r.hits) == 3 {
			foot = i
		}
	}
	if foot < 0 || !strings.Contains(lines[foot], "▐Diff▌▐Files▌▐⋯▌") || strings.TrimSpace(lines[foot-1]) == "" {
		t.Fatalf("no strip in the block's foot (row %d):\n%s", foot, strings.Join(lines, "\n"))
	}
	if m.ui.hover != 0 {
		t.Error("the test hovered")
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
		if !m.visible || m.ui.hoverID == "" {
			t.Fatalf("%T: setup: visible %v hoverID %q", msg, m.visible, m.ui.hoverID)
		}
		probeVisible = func(string) (bool, bool) { return false, false }
		nm, _ = m.Update(msg)
		m = nm.(sbModel)
		if m.visible || m.ui.hover != 0 || m.ui.hoverX != 0 || m.ui.hoverID != "" {
			t.Errorf("%T: hidden but hover %d,%d %q", msg, m.ui.hover, m.ui.hoverX, m.ui.hoverID)
		}
	}
}

// The pointer is read from the frame View drew, not from a fresh render.
func TestMotionReadsTheLastFrame(t *testing.T) {
	m := sbModel{width: 32, height: 40, visible: true, frame: &[]lineRef{{id: "a:@9", kind: "agent"}}}
	nm, _ := m.Update(tea.MouseMotionMsg{X: 4, Y: 0})
	if got := nm.(sbModel).ui.hoverID; got != "a:@9" {
		t.Errorf("hoverID %q", got)
	}
	m.View()
	if len(*m.frame) < 2 {
		t.Errorf("View left %d refs", len(*m.frame))
	}
}

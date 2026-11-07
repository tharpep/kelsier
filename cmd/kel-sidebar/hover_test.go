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
		for vi := 0; vi < len(variants)*4; vi++ {
			ui, h, drawer := variants[vi/4], []int{16, 40}[vi%2], vi%4 >= 2
			rng := rand.New(rand.NewSource(int64(h)*7919 + int64(len(name)) + int64(vi)))
			m := sbModel{doc: doc, width: 32, height: h, visible: true, ui: ui}
			if drawer {
				m.openDrawer()
			}
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
					if br[i].kind == "drawer" {
						free[i] = true
					}
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

// The blank row after an agent card is the card's: hovering it lights the
// card and clicking it selects the agent.
func TestSpacerBelongsToCard(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	m := sbModel{doc: loadFixture(t, "fifteen-three-repos"), width: 32, height: 60, visible: true}
	lines, refs := hoverFrame(m)
	id, name, sp := "", -1, -1
	for i, r := range refs {
		if r.kind == "agent" && strings.TrimSpace(lines[i]) == "" {
			id, sp = r.id, i
			name = nameRow(refs, id)
			break
		}
	}
	if sp < 0 {
		t.Fatal("no agent spacer row in the frame")
	}
	nm, _ := m.Update(tea.MouseMotionMsg{X: 4, Y: sp})
	hm := nm.(sbModel)
	if hm.ui.hoverID != id {
		t.Fatalf("hoverID %q, want %q", hm.ui.hoverID, id)
	}
	hov, _ := hoverFrame(hm)
	if !strings.HasSuffix(strings.TrimRight(hov[name], " "), "▐Diff▌▐Files▌▐⋯▌") {
		t.Errorf("name row not showing buttons: %q", hov[name])
	}
	out, _ := renderView(hm.view(), hm.width, hm.height, 0)
	if row := strings.Split(out, "\n")[sp]; !strings.Contains(row, "▏") {
		t.Errorf("spacer not painted: %q", row)
	}
	nm, _ = m.Update(tea.MouseClickMsg{X: 4, Y: sp, Button: tea.MouseLeft})
	if got := nm.(sbModel).ui.sel; got != id {
		t.Errorf("click on the spacer selected %q, want %q", got, id)
	}
}

// Every blank row right under an agent row is that card's.
func TestLastCardSpacerIsOwned(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	m := sbModel{doc: loadFixture(t, "fifteen-three-repos"), width: 32, height: 60, visible: true}
	lines, refs := hoverFrame(m)
	closing := false
	for i := 1; i < len(refs); i++ {
		if strings.TrimSpace(lines[i]) != "" || refs[i-1].kind != "agent" {
			continue
		}
		if !refs[i].pad || refs[i].id != refs[i-1].id {
			t.Fatalf("row %d is a blank under an agent row but not its card's: %+v", i, refs[i])
		}
		if i+1 < len(refs) && refs[i+1].kind == "group" {
			closing = true
			nm, _ := m.Update(tea.MouseClickMsg{X: 4, Y: i, Button: tea.MouseLeft})
			if got := nm.(sbModel).ui.sel; got != refs[i].id {
				t.Errorf("click on the group-closing spacer selected %q, want %q", got, refs[i].id)
			}
		}
	}
	if !closing {
		t.Fatal("no group-closing spacer owned by a card")
	}
}

// The last card before the footer owns its spacer too.
func TestLastCardSpacerIsOwnedAtTheEnd(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	for _, name := range []string{"fifteen-three-repos", "done-seen", "dead-agent"} {
		m := sbModel{doc: loadFixture(t, name), width: 32, height: 80, visible: true}
		lines, refs := hoverFrame(m)
		last := -1
		for i, r := range refs {
			if r.kind == "agent" {
				last = i
			}
		}
		if last < 0 || !refs[last].pad || strings.TrimSpace(lines[last]) != "" {
			t.Errorf("%s: the last agent row %d is not a spacer: %+v", name, last, refs[last])
		}
	}
}

// The selected card's spacer is raised and carries the bar.
func TestSelectedSpacerIsRaised(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	m := sbModel{doc: loadFixture(t, "fifteen-three-repos"), width: 32, height: 60, visible: true, ui: uiState{sel: "a:@1"}}
	out, refs := renderView(m.view(), m.width, m.height, 0)
	rows := strings.Split(out, "\n")
	found := false
	for i, r := range refs {
		if r.pad && r.id == "a:@1" {
			found = true
			if !strings.Contains(rows[i], raised) || !strings.Contains(rows[i], "▌") {
				t.Errorf("spacer not raised: %q", rows[i])
			}
		}
	}
	if !found {
		t.Fatal("a:@1 has no spacer")
	}
}

// A card with background rows under it keeps its spacer below them, owned and flat.
func TestSpacerBelowBackgroundRows(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	m := sbModel{doc: loadFixture(t, "fifteen-three-repos"), width: 32, height: 60, visible: true}
	_, refs := hoverFrame(m)
	bg := -1
	for i, r := range refs {
		if r.kind == "bg" && i > 0 && refs[i-1].kind != "bg" {
			bg = i
			break
		}
	}
	if bg < 0 {
		t.Fatal("no bg row under a card")
	}
	card := ""
	for i := bg - 1; i >= 0; i-- {
		if refs[i].kind == "agent" {
			card = refs[i].id
			break
		}
	}
	m.ui.sel = card
	out, refs := renderView(m.view(), m.width, m.height, 0)
	rows := strings.Split(out, "\n")
	sp := 0
	for refs[sp].kind != "bg" {
		sp++
	}
	for refs[sp].kind == "bg" {
		sp++
	}
	if !refs[sp].pad || refs[sp].id != card {
		t.Fatalf("row %d under the bg rows is %+v, want %s's spacer", sp, refs[sp], card)
	}
	if rows[sp] != "" {
		t.Errorf("spacer under bg rows is raised: %q", rows[sp])
	}
}

// The blank after a quiet trailer belongs to nobody.
func TestBlankAfterTrailerIsUnowned(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	m := sbModel{doc: loadFixture(t, "density"), width: 32, height: 80, visible: true, ui: uiState{now: 1792018800}}
	lines, refs := hoverFrame(m)
	seen := false
	for i := 0; i+1 < len(refs); i++ {
		if refs[i].kind == "fold" && strings.TrimSpace(lines[i+1]) == "" {
			seen = true
			if refs[i+1].id != "" || refs[i+1].pad {
				t.Errorf("blank after the trailer at %d is owned: %+v", i, refs[i+1])
			}
		}
	}
	if !seen {
		t.Fatal("no trailer followed by a blank")
	}
}

// Hovering the selected card's spacer paints nothing.
func TestHoverSelectedSpacerDoesNotRepaint(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	m := sbModel{doc: loadFixture(t, "fifteen-three-repos"), width: 32, height: 60, visible: true, ui: uiState{sel: "a:@1"}}
	before, refs := renderView(m.view(), m.width, m.height, 0)
	sp := -1
	for i, r := range refs {
		if r.pad && r.id == "a:@1" {
			sp = i
		}
	}
	nm, _ := m.Update(tea.MouseMotionMsg{X: 4, Y: sp})
	after, _ := renderView(nm.(sbModel).view(), m.width, m.height, 0)
	if sp < 0 || before != after {
		t.Errorf("hover on the selected spacer (row %d) repainted the frame", sp)
	}
}

// At 32x30 the open drawer shows every item, Kill last.
func TestDrawerFitsBesideItsSpacer(t *testing.T) {
	m := drawerModel(t, "a:@1")
	m.height = 30
	m = press(m, "right")
	if m.drawer == nil {
		t.Fatal("no drawer")
	}
	lines, refs := hoverFrame(m)
	n, last := 0, ""
	for i, r := range refs {
		if r.kind == "drawer" && strings.HasPrefix(r.id, "dr:") {
			n++
			last = lines[i]
		}
	}
	if n != len(m.drawer.items) || !strings.Contains(last, "Kill") {
		t.Errorf("%d of %d drawer rows shown, last %q", n, len(m.drawer.items), last)
	}
}

// A click on the selected card's spacer with its drawer open only closes the drawer.
func TestSpacerClickClosesDrawerOnly(t *testing.T) {
	m := drawerModel(t, "a:@1")
	m = press(m, "right")
	_, refs := hoverFrame(m)
	sp := -1
	for i, r := range refs {
		if r.pad && r.id == "a:@1" {
			sp = i
		}
	}
	if sp < 0 || m.drawer == nil {
		t.Fatalf("setup: spacer %d, drawer %v", sp, m.drawer)
	}
	got := click(m, sp, tea.MouseLeft)
	if got.drawer != nil || got.pending != nil || got.ui.sel != "a:@1" {
		t.Errorf("drawer %v, pending %v, sel %q", got.drawer, got.pending != nil, got.ui.sel)
	}
}

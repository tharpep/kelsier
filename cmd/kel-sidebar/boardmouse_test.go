package main

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// at is where the frame now drawn has a button pressing key: its row and a
// column inside it.
func at(t *testing.T, m sbModel, key string) (x, y int) {
	t.Helper()
	for y, r := range m.clickRefs() {
		for _, h := range r.hits {
			if h.key == key {
				return h.x0, y
			}
		}
		if r.id == key {
			return 2, y
		}
	}
	t.Fatalf("no %q in the frame:\n%s", key, plain(m.render()))
	return 0, 0
}

func boardClick(t *testing.T, m sbModel, key string, b tea.MouseButton) (sbModel, tea.Cmd) {
	t.Helper()
	x, y := at(t, m, key)
	nm, c := m.Update(tea.MouseClickMsg{Button: b, X: x, Y: y})
	return nm.(sbModel), c
}

func recordKel(t *testing.T) *[]string {
	t.Helper()
	var got []string
	old := runKel
	runKel = func(dir string, env []string, a ...string) tea.Cmd {
		got = append(got, strings.Join(a, " "))
		return nil
	}
	t.Cleanup(func() { runKel = old })
	return &got
}

// The header's esc closes the board, as the key does.
func TestBoardMouseHeaderEscCloses(t *testing.T) {
	m := boardModel(t)
	if _, c := boardClick(t, m, "esc", tea.MouseLeft); !quits(c) {
		t.Error("a click on the header's esc left the board open")
	}
}

// Each face named on the last line is a click that turns every tile to it.
func TestBoardMouseFaceNamesChooseTheFace(t *testing.T) {
	m := boardModel(t)
	for _, f := range []face{faceTimeline, faceScreen, faceCard} {
		m, _ = boardClick(t, m, "face:"+string(rune('0'+f)), tea.MouseLeft)
		if m.ui.face != f {
			t.Fatalf("a click on %s showed %s", faceNames[f], faceNames[m.ui.face])
		}
	}
}

// Every chip on the last line does its key: v turns the face, a marks the
// selected agent seen.
func TestBoardMouseChipsPressTheirKeys(t *testing.T) {
	m := boardModel(t)
	m.ui.sel = "a:@3"
	kel := recordKel(t)
	m, _ = boardClick(t, m, "v", tea.MouseLeft)
	if m.ui.face != faceScreen {
		t.Errorf("the v chip left the face at %s", faceNames[m.ui.face])
	}
	boardClick(t, m, "a", tea.MouseLeft)
	if strings.Join(*kel, ";") != "_seen @3" {
		t.Errorf("the a chip asked kel for %q", *kel)
	}
}

// x asks, and the question's y is a click: kill is still asked (invariant 1),
// and answerable without the keyboard. A click on n, or on a tile, asks no
// more.
func TestBoardMouseKillAsksAndTheAnswerIsAClick(t *testing.T) {
	m := boardModel(t)
	m.ui.sel = "a:@3"
	kel := recordKel(t)
	m, _ = boardClick(t, m, "x", tea.MouseLeft)
	if m.mode != modeKill || len(*kel) != 0 {
		t.Fatalf("the x chip: mode %d, kel %q", m.mode, *kel)
	}
	if nm, _ := boardClick(t, m, "hint:n no", tea.MouseLeft); nm.mode != modeNormal || len(*kel) != 0 {
		t.Errorf("a click on n: mode %d, kel %q", nm.mode, *kel)
	}
	if nm, _ := boardClick(t, m, "sel:a:@2", tea.MouseLeft); nm.mode != modeNormal || nm.view().sel != "a:@2" || len(*kel) != 0 {
		t.Errorf("a click on a tile mid-question: mode %d, sel %s, kel %q", nm.mode, nm.view().sel, *kel)
	}
	m, _ = boardClick(t, m, "hint:y yes", tea.MouseLeft)
	if m.mode != modeNormal || len(*kel) != 1 || !strings.HasPrefix((*kel)[0], "kill ") {
		t.Errorf("a click on y: mode %d, kel %q", m.mode, *kel)
	}
}

// The wheel moves a whole row of tiles, as j and k do.
func TestBoardMouseWheelMovesByRows(t *testing.T) {
	m := boardModel(t)
	m.ui.sel = "a:@1"
	want, _ := boardPress(m, "j")
	nm, _ := m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown, X: 10, Y: 10})
	m = nm.(sbModel)
	if m.view().sel != want.view().sel || m.view().sel == "a:@1" {
		t.Fatalf("wheel down landed on %s, j on %s", m.view().sel, want.view().sel)
	}
	nm, _ = m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelUp, X: 10, Y: 10})
	if got := nm.(sbModel).view().sel; got != "a:@1" {
		t.Errorf("wheel up landed on %s", got)
	}
}

// Right-click on a tile selects it and draws its menu inside the board, over
// the footer, without the verbs that rearrange the window beneath; an item
// clicked is its key, and a click beside the box closes it.
func TestBoardMouseRightClickOpensTheTilesMenu(t *testing.T) {
	m := boardModel(t)
	m.ui.sel = "a:@1"
	m.frame = new([]lineRef)
	m, _ = boardClick(t, m, "sel:a:@3", tea.MouseRight)
	if m.menu == nil || m.view().sel != "a:@3" {
		t.Fatalf("right-click: menu %v, sel %s", m.menu, m.view().sel)
	}
	var keys []string
	for _, it := range m.menu.items {
		keys = append(keys, it.key)
	}
	if got := strings.Join(keys, " "); got != "enter space a d f e p h i x" {
		t.Errorf("the board's menu is %q", got)
	}
	out, refs := m.draw()
	last := plain(strings.Split(out, "\n")[len(refs)-1])
	if !strings.HasPrefix(last, " └") || !strings.Contains(plain(out), "Mark seen") {
		t.Errorf("the menu is not drawn over the footer:\n%s", plain(out))
	}
	x, y := at(t, m, "mi:a")
	if nm, _ := m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: boardMenuW(m.width) + 5, Y: y}); nm.(sbModel).menu != nil {
		t.Error("a click beside the menu left it open")
	}
	kel := recordKel(t)
	nm, _ := m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: x, Y: y})
	if nm.(sbModel).menu != nil || strings.Join(*kel, ";") != "_seen @3" {
		t.Errorf("Mark seen clicked: menu %v, kel %q", nm.(sbModel).menu, *kel)
	}
	nm, c := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if nm.(sbModel).menu != nil || quits(c) {
		t.Error("esc must close the menu and leave the board open")
	}
}

// History in the board's menu shows the history: h alone moves left there.
func TestBoardMouseMenuHistoryIsNotLeft(t *testing.T) {
	m := boardModel(t)
	m.ui.sel = "a:@3"
	m, _ = boardClick(t, m, "sel:a:@3", tea.MouseRight)
	kel := recordKel(t)
	_, y := at(t, m, "mi:h")
	nm, _ := m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 3, Y: y})
	if nm.(sbModel).view().sel != "a:@3" || len(*kel) != 1 || !strings.Contains((*kel)[0], "history") {
		t.Errorf("History clicked: sel %s, kel %q", nm.(sbModel).view().sel, *kel)
	}
}

// A worktree row: a click selects it, its chip s asks as the key does, and
// its right-click menu has what a worktree can do on the board.
func TestBoardMouseWorktreeRow(t *testing.T) {
	m := needsModel(t, "fifteen-three-repos")
	m.doc, _ = loadBoardFixture(t, "worktrees")
	m.board, m.width, m.height = true, 150, 40
	m.ui.sel = "a:@1"
	const wt = "w:/home/me/code/kelsier-fix-auth"
	m, _ = boardClick(t, m, "sel:"+wt, tea.MouseLeft)
	if m.view().sel != wt {
		t.Fatalf("a click on the worktree row selected %s", m.view().sel)
	}
	if nm, _ := boardClick(t, m, "s", tea.MouseLeft); nm.mode != modeSweep {
		t.Errorf("the s chip on a worktree: mode %d", nm.mode)
	}
	m, _ = boardClick(t, m, "sel:"+wt, tea.MouseRight)
	var keys []string
	for _, it := range m.menu.items {
		keys = append(keys, it.key)
	}
	if got := strings.Join(keys, " "); got != "g t s" {
		t.Errorf("a worktree's board menu is %q", got)
	}
}

// The tile under the pointer takes the sidebar's hover fill; the selected
// one keeps the raised surface.
func TestBoardMouseHoverFillsTheTile(t *testing.T) {
	m := boardModel(t)
	m.ui.sel = "a:@1"
	m.frame = new([]lineRef)
	m.View()
	x, y := at(t, m, "sel:a:@3")
	nm, _ := m.Update(tea.MouseMotionMsg{X: x + 3, Y: y + 2})
	m = nm.(sbModel)
	if got := m.boardOpts().hover; got != "a:@3" {
		t.Fatalf("hover over a:@3 reads %q", got)
	}
	hov := lifted.mix(baseBg, 0.25).bg()
	if out, _ := m.draw(); !strings.Contains(out, hov) {
		t.Error("the hovered tile has no hover fill")
	}
	nm, _ = m.Update(tea.MouseMotionMsg{X: x + 3, Y: 0})
	if out, _ := nm.(sbModel).draw(); strings.Contains(out, hov) {
		t.Error("the hover fill stayed after the pointer left")
	}
}

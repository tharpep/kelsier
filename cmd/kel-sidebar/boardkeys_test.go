package main

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func boardModel(t *testing.T) sbModel {
	t.Helper()
	m := needsModel(t, "fifteen-three-repos")
	m.doc, _ = loadBoardFixture(t, "fifteen-three-repos")
	m.board, m.width, m.height = true, 150, 40
	return m
}

func boardPress(m sbModel, k string) (sbModel, tea.Cmd) {
	var msg tea.KeyPressMsg
	switch k {
	case "esc":
		msg = tea.KeyPressMsg{Code: tea.KeyEscape}
	case "enter":
		msg = tea.KeyPressMsg{Code: tea.KeyEnter}
	default:
		msg = tea.KeyPressMsg{Code: rune(k[0]), Text: k}
	}
	nm, c := m.Update(msg)
	return nm.(sbModel), c
}

func quits(c tea.Cmd) bool {
	if c == nil {
		return false
	}
	_, ok := c().(tea.QuitMsg)
	return ok
}

// h and l walk the wall in reading order; j and k keep the column, landing
// on a shorter row's last tile.
func TestBoardKeysMoveOnTheGrid(t *testing.T) {
	m := boardModel(t)
	m.ui.sel = "a:@1"
	for _, step := range []struct{ key, want string }{
		{"l", "a:@2"}, {"j", "a:@7"}, {"j", "b:bg2"}, {"k", "a:@6"}, {"h", "a:@3"},
		{"k", "a:@11"}, {"k", "a:@11"},
	} {
		m, _ = boardPress(m, step.key)
		if got := m.view().sel; got != step.want {
			t.Fatalf("%s: on %s, want %s", step.key, got, step.want)
		}
	}
	if got := sbLoadShared().Selected; got != "a:@11" {
		t.Errorf("sidebar.json holds %q: the board's selection is the sidebar's", got)
	}
}

func TestBoardEnterJumpsAndCloses(t *testing.T) {
	m := boardModel(t)
	m.ui.sel = "a:@2"
	got := recordJumps(t)
	_, c := boardPress(m, "enter")
	if len(*got) != 1 || !strings.HasPrefix((*got)[0], "@2|") || !quits(c) {
		t.Errorf("enter jumped %v and quit=%v", *got, quits(c))
	}
}

func TestBoardClickSelectsThenJumps(t *testing.T) {
	m := boardModel(t)
	m.ui.sel = "a:@1"
	got := recordJumps(t)
	click := func(id string) tea.Cmd {
		for y, r := range m.clickRefs() {
			for _, h := range r.hits {
				if h.key == "sel:"+id {
					nm, c := m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: h.x0 + 2, Y: y})
					m = nm.(sbModel)
					return c
				}
			}
		}
		t.Fatalf("no tile %s on the board", id)
		return nil
	}
	click("a:@2")
	if m.view().sel != "a:@2" || len(*got) != 0 {
		t.Fatalf("first click: sel %s, jumps %v", m.view().sel, *got)
	}
	if c := click("a:@2"); len(*got) != 1 || !quits(c) {
		t.Errorf("a click on the selected tile jumped %v, quit=%v", *got, quits(c))
	}
}

// v turns every tile to the next face, and the face is kept in sidebar.json
// for the next board.
func TestBoardFaceCyclesAndIsKept(t *testing.T) {
	m := boardModel(t)
	for _, want := range []face{faceScreen, faceTimeline, faceCard, faceScreen} {
		m, _ = boardPress(m, "v")
		if m.ui.face != want {
			t.Fatalf("face %s, want %s", faceNames[m.ui.face], faceNames[want])
		}
	}
	if next := (sbModel{board: true}).withShared(); next.face != faceScreen {
		t.Errorf("a new board opens on %s", faceNames[next.face])
	}
	if out := plain(m.render()); !strings.Contains(out, "Bash(npm test -- --watch=false)") {
		t.Errorf("the screen face does not show the pane:\n%s", out)
	}
}

func TestBoardSeenAsksKel(t *testing.T) {
	m := boardModel(t)
	m.ui.sel = "a:@3"
	var args []string
	old := runKel
	runKel = func(dir string, env []string, a ...string) tea.Cmd { args = a; return nil }
	t.Cleanup(func() { runKel = old })
	boardPress(m, "a")
	if strings.Join(args, " ") != "_seen @3" {
		t.Errorf("a asked kel for %q", args)
	}
}

// The board answers its own keys and the four it shares; the sidebar's other
// verbs (new, quit kel, zoom…) do nothing on it, and esc closes it.
func TestBoardIgnoresTheSidebarsOtherKeys(t *testing.T) {
	m := boardModel(t)
	m.ui.sel = "a:@2"
	for _, k := range []string{"n", "w", "z", "s", "?", "r"} {
		nm, c := boardPress(m, k)
		if nm.mode != modeNormal || nm.help || c != nil {
			t.Errorf("%s did something on the board", k)
		}
	}
	if nm, c := boardPress(m, "q"); !quits(c) || nm.mode == modeQuit {
		t.Error("q on the board must close it, never ask to quit kel")
	}
	if _, c := boardPress(m, "esc"); !quits(c) {
		t.Error("esc does not close the board")
	}
	if nm, _ := boardPress(m, "x"); nm.mode != modeKill || nm.target != "a:@2" {
		t.Errorf("x asks no kill question: mode %d target %s", nm.mode, nm.target)
	}
	if nm, _ := boardPress(m, "x"); !strings.Contains(plain(nm.render()), "kill fix-auth?") {
		t.Errorf("the kill question is not on the board:\n%s", plain(nm.render()))
	}
}

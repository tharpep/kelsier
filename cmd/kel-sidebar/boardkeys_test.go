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
	if nm, _ := boardPress(m, "x"); !strings.Contains(plain(nm.render()), "kill fix-auth") {
		t.Errorf("the kill question is not on the board:\n%s", plain(nm.render()))
	}
}

// A filter that hides the selected tile moves the selection to the first
// tile it shows: enter must never jump to an agent the board is not drawing.
func TestBoardFilterNeverActsOnAHiddenTile(t *testing.T) {
	m := boardModel(t)
	m.ui.sel = "a:@2"
	m.ui.filter = "dash"
	if got := m.view().sel; got != "a:@6" {
		t.Fatalf("filtered to dash, the selection is %s", got)
	}
	got := recordJumps(t)
	boardPress(m, "enter")
	if len(*got) != 1 || !strings.HasPrefix((*got)[0], "@6|") {
		t.Errorf("enter jumped %v, want the dashboard shown", *got)
	}
}

// A background tile is a tile, whatever the sidebar's background fold:
// selectable from the grid and by a click, and enter opens it (claude
// attach), the board staying open beneath.
func TestBoardBackgroundTilesActFoldedOrNot(t *testing.T) {
	for _, open := range []bool{false, true} {
		m := boardModel(t)
		m.ui.expanded = map[string]bool{bgKey: open}
		m.ui.sel = "a:@7"
		m, _ = boardPress(m, "j")
		if got := m.view().sel; got != "b:bg2" {
			t.Fatalf("fold open=%v: j from usage-charts landed on %s", open, got)
		}
		var args []string
		old := runKel
		runKel = func(dir string, env []string, a ...string) tea.Cmd {
			args = a
			return func() tea.Msg { return nil }
		}
		jumps := recordJumps(t)
		nm, c := boardPress(m, "enter")
		runKel = old
		if strings.Join(args, " ") != "_panel attach /home/me/code/api-gw nightly bg2" || quits(c) || len(*jumps) != 0 {
			t.Errorf("fold open=%v: enter asked kel %q, quit=%v, jumps %v", open, args, quits(c), *jumps)
		}
		if nm.ui.expanded[bgKey] != open {
			t.Errorf("fold open=%v: enter on a tile flipped the list's fold", open)
		}
	}
}

// Worktrees are rows below the tiles: j walks into them, s on one asks and
// then sweeps that one path through kel sweep, and s anywhere else sweeps
// nothing. Enter on a worktree jumps nowhere.
func TestBoardSweepsOneWorktree(t *testing.T) {
	m := needsModel(t, "fifteen-three-repos")
	m.doc, _ = loadBoardFixture(t, "worktrees")
	m.board, m.width, m.height = true, 150, 40
	m.ui.sel = "a:@1"
	var args []string
	old := runKel
	runKel = func(dir string, env []string, a ...string) tea.Cmd { args = a; return nil }
	t.Cleanup(func() { runKel = old })
	if nm, _ := boardPress(m, "s"); nm.mode != modeNormal || args != nil {
		t.Fatalf("s on an agent: mode %d, kel %q", nm.mode, args)
	}
	m, _ = boardPress(m, "j")
	m, _ = boardPress(m, "j")
	if got := m.view().sel; got != "w:/home/me/code/kelsier-fix-auth" {
		t.Fatalf("j j from the tile landed on %s", got)
	}
	jumps := recordJumps(t)
	if nm, c := boardPress(m, "enter"); len(*jumps) != 0 || quits(c) || nm.mode != modeNormal {
		t.Errorf("enter on a worktree jumped %v, quit=%v", *jumps, quits(c))
	}
	m, _ = boardPress(m, "s")
	if out := plain(m.render()); m.mode != modeSweep || !strings.Contains(out, "sweep fix-auth?") {
		t.Fatalf("s on a worktree asks nothing:\n%s", out)
	}
	m, _ = boardPress(m, "y")
	if strings.Join(args, " ") != "sweep /home/me/code/kelsier-fix-auth" {
		t.Errorf("y asked kel for %q", args)
	}
	if out := plain(m.render()); strings.Contains(out, "need you") || !strings.Contains(out, "1 working") {
		t.Errorf("worktrees counted as agents in the header:\n%s", out)
	}
}

// A click on a board heading folds nothing: the list's folds are not the
// board's, and a click there once shut the repo in the sidebar beneath.
func TestBoardHeadingClickFoldsNothing(t *testing.T) {
	m := boardModel(t)
	for y, r := range m.clickRefs() {
		if r.kind != "group" {
			continue
		}
		nm, _ := m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 3, Y: y})
		if got := nm.(sbModel).ui.expanded; len(got) != 0 {
			t.Fatalf("a click on heading line %d set folds %v", y, got)
		}
	}
}

// A worktree row added or gone never moves a tile: the rows sit below the
// whole wall.
func TestBoardWorktreesNeverMoveTiles(t *testing.T) {
	with, now := loadBoardFixture(t, "worktrees")
	without := with
	without.Repos = nil
	for _, sz := range boardSizes {
		a, ra := renderBoard(buildView(without, uiState{now: now}), sz[0], sz[1], now, boardOpts{})
		b, rb := renderBoard(buildView(with, uiState{now: now}), sz[0], sz[1], now, boardOpts{})
		pa, pb := tilePlaces(t, a, ra), tilePlaces(t, b, rb)
		for id, p := range pa {
			if pb[id] != p {
				t.Errorf("%dx%d: worktrees moved %s from %v to %v", sz[0], sz[1], id, p, pb[id])
			}
		}
	}
}

// The filter, once kept, says so on the board's last line, as the sidebar's
// does: else a board of three tiles reads as a fleet of three.
func TestBoardSaysItsFilter(t *testing.T) {
	m := boardModel(t)
	m.ui.filter = "dash"
	if out := plain(m.render()); !strings.Contains(out, "/dash  esc clears") {
		t.Errorf("the board hides its filter:\n%s", out)
	}
}

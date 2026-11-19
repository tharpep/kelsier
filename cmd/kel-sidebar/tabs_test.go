package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// The Trees view at every width the sidebar is meant for, colours included.
func TestTreesGolden(t *testing.T) {
	t.Setenv("HOME", "/home/me")
	doc, now, err := readFixture(filepath.Join(fixtures, "worktrees.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range []int{28, 32, 40} {
		got, _ := renderView(buildView(doc, uiState{tab: "trees", sel: "w:/home/me/code/kelsier-old-ui", focused: true, now: now}), w, 30, now)
		gold := filepath.Join(fixtures, "worktrees.trees."+itoa(w)+"x30.golden")
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
			t.Errorf("trees at %d cols changed:\n--- got\n%s\n--- want\n%s", w, plain(got), plain(string(want)))
		}
	}
}

// The strip is one fixed row: choosing a tab swaps the list's content and
// moves nothing else — the strip, the inspector and the footer keep their
// rows.
func TestTabsMoveNoRows(t *testing.T) {
	t.Setenv("HOME", "/home/me")
	doc, now, _ := readFixture(filepath.Join(fixtures, "worktrees.json"))
	rows := func(tab string) []lineRef {
		_, refs := renderView(buildView(doc, uiState{tab: tab, now: now}), 32, 30, now)
		return refs
	}
	agents, trees := rows("agents"), rows("trees")
	if len(agents) != 30 || len(trees) != 30 {
		t.Fatalf("frames are %d and %d lines, want 30", len(agents), len(trees))
	}
	for y := range agents {
		a, b := agents[y].kind, trees[y].kind
		fixed := func(k string) bool { return k == "tabs" || k == "inspector" || k == "foot" || k == "menu" }
		if (fixed(a) || fixed(b)) && a != b {
			t.Errorf("line %d is %q under Agents and %q under Trees", y, a, b)
		}
	}
	if agents[1].kind != "tabs" {
		t.Errorf("the strip is not on the row under kel's: %q", agents[1].kind)
	}
}

// At four columns the rail draws no strip.
func TestRailHasNoStrip(t *testing.T) {
	_, refs := renderView(buildView(loadFixture(t, "worktrees"), uiState{tab: "trees"}), 4, 20, 0)
	for _, r := range refs {
		if r.kind == "tabs" {
			t.Fatal("the rail drew the tab strip")
		}
	}
}

func TestTabStripLabels(t *testing.T) {
	for _, c := range []struct {
		w    int
		want string
	}{{40, "Agents  Board  PRs  Trees"}, {28, "Agents  Board  PRs  Trees"}, {27, "Agt  Brd  PR  Tree"}, {21, "Agt  Brd  PR  Tree"}, {20, ""}} {
		if got := strings.TrimSpace(plain(tabStrip("agents", "", c.w).s)); got != c.want {
			t.Errorf("at %d: %q, want %q", c.w, got, c.want)
		}
	}
}

func tabPress(m sbModel, shift bool) (sbModel, tea.Cmd) {
	msg := tea.KeyPressMsg{Code: tea.KeyTab}
	if shift {
		msg.Mod = tea.ModShift
	}
	nm, c := m.Update(msg)
	return nm.(sbModel), c
}

// Tab walks the four round; Board and PRs open their popups; Agents and
// Trees each keep their own cursor, and the choice is shared.
func TestTabCyclesAndKeepsEachListsCursor(t *testing.T) {
	m := needsModel(t, "worktrees")
	m.ui.sel = "a:@2"
	var c tea.Cmd
	for _, want := range []string{"board", "prs", "trees"} {
		if m, c = tabPress(m, false); m.ui.tab != want {
			t.Fatalf("tab went to %q, want %q", m.ui.tab, want)
		}
		if (want == "board" || want == "prs") != (c != nil) {
			t.Errorf("%s: command %v", want, c != nil)
		}
	}
	if v := m.view(); !strings.HasPrefix(v.sel, "w:") || v.tab != "trees" {
		t.Fatalf("Trees selects %q", v.sel)
	}
	nm, _ := m.press("j")
	m = nm.(sbModel)
	treeSel := m.view().sel
	if sh := sbLoadShared(); sh.View != "trees" || sh.Selected != treeSel || sh.Parked != "a:@2" {
		t.Errorf("sidebar.json holds %+v", sh)
	}
	if m, _ = tabPress(m, false); m.view().sel != "a:@2" {
		t.Errorf("back on Agents the cursor is %q, want a:@2", m.view().sel)
	}
	if m, _ = tabPress(m, true); m.view().sel != treeSel {
		t.Errorf("shift-tab back to Trees: cursor %q, want %q", m.view().sel, treeSel)
	}
}

// A click on a tab chooses it; the strip's hits cover each label.
func TestClickATab(t *testing.T) {
	m := needsModel(t, "worktrees")
	m.height = 30
	_, refs := m.draw()
	var x int
	for _, h := range refs[1].hits {
		if h.key == "tab:trees" {
			x = h.x0 + 1
		}
	}
	if x == 0 {
		t.Fatal("no Trees hit on the strip")
	}
	nm, _ := m.Update(tea.MouseClickMsg{X: x, Y: 1, Button: tea.MouseLeft})
	if got := nm.(sbModel).view().tab; got != "trees" {
		t.Errorf("clicked Trees, shows %q", got)
	}
}

// s on a worktree asks, and sweeps that one path through kel sweep; it is
// never the everything-landed sweep.
func TestTreesSweepOne(t *testing.T) {
	m := needsModel(t, "worktrees")
	m.ui.tab, m.ui.sel = "trees", "w:/home/me/code/kelsier-old-ui"
	nm, _ := m.press("s")
	m = nm.(sbModel)
	if m.mode != modeSweep || m.target != "w:/home/me/code/kelsier-old-ui" {
		t.Fatalf("s: mode %v target %q", m.mode, m.target)
	}
	if v := m.view(); !strings.Contains(v.promptTitle, "sweep old-ui?") {
		t.Errorf("asks %q", v.promptTitle)
	}
}

// On the board, Trees hands the choice to the sidebar and closes; Board
// stays put.
func TestBoardTreesTabHandsOver(t *testing.T) {
	m := boardModel(t)
	m.ui.tab, m.ui.sel = "board", "a:@1"
	nm, c := m.setTab("board")
	if c != nil || nm.(sbModel).ui.tab != "board" {
		t.Fatal("Board on the board did something")
	}
	_, c = m.setTab("trees")
	if c == nil {
		t.Fatal("Trees on the board did not close it")
	}
	if sh := sbLoadShared(); sh.View != "trees" || sh.Parked != "a:@1" {
		t.Errorf("sidebar.json holds %+v", sh)
	}
}

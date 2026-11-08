package main

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func railLines(t *testing.T, fixture string, h int) []string {
	t.Helper()
	v := buildView(loadFixture(t, fixture), uiState{})
	out, _ := renderView(v, 4, h, 1792018800)
	return strings.Split(plain(out), "\n")
}

func TestRailShowsCountDotsServerAndMenu(t *testing.T) {
	v := buildView(loadFixture(t, "fifteen-three-repos"), uiState{})
	out, refs := renderView(v, 4, 0, 1792018800)
	lines := strings.Split(plain(out), "\n")
	if lines[0] != " ●3" {
		t.Errorf("the count comes first: %q", lines[0])
	}
	if want := 1 + len(v.agents) + 1 + 1; len(lines) != want {
		t.Fatalf("%d lines, want %d (count, a dot per agent, ▪, ≡):\n%s", len(lines), want, plain(out))
	}
	for i, a := range v.agents {
		dot, _ := stateLook(a.agent.State, a.agent.Unseen, a.agent.StateSince, 1792018800)
		if lines[1+i] != " "+dot || refs[1+i].id != agentID(a.key) {
			t.Errorf("line %d: %q %q, want %q for %s", 1+i, lines[1+i], refs[1+i].id, dot, agentID(a.key))
		}
	}
	if lines[len(lines)-1] != " ≡" || refs[len(refs)-1].id != railFullID {
		t.Errorf("≡ ends the rail: %q %q", lines[len(lines)-1], refs[len(refs)-1].id)
	}
	for _, l := range lines {
		if len([]rune(l)) > 4 {
			t.Errorf("%q is wider than the rail", l)
		}
	}
}

func TestRailHasNoCountWhenNothingNeedsYou(t *testing.T) {
	lines := railLines(t, "done-seen", 0)
	if lines[0] == "" || strings.ContainsAny(lines[0], "0123456789") {
		t.Errorf("the first line is a dot, not a count: %q", lines[0])
	}
}

func TestRailMarksServers(t *testing.T) {
	if got := strings.Join(railLines(t, "servers", 0), "\n"); !strings.Contains(got, "▪") {
		t.Errorf("a server pane shows ▪:\n%s", got)
	}
	if got := strings.Join(railLines(t, "one-agent", 0), "\n"); strings.Contains(got, "▪") {
		t.Errorf("no server, no ▪:\n%s", got)
	}
}

func TestRailPinsMenuToTheFootAtAHeight(t *testing.T) {
	lines := railLines(t, "fifteen-three-repos", 12)
	if len(lines) != 12 || lines[11] != " ≡" {
		t.Errorf("≡ is the last of 12 lines: %d %q", len(lines), lines[len(lines)-1])
	}
	if lines := railLines(t, "fifteen-three-repos", 1); len(lines) != 1 || lines[0] != " ≡" {
		t.Errorf("one row is all menu: %q", lines)
	}
}

func TestRailClickOnADotJumps(t *testing.T) {
	m := needsModel(t, "fifteen-three-repos")
	m.width = 4
	got := recordJumps(t)
	v := m.view()
	_, refs := renderView(v, 4, m.height, 0)
	row := -1
	for i, r := range refs {
		if r.id == agentID(v.agents[0].key) {
			row = i
		}
	}
	if row < 0 {
		t.Fatal("no dot for the first agent")
	}
	m.update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 1, Y: row})
	want := v.agents[0].wid + "|" + v.agents[0].pane
	if len(*got) != 1 || (*got)[0] != want {
		t.Errorf("jumps %v, want [%s]", *got, want)
	}
}

func TestRailClickOnMenuAsksForFull(t *testing.T) {
	m := needsModel(t, "fifteen-three-repos")
	m.width, m.height = 4, 20
	m.ui.myWindow = "@7"
	var args []string
	old := runKel
	runKel = func(dir string, env []string, a ...string) tea.Cmd { args = a; return nil }
	t.Cleanup(func() { runKel = old })
	m.update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 1, Y: 19})
	if strings.Join(args, " ") != "_sidebar size @7 full" {
		t.Errorf("asked kel for %q", args)
	}
}

func TestRailDrawsWhateverTheSidebarWasDoing(t *testing.T) {
	m := needsModel(t, "fifteen-three-repos")
	m.width, m.settings = 4, true
	if out := plain(m.render()); !strings.Contains(out, "≡") {
		t.Errorf("settings do not fit a rail:\n%s", out)
	}
}

package main

import (
	"strings"
	"testing"

	"github.com/tharpep/kelsier/internal/fleet"
)

func nestDoc() fleet.Doc {
	st := "working"
	return fleet.Doc{
		Agents: []fleet.Agent{
			{Group: "webapp", Name: "feat-login", WindowID: sp("@1"), PaneID: sp("%1"), Managed: true, State: &st},
		},
		Panes: &[]fleet.Pane{
			{PaneID: "%1", WindowID: "@1", PaneIndex: fp(0), Cmd: "claude", Kind: "agent", Agent: true, Cwd: "/r/webapp"},
			{PaneID: "%2", WindowID: "@1", PaneIndex: fp(1), Cmd: "python3", Kind: "server", Cwd: "/r/webapp", Ports: &[]int{18765}},
			{PaneID: "%3", WindowID: "@1", PaneIndex: fp(2), Cmd: "bash", Kind: "shell", Cwd: "/r/webapp"},
			{PaneID: "%9", WindowID: "@9", PaneIndex: fp(0), Cmd: "bash", Kind: "shell", Cwd: "/home/me"},
		},
	}
}

func fp(f float64) *float64 { return &f }

func shellIDs(v sbView) []string {
	var ids []string
	for _, sh := range v.shells {
		ids = append(ids, sh.pane.PaneID)
	}
	return ids
}

func TestPaneInAgentlessWindowStaysInShells(t *testing.T) {
	v := buildView(nestDoc(), uiState{})
	if got := shellIDs(v); len(got) != 1 || got[0] != "%9" {
		t.Errorf("Shells holds %v", got)
	}
	d := nestDoc()
	d.Agents = nil
	if got := shellIDs(buildView(d, uiState{})); len(got) != 3 {
		t.Errorf("with no agent every non-agent pane is a shell: %v", got)
	}
}

func TestSharedWindowNestsUnderFirstAgent(t *testing.T) {
	d := nestDoc()
	st := "working"
	d.Agents = append(d.Agents, fleet.Agent{Group: "webapp", Name: "feat-login-2", WindowID: sp("@1"), PaneID: sp("%4"), State: &st})
	*d.Panes = append(*d.Panes, fleet.Pane{PaneID: "%4", WindowID: "@1", PaneIndex: fp(3), Cmd: "claude", Kind: "agent", Agent: true, Cwd: "/r/webapp"})
	v := buildView(d, uiState{})
	first := v.agents[0].key
	if len(v.under[first]) != 2 || len(v.under[v.agents[1].key]) != 0 {
		t.Errorf("under: %v (first agent %s)", v.under, first)
	}
}

func TestQuietFoldedAgentReturnsPanesToShells(t *testing.T) {
	d := nestDoc()
	idle := "idle"
	d.Agents[0].State = &idle
	d.Agents[0].StateSince = fp(1000)
	v := buildView(d, uiState{now: 1000 + 3*24*3600})
	if !v.agents[0].folded {
		t.Fatal("the idle 3d agent should be folded into the quiet trailer")
	}
	got := strings.Join(shellIDs(v), " ")
	if !strings.Contains(got, "%2") || !strings.Contains(got, "%3") || len(v.under) != 0 {
		t.Errorf("Shells holds %q, under %v: a folded agent's server must stay visible", got, v.under)
	}
}

func inspectorText(v sbView) string {
	var rows []string
	for _, l := range v.inspector(32, 0) {
		rows = append(rows, strings.TrimRight(plain(l.s), " "))
	}
	return strings.Join(rows, "\n")
}

// A window's other panes are in its agent's inspector, each a click, and the
// row says only that a server runs there: ▪ before the age.
func TestPaneNestsUnderItsAgent(t *testing.T) {
	v := buildView(nestDoc(), uiState{sel: agentID("%1")})
	if got := shellIDs(v); len(got) != 1 || got[0] != "%9" {
		t.Errorf("shells holds %v, want only %%9 (no agent in its window)", got)
	}
	if n := len(v.under[v.agents[0].key]); n != 2 {
		t.Errorf("%d panes under the agent, want 2", n)
	}
	out, _ := renderView(v, 32, 0, 0)
	if strings.Contains(plain(out), "└") {
		t.Errorf("a nested row is drawn:\n%s", plain(out))
	}
	for _, r := range strings.Split(plain(out), "\n") {
		if strings.Contains(r, "● feat-login") && !strings.Contains(r, "▪") {
			t.Errorf("the agent's row has no server mark: %q", r)
		}
	}
	if got := inspectorText(v); !strings.Contains(got, "1 ▪ python3 :18765 · 2 $ bash") {
		t.Errorf("the inspector does not list the window's panes:\n%s", got)
	}
	if !strings.Contains(plain(out), "▸ shells                     1") {
		t.Errorf("the shells heading does not count what it holds:\n%s", plain(out))
	}
}

// A nested pane is selected from the inspector, and has its menu.
func TestNestedPaneIsSelectableWithItsMenu(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	v := buildView(nestDoc(), uiState{sel: "%2"})
	if v.sel != "%2" || v.inner["%2"] != agentID("%1") {
		t.Fatalf("a nested pane is not selectable: sel %q inner %v", v.sel, v.inner)
	}
	what, _, acts := selActions(v)
	if what != "pane" || len(acts) == 0 {
		t.Errorf("selActions on a nested pane: %q %v", what, acts)
	}
	m := sbModel{doc: nestDoc(), width: 32, height: 40, visible: true, ui: uiState{sel: "%2"}}
	if _, ok := m.paneOf("%2"); !ok {
		t.Error("paneOf does not find a nested pane")
	}
	m = press(m, "right")
	if m.menu == nil || m.menu.entry != "%2" {
		t.Errorf("the menu did not open on a nested pane: %+v", m.menu)
	}
}

// Mode and wt are in the inspector, faint on the facts row; never on the row.
func TestWtTagOnWorktreeAgentOnly(t *testing.T) {
	d := nestDoc()
	facts := func() string {
		return strings.Split(inspectorText(buildView(d, uiState{sel: agentID("%1")})), "\n")[2]
	}
	if r := facts(); strings.HasSuffix(r, "wt") {
		t.Errorf("in-place agent tagged: %q", r)
	}
	(*d.Panes)[0].Git = &fleet.Git{Root: "/r/webapp-feat", Worktree: true}
	if r := facts(); !strings.HasSuffix(r, " wt") {
		t.Errorf("worktree agent not tagged: %q", r)
	}
	plan := "plan"
	d.Agents[0].Mode = &plan
	if r := facts(); !strings.HasSuffix(r, " plan wt") {
		t.Errorf("mode and wt do not share the row: %q", r)
	}
	out, _ := renderView(buildView(d, uiState{}), 32, 0, 0)
	for _, r := range strings.Split(plain(out), "\n") {
		if strings.Contains(r, "● feat-login") && (strings.Contains(r, "plan") || strings.Contains(r, " wt")) {
			t.Errorf("a tag on the row: %q", r)
		}
	}
}

// A digit selects that pane of the agent's inspector, from the agent or from
// a sibling pane; a number past the list, or with nothing nested, does nothing.
func TestDigitSelectsAnInspectorPane(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	m := sbModel{doc: nestDoc(), width: 32, height: 40, visible: true, ui: uiState{sel: agentID("%1")}}
	if got := inspectorText(m.view()); !strings.Contains(got, "1 ▪ python3 :18765 · 2 $ bash") {
		t.Errorf("the inspector's panes are not numbered:\n%s", got)
	}
	if m = press(m, "2"); m.view().sel != "%3" {
		t.Errorf("2 from the agent selected %q, want %%3", m.view().sel)
	}
	if m = press(m, "1"); m.view().sel != "%2" {
		t.Errorf("1 from a nested pane selected %q, want %%2", m.view().sel)
	}
	if m = press(m, "7"); m.view().sel != "%2" {
		t.Errorf("7 with two panes moved the selection to %q", m.view().sel)
	}
}

// x asks on every pane, a shell at its prompt included, and the kill question
// says whether the window goes with the agent.
func TestXAsksAndSaysWhatCloses(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	m := sbModel{doc: nestDoc(), width: 32, height: 40, visible: true, ui: uiState{sel: "%3"}}
	if m = press(m, "x"); m.mode != modeClose || !strings.Contains(plain(m.render()), "close bash?") {
		t.Errorf("x on a shell did not ask:\n%s", plain(m.render()))
	}
	m = press(m, "n")
	m.ui.sel = agentID("%1")
	if m = press(m, "x"); !strings.Contains(plain(m.render()), "kill feat-login (pane only)?") {
		t.Errorf("an agent beside other panes does not say only its pane goes:\n%s", plain(m.render()))
	}
	m = press(m, "n")
	d := nestDoc()
	*d.Panes = (*d.Panes)[:1]
	m = sbModel{doc: d, width: 32, height: 40, visible: true, ui: uiState{sel: agentID("%1")}}
	if m = press(m, "x"); !strings.Contains(plain(m.render()), "kill feat-login + window?") {
		t.Errorf("an agent alone does not say its window goes:\n%s", plain(m.render()))
	}
}

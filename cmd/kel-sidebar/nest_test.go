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

func TestPaneNestsUnderItsAgent(t *testing.T) {
	v := buildView(nestDoc(), uiState{})
	if got := shellIDs(v); len(got) != 1 || got[0] != "%9" {
		t.Errorf("Shells holds %v, want only %%9 (no agent in its window)", got)
	}
	if n := len(v.under[v.agents[0].key]); n != 2 {
		t.Errorf("%d panes under the agent, want 2", n)
	}
	out, _ := renderView(v, 32, 0, 0)
	rows := strings.Split(plain(out), "\n")
	at := func(sub string) int {
		for i, r := range rows {
			if strings.Contains(r, sub) {
				return i
			}
		}
		t.Fatalf("no row with %q:\n%s", sub, plain(out))
		return -1
	}
	card, srv, sh := at("feat-login"), at("└ ▪ python3 :18765"), at("└ $ bash")
	if !(card < srv && srv < sh) {
		t.Errorf("rows out of order: card %d server %d shell %d", card, srv, sh)
	}
	if !strings.Contains(plain(out), "Shells                      1") {
		t.Errorf("Shells header does not count what is listed there:\n%s", plain(out))
	}
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

func TestNestedPaneIsSelectableWithDrawer(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	v := buildView(nestDoc(), uiState{sel: "%2"})
	if v.indexOf("%2") < 0 || v.indexOf("%3") < 0 {
		t.Fatalf("nested panes are not selectable: %v", v.entryIDs)
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
	if m.view().drawer == nil || m.view().drawer.id != "%2" {
		t.Errorf("the drawer did not open on a nested pane: %+v", m.view().drawer)
	}
}

func TestWtTagOnWorktreeAgentOnly(t *testing.T) {
	d := nestDoc()
	row := func() string {
		out, _ := renderView(buildView(d, uiState{}), 32, 0, 0)
		for _, r := range strings.Split(plain(out), "\n") {
			if strings.Contains(r, "feat-login") {
				return r
			}
		}
		return ""
	}
	if r := row(); strings.Contains(r, "wt") {
		t.Errorf("in-place agent tagged: %q", r)
	}
	(*d.Panes)[0].Git = &fleet.Git{Root: "/r/webapp-feat", Worktree: true}
	if r := row(); !strings.Contains(r, "feat-login wt") {
		t.Errorf("worktree agent not tagged: %q", r)
	}
	plan := "plan"
	d.Agents[0].Mode = &plan
	if r := row(); !strings.Contains(r, "feat-login plan wt") {
		t.Errorf("mode and wt do not share the row: %q", r)
	}
}

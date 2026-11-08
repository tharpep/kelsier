package main

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func peekModel(t *testing.T, sel string) (sbModel, *string) {
	t.Helper()
	var got string
	old := runKel
	runKel = func(dir string, env []string, args ...string) tea.Cmd {
		got = strings.Join(args, " ")
		return nil
	}
	t.Cleanup(func() { runKel = old })
	return drawerModel(t, sel), &got
}

func space(m sbModel) (sbModel, tea.Cmd) {
	nm, c := m.Update(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	return nm.(sbModel), c
}

// space on an agent row peeks at that agent's pane; on a pane row, at the pane.
func TestSpacePeeks(t *testing.T) {
	m, rec := peekModel(t, "a:@1")
	a, ok := m.agentOf("a:@1")
	if !ok {
		t.Fatal("fixture has no a:@1")
	}
	space(m)
	got := *rec
	if !strings.HasPrefix(got, "_panel peek ") || !strings.HasSuffix(got, " "+a.agent.Name+" "+a.pane) {
		t.Errorf("space on an agent: %q", got)
	}

	var pid string
	for _, id := range m.view().entryIDs {
		if p, ok := m.paneOf(id); ok && !strings.HasPrefix(id, "a:") {
			pid = p.PaneID
			break
		}
	}
	if pid == "" {
		t.Fatal("fixture has no pane row")
	}
	m, rec = peekModel(t, pid)
	*rec = ""
	space(m)
	got = *rec
	if !strings.HasPrefix(got, "_panel peek ") || !strings.HasSuffix(got, " "+pid) {
		t.Errorf("space on a pane: %q", got)
	}
}

// space keeps folding Repos and a quiet trailer; it peeks at nothing there.
func TestSpaceStillFolds(t *testing.T) {
	m, rec := peekModel(t, reposID)
	before := m.view().reposOpen
	m, _ = space(m)
	if *rec != "" {
		t.Errorf("space on Repos ran kel: %q", *rec)
	}
	if m.view().reposOpen == before {
		t.Error("space did not fold Repos")
	}
}

// the drawer lists Peek straight after Go to it, for agents and panes, not a background agent or Repos.
func TestDrawerListsPeek(t *testing.T) {
	m := drawerModel(t, "a:@1")
	items := entryMenu(m.view())
	if len(items) < 2 || items[0].label != "Go to it" || items[1] != (sbAct{"space", "Peek"}) {
		t.Errorf("agent drawer: %v", items)
	}
	for _, id := range []string{"b:bg1", reposID} {
		v := drawerModel(t, id).view()
		if v.sel != id {
			t.Fatalf("the cursor is on %q, not %q", v.sel, id)
		}
		for _, it := range entryMenu(v) {
			if it.label == "Peek" {
				t.Errorf("%s offers Peek", id)
			}
		}
	}
}

// choosing Peek in the drawer runs it.
func TestDrawerPeekRuns(t *testing.T) {
	m, rec := peekModel(t, "a:@1")
	m = press(m, "right")
	m = press(m, "down")
	if m.drawer == nil || m.drawer.items[m.drawer.sel].label != "Peek" {
		t.Fatalf("drawer: %+v", m.drawer)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !strings.HasPrefix(*rec, "_panel peek ") {
		t.Errorf("Peek in the drawer ran %q", *rec)
	}
}

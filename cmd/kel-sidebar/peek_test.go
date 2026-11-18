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
	return atModel(t, sel), &got
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

	for _, pid := range []string{"%2", "%14"} {
		m, rec = peekModel(t, pid)
		m.ui.expanded = map[string]bool{shellsKey: true}
		*rec = ""
		space(m)
		got = *rec
		if !strings.HasPrefix(got, "_panel peek ") || !strings.HasSuffix(got, " "+pid) {
			t.Errorf("space on pane %s: %q", pid, got)
		}
	}
}

// space folds a heading and a quiet trailer; it peeks at nothing there.
func TestSpaceStillFolds(t *testing.T) {
	m, rec := peekModel(t, "g:kelsier")
	m, _ = space(m)
	if *rec != "" {
		t.Errorf("space on a heading ran kel: %q", *rec)
	}
	if !m.view().shut["kelsier"] {
		t.Error("space did not fold the group")
	}
}

// The menu lists Peek straight after Go to it, for agents and panes, not a
// background agent or a heading.
func TestMenuListsPeek(t *testing.T) {
	m := atModel(t, "a:@1")
	items := entryMenu(m.view())
	if len(items) < 2 || items[0].label != "Go to it" || items[1] != (sbAct{"space", "Peek"}) {
		t.Errorf("agent menu: %v", items)
	}
	for _, id := range []string{"b:bg1", "g:kelsier"} {
		v := atModel(t, id).view()
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

// Choosing Peek in the menu runs it, as does the inspector's ␣.
func TestMenuPeekRuns(t *testing.T) {
	m, rec := peekModel(t, "a:@1")
	m = press(m, "right")
	m = press(m, "down")
	if m.menu == nil || m.menu.items[m.menu.sel].label != "Peek" {
		t.Fatalf("menu: %+v", m.menu)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !strings.HasPrefix(*rec, "_panel peek ") {
		t.Errorf("Peek in the menu ran %q", *rec)
	}
	m, rec = peekModel(t, "a:@1")
	x, y := inspectorHit(t, m, "space")
	m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	if !strings.HasPrefix(*rec, "_panel peek ") {
		t.Errorf("the inspector's peek ran %q", *rec)
	}
}

// A server's row in shells gives up its command's letters before its port.
func TestServerPortSurvivesANarrowRow(t *testing.T) {
	doc := loadFixture(t, "servers")
	out, _ := renderView(buildView(doc, uiState{expanded: map[string]bool{shellsKey: true}}), 16, 0, 0)
	if !strings.Contains(plain(out), ":8000") {
		t.Errorf("the port was cut:\n%s", plain(out))
	}
}

package main

import (
	"os"
	"strings"
	"testing"
)

func TestFreshSidebarAdoptsTheCurrentAgentAsItsSelection(t *testing.T) {
	for _, stale := range []string{"", "a:@gone"} {
		m := needsModel(t, "fifteen-three-repos")
		m.ui.myWindow = "@2"
		m.ui.sel = stale
		if stale != "" {
			m.saveShared()
		}
		nm, _ := m.Update(sbLoaded{m.doc})
		m = nm.(sbModel)
		if m.ui.sel != "a:@2" {
			t.Fatalf("stale %q: sel %q, want the current agent", stale, m.ui.sel)
		}
		if m.ui.sel != m.view().sel {
			t.Errorf("highlight %q and target %q differ", m.view().sel, m.ui.sel)
		}
		if dir, _ := m.spaceFor(m.ui.sel); dir == "" || dir == "~" || dir == homeDir() {
			t.Errorf("stale %q: new agent would start in %q", stale, dir)
		}
		if strings.HasSuffix(m.newWhere(), "~") {
			t.Errorf("stale %q: prompt names %q", stale, m.newWhere())
		}
		if sbLoadShared().Selected != "a:@2" {
			t.Errorf("stale %q: the adopted selection is shared", stale)
		}
	}
}

// The board and the shown sidebar share one selection. A tile the list has
// folded away (a background session, an agent in a shut repo) is still
// there, so the shown sidebar's reload must not take it from the board: it
// did, within two seconds, in a live rig.
func TestShownSidebarLeavesAFoldedSelectionShared(t *testing.T) {
	for _, sel := range []string{"b:bg2", "a:@4"} {
		m := needsModel(t, "fifteen-three-repos")
		m.doc, _ = loadBoardFixture(t, "fifteen-three-repos")
		m.ui.myWindow = "@2"
		m.ui.sel = sel
		m.ui.expanded = map[string]bool{bgKey: false, groupKey("api-gw"): false}
		m.saveShared()
		nm, _ := m.Update(sbLoaded{m.doc})
		m = nm.(sbModel)
		if got := sbLoadShared().Selected; got != sel {
			t.Errorf("%s folded away: the shown sidebar shared %q", sel, got)
		}
		if v := m.view(); v.sel == sel {
			t.Errorf("%s folded away is highlighted in the list, not its heading", sel)
		}
	}
}

func homeDir() string { return os.Getenv("HOME") }

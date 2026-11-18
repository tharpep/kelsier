package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/tharpep/kelsier/internal/fleet"
)

// rowsOf is the screen row of every entry, and of the inspector's first line.
func rowsOf(t *testing.T, v sbView, h int) map[string]int {
	t.Helper()
	_, refs := renderView(v, 32, h, 1792018800)
	at := map[string]int{}
	for i, r := range refs {
		switch {
		case r.kind == "inspector":
			if _, ok := at["inspector"]; !ok {
				at["inspector"] = i
			}
		case r.id != "" && listKind(r.kind):
			if _, ok := at[r.id]; ok {
				t.Fatalf("%s is on two rows", r.id)
			}
			at[r.id] = i
		}
	}
	return at
}

func sameRows(t *testing.T, what string, want, got map[string]int) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("%s: %d rows, want %d", what, len(got), len(want))
	}
	for id, y := range want {
		if got[id] != y {
			t.Errorf("%s: %s moved from row %d to %d", what, id, y, got[id])
		}
	}
}

// The core promise (issue #59): an entry is one line in every state,
// selected or not, so no row moves when an agent changes state or the
// selection moves. Every fixture, every entry selected, every agent put
// through every state.
func TestRowsNeverMoveWithStateOrSelection(t *testing.T) {
	states := []struct {
		state, note string
		unseen      bool
	}{
		{"working", "", false}, {"waiting", "Claude needs your permission to use Bash", false},
		{"waiting", "", false}, {"done", "", true}, {"done", "", false}, {"error", "API Error: Overloaded", false},
		{"dead", "", false}, {"throttled", "", false}, {"idle", "", false}, {"", "", false},
	}
	files, _ := filepath.Glob(filepath.Join(fixtures, "*.json"))
	for _, f := range files {
		name := strings.TrimSuffix(filepath.Base(f), ".json")
		doc := loadFixture(t, name)
		base := buildView(doc, uiState{})
		for _, h := range []int{0, 60} {
			want := rowsOf(t, base, h)
			for _, id := range base.entryIDs {
				sameRows(t, name+" selecting "+id, want, rowsOf(t, buildView(doc, uiState{sel: id}), h))
			}
			for i := range doc.Agents {
				for _, s := range states {
					d := doc
					d.Agents = append([]fleet.Agent(nil), doc.Agents...)
					a := &d.Agents[i]
					a.State, a.Unseen, a.Note = nil, s.unseen, nil
					if s.state != "" {
						a.State = sp(s.state)
					}
					if s.note != "" {
						a.Note = sp(s.note)
					}
					sameRows(t, name+" with "+a.Name+" "+s.state, want, rowsOf(t, buildView(d, uiState{sel: base.sel}), h))
				}
			}
		}
	}
}

// Every row of the list is exactly one screen line: no entry owns a second
// row in any fixture at any width.
func TestEveryEntryIsOneLine(t *testing.T) {
	files, _ := filepath.Glob(filepath.Join(fixtures, "*.json"))
	for _, f := range files {
		name := strings.TrimSuffix(filepath.Base(f), ".json")
		doc := loadFixture(t, name)
		for _, w := range []int{28, 32, 40} {
			v := buildView(doc, uiState{})
			for _, id := range v.entryIDs {
				_, refs := renderView(buildView(doc, uiState{sel: id}), w, 0, 1792018800)
				n := 0
				for _, r := range refs {
					if r.id == id {
						n++
					}
				}
				if n != 1 {
					t.Errorf("%s at %d: %s selected has %d rows", name, w, id, n)
				}
			}
		}
	}
}

// A repo group folds by hand and stays folded, shared through sidebar.json;
// folded, its heading carries a dot per agent in the agent's colour, and the
// agents leave the cursor's walk.
func TestRepoGroupFoldsByHand(t *testing.T) {
	m := atModel(t, "g:api-gw")
	m = press(m, "enter")
	if open, ok := m.ui.expanded[groupKey("api-gw")]; !ok || open {
		t.Fatalf("enter did not fold api-gw: %v", m.ui.expanded)
	}
	if sbLoadShared().Expanded[groupKey("api-gw")] {
		t.Error("the fold is not shared")
	}
	v := m.view()
	for _, id := range []string{"a:@4", "a:@5", "a:@11"} {
		if v.indexOf(id) >= 0 {
			t.Errorf("%s is still walked folded", id)
		}
	}
	out, _ := renderView(v, 32, 40, 1792018800)
	head := strings.Split(out, "\n")[rowOf(t, m, "g:api-gw")]
	if p := plain(head); !strings.HasPrefix(p, "▌▸ api-gw") || !strings.HasSuffix(strings.TrimRight(p, " "), "○ ◌ ●") {
		t.Errorf("folded heading %q", p)
	}
	if !strings.Contains(head, yellow+bold+"●") || !strings.Contains(head, purple+"◌") {
		t.Errorf("the dots are not in their states' colours: %q", head)
	}
	if got := press(press(m, "l"), "h"); got.ui.expanded[groupKey("api-gw")] {
		t.Error("l then h left the group open")
	}
	if got := press(m, "l"); !got.ui.expanded[groupKey("api-gw")] {
		t.Error("l did not open the group")
	}
}

// An agent folded away selects its heading, so the cursor is always on a row.
func TestFoldedAgentSelectsItsHeading(t *testing.T) {
	doc := loadFixture(t, "fifteen-three-repos")
	shut := map[string]bool{groupKey("kelsier"): false}
	if got := buildView(doc, uiState{sel: "a:@2", expanded: shut}).sel; got != "g:kelsier" {
		t.Errorf("a folded agent's selection: %q", got)
	}
	if got := buildView(doc, uiState{sel: "a:@gone", expanded: shut}).sel; got != "g:kelsier" {
		t.Errorf("the current agent folded: %q", got)
	}
	if got := buildView(doc, uiState{sel: "%14"}).sel; got != shellsID {
		t.Errorf("a pane in folded shells (as a peek's ↵ selects it): %q", got)
	}
	if got := buildView(doc, uiState{sel: bgID("bg2")}).sel; got != bgHeadID {
		t.Errorf("a folded background session: %q", got)
	}
}

// A heading too narrow for its dots keeps the ones that need you.
func TestNarrowRollupKeepsWhatNeedsYou(t *testing.T) {
	dots := []string{dim + "○" + reset, cyan + "●" + reset, dim + "○" + reset, yellow + bold + "●" + reset, dim + "○" + reset}
	if got := rollup(dots, 2); !strings.HasPrefix(got, yellow) {
		t.Errorf("two columns for five dots: %q", plain(got))
	}
	if got := plain(rollup(dots, 5)); got != "○●○●○" {
		t.Errorf("packed: %q", got)
	}
	if got := plain(rollup(dots, 9)); got != "○ ● ○ ● ○" {
		t.Errorf("spaced: %q", got)
	}
}

// The background and shells groups start folded and open by hand.
func TestBackgroundAndShellsFoldedByDefault(t *testing.T) {
	m := atModel(t, "a:@1")
	v := m.view()
	if v.bgOpen || v.shOpen || v.indexOf("%14") >= 0 || v.indexOf(bgID("bg2")) >= 0 {
		t.Fatalf("open by default: bg %v shells %v", v.bgOpen, v.shOpen)
	}
	m.ui.sel = shellsID
	m = press(m, "enter")
	m.ui.sel = bgHeadID
	m = press(m, "enter")
	v = m.view()
	if v.indexOf("%14") < 0 || v.indexOf(bgID("bg2")) < 0 {
		t.Errorf("enter did not open them: %v", v.entryIDs)
	}
}

// Fold rows and headings resolve to their repo's checkout, for n, f, g; the
// background and shells headings to none.
func TestFoldRowsResolveTheirSpace(t *testing.T) {
	m := needsModel(t, "density")
	for _, id := range []string{quietID("web-dashboard"), groupID("web-dashboard")} {
		if sp, ok := m.selSpace(id); !ok || sp.label != "web-dashboard" || sp.parent != "" {
			t.Errorf("%s resolves to %+v, %v; want the repo's main space", id, sp, ok)
		}
	}
	for _, id := range []string{shellsID, bgHeadID} {
		if _, ok := m.selSpace(id); ok {
			t.Errorf("%s resolves to a space", id)
		}
	}
}

// Scrolled into a group, its heading stays at the top of the list, and is
// still the heading: a click folds it.
func TestStickyGroupHeading(t *testing.T) {
	m := atModel(t, "a:@8")
	m.height = 16
	out, refs := renderView(m.view(), 32, 16, 1792018800)
	rows := strings.Split(plain(out), "\n")
	if refs[2].id != "g:kelsier" || !strings.HasPrefix(rows[2], " ▾ kelsier") || refs[3].id != "a:@3" {
		t.Errorf("row 2 is %q (%s), want the pinned heading:\n%s", rows[2], refs[2].id, strings.Join(rows, "\n"))
	}
	if rowOf(t, m, "a:@8") < 0 {
		t.Error("the selection is off screen")
	}
}

// The inspector is always inspectorRows tall, above a blank and the footer,
// whatever is selected, and empty when nothing is.
func TestInspectorKeepsItsHeight(t *testing.T) {
	doc := loadFixture(t, "fifteen-three-repos")
	base := buildView(doc, uiState{})
	ids := append(append([]string{}, base.entryIDs...), "%7", bgID("bg1"))
	for _, id := range ids {
		v := buildView(doc, uiState{sel: id})
		_, refs := renderView(v, 32, 36, 1792018800)
		var at []int
		for i, r := range refs {
			if r.kind == "inspector" {
				at = append(at, i)
			}
		}
		if len(at) != inspectorRows || at[0] != 36-2-inspectorRows || at[len(at)-1] != 36-3 {
			t.Errorf("%s: inspector rows %v", id, at)
		}
		if rows := v.inspectRows(32, 1792018800); len(rows) == 0 {
			t.Errorf("%s: an empty inspector", id)
		}
	}
	empty := buildView(fleet.Doc{Panes: &[]fleet.Pane{}}, uiState{})
	for _, l := range empty.inspector(32, 0) {
		if strings.TrimSpace(plain(l.s)) != "" {
			t.Errorf("nothing selected, yet %q", plain(l.s))
		}
	}
}

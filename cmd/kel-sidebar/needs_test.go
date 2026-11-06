package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/tharpep/kelsier/internal/fleet"
)

func needsModel(t *testing.T, fixture string) sbModel {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	_ = os.MkdirAll(filepath.Join(os.Getenv("XDG_STATE_HOME"), "kel"), 0o700)
	return sbModel{doc: loadFixture(t, fixture), width: 32, height: 60, visible: true}
}

func recordJumps(t *testing.T) *[]string {
	t.Helper()
	var got []string
	old := jump
	jump = func(wid, pane string) { got = append(got, wid+"|"+pane) }
	t.Cleanup(func() { jump = old })
	return &got
}

func needNames(v sbView) string {
	var n []string
	for _, a := range v.needs {
		n = append(n, a.agent.Name)
	}
	return strings.Join(n, " ")
}

func TestNeedsYouListsWhatWantsALook(t *testing.T) {
	v := buildView(loadFixture(t, "fifteen-three-repos"), uiState{})
	// done and unseen, then the two waiting, the longest-waiting first
	if got := needNames(v); got != "docs fix-auth api-auth" {
		t.Errorf("needs you: %q", got)
	}
	if v.entryIDs[0] != "q:@3" {
		t.Errorf("the pointers come first in the cursor's order: %q", v.entryIDs[:3])
	}
	out, _ := renderView(v, 32, 0, 1792018800)
	if !strings.Contains(plain(out), "  Needs you                   3\n") {
		t.Errorf("header with its count:\n%s", plain(out))
	}
	for _, name := range []string{"docs", "fix-auth", "api-auth"} {
		if strings.Count(plain(out), name) < 2 {
			t.Errorf("%s must show in Needs you and in its repo group", name)
		}
	}
	for _, f := range []string{"empty", "quota", "one-agent", "done-seen"} {
		out, _ := renderView(buildView(loadFixture(t, f), uiState{}), 32, 0, 0)
		if strings.Contains(out, "Needs you") {
			t.Errorf("%s: nothing needs you, so no section", f)
		}
	}
	doc := loadFixture(t, "error-agent")
	if got := needNames(buildView(doc, uiState{})); got != "retrying" {
		t.Errorf("an error needs you: %q", got)
	}
}

func TestNeedsYouOldestFirst(t *testing.T) {
	doc := loadFixture(t, "fifteen-three-repos")
	for i := range doc.Agents {
		var since float64
		switch doc.Agents[i].Name {
		case "docs":
			since = 3000
		case "fix-auth":
			since = 1000
		case "api-auth":
			since = 2000
		default:
			continue
		}
		doc.Agents[i].StateSince = &since
	}
	if got := needNames(buildView(doc, uiState{})); got != "fix-auth api-auth docs" {
		t.Errorf("by state_since, oldest first: %q", got)
	}
}

func TestNeedsYouPointerJumpsAndSelectsTheAgent(t *testing.T) {
	jumps := recordJumps(t)
	m := needsModel(t, "fifteen-three-repos")
	y := rowOf(t, m, "q:@2")
	nm, _ := m.Update(tea.MouseClickMsg{X: 8, Y: y, Button: tea.MouseLeft})
	m = nm.(sbModel)
	if len(*jumps) != 1 || (*jumps)[0] != "@2|%3" {
		t.Errorf("a click on the pointer jumps as the agent's own row does: %v", *jumps)
	}
	if m.ui.sel != "a:@2" {
		t.Errorf("selects the agent's row, not the pointer: %q", m.ui.sel)
	}
	if sbLoadShared().Selected != "a:@2" {
		t.Error("the selection is shared")
	}

	// j walks onto a pointer like any entry; enter on it does the same
	m = needsModel(t, "fifteen-three-repos")
	m.ui.sel = "q:@3"
	nm, _ = m.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	m = nm.(sbModel)
	if m.view().sel != "q:@2" {
		t.Errorf("j passes through the pointers: %q", m.view().sel)
	}
	*jumps = nil
	nm, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = nm.(sbModel)
	if len(*jumps) != 1 || m.ui.sel != "a:@2" {
		t.Errorf("enter on a pointer: jumps %v, sel %q", *jumps, m.ui.sel)
	}
}

func hasSummary(a fleet.Agent) bool {
	if a.State == nil {
		return false
	}
	switch *a.State {
	case "working":
		return a.Doing != nil && *a.Doing != ""
	case "waiting", "error":
		return a.Note != nil && *a.Note != ""
	case "done", "idle":
		return a.Title != nil && *a.Title != ""
	}
	return false
}

// Every agent has one summary line when something is known and none when
// nothing is, in every fixture, selected or not.
func TestOneSummaryLinePerAgent(t *testing.T) {
	files, _ := filepath.Glob(filepath.Join(fixtures, "*.json"))
	for _, f := range files {
		name := strings.TrimSuffix(filepath.Base(f), ".json")
		doc := loadFixture(t, name)
		base := buildView(doc, uiState{})
		for _, sel := range append([]string{""}, agentIDs(base)...) {
			ui := uiState{sel: sel}
			if sel == "" && len(base.spaces) > 0 {
				ui.sel = spaceID(base.spaces[0].key)
			}
			v := buildView(doc, ui)
			_, refs := renderView(v, 32, 0, 1792018800)
			for _, a := range v.agents {
				id := agentID(a.key)
				n := 0
				for _, r := range refs {
					if r.id == id && r.sum {
						n++
					}
				}
				switch {
				case !hasSummary(a.agent) && n != 0:
					t.Errorf("%s %s (sel %q): %d summary lines for an agent with nothing known", name, a.agent.Name, ui.sel, n)
				case hasSummary(a.agent) && id != ui.sel && n != 1:
					t.Errorf("%s %s: %d summary lines, want exactly one", name, a.agent.Name, n)
				case hasSummary(a.agent) && id == ui.sel && (n < 1 || n > 2):
					t.Errorf("%s %s selected: %d summary lines, want one wrapped to at most two", name, a.agent.Name, n)
				}
			}
		}
	}
}

func agentIDs(v sbView) []string {
	var ids []string
	for _, a := range v.agents {
		ids = append(ids, agentID(a.key))
	}
	return ids
}

func TestUnknownAgentSaysNoHooks(t *testing.T) {
	out, _ := renderView(buildView(loadFixture(t, "unknowns"), uiState{}), 32, 0, 0)
	for _, l := range strings.Split(plain(out), "\n") {
		if strings.Contains(l, "nostate") {
			if !strings.HasPrefix(l, "  ? nostate") || !strings.HasSuffix(strings.TrimRight(l, " "), "no hooks") {
				t.Errorf("an agent kel has no hooks for: %q", l)
			}
			return
		}
	}
	t.Error("no nostate row")
}

func TestSelectedBlockCarriesTitleAndModel(t *testing.T) {
	doc := loadFixture(t, "one-agent")
	out, _ := renderView(buildView(doc, uiState{}), 32, 0, 1792018800)
	rows := strings.Split(plain(out), "\n")
	var at int
	for i, l := range rows {
		if strings.Contains(l, "Okay, so I put out PR 16 as") {
			at = i
		}
	}
	if at == 0 || !strings.Contains(rows[at+1], "an audit and exploration.") {
		t.Fatalf("the title wraps in the block:\n%s", plain(out))
	}
	if !strings.Contains(rows[at+2], "Opus 5.5 · 41% · $1.20") {
		t.Errorf("model, context and cost under the title: %q", rows[at+2])
	}
	doc.Agents[0].Context.Model = nil
	out, _ = renderView(buildView(doc, uiState{}), 32, 0, 1792018800)
	if !strings.Contains(plain(out), "   41% · $1.20") {
		t.Errorf("an unknown model leaves the line as it was:\n%s", plain(out))
	}
}

func TestSplitNote(t *testing.T) {
	for in, want := range map[string][2]string{
		"Claude needs your permission to use Bash":                   {"allow Bash?", ""},
		"Claude needs your permission to use Bash: go test ./... -v": {"allow Bash?", "go test ./... -v"},
		"API Error: Rate limit reached":                              {"Rate limit reached", ""},
	} {
		if r, a := splitNote(in); r != want[0] || a != want[1] {
			t.Errorf("%q: %q %q, want %q", in, r, a, want)
		}
	}
	m := "Opus 5.5 (1M context)"
	if shortModel(&m) != "Opus 5.5" || shortModel(nil) != "" {
		t.Errorf("shortModel: %q", shortModel(&m))
	}
}

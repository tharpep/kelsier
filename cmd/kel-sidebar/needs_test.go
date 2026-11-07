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

func TestAgentWithNoStateReadsUnknown(t *testing.T) {
	out, _ := renderView(buildView(loadFixture(t, "unknowns"), uiState{}), 32, 0, 0)
	for _, l := range strings.Split(plain(out), "\n") {
		if strings.Contains(l, "nostate") {
			if !strings.HasPrefix(l, "  ? nostate") || !strings.HasSuffix(strings.TrimRight(l, " "), "unknown") {
				t.Errorf("an agent with no state should read unknown: %q", l)
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

func TestShortNoteAndModel(t *testing.T) {
	for in, want := range map[string]string{
		"Claude needs your permission to use Bash": "allow Bash?",
		"API Error: Rate limit reached":            "Rate limit reached",
	} {
		if got := shortNote(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
	m := "Opus 5.5 (1M context)"
	if shortModel(&m) != "Opus 5.5" || shortModel(nil) != "" {
		t.Errorf("shortModel: %q", shortModel(&m))
	}
}

func TestNeedsYouStaysPinnedWhileScrolled(t *testing.T) {
	doc := loadFixture(t, "fifteen-three-repos")
	out, _ := renderView(buildView(doc, uiState{focused: true, sel: "a:@8"}), 32, 30, 1792018800)
	rows := strings.Split(plain(out), "\n")
	if len(rows) != 30 {
		t.Fatalf("%d rows", len(rows))
	}
	for i, want := range []string{"  kel ", "", "  Needs you ", "  ● docs ", "  ● fix-auth ", "  ● api-auth ", ""} {
		if !strings.HasPrefix(rows[i], want) {
			t.Errorf("row %d: %q, want %q first:\n%s", i, rows[i], want, strings.Join(rows[:8], "\n"))
		}
	}
	if !strings.Contains(plain(out), "spike") {
		t.Error("the selection is scrolled off")
	}
}

func TestNeedsYouFoldsPastFour(t *testing.T) {
	jumps := recordJumps(t)
	m := sixNeeding(t)
	m.ui.sel = "a:@8"
	out, refs := renderView(m.view(), m.width, m.height, 1792018800)
	rows := strings.Split(plain(out), "\n")
	if got := needNames(m.view()); got != "docs fix-auth api-auth gateway kelsier dashboard" {
		t.Fatalf("needs: %q", got)
	}
	if !strings.HasPrefix(rows[5], "  ● api-auth") || !strings.HasPrefix(rows[6], "  +3 more") || rows[7] != "" {
		t.Errorf("three rows then +3 more:\n%s", strings.Join(rows[:9], "\n"))
	}
	if !strings.Contains(out, yellow+"+3 more") {
		t.Error("+N more is in the waiting colour")
	}
	nm, _ := m.Update(tea.MouseClickMsg{X: 4, Y: 6, Button: tea.MouseLeft})
	m = nm.(sbModel)
	if refs[6].id != "q:@4" || len(*jumps) != 1 || (*jumps)[0] != "@4|%6" || m.ui.sel != "a:@4" {
		t.Errorf("a click on +3 more jumps to the oldest one not shown (gateway): id %q jumps %v sel %q", refs[6].id, *jumps, m.ui.sel)
	}
}

func sixNeeding(t *testing.T) sbModel {
	m := needsModel(t, "fifteen-three-repos")
	m.height = 30
	for i := range m.doc.Agents {
		a := &m.doc.Agents[i]
		since, ok := map[string]float64{"docs": 100, "fix-auth": 200, "api-auth": 300, "gateway": 400, "kelsier": 500, "dashboard": 600}[a.Name]
		if !ok {
			continue
		}
		a.State, a.StateSince, a.Note, a.Unseen = sp("waiting"), &since, sp("Claude needs your permission to use Edit"), false
	}
	return m
}

func TestCursorIsAlwaysOnAVisibleRowPastTheFold(t *testing.T) {
	jumps := recordJumps(t)
	m := sixNeeding(t)
	m.ui.sel = "q:@3"
	for step := 0; step < 6; step++ {
		sel := m.view().sel
		if want := m.view().entryIDs[step]; sel != want {
			t.Fatalf("step %d: sel %q, want %q", step, sel, want)
		}
		out, refs := renderView(m.view(), m.width, m.height, 1792018800)
		shown := false
		for i, r := range refs {
			if r.id == sel && strings.Contains(strings.Split(out, "\n")[i], raised) {
				shown = true
			}
		}
		if !shown {
			t.Errorf("step %d: %q has no raised row:\n%s", step, sel, plain(out))
		}
		nm, _ := m.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
		m = nm.(sbModel)
	}
	m.ui.sel = "q:@6"
	nm, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = nm.(sbModel)
	if len(*jumps) != 1 || m.ui.sel != "a:@6" {
		t.Errorf("enter on the selected hidden row: jumps %v sel %q", *jumps, m.ui.sel)
	}
}

func TestPointerCursorFallsBackToItsAgent(t *testing.T) {
	doc := loadFixture(t, "fifteen-three-repos")
	if got := buildView(doc, uiState{sel: "q:@5"}).sel; got != "a:@5" {
		t.Errorf("q: of an agent that left Needs you: %q", got)
	}
}

func TestQuietKind(t *testing.T) {
	const now = 1792018800
	st := func(s string) *string { return &s }
	since := func(ago float64) *float64 { f := float64(now) - ago; return &f }
	idle := func(s string, since, act *float64) fleet.Agent {
		return fleet.Agent{State: &s, StateSince: since, Activity: act}
	}
	for _, c := range []struct {
		name string
		a    fleet.Agent
		want string
	}{
		{"nil state, no clock", fleet.Agent{}, ""},
		{"unknown, no since, activity 25h old", fleet.Agent{Activity: since(90000)}, "unknown"},
		{"unknown, no since, activity 1h old", fleet.Agent{Activity: since(3600)}, ""},
		{"idle, no since, old activity", idle("idle", nil, since(9e5)), ""},
		{"nil state, young", fleet.Agent{StateSince: since(60)}, ""},
		{"nil state at 24h", fleet.Agent{StateSince: since(86400)}, "unknown"},
		{"empty state at 24h", fleet.Agent{State: st(""), StateSince: since(90000)}, "unknown"},
		{"unknown just under 24h", fleet.Agent{StateSince: since(86399)}, ""},
		{"idle at 24h", fleet.Agent{State: st("idle"), StateSince: since(86400)}, "idle"},
		{"idle just under 24h", fleet.Agent{State: st("idle"), StateSince: since(86399)}, ""},
		{"done at 24h", fleet.Agent{State: st("done"), StateSince: since(90000)}, "idle"},
		{"idle with no clock", fleet.Agent{State: st("idle")}, ""},
		{"waiting for days", fleet.Agent{State: st("waiting"), StateSince: since(9e5)}, ""},
		{"working for days", fleet.Agent{State: st("working"), StateSince: since(9e5)}, ""},
		{"error for days", fleet.Agent{State: st("error"), StateSince: since(9e5)}, ""},
		{"dead for days", fleet.Agent{State: st("dead"), StateSince: since(9e5)}, ""},
		{"throttled for days", fleet.Agent{State: st("throttled"), StateSince: since(9e5)}, ""},
	} {
		if got := quietKind(c.a, now); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestQuietFoldNeverHidesWhatMatters(t *testing.T) {
	doc := loadFixture(t, "density")
	const now = 1792018800
	folded := func(ui uiState) map[string]bool {
		ui.now = now
		out := map[string]bool{}
		for _, a := range buildView(doc, ui).agents {
			if a.folded {
				out[a.agent.Name] = true
			}
		}
		return out
	}
	if got := folded(uiState{}); !got["gateway"] || !got["dashboard"] || !got["scratch"] || len(got) != 3 {
		t.Errorf("folded: %v, want gateway dashboard scratch", got)
	}
	if got := folded(uiState{sel: "a:@4"}); got["gateway"] {
		t.Error("the selected agent folded")
	}
	if got := folded(uiState{expanded: map[string]bool{quietKey("web-dashboard"): true}}); got["dashboard"] || got["scratch"] || !got["gateway"] {
		t.Errorf("quiet:web-dashboard does not open that group alone: %v", got)
	}
	for i := range doc.Agents {
		if doc.Agents[i].Name == "dashboard" {
			doc.Agents[i].Unseen = true
		}
	}
	if got := folded(uiState{}); got["dashboard"] {
		t.Error("an unseen done agent, in Needs you, folded")
	}
	for i := range doc.Agents {
		if doc.Agents[i].Name == "kelsier" {
			doc.Agents[i].State = nil
		}
	}
	if got := folded(uiState{}); got["kelsier"] {
		t.Error("the agent of the window the sidebar sits in folded")
	}
	if got := folded(uiState{filter: "a"}); len(got) != 0 {
		t.Errorf("a filter left agents folded: %v", got)
	}
}

func TestQuietText(t *testing.T) {
	for _, c := range []struct {
		q    sbQuiet
		want string
	}{
		{sbQuiet{idle: 3, unknown: 1}, "+ 3 idle 1d+ · 1 unknown"},
		{sbQuiet{idle: 2}, "+ 2 idle 1d+"},
		{sbQuiet{unknown: 2}, "+ 2 unknown"},
		{sbQuiet{idle: 1, unknown: 1, open: true}, "− 1 idle 1d+ · 1 unknown"},
	} {
		if got := c.q.text(); got != c.want {
			t.Errorf("got %q, want %q", got, c.want)
		}
	}
}

func TestDensityFrames(t *testing.T) {
	t.Setenv("HOME", "/home/me")
	doc := loadFixture(t, "density")
	open := uiState{now: 1792018800, expanded: map[string]bool{reposKey: true, quietKey("web-dashboard"): true}}
	out, _ := renderView(buildView(doc, open), 32, 0, 1792018800)
	got := plain(out) + "\n"
	gold := filepath.Join(fixtures, "density-open.32.golden")
	if *update {
		if err := os.WriteFile(gold, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(gold)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("changed:\n--- got\n%s\n--- want\n%s", got, want)
	}
}

func TestQuietTrailerToggles(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	m := sbModel{doc: loadFixture(t, "density"), width: 32, height: 40, visible: true, ui: uiState{now: 1792018800}}
	id := quietID("web-dashboard")
	if m.view().indexOf(id) < 0 {
		t.Fatalf("the trailer is not selectable: %v", m.view().entryIDs)
	}
	m.activate(id)
	if !m.ui.expanded[quietKey("web-dashboard")] {
		t.Error("enter on the trailer did not open the group")
	}
	m.activate(id)
	if m.ui.expanded[quietKey("web-dashboard")] {
		t.Error("a second enter did not fold the group")
	}
}

func TestFoldRowsResolveTheirSpace(t *testing.T) {
	m := needsModel(t, "density")
	m.ui.expanded = map[string]bool{reposKey: true}
	if sp, ok := m.selSpace(quietID("web-dashboard")); !ok || sp.label != "web-dashboard" || sp.parent != "" {
		t.Errorf("a quiet trailer resolves to %+v, %v; want the repo's main space", sp, ok)
	}
	if _, ok := m.selSpace(reposID); ok {
		t.Error("the Repos header resolves to a space")
	}
	m.ui.sel = reposID
	for _, k := range []string{"n", "w", "t", "f", "g", "d"} {
		nm, cmd := m.Update(tea.KeyPressMsg{Code: rune(k[0]), Text: k})
		got := nm.(sbModel)
		if cmd != nil || got.mode != modeNormal || !got.msgBad {
			t.Errorf("%s on Repos: mode %v msgBad %v cmd %v", k, got.mode, got.msgBad, cmd != nil)
		}
	}
}

func TestFilteredShellKeepsItsMenu(t *testing.T) {
	doc := loadFixture(t, "servers")
	for i := range *doc.Panes {
		if p := &(*doc.Panes)[i]; p.PaneID == "%3" {
			p.Cmd, p.Ports = "vite", &[]int{5173}
		}
	}
	v := buildView(doc, uiState{filter: "vite", sel: "%3"})
	if len(v.shells) != 1 || len(v.spaces) != 0 {
		t.Fatalf("filter vite kept %d shells, %d spaces", len(v.shells), len(v.spaces))
	}
	if kind, name, acts := selActions(v); kind != "pane" || name != "vite :5173" || len(acts) == 0 {
		t.Errorf("hints: %q %q %v", kind, name, acts)
	}
	if acts := entryMenu(v); len(acts) == 0 {
		t.Errorf("menu: %v", acts)
	}
}

func TestPinnedReposHeaderFolds(t *testing.T) {
	doc := loadFixture(t, "fifteen-three-repos")
	ui := uiState{focused: true, sel: spaceID("/home/me/code/web-dashboard"), expanded: map[string]bool{reposKey: true}}
	out, refs := renderView(buildView(doc, ui), 32, 16, 0)
	rows := strings.Split(out, "\n")
	for i, r := range refs {
		if r.id != reposID {
			continue
		}
		if strings.Contains(plain(rows[i]), "▌") || !strings.HasPrefix(plain(rows[i]), "  Repos ▾") {
			t.Errorf("pinned Repos row %q carries the cursor or lost its label", plain(rows[i]))
		}
		m := sbModel{doc: doc, width: 32, height: 16, visible: true, ui: ui}
		m.activate(reposID)
		if m.ui.expanded[reposKey] {
			t.Error("a click on the pinned row did not fold Repos")
		}
		return
	}
	t.Fatal("no row carries the Repos id while scrolled inside it")
}

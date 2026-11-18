package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

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

// What needs you is counted in the footer, in the waiting colour, and a
// click on the count is ` (the next one); the agents stay in their groups,
// once each, with no section of their own.
func TestNeedsYouIsTheFootersCount(t *testing.T) {
	v := buildView(loadFixture(t, "fifteen-three-repos"), uiState{})
	if got := needNames(v); got != "docs fix-auth api-auth" {
		t.Errorf("needs you: %q", got)
	}
	out, refs := renderView(v, 32, 30, 1792018800)
	rows := strings.Split(plain(out), "\n")
	if foot := rows[len(rows)-1]; !strings.HasPrefix(foot, " ●3 need you") {
		t.Errorf("footer %q", foot)
	}
	if !strings.Contains(out, yellow+bold+"●3 need you") {
		t.Error("the count is not in the waiting colour")
	}
	if k := refs[len(refs)-1].hitAt(3); k != "`" {
		t.Errorf("a click on the count presses %q", k)
	}
	for _, name := range []string{"docs", "fix-auth", "api-auth"} {
		n := 0
		for _, r := range rows {
			if strings.HasPrefix(strings.TrimLeft(r, " ▌"), "● "+name+" ") {
				n++
			}
		}
		if n != 1 {
			t.Errorf("%s has %d rows, want its one in its group", name, n)
		}
	}
	if strings.Contains(plain(out), "Needs you") {
		t.Error("a Needs you section is drawn")
	}
	for _, f := range []string{"empty", "quota", "one-agent", "done-seen"} {
		out, _ := renderView(buildView(loadFixture(t, f), uiState{}), 32, 30, 0)
		if strings.Contains(out, "need you") {
			t.Errorf("%s: nothing needs you, so no count", f)
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
			if !strings.HasPrefix(l, " ? nostate") || !strings.HasSuffix(strings.TrimRight(l, " "), "unknown") {
				t.Errorf("an agent with no state should read unknown: %q", l)
			}
			return
		}
	}
	t.Error("no nostate row")
}

// The inspector carries the last message, on one line, and the model,
// context and cost; the session's title never stands in.
func TestInspectorCarriesLastMessageAndModel(t *testing.T) {
	doc := loadFixture(t, "one-agent")
	done := "done"
	doc.Agents[0].State = &done
	doc.Agents[0].Doing = nil
	doc.Agents[0].LastMessage = sp("PR 59 is merged, so passes now skip the card greeting and go straight on")
	text := func() string {
		var rows []string
		for _, l := range buildView(doc, uiState{}).inspector(32, 1792018800) {
			rows = append(rows, plain(l.s))
		}
		return strings.Join(rows, "\n")
	}
	got := text()
	if !strings.Contains(got, " PR 59 is merged, so pass") {
		t.Errorf("the last message, cut to its row:\n%s", got)
	}
	if strings.Contains(got, "Okay, so I put out PR 16") {
		t.Errorf("the title is in the inspector:\n%s", got)
	}
	if !strings.Contains(got, " Opus 5.5 · 41% · $1.20") {
		t.Errorf("model, context and cost:\n%s", got)
	}
	doc.Agents[0].Context.Model = nil
	if got := text(); !strings.Contains(got, "\n 41% · $1.20") {
		t.Errorf("an unknown model leaves the line as it was:\n%s", got)
	}
}

func TestSummaryLineByState(t *testing.T) {
	doc := loadFixture(t, "one-agent")
	a := &doc.Agents[0]
	a.LastMessage = sp("the last words")
	for state, want := range map[string]string{"working": "editing sbview.go", "done": "the last words", "idle": "the last words"} {
		a.State = &state
		a.Doing = sp("editing sbview.go")
		if got := summaryOf(*a).text; got != want {
			t.Errorf("%s: %q, want %q", state, got, want)
		}
		if state == "working" {
			continue
		}
		out, _ := renderView(buildView(doc, uiState{}), 32, 0, 1792018800)
		if !strings.Contains(plain(out), want) {
			t.Errorf("%s: the line is not drawn:\n%s", state, plain(out))
		}
	}
	for _, state := range []string{"done", "idle"} {
		a.State = &state
		a.LastMessage = nil
		if got := summaryOf(*a); got.text != "" {
			t.Errorf("%s with no message has a line: %q", state, got.text)
		}
		out, _ := renderView(buildView(doc, uiState{}), 32, 0, 1792018800)
		if strings.Contains(plain(out), "Okay, so I put out PR 16") {
			t.Errorf("%s: the title stands in for a missing message:\n%s", state, plain(out))
		}
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
		{"unknown, no since, activity at 24h", fleet.Agent{Activity: since(86400)}, "unknown"},
		{"done seen, no clock", fleet.Agent{State: st("done")}, ""},
		{"unknown, no since, activity 1h old", fleet.Agent{Activity: since(3600)}, ""},
		{"idle, no since, old activity", idle("idle", nil, since(9e5)), ""},
		{"nil state, young", fleet.Agent{StateSince: since(60)}, ""},
		{"nil state at 24h", fleet.Agent{StateSince: since(86400)}, "unknown"},
		{"empty state at 24h", fleet.Agent{State: st(""), StateSince: since(90000)}, "unknown"},
		{"unknown just under 24h", fleet.Agent{StateSince: since(86399)}, ""},
		{"idle at 24h", fleet.Agent{State: st("idle"), StateSince: since(86400)}, "idle"},
		{"idle just under 24h", fleet.Agent{State: st("idle"), StateSince: since(86399)}, ""},
		{"idle at 23h59m", fleet.Agent{State: st("idle"), StateSince: since(23*3600 + 59*60)}, ""},
		{"done seen 23h59m", fleet.Agent{State: st("done"), StateSince: since(23*3600 + 59*60)}, ""},
		{"done seen 24h", fleet.Agent{State: st("done"), StateSince: since(86400)}, "idle"},
		{"done unseen 3d", fleet.Agent{State: st("done"), Unseen: true, StateSince: since(259200)}, ""},
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
	open := uiState{now: 1792018800, expanded: map[string]bool{shellsKey: true, bgKey: true, quietKey("web-dashboard"): true}}
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

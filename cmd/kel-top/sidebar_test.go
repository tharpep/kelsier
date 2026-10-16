package main

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/tharpep/kelsier/internal/fleet"
)

// -update rewrites the golden frames from what renders now. Only after the
// author has looked at them (test/sidebar-frames.sh): a golden file is an
// approved frame, not a snapshot of whatever the code happens to draw.
var update = flag.Bool("update", false, "rewrite testdata/sidebar/*.golden")

const fixtures = "testdata/sidebar"

func loadFixture(t *testing.T, name string) fleet.Doc {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(fixtures, name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var d fleet.Doc
	if err := json.Unmarshal(b, &d); err != nil {
		t.Fatal(err)
	}
	return d
}

var ansi = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func plain(s string) string { return ansi.ReplaceAllString(s, "") }

// Every fixture at every width the sidebar is meant for, colours included.
func TestSidebarGolden(t *testing.T) {
	t.Setenv("HOME", "/home/me") // the fixtures' home, for ~ in paths
	files, _ := filepath.Glob(filepath.Join(fixtures, "*.json"))
	if len(files) == 0 {
		t.Fatal("no fixtures")
	}
	for _, f := range files {
		name := strings.TrimSuffix(filepath.Base(f), ".json")
		for _, c := range []struct {
			w, h    int
			focused bool
		}{{28, 0, false}, {32, 0, false}, {40, 0, false}, {32, 0, true}, {32, 30, true}} {
			w := c.w
			got, err := fixtureFrame(f, w, c.h, c.focused)
			if err != nil {
				t.Fatal(err)
			}
			gold := filepath.Join(fixtures, name+"."+itoa(w)+".golden")
			if c.focused {
				gold = filepath.Join(fixtures, name+".focused."+itoa(w)+".golden")
			}
			if c.h > 0 {
				// at a real height: the actions group at the foot
				gold = filepath.Join(fixtures, name+".focused."+itoa(w)+"x"+itoa(c.h)+".golden")
			}
			if *update {
				if err := os.WriteFile(gold, []byte(got+"\n"), 0o644); err != nil {
					t.Fatal(err)
				}
				continue
			}
			want, err := os.ReadFile(gold)
			if err != nil {
				t.Fatalf("%s: %v (run: go test ./cmd/kel-top -run Golden -update, after reviewing the frames)", gold, err)
			}
			if got+"\n" != string(want) {
				t.Errorf("%s at %d cols changed:\n--- got\n%s\n--- want\n%s", name, w, plain(got), plain(string(want)))
			}
		}
	}
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

// Every width from 0 up must render without panicking: tmux squeezes the
// pane on a resize before kel re-pins it. The first version died at 1 column.
func TestSidebarRendersAtAnyWidth(t *testing.T) {
	doc := loadFixture(t, "fifteen-three-repos")
	for w := 0; w <= 40; w++ {
		for _, h := range []int{0, 1, 2, 5, 40} {
			out, refs := renderView(buildView(doc, uiState{}), w, h, 0)
			if h > 0 && len(refs) > h {
				t.Errorf("%dx%d: %d lines", w, h, len(refs))
			}
			if h > 0 && strings.Count(out, "\n")+1 != len(refs) {
				t.Errorf("%dx%d: refs out of step with lines", w, h)
			}
		}
	}
}

func TestSidebarView(t *testing.T) {
	v := buildView(loadFixture(t, "fifteen-three-repos"), uiState{})
	var keys []string
	for _, s := range v.spaces {
		keys = append(keys, s.label+"|"+filepath.Base(s.key))
		for _, p := range s.panes {
			if p.Kind == "agent" {
				t.Errorf("agent pane %s listed under space %s: agents have one home", p.PaneID, s.label)
			}
			if p.Cmd == "bash" && p.WindowID == "@8" {
				t.Errorf("a dead agent's shell reappeared under %s", s.label)
			}
		}
		if s.open != s.current {
			t.Errorf("space %s: open=%v current=%v; only the current space opens by default", s.label, s.open, s.current)
		}
	}
	// a worktree is its own space; the main checkout sorts before its worktrees
	want := "api-gw|api-gw kelsier|kelsier kelsier|kelsier-docs kelsier|kelsier-fix-auth misc|group:misc web-dashboard|web-dashboard web-dashboard|web-dashboard-usage-charts"
	if got := strings.Join(keys, " "); got != want {
		t.Errorf("spaces:\n got %s\nwant %s", got, want)
	}
	var names []string
	for _, a := range v.agents {
		names = append(names, a.agent.Name)
	}
	// triage: waiting, working, an unseen done, throttled, seen done, idle, dead
	if got := strings.Join(names, " "); got != "fix-auth kelsier usage-charts docs rate-limits dashboard gateway spike" {
		t.Errorf("agents in triage order: got %s", got)
	}
	if v.sel != "a:@1" {
		t.Errorf("default selection: got %q, want the current agent", v.sel)
	}

	// a toggle closes the current space and opens another
	v = buildView(loadFixture(t, "fifteen-three-repos"), uiState{expanded: map[string]bool{
		"/home/me/code/kelsier": false, "/home/me/code/api-gw": true}})
	for _, s := range v.spaces {
		if want := s.label == "api-gw"; s.open != want {
			t.Errorf("after toggles, space %s open=%v", s.key, s.open)
		}
	}

	// unknown panes say so, in both sections
	out, _ := renderView(buildView(fleet.Doc{}, uiState{}), 32, 0, 0)
	if strings.Count(plain(out), "? panes unknown") != 2 {
		t.Errorf("a doc with no panes list must read unknown, not empty:\n%s", plain(out))
	}
}

// Each screen line maps to the entry drawn on it — what a click will hit.
func TestSidebarLineRefs(t *testing.T) {
	v := buildView(loadFixture(t, "one-agent"), uiState{})
	out, refs := renderView(v, 32, 12, 0)
	lines := strings.Split(plain(out), "\n")
	for i, l := range lines {
		if strings.Contains(l, "working") && refs[i].id != "a:@1" {
			t.Errorf("line %d %q maps to %+v", i, l, refs[i])
		}
		if strings.Contains(l, "main ↑2") && refs[i].kind != "space" {
			t.Errorf("line %d %q maps to %+v", i, l, refs[i])
		}
	}
	if !strings.Contains(lines[len(lines)-1], "Ctrl-b then space") {
		t.Error("the footer is not pinned to the last line")
	}
}

// Selection is an accent bar, never inverse video (spec.md §5d).
func TestSidebarNoInverse(t *testing.T) {
	out, _ := renderView(buildView(loadFixture(t, "fifteen-three-repos"), uiState{}), 32, 0, 0)
	if strings.Contains(out, "\x1b[7m") {
		t.Error("inverse video in the sidebar")
	}
	if !strings.Contains(out, accent+"▌") {
		t.Error("no selection bar")
	}
}

func TestParseVisible(t *testing.T) {
	cases := map[string][2]bool{
		"1 1": {true, false}, "1 2 1\n": {true, true}, "1 1 0": {true, false},
		"0 1 1": {false, false}, "1 0 1": {false, false}, "": {false, false}, "x": {false, false},
	}
	for in, want := range cases {
		if v, a := parseVisible(in); v != want[0] || a != want[1] {
			t.Errorf("parseVisible(%q) = %v %v, want %v", in, v, a, want)
		}
	}
}

// The footer: how to get in while unfocused; the selection's keys once in.
func TestSidebarHint(t *testing.T) {
	doc := loadFixture(t, "fifteen-three-repos")
	foot := func(ui uiState, w int) string {
		out, _ := renderView(buildView(doc, ui), w, 30, 0)
		l := strings.Split(plain(out), "\n")
		return l[len(l)-1]
	}
	// someone waits: the unfocused footer is the alarm, and jumps on a click
	if got := foot(uiState{}, 32); got != "1 waiting · click to jump" {
		t.Errorf("unfocused footer with one waiting: %q", got)
	}
	out, refs := renderView(buildView(loadFixture(t, "one-agent"), uiState{}), 32, 30, 0)
	if l := strings.Split(plain(out), "\n"); l[len(l)-1] != "click, or Ctrl-b then space" {
		t.Errorf("unfocused footer: %q", l[len(l)-1])
	}
	out, refs = renderView(buildView(doc, uiState{}), 32, 30, 0)
	_ = out
	if r := refs[len(refs)-1]; r.kind != "action" || r.id != "act:`" {
		t.Errorf("the alarm does not jump: %+v", r)
	}
	if got := foot(uiState{focused: true}, 32); !strings.HasPrefix(got, "e edit · p plan · x kill") {
		t.Errorf("agent footer: %q", got)
	}
	if got := foot(uiState{focused: true, sel: spaceID("/home/me/code/api-gw")}, 32); !strings.HasPrefix(got, "f files · g git · e edit") {
		t.Errorf("space footer: %q", got)
	}
	if got := foot(uiState{focused: true, sel: "%7", expanded: map[string]bool{"/home/me/code/api-gw": true}}, 40); !strings.Contains(got, "o open") {
		t.Errorf("server footer: %q", got)
	}
	for w := 1; w <= 40; w++ {
		if got := foot(uiState{focused: true}, w); len([]rune(got)) > w {
			t.Errorf("width %d: footer %q overflows", w, got)
		}
	}
	if got := foot(uiState{focused: true}, 40); !strings.HasSuffix(got, "· ?") {
		t.Errorf("with room, the footer ends in ?: %q", got)
	}
}

func TestSummarise(t *testing.T) {
	refusal := "\x1b[1;33mkel\x1b[0m refusing to kill 'x' — the worktree has work that isn't safe to lose:\n" +
		"  uncommitted changes:\n    ?? notes.txt\n    M  main.go\n\x1b[1;31mkel\x1b[0m commit & push, or run: kel kill x --force\n"
	if got := summarise(refusal); got != "kept — at risk: ?? notes.txt, M  main.go" {
		t.Errorf("refusal: %q", got)
	}
	if got := summarise("creating worktree  /x\nkel new agent  a  [g]  (inplace)\n"); got != "new agent  a  [g]  (inplace)" {
		t.Errorf("last line: %q", got)
	}
}

// keys and clicks, driven through Update with no tmux: nothing here jumps,
// because a jump would talk to whatever tmux server the test runs under
func TestSidebarKeys(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	os.MkdirAll(filepath.Join(os.Getenv("XDG_STATE_HOME"), "kel"), 0o700)
	m := sbModel{doc: loadFixture(t, "fifteen-three-repos"), width: 32, height: 60, visible: true}
	press := func(k string) {
		var msg tea.KeyPressMsg
		switch k {
		case "esc":
			msg = tea.KeyPressMsg{Code: tea.KeyEscape}
		case "space":
			msg = tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
		default:
			msg = tea.KeyPressMsg{Code: rune(k[0]), Text: k}
		}
		nm, _ := m.Update(msg)
		m = nm.(sbModel)
	}

	if m.view().sel != "a:@1" {
		t.Fatalf("start: %q", m.view().sel)
	}
	press("k")
	// fix-auth's background agent sits between it and kelsier
	if sel := m.view().sel; sel != "b:bg1" {
		t.Errorf("k from kelsier: %q, want fix-auth's background agent above it", sel)
	}
	if sbLoadShared().Selected != "b:bg1" {
		t.Error("the cursor is not shared through sidebar.json")
	}

	// space on the current space folds it, and the fold is shared
	m.ui.sel = spaceID("/home/me/code/kelsier")
	press("space")
	for _, s := range m.view().spaces {
		if s.key == "/home/me/code/kelsier" && s.open {
			t.Error("space did not fold the current space")
		}
	}
	if sbLoadShared().Expanded["/home/me/code/kelsier"] != false {
		t.Error("the fold is not shared")
	}

	// / filters live; esc clears
	press("/")
	for _, c := range "usage" {
		press(string(c))
	}
	if n := len(m.view().agents); n != 1 {
		t.Errorf("filter usage: %d agents", n)
	}
	press("esc")
	if m.ui.filter != "" || m.mode != modeNormal {
		t.Error("esc did not clear the filter")
	}

	// x asks first, and anything but y leaves the agent alone
	m.ui.sel = "a:@2"
	press("x")
	if m.mode != modeKill || !strings.Contains(plain(m.render()), "kill fix-auth?") || !strings.Contains(plain(m.render()), "y yes · n no") {
		t.Errorf("x did not ask:\n%s", plain(m.render()))
	}
	press("n")
	if m.mode != modeNormal {
		t.Error("n did not cancel the kill")
	}

	// r and x need an agent
	m.ui.sel = spaceID("/home/me/code/api-gw")
	press("r")
	if m.mode != modeNormal || !strings.Contains(m.msg, "select an agent") {
		t.Errorf("r on a space: mode %v msg %q", m.mode, m.msg)
	}

	// a click on a space's line toggles it
	_, refs := renderView(m.view(), m.width, m.height, 0)
	y := -1
	for i, r := range refs {
		if r.id == spaceID("/home/me/code/api-gw") {
			y = i
			break
		}
	}
	nm, _ := m.Update(tea.MouseClickMsg{X: 5, Y: y, Button: tea.MouseLeft})
	m = nm.(sbModel)
	for _, s := range m.view().spaces {
		if s.key == "/home/me/code/api-gw" && !s.open {
			t.Error("a click did not open the space")
		}
	}
	if m.view().sel != spaceID("/home/me/code/api-gw") {
		t.Error("a click did not select")
	}
}

// The actions group sits at the foot when there is room, and a click on one
// is its key: Q asks to quit.
func TestSidebarActions(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	m := sbModel{doc: loadFixture(t, "one-agent"), width: 32, height: 30, visible: true}
	out, refs := renderView(m.view(), 32, 30, 0)
	if !strings.Contains(plain(out), "\nkel\n") {
		t.Fatalf("no kel actions at 32x30:\n%s", plain(out))
	}
	y := -1
	for i, r := range refs {
		if r.id == "act:q" {
			y = i
		}
	}
	if y < 0 {
		t.Fatal("no q action line")
	}
	nm, _ := m.Update(tea.MouseClickMsg{X: 2, Y: y, Button: tea.MouseLeft})
	if nm.(sbModel).mode != modeQuit {
		t.Error("a click on Q did not ask to quit")
	}
	// no room, no actions: fifteen agents fill a short sidebar
	out, _ = renderView(buildView(loadFixture(t, "fifteen-three-repos"), uiState{}), 32, 20, 0)
	if strings.Contains(plain(out), "actions") {
		t.Error("actions shown with no room for them")
	}
}

// A main checkout is named by its directory, not its window's group.
func TestSidebarSpaceLabel(t *testing.T) {
	doc := loadFixture(t, "one-agent")
	(*doc.Panes)[0].Group = "kelsier"
	(*doc.Panes)[0].Git.Root = "/home/me/code/ai-context"
	v := buildView(doc, uiState{})
	if v.spaces[0].label != "ai-context" {
		t.Errorf("label %q, want the repo's own name", v.spaces[0].label)
	}
}

func TestShortNote(t *testing.T) {
	for in, want := range map[string]string{
		"Claude needs your permission to use Bash":  "allow Bash?",
		"Claude needs your permission to use Edit.": "allow Edit?",
		"Claude is waiting for your input":          "your input",
		"something new":                             "something new",
	} {
		if got := shortNote(in); got != want {
			t.Errorf("shortNote(%q) = %q, want %q", in, got, want)
		}
	}
}

// A working agent silent for half an hour reads working?, not working.
func TestQuietWorking(t *testing.T) {
	w, d := "working", "done"
	n := func(v float64) *float64 { return &v }
	now := int64(100000)
	if quietFor(fleet.Agent{State: &w, StateSince: n(99000)}, now) != 0 {
		t.Error("17 minutes is not quiet")
	}
	if quietFor(fleet.Agent{State: &w, StateSince: n(90000), Context: &fleet.Context{At: n(99500)}}, now) != 0 {
		t.Error("a recent statusline update is not quiet")
	}
	if quietFor(fleet.Agent{State: &w, StateSince: n(50000)}, now) == 0 {
		t.Error("14 hours of silence must read quiet")
	}
	if quietFor(fleet.Agent{State: &d, StateSince: n(50000)}, now) != 0 {
		t.Error("only working can be quiet")
	}
}

// The entry under the pointer gets a dim bar; the selected one keeps its own.
func TestSidebarHover(t *testing.T) {
	doc := loadFixture(t, "fifteen-three-repos")
	_, refs := renderView(buildView(doc, uiState{}), 32, 40, 0)
	y := -1
	for i, r := range refs {
		if r.id == "a:@2" {
			y = i
			break
		}
	}
	out, _ := renderView(buildView(doc, uiState{hover: y + 1}), 32, 40, 0)
	if !strings.Contains(strings.Split(out, "\n")[y], "▏") {
		t.Errorf("hovered line has no hover bar: %q", plain(strings.Split(out, "\n")[y]))
	}
	if strings.Count(out, "▏") != 2 {
		t.Errorf("hover marks the whole entry (2 lines), got %d", strings.Count(out, "▏"))
	}
}

// A click on a prompt's choices line is the key of the choice clicked.
func TestHintKey(t *testing.T) {
	if k := hintKey("y yes · n no", 1); k.Text != "y" {
		t.Errorf("click on yes: %+v", k)
	}
	if k := hintKey("y yes · n no", 9); k.Text != "n" {
		t.Errorf("click on no: %+v", k)
	}
	if k := hintKey("enter ok · esc cancel", 14); k.Code != tea.KeyEscape {
		t.Errorf("click on cancel: %+v", k)
	}
	if keyWords("C-b") != "Ctrl-b" || keyWords("M-a") != "Alt-a" {
		t.Error("the prefix must read in words")
	}
}

// The quota line gives up what does not fit, least needed first, and says
// nothing when nothing was reported — never a made-up 0%.
func TestQuotaLine(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	q := &fleet.Quota{
		FiveHour: &fleet.Window{Pct: f(27), ResetsAt: f(1000 + 12480)},
		SevenDay: &fleet.Window{Pct: f(51)},
		Spend:    &fleet.Window{Pct: f(20)},
	}
	if got := plain(quotaLine(q, 32, 1000)); got != "5h █████ 27% 3h28m · wk 51%" {
		t.Errorf("at 32: %q", got)
	}
	if got := plain(quotaLine(q, 24, 1000)); got != "5h █████ 27% · wk 51%" {
		t.Errorf("at 24: %q", got)
	}
	if got := plain(quotaLine(q, 18, 1000)); got != "5h █████ 27%" {
		t.Errorf("at 18: %q", got)
	}
	if quotaLine(nil, 32, 1000) != "" {
		t.Error("no quota reported must be no line")
	}
	if got := plain(quotaLine(&fleet.Quota{FiveHour: &fleet.Window{}}, 32, 1000)); got != "5h ?" {
		t.Errorf("unknown 5h: %q", got)
	}
	q.Spend.Pct = f(60)
	if got := plain(quotaLine(q, 40, 1000)); !strings.HasSuffix(got, "· spend 60%") {
		t.Errorf("spend from 50%%: %q", got)
	}
}

// n / w take a name, a directory, or both: no shell needed to cd first.
func TestParseNew(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "code", "My App"), 0o755); err != nil {
		t.Fatal(err)
	}
	cases := []struct{ in, name, dir, err string }{
		{"fix", "fix", "", ""},
		{"~/code/My App", "", "", "type a name, a directory, or both"},
		{"~/code", "code", filepath.Join(home, "code"), ""},
		{"fix ~/code", "fix", filepath.Join(home, "code"), ""},
		{"fix ~/nope", "", "", "no such directory: ~/nope"},
		{"a b c", "", "", "type a name, a directory, or both"},
	}
	t.Setenv("HOME", home)
	for _, c := range cases {
		n, d, e := parseNew(c.in, home)
		if n != c.name || d != c.dir || e != c.err {
			t.Errorf("parseNew(%q) = %q %q %q", c.in, n, d, e)
		}
	}
	if slugify("My App") != "My-App" || slugify("...") != "agent" {
		t.Error("slugify")
	}
}

func TestCompleteDir(t *testing.T) {
	home := t.TempDir()
	for _, d := range []string{"code/kelsier", "code/kel-old", "code/api", ".config"} {
		_ = os.MkdirAll(filepath.Join(home, d), 0o755)
	}
	_ = os.WriteFile(filepath.Join(home, "code", "kfile"), nil, 0o644)
	cases := []struct{ in, out, cands string }{
		{"fix ~/co", "fix ~/code/", ""},
		{"~/code/a", "~/code/api/", ""},
		{"~/code/k", "~/code/kel", "kel-old kelsier"},
		{"~/code/z", "~/code/z", "no directory matches"},
		{"~/", "~/code/", ""}, // dotfiles only when asked for
		{"~/.c", "~/.config/", ""},
		{"fix ", "fix ~/", ""},
	}
	for _, c := range cases {
		out, cands := completeDir(c.in, home)
		if out != c.out || cands != c.cands {
			t.Errorf("completeDir(%q) = %q %q", c.in, out, cands)
		}
	}
}

// A click on a key in the keys list presses it; a row that is no single
// action closes the list and does nothing else.
func TestHelpClick(t *testing.T) {
	m := sbModel{help: true, width: 32, height: 40}
	row := func(k string) int {
		for i, h := range sbHelp {
			if h[0] == k {
				return i + 1
			}
		}
		t.Fatalf("no row %q", k)
		return 0
	}
	click := func(y int) sbModel {
		out, _ := m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, Y: y})
		return out.(sbModel)
	}
	if got := click(row("n")); got.help || got.mode != modeNew {
		t.Errorf("n row: help %v mode %v", got.help, got.mode)
	}
	if got := click(row("/")); got.mode != modeFilter {
		t.Errorf("/ row: mode %v", got.mode)
	}
	if got := click(row("j k")); got.help || got.mode != modeNormal {
		t.Errorf("j k row should only close: help %v mode %v", got.help, got.mode)
	}
	if helpKey("  k") != ";" || helpKey("  q") != "" || helpKey("esc") != "" || helpKey("n name dir") != "n" {
		t.Error("helpKey")
	}
}

// Phase 2: a repo's worktrees follow it, open or not; a nested worktree's
// agents belong to the repo, not to a group named after the branch.
func TestWorktreesNest(t *testing.T) {
	v := buildView(loadFixture(t, "worktrees"), uiState{})
	var order []string
	for _, s := range v.spaces {
		order = append(order, s.label)
	}
	got := strings.Join(order, " ")
	if !strings.Contains(got, "kelsier agent-af1a1567ab46ddb66 docs fix-auth gone old-ui") {
		t.Errorf("kelsier's worktrees do not follow it: %s", got)
	}
	for _, s := range v.spaces {
		switch s.label {
		case "old-ui":
			if !s.closed || s.repoName() != "kelsier" || s.land.Code != "merged" {
				t.Errorf("old-ui: %+v", s)
			}
			ui := uiState{focused: true, sel: spaceID(s.key)}
			if h := hint(buildView(loadFixture(t, "worktrees"), ui), 40); !strings.HasPrefix(h, "s sweep") {
				t.Errorf("a merged worktree's hint: %q", h)
			}
		case "fix-auth":
			if s.closed || s.title() != "kelsier/fix-auth" {
				t.Errorf("fix-auth: closed %v title %q", s.closed, s.title())
			}
		case "notes":
			if !s.closed || spaceDetail(s) != "main · no window" {
				t.Errorf("a repo known only from a worktree: %q", spaceDetail(s))
			}
		}
	}
}

// The keys list shows the hover bar on a key a click would run, and only there.
func TestHelpHover(t *testing.T) {
	m := sbModel{help: true, width: 32, height: 40}
	for i, h := range sbHelp {
		m.ui.hover = i + 2
		line := strings.Split(plain(m.render()), "\n")[i+1]
		if bar := strings.HasPrefix(line, "▏"); bar != (helpKey(h[0]) != "") {
			t.Errorf("row %q hovered: %q", h[0], line)
		}
	}
}

// A long worktree name gives way to its state, at any width.
func TestWorktreeRowKeepsState(t *testing.T) {
	for _, w := range []int{28, 32, 40} {
		out, _ := renderView(buildView(loadFixture(t, "worktrees"), uiState{}), w, 0, 0)
		found := false
		for _, l := range strings.Split(plain(out), "\n") {
			if strings.Contains(l, "agent-af1a") {
				found = true
				if !strings.HasSuffix(l, "●3") || len([]rune(l)) > w {
					t.Errorf("at %d: %q", w, l)
				}
			}
		}
		if !found {
			t.Errorf("at %d: no Claude worktree row", w)
		}
	}
}

// A click elsewhere ends a prompt as esc would, then does what it would have
// done; leaving the sidebar ends it too. The author was stuck typing a name.
func TestPromptClickOff(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	m := sbModel{doc: loadFixture(t, "one-agent"), width: 32, height: 30, visible: true}
	nm, _ := m.key(tea.KeyPressMsg{Code: 'n', Text: "n"})
	m = nm.(sbModel)
	if m.mode != modeNew || m.view().promptTitle == "" {
		t.Fatalf("n: mode %v title %q", m.mode, m.view().promptTitle)
	}
	_, refs := renderView(m.view(), 32, 30, 0)
	y := -1
	for i, r := range refs {
		if r.kind == "agent" {
			y = i
		}
	}
	nm, _ = m.Update(tea.MouseClickMsg{X: 4, Y: y, Button: tea.MouseLeft})
	if got := nm.(sbModel); got.mode != modeNormal || got.input != "" {
		t.Errorf("a click on an agent left the prompt open: %v %q", got.mode, got.input)
	}
	m.mode, m.input = modeNormal, ""
	nm, _ = m.key(tea.KeyPressMsg{Code: ';', Text: ";"})
	m = nm.(sbModel)
	if m.mode != modeCommand {
		t.Fatalf("; did not open the command prompt")
	}
	nm, _ = m.Update(tea.BlurMsg{})
	if nm.(sbModel).mode != modeNormal {
		t.Error("leaving the sidebar left the prompt open")
	}
}

// The action grid: each half of a line is its own button.
func TestActionGrid(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	m := sbModel{doc: loadFixture(t, "one-agent"), width: 32, height: 30, visible: true, ui: uiState{focused: true}}
	out, refs := renderView(m.view(), 32, 30, 0)
	y := -1
	for i, r := range refs {
		if r.id == "act:n" {
			y = i
		}
	}
	if y < 0 || refs[y].id2 != "act:w" {
		t.Fatalf("no n|w line:\n%s", plain(out))
	}
	nm, _ := m.Update(tea.MouseClickMsg{X: 20, Y: y, Button: tea.MouseLeft})
	if nm.(sbModel).mode != modeNewWorktree {
		t.Error("a click on the right half did not press w")
	}
	nm, _ = m.Update(tea.MouseClickMsg{X: 2, Y: y, Button: tea.MouseLeft})
	if nm.(sbModel).mode != modeNew {
		t.Error("a click on the left half did not press n")
	}
	// hover marks the half under the pointer
	m.ui.hover, m.ui.hoverX = y+1, 20
	out, _ = renderView(m.view(), 32, 30, 0)
	if l := strings.Split(plain(out), "\n")[y]; !strings.Contains(l, "▏w") || strings.HasPrefix(l, "▏") {
		t.Errorf("hover on the right half: %q", l)
	}
}

// A working agent says what it is doing; its tag is the repo it stands in.
func TestAgentDoing(t *testing.T) {
	doc := loadFixture(t, "one-agent")
	doc.Agents[0].Doing = sp("editing sbview.go")
	out, _ := renderView(buildView(doc, uiState{}), 32, 0, 0)
	if !strings.Contains(plain(out), "   editing sbview.go") {
		t.Errorf("no doing line:\n%s", plain(out))
	}
}

func sp(s string) *string { return &s }

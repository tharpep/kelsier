package main

import (
	"encoding/json"
	"fmt"
	"os/exec"
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
				t.Fatalf("%s: %v (run: go test ./cmd/kel-sidebar -run Golden -update, after reviewing the frames)", gold, err)
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
			// a line wider than the pane is one tmux wraps onto the next
			for _, l := range strings.Split(plain(out), "\n") {
				if w > 0 && len([]rune(l)) > w {
					t.Errorf("%dx%d: %q is wider than the pane", w, h, l)
				}
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
	// a fixed order: repos by name, agents in the order their windows were made
	if got := strings.Join(names, " "); got != "gateway rate-limits api-auth kelsier fix-auth docs dashboard usage-charts spike" {
		t.Errorf("agents in a fixed order: got %s", got)
	}
	if v.sel != "a:@1" {
		t.Errorf("default selection: got %q, want the current agent", v.sel)
	}

	if v.reposOpen || v.indexOf(spaceID("/home/me/code/api-gw")) >= 0 {
		t.Error("Repos is folded by default: no space is selectable")
	}
	v = buildView(loadFixture(t, "fifteen-three-repos"), uiState{expanded: map[string]bool{reposKey: true}})
	if !v.reposOpen || v.indexOf(spaceID("/home/me/code/api-gw")) < 0 {
		t.Error("sec:repos opens Repos")
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
	if !strings.Contains(lines[len(lines)-1], "Settings") {
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
	for range 4 {
		press("j")
	}
	// kelsier, its nvim pane, fix-auth, its bash pane, then fix-auth's background agent
	if sel := m.view().sel; sel != "b:bg1" {
		t.Errorf("j x4 from kelsier: %q, want fix-auth's background agent", sel)
	}
	if sbLoadShared().Selected != "b:bg1" {
		t.Error("the cursor is not shared through sidebar.json")
	}

	// space on the Repos header opens it, and the fold is shared
	m.ui.sel = reposID
	press("space")
	if !m.view().reposOpen || !sbLoadShared().Expanded[reposKey] {
		t.Error("space did not open Repos, or the fold is not shared")
	}
	press("space")
	if m.view().reposOpen || sbLoadShared().Expanded[reposKey] {
		t.Error("space did not fold Repos again")
	}
	m.ui.expanded = map[string]bool{reposKey: true}

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

	m.ui.sel = spaceID("/home/me/code/kelsier")
	press("l")
	m.ui.sel = "%2"
	press("x")
	if m.mode != modeClose || !strings.Contains(plain(m.render()), "close nvim?") {
		t.Errorf("x on a program pane did not ask:\n%s", plain(m.render()))
	}
	press("n")
	if m.mode != modeNormal {
		t.Error("n did not cancel the close")
	}

	// r and x need an agent
	m.ui.sel = spaceID("/home/me/code/api-gw")
	press("r")
	if m.mode != modeNormal || !strings.Contains(m.msg, "select an agent") {
		t.Errorf("r on a space: mode %v msg %q", m.mode, m.msg)
	}

	// a click on a space selects it
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
	if m.view().sel != spaceID("/home/me/code/api-gw") {
		t.Error("a click did not select")
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
		"API Error: Rate limit reached":             "Rate limit reached",
		"something new":                             "something new",
	} {
		if got := shortNote(in); got != want {
			t.Errorf("shortNote(%q) = %q, want %q", in, got, want)
		}
	}
}

// An agent whose turn ended on an API error is red, says error, says why,
// and stays where it was in the list.
func TestErrorAgent(t *testing.T) {
	doc := loadFixture(t, "fifteen-three-repos")
	st, note := "error", "API Error: Rate limit reached"
	for i := range doc.Agents {
		if doc.Agents[i].Name == "gateway" {
			doc.Agents[i].State, doc.Agents[i].Note = &st, &note
		}
	}
	v := buildView(doc, uiState{})
	before := buildView(loadFixture(t, "fifteen-three-repos"), uiState{})
	for i := range v.agents {
		if v.agents[i].agent.Name != before.agents[i].agent.Name {
			t.Fatalf("an errored agent moved the list: %d is %s, was %s", i, v.agents[i].agent.Name, before.agents[i].agent.Name)
		}
	}
	out, _ := renderView(v, 32, 0, 0)
	if !strings.Contains(out, red+"●") || !strings.Contains(plain(out), "▐error▌") || !strings.Contains(plain(out), "Rate limit reached") {
		t.Errorf("error agent not drawn as one:\n%s", plain(out))
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
	out, _ := renderView(buildView(doc, uiState{hover: y + 1, hoverID: "a:@2"}), 32, 40, 0)
	soft := lifted.mix(baseBg, 0.25).bg()
	if !strings.HasPrefix(strings.Split(out, "\n")[y], soft) {
		t.Errorf("hovered line is not lifted: %q", plain(strings.Split(out, "\n")[y]))
	}
	// its two lines, one lifted unit
	lit := 0
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, soft) && strings.TrimSpace(strings.TrimPrefix(plain(l), "▏")) != "" {
			lit++
		}
	}
	if lit != 2 {
		t.Errorf("hover lifts the whole entry (2 lines), got %d", lit)
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
	// a choices line too wide for the sidebar wraps; no choice is lost
	got := wrapHint("enter window · | right · - down · esc", 30)
	if strings.Join(got, " · ") != "enter window · | right · - down · esc" || len(got) != 2 {
		t.Errorf("wrapped choices: %q", got)
	}
	if k := hintKey("  "+got[1], 3); k.Text != "-" {
		t.Errorf("click on the wrapped line's first choice: %+v", k)
	}
	if keyWords("C-b") != "Ctrl-b" || keyWords("M-a") != "Alt-a" {
		t.Error("the prefix must read in words")
	}
}

// n / w take nothing, a name, a directory, or both: no shell needed to cd
// first, and no name needed at all.
func TestParseNew(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "code", "My App"), 0o755); err != nil {
		t.Fatal(err)
	}
	cases := []struct{ in, name, dir, err string }{
		{"", "", "", ""},
		{"fix", "fix", "", ""},
		{"~/code/My App", "", "", "type a name, a directory, or both"},
		{"~/code", "", filepath.Join(home, "code"), ""},
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
			// kelsier folds its idle worktrees; old-ui is selectable only open
			ui := uiState{focused: true, sel: spaceID(s.key), expanded: map[string]bool{reposKey: true, wtKey("/home/me/code/kelsier"): true}}
			if acts := entryMenu(buildView(loadFixture(t, "worktrees"), ui)); len(acts) < 2 || acts[1].key != "s" {
				t.Errorf("a merged worktree's menu does not lead with sweep: %+v", acts)
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

// A repo with more than three worktrees folds them behind one summary row; the
// row opens the list and closes it again. The frames are plain text.
func TestWorktreeFold(t *testing.T) {
	t.Setenv("HOME", "/home/me")
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	const repo = "/home/me/code/orca"
	doc := loadFixture(t, "folds/many-worktrees")
	frame := func(ui uiState) string {
		out, _ := renderView(buildView(doc, ui), 32, 0, 0)
		return plain(out) + "\n"
	}
	check := func(name, got string) {
		t.Helper()
		gold := filepath.Join(fixtures, "folds", "many-worktrees."+name+".32.golden")
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
			t.Errorf("%s changed:\n--- got\n%s\n--- want\n%s", name, got, want)
		}
	}
	check("folded", frame(uiState{expanded: map[string]bool{reposKey: true}}))
	check("expanded", frame(uiState{expanded: map[string]bool{reposKey: true, wtKey(repo): true}}))

	// enter on the summary row (a click is the same call) flips the fold, in the
	// shared map like any other, and keeps the cursor on the row
	m := sbModel{doc: doc, width: 32, height: 30, visible: true, ui: uiState{expanded: map[string]bool{reposKey: true}}}
	m.ui.sel = wtID(repo)
	if v := m.view(); v.indexOf(wtID(repo)) < 0 {
		t.Fatalf("the summary row is not selectable: %v", v.entryIDs)
	}
	m.activate(wtID(repo))
	if !m.ui.expanded[wtKey(repo)] || m.ui.sel != wtID(repo) {
		t.Errorf("after enter: expanded %v, sel %q", m.ui.expanded, m.ui.sel)
	}
	if v := m.view(); v.indexOf(spaceID("/home/me/code/orca-a")) < 0 {
		t.Errorf("an open list should select worktrees: %v", v.entryIDs)
	}
	m.activate(wtID(repo))
	if m.ui.expanded[wtKey(repo)] {
		t.Errorf("a second enter should fold again: %v", m.ui.expanded)
	}

	// three worktrees do not fold
	three := doc
	three.Repos = &[]fleet.Repo{(*doc.Repos)[0]}
	(*three.Repos)[0].Worktrees = (*three.Repos)[0].Worktrees[:3]
	for _, s := range buildView(three, uiState{}).spaces {
		if s.folded || s.wtSummary() {
			t.Errorf("%s: three worktrees folded", s.label)
		}
	}
}

// A worktree with an agent in it stays on screen while its repo is folded.
func TestWorktreeFoldKeepsAgents(t *testing.T) {
	v := buildView(loadFixture(t, "worktrees"), uiState{})
	shown := map[string]bool{}
	for _, s := range v.spaces {
		shown[s.label] = !s.folded
	}
	for label, want := range map[string]bool{"docs": true, "fix-auth": true, "agent-af1a1567ab46ddb66": false, "old-ui": false, "gone": false} {
		if shown[label] != want {
			t.Errorf("%s shown=%v, want %v", label, shown[label], want)
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
		ui := uiState{expanded: map[string]bool{reposKey: true, wtKey("/home/me/code/kelsier"): true}}
		out, _ := renderView(buildView(loadFixture(t, "worktrees"), ui), w, 0, 0)
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
	// past the last choice on the choices line is anywhere else too
	_, refs = renderView(m.view(), 32, 30, 0)
	for i, r := range refs {
		if r.kind == "hint2" {
			y = i
		}
	}
	nm, _ = m.Update(tea.MouseClickMsg{X: 31, Y: y, Button: tea.MouseLeft})
	if got := nm.(sbModel); got.mode != modeNormal {
		t.Errorf("a click past the choices left the prompt open: %v", got.mode)
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


// A working agent says what it is doing; its tag is the repo it stands in.
func TestAgentDoing(t *testing.T) {
	doc := loadFixture(t, "one-agent")
	doc.Agents[0].Doing = sp("editing sbview.go")
	out, _ := renderView(buildView(doc, uiState{}), 32, 0, 0)
	if !strings.Contains(plain(out), "   editing sbview.go") {
		t.Errorf("no doing line:\n%s", plain(out))
	}
	// and on a working agent that is not selected, in dim
	doc = loadFixture(t, "fifteen-three-repos")
	st := "working"
	for i := range doc.Agents {
		if doc.Agents[i].Name == "usage-charts" {
			doc.Agents[i].Doing, doc.Agents[i].State = sp("running the tests"), &st
		}
	}
	out, _ = renderView(buildView(doc, uiState{sel: "a:@1"}), 32, 0, 0)
	if !strings.Contains(out, dim+"running the tests") {
		t.Errorf("an unselected working agent shows no doing line:\n%s", plain(out))
	}
}

func TestAgentMode(t *testing.T) {
	for in, want := range map[string]string{"plan": "plan", "acceptEdits": "edits", "bypassPermissions": "bypass", "dontAsk": "dontask", "Auto": "auto", "default": "", "": ""} {
		if got := modeWord(&in); got != want {
			t.Errorf("modeWord(%q) = %q, want %q", in, got, want)
		}
	}
	if modeWord(nil) != "" {
		t.Error("an agent with no mode shows none")
	}
	doc := loadFixture(t, "one-agent")
	doc.Agents[0].Mode = sp("acceptEdits")
	out, _ := renderView(buildView(doc, uiState{}), 32, 0, 0)
	if !strings.Contains(plain(out), "kelsier edits") || !strings.Contains(out, faint+"edits") {
		t.Errorf("no faint mode after the name:\n%s", plain(out))
	}
	doc.Agents[0].Mode = nil
	out, _ = renderView(buildView(doc, uiState{}), 32, 0, 0)
	if strings.Contains(plain(out), "edits") || strings.Contains(plain(out), "plan") {
		t.Errorf("a null mode drew something:\n%s", plain(out))
	}
	doc.Agents[0].Name = "a-long-agent-name-that-cannot-fit"
	doc.Agents[0].Mode = sp("bypassPermissions")
	out, _ = renderView(buildView(doc, uiState{}), 28, 0, 0)
	bare := doc
	bare.Agents = append([]fleet.Agent(nil), doc.Agents...)
	bare.Agents[0].Mode = nil
	outBare, _ := renderView(buildView(bare, uiState{}), 28, 0, 0)
	if plain(out) != plain(outBare) {
		t.Errorf("a name that cannot fit whole must lose the mode, not width:\n%s\nwant\n%s", plain(out), plain(outBare))
	}
	doc = loadFixture(t, "fifteen-three-repos")
	out, _ = renderView(buildView(doc, uiState{}), 32, 0, 0)
	if !strings.Contains(plain(out), "● fix-auth ") || strings.Contains(plain(out), "fix-a…") && strings.Contains(plain(out), "plan") {
		t.Errorf("name, age and chip come before the mode:\n%s", plain(out))
	}
}

func TestAgentTasks(t *testing.T) {
	doc := loadFixture(t, "fifteen-three-repos")
	out, _ := renderView(buildView(doc, uiState{sel: "a:@2"}), 32, 0, 0)
	p := plain(out)
	if !regexp.MustCompile(`editing UsageChart\.tsx +1/4 *$`).MatchString(strings.Split(p, "\n")[findRow(p, "editing Usage")]) || !strings.Contains(out, dim+"1/4") {
		t.Errorf("no dim 1/4 right-aligned on the summary line:\n%s", p)
	}
	doc = loadFixture(t, "one-agent")
	out, _ = renderView(buildView(doc, uiState{sel: "a:@1"}), 32, 0, 0)
	p = plain(out)
	if !regexp.MustCompile(`\$1\.20 +3/7 `).MatchString(p) || strings.Count(p, "3/7") != 1 {
		t.Errorf("the selected block's context line lacks the count, once:\n%s", p)
	}
	doc.Agents[0].Tasks = nil
	out, _ = renderView(buildView(doc, uiState{sel: "a:@1"}), 32, 0, 0)
	if strings.Contains(plain(out), "/7") {
		t.Errorf("no tasks drew a count:\n%s", plain(out))
	}
}

func findRow(s, sub string) int {
	for i, l := range strings.Split(s, "\n") {
		if strings.Contains(l, sub) {
			return i
		}
	}
	return 0
}

func sp(s string) *string { return &s }

// d asks which base; tab completes a branch.
func TestCompleteBranch(t *testing.T) {
	d := t.TempDir()
	for _, a := range [][]string{{"init", "-q", "-b", "main"}, {"commit", "-q", "--allow-empty", "-m", "i"}, {"branch", "feat-a"}, {"branch", "feat-b"}} {
		c := exec.Command("git", append([]string{"-c", "user.email=t@t", "-c", "user.name=t"}, a...)...)
		c.Dir = d
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("%v %s", err, out)
		}
	}
	if got, c := completeBranch("ma", d); got != "main" || c != "" {
		t.Errorf("ma: %q %q", got, c)
	}
	if got, c := completeBranch("f", d); got != "feat-" || c != "feat-a feat-b" {
		t.Errorf("f: %q %q", got, c)
	}
}

// ;'s history keeps the last twenty, without repeats in a row.
func TestPromptHistory(t *testing.T) {
	sd := t.TempDir()
	t.Setenv("XDG_STATE_HOME", sd)
	_ = os.MkdirAll(filepath.Join(sd, "kel"), 0o700) // kel made it long before
	for i := 0; i < 25; i++ {
		saveHistory(fmt.Sprintf("cmd%d", i))
	}
	saveHistory("cmd24")
	h := loadHistory()
	if len(h) != 20 || h[0] != "cmd5" || h[19] != "cmd24" {
		t.Errorf("history: %v", h)
	}
	m := sbModel{mode: modeCommand, width: 32, height: 30}
	nm, _ := m.key(tea.KeyPressMsg{Code: tea.KeyUp})
	if nm.(sbModel).input != "cmd24" {
		t.Errorf("up: %q", nm.(sbModel).input)
	}
}

func TestTwoAgentsOneWindow(t *testing.T) {
	sp := func(s string) *string { return &s }
	st := "working"
	doc := fleet.Doc{
		Agents: []fleet.Agent{
			{Group: "kelsier", Name: "a", WindowID: sp("@1"), PaneID: sp("%1"), Managed: true, State: &st},
			{Group: "kelsier", Name: "a-3", WindowID: sp("@1"), PaneID: sp("%3"), State: &st},
		},
		Panes: &[]fleet.Pane{
			{PaneID: "%1", WindowID: "@1", Cmd: "claude", Kind: "agent", Agent: true, Cwd: "/x"},
			{PaneID: "%3", WindowID: "@1", Cmd: "claude", Kind: "agent", Agent: true, Cwd: "/x"},
		},
	}
	v := buildView(doc, uiState{})
	if len(v.agents) != 2 {
		t.Fatalf("%d agents, want 2", len(v.agents))
	}
	if agentID(v.agents[0].key) == agentID(v.agents[1].key) || v.agents[0].pane == v.agents[1].pane {
		t.Errorf("the two agents share an id or a pane: %+v", v.agents)
	}
}

func TestSidebarRelaunchDeadAgent(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	doc := loadFixture(t, "dead-agent")
	// a:@2 is the dead agent ("crashed")
	vDead := buildView(doc, uiState{focused: true, sel: "a:@2"})
	_, _, actsDead := selActions(vDead)
	hasRelaunch := false
	for _, a := range actsDead {
		if a.key == "R" && a.label == "relaunch" {
			hasRelaunch = true
			break
		}
	}
	if !hasRelaunch {
		t.Errorf("dead agent actions missing R relaunch: %+v", actsDead)
	}
	if acts := entryMenu(vDead); len(acts) < 3 || acts[2].key != "R" {
		t.Errorf("a dead agent's menu does not lead with relaunch, after Go to it and Peek: %+v", acts)
	}

	// a:@1 is live ("kelsier")
	vLive := buildView(doc, uiState{focused: true, sel: "a:@1"})
	_, _, actsLive := selActions(vLive)
	for _, a := range actsLive {
		if a.key == "R" {
			t.Errorf("live agent actions unexpectedly has R: %+v", actsLive)
		}
	}

	// pressing R triggers relaunch
	m := sbModel{doc: doc, width: 32, height: 30, visible: true, ui: uiState{focused: true, sel: "a:@2"}}
	nm, cmd := m.Update(tea.KeyPressMsg{Code: rune('R'), Text: "R"})
	if nm.(sbModel).msg != "relaunching crashed…" {
		t.Errorf("got msg %q, want 'relaunching crashed…'", nm.(sbModel).msg)
	}
	if cmd == nil {
		t.Error("pressing R did not return a tea.Cmd")
	}

	// pressing R on live agent warns that it is not dead
	mLive := sbModel{doc: doc, width: 32, height: 30, visible: true, ui: uiState{focused: true, sel: "a:@1"}}
	nmLive, cmdLive := mLive.Update(tea.KeyPressMsg{Code: rune('R'), Text: "R"})
	if nmLive.(sbModel).msg != "kelsier is not dead" || !nmLive.(sbModel).msgBad {
		t.Errorf("live agent R msg: got %q (bad=%v), want 'kelsier is not dead'", nmLive.(sbModel).msg, nmLive.(sbModel).msgBad)
	}
	if cmdLive != nil {
		t.Error("pressing R on live agent should not return a tea.Cmd")
	}

	// right-click the dead agent, then Relaunch in its menu
	nm, _ = m.Update(tea.MouseClickMsg{X: 4, Y: rowOf(t, m, m.ui.sel), Button: tea.MouseRight})
	m = nm.(sbModel)
	nmClick, cmdClick := m.Update(tea.MouseClickMsg{X: 4, Y: rowOf(t, m, "dr:R"), Button: tea.MouseLeft})
	if nmClick.(sbModel).msg != "relaunching crashed…" {
		t.Errorf("Relaunch in the menu: got msg %q", nmClick.(sbModel).msg)
	}
	if cmdClick == nil {
		t.Error("Relaunch in the menu did not return a tea.Cmd")
	}
}

// pinnedEnd is the first line under the kel header, its blank and the pinned
// Needs you block (header, rows, blank) when there is one.
func pinnedEnd(lines []string) int {
	if len(lines) > 2 && strings.HasPrefix(lines[2], "  Needs you") {
		for i := 3; i < len(lines); i++ {
			if lines[i] == "" {
				return i + 1
			}
		}
	}
	return 2
}

func TestStickyHeaders(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	// 2. Sticky Section Headers
	fifteenDoc := loadFixture(t, "fifteen-three-repos")

	// Select last agent "spike" (@8) at tight height h=16
	vSpike := buildView(fifteenDoc, uiState{focused: true, sel: "a:@8"})
	outSpike, _ := renderView(vSpike, 32, 16, 0)
	linesSpike := strings.Split(plain(outSpike), "\n")
	if i := pinnedEnd(linesSpike); len(linesSpike) <= i || linesSpike[i] != "  Agents" {
		t.Errorf("sticky header for scrolled agents: want Agents under the pinned block, got %q", linesSpike)
	}
	if !strings.Contains(plain(outSpike), "spike") {
		t.Errorf("selected agent spike was scrolled off screen")
	}

	// Select a space that scrolls past the top at h=16
	vSpace := buildView(fifteenDoc, uiState{focused: true, sel: spaceID("/home/me/code/web-dashboard"), expanded: map[string]bool{reposKey: true}})
	outSpace, _ := renderView(vSpace, 32, 16, 0)
	linesSpace := strings.Split(plain(outSpace), "\n")
	if i := pinnedEnd(linesSpace); len(linesSpace) <= i || !strings.HasPrefix(linesSpace[i], "  Repos ▾") {
		t.Errorf("sticky header for scrolled spaces: want Repos under the pinned block, got %q", linesSpace)
	}
}

// A scrolled list pins the header of the section it shows, and never a
// second header above the real one.
func TestStickyHeaderNamesTheShownSection(t *testing.T) {
	doc := loadFixture(t, "fifteen-three-repos")
	for _, id := range buildView(doc, uiState{}).entryIDs {
		for h := 8; h <= 24; h++ {
			out, _ := renderView(buildView(doc, uiState{focused: true, sel: id}), 32, h, 0)
			l := strings.Split(plain(out), "\n")
			for i := 0; i < 10 && i+1 < len(l); i++ {
				if l[i] == "  Spaces" && (l[i+1] == "  Agents" || (l[i+1] == "" && i+2 < len(l) && l[i+2] == "  Agents")) {
					t.Errorf("sel %s at h=%d: spaces pinned above agents:\n%s", id, h, strings.Join(l[:4], "\n"))
				}
			}
		}
	}
}

// The sidebar's surfaces come from the terminal's colours: on a navy theme it
// is a darker navy, the selection a lighter one, never a neutral grey.
func TestPaletteFollowsTheTerminal(t *testing.T) {
	keepPalette(t)
	navy := rgb{0x1e, 0x1e, 0x2e}
	applyPalette(navy, rgb{0xcd, 0xd6, 0xf4})
	if !(baseBg.r < navy.r && baseBg.b < navy.b && baseBg.b > baseBg.r) {
		t.Errorf("surface %s is not a darker navy than %s", baseBg.hex(), navy.hex())
	}
	if raised != navy.mix(rgb{255, 255, 255}, 0.08).bg() {
		t.Errorf("selection is not a lighter shade of the terminal's background")
	}
	light := rgb{0xef, 0xf1, 0xf5}
	applyPalette(light, rgb{0x4c, 0x4f, 0x69})
	if baseBg.dark() || raised != light.mix(rgb{}, 0.10).bg() {
		t.Errorf("a light theme gets a dark sidebar: %s", baseBg.hex())
	}
}

// Every cell of a frame carries the sidebar's own colours, out to the width
// and down to the height, so none shows the terminal's through.
func TestPaintFrameCoversEveryCell(t *testing.T) {
	out := paintFrame("ab\n"+dim+"c"+reset+"d", 6, 4)
	lines := strings.Split(out, "\n")
	if len(lines) != 4 {
		t.Fatalf("%d lines, want 4", len(lines))
	}
	for i, l := range lines {
		if !strings.HasPrefix(l, base) || visWidth(l) != 6 {
			t.Errorf("line %d not painted to width: %q", i, l)
		}
		if strings.Contains(strings.TrimSuffix(l, reset), reset) && !strings.Contains(l, reset+base) {
			t.Errorf("line %d drops to the terminal's colours after a reset: %q", i, l)
		}
	}
}

// keepPalette puts the colours back as they were, so the golden frames do not
// depend on which test ran first.
func keepPalette(t *testing.T) {
	bb, bf, b, d, m, f, r, dr := baseBg, baseFg, base, dim, mid, faint, raised, drawerBg
	li, lt := lifted, lit
	y, rd, g, p := yellowBg, redBg, greenBg, purpleBg
	t.Cleanup(func() {
		baseBg, baseFg, base, dim, mid, faint, raised, drawerBg = bb, bf, b, d, m, f, r, dr
		lifted, lit = li, lt
		yellowBg, redBg, greenBg, purpleBg = y, rd, g, p
	})
}

// rowOf is the screen row whose entry is id.
func rowOf(t *testing.T, m sbModel, id string) int {
	t.Helper()
	_, refs := renderView(m.view(), m.width, m.height, 0)
	for i, r := range refs {
		if r.id == id || r.id2 == id {
			return i
		}
	}
	t.Fatalf("no row for %q", id)
	return -1
}

// The foot is Settings and Help, each a click, focused or not, whatever
// waits.
func TestSidebarFooter(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	doc := loadFixture(t, "one-agent")
	for _, ui := range []uiState{{}, {focused: true}} {
		out, refs := renderView(buildView(doc, ui), 32, 30, 0)
		l := strings.Split(plain(out), "\n")
		last, r := l[len(l)-1], refs[len(refs)-1]
		if !strings.HasPrefix(last, "  ≡ Settings") || !strings.HasSuffix(last, "? Help") {
			t.Errorf("footer: %q", last)
		}
		if r.at(4) != "open:settings" || r.at(29) != "open:help" || r.at(0) != "" {
			t.Errorf("footer clicks: %+v", r)
		}
	}
	m := sbModel{doc: doc, width: 32, height: 30, visible: true}
	nm, _ := m.Update(tea.MouseClickMsg{X: 29, Y: 29, Button: tea.MouseLeft})
	if !nm.(sbModel).help {
		t.Error("Help did not open the keys")
	}
	// someone waiting changes nothing here: the alarm lives on tmux's bar
	out, _ := renderView(buildView(loadFixture(t, "fifteen-three-repos"), uiState{}), 32, 30, 0)
	if l := strings.Split(plain(out), "\n"); !strings.HasPrefix(l[len(l)-1], "  ≡ Settings") {
		t.Errorf("foot while an agent waits: %q", l[len(l)-1])
	}
	for w := 1; w <= 40; w++ {
		out, _ := renderView(buildView(doc, uiState{}), w, 30, 0)
		l := strings.Split(plain(out), "\n")
		if len([]rune(l[len(l)-1])) > w {
			t.Errorf("width %d: footer %q overflows", w, l[len(l)-1])
		}
	}
}

// The header's + and ⋯ open kel's menus; an item clicked is its key, and a
// click anywhere else closes the menu without doing anything.
func TestKelMenus(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	m := sbModel{doc: loadFixture(t, "one-agent"), width: 32, height: 30, visible: true}
	_, refs := renderView(m.view(), 32, 30, 0)
	if refs[0].kind != "menu" || refs[0].at(2) != "" {
		t.Fatalf("header: %+v", refs[0])
	}
	open := func(x int) sbModel {
		nm, _ := m.Update(tea.MouseClickMsg{X: x, Y: 0, Button: tea.MouseLeft})
		return nm.(sbModel)
	}
	nw := open(refs[0].from + 2)
	if nw.menu == nil || nw.menu.title != "New" {
		t.Fatalf("+ New opened %+v", nw.menu)
	}
	nm, _ := nw.Update(tea.MouseClickMsg{X: 4, Y: rowOf(t, nw, "mi:w"), Button: tea.MouseLeft})
	if got := nm.(sbModel); got.mode != modeNewWorktree || got.menu != nil {
		t.Errorf("Agent in a worktree: mode %v, menu %+v", got.mode, got.menu)
	}
	kl := open(refs[0].split + 1)
	if kl.menu == nil || kl.menu.title != "kel" {
		t.Fatalf("⋯ opened %+v", kl.menu)
	}
	nm, _ = kl.Update(tea.MouseClickMsg{X: 4, Y: rowOf(t, kl, "mi:q"), Button: tea.MouseLeft})
	if nm.(sbModel).mode != modeQuit {
		t.Error("Quit kel did not ask to quit")
	}
	nm, _ = kl.Update(tea.MouseClickMsg{X: 4, Y: 28, Button: tea.MouseLeft})
	if got := nm.(sbModel); got.menu != nil || got.mode != modeNormal {
		t.Errorf("a click outside: menu %+v mode %v", got.menu, got.mode)
	}
	// the keyboard walks it too: down, enter
	nm, _ = kl.key(tea.KeyPressMsg{Code: tea.KeyDown})
	nm, _ = nm.(sbModel).key(tea.KeyPressMsg{Code: tea.KeyEnter})
	if nm.(sbModel).menu != nil {
		t.Error("enter did not choose")
	}
}

// A right-click on an entry selects it and opens its drawer beneath it.
func TestEntryMenu(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	m := sbModel{doc: loadFixture(t, "fifteen-three-repos"), width: 32, height: 40, visible: true}
	y := rowOf(t, m, "a:@2")
	nm, _ := m.Update(tea.MouseClickMsg{X: 4, Y: y, Button: tea.MouseRight})
	m = nm.(sbModel)
	if m.ui.sel != "a:@2" || m.drawer == nil || m.drawer.id != "a:@2" || m.menu != nil {
		t.Fatalf("right-click fix-auth: sel %q drawer %+v", m.ui.sel, m.drawer)
	}
	nm, _ = m.Update(tea.MouseClickMsg{X: 4, Y: rowOf(t, m, "dr:x"), Button: tea.MouseLeft})
	if got := nm.(sbModel); got.mode != modeKill || got.target != "a:@2" || got.drawer != nil {
		t.Errorf("Kill: mode %v target %q drawer %+v", got.mode, got.target, got.drawer)
	}
	acts := entryMenu(buildView(m.doc, uiState{sel: "a:@2"}))
	has := false
	for _, a := range acts {
		has = has || a == sbAct{"h", "History"}
	}
	if !has {
		t.Errorf("an agent's menu lacks h History: %+v", acts)
	}
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'h', Text: "h"})
	if cmd == nil {
		t.Error("h on an agent opened no history")
	}
	m.ui.expanded = map[string]bool{reposKey: true}
	m.ui.sel = "s:/home/me/code/api-gw"
	if _, cmd := m.Update(tea.KeyPressMsg{Code: 'h', Text: "h"}); cmd != nil {
		t.Error("h on a space opens a popup")
	}
}

// The entry under the pointer shows its buttons in place of its detail; the
// pointer can move along the row and keep them; a button clicked acts on that
// entry, whatever was selected, and ⋯ opens its menu.
func TestRowButtons(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	m := sbModel{doc: loadFixture(t, "fifteen-three-repos"), width: 32, height: 40, visible: true}
	y := rowOf(t, m, "a:@2")
	nm, _ := m.Update(tea.MouseMotionMsg{X: 4, Y: y})
	m = nm.(sbModel)
	out, refs := renderView(m.view(), 32, 40, 0)
	r := refs[y]
	head := strings.Split(plain(out), "\n")[y]
	if r.id != "a:@2" || len(r.hits) != 3 || r.hits[2].key != "menu" {
		t.Fatalf("row %d: %+v", y, r)
	}
	for _, h := range r.hits {
		if h.key == "d" && !strings.HasPrefix(string([]rune(head)[h.x0:]), "▐Diff▌") {
			t.Errorf("Diff is not where its hit is (%d): %q", h.x0, head)
		}
	}
	nm, _ = m.Update(tea.MouseMotionMsg{X: r.hits[1].x0, Y: y})
	m = nm.(sbModel)
	if _, r2 := renderView(m.view(), 32, 40, 0); len(r2[y].hits) != 3 || r2[y].id != "a:@2" {
		t.Errorf("moving along the row lost the buttons: %+v", r2[y])
	}
	nm, cmd := m.Update(tea.MouseClickMsg{X: r.hits[1].x0 + 1, Y: y, Button: tea.MouseLeft})
	if nm.(sbModel).ui.sel != "a:@2" || cmd == nil {
		t.Errorf("Files: sel %q, cmd %v", nm.(sbModel).ui.sel, cmd)
	}
	nm, _ = m.Update(tea.MouseClickMsg{X: r.hits[2].x0, Y: y, Button: tea.MouseLeft})
	if d := nm.(sbModel).drawer; d == nil || d.id != "a:@2" {
		t.Errorf("⋯ opened %+v", d)
	}
}

// config.toml is read with bin/kel's rules and written in place: a key's
// line keeps its indent and comment, a new key goes at the end, and every
// other line is left as it was.
func TestConfigReadWrite(t *testing.T) {
	p := filepath.Join(t.TempDir(), "kel", "config.toml")
	if err := setConfig(p, "pr", "off"); err != nil || readConfig(p)["pr"] != "off" {
		t.Fatalf("a new file: %v %v", err, readConfig(p))
	}
	os.WriteFile(p, []byte("[ui]\n  sidebar_bg = \"#1e1e2e\"   # WezTerm\n# notify = \"waiting\"\nctx_warn = 70 # percent\n"), 0o644)
	c := readConfig(p)
	if c["sidebar_bg"] != "#1e1e2e" || c["ctx_warn"] != "70" || c["notify"] != "" {
		t.Errorf("read: %v", c)
	}
	setConfig(p, "ctx_warn", "75")
	setConfig(p, "notify", "waiting done")
	b, _ := os.ReadFile(p)
	want := "[ui]\n  sidebar_bg = \"#1e1e2e\"   # WezTerm\n# notify = \"waiting\"\nctx_warn = \"75\"  # percent\nnotify = \"waiting done\"\n"
	if string(b) != want {
		t.Errorf("written:\n%s\nwant:\n%s", b, want)
	}
}

// Settings opens from the foot; a click on a switch flips it in config.toml,
// a stepper's arrows move it, and Back closes the screen.
func TestSettingsScreen(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := sbModel{doc: loadFixture(t, "one-agent"), width: 32, height: 40, visible: true}
	nm, _ := m.Update(tea.MouseClickMsg{X: 4, Y: 39, Button: tea.MouseLeft})
	m = nm.(sbModel)
	if !m.settings {
		t.Fatal("Settings did not open")
	}
	row := func(name string) (int, lineRef) {
		_, refs := renderSettings(readConfig(kelConfigPath()), 32, 40, m.setSel, 0, 0)
		for i, r := range refs {
			if r.id == "set:"+name {
				return i, r
			}
		}
		t.Fatalf("no row %s", name)
		return 0, lineRef{}
	}
	y, _ := row("notify_bell")
	nm, _ = m.Update(tea.MouseClickMsg{X: 10, Y: y, Button: tea.MouseLeft})
	m = nm.(sbModel)
	if readConfig(kelConfigPath())["notify_bell"] != "off" {
		t.Error("the bell switch did not write off")
	}
	y, r := row("ctx_warn")
	nm, _ = m.Update(tea.MouseClickMsg{X: r.hits[1].x0, Y: y, Button: tea.MouseLeft})
	m = nm.(sbModel)
	if readConfig(kelConfigPath())["ctx_warn"] != "75" {
		t.Errorf("ctx_warn › wrote %q", readConfig(kelConfigPath())["ctx_warn"])
	}
	y, _ = row("notify:done")
	m.Update(tea.MouseClickMsg{X: 10, Y: y, Button: tea.MouseLeft})
	if got := readConfig(kelConfigPath())["notify"]; got != "waiting error done" {
		t.Errorf("finishes switch: notify %q", got)
	}
	nm, _ = m.Update(tea.MouseClickMsg{X: 3, Y: 0, Button: tea.MouseLeft})
	if nm.(sbModel).settings {
		t.Error("Back did not close the screen")
	}
}

// What an agent is doing wraps onto a second line at a space, and only what
// still does not fit is cut.
func TestWrapWords(t *testing.T) {
	got := wrapWords("track hovered entry so the strip persists across pointer moves", 27, 2)
	if len(got) != 2 || got[0] != "track hovered entry so the" || !strings.HasSuffix(got[1], "…") {
		t.Errorf("wrapped: %q", got)
	}
	if got := wrapWords("editing sbview.go", 27, 2); len(got) != 1 {
		t.Errorf("a short one stays one line: %q", got)
	}
}

func TestStateLook(t *testing.T) {
	const now = 1_000_000
	st := func(s string) *string { return &s }
	h := 3600.0
	for _, c := range []struct {
		name   string
		state  *string
		unseen bool
		ago    float64
		dot    string
		colour string
	}{
		{"idle", st("idle"), false, 60, "○", dim},
		{"done unseen", st("done"), true, 60, "●", green},
		{"done seen", st("done"), false, 60, "○", green},
		{"done seen 11h59m", st("done"), false, 12*h - 60, "○", green},
		{"done seen 12h", st("done"), false, 12 * h, "○", dim},
		{"done seen no clock", st("done"), false, -1, "○", dim},
		{"done unseen 3d", st("done"), true, 72 * h, "●", green},
		{"working", st("working"), false, 60, "●", cyan},
		{"waiting", st("waiting"), false, 60, "●", yellow + bold},
		{"error", st("error"), false, 60, "●", red},
		{"dead", st("dead"), false, 60, "✕", red},
		{"throttled", st("throttled"), false, 60, "◌", purple},
		{"unknown", st(""), false, 60, "?", dim},
		{"no state", nil, false, 60, "?", dim},
	} {
		since := float64(now) - c.ago
		sincep := &since
		if c.ago < 0 {
			sincep = nil
		}
		if d, col := stateLook(c.state, c.unseen, sincep, now); d != c.dot || col != c.colour {
			t.Errorf("%s: got %q %q, want %q %q", c.name, d, col, c.dot, c.colour)
		}
	}
}

func TestRunningSubagentsKeepWorkingGlyph(t *testing.T) {
	const now = 1_000_000
	w := "working"
	silent := func(subs ...fleet.Subagent) fleet.Agent {
		since := float64(now - 31*60)
		return fleet.Agent{State: &w, StateSince: &since, Subagents: subs}
	}
	if quietFor(silent(), now) == 0 {
		t.Error("a working agent with no subagents and 31 minutes of silence is not marked quiet")
	}
	if quietFor(silent(fleet.Subagent{Status: "completed"}), now) == 0 {
		t.Error("finished subagents keep the agent quiet")
	}
	running := silent(fleet.Subagent{Status: "completed"}, fleet.Subagent{Status: "running"})
	if quietFor(running, now) != 0 {
		t.Error("an agent with a running subagent is marked quiet")
	}
	if age, _, _, _ := agentAge(running, now); strings.Contains(age, "quiet") {
		t.Errorf("age %q carries the quiet marker", age)
	}
	if d, c := stateLook(running.State, false, nil, now); d != "●" || c != cyan {
		t.Errorf("running subagent: glyph %q %q, want the working glyph", d, c)
	}
}

func TestWorktreeDetailLandLabels(t *testing.T) {
	for code, want := range map[string]string{"fresh": "no commits", "detached": "detached"} {
		got := worktreeDetail(sbSpace{closed: true, land: &fleet.Land{Code: code}})
		if plain(got) != want || !strings.Contains(got, dim+want+reset) {
			t.Fatalf("%s: got %q, want dim %q", code, got, want)
		}
	}
}

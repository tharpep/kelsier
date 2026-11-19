package main

import (
	"fmt"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/tharpep/kelsier/internal/fleet"
)

// The view tabs (spec.md §5d): Agents · Board · PRs · Trees, one row under
// kel's own in the sidebar and in the board's header. They are views of the
// one surface (invariant 6), not lists of their own: Agents is the list, Board
// and PRs open the board and the gh-dash popup, Trees swaps the list for the
// repos' worktrees. The strip takes the row the blank under kel's used to, so
// nothing below it moves when a tab is chosen.

var (
	tabNames  = []string{"agents", "board", "prs", "trees"}
	tabLabels = []string{"Agents", "Board", "PRs", "Trees"}
	tabShort  = []string{"Agt", "Brd", "PR", "Tree"}
)

// tabOf is a tab's name as stored, "" read as agents.
func tabOf(t string) string {
	for _, n := range tabNames {
		if n == t {
			return n
		}
	}
	return "agents"
}

// tabStep is the tab d steps from t, round the four.
func tabStep(t string, d int) string {
	n := len(tabNames)
	for i, name := range tabNames {
		if name == tabOf(t) {
			return tabNames[((i+d)%n+n)%n]
		}
	}
	return tabNames[0]
}

// tabsWidth is what the strip needs: each label with a space either side,
// the first label in the list's name column.
func tabsWidth(labels []string) int {
	n := 0
	for _, l := range labels {
		n += len([]rune(l)) + 2
	}
	return n
}

// tabStrip draws the strip at w: on is the tab shown, on the selection's
// surface; lit the one under the pointer. Too narrow for the short labels it
// is a blank row, which keeps the rows below where they were.
func tabStrip(on, lit string, w int) line {
	labels := tabLabels
	if tabsWidth(labels) > w-1 {
		labels = tabShort
	}
	if tabsWidth(labels) > w-1 {
		return line{}
	}
	var b strings.Builder
	x := 0
	var hits []hit
	for i, l := range labels {
		cell := " " + l + " "
		n := len([]rune(cell))
		key := "tab:" + tabNames[i]
		switch {
		case tabNames[i] == tabOf(on):
			b.WriteString(raised + accent + bold + cell + reset)
		case key == lit:
			b.WriteString(lifted.mix(baseBg, 0.25).bg() + cell + reset)
		default:
			b.WriteString(dim + cell + reset)
		}
		hits = append(hits, hit{x, x + n, key})
		x += n
	}
	return line{b.String(), lineRef{kind: "tabs", hits: hits}}
}

// sbTree is one linked worktree in the Trees view.
type sbTree struct {
	repo string
	wt   fleet.Worktree
}

// treeRows is every repo's linked worktrees, in the board's order, kept to
// what the filter matches.
func treeRows(repos []fleet.Repo, filter string) []sbTree {
	var out []sbTree
	q := strings.ToLower(filter)
	for _, r := range repos {
		for _, w := range r.Worktrees {
			if q != "" && !strings.Contains(strings.ToLower(wtLabel(w)), q) {
				continue
			}
			out = append(out, sbTree{r.Name, w})
		}
	}
	return out
}

// treeEntries makes the Trees view's rows the selectable ones, and settles
// the selection on one of them.
func (v *sbView) treeEntries() {
	v.entryIDs, v.inner = nil, map[string]string{}
	for _, t := range v.trees {
		v.entryIDs = append(v.entryIDs, wtID(t.wt.Path))
	}
	if v.indexOf(v.sel) < 0 {
		v.sel = ""
		if len(v.entryIDs) > 0 {
			v.sel = v.entryIDs[0]
		}
	}
}

// treeOf is the worktree an id names in the view.
func (v sbView) treeOf(id string) (sbTree, bool) {
	for _, t := range v.trees {
		if wtID(t.wt.Path) == id {
			return t, true
		}
	}
	return sbTree{}, false
}

// shortLand is landWord in a row's room: what landing the worktree needs. An
// answer kel does not have is ?, never clean (invariant 3).
func shortLand(w fleet.Worktree) string {
	if w.Missing {
		return red + "missing" + reset
	}
	l := w.Land
	if l == nil {
		return dim + "?" + reset
	}
	n := 0
	if l.N != nil {
		n = int(*l.N)
	}
	switch l.Code {
	case "merged":
		return green + "merged" + reset
	case "dirty":
		return yellow + fmt.Sprintf("●%d dirty", n) + reset
	case "unpushed":
		return yellow + fmt.Sprintf("↑%d", n) + reset
	case "fresh":
		return dim + "new" + reset
	case "review":
		return mid + "review" + reset
	case "behind":
		return dim + fmt.Sprintf("↓%d", n) + reset
	case "no_pr":
		return dim + "no PR" + reset
	case "detached", "clean":
		return dim + l.Code + reset
	}
	return dim + "?" + reset
}

// treeLines is the Trees view's list: each repo under its name, a row per
// worktree with its land state's dot and what landing it needs.
func (v sbView) treeLines(w int) []line {
	var body []line
	if v.unknown {
		return []line{{" " + dim + truncate("? panes unknown", w-2) + reset, lineRef{}}}
	}
	if len(v.trees) == 0 {
		word := "no worktrees"
		if v.filter != "" {
			word = "no worktree matches"
		}
		return []line{{" " + dim + truncate(word, w-2) + reset, lineRef{}}}
	}
	avail := max(w-4, 0)
	for i, t := range v.trees {
		if i == 0 || v.trees[i-1].repo != t.repo {
			if i > 0 {
				body = append(body, line{})
			}
			body = append(body, line{" " + dim + "▾ " + truncate(t.repo, max(avail-1, 1)) + reset, lineRef{kind: "treehead"}})
		}
		id := wtID(t.wt.Path)
		dot := dim + "○" + reset
		switch {
		case t.wt.Missing:
			dot = red + "✕" + reset
		case t.wt.Land != nil && t.wt.Land.Code == "merged":
			dot = green + "●" + reset
		case t.wt.Land != nil && (t.wt.Land.Code == "dirty" || t.wt.Land.Code == "unpushed"):
			dot = yellow + "●" + reset
		}
		right := shortLand(t.wt)
		nameW, rightW := fitPair(wtLabel(t.wt), visWidth(right), avail-2)
		if rightW == 0 {
			right = ""
		} else if rightW < visWidth(right) {
			right = truncateStyled(right, rightW)
		}
		bar := " "
		if id == v.sel {
			bar = accent + "▌" + reset
		}
		s := spread(bar+dot+" "+truncate(wtLabel(t.wt), nameW), right, w)
		if id == v.sel {
			s = fill(s, w, raised)
		}
		body = append(body, line{s, lineRef{id: id, kind: "tree"}})
	}
	return body
}

// inspectTree: a worktree's branch and repo, where it is, what landing it
// needs, who stands in it, and its keys.
func (v sbView) inspectTree(t sbTree) []irow {
	who := dim + "no agent here" + reset
	var names []string
	for _, a := range v.agents {
		if standsIn(a, t.wt.Path) {
			names = append(names, a.agent.Name)
		}
	}
	switch {
	case len(names) > 0:
		who = mid + "agent " + strings.Join(names, ", ") + reset
	case t.wt.Sessions > 0:
		who = mid + fmt.Sprintf("%d claude session elsewhere", t.wt.Sessions) + reset
	case t.wt.Sessions < 0:
		who = dim + "claude sessions ?" + reset
	}
	if t.wt.Locked {
		who += dim + " · locked" + reset
	}
	return []irow{
		{left: bold + wtLabel(t.wt) + reset, right: dim + t.repo + reset},
		{left: dim + tildify(t.wt.Path) + reset},
		{left: landWord(t.wt)},
		{left: who},
		chips([]sbAct{{"enter", "go"}, {"g", "lazygit"}, {"s", "sweep"}}),
	}
}

// treeTitle names a worktree in a popup's border: its repo and its branch.
func treeTitle(t sbTree) string {
	return t.repo + "/" + wtLabel(t.wt)
}

// standsIn: is the agent working in the worktree at path?
func standsIn(a sbAgent, path string) bool {
	return a.git != nil && a.git.Root == path || a.cwd == path || strings.HasPrefix(a.cwd, path+"/")
}

// setTab chooses a tab: Agents and Trees swap the list, keeping each list's
// own cursor; Board and PRs open their popups. On the board, Agents and Trees
// are the sidebar's lists, so the board hands the choice over and closes.
func (m sbModel) setTab(t string) (tea.Model, tea.Cmd) {
	t = tabOf(t)
	was, cur := tabOf(m.ui.tab), m.view().sel
	m.menu, m.layouts, m.help, m.settings = nil, nil, false, false
	if m.board {
		switch t {
		case "board":
			m.ui.tab = t
			return m, nil
		case "prs":
			m.ui.tab = t
			return m, m.dash()
		}
		m.ui.fileView = t
		if strings.HasPrefix(cur, "w:") != (t == "trees") {
			m.ui.sel, m.ui.parked = m.ui.parked, cur
		}
		m.saveShared()
		return m, tea.Sequence(runKelQuiet("_sidebar", "wake"), tea.Quit)
	}
	m.ui.tab = t
	if (was == "trees") != (t == "trees") {
		m.ui.sel, m.ui.parked = m.ui.parked, cur
	}
	m.saveShared()
	switch t {
	case "board":
		return m, runKel("", nil, "board")
	case "prs":
		return m, m.dash()
	}
	return m, nil
}

// dash opens every open space's PRs in gh dash, the selected one's repo
// first.
func (m sbModel) dash() tea.Cmd {
	dir := os.Getenv("HOME")
	v := m.view()
	if t, ok := v.treeOf(v.sel); ok && !t.wt.Missing {
		dir = t.wt.Path
	} else if sp, ok := m.selSpace(v.sel); ok {
		dir = sp.dir()
	}
	return runKel(dir, nil, "_panel", "dash", dir)
}

// treeKey is a key a selected worktree answers: s sweeps it through kel
// sweep and its checks (never forced, never where a window or a Claude
// session stands), g opens lazygit there, t a shell.
func (m sbModel) treeKey(k string) (tea.Model, tea.Cmd, bool) {
	v := m.view()
	t, ok := v.treeOf(v.sel)
	if !ok {
		return m, nil, false
	}
	switch k {
	case "s":
		m.mode, m.target = modeSweep, v.sel
		return m, nil, true
	case "g":
		return m, runKel(t.wt.Path, nil, "_panel", "git", t.wt.Path, treeTitle(t)), true
	case "t":
		return m, runKel(t.wt.Path, nil, "shell", t.wt.Path), true
	}
	return m, nil, false
}

// goTree is enter on a worktree: to the agent working in it, else a pane
// standing in it; with neither, the footer says how to open one.
func (m *sbModel) goTree(id string) {
	v := m.view()
	t, ok := v.treeOf(id)
	if !ok {
		return
	}
	for _, a := range v.agents {
		if standsIn(a, t.wt.Path) {
			jump(a.wid, a.pane)
			return
		}
	}
	if m.doc.Panes != nil {
		for _, p := range *m.doc.Panes {
			if p.Cwd == t.wt.Path || strings.HasPrefix(p.Cwd, t.wt.Path+"/") {
				jump(p.WindowID, p.PaneID)
				return
			}
		}
	}
	m.msg, m.msgBad = "nothing open in "+wtLabel(t.wt)+" · t opens a shell", false
}

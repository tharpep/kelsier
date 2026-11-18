package main

import (
	"fmt"
	"strings"

	"github.com/tharpep/kelsier/internal/fleet"
)

// inspectorRows is the inspector's height, whatever is selected: the list
// above it never moves when the selection does.
const inspectorRows = 5

// irow is one inspector row: its left and right, and the buttons on it, their
// columns counted from the row's first.
type irow struct {
	left, right string
	hits        []hit
}

// inspector is the selected entry's detail, pinned above the footer on the
// raised surface: what a row used to open into, and the drawer's facts. Its
// last row is the entry's keys, each a click; ⋯ opens the entry's menu.
func (v sbView) inspector(w int, now int64) []line {
	rows := v.inspectRows(w, now)
	out := make([]line, inspectorRows)
	for i := range out {
		var r irow
		if i < len(rows) {
			r = rows[i]
		}
		s := irowString(r, w)
		if v.sel != "" {
			s = fill(s, w, raised)
		}
		var hits []hit
		for _, h := range r.hits {
			if h.x1 <= w-1 {
				hits = append(hits, hit{h.x0 + 1, h.x1 + 1, h.key})
			}
		}
		out[i] = line{s, lineRef{kind: "inspector", hits: hits}}
	}
	return out
}

// irowString sets a row one column in, its right flush one column clear of
// the edge. The left is cut before the right goes; the right goes when it
// would leave the left less than half.
func irowString(r irow, w int) string {
	room := w - 2
	left, right := r.left, r.right
	lw, rw := visWidth(left), visWidth(right)
	if rw > 0 && lw+1+rw > room && rw+1 > room/2 {
		right, rw = "", 0
	}
	if max := room - rw - min(rw, 1); lw > max {
		left = ""
		if max > 0 {
			left = truncateStyled(r.left, max)
		}
	}
	return spread(" "+left, right, w)
}

func (v sbView) inspectRows(w int, now int64) []irow {
	id := v.sel
	switch {
	case id == "":
		return nil
	case strings.HasPrefix(id, "a:"):
		for _, a := range v.agents {
			if agentID(a.key) == id {
				return v.inspectAgent(a, w, now)
			}
		}
	case strings.HasPrefix(id, "g:"):
		return v.inspectGroup(strings.TrimPrefix(id, "g:"))
	case strings.HasPrefix(id, "x:"):
		q := v.quiet[strings.TrimPrefix(id, "x:")]
		word := "show"
		if q.open {
			word = "hide"
		}
		return []irow{{left: mid + strings.TrimSpace(strings.TrimLeft(q.text(), "+−")) + reset}, {}, {}, {}, chips([]sbAct{{"enter", word}})}
	case id == bgHeadID:
		return headRows("background", fmt.Sprintf("%d sessions", len(v.bgLoose)), v.bgOpen)
	case id == shellsID:
		return headRows("shells", fmt.Sprintf("%d panes", len(v.shells)), v.shOpen)
	case strings.HasPrefix(id, "b:"):
		for _, list := range append([][]fleet.Background{v.bgLoose}, mapValues(v.bgUnder)...) {
			for _, b := range list {
				if bgID(b.ID) == id {
					_, c, word := bgLook(b)
					return []irow{
						{left: bold + b.Name + reset + "  " + c + word + reset, right: dim + b.Group + reset},
						{left: dim + tildify(b.Cwd) + reset},
						{left: dim + "background session" + reset},
						{},
						chips([]sbAct{{"enter", "open"}, {"menu", ""}}),
					}
				}
			}
		}
	default:
		if p, ok := v.paneByID(id); ok {
			return v.inspectPane(p)
		}
	}
	return nil
}

func headRows(name, count string, open bool) []irow {
	word := "open"
	if open {
		word = "fold"
	}
	return []irow{{left: bold + name + reset, right: dim + count + reset}, {}, {}, {}, chips([]sbAct{{"enter", word}})}
}

// inspectAgent: name, state and age; what it is doing (or the card it left
// you); model, context and cost, with its mode and worktree tags; the other
// panes of its window, its subagents and background children, each pane and
// child a click that selects it; its keys.
func (v sbView) inspectAgent(a sbAgent, w int, now int64) []irow {
	word, wc := stateWord(a.agent, now)
	age := ""
	if q := quietFor(a.agent, now); q > 0 {
		age = humanizeSecs(q)
	} else if a.agent.StateSince != nil && a.agent.State != nil && *a.agent.State != "" {
		age = humanizeSecs(now - int64(*a.agent.StateSince))
	}
	// the name gives way first: the state word is what the row is for
	nameW := w - 2 - len([]rune(age)) - 1 - len([]rune(word)) - 2
	rows := []irow{{left: bold + truncate(a.agent.Name, max(nameW, 4)) + reset + "  " + wc + word + reset, right: dim + age + reset}}

	tasks := taskCount(a.agent.Tasks)
	second := irow{right: dim + tasks + reset}
	if c := a.agent.Cards; c != nil && c.Unseen && c.Latest != nil {
		col := dim
		if c.Latest.Kind == "decision" {
			col = yellow
		}
		second.left = col + "▸ " + c.Latest.Kind + ": " + c.Latest.Title + reset
	} else if sum := summaryOf(a.agent); sum.text != "" {
		second.left = sum.colour + strings.TrimSpace(strings.SplitN(sum.text, "\n", 2)[0]) + reset
	} else if sub := firstRunning(a.agent); sub != "" && a.agent.State != nil && *a.agent.State == "working" {
		second.left = dim + sub + reset
	}
	rows = append(rows, second)

	var tags []string
	if mw := modeWord(a.agent.Mode); mw != "" {
		tags = append(tags, mw)
	}
	if a.worktree() {
		tags = append(tags, "wt")
	}
	rows = append(rows, irow{left: mid + agentFacts(a.agent) + reset, right: faint + strings.Join(tags, " ") + reset})

	type seg struct{ s, key string }
	var segs []seg
	for _, sh := range v.under[a.key] {
		segs = append(segs, seg{paneGlyph(sh.pane.Kind) + " " + paneLabel(sh.pane), "sel:" + sh.pane.PaneID})
	}
	if n := runningCount(a.agent); n > 0 {
		t := fmt.Sprintf("%d subagents", n)
		if n == 1 {
			t = "1 subagent"
		}
		segs = append(segs, seg{dim + t + reset, ""})
	}
	for _, b := range v.bgUnder[a.key] {
		d, c, _ := bgLook(b)
		segs = append(segs, seg{c + d + reset + " " + b.Name + dim + " · bg" + reset, "sel:" + bgID(b.ID)})
	}
	var four irow
	x := 0
	for i, sg := range segs {
		sep := ""
		if i > 0 {
			sep = dim + " · " + reset
		}
		n := visWidth(sep + sg.s)
		if x+n > w-2 {
			if rest := fmt.Sprintf(" +%d", len(segs)-i); x+len(rest) <= w-2 {
				four.left += dim + rest + reset
			}
			break
		}
		four.left += sep + sg.s
		if sg.key != "" {
			four.hits = append(four.hits, hit{x + visWidth(sep), x + n, sg.key})
		}
		x += n
	}
	rows = append(rows, four)

	keys := []sbAct{{"enter", "go"}, {"space", "peek"}}
	if a.agent.State != nil && *a.agent.State == "dead" && a.agent.Managed {
		keys = append(keys, sbAct{"R", "relaunch"})
	}
	return append(rows, chips(append(keys, sbAct{"menu", ""})))
}

// inspectGroup: a repo group's heading — its agents, and where its main
// checkout stands.
func (v sbView) inspectGroup(repo string) []irow {
	n := 0
	for _, a := range v.agents {
		if a.repo == repo {
			n++
		}
	}
	count := fmt.Sprintf("%d agents", n)
	if n == 1 {
		count = "1 agent"
	}
	name := repo
	if name == "" {
		name = "misc"
	}
	rows := []irow{{left: bold + name + reset, right: dim + count + reset}, {}, {}, {}}
	if s, ok := v.groupSpace(repo); ok {
		if s.git != nil {
			rows[1].left = mid + spacePos(s) + reset
		}
		rows[2].left = dim + tildify(s.dir()) + reset
		switch {
		case s.ports == nil:
			rows[3].left = dim + ":?" + reset
		case len(*s.ports) > 0:
			ps := make([]string, len(*s.ports))
			for i, p := range *s.ports {
				ps[i] = fmt.Sprintf(":%d", p)
			}
			rows[3].left = dim + strings.Join(ps, " ") + reset
		}
	}
	word := "fold"
	if v.shut[repo] {
		word = "open"
	}
	return append(rows, chips([]sbAct{{"enter", word}, {"menu", ""}}))
}

// inspectPane: a pane's command and ports, its window, where it stands, and
// whose window it shares when it has no row of its own.
func (v sbView) inspectPane(p fleet.Pane) []irow {
	rows := []irow{
		{left: paneGlyph(p.Kind) + " " + bold + paneLabel(p) + reset, right: dim + p.WindowName + reset},
		{left: dim + tildify(p.Cwd) + reset},
		{},
		{},
	}
	if p.Git != nil {
		rows[2].left = mid + spacePos(sbSpace{git: p.Git, cwd: p.Cwd}) + reset
	}
	if c := p.Cards; c != nil && c.Unseen && c.Latest != nil {
		col := dim
		if c.Latest.Kind == "decision" {
			col = yellow
		}
		rows[3].left = col + "▸ " + c.Latest.Kind + ": " + c.Latest.Title + reset
	} else if o := v.inner[p.PaneID]; o != "" {
		for _, a := range v.agents {
			if agentID(a.key) == o {
				rows[3].left = dim + "in " + a.agent.Name + "'s window" + reset
			}
		}
	}
	return append(rows, chips([]sbAct{{"enter", "go"}, {"space", "peek"}, {"menu", ""}}))
}

// stateWord is the inspector's word for an agent's state, in its colour.
func stateWord(a fleet.Agent, now int64) (word, colour string) {
	if quietFor(a, now) > 0 {
		return "working?", dim + italic
	}
	if a.State == nil || *a.State == "" {
		return "unknown", dim
	}
	_, c := stateLook(a.State, a.Unseen, a.StateSince, now)
	return *a.State, c
}

// firstRunning is what the agent's first running subagent is doing: what a
// working agent with no word of its own is doing.
func firstRunning(a fleet.Agent) string {
	for _, s := range a.Subagents {
		if s.Status == "running" {
			return s.Label
		}
	}
	return ""
}

func runningCount(a fleet.Agent) (n int) {
	for _, s := range a.Subagents {
		if s.Status == "running" {
			n++
		}
	}
	return
}

// chips is the inspector's last row: each key and its word, a click.
func chips(acts []sbAct) irow {
	var r irow
	x := 0
	for i, a := range acts {
		if i > 0 {
			r.left += "  "
			x += 2
		}
		g := map[string]string{"enter": "↵", "space": "␣", "menu": "⋯"}[a.key]
		if g == "" {
			g = a.key
		}
		t := bold + g + reset
		n := len([]rune(g))
		if a.label != "" {
			t += " " + dim + a.label + reset
			n += 1 + len([]rune(a.label))
		}
		r.left += t
		r.hits = append(r.hits, hit{x, x + n, a.key})
		x += n
	}
	return r
}

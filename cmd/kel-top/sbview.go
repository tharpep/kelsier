package main

// The sidebar's view: the fleet document turned into two sections, and those
// drawn as text (spec.md §5, §5d). Pure — no tmux, no clock, no files — so a
// fixture renders the same frame every time and the golden tests can hold an
// approved frame in place (§5e). sidebar.go owns everything live.
//
//	spaces   where: one entry per repo checkout or linked worktree, its git
//	         position and ports; the current one expanded to its non-agent
//	         panes (servers, shells, programs)
//	agents   who needs me: every agent, most urgent first
//
// Agents are listed only under `agents` — one home per fact (§7) — so an
// expanded space shows what else is running there, not the agents again.

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/tharpep/kelsier/internal/fleet"
)

// §5d: the 16 ANSI colours only, and colour means state. blue is the one
// accent — no state uses it, so the selection can never read as a state.
const accent = "\x1b[34m"

const italic = "\x1b[3m"

// uiState is what the user has done to the view; it is not data.
type uiState struct {
	sel      string          // selected entry id (see sbEntry.id)
	expanded map[string]bool // explicit toggles by space key; absent = default
	filter   string
	myWindow string // the window this sidebar sits in — current, whatever the doc says
	focused  bool   // the keyboard is in this sidebar
	hover    int    // screen line under the pointer, plus one; 0 = none
	prefix   string // the tmux prefix in words ("Ctrl-b"), for hints
	restore  bool   // a previous workspace waits to be restored
}

type sbSpace struct {
	key, label string
	git        *fleet.Git
	cwd        string
	ports      *[]int // union over the space's panes; nil = could not tell
	panes      []fleet.Pane
	state      *string // the most urgent agent state in the space, nil if none
	unseen     bool
	current    bool
	open       bool
	// phase 2: a repo's worktrees nest under it (fleet.Repo)
	parent  string      // the main checkout's key, for a linked worktree
	land    *fleet.Land // a linked worktree's land state; nil = not known
	closed  bool        // no pane stands here: known from git, not tmux
	missing bool        // a worktree whose directory is gone
	agents  int         // agents working in this checkout
	repo    string      // the repo's name, for a worktree whose label is its branch
}

type sbAgent struct {
	wid     string
	pane    string // the pane to jump to; "" when the window lists none
	agent   fleet.Agent
	current bool
}

type sbView struct {
	spaces []sbSpace
	agents []sbAgent
	// bg: background agents (`claude --bg`), each nested under the top agent
	// of the same name when there is one — Claude Code does not say which
	// agent dispatched it, and a same-named one is how the author makes them
	bgUnder  map[string][]fleet.Background // agent window id -> its children
	bgLoose  []fleet.Background            // no such agent: listed on their own
	unknown  bool                          // the fleet could not list panes: say so, never "nothing open"
	sel      string
	filter   string
	entryIDs []string // selectable ids in screen order, for j/k

	footer, footerColour string // a prompt or a message; "" = the key hint
	hint2                string // a line above the footer: a prompt's choices, clickable
	focused              bool
	hover                int
	prefix               string
	restore              bool
	quota                *fleet.Quota // the account's, for the last line; nil = none reported
	waiting              int          // agents blocked on you, and since when the oldest
	waitSince            int64
}

// lineRef says which entry a screen line belongs to — what a click hits.
type lineRef struct {
	id   string // "" for labels, blanks and the footer
	kind string // space | pane | agent
}

func spaceID(key string) string { return "s:" + key }
func bgID(id string) string     { return "b:" + id }

// subLook: a subagent's dot — working cyan, finished grey ✓, failed red ✕.
func subLook(status string) (dot, colour string) {
	switch status {
	case "running":
		return "●", cyan
	case "completed":
		return "✓", dim
	case "failed", "killed":
		return "✕", red
	}
	return "?", dim
}

// bgLook: a background agent's dot and word. Claude Code calls one waiting on
// you "blocked"; busy is working; idle is calm.
func bgLook(b fleet.Background) (dot, colour, word string) {
	switch {
	case b.State == "blocked":
		return "●", yellow + bold, "blocked"
	case b.Status == "busy":
		return "●", cyan, "working"
	case b.Status == "idle":
		return "○", dim, "idle"
	}
	return "?", dim, "?"
}
func agentID(wid string) string { return "a:" + wid }

// spaceKey groups a pane: its repo root (a linked worktree is its own root, so
// each worktree is its own space), else its group label.
func spaceKey(p fleet.Pane) (key, label string) {
	label = p.Group
	if label == "" {
		label = "misc"
	}
	if p.Git != nil && p.Git.Root != "" {
		// a main checkout is named by its own directory: the window's group
		// is wherever the agent was filed, and the author saw two spaces both
		// "kelsier · main" because one of them was ~/code/ai-context. A
		// linked worktree keeps the group, which names the repo it belongs to
		if !p.Git.Worktree {
			label = filepath.Base(p.Git.Root)
		}
		return p.Git.Root, label
	}
	return "group:" + label, label
}

func buildView(doc fleet.Doc, ui uiState) sbView {
	v := sbView{sel: ui.sel, filter: ui.filter, focused: ui.focused, hover: ui.hover,
		prefix: ui.prefix, restore: ui.restore, quota: doc.Quota}
	for _, a := range doc.Agents {
		if a.WindowID != nil && a.State != nil && *a.State == "waiting" {
			v.waiting++
			if a.StateSince != nil && (v.waitSince == 0 || int64(*a.StateSince) < v.waitSince) {
				v.waitSince = int64(*a.StateSince)
			}
		}
	}
	if v.prefix == "" {
		v.prefix = "Ctrl-b"
	}
	if doc.Panes == nil {
		v.unknown = true
		return v
	}
	curWid := ui.myWindow
	if curWid == "" && doc.Current.WindowID != nil {
		curWid = *doc.Current.WindowID
	}

	// an agent's own pane is the one tmux reports as its agent; a crashed
	// agent's window holds only a shell, which is still the agent's pane — it
	// must not reappear under spaces as a stray shell
	agentPane := map[string]string{}
	byWin := map[string][]fleet.Pane{}
	for _, p := range *doc.Panes {
		byWin[p.WindowID] = append(byWin[p.WindowID], p)
		if p.Agent {
			agentPane[p.WindowID] = p.PaneID
		}
	}
	for _, a := range doc.Agents {
		// a record whose window is gone is not in the workspace (kel ls and
		// kel prune deal with those); the sidebar lists what is open
		if a.WindowID == nil {
			continue
		}
		wid := *a.WindowID
		// a window kel did not start is an agent only if something agent-
		// like runs in it; a `kel shell` or a hand-made window holding a
		// shell or a server belongs under its space, not here as "?"
		if !a.Managed {
			if _, ok := agentPane[wid]; !ok {
				continue
			}
		}
		if _, ok := agentPane[wid]; !ok && len(byWin[wid]) > 0 {
			first := byWin[wid][0]
			for _, p := range byWin[wid] {
				if f(p.PaneIndex) < f(first.PaneIndex) {
					first = p
				}
			}
			agentPane[wid] = first.PaneID
		}
		v.agents = append(v.agents, sbAgent{wid: wid, pane: agentPane[wid], agent: a, current: wid == curWid})
	}
	// triage order: waiting first, longest-waiting first within a state
	sort.SliceStable(v.agents, func(i, j int) bool {
		ai, aj := v.agents[i].agent, v.agents[j].agent
		ri, rj := triageRank(ai.State), triageRank(aj.State)
		if ai.Unseen {
			ri = 2 // a finished agent you have not looked at outranks idle and seen-done
		}
		if aj.Unseen {
			rj = 2
		}
		if ri != rj {
			return ri < rj
		}
		if si, sj := f(ai.StateSince), f(aj.StateSince); si != sj {
			return si < sj
		}
		return ai.Name < aj.Name
	})

	idx := map[string]int{}
	for _, p := range *doc.Panes {
		key, label := spaceKey(p)
		i, ok := idx[key]
		if !ok {
			i = len(v.spaces)
			idx[key] = i
			v.spaces = append(v.spaces, sbSpace{key: key, label: label, git: p.Git, cwd: p.Cwd, ports: &[]int{}})
		}
		s := &v.spaces[i]
		if p.WindowID == curWid {
			s.current = true
		}
		if p.Ports == nil {
			if len(*s.ports) == 0 {
				s.ports = nil
			}
		} else {
			if s.ports == nil {
				s.ports = &[]int{}
			}
			*s.ports = append(*s.ports, *p.Ports...)
		}
		if agentPane[p.WindowID] == p.PaneID {
			for _, a := range v.agents {
				if a.wid == p.WindowID && (a.agent.State == nil || *a.agent.State != "dead") {
					s.agents++
				}
				if a.wid == p.WindowID && (s.state == nil || triageRank(a.agent.State) < triageRank(s.state)) {
					s.state = a.agent.State
				}
				if a.wid == p.WindowID && a.agent.Unseen {
					s.unseen = true
				}
			}
			continue
		}
		s.panes = append(s.panes, p)
	}
	if doc.Background != nil {
		for _, b := range *doc.Background {
			if b.Git != nil {
				if i, ok := idx[b.Git.Root]; ok {
					v.spaces[i].agents++
				}
			}
		}
	}
	// every worktree of each repo, nested under it — open in a window or not,
	// kel's or Claude Code's (.claude/worktrees): the author found worktrees
	// hard to keep track of. A repo whose main checkout has no pane still
	// gets its row, to nest under
	if doc.Repos != nil {
		add := func(key string, sp sbSpace) int {
			if i, ok := idx[key]; ok {
				return i
			}
			idx[key] = len(v.spaces)
			v.spaces = append(v.spaces, sp)
			return len(v.spaces) - 1
		}
		for _, r := range *doc.Repos {
			add(r.Root, sbSpace{key: r.Root, label: r.Name, cwd: r.Root, closed: true, ports: &[]int{},
				git: &fleet.Git{Root: r.Root, Branch: r.Branch}})
			for _, w := range r.Worktrees {
				i := add(w.Path, sbSpace{key: w.Path, cwd: w.Path, closed: true, ports: &[]int{},
					git: &fleet.Git{Root: w.Path, Worktree: true, Branch: w.Branch}})
				s := &v.spaces[i]
				s.parent, s.land, s.missing, s.repo = r.Root, w.Land, w.Missing, r.Name
				s.label = filepath.Base(w.Path)
				// Claude Code's branch is the folder with "worktree-" before it
				if w.Branch != nil && !strings.Contains(w.Path, "/.claude/worktrees/") {
					s.label = *w.Branch
				}
			}
		}
	}
	for i := range v.spaces {
		s := &v.spaces[i]
		if s.ports != nil {
			sort.Ints(*s.ports)
		}
		sort.SliceStable(s.panes, func(a, b int) bool {
			pa, pb := s.panes[a], s.panes[b]
			if f(pa.WindowIndex) != f(pb.WindowIndex) {
				return f(pa.WindowIndex) < f(pb.WindowIndex)
			}
			return f(pa.PaneIndex) < f(pb.PaneIndex)
		})
		s.open = s.current
		if o, ok := ui.expanded[s.key]; ok {
			s.open = o
		}
	}
	// stable order, for muscle memory: by label, the main checkout before its
	// worktrees, then by path; a repo's listed worktrees right under it
	sort.SliceStable(v.spaces, func(i, j int) bool {
		si, sj := v.spaces[i], v.spaces[j]
		if si.label != sj.label {
			return si.label < sj.label
		}
		wi, wj := si.git != nil && si.git.Worktree, sj.git != nil && sj.git.Worktree
		if wi != wj {
			return !wi
		}
		return si.key < sj.key
	})
	{
		kids := map[string][]sbSpace{}
		var tops []sbSpace
		for _, s := range v.spaces {
			if _, ok := idx[s.parent]; s.parent != "" && ok {
				kids[s.parent] = append(kids[s.parent], s)
			} else {
				tops = append(tops, s)
			}
		}
		v.spaces = v.spaces[:0:0]
		for _, t := range tops {
			v.spaces = append(v.spaces, t)
			v.spaces = append(v.spaces, kids[t.key]...)
		}
	}

	if ui.filter != "" {
		v.applyFilter(strings.ToLower(ui.filter))
	}
	if doc.Background != nil {
		names := map[string]string{}
		for _, a := range v.agents {
			names[a.agent.Name] = a.wid
		}
		v.bgUnder = map[string][]fleet.Background{}
		for _, b := range *doc.Background {
			if ui.filter != "" && !strings.Contains(strings.ToLower(b.Name), strings.ToLower(ui.filter)) {
				continue
			}
			if wid, ok := names[b.Name]; ok {
				v.bgUnder[wid] = append(v.bgUnder[wid], b)
			} else {
				v.bgLoose = append(v.bgLoose, b)
			}
		}
	}
	for _, s := range v.spaces {
		v.entryIDs = append(v.entryIDs, spaceID(s.key))
		if s.open {
			for _, p := range s.panes {
				v.entryIDs = append(v.entryIDs, p.PaneID)
			}
		}
	}
	for _, a := range v.agents {
		v.entryIDs = append(v.entryIDs, agentID(a.wid))
		for _, b := range v.bgUnder[a.wid] {
			v.entryIDs = append(v.entryIDs, bgID(b.ID))
		}
	}
	for _, b := range v.bgLoose {
		v.entryIDs = append(v.entryIDs, bgID(b.ID))
	}
	if v.indexOf(v.sel) < 0 {
		v.sel = ""
		for _, a := range v.agents {
			if a.current {
				v.sel = agentID(a.wid)
			}
		}
		if v.sel == "" && len(v.entryIDs) > 0 {
			v.sel = v.entryIDs[0]
		}
	}
	return v
}

// applyFilter keeps what matches by name; a space survives if it or any of
// its panes matches, and opens to show the match.
func (v *sbView) applyFilter(q string) {
	var ss []sbSpace
	for _, s := range v.spaces {
		hit := strings.Contains(strings.ToLower(s.label), q) ||
			(s.git != nil && s.git.Branch != nil && strings.Contains(strings.ToLower(*s.git.Branch), q))
		var ps []fleet.Pane
		for _, p := range s.panes {
			if strings.Contains(strings.ToLower(paneLabel(p)), q) {
				ps = append(ps, p)
			}
		}
		if hit {
			ss = append(ss, s)
		} else if len(ps) > 0 {
			s.panes, s.open = ps, true
			ss = append(ss, s)
		}
	}
	v.spaces = ss
	var as []sbAgent
	for _, a := range v.agents {
		if strings.Contains(strings.ToLower(a.agent.Name), q) || strings.Contains(strings.ToLower(a.agent.Group), q) {
			as = append(as, a)
		}
	}
	v.agents = as
}

func (v sbView) indexOf(id string) int {
	for i, e := range v.entryIDs {
		if e == id {
			return i
		}
	}
	return -1
}

// ---------------------------------------------------------------- drawing

// stateLook is §5d's table: one colour and one dot per state. Unknown is its
// own look, never idle's.
func stateLook(state *string, unseen bool) (dot, colour string) {
	if state == nil || *state == "" {
		return "?", dim
	}
	switch *state {
	case "waiting":
		return "●", yellow + bold
	case "working":
		return "●", cyan
	case "done":
		if unseen {
			return "●", green
		}
		return "○", dim
	case "throttled":
		return "◌", purple
	case "dead":
		return "✕", red
	case "idle":
		return "○", dim
	}
	return "?", dim
}

func paneLabel(p fleet.Pane) string {
	l := p.Cmd
	if p.Kind == "server" {
		if p.Ports == nil {
			l += " :?"
		} else {
			for _, n := range *p.Ports {
				l += fmt.Sprintf(" :%d", n)
			}
		}
	}
	return l
}

func paneGlyph(kind string) string {
	switch kind {
	case "server":
		return "▪"
	case "shell":
		return dim + "$" + reset
	}
	return dim + "▸" + reset
}

// spaceDetail is the second line: branch and position, then ports. Unknowns
// are a dim ?, never a clean-looking blank (invariant 3).
func spaceDetail(s sbSpace) string {
	var parts []string
	switch g := s.git; {
	case s.closed && g != nil:
		// known from git alone: no window stands here to say more
		b := "detached"
		if g.Branch != nil {
			b = *g.Branch
		}
		return b + " · no window"
	case g == nil:
		parts = append(parts, tildify(s.cwd))
	case g.Branch == nil && g.Dirty == nil:
		parts = append(parts, "?")
	default:
		b := "detached"
		if g.Branch != nil {
			b = *g.Branch
		}
		pos := b
		if g.Ahead != nil && *g.Ahead > 0 {
			pos += fmt.Sprintf(" ↑%d", *g.Ahead)
		}
		if g.Behind != nil && *g.Behind > 0 {
			pos += fmt.Sprintf(" ↓%d", *g.Behind)
		}
		if g.Dirty == nil {
			pos += " ●?"
		} else if *g.Dirty > 0 {
			pos += fmt.Sprintf(" ●%d", *g.Dirty)
		}
		parts = append(parts, pos)
	}
	if s.ports == nil {
		parts = append(parts, ":?")
	} else if len(*s.ports) > 0 {
		ps := make([]string, len(*s.ports))
		for i, n := range *s.ports {
			ps[i] = fmt.Sprintf(":%d", n)
		}
		parts = append(parts, strings.Join(ps, " "))
	}
	if s.agents > 1 {
		// passive: several agents in one checkout is allowed, just said
		parts = append(parts, fmt.Sprintf("%d agents", s.agents))
	}
	return strings.Join(parts, " · ")
}

// worktreeDetail is a worktree row's tail, coloured: where it stands, then
// what landing it needs. Open ones read git live (pane.Git); closed ones the
// land check alone. merged is green — it is what `s` sweeps.
func worktreeDetail(s sbSpace) string {
	if s.missing {
		return red + "missing" + reset
	}
	var parts []string
	if !s.closed && s.git != nil {
		g := s.git
		pos := ""
		if g.Ahead != nil && *g.Ahead > 0 {
			pos += fmt.Sprintf("↑%d", *g.Ahead)
		}
		if g.Dirty == nil {
			pos = strings.TrimSpace(pos + " ●?")
		} else if *g.Dirty > 0 {
			pos = strings.TrimSpace(fmt.Sprintf("%s ●%d", pos, *g.Dirty))
		}
		if pos != "" {
			parts = append(parts, dim+pos+reset)
		}
	}
	if l := s.land; l == nil {
		parts = append(parts, dim+"?"+reset)
	} else {
		n := 0
		if l.N != nil {
			n = int(*l.N)
		}
		switch l.Code {
		case "merged":
			parts = append(parts, green+"merged"+reset)
		case "review":
			parts = append(parts, dim+"review"+reset)
		case "no_pr":
			parts = append(parts, dim+"no PR"+reset)
		case "behind":
			parts = append(parts, dim+fmt.Sprintf("behind %d", n)+reset)
		case "unknown":
			parts = append(parts, dim+"?"+reset)
		case "dirty":
			if s.closed {
				parts = append(parts, dim+fmt.Sprintf("●%d", n)+reset)
			}
		case "unpushed":
			if s.closed {
				parts = append(parts, dim+fmt.Sprintf("↑%d unpushed", n)+reset)
			}
		}
	}
	if s.agents > 1 {
		parts = append(parts, dim+fmt.Sprintf("%d agents", s.agents)+reset)
	}
	return strings.Join(parts, dim+" · "+reset)
}

// truncateStyled cuts a string holding colour codes to w visible columns.
func truncateStyled(s string, w int) string {
	var b strings.Builder
	n, esc := 0, false
	r := []rune(s)
	for i, c := range r {
		switch {
		case c == '\x1b':
			esc = true
		case esc:
			if c == 'm' {
				esc = false
			}
		default:
			if n == w-1 && visibleAfter(r[i+1:]) {
				b.WriteString("…" + reset)
				return b.String()
			}
			n++
		}
		b.WriteRune(c)
	}
	return b.String()
}

func visibleAfter(r []rune) bool {
	esc := false
	for _, c := range r {
		switch {
		case c == '\x1b':
			esc = true
		case esc:
			if c == 'm' {
				esc = false
			}
		default:
			return true
		}
	}
	return false
}

func tildify(p string) string {
	if h := os.Getenv("HOME"); h != "" && (p == h || strings.HasPrefix(p, h+string(filepath.Separator))) {
		return "~" + strings.TrimPrefix(p, h)
	}
	return p
}

// quietFor: a `working` agent kel has heard nothing from — no state change, no
// statusline update — for half an hour. An interrupt (esc) fires no Stop, and
// the author saw "working 14h28m" on an agent sitting at its prompt. Claude
// Code's statusLine updates through a working turn, so this much silence is
// not work; it reads "working?", never a confident "working" (invariant 3).
// Window activity was the first idea and does not work: the window's own
// sidebar redraws in it.
func quietFor(a fleet.Agent, now int64) int64 {
	if a.State == nil || *a.State != "working" {
		return 0
	}
	last := f(a.StateSince)
	if a.Context != nil && f(a.Context.At) > last {
		last = f(a.Context.At)
	}
	if last == 0 || now-int64(last) < 1800 {
		return 0
	}
	return now - int64(last)
}

// shortNote turns Claude Code's notification text into the reason, short
// enough for a 32-column line: what it is waiting for.
func shortNote(n string) string {
	n = strings.TrimSpace(n)
	if t := strings.TrimPrefix(n, "Claude needs your permission to use "); t != n {
		return "allow " + strings.TrimSuffix(t, ".") + "?"
	}
	if strings.HasPrefix(n, "Claude is waiting for your input") {
		return "your input"
	}
	return n
}

// agentDetail: why it is in this state and for how long, then context and
// cost — or, when it waits on you, what it is waiting for (Orca shows
// "awaiting permission"; kel had the exact text all along and showed none of
// it). The state word carries the state's colour; the rest is dim.
// Plain text out, styles beside it: the renderer truncates first and colours
// after, or the escape codes count as characters and cut the line short.
func agentDetail(a fleet.Agent, now int64) (word, colour, rest, restColour string) {
	if q := quietFor(a, now); q > 0 {
		// grey italic, the whole line: kel is not sure, and says so in how it
		// looks as well as in the `?`
		return "working?", dim + italic, " · quiet " + humanizeSecs(q), dim + italic
	}
	_, colour = stateLook(a.State, a.Unseen)
	word = "?"
	if a.State != nil && *a.State != "" {
		word = *a.State
		if a.StateSince != nil {
			word += " " + humanizeSecs(now-int64(*a.StateSince))
		}
	}
	// calm states (idle, a done you have seen, unknown) keep stateLook's
	// grey: only what needs you stands out of the list
	if a.State != nil && *a.State == "waiting" && a.Note != nil && *a.Note != "" {
		// the reason is the point of the line: readable, not grey
		return word, colour, " · " + shortNote(*a.Note), ""
	}
	if a.Context == nil {
		return word, colour, " · ctx ?", dim
	}
	pct := "ctx ?"
	if a.Context.Pct != nil {
		pct = fmt.Sprintf("%.0f%%", *a.Context.Pct)
	}
	c := "$?"
	if a.Context.CostUSD != nil {
		c = money(*a.Context.CostUSD)
	}
	return word, colour, " · " + pct + " · " + c, dim
}

// renderView draws the view at w×h. h == 0 means "no size yet": draw it all.
// It returns, for every screen line, the entry the line belongs to.
func renderView(v sbView, w, h int, now int64) (string, []lineRef) {
	if w <= 0 && h <= 0 {
		w = 32
	}
	type line struct {
		s   string
		ref lineRef
	}
	var body []line
	add := func(s string, r lineRef) { body = append(body, line{s, r}) }
	// every entry line starts with one column, the selection bar. Where you
	// are is the name in bold — the author found `▌›○` three marks too many
	// before a name, so the `›` marker went (2026-10-15)
	lead := func(id string) string {
		if id == v.sel {
			return accent + "▌" + reset
		}
		return " "
	}
	weight := func(s string, current bool) string {
		if current {
			return bold + s + reset
		}
		return s
	}
	text := w - 3 // lead + dot + space

	add(dim+truncate("spaces", w)+reset, lineRef{})
	if v.unknown {
		add("  "+dim+truncate("? panes unknown", w-2)+reset, lineRef{})
	} else if len(v.spaces) == 0 {
		add("  "+dim+truncate("nothing open", w-2)+reset, lineRef{})
	}
	for _, s := range v.spaces {
		id := spaceID(s.key)
		r := lineRef{id: id, kind: "space"}
		dot := " "
		if s.state != nil {
			d, c := stateLook(s.state, s.unseen)
			dot = c + d + reset
		}
		indent := ""
		if s.parent != "" {
			// a worktree: one line under its repo, the branch and what
			// landing it needs — `└ ● fix-auth  ↑2 · review`
			// the state is what the row is for, so a long name gives way to
			// it (Claude Code names worktrees agent-af1a1567ab46ddb66)
			d := worktreeDetail(s)
			dw := len([]rune(stripANSI(d)))
			avail := w - 4 // after the bar, └, the dot and a space
			lw := avail
			if d != "" && len([]rune(s.label))+2+dw > avail {
				lw = max(avail-2-dw, 8)
			}
			label := truncate(s.label, lw)
			if s.closed {
				label = dim + label + reset
			}
			row := lead(id) + dim + "└" + reset + dot + " " + weight(label, s.current)
			if d != "" {
				if room := avail - len([]rune(stripANSI(label))) - 2; room > 0 {
					row += "  " + truncateStyled(d, room)
				}
			}
			add(row, r)
			indent = "  "
		} else {
			add(lead(id)+dot+" "+weight(truncate(s.label, text), s.current), r)
			add("   "+dim+truncate(spaceDetail(s), text)+reset, r)
		}
		if !s.open {
			continue
		}
		// two panes that would read the same (`$ bash`, `$ bash`) are told
		// apart by their window's name
		seen := map[string]int{}
		for _, p := range s.panes {
			seen[paneLabel(p)]++
		}
		for _, p := range s.panes {
			pr := lineRef{id: p.PaneID, kind: "pane"}
			label := truncate(paneLabel(p), w-5)
			if seen[paneLabel(p)] > 1 && p.WindowName != "" {
				if room := w - 5 - len([]rune(label)) - 3; room > 0 {
					label += dim + " · " + truncate(p.WindowName, room) + reset
				}
			}
			add(lead(p.PaneID)+indent+" "+paneGlyph(p.Kind)+" "+weight(label, p.Current), pr)
		}
	}

	add("", lineRef{})
	add(dim+truncate("agents", w)+reset, lineRef{})
	if v.unknown {
		add("  "+dim+truncate("? panes unknown", w-2)+reset, lineRef{})
	} else if len(v.agents) == 0 {
		add("  "+dim+truncate("none", w-2)+reset, lineRef{})
	}
	for _, a := range v.agents {
		id := agentID(a.wid)
		r := lineRef{id: id, kind: "agent"}
		d, c := stateLook(a.agent.State, a.agent.Unseen)
		if quietFor(a.agent, now) > 0 {
			d, c = "◌", dim // not heard from: no state colour it has not earned
		}
		name := weight(truncate(a.agent.Name, text), a.current)
		// the repo, dim beside the name, when there is room: two repos can
		// each have an agent called docs
		if g := a.agent.Group; g != "" && g != a.agent.Name && len([]rune(a.agent.Name))+2+len([]rune(g)) <= text {
			name += "  " + dim + g + reset
		}
		add(lead(id)+c+d+reset+" "+name, r)
		word, wc, rest, rc := agentDetail(a.agent, now)
		word = truncate(word, text)
		rest = truncate(rest, text-len([]rune(word)))
		add("   "+wc+word+reset+rc+rest+reset, r)
		// subagents: the Task-tool helpers this agent has running, under it
		for _, sub := range a.agent.Subagents {
			d, c := subLook(sub.Status)
			label := sub.Label
			if sub.AgentType != "" && sub.AgentType != "general-purpose" {
				label += " · " + sub.AgentType
			}
			add("   "+dim+"└"+reset+" "+c+d+reset+" "+dim+truncate(label, w-7)+reset, r)
		}
		for _, b := range v.bgUnder[a.wid] {
			d, c, w2 := bgLook(b)
			bid := bgID(b.ID)
			label := truncate(b.Name+" · bg · "+w2, w-7)
			add(lead(bid)+"  "+dim+"└"+reset+" "+c+d+reset+" "+label, lineRef{id: bid, kind: "bg"})
		}
	}
	for _, b := range v.bgLoose {
		d, c, w2 := bgLook(b)
		bid := bgID(b.ID)
		add(lead(bid)+c+d+reset+" "+truncate(b.Name, text)+"  "+dim+"bg"+reset, lineRef{id: bid, kind: "bg"})
		add("   "+c+w2+reset+dim+truncate(" · "+b.Group, text-len([]rune(w2)))+reset, lineRef{id: bid, kind: "bg"})
	}

	// actions: what does not depend on the selection, as keys you can also
	// click — the author found the lower half empty and keys hard to recall.
	// Only when the height leaves room below the lists; the same keys as
	// everywhere, so it is not a second surface (invariant 6).
	var acts []line
	if h > 0 {
		acts = append(acts, line{dim + truncate("actions", w) + reset, lineRef{}})
		list := sbActions
		if v.restore {
			// one line at startup was the only mention: until it is used, the
			// previous workspace stays one click away
			list = append([][2]string{{"R", "restore last workspace"}}, list...)
		}
		for _, a := range list {
			acts = append(acts, line{" " + bold + a[0] + reset + "  " + dim + truncate(a[1], w-4) + reset,
				lineRef{id: "act:" + a[0], kind: "action"}})
		}
	}

	// The footer teaches the keys by showing the ones that fit what is
	// selected, so nothing has to be remembered and nothing is added: one
	// line, as before. Without the keyboard it says only how to get it.
	footer, fc := hint(v, w), dim
	if v.filter != "" {
		footer = "/" + v.filter + "  esc clears"
	}
	if v.footer != "" {
		footer, fc = v.footer, v.footerColour
	}
	foot := line{fc + truncate(footer, w) + reset, lineRef{}}
	// the waiting alarm, which tmux's bar carried until the sidebar took its
	// place: while you are elsewhere, it is the footer, and a click jumps
	if !v.focused && v.footer == "" && v.filter == "" && v.waiting > 0 {
		t := fmt.Sprintf("%d waiting", v.waiting)
		if v.waitSince > 0 && now >= v.waitSince {
			t += " " + humanizeSecs(now-v.waitSince)
		}
		foot = line{yellow + bold + truncate(t+" · click to jump", w) + reset, lineRef{id: "act:`", kind: "action"}}
	}
	// the quota is the last line, where tmux's bar used to carry it: with
	// the sidebar showing, kel turns that bar off (it repeated what the
	// sidebar and Claude's own row already said — the author, 2026-10-16)
	var qline *line
	if q := quotaLine(v.quota, w, now); q != "" && h > 3 {
		qline = &line{q, lineRef{}}
	}
	var foot2 *line
	if v.hint2 != "" {
		foot2 = &line{dim + truncate(v.hint2, w) + reset, lineRef{id: "hint2", kind: "hint2"}}
	}

	// scroll so the selected entry is on screen; the footer is pinned
	if h > 0 {
		room := h - 1
		if qline != nil {
			room--
		}
		if room < 0 {
			room = 0
		}
		off := 0
		if len(body) > room {
			first, last := -1, -1
			for i, l := range body {
				if l.ref.id != "" && l.ref.id == v.sel {
					if first < 0 {
						first = i
					}
					last = i
				}
			}
			if last >= room {
				off = last - room + 1
			}
			if first >= 0 && first < off {
				off = first
			}
			body = body[off:]
			if len(body) > room {
				body = body[:room]
			}
		}
		// the actions sit at the bottom, above the footer, when they fit
		// under everything else with a blank line between
		// and a blank line under them, so the footer does not read as one
		// more action
		if len(body)+1+len(acts)+1 <= room {
			for len(body)+len(acts)+1 < room {
				body = append(body, line{"", lineRef{}})
			}
			body = append(body, acts...)
		}
		for len(body) < room {
			body = append(body, line{"", lineRef{}})
		}
	}
	if foot2 != nil && h > 2 && len(body) > 0 {
		// a prompt's choices take the last body line: `enter ok · esc cancel`
		body[len(body)-1] = *foot2
	}
	if h != 1 || len(body) == 0 {
		body = append(body, foot)
	}
	if qline != nil {
		body = append(body, *qline)
	}
	if h > 0 && len(body) > h {
		body = body[:h]
	}
	// hover: the entry under the pointer gets a dim bar where the selection's
	// would be, so what a click would hit is visible before the click
	if v.hover > 0 && v.hover <= len(body) {
		if id := body[v.hover-1].ref.id; id != "" && id != v.sel {
			for i, l := range body {
				if l.ref.id == id && strings.HasPrefix(l.s, " ") {
					body[i].s = dim + "▏" + reset + l.s[1:]
				}
			}
		}
	}
	var b strings.Builder
	refs := make([]lineRef, len(body))
	for i, l := range body {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(l.s)
		refs[i] = l.ref
	}
	return b.String(), refs
}

// hint is the footer for the selected entry: its keys, most useful first, as
// many as fit, then `?` for the rest when there is room.
func hint(v sbView, w int) string {
	if !v.focused {
		// how to get in, in words: the author did not know "prefix" was Ctrl-b
		// "Ctrl-b space, or click" read as one chord plus a click: the
		// prefix is pressed and let go, then the key
		return "click, or " + v.prefix + " then space"
	}
	var keys []string
	switch {
	case strings.HasPrefix(v.sel, "a:"):
		// enter is the one key everybody tries; the room goes to the rest
		keys = []string{"e edit", "p plan", "x kill", "r rename", "enter"}
	case strings.HasPrefix(v.sel, "s:"):
		keys = []string{"f files", "g git", "e edit", "d diff", "n new", "t shell", "c claude", "G prs"}
		for _, s := range v.spaces {
			if spaceID(s.key) == v.sel && s.land != nil && s.land.Code == "merged" && s.closed {
				keys = append([]string{"s sweep"}, keys...) // what it is there for
			}
		}
	default: // a pane
		keys = []string{"f files", "g git", "e edit", "enter"}
		for _, s := range v.spaces {
			for _, p := range s.panes {
				if p.PaneID == v.sel && p.Kind == "server" {
					keys = []string{"o open", "f files", "g git", "enter"}
				}
			}
		}
	}
	out := ""
	for _, k := range keys {
		next := k
		if out != "" {
			next = out + " · " + k
		}
		if len([]rune(next))+len([]rune(" · ?")) > w && out != "" {
			break
		}
		out = next
	}
	if len([]rune(out))+len([]rune(" · ?")) <= w {
		out += " · ?"
	}
	return out
}

// sbActions are the keys that do not depend on the selection, shown at the
// foot of the sidebar when there is room, and clickable.
var sbActions = [][2]string{
	{"n", "new agent"},
	{"w", "new worktree agent"},
	{"t", "shell here"},
	{"`", "jump to who waits"},
	{"u", "usage"},
	{";", "kel command"}, // not `:` — that needs shift
	{"Q", "quit kel"},
	{"?", "all keys"},
}

// quotaLine is the account's quota as one line: the 5-hour window as a bar,
// its reset, the week, and spend once it matters. What does not fit goes,
// least needed first; nothing reported is no line (never a made-up 0%).
func quotaLine(q *fleet.Quota, w int, now int64) string {
	if q == nil || w <= 0 {
		return ""
	}
	heat := func(p int) string {
		switch {
		case p >= 90:
			return red + bold
		case p >= 70:
			return yellow
		}
		return ""
	}
	type seg struct{ plain, col string }
	var five, reset5, week, spend *seg
	if f := q.FiveHour; f != nil {
		if f.Pct == nil {
			five = &seg{"█████ ?", dim + "█████" + reset + " " + dim + "?" + reset}
		} else {
			p := int(*f.Pct)
			n := (p + 19) / 20
			if n > 5 {
				n = 5
			}
			bar := strings.Repeat("█", n)
			track := strings.Repeat("█", 5-n)
			pct := fmt.Sprintf("%d%%", p)
			five = &seg{bar + track + " " + pct, heat(p) + bar + reset + dim + track + reset + " " + heat(p) + pct + reset}
			if f.ResetsAt != nil && int64(*f.ResetsAt) > now {
				d := humanizeSecs(int64(*f.ResetsAt) - now)
				reset5 = &seg{" " + d, dim + " " + d + reset}
			}
		}
	}
	if s := q.SevenDay; s != nil {
		if s.Pct == nil {
			week = &seg{"wk ?", dim + "wk ?" + reset}
		} else {
			p := int(*s.Pct)
			t := fmt.Sprintf("wk %d%%", p)
			week = &seg{t, heat(p) + t + reset}
		}
	}
	if s := q.Spend; s != nil && s.Pct != nil && *s.Pct >= 50 {
		p := int(*s.Pct)
		t := fmt.Sprintf("spend %d%%", p)
		spend = &seg{t, heat(p) + t + reset}
	}
	build := func(label bool, parts ...*seg) (string, int) {
		var plain, col []string
		head := ""
		for _, p := range parts {
			if p == nil {
				continue
			}
			if p == reset5 { // glued to the 5-hour figure
				if len(plain) > 0 {
					plain[len(plain)-1] += p.plain
					col[len(col)-1] += p.col
				}
				continue
			}
			plain, col = append(plain, p.plain), append(col, p.col)
		}
		if len(plain) == 0 {
			return "", 0
		}
		if label {
			head = "claude "
		}
		n := len([]rune(head + strings.Join(plain, " · ")))
		return dim + head + reset + strings.Join(col, dim+" · "+reset), n
	}
	for _, try := range [][]*seg{
		{five, reset5, week, spend},
		{five, reset5, week},
		{five, week, spend},
		{five, week},
		{five},
	} {
		for _, label := range []bool{true, false} {
			if s, n := build(label, try...); s != "" && n <= w {
				return s
			}
		}
	}
	return ""
}

var ansiRE = regexp.MustCompile("\x1b\\[[0-9;]*m")

func stripANSI(s string) string { return ansiRE.ReplaceAllString(s, "") }

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
	"sort"
	"strings"

	"github.com/tharpep/kelsier/internal/fleet"
)

// §5d: the 16 ANSI colours only, and colour means state. blue is the one
// accent — no state uses it, so the selection can never read as a state.
const accent = "\x1b[34m"

// uiState is what the user has done to the view; it is not data.
type uiState struct {
	sel      string          // selected entry id (see sbEntry.id)
	expanded map[string]bool // explicit toggles by space key; absent = default
	filter   string
	myWindow string // the window this sidebar sits in — current, whatever the doc says
	focused  bool   // the keyboard is in this sidebar
	hover    int    // screen line under the pointer, plus one; 0 = none
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
}

type sbAgent struct {
	wid     string
	pane    string // the pane to jump to; "" when the window lists none
	agent   fleet.Agent
	current bool
}

type sbView struct {
	spaces   []sbSpace
	agents   []sbAgent
	unknown  bool // the fleet could not list panes: say so, never "nothing open"
	sel      string
	filter   string
	entryIDs []string // selectable ids in screen order, for j/k

	footer, footerColour string // a prompt or a message; "" = the key hint
	focused              bool
	hover                int
}

// lineRef says which entry a screen line belongs to — what a click hits.
type lineRef struct {
	id   string // "" for labels, blanks and the footer
	kind string // space | pane | agent
}

func spaceID(key string) string { return "s:" + key }
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
	v := sbView{sel: ui.sel, filter: ui.filter, focused: ui.focused, hover: ui.hover}
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
	// worktrees, then by path
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

	if ui.filter != "" {
		v.applyFilter(strings.ToLower(ui.filter))
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
	return strings.Join(parts, " · ")
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
func agentDetail(a fleet.Agent, now int64) (word, colour, rest string) {
	if q := quietFor(a, now); q > 0 {
		return "working?", "", " · quiet " + humanizeSecs(q)
	}
	_, colour = stateLook(a.State, a.Unseen)
	word = "?"
	if a.State != nil && *a.State != "" {
		word = *a.State
		if a.StateSince != nil {
			word += " " + humanizeSecs(now-int64(*a.StateSince))
		}
	}
	if colour == dim {
		colour = ""
	}
	if a.State != nil && *a.State == "waiting" && a.Note != nil && *a.Note != "" {
		return word, colour, " · " + shortNote(*a.Note)
	}
	if a.Context == nil {
		return word, colour, " · ctx ?"
	}
	pct := "ctx ?"
	if a.Context.Pct != nil {
		pct = fmt.Sprintf("%.0f%%", *a.Context.Pct)
	}
	c := "$?"
	if a.Context.CostUSD != nil {
		c = money(*a.Context.CostUSD)
	}
	return word, colour, " · " + pct + " · " + c
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
	// every entry line starts with two columns: the selection bar and the
	// current marker, so text lines up whether or not either is shown
	lead := func(id string, current bool) string {
		b := " "
		if id == v.sel {
			b = accent + "▌" + reset
		}
		c := " "
		if current {
			c = "›"
		}
		return b + c
	}
	text := w - 4 // lead + dot + space

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
		add(lead(id, s.current)+dot+" "+bold+truncate(s.label, text)+reset, r)
		add("    "+dim+truncate(spaceDetail(s), text)+reset, r)
		if !s.open {
			continue
		}
		for _, p := range s.panes {
			pr := lineRef{id: p.PaneID, kind: "pane"}
			add(lead(p.PaneID, p.Current)+"  "+paneGlyph(p.Kind)+" "+truncate(paneLabel(p), w-6), pr)
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
		name := truncate(a.agent.Name, text)
		// the repo, dim beside the name, when there is room: two repos can
		// each have an agent called docs
		if g := a.agent.Group; g != "" && g != a.agent.Name && len([]rune(name))+2+len([]rune(g)) <= text {
			name = bold + name + reset + "  " + dim + g + reset
		} else {
			name = bold + name + reset
		}
		add(lead(id, a.current)+c+d+reset+" "+name, r)
		word, wc, rest := agentDetail(a.agent, now)
		word = truncate(word, text)
		rest = truncate(rest, text-len([]rune(word)))
		add("    "+wc+word+reset+dim+rest+reset, r)
	}

	// actions: what does not depend on the selection, as keys you can also
	// click — the author found the lower half empty and keys hard to recall.
	// Only when the height leaves room below the lists; the same keys as
	// everywhere, so it is not a second surface (invariant 6).
	var acts []line
	if h > 0 {
		acts = append(acts, line{dim + truncate("actions", w) + reset, lineRef{}})
		for _, a := range sbActions {
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

	// scroll so the selected entry is on screen; the footer is pinned
	if h > 0 {
		room := h - 1
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
		if len(body)+1+len(acts) <= room {
			for len(body)+len(acts) < room {
				body = append(body, line{"", lineRef{}})
			}
			body = append(body, acts...)
		}
		for len(body) < room {
			body = append(body, line{"", lineRef{}})
		}
	}
	if h != 1 || len(body) == 0 {
		body = append(body, foot)
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
		return "prefix space"
	}
	var keys []string
	switch {
	case strings.HasPrefix(v.sel, "a:"):
		// enter is the one key everybody tries; the room goes to the rest
		keys = []string{"e edit", "p plan", "x kill", "r rename", "enter"}
	case strings.HasPrefix(v.sel, "s:"):
		keys = []string{"f files", "g git", "e edit", "d diff", "n new", "t shell"}
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
	{"Q", "quit kel"},
	{"?", "all keys"},
}

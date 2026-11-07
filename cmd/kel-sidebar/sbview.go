package main

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/tharpep/kelsier/internal/fleet"
)

// §5d: colour means state. blue is the one accent — no state uses it, so the
// selection can never read as a state.
const accent = "\x1b[38;2;106;167;248m"

const italic = "\x1b[3m"

// uiState is what the user has done to the view; it is not data.
type uiState struct {
	sel      string          // selected entry id (see sbEntry.id)
	expanded map[string]bool
	now      int64
	filter   string
	myWindow string // the window this sidebar sits in — current, whatever the doc says
	focused  bool   // the keyboard is in this sidebar
	hover    int    // screen line under the pointer, plus one; 0 = none
	hoverX   int    // and its column
	hoverID  string // the entry it is on
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
	// phase 2: a repo's worktrees nest under it (fleet.Repo)
	parent     string      // the main checkout's key, for a linked worktree
	land       *fleet.Land // a linked worktree's land state; nil = not known
	closed     bool        // no pane stands here: known from git, not tmux
	missing    bool        // a worktree whose directory is gone
	agents     int         // agents working in this checkout
	repo       string      // the repo's name, for a worktree whose label is its branch
	folded     bool
	wtFold     bool
	wtOpen     bool
	wtHidden   int
	wtUnpushed int
}

const wtFoldAt = 3

func wtID(key string) string { return "w:" + key }

func (s sbSpace) wtSummary() bool { return s.wtFold && (s.wtOpen || s.wtHidden > 0) }

func (s sbSpace) wtText() string {
	if s.wtOpen {
		return "− worktrees"
	}
	t := fmt.Sprintf("+%d worktrees", s.wtHidden)
	if s.wtHidden == 1 {
		t = "+1 worktree"
	}
	if s.wtUnpushed > 0 {
		t += fmt.Sprintf("  %d unpushed", s.wtUnpushed)
	}
	return t
}

func wtKey(key string) string { return "wt:" + key }

type sbAgent struct {
	repo string // the repo its pane stands in — not its record's group,
	// which says where it was filed: the author's `newer` read "kelsier"
	// while it worked in ai-context
	wid     string
	pane    string // the pane to jump to; "" when the window lists none
	key     string
	shared  bool
	agent   fleet.Agent
	current bool
	quiet   string
	folded  bool
	git     *fleet.Git
	cwd     string
}

type sbShell struct {
	pane  fleet.Pane
	where string
}

type sbQuiet struct {
	idle, unknown int
	open          bool
}

func (q sbQuiet) text() string {
	var parts []string
	if q.idle > 0 {
		parts = append(parts, fmt.Sprintf("%d idle 1d+", q.idle))
	}
	if q.unknown > 0 {
		parts = append(parts, fmt.Sprintf("%d unknown", q.unknown))
	}
	mark := "+"
	if q.open {
		mark = "−"
	}
	return mark + " " + strings.Join(parts, " · ")
}

const (
	reposID    = "r:repos"
	reposKey   = "sec:repos"
	quietAfter = 24 * 3600
)

func quietID(repo string) string  { return "x:" + repo }
func quietKey(repo string) string { return "quiet:" + repo }

func quietKind(a fleet.Agent, now int64) string {
	since := a.StateSince
	unknown := a.State == nil || *a.State == ""
	if since == nil && unknown {
		since = a.Activity
	}
	if since == nil || now-int64(*since) < quietAfter {
		return ""
	}
	switch {
	case unknown:
		return "unknown"
	case *a.State == "idle":
		return "idle"
	}
	return ""
}

type sbView struct {
	shells    []sbShell
	reposOpen bool
	quiet     map[string]sbQuiet // by repo: the group's quiet fold, if it has one
	spaces    []sbSpace
	agents    []sbAgent
	needs     []sbAgent
	// bg: background agents (`claude --bg`), each nested under the top agent
	// of the same name when there is one — Claude Code does not say which
	// agent dispatched it, and a same-named one is how the author makes them
	bgUnder  map[string][]fleet.Background // agent window id -> its children
	bgLoose  []fleet.Background            // no such agent: listed on their own
	unknown  bool                          // the fleet could not list panes: say so, never "nothing open"
	sel      string
	filter   string
	entryIDs []string // selectable ids in screen order, for j/k

	footer, footerColour string // the last command's word; "" = how to get in, or the keys
	promptTitle          string // a prompt's question: "new agent in kelsier"
	promptColour         string // its colour; "" = the accent
	input                string // what is being typed, with its cursor
	hint2                string // a prompt's choices, clickable: "enter ok · esc cancel"
	focused              bool
	hover                int
	hoverX               int
	hoverID              string
	restore              bool
	menu                 *sbMenu // open over the rows, or nil
	drawer               *sbDrawer
}

// lineRef says which entry a screen line belongs to — what a click hits.
type lineRef struct {
	id   string // "" for labels, blanks and the footer
	kind string // space | pane | agent
	// a line of two actions side by side: from column split on, a click
	// is id2's (the actions grid)
	id2   string
	split int
	right string // that action, drawn — kept here so the hover can mark it
	from  int    // a click left of this column hits nothing
	hits  []hit  // buttons drawn on the line
	sum   bool   // an agent's summary line (spec §5d)
	text  string // a choices line as drawn, without colour, for hintKey
}

// hit is a button on a line: columns [x0, x1) press key ("menu" opens the
// entry's own menu).
type hit struct {
	x0, x1 int
	key    string
}

// line is one screen line and what a click on it hits.
type line struct {
	s   string
	ref lineRef
}

// at is the entry a click at column x hits.
func (r lineRef) at(x int) string {
	if x < r.from {
		return ""
	}
	if r.id2 != "" && x >= r.split {
		return r.id2
	}
	return r.id
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
func needID(key string) string  { return "q:" + key }

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
	v := sbView{sel: ui.sel, filter: ui.filter, focused: ui.focused, hover: ui.hover, hoverX: ui.hoverX, hoverID: ui.hoverID,
		restore: ui.restore}
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
	isAgentPane := map[string]bool{}
	byWin := map[string][]fleet.Pane{}
	for _, p := range *doc.Panes {
		byWin[p.WindowID] = append(byWin[p.WindowID], p)
		if p.Agent {
			if _, ok := agentPane[p.WindowID]; !ok {
				agentPane[p.WindowID] = p.PaneID
			}
			isAgentPane[p.PaneID] = true
		}
	}
	for _, a := range doc.Agents {
		// a record whose window is gone is not in the workspace (kel ls and
		// kel prune deal with those); the sidebar lists what is open
		if a.WindowID == nil {
			continue
		}
		wid := *a.WindowID
		pane, key := "", wid
		if a.PaneID != nil {
			pane, key = *a.PaneID, *a.PaneID
		}
		// a window kel did not start is an agent only if something agent-
		// like runs in it; a `kel shell` or a hand-made window holding a
		// shell or a server belongs under its space, not here as "?"
		if !a.Managed && !isAgentPane[pane] {
			if _, ok := agentPane[wid]; !ok || pane != "" {
				continue
			}
		}
		if pane == "" {
			pane = agentPane[wid]
		}
		if _, ok := agentPane[wid]; !ok && pane == "" && len(byWin[wid]) > 0 {
			first := byWin[wid][0]
			for _, p := range byWin[wid] {
				if f(p.PaneIndex) < f(first.PaneIndex) {
					first = p
				}
			}
			agentPane[wid] = first.PaneID
			pane = first.PaneID
		}
		repo := a.Group
		var git *fleet.Git
		var cwd string
		for _, p := range byWin[wid] {
			if p.PaneID == pane {
				git, cwd = p.Git, p.Cwd
			}
			if p.PaneID == pane && p.Git != nil {
				repo = repoLabel(doc, p.Git.Root)
				// a linked worktree kel did not list under a repo: its group names
				// the repo, as it does for the space (spaceKey)
				if p.Git.Worktree && repo == filepath.Base(p.Git.Root) && p.Group != "" {
					repo = p.Group
				}
			}
		}
		v.agents = append(v.agents, sbAgent{wid: wid, pane: pane, key: key, agent: a, current: wid == curWid, repo: repo, shared: len(byWin[wid]) > 1, git: git, cwd: cwd})
	}
	// a fixed order: repos by name, as spaces are, and agents in the order
	// their windows were made. State changes a row's dot and chip, never its
	// place — rows that moved under the pointer were the author's complaint
	// (2026-11-03); what needs you is the chips', the banner's and `'s job
	sort.SliceStable(v.agents, func(i, j int) bool {
		ai, aj := v.agents[i], v.agents[j]
		if ai.repo != aj.repo {
			return ai.repo < aj.repo
		}
		if xi, xj := f(ai.agent.Index), f(aj.agent.Index); xi != xj {
			return xi < xj
		}
		return ai.agent.Name < aj.agent.Name
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
			if s.ports != nil && len(*s.ports) == 0 {
				s.ports = nil
			}
		} else {
			if s.ports == nil {
				s.ports = &[]int{}
			}
			*s.ports = append(*s.ports, *p.Ports...)
		}
		if isAgentPane[p.PaneID] || agentPane[p.WindowID] == p.PaneID {
			for _, a := range v.agents {
				if a.pane != p.PaneID {
					continue
				}
				if a.agent.State == nil || *a.agent.State != "dead" {
					s.agents++
				}
				if s.state == nil || triageRank(a.agent.State) < triageRank(s.state) {
					s.state = a.agent.State
				}
				if a.agent.Unseen {
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
			if ks := kids[t.key]; ui.filter == "" && len(ks) > wtFoldAt {
				t.wtFold, t.wtOpen = true, ui.expanded[wtKey(t.key)]
				for i := range ks {
					k := &ks[i]
					if t.wtOpen || k.agents > 0 || k.current || k.state != nil {
						continue
					}
					k.folded = true
					t.wtHidden++
					if k.land != nil && k.land.Code == "unpushed" && k.land.N != nil {
						t.wtUnpushed += int(*k.land.N)
					}
				}
			}
			v.spaces = append(v.spaces, t)
			v.spaces = append(v.spaces, kids[t.key]...)
		}
	}

	for _, s := range v.spaces {
		inRepo := s.git != nil && s.git.Root != ""
		where := ""
		if inRepo && len(s.panes) > 0 {
			where = s.repoName()
		}
		for _, p := range s.panes {
			w := where
			if !inRepo {
				w = tildify(p.Cwd)
			}
			v.shells = append(v.shells, sbShell{p, w})
		}
	}
	v.reposOpen = ui.expanded[reposKey] || ui.filter != ""
	if ui.filter != "" {
		v.applyFilter(strings.ToLower(ui.filter))
	}
	if doc.Background != nil {
		names := map[string]string{}
		for _, a := range v.agents {
			names[a.agent.Name] = a.key
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
	v.needs = needsYou(v.agents)
	for _, a := range v.needs {
		v.entryIDs = append(v.entryIDs, needID(a.key))
	}
	for _, sh := range v.shells {
		v.entryIDs = append(v.entryIDs, sh.pane.PaneID)
	}
	if !v.unknown {
		v.entryIDs = append(v.entryIDs, reposID)
	}
	for _, s := range v.spaces {
		if s.folded || !v.reposOpen {
			continue
		}
		v.entryIDs = append(v.entryIDs, spaceID(s.key))
		if s.wtSummary() {
			v.entryIDs = append(v.entryIDs, wtID(s.key))
		}
	}
	if ui.filter == "" {
		var inNeeds map[string]bool
		for i := range v.agents {
			a := &v.agents[i]
			if a.current || agentID(a.key) == ui.sel {
				continue
			}
			if a.quiet = quietKind(a.agent, ui.now); a.quiet == "" {
				continue
			}
			if inNeeds == nil {
				inNeeds = map[string]bool{}
				for _, n := range v.needs {
					inNeeds[n.key] = true
				}
			}
			if inNeeds[a.key] {
				a.quiet = ""
				continue
			}
			if v.quiet == nil {
				v.quiet = map[string]sbQuiet{}
			}
			q := v.quiet[a.repo]
			q.open = ui.expanded[quietKey(a.repo)]
			if a.quiet == "idle" {
				q.idle++
			} else {
				q.unknown++
			}
			v.quiet[a.repo] = q
			a.folded = !q.open
		}
	}
	for i, a := range v.agents {
		if !a.folded {
			v.entryIDs = append(v.entryIDs, agentID(a.key))
			for _, b := range v.bgUnder[a.key] {
				v.entryIDs = append(v.entryIDs, bgID(b.ID))
			}
		}
		if _, ok := v.quiet[a.repo]; ok && (i+1 == len(v.agents) || v.agents[i+1].repo != a.repo) {
			v.entryIDs = append(v.entryIDs, quietID(a.repo))
		}
	}
	for _, b := range v.bgLoose {
		v.entryIDs = append(v.entryIDs, bgID(b.ID))
	}
	if v.indexOf(v.sel) < 0 {
		was := v.sel
		v.sel = ""
		if k, ok := strings.CutPrefix(was, "q:"); ok && v.indexOf(agentID(k)) >= 0 {
			v.sel = agentID(k)
		}
		for _, a := range v.agents {
			if v.sel != "" {
				break
			}
			if a.current {
				v.sel = agentID(a.key)
			}
		}
		if v.sel == "" && len(v.entryIDs) > 0 {
			v.sel = v.entryIDs[0]
		}
	}
	return v
}

// applyFilter keeps what matches by name.
func (v *sbView) applyFilter(q string) {
	var ss []sbSpace
	for _, s := range v.spaces {
		if strings.Contains(strings.ToLower(s.label), q) ||
			(s.git != nil && s.git.Branch != nil && strings.Contains(strings.ToLower(*s.git.Branch), q)) {
			ss = append(ss, s)
		}
	}
	v.spaces = ss
	var shs []sbShell
	for _, sh := range v.shells {
		if strings.Contains(strings.ToLower(paneLabel(sh.pane)), q) || strings.Contains(strings.ToLower(sh.where), q) {
			shs = append(shs, sh)
		}
	}
	v.shells = shs
	var as []sbAgent
	for _, a := range v.agents {
		if strings.Contains(strings.ToLower(a.agent.Name), q) || strings.Contains(strings.ToLower(a.agent.Group), q) {
			as = append(as, a)
		}
	}
	v.agents = as
}

func needsYou(agents []sbAgent) []sbAgent {
	var out []sbAgent
	for _, a := range agents {
		if st := a.agent.State; st != nil && (*st == "waiting" || *st == "error" || (*st == "done" && a.agent.Unseen)) {
			out = append(out, a)
		}
	}
	since := func(a sbAgent) float64 {
		if a.agent.StateSince == nil {
			return math.MaxFloat64
		}
		return *a.agent.StateSince
	}
	sort.SliceStable(out, func(i, j int) bool { return since(out[i]) < since(out[j]) })
	return out
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
		return "○", green
	case "throttled":
		return "◌", purple
	case "error":
		return "●", red
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
	if s.closed && s.git != nil {
		// known from git alone: no window stands here to say more
		return spacePos(s) + " · no window"
	}
	parts := []string{spacePos(s)}
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

// spacePos is where a space stands — branch and position, or its directory —
// the part of spaceDetail that fits on the entry's own line.
func spacePos(s sbSpace) string {
	switch g := s.git; {
	case s.closed && g != nil:
		if g.Branch != nil {
			return *g.Branch
		}
		return "detached"
	case g == nil:
		return tildify(s.cwd)
	case g.Branch == nil && g.Dirty == nil:
		return "?"
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
		return pos
	}
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
	if a.State == nil || *a.State != "working" || runningSubs(a) {
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

func runningSubs(a fleet.Agent) bool {
	for _, sub := range a.Subagents {
		if sub.Status == "running" {
			return true
		}
	}
	return false
}

func shortNote(n string) string {
	n = strings.TrimSpace(strings.SplitN(strings.TrimSpace(n), "\n", 2)[0])
	if t := strings.TrimPrefix(n, "Claude needs your permission to use "); t != n {
		return "allow " + strings.TrimSuffix(t, ".") + "?"
	}
	if t := strings.TrimPrefix(n, "API Error: "); t != n {
		return t
	}
	if strings.HasPrefix(n, "Claude is waiting for your input") {
		return "your input"
	}
	return n
}

type summary struct {
	text, colour string
	titled, wrap bool
}

func summaryOf(a fleet.Agent) summary {
	if a.State == nil {
		return summary{}
	}
	note := ""
	if a.Note != nil {
		note = *a.Note
	}
	switch *a.State {
	case "working":
		if a.Doing != nil && *a.Doing != "" {
			return summary{text: *a.Doing, colour: dim, wrap: true}
		}
	case "waiting":
		if r := shortNote(note); r != "" {
			return summary{text: r, colour: mid}
		}
	case "error":
		if r := shortNote(note); r != "" {
			return summary{text: r, colour: red}
		}
	case "done", "idle":
		return summary{titled: true}
	}
	return summary{}
}

// modeWord is an agent's permission mode as the row shows it; the default
// mode, which kel never writes, shows nothing.
func modeWord(m *string) string {
	if m == nil {
		return ""
	}
	switch *m {
	case "", "default":
		return ""
	case "acceptEdits":
		return "edits"
	case "bypassPermissions":
		return "bypass"
	case "dontAsk":
		return "dontask"
	}
	return strings.ToLower(*m)
}

func taskCount(t *fleet.Tasks) string {
	if t == nil {
		return ""
	}
	return fmt.Sprintf("%d/%d", t.Done, t.Total)
}

// agentAge is the right edge of an agent's row: how long it has been in its
// state, and a chip naming the state when it is one that needs a word.
// A `working` kel has not heard from reads "quiet", grey italic: kel is not
// sure, and says so (invariant 3).
func agentAge(a fleet.Agent, now int64) (age, chipText, chipFg string, chipBg rgb) {
	if q := quietFor(a, now); q > 0 {
		return dim + italic + "quiet " + humanizeSecs(q) + reset, "", "", rgb{}
	}
	if a.State == nil || *a.State == "" {
		return dim + "unknown" + reset, "", "", rgb{}
	}
	if a.StateSince != nil {
		age = dim + humanizeSecs(now-int64(*a.StateSince)) + reset
	}
	switch *a.State {
	case "waiting":
		return age, "needs you", yellow, yellowBg
	case "error":
		return age, "error", red, redBg
	case "dead":
		return age, "dead", red, redBg
	case "throttled":
		return age, "quota", purple, purpleBg
	case "done":
		if a.Unseen {
			return age, "done", green, greenBg
		}
	}
	return age, "", "", rgb{}
}

// agentFacts is the selected agent's detail: context and cost. Unknowns are
// `?`, never a plausible 0 (invariant 3).
func agentFacts(a fleet.Agent) string {
	if a.Context == nil {
		return "ctx ?"
	}
	pct := "ctx ?"
	if a.Context.Pct != nil {
		pct = fmt.Sprintf("%.0f%%", *a.Context.Pct)
	}
	c := "$?"
	if a.Context.CostUSD != nil {
		c = money(*a.Context.CostUSD)
	}
	if m := shortModel(a.Context.Model); m != "" {
		return m + " · " + pct + " · " + c
	}
	return pct + " · " + c
}

var modelNote = regexp.MustCompile(`\s*\([^)]*\)\s*$`)

func shortModel(m *string) string {
	if m == nil {
		return ""
	}
	return strings.TrimSpace(modelNote.ReplaceAllString(*m, ""))
}

// chip names a state on a fill of its colour, with half-block ends in that
// fill so it reads rounded: ▐needs you▌. As wide as " text ".
func chip(text, fg string, bg rgb) string {
	return bg.fg() + "▐" + reset + bg.bg() + fg + text + reset + bg.fg() + "▌" + reset
}

// wrapHint breaks a choices line ("y yes · n no · esc") between choices to
// fit w columns; a choice wider than w alone is cut.
func wrapHint(h string, w int) []string {
	if h == "" {
		return nil
	}
	var out []string
	cur := ""
	for _, seg := range strings.Split(h, " · ") {
		switch {
		case cur == "":
			cur = seg
		case len([]rune(cur))+3+len([]rune(seg)) <= w:
			cur += " · " + seg
		default:
			out = append(out, truncate(cur, w))
			cur = seg
		}
	}
	return append(out, truncate(cur, w))
}

func visWidth(s string) int { return len([]rune(stripANSI(s))) }

// spread sets right flush against the right edge, one column in, after left.
// Each side arrives already cut to fit.
func spread(left, right string, w int) string {
	if right == "" {
		return left
	}
	return left + strings.Repeat(" ", max(1, w-visWidth(left)-visWidth(right)-1)) + right
}

// fill paints a line's background across w columns. A reset inside the line
// would end the paint, so each one takes the background up again.
func fill(s string, w int, bg string) string {
	s = bg + strings.ReplaceAll(s, reset, reset+bg)
	return s + strings.Repeat(" ", max(0, w-visWidth(s))) + reset
}

// fitPair cuts a name and a right-hand detail to share avail columns: the
// name keeps at least half, the detail what is left, and goes when that is
// too little to read.
func fitPair(name string, right int, avail int) (nameW, rightW int) {
	n := len([]rune(name))
	if n+2+right <= avail {
		return n, right
	}
	nameW = max(min(n, avail-2-right), min(n, avail/2))
	rightW = avail - nameW - 2
	if rightW < 4 {
		return min(n, avail), 0
	}
	return nameW, rightW
}

// renderView draws the view at w×h. h == 0 means "no size yet": draw it all.
// It returns, for every screen line, the entry the line belongs to.
func renderView(v sbView, w, h int, now int64) (string, []lineRef) {
	if w <= 0 && h <= 0 {
		w = 32
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
	// one line per entry, herdr's way (2026-11-03): the name, and what it
	// is doing right-aligned. lead, a space, the dot and a space before the
	// name; one column clear of the right edge
	avail := max(w-5, 0)
	in := "  " // the text column; a sidebar squeezed narrower than any name gives it up
	if w < 12 {
		in = ""
	}
	label := func(s string) string { return in + dim + truncate(s, w-2) + reset }
	drawn := func(id string) bool { return v.drawer != nil && v.drawer.id == id && id == v.sel }
	drawer := func(id string) {
		var git *fleet.Git
		var cwd string
		for _, a := range v.agents {
			if agentID(a.key) == id {
				git, cwd = a.git, a.cwd
			}
		}
		for _, l := range drawerLines(v.drawer, git, cwd, w, lead(id)) {
			add(l.s, l.ref)
		}
	}
	// the selected entry stands on a raised block, a blank row above and
	// below, with its detail inside — opencode's way
	entry := func(id string, r lineRef, rows []string, sums int) {
		marked := func(i int) lineRef {
			rr := r
			rr.sum = i >= 1 && i <= sums
			return rr
		}
		if id != v.sel {
			for i, s := range rows {
				add(s, marked(i))
				r.hits = nil
			}
			return
		}
		add(fill(lead(id), w, raised), r)
		for i, s := range rows {
			add(fill(s, w, raised), marked(i))
		}
		if drawn(id) {
			drawer(id)
			return
		}
		foot := line{fill(lead(id), w, raised), r}
		if btns := rowButtons(r.kind); len(btns) > 0 {
			st := buttonStrip(id, r.kind, btns, w, lifted.mix(rgb{255, 255, 255}, 0.10))
			foot = line{fill(lead(id)+st.s[1:], w, raised), st.ref}
		}
		add(foot.s, foot.ref)
	}
	// the entry under the pointer shows its buttons inline, in place of its
	// detail. A strip is never inserted, because an inserted row moved the
	// rows above the pointer (the author, 2026-11-05). The selection's block
	// carries its own strip, so only a selected row without one draws them
	inline := func(id, kind string, used int, block bool) (string, int, []hit) {
		if v.hover == 0 || v.menu != nil || id != v.hoverID || (id == v.sel && block) {
			return "", -1, nil
		}
		btns := rowButtons(kind)
		bw := func() (n int) {
			for _, b := range btns {
				n += len([]rune(b.label)) + 2
			}
			return
		}
		for len(btns) > 1 && used+4+bw()+2 > w {
			btns = btns[1:]
		}
		fc := lifted
		if id == v.sel {
			fc = lifted.mix(rgb{255, 255, 255}, 0.10)
		}
		x := w - 1 - bw()
		var hits []hit
		var sb strings.Builder
		for _, b := range btns {
			n := len([]rune(b.label)) + 2
			hits = append(hits, hit{x, x + n, b.key})
			sb.WriteString(buttonIn(b.label, fc))
			x += n
		}
		return sb.String(), max(w-used-bw()-2, 1), hits
	}
	one := func(id string, r lineRef, s string) {
		if id == v.sel {
			s = fill(s, w, raised)
		}
		add(s, r)
		if drawn(id) {
			drawer(id)
		}
	}

	needEnd := 0
	reposPin := ""
	if len(v.needs) > 0 {
		add(spread(in+yellow+truncate("Needs you", w-8)+reset, faint+fmt.Sprint(len(v.needs))+reset, w), lineRef{kind: "header"})
		for _, a := range v.needs {
			id := needID(a.key)
			d, c := stateLook(a.agent.State, a.agent.Unseen)
			reason := ""
			if *a.agent.State == "done" {
				reason = "done"
			} else if a.agent.Note != nil {
				reason = shortNote(*a.agent.Note)
			}
			age := ""
			if a.agent.StateSince != nil {
				age = humanizeSecs(now - int64(*a.agent.StateSince))
			}
			nw, rw := fitPair(a.agent.Name, len([]rune(reason)), max(avail-len([]rune(age))-1, 0))
			left := lead(id) + " " + c + d + reset + " " + truncate(a.agent.Name, nw)
			if rw > 0 {
				left += "  " + mid + truncate(reason, rw) + reset
			}
			one(id, lineRef{id: id, kind: "need"}, spread(left, dim+age+reset, w))
		}
		add("", lineRef{})
		needEnd = len(body)
	}
	shellN := ""
	if len(v.shells) > 0 {
		shellN = faint + fmt.Sprint(len(v.shells)) + reset
	}
	add(spread(in+dim+truncate("Shells", w-8)+reset, shellN, w), lineRef{kind: "header"})
	if v.unknown {
		add("    "+dim+truncate("? panes unknown", w-4)+reset, lineRef{})
	} else if len(v.shells) == 0 {
		add("    "+dim+truncate("none", w-4)+reset, lineRef{})
	}
	seen := map[string]int{}
	for _, sh := range v.shells {
		seen[paneLabel(sh.pane)]++
	}
	for _, sh := range v.shells {
		p := sh.pane
		pr := lineRef{id: p.PaneID, kind: "pane"}
		lw, rw := fitPair(paneLabel(p), len([]rune(sh.where)), avail)
		right := ""
		if rw > 0 {
			right = dim + truncate(sh.where, rw) + reset
		}
		if btns, bn, hits := inline(p.PaneID, "pane", 4, false); bn >= 0 {
			right, lw, pr.hits = btns, bn, hits
		}
		lbl := truncate(paneLabel(p), lw)
		if seen[paneLabel(p)] > 1 && p.WindowName != "" {
			if room := lw - len([]rune(lbl)) - 3; room > 0 {
				lbl += dim + " · " + truncate(p.WindowName, room) + reset
			}
		}
		one(p.PaneID, pr, spread(lead(p.PaneID)+" "+paneGlyph(p.Kind)+" "+weight(lbl, p.Current), right, w))
		pr.hits = nil
		if p.Cards != nil && p.Cards.Unseen && p.Cards.Latest != nil {
			latest := p.Cards.Latest
			col := dim
			if latest.Kind == "decision" {
				col = yellow
			}
			one(p.PaneID, pr, lead(p.PaneID)+"   "+col+truncate("▸ "+latest.Kind+": "+latest.Title, avail)+reset)
		}
	}
	if !v.unknown {
		name, tail := "Repos ▸", fmt.Sprint(len(v.spaces))
		if v.reposOpen {
			name = "Repos ▾"
		} else if m := mergedCount(v.spaces); m > 0 {
			tail += fmt.Sprintf(" · %d merged", m)
		}
		lw, rw := fitPair(name, len([]rune(tail)), avail)
		row := func(bar string) string {
			return spread(bar+" "+dim+truncate(name, lw)+reset, faint+truncate(tail, rw)+reset, w)
		}
		reposPin = row(" ")
		one(reposID, lineRef{id: reposID, kind: "header"}, row(lead(reposID)))
	}
	if v.reposOpen && len(v.spaces) == 0 && !v.unknown {
		add("    "+dim+truncate("nothing open", w-4)+reset, lineRef{})
	}
	for _, s := range v.spaces {
		if s.folded || !v.reposOpen {
			continue
		}
		id := spaceID(s.key)
		r := lineRef{id: id, kind: "space"}
		summary := func() {
			if s.wtSummary() {
				wid := wtID(s.key)
				one(wid, lineRef{id: wid, kind: "space"}, lead(wid)+" "+dim+"└"+reset+"  "+dim+truncate(s.wtText(), w-6)+reset)
			}
		}
		dot := dim + "·" + reset
		if s.state != nil {
			d, c := stateLook(s.state, s.unseen)
			dot = c + d + reset
		}
		if s.parent != "" {
			// a worktree: one line under its repo, the branch and what
			// landing it needs — `└● fix-auth     ↑2 · review`
			// the state is what the row is for, so a long name gives way to
			// it (Claude Code names worktrees agent-af1a1567ab46ddb66)
			d := worktreeDetail(s)
			dw := visWidth(d)
			av := max(w-6, 0)
			lw := av
			if d != "" && len([]rune(s.label))+2+dw > av {
				lw = max(av-2-dw, 8)
			}
			btns, bn, hits := inline(id, "space", 5, false)
			if bn >= 0 {
				lw = bn
			}
			lbl := truncate(s.label, lw)
			if s.closed {
				lbl = dim + lbl + reset
			}
			left := lead(id) + " " + dim + "└" + reset + dot + " " + weight(lbl, s.current)
			right := ""
			if room := av - visWidth(lbl) - 2; d != "" && room > 0 {
				right = truncateStyled(d, room)
			}
			if bn >= 0 {
				right, r.hits = btns, hits
			}
			one(id, r, spread(left, right, w))
		} else {
			pos := spacePos(s)
			lw, rw := fitPair(s.label, len([]rune(pos)), avail)
			right := ""
			if rw > 0 {
				right = dim + truncate(pos, rw) + reset
			}
			if btns, bn, hits := inline(id, "space", 4, true); bn >= 0 {
				right, lw, r.hits = btns, bn, hits
			}
			rows := []string{spread(lead(id)+" "+dot+" "+weight(truncate(s.label, lw), s.current), right, w)}
			if id == v.sel {
				// the rest of where it stands — ports, how many agents —
				// not the position its own row already shows
				if more := strings.TrimPrefix(spaceDetail(s), spacePos(s)+" · "); more != spaceDetail(s) {
					rows = append(rows, lead(id)+"   "+dim+truncate(more, avail)+reset)
				}
			}
			entry(id, r, rows, 0)
		}
		summary()
	}

	add("", lineRef{})
	add(label("Agents"), lineRef{kind: "header"})
	if v.unknown {
		add("    "+dim+truncate("? panes unknown", w-4)+reset, lineRef{})
	} else if len(v.agents) == 0 {
		add("    "+dim+truncate("none", w-4)+reset, lineRef{})
	}
	// grouped under their repo: two repos can each have an agent called docs
	// each repo's agents under its name and their count, a blank row
	// between repos: the author found one run of rows too tight to read
	count := map[string]int{}
	for _, a := range v.agents {
		count[a.repo]++
	}
	groupHead := func(g string, n int) {
		if g == "" {
			g = "misc"
		}
		add(spread("  "+mid+truncate(g, w-8)+reset, faint+fmt.Sprint(n)+reset, w), lineRef{kind: "group"})
	}
	group := "\x00"
	firstCard := true
	card := func(a sbAgent) {
		id := agentID(a.key)
		r := lineRef{id: id, kind: "agent"}
		d, c := stateLook(a.agent.State, a.agent.Unseen)
		if quietFor(a.agent, now) > 0 {
			d, c = "◌", dim // not heard from: no state colour it has not earned
		}
		age, ct, cf, cb := agentAge(a.agent, now)
		right := age
		if ct != "" {
			right = strings.TrimSpace(age + " " + chip(ct, cf, cb))
		}
		lw, rw := fitPair(a.agent.Name, visWidth(right), avail)
		if rw < visWidth(right) {
			// too narrow for both: the chip says more than the age
			switch {
			case ct != "" && len([]rune(ct))+2 <= rw:
				right = chip(ct, cf, cb)
			case visWidth(age) <= rw:
				right = age
			default:
				right = ""
			}
		}
		btns, bn, hits := inline(id, "agent", 4, true)
		if bn >= 0 {
			right, lw, r.hits = btns, bn, hits
		}
		mw := modeWord(a.agent.Mode)
		nameW := lw
		if mw != "" {
			extra := len([]rune(mw)) + 1
			budget := lw
			if bn < 0 {
				budget = max(lw, avail-2-visWidth(right))
			}
			if n := len([]rune(a.agent.Name)); n <= lw && n+extra <= budget {
				nameW = n
			} else {
				mw = ""
			}
		}
		left := lead(id) + " " + c + d + reset + " " + weight(truncate(a.agent.Name, nameW), a.current)
		if mw != "" {
			left += " " + faint + mw + reset
		}
		rows := []string{spread(left, right, w)}
		pre := lead(id) + "   "
		sel := id == v.sel
		sum := summaryOf(a.agent)
		title := ""
		if t := a.agent.Title; t != nil {
			title = *t
		}
		tasks := taskCount(a.agent.Tasks)
		tw := 0
		if tasks != "" {
			tw = len(tasks) + 1
		}
		ta := avail
		if !sel {
			ta -= tw
		}
		var lines []string
		switch {
		case sum.titled && title != "" && !sel:
			lines = []string{pre + dim + truncate(title, ta) + reset}
		case sum.titled && title != "":
			for _, l := range wrapWords(title, avail, 2) {
				lines = append(lines, pre+dim+l+reset)
			}
		case sum.text != "" && sel && sum.wrap:
			for _, l := range wrapWords(sum.text, avail, 2) {
				lines = append(lines, pre+sum.colour+l+reset)
			}
		case sum.text != "":
			lines = []string{pre + sum.colour + truncate(sum.text, ta) + reset}
		}
		if tasks != "" && !sel && len(lines) > 0 {
			lines[0] = spread(lines[0], dim+tasks+reset, w)
		}
		rows = append(rows, lines...)
		sums := len(lines)
		if sel {
			if title != "" && !sum.titled {
				for _, l := range wrapWords(title, avail, 2) {
					rows = append(rows, pre+dim+l+reset)
				}
			}
			ft := agentFacts(a.agent)
			facts := pre + mid + truncate(ft, avail) + reset
			if tasks != "" && len([]rune(ft)) <= avail-tw {
				facts = spread(facts, dim+tasks+reset, w)
			}
			rows = append(rows, facts)
			// subagents: the Task-tool helpers this agent has running
			for _, sub := range a.agent.Subagents {
				sd, sc := subLook(sub.Status)
				sl := sub.Label
				if sub.AgentType != "" && sub.AgentType != "general-purpose" {
					sl += " · " + sub.AgentType
				}
				rows = append(rows, pre+dim+"└"+reset+" "+sc+sd+reset+" "+dim+truncate(sl, avail-4)+reset)
			}
		}
		if a.agent.Cards != nil && a.agent.Cards.Unseen && a.agent.Cards.Latest != nil {
			latest := a.agent.Cards.Latest
			col := dim
			if latest.Kind == "decision" {
				col = yellow
			}
			rows = append(rows, pre+col+truncate("▸ "+latest.Kind+": "+latest.Title, avail)+reset)
		}
		entry(id, r, rows, sums)
		for _, b := range v.bgUnder[a.key] {
			bd, bc, _ := bgLook(b)
			bid := bgID(b.ID)
			bright := ""
			if b.State == "blocked" {
				bright = chip("blocked", yellow, yellowBg)
			}
			bl := truncate(b.Name+" · bg", max(avail-4-visWidth(bright)-1, 1))
			one(bid, lineRef{id: bid, kind: "bg"}, spread(lead(bid)+"   "+dim+"└"+reset+" "+bc+bd+reset+" "+bl, bright, w))
		}
	}
	for i, a := range v.agents {
		if a.repo != group {
			if i > 0 {
				add("", lineRef{})
			}
			group = a.repo
			groupHead(group, count[group])
			firstCard = true
		}
		if !a.folded {
			if !firstCard {
				add("", lineRef{})
			}
			firstCard = false
			card(a)
		}
		if q, ok := v.quiet[a.repo]; ok && (i+1 == len(v.agents) || v.agents[i+1].repo != a.repo) {
			if !firstCard {
				add("", lineRef{})
			}
			firstCard = false
			id := quietID(a.repo)
			one(id, lineRef{id: id, kind: "fold"}, lead(id)+"   "+dim+truncate(q.text(), avail)+reset)
		}
	}
	if len(v.bgLoose) > 0 {
		if len(v.agents) > 0 {
			add("", lineRef{})
		}
		groupHead("background", len(v.bgLoose))
	}
	for _, b := range v.bgLoose {
		bd, bc, _ := bgLook(b)
		bid := bgID(b.ID)
		right := ""
		if b.State == "blocked" {
			right = chip("blocked", yellow, yellowBg)
		} else if b.Group != "" {
			right = dim + b.Group + reset
		}
		lw, rw := fitPair(b.Name, visWidth(right), avail)
		if rw < visWidth(right) {
			right = ""
		}
		one(bid, lineRef{id: bid, kind: "bg"}, spread(lead(bid)+" "+bc+bd+reset+" "+truncate(b.Name, lw), right, w))
	}

	// the top: kel, and its two menus. Mouse first (2026-11-04): what the key
	// grid at the foot listed is in these menus and in each entry's own,
	// right-click, with its key beside it — learned by using the mouse
	var head []line
	if w >= 16 {
		from := w - 1 - visWidth(button("+ New")+" "+button("⋯"))
		ref := lineRef{kind: "menu", id: "menu:new", id2: "menu:kel", from: from, split: from + visWidth(button("+ New")) + 1}
		on := v.hoverOn(ref, 0)
		right := buttonLit("+ New", on == "menu:new") + " " + buttonLit("⋯", on == "menu:kel")
		head = []line{{spread("  "+bold+"kel"+reset, right, w), ref}, {"", lineRef{}}}
	}

	// the tail: a prompt — what it asks, what you type, how to answer — or
	// the last command's word, or how to get in. A prompt stands out (bold,
	// the accent `›`) because the author could not tell one had opened
	var tail []line
	switch {
	case v.promptTitle != "":
		c := v.promptColour
		if c == "" {
			c = accent
		}
		tail = append(tail, line{in + c + bold + truncate(v.promptTitle, w-2) + reset, lineRef{}})
		if v.input != "" {
			tail = append(tail, line{in + accent + "› " + reset + bold + tail2(v.input, w-4) + reset, lineRef{}})
		}
		// the choices wrap rather than lose one off the end; each line keeps
		// its own text, so a click finds the word under it
		for _, h := range wrapHint(v.hint2, w-len(in)) {
			tail = append(tail, line{in + dim + h + reset, lineRef{id: "hint2", kind: "hint2", text: in + h}})
		}
	case v.footer != "":
		tail = append(tail, line{in + v.footerColour + truncate(v.footer, w-2) + reset, lineRef{}})
	case v.filter != "":
		tail = append(tail, line{in + dim + truncate("/"+v.filter+"  esc clears", w-2) + reset, lineRef{}})
	}
	// scroll so the selected entry is on screen; the bottom is pinned
	if h > 0 {
		if len(tail) == 0 {
			tail = append(tail, footerLine(w, in, ""))
			tail[0] = footerLine(w, in, v.hoverOn(tail[0].ref, h-1))
		}
		room := h - len(tail) - len(head)
		if room < 0 {
			room = 0
		}
		off := 0
		var pinned []line
		if len(body) > room && needEnd > 0 {
			pinned = pinNeeds(body[:needEnd], v, w, in, lead)
			if room-len(pinned) < 4 {
				pinned = nil
			} else {
				body = body[needEnd:]
				room -= len(pinned)
			}
		}
		if len(body) > room {
			first, last := -1, -1
			for i, l := range body {
				if l.ref.id != "" && l.ref.id == v.sel || l.ref.kind == "drawer" {
					if first < 0 {
						first = i
					}
					last = i
				}
			}
			if v.drawer != nil && last-first+1 >= room {
				for i, l := range body {
					if l.ref.kind == "drawer" && l.ref.id == "dr:"+v.drawer.items[v.drawer.sel].key {
						last = i
					}
				}
				first = max(first, last-room+2)
			}
			if last >= room {
				off = last - room + 1
			}
			if first >= 0 && first < off {
				off = first
			}
			if off > 0 && room > 2 {
				// a section's own header, or the blank before it, starts
				// the screen as it is; anything else gets its section's
				// header pinned above it, the offset settled first so the
				// header names the section actually shown
				starts := func(o int) bool {
					return body[o].ref.kind == "header" || (body[o].s == "" && o+1 < len(body) && body[o+1].ref.kind == "header")
				}
				if !starts(off) {
					avail := room - 1
					if last >= 0 && last >= off+avail {
						off = last - avail + 1
					}
					if first >= 0 && first < off {
						off = first
					}
				}
				switch {
				case body[off].ref.kind == "header":
					body = body[off:]
				case starts(off):
					body = body[off+1:]
				default:
					pinned := line{label("Shells"), lineRef{kind: "header"}}
					for _, l := range body[:off] {
						if l.ref.kind == "header" {
							pinned = line{l.s, lineRef{kind: "header"}}
							if l.ref.id == reposID {
								pinned = line{reposPin, l.ref}
							}
						}
					}
					body = append([]line{pinned}, body[off:]...)
				}
			} else {
				body = body[off:]
			}
			if len(body) > room {
				body = body[:room]
			}
		}
		for len(body) < room {
			body = append(body, line{"", lineRef{}})
		}
		body = append(pinned, body...)
	} else if len(tail) == 0 {
		tail = append(tail, footerLine(w, in, ""))
	}
	body = append(head, body...)
	if h != 1 || len(body) == 0 {
		body = append(body, tail...)
	}
	if h > 0 && len(body) > h {
		body = body[:h]
	}
	// an open menu draws over the rows, at the row it was opened from
	if v.menu != nil {
		box := menuLines(*v.menu, w)
		y := v.menu.y
		if h > 0 && y+len(box) > h {
			y = max(0, h-len(box))
		}
		for len(body) < y+len(box) {
			body = append(body, line{"", lineRef{}})
		}
		copy(body[y:], box)
	}
	// hover: the entry under the pointer gets a dim bar where the selection's
	// would be, so what a click would hit is visible before the click
	if v.hover > 0 && v.hover <= len(body) && v.menu == nil && body[v.hover-1].ref.kind != "menu" && body[v.hover-1].ref.kind != "drawer" {
		hl := body[v.hover-1]
		if hl.ref.id2 != "" && v.hoverX >= hl.ref.split {
			body[v.hover-1].ref.right = dim + "▏" + reset + hl.ref.right[1:]
		} else if id := hl.ref.id; id != "" && id != v.sel {
			soft := lifted.mix(baseBg, 0.25).bg()
			for i, l := range body {
				if l.ref.id != id {
					continue
				}
				if strings.HasPrefix(l.s, " ") {
					body[i].s = dim + "▏" + reset + l.s[1:]
				}
				if len(rowButtons(l.ref.kind)) > 0 {
					body[i].s = fill(body[i].s, w, soft)
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
		ls := l.s + l.ref.right
		if w > 0 && visWidth(ls) > w {
			ls = truncateStyled(ls, w)
		}
		b.WriteString(ls)
		refs[i] = l.ref
	}
	return b.String(), refs
}

const needsPinned = 4

// pinNeeds is the Needs you block as it stays above a scrolled list: its
// header and at most four rows, the fourth becoming `+N more`.
func pinNeeds(block []line, v sbView, w int, in string, lead func(string) string) []line {
	rows := block[1 : len(block)-1]
	if len(rows) <= needsPinned {
		return block
	}
	out := append([]line{block[0]}, rows[:needsPinned-1]...)
	hidden := v.needs[needsPinned-1:]
	id := needID(hidden[0].key)
	for _, a := range hidden {
		if needID(a.key) == v.sel {
			id = v.sel
		}
	}
	more := lead(id) + " " + yellow + fmt.Sprintf("+%d more", len(hidden)) + reset
	if id == v.sel {
		more = fill(more, w, raised)
	}
	return append(out, line{more, lineRef{id: id, kind: "need"}}, block[len(block)-1])
}

// sbAct is a key and what it does, shown at the foot and clickable.
type sbAct struct{ key, label string }

// selActions are the selected entry's verbs, under what it is and its name
// — "agent kelsier", "space kelsier": the two often share a name.
func selActions(v sbView) (string, string, []sbAct) {
	switch {
	case strings.HasPrefix(v.sel, "a:"):
		for _, a := range v.agents {
			if agentID(a.key) == v.sel {
				acts := []sbAct{{"d", "diff"}, {"f", "files"}, {"e", "edit file"}, {"p", "plan"}, {"h", "history"}, {"i", "cards"}, {"z", "zoom"}}
				if a.shared {
					acts = append(acts, sbAct{"m", "own window"})
				}
				acts = append(acts, sbAct{"r", "rename"}, sbAct{"x", "kill"})
				if a.agent.State != nil && *a.agent.State == "dead" {
					acts = append([]sbAct{{"R", "relaunch"}}, acts...)
				}
				return "agent", a.agent.Name, acts
			}
		}
	case strings.HasPrefix(v.sel, "b:"):
		for _, list := range append([][]fleet.Background{v.bgLoose}, mapValues(v.bgUnder)...) {
			for _, b := range list {
				if bgID(b.ID) == v.sel {
					return "background", b.Name, []sbAct{{"enter", "open it"}}
				}
			}
		}
	case strings.HasPrefix(v.sel, "s:"):
		for _, s := range v.spaces {
			if spaceID(s.key) != v.sel {
				continue
			}
			var acts []sbAct
			if s.land != nil && s.land.Code == "merged" && s.closed {
				acts = append(acts, sbAct{"s", "sweep"})
			}
			acts = append(acts, sbAct{"f", "files"}, sbAct{"g", "git"})
			if s.git != nil {
				acts = append(acts, sbAct{"d", "diff"})
			}
			acts = append(acts, sbAct{"e", "nvim"}, sbAct{"t", "shell"})
			if s.ports != nil && len(*s.ports) > 0 {
				acts = append(acts, sbAct{"o", "browser"})
			}
			acts = append(acts, sbAct{"c", "claude"})
			what := "space"
			if s.parent != "" {
				what = "worktree"
			}
			return what, s.label, acts
		}
	default:
		for _, sh := range v.shells {
			p := sh.pane
			if p.PaneID != v.sel {
				continue
			}
			acts := []sbAct{{"enter", "go to it"}}
			if p.Kind == "server" {
				acts = append(acts, sbAct{"o", "browser"})
			}
			acts = append(acts, sbAct{"f", "files"}, sbAct{"g", "git"}, sbAct{"e", "nvim"}, sbAct{"i", "cards"}, sbAct{"x", "close"}, sbAct{"z", "zoom"})
			return "pane", paneLabel(p), acts
		}
	}
	return "", "", nil
}

func mapValues(m map[string][]fleet.Background) [][]fleet.Background {
	var out [][]fleet.Background
	for _, v := range m {
		out = append(out, v)
	}
	return out
}

// tail2 keeps the end of what is typed, where the cursor is.
func tail2(s string, w int) string {
	r := []rune(s)
	if w <= 1 || len(r) <= w {
		return s
	}
	return "…" + string(r[len(r)-w+1:])
}

var ansiRE = regexp.MustCompile("\x1b\\[[0-9;]*m")

func stripANSI(s string) string { return ansiRE.ReplaceAllString(s, "") }

// repoLabel names the repo a checkout belongs to: the main checkout's folder,
// also for a linked worktree (from doc.Repos), else the root's own folder.
func repoLabel(doc fleet.Doc, root string) string {
	if doc.Repos != nil {
		for _, r := range *doc.Repos {
			if r.Root == root {
				return r.Name
			}
			for _, w := range r.Worktrees {
				if w.Path == root {
					return r.Name
				}
			}
		}
	}
	return filepath.Base(root)
}

type sbMenu struct {
	title string
	items []sbAct
	y     int // the row it opens at
	sel   int // the item under the pointer or the arrow keys
}

type sbDrawer struct {
	id    string
	items []sbAct
	sel   int
}

func drawerLines(d *sbDrawer, git *fleet.Git, cwd string, w int, bar string) []line {
	avail := max(w-5, 0)
	row := func(s string, r lineRef) line { return line{fill(s, w, drawerBg), r} }
	blank := row(bar, lineRef{kind: "drawer"})
	pre := bar + "   "
	out := []line{blank}
	if git != nil {
		out = append(out, row(pre+mid+truncate(spacePos(sbSpace{git: git, cwd: cwd}), avail)+reset, lineRef{kind: "drawer"}), blank)
	}
	for i, it := range d.items {
		k := it.key
		if k == "enter" {
			k = "↵"
		}
		c := ""
		if it.key == "q" || it.key == "x" {
			c = red
		}
		lbl := truncate(it.label, max(avail-2-len([]rune(k)), 1))
		left := pre + c + lbl + reset
		if i == d.sel {
			left = bar + "  " + accent + "›" + reset + c + bold + lbl + reset
		}
		out = append(out, row(spread(left, dim+k+reset, w), lineRef{id: "dr:" + it.key, kind: "drawer"}))
	}
	return out
}

// newMenu holds what makes something.
func newMenu() []sbAct {
	return []sbAct{{"n", "New agent"}, {"w", "Agent in a worktree"}, {"|", "Split right"}, {"-", "Split down"}}
}

// kelMenu holds kel's own: what the key grid's kel half was.
func kelMenu(v sbView) []sbAct {
	acts := []sbAct{{"`", "Next that needs you"}, {"u", "Usage"}, {"v", "Pull requests"}, {";", "Run a kel command"}}
	if v.restore {
		acts = append(acts, sbAct{"b", "Restore last workspace"})
	}
	return append(acts, sbAct{"?", "All keys"}, sbAct{"q", "Quit kel"})
}

// entryMenu holds the selected entry's own verbs, as selActions lists them.
func entryMenu(v sbView) []sbAct {
	what, _, acts := selActions(v)
	if what == "" {
		return nil
	}
	out := []sbAct{{"enter", "Go to it"}}
	for _, a := range acts {
		if a.key == "enter" {
			continue
		}
		out = append(out, sbAct{a.key, strings.ToUpper(a.label[:1]) + a.label[1:]})
	}
	return out
}

// menuLines draws a menu one column in from each edge, boxed, each item with
// its key on the right; the item under the pointer is lit.
func menuLines(mn sbMenu, w int) []line {
	mw := min(w-2, 30)
	if mw < 10 {
		return nil
	}
	bg, edge := raised, faint
	row := func(s string, r lineRef) line { return line{" " + fill(s, mw, bg), r} }
	t := truncate(mn.title, mw-6)
	out := []line{row(edge+"┌ "+reset+bold+t+reset+" "+edge+strings.Repeat("─", max(0, mw-4-len([]rune(t))))+"┐"+reset, lineRef{kind: "menuitem"})}
	for i, it := range mn.items {
		k := it.key
		if k == "enter" {
			k = "↵"
		}
		lbl := truncate(it.label, mw-8)
		c := ""
		if it.key == "q" || it.key == "x" {
			c = red
		}
		ln := spread(edge+"│"+reset+" "+c+lbl+reset, dim+k+reset+" "+edge+"│"+reset, mw+1)
		if i == mn.sel {
			ln = edge + "│" + reset + accent + "›" + reset + c + bold + lbl + reset
			ln = spread(ln, dim+k+reset+" "+edge+"│"+reset, mw+1)
		}
		out = append(out, row(ln, lineRef{id: "mi:" + it.key, kind: "menuitem"}))
	}
	return append(out, row(edge+"└"+strings.Repeat("─", max(0, mw-2))+"┘"+reset, lineRef{kind: "menuitem"}))
}

// button is a clickable word on a lifted fill, rounded like a chip; under
// the pointer it lights up, so it reads as something to click.
func button(t string) string { return buttonIn(t, lifted) }

func buttonLit(t string, on bool) string {
	if on {
		return buttonIn(t, lit)
	}
	return button(t)
}

func buttonIn(t string, c rgb) string {
	return c.fg() + "▐" + reset + c.bg() + baseFg.fg() + t + reset + c.fg() + "▌" + reset
}

// footerLine is the foot of the sidebar: Settings on the left, Help on the
// right, each a click; the one under the pointer (hover: its id) lights up.
func footerLine(w int, in, hover string) line {
	word := func(icon, t, id string) string {
		if id == hover {
			return accent + icon + " " + bold + t + reset
		}
		return dim + icon + " " + reset + t
	}
	right := word("?", "Help", "open:help")
	left := in + word("≡", "Settings", "open:settings")
	return line{spread(left, right, w), lineRef{kind: "menu", id: "open:settings", id2: "open:help", from: len([]rune(in)), split: w - 1 - visWidth(right)}}
}

// hoverOn is what the pointer is on in a line drawn as ref, at row y: an id,
// or "".
func (v sbView) hoverOn(ref lineRef, y int) string {
	if v.hover != y+1 {
		return ""
	}
	return ref.at(v.hoverX)
}

// rowButtons are the buttons an entry of a kind shows: inline under the pointer, in the foot of its block when selected.
func rowButtons(kind string) []sbAct {
	switch kind {
	case "agent":
		return []sbAct{{"d", "Diff"}, {"f", "Files"}, {"menu", "⋯"}}
	case "space", "pane":
		return []sbAct{{"f", "Files"}, {"g", "Git"}, {"menu", "⋯"}}
	}
	return nil
}

// buttonStrip is the line of an entry's buttons in the foot of its block,
// recording where each one is for a click.
func buttonStrip(id, kind string, btns []sbAct, w int, fillc rgb) line {
	x := 6
	s := "    " + faint + "└ " + reset
	var hits []hit
	for _, b := range btns {
		n := len([]rune(b.label)) + 2
		if x+n > w-1 {
			break
		}
		hits = append(hits, hit{x, x + n, b.key})
		s += buttonIn(b.label, fillc)
		x += n
	}
	return line{s, lineRef{id: id, kind: kind, hits: hits}}
}

// wrapWords breaks text into at most n lines of w columns, at spaces where it
// can; what still does not fit ends the last line with an ellipsis. What an
// agent is doing was always cut at one line (the author, 2026-11-04).
func wrapWords(text string, w, n int) []string {
	if w <= 0 || n <= 0 {
		return nil
	}
	var out []string
	r := []rune(strings.TrimSpace(text))
	for len(r) > 0 && len(out) < n {
		if len(r) <= w {
			out = append(out, string(r))
			return out
		}
		if len(out) == n-1 {
			out = append(out, truncate(string(r), w))
			return out
		}
		cut := w
		for i := w; i > w/2; i-- {
			if r[i] == ' ' {
				cut = i
				break
			}
		}
		out = append(out, strings.TrimRight(string(r[:cut]), " "))
		r = []rune(strings.TrimLeft(string(r[cut:]), " "))
	}
	return out
}

func mergedCount(spaces []sbSpace) (n int) {
	for _, s := range spaces {
		if s.land != nil && s.land.Code == "merged" {
			n++
		}
	}
	return
}

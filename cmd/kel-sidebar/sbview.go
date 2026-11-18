package main

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
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
	sel      string // selected entry id (see sbEntry.id)
	expanded map[string]bool
	now      int64
	filter   string
	myWindow string // the window this sidebar sits in — current, whatever the doc says
	focused  bool   // the keyboard is in this sidebar
	hover    int    // screen line under the pointer, plus one; 0 = none
	hoverX   int    // and its column
	prefix   string // the tmux prefix in words ("Ctrl-b"), for hints
	restore  bool   // a previous workspace waits to be restored
}

type sbSpace struct {
	key, label string
	git        *fleet.Git
	cwd        string
	ports      *[]int // union over the space's panes; nil = could not tell
	panes      []fleet.Pane
	parent     string // the main checkout's key, for a linked worktree
	closed     bool   // no pane stands here: known from git, not tmux
	repo       string // the repo's name, for a worktree whose label is its branch
}

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
	server  bool // its window has a server pane: the row's ▪
	git     *fleet.Git
	cwd     string
}

func (a sbAgent) worktree() bool {
	return a.git != nil && a.git.Worktree || a.agent.Isolation != nil && *a.agent.Isolation == "worktree"
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
	quietAfter    = 24 * 3600
	doneFadeAfter = 12 * 3600
	bgHeadID      = "h:background"
	bgKey         = "sec:background"
	shellsID      = "h:shells"
	shellsKey     = "sec:shells"
)

// a repo group's heading, and the fold it opens and shuts: open until shut by
// hand
func groupID(repo string) string  { return "g:" + repo }
func groupKey(repo string) string { return "grp:" + repo }

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
	case *a.State == "done" && !a.Unseen:
		return "idle"
	}
	return ""
}

type sbView struct {
	servers bool // some pane in the workspace runs a server
	shells  []sbShell
	under   map[string][]sbShell // agent key -> the other panes of its window, in its inspector
	quiet   map[string]sbQuiet   // by repo: the group's quiet fold, if it has one
	spaces  []sbSpace
	agents  []sbAgent
	needs   []sbAgent         // what wants a look: the footer's count, the rail's
	shut    map[string]bool   // repo groups folded by hand
	bgOpen  bool              // the background group is open
	shOpen  bool              // the shells group is open
	inner   map[string]string // an id with no row of its own -> the row that holds it
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
	restore              bool
	menu                 *sbMenu // open over the rows, or nil
}

// lineRef says which entry a screen line belongs to — what a click hits.
type lineRef struct {
	id   string // "" for labels, blanks and the footer
	kind string // agent | pane | bg | group | fold | inspector | foot | menu | menuitem | hint2 | rail
	// the header's two menus side by side: from column split on, a click
	// is id2's
	id2   string
	split int
	from  int    // a click left of this column hits nothing
	hits  []hit  // buttons drawn on the line
	text  string // a choices line as drawn, without colour, for hintKey
}

// hit is a button on a line: columns [x0, x1) press key ("menu" opens the
// entry's own menu, "sel:<id>" selects id, "open:…" a screen).
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
	v := sbView{sel: ui.sel, filter: ui.filter, focused: ui.focused, hover: ui.hover, hoverX: ui.hoverX,
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
		if p.Kind == "server" {
			v.servers = true
		}
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
		server := false
		for _, p := range byWin[wid] {
			server = server || p.Kind == "server"
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
		v.agents = append(v.agents, sbAgent{wid: wid, pane: pane, key: key, agent: a, current: wid == curWid, repo: repo, shared: len(byWin[wid]) > 1, server: server, git: git, cwd: cwd})
	}
	// a fixed order: repos by name, as spaces are, and agents in the order
	// their windows were made. State changes a row's dot and right slot, never its
	// place — rows that moved under the pointer were the author's complaint
	// (2026-11-03); what needs you is the dots', the banner's and `'s job
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
			continue
		}
		s.panes = append(s.panes, p)
	}
	// every checkout of each repo, open in a window or not, so a repo group
	// whose main checkout has no pane still has a directory for its menu
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
				s.parent, s.repo = r.Root, r.Name
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
	v.nestPanes()
	v.entries(ui)
	return v
}

// entries lists the selectable ids in screen order, from the folds, and
// settles the selection on one of them.
func (v *sbView) entries(ui uiState) {
	open := func(key string, def bool) bool {
		if ui.filter != "" {
			return true
		}
		if o, ok := ui.expanded[key]; ok {
			return o
		}
		return def
	}
	v.shut = map[string]bool{}
	v.inner = map[string]string{}
	for i, a := range v.agents {
		if i == 0 || v.agents[i-1].repo != a.repo {
			v.entryIDs = append(v.entryIDs, groupID(a.repo))
			v.shut[a.repo] = !open(groupKey(a.repo), true)
		}
		if v.shut[a.repo] {
			continue
		}
		if !a.folded {
			id := agentID(a.key)
			v.entryIDs = append(v.entryIDs, id)
			for _, sh := range v.under[a.key] {
				v.inner[sh.pane.PaneID] = id
			}
			for _, b := range v.bgUnder[a.key] {
				v.inner[bgID(b.ID)] = id
			}
		}
		if _, ok := v.quiet[a.repo]; ok && (i+1 == len(v.agents) || v.agents[i+1].repo != a.repo) {
			v.entryIDs = append(v.entryIDs, quietID(a.repo))
		}
	}
	if len(v.bgLoose) > 0 {
		v.entryIDs = append(v.entryIDs, bgHeadID)
		if v.bgOpen = open(bgKey, false); v.bgOpen {
			for _, b := range v.bgLoose {
				v.entryIDs = append(v.entryIDs, bgID(b.ID))
			}
		}
	}
	if len(v.shells) > 0 {
		v.entryIDs = append(v.entryIDs, shellsID)
		// folded while there are agents to look at; with none, the panes
		// are all there is
		if v.shOpen = open(shellsKey, len(v.agents) == 0); v.shOpen {
			for _, sh := range v.shells {
				v.entryIDs = append(v.entryIDs, sh.pane.PaneID)
			}
		}
	}
	if v.indexOf(v.sel) >= 0 || v.inner[v.sel] != "" {
		return
	}
	// gone, or folded away: an entry in a shut group selects its heading, else
	// the agent this sidebar sits beside, else the first entry
	was := v.sel
	v.sel = ""
	for _, sh := range v.shells {
		if sh.pane.PaneID == was {
			v.sel = shellsID
		}
	}
	for _, b := range v.bgLoose {
		if bgID(b.ID) == was {
			v.sel = bgHeadID
		}
	}
	for _, a := range v.agents {
		switch {
		case agentID(a.key) == was && v.shut[a.repo]:
			v.sel = groupID(a.repo)
		case a.current && v.sel == "" && v.shut[a.repo]:
			v.sel = groupID(a.repo)
		case a.current && v.sel == "" && v.indexOf(agentID(a.key)) >= 0:
			v.sel = agentID(a.key)
		}
	}
	if v.sel == "" && len(v.entryIDs) > 0 {
		v.sel = v.entryIDs[0]
	}
}

func (v *sbView) nestPanes() {
	owner := map[string]string{}
	for _, a := range v.agents {
		if _, ok := owner[a.wid]; !ok && !a.folded {
			owner[a.wid] = a.key
		}
	}
	var rest []sbShell
	for _, sh := range v.shells {
		if k, ok := owner[sh.pane.WindowID]; ok {
			if v.under == nil {
				v.under = map[string][]sbShell{}
			}
			v.under[k] = append(v.under[k], sh)
		} else {
			rest = append(rest, sh)
		}
	}
	v.shells = rest
}

func (v sbView) windowPanes(wid string) int {
	n := 0
	for _, a := range v.agents {
		if a.wid == wid && a.pane != "" {
			n++
		}
	}
	for _, sh := range v.shells {
		if sh.pane.WindowID == wid {
			n++
		}
	}
	for _, shs := range v.under {
		for _, sh := range shs {
			if sh.pane.WindowID == wid {
				n++
			}
		}
	}
	return n
}

func (v sbView) paneByID(id string) (fleet.Pane, bool) {
	for _, sh := range v.shells {
		if sh.pane.PaneID == id {
			return sh.pane, true
		}
	}
	for _, shs := range v.under {
		for _, sh := range shs {
			if sh.pane.PaneID == id {
				return sh.pane, true
			}
		}
	}
	return fleet.Pane{}, false
}

// foldOf is the fold an entry opens and shuts — a group's heading, a quiet
// trailer — and whether it is open now.
func (v sbView) foldOf(id string) (key string, open, ok bool) {
	switch {
	case strings.HasPrefix(id, "g:"):
		repo := strings.TrimPrefix(id, "g:")
		return groupKey(repo), !v.shut[repo], true
	case id == bgHeadID:
		return bgKey, v.bgOpen, true
	case id == shellsID:
		return shellsKey, v.shOpen, true
	case strings.HasPrefix(id, "x:"):
		repo := strings.TrimPrefix(id, "x:")
		return quietKey(repo), v.quiet[repo].open, true
	}
	return "", false, false
}

// groupSpace is the checkout a repo group's heading stands for: the main
// one, else the first of its worktrees.
func (v sbView) groupSpace(repo string) (sbSpace, bool) {
	var wt *sbSpace
	for i, s := range v.spaces {
		switch {
		case s.parent == "" && (s.git == nil || !s.git.Worktree) && s.label == repo:
			return s, true
		case wt == nil && s.repoName() == repo:
			wt = &v.spaces[i]
		}
	}
	if wt != nil {
		return *wt, true
	}
	return sbSpace{}, false
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
func stateLook(state *string, unseen bool, since *float64, now int64) (dot, colour string) {
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
		if since == nil || now-int64(*since) >= doneFadeAfter {
			return "○", dim
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

// paneLabelFit is the label in w columns. A server gives up its command's
// letters before its ports: the port is what the row is for.
func paneLabelFit(p fleet.Pane, w int) string {
	label := paneLabel(p)
	if p.Kind == "server" && len([]rune(label)) > w {
		if tail := label[len(p.Cmd):]; w >= len([]rune(tail))+2 {
			return truncate(p.Cmd, w-len([]rune(tail))) + tail
		}
	}
	return truncate(label, w)
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

// spacePos is where a space stands: branch and position, or its directory.
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

type summary struct{ text, colour string }

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
			return summary{text: *a.Doing, colour: dim}
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
		if a.LastMessage != nil && *a.LastMessage != "" {
			return summary{text: *a.LastMessage, colour: dim}
		}
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

// agentFacts is the inspector's model, context and cost. Unknowns are
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
//
// Top to bottom: kel and its two menus, the list, the inspector, a blank, the
// footer. Every list entry is one line in every state, selected or not, and
// the inspector is always inspectorRows tall, so nothing in the list moves
// when a state changes or the selection does (issue #59).
func renderView(v sbView, w, h int, now int64) (string, []lineRef) {
	if w <= 0 && h <= 0 {
		w = 32
	}
	if isRail(w) {
		return renderRail(v, w, h, now)
	}
	const in = " "
	gap := line{"", lineRef{}}
	body := v.listLines(w, now)

	// the top: kel, and its two menus. Mouse first (2026-11-04): what the key
	// grid at the foot listed is in these menus and in each entry's own,
	// right-click, with its key beside it — learned by using the mouse
	var head []line
	if w >= 16 {
		from := w - 1 - visWidth(button("+ New")+" "+button("⋯"))
		ref := lineRef{kind: "menu", id: "menu:new", id2: "menu:kel", from: from, split: from + visWidth(button("+ New")) + 1}
		on := v.hoverOn(ref, 0)
		right := buttonLit("+ New", on == "menu:new") + " " + buttonLit("⋯", on == "menu:kel")
		head = []line{{spread(in+bold+"kel"+reset, right, w), ref}, gap}
	}

	// the tail: a prompt — what it asks, what you type, how to answer — or
	// the last command's word, or the filter, or the footer. A prompt stands
	// out (bold, the accent `›`) because the author could not tell one had
	// opened
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
	foot := len(tail) == 0
	if foot {
		tail = []line{footerLine(w, in, len(v.needs), "")}
	}

	insp := v.inspector(w, now)
	if h > 0 && h < len(head)+len(insp)+1+len(tail)+3 {
		insp = nil // too short for one: the list keeps what room there is
	}
	if h > 0 {
		room := h - len(head) - len(tail)
		if insp != nil {
			room -= len(insp) + 1
		}
		target := v.sel
		if o := v.inner[target]; o != "" {
			target = o
		}
		body = scrollTo(body, max(room, 0), target)
		for len(body) < room {
			body = append(body, gap)
		}
	} else if len(body) > 0 {
		body = append(body, gap)
	}
	out := append(head, body...)
	inspEnd := len(out)
	if insp != nil {
		out = append(out, insp...)
		inspEnd = len(out)
		out = append(out, gap)
	}
	out = append(out, tail...)
	if foot && h > 0 {
		y := len(out) - 1
		out[y] = footerLine(w, in, len(v.needs), v.hoverKey(out[y].ref, y))
	}
	if h > 0 && len(out) > h {
		out = out[:h]
	}
	// an open menu draws over what is there, never between rows: kel's at
	// the row it was opened from, an entry's with its foot on the
	// inspector's last row
	if v.menu != nil {
		box := menuLines(*v.menu, w)
		y := v.menu.y
		if v.menu.entry != "" {
			y = max(0, inspEnd-len(box))
		}
		if h > 0 && y+len(box) > h {
			y = max(0, h-len(box))
		}
		for len(out) < y+len(box) {
			out = append(out, gap)
		}
		copy(out[y:], box)
	}
	// hover: the entry under the pointer gets a dim bar where the selection's
	// would be, so what a click would hit is visible before the click
	if v.hover > 0 && v.hover <= len(out) && v.menu == nil {
		hl := out[v.hover-1]
		if id := hl.ref.id; id != "" && id != v.sel && v.inner[v.sel] != id && listKind(hl.ref.kind) {
			s := hl.s
			if strings.HasPrefix(s, " ") {
				s = dim + "▏" + reset + s[1:]
			}
			out[v.hover-1].s = fill(s, w, lifted.mix(baseBg, 0.25).bg())
		}
	}
	return joinLines(out, w)
}

// listKind: a line of the list, which a hover lights.
func listKind(k string) bool {
	switch k {
	case "agent", "pane", "bg", "group", "fold":
		return true
	}
	return false
}

// scrollTo cuts the list to room lines with the target's line on screen. A
// group's heading stays above its rows: a screen that starts inside a group
// has the heading pinned at its top.
func scrollTo(body []line, room int, target string) []line {
	if len(body) <= room {
		return body
	}
	if room <= 0 {
		return nil
	}
	y := 0
	for i, l := range body {
		if target != "" && l.ref.id == target {
			y = i
		}
	}
	off := max(0, y-room+1)
	switch {
	case off == 0:
		return body[:room]
	case body[off].ref.kind == "group":
		return body[off : off+room]
	case body[off].s == "" && off+1 < len(body) && body[off+1].ref.kind == "group":
		return body[off+1 : min(len(body), off+1+room)]
	}
	head := -1
	for i := off; i >= 0; i-- {
		if body[i].ref.kind == "group" {
			head = i
			break
		}
	}
	if head < 0 || room < 3 {
		return body[off : off+room]
	}
	return append([]line{body[head]}, body[off+1:off+room]...)
}

// listLines is the list: one line per entry. Repo groups by name, each under
// its heading, folded by hand; then the background sessions with no agent to
// sit under, then the panes of windows with no agent.
func (v sbView) listLines(w int, now int64) []line {
	var body []line
	add := func(s string, r lineRef) { body = append(body, line{s, r}) }
	gap := func() {
		if len(body) > 0 {
			add("", lineRef{})
		}
	}
	marked := func(id string) bool { return id == v.sel || v.inner[v.sel] == id }
	// the bar, the glyph and a space before the name; one column clear of
	// the right edge
	avail := max(w-4, 0)
	row := func(id, kind, left, right string) {
		bar := " "
		if marked(id) {
			bar = accent + "▌" + reset
		}
		s := spread(bar+left, right, w)
		if marked(id) {
			s = fill(s, w, raised)
		}
		add(s, lineRef{id: id, kind: kind})
	}
	weight := func(s string, current bool) string {
		if current {
			return bold + s + reset
		}
		return s
	}
	// a heading: ▾ open, ▸ folded. A folded one says what it hides on its
	// right — a dot per agent, so one that needs you is never out of sight —
	// or, for panes, how many
	heading := func(id, name string, open, current bool, dots []string, count string) {
		g := "▸"
		if open {
			g = "▾"
		}
		n := len([]rune(name))
		rw := len(count)
		if len(dots) > 0 {
			rw = 2*len(dots) - 1
		}
		nw := min(n, max(avail-1-rw, 4))
		r := ""
		if count != "" {
			r = faint + count + reset
		} else if len(dots) > 0 {
			r = rollup(dots, avail-nw-1)
		}
		nm := dim + truncate(name, nw) + reset
		if current {
			nm = bold + truncate(name, nw) + reset
		}
		row(id, "group", dim+g+reset+" "+nm, r)
	}
	if v.unknown {
		add(" "+dim+truncate("? panes unknown", w-2)+reset, lineRef{})
		return body
	}
	if len(v.agents) == 0 && len(v.bgLoose) == 0 && len(v.shells) == 0 {
		add(" "+dim+truncate("nothing open", w-2)+reset, lineRef{})
		return body
	}
	for i, a := range v.agents {
		if i == 0 || v.agents[i-1].repo != a.repo {
			gap()
			var dots []string
			current := false
			if v.shut[a.repo] {
				for _, b := range v.agents[i:] {
					if b.repo != a.repo {
						break
					}
					d, c := agentDot(b.agent, now)
					dots = append(dots, c+d+reset)
					current = current || b.current
				}
			}
			name := a.repo
			if name == "" {
				name = "misc"
			}
			heading(groupID(a.repo), name, !v.shut[a.repo], current, dots, "")
		}
		if v.shut[a.repo] {
			continue
		}
		if !a.folded {
			d, c := agentDot(a.agent, now)
			lw, right := agentRight(a, avail, now)
			row(agentID(a.key), "agent", c+d+reset+" "+weight(truncate(a.agent.Name, lw), a.current), right)
		}
		if q, ok := v.quiet[a.repo]; ok && (i+1 == len(v.agents) || v.agents[i+1].repo != a.repo) {
			row(quietID(a.repo), "fold", "  "+dim+truncate(q.text(), avail-2)+reset, "")
		}
	}
	if len(v.bgLoose) > 0 {
		gap()
		var dots []string
		if !v.bgOpen {
			for _, b := range v.bgLoose {
				d, c, _ := bgLook(b)
				dots = append(dots, c+d+reset)
			}
		}
		heading(bgHeadID, "background", v.bgOpen, false, dots, "")
		for _, b := range v.bgLoose {
			if !v.bgOpen {
				break
			}
			d, c, _ := bgLook(b)
			txt, col := b.Group, dim
			if b.State == "blocked" {
				txt, col = "blocked", amberDim
			}
			lw, rw := fitPair(b.Name, len([]rune(txt)), avail)
			right := ""
			if rw > 0 {
				right = col + truncate(txt, rw) + reset
			}
			row(bgID(b.ID), "bg", c+d+reset+" "+truncate(b.Name, lw), right)
		}
	}
	if len(v.shells) > 0 {
		gap()
		count := ""
		if !v.shOpen {
			count = fmt.Sprint(len(v.shells))
		}
		heading(shellsID, "shells", v.shOpen, false, nil, count)
		seen := map[string]int{}
		for _, sh := range v.shells {
			seen[paneLabel(sh.pane)]++
		}
		for _, sh := range v.shells {
			if !v.shOpen {
				break
			}
			p := sh.pane
			txt, col := sh.where, dim
			if t, c := cardSlot(p.Cards); t != "" {
				txt, col = t, c
			}
			lw, rw := fitPair(paneLabel(p), len([]rune(txt)), avail)
			if n := len([]rune(paneLabel(p))); p.Kind == "server" && lw < n {
				// a port is never cut: the port is what the row is for
				lw = min(n, avail)
				if rw = avail - lw - 2; rw < 4 {
					rw = 0
				}
			}
			right := ""
			if rw > 0 {
				right = col + truncate(txt, rw) + reset
			}
			lbl := paneLabelFit(p, lw)
			if seen[paneLabel(p)] > 1 && p.WindowName != "" {
				if room := lw - len([]rune(lbl)) - 3; room > 0 {
					lbl += dim + " · " + truncate(p.WindowName, room) + reset
				}
			}
			row(p.PaneID, "pane", paneGlyph(p.Kind)+" "+weight(lbl, p.Current), right)
		}
	}
	return body
}

// agentRight is an agent row's right slot and the columns its name keeps.
// The ▪ (its window has a server) goes first when both do not fit.
func agentRight(a sbAgent, avail int, now int64) (nameW int, right string) {
	txt, col := slotText(a.agent, now)
	n := len([]rune(txt))
	server := a.server
	need := n
	if server {
		need = len([]rune(strings.TrimSpace("▪ " + txt)))
	}
	nameW, rw := fitPair(a.agent.Name, need, avail)
	if server && rw < need {
		server = false
		nameW, rw = fitPair(a.agent.Name, n, avail)
	}
	if rw <= 0 {
		return nameW, ""
	}
	if server {
		right = dim + "▪" + reset
		if txt != "" {
			right += " " + col + truncate(txt, rw-2) + reset
		}
		return nameW, right
	}
	if txt != "" {
		right = col + truncate(txt, rw) + reset
	}
	return nameW, right
}

// rollup is a folded heading's dots in room columns: spaced when they fit,
// packed when only that fits, else the most urgent first.
func rollup(dots []string, room int) string {
	switch {
	case room <= 0:
		return ""
	case 2*len(dots)-1 <= room:
		return strings.Join(dots, " ")
	case len(dots) <= room:
		return strings.Join(dots, "")
	}
	ranked := slices.Clone(dots)
	sort.SliceStable(ranked, func(i, j int) bool { return dotRank(ranked[i]) < dotRank(ranked[j]) })
	return strings.Join(ranked[:room], "")
}

// dotRank orders drawn dots by urgency, from their look: waiting and error
// first, then done unseen, then working, then the calm ones.
func dotRank(d string) int {
	switch {
	case strings.HasPrefix(d, yellow), strings.HasPrefix(d, red+"●"):
		return 0
	case strings.HasPrefix(d, green+"●"):
		return 1
	case strings.HasPrefix(d, cyan):
		return 2
	}
	return 3
}

// agentDot is an agent's first-column glyph (§5d's table). A `working` kel has
// not heard from in half an hour draws a grey ◌: no state colour it has not
// earned.
func agentDot(a fleet.Agent, now int64) (dot, colour string) {
	if quietFor(a, now) > 0 {
		return "◌", dim
	}
	return stateLook(a.State, a.Unseen, a.StateSince, now)
}

// slotText is the right of an agent's row, by priority: why it waits, dead,
// quota; an unseen card; else how long it has been in its state. A `working`
// kel has not heard from reads "quiet", grey italic: kel is not sure, and
// says so (invariant 3).
func slotText(a fleet.Agent, now int64) (text, colour string) {
	if q := quietFor(a, now); q > 0 {
		return "quiet " + humanizeSecs(q), dim + italic
	}
	if a.State == nil || *a.State == "" {
		return "unknown", dim
	}
	note := ""
	if a.Note != nil {
		note = shortNote(*a.Note)
	}
	switch *a.State {
	case "waiting":
		if note == "" {
			note = "waiting"
		}
		return note, amberDim
	case "error":
		if note == "" {
			note = "error"
		}
		return note, red
	case "dead":
		return "dead", red
	case "throttled":
		return "quota", purple
	}
	if t, c := cardSlot(a.Cards); t != "" {
		return t, c
	}
	if a.StateSince != nil {
		return humanizeSecs(now - int64(*a.StateSince)), dim
	}
	return "", ""
}

// cardSlot names an unseen card in a row's right slot: a decision asks for
// you, in amber; a result is dim.
func cardSlot(c *fleet.PaneCards) (text, colour string) {
	if c == nil || !c.Unseen || c.Latest == nil {
		return "", ""
	}
	if c.Latest.Kind == "decision" {
		return "▸ decision", amberDim
	}
	return "▸ " + c.Latest.Kind, dim
}

// joinLines is the frame's text and, line for line, what a click on each hits.
func joinLines(body []line, w int) (string, []lineRef) {
	var b strings.Builder
	refs := make([]lineRef, len(body))
	for i, l := range body {
		if i > 0 {
			b.WriteByte('\n')
		}
		ls := l.s
		if w > 0 && visWidth(ls) > w {
			ls = truncateStyled(ls, w)
		}
		b.WriteString(ls)
		refs[i] = l.ref
	}
	return b.String(), refs
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
				if v.windowPanes(a.wid) > 1 {
					acts = append(acts, sbAct{"m", "own window"}, sbAct{"S", "swap with…"})
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
	case strings.HasPrefix(v.sel, "g:"):
		repo := strings.TrimPrefix(v.sel, "g:")
		s, ok := v.groupSpace(repo)
		if !ok {
			break
		}
		acts := []sbAct{{"f", "files"}, {"g", "git"}}
		if s.git != nil {
			acts = append(acts, sbAct{"d", "diff"})
		}
		acts = append(acts, sbAct{"e", "nvim"}, sbAct{"t", "shell"})
		if s.ports != nil && len(*s.ports) > 0 {
			acts = append(acts, sbAct{"o", "browser"})
		}
		return "repo", repo, append(acts, sbAct{"c", "claude"})
	default:
		if p, ok := v.paneByID(v.sel); ok {
			acts := []sbAct{{"enter", "go to it"}}
			if p.Kind == "server" {
				acts = append(acts, sbAct{"o", "browser"})
			}
			if v.windowPanes(p.WindowID) > 1 {
				acts = append(acts, sbAct{"m", "own window"})
			}
			acts = append(acts, sbAct{"M", "move beside…"})
			if v.windowPanes(p.WindowID) > 1 {
				acts = append(acts, sbAct{"S", "swap with…"})
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
	y     int    // the row kel's own menus open at
	sel   int    // the item under the pointer or the arrow keys
	entry string // the entry this is the menu of, drawn over the inspector; "" = kel's
}

// newMenu holds what makes something.
func newMenu() []sbAct {
	return []sbAct{{"n", "New agent"}, {"w", "Agent in a worktree"}, {"|", "Split right"}, {"-", "Split down"}, {"t", "Shell window"}}
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
	var out []sbAct
	if what != "repo" {
		out = append(out, sbAct{"enter", "Go to it"})
	}
	if what == "agent" || what == "pane" {
		out = append(out, sbAct{"space", "Peek"})
	}
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

// button is a clickable word on a lifted fill, with half-block ends; under
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

// footerLine is the foot of the sidebar: what needs you, counted (`●N need you`), on the
// left; settings and help on the right. Each is a click: the count is `,
// the next agent that needs you. lit is the one under the pointer.
func footerLine(w int, in string, needs int, lit string) line {
	var hits []hit
	left := in
	if needs > 0 {
		t := fmt.Sprintf("●%d need you", needs)
		c := yellow + bold
		if lit == "`" {
			c += "\x1b[4m"
		}
		left += c + t + reset
		x := len([]rune(in))
		hits = append(hits, hit{x, x + len([]rune(t)), "`"})
	}
	icon := func(g, key string) string {
		if key == lit {
			return accent + bold + g + reset
		}
		return dim + g + reset
	}
	right := icon("≡", "open:settings") + "  " + icon("?", "open:help")
	x := w - 1 - visWidth(right)
	hits = append(hits, hit{x - 1, x + 2, "open:settings"}, hit{x + 2, x + 5, "open:help"})
	return line{spread(left, right, w), lineRef{kind: "foot", hits: hits}}
}

// hoverKey is the hit under the pointer on a line drawn as ref at row y, or "".
func (v sbView) hoverKey(ref lineRef, y int) string {
	if v.hover != y+1 {
		return ""
	}
	return ref.hitAt(v.hoverX)
}

// hitAt is the key of the button at column x, or "".
func (r lineRef) hitAt(x int) string {
	for _, h := range r.hits {
		if x >= h.x0 && x < h.x1 {
			return h.key
		}
	}
	return ""
}

// hoverOn is what the pointer is on in a line drawn as ref, at row y: an id,
// or "".
func (v sbView) hoverOn(ref lineRef, y int) string {
	if v.hover != y+1 {
		return ""
	}
	return ref.at(v.hoverX)
}

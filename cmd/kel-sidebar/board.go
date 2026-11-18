package main

import (
	"fmt"
	"math"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/tharpep/kelsier/internal/fleet"
)

// The board is the sidebar drawn at full width (issue #60): one rounded tile
// per agent, grouped by repo in the list's order, so the rail, the list and
// the board are one surface at three widths (invariant 6). A tile's place
// depends on the fleet's shape and the width alone — never on its state, its
// face or the selection — so nothing moves under the pointer (§5d).

type face int

const (
	faceCard face = iota
	faceScreen
	faceTimeline
)

var faceNames = []string{"card", "screen", "timeline"}

func parseFace(s string) (face, bool) {
	for i, n := range faceNames {
		if n == s {
			return face(i), true
		}
	}
	return 0, false
}

const (
	boardMinTile = 40 // three across at 150 columns, two at 100, one below 85
	boardGap     = 3
	tileBody     = 5 // every face fills the same five rows: boxes never resize
	tileH        = tileBody + 2
	stripSpan    = 3600
)

type boardOpts struct {
	face face
	loc  *time.Location // clock times in the timeline; UTC in the fixtures
}

type boardTile struct {
	id   string
	repo string
	a    *sbAgent
	bg   *fleet.Background
	wt   *fleet.Worktree // a worktree's row, below the tiles
}

const wtHeading = "worktrees · "

// boardTiles is every agent in the list's order — folds and quiet trailers
// are the list's, not the board's — then the background sessions, then each
// repo's linked worktrees, one row each: below every tile, so a worktree
// added or swept never moves one.
func boardTiles(v sbView) []boardTile {
	var ts []boardTile
	for i := range v.agents {
		a := &v.agents[i]
		ts = append(ts, boardTile{id: agentID(a.key), repo: a.repo, a: a})
	}
	for i := range v.bgLoose {
		b := &v.bgLoose[i]
		ts = append(ts, boardTile{id: bgID(b.ID), repo: "background", bg: b})
	}
	for _, r := range v.repos {
		for i := range r.Worktrees {
			w := &r.Worktrees[i]
			if v.filter != "" && !strings.Contains(strings.ToLower(wtLabel(*w)), strings.ToLower(v.filter)) {
				continue
			}
			ts = append(ts, boardTile{id: wtID(w.Path), repo: wtHeading + r.Name, wt: w})
		}
	}
	return ts
}

func wtID(path string) string { return "w:" + path }

// wtLabel names a worktree by its branch, as the list named it; Claude
// Code's own are named by their folder, which their branch only prefixes.
func wtLabel(w fleet.Worktree) string {
	if w.Branch != nil && !strings.Contains(w.Path, "/.claude/worktrees/") {
		return *w.Branch
	}
	return filepath.Base(w.Path)
}

func boardCols(w int) (cols, tileW int) {
	inner := w - 2
	cols = max(1, (inner+boardGap)/(boardMinTile+boardGap))
	return cols, max(0, (inner-boardGap*(cols-1))/cols)
}

// a block is a run of tiles on one row of the wall, with its repo's heading
// when it is the repo's first row; the board scrolls by whole blocks
type boardBlock struct {
	heading string
	tiles   []boardTile
}

func (b boardBlock) height() int {
	h := tileH
	if b.tiles[0].wt != nil {
		h = 1
		if b.heading != "" {
			h++ // the blank above a repo's worktrees
		}
	}
	if b.heading != "" {
		h++
	}
	return h
}

func boardBlocks(ts []boardTile, cols int) []boardBlock {
	var out []boardBlock
	for i, t := range ts {
		switch {
		case i == 0 || ts[i-1].repo != t.repo:
			out = append(out, boardBlock{heading: t.repo})
		case len(out[len(out)-1].tiles) == cols, t.wt != nil:
			out = append(out, boardBlock{})
		}
		out[len(out)-1].tiles = append(out[len(out)-1].tiles, t)
	}
	return out
}

// renderBoard draws the board at w×h (h == 0: all of it): the header, the
// wall, a blank and the keys. Past the bottom it scrolls by whole rows of
// tiles, just far enough to show the selected one.
func renderBoard(v sbView, w, h int, now int64, o boardOpts) (string, []lineRef) {
	if w <= 0 {
		w = 150
	}
	if o.loc == nil {
		o.loc = time.Local
	}
	ts := boardTiles(v)
	cols, tw := boardCols(w)
	blocks := boardBlocks(ts, cols)

	head := []line{{boardHeader(v, ts, w, now), lineRef{kind: "head"}}, {}}
	var body []line
	var above, below unseen
	switch {
	case v.unknown:
		body = []line{{" " + dim + truncate("? panes unknown", w-2) + reset, lineRef{}}}
	case len(ts) == 0:
		body = []line{{" " + dim + truncate("nothing open", w-2) + reset, lineRef{}}}
	default:
		area := h - len(head) - 2
		first, last := 0, len(blocks)
		if h > 0 {
			sel := -1
			for i, b := range blocks {
				for _, t := range b.tiles {
					if t.id == v.sel {
						sel = i
					}
				}
			}
			span := func(a, b int) (n int) {
				for _, bl := range blocks[a : b+1] {
					n += bl.height()
				}
				return
			}
			for sel >= 0 && first < sel && span(first, sel) > area {
				first++
			}
			last = first
			for last < len(blocks) && span(first, last) <= area {
				last++
			}
			last = max(last, first+1)
		}
		for i, b := range blocks {
			switch {
			case i < first:
				above.add(b)
			case i >= last:
				below.add(b)
			default:
				body = append(body, b.lines(v, w, tw, now, o)...)
			}
		}
	}

	foot := []line{{}, boardFooter(v, w, o.face, above, below)}
	if h > 0 {
		area := max(0, h-len(head)-len(foot))
		body = body[:min(len(body), area)]
		for len(body) < area {
			body = append(body, line{})
		}
	}
	all := append(append(head, body...), foot...)
	if h > 0 && len(all) > h {
		all = all[:h]
	}
	return joinLines(all, w)
}

// unseen is what the wall scrolled out of sight, in the units it scrolls
// by: rows of tiles, and worktree rows, which are said apart
type unseen struct{ rows, worktrees int }

func (u *unseen) add(b boardBlock) {
	if b.tiles[0].wt != nil {
		u.worktrees += len(b.tiles)
	} else {
		u.rows++
	}
}

// hint is `↓2`, `↓2 · 3 worktrees`, `↓1 worktree`, or "" with nothing hidden
func (u unseen) hint(arrow string) string {
	var parts []string
	if u.rows > 0 {
		parts = append(parts, fmt.Sprintf("%s%d", arrow, u.rows))
	}
	if u.worktrees > 0 {
		w := fmt.Sprintf("%d worktree", u.worktrees)
		if u.worktrees > 1 {
			w += "s"
		}
		if u.rows == 0 {
			w = arrow + w
		}
		parts = append(parts, w)
	}
	return strings.Join(parts, " · ")
}

func (b boardBlock) lines(v sbView, w, tw int, now int64, o boardOpts) []line {
	var out []line
	if b.heading != "" && b.tiles[0].wt != nil {
		out = append(out, line{})
	}
	if b.heading != "" {
		// no id: the list's folds are not the board's, so a click on a
		// heading here must not open or shut one in the sidebar
		out = append(out, line{" " + dim + truncate(b.heading, w-2) + reset, lineRef{kind: "group"}})
	}
	if t := b.tiles[0]; t.wt != nil {
		row := wtRow(*t.wt, w-2)
		if t.id == v.sel {
			row = fill(row, w-2, raised)
		}
		return append(out, line{" " + row, lineRef{kind: "worktree", hits: []hit{{1, w - 1, "sel:" + t.id}}}})
	}
	drawn := make([][]string, len(b.tiles))
	for i, t := range b.tiles {
		drawn[i] = t.draw(v, tw, now, o, t.id == v.sel)
	}
	for r := 0; r < tileH; r++ {
		var s strings.Builder
		s.WriteString(" ")
		x := 1
		var hits []hit
		for i, t := range b.tiles {
			if i > 0 {
				s.WriteString(strings.Repeat(" ", boardGap))
				x += boardGap
			}
			s.WriteString(drawn[i][r])
			hits = append(hits, hit{x, x + tw, "sel:" + t.id})
			x += tw
		}
		out = append(out, line{s.String(), lineRef{kind: "board", hits: hits}})
	}
	return out
}

// boardStep is the tile a move lands on: h and l walk the wall in reading
// order, j and k go to the same column a row down or up (the row's last
// tile when it is shorter). With no tile selected any move takes the first;
// at an edge it stays, "".
func boardStep(v sbView, cols int, k string) string {
	blocks := boardBlocks(boardTiles(v), cols)
	if len(blocks) == 0 {
		return ""
	}
	var flat []string
	bi, ti, at := -1, -1, -1
	for i, b := range blocks {
		for j, t := range b.tiles {
			if t.id == v.sel {
				bi, ti, at = i, j, len(flat)
			}
			flat = append(flat, t.id)
		}
	}
	if at < 0 {
		return flat[0]
	}
	switch k {
	case "left", "h":
		if at > 0 {
			return flat[at-1]
		}
	case "right", "l":
		if at+1 < len(flat) {
			return flat[at+1]
		}
	case "up", "k":
		if bi > 0 {
			b := blocks[bi-1].tiles
			return b[min(ti, len(b)-1)].id
		}
	case "down", "j":
		if bi+1 < len(blocks) {
			b := blocks[bi+1].tiles
			return b[min(ti, len(b)-1)].id
		}
	}
	return ""
}

// wtRow is a worktree in n columns: its land state's dot, its branch, what
// landing it needs, and where it is. `s` sweeps one that has landed.
func wtRow(w fleet.Worktree, n int) string {
	dot, word := dim+"○"+reset, landWord(w)
	switch {
	case w.Missing:
		dot = red + "✕" + reset
	case w.Land != nil && w.Land.Code == "merged":
		dot = green + "●" + reset
	}
	lw := min(32, max(8, n/4))
	s := "  " + dot + " " + fit(truncate(wtLabel(w), lw), lw) + "  " + fit(word, 26) + "  " + dim + tildify(w.Path) + reset
	return fit(s, n)
}

// landWord is what landing the worktree needs, as kel sweep would judge it;
// an answer kel does not have is ?, never clean (invariant 3).
func landWord(w fleet.Worktree) string {
	if w.Missing {
		return red + "missing" + reset
	}
	l := w.Land
	if l == nil {
		l = &fleet.Land{Code: "?"}
	}
	n := 0
	if l.N != nil {
		n = int(*l.N)
	}
	s := ""
	switch l.Code {
	case "merged":
		s = green + "merged" + reset
	case "dirty":
		s = yellow + fmt.Sprintf("●%d uncommitted", n) + reset
	case "unpushed":
		s = yellow + fmt.Sprintf("↑%d unpushed", n) + reset
	case "fresh":
		s = dim + "no commits" + reset
	case "review":
		s = mid + "review" + reset
	case "behind":
		s = dim + fmt.Sprintf("behind %d", n) + reset
	case "no_pr":
		s = dim + "no PR" + reset
	case "detached", "clean":
		s = dim + l.Code + reset
	default:
		s = dim + "?" + reset
	}
	switch {
	case w.Sessions > 0:
		s += dim + " · claude" + reset
	case w.Locked:
		s += dim + " · locked" + reset
	}
	return s
}

// ---------------------------------------------------------------- a tile

// draw is the tile as tileH strings of exactly tw columns.
func (t boardTile) draw(v sbView, tw int, now int64, o boardOpts, selected bool) []string {
	n := tw - 4
	var tl, tr, bl, br, border string
	var rows []string
	if t.bg != nil {
		dot, c, word := bgLook(*t.bg)
		border = strings.ReplaceAll(c, bold, "")
		tl = c + dot + reset + " " + bold + t.bg.Name + reset
		tr = dim + "bg" + reset
		switch o.face {
		case faceCard:
			wc := c
			if t.bg.State == "blocked" {
				wc = amberDim
			}
			rows = []string{wc + word + reset, dim + tildify(t.bg.Cwd) + reset, dim + "background session" + reset}
		case faceScreen:
			rows = []string{dim + "no pane: a background session" + reset}
		default:
			rows = []string{dim + "?" + reset}
		}
	} else {
		a := t.a.agent
		dot, c := agentDot(a, now)
		border = strings.ReplaceAll(c, bold, "")
		tl = c + dot + reset + " " + bold + a.Name + reset
		tr = dim + agentAge(a, now) + reset
		switch o.face {
		case faceCard:
			rows = v.cardRows(*t.a, n, now)
			bl, br = edgeFacts(a)
		case faceScreen:
			tl += " " + dim + t.repo + reset
			rows = screenRows(a)
		default:
			rows = timelineRows(a, n, now, o.loc)
			bl, br = edgeFacts(a)
		}
	}
	out := []string{edge(tw, "╭", "╮", tl, tr, border)}
	for i := 0; i < tileBody; i++ {
		s := ""
		if i < len(rows) {
			s = rows[i]
		}
		cell := " " + fit(s, n) + " "
		if selected {
			cell = fill(cell, max(0, tw-2), raised)
		}
		if tw < 2 {
			out = append(out, fit("", tw))
			continue
		}
		out = append(out, border+"│"+reset+fit(cell, tw-2)+border+"│"+reset)
	}
	return append(out, edge(tw, "╰", "╯", bl, br, border))
}

// edge is a tile's top or bottom: `╭─ left ───── right ─╮`. The right goes
// before the left is cut.
func edge(tw int, lc, rc, left, right, border string) string {
	if tw < 2 {
		return fit("", tw)
	}
	inner := tw - 2
	seg := func(s string) (string, int) {
		if s == "" {
			return border + "─" + reset, 1
		}
		return border + "─" + reset + " " + s + " ", visWidth(s) + 3
	}
	l, lw := seg(left)
	r, rw := "", 0
	if right != "" {
		r, rw = " "+right+" "+border+"─"+reset, visWidth(right)+3
	}
	if lw+rw > inner {
		r, rw = "", 0
	}
	if lw > inner {
		l, lw = fit(border+"─"+reset+" "+left, inner), inner
	}
	return border + lc + reset + l + border + strings.Repeat("─", max(0, inner-lw-rw)) + reset + r + border + rc + reset
}

// fit is s cut or padded to exactly w columns.
func fit(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if visWidth(s) > w {
		s = truncateStyled(s, w)
	}
	return s + strings.Repeat(" ", max(0, w-visWidth(s)))
}

func agentAge(a fleet.Agent, now int64) string {
	if q := quietFor(a, now); q > 0 {
		return humanizeSecs(q)
	}
	if a.State == nil || *a.State == "" || a.StateSince == nil {
		return "?"
	}
	return humanizeSecs(now - int64(*a.StateSince))
}

// edgeFacts is a tile's bottom edge: its model on the left; its PR, CI and
// diff on the right. Each is absent when kel cannot tell.
func edgeFacts(a fleet.Agent) (left, right string) {
	if a.Context != nil {
		if m := shortModel(a.Context.Model); m != "" {
			left = dim + m + reset
		}
	}
	var parts []string
	if p := a.PR; p != nil {
		s := mid + fmt.Sprintf("#%d", p.Number) + reset
		switch {
		case p.State == "merged" || p.State == "closed":
			s += " " + dim + p.State + reset
		case p.CI == nil:
			s += " " + dim + "?" + reset
		case *p.CI == "pass":
			s += " " + green + "✓" + reset
		case *p.CI == "fail":
			s += " " + red + "✕" + reset
		case *p.CI == "pending":
			s += " " + dim + "◌" + reset
		default:
			s += " " + dim + "?" + reset
		}
		parts = append(parts, s)
	}
	if d := a.Diff; d != nil && d.Added == 0 && d.Removed == 0 {
		parts = append(parts, dim+"±0"+reset)
	} else if d != nil {
		parts = append(parts, green+fmt.Sprintf("+%d", d.Added)+reset+" "+red+fmt.Sprintf("−%d", d.Removed)+reset)
	}
	return left, strings.Join(parts, "  ")
}

// cardRows: what it is saying or doing (two lines), its subagent or its
// window's server, the hour's state strip, and its context, cost and tasks.
func (v sbView) cardRows(sa sbAgent, n int, now int64) []string {
	a := sa.agent
	top := agentMessage(a, n, now)
	if x := v.extraLine(sa); x != "" {
		top = append(top, x)
	}
	for len(top) < 3 {
		top = append(top, "")
	}
	return append(top[:3], stateStrip(a, n, now), ctxRow(a, n))
}

func agentMessage(a fleet.Agent, n int, now int64) []string {
	if q := quietFor(a, now); q > 0 {
		return []string{dim + italic + "quiet " + humanizeSecs(q) + reset}
	}
	if a.State == nil || *a.State == "" {
		return []string{dim + "unknown" + reset}
	}
	if c := a.Cards; c != nil && c.Unseen && c.Latest != nil {
		col := dim
		if c.Latest.Kind == "decision" {
			col = yellow
		}
		return paint(wrap2("▸ "+c.Latest.Kind+": "+c.Latest.Title, n), col)
	}
	note := ""
	if a.Note != nil {
		note = shortNote(*a.Note)
	}
	last := ""
	if a.LastMessage != nil {
		last = *a.LastMessage
	}
	switch *a.State {
	case "waiting":
		if note == "" {
			return []string{amberDim + "waiting" + reset}
		}
		if q := strings.Index(note, "? "); strings.HasPrefix(note, "allow ") && q > 0 {
			return []string{amberDim + note[:q+1] + reset, truncate(note[q+2:], n)}
		}
		return paint(wrap2(note, n), amberDim)
	case "error":
		if note == "" {
			note = "error"
		}
		return paint(wrap2(note, n), red)
	case "dead":
		out := []string{red + "dead" + reset}
		if last != "" {
			out = append(out, dim+truncate("last: "+firstLine(last), n)+reset)
		}
		return out
	case "throttled":
		s := purple + "quota" + reset
		if note != "" {
			s += dim + " · " + note + reset
		}
		return []string{s}
	case "working":
		if a.Doing != nil && *a.Doing != "" {
			return wrap2(*a.Doing, n)
		}
	case "done", "idle":
		if last != "" {
			col := dim
			if *a.State == "done" && a.Unseen {
				col = ""
			}
			return paint(wrap2(firstLine(last), n), col)
		}
	}
	return nil
}

// extraLine is the card's third line: the running subagent, else a server
// in the agent's window.
func (v sbView) extraLine(sa sbAgent) string {
	running := 0
	var first fleet.Subagent
	for _, s := range sa.agent.Subagents {
		if s.Status == "running" {
			if running == 0 {
				first = s
			}
			running++
		}
	}
	if running > 0 {
		s := "└ " + first.Label
		if first.AgentType != "" {
			s = "└ " + first.AgentType + " · " + first.Label
		}
		if running > 1 {
			s += fmt.Sprintf(" +%d", running-1)
		}
		return dim + s + reset
	}
	var servers []string
	for _, sh := range v.under[sa.key] {
		if sh.pane.Kind == "server" {
			servers = append(servers, "▪ "+paneLabel(sh.pane))
		}
	}
	if len(servers) > 0 {
		return dim + strings.Join(servers, "  ") + reset
	}
	return ""
}

func firstLine(s string) string {
	return strings.TrimSpace(strings.SplitN(strings.TrimSpace(s), "\n", 2)[0])
}

func paint(ls []string, col string) []string {
	if col == "" {
		return ls
	}
	for i, l := range ls {
		ls[i] = col + l + reset
	}
	return ls
}

// wrap2 breaks s into at most two lines of n columns, between words; the
// second is cut with … when s runs on.
func wrap2(s string, n int) []string {
	s = strings.Join(strings.Fields(s), " ")
	if n <= 0 || s == "" {
		return nil
	}
	r := []rune(s)
	if len(r) <= n {
		return []string{s}
	}
	cut := n
	for i := n; i > n/2; i-- {
		if r[i] == ' ' {
			cut = i
			break
		}
	}
	return []string{strings.TrimRight(string(r[:cut]), " "), truncate(strings.TrimLeft(string(r[cut:]), " "), n)}
}

func ctxRow(a fleet.Agent, n int) string {
	left := ""
	if c := a.Context; c != nil {
		if c.Pct != nil {
			left = bar(*c.Pct, 10) + " " + dim + fmt.Sprintf("%.0f%%", *c.Pct) + reset
		} else {
			left = dim + "ctx ?" + reset
		}
		if c.CostUSD != nil {
			left += "  " + money(*c.CostUSD)
		} else {
			left += "  " + dim + "$?" + reset
		}
	}
	right := ""
	if t := a.Tasks; t != nil && t.Total > 0 {
		if t.Total <= 10 {
			right = mid + strings.Repeat("■", t.Done) + reset + dim + strings.Repeat("□", max(0, t.Total-t.Done)) + reset
		} else {
			right = mid + taskCount(t) + reset
		}
	}
	return pairIn(left, right, n)
}

// pairIn sets right flush to the end of n columns, after left; right goes
// when both will not fit.
func pairIn(left, right string, n int) string {
	lw, rw := visWidth(left), visWidth(right)
	if right == "" || lw+1+rw > n {
		return left
	}
	return left + strings.Repeat(" ", n-lw-rw) + right
}

func bar(pct float64, n int) string {
	full := int(math.Round(pct / 100 * float64(n)))
	full = max(0, min(n, full))
	col := green
	if pct >= 70 {
		col = yellow
	}
	return col + strings.Repeat("━", full) + reset + dim + strings.Repeat("━", n-full) + reset
}

// ---------------------------------------------------------------- history

// events is the agent's transitions, oldest first: its log, and the state it
// is in now from state_since, which the log may not have reached. A hook
// that fires twice in one state logs it twice; that is one stretch.
func events(a fleet.Agent) []fleet.Transition {
	var ev []fleet.Transition
	if a.History != nil {
		ev = append(ev, *a.History...)
	}
	sort.SliceStable(ev, func(i, j int) bool { return ev[i].At < ev[j].At })
	if a.State != nil && *a.State != "" && a.StateSince != nil {
		if len(ev) == 0 || ev[len(ev)-1].State != *a.State {
			note := ""
			if a.Note != nil {
				note = *a.Note
			}
			ev = append(ev, fleet.Transition{At: *a.StateSince, State: *a.State, Note: note})
		}
	}
	sort.SliceStable(ev, func(i, j int) bool { return ev[i].At < ev[j].At })
	out := ev[:0]
	for _, e := range ev {
		if len(out) == 0 || out[len(out)-1].State != e.State {
			out = append(out, e)
		}
	}
	return out
}

func stripCell(state string) (glyph, colour string) {
	switch state {
	case "working":
		return "▆", cyan
	case "waiting":
		return "▆", yellow
	case "done":
		return "▆", green
	case "throttled":
		return "▆", purple
	case "error":
		return "▆", red
	case "dead":
		return "▁", red
	case "idle":
		return "▁", dim
	}
	return " ", ""
}

// stateStrip is the last hour in n cells, oldest left. A stretch kel holds
// no record of is blank, never idle.
func stateStrip(a fleet.Agent, n int, now int64) string {
	ev := events(a)
	if len(ev) == 0 || n <= 0 {
		return ""
	}
	var b strings.Builder
	cur := "\x00"
	for i := 0; i < n; i++ {
		t := float64(now-stripSpan) + (float64(i)+0.5)*stripSpan/float64(n)
		state := ""
		for _, e := range ev {
			if e.At <= t {
				state = e.State
			}
		}
		g, c := stripCell(state)
		if c != cur {
			if cur != "\x00" && cur != "" {
				b.WriteString(reset)
			}
			b.WriteString(c)
			cur = c
		}
		b.WriteString(g)
	}
	if cur != "" {
		b.WriteString(reset)
	}
	return b.String()
}

func clock(at float64, loc *time.Location) string {
	return time.Unix(int64(at), 0).In(loc).Format("15:04")
}

// timelineRows: the hour's ends, its strip, the last two transitions, and
// the day's waits counted over what the log holds.
func timelineRows(a fleet.Agent, n int, now int64, loc *time.Location) []string {
	ev := events(a)
	if len(ev) == 0 {
		return []string{dim + "?" + reset}
	}
	rows := []string{pairIn(dim+clock(float64(now-stripSpan), loc)+reset, dim+"now"+reset, n), stateStrip(a, n, now)}
	recent := ev[max(0, len(ev)-2):]
	for _, e := range recent {
		s := dim + clock(e.At, loc) + reset + " "
		_, c := stripCell(e.State)
		if c == "" {
			c = dim
		}
		if e.State == "waiting" && e.Note != "" {
			s += amberDim + shortNote(e.Note) + reset
		} else {
			s += c + e.State + reset
			if e.Note != "" && e.State != "working" {
				s += " " + dim + shortNote(e.Note) + reset
			}
		}
		rows = append(rows, s)
	}
	for len(rows) < 4 {
		rows = append(rows, "")
	}
	if a.History != nil {
		rows = append(rows, dim+waitSummary(ev, now, loc)+reset)
	}
	return rows
}

// waitSummary counts today's waits and the longest. The log is trimmed, so
// a log that starts after midnight says "since" its first line, not "today".
func waitSummary(ev []fleet.Transition, now int64, loc *time.Location) string {
	t := time.Unix(now, 0).In(loc)
	from := float64(time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc).Unix())
	when := "today"
	if ev[0].At > from {
		from, when = ev[0].At, "since "+clock(ev[0].At, loc)
	}
	waits, longest := 0, 0.0
	for i, e := range ev {
		if e.State != "waiting" || e.At < from {
			continue
		}
		waits++
		end := float64(now)
		if i+1 < len(ev) {
			end = ev[i+1].At
		}
		longest = math.Max(longest, end-e.At)
	}
	switch waits {
	case 0:
		return "no waits " + when
	case 1:
		return "1 wait " + when + " · " + humanizeSecs(int64(longest))
	}
	return fmt.Sprintf("%d waits %s · longest %s", waits, when, humanizeSecs(int64(longest)))
}

// ---------------------------------------------------------------- screen

var termEscape = regexp.MustCompile("\x1b\\[[0-9;?]*[ -/]*[@-~]|\x1b\\][^\x07\x1b]*(\x07|\x1b\\\\)|[\x00-\x08\x0b-\x1f\x7f]")

// screenRows is the pane's last lines, as plain text: kel shows the screen,
// it does not let the agent's colours or cursor codes into its own frame.
func screenRows(a fleet.Agent) []string {
	if a.Screen == nil {
		return []string{dim + "?" + reset}
	}
	ls := *a.Screen
	ls = ls[max(0, len(ls)-tileBody):]
	out := make([]string, len(ls))
	for i, l := range ls {
		out[i] = strings.ReplaceAll(termEscape.ReplaceAllString(l, ""), "\t", "    ")
	}
	return out
}

// ---------------------------------------------------------------- frame

type countLook struct{ word, glyph, colour string }

var countOrder = []countLook{
	{"need you", "●", yellow},
	{"working", "●", cyan},
	{"throttled", "◌", purple},
	{"done", "●", green},
	{"idle", "○", dim},
	{"stopped", "✕", red},
	{"unknown", "?", dim},
}

func tileCount(t boardTile) string {
	if t.wt != nil {
		return ""
	}
	if t.bg != nil {
		switch {
		case bgNeedsYou(*t.bg):
			return "need you"
		case t.bg.Status == "busy":
			return "working"
		case t.bg.Status == "idle":
			return "idle"
		}
		return "unknown"
	}
	a := t.a.agent
	if a.State == nil || *a.State == "" {
		return "unknown"
	}
	if agentNeedsYou(a) {
		return "need you"
	}
	switch s := *a.State; {
	case s == "dead":
		return "stopped"
	case s == "working" || s == "throttled" || s == "done" || s == "idle":
		return s
	}
	return "unknown"
}

// boardHeader: kel board, the agents counted by state, the 5h quota, esc.
// Short of room the counts lose their words, then the right side loses esc.
func boardHeader(v sbView, ts []boardTile, w int, now int64) string {
	n := map[string]int{}
	for _, t := range ts {
		n[tileCount(t)]++
	}
	n["need you"] = v.needCount()
	counts := func(words bool) string {
		var parts []string
		for _, c := range countOrder {
			if n[c.word] == 0 {
				continue
			}
			s := fmt.Sprintf("%s%d", c.glyph, n[c.word])
			if words {
				s = fmt.Sprintf("%s %d %s", c.glyph, n[c.word], c.word)
			}
			parts = append(parts, c.colour+s+reset)
		}
		sep := " "
		if words {
			sep = "   "
		}
		return strings.Join(parts, sep)
	}
	quota := dim + "5h ?" + reset
	if q := v.quota; q != nil && q.FiveHour != nil && q.FiveHour.Pct != nil {
		p := *q.FiveHour.Pct
		quota = dim + "5h" + reset + " " + bar(p, 16) + " " + dim + fmt.Sprintf("%.0f%%", p) + reset
	}
	right := []string{quota, dim + "esc" + reset}
	title := " " + bold + "kel board" + reset
	for _, words := range []bool{true, false} {
		for k := len(right); k >= 1; k-- {
			left := title
			if c := counts(words); c != "" {
				left += "     " + c
			}
			r := strings.Join(right[:k], "    ")
			if visWidth(left)+2+visWidth(r) <= w-1 {
				return spread(left, r, w)
			}
		}
	}
	return truncateStyled(title+"  "+counts(false), w)
}

// boardFooter: the keys, and on the right which face is showing, with what
// is scrolled out of sight: rows of tiles, and worktrees apart. A prompt (a kill's question, the
// filter, d's base) or the last command's word takes the line instead.
func boardFooter(v sbView, w int, f face, above, below unseen) line {
	if v.promptTitle != "" {
		c := v.promptColour
		if c == "" {
			c = accent
		}
		s := " " + c + bold + v.promptTitle + reset
		if v.input != "" {
			s += "  " + v.input
		}
		if v.hint2 != "" {
			s += "   " + dim + v.hint2 + reset
		}
		return line{truncateStyled(s, w-1), lineRef{kind: "foot"}}
	}
	if v.footer != "" {
		c := v.footerColour
		if c == "" {
			c = mid
		}
		return line{" " + c + truncate(v.footer, w-2) + reset, lineRef{kind: "foot"}}
	}
	acts := []sbAct{{"enter", "go"}, {"space", "peek"}, {"v", "face"}, {"a", "seen"}, {"d", "diff"}, {"x", "kill"}, {"/", "filter"}}
	if strings.HasPrefix(v.sel, "w:") {
		acts = []sbAct{{"s", "sweep"}, {"v", "face"}, {"/", "filter"}}
	}
	keys := chips(acts)
	var fs []string
	for i, n := range faceNames {
		if face(i) == f {
			fs = append(fs, bold+n+reset)
		} else {
			fs = append(fs, faint+n+reset)
		}
	}
	right := strings.Join(fs, faint+" · "+reset)
	var more []string
	for _, s := range []string{above.hint("↑"), below.hint("↓")} {
		if s != "" {
			more = append(more, s)
		}
	}
	if len(more) > 0 {
		right = dim + strings.Join(more, "  ") + reset + "   " + right
	}
	if v.filter != "" {
		right = accent + "/" + v.filter + reset + dim + "  esc clears" + reset + "   " + right
	}
	keys.right = right
	var hits []hit
	for _, h := range keys.hits {
		if h.x1 <= w-1 {
			hits = append(hits, hit{h.x0 + 1, h.x1 + 1, h.key})
		}
	}
	return line{irowString(keys, w), lineRef{kind: "foot", hits: hits}}
}

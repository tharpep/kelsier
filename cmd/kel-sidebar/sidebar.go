package main

// kel-sidebar: the pane on the left of every kel window (v0.10).
//
// This file is the live half — tmux, the clock, the state directory, the
// keys. What it draws is sbview.go, which is pure so its frames can be held by
// golden tests. There is no way to quit: the pane is kel's, and closing it is
// `kel sidebar off`.
//
// One of these runs in every window, so a hidden one must cost nothing. It is
// "visible" when its window is the active window of an attached session —
// one cheap tmux call. Visible: reload every 2s. Hidden: look again every 10s
// and do nothing else. SIGUSR1 (`kel _sidebar wake`, sent on window changes
// and agent state changes) makes it look at once.
//
// Loading is never done inside Update: fleet.Load runs git and the port scan,
// and a keypress must not wait on either (spec.md §5e). A load is a tea.Cmd
// whose result arrives as sbLoaded; the visible sidebar leaves the document
// in sidebar-cache.json, so a sidebar that has just become visible draws from
// that at once and only then asks for a fresh one.
//
// Every action that changes the workspace is a kel command — `kel new`,
// `kel kill`, `kel rename` — so kill-safety lives in one place, cmd_kill, and
// the sidebar never re-implements a refusal.

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/tharpep/kelsier/internal/fleet"
)

type sbTick struct{}
type sbWake struct{}
type sbLoaded struct{ doc fleet.Doc }
type sbBase struct{ ref string } // d's default base, worked out off the key path

// sbDone is a kel command finishing: its output becomes the footer message.
type sbDone struct {
	ok  bool
	msg string
}

// the footer's modes: typing a filter, typing a name for n / w / r, or
// answering the kill question
type sbMode int

const (
	modeNormal sbMode = iota
	modeFilter
	modeNew
	modeNewWorktree
	modeRename
	modeKill
	modeQuit
	modeSweep
	modeCommand
	modeDiff
)

type sbModel struct {
	doc           fleet.Doc
	ui            uiState
	width, height int
	myPane        string
	visible       bool
	loading       bool

	mode   sbMode
	input  string // the name being typed
	cands  string // tab's choices when a typed directory is ambiguous
	hist   int    // how far back ;'s history is, 0 = what is being typed
	target string // the entry a pending kill or rename acts on
	msg    string // the last command's result, shown until the next key
	msgBad bool
	help   bool

	pending tea.Cmd // a command activate() started, returned by the caller
}

// ---------------------------------------------------------------- state dir

func sbStatePath(name string) string { return filepath.Join(fleet.StateDir(), name) }

func sbStateFile() string { return sbStatePath("sidebar.json") }

// sbShared is the view state every window's sidebar shares (spec.md §5):
// the cursor and which spaces have been opened or closed by hand.
type sbShared struct {
	Selected string          `json:"selected"`
	Expanded map[string]bool `json:"expanded,omitempty"`
}

func sbLoadShared() sbShared {
	var v sbShared
	if b, err := os.ReadFile(sbStateFile()); err == nil {
		_ = json.Unmarshal(b, &v)
	}
	return v
}

func (m *sbModel) saveShared() {
	b, _ := json.Marshal(sbShared{Selected: m.ui.sel, Expanded: m.ui.expanded})
	fleet.WriteAtomic(sbStateFile(), b)
}

func sbLoadCache() (fleet.Doc, bool) {
	var d fleet.Doc
	b, err := os.ReadFile(sbStatePath("sidebar-cache.json"))
	if err != nil || json.Unmarshal(b, &d) != nil {
		return d, false
	}
	return d, true
}

// markSeen records that this sidebar's window is being looked at, for `done`
// until seen (spec.md §9). kel-fleet compares it with the state's epoch.
func (m *sbModel) markSeen() {
	if m.ui.myWindow == "" {
		return
	}
	fleet.WriteAtomic(sbStatePath(m.ui.myWindow+".seen"), []byte(strconv.FormatInt(time.Now().Unix(), 10)+"\n"))
}

// ---------------------------------------------------------------- tmux

// visible: this pane's window is its session's active window, and someone is
// attached to that session. Format "1 2 0" = active, two clients, and — the
// third field — whether this pane is the window's active one, which is the
// fallback for focus when the terminal does not report it.
func parseVisible(s string) (visible, active bool) {
	f := strings.Fields(s)
	if len(f) < 2 || f[0] != "1" {
		return false, false
	}
	n, err := strconv.Atoi(f[1])
	visible = err == nil && n > 0
	return visible, visible && len(f) > 2 && f[2] == "1"
}

func (m *sbModel) checkVisible() {
	if m.myPane == "" {
		m.visible = true // not in tmux (--once, a test): draw as if seen
		return
	}
	out, err := exec.Command("tmux", "display-message", "-p", "-t", m.myPane,
		"#{window_active} #{session_attached} #{pane_active}").Output()
	if err != nil {
		m.visible = false
		return
	}
	m.visible, m.ui.focused = parseVisible(string(out))
}

func jump(wid, pane string) {
	args := []string{"select-window", "-t", wid}
	if pane != "" {
		args = append(args, ";", "select-pane", "-t", pane)
	}
	_ = exec.Command("tmux", args...).Run()
}

// kelBin is the kel that made this sidebar (sidebar_ensure passes it), so the
// test suite's sidebars run the suite's kel, never an installed one.
func kelBin() string {
	if b := os.Getenv("KEL_BIN"); b != "" {
		return b
	}
	return filepath.Join(os.Getenv("HOME"), ".local", "bin", "kel")
}

// runKel runs a kel command and reports its last line. stderr is where kel's
// refusals go — the file a kill would lose — so it is what the footer shows.
func runKel(dir string, env []string, args ...string) tea.Cmd {
	return runKelSaying("", dir, env, args...)
}

// runKelSaying is runKel with the footer's words for a success, when kel's
// own output is not the thing to show.
func runKelSaying(ok, dir string, env []string, args ...string) tea.Cmd {
	return func() tea.Msg { return kelDone(ok, dir, env, args...) }
}

func kelDone(ok, dir string, env []string, args ...string) sbDone {
	c := exec.Command(kelBin(), args...)
	c.Dir = dir
	c.Env = append(os.Environ(), env...)
	out, err := c.CombinedOutput()
	if err == nil && ok != "" {
		return sbDone{ok: true, msg: ok}
	}
	return sbDone{ok: err == nil, msg: summarise(string(out))}
}

// summarise picks the line worth a footer. A kill refusal ends with the
// advice to commit, but what matters at a glance is what would have been
// lost, so that is what it shows (invariant 1: name the work at risk).
func summarise(out string) string {
	if strings.Contains(out, "refusing to kill") {
		var risk []string
		for _, l := range strings.Split(stripANSI(out), "\n") {
			if strings.HasPrefix(l, "    ") {
				risk = append(risk, strings.TrimSpace(l))
			}
		}
		if len(risk) > 3 {
			risk = append(risk[:3], "…")
		}
		return "kept — at risk: " + strings.Join(risk, ", ")
	}
	return lastLine(out)
}

func lastLine(s string) string {
	s = strings.TrimRight(stripANSI(s), "\n ")
	if i := strings.LastIndex(s, "\n"); i >= 0 {
		s = s[i+1:]
	}
	return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(s), "kel"))
}

// ---------------------------------------------------------------- loading

func (m *sbModel) loadCmd() tea.Cmd {
	if m.loading {
		return nil
	}
	m.loading = true
	trace(m.myPane)
	return func() tea.Msg { return sbLoaded{fleet.Load(fleet.Options{Panes: true})} }
}

// trace logs each load for the suite's sleep/wake checks (KEL_SIDEBAR_TRACE)
func trace(pane string) {
	if t := os.Getenv("KEL_SIDEBAR_TRACE"); t != "" {
		if f, err := os.OpenFile(t, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
			fmt.Fprintf(f, "reload %s %d\n", pane, time.Now().Unix())
			f.Close()
		}
	}
}

func tickAfter(visible bool) tea.Cmd {
	d := 10 * time.Second
	if visible {
		d = 2 * time.Second
	}
	return tea.Tick(d, func(time.Time) tea.Msg { return sbTick{} })
}

// ---------------------------------------------------------------- entries

func (m sbModel) view() sbView {
	v := buildView(m.doc, m.ui)
	ask := func(title, colour, hint string) {
		v.promptTitle, v.promptColour, v.hint2 = title, colour, hint
	}
	typed := m.input + "▏"
	switch m.mode {
	// every prompt says what it asks, shows what you type, and how to
	// answer, on a line you can also click
	case modeFilter:
		ask("filter by name", "", "enter keep · esc clear")
		v.input = typed
	case modeNew, modeNewWorktree:
		// where it will start, said before enter: the selected space, or
		// the directory typed after the name
		what := "new agent"
		if m.mode == modeNewWorktree {
			what = "new worktree agent"
		}
		ask(what+" in "+m.newWhere(), "", "enter go · type a name · tab dir")
		if m.cands != "" {
			v.hint2 = m.cands
		}
		v.input = typed
	case modeRename:
		ask("rename "+m.entryName(m.target), "", "enter ok · esc cancel")
		v.input = typed
	case modeCommand:
		ask("kel command", "", "enter run · esc cancel · ↑ last")
		v.input = typed
	case modeDiff:
		what := "diff"
		if sp, ok := m.selSpace(m.target); ok {
			what = "diff " + sp.label + " against"
		}
		ask(what, "", "enter ok · esc cancel · tab branch")
		if m.cands != "" {
			v.hint2 = m.cands
		}
		v.input = typed
	case modeKill:
		ask("kill "+m.entryName(m.target)+"?", yellow, "y yes · n no")
	case modeQuit:
		ask("quit kel?", yellow, "y yes · n no")
	case modeSweep:
		q := "sweep everything landed?"
		if m.target != "" {
			q = "sweep " + filepath.Base(m.target) + "?"
			if sp, ok := m.selSpace(spaceID(m.target)); ok {
				q = "sweep " + sp.label + "?"
			}
		}
		ask(q, yellow, "y yes · n no")
	default:
		if m.msg != "" {
			v.footer, v.footerColour = m.msg, ""
			if m.msgBad {
				v.footerColour = red
			}
		}
	}
	return v
}

// cancel ends a prompt as esc would: what a click elsewhere, or leaving the
// sidebar, does — the author was stuck typing a name they no longer wanted.
// A filter is kept: you typed it to see the result.
func (m *sbModel) cancel() {
	if m.mode == modeNormal {
		return
	}
	m.mode, m.input, m.cands = modeNormal, "", ""
}

func (m *sbModel) move(d int) {
	v := m.view()
	i := v.indexOf(v.sel) + d
	if i < 0 || i >= len(v.entryIDs) {
		return
	}
	m.ui.sel = v.entryIDs[i]
	m.saveShared()
}

func (m *sbModel) toggle(key string, open bool) {
	if m.ui.expanded == nil {
		m.ui.expanded = map[string]bool{}
	}
	m.ui.expanded[key] = !open
	m.saveShared()
}

// agentOf reads the built view, not view(): view() draws the kill prompt,
// which names the agent through here
func (m sbModel) agentOf(id string) (sbAgent, bool) {
	for _, a := range buildView(m.doc, m.ui).agents {
		if agentID(a.wid) == id {
			return a, true
		}
	}
	return sbAgent{}, false
}

func (m sbModel) entryName(id string) string {
	if a, ok := m.agentOf(id); ok {
		return a.agent.Name
	}
	return id
}

// activate is enter, and a click: jump to an agent or pane; open or close a
// space.
func (m *sbModel) activate(id string) {
	v := m.view()
	switch {
	case strings.HasPrefix(id, "s:"):
		for _, s := range v.spaces {
			if spaceID(s.key) == id {
				m.toggle(s.key, s.open)
			}
		}
	case strings.HasPrefix(id, "a:"):
		if a, ok := m.agentOf(id); ok {
			jump(a.wid, a.pane)
		}
	case strings.HasPrefix(id, "b:"):
		// a background agent has no pane to jump to: open it, `claude
		// attach`, in a popup — prefix q closes it, and it keeps running
		if m.doc.Background != nil {
			for _, b := range *m.doc.Background {
				if bgID(b.ID) == id {
					m.pending = runKel(b.Cwd, nil, "_panel", "attach", b.Cwd, b.Name, b.ID)
				}
			}
		}
	default:
		if m.doc.Panes != nil {
			for _, p := range *m.doc.Panes {
				if p.PaneID == id {
					jump(p.WindowID, p.PaneID)
				}
			}
		}
	}
}

// spaceFor is the space the selected entry stands in: where n and w create
// the new agent. Its directory and its repo label (the group) — passing the
// group keeps a new agent beside its repo's others even from a worktree,
// whose directory name is not the repo's.
func (m sbModel) spaceFor(id string) (dir, group string) {
	s, ok := m.selSpace(id)
	if !ok {
		return os.Getenv("HOME"), ""
	}
	return s.dir(), s.repoName()
}

// repoName is the group a space's agents belong in: the repo, which for a
// nested worktree is not its label (that is the branch)
func (s sbSpace) repoName() string {
	if s.repo != "" {
		return s.repo
	}
	return s.label
}

func (s sbSpace) dir() string {
	if s.git != nil && s.git.Root != "" {
		return s.git.Root
	}
	return s.cwd
}

// title names a space in a popup's border: the repo, and the branch when
// there is one — `kelsier/fix-auth`
func (s sbSpace) title() string {
	if s.git != nil && s.git.Branch != nil {
		return s.repoName() + "/" + *s.git.Branch
	}
	return s.repoName()
}

// selSpace is the space the selected entry stands in.
func (m sbModel) selSpace(id string) (sbSpace, bool) {
	v := buildView(m.doc, m.ui)
	key := ""
	switch {
	case strings.HasPrefix(id, "s:"):
		key = strings.TrimPrefix(id, "s:")
	case m.doc.Panes != nil:
		wid := ""
		if a, ok := m.agentOf(id); ok {
			wid = a.wid
		}
		for _, p := range *m.doc.Panes {
			if p.PaneID == id || (wid != "" && p.WindowID == wid) {
				key, _ = spaceKey(p)
				break
			}
		}
	}
	for _, s := range v.spaces {
		if s.key == key {
			return s, true
		}
	}
	return sbSpace{}, false
}

// panel opens one of the popups at the selected entry's space (spec.md §5f).
func (m sbModel) panel(what string) (sbModel, tea.Cmd) {
	sp, ok := m.selSpace(m.view().sel)
	if !ok {
		m.msg, m.msgBad = "select something first", true
		return m, nil
	}
	dir, title := sp.dir(), sp.title()
	switch what {
	case "diff":
		// against what: the base it lands on, offered and editable — the
		// author could not diff against anything else. The base is a few
		// git calls, so it is asked for off the key path
		m.mode, m.target, m.input = modeDiff, spaceID(sp.key), ""
		return m, func() tea.Msg { return sbBase{fleet.BaseRef(dir)} }
	case "edit", "plan":
		a, ok := m.agentOf(m.view().sel)
		if !ok && what == "edit" {
			// e on a space or a pane: your own nvim at that directory
			return m, runKel(dir, nil, "_panel", "editor", dir, title)
		}
		if !ok {
			m.msg, m.msgBad = "select an agent first", true
			return m, nil
		}
		return m, runKel(dir, nil, "_panel", what, dir, title, a.wid)
	}
	return m, runKel(dir, nil, "_panel", what, dir, title)
}

// browse opens the selected server's lowest port — a server pane's own, else
// its space's — and names the rest, which are often a debugger.
func (m sbModel) browse() (sbModel, tea.Cmd) {
	var ports []int
	id := m.view().sel
	if m.doc.Panes != nil {
		for _, p := range *m.doc.Panes {
			if p.PaneID == id && p.Ports != nil {
				ports = append(ports, *p.Ports...)
			}
		}
	}
	if len(ports) == 0 {
		if sp, ok := m.selSpace(id); ok && sp.ports != nil {
			ports = append(ports, *sp.ports...)
		}
	}
	if len(ports) == 0 {
		m.msg, m.msgBad = "no server here", true
		return m, nil
	}
	sort.Ints(ports)
	say := fmt.Sprintf("opened :%d", ports[0])
	if len(ports) > 1 {
		var rest []string
		for _, p := range ports[1:] {
			rest = append(rest, fmt.Sprintf(":%d", p))
		}
		say += " · also " + strings.Join(rest, " ")
	}
	return m, runKelSaying(say, "", nil, "_panel", "browse", fmt.Sprintf("http://localhost:%d", ports[0]))
}

// ---------------------------------------------------------------- update

func (m sbModel) Init() tea.Cmd { return tickAfter(m.visible) }

func (m sbModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height

	case sbTick:
		m.checkVisible()
		if m.visible {
			return m, tea.Batch(m.loadCmd(), tickAfter(true)) // the one tick chain
		}
		return m, tickAfter(false)

	case sbWake:
		was := m.visible
		m.checkVisible()
		if !m.visible {
			return m, nil
		}
		if !was {
			// just shown: draw the last document any sidebar saw, with this
			// window as current, before git has been asked anything
			if d, ok := sbLoadCache(); ok && d.GeneratedAt >= m.doc.GeneratedAt {
				m.doc = d
			}
			m.ui = m.withShared()
			m.markSeen()
			return m, tea.Batch(m.loadCmd(), tickAfter(true))
		}
		return m, m.loadCmd()

	case sbBase:
		// d's default, filled in unless something was typed meanwhile
		if m.mode == modeDiff && m.input == "" {
			m.input = msg.ref
		}
		return m, nil
	case sbLoaded:
		m.loading = false
		m.doc = msg.doc
		m.ui = m.withShared()
		if m.visible {
			if b, err := json.Marshal(m.doc); err == nil {
				fleet.WriteAtomic(sbStatePath("sidebar-cache.json"), b)
			}
			// an agent here finished while you were looking at it: seen
			for _, a := range m.doc.Agents {
				if a.WindowID != nil && *a.WindowID == m.ui.myWindow && a.Unseen {
					m.markSeen()
				}
			}
		}

	case sbDone:
		m.msg, m.msgBad = msg.msg, !msg.ok
		return m, m.loadCmd()

	// focus: the terminal's report when tmux passes it (focus-events on),
	// else #{pane_active} on each tick; a key or a click proves it as well
	case tea.FocusMsg:
		m.ui.focused = true
	case tea.BlurMsg:
		m.ui.focused = false
		m.ui.hover = 0
		// leaving the sidebar ends a prompt, as clicking elsewhere does
		if m.mode == modeFilter {
			m.mode, m.input = modeNormal, ""
		} else {
			m.cancel()
		}

	case tea.MouseMotionMsg:
		m.ui.hover, m.ui.hoverX = msg.Y+1, msg.X

	case tea.MouseClickMsg:
		m.ui.focused = true
		if msg.Button != tea.MouseLeft {
			return m, nil
		}
		if m.help { // the keys list: a key clicked is pressed; elsewhere closes it
			m.help = false
			if msg.Y >= 1 && msg.Y <= len(sbHelp) {
				if k := helpKey(sbHelp[msg.Y-1][0]); k != "" {
					return m.key(tea.KeyPressMsg{Code: rune(k[0]), Text: k})
				}
			}
			return m, nil
		}
		_, refs := renderView(m.view(), m.width, m.height, time.Now().Unix())
		if msg.Y >= 0 && msg.Y < len(refs) && refs[msg.Y].kind == "hint2" && m.cands == "" {
			// a prompt's choices: whichever word was clicked is that key
			return m.key(hintKey(m.view().hint2, msg.X))
		}
		// a click anywhere else ends a prompt, as esc would, and then does
		// what it would have done
		if m.mode != modeNormal && m.mode != modeFilter {
			m.cancel()
		} else if m.mode == modeFilter {
			m.mode, m.input = modeNormal, ""
		}
		if msg.Y >= 0 && msg.Y < len(refs) && refs[msg.Y].kind == "action" {
			// an action is its key, pressed — the half of the line clicked
			k := strings.TrimPrefix(refs[msg.Y].at(msg.X), "act:")
			if k == "enter" {
				return m.key(tea.KeyPressMsg{Code: tea.KeyEnter})
			}
			return m.key(tea.KeyPressMsg{Code: rune(k[0]), Text: k})
		}
		if msg.Y >= 0 && msg.Y < len(refs) && refs[msg.Y].id != "" {
			m.ui.sel = refs[msg.Y].id
			m.saveShared()
			m.activate(m.ui.sel)
			c := m.pending
			m.pending = nil
			return m, c
		}

	case tea.MouseWheelMsg:
		switch msg.Button {
		case tea.MouseWheelDown:
			m.move(1)
		case tea.MouseWheelUp:
			m.move(-1)
		}

	case tea.KeyPressMsg:
		return m.key(msg)
	}
	return m, nil
}

// withShared re-reads the cursor and the hand-opened spaces another window's
// sidebar may have changed.
func (m sbModel) withShared() uiState {
	ui := m.ui
	_, err := os.Stat(sbStatePath("restore-offer"))
	ui.restore = err == nil
	sh := sbLoadShared()
	if sh.Selected != "" {
		ui.sel = sh.Selected
	}
	ui.expanded = sh.Expanded
	return ui
}

func (m sbModel) key(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	k := msg.String()
	m.msg = ""
	m.ui.focused = true

	switch m.mode {
	case modeQuit:
		m.mode = modeNormal
		if k == "y" {
			m.msg = "saving, then quitting…"
			// kel quit refuses while a worktree holds unsaved work, and says
			// what — that lands in the footer like a kill's refusal
			return m, runKel("", nil, "quit")
		}
		return m, nil
	case modeSweep:
		m.mode = modeNormal
		if k == "y" {
			// kel sweep decides, fresh: it removes only what landed, and git
			// itself refuses a worktree with changes in it (invariant 1)
			m.msg = "sweeping…"
			if m.target != "" {
				return m, runKel("", nil, "sweep", m.target)
			}
			return m, runKel("", nil, "sweep")
		}
		return m, nil
	case modeKill:
		m.mode = modeNormal
		if k == "y" {
			if a, ok := m.agentOf(m.target); ok {
				name := a.agent.Name // an unmanaged window has no group of its own
				if a.agent.Managed && a.agent.Group != "" {
					name = a.agent.Group + "/" + name
				}
				m.msg = "killing " + a.agent.Name + "…"
				return m, runKel("", nil, "kill", name)
			}
		}
		return m, nil

	case modeFilter, modeNew, modeNewWorktree, modeRename, modeCommand, modeDiff:
		switch k {
		case "esc":
			if m.mode == modeFilter {
				m.ui.filter = ""
			}
			m.mode, m.input, m.cands = modeNormal, "", ""
		case "enter":
			mode, name := m.mode, strings.TrimSpace(m.input)
			m.mode, m.input, m.cands = modeNormal, "", ""
			if mode == modeFilter || name == "" && mode != modeNew && mode != modeNewWorktree {
				return m, nil
			}
			switch mode {
			case modeCommand:
				// kel's own prompt, answered here: one line in the footer,
				// more in a popup (cmd_prompt)
				m.msg = "kel " + name + "…"
				saveHistory(name)
				return m, runKel("", []string{"KEL_PROMPT_PRINT=1"}, "_prompt", name)
			case modeRename:
				a, ok := m.agentOf(m.target)
				if !ok {
					return m, nil
				}
				// cmd_rename names the window its $TMUX_PANE is in
				return m, runKel("", []string{"TMUX_PANE=" + a.pane}, "rename", name)
			default:
				dir, group := m.spaceFor(m.ui.sel)
				// after " -- ": flags for the agent itself, as `kel new` takes
				// them (`fix -- --model sonnet`)
				name, extra, _ := strings.Cut(name, " -- ")
				nm, typed, err := parseNew(strings.TrimSpace(name), os.Getenv("HOME"))
				if err != "" {
					m.msg, m.msgBad = err, true
					return m, nil
				}
				args := []string{"new"}
				if mode == modeNewWorktree {
					args = append(args, "-w")
				}
				if typed != "" {
					// a directory of your own: its repo names the group,
					// as `kel new` run there would
					dir = typed
				} else if group != "" && group != "misc" {
					args = append(args, "--group", group)
				}
				if nm != "" {
					args = append(args, nm)
				}
				if f := strings.Fields(extra); len(f) > 0 {
					args = append(append(args, "--"), f...)
				}
				return m, runKel(dir, nil, args...)
			case modeDiff:
				sp, ok := m.selSpace(m.target)
				if !ok {
					return m, nil
				}
				d, t := sp.dir(), sp.title()
				return m, runKel(d, nil, "_panel", "diff", d, t, name)
			}
		case "up", "down":
			// ;'s history, newest first: the last twenty commands
			if m.mode == modeCommand {
				h := loadHistory()
				if k == "up" && m.hist < len(h) {
					m.hist++
				} else if k == "down" && m.hist > 0 {
					m.hist--
				}
				m.input = ""
				if m.hist > 0 {
					m.input = h[len(h)-m.hist]
				}
			}
			return m, nil
		case "tab":
			if m.mode == modeDiff {
				if sp, ok := m.selSpace(m.target); ok {
					m.input, m.cands = completeBranch(m.input, sp.dir())
				}
				return m, nil
			}
			if m.mode == modeNew || m.mode == modeNewWorktree {
				m.input, m.cands = completeDir(m.input, os.Getenv("HOME"))
				return m, nil
			}
		case "backspace":
			m.cands = ""
			if r := []rune(m.input); len(r) > 0 {
				m.input = string(r[:len(r)-1])
			}
		default:
			if msg.Text != "" {
				m.input += msg.Text
				m.cands = ""
			}
		}
		if m.mode == modeFilter {
			m.ui.filter = m.input
		}
		return m, nil
	}

	if m.help {
		m.help = false
		return m, nil
	}
	switch k {
	case "j", "down":
		m.move(1)
	case "k", "up":
		m.move(-1)
	case "enter":
		m.activate(m.view().sel)
		c := m.pending
		m.pending = nil
		return m, c
	case "space", "l", "right", "h", "left":
		v := m.view()
		for _, s := range v.spaces {
			if spaceID(s.key) == v.sel {
				open := k == "h" || k == "left" // h closes, l opens, space flips
				if k == "space" {
					open = s.open
				}
				m.toggle(s.key, open)
			}
		}
	case "/":
		m.mode, m.input = modeFilter, m.ui.filter
	case "n":
		m.mode = modeNew
	case "w":
		m.mode = modeNewWorktree
	case "r", "x":
		v := m.view()
		if !strings.HasPrefix(v.sel, "a:") {
			m.msg, m.msgBad = "select an agent first", true
			return m, nil
		}
		m.target = v.sel
		if k == "x" {
			m.mode = modeKill
		} else {
			m.mode, m.input = modeRename, m.entryName(v.sel)
		}
	case "f":
		return m.panel("files")
	case "g":
		return m.panel("git")
	case "d":
		return m.panel("diff")
	case "e":
		return m.panel("edit")
	case "o":
		return m.browse()
	case "p":
		return m.panel("plan")
	case "c":
		return m.panel("claude")
	case "v", "G", "shift+g":
		// every open space's PRs, the selected one's repo first
		dir := os.Getenv("HOME")
		if sp, ok := m.selSpace(m.view().sel); ok {
			dir = sp.dir()
		}
		return m, runKel(dir, nil, "_panel", "dash", dir)
	case "u":
		// the account's usage, not the selection's: from wherever is selected,
		// or home when nothing is
		dir := os.Getenv("HOME")
		if sp, ok := m.selSpace(m.view().sel); ok {
			dir = sp.dir()
		}
		return m, runKel(dir, nil, "_panel", "usage", dir)
	case "t":
		dir, _ := m.spaceFor(m.view().sel)
		return m, runKel(dir, nil, "shell", dir)
	case "q", "Q", "shift+q":
		m.mode = modeQuit
	case "s":
		// on a worktree: that one; anywhere else: everything that landed
		m.mode, m.target = modeSweep, ""
		if sp, ok := m.selSpace(m.view().sel); ok && strings.HasPrefix(m.view().sel, "s:") && sp.parent != "" {
			m.target = sp.key
		}
	case "b", "R", "shift+r":
		if m.ui.restore {
			m.msg = "restoring…"
			return m, runKel("", nil, "restore")
		}
	case "`":
		return m, runKel("", nil, "jump")
	case ";", ":":
		// typed here, like a name — tmux's own prompt opened at the foot of
		// the window, where the author did not see it had (2026-10-16). `;`
		// because `:` needs shift; `:` still works for the hands that try it
		m.mode, m.input, m.hist = modeCommand, "", 0
	case "?":
		m.help = true
	case "esc":
		if m.ui.filter != "" {
			m.ui.filter = ""
			return m, nil
		}
		m.ui.focused = false
		// back to where you were before prefix Space — in this sidebar's own
		// window: with no target, which window tmux calls current differs
		// between versions (macOS CI picked another one)
		args := []string{"last-pane"}
		if m.myPane != "" {
			args = append(args, "-t", m.myPane)
		}
		_ = exec.Command("tmux", args...).Run()
	}
	return m, nil
}

// ---------------------------------------------------------------- drawing

// the full key list, inside the sidebar — not a second surface (invariant 6)
var sbHelp = [][2]string{
	{"#", "move"},
	{"j k", "up, down"},
	{"enter", "jump · fold a space"},
	{"/", "filter by name"},
	{"esc", "back to your pane"},
	{"#", "an agent"},
	{"e", "edit its last file"},
	{"p", "plan and tasks"},
	{"r", "rename"},
	{"x", "kill"},
	{"#", "a space"},
	{"f", "files (yazi)"},
	{"g", "git (lazygit)"},
	{"d", "diff (pick a base)"},
	{"e", "nvim here"},
	{"t", "shell here"},
	{"o", "open in browser"},
	{"c", "claude config"},
	{"s", "sweep if landed"},
	{"#", "kel"},
	{"n", "new agent (tab: dir)"},
	{"n x -- …", "…with claude flags"},
	{"w", "new worktree agent"},
	{"`", "next waiting agent"},
	{"u", "usage (ccusage)"},
	{"v", "PRs, every space"},
	{";", "kel command (↑)"},
	{"q", "quit kel"},
	{"P", ""},
	{"  space", "the sidebar"},
	{"  `", "next waiting agent"},
	{"  k", "kel command"},
	{"  q", "close a popup"},
}

func (m sbModel) render() string {
	if m.help {
		var b strings.Builder
		b.WriteString(dim + truncate("keys", m.width) + reset + "\n")
		pk := m.ui.prefix
		if pk == "" {
			pk = "Ctrl-b"
		}
		for i, h := range sbHelp {
			if h[0] == "#" {
				// a group, named like the sidebar's own sections
				b.WriteString(dim + truncate(h[1], m.width) + reset + "\n")
				continue
			}
			if h[0] == "P" {
				// the prefix keys under one heading that says how they go:
				// "Ctrl-b space" read as one chord (the author, 2026-10-16)
				b.WriteString(dim + truncate("anywhere: "+pk+", let go, then", m.width) + reset + "\n")
				continue
			}
			// what a click runs is bold; the rest is how to move, not a verb
			k, kc := h[0], dim
			lead := " "
			if helpKey(k) != "" {
				kc = bold
				// the same hover bar as every other button: what a click
				// would run shows before the click (line i+1, hover is +1)
				if m.ui.hover == i+2 {
					lead = dim + "▏" + reset
				}
			}
			b.WriteString(lead + kc + truncate(pad(k, 8), m.width-1) + reset + dim + truncate(h[1], m.width-9) + reset + "\n")
		}
		b.WriteString(dim + truncate("click a bold key to run it", m.width) + reset)
		return b.String()
	}
	out, _ := renderView(m.view(), m.width, m.height, time.Now().Unix())
	return out
}

func (m sbModel) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	// all motion, not just with a button down: hover needs the pointer's
	// position when nothing is pressed
	v.MouseMode = tea.MouseModeAllMotion
	v.ReportFocus = true
	return v
}

func runSidebar(once bool) {
	m := sbModel{myPane: os.Getenv("TMUX_PANE")}
	if m.myPane != "" {
		// the window this sidebar sits in is current to it, whatever the
		// fleet document's idea of the client's window — with no client
		// attached there is none
		if out, err := exec.Command("tmux", "display-message", "-p", "-t", m.myPane, "#{window_id}").Output(); err == nil {
			m.ui.myWindow = strings.TrimSpace(string(out))
		}
	}
	m.ui.prefix = prefixWords()
	m.checkVisible()
	// draw once whatever: an empty column helps no one. The first load is the
	// one synchronous one — there is nothing on screen yet to keep responsive
	trace(m.myPane)
	m.doc = fleet.Load(fleet.Options{Panes: true})
	m.ui = m.withShared()
	if m.visible {
		m.markSeen()
	}
	if once {
		if c := os.Getenv("COLUMNS"); c != "" {
			fmt.Sscanf(c, "%d", &m.width)
		}
		if h := os.Getenv("LINES"); h != "" {
			fmt.Sscanf(h, "%d", &m.height)
		}
		fmt.Println(m.render())
		return
	}
	p := tea.NewProgram(m)
	ch := make(chan os.Signal, 4)
	signal.Notify(ch, syscall.SIGUSR1)
	go func() {
		for range ch {
			p.Send(sbWake{})
		}
	}()
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "kel-sidebar: %v\n", err)
		os.Exit(1)
	}
}

// ---------------------------------------------------------------- fixtures

// fixtureFrame renders a saved fleet document — the frames the author
// approves before the sidebar is wired to anything live (spec.md §5e). "now"
// is the document's own generated_at, so ages never drift between runs.
func fixtureFrame(path string, w, h int, focused bool) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var doc fleet.Doc
	if err := json.Unmarshal(b, &doc); err != nil {
		return "", fmt.Errorf("%s: %v", path, err)
	}
	var now int64
	if t, err := time.Parse(time.RFC3339, doc.GeneratedAt); err == nil {
		now = t.Unix()
	}
	out, _ := renderView(buildView(doc, uiState{focused: focused}), w, h, now)
	return out, nil
}

// runFixtures prints every fixture at every width, labelled — what
// test/sidebar-frames.sh runs for a review.
func runFixtures(path string, widths []int, h int) {
	files := []string{path}
	if fi, err := os.Stat(path); err == nil && fi.IsDir() {
		files, _ = filepath.Glob(filepath.Join(path, "*.json"))
		sort.Strings(files)
	}
	for _, f := range files {
		for _, w := range widths {
			for _, focused := range []bool{false, true} {
				out, err := fixtureFrame(f, w, h, focused)
				if err != nil {
					fmt.Fprintf(os.Stderr, "kel-sidebar: %v\n", err)
					os.Exit(1)
				}
				name := strings.TrimSuffix(filepath.Base(f), ".json")
				state := "unfocused"
				if focused {
					state = "focused"
				}
				fmt.Printf("%s── %s · %d cols · %s %s%s\n", dim, name, w, state, strings.Repeat("─", max(0, 12-len(name))), reset)
				fmt.Println(out)
				fmt.Println()
			}
		}
	}
}

// prefixWords is tmux's prefix as a person would say it: C-b is Ctrl-b, M-a
// is Alt-a. The author did not know "prefix" meant Ctrl-b; nothing kel shows
// says "prefix" or "C-b" now.
func prefixWords() string {
	out, err := exec.Command("tmux", "show-options", "-gv", "prefix").Output()
	if err != nil {
		return "Ctrl-b"
	}
	return keyWords(strings.TrimSpace(string(out)))
}

func keyWords(k string) string {
	switch {
	case k == "":
		return "Ctrl-b"
	case strings.HasPrefix(k, "C-"):
		return "Ctrl-" + k[2:]
	case strings.HasPrefix(k, "M-"):
		return "Alt-" + k[2:]
	}
	return k
}

// hintKey maps a click on a prompt's choices line ("y yes · n no") to the key
// of the choice under x: the first character of the segment clicked.
func hintKey(hint string, x int) tea.KeyPressMsg {
	pos := 0
	for _, seg := range strings.Split(hint, " · ") {
		n := len([]rune(seg))
		if x < pos+n+3 {
			k := strings.Fields(seg)[0]
			switch k {
			case "esc":
				return tea.KeyPressMsg{Code: tea.KeyEscape}
			case "enter":
				return tea.KeyPressMsg{Code: tea.KeyEnter}
			case "tab":
				return tea.KeyPressMsg{Code: tea.KeyTab}
			}
			return tea.KeyPressMsg{Code: rune(k[0]), Text: k}
		}
		pos += n + 3
	}
	return tea.KeyPressMsg{}
}

// parseNew reads what n / w typed: nothing, a name, a directory, or a name
// and then a directory — so an agent can start anywhere without a shell to cd
// in first. A word is a directory when it looks like a path (~, /, .). No
// name is kel new's to give. err is what to say instead.
func parseNew(in, home string) (name, dir, err string) {
	f := strings.Fields(in)
	isPath := func(s string) bool {
		return strings.HasPrefix(s, "~") || strings.HasPrefix(s, "/") || strings.HasPrefix(s, ".")
	}
	switch {
	case len(f) == 0:
		return "", "", ""
	case len(f) == 1 && !isPath(f[0]):
		return f[0], "", ""
	case len(f) == 1:
		dir = f[0]
	case len(f) == 2 && isPath(f[1]):
		name, dir = f[0], f[1]
	default:
		return "", "", "type a name, a directory, or both"
	}
	dir = expandHome(dir, home)
	if st, e := os.Stat(dir); e != nil || !st.IsDir() {
		return "", "", "no such directory: " + tildify(dir)
	}
	if abs, e := filepath.Abs(dir); e == nil {
		dir = abs
	}
	return name, dir, ""
}

func expandHome(p, home string) string {
	if p == "~" {
		return home
	}
	if strings.HasPrefix(p, "~/") {
		return filepath.Join(home, p[2:])
	}
	return p
}

// completeDir completes the directory being typed — the last word, when it
// looks like a path — as a shell's tab would: as far as it is unambiguous,
// and the choices, when several remain, for the line above the prompt.
// Directories only; dotfiles only when asked for with a dot.
func completeDir(in, home string) (string, string) {
	i := strings.LastIndexByte(in, ' ')
	head, word := in[:i+1], in[i+1:]
	if word == "" || !(strings.HasPrefix(word, "~") || strings.HasPrefix(word, "/") || strings.HasPrefix(word, ".")) {
		if word == "" {
			return in + "~/", ""
		}
		return in, "a directory starts with ~ / or ."
	}
	if word == "~" {
		return head + "~/", ""
	}
	j := strings.LastIndexByte(word, '/')
	parent, part := word[:j+1], word[j+1:]
	ents, err := os.ReadDir(expandHome(strings.TrimSuffix(parent, "/"), home) + "/")
	if parent == "" || err != nil {
		return in, ""
	}
	var names []string
	for _, e := range ents {
		n := e.Name()
		if !strings.HasPrefix(n, part) || (strings.HasPrefix(n, ".") && !strings.HasPrefix(part, ".")) {
			continue
		}
		if e.IsDir() {
			names = append(names, n)
		} else if e.Type()&os.ModeSymlink != 0 {
			if st, err := os.Stat(filepath.Join(expandHome(strings.TrimSuffix(parent, "/"), home), n)); err == nil && st.IsDir() {
				names = append(names, n)
			}
		}
	}
	switch len(names) {
	case 0:
		return in, "no directory matches"
	case 1:
		return head + parent + names[0] + "/", ""
	}
	return head + parent + commonPrefix(names), strings.Join(names, " ")
}

// commonPrefix is what every name starts with: what tab completes to
func commonPrefix(names []string) string {
	common := names[0]
	for _, n := range names[1:] {
		for !strings.HasPrefix(n, common) {
			common = common[:len(common)-1]
		}
	}
	return common
}

// newWhere says where n / w will start the agent, for the prompt.
func (m sbModel) newWhere() string {
	if _, dir, err := parseNew(m.input, os.Getenv("HOME")); err == "" && dir != "" {
		return filepath.Base(dir) // the folder; the path itself is on the line
	}
	if strings.Contains(m.input, "/") || strings.Contains(m.input, "~") {
		return "…" // a directory being typed: said once it is one
	}
	if sp, ok := m.selSpace(m.ui.sel); ok {
		return sp.label
	}
	return "~"
}

// helpKey is the key a click on a row of the keys list presses, or "" for a
// row that is not one action (j k, click, esc, the popup's q). The author
// asked for every key there to be clickable (2026-10-16); the prefix rows run
// their sidebar twin — `k` is `;` here.
func helpKey(row string) string {
	switch f := strings.Fields(row); {
	case len(f) == 0:
		return ""
	case row == "  k":
		return ";"
	case row == "  `":
		return "`"
	case strings.HasPrefix(row, " "), row == "#":
		return ""
	default:
		switch f[0] {
		case "/", "n", "w", "r", "x", "t", ";", "`", "s", "q", "f", "g", "d", "e", "o", "p", "c", "v", "u":
			return f[0]
		}
	}
	return ""
}

// ;'s history: the last twenty commands, in the state dir — small, local,
// and only what you typed to kel.
func histFile() string { return sbStatePath("prompt-history") }

func loadHistory() []string {
	b, err := os.ReadFile(histFile())
	if err != nil {
		return nil
	}
	var out []string
	for _, l := range strings.Split(string(b), "\n") {
		if l != "" {
			out = append(out, l)
		}
	}
	return out
}

func saveHistory(cmd string) {
	h := loadHistory()
	if len(h) > 0 && h[len(h)-1] == cmd {
		return
	}
	h = append(h, cmd)
	if len(h) > 20 {
		h = h[len(h)-20:]
	}
	fleet.WriteAtomic(histFile(), []byte(strings.Join(h, "\n")+"\n"))
}

// completeBranch completes a branch name from the repo's local and remote
// branches, as completeDir does a directory.
func completeBranch(in, dir string) (string, string) {
	out, err := exec.Command("git", "-C", dir, "for-each-ref", "--format=%(refname:short)", "refs/heads", "refs/remotes").Output()
	if err != nil {
		return in, ""
	}
	var names []string
	for _, n := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if strings.HasPrefix(n, in) && !strings.HasSuffix(n, "/HEAD") && n != "origin" {
			names = append(names, n)
		}
	}
	switch len(names) {
	case 0:
		return in, "no branch matches"
	case 1:
		return names[0], ""
	}
	common := commonPrefix(names)
	if len(names) > 6 {
		names = append(names[:6], "…")
	}
	return common, strings.Join(names, " ")
}

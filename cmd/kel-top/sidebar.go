package main

// kel-top --sidebar: the pane on the left of every kel window (v0.10).
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
	target string // the entry a pending kill or rename acts on
	msg    string // the last command's result, shown until the next key
	msgBad bool
	help   bool
}

// ---------------------------------------------------------------- state dir

func sbStatePath(name string) string {
	d := os.Getenv("XDG_STATE_HOME")
	if d == "" {
		d = filepath.Join(os.Getenv("HOME"), ".local", "state")
	}
	return filepath.Join(d, "kel", name)
}

func sbStateFile() string { return sbStatePath("sidebar.json") }

// atomicWrite: a sidebar in another window may be reading the same file
func atomicWrite(f string, b []byte) {
	tmp := f + ".tmp" + strconv.Itoa(os.Getpid())
	if os.WriteFile(tmp, b, 0o600) == nil {
		_ = os.Rename(tmp, f)
	}
}

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
	atomicWrite(sbStateFile(), b)
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
	atomicWrite(sbStatePath(m.ui.myWindow+".seen"), []byte(strconv.FormatInt(time.Now().Unix(), 10)+"\n"))
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
		for _, l := range strings.Split(ansiSeq.Replace(out), "\n") {
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

var ansiSeq = strings.NewReplacer("\x1b[0m", "", "\x1b[1;32m", "", "\x1b[1;31m", "", "\x1b[1;33m", "", "\x1b[2m", "")

func lastLine(s string) string {
	s = strings.TrimRight(ansiSeq.Replace(s), "\n ")
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
	switch m.mode {
	case modeFilter:
		v.footer, v.footerColour = "/"+m.input+"▏", ""
	case modeNew:
		v.footer, v.footerColour = "new: "+m.input+"▏", ""
	case modeNewWorktree:
		v.footer, v.footerColour = "new worktree: "+m.input+"▏", ""
	case modeRename:
		v.footer, v.footerColour = "rename: "+m.input+"▏", ""
	case modeKill:
		v.footer, v.footerColour = "kill "+m.entryName(m.target)+"? y/n", yellow
	case modeQuit:
		v.footer, v.footerColour = "quit kel? y/n", yellow
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
	return s.dir(), s.label
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
		return s.label + "/" + *s.git.Branch
	}
	return s.label
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
		// the base is a few git calls: worked out off the key path
		return m, func() tea.Msg { return kelDone("", dir, nil, "_panel", "diff", dir, title, fleet.BaseRef(dir)) }
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

	case sbLoaded:
		m.loading = false
		m.doc = msg.doc
		m.ui = m.withShared()
		if m.visible {
			if b, err := json.Marshal(m.doc); err == nil {
				atomicWrite(sbStatePath("sidebar-cache.json"), b)
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

	case tea.MouseClickMsg:
		m.ui.focused = true
		if msg.Button != tea.MouseLeft || m.mode != modeNormal {
			return m, nil
		}
		_, refs := renderView(m.view(), m.width, m.height, time.Now().Unix())
		if msg.Y >= 0 && msg.Y < len(refs) && refs[msg.Y].kind == "action" {
			// an action is its key, pressed
			k := strings.TrimPrefix(refs[msg.Y].id, "act:")
			return m.key(tea.KeyPressMsg{Code: rune(k[0]), Text: k})
		}
		if msg.Y >= 0 && msg.Y < len(refs) && refs[msg.Y].id != "" {
			m.ui.sel = refs[msg.Y].id
			m.saveShared()
			m.activate(m.ui.sel)
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

	case modeFilter, modeNew, modeNewWorktree, modeRename:
		switch k {
		case "esc":
			if m.mode == modeFilter {
				m.ui.filter = ""
			}
			m.mode, m.input = modeNormal, ""
		case "enter":
			mode, name := m.mode, strings.TrimSpace(m.input)
			m.mode, m.input = modeNormal, ""
			if name == "" || mode == modeFilter {
				return m, nil
			}
			switch mode {
			case modeRename:
				a, ok := m.agentOf(m.target)
				if !ok {
					return m, nil
				}
				// cmd_rename names the window its $TMUX_PANE is in
				return m, runKel("", []string{"TMUX_PANE=" + a.pane}, "rename", name)
			default:
				dir, group := m.spaceFor(m.ui.sel)
				args := []string{"new"}
				if mode == modeNewWorktree {
					args = append(args, "-w")
				}
				if group != "" && group != "misc" {
					args = append(args, "--group", group)
				}
				return m, runKel(dir, nil, append(args, name)...)
			}
		case "backspace":
			if r := []rune(m.input); len(r) > 0 {
				m.input = string(r[:len(r)-1])
			}
		default:
			if msg.Text != "" {
				m.input += msg.Text
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
	case "t":
		dir, _ := m.spaceFor(m.view().sel)
		return m, runKel(dir, nil, "shell", dir)
	case "Q", "shift+q":
		m.mode = modeQuit
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
	{"j k", "move"},
	{"enter", "jump · open a space"},
	{"space h l", "fold a space"},
	{"click", "jump · fold"},
	{"/", "filter · esc clears"},
	{"n", "new agent here"},
	{"w", "new worktree agent"},
	{"r", "rename agent"},
	{"x", "kill agent"},
	{"t", "shell window here"},
	{"Q", "quit kel (saved first)"},
	{"f", "files (yazi)"},
	{"g", "git (lazygit)"},
	{"d", "diff against base"},
	{"e", "edit: agent's last file, else nvim"},
	{"o", "open server in browser"},
	{"p", "agent's plan and tasks"},
	{"esc", "back to your pane"},
	{"prefix space", "focus the sidebar"},
	{"prefix `", "next waiting agent"},
}

func (m sbModel) render() string {
	if m.help {
		var b strings.Builder
		b.WriteString(dim + truncate("keys", m.width) + reset + "\n")
		for _, h := range sbHelp {
			b.WriteString(truncate(pad(h[0], 13), m.width) + dim + truncate(h[1], m.width-13) + reset + "\n")
		}
		b.WriteString(dim + truncate("any key closes", m.width) + reset)
		return b.String()
	}
	out, _ := renderView(m.view(), m.width, m.height, time.Now().Unix())
	return out
}

func (m sbModel) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
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
		fmt.Fprintf(os.Stderr, "kel-top --sidebar: %v\n", err)
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
					fmt.Fprintf(os.Stderr, "kel-top: %v\n", err)
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

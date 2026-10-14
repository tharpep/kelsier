package main

// kel-top --sidebar: the pane on the left of every kel window (v0.10 M2).
//
// Deliberately rough — M2 is the plumbing, and this list exists so the layout
// can be tried before M3 builds the real thing: groups, then every pane under
// them, j/k to move, enter to jump. No other keys, and no way to quit: the
// pane is kel's, and closing it is `kel sidebar off`.
//
// One of these runs in every window, so a hidden one must cost nothing. It is
// "visible" when its window is the active window of an attached session —
// one cheap tmux call. Visible: reload every 2s. Hidden: look again every 10s
// and do nothing else. SIGUSR1 (`kel _sidebar wake`, sent on window changes
// and agent state changes) makes it look at once.

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

// sbRow is one line: a group header (pane == nil) or a pane.
type sbRow struct {
	group string
	pane  *fleet.Pane
	state *string // the window's agent state, for agent panes
}

type sbModel struct {
	rows          []sbRow
	sel           string // selected pane id, shared by every sidebar
	width, height int
	myPane        string
	visible       bool
}

func sbStateFile() string {
	d := os.Getenv("XDG_STATE_HOME")
	if d == "" {
		d = filepath.Join(os.Getenv("HOME"), ".local", "state")
	}
	return filepath.Join(d, "kel", "sidebar.json")
}

// the cursor is shared through the state dir, so switching windows shows the
// same sidebar rather than a different one (spec.md §5)
func sbLoadSel() string {
	var v struct {
		Selected string `json:"selected"`
	}
	if b, err := os.ReadFile(sbStateFile()); err == nil {
		_ = json.Unmarshal(b, &v)
	}
	return v.Selected
}

func sbSaveSel(id string) {
	b, _ := json.Marshal(map[string]string{"selected": id})
	f := sbStateFile()
	tmp := f + ".tmp" + strconv.Itoa(os.Getpid())
	if os.WriteFile(tmp, b, 0o600) == nil {
		_ = os.Rename(tmp, f)
	}
}

// visible: this pane's window is its session's active window, and someone is
// attached to that session. Format "1 2" = active, two clients.
func parseVisible(s string) bool {
	f := strings.Fields(s)
	if len(f) != 2 || f[0] != "1" {
		return false
	}
	n, err := strconv.Atoi(f[1])
	return err == nil && n > 0
}

func (m *sbModel) checkVisible() {
	if m.myPane == "" {
		m.visible = true // not in tmux (--once, a test): draw as if seen
		return
	}
	out, err := exec.Command("tmux", "display-message", "-p", "-t", m.myPane,
		"#{window_active} #{session_attached}").Output()
	m.visible = err == nil && parseVisible(string(out))
}

func (m *sbModel) reload() {
	if t := os.Getenv("KEL_SIDEBAR_TRACE"); t != "" {
		if f, err := os.OpenFile(t, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
			fmt.Fprintf(f, "reload %s %d\n", m.myPane, time.Now().Unix())
			f.Close()
		}
	}
	doc := fleet.Load(fleet.Options{Panes: true})
	m.rows = sbRows(doc)
	if s := sbLoadSel(); s != "" {
		m.sel = s
	}
	if m.sel == "" || m.indexOf(m.sel) < 0 {
		for _, r := range m.rows {
			if r.pane != nil {
				m.sel = r.pane.PaneID
				break
			}
		}
	}
}

// sbRows groups the panes: group headers in name order, then each group's
// panes by window index and pane index.
func sbRows(doc fleet.Doc) []sbRow {
	if doc.Panes == nil {
		return nil
	}
	states := map[string]*string{}
	for _, a := range doc.Agents {
		if a.WindowID != nil {
			states[*a.WindowID] = a.State
		}
	}
	byGroup := map[string][]fleet.Pane{}
	for _, p := range *doc.Panes {
		g := p.Group
		if g == "" {
			g = "misc"
		}
		byGroup[g] = append(byGroup[g], p)
	}
	groups := make([]string, 0, len(byGroup))
	for g := range byGroup {
		groups = append(groups, g)
	}
	sort.Strings(groups)
	f := func(p *float64) float64 {
		if p == nil {
			return 0
		}
		return *p
	}
	var rows []sbRow
	for _, g := range groups {
		ps := byGroup[g]
		sort.SliceStable(ps, func(i, j int) bool {
			if f(ps[i].WindowIndex) != f(ps[j].WindowIndex) {
				return f(ps[i].WindowIndex) < f(ps[j].WindowIndex)
			}
			return f(ps[i].PaneIndex) < f(ps[j].PaneIndex)
		})
		rows = append(rows, sbRow{group: g})
		for i := range ps {
			p := ps[i]
			rows = append(rows, sbRow{group: g, pane: &p, state: states[p.WindowID]})
		}
	}
	return rows
}

func (m sbModel) indexOf(id string) int {
	for i, r := range m.rows {
		if r.pane != nil && r.pane.PaneID == id {
			return i
		}
	}
	return -1
}

func (m *sbModel) move(d int) {
	i := m.indexOf(m.sel)
	for j := i + d; j >= 0 && j < len(m.rows); j += d {
		if m.rows[j].pane != nil {
			m.sel = m.rows[j].pane.PaneID
			sbSaveSel(m.sel)
			return
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

func (m sbModel) Init() tea.Cmd { return tickAfter(m.visible) }

func (m sbModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case sbTick:
		m.checkVisible()
		if m.visible {
			m.reload()
		}
		return m, tickAfter(m.visible) // the one tick chain
	case sbWake:
		was := m.visible
		m.checkVisible()
		if m.visible {
			m.reload()
			if !was {
				return m, tickAfter(true) // shorten the wait now we are seen
			}
		}
	case tea.KeyPressMsg:
		switch msg.String() {
		case "j", "down":
			m.move(1)
		case "k", "up":
			m.move(-1)
		case "enter":
			if i := m.indexOf(m.sel); i >= 0 {
				p := m.rows[i].pane
				_ = exec.Command("tmux", "select-window", "-t", p.WindowID,
					";", "select-pane", "-t", p.PaneID).Run()
			}
		}
	}
	return m, nil
}

// glyph and label for one pane row
func sbPaneText(r sbRow) (string, string) {
	p := r.pane
	switch p.Kind {
	case "agent":
		text, colour := stateCell(r.state)
		g := strings.TrimSpace(strings.TrimPrefix(text, strings.Fields(text)[0]))
		if g == "" {
			g = "●"
		}
		return colour + g + reset, p.WindowName
	case "server":
		ps := ""
		if p.Ports != nil {
			for _, n := range *p.Ports {
				ps += fmt.Sprintf(" :%d", n)
			}
		}
		return green + "▪" + reset, p.Cmd + ps
	case "shell":
		return dim + "$" + reset, p.WindowName
	}
	return dim + "▸" + reset, p.Cmd
}

func (m sbModel) render() string {
	w := m.width
	if w <= 0 && m.height <= 0 {
		w = 32 // no size yet (--once without COLUMNS)
	}
	// tmux squeezes the pane proportionally on a resize before `kel _sidebar
	// fit` re-pins it, so any width down to 0 must render — the first version
	// panicked at 1 column and took the pane with it
	lw := w - 5
	if lw < 0 {
		lw = 0
	}
	var b strings.Builder
	n := 0
	for _, r := range m.rows {
		if m.height > 0 && n >= m.height-1 {
			break
		}
		if r.pane == nil {
			b.WriteString(bold + truncate(r.group, w) + reset + "\n")
			n++
			continue
		}
		glyph, label := sbPaneText(r)
		mark := " "
		if r.pane.Current {
			mark = "›"
		}
		line := mark + " " + glyph + " " + truncate(label, lw)
		if r.pane.PaneID == m.sel {
			line = "\x1b[7m" + mark + " " + reset + glyph + "\x1b[7m " + pad(truncate(label, lw), w-4) + reset
		}
		b.WriteString(line + "\n")
		n++
	}
	if len(m.rows) == 0 {
		b.WriteString(dim + "no panes" + reset + "\n")
	}
	if m.height == 0 || n < m.height-1 {
		b.WriteString(dim + truncate("j/k move · enter jump", w) + reset)
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m sbModel) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	return v
}

func runSidebar(once bool) {
	m := sbModel{myPane: os.Getenv("TMUX_PANE")}
	m.checkVisible()
	m.reload() // draw once whatever: an empty column helps no one
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

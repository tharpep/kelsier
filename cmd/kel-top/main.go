// kel-top is the fleet dashboard: one read-only view of every agent, sorted
// so the row that needs you is always at the top.
//
//	kel top            interactive  (j/k scroll, s sort, / filter, q quit)
//	kel top --once     render one frame to stdout and exit
//
// Read-only by design: it never acts on an agent. The board (Ctrl+Space) owns
// actions, and keeping that boundary is why this can refresh under you without
// ever doing something you did not ask for.
package main

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/tharpep/kelsier/internal/fleet"
)

// ---------------------------------------------------------------- ordering

type sortMode int

const (
	sortTriage sortMode = iota
	sortCtx
	sortCost
)

func (s sortMode) String() string {
	return [...]string{"triage", "ctx", "cost"}[s]
}

// triageRank puts the row you must deal with first at the top.
func triageRank(state *string) int {
	if state == nil {
		return 5
	}
	switch *state {
	case "waiting":
		return 0
	case "working":
		return 1
	case "throttled":
		return 2
	case "done":
		return 3
	case "dead":
		return 6
	default: // idle
		return 5
	}
}

func f(p *float64) float64 {
	if p == nil {
		return 0
	}
	return *p
}

func sortAgents(a []fleet.Agent, mode sortMode, now int64) {
	sort.SliceStable(a, func(i, j int) bool {
		switch mode {
		case sortCtx:
			ci, cj := ctxPct(a[i]), ctxPct(a[j])
			if ci != cj {
				return ci > cj
			}
		case sortCost:
			ci, cj := cost(a[i]), cost(a[j])
			if ci != cj {
				return ci > cj
			}
		default:
			ri, rj := triageRank(a[i].State), triageRank(a[j].State)
			if ri != rj {
				return ri < rj
			}
			// within a state, longest-waiting first
			si, sj := f(a[i].StateSince), f(a[j].StateSince)
			if si != sj {
				return si < sj
			}
		}
		if a[i].Group != a[j].Group {
			return a[i].Group < a[j].Group
		}
		return a[i].Name < a[j].Name
	})
}

func ctxPct(a fleet.Agent) float64 {
	if a.Context == nil {
		return -1
	}
	return f(a.Context.Pct)
}

func cost(a fleet.Agent) float64 {
	if a.Context == nil {
		return -1
	}
	return f(a.Context.CostUSD)
}

// ---------------------------------------------------------------- rendering

const (
	reset  = "\x1b[0m"
	dim    = "\x1b[2m"
	bold   = "\x1b[1m"
	yellow = "\x1b[33m"
	red    = "\x1b[31m"
	cyan   = "\x1b[36m"
	green  = "\x1b[32m"
	purple = "\x1b[35m"
)

func stateCell(state *string) (text, colour string) {
	s := "idle"
	if state != nil && *state != "" {
		s = *state
	}
	switch s {
	case "waiting":
		return s + " ?", yellow + bold
	case "working":
		return s + " *", cyan
	case "done":
		return s + " !", green
	case "throttled":
		return s + " ~", purple
	case "dead":
		return s + " x", red + bold
	}
	return s + "  ", dim
}

// humanizeSecs matches bin/kel's helper: 45s, 6m, 1h05m.
func humanizeSecs(s int64) string {
	switch {
	case s < 0:
		return "-"
	case s < 60:
		return fmt.Sprintf("%ds", s)
	case s < 3600:
		return fmt.Sprintf("%dm", s/60)
	default:
		return fmt.Sprintf("%dh%02dm", s/3600, (s%3600)/60)
	}
}

func money(v float64) string { return fmt.Sprintf("$%.2f", v) }

func truncate(s string, n int) string {
	if n <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n == 1 {
		return "…"
	}
	return string(r[:n-1]) + "…"
}

func pad(s string, n int) string {
	r := []rune(s)
	if len(r) >= n {
		return string(r[:n])
	}
	return s + strings.Repeat(" ", n-len(r))
}

type model struct {
	agents   []fleet.Agent
	last     map[string]string
	mode     sortMode
	cursor   int
	top      int
	filter   string
	filtOn   bool
	width    int
	height   int
	ctxWarn  float64
	quitting bool
}

type tickMsg time.Time

func refresh() tea.Cmd {
	return tea.Tick(2*time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m model) Init() tea.Cmd { return refresh() }

func (m *model) reload() {
	doc := fleet.Load(true)
	m.agents = doc.Agents
	sortAgents(m.agents, m.mode, time.Now().Unix())
	ids := make([]string, 0, len(m.agents))
	for _, a := range m.agents {
		if a.WindowID != nil {
			ids = append(ids, *a.WindowID)
		}
	}
	m.last = fleet.LastOutput(ids)
}

func (m model) visible() []fleet.Agent {
	if m.filter == "" {
		return m.agents
	}
	q := strings.ToLower(m.filter)
	out := []fleet.Agent{}
	for _, a := range m.agents {
		if strings.Contains(strings.ToLower(a.Group+"/"+a.Name), q) {
			out = append(out, a)
		}
	}
	return out
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case tickMsg:
		m.reload()
		return m, refresh()

	case tea.KeyPressMsg:
		k := msg.String()
		if m.filtOn {
			switch k {
			case "enter", "esc":
				m.filtOn = false
			case "backspace":
				if len(m.filter) > 0 {
					m.filter = m.filter[:len(m.filter)-1]
				}
			case "ctrl+c":
				m.quitting = true
				return m, tea.Quit
			default:
				if msg.Text != "" {
					m.filter += msg.Text
				}
			}
			m.cursor = 0
			m.top = 0
			return m, nil
		}
		n := len(m.visible())
		switch k {
		case "q", "esc", "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		case "j", "down":
			if m.cursor < n-1 {
				m.cursor++
			}
		case "k", "up":
			if m.cursor > 0 {
				m.cursor--
			}
		case "g", "home":
			m.cursor = 0
		case "G", "end":
			m.cursor = n - 1
		case "s":
			m.mode = (m.mode + 1) % 3
			sortAgents(m.agents, m.mode, time.Now().Unix())
			m.cursor = 0
		case "/":
			m.filtOn = true
			m.filter = ""
		case "r":
			m.reload()
		}
	}
	return m, nil
}

func (m model) render() string {
	w := m.width
	if w <= 0 {
		w = 100
	}
	rows := m.visible()
	now := time.Now().Unix()

	// fixed columns, then LAST OUTPUT gets whatever is left
	const (
		wGroup = 10
		wAgent = 14
		wState = 11
		wFor   = 6
		wCtx   = 7
		wCost  = 8
	)
	fixed := 2 + wGroup + 1 + wAgent + 1 + wState + 1 + wFor + 1 + wCtx + 1 + wCost + 1
	wLast := w - fixed
	showLast := wLast >= 12
	showCost := w >= 66
	showCtx := w >= 58

	var b strings.Builder
	hdr := "  " + pad("GROUP", wGroup) + " " + pad("AGENT", wAgent) + " " +
		pad("STATE", wState) + " " + pad("FOR", wFor)
	if showCtx {
		hdr += " " + pad("CTX", wCtx)
	}
	if showCost {
		hdr += " " + pad("$", wCost)
	}
	if showLast {
		hdr += " " + "LAST OUTPUT"
	}
	b.WriteString(dim + strings.TrimRight(truncate(hdr, w), " ") + reset + "\n")

	// window the rows to the space we have
	body := m.height - 3
	if body < 1 {
		body = 1
	}
	if m.cursor < m.top {
		m.top = m.cursor
	}
	if m.cursor >= m.top+body {
		m.top = m.cursor - body + 1
	}
	end := m.top + body
	if end > len(rows) {
		end = len(rows)
	}

	if len(rows) == 0 {
		b.WriteString(dim + "  no agents" + reset + "\n")
	}
	for i := m.top; i < end; i++ {
		a := rows[i]
		st, col := stateCell(a.State)
		marker := "  "
		if i == m.cursor {
			marker = cyan + "› " + reset
		}
		forCol := "-"
		if a.StateSince != nil {
			forCol = humanizeSecs(now - int64(*a.StateSince))
		}
		line := marker + pad(a.Group, wGroup) + " " + pad(a.Name, wAgent) + " " +
			col + pad(st, wState) + reset + " " + pad(forCol, wFor)
		if showCtx {
			c := "-"
			cc := ""
			if a.Context != nil && a.Context.Pct != nil {
				p := *a.Context.Pct
				c = fmt.Sprintf("%d%%", int(p))
				if a.Compactions != nil && *a.Compactions > 0 {
					c += fmt.Sprintf("×%d", int(*a.Compactions))
				}
				switch {
				case p >= 90:
					cc = red + bold
				case p >= m.ctxWarn:
					cc = yellow
				}
			}
			if cc != "" {
				line += " " + cc + pad(c, wCtx) + reset
			} else {
				line += " " + pad(c, wCtx)
			}
		}
		if showCost {
			c := "-"
			if a.Context != nil && a.Context.CostUSD != nil && *a.Context.CostUSD > 0 {
				c = money(*a.Context.CostUSD)
			}
			line += " " + pad(c, wCost)
		}
		if showLast {
			out := ""
			if a.WindowID != nil {
				out = m.last[*a.WindowID]
			}
			line += " " + dim + truncate(out, wLast) + reset
		}
		b.WriteString(strings.TrimRight(line, " ") + "\n")
	}

	// footer
	foot := fmt.Sprintf("j/k scroll   s sort: %s   / filter   q quit", m.mode)
	if m.filtOn {
		foot = "filter: " + m.filter + "▌   enter/esc done"
	} else if m.filter != "" {
		foot = fmt.Sprintf("filter %q (%d/%d)   s sort: %s   / refilter   q quit",
			m.filter, len(rows), len(m.agents), m.mode)
	}
	b.WriteString("\n" + dim + truncate("  "+foot, w) + reset)
	return b.String()
}

func (m model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	return v
}

func main() {
	once := false
	for _, a := range os.Args[1:] {
		switch a {
		case "--once":
			once = true
		case "-h", "--help":
			fmt.Fprintln(os.Stderr, "usage: kel-top [--once]")
			return
		default:
			fmt.Fprintf(os.Stderr, "kel-top: unknown flag %s\n", a)
			os.Exit(2)
		}
	}

	m := model{mode: sortTriage, ctxWarn: 70, width: 100, height: 24}
	if v := os.Getenv("KEL_CTX_WARN"); v != "" {
		var n float64
		if _, err := fmt.Sscanf(v, "%f", &n); err == nil {
			m.ctxWarn = n
		}
	}
	m.reload()

	if once || os.Getenv("KEL_TOP_ONCE") != "" {
		if w := os.Getenv("COLUMNS"); w != "" {
			fmt.Sscanf(w, "%d", &m.width)
		}
		m.height = len(m.agents) + 4
		fmt.Println(m.render())
		return
	}

	if _, err := tea.NewProgram(m).Run(); err != nil {
		fmt.Fprintf(os.Stderr, "kel-top: %v\n", err)
		os.Exit(1)
	}
}

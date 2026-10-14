// kel-top is the fleet dashboard: one read-only view of every agent, sorted
// so the row that needs you is always at the top.
//
//	kel top                 interactive  (j/k scroll, s sort, / filter, ? help, q quit)
//	kel top --once          render one frame to stdout and exit
//	kel-top --sidebar       the pane on the left of every kel window (sidebar.go)
//	kel top --dump-help-keys  print the navigation keymap's key set — see keys.go
//
// Read-only for now: it never acts on an agent. v0.10 deleted the fzf board
// that used to own actions; this is the seed of the sidebar that replaces it
// (docs/rollout.md § v0.10), and acting arrives with that.
package main

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
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

// landCell turns the land code into the verb it implies. Blank for clean —
// a row with nothing to do should be quiet, not say "clean" in every line.
func landCell(l *fleet.Land) (text, colour string) {
	if l == nil {
		return "", ""
	}
	n := 0
	if l.N != nil {
		n = int(*l.N)
	}
	switch l.Code {
	case "dirty":
		return fmt.Sprintf("dirty %d", n), yellow
	case "unpushed":
		return fmt.Sprintf("unpushed %d", n), yellow
	case "behind":
		return fmt.Sprintf("behind %d", n), yellow
	case "merged":
		return "merged", green + bold
	case "review":
		return "in review", cyan
	case "no_pr":
		return "no PR", purple
	case "unknown":
		return "?", dim
	}
	return "", ""
}

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

// tidy strips trailing blanks from a rendered row. A plain TrimRight is not
// enough: a padded coloured cell puts its spaces BEFORE the reset sequence, so
// they hide at the end of the line behind an escape.
func tidy(s string) string {
	for {
		if strings.HasSuffix(s, reset) {
			s = strings.TrimSuffix(s, reset)
			continue
		}
		if strings.HasSuffix(s, " ") {
			s = strings.TrimRight(s, " ")
			continue
		}
		break
	}
	return s + reset
}

func pad(s string, n int) string {
	if n <= 0 {
		return "" // a squeezed pane asks for negative widths; slicing them panics
	}
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
	keys     keys
	help     help.Model
}

type tickMsg time.Time

func refresh() tea.Cmd {
	return tea.Tick(2*time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m model) Init() tea.Cmd { return refresh() }

func (m *model) reload() {
	doc := fleet.Load(fleet.Options{Dirty: true, Land: true})
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
		if m.filtOn {
			switch {
			case key.Matches(msg, m.keys.FilterDone):
				m.filtOn = false
			case key.Matches(msg, m.keys.FilterBack):
				if len(m.filter) > 0 {
					m.filter = m.filter[:len(m.filter)-1]
				}
			case key.Matches(msg, m.keys.FilterQuit):
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
		switch {
		case key.Matches(msg, m.keys.Quit):
			m.quitting = true
			return m, tea.Quit
		case key.Matches(msg, m.keys.Down):
			if m.cursor < n-1 {
				m.cursor++
			}
		case key.Matches(msg, m.keys.Up):
			if m.cursor > 0 {
				m.cursor--
			}
		case key.Matches(msg, m.keys.Top):
			m.cursor = 0
		case key.Matches(msg, m.keys.Bottom):
			m.cursor = n - 1
		case key.Matches(msg, m.keys.Sort):
			m.mode = (m.mode + 1) % 3
			sortAgents(m.agents, m.mode, time.Now().Unix())
			m.cursor = 0
		case key.Matches(msg, m.keys.Filter):
			m.filtOn = true
			m.filter = ""
		case key.Matches(msg, m.keys.Reload):
			m.reload()
		case key.Matches(msg, m.keys.ToggleHelp):
			m.help.ShowAll = !m.help.ShowAll
		case key.Matches(msg, m.keys.Jump):
			// #3/#27: an explicit no-op, not a keypress with no effect and
			// no case to find it in. The real jump arrives with the
			// sidebar (docs/rollout.md § v0.10), reusing this binding.
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

	// fixed columns, then LAST OUTPUT gets whatever is left. Column order
	// is GROUP AGENT STATE FOR LAND CTX $: LAND moved
	// ahead of CTX and $ because it names an action, where a dollar figure
	// names something already spent.
	const (
		wGroup = 10
		wAgent = 14
		wState = 11
		wFor   = 6
		wLand  = 11
		wCtx   = 7
		wCost  = 8
	)
	// Show/hide gates are derived from the column widths, not hand-copied
	// numbers -- that hand-copying is exactly how the previous version drifted
	// (showLand:=w>=74 against a budget that actually needed 76: two columns'
	// worth of space that did not exist got drawn into). Each gate is "the
	// fixed width needed once everything up to and including this column is
	// shown", so turning a column on can never cost more than the terminal
	// actually has.
	base := 2 + wGroup + 1 + wAgent + 1 + wState + 1 + wFor + 1
	gateLand := base + wLand + 1
	gateCtx := gateLand + wCtx + 1
	gateCost := gateCtx + wCost + 1
	showLand := w >= gateLand
	showCtx := w >= gateCtx
	showCost := w >= gateCost
	fixed := base
	if showLand {
		fixed += wLand + 1
	}
	if showCtx {
		fixed += wCtx + 1
	}
	if showCost {
		fixed += wCost + 1
	}
	wLast := w - fixed
	showLast := wLast >= 12

	var b strings.Builder
	hdr := "  " + pad("GROUP", wGroup) + " " + pad("AGENT", wAgent) + " " +
		pad("STATE", wState) + " " + pad("FOR", wFor)
	if showLand {
		hdr += " " + pad("LAND", wLand)
	}
	if showCtx {
		hdr += " " + pad("CTX", wCtx)
	}
	if showCost {
		hdr += " " + pad("$", wCost)
	}
	if showLast {
		hdr += " " + "LAST OUTPUT"
	}

	// #26: header and footer used to print unconditionally while only the
	// body was clamped to a minimum of one row, so a 1- or 2-row terminal
	// overflowed it. Drop the footer first, then the header, before the
	// body is ever asked to shrink below one row.
	showHeader, showFooter := true, true
	body := m.height - 3 // header + blank line + footer
	if body < 1 {
		showFooter = false
		body = m.height - 1 // header only
		if body < 1 {
			showHeader = false
			body = m.height
			if body < 1 {
				body = 1
			}
		}
	}

	if showHeader {
		b.WriteString(dim + strings.TrimRight(truncate(hdr, w), " ") + reset + "\n")
	}

	// window the rows to the space we have
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
		if showLand {
			lt, lc := landCell(a.Land)
			if lc != "" {
				line += " " + lc + pad(lt, wLand) + reset
			} else {
				line += " " + pad(lt, wLand)
			}
		}
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
		b.WriteString(tidy(line) + "\n")
	}

	if !showFooter {
		return strings.TrimSuffix(b.String(), "\n")
	}

	// footer: a plain label plus help.Model's own rendering of m.keys
	// (keys.go) -- the same bindings Update() matches against, so a key
	// cannot appear here without being handled, or be handled without
	// appearing here. `?` toggles the short/full view.
	label := fmt.Sprintf("sort: %s", m.mode)
	if m.filtOn {
		label = "filter: " + m.filter + "▌"
	} else if m.filter != "" {
		label = fmt.Sprintf("filter %q (%d/%d)   sort: %s", m.filter, len(rows), len(m.agents), m.mode)
	}
	const indent, sep = "  ", "   "
	avail := w - len([]rune(indent)) - len([]rune(label)) - len([]rune(sep))
	if avail < 1 {
		avail = 1
	}
	m.help.SetWidth(avail)
	var helpTxt string
	if m.filtOn {
		helpTxt = m.help.View(filterHelp(m.keys))
	} else {
		helpTxt = m.help.View(normalHelp(m.keys))
	}
	// help.Model's own SetWidth truncation is best-effort: its ellipsis check
	// only fires while the running total is still comfortably under budget,
	// so a narrow `avail` (found by rendering the width sweep, not by reading
	// this) can walk straight past it and print everything anyway. Styles are
	// zeroed in main() so helpTxt carries no ANSI, which is what makes a
	// plain rune truncate a safe backstop here.
	helpTxt = truncate(helpTxt, avail)
	foot := strings.TrimRight(indent+dim+label+reset+sep+helpTxt, " ")
	b.WriteString("\n" + foot)
	return b.String()
}

func (m model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	return v
}

func main() {
	once, sidebar := false, false
	for _, a := range os.Args[1:] {
		switch a {
		case "--once":
			once = true
		case "--sidebar":
			sidebar = true
		case "--dump-help-keys":
			// the guard's data half: test/kel-test.sh diffs this against a
			// hand-kept expected set, so a key cannot go undocumented
			fmt.Print(dumpHelpKeys(newKeys()))
			return
		case "-h", "--help":
			fmt.Fprintln(os.Stderr, "usage: kel-top [--once] [--sidebar] [--dump-help-keys]")
			return
		default:
			fmt.Fprintf(os.Stderr, "kel-top: unknown flag %s\n", a)
			os.Exit(2)
		}
	}

	if sidebar {
		runSidebar(once)
		return
	}

	m := model{
		mode: sortTriage, ctxWarn: 70, width: 100, height: 24,
		keys: newKeys(),
		help: help.New(),
	}
	// Plain, not styled: render() applies its own `dim` to the whole footer
	// and, more importantly, needs the truncate() backstop below to be safe
	// against ANSI, which only holds if help.Model's own output carries none.
	m.help.Styles = help.Styles{}
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
		// LINES lets the test suite drive the 1-4 row height sweep (#26)
		// without a real terminal — COLUMNS above is the same idea, already
		// relied on by the width sweep.
		if h := os.Getenv("LINES"); h != "" {
			fmt.Sscanf(h, "%d", &m.height)
		}
		fmt.Println(m.render())
		return
	}

	if _, err := tea.NewProgram(m).Run(); err != nil {
		fmt.Fprintf(os.Stderr, "kel-top: %v\n", err)
		os.Exit(1)
	}
}

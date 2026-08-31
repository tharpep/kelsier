// Package fleet computes the kelsier fleet document: one answer to "what is
// the fleet doing", assembled from tmux and the on-disk state directory.
//
// This is a port of _fleet_bash in bin/kel, and the bash version is the
// oracle: the two must emit the same document for the same state, and the
// regression suite fails on a difference. See docs/rollout.md § v0.6.
package fleet

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Context mirrors <window-id>.ctx, written by `kel statusline`.
type Context struct {
	Pct      *float64 `json:"pct"`
	CostUSD  *float64 `json:"cost_usd"`
	InTokens *float64 `json:"in_tokens"`
	Size     *float64 `json:"size"`
	Rate5h   *float64 `json:"rate_5h"`
	At       *float64 `json:"at"`
	Model    *string  `json:"model"`
}

// Agent is one row of the fleet: a live tmux window, or a record whose window
// is gone (window_id null, state "dead").
type Agent struct {
	Group         string   `json:"group"`
	Name          string   `json:"name"`
	WindowID      *string  `json:"window_id"`
	Index         *float64 `json:"index"`
	Current       bool     `json:"current"`
	Managed       bool     `json:"managed"`
	State         *string  `json:"state"`
	StateSince    *float64 `json:"state_since"`
	Note          *string  `json:"note"`
	Isolation     *string  `json:"isolation"`
	Branch        *string  `json:"branch"`
	Repo          *string  `json:"repo"`
	Cwd           *string  `json:"cwd"`
	AgentCmd      *string  `json:"agent"`
	ClaudeSession *string  `json:"claude_session"`
	Compactions   *float64 `json:"compactions"`
	Dirty         *float64 `json:"dirty"`
	Land          *Land    `json:"land"`
	Panes         []string `json:"panes"`
	Activity      *float64 `json:"activity"`
	Context       *Context `json:"context"`
}

type Current struct {
	Group    *string `json:"group"`
	WindowID *string `json:"window_id"`
}

type Doc struct {
	GeneratedAt string  `json:"generated_at"`
	Current     Current `json:"current"`
	Agents      []Agent `json:"agents"`
}

// record is the on-disk agent record, sessions/<group>/<name>.json.
type record struct {
	Name          string   `json:"name"`
	WindowID      string   `json:"window_id"`
	Repo          *string  `json:"repo"`
	Cwd           *string  `json:"cwd"`
	Isolation     *string  `json:"isolation"`
	Branch        *string  `json:"branch"`
	Agent         *string  `json:"agent"`
	Group         *string  `json:"group"`
	ClaudeSession *string  `json:"claude_session"`
	Compactions   *float64 `json:"compactions"`
}

var shells = map[string]bool{
	"bash": true, "zsh": true, "fish": true, "sh": true, "dash": true, "tmux": true,
}

func sp(s string) *string { return &s }

// numPtr parses a numeric field the way jq's `tonumber? // null` does:
// anything unparseable becomes null rather than an error.
func numPtr(s string) *float64 {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return nil
	}
	return &f
}

func emptyToNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func tmuxOut(args ...string) string {
	out, err := exec.Command("tmux", args...).Output()
	if err != nil {
		return ""
	}
	return string(out)
}

func lines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if l != "" {
			out = append(out, l)
		}
	}
	return out
}

func stateDir() string {
	if d := os.Getenv("XDG_STATE_HOME"); d != "" {
		return filepath.Join(d, "kel")
	}
	return filepath.Join(os.Getenv("HOME"), ".local", "state", "kel")
}

func prefix() string {
	if p := os.Getenv("KEL_SESSION"); p != "" {
		return p
	}
	return "kel"
}

// groupOf maps a tmux session name to a kel group, or "" if it isn't ours.
func groupOf(session, pfx string) (string, bool) {
	switch {
	case session == pfx:
		return pfx, true
	case strings.HasPrefix(session, pfx+"/"):
		return session[len(pfx)+1:], true
	}
	return "", false
}

// Options selects the expensive extras. The status bar asks for none of them;
// `kel top` asks for Dirty+Land; only PR may touch the network.
type Options struct {
	Dirty bool
	Land  bool
	PR    bool // may refresh the PR cache — never on a refresh path
}

// Load assembles the fleet document.
func Load(o Options) Doc {
	withDirty := o.Dirty || o.Land || o.PR
	sd := stateDir()
	sessionsDir := filepath.Join(sd, "sessions")
	pfx := prefix()

	// The three tmux queries are independent; run them at once. This is the
	// concrete win over bash, which can only do them in sequence.
	var winsRaw, panesRaw, curRaw string
	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		winsRaw = tmuxOut("list-windows", "-a", "-F",
			"#{session_name}\t#{window_index}\t#{window_id}\t#{window_name}\t#{window_activity}")
	}()
	go func() {
		defer wg.Done()
		panesRaw = tmuxOut("list-panes", "-a", "-F", "#{window_id}\t#{pane_current_command}")
	}()
	go func() {
		defer wg.Done()
		curRaw = tmuxOut("display-message", "-p", "#{client_session} #{window_id}")
	}()
	wg.Wait()

	curSess, curWid := "", ""
	if f := strings.Fields(strings.TrimSpace(curRaw)); len(f) == 2 {
		curSess, curWid = f[0], f[1]
	}

	paneMap := map[string][]string{}
	for _, l := range lines(panesRaw) {
		if p := strings.SplitN(l, "\t", 2); len(p) == 2 {
			paneMap[p[0]] = append(paneMap[p[0]], p[1])
		}
	}

	type stateEntry struct {
		state string
		since *float64
		note  *string
	}
	stateMap := map[string]stateEntry{}
	for _, f := range globSorted(filepath.Join(sd, "*.state")) {
		wid := strings.TrimSuffix(filepath.Base(f), ".state")
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		// "<state> <epoch> <note...>" — note may contain spaces
		line := strings.SplitN(strings.TrimRight(string(b), "\n"), "\n", 2)[0]
		parts := strings.SplitN(line, " ", 3)
		e := stateEntry{}
		if len(parts) > 0 {
			e.state = parts[0]
		}
		if len(parts) > 1 {
			e.since = numPtr(parts[1])
		}
		if len(parts) > 2 {
			e.note = emptyToNil(strings.TrimSpace(parts[2]))
		}
		stateMap[wid] = e
	}

	ctxMap := map[string]*Context{}
	for _, f := range globSorted(filepath.Join(sd, "*.ctx")) {
		wid := strings.TrimSuffix(filepath.Base(f), ".ctx")
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		c := strings.Split(strings.SplitN(strings.TrimRight(string(b), "\n"), "\n", 2)[0], "\t")
		at := func(i int) string {
			if i < len(c) {
				return c[i]
			}
			return ""
		}
		rate := numPtr(at(4))
		if rate != nil && *rate == -1 { // -1 is "unknown", not a value
			rate = nil
		}
		ctxMap[wid] = &Context{
			Pct: numPtr(at(0)), CostUSD: numPtr(at(1)), InTokens: numPtr(at(2)),
			Size: numPtr(at(3)), Rate5h: rate, At: numPtr(at(5)),
			Model: emptyToNil(at(6)),
		}
	}

	var records []record
	for _, f := range globSorted(filepath.Join(sessionsDir, "*", "*.json")) {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var r record
		if json.Unmarshal(b, &r) == nil {
			records = append(records, r)
		}
	}
	recGroup := func(r record) string {
		if r.Group != nil && *r.Group != "" {
			return *r.Group
		}
		return "misc"
	}
	recMap := map[string]record{}
	for _, r := range records {
		recMap[recGroup(r)+" "+r.Name] = r
	}

	lands := map[string]*Land{}
	if o.Land || o.PR {
		lands = landMap(records, o.PR)
	}

	// dirty is a property of the directory, not the window
	dirtyMap := map[string]*float64{}
	if withDirty {
		seen := map[string]bool{}
		for _, r := range records {
			if r.Cwd == nil || *r.Cwd == "" || seen[*r.Cwd] {
				continue
			}
			seen[*r.Cwd] = true
			if n, ok := gitDirty(*r.Cwd); ok {
				dirtyMap[*r.Cwd] = n
			}
		}
	}

	type win struct{ sess, idx, wid, name, activity string }
	var wins []win
	for _, l := range lines(winsRaw) {
		p := strings.Split(l, "\t")
		if len(p) == 5 {
			wins = append(wins, win{p[0], p[1], p[2], p[3], p[4]})
		}
	}

	agents := []Agent{}
	for _, w := range wins {
		g, ok := groupOf(w.sess, pfx)
		if !ok {
			continue
		}
		panes := paneMap[w.wid]
		if panes == nil {
			panes = []string{}
		}
		st, hasState := stateMap[w.wid]

		// effective state: a hook said working/waiting/throttled, but if the
		// window holds nothing but a shell the process is gone
		var eff *string
		if hasState && st.state != "" {
			eff = sp(st.state)
			if st.state == "working" || st.state == "waiting" || st.state == "throttled" {
				alive := false
				for _, c := range panes {
					if !shells[c] {
						alive = true
						break
					}
				}
				if !alive {
					eff = sp("dead")
				}
			}
		}

		rec, managed := recMap[g+" "+w.name]
		a := Agent{
			Group: g, Name: w.name, WindowID: sp(w.wid), Index: numPtr(w.idx),
			Current: w.sess == curSess && w.wid == curWid, Managed: managed,
			State: eff, Panes: panes, Activity: numPtr(w.activity),
			Context: ctxMap[w.wid],
		}
		if hasState {
			a.StateSince, a.Note = st.since, st.note
		}
		if managed {
			a.Isolation, a.Branch, a.Repo, a.Cwd = rec.Isolation, rec.Branch, rec.Repo, rec.Cwd
			a.AgentCmd, a.ClaudeSession, a.Compactions = rec.Agent, rec.ClaudeSession, rec.Compactions
			if rec.Cwd != nil {
				a.Dirty = dirtyMap[*rec.Cwd]
				a.Land = lands[*rec.Cwd]
			}
		}
		agents = append(agents, a)
	}

	// records with no live window. Same liveness rule as meta_is_live: the
	// window must match BOTH the recorded id and the recorded name.
	for _, r := range records {
		live := false
		for _, w := range wins {
			if w.wid == r.WindowID && w.name == r.Name {
				live = true
				break
			}
		}
		if live {
			continue
		}
		a := Agent{
			Group: recGroup(r), Name: r.Name, WindowID: nil, Index: nil,
			Current: false, Managed: true, State: sp("dead"),
			Isolation: r.Isolation, Branch: r.Branch, Repo: r.Repo, Cwd: r.Cwd,
			AgentCmd: r.Agent, ClaudeSession: r.ClaudeSession, Compactions: r.Compactions,
			Panes: []string{},
		}
		if r.Cwd != nil {
			a.Dirty = dirtyMap[*r.Cwd]
			a.Land = lands[*r.Cwd]
		}
		agents = append(agents, a)
	}

	cur := Current{}
	if curSess != "" {
		if g, ok := groupOf(curSess, pfx); ok {
			cur.Group = sp(g)
		}
	}
	if curWid != "" {
		cur.WindowID = sp(curWid)
	}

	return Doc{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Current:     cur,
		Agents:      agents,
	}
}

func globSorted(pattern string) []string {
	m, err := filepath.Glob(pattern)
	if err != nil {
		return nil
	}
	return m // filepath.Glob already returns sorted results
}

func gitDirty(dir string) (*float64, bool) {
	if err := exec.Command("git", "-C", dir, "rev-parse", "--git-dir").Run(); err != nil {
		return nil, false
	}
	out, err := exec.Command("git", "-C", dir, "status", "--porcelain").Output()
	if err != nil {
		return nil, false
	}
	n := float64(len(lines(string(out))))
	return &n, true
}

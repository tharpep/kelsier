// Package fleet computes the kelsier fleet document: one answer to "what is
// the fleet doing", assembled from tmux and the on-disk state directory.
//
// It began as a port of bin/kel's bash reader, checked against it by a
// differential test.  v0.10 deleted the bash reader: this is now the only
// implementation, and the document it emits is the contract every surface
// (kel ls, the bar, kel top, the sidebar) reads.
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

// groupOf says whether a tmux session is kel's, and for a legacy v0.9
// per-repo session (`kel/<group>`) which group it named.  Since v0.10 every
// kel window lives in the one session named exactly pfx, which carries no
// group — that comes from the window (see windowGroup).
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
			"#{session_name}\t#{window_index}\t#{window_id}\t#{window_name}\t#{window_activity}\t#{@kel-group}")
	}()
	go func() {
		defer wg.Done()
		panesRaw = tmuxOut("list-panes", "-a", "-F", "#{window_id}\t#{pane_current_command}\t#{pane_current_path}")
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
	firstPath := map[string]string{} // window id -> its first pane's cwd
	for _, l := range lines(panesRaw) {
		if p := strings.SplitN(l, "\t", 3); len(p) >= 2 {
			paneMap[p[0]] = append(paneMap[p[0]], p[1])
			if _, seen := firstPath[p[0]]; !seen && len(p) == 3 {
				firstPath[p[0]] = p[2]
			}
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
	var unreadable []string // sessions/<group>/<name>.json that failed to parse — #25
	for _, f := range globSorted(filepath.Join(sessionsDir, "*", "*.json")) {
		b, err := os.ReadFile(f)
		if err != nil {
			unreadable = append(unreadable, f)
			continue
		}
		var r record
		if json.Unmarshal(b, &r) == nil {
			records = append(records, r)
		} else {
			unreadable = append(unreadable, f)
		}
	}
	recGroup := func(r record) string {
		if r.Group != nil && *r.Group != "" {
			return *r.Group
		}
		return "misc"
	}
	// Keyed by window id: since v0.10 the session no longer names the group,
	// so a record is matched to its window the way liveness always was — the
	// same id AND the same name (see the dead-record loop below).
	recByWid := map[string]record{}
	for _, r := range records {
		if r.WindowID != "" {
			recByWid[r.WindowID] = r
		}
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

	type win struct{ sess, idx, wid, name, activity, kelGroup string }
	var wins []win
	for _, l := range lines(winsRaw) {
		p := strings.Split(l, "\t")
		if len(p) == 6 {
			wins = append(wins, win{p[0], p[1], p[2], p[3], p[4], p[5]})
		}
	}

	agents := []Agent{}
	curGroup := ""
	for _, w := range wins {
		legacy, ok := groupOf(w.sess, pfx)
		if !ok {
			continue
		}
		rec, managed := recByWid[w.wid]
		if managed && rec.Name != w.name {
			managed = false
		}
		// where a window's group comes from, most authoritative first: its
		// record; the @kel-group window option kel new sets; a legacy v0.9
		// kel/<group> session; the repo its first pane stands in
		g := ""
		switch {
		case managed:
			g = recGroup(rec)
		case w.kelGroup != "":
			g = w.kelGroup
		case w.sess != pfx:
			g = legacy
		default:
			g = repoGroup(firstPath[w.wid])
		}
		if w.wid == curWid {
			curGroup = g
		}
		panes := paneMap[w.wid]
		if panes == nil {
			panes = []string{}
		}
		st, hasState := stateMap[w.wid]

		// effective state: a hook said idle/working/waiting/throttled, but if
		// the window holds nothing but a shell the process is gone. idle is a
		// real signal (SessionStart already fired), not "no signal yet" — that
		// case is hasState==false, handled below by leaving eff nil. An agent
		// that crashed between SessionStart and its first prompt used to sit
		// reading "idle" forever here, matching bin/kel's effective_state
		// before it was fixed for the same reason: found by walking through
		// the tool as a fresh user, not by inspection.
		var eff *string
		if hasState && st.state != "" {
			eff = sp(st.state)
			if st.state == "idle" || st.state == "working" || st.state == "waiting" || st.state == "throttled" {
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

	// #25: a record that fails to parse must render as a visibly wrong row,
	// not vanish — invariant 3 (never report a guess as a fact; an unknown
	// gets its own distinct display). group/name come from the path, the
	// only part that survives a parse failure; every other field is null,
	// the same shape the "no live window" agent above uses.
	for _, f := range unreadable {
		agents = append(agents, Agent{
			Group: filepath.Base(filepath.Dir(f)),
			Name:  strings.TrimSuffix(filepath.Base(f), ".json"),
			State: sp("unreadable"), Managed: true, Panes: []string{},
		})
	}

	cur := Current{}
	if curGroup != "" {
		cur.Group = sp(curGroup)
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

// repoGroup names the group for a directory kel did not create a window in:
// a .kel/group file at or above it (stopping at the repo root, as bin/kel's
// dir_group does), else the repo root's basename, else "misc".  Stat calls
// only — this runs on the bar's redraw path, where a `git` fork per window
// would break invariant 5.
func repoGroup(dir string) string {
	if dir == "" {
		return "misc"
	}
	for d := filepath.Clean(dir); ; d = filepath.Dir(d) {
		if b, err := os.ReadFile(filepath.Join(d, ".kel", "group")); err == nil {
			if g := strings.TrimSpace(string(b)); slugOK(g) {
				return g
			}
		}
		if _, err := os.Stat(filepath.Join(d, ".git")); err == nil {
			return filepath.Base(d)
		}
		if d == filepath.Dir(d) {
			return "misc"
		}
	}
}

// slugOK matches bin/kel's slug_ok: A-Z a-z 0-9 . _ -, and not empty.
func slugOK(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		ok := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' ||
			c == '.' || c == '_' || c == '-'
		if !ok {
			return false
		}
	}
	return true
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

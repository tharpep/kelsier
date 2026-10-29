// Package fleet computes the kelsier fleet document: one answer to "what is
// the fleet doing", assembled from tmux and the on-disk state directory.
//
// It began as a port of bin/kel's bash reader, checked against it by a
// differential test.  v0.10 deleted the bash reader: this is now the only
// implementation, and the document it emits is the contract every surface
// (kel ls, the bar, the sidebar) reads.
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
	PaneID        *string  `json:"pane_id"`
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
	// Doing: a working agent's last tool call in words, read from the tail
	// of Claude Code's transcript (doing.go). Sidebar path only
	Doing   *string  `json:"doing,omitempty"`
	Context *Context `json:"context"`
	// Unseen marks a `done` the user has not looked at since it finished: the
	// state is newer than the epoch in <window-id>.seen, which the sidebar
	// writes when the window is shown (spec.md §9). No .seen file means never
	// looked at. A flag beside State, never a state of its own, so kel jump,
	// the notifications and the bar — which all match on state — see `done`.
	Unseen bool `json:"unseen"`
	// Claude is Claude Code's own view of this agent (claude.go): only on
	// the sidebar's path (Options.Panes), and null when it could not be read
	Claude *ClaudeView `json:"claude,omitempty"`
	// Subagents: the agent panel's rows, from `kel substatus` (claude.go).
	// Empty when none are showing; recorded within subsFresh or not at all
	Subagents []Subagent `json:"subagents,omitempty"`
}

type Current struct {
	Group    *string `json:"group"`
	WindowID *string `json:"window_id"`
}

type Doc struct {
	GeneratedAt string  `json:"generated_at"`
	Current     Current `json:"current"`
	Agents      []Agent `json:"agents"`
	// Quota is null when no session has reported rate limits (quota.go).
	Quota *Quota `json:"quota"`
	Here  *Here  `json:"here,omitempty"` // only with Options.Here
	// Panes is present only when asked for (Options.Panes): every pane in
	// every kel window, for the sidebar. A pointer so that "asked, and there
	// are none" is [] while "not asked" omits the key.
	Panes *[]Pane `json:"panes,omitempty"`
	// Background lists agents Claude Code runs in no pane (`claude --bg`).
	// Null, not empty, when Claude Code could not be asked (claude.go)
	Background *[]Background `json:"background,omitempty"`
	// Repos: every repo a pane stands in, with all its worktrees and their
	// land state (worktrees.go). Only with Options.Panes or Worktrees
	Repos *[]Repo `json:"repos,omitempty"`
}

// record is the on-disk agent record, sessions/<group>/<name>.json.
type record struct {
	Name          string   `json:"name"`
	WindowID      string   `json:"window_id"`
	PaneID        string   `json:"pane_id"`
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

// StateDir is kel's state directory (CLAUDE.md, the on-disk contract).
func StateDir() string {
	if d := os.Getenv("XDG_STATE_HOME"); d != "" {
		return filepath.Join(d, "kel")
	}
	return filepath.Join(os.Getenv("HOME"), ".local", "state", "kel")
}

// WriteAtomic replaces path in one rename, so a reader in another process
// sees the old file or the new one, never half of either. 0600: the state
// directory holds repo paths and Claude Code's notification text.
func WriteAtomic(path string, b []byte) {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return
	}
	_, werr := tmp.Write(b)
	if cerr := tmp.Close(); werr == nil && cerr == nil && os.Chmod(tmp.Name(), 0o600) == nil && os.Rename(tmp.Name(), path) == nil {
		return
	}
	_ = os.Remove(tmp.Name())
}

func prefix() string {
	if p := os.Getenv("KEL_SESSION"); p != "" {
		return p
	}
	return "kel"
}

// Options selects the expensive extras. The status bar asks for none of them;
// only PR may touch the network.
type Options struct {
	Dirty bool
	Land  bool
	PR    bool // may refresh the PR cache — never on a refresh path
	Panes bool // every pane, with kind, git position and ports (panes.go)
	Here  bool // the focused pane's repo and git position (quota.go)
	// Worktrees: the repos' worktrees asked fresh, not from the sidebar's
	// cache — for `kel sweep`, which acts on the answer. Implies Panes
	Worktrees bool
}

// Load assembles the fleet document.
func Load(o Options) Doc {
	withDirty := o.Dirty || o.Land || o.PR
	sd := StateDir()
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
		// path last: it is the one field that could hold anything
		panesRaw = tmuxOut("list-panes", "-a", "-F",
			"#{window_id}\t#{pane_current_command}\t#{pane_id}\t#{pane_index}\t#{pane_active}\t#{pane_pid}\t#{@kel-sidebar}\t#{@kel-agent}\t#{pane_current_path}")
	}()
	go func() {
		defer wg.Done()
		curRaw = tmuxOut("display-message", "-p", "#{client_session} #{window_id} #{pane_id}")
	}()
	wg.Wait()

	curSess, curWid, curPane := "", "", ""
	if f := strings.Fields(strings.TrimSpace(curRaw)); len(f) == 3 {
		curSess, curWid, curPane = f[0], f[1], f[2]
	}

	paneMap := map[string][]string{}
	firstPath := map[string]string{} // window id -> its first pane's cwd
	paneRows := map[string][]paneRow{}
	for _, l := range lines(panesRaw) {
		p := strings.SplitN(l, "\t", 9)
		if len(p) != 9 {
			continue
		}
		// The sidebar pane (v0.10 M2) is kel's, not the user's: it runs
		// kel-sidebar, a non-shell, so counting it would read a crashed agent as
		// alive (invariant 3) and list the sidebar inside itself.
		if p[6] == "1" {
			continue
		}
		paneMap[p[0]] = append(paneMap[p[0]], p[1])
		if _, seen := firstPath[p[0]]; !seen {
			firstPath[p[0]] = p[8]
		}
		paneRows[p[0]] = append(paneRows[p[0]], paneRow{
			wid: p[0], cmd: p[1], id: p[2], idx: p[3], active: p[4], pid: p[5], stamp: p[7], path: p[8]})
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
	recByPane := map[string]record{}
	for _, r := range records {
		if r.WindowID != "" {
			recByWid[r.WindowID] = r
		}
		if r.PaneID != "" {
			recByPane[r.PaneID] = r
		}
	}

	paneRec := func(p paneRow) (record, bool) {
		r, ok := recByPane[p.id]
		if !ok || p.stamp != recGroup(r)+"/"+r.Name {
			return record{}, false
		}
		return r, true
	}

	lands := map[string]*Land{}
	locals := map[string]localState{}
	if o.Land || o.PR {
		lands, locals = landMap(records, o.PR)
	}

	// dirty is a property of the directory, not the window: read from what
	// landMap already asked git, or asked here when only dirty was wanted
	dirtyMap := map[string]*float64{}
	if withDirty {
		for _, r := range records {
			if r.Cwd == nil || *r.Cwd == "" {
				continue
			}
			ls, ok := locals[*r.Cwd]
			if !ok {
				ls = local(*r.Cwd)
				locals[*r.Cwd] = ls
			}
			if ls.repo {
				n := ls.dirty
				dirtyMap[*r.Cwd] = &n
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
	agentPanes := map[string]bool{}
	var paneWins []winInfo
	curGroup := ""
	for _, w := range wins {
		// every kel window is in the one session named pfx (v0.10); v0.9's
		// per-repo `kel/<group>` sessions went once every machine upgraded
		if w.sess != pfx {
			continue
		}
		rec, managed := recByWid[w.wid]
		if managed && rec.Name != w.name {
			managed = false
		}
		// where a window's group comes from, most authoritative first: its
		// record; the @kel-group window option kel new sets; the repo its
		// first pane stands in
		g := ""
		switch {
		case managed:
			g = recGroup(rec)
		case w.kelGroup != "":
			g = w.kelGroup
		default:
			g = repoGroup(firstPath[w.wid])
		}
		if w.wid == curWid {
			curGroup = g
		}
		agentCmd := ""
		if managed && rec.Agent != nil {
			if f := strings.Fields(*rec.Agent); len(f) > 0 {
				agentCmd = f[0]
			}
		}
		paneWins = append(paneWins, winInfo{w.wid, w.idx, w.name, g, agentCmd})
		panes := paneMap[w.wid]
		if panes == nil {
			panes = []string{}
		}

		var aps []paneRow
		for _, r := range paneRows[w.wid] {
			_, st := stateMap[r.id]
			_, cx := ctxMap[r.id]
			_, rp := paneRec(r)
			if st || cx || rp || isAgentCmd(r.cmd) || (agentCmd != "" && r.cmd == agentCmd) {
				aps = append(aps, r)
			}
		}
		legacy := len(aps) == 0
		if legacy {
			if rows := paneRows[w.wid]; len(rows) > 0 {
				aps = rows[:1]
			} else {
				aps = []paneRow{{wid: w.wid}}
			}
		}

		for i, r := range aps {
			arec, amanaged := paneRec(r)
			if !amanaged && i == 0 && managed && (rec.PaneID == "" || rec.PaneID == r.id) {
				arec, amanaged = rec, true
			}
			name := w.name
			switch {
			case amanaged:
				name = arec.Name
			case i > 0:
				name = w.name + "-" + r.idx
			}
			ag := g
			if amanaged && !(i == 0 && managed) {
				ag = recGroup(arec)
			}
			st, hasState := stateMap[r.id]
			if !hasState && i == 0 {
				st, hasState = stateMap[w.wid]
			}
			cx := ctxMap[r.id]
			if cx == nil && i == 0 {
				cx = ctxMap[w.wid]
			}

			var eff *string
			if hasState && st.state != "" {
				eff = sp(st.state)
				if st.state == "idle" || st.state == "working" || st.state == "waiting" || st.state == "throttled" {
					alive := false
					if legacy {
						for _, c := range panes {
							if !shells[c] {
								alive = true
								break
							}
						}
					} else {
						alive = !shells[r.cmd]
					}
					if !alive {
						eff = sp("dead")
					}
				}
			}

			a := Agent{
				Group: ag, Name: name, WindowID: sp(w.wid), PaneID: emptyToNil(r.id), Index: numPtr(w.idx),
				Current: w.sess == curSess && w.wid == curWid, Managed: amanaged,
				State: eff, Panes: panes, Activity: numPtr(w.activity),
				Context: cx,
			}
			if hasState {
				a.StateSince, a.Note = st.since, st.note
			}
			if eff != nil && *eff == "done" {
				a.Unseen = unseen(sd, w.wid, a.StateSince)
			}
			a.Subagents = loadSubs(sd, r.id, time.Now().Unix())
			if a.Subagents == nil && i == 0 {
				a.Subagents = loadSubs(sd, w.wid, time.Now().Unix())
			}
			if amanaged {
				a.Isolation, a.Branch, a.Repo, a.Cwd = arec.Isolation, arec.Branch, arec.Repo, arec.Cwd
				a.AgentCmd, a.ClaudeSession, a.Compactions = arec.Agent, arec.ClaudeSession, arec.Compactions
				if arec.Cwd != nil {
					a.Dirty = dirtyMap[*arec.Cwd]
					a.Land = lands[*arec.Cwd]
				}
			}
			if r.id != "" && (!legacy || amanaged || isAgentCmd(r.cmd) || (agentCmd != "" && r.cmd == agentCmd)) {
				agentPanes[r.id] = true
			}
			agents = append(agents, a)
		}
	}

	// records with no live window. Same liveness rule as meta_is_live: the
	// window must match BOTH the recorded id and the recorded name.
	stamps := map[string]string{}
	for _, rows := range paneRows {
		for _, r := range rows {
			stamps[r.id] = r.stamp
		}
	}
	for _, r := range records {
		live := r.PaneID != "" && stamps[r.PaneID] == recGroup(r)+"/"+r.Name
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

	now := time.Now()
	doc := Doc{
		GeneratedAt: now.UTC().Format(time.RFC3339),
		Current:     cur,
		Agents:      agents,
		Quota:       loadQuota(sd, now.Unix()),
	}
	if o.Here {
		for _, rows := range paneRows {
			for _, r := range rows {
				if r.id == curPane {
					doc.Here = loadHere(r.path)
				}
			}
		}
	}
	if o.Panes || o.Worktrees {
		ps := buildPanes(paneRows, paneWins, curPane, agentPanes)
		doc.Panes = &ps
		withClaude(&doc, sd, ps)
		withDoing(&doc)
		withRepos(&doc, sd, o.Worktrees)
	}
	return doc
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

// unseen: the window's .seen epoch is older than the state's, or absent. A
// done with no epoch of its own cannot be dated, so it counts as unseen —
// loud is the safe side of a guess about whether you have looked.
func unseen(sd, wid string, since *float64) bool {
	b, err := os.ReadFile(filepath.Join(sd, wid+".seen"))
	if err != nil {
		return true
	}
	seen := numPtr(strings.TrimSpace(string(b)))
	if seen == nil || since == nil {
		return true
	}
	// a tie is unseen: epochs are whole seconds, so "shown at 100, finished at
	// 100" may be either order. If you were looking, the sidebar showing it
	// marks it seen again on its next load, so the loud side costs a blink.
	return *since >= *seen
}

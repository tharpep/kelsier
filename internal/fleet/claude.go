package fleet

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Claude Code's own account of its sessions — `claude agents --json` — read
// alongside the hooks (spec.md §9). The hooks say what changed; this says what
// is. An interrupt (esc) fires no Stop, so a hook-only kel showed an agent at
// its prompt as "working" for fifteen hours while Claude Code itself reported
// it idle. It also lists background agents (`claude --bg`), which run in no
// tmux pane and which kel could not see at all.
//
// One call is a node start, ~1 s, so it is cached in the state dir, refreshed
// detached once older than claudeTTL, and only read on the sidebar's path (Options.Panes), never
// the bar's. Unavailable — no claude, an older one without `agents --json`,
// a timeout — is nil, and every reader falls back to the hooks alone.

const (
	claudeTTL          = 10 * time.Second
	claudeRefreshGuard = 5 * time.Second
	claudeMaxAge       = time.Minute
)

// ClaudeSession is one entry of `claude agents --json`. Only the fields kel
// reads; the rest pass by.
type ClaudeSession struct {
	PID       int    `json:"pid"`
	ID        string `json:"id,omitempty"` // background sessions: the short id `claude attach` takes
	Cwd       string `json:"cwd"`
	Kind      string `json:"kind"` // interactive | background
	SessionID string `json:"sessionId"`
	Name      string `json:"name"`
	Status    string `json:"status"`          // busy | idle | waiting
	State     string `json:"state,omitempty"` // background: e.g. blocked
	StartedAt int64  `json:"startedAt"`       // ms
	// WaitingFor is what a waiting session waits on, in Claude Code's words
	WaitingFor string `json:"waitingFor,omitempty"`
}

// ClaudeView is what an agent row learns from Claude Code.
type ClaudeView struct {
	Status    string `json:"status"`
	SessionID string `json:"session_id"`
	Name      string `json:"name"`
	At        int64  `json:"at,omitempty"`
}

// Background is an agent Claude Code runs outside any pane.
type Background struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Cwd        string `json:"cwd"`
	Status     string `json:"status"`
	State      string `json:"state,omitempty"`
	SessionID  string `json:"session_id"`
	StartedAt  int64  `json:"-"`
	WaitingFor string `json:"-"`
	Group      string `json:"group"`
	Git        *Git   `json:"git"`
}

type claudeCache struct {
	At       int64           `json:"at"`
	Bin      string          `json:"bin"`
	Sessions []ClaudeSession `json:"sessions"`
	Failed   bool            `json:"failed"`
	// Refreshing is the epoch a detached refresh was last started.
	Refreshing int64 `json:"refreshing,omitempty"`
}

// claudeBin finds claude once and remembers where: a PATH lookup on WSL walks
// every /mnt/c directory (CLAUDE.md, traps), so it is not paid per refresh.
func claudeBin(cached string) string {
	if b := os.Getenv("KEL_CLAUDE_BIN"); b != "" {
		return b
	}
	if cached != "" {
		if _, err := os.Stat(cached); err == nil {
			return cached
		}
	}
	b, err := exec.LookPath("claude")
	if err != nil {
		return ""
	}
	return b
}

// claudeSessions returns what the cache holds at once and never waits for
// claude: a cache older than claudeTTL starts a detached refresh and is still
// returned, up to claudeMaxAge, past which it is unknown (ok false). Only a
// missing cache reads inline, once, so a first sidebar is not empty.
func claudeSessions(sd string) (sessions []ClaudeSession, at int64, ok bool) {
	path := filepath.Join(sd, "claude-agents.json")
	var c claudeCache
	b, err := os.ReadFile(path)
	if err != nil || json.Unmarshal(b, &c) != nil {
		return refreshClaudeAgents(sd, "")
	}
	age := time.Since(time.Unix(c.At, 0))
	if age >= claudeTTL && time.Since(time.Unix(c.Refreshing, 0)) >= claudeRefreshGuard {
		c.Refreshing = time.Now().Unix()
		if b, err := json.Marshal(c); err == nil {
			WriteAtomic(path, b)
		}
		StartClaudeRefresh(sd)
	}
	return c.Sessions, c.At, !c.Failed && age < claudeMaxAge
}

// RefreshClaudeAgents asks claude and rewrites the cache in sd.
func RefreshClaudeAgents(sd string) {
	var c claudeCache
	if b, err := os.ReadFile(filepath.Join(sd, "claude-agents.json")); err == nil {
		json.Unmarshal(b, &c)
	}
	refreshClaudeAgents(sd, c.Bin)
}

// refreshClaudeAgents asks claude and writes the cache atomically.
func refreshClaudeAgents(sd, cachedBin string) ([]ClaudeSession, int64, bool) {
	bin := claudeBin(cachedBin)
	c := claudeCache{At: time.Now().Unix(), Bin: bin}
	if bin == "" {
		c.Failed = true
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, bin, "agents", "--json").Output()
		if err != nil || json.Unmarshal(out, &c.Sessions) != nil {
			c.Failed, c.Sessions = true, nil
		}
	}
	if b, err := json.Marshal(c); err == nil {
		WriteAtomic(filepath.Join(sd, "claude-agents.json"), b)
	}
	return c.Sessions, c.At, !c.Failed
}

// StartClaudeRefresh is how a host refreshes a stale cache without making the
// load wait: a goroutine for a long-lived process, a detached re-exec for a
// short-lived one (cmd/kel-fleet).
var StartClaudeRefresh = func(sd string) { go RefreshClaudeAgents(sd) }

// parentOf reads a process's parent: /proc on Linux, one `ps` on macOS. ok is
// false when the process is gone or unreadable.
func parentOf(pid int) (int, bool) {
	if b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat"); err == nil {
		s := string(b)
		if r := strings.LastIndexByte(s, ')'); r >= 0 {
			if f := strings.Fields(s[r+1:]); len(f) >= 2 {
				if pp, err := strconv.Atoi(f[1]); err == nil {
					return pp, true
				}
			}
		}
		return 0, false
	}
	out, err := exec.Command("ps", "-o", "ppid=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return 0, false
	}
	pp, err := strconv.Atoi(strings.TrimSpace(string(out)))
	return pp, err == nil
}

// paneOfPID walks up from pid to the first ancestor that is a pane's process.
// A claude started at a pane's shell is that shell's child; a few hops at most.
func paneOfPID(pid int, panePIDs map[int]string) (string, bool) {
	for i := 0; i < 8 && pid > 1; i++ {
		if p, ok := panePIDs[pid]; ok {
			return p, true
		}
		pp, ok := parentOf(pid)
		if !ok {
			return "", false
		}
		pid = pp
	}
	return "", false
}

// withClaude folds Claude Code's sessions into the document by the precedence
// rule of spec.md §9: an interactive session finds its agent by walking up from
// its process to a pane's, and corrects the hook state only where it is newer.
func withClaude(doc *Doc, sd string, panes []Pane) {
	sessions, at, ok := claudeSessions(sd)
	if !ok {
		return
	}
	pidPane := map[int]string{}
	for _, p := range panes {
		if p.PID != nil {
			pidPane[int(*p.PID)] = p.PaneID
		}
	}
	paneWin := map[string]string{}
	for _, p := range panes {
		paneWin[p.PaneID] = p.WindowID
	}
	byWin := map[string]int{}
	byPane := map[string]int{}
	for i, a := range doc.Agents {
		if a.PaneID != nil {
			byPane[*a.PaneID] = i
		} else if a.WindowID != nil {
			if _, ok := byWin[*a.WindowID]; !ok {
				byWin[*a.WindowID] = i
			}
		}
	}
	bg := []Background{}
	roots := map[string]bool{}
	for _, s := range sessions {
		if s.Kind == "background" {
			bg = append(bg, Background{ID: s.ID, Name: s.Name, Cwd: s.Cwd, Status: s.Status,
				State: s.State, SessionID: s.SessionID, StartedAt: s.StartedAt, WaitingFor: s.WaitingFor,
				Group: worktreeRepo(s.Cwd)})
			if r, _, ok := repoRoot(s.Cwd); ok {
				roots[r] = true
			}
			continue
		}
		pane, ok := paneOfPID(s.PID, pidPane)
		if !ok {
			continue // a claude in no kel pane: another terminal, not this workspace
		}
		if doc.Panes != nil {
			for pi := range *doc.Panes {
				if (*doc.Panes)[pi].PaneID == pane {
					(*doc.Panes)[pi].Kind = "agent"
					(*doc.Panes)[pi].Agent = true
				}
			}
		}
		i, ok := byPane[pane]
		if !ok {
			i, ok = byWin[paneWin[pane]]
		}
		if !ok {
			continue
		}
		correct(&doc.Agents[i], s, at)
	}
	kept := bg[:0]
	for _, b := range bg {
		i, ok := foldTarget(doc.Agents, b)
		if !ok {
			kept = append(kept, b)
			continue
		}
		correct(&doc.Agents[i], ClaudeSession{Kind: "background", Status: b.Status, State: b.State, SessionID: b.SessionID,
			Name: b.Name, StartedAt: b.StartedAt, WaitingFor: b.WaitingFor}, at)
	}
	bg = kept
	pos := gitPositions(roots)
	for i := range bg {
		if r, linked, ok := repoRoot(bg[i].Cwd); ok && pos[r] != nil {
			g := *pos[r]
			g.Worktree = linked
			bg[i].Git = &g
		}
	}
	doc.Background = &bg
}

// foldTarget finds the one agent a background session is: Claude Code can move
// a session to the background while its window goes on showing it. The agent
// is managed, has a window and no session of its own, and either shares the
// session's name and directory or records its id. Zero or several is no fold.
func foldTarget(agents []Agent, b Background) (int, bool) {
	found := -1
	for i, a := range agents {
		if !a.Managed || a.WindowID == nil || a.Claude != nil {
			continue
		}
		byID := a.ClaudeSession != nil && *a.ClaudeSession == b.SessionID
		byName := a.Name == b.Name && a.Cwd != nil && (b.Cwd == *a.Cwd || strings.HasPrefix(b.Cwd, *a.Cwd+"/"))
		if !byID && !byName {
			continue
		}
		if found >= 0 {
			return 0, false
		}
		found = i
	}
	return found, found >= 0
}

func mapClaude(s ClaudeSession) string {
	switch {
	case s.Status == "waiting":
		return "waiting"
	case s.Status == "busy":
		return "working"
	case s.Status == "idle":
		return "idle"
	}
	return ""
}

func replaced(a *Agent, s ClaudeSession, at int64) bool {
	if s.Kind == "background" {
		return a.State == nil || a.StateSince == nil || *a.StateSince < float64(at)
	}
	return s.StartedAt > 0 && a.StateSince != nil && *a.StateSince < float64(s.StartedAt/1000)
}

// correct gives an agent Claude Code's own account of it (spec.md §9). The
// newer of the hook and the cache wins; a session that began after the hook
// state was written has replaced whatever wrote it.
func correct(a *Agent, s ClaudeSession, at int64) {
	a.Claude = &ClaudeView{Status: s.Status, SessionID: s.SessionID, Name: s.Name, At: at}
	ctxOlder := a.Context != nil && (a.Context.At == nil || *a.Context.At < float64(at))
	if a.Context != nil && a.Context.Session != nil && *a.Context.Session != s.SessionID && ctxOlder {
		a.Context = nil
	}
	if a.State != nil && *a.State == "dead" {
		return
	}
	want := mapClaude(s)
	enter := func() {
		a.Note = nil
		if want == "waiting" {
			a.Note = emptyToNil(s.WaitingFor)
		}
	}
	if a.ClaudeSession != nil && *a.ClaudeSession != "" && *a.ClaudeSession != s.SessionID && replaced(a, s, at) {
		a.Unseen, a.StateSince = false, nil
		if a.State == nil || ctxOlder {
			a.Context = nil
		}
		if want != "" {
			a.State, a.StateFrom = sp(want), "claude"
			enter()
		}
		return
	}
	if want == "" {
		return
	}
	if a.State == nil {
		a.State = sp(want)
		enter()
		return
	}
	if a.StateSince != nil && *a.StateSince >= float64(at) {
		return
	}
	cur := *a.State
	if cur == want {
		return
	}
	if want == "idle" && (cur == "error" || cur == "throttled" || cur == "done") {
		return
	}
	a.State, a.Unseen, a.StateFrom = sp(want), false, "claude"
	enter()
}

// Subagent is one row of an agent's subagent panel (Task-tool helpers).
type Subagent struct {
	ID        string  `json:"id"`
	Name      string  `json:"name,omitempty"`
	AgentType string  `json:"agentType"`
	Status    string  `json:"status"` // running | completed | failed | killed
	Label     string  `json:"label"`
	StartTime float64 `json:"startTime"` // ms
}

// subsFresh: `kel substatus` runs every refresh tick while the panel has rows
// and stops when it has none, so a file older than this is no rows now.
const subsFresh = 30

// loadSubs reads <window-id>.subs: an epoch line, then the tasks as JSON.
func loadSubs(sd, wid string, now int64) []Subagent {
	b, err := os.ReadFile(filepath.Join(sd, wid+".subs"))
	if err != nil {
		return nil
	}
	lines := strings.SplitN(string(b), "\n", 3)
	if len(lines) < 2 {
		return nil
	}
	at, err := strconv.ParseInt(strings.TrimSpace(lines[0]), 10, 64)
	if err != nil || now-at > subsFresh {
		return nil
	}
	var subs []Subagent
	if json.Unmarshal([]byte(lines[1]), &subs) != nil {
		return nil
	}
	return subs
}

// worktreeRepo names the repo a directory belongs to: for a linked worktree,
// the main repo — its `.git` file points at <main>/.git/worktrees/<name> —
// not the worktree folder, whose name (`api-gw-wt5`) is what repoGroup would
// give. Stat and one small read; no git fork.
func worktreeRepo(dir string) string {
	root, linked, ok := repoRoot(dir)
	if !ok || !linked {
		return repoGroup(dir)
	}
	if m := mainRoot(root); m != root {
		return filepath.Base(m)
	}
	return repoGroup(dir)
}

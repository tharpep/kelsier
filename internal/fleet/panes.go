package fleet

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Pane is one tmux pane in a kel window — every pane, not just agents. The
// sidebar (v0.10 M3) lists these; `kel _fleet --panes` asks for them. Opt-in,
// because the git and port lookups cost more than the bar's redraw budget
// (invariant 5).
type Pane struct {
	PaneID      string   `json:"pane_id"`
	WindowID    string   `json:"window_id"`
	WindowIndex *float64 `json:"window_index"`
	WindowName  string   `json:"window_name"`
	Group       string   `json:"group"`
	PaneIndex   *float64 `json:"pane_index"`
	Active      bool     `json:"active"`  // the active pane of its window
	Current     bool     `json:"current"` // the pane the client is looking at
	Cmd         string   `json:"cmd"`
	PID         *float64 `json:"pid"`
	Cwd         string   `json:"cwd"`
	Kind        string   `json:"kind"`  // agent | server | shell | program
	Agent       bool     `json:"agent"` // this pane is its window's agent
	// Ports is the listening TCP ports in the pane's process tree. null means
	// "could not tell" (no /proc and no lsof), never "none" — that is [].
	Ports *[]int `json:"ports"`
	Git   *Git   `json:"git"` // null outside a repo
}

// Git is where a pane's directory stands: position, not the land verdict
// (that is land.go, which also asks the network).
type Git struct {
	Root     string  `json:"root"`
	Worktree bool    `json:"worktree"` // a linked worktree, not the main checkout
	Branch   *string `json:"branch"`   // null when detached
	Upstream *string `json:"upstream"`
	Ahead    *int    `json:"ahead"`
	Behind   *int    `json:"behind"`
	Dirty    *int    `json:"dirty"`
}

// paneRow is one line of `tmux list-panes -a`.
type paneRow struct {
	wid, cmd, id, idx, active, pid, stamp, path string
}

// repoRoot walks up from dir to the first directory holding `.git`. linked is
// true when `.git` is a file — a linked worktree (git worktree add) points at
// its main repo that way; a main checkout has a directory. Stat calls only.
func repoRoot(dir string) (root string, linked, ok bool) {
	if dir == "" {
		return "", false, false
	}
	for d := filepath.Clean(dir); ; d = filepath.Dir(d) {
		if fi, err := os.Stat(filepath.Join(d, ".git")); err == nil {
			return d, !fi.IsDir(), true
		}
		if d == filepath.Dir(d) {
			return "", false, false
		}
	}
}

// parseStatusV2 reads `git status --porcelain=v2 --branch`: the header lines
// give branch, upstream and ahead/behind; every other line is one changed or
// untracked path, which is what `dirty` counts (the same count land.go's
// `git status --porcelain` gives).
func parseStatusV2(out string) Git {
	var g Git
	dirty := 0
	for _, l := range strings.Split(out, "\n") {
		switch {
		case l == "":
		case strings.HasPrefix(l, "# branch.head "):
			if h := strings.TrimPrefix(l, "# branch.head "); h != "(detached)" {
				g.Branch = sp(h)
			}
		case strings.HasPrefix(l, "# branch.upstream "):
			g.Upstream = sp(strings.TrimPrefix(l, "# branch.upstream "))
		case strings.HasPrefix(l, "# branch.ab "):
			for _, f := range strings.Fields(strings.TrimPrefix(l, "# branch.ab ")) {
				n, err := strconv.Atoi(f[1:])
				if err != nil {
					continue
				}
				n2 := n
				if f[0] == '+' {
					g.Ahead = &n2
				} else if f[0] == '-' {
					g.Behind = &n2
				}
			}
		case strings.HasPrefix(l, "#"):
		default:
			dirty++
		}
	}
	g.Dirty = &dirty
	return g
}

type gitCacheEntry struct {
	at  time.Time
	git Git
}

var (
	gitMu    sync.RWMutex
	gitCache = map[string]gitCacheEntry{}
)

const gitTTL = 2 * time.Second

// gitPositions computes Git for each distinct repo root, concurrently — one
// `git status` fork per repo however many panes stand in it. Results are
// cached for gitTTL (2s) to prevent thrashing during rapid sidebar updates.
func gitPositions(roots map[string]bool) map[string]*Git {
	out := map[string]*Git{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	now := time.Now()
	for r := range roots {
		gitMu.RLock()
		cached, ok := gitCache[r]
		gitMu.RUnlock()
		if ok && now.Sub(cached.at) < gitTTL {
			cp := cached.git
			out[r] = &cp
			continue
		}

		wg.Add(1)
		go func(r string) {
			defer wg.Done()
			b, err := exec.Command("git", "-C", r, "status", "--porcelain=v2", "--branch").Output()
			if err != nil {
				return // not readable as a repo: leave the pane's git null
			}
			g := parseStatusV2(string(b))
			g.Root = r

			gitMu.Lock()
			gitCache[r] = gitCacheEntry{at: time.Now(), git: g}
			gitMu.Unlock()

			mu.Lock()
			out[r] = &g
			mu.Unlock()
		}(r)
	}
	wg.Wait()
	return out
}

var semverRe = regexp.MustCompile(`^[0-9]+\.[0-9]+(\.[0-9]+)?$`)

var knownAgents = map[string]bool{
	"claude":   true,
	"agy":      true,
	"codex":    true,
	"aider":    true,
	"opencode": true,
}

func isAgentCmd(cmd string) bool {
	return knownAgents[cmd]
}

// classify names what a pane is doing, cheapest signal first. agentCmd is the
// first word of the window's record `agent` (empty for an unmanaged window).
// Claude Code's process names itself `claude`, so tmux reports exactly that.
// Known agents (claude, agy, codex, aider, opencode) are recognized even
// without an agent record. On macOS, native Claude installs link to
// versions/<version>, so tmux reports the version (e.g. 2.1.304). When agentCmd
// is claude, we recognize the semver.
// Unmanaged native Claude panes are promoted by withClaude via PID verification.
func classify(cmd, agentCmd string, ports *[]int) (kind string, agent bool) {
	switch {
	case cmd != "" && (cmd == agentCmd || knownAgents[cmd] || (agentCmd == "claude" && semverRe.MatchString(cmd))):
		return "agent", true
	case ports != nil && len(*ports) > 0:
		return "server", false
	case shells[cmd]:
		return "shell", false
	}
	return "program", false
}

// buildPanes assembles the panes array for the windows kel owns.
func buildPanes(rows map[string][]paneRow, wins []winInfo, curPane string, agentPanes map[string]bool) []Pane {
	// git: one status per distinct repo root
	roots := map[string]bool{}
	rootOf := map[string]string{}
	linkedOf := map[string]bool{}
	for _, w := range wins {
		for _, r := range rows[w.wid] {
			if root, linked, ok := repoRoot(r.path); ok {
				roots[root] = true
				rootOf[r.path] = root
				linkedOf[root] = linked
			}
		}
	}
	gits := gitPositions(roots)

	// ports: by pane pid, cached (ports.go)
	var pids []int
	for _, w := range wins {
		for _, r := range rows[w.wid] {
			if n, err := strconv.Atoi(r.pid); err == nil {
				pids = append(pids, n)
			}
		}
	}
	ports := panePorts(pids)

	out := []Pane{}
	for _, w := range wins {
		for _, r := range rows[w.wid] {
			p := Pane{
				PaneID: r.id, WindowID: w.wid, WindowIndex: numPtr(w.idx),
				WindowName: w.name, Group: w.group, PaneIndex: numPtr(r.idx),
				Active: r.active == "1", Current: r.id == curPane,
				Cmd: r.cmd, PID: numPtr(r.pid), Cwd: r.path,
			}
			if n, err := strconv.Atoi(r.pid); err == nil && ports != nil {
				pp := ports[n]
				if pp == nil {
					pp = []int{}
				}
				p.Ports = &pp
			}
			p.Kind, _ = classify(r.cmd, w.agentCmd, p.Ports)
			p.Agent = agentPanes[r.id]
			if p.Agent {
				p.Kind = "agent"
			} else if p.Kind == "agent" {
				p.Kind = "program"
			}
			if root, ok := rootOf[r.path]; ok {
				if g := gits[root]; g != nil {
					gg := *g
					gg.Worktree = linkedOf[root]
					p.Git = &gg
				}
			}
			out = append(out, p)
		}
	}
	return out
}

// winInfo is what buildPanes needs about a window, resolved by Load.
type winInfo struct {
	wid, idx, name, group, agentCmd string
}

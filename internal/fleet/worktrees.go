package fleet

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Every worktree of every repo the workspace stands in — not only the ones a
// window is open in. The author found worktrees "aren't easy to keep track
// of", so the sidebar nests each repo's worktrees under it with its land
// state, including the ones nobody has open and Claude Code's own
// (`claude --worktree` puts them in <repo>/.claude/worktrees); `git worktree
// list` knows them all, wherever they live.
//
// One `git worktree list` per repo, then landOf per linked worktree (several
// local git calls, never the network: the PR half reads the cache only). On
// the sidebar's path that is cached in the state dir for worktreesTTL; `kel
// sweep` asks fresh (Options.Worktrees), since it acts on the answer.

const worktreesTTL = 20 * time.Second

// Repo is one repository and its linked worktrees.
type Repo struct {
	Root      string     `json:"root"` // the main checkout
	Name      string     `json:"name"`
	Branch    *string    `json:"branch"` // the main checkout's; null when detached
	Worktrees []Worktree `json:"worktrees"`
}

// Worktree is one linked worktree. Land is null while it is missing.
type Worktree struct {
	Path    string  `json:"path"`
	Branch  *string `json:"branch"` // null when detached
	Land    *Land   `json:"land"`
	Missing bool    `json:"missing"` // its directory is gone (git calls it prunable)
	Locked  bool    `json:"locked"`
	// Sessions counts Claude Code sessions standing in it — background ones,
	// and interactive ones in other terminals, which no pane shows. -1 when
	// Claude Code could not be asked. Sweep leaves a worktree in use alone
	Sessions int `json:"sessions"`
}

type worktreesCache struct {
	At     int64            `json:"at"`
	Repos  map[string]Repo  `json:"repos"`
	Stamps map[string]int64 `json:"stamps"` // each repo's worktreeStamp when read
}

// worktreeStamp changes when a worktree is added, removed or pruned — git
// keeps one directory per worktree in <main>/.git/worktrees — so the cache
// never hides a new one for its whole TTL. One stat; 0 when there are none.
func worktreeStamp(main string) int64 {
	st, err := os.Stat(filepath.Join(main, ".git", "worktrees"))
	if err != nil {
		return 0
	}
	return st.ModTime().UnixNano()
}

// mainRoot is the main checkout a repo root belongs to: a linked worktree's
// `.git` file points at <main>/.git/worktrees/<name>. Stat and one read.
func mainRoot(root string) string {
	b, err := os.ReadFile(filepath.Join(root, ".git"))
	if err != nil {
		return root // a directory (the main checkout), or unreadable
	}
	gd := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(b)), "gitdir:"))
	if !filepath.IsAbs(gd) {
		gd = filepath.Join(root, gd)
	}
	sep := string(filepath.Separator)
	if i := strings.Index(gd, sep+".git"+sep+"worktrees"+sep); i > 0 {
		return gd[:i]
	}
	return root
}

// parseWorktrees reads `git worktree list --porcelain`: blocks of lines, the
// main checkout first.
func parseWorktrees(out string) (main Worktree, linked []Worktree, ok bool) {
	var all []Worktree
	var cur *Worktree
	for _, l := range strings.Split(out, "\n") {
		k, v, _ := strings.Cut(l, " ")
		switch k {
		case "worktree":
			all = append(all, Worktree{Path: v})
			cur = &all[len(all)-1]
		case "branch":
			if cur != nil {
				cur.Branch = sp(strings.TrimPrefix(v, "refs/heads/"))
			}
		case "prunable":
			if cur != nil {
				cur.Missing = true
			}
		case "locked":
			if cur != nil {
				cur.Locked = true
			}
		}
	}
	if len(all) == 0 {
		return Worktree{}, nil, false
	}
	return all[0], all[1:], true
}

// repoOf lists one repo's worktrees and the land state of each linked one.
func repoOf(root string) (Repo, bool) {
	out, ok := git(root, "worktree", "list", "--porcelain")
	if !ok {
		return Repo{}, false
	}
	main, linked, ok := parseWorktrees(out)
	if !ok {
		return Repo{}, false
	}
	r := Repo{Root: main.Path, Name: filepath.Base(main.Path), Branch: main.Branch, Worktrees: linked}
	var wg sync.WaitGroup
	for i := range r.Worktrees {
		w := &r.Worktrees[i]
		if w.Missing {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			br := ""
			if w.Branch != nil {
				br = *w.Branch
			}
			w.Land = landOf(w.Path, br, false)
		}()
	}
	wg.Wait()
	sort.SliceStable(r.Worktrees, func(i, j int) bool { return r.Worktrees[i].Path < r.Worktrees[j].Path })
	if r.Worktrees == nil {
		r.Worktrees = []Worktree{}
	}
	return r, true
}

// loadRepos returns the repos under the given roots (main or linked), from
// the cache when fresh is false and the cache is young enough.
func loadRepos(sd string, roots []string, fresh bool) []Repo {
	mains := map[string]bool{}
	for _, r := range roots {
		if r != "" {
			mains[mainRoot(r)] = true
		}
	}
	path := filepath.Join(sd, "worktrees.json")
	var c worktreesCache
	if !fresh {
		if b, err := os.ReadFile(path); err == nil && json.Unmarshal(b, &c) == nil &&
			time.Since(time.Unix(c.At, 0)) < worktreesTTL {
			hit := true
			for m := range mains {
				if _, ok := c.Repos[m]; !ok || c.Stamps[m] != worktreeStamp(m) {
					hit = false
				}
			}
			if hit {
				return pick(c.Repos, mains)
			}
		}
	}
	c = worktreesCache{At: time.Now().Unix(), Repos: map[string]Repo{}, Stamps: map[string]int64{}}
	for m := range mains {
		c.Stamps[m] = worktreeStamp(m) // before reading: a change mid-read re-reads next time
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for m := range mains {
		wg.Add(1)
		go func(m string) {
			defer wg.Done()
			if r, ok := repoOf(m); ok {
				mu.Lock()
				c.Repos[m] = r
				mu.Unlock()
			}
		}(m)
	}
	wg.Wait()
	if b, err := json.Marshal(c); err == nil {
		tmp := path + ".tmp" + strconv.Itoa(os.Getpid())
		if os.WriteFile(tmp, b, 0o600) == nil {
			_ = os.Rename(tmp, path)
		}
	}
	return pick(c.Repos, mains)
}

func pick(all map[string]Repo, mains map[string]bool) []Repo {
	out := []Repo{}
	for m := range mains {
		if r, ok := all[m]; ok {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Root < out[j].Root })
	return out
}

// withRepos adds doc.Repos for every repo a pane or a background agent
// stands in.
func withRepos(doc *Doc, sd string, fresh bool) {
	var roots []string
	if doc.Panes != nil {
		for _, p := range *doc.Panes {
			if p.Git != nil {
				roots = append(roots, p.Git.Root)
			}
		}
	}
	if doc.Background != nil {
		for _, b := range *doc.Background {
			if r, _, ok := repoRoot(b.Cwd); ok {
				roots = append(roots, r)
			}
		}
	}
	rs := loadRepos(sd, roots, fresh)
	sessions, ok := claudeSessions(sd) // cached alongside withClaude's read
	for i := range rs {
		for j := range rs[i].Worktrees {
			w := &rs[i].Worktrees[j]
			if !ok {
				w.Sessions = -1
				continue
			}
			for _, s := range sessions {
				if s.Cwd == w.Path || strings.HasPrefix(s.Cwd, w.Path+string(filepath.Separator)) {
					w.Sessions++
				}
			}
		}
	}
	doc.Repos = &rs
}

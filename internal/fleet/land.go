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

// Land answers "what would it take to land this branch?" — one code naming the
// next action, not a ready/not-ready boolean, because ready/not-ready collapses
// five situations that each need something different done to them.
//
// Precedence, most-blocking first:
//
//	dirty N    uncommitted work      -> commit it
//	unpushed N commits only you have -> push
//	merged     its PR landed         -> sweepable
//	review     PR open               -> wait, or nudge
//	behind N   base moved under you  -> rebase
//	no_pr      pushed, no PR         -> open one
//	clean      nothing to say
//	unknown    gh could not answer the PR half
//
// dirty/unpushed/behind are pure local git, so an unusable token costs the
// three PR states and leaves the rest honest.
type Land struct {
	Code string   `json:"code"`
	N    *float64 `json:"n"`
}

func git(dir string, args ...string) (string, bool) {
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
	if err != nil {
		return "", false
	}
	return strings.TrimRight(string(out), "\n"), true
}

func countLines(s string) float64 {
	if s == "" {
		return 0
	}
	return float64(len(lines(s)))
}

// baseRef is the branch this one is trying to land on.
func baseRef(dir string) string {
	if b, ok := git(dir, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"); ok && b != "" {
		return b
	}
	for _, b := range []string{"origin/main", "origin/master", "main", "master"} {
		if _, ok := git(dir, "rev-parse", "--verify", "--quiet", b); ok {
			return b
		}
	}
	return ""
}

func hasRemote(dir string) bool {
	out, ok := git(dir, "remote")
	return ok && strings.TrimSpace(out) != ""
}

// repoSlug is owner/repo for a GitHub remote, or "" for anything else.
func repoSlug(dir string) string {
	u, ok := git(dir, "remote", "get-url", "origin")
	if !ok {
		return ""
	}
	i := strings.Index(u, "github.com")
	if i < 0 {
		return ""
	}
	r := strings.TrimPrefix(strings.TrimPrefix(u[i+len("github.com"):], ":"), "/")
	r = strings.TrimSuffix(r, ".git")
	if !strings.Contains(r, "/") {
		return ""
	}
	return r
}

type prEntry struct {
	Branch string `json:"branch"`
	Merged bool   `json:"merged"`
	State  string `json:"state"`
}

func prCacheDir() string { return filepath.Join(stateDir(), "pr-cache") }

func prCacheFile(dir string) string {
	slug := repoSlug(dir)
	if slug == "" {
		return ""
	}
	return filepath.Join(prCacheDir(), strings.ReplaceAll(slug, "/", "__")+".json")
}

func prTTL() int64 {
	if v := os.Getenv("KEL_PR_TTL"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	}
	return 300
}

// prRefresh refetches when the cache is stale. One request per repo, not per
// branch: `gh pr status` costs ~2s and answers for one branch, while the pulls
// endpoint costs ~0.6s and answers for all of them.
func prRefresh(dir string) {
	f := prCacheFile(dir)
	if f == "" {
		return
	}
	if b, err := os.ReadFile(f + ".at"); err == nil {
		if ts, err := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64); err == nil {
			if time.Now().Unix()-ts < prTTL() {
				return
			}
		}
	}
	if _, err := exec.LookPath("gh"); err != nil {
		return
	}
	out, err := exec.Command("gh", "api",
		"repos/"+repoSlug(dir)+"/pulls?state=all&per_page=100",
		"--jq", `[.[] | {branch: .head.ref, merged: (.merged_at != null), state: .state}]`,
	).Output()
	if err != nil || len(out) == 0 {
		return
	}
	_ = os.MkdirAll(prCacheDir(), 0o755)
	if err := os.WriteFile(f, out, 0o644); err == nil {
		_ = os.WriteFile(f+".at", []byte(strconv.FormatInt(time.Now().Unix(), 10)+"\n"), 0o644)
	}
}

// prStateOf returns merged | open | none | unknown | na.
// "na" is not "unknown": a repo with no GitHub remote has no PR question, so it
// should read clean rather than as a failure to find out.
func prStateOf(dir, branch string, wantPR bool) string {
	if repoSlug(dir) == "" {
		return "na"
	}
	if branch == "" || branch == "-" {
		return "unknown"
	}
	if wantPR {
		prRefresh(dir)
	}
	f := prCacheFile(dir)
	b, err := os.ReadFile(f)
	if err != nil {
		return "unknown"
	}
	var prs []prEntry
	if json.Unmarshal(b, &prs) != nil {
		return "unknown"
	}
	found, merged, open := false, false, false
	for _, p := range prs {
		if p.Branch != branch {
			continue
		}
		found = true
		if p.Merged {
			merged = true
		}
		if strings.EqualFold(p.State, "open") {
			open = true
		}
	}
	switch {
	case !found:
		return "none"
	case merged:
		return "merged"
	case open:
		return "open"
	}
	return "none"
}

func landOf(dir, branch string, wantPR bool) *Land {
	num := func(n float64) *float64 { return &n }
	if dir == "" {
		return &Land{Code: "clean"}
	}
	if _, ok := git(dir, "rev-parse", "--git-dir"); !ok {
		return &Land{Code: "clean"}
	}
	if branch == "" || branch == "-" {
		branch, _ = git(dir, "rev-parse", "--abbrev-ref", "HEAD")
	}

	if out, ok := git(dir, "status", "--porcelain"); ok {
		if n := countLines(out); n > 0 {
			return &Land{Code: "dirty", N: num(n)}
		}
	}
	if hasRemote(dir) {
		if out, ok := git(dir, "log", "--oneline", "HEAD", "--not", "--remotes"); ok {
			if n := countLines(out); n > 0 {
				return &Land{Code: "unpushed", N: num(n)}
			}
		}
	}

	base := baseRef(dir)
	// Ask git before gh: if HEAD is already an ancestor of the base the work is
	// in, and that holds for a local remote, a non-GitHub remote, and offline.
	// gh only adds the squash-merge case, where the commit is rewritten and the
	// ancestor test cannot see it.
	if base != "" && branch != strings.TrimPrefix(base, "origin/") {
		if exec.Command("git", "-C", dir, "merge-base", "--is-ancestor", "HEAD", base).Run() == nil {
			return &Land{Code: "merged"}
		}
	}

	pr := prStateOf(dir, branch, wantPR)
	switch pr {
	case "merged":
		return &Land{Code: "merged"}
	case "open":
		return &Land{Code: "review"}
	}

	// standing on the base branch itself: nothing to land, and "no PR" would be
	// suggesting an action that does not exist
	if base != "" && branch == strings.TrimPrefix(base, "origin/") {
		return &Land{Code: "clean"}
	}
	if base != "" {
		if out, ok := git(dir, "rev-list", "--count", "HEAD..."+base, "--right-only"); ok {
			if n, err := strconv.ParseFloat(strings.TrimSpace(out), 64); err == nil && n > 0 {
				return &Land{Code: "behind", N: num(n)}
			}
		}
		// nothing ahead of base means there is nothing to land: a freshly
		// created worktree branch should not nag you to open a PR for zero
		// commits
		if out, ok := git(dir, "rev-list", "--count", base+"...HEAD", "--right-only"); ok {
			if n, err := strconv.ParseFloat(strings.TrimSpace(out), 64); err == nil && n == 0 {
				return &Land{Code: "clean"}
			}
		}
	}
	switch pr {
	case "none":
		return &Land{Code: "no_pr"}
	case "unknown":
		return &Land{Code: "unknown"}
	}
	return &Land{Code: "clean"}
}

// landMap classifies each distinct worktree once, concurrently.
func landMap(records []record, wantPR bool) map[string]*Land {
	type job struct{ cwd, branch string }
	seen := map[string]job{}
	for _, r := range records {
		if r.Cwd == nil || *r.Cwd == "" {
			continue
		}
		if _, ok := seen[*r.Cwd]; ok {
			continue
		}
		br := ""
		if r.Branch != nil {
			br = *r.Branch
		}
		seen[*r.Cwd] = job{*r.Cwd, br}
	}
	// refresh each repo's PR cache once, before fanning out, so N worktrees of
	// one repo make one request rather than racing N of them
	if wantPR {
		done := map[string]bool{}
		for _, j := range seen {
			if s := repoSlug(j.cwd); s != "" && !done[s] {
				done[s] = true
				prRefresh(j.cwd)
			}
		}
	}
	out := map[string]*Land{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, j := range seen {
		wg.Add(1)
		go func(j job) {
			defer wg.Done()
			l := landOf(j.cwd, j.branch, false) // cache is already warm
			mu.Lock()
			out[j.cwd] = l
			mu.Unlock()
		}(j)
	}
	wg.Wait()
	return out
}

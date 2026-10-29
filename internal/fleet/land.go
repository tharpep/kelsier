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

// BaseRef is the branch this one is trying to land on; "" when there is none.
// Exported for the sidebar's `d`, which diffs against it.
func BaseRef(dir string) string { return baseRef(dir) }

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

// A PR is matched by its head commit, not its branch name. The local branch
// need not be called what the PR's head is: `gh pr checkout` and Claude Code's
// worktrees name it `pr-147` or `worktree-*`, so a squash-merged PR read as
// "behind its base" — the squash puts one commit on base the branch lacks
// (2026-10-20). A commit made after the merge is new work, and stops matching.
type prEntry struct {
	SHA    string `json:"sha"`
	Merged bool   `json:"merged"`
	State  string `json:"state"`
}

func prCacheDir() string { return filepath.Join(StateDir(), "pr-cache") }

func prCacheFile(dir string) string {
	slug := repoSlug(dir)
	if slug == "" {
		return ""
	}
	// .sha.json: the head-commit cache. The branch-keyed one it replaced would
	// parse with no sha, match nothing, and read "none" until it expired.
	return filepath.Join(prCacheDir(), strings.ReplaceAll(slug, "/", "__")+".sha.json")
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
//
// `pr = off` (KEL_PR, exported by bin/kel's config loader) stops it: this is
// the one call in kel that leaves the machine.  Only the bash reader used to
// honour it, so with kel-fleet installed the setting silently did nothing —
// invisible until the bash reader was deleted and its test had to move here.
func prRefresh(dir string) {
	switch os.Getenv("KEL_PR") {
	case "0", "off", "no", "false":
		return
	}
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
	// --paginate: one page of 100 cut a real repo's 149 PRs short, so an older
	// one read "none". The filter runs per page and --slurp refuses --jq, so
	// it prints one PR per line and they are collected here.
	out, err := exec.Command("gh", "api", "--paginate",
		"repos/"+repoSlug(dir)+"/pulls?state=all&per_page=100",
		"--jq", `.[] | {sha: .head.sha, merged: (.merged_at != null), state: .state}`,
	).Output()
	if err != nil {
		return
	}
	prs := []prEntry{}
	for _, l := range strings.Split(string(out), "\n") {
		var p prEntry
		if json.Unmarshal([]byte(l), &p) == nil && p.SHA != "" {
			prs = append(prs, p)
		}
	}
	if out, err = json.Marshal(prs); err != nil {
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
func prStateOf(dir string, wantPR bool) string {
	if repoSlug(dir) == "" {
		return "na"
	}
	head, ok := git(dir, "rev-parse", "HEAD")
	if !ok || head == "" {
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
		if p.SHA != head {
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

// mergedPRHeads returns the head SHAs of merged PRs found in the repo's PR cache.
func mergedPRHeads(dir string) []string {
	f := prCacheFile(dir)
	if f == "" {
		return nil
	}
	b, err := os.ReadFile(f)
	if err != nil {
		return nil
	}
	var prs []prEntry
	if json.Unmarshal(b, &prs) != nil {
		return nil
	}
	var shas []string
	seen := map[string]bool{}
	for _, p := range prs {
		if p.Merged && p.SHA != "" && !seen[p.SHA] {
			seen[p.SHA] = true
			shas = append(shas, p.SHA)
		}
	}
	return shas
}

// localCommits filters shas down to those present in the local repository as commit objects.
func localCommits(dir string, shas []string) []string {
	if len(shas) == 0 {
		return nil
	}
	cmd := exec.Command("git", "-C", dir, "cat-file", "--batch-check=%(objectname) %(objecttype)")
	cmd.Stdin = strings.NewReader(strings.Join(shas, "\n") + "\n")
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	var local []string
	for _, line := range strings.Split(string(out), "\n") {
		parts := strings.Fields(line)
		if len(parts) == 2 && parts[1] == "commit" {
			local = append(local, parts[0])
		}
	}
	return local
}

// mergedByPR returns true if every commit on HEAD is reachable from base or
// from some merged PR head that is a local object (Issue #2).
func mergedByPR(dir, base string) bool {
	shas := localCommits(dir, mergedPRHeads(dir))
	if len(shas) == 0 {
		return false
	}
	args := []string{"rev-list", "--count", "HEAD", "--not"}
	if base != "" {
		args = append(args, base)
	}
	args = append(args, shas...)
	out, ok := git(dir, args...)
	if !ok {
		return false
	}
	return strings.TrimSpace(out) == "0"
}

// localState is what the working tree alone can say: is it a repo, how many
// files are uncommitted, how many commits are on no remote. One read serves
// the dirty count, the two land codes that need no PR, and the privacy
// check that keeps such work from ever reaching gh — three readers that each
// ran their own `git status` before.
type localState struct {
	repo            bool
	dirty, unpushed float64
}

func local(dir string) localState {
	if dir == "" {
		return localState{}
	}
	if _, ok := git(dir, "rev-parse", "--git-dir"); !ok {
		return localState{}
	}
	ls := localState{repo: true}
	if out, ok := git(dir, "status", "--porcelain"); ok {
		ls.dirty = countLines(out)
	}
	if ls.dirty == 0 && hasRemote(dir) {
		shas := localCommits(dir, mergedPRHeads(dir))
		args := []string{"log", "--oneline", "HEAD", "--not", "--remotes"}
		args = append(args, shas...)
		if out, ok := git(dir, args...); ok {
			ls.unpushed = countLines(out)
		}
	}
	return ls
}

func landOf(dir, branch string, wantPR bool) *Land {
	if wantPR {
		prRefresh(dir)
	}
	return landFrom(dir, branch, wantPR, local(dir))
}

func landFrom(dir, branch string, wantPR bool, ls localState) *Land {
	num := func(n float64) *float64 { return &n }
	if !ls.repo {
		return &Land{Code: "clean"}
	}
	if branch == "" || branch == "-" {
		branch, _ = git(dir, "rev-parse", "--abbrev-ref", "HEAD")
	}
	if ls.dirty > 0 {
		return &Land{Code: "dirty", N: num(ls.dirty)}
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
	if (base == "" || branch != strings.TrimPrefix(base, "origin/")) && mergedByPR(dir, base) {
		return &Land{Code: "merged"}
	}

	if ls.unpushed > 0 {
		return &Land{Code: "unpushed", N: num(ls.unpushed)}
	}

	pr := prStateOf(dir, wantPR)
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

// landMap classifies each distinct worktree once, concurrently, and returns
// what the working tree said of each so the dirty count need not ask again.
func landMap(records []record, wantPR bool) (map[string]*Land, map[string]localState) {
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
	//
	// A worktree with uncommitted or unpushed work is answered by local git
	// alone (dirty / unpushed outrank every PR state), so it must not be the
	// reason kel talks to github.com — spec.md promises that, and only the
	// bash reader used to keep it.
	locals := map[string]localState{}
	for _, j := range seen {
		locals[j.cwd] = local(j.cwd)
	}
	if wantPR {
		done := map[string]bool{}
		for _, j := range seen {
			ls := locals[j.cwd]
			if s := repoSlug(j.cwd); s != "" && !done[s] && ls.dirty == 0 && ls.unpushed == 0 {
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
			l := landFrom(j.cwd, j.branch, false, locals[j.cwd]) // cache is already warm
			mu.Lock()
			out[j.cwd] = l
			mu.Unlock()
		}(j)
	}
	wg.Wait()
	return out, locals
}

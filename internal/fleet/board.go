package fleet

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Diff is an agent's checkout measured against the branch it would land on:
// `git diff --shortstat <merge base>`, committed and uncommitted together.
// Untracked files are not counted, as git does not count them.
type Diff struct {
	Added   int `json:"added"`
	Removed int `json:"removed"`
}

// PR is the pull request an agent's branch heads. CI is null when kel could
// not ask (gh failed, or `pr = off`), never a guess.
type PR struct {
	Number int     `json:"number"`
	State  string  `json:"state"` // open | merged | closed
	CI     *string `json:"ci"`    // pass | fail | pending
}

// Transition is one line of <pane-id>.log: when the agent entered a state,
// and the note it entered it with.
type Transition struct {
	At    float64 `json:"at"`
	State string  `json:"state"`
	Note  string  `json:"note,omitempty"`
}

// withBoard fills the board's facts (Options.Board): each agent's diff, PR
// and CI, state history and the last lines of its pane. Only the board asks,
// so none of it is on the bar's redraw path (invariant 5).
func withBoard(doc *Doc, sd string) {
	roots := map[string]string{}
	if doc.Panes != nil {
		for _, p := range *doc.Panes {
			if p.Git != nil {
				roots[p.PaneID] = p.Git.Root
			}
		}
	}
	var wg sync.WaitGroup
	for i := range doc.Agents {
		a := &doc.Agents[i]
		if a.PaneID == nil {
			continue
		}
		pane := *a.PaneID
		a.History = readHistory(filepath.Join(sd, pane+".log"))
		wg.Add(1)
		go func() {
			defer wg.Done()
			a.Screen = captureScreen(pane)
			if root := roots[pane]; root != "" {
				a.Diff = diffOf(root)
				a.PR = prOf(root)
			}
		}()
	}
	wg.Wait()
}

var boardStates = map[string]bool{
	"idle": true, "working": true, "waiting": true, "done": true,
	"throttled": true, "error": true, "dead": true,
}

// readHistory parses <pane-id>.log, `<epoch> <event> <state> <note>` per
// line. No file is null: the log is cleared with each new session, so kel
// cannot say what came before.
func readHistory(path string) *[]Transition {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	ts := parseHistory(string(b))
	return &ts
}

func parseHistory(s string) []Transition {
	ts := []Transition{}
	for _, l := range lines(s) {
		f := strings.SplitN(l, " ", 4)
		if len(f) < 3 || !boardStates[f[2]] {
			continue
		}
		at, err := strconv.ParseFloat(f[0], 64)
		if err != nil {
			continue
		}
		t := Transition{At: at, State: f[2]}
		if len(f) == 4 {
			t.Note = strings.TrimSpace(f[3])
		}
		ts = append(ts, t)
	}
	return ts
}

// captureScreen is the pane's last five lines that hold anything. Reading a
// pane never types into it (invariant 4).
func captureScreen(pane string) *[]string {
	out, err := exec.Command("tmux", "capture-pane", "-p", "-J", "-t", pane).Output()
	if err != nil {
		return nil
	}
	ls := strings.Split(strings.TrimRight(string(out), " \t\n"), "\n")
	for i := range ls {
		ls[i] = strings.TrimRight(ls[i], " ")
	}
	ls = ls[max(0, len(ls)-5):]
	return &ls
}

var shortstatRe = regexp.MustCompile(`(\d+) (insertion|deletion)`)

func parseShortstat(s string) Diff {
	var d Diff
	for _, m := range shortstatRe.FindAllStringSubmatch(s, -1) {
		n, _ := strconv.Atoi(m[1])
		if m[2] == "insertion" {
			d.Added = n
		} else {
			d.Removed = n
		}
	}
	return d
}

type diffMemo struct {
	at time.Time
	d  *Diff
}

var (
	diffMu    sync.Mutex
	diffCache = map[string]diffMemo{}
)

// diffFor is how long a measured diff is reused: the board reloads every two
// seconds, and two git calls per worktree each time would be most of it.
const diffFor = 10 * time.Second

// diffOf is the checkout against the merge base with the branch it lands on.
// No base, or a git that fails, is null.
func diffOf(root string) *Diff {
	diffMu.Lock()
	m, ok := diffCache[root]
	diffMu.Unlock()
	if ok && time.Since(m.at) < diffFor {
		return m.d
	}
	var d *Diff
	if base := baseRef(root); base != "" {
		if mb, ok := git(root, "merge-base", "HEAD", base); ok && mb != "" {
			if out, ok := git(root, "diff", "--shortstat", mb); ok {
				v := parseShortstat(out)
				d = &v
			}
		}
	}
	diffMu.Lock()
	diffCache[root] = diffMemo{time.Now(), d}
	diffMu.Unlock()
	return d
}

// prOf is the PR whose head is this checkout's HEAD, from pr-cache. A cache
// past its TTL starts a refresh off the load's path (refreshPRLater) and is
// read as it stands meanwhile: the board never waits on the network.
func prOf(root string) *PR {
	f := prCacheFile(root)
	if f == "" {
		return nil
	}
	refreshPRLater(root, f)
	head, ok := git(root, "rev-parse", "HEAD")
	if !ok || head == "" {
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
	p := matchPR(prs, head)
	if p == nil {
		return nil
	}
	if p.State == "open" {
		p.CI = ciOf(root, p.Number, head)
	}
	return p
}

// StartPRRefresh runs RefreshPR away from the caller: a goroutine in the
// long-lived board, a detached `kel-fleet --refresh-pr` from the CLI, which
// exits before a goroutine would finish (cmd/kel-fleet).
var StartPRRefresh = func(dir string) { go RefreshPR(dir) }

// prRefreshing is how long a started refresh holds off another: one gh call
// per repo however many agents and boards read it, and a gh that failed is
// asked again after it, not on every reload.
const prRefreshing = 60

// RefreshPR refetches dir's pr-cache when it is stale, as the --pr path
// does, and lets the next stale read start another.
func RefreshPR(dir string) {
	prRefresh(dir)
	if f := prCacheFile(dir); f != "" {
		_ = os.Remove(f + ".refreshing")
	}
}

// refreshPRLater starts a refresh of a pr-cache past its TTL. Never under
// pr = off, never twice at once (the .refreshing epoch), and never for a
// checkout with uncommitted files: work nobody has committed must not be
// the reason kel talks to github.com, as landMap keeps it.
func refreshPRLater(root, f string) {
	if prOff() {
		return
	}
	now := time.Now().Unix()
	if epochIn(f+".at") > now-prTTL() || epochIn(f+".refreshing") > now-prRefreshing {
		return
	}
	if local(root).dirty != 0 {
		return
	}
	_ = os.MkdirAll(prCacheDir(), 0o755)
	WriteAtomic(f+".refreshing", []byte(strconv.FormatInt(now, 10)+"\n"))
	StartPRRefresh(root)
}

// epochIn is the epoch a one-line file holds; 0 when it is absent or junk.
func epochIn(path string) int64 {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	n, _ := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
	return n
}

// matchPR is the PR headed by sha. A cache written before kel kept the
// number has none, and reads as no PR until it is refetched.
func matchPR(prs []prEntry, sha string) *PR {
	for _, e := range prs {
		if e.SHA != sha || e.Number == 0 {
			continue
		}
		st := strings.ToLower(e.State)
		if e.Merged {
			st = "merged"
		}
		return &PR{Number: e.Number, State: st}
	}
	return nil
}

type ciEntry struct {
	SHA    string  `json:"sha"`
	CI     *string `json:"ci"`
	At     int64   `json:"at"`
	Failed bool    `json:"failed,omitempty"`
}

// ciRetry is how soon a gh that failed is asked again: its answer stays
// null meanwhile, and the board does not spend a call on every reload.
const ciRetry = 30

func ciCacheFile(root string) string {
	f := prCacheFile(root)
	if f == "" {
		return ""
	}
	return strings.TrimSuffix(f, ".sha.json") + ".ci.json"
}

// ciOf is the PR's checks rolled into one word, from `gh pr checks`, cached
// per PR and head commit for the PR cache's TTL. `pr = off` asks nothing; a
// failing gh is null and is not cached, so the next load asks again.
func ciOf(root string, number int, sha string) *string {
	switch os.Getenv("KEL_PR") {
	case "0", "off", "no", "false":
		return nil
	}
	f := ciCacheFile(root)
	cache := map[string]ciEntry{}
	if b, err := os.ReadFile(f); err == nil {
		_ = json.Unmarshal(b, &cache)
	}
	key := strconv.Itoa(number)
	now := time.Now().Unix()
	if e, ok := cache[key]; ok && e.SHA == sha {
		if ttl := map[bool]int64{false: prTTL(), true: ciRetry}[e.Failed]; now-e.At < ttl {
			return e.CI
		}
	}
	if _, err := exec.LookPath("gh"); err != nil {
		return nil
	}
	var stderr strings.Builder
	cmd := exec.Command("gh", "pr", "checks", key, "--repo", repoSlug(root), "--json", "bucket")
	cmd.Stderr = &stderr
	out, _ := cmd.Output()
	var checks []struct {
		Bucket string `json:"bucket"`
	}
	e := ciEntry{SHA: sha, At: now}
	switch {
	case json.Unmarshal(out, &checks) == nil:
		e.CI = rollup(checks)
	case strings.Contains(stderr.String(), "no checks reported"):
	default:
		e.Failed = true
	}
	cache[key] = e
	ci := e.CI
	if b, err := json.Marshal(cache); err == nil {
		_ = os.MkdirAll(prCacheDir(), 0o755)
		WriteAtomic(f, b)
	}
	return ci
}

// rollup: any failed or cancelled check fails the PR, any still running
// leaves it pending, and no checks at all says nothing.
func rollup(checks []struct {
	Bucket string `json:"bucket"`
}) *string {
	if len(checks) == 0 {
		return nil
	}
	word := "pass"
	for _, c := range checks {
		switch c.Bucket {
		case "fail", "cancel":
			return sp("fail")
		case "pending":
			word = "pending"
		}
	}
	return &word
}

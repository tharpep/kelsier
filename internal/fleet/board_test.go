package fleet

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestParseHistory(t *testing.T) {
	got := parseHistory("100 SessionStart idle \n160 UserPromptSubmit working \nnot a line\n200 Notification waiting allow Bash? npm test\n210 ClaudeAgents bogus x\n")
	if len(got) != 3 || got[2].State != "waiting" || got[2].Note != "allow Bash? npm test" || got[1].At != 160 {
		t.Errorf("parsed %+v", got)
	}
	if h := readHistory(filepath.Join(t.TempDir(), "%9.log")); h != nil {
		t.Errorf("no log reads %v, want null: kel cannot say what came before", *h)
	}
}

func TestParseShortstat(t *testing.T) {
	for in, want := range map[string]Diff{
		" 3 files changed, 214 insertions(+), 37 deletions(-)": {214, 37},
		" 1 file changed, 1 insertion(+)":                      {1, 0},
		" 2 files changed, 9 deletions(-)":                     {0, 9},
		"":                                                     {0, 0},
	} {
		if got := parseShortstat(in); got != want {
			t.Errorf("%q: %+v, want %+v", in, got, want)
		}
	}
}

func TestMatchPR(t *testing.T) {
	prs := []prEntry{
		{Number: 0, SHA: "old", State: "open"},
		{Number: 61, SHA: "abc", State: "open"},
		{Number: 57, SHA: "def", State: "closed", Merged: true},
	}
	if p := matchPR(prs, "abc"); p == nil || p.Number != 61 || p.State != "open" {
		t.Errorf("abc: %+v", p)
	}
	if p := matchPR(prs, "def"); p == nil || p.State != "merged" {
		t.Errorf("def: %+v", p)
	}
	if p := matchPR(prs, "old"); p != nil {
		t.Errorf("a cache entry with no number read as PR %+v", p)
	}
	if p := matchPR(prs, "zzz"); p != nil {
		t.Errorf("no PR read as %+v", p)
	}
}

func TestRollup(t *testing.T) {
	type c = struct {
		Bucket string `json:"bucket"`
	}
	for _, tc := range []struct {
		in   []c
		want string
	}{
		{[]c{{"pass"}, {"skipping"}}, "pass"},
		{[]c{{"pass"}, {"pending"}}, "pending"},
		{[]c{{"pending"}, {"fail"}}, "fail"},
		{[]c{{"cancel"}}, "fail"},
		{nil, ""},
	} {
		got := ""
		if r := rollup(tc.in); r != nil {
			got = *r
		}
		if got != tc.want {
			t.Errorf("%v: %q, want %q", tc.in, got, tc.want)
		}
	}
}

func ghRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "--allow-empty", "-m", "i"},
		{"remote", "add", "origin", "https://github.com/example/r.git"},
	} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return dir
}

func stubGH(t *testing.T, script string) string {
	t.Helper()
	bin := t.TempDir()
	calls := filepath.Join(bin, "calls")
	stub := "#!/bin/sh\necho x >> " + calls + "\n" + script
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return calls
}

func callCount(path string) int {
	b, _ := os.ReadFile(path)
	return strings.Count(string(b), "x")
}

// Invariant 3: a gh that fails leaves CI null, never pass; it is asked again
// after a short wait, not on every reload. pr = off asks nothing.
func TestCIFailingGHReadsUnknown(t *testing.T) {
	dir := ghRepo(t)
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	calls := stubGH(t, "echo 'HTTP 401' >&2\nexit 1\n")
	t.Setenv("KEL_PR", "")
	if ci := ciOf(dir, 7, "abc"); ci != nil {
		t.Errorf("a failing gh read %q", *ci)
	}
	if ci := ciOf(dir, 7, "abc"); ci != nil || callCount(calls) != 1 {
		t.Errorf("second read: ci %v after %d calls, want null after 1", ci, callCount(calls))
	}
	t.Setenv("KEL_PR", "off")
	if ci := ciOf(dir, 8, "abc"); ci != nil || callCount(calls) != 1 {
		t.Errorf("pr = off: ci %v, %d calls", ci, callCount(calls))
	}
}

func TestCIFromChecks(t *testing.T) {
	dir := ghRepo(t)
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("KEL_PR", "")
	calls := stubGH(t, "echo '[{\"bucket\":\"pass\"},{\"bucket\":\"pending\"}]'\nexit 8\n")
	if ci := ciOf(dir, 7, "abc"); ci == nil || *ci != "pending" {
		t.Fatalf("ci %v, want pending", ci)
	}
	if ci := ciOf(dir, 7, "abc"); ci == nil || *ci != "pending" || callCount(calls) != 1 {
		t.Errorf("cached read: %v after %d calls", ci, callCount(calls))
	}
	if ci := ciOf(dir, 7, "new-head"); ci == nil || callCount(calls) != 2 {
		t.Errorf("a new head commit did not ask again: %d calls", callCount(calls))
	}
}

func TestDiffAgainstTheMergeBase(t *testing.T) {
	dir := ghRepo(t)
	run := func(args ...string) {
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("update-ref", "refs/remotes/origin/main", "HEAD")
	run("checkout", "-q", "-b", "feat")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("1\n2\n3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "a.txt")
	run("-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "-m", "a")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("1\n2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if d := diffOf(dir); d == nil || *d != (Diff{Added: 2, Removed: 0}) {
		t.Errorf("diff %+v, want +2 −0 (committed and uncommitted together)", d)
	}
	if d := diffOf(t.TempDir()); d != nil {
		t.Errorf("no repo read %+v, want null", *d)
	}
}

// A pr-cache past its TTL starts one refresh off the board's path, and none
// under pr = off, while one is running, while the cache is fresh, or for a
// checkout with uncommitted work.
func TestBoardRefreshesAStalePRCacheLater(t *testing.T) {
	dir := ghRepo(t)
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("KEL_PR", "")
	var started []string
	old := StartPRRefresh
	StartPRRefresh = func(d string) { started = append(started, d) }
	t.Cleanup(func() { StartPRRefresh = old })
	f := prCacheFile(dir)

	t.Setenv("KEL_PR", "off")
	prOf(dir)
	if len(started) != 0 {
		t.Fatal("pr = off started a refresh")
	}
	t.Setenv("KEL_PR", "")
	prOf(dir)
	prOf(dir)
	if len(started) != 1 || started[0] != dir {
		t.Fatalf("a stale cache started %v, want one refresh of %s", started, dir)
	}
	if _, err := os.Stat(f + ".refreshing"); err != nil {
		t.Errorf("no .refreshing marker: %v", err)
	}
	_ = os.Remove(f + ".refreshing")
	WriteAtomic(f+".at", []byte(strconv.FormatInt(time.Now().Unix(), 10)+"\n"))
	prOf(dir)
	if len(started) != 1 {
		t.Error("a fresh cache started a refresh")
	}
	_ = os.Remove(f + ".at")
	if err := os.WriteFile(filepath.Join(dir, "wip.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	prOf(dir)
	if len(started) != 1 {
		t.Error("uncommitted work started a refresh: it must never be why kel calls gh")
	}
}

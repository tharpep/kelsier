package fleet

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestParseWorktrees(t *testing.T) {
	out := "worktree /r\nHEAD aaa\nbranch refs/heads/main\n\n" +
		"worktree /r/.claude/worktrees/x\nHEAD bbb\nbranch refs/heads/worktree-x\n\n" +
		"worktree /gone\nHEAD ccc\ndetached\nprunable gitdir file points to non-existent location\n\n" +
		"worktree /held\nHEAD ddd\nbranch refs/heads/held\nlocked\n"
	main, linked, ok := parseWorktrees(out)
	if !ok || main.Path != "/r" || *main.Branch != "main" || len(linked) != 3 {
		t.Fatalf("main %+v linked %d", main, len(linked))
	}
	if *linked[0].Branch != "worktree-x" || linked[1].Branch != nil || !linked[1].Missing || !linked[2].Locked {
		t.Errorf("linked: %+v", linked)
	}
	if _, _, ok := parseWorktrees(""); ok {
		t.Error("nothing is not a repo")
	}
}

// Every worktree is found from any of them, and a fresh one is merged — there
// is nothing in it the base lacks — while one with a commit of its own is not.
func TestLoadRepos(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	d, _ := filepath.EvalSymlinks(t.TempDir())
	r := filepath.Join(d, "repo")
	run := func(dir string, a ...string) {
		c := exec.Command("git", append([]string{"-c", "user.email=t@t", "-c", "user.name=t"}, a...)...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", a, err, out)
		}
	}
	_ = os.MkdirAll(r, 0o755)
	run(r, "init", "-q", "-b", "main")
	_ = os.WriteFile(filepath.Join(r, "f"), []byte("a"), 0o644)
	run(r, "add", "-A")
	run(r, "commit", "-qm", "i")
	run(r, "worktree", "add", "-q", "-b", "fresh", filepath.Join(d, "fresh"))
	run(r, "worktree", "add", "-q", "-b", "work", filepath.Join(d, "work"))
	_ = os.WriteFile(filepath.Join(d, "work", "g"), []byte("b"), 0o644)
	run(filepath.Join(d, "work"), "add", "-A")
	run(filepath.Join(d, "work"), "commit", "-qm", "w")

	if mainRoot(filepath.Join(d, "work")) != r {
		t.Errorf("mainRoot(work) = %q", mainRoot(filepath.Join(d, "work")))
	}
	rs := loadRepos(d, []string{filepath.Join(d, "work")}, true)
	if len(rs) != 1 || rs[0].Root != r || len(rs[0].Worktrees) != 2 {
		t.Fatalf("repos: %+v", rs)
	}
	got := map[string]string{}
	for _, w := range rs[0].Worktrees {
		got[*w.Branch] = w.Land.Code
	}
	if got["fresh"] != "merged" || got["work"] == "merged" {
		t.Errorf("land: %v", got)
	}
	// the cache answers the second ask, the same
	if again := loadRepos(d, []string{r}, false); len(again) != 1 || len(again[0].Worktrees) != 2 {
		t.Errorf("cached: %+v", again)
	}
}

// A worktree added inside the cache's TTL is seen at once.
func TestReposCacheSeesNewWorktree(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	d, _ := filepath.EvalSymlinks(t.TempDir())
	r := filepath.Join(d, "repo")
	run := func(a ...string) {
		c := exec.Command("git", append([]string{"-c", "user.email=t@t", "-c", "user.name=t"}, a...)...)
		c.Dir = r
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", a, err, out)
		}
	}
	_ = os.MkdirAll(r, 0o755)
	run("init", "-q", "-b", "main")
	run("commit", "-q", "--allow-empty", "-m", "i")
	run("worktree", "add", "-q", "-b", "a", filepath.Join(d, "a"))
	if rs := loadRepos(d, []string{r}, false); len(rs[0].Worktrees) != 1 {
		t.Fatalf("first read: %+v", rs)
	}
	run("worktree", "add", "-q", "-b", "b", filepath.Join(d, "b"))
	if rs := loadRepos(d, []string{r}, false); len(rs[0].Worktrees) != 2 {
		t.Errorf("a cached read hid the new worktree: %+v", rs[0].Worktrees)
	}
}

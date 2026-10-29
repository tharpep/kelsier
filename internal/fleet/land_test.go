package fleet

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The two land cases test/kel-test.sh cannot reach: a kel agent is always
// started inside a real directory, so "not a repo" and "gone" only happen to
// a record whose directory changed under it.  Both must read clean, not
// error and not guess.
func TestLandOutsideARepo(t *testing.T) {
	if l := landOf(t.TempDir(), "", false); l.Code != "clean" {
		t.Errorf("non-git dir: got %q, want clean", l.Code)
	}
	if l := landOf(filepath.Join(t.TempDir(), "nope"), "", false); l.Code != "clean" {
		t.Errorf("missing dir: got %q, want clean", l.Code)
	}
}

// pr = off must stop the one network call: prRefresh returns before it looks
// gh up at all, so a gh that would record being called never is.
func TestPROffMakesNoCall(t *testing.T) {
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
	bin := t.TempDir()
	calls := filepath.Join(bin, "calls")
	stub := "#!/bin/sh\necho x >> " + calls + "\nexit 1\n"
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	t.Setenv("KEL_PR", "off")
	prRefresh(dir)
	if _, err := os.Stat(calls); err == nil {
		t.Fatal("KEL_PR=off still called gh")
	}
	t.Setenv("KEL_PR", "")
	prRefresh(dir)
	if _, err := os.Stat(calls); err != nil {
		t.Fatal("the stub was never called with pr on — the test proves nothing")
	}
}

// A squash-merged PR reads merged when the local branch is named something
// else than the PR's head (`gh pr checkout` gives `pr-147`, Claude Code's
// worktrees `worktree-*`). The squash leaves one commit on base the branch
// lacks, so a branch-name lookup missed the PR and the row read "behind 1".
func TestSquashMergedUnderAnotherName(t *testing.T) {
	origin, dir := t.TempDir(), t.TempDir()
	run := func(d string, args ...string) string {
		t.Helper()
		out, err := exec.Command("git", append([]string{"-C", d, "-c", "user.email=t@t", "-c", "user.name=t"}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return string(out)
	}
	run(origin, "init", "-q", "--bare", "-b", "main")
	run(dir, "init", "-q", "-b", "main")
	run(dir, "commit", "-q", "--allow-empty", "-m", "base")
	run(dir, "remote", "add", "origin", origin)
	run(dir, "push", "-q", "origin", "main")
	run(dir, "checkout", "-q", "-b", "pr-147")
	run(dir, "commit", "-q", "--allow-empty", "-m", "the work")
	run(dir, "push", "-q", "origin", "pr-147:feat/release-management")
	run(dir, "branch", "-q", "--set-upstream-to", "origin/feat/release-management")
	// the squash: a new commit on main holding the work, not the branch's own
	run(dir, "checkout", "-q", "main")
	run(dir, "commit", "-q", "--allow-empty", "-m", "the work (#147)")
	run(dir, "push", "-q", "origin", "main")
	run(dir, "checkout", "-q", "pr-147")
	run(dir, "remote", "set-url", "origin", "https://github.com/example/r.git")
	run(dir, "remote", "set-url", "--push", "origin", origin)
	head := run(dir, "rev-parse", "HEAD")
	head = head[:len(head)-1]

	bin := t.TempDir()
	stub := "#!/bin/sh\nprintf '%s\\n' '{\"sha\":\"" + head + "\",\"merged\":true,\"state\":\"closed\"}'\n"
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("KEL_PR", "")

	if l := landOf(dir, "pr-147", true); l.Code != "merged" {
		t.Fatalf("squash-merged under another name: got %q, want merged", l.Code)
	}
}

// Issue #2: A branch whose commits are ancestors of a merged PR head reads
// merged, even when the remote head branch was deleted on merge (so local git
// sees them on no remote tracking ref) and even when the squash merge gave
// main a different SHA.
func TestSquashMergedAncestorReachable(t *testing.T) {
	origin, dir := t.TempDir(), t.TempDir()
	run := func(d string, args ...string) string {
		t.Helper()
		out, err := exec.Command("git", append([]string{"-C", d, "-c", "user.email=t@t", "-c", "user.name=t"}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return string(out)
	}
	run(origin, "init", "-q", "--bare", "-b", "main")
	run(dir, "init", "-q", "-b", "main")
	run(dir, "commit", "-q", "--allow-empty", "-m", "base")
	run(dir, "remote", "add", "origin", origin)
	run(dir, "push", "-q", "origin", "main")

	run(dir, "checkout", "-q", "-b", "feat")
	run(dir, "commit", "-q", "--allow-empty", "-m", "commit 1")
	c1 := strings.TrimSpace(run(dir, "rev-parse", "HEAD"))
	run(dir, "commit", "-q", "--allow-empty", "-m", "commit 2")
	prHead := strings.TrimSpace(run(dir, "rev-parse", "HEAD"))

	// Push to origin then squash-merge onto main
	run(dir, "push", "-q", "origin", "feat")
	run(dir, "checkout", "-q", "main")
	run(dir, "commit", "-q", "--allow-empty", "-m", "feat squashed (#2)")
	run(dir, "push", "-q", "origin", "main")

	// Delete remote feat branch (GitHub "delete head branch on merge")
	run(dir, "push", "-q", "origin", "--delete", "feat")

	// Reset local branch to c1 (local branch left behind the PR head)
	run(dir, "checkout", "-q", "feat")
	run(dir, "reset", "-q", "--hard", c1)
	run(dir, "remote", "set-url", "origin", "https://github.com/example/r.git")
	run(dir, "remote", "set-url", "--push", "origin", origin)

	// Stub gh returning the merged PR head
	bin := t.TempDir()
	stub := "#!/bin/sh\nprintf '%s\\n' '{\"sha\":\"" + prHead + "\",\"merged\":true,\"state\":\"closed\"}'\n"
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("KEL_PR", "")

	// With PR refresh, c1 is an ancestor of prHead which is merged, so it reads merged
	if l := landOf(dir, "feat", true); l.Code != "merged" {
		t.Fatalf("ancestor of merged PR head: got %q, want merged", l.Code)
	}

	// An unmerged commit made after or draft amended must NOT read merged
	run(dir, "commit", "-q", "--allow-empty", "-m", "unmerged work")
	if l := landOf(dir, "feat", false); l.Code != "unpushed" || l.N == nil || *l.N != 1 {
		t.Fatalf("unmerged work: got %q (n=%v), want unpushed 1", l.Code, l.N)
	}

	// A branch with a new commit not in any merged PR reads unpushed
	run(dir, "checkout", "-q", "-b", "other", "main")
	run(dir, "commit", "-q", "--allow-empty", "-m", "other commit")
	if l := landOf(dir, "other", false); l.Code != "unpushed" || l.N == nil || *l.N != 1 {
		t.Fatalf("other work: got %q (n=%v), want unpushed 1", l.Code, l.N)
	}
}

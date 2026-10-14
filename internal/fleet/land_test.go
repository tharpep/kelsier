package fleet

import (
	"os"
	"os/exec"
	"path/filepath"
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

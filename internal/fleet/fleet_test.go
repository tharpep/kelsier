package fleet

import (
	"os"
	"path/filepath"
	"testing"
)

// repoGroup is how an unmanaged window (a hand-made `prefix c`) gets a group
// now that the one kel session no longer names it.  It must agree with
// bin/kel's group_for_cwd on every case that function handles.
func TestRepoGroup(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "api-gw")
	deep := filepath.Join(repo, "svc", "deep")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}

	cases := []struct{ name, dir, want string }{
		{"the repo root", repo, "api-gw"},
		{"a subdirectory", deep, "api-gw"},
		{"outside any repo", root, "misc"},
		{"no directory at all", "", "misc"},
		{"a directory that is gone", filepath.Join(root, "nope"), "misc"},
	}
	for _, c := range cases {
		if got := repoGroup(c.dir); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}

	// .kel/group pins a directory and everything under it
	pin := filepath.Join(repo, "svc", ".kel")
	if err := os.MkdirAll(pin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pin, "group"), []byte("  billing \r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := repoGroup(deep); got != "billing" {
		t.Errorf(".kel/group under the repo: got %q, want billing", got)
	}
	if got := repoGroup(repo); got != "api-gw" {
		t.Errorf("a sibling of the pinned dir: got %q, want api-gw", got)
	}
	// a non-slug is ignored, not truncated
	if err := os.WriteFile(filepath.Join(pin, "group"), []byte("not a slug!"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := repoGroup(deep); got != "api-gw" {
		t.Errorf("a non-slug .kel/group: got %q, want api-gw", got)
	}
}

// done is unseen until the window's .seen epoch reaches the state's;
// no .seen, or a done with no epoch, is unseen — the loud side of the guess.
func TestUnseen(t *testing.T) {
	sd := t.TempDir()
	n := func(v float64) *float64 { return &v }
	if !unseen(sd, "@1", n(100)) {
		t.Error("no .seen file must read unseen")
	}
	os.WriteFile(filepath.Join(sd, "@1.seen"), []byte("150\n"), 0o600)
	if unseen(sd, "@1", n(100)) {
		t.Error("shown after it finished must read seen")
	}
	if unseen(sd, "@1", n(150)) {
		t.Error("finished in the second it was shown must read seen")
	}
	if !unseen(sd, "@1", n(200)) {
		t.Error("finished again after being shown must read unseen")
	}
	if !unseen(sd, "@1", nil) {
		t.Error("a done with no epoch must read unseen")
	}
	os.WriteFile(filepath.Join(sd, "@1.seen"), []byte("garbage"), 0o600)
	if !unseen(sd, "@1", n(100)) {
		t.Error("an unreadable .seen must read unseen")
	}
}

// .ctx grew an eighth field, the session; a seven-field file still parses.
func TestCtxSessionField(t *testing.T) {
	c := parseCtx("25\t1.5\t100\t1000\t-1\t1700000000\topus\tsess-1")
	if c.Session == nil || *c.Session != "sess-1" || c.Model == nil || *c.Model != "opus" {
		t.Errorf("8 fields: %+v", c)
	}
	c = parseCtx("25\t1.5\t100\t1000\t-1\t1700000000\topus")
	if c.Session != nil || c.Pct == nil || *c.Pct != 25 {
		t.Errorf("7 fields: %+v", c)
	}
	c = parseCtx("25\t1.5\t100\t1000\t-1\t1700000000\t\tsess-2")
	if c.Model != nil || c.Session == nil || *c.Session != "sess-2" {
		t.Errorf("empty model: %+v", c)
	}
}

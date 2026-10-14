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

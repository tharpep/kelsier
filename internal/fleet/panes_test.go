package fleet

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestParseStatusV2(t *testing.T) {
	out := "# branch.oid 1234abcd\n# branch.head feat\n# branch.upstream origin/feat\n# branch.ab +2 -1\n" +
		"1 .M N... 100644 100644 100644 aaaa bbbb file.go\n? new.txt\n"
	g := parseStatusV2(out)
	if g.Branch == nil || *g.Branch != "feat" {
		t.Errorf("branch: %v", g.Branch)
	}
	if g.Upstream == nil || *g.Upstream != "origin/feat" {
		t.Errorf("upstream: %v", g.Upstream)
	}
	if g.Ahead == nil || *g.Ahead != 2 || g.Behind == nil || *g.Behind != 1 {
		t.Errorf("ahead/behind: %v %v", g.Ahead, g.Behind)
	}
	if g.Dirty == nil || *g.Dirty != 2 {
		t.Errorf("dirty: %v", g.Dirty)
	}

	// detached HEAD, no upstream: branch and ahead/behind are unknown (null),
	// not empty strings or zeros — invariant 3
	g = parseStatusV2("# branch.oid 1234\n# branch.head (detached)\n")
	if g.Branch != nil || g.Upstream != nil || g.Ahead != nil || g.Behind != nil {
		t.Errorf("detached: %+v", g)
	}
	if g.Dirty == nil || *g.Dirty != 0 {
		t.Errorf("clean dirty count: %v", g.Dirty)
	}
}

func TestParseProcNetTCP(t *testing.T) {
	// header, a LISTEN on :1F90 (8080), an ESTABLISHED (01) on another port,
	// and a LISTEN with inode 0 (no owner visible) — only the first counts
	s := "  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode\n" +
		"   0: 0100007F:1F90 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 4242 1 0\n" +
		"   1: 0100007F:0BB8 0100007F:D431 01 00000000:00000000 00:00000000 00000000  1000        0 4343 1 0\n" +
		"   2: 00000000:0016 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 0 1 0\n"
	got := parseProcNetTCP(s)
	if !reflect.DeepEqual(got, map[string]int{"4242": 8080}) {
		t.Errorf("got %v", got)
	}
	// IPv6 rows have a longer address; the port is still after the last ':'
	s6 := "  sl  local_address                         remote_address                        st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode\n" +
		"   0: 00000000000000000000000001000000:1435 00000000000000000000000000000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 5151 1 0\n"
	if got := parseProcNetTCP(s6); !reflect.DeepEqual(got, map[string]int{"5151": 5173}) {
		t.Errorf("ipv6: got %v", got)
	}
}

func TestParseLsof(t *testing.T) {
	s := "p101\nn*:3000\nn[::1]:3000\np202\nn127.0.0.1:8000\n"
	got := parseLsof(s)
	want := map[int][]int{101: {3000, 3000}, 202: {8000}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v", got)
	}
}

func TestClassify(t *testing.T) {
	ports := []int{3000}
	none := []int{}
	cases := []struct {
		cmd, agentCmd string
		ports         *[]int
		kind          string
		agent         bool
	}{
		{"claude", "claude", &none, "agent", true},
		{"claude", "", &none, "agent", true},     // unmanaged Claude Code
		{"agy", "", &none, "agent", true},        // unmanaged Antigravity CLI
		{"codex", "", &none, "agent", true},      // unmanaged Codex
		{"aider", "", &none, "agent", true},      // unmanaged Aider
		{"opencode", "", &none, "agent", true},   // unmanaged OpenCode
		{"sleep", "sleep", &none, "agent", true}, // the record's own agent
		{"node", "claude", &ports, "server", false},
		{"bash", "claude", &none, "shell", false},
		{"nvim", "claude", &none, "program", false},
		{"nvim", "claude", nil, "program", false}, // ports unknown: not a server
	}
	for _, c := range cases {
		k, a := classify(c.cmd, c.agentCmd, c.ports)
		if k != c.kind || a != c.agent {
			t.Errorf("classify(%q, %q): got %s/%v, want %s/%v", c.cmd, c.agentCmd, k, a, c.kind, c.agent)
		}
	}
}

func TestRepoRootWorktree(t *testing.T) {
	root := t.TempDir()
	main := filepath.Join(root, "repo")
	wt := filepath.Join(root, "wt")
	os.MkdirAll(filepath.Join(main, ".git"), 0o755)
	os.MkdirAll(filepath.Join(main, "sub"), 0o755)
	os.MkdirAll(wt, 0o755)
	os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: ../repo/.git/worktrees/wt\n"), 0o644)

	if r, linked, ok := repoRoot(filepath.Join(main, "sub")); !ok || r != main || linked {
		t.Errorf("main checkout: %q %v %v", r, linked, ok)
	}
	if r, linked, ok := repoRoot(wt); !ok || r != wt || !linked {
		t.Errorf("linked worktree: %q %v %v", r, linked, ok)
	}
	if _, _, ok := repoRoot(root); ok {
		t.Error("outside a repo should not be ok")
	}
}

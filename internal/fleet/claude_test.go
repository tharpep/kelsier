package fleet

import (
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

func stubClaude(t *testing.T, sleep string) {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "claude")
	script := "#!/bin/sh\nsleep " + sleep + "\necho '[{\"pid\":7,\"kind\":\"interactive\",\"status\":\"busy\"}]'\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KEL_CLAUDE_BIN", bin)
}

func countSpawns(t *testing.T) *int32 {
	var n int32
	old := spawnRefresh
	spawnRefresh = func() { atomic.AddInt32(&n, 1) }
	t.Cleanup(func() { spawnRefresh = old })
	return &n
}

func writeCache(t *testing.T, sd, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(sd, "claude-agents.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// A stale cache is returned at once, starts one refresh, and a second call
// inside the guard starts none.
func TestClaudeSessionsStaleReturnsAtOnce(t *testing.T) {
	sd := t.TempDir()
	stubClaude(t, "1")
	n := countSpawns(t)
	at := time.Now().Add(-30 * time.Second).Unix()
	writeCache(t, sd, `{"at":`+strconv.FormatInt(at, 10)+`,"sessions":[{"pid":1,"kind":"interactive","status":"idle"}]}`)
	start := time.Now()
	s, ok := claudeSessions(sd)
	if d := time.Since(start); d > 50*time.Millisecond {
		t.Errorf("stale read took %v, want under 50ms", d)
	}
	if !ok || len(s) != 1 || s[0].PID != 1 {
		t.Errorf("stale cache not returned: %v %v", s, ok)
	}
	claudeSessions(sd)
	if *n != 1 {
		t.Errorf("%d refreshes started, want 1", *n)
	}
}

// A cache older than claudeMaxAge is unknown, not a confident old answer.
func TestClaudeSessionsTooOldIsUnknown(t *testing.T) {
	sd := t.TempDir()
	countSpawns(t)
	at := time.Now().Add(-time.Hour).Unix()
	writeCache(t, sd, `{"at":`+strconv.FormatInt(at, 10)+`,"sessions":[{"pid":1}]}`)
	if _, ok := claudeSessions(sd); ok {
		t.Error("an hour-old cache read as known")
	}
}

// A fresh cache starts nothing; a missing one reads inline and writes it.
func TestClaudeSessionsFreshAndMissing(t *testing.T) {
	sd := t.TempDir()
	stubClaude(t, "0")
	n := countSpawns(t)
	s, ok := claudeSessions(sd)
	if !ok || len(s) != 1 || s[0].PID != 7 {
		t.Errorf("missing cache not read inline: %v %v", s, ok)
	}
	if _, err := os.Stat(filepath.Join(sd, "claude-agents.json")); err != nil {
		t.Errorf("inline read wrote no cache: %v", err)
	}
	claudeSessions(sd)
	if *n != 0 {
		t.Errorf("fresh cache started %d refreshes", *n)
	}
}

// The refresh writes the cache and clears the refreshing mark.
func TestRefreshClaudeAgentsWritesCache(t *testing.T) {
	sd := t.TempDir()
	t.Setenv("XDG_STATE_HOME", sd)
	if err := os.MkdirAll(StateDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	stubClaude(t, "0")
	cache := filepath.Join(StateDir(), "claude-agents.json")
	os.WriteFile(cache, []byte(`{"at":1,"refreshing":99,"sessions":[]}`), 0o600)
	RefreshClaudeAgents()
	s, ok := claudeSessions(StateDir())
	if !ok || len(s) != 1 || s[0].PID != 7 {
		t.Errorf("refresh did not write the cache: %v %v", s, ok)
	}
}

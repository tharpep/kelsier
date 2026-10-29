package fleet

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestQuota(t *testing.T) {
	sd := t.TempDir()
	if loadQuota(sd, 1000) != nil {
		t.Error("no file must be no quota, not a zero one")
	}
	write := func(s string) { os.WriteFile(filepath.Join(sd, "quota"), []byte(s), 0o600) }

	now := int64(100000)
	write("58 104800 41 600000 -1 0 99000\n")
	q := loadQuota(sd, now)
	if q == nil || q.FiveHour.Pct == nil || *q.FiveHour.Pct != 58 || *q.SevenDay.Pct != 41 {
		t.Fatalf("fresh figures: %+v", q)
	}
	if q.Spend != nil {
		t.Error("an unreported spend limit must be absent, not 0%")
	}

	// the 5h window reset since it was seen: unknown, not the old 58
	write("58 99500 41 600000 -1 0 99000\n")
	if q := loadQuota(sd, now); q.FiveHour.Pct != nil || q.SevenDay.Pct == nil {
		t.Errorf("past 5h reset: %+v %+v", q.FiveHour, q.SevenDay)
	}
	// no reset given, but seen longer ago than the window is long
	write("58 0 41 0 -1 0 80000\n")
	if q := loadQuota(sd, now); q.FiveHour.Pct != nil || q.SevenDay.Pct == nil {
		t.Errorf("seen 5.5h ago: %+v %+v", q.FiveHour, q.SevenDay)
	}
	write("58 0 41 0 -1 0 1\n")
	if q := loadQuota(sd, now*10); q.SevenDay.Pct != nil {
		t.Error("a weekly figure older than a week must read unknown")
	}
	for _, bad := range []string{"", "garbage", "1 2 3", "a b c d e f g"} {
		write(bad)
		if loadQuota(sd, now) != nil {
			t.Errorf("unreadable %q must be no quota", bad)
		}
	}
}

// Subagent rows are current only while kel substatus keeps writing them.
func TestLoadSubs(t *testing.T) {
	sd := t.TempDir()
	os.WriteFile(filepath.Join(sd, "@1.subs"), []byte("100\n[{\"id\":\"t1\",\"status\":\"running\",\"label\":\"x\"}]\n"), 0o600)
	if s := loadSubs(sd, "@1", 110); len(s) != 1 || s[0].Label != "x" {
		t.Errorf("fresh: %+v", s)
	}
	if s := loadSubs(sd, "@1", 200); s != nil {
		t.Error("older than subsFresh must read as no subagents now")
	}
	if loadSubs(sd, "@2", 110) != nil {
		t.Error("no file, no subagents")
	}
}

// Claude Code's status corrects the hooks where they cannot know, and never
// overrides waiting.
func TestWithClaude(t *testing.T) {
	sd := t.TempDir()
	pid := os.Getpid()
	w, wt := "working", "waiting"
	for _, c := range []struct {
		hook, claude, want string
	}{{w, "idle", "idle"}, {"idle", "busy", "working"}, {wt, "idle", "waiting"}, {w, "busy", "working"}} {
		cache := fmt.Sprintf(`{"at":%d,"sessions":[{"pid":%d,"kind":"interactive","status":%q}]}`, time.Now().Unix(), pid, c.claude)
		os.WriteFile(filepath.Join(sd, "claude-agents.json"), []byte(cache), 0o600)
		st := c.hook
		doc := Doc{Agents: []Agent{{WindowID: sp("@1"), State: &st}}}
		p := float64(pid)
		withClaude(&doc, sd, []Pane{{PaneID: "%1", WindowID: "@1", PID: &p}})
		if got := *doc.Agents[0].State; got != c.want {
			t.Errorf("hook %s + claude %s: %s, want %s", c.hook, c.claude, got, c.want)
		}
	}
}

func TestQuotaCmd(t *testing.T) {
	sd := t.TempDir()
	now := time.Now().Unix()

	// 1. Quota fallback from quota-cmd
	os.WriteFile(filepath.Join(sd, "quota-cmd"), []byte(fmt.Sprintf("42 0 %d\n", now)), 0o600)
	q := loadQuota(sd, now)
	if q == nil || q.Spend == nil || q.Spend.Pct == nil || *q.Spend.Pct != 42 {
		t.Fatalf("quota-cmd fallback: got %+v, want Spend.Pct=42", q)
	}

	// 2. Claude Code quota payload spend_limit beats quota-cmd
	// format: <pct5> <at5> <pct7> <at7> <spend> <atspend> <epoch>
	os.WriteFile(filepath.Join(sd, "quota"), []byte(fmt.Sprintf("-1 0 -1 0 85 0 %d\n", now)), 0o600)
	q = loadQuota(sd, now)
	if q == nil || q.Spend == nil || q.Spend.Pct == nil || *q.Spend.Pct != 85 {
		t.Fatalf("quota payload wins: got %+v, want Spend.Pct=85", q)
	}

	// 3. Stale quota-cmd (reset in the past)
	os.Remove(filepath.Join(sd, "quota"))
	os.WriteFile(filepath.Join(sd, "quota-cmd"), []byte(fmt.Sprintf("42 %d %d\n", now-10, now)), 0o600)
	q = loadQuota(sd, now)
	if q != nil && q.Spend != nil && q.Spend.Pct != nil {
		t.Fatalf("stale reset in past should be null: got %+v", q)
	}

	// 4. Stale quota-cmd older than 2 * ttl
	t.Setenv("KEL_QUOTA_TTL", "10")
	os.WriteFile(filepath.Join(sd, "quota-cmd"), []byte(fmt.Sprintf("42 0 %d\n", now-25)), 0o600)
	q = loadQuota(sd, now)
	if q != nil && q.Spend != nil && q.Spend.Pct != nil {
		t.Fatalf("older than 2*ttl should be null: got %+v", q)
	}

	// 5. Stale quota-cmd with future reset but sample older than 2 * ttl
	os.WriteFile(filepath.Join(sd, "quota-cmd"), []byte(fmt.Sprintf("42 %d %d\n", now+1000, now-25)), 0o600)
	q = loadQuota(sd, now)
	if q != nil && q.Spend != nil && q.Spend.Pct != nil {
		t.Fatalf("future reset with sample older than 2*ttl should be null: got %+v", q)
	}
}

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

// withClaude marks a pane whose pid is in the session list as an agent, even
// if its Cmd is a version number (macOS native installer). A shell beside it
// stays a shell.
func TestWithClaudeMarksPaneAgent(t *testing.T) {
	sd := t.TempDir()
	pid := os.Getpid()
	cache := fmt.Sprintf(`{"at":%d,"sessions":[{"pid":%d,"kind":"interactive","status":"busy"}]}`, time.Now().Unix(), pid)
	os.WriteFile(filepath.Join(sd, "claude-agents.json"), []byte(cache), 0o600)
	p := float64(pid)
	panes := []Pane{
		{PaneID: "%1", WindowID: "@1", PID: &p, Cmd: "2.1.304", Kind: "program", Agent: false},
		{PaneID: "%2", WindowID: "@1", Cmd: "bash", Kind: "shell", Agent: false},
	}
	doc := Doc{Panes: &panes, Agents: []Agent{{WindowID: sp("@1")}}}
	withClaude(&doc, sd, panes)
	if (*doc.Panes)[0].Kind != "agent" || !(*doc.Panes)[0].Agent {
		t.Errorf("pane %%1: got kind=%s agent=%v, want agent/true", (*doc.Panes)[0].Kind, (*doc.Panes)[0].Agent)
	}
	if (*doc.Panes)[1].Kind != "shell" || (*doc.Panes)[1].Agent {
		t.Errorf("pane %%2: got kind=%s agent=%v, want shell/false", (*doc.Panes)[1].Kind, (*doc.Panes)[1].Agent)
	}
}

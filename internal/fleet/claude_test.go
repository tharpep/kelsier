package fleet

import (
	"encoding/json"
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
	old := StartClaudeRefresh
	StartClaudeRefresh = func(string) { atomic.AddInt32(&n, 1) }
	t.Cleanup(func() { StartClaudeRefresh = old })
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
	s, _, ok := claudeSessions(sd)
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
	if _, _, ok := claudeSessions(sd); ok {
		t.Error("an hour-old cache read as known")
	}
}

// A fresh cache starts nothing; a missing one reads inline and writes it.
func TestClaudeSessionsFreshAndMissing(t *testing.T) {
	sd := t.TempDir()
	stubClaude(t, "0")
	n := countSpawns(t)
	s, _, ok := claudeSessions(sd)
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

// The default refresh, a goroutine as the sidebar runs it, writes the cache
// and clears the refreshing mark.
func TestStaleCacheRefreshesInGoroutine(t *testing.T) {
	sd := t.TempDir()
	stubClaude(t, "0")
	writeCache(t, sd, `{"at":1,"sessions":[]}`)
	if s, _, _ := claudeSessions(sd); len(s) != 0 {
		t.Errorf("stale cache not returned as it was: %v", s)
	}
	for i := 0; i < 100; i++ {
		if s, _, _ := claudeSessions(sd); len(s) == 1 && s[0].PID == 7 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Error("the goroutine refresh never wrote the cache")
}

type cs map[string]any

type scene struct {
	hook, note, rec, ctxSession string
	ctxAt                       int64
	ctxModel                    string
	since                       int64
	cwd, name                   string
	cacheAt                     int64
	failed                      bool
	sessions                    []cs
}

func (sc scene) run(t *testing.T) Agent {
	t.Helper()
	sd := t.TempDir()
	pid := os.Getpid()
	for _, s := range sc.sessions {
		if s["pid"] == "self" {
			s["pid"] = pid
		}
		if s["pid"] == "parent" {
			s["pid"] = os.Getppid()
		}
	}
	b, _ := json.Marshal(map[string]any{"at": sc.cacheAt, "failed": sc.failed, "sessions": sc.sessions})
	writeCache(t, sd, string(b))
	a := Agent{Name: sc.name, Managed: true, WindowID: sp("@1"), PaneID: sp("%1"), State: sp(sc.hook),
		Cwd: sp(sc.cwd), Context: &Context{Pct: new(float64)}}
	if sc.name == "" {
		a.Name = "docs"
	}
	if sc.since != 0 {
		f := float64(sc.since)
		a.StateSince = &f
	}
	if sc.note != "" {
		a.Note = sp(sc.note)
	}
	if sc.rec != "" {
		a.ClaudeSession = sp(sc.rec)
	}
	if sc.ctxSession != "" {
		a.Context.Session = sp(sc.ctxSession)
	}
	if sc.ctxAt != 0 {
		f := float64(sc.ctxAt)
		a.Context.At = &f
	}
	if sc.ctxModel != "" {
		a.Context.Model = sp(sc.ctxModel)
	}
	p := float64(pid)
	panes := []Pane{{PaneID: "%1", WindowID: "@1", PID: &p}}
	doc := Doc{Agents: []Agent{a}}
	withClaude(&doc, sd, panes)
	return doc.Agents[0]
}

func self(status, id string) cs {
	return cs{"pid": "self", "kind": "interactive", "status": status, "sessionId": id}
}

func stateOf(a Agent) string {
	if a.State == nil {
		return "<nil>"
	}
	return *a.State
}

func wantState(t *testing.T, a Agent, want string) {
	t.Helper()
	if got := stateOf(a); got != want {
		t.Errorf("state %s, want %s", got, want)
	}
}

func TestF1BackgroundWaitingBeatsOldDone(t *testing.T) {
	now := time.Now().Unix()
	since := now - 37*3600
	a := scene{hook: "done", since: since, rec: "A", cwd: "/r/docs", cacheAt: now,
		sessions: []cs{{"pid": 99999999, "kind": "background", "id": "bg1", "name": "docs", "cwd": "/r/docs",
			"sessionId": "B", "status": "waiting", "state": "blocked", "waitingFor": "permission prompt",
			"startedAt": (since + 3) * 1000}}}.run(t)
	wantState(t, a, "waiting")
	if a.StateSince != nil || a.Context != nil || a.Unseen {
		t.Errorf("since %v ctx %v unseen %v, want nil nil false", a.StateSince, a.Context, a.Unseen)
	}
	if a.Note == nil || *a.Note != "permission prompt" {
		t.Errorf("note %v", a.Note)
	}
}

func TestHookNewerThanCacheStands(t *testing.T) {
	now := time.Now().Unix()
	for _, c := range []struct{ hook, status string }{{"working", "idle"}, {"done", "busy"}} {
		a := scene{hook: c.hook, since: now - 5, cacheAt: now - 10, sessions: []cs{self(c.status, "A")}}.run(t)
		wantState(t, a, c.hook)
	}
	a := scene{hook: "working", since: now - 5, cacheAt: now - 5, sessions: []cs{self("idle", "A")}}.run(t)
	wantState(t, a, "working")
}

func TestCacheNewerCorrectsHook(t *testing.T) {
	now := time.Now().Unix()
	for _, c := range []struct{ hook, status, want string }{{"working", "idle", "idle"}, {"done", "busy", "working"}, {"idle", "busy", "working"}} {
		a := scene{hook: c.hook, since: now - 30, cacheAt: now - 5, sessions: []cs{self(c.status, "A")}}.run(t)
		wantState(t, a, c.want)
		if a.StateSince == nil {
			t.Errorf("%s: StateSince dropped", c.hook)
		}
	}
}

func TestWaitingClearsOnNewerIdle(t *testing.T) {
	now := time.Now().Unix()
	a := scene{hook: "waiting", note: "allow Bash? ls", since: now - 30, cacheAt: now - 5, sessions: []cs{self("idle", "A")}}.run(t)
	wantState(t, a, "idle")
	if a.Note != nil {
		t.Errorf("note %q survived leaving waiting", *a.Note)
	}
}

func TestWaitingBecomesWorkingOnNewerBusy(t *testing.T) {
	now := time.Now().Unix()
	a := scene{hook: "waiting", note: "allow Bash? ls", since: now - 30, cacheAt: now - 5, sessions: []cs{self("busy", "A")}}.run(t)
	wantState(t, a, "working")
	if a.Note != nil {
		t.Errorf("note %q survived leaving waiting", *a.Note)
	}
}

func TestClaudeWaitingEntersWaiting(t *testing.T) {
	now := time.Now().Unix()
	s := self("waiting", "A")
	s["waitingFor"] = "plan approval"
	a := scene{hook: "working", since: now - 30, cacheAt: now - 5, sessions: []cs{s}}.run(t)
	wantState(t, a, "waiting")
	if a.Note == nil || *a.Note != "plan approval" {
		t.Errorf("note %v", a.Note)
	}
}

func TestErrorHoldsAgainstIdle(t *testing.T) {
	now := time.Now().Unix()
	for _, h := range []string{"error", "throttled", "done"} {
		a := scene{hook: h, since: now - 30, cacheAt: now - 5, sessions: []cs{self("idle", "A")}}.run(t)
		wantState(t, a, h)
	}
}

func TestClearRaceKeepsFreshHook(t *testing.T) {
	now := time.Now().Unix()
	s := self("idle", "A")
	s["startedAt"] = (now - 7200) * 1000
	a := scene{hook: "working", since: now - 3, rec: "B", cacheAt: now - 8, sessions: []cs{s}}.run(t)
	wantState(t, a, "working")
}

func TestSplitClaudeDoesNotCorrectFirstAgent(t *testing.T) {
	sd := t.TempDir()
	pid, ppid := os.Getpid(), os.Getppid()
	now := time.Now().Unix()
	b, _ := json.Marshal(map[string]any{"at": now, "sessions": []cs{
		{"pid": pid, "kind": "interactive", "status": "idle", "sessionId": "A"},
		{"pid": ppid, "kind": "interactive", "status": "busy", "sessionId": "S"}}})
	writeCache(t, sd, string(b))
	p1, p2 := float64(pid), float64(ppid)
	panes := []Pane{{PaneID: "%1", WindowID: "@1", PID: &p1}, {PaneID: "%2", WindowID: "@1", PID: &p2}}
	doc := Doc{Agents: []Agent{{Name: "docs", WindowID: sp("@1"), PaneID: sp("%1"), State: sp("idle")}}}
	withClaude(&doc, sd, panes)
	wantState(t, doc.Agents[0], "idle")
	if doc.Agents[0].Claude == nil || doc.Agents[0].Claude.SessionID != "A" {
		t.Errorf("claude view %+v", doc.Agents[0].Claude)
	}
}

func foldDoc(t *testing.T, agents []Agent, bg ...cs) Doc {
	sd := t.TempDir()
	b, _ := json.Marshal(map[string]any{"at": time.Now().Unix(), "sessions": bg})
	writeCache(t, sd, string(b))
	doc := Doc{Agents: agents}
	withClaude(&doc, sd, nil)
	return doc
}

func bgDoc(cwd string) cs {
	return cs{"pid": 99999999, "kind": "background", "id": "b", "name": "docs", "cwd": cwd, "sessionId": "B", "status": "busy"}
}

func managed(name, cwd, wid string) Agent {
	return Agent{Name: name, Managed: true, WindowID: sp(wid), Cwd: sp(cwd), State: sp("idle")}
}

func TestFoldNeedsCwd(t *testing.T) {
	doc := foldDoc(t, []Agent{managed("docs", "/r1", "@1"), managed("docs", "/r2", "@2")}, bgDoc("/r2/sub"))
	if doc.Agents[0].Claude != nil || doc.Agents[1].Claude == nil {
		t.Errorf("fold went to %v / %v, want /r2 only", doc.Agents[0].Claude, doc.Agents[1].Claude)
	}
	if len(*doc.Background) != 0 {
		t.Errorf("folded session still listed: %v", *doc.Background)
	}
	doc = foldDoc(t, []Agent{managed("docs", "/r1", "@1")}, bgDoc("/elsewhere"))
	if doc.Agents[0].Claude != nil || len(*doc.Background) != 1 {
		t.Errorf("a name match in another directory folded: %v", doc.Agents[0].Claude)
	}
}

func TestFoldAmbiguousIsListed(t *testing.T) {
	doc := foldDoc(t, []Agent{managed("docs", "/r", "@1"), managed("docs", "/r", "@2")}, bgDoc("/r"))
	if doc.Agents[0].Claude != nil || doc.Agents[1].Claude != nil || len(*doc.Background) != 1 {
		t.Errorf("an ambiguous fold was taken: %v %v bg=%d", doc.Agents[0].Claude, doc.Agents[1].Claude, len(*doc.Background))
	}
}

func TestFoldBySessionID(t *testing.T) {
	a := managed("renamed", "/r9", "@1")
	a.ClaudeSession = sp("B")
	doc := foldDoc(t, []Agent{a}, bgDoc("/r"))
	if doc.Agents[0].Claude == nil || len(*doc.Background) != 0 {
		t.Errorf("a record's own session id did not fold")
	}
}

func TestCtxOfOtherSessionDropped(t *testing.T) {
	now := time.Now().Unix()
	a := scene{hook: "idle", since: now - 5, ctxSession: "OLD", cacheAt: now, sessions: []cs{self("idle", "NEW")}}.run(t)
	if a.Context != nil {
		t.Error("another session's context survived")
	}
	a = scene{hook: "idle", since: now - 5, ctxSession: "NEW", cacheAt: now, sessions: []cs{self("idle", "NEW")}}.run(t)
	if a.Context == nil {
		t.Error("the live session's own context was dropped")
	}
	a = scene{hook: "idle", since: now - 5, cacheAt: now, sessions: []cs{self("idle", "NEW")}}.run(t)
	if a.Context == nil {
		t.Error("a context with no recorded session was dropped")
	}
}

func TestStaleCacheHooksAlone(t *testing.T) {
	now := time.Now().Unix()
	s := scene{hook: "waiting", since: now - 300, cacheAt: now - 120, sessions: []cs{self("busy", "A")}}
	wantState(t, s.run(t), "waiting")
	s.cacheAt, s.failed = now, true
	wantState(t, s.run(t), "waiting")
}

func bgSession(status, state, sid string, startedAt int64) cs {
	s := cs{"pid": 99999999, "kind": "background", "id": "9ccb61f4", "name": "chat-service-head",
		"cwd": "/home/tharpep/code/chat-service", "sessionId": sid, "status": status, "state": state}
	if startedAt != 0 {
		s["startedAt"] = startedAt
	}
	return s
}

func TestBackgroundBlockedIdleIsIdle(t *testing.T) {
	now := time.Now().Unix()
	since := now - 37*3600
	for _, started := range []int64{0, (since + 3) * 1000} {
		a := scene{hook: "done", since: since, rec: "3b521dc0-6479-44cc-922e-68bab94e0ab0",
			name: "chat-service-head", cwd: "/home/tharpep/code/chat-service", ctxModel: "Fable 5.1", cacheAt: now,
			sessions: []cs{bgSession("idle", "blocked", "9ccb61f4-32e5-4335-9ab3-77500969210a", started)}}.run(t)
		wantState(t, a, "idle")
		if a.Note != nil || a.Context != nil || a.StateSince != nil || a.Unseen {
			t.Errorf("startedAt %d: note %v ctx %v since %v unseen %v", started, a.Note, a.Context, a.StateSince, a.Unseen)
		}
		if a.Claude == nil || a.Claude.SessionID != "9ccb61f4-32e5-4335-9ab3-77500969210a" {
			t.Errorf("claude view %+v", a.Claude)
		}
	}
}

func TestBackgroundFoldFreshHookStands(t *testing.T) {
	now := time.Now().Unix()
	a := scene{hook: "working", since: now - 2, rec: "A", name: "chat-service-head", cwd: "/home/tharpep/code/chat-service",
		ctxModel: "m", cacheAt: now - 8, sessions: []cs{bgSession("idle", "blocked", "B", 0)}}.run(t)
	wantState(t, a, "working")
	if a.Context == nil {
		t.Error("context dropped")
	}
}

func TestBackgroundSameSessionKeepsHook(t *testing.T) {
	now := time.Now().Unix()
	a := scene{hook: "done", since: now - 37*3600, rec: "B", name: "chat-service-head", cwd: "/home/tharpep/code/chat-service",
		ctxModel: "m", cacheAt: now, sessions: []cs{bgSession("idle", "blocked", "B", 0)}}.run(t)
	wantState(t, a, "done")
	if a.Context == nil {
		t.Error("context dropped")
	}
}

func TestCtxNewerThanCacheKept(t *testing.T) {
	now := time.Now().Unix()
	a := scene{hook: "idle", since: now - 30, ctxSession: "B", ctxAt: now, cacheAt: now - 8,
		sessions: []cs{self("idle", "A")}}.run(t)
	if a.Context == nil {
		t.Error("a context newer than the cache was dropped")
	}
}

func TestCacheKeepsStartedAtAndWaitingFor(t *testing.T) {
	sd := t.TempDir()
	bin := filepath.Join(t.TempDir(), "claude")
	script := "#!/bin/sh\necho '[{\"pid\":7,\"kind\":\"background\",\"status\":\"waiting\",\"startedAt\":1793943696396,\"waitingFor\":\"plan approval\"}]'\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KEL_CLAUDE_BIN", bin)
	refreshClaudeAgents(sd, "")
	s, _, _ := claudeSessions(sd)
	if len(s) != 1 || s[0].StartedAt != 1793943696396 || s[0].WaitingFor != "plan approval" {
		t.Errorf("reread %+v", s)
	}
}

func bgWaiting(sid string) cs {
	return cs{"pid": 99999999, "kind": "background", "id": "bg1", "name": "docs", "cwd": "/r/docs",
		"sessionId": sid, "status": "waiting", "waitingFor": "permission prompt"}
}

func TestCorrectTagsStateFromClaude(t *testing.T) {
	now := time.Now().Unix()
	a := scene{hook: "done", since: now - 37*3600, rec: "A", cwd: "/r/docs", cacheAt: now,
		sessions: []cs{bgWaiting("B")}}.run(t)
	if a.StateFrom != "claude" || a.Claude == nil || a.Claude.At != now {
		t.Errorf("replaced: from %q view %+v", a.StateFrom, a.Claude)
	}
	a = scene{hook: "working", since: now - 30, cacheAt: now, sessions: []cs{self("idle", "A")}}.run(t)
	wantState(t, a, "idle")
	if a.StateFrom != "claude" {
		t.Errorf("newer cache: from %q", a.StateFrom)
	}
	a = scene{hook: "working", since: now, cacheAt: now, sessions: []cs{self("idle", "A")}}.run(t)
	if a.StateFrom != "" {
		t.Errorf("hook stands: from %q", a.StateFrom)
	}
	a = scene{hook: "done", since: now - 60, cacheAt: now, sessions: []cs{self("idle", "A")}}.run(t)
	if a.StateFrom != "" {
		t.Errorf("done held against idle: from %q", a.StateFrom)
	}
}

func TestWrittenStateIsHookStateNextLoad(t *testing.T) {
	now := time.Now().Unix()
	for _, rec := range []string{"A", "B"} {
		a := scene{hook: "waiting", since: now, rec: rec, cwd: "/r/docs", cacheAt: now,
			sessions: []cs{bgWaiting("B")}}.run(t)
		wantState(t, a, "waiting")
		if a.StateFrom != "" {
			t.Errorf("rec %s: written state tagged again: %q", rec, a.StateFrom)
		}
	}
}

func TestF1ContextStaysUnknownAfterWrite(t *testing.T) {
	now := time.Now().Unix()
	sc := scene{hook: "waiting", since: now, rec: "B", cwd: "/r/docs", cacheAt: now, sessions: []cs{bgWaiting("B")}}
	sd := t.TempDir()
	b, _ := json.Marshal(map[string]any{"at": sc.cacheAt, "sessions": sc.sessions})
	writeCache(t, sd, string(b))
	f := float64(now)
	doc := Doc{Agents: []Agent{{Name: "docs", Managed: true, WindowID: sp("@1"), PaneID: sp("%1"), State: sp("waiting"),
		StateSince: &f, Cwd: sp("/r/docs"), ClaudeSession: sp("B")}}}
	withClaude(&doc, sd, []Pane{{PaneID: "%1", WindowID: "@1"}})
	if doc.Agents[0].Context != nil {
		t.Errorf("context appeared: %+v", doc.Agents[0].Context)
	}
}

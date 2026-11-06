package fleet

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadTasks(t *testing.T) {
	d := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", d)
	put := func(sid, name, body string) {
		dir := filepath.Join(d, "tasks", sid)
		_ = os.MkdirAll(dir, 0o700)
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	put("s1", "1.json", `{"id":"1","status":"completed"}`)
	put("s1", "2.json", `{"id":"2","status":"in_progress"}`)
	put("s1", "3.json", `{"id":"3","status":"pending"}`)
	put("s1", "4.json", `{"id":"4","status":"deleted"}`)
	put("s1", ".highwatermark", `4`)
	if got := readTasks("s1"); got == nil || got.Done != 1 || got.Total != 3 {
		t.Errorf("readTasks(s1) = %+v, want 1 of 3", got)
	}
	put("bad", "1.json", `{"id":"1","status":"completed"}`)
	put("bad", "2.json", `{not json`)
	if readTasks("bad") != nil {
		t.Error("a file that does not parse must make the count absent, not short")
	}
	put("nostatus", "1.json", `{"id":"1"}`)
	if readTasks("nostatus") != nil {
		t.Error("a task with no status must make the count absent")
	}
	put("gone", "1.json", `{"status":"deleted"}`)
	if readTasks("gone") != nil {
		t.Error("only deleted tasks is no list")
	}
	if readTasks("nodir") != nil || readTasks("") != nil || readTasks("../s1") != nil {
		t.Error("no directory, no session or a path is no list")
	}
}

func TestWithTasks(t *testing.T) {
	d := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", d)
	dir := filepath.Join(d, "tasks", "sid1")
	_ = os.MkdirAll(dir, 0o700)
	_ = os.WriteFile(filepath.Join(dir, "1.json"), []byte(`{"status":"completed"}`), 0o600)
	sid := "sid1"
	doc := Doc{Agents: []Agent{{ClaudeSession: &sid}, {Claude: &ClaudeView{SessionID: "sid1"}}, {}}}
	withTasks(&doc)
	if a := doc.Agents; a[0].Tasks == nil || a[1].Tasks == nil || a[2].Tasks != nil {
		t.Errorf("tasks by session: %+v %+v %+v", a[0].Tasks, a[1].Tasks, a[2].Tasks)
	}
}

package fleet

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLastMessage(t *testing.T) {
	f := filepath.Join(t.TempDir(), "s.jsonl")
	write := func(lines ...string) { _ = os.WriteFile(f, []byte(strings.Join(lines, "\n")+"\n"), 0o600) }
	text := func(s string) string {
		return `{"type":"assistant","message":{"content":[{"type":"text","text":` + quote(s) + `}]}}`
	}
	tool := `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Edit","input":{"file_path":"/a/b"}}]}}`
	think := `{"type":"assistant","message":{"content":[{"type":"thinking","thinking":"secret"}]}}`
	result := `{"type":"user","message":{"content":[{"type":"tool_result","content":"ok"}]}}`

	write(text("older words"), text("PR 59 is merged.\nSecond line."), tool, result, think)
	if got := lastMessageOf(f); got != "PR 59 is merged." {
		t.Errorf("last text before tool_use: %q", got)
	}

	write(text("## **Done** with `go test` and a \n- list"))
	if got := lastMessageOf(f); got != "Done with go test and a" {
		t.Errorf("markdown stripped: %q", got)
	}
	write(text("```go\nx := 1\n```"))
	if got := lastMessageOf(f); got != "x := 1" {
		t.Errorf("fence line: %q", got)
	}
	write(text("- bullet one\n- two"))
	if got := lastMessageOf(f); got != "bullet one" {
		t.Errorf("bullet: %q", got)
	}

	write(text("\x1b[2J\x1b]0;pwn\x07hi\r\b there\ttab"))
	if got := lastMessageOf(f); got != "[2J ]0;pwn hi there tab" {
		t.Errorf("controls: %q", got)
	}

	write(`{"type":"user","message":{"content":"hi"}}`, tool, think)
	if got := lastMessageOf(f); got != "" {
		t.Errorf("no assistant text: %q", got)
	}
	write(`{"type":"PLANNER_RESPONSE","tool_calls":[]}`)
	if got := lastMessageOf(f); got != "" {
		t.Errorf("agy: %q", got)
	}
	if lastMessageOf(filepath.Join(t.TempDir(), "none")) != "" || lastMessageOf("") != "" {
		t.Error("no file")
	}

	pad := `{"type":"user","message":{"content":"` + strings.Repeat("y", 1<<20) + `"}}`
	write(text("found past one tail"), pad)
	if got := lastMessageOf(f); got != "found past one tail" {
		t.Errorf("tail widens: %q", got)
	}
	write(text("too far back"), pad, pad, pad, pad)
	if got := lastMessageOf(f); got != "" {
		t.Errorf("read beyond the largest tail: %q", got)
	}
}

func TestWithLastMessageOnlyDoneAndIdle(t *testing.T) {
	f := filepath.Join(t.TempDir(), "s.jsonl")
	_ = os.WriteFile(f, []byte(`{"type":"assistant","message":{"content":[{"type":"text","text":"hello"}]}}`+"\n"), 0o600)
	var doc Doc
	for _, s := range []string{"done", "idle", "working", "waiting", "error"} {
		st := s
		doc.Agents = append(doc.Agents, Agent{State: &st})
	}
	doc.Agents = append(doc.Agents, Agent{})
	paths := make([]string, len(doc.Agents))
	for i := range paths {
		paths[i] = f
	}
	withLastMessage(&doc, paths)
	for i, want := range []bool{true, true, false, false, false, false} {
		if got := doc.Agents[i].LastMessage != nil; got != want {
			t.Errorf("agent %d: has message %v, want %v", i, got, want)
		}
	}
}

func quote(s string) string { b, _ := json.Marshal(s); return string(b) }

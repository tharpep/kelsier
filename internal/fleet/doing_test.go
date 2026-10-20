package fleet

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDoing(t *testing.T) {
	d := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", d)
	p := filepath.Join(d, "projects", "-x")
	_ = os.MkdirAll(p, 0o700)
	f := filepath.Join(p, "sid1.jsonl")
	write := func(lines ...string) { _ = os.WriteFile(f, []byte(strings.Join(lines, "\n")+"\n"), 0o600) }
	tool := `{"type":"assistant","message":{"content":[{"type":"text","text":"ok"},{"type":"tool_use","name":"Edit","input":{"file_path":"/a/b/sbview.go"}}]}}`
	result := `{"type":"user","message":{"content":[{"type":"tool_result","content":"done"}]}}`
	write(tool, result)
	if transcriptOf("sid1") != f || doingOf(f) != "editing sbview.go" {
		t.Errorf("edit: %q %q", transcriptOf("sid1"), doingOf(f))
	}
	write(tool, `{"type":"assistant","message":{"content":[{"type":"thinking","thinking":"hm"}]}}`)
	if doingOf(f) != "thinking" {
		t.Errorf("thinking: %q", doingOf(f))
	}
	write(`{"type":"user","message":{"content":"hi"}}`)
	if doingOf(f) != "" {
		t.Error("no assistant turn yet must say nothing")
	}
	// a tail that starts mid-line is read from the next whole line
	long := strings.Repeat("x", int(doingTails[0]))
	write(`{"type":"assistant","message":{"content":[{"type":"text","text":"`+long+`"}]}}`, tool)
	if doingOf(f) != "editing sbview.go" {
		t.Errorf("cut tail: %q", doingOf(f))
	}
	for in, want := range map[string]string{
		`Bash|{"command":"go test ./...","description":"Run the tests"}`: "run the tests",
		`Bash|{"command":"ls -la\nmore"}`:                                "$ ls -la",
		`Grep|{"pattern":"TODO"}`:                                        "searching for TODO",
		`Agent|{"description":"Find the parser"}`:                        "subagent: Find the parser",
		`WebFetch|{"url":"https://go.dev/doc"}`:                          "reading go.dev",
		`mcp__claude_ai_Gmail__search_threads|{}`:                        "using search_threads",
	} {
		name, js, _ := strings.Cut(in, "|")
		var m map[string]any
		_ = jsonUnmarshal(js, &m)
		if got := describeTool(name, m); got != want {
			t.Errorf("%s: %q, want %q", name, got, want)
		}
	}
	if transcriptOf("../x") != "" || transcriptOf("") != "" {
		t.Error("a session id is not a path")
	}
}

func jsonUnmarshal(s string, v any) error { return json.Unmarshal([]byte(s), v) }

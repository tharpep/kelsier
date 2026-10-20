package fleet

import (
	"bytes"
	"encoding/json"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// What a working agent is doing now — its last tool call, in a few words
// (backlog #40). The author asked why this was held: the source considered
// then was a PreToolUse hook on every tool, which would put kel on the
// agent's hottest path (invariant 5). Claude Code already writes every call
// to the session's transcript, so this reads the tail of that file instead —
// one stat and a read of a few tail sizes per working agent, on the
// sidebar's path only, and nothing at all on the agent's side. Nothing of it
// is kept: the words live in the fleet document and are gone with it.

// Claude Code ends each turn with a request record of 100 KB or more, after
// the assistant's own lines, so the tail widens until one is found.
var doingTails = []int64{64 << 10, 512 << 10, 2 << 20}

// transcriptOf finds a session's transcript: Claude Code keeps it as
// projects/<slug of the directory it started in>/<session id>.jsonl, and the
// slug is not worth re-deriving when a glob finds it.
func transcriptOf(sid string) string {
	if sid == "" || strings.ContainsAny(sid, "/*?[") {
		return ""
	}
	m, _ := filepath.Glob(filepath.Join(claudeHome(), "projects", "*", sid+".jsonl"))
	if len(m) == 0 {
		return ""
	}
	return m[0]
}

func claudeHome() string {
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return d
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".claude")
}

type tline struct {
	Type    string `json:"type"`
	Message struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

type tpart struct {
	Type  string         `json:"type"`
	Name  string         `json:"name"`
	Input map[string]any `json:"input"`
}

// doingOf reads the transcript's tail and describes its last assistant turn:
// the last tool it called, or "writing" when it called none. "" when the
// file cannot be read or holds no assistant turn yet — never a guess.
func doingOf(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return ""
	}
	for _, n := range doingTails {
		d, found := doingIn(f, st.Size(), n)
		if found || n >= st.Size() {
			return d
		}
	}
	return ""
}

// doingIn reads the last n bytes; found is false when they hold no assistant
// turn, so the caller may look further back.
func doingIn(f *os.File, size, n int64) (string, bool) {
	off := size - n
	if off < 0 {
		off = 0
	}
	buf := make([]byte, size-off)
	if _, err := f.ReadAt(buf, off); err != nil && err != io.EOF {
		return "", true
	}
	lines := bytes.Split(buf, []byte("\n"))
	if off > 0 && len(lines) > 0 {
		lines = lines[1:] // the first is cut mid-line
	}
	for i := len(lines) - 1; i >= 0; i-- {
		var l tline
		if json.Unmarshal(lines[i], &l) != nil || l.Type != "assistant" {
			continue
		}
		var parts []tpart
		if json.Unmarshal(l.Message.Content, &parts) != nil {
			return "writing", true
		}
		for j := len(parts) - 1; j >= 0; j-- {
			if parts[j].Type == "tool_use" {
				return describeTool(parts[j].Name, parts[j].Input), true
			}
		}
		for _, p := range parts {
			if p.Type == "thinking" {
				return "thinking", true
			}
		}
		return "writing", true
	}
	return "", false
}

func str(in map[string]any, k string) string {
	s, _ := in[k].(string)
	return strings.TrimSpace(strings.SplitN(s, "\n", 2)[0])
}

// describeTool says a tool call the way you would: "editing sbview.go".
func describeTool(name string, in map[string]any) string {
	base := func(k string) string { return filepath.Base(str(in, k)) }
	switch name {
	case "Bash":
		if d := str(in, "description"); d != "" {
			return strings.ToLower(d[:1]) + d[1:]
		}
		return "$ " + str(in, "command")
	case "Edit", "MultiEdit", "NotebookEdit":
		return "editing " + base("file_path")
	case "Write":
		return "writing " + base("file_path")
	case "Read":
		return "reading " + base("file_path")
	case "Grep":
		return "searching for " + str(in, "pattern")
	case "Glob":
		return "finding " + str(in, "pattern")
	case "Agent", "Task":
		if d := str(in, "description"); d != "" {
			return "subagent: " + d
		}
		return "running a subagent"
	case "WebSearch":
		return "searching the web: " + str(in, "query")
	case "WebFetch":
		if u, err := url.Parse(str(in, "url")); err == nil && u.Host != "" {
			return "reading " + u.Host
		}
		return "reading the web"
	case "TodoWrite", "TaskCreate", "TaskUpdate":
		return "updating its tasks"
	case "AskUserQuestion":
		return "asking you"
	}
	if strings.HasPrefix(name, "mcp__") {
		p := strings.Split(name, "__")
		return "using " + p[len(p)-1]
	}
	return strings.ToLower(name)
}

// withDoing fills Agent.Doing for every working agent whose session is known.
func withDoing(doc *Doc) {
	for i := range doc.Agents {
		a := &doc.Agents[i]
		if a.State == nil || *a.State != "working" {
			continue
		}
		sid := ""
		if a.Claude != nil {
			sid = a.Claude.SessionID
		} else if a.ClaudeSession != nil {
			sid = *a.ClaudeSession
		}
		if d := doingOf(transcriptOf(sid)); d != "" {
			a.Doing = sp(d)
		}
	}
}

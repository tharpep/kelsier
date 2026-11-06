package fleet

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// Tasks counts a session's task list, Claude Code's own files under
// tasks/<session id>/*.json. Deleted tasks are not counted. Sidebar path only.
type Tasks struct {
	Done  int `json:"done"`
	Total int `json:"total"`
}

// readTasks is nil for no directory, no tasks, or any file that is not an
// object with a status: a count that may be short is not reported (invariant 3).
func readTasks(sid string) *Tasks {
	if sid == "" || strings.ContainsAny(sid, "/*?[") {
		return nil
	}
	dir := filepath.Join(claudeHome(), "tasks", sid)
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var t Tasks
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil
		}
		var m map[string]json.RawMessage
		if json.Unmarshal(b, &m) != nil {
			return nil
		}
		raw, ok := m["status"]
		if !ok {
			return nil
		}
		var status string
		_ = json.Unmarshal(raw, &status)
		switch status {
		case "deleted":
		case "completed":
			t.Done++
			t.Total++
		default:
			t.Total++
		}
	}
	if t.Total == 0 {
		return nil
	}
	return &t
}

func withTasks(doc *Doc) {
	for i := range doc.Agents {
		doc.Agents[i].Tasks = readTasks(sessionOf(doc.Agents[i]))
	}
}

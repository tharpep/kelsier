package fleet

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
)

// A title cache on disk was rejected: the head read is a stat and a few KB,
// and a cache could outlive the session it names.

var titleHeads = []int64{16 << 10, 128 << 10, 1 << 20}

type uline struct {
	Type    string `json:"type"`
	IsMeta  bool   `json:"isMeta"`
	Message struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

// titleOf is the first prompt typed in the transcript at path, "" when none.
func titleOf(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return ""
	}
	for _, n := range titleHeads {
		t, found := titleIn(f, st.Size(), n)
		if found || n >= st.Size() {
			return t
		}
	}
	return ""
}

// titleIn reads the first n bytes; found is false when a longer read may differ.
func titleIn(f *os.File, size, n int64) (string, bool) {
	n = min(n, size)
	buf := make([]byte, n)
	if _, err := f.ReadAt(buf, 0); err != nil && err != io.EOF {
		return "", true
	}
	lines := bytes.Split(buf, []byte("\n"))
	if n < size {
		lines = lines[:len(lines)-1] // the last is cut mid-line
	}
	for _, raw := range lines {
		var l uline
		if json.Unmarshal(raw, &l) != nil || l.Type != "user" || l.IsMeta {
			continue
		}
		if t := promptText(l.Message.Content); t != "" && !strings.HasPrefix(t, "<") {
			return strings.TrimSpace(strings.SplitN(t, "\n", 2)[0]), true
		}
	}
	return "", n >= size
}

// promptText is a message's text: a string, or its text blocks joined.
func promptText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return strings.TrimSpace(s)
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &parts) != nil {
		return ""
	}
	var texts []string
	for _, p := range parts {
		if p.Type == "text" {
			texts = append(texts, p.Text)
		}
	}
	return strings.TrimSpace(strings.Join(texts, "\n"))
}

// withTitle fills Agent.Title for every agent whose session is known.
func withTitle(doc *Doc) {
	for i := range doc.Agents {
		a := &doc.Agents[i]
		sid := ""
		if a.Claude != nil {
			sid = a.Claude.SessionID
		} else if a.ClaudeSession != nil {
			sid = *a.ClaudeSession
		}
		if t := titleOf(transcriptOf(sid)); t != "" {
			a.Title = sp(t)
		}
	}
}

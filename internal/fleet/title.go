package fleet

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
	"unicode"
)

const titleHead = 128 << 10

type uline struct {
	Type    string `json:"type"`
	IsMeta  bool   `json:"isMeta"`
	Message struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

// titleOf is the first prompt typed in the transcript at path, "" when none.
// A longer head was rejected: every agent would pay for it on every load.
func titleOf(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	buf, err := io.ReadAll(io.LimitReader(f, titleHead+1))
	if err != nil {
		return ""
	}
	lines := bytes.Split(buf, []byte("\n"))
	if len(buf) > titleHead {
		lines = lines[:len(lines)-1]
	}
	for _, raw := range lines {
		var l uline
		if json.Unmarshal(raw, &l) != nil || l.Type != "user" || l.IsMeta {
			continue
		}
		if t := promptText(l.Message.Content); t != "" && !strings.HasPrefix(t, "<") {
			return cleanTitle(strings.SplitN(t, "\n", 2)[0])
		}
	}
	return ""
}

func cleanTitle(s string) string {
	return strings.Join(strings.Fields(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)), " ")
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

// withTitle fills Agent.Title from each agent's transcript, paths[i] for Agents[i].
func withTitle(doc *Doc, paths []string) {
	for i := range doc.Agents {
		if t := titleOf(paths[i]); t != "" {
			doc.Agents[i].Title = sp(t)
		}
	}
}

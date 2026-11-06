package fleet

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTitle(t *testing.T) {
	f := filepath.Join(t.TempDir(), "s.jsonl")
	write := func(lines ...string) { _ = os.WriteFile(f, []byte(strings.Join(lines, "\n")+"\n"), 0o600) }
	snap := `{"type":"file-history-snapshot"}`
	caveat := `{"type":"user","message":{"content":"<local-command-caveat>ran it</local-command-caveat>"}}`
	clear := `{"type":"user","message":{"content":"<command-name>/clear</command-name>"}}`
	meta := `{"type":"user","isMeta":true,"message":{"content":[{"type":"text","text":"Base directory for this skill"}]}}`
	blank := `{"type":"user","message":{"content":"  \n"}}`
	real := `{"type":"user","message":{"content":"Go to PR 30, review, begin phase 1.\nThen the second line."}}`

	write(snap, caveat, clear, meta, blank, real)
	if got := titleOf(f); got != "Go to PR 30, review, begin phase 1." {
		t.Errorf("after slash commands: %q", got)
	}
	write(snap, `{"type":"user","message":{"content":[{"type":"text","text":"Fix the"},{"type":"text","text":"parser"}]}}`)
	if got := titleOf(f); got != "Fix the" {
		t.Errorf("text blocks: %q", got)
	}
	write(snap, caveat, `{"type":"assistant","message":{"content":"hi"}}`)
	if got := titleOf(f); got != "" {
		t.Errorf("no prompt yet: %q", got)
	}
	_ = os.WriteFile(f, nil, 0o600)
	if got := titleOf(f); got != "" {
		t.Errorf("empty file: %q", got)
	}
	if titleOf(filepath.Join(t.TempDir(), "none.jsonl")) != "" {
		t.Error("a missing file has no title")
	}

	// a first line longer than the first read is found by reading further
	long := strings.Repeat("x", int(titleHeads[0]))
	write(snap, `{"type":"user","message":{"content":"<system-reminder>`+long+`</system-reminder>"}}`, real)
	if got := titleOf(f); got != "Go to PR 30, review, begin phase 1." {
		t.Errorf("past the first read: %q", got)
	}
}

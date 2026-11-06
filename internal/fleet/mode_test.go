package fleet

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWithMode(t *testing.T) {
	sd := t.TempDir()
	if err := os.WriteFile(filepath.Join(sd, "%1.mode"), []byte("plan\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sd, "%3.mode"), []byte("acceptEdits\n"), 0600); err != nil {
		t.Fatal(err)
	}
	p1, p2, p3 := "%1", "%2", "%3"
	doc := Doc{Agents: []Agent{{PaneID: &p1}, {PaneID: &p2}, {PaneID: &p3}, {}}}
	withMode(&doc, sd, map[string]bool{"%3": true})

	if m := doc.Agents[0].Mode; m == nil || *m != "plan" {
		t.Errorf("agent with a .mode file: mode = %v, want plan", m)
	}
	if m := doc.Agents[1].Mode; m != nil {
		t.Errorf("agent with no .mode file: mode = %q, want nil", *m)
	}
	if m := doc.Agents[2].Mode; m != nil {
		t.Errorf("stale pane adopted a mode: %q", *m)
	}
	if m := doc.Agents[3].Mode; m != nil {
		t.Errorf("agent with no pane: mode = %q, want nil", *m)
	}
}

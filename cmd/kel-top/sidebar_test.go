package main

import (
	"strings"
	"testing"

	"github.com/tharpep/kelsier/internal/fleet"
)

func f64(v float64) *float64 { return &v }

func sampleDoc() fleet.Doc {
	working := "working"
	ports := []int{3000}
	none := []int{}
	panes := []fleet.Pane{
		{PaneID: "%2", WindowID: "@1", WindowIndex: f64(1), WindowName: "auth-fix-with-a-long-name",
			Group: "api-gw", PaneIndex: f64(1), Kind: "agent", Agent: true, Ports: &none},
		{PaneID: "%3", WindowID: "@1", WindowIndex: f64(1), WindowName: "auth-fix-with-a-long-name",
			Group: "api-gw", PaneIndex: f64(2), Kind: "server", Cmd: "node", Ports: &ports},
		{PaneID: "%5", WindowID: "@2", WindowIndex: f64(2), WindowName: "zsh",
			Group: "", PaneIndex: f64(1), Kind: "shell", Ports: &none, Current: true},
	}
	return fleet.Doc{
		Agents: []fleet.Agent{{WindowID: strPtr("@1"), State: &working}},
		Panes:  &panes,
	}
}

func strPtr(s string) *string { return &s }

func TestSidebarRows(t *testing.T) {
	rows := sbRows(sampleDoc())
	var got []string
	for _, r := range rows {
		if r.pane == nil {
			got = append(got, "#"+r.group)
		} else {
			got = append(got, r.pane.PaneID)
		}
	}
	// groups in name order, an empty group shown as misc, panes by index
	want := "#api-gw %2 %3 #misc %5"
	if strings.Join(got, " ") != want {
		t.Errorf("rows: got %q, want %q", strings.Join(got, " "), want)
	}
}

// Every width from 0 up must render without panicking: tmux squeezes the
// pane on a resize before kel re-pins it. The first version died at 1 column.
func TestSidebarRendersAtAnyWidth(t *testing.T) {
	for w := 0; w <= 40; w++ {
		m := sbModel{rows: sbRows(sampleDoc()), sel: "%3", width: w, height: 10}
		out := m.render()
		if w >= 15 && !strings.Contains(out, "node :3000") {
			t.Errorf("width %d: server row missing:\n%s", w, out)
		}
	}
	m := sbModel{rows: sbRows(sampleDoc()), sel: "%2", width: 32, height: 10}
	out := m.render()
	if !strings.Contains(out, "›") {
		t.Error("the current pane is not marked")
	}
	if !strings.Contains(out, "*") {
		t.Error("a working agent shows no state glyph")
	}
}

func TestParseVisible(t *testing.T) {
	cases := map[string]bool{"1 1": true, "1 2\n": true, "0 1": false, "1 0": false, "": false, "x": false}
	for in, want := range cases {
		if parseVisible(in) != want {
			t.Errorf("parseVisible(%q) != %v", in, want)
		}
	}
}

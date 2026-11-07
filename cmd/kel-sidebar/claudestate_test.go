package main

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/tharpep/kelsier/internal/fleet"
)

func stubQuiet(t *testing.T) *[][]string {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	var calls [][]string
	old := runKelQuiet
	runKelQuiet = func(args ...string) tea.Cmd {
		calls = append(calls, args)
		return nil
	}
	t.Cleanup(func() { runKelQuiet = old })
	return &calls
}

func taggedDoc(state string) fleet.Doc {
	sp := func(s string) *string { return &s }
	return fleet.Doc{Agents: []fleet.Agent{{
		Name: "docs", Managed: true, WindowID: sp("@1"), PaneID: sp("%7"),
		State: sp(state), Note: sp("permission prompt"), StateFrom: "claude",
		Claude: &fleet.ClaudeView{SessionID: "sess-B", At: 1234},
	}}}
}

func TestLoadedWritesClaudeStateOnce(t *testing.T) {
	calls := stubQuiet(t)
	m := sbModel{width: 32, height: 40, visible: true}
	m.Update(sbLoaded{taggedDoc("waiting")})
	want := [][]string{{"_claude_state", "%7", "waiting", "1234", "permission prompt", "sess-B", ""}}
	if !reflect.DeepEqual(*calls, want) {
		t.Errorf("calls %v, want %v", *calls, want)
	}
}

func TestHiddenSidebarWritesNothing(t *testing.T) {
	calls := stubQuiet(t)
	m := sbModel{width: 32, height: 40}
	m.Update(sbLoaded{taggedDoc("waiting")})
	if len(*calls) != 0 {
		t.Errorf("a hidden sidebar ran %v", *calls)
	}
}

func TestSameStateTwiceOneCall(t *testing.T) {
	calls := stubQuiet(t)
	m := sbModel{width: 32, height: 40, visible: true}
	nm, _ := m.Update(sbLoaded{taggedDoc("waiting")})
	nm, _ = nm.Update(sbLoaded{taggedDoc("waiting")})
	if len(*calls) != 1 {
		t.Fatalf("two loads, %d calls", len(*calls))
	}
	nm, _ = nm.Update(sbLoaded{taggedDoc("working")})
	if len(*calls) != 2 {
		t.Fatalf("a new state: %d calls", len(*calls))
	}
	d := taggedDoc("working")
	d.Agents[0].StateFrom = ""
	nm, _ = nm.Update(sbLoaded{d})
	nm.Update(sbLoaded{taggedDoc("working")})
	if len(*calls) != 3 {
		t.Errorf("after the tag cleared and came back: %d calls", len(*calls))
	}
}

func TestClaudeWritesRunInSequence(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	old := runKelQuiet
	runKelQuiet = func(args ...string) tea.Cmd { return func() tea.Msg { return nil } }
	t.Cleanup(func() { runKelQuiet = old })
	d := taggedDoc("waiting")
	second := d.Agents[0]
	p := "%8"
	second.PaneID = &p
	d.Agents = append(d.Agents, second)
	m := sbModel{width: 32, height: 40, visible: true}
	_, cmd := m.Update(sbLoaded{d})
	if cmd == nil {
		t.Fatal("no command")
	}
	if got := fmt.Sprintf("%T", cmd()); got != "tea.sequenceMsg" {
		t.Errorf("two writes ran as %s, want tea.sequenceMsg", got)
	}
}

func TestKnownSessionIsPassedOn(t *testing.T) {
	calls := stubQuiet(t)
	d := taggedDoc("waiting")
	s := "sess-B"
	d.Agents[0].ClaudeSession = &s
	m := sbModel{width: 32, height: 40, visible: true}
	m.Update(sbLoaded{d})
	if got := (*calls)[0][6]; got != "sess-B" {
		t.Errorf("known session %q", got)
	}
}

func unseenDoc(window string) fleet.Doc {
	sp := func(s string) *string { return &s }
	since, panes := 100.0, []fleet.Pane{}
	return fleet.Doc{Panes: &panes, Agents: []fleet.Agent{{
		Name: "docs", Group: "g", Managed: true, WindowID: sp(window), PaneID: sp("%7"),
		State: sp("done"), StateSince: &since, Unseen: true,
	}}}
}

func TestShownWindowIsSeenAtDraw(t *testing.T) {
	stubQuiet(t)
	m := sbModel{width: 32, height: 40, visible: true, ui: uiState{myWindow: "@1"}}
	nm, _ := m.Update(sbLoaded{unseenDoc("@1")})
	got := nm.(sbModel)
	if got.doc.Agents[0].Unseen {
		t.Error("the shown sidebar's own window still reads unseen")
	}
	v := buildView(got.doc, got.ui)
	if n := needNames(v); n != "" {
		t.Errorf("needs you: %q", n)
	}
	out, _ := renderView(v, 32, 0, 1792018800)
	if strings.Contains(plain(out), "Needs you") {
		t.Errorf("frame draws Needs you:\n%s", plain(out))
	}
}

func TestOtherWindowStillNeedsYou(t *testing.T) {
	stubQuiet(t)
	m := sbModel{width: 32, height: 40, visible: true, ui: uiState{myWindow: "@1"}}
	nm, _ := m.Update(sbLoaded{unseenDoc("@2")})
	got := nm.(sbModel)
	if n := needNames(buildView(got.doc, got.ui)); n != "docs" {
		t.Errorf("needs you: %q", n)
	}
}

func TestHiddenSidebarLeavesUnseenAlone(t *testing.T) {
	stubQuiet(t)
	m := sbModel{width: 32, height: 40, ui: uiState{myWindow: "@1"}}
	nm, _ := m.Update(sbLoaded{unseenDoc("@1")})
	if !nm.(sbModel).doc.Agents[0].Unseen {
		t.Error("a hidden sidebar cleared Unseen")
	}
}

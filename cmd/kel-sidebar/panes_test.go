package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNewMenuOffersAShellWindow(t *testing.T) {
	var found bool
	for _, a := range newMenu() {
		if a == (sbAct{"t", "Shell window"}) {
			found = true
		}
	}
	if !found {
		t.Fatalf("+ New has no Shell window: %v", newMenu())
	}
	m, rec := peekModel(t, "a:@1")
	press(m, "t")
	if !strings.HasPrefix(*rec, "shell ") {
		t.Errorf("t ran %q, want kel shell <dir>", *rec)
	}
}

func pickTo(m sbModel, target, k string) sbModel {
	m.ui.sel = target
	return press(m, k)
}

func TestOwnWindowForAPlainPane(t *testing.T) {
	m, rec := peekModel(t, "%2")
	press(m, "m")
	if *rec != "_own %2" {
		t.Errorf("m on a pane sharing its window ran %q", *rec)
	}
	m, rec = peekModel(t, "%14")
	press(m, "m")
	if *rec != "" {
		t.Errorf("m on a pane alone in its window ran %q", *rec)
	}
}

func TestMoveBesidePicksATarget(t *testing.T) {
	m, rec := peekModel(t, "%2")
	m = press(m, "M")
	if m.mode != modePick {
		t.Fatalf("M left the mode at %v", m.mode)
	}
	if got := m.view().promptTitle; !strings.Contains(got, "move") {
		t.Errorf("the footer does not ask where: %q", got)
	}
	if m = pickTo(m, "a:@3", "enter"); *rec != "_join %2 %5 right" || m.mode != modeNormal {
		t.Errorf("enter on another agent ran %q, mode %v", *rec, m.mode)
	}
	m, rec = peekModel(t, "%2")
	pickTo(press(m, "M"), "%14", "-")
	if *rec != "_join %2 %14 down" {
		t.Errorf("- on a shell ran %q", *rec)
	}
	m, rec = peekModel(t, "%2")
	m = pickTo(press(m, "M"), "a:@1", "enter")
	if *rec != "" || m.mode != modePick || !m.msgBad {
		t.Errorf("a target in the same window ran %q, mode %v", *rec, m.mode)
	}
	if m = press(m, "esc"); m.mode != modeNormal || *rec != "" {
		t.Errorf("esc left mode %v, ran %q", m.mode, *rec)
	}
}

func TestSwapStaysInItsWindow(t *testing.T) {
	m, rec := peekModel(t, "%2")
	pickTo(press(m, "S"), "a:@1", "enter")
	if *rec != "_swap %2 %1" {
		t.Errorf("swap with the window's agent ran %q", *rec)
	}
	m, rec = peekModel(t, "%2")
	m = pickTo(press(m, "S"), "a:@3", "enter")
	if *rec != "" || !m.msgBad {
		t.Errorf("swap across windows ran %q", *rec)
	}
}

func TestAgentsOfferNoMove(t *testing.T) {
	m := drawerModel(t, "a:@1")
	for _, a := range entryMenu(m.view()) {
		if a.key == "M" {
			t.Errorf("an agent offers %v (backlog #42)", a)
		}
	}
}

func TestPaneFramesGolden(t *testing.T) {
	doc := loadFixture(t, "fifteen-three-repos")
	now := time.Now().Unix()
	if tm, err := time.Parse(time.RFC3339, doc.GeneratedAt); err == nil {
		now = tm.Unix()
	}
	at := func(sel string) sbModel {
		t.Setenv("XDG_STATE_HOME", t.TempDir())
		return sbModel{doc: doc, width: 32, height: 30, visible: true, ui: uiState{sel: sel, focused: true}}
	}
	menu := at("a:@1")
	menu.menu = &sbMenu{title: "New", items: newMenu(), y: 1}
	pick := press(at("%2"), "M")
	pick.ui.sel = "a:@2"
	refused := pickTo(press(at("%2"), "M"), "a:@1", "enter")
	for name, m := range map[string]sbModel{
		"new-menu":     menu,
		"pane-drawer":  press(at("%2"), "right"),
		"pick-move":    pick,
		"pick-refused": refused,
	} {
		got, _ := renderView(m.view(), 32, 30, now)
		gold := filepath.Join(fixtures, "fifteen-three-repos."+name+".32x30.golden")
		if *update {
			if err := os.WriteFile(gold, []byte(got+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		want, err := os.ReadFile(gold)
		if err != nil {
			t.Fatalf("%s: %v", gold, err)
		}
		if got+"\n" != string(want) {
			t.Errorf("%s changed:\n--- got\n%s\n--- want\n%s", gold, plain(got), plain(string(want)))
		}
	}
}

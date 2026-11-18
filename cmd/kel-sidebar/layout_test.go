package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestLayoutsMatchKel proves the menu offers exactly kel's layouts, each for
// the pane counts `kel layout` accepts (layout_table in bin/kel).
func TestLayoutsMatchKel(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "bin", "kel"))
	if err != nil {
		t.Fatal(err)
	}
	_, rest, ok := strings.Cut(string(b), "layout_table() {\n  cat <<'EOF'\n")
	body, _, ok2 := strings.Cut(rest, "\nEOF\n")
	if !ok || !ok2 {
		t.Fatal("bin/kel has no layout_table heredoc")
	}
	var kel []string
	for _, l := range strings.Split(body, "\n") {
		f := strings.Fields(l)
		kel = append(kel, strings.Join(f[:3], " "))
	}
	var menu []string
	for _, l := range sbLayouts {
		menu = append(menu, l.name+" "+strconv.Itoa(l.min)+" "+strconv.Itoa(l.max))
	}
	if strings.Join(kel, ",") != strings.Join(menu, ",") {
		t.Errorf("kel's layouts %v, the menu's %v", kel, menu)
	}
}

func TestLayoutMenuOffersWhatFits(t *testing.T) {
	for n, want := range map[int]string{
		1: "",
		2: "below editor side",
		3: "below editor main columns",
		4: "below editor main grid columns",
	} {
		var got []string
		for _, l := range layoutsFor(n) {
			got = append(got, l.name)
		}
		if strings.Join(got, " ") != want {
			t.Errorf("%d panes offer %v, want %q", n, got, want)
		}
	}
}

func TestLayoutMenuAppliesAndUndoes(t *testing.T) {
	m, rec := peekModel(t, "a:@1")
	m = press(m, "L")
	if m.layouts == nil || m.layouts.win != "@1" || m.layouts.title != "Layout · kelsier's window" {
		t.Fatalf("L opened %+v", m.layouts)
	}
	if got := press(m, "3"); *rec != "layout side @1" || got.layouts != nil {
		t.Errorf("3 ran %q and left the menu %v", *rec, got.layouts)
	}
	*rec = ""
	press(press(press(m, "j"), "j"), "enter")
	if *rec != "layout side @1" {
		t.Errorf("j j enter ran %q", *rec)
	}
	*rec = ""
	if got := press(m, "u"); *rec != "" || got.layouts != nil {
		t.Errorf("u with nothing to undo ran %q", *rec)
	}
	if err := os.MkdirAll(sbStatePath(""), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sbStatePath("@1.layout"), []byte("x\n%1 %2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	press(m, "u")
	if *rec != "layout undo @1" {
		t.Errorf("u ran %q", *rec)
	}
	if got := press(m, "esc"); got.layouts != nil {
		t.Errorf("esc left the layout menu open")
	}
	m, rec = peekModel(t, "a:@3")
	if m = press(m, "L"); m.layouts != nil || !m.msgBad {
		t.Errorf("L on an agent alone in its window opened %+v", m.layouts)
	}
}

func TestLayoutMenuEntries(t *testing.T) {
	for _, sel := range []string{"a:@1", "%2"} {
		var found bool
		for _, a := range entryMenu(atModel(t, sel).view()) {
			found = found || a == sbAct{"L", "Layout ▸"}
		}
		if !found {
			t.Errorf("%s's menu has no Layout ▸", sel)
		}
	}
	m, _ := peekModel(t, "a:@1")
	m.menu = &sbMenu{title: "kel", items: kelMenu(m.view())}
	if m = press(m, "L"); m.layouts == nil {
		t.Errorf("L from kel's menu did not open the layouts")
	}
}

func TestLayoutMenuGolden(t *testing.T) {
	doc := loadFixture(t, "fifteen-three-repos")
	now := time.Now().Unix()
	if tm, err := time.Parse(time.RFC3339, doc.GeneratedAt); err == nil {
		now = tm.Unix()
	}
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	m := sbModel{doc: doc, width: 32, height: 36, visible: true, ui: uiState{sel: "a:@1", focused: true}}
	m = press(m, "L")
	m = press(m, "j")
	got, _ := renderView(m.view(), 32, 36, now)
	gold := filepath.Join(fixtures, "fifteen-three-repos.layout-menu.32x36.golden")
	if *update {
		if err := os.WriteFile(gold, []byte(got+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(gold)
	if err != nil {
		t.Fatalf("%s: %v", gold, err)
	}
	if got+"\n" != string(want) {
		t.Errorf("%s changed:\n--- got\n%s\n--- want\n%s", gold, plain(got), plain(string(want)))
	}
}

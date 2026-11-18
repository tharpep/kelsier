package main

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// The foot is what needs you, counted, then settings and help on the right,
// each a click; nothing waiting, it is settings and help alone. It never
// overflows the pane.
func TestSidebarFooter(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	doc := loadFixture(t, "one-agent")
	out, refs := renderView(buildView(doc, uiState{}), 32, 30, 0)
	l := strings.Split(plain(out), "\n")
	last, r := l[len(l)-1], refs[len(refs)-1]
	if strings.TrimSpace(last) != "≡  ?" {
		t.Errorf("footer: %q", last)
	}
	if r.hitAt(27) != "open:settings" || r.hitAt(30) != "open:help" || r.hitAt(2) != "" {
		t.Errorf("footer clicks: %+v", r.hits)
	}
	m := sbModel{doc: doc, width: 32, height: 30, visible: true}
	nm, _ := m.Update(tea.MouseClickMsg{X: 30, Y: 29, Button: tea.MouseLeft})
	if !nm.(sbModel).help {
		t.Error("? did not open the keys")
	}
	nm, _ = m.Update(tea.MouseClickMsg{X: 27, Y: 29, Button: tea.MouseLeft})
	if !nm.(sbModel).settings {
		t.Error("≡ did not open settings")
	}
	m = sbModel{doc: loadFixture(t, "fifteen-three-repos"), width: 32, height: 30, visible: true}
	var ran string
	old := runKel
	runKel = func(dir string, env []string, args ...string) tea.Cmd {
		ran = strings.Join(args, " ")
		return nil
	}
	t.Cleanup(func() { runKel = old })
	m.Update(tea.MouseClickMsg{X: 3, Y: 29, Button: tea.MouseLeft})
	if ran != "jump" {
		t.Errorf("a click on the count ran %q, want kel jump", ran)
	}
	for w := 7; w <= 40; w++ {
		out, _ := renderView(buildView(m.doc, uiState{}), w, 30, 0)
		l := strings.Split(plain(out), "\n")
		if len([]rune(l[len(l)-1])) > w {
			t.Errorf("width %d: footer %q overflows", w, l[len(l)-1])
		}
	}
}

package fleet

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadPaneCards(t *testing.T) {
	td := t.TempDir()

	if c := readPaneCards(td, "%1"); c != nil {
		t.Errorf("missing file returned %+v, want nil", c)
	}

	cf := filepath.Join(td, "%1.cards")
	line1 := `{"kind":"update","title":"Step 1","at":100}`
	line2 := `{"kind":"result","title":"Auth ready","at":200}`
	if err := os.WriteFile(cf, []byte(line1+"\n"+line2+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	c := readPaneCards(td, "%1")
	if c == nil {
		t.Fatal("readPaneCards returned nil, want cards")
	}
	if c.Count != 2 {
		t.Errorf("count = %d, want 2", c.Count)
	}
	if !c.Unseen {
		t.Errorf("unseen = false, want true")
	}
	if c.Latest == nil || c.Latest.Kind != "result" || c.Latest.Title != "Auth ready" {
		t.Errorf("latest = %+v, want result Auth ready", c.Latest)
	}

	sf := filepath.Join(td, "%1.cards-seen")
	if err := os.WriteFile(sf, []byte("250\n"), 0644); err != nil {
		t.Fatal(err)
	}
	c = readPaneCards(td, "%1")
	if c == nil || c.Unseen {
		t.Errorf("after cards-seen, unseen = true, want false")
	}

	corruptCf := filepath.Join(td, "%2.cards")
	if err := os.WriteFile(corruptCf, []byte("not json {{{"), 0644); err != nil {
		t.Fatal(err)
	}
	c2 := readPaneCards(td, "%2")
	if c2 == nil {
		t.Fatal("corrupt file returned nil, want unreadable")
	}
	if c2.Latest == nil || c2.Latest.Kind != "unreadable" {
		t.Errorf("corrupt file latest = %+v, want unreadable", c2.Latest)
	}
}

func TestWithCardsStaleReusedPane(t *testing.T) {
	td := t.TempDir()
	cf := filepath.Join(td, "%9.cards")
	if err := os.WriteFile(cf, []byte(`{"kind":"result","title":"Old card","at":100}`+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	ps := []Pane{
		{PaneID: "%9", Cmd: "bash"},
	}
	doc := Doc{Panes: &ps}
	stale := map[string]bool{"%9": true}
	withCards(&doc, td, stale)

	if (*doc.Panes)[0].Cards != nil {
		t.Errorf("stale reused pane adopted cards: %+v", (*doc.Panes)[0].Cards)
	}
}

package fleet

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type PaneCards struct {
	Count  int            `json:"count"`
	Unseen bool           `json:"unseen"`
	Latest *PaneCardEntry `json:"latest"`
}

type PaneCardEntry struct {
	Kind  string   `json:"kind"`
	Title string   `json:"title"`
	At    *float64 `json:"at"`
}

type cardRecord struct {
	Kind  string   `json:"kind"`
	Title string   `json:"title"`
	At    *float64 `json:"at"`
}

func readPaneCards(sd, pid string) *PaneCards {
	cf := filepath.Join(sd, pid+".cards")
	f, err := os.Open(cf)
	if err != nil {
		return nil
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	var count int
	var lastLine string
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			count++
			lastLine = line
		}
	}
	if count == 0 {
		return nil
	}

	var rec cardRecord
	if err := json.Unmarshal([]byte(lastLine), &rec); err != nil || rec.Kind == "" || rec.Title == "" {
		return &PaneCards{
			Count:  count,
			Unseen: true,
			Latest: &PaneCardEntry{
				Kind:  "unreadable",
				Title: "unreadable",
			},
		}
	}

	unseen := true
	sf := filepath.Join(sd, pid+".cards-seen")
	if b, err := os.ReadFile(sf); err == nil {
		if seenAt, err := strconv.ParseFloat(strings.TrimSpace(string(b)), 64); err == nil {
			if rec.At != nil && *rec.At <= seenAt {
				unseen = false
			}
		}
	}

	return &PaneCards{
		Count:  count,
		Unseen: unseen,
		Latest: &PaneCardEntry{
			Kind:  rec.Kind,
			Title: rec.Title,
			At:    rec.At,
		},
	}
}

func withCards(doc *Doc, sd string, stalePanes map[string]bool) {
	paneCards := map[string]*PaneCards{}
	if doc.Panes != nil {
		for i := range *doc.Panes {
			p := &(*doc.Panes)[i]
			if stalePanes[p.PaneID] {
				continue
			}
			c := readPaneCards(sd, p.PaneID)
			p.Cards = c
			if c != nil {
				paneCards[p.PaneID] = c
			}
		}
	}
	for i := range doc.Agents {
		a := &doc.Agents[i]
		if a.PaneID != nil {
			if stalePanes[*a.PaneID] {
				continue
			}
			if c, ok := paneCards[*a.PaneID]; ok {
				a.Cards = c
			} else {
				c := readPaneCards(sd, *a.PaneID)
				a.Cards = c
			}
		}
	}
}

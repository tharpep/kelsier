package fleet

import (
	"os"
	"path/filepath"
	"strings"
)

func readMode(sd, pid string) *string {
	b, err := os.ReadFile(filepath.Join(sd, pid+".mode"))
	if err != nil {
		return nil
	}
	m := strings.TrimSpace(string(b))
	if m == "" {
		return nil
	}
	return &m
}

func withMode(doc *Doc, sd string, stalePanes map[string]bool) {
	for i := range doc.Agents {
		a := &doc.Agents[i]
		if a.PaneID == nil || stalePanes[*a.PaneID] {
			continue
		}
		a.Mode = readMode(sd, *a.PaneID)
	}
}

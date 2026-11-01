package fleet

import (
	"strings"
	"sync"
)

func withAgy(doc *Doc) {
	var wg sync.WaitGroup
	for i := range doc.Agents {
		a := &doc.Agents[i]
		if a.PaneID == nil || a.State == nil {
			continue
		}
		st := *a.State
		if st != "working" && st != "idle" {
			continue
		}
		paneID := *a.PaneID
		wg.Add(1)
		go func(a *Agent, pid string) {
			defer wg.Done()
			txt := tmuxOut("capture-pane", "-p", "-t", pid)
			if txt == "" {
				return
			}
			if waiting, note := checkAgyWaiting(txt); waiting {
				a.State = sp("waiting")
				a.Note = sp(note)
			}
		}(a, paneID)
	}
	wg.Wait()
}

func checkAgyWaiting(txt string) (bool, string) {
	var nonBlank []string
	for _, l := range strings.Split(txt, "\n") {
		if strings.TrimSpace(l) != "" {
			nonBlank = append(nonBlank, l)
		}
	}
	start := 0
	if len(nonBlank) > 20 {
		start = len(nonBlank) - 20
	}
	tail := strings.Join(nonBlank[start:], "\n")

	switch {
	case strings.Contains(tail, "Requesting permission for:") ||
		strings.Contains(tail, "Run this command?") ||
		strings.Contains(tail, "> 1. Yes, run command") ||
		strings.Contains(tail, "1. Yes, run command") ||
		strings.Contains(tail, "Yes, and always allow") ||
		strings.Contains(tail, "Grant permission") ||
		strings.Contains(tail, "Do you want to proceed?") ||
		strings.Contains(tail, "Are you sure you want to"):
		return true, "permission"

	case strings.Contains(tail, "(Recommended)") ||
		strings.Contains(tail, "select all that apply") ||
		strings.Contains(tail, "Press Enter to submit"):
		return true, "your input"
	}

	return false, ""
}

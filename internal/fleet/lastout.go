package fleet

import (
	"os/exec"
	"strings"
	"sync"
)

// LastOutput returns the last non-blank line each window printed, keyed by
// window id, for the windows named.
//
// Deliberately `capture-pane -p` WITHOUT -e: with escapes on, taking the tail
// chops an ANSI sequence mid-way and the colour bleeds across everything drawn
// after it. Control characters that survive anyway are stripped here.
//
// This is NOT part of the fleet document — it is a per-refresh read for
// `kel top` only. Adding it to Doc would change the contract the bash and Go
// implementations are diffed against, for data no other surface wants.
func LastOutput(windowIDs []string) map[string]string {
	out := make(map[string]string, len(windowIDs))
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, wid := range windowIDs {
		if wid == "" {
			continue
		}
		wg.Add(1)
		go func(w string) {
			defer wg.Done()
			b, err := exec.Command("tmux", "capture-pane", "-t", w, "-p").Output()
			if err != nil {
				return
			}
			var last string
			for _, l := range strings.Split(string(b), "\n") {
				if s := strings.TrimSpace(sanitize(l)); s != "" {
					last = s
				}
			}
			if last == "" {
				return
			}
			mu.Lock()
			out[w] = last
			mu.Unlock()
		}(wid)
	}
	wg.Wait()
	return out
}

// sanitize drops control characters and any stray escape sequences, so one
// pane cannot corrupt the table drawn around it.
func sanitize(s string) string {
	var b strings.Builder
	inEsc := false
	for _, r := range s {
		switch {
		case inEsc:
			// a CSI/OSC run ends at the first letter or terminator
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '\a' {
				inEsc = false
			}
		case r == 0x1b:
			inEsc = true
		case r == '\t':
			b.WriteByte(' ')
		case r < 0x20 || r == 0x7f:
			// drop
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

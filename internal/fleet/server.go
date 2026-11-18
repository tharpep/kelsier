package fleet

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// serverFormat names the tmux server: its pid and its start time. Pane and
// window ids restart from %0 and @0 with a new server, so a file keyed by one
// belongs to whichever server it was written under (backlog #44).
const serverFormat = "#{pid} #{start_time}"

// ServerID is serverFormat for the running server, or "" with none.
func ServerID() string {
	return strings.TrimSpace(tmuxOut("display-message", "-p", serverFormat))
}

// reaped lists what a new server makes stale: every pane- and window-keyed
// file, the session-to-pane stash, the waits, and the banner (pane ids too).
var reaped = []string{"%*", "@*", filepath.Join(".stash", "*"), filepath.Join(".waiting", "*"), "banner"}

// ReapStale removes the files an earlier tmux server left, once per server:
// the `server` file records the server they were last reaped for. A file
// last written before the server started cannot be about any of its panes.
// The test is by mtime, not by deleting everything, so a file the new server's
// hooks wrote before the first reap survives it, and two reaps at once agree.
// Not caught: a file the old server wrote in the second the new one started.
func ReapStale(sd, id string) {
	f := strings.Fields(id)
	if len(f) != 2 {
		return
	}
	start, err := strconv.ParseInt(f[1], 10, 64)
	if err != nil || start <= 0 {
		return
	}
	marker := filepath.Join(sd, "server")
	if b, err := os.ReadFile(marker); err == nil && strings.TrimSpace(string(b)) == id {
		return
	}
	for _, pat := range reaped {
		matches, _ := filepath.Glob(filepath.Join(sd, pat))
		for _, m := range matches {
			if fi, err := os.Lstat(m); err == nil && fi.Mode().IsRegular() && fi.ModTime().Unix() < start {
				_ = os.Remove(m)
			}
		}
	}
	WriteAtomic(marker, []byte(id+"\n"))
}

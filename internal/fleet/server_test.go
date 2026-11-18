package fleet

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestReapStale(t *testing.T) {
	sd := t.TempDir()
	start := time.Now().Add(-time.Hour)
	old := start.Add(-time.Minute)
	if err := os.MkdirAll(filepath.Join(sd, ".waiting"), 0o700); err != nil {
		t.Fatal(err)
	}
	write := func(name string, at time.Time) string {
		p := filepath.Join(sd, name)
		if err := os.WriteFile(p, []byte("x\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, at, at); err != nil {
			t.Fatal(err)
		}
		return p
	}
	stale := []string{write("%3.mode", old), write("%3.state", old), write("@2.seen", old), write(".waiting/s1", old), write("banner", old)}
	fresh := []string{write("%4.state", start), write("%3.cards", time.Now())}
	keep := []string{write("snapshot.json", old), write("quota", old)}

	id := "123 " + strconv.FormatInt(start.Unix(), 10)
	ReapStale(sd, id)
	for _, p := range stale {
		if _, err := os.Stat(p); err == nil {
			t.Errorf("%s: written before the server started, still there", filepath.Base(p))
		}
	}
	for _, p := range append(fresh, keep...) {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s: reaped, but it is the new server's or not pane-keyed", filepath.Base(p))
		}
	}
	if b, _ := os.ReadFile(filepath.Join(sd, "server")); string(b) != id+"\n" {
		t.Errorf("server = %q, want %q", b, id+"\n")
	}

	again := write("%5.mode", old)
	ReapStale(sd, id)
	if _, err := os.Stat(again); err != nil {
		t.Error("a second reap for the same server ran: it must happen once per server")
	}
}

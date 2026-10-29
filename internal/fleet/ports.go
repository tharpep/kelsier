package fleet

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Listening TCP ports per pane, found through the pane's process tree.
//
// Linux and WSL2 read /proc and fork nothing. macOS has no /proc, so it takes
// one `lsof` and one `ps` — `lsof` alone costs 100 ms and more, which the
// sidebar's refresh cannot pay every time. So the answer is cached for
// portsTTL in ports.json, keyed by pane pid: a pane that did not exist at the
// last scan forces a fresh one.

const portsTTL = 10 // seconds

type portsCache struct {
	At    int64            `json:"at"`
	Panes map[string][]int `json:"panes"` // pane pid -> ports
}

// panePorts returns pane pid -> listening ports. A nil map means "could not
// tell" (no /proc and no lsof) — callers render that as unknown, not none.
func panePorts(panePids []int) map[int][]int {
	if len(panePids) == 0 {
		return map[int][]int{}
	}
	cacheFile := filepath.Join(StateDir(), "ports.json")
	now := time.Now().Unix()
	if b, err := os.ReadFile(cacheFile); err == nil {
		var c portsCache
		if json.Unmarshal(b, &c) == nil && now-c.At < portsTTL && c.Panes != nil {
			out := map[int][]int{}
			hit := true
			for _, p := range panePids {
				v, ok := c.Panes[strconv.Itoa(p)]
				if !ok {
					hit = false
					break
				}
				out[p] = v
			}
			if hit {
				return out
			}
		}
	}

	var parent map[int]int
	var listen map[int][]int // pid -> ports it listens on
	if _, err := os.Stat("/proc/net/tcp"); err == nil {
		parent, listen = procScan(panePids)
	} else {
		parent, listen = lsofScan()
	}
	if parent == nil || listen == nil {
		return nil
	}

	children, _ := paneDescendants(parent, nil)
	out := map[int][]int{}
	for _, root := range panePids {
		seen := map[int]bool{}
		set := map[int]bool{}
		stack := []int{root}
		for len(stack) > 0 {
			pid := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if seen[pid] {
				continue
			}
			seen[pid] = true
			for _, port := range listen[pid] {
				set[port] = true
			}
			stack = append(stack, children[pid]...)
		}
		ports := []int{}
		for port := range set {
			ports = append(ports, port)
		}
		sort.Ints(ports)
		out[root] = ports
	}

	c := portsCache{At: now, Panes: map[string][]int{}}
	for k, v := range out {
		c.Panes[strconv.Itoa(k)] = v
	}
	if b, err := json.Marshal(c); err == nil {
		WriteAtomic(cacheFile, b)
	}
	return out
}

// parseProcNetTCP reads /proc/net/tcp or tcp6 and returns socket inode ->
// port for every socket in LISTEN (state 0A).
func parseProcNetTCP(s string) map[string]int {
	out := map[string]int{}
	for i, l := range strings.Split(s, "\n") {
		f := strings.Fields(l)
		if i == 0 || len(f) < 10 || f[3] != "0A" {
			continue
		}
		local := f[1]
		c := strings.LastIndex(local, ":")
		if c < 0 {
			continue
		}
		port, err := strconv.ParseInt(local[c+1:], 16, 32)
		if err != nil || f[9] == "0" {
			continue
		}
		out[f[9]] = int(port)
	}
	return out
}

// procScan: parent of each pid, and listening ports for panePids and their descendants.
func procScan(panePids []int) (parent map[int]int, listen map[int][]int) {
	inodes := map[string]int{}
	for _, f := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		if b, err := os.ReadFile(f); err == nil {
			for k, v := range parseProcNetTCP(string(b)) {
				inodes[k] = v
			}
		}
	}
	if len(inodes) == 0 {
		return map[int]int{}, map[int][]int{}
	}
	ents, err := os.ReadDir("/proc")
	if err != nil {
		return nil, nil
	}
	parent = map[int]int{}
	listen = map[int][]int{}
	for _, e := range ents {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		if b, err := os.ReadFile(filepath.Join("/proc", e.Name(), "stat")); err == nil {
			// "pid (comm) state ppid …" — comm may hold spaces and parens,
			// so split after the LAST ')'
			s := string(b)
			if r := strings.LastIndexByte(s, ')'); r >= 0 {
				if f := strings.Fields(s[r+1:]); len(f) >= 2 {
					if pp, err := strconv.Atoi(f[1]); err == nil {
						parent[pid] = pp
					}
				}
			}
		}
	}

	// Scope fd inspection strictly to panePids and their descendants.
	// On systems with thousands of processes, reading /proc/*/fd for every
	// unrelated process wastes significant I/O and CPU.
	_, relevant := paneDescendants(parent, panePids)

	for pid := range relevant {
		pidStr := strconv.Itoa(pid)
		fds, err := os.ReadDir(filepath.Join("/proc", pidStr, "fd"))
		if err != nil {
			continue // not ours to read — fine, not ours to report either
		}
		for _, fd := range fds {
			t, err := os.Readlink(filepath.Join("/proc", pidStr, "fd", fd.Name()))
			if err != nil || !strings.HasPrefix(t, "socket:[") {
				continue
			}
			if port, ok := inodes[strings.TrimSuffix(strings.TrimPrefix(t, "socket:["), "]")]; ok {
				listen[pid] = append(listen[pid], port)
			}
		}
	}
	return parent, listen
}

func paneDescendants(parent map[int]int, panePids []int) (children map[int][]int, relevant map[int]bool) {
	children = map[int][]int{}
	for c, p := range parent {
		children[p] = append(children[p], c)
	}
	relevant = map[int]bool{}
	for _, root := range panePids {
		stack := []int{root}
		for len(stack) > 0 {
			pid := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if relevant[pid] {
				continue
			}
			relevant[pid] = true
			stack = append(stack, children[pid]...)
		}
	}
	return children, relevant
}

// lsofScan is the macOS path: one lsof for listening sockets, one ps for the
// process tree.
func lsofScan() (parent map[int]int, listen map[int][]int) {
	if _, err := exec.LookPath("lsof"); err != nil {
		return nil, nil
	}
	b, err := exec.Command("lsof", "-nP", "-iTCP", "-sTCP:LISTEN", "-Fpn").Output()
	// lsof exits 1 when nothing matches — that is "no ports", not a failure
	if err != nil && len(b) == 0 {
		if ee, ok := err.(*exec.ExitError); !ok || ee.ExitCode() != 1 {
			return nil, nil
		}
	}
	listen = parseLsof(string(b))
	ps, err := exec.Command("ps", "-Ao", "pid=,ppid=").Output()
	if err != nil {
		return nil, nil
	}
	parent = map[int]int{}
	for _, l := range strings.Split(string(ps), "\n") {
		f := strings.Fields(l)
		if len(f) != 2 {
			continue
		}
		c, e1 := strconv.Atoi(f[0])
		p, e2 := strconv.Atoi(f[1])
		if e1 == nil && e2 == nil {
			parent[c] = p
		}
	}
	return parent, listen
}

// parseLsof reads `lsof -Fpn`: a `p<pid>` line, then `n<addr>:<port>` lines.
func parseLsof(s string) map[int][]int {
	out := map[int][]int{}
	pid := -1
	for _, l := range strings.Split(s, "\n") {
		switch {
		case strings.HasPrefix(l, "p"):
			if n, err := strconv.Atoi(l[1:]); err == nil {
				pid = n
			}
		case strings.HasPrefix(l, "n") && pid >= 0:
			if c := strings.LastIndex(l, ":"); c >= 0 {
				if port, err := strconv.Atoi(l[c+1:]); err == nil {
					out[pid] = append(out[pid], port)
				}
			}
		}
	}
	return out
}

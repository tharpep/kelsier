package fleet

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Quota is the account's rate limits as Claude Code last reported them
// (spec.md §7b). Per account, not per agent, so it is one file — `quota` in
// the state dir — written by `kel statusline` only from a payload that
// carries rate limits: an API-key session has none and never writes one.
//
// A figure that can no longer be true is null, never the old number
// (invariant 3): its window has reset since it was seen, or it was seen
// longer ago than the window is long.
type Quota struct {
	FiveHour *Window `json:"five_hour"`
	SevenDay *Window `json:"seven_day"`
	Spend    *Window `json:"spend"`
	At       float64 `json:"at"` // when it was observed
}

type Window struct {
	Pct      *float64 `json:"pct"`       // null: not reported, or stale
	ResetsAt *float64 `json:"resets_at"` // null: not reported
}

const (
	fiveHours = 5 * 3600
	sevenDays = 7 * 24 * 3600
)

// loadQuota reads `<pct5> <at5> <pct7> <at7> <spend> <atspend> <epoch>`; -1
// is "not reported", 0 for a reset is "not given". nil when there is no file
// or it cannot be read — the bar then shows no quota, which is the truth.
func loadQuota(sd string, now int64) *Quota {
	b, err := os.ReadFile(filepath.Join(sd, "quota"))
	if err != nil {
		return nil
	}
	f := strings.Fields(string(b))
	if len(f) != 7 {
		return nil
	}
	n := make([]float64, 7)
	for i, s := range f {
		v, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return nil
		}
		n[i] = v
	}
	at := n[6]
	win := func(pct, reset float64, span int64) *Window {
		if pct < 0 {
			return nil
		}
		w := &Window{}
		if reset > 0 {
			r := reset
			w.ResetsAt = &r
		}
		stale := (reset > 0 && int64(reset) <= now) || (span > 0 && now-int64(at) > span)
		if !stale {
			p := pct
			w.Pct = &p
		}
		return w
	}
	return &Quota{
		FiveHour: win(n[0], n[1], fiveHours),
		SevenDay: win(n[2], n[3], sevenDays),
		Spend:    win(n[4], n[5], 0),
		At:       at,
	}
}

// Here is where the focused pane stands — for the bar while the sidebar is
// hidden (spec.md §7b). One `git status` of one repo, asked for only then.
type Here struct {
	Group string `json:"group"`
	Git   *Git   `json:"git"` // null outside a repo, or when git could not answer
}

func loadHere(path string) *Here {
	if path == "" {
		return nil
	}
	h := &Here{Group: repoGroup(path)}
	if root, _, ok := repoRoot(path); ok {
		h.Git = gitPositions(map[string]bool{root: true})[root]
	}
	return h
}

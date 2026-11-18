package fleet

import (
	"fmt"
	"os"
	"os/exec"
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
	Spent    *float64 `json:"spent,omitempty"`
}

const (
	fiveHours = 5 * 3600
	sevenDays = 7 * 24 * 3600
)

func quotaCmd() string {
	return os.Getenv("KEL_QUOTA_CMD")
}

func quotaTTL() int64 {
	if v := os.Getenv("KEL_QUOTA_TTL"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			return n
		}
	}
	return 300
}

func quotaRetry(ttl int64) int64 {
	retry := int64(30)
	if v := os.Getenv("KEL_QUOTA_RETRY"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			retry = n
		}
	}
	return min(retry, ttl)
}

func prOff() bool {
	switch os.Getenv("KEL_PR") {
	case "0", "off", "no", "false":
		return true
	}
	return false
}

// refreshQuotaCmd runs quota_cmd detached if the cache is stale and KEL_PR is not off.
func refreshQuotaCmd(sd string, now int64) {
	cmdStr := quotaCmd()
	if cmdStr == "" || prOff() {
		return
	}
	ttl := quotaTTL()
	atFile := filepath.Join(sd, "quota-cmd.at")
	if b, err := os.ReadFile(atFile); err == nil {
		if f := strings.Fields(string(b)); len(f) > 0 {
			if ts, err := strconv.ParseInt(f[0], 10, 64); err == nil {
				wait := ttl
				if len(f) > 1 && f[1] == "fail" {
					wait = quotaRetry(ttl)
				}
				if now-ts < wait {
					return
				}
			}
		}
	}
	_ = os.MkdirAll(sd, 0o700)
	_ = os.WriteFile(atFile, []byte(strconv.FormatInt(now, 10)+"\n"), 0o644)

	target := filepath.Join(sd, "quota-cmd")
	script := fmt.Sprintf(`fail() { printf '%%s fail\n' "$(date +%%s)" > "%s"; exit 0; }
out=$(%s 2>/dev/null) || fail
set -- $out
pct=$1; reset=${2:-0}; spent=${3:--1}
case "$pct" in
  ''|*[!0-9.]*) fail ;;
esac
case "$reset" in
  ''|*[!0-9]*) reset=0 ;;
esac
case "$spent" in
  ''|*[!0-9.]*) spent=-1 ;;
esac
tmp=$(mktemp "%s/.qcmd.XXXXXX" 2>/dev/null) || fail
printf '%%s %%s %%d %%s\n' "$pct" "$reset" %d "$spent" > "$tmp" && mv -f "$tmp" "%s"
`, atFile, cmdStr, sd, now, target)

	cmd := exec.Command("sh", "-c", script)
	if err := cmd.Start(); err == nil {
		go func() { _ = cmd.Wait() }()
	}
}

// loadQuota reads `<pct5> <at5> <pct7> <at7> <spend> <atspend> <epoch>`; -1
// is "not reported", 0 for a reset is "not given". If spend is not reported in
// quota, quota-cmd (`<pct> <reset> <epoch> [<spent>]`, from KEL_QUOTA_CMD) is
// checked as a fallback.
func loadQuota(sd string, now int64) *Quota {
	var q *Quota
	b, err := os.ReadFile(filepath.Join(sd, "quota"))
	if err == nil {
		f := strings.Fields(string(b))
		if len(f) == 7 {
			n := make([]float64, 7)
			valid := true
			for i, s := range f {
				v, err := strconv.ParseFloat(s, 64)
				if err != nil {
					valid = false
					break
				}
				n[i] = v
			}
			if valid {
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
				q = &Quota{
					FiveHour: win(n[0], n[1], fiveHours),
					SevenDay: win(n[2], n[3], sevenDays),
					Spend:    win(n[4], n[5], 0),
					At:       at,
				}
			}
		}
	}

	// If spend is not reported by Claude Code, check quota_cmd fallback
	if q == nil || q.Spend == nil || q.Spend.Pct == nil {
		refreshQuotaCmd(sd, now)
		if cmdBytes, err := os.ReadFile(filepath.Join(sd, "quota-cmd")); err == nil {
			fields := strings.Fields(string(cmdBytes))
			if len(fields) >= 3 {
				pct, err1 := strconv.ParseFloat(fields[0], 64)
				reset, err2 := strconv.ParseFloat(fields[1], 64)
				at, err3 := strconv.ParseInt(fields[2], 10, 64)
				if err1 == nil && err2 == nil && err3 == nil && pct >= 0 && pct <= 100 {
					ttl := quotaTTL()
					stale := (reset > 0 && int64(reset) <= now) || (now-at > 2*ttl)
					if !stale {
						w := &Window{}
						p := pct
						w.Pct = &p
						if reset > 0 {
							r := reset
							w.ResetsAt = &r
						}
						if len(fields) >= 4 {
							if sp, err := strconv.ParseFloat(fields[3], 64); err == nil && sp >= 0 {
								w.Spent = &sp
							}
						}
						if q == nil {
							q = &Quota{At: float64(at)}
						}
						q.Spend = w
					}
				}
			}
		}
	}

	return q
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

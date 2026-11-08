package main

import "strconv"

// A pane this narrow is the rail: tmux squeezes a full sidebar through these
// widths on its way to the rail's own four columns.
const railMaxWidth = 6

const railFullID = "rail:full"

func isRail(w int) bool { return w >= 1 && w <= railMaxWidth }

// renderRail is the sidebar at four columns (spec.md §5b): what needs you, a
// dot per agent in the list's order, a mark when a server runs, and ≡ at the
// foot to bring the full sidebar back. No hover, no drawer.
func renderRail(v sbView, w, h int, now int64) (string, []lineRef) {
	var body []line
	if n := len(v.needs); n > 0 {
		body = append(body, line{" " + yellow + bold + "●" + strconv.Itoa(min(n, 99)) + reset, lineRef{id: needID(v.needs[0].key), kind: "rail"}})
	}
	for _, a := range v.agents {
		dot, colour := stateLook(a.agent.State, a.agent.Unseen, a.agent.StateSince, now)
		body = append(body, line{" " + colour + dot + reset, lineRef{id: agentID(a.key), kind: "rail"}})
	}
	if v.servers {
		body = append(body, line{" " + dim + "▪" + reset, lineRef{kind: "rail"}})
	}
	foot := line{" " + dim + "≡" + reset, lineRef{id: railFullID, kind: "rail"}}
	if h > 0 {
		body = body[:min(len(body), h-1)]
		for len(body) < h-1 {
			body = append(body, line{})
		}
	}
	return joinLines(append(body, foot), w)
}

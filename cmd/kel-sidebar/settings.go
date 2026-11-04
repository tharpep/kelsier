package main

// The settings screen: kel's config.toml as switches, steppers and choices,
// each a click (spec.md §5d, mouse first). Only settings kel has; the rest is
// one button away, in the file itself.

import (
	"fmt"
	"strconv"
	"strings"
)

type setKind int

const (
	setHead setKind = iota
	setBlank
	setToggle
	setStep
	setChoice
	setShow
	setButton
)

type setRow struct {
	name, label, value string
	kind               setKind
	on                 bool
	opts               []string // a choice's options; value is the chosen one
}

// configDefaults are bin/kel's, for a key the file does not set.
var configDefaults = map[string]string{
	"notify": "waiting error", "notify_bell": "on", "notify_desktop": "on", "notify_banner": "on",
	"sidebar_width": "32", "ctx_warn": "70", "pr": "on", "pr_ttl": "300", "agent": "claude",
	"cache_ttl": "300",
}

func cfgGet(c map[string]string, k string) string {
	if v, ok := c[k]; ok {
		return v
	}
	return configDefaults[k]
}

func isOn(v string) bool {
	switch v {
	case "0", "off", "no", "false":
		return false
	}
	return true
}

var prTTLs = []int{60, 120, 300, 600, 1800}

func settingRows(c map[string]string) []setRow {
	states := " " + cfgGet(c, "notify") + " "
	has := func(s string) bool { return strings.Contains(states, " "+s+" ") }
	wt := cfgGet(c, "worktrees")
	if wt == "" {
		wt = "beside the repo"
	}
	bg := "Terminal"
	if cfgGet(c, "sidebar_bg") != "" {
		bg = "Fixed"
	}
	ttl, _ := strconv.Atoi(cfgGet(c, "pr_ttl"))
	return []setRow{
		{kind: setHead, label: "Tell me when an agent"},
		{name: "notify:waiting", label: "needs me", kind: setToggle, on: has("waiting")},
		{name: "notify:error", label: "hits an error", kind: setToggle, on: has("error")},
		{name: "notify:done", label: "finishes", kind: setToggle, on: has("done")},
		{name: "notify_bell", label: "with the bell", kind: setToggle, on: isOn(cfgGet(c, "notify_bell"))},
		{name: "notify_desktop", label: "on the desktop", kind: setToggle, on: isOn(cfgGet(c, "notify_desktop"))},
		{name: "notify_banner", label: "in a banner", kind: setToggle, on: isOn(cfgGet(c, "notify_banner"))},
		{kind: setBlank},
		{kind: setHead, label: "Sidebar"},
		{name: "sidebar_width", label: "Width", kind: setStep, value: cfgGet(c, "sidebar_width")},
		{name: "sidebar_bg", label: "Background", kind: setChoice, value: bg, opts: []string{"Terminal", "Fixed"}},
		{kind: setBlank},
		{kind: setHead, label: "Agents"},
		{name: "agent", label: "Command", kind: setShow, value: cfgGet(c, "agent")},
		{name: "worktrees", label: "Worktrees", kind: setShow, value: tildify(wt)},
		{name: "ctx_warn", label: "Warn at context", kind: setStep, value: cfgGet(c, "ctx_warn") + "%"},
		{name: "cache_ttl", label: "Cache stays warm", kind: setChoice, value: cacheChoice(cfgGet(c, "cache_ttl")), opts: []string{"5m", "1h"}},
		{kind: setBlank},
		{kind: setHead, label: "Network"},
		{name: "pr", label: "PR lookups (gh)", kind: setToggle, on: isOn(cfgGet(c, "pr"))},
		{name: "pr_ttl", label: "Check every", kind: setStep, value: humanizeSecs(int64(ttl))},
		{kind: setBlank},
		{name: "open", label: "Open config.toml", kind: setButton},
	}
}

// settable reports whether a row does something when chosen.
func (r setRow) settable() bool {
	return r.kind == setToggle || r.kind == setStep || r.kind == setChoice || r.kind == setButton
}

// renderSettings draws the screen: Back on top, the rows, the foot. sel is
// the row the keyboard is on; hover (row plus one) and hoverX the pointer,
// whose row lifts so it reads as something to click.
func renderSettings(c map[string]string, w, h, sel, hover, hoverX int) (string, []lineRef) {
	var body []line
	back := accent + "‹ Back" + reset
	if hover == 1 && hoverX >= 2 && hoverX < 8 {
		back = accent + bold + "‹ Back" + reset
	}
	body = append(body, line{spread("  "+back, bold+"Settings"+reset, w), lineRef{kind: "set", id: "set:back", hits: []hit{{2, 8, "back"}}}}, line{"", lineRef{}})
	for i, r := range settingRows(c) {
		lead := " "
		if i == sel && r.settable() {
			lead = accent + "▌" + reset
		}
		ref := lineRef{kind: "set", id: "set:" + r.name}
		var s string
		switch r.kind {
		case setHead:
			s, ref = "  "+dim+r.label+reset, lineRef{}
		case setBlank:
			ref = lineRef{}
		case setToggle:
			sw := faint + "○" + reset + faint + "━━" + reset
			if r.on {
				sw = accent + "━━" + reset + bold + "●" + reset
			}
			s = spread(lead+"   "+truncate(r.label, w-10), sw, w)
			ref.hits = []hit{{0, w, "toggle"}}
		case setStep:
			right := accent + "‹ " + reset + bold + r.value + reset + accent + " ›" + reset
			s = spread(lead+"   "+truncate(r.label, w-12), right, w)
			x := w - 1 - visWidth(right)
			ref.hits = []hit{{x, x + 2, "dec"}, {w - 3, w - 1, "inc"}}
		case setChoice:
			right, x := "", 0
			for _, o := range r.opts {
				if o == r.value {
					right += button(o)
				} else {
					right += dim + " " + o + " " + reset
				}
			}
			x = w - 1 - visWidth(right)
			for _, o := range r.opts {
				n := len([]rune(o)) + 2
				ref.hits = append(ref.hits, hit{x, x + n, "opt:" + o})
				x += n
			}
			s = spread(lead+"   "+truncate(r.label, w-20), right, w)
		case setShow:
			s = spread(lead+"   "+truncate(r.label, w-16), dim+truncate(r.value, 14)+reset, w)
			ref = lineRef{}
		case setButton:
			s = lead + "   " + button(r.label)
			ref.hits = []hit{{4, 4 + len([]rune(r.label)) + 2, "open"}}
		}
		if hover == len(body)+1 && r.settable() {
			s = fill(s, w, raised)
		}
		body = append(body, line{s, ref})
	}
	// Settings stays lit while its screen is open: where you are
	foot := footerLine(w, "  ", "open:settings")
	if h > 0 && hover == h && foot.ref.at(hoverX) == "open:help" {
		foot = footerLine(w, "  ", "open:help")
	}
	if h > 0 {
		for len(body) < h-1 {
			body = append(body, line{"", lineRef{}})
		}
		body = append(body[:min(len(body), h-1)], foot)
	} else {
		body = append(body, foot)
	}
	var b strings.Builder
	refs := make([]lineRef, len(body))
	for i, l := range body {
		if i > 0 {
			b.WriteByte('\n')
		}
		ls := l.s
		if w > 0 && visWidth(ls) > w {
			ls = truncateStyled(ls, w)
		}
		b.WriteString(ls)
		refs[i] = l.ref
	}
	return b.String(), refs
}

// applySetting changes one setting by name: toggle, dec, inc, opt:<o>. It
// writes the user's config.toml and says whether the sidebars must refit.
func applySetting(path, name, how string, termBg string) (refit bool, err error) {
	c := readConfig(path)
	switch {
	case strings.HasPrefix(name, "notify:"):
		state := strings.TrimPrefix(name, "notify:")
		var keep []string
		has := false
		for _, s := range strings.Fields(cfgGet(c, "notify")) {
			if s == state {
				has = true
				continue
			}
			keep = append(keep, s)
		}
		if !has {
			keep = append(keep, state)
		}
		return false, setConfig(path, "notify", strings.Join(keep, " "))
	case name == "notify_bell", name == "notify_desktop", name == "notify_banner", name == "pr":
		v := "on"
		if isOn(cfgGet(c, name)) {
			v = "off"
		}
		return false, setConfig(path, name, v)
	case name == "sidebar_width":
		n, _ := strconv.Atoi(cfgGet(c, name))
		n = step(n, how, 2, 24, 48)
		return true, setConfig(path, name, strconv.Itoa(n))
	case name == "ctx_warn":
		n, _ := strconv.Atoi(cfgGet(c, name))
		return false, setConfig(path, name, strconv.Itoa(step(n, how, 5, 50, 95)))
	case name == "pr_ttl":
		n, _ := strconv.Atoi(cfgGet(c, name))
		i := 0
		for j, t := range prTTLs {
			if t <= n {
				i = j
			}
		}
		i = step(i, how, 1, 0, len(prTTLs)-1)
		return false, setConfig(path, name, strconv.Itoa(prTTLs[i]))
	case name == "cache_ttl":
		v := "3600"
		if how == "opt:5m" || (how == "toggle" && cfgGet(c, name) == "3600") {
			v = "300"
		}
		return false, setConfig(path, name, v)
	case name == "sidebar_bg":
		v := ""
		if how == "opt:Fixed" || (how == "toggle" && cfgGet(c, name) == "") {
			v = termBg
		}
		return true, setConfig(path, name, v)
	}
	return false, fmt.Errorf("no setting %q", name)
}

// step moves n by d toward how ("dec" or anything else), held to [lo, hi];
// a click past the end wraps, so a stepper is never stuck.
func step(n int, how string, d, lo, hi int) int {
	if how == "dec" {
		n -= d
	} else {
		n += d
	}
	switch {
	case n > hi:
		return lo
	case n < lo:
		return hi
	}
	return n
}

// cacheChoice names a cache_ttl as Settings offers it: Claude Code's prompt
// cache lives five minutes, or an hour with the longer cache.
func cacheChoice(v string) string {
	if v == "3600" {
		return "1h"
	}
	return "5m"
}

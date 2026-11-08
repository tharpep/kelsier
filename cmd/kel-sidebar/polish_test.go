package main

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// Help wears the same Back row as Settings, and a click on it closes Help.
func TestHelpHasBack(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	m := sbModel{help: true, width: 32, height: 40}
	if first := strings.Split(plain(m.render()), "\n")[0]; !strings.Contains(first, "‹ Back") || !strings.HasSuffix(first, "keys") {
		t.Errorf("Help's top row: %q", first)
	}
	m.ui.hover, m.ui.hoverX = 1, 3
	if first := strings.Split(m.render(), "\n")[0]; !strings.Contains(first, accent+bold+"‹ Back") {
		t.Errorf("Back does not lift under the pointer: %q", first)
	}
	nm, _ := m.Update(tea.MouseClickMsg{X: 3, Y: 0, Button: tea.MouseLeft})
	if nm.(sbModel).help {
		t.Error("Back did not close Help")
	}
}

// The n x row's description does not repeat the key column's ellipsis.
func TestHelpNoDoubleEllipsis(t *testing.T) {
	for _, h := range sbHelp {
		if strings.HasSuffix(h[0], "…") && strings.HasPrefix(h[1], "…") {
			t.Errorf("%q / %q", h[0], h[1])
		}
	}
}

// An exported KEL_* var wins over config.toml as in bin/kel: the switch shows
// the effective value, is tagged env, and a click writes nothing.
func TestSettingsShowEnvOverride(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("KEL_NOTIFY_DESKTOP", "off")
	t.Setenv("KEL_CTX_WARN", "")
	if err := setConfig(kelConfigPath(), "notify_desktop", "on"); err != nil {
		t.Fatal(err)
	}
	out, refs := renderSettings(readConfig(kelConfigPath()), 32, 40, 1, 0, 0)
	y := -1
	for i, r := range refs {
		if r.id == "set:notify_desktop" {
			y = i
		}
	}
	line := strings.Split(plain(out), "\n")[y]
	if !strings.Contains(line, "env") || !strings.Contains(line, "○") {
		t.Errorf("desktop row: %q", line)
	}
	if len(refs[y].hits) != 0 {
		t.Errorf("an overridden switch is clickable: %+v", refs[y].hits)
	}
	for _, r := range settingRows(readConfig(kelConfigPath())) {
		if r.name == "notify_bell" && (r.env || !r.settable()) {
			t.Error("a row with no env var is marked overridden")
		}
		if r.name == "ctx_warn" && r.env {
			t.Error("an empty env var overrides")
		}
	}
	m := sbModel{settings: true, width: 32, height: 40, setSel: 1}
	m.Update(tea.MouseClickMsg{X: 10, Y: y, Button: tea.MouseLeft})
	if got := readConfig(kelConfigPath())["notify_desktop"]; got != "on" {
		t.Errorf("a click on an overridden switch wrote %q", got)
	}
}

// A footer message ends on its own; a prompt is not one and stays.
func TestToastExpires(t *testing.T) {
	m := sbModel{width: 32, height: 20, visible: true, msg: "new agent feat-login [webapp]"}
	m.msgAt = time.Now()
	nm, _ := m.Update(sbTick{})
	if nm.(sbModel).msg == "" {
		t.Error("a fresh message was dropped")
	}
	m.msgAt = time.Now().Add(-toastFor - time.Second)
	nm, _ = m.Update(sbTick{})
	if nm.(sbModel).msg != "" {
		t.Error("an old message stayed")
	}
	m = sbModel{width: 32, height: 20, visible: true, mode: modeKill, target: "a:@1"}
	nm, _ = m.Update(sbTick{})
	if nm.(sbModel).mode != modeKill {
		t.Error("a prompt expired")
	}
	nm, _ = sbModel{width: 32, height: 20, visible: true}.Update(sbDone{ok: true, msg: "done"})
	if s := nm.(sbModel); s.msg != "done" || time.Since(s.msgAt) > time.Second {
		t.Errorf("a finished command is not stamped: %q %v", s.msg, s.msgAt)
	}
}

// A hovered server row trades its name for the buttons, never its port.
func TestServerPortSurvivesHover(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	m := sbModel{doc: loadFixture(t, "fifteen-three-repos"), width: 32, height: 0, visible: true}
	_, refs := renderView(m.view(), 32, 0, 0)
	y := nameRow(refs, "%7")
	m.ui.hover, m.ui.hoverX, m.ui.hoverID = y+1, 3, "%7"
	lines, _ := hoverFrame(m)
	if !strings.Contains(lines[y], ":8000") {
		t.Errorf("hovered server row: %q", lines[y])
	}
}

// With the branch equal to the agent's name, the drawer does not say it twice.
func TestDrawerSkipsBranchEqualToName(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	m := drawerModel(t, "a:@7")
	v := m.view()
	var name, branch string
	for i := range v.agents {
		if agentID(v.agents[i].key) == "a:@7" && v.agents[i].git != nil && v.agents[i].git.Branch != nil {
			branch = *v.agents[i].git.Branch
			v.agents[i].agent.Name = branch
			name = branch
		}
	}
	if name == "" {
		t.Skip("fixture agent has no branch")
	}
	v.drawer = &sbDrawer{id: "a:@7", items: []sbAct{{"enter", "Go to it"}}}
	out, _ := renderView(v, 32, 0, 0)
	if n := strings.Count(plain(out), branch); n != 1 {
		t.Errorf("%q appears %d times:\n%s", branch, n, plain(out))
	}
}

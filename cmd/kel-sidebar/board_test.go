package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/tharpep/kelsier/internal/fleet"
)

const boardFixtures = "testdata/board"

var boardSizes = [][2]int{{150, 40}, {100, 40}, {60, 40}}

func loadBoardFixture(t *testing.T, name string) (fleet.Doc, int64) {
	t.Helper()
	doc, now, err := readFixture(filepath.Join(boardFixtures, name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	return doc, now
}

// Every board fixture in every face at every size, colours included. A
// golden file is a frame the author approved (spec.md §5e).
func TestBoardGolden(t *testing.T) {
	t.Setenv("HOME", "/home/me")
	files, _ := filepath.Glob(filepath.Join(boardFixtures, "*.json"))
	if len(files) == 0 {
		t.Fatal("no board fixtures")
	}
	for _, f := range files {
		name := strings.TrimSuffix(filepath.Base(f), ".json")
		for _, sz := range boardSizes {
			for fc, fn := range faceNames {
				got, err := boardFrame(f, sz[0], sz[1], face(fc))
				if err != nil {
					t.Fatal(err)
				}
				gold := filepath.Join(boardFixtures, name+"."+fn+"."+itoa(sz[0])+"x"+itoa(sz[1])+".golden")
				if *update {
					if err := os.WriteFile(gold, []byte(got+"\n"), 0o644); err != nil {
						t.Fatal(err)
					}
					continue
				}
				want, err := os.ReadFile(gold)
				if err != nil {
					t.Fatalf("%s: %v (run: go test ./cmd/kel-sidebar -run Golden -update, after reviewing the frames)", gold, err)
				}
				if got+"\n" != string(want) {
					t.Errorf("%s %s at %dx%d changed:\n--- got\n%s\n--- want\n%s", name, fn, sz[0], sz[1], plain(got), plain(string(want)))
				}
			}
		}
	}
}

// tilePlaces is where each tile's top-left corner is drawn, read from the
// frame: the line's click map names the tile, the text must hold its ╭.
func tilePlaces(t *testing.T, out string, refs []lineRef) map[string][2]int {
	t.Helper()
	lines := strings.Split(plain(out), "\n")
	places := map[string][2]int{}
	for y, r := range refs {
		if r.kind != "board" {
			continue
		}
		for _, h := range r.hits {
			id := strings.TrimPrefix(h.key, "sel:")
			if _, ok := places[id]; ok {
				continue
			}
			if rs := []rune(lines[y]); h.x0 >= len(rs) || rs[h.x0] != '╭' {
				t.Fatalf("tile %s claims line %d column %d, which is not its corner: %q", id, y, h.x0, lines[y])
			}
			places[id] = [2]int{y, h.x0}
		}
	}
	return places
}

// mutate gives every agent the same state and strips or stretches every
// fact a tile shows, so a tile whose place hung on its content would move.
func mutate(doc fleet.Doc, state *string, long bool) fleet.Doc {
	out := doc
	out.Agents = make([]fleet.Agent, len(doc.Agents))
	copy(out.Agents, doc.Agents)
	for i := range out.Agents {
		a := &out.Agents[i]
		a.State = state
		if long {
			n := strings.Repeat("a very long note that runs past the edge of any tile ", 4)
			a.Note, a.LastMessage, a.Doing = &n, &n, &n
			screen := []string{n, n, n, n, n, n, n}
			a.Screen = &screen
			a.Diff = &fleet.Diff{Added: 123456, Removed: 654321}
			a.PR = &fleet.PR{Number: 99999, State: "open"}
		} else {
			a.Note, a.LastMessage, a.Doing, a.Context, a.Tasks = nil, nil, nil, nil, nil
			a.History, a.Screen, a.Diff, a.PR, a.StateSince = nil, nil, nil, nil, nil
			a.Subagents = nil
		}
	}
	return out
}

// The board's promise: a tile's place depends on the fleet's shape and the
// width alone. Its face, its state, what it says and which tile is
// selected never move it.
func TestBoardTilesNeverMove(t *testing.T) {
	doc, now := loadBoardFixture(t, "fifteen-three-repos")
	var docs []fleet.Doc
	for _, s := range []string{"", "idle", "working", "waiting", "done", "throttled", "error", "dead"} {
		var st *string
		if s != "" {
			st = sp(s)
		}
		docs = append(docs, mutate(doc, st, false), mutate(doc, st, true))
	}
	ids := []string{""}
	for _, tl := range boardTiles(buildView(doc, uiState{})) {
		ids = append(ids, tl.id)
	}
	for _, sz := range [][2]int{{150, 0}, {100, 0}, {60, 0}, {150, 40}} {
		out, refs := renderBoard(buildView(doc, uiState{now: now}), sz[0], sz[1], now, boardOpts{loc: time.UTC})
		want := tilePlaces(t, out, refs)
		if len(want) != 10 {
			t.Fatalf("%dx%d: %d tiles placed, want all 10", sz[0], sz[1], len(want))
		}
		check := func(what string, d fleet.Doc, ui uiState, fc face) {
			ui.now = now
			out, refs := renderBoard(buildView(d, ui), sz[0], sz[1], now, boardOpts{face: fc, loc: time.UTC})
			got := tilePlaces(t, out, refs)
			for id, p := range want {
				if got[id] != p {
					t.Errorf("%dx%d %s: tile %s at %v, want %v", sz[0], sz[1], what, id, got[id], p)
				}
			}
		}
		for fc := range faceNames {
			for _, id := range ids {
				check("face "+faceNames[fc]+" sel "+id, doc, uiState{sel: id}, face(fc))
			}
			for i, d := range docs {
				check("face "+faceNames[fc]+" mutation "+itoa(i), d, uiState{}, face(fc))
			}
		}
	}
}

// Every width must draw without a panic or a line wider than the popup.
func TestBoardRendersAtAnyWidth(t *testing.T) {
	doc, now := loadBoardFixture(t, "fifteen-three-repos")
	for w := 1; w <= 160; w++ {
		for _, h := range []int{0, 3, 12, 40} {
			for fc := range faceNames {
				out, refs := renderBoard(buildView(doc, uiState{now: now}), w, h, now, boardOpts{face: face(fc), loc: time.UTC})
				if h > 0 && len(refs) > h {
					t.Errorf("%dx%d: %d lines", w, h, len(refs))
				}
				for _, l := range strings.Split(plain(out), "\n") {
					if len([]rune(l)) > w {
						t.Errorf("%dx%d %s: %q is wider than the popup", w, h, faceNames[fc], l)
					}
				}
			}
		}
	}
}

// Invariant 3 on the board: what kel cannot read is `?` or absent, never a
// zero, an empty bar or an idle stretch.
func TestBoardUnknownStaysUnknown(t *testing.T) {
	t.Setenv("HOME", "/home/me")
	got, err := boardFrame(filepath.Join(boardFixtures, "unknowns.json"), 150, 40, faceCard)
	if err != nil {
		t.Fatal(err)
	}
	p := plain(got)
	for _, want := range []string{"5h ?", "ctx ?  $?", "#12 ?", "─ ? nostate ─", "─ ? ─╮"} {
		if !strings.Contains(p, want) {
			t.Errorf("missing %q:\n%s", want, p)
		}
	}
	for _, never := range []string{"0%", "$0.00", "+0", "±0", "▁"} {
		if strings.Contains(p, never) {
			t.Errorf("%q drawn for an unknown:\n%s", never, p)
		}
	}
	strip := stateStrip(fleet.Agent{State: sp("working"), StateSince: fp(1000 - 600)}, 60, 1000)
	if !strings.HasPrefix(strip, strings.Repeat(" ", 40)) || !strings.Contains(strip, "▆") {
		t.Errorf("an hour with ten known minutes drew %q", plain(strip))
	}
}

// The board's header, the sidebar's foot and the rail say one need-you
// number for every fixture, and it is needCount's: a blocked background
// session counts in all three.
func TestNeedYouCountsAgree(t *testing.T) {
	t.Setenv("HOME", "/home/me")
	files, _ := filepath.Glob("testdata/*/*.json")
	foot := regexp.MustCompile(`●(\d+) need you`)
	head := regexp.MustCompile(`● (\d+) need you`)
	rail := regexp.MustCompile(`^ ●(\d+)$`)
	sawBg := false
	for _, f := range files {
		doc, now, err := readFixture(f)
		if err != nil {
			continue
		}
		v := buildView(doc, uiState{now: now})
		want := itoa(v.needCount())
		if want == "0" {
			want = ""
		}
		for _, b := range v.bgLoose {
			sawBg = sawBg || bgNeedsYou(b)
		}
		num := func(re *regexp.Regexp, s string) string {
			if m := re.FindStringSubmatch(s); m != nil {
				return m[1]
			}
			return ""
		}
		side, _ := renderView(v, 32, 0, now)
		board, _ := renderBoard(v, 150, 0, now, boardOpts{loc: time.UTC})
		rl, _ := renderView(v, 4, 0, now)
		got := []string{
			num(foot, plain(side)),
			num(head, strings.SplitN(plain(board), "\n", 2)[0]),
			num(rail, strings.SplitN(plain(rl), "\n", 2)[0]),
		}
		for i, g := range got {
			if g != want {
				t.Errorf("%s: %s says %q, needCount %q", f, []string{"sidebar", "board", "rail"}[i], g, want)
			}
		}
	}
	if !sawBg {
		t.Error("no fixture has a blocked background session of its own")
	}
}

// The scroll hint counts in the units the wall scrolls by: rows of tiles,
// with worktree rows said apart, never as tiles. The worktrees fixture with
// two more agents, so a row of tiles can be hidden too.
func TestBoardScrollHintSaysWorktreesApart(t *testing.T) {
	t.Setenv("HOME", "/home/me")
	doc, now := loadBoardFixture(t, "worktrees")
	a0 := doc.Agents[0]
	for _, n := range []string{"2", "3"} {
		a := a0
		a.Name, a.WindowID, a.PaneID = a0.Name+"-"+n, sp("@9"+n), sp("%9"+n)
		doc.Agents = append(doc.Agents, a)
		if doc.Panes != nil && a0.PaneID != nil {
			for _, p := range *doc.Panes {
				if p.PaneID == *a0.PaneID {
					p.PaneID, p.WindowID = "%9"+n, "@9"+n
					*doc.Panes = append(*doc.Panes, p)
					break
				}
			}
		}
	}
	cases := []struct {
		h    int
		hint string
	}{{12, "↓1 · 7 worktrees"}, {20, "↓7 worktrees"}}
	for _, c := range cases {
		got, _ := renderBoard(buildView(doc, uiState{now: now}), 100, c.h, now, boardOpts{face: faceCard, loc: time.UTC})
		rows := strings.Split(plain(got), "\n")
		if foot := rows[len(rows)-1]; !strings.Contains(foot, c.hint+"   card") {
			t.Errorf("at %d rows the hint is not %q: %q", c.h, c.hint, foot)
		}
		gold := filepath.Join(boardFixtures, "worktrees.scroll."+itoa(c.h)+".golden")
		if *update {
			if err := os.WriteFile(gold, []byte(got+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		want, err := os.ReadFile(gold)
		if err != nil {
			t.Fatal(err)
		}
		if got+"\n" != string(want) {
			t.Errorf("worktrees at 100x%d changed:\n--- got\n%s\n--- want\n%s", c.h, plain(got), plain(string(want)))
		}
	}
}

// The board's one motion: on the pulse a working tile's dot reads ◉ and
// nothing else in the frame changes. The sidebar ignores the pulse.
func TestBoardPulseMovesOnlyWorkingDots(t *testing.T) {
	t.Setenv("HOME", "/home/me")
	doc, now := loadBoardFixture(t, "fifteen-three-repos")
	v := buildView(doc, uiState{now: now})
	still, _ := renderBoard(v, 150, 0, now, boardOpts{loc: time.UTC})
	moved, _ := renderBoard(v, 150, 0, now, boardOpts{loc: time.UTC, pulse: true})
	working := 0
	for _, a := range v.agents {
		if d, _ := agentDot(a.agent, now); d == "●" && a.agent.State != nil && *a.agent.State == "working" {
			working++
		}
	}
	for _, b := range v.bgLoose {
		if _, _, w := bgLook(b); w == "working" {
			working++
		}
	}
	if working == 0 {
		t.Fatal("the fixture has no working tile")
	}
	if strings.Contains(still, "◉") {
		t.Error("the still frame has a ◉")
	}
	if n := strings.Count(moved, "◉"); n != working {
		t.Errorf("%d dots pulse, want one per working tile (%d)", n, working)
	}
	if strings.ReplaceAll(moved, "◉", "●") != still {
		t.Error("the pulse changed more than the working dots")
	}
	m := sbModel{doc: doc, width: 32, height: 40, visible: true}
	nm, cmd := m.update(sbPulse{})
	if nm.(sbModel).pulse || cmd != nil {
		t.Error("the sidebar answered the pulse")
	}
	m.board = true
	nm, cmd = m.update(sbPulse{})
	if !nm.(sbModel).pulse || cmd == nil {
		t.Error("the board did not flip and ask for the next pulse")
	}
}

// With no working tile the board stops asking for pulses, and the load that
// brings one back starts them again.
func TestBoardPulseStopsWithNothingWorking(t *testing.T) {
	t.Setenv("HOME", "/home/me")
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	busy, _ := loadBoardFixture(t, "fifteen-three-repos")
	calm, _ := loadBoardFixture(t, "fifteen-three-repos")
	calm.Background = nil
	for i := range calm.Agents {
		calm.Agents[i].State = sp("idle")
	}
	m := sbModel{doc: calm, width: 150, height: 40, visible: true, board: true, pulse: true}
	nm, cmd := m.update(sbPulse{})
	m = nm.(sbModel)
	if cmd != nil || m.pulse || !m.pulseOff {
		t.Fatal("a board with nothing working kept pulsing")
	}
	nm, cmd = m.update(sbLoaded{busy})
	if m = nm.(sbModel); cmd == nil || m.pulseOff {
		t.Error("a load with a working tile did not start the pulse again")
	}
}

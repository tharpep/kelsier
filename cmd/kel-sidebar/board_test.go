package main

import (
	"os"
	"path/filepath"
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

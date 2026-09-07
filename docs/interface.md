# kelsier — the interface

**Status:** written 2026-09-01; corrected 2026-09-04 after a cold review found
three of its factual claims wrong, and again as each phase reached the code.
Corrections are marked where they changed a conclusion, not silently folded in —
this document has been wrong in every phase so far, and the record of *how* is
worth more than a clean read. Phases 0–2 of `interface-build.md` are built;
3–5 are not.

Supersedes `spec.md` §5b. `spec.md` still owns the substrate, the record format
and the state model; this file owns everything the user touches.
`interface-build.md` owns the order it gets built in.

---

## 1. The diagnosis

kel has three interactive surfaces. Asked what distinguishes them, the author —
who built all three — could not say. That is the finding, so it goes first.

| surface | reads | acts |
|---|---|---|
| board (`Ctrl+Space`) | `fleet_json` | yes |
| `kel top` (`prefix t`) | `fleet_json` | no |
| `kel cheat` (`prefix k`) | nothing — a hand-written page | no |

The board and `kel top` are two renderers over one document with overlapping
fields. They disagree on `/` vs live filter, `q` vs `esc`, `r` vs `ctrl-r` — not
because two models were chosen, but because two models happened.

`kel cheat` exists because neither of the others teaches its own keys. It is a
symptom with a `prefix` binding, and it is wrong about five keys, which is what a
hand-maintained mirror of a keymap always becomes.

**What this argues, precisely.** Same data and overlapping rendering argues for
*one mental model and one renderer*. It does **not** by itself prove a picker and
a dashboard are the same interaction — that was an assumed step in the first
draft and the review was right to name it. The reason to merge anyway is the
diagnosis above: the author cannot tell them apart, so the distinction is not
earning the second surface even if one could theoretically exist.

---

## 2. The decision

**One surface, two implementations, two idioms.** The Go build is the real one.
`bin/kel`'s fzf board remains the fallback.

**Idioms deliberately differ.** The first draft required both to offer "the same
actions," which is unbuildable: the board's fzf is a live type-to-filter picker
(`bin/kel:1638-1650`, no `--disabled`), so binding bare `k` or `:` steals them
from the query. The fallback keeps its `ctrl-*` idiom.

**What is guaranteed, and what is not.**

- *Guaranteed:* both implementations expose the same **action set**, read the
  same **keymap file** (§6), and can dump it for a test to compare.
- *Not guaranteed:* the same keys, the same layout, the same interaction shape.

**The `kel-fleet` analogy does not hold, and the first draft leaned on it.** That
fallback is the same algorithm in another language, mechanically comparable
because a JSON document has one correct value. This is a different interaction
model in another language. The differential oracle does not transfer, and the
substitute — an action-set comparison — is weaker. Say so rather than borrowing
the stronger word.

**The fallback has a rot problem with no clean answer.** fzf is not installed on
the macOS CI runner (which is why `_kel_actions_or_dump` puts its dump branch
before `need fzf`, `bin/kel:1477-1481`), so the fallback UI cannot be exercised
end-to-end in CI. The key-dump guard is what there is. This is a known weakness,
recorded rather than papered over.

**v1.0's criterion is affected and this file does not get to change it.**
`rollout.md:894` requires `git clone && ./install.sh` to work with or without Go.
Softening that to "works, reduced" is a milestone amendment and belongs in
`rollout.md` § v1.0, argued there.

**New dependencies, as a decision.** The Go surface needs `bubbles` (keymap +
help), and a confirm needs `huh`; both pull `lipgloss`. This is real scope in a
project whose CLAUDE.md opens with "no runtime, no build." The trade: they are
Go-side only, the bash fallback stays dependency-free, and `install.sh` already
works with no toolchain. Verified compatible with the pinned
`charm.land/bubbletea/v2 v2.0.9` in a scratch module — **not** currently in
`go.mod`; the first draft implied otherwise.

---

## 3. What the surface is for

> Show me every agent, tell me which one is blocked, and let me act on it
> without leaving.

An element that serves neither scanning-for-blocked nor acting-on-one is a
candidate for deletion, not for a submenu.

---

## 4. Information architecture

### 4a. The row

```
  ● GROUP    AGENT     STATE    FOR   LAND      CTX   $      LAST OUTPUT
  ◐ kelsier  docs      waiting  4m    dirty     62%   $1.40  Should I also updat…
```

**Correction.** The first draft claimed `kel top`'s column priority contradicts
its own comment. It does not — `fixed` with LAND is 76, so `showLast` needs w≥88:
LAST OUTPUT drops at 87, LAND survives to 74. The code implements its comment
exactly. What remains is a *preference*, stated as one: LAND should also outrank
CTX and `$`, because it names an action where a dollar figure names something
already spent.

Two real defects found in the same block, neither previously recorded:

- **Off-by-two.** `showLand := w >= 74` (`main.go:363`) against a 76-column
  budget. At w=74–75 LAND is drawn into space that does not exist. Correct
  thresholds, once §4a's reorder puts LAND ahead of CTX and `$`, are
  **59 / 67 / 76** — verified by rendering, not by arithmetic. (This file first
  said 55 / 64 / 76, which are the gates for the *un*-reordered column order;
  stating a fix and a reorder together, computed against different orders, is
  the kind of error that only shows up when someone builds it.) They are now
  derived from the column widths in code rather than written down, so the
  numbers cannot drift from the layout again.
- **A false comment.** "Whatever is dropped gives its width back to what
  remains" (`main.go:358-360`) is untrue: freed width flows only into `wLast`,
  and by the time anything drops, `showLast` is already false. It goes nowhere.

### 4b. The detail

For the highlighted agent: repo path, branch, isolation; LAND with ahead/behind
and uncommitted files; context pct, tokens, window size, model, compactions;
cost; last 8 lines of pane output.

**PR state is deliberately excluded.** It is the one call in kel that leaves the
machine (`bin/kel:803-808`), v0.9 closed it by defaulting `pr = "off"`, and it is
the clause invariant 3 admits is unguarded. A per-cursor-move pane is the worst
available place for it. `kel sweep` remains its only caller.

**The hot path, corrected twice over.** Backlog #34 says the preview is "a
rendering change, not a new computation" — wrong. But the first draft's account
was also wrong in both directions:

- It is not "forks `git status`." `cmd_board_preview` (`bin/kel:1320`) calls
  `fleet_json`, then `kel_windows`, then `capture-pane`, then `git rev-parse`,
  then `git status` — roughly five tmux forks, two jq and two git **per cursor
  move**.
- The proposed fix was a regression. `fleet_json --dirty` runs
  `git status --porcelain` for *every distinct cwd in the fleet*
  (`bin/kel:905-913`), so calling it per keystroke turns one git fork into N.

**The actual fix:** compute the document **once before fzf starts**, write it to
a temp file, and have `_board_preview` read that. This is #34, #21 and invariant
5 in one change, and it ships without the merge.

Also unnoticed by #34: the board **row** already shows dirty
(`bin/kel:1261,1284,1289`). "From Ctrl+Space you cannot see the state of the
work" was already half wrong.

---

## 5. The keymap

### 5a. The seam

The actions currently three keypresses deep are not one group:

| | actions | why |
|---|---|---|
| **per-agent** | jump, kill, rename, new, worktree, move, adopt, relaunch | the highlighted row is the argument |
| **fleet-wide** | sweep, restore, prune, doctor, config, update, ls, go, shutdown | no row needed; infrequent |

`ctrl-f` mixes both, which is why it reads as "too much and not enough"
simultaneously.

**Where the seam is honest and where it is not.** `adopt` is the best case: it
applies only to unmanaged rows, so the row genuinely decides. But `new` and
`worktree` take a **directory**, not an agent (`cmd_ask_new "$cwd"`,
`bin/kel:1408`) — two rows in one repo are the same target. And `kill` is
per-agent while `sweep` is `kill` in a loop (`bin/kel:1974`): the seam sorts by
"needs a row," but *risk* cuts across it, which is why §5c constrains the
palette.

### 5b. Per-agent keys (Go surface)

**These are chosen against the navigation keys that already exist**, which the
first draft failed to do — it assigned `k` to kill in a table where `k` is
cursor-up (`main.go:317`), plus `g`/`G` and `r`. Holding the up-arrow would have
fired kills.

```
already bound: j/k up-down · g/G top-bottom · s sort · / filter · r reload · q quit

enter  jump to it        n  new agent in its dir
x      kill  (confirms)  w  new worktree agent
c      rename            m  move
l      relaunch          a  adopt   (unmanaged rows only)
```

`enter` currently matches no case and does nothing — a keypress with no effect
and no feedback, which is the pattern under audit.

Go-to-group is **not** a per-agent key: `cmd_board_jump` already switches client
to the group, so `enter` covers it. Standalone group switching lives in §5c.

Every action takes the row as an explicit target, which forces `backlog.md` #31
closed: `move` and `adopt` currently re-derive from `$TMUX_PANE`.

### 5c. Fleet-wide — the palette

`:` opens a list of every fleet-wide command, filterable, **shown in full on
open**. A palette you must already know the name of is the same failure as a key
that appears nowhere.

```
:sweep   :restore   :prune   :doctor   :config   :update   :ls   :go   :shutdown
```

**`:shutdown`, not `:quit`** — `q` already quits the view, and two meanings of
the same word one `:` apart on the tool's most destructive verb is the collision
class §5d exists to close.

**Its semantics must be stated, because #33 left them ambiguous.** `kel kill`
runs `git worktree remove` (`bin/kel:2010-2012`). So "end everything the way
`kel kill` does" would delete every worktree directory in the fleet.
**`:shutdown` snapshots and kills the tmux sessions only. It never removes a
worktree.** It refuses, naming them, if any worktree has uncommitted or unpushed
work — and unlike `kel kill` that check covers `inplace` agents too, since
`bin/kel:1985` gates the existing refusal on `iso = worktree` and "end
everything" is not the place to inherit that narrowing.

**Risk constraint.** `:sweep`, `:restore` and `:shutdown` confirm, showing what
is at stake, because the palette shows no state by design.

### 5d. Case is not a namespace *for verbs*

No two **actions** differ only by case. Navigation keeps `g`/`G` — that pair is
a spatial convention, not two verbs, and the first draft's blanket claim was
contradicted by keys the merged surface has to keep.

The rename/restore adjacency (`backlog.md` #28) is closed by putting fleet-wide
verbs behind `:`, where no single letter can be mistyped into one.

### 5e. Depth

`Ctrl+Space` → `x` is a kill: two keypresses. `Ctrl+Space` → `:` → `sweep`⏎ is
more keystrokes than `ctrl-f`, `s` is today.

**So this is not a reduction in keystrokes, and the first draft implied it was.**
Backlog #35 counted levels including the entry key; by that metric `:` is still
three. The claim is narrower and should be stated as such: **a type-ahead list
you can see in full beats a menu you must navigate blind**, and the per-agent
half genuinely drops from three to two. Whether that is worth it is a judgement,
not a metric — do not re-derive a win by changing how you count.

### 5f. `prefix m` and `prefix \``

`prefix m` (`kel.conf:66`) exists to satisfy standing rule 2 — a prefix key *and*
a board path for the same actions. Once §5b gives every per-agent action an
explicit target, `prefix m` is the same list scoped to the current window and
keeps working unchanged.

`prefix \`` — jump-to-next-waiting — is untouched by all of this. It is the core
loop and appears in neither list because it is not a surface action.

---

## 6. The keymap is a data file

**One file, read by both implementations.** Not generated from Go: the first
draft had bash printing "the generated keymap" from a binary that by definition
is not installed — the fork it existed to prevent.

```
scope       action    go_key   bash_key   help
agent       kill      x        ctrl-k     kill this agent
agent       rename    c        ctrl-r     rename it
fleet       sweep     :sweep   ctrl-f     tear down finished agents
```

- Go binds via `key.Binding` built from this file; footer and `?` render from it.
- bash binds its `ctrl-*` column and prints the same file for `?`.
- both expose a dump so a guard can compare action sets (§2).

**The file holds actions, not navigation** — a distinction Phase 2 found and
this section originally missed. `j`/`k`/`g`/`G`/`s`/`/`/`r`/`q` are `kel top`'s
own movement keys; the fzf board gets its navigation from fzf itself, so there
is no bash counterpart for them to stay in sync *with*. Putting them in a shared
file would mean maintaining a column only one reader ever consults. They live in
`cmd/kel-top/keys.go` as one `key.Binding` struct that both `Update()` and the
help view read, so the drift this section exists to prevent is still closed —
just by a second mechanism rather than the file. The one crossing is `enter`:
its key is reserved in the file's `agent/jump` row, so its *help text* comes
from there rather than being written twice.

`kel cheat` stops being a page; the command survives only as "print this file."

**`prefix ?` stays tmux's.** The first draft claimed it — it is stock
`list-keys`, `kel.conf` binds no `?`, and overriding a native key is what the
v0.9 pruning was careful not to do. It is also the only way to see whether kel's
binds loaded at all, which is #36's whole subject.

---

## 7. Errors cannot be swallowed

1. **No kel surface redirects another kel subcommand's output to `/dev/null`.**
   Narrowed from the first draft, which said "no command" — `bin/kel` has 175
   `/dev/null` occurrences and nearly all are legitimate probes. The greppable
   rule is `"$SELF" …` invocations, which still catches the two real cases:
   `bin/kel:1650` and `bin/kel:1974`.
2. **A refusal renders inside the surface's own event loop.** Go: a `huh` confirm
   as a sub-model. bash: fzf's `execute()`, which shows output and returns, plus
   `change-footer`. Never a popup that closes on exit.
   **The message renders in-process; the check does not.** `kel kill`'s refusal
   is `bin/kel:571-579` — eight lines of invariant 1. Go shells out and
   *captures* it. Reimplementing kill-safety in a second language with no
   differential test would be worse than the bug this rule fixes.
3. **After a mutating action, re-read and re-render — except after a launch.**
   `kel new` sends its command with `send-keys`, so there is a window where the
   pane is a bare shell and `effective_state` reads `dead`; CLAUDE.md says do not
   fix that by polling. A record younger than its launch renders **unknown**,
   never `dead`. Without this carve-out the rule manufactures the exact invariant
   3 violation it exists to prevent.
4. **A menu offers only what applies** — no `relaunch` on a dead record, no
   `adopt` on a managed window.

Corollaries: `display-popup` uses `-EE`; `run-shell` in `kel.conf` passes `-E`
**except** the snapshot hooks at `:47-49`, which are fire-and-forget.

---

## 8. Read-only is not a third state

No surveyed TUI draws a selection cursor it will not honour. Since the merged
surface acts, this resolves itself; the section exists only so the `›`-with-no-
binding pattern is not reintroduced.

---

## 9. What gets deleted

| | why |
|---|---|
| `ctrl-f`'s mixing of two action classes | it becomes the fleet-wide list only |
| `kel top` as a command, `prefix t` | folded into the one surface |
| `kel cheat` as a written page | §6 — the command prints the keymap file |
| `cmd_menu()` (`bin/kel:1657`) | dead; the dispatcher calls `cmd_board` |

**`prefix g` is not deleted.** The first draft repointed it at `kel go`, citing
#37. `kel go` with no argument *prints* a list and returns (`bin/kel:1085-1092`)
— it is not a picker, and via `run-shell -b` the output goes nowhere. #37 is
wrong and the first draft inherited it uninspected. `prefix g` keeps
`choose-tree` until `:go` is a real picker; then it repoints.

---

## 10. Reduced mode

The fzf board keeps today's idiom and today's keys: type to filter, `enter`
jump, `tab` per-agent actions, `ctrl-k` kill, `ctrl-f` fleet-wide, `ctrl-n` new.
What it gains is the shared keymap file, `?`, and explicit targets for `move` and
`adopt`. What it does not gain is the Go keymap.

This is a much smaller change than the first draft implied, and that is the point
of dropping key parity.

---

## 11. Still open

- **Panes.** #32 and #22 are one question: is a pane a thing kel can name? Blocks
  nothing here.
- **Invariant 5 has no budget.** `kel top` reloads every 2s with `Dirty: true,
  Land: true` (`main.go:245-260`) — acceptable for a dashboard you opened, but it
  is about to become what `Ctrl+Space` does. **A measured ceiling for one open is
  a precondition**, not a follow-up.
- **Naming.** "board" described a picker; the merged surface is more than that.
- **R6.** `kel peek` was rejected partly because a row expander "does not cross
  `kel top`'s read-only boundary." §8 abolishes that boundary; R6's rationale
  needs amending, though the conclusion may stand.

---

## 12. Effect on `backlog.md`

**Specified here:** #17–#20 (§7), #23 (§9), #24, #27 (§6), #28 (§5d), #29, #30,
#33 (§5c), #31 (§5b), #34 (§4b), #35 (§5a), #37 (§9 — *and #37 is itself wrong;
correct it*).

**Promoted, not unaffected** — the first draft filed these as untouched, which
was wrong: #21 (§4b rewrites that function; write the guard while there), #25 (a
malformed record vanishing from `kel top` becomes vanishing from the *primary*
surface), #26 (a height bug in an opt-in dashboard becomes one in `Ctrl+Space`).

**Still open:** #22, #32, #36.

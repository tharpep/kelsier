# The interface build — phases

Execution plan for `interface.md`. That file says **what**; this one says **in
what order** and **what done means**. It is a work order — when the last phase
lands, this file is deleted, not archived.

Rewritten 2026-09-04 after a cold review. The order changed: the keymap file now
comes before any UI work, because everything else forks without it.

**Rules across every phase.**

- `test/kel-test.sh` passes before you commit; `bash -n bin/kel && bash -n
  install.sh`; `go vet ./... && go build ./...`.
- Docs move with the change — `spec.md`, `rollout.md`, `usage.md`.
- The five invariants in `CLAUDE.md` are not negotiable. Each phase names the
  ones it can break.
- **Run it.** Several bugs here were invisible to inspection and obvious on the
  first run; the first draft of this spec bound `k` to kill in a table where `k`
  is cursor-up, and no amount of reading caught it.

**Status.** 0 ☑ · 1 ☑ · 2 ☑ · 3 ☐ · 4 ☐ · 5 ☐

**Phase 2 landed 2026-09-07.** Suite 290 → 305. Two corrections:

- item 4's `55 / 64 / 76` were computed for the *un*-reordered column order and
  stated next to an instruction to reorder. The real gates are 59 / 67 / 76, and
  they are now derived from the column widths so they cannot drift again.
- **#25 was much worse than its title.** Go dropped one record; bash's
  `jq --slurpfile` aborted the entire stream, so one truncated session file
  emitted an empty document and blanked the board, `kel ls`, the bar and
  `kel jump` together. Both sides fixed, and the corrupt record now sits in the
  differential fixture. `backlog.md` #25 carries the corrected account.

**Phase 0 landed 2026-09-04.** Suite 255 → 274. Two corrections it forced back
into this file: item 3's `-E` does not exist before tmux 3.6 and would have
broken every key it touched (`backlog.md` #19 has the corrected diagnosis), and
`ctrl-g` is a navigation verb — it keeps `become()`, like `enter`. Measured: the
preview dropped ~80ms → ~71ms per cursor move, with a one-time ~90ms at board
open. The remainder is the deliberately-live `capture-pane`.

**Phase 1 landed 2026-09-04.** Suite 274 → 290. The keymap lives at
`cmd/kel-top/keymap.tsv` — under the Go package, because `go:embed` cannot
reach outside its own directory; bash walks up from `_real_self` to the same
physical file. Three corrections it forced:

- item 5's navigation list named `enter`, which §5b assigns to `jump` — the
  guard would have rejected the design it exists to protect. It now allows
  `jump` to claim `enter` and no other action to claim any working nav key.
- reading the five board keys with one lookup call each added **48ms to board
  open** — over half the cost of the snapshot Phase 0 introduced for exactly
  this reason. One awk pass instead: ~15ms. The per-action helper was deleted
  rather than left as a tempting shim.
- the keymap became load-bearing (a missing file is a dead board, not a
  degraded one), so `kel doctor` gained a probe for it. A hard dependency with
  no probe is how an install goes quiet.

---

## Phase 0 — Make failures visible

**Goal.** Every command reachable from a surface can print, and its output
reaches a human. Independent of the merge — kept even if the merge is abandoned.

**Files.** `bin/kel`, `tmux/kel.conf`, `test/kel-test.sh`

**Do.**

1. **Board fzf** (`bin/kel:1638-1653`). Drop `>/dev/null 2>&1` — the trailing
   `|| true` already absorbs fzf's exit status. Convert `tab`, `ctrl-g`,
   `ctrl-n`, `ctrl-f` from `become(...)` to `execute(...)+reload($SELF
   _board_rows)`. `execute()` restores the terminal, shows output, returns to
   fzf; `become()` is `execve(2)`, one-way. **Leave `enter:become(...)` alone** —
   jumping *should* replace the board. `ctrl-k` is already correct; copy it.
2. **Popups** (`bin/kel:1592`, `1624`, `1935`, `2275`). `-E` → `-EE`. Single `-E`
   closes on any exit including a `die`. `bin/kel:2038` already handles this with
   an `[any key]` pause — leave it.
3. **Keybinds** (`tmux/kel.conf:56,57,66,69`). Add `-E`. **Do not touch
   `kel.conf:47-49`** — snapshot hooks, deliberately fire-and-forget; `-E` makes
   every pane split noisy.
4. **Preview argument guard** (`bin/kel:1321`). `local name="$1" group="$2"` under
   `set -u` renders `$2: unbound variable` when fzf's `{2}` expands empty.
   `cmd_board_kill:1363` already does this correctly. (`backlog.md` #21.)
5. **Preview stops recomputing everything per keystroke.** `cmd_board_preview`
   (`bin/kel:1320`) calls `fleet_json`, `kel_windows`, `capture-pane`,
   `git rev-parse` and `git status` on **every cursor move**. Compute the
   document **once before fzf starts**, into a `mktemp` file the preview reads.
   **Do not substitute `fleet_json --dirty` inline** — it runs `git status
   --porcelain` for every distinct cwd in the fleet (`bin/kel:905-913`), turning
   one git fork per keystroke into N. Add `land` to the rendered output; leave
   `capture-pane` per-agent, it is the one thing that must be live.
6. **Measure it.** Record the wall time of one `Ctrl+Space` open and one cursor
   move, before and after. This is the baseline invariant 5 has never had, and
   §11 makes it a precondition for the merge.

**Must not.** Invariant 5: item 5 must make the preview cheaper — verify with
item 6, not by reasoning. Invariant 1: item 1 changes how the kill refusal
reaches the screen; it must still refuse.

**Done when.** A dirty-worktree kill through `cmd_board_kill` emits the at-risk
paths to captured output. `_board_preview` with zero and one argument exits 0
with no bash diagnostic. Source guards: no `display-popup` uses single `-E`; no
`kel.conf` bind invoking `kel` uses `run-shell -b` without `-E`; no `"$SELF"`
invocation redirects to `/dev/null`. Each proven to fail by reverting one case.
The measurement from item 6 is in the commit message.

---

## Phase 1 — The keymap becomes a file

**Phase 1 landed 2026-09-04.** Suite 274 → 290. One correction the collision
guard forced back into this file: item 5's nav-key list below reads
`j k g G s / r q esc enter`, but `enter` is not actually a functioning nav key
today — `kel-top`'s normal-mode `Update()` has no case for it (only filter
mode's `enter`/`esc` closes the filter box, a different switch). §5b assigns
`enter` to `jump` by design ("`enter` currently matches no case and does
nothing... Go-to-group is not a per-agent key... `enter` covers it"). The
guard's actual rule, as shipped: no *other* per-agent action may claim a
working nav key (`j k g G s / r q esc`), and no action but `jump` may claim
`enter` — both directions are checked and both are proven to fail (see
`test/kel-test.sh`, "the keymap becomes a file" section). Treating `enter` as
a plain member of the nav-key set, as item 5 originally read, would make the
guard reject the very design it exists to protect.

**Goal.** One data file both implementations read. Nothing renders differently
yet. This is first because every later phase forks the keymap without it.

**Files.** new keymap file, `bin/kel`, `cmd/kel-top/main.go`, `test/kel-test.sh`

**Do.**

1. Create the file — `scope`, `action`, `go_key`, `bash_key`, `help`, one row per
   action, tab-separated. Read `CLAUDE.md`'s trap on `IFS=$'\t' read` first:
   **read it one field per line**, not with tab as IFS, or empty columns shift.
2. bash reads it to build its `ctrl-*` binds and to print `?`.
3. Go reads it to build `key.Binding`s. No behaviour change yet — just make the
   existing binds come from the file.
4. **Both grow a dump** (`--dump-keys` / `_dumpkeys`) printing `scope action key`.
5. **The collision test, written now, before any UI work.** Assert no `go_key`
   collides with an existing navigation key (`j k g G s / r q esc enter`), and no
   two actions in one scope share a key. The first draft of `interface.md`
   violated this three ways.

**Must not.** Do not change what any key does in this phase.

**Done when.** Both dumps agree on the action set. The collision test passes and
fails when `kill` is set to `k`. `bash -n` clean and the suite green with no
behaviour delta.

---

## Phase 2 — Generated help, and the rendering bugs

**Goal.** `kel top` renders its help from the file, and stops being wrong about
its own layout.

**Files.** `cmd/kel-top/main.go`, `go.mod`, `test/kel-test.sh`

**Do.**

1. Add `charm.land/bubbles/v2`. Footer via `help.Model` short view; `?` for full.
   **The v2 API is `h.SetWidth(n)`, not `h.Width = n`** — the tutorials are v1.
2. `main.go:473` hand-writes the footer; six handled keys are documented nowhere
   (`g`, `G`, `r`, `esc`, `ctrl+c`, and `backspace` in filter mode). They come
   back for free. (#27.)
3. `enter` matches no case (`main.go:311-334`) — give it an action or an explicit
   no-op message.
4. **Column thresholds are off by two.** `showLand := w >= 74` against a
   76-column budget draws LAND into space that does not exist at w=74–75. Correct
   to **59 / 67 / 76** — and derive them from the column widths rather than
   writing them down. Then apply the §4a preference: LAND outranks CTX and `$`.
   (This item first said 55 / 64 / 76, which are correct only for the
   *un*-reordered order — the numbers and the reorder instruction were computed
   against different layouts.)
5. **Delete the false comment** at `main.go:358-360` — "whatever is dropped gives
   its width back to what remains" is untrue; freed width flows only to `wLast`,
   which is already hidden by then.
6. **#25 and #26**, promoted because this surface is becoming the primary one: a
   malformed record must render as `unreadable`, not vanish
   (`internal/fleet/fleet.go:254-256`); and drop footer, then header, before
   clamping the body, so heights 1–4 do not overflow.

**Must not.** No actions yet. This phase changes what the surface *says*.

**Done when.** A bidirectional guard: every key the update loop handles appears
in the help, and every key in the help is handled. A width sweep asserts no row
exceeds the terminal width at 50–90 columns; a height sweep at 1–4 rows. A
corrupt record keeps the agent count.

---

## Phase 3 — The mode machine and the palette

**Goal.** An explicit mode state, and a palette carrying read-only globals only.

**Files.** `cmd/kel-top/main.go`, `test/kel-test.sh`

**Do.**

1. Replace the `m.filtOn` boolean (`main.go:287`) with a `mode` enum —
   `modeNormal`, `modeFilter`, `modePalette`. `Update` dispatches to one
   `updateX` per mode. **Booleans are the thing being removed**: three flags is
   eight representable states and about four legal ones.
2. `:` opens the palette **showing every command**, filterable — not a blank
   prompt.
3. Wire `:ls`, `:doctor`, `:config` only. All read-only or non-destructive.
4. Reconcile the two interaction models: `q` quits, `esc` leaves a mode, reload
   is one key across both surfaces.

**Must not.** Nothing destructive in the palette this phase. No confirm yet.

**Done when.** Mode transitions are table-tested: from each mode every bound key
either produces the documented next mode or is an explicit no-op. The palette
renders its full list on an empty query.

---

## Phase 4 — Actions

**Goal.** The surface acts. Safest first, `kill` last, `:shutdown` after that.

**Files.** `cmd/kel-top/main.go`, `bin/kel`, `test/kel-test.sh`

**Do.**

1. Order: `enter` jump, `c` rename, `n`/`w` new/worktree, `m`/`a` move/adopt,
   `l` relaunch, `x` kill. Then `:sweep`, `:restore`, `:prune`, `:update`, and
   `:shutdown` last.
2. Every per-agent action takes the row as an explicit target — this is what
   forces #31 closed. Note `new`/`worktree` take a **directory**
   (`cmd_ask_new "$cwd"`, `bin/kel:1408`), so the row supplies its cwd, not its
   identity.
3. **Shell out and capture; never inherit, never reimplement.**
   `CombinedOutput()` gives you the refusal text to render in your own view.
   `kel kill`'s refusal is `bin/kel:571-579` — eight lines of invariant 1.
   Duplicating it into Go with no differential test is the worst outcome
   available in this build.
4. Confirms are a `huh` sub-model — one more mode, delegation not a hand-rolled
   switch. `:sweep`, `:restore` and `:shutdown` confirm, showing what is at stake.
5. **`:shutdown` is new** (#33) and its semantics are load-bearing: snapshot,
   kill the tmux sessions, **never remove a worktree** — `kel kill` runs
   `git worktree remove` (`bin/kel:2010-2012`) and "end everything" must not
   inherit that. Refuse, naming them, if any worktree has uncommitted or unpushed
   work, including `inplace` agents (`bin/kel:1985` gates the existing refusal on
   `iso = worktree`; do not inherit that narrowing here).
6. Apply §7 rule 3 with its carve-out: re-read after a mutating action, **except**
   that a record younger than its launch renders `unknown`, never `dead`.
   CLAUDE.md's `kel new` race is the reason.

**Must not.** Invariant 1: a dirty worktree still refuses and the refusal names
the files — the clause CLAUDE.md flags unguarded; guard it here. Invariant 4:
no sixth `send-keys` call site.

**Done when.** Kill through the surface refuses on a dirty worktree, renders the
at-risk paths, leaves the window alive; `--force` overrides. `:shutdown` ends two
groups, leaves the snapshot restorable, and leaves every worktree directory on
disk. Every action driven from a row that is **not** the current window.

---

## Phase 5 — Parity, then deletions

**Goal.** The fallback reaches the same action set; the replaced surfaces go.

**Files.** `bin/kel`, `tmux/kel.conf`, `docs/*`, `completions/*`

**Do.**

1. The fzf board reaches every action in the keymap file via its `bash_key`
   column — **its own idiom, not the Go keys** (`interface.md` §2). In practice
   this is mostly today's binds plus explicit targets for `move` and `adopt`.
2. The parity guard: both dumps expose the same action set. It cannot assert they
   render alike; keep it honest to that.
3. Deletions: `ctrl-f` becomes fleet-wide-only; `kel top` as a command and
   `prefix t`; `kel cheat` as a page (the command prints the keymap file);
   `cmd_menu()` (`bin/kel:1657`).
4. **`prefix g` stays on `choose-tree`** until `:go` is a real picker — `kel go`
   with no argument prints and returns (`bin/kel:1085-1092`), and `run-shell -b`
   discards that. Fix `backlog.md` #37, which claims otherwise.
5. Completions and `kel --help` move with the dispatcher. `usage.md`, `README.md`
   and `spec.md` all document `kel top` / `prefix t` / `ctrl-f`; the completion
   guard will fail on removal, which is correct — the completions were dead from
   v0.6 to v0.9 because this was missed once already.

**Must not.** Do not delete a surface whose replacement is not daily-driven.

**Done when.** `bin/kel` has no unreferenced `cmd_*`, both completions match the
dispatcher, the parity guard fails when an action is added to one side only, and
this file is deleted in the same commit.

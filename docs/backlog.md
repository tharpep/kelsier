# kelsier — backlog

Candidates, not commitments. Same rule as `rollout.md`: nothing gets built until
daily use produces the specific pain that unlocks it. This file is where an idea
waits and what it's waiting for.

Came out of a post-v0.4 review pass — a human read plus a second opinion from
Gemini 3.1 Pro via `agy`, then a blunt critical pass over the result — on human
use, UX, and the competitive landscape (Claude Squad, Vibe Kanban,
Nimbalyst/Crystal, Conductor, agent-farm, the cloud agents).

**Second pass (2026-08-29, Claude Opus 5)** re-read the Claude Code docs rather
than working from memory, and that changed the picture: hook payloads carry no
token or cost data, but `statusLine` does — which shipped as v0.4.1 (see
`rollout.md`) and retired the "fleet cost view" parked entry. It also
found that kel wires 5 of Claude Code's **30** hook events, and that the one it
leans on hardest, `Notification`, carries a `notification_type` kel ignores.
Entries #14–#16 and the *Known interactions* section come from that pass.

**Numbering** is allocation order and a stable ID — not priority. Tier, and
position within a tier, carry that.

**Scheduled entries.** `rollout.md` now assigns most of this file to a release,
so tiers describe *kind* and rollout describes *when*; where they disagree,
rollout wins.

| release | entries |
|---|---|
| ~~v0.5~~ **done** | ~~#14~~ · ~~#15~~ · ~~#13~~ · ~~#1~~ · ~~#4~~ · macOS fixes · CI |
| ~~v0.6~~ **done** | ~~`_fleet --json`~~ · ~~#2~~ (folds #3, #7) · Go seam · ~~#5~~ **cut, see R6** |
| ~~v0.7~~ **done** | ~~#6~~ · ~~#8~~ · ~~#12~~ |
| v0.8 | #11 · config file (un-parked below) |
| the TUI pass | #35 (upstream of the rest) · #17–#31 · #32–#34 · #36 · #37 |
| unscheduled | #9 · #10 · #16 · everything under *Parked* |

**Tags.** `[fits]` — inside "just bookkeeping, never wrap the agent."
`[borderline]` — one design decision away from crossing a non-goal; the entry
says which. `[violates]` — recorded so it stops getting re-proposed.

The TUI pass adds three that describe *what makes an entry done* rather than
whether it belongs: `[defect]` — code exists and misbehaves, done when fixed with
a test. `[coverage]` — a command exists with no TUI path, done when a path exists
and a guard asserts it. `[capability]` — kel has no notion of the thing, done when
it does. None of the three takes a gate; a confirmed defect gated on future pain
is a category error.

---

## Tier 1 — close the core loop

The core value prop is "know which agent is blocked, get there in one key." Half
of that — the *knowing* — currently only works if you're looking at the status
bar. These finish it.

Key budget: one new binding, `prefix t` (dashboard, replaces tmux's useless
clock). `prefix v` was reserved for peek, which is cut (R6) — the key is free
again. Everything else is a subcommand or a board action.

**Principle from the review:** the status bar stays minimal. Everything richer —
durations, last output, git state, merge-readiness — lives in `kel top` (#2),
not the bar. The one concession on the bar is #4.

### 14. Notification fidelity — stop crying wolf  ·  `[fits — correctness]`  ·  **shipped v0.5**

`kel hook` maps **every** `Notification` to `waiting`. The payload carries a
`notification_type`, and only some of its values mean "blocked on you":

| `notification_type` | today | should be |
|---|---|---|
| `permission_prompt`, `agent_needs_input` | `waiting` | `waiting` ✓ |
| `idle_prompt` | `waiting` | *not* blocked — you just left it sitting |
| `agent_completed` | `waiting` | `done` |
| `quota_auto_resume_fired` / `_stale` / `_disabled` | `waiting` | `throttled` — its own state |

So `` prefix ` `` — the one keystroke the whole tool is built around — currently
teleports you to agents that are not blocked on you. `notification_text` is in
the same payload and belongs in the board preview.

`throttled` is worth a real state: "this agent is waiting on quota, not on me"
is exactly the thing you otherwise discover by walking over to it. Suggested
bar glyph `~`, sorted below `waiting` in `kel top`.

**Gate:** none. This is a `case` on a field already being parsed. Do it first.
~10 LOC.

### 15. Compaction counter  ·  `[fits]`  ·  **shipped v0.5**

`PreCompact` / `PostCompact` are two more hook events, same shape as the five
already wired. Count them per agent (`compactions: N` in the record). "This one
has compacted three times" is a deterministic, zero-cost signal that a session
is long in the tooth — it pairs with the context % from v0.4.1 and catches the
case context % *can't*: an agent that keeps getting squeezed back down.

Also worth taking while in there: `SessionStart` has matcher values
(`startup`, `resume`, `clear`, `compact`, `fork`) and kel maps all of them to
`idle`. A `compact` restart is not the same event as a fresh `startup`.

**Gate:** none. Two hook lines + a jq increment. ~10 LOC.

### 16. Precise blocked-state via permission hooks  ·  `[fits]`  ·  *still open*

`PermissionRequest` and `PermissionDenied` are dedicated events. They are a
sharper signal than `Notification`+`permission_prompt` — a request is
unambiguously "kel, this agent cannot proceed without you," and a denial is
worth surfacing because a denied agent often stalls quietly afterward.

Fold in only after #14 — #14 is the correctness fix, this is the refinement.

**Gate:** after #14, if `Notification` still feels imprecise in daily use.

### 1. Fleet notifications  ·  `[fits]`  ·  **shipped v0.5**

The status line is the only passive surface. In another app, another workspace,
or with tmux detached, "who needs you" reaches you nowhere — and `` prefix ` ``
can't help you act on a thing you don't know about.

Fire a signal when an agent enters `waiting` (optionally `done` / `dead`) and
you are not on that window.

- Minimal: terminal bell + `tmux display-message` to the attached client.
- Then: a desktop notification — OSC 9 / OSC 777 through the terminal, or a
  `notify-send` / PowerShell-toast command the user configures.
- Guard: only when the window isn't focused; user picks which states notify.

**On the reference machine (WSL2), `notify-send` is not installed and there is
no desktop session to install it into.** The working ladder here is: bell →
`tmux display-message` → OSC 9 through Windows Terminal → `powershell.exe`
toast (it is on `PATH` at `/mnt/c/WINDOWS/...`). Do not write "install
`notify-send`" into this entry; it is a dead end on this box.

Do #14 first. Notifying on today's `waiting` would page you for `idle_prompt`,
which is precisely the false positive that makes people turn notifications off.

**Gate:** none, but sequenced after #14.

### 2. Fleet dashboard — `kel top`  ·  `[fits]`  ·  **shipped v0.6**

One read-only view of the whole fleet. `kel ls` has the table but you must drop
to a shell for it; the board is a *picker*, not a status view; `prefix k` is the
beginner primer and stays that way (do **not** overload it — the review and
`agy` landed there independently).

**Surface.** `kel top`, bound to `prefix t` (replaces tmux's clock — no loss).
Runs on plain stdout when not in tmux.

**Shows**, one row per agent, every group:

```
GROUP  AGENT  STATE  FOR   CTX   $     IDLE  BRANCH  DIRTY  LAST OUTPUT
```

- **STATE** — coloured glyph, as the bar.
- **FOR** — time in the current state, from the `.state` epoch (`6m`, `1h04m`).
  The column you're scanning: "waiting 6m" is the signal.
- **IDLE** — time since the window last produced output (`#{window_activity}`).
- **LAST OUTPUT** — `capture-pane -p` (no `-e`), stray control chars stripped,
  then truncated to fit. Never `capture-pane -e | tail -1` — that chops an ANSI
  escape mid-sequence and bleeds colour across the whole table.
- **CTX / $** — context-window percent and session cost, read straight from
  `<wid>.ctx` (v0.4.1). These were parked when this entry was written and are
  now free: no extra work, no scraping, already on disk. Colour CTX at
  `KEL_CTX_WARN` and again at 90.
- compactions (#15), when that lands — a small `×3` beside CTX.

**Sort = triage order:** `waiting` (longest first) → `working` → `done` →
`idle` → `dead`. The top row is always "deal with me first."

**Refresh.** Popup runs a 2 s redraw loop (`kel top --once` + `read -t2 -n1`);
`q` quits. No actions — the board owns those.

**Degrades.** 2 agents: a short table. 20: still one screen, sorted so the
urgent rows never fall below the fold; beyond that it scrolls.

**Sequencing note (second pass).** Build #14 and #15 *before* this. As
originally scoped, `kel top` was a new surface showing the same four states the
bar already shows; with accurate states, context, cost and compaction counts it
becomes a view worth opening. The scarce resource here was never display room,
it was what kel knows.

**Gate:** none. This is "clarity on what's going on," the thing most asked for.
Folds in #3 and makes the raw `kel ls` table mostly a scripting interface.

~60 LOC.

### 3. Waiting-duration + stall flag  ·  `[fits — folded into #2]`  ·  **FOR column shipped v0.6; stall flag still open**

The `.state` epoch gives time-in-state for free — it's the `kel top` FOR column.
Also flag an agent stuck on `working` with no state change in >N minutes: a soft
"is this one wedged?" without a new hook. **Dashboard-only** — this does not go
on the status bar (see the principle above).

**Gate:** ships with #2.

### 4. Informative "+N waiting" badge  ·  `[fits]`  ·  **shipped v0.5**

`⟨+2 waiting⟩` tells you someone's blocked, not who or where. Expand to
`⟨api·1 infra·1⟩`, or have `` prefix ` `` print `→ api/auth-fix (waiting 4m)` as
it jumps. This is the one place richer state earns a spot on the bar — it's the
same footprint, more signal.

**Gate:** low; bundle with #2.

## Tier 2 — worktree & fleet hygiene

### 6. Merge-readiness  ·  `[fits — status only]`  ·  **shipped in v0.7 as the LAND column**

`kel new -w` makes spawning five worktrees trivial and does nothing for the
other end of the day, when five branches need to land. For each worktree agent,
in `kel top`: **ahead/behind main, and pushed?** — two `git` status reads, no
more. kel never runs a merge, and it does **not** predict conflicts or test a
rebase — that's `lazygit`'s job (and the reason the diff viewer is rejected,
#R5). This is a fleet glance — "which of my worktree agents are ready" — that
lazygit can't give you because it's one repo at a time.

**Use `gh` for the PR half** (it is installed): `gh pr status --json
number,state,isDraft,mergeStateStatus`. Keep plain `git` for ahead/behind — no
network, no auth, always answerable.

**Auth is the design constraint, not a footnote.** `gh` needs a live token, and
tokens expire. `gh pr status` on a stale token exits non-zero and writes to
stderr; a naive `2>/dev/null` would render that as *"no PR"* — which reads as
"not ready to land" when the truth is "kel has no idea." That is the same class
of lie as a status line that says `working` for a dead agent, and this file
already treats that as unacceptable.

So the column is **three-valued, not two**: ready / not-ready / **unknown**,
with `unknown` rendered distinctly (`?`, dim) and never collapsed into a
negative. Concretely:

- branch out to `gh` only for agents whose worktree is otherwise clean and
  pushed — the ones where a PR answer would change what you do
- treat any non-zero exit as `unknown`; never infer absence from failure
- cache the answer per branch with a short TTL, so one auth failure doesn't
  re-stall every row on every redraw
- `kel doctor` gets a `gh auth status` probe, so the fix is one command away
  and the failure has a named home rather than a silent dash

**Gate:** ~~when you've felt the integration pile-up.~~ Shipped — `land_of` /
`pr_state_of`, `internal/fleet/land.go`, the **LAND** column in `kel top`, and
the three-valued unknown handling above came through intact.

### 7. Git dirty state — dashboard-only  ·  `[fits]`  ·  **in the fleet document since v0.6**

The DIRTY column in `kel top` (already in #2). Not on the status bar — a cramped
bar is why grouping exists, and the principle above keeps it minimal.

**Gate:** ships with #2; nothing extra to build.

### 8. Batch teardown — `kel sweep`  ·  `[fits]`  ·  **shipped v0.7**

`kel kill` and `kel prune` are one-at-a-time. Run five worktree agents, they all
finish, and `kel top` is a graveyard you clear one command at a time.

`kel sweep` — kill every `done` / `dead` agent **whose branch is merged into
main and whose worktree is clean**; report (don't touch) anything unmerged,
unpushed, or dirty. Same safety rule as `kel kill`: never delete an agent's only
copy of real work. `-n` dry-run; `-f` to include unmerged.

**Gate:** the first time you close five agents by hand in one sitting.
~30 LOC.

---

## Tier 3 — breadth / opportunistic

### 9. Second state adapter  ·  `[fits — it's the spec's design]`

State detection is meant to be a per-agent adapter (`spec.md` §9); only Claude
Code exists. Add one more — Codex / Aider / `agy` — even a crude
process-plus-last-line fallback. Proves the abstraction and de-risks the v1.0
metadata schema before it's locked.

**The coupling got deeper in v0.4.1, and that should be recorded honestly.**
kel now has *two* Claude-Code-shaped adapters — the state hooks and the
`statusLine` context reader — and only the first has any notion of being
swappable. A second agent would need a `state` adapter and would simply have no
context/cost column. That is the right trade for a personal tool, but the v1.0
schema should treat `context` as an optional, per-adapter capability rather
than a field every agent is assumed to have.

**Gate:** the day you actually run a non-Claude-Code agent under kel — not a
minute before.

### 10. Session presets — `kel new --preset`  ·  `[borderline → fits]`

Reverses the "session templates — not on the path" line in `rollout.md`. The
reversal is deliberate: this is tmux layout automation, native to what kel is.

**Surface.** `kel new <name> --preset <p>`. A preset is a bash fragment at
`<repo>/.kel/presets/<p>` (and `~/.config/kel/presets/<p>`; repo wins) — same
category as the existing executable `.kel/setup`.

**Declares** (all optional):

```bash
# .kel/presets/review
KEL_AGENT="claude"
KEL_MODEL="opus"                       # appended as --model
KEL_ISOLATION="worktree"               # else inplace
KEL_PANES=( '$EDITOR .' 'git diff main...HEAD | less -R' )
KEL_LAYOUT="main-vertical"             # any tmux select-layout name
```

`kel new` sources it in a subshell, reads those vars back, then: creates the
window + agent, opens one **shell** pane per `KEL_PANES` entry, runs
`select-layout $KEL_LAYOUT`, and only *then* sends each command into its pane.
Spawning the panes with their commands directly would let a fast-exiting command
close its pane before `select-layout` runs and throw off the geometry.

**Precedence.** explicit CLI flag > preset > default. `-w` forces worktree;
nothing de-escalates isolation.

**Restore.** Presets run only at `kel new`. They produce ordinary tmux splits,
so the existing snapshot captures the *result* and restore rebuilds from that —
it never re-runs the preset. Preset = how it's born; snapshot = how it comes
back.

**Discovery.** `kel presets` lists them; `--preset <TAB>` completes.

**Not this.** Jinja-style templating; re-enforcing the layout after you resize;
integrating Claude Code Skills / MCP (#R2).

**The risk this entry doesn't name (second pass):** it introduces a
config-file format — `.kel/presets/<p>` with `KEL_*` variables — *before* v1.0's
`~/.config/kel/config.toml` exists. Ship it and kel has two config systems and a
migration to write. Of everything in Tier 3 this is the most likely to be
regretted, and `.kel/setup` plus a shell alias already covers most of the pain.
If it is built before v1.0, it should be a `.toml` block the eventual config
loader can absorb, not a sourced bash fragment.

**Gate:** when you type the same `kel new … && tmux split … && …` twice.
~50 LOC.

### 11. `install.sh --uninstall`  ·  `[fits — release blocker]`  ·  **shipped v0.8**

Unwind the symlink, the `.tmux.conf` source line, and the `settings.json` hook
merge — the `.kel-bak.*` copies make it safe. Not urgent (this is a personal
tool), but it blocks any public release.

**Gate:** before showing anyone.

### 12. `.kel/group` per-directory override  ·  `[fits]`  ·  **shipped v0.7**

Already noted in `usage.md` / `rollout.md`. A file that pins a directory's
group, for monorepos that don't split cleanly on git root.

**Gate:** when the monorepo fallback is a daily annoyance — not before.

### 13. Restart-in-place  ·  `[fits]`  ·  **shipped v0.5**

> Moved out of Tier 3 order. v0.4 shipped `dead` detection, so kel can now
> *diagnose* a crashed agent with no way to *fix* it short of a workspace-wide
> `kel restore`. A state with no exit is worse than no state, and this is
> ~15 LOC that completes something already shipped.


A crashed agent leaves a live window sitting at a bare shell, its worktree and
branch intact. `kel new` would try to build a duplicate worktree; `kel restore`
is workspace-wide. Add a single "restart this one" — an item in the `prefix m`
manage menu and `kel restart [name]` — that relaunches the agent (`--resume` the
recorded session id) in the existing window, touching nothing else.

**Gate:** the first time you rebuild a crashed agent by hand.
~15 LOC.

---

## The TUI pass  ·  audited 2026-09-01

Every entry below carries **Error / Surface / Fix / Test**. Confirmed defects carry
no gate: gating a known bug on future pain is a category error. Only the design
questions at the end are ungated because they *cannot* be gated.

Three kinds here, separated because what makes them done differs:

| kind | done when |
|---|---|
| **defect** | fixed, with a test |
| **coverage gap** | a command exists but no TUI path reaches it — done when a path exists *and* a guard asserts it |
| **capability gap** | kel has no notion of the thing at all — done when it does |

The coverage row is the one that turns "the UI lacks things" from taste into a
table. It is also structurally blind to the capability row, which is why that row
was assembled from the author's own account rather than derived.

**Standing design decision (2026-09-01).** Four rules. The third constrains fixes;
the fourth was added after the first draft of this section broke it repeatedly.

1. Every capability has a bash command. Already true.
2. The commonly-used ones need **both** a prefix key **and** a path from
   `Ctrl+Space`. Not either — a user who lives in the TUI and a user who reaches
   for keys are the same person at different moments.
3. **Depth is a cost, not a container.** `Ctrl+Space` → board → `ctrl-f` → item is
   three levels. Adding an item to `ctrl-f` is therefore not a fix for
   unreachability; it relocates the problem. `7e80559` pruned `prefix k`'s menu on
   exactly this reasoning — *"wrapping 'press prefix, then press the native key' in
   a submenu is strictly more steps"* — and then added `ctrl-f`, which reintroduced
   depth on a different axis. Both halves of that commit were right about their own
   problem; together they produced the current shape.
4. **The user uses kel. tmux is under the hood.** So "that is native tmux" is not a
   reason a gap is acceptable — if the thing being acted on is a kel concept, kel
   should own the verb. Terminal concerns (scrolling, copy mode) are the exception,
   not the pattern. See #37.

This makes "is X reachable from Ctrl+Space" checkable, "how many keypresses"
countable, and "whose surface answers this" answerable. Several items below only
count as gaps under rules 2 and 4, and several candidate fixes are ruled out by
rule 3.

### 35. Common actions cost three levels  ·  `[capability]`  ·  *open*

**Error.** Derived keypress depth, counting the entry key itself:

| depth | actions |
|---|---|
| 1 | board (`Ctrl+Space`) |
| 2 | jump, kill, go, new, rename, top, cheat |
| **3, nothing shallower** | config, sweep, restore, prune, doctor, new-worktree, move, adopt, relaunch |
| unreachable | `ls`, `update` |

Nine actions sit three keys deep with no shorter route. And rule 2 is broken in
**both** directions, which the depth count alone hides: config, sweep, restore,
prune and doctor have **no prefix key**; move, adopt and relaunch have **no
`Ctrl+Space` path at all** — they exist only under `prefix m`. So the two surfaces
that are documented as "the same menu" do not offer the same actions.

`7e80559` removed `prefix k`'s and `prefix m`'s menus — both one level — and their
contents now live at level three. That commit's own reasoning was that submenus add
steps; it applied that to native tmux keys and not to kel's own actions.

**Surface.** The table above is derivable from the bind and menu definitions, so it
can be asserted: a guard capping depth for a "common" list, with the list being the
author's call rather than the guard's, plus a check that anything on it has both a
prefix key and a `Ctrl+Space` path.

**Fix.** Undecided, and upstream of every other UI item here — this is what the
design questions below resolve into. Three shapes, none of them "add another menu":
promote the daily actions to prefix keys so the board is never the only path;
flatten `ctrl-f` and `tab` onto the board's own screen instead of a nested popup; or
make the board's first screen answer more questions so fewer actions are needed at
all (see #34).

**Test.** The depth table asserted against a cap, and the both-paths check for the
common set. Prove-can-fail by burying a common action one level deeper, and by
removing one of its two paths.

### Silent failure — these gate everything else

Four independent mechanisms. Any UI redesign layered on top inherits the silence,
so these come first regardless of how the surfaces are later reshaped.

### 17. The board discards all output from everything it launches  ·  `[defect]`  ·  *open*

**Error.** `cmd_board` (`bin/kel:1653`) launches fzf with `>/dev/null 2>&1 || true`.
Every `--bind` uses `become()`, which execs in place and inherits fzf's
descriptors, so every command reached from the board has stdout and stderr on
`/dev/null` for the rest of the session. fzf still draws, because it writes
`/dev/tty`. The worst instance is `cmd_kill`'s uncommitted-work refusal — the
message invariant 1 calls safety-critical — swallowed when a kill goes through
the board. `cmd_board_kill:1364` writes its *own* prompt to `/dev/tty`, which is
the shape of a half-applied fix.

**Surface.** Two layers. A source guard: the board's fzf invocation must not
redirect stdout or stderr to `/dev/null` — legitimate as a source check because
the defect is the source shape. A behavioural assertion: `cmd_board_kill` takes
`group name` and reads its prompt from `/dev/tty`, so it can be driven directly
against a dirty worktree with output captured, asserting the at-risk path appears.

**Fix.** Drop `>/dev/null 2>&1`. The trailing `|| true` already absorbs fzf's exit
status, which is what the redirect was likely for.

**Test.** Both layers above; prove-can-fail by reintroducing the redirect and
watching the behavioural assertion break. Stated limit: the suite cannot drive
fzf interactively, so the `become()` chain itself stays unverified — the guard
covers the shape and the endpoints, not the middle.

### 18. Every popup closes before its error can be read  ·  `[defect]`  ·  *open*

**Error.** `man tmux`: single `-E` closes a popup when the command exits, on any
status; `-EE` closes it only on success. Four popups use single `-E`
(`cmd_here_actions:1592`, `cmd_board:1624`, `cmd_top:1935`, `cmd_config:2275`), so
a `die` closes the popup instantly and its message is unreadable. `cmd_run_popup`
is the one place this was addressed, by appending `[any key]` and a `read` — the
comment at `:2041-2046` names the bug as confirmed live. That fix was never
applied to the other four.

**Surface.** A source guard asserting no kel `display-popup` uses single `-E`.
Behaviourally, `prefix t` with `$KEL_TOP_BIN` absent hits `die "run ./install.sh
to build it"` (`bin/kel:1924`) — reachable in the suite by pointing
`KEL_TOP_BIN` at a nonexistent path and asserting the message is emitted.

**Fix.** `-E` → `-EE` on all four. A failing command then leaves the popup open.

**Test.** The source guard, plus the `KEL_TOP_BIN` case above. Prove-can-fail by
reverting one popup to `-E`.

### 19. tmux keybindings drop stderr  ·  `[defect]`  ·  *open*

**Error.** `C-Space`, `b`, `m` and `t` (`tmux/kel.conf:56,57,66,69`) use
`run-shell -b`. `-b` backgrounds the command and tmux discards its output; `-E`
is what redirects it. So every `die`/`warn` from `cmd_board`,
`cmd_here_actions` and `cmd_top` fired from a keybinding is dropped before it
reaches a pane.

**Surface.** A source guard: any `run-shell` invoking `kel` in `kel.conf` must
pass `-E`. The failure is invisible at runtime by construction, which is what
makes the source check the honest instrument here rather than a shortcut.

**Fix.** Add `-E` to those four binds, or route them through `display-popup`
which already has a pane to write to.

**Test.** The source guard, proven to fail by removing `-E` from one bind.

### 20. `_run`'s pause holds a popup open over nothing  ·  `[defect]`  ·  *open*

**Error.** `cmd_run_popup:2027-2057` fixes #18 for its own four commands, but not
#17: reached from the board, its output still goes to `/dev/null`, so the popup
stays open showing an empty screen and blocks on `read -rsn1`. It reads as frozen.
This is why sweep/restore/prune/doctor — the advertised `ctrl-f` items — appear to
do nothing.

**Surface.** Resolved by fixing #17; until then, assert `_run doctor` emits
recognisable output when invoked with stdout captured.

**Fix.** Falls out of #17. Keep the `[any key]` pause, which is correct once there
is something to read.

**Test.** `KEL_IN_POPUP=1 kel _run doctor` with output captured must contain a
probe label. Guards the composition of #17 and #18, which is where the user
actually lands.

### Defects

### 21. `_board_preview` has no argument guard  ·  `[defect]`  ·  *open*

**Error.** `bin/kel:1320` does `local name="$1" group="$2"` under `set -u`. The
board invokes it as `_board_preview {2} {1}`; when fzf's `{2}` expands empty the
shell collapses the arguments and `$2` is unbound, rendering
`$2: unbound variable` into the preview pane. Reproduced directly. It is the
most-invoked helper in the UI — preview fires on every cursor move.

**Surface.** Invoke it with zero and one argument and assert it exits 0 with empty
output rather than a bash diagnostic.

**Fix.** Default both parameters and return early when either is empty, matching
`cmd_board_kill:1363` which already does exactly this.

**Test.** The two assertions above; prove-can-fail by removing the guard.

### 22. `resolve_agent` silently drops the middle path segment  ·  `[defect]`  ·  *open*

**Error.** `bin/kel:335` uses `${want%%/*}` and `${want##*/}` — first slash and
last slash — so `grp/wname/cmd` resolves to group `grp`, name `cmd`, discarding
`wname`. Pane rows carry `wname/cmd` in field 2 (`:1289-1316`), so `ctrl-k` or
`tab`→`k` on a pane row in a multi-pane window resolves to a record that does not
exist and dies. Invisibly, per #17.

**Surface.** `resolve_agent` is reachable through `kel kill`; assert a
three-segment argument is refused with a message naming what it could not resolve,
rather than resolving to the wrong record.

**Fix.** Reject arguments with more than one `/`, or resolve pane rows through
their window id rather than a composed name. The second is the real fix and needs
a decision about whether a pane is addressable at all — see #32.

**Test.** `kel kill a/b/c` must fail with a message naming `a/b/c`, not report a
missing agent called `c`.

### 23. `cmd_menu` is dead code  ·  `[defect]`  ·  *open*

**Error.** `bin/kel:1657` defines `cmd_menu() { cmd_board; }`. The dispatcher's
`board|menu` case (`:3026`) calls `cmd_board` directly, so nothing reaches it.
The house rule is to delete dead code rather than keep shims.

**Surface.** The dispatcher-reachability check already added in the docs guard can
be extended to flag a `cmd_*` function no dispatcher case names.

**Fix.** Delete the function.

**Test.** Extend that guard; prove-can-fail by re-adding an unreferenced `cmd_`
function.

### 24. `kel cheat` names the wrong key for five actions  ·  `[defect]`  ·  *open*

**Error.** `bin/kel:2435-2436` says new / worktree / rename / adopt / relaunch are
at `ctrl-f`→board. They are on **`tab`** (`:1646`); `ctrl-f` (`:1651`) opens the
fleet menu, which contains none of them. The cheat sheet is the only place the
full keymap exists, so a user reads it, presses `ctrl-f`, does not find rename,
and concludes the key did nothing. This compounds every legibility complaint
below and is cheaper to fix than any of them.

**Surface.** Assert the cheat sheet's key attributions against the board's actual
`--bind` list — the same counted-not-matched approach as the existing docs guards.

**Fix.** Correct the line to name `tab`, and split the fleet sentence from the
per-agent one.

**Test.** A guard asserting every action named in `cmd_cheat` alongside a key is
bound to that key. Prove-can-fail by swapping two key names.

### 25. A malformed record makes an agent vanish from `kel top`  ·  `[defect]`  ·  *open*

**Error.** `internal/fleet/fleet.go:254-256` drops a record whose JSON fails to
unmarshal and continues. The agent disappears from the dashboard with no
indication — a silent omission where invariant 3 requires a distinct display for
an unknown.

**Surface.** Write a deliberately corrupt record and assert the fleet document
still accounts for it.

**Fix.** Emit a placeholder entry carrying the file name and an `unreadable`
state, so the row is visibly wrong rather than absent.

**Test.** Corrupt one record; assert `_fleet` returns the same agent count and the
bad one renders distinctly. This case is currently untested in either
implementation, so it needs the bash side too.

### 26. `kel top` has no minimum height  ·  `[defect]`  ·  *open*

**Error.** `cmd/kel-top/main.go:396-398` clamps the body to at least one row, but
header and footer print unconditionally, so a one- or two-row terminal overflows.
The 80-column case is tested; height is not.

**Surface.** Render at heights 1 through 4 and assert total output never exceeds
the height.

**Fix.** Drop the footer, then the header, before clamping the body.

**Test.** The height sweep above, alongside the existing width test.

### 27. Six `kel top` keys are undocumented on screen  ·  `[defect]`  ·  *open*

**Error.** `g`, `G`, `r`, `esc`, `ctrl+c` are handled (`main.go:311-334`) and
appear in no hint line; `backspace` and `ctrl+c` are handled in filter mode and
absent from its footer (`:476`). Separately, `enter` in normal mode matches no
case — a keypress with no effect and no feedback, which is the exact pattern under
audit.

**Surface.** Assert every key the update loop handles appears in the rendered hint
line, and that every key named in the hint line is handled.

**Fix.** Extend the hint line; give `enter` either an action or an explicit
no-op message.

**Test.** The bidirectional assertion above — it catches both drift directions,
which a one-way check would not.

### 28. `r` and `R` sit one case apart for unrelated actions  ·  `[defect]`  ·  *open*

**Error.** In the board's menus `r` renames one agent and `R` restores the entire
workspace. Commit `9c6029c` already fixed one instance of exactly this adjacency
(restart against restore); it has recurred one letter over. A mistyped shift key
runs a workspace rebuild.

**Surface.** Assert no two items reachable from the same surface differ only by
case when one is destructive.

**Fix.** Move `restore` off `R`, or require a confirmation the way `_board_kill`
does.

**Test.** The case-collision guard; prove-can-fail by reintroducing the pair.

### Coverage gaps — a command with no TUI path

### 29. `kel ls` is unreachable from the TUI  ·  `[coverage]`  ·  *open*

**Error.** The most basic read in the tool has no entry point from any surface:
not a board bind, not `tab`, not `ctrl-f`, not a prefix key. Derived, not
asserted by hand.

**Surface.** A guard asserting every user-facing command has at least one TUI
path. This is the instrument that makes the whole coverage row checkable.

**Fix.** Not by adding a `ctrl-f` item — rule 3. The board *is* the interactive
`ls`, so the gap is that nothing says so and that it costs a keypress to learn.
Two candidates: surface the count in the board's own border label so
`Ctrl+Space` answers "what have I got" on arrival, and give `ls` a prefix key so
the answer is available without opening anything. Both are shallower than today,
neither adds a level.

**Test.** The coverage guard, with `ls` and `update` as its initial failures if
run before the fix. The guard asserts a path exists; it deliberately does not
assert *which*, because that is a design call and a test that pinned it would
have to be rewritten by every redesign.

### 30. `kel update` is unreachable from the TUI  ·  `[coverage]`  ·  *open*

**Error.** Shipped 2026-09-01 with a dispatcher entry, completions and docs, and
no TUI path — the coverage gap was created in the same commit that added the
command, which is the argument for the guard existing.

**Surface.** Same guard as #29.

**Fix.** `doctor`'s behind-upstream probe already tells the user to run it, so the
honest fix is for that message to be actionable where it appears rather than for
`update` to gain a menu item three levels down. A prefix key is defensible; a
`ctrl-f` entry is rule 3 again.

**Test.** Same guard.

### 31. `move` and `adopt` reach the TUI but pass no target  ·  `[defect]`  ·  *open*

**Error.** In `cmd_board_actions` (`:1444-1454`), `relaunch` passes
`'$group/$name'`, `go` passes `'$group'` and `kill` passes both — but `move` calls
`_ask_move`, which execs `move "$grp"` with no agent, and `adopt` is called with
no arguments at all. Both fall back to `_kel_window_target`, which re-derives from
`$TMUX_PANE`.

This is a **partial migration, not an oversight.** `7e80559` states that all three
of move/adopt/relaunch "only know how to act on whichever pane I was invoked from
(no explicit wid parameter exists for any of them)", and gates them behind `$here`
for exactly that reason — correct for `prefix m`, which is always the window you
are standing in. `relaunch` was later given an explicit target; the other two were
not. So the `$here` gate is now load-bearing for two items and vestigial for the
third, and the comment at `:1576-1586` threads `wid`/`cwd`/`group`/`name`
explicitly for other fields while these two still rely on invocation context.

**Surface.** Assert both carry an explicit target, the same way the other three
items in the same list do.

**Fix.** Give `move` and `adopt` explicit targets, then re-examine whether `$here`
still needs to exist — if all three take a target, the gate's original reason is
gone and the board's `tab` could offer them too, which is one fewer asymmetry
between two surfaces that are supposed to be the same menu.

**Test.** Drive both from a window that is not the one being acted on and assert
the record that changes is the intended one. That asymmetry is what a same-window
test would miss, and it is precisely the case `$here` was invented to avoid.


### 36. Nothing in kel can tell you kel's keys are loaded  ·  `[capability]`  ·  *open*

**Error.** `cmd_doctor` runs fourteen probes and **not one mentions a keybinding**.
Its only signal about `kel.conf` is indirect — `allow-passthrough on`, which the
conf sets at `tmux/kel.conf:96` — and that probe runs
`tmux start-server \; show -gv allow-passthrough`, which starts a *fresh* server
and reads the option there. A fresh server sources `~/.tmux.conf`; the server you
are attached to may predate the install. So the probe can report ✓ while the live
session has none of kel's binds, and every downstream complaint — "prefix | does
nothing", "the button did nothing" — is indistinguishable from a real defect.

Found by making the mistake: diagnosing a reported dead keybind by reaching for
`tmux list-keys` rather than a kel command, on a server this session had itself
created. The tool offers no path to that answer, so the reflex was to leave kel —
which is the same failure the rest of this section describes, one level up.

**Surface.** A probe against the **attached** client, not a new server:
`tmux list-keys -T prefix` filtered for the binds `kel.conf` defines, comparing
what is registered against what the file declares. Both halves matter — the file
being correct and the server having read it are different facts, and today neither
is checked.

**Fix.** Add the probe. Derive the expected bind list from `kel.conf` rather than
hardcoding it, so the probe cannot drift from the file the way `spec.md` §11 did.
Report the difference, not a boolean: "kel.conf declares 12 binds, this server has
3" names the problem and implies `tmux source-file ~/.tmux.conf`. Also worth
correcting the `allow-passthrough` probe to read the attached server.

**Test.** Start a server with `-f /dev/null`, assert the probe fails and names the
count; source `kel.conf`, assert it passes. The suite already starts bare servers
this way (`reset` uses `tmux -f /dev/null start-server`), so the fixture exists.
Prove-can-fail by pointing the probe at a new server instead of the attached one —
which reproduces the current bug and is the assertion that matters most here.

### 37. kel-level concepts served by raw tmux surfaces  ·  `[capability]`  ·  *open*

**Error.** The user uses kel; tmux is under the hood. Three binds break that:

| bind | gives you | kel already has |
|---|---|---|
| `prefix g` (`kel.conf:72`) | `choose-tree -Zs` — tmux's session tree, listing sessions named `kel/misc` in tmux's vocabulary | `cmd_go` with no argument, a group picker built from `group_sessions` (`bin/kel:1085-1088`) |
| `prefix \|` (`:78`) | `split-window -h` | nothing — see #32 |
| `prefix -` (`:79`) | `split-window -v` | nothing — see #32 |

`prefix g` is the clearest: a kel key, on a kel concept, answered by a tmux
surface, when the kel equivalent is one function call away. The wheel binds
(`:39-40`) are *not* in this category — scrolling is a terminal concern and
delegating it is correct.

This axis was missed on the first pass because the audit accepted "that is native
tmux" as a reason something was fine. Under "the user uses kel", delegating a kel
concept to tmux is the finding, not the excuse. Recorded because the same reasoning
will look reasonable again next time.

**Surface.** Enumerate binds in `kel.conf` that invoke tmux directly, and for each
ask whether the thing being acted on is a kel concept (a group, an agent, a pane
kel tracks) or a terminal concept (scrolling, copy mode). The first list should be
empty; today it has three entries.

**Fix.** Point `prefix g` at `kel go`. Panes depend on #32 and the addressing
question in #22 — whether a pane is a thing kel can name — so they are not fixable
independently.

**Test.** A guard asserting no `kel.conf` bind acts on a group, agent or window
through a bare tmux command, with the terminal-concept binds listed as exemptions
carrying that reason. Prove-can-fail by pointing a bind back at `choose-tree`.

### Capability gaps — kel has no notion of the thing

Assembled from the author's account, because the coverage guard cannot see these
by construction: it enumerates commands, and a capability that is not a command
is invisible to it.

### 32. Panes are discovered, never managed  ·  `[capability]`  ·  *open*

**Error.** `prefix |` and `prefix -` are bound and live, and `kel cheat` documents
them — verified against `tmux list-keys`. So a split happens, **as tmux**, which is
not the same as kel supporting panes: the keys are raw `split-window`, kel is not
involved, and it learns about the result only afterwards (`:1256-1257`). There is
no kel-level pane action anywhere — nothing from `Ctrl+Space`, no way to open a
pane *for* something (an editor, a shell, a git view), no pane item in `tab`'s
menu. Under the Ctrl+Space decision and #37 this is a gap regardless of the keybind
working, because the primary entry offers nothing and the thing being acted on is a
pane kel will have to track. Invariant 4 forbids kel *rearranging* a user's panes;
it does not forbid offering to create one.

**Surface.** A coverage question phrased over capabilities rather than commands:
from Ctrl+Space, can the user create a pane? Today, no.

**Fix.** Undecided, and it needs the addressing question from #22 answered first —
whether a pane is a thing kel can name. A `tab` item that splits the highlighted
agent's window is the smallest version.

**Test.** Once a pane is addressable: create one from the board, assert it exists
in the fleet document and that the board lists it.

### 33. There is no way to close kel  ·  `[capability]`  ·  *open*

**Error.** No command and no binding ends the workspace. `prefix d` detaches and
the `client-detached` hook snapshots, leaving every agent running. To actually
stop, a user kills each group's tmux session by hand. Confirmed absent from the
dispatcher.

**Surface.** Nothing to surface — the capability does not exist. Its absence is
visible only as the user's own experience, which is how it went unrecorded until
now.

**Fix.** A `kel quit` that snapshots, then kills every kel session, refusing on
unsaved worktree work the way `kel kill` does. The refusal matters more than the
convenience: this is the one command that could destroy several agents at once.

**Test.** With agents in two groups, `kel quit` ends both and leaves the snapshot
restorable. With a dirty worktree, it refuses and names it — the same assertion
shape as invariant 1.

### 34. Git state is computed but absent from the primary flow  ·  `[capability]`  ·  *open*

**Error.** `_fleet --land` computes dirty, unpushed, behind, review and merged, and
`kel top` renders all of it. The board preview
(`cmd_board_preview`) shows group, agent, isolation, directory, last output,
context and compactions — and uses `.branch` only as a parenthetical. No dirty, no
land, no PR. So from Ctrl+Space you cannot see the state of the work, though the
tool already knows it.

**Surface.** Assert the preview renders the land and dirty fields for an agent
whose fleet entry carries them.

**Fix.** Add them to the preview. The data is already in the document the preview
reads, so this is a rendering change, not a new computation — and `--dirty` is
opt-in for cost reasons, so the preview must pass it.

**Test.** Fixture with a dirty worktree and a known land code; assert both appear
in the preview output. Also assert the preview still renders when they are absent,
since `--dirty` is optional.

### Design questions — author judgment, no gate possible

These have no right answer and no trigger that could resolve them from outside.
Recorded so they are not mistaken for defects, and deliberately without invented
acceptance criteria — inventing them is how a doc starts lying about what done
means.

- **What belongs in `ctrl-f`.** Seven flat items today, no grouping, mixing
  `config` (near-daily) with `restore` (rebuilds the entire workspace). The author's
  account is that it "holds too much, but not enough" — both at once, which no
  reordering alone resolves.
- **How the second level announces itself.** One `--footer` names three of seven
  board keys; `ctrl-g`, `ctrl-r`, `ctrl-k` and `ctrl-n` appear nowhere on screen,
  and a sub-menu's header renders only after the key is pressed.
- **Whether `kel top` should act.** It is read-only by design, and shows a `›`
  cursor and action-shaped verbs (dirty, unpushed, behind, review) with nothing
  bound. Either the affordances go or the actions arrive.
- **One interaction model or two.** The board filters live with no mode; `kel top`
  needs `/` to enter one. `q` quits `kel top` and types into the board's filter.
  Reload is `ctrl-r` in one and `r` in the other.
- **Whether `kel cheat` should teach tmux at all.** Its WINDOWS and PANES columns
  document `prefix c`, `&`, `z`, `x` and the arrows — native tmux keys kel never
  binds. Under rule 4 there are two defensible answers and they lead opposite ways:
  kel owns those verbs and the columns become kel commands, or kel is explicitly a
  layer over tmux and teaching its keys is a service. Today it does the second while
  the rest of the tool argues for the first.



- ~~**Fleet cost / token view.**~~ **Un-parked and shipped in v0.4.1.** The gate
  was "only if Claude Code exposes cost in a hook payload or `--json` output — a
  stable interface, never a scrape." It does, just not where this entry looked:
  not in hooks, but in the **`statusLine`** payload, which hands a script
  `context_window.used_percentage`, `cost.total_cost_usd`, `rate_limits.*` and
  `prompt_cache.*` on stdin, locally, without consuming tokens. Documented and
  versioned — a real interface, not a scrape. `kel statusline` records it to
  `<wid>.ctx`; the bar, `kel ls --json` and the board read it. The lesson worth
  keeping: this sat parked for a release because the search was for a *hook*.
- ~~**Config file / theming**~~ — **shipped v0.8.**
  (`~/.config/kel/config.toml` + `.kel/config.toml`): status-line format,
  colours, state glyphs, board size, keybind opt-outs, the `KEL_*` env vars
  migrated in. It was parked behind "the v1.0 Go rewrite", which no longer
  exists as a milestone — and it has to land *before* #10 presets so kel never
  runs two config systems at once. See `rollout.md` § v0.8.
- **Two-slot / swap-pane layout** — cut in `spec.md` §5c. Revisit only against a
  pain the popup board cannot address.
- **PR status on the board row; attach-to-external-agent** — candidates in
  `spec.md`, none committed.

---

## Known interactions — not features, things that will bite

### Claude Code Agent Teams

Claude Code has an experimental multi-agent mode
(`CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS=1`): a lead session spawns named
teammates that share a task list and message each other.

**It does not compete with kelsier**, and it is worth being explicit about why,
because the names sound like the same product. Agent teams are *intra-session*:
one team per session, scoped to that session, one repo, lead-plus-teammates,
tokens scaling per teammate. kel's problem is *inter-session*: N sessions the
human started, across repos, and which one is blocked. Nothing to defend
against and nothing to copy.

**But there is a concrete collision.** With `teammateMode: "tmux"` (or `"auto"`
inside tmux), Claude Code **splits panes inside the window it is running in** —
which, under kel, is one of kel's agent windows. Consequences:

- `kel snapshot` records those teammate panes as ordinary panes. On restore,
  `restore_from_snapshot` rebuilds them as bare shells with the saved cwd, and
  `KEL_RESTORE_CMDS` won't re-run them (nor should it). You get a window with
  the right geometry and three empty shells.
- `effective_state`'s "is anything but a shell alive here?" check reads
  teammate panes as evidence the agent is alive, so a lead that dies beside a
  live teammate would not be flagged `dead`.
- the bar's pane count (`·3`) starts counting teammates, which is arguably
  correct but was never the intent.

**Not worth pre-solving.** The cheap defensive move, if teams ever get switched
on, is for `kel snapshot` to record pane count but restore only pane 0 for a
window whose agent has a recorded `claude_session` — let the agent rebuild its
own teammates on resume. Recorded here so the failure is recognised in ten
seconds instead of debugged.

**Default is `"in-process"`**, so this is inert until deliberately enabled.

---

## Rejected — with rationale

### R1. Shared context between agents  ·  `[violates]`

Context management is a hard non-goal, and `CLAUDE.md` + Claude Code's own
memory already cover it — the author's read and `agy`'s agreed. Anything that
syncs or broadcasts context is out. This kills the "context-broadcast between
agents" note in `rollout.md`.

### R2. Claude Code Skills / MCP integration  ·  `[violates]`

The agent's environment is the agent's. kel wiring tools or MCP servers into a
session is the definition of a harness.

### R3. Documentation surfacing & drift detection  ·  `[violates]`

Two versions, both out. **The shortcut** (`kel docs` — find and open the repo's
`CLAUDE.md` / `AGENTS.md` / `README`) is feature creep: you already have a shell
in that pane, and `fzf` + `bat` + your editor do this with no help from kel.
**Drift detection** (does `CLAUDE.md` still match the code?) needs an LLM; kel is
a bookkeeping script and doesn't reason about content.

### R4. Codebase structure / tree view  ·  `[violates]`

`yazi` / `eza --tree` / `tre` in an adjacent pane. Explicit non-goal (§"Not a
file manager"); the author's instinct to push back is right.

### R6. Quick peek — `kel peek`  ·  `[obsoleted, not violating]`

Cut 2026-08-30. It was a good idea when written and two things shipped since
that ate it:

- the **board preview** shows the last 8 non-blank lines, plus the `says` line
  carrying `notification_text` — the literal permission prompt — and context,
  cost, compactions and uncommitted files
- **`kel top`** shows a LAST OUTPUT column for *every* agent at once, which is
  strictly more than peeking at one

What peek would still uniquely offer is narrow: deep searchable scrollback
(~1000 lines through `less` rather than 8), read-only by construction so you
cannot fat-finger into an agent's prompt, and zero movement between groups.
Against that, switching windows is one keystroke — the tool's entire premise —
and spending a keybinding to avoid a keystroke is a bad trade.

**If the want ever returns, build it into `kel top`** as a row expander:
`enter` on the highlighted row pages that agent's scrollback, `q` back to the
table. That is where you are already standing when the question occurs to you,
it costs no new keybinding, and it is a view rather than an action so it does
not cross `kel top`'s read-only boundary.

Original entry kept below for the design detail, should that happen.

<details><summary>the original #5</summary>

### 5. Quick peek — `kel peek`  ·  `[fits]`

Glance at an agent's recent output without leaving the pane you're typing in.
Same core loop as jump-to-blocked: see *why* an agent is blocked without losing
your context.

**Surface.** `kel peek [name]`, bound to `prefix v` (unbound in stock tmux).

**Target.** No name → the agent whose window most recently produced output
(`tmux list-windows -a -F '#{window_activity} #{window_id}' | sort -rn | head`),
falling back to the newest `.state` mtime if that's ambiguous. `name` → that
agent.

**Shows.** `display-popup -w 95% -h 90% -E` running
`tmux capture-pane -t <wid> -p -e -S -1000 | less -R +G` — last ~1000 lines,
colour preserved, scrolled to the bottom. `less` keys to scroll and search;
`q` drops you back exactly where you were.

**Edge case.** If the agent has shelled into a full-screen TUI (lazygit, an
editor), `capture-pane` grabs the alternate screen and the capture looks odd.
Acceptable for a one-shot bash peek.

**Does not.** Send keystrokes, switch windows, follow live output, touch state.

**Partly absorbed by v0.4.1.** A good share of "let me peek at that one" was
really "how deep is this session," which is now a number on the bar and in
`kel ls`. Still worth building for the other half — *why* it's blocked — but
it dropped a notch in value.

**Gate:** do it alongside #2. ~20 LOC.

---

</details>

### R5. Bespoke diff viewer  ·  `[violates]`

`lazygit` + the editor (`spec.md` §3). Merge-readiness (#6) gives the *status*
of a branch — ahead/behind, pushed — never a *viewer* or a conflict check.

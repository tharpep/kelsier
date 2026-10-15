# kelsier — working notes for agents

`kel` is a terminal workspace for running several coding agents at once, and
for seeing what each one is doing, where, and to what — the views an IDE gives
you, in the terminal. `tmux` does the multiplexing; `kel` does the bookkeeping.
Go for everything that reads or renders; bash for the hooks, the installer, and
the mutating commands until they move.

**kel is mid-pivot (v0.10)** to an Orca-shaped sidebar — see *Where things
stand* at the bottom. `v0.9.0` is the tag for the old design.

Read `docs/spec.md` for the design and `docs/rollout.md` for the build order and
current status. **`rollout.md` is the source of truth for what to work on next.**
This file is the operating manual: where things live, what must never break, how
to verify a change, and the traps that have already cost time.

---

## The bet, and the one distinction that matters

> The valuable part is the bookkeeping, not the interface.

This is about **the agent's interface**, not kel's. kel must never wrap Claude
Code, proxy its API calls, inject prompts, or manage its context — an agent in a
kel window is byte-identical to one in a bare terminal. kel's *own* interface
(the sidebar, the status bar, `kel top`) is explicitly in scope and is the whole
point of the pivot. Do not cite the line above as an argument against improving
kel's own UX.

Non-goals, held firm: not a harness, not a terminal, not a multiplexer, not a
file manager / editor / git client (kel *opens* `yazi`, `$EDITOR` and `lazygit`
in the right place, it never reimplements them), not a daemon, not
cloud-anything, not a team tool. `docs/spec.md`
§2 has the full list and `docs/backlog.md` has a *Rejected* section with
rationale — check it before proposing a feature, several good-sounding ideas are
already ruled out there.

Scope calls favour the author's own workflow over hypothetical other users.
This is a personal tool, run on three machines, intended to be shareable at
v1.0 — not a product chasing an audience.

---

## What `ls` won't tell you

The tree is self-describing. These relationships are not.

| | |
|---|---|
| `bin/kel` | the hooks, the statusline adapter, and the mutating commands (`new`, `kill`, `restore`, `sweep`…) until they move to Go. No longer reads the fleet itself |
| `cmd/kel-fleet` | **the only fleet reader** since v0.10. `bin/kel`'s `fleet_json` runs it and fails loudly without it — there is no bash fallback any more |
| `cmd/kel-top` | `kel top`, and the seed of the sidebar |
| `internal/fleet` | the fleet document, shared by both Go binaries |
| `install.sh` | requires Go and builds both binaries. Merges into `~/.claude/settings.json` with jq, non-clobbering, keeping the newest three backups. Every line it writes outside this repo has an inverse in `--uninstall` |
| `docs/rollout.md` | **start here.** The source of truth for what to work on next — `spec.md` section numbers are not a build order |
| `docs/backlog.md` | candidates, each with a *gate*: the specific pain that unlocks it. Its *Rejected* section rules several good-sounding ideas out already |

`bin/kel` is `cmd_<name>` functions with a `main` dispatcher at the bottom.
Internal subcommands are `_`-prefixed (`_fleet`, `_cfgdump`) and are wired into
tmux or the tests, never typed by hand. Helpers are plain snake_case
(`ctx_field`, `resolve_pane`, `si_units`) — the `_` prefix means "dispatcher
entry", not "private", so a new helper that takes one reads as a subcommand that
isn't.

---

## The on-disk contract

State lives in `${XDG_STATE_HOME:-~/.local/state}/kel/`. **This format is the
interface**, not the language — bash writes most of it and Go reads it, and a
desktop app may one day read it too (spec §12c). Changing it is a breaking
change; migrate old records in `migrate_meta`.

| path | contents |
|---|---|
| `sessions/<group>/<name>.json` | one record per managed agent. Keyed by group **and** name — two repos can each have a `docs`. |
| `<window-id>.state` | `<state> <epoch> <note>` — one line, atomic write |
| `<window-id>.ctx` | `<pct> <cost> <in_tokens> <ctx_size> <rate_5h> <epoch> <model>`, tab-separated |
| `snapshot.json` | full workspace shape, format v2 since v0.10: `{version:2, saved_at, saved_epoch, last, windows:[{name, group, layout, panes:[{cwd,cmd}]}]}`. `.prev` keeps one generation. Restore still reads v1 (`groups` keyed by group) |
| `snapshots/<epoch>.json` | previous workspaces, archived by `kel` before a fresh start so the hooks cannot overwrite them; newest five kept |
| `.restoring` | lockfile held during a restore (see Invariants) |
| `.stash/<claude-session-id>` | pane id from `SessionStart`, for hooks with no `$TMUX_PANE` |
| `doctor.json` | cached probe results |
| `pr-cache/<owner>__<repo>.json` | PR list from `gh`, plus `.at` with the fetch epoch |
| `sidebar.json` | `{selected: <entry id>, expanded: {<space key>: bool}}` — the sidebar's cursor (`s:<space>`, `a:<window id>` or a pane id) and the spaces opened or folded by hand, shared so every window's sidebar shows the same thing |
| `sidebar-cache.json` | the last fleet document the visible sidebar loaded, so a sidebar just shown draws at once, before git is asked anything |
| `quota` | `<5h%> <5h reset> <7d%> <7d reset> <spend%> <spend reset> <epoch seen>`, space-separated; `-1` = not reported, `0` reset = not given. Written by `kel statusline` only from a payload carrying rate limits; `kel-fleet` reads it as `quota`, nulling a figure whose window reset or that is older than its window |
| `<window-id>.seen` | one epoch: when the window was last shown. `kel-fleet` reads it for `unseen` on a `done` agent (no file = unseen); the shown sidebar writes it for its own window |
| `<window-id>.edit` | two lines: the path the window's agent last edited, then its first changed line. Written by the `PostToolUse` hook (matched to `Edit\|Write\|MultiEdit` in settings.json), read by `kel _panel edit` |
| `sidebar-off` | present after `kel sidebar off`; no new sidebars until `kel sidebar on` |
| `ports.json` | `{at, panes:{<pane pid>: [ports]}}` — the listening-port scan behind `_fleet --panes`, reused for 10 s |

**The directory is `0700`.** The records carry repo paths, branch names and
Claude session ids, and `<window-id>.state` carries Claude Code's notification
text — the agent quoting a command back at you, greps and file paths included.
`migrate_meta` re-applies the mode so pre-0.9 installs get fixed. Deliberately
**not** a global `umask 077`: that would also apply to the files
`git worktree add` creates, and kel has no business writing your source at 0600.

State is keyed by **window id**, metadata by **group + name**. tmux auto-names
every `claude` window `claude`, so name-keyed state would collide.

**One session, so the group lives on the window.** Since v0.10 every kel window
is in the single `$KEL_PREFIX` session, and kel stamps its group on the window
as the tmux option `@kel-group`. Where a window's group comes from, in order:
its record (matched by window id + name), `@kel-group`, a legacy v0.9
`kel/<group>` session name, the repo its first pane stands in. Any lookup of
"the window I am in" must target `$TMUX_PANE` — with one session, tmux's
"current window" is whichever was selected last.

**The sidebar pane is kel's, not the user's.** Every kel window carries one
(v0.10 M2), marked with the pane option `@kel-sidebar` (and the window with
`@kel-has-sidebar`). It runs `kel-top --sidebar`, a live non-shell — so
anything that asks "what is the user's pane" or "is anything alive in this
window" must skip it: use `agent_pane <wid>` in bash; `kel-fleet` drops it
before anything else sees the panes. Six lookups had to change for M2 (the
dead check twice, `kel new`'s and relaunch's send-keys, restore's first pane,
`resolve_pane`'s cwd match).

**Read it through `fleet_json`, never by re-deriving.** `kel _fleet [--dirty]
[--land] [--pr]` — that is, `kel-fleet` — turns all of the above into one JSON
document, and `ls` / `--json` / `status-line` / the completions / `kel top` all
read it. Adding a field means adding it there once, in Go. `--panes` (v0.10 M1)
adds every pane in every kel window — kind (agent / server / shell / program),
cwd, listening ports and git position — for the sidebar; it is opt-in because
its git and port lookups do not fit the bar's redraw budget. `--here` (M4)
adds where the focused pane stands — one `git status` — for the bar while the
sidebar is off. `quota` is always present (null when no session reported one).

### Agent states

`idle` · `working` · `waiting` (blocked on you) · `done` · `throttled` (waiting
on quota, resumes itself) · `dead` (process gone, computed at read time, not a
hook). `kel jump` matches `waiting` only.

---

## Invariants — do not break these

The **kind** column is the honest one. `guarded` means an assertion in
`test/kel-test.sh` checks it; `partial` means one clause is checked and another
is not; `prohibition` means it holds by review and no integration run could
establish it. An unlabelled invariant would read as verified, which is why the
column exists rather than a trailing sentence.

| # | invariant | kind | what actually proves it |
|---|---|---|---|
| 1 | **`kel kill` must never destroy the only copy of work.** Refuses on uncommitted or unpushed changes in a worktree, names what is at risk, `-f` overrides. The one unforgivable bug in this category of tool — treat any change near it with suspicion. When `kill` moves to Go, its tests move first. | guarded | `"a dirty worktree is refused"` checks the refusal, that the window survives, and `"...and names the file at risk"` that the path is in the message. |
| 2 | **A restore must not be able to eat its own snapshot.** Rebuilding panes fires kel's own `after-split-window` / `pane-exited` hooks, which run `kel snapshot` in the background. `restore_from_snapshot` takes the `.restoring` lockfile and reads a private `mktemp` copy. The lock is a **file, not an env var** — the hooks spawn a fresh `kel` from the tmux server, which inherits nothing. The sidebar design adds a pane to every window and hooks to create them; the lock must cover those, so re-check this then. | guarded | three real restore cycles diff `snapshot.json` before and after; a fresh lock is proven to block a write and a stale or unparseable one is proven not to. |
| 3 | **Never report a guess as a fact.** A dead agent must not read `working`; a failing `gh` must not render as "no PR"; a missing `kel-fleet` must not render as "no agents"; an unknown value gets its own distinct display, never a plausible-looking default. | guarded | dead-not-working, null-context-not-`0%`, `"a failing gh reads unknown"`, and `"a missing kel-fleet fails loudly"` (plus the bar saying so). |
| 4 | **kel never types into an agent pane** except the initial launch command — and never into its own sidebar. kel adds its sidebar pane to every window (and `fit` re-pins *that pane's* width), but never moves, resizes or re-points the user's panes, and never alters what runs in one. **Changed in v0.10:** the old rule forbade kel splitting a window at all; the sidebar is the one exception. (Moving panes with `swap-pane` was considered and rejected — `spec.md` §5b.) | prohibition | nothing, deliberately — "never, across a codebase" holds by reviewing the `send-keys` call sites (launch, restore's pane replay, relaunch). |
| 5 | **Hot paths stay cheap.** `kel statusline` runs on a 300 ms debounce and is cancelled if it overruns; `kel status-line` runs on every bar redraw. One `jq` pass, one atomic write, and only call `refresh-client -S` when the rendered value actually changed. | ⚠ unguarded | nothing. `refresh-client` appears nowhere in the suite. |
| 6 | **One surface.** Every interactive kel gesture lives in the sidebar (or is a CLI command). No second board, no menus, no cheat sheet — four overlapping surfaces with four key schemes is what v0.10 deleted. A new view is a sidebar mode or a panel tab. | prohibition | review. |
| 7 | **Embed, don't build.** Files, diffs, editing and git are `yazi`, `lazygit`, `$EDITOR` / Neovim (`diffview.nvim` or `delta` for diffs); the browser is the user's own. kel starts or owns none of them: `e` reuses an nvim you opened, or a popup that ends with it. kel decides where they open and at which directory; it never renders a tree or a diff itself (`backlog.md` R4, R5, R7). | prohibition | review. |

---

## Testing

```sh
bash -n bin/kel && bash -n install.sh      # syntax
go vet ./... && go build ./... && go test ./...
test/kel-test.sh                            # ~1 min; it prints its own count
```

Go is **required**. The suite builds `kel-fleet` and `kel-top` from this
checkout into its own work dir and points `$KEL_FLEET_BIN` / `$KEL_TOP_BIN` at
them, so a stale binary in `~/.local/bin` is never what gets tested. The bash
↔ Go differential that guarded the port went with the bash reader; Go unit
tests (`internal/fleet/*_test.go`) cover what the suite cannot reach.

CI (`.github/workflows/ci.yml`) runs exactly this on `ubuntu-latest` and
`macos-latest` on every push, plus `/bin/bash -n bin/kel` on macOS to catch
bash-4 syntax against the stock 3.2.

The suite runs fully isolated on three axes and **will not touch your real
workspace**: its own `TMUX_TMPDIR` (so its `tmux kill-server` cannot reach the
default socket — it once killed a live workspace), its own `XDG_STATE_HOME`,
and `KEL_SESSION=keltest`. Agents are faked with `sleep 9999`, so nothing
reaches an API. Any test you add must keep all three.

**An attached client is testable after all:** `( sleep N ) | tmux -C attach`
is a control-mode client with no terminal, holding the attach open as long as
its stdin. The suite uses it for "current pane"; the notifications section
still says focus suppression is untestable — it no longer has to be.

Run it before every commit. It covers: cross-group name collisions, ambiguous
`kill` resolution, rename scoping, legacy record migration, three consecutive
restore cycles with snapshot integrity, worktree kill-safety, land state, the
one network call and its off switch, and `kel top`'s rendering.

For anything involving hooks or the status line, drive it with a real tmux
server and a synthetic JSON payload rather than reasoning about it — several
bugs in this repo were invisible to inspection and obvious on the first run.

---

## Traps already paid for

Tagged the same way as the invariants: `guarded` has an assertion, `unguarded`
relies on you remembering, `principle` is a fact about bash, tmux or Claude Code
with nothing in kel to assert against. Re-breaking an `unguarded` one is silent.

- `[guarded]` **`IFS=$'\t' read` silently shifts fields.** Tab is IFS *whitespace*, so bash
  folds runs of it into one delimiter and drops leading/trailing empties. One
  empty field (an unset model name) made kel report cost as the context
  percentage. Read multi-field JSON **one field per line** instead.
- `[principle]` **tmux hooks fire during kel's own operations.** Anything that splits or
  closes panes will re-enter `kel snapshot`. Check before adding a hook.
- `[guarded]` **Claude Code's `Notification` is not one event.** Branch on
  `notification_type`; mapping them all to `waiting` made the jump key lie.
- `[principle]` **Hook payloads carry no token/cost data** — that comes from `statusLine`,
  which is a separate settings key with a different payload shape.
- `[guarded]` **`context_window.used_percentage` is `null`, not `0`**, before the first API
  call and again right after `/compact`. `// 0` in the jq turns "not measured"
  into a confident green `0%` on a full context window. Keep it empty and render
  the unknown as unknown.
- `[partial]` **`agent.name` in a `statusLine` payload is not a subagent.** It marks a
  session started with `claude --agent`. Task-tool subagents never invoke this
  command — they go through a separate `subagentStatusLine` setting carrying a
  `tasks[]` array. No per-subagent cost is exposed in either payload, so a
  combined main-plus-subagents dollar figure cannot be computed.
- `[principle]` **`kel new` sends the agent command with `send-keys`**, so there is a brief
  window where the pane still shows a bare shell and `effective_state` reads
  `dead`. Harmless in practice; do not "fix" it by polling. Tests must wait for
  the process (`agent_up` in the suite) — v0.6 made the fleet read 3x faster
  and immediately started losing that race.
- `[unguarded]` **`command -v` on a binary that does *not* exist costs ~76ms on WSL2.** It
  walks all of `$PATH`, and 28 of this machine's 44 entries are Windows dirs
  under `/mnt/c`. The Go seam therefore stats one known path
  (`$KEL_FLEET_BIN`) instead. Never put a `command -v` for an optional binary
  on a hot path.
- `[unguarded]` **An unconditional `mkdir -p` is not free** — three already-existing dirs
  measured ~10ms, paid by every invocation including each bar redraw.
- `[guarded]` **A hook's stdout lands in view-mode on the pane you are in.**
  `run-shell` (hooks included) shows a command's stdout in view-mode on the
  active pane, which then swallows every key until `q`. `kel snapshot` prints
  where it wrote, so the `after-split-window` hook put you into view-mode on
  every split, for releases. Every `set-hook … run-shell` in `kel.conf` now
  ends `>/dev/null 2>&1`, and the suite checks it. Same family: an
  `if-shell` with an empty branch (`''`) is an error tmux shows the same way.
- `[guarded]` **The suite must not load your tmux config.** `reset()` starts
  tmux with `-f /dev/null`, but a test that kills the server lets *kel* start
  the next one, which read `~/.tmux.conf` — kel's own hooks, running the
  installed kel against the suite's state. CI has no such file, so it only
  bit a dev box. A `tmux` shim on the suite's `PATH` adds `-f /dev/null` to
  every call.
- `[guarded]` **`grep -vx 1` on a column of flags matches empty lines and
  prints nothing.** A user pane's `@kel-sidebar` is empty, so
  `[ -n "$(… | grep -vx 1)" ]` read "no user panes" for every window and
  `sidebar_reap` closed them all. Count instead (`grep -cvx 1`).
- `[guarded]` **`run-shell` shows stdout, never stderr — and `-E` is tmux 3.6+.**
  `man tmux`: *"any output to stdout is displayed in view mode after the command
  finishes."* `die`/`warn` write to stderr, so a keybinding's error vanished. The
  flag that fixes this properly (`run-shell -E`) does not exist below 3.6, and
  kel's floor is 3.0 — a `-E`'d bind dies with *unknown flag -E* and takes the
  whole key with it. Use a literal `2>&1` inside the command string instead.
  `backlog.md` #19 recorded the opposite of all of this for two releases.
- `[guarded]` **One corrupt record must not blank the fleet.** A truncated
  `sessions/*.json` — what a crash mid-write leaves — once made the bash reader
  emit *nothing* (`jq --slurpfile` aborts the whole stream). Go fails per
  record and renders it `unreadable`; the suite asserts the other agents
  survive. Any new bash that slurps records needs a `jq -e .` per file first.
- `[guarded]` **A promise tested only in one implementation is not tested.**
  `pr = off` and "dirty work never reaches `gh`" were asserted against bash's
  own helpers for releases, while `kel-fleet` — which answered whenever it was
  installed — broke both. Assert a promise through the path that actually runs
  (`kel _fleet`), never through an internal helper.
- `[unguarded]` **`prune_state` is not `kel prune`.** `cmd_prune` reaps dead
  *agents*; `prune_state` reaps orphaned state files and is called from `cmd_ls`
  and `cmd_attach`. A test that drives `kel prune` to exercise `prune_state`
  passes vacuously.

---

## Portability

kel runs on a Linux desktop, WSL2, and a **MacBook**. The three GNU coreutils
assumptions that used to break macOS *silently* are fixed, and CI now runs the
whole suite on `macos-latest` so they cannot come back. `bin/kel` uses no bash
4+ features either (macOS ships bash 3.2, and CI checks against it explicitly).

**When writing new shell here, assume BSD userland.** The fixed cases are the
pattern to follow — each was solved by removing the dependency, not by swapping
a GNU flag for a BSD one:

| don't | do |
|---|---|
| `sed 's/\.\(a\|b\)$//'` (`\|` is a GNU BRE extension) | parameter expansion: `${v%.a}` / `${v%.b}` |
| `stat -c %Y` (BSD is `-f %m`) | have the writer record the epoch in the file |
| `sort -V` (absent from BSD sort) | compare `${v%%.*}` / `${v#*.}` arithmetically |
| `setsid` (absent on macOS) | `( cmd & )` |
| `script -qfc CMD file` (BSD wants `script -q file CMD`) | avoid; don't put it in tests |
| `wc -l` (BSD pads the count with spaces: `       1`) | `grep -c .`, or trim before comparing |
| `wc -L` (GNU: display columns; macOS: bytes, so `›` counts 3) | `perl -CS` and `length`, which counts characters |
| assuming a tmux flag is absent (Homebrew ships 3.7; Ubuntu 3.4) | gate the assertion on `tmux -V`, and keep kel itself at its 3.0 floor |
| `timeout` (GNU coreutils; absent on macOS) | prove the property directly instead of by elapsed time |
| relying on `sort` ordering across implementations | compute what you need explicitly (two-pass awk) |

`install/` covers both: `10-system-tools.sh` + `20-user-tools.sh` for
Debian/Ubuntu, `macos-tools.sh` (Homebrew, one script, no sudo) for the Mac.
Both install Go, which is required.
Keep them in step when the toolchain changes.

**Do not solve portability with containers.** kel drives the *host's* tmux
server and spawns agents that edit the host's repos with the host's `claude`
auth; a container has its own PID/PTY namespaces, and on macOS it is a Linux VM
besides, so its tmux could never be the one you attach to. Cross-compilation is
the answer for Go (`GOOS=darwin GOARCH=arm64`), POSIX-clean shell for bash.

One trap that is not about coreutils: a unix socket path caps around 104
characters, and macOS sets `$TMPDIR` to a long `/var/folders/...` path. That is
why `test/kel-test.sh` puts its `TMUX_TMPDIR` directly under `/tmp` rather than
under its own work dir.

---

## House style

- **Bash**: `set -uo pipefail`, no `-e` (many intentional `|| true`). Guard
  every `tmux` call that may race with a dying window. Prefer one `jq` pass over
  several. Keep functions small and `cmd_`-prefixed.
- **Comments explain *why*, and especially why-not.** The codebase records
  rejected approaches inline so they are not re-proposed; keep doing that.
- **Docs are part of the change.** A behaviour change updates `spec.md`
  (design), `rollout.md` (status) and `usage.md` (reference) in the same commit.
  A candidate that is not being built goes in `backlog.md` with a **gate** — the
  specific pain that would unlock it.
- **Commits**: a summary line, then prose explaining the *reasoning* and any
  measurement. Long, specific commit messages are the norm here — the history is
  used as a design record.
- Currently committing directly to `main`; a PR workflow is planned after v1.0,
  once the tool is in real daily use and `main` needs protecting.

---

## Where things stand

**v0.10, the pivot, is in progress.** Steps 1–3 are done: `v0.9.0` tagged as
the marker for the old design; the docs rewritten to the new one; the old
surfaces deleted (board, menus, `kel cheat`, `go` / `move` / `adopt`, the bash
fleet reader). Go is now required.

**Step 4, the sidebar, is being built milestone by milestone** — the plan is
`rollout.md` § v0.10 (M0–M6), each milestone planned with the author before
code. **M0–M3 are done**: one session, `kel` never prompts (`kel restore` by choice),
`_fleet --panes` knows every pane, and every window carries the sidebar —
`spaces` / `agents`, keys, mouse, `done` until seen. `v0.10.0` is tagged
when the author has daily-driven it. **M4 is done** too: the bar is the
workspace footer and Claude's row is the session only, inside kel. **M5 is
done**: `f` `g` `d` `e` `o` open yazi, lazygit, the diff, the agent's last
file and the browser — kel owns none of them, and `KEL_POPUP_TRACE` lets the
suite check what a popup would open without a terminal client. **M6 is next**:
plan and tasks. The
sidebar's look is held by golden frames (`cmd/kel-top/testdata/sidebar`);
change one only after showing the author the frame. The layout
is decided — a sidebar pane in every window, measured against `swap-pane` in
`spec.md` §5b. A design pass after M2 settled the rest (`rollout.md`, "After
M2"): `spaces` / `agents` sections, the visual language and how it is held
(§5d, §5e), one home per fact in the bottom rows (§7), and the editor / diff /
browser embeds (§5f).

Bubble Tea is **v2** (`charm.land/bubbletea/v2`, `View() tea.View`,
`tea.KeyPressMsg`); almost every tutorial online is v1 and will not compile.

Versioning is in `rollout.md`: `0.x` carries no promise, `0.10` follows `0.9`,
and `v0.10.0` is tagged when the sidebar is usable.

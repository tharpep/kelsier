# kelsier — working notes for agents

`kel` is a terminal workspace for running several coding agents at once. `tmux`
does the multiplexing; `kel` does the bookkeeping. One bash script, one tmux
config fragment, one installer. No runtime, no build.

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
(the board, the status bar, `kel top`) is explicitly in scope and worth
investing in; it is what makes the tool get used. Do not cite the line above as
an argument against improving kel's own UX.

Non-goals, held firm: not a harness, not a terminal, not a multiplexer, not a
file manager, not a daemon, not cloud-anything, not a team tool. `docs/spec.md`
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
| `bin/kel` | the whole tool, and the permanent fallback for every Go surface |
| `cmd/kel-fleet` | a port of `bin/kel`'s `_fleet_bash`. The suite asserts the two emit an identical document; a disagreement is a failure, not a curiosity |
| `cmd/kel-top` | **Go only, no fallback.** `kel top` dies without the binary instead of degrading. Do not assume it mirrors `kel-fleet` |
| `internal/fleet` | the fleet document, shared by both Go binaries |
| `install.sh` | merges into `~/.claude/settings.json` with jq, non-clobbering, and backs it up. Every line it writes outside this repo has an inverse in `--uninstall` |
| `docs/rollout.md` | **start here.** The source of truth for what to work on next — `spec.md` section numbers are not a build order |
| `docs/backlog.md` | candidates, each with a *gate*: the specific pain that unlocks it. Its *Rejected* section rules several good-sounding ideas out already |

`bin/kel` is `cmd_<name>` functions with a `main` dispatcher at the bottom.
Internal subcommands are `_`-prefixed (`_board_rows`, `_fleet`, …) and are wired
into tmux or fzf, never typed by hand. Helpers are plain snake_case
(`ctx_field`, `resolve_pane`, `si_units`) — the `_` prefix means "dispatcher
entry", not "private", so a new helper that takes one reads as a subcommand that
isn't.

---

## The on-disk contract

State lives in `${XDG_STATE_HOME:-~/.local/state}/kel/`. **This format is the
interface**, not the language — the Go migration (rollout § v0.6) depends on
both implementations reading and writing it identically. Changing it is a
breaking change; migrate old records in `migrate_meta`.

| path | contents |
|---|---|
| `sessions/<group>/<name>.json` | one record per managed agent. Keyed by group **and** name — two repos can each have a `docs`. |
| `<window-id>.state` | `<state> <epoch> <note>` — one line, atomic write |
| `<window-id>.ctx` | `<pct> <cost> <in_tokens> <ctx_size> <rate_5h> <epoch> <model>`, tab-separated |
| `snapshot.json` | full workspace shape; `.prev` keeps one generation |
| `.restoring` | lockfile held during a restore (see Invariants) |
| `.stash/<claude-session-id>` | pane id from `SessionStart`, for hooks with no `$TMUX_PANE` |
| `doctor.json`, `last-group` | cached probe results, last-used group |
| `usage.log` | `<epoch> <subcommand>` per user gesture — scaffolding for the prune pass, delete it with that card |

**The directory is `0700`.** The records carry repo paths, branch names and
Claude session ids, and `<window-id>.state` carries Claude Code's notification
text — the agent quoting a command back at you, greps and file paths included.
`migrate_meta` re-applies the mode so pre-0.9 installs get fixed. Deliberately
**not** a global `umask 077`: that would also apply to the files
`git worktree add` creates, and kel has no business writing your source at 0600.

State is keyed by **window id**, metadata by **group + name**. tmux auto-names
every `claude` window `claude`, so name-keyed state would collide.

**Read it through `fleet_json`, never by re-deriving.** `kel _fleet [--dirty]`
turns all of the above into one JSON document, and `ls` / `--json` / the board /
`status-line` are each a `jq` over it. Adding a field means adding it there
once. This replaced `gather_rows`, whose 13 positional TSV fields caused three
bugs. It is also the contract the Go `kel-fleet` must match.

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
| 1 | **`kel kill` must never destroy the only copy of work.** Refuses on uncommitted or unpushed changes in a worktree, shows what is at risk, `-f` overrides. The one unforgivable bug in this category of tool — treat any change near it with suspicion. | partial | `"a dirty worktree is refused"` greps the output for `refusing` and counts windows. **"Shows exactly what is at risk" is unguarded** — nothing checks the message names the files. |
| 2 | **A restore must not be able to eat its own snapshot.** Rebuilding panes fires kel's own `after-split-window` / `pane-exited` hooks, which run `kel snapshot` in the background. `restore_from_snapshot` takes the `.restoring` lockfile and reads a private `mktemp` copy. The lock is a **file, not an env var** — the hooks spawn a fresh `kel` from the tmux server, which inherits nothing. | guarded | three real restore cycles diff `snapshot.json` before and after; separately, a fresh lock is proven to block a `kel snapshot` write and a stale or unparseable one is proven not to. |
| 3 | **Never report a guess as a fact.** A dead agent must not read `working`; an expired `gh` token must not render as "no PR"; an unknown value gets its own distinct display, never a plausible-looking default. | partial | dead-not-working and null-context-not-`0%` are both asserted. **The `gh` clause this invariant names is unguarded** — only "no remote" and "`gh` absent" are covered, never "token fails against a real remote". |
| 4 | **kel never sends keys into an agent pane** except the initial launch command, and never splits, resizes or re-points a user's panes. | prohibition | nothing, deliberately. "Never, across a whole codebase" is not establishable by one integration run; it holds by reviewing the `send-keys` call sites, of which there are five (launch, restore's pane replay, twice in relaunch). |
| 5 | **Hot paths stay cheap.** `kel statusline` runs on a 300 ms debounce and is cancelled if it overruns; `kel status-line` runs on every bar redraw. One `jq` pass, one atomic write, and only call `refresh-client -S` when the rendered value actually changed. | ⚠ unguarded | nothing. `"...but hot paths are not counted"` sounds like this and is not: it asserts only that hot-path commands skip `usage.log`. `refresh-client` appears nowhere in the suite. |

---

## Testing

```sh
bash -n bin/kel && bash -n install.sh      # syntax
go vet ./... && go build ./...              # Go, if installed
test/kel-test.sh                            # ~2 min; it prints its own count
```

Go is **optional**: `bin/kel` falls back to its bash implementation whenever
`$KEL_FLEET_BIN` is absent, and `./install.sh` works with no toolchain. But
where Go exists, the suite builds `kel-fleet` and asserts the two produce an
identical document — that differential is the entire safety net for the port,
so a disagreement is a failure, not a curiosity.

CI (`.github/workflows/ci.yml`) runs exactly this on `ubuntu-latest` and
`macos-latest` on every push, plus `/bin/bash -n bin/kel` on macOS to catch
bash-4 syntax against the stock 3.2.

The suite runs fully isolated on three axes and **will not touch your real
workspace**: its own `TMUX_TMPDIR` (so its `tmux kill-server` cannot reach the
default socket — it once killed a live workspace), its own `XDG_STATE_HOME`,
and `KEL_SESSION=keltest`. Agents are faked with `sleep 9999`, so nothing
reaches an API. Any test you add must keep all three.

Run it before every commit. It covers: cross-group name collisions, ambiguous
`kill` resolution, rename/move scoping, legacy record migration, three
consecutive restore cycles with snapshot integrity, and worktree kill-safety.

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
- `[unguarded]` **`fzf --popup` needs a tty**; a tmux key binding has none, so
  `run-shell` + `fzf --popup` dies with *inappropriate ioctl for device* and the
  key silently does nothing. Use tmux's `display-popup`, and pass `KEL_*`
  across with `-e` — a popup runs a fresh shell off the tmux **server's**
  environment, not the caller's.
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
| `timeout` (GNU coreutils; absent on macOS) | prove the property directly instead of by elapsed time |
| relying on `sort` ordering across implementations | compute what you need explicitly (two-pass awk) |

`install/` covers both: `10-system-tools.sh` + `20-user-tools.sh` for
Debian/Ubuntu, `macos-tools.sh` (Homebrew, one script, no sudo) for the Mac.
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

**v0.6 is done** — `kel _fleet --json` (one computed view), the Go `kel-fleet`
behind the fallback seam, and `kel top`. `#5` peek was cut; the reasoning is in
`backlog.md` R6.

Go is a strangler fig on `main`: bash stays the permanent fallback *and* the
differential-test oracle, read surfaces port first, mutating commands last or
never. `rollout.md` § v0.6 has the rules — follow them rather than re-deriving
the strategy. Bubble Tea is **v2** (`charm.land/bubbletea/v2`, `View() tea.View`,
`tea.KeyPressMsg`); almost every tutorial online is v1 and will not compile.

**v0.7, v0.8 and v0.9a are done** too: land state, `kel sweep`, `.kel/group`,
`install.sh --uninstall`, `config.toml`, the README, pane-border state, and the
board learning panes rather than only agents.

**v0.9 shipped as a clarity pass, not the prune pass.** The prune pass asks
*did I reach for it*, and nothing recorded an answer — so it was gated on
something no amount of work could produce, while sitting last in front of v1.0.
It has moved to its own card **after v1.0** and no longer blocks it.

What v0.9 did instead was subtract *confusion*, decided by inspection:

- eight bugs, two of which made shipped surfaces silently inert (the shell
  completions had been dead since v0.6; `kel ls --json` could emit English)
- `kel ls` grew a column header, and `inplace` renders as `repo`
- `doctor` prescribes a fix per failed probe; `install/macos-tools.sh` added
- **`kel adopt`** split out of `kel move`, which now refuses rather than
  silently adopting
- **`kel restart` is now `kel relaunch`** — no alias — and the two menus agree
  on every key they share
- state dir `0700`, `pr = "off"` to close the one network call, and the usage
  counter (`kel _usage`)

**Next: v1.0.** `install.sh` has run on a Mac as of 2026-09-01 — every doctor
probe green, both Go binaries built, the suite passing there.
`install/macos-tools.sh` remains syntax-checked and nothing more. It installs
around fifteen formulae, appends a block to `~/.zshrc` and installs Node, so it
needs a machine you are willing to provision. That is the last gate.
`rollout.md` § v0.9-hardening has what else that pass changed.

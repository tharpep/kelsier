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

## Layout

```
bin/kel              the whole tool — one bash script, ~1400 lines
tmux/kel.conf        keybindings, status line, hooks; sourced from ~/.tmux.conf
install.sh           symlinks kel, sources kel.conf, merges hooks + statusLine
                     into ~/.claude/settings.json (jq, non-clobbering, backs up)
install/             machine provisioning (apt — Debian/Ubuntu only, see Portability)
completions/         bash + zsh completion
test/kel-test.sh     the regression suite — run it before every commit
docs/spec.md         design and rationale
docs/rollout.md      build order, current status, what's next   <- start here
docs/backlog.md      candidates, with a gate on each; tags [fits]/[borderline]/[violates]
docs/usage.md        user-facing reference
docs/setup.md        provisioning a machine
```

`bin/kel` is organised as `cmd_<name>` functions with a `main` dispatcher at the
bottom. Internal subcommands are prefixed `_` (`_board_rows`, `_board_preview`,
…) and are wired into tmux or fzf, not typed by hand.

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

State is keyed by **window id**, metadata by **group + name**. tmux auto-names
every `claude` window `claude`, so name-keyed state would collide.

### Agent states

`idle` · `working` · `waiting` (blocked on you) · `done` · `throttled` (waiting
on quota, resumes itself) · `dead` (process gone, computed at read time, not a
hook). `kel jump` matches `waiting` only.

---

## Invariants — do not break these

1. **`kel kill` must never destroy the only copy of work.** It refuses on
   uncommitted or unpushed changes in a worktree and shows exactly what is at
   risk. `-f` overrides. This is the one unforgivable bug in this category of
   tool; treat any change near it with suspicion and test it.
2. **A restore must not be able to eat its own snapshot.** Rebuilding panes
   fires kel's own `after-split-window` / `pane-exited` hooks, which run
   `kel snapshot` in the background. `restore_from_snapshot` takes the
   `.restoring` lockfile and reads from a private `mktemp` copy. The lock is a
   **file, not an env var** — the hooks spawn a fresh `kel` from the tmux
   server, which inherits nothing.
3. **Never report a guess as a fact.** A dead agent must not read `working`; an
   expired `gh` token must not render as "no PR"; an unknown value gets its own
   distinct display, never a plausible-looking default. The status bar's whole
   value is that you can trust it.
4. **kel never sends keys into an agent pane** except the initial launch
   command, and never splits, resizes or re-points a user's panes.
5. **Hot paths stay cheap.** `kel statusline` runs on a 300 ms debounce and is
   cancelled if it overruns; `kel status-line` runs on every bar redraw. One
   `jq` pass, one atomic write, and only call `refresh-client -S` when the
   rendered value actually changed.

---

## Testing

```sh
bash -n bin/kel && bash -n install.sh      # syntax
test/kel-test.sh                            # 75 cases, ~60s
```

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

- **`IFS=$'\t' read` silently shifts fields.** Tab is IFS *whitespace*, so bash
  folds runs of it into one delimiter and drops leading/trailing empties. One
  empty field (an unset model name) made kel report cost as the context
  percentage. Read multi-field JSON **one field per line** instead.
- **`fzf --popup` needs a tty**; a tmux key binding has none, so
  `run-shell` + `fzf --popup` dies with *inappropriate ioctl for device* and the
  key silently does nothing. Use tmux's `display-popup`, and pass `KEL_*`
  across with `-e` — a popup runs a fresh shell off the tmux **server's**
  environment, not the caller's.
- **tmux hooks fire during kel's own operations.** Anything that splits or
  closes panes will re-enter `kel snapshot`. Check before adding a hook.
- **Claude Code's `Notification` is not one event.** Branch on
  `notification_type`; mapping them all to `waiting` made the jump key lie.
- **Hook payloads carry no token/cost data** — that comes from `statusLine`,
  which is a separate settings key with a different payload shape.
- **`kel new` sends the agent command with `send-keys`**, so there is a brief
  window where the pane still shows a bare shell and `effective_state` reads
  `dead`. Harmless in practice; do not "fix" it by polling.

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

`install/` is still apt-only — provisioning a Mac is a separate, larger job and
is not scheduled.

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

**v0.5 is complete** — `#14` notification fidelity, `#15` compaction counter,
`#13` restart-in-place, `#1` fleet notifications, `#4` per-group badge, plus
the macOS portability fixes and CI. Next is **v0.6** (`rollout.md`):
`kel _fleet --json`, then `#2` `kel top`, then `#5` peek.
**Go enters at v0.6** as a strangler fig on `main`
(`kel _fleet --json` first, then `kel top` in Bubble Tea **v2**), with bash
retained as the fallback and as the differential-test oracle. `rollout.md`
§ v0.6 has the rules; follow them rather than re-deriving the strategy.

# kelsier — spec

A terminal workspace for running several coding agents at once. `tmux` does the
multiplexing; `kel` does the bookkeeping.

This document is the design. `rollout.md` is the order things get built in, and
that order is deliberately far more conservative than the section numbers here
imply — read it before writing code.

---

## 1. Premise

Running N interactive agent sessions is not hard to *start* and very hard to
*keep track of*. The failure mode is tab sprawl: you lose which terminal holds
which task, which branch it's on, and which one is blocked waiting on you.

Existing tools solve this by owning the agent — a harness, a wrapper, a GUI.
That trades the sprawl problem for a capability ceiling.

`kel` does not wrap anything. `tmux` allocates a PTY and the agent owns it
completely. Claude Code inside a `kel`-managed window is byte-identical to
Claude Code in a bare terminal: same TUI, same keybindings, same MCP, same
hooks, same everything. `kel` only tracks state, renders the fleet, and gets
out of the way.

**The bet:** the valuable part is the bookkeeping, not the interface. Anything
that requires reimplementing the agent's interface is out of scope forever.

---

## 2. Non-goals

Explicit, because each of these is a thing this project will be tempted into.

- **Not a harness.** Never proxies model API calls. Never injects prompts.
  Never manages context.
- **Not a terminal.** Uses whatever emulator the user has.
- **Not a multiplexer.** `tmux` exists and is thirty years old.
- **Not a file manager.** `yazi` exists and is better than anything this
  project would build. The panel may *launch* it. It will never reimplement it.
- **Not a daemon.** State lives on disk and in `tmux`; nothing runs in the
  background but `tmux` itself.
- **Not cloud-anything.** Fully local, no telemetry, no account.
- **Not a team tool.** Single user, single machine.

---

## 3. What the user actually does with it

Two intentions, both first-class:

1. **Run several agents in parallel and never lose track.** Which one is
   working, which is blocked on a permission prompt, which is done. Jump to the
   blocked one in one keystroke.
2. **Hand-edit the code the agents produce.** The user is building a custom
   Neovim config and wants editing-by-hand to be a smooth part of the loop, not
   a context switch out of it. The agent writes a first pass; the user reads it,
   fixes it, steers. `kel` must make "open this session's repo in my editor"
   frictionless and must never re-point that editor at a different directory.

The review surface for (2) is `lazygit` plus the editor, not a bespoke diff
viewer.

---

## 4. Substrate

`tmux` on Linux. On the user's machine that is **WSL2 Ubuntu** — native Windows
PowerShell has no `tmux` and nothing with `swap-pane` semantics, so the design
does not target it. Setup, package list, and WSL2 gotchas live in `setup.md`.

Everything `kel` produces is text: a `tmux` config fragment, hook scripts, a
status/jump script, an install script. That is a dotfiles repo. A second
machine is: install Ubuntu, `git clone`, `./install.sh`.

---

## 5. Layout

### 5a. The shipping layout: native windows + popup board

Each agent session is an ordinary `tmux` window. `kel` owns which windows
exist and what state each is in; it does **not** own what happens inside a
window.

**Pane layout inside a window is entirely the user's, via native `tmux`.** A
session's window can be a lone agent pane, or `[ agent | nvim ]`, or that plus
a test-runner strip below — split with `prefix %` / `prefix "`, resized freely,
and persistent. Every session can carry a different layout. This is *more*
freedom than the rejected two-slot design (5b), which locked every session to
`[ agent | one panel ]`. `kel` never splits, resizes, or sends keys to a
session's panes.

The fleet view is a `tmux` popup — `display-popup -E "kel board"` — opened over
whatever window is current and dismissed with a keystroke. It does not occupy a
pane. It does not need to survive switches because it is transient by design.
(If a persistent board is wanted, a dedicated window running `kel board` in its
non-popup form does that — nothing forbids it; it is just not forced into every
session.)

This is the layout that gets built. It has no held-pane lifecycle, no geometry
sync, no override of native `tmux` window switching.

### 5b. Rejected: the two-slot / swap-pane architecture

An earlier design used one visible window split into two slots, with every
agent and editor living in a hidden holding session and swapped into the
visible slots via `swap-pane`. It is recorded here so the idea has a written
answer and does not get re-proposed.

Why it was cut:

- **`swap-pane` is the hardest thing in the design and buys less flexibility
  than native windows, not more.** Native `tmux` splits give real side-by-side
  comparison; the rigid two-slot layout forbids it.
- **Geometry desync.** `-x/-y` on the holding session only covers creation.
  Every terminal resize desyncs the hidden panes and the next swap-in janks
  unless a `client-resized` hook re-lays-out every hidden pane.
- **`prefix 1`..`9` collides** with default `tmux` window switching — a
  muscle-memory tax everywhere else in `tmux`, forever.
- **Three panel modes competing for one right slot** means you cannot see the
  board while editing. Triage becomes board -> edit -> board cycling, which is
  the friction that sends people back to plain tabs.
- **Status line breaks past ~6 sessions.** `1:auth-fix? 2:rate-limit* ...`
  stops being readable and starts being truncated.

If the popup board turns out to be insufficient in daily use, revisit — but
only with a concrete pain that the popup cannot address.

---

## 6. Switching

The board is **not** the switch mechanism. If switching means open-menu,
find-row, press-enter, it is slower than the terminal tabs it replaces and it
will be abandoned. Switching is one keystroke.

| Binding | Action |
|---|---|
| native `tmux` window nav (`prefix n`/`p`/number) | jump between sessions |
| `` prefix ` `` | jump to the next session in `waiting` state |
| `prefix Tab` | toggle to previous session (native `tmux` `last-window`) |

The `` ` `` binding is the capability terminal tabs cannot offer: not "go to
session 3" but "go to whoever is blocked on me." Expect it to become the
primary navigation.

`kel` binds only `` ` ``. It does not rebind native window navigation.

---

## 7. Status line

`tmux`'s own window list is not a fleet view — it has no knowledge of agent
state. Render one:

```
set -g status-left "#(kel status-line)"
```

```
[kel] 1:auth-fix? 2:rate-limit* 3:docs! 4:sazed
```

Suffixes encode state: `?` waiting on you, `*` working, `!` done, bare name for
idle.

`#()` only re-runs on the status interval, so a state change lags by up to the
interval (default 15s). Every state hook must call `tmux refresh-client -S`
after writing its state file, so the bar updates the moment an agent blocks.

Past ~6 sessions this line stops fitting. That is the point at which the popup
board earns its place — not before.

---

## 8. Session model

A session owns exactly one `tmux` window and exactly one working directory.

```
Session
  name          string    unique, slug, becomes the tmux window name
  repo          path      the origin repository
  cwd           path      where the agent actually runs
  isolation     enum      inplace | worktree
  branch        string    worktree only
  agent         string    command to run in the window
  state         enum      idle | working | waiting | done | dead | unknown
  created_at    timestamp
  window        string    tmux window id, e.g. @7 — stable, never an index
```

**Isolation is opt-in.** Default is `inplace`: the agent runs in the repo you
are standing in, editing your actual checkout. Forcing a worktree on a one-line
fix is the kind of ceremony that makes people abandon their own tooling.

`worktree` creates a linked git worktree on a new branch. Two agents can then
write to the same repo without touching each other's files. Worth it for
parallel feature work; wasteful for a quick fix.

**Worktree provisioning.** A fresh worktree has no untracked files — no
`node_modules`, no `.venv`, no `.env`, no build cache. Per-repo setup hook, run
after creation, path configurable:

```
.kel/setup      # executable, runs in the new worktree, gets $KEL_REPO
```

Without this, worktree sessions are unusable on any real project. Not a
nice-to-have.

---

## 9. State detection

State comes from the agent's own hook system, not from scraping terminal
output. Screen-scraping couples you to a TUI's rendering.

For Claude Code, three hooks in `~/.claude/settings.json`, each writing one
line to a state file and touching `tmux`:

| Hook event | State |
|---|---|
| `UserPromptSubmit` | `working` |
| `Notification` | `waiting` |
| `Stop` | `done` |

`Notification` carries matchers (`permission_prompt`, `idle_prompt`,
`auth_success`), so "wants permission" can be distinguished from "went idle" if
that turns out to matter.

Hooks run in their own process with no controlling terminal and cannot write to
`/dev/tty`. They *can* run `tmux` commands, since the `tmux` client is a
separate process talking to the server socket. They need the target window id:
capture `$TMUX_PANE` (or `#{window_id}`) at `SessionStart`, stash it in the
session record, read it back. **Verify `$TMUX_PANE` is inherited into the hook
environment first — if it isn't, the stash is the only path.** This is the
first thing v0 tests.

**State file format.** One file per session, `~/.local/state/kel/<name>.state`.
Single line: `<state> <epoch-seconds>`. Written atomically (write temp in the
same dir, `mv` into place) because the status script and the jump script both
read it. Consumers tolerate a missing or half-written file by treating it as
`unknown`.

**Agent-agnostic design.** State detection is a per-agent adapter. Claude Code
uses hooks. An agent with no hook system falls back to `unknown` and the board
shows the pane's last output line instead. Never make a hook system a
requirement to run an agent.

---

## 10. Commands

`kel` with no arguments opens the board (popup when run from inside `tmux`).
Everything below is the scripting surface underneath it.

```
kel                        open the board
kel new <name>             session in the current repo, inplace
kel new <name> --worktree  session in a fresh worktree on branch <name>
kel go <name>              switch to that window
kel kill <name>            close window, optionally remove worktree
kel ls [--json]            list sessions
kel status-line            render the status-left string
kel jump                   select the next window in `waiting` state
kel doctor                 capability probe
```

`kill` on a worktree session refuses when there are uncommitted or unpushed
changes, and says why. Deleting an agent's only copy of its work is the one
unforgivable bug in this category of tool.

---

## 11. Capability probe

`kel doctor` runs once per machine, caches results in the config dir, and the
rest of the tool branches on them.

| Probe | Why |
|---|---|
| `$TMUX_PANE` / `#{window_id}` reaches hooks | the state adapter depends on it; fall back to the `SessionStart` stash |
| `split-window -c` lands in the right cwd | some `tmux` builds don't honor it; fall back to `cd` as the pane command |
| `display-popup` exists | otherwise the board opens as a window |
| git version supports the worktree flags used | fail early with a clear message |
| `node` is on `PATH` inside the shell `tmux` spawns | Claude Code needs it; catch a broken WSL PATH early |

---

## 12. Stack

- **v0: none.** Shell scripts and `tmux` config. See `rollout.md`.
- **Later: Go + Bubble Tea**, single static binary, if and only if the board
  outgrows a shell script. Single binary, no runtime to install, best TUI
  ecosystem.
- **`tmux`** as the required substrate.
- **Config:** `~/.config/kel/config.toml` — repos root, default agent command,
  editor, keybindings. Per-repo overrides in `.kel/config.toml`.
- **State:** `~/.local/state/kel/`. `tmux` is the source of truth for what is
  alive; these files hold the metadata `tmux` does not know.

Portability rule: nothing machine-specific in the binary or the config schema.
The only per-machine value is the repos root.

---

## 13. On making it public

Reasonable, eventually, with two honest caveats.

The space is crowded and moves fast — there are already terminal agent
multiplexers with five-figure star counts and venture-backed GUI competitors.
Catching on is mostly timing and luck, not merit.

The real risk isn't obscurity, it's the opposite. Users arrive with feature
requests and the tool drifts toward being everyone's tool instead of yours. The
non-goals section is the defense. It's in the README, not buried here, so "can
it wrap the agent and do X" has a public answer that isn't a conversation.

Practical: MIT. README leads with the layout and the jump-to-blocked keystroke,
because that's the whole pitch. No issue templates and no roadmap promises
until it's been used for a month.

Build it for you. If it catches, that's information, not an obligation.

# kelsier — spec

A terminal workspace for running several coding agents at once. `tmux` does the
multiplexing; `kel` does the bookkeeping.

This document is the design and the rationale. `rollout.md` is the build order
and the current status; `usage.md` is the reference for what exists today.

---

## 1. Premise

Running N interactive agent sessions is not hard to *start* and very hard to
*keep track of*. The failure mode is tab sprawl: you lose which terminal holds
which task, which branch it's on, and which one is blocked waiting on you.

Existing tools solve this by owning the agent — a harness, a wrapper, a GUI.
That trades the sprawl problem for a capability ceiling.

`kel` does not wrap anything. `tmux` allocates a PTY and the agent owns it
completely. Claude Code inside a `kel` window is byte-identical to Claude Code
in a bare terminal: same TUI, same keybindings, same MCP, same hooks, same
everything. `kel` only tracks state, renders the fleet, and gets out of the way.

**The bet:** the valuable part is the bookkeeping, not the interface. Anything
that requires reimplementing the agent's interface is out of scope forever.

---

## 2. Non-goals

- **Not a harness.** Never proxies model API calls. Never injects prompts.
  Never manages context.
- **Not a terminal.** Uses whatever emulator the user has.
- **Not a multiplexer.** `tmux` exists and is thirty years old.
- **Not a file manager.** `yazi` exists and is better than anything this project
  would build. It will never be reimplemented here.
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
2. **Hand-edit the code the agents produce.** Editing-by-hand is a smooth part
   of the loop, not a context switch out of it. The agent writes a first pass;
   the user reads it, fixes it, steers. `kel` must never re-point an editor at a
   different directory — which is why panes inside a window are the user's, not
   kel's (§5).

The review surface for (2) is `lazygit` plus the editor, not a bespoke diff
viewer.

---

## 4. Substrate

`tmux` on Linux. On the reference machine that is **WSL2 Ubuntu 24.04** — native
Windows PowerShell has no `tmux`. Setup, toolchain, and WSL2 gotchas are in
`setup.md`.

Everything `kel` is is text: one shell script (`bin/kel`), a `tmux` config
fragment (`tmux/kel.conf`), and an installer (`install.sh`). That is a dotfiles
repo. A second machine is: provision the toolchain (`install/`), `git clone`,
`./install.sh`.

---

## 5. Layout

**Each agent is an ordinary `tmux` window.** `kel` owns which windows exist and
what state each is in; it does **not** own what happens inside a window.

**Pane layout inside a window is entirely the user's, via native `tmux`.** A
window can be a lone agent pane, or `[ agent | nvim ]`, or that plus a
test-runner strip — split with `prefix |` / `prefix -` (or the defaults `%` /
`"`), resized freely. `kel` never splits, resizes, or sends keys to a window's
panes.

**The fleet view is the status line** (§7), backed by `kel status-line`. It
replaces tmux's own window list. `prefix k` is a floating menu of the common
moves.

### 5a. Planned, not built

- **v0.2 — grouping by repo.** One `tmux` session per repo when the flat single
  session overflows. `kel jump` stays global. See `rollout.md`.
- **v0.3 — the board.** `kel board` as a `display-popup` TUI — a cross-group
  navigator with filter / scroll / browse. Transient; never occupies a pane.

### 5b. Rejected: the two-slot / swap-pane architecture

An earlier design used one visible window split into two slots, with every agent
and editor in a hidden holding session swapped into the visible slots via
`swap-pane`. Cut. Recorded so it does not get re-proposed:

- **`swap-pane` is the hardest thing in the design and buys less flexibility
  than native windows.** Native splits give real side-by-side; the two-slot
  layout forbids it.
- **Geometry desync** on every terminal resize; the next swap-in janks.
- **`prefix 1`..`9` collides** with native window switching — a muscle-memory
  tax everywhere else in `tmux`.
- **Three panel modes over one slot** means you can't see the board while
  editing.

Revisit only with a concrete pain the board (5a) cannot address.

---

## 6. Switching

Switching is one keystroke — never open-menu, find-row, press-enter.

| Key | Action |
|---|---|
| `prefix 0`..`9` / `prefix n` / `prefix p` / `prefix w` | native window nav |
| `` prefix ` `` | jump to the next window in `waiting` state — cycles, wraps |
| `prefix k` | floating menu (jump, new, split, scroll, rename, close, detach) |

The `` ` `` binding is the capability terminal tabs cannot offer: not "go to
window 3" but "go to whoever is blocked on me." `kel` binds only `` ` `` and
`k`; native window nav is untouched.

---

## 7. Status line

`tmux`'s own window list has no knowledge of agent state. Replace it:

```
set -g status-left "#($HOME/.local/bin/kel status-line)"
set -g window-status-format ""          # hide tmux's list
set -g window-status-current-format ""
```

```
[kel] 0:auth-fix? [1:rate-limit*] 2:docs!
```

`?` waiting on you, `*` working, `!` done, bare = idle; `[ ]` marks the current
window. `#()` only re-runs on the status interval, so every state hook calls
`tmux refresh-client -S` after writing — the bar updates the moment an agent
changes state.

Past ~6 windows across several repos this line stops fitting. That is the v0.2
grouping trigger (§5a).

---

## 8. Session model

A session is one `tmux` window and one working directory. kel-managed sessions
get a metadata file; hand-made windows (`prefix c`, or the menu's "new agent")
are tracked for state but have no metadata and show as `(unmanaged)`.

`~/.local/state/kel/sessions/<name>.json`:

```
name           slug, the tmux window name
window_id      tmux window id, e.g. @7 — stable; state is keyed by this, not name
repo           the origin repository (may be empty)
cwd            where the agent runs
isolation      inplace | worktree
branch         worktree only
agent          command run in the window (default: claude)
claude_session Claude Code's session id, recorded by the hook — for exact restore
created_at     timestamp
```

(v0.2 adds a `group` field.)

**Isolation is opt-in.** Default `inplace`: the agent edits your actual
checkout. `--worktree` creates a linked worktree on a new branch so two agents
can work one repo without colliding.

**Worktree provisioning.** A fresh worktree has no untracked files. After
creating one, `kel new -w` runs the repo's setup hook if present:

```
.kel/setup      # executable, runs in the new worktree
                # env: $KEL_REPO (origin)  $KEL_WORKTREE (this path)
```

It must only create gitignored paths — anything git would report as a change
blocks `kel kill` (§10). Example in `examples/kel-setup`.

---

## 9. State detection

State comes from the agent's own hook system, not from scraping terminal output.

For Claude Code, five hooks in `~/.claude/settings.json` (wired by `install.sh`,
each calling `kel hook <EVENT>` with the payload on stdin):

| Hook event | State |
|---|---|
| `SessionStart` | `idle` |
| `UserPromptSubmit` | `working` |
| `Notification` | `waiting` |
| `Stop` | `done` |
| `SessionEnd` | state file removed |

Hooks run with no controlling terminal but can run `tmux` commands (the client
talks to the server socket). `kel hook` resolves which window it's in, in order:

1. `$TMUX_PANE` from the hook's environment *(verified present with real Claude
   Code)*
2. the pane stashed by this session's `SessionStart` hook, keyed by session id
3. the single tmux pane whose current path equals the payload's `cwd`

**State file.** `~/.local/state/kel/<window-id>.state`, one line
`<state> <epoch>`, written atomically (temp in the same dir, `mv` into place).
Keyed by window id because `tmux` auto-names every `claude` window `claude` —
name keying would collide. Stale files are pruned on `kel ls` / `kel`.

**Agent-agnostic.** State detection is a per-agent adapter. An agent with no
hook system would fall back to `unknown` and the last output line. Not built —
Claude Code is the only adapter so far.

---

## 10. Commands

```
kel                        enter the workspace (attach the `kel` tmux session)
kel new <name> [-w]        window + agent, inplace or in a git worktree
      [--agent CMD] [--no-agent]
kel kill <name> [-f]       close the window; remove the worktree; -f overrides
                           the uncommitted / unpushed check
kel ls [--json]            state · isolation · branch · dirty · path
kel restore [-c]           rebuild windows after a kill / reboot; -c resumes
                           each conversation (--resume <id> || --continue)
kel prune [-f]             discard dead session records (and their worktrees)
kel doctor                 capability probe, cached to doctor.json
```

Internal (wired into tmux / Claude Code): `kel status-line`, `kel jump`,
`kel cheat`, `kel hook <EVENT>`.

`kel kill` on a worktree refuses when there are uncommitted or unpushed changes,
and shows exactly what's at risk. Deleting an agent's only copy of its work is
the one unforgivable bug in this category of tool.

---

## 11. Capability probe

`kel doctor` prints pass/fail and caches JSON to
`~/.local/state/kel/doctor.json`. Fails (exit 1) only if `tmux`, a modern `tmux`,
or `jq` is missing.

| Probe | Why |
|---|---|
| `tmux` present, `>= 3.0` | the substrate |
| `display-popup` available | the `prefix k` cheatsheet and the future board |
| `git worktree` available | `kel new -w` |
| `jq` present | metadata + the hook merge |
| `node` on `PATH` | Claude Code needs it; catches a broken WSL PATH |
| `claude` on `PATH` | the default agent |
| kel hooks wired in `settings.json` | state detection |
| `allow-passthrough on` | Neovim OSC 52 clipboard through tmux |

---

## 12. Stack

- **Now: shell + `tmux` config.** `bin/kel` is one bash script. No runtime, no
  build.
- **Later (v1.0): Go + Bubble Tea**, single static binary — if and only if the
  board outgrows a shell TUI.
- **`tmux`** as the required substrate.
- **State:** `~/.local/state/kel/` — `tmux` is the source of truth for what's
  alive; these files hold the metadata `tmux` doesn't know.
- **Config (v1.0):** `~/.config/kel/config.toml` + per-repo `.kel/config.toml`.
  Today the only knobs are env vars (`KEL_SESSION`, `KEL_AGENT`,
  `KEL_WORKTREES`).

Portability rule: nothing machine-specific in the script or the schema.

---

## 13. On making it public

Reasonable, eventually, with two honest caveats.

The space is crowded and moves fast — terminal agent multiplexers with
five-figure star counts, venture-backed GUI competitors. Catching on is mostly
timing and luck.

The real risk isn't obscurity, it's the opposite: users arrive with feature
requests and the tool drifts toward being everyone's tool instead of yours. The
non-goals section is the defense, and it's in the README so "can it wrap the
agent and do X" has a public answer that isn't a conversation.

Practical: MIT. README leads with the layout and the jump-to-blocked keystroke.
No issue templates, no roadmap promises, until it's been used for a month.

Build it for you. If it catches, that's information, not an obligation.

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
replaces tmux's own window list. The board (§5b) is the interactive navigator;
`prefix k` is a "new to kel?" menu of the common tmux moves for newcomers.

### 5a. Grouping (v0.2, built)

One `tmux` session per repo — `kel/<group>`, group = repo basename (or
`--group`, or `misc`). Inside a group, agents are windows and native nav is
untouched. Between groups: native `prefix (` / `)`, `prefix g` (group tree),
`kel go <group>`. **`kel jump` is global** — it crosses groups. The status line
shows the current group in full plus `⟨+N waiting⟩` for the rest.

### 5b. The board (v0.3, built; navigator in v0.4)

`kel board` — a `display-popup` running `fzf` over every agent: fuzzy filter, a
preview pane (metadata, recent pane output, git status). `enter` jumps to the
highlighted agent; **`tab`** opens a labelled `tmux display-menu` acting on it
(jump / new agent in its dir / rename / go to its group / kill) — the discoverable
counterpart to the hidden accelerators `ctrl-n` / `ctrl-k` / `ctrl-g` / `ctrl-r`,
which stay bound. Opened by **`Ctrl+Space`** (no prefix) or `prefix b`. Since the
`tab` menu is invoked through fzf `become` rather than a key binding, its
`display-menu` needs an explicit `-c <client>`. The old `display-menu`
quick-jump is retired; `kel menu` is a one-release alias for `kel board`.

### 5c. Rejected: the two-slot / swap-pane architecture

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

Revisit only with a concrete pain the board (§5b) cannot address.

---

## 6. Switching

Switching is one keystroke — never open-menu, find-row, press-enter.

| Key | Action |
|---|---|
| `prefix 0`..`9` / `n` / `p` / `w` | native window nav (within a group) |
| `` prefix ` `` | jump to the next `waiting` agent in **any** group — cycles, wraps |
| `Ctrl+Space` / `prefix b` | the board — find an agent (`enter` jump, `tab` act) |
| `prefix m` | manage the current agent (rename / move / new sibling / kill) |
| `prefix g` / `prefix (` `)` | pick / cycle groups |
| `prefix ,` | rename this window (routes through `kel rename`) |
| `prefix k` | "new to kel?" primer (new, browse, split, scroll, rename, close, detach, show me around) |

The `` ` `` binding is the capability terminal tabs cannot offer: not "go to
window 3" but "go to whoever is blocked on me," and it ignores group
boundaries. `kel` binds `` ` ``, `b`, `m`, `g`, `k`, `,`, `Ctrl+Space` and the
`|`/`-` split keys; native window nav is untouched.

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

## 8. The agent record

An agent is one `tmux` window and one working directory. kel-managed agents
get a metadata record; a hand-made `prefix c` window is tracked for state but
has no metadata and shows as `(unmanaged)` (every "new agent" menu item routes
through `kel new`, so those are managed). Adopt a `prefix c` window with
`kel move`.

`~/.local/state/kel/sessions/<group>/<name>.json`:

```
name           slug, the tmux window name
window_id      tmux window id, e.g. @7 — stable; state is keyed by this, not name
repo           the origin repository (may be empty)
cwd            where the agent runs
isolation      inplace | worktree
branch         worktree only
agent          command run in the window (default: claude)
group          repo basename (or --group, or misc); tmux session is kel/<group>
claude_session Claude Code's session id, recorded by the hook — for exact restore
created_at     timestamp
```

Records are keyed by **group and name**, not name alone — two repos may each
have an agent called `docs`. A bare `kel kill docs` resolves to the group you
are standing in, then to a unique match anywhere; when the name exists in
several groups it lists them and asks for `kel kill <group>/<name>`.

Pre-v0.2 records without `group` get one derived from `repo` on the next
command; records written flat into `sessions/` by v0.4 and earlier are filed
under `sessions/<group>/` on the next command. Their live windows stay in the
old flat `kel` session until recreated.

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

**Dead agents (v0.4).** A SIGKILL / OOM / crash fires neither `Stop` nor
`SessionEnd`, so the state file keeps saying `working`. `kel` catches this at
*read* time, not with a new hook: in `gather_rows` and `kel status-line`, if the
record says `working` / `waiting` but the window's only live process is a bare
shell, the effective state becomes `dead` (bar suffix `x`, red). `kel ls` shows
`dead`; the board still lists it so you can kill or restart it.

### 9a. Context and cost (v0.4.1)

Hook payloads carry no token, context or cost data. Claude Code's **`statusLine`**
does: it pipes a JSON blob to a command on every conversation update, carrying
`context_window.used_percentage`, `cost.total_cost_usd`, `rate_limits.*` and
`prompt_cache.*`. That is a documented, versioned interface — not a scrape of
the transcript — so it satisfies the standing rule that kel never reads an
agent's private files.

`kel statusline` (wired by `install.sh`) records one line per agent window to
`~/.local/state/kel/<window-id>.ctx`:

```
<pct> <cost_usd> <in_tokens> <ctx_size> <rate_5h> <epoch> <model>
```

Reads are cheap and everywhere: the status line shows `·NN%` from
`KEL_CTX_WARN` (default 70) up, `kel ls` gains a `CTX` column, `--json` gains
`context_pct` / `cost_usd`, and the board preview shows context and cost.

Constraints that shape the implementation: updates are debounced to 300 ms and
an in-flight script is **cancelled** when the next one arrives, so the handler
is one `jq`, one atomic write, and a `refresh-client -S` **only** when the
displayed integer changes. Writes are gated on a non-zero `context_window_size`
so a malformed payload cannot blank a good record.

**Agent-agnostic.** State detection is a per-agent adapter. An agent with no
hook system would fall back to `unknown` and the last output line. Not built —
Claude Code is the only adapter so far. Note that context/cost is a *second*
Claude-Code-shaped adapter, and unlike state it has no fallback: another agent
would simply have no `CTX` column.

---

## 10. Commands

```
kel                        dir-aware entry (like `claude`): in a repo, attach
                           that group or start its agent; else the last group
kel new <name> [-w]        window + agent, inplace or in a git worktree
      [--agent CMD] [--no-agent]
kel kill <name> [-f]       close the window; remove the worktree; -f overrides
                           the uncommitted / unpushed check
kel ls [--json]            every agent, grouped by repo
kel go [<group>]           switch to a group (no arg: list them)
kel move [<group>]         relocate the current window to another group
kel rename <newname>       rename the current window, keep the record in sync
kel board                  the fleet browser — filter, preview  (Ctrl+Space or
                           prefix b); enter jumps, tab acts on the highlighted agent
kel restore [-c] [-s]      rebuild the workspace after a kill / reboot — groups,
                           windows, splits, agents; -c resume conversations,
                           -s force the snapshot
kel prune [-f]             discard dead agent records (and their worktrees)
kel doctor                 capability probe, cached to doctor.json
```

Internal (wired into tmux / Claude Code): `kel status-line [group]`,
`kel jump`, `kel snapshot`, `kel cheat`, `kel hook <EVENT>`, and the board
helpers `kel _board_rows` / `_board_preview` / `_board_jump` / `_board_kill` /
`_board_actions` / `_board_rename`. `kel menu` is a one-release alias for
`kel board`.

**Workspace snapshot.** `kel snapshot` (fired by a `client-detached` /
`after-split-window` tmux hook and by state-changing commands) writes the full
shape — groups, windows, pane layouts, per-pane cwd — to `snapshot.json` (the
previous generation is kept as `snapshot.json.prev`). A restore holds a
`.restoring` lockfile so the `after-split-window` / `pane-exited` hooks it
trips can't write the half-rebuilt workspace over the file it is reading. On a
dead server `kel` offers `restore_from_snapshot`: recreate everything, re-split
panes, `select-layout` the saved geometry, resume agents by session id,
re-run allowlisted pane commands.

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
| `display-popup` available | the board and the `prefix k` / `prefix m` menus |
| `fzf` present | `kel board` |
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
  Today the only knobs are env vars: `KEL_SESSION`, `KEL_AGENT`, `KEL_GROUP`,
  `KEL_WORKTREES`, `KEL_RESTORE_CMDS`.

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

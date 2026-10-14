# kelsier — spec

A terminal workspace for running several coding agents at once — and for seeing
what each one is doing, where, and to what, the way an IDE would show you.
`tmux` does the multiplexing; `kel` does the bookkeeping and draws one view
over all of it.

This document is the design and the rationale. `rollout.md` is the build order
and the current status; `usage.md` is the reference for what exists today.

**v0.10 is a pivot.** The design up to v0.9 — one board, three menus, a
dashboard and a cheat sheet over tmux sessions-per-repo — is preserved at the
`v0.9.0` tag (`git show v0.9.0:docs/spec.md`). This document describes where
kel is going, and marks plainly what exists versus what is designed.

---

## 1. Premise

Running N interactive agent sessions is not hard to *start* and very hard to
*keep track of*. The failure mode is tab sprawl: you lose which terminal holds
which task, which branch it's on, and which one is blocked waiting on you.

The author built kel to fix that and then found they did not love using it.
The bookkeeping was right; the interface had grown one surface per problem —
an fzf board, a per-agent menu, a fleet menu, a dashboard, a cheat sheet — each
with its own keys. The fix is not more surfaces. It is one.

The reference point is **Orca** (onorca.dev): a desktop app with a persistent
sidebar of projects → worktrees → agents, each with a state dot and its
branch; the selected agent filling the middle; a file tree and git view on the
right; quota burn along the bottom. That is the shape kel wants, **in the
terminal** — bringing the IDE's crucial views into the terminal rather than
bringing the terminal into an IDE. The terminal is the lightest, leanest and
most compatible place to start. A desktop app may follow (§12c), as a second
reader of the same state.

**The bet is unchanged:** the valuable part is the bookkeeping, not the
*agent's* interface. kel never wraps Claude Code, proxies its API calls,
injects prompts or manages its context — an agent in kel is byte-identical to
one in a bare terminal. kel's *own* interface is exactly what this pivot is
about.

---

## 2. Non-goals

- **Not a harness.** Never proxies model API calls. Never injects prompts.
  Never manages context.
- **Not a terminal.** Uses whatever emulator the user has.
- **Not a multiplexer.** `tmux` exists and is thirty years old.
- **Not a file manager, an editor, or a git client.** `yazi`, `$EDITOR` and
  `lazygit` exist and are better than anything this would build. kel decides
  *where* they open and *which directory* they point at; it never reimplements
  what they show. (Rejected items R4 and R5 in `backlog.md` still hold — this
  is how the IDE views arrive without violating them.)
- **Not a daemon.** State lives on disk and in `tmux`; nothing runs in the
  background but `tmux` itself and, when it is open, the sidebar.
- **Not cloud-anything.** Fully local, no telemetry, no account.
- **Not a team tool.** Single user, their own machines.

---

## 3. What the user actually does with it

The author's own list, in their words where possible:

1. **A higher-level view of every terminal** — not just agents. Which panes
   are Claude, which are a running backend, which are a plain shell.
2. **Where each one is standing** — the repo, whether it is a worktree or the
   main checkout, the branch, whether it is up to date, how many commits are
   unpushed, whether there is uncommitted work.
3. **For each agent:** state, context %, cost, model.
4. **The scaffolding and the files** — a tree, and file contents the way an IDE
   shows them, without leaving the terminal.
5. **Claude's current plan and task list** for a session.
6. **Automations** — scheduled or looping agents, newer than the rest and the
   least settled. Last.

And the loop that has been true since v0: run several agents in parallel, jump
to the one blocked on you in one keystroke, hand-edit what they produce.

---

## 4. Substrate

`tmux` ≥ 3.0 on Linux, WSL2 and macOS. Native Windows has no `tmux`; WSL2 is
the Windows story. Setup and toolchain in `setup.md`.

**Containers are not the answer** to portability: kel drives the *host's* tmux
server — the one you attach to — and spawns agents that edit the host's repos
with the host's `claude` credentials. A container has its own PID and PTY
namespaces, and on macOS it is a Linux VM besides, so its tmux could never be
your tmux. Portability is cross-compilation for Go and POSIX-clean shell for
the bash that remains.

---

## 5. Layout — the workspace  ·  *designed, not built*

```
┌ kel ──────────────┬──────── the window's own panes ──┬ panel (toggle) ┐
│ kelsier           │                                  │ [files] git    │
│ ▸ main      ↑2 ●  │  claude            │ nvim        │  plan  tasks   │
│   ◐ claude  41%   │                    │             │                │
│     opus $1.20    │  (your splits, as  │             │  yazi / lazygit│
│   ▪ npm dev :3000 │   you made them —  │             │  / plan.md     │
│ ▸ wt/fix-auth  ✓  │   kel never moves  │             │                │
│   ● claude WAIT   │   or resizes them) │             │                │
│   $ zsh           │                    │             │                │
│ api-server        │                    │             │                │
├───────────────────┴──────────────────────────────────┴────────────────┤
│ claude 58% 5h · 41% wk                      4 agents · 1 waiting      │
└───────────────────────────────────────────────────────────────────────┘
```

**A sidebar pane in every window** (decided 2026-10-14, after measuring — §5b).
Agents and everything else stay ordinary tmux windows, with whatever splits
you made. Each window carries its own sidebar pane at a fixed width on the
left, all running the same Go program over the same state, so the sidebar
reads as one persistent thing that is simply always there. Selecting a row is
`select-window` (plus `select-pane` for a pane row): **nothing is moved, swapped
or resized.**

- **Sidebar** — a Go TUI (Bubble Tea v2), grown from `kel top`. Repo →
  worktree → *every* pane. Each row: what is running (agent / server / shell,
  from the pane's process), and for the worktree its git position (branch,
  ahead/behind, dirty). Agent rows add state, context %, cost and model. It is
  **the only kel surface**: no board, no menus, no cheat sheet.
- **The window's own panes** — untouched. Agents stay byte-identical; an
  `[agent | nvim]` pair stays a pair.
- **Panel** — files (`yazi`), git (`lazygit`), the session's plan and its task
  list, opened at the selected pane's directory. Off by default; whether it is
  a pane in the window or a `display-popup` is a step-4 decision.
- **Bottom bar** — quota burn (5h / weekly) and fleet counts.
- **Keys** live in the sidebar, about eight of them, all shown in its footer.

**What one sidebar per window costs, and how it is paid:**

- **N processes.** One `kel-top` measured ~10–12 MB and ~2.5% CPU while
  polling. Ten polling instances would be a quarter of a core for nothing, so
  **a sidebar whose window is not active idles** — no polling, no rendering —
  and is woken when its window becomes active (a `session-window-changed` /
  `after-select-window` hook, or control-mode events).
- **Identical everywhere.** Cursor, expanded rows and filter are *view state*
  shared through the state directory, so switching windows shows the same
  sidebar, not a different one. That is also what a later desktop app would
  read (§12c).
- **Every window needs one.** `kel new` creates it; a hook on `after-new-window`
  covers a hand-made `prefix c` window. A sidebar pane must be recognisable
  (a `@kel-sidebar` pane option) so snapshot, restore, `kel ls` and the dead-agent
  check all skip it — it is kel's pane, not the user's.

### 5a. What tmux gives this

- **Control mode** (`tmux -C`) — subscribe to tmux's own events (window added,
  pane exited, window changed) instead of polling.
- **Hooks** (`after-new-window`, `session-window-changed`, `pane-exited`) —
  create a window's sidebar, wake the visible one.
- **`display-popup`** — floating overlays (lazygit, yazi, a plan) dismissed
  with `q`.
- **Per-pane user options** (`@kel-sidebar`, `@kel-kind`) — metadata tmux keeps
  for us, readable in one `list-panes -F`.
- **`#{pane_current_command}` / `#{pane_pid}` / `#{pane_current_path}`** —
  what is running and where, for every pane, in one call.

### 5b. Why not one workspace window with panes swapped in

That was chosen first, on 2026-10-14, and reversed the same day after
measuring. It is recorded so it is not re-proposed — the v0.2 spec had
rejected it too (§5c at the `v0.9.0` tag), and its objections turned out to be
half right.

**Measured** on an isolated tmux server, with a script logging every
`SIGWINCH`, and a real Claude Code 2.1.299:

| test | result |
|---|---|
| `swap-pane` into a slot of the **same** size | 0 resizes in 6 swaps |
| `swap-pane` into a **different** size | 1 resize per swap |
| terminal resized while a pane is hidden | hidden pane keeps its size; resized once when next shown |
| switching windows that each carry a fixed sidebar pane | 0 resizes (after a new window's first display) |
| Claude Code under resize | redraws correctly, but it is **not** an alternate-screen app: each shrink pushed stale lines into tmux scrollback (0 → 3 → 6 lines over four resizes of an idle session) |

**The verdict on v0.2's four objections:**

| objection | verdict |
|---|---|
| no real side-by-side: a swapped pane cannot carry the user's own split | **true, and decisive.** `swap-pane` moves one pane. Keeping `[agent \| nvim]` together needs a nested tmux client in the slot or splits flattened into sidebar rows; both cost more than they buy |
| geometry desync; the next swap-in janks | **solvable** — keep the holding session the slot's size and a swap resizes nothing. Not decisive on its own |
| `prefix 1`..`9` collides with native window switching | moot either way — navigation moves into the sidebar |
| cannot see the board while editing | moot either way — the sidebar is persistent |

**Orca agrees with the rule underneath.** Its PTYs live in a daemon (tmux's
role) and each terminal is an xterm.js view; switching shows a different view
and never moves the process. Its source holds and coalesces resizes and leaves
hidden terminals at their size, because spurious resizes "garble" a
full-screen program's grid (`src/main/ipc/pty/ipc/resize-visibility.ts`,
`src/renderer/src/lib/pane-manager/pane-pty-resize-hold.ts` in
`stablyai/orca`). **Never resize a pane you are not showing, and show a layout
where it already is** — which is exactly what a sidebar per window does.

### 5c. Still open — for the step-4 planning

- **Groups.** Whether sessions-per-repo survive, now that the sidebar groups by
  repo itself. One session for everything would make `select-window` the only
  navigation verb.
- **The panel's form.** A real pane (stays open while you work, costs width in
  every window) or a `display-popup` (no layout cost, modal).
- **Sidebar width** — fixed, or remembered per user; it must be the same in
  every window or switching will look like it jumps.
- **Idle and wake** — hook vs control mode, measured for the cost of each.
- **Restore** — rebuilds windows and splits as today, then adds the sidebar
  pane to each; the `.restoring` lock must cover the hooks that adds.

---

## 6. Keys  ·  *today*

| Key | Action |
|---|---|
| `` prefix ` `` | jump to the next `waiting` agent in **any** group — cycles, wraps |
| `prefix t` | `kel top` — every agent, sorted so the one needing you is first |
| `prefix g` | pick a group (tmux's own session tree) |
| `prefix ,` | rename this window (routes through `kel rename`) |
| `prefix \|` / `prefix -` | split, keeping the current directory |

v0.10 removed `Ctrl+Space` / `prefix b` (the board), `prefix m` (the per-agent
menu) and `prefix k` (`kel cheat`). The sidebar's keys replace all of them and
are not designed yet.

---

## 7. Status line

`tmux`'s own window list has no knowledge of agent state, so kel replaces it:

```
set -g status-left "#($HOME/.local/bin/kel status-line)"
set -g window-status-format ""
set -g window-status-current-format ""
```

```
[api-gw] 1:auth-fix? [2:rate-limit·91%*] 3:docs   ⟨infra·1 web·2⟩
```

`?` waiting on you, `*` working, `!` done, `~` throttled, `x` dead, bare =
idle; `[ ]` marks the current window; `⟨…⟩` names other groups with agents
blocked on you. Every state hook calls `tmux refresh-client -S` after writing,
so the bar changes the moment an agent does — but only when the rendered value
actually changed (invariant 5). In the workspace design this becomes the
bottom bar of §5.

---

## 8. The agent record

An agent is one `tmux` window and one working directory. kel-managed agents
get a metadata record; a hand-made `prefix c` window is tracked for state but
has no record and shows as `(unmanaged)`. (v0.10 removed `kel adopt` and
`kel move`; the sidebar lists every pane, which makes "adopting" one moot.)

`~/.local/state/kel/sessions/<group>/<name>.json`:

```
name           slug, the tmux window name
window_id      tmux window id, e.g. @7 — stable; state is keyed by this, not name
repo           the origin repository (may be empty)
cwd            where the agent runs
isolation      inplace | worktree   (kel ls renders inplace as "repo")
branch         worktree only
agent          command run in the window (default: claude)
group          repo basename (or --group, or misc); tmux session is kel/<group>
claude_session Claude Code's session id, recorded by the hook — for exact restore
compactions    PreCompact count
created_at     timestamp
```

Records are keyed by **group and name** — two repos may each have a `docs`. A
bare `kel kill docs` resolves to the group you are standing in, then to a
unique match anywhere; when ambiguous it lists the candidates and asks for
`kel kill <group>/<name>`. Older record shapes are migrated by `migrate_meta`.

**Isolation is opt-in.** Default `inplace`: the agent edits your checkout.
`-w` creates a linked worktree on a new branch. After creating one, `kel new -w`
runs the repo's `.kel/setup` if present (env `$KEL_REPO`, `$KEL_WORKTREE`). It
must only create gitignored paths — anything git would report as a change
blocks `kel kill`. Example in `examples/kel-setup`.

---

## 9. State detection

State comes from the agent's own hook system, never from scraping its output.
For Claude Code, `install.sh` wires these hooks, each calling `kel hook <EVENT>`
with the payload on stdin:

| Hook event | State |
|---|---|
| `SessionStart` | `idle` |
| `UserPromptSubmit` | `working` |
| `Notification` | by `notification_type` — below |
| `Stop` | `done` |
| `SessionEnd` | state file removed |
| `PreCompact` | no change — increments `compactions` |
| `PostCompact` | no change — redraws the bar |

| `notification_type` | state |
|---|---|
| `permission_prompt`, `agent_needs_input`, `elicitation_*dialog` | `waiting` |
| `agent_completed` | `done` |
| `quota_auto_resume_*` | `throttled` — waiting on quota, not on you |
| `idle_prompt`, `auth_success`, `elicitation_complete/response` | no change |
| anything added later | `waiting` (safe default) |

`notification_text` is kept as the third field of the state file — the literal
permission prompt — so "why is this blocked" needs no pane scrape. `kel jump`
matches `waiting` only.

`kel hook` resolves which window it is in, in order: `$TMUX_PANE`; the pane
stashed by this session's `SessionStart`, keyed by session id; the single pane
whose current path equals the payload's `cwd`.

**State file.** `~/.local/state/kel/<window-id>.state`, one line
`<state> <epoch> <note>`, written atomically. Keyed by window id because tmux
auto-names every `claude` window `claude`.

**Dead agents.** A crash fires neither `Stop` nor `SessionEnd`, so the file
keeps saying whatever it last said. kel catches this at *read* time: if the
record says `idle` / `working` / `waiting` / `throttled` but the window's only
live process is a bare shell, the effective state is `dead`. This check lives
in two places kept in sync by hand — `effective_state()` in `bin/kel` and
`internal/fleet/fleet.go` — until `bin/kel`'s remaining readers move to Go.

### 9a. Context and cost

Hook payloads carry no token, context or cost data. Claude Code's
**`statusLine`** does — a documented, versioned interface, not a transcript
scrape. `kel statusline` records one line per agent window to
`<window-id>.ctx`:

```
<pct> <cost_usd> <in_tokens> <ctx_size> <rate_5h> <epoch> <model>
```

Updates are debounced to 300 ms and an in-flight script is **cancelled** when
the next arrives, so the handler is one `jq`, one atomic write, and a
`refresh-client -S` only when the displayed integer changes. Writes are gated
on a non-zero `context_window_size`.

**`used_percentage` is `null`, not `0`,** before the first API call and right
after `/compact`. The row shows a dim `ctx ?`, and the unmeasured case
declines to overwrite a real figure.

**`agent.name` does not mean "subagent".** It marks a session started with
`claude --agent`. Task-tool subagents report through `subagentStatusLine`; no
per-subagent cost is exposed anywhere.

The row Claude Code shows (repo identity, `250k/1M`, quota resets, `fast` /
`xhigh`) is rendered from the live payload and persisted nowhere — see
`usage.md`.

### 9b. Notifications

When an agent **transitions into** a state listed in `KEL_NOTIFY` (default
`waiting`) and you are **not looking at that window**, kel notifies: `tmux
display-message` always, plus `$KEL_NOTIFY_CMD "<title>" "<body>"` if set.
Hook-driven, never polled — a poller would be a daemon.

### 9c. The fleet document

`kel _fleet [--dirty] [--land] [--pr]` is the whole fleet, computed once, as
one JSON object: `{generated_at, current:{group,window_id}, agents:[…]}`.
`kel ls`, `ls --json`, the bar, the completions and `kel top` all read it.
Adding a field means adding it there once.

Since v0.10 it is produced **only** by the Go `kel-fleet`; the bash reader it
was ported from, and the differential test that held them together, are gone.
A missing `kel-fleet` fails loudly — an empty document would read as "no
agents", which is a guess reported as a fact. The sidebar will need fields the
document lacks today (every pane, its process, git position for every
worktree); they go here, not into the sidebar.

`--dirty` is opt-in because `git status` per worktree is the expensive part
and the bar never shows it.

### 9d. Land state

`land` answers *what would it take to land this branch?* as one code naming
the next action.

| code | means | you |
|---|---|---|
| `dirty N` | uncommitted work | commit it |
| `unpushed N` | commits only you have | push |
| `merged` | its PR landed, or HEAD is in the base | sweep it |
| `review` | PR open | wait, or nudge |
| `behind N` | base moved under you | rebase |
| `no_pr` | pushed, nothing opened | open one |
| `clean` | nothing to say | — |
| `unknown` | `gh` could not answer the PR half | — |

Precedence is that order. `merged` asks **git first** (ancestry); `gh` only
adds the squash-merge case. So the column degrades without a token rather than
going dark. PR state is **one request per repo**, cached under `pr-cache/` for
`KEL_PR_TTL` (default 300s); only `--pr` may refresh it, and a repo whose
worktrees are all dirty or unpushed is never refreshed at all.

---

## 10. Commands  ·  *today*

```
kel                        dir-aware entry (like `claude`): in a repo, attach
                           that group or start its agent; else the last group
kel new <name> [-w]        window + agent, inplace or in a git worktree
      [--group G] [--agent CMD] [--no-agent]
kel kill <name> [-f]       close the window; remove the worktree; -f overrides
                           the uncommitted / unpushed check
kel ls [--json]            every agent, grouped by repo
kel rename <newname>       rename the current window, keep the record in sync
kel top                    every agent, sorted so the one needing you is
                           first (prefix t) — the seed of the sidebar
kel sweep [-n] [-f]        close out every finished agent whose work landed;
                           everything else is reported with its reason
kel relaunch [name] [-f]   relaunch ONE crashed agent in its existing window
kel restore [-c] [-s]      rebuild the workspace after a kill / reboot
kel prune [-f]             discard dead agent records (and their worktrees)
kel doctor                 capability probe, cached to doctor.json
kel config                 edit config.toml, seeded from the example once
kel update                 fast-forward the clone, then re-run install.sh
```

Internal: `kel status-line`, `kel jump`, `kel snapshot`, `kel hook <EVENT>`,
`kel statusline`, `kel _fleet`.

v0.10 removed `board`, `menu`, `cheat`, `go`, `move`, `adopt` and `_usage`.

**Workspace snapshot.** `kel snapshot` (fired by tmux hooks and by
state-changing commands) writes the full shape to `snapshot.json`, keeping one
`.prev`. A restore holds a `.restoring` lockfile so the hooks it trips cannot
overwrite the file it is reading (invariant 2).

**`kel kill` on a worktree refuses** when there are uncommitted or unpushed
changes, and names what is at risk. Deleting an agent's only copy of its work
is the one unforgivable bug in this category of tool (invariant 1).

---

## 11. Capability probe

`kel doctor` prints pass/fail, with a fix for each failure, and caches JSON to
`doctor.json`. Exits 1 if `tmux`, a modern `tmux`, `jq` or `kel-fleet` is
missing.

| Probe | Why |
|---|---|
| `tmux` present, `>= 3.0` | the substrate |
| `display-popup` available | `kel top`, `kel config` |
| `git worktree` available | `kel new -w` |
| `jq` present | metadata, hooks, the installer's settings merge |
| `node` on `PATH` | Claude Code needs it; catches a broken WSL PATH |
| `claude` on `PATH` | the default agent |
| `gh` authenticated | PR state for `kel sweep` and the LAND column |
| kel hooks wired in `settings.json` | state detection |
| kel `statusLine` wired | context % and cost, per agent |
| `kel-fleet` built | the only fleet reader since v0.10 |
| `kel-top` built | `kel top`, and the sidebar after it |
| the wired kel path resolves | `install.sh` records an absolute path into the clone; moving it silences every hook |
| hooks not disabled by managed settings | `allowManagedHooksOnly` / `disableAllHooks` stop hooks while the entries remain |
| `allow-passthrough on` | Neovim OSC 52 clipboard through tmux |
| clone level with its upstream | `kel update` has something to apply |

The upstream probe does **not** fetch. A probe that needs the network is one
people stop running.

### 11a. Updating, and releases

`install.sh` symlinks `bin/kel`, the completions and `tmux/kel.conf` out of the
clone, so the working tree **is** the installed artifact; `kel-fleet` and
`kel-top` are built into `~/.local/bin`. `kel update` fast-forwards the clone
and re-runs `install.sh`. It refuses rather than merges — on a dirty tree, a
detached HEAD, no upstream, or a non-fast-forward.

Until v0.9 this section said releases and tags were "the wrong mechanism":
there was no artifact the symlink did not already point at. **Go being required
changes that.** A machine without a Go toolchain now cannot install kel from a
clone, and prebuilt `kel-fleet` / `kel-top` binaries attached to a tagged
GitHub release are the fix. Versioning policy is in `rollout.md`.

---

## 12. Stack

- **Go** for everything that reads or renders: `kel-fleet` (the fleet
  document), `kel-top` (becoming the sidebar). Bubble Tea **v2**
  (`charm.land/bubbletea/v2`) — almost every tutorial online is v1 and will
  not compile.
- **Bash** for the hooks and the installer, permanently: a hook must start in
  milliseconds with nothing to build, and the installer runs before anything
  is built.
- **`bin/kel` is transitional.** Its mutating commands (`new`, `kill`,
  `restore`, `sweep`, …) are still bash and move to Go as the sidebar needs
  them — `kill`'s refusal logic last, with its tests ported first.
- **State:** `~/.local/state/kel/`, mode `0700`. tmux is the source of truth
  for what is alive; the files hold what tmux does not know. The format is the
  interface, not the language — see *The on-disk contract* in `CLAUDE.md`.
- **Config:** `~/.config/kel/config.toml`, overridden by a per-repo
  `.kel/config.toml`, overridden by an exported `KEL_*` env var. A flat
  `key = value` subset of TOML; `examples/config.toml` lists every key.

### 12b. What kel keeps, and what leaves the machine

**kel makes exactly one network call**: `gh api repos/<slug>/pulls`, for the
two land codes local git cannot answer. Three things narrow it:

1. no slug unless `origin` is on `github.com`;
2. local git answers first — a repo whose worktrees are dirty or unpushed is
   never asked about (enforced in `kel-fleet` since v0.10; before that only
   the bash reader kept this promise);
3. `pr = "off"` disables it outright (likewise only honoured by bash until
   v0.10).

Nothing else leaves the machine. kel does not wrap Claude Code, so your code
reaches Anthropic by exactly the path it would from a bare terminal.

**What it keeps** is in the `0700` state directory. The sensitive part is
`<window-id>.state`, which stores Claude Code's notification text — the agent
quoting a command back at you. **Agent output is read but never stored**: `kel
top` shows the last line of each pane and drops it. `$KEL_NOTIFY_CMD` is the
one place untrusted text crosses a boundary, so `setup.md`'s recipes escape it.

### 12c. A desktop app, later

Agreed direction, not scheduled: the terminal version stays a thin reader of
the state directory and the fleet document, so a GUI (the Orca shape, as an
app) can be a second reader of the same files without kel growing a server.
Nothing in the terminal work may make that harder — in practice: new data goes
into the fleet document, not into the sidebar's private state.

---

## 13. On making it public

Reasonable, eventually. The space is crowded and moves fast. The real risk is
not obscurity but drift — users arrive with requests and the tool becomes
everyone's instead of yours. The non-goals are the defence, and they are in
the README. MIT. Build it for you; if it catches, that is information, not an
obligation.

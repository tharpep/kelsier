# kel — usage

`kel` is one command. `tmux` does the multiplexing underneath; you rarely type
`tmux` directly any more.

## Install

```bash
cd ~/code/kelsier && ./install.sh
tmux kill-server        # then reopen; restart any running Claude sessions
```

`install.sh` symlinks `bin/kel` to `~/.local/bin`, makes `~/.tmux.conf` source
`tmux/kel.conf`, and merges the 5 state hooks into `~/.claude/settings.json`
(non-clobbering, idempotent — a `.kel-bak.<epoch>` copy is kept each run).

## Commands

```
kel                    enter the workspace  (attach the `kel` tmux session)
kel new <name>         new window + agent in the current repo   (isolation: inplace)
kel new <name> -w      ...in a fresh git worktree on a new branch <name>
kel new <name> --no-agent      just make the window, don't start the agent
kel new <name> --agent CMD     run CMD instead of `claude`
kel kill <name>        close the window; remove the worktree if it was one
kel kill <name> -f     ...even with uncommitted / unpushed work
kel ls [--json]        list sessions: state · isolation · branch · dirty · path
kel restore [-c]       rebuild windows after a kill / reboot (-c resumes agents)
kel prune [-f]         discard dead session records (and their worktrees)
kel doctor             probe the machine, cache to ~/.local/state/kel/doctor.json
```

Internal (wired into tmux / Claude Code, you won't call these):
`kel status-line`, `kel jump`, `kel cheat`, `kel hook <EVENT>`.

## Detach vs kill

`prefix d` **detaches** — the session and every agent keep running; `kel`
reattaches you exactly where you were. This is the "close the terminal, come
back tomorrow" path, and it round-trips perfectly (agents survive a full tmux
detach).

**Killing** the session (`prefix &` on the last window, `tmux kill-session`, a
reboot) ends the agent processes. `kel ls` then shows those sessions as `dead`
with their dirs and branches intact; `kel restore` rebuilds the windows, and
`kel restore -c` also runs `<agent> --continue` in each so Claude Code resumes
the prior conversation. `kel new <name>` reclaims a `dead` record; `kel kill <name>` discards one, or
`kel prune` clears them all at once (both honour the worktree-safety check;
`kel prune -f` overrides). `kel restore -c` runs `<agent> --continue || <agent>`,
so a session with no prior conversation still comes back with a fresh agent.

## The model

- **One tmux session, `kel`.** Every agent is a window in it. `kel new` adds a
  window there from anywhere — a plain shell or inside tmux.
- **Window name = session name.** State is keyed by window *id* underneath, so
  two windows can even share a name, but name them (`kel new` does, or
  `prefix ,`) so the status bar reads.
- **`kel-managed` vs unmanaged.** Windows you make by hand (`prefix c`) still get
  state tracking and show in `kel ls` as `(unmanaged)` — they just have no
  metadata, so `kel kill` on them only closes the window.

## Isolation

`inplace` (default) — the agent edits your actual checkout. Right for most
tasks.

`--worktree` — a linked git worktree on a new branch, so two agents can work the
same repo without colliding. After creating it, kel runs the repo's
**`.kel/setup`** if present:

```
.kel/setup      # executable, runs in the new worktree
                # env: $KEL_REPO (origin repo)  $KEL_WORKTREE (this path)
```

Use it to install deps / copy `.env` / warm caches. **It must only create
gitignored paths** — anything it leaves that git would report as a change makes
`kel kill` refuse to remove the worktree (which is the point: kel never deletes
an agent's only copy of real work — commit & push, or `--force`).

Example `.kel/setup` for a Node project:

```bash
#!/usr/bin/env bash
set -e
cp "$KEL_REPO/.env" .env 2>/dev/null || true
npm ci
```

## Keys

`prefix` is `Ctrl+b`. Full list: `prefix k` (or `kel cheat`).

| | |
|---|---|
| `prefix 0..9` / `n` / `p` / `w` | move between agents |
| `` prefix ` `` | jump to the next agent **waiting** on you |
| `prefix c` | blank window · `prefix ,` rename · `prefix &` close |
| `prefix [` | scroll an agent's output (`q` to leave) |
| `prefix %` `"` | split a window (agent + editor) · arrows move · `z` zoom |
| `prefix d` | detach — agents keep running · `kel` to return |

## State

`~/.local/state/kel/`
- `<window-id>.state` — `<state> <epoch>`, written by the hooks
- `sessions/<name>.json` — metadata for kel-managed sessions
- `.stash/<claude-session-id>` — pane id from SessionStart, for hooks that don't
  inherit `$TMUX_PANE`
- `doctor.json` — last `kel doctor` result

Events → states: `UserPromptSubmit`→working, `Notification`→waiting,
`Stop`→done, `SessionStart`→idle, `SessionEnd`→cleared.

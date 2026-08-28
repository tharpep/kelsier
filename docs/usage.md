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
kel                    enter the workspace (attach the last group you used)
kel new <name>         new window + agent in the current repo   (inplace)
kel new <name> -w      ...in a fresh git worktree on a new branch <name>
kel new <name> --group G       put it in group G instead of the repo's
kel new <name> --no-agent      just make the window, don't start the agent
kel new <name> --agent CMD     run CMD instead of `claude`
kel kill <name>        close the window; remove the worktree if it was one
kel kill <name> -f     ...even with uncommitted / unpushed work
kel ls [--json]        list every agent, grouped by repo
kel go [<group>]       switch to a group  (no arg: list the groups)
kel menu               floating list of every agent — press a key to jump
kel restore [-c] [-s]  rebuild the workspace after a kill / reboot
                       (-c resume conversations; -s force the snapshot)
kel prune [-f]         discard dead session records (and their worktrees)
kel doctor             probe the machine, cache to ~/.local/state/kel/doctor.json
```

Internal (wired into tmux / Claude Code, you won't call these):
`kel status-line [group]`, `kel jump`, `kel cheat`, `kel hook <EVENT>`.

Env knobs: `KEL_SESSION` (session-name prefix, default `kel`), `KEL_AGENT`
(default `claude`), `KEL_WORKTREES` (default `<repo>/../.kel-worktrees`).

## The model — groups

**One tmux session per repo.** `kel new` reads where you ran it: `~/code/api`
→ group `api` → tmux session `kel/api`. `--group G` overrides;
non-repo agents fall into `misc`.

- Inside a group, agents are windows — native `prefix 0-9` / `n` / `p` / `w`.
- Between groups: native `prefix (` / `)` cycle, `prefix G` session tree, or
  `kel go <group>`.
- `` prefix ` `` (jump to next **waiting** agent) is **global** — it crosses
  group boundaries. That's the point of it: go to whoever's blocked, not to a
  specific group.
- `kel` (bare, from a shell) drops you back in the last group you used.

**Status line.** The group you're in, in full, plus a badge for the rest:

```
[api] 0:auth-fix? [1:rate-limit*] 2:docs   ⟨+2 waiting⟩
```

`?` waiting on you · `*` working · `!` done · bare = idle · `[ ]` = current
window · `⟨+N waiting⟩` = agents blocked in other groups (hit `` ` `` to reach
them). In a non-kel tmux session the bar shows a compact `[kel] N agents · M
waiting`.

**Managed vs unmanaged.** `kel new` writes a metadata record. Windows you make
by hand (`prefix c`, or the menus' "new agent here") are still state-tracked
and show in `kel ls` as `(unmanaged)` — `kel kill` on them just closes the
window. State is keyed by window id, so two windows may even share a name.

## Detach vs kill

**`prefix d` detaches** — the session and every agent keep running; `kel`
reattaches you. Round-trips perfectly; agents survive a full tmux detach.

**Killing** a session (`prefix &` on its last window, `tmux kill-session`)
ends its agent processes. `kel ls` then shows it as `dead`; `kel new` reclaims
the record, `kel kill` / `kel prune` discard it.

**A reboot / crash** takes down everything. kel snapshots the whole workspace —
every group, window, pane layout, and per-pane directory — on detach and
whenever the shape changes (`~/.local/state/kel/snapshot.json`). The next time
you run `kel` it offers to rebuild it:

```
kel rebuild your workspace? 3 group(s), 9 window(s)  [Y/n]
```

Yes → every group, window, and split comes back in place, agents resumed
(`--resume <session-id>`), your editor / lazygit panes re-launched. `kel
restore` does the same non-interactively; `kel restore -s` forces the snapshot
even if some sessions are still live.

**`prefix D`** is the deliberate "leave kel" — detaches *and* snapshots. Plain
`prefix d` also detaches (agents keep running either way); `kel` brings you back
to your last group.

## Isolation

`inplace` (default) — the agent edits your actual checkout. Right for most
tasks.

`--worktree` — a linked git worktree on a new branch, so two agents can work the
same repo without colliding. kel then runs the repo's **`.kel/setup`** if
present:

```
.kel/setup      # executable, runs in the new worktree
                # env: $KEL_REPO (origin repo)  $KEL_WORKTREE (this path)
```

Use it to install deps / copy `.env` / warm caches. **It must only create
gitignored paths** — anything it leaves that git would report as a change makes
`kel kill` refuse to remove the worktree (the point: kel never deletes an
agent's only copy of real work — commit & push, or `-f`). Full example in
`examples/kel-setup`.

## Keys

`kel.conf` also turns on the mouse (click a pane to focus, drag borders to
resize, wheel to scroll — text selection then needs Shift+drag), counts windows
from 1, and labels each pane. **window = a tab, one per agent; pane = a split
inside a window; group = one tmux session per repo.**

`prefix` is `Ctrl+b`. `prefix k` = a floating menu of the common moves;
`prefix k` → `?` (or `kel cheat`) shows the full reference.

| | |
|---|---|
| `prefix 0..9` / `n` / `p` / `w` | move between agents in the group |
| `` prefix ` `` | jump to the next agent **waiting** on you — any group |
| `prefix m` | fleet menu — every agent, every group, press a key to jump |
| `prefix G` / `prefix (` `)` | pick / cycle groups |
| `prefix k` | command menu (new · jump · split · scroll · rename · close · detach) |
| `prefix [` | tmux scroll mode for a shell pane (`q` to leave) |
| mouse wheel / PgUp PgDn | scroll an agent's conversation (kel maps the wheel to PageUp/Down) |
| `prefix \|` / `-` | split a window (agent + editor); arrows or click to focus; `z` zoom |
| `prefix d` | detach — agents keep running; `kel` to return |

## State files

`~/.local/state/kel/`
- `<window-id>.state` — `<state> <epoch>`, written by the hooks, pruned when the
  window is gone
- `sessions/<name>.json` — metadata for kel-managed sessions (incl. `group`)
- `last-group` — the group `kel` bare reattaches to
- `.stash/<claude-session-id>` — pane id from SessionStart, for hooks that don't
  inherit `$TMUX_PANE`
- `doctor.json` — last `kel doctor` result

Events → states: `SessionStart`→idle, `UserPromptSubmit`→working,
`Notification`→waiting, `Stop`→done, `SessionEnd`→cleared.

## Known limits

- **Two agents side by side in one window.** It works — both run fine — but
  kel's state tracking is per *window*, so the status bar shows one state for
  the pair and `kel jump` treats them as one. `kel ls` shows
  `auth (claude|claude)` so at least it's visible. One agent per window is the
  model; genuine first-class side-by-side agents would need state keyed by pane.
- **Restored panes run a fresh shell** unless the command was an agent or on
  the `KEL_RESTORE_CMDS` allowlist (nvim, lazygit, htop, …). Set
  `KEL_RESTORE_CMDS` to extend it.

## Migrating from the flat session (pre-v0.2)

The old single `kel` session keeps working. Its records get a `group` field
(from the repo) on the next `kel` command; the live windows stay put until you
kill / recreate them, and new `kel new` calls create per-repo sessions. The old
`kel` group drains on its own over a few days.

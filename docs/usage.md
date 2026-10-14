# kel — usage

The reference for what exists **today**. kel is mid-pivot (v0.10, see
`rollout.md`): the old board and menus are gone and the sidebar that replaces
them is not built yet, so for now kel is the commands below, the status bar,
`kel top` and jump-to-blocked.

## Install

```bash
cd ~/code/kelsier && ./install.sh
tmux source-file ~/.tmux.conf     # pick up kel.conf changes
# first install only: restart running Claude sessions so they load the hooks
```

Needs `tmux` ≥ 3.0, `jq`, `git` and **Go** — `install.sh` builds `kel-fleet`
and `kel-top` and fails if it cannot. It symlinks `bin/kel` into
`~/.local/bin`, makes `~/.tmux.conf` source `tmux/kel.conf`, and merges the
state hooks and the `statusLine` into `~/.claude/settings.json`, keeping the
newest three backups of each file it edits.

**`./install.sh --uninstall` reverses all of it**, restoring any `statusLine`
kel displaced, and leaves `~/.local/state/kel` alone — uninstalling must not be
able to lose work. `--uninstall --purge` removes that too, after asking.

**Upgrading from v0.9:** a running tmux server keeps the old bindings
(`Ctrl+Space`, `prefix b`, `prefix m`, `prefix k`) until it restarts —
`source-file` adds bindings, it does not remove them. `tmux kill-server` (after
`kel snapshot`) or unbind them by hand. Per-repo sessions from v0.9
(`kel/<group>`) keep working until their windows close; new windows go into
the one `kel` session, and `kel restore` brings a v0.9 snapshot back into it.

## Commands

```
kel                    in a repo: go to (or start) that repo's agent, the way
                       `claude` keys off the directory. Never asks anything:
                       with no workspace running it starts fresh and, if there
                       was one before, says `kel restore` brings it back
kel new <name>         new window + agent in the current repo   (inplace)
kel new <name> -w      ...in a fresh git worktree on a new branch <name>
kel new <name> --group G       put it in group G instead of the repo's
kel new <name> --no-agent      just make the window, don't start the agent
kel new <name> --agent CMD     run CMD instead of `claude`
kel kill <name>        close the window; remove the worktree if it was one
kel kill <name> -f     ...even with uncommitted / unpushed work
kel ls [--json]        every agent, grouped by repo (state, context %, cost);
                       --json is the fleet document {generated_at, current, agents}
kel rename <newname>   rename THIS window and keep its record in sync
kel top                every agent, sorted so the one needing you is first —
                       state, time in state, LAND, context, cost, last output.
                       j/k scroll, s sort, / filter, q quit   (prefix t)
kel sweep [-n] [-f]    close out every finished agent whose branch has landed;
                       anything dirty, unpushed or unmerged is reported, never
                       touched. -n shows the plan first
kel relaunch [name]    relaunch ONE crashed agent in its existing window
kel restore [<n>]      bring back a previous workspace — windows, splits,
                       directories, conversations resumed (--no-resume: don't)
kel restore --list     the last five saved workspaces, newest first
kel prune [-f]         discard dead agent records (and their worktrees)
kel doctor             check the machine; every failure prints its fix
kel config             edit config.toml (seeded from the example, once)
kel update             fast-forward the clone kel runs from, then re-run
                       install.sh. Refuses on a dirty tree, detached HEAD,
                       no upstream, or a non-fast-forward
```

Internal (wired into tmux / Claude Code): `kel status-line`, `kel jump`,
`kel snapshot`, `kel hook <EVENT>`, `kel statusline`, `kel _fleet`.

## Keys

`prefix` is `Ctrl+b`.

| | |
|---|---|
| `` prefix ` `` | jump to the next agent **waiting** on you — any group |
| `prefix t` | `kel top` |
| `prefix 1..9` / `n` / `p` / `w` | move between windows — every agent is in one session (native tmux) |
| `prefix ,` | rename this window (routes through `kel rename`) |
| `prefix \|` / `-` | split, keeping the current directory |
| `prefix [` | scroll mode for a shell pane (`q` to leave) |
| mouse wheel | scroll an agent's conversation (mapped to PageUp/Down) |
| `prefix d` | detach — agents keep running; `kel` to return |

`kel.conf` also turns on the mouse (to select text, hold Shift while you drag),
counts windows from 1, and labels each pane.

## Groups

**Every window lives in one tmux session, `kel`**; a group is a label, not a
session. By default it is the repo: `kel new` reads `$PWD` at that moment and
uses the git root's basename. `--group G` or `$KEL_GROUP` override it; a
`.kel/group` file pins a directory and everything under it (`$KEL_GROUP` still
wins). Non-repo agents go to `misc`. A window you make yourself (`prefix c`)
gets the group of the repo its pane is standing in.

The group is what `kel ls` sorts by, what `kel kill <group>/<name>` names, and
— from M3 — what the sidebar groups rows under.

**window vs pane:** `kel new` makes a **window** — one agent. `prefix | / -`
makes a **pane** inside it, for your own editor, shell or tests. kel tracks
state per window, so a second agent belongs in its own window.

## The status bar

```
[kel] 1:auth-fix? [2:rate-limit·91%*] 3:docs 4:sazed
```

`?` waiting on you · `*` working · `!` done · `~` throttled (waiting on quota,
resumes itself) · `x` dead (the process exited without a clean stop — `kel
relaunch` or `kel kill` it) · bare = idle · `[ ]` current window · `·91%`
context, shown from `KEL_CTX_WARN` (default 70) up. If `kel-fleet` is missing
the bar says so in red rather than going blank. This is an interim bar: M4
turns it into the workspace footer (`spec.md` §7b).

## Detach, kill, reboot

**`prefix d` detaches** and snapshots; every agent keeps running and `kel`
brings you back. **Killing** the session ends its agents; `kel ls` then shows
them `dead`.

**After a reboot, `kel` just opens** — it never asks. If a workspace was
running before, it says so in one line:

```
kel previous workspace: 9 window(s) · kel restore
```

and keeps that workspace safe in `snapshots/` (the newest five are kept), so
starting fresh cannot overwrite it. **`kel restore`** brings it back — every
window, split and directory, with conversations resumed. **`kel restore
--list`** shows the saved workspaces and **`kel restore 2`** picks an older
one. Restoring into a running workspace only adds what is missing.

## Isolation

`inplace` (default, shown as **`repo`**) — the agent edits your checkout.
`-w` — a linked worktree on a new branch, so two agents can work one repo. kel
then runs the repo's **`.kel/setup`** if present (env `$KEL_REPO`,
`$KEL_WORKTREE`), which must only create gitignored paths: anything git would
report as a change makes `kel kill` refuse. Example in `examples/kel-setup`.

`kel kill` and `kel prune` also clean up git's own worktree record when the
directory was deleted by hand.

## Settings

`~/.config/kel/config.toml`, overridden by a per-repo `.kel/config.toml`,
overridden by an exported `KEL_*` env var. `kel config` opens it, seeded from
`examples/config.toml`, which lists every key with its default. The ones you
are likely to want: `notify` (states worth interrupting you for), `notify_cmd`
(a desktop-notification command — `setup.md` has recipes), `ctx_warn`, `pr =
"off"` (no network call at all), `worktrees`, `restore_cmds`.

## What kel writes, and what leaves the machine

Everything lives in `${XDG_STATE_HOME:-~/.local/state}/kel/`, mode `0700`:

- `sessions/<group>/<name>.json` — one record per managed agent
- `<window-id>.state` — state from the hooks, including Claude Code's
  notification text (the agent quoting a command back at you)
- `<window-id>.ctx` — context, cost and model from `kel statusline`
- `snapshot.json` (+ `.prev`), `snapshots/` (previous workspaces),
  `doctor.json`, `pr-cache/`,
  `.stash/`

Field layouts are in `CLAUDE.md` under *The on-disk contract*.

**One network call**, `gh api repos/<slug>/pulls`, and only from `kel sweep`
(the `--pr` path). Never for a repo whose `origin` is not on `github.com`,
never for one whose work is still uncommitted or unpushed, and never with `pr =
"off"`. Nothing else leaves the machine; kel does not wrap Claude Code.

## Claude Code's own row

`install.sh` wires `kel statusline` as Claude Code's `statusLine`, so this row
appears in **every** Claude Code session on the machine:

```
tharpep/kelsier@auth-fix · opus-5[1m] fast xhigh · ███░░░░░░░ 25% ctx · $31.90 · 5h 74% (39m)
```

| segment | appears when |
|---|---|
| `owner/name@worktree` | outside a kel window, and the payload names a repo |
| `fast` `xhigh` `no-think` | those modes are not at their defaults |
| `250k/1M` after the bar | context is at `KEL_CTX_WARN` or above |
| `$N.NN` | cost is non-zero |
| `5h` / `7d` / `spend` + reset | that quota is at 50% or above |
| an agent name | the session was started with `claude --agent` |

`ctx ?` means not measured yet (before the first reply, and right after
`/compact`) — not `0%`. Subagent cost is not in this row; no payload exposes it.

## Known limits

- **Two agents in one window** run fine, but state is per window, so the bar
  shows one state for the pair.
- **Restored panes run a fresh shell** unless the command was an agent or on
  `restore_cmds` (nvim, lazygit, htop, …).

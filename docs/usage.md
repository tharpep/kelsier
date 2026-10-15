# kel — usage

The reference for what exists **today**. kel is mid-pivot (v0.10, see
`rollout.md`): the old board and menus are gone, replaced by the sidebar; kel
is the sidebar, the commands below, the status bar, `kel top` and
jump-to-blocked.

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
kel sidebar on|off     the sidebar pane in every window (on by default; off is
                       remembered until you turn it back on)
kel url [dir|agent]    a worktree's dev-server URLs, one per line; exit 1 for none
                       (for scripts — a Playwright skill can ask instead of guessing)
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

## The sidebar

Every kel window has a 32-column pane on its left — the same in every window —
in two sections:

- **`spaces`** — where things are: one entry per repo checkout or worktree,
  with its branch, `↑` unpushed / `↓` behind, `●` uncommitted files and the
  ports its servers listen on. The space you are in is open, listing its
  servers, shells and programs; the others are folded.
- **`agents`** — who needs you, most urgent first: a dot per state (yellow
  waiting, cyan working, green finished-and-not-yet-looked-at, dim idle or
  seen, magenta throttled, red `✕` dead, dim `?` unknown), then how long, the
  context % and the cost.

A blue `▌` marks the selection, `›` where you are. The footer reads `prefix
space` until the sidebar has the keyboard, then shows the keys for whatever is
selected (`e edit · p plan · x kill · ?` on an agent) — `?` lists them all.

| key | |
|---|---|
| `prefix Space` | put the keyboard in the sidebar (replaces tmux's `next-layout` on that key) |
| `j` / `k` | move |
| `enter` | jump to an agent or pane · open or fold a space |
| `space` / `l` / `h` | fold, open, close a space |
| `/` | filter by name, live · `esc` clears |
| `n` / `w` | new agent / new worktree agent in the selected entry's space — type the name in the footer |
| `r` | rename the selected agent |
| `x` | kill the selected agent, after `y` — a worktree with unsaved work is refused, and the footer names what is at risk |
| `f` / `g` | yazi / lazygit in a popup, at the selected entry's worktree |
| `d` | that worktree's changes — committed and uncommitted — against the branch it lands on (diffview.nvim if installed, else `delta`, else `git diff`) |
| `e` | the file the selected agent last edited, at the line — in the nvim you have open in that worktree, else a popup |
| `o` | the selected server in your browser — its lowest port; the footer names the others |
| `p` | the selected agent's plan (from plan mode), then its task list — `✓` done, `▸` in progress, `·` pending, and what blocks what. Most sessions on newer models keep no task list; it says so |
| `?` | every key |
| `esc` | back to the pane you were in |
| click · wheel | select and jump, fold a space · move |

**`prefix q` closes any popup**, whatever is running in it — even vim that
yazi opened for you. It says so in every popup's title. Quitting the tool
itself closes it too (`q` in yazi and lazygit, where `esc` also works, `:q` in
nvim). yazi and lazygit keep running when closed with `prefix q`, so
reopening them lands where you left off; the diff, plan and editor popups are
made fresh each time. Outside a popup `prefix q` is tmux's usual
`display-panes`. A tool that is not installed is said in
the footer (`kel doctor` lists them as optional). The browser is `$BROWSER`,
else `open` / `xdg-open` / `wslview`, else Windows' `start` from WSL.

A finished agent stays green until its window has been shown; after that it
dims. Hidden sidebars do no work; the one you can see refreshes every two
seconds and at once when an agent changes state. `kel sidebar off` removes it
everywhere (and new windows get none) until `kel sidebar on`; `sidebar =
"off"` in config disables it outright; `sidebar_width` sets the width and
`sidebar_style` its background (black by default; `"default"` for the
terminal's own).

## Groups

**Every window lives in one tmux session, `kel`**; a group is a label, not a
session. By default it is the repo: `kel new` reads `$PWD` at that moment and
uses the git root's basename. `--group G` or `$KEL_GROUP` override it; a
`.kel/group` file pins a directory and everything under it (`$KEL_GROUP` still
wins). Non-repo agents go to `misc`. A window you make yourself (`prefix c`)
gets the group of the repo its pane is standing in.

The group is what `kel ls` sorts by, what `kel kill <group>/<name>` names, and
the label each of the sidebar's spaces carries.

**window vs pane:** `kel new` makes a **window** — one agent. `prefix | / -`
makes a **pane** inside it, for your own editor, shell or tests. kel tracks
state per window, so a second agent belongs in its own window.

## The status bar

```
claude ███░░ 58% (1h20m) · wk 41%  │  1 waiting 6m · $24.10
```

Only what belongs to no single pane — everything per pane is in the sidebar:

| segment | |
|---|---|
| `claude ███░░ 58% (1h20m)` | the 5-hour quota and when it resets; `░░░░░ ?` once it has reset since it was last seen, never the old figure |
| `wk 41%` | the weekly quota (`wk ?` when stale) |
| `spend 61%` | the spend limit, from 50% only |
| `1 waiting 6m` | agents blocked on you, and the longest wait; `nothing waiting` when none are |
| `$24.10` | the cost of the agents open right now |
| `kelsier · fix-auth ↑2 ●3` | only while the sidebar is off: where the pane you are in stands |

No quota shows until a session has reported one — an API-key session never
does, and the bar never invents a `0%`. If `kel-fleet` is missing the bar says
so in red rather than going blank.

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
one. Restoring into a running workspace only adds what is missing, and closes
the empty shell a fresh `kel` opened if you never used it.

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
- `quota` — the account's rate limits, from `kel statusline`
- `<window-id>.seen`, `sidebar.json`, `sidebar-cache.json` — the sidebar's
- `<window-id>.edit` — the file the agent last edited, and the line, from a
  `PostToolUse` hook on Edit / Write; `<window-id>.plan` — the plan file a
  plan-mode Write named; `<window-id>.view` — what `p` last showed
- kel *reads*, never writes, Claude Code's own `~/.claude/tasks/<session>/`
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
appears in **every** Claude Code session on the machine. Inside a kel window
it is the session only — where it stands is the sidebar's, the quota the
bar's:

```
Opus 5.5 fast xhigh · ███░░░░░░░ 25% ctx · $31.90
```

Outside kel, where there is no sidebar or bar, it also carries those:

```
tharpep/kelsier@auth-fix · Opus 5.5 · ███░░░░░░░ 25% ctx · $31.90 · 5h 74% (39m)
```

| segment | appears when |
|---|---|
| `owner/name@worktree` | outside a kel window, and the payload names a repo |
| `fast` `xhigh` `no-think` | those modes are not at their defaults |
| `250k/1M` after the bar | context is at `KEL_CTX_WARN` or above |
| `$N.NN` | cost is non-zero |
| `5h` / `7d` / `spend` + reset | outside a kel window, and that quota is at 50% or above |
| an agent name | the session was started with `claude --agent` |

`ctx ?` means not measured yet (before the first reply, and right after
`/compact`) — not `0%`. Subagent cost is not in this row; no payload exposes it.

## Known limits

- **Two agents in one window** run fine, but state is per window, so the bar
  shows one state for the pair.
- **Restored panes run a fresh shell** unless the command was an agent or on
  `restore_cmds` (nvim, lazygit, htop, …).

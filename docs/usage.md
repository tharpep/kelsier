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
(`Ctrl+Space`, `Ctrl-b b`, `Ctrl-b m`, `Ctrl-b k`) until it restarts —
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
kel shot [agent]       the pane (yours, or the agent's) as an SVG in ~/Pictures/kel, with freeze
kel new <name> <dir>   ...in <dir> instead — no cd first; `kel new <dir>` names it after the folder
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
kel shell [dir]        a plain shell window there — no agent, no record
kel quit [-f]          save the workspace, then close it all; refuses while a
                       worktree holds unsaved work (-f overrides). `kel`, then
                       `kel restore`, brings it back
kel url [dir|agent]    a worktree's dev-server URLs, one per line; exit 1 for none
                       (for scripts — a Playwright skill can ask instead of guessing)
kel top                every agent, sorted so the one needing you is first —
                       state, time in state, LAND, context, cost, last output.
                       j/k scroll, s sort, / filter, q quit   (Ctrl-b t)
kel sweep [-n] [-f]    close out every finished agent whose branch has landed,
                       and every landed worktree no window or Claude session is
                       in (with its branch, if git agrees it merged); a gone
                       one is pruned. Dirty, unpushed or unmerged is never
                       touched. -n shows the plan first
kel sweep <path>       the same, for one worktree — says why when it won't
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

Keys below use `Ctrl-b`, tmux's default prefix; if you changed yours, kel's own hints use your key.

| | |
|---|---|
| `` Ctrl-b ` `` | jump to the next agent **waiting** on you — any group |
| `Ctrl-b t` | `kel top` |
| `Ctrl-b 1..9` / `n` / `p` / `w` | move between windows — every agent is in one session (native tmux) |
| `Ctrl-b ,` | rename this window (routes through `kel rename`) |
| `Ctrl-b \|` / `-` | split, keeping the current directory |
| `Ctrl-b [` | scroll mode for a shell pane (`q` to leave) |
| mouse wheel | scroll an agent's conversation (mapped to PageUp/Down) |
| `Ctrl-b d` | detach — agents keep running; `kel` to return |

`kel.conf` also turns on the mouse (to select text, hold Shift while you drag),
counts windows from 1, and labels each pane.

## The sidebar

Every kel window has a 32-column pane on its left — the same in every window —
in two sections:

- **`spaces`** — where things are: one entry per repo checkout or worktree,
  with its branch, `↑` unpushed / `↓` behind, `●` uncommitted files and the
  ports its servers listen on. The space you are in is open, listing its
  servers, shells and programs; the others are folded. **Every worktree of a
  repo nests under it** — kel's, Claude Code's own (`.claude/worktrees`) and
  ones made by hand, open in a window or not — one line each: `└ ● fix-auth
  ↑1 · review`, grey when no window stands there, `merged` in green when it
  has landed. `s` sweeps a landed one (`s` elsewhere: everything landed),
  asking first. A checkout two or more agents work in says so (`2 agents`)
  — said, never refused.
- **`agents`** — who needs you, most urgent first: a dot per state (yellow
  waiting, cyan working, green finished-and-not-yet-looked-at, dim idle or
  seen, magenta throttled, red `✕` dead, dim `?` unknown), then how long, the
  context % and the cost. A working agent adds a third line saying what it
  is doing — `editing sbview.go`, `run the tests` — its last tool call, read
  from the tail of Claude Code's own transcript. The repo beside an agent's
  name is the one it works in.

At the foot, when there is room, two groups of keys you can also click, two to
a line:

```
agent kelsier                       space kelsier
 e edit file     p plan              f files         g git
 r rename        x kill              d diff          e nvim
                                     t shell         c claude
kel                                  v PRs
 n new agent     w worktree
 ` waiting       u usage
 ; command       ? keys
 q quit
```

The first is **what you can do to the selected thing** — an agent, a space or
worktree, a pane — under what it is and its name; the second is **kel's own**
(`b restore` joins it while a previous workspace waits). No key needs shift.
Hovering shows which half a click would hit.

**A prompt says what it asks, shows what you type, and how to answer**, on a
line you can click: `new agent in kelsier` / `› fix▏` / `enter ok · esc
cancel · tab dir`. **A click anywhere else, or leaving the sidebar, cancels
it** as `esc` would — the click then does what it would have done. `;` opens
one for a kel command (`url`, `sweep -n`, `restore --list`…): a one-line
answer lands in the footer, a longer one in a popup.

**`n` and `w` start an agent anywhere.** Type a name and it starts in the
selected space; type a directory (`~/code/api`, `/srv/x`, `../other`) and it
starts there, named after the folder; type both (`fix ~/code/api`) for both.
`tab` completes the directory as a shell would, and the prompt says where the
agent will start before you press `enter`.

**`Ctrl-b k` means: press Ctrl-b, let go, then press k** — tmux's prefix, then
the key. kel writes every one of these the same way, using your real prefix.

**`Ctrl-b k`** opens a `kel` prompt at the bottom of the screen, so a kel
command never needs a shell: type `new docs -w`, `restore`, `url`… A
one-line answer comes back as a message, a longer one in a popup.

A blue `▌` marks the selection; where you are is the name in bold. Only states
that need you are coloured — waiting yellow, working cyan, finished-and-unseen
green, dead red; idle, seen and unknown are grey, and a doubtful `working?` is
grey italic. The footer reads `click, or Ctrl-b then space` until the sidebar
has the keyboard — or, while an agent waits on you, `1 waiting 6m · Ctrl-b
then \`` in yellow, which a click also jumps — then shows the keys for whatever is selected (`e edit · p plan · x kill · ?` on an agent) — `?` lists them all, and a click on a bold key there runs it.

| key | |
|---|---|
| `Ctrl-b Space` | put the keyboard in the sidebar (replaces tmux's `next-layout` on that key) |
| `j` / `k` | move |
| `enter` | jump to an agent or pane · open or fold a space |
| `space` / `l` / `h` | fold, open, close a space |
| `/` | filter by name, live · `esc` clears |
| `n` / `w` | new agent / new worktree agent in the selected entry's space — type the name in the footer |
| `r` | rename the selected agent |
| `x` | kill the selected agent, after `y` — a worktree with unsaved work is refused, and the footer names what is at risk |
| `f` / `g` | yazi / lazygit in a popup, at the selected entry's worktree |
| `d` | that worktree's changes — committed and uncommitted — against a base you are asked for: the branch it lands on, offered and editable, `tab` completes a branch (diffview.nvim if installed, else `delta`, else `git diff`) |
| `e` | the file the selected agent last edited, at the line — in the nvim you have open in that worktree, else a popup |
| `o` | the selected server in your browser — its lowest port; the footer names the others |
| `e` on a space | nvim at that directory, kept between opens — your buffers are still there next time |
| `t` | a plain shell window in the selected space — no agent |
| `q` | quit kel, after `y`: saves the workspace, then closes it all; refuses while a worktree holds unsaved work |
| `p` | the selected agent's plan (from plan mode), then its task list — `✓` done, `▸` in progress, `·` pending, and what blocks what. Most sessions on newer models keep no task list; it says so. Rendered with glow when installed |
| `c` | the Claude Code configuration that applies to the selected space, in yazi, a tab per place: the repo's `CLAUDE.md`, its `.claude/`, the global `~/.claude` and the project's memory. To read and open — kel writes none of it |
| `v` | gh dash across **every repo a space is open in**: tabs for the selected space's repo, yours everywhere, review requests, all open; issues too. `/` edits a search, `t` drops the repo filter, `?` lists gh dash's keys |
| `u` | Claude Code usage from ccusage — the current 5-hour block and two weeks by day; quit it (`q`) and type any other report (`monthly`, `session`, `daily --since 20261001`). ccusage reads Claude Code's own transcripts, so kel stores no history |
| `n name -- flags` | after ` -- `, flags for Claude itself: `fix -- --model sonnet` (`kel new fix -- --model sonnet` from a shell) |
| `;` | a kel command; quotes keep words together, `↑` / `↓` walk the last twenty |
| `s` | sweep the selected worktree once it has landed (anywhere else: everything landed), after `y` |
| `?` | every key |
| `esc` | back to the pane you were in |
| click · wheel | select and jump, fold a space · move |

**`Ctrl-b q` closes any popup**, whatever is running in it — even vim that
yazi opened for you. It says so in every popup's title. Quitting the tool
itself closes it too (`q` in yazi and lazygit, where `esc` also works, `:q` in
nvim). yazi and lazygit keep running when closed with `Ctrl-b q`, so
reopening them lands where you left off; the diff, plan and editor popups are
made fresh each time. Outside a popup `Ctrl-b q` is tmux's usual
`display-panes`. A tool that is not installed is said in
the footer (`kel doctor` lists them as optional). The browser is `$BROWSER`,
else `open` / `xdg-open` / `wslview`, else Windows' `start` from WSL.

A finished agent stays green until its window has been shown; after that it
dims. A waiting agent says what it waits for — `waiting 6m · allow Bash?`. An
agent kel has heard nothing from for half an hour while "working" reads
`◌ working? · quiet 14h`: an interrupt (esc) fires no hook, so kel will not
claim it is still at work. The entry under the mouse pointer shows a dim bar —
what a click would hit.

Under an agent: its subagents (`└ ● Reading parser.go · Explore`, `✓` when
done), and any background agent (`claude --bg`) of the same name (`└ ● pr52 ·
bg · blocked`); other background agents are listed on their own, marked `bg`.
`enter` on one opens it (`claude attach`) in a popup. An agent's working or
idle comes from Claude Code itself where it can say.

When an agent starts waiting and you are looking elsewhere, **a banner opens
at the top of the window** — `● pr52 is waiting — Claude needs your
permission to use Bash   ↵ go  or Ctrl-b then \`  ✕`, with `+1 more` when several are. Click it
(or `↵ go`, or the key it names) to go there; `✕` dismisses it. It also leaves by itself when you
visit that agent or the agent moves on; it is never shown for the agent you
are looking at. `notify_banner = "off"` turns it off. kel also rings your
terminal's bell and shows a desktop notification (a Windows toast on WSL) —
see `setup.md` §5b to change either. Hidden sidebars do no work; the one you can see refreshes every two
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
wins). Non-repo agents go to `misc`. A window you make yourself (`Ctrl-b c`)
gets the group of the repo its pane is standing in.

The group is what `kel ls` sorts by, what `kel kill <group>/<name>` names, and
the label each of the sidebar's spaces carries.

**window vs pane:** `kel new` makes a **window** — one agent. `Ctrl-b | / -`
makes a **pane** inside it, for your own editor, shell or tests. kel tracks
state per window, so a second agent belongs in its own window.

## The quota line, and tmux's bar

While sidebars show, **tmux's own bar is off**: it repeated what the sidebar
and Claude's row already say. The account's quota — the one thing that
belongs to no pane — is the sidebar's last line:

```
claude █████ 58% 1h20m · wk 41%
```

It drops the `claude` label, then the reset time, when the sidebar is narrow;
no line until a session has reported a quota. The waiting alarm is the
sidebar's footer (above).

`kel sidebar off` brings tmux's bar back, carrying the same and more:

```
claude ███░░ 58% (1h20m) · wk 41%  │  1 waiting 6m · $24.10
```

| segment | |
|---|---|
| `claude ███`‹grey›`██ 58% (1h20m)` | the 5-hour quota and when it resets; `░░░░░ ?` once it has reset since it was last seen, never the old figure |
| `wk 41%` | the weekly quota (`wk ?` when stale) |
| `spend 61%` | the spend limit, from 50% only |
| `1 waiting 6m` | agents blocked on you, and the longest wait; `nothing waiting` when none are |
| `$24.10` | the cost of the agents open right now |
| `kelsier · fix-auth ↑2 ●3` | only while the sidebar is off: where the pane you are in stands |

No quota shows until a session has reported one — an API-key session never
does, and the bar never invents a `0%`. If `kel-fleet` is missing the bar says
so in red rather than going blank.

## Detach, kill, reboot

**`Ctrl-b d` detaches** and snapshots; every agent keeps running and `kel`
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
- `banner` — one line per agent the top banner is showing (window, state,
  name, Claude Code's words), gone when it is; `prompt-history` — the last
  twenty `;` commands; `gh-dash.yml` — the config `v` writes for gh dash
- `quota` — the account's rate limits, from `kel statusline`
- `sidebar-fail.log` — appended only when a window is left with no sidebar
  pane, with whatever tmux said, newest 100 kept. Why that happens is not
  known yet; a window switch heals it. **Temporary**: when a log explains a
  real failure, that is fixed and this goes with it
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
Opus 5.5 fast xhigh · ███░░░░░░░ 25% ctx · $31.90 · 14:32
```

Outside kel, where there is no sidebar or bar, it also carries those:

```
tharpep/kelsier@auth-fix · Opus 5.5 · ███░░░░░░░ 25% ctx · $31.90 · 14:32 · 5h 74% (39m)
```

| segment | appears when |
|---|---|
| `owner/name@worktree` | outside a kel window, and the payload names a repo |
| `fast` `xhigh` `no-think` | those modes are not at their defaults |
| `250k/1M` after the bar | context is at `KEL_CTX_WARN` or above |
| `14:32` | always — the clock tmux's bar carried before the sidebar turned it off |
| `$N.NN` | cost is non-zero |
| `5h` + reset | outside a kel window, always when reported |
| `7d` / `spend` + reset | outside a kel window, from 75% / 50% |
| an agent name | the session was started with `claude --agent` |

`ctx ?` means not measured yet (before the first reply, and right after
`/compact`) — not `0%`. Subagent cost is not in this row; no payload exposes it.

## Known limits

- **Two agents in one window** run fine, but state is per window, so the bar
  shows one state for the pair.
- **Restored panes run a fresh shell** unless the command was an agent or on
  `restore_cmds` (nvim, lazygit, htop, …).

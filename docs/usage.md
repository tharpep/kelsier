# kel — usage

`kel` is one command. `tmux` does the multiplexing underneath; you rarely type
`tmux` directly any more.

## Install

```bash
cd ~/code/kelsier && ./install.sh
tmux source-file ~/.tmux.conf     # pick up kel.conf changes
# first install only: restart running Claude sessions so they load the hooks
```

`install.sh` symlinks `bin/kel` to `~/.local/bin`, makes `~/.tmux.conf` source
`tmux/kel.conf`, and merges the 7 state hooks + the `statusLine` into
`~/.claude/settings.json`. **`./install.sh --uninstall` reverses all of it** —
symlinks, completions, the `~/.tmux.conf` line, the `~/.zshrc` fpath line and
the `settings.json` merge, restoring any `statusLine` kel displaced. It leaves
`~/.local/state/kel` alone, because uninstalling the tool must not be able to
lose your work; `--uninstall --purge` removes that too, after asking
(non-clobbering, idempotent — a `.kel-bak.<epoch>` copy is kept each run).

## Commands

```
kel                    in a repo: go to (or start) that repo's agent, the way
                       `claude` keys off the directory. Elsewhere: your last group.
kel new <name>         new window + agent in the current repo   (inplace)
kel new <name> -w      ...in a fresh git worktree on a new branch <name>
kel new <name> --group G       put it in group G instead of the repo's
kel new <name> --no-agent      just make the window, don't start the agent
kel new <name> --agent CMD     run CMD instead of `claude`
kel kill <name>        close the window; remove the worktree if it was one
kel kill <name> -f     ...even with uncommitted / unpushed work
kel ls [--json]        list every agent, grouped by repo (state, context %, cost)
                       --json emits the fleet document: {generated_at, current,
                       agents:[...]}.  v0.6 changed this from a bare array —
                       the list is now under .agents
kel go [<group>]       switch to a group  (no arg: list the groups)
kel move [<group>]     change THIS agent's group
kel adopt [<group>]    make THIS window a kel agent — a plain `prefix c`
                       window, your editor, lazygit. With no group, both use
                       the repo the PANE is standing in, else $KEL_GROUP,
                       else `misc`
kel rename <newname>   rename THIS window and keep its metadata record in sync
kel board              browse agents AND panes — your editor, lazygit, a shell;
                       enter jumps to the pane itself  (Ctrl+Space
                       or prefix b).  enter jumps; tab = act on the highlighted
                       agent (jump / new here / rename / go to group / kill)
kel sweep [-n] [-f]    close out every finished agent whose branch has landed;
                       reports anything dirty, unpushed or unmerged instead of
                       touching it. -n shows what it would do
kel top                the fleet dashboard (also prefix t) — every agent, sorted
                       with a LAND column naming what each branch still needs
                       (dirty / unpushed / behind / no PR / in review / merged)
                       by who needs you. j/k scroll, s cycles sort
                       (triage/ctx/cost), / filters, q quits. Needs the Go
                       build; `kel doctor` says whether you have it
kel relaunch [name]    relaunch ONE crashed agent in its existing window (-f to
                       force while something is still running there)
kel restore [-c] [-s]  rebuild the workspace after a kill / reboot
                       (-c resume conversations; -s force the snapshot)
kel prune [-f]         discard dead agent records (and their worktrees)
kel doctor             probe the machine, cache to ~/.local/state/kel/doctor.json
```

Internal (wired into tmux / Claude Code, you won't call these):
`kel status-line [group]`, `kel jump`, `kel snapshot`, `kel cheat`,
`kel hook <EVENT>`, `kel statusline`, `kel _board_*`. `kel menu` is a deprecated alias for
`kel board` — kept one release for muscle memory.

**What you have actually reached for.** `kel _usage` prints a count per
subcommand, from a log kel appends to on each command you run — epoch and name
only, never arguments. Hot paths and background work are excluded, so it is
gestures rather than noise. `usage = "off"` records nothing. Both the counter
and `_usage` are scaffolding for the prune pass and go away with it.

**What kel writes, and where.** Everything lives in
`${XDG_STATE_HOME:-~/.local/state}/kel/`, which kel keeps at mode `0700` — the
records carry repo paths, branch names and Claude session ids, and a
`<window>.state` file carries Claude Code's notification text, which is the
agent quoting a command back at you.

**What leaves the machine.** One call, `gh api repos/<slug>/pulls`, and only
from `kel sweep`. It never fires for a repo whose `origin` is not on
`github.com`, and `pr = "off"` disables it outright — `land` then runs on local
git alone, which still answers dirty / unpushed / merged / behind. kel makes no
other network call and never sends anything to an agent API: it does not wrap
Claude Code, so your code reaches Anthropic by exactly the path it would from a
bare terminal.

**Shell completion.** `install.sh` drops a bash completion into
`~/.local/share/bash-completion/completions/kel` and a zsh one on the zsh
`fpath` (`~/.local/share/zsh/site-functions/_kel`, plus an `fpath+=` line in
`~/.zshrc` if that file exists — run `compinit` or restart the shell).
`kel kill <TAB>` completes agent names; `kel go <TAB>` / `kel move <TAB>`
complete group names.

Settings live in `~/.config/kel/config.toml`, overridden by a per-repo
`.kel/config.toml`, overridden by the env vars below. Copy
`examples/config.toml` to get started — every key is listed there with its
default. Env knobs: `KEL_NOTIFY` (states worth interrupting you for, default `waiting`;
e.g. `"waiting dead"`), `KEL_NOTIFY_CMD` (a command taking title + body — see
`setup.md`; kel always does a `tmux display-message` regardless),
`KEL_CTX_WARN` (context % at which the bar starts showing it, default `70`),
`KEL_BOARD_W` / `KEL_BOARD_H` (board popup size),
`KEL_GROUP` (force every `kel new` into one group — see below; a `.kel/group`
file pins a directory and everything under it, for monorepos that don't split
on git root, and `KEL_GROUP` still outranks it),
`KEL_SESSION` (session-name prefix, default `kel`), `KEL_AGENT` (default
`claude`), `KEL_WORKTREES` (default `<repo>/../.kel-worktrees`),
`KEL_RESTORE_CMDS` (pane commands to re-run on restore).

## The model — groups

**A group is a tmux session.** `kel/api` — the group *is* `api`, there's no
separate layer; the `group` field in the metadata just records which one a
window belongs to.

**By default a group is a repo.** `kel new` reads `$PWD` *at that moment*,
walks up to the git root, and uses its basename: `~/code/api` → group `api` →
session `kel/api`. `--group G` overrides; non-repo agents fall into `misc`.
That's the only moment the group is chosen — kel never watches your `cd`.

**Want one flat workspace instead?** `export KEL_GROUP=work` in `~/.bashrc` —
now every `kel new` lands in `kel/work` no matter the repo, so `prefix 1-9`
reaches every agent like plain tabs. `kel new x --group foo` still peels one
off into its own group when you want that.

**Monorepos.** One git root = one default group, so every service in a monorepo
lands together. Split them with `kel new checkout-svc --group payments` per
agent, or `export KEL_GROUP=payments` in the shell you work that service from.
(A per-directory `.kel/group` file is on the backlog — only if this bites.)

**Outside a repo**, `kel new` / `kel move` fall into a `misc` group and say so;
pass `--group NAME` (or `kel move NAME`) to name it something real.

**Relocating a window** — `cd` to another repo in a shell pane, run `kel move`;
the window (agent and all) hops into that repo's group and you follow it,
nothing restarts. `kel move <group>` for an explicit target.

**Adopting a window** — `kel adopt` (or `prefix m` → `a`) makes a window kel
does not track into a managed agent, in place or in a group you name. That is
what a hand-made `prefix c` window needs, and what the board's unmanaged pane
rows are asking for. Until v0.9 this was a silent fallback inside `kel move`,
reachable only if you happened to run the wrong command on the right window;
`move` now refuses and points here.

**window vs pane vs group:**
- `kel new <name>` → a new **window** (tab) = a new agent. The kel way to add one.
- `prefix | / -` → a new **pane** (split) inside a window — for *your* stuff
  (editor, shell, tests), not agents. kel tracks state per window, so a
  second agent belongs in its own window.
- a **group** is decided when you run `kel new` — from your directory's git
  root (or `--group` / `KEL_GROUP`), once. kel does not watch `cd`; a window
  never changes group on its own.

- The status bar shows only the current group's windows, plus `⟨api·1 web·2⟩`
  naming the other groups that have agents blocked on you. Inside a group: native `prefix 1-9` / `n` / `p`
  / `w`.
- Between groups: native `prefix (` / `)` cycle, `prefix g` group tree,
  `kel go <group>`, or the board (`Ctrl+Space`) → `enter` (jump) / `tab` → "go
  to its group".
- `` prefix ` `` (jump to next **waiting** agent) is **global** — it crosses
  group boundaries. That's the point of it: go to whoever's blocked, not to a
  specific group.
- `kel` (bare, from a shell) is **directory-aware**, like `claude`: run inside a
  repo, it attaches that repo's group — or, if nothing is running there yet,
  starts an agent for it (`kel new <repo>`) and drops you in. `KEL_GROUP`
  overrides the target. Run it outside any repo and it falls back to your last
  group. (After a reboot the snapshot-rebuild prompt still takes priority.)

**Status line.** The group you're in, in full, plus a badge for the rest:

```
[api] 0:auth-fix? [1:rate-limit*] 2:docs   ⟨infra·1 web·2⟩
```

`?` waiting on you · `*` working · `!` done · `x` = the agent process died
without a clean exit (SIGKILL / OOM / crash — `kel kill` or `kel relaunch` it) · bare =
idle · `[ ]` = current window · `⟨infra·1 web·2⟩` = other groups with agents
blocked on you, busiest first, capped at three then `+N` (hit `` ` `` to reach
them). In a non-kel tmux session the bar shows a compact
`[kel] N agents · M waiting`.

The `x` / `dead` state is a read-time check, not a hook: if the record says
`working` / `waiting` but the window's only process is a bare shell, the agent
is gone. It still shows on the board so you can act on it.

**Managed vs unmanaged.** `kel new` writes a metadata record — and so do both
menu paths' "new agent here" now (they route through `kel new`). Only tmux's
own `prefix c` makes an *unmanaged* window: still state-tracked, shown in
`kel ls` as `(unmanaged)`, and `kel kill` on it just closes the window (adopt it
into kel with **`kel adopt`**). State is keyed by window id, so two windows may even
share a name.

## Detach vs kill

**`prefix d` detaches** — the group and every agent keep running; `kel`
reattaches you. Round-trips perfectly; agents survive a full tmux detach.

**Killing** a group (`prefix &` on its last window, `tmux kill-session`)
ends its agent processes. `kel ls` then shows it as `dead`; `kel new` reclaims
the record, `kel kill` / `kel prune` discard it.

**A reboot / crash** takes down everything. kel snapshots the whole workspace —
every group, window, pane layout, and per-pane directory — on detach and
whenever the shape changes (`~/.local/state/kel/snapshot.json`, with one
generation kept as `snapshot.json.prev`). Starting `kel` with no live workspace
offers to rebuild it: **y** rebuilds, **n** starts fresh and won't ask again for
that snapshot, **d** discards the saved workspace. A new snapshot is a new
question, so `n` never hides a workspace you actually have. The next time
you run `kel` it offers to rebuild it:

```
kel rebuild your workspace? 3 group(s), 9 window(s)  [Y/n]
```

Yes → every group, window, and split comes back in place, agents resumed
(`--resume <session-id>`), your editor / lazygit panes re-launched. `kel
restore` does the same non-interactively; `kel restore -s` forces the snapshot
even if some sessions are still live.

Plain `prefix d` detaches and snapshots in one step (the `client-detached`
hook) — there is no separate key. Agents keep running; `kel` brings you back to
your last group.

## Isolation

`inplace` (default) — the agent edits your actual checkout. Right for most
tasks. `kel ls` shows this as **`repo`** in the WHERE column; `inplace` is the
word stored on disk, kept so the record format does not change.

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
resize, wheel to scroll — **to select text, hold Shift while you drag**, since
mouse mode owns a plain drag), counts windows from 1, and labels each pane.
**window = a tab, one per agent; pane = a split inside a window; group = one
tmux session per repo.**

`prefix` is `Ctrl+b`. Four keys carry the whole tool:

- **`` prefix ` ``** — jump to whoever's waiting on you (any group)
- **`Ctrl+Space`** (or `prefix b`) — the board: *find* an agent. `enter` jumps
  to it; `tab` opens a labelled menu to *act* on it (jump / new here / rename /
  go to its group / kill)
- **`prefix m`** — *manage* the agent you're on: rename / move to a group /
  new sibling / kill
- **`prefix k`** — "new to kel/tmux?" primer of the common moves; `prefix k`
  → `?` (or `kel cheat`) shows the full reference

| | |
|---|---|
| `Ctrl+Space` / `prefix b` | the board — filter, preview; `enter` jump, `tab` act |
| `prefix m` | manage this agent — rename / move / new sibling / kill |
| `prefix 0..9` / `n` / `p` / `w` | move between agents in the group |
| `` prefix ` `` | jump to the next agent **waiting** on you — any group |
| `prefix g` / `prefix (` `)` | pick / cycle groups |
| `prefix ,` | rename this window (routes through `kel rename`) |
| `prefix k` | "new to kel?" primer — new agent, browse, split, scroll, zoom, rename, close, detach, "show me around" |
| `prefix [` | tmux scroll mode for a shell pane (`q` to leave) |
| mouse wheel / PgUp PgDn | scroll an agent's conversation (kel maps the wheel to PageUp/Down) |
| `prefix \|` / `-` | split a window (agent + editor); arrows or click to focus; `z` zoom |
| `prefix d` | detach — agents keep running; `kel` to return |

## State files

`~/.local/state/kel/`
- `<window-id>.ctx` — `<pct> <cost> <in_tokens> <ctx_size> <rate_5h> <epoch>
  <model>`, written by `kel statusline` from the JSON Claude Code hands its
  status-line command. This is how kel knows an agent's context usage and cost
  **without interrupting it** — no `/context`, no reading the transcript. The
  bar shows `·NN%` only from `KEL_CTX_WARN` (default 70) up; `kel ls`, `--json`
  and the board preview always show it. Pruned with the window.
- `<window-id>.state` — `<state> <epoch> <note>`, written by the hooks, pruned when the
  window is gone
- `sessions/<group>/<name>.json` — one record per kel-managed agent. Keyed by
  group *and* name, so two repos can both have a `docs` agent; disambiguate on
  the command line with `kel kill <group>/<name>`.
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

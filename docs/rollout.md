# kelsier — rollout

The constraint is **"will I actually adopt this,"** not "can it be built." Each
stage ships the minimum that answers a real question; the next stage waits for
daily use to produce a specific pain.

`spec.md` section numbers are not a build order. This file is.

---

## v0 — status line + jump-to-blocked  ·  **done** (`beaf4d4`)

**Question:** does an always-visible state line plus jump-to-blocked fix the
sprawl? **Verified end-to-end** with real Claude Code 2026-08-28 — Claude Code
does pass tmux context to hook subprocesses.

- state adapter: 5 Claude Code hooks → `<state> <epoch>` per window, keyed by
  window id, atomic writes, `refresh-client -S` to redraw now
- pane resolution: `$TMUX_PANE` → `SessionStart` stash → lone pane matching cwd
- `status-left` fleet line (current window bracketed), `` prefix ` `` jump,
  `prefix k` cheatsheet
- `install.sh` merges hooks into `settings.json` without clobbering

## v0.1 — session lifecycle  ·  **done** (`c6606ea`)

**Trigger:** hand-rolling `tmux new-window` + `cd` + `git worktree` every time.

- consolidated the loose scripts into one `kel` command (subcommands: `new`,
  `kill`, `ls`, `restore`, `doctor`, `status-line`, `jump`, `cheat`, `hook`)
- `kel new <name> [-w] [--agent] [--no-agent]` — window + agent, inplace or in a
  git worktree; worktree runs the repo's `.kel/setup` (v0.2's hook, folded in)
- `kel kill` removes the worktree; refuses on uncommitted / unpushed unless `-f`
- `kel ls [--json]`; `kel doctor` cached to `doctor.json`
- graceful kill/reboot: dead sessions in `kel ls`, `kel restore [-c]` rebuilds
  windows from metadata (`-c` → `<agent> --continue`), `kel new` reclaims a dead
  record, stale `*.state` auto-pruned
- **one flat tmux session `kel`** — every agent is a window in it

**Testing before v0.2:** see the checklist at the bottom of this file.

## v0.2 — grouping by repo + a selectable fleet menu  ·  **done**

> **v0.2.1** added: `kel move` (relocate a window to another group), `KEL_GROUP`
> (one flat group), the session→agent/group rename, all-lowercase keys, plus:
> mouse on + smart wheel-scroll (PageUp/Dn to full-screen
> TUIs), 1-indexed windows, pane labels, pane commands in `kel ls`, and
> **workspace snapshot / restore** — `kel` rebuilds every group, window, and
> split (agents resumed) after a reboot. (Detach + snapshot ended up on plain
> `prefix d` via the `client-detached` hook — no separate key.)

> **Shipped.** One tmux session per repo (`kel/<group>`); `kel jump` global;
> status line = current group + `⟨+N waiting⟩`; `kel go`, the `kel menu`
> quick-jump, `prefix g` group tree; `group` metadata field + auto-migration
> from the flat session. `kel restore` rebuilds into groups. (v0.4 retired the
> `kel menu` quick-jump — the board replaced it.)

**Trigger:** the flat `kel` session overflows — routinely running agents across
several repos at once, so the status line is cramped, `prefix 1-9` runs out, and
unrelated projects share one list. **Do not build until flat use actually hits
this** (see the v0.1 test plan — the specific signal is "10+ agents across 2+
repos in a normal week" or "I keep losing which window is which project").

Two things land together: sessions grouped by repo, and the first
*select-and-see* surface beyond commands (`kel menu`). Commands stay the primary
path; the menu and, later, the board are faster surfaces onto the same ops.

### Grouping

The grouping unit is the repo. One tmux session per repo, named after it;
`kel new` targets the session for wherever it was run (`--group <g>` to
override, `~`-rooted agents fall into `misc`). Group *switching* is mostly
native tmux (`prefix s` tree, `prefix (` / `)` cycle) — kel only adds the
state-aware pieces.

```
session "api-gateway"   auth-fix?   rate-limit*   docs
session "coppermind"    sazed!      atium-check
session "infra"         tf-upgrade
```

- **`kel jump` goes global** — next `waiting` agent in *any* group, switching
  sessions if needed. Stable cycle order (group name, then window index).
- **Status line — option A (minimal).** Current group in full; every other group
  collapses to one badge:
  `[api-gateway] 0:auth-fix? [1:rate-limit*] 2:docs   ⟨+2 waiting⟩`
  You don't need to know *which* group — `` prefix ` `` takes you there. (Option
  B, per-group badges, only if you later miss knowing which project is blocked.)
- `kel ls` grows group headers; `kel restore` / `kel prune` operate across all
  groups; `kel` bare lands in the last-used group (tracked in a
  `last-group` state file).
- `kel go <group>` — new; `switch-client` to that group, shell-completion on
  group names.
- session metadata gains a `group` field.
- **Migration:** existing flat-session records get `group` from their `repo`
  basename on first v0.2 run; live windows stay where they are until killed /
  recreated. No forced move — the old `kel` group empties over a few days.

### `kel menu` — the selectable fleet view

`kel menu` (and a `prefix` binding, e.g. `prefix m`) generates a **dynamic**
`tmux display-menu`: a floating list of every agent across every group, each
showing its state, press a key to jump there.

```
 ┌ kel ───────────────────┐
 │ api-gateway            │
 │  1  auth-fix       ?   │
 │  2  rate-limit     *   │
 │ coppermind            │
 │  3  sazed         !   │
 │  4  atium-check       │
 │ ─────────────────────  │
 │  n  new agent here    │
 │  `  next waiting      │
 └───────────────────────┘
```

Native tmux, ~30–40 lines to build the menu string. No scroll, single-key
items — good to ~15–20 agents. Past that (or when you want filter / sort /
browse) is the v0.3 board.

`prefix g` → native `choose-tree -Zs` for a plain group picker; a state-aware
group menu can come later if the plain one isn't enough.

### Build order

1. `group` field + `kel new` targeting group sessions
2. `kel jump` cross-session
3. `kel ls` grouped
4. `kel restore` / `kel prune` across groups
5. `kel go` + `prefix g`
6. `kel menu` (dynamic display-menu)
7. two-level status line (option A) — last, it's the fiddliest
8. metadata auto-migration

~180–230 lines net. No new dependencies. Fiddliest parts: the status-line
width handling and the `kel jump` cycle order.

### Risks

- **Session name collision** — two repos both named `api`. `--group` override +
  document; rare enough.
- **Status-line clutter** — option A sidesteps it; if even that feels noisy,
  drop to just the current group's windows and rely on `` ` `` for cross-group.
- **The flat→grouped transition** — a few days of a `kel` group alongside real
  groups until the old one empties.

## v0.3 — the board (popup)  ·  **done**

> **Shipped** on `fzf` (not a hand-rolled TUI or Bubble Tea). `kel board` /
> `prefix b`: a `display-popup` with fuzzy filter, a preview pane (metadata +
> recent output + git status), and binds — enter jump, ctrl-n new, ctrl-k kill,
> ctrl-g go-to-group, ctrl-r refresh. ~50 lines.

**Trigger:** `kel menu` stops being enough — more than ~20 agents, or you want
filter / sort / browse across everything at once.

- `kel board` as a `display-popup` TUI (shell TUI, or Bubble Tea if it needs the
  structure) — the cross-group navigator, filter, jump, new, kill
- transient: opens over the current window, never occupies a pane

## v0.4 — UX & learnability pass  ·  **done**

> A review (mine + Gemini via agy) found the core loop solid but flagged
> friction for someone learning the tool. No new features.

- **One job per key.** The old `display-menu` quick-jump is retired (`kel menu`
  is a one-release alias for `kel board`). It took two review passes (mine, then
  Gemini 3.1 Pro on the first cut) to land the final shape:
  - **`` prefix ` ``** — jump to whoever's waiting
  - **`Ctrl+Space` / `prefix b`** — the board: *find* an agent. Compact
    `--footer` (`enter jump · tab actions`); `enter` jumps, **`tab`** opens a
    labelled `tmux display-menu` on the highlighted agent (jump / new here /
    rename / go to group / kill). `ctrl-n/k/g/r` stay bound, unadvertised.
  - **`prefix m`** — *manage* the agent you're on (rename / move / new sibling /
    kill). A `display-menu`, not the board.
  - **`prefix k`** — the "new to kel?" primer (tmux primitives + "browse agents"
    + "show me around").

  The first cut had the board on three keys with a truncating single-line
  header; the fix was the footer + `tab` menu, and repointing `prefix m` from
  the board to "manage".
- **Bugs.** `kel rename <new>` (+ `prefix ,`) renames a window *and* its
  metadata record — a bare `rename-window` used to desync it so `kel kill`
  couldn't clean up; `kel kill` now also finds a record by window id as a
  fallback. Both menu "new agent" paths route through `kel new` (managed). The
  board's `ctrl-k` always confirms; `ctrl-n` uses tmux's own prompt. A crashed
  agent (no clean exit) now reads as `dead` / `x` instead of stuck on `working`.
- **Learnability.** First run (and `kel` with zero agents) prints the three keys
  that matter. Shell completion for bash + zsh (`install.sh` wires it) —
  subcommands, agent names for `kill`, group names for `go` / `move`. The
  copy-text tip (Shift+drag) moved up into `kel cheat`'s model section.
- **Dir-aware `kel`.** Bare `kel` now keys off the current directory the way
  `claude` does: inside a repo it attaches that repo's group, or starts an agent
  for it if nothing's running there. `KEL_GROUP` overrides; outside a repo it
  still falls back to the last-used group; the reboot snapshot-rebuild prompt
  still wins.
- **Group model.** `kel new` / `kel move` outside a repo now warn about the
  `misc` fallback. `kel move` warns when relocating a worktree agent to a
  mismatched group (cosmetic only). Monorepo guidance documented in `usage.md`;
  a `.kel/group` file stays on the backlog.

**Backlog from this round:** `install.sh --uninstall`; `.kel/group` per-dir
override; context-broadcast between agents.

## v1.0 — consolidation

**Trigger:** the shell scripts got slow or hairy; or it's ready to show people.

- Go + Bubble Tea single static binary replacing the shell `kel`
- `~/.config/kel/config.toml` + per-repo `.kel/config.toml`
- README leading with the layout and the jump key; MIT

---

## Explicitly not on the path

- **Two-slot / swap-pane architecture** — cut, rationale in `spec.md` §5c.
  Revisit only with a concrete pain the popup board cannot address.
- Diff view in an `ops` panel, session templates, PR status on the row,
  attach-to-external — candidates in `spec.md`; none committed, none blocking.

---

## Testing v0.1 before v0.2

### Surface check (once, ~10 min)

- [ ] `kel new a` from inside a repo → window + `claude` starts, you land on it
- [ ] `kel new b --no-agent` → window, no agent
- [ ] `kel new c -w` from a repo with a `.kel/setup` → worktree on branch `c`,
      setup ran, you're in it
- [ ] `kel ls` → all three, right isolation / branch / dirty
- [ ] `kel kill a` → window gone
- [ ] make an uncommitted change in `c`'s worktree, `kel kill c` → **refuses**,
      shows the diff; `kel kill c -f` → removes it, branch kept
- [ ] `prefix d`, then `kel` → back exactly where you were, agents still running
- [ ] kill the session (`tmux kill-session -t kel`), `kel ls` → sessions show
      `dead`; `kel restore -c` → windows rebuilt, agents resuming
- [ ] `` prefix ` `` with one agent `waiting` → jumps to it
- [ ] `prefix k` → cheatsheet; status bar shows `*` / `?` / `!` as agents work

### Real use (the actual test — ~1–2 weeks)

Run your genuine parallel work through `kel`. Watch for:

**v0.1 is working** if — you `kel new` without thinking about tmux, `` prefix ` ``
is reflexive, you stop opening throwaway terminals to check on an agent, and
`kel restore` gets you back after a reboot.

**v0.1 needs a fix first** (not v0.2) if — hooks misfire (state stuck / wrong),
`kel kill` ever loses work, the status line lies, or `restore` doesn't
faithfully rebuild.

**Go to v0.2** when — you hit **10+ agents across 2+ repos in a normal week**, or
you notice you keep losing which window belongs to which project, or the status
line is too wide to read. Not before.

**Stay flat** if — you top out around 5–6 agents and they're usually one repo at
a time. Then v0.2 is dead weight and the flat model wins.

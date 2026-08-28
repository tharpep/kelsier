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

## v0.2 — grouping by repo

**Trigger:** the flat `kel` session overflows — routinely running agents across
several repos at once, so the status line is cramped, `prefix 1-9` runs out, and
unrelated projects share one list. **Do not build until flat use actually hits
this** (see the v0.1 test plan — the specific signal is "10+ agents across 2+
repos in a normal week" or "I keep losing which window is which project").

**Design.** The grouping unit is the repo. One tmux session per repo, named
after it; `kel new` targets the session for wherever it was run (`--group <g>`
to override, `~`-rooted sessions fall into `misc`).

```
session "api-gateway"   auth-fix?   rate-limit*   docs
session "coppermind"    sazed!      atium-check
session "infra"         tf-upgrade
```

- **`kel jump` goes global** — next `waiting` agent in *any* group, switching
  sessions if needed. It must ignore group boundaries; that's the point of it.
- **Status line, two-level** — current group in full, other groups compact and
  only when they want attention:
  `[api-gateway] 0:auth-fix? [1:rate-limit*] 2:docs · coppermind⟨1?⟩`
- `kel ls` groups with headers; `kel restore` puts sessions back in their groups
- `kel` bare → last group, or a picker if ambiguous
- new: `kel go <group>`, maybe `prefix G` for a state-aware group jump
- session metadata gains a `group` field

Within a group, nothing changes — native `prefix 0-9` / `n` / `p`.

Cost ~100–150 lines; the fiddly part is keeping the two-level status line
legible. This replaces the old "chrome-tab-groups inside one session" idea —
real tmux sessions do the same job without a bookkeeping layer to invent.

## v0.3 — the board (popup)

**Trigger:** still losing track *despite* v0.2 — you want filter / scroll /
browse across everything at once.

- `kel board` as a `display-popup` TUI (shell TUI, or Bubble Tea if it needs the
  structure) — the cross-group navigator, filter, jump, new, kill
- transient: opens over the current window, never occupies a pane

## v1.0 — consolidation

**Trigger:** the shell scripts got slow or hairy; or it's ready to show people.

- Go + Bubble Tea single static binary replacing the shell `kel`
- `~/.config/kel/config.toml` + per-repo `.kel/config.toml`
- README leading with the layout and the jump key; MIT

---

## Explicitly not on the path

- **Two-slot / swap-pane architecture** — cut, rationale in `spec.md` 5b. Revisit
  only with a concrete pain the popup board cannot address.
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

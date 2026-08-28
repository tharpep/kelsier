# kelsier — rollout

The constraint is **"will I actually adopt this,"** not "can it be built." So
each stage ships the minimum that answers a real question, and nothing is added
until daily use produces a specific pain that demands it.

The `spec.md` section numbers are not a build order. This file is.

---

## v0 — no Go, just shell + tmux config

> **Status: built 2026-08-28** (`5b40b5d`). All mechanics unit-tested against an isolated tmux server. Not yet verified against a real authenticated Claude Code session — the one open question is whether Claude Code passes `$TMUX_PANE` to hook subprocesses; tiers 2 (stash) and 3 (cwd match) in `hooks/kel-hook.sh` are the fallback. See `docs/v0.md`.

**Question it answers:** does an always-visible state line plus jump-to-blocked
actually fix the sprawl?

- `bin/kel-status` — reads `~/.local/state/kel/*.state`, prints
  `1:auth-fix? 2:rate-limit*` for `status-left`.
- `bin/kel-jump` — finds the first `.state` file in `waiting`, runs
  `tmux select-window` on its window.
- Three Claude Code hooks (`UserPromptSubmit` / `Notification` / `Stop`), each
  writes `<state> <epoch>` atomically to the session's state file and runs
  `tmux refresh-client -S`.
- `hooks/kel-hook.sh` resolves the pane 3 ways: `$TMUX_PANE` from the env, the
  `SessionStart` stash, or the lone tmux pane whose cwd matches the payload.
- Sessions are native `tmux` windows made by hand. Window name = session name.
- `tmux/kel.conf` — the `status-left` line and the `` prefix ` `` binding,
  sourced from `~/.tmux.conf`.
- `install.sh` — apt/release installs (see `setup.md`), symlinks, and a
  `jq`-based merge of the hooks into `~/.claude/settings.json` that does not
  clobber existing MCP servers or hooks.

**Get right now, because it is painful to retrofit:** the state adapter — file
format (`<state> <epoch>`), atomic write, the `window_id` stash, and the merge
into `settings.json`. Everything above this is just rendering.

**Session creation is not part of v0 — the on-board agent does it.** Claude Code
in the shell runs `git`, `tmux`, and shell commands directly. Until `kel new`
exists you start one session and ask it to provision the rest:

```
git worktree add ~/code/.wt/<name> -b <name>
cd ~/code/.wt/<name> && <install deps> && cp <repo>/.env .env
tmux new-window -n <name> -c ~/code/.wt/<name>
tmux send-keys -t <name> 'claude' Enter
```

That last line has the agent launch another agent in a new window — a normal
interactive session the parent does not control. Its first permission prompt
fires the `Notification` hook, so it appears as `?` on the status line
immediately. Allowlist the `git worktree` / `tmux` verbs in `settings.json` so
this does not prompt every time. A saved prompt snippet (or a `CLAUDE.md` line)
is the proto-`kel new`; `kel new` in v0.1 just removes the need to spell it out.

---

## Then, each only on a proven daily pain

| Add | Trigger — the pain that justifies it |
|---|---|
| ~~`kel new` / `kel kill`~~ **done** | consolidated the loose scripts into one `kel` command; `new`/`kill`/`ls`/`doctor`; `--worktree` runs `.kel/setup` |
| ~~`.kel/setup` hook~~ **done** | folded into v0.1 — `kel new -w` runs it after creating the worktree |
| `kel doctor` | a machine behaved differently and you want the probe cached instead of rediscovered |
| Bubble Tea board (popup) | still losing track *despite* the status line — i.e. routinely >6 sessions, need filter / scroll / browse |
| Go rewrite of `kel-status` / `kel-jump` | the shell scripts became too slow or too hairy to maintain |

## Explicitly not on the path

- **Two-slot / swap-pane architecture.** Cut. Rationale in `spec.md` 5b. Only
  revisit with a concrete pain the popup board cannot address.
- Diff view, session templates, PR status on the row, attach-to-external —
  `spec.md` calls these candidates; none are committed and none block anything.

---

## Success / failure signals for v0

**Adopt** if, after ~2 weeks: you reach for `` prefix ` `` reflexively, you stop
opening extra terminal tabs to "check on" an agent, and you can answer "which
agent needs me" without switching windows.

**Abandon or rethink** if: the status line is noise you ignore, the hooks
misfire often enough that you stop trusting the state, or you find yourself
back in plain tabs within a week.

# kelsier

A terminal workspace for running several coding agents at once — and seeing,
the way an IDE would show you, what each one is doing, where, and to what.
`tmux` does the multiplexing; `kel` does the bookkeeping.

Command is `kel`. Named for the crew leader who assembles the team and runs the
job without doing every piece himself.

---

## The problem

Running N interactive agent sessions is easy to *start* and hard to *keep track
of*. You lose which terminal holds which task, which branch it's on, whether
its work is pushed, and which one is blocked waiting on you. Leaving the IDE for
the terminal costs you the file tree, the git view and the overview that came
with it.

## The approach

`kel` does not wrap the agent. `tmux` allocates a PTY and the agent owns it
completely — Claude Code inside kel is byte-identical to Claude Code in a bare
terminal. kel tracks state from Claude Code's own hooks and `statusLine`, and
puts one view over all of it.

**Where it is going** (v0.10, in progress): one workspace window with a
persistent sidebar listing every pane — agents, servers, shells — grouped by
repo and worktree, each with its branch, ahead/behind, dirty state, and for
agents their state, context, cost and model. The selected pane fills the main
slot. A panel opens `yazi`, `lazygit`, or the session's plan and task list.
The reference is Orca's layout, in the terminal. Design in `docs/spec.md` §5.

## Non-goals

- **Not a harness.** Never proxies model API calls, injects prompts, or manages
  context.
- **Not a terminal, not a multiplexer.** Your emulator; `tmux`.
- **Not a file manager, editor or git client.** It opens `yazi`, `$EDITOR` and
  `lazygit` in the right place; it never reimplements them.
- **Not a daemon.** State lives on disk and in `tmux`.
- **Not cloud-anything.** Fully local, no telemetry, no account.
- **Not a team tool.** Single user, their own machines.

## Status

**Mid-pivot.** `v0.9.0` is the last release of the old design (an fzf board
and menus over tmux sessions-per-repo). v0.10 replaced those surfaces with one
sidebar, and it is in daily use; `v0.10.0` is tagged once it has been
daily-driven. MIT. CI runs the suite on Linux and macOS on every push.
`docs/rollout.md` is the source of truth for what is next.

## Today

Every kel window carries a sidebar on its left: `spaces` (each repo and its
worktrees, with the branch, ahead/behind, dirty state and what landing it
needs) and `agents` (each one's state, context and cost, and what it waits
for). Click a row, or `Ctrl-b space` to give it the keys; its footer shows
the keys for whatever is selected. `` Ctrl-b ` `` jumps to whoever is blocked
on you, across every repo. Claude Code's own row shows the session — model,
context, cost — and the time.

```
kel                  go to (or start) this repo's agent, like `claude`
kel new <name> [-w]  new window + agent, optionally in a git worktree
kel ls [--json]      every agent, grouped by repo
kel relaunch [name]  relaunch one crashed agent in place
kel sweep [-n]       close out every finished agent that landed
kel kill <name>      close one       ·  kel prune    discard dead records
kel restore [--list] bring back a previous workspace (kel never asks)
kel doctor           check the machine
kel config / update  edit settings / pull and re-wire
```

## Install

```sh
git clone https://github.com/tharpep/kelsier && cd kelsier && ./install.sh
```

Needs `tmux` ≥ 3.0, `jq`, `git` and **Go** (it builds `kel-fleet` and
`kel-top`, the sidebar). `./install.sh --uninstall` reverses every change and leaves your
agent records alone. `kel doctor` tells you what you have. Linux, WSL2 and
macOS; `install/` provisions a fresh machine.

`docs/usage.md` is the reference · `docs/rollout.md` is the plan ·
`docs/spec.md` is the design · `examples/config.toml` is every setting.

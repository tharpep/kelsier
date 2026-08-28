# kelsier

A terminal workspace for running several coding agents at once. `tmux` does the
multiplexing; `kel` does the bookkeeping.

Command is `kel`. Named for the crew leader who assembles the team and runs the
job without doing every piece himself.

---

## The problem

Running N interactive agent sessions is easy to *start* and hard to *keep track
of*. The failure mode is tab sprawl: you lose which terminal holds which task,
which branch it's on, and which one is blocked waiting on you.

## The approach

`kel` does not wrap the agent. `tmux` allocates a PTY and the agent owns it
completely — Claude Code inside a `kel`-managed window is byte-identical to
Claude Code in a bare terminal: same TUI, same keybindings, same MCP, same
hooks. `kel` only tracks state, renders a fleet view, and gives you one
keystroke to jump to whichever agent is blocked on you.

**The bet:** the valuable part is the bookkeeping, not the interface.

## Non-goals

Explicit, because each is a thing this project will be tempted into.

- **Not a harness.** Never proxies model API calls. Never injects prompts.
  Never manages context.
- **Not a terminal.** Uses whatever emulator you have.
- **Not a multiplexer.** `tmux` exists and is thirty years old.
- **Not a file manager.** `yazi` exists. The panel can *launch* it; it will
  never reimplement it.
- **Not a daemon.** State lives on disk and in `tmux`; nothing runs in the
  background but `tmux` itself.
- **Not cloud-anything.** Fully local, no telemetry, no account.
- **Not a team tool.** Single user, single machine — but trivially portable
  between your own machines (see `docs/setup.md`).

## Status

Pre-v0. See `docs/spec.md` for the design and `docs/rollout.md` for what gets
built in what order. The short version: v0 is shell scripts and `tmux` config,
no Go binary, and it exists to answer one question — does an always-visible
state line plus jump-to-blocked actually fix the sprawl?

## Substrate

Linux, via `tmux`. On Windows that means WSL2 (native Windows PowerShell has no
`tmux` and nothing with `swap-pane` semantics). Setup in `docs/setup.md`.

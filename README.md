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
completely — Claude Code inside a `kel` window is byte-identical to Claude Code
in a bare terminal: same TUI, same keybindings, same MCP, same hooks. `kel`
only tracks state, renders a fleet view on the status bar, and gives you one
keystroke to jump to whichever agent is blocked on you.

**The bet:** the valuable part is the bookkeeping, not the interface.

## Non-goals

Explicit, because each is a thing this project will be tempted into.

- **Not a harness.** Never proxies model API calls. Never injects prompts.
  Never manages context.
- **Not a terminal.** Uses whatever emulator you have.
- **Not a multiplexer.** `tmux` exists and is thirty years old.
- **Not a file manager.** `yazi` exists and is better than anything this would
  build. It will never be reimplemented here.
- **Not a daemon.** State lives on disk and in `tmux`; nothing runs in the
  background but `tmux` itself.
- **Not cloud-anything.** Fully local, no telemetry, no account.
- **Not a team tool.** Single user, single machine — but trivially portable
  between your own machines (see `docs/setup.md`).

## Status

**v0.4** — working and in daily use. One `kel` command:

```
kel                go to (or start) this repo's agent   kel ls      every agent, grouped
kel new <name>     new window + agent                   kel go [G]   switch group
kel new <name> -w  ...in a git worktree                 kel restore  rebuild after a kill
kel kill <name>    close one                            kel rename   rename this agent
```

One tmux session per repo and a state-aware status line. Four keys carry it:
`` prefix ` `` jumps to whoever's blocked on you (any group); **`Ctrl+Space`**
(or `prefix b`) opens the board to *find* an agent — `enter` jumps, `tab` acts;
**`prefix m`** *manages* the agent you're on; **`prefix k`** is the "new to kel?"
primer. Shell + `tmux` config, no binary yet.

`docs/usage.md` is the reference · `docs/rollout.md` is the build order ·
`docs/spec.md` is the design.

## Substrate

Linux, via `tmux`. On Windows that means WSL2 (native Windows PowerShell has no
`tmux`). Setup and toolchain in `docs/setup.md`.

**macOS is intended but not yet supported.** The script is bash-3.2-clean, but
three GNU coreutils assumptions fail silently on BSD userland — `rollout.md`
§ v0.5 lists them. Don't run it on a Mac until those land.

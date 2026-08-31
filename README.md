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

**The bet:** the valuable part is the bookkeeping, not the agent's interface.
kel's own surfaces — the bar, the board, the dashboard — are very much the
point; what it never does is reimplement Claude Code.

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

**v0.8** — working, in daily use, MIT. Linux and macOS green in CI on every
push. `docs/rollout.md` is the build order and the source of truth for what is
next.

## What it looks like

The status bar is the fleet. `?` is waiting on you, `*` working, `!` done,
`~` throttled, `x` dead, and a percentage appears once an agent's context
window is worth worrying about:

```
[api-gw] 1:auth-fix? 2:rate-limit·91%* 3:docs   ⟨infra·1 web·2⟩
```

`` prefix ` `` jumps to whoever is blocked on you, across every repo. That is
the one keystroke the tool exists for — not "go to window 3" but "go to
whoever needs me."

`prefix t` opens the dashboard:

```
  GROUP    AGENT       STATE       FOR   CTX     $        LAND        LAST OUTPUT
› api-gw   auth-fix    waiting ?   6m    12%     $2.40    dirty 3     Allow Bash to run npm test?
  api-gw   rate-limit  working *   1m    91%×3   $18.70   unpushed 2  Reading src/middleware…
  infra    tf-upgrade  throttled ~ 4m    –       –                    quota, resumes 22:40
  coppermind sazed     done !      12m   31%     $0.90    merged      Done — 3 files changed

  j/k scroll   s sort: triage   / filter   q quit
```

Sorted so the row that needs you is first. **LAND** names what each branch
still needs — commit it, push it, rebase it, open a PR, or sweep it.

## Commands

```
kel                  go to (or start) this repo's agent, like `claude`
kel new <name> [-w]  new window + agent, optionally in a git worktree
kel ls [--json]      every agent, grouped by repo
kel top              the fleet dashboard                        (prefix t)
kel board            find an agent — filter, preview, act       (Ctrl+Space)
kel relaunch [name]  relaunch one crashed agent in place
kel sweep [-n]       close out every finished agent that landed
kel kill <name>      close one       ·  kel prune    discard dead records
kel go [group]       switch group    ·  kel move     regroup this agent
kel adopt [group]    track a window kel does not manage yet
kel restore [-c]     rebuild after a reboot
kel doctor           check the machine
```

Four keys carry it: `` prefix ` `` jump-to-blocked · **`Ctrl+Space`** the board
(`enter` jumps, `tab` acts) · **`prefix t`** the dashboard · **`prefix m`**
manage the agent you're on. `prefix k` is a "new to kel?" primer.

## How it knows

Claude Code's own hooks report state — no polling, no scraping, no daemon. Its
`statusLine` reports context %, cost and rate-limit burn, which is how the bar
knows how deep a session is **without interrupting it**. Land state is plain
`git`, with one cached `gh` call per repo for PR status; an expired token
degrades that column rather than blacking it out.

## Install

```sh
git clone https://github.com/tharpep/kelsier && cd kelsier && ./install.sh
```

Needs `tmux` >= 3.0, `jq`, and `git`; `fzf` for the board. Go is **optional** —
it builds a faster fleet reader and the dashboard, and everything still works
without it. `./install.sh --uninstall` reverses every change it made and leaves
your agent records alone. `kel doctor` tells you what you have.

`docs/usage.md` is the reference · `docs/rollout.md` is the build order ·
`docs/spec.md` is the design · `examples/config.toml` is every setting.

## Substrate

Linux, via `tmux`. On Windows that means WSL2 (native Windows PowerShell has no
`tmux`). Setup and toolchain in `docs/setup.md`.

**macOS works** as of v0.5 and is covered by CI on every push. The script is
bash-3.2-clean and free of GNU coreutils assumptions; `install/` provisioning
is still Debian-only, so on a Mac install `tmux`, `jq` and `fzf` yourself.

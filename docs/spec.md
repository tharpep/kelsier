# kelsier — spec

A terminal workspace for running several coding agents at once — and for seeing
what each one is doing, where, and to what, the way an IDE would show you.
`tmux` does the multiplexing; `kel` does the bookkeeping and draws one view
over all of it.

This document is the design and the rationale. `rollout.md` is the build order
and the current status; `usage.md` is the reference for what exists today.

**v0.10 is a pivot.** The design up to v0.9 — one board, three menus, a
dashboard and a cheat sheet over tmux sessions-per-repo — is preserved at the
`v0.9.0` tag (`git show v0.9.0:docs/spec.md`). This document describes where
kel is going, and marks plainly what exists versus what is designed.

---

## 1. Premise

Running N interactive agent sessions is not hard to *start* and very hard to
*keep track of*. The failure mode is tab sprawl: you lose which terminal holds
which task, which branch it's on, and which one is blocked waiting on you.

The author built kel to fix that and then found they did not love using it.
The bookkeeping was right; the interface had grown one surface per problem —
an fzf board, a per-agent menu, a fleet menu, a dashboard, a cheat sheet — each
with its own keys. The fix is not more surfaces. It is one.

The reference point is **Orca** (onorca.dev): a desktop app with a persistent
sidebar of projects → worktrees → agents, each with a state dot and its
branch; the selected agent filling the middle; a file tree and git view on the
right; quota burn along the bottom. That is the shape kel wants, **in the
terminal** — bringing the IDE's crucial views into the terminal rather than
bringing the terminal into an IDE. The terminal is the lightest, leanest and
most compatible place to start. A desktop app may follow (§12c), as a second
reader of the same state.

The closest reference in the terminal is **herdr** (`herdrdev/herdr`): an
agent-aware multiplexer in Rust with a `spaces` / `agents` sidebar. kel
borrows its sidebar's shape (§5) and differs on purpose in two places: it
stays on tmux rather than replacing it (`backlog.md` R11), and it reads state
from the agent's hooks rather than parsing the screen (§9), so a permission
prompt can never be mistaken for an idle agent.

**The bet is unchanged:** the valuable part is the bookkeeping, not the
*agent's* interface. kel never wraps Claude Code, proxies its API calls,
injects prompts or manages its context — an agent in kel is byte-identical to
one in a bare terminal. kel's *own* interface is exactly what this pivot is
about.

---

## 2. Non-goals

- **Not a harness.** Never proxies model API calls. Never injects prompts.
  Never manages context.
- **Not a terminal.** Uses whatever emulator the user has.
- **Not a multiplexer.** `tmux` exists and is thirty years old.
- **Not a file manager, an editor, or a git client.** `yazi`, `$EDITOR` and
  `lazygit` exist and are better than anything this would build. kel decides
  *where* they open and *which directory* they point at; it never reimplements
  what they show. (Rejected items R4 and R5 in `backlog.md` still hold — this
  is how the IDE views arrive without violating them.) The three an ADE builds
  in arrive the same way (§5f): **editing** is a long-lived Neovim per
  worktree that kel hands files to over its own RPC socket; **diffs** are
  `diffview.nvim` or `delta` against the land base; **the browser** is your
  real one, opened at a server pane's port. Browser *automation* — Playwright
  and the like — is the agent's tooling, configured where the agent's tooling
  lives, never shipped or wired by kel (R2).
- **Not a daemon.** State lives on disk and in `tmux`; nothing runs in the
  background but `tmux` itself and, when it is open, the sidebar.
- **Not cloud-anything.** Fully local, no telemetry, no account.
- **Not a team tool.** Single user, their own machines.

---

## 3. What the user actually does with it

The author's own list, in their words where possible:

1. **A higher-level view of every terminal** — not just agents. Which panes
   are Claude, which are a running backend, which are a plain shell.
2. **Where each one is standing** — the repo, whether it is a worktree or the
   main checkout, the branch, whether it is up to date, how many commits are
   unpushed, whether there is uncommitted work.
3. **For each agent:** state, context %, cost, model.
4. **The scaffolding and the files** — a tree, and file contents the way an IDE
   shows them, without leaving the terminal.
5. **Claude's current plan and task list** for a session.
6. **Automations** — scheduled or looping agents, newer than the rest and the
   least settled. Last.

And the loop that has been true since v0: run several agents in parallel, jump
to the one blocked on you in one keystroke, hand-edit what they produce.

---

## 4. Substrate

`tmux` ≥ 3.0 on Linux, WSL2 and macOS. Native Windows has no `tmux`; WSL2 is
the Windows story. Setup and toolchain in `setup.md`.

**Containers are not the answer** to portability: kel drives the *host's* tmux
server — the one you attach to — and spawns agents that edit the host's repos
with the host's `claude` credentials. A container has its own PID and PTY
namespaces, and on macOS it is a Linux VM besides, so its tmux could never be
your tmux. Portability is cross-compilation for Go and POSIX-clean shell for
the bash that remains.

---

## 5. Layout — the workspace  ·  *sidebar built (M2, M3); panel designed*

```
┌ sidebar ─────────────────┬──────── the window's own panes ──────────┐
│ spaces                   │                                          │
│ ● kelsier                │  claude              │ nvim              │
│   main  ↑2 ●             │                      │                   │
│ ○ kelsier                │  (your splits, as you made them —       │
│   wt/fix-auth  ✓         │   kel never moves or resizes them)      │
│ ● api-server             │                      │                   │
│   main  :3000            │                      │                   │
│                          │                      │                   │
│ agents                   │                      │                   │
│ ● fix-auth               │                      │                   │
│   waiting 6m             │                      │                   │
│ ◐ kelsier                │                      │                   │
│   41% · $1.20            │                      │                   │
│ ◆ docs                   │                      │                   │
│   done                   │                      │                   │
│                          │                      │                   │
│ e edit · p plan · ?      │                      │                   │
├──────────────────────────┴──────────────────────────────────────────┤
│ claude ███░░ 58% (1h20m) · wk 41%  │  1 waiting 6m · $24.10          │
└─────────────────────────────────────────────────────────────────────┘
```

Glyphs and colours above are placeholders; the visual language is §5d and is
settled on rendered frames, not on this drawing. The panel (files, git, plan,
tasks) is a popup over all of it (§5c), so it has no column of its own.

**A sidebar pane in every window** (decided 2026-10-14, after measuring — §5b).
Agents and everything else stay ordinary tmux windows, with whatever splits
you made. Each window carries its own sidebar pane at a fixed width on the
left, all running the same Go program over the same state, so the sidebar
reads as one persistent thing that is simply always there. Selecting a row is
`select-window` (plus `select-pane` for a pane row): **nothing is moved, swapped
or resized.**

- **Sidebar** — a Go TUI (Bubble Tea v2), grown from `kel top`. **Two
  sections**, after herdr (`herdrdev/herdr`), which answers the same question
  in the terminal:
  - **`spaces`** — *where*: one entry per repo / worktree, its git position
    (branch, ahead/behind, dirty, land) and the ports its servers listen on.
    Expanding an entry lists its panes — agent, server, shell, program — so
    *every* pane is still one keystroke away.
  - **`agents`** — *who needs me*: a flat list of every agent with its state
    dot, then context % and cost. This is `` prefix ` `` kept on screen: with
    ten panes, a single repo → worktree → pane tree buries the one agent that
    is blocked, which is why the tree the first draft of this section drew was
    split.

  Each entry is two lines — the name, then a dim detail line. It is **the only
  kel surface**: no board, no menus, no cheat sheet.
- **The window's own panes** — untouched. Agents stay byte-identical; an
  `[agent | nvim]` pair stays a pair.
- **Panel** — files (`yazi`), git (`lazygit`), the session's plan and its task
  list, opened at the selected pane's directory as a titled `display-popup`
  (§5c).
- **Bottom bar** — only what belongs to no pane: quota and the waiting alarm
  (§7).
- **Keys** live in the sidebar, all shown in its footer. **The mouse is a
  first-class way to drive it**, not a fallback: clicking an entry is the
  fastest jump there is, and herdr's users single it out.

**What one sidebar per window costs, and how it is paid:**

- **N processes.** One `kel-top` measured ~10–12 MB and ~2.5% CPU while
  polling. Ten polling instances would be a quarter of a core for nothing, so
  **a sidebar whose window is not active idles** — no polling, no rendering —
  and is woken when its window becomes active. *Built in M2:* the visible
  sidebar reloads every 2 s, a hidden one only checks every 10 s whether it has
  become visible, and `SIGUSR1` (`kel _sidebar wake`, from the window-change
  hooks and every state change) makes it look at once.
- **Thin renderers.** Every sidebar draws the same thing, so each one should
  be a renderer of one computed model, not N computations of it: one fleet
  read feeds a cached model in the state directory, and the sidebars draw it.
  N sidebars then cost N renders, never N `git status` sweeps.
- **Identical everywhere.** Cursor, expanded rows and filter are *view state*
  shared through the state directory, so switching windows shows the same
  sidebar, not a different one. That is also what a later desktop app would
  read (§12c).
- **Every window needs one.** `kel new` creates it; a hook on `after-new-window`
  covers a hand-made `prefix c` window. A sidebar pane must be recognisable
  (a `@kel-sidebar` pane option) so `kel ls` and the dead-agent check skip it
  and snapshot/restore treat it as kel's own — it is kel's pane, not the
  user's.

### 5a. What tmux gives this

herdr's one structural advantage is that it owns the event loop — it *is* the
multiplexer, so it never has to ask what changed. kel stays on tmux (it keeps
tmux's persistence, detach, SSH and thirty years of config for free — herdr's
reviewers list resurrection and status widgets among what it is missing) and
closes that gap by asking tmux properly:

- **Control mode** (`tmux -C`) — subscribe to tmux's own events (window added,
  pane exited, window changed) instead of polling.
- **Format subscriptions** (`refresh-client -B name:what:format`, tmux 3.2+) —
  a control-mode client is sent `%subscription-changed` whenever a format's
  value changes: the active window, each pane's path or current command. This
  is how the sidebar becomes event-driven instead of polling every 2 s. It
  raises the **sidebar's** floor to tmux 3.2 (2021; Ubuntu 22.04 ships 3.2a)
  while kel's own stays 3.0 — below 3.2 the sidebar keeps M2's poll.
- **Hooks** (`after-new-window`, `session-window-changed`, `pane-exited`) —
  create a window's sidebar, wake the visible one.
- **Per-pane styles** (`set -p window-style`, `window-active-style`) and
  `pane-active-border-style` — the sidebar gets a background of its own and a
  border that matches it, so it reads as **chrome, not a pane in a box**; the
  pane you are in gets the one accent-coloured border. Much of herdr's polish
  is exactly this.
- **`display-popup`** — floating overlays (lazygit, yazi, a plan) dismissed
  with `q`. Titled and bordered (`-T " lazygit · fix-auth "`, `-b rounded`,
  3.3+, plain border below) so a popup reads as part of kel, with where it
  opened in its title.
- **Per-pane user options** (`@kel-sidebar`, `@kel-kind`) — metadata tmux keeps
  for us, readable in one `list-panes -F`.
- **`#{pane_current_command}` / `#{pane_pid}` / `#{pane_current_path}`** —
  what is running and where, for every pane, in one call.

### 5b. Why not one workspace window with panes swapped in

That was chosen first, on 2026-10-14, and reversed the same day after
measuring. It is recorded so it is not re-proposed — the v0.2 spec had
rejected it too (§5c at the `v0.9.0` tag), and its objections turned out to be
half right.

**Measured** on an isolated tmux server, with a script logging every
`SIGWINCH`, and a real Claude Code 2.1.299:

| test | result |
|---|---|
| `swap-pane` into a slot of the **same** size | 0 resizes in 6 swaps |
| `swap-pane` into a **different** size | 1 resize per swap |
| terminal resized while a pane is hidden | hidden pane keeps its size; resized once when next shown |
| switching windows that each carry a fixed sidebar pane | 0 resizes (after a new window's first display) |
| Claude Code under resize | redraws correctly, but it is **not** an alternate-screen app: each shrink pushed stale lines into tmux scrollback (0 → 3 → 6 lines over four resizes of an idle session) |

**The verdict on v0.2's four objections:**

| objection | verdict |
|---|---|
| no real side-by-side: a swapped pane cannot carry the user's own split | **true, and decisive.** `swap-pane` moves one pane. Keeping `[agent \| nvim]` together needs a nested tmux client in the slot or splits flattened into sidebar rows; both cost more than they buy |
| geometry desync; the next swap-in janks | **solvable** — keep the holding session the slot's size and a swap resizes nothing. Not decisive on its own |
| `prefix 1`..`9` collides with native window switching | moot either way — navigation moves into the sidebar |
| cannot see the board while editing | moot either way — the sidebar is persistent |

**Orca agrees with the rule underneath.** Its PTYs live in a daemon (tmux's
role) and each terminal is an xterm.js view; switching shows a different view
and never moves the process. Its source holds and coalesces resizes and leaves
hidden terminals at their size, because spurious resizes "garble" a
full-screen program's grid (`src/main/ipc/pty/ipc/resize-visibility.ts`,
`src/renderer/src/lib/pane-manager/pane-pty-resize-hold.ts` in
`stablyai/orca`). **Never resize a pane you are not showing, and show a layout
where it already is** — which is exactly what a sidebar per window does.

### 5c. Decided in the step-4 planning (2026-10-14)

| question | decision |
|---|---|
| groups | *built (M0).* **One `kel` tmux session for everything.** The sidebar groups by repo → worktree from each pane's directory; sessions-per-repo would only duplicate that and make navigation two-level. `group` stays in the records as the repo label (no format change); `.kel/group` keeps working as a display grouping; restore merges old per-repo sessions into the one |
| the panel | **a `display-popup` first** (~90%) for `yazi`, `lazygit`, plan and tasks — no layout cost, nothing resizes. A pinned right-hand pane only if the popup gets reopened constantly. *Revised 2026-10-15, after the author was trapped:* a popup takes every key, prefix included, so when yazi handed them to vim, the title's "q closes" started a vim macro. **Each popup now hosts a small tmux session of its own** (`_kelpop_<n>`, status off, marked `@kel-popup`) and attaches to it; inside, tmux's keys work again and `prefix q` detaches — closing the popup whatever runs there. The `_` name keeps it out of every kel filter (sidebar, fleet, snapshot, restore). yazi and lazygit keep their session between opens; the diff, plan and edit views are made fresh |
| sidebar width | *built (M2).* **Fixed, 32 columns by default**, `sidebar_width` in config, identical in every window. Hide/show applies to every window at once and resizes the user's panes, so it is a rare gesture |
| idle and wake | *built (M2).* **Hook-driven.** Hidden sidebars sleep; `session-window-changed` wakes the shown one; it refreshes when kel's state hooks fire, plus a slow fallback poll. **Next (decided after M2):** format subscriptions (§5a) replace the visible sidebar's 2 s poll on tmux ≥ 3.2; the hooks and `SIGUSR1` stay, as the wake for kel's own state changes, which tmux cannot see |
| restore | *built (M2).* **Rebuild windows and splits as today, sidebars included, under the existing `.restoring` lock.** A snapshot records each sidebar as a `@kel-sidebar` pane (its geometry is part of the window's layout string, so skipping it would break `select-layout`), and restore relaunches it rather than replaying a command — so a restore cannot duplicate one. The author found the old restore hard to work with — the rebuild is a chance to simplify it, not just extend it |
| scope | **only kel's session.** Panes in other tmux sessions are not part of the workspace |
| focus | *built (M3).* **One key to focus the sidebar** (`prefix Space`, taking tmux's `next-layout`); `enter` jumps to the selected pane, `esc` returns where you were. `` prefix ` `` is unchanged |
| keys | *built (M3, M5).* `j`/`k` move · `enter` jump · `/` filter · `n` new agent · `w` new worktree agent · `x` kill (kill-safety intact) · `r` rename · `f` files · `g` git · `d` the worktree's diff · `e` edit what the agent last touched · `o` a server in the browser · `p` plan and tasks · `?` help. None deeper than one key |
| sections | *built (M3).* **`spaces` above `agents`** (§5), after herdr, replacing the single repo → worktree → pane tree |
| mouse | *built (M3).* **First-class.** A click selects and jumps; the wheel scrolls; every gesture a key does on a row, a click can reach. Bubble Tea v2 reports mouse events; tmux passes them to the pane under the pointer with `mouse on`, which kel's config already sets |
| done, until seen | *built (M3).* **`done` stays loud until you have looked at the agent**, then quietens — herdr's semantics, and what makes the dots worth reading. A **flag on `done`, not a new state**: `kel jump`, the notifications and every reader that matches on state are untouched. When "looked at" happened is recorded from the window-change event the sidebar already receives (§9) |
| footer | *built 2026-10-15, after daily use.* **The keys for what is selected, and only while the sidebar has the keyboard.** The author did not want to remember keys and did not want a busier sidebar, so the one footer line teaches them: an agent shows `e edit · p plan · x kill · ?`, a space `f files · g git · d diff · ?`, a server `o open · …`, as many as fit, then `?` for the rest; `enter` is left out as the key everybody tries. Without the keyboard it reads `click, or Ctrl-b then space` — how to get in, with "then" because `Ctrl-b space` read as one chord (2026-10-16) — or, while an agent waits, `1 waiting 6m · click to jump`, the alarm tmux's bar used to carry. Focus comes from the terminal's focus events (kel sets `focus-events on`), else `#{pane_active}` on each tick, and any key or click proves it |
| actions | *built 2026-10-15, after daily use.* **The keys that do not depend on the selection, at the foot of the sidebar, clickable** — `n` new agent · `w` worktree agent · `t` shell · `Q` quit · `?` all keys. Only when the lists leave room; the same keys as everywhere, so one surface still (invariant 6). The author found the lower half empty and the keys hard to recall |
| `kel>` prompt | *built 2026-10-15.* **`prefix k`** runs a typed kel command from wherever you are — the author asked where kel commands are meant to be typed when every pane is an agent. A one-line answer is a message, a longer one a popup, never view-mode on your pane |
| visual language | *built (M3).* §5d. Settled on rendered frames, approved by the author, before M3 was wired to live data; the background is true black (`colour16`), the author's call after `colour234` read as grey |

**Still open** — settled when their milestone is planned, not before:

- ~~The per-worktree Neovim's lifecycle~~ — settled in M5 planning by not
  having one: **kel owns no editor.** `e` uses an nvim you already have open
  in that worktree, else a popup (§5f). With nothing of kel's running, there
  is nothing for `kill` to close, and no unsaved buffer it could lose.
- ~~Whether `agents` repeats what `spaces` lists~~ — settled in M3 planning:
  **agents only under `agents`**; an open space lists the other panes.

### 5d. Visual language  ·  *decided 2026-10-14*

The fear this section answers is a sidebar that works and is unpleasant to look
at. M2's frame — a repo name, one inverse-video row, a key hint — is the
baseline it improves on. The rules, most of which are why herdr looks at home
in any terminal:

- **The terminal's 16 ANSI colours, never hex.** The sidebar then wears the
  user's terminal theme, light or dark, with no theme of its own to maintain.
- **Colour means state, and nothing else.** One colour per state (waiting,
  working, done-unseen, throttled, dead); everything else is the default
  foreground, bold, or dim. A colour that does not mean a state is noise.
- **Name bold, detail dim.** Two lines per entry; the second is always dim.
- **One accent for the selected entry**, and it is not inverse video.
- **Dots, not words, for state.** The word appears only where there is room to
  say *why* (`waiting 6m`), never as the only signal.
- **Whitespace, not box-drawing.** No borders inside the sidebar; sections are
  separated by a blank line and a dim lowercase label (`spaces`, `agents`).
- **Chrome, not a box** — the sidebar's background and border come from tmux
  per-pane styles (§5a), so it reads as part of the window, the way herdr's
  does.
- **Nerd Font glyphs only as an option**, with a plain-Unicode default. They
  break on half the machines you SSH into, and an unknown glyph renders as a
  box — a guess displayed as a fact, in miniature.
- **Unknown is its own look** (invariant 3): dim `?`, never a plausible
  default.
- *Revised 2026-10-15, from the author's screenshots:* the `›` marker went —
  `▌›○` was three marks before a name — and "you are here" is the name in
  bold, others regular. The state word on an agent's second line carries its
  state's colour, grey for calm ones, so the list is quiet unless something
  needs you; uncertainty (`working?`) is grey italic, the whole line. Two
  panes that would read alike (`$ bash`) carry their window's name. A blank
  line keeps the footer from reading as one more action. Bar tracks are a
  solid grey block, not `░` (a checkerboard in the author's font), in both
  the tmux bar and Claude's row; the tmux bar is black, with only the time on
  its right; a window with one pane of yours has no pane title

### 5e. How we know the sidebar is good  ·  *decided 2026-10-14*

Green tests are not sign-off (the author verifies UX by hand), but they can
make sure a frame the author approved stays approved.

- **Design frames before wiring.** M3 starts from fixture fleet documents —
  none, one agent, fifteen across three repos, a 60-character branch, unknown
  context, a dead agent, servers with ports — rendered at 28, 32 and 40
  columns. The frames are iterated with the author until they are right;
  only then is the sidebar connected to live data.
- **Golden frames.** Those approved frames are committed, and `View()` over
  each fixture is compared against them (`teatest`, or the same direct
  rendering `kel top`'s tests use). An accidental visual change shows up as a
  diff to accept or reject.
- **Real frames from real tmux.** `tmux capture-pane -e -p -t <sidebar>`
  inside the suite's isolated server is the pane as tmux actually drew it,
  colours included — what catches a style that is right in `View()` and wrong
  in the terminal.
- **Timing budgets, asserted:** a window switch shows the correct sidebar
  within ~50 ms; a keypress produces a frame without waiting on git; a hidden
  sidebar does no work. *Measured in M3:* the switch reaches the shown sidebar
  in ~130–140 ms, most of it `kel _sidebar wake` starting bash; every
  sidebar already knows its own window, so the frame is right before the
  fresh load lands. The suite asserts under a second so CI cannot flake;
  format subscriptions (§5a) are what would close the gap.
- **Recorded, and compared honestly.** A VHS tape (`charmbracelet/vhs`) drives
  a real session and exports a GIF or PNG per milestone, to be put beside
  herdr's own screenshots.

### 5f. Editor, diffs and the browser  ·  *built (M5)*

What the desktop ADEs (Orca, Superset, Conductor, T3 Code) build in, kel gets
by deciding where existing tools open — invariant 7.

- **Editor: your nvim, else a popup** *(decided in M5 planning, replacing a
  kel-owned nvim per worktree)*. Every nvim since 0.9 listens on a socket of
  its own (`v:servername`, `nvim.<pid>.0` under `$XDG_RUNTIME_DIR` or the temp
  dir). So `e` looks for an nvim already running in a pane of that worktree,
  hands it the file with `nvim --server <sock> --remote` — the editor's RPC,
  not typing into a pane, so invariant 4 is untouched — and selects that
  pane. Finding none, it opens nvim on the file in a popup, gone when you quit
  it. kel owns no editor, so it never has to decide where one lives or when
  `kill` may close it. The point is `e`: a `PostToolUse` hook on `Edit` /
  `Write` records the last file each agent touched (§9), and `e` opens it, at
  the line. The IDE features — tree, signs, search — are Neovim plugins the
  user already chose.
- **Diffs: `d` reviews a worktree** against its land base — committed and
  uncommitted, what the agent changed — `nvim -c "DiffviewOpen <base>"` when
  `diffview.nvim` is installed, else `git diff --merge-base <base> | delta`
  (or plain `git diff`) in a popup. No base reads `no base — can't diff` in
  the footer, never an empty diff (invariant 3). Land state (§9d)
  already knows the base; that is the whole contribution. A diff renderer is
  years of work kel would do worse (R5).
- **Browser: `o` opens a server pane's port** in the real browser —
  `$BROWSER`, then `open` (macOS), `xdg-open`, `wslview`, and on WSL2 without
  either, `cmd.exe /c start` (WSL forwards `localhost`). M1 already knows
  every pane's ports. Several ports open the lowest and list the rest in the
  footer (`opened :3000 · also :3001 :9229`) — the extras are often a
  debugger. *(Decided in M5 planning.)* A browser *inside* the terminal (`carbonyl`) is rejected
  (backlog R7).
- **`kel url [worktree]`** prints that worktree's server URL, so an agent's
  own browser tooling (a Playwright skill, say) can ask kel where the dev
  server is instead of guessing `:3000`. Read-only — the agent asks, kel never
  tells it unprompted (R2).

---

## 6. Keys

The sidebar's keys are decided in §5c. Today's bindings are listed in
`usage.md` — one list, so the two cannot drift.

Two rules hold across both. **Switching is one keystroke** — never
open-menu, find-row, press-enter. And **`` prefix ` `` is the key the tool
exists for**: not "go to window 3" but "go to whoever is blocked on me",
across every repo; it matches `waiting` only, cycles and wraps.

---

## 7. The two bottom rows

There are two, with different jobs, and the sidebar (§5) takes the
per-window list both used to carry.

| where | drawn by | scope |
|---|---|---|
| Claude Code's own row, inside each agent pane | `kel statusline` | **this session** — what you need while typing to it |
| tmux's bottom bar | `kel status-line` | **the account and the workspace** — what belongs to no single pane (Orca's footer) |

**Every fact has one home** *(decided 2026-10-14)*. The first draft of this
section put land state in three places — Claude's row, the bar and the sidebar
— and fleet counts, ports and location in both the bar and the sidebar. Each
fact now lives where you are looking when you need it. Nothing was dropped;
facts moved.

| fact | its one home | the draft also had it in |
|---|---|---|
| model, context bar, cost, `fast` / `xhigh` | **Claude's row** — you are typing to that session | — |
| repo / worktree, branch, `↑N`, dirty, land | **sidebar**, per space | Claude's row; the bar's location |
| agent state, per-agent context % and cost | **sidebar**, per agent | — |
| listening ports | **sidebar**, on the space whose server holds them | the bar's machine segment |
| pane count | **sidebar** — it *is* the list | the bar |
| quota: 5 h with reset, weekly | **the sidebar's last line** — per account, belongs to no pane (the bar's, while the sidebar is off) | Claude's row |
| total cost of live sessions | the bar, while the sidebar is off — an aggregate has no row; with the sidebar, each agent's own cost | — |
| waiting count, oldest wait | **the sidebar's footer** while it lacks the keyboard — the alarm, a click jumps (the bar's, while the sidebar is off) | (the sidebar shows each dot) |
| keys, the restore hint | **the sidebar's footer** | startup text |

**tmux's bar is off while sidebars show** (2026-10-16): two status lines
stacked under every agent — the bar, and Claude's row — read as one repeated
thing, so kel sets the session's `status off` with the first sidebar and
`kel sidebar off` unsets it.

**When the sidebar is hidden, the bar takes location back.** Hiding is one
global gesture (`kel sidebar off`, which leaves `sidebar-off` in the state
directory), so the bar checks that one file and appends the focused pane's
repo, branch and land code while it exists. A fact never disappears; it only
moves to where there is room.

### 7a. Claude's row  ·  *built (M4)*

```
Opus 5.5 · ████░░ 36% ctx · $10.65
```

- **the session** — model, context bar, cost; `fast` / `xhigh` only when not
  default; `ctx ?` when unmeasured. That is all: this row is read while typing
  to one agent, and everything else on it was a copy.
- *Moved out:* location and land to the sidebar; quota to the bar. Both were
  in the first draft of this row. Dropping them also drops the cached git
  position this row needed — it runs on every update and could never run git.
- **Outside kel it keeps them** *(decided 2026-10-14, M4 planning)*. One home
  is a rule *inside* kel, where the sidebar and the bar exist. Claude Code run
  in a plain terminal has neither, so there the row is the only place either
  fact can live: it keeps the payload's repo@worktree identity (no git — it is
  in the JSON) and the quota chips it shows today. "Inside kel" is what the
  row already knows: it found a kel window to label. The `quota` file is
  written either way — it is the account's, wherever it was observed.

### 7b. tmux's bottom bar  ·  *built (M4)*

```
claude ███░░ 58% (1h20m) · wk 41%  │  1 waiting 6m · $24.10
```

With nothing waiting, and with the sidebar hidden:

```
claude ███░░ 58% (1h20m) · wk 41%  │  $24.10
claude ███░░ 58% (1h20m) · wk 41%  │  1 waiting 6m · $24.10  │  kelsier · fix-auth ↑2 ●3
```

1. **quota** — the 5-hour window as a bar and percentage with its reset time,
   and the weekly window. No `5h` label: the window is always five hours, and
   the weekly is the only other one Claude Code reports. Always shown when
   known; API-key sessions have none and show nothing, never a fake `0%`.
2. **the alarm** — how many agents wait on you and for how long the oldest has
   waited; absent when none do. Then the total cost of live sessions
   (cumulative per session, so "spent by open sessions", never "today").
3. **location** — only while the sidebar is hidden: the focused pane's repo
   and branch position — `↑N` unpushed, `●N` uncommitted — from one `git
   status` of that pane's root. *Not* the land code (decided in M4 planning):
   `review` / `merged` / `no PR` need the base branch and the PR cache, which
   do not belong on a path that redraws constantly.
4. **spend** — the spend limit, after the weekly window, **only from 50%**:
   account-wide like the others, and almost always irrelevant.

Cut from the first draft, each because the sidebar holds it: per-state counts
(`4 agents · 2 done`), pane count, the port list, and location while the
sidebar is showing. Memory was considered earlier and dropped: it has never
been the problem.

The per-window list (`1:auth-fix? 2:docs`) is gone — the sidebar replaced it,
and it was what overflowed the bar past about six windows. With live agents
and nothing to say, the second segment reads `nothing waiting` rather than
going blank: an empty bar would look like a broken one.

**Stale quota reads as unknown.** Quota is per account, so it moves out of the
per-window `.ctx` into its own small file (5h %, its reset, weekly %, its
reset, the epoch it was observed) — an on-disk addition, recorded in
`CLAUDE.md`'s contract table when built. If the 5h reset has passed since the
last observation, the bar shows `░░░░░ ?`, never the old figure (invariant 3).
Same for the weekly. It is written only from a payload that carries rate
limits — an API-key session never writes one — and `kel-fleet` reads it into
the fleet document as `quota`, so the bar stays one `jq` over one document.

**Total cost** is the sum of the live agents' known costs. An agent with no
figure yet has made no measured call, so it adds nothing rather than an
unknown.

**Ports are too slow for the hot path** — on macOS they need `lsof`, ~100 ms
and up — which is one more reason they live in the sidebar, off the bar's
redraw path (invariant 5). `kel-fleet` computes them on a ~10 s cache.

**Narrow terminals** mostly stop being a question: the bar is two short
segments. If it still overflows, location drops first, then cost; quota and
the alarm stay.

### 7c. How it updates

`tmux`'s `#()` only re-runs on the status interval, so every state hook calls
`tmux refresh-client -S` after writing — but only when the rendered value
actually changed (invariant 5).

---

## 8. The agent record

An agent is one `tmux` window and one working directory. kel-managed agents
get a metadata record; a hand-made `prefix c` window is tracked for state but
has no record and shows as `(unmanaged)`. The sidebar
(§5) lists every pane, managed or not.

`~/.local/state/kel/sessions/<group>/<name>.json`:

```
name           slug, the tmux window name
window_id      tmux window id, e.g. @7 — stable; state is keyed by this, not name
repo           the origin repository (may be empty)
cwd            where the agent runs
isolation      inplace | worktree   (kel ls renders inplace as "repo")
branch         worktree only
agent          command run in the window (default: claude)
group          repo basename (or --group, or misc); tmux session is kel/<group>
claude_session Claude Code's session id, recorded by the hook — for exact restore
compactions    PreCompact count
created_at     timestamp
```

Records are keyed by **group and name** — two repos may each have a `docs`. A
bare `kel kill docs` resolves to the group you are standing in, then to a
unique match anywhere; when ambiguous it lists the candidates and asks for
`kel kill <group>/<name>`. Older record shapes are migrated by `migrate_meta`.

**Isolation is opt-in.** Default `inplace`: the agent edits your checkout.
`-w` creates a linked worktree on a new branch. After creating one, `kel new -w`
runs the repo's `.kel/setup` if present (env `$KEL_REPO`, `$KEL_WORKTREE`). It
must only create gitignored paths — anything git would report as a change
blocks `kel kill`. Example in `examples/kel-setup`.

---

## 9. State detection

State comes from the agent's own hook system, never from scraping its output.
For Claude Code, `install.sh` wires these hooks, each calling `kel hook <EVENT>`
with the payload on stdin:

| Hook event | State |
|---|---|
| `SessionStart` | `idle` |
| `UserPromptSubmit` | `working` |
| `Notification` | by `notification_type` — below |
| `Stop` | `done` |
| `SessionEnd` | state file removed |
| `PreCompact` | no change — increments `compactions` |
| `PostCompact` | no change — redraws the bar |
| `PostToolUse` on `Edit` / `Write` | *built (M5).* no change — records the last file the agent touched and its first changed line (`tool_response.structuredPatch[0].newStart`), for `e` (§5f). *Built (M6):* a write under `~/.claude/plans/` is the session's plan, recorded as `<window-id>.plan` instead |

The `PostToolUse` entry is wired with a **matcher limited to those tools**:
`PostToolUse` fires after every tool call, so an unmatched hook would put a
`kel` start on the agent's hottest path (invariant 5). It reads the tool's own
payload — the file path is in it — so it never scrapes a transcript.

**Plan and tasks** *(measured 2026-10-15 on Claude Code 2.1.300; built,
M6)*. The first design said a `PostToolUse` hook on `TodoWrite` would
carry the task list. Measured with a headless session and a hook that logged
every payload, both halves of that were stale:

- **`TodoWrite` is gone.** Since 2.1.142 the task tools are `TaskCreate` /
  `TaskUpdate` / `TaskGet` / `TaskList`, and since 2.1.268 newer models —
  Opus 5.5 among them — do not get them by default at all: most of the
  author's sessions keep no task list. kel never turns them on
  (`CLAUDE_CODE_ENABLE_TODO_TOOLS` would alter the agent — not a harness).
  When a session has them they do fire `PostToolUse` (`TaskCreate` returns
  `{task:{id,subject}}`; `TaskUpdate` takes `{taskId,status}`), and Claude
  Code keeps the whole list on disk as `~/.claude/tasks/<session id>/<id>.json`
  — `subject`, `status`, `blockedBy`. kel **reads that directory** for the
  agent's `claude_session` rather than rebuilding the list from hook events: it
  is exactly the current list, with no state machine to keep in step. It is
  undocumented, so a file that does not parse makes the list read
  `tasks unreadable`, never an empty list (invariant 3). No directory means
  the session keeps no list, which is the common case and says so.
- **The plan needs no new hook.** In plan mode Claude writes its plan with the
  ordinary `Write` tool to `~/.claude/plans/<generated name>.md` — which the
  M5 hook already sees. So the generated name never has to be tied to a
  session after the fact: the write itself names the window. Recording it as
  the plan rather than the last edit also fixes `e`, which would otherwise
  open the plan as "the file the agent last touched".

**Claude Code's own view** *(built 2026-10-16)*. `claude agents --json` lists
Claude Code's sessions with a busy/idle status — interactive ones, and
background ones (`claude --bg`) that run in no pane. On the sidebar's path
only (a node start, ~0.7 s, cached 10 s in `claude-agents.json`), kel matches
each interactive session to its window by walking up from its pid to a pane's,
and lets it correct the hooks where they cannot know: `working` + idle →
`idle` (an interrupt fires no Stop — the "working 15h" agent), `idle`/`done`
+ busy → `working`. `waiting` is never overridden. Background agents are
listed (`bg`), nested under the top agent of the same name — Claude Code does
not say which agent dispatched one — and `enter` opens one with `claude
attach` in a popup. Unreadable (no claude, a timeout) is null: hooks alone.

**Subagents** *(built 2026-10-16)*. `kel substatus` is Claude Code's
`subagentStatusLine`: run every refresh tick while the agent panel shows
subagent rows, all rows in one payload (measured: id, label, agentType,
status). It records `<window-id>.subs` and prints nothing — which, per the
docs, keeps Claude Code's own rows as they were. The sidebar nests them under
their agent (`└ ● Reading parser.go · Explore`); a file older than 30 s is "no
subagents now", since the panel stops calling once it is empty.

**Done, until seen** *(built, M3)*. `done` carries a flag: unseen until the
agent's window is shown, then seen. The sidebar records the moment from the
window-change event it already receives, as an epoch next to the state file;
`done` is unseen while the state's epoch is newer than the last time its
window was shown — a tie in the same second reads unseen, since the order is
unknown and the sidebar showing it re-marks it on its next load. Only the
sidebar's look changes — `kel jump`, notifications
and every reader that matches on state see plain `done`, as today.

| `notification_type` | state |
|---|---|
| `permission_prompt`, `agent_needs_input`, `elicitation_*dialog` | `waiting` |
| `agent_completed` | `done` |
| `quota_auto_resume_*` | `throttled` — waiting on quota, not on you |
| `idle_prompt` | no change — except a `working` becomes `idle`: an interrupt fires no `Stop`, and an agent read "working 13h29m" at its prompt (2026-10-15) |
| `auth_success`, `elicitation_complete/response` | no change |
| anything added later | `waiting` (safe default) |

`notification_text` is kept as the third field of the state file — the literal
permission prompt — so "why is this blocked" needs no pane scrape. `kel jump`
matches `waiting` only.

`kel hook` resolves which window it is in, in order: `$TMUX_PANE`; the pane
stashed by this session's `SessionStart`, keyed by session id; the single pane
whose current path equals the payload's `cwd`.

**State file.** `~/.local/state/kel/<window-id>.state`, one line
`<state> <epoch> <note>`, written atomically. Keyed by window id because tmux
auto-names every `claude` window `claude`.

**Dead agents.** A crash fires neither `Stop` nor `SessionEnd`, so the file
keeps saying whatever it last said. kel catches this at *read* time: if the
record says `idle` / `working` / `waiting` / `throttled` but the window's only
live process is a bare shell, the effective state is `dead`. This check lives
in two places kept in sync by hand — `effective_state()` in `bin/kel` and
`internal/fleet/fleet.go` — until `bin/kel`'s remaining readers move to Go.

### 9a. Context and cost

Hook payloads carry no token, context or cost data. Claude Code's
**`statusLine`** does — a documented, versioned interface, not a transcript
scrape. `kel statusline` records one line per agent window to
`<window-id>.ctx`:

```
<pct> <cost_usd> <in_tokens> <ctx_size> <rate_5h> <epoch> <model>
```

Updates are debounced to 300 ms and an in-flight script is **cancelled** when
the next arrives, so the handler is one `jq`, one atomic write, and a
`refresh-client -S` only when the displayed integer changes. Writes are gated
on a non-zero `context_window_size`.

**`used_percentage` is `null`, not `0`,** before the first API call and right
after `/compact`. The row shows a dim `ctx ?`, and the unmeasured case
declines to overwrite a real figure.

**`agent.name` does not mean "subagent".** It marks a session started with
`claude --agent`. Task-tool subagents report through `subagentStatusLine`; no
per-subagent cost is exposed anywhere.

The row Claude Code shows (repo identity, `250k/1M`, quota resets, `fast` /
`xhigh`) is rendered from the live payload and persisted nowhere — see
`usage.md`.

### 9b. Notifications

When an agent **transitions into** a state listed in `KEL_NOTIFY` (default
`waiting`) and you are **not looking at that window**, kel notifies:
`tmux display-message`, **the bell** on every terminal attached to kel, and
**a desktop notification** — a Windows toast from WSL (built-in WinRT via
`powershell.exe`), `osascript` on macOS, `notify-send` on Linux.
`$KEL_NOTIFY_CMD` replaces the desktop half. Hook-driven, never polled — a
poller would be a daemon.

*Revised 2026-10-15.* kel used to ship no platform detection and leave
recipes in `setup.md`; in practice nothing reached the author outside the
terminal, while both tools kel was measured against (herdr, Orca) tell you
when an agent needs you. The text is untrusted (the agent quoting a command):
it crosses into PowerShell as base64 and into AppleScript as arguments. The
bell is the terminal's own — the "screen flash" Claude Code itself uses on a
Mac. On WSL, the PowerShell path is stat'ed, never looked up on `PATH` (the
76 ms trap).

### 9c. The fleet document

`kel _fleet [--dirty] [--land] [--pr]` is the whole fleet, computed once, as
one JSON object: `{generated_at, current:{group,window_id}, agents:[…]}`.
`kel ls`, `ls --json`, the bar, the completions and `kel top` all read it.
Adding a field means adding it there once.

Since v0.10 it is produced **only** by the Go `kel-fleet`; the bash reader it
was ported from, and the differential test that held them together, are gone.
A missing `kel-fleet` fails loudly — an empty document would read as "no
agents", which is a guess reported as a fact. The sidebar will need fields the
document lacked; they went here, not into the sidebar: **`--panes`** *(M1)*
adds a `panes` array — every pane in every kel window, with `kind` (`agent`
from the window's record or a `claude` process; `server` if anything in its
process tree listens on TCP; `shell`; else `program`), `ports`, and `git`
(root, linked worktree or main checkout, branch, upstream, ahead/behind,
dirty — one `git status --porcelain=v2 --branch` per repo root). Ports come
from `/proc` on Linux and WSL2 with no fork, and from one `lsof` + `ps` on
macOS, cached 10 s. Unknowns stay null: no ports scan means `ports: null`, not
`[]`; no client attached means no pane is `current`.

`--dirty` is opt-in because `git status` per worktree is the expensive part
and the bar never shows it.

### 9d. Land state

`land` answers *what would it take to land this branch?* as one code naming
the next action.

| code | means | you |
|---|---|---|
| `dirty N` | uncommitted work | commit it |
| `unpushed N` | commits only you have | push |
| `merged` | its PR landed, or HEAD is in the base | sweep it |
| `review` | PR open | wait, or nudge |
| `behind N` | base moved under you | rebase |
| `no_pr` | pushed, nothing opened | open one |
| `clean` | nothing to say | — |
| `unknown` | `gh` could not answer the PR half | — |

Precedence is that order. `merged` asks **git first** (ancestry); `gh` only
adds the squash-merge case. So the column degrades without a token rather than
going dark. PR state is **one request per repo**, cached under `pr-cache/` for
`KEL_PR_TTL` (default 300s); only `--pr` may refresh it, and a repo whose
worktrees are all dirty or unpushed is never refreshed at all.

---

## 10. Commands

The command list is `usage.md` (and `kel --help`, which the suite checks
against the dispatcher and both completions). What belongs here is the design
two of them carry, and one that is designed:

**`kel url [worktree]`** *(built, M5)* prints the URL of the server
listening in that worktree (or the one you are standing in) — read from the
fleet document's ports, never by probing. Several servers print one line each;
none prints nothing and exits 1, so a script can tell "no server" from a URL.
It is the first of a small set of **read-only commands an agent may call**;
the rest (`kel state`, `kel wait --state`) wait in `backlog.md` #39.

**Workspace snapshot.** `kel snapshot` (fired by tmux hooks and by
state-changing commands) writes the full shape to `snapshot.json`, keeping one
`.prev`. A restore holds a `.restoring` lockfile so the hooks it trips cannot
overwrite the file it is reading (invariant 2).

**Restore is by choice, like `claude --resume`** *(built in M0).* Until v0.10, `kel` put a rebuild prompt in front of you whenever a
snapshot existed and the server was down — every first `kel` after a reboot,
whatever you came to do. The author's account: they never quite understood
what it was asking and were just trying to move on. So:

- **`kel` always just opens.** No prompt, ever.
- **`kel restore`** rebuilds the most recent previous workspace, resuming
  conversations by default (resuming is why you restore; the old `-c` becomes
  the default). **`kel restore --list`** picks an earlier one.
- **A passive hint, not a question:** when an unrestored workspace exists, the
  startup text says so once — `previous workspace: 3 windows · kel restore` —
  and later the sidebar footer can carry it.
- **Starting fresh must not destroy the old workspace.** The snapshot hooks
  rewrite `snapshot.json` within seconds of anything changing, and `.prev` is
  one generation. So when `kel` starts with no server and a snapshot exists, it
  first moves it into `snapshots/<epoch>.json` (the newest five kept), and
  `kel restore` reads from there. Invariant 1's spirit, applied to layouts.
- The declined-snapshot machinery (`snapshot-declined`, the `n` / `d` answers)
  is deleted: with no question there is nothing to remember declining.

**`kel kill` on a worktree refuses** when there are uncommitted or unpushed
changes, and names what is at risk. Deleting an agent's only copy of its work
is the one unforgivable bug in this category of tool (invariant 1).

---

## 11. Capability probe

`kel doctor` prints pass/fail, with a fix for each failure, and caches JSON to
`doctor.json`. Exits 1 if `tmux`, a modern `tmux`, `jq` or `kel-fleet` is
missing.

| Probe | Why |
|---|---|
| `tmux` present, `>= 3.0` | the substrate |
| `display-popup` available | `kel top`, `kel config` |
| `git worktree` available | `kel new -w` |
| `jq` present | metadata, hooks, the installer's settings merge |
| `node` on `PATH` | Claude Code needs it; catches a broken WSL PATH |
| `claude` on `PATH` | the default agent |
| `gh` authenticated | PR state for `kel sweep` and the LAND column |
| kel hooks wired in `settings.json` | state detection |
| kel `statusLine` wired | context % and cost, per agent |
| `kel-fleet` built | the only fleet reader since v0.10 |
| `kel-top` built | `kel top`, and the sidebar after it |
| the wired kel path resolves | `install.sh` records an absolute path into the clone; moving it silences every hook |
| hooks not disabled by managed settings | `allowManagedHooksOnly` / `disableAllHooks` stop hooks while the entries remain |
| `allow-passthrough on` | Neovim OSC 52 clipboard through tmux |
| clone level with its upstream | `kel update` has something to apply |

The upstream probe does **not** fetch. A probe that needs the network is one
people stop running.

### 11a. Updating, and releases

`install.sh` symlinks `bin/kel`, the completions and `tmux/kel.conf` out of the
clone, so the working tree **is** the installed artifact; `kel-fleet` and
`kel-top` are built into `~/.local/bin`. `kel update` fast-forwards the clone
and re-runs `install.sh`. It refuses rather than merges — on a dirty tree, a
detached HEAD, no upstream, or a non-fast-forward.

Until v0.9 this section said releases and tags were "the wrong mechanism":
there was no artifact the symlink did not already point at. **Go being required
changes that.** A machine without a Go toolchain now cannot install kel from a
clone, and prebuilt `kel-fleet` / `kel-top` binaries attached to a tagged
GitHub release are the fix. Versioning policy is in `rollout.md`.

---

## 12. Stack

- **Go** for everything that reads or renders: `kel-fleet` (the fleet
  document), `kel-top` (becoming the sidebar). Bubble Tea **v2**
  (`charm.land/bubbletea/v2`) — almost every tutorial online is v1 and will
  not compile.
- **Bash** for the hooks and the installer, permanently: a hook must start in
  milliseconds with nothing to build, and the installer runs before anything
  is built.
- **`bin/kel` is transitional.** Its mutating commands (`new`, `kill`,
  `restore`, `sweep`, …) are still bash and move to Go as the sidebar needs
  them — `kill`'s refusal logic last, with its tests ported first.
- **State:** `~/.local/state/kel/`, mode `0700`. tmux is the source of truth
  for what is alive; the files hold what tmux does not know. The format is the
  interface, not the language — see *The on-disk contract* in `CLAUDE.md`.
- **Config:** `~/.config/kel/config.toml`, overridden by a per-repo
  `.kel/config.toml`, overridden by an exported `KEL_*` env var. A flat
  `key = value` subset of TOML; `examples/config.toml` lists every key.

### 12b. What kel keeps, and what leaves the machine

**kel makes exactly one network call**: `gh api repos/<slug>/pulls`, for the
two land codes local git cannot answer. Three things narrow it:

1. no slug unless `origin` is on `github.com`;
2. local git answers first — a repo whose worktrees are dirty or unpushed is
   never asked about (enforced in `kel-fleet` since v0.10; before that only
   the bash reader kept this promise);
3. `pr = "off"` disables it outright (likewise only honoured by bash until
   v0.10).

Nothing else leaves the machine. kel does not wrap Claude Code, so your code
reaches Anthropic by exactly the path it would from a bare terminal.

**What it keeps** is in the `0700` state directory. The sensitive part is
`<window-id>.state`, which stores Claude Code's notification text — the agent
quoting a command back at you. **Agent output is read but never stored**: `kel
top` shows the last line of each pane and drops it. `$KEL_NOTIFY_CMD` is the
one place untrusted text crosses a boundary, so `setup.md`'s recipes escape it.

### 12c. A desktop app, later

Agreed direction, not scheduled: the terminal version stays a thin reader of
the state directory and the fleet document, so a GUI (the Orca shape, as an
app) can be a second reader of the same files without kel growing a server.
Nothing in the terminal work may make that harder — in practice: new data goes
into the fleet document, not into the sidebar's private state.

---

## 13. On making it public

Reasonable, eventually. The space is crowded and moves fast. The real risk is
not obscurity but drift — users arrive with requests and the tool becomes
everyone's instead of yours. The non-goals are the defence, and they are in
the README. MIT. Build it for you; if it catches, that is information, not an
obligation.

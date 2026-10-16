# kelsier — rollout

The build order and the current status. **This file is the source of truth for
what to work on next**; `spec.md` section numbers are not a build order.
`backlog.md` holds candidates that are not on the path yet, each with a gate.

The constraint has always been **"will I actually adopt this,"** not "can it
be built." v0.10 exists because, by that measure, v0.9 failed: the author built
it and did not love using it.

---

## Versioning, plainly

kel uses `MAJOR.MINOR.PATCH` with the usual meaning for a `0.x` project:

- **`0.x` means "still changing; anything may break."** It carries no promise,
  and nothing requires 1.0 to follow 0.9. After `0.9` comes `0.10`, `0.11`, …
  — as many as it takes. A redesign is exactly what `0.x` is for.
- **A minor bump (`0.10` → `0.11`)** marks a usable step: something you would
  install and daily-drive. **A patch (`0.10.0` → `0.10.1`)** is a fix to it.
- **`1.0` means "I will stop breaking things"** — the on-disk format, the
  commands and the keys are stable enough that someone else can rely on them.
  It is a promise, not a size.
- **A release is a tag plus notes.** `git tag -a v0.10.0 -m "…" && git push
  origin v0.10.0`, then `gh release create v0.10.0 --generate-notes`. Once
  someone other than the author installs kel, attach prebuilt `kel-fleet` /
  `kel-top` binaries to the release (GoReleaser, or a CI job that
  cross-compiles `GOOS=linux,darwin` × `GOARCH=amd64,arm64`) — Go being
  required means a machine with no toolchain otherwise cannot install it.

Tags so far: **`v0.9.0`**, cut 2026-10-14 as the marker for the old design
before the pivot. The next tag is `v0.10.0`, when the sidebar is usable.

---

## History, condensed

Everything up to the pivot, in full, is `git show v0.9.0:docs/rollout.md`.

| version | what it added |
|---|---|
| v0 | the status bar as the fleet; `` prefix ` `` jump-to-blocked |
| v0.1 | `kel new` / `kill` / `ls`; kill refuses to lose work |
| v0.2 | grouping by repo (one tmux session per repo) |
| v0.3 – v0.4 | the fzf board; a learnability pass; dead-agent detection |
| v0.4.1 | `kel statusline` — context % and cost without interrupting the agent |
| v0.5 | notification fidelity, compaction counter, relaunch-in-place, fleet notifications; macOS fixes; CI on Linux + macOS |
| v0.6 | the fleet document (`kel _fleet`); Go enters (`kel-fleet`, `kel top`) |
| v0.7 | land state, `kel sweep`, `.kel/group` |
| v0.8 | `install.sh --uninstall`, `config.toml`, the README |
| v0.9 | pane-border state, a clarity pass, `kel adopt`, `kel relaunch`, `0700` state dir, the Mac install |

What carries forward unchanged: the state directory and its format, the hooks,
the fleet document, kill-safety, worktree creation, snapshot/restore, the
statusline adapter. What did not: every interactive surface.

---

## v0.10 — the pivot  ·  *in progress*

**Theme:** one surface. An Orca-shaped workspace in the terminal: a persistent
sidebar in every window listing every pane, your own splits left untouched, IDE views (files,
git, plan, tasks) in a panel. Design in `spec.md` §5.

### Step 1 — tag the old design  ·  **done**

`v0.9.0`, pushed. Every deleted surface is recoverable from it.

### Step 2 — rewrite the docs to the new design  ·  **done**

`spec.md`, this file, `usage.md`, `backlog.md`, the README and `CLAUDE.md`
rewritten. `interface.md` and `interface-build.md` deleted — they designed the
TUI pass over the old board, which no longer exists.

### Step 3 — delete the old surfaces  ·  **done** (`622ae90`)

- the fzf board, both menus, `kel cheat`, the popup runner and sweep UI
- `kel go` / `move` / `adopt` — groups-as-sessions is on its way out, and the
  sidebar lists every pane, so adopting one means nothing
- the bash fleet reader and its land/PR helpers. **Go is now required**;
  `install.sh` fails without it, and a missing `kel-fleet` fails loudly
- the keymap file and the usage counter (it existed to gate this very pass)

`bin/kel` went from 3,247 lines to about 1,950. Moving the bash-only tests onto
`kel-fleet` found two real bugs, both fixed with tests that fail without the
fix: `kel-fleet` ignored `pr = off`, and it called `gh` for repos that local git
had already answered.

### Step 4 — the build plan  ·  *M0–M6 done*

Agreed 2026-10-14. Each milestone ends with something to try, and the suite
stays green throughout. The decisions behind it are `spec.md` §5c and §7.

**M0 — one session, and restore by choice**  ·  **done**
- `kel new` puts every window in a single `kel` session; `group` stays in the
  records as the repo label (no on-disk format change)
- the bar, `kel` (attach) and `prefix g` simplify — `prefix g` goes
- `kel` never prompts: a snapshot from a dead server is archived to
  `snapshots/<epoch>.json` (newest five kept) and the workspace opens fresh,
  with a one-line hint (`spec.md` §10)
- `kel restore` rebuilds the newest archived workspace, conversations resumed
  by default; `--list` picks an earlier one; a v0.9 multi-session snapshot
  restores into the one session
- the declined-snapshot file and its prompt answers are deleted
- *try:* reboot, run `kel` — it just opens; `kel restore` brings it all back;
  `prefix 1..9` reaches every agent
- *tests:* session counts adapted; `kel` with a snapshot present opens without
  prompting; the old workspace survives a fresh start and `kel restore`
  brings it back; `--list` shows the archive; the archive caps at five; a v0.9
  multi-session snapshot restores into one session
- *found on the way:* `kel rename` and `kel relaunch` asked tmux for "the
  current window" rather than the window of the pane they ran in — correct by
  luck while each repo had its own session, wrong as soon as two shared one.
  Both now target `$TMUX_PANE`. And outside tmux, `#{@kel-group}` resolves
  against tmux's default window, which made `kel kill <name>` guess a group
  instead of reporting the ambiguity; `current_group` now answers only from
  inside tmux.

**M1 — the fleet learns every pane**  ·  **done**
- a `panes[]` list in the fleet document: window, kind (agent / server /
  shell, from the process tree), command, cwd, repo, worktree or main
  checkout, branch, ahead/behind, dirty, listening ports
- git position computed once per repo root; ports cached ~10 s. The
  git-position cache for Claude's row moved to M4, where its consumer is
- *try:* `kel _fleet | jq .panes` shows everything open
- *tests:* Go unit tests for classification; suite assertions over real panes
  (a `sleep` agent, a `python3 -m http.server` for a port, a plain shell)

**M2 — sidebar plumbing**  ·  **done** — *the author tries it here, rough*
- `kel-top --sidebar`, a minimal list
- `kel new` and an `after-new-window` hook add a 32-column sidebar pane to
  every window, marked `@kel-sidebar`, without stealing focus
- hidden sidebars sleep; a hook wakes the shown one; view state shared through
  the state directory
- snapshot records the sidebar as a marked pane and restore relaunches it
  (its geometry is part of the window's layout string, so skipping it would
  break `select-layout`); `kel ls` and the dead-agent check ignore it
- *try:* every window has the column; switching shows the same thing
- *tests:* a sidebar in new and hand-made windows; three restore cycles keep
  the snapshot intact (invariant 2) with no duplicate sidebars; hidden
  sidebars do not poll
- *found on the way, each now tested:* the sidebar would have kept a dead
  agent reading alive, made relaunch refuse forever and type into kel-top,
  and made `resolve_pane`'s directory match ambiguous; the first `reap`
  closed every window; the first `ensure` (an `if-shell` with an empty
  branch) and — since long before v0.10 — the `after-split-window` snapshot
  hook both left the active pane in view-mode, swallowing keys; the sidebar
  panicked when a resize squeezed it to one column; and the suite was loading
  the developer's own `~/.tmux.conf` whenever kel restarted its server

**After M2 — the design pass** (2026-10-14). Before planning M3 the author
looked at the field — Orca, Superset, Conductor, T3 Code and, closest to kel,
herdr — and settled what to take: `spaces` / `agents` sections, `done` until
seen, the mouse as first-class, tmux per-pane styling and format
subscriptions, a visual language and a way to hold it (`spec.md` §5a–§5f), one
home per fact across the two bottom rows (§7), and the editor / diff / browser
embeds (§5f). What not to take is `backlog.md` R7–R11. M3 onward below is
re-planned against those, and each milestone is still planned with the author
before code.

**M3 — the sidebar itself**  →  **tag `v0.10.0`**  ·  **done** — *tagging is the author's call, after daily use*

*M3a — frames first.*  ·  **done.** Nine fixtures, reviewed with
`test/sidebar-frames.sh`, approved, and held as golden files (rewrite with
`go test ./cmd/kel-top -run Golden -update` only after looking). Judged live
too: the first background, `colour234`, read as grey and became true black
(`sidebar_style`).
- fixture fleet documents (none, one agent, fifteen across three repos, a
  60-character branch, unknown context, a dead agent, servers with ports)
- the `spaces` / `agents` layout and the visual language (`spec.md` §5d),
  rendered at 28 / 32 / 40 columns and iterated with the author until they
  are right
- the sidebar's tmux styling: its own background and border, the accent
  border on the pane you are in (§5a)
- *try:* look at the frames, beside herdr's
- *tests:* the approved frames become golden files

*M3b — wired.*  ·  **done.**
- live data loaded off the key path (a `tea.Cmd`), and the last document left
  in `sidebar-cache.json` so a sidebar just shown draws at once. Only the
  visible sidebar loads, so "one model, N renderers" came down to that cache
- keys `j`/`k` `enter` `/` `n` `w` `x` `r` `?`; `prefix Space` focuses it,
  `esc` returns; **mouse**: click selects and jumps, the wheel scrolls
- `n` / `x` / `r` call the existing `kel new` / `kill` / `rename`, so
  kill-safety is unchanged
- `done` until seen (`spec.md` §9)
- format subscriptions replace the visible sidebar's 2 s poll on tmux ≥ 3.2
  — *if* the poll is felt in daily use; M2's mechanism stays. **Not built**:
  the switch measured ~130–140 ms, most of it `kel _sidebar wake` starting
  bash, and the frame is already right because each sidebar knows its own
  window
- *try:* daily-drive it — this is where we learn whether it is right
- *tests:* golden frames; `capture-pane -e` of a real sidebar; keys and clicks
  driven through a real tmux pane; `x` on a dirty worktree still refusing; a
  window switch shows the right sidebar within budget (`spec.md` §5e); a VHS
  recording for the release notes (not made yet)
- *found on the way:* `view()` drew the kill prompt by naming the agent through
  a lookup that called `view()` — a stack overflow the first key test found;
  and a `.seen` written in the same second the agent finished read as seen,
  which the suite caught as a flake. Ties now read unseen (`spec.md` §9), and
  the timing check removes the old `.seen` first — it had been passing on one
  left by an earlier jump

**M4 — the two bottom rows**  →  `v0.11`  ·  **done**
- **a quota file**: `kel statusline` writes `quota` (5h %, its reset, 7d %,
  its reset, spend %, its reset, the epoch seen) to the state dir, only from a
  payload that carries rate limits; `kel-fleet` adds it to the fleet document
  as `quota`, nulling any figure whose reset has passed. The bar refreshes
  only when a displayed integer moves
- **the bar** (`cmd_status_line`) becomes the workspace footer:
  `claude ███░░ 58% (1h20m) · wk 41%  │  1 waiting 6m · $24.10`, spend from
  50%; the waiting part absent when nothing waits; the per-window list gone
- **location while the sidebar is hidden**: with `sidebar-off` present, the
  focused pane's repo and `branch ↑N ●N` — one `git status`, only then
- **Claude's row** (`cmd_statusline`) inside a kel window: model, modes,
  context, cost, nothing else. Outside kel it keeps repo identity and the
  quota chips (`spec.md` §7a)
- *try:* a day with the sidebar on, then `kel sidebar off` — is anything
  missed?
- *tests:* stale 5h and 7d each render `?`; an API-key payload writes no
  quota and the bar shows none; spend absent below 50%; the waiting segment
  absent when nothing waits; location appears exactly when `sidebar-off`
  exists; inside kel the row carries no label or quota, outside it keeps
  both; a fleet-document `quota` unit test in Go
- *found on the way:* the bar's jq bound an absent segment to `empty`, which
  ends the whole program in jq — a bar with no quota would have been blank.
  Absent segments are `null` now. And the M3 focus test raced `kel new`'s
  own jump to the window it made; it waits for that jump first
- *added in building:* with live agents and nothing to report, the bar says
  `nothing waiting` — a blank second segment looked broken

**M5 — panel popups and the embeds**  →  `v0.11`  ·  **done**
- **popups** at the selected entry's directory, titled with the tool and the
  space (` lazygit · kelsier/fix-auth `), closed when the tool quits: `f`
  yazi, `g` lazygit
- **`d`** reviews a worktree against its land base (`baseRef`, already in
  `land.go`), committed and uncommitted: `diffview.nvim` if installed, else
  `git diff --merge-base <base> | delta`. No base: the footer says so
- **`o`** opens a server's lowest port in the browser — `$BROWSER`, `open`,
  `xdg-open`, `wslview`, then WSL's `cmd.exe /c start` — and lists the rest
- **`kel url [worktree]`**: that worktree's server URLs, one per line; exit 1
  for none
- **`e`** opens the file the agent last touched, at its line: a new
  `PostToolUse` hook matched to `Edit|Write` records it per window (merged by
  `install.sh`, removed by `--uninstall`). It goes to an nvim already open in
  that worktree, over nvim's own socket, else a popup. kel owns no editor
- a missing tool is a footer message, never a crash; `kel doctor` lists
  yazi, lazygit and delta as optional
- `p` (plan / tasks) **moved to M6**, where the task list it shows arrives
- *tests:* popups' commands and directories through stub tools on PATH;
  `kel url` with none, one and two servers; `d` with no base reads unknown;
  the hook records Edit / Write and nothing else; `e` with nothing recorded
  says so; `e` reaches a running nvim's socket (a stub listening on it)
- *how the popups are tested:* a popup needs a client with a terminal, and the
  suite's are control-mode, so `KEL_POPUP_TRACE` records what would open.
  Everything before the draw — the tool lookup, the refusals, the base, the
  socket — runs for real; the `display-popup` flags themselves are checked by
  hand. A renamed `sleep` plays nvim, and a bound unix socket plays its
  listener
- *not built:* the `diffview.nvim` path is chosen when it loads, but this
  machine does not have it, so only `delta` was tried by hand
- *found on the way:* the suite's "errors cannot be swallowed" guard caught
  the first `popup()` using a single `-E`, which closes a popup the instant its
  tool exits — a failed lazygit's error would have flashed and gone. It is
  `-EE`, and the guard counts it. And by hand, on an isolated server with a
  real pty client: `display-popup` blocks its caller until the popup closes,
  harmless only because the sidebar runs actions off its key path

**M6 — plan and tasks**  →  `v0.12`  ·  **done**
Planned after measuring the real payloads (`spec.md` §9, "Plan and tasks"):
the `TodoWrite` design was stale, and the plan turned out to need no new hook.
- **the plan**: the M5 `PostToolUse` hook sends a write under
  `~/.claude/plans/` to `<window-id>.plan` (its path) instead of `.edit`, so
  `e` stops mistaking the plan for the last edit
- **tasks**: read from `~/.claude/tasks/<claude_session>/*.json` — the
  record's `claude_session`, or the `.stash` entry for an unmanaged window. No
  directory: "no task list" (the usual case on Opus 5.5). A file that does not
  parse: "tasks unreadable", never a shorter list
- **`p`** opens a popup at the agent: the plan (through `glow` or `bat` if
  installed, else plain), then its tasks — `✓` done, `▸` in progress, `·`
  pending, with what blocks what. Neither: the footer says so
- *try:* a session in plan mode; then one started with
  `CLAUDE_CODE_ENABLE_TODO_TOOLS=1` by you, for the tasks half
- *tests:* a Write under the plans dir lands in `.plan` and not `.edit`; tasks
  from a fixture directory in every status; a missing directory says "no task
  list"; a corrupt file says "unreadable"; `p` with neither says so; the
  popup's contents through `KEL_POPUP_TRACE`
- **automations stay gated** (`backlog.md` #38): the sidebar, panel and plan
  are not daily-driven yet. After M6, `v0.10.0` is tagged when the author says
  so
- *after M6, from daily use (screenshots):* trapped in lazygit (`esc` now
  closes it), the quota bar unreadable (`█` for `▓`), keys to remember (the
  footer now shows the selection's keys, and `prefix space` when unfocused),
  and trapped in vim inside yazi — so every popup now hosts a small tmux
  session where `prefix q` always closes it (`spec.md` §5c, "the panel")
- *the daily-use batch (2026-10-15, from three screenshots):* spaces named by
  their repo (two read "kelsier · main", one was ai-context); `idle_prompt`
  ends a `working` no `Stop` ended; no pane titles inside popups; `t` / `kel
  shell`; `e` on a space opens a kept nvim; `kel quit` and `Q` (backlog #33);
  an `actions` group in the sidebar's empty lower half; `prefix k`, a kel
  prompt. Plain windows (a shell, a server) are no longer listed as `?`
  agents. Separately, outside the repo: the author's nvim gained LSP for Go
  (gopls), Python (basedpyright + ruff) and TypeScript (TS 7's own `tsc
  --lsp`), treesitter parsers and diffview, and `EDITOR=nvim`
- *the second look at herdr and Orca (2026-10-15):* notifications by default
  (bell + Windows toast / osascript / notify-send), the reason a waiting agent
  waits on its row (`allow Bash?`), `working?` for an agent silent half an hour,
  and a hover bar for what a click would hit. Orca's "what it is doing now" line
  is not built: it needs a hook on every tool call — backlog, gated
- *a visual pass (2026-10-15):* calm states grey and doubt in grey italic, no
  `›` (bold name instead), window names on look-alike panes, a gap above the
  footer, solid bar tracks, a black bar with only the time, no pane title on a
  single pane (`spec.md` §5d). *Found on the way:* truncating a detail line
  after colouring it counted escape codes as characters, cutting `working? ·
  quiet 14h…`; it truncates first now. And reproducing Claude's row by hand
  through a real pane id wrote made-up numbers into that agent's `.ctx` — an
  agent read `$0.00`; restored. Reproduce statusline rows without
  `TMUX_PANE` set to a live pane
- *phase 1 of the 2026-10-16 batch:* agent state from Claude Code itself
  (`claude agents --json`), background agents in the sidebar, subagents nested
  under their agent (`subagentStatusLine`, payload measured first). Phase 2:
  worktrees nested under their repo with a passive agent count and tidy-up;
  phase 3: glow, ccusage, gh-dash, freeze + vhs, and a `.claude` viewer.
  Automations stay held until there are some
- *nothing hidden (2026-10-16):* the author could not close a Claude popup —
  esc and q are Claude's, and the title said "prefix q". kel now never shows
  "prefix" or "C-b": every hint reads the real prefix in words (`Ctrl-b`).
  Each popup's hosted session has a one-line bar with a clickable `✕ close`;
  the bar's `1 waiting` is a click target that jumps there; every prompt says
  how to answer on a clickable line (`y yes · n no`, `enter ok · esc
  cancel`); a click closes the keys list; actions gained `` ` `` jump, `:`
  kel command, and `R` restore while a previous workspace waits
- *before phase 2 (2026-10-16), from the author's review:* tmux's bar is off
  while sidebars show — it and Claude's row read as one thing said twice; the
  quota became the sidebar's last line and the waiting alarm its unfocused
  footer (a click jumps). `n` / `w` take a directory (`fix ~/code/api`, tab
  completes) and `kel new <name> <dir>` does the same, so starting an agent
  elsewhere no longer means a shell and a `cd`. The kel-command action is `;`
  (`:` needs shift). Hints say "Ctrl-b then space": "Ctrl-b space, or click"
  read as a chord. The suite went from ~3 min to ~1.5: two 40 s control-mode
  holds were "released" with `kill $!`, which hit tmux while `wait` sat out
  the `sleep` feeding it
- *phase 2 (2026-10-16):* worktrees nest under their repo — every one `git
  worktree list` knows, kel's, Claude Code's and hand-made, open or not — with
  land state; a checkout with several agents says `2 agents`, nothing more;
  `s` and `kel sweep [path]` remove landed worktrees no window or Claude
  session stands in (git refuses changes; branches only via `branch -d`).
  The `?` keys list runs a clicked key, with the hover bar on what a click
  would run. Phase 3 next: glow, ccusage, gh-dash, freeze + vhs, the
  `.claude` viewer
- *phase 3 (2026-10-16):* `install/30-extras.sh` (release binaries into
  ~/.local/bin, no sudo; Homebrew on the Mac) installs glow, freeze, vhs with
  ttyd and ffmpeg, ccusage and gh-dash. `p` renders with glow; `u` is ccusage
  (Claude Code's own transcripts — kel keeps nothing); `G` is gh dash at the
  space; `c` opens the Claude config that applies there in yazi, a tab per
  place, view and open only; `kel shot` draws a pane with freeze — SVG,
  because freeze's PNG path segfaults on this WSL2 (Go build and release
  alike) and its .webp is an SVG by another name. vhs is installed, not
  wired: a recording is the author's to script
- *found on the way:* M5's `hook_edit` had real newlines inside its `printf`
  format — a quoting slip in its writing that happened to print the same
  thing; rewritten while the plan / edit split went in

**Why this order:** M0–M1 are data and plumbing you can mostly ignore; M2 is
where the tmux-hook risk lives, so it is proven before any UI depends on it;
M3 is the first thing that feels different.

**Restore, answered:** what made it hard was the prompt — it stood between
the author and whatever they came to do, asking a question they did not
understand. M0 removes it (`spec.md` §10).

---

## v1.0 — shareable

**v1.0 is a promise, not a size**: the on-disk format, the commands and the
sidebar's keys will stop changing under you. It means the author daily-drives
the sidebar and stands behind it — author judgment, deliberately with no proxy
metric — and someone who is not the author can install it on Linux or a Mac
from a release, without a Go toolchain.

---

## Explicitly not on the path

- Anything that wraps, proxies or injects into the agent (`spec.md` §2).
- A bespoke file viewer, diff viewer or editor — the panel embeds `yazi`,
  `lazygit` and `$EDITOR` (`backlog.md` R4, R5).
- A daemon. The sidebar runs while it is open and not otherwise.

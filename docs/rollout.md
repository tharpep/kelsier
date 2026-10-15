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

### Step 4 — the build plan  ·  *M0–M3 done, M4 next*

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

**M4 — the two bottom rows**  →  `v0.11`
- one home per fact (`spec.md` §7): Claude's row shrinks to the session; a
  quota state file; the bar becomes quota plus the waiting alarm and live
  cost, taking location back only while the sidebar is hidden
- *tests:* stale quota renders `?`; no `0%` without a quota; location appears
  exactly when `sidebar-off` exists; the bar stays one `jq` pass and refreshes
  only on change

**M5 — panel popups and the embeds**  →  `v0.11`
- `f` yazi, `g` lazygit, `p` plan/tasks — titled popups at the selected
  pane's directory
- `d` reviews a worktree's diff against its land base; `o` opens a server's
  port in the browser; `kel url` (`spec.md` §5f, §10)
- `e` and the per-worktree Neovim — its lifecycle is the open question in
  `spec.md` §5c, settled when this milestone is planned
- *tests:* each popup opens at the right directory; `kel url` with none, one
  and two servers; `d` with no base reads unknown, never an empty diff

**M6 — plan and tasks, then automations**  →  `v0.12+`
- tasks come from a `PostToolUse` hook on `TodoWrite` (`spec.md` §9) — the
  payload carries the list, so no transcript is read
- plan files in `~/.claude/plans/` have generated names; tying one to a
  session probably means the transcript. Investigated when we get there —
  it blocks nothing earlier.

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

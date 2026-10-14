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

### Step 4 — the sidebar  ·  *next, being planned*

Not started. To be planned in conversation before any code; the questions it
it had to answer first are decided in `spec.md` §5c. A draft order:

1. **The fleet document learns every pane** — kind (agent / server / shell,
   from the process), cwd, and git position per worktree (branch,
   ahead/behind, dirty). Data first, in `kel-fleet`, so the sidebar and any
   later GUI read the same thing.
2. **The sidebar's plumbing** — a sidebar pane in every window (created by
   `kel new` and an `after-new-window` hook), marked so snapshot, restore and
   the dead-agent check skip it; inactive instances idle and wake when their
   window is shown; view state shared through the state directory. Prove it by
   hand on a real tmux server before building UI on it.
3. **The sidebar** — `kel top` grown into it: repo → worktree → pane, about
   eight keys, all in its footer. Navigation and jump first; `n`ew / `x` kill
   / `r`ename by calling the existing `bin/kel` commands.
4. **The panel** — files (`yazi`), git (`lazygit`), opened at the selected
   pane's directory.
5. **The two bottom rows** — `spec.md` §7: Claude's row gains git position
   and always-on quota; the tmux bar becomes the workspace footer (quota,
   fleet, panes and ports, location). Quota moves to its own state file.
6. **Plan and tasks** — the plan from `~/.claude/plans/*.md`; the task list
   from the session transcript the hooks already point at.
7. **Automations** — last, and only once the rest is daily-driven.

`v0.10.0` is tagged when 1–3 are usable.

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

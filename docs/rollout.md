# kelsier — rollout

The constraint is **"will I actually adopt this,"** not "can it be built." Each
stage ships the minimum that answers a real question; the next stage waits for
daily use to produce a specific pain.

`spec.md` section numbers are not a build order. This file is. `docs/backlog.md`
holds candidates that aren't on the path yet, and what each one is waiting for.

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

## v0.2 — grouping by repo + a selectable fleet menu  ·  **done**

> **v0.2.1** added: `kel move` (relocate a window to another group), `KEL_GROUP`
> (one flat group), the session→agent/group rename, all-lowercase keys, plus:
> mouse on + smart wheel-scroll (PageUp/Dn to full-screen
> TUIs), 1-indexed windows, pane labels, pane commands in `kel ls`, and
> **workspace snapshot / restore** — `kel` rebuilds every group, window, and
> split (agents resumed) after a reboot. (Detach + snapshot ended up on plain
> `prefix d` via the `client-detached` hook — no separate key.)

> **Shipped.** One tmux session per repo (`kel/<group>`); `kel jump` global;
> status line = current group + `⟨+N waiting⟩`; `kel go`, the `kel menu`
> quick-jump, `prefix g` group tree; `group` metadata field + auto-migration
> from the flat session. `kel restore` rebuilds into groups. (v0.4 retired the
> `kel menu` quick-jump — the board replaced it.)

**Trigger:** the flat `kel` session overflows — routinely running agents across
several repos at once, so the status line is cramped, `prefix 1-9` runs out, and
unrelated projects share one list. **Do not build until flat use actually hits
this** (see the v0.1 test plan — the specific signal is "10+ agents across 2+
repos in a normal week" or "I keep losing which window is which project").

Two things land together: sessions grouped by repo, and the first
*select-and-see* surface beyond commands (`kel menu`). Commands stay the primary
path; the menu and, later, the board are faster surfaces onto the same ops.

### Grouping

The grouping unit is the repo. One tmux session per repo, named after it;
`kel new` targets the session for wherever it was run (`--group <g>` to
override, `~`-rooted agents fall into `misc`). Group *switching* is mostly
native tmux (`prefix s` tree, `prefix (` / `)` cycle) — kel only adds the
state-aware pieces.

```
session "api-gateway"   auth-fix?   rate-limit*   docs
session "coppermind"    sazed!      atium-check
session "infra"         tf-upgrade
```

- **`kel jump` goes global** — next `waiting` agent in *any* group, switching
  sessions if needed. Stable cycle order (group name, then window index).
- **Status line — option A (minimal).** Current group in full; every other group
  collapses to one badge:
  `[api-gateway] 0:auth-fix? [1:rate-limit*] 2:docs   ⟨+2 waiting⟩`
  You don't need to know *which* group — `` prefix ` `` takes you there. (Option
  B, per-group badges, only if you later miss knowing which project is blocked.)
- `kel ls` grows group headers; `kel restore` / `kel prune` operate across all
  groups; `kel` bare lands in the last-used group (tracked in a
  `last-group` state file).
- `kel go <group>` — new; `switch-client` to that group, shell-completion on
  group names.
- session metadata gains a `group` field.
- **Migration:** existing flat-session records get `group` from their `repo`
  basename on first v0.2 run; live windows stay where they are until killed /
  recreated. No forced move — the old `kel` group empties over a few days.

### `kel menu` — the selectable fleet view

`kel menu` (and a `prefix` binding, e.g. `prefix m`) generates a **dynamic**
`tmux display-menu`: a floating list of every agent across every group, each
showing its state, press a key to jump there.

```
 ┌ kel ───────────────────┐
 │ api-gateway            │
 │  1  auth-fix       ?   │
 │  2  rate-limit     *   │
 │ coppermind            │
 │  3  sazed         !   │
 │  4  atium-check       │
 │ ─────────────────────  │
 │  n  new agent here    │
 │  `  next waiting      │
 └───────────────────────┘
```

Native tmux, ~30–40 lines to build the menu string. No scroll, single-key
items — good to ~15–20 agents. Past that (or when you want filter / sort /
browse) is the v0.3 board.

`prefix g` → native `choose-tree -Zs` for a plain group picker; a state-aware
group menu can come later if the plain one isn't enough.

### Build order

1. `group` field + `kel new` targeting group sessions
2. `kel jump` cross-session
3. `kel ls` grouped
4. `kel restore` / `kel prune` across groups
5. `kel go` + `prefix g`
6. `kel menu` (dynamic display-menu)
7. two-level status line (option A) — last, it's the fiddliest
8. metadata auto-migration

~180–230 lines net. No new dependencies. Fiddliest parts: the status-line
width handling and the `kel jump` cycle order.

### Risks

- **Session name collision** — two repos both named `api`. `--group` override +
  document; rare enough.
- **Status-line clutter** — option A sidesteps it; if even that feels noisy,
  drop to just the current group's windows and rely on `` ` `` for cross-group.
- **The flat→grouped transition** — a few days of a `kel` group alongside real
  groups until the old one empties.

## v0.3 — the board (popup)  ·  **done**

> **Shipped** on `fzf` (not a hand-rolled TUI or Bubble Tea). `kel board` /
> `prefix b`: a `display-popup` with fuzzy filter, a preview pane (metadata +
> recent output + git status), and binds — enter jump, ctrl-n new, ctrl-k kill,
> ctrl-g go-to-group, ctrl-r refresh. ~50 lines.

**Trigger:** `kel menu` stops being enough — more than ~20 agents, or you want
filter / sort / browse across everything at once.

- `kel board` as a `display-popup` TUI (shell TUI, or Bubble Tea if it needs the
  structure) — the cross-group navigator, filter, jump, new, kill
- transient: opens over the current window, never occupies a pane

## v0.4 — UX & learnability pass  ·  **done**

> A review (mine + Gemini via agy) found the core loop solid but flagged
> friction for someone learning the tool. No new features.

- **One job per key.** The old `display-menu` quick-jump is retired (`kel menu`
  is a one-release alias for `kel board`). It took two review passes (mine, then
  Gemini 3.1 Pro on the first cut) to land the final shape:
  - **`` prefix ` ``** — jump to whoever's waiting
  - **`Ctrl+Space` / `prefix b`** — the board: *find* an agent. Compact
    `--footer` (`enter jump · tab actions`); `enter` jumps, **`tab`** opens a
    labelled `tmux display-menu` on the highlighted agent (jump / new here /
    rename / go to group / kill). `ctrl-n/k/g/r` stay bound, unadvertised.
  - **`prefix m`** — *manage* the agent you're on (rename / move / new sibling /
    kill). A `display-menu`, not the board.
  - **`prefix k`** — the "new to kel?" primer (tmux primitives + "browse agents"
    + "show me around").

  The first cut had the board on three keys with a truncating single-line
  header; the fix was the footer + `tab` menu, and repointing `prefix m` from
  the board to "manage".
- **Bugs.** `kel rename <new>` (+ `prefix ,`) renames a window *and* its
  metadata record — a bare `rename-window` used to desync it so `kel kill`
  couldn't clean up; `kel kill` now also finds a record by window id as a
  fallback. Both menu "new agent" paths route through `kel new` (managed). The
  board's `ctrl-k` always confirms; `ctrl-n` uses tmux's own prompt. A crashed
  agent (no clean exit) now reads as `dead` / `x` instead of stuck on `working`.
- **Learnability.** First run (and `kel` with zero agents) prints the three keys
  that matter. Shell completion for bash + zsh (`install.sh` wires it) —
  subcommands, agent names for `kill`, group names for `go` / `move`. The
  copy-text tip (Shift+drag) moved up into `kel cheat`'s model section.
- **Dir-aware `kel`.** Bare `kel` now keys off the current directory the way
  `claude` does: inside a repo it attaches that repo's group, or starts an agent
  for it if nothing's running there. `KEL_GROUP` overrides; outside a repo it
  still falls back to the last-used group; the reboot snapshot-rebuild prompt
  still wins.
- **Group model.** `kel new` / `kel move` outside a repo now warn about the
  `misc` fallback. `kel move` warns when relocating a worktree agent to a
  mismatched group (cosmetic only). Monorepo guidance documented in `usage.md`;
  a `.kel/group` file stays on the backlog.

**Backlog from this round** is folded into `docs/backlog.md`, along with a
post-v0.4 review pass (human + Gemini via `agy`). `install.sh --uninstall` and
the `.kel/group` per-dir override carried over as #11 / #12; context-broadcast
between agents is now explicitly rejected there (R1 — `CLAUDE.md` covers it).

## v0.4.1 — two bugs and the context adapter  ·  **done**

**Trigger:** an end-to-end exercise of v0.4 (a real tmux server, simulated
hooks, repeated reboot/restore cycles) plus a re-read of the Claude Code docs
instead of working from memory.

### Bugs

- **`kel restore` destroyed the snapshot it restored from.** Rebuilding panes
  fires kel's own `after-split-window` / `pane-exited` hooks, which run
  `kel snapshot` in the background over the file the restore is still reading —
  and restore re-read it with a fresh `jq` per field per window, so it also
  truncated itself. Measured on 2 groups / 4 windows / 5 panes: the snapshot
  was reduced to one window 3/3 runs; the restore dropped a whole group once
  and two of three windows once. Fixed with a `.restoring` lockfile (a file,
  not an env var — the hooks spawn a separate `kel` from the tmux server) plus
  a private `mktemp` copy to read from, and `snapshot.json.prev` as a spare
  generation. An actual reboot never corrupted the snapshot; only `restore` did.
- **Agent names were one global namespace.** `sessions/<name>.json` meant a
  second repo could not have an agent called `docs` — in a tool organised
  around one session per repo, exactly the names you reuse. Records moved to
  `sessions/<group>/<name>.json`; `resolve_agent` takes `name` or
  `group/name`, prefers the group you are standing in, and lists candidates
  instead of guessing. Existing flat records are filed on next run.

### `kel statusline` — context and cost, without interrupting the agent

Hook payloads carry no token or cost data. **`statusLine` does**, and it was the
interface the backlog's parked cost entry was waiting for — documented and
versioned, not a transcript scrape. Claude Code pipes a JSON blob on every
conversation update; `kel statusline` takes one `jq` pass over it and records
`<wid>.ctx`.

- **records** context %, cost, tokens, context size, 5-hour rate-limit burn,
  and model, per agent window
- **surfaces** on the tmux bar (only from `KEL_CTX_WARN`, default 70, up — the
  bar stays minimal), as a `CTX` column plus cost in `kel ls`,
  `context_pct` / `cost_usd` in `--json`, and two lines in the board preview
- **renders** Claude Code's own status row: `kel <group>/<agent> · <model> ·
  ▓▓▓░ 42% ctx · $1.23 · 5h 71%`
- **costs** ~38 ms per invocation against a 300 ms debounce, and only calls
  `refresh-client -S` when the integer percent actually moves — `#(kel
  status-line)` is not free and this fires several times a second
- `install.sh` wires it the way it wires hooks, and preserves any statusLine it
  displaces to `~/.claude/statusline-prev`, chained after kel's own row
- `kel doctor` probes that it is wired

Two things worth remembering from building it: bash folds runs of tab (an IFS
whitespace character) into a single delimiter, so `IFS=$'\t' read` silently
shifts every value after an empty field — the payload is read one field per
line instead. And a malformed payload must never overwrite a good `.ctx`, so
the write is gated on a non-zero `context_window_size`.

**Not** built here, and now in `backlog.md` #14–#16: `Notification` fidelity
(kel still maps every notification to `waiting`, including `idle_prompt`),
compaction counters, and the permission hooks.

## v0.5 — tell the truth, and tell me out of band  ·  **done**

**Theme:** signal, not surfaces. Everything here is hook/state work in the layer
v0.4.1 just warmed up; nothing new to look at.

- ~~**#14** notification fidelity~~ — **done** (`84f7329`). `Notification` maps to
  what it means; `throttled` is a real state; `kel jump` stopped lying.
- ~~**#15** compaction counter~~ — **done**. `PreCompact` increments
  `compactions` on the record and deliberately leaves `.state` alone
  (compaction is mid-turn; the agent is still working). Catches what context %
  can't — context % drops back down after each compaction and looks healthy
  again. In the board preview and `--json`, not on the bar.
  The `SessionStart` matcher sub-item was **dropped**: the documented payload
  has no field naming the trigger, and matchers select which hook entry runs
  rather than appearing in the payload. Achievable with a matcher-scoped entry
  in `settings.json` if it ever matters; guessing at a field name was the
  alternative, and that is how you get a silent wrong branch.
- ~~**#13** restart-in-place~~ — **done**. `kel restart [name] [-f]`, also on
  `prefix m`. Same window, worktree, branch and conversation. The liveness
  guard asks the *pane*, not the state file: the first cut used
  `effective_state` and would have double-launched a freshly-started agent,
  which has no `.state` yet.
- ~~**#1** fleet notifications~~ — **done**. Hook-driven, never polled. Two
  gates: only on a *transition into* a notifying state, and only when you are
  not already looking (window active **and** a client attached). `KEL_NOTIFY`
  picks the states. Delivery is one `notify()` function — `tmux
  display-message` plus a backgrounded `$KEL_NOTIFY_CMD`; no platform
  detection, recipes in `setup.md`, and that function is the seam if that
  changes. Backgrounding avoids `setsid`, which macOS lacks.
- ~~**#4** informative badge~~ — **done**. `⟨api·1 infra·1⟩`, capped at three
  groups then `+N`. `kel jump` now also reports where it landed and for how
  long. `humanize_secs` was written here because v0.6's `kel top` FOR column
  needs it too.
- **macOS portability** — added to this card because it blocks a machine that
  is already in use. `bin/kel` uses no bash 4+ features, so the language is
  fine (macOS ships bash 3.2); three GNU coreutils assumptions are not, and all
  three fail *silently*:
  - `prune_state`'s `sed 's/\.\(state\|ctx\)$//'` — `\|` is a GNU BRE
    extension, so on BSD sed the extension is never stripped and **live state
    files are deleted on every `kel ls`**
  - `snapshot`'s `stat -c %Y` (BSD is `stat -f %m`) — falls back to `0`, so the
    `.restoring` lock always reads stale and **the restore protection added in
    v0.4.1 is disabled**
  - `cmd_doctor`'s `sort -V` — absent from BSD sort, so the `tmux >= 3.0` probe
    misreports

  Taking a wrong branch quietly is the worst failure mode for a tool whose
  value is being trustworthy (invariant 3 in `CLAUDE.md`). `install/` is
  apt-only and is a separate, larger job — not in this card.

  **Done.** All three are now dependency-free rather than swapped for a BSD
  spelling. A fourth turned up while fixing them: a unix socket path caps
  around 104 chars and macOS sets `$TMPDIR` to a long `/var/folders/...` path,
  so the test suite's `TMUX_TMPDIR` now lives directly under `/tmp`.
- **CI** — `.github/workflows/ci.yml` runs syntax + the suite on
  `ubuntu-latest` and `macos-latest`, plus `/bin/bash -n bin/kel` on macOS
  against the stock 3.2. This is what stops the GNU-isms coming back; fixing
  them by hand was the one-off. Deliberately no shellcheck yet — a red build
  should mean exactly one thing.

## v0.6 — one place to look  ·  *Go enters here*  ·  **done**

**Theme:** one computed view of the fleet, and a dashboard on top of it.

- **`kel _fleet --json`** — fleet state computed **once**, consumed by `ls`,
  `--json`, the board, `status-line` and `kel top`. Today every surface
  re-derives it, positionally, over TSV. That plumbing caused both of v0.4.1's
  real bugs: adding one field meant touching four consumers, and bash folding a
  run of tabs (IFS whitespace) shifted every value after an empty field, so an
  unset model name reported cost as the context percentage. This is the fix,
  and it is also the port boundary (below).
- **#2** `kel top` (folds in **#3** durations/stall and **#7** dirty) — now
  worth building, because it finally has columns the bar doesn't already show:
  CTX, cost, compactions.
- ~~**#5** peek~~ — **cut**, now `backlog.md` R6. The board preview and
  `kel top`'s LAST OUTPUT column between them answer "what is this thing
  doing" twice over, and paying a keybinding to avoid a keystroke is a bad
  trade in a tool whose premise is that switching costs one key. If the want
  returns it belongs inside `kel top` as a row expander, not as its own
  command.
- **Also shipped here:** declining the rebuild prompt is now durable. Saying
  no used to last exactly one invocation, so a workspace you had finished with
  asked again on every single `kel`. The refusal is remembered against that
  snapshot's timestamp — a *new* snapshot is a new question and still asks —
  and the prompt grew a `d` to discard the saved workspace outright.

### Why Go starts here, and not with a rewrite

Two components, and they are exactly where Go pays:

- **`_fleet`** is the hot path (`status-line` on every refresh, `statusline`
  every 300 ms), it is N sequential `tmux` calls plus N `jq` forks, and it is
  where both type bugs lived. Speed, concurrency and types, one component.
- **`kel top`** is new, has no bash version to keep in sync, and is precisely
  what Bubble Tea is for.

**Strangler fig, not a parallel branch.** Both implementations live on `main`
and the migration unit is the *subcommand*:

```
bin/kel        bash — the dispatcher.  Always works.  No Go required.
go/            Go source
  -> kel-fleet built by install.sh when `go` is on PATH
```

`gather_rows` becomes "use `kel-fleet` if it exists, else the bash path" — one
`if`. Rules that make this hold:

- **the on-disk format is the contract**, not the language: `sessions/<group>/<name>.json`,
  `<wid>.state`, `<wid>.ctx`, `snapshot.json`. Either implementation must read
  and write them identically, so any command can be either one on any day.
- **bash stays the fallback, permanently, until v1.0 says otherwise** — but
  *not* for the portability reason first given here. That argument was that a
  work laptop might not get a Go toolchain; the work laptop is a **MacBook**,
  and Go cross-compiles to it (`GOOS=darwin GOARCH=arm64`) from the Linux
  desktop, so it needs no toolchain at all. The honest position is the
  inverse: **Go is the more portable half.** Its stdlib behaves identically on
  Linux and macOS, while bash's value depends on which `sed` and `stat` are
  installed — see the v0.5 card. The real reasons to keep bash are no build
  step, edit-in-place on a tool you bend weekly, and having a second
  implementation to differential-test against. Those are enough; portability is
  not one of them, and if anything macOS is an argument for moving *sooner*.
- **the bash implementation is the test oracle.** Differential-test Go against
  it — same state dir, same JSON — on top of the existing 28-case suite. This
  is the specific answer to "a rewrite re-earns every bug you just fixed."
- **short feature branches per component**, never a long-lived `go` branch: you
  would daily-drive one and let the other rot, land fixes twice, and stop
  dogfooding the version you are trying to ship.
- **target Bubble Tea v2** (Feb 2026 — declarative `View`, new renderer, with
  matching Lip Gloss v2 / Bubbles v2). Nearly every tutorial and model answer
  still describes v1. This is a deliberate choice, not a default.

**Mutating commands port last, or never.** `kel kill`'s uncommitted/unpushed
check is the one place a bug is unforgivable (§10). Read/render surfaces are
pure functions of on-disk state — if the Go `ls` is wrong, nothing is lost.

## v0.7 — landing the work

**Theme:** the other end of the day. `kel new -w` makes five worktrees trivial
and does nothing for the five branches that need to land.

- **#6** merge-readiness via `gh` — ahead/behind from plain `git`, PR state from
  `gh pr status --json`. The column is **three-valued**: ready / not-ready /
  **unknown**. An expired `gh` token exits non-zero with empty stdout, which is
  byte-identical to "there is no PR" — rendering that as not-ready is the same
  class of lie as a bar that says `working` for a dead agent.
- **#8** `kel sweep` — batch teardown of merged, clean, pushed agents.
- **#12** `.kel/group` per-directory override.

## v0.9a — the system around the agent

**Theme:** kel manages the *system around* the agent, never the agent. That is
the pitch, sharper than "know which agent is blocked" — which, since
`claude agents` shipped, is a thing Anthropic does for its own sessions and
only its own. kel's claim is the terminal staying navigable: across agents,
across repos, and across agent-and-not-agent, with nothing owning your session.

Came out of a landscape read (2026-08-31) of Claude Squad, agent-deck, amux,
NTM, tmai, tmux-agent, tmux-agent-indicator, sesh, dmux/muxtree, Zellij, and
Claude Code's own agent view / cross-session messaging.

Ordered by how directly each serves that pitch.

### B′. Parity from inside  ·  the biggest gap, and it was invisible

The author enters with `kel` once and then lives inside tmux. Under that
workflow these are **unreachable**, because no key or menu offers them:

| missing | why it matters |
|---|---|
| `kel new -w` | *worktree* creation. Every "new agent" path — board `tab`→n, board `ctrl-n`, `prefix m`→n, `prefix k`→n — makes a plain in-place agent. The isolation story is shell-only, which means it effectively does not exist. |
| `kel sweep` | the command that closes out the day. Shipped in v0.7 with no key. |
| `kel restore` / `kel prune` / `kel doctor` | rare, but stranded |

Fix: the "new agent" prompt asks in-place vs worktree, and the rare commands
get menu entries — they do not each need a keybinding, and the key budget is
deliberate.

### A. State on the pane border and window title

State lives only on the status bar: one line, at the bottom, that you have to
look *at*. When Claude Code is full-screen a coloured pane border is peripheral
vision instead. kel already sets `pane-border-status` and already has the
state from its hooks; it just never colours it. Prior art:
`tmux-agent-indicator`, which reaches the same states through the same hooks
and paints borders, window titles and status icons.

### B. The board learns panes, not just agents

`Ctrl+Space` lists agents. Your nvim pane, lazygit, a scratch shell — invisible.
That is the literal "can't move between Claude Code and any other terminal
thing" complaint. `tmux-agent` ships five switchers; the one worth copying is
"all panes, with directory and branch context."

### F. Propagate kel's name into Claude Code

`kel new auth-fix` should launch `claude --name auth-fix`, so `/list-agents`,
`@mentions` and `kel ls` agree on the name **you chose** rather than Claude's
auto-generated one. One line, no user-visible surface.

Speculative — the benefit only lands if cross-session messaging gets used — but
it has the nice property that if it is never used there is nothing to prune,
because there is no surface. `kel rename` cannot rename a *running* Claude
session (`/rename` is interactive-only), so names drift until that agent
restarts. That is a footnote, not something to build around.

### Considered and not taken

- **C. A scratch-shell popup.** The only delta over `prefix |` is that a split
  resizes the agent's pane and makes Claude Code's TUI reflow. Real, but not
  worth a keybinding. Dropped.
- **D. Reaching a repo with no agent yet** (`sesh` merges tmux sessions +
  zoxide frecency + configured projects). Reading shell history to guess where
  you want to go is invasive, and it is someone else's data. A non-invasive
  version could use kel's *own* record history instead — but bare `kel` is
  already directory-aware, and the moment you want a brand-new repo is a moment
  you are plausibly already in a shell. **Parked as probably unnecessary.**
- **E. Delivering into a session** via `CLAUDE_CODE_MESSAGING_SOCKET`, which
  Claude Code exports to hooks. Looked promising and mostly is not: agent-to-
  agent awareness is shared context between agents, already rejected as R1;
  you messaging another agent is first-party cross-session messaging, which kel
  would be reimplementing; and routing kel's own notifications into a session
  pollutes that agent's context and costs tokens for what a tmux message does
  free. What remains is kel telling an agent a system fact it cannot know, like
  "you were killed and restarted mid-task." **Parked**, with the reasoning
  recorded so it is not re-proposed as an obviously good idea.
- **Floating panes** (Zellij). A *persistent* floating pane is tabs with extra
  steps — it recreates the sprawl kel exists to fix. A *transient* popup does
  not, which is why the board and `kel top` already use one.
- **Auto-compaction watchdogs** (amux), **broadcast/approval machinery** (NTM),
  **owning the agent UI** (Claude Squad, agent-deck). All cross the non-goals.

### Known interaction: Claude Code's agent view

`claude agents` manages *its own* background sessions and only those —
interactive sessions started in a terminal do not appear until backgrounded.
But pressing `←` or `/bg` inside a kel window hands that session to Claude
Code's supervisor and leaves the kel window holding a dead pane. Same class as
the Agent Teams entry in `backlog.md`. Recorded so it is recognised rather than
debugged.

## v0.9-clarity — the pass that did not need evidence  ·  **done**

Split out of v0.9 below. The prune pass asks *did I reach for it*, and that
question has no data yet: on 2026-08-31 the state dir held one record ever, no
snapshot, and no config file the user had written. But the target machine
became a work MacBook, which justifies polish now — on the input that *is*
available, which is inspection.

So: not "remove what is unused" but "remove what is confusing or broken",
decided by reading. Landed so far —

- **five bugs**, two of which made shipped surfaces silently inert: the shell
  completions had been dead since v0.6 (and their `group/name` filter had never
  worked), `kel ls --json` could emit English, `prefix k` bound `w` twice, the
  badge docs were three versions stale, and doctor mislabelled `gh`.
- **`kel ls`** got a column header, and `inplace` renders as `repo`.
- **doctor** prescribes a fix per failed probe, and `install/macos-tools.sh`
  provisions a Mac.
- **`kel adopt`** split out of `kel move`; `move` refuses instead of silently
  adopting. Found two latent bugs in the shared code while splitting it: both
  commands resolved the *active* window rather than the caller's pane, and took
  their group from `#{client_session}` rather than from the window's own
  session.

- **`restart` → `relaunch`**, no alias, and the two menus reconciled: `w`, `n`,
  `g`, `r` and `x` now mean the same thing in `prefix m` and `prefix k`, and `s`
  is unambiguously sweep. Menu labels state what each command *touches* —
  sweep closes windows, prune only forgets records.

**Addendum, same day: walking through it as a fresh user.** Reading catches
staleness; only actually running the tool catches behaviour. Doing that — bare
`kel` in an empty repo, the cheat sheet, the board, `adopt`/`move`/`sweep`/
`relaunch`, invariant 1 on a dirty worktree — found:

- The two "what does `prefix m` do" summaries (the cheat sheet and the
  first-run `quickstart()` shown on a bare `kel` with nothing running) still
  said "rename / move / kill / new sibling" — missing `adopt` and `relaunch`,
  the two newest items in that exact menu.
- `kel _usage` was filed under "internal (wired by install.sh)" in `--help`.
  Nothing wires it; it is a diagnostic meant to be typed, like `doctor`. Moved
  next to `doctor`, with an honest note that it is unwired scaffolding.
- **A real one, found only by triggering it live, not by reading:** an agent
  that fires `SessionStart` (state `idle`) and then crashes before its first
  prompt was reported `idle` forever — no glyph, no colour, nothing to say it
  had died. The dead-check only looked at `working` / `waiting` / `throttled`,
  treating `idle` as "no signal yet" when it is a real one. Worse, it was wrong
  the same way in three independent places — `effective_state()`, the
  `_fleet_bash` jq pipeline, and `internal/fleet/fleet.go` — which is exactly
  what the Go/bash differential test cannot catch: it only proves the two
  agree, and here they agreed on the wrong answer. Fixed in all three, and the
  differential fixture now carries an idle-then-crashed agent so a future
  divergence between them would be caught.
- Confirmed by testing, not fixed: `land_of` gives an in-place (non-worktree)
  agent standing on trunk the code `clean`, never `merged` — `merged` requires
  a branch to compare against the base, and trunk has none. `kel sweep` without
  `-f` only accepts `merged`, so for anyone who never runs `kel new -w`, sweep
  is close to a no-op by construction, and nothing says so. Left as a finding,
  not a fix: whether "clean" should sweep by default is a product call, not a
  bug — `clean` is also the fallback for "not a git repo at all", so treating
  it as automatically safe is not obviously right either.

**Second addendum: the board becomes the hub.** Different in kind from
everything above — not a bug found by using the tool, but a redesign proposed
by the user after using it: three menus (`prefix k`, `prefix m`, board `tab`)
covering overlapping ground, `prefix k` acting like a menu while `kel cheat`
(the actual reference) sat one level down inside it as a single item, and the
board only ever able to *select* an agent — no path from it to the dashboard,
to config, or to sweep/restore/prune/doctor.

The shape that came out of that discussion: the board is the hub, with a
second action layer for everything that isn't about one agent, and dedicated
prefixes stay as fast, direct doors to the same destinations rather than being
removed.

- **`kel config`** — config.toml had no path into it from inside the tool at
  all (this was true before the board work started; found the same way as the
  idle/dead bug, by trying to use the feature). Opens it in `$VISUAL`/
  `$EDITOR`, seeded from `examples/config.toml` the first time. Needed a small
  fix to get right: `$SELF` stays a symlink (kel is always symlinked, never
  copied), so a naive relative path from it looks in the wrong place —
  confirmed by testing through the real installed symlink, not just reasoning
  about it. `readlink -f` would resolve it in one call; that flag is GNU-only,
  so `_real_self` walks the chain by hand.
- **The board gains `ctrl-f`** — a second `tmux display-menu`, not scoped to
  any row: the dashboard, `kel config`, and sweep/restore/prune/doctor. The
  same `become()`-into-a-menu mechanism `tab` already used, applied to
  something that isn't per-agent.
- **`prefix m` and the board's `tab` are now the SAME menu.** `cmd_here_actions`
  gathers the current window's own wid/name/group/cwd and calls the exact
  function `tab` calls, with an extra that turns on three items — move,
  adopt, relaunch — that only know how to act on "whichever pane I was
  invoked from," which is correct for `prefix m` (always the window you're
  standing in) and would be wrong for an arbitrary highlighted board row.
  One action list, reachable two ways, instead of two that drift.
- **`prefix k` reverts to `kel cheat`, no menu.** Checked item by item: every
  single thing the old menu offered was already reachable somewhere else — a
  native tmux default it was silently wrapping (`prefix w`/`[`/`z`/`&`/`d`
  were never touched by kel), an existing kel binding (`prefix |`/`-`/`,`),
  or now `ctrl-f` / `prefix m`. Wrapping "press prefix, then press the native
  key" in a submenu was strictly more steps than pressing the native key.

Fourteen new tests. The menu-key-uniqueness guard from the first addendum
moved with the menus: `prefix k`/`prefix m` no longer live in `tmux.conf`, so
a text-parsing extractor over that file can no longer see them at all. It now
runs the real `cmd_board_actions` / `cmd_board_fleet_actions` functions with a
`KEL_DUMP_ITEMS` flag that prints the menu's items instead of opening it —
exercising the actual conditional logic (the move/adopt/relaunch branch),
not a static guess at its shape. Verified against the tool itself, not just
the test suite: planted a real duplicate key in `bin/kel`, confirmed the
suite caught it, reverted.

That is the whole card. What it did **not** do is answer the docket below,
which still wants use rather than reasoning.

## The prune pass — after v1.0, gated on use

**Theme:** subtract. Everything above was added on the strength of an argument.
This is where each one has to survive contact with having actually been used.

Go through every shipped surface and give it a verdict — **keep**,
**simplify**, or **cut** — on one question: *did I reach for it?* Not "is it
defensible", which everything here is, but whether it earned its place. peek
was cut before it was built (`backlog.md` R6) and cost nothing; cutting
something already built costs a commit and a docs edit, which is still almost
nothing.

There is no deprecation contract to honour — one user, no installs to break —
so the only question is what v1.0 should contain. Shipping v1.0 carrying dead
weight is a worse mistake than having built something that later gets cut.

**Not everything unwinds equally, and that should bias what gets built:**

| cheap to remove | sticky |
|---|---|
| a column, a sort mode, a subcommand, a `--flag` | a **keybinding** — muscle memory outlives the feature |
| anything computed at read time | the **on-disk format** — needs a migration |

Prefer a column over a keybinding, and a flag over a format change. `land` is a
column and could vanish in one commit; `prefix t` is stickier; `<wid>.ctx` is
the stickiest thing in the tool.

### The docket

Named now so the pass does not start from a blank page. Each needs a verdict,
not a defence.

| | the question |
|---|---|
| `kel top` sort modes | Does anything but triage get used? If `s` never gets pressed, three modes are two too many. |
| `throttled` as its own state | Distinct glyph, distinct colour, its own branch in the hook. Did knowing "quota, not you" ever change what you did? |
| compaction counter | `×3` beside CTX. Is it signal, or is context % already enough? |
| `⟨api·1 infra·1⟩` badge | Richer than the old `+N waiting`. Do you read the group names, or just notice that the badge is there? |
| `kel restart` | `sweep` now clears the graveyard. Is restart-in-place still reached for, or is `kel new` fine? |
| `kel move` | Shipped in v0.2 and barely mentioned since. |
| `config.toml` | Newest and least proven. If every setting stayed default for a month, it is a file nobody edits. |
| `--pr` / land's gh half | The only part that touches the network. Local git covers four of the eight codes. |
| `prefix k` primer | Written for a newcomer. There is one user and he wrote it. |

### What is out of scope here

Not a rewrite, not a redesign, and not an argument about the non-goals — those
are settled. Only: remove what is not used, and simplify what is used but
overbuilt.

**Gate:** a stretch of ordinary use with the whole feature set present, on both
machines. This is the one card in the file that genuinely cannot be rushed,
because its input is evidence rather than reasoning.

**It is no longer a v0.9 card, and it no longer blocks v1.0.** It sat last in
front of v1.0 while being gated on something no amount of work could produce,
which pinned a product milestone to a wait. v0.9 shipped as the clarity pass;
this runs when the evidence exists.

**The instrument exists now.** `kel _usage` reads a counter that appends one
line — epoch and subcommand name, never arguments — per command you actually
run. Hot paths, the fleet read, the board preview and background snapshots are
excluded, so the log is gestures rather than noise. `usage = "off"` disables
it. Both the counter and `_usage` are scaffolding for this card and should be
deleted along with it.

## v0.8 — someone else could install this  ·  **done**

- ~~**#11** `install.sh --uninstall`~~ — **done**. Every line install.sh
  writes outside the repo has an inverse; settings.json is unwired surgically
  so your own hooks survive, a displaced statusLine is restored rather than
  deleted, and the state dir is kept unless you also pass --purge. Verified as
  a round-trip in a throwaway HOME seeded to look like a machine that already
  had its own config.
- ~~**config.toml**~~ — **done**. User file, overridden by a per-repo one,
  overridden by an exported `KEL_*`. A flat `key = value` subset rather than a
  parser, and fork-free unless a config actually exists — the first cut used
  `git rev-parse` and cost 5ms on every bar redraw. `examples/config.toml`
  lists every key and is parsed by a test so it cannot rot.
- ~~README leading with the layout and the jump key; MIT~~ — **done**. It now
  opens with what the bar and the dashboard actually look like, then the jump
  key, then the commands.
- ~~docs pass~~ — **done** alongside each change rather than as a sweep, per
  the house rule that a behaviour change updates the docs in the same commit.

## v1.0 — shareable

**v1.0 is a product milestone, not an implementation one.** It previously read
"Go + Bubble Tea single static binary," which would have been reached without
kelsier getting better at anything, while `install.sh --uninstall` — a blocker
by the backlog's own words — sat outside the definition.

**v1.0 means:** the loop is closed (you know which agent is blocked, why, how
deep it is, and what it cost), the worktree lifecycle ends as cleanly as it
starts, the install is reversible, and someone who is not the author can run
`git clone && ./install.sh` and have it work — with or without Go.

How much of it is Go by then is an outcome, not a requirement.

### v2.0 — all Go, if ever

Retiring `bin/kel` is its own decision, taken after v1.0 with evidence from
running both. It needs: every subcommand ported and differentially tested, a
release pipeline (so a machine without a toolchain gets a binary rather than
nothing), and an honest answer to what is lost — editing a script and having it
live is a real feature of a tool you bend to this week's workflow.

---

## Explicitly not on the path

- **Two-slot / swap-pane architecture** — cut, rationale in `spec.md` §5c.
  Revisit only with a concrete pain the popup board cannot address.
- Diff view in an `ops` panel, PR status on the row, attach-to-external —
  candidates in `spec.md`; none committed, none blocking.
- Session templates moved **onto** the backlog as `kel new --preset`
  (`backlog.md` #10). The reversal is deliberate: it's tmux layout automation,
  not agent-wrapping.

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

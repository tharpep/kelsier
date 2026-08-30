# kelsier — backlog

Candidates, not commitments. Same rule as `rollout.md`: nothing gets built until
daily use produces the specific pain that unlocks it. This file is where an idea
waits and what it's waiting for.

Came out of a post-v0.4 review pass — a human read plus a second opinion from
Gemini 3.1 Pro via `agy`, then a blunt critical pass over the result — on human
use, UX, and the competitive landscape (Claude Squad, Vibe Kanban,
Nimbalyst/Crystal, Conductor, agent-farm, the cloud agents).

**Second pass (2026-08-29, Claude Opus 5)** re-read the Claude Code docs rather
than working from memory, and that changed the picture: hook payloads carry no
token or cost data, but `statusLine` does — which shipped as v0.4.1 (see
`rollout.md`) and retired the "fleet cost view" parked entry. It also
found that kel wires 5 of Claude Code's **30** hook events, and that the one it
leans on hardest, `Notification`, carries a `notification_type` kel ignores.
Entries #14–#16 and the *Known interactions* section come from that pass.

**Numbering** is allocation order and a stable ID — not priority. Tier, and
position within a tier, carry that.

**Scheduled entries.** `rollout.md` now assigns most of this file to a release,
so tiers describe *kind* and rollout describes *when*; where they disagree,
rollout wins.

| release | entries |
|---|---|
| ~~v0.5~~ **done** | ~~#14~~ · ~~#15~~ · ~~#13~~ · ~~#1~~ · ~~#4~~ · macOS fixes · CI |
| v0.6 | `_fleet --json` · #2 (folds #3, #7) · #5  — *Go starts here* |
| v0.7 | #6 · #8 · #12 |
| v0.8 | #11 · config file (un-parked below) |
| unscheduled | #9 · #10 · #16 · everything under *Parked* |

**Tags.** `[fits]` — inside "just bookkeeping, never wrap the agent."
`[borderline]` — one design decision away from crossing a non-goal; the entry
says which. `[violates]` — recorded so it stops getting re-proposed.

---

## Tier 1 — close the core loop

The core value prop is "know which agent is blocked, get there in one key." Half
of that — the *knowing* — currently only works if you're looking at the status
bar. These finish it.

Key budget: the polished designs below add exactly two bindings — `prefix t`
(dashboard, replaces tmux's useless clock) and `prefix v` (peek, unbound in
stock tmux). Everything else is a subcommand or a board action.

**Principle from the review:** the status bar stays minimal. Everything richer —
durations, last output, git state, merge-readiness — lives in `kel top` (#2),
not the bar. The one concession on the bar is #4.

### 14. Notification fidelity — stop crying wolf  ·  `[fits — correctness]`  ·  **shipped v0.5**

`kel hook` maps **every** `Notification` to `waiting`. The payload carries a
`notification_type`, and only some of its values mean "blocked on you":

| `notification_type` | today | should be |
|---|---|---|
| `permission_prompt`, `agent_needs_input` | `waiting` | `waiting` ✓ |
| `idle_prompt` | `waiting` | *not* blocked — you just left it sitting |
| `agent_completed` | `waiting` | `done` |
| `quota_auto_resume_fired` / `_stale` / `_disabled` | `waiting` | `throttled` — its own state |

So `` prefix ` `` — the one keystroke the whole tool is built around — currently
teleports you to agents that are not blocked on you. `notification_text` is in
the same payload and belongs in the board preview.

`throttled` is worth a real state: "this agent is waiting on quota, not on me"
is exactly the thing you otherwise discover by walking over to it. Suggested
bar glyph `~`, sorted below `waiting` in `kel top`.

**Gate:** none. This is a `case` on a field already being parsed. Do it first.
~10 LOC.

### 15. Compaction counter  ·  `[fits]`  ·  **shipped v0.5**

`PreCompact` / `PostCompact` are two more hook events, same shape as the five
already wired. Count them per agent (`compactions: N` in the record). "This one
has compacted three times" is a deterministic, zero-cost signal that a session
is long in the tooth — it pairs with the context % from v0.4.1 and catches the
case context % *can't*: an agent that keeps getting squeezed back down.

Also worth taking while in there: `SessionStart` has matcher values
(`startup`, `resume`, `clear`, `compact`, `fork`) and kel maps all of them to
`idle`. A `compact` restart is not the same event as a fresh `startup`.

**Gate:** none. Two hook lines + a jq increment. ~10 LOC.

### 16. Precise blocked-state via permission hooks  ·  `[fits]`  ·  *still open*

`PermissionRequest` and `PermissionDenied` are dedicated events. They are a
sharper signal than `Notification`+`permission_prompt` — a request is
unambiguously "kel, this agent cannot proceed without you," and a denial is
worth surfacing because a denied agent often stalls quietly afterward.

Fold in only after #14 — #14 is the correctness fix, this is the refinement.

**Gate:** after #14, if `Notification` still feels imprecise in daily use.

### 1. Fleet notifications  ·  `[fits]`  ·  **shipped v0.5**

The status line is the only passive surface. In another app, another workspace,
or with tmux detached, "who needs you" reaches you nowhere — and `` prefix ` ``
can't help you act on a thing you don't know about.

Fire a signal when an agent enters `waiting` (optionally `done` / `dead`) and
you are not on that window.

- Minimal: terminal bell + `tmux display-message` to the attached client.
- Then: a desktop notification — OSC 9 / OSC 777 through the terminal, or a
  `notify-send` / PowerShell-toast command the user configures.
- Guard: only when the window isn't focused; user picks which states notify.

**On the reference machine (WSL2), `notify-send` is not installed and there is
no desktop session to install it into.** The working ladder here is: bell →
`tmux display-message` → OSC 9 through Windows Terminal → `powershell.exe`
toast (it is on `PATH` at `/mnt/c/WINDOWS/...`). Do not write "install
`notify-send`" into this entry; it is a dead end on this box.

Do #14 first. Notifying on today's `waiting` would page you for `idle_prompt`,
which is precisely the false positive that makes people turn notifications off.

**Gate:** none, but sequenced after #14.

### 2. Fleet dashboard — `kel top`  ·  `[fits]`  ·  **shipped v0.6**

One read-only view of the whole fleet. `kel ls` has the table but you must drop
to a shell for it; the board is a *picker*, not a status view; `prefix k` is the
beginner primer and stays that way (do **not** overload it — the review and
`agy` landed there independently).

**Surface.** `kel top`, bound to `prefix t` (replaces tmux's clock — no loss).
Runs on plain stdout when not in tmux.

**Shows**, one row per agent, every group:

```
GROUP  AGENT  STATE  FOR   CTX   $     IDLE  BRANCH  DIRTY  LAST OUTPUT
```

- **STATE** — coloured glyph, as the bar.
- **FOR** — time in the current state, from the `.state` epoch (`6m`, `1h04m`).
  The column you're scanning: "waiting 6m" is the signal.
- **IDLE** — time since the window last produced output (`#{window_activity}`).
- **LAST OUTPUT** — `capture-pane -p` (no `-e`), stray control chars stripped,
  then truncated to fit. Never `capture-pane -e | tail -1` — that chops an ANSI
  escape mid-sequence and bleeds colour across the whole table.
- **CTX / $** — context-window percent and session cost, read straight from
  `<wid>.ctx` (v0.4.1). These were parked when this entry was written and are
  now free: no extra work, no scraping, already on disk. Colour CTX at
  `KEL_CTX_WARN` and again at 90.
- compactions (#15), when that lands — a small `×3` beside CTX.

**Sort = triage order:** `waiting` (longest first) → `working` → `done` →
`idle` → `dead`. The top row is always "deal with me first."

**Refresh.** Popup runs a 2 s redraw loop (`kel top --once` + `read -t2 -n1`);
`q` quits. No actions — the board owns those.

**Degrades.** 2 agents: a short table. 20: still one screen, sorted so the
urgent rows never fall below the fold; beyond that it scrolls.

**Sequencing note (second pass).** Build #14 and #15 *before* this. As
originally scoped, `kel top` was a new surface showing the same four states the
bar already shows; with accurate states, context, cost and compaction counts it
becomes a view worth opening. The scarce resource here was never display room,
it was what kel knows.

**Gate:** none. This is "clarity on what's going on," the thing most asked for.
Folds in #3 and makes the raw `kel ls` table mostly a scripting interface.

~60 LOC.

### 3. Waiting-duration + stall flag  ·  `[fits — folded into #2]`  ·  **FOR column shipped v0.6; stall flag still open**

The `.state` epoch gives time-in-state for free — it's the `kel top` FOR column.
Also flag an agent stuck on `working` with no state change in >N minutes: a soft
"is this one wedged?" without a new hook. **Dashboard-only** — this does not go
on the status bar (see the principle above).

**Gate:** ships with #2.

### 4. Informative "+N waiting" badge  ·  `[fits]`  ·  **shipped v0.5**

`⟨+2 waiting⟩` tells you someone's blocked, not who or where. Expand to
`⟨api·1 infra·1⟩`, or have `` prefix ` `` print `→ api/auth-fix (waiting 4m)` as
it jumps. This is the one place richer state earns a spot on the bar — it's the
same footprint, more signal.

**Gate:** low; bundle with #2.

### 5. Quick peek — `kel peek`  ·  `[fits]`

Glance at an agent's recent output without leaving the pane you're typing in.
Same core loop as jump-to-blocked: see *why* an agent is blocked without losing
your context.

**Surface.** `kel peek [name]`, bound to `prefix v` (unbound in stock tmux).

**Target.** No name → the agent whose window most recently produced output
(`tmux list-windows -a -F '#{window_activity} #{window_id}' | sort -rn | head`),
falling back to the newest `.state` mtime if that's ambiguous. `name` → that
agent.

**Shows.** `display-popup -w 95% -h 90% -E` running
`tmux capture-pane -t <wid> -p -e -S -1000 | less -R +G` — last ~1000 lines,
colour preserved, scrolled to the bottom. `less` keys to scroll and search;
`q` drops you back exactly where you were.

**Edge case.** If the agent has shelled into a full-screen TUI (lazygit, an
editor), `capture-pane` grabs the alternate screen and the capture looks odd.
Acceptable for a one-shot bash peek.

**Does not.** Send keystrokes, switch windows, follow live output, touch state.

**Partly absorbed by v0.4.1.** A good share of "let me peek at that one" was
really "how deep is this session," which is now a number on the bar and in
`kel ls`. Still worth building for the other half — *why* it's blocked — but
it dropped a notch in value.

**Gate:** do it alongside #2. ~20 LOC.

---

## Tier 2 — worktree & fleet hygiene

### 6. Merge-readiness  ·  `[fits — status only]`

`kel new -w` makes spawning five worktrees trivial and does nothing for the
other end of the day, when five branches need to land. For each worktree agent,
in `kel top`: **ahead/behind main, and pushed?** — two `git` status reads, no
more. kel never runs a merge, and it does **not** predict conflicts or test a
rebase — that's `lazygit`'s job (and the reason the diff viewer is rejected,
#R5). This is a fleet glance — "which of my worktree agents are ready" — that
lazygit can't give you because it's one repo at a time.

**Use `gh` for the PR half** (it is installed): `gh pr status --json
number,state,isDraft,mergeStateStatus`. Keep plain `git` for ahead/behind — no
network, no auth, always answerable.

**Auth is the design constraint, not a footnote.** `gh` needs a live token, and
tokens expire. `gh pr status` on a stale token exits non-zero and writes to
stderr; a naive `2>/dev/null` would render that as *"no PR"* — which reads as
"not ready to land" when the truth is "kel has no idea." That is the same class
of lie as a status line that says `working` for a dead agent, and this file
already treats that as unacceptable.

So the column is **three-valued, not two**: ready / not-ready / **unknown**,
with `unknown` rendered distinctly (`?`, dim) and never collapsed into a
negative. Concretely:

- branch out to `gh` only for agents whose worktree is otherwise clean and
  pushed — the ones where a PR answer would change what you do
- treat any non-zero exit as `unknown`; never infer absence from failure
- cache the answer per branch with a short TTL, so one auth failure doesn't
  re-stall every row on every redraw
- `kel doctor` gets a `gh auth status` probe, so the fix is one command away
  and the failure has a named home rather than a silent dash

**Gate:** when you've felt the integration pile-up.

### 7. Git dirty state — dashboard-only  ·  `[fits]`  ·  **in the fleet document since v0.6**

The DIRTY column in `kel top` (already in #2). Not on the status bar — a cramped
bar is why grouping exists, and the principle above keeps it minimal.

**Gate:** ships with #2; nothing extra to build.

### 8. Batch teardown — `kel sweep`  ·  `[fits]`

`kel kill` and `kel prune` are one-at-a-time. Run five worktree agents, they all
finish, and `kel top` is a graveyard you clear one command at a time.

`kel sweep` — kill every `done` / `dead` agent **whose branch is merged into
main and whose worktree is clean**; report (don't touch) anything unmerged,
unpushed, or dirty. Same safety rule as `kel kill`: never delete an agent's only
copy of real work. `-n` dry-run; `-f` to include unmerged.

**Gate:** the first time you close five agents by hand in one sitting.
~30 LOC.

---

## Tier 3 — breadth / opportunistic

### 9. Second state adapter  ·  `[fits — it's the spec's design]`

State detection is meant to be a per-agent adapter (`spec.md` §9); only Claude
Code exists. Add one more — Codex / Aider / `agy` — even a crude
process-plus-last-line fallback. Proves the abstraction and de-risks the v1.0
metadata schema before it's locked.

**The coupling got deeper in v0.4.1, and that should be recorded honestly.**
kel now has *two* Claude-Code-shaped adapters — the state hooks and the
`statusLine` context reader — and only the first has any notion of being
swappable. A second agent would need a `state` adapter and would simply have no
context/cost column. That is the right trade for a personal tool, but the v1.0
schema should treat `context` as an optional, per-adapter capability rather
than a field every agent is assumed to have.

**Gate:** the day you actually run a non-Claude-Code agent under kel — not a
minute before.

### 10. Session presets — `kel new --preset`  ·  `[borderline → fits]`

Reverses the "session templates — not on the path" line in `rollout.md`. The
reversal is deliberate: this is tmux layout automation, native to what kel is.

**Surface.** `kel new <name> --preset <p>`. A preset is a bash fragment at
`<repo>/.kel/presets/<p>` (and `~/.config/kel/presets/<p>`; repo wins) — same
category as the existing executable `.kel/setup`.

**Declares** (all optional):

```bash
# .kel/presets/review
KEL_AGENT="claude"
KEL_MODEL="opus"                       # appended as --model
KEL_ISOLATION="worktree"               # else inplace
KEL_PANES=( '$EDITOR .' 'git diff main...HEAD | less -R' )
KEL_LAYOUT="main-vertical"             # any tmux select-layout name
```

`kel new` sources it in a subshell, reads those vars back, then: creates the
window + agent, opens one **shell** pane per `KEL_PANES` entry, runs
`select-layout $KEL_LAYOUT`, and only *then* sends each command into its pane.
Spawning the panes with their commands directly would let a fast-exiting command
close its pane before `select-layout` runs and throw off the geometry.

**Precedence.** explicit CLI flag > preset > default. `-w` forces worktree;
nothing de-escalates isolation.

**Restore.** Presets run only at `kel new`. They produce ordinary tmux splits,
so the existing snapshot captures the *result* and restore rebuilds from that —
it never re-runs the preset. Preset = how it's born; snapshot = how it comes
back.

**Discovery.** `kel presets` lists them; `--preset <TAB>` completes.

**Not this.** Jinja-style templating; re-enforcing the layout after you resize;
integrating Claude Code Skills / MCP (#R2).

**The risk this entry doesn't name (second pass):** it introduces a
config-file format — `.kel/presets/<p>` with `KEL_*` variables — *before* v1.0's
`~/.config/kel/config.toml` exists. Ship it and kel has two config systems and a
migration to write. Of everything in Tier 3 this is the most likely to be
regretted, and `.kel/setup` plus a shell alias already covers most of the pain.
If it is built before v1.0, it should be a `.toml` block the eventual config
loader can absorb, not a sourced bash fragment.

**Gate:** when you type the same `kel new … && tmux split … && …` twice.
~50 LOC.

### 11. `install.sh --uninstall`  ·  `[fits — release blocker]`

Unwind the symlink, the `.tmux.conf` source line, and the `settings.json` hook
merge — the `.kel-bak.*` copies make it safe. Not urgent (this is a personal
tool), but it blocks any public release.

**Gate:** before showing anyone.

### 12. `.kel/group` per-directory override  ·  `[fits]`

Already noted in `usage.md` / `rollout.md`. A file that pins a directory's
group, for monorepos that don't split cleanly on git root.

**Gate:** when the monorepo fallback is a daily annoyance — not before.

### 13. Restart-in-place  ·  `[fits]`  ·  **shipped v0.5**

> Moved out of Tier 3 order. v0.4 shipped `dead` detection, so kel can now
> *diagnose* a crashed agent with no way to *fix* it short of a workspace-wide
> `kel restore`. A state with no exit is worse than no state, and this is
> ~15 LOC that completes something already shipped.


A crashed agent leaves a live window sitting at a bare shell, its worktree and
branch intact. `kel new` would try to build a duplicate worktree; `kel restore`
is workspace-wide. Add a single "restart this one" — an item in the `prefix m`
manage menu and `kel restart [name]` — that relaunches the agent (`--resume` the
recorded session id) in the existing window, touching nothing else.

**Gate:** the first time you rebuild a crashed agent by hand.
~15 LOC.

---

## Parked — needs a concrete pain first

- ~~**Fleet cost / token view.**~~ **Un-parked and shipped in v0.4.1.** The gate
  was "only if Claude Code exposes cost in a hook payload or `--json` output — a
  stable interface, never a scrape." It does, just not where this entry looked:
  not in hooks, but in the **`statusLine`** payload, which hands a script
  `context_window.used_percentage`, `cost.total_cost_usd`, `rate_limits.*` and
  `prompt_cache.*` on stdin, locally, without consuming tokens. Documented and
  versioned — a real interface, not a scrape. `kel statusline` records it to
  `<wid>.ctx`; the bar, `kel ls --json` and the board read it. The lesson worth
  keeping: this sat parked for a release because the search was for a *hook*.
- ~~**Config file / theming**~~ — **un-parked, scheduled for v0.8.**
  (`~/.config/kel/config.toml` + `.kel/config.toml`): status-line format,
  colours, state glyphs, board size, keybind opt-outs, the `KEL_*` env vars
  migrated in. It was parked behind "the v1.0 Go rewrite", which no longer
  exists as a milestone — and it has to land *before* #10 presets so kel never
  runs two config systems at once. See `rollout.md` § v0.8.
- **Two-slot / swap-pane layout** — cut in `spec.md` §5c. Revisit only against a
  pain the popup board cannot address.
- **PR status on the board row; attach-to-external-agent** — candidates in
  `spec.md`, none committed.

---

## Known interactions — not features, things that will bite

### Claude Code Agent Teams

Claude Code has an experimental multi-agent mode
(`CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS=1`): a lead session spawns named
teammates that share a task list and message each other.

**It does not compete with kelsier**, and it is worth being explicit about why,
because the names sound like the same product. Agent teams are *intra-session*:
one team per session, scoped to that session, one repo, lead-plus-teammates,
tokens scaling per teammate. kel's problem is *inter-session*: N sessions the
human started, across repos, and which one is blocked. Nothing to defend
against and nothing to copy.

**But there is a concrete collision.** With `teammateMode: "tmux"` (or `"auto"`
inside tmux), Claude Code **splits panes inside the window it is running in** —
which, under kel, is one of kel's agent windows. Consequences:

- `kel snapshot` records those teammate panes as ordinary panes. On restore,
  `restore_from_snapshot` rebuilds them as bare shells with the saved cwd, and
  `KEL_RESTORE_CMDS` won't re-run them (nor should it). You get a window with
  the right geometry and three empty shells.
- `effective_state`'s "is anything but a shell alive here?" check reads
  teammate panes as evidence the agent is alive, so a lead that dies beside a
  live teammate would not be flagged `dead`.
- the bar's pane count (`·3`) starts counting teammates, which is arguably
  correct but was never the intent.

**Not worth pre-solving.** The cheap defensive move, if teams ever get switched
on, is for `kel snapshot` to record pane count but restore only pane 0 for a
window whose agent has a recorded `claude_session` — let the agent rebuild its
own teammates on resume. Recorded here so the failure is recognised in ten
seconds instead of debugged.

**Default is `"in-process"`**, so this is inert until deliberately enabled.

---

## Rejected — with rationale

### R1. Shared context between agents  ·  `[violates]`

Context management is a hard non-goal, and `CLAUDE.md` + Claude Code's own
memory already cover it — the author's read and `agy`'s agreed. Anything that
syncs or broadcasts context is out. This kills the "context-broadcast between
agents" note in `rollout.md`.

### R2. Claude Code Skills / MCP integration  ·  `[violates]`

The agent's environment is the agent's. kel wiring tools or MCP servers into a
session is the definition of a harness.

### R3. Documentation surfacing & drift detection  ·  `[violates]`

Two versions, both out. **The shortcut** (`kel docs` — find and open the repo's
`CLAUDE.md` / `AGENTS.md` / `README`) is feature creep: you already have a shell
in that pane, and `fzf` + `bat` + your editor do this with no help from kel.
**Drift detection** (does `CLAUDE.md` still match the code?) needs an LLM; kel is
a bookkeeping script and doesn't reason about content.

### R4. Codebase structure / tree view  ·  `[violates]`

`yazi` / `eza --tree` / `tre` in an adjacent pane. Explicit non-goal (§"Not a
file manager"); the author's instinct to push back is right.

### R5. Bespoke diff viewer  ·  `[violates]`

`lazygit` + the editor (`spec.md` §3). Merge-readiness (#6) gives the *status*
of a branch — ahead/behind, pushed — never a *viewer* or a conflict check.

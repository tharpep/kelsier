# kelsier — backlog

Candidates, not commitments. Nothing gets built until daily use produces the
specific pain that unlocks it — each entry names that pain as its **gate**.
`rollout.md` decides what is next; where the two disagree, rollout wins.

Rewritten for the v0.10 pivot. The full pre-pivot file, including every
shipped entry's design notes, is `git show v0.9.0:docs/backlog.md`.

**Numbering** is allocation order and a stable ID, not priority. **Tags:**
`[fits]` — inside "bookkeeping, never wrap the agent." `[borderline]` — one
design decision away from crossing a non-goal; the entry says which.
`[violates]` — recorded so it stops getting re-proposed.

---

## Closed by the pivot

The 2026-09-01 TUI audit (#17–#37) catalogued what was wrong with the board,
its menus and `kel cheat`. v0.10 deleted those surfaces, so most entries were
closed by removal rather than fixed. Recorded so nobody goes looking for them:

| entries | outcome |
|---|---|
| #17 #18 #20 #21 #23 #24 #28 #29 #30 #31 #35 #36 | the board, its menus, `_run`, `kel menu` and `kel cheat` are gone |
| #19 #25 #26 #27 | fixed before the pivot, with tests that survive it |
| #22 | `resolve_agent` dropping a middle path segment — **still open**, below |
| #32 #34 #37 | absorbed into the sidebar's design (`rollout.md` § v0.10 step 4) |
| #33 | **still open**, below |

Three rules from that audit outlive the surfaces it audited, and the sidebar
inherits them:

1. **Depth is a cost, not a container.** Adding an item to a submenu does not
   fix unreachability; it relocates it. The board's `ctrl-f` reintroduced
   depth on one axis in the same commit that removed it on another.
2. **The user uses kel; tmux is under the hood.** "That is native tmux" is not
   a reason a gap is acceptable — if the thing acted on is a kel concept, kel
   owns the verb. Terminal concerns (scrolling, copy mode) are the exception.
3. **One interaction model.** The board filtered live with no mode, `kel top`
   needed `/`; `q` quit one and typed into the other; reload was `ctrl-r` in
   one and `r` in the other. One surface makes this impossible by construction.

---

## Open

### 22. `resolve_agent` silently drops a middle path segment  ·  `[defect]`

`resolve_agent` splits on the first and last `/` (`${want%%/*}`,
`${want##*/}`), so `kel kill a/b/c` looks up group `a`, name `c`, silently
discarding `b`. The board's pane rows were the path that produced such names;
with the board gone it takes a typo to reach. Still a guess reported as a fact:
a three-segment name must be refused, naming what it could not resolve. Fix
when the resolver is next touched — likely when `kill` moves to Go.

### 33. There is no way to close kel  ·  `[fits]`

No command ends the workspace. `prefix d` detaches and snapshots, leaving every
agent running; to stop, you kill each session by hand. A `kel quit` would
snapshot, then end every kel session, **refusing on unsaved worktree work the
way `kel kill` does** — this is the one command that could destroy several
agents at once, so the refusal matters more than the convenience.

**Gate:** the sidebar's workspace exists (step 4) — "quit" means something
different once every window carries a sidebar.

### 16. Precise blocked-state via permission hooks  ·  `[fits]`

`PermissionRequest` and `PermissionDenied` are dedicated events, sharper than
`Notification` + `permission_prompt`; a denial is worth surfacing because a
denied agent often stalls quietly afterward.

**Gate:** `Notification` still feels imprecise in daily use.

### 3b. Stall flag  ·  `[fits]`

Flag an agent stuck on `working` with no state change in more than N minutes —
a soft "is this one wedged?" with no new hook. The time-in-state it needs is
already the FOR column. Sidebar-only, never the bar.

**Gate:** an agent wedges and you notice late.

### 9. Second state adapter  ·  `[fits]`

State detection is a per-agent adapter; only Claude Code exists. Context/cost
is a *second* Claude-Code-shaped adapter with no fallback, so the fleet
document should treat `context` as an optional per-adapter capability.

**Gate:** the day you actually run a non-Claude-Code agent under kel.

### 10. Session presets — `kel new --preset`  ·  `[borderline → fits]`

A named recipe for a new agent: isolation, model, extra panes and a layout.
Tmux layout automation, native to what kel is. If built, it is a `[preset.x]`
block in `config.toml`, never a sourced bash fragment — one config system.
Full design at the `v0.9.0` tag. The sidebar's panel may make most of it moot.

**Gate:** you type the same `kel new … && tmux split …` sequence twice.

### 39. Read-only commands for agents — `kel state`, `kel wait`  ·  `[fits]`

herdr's socket API lets agents split panes, read each other's output, type
into each other and `wait --state`. Most of that is out for kel — typing into
an agent pane breaks invariant 4, and reading another pane's output is kel
storing agent output. What survives is **read-only**: `kel state <agent>`
prints a state, `kel wait <agent> --state done` blocks until it holds (a poll
of the state file, in the calling process — not a daemon). An agent can then
coordinate others through commands it chose to run. `kel url` (`spec.md` §10)
is the first of these and is designed already.

**Gate:** the first time you want one agent to wait on another and end up
watching both yourself.

### 38. Automations view  ·  `[borderline]`

The author's sixth want (`spec.md` §3): see and organise scheduled or looping
agents — cron, Claude routines, `/loop`. Borderline because *running* them
would make kel a scheduler, which is a daemon; *listing* what is scheduled and
jumping to its output is bookkeeping. Only the second is in scope.

**Gate:** the sidebar, panel, plan and tasks are daily-driven (`rollout.md`
step 4.6).

---

## Known interactions — not features, things that will bite

### Claude Code Agent Teams

An experimental mode (`CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS=1`) where a lead
session spawns teammates. It does not compete with kel — teams are
intra-session, kel is inter-session — but with `teammateMode: "tmux"` Claude
Code **splits panes inside the window it runs in**. Under kel that means:
snapshot records the teammate panes and restore rebuilds them as bare shells;
a lead that dies beside a live teammate is not flagged `dead`. **The workspace
design makes this sharper**: teammate panes would split a window that also
holds kel's sidebar pane, and the sidebar must list them as panes, not agents. Default is `"in-process"`, so it is inert until enabled —
but step 4 should decide what happens rather than discover it.

---

## Rejected — with rationale

### R1. Shared context between agents  ·  `[violates]`

Context management is a hard non-goal; `CLAUDE.md` and Claude Code's own memory
cover it. Anything that syncs or broadcasts context is out.

### R2. Claude Code Skills / MCP integration  ·  `[violates]`

The agent's environment is the agent's. kel wiring tools or MCP servers into a
session is the definition of a harness.

### R3. Documentation surfacing & drift detection  ·  `[violates]`

`kel docs` is feature creep — `fzf` + `bat` + your editor do it. Drift
detection needs an LLM, and kel does not reason about content.

### R4. Codebase structure / tree view  ·  `[violates — as a reimplementation]`

Still rejected as something kel *draws*. The v0.10 panel shows a tree by
embedding `yazi` at the selected pane's directory — exactly what this entry
recommended ("`yazi` in an adjacent pane"). The difference is that kel now
opens it for you, in the right place; it still never renders a tree itself.

### R5. Bespoke diff viewer  ·  `[violates]`

`lazygit` + the editor, embedded in the panel. LAND gives the *status* of a
branch, never a viewer or a conflict check.

### R6. Quick peek — `kel peek`  ·  `[obsoleted]`

Cut 2026-08-30: `kel top`'s LAST OUTPUT column showed every agent at once. In
the sidebar design selecting a row *is* the peek — it shows the
real pane. Original design at the `v0.9.0` tag.

### R7. A browser inside the terminal  ·  `[violates]`

`carbonyl` runs Chromium in a terminal and works; it is a novelty, and tmux
passes terminal graphics through poorly. The need underneath is "see what this
worktree serves", which `o` meets by opening the port in the real browser
(`spec.md` §5f). Browser automation is the agent's own tooling (R2).

### R8 – R11. What the desktop ADEs do that kel will not  ·  `[violates]`

Recorded 2026-10-14 from a look at Orca, Superset, Conductor, T3 Code and
herdr, so they stop looking like gaps:

- **R8. Cloud or remote sandboxes** (Conductor, Superset) — not
  cloud-anything.
- **R9. One prompt fanned out to N agents, best one merged** (Orca) — doable
  with only launch commands, but it is a demo and team feature, not the
  author's loop.
- **R10. A plugin marketplace** (herdr) — a personal tool's extension point is
  its source.
- **R11. Becoming the multiplexer** (herdr) — owning the event loop is
  herdr's real advantage, and it costs a reimplementation of tmux's
  persistence, detach and SSH. kel closes the gap by asking tmux properly
  (`spec.md` §5a: control mode, format subscriptions) instead.

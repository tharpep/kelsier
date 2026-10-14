#!/usr/bin/env bash
# kelsier regression suite.
#
#   test/kel-test.sh            run everything
#   test/kel-test.sh -v         show each command's output on failure
#
# Fully isolated on three axes, because this suite calls `tmux kill-server`:
#   * $TMUX_TMPDIR   -> its own tmux socket, so kill-server can NEVER reach the
#                       server your real workspace is on
#   * $XDG_STATE_HOME-> its own records, state and snapshot
#   * KEL_SESSION    -> its own session-name prefix
# Agents are faked with `sleep 9999` — no Claude Code session is ever started,
# no tokens spent.  Everything is torn down on the way out, including on
# failure.
#
# Requires: tmux, jq, git, go.  ~2 min on an M-series Mac (2026-09-01). The suite
# prints its pass/fail count at the end.
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
KEL="$HERE/../bin/kel"
[ -x "$KEL" ] || { echo "no executable bin/kel next to $HERE" >&2; exit 1; }

VERBOSE=0; [ "${1:-}" = "-v" ] && VERBOSE=1

WORK="$(mktemp -d "${TMPDIR:-/tmp}/kel-test.XXXXXX")"
export XDG_STATE_HOME="$WORK/state"
export KEL_SESSION=keltest
# Sidebars off unless a section turns them on: the sections written before
# v0.10 count panes, and a sidebar is one more.  The sidebar's own section
# turns it on and re-checks what matters with it present.
export KEL_SIDEBAR=off

# tmux resolves its socket under $TMUX_TMPDIR, and bin/kel inherits it — so the
# suite and the tool under test share one throwaway server and `kill-server`
# cannot touch the default socket.  Without this the suite kills a real
# workspace, which is not hypothetical: it has happened.
# Deliberately /tmp and not under $WORK: a unix socket path caps at ~104 chars,
# and $TMPDIR is a long /var/folders/... path on macOS, so "$WORK/tmux" can
# overflow it — tmux then fails with "File name too long" and every test that
# needs a server fails for a reason that looks nothing like the cause.
TMUX_TMPDIR="$(mktemp -d /tmp/kel-tsock.XXXXXX)" || exit 1
export TMUX_TMPDIR
[ -n "$TMUX_TMPDIR" ] && [ -d "$TMUX_TMPDIR" ] || {
  echo "refusing to run: TMUX_TMPDIR is not set, kill-server would hit the default socket" >&2
  exit 1
}
unset TMUX TMUX_PANE      # don't inherit an outer tmux; we are not "inside" ours
# ...nor its config.  reset() starts the server with -f /dev/null, but once a
# test kills it, the next one is started by KEL (`new-session` in a restore),
# and that loads the developer's real ~/.tmux.conf — kel's own hooks included,
# running the installed kel against this suite's state.  CI has no such file,
# so this only ever bit a dev box: it put a test's agent pane into view-mode.
# Every tmux call — the suite's, kel's, kel-top's — goes through this shim.
mkdir -p "$WORK/shim"
printf '#!/bin/sh\nexec %s -f /dev/null "$@"\n' "$(command -v tmux)" > "$WORK/shim/tmux"
chmod +x "$WORK/shim/tmux"
export PATH="$WORK/shim:$PATH"
STATE="$XDG_STATE_HOME/kel"
SESSIONS="$STATE/sessions"

# Build the Go binaries from THIS checkout, not whatever an earlier install.sh
# left in ~/.local/bin: bin/kel has no fallback reader any more (v0.10), so a
# stale installed kel-fleet would be what the suite actually tested.
command -v go >/dev/null 2>&1 || { echo "the suite needs Go (kel-fleet, kel-top)" >&2; exit 1; }
export KEL_FLEET_BIN="$WORK/kel-fleet" KEL_TOP_BIN="$WORK/kel-top"
( cd "$HERE/.." && go build -o "$KEL_FLEET_BIN" ./cmd/kel-fleet && go build -o "$KEL_TOP_BIN" ./cmd/kel-top ) \
  || { echo "go build failed" >&2; exit 1; }

cleanup() { tmux kill-server 2>/dev/null; rm -rf "$WORK" "$TMUX_TMPDIR"; }
trap cleanup EXIT INT TERM

pass=0; fail=0; failed=()
ok() {  # description  test-expression
  if eval "$2"; then printf '  ok   %s\n' "$1"; pass=$((pass+1))
  else printf '  \033[31mFAIL\033[0m %s\n' "$1"; fail=$((fail+1)); failed+=("$1")
       [ "$VERBOSE" = 1 ] && printf '       expr: %s\n' "$2"; fi
}
section() { printf '\n\033[1m== %s ==\033[0m\n' "$*"; }

# Poll for a condition instead of sleeping a guessed interval.  A fixed sleep
# is either slower than it needs to be or occasionally too short, and the
# second kind reddens CI at random — this suite already did it once, on a 0.8s
# wait for a SIGINT'd process to actually die.
wait_until() {   # 'test expression'  [max tenths of a second, default 50]
  local i=0 max="${2:-50}"
  while [ "$i" -lt "$max" ]; do
    eval "$1" 2>/dev/null && return 0
    i=$((i + 1)); sleep 0.1
  done
  return 1
}

reset() {
  tmux kill-server 2>/dev/null; sleep 0.4
  # Start the server ourselves with NO user config.  Otherwise the suite
  # inherits whatever ~/.tmux.conf does — on a developer box that sources
  # kel.conf and sets base-index 1, on a CI runner there is no file at all —
  # and the two environments quietly disagree about how windows are numbered.
  tmux -f /dev/null start-server 2>/dev/null
  rm -rf "$XDG_STATE_HOME" "$WORK/repos"; mkdir -p "$STATE"
  local r
  for r in api-gw coppermind; do
    mkdir -p "$WORK/repos/$r"
    ( cd "$WORK/repos/$r" && git init -q -b main . \
      && echo hi > README.md && git add -A \
      && git -c user.email=t@t -c user.name=t commit -qm init ) >/dev/null 2>&1
  done
}
# run kel from inside repo $1
K() { local r="$1"; shift; ( cd "$WORK/repos/$r" && "$KEL" "$@" ); }
wins() { tmux list-windows -a -F x 2>/dev/null | grep -c x; }
grps() { tmux list-sessions -F '#{session_name}' 2>/dev/null | grep -c '^keltest'; }   # kel's sessions
# feed a Claude Code hook payload for pane $1, event $2, notification_type $3, text $4
hook() {
  printf '%s' "{\"session_id\":\"s\",\"cwd\":\"$WORK\",\"hook_event_name\":\"$2\"\
${3:+,\"notification_type\":\"$3\"}${4:+,\"notification_text\":\"$4\"}}" \
    | TMUX_PANE="$1" "$KEL" hook "$2"
}
state_of() { "$KEL" ls 2>/dev/null | awk -v n="$1" '$2==n {print $3}'; }
# Look windows up by NAME, never by index.  kel.conf sets base-index 1, so a
# developer box numbers windows from 1 while a bare tmux (CI, a fresh machine)
# numbers from 0 — hardcoding ":1" silently targets the wrong window, or none.
# `kel new` starts the agent with send-keys, so for a moment the pane is still
# a bare shell and effective_state correctly calls that `dead`.  Wait for the
# process before asserting on state.  v0.6 made the fleet read 3x faster, which
# is what first lost this race.
# the sidebar runs kel-top, a non-shell, so it is excluded — else this would
# report the agent up before it has started
agent_up() { wait_until "tmux list-panes -t '$1' -F '#{@kel-sidebar}|#{pane_current_command}' 2>/dev/null | awk -F'|' '\$1==\"\"{print \$2}' | grep -qvxE 'bash|zsh|fish|sh|dash|tmux'"; }
NEW() { K "$1" new "$2" --agent 'sleep 9999' >/dev/null 2>&1; agent_up "$(window_of "$1" "$2")"; }
# By group AND name: since v0.10 every window shares one session, so two
# repos' `docs` agents are two windows in it, told apart by @kel-group.
window_of() { tmux list-windows -a -F '#{session_name} #{@kel-group} #{window_id} #{window_name}' 2>/dev/null \
                | awk -v g="$1" -v n="$2" '$1 ~ /^keltest/ && $2==g && $4==n {print $3; exit}'; }
pane_of()   { local w; w="$(window_of "$1" "$2")"; [ -n "$w" ] && tmux list-panes -t "$w" -F '#{pane_id}' | head -1; }

# ---------------------------------------------------------------- isolation
section "the suite cannot reach your real tmux server"
tmux new-session -d -s isocheck -c /tmp 2>/dev/null
ok "our socket lives under \$TMUX_TMPDIR"  '[ -n "$(find "$TMUX_TMPDIR" -type s 2>/dev/null)" ]'
ok "  ...and its path fits in sun_path"   '[ "${#TMUX_TMPDIR}" -lt 70 ]'
ok "and not on the default socket"        '[ ! -S "/tmp/tmux-$(id -u)/default" ] || ! tmux -S "/tmp/tmux-$(id -u)/default" has-session -t isocheck 2>/dev/null'
tmux kill-server 2>/dev/null

# ---------------------------------------------------------------- records
section "records are keyed by group AND name"
reset
NEW api-gw docs
NEW coppermind docs
ok "the same name works in two repos"   '[ "$(wins)" = 2 ]'
ok "one record per group"               '[ -f "$SESSIONS/api-gw/docs.json" ] && [ -f "$SESSIONS/coppermind/docs.json" ]'
ok "ls shows both"                      '[ "$("$KEL" ls 2>/dev/null | grep -c "  docs")" = 2 ]'
# the WHERE column is a render-time rename; the record keeps saying "inplace"
ok "ls prints a column header"           '[ -n "$("$KEL" ls 2>/dev/null | grep "IDX .*NAME .*STATE .*WHERE .*BRANCH")" ]'
ok "  ...and an in-place agent is repo"  '[ -n "$("$KEL" ls 2>/dev/null | awk "\$2==\"docs\"" | grep -w repo)" ]'
ok "  ...while the record says inplace"  '[ "$(jq -r .isolation "$SESSIONS/api-gw/docs.json")" = inplace ]'
ok "  ...and piped output has no ANSI"   '! "$KEL" ls 2>/dev/null | grep -q "$(printf "\033")"'

section "ambiguity is reported, never guessed"
out="$("$KEL" kill docs 2>&1)"
ok "a bare ambiguous kill refuses"      '[[ "$out" == *ambiguous* ]]'
ok "  ...and names the candidates"      '[[ "$out" == *api-gw/docs* && "$out" == *coppermind/docs* ]]'
"$KEL" kill coppermind/docs >/dev/null 2>&1
ok "group/name kills exactly one"       '[ "$(wins)" = 1 ]'
ok "  ...and it is the right one"       '[ -f "$SESSIONS/api-gw/docs.json" ] && [ ! -f "$SESSIONS/coppermind/docs.json" ]'
ok "a now-unique bare name works"       '"$KEL" kill docs >/dev/null 2>&1; [ ! -f "$SESSIONS/api-gw/docs.json" ]'

section "rename stays inside its group"
reset
NEW api-gw alpha
NEW coppermind alpha
w="$(window_of api-gw alpha)"
# kel rename acts on the window it is run from; TMUX + TMUX_PANE is how tmux
# itself tells a command where it is standing
TMUX="$TMUX_TMPDIR/tmux-$(id -u)/default,$(tmux display-message -p '#{pid}'),0" \
  TMUX_PANE="$(tmux list-panes -t "$w" -F '#{pane_id}' | head -1)" "$KEL" rename beta >/dev/null 2>&1
ok "rename moves the record"            '[ -f "$SESSIONS/api-gw/beta.json" ] && [ ! -f "$SESSIONS/api-gw/alpha.json" ]'
ok "  ...and leaves the other group"    '[ -f "$SESSIONS/coppermind/alpha.json" ]'
ok "  ...and rewrites .name"            '[ "$(jq -r .name "$SESSIONS/api-gw/beta.json")" = beta ]'

section "state is private, and the network is optional"
ok "the state dir is 0700"              '[ "$(ls -ld "$XDG_STATE_HOME/kel" | cut -c1-10)" = "drwx------" ]'
# pr = off must stop the only call that leaves the machine.  Until v0.10 this
# was asserted against bash's own PR helper only -- and kel-fleet, which
# answers whenever it is installed, ignored the setting entirely.  Driven
# through the fleet now, so the assertion is on the code that actually runs.
#
# The fixture has to get PAST every local answer to reach the gh half: a
# github.com origin (or there is no slug to ask about), a feature branch (on
# the base branch the PR state is moot), and a remote-tracking ref at HEAD (or
# it short-circuits as unpushed).  update-ref fakes "pushed" with no network.
# %s, not a literal $WORK: single quotes would write the variable NAME into
# the script, which then has no $WORK set and logs to /gh.calls
mkdir -p "$WORK/nogh2"
printf '#!/bin/sh\necho CALLED >> "%s/gh.calls"\nexit 1\n' "$WORK" > "$WORK/nogh2/gh"
chmod +x "$WORK/nogh2/gh"
GHR="$WORK/repos/ghrepo"; mkdir -p "$GHR"
# -b main and an explicit identity, like every other fixture here: a CI runner
# has no git user configured, so a bare `git commit` fails and leaves no HEAD.
git -C "$GHR" init -q -b main
git -C "$GHR" -c user.email=t@t -c user.name=t commit -q --allow-empty -m init
git -C "$GHR" remote add origin https://github.com/example/ghrepo.git
git -C "$GHR" update-ref refs/remotes/origin/main HEAD
git -C "$GHR" checkout -q -b feat
git -C "$GHR" -c user.email=t@t -c user.name=t commit -q --allow-empty -m feat
git -C "$GHR" update-ref refs/remotes/origin/feat HEAD
NEW ghrepo gh1
GHLAND() { PATH="$WORK/nogh2:$PATH" "$@" "$KEL" _fleet --land --pr | jq -r '.agents[]|select(.name=="gh1")|.land.code'; }
rm -f "$WORK/gh.calls" "$STATE/pr-cache/"* 2>/dev/null
code="$(GHLAND env)"
ok "pr on reaches gh"                   '[ -f "$WORK/gh.calls" ]'
# Invariant 3 in its own wording: "an expired gh token must not render as no
# PR".  A gh that exits nonzero against a real github remote reads `unknown`,
# never a state that looks like a settled answer.
ok "  ...and a failing gh reads unknown" '[ "$code" = unknown ]'
rm -f "$WORK/gh.calls" "$STATE/pr-cache/"* 2>/dev/null
GHLAND env KEL_PR=off >/dev/null
ok "  ...and pr = off does not"         '[ ! -f "$WORK/gh.calls" ]'
# local git answers first: uncommitted work outranks every PR state, so it
# must not be the reason kel talks to github.com
echo wip > "$GHR/wip.txt"
rm -f "$WORK/gh.calls" "$STATE/pr-cache/"* 2>/dev/null
code="$(GHLAND env)"
ok "  ...and dirty work never reaches gh" '[ ! -f "$WORK/gh.calls" ]'
ok "  ...still answering dirty"         '[ "$code" = dirty ]'
rm -f "$GHR/wip.txt"

section "legacy flat records migrate"
reset
NEW api-gw legacy
mv "$SESSIONS/api-gw/legacy.json" "$SESSIONS/legacy.json"; rmdir "$SESSIONS/api-gw"
jq 'del(.group)' "$SESSIONS/legacy.json" > "$WORK/l" && mv "$WORK/l" "$SESSIONS/legacy.json"
"$KEL" ls >/dev/null 2>&1
ok "a flat pre-v0.2 record is filed"    '[ -f "$SESSIONS/api-gw/legacy.json" ]'
ok "  ...with a group derived from repo" '[ "$(jq -r .group "$SESSIONS/api-gw/legacy.json")" = api-gw ]'

section "_fleet is one computed view, and every surface agrees with it"
reset
NEW api-gw a
NEW coppermind b
P="$(pane_of api-gw a)"
hook "$P" Notification permission_prompt "needs Bash"
ok "emits valid JSON"                    '"$KEL" _fleet | jq -e . >/dev/null'
ok "one entry per live agent"            '[ "$("$KEL" _fleet | jq ".agents|length")" = 2 ]'
ok "carries state from the hook"         '[ "$("$KEL" _fleet | jq -r ".agents[]|select(.name==\"a\")|.state")" = waiting ]'
ok "  ...and the note with it"           '[[ "$("$KEL" _fleet | jq -r ".agents[]|select(.name==\"a\")|.note")" == *"needs Bash"* ]]'
ok "dirty is null unless asked for"      '[ "$("$KEL" _fleet | jq -r ".agents[0].dirty")" = null ]'
ok "  ...and computed when it is"        '[ "$("$KEL" _fleet --dirty | jq -r ".agents[0].dirty")" != null ]'
ok "ls agrees with _fleet on state"      '[ "$(state_of a)" = "$("$KEL" _fleet | jq -r ".agents[]|select(.name==\"a\")|.state")" ]'
# a record whose window is gone must still appear, flagged, not vanish
"$KEL" kill coppermind/b >/dev/null 2>&1
NEW api-gw c
mv "$SESSIONS/api-gw/c.json" "$SESSIONS/api-gw/ghost.json"
jq '.name="ghost"' "$SESSIONS/api-gw/ghost.json" > "$WORK/g" && mv "$WORK/g" "$SESSIONS/api-gw/ghost.json"
ok "an orphaned record shows as dead"    '[ "$("$KEL" _fleet | jq -r ".agents[]|select(.name==\"ghost\")|.state")" = dead ]'
ok "  ...with a null window_id"          '[ "$("$KEL" _fleet | jq -r ".agents[]|select(.name==\"ghost\")|.window_id")" = null ]'
# #25: a record whose JSON does not even parse must still render, distinctly
# -- not vanish, and (the worse bug this actually was) not take the rest of
# the document with it. Before the fix, --slurpfile aborted on the first bad
# value it hit in the concatenated stream, so this single corrupt file used
# to zero out every OTHER agent's entry too, not just its own.  That was the
# bash reader; Go's json.Unmarshal fails per record, and this keeps it so.
BEFORE_N="$("$KEL" _fleet | jq '.agents | length')"
printf 'not json {{{' > "$SESSIONS/api-gw/garbage.json"
ok "a corrupt record keeps the other agents" \
  '[ "$("$KEL" _fleet | jq ".agents | length")" = "$((BEFORE_N + 1))" ]'
ok "  ...and renders itself as unreadable" \
  '[ "$("$KEL" _fleet | jq -r ".agents[]|select(.name==\"garbage\")|.state")" = unreadable ]'
ok "  ...named from its path, group included" \
  '[ "$("$KEL" _fleet | jq -r ".agents[]|select(.name==\"garbage\")|.group")" = api-gw ]'
ok "  ...and _fleet still exits 0"       '"$KEL" _fleet >/dev/null 2>&1'
rm -f "$SESSIONS/api-gw/garbage.json"
# An agent that crashes right after SessionStart (idle) used to stay reading
# "idle" forever: the dead-check only looked at working/waiting/throttled,
# treating idle as "no signal yet" when it is actually a real one -- SessionStart
# already fired. Found by walking through the tool as a fresh user, not by
# inspection, and it was wrong in THREE independent places: this jq pipeline,
# the standalone effective_state() bash function, and internal/fleet/fleet.go.
# (The jq pipeline went with the bash reader in v0.10.)
reset
NEW api-gw idler
P="$(pane_of api-gw idler)"
hook "$P" SessionStart
ok "SessionStart sets idle"              '[ "$(state_of idler)" = idle ]'
PPID_="$(tmux list-panes -t "$(window_of api-gw idler)" -F "#{pane_pid}")"
kill -9 "$PPID_" 2>/dev/null
wait_until '[ "$(tmux list-panes -t "$(window_of api-gw idler)" -F "#{pane_current_command}")" = bash ]'
ok "an idle agent that crashes reads dead" '[ "$(state_of idler)" = dead ]'
ok "  ...and _fleet agrees"              '[ "$("$KEL" _fleet | jq -r ".agents[]|select(.name==\"idler\")|.state")" = dead ]'

# empty fleet must be a document, not an error
reset
ok "an empty fleet is {agents:[]}"       '[ "$("$KEL" _fleet | jq -c ".agents")" = "[]" ]'
ok "  ...and still exits 0"              '"$KEL" _fleet >/dev/null 2>&1'
ok "  ...with nothing on stderr"         '[ -z "$("$KEL" _fleet 2>&1 >/dev/null)" ]'

# No fallback reader since v0.10, so a missing or broken kel-fleet has to fail
# where you can see it.  An empty document would read as "no agents" -- a
# guess reported as a fact (invariant 3).
printf '#!/bin/sh\nexit 2\n' > "$WORK/stale"; chmod +x "$WORK/stale"
ok "a missing kel-fleet fails loudly"    '! KEL_FLEET_BIN=/nonexistent "$KEL" _fleet >/dev/null 2>&1'
ok "  ...and says how to fix it"         '[[ "$(KEL_FLEET_BIN=/nonexistent "$KEL" ls 2>&1)" == *install.sh* ]]'
ok "  ...as does a broken one"           '! KEL_FLEET_BIN="$WORK/stale" "$KEL" ls >/dev/null 2>&1'
ok "  ...and ls prints no empty table"   '[[ "$(KEL_FLEET_BIN=/nonexistent "$KEL" ls 2>/dev/null)" != *NAME* ]]'
ok "the bar names the problem too"       '[[ "$(TMUX=x KEL_FLEET_BIN=/nonexistent "$KEL" status-line)" == *"kel-fleet missing"* ]]'

section "kel top renders and sorts (#2)"
GOBIN_TOP="$KEL_TOP_BIN"
{
  reset
  NEW api-gw idler
  NEW api-gw blocked
  NEW coppermind busy
  hook "$(pane_of api-gw blocked)"   Notification permission_prompt "needs Bash"
  hook "$(pane_of coppermind busy)"  UserPromptSubmit
  printf '%s' '{"context_window":{"used_percentage":93,"context_window_size":200000,"total_input_tokens":9},"cost":{"total_cost_usd":18.7}}' \
    | TMUX_PANE="$(pane_of coppermind busy)" "$KEL" statusline >/dev/null
  frame() { COLUMNS="${1:-120}" "$GOBIN_TOP" --once 2>/dev/null | sed 's/\x1b\[[0-9;]*m//g'; }
  # Widest line in CHARACTERS. Not `wc -L`: GNU counts display columns, macOS
  # counts bytes, so the multibyte › and … made every frame look too wide on
  # the Mac runner. perl -CS decodes UTF-8 and is on both.
  maxw() { perl -CS -ne 'chomp; $m = length if length > $m; END { print $m + 0 }'; }
  ok "renders a header"                    '[[ "$(frame)" == *"GROUP"*"AGENT"*"STATE"* ]]'
  ok "lists every agent"                   '[ "$(frame | grep -cE "^.? *(api-gw|coppermind) ")" = 3 ]'
  ok "triage puts waiting first"           '[[ "$(frame | sed -n 2p)" == *blocked*waiting* ]]'
  ok "  ...working above idle"             '[ "$(frame | grep -n busy | cut -d: -f1)" -lt "$(frame | grep -n idler | cut -d: -f1)" ]'
  ok "shows context and cost"              '[[ "$(frame)" == *"93%"* && "$(frame)" == *"18.70"* ]]'
  ok "draws the key hints"                 '[[ "$(frame)" == *"down"* && "$(frame)" == *"q/esc/ctrl+c quit"* ]]'
  # Phase 2 / #27: these six were handled and appeared in no hint line.
  # help.Model renders from keys.go's bindings now, so they come back for
  # free -- checked individually rather than trusted on the strength of that
  # claim.
  ok "  ...g and G, previously undocumented"     '[[ "$(frame)" == *"g top"* && "$(frame)" == *"G bottom"* ]]'
  ok "  ...r, previously undocumented"           '[[ "$(frame)" == *"r reload"* ]]'
  ok "  ...esc and ctrl+c, previously undocumented" '[[ "$(frame)" == *"q/esc/ctrl+c quit"* ]]'
  # backspace only has meaning in filter mode, which --once cannot reach (it
  # renders one frame with no keys processed) -- checked below instead, in
  # the interactive tmux pane already needed for the enter no-op test.
  ok "80 cols still fits the table"        '[ "$(frame 80 | head -1 | maxw)" -le 80 ]'
  ok "50 cols drops columns, not rows"     '[ "$(frame 50 | grep -cE "^.? *(api-gw|coppermind) ")" = 3 ]'
  ok "  ...and stays within 50"            '[ "$(frame 50 | maxw)" -le 50 ]'
  ok "no trailing whitespace on any row"   '! frame | grep -q " $"'

  # #4/§4a: LAND outranks CTX and $, and the show/hide gates are derived from
  # the column widths rather than hand-copied -- which is exactly how the
  # previous numbers (58/66/74 against a 76-column budget) drifted. Proven by
  # rendering, not by re-deriving the arithmetic a second time here.
  widths_ok() {
    local w got
    for w in 50 55 58 59 60 64 65 66 67 68 74 75 76 77 80 85 90; do
      got="$(frame "$w" | maxw)"
      if [ "$got" -gt "$w" ]; then
        echo "    overflow: w=$w rendered $got" >&2
        return 1
      fi
    done
    return 0
  }
  ok "width sweep 50-90 never overflows the table" 'widths_ok'
  ok "  ...and LAND shows before CTX and \$ as it widens" \
    '[[ "$(frame 60 | head -1)" == *LAND* && "$(frame 60 | head -1)" != *CTX* ]]'

  # #6/#26: header and footer drop before the body is ever asked below one
  # row. LINES drives this the same way COLUMNS already drives the width
  # sweep above -- there is no real terminal in this suite either.
  heights_ok() {
    local h out n
    for h in 1 2 3 4; do
      out="$(COLUMNS=100 LINES="$h" "$GOBIN_TOP" --once 2>/dev/null)"
      n="$(printf '%s\n' "$out" | awk 'END{print NR}')"
      if [ "$n" -gt "$h" ]; then
        echo "    overflow: h=$h rendered $n lines" >&2
        return 1
      fi
    done
    return 0
  }
  ok "height sweep 1-4 never overflows the terminal" 'heights_ok'

  # #3/#27, the bidirectional guard: every key Update() matches is one of
  # keys.go's bindings (dumped here), and every binding in the dump is one
  # Update() actually matches -- checked by hand against this literal list
  # rather than against keys.go itself, so a change to one side without the
  # other is a real diff, not a tautology.
  EXPECTED_HELP_KEYS="$(sort <<'EOF'
normal	k
normal	up
normal	j
normal	down
normal	g
normal	home
normal	G
normal	end
normal	s
normal	/
normal	r
normal	enter
normal	?
normal	q
normal	esc
normal	ctrl+c
filter	enter
filter	esc
filter	backspace
filter	ctrl+c
EOF
)"
  ACTUAL_HELP_KEYS="$("$GOBIN_TOP" --dump-help-keys | awk -F'\t' '{print $1"\t"$2}' | sort)"
  ok "the bidirectional help guard (every handled key <-> every documented one)" \
    '[ "$EXPECTED_HELP_KEYS" = "$ACTUAL_HELP_KEYS" ]'
  # prove it can fail: drop one binding from the actual side and confirm the
  # comparison above would have caught it.
  ACTUAL_MINUS_G="$(printf '%s\n' "$ACTUAL_HELP_KEYS" | grep -v '^normal	g$')"
  ok "  ...and a missing key would be caught" '[ "$EXPECTED_HELP_KEYS" != "$ACTUAL_MINUS_G" ]'

  # #3: enter now matches a case in normal mode -- an explicit no-op, not a
  # keypress with no effect and no case to find it in (Phase 2's "must not:
  # no actions yet" is the reason it stays a no-op). Driven through a real
  # tmux pane rather than reasoned about, per CLAUDE.md's testing guidance.
  TWIN="$(tmux new-window -d -P -F '#{window_id}' -t '=keltest:' -n kt "COLUMNS=100 LINES=24 $GOBIN_TOP")"
  wait_until "tmux capture-pane -p -t '$TWIN' 2>/dev/null | grep -q GROUP"
  # The cursor marker exists from the very first frame (on row 0), so waiting
  # for it to merely be PRESENT races the keypress: it can be satisfied by
  # the frame rendered before "j" was even processed. Wait for it to move
  # OFF that first row instead -- a positive, unambiguous signal that "j" has
  # actually been applied -- before capturing the position enter must not
  # then change.
  # Line NUMBER only: the cursor row also carries the FOR column, which ticks
  # every second -- comparing the whole line made this fail whenever a second
  # boundary fell between the two captures.
  curline() { tmux capture-pane -p -t "$TWIN" 2>/dev/null | grep -n '›' | cut -d: -f1; }
  CUR0="$(curline)"
  tmux send-keys -t "$TWIN" j
  wait_until '[ "$(curline)" != "$CUR0" ]'
  CUR1="$(curline)"
  tmux send-keys -t "$TWIN" Enter
  sleep 0.3   # nothing to wait FOR: asserting silence, same idiom as #1's notify test
  CUR2="$(curline)"
  ok "enter is a genuine no-op in normal mode (#3)" '[ "$CUR1" = "$CUR2" ]'
  ok "  ...and the program is still running, not crashed" \
    '[ "$(tmux list-panes -t "$TWIN" -F "#{pane_current_command}" 2>/dev/null)" != bash ]'
  # backspace was the sixth undocumented key (#27) and only means anything in
  # filter mode, which --once cannot reach -- this is the one check in this
  # section that needs the live pane rather than a rendered frame.
  tmux send-keys -t "$TWIN" /
  wait_until "tmux capture-pane -p -t '$TWIN' 2>/dev/null | grep -q backspace"
  ok "backspace, previously undocumented, in filter mode's hints" \
    '[[ "$(tmux capture-pane -p -t "$TWIN" 2>/dev/null)" == *backspace* ]]'
  tmux kill-window -t "$TWIN" 2>/dev/null
}

section "land: one verb per row, degrading without gh (#6)"
reset
NEW api-gw solo
NEW coppermind solo2
# code/n for one agent, read the way kel sweep reads it.  The non-git and
# missing-directory cases live in internal/fleet/land_test.go: a kel agent
# cannot be started in a directory that does not exist.
L() { "$KEL" _fleet --land | jq -r --arg n "$1" '.agents[]|select(.name==$n)|.land|"\(.code)/\(.n // "")"'; }
ok "a repo with no remote is clean"      '[ "$(L solo)" = "clean/" ]'
echo wip > "$WORK/repos/api-gw/wip.txt"
ok "uncommitted work -> dirty N"         '[ "$(L solo)" = "dirty/1" ]'
( cd "$WORK/repos/api-gw" && git add -A && git -c user.email=t@t -c user.name=t commit -qm w ) >/dev/null 2>&1
ok "committed, no remote -> clean"       '[ "$(L solo)" = "clean/" ]'
# a remote makes unpushed meaningful
git -C "$WORK/repos/coppermind" remote add origin "$WORK/repos/api-gw" 2>/dev/null
echo z > "$WORK/repos/coppermind/z.txt"
( cd "$WORK/repos/coppermind" && git add -A && git -c user.email=t@t -c user.name=t commit -qm z ) >/dev/null 2>&1
ok "commits no remote has -> unpushed N" '[[ "$(L solo2)" == unpushed/* ]]'
ok "a non-GitHub remote is never unknown" '[[ "$(L solo2)" != unknown/* ]]'
ok "--land fills the document"           '[ "$("$KEL" _fleet --land | jq -r ".agents[0].land.code")" != null ]'
ok "  ...and plain _fleet leaves it null" '[ "$("$KEL" _fleet | jq -r ".agents[0].land")" = null ]'
# Prove it needs no network by taking gh away, rather than by timing it:
# `timeout` is GNU coreutils and absent on macOS — caught by CI, which is the
# whole reason the macOS runner exists.
mkdir -p "$WORK/nogh"; printf '#!/bin/sh\nexit 127\n' > "$WORK/nogh/gh"; chmod +x "$WORK/nogh/gh"
ok "  ...and needs no gh at all"         '[ "$(PATH="$WORK/nogh:$PATH" "$KEL" _fleet --land | jq -Sc ".agents[].land")" = "$("$KEL" _fleet --land | jq -Sc ".agents[].land")" ]'

section ".kel/group pins a directory to a group (#12)"
reset
mkdir -p "$WORK/repos/api-gw/svc/deep"
NEWD() { ( cd "$1" && "$KEL" new "$2" --agent 'sleep 9999' ) >/dev/null 2>&1; agent_up "$(tmux list-windows -a -F '#{window_id} #{window_name}' | awk -v n="$2" '$2==n{print $1}')"; }
NEWD "$WORK/repos/api-gw/svc" plain
ok "no file: group is the repo"          '[ "$(jq -r .group "$SESSIONS/api-gw/plain.json")" = api-gw ]'
mkdir -p "$WORK/repos/api-gw/svc/.kel"; echo billing > "$WORK/repos/api-gw/svc/.kel/group"
NEWD "$WORK/repos/api-gw/svc" pinned
ok "the file pins the group"             '[ -f "$SESSIONS/billing/pinned.json" ]'
NEWD "$WORK/repos/api-gw/svc/deep" nested
ok "  ...and applies to subdirectories"  '[ -f "$SESSIONS/billing/nested.json" ]'
NEWD "$WORK/repos/api-gw" outside
ok "  ...but not to a sibling"           '[ -f "$SESSIONS/api-gw/outside.json" ]'
printf 'not a slug!\n' > "$WORK/repos/api-gw/svc/.kel/group"
out="$( ( cd "$WORK/repos/api-gw/svc" && "$KEL" new bad --agent 'sleep 9999' ) 2>&1 )"
ok "a non-slug is refused, not truncated" '[[ "$out" == *"not a slug"* ]] && [ -f "$SESSIONS/api-gw/bad.json" ]'
printf '  billing  \r\n' > "$WORK/repos/api-gw/svc/.kel/group"
NEWD "$WORK/repos/api-gw/svc" trimmed
ok "whitespace and CRLF are tolerated"   '[ -f "$SESSIONS/billing/trimmed.json" ]'
echo billing > "$WORK/repos/api-gw/svc/.kel/group"
ok "\$KEL_GROUP still outranks the file"  '( cd "$WORK/repos/api-gw/svc" && KEL_GROUP=forced "$KEL" new f --agent "sleep 9999" ) >/dev/null 2>&1; [ -f "$SESSIONS/forced/f.json" ]'

section "kel sweep closes out what has landed (#8)"
reset
git init -q --bare -b main "$WORK/origin.git"
git init -q -b main "$WORK/sw" && ( cd "$WORK/sw" && git remote add origin "$WORK/origin.git" \
  && echo a>f && git add -A && git -c user.email=t@t -c user.name=t commit -qm i && git push -qu origin main ) >/dev/null 2>&1
for n in landed openwork messy; do ( cd "$WORK/sw" && "$KEL" new "$n" -w --agent 'sleep 9999' ) >/dev/null 2>&1; done
SWT="$WORK/.kel-worktrees"
( cd "$SWT/sw-landed" && echo x>x && git add -A && git -c user.email=t@t -c user.name=t commit -qm x && git push -q origin landed ) >/dev/null 2>&1
( cd "$WORK/sw" && git fetch -q && git merge -q --no-edit origin/landed && git push -q origin main ) >/dev/null 2>&1
( cd "$SWT/sw-landed" && git fetch -q ) >/dev/null 2>&1
( cd "$SWT/sw-openwork" && echo y>y && git add -A && git -c user.email=t@t -c user.name=t commit -qm y && git push -q origin openwork && git fetch -q ) >/dev/null 2>&1
( cd "$SWT/sw-messy" && echo z>z )
for n in landed openwork messy; do hook "$(pane_of sw "$n")" Stop; done
ok "merged is detected without gh"       '[ "$("$KEL" _fleet --land | jq -r ".agents[]|select(.name==\"landed\")|.land.code")" = merged ]'
ok "-n sweeps nothing"                   '"$KEL" sweep -n >/dev/null 2>&1; [ -d "$SWT/sw-landed" ]'
ok "  ...but says what it would take"    '[[ "$("$KEL" sweep -n 2>&1)" == *"would sweep"*landed* ]]'
"$KEL" sweep >/dev/null 2>&1
ok "sweep removes the merged worktree"   '[ ! -d "$SWT/sw-landed" ]'
ok "  ...and its record"                 '[ ! -f "$SESSIONS/sw/landed.json" ]'
ok "keeps the unmerged one"              '[ -d "$SWT/sw-openwork" ] && [ -f "$SESSIONS/sw/openwork.json" ]'
ok "keeps the dirty one"                 '[ -d "$SWT/sw-messy" ] && [ -f "$SESSIONS/sw/messy.json" ]'
ok "  ...and says why for each"          'o="$("$KEL" sweep 2>&1)"; [[ "$o" == *"uncommitted work"* ]]'
ok "-f still refuses uncommitted work"   '"$KEL" sweep -f >/dev/null 2>&1; [ -d "$SWT/sw-messy" ]'

section "config.toml (#10-adjacent, v0.8)"
reset
CH="$WORK/cfghome"; mkdir -p "$CH/.config/kel" "$WORK/repos/api-gw/.kel"
printf 'ctx_warn = 55\nnotify = "waiting dead"\n' > "$CH/.config/kel/config.toml"
CFG() { HOME="$CH" XDG_CONFIG_HOME="$CH/.config" bash -c "cd '$1' && '$KEL' _cfgdump"; }
ok "the user config is read"             '[[ "$(CFG "$CH")" == *"ctx_warn=55"* ]]'
ok "  ...including multi-word values"    '[[ "$(CFG "$CH")" == *"notify=waiting dead"* ]]'
printf 'ctx_warn = 80\n' > "$WORK/repos/api-gw/.kel/config.toml"
ok "a repo config outranks the user one" '[[ "$(CFG "$WORK/repos/api-gw")" == *"ctx_warn=80"* ]]'
ok "  ...and inherits what it omits"     '[[ "$(CFG "$WORK/repos/api-gw")" == *"notify=waiting dead"* ]]'
mkdir -p "$WORK/repos/api-gw/a/b"
ok "  ...and applies in subdirectories"  '[[ "$(CFG "$WORK/repos/api-gw/a/b")" == *"ctx_warn=80"* ]]'
ok "an exported var outranks both"       '[[ "$(HOME="$CH" XDG_CONFIG_HOME="$CH/.config" KEL_CTX_WARN=99 bash -c "cd $WORK/repos/api-gw && \"$KEL\" _cfgdump")" == *"ctx_warn=99"* ]]'
printf '[section]\n# a comment\n\n  ctx_warn   =   42   # trailing\n' > "$CH/.config/kel/config.toml"
rm -f "$WORK/repos/api-gw/.kel/config.toml"
ok "sections, comments and spacing"      '[[ "$(CFG "$CH")" == *"ctx_warn=42"* ]]'
printf 'nonsense line with no equals\nBAD_KEY = 1\nctx_warn = 7\n' > "$CH/.config/kel/config.toml"
ok "junk lines are skipped, not fatal"   '[[ "$(CFG "$CH")" == *"ctx_warn=7"* ]]'
ok "the shipped example parses"          'HOME="$CH" XDG_CONFIG_HOME="$CH/.config" bash -c "mkdir -p $CH/.config/kel && cp $HERE/../examples/config.toml $CH/.config/kel/config.toml && cd $CH && \"$KEL\" _cfgdump" >/dev/null'

section "kel config (v0.9: it had no path into it at all before this)"
reset
CFH="$WORK/cfghome2"; mkdir -p "$CFH/.config"
# Invoked through a SYMLINK, matching the real install shape (~/.local/bin/kel
# is always a symlink to bin/kel, never a copy). That is the exact case
# _real_self exists for: a naive $(dirname "$SELF") stays pointed at the
# symlink's own directory, which has no examples/ next to it.
KELLINK="$WORK/kel-symlink"; ln -sf "$KEL" "$KELLINK"
CFGRUN() { HOME="$CFH" XDG_CONFIG_HOME="$CFH/.config" EDITOR=cat "$KELLINK" config; }
out="$(CFGRUN 2>&1)"
ok "seeds config.toml on first run"      '[ -f "$CFH/.config/kel/config.toml" ]'
ok "  ...from the REAL shipped example"  '[[ "$out" == *"copy to ~/.config/kel/config.toml"* ]]'
ok "  ...found through the symlink"      '[[ "$out" == *"created"*"from the shipped example"* ]]'
ok "  ...cat (the fake EDITOR) ran on it" '[[ "$out" == *"[fleet]"* ]]'
echo "# a mark that must survive" >> "$CFH/.config/kel/config.toml"
out2="$(CFGRUN 2>&1)"
ok "a second run does not re-seed"       '[[ "$out2" != *created* ]]'
ok "  ...and the file is untouched"      '[[ "$out2" == *"a mark that must survive"* ]]'
mkdir -p "$WORK/noeditor"
for b in bash sh jq tmux cat mkdir dirname basename cp printf readlink date grep sed awk mktemp rm mv; do
  [ -e "$WORK/noeditor/$b" ] || ln -sf "$(command -v "$b" 2>/dev/null)" "$WORK/noeditor/$b" 2>/dev/null
done
# captured, then pattern-matched — not piped through grep inside the eval'd
# expression, which the ok() harness does not reliably propagate an exit
# status through here
noed_out="$(env -u EDITOR -u VISUAL HOME="$CFH" XDG_CONFIG_HOME="$CFH/.config" PATH="$WORK/noeditor" "$KELLINK" config 2>&1)"
ok "with no editor anywhere, it says so, not silently failing" \
  '[[ "$noed_out" == *"no editor found"* ]]'


section "the system around the agent (v0.9a)"
reset
NEW api-gw agent1
W1="$(window_of api-gw agent1)"; P1="$(pane_of api-gw agent1)"
# A — state on the pane border, so a full-screen agent still signals
border() { tmux show-options -w -t "$W1" pane-border-style 2>/dev/null | awk '{print $2}'; }
hook "$P1" UserPromptSubmit
ok "working leaves the border alone"     '[ -z "$(border)" ]'
hook "$P1" Notification permission_prompt "x"
ok "waiting paints the border"           '[ -n "$(border)" ]'
hook "$P1" Notification quota_auto_resume_fired "q"
ok "  ...a different colour when throttled" '[ "$(border)" != "" ]'
hook "$P1" SessionEnd
ok "session end clears it"               '[ -z "$(border)" ]'

ok "kel new -w still makes a worktree"   'NEW_W() { ( cd "$WORK/repos/api-gw" && "$KEL" new wt1 -w --agent "sleep 9999" ) >/dev/null 2>&1; }; NEW_W; [ "$(jq -r .isolation "$SESSIONS/api-gw/wt1.json")" = worktree ]'
# doctor hints go to stderr only — the cached JSON schema must not gain a field
"$KEL" doctor >/dev/null 2>&1
ok "doctor caches the same schema"       '[ "$(jq -Sr "keys | join(\",\")" "$XDG_STATE_HOME/kel/doctor.json")" = "allow_passthrough,checked_at,claude,display_popup,gh_auth,git_worktree,go_fleet,go_top,hooks_permitted,hooks_wired,jq,kel_path,node,statusline_wired,tmux,tmux>=3.0,up_to_date" ]'
# capture, do not pipe: doctor exits non-zero when a required probe fails, and
# `set -o pipefail` would then fail this test even though the hint was printed
dout="$(PATH="$WORK/nogh:$PATH" "$KEL" doctor 2>&1 || true)"
ok "  ...and a failed probe prints a fix" '[[ "$dout" == *"gh auth login"* ]]'

# the completion scripts' own jq filters, run against a real document.
# These broke silently in v0.6 (bare array -> {generated_at,current,agents})
# and stayed broken because _kel_names swallows jq's error with 2>/dev/null.
section "shell completions read the fleet document"
# The completion scripts' own jq filters, run against a real document.  Two
# separate breakages hid here: v0.6 changed `ls --json` from a bare array to
# {generated_at,current,agents}, and the `group/name` half was ALWAYS applied
# to the document rather than to each agent (yielding "null/null").  Both were
# invisible because _kel_names swallows jq's error with 2>/dev/null.
CFILT() { "$KEL" ls --json 2>/dev/null | jq -r "$1" 2>/dev/null | sort -u; }
NAMEFILT='.agents[] | .name, "\(.group)/\(.name)"'
ok "the name filter returns bare names"  '[ -n "$(CFILT "$NAMEFILT" | grep -x agent1)" ]'
ok "  ...and qualified group/name"       '[ -n "$(CFILT "$NAMEFILT" | grep -x "api-gw/agent1")" ]'
ok "  ...never a null/null row"          '[ -z "$(CFILT "$NAMEFILT" | grep null)" ]'
ok "the group filter returns groups"     '[ -n "$(CFILT ".agents[].group" | grep -x api-gw)" ]'
ok "both completion files use it"        'for f in bash zsh; do grep -q "agents\[\] | .name" "$HERE/../completions/kel.$f" || exit 1; done'
# The two lists drifted apart twice: the v0.6 document change, and `kel config`,
# which shipped in v0.9 and was in neither file until 2026-09-01. Compare them
# to each other rather than to a third copy that would drift as well.
# Scoped to the subcmds array: `_alternative 'flags:flag:(-f)'` elsewhere in the
# zsh file matches a bare quoted-word-colon pattern too.
BLIST="$(sed -n 's/.*subcmds="\([^"]*\)".*/\1/p' "$HERE/../completions/kel.bash" | tr ' ' '\n' | sort -u)"
ZLIST="$(awk '/subcmds=\(/,/^  \)/' "$HERE/../completions/kel.zsh" | grep -oE "'[a-z-]+:" | tr -d "':" | sort -u)"
ok "bash and zsh complete the same set"  '[ "$BLIST" = "$ZLIST" ]'
ok "  ...and it is not empty"            '[ "$(printf "%s\n" "$BLIST" | grep -c .)" -ge 12 ]'
ok "  ...and includes config"            '[ -n "$(printf "%s\n" "$BLIST" | grep -x config)" ]'
ok "  ...and includes update"            '[ -n "$(printf "%s\n" "$BLIST" | grep -x update)" ]'
# an empty fleet must still answer --json with JSON, not an English sentence
ok "--json is JSON with an empty fleet"  'E="$(mktemp -d "$WORK/empty.XXXX")"; XDG_STATE_HOME="$E" KEL_SESSION=nosuchprefix "$KEL" ls --json | jq -e ".agents | length == 0" >/dev/null'

# F — kel's chosen name is handed to Claude Code
( cd "$WORK/repos/coppermind" && KEL_AGENT=claude "$KEL" new named --no-agent ) >/dev/null 2>&1
ok "claude is launched with --name"      '[ "$(jq -r .agent "$SESSIONS/coppermind/named.json")" = "claude --name named" ]'
( cd "$WORK/repos/coppermind" && "$KEL" new other --agent 'sleep 9999' ) >/dev/null 2>&1
ok "  ...and other agents are untouched" '[ "$(jq -r .agent "$SESSIONS/coppermind/other.json")" = "sleep 9999" ]'

# ---------------------------------------------------------------- v0.10 M0
section "every window lives in one session (v0.10)"
reset
NEW api-gw docs
NEW coppermind docs
ok "two repos, one session"              '[ "$(grps)" = 1 ] && [ "$(tmux list-sessions -F "#{session_name}" | grep -cx keltest)" = 1 ]'
ok "  ...two windows in it"              '[ "$(wins)" = 2 ]'
ok "  ...each stamped with its group"    '[ -n "$(window_of api-gw docs)" ] && [ -n "$(window_of coppermind docs)" ]'
ok "  ...records still filed by group"   '[ -f "$SESSIONS/api-gw/docs.json" ] && [ -f "$SESSIONS/coppermind/docs.json" ]'
ok "_fleet reports both groups"          '[ "$("$KEL" _fleet | jq -r "[.agents[].group] | sort | join(\",\")")" = "api-gw,coppermind" ]'
# a hand-made window has no record and no @kel-group: kel-fleet finds its
# repo from the pane's directory, with no git fork (the bar's hot path)
tmux new-window -d -t '=keltest:' -n handmade -c "$WORK/repos/coppermind"
wait_until '[ "$(wins)" = 3 ]'
ok "a hand-made window gets its repo's group" '[ "$("$KEL" _fleet | jq -r ".agents[] | select(.name==\"handmade\") | .group")" = coppermind ]'
# the upgrade path: a v0.9 server still holds a per-repo session
tmux new-session -d -s 'keltest/legacy' -n old -c "$WORK"
ok "a v0.9 per-repo session still reads, with its group" '[ "$("$KEL" _fleet | jq -r ".agents[] | select(.name==\"old\") | .group")" = legacy ]'
tmux kill-session -t '=keltest/legacy'
export TMUX="$TMUX_TMPDIR/tmux-$(id -u)/default,$(tmux display-message -p '#{pid}'),0"
bar="$("$KEL" status-line)"
unset TMUX
# grep -c ., not wc -l: BSD pads the count (CLAUDE.md, portability)
ok "the bar lists every agent in the session" '[ "$(printf "%s" "$bar" | grep -o ":docs" | grep -c .)" = 2 ] && [[ "$bar" == *handmade* ]]'

section "kel never prompts; restore is by choice (v0.10)"
reset
NEW api-gw auth-fix
NEW coppermind sazed
tmux split-window -d -t "$(window_of api-gw auth-fix)" -c "$WORK/repos/api-gw" 'sleep 9999'
wait_until '[ "$(tmux list-panes -t "$(window_of api-gw auth-fix)" -F x 2>/dev/null | grep -c x)" = 2 ]'
"$KEL" snapshot >/dev/null 2>&1
want="$(jq -c '[.windows[] | [.group, .name, (.panes | length)]]' "$STATE/snapshot.json")"
tmux kill-server 2>/dev/null; sleep 0.6
# stdin closed: a prompt would read EOF and take its default — which is how
# the old one could not be caught by a test that only checked the exit code
out="$( (cd "$WORK" && "$KEL" < /dev/null) 2>&1 )"; rc=$?
ok "kel opens without prompting"         '[ "$rc" = 0 ] && [[ "$out" != *"[Y/n"* ]] && [ "$(grps)" = 1 ]'
ok "  ...and says how to get it back"    '[[ "$out" == *"previous workspace: 2 window"*"kel restore"* ]]'
ok "  ...having moved the old snapshot to the archive" '[ "$(ls "$STATE/snapshots" | grep -c .)" = 1 ] && [ ! -e "$STATE/snapshot.json" ]'
"$KEL" snapshot >/dev/null 2>&1          # what the tmux hooks do the moment anything changes
ok "  ...where the fresh start's own snapshot cannot overwrite it" \
  '[ "$(jq -c "[.windows[] | [.group, .name, (.panes | length)]]" "$STATE/snapshots/"*.json)" = "$want" ]'
ok "--list shows it"                     '[[ "$("$KEL" restore --list 2>&1)" == *auth-fix*sazed* ]]'
# the fresh shell must have drawn its prompt (see the sidebar section)
wait_until '[ -n "$(tmux capture-pane -p -t "$(tmux list-windows -t =keltest -F "#{window_id}" | head -1)" | grep .)" ]' 50
"$KEL" restore >/dev/null 2>&1
wait_until '[ -n "$(window_of api-gw auth-fix)" ] && [ -n "$(window_of coppermind sazed)" ]'
ok "kel restore brings it back"          '[ -n "$(window_of api-gw auth-fix)" ] && [ -n "$(window_of coppermind sazed)" ]'
ok "  ...splits included"                '[ "$(tmux list-panes -t "$(window_of api-gw auth-fix)" -F x | grep -c x)" = 2 ]'
ok "  ...into the one session"           '[ "$(grps)" = 1 ]'
ok "  ...conversations resumed by default" 'agent_up "$(window_of coppermind sazed)"'
ok "  ...and the archive entry is used up" '[ -z "$(ls "$STATE/snapshots" 2>/dev/null)" ]'
ok "  ...and the untouched fresh-start shell is closed" '[ "$(wins)" = 2 ]'
ok "  ...and no stale lock is left"      '[ ! -e "$STATE/.restoring" ]'

# a fresh-start shell you actually used is yours — restore must leave it
"$KEL" snapshot >/dev/null 2>&1
tmux kill-server 2>/dev/null; sleep 0.6
(cd "$WORK" && "$KEL" < /dev/null) >/dev/null 2>&1
WW="$(tmux list-windows -t '=keltest' -F '#{window_id}' | head -1)"
tmux send-keys -t "$WW" 'echo used' Enter
wait_until "tmux capture-pane -p -t '$WW' | grep -qx used"
"$KEL" restore >/dev/null 2>&1
wait_until '[ -n "$(window_of coppermind sazed)" ]'
ok "a fresh-start shell you used is kept" '[ -n "$(tmux list-windows -a -F "#{window_id}" | grep -x "$WW")" ]'

# the archive keeps five, newest first; `kel restore <n>` picks one
mkdir -p "$STATE/snapshots"
for t in 101 102 103 104 105 106; do
  printf '{"version":2,"saved_epoch":%s,"last":-1,"windows":[{"name":"w%s","group":"old","layout":"","panes":[{"cwd":"%s","cmd":"bash"}]}]}' \
    "$t" "$t" "$WORK" > "$STATE/snapshots/$t.json"
done
"$KEL" snapshot >/dev/null 2>&1
tmux kill-server 2>/dev/null; sleep 0.6
(cd "$WORK" && "$KEL" < /dev/null) >/dev/null 2>&1
ok "the archive keeps the newest five"   '[ "$(ls "$STATE/snapshots" | grep -c .)" = 5 ] && [ ! -e "$STATE/snapshots/101.json" ] && [ ! -e "$STATE/snapshots/102.json" ]'
"$KEL" restore 2 >/dev/null 2>&1
ok "kel restore 2 picks the second newest" '[ -n "$(window_of old w106)" ] && [ -z "$(window_of old w105)" ]'
ok "a number past the end is refused"    '! "$KEL" restore 9 >/dev/null 2>&1'

# a v0.9 snapshot keyed windows by group, one session each; it must come back
# into the one session, each window keeping its group
reset
printf '{"saved_at":"x","last_group":"api-gw","last_window":"a","groups":{"api-gw":[{"name":"a","layout":"","panes":[{"cwd":"%s","cmd":"bash"}]}],"coppermind":[{"name":"b","layout":"","panes":[{"cwd":"%s","cmd":"bash"}]}]}}' \
  "$WORK/repos/api-gw" "$WORK/repos/coppermind" > "$STATE/snapshot.json"
"$KEL" restore --no-resume >/dev/null 2>&1
ok "a v0.9 snapshot restores into one session" '[ "$(grps)" = 1 ] && [ -n "$(window_of api-gw a)" ] && [ -n "$(window_of coppermind b)" ]'

section "the fleet learns every pane (v0.10 M1)"
reset
R="$WORK/repos/api-gw"
# a real upstream position without a network: a remote-tracking ref at the
# first commit, the branch tracking it, one commit past it, one dirty file
git -C "$R" remote add origin "$WORK/nowhere.git"
git -C "$R" update-ref refs/remotes/origin/main HEAD
git -C "$R" config branch.main.remote origin
git -C "$R" config branch.main.merge refs/heads/main
git -C "$R" -c user.email=t@t -c user.name=t commit -q --allow-empty -m ahead
echo wip > "$R/dirt.txt"
NEW api-gw agent
AW="$(window_of api-gw agent)"
# a listener that binds port 0 and says which port it got — python3 is on
# both CI runners; a fixed port could collide
tmux split-window -d -t "$AW" -c "$R" \
  "python3 -c 'import socket,time; s=socket.socket(); s.bind((\"127.0.0.1\",0)); s.listen(); open(\"$WORK/port\",\"w\").write(str(s.getsockname()[1])); time.sleep(9999)'"
tmux split-window -d -t "$AW" -c "$R"
tmux new-window -d -t '=keltest:' -n odd -c "$WORK" 'sleep 9999'   # a non-shell in an unmanaged window
( cd "$R" && "$KEL" new wtp -w --agent 'sleep 9999' ) >/dev/null 2>&1
wait_until '[ -s "$WORK/port" ]' 50
wait_until '[ "$("$KEL" _fleet --panes | jq "[.panes[] | select(.kind==\"server\")] | length")" = 1 ]' 30
P() { "$KEL" _fleet --panes | jq -c "$1"; }
ok "--panes lists every pane"            '[ "$(P ".panes | length")" = 5 ]'
ok "  ...and plain _fleet has no panes key" '[ "$("$KEL" _fleet | jq "has(\"panes\")")" = false ]'
ok "the agent's pane is an agent"        '[ "$(P "[.panes[] | select(.window_name==\"agent\" and .kind==\"agent\" and .agent)] | length")" = 1 ]'
ok "a bare shell is a shell"             '[ "$(P "[.panes[] | select(.window_name==\"agent\" and .kind==\"shell\")] | length")" = 1 ]'
ok "a listener is a server, with its port" '[ "$(P "[.panes[] | select(.kind==\"server\") | .ports[]]")" = "[$(cat "$WORK/port")]" ]'
ok "a non-shell, non-agent is a program" '[ "$(P ".panes[] | select(.window_name==\"odd\") | .kind")" = "\"program\"" ]'
ok "git: branch and upstream"            '[ "$(P ".panes[] | select(.window_name==\"agent\" and .agent) | .git | [.branch, .upstream]")" = "[\"main\",\"origin/main\"]" ]'
ok "  ...ahead / behind / dirty"         '[ "$(P ".panes[] | select(.window_name==\"agent\" and .agent) | .git | [.ahead, .behind, .dirty]")" = "[1,0,1]" ]'
ok "  ...a main checkout is not a worktree" '[ "$(P ".panes[] | select(.window_name==\"agent\" and .agent) | .git.worktree")" = false ]'
ok "  ...a kel new -w pane is a worktree" '[ "$(P ".panes[] | select(.window_name==\"wtp\") | .git.worktree")" = true ]'
ok "  ...and outside a repo, git is null" '[ "$(P ".panes[] | select(.window_name==\"odd\") | .git")" = null ]'
# "current" is the pane a client is looking at.  With no client attached,
# nothing is — and kel must say none rather than guess one (invariant 3).
ok "with no client attached, no pane claims to be current" '[ "$(P "[.panes[] | select(.current)] | length")" = 0 ]'
# A control-mode client (tmux -C) is a real attached client with no terminal,
# so the with-a-client case is testable after all: it holds the attach open
# for as long as its stdin does.
( sleep 5 ) | tmux -C attach -t '=keltest' >/dev/null 2>&1 &
CC=$!
wait_until 'tmux list-clients 2>/dev/null | grep -q .' 30
SHP="$(tmux list-panes -t "$AW" -F '#{pane_id} #{pane_current_command}' | awk '$2=="bash"||$2=="zsh"||$2=="sh"{print $1; exit}')"
tmux select-window -t "$AW"; tmux select-pane -t "$SHP"
ok "with a client, exactly the pane it is looking at is current" \
  '[ "$(P "[.panes[] | select(.current) | .pane_id]")" = "[\"$SHP\"]" ]'
wait "$CC" 2>/dev/null
ok "the ports scan is cached"            '[ -f "$STATE/ports.json" ]'

# ---------------------------------------------------------------- v0.10 M2
section "the sidebar: one pane on the left of every kel window (v0.10 M2)"
export KEL_SIDEBAR=on KEL_SIDEBAR_TRACE="$WORK/sidebar.trace"
: > "$KEL_SIDEBAR_TRACE"
reset                                    # the server inherits both from here
sbcount() { tmux list-panes -t "$1" -F '#{@kel-sidebar}' 2>/dev/null | grep -cx 1; }
userpane() { tmux list-panes -t "$1" -F '#{@kel-sidebar}|#{pane_id}' | awk -F'|' '$1=="" {print $2; exit}'; }
NEW api-gw agent
SBW="$(window_of api-gw agent)"
wait_until '[ "$(sbcount "$SBW")" = 1 ]'
ok "kel new gives the window exactly one sidebar" '[ "$(sbcount "$SBW")" = 1 ]'
# no awk here: macOS's awk rejected the escaped program GNU awk accepted
ok "  ...on the left, at the configured width"    'tmux list-panes -t "$SBW" -F "#{@kel-sidebar}:#{pane_left}:#{pane_width}" | grep -qx "1:0:32"'
ok "  ...running kel-top"                         'wait_until "tmux list-panes -t $SBW -F \"#{@kel-sidebar} #{pane_current_command}\" | grep -qx \"1 kel-top\""'
ok "  ...focus stayed on the user's pane"         '[ -z "$(tmux list-panes -t "$SBW" -F "#{pane_active}#{@kel-sidebar}" | grep -x 11)" ]'
ok "  ...and the agent started there, not in it"  '[ "$(tmux display -p -t "$(userpane "$SBW")" "#{pane_current_command}")" = sleep ]'

HW="$(tmux new-window -d -P -F '#{window_id}' -t '=keltest:' -c "$WORK/repos/api-gw")"
"$KEL" _sidebar ensure "$HW"
ok "a hand-made window gets one (the hook's command)" '[ "$(sbcount "$HW")" = 1 ]'
for i in 1 2 3 4 5 6 7 8 9 10; do "$KEL" _sidebar ensure "$HW" & done; wait
ok "  ...and ten racing ensures still leave one"  '[ "$(sbcount "$HW")" = 1 ]'
OW="$(tmux new-session -d -P -F '#{window_id}' -s notkel)"
"$KEL" _sidebar ensure "$OW"
ok "a window outside kel is left alone"           '[ "$(sbcount "$OW")" = 0 ]'
tmux kill-session -t '=notkel'

ok "_fleet --panes never lists a sidebar"         '[ "$("$KEL" _fleet --panes | jq "[.panes[] | select(.cmd == \"kel-top\")] | length")" = 0 ]'
ok "  ...nor counts it among a window's panes"    '[ "$("$KEL" _fleet | jq -r ".agents[] | select(.name == \"agent\") | .panes | length")" = 1 ]'

# invariant 3: kel-top is a live non-shell; it must not keep a dead agent alive
P="$(userpane "$SBW")"
hook "$P" UserPromptSubmit
tmux send-keys -t "$P" C-c
wait_until '[ "$(state_of agent)" = dead ]'
ok "a crashed agent beside a live sidebar reads dead" '[ "$(state_of agent)" = dead ]'
"$KEL" relaunch api-gw/agent >/dev/null 2>&1
ok "relaunch types into the agent's pane, not the sidebar" \
  'wait_until "[ \"\$(tmux display -p -t $P \"#{pane_current_command}\")\" = sleep ]" && [ "$(sbcount "$SBW")" = 1 ]'

tmux kill-pane -t "$(userpane "$HW")"
"$KEL" _sidebar reap
ok "a window left holding only its sidebar is closed" '! tmux list-windows -a -F "#{window_id}" | grep -qx "$HW"'
ok "  ...and every other window is left alone"    '[ -n "$(window_of api-gw agent)" ] && [ "$(sbcount "$SBW")" = 1 ]'

# sleep/wake: no client attached, so nothing is visible — every sidebar drew
# once at startup and must not reload again
NEW coppermind other
OTW="$(window_of coppermind other)"
wait_until '[ "$(sbcount "$OTW")" = 1 ]'
SB1="$(tmux list-panes -t "$SBW" -F '#{@kel-sidebar} #{pane_id}' | awk '$1==1 {print $2}')"
SB2="$(tmux list-panes -t "$OTW" -F '#{@kel-sidebar} #{pane_id}' | awk '$1==1 {print $2}')"
reloads() { grep -c " $1 " "$KEL_SIDEBAR_TRACE"; }
sleep 4
ok "with no one looking, sidebars do not reload"  '[ "$(reloads "$SB1")" = 1 ] && [ "$(reloads "$SB2")" = 1 ]'
( sleep 7 ) | tmux -C attach -t '=keltest' >/dev/null 2>&1 &
CC=$!
wait_until 'tmux list-clients 2>/dev/null | grep -q .' 30
tmux select-window -t "$SBW"; "$KEL" _sidebar wake
sleep 5
ok "the shown sidebar reloads; the hidden one does not" '[ "$(reloads "$SB1")" -ge 3 ] && [ "$(reloads "$SB2")" -le 2 ]'
wait "$CC" 2>/dev/null

"$KEL" sidebar off >/dev/null 2>&1
ok "kel sidebar off removes every sidebar"        '[ "$(sbcount "$SBW")" = 0 ] && [ "$(sbcount "$OTW")" = 0 ]'
NEW api-gw later
ok "  ...and new windows get none"                '[ "$(sbcount "$(window_of api-gw later)")" = 0 ]'
"$KEL" sidebar on >/dev/null 2>&1
ok "kel sidebar on puts them back, everywhere"    '[ "$(sbcount "$SBW")" = 1 ] && [ "$(sbcount "$(window_of api-gw later)")" = 1 ]'

# restore keeps one sidebar per window and the user's splits (invariant 2)
reset
NEW api-gw auth-fix
NEW coppermind sazed
AFW="$(window_of api-gw auth-fix)"
wait_until '[ "$(sbcount "$AFW")" = 1 ]'
tmux split-window -d -t "$(userpane "$AFW")" -c "$WORK/repos/api-gw" 'sleep 9999'
"$KEL" snapshot >/dev/null 2>&1
shape() { jq -c '[.windows[] | [.group, .name, ([.panes[] | select(.sidebar)] | length), ([.panes[] | select(.sidebar | not)] | length)]]' "$1"; }
want="$(shape "$STATE/snapshot.json")"
ok "the snapshot records each sidebar"           '[ "$want" = "[[\"api-gw\",\"auth-fix\",1,2],[\"coppermind\",\"sazed\",1,1]]" ]'
for i in 1 2 3; do
  tmux kill-server 2>/dev/null; sleep 0.6
  "$KEL" restore >/dev/null 2>&1
  wait_until '[ "$(wins)" = 2 ]'
  ok "restore $i: one sidebar per window, splits intact" "[ '$(shape "$STATE/snapshot.json")' = '$want' ]"
done
# a snapshot from before the sidebar existed: its layout first, then a sidebar
reset
printf '{"version":2,"saved_epoch":1,"last":0,"windows":[{"name":"old","group":"api-gw","layout":"","panes":[{"cwd":"%s","cmd":"bash"},{"cwd":"%s","cmd":"bash"}]}]}' \
  "$WORK/repos/api-gw" "$WORK/repos/api-gw" > "$STATE/snapshot.json"
"$KEL" restore --no-resume >/dev/null 2>&1
ok "a pre-sidebar snapshot restores, and gains one" '[ "$(sbcount "$(window_of api-gw old)")" = 1 ] && [ "$(tmux list-panes -t "$(window_of api-gw old)" -F x | grep -c x)" = 3 ]'

# the fresh-start shell has a sidebar now; restore must still close it unused
NEW api-gw keep
"$KEL" snapshot >/dev/null 2>&1
tmux kill-server 2>/dev/null; sleep 0.6
(cd "$WORK" && "$KEL" < /dev/null) >/dev/null 2>&1
# wait for the fresh shell to draw its prompt: until it has, there is nothing
# to judge "untouched" by and kel rightly keeps the window. A person cannot
# type `kel restore` faster than a prompt appears; CI's runner can.
WW="$(tmux list-windows -t '=keltest' -F '#{window_id}' | head -1)"
wait_until '[ -n "$(tmux capture-pane -p -t "$(userpane "$WW")" | grep .)" ]' 50
"$KEL" restore >/dev/null 2>&1
wait_until '[ -n "$(window_of api-gw keep)" ]'
ok "restore still closes an unused fresh-start shell beside its sidebar" '[ "$(wins)" = 2 ]'
export KEL_SIDEBAR=off
unset KEL_SIDEBAR_TRACE

# ---------------------------------------------------------------- restore
section "restore rebuilds the workspace and keeps the snapshot"
reset
NEW api-gw auth-fix
K api-gw     new tests    --agent 'sleep 9999' >/dev/null 2>&1
K coppermind new sazed    --agent 'sleep 9999' >/dev/null 2>&1
tmux split-window -d -t "$(window_of api-gw auth-fix)" -c "$WORK/repos/api-gw" 'sleep 9999'
wait_until '[ "$(tmux list-panes -t "$(window_of api-gw auth-fix)" -F x 2>/dev/null | grep -c x)" = 2 ]'
"$KEL" snapshot >/dev/null 2>&1
want="$(jq -c '[.windows[] | [.group, .name]]' "$STATE/snapshot.json")"
for i in 1 2 3; do
  tmux kill-server 2>/dev/null; sleep 0.6
  "$KEL" restore >/dev/null 2>&1
  wait_until '[ "$(wins)" = 3 ] && [ "$(grps)" = 1 ]'
  got="$(jq -c '[.windows[] | [.group, .name]]' "$STATE/snapshot.json")"
  ok "restore $i rebuilds 3 windows in one session" "[ '$(wins)' = 3 ] && [ '$(grps)' = 1 ]"
  ok "restore $i leaves the snapshot intact"    "[ '$got' = '$want' ]"
  ok "restore $i rebuilds the split pane"       '[ "$(tmux list-panes -t "$(window_of api-gw auth-fix)" -F x 2>/dev/null | grep -c x)" = 2 ]'
done
ok "no stale .restoring lock is left"   '[ ! -e "$STATE/.restoring" ]'
ok "a .prev generation is kept"         '[ -f "$STATE/snapshot.json.prev" ]'

section "portability fixes keep behaving (no stat/sed/sort dependencies)"
reset
NEW api-gw a
W0="$(window_of api-gw a)"
printf 'waiting 1\n' > "$STATE/@99.state"; printf 'x\n' > "$STATE/@99.ctx"
printf 'idle 1\n'    > "$STATE/$W0.state"
"$KEL" ls >/dev/null 2>&1
ok "prune drops a stale .state"          '[ ! -e "$STATE/@99.state" ]'
ok "prune drops a stale .ctx"            '[ ! -e "$STATE/@99.ctx" ]'
ok "prune keeps a LIVE window's state"   '[ -e "$STATE/$W0.state" ]'
rm -f "$STATE/snapshot.json"
date +%s > "$STATE/.restoring";                  "$KEL" snapshot >/dev/null 2>&1
ok "a fresh lock stops snapshot writing" '[ ! -f "$STATE/snapshot.json" ]'
echo $(( $(date +%s) - 400 )) > "$STATE/.restoring"; "$KEL" snapshot >/dev/null 2>&1
ok "a stale lock does not"               '[ -f "$STATE/snapshot.json" ]'
rm -f "$STATE/snapshot.json"; printf 'garbage\n' > "$STATE/.restoring"
"$KEL" snapshot >/dev/null 2>&1
ok "an unparseable lock reads as stale"  '[ -f "$STATE/snapshot.json" ]'

# ---------------------------------------------------------------- safety
section "kill never destroys the only copy of work"
reset
K api-gw new wt -w --agent 'sleep 9999' >/dev/null 2>&1
echo wip > "$WORK/repos/.kel-worktrees/api-gw-wt/scratch.txt"
out="$("$KEL" kill api-gw/wt 2>&1)"
ok "a dirty worktree is refused"        '[[ "$out" == *refusing* ]]'
# Invariant 1 says the refusal "shows exactly what is at risk", and until
# 2026-09-01 only the word `refusing` was checked. bin/kel prints
# `git status --short`, so the at-risk path is nameable and worth asserting.
ok "  ...and names the file at risk"    '[[ "$out" == *scratch.txt* ]]'
ok "  ...and the window survives"       '[ "$(wins)" = 1 ]'
ok "  ...and --force gets through"      '"$KEL" kill api-gw/wt -f >/dev/null 2>&1; [ "$(wins)" = 0 ]'

# a worktree directory removed by hand (rm -rf, a file manager) BEFORE kill
# used to leave git's own worktree record behind, permanently locking the
# branch ("used by worktree") until someone ran `git worktree prune` by
# hand — kel kill silently skipped the whole removal block because it
# required the directory to still exist. Reported externally, reproduced,
# fixed: `git worktree remove` handles a missing directory fine on its own.
section "kill cleans up a worktree whose directory is already gone"
reset
K api-gw new wt2 -w --agent 'sleep 9999' >/dev/null 2>&1
WT2="$WORK/repos/.kel-worktrees/api-gw-wt2"
[ -d "$WT2" ] || { echo "setup failed: $WT2 missing"; exit 1; }
rm -rf "$WT2"
out="$("$KEL" kill api-gw/wt2 2>&1)"
ok "kill still succeeds"                '[[ "$out" == *killed* ]]'
ok "  ...and the phantom entry is gone" '! git -C "$WORK/repos/api-gw" worktree list | grep -q wt2'
ok "  ...so the branch can be deleted"  'git -C "$WORK/repos/api-gw" branch -D wt2 >/dev/null 2>&1'

# kel prune had the identical bug, reached a different way: a DEAD agent
# (its window gone, not killed through kel) whose worktree directory was also
# removed by hand. cmd_prune has no other test coverage at all before this.
section "prune cleans up a dead agent's worktree the same way"
reset
K api-gw new wt3 -w --agent 'sleep 9999' >/dev/null 2>&1
WT3="$WORK/repos/.kel-worktrees/api-gw-wt3"
[ -d "$WT3" ] || { echo "setup failed: $WT3 missing"; exit 1; }
rm -rf "$WT3"
tmux kill-window -t "$(window_of api-gw wt3)" 2>/dev/null
wait_until '[ -n "$("$KEL" ls 2>/dev/null | grep wt3)" ]'   # record still exists, window does not
out="$("$KEL" prune 2>&1)"
ok "prune reports it"                   '[[ "$out" == *"pruned 1"* ]]'
ok "  ...and the phantom entry is gone" '! git -C "$WORK/repos/api-gw" worktree list | grep -q wt3'
ok "  ...so the branch can be deleted"  'git -C "$WORK/repos/api-gw" branch -D wt3 >/dev/null 2>&1'

# ---------------------------------------------------------------- state
section "Notification maps to what it actually means (#14)"
reset
NEW api-gw a
P="$(pane_of api-gw a)"
hook "$P" UserPromptSubmit
ok "UserPromptSubmit -> working"                    '[ "$(state_of a)" = working ]'
hook "$P" Notification permission_prompt "needs Bash"
ok "permission_prompt -> waiting"                   '[ "$(state_of a)" = waiting ]'
ok "  ...and the reason is kept"                    '[[ "$("$KEL" _fleet | jq -r ".agents[0].note")" == *"needs Bash"* ]]'
hook "$P" UserPromptSubmit
hook "$P" Notification idle_prompt "still there?"
ok "idle_prompt does NOT claim your attention"      '[ "$(state_of a)" = working ]'
hook "$P" Notification agent_completed "done"
ok "agent_completed -> done"                        '[ "$(state_of a)" = done ]'
hook "$P" Notification quota_auto_resume_fired "q"
ok "quota_auto_resume_* -> throttled"               '[ "$(state_of a)" = throttled ]'
hook "$P" Notification agent_needs_input "which?"
ok "agent_needs_input -> waiting"                   '[ "$(state_of a)" = waiting ]'
hook "$P" Notification some_type_from_the_future "?"
ok "an unknown type falls back to waiting"          '[ "$(state_of a)" = waiting ]'

section "dead agents are detected at read time"
reset
NEW api-gw a
P="$(pane_of api-gw a)"
W="$(tmux display-message -p -t "$P" '#{window_id}')"
hook "$P" UserPromptSubmit
tmux send-keys -t "$P" C-c 2>/dev/null
wait_until '[ "$(state_of a)" = dead ]'
ok "a crashed agent reads dead, not working"        '[ "$(state_of a)" = dead ]'

section "compaction counter (#15)"
reset
NEW api-gw a
P="$(pane_of api-gw a)"
M="$SESSIONS/api-gw/a.json"
hook "$P" UserPromptSubmit
hook "$P" PreCompact
ok "PreCompact starts the count at 1"       '[ "$(jq -r ".compactions // 0" "$M")" = 1 ]'
ok "  ...and leaves state alone (mid-turn)" '[ "$(state_of a)" = working ]'
hook "$P" PreCompact; hook "$P" PreCompact
ok "  ...and accumulates"                   '[ "$(jq -r .compactions "$M")" = 3 ]'
ok "PostCompact does not double-count"      'hook "$P" PostCompact; [ "$(jq -r .compactions "$M")" = 3 ]'
ok "--json exposes it"                      '[ "$("$KEL" ls --json | jq -r ".agents[0].compactions")" = 3 ]'

section "relaunch-in-place (#13)"
reset
NEW api-gw a
W0="$(window_of api-gw a)"
M="$SESSIONS/api-gw/a.json"
P="$(tmux list-panes -t "$W0" -F '#{pane_id}')"
hook "$P" UserPromptSubmit
before="$(jq -Sc 'del(.compactions)' "$M")"
out="$("$KEL" relaunch api-gw/a 2>&1)"
ok "refuses while a process is alive"       '[[ "$out" == *"live process"* ]]'
ok "  ...even though state says working"    '[ "$(state_of a)" = working ]'
tmux send-keys -t "$P" C-c 2>/dev/null
wait_until '[ "$(state_of a)" = dead ]'
ok "  ...the agent now reads dead"          '[ "$(state_of a)" = dead ]'
"$KEL" relaunch api-gw/a >/dev/null 2>&1
wait_until '[ "$(wins)" = 1 ]'
ok "relaunch reuses the SAME window"         '[ "$(tmux list-windows -a -F "#{window_id}" | head -1)" = "$W0" ]'
ok "  ...creates no extra window"           '[ "$(wins)" = 1 ]'
ok "  ...creates no worktree"               '[ ! -d "$WORK/repos/.kel-worktrees" ]'
ok "  ...leaves the record otherwise as-is" '[ "$(jq -Sc "del(.compactions)" "$M")" = "$before" ]'
ok "an unknown name is refused"             '! "$KEL" relaunch nope-not-here >/dev/null 2>&1'
ok "the old 'restart' name is gone"        '! "$KEL" restart api-gw/a >/dev/null 2>&1'

section "fleet notifications (#1)"
reset
NEW api-gw a
P="$(pane_of api-gw a)"
NLOG="$WORK/notify.log"; : > "$NLOG"
printf '#!/bin/sh\nprintf "%%s|%%s\\n" "$1" "$2" >> %s\n' "$NLOG" > "$WORK/stub"; chmod +x "$WORK/stub"
export KEL_NOTIFY_CMD="$WORK/stub"
hook "$P" UserPromptSubmit
hook "$P" Notification permission_prompt "needs Bash"
wait_until '[ "$(grep -c . "$NLOG")" = 1 ]'
ok "notifies on entering waiting"           '[ "$(grep -c . "$NLOG")" = 1 ]'
ok "  ...with the reason as the body"       'grep -q "needs Bash" "$NLOG"'
hook "$P" Notification permission_prompt "needs Bash again"
sleep 0.3   # nothing to wait FOR: asserting silence
ok "silent on a repeat of the same state"   '[ "$(grep -c . "$NLOG")" = 1 ]'
hook "$P" UserPromptSubmit
hook "$P" Notification permission_prompt "blocked once more"
wait_until '[ "$(grep -c . "$NLOG")" = 2 ]'
ok "notifies again after leaving+returning" '[ "$(grep -c . "$NLOG")" = 2 ]'
: > "$NLOG"; hook "$P" Stop; sleep 0.3
ok "does not notify for states not in \$KEL_NOTIFY" '[ "$(grep -c . "$NLOG")" = 0 ]'
: > "$NLOG"; KEL_NOTIFY="done" hook "$P" UserPromptSubmit >/dev/null 2>&1
: > "$NLOG"; KEL_NOTIFY="done" hook "$P" Stop
wait_until '[ "$(grep -c . "$NLOG")" = 1 ]'
ok "\$KEL_NOTIFY selects which states do"   '[ "$(grep -c . "$NLOG")" = 1 ]'
unset KEL_NOTIFY_CMD
# The cases above already prove the detached path: this suite never attaches a
# client, so every notification here took the "nobody is looking" branch.
# Suppression *while focused* needs an attached client, and attaching one
# portably in CI would mean `script`, whose flags differ between GNU and BSD —
# exactly the class of trap this suite exists to catch. Verified by hand.

# ---------------------------------------------------------------- statusline
section "kel update refuses before it touches anything"
# Driven against a throwaway clone, with $SELF pointed into it, so nothing here
# can pull or re-wire the checkout the suite is running from. Each refusal is
# asserted to leave HEAD where it was: a guard that declines but has already
# moved the tree is not a guard.
UPSTREAM="$WORK/up.git"; UCLONE="$WORK/uclone"
git init -q --bare "$UPSTREAM"
git clone -q "$UPSTREAM" "$UCLONE" 2>/dev/null
( cd "$UCLONE" && git config user.email t@t.t && git config user.name T \
  && mkdir -p bin && cp "$KEL" bin/kel && printf 'x\n' > f.txt \
  && git add -A && git commit -q -m init && git push -q origin HEAD:refs/heads/main \
  && git branch -q -u origin/main 2>/dev/null ) 2>/dev/null
UKEL="$UCLONE/bin/kel"
UHEAD() { git -C "$UCLONE" rev-parse HEAD; }

before="$(UHEAD)"
printf 'dirty\n' >> "$UCLONE/f.txt"
uout="$("$UKEL" update 2>&1 || true)"
ok "refuses a dirty tree"                '[[ "$uout" == *"uncommitted changes"* ]]'
ok "  ...and moved nothing"              '[ "$(UHEAD)" = "$before" ]'
( cd "$UCLONE" && git checkout -q -- f.txt )

uout="$( cd "$UCLONE" && git checkout -q --detach && "$UKEL" update 2>&1 || true)"
ok "refuses a detached HEAD"             '[[ "$uout" == *"detached HEAD"* ]]'
( cd "$UCLONE" && git checkout -q main 2>/dev/null || git checkout -q master 2>/dev/null )

uout="$( cd "$UCLONE" && git checkout -q -b nobranch-upstream && "$UKEL" update 2>&1 || true)"
ok "refuses a branch with no upstream"   '[[ "$uout" == *"no upstream"* ]]'
( cd "$UCLONE" && git checkout -q - 2>/dev/null )

ok "reports already-up-to-date"           '[[ "$("$UKEL" update 2>&1 || true)" == *"up to date"* ]]'
ok "  ...and left HEAD alone"              '[ "$(UHEAD)" = "$before" ]'
# a non-clone install must say so rather than half-running
mkdir -p "$WORK/nogit/bin" && cp "$KEL" "$WORK/nogit/bin/kel"
ok "refuses when it is not a git clone"  '[[ "$("$WORK/nogit/bin/kel" update 2>&1 || true)" == *"not a git clone"* ]]'

section "the docs keep up with the code"
# Two hand-written lists of things bin/kel already knows. That is the shape that
# rots here: the spec's probe table was missing `gh authenticated` and `statusLine wired`
# until 2026-09-01, and `kel config` was in neither completion file from v0.9
# until the same day. Both are counted rather than string-matched — the wording
# differs on purpose ("the wired kel path still resolves" vs "…resolves"), and a
# mapping table to reconcile that would be the burden these guards remove.

# The spec's table folds "tmux present" and "tmux >= 3.0" into one row, and its
# "clone level with its upstream" row is rendered directly rather than through
# probe(). Both adjustments are named here so a future reader can re-derive them.
DPROBES=$(awk '/^cmd_doctor\(\)/,/^}/' "$HERE/../bin/kel" | grep -cE '\$\(probe ')
DROWS=$(awk '/^## .*Capability probe/,/^### /' "$HERE/../docs/spec.md" \
        | grep '^| ' | grep -v '^| Probe' | grep -vc 'clone level')
ok "every doctor probe has a spec row"   '[ "$DPROBES" = "$(( DROWS + 1 ))" ]'
ok "  ...and there are some to check"    '[ "$DPROBES" -ge 12 ]'

# The four machine-readable command lists: the dispatcher, the header comment
# `kel help` renders, and both completions. -h/--help/help/cheat are excluded
# with a reason — the first three are flags rather than commands, and `cheat` is
# documented under the header's "internal" heading while still being completable.
DISP=$(awk '/^  case "\$cmd" in/,/^  esac/' "$HERE/../bin/kel" \
       | grep -oE '^    [a-z|-]+\)' | tr -d ' )' | tr '|' '\n' \
       | grep -vE '^(hook|statusline|status-line|snapshot|jump|attach|list|menu|help|cheat|-h|--help)$' \
       | grep -v '^_' | grep -v '^$' | sort -u)
HELP=$(awk '/^# internal \(/{exit} /^#   kel [a-z]/{print $3}' "$HERE/../bin/kel" | sort -u)
CBASH=$(sed -n 's/.*subcmds="\([^"]*\)".*/\1/p' "$HERE/../completions/kel.bash" \
        | tr ' ' '\n' | grep -vE '^(help|cheat)$' | sort -u)
CZSH=$(awk '/subcmds=\(/,/^  \)/' "$HERE/../completions/kel.zsh" \
       | grep -oE "'[a-z-]+:" | tr -d "':" | grep -vE '^(help|cheat)$' | sort -u)
ok "the dispatcher and kel help agree"   '[ "$DISP" = "$HELP" ]'
ok "  ...and the bash completion too"    '[ "$DISP" = "$CBASH" ]'
ok "  ...and the zsh completion too"     '[ "$DISP" = "$CZSH" ]'
ok "  ...over a real number of commands" '[ "$(printf "%s\n" "$DISP" | grep -c .)" -ge 12 ]'

# install.sh's closing message once died with "bad substitution" — a backtick
# in an unquoted heredoc — which `bash -n` cannot see: it only fails when run.
# Render it the way install.sh does, without running the install.
INSTALL_MSG="$(sed -n "/^cat <<'EOF'/,/^EOF/p" "$HERE/../install.sh")"
ok "install.sh's closing message renders" \
  '[ -n "$INSTALL_MSG" ] && out="$(bash -c "$INSTALL_MSG" 2>&1)" && [[ "$out" == *"kel restore"* ]] && [[ "$out" != *"bad substitution"* ]]'

section "errors cannot be swallowed"
# display-popup: a single -E closes the popup the instant shell-command exits
# — including a `die`, so the message a human needed never rendered. Two -E
# closes only on success, leaving a failure on screen. Counted, not matched
# to line numbers.
POPUP_EE=$(grep -c -- '-EE' "$HERE/../bin/kel")
ok "the top/config popups use -EE"       '[ "$POPUP_EE" = 2 ]'
POPUP_SINGLE_E=$(grep -cE '(^|[^E-])-E([^E]|$)' "$HERE/../bin/kel")
ok "  ...and none uses a single -E"      '[ "$POPUP_SINGLE_E" = 0 ]'

# run-shell in kel.conf: tmux only added a -E flag to run-shell (redirect
# stderr into the stdout run-shell already shows on failure) in 3.6 — this
# machine's tmux is 3.4, and `tmux source-file` on a -E'd run-shell dies with
# "unknown flag -E", confirmed live below. That would have broken every key
# this touches on kel's own documented tmux >= 3.0 floor, so the fix is the
# portable equivalent: a literal `2>&1` inside the invoked command, which
# every tmux back to 3.0 already displays on failure via run-shell's
# built-in view-mode behaviour. The snapshot hooks
# (client-detached, after-split-window, pane-exited) stay fire-and-forget on
# purpose either way — do not add this to them.
CONF="$HERE/../tmux/kel.conf"
KELBINDS=$(grep -n '^bind.*run-shell -b .*local/bin/kel ' "$CONF")
ok "kel.conf's foreground kel bind exists"             '[ "$(printf "%s\n" "$KELBINDS" | grep -c .)" = 1 ]'
ok "  ...and surfaces run-shell's stderr"               '[ "$(printf "%s\n" "$KELBINDS" | grep -c "2>&1")" = 1 ]'
ok "  ...while every hook discards its output"          '[ -z "$(grep "^set-hook" "$CONF" | grep "run-shell" | grep -v ">/dev/null 2>&1")" ]'
ok "  ...and this config actually sources under tmux"  \
  'tmux -S "$TMUX_TMPDIR/guard.sock" new-session -d -s guardtest 2>/dev/null
   rc=0; tmux -S "$TMUX_TMPDIR/guard.sock" source-file "$CONF" 2>/dev/null || rc=$?
   tmux -S "$TMUX_TMPDIR/guard.sock" kill-server 2>/dev/null
   [ "$rc" = 0 ]'
# prove it would NOT source with a literal -E flag below tmux 3.6 — kept as a
# regression guard, because an earlier spec prescribed exactly that. From 3.6
# the flag exists (Homebrew ships 3.7), so there the check has nothing to
# prove; kel's floor is 3.0, which is why kel.conf still cannot use it.
TV="$(tmux -V | grep -oE '[0-9]+\.[0-9]+' | head -1)"
if [ "${TV%%.*}" -lt 3 ] || { [ "${TV%%.*}" -eq 3 ] && [ "${TV#*.}" -lt 6 ]; }; then
sed 's/run-shell -b "\(\$HOME[^"]*\)"/run-shell -b -E "\1"/' "$CONF" > "$WORK/kel-conf-dash-E"
ok "  ...confirmed: a literal -E flag does NOT source on tmux $TV" \
  'tmux -S "$TMUX_TMPDIR/guard2.sock" new-session -d -s guardtest2 2>/dev/null
   rc=0; tmux -S "$TMUX_TMPDIR/guard2.sock" source-file "$WORK/kel-conf-dash-E" 2>/dev/null || rc=$?
   tmux -S "$TMUX_TMPDIR/guard2.sock" kill-server 2>/dev/null
   [ "$rc" != 0 ]'
else
  echo "  skip  (tmux $TV has run-shell -E; the guard only means something below 3.6)"
fi
# prove the stderr-redirect guard bites: drop one 2>&1 in a scratch copy.
sed 's/kel top 2>&1"/kel top"/' "$CONF" > "$WORK/kel-conf-revert"
REVERTED=$(grep -n '^bind.*run-shell -b .*local/bin/kel ' "$WORK/kel-conf-revert")
ok "  ...and dropping it is caught"       '[ "$(printf "%s\n" "$REVERTED" | grep -c "2>&1")" != 1 ]'

section "statusline records context without a live agent"
reset
NEW api-gw a
P="$(pane_of api-gw a)"
W="$(tmux display-message -p -t "$P" '#{window_id}')"
sl() { printf '%s' "$1" | TMUX_PANE="$P" "$KEL" statusline; }
sl '{"session_id":"a","model":{"display_name":"Opus 5"},"workspace":{"current_dir":"/x"},
     "cost":{"total_cost_usd":1.5},"rate_limits":{"five_hour":{"used_percentage":12}},
     "context_window":{"total_input_tokens":15500,"context_window_size":200000,"used_percentage":42}}' >/dev/null
ok "a .ctx file is written"                         '[ -f "$STATE/$W.ctx" ]'
ok "  ...with the right percentage"                 '[ "$(cut -f1 "$STATE/$W.ctx")" = 42 ]'
ok "  ...and the model in the last field"           '[ "$(cut -f7 "$STATE/$W.ctx")" = "Opus 5" ]'
ok "ls surfaces it"                                 '[[ "$("$KEL" ls 2>/dev/null)" == *42%* ]]'
ok "--json surfaces it"                             '[ "$("$KEL" ls --json | jq -r ".agents[0].context.pct")" = 42 ]'
# the IFS-tab-folding regression: an empty field must not shift every later value
sl '{"session_id":"a","cost":{"total_cost_usd":9.25},
     "context_window":{"total_input_tokens":170000,"context_window_size":200000,"used_percentage":86}}' >/dev/null
ok "an empty field does not shift the others"       '[ "$(cut -f1 "$STATE/$W.ctx")" = 86 ] && [ "$(cut -f2 "$STATE/$W.ctx")" = 9.25 ]'
# a malformed payload must never blank a good record
for bad in '' 'not json' '{}' '{"context_window":null}'; do sl "$bad" >/dev/null 2>&1; done
ok "garbage payloads cannot blank a good record"    '[ "$(cut -f1 "$STATE/$W.ctx")" = 86 ]'

# used_percentage is null before the first API call and again right after
# /compact. That is "not measured", not "empty", and the two must render
# differently.
NULLPCT='{"session_id":"a","model":{"display_name":"Opus 5"},
          "context_window":{"total_input_tokens":170000,"context_window_size":200000,"used_percentage":null}}'
ok "an unmeasured context renders as unknown"       '[[ "$(sl "$NULLPCT")" == *"ctx ?"* ]]'
ok "  ...and not as 0%"                             '[[ "$(sl "$NULLPCT")" != *"0%"* ]]'
ok "  ...and does not overwrite a real record"      '[ "$(cut -f1 "$STATE/$W.ctx")" = 86 ]'
ok "a real 0% still renders as a figure"            '[[ "$(sl "{\"session_id\":\"a\",\"context_window\":{\"context_window_size\":200000,\"used_percentage\":0}}")" == *"0%"* ]]'

# `agent.name` marks a session started with `claude --agent`, NOT a Task-tool
# subagent — those render through a separate `subagentStatusLine` setting and
# never reach this channel. An --agent session is primary and records normally.
AGENTPL='{"session_id":"a","agent":{"name":"Explore"},
          "context_window":{"total_input_tokens":8000,"context_window_size":200000,"used_percentage":4}}'
AGENTROW="$(sl "$AGENTPL")"
ok "an --agent session still records context"       '[ "$(cut -f1 "$STATE/$W.ctx")" = 4 ]'
ok "  ...and names the agent in the row"            '[[ "$AGENTROW" == *Explore* ]]'
sl '{"session_id":"a","context_window":{"total_input_tokens":170000,"context_window_size":200000,"used_percentage":86}}' >/dev/null

# these arrive in the payload, so rendering them costs no git subprocess.
# Driven WITHOUT $TMUX_PANE and with a cwd no pane holds: inside a kel window
# the `kel <group>/<name>` label owns that slot.
IDROW="$(printf '%s' '{"session_id":"zz","workspace":{"current_dir":"/nonexistent-kel-test","repo":{"owner":"tharpep","name":"kelsier"}},"worktree":{"name":"wt1"},"context_window":{"context_window_size":200000,"used_percentage":5}}' | "$KEL" statusline)"
ok "repo identity renders from the payload"         '[[ "$IDROW" == *tharpep/kelsier@wt1* ]]'
ok "  ...and yields to the kel label inside kel"    '[[ "$(sl "{\"session_id\":\"a\",\"workspace\":{\"repo\":{\"owner\":\"tharpep\",\"name\":\"kelsier\"}},\"context_window\":{\"context_window_size\":200000,\"used_percentage\":5}}")" != *tharpep/kelsier* ]]'
ok "absolute tokens appear once past the warn"      '[[ "$(sl "{\"session_id\":\"a\",\"context_window\":{\"total_input_tokens\":847000,\"context_window_size\":1000000,\"used_percentage\":85}}")" == *847k/1M* ]]'
ok "  ...and not below it"                          '[[ "$(sl "{\"session_id\":\"a\",\"context_window\":{\"total_input_tokens\":20000,\"context_window_size\":1000000,\"used_percentage\":2}}")" != *20k/1M* ]]'
# 5400s sits mid-hour, so the hour digit cannot flip while the test runs. An
# exact minute count would race the clock; only the shape is asserted.
RESET_AT=$(( $(date +%s) + 5400 ))
QROW="$(sl "{\"session_id\":\"a\",\"context_window\":{\"context_window_size\":200000,\"used_percentage\":5},\"rate_limits\":{\"five_hour\":{\"used_percentage\":74,\"resets_at\":$RESET_AT}}}")"
ok "a quota past 50% names its reset"               '[[ "$QROW" == *"5h 74% (1h"* ]]'
ok "  ...and a quota below 50% stays hidden"        '[[ "$(sl "{\"session_id\":\"a\",\"context_window\":{\"context_window_size\":200000,\"used_percentage\":5},\"rate_limits\":{\"five_hour\":{\"used_percentage\":31}}}")" != *5h* ]]'
ok "a reset already in the past is omitted"         '[[ "$(sl "{\"session_id\":\"a\",\"context_window\":{\"context_window_size\":200000,\"used_percentage\":5},\"rate_limits\":{\"five_hour\":{\"used_percentage\":74,\"resets_at\":1}}}")" == *"5h 74%"* ]]'
ok "non-default modes render"                       '[[ "$(sl "{\"session_id\":\"a\",\"fast_mode\":true,\"effort\":{\"level\":\"xhigh\"},\"context_window\":{\"context_window_size\":200000,\"used_percentage\":5}}")" == *"fast xhigh"* ]]'
ok "  ...and default ones do not"                   '[[ "$(sl "{\"session_id\":\"a\",\"effort\":{\"level\":\"medium\"},\"thinking\":{\"enabled\":true},\"context_window\":{\"context_window_size\":200000,\"used_percentage\":5}}")" != *medium* ]]'

# ---------------------------------------------------------------- summary
printf '\n'
if [ "$fail" -eq 0 ]; then
  printf '  \033[32m%d passed\033[0m\n' "$pass"
else
  printf '  \033[32m%d passed\033[0m, \033[31m%d failed\033[0m\n' "$pass" "$fail"
  printf '    - %s\n' "${failed[@]}"
fi
exit "$fail"

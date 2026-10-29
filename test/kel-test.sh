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
# Requires: tmux, jq, git, go.  ~2 min (2026-10-16, WSL2). The suite
# prints its pass/fail count at the end.
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
KEL="$HERE/../bin/kel"
[ -x "$KEL" ] || { echo "no executable bin/kel next to $HERE" >&2; exit 1; }

VERBOSE=0; [ "${1:-}" = "-v" ] && VERBOSE=1

WORK="$(mktemp -d "${TMPDIR:-/tmp}/kel-test.XXXXXX")"
export XDG_STATE_HOME="$WORK/state"
export KEL_SESSION=keltest
# kel notifies by default since 2026-10-15 (a bell, and a Windows toast on
# WSL): a suite that fakes waiting agents would pop real notifications on the
# developer's desktop. The notification tests use a KEL_NOTIFY_CMD stub instead
export KEL_NOTIFY_DESKTOP=off KEL_NOTIFY_BELL=off
# kel asks Claude Code for its sessions (`claude agents --json`) on the
# sidebar's path: here a stub answers, from $WORK/claude-agents.json, or the
# developer's real sessions would leak into the suite's fleet
export KEL_CLAUDE_BIN="$WORK/claude-stub"
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
unset TMUX TMUX_PANE VISUAL  # don't inherit an outer tmux or interactive editor
# ...nor its config.  reset() starts the server with -f /dev/null, but once a
# test kills it, the next one is started by KEL (`new-session` in a restore),
# and that loads the developer's real ~/.tmux.conf — kel's own hooks included,
# running the installed kel against this suite's state.  CI has no such file,
# so this only ever bit a dev box: it put a test's agent pane into view-mode.
# Every tmux call — the suite's, kel's, kel-sidebar's — goes through this shim.
mkdir -p "$WORK/shim"
printf '#!/bin/sh\nexec %s -f /dev/null "$@"\n' "$(command -v tmux)" > "$WORK/shim/tmux"
chmod +x "$WORK/shim/tmux"
export PATH="$WORK/shim:$PATH"
printf '#!/bin/sh\n[ "$1 $2" = "agents --json" ] && { cat "%s/claude-agents.json" 2>/dev/null || echo "[]"; }\n' "$WORK" > "$WORK/claude-stub"
chmod +x "$WORK/claude-stub"
STATE="$XDG_STATE_HOME/kel"
SESSIONS="$STATE/sessions"

# Build the Go binaries from THIS checkout, not whatever an earlier install.sh
# left in ~/.local/bin: bin/kel has no fallback reader any more (v0.10), so a
# stale installed kel-fleet would be what the suite actually tested.
command -v go >/dev/null 2>&1 || { echo "the suite needs Go (kel-fleet, kel-sidebar)" >&2; exit 1; }
export KEL_FLEET_BIN="$WORK/kel-fleet" KEL_SIDEBAR_BIN="$WORK/kel-sidebar"
( cd "$HERE/.." && go build -o "$KEL_FLEET_BIN" ./cmd/kel-fleet && go build -o "$KEL_SIDEBAR_BIN" ./cmd/kel-sidebar ) \
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
${3:+,\"notification_type\":\"$3\"}${4:+,\"message\":\"$4\"}}" \
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
# the sidebar runs kel-sidebar, a non-shell, so it is excluded — else this would
# report the agent up before it has started
agent_up() { wait_until "tmux list-panes -t '$1' -F '#{@kel-sidebar}|#{pane_current_command}' 2>/dev/null | awk -F'|' '\$1==\"\"{print \$2}' | grep -qvxE 'bash|zsh|fish|sh|dash|tmux'"; }
NEW() { K "$1" new "$2" --agent 'sleep 9999' >/dev/null 2>&1; agent_up "$(window_of "$1" "$2")"; }
# By group AND name: since v0.10 every window shares one session, so two
# repos' `docs` agents are two windows in it, told apart by @kel-group.
window_of() { tmux list-windows -a -F '#{session_name} #{@kel-group} #{window_id} #{window_name}' 2>/dev/null \
                | awk -v g="$1" -v n="$2" '$1 ~ /^keltest/ && $2==g && $4==n {print $3; exit}'; }
pane_of()   { local w; w="$(window_of "$1" "$2")"; [ -n "$w" ] && tmux list-panes -t "$w" -F '#{pane_id}' | head -1; }
# Hold a control-mode client attached for at most $1 s, and end it at once with
# release_client. `kill $!` used to target tmux, the pipeline's last process,
# and `wait` then sat out the whole `sleep` on the left: two 40 s holds cost
# the suite 77 s of nothing. Killing the sleep closes the client's stdin.
hold_client() {
  { sleep "$1" & echo $! > "$WORK/hold.pid"; wait; } | tmux -C attach -t '=keltest' >/dev/null 2>&1 &
  CC=$!
  wait_until 'tmux list-clients 2>/dev/null | grep -q .' 30
}
release_client() { kill "$(cat "$WORK/hold.pid" 2>/dev/null)" 2>/dev/null; wait "$CC" 2>/dev/null; }

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
# a directory, so no shell has to cd there first (2026-10-16)
( cd "$WORK" && "$KEL" new far "$WORK/repos/coppermind" --agent 'sleep 9999' ) >/dev/null 2>&1
ok "kel new <name> <dir> starts in dir, grouped by its repo" '[ "$(cd "$(jq -r .cwd "$SESSIONS/coppermind/far.json")" && pwd -P)" = "$(cd "$WORK/repos/coppermind" && pwd -P)" ]'
( cd "$WORK/repos" && "$KEL" new ./api-gw --agent 'sleep 9999' ) >/dev/null 2>&1
ok "kel new <dir> needs no name: <repo>-1" '[ -f "$SESSIONS/api-gw/api-gw-1.json" ]'
K api-gw new --agent 'sleep 9999' >/dev/null 2>&1
ok "  ...and the next one is <repo>-2"   '[ -f "$SESSIONS/api-gw/api-gw-2.json" ] && [ -n "$(window_of api-gw api-gw-2)" ]'
"$KEL" kill -f api-gw/api-gw-1 >/dev/null 2>&1
K api-gw new --agent 'sleep 9999' >/dev/null 2>&1
ok "  ...a freed number is taken again"  '[ -f "$SESSIONS/api-gw/api-gw-1.json" ]'
git -C "$WORK/repos/api-gw" branch api-gw-3 2>/dev/null
K api-gw new -w --agent 'sleep 9999' >/dev/null 2>&1
ok "  ...and -w skips a number whose branch exists" '[ -f "$SESSIONS/api-gw/api-gw-4.json" ] && [ "$(jq -r .branch "$SESSIONS/api-gw/api-gw-4.json")" = api-gw-4 ]'
"$KEL" kill -f api-gw/api-gw-4 >/dev/null 2>&1; git -C "$WORK/repos/api-gw" branch -D api-gw-3 api-gw-4 >/dev/null 2>&1
ok "kel new with a missing dir says so"  '[[ "$( cd "$WORK" && "$KEL" new x "$WORK/nope" 2>&1 )" == *"no such directory"* ]]'
"$KEL" kill -f coppermind/far >/dev/null 2>&1; "$KEL" kill -f api-gw/api-gw-1 >/dev/null 2>&1; "$KEL" kill -f api-gw/api-gw-2 >/dev/null 2>&1
( cd "$WORK/repos/api-gw" && "$KEL" new flags --agent 'sleep 9999' -- --model 'son net' ) >/dev/null 2>&1
ok "kel new <name> -- <args> passes them to the agent, quoted" '[ "$(jq -r .agent "$SESSIONS/api-gw/flags.json")" = "sleep 9999 --model son\\ net" ]'
"$KEL" kill -f api-gw/flags >/dev/null 2>&1
( cd "$WORK/repos/api-gw" && KEL_PROMPT_PRINT=1 "$KEL" _prompt "new quoted --agent \"sleep 9999\"" ) >/dev/null 2>&1
ok "; keeps quoted words together"       '[ "$(jq -r .agent "$SESSIONS/api-gw/quoted.json" 2>/dev/null)" = "sleep 9999" ]'
"$KEL" kill -f api-gw/quoted >/dev/null 2>&1

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
NEW api-gw duo
DW2="$(window_of api-gw duo)"; DP1="$(pane_of api-gw duo)"
ok "a new agent's record names its pane"  '[ "$(jq -r .pane_id "$SESSIONS/api-gw/duo.json")" = "$DP1" ]'
ok "  ...and the pane names its agent"    '[ "$(tmux display -p -t "$DP1" "#{@kel-agent}")" = api-gw/duo ]'
DP2="$(tmux split-window -d -P -F '#{pane_id}' -t "$DP1" -c "$WORK/repos/api-gw")"
tmux send-keys -t "$DP2" 'sleep 9999' Enter
wait_until '[ "$(tmux display -p -t "$DP2" "#{pane_current_command}")" = sleep ]'
hook "$DP1" UserPromptSubmit
hook "$DP2" Notification permission_prompt "needs Bash"
duo() { "$KEL" _fleet | jq -r --arg w "$DW2" '[.agents[] | select(.window_id == $w)] | sort_by(.pane_id) | map("\(.pane_id) \(.state)") | join(",")'; }
ok "two agents in one window are two agents" '[ "$("$KEL" _fleet | jq --arg w "$DW2" "[.agents[] | select(.window_id == \$w)] | length")" = 2 ]'
ok "  ...each with its own state"          '[[ ",$(duo)," == *",$DP1 working,"* ]] && [[ ",$(duo)," == *",$DP2 waiting,"* ]]'
ok "  ...kept in a file per pane"          '[ -f "$STATE/$DP2.state" ] && [ ! -f "$STATE/$DW2.state" ]'
ok "  ...the second named after the window" '"$KEL" _fleet | jq -e --arg p "$DP2" ".agents[] | select(.pane_id == \$p) | .name | startswith(\"duo-\")" >/dev/null'
tmux send-keys -t "$DP2" C-c
wait_until '[[ ",$(duo)," == *",$DP2 dead,"* ]]'
ok "one dying leaves the other alone"     '[[ ",$(duo)," == *",$DP2 dead,"* ]] && [[ ",$(duo)," == *",$DP1 working,"* ]]'
tmux kill-pane -t "$DP2"
"$KEL" kill api-gw/duo >/dev/null 2>&1
NEW api-gw host
HW2="$(window_of api-gw host)"; HP="$(pane_of api-gw host)"
K api-gw new guest --split right --at "$HP" --agent 'sleep 9999' >/dev/null 2>&1
GP="$(jq -r .pane_id "$SESSIONS/api-gw/guest.json")"
wait_until '[ "$(tmux display -p -t "$GP" "#{pane_current_command}" 2>/dev/null)" = sleep ]'
ok "kel new --split starts an agent beside another" '[ "$(tmux display -p -t "$GP" "#{window_id}")" = "$HW2" ] && [ "$(jq -r .window_id "$SESSIONS/api-gw/guest.json")" = "$HW2" ]'
ok "  ...right of it, running its own agent"     '[ "$(tmux display -p -t "$GP" "#{pane_left}")" -gt "$(tmux display -p -t "$HP" "#{pane_left}")" ] && [ "$(tmux display -p -t "$GP" "#{pane_current_command}")" = sleep ]'
ok "  ...the window keeping its first agent's name" '[ "$(tmux display -p -t "$HW2" "#{window_name}")" = host ]'
ok "  ...and the fleet lists both, by name"     '[ "$("$KEL" _fleet | jq -r --arg w "$HW2" "[.agents[] | select(.window_id == \$w) | .name] | sort | join(\",\")")" = guest,host ]'
hook "$HP" UserPromptSubmit
hook "$GP" Notification permission_prompt "needs Bash"
ok "the window's border shows the agent that waits" '[ "$(tmux show-options -wv -t "$HW2" pane-border-style)" = fg=colour214 ]'
ok "  ...and the banner names that pane"        '[ "$(cut -f1 "$STATE/banner" | tail -1)" = "$GP" ]'
grep -E '^set -g (@kel-pane-|pane-border-format )' "$HERE/../tmux/kel.conf" > "$WORK/titles.conf"
tmux source-file "$WORK/titles.conf"
title() { tmux display -p -t "$1" '#{T:pane-border-format}' | sed 's/#\[[^]]*\]//g'; }
ok "each agent pane's title says who and how"   '[ "$(title "$HP")" = " ● $(tmux display -p -t "$HP" "#{pane_index}") · host working " ] && [ "$(title "$GP")" = " ● $(tmux display -p -t "$GP" "#{pane_index}") · guest waiting " ]'
ok "  ...in the state's colour"                  'tmux display -p -t "$GP" "#{T:pane-border-format}" | grep -q "fg=yellow"'
hold_client 20
tmux select-window -t "$(window_of api-gw a)"
TMUX="$TMUX_TMPDIR/tmux-$(id -u)/default,$(tmux display-message -p '#{pid}'),0" "$KEL" jump
ok "kel jump goes to the waiting pane"         '[ "$(tmux display -p -t "$HW2" "#{pane_id}")" = "$GP" ]'
release_client
"$KEL" _own "$GP" >/dev/null 2>&1
OW2="$(jq -r .window_id "$SESSIONS/api-gw/guest.json")"
ok "_own moves an agent to a window of its own" '[ "$OW2" != "$HW2" ] && [ "$(tmux display -p -t "$GP" "#{window_id}")" = "$OW2" ]'
ok "  ...named after it, still running"         '[ "$(tmux display -p -t "$OW2" "#{window_name}")" = guest ] && [ "$(tmux display -p -t "$GP" "#{pane_current_command}")" = sleep ]'
tmux join-pane -h -s "$GP" -t "$HP"
jq --arg w "$HW2" '.window_id = $w' "$SESSIONS/api-gw/guest.json" > "$WORK/g" && mv "$WORK/g" "$SESSIONS/api-gw/guest.json"
"$KEL" kill api-gw/guest >/dev/null 2>&1
ok "kel kill closes a sharing agent's pane, not its window" '! tmux list-panes -a -F "#{pane_id}" | grep -qxF "$GP" && [ "$(tmux display -p -t "$HP" "#{pane_current_command}")" = sleep ]'
K api-gw new guest --split down --at "$HP" --agent 'sleep 9999' >/dev/null 2>&1
"$KEL" kill api-gw/host >/dev/null 2>&1
ok "  ...and a window that loses its named agent takes the next one's name" '[ "$(tmux display -p -t "$HW2" "#{window_name}")" = guest ]'
"$KEL" kill api-gw/guest >/dev/null 2>&1
ok "the last agent's kill closes the window"     '! tmux list-windows -a -F "#{window_id}" | grep -qxF "$HW2"'

# Antigravity (agy) agent vendor support
K api-gw new agy-agent --agent agy >/dev/null 2>&1
AGY_PANE="$(pane_of api-gw agy-agent)"
ok "kel new --agent agy starts agy without claude-specific flags" '[ "$(jq -r .agent "$SESSIONS/api-gw/agy-agent.json" 2>/dev/null)" = agy ]'
ok "  ...and _fleet classifies an agy pane as agent" '[ "$("$KEL" _fleet --panes | jq -r --arg p "$AGY_PANE" ".panes[] | select(.pane_id == \$p) | .kind")" = agent ]'
"$KEL" kill api-gw/agy-agent >/dev/null 2>&1
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
# phase 2: worktrees no window stands in — by hand here, as Claude Code
# makes its own — are listed under their repo, and swept once they landed
GW() { git -C "$WORK/sw" worktree add -q -b "$1" "$WORK/sw-$1" >/dev/null 2>&1; }
GW hm                                                   # nothing of its own: merged
GW hw; ( cd "$WORK/sw-hw" && echo w>w && git add -A && git -c user.email=t@t -c user.name=t commit -qm w ) >/dev/null 2>&1
GW hd; echo u > "$WORK/sw-hd/untracked"                 # merged, but holding a new file
GW hg; rm -rf "$WORK/sw-hg"                             # its directory gone
GW hb                                                   # a Claude Code session is in it
GW ho; tmux new-window -d -t '=keltest:' -n hoshell -c "$WORK/sw-ho"
printf '[{"pid":1,"cwd":"%s","kind":"background","sessionId":"x","name":"hb","status":"busy","id":"b1"}]' \
  "$(cd "$WORK/sw-hb" && pwd -P)" > "$WORK/claude-agents.json"
rm -f "$STATE/claude-agents.json"
WTL() { "$KEL" _fleet --panes | jq -r --arg b "$1" '.repos[].worktrees[] | select(.branch == $b) | .land.code // "-"'; }
ok "a worktree with no window is listed under its repo" '[ "$(WTL hm)" = merged ] && [ "$(WTL hw)" != merged ]'
ok "  ...and the Claude Code session in one is counted" '[ "$("$KEL" _fleet --panes | jq -r ".repos[].worktrees[] | select(.branch == \"hb\") | .sessions")" = 1 ]'
o="$("$KEL" sweep -n 2>&1)"
ok "sweep -n names the landed and the gone"  '[[ "$o" == *"would sweep  hm"* && "$o" == *"would prune  hg"* ]] && [ -d "$WORK/sw-hm" ]'
o="$("$KEL" sweep "$WORK/sw-hw" 2>&1)"
ok "sweeping an unmerged one by name refuses, and says why" '[ -d "$WORK/sw-hw" ] && [[ "$o" == *"kept  hw"* ]]'
o="$("$KEL" sweep "$WORK/sw-ho" 2>&1)"
ok "  ...as it does where a window is open"  '[ -d "$WORK/sw-ho" ] && [[ "$o" == *"window is open there (hoshell)"* ]]'
o="$("$KEL" sweep 2>&1)"
ok "sweep removes a landed worktree and its branch" '[ ! -d "$WORK/sw-hm" ] && ! git -C "$WORK/sw" rev-parse --verify --quiet refs/heads/hm >/dev/null'
ok "  ...prunes one whose directory is gone"  '! git -C "$WORK/sw" worktree list | grep -q sw-hg'
ok "  ...leaves one with a new file in it"     '[ -f "$WORK/sw-hd/untracked" ] && [[ "$("$KEL" sweep "$WORK/sw-hd" 2>&1)" == *"kept  hd  — uncommitted work"* ]]'
ok "  ...and one a Claude Code session is using" '[ -d "$WORK/sw-hb" ] && [[ "$o" == *"kept  hb  — a Claude Code session"* ]]'
ok "  ...and the unmerged one, without a word"  '[ -d "$WORK/sw-hw" ] && [[ "$o" != *"kept  hw"* ]]'
rm -f "$WORK/claude-agents.json" "$STATE/claude-agents.json"

# squash-merged branch deletion in sweep
GW hsq
( cd "$WORK/sw-hsq" && echo sq > sq && git add -A && git -c user.email=t@t -c user.name=t commit -qm sq && git push -q origin hsq ) >/dev/null 2>&1
( cd "$WORK/sw" && git fetch -q && git merge --squash origin/hsq && git -c user.email=t@t -c user.name=t commit -qm "squash hsq" && git push -q origin main ) >/dev/null 2>&1
git push -q "$WORK/origin.git" --delete hsq >/dev/null 2>&1
HSQ_HEAD="$(git -C "$WORK/sw-hsq" rev-parse HEAD)"
mkdir -p "$STATE/pr-cache"
printf '[{"sha":"%s","merged":true,"state":"closed"}]\n' "$HSQ_HEAD" > "$STATE/pr-cache/example__sw.sha.json"
git -C "$WORK/sw" remote set-url origin "https://github.com/example/sw.git"
git -C "$WORK/sw" remote set-url --push origin "$WORK/origin.git"
o="$("$KEL" sweep "$WORK/sw-hsq" 2>&1)"
ok "sweep removes a squash-merged worktree and its local branch" '[ ! -d "$WORK/sw-hsq" ] && ! git -C "$WORK/sw" rev-parse --verify --quiet refs/heads/hsq >/dev/null'
ok "  ...reporting both worktree and branch swept" '[[ "$o" == *"swept  hsq  (worktree and branch)"* ]]'
git -C "$WORK/sw" remote set-url origin "$WORK/origin.git"

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
ok "doctor caches the same schema"       '[ "$(jq -Sr "keys | join(\",\")" "$XDG_STATE_HOME/kel/doctor.json")" = "allow_passthrough,checked_at,claude,display_popup,gh_auth,git_worktree,go_fleet,go_sidebar,hooks_permitted,hooks_wired,jq,kel_path,node,statusline_wired,tmux,tmux>=3.0,up_to_date" ]'
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
# a session that is not kel's is not read: v0.9's `kel/<group>` ones included,
# whose readers went once every machine ran v0.10
tmux new-session -d -s 'keltest/legacy' -n old -c "$WORK"
ok "a session that is not kel's is not read" '[ -z "$("$KEL" _fleet | jq -r ".agents[] | select(.name==\"old\") | .name")" ]'
tmux kill-session -t '=keltest/legacy'
# the bar is the workspace footer (v0.10 M4, spec.md §7b): quota, the
# waiting alarm, live cost — and where you stand only while the sidebar is off
bar() { TMUX="$TMUX_TMPDIR/tmux-$(id -u)/default,$(tmux display-message -p '#{pid}'),0" "$KEL" status-line | sed 's/#\[[^]]*\]//g'; }
ok "the bar no longer lists windows"            '[[ "$(bar)" != *docs* ]] && [[ "$(bar)" != *handmade* ]]'
ok "  ...shows no quota no session reported"    '[[ "$(bar)" != *claude* ]] && [[ "$(bar)" != *%* ]]'
ok "  ...and says when nothing waits"           '[[ "$(bar)" == *"nothing waiting"* ]]'
DW="$(window_of api-gw docs)"
printf 'waiting %s\n' "$(( $(date +%s) - 400 ))" > "$STATE/$DW.state"
ok "one waiting agent, and for how long"        '[[ "$(bar)" == *"1 waiting 6m"* ]]'
ok "  ...a click target that jumps there"        '[[ "$(TMUX="$TMUX_TMPDIR/tmux-$(id -u)/default,$(tmux display-message -p "#{pid}"),0" "$KEL" status-line)" == *"#[range=user|jump]"*"1 waiting"*"#[norange]"* ]]'
# kel jump (prefix `): the next waiting agent after the window you are in,
# wrapping round — read from the fleet document, so the same dead check and
# state the sidebar shows
CW="$(window_of coppermind docs)"
printf 'waiting %s\n' "$(( $(date +%s) - 60 ))" > "$STATE/$CW.state"
hold_client 20
tmux select-window -t "$DW"
TMUX="$TMUX_TMPDIR/tmux-$(id -u)/default,$(tmux display-message -p '#{pid}'),0" "$KEL" jump
cur="$(tmux list-windows -t '=keltest' -F '#{window_active} #{window_id}' | awk '$1==1 {print $2}')"
ok "kel jump goes to the next waiting agent"    '[ "$cur" = "$CW" ]'
TMUX="$TMUX_TMPDIR/tmux-$(id -u)/default,$(tmux display-message -p '#{pid}'),0" "$KEL" jump
cur="$(tmux list-windows -t '=keltest' -F '#{window_active} #{window_id}' | awk '$1==1 {print $2}')"
ok "  ...and wraps round to the first"          '[ "$cur" = "$DW" ]'
release_client
rm -f "$STATE/$CW.state"
now=$(date +%s)
printf '58 %s 41 %s 12 0 %s\n' "$((now + 5400))" "$((now + 300000))" "$now" > "$STATE/quota"
rawbar() { TMUX="$TMUX_TMPDIR/tmux-$(id -u)/default,$(tmux display-message -p '#{pid}'),0" "$KEL" status-line; }
ok "quota: 5h as a bar with its reset, then the week" '[[ "$(bar)" == *"claude █████ 58% (1h"* ]] && [[ "$(rawbar)" == *"███#[fg=colour238]██#[default]"* ]] && [[ "$(bar)" == *"wk 41%"* ]]'
ok "  ...spend only from 50%"                   '[[ "$(bar)" != *spend* ]]'
printf '58 %s 41 %s 61 0 %s\n' "$((now + 5400))" "$((now + 300000))" "$now" > "$STATE/quota"
ok "  ...and then it shows"                     '[[ "$(bar)" == *"spend 61%"* ]]'
printf '58 %s 41 %s -1 0 %s\n' "$((now - 60))" "$((now + 300000))" "$now" > "$STATE/quota"
ok "a 5h window that has reset reads unknown, not 58%" '[[ "$(rawbar)" == *"#[fg=colour238]█████#[default] ?"* ]] && [[ "$(bar)" != *58%* ]]'
ok "  ...while the week still shows"            '[[ "$(bar)" == *"wk 41%"* ]]'
ok "no location while the sidebar shows"        '[[ "$(bar)" != *main* ]]'
# "focused" needs a client: with none attached tmux has no pane to name
: > "$STATE/sidebar-off"
hold_client 6
ok "  ...and the focused pane's repo and branch when it is off" '[[ "$(bar)" == *" · main"* ]]'
release_client
rm -f "$STATE/sidebar-off" "$STATE/quota" "$STATE/$DW.state"

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
ok "  ...and the sidebar offers it (b) until used" '[ -f "$STATE/restore-offer" ]'
"$KEL" snapshot >/dev/null 2>&1          # what the tmux hooks do the moment anything changes
ok "  ...where the fresh start's own snapshot cannot overwrite it" \
  '[ "$(jq -c "[.windows[] | [.group, .name, (.panes | length)]]" "$STATE/snapshots/"*.json)" = "$want" ]'
ok "--list shows it"                     '[[ "$("$KEL" restore --list 2>&1)" == *auth-fix*sazed* ]]'
# the fresh shell must have drawn its prompt (see the sidebar section)
wait_until '[ -n "$(tmux capture-pane -p -t "$(tmux list-windows -t =keltest -F "#{window_id}" | head -1)" | grep .)" ]' 50
"$KEL" restore >/dev/null 2>&1
wait_until '[ -n "$(window_of api-gw auth-fix)" ] && [ -n "$(window_of coppermind sazed)" ]'
ok "kel restore brings it back"          '[ -n "$(window_of api-gw auth-fix)" ] && [ -n "$(window_of coppermind sazed)" ]'
ok "  ...and the offer is gone"           '[ ! -f "$STATE/restore-offer" ]'
ok "  ...splits included"                '[ "$(tmux list-panes -t "$(window_of api-gw auth-fix)" -F x | grep -c x)" = 2 ]'
ok "  ...into the one session"           '[ "$(grps)" = 1 ]'
ok "  ...the agents running again"         'agent_up "$(window_of coppermind sazed)"'
ok "  ...and the archive entry is used up" '[ -z "$(ls "$STATE/snapshots" 2>/dev/null)" ]'
ok "  ...and the untouched fresh-start shell is closed" '[ "$(wins)" = 2 ]'
ok "  ...and no stale lock is left"      '[ ! -e "$STATE/.restoring" ]'
# a repo with no agent yet: bare kel starts one and names it, never asking
"$KEL" kill -f api-gw/auth-fix >/dev/null 2>&1
out="$( (cd "$WORK/repos/api-gw" && KEL_AGENT='sleep 9999' "$KEL" < /dev/null) 2>&1 )"
ok "kel in a repo with no agent starts one, named for it" '[ -f "$SESSIONS/api-gw/api-gw-1.json" ] && [ -n "$(window_of api-gw api-gw-1)" ]'

# What restore actually types.  The check above could not see it: `sleep 9999
# --continue || sleep 9999` starts a fresh sleep whatever the flag does, so it
# passed with nothing resumed.  An agent that records its arguments can.
# Two agents in ONE directory: `--continue` resumed the same conversation in
# both, so a record with no session id must resume by its name instead.
printf '#!/bin/sh\necho "$*" >> "%s/argv.$KEL_ARGV"\nexec sleep 9999\n' "$WORK" > "$WORK/argv-agent"; chmod +x "$WORK/argv-agent"
reset
K api-gw new twin1 --agent "KEL_ARGV=twin1 $WORK/argv-agent --name twin1" >/dev/null 2>&1
K api-gw new twin2 --agent "KEL_ARGV=twin2 $WORK/argv-agent --name twin2" >/dev/null 2>&1
agent_up "$(window_of api-gw twin1)"; agent_up "$(window_of api-gw twin2)"
hook "$(pane_of api-gw twin1)" SessionStart
ok "SessionStart records the agent's Claude session" '[ "$(jq -r .claude_session "$SESSIONS/api-gw/twin1.json")" = s ]'
ok "  ...on its own record only"         '[ "$(jq -r ".claude_session // empty" "$SESSIONS/api-gw/twin2.json")" = "" ]'
"$KEL" snapshot >/dev/null 2>&1
rm -f "$WORK"/argv.*
tmux kill-server 2>/dev/null; sleep 0.6
(cd "$WORK" && "$KEL" < /dev/null) >/dev/null 2>&1
wait_until '[ -n "$(tmux capture-pane -p -t "$(tmux list-windows -t =keltest -F "#{window_id}" | head -1)" | grep .)" ]' 50
"$KEL" restore >/dev/null 2>&1
wait_until '[ -s "$WORK/argv.twin1" ] && [ -s "$WORK/argv.twin2" ]'
ok "restore resumes a session it recorded, by id"   'grep -q -- "--resume s" "$WORK/argv.twin1"'
ok "  ...and one it did not, by its name"           'grep -q -- "--resume twin2" "$WORK/argv.twin2"'
ok "  ...never --continue, which takes the directory's newest" '! grep -q -- --continue "$WORK"/argv.*'

reset
K api-gw new left --agent "KEL_ARGV=left $WORK/argv-agent --name left" >/dev/null 2>&1
agent_up "$(window_of api-gw left)"
K api-gw new right --split right --at "$(pane_of api-gw left)" --agent "KEL_ARGV=right $WORK/argv-agent --name right" >/dev/null 2>&1
wait_until '[ "$(tmux list-panes -t "$(window_of api-gw left)" -F x | grep -c x)" = 2 ]'
"$KEL" snapshot >/dev/null 2>&1
ok "a snapshot names the agent in each pane"       '[ "$(jq -r "[.windows[].panes[].agent // empty] | sort | join(\",\")" "$STATE/snapshot.json")" = api-gw/left,api-gw/right ]'
rm -f "$WORK"/argv.*
tmux kill-server 2>/dev/null; sleep 0.6
(cd "$WORK" && "$KEL" < /dev/null) >/dev/null 2>&1
wait_until '[ -n "$(tmux capture-pane -p -t "$(tmux list-windows -t =keltest -F "#{window_id}" | head -1)" | grep .)" ]' 50
"$KEL" restore >/dev/null 2>&1
wait_until '[ -s "$WORK/argv.right" ]'
RP="$(jq -r .pane_id "$SESSIONS/api-gw/right.json")"
ok "restore resumes the split agent in its split"  '[ -s "$WORK/argv.right" ] && [ "$(tmux display -p -t "$RP" "#{window_id}")" = "$(window_of api-gw left)" ]'
ok "  ...stamped with its name"                    '[ "$(tmux display -p -t "$RP" "#{@kel-agent}")" = api-gw/right ]'

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
hold_client 5
SHP="$(tmux list-panes -t "$AW" -F '#{pane_id} #{pane_current_command}' | awk '$2=="bash"||$2=="zsh"||$2=="sh"{print $1; exit}')"
tmux select-window -t "$AW"; tmux select-pane -t "$SHP"
ok "with a client, exactly the pane it is looking at is current" \
  '[ "$(P "[.panes[] | select(.current) | .pane_id]")" = "[\"$SHP\"]" ]'
release_client
ok "the ports scan is cached"            '[ -f "$STATE/ports.json" ]'

# ---------------------------------------------------------------- v0.10 M2
section "the sidebar: one pane on the left of every kel window (v0.10 M2)"
export KEL_SIDEBAR=on KEL_SIDEBAR_TRACE="$WORK/sidebar.trace"
: > "$KEL_SIDEBAR_TRACE"
reset                                    # the server inherits both from here
sbcount() { tmux list-panes -t "$1" -F '#{@kel-sidebar}' 2>/dev/null | grep -cx 1; }
userpane() { tmux list-panes -t "$1" -F '#{@kel-sidebar}|#{pane_id}' | awk -F'|' '$1=="" {print $2; exit}'; }
sidebar_of() { tmux list-panes -t "$1" -F '#{@kel-sidebar}|#{pane_id}' | awk -F'|' '$1=="1" {print $2; exit}'; }
NEW api-gw agent
SBW="$(window_of api-gw agent)"
wait_until '[ "$(sbcount "$SBW")" = 1 ]'
ok "kel new gives the window exactly one sidebar" '[ "$(sbcount "$SBW")" = 1 ]'
ok "  ...and tmux's own bar stays on beside it"   '[ "$(tmux show-options -v -t "=keltest:" status 2>/dev/null)" != off ]'
# no awk here: macOS's awk rejected the escaped program GNU awk accepted
ok "  ...on the left, at the configured width"    'tmux list-panes -t "$SBW" -F "#{@kel-sidebar}:#{pane_left}:#{pane_width}" | grep -qx "1:0:32"'
ok "  ...running kel-sidebar"                         'wait_until "tmux list-panes -t $SBW -F \"#{@kel-sidebar} #{pane_current_command}\" | grep -qx \"1 kel-sidebar\""'
ok "  ...focus stayed on the user's pane"         '[ -z "$(tmux list-panes -t "$SBW" -F "#{pane_active}#{@kel-sidebar}" | grep -x 11)" ]'
ok "  ...and the agent started there, not in it"  '[ "$(tmux display -p -t "$(userpane "$SBW")" "#{pane_current_command}")" = sleep ]'

HW="$(tmux new-window -d -P -F '#{window_id}' -t '=keltest:' -c "$WORK/repos/api-gw")"
"$KEL" _sidebar ensure "$HW"
ok "a hand-made window gets one (the hook's command)" '[ "$(sbcount "$HW")" = 1 ]'
for i in 1 2 3 4 5 6 7 8 9 10; do "$KEL" _sidebar ensure "$HW" & done; wait
ok "  ...and ten racing ensures still leave one"  '[ "$(sbcount "$HW")" = 1 ]'
# A sidebar that died: the test for "has one" is the pane itself, so nothing
# is left claiming one.  ensure adds it back; heal (every window switch) does
# for every kel window, and only once.
tmux kill-pane -t "$(tmux list-panes -t "$HW" -F '#{@kel-sidebar} #{pane_id}' | awk '$1==1 {print $2}')"
"$KEL" _sidebar ensure "$HW"
wait_until "tmux list-panes -t $HW -F '#{@kel-sidebar} #{pane_current_command}' | grep -qx '1 kel-sidebar'"
ok "a window whose sidebar died gets it back"     '[ "$(sbcount "$HW")" = 1 ]'
for i in 1 2 3 4 5 6 7 8 9 10; do "$KEL" _sidebar ensure "$HW" & done; wait
ok "  ...and ten racing ensures still leave one"  '[ "$(sbcount "$HW")" = 1 ]'
tmux kill-pane -t "$(tmux list-panes -t "$HW" -F '#{@kel-sidebar} #{pane_id}' | awk '$1==1 {print $2}')"
"$KEL" _sidebar heal; "$KEL" _sidebar heal
wait_until "tmux list-panes -t $HW -F '#{@kel-sidebar} #{pane_current_command}' | grep -qx '1 kel-sidebar'"
ok "  ...a window switch heals it, once"          '[ "$(sbcount "$HW")" = 1 ]'
ok "  ...and kel.conf heals on a window switch"   'grep -q "session-window-changed.*kel _sidebar heal" "$HERE/../tmux/kel.conf"'
# a sidebar that dies says why: tmux reports nothing about a pane whose
# process exits, so its stderr is the only witness (backlog #41)
printf '#!/bin/sh\necho "kel-sidebar: probe crash" >&2\nexit 1\n' > "$WORK/kel-sidebar-crash"; chmod +x "$WORK/kel-sidebar-crash"
CW="$(tmux new-window -d -P -F '#{window_id}' -t '=keltest:' -c "$WORK/repos/api-gw" 'sleep 9999')"
KEL_SIDEBAR_BIN="$WORK/kel-sidebar-crash" "$KEL" _sidebar ensure "$CW"
wait_until 'grep -q "probe crash" "$STATE/sidebar-fail.log" 2>/dev/null'
ok "a sidebar that dies leaves its error in sidebar-fail.log" 'grep -q "probe crash" "$STATE/sidebar-fail.log"'
tmux kill-window -t "$CW" 2>/dev/null
# SIGUSR1 kills a process that has not asked for it yet, and a new binary's
# first launch on macOS takes up to 700 ms before its Go runtime does: the
# wake a window switch sends killed that sidebar (backlog #41). A shell that
# sleeps before it becomes the sidebar is that window, held open
printf '#!/bin/sh\nsleep 1\nexec "%s"\n' "$KEL_SIDEBAR_BIN" > "$WORK/kel-sidebar-slow"; chmod +x "$WORK/kel-sidebar-slow"
SLW="$(tmux new-window -d -P -F '#{window_id}' -t '=keltest:' -c "$WORK/repos/api-gw" 'sleep 9999')"
KEL_SIDEBAR_BIN="$WORK/kel-sidebar-slow" "$KEL" _sidebar ensure "$SLW"
"$KEL" _sidebar wake
wait_until "tmux list-panes -t $SLW -F '#{@kel-sidebar} #{pane_current_command}' | grep -qx '1 kel-sidebar'" 30
ok "a wake while a sidebar starts does not kill it" "tmux list-panes -t $SLW -F '#{@kel-sidebar} #{pane_current_command}' | grep -qx '1 kel-sidebar'"
wait_until "[ \"\$(tmux list-panes -t $SLW -F '#{@kel-ready}' | grep -cx 1)\" = 1 ]" 30
ok "  ...which marks itself ready for the next" "[ \"\$(tmux list-panes -t $SLW -F '#{@kel-ready}' | grep -cx 1)\" = 1 ]"
tmux kill-window -t "$SLW" 2>/dev/null
OW="$(tmux new-session -d -P -F '#{window_id}' -s notkel)"
"$KEL" _sidebar ensure "$OW"
ok "a window outside kel is left alone"           '[ "$(sbcount "$OW")" = 0 ]'
tmux kill-session -t '=notkel'

ok "_fleet --panes never lists a sidebar"         '[ "$("$KEL" _fleet --panes | jq "[.panes[] | select(.cmd == \"kel-sidebar\")] | length")" = 0 ]'
ok "  ...nor counts it among a window's panes"    '[ "$("$KEL" _fleet | jq -r ".agents[] | select(.name == \"agent\") | .panes | length")" = 1 ]'

# invariant 3: kel-sidebar is a live non-shell; it must not keep a dead agent alive
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

docked() { tmux list-panes -t "$1" -F '#{@kel-sidebar}:#{pane_at_left}#{pane_at_top}#{pane_at_bottom}:#{pane_width}' | grep -qx "1:111:32" && [ "$(sbcount "$1")" = 1 ]; }
NEW api-gw layout
LW="$(window_of api-gw layout)"; LA="$(userpane "$LW")"
wait_until "docked $LW"
tmux split-window -h -t "$LA" -c "$WORK/repos/api-gw"
tmux select-layout -t "$LW" even-horizontal; "$KEL" _sidebar fit
ok "fit pins a stretched sidebar back to its width"  "docked $LW"
tmux select-layout -t "$LW" main-vertical; tmux rotate-window -t "$LW"; "$KEL" _sidebar fit
ok "  ...re-docks one rotated off the left"       "docked $LW"
tmux swap-pane -s "$LA" -t "$(sidebar_of "$LW")"; "$KEL" _sidebar fit
ok "  ...and one swapped with the agent"          "docked $LW"
ok "  ...whose agent is the same pane, still running" '[ "$(tmux display -p -t "$LA" "#{window_id} #{pane_current_command}")" = "$LW sleep" ]'
grep -E '^set-hook -g (window-layout-changed|pane-focus-in) ' "$HERE/../tmux/kel.conf" \
  | sed "s#\$HOME/.local/bin/kel#$KEL#g" > "$WORK/hooks.conf"
tmux source-file "$WORK/hooks.conf"
tmux set-option -g focus-events on
ok "kel.conf wires the layout and focus hooks"    '[ "$(grep -c . "$WORK/hooks.conf")" = 2 ]'
tmux select-layout -t "$LW" main-vertical
ok "  ...a layout change re-docks by itself"      "wait_until 'docked $LW'"
ok "  ...rotate's keys re-dock too (it fires no layout hook)" '[ "$(grep -c "rotate-window.*_sidebar fit" "$HERE/../tmux/kel.conf")" = 2 ]'
hold_client 20
tmux select-window -t "$LW"; tmux select-pane -t "$LA"
tmux select-pane -t "$(sidebar_of "$LW")"
ok "selecting the sidebar by hand moves on to your pane" "wait_until '[ -z \"\$(tmux display -p -t $LW \"#{@kel-sidebar}\")\" ]'"
"$KEL" _sidebar focus "$LW"; sleep 0.3
ok "  ...prefix Space gets in, and stays"         '[ "$(tmux display -p -t "$LW" "#{@kel-sidebar}")" = 1 ]'
n0="$(tmux list-panes -t "$LW" | grep -c .)"
tmux send-keys -t "$(sidebar_of "$LW")" '|'
ok "| in the sidebar splits your pane"            "wait_until '[ \"\$(tmux list-panes -t $LW | grep -c .)\" = $((n0 + 1)) ]'"
# pwd -P: macOS's $TMPDIR is under /var, a link to /private/var, and tmux
# reports the resolved path.
ok "  ...at its directory, with the keyboard there" '[ "$(tmux display -p -t "$LW" "#{@kel-sidebar}|#{pane_current_path}")" = "|$(cd "$WORK/repos/api-gw" && pwd -P)" ]'
ok "  ...and focusing your pane cleared the way in" '[ -z "$(tmux show-options -wv -t "$LW" @kel-via 2>/dev/null)" ]'
tmux resize-pane -Z -t "$LA"
"$KEL" _click unzoom '' "$(tmux list-clients -F '#{client_name}' | head -1)"
ok "a click on the bar's zoomed restores the layout" '[ "$(tmux display -p -t "$LW" "#{window_zoomed_flag}")" = 0 ]'
release_client
tmux set-hook -gwu window-layout-changed; tmux set-hook -gwu pane-focus-in; tmux set-option -gu focus-events
tmux break-pane -d -s "$(sidebar_of "$LW")"
"$KEL" _sidebar heal
ok "a window holding only a sidebar is closed on heal" '! tmux list-panes -s -t =keltest -F "#{window_panes} #{@kel-sidebar}" | grep -qx "1 1"'
ok "  ...and the window it left gets one back"    "wait_until 'docked $LW'"
tmux kill-window -t "$LW"

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
hold_client 7
tmux select-window -t "$SBW"; "$KEL" _sidebar wake
sleep 5
ok "the shown sidebar reloads; the hidden one does not" '[ "$(reloads "$SB1")" -ge 3 ] && [ "$(reloads "$SB2")" -le 2 ]'
release_client

"$KEL" sidebar off >/dev/null 2>&1
ok "kel sidebar off removes every sidebar"        '[ "$(sbcount "$SBW")" = 0 ] && [ "$(sbcount "$OTW")" = 0 ]'
ok "  ...and gives tmux's bar back"                '[ "$(tmux show-options -v -t "=keltest:" status 2>/dev/null)" != off ]'
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

# ---- v0.10 M3: the sidebar itself, driven through its real pane
# A control client makes one window the shown one, so its sidebar loads live
# data; keys go in with send-keys, exactly as typed. Agents are sleep, also
# for the ones the sidebar's own `n` starts (KEL_AGENT reaches it through the
# server's environment).
export KEL_AGENT='sleep 9999'
reset
sbpane() { tmux list-panes -t "$1" -F '#{@kel-sidebar} #{pane_id}' | awk '$1==1 {print $2; exit}'; }
sbtext() { tmux capture-pane -p -t "$1" 2>/dev/null; }
active_win() { tmux list-windows -t '=keltest' -F '#{window_active} #{window_id}' | awk '$1==1 {print $2}'; }
ms() { perl -MTime::HiRes=time -e 'printf "%d\n", time*1000'; }   # BSD date has no %N
# when an assertion here fails, show what the sidebar had on screen — these
# have failed only on CI's macOS runner, where nothing can be looked at after
sbdiag() { # pane  fail-count-before
  [ "$fail" -gt "$2" ] || return 0
  printf '       sidebar %s showed:\n' "$1"; sbtext "$1" | sed 's/^/       | /'
  printf '       windows: %s\n' "$(tmux list-windows -t '=keltest' -F '#{window_active}#{window_name}' | tr '\n' ' ')"
}
K api-gw new wt -w >/dev/null 2>&1
WTW="$(window_of api-gw wt)"; agent_up "$WTW"
echo wip > "$WORK/repos/.kel-worktrees/api-gw-wt/scratch.txt"
NEW api-gw main1
MW="$(window_of api-gw main1)"
hold_client 40
tmux select-window -t "$MW"; "$KEL" _sidebar wake
SP="$(sbpane "$MW")"
wait_until 'sbtext "$SP" | grep -q "wt"' 50
ok "the sidebar draws spaces, then agents"       'sbtext "$SP" | grep -qx spaces && sbtext "$SP" | grep -qx agents'
ok "  ...the worktree as a space of its own"     '[ "$(sbtext "$SP" | grep -c "api-gw")" -ge 2 ]'
ok "  ...and selects with a bar, not inverse video" '! tmux capture-pane -e -p -t "$SP" | grep -q "\[7m" && sbtext "$SP" | grep -q "▌"'

tmux send-keys -t "$SP" j
wait_until '[ "$(jq -r .selected "$STATE/sidebar.json" 2>/dev/null)" = "a:$(userpane "$WTW")" ]'
ok "j moves the shared cursor"                   '[ "$(jq -r .selected "$STATE/sidebar.json")" = "a:$(userpane "$WTW")" ]'
tmux send-keys -t "$SP" Enter
wait_until '[ "$(active_win)" = "$WTW" ]'
ok "enter jumps to the agent's window"          '[ "$(active_win)" = "$WTW" ]'

# enter on a space header row jumps to that space's window
tmux select-window -t "$MW"
wait_until '[ "$(active_win)" = "$MW" ]' 50
tmux send-keys -t "$SP" k
wait_until '[[ "$(jq -r .selected "$STATE/sidebar.json" 2>/dev/null)" == s:* ]]' 50
tmux send-keys -t "$SP" Enter
wait_until '[ "$(active_win)" = "$WTW" ]' 50
ok "enter on a space header row jumps to that space's window" '[ "$(active_win)" = "$WTW" ]'

# invariant 1, through the sidebar: x, y on dirty work is refused by kel kill
"$KEL" _sidebar wake
SW="$(sbpane "$WTW")"
wait_until 'sbtext "$SW" | grep -q agents'
tmux send-keys -t "$SW" x
wait_until 'sbtext "$SW" | grep -q "kill wt?"'
ok "x asks before killing, and says how to answer" 'sbtext "$SW" | grep -q "kill wt?" && sbtext "$SW" | grep -q "y yes · n no"'
tmux send-keys -t "$SW" y
wait_until 'sbtext "$SW" | grep -q "at risk"' 80
ok "  ...and a dirty worktree is refused"        '[ -n "$(window_of api-gw wt)" ]'
ok "  ...naming the file at risk, in the sidebar" 'sbtext "$SW" | grep -q "scratch.txt"'

# n: a new agent in the selected entry's space, named in the footer
f0=$fail
tmux send-keys -t "$SW" n
wait_until 'sbtext "$SW" | grep -q "^new agent in "'
tmux send-keys -t "$SW" -l fresh
tmux send-keys -t "$SW" Enter
wait_until 'sbtext "$SW" | grep -q "^new agent where?"'
ok "n asks where, after the name"         'sbtext "$SW" | grep -q "enter window · | right · - down"'
tmux send-keys -t "$SW" Enter
# kel new stamps the window's group before it writes the record, so wait for
# both — waiting on the window alone raced the record on CI's macOS runner,
# as the sbdiag dump below showed (the window there, active, the record not)
wait_until '[ -n "$(window_of api-gw fresh)" ] && [ -f "$SESSIONS/api-gw/fresh.json" ]' 80
ok "n names and starts an agent, in that space's repo" '[ -n "$(window_of api-gw fresh)" ] && [ -f "$SESSIONS/api-gw/fresh.json" ]'
sbdiag "$SW" "$f0"
wait_until '[ "$(active_win)" = "$(window_of api-gw fresh)" ]' 50
tmux select-window -t "$WTW"; "$KEL" _sidebar wake
wait_until 'sbtext "$SW" | grep -q agents'
f0=$fail
tmux send-keys -t "$SW" n
wait_until 'sbtext "$SW" | grep -q "^new agent in "'
tmux send-keys -t "$SW" Enter
wait_until 'sbtext "$SW" | grep -q "^new agent where?"'
tmux send-keys -t "$SW" Enter
wait_until '[ -n "$(window_of api-gw api-gw-1)" ] && [ -f "$SESSIONS/api-gw/api-gw-1.json" ]' 80
ok "  ...and n, enter needs no name"      '[ -n "$(window_of api-gw api-gw-1)" ] && [ -f "$SESSIONS/api-gw/api-gw-1.json" ]'
SPW="$(window_of api-gw api-gw-1)"
tmux select-window -t "$SPW"; "$KEL" _sidebar wake
SW2="$(sbpane "$SPW")"
wait_until 'sbtext "$SW2" | grep -q agents'
tmux select-pane -t "$(userpane "$SPW")"; tmux select-pane -t "$SW2"
tmux send-keys -t "$SW2" n
wait_until 'sbtext "$SW2" | grep -q "^new agent in "'
tmux send-keys -t "$SW2" Enter
wait_until 'sbtext "$SW2" | grep -q "^new agent where?"'
tmux send-keys -t "$SW2" '|'
wait_until '[ -f "$SESSIONS/api-gw/api-gw-2.json" ]' 80
ok "  ...and n, enter, | splits the pane you were in" '[ "$(jq -r .window_id "$SESSIONS/api-gw/api-gw-2.json")" = "$SPW" ] && [ "$(tmux list-panes -t "$SPW" -F "#{@kel-agent}" | grep -cx "api-gw/api-gw-[12]")" = 2 ]'
sbdiag "$SW" "$f0"

# prefix Space is `kel _sidebar focus`; esc goes back. kel new ends by
# jumping to the window it made, just after the window exists — wait for that
# jump, or it can land after the select below and take the client with it
wait_until '[ "$(active_win)" = "$(window_of api-gw api-gw-1)" ]' 50
tmux select-window -t "$MW"
UP="$(userpane "$MW")"; tmux select-pane -t "$UP"
"$KEL" _sidebar focus "$MW"   # what prefix Space passes: the window it was pressed in
ok "_sidebar focus puts the keyboard in the sidebar" '[ "$(tmux display -p -t "$MW" "#{pane_id}")" = "$SP" ]'
f0=$fail
tmux send-keys -t "$SP" Escape
wait_until '[ "$(tmux display -p -t "$MW" "#{pane_id}")" = "$UP" ]'
ok "  ...and esc returns to your pane"           '[ "$(tmux display -p -t "$MW" "#{pane_id}")" = "$UP" ]'
sbdiag "$SP" "$f0"

# done until seen: finished while you were elsewhere stays loud until shown
hook "$(userpane "$WTW")" Stop
U() { "$KEL" _fleet | jq -r --arg w "$WTW" '.agents[] | select(.window_id == $w) | "\(.state) \(.unseen)"'; }
ok "a done agent you have not looked at is unseen" '[ "$(U)" = "done true" ]'
# the earlier jump already marked this window; time a fresh mark, not that
tmux select-window -t "$MW"; rm -f "$STATE/$WTW.seen"
t0="$(ms)"
tmux select-window -t "$WTW"; "$KEL" _sidebar wake
wait_until '[ -f "$STATE/$WTW.seen" ]' 30
t1="$(ms)"
# shown in the same second it finished is a tie, read unseen; the shown
# sidebar marks it again on its next load (2 s)
wait_until '[ "$(U)" = "done false" ]' 60
ok "  ...and seen once its window is shown"      '[ "$(U)" = "done false" ]'
printf '       (the shown sidebar noticed in %s ms)\n' "$((t1 - t0))"
ok "a window switch reaches the shown sidebar in under a second" '[ $((t1 - t0)) -lt 1000 ]'
release_client
unset KEL_AGENT

# ---- v0.10 M5: the popups and the embeds
# A popup needs a client with a terminal and the suite has only control-mode
# ones, so KEL_POPUP_TRACE records what would open — tool, directory, title —
# while everything before the draw (refusals, the tool lookup, the base, the
# socket) runs for real. The tools are stubs on PATH: CI has none of them.
export KEL_AGENT='sleep 9999' KEL_POPUP_TRACE="$WORK/popups" BROWSER="$WORK/shim/browser"
export XDG_RUNTIME_DIR="$WORK/run"; mkdir -p "$XDG_RUNTIME_DIR"
for t in yazi lazygit; do printf '#!/bin/sh\nexit 0\n' > "$WORK/shim/$t"; chmod +x "$WORK/shim/$t"; done
printf '#!/bin/sh\necho "$@" >> %s/nvim.calls\n' "$WORK" > "$WORK/shim/nvim"; chmod +x "$WORK/shim/nvim"
printf '#!/bin/sh\necho "$1" >> %s/browsed\n' "$WORK" > "$WORK/shim/browser"; chmod +x "$WORK/shim/browser"
: > "$KEL_POPUP_TRACE"
reset
R="$WORK/repos/api-gw"
K api-gw new wt5 -w >/dev/null 2>&1
W5="$(window_of api-gw wt5)"; agent_up "$W5"
WT5="$(cd "$WORK/repos/.kel-worktrees/api-gw-wt5" && pwd -P)"

# kel url: none, one, two
ok "kel url with no server exits 1"              '! "$KEL" url "$WT5" >/dev/null 2>&1'
listen() { # dir file — a server on a free port, which it writes to file
  tmux split-window -d -t "$W5" -c "$1" \
    "python3 -c 'import socket,time; s=socket.socket(); s.bind((\"127.0.0.1\",0)); s.listen(); open(\"$2\",\"w\").write(str(s.getsockname()[1])); time.sleep(9999)'"
  wait_until "[ -s '$2' ]" 50
}
listen "$WT5" "$WORK/p1"
wait_until '"$KEL" url "$WT5" >/dev/null 2>&1' 40
ok "  ...with one, its URL"                     '[ "$("$KEL" url "$WT5")" = "http://localhost:$(cat "$WORK/p1")" ]'
ok "  ...found by the agent's name too"         '[ "$("$KEL" url wt5)" = "http://localhost:$(cat "$WORK/p1")" ]'
listen "$WT5" "$WORK/p2"
wait_until '[ "$("$KEL" url "$WT5" | grep -c .)" = 2 ]' 40
ok "  ...with two, both, one per line"          '[ "$("$KEL" url "$WT5" | grep -c localhost)" = 2 ]'

# the popups, through the sidebar's keys
hold_client 40
tmux select-window -t "$W5"; "$KEL" _sidebar wake
S5="$(sbpane "$W5")"
wait_until 'sbtext "$S5" | grep -q wt5'
wait_until 'jq -e --arg d "$WT5" "[.panes[] | select(.cwd == \$d and .git.branch == \"wt5\")] | length > 0" "$STATE/sidebar-cache.json" >/dev/null 2>&1' 100
f0=$fail
tmux send-keys -t "$S5" f
wait_until 'grep -q yazi "$KEL_POPUP_TRACE"' 150
ok "f opens yazi, titled, at the worktree"       'grep -q "^$WT5	yazi · api-gw/wt5 · ? its keys · prefix q closes	yazi$" "$KEL_POPUP_TRACE"'
[ "$fail" -gt "$f0" ] && sed 's/^/       popup trace: /' "$KEL_POPUP_TRACE"
tmux send-keys -t "$S5" g
wait_until 'grep -q lazygit "$KEL_POPUP_TRACE"'
ok "g opens lazygit there"                      'grep -q "^$WT5	lazygit · api-gw/wt5 · ? its keys · prefix q closes	lazygit --use-config-file .*lazygit-overlay.yml$" "$KEL_POPUP_TRACE"'
ok "  ...where esc closes it, as everywhere in kel" 'grep -qx "quitOnTopLevelReturn: true" "$STATE/lazygit-overlay.yml"'

# o: the lowest port, the rest named in the footer
f0=$fail
tmux send-keys -t "$S5" o
wait_until '[ -s "$WORK/browsed" ]'
lo="$(sort -n "$WORK/p1" "$WORK/p2" | head -1)"; hi="$(sort -n "$WORK/p1" "$WORK/p2" | tail -1)"
ok "o opens the lowest port in the browser"     '[ "$(cat "$WORK/browsed")" = "http://localhost:$lo" ]'
ok "  ...and names the other"                   'wait_until "sbtext \"$S5\" | grep -q \"also :$hi\""'
sbdiag "$S5" "$f0"

# d: committed and uncommitted work against the land base
echo more >> "$WT5/README.md"
tmux send-keys -t "$S5" d
wait_until 'sbtext "$S5" | grep -q "^› main"'
ok "d asks against what, offering the base it lands on" 'sbtext "$S5" | grep -q "^diff wt5 against" && sbtext "$S5" | grep -q "^› main"'
tmux send-keys -t "$S5" Enter
wait_until 'grep -q "diff ·" "$KEL_POPUP_TRACE"'
ok "d diffs the worktree against its base"      'grep -q "^$WT5	diff · api-gw/wt5 · main · prefix q closes	" "$KEL_POPUP_TRACE"'
# ;: a kel command typed in the sidebar, answered in its footer
tmux send-keys -t "$S5" '\;'   # escaped: a lone ; is tmux's command separator
wait_until 'sbtext "$S5" | grep -q "^kel command"'
tmux send-keys -t "$S5" -l "url $WORK/repos"; tmux send-keys -t "$S5" Enter
wait_until 'sbtext "$S5" | grep -q "no server listening"' 50
ok "; runs a kel command and answers in the footer" 'sbtext "$S5" | grep -q "no server listening"'
git -C "$WT5" checkout -q -- README.md
ok "  ...no changes is said, not an empty diff" '[[ "$("$KEL" _panel diff "$WT5" t main 2>&1)" == *"no changes against main"* ]]'
ok "  ...and no base is said, not an empty diff" '[[ "$("$KEL" _panel diff "$WT5" t "" 2>&1)" == *"no base"* ]]'

# e: the edit hook, then the file — into a running nvim, else a popup
K5="$(userpane "$W5")"
ok "e with nothing edited says so"              '[[ "$("$KEL" _panel edit "$WT5" t "$W5" 2>&1)" == *"nothing edited yet"* ]]'
printf '%s' "{\"session_id\":\"s\",\"cwd\":\"$WT5\",\"hook_event_name\":\"PostToolUse\",\"tool_name\":\"Bash\",\"tool_input\":{\"command\":\"ls\"}}" \
  | TMUX_PANE="$(userpane "$W5")" "$KEL" hook PostToolUse
ok "a tool call that edits no file records nothing" '[ ! -f "$STATE/$K5.edit" ]'
printf '%s' "{\"session_id\":\"s\",\"cwd\":\"$WT5\",\"hook_event_name\":\"PostToolUse\",\"tool_name\":\"Edit\",\"tool_input\":{\"file_path\":\"$WT5/README.md\"},\"tool_response\":{\"structuredPatch\":[{\"newStart\":12}]}}" \
  | TMUX_PANE="$(userpane "$W5")" "$KEL" hook PostToolUse
ok "an Edit records the file and its first changed line" '[ "$(sed -n 1p "$STATE/$K5.edit")" = "$WT5/README.md" ] && [ "$(sed -n 2p "$STATE/$K5.edit")" = 12 ]'
ok "  ...and leaves the agent's state alone"    '[ ! -f "$STATE/$K5.state" ]'
"$KEL" _panel edit "$WT5" api-gw/wt5 "$W5" >/dev/null 2>&1
ok "e with no nvim open there: a popup at the line" 'grep -q "^$WT5	nvim · api-gw/wt5 · prefix q closes	nvim +12 $WT5/README.md$" "$KEL_POPUP_TRACE"'
# a pane whose process is called nvim (a renamed sleep), and a socket where
# nvim 0.9+ would listen: e must reach it over the socket and select the pane
mkdir -p "$WORK/nvimbin"; cp "$(command -v sleep)" "$WORK/nvimbin/nvim"
NP="$(tmux split-window -d -P -F '#{pane_id}' -t "$W5" -c "$WT5" "'$WORK/nvimbin/nvim' 9999")"
wait_until '[ "$(tmux display -p -t "$NP" "#{pane_current_command}")" = nvim ]'
python3 -c 'import socket,sys; socket.socket(socket.AF_UNIX).bind(sys.argv[1])' "$XDG_RUNTIME_DIR/nvim.$(tmux display -p -t "$NP" '#{pane_pid}').0"
: > "$WORK/nvim.calls"
"$KEL" _panel edit "$WT5" api-gw/wt5 "$W5" >/dev/null 2>&1
ok "e with nvim open there: the file goes over its socket" 'grep -q -- "--server $XDG_RUNTIME_DIR/nvim\..*--remote-expr .*edit +12 .*README.md" "$WORK/nvim.calls"'
ok "  ...and its pane is selected"              '[ "$(tmux display -p -t "$W5" "#{pane_id}")" = "$NP" ]'

# ---- v0.10 M6: the plan and the tasks — against a CLAUDE_CONFIG_DIR of the
# suite's own, never the developer's ~/.claude
export CLAUDE_CONFIG_DIR="$WORK/claude"; mkdir -p "$CLAUDE_CONFIG_DIR/plans"
ok "p with no plan and no tasks says so"        '[[ "$("$KEL" _panel plan "$WT5" t "$W5" 2>&1)" == *"no plan or task list"* ]]'
PLAN="$CLAUDE_CONFIG_DIR/plans/gentle-otter.md"
printf '# Fix the parser\n\n1. read it\n2. fix it\n' > "$PLAN"
printf '%s' "{\"session_id\":\"s\",\"cwd\":\"$WT5\",\"hook_event_name\":\"PostToolUse\",\"tool_name\":\"Write\",\"tool_input\":{\"file_path\":\"$PLAN\"}}" \
  | TMUX_PANE="$(userpane "$W5")" "$KEL" hook PostToolUse
ok "a Write under the plans dir is the plan"     '[ "$(sed -n 1p "$STATE/$K5.plan")" = "$PLAN" ]'
ok "  ...and not the last edit"                  '[ "$(sed -n 1p "$STATE/$K5.edit")" = "$WT5/README.md" ]'
: > "$KEL_POPUP_TRACE"
"$KEL" _panel plan "$WT5" api-gw/wt5 "$W5" >/dev/null 2>&1
ok "p opens the plan in a popup"                 'grep -q "^$WT5	plan · api-gw/wt5 · prefix q closes	" "$KEL_POPUP_TRACE"'
ok "  ...the plan's own text"                    'grep -q "Fix the parser" "$STATE/$K5.view"'
ok "  ...and says when the session keeps no tasks" 'grep -q "no task list" "$STATE/$K5.view"'
# tasks: Claude Code's own list, as it stands on disk for the agent's session
jq '.claude_session = "sess5"' "$SESSIONS/api-gw/wt5.json" > "$WORK/r" && mv "$WORK/r" "$SESSIONS/api-gw/wt5.json"
TD="$CLAUDE_CONFIG_DIR/tasks/sess5"; mkdir -p "$TD"
printf '{"id":"1","subject":"read it","status":"completed","blockedBy":[]}'  > "$TD/1.json"
printf '{"id":"2","subject":"fix it","activeForm":"Fixing it","status":"in_progress","blockedBy":[]}' > "$TD/2.json"
printf '{"id":"3","subject":"ship it","status":"pending","blockedBy":["2"]}' > "$TD/3.json"
printf '{"id":"4","subject":"dropped","status":"deleted","blockedBy":[]}'    > "$TD/4.json"
"$KEL" _panel plan "$WT5" api-gw/wt5 "$W5" >/dev/null 2>&1
V="$STATE/$K5.view"
ok "tasks: done, in progress (as it is phrased), pending" 'grep -q "^✓ read it" "$V" && grep -q "^▸ Fixing it" "$V" && grep -q "^· ship it" "$V"'
ok "  ...with what blocks what"                  'grep -q "ship it.*blocked by 2" "$V"'
ok "  ...the count, deleted ones gone"           'grep -q "1 of 3 done" "$V" && ! grep -q dropped "$V"'
printf '{"id":"5", trunc' > "$TD/5.json"
"$KEL" _panel plan "$WT5" api-gw/wt5 "$W5" >/dev/null 2>&1
ok "a file that does not parse: unreadable, not a shorter list" 'grep -q "unreadable.*5.json" "$V" && ! grep -q "✓ read it" "$V"'
rm -rf "$TD" "$STATE/$K5.plan"
ok "with neither again, p refuses"              '! "$KEL" _panel plan "$WT5" t "$W5" >/dev/null 2>&1'
mkdir -p "$TD"; printf '{"id":"1","subject":"solo","status":"pending","blockedBy":[]}' > "$TD/1.json"
ok "tasks alone are enough for p, the plan said missing" '"$KEL" _panel plan "$WT5" t "$W5" >/dev/null 2>&1 && grep -q "has not written one" "$V" && grep -q "solo" "$V"'
unset CLAUDE_CONFIG_DIR

# the hosted session behind every real popup: a popup takes every key, so it
# attaches to a small session of its own where prefix q works. Drawing needs a
# terminal client the suite lacks — its control client is hung up by one — so
# KEL_POPUP_NODRAW stops just before; the draw was checked by hand
printf '#!/bin/sh\nexec sleep 30\n' > "$WORK/shim/yazi"
( unset KEL_POPUP_TRACE; KEL_POPUP_NODRAW=1 TMUX="$TMUX_TMPDIR/tmux-$(id -u)/default,1,0" "$KEL" _panel files "$WT5" api-gw/wt5 ) >/dev/null 2>&1
POP="$(tmux list-sessions -F '#{session_name}' | grep '^_kelpop_' | head -1)"
ok "a popup's tool runs in a hosted session"     '[ -n "$POP" ] && [ "$(tmux list-panes -t "=$POP:" -F "#{pane_current_command}")" = sleep ]'
ok "  ...marked as a popup's"                      '[ "$(tmux show-options -v -t "=$POP:" @kel-popup)" = 1 ]'
ok "  ...nor a pane title — kel's labels stay out" '[ "$(tmux show-options -wv -t "=$POP:" pane-border-status)" = off ]'
ok "  ...but a bar of its own with a clickable close" '[[ "$(tmux show-options -v -t "=$POP:" status-left)" == *"range=user|close"*"close"*"Ctrl-b then q"* ]]'
ok "  ...which kel never counts: not the fleet"  '! "$KEL" _fleet --panes | jq -r ".panes[].window_id" | grep -qxF "$(tmux display -p -t "=$POP:" "#{window_id}")"'
ok "  ...nor gives a sidebar"                    '[ "$(tmux list-panes -t "=$POP:" -F x | grep -c x)" = 1 ]'
"$KEL" snapshot >/dev/null 2>&1
ok "  ...nor snapshots"                           '! grep -q _kelpop_ "$STATE/snapshot.json" && [ "$(jq ".windows | length" "$STATE/snapshot.json")" = "$(tmux list-windows -t =keltest -F x | grep -c x)" ]'
( unset KEL_POPUP_TRACE; KEL_POPUP_NODRAW=1 TMUX="$TMUX_TMPDIR/tmux-$(id -u)/default,1,0" "$KEL" _panel files "$WT5" api-gw/wt5 ) >/dev/null 2>&1
ok "files and git keep theirs between opens"     '[ "$(tmux list-sessions -F "#{session_name}" | grep -c "^_kelpop_")" = 1 ]'
tmux kill-session -t "=$POP" 2>/dev/null
printf '#!/bin/sh\nexit 0\n' > "$WORK/shim/yazi"
# read from kel.conf itself: on CI's macOS runner (Homebrew tmux 3.7) a
# throwaway server's list-keys printed nothing at all, start-server or a real
# session alike, while Ubuntu's 3.4 printed the binding. The file loading on
# both is CI's own "kel.conf parses" step; the binding's behaviour was checked
# by hand on 3.4 and is unverified on 3.7
ok "kel.conf: prefix q closes a popup, and only a popup" \
  'grep -qxF "bind-key q if-shell -F '"'"'#{@kel-popup}'"'"' '"'"'detach-client'"'"' '"'"'display-panes'"'"'" "$HERE/../tmux/kel.conf"'

# ---- daily-use batch (2026-10-15): from the author's screenshots
# idle_prompt: an interrupt fires no Stop, so a turn stopped with esc stayed
# `working` for 13 hours; Claude Code's ~60 s idle_prompt now ends it
UP5="$(userpane "$W5")"
hook "$UP5" UserPromptSubmit
hook "$UP5" Notification idle_prompt
ok "idle_prompt ends a working that no Stop ended" '[ "$(cut -d" " -f1 "$STATE/$K5.state")" = idle ]'
hook "$UP5" Stop
hook "$UP5" Notification idle_prompt
ok "  ...and leaves any other state alone"      '[ "$(cut -d" " -f1 "$STATE/$K5.state")" = done ]'
# e on a space: your nvim at that directory, kept between opens
: > "$KEL_POPUP_TRACE"
"$KEL" _panel editor "$WT5" api-gw/wt5 >/dev/null 2>&1
ok "e on a space opens nvim there"              'grep -q "^$WT5	nvim · api-gw/wt5 · prefix q closes	nvim .$" "$KEL_POPUP_TRACE"'
# kel shell: a plain window, no agent, no record
"$KEL" shell "$WT5" >/dev/null 2>&1
SHW="$(tmux list-windows -t =keltest -F '#{window_id} #{window_name}' | awk '$2=="api-gw-wt5" {print $1; exit}')"
ok "kel shell opens a plain window in that directory" '[ -n "$SHW" ] && [ "$(tmux display -p -t "$SHW" "#{@kel-group}")" = api-gw ] && [ "$(tmux display -p -t "$SHW" "#{pane_current_path}")" = "$WT5" ]'
ok "  ...in its repo's group, not the worktree folder's" '[ -n "$SHW" ] && [ "$(tmux display -p -t "$SHW" "#{@kel-group}")" != api-gw-wt5 ]'
ok "  ...with its sidebar, and no agent record"  '[ -n "$SHW" ] && [ "$(sbcount "$SHW")" = 1 ] && ! grep -rqs "\"window_id\": *\"$SHW\"" "$SESSIONS"'
# prefix k: a multi-line answer opens in a popup, not view-mode on your pane
: > "$KEL_POPUP_TRACE"
"$KEL" _prompt "url $WT5" >/dev/null 2>&1
ok "prefix k: a long answer goes to a popup"     'grep -q "kel url $WT5 · prefix q closes	less -R" "$KEL_POPUP_TRACE" && grep -c localhost "$STATE/prompt.out" | grep -qx 2'
# ---- Claude Code's own view: `claude agents --json`, and subagents
# '|', not a space: awk folds a leading empty field away, and the user's pane
# is the one whose @kel-sidebar is empty
SPID="$(tmux list-panes -t "$W5" -F '#{@kel-sidebar}|#{pane_pid}' | awk -F'|' '$1=="" {print $2; exit}')"
APID="$(pgrep -P "$SPID" sleep | head -1)"
hook "$UP5" UserPromptSubmit
printf '[{"pid":%s,"cwd":"%s","kind":"interactive","sessionId":"s5","name":"wt5","status":"idle"},{"pid":1,"id":"bg77","cwd":"%s","kind":"background","sessionId":"s77","name":"runner","status":"busy"}]' \
  "${APID:-0}" "$WT5" "$WT5" > "$WORK/claude-agents.json"
rm -f "$STATE/claude-agents.json"
FA() { "$KEL" _fleet --panes | jq -r "$1"; }
ok "working, but Claude Code says idle: idle"     '[ "$(FA ".agents[] | select(.name==\"wt5\") | .state")" = idle ]'
ok "  ...the hook state alone still says working" '[ "$("$KEL" _fleet | jq -r ".agents[] | select(.name==\"wt5\") | .state")" = working ]'
ok "a background agent is listed, with its repo"  '[ "$(FA ".background[0].id")" = bg77 ] && [ "$(FA ".background[0].group")" = api-gw ]'
printf '[{"pid":1,"id":"bg55","cwd":"%s","kind":"background","sessionId":"s55","name":"wt5","status":"busy"}]' "$WT5" > "$WORK/claude-agents.json"
rm -f "$STATE/claude-agents.json"; hook "$UP5" Stop
ok "a background session named like an agent is that agent" '[ "$(FA ".background | length")" = 0 ] && [ "$(FA ".agents[] | select(.name==\"wt5\") | .state + \"/\" + .claude.status")" = working/busy ]'
rm -f "$STATE/claude-agents.json"; hook "$UP5" UserPromptSubmit
printf '#!/bin/sh\nexit 1\n' > "$WORK/claude-stub.fail"; chmod +x "$WORK/claude-stub.fail"
rm -f "$STATE/claude-agents.json"
ok "no Claude Code to ask: the hooks alone, and no background list" \
  '[ "$(KEL_CLAUDE_BIN="$WORK/claude-stub.fail" "$KEL" _fleet --panes | jq -r ".agents[] | select(.name==\"wt5\") | .state, (.background == null)" | tr "\n" " ")" = "working true " ]'
rm -f "$STATE/claude-agents.json" "$WORK/claude-agents.json"
# subagents: kel substatus records the panel's rows and prints nothing
sub="$(printf '%s' "{\"session_id\":\"s\",\"cwd\":\"$WT5\",\"tasks\":[{\"id\":\"t1\",\"type\":\"local_agent\",\"agentType\":\"Explore\",\"status\":\"running\",\"description\":\"Find the parser\",\"label\":\"Reading parser.go\",\"startTime\":1}]}" \
  | TMUX_PANE="$UP5" "$KEL" substatus)"
ok "kel substatus prints nothing (Claude's rows stay its own)" '[ -z "$sub" ]'
ok "  ...and the agent's subagents are in the fleet" '[ "$("$KEL" _fleet | jq -r ".agents[] | select(.name==\"wt5\") | .subagents[0] | .label + \"/\" + .agentType")" = "Reading parser.go/Explore" ]'

# ---- phase 3: usage, PRs, the claude config, a pane as a picture
printf '#!/bin/sh\necho ccusage "$@"\n' > "$WORK/shim/ccusage"; chmod +x "$WORK/shim/ccusage"
"$KEL" _panel usage "$WT5" >/dev/null 2>&1
ok "u shows ccusage: this block, two weeks, then any report" 'grep -q "usage · ccusage · q, then another report" "$KEL_POPUP_TRACE" && grep -q "ccusage blocks --active --color" "$KEL_POPUP_TRACE" && grep -q "ccusage \$r --color" "$KEL_POPUP_TRACE"'
mkdir -p "$WORK/ghshim"; printf '#!/bin/sh\n[ "$1 $2" = "extension list" ] && printf "gh dash\\tdlvhdr/gh-dash\\tv4\\n"\nexit 0\n' > "$WORK/ghshim/gh"; chmod +x "$WORK/ghshim/gh"
( cd "$WT5" && git remote add origin https://github.com/me/api-gw.git )
PATH="$WORK/ghshim:$PATH" "$KEL" _panel dash "$WT5" >/dev/null 2>&1
ok "v opens gh dash on every open space's repos" 'grep -q "PRs · every open space · / search · ? its keys.*gh dash --config $STATE/gh-dash.yml$" "$KEL_POPUP_TRACE"'
ok "  ...the selected one's first, then all of them" 'grep -A1 "title: api-gw$" "$STATE/gh-dash.yml" | grep -q "repo:me/api-gw" && grep -q "title: mine, every space" "$STATE/gh-dash.yml"'
( cd "$WT5" && git remote remove origin )
printf '#!/bin/sh\n[ "$1 $2" = "extension list" ] && exit 0\nexit 0\n' > "$WORK/ghshim/gh"
ok "  ...and says so when gh-dash is missing"    '[[ "$(PATH="$WORK/ghshim:$PATH" "$KEL" _panel dash "$WT5" t 2>&1)" == *"gh-dash is not installed"* ]]'
CC="$WORK/ccfg"; MEM="$CC/projects/$(printf '%s' "$WT5" | sed 's/[^A-Za-z0-9]/-/g')/memory"
mkdir -p "$MEM" "$WT5/.claude"; echo g > "$CC/CLAUDE.md"; echo p > "$WT5/CLAUDE.md"
CLAUDE_CONFIG_DIR="$CC" "$KEL" _panel claude "$WT5" api-gw/wt5 >/dev/null 2>&1
ok "c opens the claude config in yazi, a tab per place" 'grep -qF "claude config · api-gw/wt5 · tabs: 1-4 · prefix q closes	yazi $WT5/CLAUDE.md $WT5/.claude $CC/CLAUDE.md $MEM" "$KEL_POPUP_TRACE"'
rm -rf "$WT5/.claude" "$WT5/CLAUDE.md"
printf '#!/bin/sh\nwhile [ $# -gt 0 ]; do [ "$1" = -o ] && o="$2"; shift; done\ncat > "$o"\n' > "$WORK/shim/freeze"; chmod +x "$WORK/shim/freeze"
sh_out="$(TMUX_PANE="$(userpane "$W5")" KEL_SHOT_DIR="$WORK/shots" "$KEL" shot 2>&1)"
ok "kel shot draws the pane to an svg, and says where" '[[ "$sh_out" == *"$WORK/shots/wt5-"*.svg* ]] && [ -s "$(ls "$WORK"/shots/*.svg | head -1)" ]'
rm -f "$WORK/shim/ccusage" "$WORK/shim/freeze"

# kel quit: never the only copy of work, then everything, saved first
echo wip > "$WT5/scratch-quit.txt"
qout="$("$KEL" quit 2>&1)"
ok "kel quit refuses while a worktree holds unsaved work" '[[ "$qout" == *"refusing to quit"* ]] && tmux has-session -t =keltest 2>/dev/null'
ok "  ...and names the file at risk"            '[[ "$qout" == *scratch-quit.txt* ]]'
rm -f "$WT5/scratch-quit.txt"
tmux new-session -d -s _kelpop_quitcheck 'sleep 30'
"$KEL" quit >/dev/null 2>&1
ok "kel quit closes kel and its popups' sessions" '! tmux has-session -t =keltest 2>/dev/null && ! tmux has-session -t =_kelpop_quitcheck 2>/dev/null'
ok "  ...having saved the workspace first"      'jq -r ".windows[].name" "$STATE/snapshot.json" | grep -qx wt5'

ok "a missing tool is said, not a crash"        '[[ "$(PATH="/usr/bin:/bin" KEL_FLEET_BIN="$KEL_FLEET_BIN" "$KEL" _panel files "$WT5" t 2>&1)" == *"yazi is not installed"* ]]'
release_client
unset KEL_AGENT KEL_POPUP_TRACE BROWSER XDG_RUNTIME_DIR
rm -f "$WORK/shim/yazi" "$WORK/shim/lazygit" "$WORK/shim/nvim" "$WORK/shim/browser"

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
K0="$(userpane "$W0")"
printf 'idle 1\n'    > "$STATE/$W0.state"
"$KEL" ls >/dev/null 2>&1
ok "prune drops a stale .state"          '[ ! -e "$STATE/@99.state" ]'
ok "prune drops a stale .ctx"            '[ ! -e "$STATE/@99.ctx" ]'
ok "prune keeps a LIVE agent's state"     '[ -e "$STATE/$K0.state" ]'
ok "  ...moved from its window to its pane" '[ ! -e "$STATE/$W0.state" ] && [ "$(cut -d" " -f1 "$STATE/$K0.state")" = idle ]'
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
# the other half of invariant 1: committed but on no remote.  Only a repo with
# a remote can say "unpushed" — without one every commit would read that way.
UR="$WORK/unpushed"; UO="$WORK/unpushed-origin.git"
git init -q --bare -b main "$UO"
git init -q -b main "$UR" && git -C "$UR" -c user.email=t@t -c user.name=t commit -q --allow-empty -m base
git -C "$UR" remote add origin "$UO" && git -C "$UR" push -q origin main
( cd "$UR" && "$KEL" new up -w --agent 'sleep 9999' >/dev/null 2>&1 )
UWT="$(jq -r .cwd "$SESSIONS/unpushed/up.json" 2>/dev/null)"
git -C "$UWT" -c user.email=t@t -c user.name=t commit -q --allow-empty -m "only here"
out="$("$KEL" kill unpushed/up 2>&1)"
ok "a worktree whose commits are on no remote is refused" '[[ "$out" == *refusing* ]] && [ -n "$(window_of unpushed up)" ]'
ok "  ...naming the commit at risk"       '[[ "$out" == *"only here"* ]]'
"$KEL" kill unpushed/up -f >/dev/null 2>&1

# Issue #2: a worktree whose commits are in a merged PR's head is safe to kill
# even if the remote tracking branch was deleted on merge
MR="$WORK/merged-pr"; MO="$WORK/merged-origin.git"
git init -q --bare -b main "$MO"
git init -q -b main "$MR" && git -C "$MR" -c user.email=t@t -c user.name=t commit -q --allow-empty -m base
git -C "$MR" remote add origin "$MO" && git -C "$MR" push -q origin main
git -C "$MR" remote set-url origin "https://github.com/example/merged.git"
git -C "$MR" remote set-url --push origin "$MO"
( cd "$MR" && "$KEL" new mp -w --agent 'sleep 9999' >/dev/null 2>&1 )
MWT="$(jq -r .cwd "$SESSIONS/merged/mp.json" 2>/dev/null)"
git -C "$MWT" -c user.email=t@t -c user.name=t commit -q --allow-empty -m "in pr"
MP_HEAD="$(git -C "$MWT" rev-parse HEAD)"
mkdir -p "$STATE/pr-cache"
printf '[{"sha":"%s","merged":true,"state":"closed"}]\n' "$MP_HEAD" > "$STATE/pr-cache/example__merged.sha.json"
out="$("$KEL" kill merged/mp 2>&1)"
ok "a worktree in a merged PR is safe to kill without -f" '[[ "$out" == *killed* ]]'

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
# never `waiting` — it is you being idle, not the agent asking. Since
# 2026-10-15 it does end a `working` no Stop ended (an interrupt): idle
ok "idle_prompt does NOT claim your attention"      '[ "$(state_of a)" != waiting ] && [ "$(state_of a)" = idle ]'
hook "$P" Notification agent_completed "done"
ok "agent_completed -> done"                        '[ "$(state_of a)" = done ]'
hook "$P" Notification quota_auto_resume_fired "q"
ok "quota_auto_resume_* -> throttled"               '[ "$(state_of a)" = throttled ]'
hook "$P" Notification agent_needs_input "which?"
ok "agent_needs_input -> waiting"                   '[ "$(state_of a)" = waiting ]'
hook "$P" Notification some_type_from_the_future "?"
ok "an unknown type falls back to waiting"          '[ "$(state_of a)" = waiting ]'
tooluse() { printf '%s' "{\"session_id\":\"s\",\"cwd\":\"$WORK\",\"hook_event_name\":\"PostToolUse\",\"tool_name\":\"$2\",\"tool_input\":{\"command\":\"ls\"}}" | TMUX_PANE="$1" "$KEL" hook PostToolUse; }
tooluse "$P" Bash
ok "a tool run after a wait -> working"             '[ "$(state_of a)" = working ]'
hook "$P" Stop
tooluse "$P" Bash
ok "  ...but a tool never wakes a finished agent"   '[ "$(state_of a)" = done ]'

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
# the banner: a line at the top while an agent waits, gone when it moves on
: > "$NLOG"; hook "$P" UserPromptSubmit; hook "$P" Notification permission_prompt 'run #(touch pwned)?'
AW="$(window_of api-gw a)"
ok "a waiting agent puts up the banner, above the bar" '[ "$(tmux show-options -v -t "=keltest:" status)" = 2 ] && [ -n "$(tmux show-options -v -t "=keltest:" status-format[1])" ] && [[ "$(tmux show-options -v -t "=keltest:" status-format[0])" == *"a#[nobold,fg=colour252] is waiting"* ]]'
ok "  ...its text printed, never run as a format" '[[ "$(tmux show-options -v -t "=keltest:" status-format[0])" == *"##(touch pwned)"* ]]'
hook "$P" UserPromptSubmit
# "is waiting" alone let a bug through: an emptied banner file once drew
# "● is" from a blank line, which contains no "is waiting".  Check the bar
# itself is gone, not just one phrase.
ok "  ...and takes it down when the agent moves on" '[ ! -s "$STATE/banner" ] && [ -z "$(tmux show-options -v -t "=keltest:" status-format[0] 2>/dev/null)" ] && [ -z "$(tmux show-options -v -t "=keltest:" status 2>/dev/null)" ]'
NEW api-gw b
hold_client 20
tmux select-window -t "$(window_of api-gw b)"
hook "$P" Notification permission_prompt "needs Bash"
ok "  ...two lines: one per waiting agent" '[ "$(grep -c . "$STATE/banner")" = 1 ]'
tmux select-window -t "$AW"; "$KEL" _banner seen
ok "visiting the agent clears its line"   '[ ! -s "$STATE/banner" ]'
tmux select-window -t "$(window_of api-gw b)"; hook "$P" UserPromptSubmit; hook "$P" Notification permission_prompt "again"
"$KEL" _click bx '' ''
ok "✕ dismisses it"                        '[ ! -s "$STATE/banner" ]'
ok "kel.conf clears it when a window is visited" 'grep -q "session-window-changed.*kel _banner seen" "$HERE/../tmux/kel.conf"'
release_client
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

section "kel reload refreshes config and restarts sidebars in place (#4)"
reset
export KEL_SIDEBAR=on
K api-gw new a1 -w --agent 'sleep 9999' >/dev/null 2>&1
WID="$(window_of api-gw a1)"
wait_until '[ -n "$(sbpane "$WID")" ]' 50
SB_PANE="$(sbpane "$WID")"
AGENT_PANE="$(userpane "$WID")"

SB_PID_BEFORE="$(tmux display-message -p -t "$SB_PANE" '#{pane_pid}')"
AGENT_PID_BEFORE="$(tmux display-message -p -t "$AGENT_PANE" '#{pane_pid}')"

"$KEL" reload >/dev/null 2>&1
# wait for sidebar process to respawn with a new pid
wait_until '[ "$(tmux display-message -p -t "'"$SB_PANE"'" "#{pane_pid}")" != "'"$SB_PID_BEFORE"'" ]' 50
SB_PID_AFTER="$(tmux display-message -p -t "$SB_PANE" '#{pane_pid}')"
AGENT_PID_AFTER="$(tmux display-message -p -t "$AGENT_PANE" '#{pane_pid}')"

ok "after reload, the sidebar pane has a new pid" '[ -n "$SB_PID_AFTER" ] && [ "$SB_PID_AFTER" != "$SB_PID_BEFORE" ]'
ok "  ...and the agent pane pid is unchanged (invariant 4)" '[ "$AGENT_PID_AFTER" = "$AGENT_PID_BEFORE" ]'

# with sidebar-off, nothing is respawned
touch "$STATE/sidebar-off"
SB_PID_SAVED="$SB_PID_AFTER"
"$KEL" reload >/dev/null 2>&1
sleep 0.5
SB_PID_OFF="$(tmux display-message -p -t "$SB_PANE" '#{pane_pid}')"
ok "with sidebar-off, no sidebar is respawned" '[ "$SB_PID_OFF" = "$SB_PID_SAVED" ]'
rm -f "$STATE/sidebar-off"
export KEL_SIDEBAR=off

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
       | grep -vE '^(hook|statusline|substatus|status-line|snapshot|jump|attach|list|menu|help|cheat|-h|--help)$' \
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
# two: kel config, and the sidebar's popup() (M5) — whose comment
# names -EE again, so it is counted on the option line only
POPUP_EE=$(grep -c -- '-EE -w\|-EE "' "$HERE/../bin/kel")
ok "the config/sidebar popups use -EE" '[ "$POPUP_EE" = 2 ]'
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
# The bind this holds onto is prefix , (`kel rename`), which refuses a bad
# name with die. prefix t (`kel top`) carried it until kel top went.
KELBINDS=$(grep -n '^bind.*run-shell .*local/bin/kel rename' "$CONF")
ok "kel.conf's foreground kel bind exists"             '[ "$(printf "%s\n" "$KELBINDS" | grep -c .)" = 1 ]'
ok "  ...and surfaces run-shell's stderr"               '[ "$(printf "%s\n" "$KELBINDS" | grep -c "2>&1")" = 1 ]'
# per kel COMMAND, not per line: a hook chains several (wake; heal; banner
# seen), and a whole-line check passed as soon as any one was silenced
HOOKCMDS="$(grep "^set-hook" "$CONF" | grep "run-shell" | sed "s/.*run-shell -b '//; s/'\"$//" | tr ';' '\n' | grep 'bin/kel')"
ok "  ...while every hook discards its output"          '[ -n "$HOOKCMDS" ] && [ -z "$(printf "%s\n" "$HOOKCMDS" | grep -v ">/dev/null 2>&1")" ]'
ok "  ...counted per command"                           'printf "%s\n" "$HOOKCMDS" | sed "1s/ >\/dev\/null 2>&1//" | grep -qv ">/dev/null 2>&1"'
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
sed "s/kel rename '%%' 2>&1\"/kel rename '%%'\"/" "$CONF" > "$WORK/kel-conf-revert"
REVERTED=$(grep -n '^bind.*run-shell .*local/bin/kel rename' "$WORK/kel-conf-revert")
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
ok "a .ctx file is written"                         '[ -f "$STATE/$P.ctx" ]'
ok "  ...with the right percentage"                 '[ "$(cut -f1 "$STATE/$P.ctx")" = 42 ]'
ok "  ...and the model in the last field"           '[ "$(cut -f7 "$STATE/$P.ctx")" = "Opus 5" ]'
ok "ls surfaces it"                                 '[[ "$("$KEL" ls 2>/dev/null)" == *42%* ]]'
ok "--json surfaces it"                             '[ "$("$KEL" ls --json | jq -r ".agents[0].context.pct")" = 42 ]'
# the IFS-tab-folding regression: an empty field must not shift every later value
sl '{"session_id":"a","cost":{"total_cost_usd":9.25},
     "context_window":{"total_input_tokens":170000,"context_window_size":200000,"used_percentage":86}}' >/dev/null
ok "an empty field does not shift the others"       '[ "$(cut -f1 "$STATE/$P.ctx")" = 86 ] && [ "$(cut -f2 "$STATE/$P.ctx")" = 9.25 ]'
# a malformed payload must never blank a good record
for bad in '' 'not json' '{}' '{"context_window":null}'; do sl "$bad" >/dev/null 2>&1; done
ok "garbage payloads cannot blank a good record"    '[ "$(cut -f1 "$STATE/$P.ctx")" = 86 ]'

# used_percentage is null before the first API call and again right after
# /compact. That is "not measured", not "empty", and the two must render
# differently.
NULLPCT='{"session_id":"a","model":{"display_name":"Opus 5"},
          "context_window":{"total_input_tokens":170000,"context_window_size":200000,"used_percentage":null}}'
ok "an unmeasured context renders as unknown"       '[[ "$(sl "$NULLPCT")" == *"ctx ?"* ]]'
ok "  ...and not as 0%"                             '[[ "$(sl "$NULLPCT")" != *"0%"* ]]'
ok "  ...and does not overwrite a real record"      '[ "$(cut -f1 "$STATE/$P.ctx")" = 86 ]'
ok "a real 0% still renders as a figure"            '[[ "$(sl "{\"session_id\":\"a\",\"context_window\":{\"context_window_size\":200000,\"used_percentage\":0}}")" == *"0%"* ]]'

# `agent.name` marks a session started with `claude --agent`, NOT a Task-tool
# subagent — those render through a separate `subagentStatusLine` setting and
# never reach this channel. An --agent session is primary and records normally.
AGENTPL='{"session_id":"a","agent":{"name":"Explore"},
          "context_window":{"total_input_tokens":8000,"context_window_size":200000,"used_percentage":4}}'
AGENTROW="$(sl "$AGENTPL")"
ok "an --agent session still records context"       '[ "$(cut -f1 "$STATE/$P.ctx")" = 4 ]'
ok "  ...and names the agent in the row"            '[[ "$AGENTROW" == *Explore* ]]'
sl '{"session_id":"a","context_window":{"total_input_tokens":170000,"context_window_size":200000,"used_percentage":86}}' >/dev/null

# these arrive in the payload, so rendering them costs no git subprocess.
# Driven WITHOUT $TMUX_PANE and with a cwd no pane holds: inside a kel window
# the `kel <group>/<name>` label owns that slot.
IDROW="$(printf '%s' '{"session_id":"zz","workspace":{"current_dir":"/nonexistent-kel-test","repo":{"owner":"tharpep","name":"kelsier"}},"worktree":{"name":"wt1"},"context_window":{"context_window_size":200000,"used_percentage":5}}' | "$KEL" statusline)"
ok "repo identity renders from the payload"         '[[ "$IDROW" == *tharpep/kelsier@wt1* ]]'
# inside a kel window the row is the session only (spec.md §7a): no repo
# identity, no kel label (both "/"-shaped), no quota — those are the
# sidebar's and the bar's
INROW="$(sl "{\"session_id\":\"a\",\"workspace\":{\"repo\":{\"owner\":\"tharpep\",\"name\":\"kelsier\"}},\"context_window\":{\"context_window_size\":200000,\"used_percentage\":5}}")"
ok "  ...but inside kel the row is the session only" '[[ "$INROW" != */* ]] && [[ "$INROW" == *"5%"* ]]'
ok "absolute tokens appear once past the warn"      '[[ "$(sl "{\"session_id\":\"a\",\"context_window\":{\"total_input_tokens\":847000,\"context_window_size\":1000000,\"used_percentage\":85}}")" == *847k/1M* ]]'
ok "  ...and not below it"                          '[[ "$(sl "{\"session_id\":\"a\",\"context_window\":{\"total_input_tokens\":20000,\"context_window_size\":1000000,\"used_percentage\":2}}")" != *20k/1M* ]]'
# 5400s sits mid-hour, so the hour digit cannot flip while the test runs. An
# exact minute count would race the clock; only the shape is asserted.
RESET_AT=$(( $(date +%s) + 5400 ))
# the quota chips live in the row only outside kel, where there is no bar:
# no $TMUX_PANE and a cwd no pane holds, as for the identity above
slout() { printf '%s' "$1" | env -u TMUX -u TMUX_PANE "$KEL" statusline; }
QP="{\"session_id\":\"zz\",\"workspace\":{\"current_dir\":\"/nonexistent-kel-test\"},\"context_window\":{\"context_window_size\":200000,\"used_percentage\":5},\"rate_limits\":{\"five_hour\":{\"used_percentage\":74,\"resets_at\":$RESET_AT}}}"
QROW="$(slout "$QP")"
ok "outside kel, a quota past 50% names its reset"  '[[ "$QROW" == *"5h 74% (1h"* ]]'
# the 5-hour window shows at any figure — the smallest limit is the one to
# watch; the week still waits for 50%
ok "  ...the 5h one at any figure"                  '[[ "$(slout "{\"session_id\":\"zz\",\"workspace\":{\"current_dir\":\"/nonexistent-kel-test\"},\"context_window\":{\"context_window_size\":200000,\"used_percentage\":5},\"rate_limits\":{\"five_hour\":{\"used_percentage\":31}}}")" == *"5h 31%"* ]]'
ok "  ...and the week below 75% stays hidden"       '[[ "$(slout "{\"session_id\":\"zz\",\"workspace\":{\"current_dir\":\"/nonexistent-kel-test\"},\"context_window\":{\"context_window_size\":200000,\"used_percentage\":5},\"rate_limits\":{\"seven_day\":{\"used_percentage\":60}}}")" != *7d* ]]'
ok "  ...shown from 75%"                            '[[ "$(slout "{\"session_id\":\"zz\",\"workspace\":{\"current_dir\":\"/nonexistent-kel-test\"},\"context_window\":{\"context_window_size\":200000,\"used_percentage\":5},\"rate_limits\":{\"seven_day\":{\"used_percentage\":80}}}")" == *"7d 80%"* ]]'
ok "a reset already in the past is omitted"         '[[ "$(slout "{\"session_id\":\"zz\",\"workspace\":{\"current_dir\":\"/nonexistent-kel-test\"},\"context_window\":{\"context_window_size\":200000,\"used_percentage\":5},\"rate_limits\":{\"five_hour\":{\"used_percentage\":74,\"resets_at\":1}}}")" == *"5h 74%"* ]]'
ok "inside kel the row carries no quota"            '[[ "$(sl "$QP")" != *5h* ]]'
# the account file the bar reads: written from a payload with rate limits,
# never from one without (an API-key session has none — no fake 0%)
ok "a payload with rate limits writes the quota file" '[ "$(cut -d" " -f1 "$STATE/quota")" = 74 ]'
rm -f "$STATE/quota"
sl '{"session_id":"a","context_window":{"context_window_size":200000,"used_percentage":5}}' >/dev/null
ok "  ...and one without writes none"               '[ ! -f "$STATE/quota" ]'

# Issue #3: quota_cmd fallback when Claude Code statusline carries no spend_limit
export KEL_QUOTA_CMD="echo 42 0"
export KEL_QUOTA_TTL=60
rm -f "$STATE/quota" "$STATE/quota-cmd" "$STATE/quota-cmd.at"
"$KEL" _fleet >/dev/null 2>&1
wait_until '[ -f "$STATE/quota-cmd" ]' 30
ok "quota_cmd populates spend percentage fallback (#3)" '[ "$("$KEL" _fleet | jq -r .quota.spend.pct)" = 42 ]'

# quota_cmd that fails leaves .quota.spend null
export KEL_QUOTA_CMD="exit 1"
rm -f "$STATE/quota" "$STATE/quota-cmd" "$STATE/quota-cmd.at"
"$KEL" _fleet >/dev/null 2>&1
sleep 0.2
ok "failing quota_cmd leaves .quota.spend null" '[ "$("$KEL" _fleet | jq -r .quota.spend)" = null ]'

# pr = off suppresses quota_cmd
MARKER="$WORK/quota-ran"
export KEL_QUOTA_CMD="touch $MARKER && echo 42 0"
export KEL_PR=off
rm -f "$STATE/quota" "$STATE/quota-cmd" "$STATE/quota-cmd.at" "$MARKER"
"$KEL" _fleet >/dev/null 2>&1
sleep 0.2
ok "pr = off stops quota_cmd from running" '[ ! -f "$MARKER" ]'
unset KEL_QUOTA_CMD KEL_QUOTA_TTL KEL_PR
ok "non-default modes render"                       '[[ "$(sl "{\"session_id\":\"a\",\"fast_mode\":true,\"effort\":{\"level\":\"xhigh\"},\"context_window\":{\"context_window_size\":200000,\"used_percentage\":5}}")" == *"fast xhigh"* ]]'
ok "  ...and default ones do not"                   '[[ "$(sl "{\"session_id\":\"a\",\"effort\":{\"level\":\"medium\"},\"thinking\":{\"enabled\":true},\"context_window\":{\"context_window_size\":200000,\"used_percentage\":5}}")" != *medium* ]]'

section "a selection reaches the system clipboard, not just tmux's buffer"
# tmux decides a terminal takes OSC 52 from TERM, and xterm-256color matches
# its xterm*:clipboard feature — but Apple Terminal sets that TERM and does
# not implement it, so `set-clipboard external` dropped every selection.
# KEL_COPY_CMD is the seam: no real clipboard is touched here.
CLIP="$WORK/clipboard"
printf 'selected-text' | KEL_COPY_CMD="cat > '$CLIP'" "$KEL" _copy
ok "_copy sends stdin to the clipboard command" '[ "$(cat "$CLIP" 2>/dev/null)" = selected-text ]'
printf 'x' | KEL_COPY_CMD="exit 0" "$KEL" _copy
ok "  ...and consumes stdin when it writes nowhere" "printf x | KEL_COPY_CMD='exit 0' '$KEL' _copy"
# the binding, in both tables: mode-keys follows $EDITOR, so either may be live
for t in copy-mode copy-mode-vi; do
  ok "$t drag-end copies through kel" \
     "grep -q 'bind -T $t *MouseDragEnd1Pane send -X copy-pipe-and-cancel' '$HERE/../tmux/kel.conf'"
done
ok "enter copies too (Ctrl-b [ , select, enter)" \
   "grep -q 'bind -T copy-mode *Enter send -X copy-pipe-and-cancel' '$HERE/../tmux/kel.conf'"
ok "a double-click outside copy mode copies through kel" \
   "grep -q '^bind -n DoubleClick1Pane.*kel. _copy' '$HERE/../tmux/kel.conf'"
# the wheel down once ran `copy-mode -e` at the live view: it entered copy
# mode without moving, so the pane stopped updating while looking unchanged
ok "the wheel down never enters copy mode" \
   "grep '^bind -n WheelDownPane' '$HERE/../tmux/kel.conf' | grep -vq copy-mode"
ok "  ...and the wheel up scrolls as it enters" \
   "grep '^bind -n WheelUpPane' '$HERE/../tmux/kel.conf' | grep -q 'copy-mode -e.*scroll-up'"

# ---------------------------------------------------------------- summary
printf '\n'
if [ "$fail" -eq 0 ]; then
  printf '  \033[32m%d passed\033[0m\n' "$pass"
else
  printf '  \033[32m%d passed\033[0m, \033[31m%d failed\033[0m\n' "$pass" "$fail"
  printf '    - %s\n' "${failed[@]}"
fi
exit "$fail"

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
# Requires: tmux, jq, git.  ~40s.
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
KEL="$HERE/../bin/kel"
[ -x "$KEL" ] || { echo "no executable bin/kel next to $HERE" >&2; exit 1; }

VERBOSE=0; [ "${1:-}" = "-v" ] && VERBOSE=1

WORK="$(mktemp -d "${TMPDIR:-/tmp}/kel-test.XXXXXX")"
export XDG_STATE_HOME="$WORK/state"
export KEL_SESSION=keltest

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
STATE="$XDG_STATE_HOME/kel"
SESSIONS="$STATE/sessions"

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
grps() { tmux list-sessions -F '#{session_name}' 2>/dev/null | grep -c keltest; }
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
pane_of()   { tmux list-panes -s -t "=keltest/$1" -F '#{pane_id} #{window_name}'  | awk -v n="$2" '$2==n {print $1; exit}'; }
window_of() { tmux list-windows  -t "=keltest/$1" -F '#{window_id} #{window_name}' | awk -v n="$2" '$2==n {print $1; exit}'; }

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
K api-gw     new docs --agent 'sleep 9999' >/dev/null 2>&1
K coppermind new docs --agent 'sleep 9999' >/dev/null 2>&1
ok "the same name works in two repos"   '[ "$(wins)" = 2 ]'
ok "one record per group"               '[ -f "$SESSIONS/api-gw/docs.json" ] && [ -f "$SESSIONS/coppermind/docs.json" ]'
ok "ls shows both"                      '[ "$("$KEL" ls 2>/dev/null | grep -c "  docs")" = 2 ]'

section "ambiguity is reported, never guessed"
out="$("$KEL" kill docs 2>&1)"
ok "a bare ambiguous kill refuses"      '[[ "$out" == *ambiguous* ]]'
ok "  ...and names the candidates"      '[[ "$out" == *api-gw/docs* && "$out" == *coppermind/docs* ]]'
"$KEL" kill coppermind/docs >/dev/null 2>&1
ok "group/name kills exactly one"       '[ "$(wins)" = 1 ]'
ok "  ...and it is the right one"       '[ -f "$SESSIONS/api-gw/docs.json" ] && [ ! -f "$SESSIONS/coppermind/docs.json" ]'
ok "a now-unique bare name works"       '"$KEL" kill docs >/dev/null 2>&1; [ ! -f "$SESSIONS/api-gw/docs.json" ]'

section "rename and move stay inside their group"
reset
K api-gw     new alpha --agent 'sleep 9999' >/dev/null 2>&1
K coppermind new alpha --agent 'sleep 9999' >/dev/null 2>&1
w="$(tmux list-windows -a -F '#{session_name} #{window_id}' | awk '$1=="keltest/api-gw"{print $2}')"
"$KEL" _board_rename "$w" beta >/dev/null 2>&1
ok "rename moves the record"            '[ -f "$SESSIONS/api-gw/beta.json" ] && [ ! -f "$SESSIONS/api-gw/alpha.json" ]'
ok "  ...and leaves the other group"    '[ -f "$SESSIONS/coppermind/alpha.json" ]'
ok "  ...and rewrites .name"            '[ "$(jq -r .name "$SESSIONS/api-gw/beta.json")" = beta ]'

section "legacy flat records migrate"
reset
K api-gw new legacy --agent 'sleep 9999' >/dev/null 2>&1
mv "$SESSIONS/api-gw/legacy.json" "$SESSIONS/legacy.json"; rmdir "$SESSIONS/api-gw"
jq 'del(.group)' "$SESSIONS/legacy.json" > "$WORK/l" && mv "$WORK/l" "$SESSIONS/legacy.json"
"$KEL" ls >/dev/null 2>&1
ok "a flat pre-v0.2 record is filed"    '[ -f "$SESSIONS/api-gw/legacy.json" ]'
ok "  ...with a group derived from repo" '[ "$(jq -r .group "$SESSIONS/api-gw/legacy.json")" = api-gw ]'

# ---------------------------------------------------------------- restore
section "restore rebuilds the workspace and keeps the snapshot"
reset
K api-gw     new auth-fix --agent 'sleep 9999' >/dev/null 2>&1
K api-gw     new tests    --agent 'sleep 9999' >/dev/null 2>&1
K coppermind new sazed    --agent 'sleep 9999' >/dev/null 2>&1
tmux split-window -d -t "$(window_of api-gw auth-fix)" -c "$WORK/repos/api-gw" 'sleep 9999'
wait_until '[ "$(tmux list-panes -t "$(window_of api-gw auth-fix)" -F x 2>/dev/null | grep -c x)" = 2 ]'
"$KEL" snapshot >/dev/null 2>&1
want="$(jq -Sc '.groups|map_values(map(.name))' "$STATE/snapshot.json")"
for i in 1 2 3; do
  tmux kill-server 2>/dev/null; sleep 0.6
  "$KEL" restore -s >/dev/null 2>&1
  wait_until '[ "$(wins)" = 3 ] && [ "$(grps)" = 2 ]'
  got="$(jq -Sc '.groups|map_values(map(.name))' "$STATE/snapshot.json")"
  ok "restore $i rebuilds 3 windows / 2 groups" "[ '$(wins)' = 3 ] && [ '$(grps)' = 2 ]"
  ok "restore $i leaves the snapshot intact"    "[ '$got' = '$want' ]"
  ok "restore $i rebuilds the split pane"       '[ "$(tmux list-panes -t "$(window_of api-gw auth-fix)" -F x 2>/dev/null | grep -c x)" = 2 ]'
done
ok "no stale .restoring lock is left"   '[ ! -e "$STATE/.restoring" ]'
ok "a .prev generation is kept"         '[ -f "$STATE/snapshot.json.prev" ]'

section "portability fixes keep behaving (no stat/sed/sort dependencies)"
reset
K api-gw new a --agent 'sleep 9999' >/dev/null 2>&1
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
ok "  ...and the window survives"       '[ "$(wins)" = 1 ]'
ok "  ...and --force gets through"      '"$KEL" kill api-gw/wt -f >/dev/null 2>&1; [ "$(wins)" = 0 ]'

# ---------------------------------------------------------------- state
section "Notification maps to what it actually means (#14)"
reset
K api-gw new a --agent 'sleep 9999' >/dev/null 2>&1
P="$(pane_of api-gw a)"
hook "$P" UserPromptSubmit
ok "UserPromptSubmit -> working"                    '[ "$(state_of a)" = working ]'
hook "$P" Notification permission_prompt "needs Bash"
ok "permission_prompt -> waiting"                   '[ "$(state_of a)" = waiting ]'
ok "  ...and the reason is kept"                    '[[ "$("$KEL" _board_preview a api-gw)" == *"needs Bash"* ]]'
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
K api-gw new a --agent 'sleep 9999' >/dev/null 2>&1
P="$(pane_of api-gw a)"
W="$(tmux display-message -p -t "$P" '#{window_id}')"
hook "$P" UserPromptSubmit
tmux send-keys -t "$P" C-c 2>/dev/null
wait_until '[ "$(state_of a)" = dead ]'
ok "a crashed agent reads dead, not working"        '[ "$(state_of a)" = dead ]'

section "compaction counter (#15)"
reset
K api-gw new a --agent 'sleep 9999' >/dev/null 2>&1
P="$(pane_of api-gw a)"
M="$SESSIONS/api-gw/a.json"
hook "$P" UserPromptSubmit
hook "$P" PreCompact
ok "PreCompact starts the count at 1"       '[ "$(jq -r ".compactions // 0" "$M")" = 1 ]'
ok "  ...and leaves state alone (mid-turn)" '[ "$(state_of a)" = working ]'
hook "$P" PreCompact; hook "$P" PreCompact
ok "  ...and accumulates"                   '[ "$(jq -r .compactions "$M")" = 3 ]'
ok "PostCompact does not double-count"      'hook "$P" PostCompact; [ "$(jq -r .compactions "$M")" = 3 ]'
ok "--json exposes it"                      '[ "$("$KEL" ls --json | jq -r ".[0].compactions")" = 3 ]'
ok "the board preview shows it"             '[[ "$("$KEL" _board_preview a api-gw)" == *"compacted  3"* ]]'

section "restart-in-place (#13)"
reset
K api-gw new a --agent 'sleep 9999' >/dev/null 2>&1
W0="$(window_of api-gw a)"
M="$SESSIONS/api-gw/a.json"
P="$(tmux list-panes -t "$W0" -F '#{pane_id}')"
hook "$P" UserPromptSubmit
before="$(jq -Sc 'del(.compactions)' "$M")"
out="$("$KEL" restart api-gw/a 2>&1)"
ok "refuses while a process is alive"       '[[ "$out" == *"live process"* ]]'
ok "  ...even though state says working"    '[ "$(state_of a)" = working ]'
tmux send-keys -t "$P" C-c 2>/dev/null
wait_until '[ "$(state_of a)" = dead ]'
ok "  ...the agent now reads dead"          '[ "$(state_of a)" = dead ]'
"$KEL" restart api-gw/a >/dev/null 2>&1
wait_until '[ "$(wins)" = 1 ]'
ok "restart reuses the SAME window"         '[ "$(tmux list-windows -a -F "#{window_id}" | head -1)" = "$W0" ]'
ok "  ...creates no extra window"           '[ "$(wins)" = 1 ]'
ok "  ...creates no worktree"               '[ ! -d "$WORK/repos/.kel-worktrees" ]'
ok "  ...leaves the record otherwise as-is" '[ "$(jq -Sc "del(.compactions)" "$M")" = "$before" ]'
ok "an unknown name is refused"             '! "$KEL" restart nope-not-here >/dev/null 2>&1'

section "fleet notifications (#1)"
reset
K api-gw new a --agent 'sleep 9999' >/dev/null 2>&1
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

section "per-group waiting badge (#4)"
reset
K api-gw     new a --agent 'sleep 9999' >/dev/null 2>&1
K coppermind new b --agent 'sleep 9999' >/dev/null 2>&1
for g in api-gw coppermind; do
  hook "$(tmux list-panes -t "=keltest/$g" -F '#{pane_id}' | head -1)" Notification permission_prompt "x"
done
export TMUX="$TMUX_TMPDIR/tmux-$(id -u)/default,$(tmux display-message -p '#{pid}'),0"
bar="$("$KEL" status-line api-gw)"
ok "the badge names the other group"        '[[ "$bar" == *"coppermind·1"* ]]'
ok "  ...and not the one you are in"        '[[ "$bar" != *"api-gw·1"* ]]'
ok "  ...and not the old +N form"           '[[ "$bar" != *"+1 waiting"* ]]'
unset TMUX

# ---------------------------------------------------------------- statusline
section "statusline records context without a live agent"
reset
K api-gw new a --agent 'sleep 9999' >/dev/null 2>&1
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
ok "--json surfaces it"                             '[ "$("$KEL" ls --json | jq -r ".[0].context_pct")" = 42 ]'
# the IFS-tab-folding regression: an empty field must not shift every later value
sl '{"session_id":"a","cost":{"total_cost_usd":9.25},
     "context_window":{"total_input_tokens":170000,"context_window_size":200000,"used_percentage":86}}' >/dev/null
ok "an empty field does not shift the others"       '[ "$(cut -f1 "$STATE/$W.ctx")" = 86 ] && [ "$(cut -f2 "$STATE/$W.ctx")" = 9.25 ]'
# a malformed payload must never blank a good record
for bad in '' 'not json' '{}' '{"context_window":null}'; do sl "$bad" >/dev/null 2>&1; done
ok "garbage payloads cannot blank a good record"    '[ "$(cut -f1 "$STATE/$W.ctx")" = 86 ]'

# ---------------------------------------------------------------- summary
printf '\n'
if [ "$fail" -eq 0 ]; then
  printf '  \033[32m%d passed\033[0m\n' "$pass"
else
  printf '  \033[32m%d passed\033[0m, \033[31m%d failed\033[0m\n' "$pass" "$fail"
  printf '    - %s\n' "${failed[@]}"
fi
exit "$fail"

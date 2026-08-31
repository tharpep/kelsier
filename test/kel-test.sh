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
# `kel new` starts the agent with send-keys, so for a moment the pane is still
# a bare shell and effective_state correctly calls that `dead`.  Wait for the
# process before asserting on state.  v0.6 made the fleet read 3x faster, which
# is what first lost this race.
agent_up() { wait_until "tmux list-panes -t '$1' -F '#{pane_current_command}' 2>/dev/null | grep -qvxE 'bash|zsh|fish|sh|dash|tmux'"; }
NEW() { K "$1" new "$2" --agent 'sleep 9999' >/dev/null 2>&1; agent_up "$(window_of "$1" "$2")"; }
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
NEW api-gw docs
NEW coppermind docs
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
NEW api-gw alpha
NEW coppermind alpha
w="$(tmux list-windows -a -F '#{session_name} #{window_id}' | awk '$1=="keltest/api-gw"{print $2}')"
"$KEL" _board_rename "$w" beta >/dev/null 2>&1
ok "rename moves the record"            '[ -f "$SESSIONS/api-gw/beta.json" ] && [ ! -f "$SESSIONS/api-gw/alpha.json" ]'
ok "  ...and leaves the other group"    '[ -f "$SESSIONS/coppermind/alpha.json" ]'
ok "  ...and rewrites .name"            '[ "$(jq -r .name "$SESSIONS/api-gw/beta.json")" = beta ]'

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
ok "the board agrees too"                '[[ "$("$KEL" _board_rows | sed "s/\x1b\[[0-9;]*m//g")" == *"api-gw	a	waiting"* ]]'
# a record whose window is gone must still appear, flagged, not vanish
"$KEL" kill coppermind/b >/dev/null 2>&1
NEW api-gw c
mv "$SESSIONS/api-gw/c.json" "$SESSIONS/api-gw/ghost.json"
jq '.name="ghost"' "$SESSIONS/api-gw/ghost.json" > "$WORK/g" && mv "$WORK/g" "$SESSIONS/api-gw/ghost.json"
ok "an orphaned record shows as dead"    '[ "$("$KEL" _fleet | jq -r ".agents[]|select(.name==\"ghost\")|.state")" = dead ]'
ok "  ...with a null window_id"          '[ "$("$KEL" _fleet | jq -r ".agents[]|select(.name==\"ghost\")|.window_id")" = null ]'
# empty fleet must be a document, not an error
reset
ok "an empty fleet is {agents:[]}"       '[ "$("$KEL" _fleet | jq -c ".agents")" = "[]" ]'
ok "  ...and still exits 0"              '"$KEL" _fleet >/dev/null 2>&1'

section "the Go kel-fleet agrees with the bash one (v0.6 seam)"
GOBIN_FLEET="$WORK/kel-fleet"
if command -v go >/dev/null 2>&1 && (cd "$HERE/.." && go build -o "$GOBIN_FLEET" ./cmd/kel-fleet) 2>/dev/null; then
  reset
  NEW api-gw auth-fix
  NEW api-gw wt
  NEW coppermind sazed
  P="$(pane_of api-gw auth-fix)"
  hook "$P" Notification permission_prompt "needs Bash now"
  hook "$P" PreCompact
  printf '%s' '{"model":{"display_name":"Opus 5"},"cost":{"total_cost_usd":18.7},"context_window":{"total_input_tokens":178000,"context_window_size":200000,"used_percentage":91},"rate_limits":{"five_hour":{"used_percentage":74}}}' \
    | TMUX_PANE="$P" "$KEL" statusline >/dev/null
  echo wip > "$WORK/repos/api-gw/dirt.txt"
  tmux new-window -d -t '=keltest/api-gw' -n stray 2>/dev/null      # unmanaged
  cp "$SESSIONS/coppermind/sazed.json" "$SESSIONS/coppermind/ghost.json"
  jq '.name="ghost"' "$SESSIONS/coppermind/ghost.json" > "$WORK/g" && mv "$WORK/g" "$SESSIONS/coppermind/ghost.json"
  # Warm every index first. The first `git status` in a fresh worktree
  # refreshes .git/index, so whichever implementation runs first can legitimately
  # see a different count from the second — a real source of nondeterminism that
  # is not a kel bug and would otherwise show up as a flaky differential.
  for d in $("$KEL" _fleet | jq -r '.agents[].cwd // empty' | sort -u); do
    git -C "$d" status --porcelain >/dev/null 2>&1 || true
  done
  norm() { jq -S 'del(.generated_at)'; }
  for flag in "" "--dirty" "--land"; do
    "$KEL" _fleet $flag | norm > "$WORK/bash.json"
    KEL_FLEET_BIN=/nonexistent "$KEL" _fleet $flag | norm > "$WORK/bash2.json"
    "$GOBIN_FLEET" $flag       | norm > "$WORK/go.json"
    ok "bash and Go agree${flag:+ ($flag)}"  '[ -z "$(diff "$WORK/bash2.json" "$WORK/go.json")" ]'
    [ -n "$(diff "$WORK/bash2.json" "$WORK/go.json")" ] && diff "$WORK/bash2.json" "$WORK/go.json" | head -14 | sed "s/^/       /"
  done
  # regenerate without flags: the loop above left go.json holding --dirty output
  "$GOBIN_FLEET" | norm > "$WORK/go-plain.json"
  KEL_FLEET_BIN=/nonexistent "$KEL" _fleet | norm > "$WORK/bash-plain.json"
  ok "the seam prefers the Go binary"  'KEL_FLEET_BIN="$GOBIN_FLEET" "$KEL" _fleet | norm | diff -q - "$WORK/go-plain.json" >/dev/null'
  ok "  ...and falls back without it"  'KEL_FLEET_BIN=/nonexistent "$KEL" _fleet | jq -e ".agents|length > 0" >/dev/null'
  ok "  ...identically either way"     '[ -z "$(diff "$WORK/bash-plain.json" "$WORK/go-plain.json")" ]'
else
  echo "  skip  (no Go toolchain — bash implementation is the only one here)"
fi

section "kel top renders and sorts (#2)"
GOBIN_TOP="$WORK/kel-top"
if command -v go >/dev/null 2>&1 && (cd "$HERE/.." && go build -o "$GOBIN_TOP" ./cmd/kel-top) 2>/dev/null; then
  reset
  NEW api-gw idler
  NEW api-gw blocked
  NEW coppermind busy
  hook "$(pane_of api-gw blocked)"   Notification permission_prompt "needs Bash"
  hook "$(pane_of coppermind busy)"  UserPromptSubmit
  printf '%s' '{"context_window":{"used_percentage":93,"context_window_size":200000,"total_input_tokens":9},"cost":{"total_cost_usd":18.7}}' \
    | TMUX_PANE="$(pane_of coppermind busy)" "$KEL" statusline >/dev/null
  frame() { COLUMNS="${1:-120}" "$GOBIN_TOP" --once 2>/dev/null | sed 's/\x1b\[[0-9;]*m//g'; }
  ok "renders a header"                    '[[ "$(frame)" == *"GROUP"*"AGENT"*"STATE"* ]]'
  ok "lists every agent"                   '[ "$(frame | grep -cE "^.? *(api-gw|coppermind) ")" = 3 ]'
  ok "triage puts waiting first"           '[[ "$(frame | sed -n 2p)" == *blocked*waiting* ]]'
  ok "  ...working above idle"             '[ "$(frame | grep -n busy | cut -d: -f1)" -lt "$(frame | grep -n idler | cut -d: -f1)" ]'
  ok "shows context and cost"              '[[ "$(frame)" == *"93%"* && "$(frame)" == *"18.70"* ]]'
  ok "draws the key hints"                 '[[ "$(frame)" == *"j/k scroll"*"q quit"* ]]'
  ok "80 cols still fits the table"        '[ "$(frame 80 | head -1 | wc -L)" -le 80 ]'
  ok "50 cols drops columns, not rows"     '[ "$(frame 50 | grep -cE "^.? *(api-gw|coppermind) ")" = 3 ]'
  ok "  ...and stays within 50"            '[ "$(frame 50 | wc -L)" -le 50 ]'
  ok "no trailing whitespace on any row"   '! frame | grep -q " $"'
else
  echo "  skip  (no Go toolchain)"
fi

section "land: one verb per row, degrading without gh (#6)"
reset
NEW api-gw solo
L() { "$KEL" _land "$1" "${2:-}" | tr '\t' '/'; }
ok "a repo with no remote is clean"      '[ "$(L "$WORK/repos/api-gw")" = "clean/" ]'
echo wip > "$WORK/repos/api-gw/wip.txt"
ok "uncommitted work -> dirty N"         '[ "$(L "$WORK/repos/api-gw")" = "dirty/1" ]'
( cd "$WORK/repos/api-gw" && git add -A && git -c user.email=t@t -c user.name=t commit -qm w ) >/dev/null 2>&1
ok "committed, no remote -> clean"       '[ "$(L "$WORK/repos/api-gw")" = "clean/" ]'
ok "a non-git directory is clean"        '[ "$(L /tmp)" = "clean/" ]'
ok "a missing directory is clean"        '[ "$(L "$WORK/nope")" = "clean/" ]'
# a remote makes unpushed meaningful
git -C "$WORK/repos/coppermind" remote add origin "$WORK/repos/api-gw" 2>/dev/null
echo z > "$WORK/repos/coppermind/z.txt"
( cd "$WORK/repos/coppermind" && git add -A && git -c user.email=t@t -c user.name=t commit -qm z ) >/dev/null 2>&1
ok "commits no remote has -> unpushed N" '[[ "$(L "$WORK/repos/coppermind")" == unpushed/* ]]'
ok "a non-GitHub remote is never unknown" '[[ "$(L "$WORK/repos/coppermind")" != unknown/* ]]'
ok "--land fills the document"           '[ "$("$KEL" _fleet --land | jq -r ".agents[0].land.code")" != null ]'
ok "  ...and plain _fleet leaves it null" '[ "$("$KEL" _fleet | jq -r ".agents[0].land")" = null ]'
# Prove it needs no network by taking gh away, rather than by timing it:
# `timeout` is GNU coreutils and absent on macOS — caught by CI, which is the
# whole reason the macOS runner exists.
mkdir -p "$WORK/nogh"; printf '#!/bin/sh\nexit 127\n' > "$WORK/nogh/gh"; chmod +x "$WORK/nogh/gh"
ok "  ...and needs no gh at all"         '[ "$(PATH="$WORK/nogh:$PATH" "$KEL" _fleet --land | jq -Sc ".agents[].land")" = "$("$KEL" _fleet --land | jq -Sc ".agents[].land")" ]'
# bin/kel updates on pull; kel-fleet only on install.sh, so a stale binary that
# rejects a new flag is a normal transient state and must not break the command
printf '#!/bin/sh\nexit 2\n' > "$WORK/stale"; chmod +x "$WORK/stale"
ok "a broken kel-fleet falls back to bash" 'KEL_FLEET_BIN="$WORK/stale" "$KEL" _fleet --land | jq -e ".agents" >/dev/null'

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

# B — the board lists panes, not only agents
tmux split-window -d -t "$W1" -c "$WORK" 'sleep 9999'
tmux new-window  -d -t '=keltest/api-gw' -n plainwin -c "$WORK"
wait_until '[ "$(tmux list-panes -a -F x | grep -c x)" -ge 3 ]'
rows() { "$KEL" _board_rows | sed 's/\x1b\[[0-9;]*m//g'; }
ok "the board still lists the agent"     '[ -n "$(rows | awk -F"\t" "\$2==\"agent1\"")" ]'
ok "  ...and now its other pane too"     '[ -n "$(rows | awk -F"\t" "\$3==\"pane\"")" ]'
ok "  ...targeted by pane id"            '[[ "$(rows | awk -F"\t" "\$3==\"pane\"{print \$6; exit}")" == %* ]]'
ok "an agent is never listed twice"      '[ "$(rows | awk -F"\t" "\$2==\"agent1\"" | grep -c .)" = 1 ]'
[ "$(rows | awk -F"\t" '$2=="agent1"' | grep -c .)" = 1 ] || rows | sed "s/^/       /"
ok "a plain window appears as well"      '[ -n "$(rows | grep plainwin)" ]'

# B' — worktree reachable from a menu, and the stranded commands guarded
ok "kel new -w still makes a worktree"   'NEW_W() { ( cd "$WORK/repos/api-gw" && "$KEL" new wt1 -w --agent "sleep 9999" ) >/dev/null 2>&1; }; NEW_W; [ "$(jq -r .isolation "$SESSIONS/api-gw/wt1.json")" = worktree ]'
ok "_run refuses anything not allowed"   '! "$KEL" _run kill >/dev/null 2>&1'
ok "_run doctor runs"                    'KEL_IN_POPUP=1 "$KEL" _run doctor >/dev/null 2>&1'
ok "_sweepui needs an explicit yes"      'printf "n\n" | KEL_IN_POPUP=1 "$KEL" _sweepui 2>&1 | grep -q "nothing swept"'

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
# an empty fleet must still answer --json with JSON, not an English sentence
ok "--json is JSON with an empty fleet"  'E="$(mktemp -d "$WORK/empty.XXXX")"; XDG_STATE_HOME="$E" KEL_SESSION=nosuchprefix "$KEL" ls --json | jq -e ".agents | length == 0" >/dev/null'

# F — kel's chosen name is handed to Claude Code
( cd "$WORK/repos/coppermind" && KEL_AGENT=claude "$KEL" new named --no-agent ) >/dev/null 2>&1
ok "claude is launched with --name"      '[ "$(jq -r .agent "$SESSIONS/coppermind/named.json")" = "claude --name named" ]'
( cd "$WORK/repos/coppermind" && "$KEL" new other --agent 'sleep 9999' ) >/dev/null 2>&1
ok "  ...and other agents are untouched" '[ "$(jq -r .agent "$SESSIONS/coppermind/other.json")" = "sleep 9999" ]'

# ---------------------------------------------------------------- restore
section "restore rebuilds the workspace and keeps the snapshot"
reset
NEW api-gw auth-fix
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
ok "  ...and the window survives"       '[ "$(wins)" = 1 ]'
ok "  ...and --force gets through"      '"$KEL" kill api-gw/wt -f >/dev/null 2>&1; [ "$(wins)" = 0 ]'

# ---------------------------------------------------------------- state
section "Notification maps to what it actually means (#14)"
reset
NEW api-gw a
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
ok "the board preview shows it"             '[[ "$("$KEL" _board_preview a api-gw)" == *"compacted  3"* ]]'

section "restart-in-place (#13)"
reset
NEW api-gw a
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

section "per-group waiting badge (#4)"
reset
NEW api-gw a
NEW coppermind b
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

# ---------------------------------------------------------------- summary
printf '\n'
if [ "$fail" -eq 0 ]; then
  printf '  \033[32m%d passed\033[0m\n' "$pass"
else
  printf '  \033[32m%d passed\033[0m, \033[31m%d failed\033[0m\n' "$pass" "$fail"
  printf '    - %s\n' "${failed[@]}"
fi
exit "$fail"

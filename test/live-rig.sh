#!/usr/bin/env bash
# A live rig for seeing what a person sees: popups, the board, a real client.
# The suite cannot draw these (its clients are control-mode), so this is a
# manual tool, never a CI step.
#
# Two tmux servers. The OUTER (no config, status off) has one pane, a real
# terminal client attached to the INNER, which loads tmux/kel.conf unmodified
# from this checkout and holds a fake fleet: six `sleep 9999` agents in two
# repos, states set through `kel hook`, a blocked background session from a
# stubbed `claude agents --json`, a server pane, and worktrees. `claude attach`
# fails as it does with no auth.
#
#   test/live-rig.sh up            build from this checkout, start, fake the fleet
#                                  (KEL_RIG_W x KEL_RIG_H, default 160x45)
#   test/live-rig.sh keys <k>...   send keys to the outer pane (what a person types)
#   test/live-rig.sh shot <name>   append the screen (ANSI) to $KEL_RIG_SHOTS with a header line
#   test/live-rig.sh plain         print the screen as text
#   test/live-rig.sh tmux <args>   a command on the inner server
#   test/live-rig.sh down          kill both servers, remove the rig
#
# Isolated as the suite is: both servers on their own sockets under a short
# TMUX_TMPDIR in /tmp, its own XDG_STATE_HOME and XDG_CONFIG_HOME (pr = off),
# KEL_SESSION=kelrig, and a fake HOME whose .local/bin/kel is this checkout's
# bin/kel, first on PATH so an installed kel is never what runs. Every tmux
# call names its socket with -S; never a bare tmux.
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO="$(cd "$HERE/.." && pwd)"
RIG="/tmp/kelrig-$(id -u)"
SHOTS="${KEL_RIG_SHOTS:-$RIG-shots.ans}"
TMUX_REAL="$(command -v tmux)" || { echo "no tmux" >&2; exit 1; }
unset TMUX TMUX_PANE
for v in $(compgen -e | grep "^KEL_" | grep -v "^KEL_RIG_"); do unset "$v"; done
export GOPATH="$(go env GOPATH)" GOCACHE="$(go env GOCACHE)" GOMODCACHE="$(go env GOMODCACHE)"
export TMUX_TMPDIR="$RIG/t"
SOCKDIR="$TMUX_TMPDIR/tmux-$(id -u)"
OUTER="$SOCKDIR/kelrig-outer"
INNER="$SOCKDIR/kelrig-inner"
export HOME="$RIG/home" XDG_STATE_HOME="$RIG/state" XDG_CONFIG_HOME="$RIG/cfg" KEL_SESSION=kelrig \
       KEL_FLEET_BIN="$RIG/bin/kel-fleet" KEL_SIDEBAR_BIN="$RIG/bin/kel-sidebar" KEL_CLAUDE_BIN="$RIG/claude-stub" \
       KEL_NOTIFY_DESKTOP=off KEL_NOTIFY_BELL=off PATH="$RIG/shim:$RIG/home/.local/bin:$PATH"

O() { "$TMUX_REAL" -S "$OUTER" "$@"; }
I() { "$TMUX_REAL" -S "$INNER" "$@"; }

rig_down() {
  [ -S "$OUTER" ] && O kill-server 2>/dev/null
  [ -S "$INNER" ] && I kill-server 2>/dev/null
  case "$RIG" in /tmp/kelrig-*) rm -rf "$RIG" ;; esac
}

pane() { I list-panes -a -F '#{@kel-agent} #{pane_id}' | awk -v a="$1" '$1==a{print $2; exit}'; }
hk() {
  printf '{"session_id":"s-%s","cwd":"%s","hook_event_name":"%s"%s}' "$1" "$RIG" "$2" "${3:-}" \
    | TMUX_PANE="$(pane "$1")" kel hook "$2" >/dev/null 2>&1
}
K() { local r="$1"; shift; ( cd "$RIG/repos/$r" && kel "$@" ) >/dev/null 2>&1; }

rig_up() {
  rig_down
  RIG_OK=0
  trap '[ "$RIG_OK" = 1 ] || rig_down' EXIT
  mkdir -p "$RIG/shim" "$RIG/home/.local/bin" "$RIG/state" "$RIG/cfg/kel" "$RIG/repos" "$RIG/bin"
  mkdir -m 700 -p "$SOCKDIR"
  ( cd "$REPO" && go build -o "$RIG/bin/kel-fleet" ./cmd/kel-fleet && go build -o "$RIG/bin/kel-sidebar" ./cmd/kel-sidebar ) || return 1
  ln -s "$REPO/bin/kel" "$RIG/home/.local/bin/kel"
  touch "$RIG/home/.hushlogin"
  printf '#!/bin/sh\nexec %s -S %s "$@"\n' "$TMUX_REAL" "$INNER" > "$RIG/shim/tmux"
  chmod +x "$RIG/shim/tmux"
  printf '#!/bin/sh\n[ "$1 $2" = "agents --json" ] && echo %s\n' \
    "'[{\"pid\":1,\"id\":\"bg7\",\"cwd\":\"$RIG/repos/coppermind\",\"kind\":\"background\",\"sessionId\":\"bgs\",\"name\":\"nightly\",\"status\":\"idle\",\"state\":\"blocked\",\"startedAt\":1}]'" \
    > "$RIG/claude-stub"
  printf '#!/bin/sh\necho "Error: not logged in. Run claude /login." >&2\nexit 1\n' > "$RIG/shim/claude"
  chmod +x "$RIG/claude-stub" "$RIG/shim/claude"
  printf '#!/bin/sh\necho "$*" >> %s/gh.calls\nexit 1\n' "$RIG" > "$RIG/shim/gh"
  chmod +x "$RIG/shim/gh"
  printf 'pr = off\n' > "$RIG/cfg/kel/config.toml"
  printf 'set -g default-terminal tmux-256color\nset -as terminal-features ",*:RGB"\nsource-file %s/tmux/kel.conf\n' "$REPO" > "$RIG/inner.conf"

  local r
  for r in api-gw coppermind; do
    mkdir -p "$RIG/repos/$r"
    ( cd "$RIG/repos/$r" && git init -q -b main . && echo hi > README.md && git add -A \
      && git -c user.email=t@t -c user.name=t commit -qm init ) >/dev/null 2>&1
  done
  local G="git -C $RIG/repos/api-gw"
  $G remote add origin https://github.com/example/api-gw.git
  $G update-ref refs/remotes/origin/main HEAD
  mkdir -p "$RIG/state/kel/pr-cache"
  printf '[{"number":7,"sha":"%s","merged":false,"state":"open"}]\n' "$($G rev-parse HEAD)" > "$RIG/state/kel/pr-cache/example__api-gw.sha.json"
  echo 1 > "$RIG/state/kel/pr-cache/example__api-gw.sha.json.at"
  local C="git -C $RIG/repos/coppermind"
  $C worktree add -q "$RIG/repos/coppermind-landed" -b landed-fix
  git -C "$RIG/repos/coppermind-landed" -c user.email=t@t -c user.name=t commit -q --allow-empty -m fix
  $C merge -q --ff-only landed-fix
  $C worktree add -q "$RIG/repos/coppermind-wip" -b wip
  echo x > "$RIG/repos/coppermind-wip/wip.txt"

  I -f "$RIG/inner.conf" start-server \; set -g exit-empty off
  # the outer first, so kel new makes the inner session at the client's size
  O -f /dev/null new-session -d -s kv -x "${KEL_RIG_W:-160}" -y "${KEL_RIG_H:-45}" "sleep 99999"
  O set -g default-terminal tmux-256color
  O set -as terminal-features ",*:RGB"
  O set -g status off
  K api-gw new auth --agent 'sleep 9999'
  K api-gw new docs --agent 'sleep 9999'
  K api-gw new tests --agent 'sleep 9999'
  K coppermind new parser --agent 'sleep 9999'
  K coppermind new ui --agent 'sleep 9999'
  K coppermind new old --agent 'sleep 9999'
  sleep 1.5
  hk api-gw/auth UserPromptSubmit
  hk api-gw/docs UserPromptSubmit
  hk api-gw/docs PermissionRequest ',"tool_name":"Bash","tool_input":{"command":"rm -rf build/"}'
  hk api-gw/tests UserPromptSubmit
  hk api-gw/tests Stop
  hk coppermind/parser UserPromptSubmit
  hk coppermind/ui SessionStart
  hk coppermind/old UserPromptSubmit
  # dead: the agent process goes while its state still says working
  pkill -f "sleep 9999" -P "$(I display -p -t "$(pane coppermind/old)" '#{pane_pid}')" 2>/dev/null
  I send-keys -t "$(pane coppermind/old)" C-c
  I split-window -d -h -t "$(pane coppermind/parser)" -c "$RIG/repos/coppermind" "python3 -m http.server 18765 --bind 127.0.0.1"
  I select-window -t "$(I list-panes -a -F '#{@kel-agent} #{window_id}' | awk '$1=="api-gw/auth"{print $2}')"
  O respawn-pane -k -t kv "env -u TMUX TMUX_TMPDIR=$TMUX_TMPDIR TERM=tmux-256color $TMUX_REAL -S $INNER attach -t =kelrig"
  sleep 2
  RIG_OK=1
  echo "rig up: $RIG · shots go to $SHOTS"
}

need_up() { [ -S "$OUTER" ] || { echo "rig is not up: test/live-rig.sh up" >&2; exit 1; }; }

case "${1:-}" in
  up) rig_up ;;
  down) rig_down ;;
  keys) need_up; shift; O send-keys -t kv "$@" ;;
  shot)
    need_up
    sleep "${KEL_RIG_WAIT:-0.8}"
    { printf '\033[0m\n======== %s ========\n' "${2:-shot}"; O capture-pane -e -p -t kv; } >> "$SHOTS" ;;
  plain) need_up; O capture-pane -p -t kv ;;
  tmux) need_up; shift; I "$@" ;;
  *) sed -n '2,22p' "$0" | sed 's/^# \{0,1\}//'; exit 2 ;;
esac

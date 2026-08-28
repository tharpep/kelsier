#!/usr/bin/env bash
# kel — state adapter for Claude Code.
#
# Wired into ~/.claude/settings.json by install.sh, once per hook event:
#   command: "<repo>/hooks/kel-hook.sh <EVENT>"
# Claude Code passes the hook payload as JSON on stdin.
#
# Job: resolve which tmux window this Claude session lives in, then write
# "<state> <epoch>" to ~/.local/state/kel/<window-id>.state and nudge tmux to
# redraw its status line now instead of on the interval.
#
# Keyed by window id (@3), not window name: names collide (tmux auto-names
# every window running `claude` as "claude"). Ids are unique and stable.
#
# Pane resolution, in order:
#   1. $TMUX_PANE in the hook's environment       (ideal — inherited from the pane)
#   2. the value stashed by this session's SessionStart hook
#   3. the single tmux pane whose current path == the payload's cwd
#
# Never fails the hook: any missing context -> exit 0 quietly.
set -uo pipefail

STATE_DIR="${XDG_STATE_HOME:-$HOME/.local/state}/kel"
STASH_DIR="$STATE_DIR/.stash"
mkdir -p "$STATE_DIR" "$STASH_DIR"

payload="$(cat 2>/dev/null || true)"
event="${1:-}"
[ -z "$event" ] && event="$(printf '%s' "$payload" | jq -r '.hook_event_name // empty' 2>/dev/null || true)"
sid="$(printf '%s' "$payload" | jq -r '.session_id // empty' 2>/dev/null || true)"
cwd="$(printf '%s' "$payload" | jq -r '.cwd // empty' 2>/dev/null || true)"

command -v tmux >/dev/null 2>&1 || exit 0

# --- 1. environment ---
pane="${TMUX_PANE:-}"

# --- 2. SessionStart stash ---
if [ -z "$pane" ] && [ -n "$sid" ] && [ -f "$STASH_DIR/$sid" ]; then
  pane="$(cat "$STASH_DIR/$sid" 2>/dev/null || true)"
fi

# --- 3. match a tmux pane by cwd (only if exactly one matches) ---
if [ -z "$pane" ] && [ -n "$cwd" ]; then
  match="$(tmux list-panes -a -F '#{pane_id} #{pane_current_path}' 2>/dev/null \
           | awk -v c="$cwd" '$2 == c {print $1}')"
  [ "$(printf '%s\n' "$match" | grep -c .)" = "1" ] && pane="$match"
fi

# SessionStart: stash whatever pane we resolved, for later hooks that lack $TMUX_PANE.
if [ "$event" = "SessionStart" ] && [ -n "$pane" ] && [ -n "$sid" ]; then
  printf '%s' "$pane" > "$STASH_DIR/$sid"
fi

[ -z "$pane" ] && exit 0

wid="$(tmux display-message -p -t "$pane" '#{window_id}' 2>/dev/null || true)"
[ -z "$wid" ] && exit 0

case "$event" in
  SessionStart)     state="idle" ;;
  UserPromptSubmit) state="working" ;;
  Notification)     state="waiting" ;;
  Stop)             state="done" ;;
  SessionEnd)
    rm -f "$STATE_DIR/$wid.state"
    [ -n "$sid" ] && rm -f "$STASH_DIR/$sid"
    tmux refresh-client -S 2>/dev/null || true
    exit 0 ;;
  *) exit 0 ;;
esac

# atomic write (temp in same dir, then rename)
tmp="$(mktemp "$STATE_DIR/.${wid#@}.XXXXXX" 2>/dev/null)" || exit 0
printf '%s %s\n' "$state" "$(date +%s)" > "$tmp"
mv -f "$tmp" "$STATE_DIR/$wid.state"

tmux refresh-client -S 2>/dev/null || true
exit 0

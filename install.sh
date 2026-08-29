#!/usr/bin/env bash
# kelsier installer.
#
#   - symlinks bin/kel into ~/.local/bin
#   - makes ~/.tmux.conf source tmux/kel.conf
#   - merges the state hooks + the statusLine into ~/.claude/settings.json
#     (jq, non-clobbering; a statusLine kel displaces is kept and chained)
#
# Idempotent. Re-run any time. Upgrades a pre-v0.1 install (loose kel-* scripts
# and hooks/kel-hook.sh) to the single `kel` command. Also installs shell
# completion (bash + zsh).
set -euo pipefail

KEL_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BIN_DIR="$HOME/.local/bin"
STATE_DIR="${XDG_STATE_HOME:-$HOME/.local/state}/kel"
SETTINGS="$HOME/.claude/settings.json"
KEL="$KEL_DIR/bin/kel"

say() { printf '\033[1;33m>>> %s\033[0m\n' "$*"; }

command -v jq   >/dev/null || { echo "kel: jq is required"   >&2; exit 1; }
command -v tmux >/dev/null || { echo "kel: tmux is required" >&2; exit 1; }

say "directories"
mkdir -p "$BIN_DIR" "$STATE_DIR" "$STATE_DIR/sessions" "$STATE_DIR/.stash"

say "command -> $BIN_DIR/kel"
chmod +x "$KEL"
ln -sf "$KEL" "$BIN_DIR/kel"
# remove pre-v0.1 loose scripts
rm -f "$BIN_DIR/kel-status" "$BIN_DIR/kel-jump" "$BIN_DIR/kel-cheat"
echo "  kel  (new kill ls go move rename board restore prune doctor cheat + internals)"

say "shell completion"
# bash: XDG completions dir is loaded lazily by bash-completion >= 2.x
BASH_COMPDIR="${XDG_DATA_HOME:-$HOME/.local/share}/bash-completion/completions"
mkdir -p "$BASH_COMPDIR"
ln -sf "$KEL_DIR/completions/kel.bash" "$BASH_COMPDIR/kel"
echo "  bash -> $BASH_COMPDIR/kel"
# zsh: drop _kel on a site-functions dir and make sure it is on fpath
ZSH_FPATH_DIR="${XDG_DATA_HOME:-$HOME/.local/share}/zsh/site-functions"
mkdir -p "$ZSH_FPATH_DIR"
ln -sf "$KEL_DIR/completions/kel.zsh" "$ZSH_FPATH_DIR/_kel"
if [ -f "$HOME/.zshrc" ]; then
  FPATH_LINE="fpath+=($ZSH_FPATH_DIR)"
  if ! grep -qF "$FPATH_LINE" "$HOME/.zshrc"; then
    printf '\n# kelsier completion\n%s\n' "$FPATH_LINE" >> "$HOME/.zshrc"
    echo "  zsh -> $ZSH_FPATH_DIR/_kel  (added fpath line to ~/.zshrc; run compinit)"
  else
    echo "  zsh -> $ZSH_FPATH_DIR/_kel"
  fi
else
  echo "  zsh -> $ZSH_FPATH_DIR/_kel  (add '$ZSH_FPATH_DIR' to fpath before compinit)"
fi

say "tmux config"
if [ -f "$HOME/.config/tmux/tmux.conf" ]; then
  TMUX_CONF="$HOME/.config/tmux/tmux.conf"
else
  TMUX_CONF="$HOME/.tmux.conf"; touch "$TMUX_CONF"
fi
SRC_LINE="source-file $KEL_DIR/tmux/kel.conf"
if grep -qF "$SRC_LINE" "$TMUX_CONF"; then
  echo "  $TMUX_CONF already sources kel.conf"
else
  printf '\n# kelsier\n%s\n' "$SRC_LINE" >> "$TMUX_CONF"
  echo "  added source-file line to $TMUX_CONF"
fi

say "Claude Code hooks -> $SETTINGS"
mkdir -p "$HOME/.claude"
[ -f "$SETTINGS" ] || echo '{}' > "$SETTINGS"
cp "$SETTINGS" "$SETTINGS.kel-bak.$(date +%s)"
tmp="$(mktemp)"
jq --arg k "$KEL" '
  # drop any prior kel hook entry (old "kel-hook.sh ..." or new "kel hook ...")
  def strip_kel:
    map(.hooks |= map(select((.command // "") | test("kel[ -]hook|kel-hook\\.sh") | not)))
    | map(select((.hooks | length) > 0));
  def wire($evt):
    ((.hooks[$evt] // []) | strip_kel)
    + [{ "hooks": [{ "type": "command", "command": ($k + " hook " + $evt) }] }];
  .hooks = (.hooks // {})
  | .hooks.SessionStart     = wire("SessionStart")
  | .hooks.UserPromptSubmit = wire("UserPromptSubmit")
  | .hooks.Notification     = wire("Notification")
  | .hooks.Stop             = wire("Stop")
  | .hooks.SessionEnd       = wire("SessionEnd")
  | .hooks.PreCompact       = wire("PreCompact")
  | .hooks.PostCompact      = wire("PostCompact")
' "$SETTINGS" > "$tmp" && mv "$tmp" "$SETTINGS"
echo "  wired: SessionStart UserPromptSubmit Notification Stop SessionEnd PreCompact PostCompact"

say "Claude Code statusLine -> $SETTINGS"
# kel's statusLine reports this agent's context window / cost, and records them
# so the tmux bar, `kel ls` and the board can show them without interrupting
# the agent.  If you already had a statusLine, keep it and chain it after ours.
PREV="$(jq -r '.statusLine.command // ""' "$SETTINGS")"
PREV_FILE="$HOME/.claude/statusline-prev"
if [ -n "$PREV" ] && ! printf '%s' "$PREV" | grep -q 'kel statusline'; then
  printf '#!/bin/sh\n# statusLine kel displaced on %s — chained after kel'"'"'s own row.\n%s\n' \
    "$(date -u +%F)" "$PREV" > "$PREV_FILE"
  chmod +x "$PREV_FILE"
  echo "  kept your existing statusLine -> $PREV_FILE (runs after kel's line)"
fi
tmp="$(mktemp)"
jq --arg k "$KEL" '.statusLine = {type:"command", command:($k + " statusline")}' \
  "$SETTINGS" > "$tmp" && mv "$tmp" "$SETTINGS"
echo "  wired: statusLine  (context % / cost per agent)"

say "done"
cat <<EOF

  Reload:
    tmux source-file ~/.tmux.conf   # picks up kel.conf  (kill-server for a clean slate)
    first install only: restart running Claude Code sessions so they load the hooks

  Use:
    kel                 go to (or start) this repo's agent
    kel new <name>      new window + agent   (-w = git worktree, --group G)
    kel ls              list every agent, grouped by repo (with context %)
    Ctrl+Space          the board — find an agent    ·   prefix m   manage this one
    kel kill <name>     close one      ·   kel doctor    check the machine
EOF

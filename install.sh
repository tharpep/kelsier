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

# Every run used to leave one more settings.json.kel-bak.<epoch> behind, never
# reaping any — twenty-one of them on the author's machine.  Keep the newest
# three.  The suffix is a 10-digit epoch, so the glob's lexical order IS
# chronological; no `ls -t`, no `sort -V` (BSD has neither the same way).
backup() {   # file — copy it aside, then trim to the newest three
  cp "$1" "$1.kel-bak.$(date +%s)"
  local all=( "$1".kel-bak.* ) n i
  n=${#all[@]}
  for (( i = 0; i < n - 3; i++ )); do rm -f "${all[$i]}"; done
}

command -v jq   >/dev/null || { echo "kel: jq is required"   >&2; exit 1; }
command -v tmux >/dev/null || { echo "kel: tmux is required" >&2; exit 1; }

# --------------------------------------------------------------------------
# --uninstall  —  put the machine back
# --------------------------------------------------------------------------
# Every line install.sh writes outside this repo has an inverse here. Your
# agents and their records are NOT touched unless you also pass --purge:
# uninstalling the tool should never be able to lose work.
if [ "${1:-}" = "--uninstall" ]; then
  purge=0; [ "${2:-}" = "--purge" ] && purge=1
  say "uninstalling kelsier"

  for f in "$BIN_DIR/kel" "$BIN_DIR/kel-fleet" "$BIN_DIR/kel-top"; do
    [ -e "$f" ] || [ -L "$f" ] || continue
    rm -f "$f" && echo "  removed $f"
  done

  BASH_COMPDIR="${XDG_DATA_HOME:-$HOME/.local/share}/bash-completion/completions"
  ZSH_FPATH_DIR="${XDG_DATA_HOME:-$HOME/.local/share}/zsh/site-functions"
  for f in "$BASH_COMPDIR/kel" "$ZSH_FPATH_DIR/_kel"; do
    [ -e "$f" ] || [ -L "$f" ] || continue
    rm -f "$f" && echo "  removed $f"
  done
  if [ -f "$HOME/.zshrc" ] && grep -qF "$ZSH_FPATH_DIR" "$HOME/.zshrc"; then
    backup "$HOME/.zshrc"
    grep -vF -e "# kelsier completion" -e "fpath+=($ZSH_FPATH_DIR)" "$HOME/.zshrc" > "$HOME/.zshrc.new" \
      && mv "$HOME/.zshrc.new" "$HOME/.zshrc"
    echo "  removed the fpath line from ~/.zshrc (backed up)"
  fi

  if [ -f "$HOME/.config/tmux/tmux.conf" ]; then TMUX_CONF="$HOME/.config/tmux/tmux.conf"; else TMUX_CONF="$HOME/.tmux.conf"; fi
  SRC_LINE="source-file $KEL_DIR/tmux/kel.conf"
  if [ -f "$TMUX_CONF" ] && grep -qF "$SRC_LINE" "$TMUX_CONF"; then
    backup "$TMUX_CONF"
    grep -vF -e "# kelsier" -e "$SRC_LINE" "$TMUX_CONF" > "$TMUX_CONF.new" && mv "$TMUX_CONF.new" "$TMUX_CONF"
    echo "  removed the source-file line from $TMUX_CONF (backed up)"
  fi

  if [ -f "$SETTINGS" ]; then
    backup "$SETTINGS"
    tmp="$(mktemp)"
    # drop kel's hook entries and, if kel displaced one, restore the statusLine
    # it replaced rather than just deleting ours
    PREV_FILE="$HOME/.claude/statusline-prev"
    PREV=''
    [ -f "$PREV_FILE" ] && PREV="$(grep -v '^#' "$PREV_FILE" | grep -v '^$' | head -1)"
    jq --arg prev "$PREV" '
      def strip_kel:
        map(.hooks |= map(select((.command // "") | test("kel[ -]hook|kel-hook\\.sh") | not)))
        | map(select((.hooks | length) > 0));
      (if .hooks then .hooks |= with_entries(.value |= strip_kel) else . end)
      | (if .hooks then .hooks |= with_entries(select((.value | length) > 0)) else . end)
      | (if (.hooks // {}) == {} then del(.hooks) else . end)
      | if ((.statusLine.command // "") | test("kel statusline"))
        then (if $prev == "" then del(.statusLine)
              else .statusLine = {type:"command", command:$prev} end)
        else . end
    ' "$SETTINGS" > "$tmp" && mv "$tmp" "$SETTINGS"
    echo "  unwired hooks + statusLine from $SETTINGS (backed up)"
    if [ -n "$PREV" ]; then
      echo "  restored the statusLine kel displaced: $PREV"
      rm -f "$PREV_FILE"
    fi
  fi

  if [ "$purge" = 1 ]; then
    printf '\033[1;31m>>> purge: delete %s and every agent record in it? [y/N] \033[0m' "$STATE_DIR"
    read -r a || a=n
    case "$a" in
      y|Y|yes) rm -rf "$STATE_DIR"; echo "  removed $STATE_DIR" ;;
      *)       echo "  kept $STATE_DIR" ;;
    esac
  else
    echo "  kept $STATE_DIR  (your agents and records — --purge also removes it)"
  fi

  say "done — kel is gone. Nothing in this repo was changed."
  echo "  running tmux sessions are untouched; 'tmux kill-server' when you're ready."
  exit 0
fi

command -v go >/dev/null || {
  echo "kel: Go is required (kel-fleet and kel-top are built from source)" >&2
  echo "     install/20-user-tools.sh (Debian/Ubuntu) or install/macos-tools.sh" >&2
  exit 1
}

say "directories"
mkdir -p "$BIN_DIR" "$STATE_DIR" "$STATE_DIR/sessions" "$STATE_DIR/.stash"

say "command -> $BIN_DIR/kel"
chmod +x "$KEL"
ln -sf "$KEL" "$BIN_DIR/kel"
# remove pre-v0.1 loose scripts
rm -f "$BIN_DIR/kel-status" "$BIN_DIR/kel-jump" "$BIN_DIR/kel-cheat"
echo "  kel  (new kill ls rename top sidebar restore relaunch sweep prune doctor config update + internals)"

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

say "kel-fleet + kel-top (Go)"
# Required since v0.10: the bash fleet reader is gone, so a failed build is a
# failed install.  The compiler's own output is left visible — hiding it is
# how a broken build used to read as "fine, using bash".
for b in kel-fleet kel-top; do
  (cd "$KEL_DIR" && go build -o "$BIN_DIR/$b" "./cmd/$b") \
    || { echo "kel: building $b failed" >&2; exit 1; }
  echo "  built $BIN_DIR/$b"
done
echo "  ($(go version | awk '{print $3}'))"

say "Claude Code hooks -> $SETTINGS"
mkdir -p "$HOME/.claude"
[ -f "$SETTINGS" ] || echo '{}' > "$SETTINGS"
backup "$SETTINGS"
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
# so the tmux bar, `kel ls` and `kel top` can show them without interrupting
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
# Quoted on purpose: nothing below is a variable, and an unquoted heredoc
# reads the backtick in "prefix `" as the start of a command substitution —
# which is how this message once died with "bad substitution".
cat <<'EOF'

  Reload:
    kel snapshot && tmux kill-server   # then `kel` — a restart is what drops
                                       # hooks and keys an older kel.conf set
    first install only: restart running Claude Code sessions so they load the hooks

  Use:
    kel                 go to (or start) this repo's agent — never asks anything
    kel new <name>      new window + agent   (-w = git worktree, --group G)
    kel restore         bring back the previous workspace   (--list to choose)
    kel sidebar off|on  the pane on the left of every window
    prefix t            kel top — every agent at a glance   ·   prefix `  next waiting
    kel kill <name>     close one      ·   kel doctor    check the machine
EOF

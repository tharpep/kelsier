#compdef kel
# zsh completion for kel (kelsier)
# install.sh drops this on an fpath dir (~/.local/share/zsh/site-functions)

# $1 = jq filter producing strings.  NOTE the filters below start at
# `.agents[]`: `kel ls --json` emits the whole fleet DOCUMENT
# ({generated_at, current, agents}), not a bare array.  Both `2>/dev/null`s
# hide a wrong filter completely, so a shape change here fails silently —
# test/kel-test.sh asserts this filter still returns names.
_kel_names() {
  kel ls --json 2>/dev/null | jq -r "$1" 2>/dev/null | sort -u
}

_kel() {
  local -a subcmds
  subcmds=(
    'new:window + agent (in place or a git worktree)'
    'kill:close a window, remove its worktree'
    'ls:list agents, grouped by repo'
    'go:switch to a group'
    'move:put the current window in another group'
    'rename:rename the current window (keeps metadata in sync)'
    'board:fzf fleet browser'
    'top:fleet dashboard — sorted by who needs you'
    'restore:rebuild the workspace after a kill / reboot'
    'restart:relaunch a crashed agent in its existing window'
    'sweep:close finished agents whose work has merged'
    'prune:discard dead agent records'
    'doctor:capability probe'
    'cheat:keybinding reference'
    'help:usage'
  )

  if (( CURRENT == 2 )); then
    _describe -t commands 'kel command' subcmds
    return
  fi

  local sub="${words[2]}"
  case "$sub" in
    kill)
      _alternative \
        'flags:flag:(-f)' \
        "agents:agent:($(_kel_names '.agents[] | .name, "\(.group)/\(.name)"'))"
      ;;
    go|move)
      compadd -- $(_kel_names '.agents[].group')
      ;;
    new)
      case "${words[CURRENT-1]}" in
        --group) compadd -- $(_kel_names '.agents[].group') ;;
        --agent) _default ;;
        *)       compadd -- -w --group --agent --no-agent ;;
      esac
      ;;
    restore) compadd -- -c -s ;;
    restart)
      _alternative 'flags:flag:(-f)' \
        "agents:agent:($(_kel_names '.agents[] | .name, "\(.group)/\(.name)"'))"
      ;;
    sweep)   compadd -- -n --dry-run -f --force ;;
    prune)   compadd -- -f ;;
    ls)      compadd -- --json ;;
  esac
}

_kel "$@"

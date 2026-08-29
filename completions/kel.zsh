#compdef kel
# zsh completion for kel (kelsier)
# install.sh drops this on an fpath dir (~/.local/share/zsh/site-functions)

_kel_names() {   # $1 = jq filter producing strings
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
    'restore:rebuild the workspace after a kill / reboot'
    'restart:relaunch a crashed agent in its existing window'
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
        "agents:agent:($(_kel_names '.[].name, "\(.group)/\(.name)"'))"
      ;;
    go|move)
      compadd -- $(_kel_names '.[].group')
      ;;
    new)
      case "${words[CURRENT-1]}" in
        --group) compadd -- $(_kel_names '.[].group') ;;
        --agent) _default ;;
        *)       compadd -- -w --group --agent --no-agent ;;
      esac
      ;;
    restore) compadd -- -c -s ;;
    restart)
      _alternative 'flags:flag:(-f)' \
        "agents:agent:($(_kel_names '.[].name, "\(.group)/\(.name)"'))"
      ;;
    prune)   compadd -- -f ;;
    ls)      compadd -- --json ;;
  esac
}

_kel "$@"

# bash completion for kel (kelsier)
# installed to ~/.local/share/bash-completion/completions/kel by install.sh

_kel_names() {   # $1 = jq filter producing strings
  kel ls --json 2>/dev/null | jq -r "$1" 2>/dev/null | sort -u
}

_kel() {
  local cur prev words cword
  _init_completion 2>/dev/null || {
    cur="${COMP_WORDS[COMP_CWORD]}"
    prev="${COMP_WORDS[COMP_CWORD-1]}"
    cword=$COMP_CWORD
  }

  local subcmds="new kill ls go move rename board restore prune doctor cheat help"

  if [ "$cword" -eq 1 ]; then
    COMPREPLY=( $(compgen -W "$subcmds" -- "$cur") )
    return
  fi

  local sub="${COMP_WORDS[1]}"
  case "$sub" in
    kill)
      case "$cur" in
        -*) COMPREPLY=( $(compgen -W "-f" -- "$cur") ) ;;
        *)  # bare names, plus group/name for the ones that repeat across groups
            COMPREPLY=( $(compgen -W "$(_kel_names '.[].name, "\(.group)/\(.name)"')" -- "$cur") ) ;;
      esac
      ;;
    go|move)
      COMPREPLY=( $(compgen -W "$(_kel_names '.[].group')" -- "$cur") )
      ;;
    new)
      case "$prev" in
        --group) COMPREPLY=( $(compgen -W "$(_kel_names '.[].group')" -- "$cur") ) ;;
        --agent) ;;
        *)       COMPREPLY=( $(compgen -W "-w --group --agent --no-agent" -- "$cur") ) ;;
      esac
      ;;
    restore)
      COMPREPLY=( $(compgen -W "-c -s" -- "$cur") )
      ;;
    prune)
      COMPREPLY=( $(compgen -W "-f" -- "$cur") )
      ;;
    ls)
      COMPREPLY=( $(compgen -W "--json" -- "$cur") )
      ;;
  esac
}
complete -F _kel kel

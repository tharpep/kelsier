# bash completion for kel (kelsier)
# installed to ~/.local/share/bash-completion/completions/kel by install.sh

# $1 = jq filter producing strings.  NOTE the filters below start at
# `.agents[]`: `kel ls --json` emits the whole fleet DOCUMENT
# ({generated_at, current, agents}), not a bare array.  Both `2>/dev/null`s
# hide a wrong filter completely, so a shape change here fails silently —
# test/kel-test.sh asserts this filter still returns names.
_kel_names() {
  kel ls --json 2>/dev/null | jq -r "$1" 2>/dev/null | sort -u
}

_kel() {
  local cur prev words cword
  _init_completion 2>/dev/null || {
    cur="${COMP_WORDS[COMP_CWORD]}"
    prev="${COMP_WORDS[COMP_CWORD-1]}"
    cword=$COMP_CWORD
  }

  local subcmds="new kill ls go move adopt rename board top restore restart sweep prune doctor cheat help"

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
            COMPREPLY=( $(compgen -W "$(_kel_names '.agents[] | .name, "\(.group)/\(.name)"')" -- "$cur") ) ;;
      esac
      ;;
    go|move|adopt)
      COMPREPLY=( $(compgen -W "$(_kel_names '.agents[].group')" -- "$cur") )
      ;;
    new)
      case "$prev" in
        --group) COMPREPLY=( $(compgen -W "$(_kel_names '.agents[].group')" -- "$cur") ) ;;
        --agent) ;;
        *)       COMPREPLY=( $(compgen -W "-w --group --agent --no-agent" -- "$cur") ) ;;
      esac
      ;;
    restore)
      COMPREPLY=( $(compgen -W "-c -s" -- "$cur") )
      ;;
    restart)
      case "$cur" in
        -*) COMPREPLY=( $(compgen -W "-f" -- "$cur") ) ;;
        *)  COMPREPLY=( $(compgen -W "$(_kel_names '.agents[] | .name, "\(.group)/\(.name)"')" -- "$cur") ) ;;
      esac
      ;;
    sweep)
      COMPREPLY=( $(compgen -W "-n --dry-run -f --force" -- "$cur") )
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

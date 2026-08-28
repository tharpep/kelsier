#!/usr/bin/env bash
# kelsier setup — per-user tools (run as tharpep)
set -euo pipefail

say() { printf '\n\033[1;33m>>> %s\033[0m\n' "$*"; }

mkdir -p "$HOME/.local/bin"

say "zoxide"
curl -sSfL https://raw.githubusercontent.com/ajeetdsouza/zoxide/main/install.sh | sh
"$HOME/.local/bin/zoxide" --version

say "fzf (git clone + install)"
if [ ! -d "$HOME/.fzf" ]; then
  git clone --depth 1 https://github.com/junegunn/fzf.git "$HOME/.fzf"
else
  git -C "$HOME/.fzf" pull --ff-only
fi
"$HOME/.fzf/install" --key-bindings --completion --no-update-rc
"$HOME/.fzf/bin/fzf" --version

say "fnm"
curl -fsSL https://fnm.vercel.app/install | bash -s -- --skip-shell
export PATH="$HOME/.local/share/fnm:$PATH"
fnm --version

say "shell rc block"
RC="$HOME/.bashrc"
if ! grep -q '>>> kelsier env >>>' "$RC" 2>/dev/null; then
cat >> "$RC" <<'EOF'

# >>> kelsier env >>>
export PATH="$HOME/.local/bin:$HOME/.local/share/fnm:$HOME/.fzf/bin:$PATH"
command -v fnm    >/dev/null && eval "$(fnm env --use-on-cd --shell bash)"
command -v zoxide >/dev/null && eval "$(zoxide init bash)"
[ -f "$HOME/.fzf.bash" ] && source "$HOME/.fzf.bash"
alias fd=fdfind
alias bat=batcat
# <<< kelsier env <<<
EOF
  echo "appended kelsier block to ~/.bashrc"
else
  echo "kelsier block already present in ~/.bashrc"
fi

say "Node.js LTS via fnm"
eval "$(fnm env --shell bash)"
fnm install --lts
fnm default "$(fnm list | grep -oE 'v[0-9]+\.[0-9]+\.[0-9]+' | tail -1)"
fnm use default
node --version
npm --version

say "user tools done"

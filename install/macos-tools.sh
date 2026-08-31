#!/usr/bin/env bash
# kelsier setup — macOS, via Homebrew.
#
# The Linux path is two scripts because apt splits root-owned system packages
# from per-user installs. Homebrew has no such split — it installs into a
# prefix you already own — so macOS is one script and needs no sudo.
#
#   bash install/macos-tools.sh
#
# Then the same last steps as Linux: Claude Code, git identity, gh auth, and
# the repo-root ./install.sh. See install/README.md.
set -euo pipefail

say() { printf '\n\033[1;33m>>> %s\033[0m\n' "$*"; }

command -v brew >/dev/null 2>&1 || {
  echo "Homebrew is required: https://brew.sh" >&2
  exit 1
}

say "Homebrew packages"
# kel itself needs only tmux, git and jq; fzf is needed for `kel board`.
# The rest is the same working set the Linux scripts install.
brew install \
  tmux git jq fzf \
  ripgrep fd bat btop \
  gh neovim lazygit yazi git-delta tealdeer \
  zoxide fnm

say "Go (optional — builds kel-fleet and kel top)"
# kel works without Go: bin/kel falls back to its bash fleet reader, and
# `kel top` says so plainly rather than dying.
if command -v go >/dev/null 2>&1; then
  echo "  already installed: $(go version)"
else
  brew install go
fi

say "shell rc block"
# macOS defaults to zsh, so this is ~/.zshrc rather than the ~/.bashrc block
# 20-user-tools.sh writes. Deliberately NO `fd=fdfind` / `bat=batcat` aliases:
# those exist only because Debian renames both binaries. Homebrew installs
# them under their real names, and the aliases would break them here.
RC="$HOME/.zshrc"
if ! grep -q '>>> kelsier env >>>' "$RC" 2>/dev/null; then
cat >> "$RC" <<'EOF'

# >>> kelsier env >>>
export PATH="$HOME/.local/bin:$PATH"
command -v fnm    >/dev/null && eval "$(fnm env --use-on-cd --shell zsh)"
command -v zoxide >/dev/null && eval "$(zoxide init zsh)"
[ -f "$HOME/.fzf.zsh" ] && source "$HOME/.fzf.zsh"
# <<< kelsier env <<<
EOF
  echo "appended kelsier block to ~/.zshrc"
else
  echo "kelsier block already present in ~/.zshrc"
fi

say "Node.js LTS via fnm"
eval "$(fnm env --shell zsh)"
fnm install --lts
fnm default "$(fnm list | grep -oE 'v[0-9]+\.[0-9]+\.[0-9]+' | tail -1)"
fnm use default
node --version
npm --version

say "macOS tools done — now: Claude Code, gh auth login, then ./install.sh"

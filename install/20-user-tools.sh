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

say "Go (optional — builds kel-fleet, the faster fleet reader)"
# Deliberately the go.dev tarball, not apt: Ubuntu ships a Go too old to build
# recent modules. Into ~/.local/go so this needs no sudo.
# kel works fine without any of this — bin/kel falls back to bash.
if command -v go >/dev/null 2>&1; then
  echo "  already installed: $(go version)"
else
  GO_VER="$(curl -fsSL 'https://go.dev/VERSION?m=text' | head -1)"
  case "$(uname -m)" in
    x86_64)         GO_ARCH=amd64 ;;
    aarch64|arm64)  GO_ARCH=arm64 ;;
    *)              GO_ARCH='' ;;
  esac
  if [ -n "${GO_VER:-}" ] && [ -n "$GO_ARCH" ]; then
    curl -fsSL "https://go.dev/dl/${GO_VER}.linux-${GO_ARCH}.tar.gz" -o /tmp/go.tgz
    rm -rf "$HOME/.local/go" && mkdir -p "$HOME/.local" && tar -C "$HOME/.local" -xzf /tmp/go.tgz
    rm -f /tmp/go.tgz
    mkdir -p "$HOME/.local/bin"
    ln -sf "$HOME/.local/go/bin/go"     "$HOME/.local/bin/go"
    ln -sf "$HOME/.local/go/bin/gofmt"  "$HOME/.local/bin/gofmt"
    echo "  installed $("$HOME/.local/go/bin/go" version)"
  else
    echo "  skipped (could not resolve a Go release for $(uname -m))"
  fi
fi

say "user tools done"

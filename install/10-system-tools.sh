#!/usr/bin/env bash
# kelsier setup — system-level upstream tools (run as root)
set -euo pipefail
cd /tmp

say() { printf '\n\033[1;33m>>> %s\033[0m\n' "$*"; }

say "Neovim (official tarball -> /opt)"
curl -fsSL -o nvim.tar.gz https://github.com/neovim/neovim/releases/latest/download/nvim-linux-x86_64.tar.gz
rm -rf /opt/nvim-linux-x86_64
tar -C /opt -xzf nvim.tar.gz
ln -sf /opt/nvim-linux-x86_64/bin/nvim /usr/local/bin/nvim
rm -f nvim.tar.gz
nvim --version | head -1

say "lazygit"
LG_VER=$(curl -fsSL "https://api.github.com/repos/jesseduffield/lazygit/releases/latest" | jq -r '.tag_name' | sed 's/^v//')
curl -fsSL -o lazygit.tar.gz "https://github.com/jesseduffield/lazygit/releases/download/v${LG_VER}/lazygit_${LG_VER}_Linux_x86_64.tar.gz"
tar -xf lazygit.tar.gz lazygit
install -m 755 lazygit /usr/local/bin/
rm -f lazygit lazygit.tar.gz
lazygit --version

say "yazi + ya"
curl -fsSL -o yazi.zip https://github.com/sxyazi/yazi/releases/latest/download/yazi-x86_64-unknown-linux-musl.zip
unzip -oq yazi.zip
install -m 755 yazi-x86_64-unknown-linux-musl/yazi /usr/local/bin/
install -m 755 yazi-x86_64-unknown-linux-musl/ya   /usr/local/bin/
rm -rf yazi.zip yazi-x86_64-unknown-linux-musl
yazi --version

say "git-delta"
DELTA_VER=$(curl -fsSL "https://api.github.com/repos/dandavison/delta/releases/latest" | jq -r '.tag_name')
curl -fsSL -o delta.deb "https://github.com/dandavison/delta/releases/download/${DELTA_VER}/git-delta_${DELTA_VER}_amd64.deb"
DEBIAN_FRONTEND=noninteractive apt-get install -y -qq ./delta.deb
rm -f delta.deb
delta --version

say "GitHub CLI (official apt repo)"
mkdir -p -m 755 /etc/apt/keyrings
curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg -o /etc/apt/keyrings/githubcli-archive-keyring.gpg
chmod go+r /etc/apt/keyrings/githubcli-archive-keyring.gpg
echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" \
  > /etc/apt/sources.list.d/github-cli.list
apt-get update -qq
DEBIAN_FRONTEND=noninteractive apt-get install -y -qq gh
gh --version | head -1

say "win32yank (clipboard paste fallback)"
curl -fsSL -o win32yank.zip https://github.com/equalsraf/win32yank/releases/latest/download/win32yank-x64.zip
unzip -oq win32yank.zip win32yank.exe
chmod +x win32yank.exe
mv -f win32yank.exe /usr/local/bin/
rm -f win32yank.zip
echo "win32yank -> $(command -v win32yank.exe)"

say "tealdeer (tldr client)"
# NOT apt: 24.04's tealdeer 1.6.1 fails to decompress the pages archive.
curl -fsSL -o /usr/local/bin/tldr https://github.com/tealdeer-rs/tealdeer/releases/latest/download/tealdeer-linux-x86_64-musl
chmod +x /usr/local/bin/tldr
/usr/local/bin/tldr --version

say "system tools done"

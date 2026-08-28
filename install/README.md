# install/

Seed scripts for what will become `install.sh`. These are the exact steps that
provisioned the first machine (WSL2 Ubuntu 24.04) on 2026-08-28 — kept here so
the eventual one-shot installer has a known-good starting point, and so a second
machine can be brought up by hand today if needed.

| Script | Runs as | Does |
|---|---|---|
| `10-system-tools.sh` | root | neovim (tarball), lazygit, yazi, git-delta, gh, win32yank |
| `20-user-tools.sh` | your user | zoxide, fzf, fnm + Node LTS, the `~/.bashrc` env block |

The apt packages (`tmux git build-essential curl wget unzip tar ripgrep fd-find
bat btop jq tldr` + yazi's preview stack) are installed separately — see
`docs/setup.md` §3a.

## Not yet done by these scripts (still manual — see docs/setup.md)

- Claude Code: `curl -fsSL https://claude.ai/install.sh | bash`
- git identity: `git config --global user.name / user.email`
- git + delta pager config (done once on machine 1)
- the kelsier hooks + tmux config (v0 — not built yet)

## Known issue

The apt `tldr` (Haskell client) fails its cache update with a binary-parse
error. Swap for `tealdeer` (`cargo install tealdeer`) or the pip client if it
keeps misbehaving.

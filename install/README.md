# install/

Seed scripts for what will become `install.sh`. These are the exact steps that
provisioned the first machine (WSL2 Ubuntu 24.04) on 2026-08-28 — kept here so
the eventual one-shot installer has a known-good starting point, and so a second
machine can be brought up by hand today if needed.

| Script | Runs as | Does |
|---|---|---|
| `10-system-tools.sh` | root | neovim (tarball), lazygit, yazi, git-delta, gh, win32yank, tealdeer |
| `20-user-tools.sh` | your user | zoxide, fzf, fnm + Node LTS, the `~/.bashrc` env block |

The apt packages (`tmux git build-essential curl wget unzip tar ripgrep fd-find
bat btop jq tldr` + yazi's preview stack) are installed separately — see
`docs/setup.md` §3a.

## Not yet done by these scripts (still manual — see docs/setup.md)

- Claude Code: `curl -fsSL https://claude.ai/install.sh | bash`
- git identity: `git config --global user.name / user.email`
- git + delta pager config (done once on machine 1)
- the kelsier hooks + tmux config (v0 — not built yet)

## Notes

- `tldr`: apt's Haskell `tldr` and apt's tealdeer 1.6.1 both fail to decompress
  the pages archive. `10-system-tools.sh` installs the tealdeer 1.9.0 release
  binary as `tldr`, which works.
- After install, run `wsl --set-default Ubuntu-24.04` from Windows if
  `docker-desktop` is still the default distro.

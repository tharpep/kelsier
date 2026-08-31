# install/

**Machine provisioning** — the toolchain kelsier assumes. Distinct from the
repo-root **`install.sh`**, which wires up `kel` itself (symlink + tmux config +
Claude Code hooks + bash/zsh completion) and assumes this toolchain is already
present.

These are the exact steps that provisioned the first machine (WSL2 Ubuntu 24.04,
2026-08-28), kept as a known-good starting point and so a second machine can be
brought up by hand.

| Script | Platform | Runs as | Installs |
|---|---|---|---|
| `10-system-tools.sh` | Debian/Ubuntu | root | neovim (tarball), lazygit, yazi, git-delta, gh, win32yank, tealdeer |
| `20-user-tools.sh` | Debian/Ubuntu | your user | zoxide, fzf, fnm + Node LTS, the `~/.bashrc` env block |
| `macos-tools.sh` | macOS | your user | all of the above via Homebrew, minus win32yank |

The apt packages (`tmux git build-essential curl wget unzip tar ripgrep fd-find
bat btop jq` + yazi's preview stack) install separately — `docs/setup.md` §3a.

**macOS is one script, not two**, because Homebrew installs into a prefix you
already own and so has no root/user split. It also skips `win32yank`, which is
a WSL clipboard shim, and writes its shell block to `~/.zshrc` without the
`fd=fdfind` / `bat=batcat` aliases — those exist only to undo Debian renames,
and on Homebrew they would break both tools.

## Order on a fresh machine

**macOS:** `bash macos-tools.sh`, then steps 4-6 below.

**Debian / Ubuntu / WSL2:**

1. apt packages — `docs/setup.md` §3a
2. `sudo bash 10-system-tools.sh`
3. `bash 20-user-tools.sh`
4. Claude Code — `curl -fsSL https://claude.ai/install.sh | bash`
5. `git config --global user.name / user.email`, `gh auth login`
6. `git clone` the repo into `~/code/kelsier`, then `./install.sh`

## Notes

- `tldr`: apt's Haskell client and apt's tealdeer 1.6.1 both fail on the current
  pages archive. `10-system-tools.sh` installs the tealdeer 1.9.0 release binary
  as `tldr`.
- Shell completion: repo-root `install.sh` symlinks `completions/kel.bash` to
  `~/.local/share/bash-completion/completions/kel` and `completions/kel.zsh` to
  `~/.local/share/zsh/site-functions/_kel` (adding an `fpath+=` line to
  `~/.zshrc` when that file exists). bash-completion 2.x picks the bash one up
  lazily; for zsh, run `compinit` or restart the shell.
- After provisioning, from Windows: `wsl --set-default Ubuntu-24.04` if
  `docker-desktop` is still the default distro.
- git + delta pager config isn't scripted — see `docs/setup.md`.

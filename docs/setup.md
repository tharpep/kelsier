# kelsier — machine setup

Target: **WSL2 Ubuntu**, terminal-only, no GUI. Also the reference for setting
`kel` up on a second machine — the steps are identical after the distro exists.

> Package landscape verified against web + `agy_research` on **2026-08-28**.
> Re-check the version notes if you're reading this much later.
>
> **Machine 1 provisioned 2026-08-28** (WSL2 2.6.1, Ubuntu 24.04.4, user
> `tharpep`): nvim 0.12.5, tmux 3.4, lazygit 0.64.1, yazi 26.8.15, delta 0.19.2,
> gh 2.98.0, fzf 0.74.3, zoxide 0.10.0, fnm 1.39.0, Node 24.20.0 (LTS),
> tealdeer 1.9.0, Claude Code 2.1.251. Scripts used: `install/10-system-tools.sh`,
> `install/20-user-tools.sh`. Post-setup: `wsl --set-default Ubuntu-24.04`
> (docker-desktop was default); git identity + delta pager configured.

---

## 1. WSL2 Ubuntu

**Pin the distro version.** `wsl --install -d Ubuntu` (bare name) is a rolling
pointer to whatever LTS the Store currently ships and it drifts over time.
`Ubuntu-24.04` is a fixed target — the same install on every machine, which is
what makes section 6 (second machine) reproducible.

**24.04 LTS, not 26.04.** As of 2026-08-28, Ubuntu 26.04 LTS "Resolute Raccoon"
is released (2026-04-23) but has **no WSL Store image yet** — `wsl --install`
still lands 24.04, and 26.04 requires a manual tarball import. Early reports
call 26.04-on-WSL bleeding-edge (provisioning-layer breakage, `sudo` 1.9.16+
behavior changes); the standard upgrade path opens with 26.04.1. There is no
reason to be early here: this stack installs almost everything from upstream
(nvim, lazygit, yazi, zoxide, gh, delta, node), so the apt base is just libc +
coreutils + `build-essential` + a few stable tools. 24.04 has standard support
to 2029. Revisit after 26.04.1 has settled, if ever.

From an elevated PowerShell on Windows:

```powershell
wsl --version              # if this errors: wsl --update
wsl --set-default-version 2
wsl --install -d Ubuntu-24.04
```

Reboot. Ubuntu opens a terminal and prompts **once** for a UNIX username and
password — this is a `sudo` account, not a desktop login. There is no login
screen and no GUI. After this, opening the terminal drops you straight at a
shell.

Set the Windows Terminal default profile to Ubuntu so "open terminal" means
"open Linux."

Docker Desktop: Settings -> Resources -> WSL Integration -> enable for the
`Ubuntu` distro. No systemd required.

---

## 2. Where code lives

```
~/code/<project>
```

Cloned from GitHub, the first time you actually work on a project. Never work
out of `/mnt/c/...` — cross-OS file access is slow and file-watching (which
Claude Code and Neovim both rely on) is unreliable there. Everything stays in
the Linux filesystem.

---

## 3. Packages

**On macOS, skip 3a–3c entirely** and run `bash install/macos-tools.sh` — one
Homebrew script that covers everything those three sections do. Homebrew has no
root/user split, so it needs no sudo and is not two scripts. Then continue at
3d (Claude Code). Two differences worth knowing: Homebrew installs `fd` and
`bat` under their real names, so the Debian rename aliases must **not** be
carried over; and the shell rc block goes in `~/.zshrc`, since macOS defaults
to zsh. `install/` is otherwise Debian/Ubuntu only.

### 3a. From apt  (Linux / WSL2)

```
sudo apt update && sudo apt install -y \
  tmux git build-essential curl wget unzip tar \
  ripgrep fd-find bat btop jq \
  ffmpegthumbnailer 7zip poppler-utils imagemagick chafa
```

Notes:
- `bat` installs as `batcat`, `fd-find` as `fdfind` — `install/20-user-tools.sh`
  aliases both back to `bat` / `fd` in the `~/.bashrc` block.
- Ubuntu 24.04 LTS ships current-enough `tmux` (3.4) and `ripgrep` (14.x).
- The last line is `yazi`'s preview stack (video thumbs, archives, PDF, images).
- **Neovim is deliberately not in this list.** apt's Neovim (0.9.5 on 24.04) is
  too old — current plugin ecosystems (Treesitter grammars, Lua LSP APIs,
  `blink.cmp` / `snacks.nvim` families) need 0.10+. Install from the official
  tarball (see 3b). Not the AppImage — that needs `libfuse2t64` on 24.04.

### 3b. Not in apt (or too old) — from upstream, handled by `install/*.sh`

| Tool | Source | Why not apt |
|---|---|---|
| `neovim` | official `nvim-linux-x86_64.tar.gz` → `/opt` | apt's 0.9.5 is below the 0.10+ floor for modern plugins |
| `lazygit` | GitHub release binary | not in the 24.04 repos |
| `yazi` | GitHub release binary (`yazi` + `ya`) | not packaged |
| `zoxide` | official install script | in 24.04 universe, but upstream ships fixes faster |
| `fzf` | `git clone ~/.fzf && ./install` | apt's 0.44 lacks 0.50+ features incl. `--tmux`, plus the shell keybindings |
| `gh` | GitHub's apt repo (keyring + source list) | not in default Ubuntu |
| `git-delta` | GitHub release `.deb` | 24.04's is older; **binary is `delta`, not `git-delta`** — git config must say `delta` |
| `win32yank` | GitHub release | clipboard paste fallback (see 4), no apt package |
| `tealdeer` (`tldr`) | GitHub release binary | apt's Haskell `tldr` AND apt's tealdeer 1.6.1 both fail to decompress the pages archive; 1.9.0 binary works |

### 3c. Node.js — via fnm, not apt

Claude Code needs Node. Ubuntu's apt Node is 18.x and stale, and a
Windows-side Node on the shared PATH will shadow it unpredictably.

```
curl -fsSL https://fnm.vercel.app/install | bash -s -- --skip-shell
# 20-user-tools.sh adds `eval "$(fnm env --use-on-cd --shell bash)"` to the shell rc
# then, in a new shell:
fnm install --lts && fnm default lts-latest
```

Installing Node via `fnm` inside WSL guarantees the Linux Node wins regardless
of what Windows has on PATH.

### 3d. Claude Code

Install inside Ubuntu (native installer or `npm i -g`). Its config lives at
`~/.claude/` **inside WSL** — separate from the Windows one. You will recreate
`settings.json` and re-add MCP servers here.

MCP servers to re-check under WSL:
- `agy` (Antigravity CLI) — needs a Linux binary on PATH, or call the Windows
  `.exe` via interop (`/mnt/c/.../agy.exe`).
- `claude-in-chrome` — talks to the Chrome extension on Windows over localhost;
  probably reachable from WSL, unverified.
- `playwright`, `context7`, `postgres`, `homelab` — npx / network based, fine.

---

## 4. Clipboard (Neovim)

Current best practice on Neovim 0.10+ is **OSC 52 for yank**, not `win32yank`:
Neovim streams yanked text to the terminal as an escape sequence, Windows
Terminal puts it on the Windows clipboard, zero subprocess overhead. `win32yank`
still works but adds ~20–50ms of Windows-interop latency per operation, and
`wl-clipboard` via WSLg desyncs after sleep/wake.

- **Yank →** `vim.g.clipboard` set to the OSC 52 provider (`vim.ui.clipboard.osc52`).
- **Paste →** terminal-native `Ctrl+Shift+V`, or `win32yank` as an explicit
  synchronous `"+p` provider. `10-system-tools.sh` drops `win32yank` on PATH as
  the fallback; `:checkhealth` auto-detects it, no `g:clipboard` override needed
  for paste.
- **tmux caveat:** OSC 52 through tmux needs `set -g allow-passthrough on` —
  `kel`'s tmux config includes it.

---

## 5. WSL2 gotchas, condensed

| Thing | Reality |
|---|---|
| File watching | works in `~`, broken on `/mnt/c` — keep code in `~/code` |
| Windows PATH | appended into WSL by default; Linux `node` via `fnm` still wins. For a clean cut, set `appendWindowsPath=false` in `/etc/wsl.conf` `[interop]` — but that also removes `clip.exe` etc. from PATH |
| Clipboard | Neovim yank via OSC 52; `win32yank` only as a paste fallback; needs `allow-passthrough on` in tmux |
| Yazi image previews | need a graphics-capable terminal (Kitty/Sixel) or `chafa` fallback — Windows Terminal support varies |
| systemd | not needed for this toolset; Docker Desktop integration doesn't require it |
| Time drift on resume | rare on current WSL; `sudo hwclock -s` if it happens |
| `apt` first run | `sudo apt update` before the first install |

---

## 5b. Desktop notifications (optional)

`kel` always flashes a `tmux display-message` when an agent starts waiting on
you and you are looking elsewhere. To get a notification that reaches you
outside the terminal, point `KEL_NOTIFY_CMD` at a script taking **title** and
**body**. kel runs it in the background, so a slow toast never stalls the hook.

kel deliberately ships no platform detection — one env var is easier to reason
about than three silent code paths.

**WSL2.** There is no desktop session, so `notify-send` is a dead end;
`powershell.exe` is on `PATH` and is the way out:

```sh
# ~/.local/bin/kel-toast    (chmod +x)
#!/bin/sh
# The body is Claude Code's notification text — the agent quoting a command
# back at you — so it is untrusted input, not a label you wrote.  PowerShell
# does no escape processing inside single quotes EXCEPT '' for a literal
# quote, so doubling them is the whole fix; interpolating raw lets a quote in
# the text close the string and run what follows.
t=$(printf '%s' "$1" | sed "s/'/''/g")
b=$(printf '%s' "$2" | sed "s/'/''/g")
powershell.exe -NoProfile -Command \
  "New-BurntToastNotification -Text '$t','$b'" >/dev/null 2>&1
# needs:  Install-Module -Name BurntToast -Scope CurrentUser
```

**macOS.** Same hazard, same reason — pass the text as arguments instead of
building a script out of it, so AppleScript never parses it as code:

```sh
#!/bin/sh
osascript - "$1" "$2" <<'APPLESCRIPT' >/dev/null 2>&1
on run argv
  display notification (item 2 of argv) with title (item 1 of argv)
end run
APPLESCRIPT
```

**Linux desktop.** `notify-send "$1" "$2"`.

Then:

```sh
export KEL_NOTIFY_CMD="$HOME/.local/bin/kel-toast"
export KEL_NOTIFY="waiting"        # or "waiting dead"
```

## 6. Second machine

On a Mac, steps 1–2 are `bash install/macos-tools.sh` (see §3); 3–5 are the
same. `install.sh` itself is portable and needs no change.

1. `wsl --install -d Ubuntu-24.04`, set user.
2. Install `fnm` + Node, Claude Code.
3. `git clone <kelsier repo> ~/code/kelsier`
4. `cd ~/code/kelsier && ./install.sh` — symlinks `kel`, sources `tmux/kel.conf`,
   wires the Claude Code hooks, and installs bash + zsh completion
   (`~/.local/share/{bash-completion/completions,zsh/site-functions}`; for zsh it
   also appends an `fpath+=` line to `~/.zshrc` — run `compinit` after).
5. Recreate `~/.claude/settings.json` MCP servers.

The only per-machine value is the repos root (`~/code`), and that's the default.

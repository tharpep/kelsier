#!/usr/bin/env bash
# kelsier setup — the optional tools kel opens (phase 3), per-user, no sudo.
#
#   glow      markdown in the terminal: `p` (plan and tasks) renders with it
#   ccusage   Claude Code usage from its own local transcripts: `u`
#   gh-dash   PRs and issues: `G` (a gh extension)
#   freeze    a pane to a PNG: `kel shot`
#   vhs       scripted terminal recordings (needs ttyd and ffmpeg, both here)
#
# Release binaries, not `go install`: a Go-built freeze crashed under Go 1.27
# on the author's machine, and the releases are what upstream tests. Every
# one is optional — kel says "<tool> is not installed" and goes on without.
set -euo pipefail

say() { printf '\n\033[1;33m>>> %s\033[0m\n' "$*"; }
BIN="$HOME/.local/bin"; mkdir -p "$BIN"
TMP="$(mktemp -d)"; trap 'rm -rf "$TMP"' EXIT

case "$(uname -m)" in
  x86_64)        ARCH=x86_64 FFARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64  FFARCH=arm64 ;;
  *) echo "unsupported architecture $(uname -m)" >&2; exit 1 ;;
esac

latest() { curl -fsSL "https://api.github.com/repos/$1/releases/latest" | jq -r .tag_name; }

# charm tools share one archive layout: <tool>_<ver>_Linux_<arch>.tar.gz
charm() { # tool
  local t="$1" tag ver
  tag="$(latest "charmbracelet/$t")"; ver="${tag#v}"
  curl -fsSL -o "$TMP/$t.tgz" \
    "https://github.com/charmbracelet/$t/releases/download/$tag/${t}_${ver}_Linux_${ARCH}.tar.gz"
  tar -xzf "$TMP/$t.tgz" -C "$TMP"
  install -m 755 "$(find "$TMP" -type f -name "$t" | head -1)" "$BIN/$t"
  "$BIN/$t" --version | head -1
}

say "glow";   charm glow
say "freeze"; charm freeze
say "vhs";    charm vhs

say "ttyd (for vhs)"
tag="$(latest tsl0922/ttyd)"
curl -fsSL -o "$BIN/ttyd" "https://github.com/tsl0922/ttyd/releases/download/$tag/ttyd.$( [ "$ARCH" = arm64 ] && echo aarch64 || echo x86_64 )"
chmod 755 "$BIN/ttyd"; "$BIN/ttyd" --version

say "ffmpeg (for vhs; a static build — apt would need sudo)"
if command -v ffmpeg >/dev/null 2>&1; then
  echo "  already installed: $(command -v ffmpeg)"
else
  curl -fsSL -o "$TMP/ff.txz" "https://johnvansickle.com/ffmpeg/releases/ffmpeg-release-${FFARCH}-static.tar.xz"
  tar -xJf "$TMP/ff.txz" -C "$TMP"
  install -m 755 "$(find "$TMP" -type f -name ffmpeg | head -1)"  "$BIN/ffmpeg"
  install -m 755 "$(find "$TMP" -type f -name ffprobe | head -1)" "$BIN/ffprobe"
  "$BIN/ffmpeg" -version | head -1
fi

say "ccusage (npm, pinned to the node it was installed with)"
# npm puts it under the fnm node it ran with; tmux's popups do not carry
# fnm's per-shell PATH, so ~/.local/bin gets a wrapper naming both by path
command -v npm >/dev/null 2>&1 || { echo "  skipped: no npm (install Node first: 20-user-tools.sh)"; exit 0; }
npm install -g --silent ccusage
NODE="$(command -v node)"; NODE="$(readlink -f "$NODE")"
JS="$(readlink -f "$(npm prefix -g)/bin/ccusage")"
printf '#!/bin/sh\nexec %s %s "$@"\n' "$NODE" "$JS" > "$BIN/ccusage"; chmod 755 "$BIN/ccusage"
"$BIN/ccusage" --version

say "gh-dash (gh extension)"
if command -v gh >/dev/null 2>&1; then
  gh extension install dlvhdr/gh-dash 2>/dev/null || gh extension upgrade dlvhdr/gh-dash || true
  gh extension list | grep -i dash || echo "  gh-dash not installed — is gh logged in? (gh auth login)"
else
  echo "  skipped: no gh"
fi

say "extras done"

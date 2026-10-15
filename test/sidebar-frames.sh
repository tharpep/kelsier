#!/usr/bin/env bash
# Print the sidebar's frames for every fixture, for review by eye (spec.md §5e).
#
#   test/sidebar-frames.sh            every fixture at 28, 32 and 40 columns
#   test/sidebar-frames.sh 32         one width
#   test/sidebar-frames.sh 32 30      one width, cut to 30 lines (the footer pins)
#
# Builds kel-top from this checkout, so what you see is what the golden tests
# hold. Once a frame is approved:  go test ./cmd/kel-top -run Golden -update
set -uo pipefail
cd "$(dirname "$0")/.." || exit 1
bin="$(mktemp -d)/kel-top"
trap 'rm -rf "$(dirname "$bin")"' EXIT
go build -o "$bin" ./cmd/kel-top || exit 1
args=(--sidebar --fixture cmd/kel-top/testdata/sidebar)
[ -n "${1:-}" ] && args+=(--width "$1")
[ -n "${2:-}" ] && args+=(--height "$2")
# the fixtures' paths live under /home/me; render ~ the way the tests do
HOME=/home/me "$bin" "${args[@]}"

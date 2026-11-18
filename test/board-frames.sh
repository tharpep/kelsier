#!/usr/bin/env bash
# Print the board's frames for every board fixture, for review by eye (spec.md §5e).
#
#   test/board-frames.sh                 every fixture, every face, at 150, 100 and 60 columns
#   test/board-frames.sh 150             one width
#   test/board-frames.sh 150 40 screen   one width and height, one face
#
# Builds kel-sidebar from this checkout, so what you see is what the golden tests
# hold. Once a frame is approved:  go test ./cmd/kel-sidebar -run Golden -update
set -uo pipefail
cd "$(dirname "$0")/.." || exit 1
bin="$(mktemp -d)/kel-sidebar"
trap 'rm -rf "$(dirname "$bin")"' EXIT
go build -o "$bin" ./cmd/kel-sidebar || exit 1
args=(--fixture cmd/kel-sidebar/testdata/board --board)
[ -n "${1:-}" ] && args+=(--width "$1")
[ -n "${2:-}" ] && args+=(--height "$2")
[ -n "${3:-}" ] && args+=(--face "$3")
# the fixtures' paths live under /home/me; render ~ the way the tests do
HOME=/home/me "$bin" "${args[@]}"

#!/usr/bin/env bash
# test.sh - run `go test` with streamed per-test progress via scripts/testfmt.
#
# Usage:
#   scripts/test.sh                                   # ./... (all packages)
#   scripts/test.sh -run TestSmoke ./library/tui/test/
#   scripts/test.sh -race ./...
set -o pipefail

# Resolve repo root from this script's location so it runs from anywhere.
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# Default to all packages when no target/flags are given.
if [ "$#" -eq 0 ]; then
	set -- ./...
fi

# `go test -json` exits non-zero on failure; testfmt re-exits non-zero on failure.
# pipefail surfaces either, so the overall exit code reflects failure exactly once.
go test -json "$@" | go run "$root/scripts/testfmt"

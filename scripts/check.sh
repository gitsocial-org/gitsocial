#!/usr/bin/env bash
# check.sh - the merge gate: `go vet`, `golangci-lint run`, then the full suite.
#
# This is the gate: the FULL `go test ./...` (~4 min warm, ~8 min cold), never
# `-short` — 66 subtests skip under it, so a short run is a smoke test, not a gate.
# `-race` and the browser site battery are release-time checks (scripts/release.sh).
#
# Usage:
#   scripts/check.sh                 # vet + lint + full test suite
#   scripts/check.sh --skip-lint     # allow a missing golangci-lint (not for the gate)
#   scripts/check.sh -short ./...    # extra args go to the test stage (smoke run)
set -o pipefail

# Resolve repo root from this script's location so it runs from anywhere.
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# --- arg parsing ---
SKIP_LINT=false
TEST_ARGS=()
for arg in "$@"; do
	case "$arg" in
	--skip-lint) SKIP_LINT=true ;;
	-h | --help)
		awk 'NR == 1 { next } /^#/ { print; next } { exit }' "$0"
		exit 0
		;;
	*) TEST_ARGS+=("$arg") ;;
	esac
done

# --- output helpers ---
log() { printf '\n\033[1m==> %s\033[0m\n' "$*"; }
info() { printf '    %s\n' "$*"; }
die() { printf '\n\033[31mFAILED at %s\033[0m\n' "$*" >&2; exit 1; }

log "1/3 go vet ./..."
go vet ./... || die "go vet"
info "vet clean"

log "2/3 golangci-lint run ./..."
if command -v golangci-lint >/dev/null 2>&1; then
	golangci-lint run ./... || die "golangci-lint"
	info "lint clean"
elif $SKIP_LINT; then
	info "(skip) golangci-lint not on PATH, --skip-lint given"
else
	die "golangci-lint (not found on PATH — 'brew install golangci-lint', or pass --skip-lint)"
fi

log "3/3 go test ./..."
"$root/scripts/test.sh" "${TEST_ARGS[@]}" || die "go test"

log "All checks passed"

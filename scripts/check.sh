#!/usr/bin/env bash
# check.sh - the gate: prose check, go vet, golangci-lint, then go test.
#
# Usage:
#   scripts/check.sh --quick       # push tier: every test except those guarded by GITSOCIAL_TEST_FULL
#   scripts/check.sh               # full tier: every test; before merging to main and at release
#   scripts/check.sh --skip-lint   # allow a missing golangci-lint (not a gate run)
#   scripts/check.sh -short ./...  # extra args go to the test stage (smoke run, not a gate run)
set -o pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

SKIP_LINT=false
QUICK=false
TEST_ARGS=()
for arg in "$@"; do
	case "$arg" in
	--skip-lint) SKIP_LINT=true ;;
	--quick) QUICK=true ;;
	-h | --help)
		awk 'NR == 1 { next } /^#/ { print; next } { exit }' "$0"
		exit 0
		;;
	*) TEST_ARGS+=("$arg") ;;
	esac
done

log() { printf '\n\033[1m==> %s\033[0m\n' "$*"; }
info() { printf '    %s\n' "$*"; }
die() { printf '\n\033[31mFAILED at %s\033[0m\n' "$*" >&2; exit 1; }

log "0/3 scripts/prose-check.sh"
"$root/scripts/prose-check.sh" || die "prose-check"
info "prose at or below baseline"

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
	die "golangci-lint (not on PATH: brew install golangci-lint, or pass --skip-lint)"
fi

if $QUICK; then
	log "3/3 go test ./... (quick tier)"
	unset GITSOCIAL_TEST_FULL
else
	log "3/3 go test ./... (full tier)"
	export GITSOCIAL_TEST_FULL=1
fi
"$root/scripts/test.sh" "${TEST_ARGS[@]}" || die "go test"

log "All checks passed"

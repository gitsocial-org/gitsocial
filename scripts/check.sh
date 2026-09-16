#!/usr/bin/env bash
# check.sh - the gate: prose check, import graph, go vet, golangci-lint, go test, coverage floors.
#
# Usage:
#   scripts/check.sh --quick       # push tier: every test except those guarded by GITSOCIAL_TEST_FULL
#   scripts/check.sh               # full tier: every test and the coverage check; before merging to main and at release
#   scripts/check.sh --skip-lint   # allow a missing golangci-lint (not a gate run)
#   scripts/check.sh -short ./...  # extra args go to the test stage (smoke run, not a gate run)
set -o pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
coverdir="$root/.test-artifacts/coverage"

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

# Only the full tier runs the coverage stage, so the stage count differs by tier.
if $QUICK; then
	last=4
else
	last=5
fi

log "0/$last scripts/prose-check.sh"
"$root/scripts/prose-check.sh" || die "prose-check"
info "prose at or below baseline"

log "1/$last scripts/import-graph.sh --check"
"$root/scripts/import-graph.sh" --check || die "import-graph"
info "imports at or below baseline"

log "2/$last go vet -tags bench ./..."
go vet -tags bench ./... || die "go vet"
info "vet clean"

log "3/$last golangci-lint run --build-tags bench ./..."
if command -v golangci-lint >/dev/null 2>&1; then
	golangci-lint run --build-tags bench ./... || die "golangci-lint"
	info "lint clean"
elif $SKIP_LINT; then
	info "(skip) golangci-lint not on PATH, --skip-lint given"
else
	die "golangci-lint (not on PATH: brew install golangci-lint, or pass --skip-lint)"
fi

if $QUICK; then
	log "4/$last go test ./... (quick tier)"
	unset GITSOCIAL_TEST_FULL
	"$root/scripts/test.sh" "${TEST_ARGS[@]}" || die "go test"
else
	log "4/$last go test ./... (full tier, writing the coverage profile)"
	export GITSOCIAL_TEST_FULL=1
	mkdir -p "$coverdir"
	# The cover flags fill the argument list, so name the packages here.
	[ ${#TEST_ARGS[@]} -gt 0 ] || TEST_ARGS=(./...)
	"$root/scripts/test.sh" "${TEST_ARGS[@]}" -coverpkg=./... -coverprofile="$coverdir/coverage.out" || die "go test"

	log "5/$last scripts/coverage.sh --check"
	"$root/scripts/coverage.sh" --check "$coverdir/coverage.out" || die "coverage"
fi

log "All checks passed"

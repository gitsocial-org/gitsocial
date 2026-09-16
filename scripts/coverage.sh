#!/usr/bin/env bash
# coverage.sh - measure statement-weighted coverage across every package.
#
# Runs the suite with `-coverpkg=./...` so code exercised by another package's
# integration tests gets credit, then reports the STATEMENT-WEIGHTED total (not
# a mean of per-package percentages, which lets a 46-statement package at 100%
# offset a 6,000-statement package at 3%). Packages with no test files of their
# own are included: `-coverpkg` instruments them too.
#
# Usage:
#   scripts/coverage.sh                    # writes .test-artifacts/coverage/
#   scripts/coverage.sh <outdir>           # writes somewhere else
#   scripts/coverage.sh --check [profile]  # fail on a package more than 2.0 points under the baseline
#   scripts/coverage.sh --update [profile] # rewrite scripts/coverage-baseline.txt from a profile
#   GS_COVER_PROFILE=old.out scripts/coverage.sh   # re-report an existing profile
#
# --check and --update default to .test-artifacts/coverage/coverage.out, the profile the full tier writes.
#
# Outputs (in <outdir>):
#   coverage.out         the merged profile
#   coverage.html        `go tool cover -html`, line-level browsing
#   summary.txt          this report (total, ranked package table, blind spots)
#   functions.txt        per-function coverage (`go tool cover -func`)
#   zero-functions.txt   every function at 0.0%
set -o pipefail

# Resolve repo root from this script's location so it runs from anywhere.
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
module="github.com/gitsocial-org/gitsocial/"
baseline="$root/scripts/coverage-baseline.txt"
gateprofile="$root/.test-artifacts/coverage/coverage.out"

log() { printf '\n\033[1m==> %s\033[0m\n' "$*"; }
info() { printf '    %s\n' "$*"; }

# aggregate prints "TOTAL hit total" and one "PKG <import path> <hit> <total>" line per package of a profile
aggregate() {
	# Blocks repeat across test binaries under -coverpkg, so a block counts once and is covered if any binary covered it.
	awk -v pkglist="$2" '
	NR == 1 { next }                                   # "mode:" header
	{
		split($1, part, ":")
		file = part[1]
		sub(/\/[^\/]*$/, "", file)
		if (!($1 in stmts)) { stmts[$1] = $2; blockpkg[$1] = file }
		if ($3 + 0 > 0) covered[$1] = 1
	}
	END {
		while ((getline line < pkglist) > 0) pkgs[line] = 0
		for (b in stmts) {
			p = blockpkg[b]
			total += stmts[b]
			pkgs[p] += stmts[b]
			if (b in covered) { hit += stmts[b]; pkghit[p] += stmts[b] }
		}
		printf "TOTAL %d %d\n", hit, total
		for (p in pkgs) printf "PKG %s %d %d\n", p, pkghit[p] + 0, pkgs[p]
	}
	' "$1"
}

# percentages prints package, percentage and statement count, tab separated, one line per package
percentages() {
	grep '^PKG ' "$1" | awk -v m="$module" '{ p = $2; sub(m, "", p); printf "%s\t%.1f\t%d\n", p, $4 ? 100 * $3 / $4 : 0, $4 }' | sort
}

# ratchet rewrites the baseline from a profile, or fails when a package sits more than 2.0 points under it
ratchet() {
	local mode="$1" profile="${2:-$gateprofile}" work
	if [ ! -s "$profile" ]; then
		printf 'coverage: no profile at %s; run scripts/check.sh or scripts/coverage.sh first\n' "$profile" >&2
		return 1
	fi
	work="$(mktemp -d)"
	trap 'rm -rf "$work"' EXIT
	(cd "$root" && go list ./...) >"$work/packages.txt"
	aggregate "$profile" "$work/packages.txt" >"$work/agg.txt"
	percentages "$work/agg.txt" >"$work/current.txt"
	if [ "$mode" = update ]; then
		cp "$work/current.txt" "$baseline"
		info "wrote $baseline: $(wc -l <"$baseline" | tr -d ' ') packages"
		return 0
	fi
	if [ ! -f "$baseline" ]; then
		printf 'coverage: no %s; run scripts/coverage.sh --update\n' "$baseline" >&2
		return 1
	fi
	awk -F'\t' '
	FNR == NR { base[$1] = $2; next }
	{ cur[$1] = $2 }
	END {
		for (p in base) {
			if (!(p in cur)) { gone = gone sprintf("  %s\n", p); continue }
			d = base[p] - cur[p]
			if (d > 2.001) dropped = dropped sprintf("  %-46s %.1f%% -> %.1f%% (-%.1f)\n", p, base[p], cur[p], d)
			kept++
		}
		for (p in cur) if (!(p in base)) added = added sprintf("  %s\n", p)
		if (dropped != "") printf "coverage: package more than 2.0 points under the baseline (scripts/coverage.sh --update to accept)\n%s", dropped
		if (gone != "") printf "coverage: baseline package missing from the profile\n%s", gone
		if (added != "") printf "coverage: package not in the baseline, not checked\n%s", added
		if (dropped != "" || gone != "") exit 1
		printf "    %d packages at or above their baseline\n", kept
	}' "$baseline" "$work/current.txt"
}

case "${1:-}" in
--check) ratchet check "${2:-}"; exit $? ;;
--update) ratchet update "${2:-}"; exit $? ;;
-h | --help) awk 'NR == 1 { next } /^#/ { sub(/^# ?/, ""); print; next } { exit }' "$0"; exit 0 ;;
esac

out="${1:-$root/.test-artifacts/coverage}"
mkdir -p "$out"
out="$(cd "$out" && pwd)"
profile="$out/coverage.out"
summary="$out/summary.txt"

# Run the suite under -coverpkg unless a profile was supplied to re-report.
if [ -n "${GS_COVER_PROFILE:-}" ]; then
	log "Reusing profile: $GS_COVER_PROFILE"
	[ "$GS_COVER_PROFILE" -ef "$profile" ] || cp "$GS_COVER_PROFILE" "$profile"
else
	log "go test ./... -coverpkg=./... (full suite, ~10 min)"
	if ! (cd "$root" && go test ./... -coverpkg=./... -coverprofile="$profile" >"$out/test.log" 2>&1); then
		info "WARNING: the suite is RED — figures below describe a failing run"
		info "see $out/test.log"
	fi
	[ -s "$profile" ] || { printf '\033[31merror: no coverage profile produced (see %s)\033[0m\n' "$out/test.log" >&2; exit 1; }
fi

# Statement-weighted totals and per-package table, computed from the profile.
(cd "$root" && go list ./...) >"$out/packages.txt"
aggregate "$profile" "$out/packages.txt" >"$out/agg.txt"

read -r _ hit total <<<"$(grep '^TOTAL ' "$out/agg.txt")"
pct=$(awk -v h="$hit" -v t="$total" 'BEGIN { printf "%.1f", t ? 100 * h / t : 0 }')
pkgcount=$(grep -c '^PKG ' "$out/agg.txt")
listed=$(wc -l <"$out/packages.txt" | tr -d ' ')

{
	printf 'GitSocial coverage — %s\n\n' "$(date '+%Y-%m-%d %H:%M')"
	printf 'STATEMENT-WEIGHTED TOTAL: %s%%  (%s of %s statements, %s packages)\n\n' "$pct" "$hit" "$total" "$pkgcount"
	printf 'Measured with -coverpkg=./... over `go list ./...` (%s packages), so a\n' "$listed"
	printf 'package with no test files of its own still counts, and code exercised by\n'
	printf 'another package'"'"'s integration tests gets credit.\n\n'
	printf 'THE NUMBER IS A FLOOR. Coverage cannot see two real test tiers:\n'
	printf '  1. The S3 helper tests drive `git push` and run the remote helper as a\n'
	printf '     CHILD PROCESS, so helper_push.go and thin.go get zero credit for ~20\n'
	printf '     passing tests.\n'
	printf '  2. ~40 browser suites behind `-tags sitetest` are JavaScript, so\n'
	printf '     site_pages*.go looks untested and is not.\n'
	printf 'Covered is also not correct: read the 0.0%% function list, not the table.\n\n'
	printf 'Per package, ranked (weakest first; statements are the weight):\n\n'
	printf '%-46s %7s %10s\n' "package" "cover" "stmts"
	grep '^PKG ' "$out/agg.txt" | awk -v m="$module" '{
		p = $2; sub(m, "", p)
		printf "%9.4f  %-46s %6.1f%% %10d\n", $4 ? 100 * $3 / $4 : 0, p, $4 ? 100 * $3 / $4 : 0, $4
	}' | sort -n | cut -c12-
} >"$summary"

# Function-level detail: every function the suite never enters.
(cd "$root" && go tool cover -func="$profile") | sed "s|$module||" >"$out/functions.txt"
awk '$NF == "0.0%"' "$out/functions.txt" >"$out/zero-functions.txt"
zero=$(wc -l <"$out/zero-functions.txt" | tr -d ' ')
zerolib=$(grep -cE '^library/(core|extensions|proposals|client)' "$out/zero-functions.txt")
printf '\n%s functions at 0.0%%: %s\n' "$zero" "$out/zero-functions.txt" >>"$summary"
printf '%s of them under library/{core,extensions,proposals,client*}; the rest are cli/ and tui/.\n' "$zerolib" >>"$summary"
printf '(ignore helper_push.go, thin.go and site_pages*.go there: blind spots, not gaps)\n' >>"$summary"

(cd "$root" && go tool cover -html="$profile" -o "$out/coverage.html")

cat "$summary"
log "Wrote $out/{coverage.out,coverage.html,summary.txt,zero-functions.txt}"

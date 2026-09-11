#!/usr/bin/env bash
# import-graph.sh - per-package size, churn, fan-in, fan-out and layer violations for the module.
# Usage: scripts/import-graph.sh [--check | --update]   no argument prints the full report
# --check fails on an upward import edge missing from scripts/import-baseline.txt or a package over 15000 non-test lines.
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root" || exit 1
baseline="scripts/import-baseline.txt"
ceiling=15000
DAYS=${DAYS:-90}

mode=report
case "${1:-}" in
--check) mode=check ;;
--update) mode=update ;;
-h | --help)
	sed -n '2,4p' "$0" | sed 's/^# //'
	exit 0
	;;
"") ;;
*)
	echo "import-graph: unknown option $1" >&2
	exit 2
	;;
esac

# layer sets LAYER to the import layer of a package: 6 cli, 5 tui and rpc, 4 client orchestration, 3 extensions, 2 core, 1 testutil
layer() {
	case "$1" in
	cli/*) LAYER=6 ;;
	library/tui* | library/rpc*) LAYER=5 ;;
	library/client* | library/import* | library/proposals*) LAYER=4 ;;
	library/extensions/*) LAYER=3 ;;
	library/core/*) LAYER=2 ;;
	library/internal/*) LAYER=1 ;;
	*) LAYER=0 ;;
	esac
}

# upward reads "importer importee" lines and prints those whose importer layer is below the importee's
upward() {
	local a b la
	while read -r a b; do
		[ -n "$a" ] || continue
		layer "$a"
		la=$LAYER
		layer "$b"
		if [ "$la" -lt "$LAYER" ]; then printf '%s %s\n' "$a" "$b"; fi
	done | sort -u
}

# pkg_lines prints the non-blank non-test line count and the non-blank test line count of a package directory
pkg_lines() {
	awk 'FNR == 1 { t = (FILENAME ~ /_test\.go$/) } /[^[:space:]]/ { if (t) tl++; else nl++ } END { print nl + 0, tl + 0 }' "$1"/*.go 2>/dev/null || echo "0 0"
}

MOD=$(go list -m)
edges=$(go list -f '{{$p:=.ImportPath}}{{range .Imports}}{{$p}} {{.}}{{"\n"}}{{end}}' ./... | grep " $MOD/" | sed "s#$MOD/##g")
pkgs=$(go list -f '{{.ImportPath}}' ./... | sed "s#$MOD/##")
up=$(printf '%s\n' "$edges" | upward)

# oversized prints "package lines" for every package over the line ceiling
oversized() {
	local p n
	for p in $pkgs; do
		n=$(pkg_lines "$p" | awk '{print $1}')
		if [ "$n" -gt "$ceiling" ]; then printf '%s %s\n' "$p" "$n"; fi
	done
}

if [ "$mode" = update ]; then
	printf '%s\n' "$up" >"$baseline"
	cat "$baseline"
	exit 0
fi

if [ "$mode" = check ]; then
	fail=0
	if [ ! -f "$baseline" ]; then
		echo "import-graph: no $baseline; run scripts/import-graph.sh --update"
		exit 1
	fi
	new=$(comm -13 <(grep -v '^[[:space:]]*$' "$baseline" | sort -u) <(printf '%s\n' "$up" | grep -v '^[[:space:]]*$'))
	if [ -n "$new" ]; then
		echo "import-graph: upward import edge not in $baseline (scripts/import-graph.sh --update to accept)"
		printf '%s\n' "$new" | sed 's/^/  /'
		fail=1
	fi
	big=$(oversized)
	if [ -n "$big" ]; then
		echo "import-graph: package over $ceiling non-test lines"
		printf '%s\n' "$big" | sed 's/^/  /'
		fail=1
	fi
	exit $fail
fi

testedges=$(go list -f '{{$p:=.ImportPath}}{{range .TestImports}}{{$p}} {{.}}{{"\n"}}{{end}}{{range .XTestImports}}{{$p}} {{.}}{{"\n"}}{{end}}' ./... | grep " $MOD/" | sed "s#$MOD/##g" | sort -u)

echo "# Import graph, $(git rev-parse --short HEAD), churn over $DAYS days"
echo
echo "## Packages"
echo
echo "| package | layer | lines | test lines | files | fan-out | fan-in | churn |"
echo "|---|--:|--:|--:|--:|--:|--:|--:|"
for p in $pkgs; do
	counts=$(pkg_lines "$p")
	lines=${counts% *}
	tlines=${counts#* }
	files=$(ls "$p"/*.go 2>/dev/null | grep -vc _test.go || true)
	out=$(printf '%s\n' "$edges" | awk -v p="$p" '$1 == p' | wc -l | tr -d ' ')
	in=$(printf '%s\n' "$edges" | awk -v p="$p" '$2 == p' | wc -l | tr -d ' ')
	churn=$(git log --since="$DAYS.days" --format=%H -- ":(glob)$p/*.go" | wc -l | tr -d ' ')
	layer "$p"
	echo "| $p | $LAYER | $lines | $tlines | $files | $out | $in | $churn |"
done | sort -t'|' -k4 -rn

echo
echo "## Upward edges (importer layer below importee layer)"
echo
printf '%s\n' "$up" | sed 's/^/- /'

echo
echo "## Test-only upward edges"
echo
comm -13 <(printf '%s\n' "$edges" | sort -u) <(printf '%s\n' "$testedges") | upward | sed 's/^/- /'

echo
echo "## Sibling edges inside core (dependency order within the layer)"
echo
printf '%s\n' "$edges" | grep '^library/core/' | grep ' library/core/' | sed 's#library/core/##g' | sort -u |
	awk '{ if ($1 != prev) { if (prev) print ""; printf "- %s ->", $1; prev = $1 } printf " %s", $2 } END { print "" }'

echo
echo "## Edges"
echo
printf '%s\n' "$edges" | sort -u |
	awk '{ if ($1 != prev) { if (prev) print ""; printf "- %s ->", $1; prev = $1 } printf " %s", $2 } END { print "" }' | sed "s#library/##g"

echo
echo "## Core packages by fan-in, with their importers"
echo
for p in $(printf '%s\n' "$pkgs" | grep '^library/core/'); do
	n=$(printf '%s\n' "$edges" | awk -v p="$p" '$2 == p' | wc -l | tr -d ' ')
	imps=$(printf '%s\n' "$edges" | awk -v p="$p" '$2 == p {print $1}' | sed 's#library/##' | sort -u | tr '\n' ' ')
	echo "$n $p: $imps"
done | sort -rn | sed 's/^[0-9]* /- /'

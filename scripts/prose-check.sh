#!/usr/bin/env bash
# prose-check.sh - count STYLE.md violations; fail when a count rises above scripts/prose-baseline.txt.
# Usage: scripts/prose-check.sh [--update | --list <rule>]   rules: emdash comment-block-go comment-block-js comment-block-css comment-block-html comment-heavy short-long flag-help error-shape
# Commit subjects over 72 characters always fail: GITSOCIAL_PUSH_RANGES (set by the hook), else @{upstream}..HEAD, else skipped.
set -o pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root" || exit 1
baseline="scripts/prose-baseline.txt"
rules="comment-block-css comment-block-go comment-block-html comment-block-js comment-heavy emdash error-shape flag-help short-long"
comment_heavy_since=2026-09-16T00:00:00 # commits before the rule are exempt
zero=0000000000000000000000000000000000000000

# the commits a rule over a range reads: the hook's ranges, else the unpushed commits, else none
ranges="${GITSOCIAL_PUSH_RANGES:-}"
if [ -z "$ranges" ] && git rev-parse -q --verify '@{upstream}' >/dev/null 2>&1; then
	ranges='@{upstream}..HEAD'
fi

# tracked files in scope, one per line
FILES=$(git ls-files | grep -vE '^specs/|^library/core/site/assets/(fonts|grammars)/|^library/core/objstore/prismcomp/|^library/core/site/assets/(prism|icons)\.js$|(^|/)testdata/|\.(golden|gz|woff2|png|svg|mp4)$|^go\.sum$' | while IFS= read -r f; do [ -f "$f" ] && printf '%s\n' "$f"; done)

# with_ext prints the in-scope files with the given extension, NUL-separated for xargs
with_ext() { printf '%s\n' "$FILES" | grep -E "\.$1\$" | tr '\n' '\0'; }

# slash_runs prints file:line for each run of more than 3 consecutive // lines, package docs excepted
slash_runs() {
	with_ext "$1" | xargs -0 awk '
		function flush() { if (n > 3 && !pkg) print file ":" start; n = 0 }
		FNR == 1 { flush() }
		/^[[:space:]]*\/\// { if (n == 0) { start = FNR; file = FILENAME; pkg = ($0 ~ /^\/\/ Package /) } n++; next }
		{ flush() }
		END { flush() }'
}

# block_runs prints file:line for each open..close comment block spanning more than 3 lines
block_runs() {
	with_ext "$1" | xargs -0 awk -v ob="$2" -v cb="$3" '
		function flush(last) { if (inb && last - start + 1 > 3) print file ":" start; inb = 0 }
		FNR == 1 { flush(prev) }
		{
			if (!inb) {
				i = index($0, ob)
				if (i && !index(substr($0, i + length(ob)), cb)) { inb = 1; start = FNR; file = FILENAME }
			} else if (index($0, cb)) { flush(FNR) }
			prev = FNR
		}
		END { flush(prev) }'
}

# error_shapes prints file:line for each error literal in non-test Go that breaks the STYLE.md error shape
error_shapes() {
	printf '%s\n' "$FILES" | grep -E '^(library|cli)/.*\.go$' | grep -v '_test\.go$' | tr '\n' '\0' |
		xargs -0 grep -HnE -A1 'fmt\.Errorf\(|errors\.New\(|PrintError\(|result\.Err(WithDetails)?(\[[^]]*\])?\(' |
		awk -v emdash="$(printf '\xe2\x80\x94')" '
		function check(loc, lit) {
			if (index(lit, emdash) || lit ~ /[();?]/ || index(lit, "\\n") || lit ~ /\. [A-Za-z]/ || tolower(lit) ~ /failed to/ || lit ~ /^[A-Z][a-z]/ || length(lit) > 100) print loc
		}
		{
			s = $0
			if (match(s, /(fmt\.Errorf|errors\.New|PrintError|result\.Err(WithDetails)?(\[[^]]*\])?)\(/)) {
				pending = ""; split($0, a, ":"); loc = a[1] ":" a[2]
				s = substr(s, RSTART + RLENGTH)
				if ($0 ~ /result\.Err/ && match(s, /"[^"]*",[[:space:]]*/)) s = substr(s, RSTART + RLENGTH)
				if (match(s, /"([^"\\]|\\.)*"/)) check(loc, substr(s, RSTART + 1, RLENGTH - 2)); else pending = loc
				next
			}
			if (pending != "" && match($0, /"([^"\\]|\\.)*"/)) check(pending, substr($0, RSTART + 1, RLENGTH - 2))
			pending = ""
		}'
}

# comment_heavy prints hash<TAB>comments/code for each commit in the range whose net new comment lines outnumber its added code lines
comment_heavy() {
	[ -n "$ranges" ] || return 0
	# shellcheck disable=SC2086
	for h in $(git log --no-merges --since="$comment_heavy_since" --format=%H $ranges 2>/dev/null); do
		git show --format= --name-only "$h" | grep -qvE '\.md$|(^|/)testdata/|\.golden$' || continue
		git show --format= --unified=0 "$h" | awk -v h="$h" '
			/^(\+\+\+|---)/ { next }
			/^[-+]/ {
				s = substr($0, 2); sub(/^[[:space:]]+/, "", s)
				if (s == "") next
				if (s ~ /^(\/\/|\/\*|\*\/|\*|<!--|-->)/) { if ($0 ~ /^\+/) c++; else r++ } else if ($0 ~ /^\+/) k++
			}
			END { if (c - r > k) print h "\t" (c - r) "/" (k + 0) }'
	done
}

# list_rule prints every offending file:line for one rule
list_rule() {
	case "$1" in
	emdash) printf '%s\n' "$FILES" | tr '\n' '\0' | xargs -0 grep -Hn -- "$(printf '\xe2\x80\x94')" | cut -d: -f1,2 ;;
	comment-block-go) slash_runs go ;;
	comment-block-js) slash_runs js; block_runs js '/*' '*/' ;;
	comment-block-css) block_runs css '/*' '*/' ;;
	comment-block-html) block_runs html '<!--' '-->' ;;
	comment-heavy) comment_heavy ;;
	short-long) grep -HnE 'Short:[[:space:]]*"' cli/gitsocial/*.go | awk -F'"' 'length($2) > 50 { split($0, a, ":"); print a[1] ":" a[2] }' ;;
	error-shape) error_shapes ;;
	flag-help)
		grep -HnE 'Flags\(\)\.(String|Bool|Int|Int64|Uint|Float64|Duration|StringSlice|StringArray|Count|Var)(Var)?P?\(' cli/gitsocial/*.go | awk '{
			s = $0; n = 0
			while (match(s, /"([^"\\]|\\.)*"/)) { last = substr(s, RSTART + 1, RLENGTH - 2); s = substr(s, RSTART + RLENGTH); n++ }
			if (n && (length(last) > 60 || index(last, "("))) { split($0, a, ":"); print a[1] ":" a[2] }
		}' ;;
	*) echo "prose-check: unknown rule $1" >&2; return 2 ;;
	esac
}

# counts prints rule<TAB>count for every rule
counts() { for r in $rules; do printf '%s\t%s\n' "$r" "$(list_rule "$r" | wc -l | tr -d ' ')"; done; }

case "${1:-}" in
--update) counts >"$baseline"; cat "$baseline"; exit 0 ;;
--list) list_rule "${2:-}"; exit $? ;;
-h | --help) sed -n '2,3p' "$0" | sed 's/^# //'; exit 0 ;;
"") ;;
*) echo "prose-check: unknown option $1" >&2; exit 2 ;;
esac

fail=0
cur=$(counts)
if [ ! -f "$baseline" ]; then
	printf '%s\n' "$cur"
	echo "prose-check: no $baseline; run scripts/prose-check.sh --update"
	exit 1
fi
while IFS=$'\t' read -r rule n; do
	b=$(awk -F'\t' -v r="$rule" '$1 == r { print $2 }' "$baseline")
	if [ -z "$b" ]; then
		echo "prose-check: $rule is missing from $baseline; run scripts/prose-check.sh --update"
		fail=1
	elif [ "$n" -gt "$b" ]; then
		echo "prose-check: $rule: $n > $b (scripts/prose-check.sh --list $rule)"
		fail=1
	fi
done <<<"$cur"

if [ -n "$ranges" ]; then
	# shellcheck disable=SC2086
	long=$(git log --no-merges --format='%h%x09%s' $ranges 2>/dev/null | awk -F'\t' 'length($2) > 72')
	if [ -n "$long" ]; then
		printf 'prose-check: commit subject over 72 characters\n%s\n' "$long"
		fail=1
	fi
fi
exit $fail

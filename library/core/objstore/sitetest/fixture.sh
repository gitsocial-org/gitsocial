#!/usr/bin/env bash
# fixture.sh - build the self-contained showcase static-site fixture.
#
# Reconstructs a thread-demo-equivalent bucket plus a tiny upstream (other-demo)
# entirely from bin/gitsocial + git + the disk-backed locals3 server, so the
# default site-test battery has a reproducible served surface with no scratchpad
# dependence. All state is isolated under <out> (its own XDG_CONFIG_HOME and
# --cache-dir); the real user config and cache are never touched. Idempotent: a
# second run is a no-op while the served manifest is present. The s3 identity
# host is the generic RFC-2606 placeholder fake.example.com; actual traffic is
# redirected to the local locals3 server by the GITSOCIAL_S3_ENDPOINT override.
set -euo pipefail

here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
repo=$(cd "$here/../../../.." && pwd)
out="${1:-$here/.fixture}"
served="$out/served"
marker="$served/thread-demo/.gitsocial/site/refs.json"
HOST=fake.example.com

if [ -f "$marker" ] && [ -d "$served/interrupted-demo" ] && [ -d "$served/extended-demo" ] && [ -d "$served/sparse-demo" ] && [ -d "$served/merged-demo" ]; then
	echo "fixture present: $served"
	exit 0
fi

bin="$repo/bin/gitsocial"
if [ ! -x "$bin" ]; then
	echo "building bin/gitsocial ..."
	(cd "$repo" && go build -o bin/gitsocial ./cli/gitsocial)
fi
locals3bin="$out/locals3bin"
go build -o "$locals3bin" "$here/../locals3"

rm -rf "$out/xdg" "$out/cache" "$served" "$out/locals3.log" \
	"$out/thread-demo" "$out/other-demo" "$out/interrupted-demo" "$out/healed-demo" \
	"$out/partial-demo" "$out/extended-demo" "$out/merged-demo"
mkdir -p "$served" "$out/xdg"
export XDG_CONFIG_HOME="$out/xdg"
cache="$out/cache"

"$locals3bin" -addr 127.0.0.1:0 -root "$served" >"$out/locals3.log" 2>&1 &
s3pid=$!
trap 'kill $s3pid 2>/dev/null || true' EXIT
port=""
for _ in $(seq 1 50); do
	port=$(sed -nE 's#.*127\.0\.0\.1:([0-9]+).*#\1#p' "$out/locals3.log" | head -1)
	[ -n "$port" ] && break
	sleep 0.1
done
[ -n "$port" ] || { echo "locals3 did not start" >&2; cat "$out/locals3.log" >&2; exit 1; }
export GITSOCIAL_S3_ENDPOINT="http://127.0.0.1:$port"
export GITSOCIAL_S3_PATH_STYLE=1
export GITSOCIAL_S3_ACCESS_KEY=dummy
export GITSOCIAL_S3_SECRET_KEY=dummy
export GITSOCIAL_S3_REGION=us-east-1
# Lower the sealed-shard size so this small fixture produces a MULTI-shard items
# and bodies index (immutable sealed shards + head + manifest), exercising the
# reader's eager-set (newest shard + head) plus on-demand older-shard loading.
# Unset in production, where the shard size stays 4000.
export GITSOCIAL_SITE_SHARD_COUNT="${GITSOCIAL_SITE_SHARD_COUNT:-4}"

# gg runs the binary against the current workspace $W and the isolated cache.
gg() { "$bin" --cache-dir "$cache" -C "$W" "$@"; }
# idof extracts the first 12-hex commit short hash from a command's output.
idof() { grep -oE '#commit:[0-9a-f]{12}' | head -1 | cut -d: -f2; }
# ident switches the workspace git author identity.
ident() { git -C "$W" config user.name "$1"; git -C "$W" config user.email "$2"; }

# ---- upstream workspace: other-demo (Grace Hopper) ----
W="$out/other-demo"
mkdir -p "$W"
git init -q -b main "$W"
ident "Grace Hopper" "grace@example.com"
printf 'other-demo\n' >"$W/README.md"
git -C "$W" add -A && git -C "$W" commit -qm "Initial commit"
gg social init >/dev/null
UP=$(gg --json social post "Original upstream idea: loose-object readers over dumb HTTP." | idof)
gg remote add "s3://$HOST/other-demo" >/dev/null
git -C "$W" push -q origin main
git -C "$W" push -q origin 'refs/heads/gitmsg/*:refs/heads/gitmsg/*'
gg site push >/dev/null

# ---- main workspace: thread-demo (Ada Lovelace) ----
W="$out/thread-demo"
mkdir -p "$W"
git init -q -b main "$W"
ident "Ada Lovelace" "ada@example.com"
printf '# thread-demo\n\nShowcase fixture.\n' >"$W/README.md"
git -C "$W" add -A && git -C "$W" commit -qm "Initial commit: README"
printf 'one\ntwo\n' >"$W/notes.txt"
git -C "$W" add -A && git -C "$W" commit -qm "Add notes"
printf 'one\ntwo\nthree\n' >"$W/notes.txt"
git -C "$W" add -A && git -C "$W" commit -qm "Extend notes"
# Source files in non-base languages, so the reader lazy-loads their grammars
# (grammars/prism-python.js, prism-rust.js) when a visitor opens them; the shell
# ships every grammar file (E1 coverage: verify_grammars.js / verify_site_features.js).
printf 'def main():\n    print("hello")\n' >"$W/hello.py"
printf 'fn main() {\n    println!("hi");\n}\n' >"$W/main.rs"
git -C "$W" add -A && git -C "$W" commit -qm "Add python and rust sources"

gg social init >/dev/null
gg pm init >/dev/null
gg review init >/dev/null
gg memo project init >/dev/null

# social: three posts, a comment thread with a reply and reply-to-reply.
P1=$(gg --json social post "Shipping the S3 static site reader this week." | idof)
gg social post "Anyone tried the new thread view yet?" >/dev/null
gg social post "Docs update landed for the ref grammar." >/dev/null
C1=$(gg --json social comment "$P1" "Congrats, this is huge!" | idof)
gg social comment "$P1" "What about generation-mode buckets?" >/dev/null
gg social comment "$P1" "Looking forward to trying it." >/dev/null
R1=$(gg --json social comment "$C1" "Thanks, appreciate it!" | idof)
gg social comment "$R1" "Seconded, well earned." >/dev/null

# social: markdown + multi-line posts (richer render coverage).
gg social post "# Release notes

**Highlights** this week:

- loose-object reader
- thread view

\`\`\`go
fmt.Println(\"ship it\")
\`\`\`" >/dev/null
gg social post "$(printf 'first line\nsecond line\nthird line')" >/dev/null

# pm: fixture issue with two cross-extension social comments.
ISSUE=$(gg --json pm issue create "Static site: thread view needs live fixture" -l "kind/task" | idof)
gg social comment "$ISSUE" "I can build the fixture this week." >/dev/null
gg social comment "$ISSUE" "Great, assign it to me." >/dev/null

# pm: an issue with a three-version edit chain (create, retitle+body, close).
ONB=$(gg --json pm issue create "Improve onboarding docs" | idof)
gg pm issue edit "$ONB" --subject "Improve onboarding and setup docs" --body "Expand the README with a quickstart and troubleshooting section." >/dev/null
gg pm issue close "$ONB" >/dev/null

# pm: milestone + sprint + linked issues + statuses.
MS=$(gg --json pm milestone create "v1.0 Launch" --due 2026-09-01 | idof)
SP=$(gg --json pm sprint create "Sprint 1: Foundations" --start 2026-07-01 --end 2026-07-14 | idof)
I1=$(gg --json pm issue create "Design the storage layout" -m "$MS" -s "$SP" -l "kind/task,status/review" | idof)
gg pm issue create "Implement the fetch loop" -m "$MS" -s "$SP" -l "kind/feature,status/in-progress" >/dev/null
gg pm issue create "Write the onboarding guide" -m "$MS" -l "kind/task" >/dev/null
gg pm issue create "Ship v1.0" -m "$MS" -l "kind/feature,status/in-progress" >/dev/null
gg pm issue close "$I1" >/dev/null

# pm: an epic with two sub-issues and a grandchild; one sub closed.
EPIC=$(gg --json pm issue create "Epic: Reader UX" -l "kind/story,status/in-progress" | idof)
KBD=$(gg --json pm issue create "Sub: keyboard navigation" --parent "$EPIC" -l "kind/task" | idof)
DARK=$(gg --json pm issue create "Sub: dark mode polish" --parent "$EPIC" -l "kind/task,status/review" | idof)
gg pm issue create "Sub-sub: focus ring tokens" --parent "$DARK" -l "kind/task" >/dev/null
gg pm issue close "$KBD" >/dev/null

# review: a PR with inline feedback from two reviewers, an approval and a
# change request; feedback is authored under distinct reviewer identities.
git -C "$W" switch -q -c feature/notes-expand
printf 'one\ntwo\nthree\nline four (added, documented)\nfive\n' >"$W/notes.txt"
git -C "$W" add -A && git -C "$W" commit -qm "Expand and edit notes"
git -C "$W" switch -q main
HEADTIP=$(git -C "$W" rev-parse feature/notes-expand | cut -c1-12)
PR=$(gg --json review pr create "Expand notes with more lines" --base '#branch:main' --head '#branch:feature/notes-expand' --reviewers bob@example.com,carol@example.com --allow-unpublished-head | idof)
ident "Bob Reviewer" "bob@example.com"
gg review feedback comment "This wording is clearer, nice." --pr "$PR" --commit "$HEADTIP" --file notes.txt --new-line 2 >/dev/null
ident "Carol Critic" "carol@example.com"
gg review feedback comment "Consider a more descriptive line here." --pr "$PR" --commit "$HEADTIP" --file notes.txt --new-line 4 --suggest >/dev/null
ident "Bob Reviewer" "bob@example.com"
gg review feedback comment "These two new lines could be a bulleted list." --pr "$PR" --commit "$HEADTIP" --file notes.txt --new-line 4 --new-line-end 5 >/dev/null
gg review feedback approve "$PR" -m "Looks good overall, thanks!" >/dev/null
ident "Carol Critic" "carol@example.com"
gg review feedback request-changes "$PR" -m "Please address the suggestion before merge." >/dev/null
ident "Ada Lovelace" "ada@example.com"

# memo: three project-tier memos with area/kind labels.
gg memo create "Cache invalidation policy" --scope project --labels "area/cache,kind/policy" >/dev/null
gg memo create "Why loose objects over dumb HTTP" --scope project --labels "area/objstore" >/dev/null
gg memo create "Effective author rule" --scope project --labels "area/protocol,kind/note" >/dev/null

# cross-repo quote of the upstream post.
gg remote add "s3://$HOST/thread-demo" >/dev/null
gg social fetch "s3://$HOST/other-demo" >/dev/null
gg social quote "s3://$HOST/other-demo#commit:$UP@gitmsg/social" "Great point from upstream, quoting for the thread." >/dev/null

# a lightweight and an annotated tag, so the site's Tags page has both shapes
# (the annotated tag must be peeled through its tag object to a commit), plus
# an earlier tag two commits back so the tag page's commits-since-previous-tag
# list and files-changed-since-previous-tag diff have a real span to render.
git -C "$W" tag v0.9 main~2
git -C "$W" tag v1.0-light main
git -C "$W" tag -a v1.0 -m "First public release" main

# publish content branches, tags, then a curated list, then the site shell.
# main + feature/notes-expand carry plain (non-gitmsg) code commits, so the site
# push below builds the single CODE items index (.gitsocial/site/items/code/,
# metadata-only, no bodies) that the reader's timeline sources code commits from
# without a per-commit loose-object walk (verify_site_features.js code-index
# assertions; the multi-branch attribution rides on main vs feature/notes-expand).
git -C "$W" push -q origin main
git -C "$W" push -q origin feature/notes-expand
git -C "$W" push -q origin 'refs/tags/*:refs/tags/*'
git -C "$W" push -q origin 'refs/heads/gitmsg/*:refs/heads/gitmsg/*'
gg social list create curated -n "Curated Follows" >/dev/null
gg social list add curated https://github.com/meshtastic/firmware -b main >/dev/null
gg social list add curated "s3://$HOST/other-demo" -b main >/dev/null
# forks: register more than the site's FORKS_CAP (10) so the config page's forks
# section exercises the "Show all N forks" expand control + filter (G5). Each
# fork ref's commit time drives the most-recently-updated ordering.
for i in $(seq 1 12); do
	gg fork add "s3://$HOST/fork-$i" >/dev/null 2>&1 || true
done

# site customization: title, accent (light + dark), and a tiny 1x1 PNG favicon,
# so the reader applies the overrides (verify_site_features.js customization checks). The favicon
# is written from a data URI directly (a minimal transparent PNG).
gg config site set title "Thread Demo" >/dev/null
gg config site set accent "#0a7" >/dev/null
gg config site set accentDark "#0dd" >/dev/null
gg config site set favicon "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+M8AAAMCAf8QO7uMAAAAAElFTkSuQmCC" >/dev/null
git -C "$W" push -q origin 'refs/gitmsg/*:refs/gitmsg/*'
gg site push >/dev/null

# ---- interrupted-push buckets (repair coverage) ----
# Both are built, then their items manifest is removed, simulating a push
# interrupted before the manifest write. interrupted-demo is served in that
# state (the reader must fall back to its bounded loose-object walk);
# healed-demo takes one more incremental push, whose repair state machine
# restores the manifest from the surviving artifacts.
for B in interrupted-demo healed-demo; do
	W="$out/$B"
	mkdir -p "$W"
	git init -q -b main "$W"
	ident "Ada Lovelace" "ada@example.com"
	printf '%s\n' "$B" >"$W/README.md"
	git -C "$W" add -A && git -C "$W" commit -qm "Initial commit"
	gg social init >/dev/null
	gg social post "First post before the interruption." >/dev/null
	gg social post "Second post before the interruption." >/dev/null
	gg remote add "s3://$HOST/$B" >/dev/null
	git -C "$W" push -q origin main
	git -C "$W" push -q origin 'refs/heads/gitmsg/*:refs/heads/gitmsg/*'
	gg site push >/dev/null
	rm -f "$served/$B/.gitsocial/site/items/social/manifest.json" \
		"$served/$B/.gitsocial/site/items/social/manifest.json.gsenc"
done
W="$out/healed-demo"
gg social post "Post after the interruption heals the artifacts." >/dev/null
git -C "$W" push -q origin 'refs/heads/gitmsg/*:refs/heads/gitmsg/*'

# ---- resumable-bootstrap buckets (cursor coverage) ----
# Both branches exceed a test-lowered per-push walk budget, so the first index
# push seals only the newest budget prefix and leaves a cursor (complete:false).
#   - partial-demo is served in that partial state: the timeline and recent
#     (light) search must work from the servable newest prefix, and the
#     "search older items" affordance must report the coverage is limited to the
#     bootstrapped prefix.
#   - extended-demo takes one more `site push`, whose backfill prepends the next
#     older segment, so its manifest covers strictly more history than partial's.
# The budget override is scoped to this block so the other fixtures (all below
# the budget) keep single-push, complete indexes. Unset in production.
export GITSOCIAL_SITE_WALK_BUDGET="${GITSOCIAL_SITE_WALK_BUDGET:-6}"
for B in partial-demo extended-demo; do
	W="$out/$B"
	mkdir -p "$W"
	git init -q -b main "$W"
	ident "Ada Lovelace" "ada@example.com"
	printf '%s\n' "$B" >"$W/README.md"
	git -C "$W" add -A && git -C "$W" commit -qm "Initial commit"
	gg social init >/dev/null
	# More posts than the budget, so one index push cannot reach the branch root.
	for i in $(seq 1 14); do
		gg social post "Post number $i in the resumable bootstrap chain." >/dev/null
	done
	gg remote add "s3://$HOST/$B" >/dev/null
	git -C "$W" push -q origin main
	git -C "$W" push -q origin 'refs/heads/gitmsg/*:refs/heads/gitmsg/*'
	gg site push >/dev/null
done
# extended-demo gets one more push, advancing its bootstrap by one segment.
W="$out/extended-demo"
gg site push >/dev/null
unset GITSOCIAL_SITE_WALK_BUDGET

# ---- sparse-demo: a repo with PM issues but NO social/review/release/memo corpus
# (only the pm extension initialized). The merged timeline and interaction-count
# load must degrade gracefully over the absent branches — no eternal "Loading…"
# (G9 regression guard: a missing per-extension corpus must not wedge the feed).
W="$out/sparse-demo"
mkdir -p "$W"
git init -q -b main "$W"
ident "Ada Lovelace" "ada@example.com"
printf 'sparse-demo\n' >"$W/README.md"
git -C "$W" add -A && git -C "$W" commit -qm "Initial commit"
gg pm init >/dev/null
gg pm issue create "Only-issue repo: no social corpus at all" -l "kind/task" >/dev/null
gg pm issue create "Second issue so the board has cards" -l "kind/feature" >/dev/null
gg remote add "s3://$HOST/sparse-demo" >/dev/null
git -C "$W" push -q origin main
git -C "$W" push -q origin 'refs/heads/gitmsg/*:refs/heads/gitmsg/*'
git -C "$W" push -q origin 'refs/gitmsg/*:refs/gitmsg/*'
gg site push >/dev/null

# ---- merged-demo: a MERGED PR whose head branch is deleted and never published,
# so its head tip survives only as the merge commit's SECOND parent on main. The
# site can't resolve base-tip/head-tip here (the head branch ref is absent), so
# this is the shape only prDiffSection's merge-base..merge-head path can render.
W="$out/merged-demo"
mkdir -p "$W"
git init -q -b main "$W"
ident "Ada Lovelace" "ada@example.com"
printf 'main\n' >"$W/README.md"
git -C "$W" add -A && git -C "$W" commit -qm "Initial commit"
gg review init >/dev/null
gg remote add "s3://$HOST/merged-demo" >/dev/null
# feature branch adds CHANGELOG.md; main then advances so the merge is a true
# merge commit (head tip = merge^2, reachable from no published branch ref).
git -C "$W" switch -q -c feature/changelog
printf '# Changelog\n\n- first entry\n- second entry\n' >"$W/CHANGELOG.md"
git -C "$W" add -A && git -C "$W" commit -qm "Add CHANGELOG"
git -C "$W" switch -q main
printf 'main\nmore\n' >"$W/README.md"
git -C "$W" add -A && git -C "$W" commit -qm "Touch README on main"
MPR=$(gg --json review pr create "Add a changelog" --base '#branch:main' --head '#branch:feature/changelog' --allow-unpublished-head | idof)
gg review pr merge "$MPR" >/dev/null
git -C "$W" branch -q -D feature/changelog
git -C "$W" push -q origin main
git -C "$W" push -q origin 'refs/heads/gitmsg/*:refs/heads/gitmsg/*'
git -C "$W" push -q origin 'refs/gitmsg/*:refs/gitmsg/*'
gg site push >/dev/null

echo "fixture built: $served"
echo "  buckets: thread-demo, other-demo, interrupted-demo, healed-demo, partial-demo, extended-demo, sparse-demo, merged-demo"

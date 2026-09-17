#!/usr/bin/env bash
# shapes.sh - build the six repo-shape fixture buckets the screenshot goldens read.
# Each bucket is generated here by git and bin/gitsocial and pushed to the
# disk-backed locals3 server, with all state isolated under <out>. Every commit
# takes a fixed date off one clock, so a build's shas and dates never change.
set -euo pipefail

here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
repo=$(cd "$here/../../../.." && pwd)
out="${1:-$here/.shapes}"
served="$out/served"
stampfile="$out/.stamp"
. "$here/fixture-lib.sh"

# EPOCH is the fixed commit clock's start, 2026-01-01 00:00:00 UTC.
EPOCH=1767225600
# FROZEN is the instant shots.js freezes Date.now() at, three days past every
# commit, so the app renders one relative-time bucket on every fixture.
FROZEN=$((EPOCH + 3 * 86400 + 4 * 3600))

# A stale stamp would let the goldens be compared against pages a previous
# binary wrote.
stamp=$(fixture_stamp "$here/shapes.sh")

if [ "$(cat "$stampfile" 2>/dev/null)" = "$stamp" ]; then
	echo "shapes present: $served"
	exit 0
fi
rm -rf "${out:?}"/* "$stampfile"
mkdir -p "$served" "$out/xdg"

fixture_build_bins
fixture_start_locals3

tick=0
# stamp_clock advances the fixed commit clock one minute and exports it to git.
stamp_clock() {
	tick=$((tick + 1))
	export GIT_AUTHOR_DATE="$((EPOCH + tick * 60)) +0000"
	export GIT_COMMITTER_DATE="$GIT_AUTHOR_DATE"
}
# gg runs the binary against the current workspace $W on the next clock tick.
gg() { stamp_clock; "$bin" --cache-dir "$cache" -C "$W" "$@"; }
# gcommit stages the workspace and commits it on the next clock tick.
gcommit() { stamp_clock; git -C "$W" add -A; git -C "$W" commit -qm "$1"; }
# publish turns the site and the page layer on and pushes the bucket: the
# default branch, any further refspecs given, the state refs, then the site.
publish() {
	local bucket=$1 branch=$2
	shift 2
	gg remote add "s3://$HOST/$bucket" >/dev/null
	gg config site set publish true >/dev/null
	gg config site set pages true >/dev/null
	gg config site set url "http://127.0.0.1:$port/$bucket/" >/dev/null
	git -C "$W" push -q origin "$branch"
	for refspec in "$@"; do git -C "$W" push -q origin "$refspec"; done
	git -C "$W" push -q origin 'refs/heads/gitmsg/*:refs/heads/gitmsg/*' 2>/dev/null || true
	git -C "$W" push -q origin 'refs/gitmsg/*:refs/gitmsg/*'
	gg push --site-only >/dev/null
}
# newrepo starts a workspace on the given default branch.
newrepo() {
	W="$out/$1"
	mkdir -p "$W"
	git init -q -b "$2" "$W"
	ident "Ada Lovelace" "ada@example.com"
}

# ---- src-repo: a source tree on a non-main default branch, no docs directory.
# Its only .txt files sit under fixture directories the document rule skips, so
# LICENSE is the one file page. release and memo stay uninitialized, so their
# sections carry the empty state.
newrepo src-repo trunk
printf '<div align="center">\n  <img src="https://img.example.com/logo.svg" alt="logo">\n  <h1>src-repo</h1>\n</div>\n\nA source tree with no documentation directory.\n\n## Building\n\n- `go build ./...`\n- `go test ./...`\n' >"$W/README.md"
printf 'MIT License\n\nPermission is hereby granted, free of charge, to any person obtaining a copy\nof this software and associated documentation files.\n' >"$W/LICENSE"
mkdir -p "$W/src" "$W/cmd" "$W/testdata" "$W/fixtures"
printf 'package main\n\nfunc main() {\n\tprintln("src-repo")\n}\n' >"$W/src/main.go"
printf 'package main\n\nfunc clamp(n, lo, hi int) int {\n\tif n < lo {\n\t\treturn lo\n\t}\n\tif n > hi {\n\t\treturn hi\n\t}\n\treturn n\n}\n' >"$W/src/util.go"
printf 'package cmd\n\nvar Name = "src"\n' >"$W/cmd/root.go"
printf 'golden output row one\ngolden output row two\n' >"$W/testdata/golden.txt"
printf 'case one\ncase two\n' >"$W/fixtures/case1.txt"
gcommit "Initial commit: sources and license"
printf 'package main\n\nvar Version = "0.2.0"\n' >"$W/src/version.go"
gcommit "Add a version constant"
gg social init >/dev/null
gg pm init >/dev/null
gg review init >/dev/null
gg social post "The tree carries sources only, no docs directory." >/dev/null
gg social post "Fixture text files stay out of the page layer." >/dev/null
CLOSED=$(gg --json pm issue create "Clamp rejects an inverted range" -l "kind/bug" | grep -oE '#commit:[0-9a-f]{12}' | head -1 | cut -d: -f2)
gg pm issue create "Document the build flags" -l "kind/task" >/dev/null
gg pm issue close "$CLOSED" >/dev/null
publish src-repo trunk

# ---- docs-repo: an .mdx documentation site. The page layer publishes the .mdx
# files as prose and the app renders them as source, so the goldens carry both
# halves of that divergence.
newrepo docs-repo main
printf '# docs-repo\n\nA documentation site written in MDX.\n' >"$W/README.md"
mkdir -p "$W/docs"
printf 'import { Callout } from "../components/Callout"\n\n# Introduction\n\nThe reader opens this page before anything else, so it states what the project does and what it does not. A bucket serves the whole site as static objects, which means every page has to read on its own without a server rendering it first. The sections below walk through installation, the first push and the layout of the published keys, in the order a new reader meets them.\n\n<Callout>Read the quick start before the reference.</Callout>\n\n## Installing\n\nDownload a release binary and put it on the path. No daemon runs and no database is created.\n\n## Publishing\n\nOne command builds the site and uploads it. The keys it writes are listed in the reference.\n' >"$W/docs/intro.mdx"
printf 'import Layout from "../layout"\n\n# Guide\n\nThis page holds the walkthrough. Each step names the command to type and the output to expect, and nothing here explains why the design is what it is. The walkthrough assumes the binary is installed and a bucket exists, and it ends with a published site the reader can open in a browser without any further setup or configuration.\n\n## First push\n\nRun the publish command. The site appears under the prefix.\n' >"$W/docs/guide.mdx"
printf '# API\n\nThe reference tables list every key, every flag and every environment variable the tool reads. A row is one line. The tables are generated from the source, so a value here and a value in the binary cannot drift apart without the generator noticing it first and failing the build.\n\n| Key | Meaning |\n|---|---|\n| `title` | the site title |\n| `url` | the public base |\n' >"$W/docs/api.md"
gcommit "Initial commit: the MDX documentation site"
gg social init >/dev/null
gg social post "The docs site is written in MDX." >/dev/null
publish docs-repo main

# ---- empty-repo: one commit, no README, no extensions. The front page has no
# README block and no placeholder standing in for one.
newrepo empty-repo main
printf 'hello\n' >"$W/hello.txt"
gcommit "Initial commit"
publish empty-repo main

# ---- code-only-repo: code, branches and tags, and no gitmsg branches at all.
# Every sidebar section stays and every list shows its own empty sentence.
newrepo code-only-repo main
printf '# code-only-repo\n\nNo GitMsg data lives here, only code.\n' >"$W/README.md"
printf 'package api\n\nfunc Ping() string { return "pong" }\n' >"$W/api.go"
gcommit "Initial commit: README and api"
printf 'package api\n\nfunc Ping() string { return "pong" }\n\nfunc Version() string { return "1.0" }\n' >"$W/api.go"
gcommit "Add a version helper"
git -C "$W" switch -q -c topic/retry
printf 'package api\n\nvar Retries = 3\n' >"$W/retry.go"
gcommit "Add a retry budget"
git -C "$W" switch -q main
stamp_clock
git -C "$W" tag -a v1.0 -m "First tag" main
publish code-only-repo main topic/retry 'refs/tags/*:refs/tags/*'

# ---- big-tree-repo: 6,000 generated files, 5,900 of them in one directory, so
# the tree caps its rows and offers the tree search instead of expanding.
newrepo big-tree-repo main
printf '# big-tree-repo\n\nSix thousand generated files.\n' >"$W/README.md"
mkdir -p "$W/nodes"
awk -v d="$W/nodes" 'BEGIN { for (i = 1; i <= 5900; i++) { f = sprintf("%s/node-%04d.txt", d, i); printf "node %04d\ngenerated row for the large-tree fixture\n", i > f; close(f) } }'
for g in $(seq 1 10); do
	mkdir -p "$W/group-$g"
	awk -v d="$W/group-$g" -v g="$g" 'BEGIN { for (i = 1; i <= 9; i++) { f = sprintf("%s/leaf-%02d.txt", d, i); printf "group %s leaf %02d\n", g, i > f; close(f) } }'
done
gcommit "Initial commit: the generated tree"
gg social init >/dev/null
gg social post "Six thousand files, one directory carries most of them." >/dev/null
publish big-tree-repo main

# ---- binary-repo: an image, a binary blob, an LFS pointer, a submodule and a
# symlink, the five objects a blob view has to label instead of rendering.
newrepo sub-lib main
printf 'library\n' >"$W/lib.txt"
gcommit "Initial commit"
sublib=$(git -C "$W" rev-parse HEAD)

newrepo binary-repo main
printf '# binary-repo\n\nObjects a blob view labels rather than renders.\n' >"$W/README.md"
openssl base64 -d -A >"$W/logo.png" <<'PNG'
iVBORw0KGgoAAAANSUhEUgAAAGAAAABgCAIAAABt+uBvAAACqElEQVR4nOyZMWvcQBCFR6vt0qY2XH21IUXAqNYfUBcO1Kk2uDNX6w+oEDhXqw6kM1frB6Q0XO3WpEmRSiCUtWdg15vd8VM1gtPHavXeHm/Gfr76Stz16dKWRCWRIXqt+HU1Lj9/9aov7foRZzEIOL2Ls7ltBJz50m6e+rewy4/fvMxSeF4ZcmQbVC6F55UhBwqCgmIoKOKCUuPYUFLUyoGCgigo4oJS49hQUtTKgYKCKCjiglLj2OeiNa4Msi5eiM81+4LJa4boh4DTFXxeuxVwpqJ1PrsurgUcK/oaf5bC72vkyMEGsRsU8R8hR45MQRToi2XIwQaxG6TUGmSgoEgKUvpisBgsBotlYbFCNBfbCeZiT3yuqXd8zhoEnN7F2dw2As684/Ma0jyX5pfCF6SVY2P+I+TIgYKCKCjiglLj2FBS1MqBgoIoKOKCUuPYUFLUyoGCoCBPBT1XrXFlkHXx8sjnmn0lmIsJOF3F57VbAWeqWuez6+L6cUSa903z6EmjJ42eNHrS6EmjJ51vTxoWg8VgMVjsf1pMNhc7COZiD3yuqQ98zhoEnN7F2dw2As584PMa2h1od3i2O5bCd6e1cmAxWAwWg8VgMVgMFoPFPqrFCnr4bt4MIyXR73s+1+yP/Fzsp4DTHfm8difgTEc+Z325H9HuQLvjndsdUBCrIKUvBovBYqlYTOmXxyGNQxqHNA5pDYe0bC52EszFvvG5pj7xOWsQcHoXZ3PbCDjzKdRcTKl9yIRqdyi1D5XYoEgbFFHSqXGgoCAKirig1Dg2lBS1cqCgIAqKuKDUODaUFLVyoCBGQQU9CeZiN3yu2Z8FczEBpzvzee1OwJnOgrnYzYh2B9od79zuwAaxG6TUGmSgoEgKUvpisBgsBovBYrDYB7DY3wEAOfJorU1KTYsAAAAASUVORK5CYII=
PNG
head -c 4096 /dev/zero >"$W/data.bin"
printf 'binary fixture payload\n' >>"$W/data.bin"
head -c 61440 /dev/zero >>"$W/data.bin"
mkdir -p "$W/assets"
printf 'version https://git-lfs.github.com/spec/v1\noid sha256:4d7a214614ab2935c943f9e0ff69d22eadbb8f32b1258daaa5e2ca24d17e2393\nsize 12345678\n' >"$W/assets/model.bin"
printf '*.bin filter=lfs diff=lfs merge=lfs -text\n' >"$W/.gitattributes"
ln -s README.md "$W/LINK.md"
printf '[submodule "vendor/lib"]\n\tpath = vendor/lib\n\turl = https://example.com/sub-lib.git\n' >"$W/.gitmodules"
stamp_clock
git -C "$W" add -A
git -C "$W" update-index --add --cacheinfo "160000,$sublib,vendor/lib"
git -C "$W" commit -qm "Initial commit: binary objects"
gg social init >/dev/null
gg social post "The blob view has five object kinds to label here." >/dev/null
publish binary-repo main

printf '%s\n' "$FROZEN" >"$out/now"
printf '%s\n' "$stamp" >"$stampfile"

echo "shapes built: $served"
echo "  buckets: src-repo, docs-repo, empty-repo, code-only-repo, big-tree-repo, binary-repo"

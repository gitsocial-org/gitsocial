#!/usr/bin/env bash
# scripts_gen_grammars.sh - regenerate site/grammars/prism-<lang>.js from the
# vendored PrismJS 1.30.0 minified components under prismcomp/. Committed
# generated output: run only when bumping the Prism version (update prismcomp/
# first, from the same cdnjs 1.30.0 build that produced site/prism.js) or adding
# a language.
#
#   bash library/core/objstore/scripts_gen_grammars.sh
#
# Each grammar ships as its own file so the reader lazy-loads only the languages
# a visitor actually opens (gs-render.js ensureGrammar); base grammars already in
# site/prism.js (go/js/ts/json/yaml/bash/markdown/markup/css/diff/clike) are not
# emitted here. Dependency chains (e.g. cpp->c) are ordered by the reader.
set -euo pipefail
here="$(cd "$(dirname "$0")" && pwd)"
src="$here/prismcomp"
dst="$here/site/grammars"
mkdir -p "$dst"
for f in "$src"/prism-*.min.js; do
	base="$(basename "$f" .min.js)"
	out="$dst/${base}.js"
	{
		printf '// %s.js - PrismJS 1.30.0 grammar component, lazily loaded by the static site.\n' "$base"
		printf '// Official minified component (cdnjs, MIT, github.com/PrismJS/prism v1.30.0):\n'
		printf '//   https://cdnjs.cloudflare.com/ajax/libs/prism/1.30.0/components/%s.min.js\n' "$base"
		printf '// Loaded on demand by gs-render.js (ensureGrammar) into window.Prism.languages;\n'
		printf '// dependency chains (e.g. cpp->c) are ordered by the reader before this runs.\n'
		cat "$f"
		printf '\n'
	} >"$out"
done
echo "generated $(ls "$dst"/prism-*.js | wc -l | tr -d ' ') grammar files in $dst"

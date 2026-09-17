#!/usr/bin/env bash
# site-coverage.sh - per-file function coverage of the browser reader assets.
#
# Runs the browser battery under NODE_V8_COVERAGE and aggregates the V8 ranges
# per asset file. A function counts as covered when its own range was entered at
# least once in any suite process.
#
# Usage:
#   scripts/site-coverage.sh                 # the table
#   scripts/site-coverage.sh --list <file>   # the uncovered function names of one asset
#   GS_JSCOV=<dir> scripts/site-coverage.sh  # report an existing profile directory, running nothing
set -o pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
assets="$root/library/core/site/assets"

list=""
if [ "$1" = "--list" ]; then
  list="$2"
  if [ -z "$list" ]; then echo "site-coverage.sh: --list needs a file name" >&2; exit 2; fi
elif [ -n "$1" ]; then
  sed -n '2,11p' "$0" | sed 's/^# \{0,1\}//'
  exit 0
fi

covdir="$GS_JSCOV"
if [ -z "$covdir" ]; then
  covdir="$root/.test-artifacts/jscov"
  rm -rf "$covdir"
  mkdir -p "$covdir"
  NODE_V8_COVERAGE="$covdir" node "$root/library/core/site/sitetest/runner.js" >"$covdir/battery.log" 2>&1
  status=$?
  if [ $status -ne 0 ]; then
    echo "battery failed, see $covdir/battery.log" >&2
    tail -40 "$covdir/battery.log" >&2
    exit $status
  fi
fi

node -e '
const fs = require("fs"), path = require("path");
const covdir = process.argv[1], assets = process.argv[2], only = process.argv[3] || "";
const perFile = new Map();
for (const name of fs.readdirSync(covdir)) {
  if (!name.endsWith(".json")) continue;
  let doc;
  try { doc = JSON.parse(fs.readFileSync(path.join(covdir, name), "utf8")); } catch (e) { continue; }
  for (const script of doc.result || []) {
    if (!script.url || !script.url.startsWith("file://")) continue;
    const file = path.basename(script.url);
    if (path.dirname(script.url.slice(7)) !== assets) continue;
    if (!perFile.has(file)) perFile.set(file, new Map());
    const fns = perFile.get(file);
    for (const fn of script.functions || []) {
      const r = fn.ranges && fn.ranges[0];
      if (!r) continue;
      const key = r.startOffset + ":" + r.endOffset;
      const prev = fns.get(key);
      const entry = { name: fn.functionName || "(anonymous)", start: r.startOffset, count: r.count };
      if (!prev || prev.count < entry.count) fns.set(key, entry);
    }
  }
}
if (!perFile.size) { console.error("no coverage data in " + covdir); process.exit(1); }
if (only) {
  const fns = perFile.get(only);
  if (!fns) { console.error("no coverage data for " + only); process.exit(1); }
  const src = fs.readFileSync(path.join(assets, only), "utf8");
  const lineOf = (off) => src.slice(0, off).split("\n").length;
  const cold = [...fns.values()].filter((f) => f.count === 0).sort((a, b) => a.start - b.start);
  for (const f of cold) console.log(String(lineOf(f.start)).padStart(5) + "  " + f.name);
  console.log("\n" + cold.length + " uncovered of " + fns.size + " in " + only);
  process.exit(0);
}
console.log("file".padEnd(16) + "covered  total  functions");
for (const file of [...perFile.keys()].sort()) {
  const fns = [...perFile.get(file).values()];
  const hit = fns.filter((f) => f.count > 0).length;
  const pct = fns.length ? (100 * hit / fns.length).toFixed(1) : "0.0";
  console.log(file.padEnd(16) + String(hit).padStart(7) + String(fns.length).padStart(7) + "  " + pct + "%");
}
' "$covdir" "$assets" "$list"

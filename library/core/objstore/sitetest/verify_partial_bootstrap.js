// verify_partial_bootstrap.js - the resumable-bootstrap contract over
// the fixture's two cursor buckets (both branches exceed a test-lowered per-push
// walk budget, so one index push cannot reach the branch root):
//   - partial-demo: served after ONE index push. Its manifest is a valid
//     version-4 newest-first prefix but incomplete (complete:false), a cursor is
//     present, the timeline + recent (light) search still work from the eager
//     set, and the "search older items" affordance reports the coverage is
//     limited to the bootstrapped prefix (corpus.partial).
//   - extended-demo: same start, then one more `site push` whose backfill
//     prepends the next older segment, so its manifest lists strictly more shards
//     / covers strictly more commits than partial-demo, still newest-first valid.
const GS = require("../site/gs-core.js");
const ORIGIN = process.env.GS_SITE_ORIGIN || "http://localhost:8000";
let pass = 0, fail = 0;
const ok = (n, c, e) => { (c ? pass++ : fail++); console.log((c ? "PASS " : "FAIL ") + n + (!c && e ? " :: " + e : "")); };

// fetchJSON fetches a bucket-relative key; null on 404. Node's fetch decodes the
// Content-Encoding: br body transparently, so text() is JSON.
async function fetchJSON(base, key) {
  const res = await fetch(base + key);
  if (res.status !== 200) return null;
  return JSON.parse(await res.text());
}

// manifestCoverage sums a manifest's shard members plus its head — the number of
// commits the (partial) index covers.
function manifestCoverage(m) {
  const shards = (m.shards || []).reduce((n, s) => n + (s.count || 0), 0);
  return shards + ((m.head && m.head.count) || 0);
}

// prefixContiguous asserts the shard list is a valid newest-first prefix: every
// shard has a nonempty endTip and the tip resolves (a served manifest at a real
// branch tip). It does not re-derive the chain (the Go tests do), only that the
// served shape is coherent for the reader.
function prefixContiguous(m) {
  if (!Array.isArray(m.shards) || !m.shards.length) return false;
  return m.shards.every((s) => s.count > 0 && /^[0-9a-f]{40}$/.test(s.endTip || ""));
}

async function checkPartialServable(base, label) {
  const manifest = await fetchJSON(base, ".gitsocial/site/items/social/manifest.json");
  ok(label + ": items manifest present (v4)", manifest && manifest.version === 4, JSON.stringify(manifest && { v: manifest.version }));
  ok(label + ": manifest incomplete (bootstrap in progress)", manifest && manifest.complete === false, "complete=" + (manifest && manifest.complete));
  ok(label + ": manifest is a valid newest-first prefix", manifest && prefixContiguous(manifest));
  const cursor = await fetchJSON(base, ".gitsocial/site/items/social/cursor.json");
  ok(label + ": a bootstrap cursor is present", cursor && cursor.version === 4 && /^[0-9a-f]{40}$/.test(cursor.oldestIndexed || ""), JSON.stringify(cursor && { v: cursor.version }));
  const refs = await fetchJSON(base, ".gitsocial/site/refs.json");
  const tip = refs && refs["refs/heads/gitmsg/social"];
  ok(label + ": manifest tip = branch tip", manifest && tip && manifest.tip === tip, "manifest=" + (manifest && manifest.tip) + " ref=" + tip);
  const bodies = await fetchJSON(base, ".gitsocial/site/bodies/social/manifest.json");
  ok(label + ": bodies manifest lockstepped + incomplete", bodies && bodies.tip === (manifest && manifest.tip) && bodies.complete === false);

  // The reader loads only the eager set (newest shard + head) and reports the
  // index as incomplete, so recent items resolve without any older-shard fetch.
  const ctx = GS.newContext(base);
  const idx = await GS.loadItemsIndex(ctx, "social");
  ok(label + ": loadItemsIndex returns an incomplete v4 index", idx && idx.version === 4 && idx.complete === false, JSON.stringify(idx && { v: idx.version, c: idx.complete }));
  ok(label + ": eager set carries the recent items", idx && idx.items.length > 0, "eager=" + (idx && idx.items.length));
  const items = await GS.loadExtItems(ctx, "social");
  ok(label + ": timeline renders recent posts from the prefix", items.length > 0, "items=" + items.length);
  const subjects = items.map((i) => (i.content || "").split("\n")[0]);
  ok(label + ": recent light search matches an indexed subject", subjects.some((s) => s.includes("resumable bootstrap chain")), JSON.stringify(subjects.slice(0, 2)));

  return { manifest, coverage: manifest ? manifestCoverage(manifest) : 0 };
}

async function main() {
  const partial = await checkPartialServable(ORIGIN + "/partial-demo/", "partial");

  // The "search older items" affordance must report limited coverage (partial):
  // buildSearchCorpus sets corpus.partial when a loaded index is incomplete.
  {
    const ctx = GS.newContext(ORIGIN + "/partial-demo/");
    const corpus = await GS.loadSearchWindow(ctx, false, false, false);
    ok("partial: search corpus flags an incomplete (partial) prefix", corpus && corpus.partial === true, JSON.stringify(corpus && { partial: corpus.partial, hasOlder: corpus.hasOlder }));
  }

  const extended = await checkPartialServable(ORIGIN + "/extended-demo/", "extended");

  // The extra backfill push on extended-demo covers strictly more history.
  ok("extended: coverage exceeds partial's (backfill prepended a segment)",
    extended.coverage > partial.coverage,
    "extended=" + extended.coverage + " partial=" + partial.coverage);
  ok("extended: has more sealed shards than partial",
    (extended.manifest.shards || []).length > (partial.manifest.shards || []).length,
    "extended=" + (extended.manifest.shards || []).length + " partial=" + (partial.manifest.shards || []).length);

  console.log("\n" + pass + " passed, " + fail + " failed");
  process.exit(fail ? 1 : 0);
}
main().catch((e) => { console.error(e); process.exit(1); });

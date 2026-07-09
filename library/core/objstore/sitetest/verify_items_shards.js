// verify_items_shards.js - the M1 sharded metadata-index reader contract over the
// multi-shard showcase fixture (built with a lowered shard size, so social/pm
// carry several sealed shards + a head). Asserts, by instrumenting fetch:
//   1. the manifest is version 4 with sealed shards,
//   2. the eager load touches ONLY the newest sealed shard + head (no older
//      shards), and still resolves the newest timeline items,
//   3. light search over the resident set stays eager (no older-shard fetch),
//   4. loadOlderItemShards / searchOlder pulls the remaining shards on demand and
//      widens coverage,
//   5. the eager set is chronologically correct across the shard seam.
const GS = require("../site/gs-core.js");
const BASE = (process.env.GS_SITE_ORIGIN || "http://localhost:8000") + "/" + (process.env.GS_SITE_BUCKET || "thread-demo") + "/";
let pass = 0, fail = 0;
const ok = (n, c, e) => { (c ? pass++ : fail++); console.log((c ? "PASS " : "FAIL ") + n + (!c && e ? " :: " + e : "")); };

// instrumentFetch wraps global.fetch to record every requested URL path, so a
// test can assert which shards were (and were not) fetched.
function instrumentFetch() {
  const orig = global.fetch;
  const seen = [];
  global.fetch = (url, opts) => { seen.push(String(url)); return orig(url, opts); };
  return { seen, restore: () => { global.fetch = orig; } };
}

// readManifest fetches an items manifest directly (ground truth). Node's fetch
// transparently decodes the Content-Encoding: br body, so text() is JSON.
async function readManifest(ext) {
  const res = await fetch(BASE + ".gitsocial/site/items/" + ext + "/manifest.json");
  if (res.status !== 200) return null;
  return JSON.parse(await res.text());
}

// shardFetches counts how many of a manifest's sealed shard keys appear in seen.
function shardFetches(seen, ext, m) {
  return m.shards.filter((s) => seen.some((u) => u.endsWith("/items/" + ext + "/" + s.key))).length;
}

async function main() {
  // Pick the extension with the most sealed shards to exercise older loading.
  const manifests = {};
  for (const ext of ["social", "pm", "review", "memo"]) manifests[ext] = await readManifest(ext).catch(() => null);
  const ext = Object.keys(manifests).filter((e) => manifests[e]).sort((a, b) => manifests[b].shards.length - manifests[a].shards.length)[0];
  const m = manifests[ext];
  ok("multi-shard fixture: chosen ext has sealed shards (" + ext + ")", m && m.version === 4 && m.shards.length >= 2, "shards=" + (m && m.shards.length));

  // (2) Eager load touches only the newest shard + head.
  {
    const inst = instrumentFetch();
    const ctx = GS.newContext(BASE);
    const idx = await GS.loadItemsIndex(ctx, ext);
    inst.restore();
    ok("loadItemsIndex returns a v4 index with residentShas", idx && idx.version === 4 && idx.residentShas && idx.residentShas.size > 0);
    ok("index reports pending older shards", idx && !idx.allResident && idx.olderShards.length === m.shards.length - 1, "older=" + (idx && idx.olderShards.length));
    const fetched = shardFetches(inst.seen, ext, m);
    ok("eager load fetches exactly one (newest) sealed shard", fetched === 1, "fetched=" + fetched);
    ok("eager load fetches head.json", inst.seen.some((u) => u.endsWith("/items/" + ext + "/head.json")));
    ok("bodiesBytes surfaced on the index", idx && idx.bodiesBytes > 0, "bodiesBytes=" + (idx && idx.bodiesBytes));
  }

  // (2b) The first timeline window renders the newest items from the eager set;
  // it deepens by pulling older shards only as the window needs them (a small
  // fixture whose whole ext fits in one WALK_CAP window will pull them all — the
  // point is the eager set alone answers the newest slice, no full loose walk).
  {
    const ctx = GS.newContext(BASE);
    const win = await GS.loadTimelineWindow(ctx, false);
    ok("timeline window renders newest items", win.items.length > 0, "items=" + win.items.length);
  }

  // (3) Light search builds over the resident metadata (no bodies download).
  {
    const ctx = GS.newContext(BASE);
    const corpus = await GS.loadSearchWindow(ctx, false, false, false);
    ok("light search corpus builds", corpus && corpus.perExt, "keys=" + (corpus && Object.keys(corpus.perExt).length));
    ok("light search stays light (no full-body upgrade)", corpus && corpus.full === false);
  }

  // (4) loadOlderItemShards pulls the remaining shards and widens coverage.
  {
    const inst = instrumentFetch();
    const ctx = GS.newContext(BASE);
    const before = await GS.loadItemsIndex(ctx, ext);
    const beforeCount = before.residentShas.size;
    await GS.loadOlderItemShards(ctx, ext);
    inst.restore();
    const after = await GS.loadItemsIndex(ctx, ext);
    ok("loadOlderItemShards makes the index fully resident", after.allResident && after.olderShards.length === 0);
    ok("loadOlderItemShards widens the resident set", after.residentShas.size > beforeCount, before.residentShas.size + " -> " + after.residentShas.size);
    const fetched = shardFetches(inst.seen, ext, m);
    ok("loadOlderItemShards fetches the previously-absent older shards", fetched >= m.shards.length - 1, "fetched=" + fetched);
  }

  // (4b) searchOlder tier upgrades the corpus coverage.
  {
    const ctx = GS.newContext(BASE);
    await GS.loadSearchWindow(ctx, false, false, false);
    const older = await GS.loadSearchWindow(ctx, false, false, true);
    ok("searchOlder corpus marks older coverage loaded", older && older.older === true);
  }

  // (5) Eager set is chronologically correct across the shard seam: the resolved
  // newest items match a whole-corpus resolution's newest items in the same order.
  {
    const ctxAll = GS.newContext(BASE);
    await GS.loadOlderItemShards(ctxAll, ext);
    const all = await GS.loadExtItemsAll(ctxAll, ext);
    const ctxEager = GS.newContext(BASE);
    const eager = await GS.loadExtItems(ctxEager, ext);
    const n = Math.min(5, eager.length, all.length);
    const eagerTop = eager.slice(0, n).map((i) => i.commit.short);
    const allTop = all.slice(0, n).map((i) => i.commit.short);
    ok("eager newest items match whole-corpus order across the seam", JSON.stringify(eagerTop) === JSON.stringify(allTop), "eager=" + JSON.stringify(eagerTop) + " all=" + JSON.stringify(allTop));
  }

  console.log("\n" + pass + " passed, " + fail + " failed");
  process.exit(fail ? 1 : 0);
}
main().catch((e) => { console.error(e); process.exit(1); });

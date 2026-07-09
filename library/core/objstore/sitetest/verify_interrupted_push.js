// verify_interrupted_push.js - the interrupted-push contract over
// the fixture's two repair buckets:
//   - interrupted-demo: its items manifest was removed after the push (a push
//     interrupted before the manifest write). The manifest must 404, and the
//     site must still render via the reader's bounded loose-object walk.
//   - healed-demo: same interruption, followed by one more incremental push.
//     The push helper's repair state machine must have restored a valid
//     version-4 manifest at the branch tip, and the site renders from it.
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

async function main() {
  // --- interrupted-demo: manifest gone, loose walk still serves the site ---
  {
    const base = ORIGIN + "/interrupted-demo/";
    const manifest = await fetchJSON(base, ".gitsocial/site/items/social/manifest.json");
    ok("interrupted: items manifest is absent (404)", manifest === null);
    const ctx = GS.newContext(base);
    const idx = await GS.loadItemsIndex(ctx, "social");
    ok("interrupted: loadItemsIndex reports no index", idx === null);
    const items = await GS.loadExtItems(ctx, "social");
    ok("interrupted: social items render via the loose walk", items.length >= 2, "items=" + items.length);
    const subjects = items.map((i) => (i.content || "").split("\n")[0]);
    ok("interrupted: walked content is intact", subjects.some((s) => s.includes("before the interruption")), JSON.stringify(subjects));
  }

  // --- healed-demo: the next push restored the manifest ---
  {
    const base = ORIGIN + "/healed-demo/";
    const manifest = await fetchJSON(base, ".gitsocial/site/items/social/manifest.json");
    ok("healed: items manifest restored", manifest !== null && manifest.version === 4, JSON.stringify(manifest && { version: manifest.version }));
    const refs = await fetchJSON(base, ".gitsocial/site/refs.json");
    const tip = refs && refs["refs/heads/gitmsg/social"];
    ok("healed: manifest tip matches the branch tip", manifest && tip && manifest.tip === tip, "manifest.tip=" + (manifest && manifest.tip) + " ref=" + tip);
    const bodies = await fetchJSON(base, ".gitsocial/site/bodies/social/manifest.json");
    ok("healed: bodies manifest lockstepped", bodies && bodies.tip === manifest.tip);
    const ctx = GS.newContext(base);
    const idx = await GS.loadItemsIndex(ctx, "social");
    ok("healed: loadItemsIndex returns the v4 index", idx && idx.version === 4);
    const items = await GS.loadExtItems(ctx, "social");
    ok("healed: all posts render, including the healing push", items.length >= 3, "items=" + items.length);
    const subjects = items.map((i) => (i.content || "").split("\n")[0]);
    ok("healed: the post-interruption item is indexed", subjects.some((s) => s.includes("heals the artifacts")), JSON.stringify(subjects));
  }

  console.log("\n" + pass + " passed, " + fail + " failed");
  process.exit(fail ? 1 : 0);
}
main().catch((e) => { console.error(e); process.exit(1); });

// verify_packfiles.js - the browser pack reader against packed-demo, a bucket
// whose git objects exist ONLY inside packfiles (no loose keys at all).
//
// Two read paths have to work there, and the split is the whole point of the
// design: a commit or tag is located by the pack map (one Range GET, no index,
// no delta chain, because the commits pack is written with --depth=0), while a
// tree or blob goes through the pack index and may resolve OFS_DELTA /
// REF_DELTA bases. This suite pins both, plus the ceiling that says an item
// detail on a packed bucket costs no more requests than on a loose one.
require("./shim.js");
require("../site/icons.js");
const GS = require("../site/gs-app.js");
const { viewNode, textOf, setHash } = global.__shim;
const ORIGIN = process.env.GS_SITE_ORIGIN || "http://localhost:8000";
const BASE = ORIGIN + "/packed-demo/";
let pass = 0, fail = 0;
const ok = (n, c, e) => { (c ? pass++ : fail++); console.log((c ? "PASS " : "FAIL ") + n + (!c && e ? " :: " + e : "")); };
const wait = (ms) => new Promise((r) => setTimeout(r, ms));

// packEntryType returns the 3-bit type code of a sha's pack entry (6 = OFS_DELTA,
// 7 = REF_DELTA, otherwise a whole object), by reading the single header byte at
// the entry's offset. Used to prove a delta is really there before asserting the
// reader resolved one.
async function packEntryType(ctx, sha) {
  for (const name of await GS.packNames(ctx)) {
    const idx = await GS.packIdx(ctx, name);
    if (!idx) continue;
    const offset = GS.packIdxFind(idx, sha);
    if (offset < 0) continue;
    const got = await GS.fetchRange(ctx.base, "objects/pack/" + name + ".pack", offset, offset + 1);
    return got ? (got.bytes[0] >> 4) & 7 : -1;
  }
  return -1;
}

// Count every bucket fetch, the same honest measure verify_request_budget uses.
const realFetch = global.fetch;
let fetches = 0;
global.fetch = async (url, opts) => { fetches++; return realFetch(url, opts); };

async function main() {
  await wait(1500); // drain the auto-run init home route

  // ---- the bucket really is pack-only ----
  const packs = await GS.fetchText(BASE, "objects/info/packs");
  const names = (packs || "").split("\n").map((l) => /^P (pack-[0-9a-f]+)\.pack$/.exec(l.trim())).filter(Boolean).map((m) => m[1]);
  ok("objects/info/packs lists the bucket's packs", names.length >= 2, "listed " + names.length);

  const ctx = GS.newContext(BASE);
  const items = await GS.loadExtItemsAll(ctx, "social");
  ok("social items index has posts", items.length > 0, "items=" + items.length);
  if (!items.length || !names.length) { console.log("\n" + pass + " passed, " + (fail + 1) + " failed"); process.exit(1); }
  const postSha = items[0].commit.hash;
  ok("the post's loose object key is absent (it lives in a pack)",
    (await GS.fetchBytes(BASE, GS.objectKey(postSha))) === null);

  // ---- commit path: the pack map locates the body ----
  const shard = await GS.packMapShard(ctx, postSha);
  ok("a pack map shard covers the post's sha prefix", !!(shard && shard.offsets[postSha]),
    shard ? "prefix shard has " + Object.keys(shard.offsets).length + " entries" : "no shard");
  const packed = await GS.getPackedObject(GS.newContext(BASE), postSha);
  ok("the post's commit object resolves out of the pack", !!packed && packed.type === "commit", packed ? packed.type : "null");
  if (packed) {
    const commit = GS.parseCommit(postSha, packed.body);
    ok("the packed commit parses with its author and message body",
      commit.authorEmail === "ada@example.com" && commit.content.length > 0, commit.content.slice(0, 40));
  }
  // getObject must reach the same object through its own loose-then-pack order.
  const viaGet = await GS.getObject(GS.newContext(BASE), postSha);
  ok("getObject falls back to the pack for a commit with no loose key", !!viaGet && viaGet.type === "commit");

  // ---- content path: the pack index locates trees and blobs, deltas and all ----
  const codeCtx = GS.newContext(BASE);
  const head = await GS.resolveRef(BASE, "refs/heads/main");
  const notes = await GS.resolvePath(codeCtx, head, "notes.txt");
  ok("a blob resolves through the pack index", !!(notes && notes.type === "blob"), notes ? notes.type : "null");
  const blob = notes && await GS.getObject(codeCtx, notes.sha);
  const blobText = blob ? new TextDecoder().decode(blob.body) : "";
  ok("the packed blob's contents are exact",
    blobText.split("\n").length === 122 && /^notes line 0001: /.test(blobText) && /gamma tail appended/.test(blobText),
    JSON.stringify(blobText.slice(0, 40)) + " lines=" + blobText.split("\n").length);

  // The earlier revision of the same file is what the content pack deltifies.
  // Assert that FIRST — git only deltifies above a content size, so a fixture
  // that quietly shrank would leave this suite testing whole objects while
  // claiming to cover delta resolution.
  const log = await GS.walkHistory(codeCtx, head, 10);
  const older = log.find((c) => /Add notes/.test(c.content));
  const olderBlob = older && await GS.resolvePath(codeCtx, older.hash, "notes.txt");
  const olderType = olderBlob && await packEntryType(codeCtx, olderBlob.sha);
  ok("the older revision is stored as a delta entry (OFS_DELTA or REF_DELTA)",
    olderType === 6 || olderType === 7, "pack entry type " + olderType);
  const olderBody = olderBlob && await GS.getObject(codeCtx, olderBlob.sha);
  const olderText = olderBody ? new TextDecoder().decode(olderBody.body) : "";
  ok("an older revision of the same blob resolves (delta chain included)",
    olderText.split("\n").length === 121 && !/gamma tail appended/.test(olderText),
    JSON.stringify(olderText.slice(0, 40)) + " lines=" + olderText.split("\n").length);

  // ---- routes render, end to end ----
  let ctxRoute = GS.newContext(BASE);
  setHash(GS.commitRef(postSha, "gitmsg/social"));
  await GS.route(ctxRoute);
  await wait(700);
  ok("post detail renders from a pack-only bucket", /packfile|pack index/.test(textOf(viewNode)), textOf(viewNode).slice(0, 80));

  ctxRoute = GS.newContext(BASE);
  setHash("file:notes.txt@main");
  await GS.route(ctxRoute);
  await wait(700);
  ok("the file view renders a packed blob", /gamma/.test(textOf(viewNode)), textOf(viewNode).slice(0, 120));

  // ---- the request ceiling: packing must not cost the detail route extra ----
  // A cold detail route on a packed bucket pays one pack map shard plus one
  // range read where a loose bucket paid one object GET, so the same ceiling
  // that guards the loose fixtures' item detail (verify_request_budget.js) has
  // to hold here too. A regression shows up as a blown ceiling, not a slow page.
  const budgetCtx = GS.newContext(BASE);
  setHash(GS.commitRef(postSha, "gitmsg/social"));
  fetches = 0;
  await GS.route(budgetCtx);
  await wait(700);
  ok("packed post detail ≤ 30 fetches (measured " + fetches + ")", fetches <= 30, "measured " + fetches);

  console.log("\n" + pass + " passed, " + fail + " failed");
  process.exit(fail ? 1 : 0);
}
main().catch((e) => { console.error(e); process.exit(1); });

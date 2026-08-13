// verify_packfiles.js - the browser pack reader against packed-demo, a bucket
// whose git objects live in packfiles (only the state refs stay loose, by
// design), and refdelta-demo, a hand-built REF_DELTA pack of the same objects.
//
// Three read paths have to work there, and the split is the whole point of the
// design: a commit or tag is located by the pack map (one Range GET, no index,
// no delta chain, because the commits pack is written with --depth=0), a tree or
// blob goes through the pack index — range-read via its fanout, never
// downloaded whole — and may resolve OFS_DELTA / REF_DELTA bases, and a loose
// state-ref object still reads loose from the same bucket.
//
// Every reconstructed object is checked CONTENT-ADDRESSED: the sha-1 of
// "<type> <len>\0" + body must equal the sha that was asked for. A delta reader
// that drops or duplicates a copy instruction still produces plausible-looking
// text, and only this check catches it.
require("./shim.js");
require("../site/icons.js");
const GS = require("../site/gs-app.js");
const crypto = require("crypto");
const { viewNode, textOf, setHash } = global.__shim;
const ORIGIN = process.env.GS_SITE_ORIGIN || "http://localhost:8000";
const BASE = ORIGIN + "/packed-demo/";
const REFDELTA = ORIGIN + "/refdelta-demo/";
let pass = 0, fail = 0;
const ok = (n, c, e) => { (c ? pass++ : fail++); console.log((c ? "PASS " : "FAIL ") + n + (!c && e ? " :: " + e : "")); };
const wait = (ms) => new Promise((r) => setTimeout(r, ms));

// Record every bucket fetch: the honest visitor cost is both the number of
// requests and the bytes they carry, and this suite asserts on both.
const realFetch = global.fetch;
let log = [];
global.fetch = async (url, opts) => {
  const res = await realFetch(url, opts);
  const bytes = (await res.clone().arrayBuffer()).byteLength;
  const range = (opts && opts.headers && opts.headers.Range) || "";
  log.push({ url: String(url).replace(ORIGIN, ""), status: res.status, bytes, range });
  return res;
};
// since returns the fetches recorded after a mark, so a single cold read can be
// costed in isolation.
const mark = () => log.length;
const since = (at) => log.slice(at);

// drain waits for the route in flight to stop fetching, rather than sleeping a
// fixed span. This suite costs cold reads off the log, so a late fetch that a
// fixed sleep did not cover is charged to the NEXT measurement instead.
async function drain(quietMs, maxMs) {
  const quiet = quietMs || 400, deadline = Date.now() + (maxMs || 15000);
  let seen = -1, still = Date.now();
  while (Date.now() < deadline) {
    if (log.length !== seen) { seen = log.length; still = Date.now(); }
    else if (Date.now() - still >= quiet) return;
    await wait(50);
  }
}

// gitSha recomputes an object's git sha-1 from what the reader reconstructed.
function gitSha(obj) {
  const head = Buffer.from(obj.type + " " + obj.body.length + "\0", "binary");
  return crypto.createHash("sha1").update(Buffer.concat([head, Buffer.from(obj.body)])).digest("hex");
}

// readsBack asserts that a sha read through getObject reconstructs to exactly
// that sha, and returns the object for further checks.
async function readsBack(ctx, sha, label) {
  const obj = await GS.getObject(ctx, sha);
  const got = obj ? gitSha(obj) : "null";
  ok(label + " reads back content-addressed (" + (obj ? obj.type : "null") + ")", got === sha, got + " != " + sha);
  return obj;
}

// packEntryType returns the 3-bit type code of a sha's pack entry (6 = OFS_DELTA,
// 7 = REF_DELTA, otherwise a whole object). Used to prove a delta is really
// there before asserting the reader resolved one.
async function packEntryType(ctx, sha) {
  for (const name of await GS.packNames(ctx)) {
    const found = await GS.packIdxLookup(ctx, name, sha);
    if (!found) continue;
    const got = await GS.fetchRange(ctx.base, "objects/pack/" + name + ".pack", found.offset, found.offset + 1);
    return got ? (got.bytes[0] >> 4) & 7 : -1;
  }
  return -1;
}

async function main() {
  await drain(); // the auto-run init home route

  // ---- the bucket really is packed ----
  const packs = await GS.fetchText(BASE, "objects/info/packs");
  const names = (packs || "").split("\n").map((l) => /^P (pack-[0-9a-f]+)\.pack$/.exec(l.trim())).filter(Boolean).map((m) => m[1]);
  ok("objects/info/packs lists the bucket's packs", names.length >= 2, "listed " + names.length);
  ok("the pack listing is what makes the reader treat the bucket as packed",
    (await GS.bucketIsPacked(GS.newContext(BASE))) === true);
  ok("a bucket with no packs is not treated as packed",
    (await GS.bucketIsPacked(GS.newContext(ORIGIN + "/thread-demo/"))) === false);

  const ctx = GS.newContext(BASE);
  const items = await GS.loadExtItemsAll(ctx, "social");
  ok("social items index has posts", items.length > 0, "items=" + items.length);
  if (!items.length || !names.length) { console.log("\n" + pass + " passed, " + (fail + 1) + " failed"); process.exit(1); }
  const postSha = items[0].commit.hash;
  ok("the post's loose object key is absent (it lives in a pack)",
    (await GS.fetchBytes(BASE, GS.objectKey(postSha))) === null);

  // The listing ORDERS the two lookups; it is never proof the bucket is loose.
  // It is rewritten best-effort on every push — its PUT can fail while the packs
  // and the pack map are already durable, and writeDumbTransportInfo's own
  // rewrite is best-effort too — so a reader that took an empty listing as
  // authoritative rendered a fully packed bucket as nothing at all, with every
  // byte present. ctx.packs.packed is the memo that listing fills, so seeding it
  // false IS the bucket-says-loose state, with the loose key genuinely absent
  // (asserted above).
  const saysLoose = GS.newContext(BASE);
  saysLoose.packs.packed = Promise.resolve(false);
  const viaMap = await GS.getObject(saysLoose, postSha);
  ok("a packed bucket whose pack listing went missing still resolves off the pack map",
    !!viaMap && gitSha(viaMap) === postSha, viaMap ? viaMap.type + " " + gitSha(viaMap) : "null");

  // A rejected object read is never kept on the context. One 500 on one blob
  // used to break that sha in every view for the rest of the session, returning
  // the cached rejection with no network activity left to recover from.
  const flaky = GS.newContext(BASE);
  const passthrough = global.fetch;
  let armed = true;
  global.fetch = async (url, opts) => {
    if (armed && /\/objects\/pack\//.test(String(url))) { armed = false; throw new Error("simulated transport failure"); }
    return passthrough(url, opts);
  };
  let rejected = false;
  try { await GS.getObject(flaky, postSha); } catch (e) { rejected = true; }
  global.fetch = passthrough;
  ok("the injected fault really reached the object read", rejected && !armed, "rejected=" + rejected + " armed=" + armed);
  const beforeRetry = mark();
  const recovered = await GS.getObject(flaky, postSha);
  ok("a failed object read is retried on the next call, not cached",
    !!recovered && gitSha(recovered) === postSha && since(beforeRetry).length > 0,
    (recovered ? gitSha(recovered) : "null") + ", " + since(beforeRetry).length + " fetches");

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
  // A cold commit read must not pay a 404 to discover the bucket is packed: the
  // pack listing already said so, so no loose object key is probed at all.
  const commitCtx = GS.newContext(BASE);
  let at = mark();
  await readsBack(commitCtx, postSha, "a mapped commit");
  const commitReads = since(at);
  ok("a cold commit read probes no loose object key",
    commitReads.filter((r) => /\/objects\/[0-9a-f]{2}\//.test(r.url)).length === 0,
    commitReads.map((r) => r.status + " " + r.url).join(" | "));
  ok("a cold commit read touches no pack index",
    commitReads.filter((r) => /\.idx/.test(r.url)).length === 0,
    commitReads.map((r) => r.url).join(" | "));

  // ---- content path: the pack index locates trees and blobs ----
  const codeCtx = GS.newContext(BASE);
  const head = await GS.resolveRef(BASE, "refs/heads/main");
  const headObj = await readsBack(codeCtx, head, "the branch tip commit");
  const rootTree = headObj && GS.parseCommit(head, headObj.body).tree;
  if (rootTree) await readsBack(codeCtx, rootTree, "the root tree via the index");
  const notes = await GS.resolvePath(codeCtx, head, "notes.txt");
  ok("a blob resolves through the pack index", !!(notes && notes.type === "blob"), notes ? notes.type : "null");
  const blob = notes && await readsBack(codeCtx, notes.sha, "the newest notes blob");
  const blobText = blob ? new TextDecoder().decode(blob.body) : "";
  ok("the packed blob's contents are exact",
    blobText.split("\n").length === 352 && /^notes line 0001: /.test(blobText) && /gamma tail appended/.test(blobText),
    JSON.stringify(blobText.slice(0, 40)) + " lines=" + blobText.split("\n").length);

  // ---- the index is RANGE-read, not downloaded ----
  // The whole point of the fanout: a cold blob read must cost a fraction of the
  // index it searched, or a packed bucket's file views pay for the index on
  // every session.
  const rangeCtx = GS.newContext(BASE);
  at = mark();
  const cold = await GS.resolvePath(rangeCtx, head, "notes.txt");
  if (cold) await GS.getObject(rangeCtx, cold.sha);
  const idxRows = since(at).filter((r) => /\.idx/.test(r.url));
  const idxBytes = idxRows.reduce((a, r) => a + r.bytes, 0);
  let biggest = 0;
  for (const name of names) {
    const probe = await GS.fetchRange(BASE, "objects/pack/" + name + ".idx", 0, 1);
    if (probe && probe.total > biggest) biggest = probe.total;
  }
  // "Fewer bytes than the whole index" is not the claim — a read of 99% of it
  // would pass that. The claim has a SHAPE, and the three kinds of range it is
  // made of scale differently, so each is bounded on its own:
  //   - the HEAD (bytes=0-4095), read once per pack and cached for the session,
  //     which also delivers a small pack's index whole,
  //   - the OFFSET TABLE (an open-ended range from ofsStart), read once per pack
  //     a lookup HITS: 4 bytes per object plus the 8-byte large-offset overflow,
  //     against a v2 index's 24 bytes per object, so a sixth of the index,
  //   - every other read is a SHA-TABLE SLICE the fanout bounds to the objects
  //     sharing the sha's first byte: 20 bytes each out of 1/256th of the
  //     index's objects, a few hundred bytes and NOT a function of how much
  //     bigger the index gets. That last one is what a regression into reading
  //     the index shows up in.
  const IDX_HEAD_BYTES = 4096;
  const heads = idxRows.filter((r) => /^bytes=0-\d/.test(r.range));
  const tables = idxRows.filter((r) => /^bytes=\d+-$/.test(r.range));
  const slices = idxRows.filter((r) => !heads.includes(r) && !tables.includes(r));
  const sliceBound = biggest / 256 + 64;
  ok("the cold read really searched a big index (" + idxBytes + " of " + biggest + " bytes over " + idxRows.length + " ranges)",
    biggest > 4 * IDX_HEAD_BYTES && slices.length > 0, "biggest index " + biggest + ", " + slices.length + " sha-table reads");
  ok("each pack's index is opened at most once (" + heads.length + " head reads over " + names.length + " packs)",
    heads.length <= names.length && heads.every((r) => r.bytes <= IDX_HEAD_BYTES),
    heads.map((r) => r.range + " " + r.bytes).join(" | "));
  ok("the offset table is read once per hit pack and is a sixth of the index",
    tables.length <= names.length && tables.every((r) => r.bytes <= biggest / 6 + 512),
    tables.map((r) => r.range + " " + r.bytes).join(" | ") + " bound " + Math.round(biggest / 6 + 512));
  ok("every sha-table read is fanout-bounded to a few hundred bytes (bound " + Math.round(sliceBound) + ")",
    slices.every((r) => r.bytes <= sliceBound),
    slices.map((r) => r.range + " " + r.bytes).join(" | "));

  // ---- deltas: three older revisions, chained rather than fanned out ----
  // Assert the entry types FIRST — git only deltifies above a content size, so a
  // fixture that quietly shrank would leave this suite testing whole objects
  // while claiming to cover delta resolution.
  const log10 = await GS.walkHistory(codeCtx, head, 10);
  const revisions = [
    { subject: /Revise notes/, has: /rewritten by revision 3/, hasNot: /rewritten by revision 4/ },
    { subject: /Amend notes/, has: /rewritten by revision 2/, hasNot: /rewritten by revision 3/ },
    { subject: /Add notes/, has: /notes line 0350/, hasNot: /rewritten by revision/ },
  ];
  const olderShas = [];
  const olderTypes = [];
  for (const rev of revisions) {
    const commit = log10.find((c) => rev.subject.test(c.content));
    const revBlob = commit && await GS.resolvePath(codeCtx, commit.hash, "notes.txt");
    if (revBlob) olderShas.push(revBlob.sha);
    olderTypes.push(revBlob ? await packEntryType(codeCtx, revBlob.sha) : -1);
    const body = revBlob && await readsBack(codeCtx, revBlob.sha, "the revision behind /" + rev.subject.source + "/");
    const text = body ? new TextDecoder().decode(body.body) : "";
    ok("the revision behind /" + rev.subject.source + "/ reconstructs exactly",
      text.split("\n").length === 351 && rev.has.test(text) && !rev.hasNot.test(text),
      JSON.stringify(text.slice(0, 40)) + " lines=" + text.split("\n").length);
  }
  // The chain's oldest revision is whichever one git chose as the base, so which
  // of the three is stored whole is git's call; that at least two are deltas,
  // over a fixture whose deepest chain is asserted past depth 1 at build time,
  // is what makes the walk above a real chain walk.
  ok("the older revisions are stored as delta entries (OFS_DELTA)",
    olderTypes.filter((t) => t === 6).length >= 2, "pack entry types " + olderTypes.join(","));

  // ---- mixed bucket: state refs stay loose next to the packs ----
  const configSha = await GS.resolveRef(BASE, "refs/gitmsg/social/config");
  ok("the bucket carries a loose state-ref object", !!configSha && (await GS.fetchBytes(BASE, GS.objectKey(configSha))) !== null,
    "config sha " + configSha);
  if (configSha) await readsBack(GS.newContext(BASE), configSha, "a loose state-ref object on a packed bucket");

  // ---- REF_DELTA: the same objects, packed with sha-named delta bases ----
  const rdCtx = GS.newContext(REFDELTA);
  const rdShas = [postSha, head, rootTree, notes && notes.sha].concat(olderShas).filter(Boolean);
  let refDeltas = 0;
  for (const sha of rdShas) if ((await packEntryType(rdCtx, sha)) === 7) refDeltas++;
  ok("refdelta-demo really stores REF_DELTA entries", refDeltas > 0, "type-7 entries among " + rdShas.length);
  for (const sha of rdShas) await readsBack(rdCtx, sha, "REF_DELTA bucket object " + sha.slice(0, 8));

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

  // ---- a sha12 commit route resolves a PACKED code commit ----
  // The static pages (front-page activity rows, the commits lists) link code
  // commits by sha12, and getObject alone cannot answer one: a short sha builds
  // a malformed loose key, and the packed path looks up exact full shas (pack
  // map offsets, pack index binary search). The route must resolve the prefix
  // through the code items index first. Pinned against a NON-TIP commit so a
  // tip-only shortcut can't fake it; the rendered detail carries the FULL sha,
  // which is the resolution this guards.
  const parentSha = (headObj && GS.parseCommit(head, headObj.body).parents[0]) || head;
  ctxRoute = GS.newContext(BASE);
  setHash("commit:" + parentSha.slice(0, 12) + "@main");
  await GS.route(ctxRoute);
  await wait(700);
  ok("a sha12 commit route renders the packed commit's detail (full sha resolved)",
    !/Commit not found/.test(textOf(viewNode)) && textOf(viewNode).indexOf(parentSha) !== -1,
    textOf(viewNode).slice(0, 100));

  // ---- the request ceiling: packing must not cost the detail route extra ----
  // A cold detail route on a packed bucket pays one pack map shard plus one
  // range read where a loose bucket paid one object GET, so the same ceiling
  // that guards the loose fixtures' item detail (verify_request_budget.js) has
  // to hold here too. A regression shows up as a blown ceiling, not a slow page.
  const budgetCtx = GS.newContext(BASE);
  setHash(GS.commitRef(postSha, "gitmsg/social"));
  at = mark();
  await GS.route(budgetCtx);
  await wait(700);
  const fetches = since(at).length;
  ok("packed post detail ≤ 30 fetches (measured " + fetches + ")", fetches <= 30, "measured " + fetches);

  console.log("\n" + pass + " passed, " + fail + " failed");
  process.exit(fail ? 1 : 0);
}
main().catch((e) => { console.error(e); process.exit(1); });

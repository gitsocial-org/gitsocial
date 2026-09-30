// unit_timeline_hydration.js - DOM-free units for corpus-backed body hydration:
// a timeline window fills its card bodies from the bodies corpus, an older shard
// loads on demand, and a commit the corpus misses keeps its object read.
const zlib = require("zlib");
const GS = require("../assets/gs-core.js");
let pass = 0, fail = 0;
function eq(a, b, msg) { if (JSON.stringify(a) === JSON.stringify(b)) { pass++; } else { fail++; console.log("FAIL", msg, "got", JSON.stringify(a), "want", JSON.stringify(b)); } }
function ok(c, msg, extra) { if (c) { pass++; } else { fail++; console.log("FAIL", msg, extra == null ? "" : extra); } }

const BASE = "http://bucket.test/repo/";
const sha = (s) => (s.repeat(40)).slice(0, 40);

// The stubbed bucket: a key to body map, with every request path logged.
let served = {};
let log = [];
global.fetch = async (url) => {
  const key = String(url).slice(BASE.length);
  log.push(key);
  const body = served[key];
  if (body === undefined) return { status: 404, ok: false, headers: { get: () => null }, arrayBuffer: async () => new ArrayBuffer(0) };
  const buf = Buffer.isBuffer(body) ? body : Buffer.from(body);
  return { status: 200, ok: true, headers: { get: () => null }, arrayBuffer: async () => buf.buffer.slice(buf.byteOffset, buf.byteOffset + buf.byteLength) };
};
const countOf = (prefix) => log.filter((k) => k.startsWith(prefix)).length;

// looseObject builds one deflated loose commit object for the fallback path.
function looseObject(message, when) {
  const body = "tree " + sha("1") + "\nauthor Bob <bob@example.com> " + when + " +0000\ncommitter Bob <bob@example.com> " + when + " +0000\n\n" + message;
  const payload = Buffer.concat([Buffer.from("commit " + Buffer.byteLength(body) + "\0"), Buffer.from(body)]);
  return zlib.deflateSync(payload);
}

const HEADER = 'GitMsg: ext="social" v="0.1.0" type="post"';
const msg = (text) => text + "\n\n" + HEADER;
const meta = (s, subject, ts) => ({ sha: s, author: "Bob", email: "bob@example.com", ts, header: HEADER, subject });
const bodyEntry = (s, text, ts) => ({ sha: s, author: "Bob", ts, message: msg(text) });
const doc = (tip, items) => JSON.stringify({ version: 4, tip, items });

async function main() {
  console.log("=== a window's bodies come from the corpus, with the object read as the fallback ===");
  const C = sha("c"), A = sha("a"), B = sha("b");
  served = {
    ".gitsocial/refs.json": JSON.stringify({ "refs/heads/gitmsg/social": B }),
    ".gitsocial/site/items/social/manifest.json": JSON.stringify({ version: 4, tip: B, complete: true, shards: [{ key: "shard-old.json" }, { key: "shard-new.json" }] }),
    ".gitsocial/site/items/social/shard-old.json": doc(B, [meta(C, "C subject", 100)]),
    ".gitsocial/site/items/social/shard-new.json": doc(B, [meta(A, "A subject", 200)]),
    ".gitsocial/site/items/social/head.json": doc(B, [meta(B, "B subject", 300)]),
    ".gitsocial/site/bodies/social/manifest.json": JSON.stringify({ version: 4, tip: B, shards: [{ key: "shard-old.json" }, { key: "shard-new.json" }] }),
    ".gitsocial/site/bodies/social/shard-old.json": doc(B, [bodyEntry(C, "C body from an older shard", 100)]),
    ".gitsocial/site/bodies/social/shard-new.json": doc(B, [bodyEntry(A, "A body from the corpus", 200)]),
    ".gitsocial/site/bodies/social/head.json": doc(B, []),
    ["objects/" + B.slice(0, 2) + "/" + B.slice(2)]: looseObject(msg("B body from its object"), 300),
  };
  log = [];
  const ctx = GS.newContext(BASE);
  const first = await GS.loadTimelineWindow(ctx, false);
  await first.hydrated;
  const byHash = new Map(first.items.map((it) => [it.commit.hash, it]));
  eq(first.items.length, 3, "the window holds the three indexed items");
  eq(byHash.get(A).content, "A body from the corpus", "an eager-set body fills its card from the corpus");
  eq(byHash.get(C).content, "C body from an older shard", "an older-shard body loads on demand");
  eq(byHash.get(B).content, "B body from its object", "a commit the corpus misses hydrates through its object");
  eq(byHash.get(A).commit.refs, [], "a corpus fill leaves the parsed refs in place");
  eq(byHash.get(A).header.type, "post", "the corpus fill keeps the parsed header");
  eq(countOf("objects/" + A.slice(0, 2)), 0, "the corpus-served commit pays no object read");
  eq(countOf("objects/" + B.slice(0, 2)), 1, "the fallback pays its one object read");
  eq(countOf(".gitsocial/packmap/"), 0, "no packmap shard is read for the window");
  eq(countOf(".gitsocial/site/bodies/social/"), 4, "the corpus costs its manifest, eager set and one older shard");

  console.log("=== the second window re-reads no corpus document ===");
  const before = countOf(".gitsocial/site/bodies/social/");
  const second = await GS.loadTimelineWindow(ctx, true);
  await second.hydrated;
  eq(countOf(".gitsocial/site/bodies/social/"), before, "the corpus state is shared across windows");

  console.log("=== search shares the documents hydration already read ===");
  const idx = await GS.loadBodyIndex(ctx, "social");
  eq(idx.items.length, 2, "the search index assembles from the shared documents");
  eq(countOf(".gitsocial/site/bodies/social/"), before, "search re-reads no bodies document hydration fetched");

  console.log("=== a bucket without a bodies corpus keeps the object path whole ===");
  const Z = sha("d");
  served = {
    ".gitsocial/refs.json": JSON.stringify({ "refs/heads/gitmsg/social": Z }),
    ".gitsocial/site/items/social/manifest.json": JSON.stringify({ version: 4, tip: Z, complete: true, shards: [] }),
    ".gitsocial/site/items/social/head.json": doc(Z, [meta(A, "A subject", 200), meta(Z, "Z subject", 400)]),
    ["objects/" + A.slice(0, 2) + "/" + A.slice(2)]: looseObject(msg("A body from its object"), 200),
  };
  log = [];
  const ctx2 = GS.newContext(BASE);
  const r2 = await GS.loadTimelineWindow(ctx2, false);
  await r2.hydrated;
  const by2 = new Map(r2.items.map((it) => [it.commit.hash, it]));
  eq(by2.get(A).content, "A body from its object", "without a corpus every body hydrates through its object");
  eq(countOf(".gitsocial/site/bodies/social/manifest.json"), 1, "the absent corpus costs one manifest probe");
  eq(by2.get(Z).content, "", "a body with no source stays empty");
  eq(GS.itemSubject(by2.get(Z)), "Z subject", "and its card falls back to the index subject");

  console.log("\n" + pass + " passed, " + fail + " failed");
  process.exit(fail ? 1 : 0);
}
main().catch((e) => { console.error(e); process.exit(1); });

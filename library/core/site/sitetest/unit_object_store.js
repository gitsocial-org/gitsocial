// unit_object_store.js - the persistent object store stays under its budgets: a session that resolves more objects than the count budget fills the store and no further, and the next boot trims it to three quarters.
const zlib = require("zlib");
require("./shim.js");
const GS = require("../assets/gs-core.js");
const { fakeCaches } = global.__shim;
let pass = 0, fail = 0;
function eq(a, b, msg) { if (JSON.stringify(a) === JSON.stringify(b)) { pass++; } else { fail++; console.log("FAIL", msg, "got", JSON.stringify(a), "want", JSON.stringify(b)); } }
function ok(c, msg, extra) { if (c) { pass++; } else { fail++; console.log("FAIL", msg, extra == null ? "" : extra); } }
const wait = (ms) => new Promise((r) => setTimeout(r, ms));

const BASE = "http://bucket.test/repo/";
const STORE = "gs-objects:" + BASE;
const OBJECT_URL = /\/objects\/[0-9a-f]{2}\/[0-9a-f]{38}$/;
const shaOf = (n) => String(n).padStart(40, "0");

// looseObject builds one deflated loose commit whose message names its sequence number.
function looseObject(n) {
  const body = "tree " + shaOf(1) + "\nauthor Bob <bob@example.com> " + (1000 + n) + " +0000\ncommitter Bob <bob@example.com> " + (1000 + n) + " +0000\n\ncommit " + n;
  const payload = Buffer.concat([Buffer.from("commit " + Buffer.byteLength(body) + "\0"), Buffer.from(body)]);
  return zlib.deflateSync(payload);
}

// The stubbed bucket: every loose object key answers, every other key is absent; object requests are counted.
let objectRequests = 0;
global.fetch = async (url) => {
  const key = String(url).slice(BASE.length);
  const m = /^objects\/([0-9a-f]{2})\/([0-9a-f]{38})$/.exec(key);
  if (!m) return { status: 404, ok: false, headers: { get: () => null }, arrayBuffer: async () => new ArrayBuffer(0) };
  objectRequests++;
  const buf = looseObject(parseInt(m[1] + m[2], 10));
  return { status: 200, ok: true, headers: { get: () => null }, arrayBuffer: async () => buf.buffer.slice(buf.byteOffset, buf.byteOffset + buf.byteLength) };
};

// settled resolves once the store's entry count has not changed for 100 ms.
async function settled(caches) {
  let last = -1;
  for (;;) {
    const n = caches.entries(STORE).length;
    if (n === last) return;
    last = n;
    await wait(100);
  }
}

// objectEntries lists the stored objects, the metadata entry left out.
const objectEntries = (caches) => caches.entries(STORE).filter((e) => OBJECT_URL.test(e.url));

async function main() {
  const caches = fakeCaches();
  global.caches = caches;
  const budget = GS.STORE_COUNT_BUDGET;
  const total = budget + 50;

  console.log("=== a session that resolves more objects than the count budget fills the store to the budget ===");
  const ctx = GS.newContext(BASE);
  await GS.objectStore(ctx);
  for (let from = 1; from <= total; from += 500) {
    const batch = [];
    for (let n = from; n < from + 500 && n <= total; n++) batch.push(GS.getObject(ctx, shaOf(n)));
    await Promise.all(batch);
  }
  eq(objectRequests, total, "every object came from the bucket once");
  await settled(caches);
  const entries = objectEntries(caches);
  eq(entries.length, budget, "the store holds the count budget and no more");
  const bytes = entries.reduce((n, e) => n + e.bytes, 0);
  ok(bytes <= GS.STORE_BYTE_BUDGET, "the store is under its byte budget", bytes);
  const meta = caches.entries(STORE).find((e) => !OBJECT_URL.test(e.url));
  const metaDoc = meta ? await (await (await caches.open(STORE)).match(meta.url)).json() : null;
  eq(metaDoc && metaDoc.bytes, bytes, "the metadata entry carries the byte total of the stored objects");
  eq(metaDoc && metaDoc.count, budget, "the metadata entry carries the entry count");
  ok(entries[0].url.endsWith(shaOf(1).slice(0, 2) + "/" + shaOf(1).slice(2)), "the first object resolved is the oldest entry", entries[0].url);

  console.log("=== the next boot trims a full store to three quarters, oldest first ===");
  const ctx2 = GS.newContext(BASE);
  await GS.objectStore(ctx2);
  const before = objectRequests;
  const obj = await GS.getObject(ctx2, shaOf(budget));
  eq(objectRequests, before, "the newest stored object is read from the store");
  eq(obj.type, "commit", "a stored object keeps its type");
  await settled(caches);
  const trimmed = objectEntries(caches);
  eq(trimmed.length, budget * 3 / 4, "the store is at three quarters of the count budget");
  ok(!trimmed.some((e) => e.url.endsWith(shaOf(1).slice(0, 2) + "/" + shaOf(1).slice(2))), "the oldest entry is gone");
  ok(trimmed.some((e) => e.url.endsWith(shaOf(budget).slice(0, 2) + "/" + shaOf(budget).slice(2))), "the newest entry stays");
  const store = await GS.objectStore(ctx2);
  eq(store.count, trimmed.length, "the open store counts what it holds");
  const stored = trimmed.reduce((n, e) => n + e.bytes, 0);
  ok(Math.abs(store.bytes - stored) <= stored * 0.05, "the running byte total is within five percent of the stored bytes", store.bytes + " vs " + stored);

  console.log("\n" + pass + " passed, " + fail + " failed");
  process.exit(fail ? 1 : 0);
}
main().catch((e) => { console.error(e); process.exit(1); });

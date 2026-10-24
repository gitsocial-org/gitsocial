// unit_pack_probe.js - a content object in the last of many packs: the probe opens the indexes several at a time, and a mapped object reads through its shard.
require("./shim.js");
const crypto = require("crypto");
const zlib = require("zlib");
const GS = require("../assets/gs-core.js");
let pass = 0, fail = 0;
function ok(c, msg, extra) { if (c) { pass++; } else { fail++; console.log("FAIL", msg, extra == null ? "" : extra); } }
function eq(a, b, msg) { ok(JSON.stringify(a) === JSON.stringify(b), msg, "got " + JSON.stringify(a) + " want " + JSON.stringify(b)); }
const tick = (ms) => new Promise((r) => setTimeout(r, ms));
const hexBytes = (h) => Uint8Array.from(h.match(/../g).map((b) => parseInt(b, 16)));

// packIdx builds a v2 pack index over full shas, each at its offset in offsets or at a fake offset 12 + i * 100.
function packIdx(shas, offsets) {
  const sorted = shas.slice().sort(), n = sorted.length;
  const buf = new Uint8Array(8 + 1024 + n * 28 + 40);
  const dv = new DataView(buf.buffer);
  dv.setUint32(0, 0xff744f63); dv.setUint32(4, 2);
  for (let b = 0, i = 0; b < 256; b++) { while (i < n && parseInt(sorted[i].slice(0, 2), 16) <= b) i++; dv.setUint32(8 + b * 4, i); }
  sorted.forEach((s, i) => { buf.set(hexBytes(s), 1032 + i * 20); dv.setUint32(1032 + n * 24 + i * 4, offsets ? offsets[s] : 12 + i * 100); });
  return buf;
}

// packWithBlob builds a pack of one blob entry at offset 12: the 12-byte header, the entry, the 20-byte trailer.
function packWithBlob(text) {
  const body = Buffer.from(text);
  const z = zlib.deflateSync(body);
  const out = new Uint8Array(12 + 1 + z.length + 20);
  out.set(Buffer.from("PACK"), 0); new DataView(out.buffer).setUint32(4, 2); new DataView(out.buffer).setUint32(8, 1);
  out[12] = 0x30 | body.length; // type blob, a size under 16
  out.set(z, 13);
  return out;
}

// blobSha returns the git sha of a blob with this text.
const blobSha = (text) => crypto.createHash("sha1").update("blob " + Buffer.byteLength(text) + "\0" + text).digest("hex");

// packWithDelta builds a pack of a base blob at offset 12 and an ofs-delta entry after it that copies the base's first six bytes and inserts the rest.
function packWithDelta(baseText, tail) {
  const base = Buffer.from(baseText), insert = Buffer.from(tail);
  const delta = Buffer.concat([Buffer.from([base.length, 6 + insert.length, 0x90, 6, insert.length]), insert]);
  const baseEntry = Buffer.concat([Buffer.from([0x30 | base.length]), zlib.deflateSync(base)]);
  const deltaEntry = Buffer.concat([Buffer.from([0x60 | delta.length, baseEntry.length]), zlib.deflateSync(delta)]);
  const head = Buffer.alloc(12); head.write("PACK"); head.writeUInt32BE(2, 4); head.writeUInt32BE(2, 8);
  return { pack: new Uint8Array(Buffer.concat([head, baseEntry, deltaEntry, Buffer.alloc(20)])), deltaAt: 12 + baseEntry.length, deltaSize: deltaEntry.length };
}

// stubBucket serves the named indexes, pack-8.pack and the pack map shards from one fetch stub, and records each index it opens.
function stubBucket(idxs, pack, shards) {
  const seen = { opened: new Set(), peak: 0 };
  let inFlight = 0;
  const serve = (buf, opts) => {
    const m = /bytes=(\d+)-(\d*)/.exec((opts && opts.headers && opts.headers.Range) || "");
    const start = m ? Number(m[1]) : 0, end = m && m[2] ? Math.min(Number(m[2]) + 1, buf.length) : buf.length;
    const body = buf.slice(start, end);
    return { ok: true, status: 206, headers: { get: (h) => (h === "Content-Range" ? "bytes " + start + "-" + (end - 1) + "/" + buf.length : null) }, arrayBuffer: async () => body.buffer.slice(body.byteOffset, body.byteOffset + body.byteLength) };
  };
  global.fetch = async (url, opts) => {
    const u = String(url);
    const idx = /objects\/pack\/(pack-\d+)\.idx$/.exec(u);
    if (idx && idxs.has(idx[1])) {
      seen.opened.add(idx[1]); inFlight++; seen.peak = Math.max(seen.peak, inFlight);
      await tick(20); inFlight--;
      return serve(idxs.get(idx[1]), opts);
    }
    if (u.endsWith("objects/pack/pack-8.pack")) return serve(pack, opts);
    const shard = /\.gitsocial\/packmap\/([0-9a-f]{2})\.json$/.exec(u);
    if (shard && shards[shard[1]]) return serve(new TextEncoder().encode(JSON.stringify(shards[shard[1]])), opts);
    return { ok: false, status: 404, headers: { get: () => null }, arrayBuffer: async () => new ArrayBuffer(0) };
  };
  return seen;
}

async function main() {
  const text = "hi\n";
  const sha = blobSha(text);
  const names = Array.from({ length: 8 }, (_, i) => "pack-" + (i + 1));
  const idxs = new Map(names.map((n, i) => [n, packIdx(i === 7 ? [sha] : [])]));
  const pack = packWithBlob(text);
  const seen = stubBucket(idxs, pack, {});
  const ctx = GS.newContext("http://bucket.test/");
  ctx.packs.names = names.slice();
  const obj = await GS.getPackedObject(ctx, sha);
  ok(!!obj && obj.type === "blob", "the blob in the last pack is found", obj && obj.type);
  eq(obj ? new TextDecoder().decode(obj.body) : "", text, "and its body reads whole");
  eq(seen.opened.size, 8, "every index was opened on the way");
  ok(seen.peak >= 6, "the indexes open several at a time, not one after the other", "peak in flight " + seen.peak);
  eq(ctx.packs.hits[0], "pack-8", "the hit pack leads the next probe");
  const again = await GS.getPackedObject(ctx, sha);
  ok(!!again && again.type === "blob", "a second read of the object resolves from the opened indexes");

  // A blob its shard maps reads at its range and opens no index.
  const mappedSeen = stubBucket(idxs, pack, { [sha.slice(0, 2)]: { version: 1, packs: ["pack-8"], offsets: { [sha]: [0, 12, pack.length - 12 - 20] } } });
  const mappedCtx = GS.newContext("http://bucket.test/");
  mappedCtx.packs.names = names.slice();
  const mapped = await GS.getPackedObject(mappedCtx, sha);
  eq(mapped ? new TextDecoder().decode(mapped.body) : "", text, "a mapped blob in the last pack reads through its shard");
  eq(mappedSeen.opened.size, 0, "a mapped read opens no index");

  // A mapped ofs-delta blob resolves through its base, which the index of its pack bounds.
  const baseText = "hello base\n", deltaText = "hello delta\n";
  const built = packWithDelta(baseText, "delta\n");
  const baseSha = blobSha(baseText), deltaSha = blobSha(deltaText);
  const deltaIdxs = new Map([["pack-8", packIdx([baseSha, deltaSha], { [baseSha]: 12, [deltaSha]: built.deltaAt })]]);
  stubBucket(deltaIdxs, built.pack, { [deltaSha.slice(0, 2)]: { version: 1, packs: ["pack-8"], offsets: { [deltaSha]: [0, built.deltaAt, built.deltaSize] } } });
  const deltaCtx = GS.newContext("http://bucket.test/");
  deltaCtx.packs.names = ["pack-8"];
  const resolved = await GS.getPackedObject(deltaCtx, deltaSha);
  ok(!!resolved && resolved.type === "blob", "a mapped ofs-delta entry resolves to a blob", resolved && resolved.type);
  eq(resolved ? new TextDecoder().decode(resolved.body) : "", deltaText, "and its delta applies to the base");
  console.log("\n" + pass + " passed, " + fail + " failed");
  process.exit(fail ? 1 : 0);
}
main().catch((e) => { console.error(e); process.exit(1); });

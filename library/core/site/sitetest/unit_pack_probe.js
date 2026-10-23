// unit_pack_probe.js - a content object in the last of many packs: the probe opens the indexes several at a time and reads the entry it finds.
require("./shim.js");
const crypto = require("crypto");
const zlib = require("zlib");
const GS = require("../assets/gs-core.js");
let pass = 0, fail = 0;
function ok(c, msg, extra) { if (c) { pass++; } else { fail++; console.log("FAIL", msg, extra == null ? "" : extra); } }
function eq(a, b, msg) { ok(JSON.stringify(a) === JSON.stringify(b), msg, "got " + JSON.stringify(a) + " want " + JSON.stringify(b)); }
const tick = (ms) => new Promise((r) => setTimeout(r, ms));
const hexBytes = (h) => Uint8Array.from(h.match(/../g).map((b) => parseInt(b, 16)));

// packIdx builds a v2 pack index over full shas, each at a fake offset 12 + i * 100.
function packIdx(shas) {
  const sorted = shas.slice().sort(), n = sorted.length;
  const buf = new Uint8Array(8 + 1024 + n * 28 + 40);
  const dv = new DataView(buf.buffer);
  dv.setUint32(0, 0xff744f63); dv.setUint32(4, 2);
  for (let b = 0, i = 0; b < 256; b++) { while (i < n && parseInt(sorted[i].slice(0, 2), 16) <= b) i++; dv.setUint32(8 + b * 4, i); }
  sorted.forEach((s, i) => { buf.set(hexBytes(s), 1032 + i * 20); dv.setUint32(1032 + n * 24 + i * 4, 12 + i * 100); });
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

async function main() {
  const text = "hi\n";
  const sha = crypto.createHash("sha1").update("blob " + text.length + "\0" + text).digest("hex");
  const names = Array.from({ length: 8 }, (_, i) => "pack-" + (i + 1));
  const idxs = new Map(names.map((n, i) => [n, packIdx(i === 7 ? [sha] : [])]));
  const pack = packWithBlob(text);
  let inFlight = 0, peak = 0; const opened = new Set();
  const serve = (buf, opts) => {
    const m = /bytes=(\d+)-(\d*)/.exec((opts && opts.headers && opts.headers.Range) || "");
    const start = m ? Number(m[1]) : 0, end = m && m[2] ? Math.min(Number(m[2]) + 1, buf.length) : buf.length;
    const body = buf.slice(start, end);
    return { ok: true, status: 206, headers: { get: (h) => (h === "Content-Range" ? "bytes " + start + "-" + (end - 1) + "/" + buf.length : null) }, arrayBuffer: async () => body.buffer.slice(body.byteOffset, body.byteOffset + body.byteLength) };
  };
  global.fetch = async (url, opts) => {
    const u = String(url);
    const idx = /objects\/pack\/(pack-\d+)\.idx$/.exec(u);
    if (idx) {
      opened.add(idx[1]); inFlight++; peak = Math.max(peak, inFlight);
      await tick(20); inFlight--;
      return serve(idxs.get(idx[1]), opts);
    }
    if (u.endsWith("objects/pack/pack-8.pack")) return serve(pack, opts);
    return { ok: false, status: 404, headers: { get: () => null }, arrayBuffer: async () => new ArrayBuffer(0) };
  };
  const ctx = GS.newContext("http://bucket.test/");
  ctx.packs.names = names.slice();
  const obj = await GS.getPackedObject(ctx, sha, true);
  ok(!!obj && obj.type === "blob", "the blob in the last pack is found", obj && obj.type);
  eq(obj ? new TextDecoder().decode(obj.body) : "", text, "and its body reads whole");
  eq(opened.size, 8, "every index was opened on the way");
  ok(peak >= 6, "the indexes open several at a time, not one after the other", "peak in flight " + peak);
  eq(ctx.packs.hits[0], "pack-8", "the hit pack leads the next probe");
  const again = await GS.getPackedObject(ctx, sha, true);
  ok(!!again && again.type === "blob", "a second read of the object resolves from the opened indexes");
  console.log("\n" + pass + " passed, " + fail + " failed");
  process.exit(fail ? 1 : 0);
}
main().catch((e) => { console.error(e); process.exit(1); });

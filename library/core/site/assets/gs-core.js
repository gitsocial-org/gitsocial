// gs-core.js - DOM-free core: object reads, gitmsg parsing, history walks, diff, markdown, routing, on the shared GS namespace

(function () {
  const root = (typeof globalThis !== "undefined") ? globalThis : (typeof window !== "undefined" ? window : this);
  const NS = root.GS || (root.GS = {});

  // Well-known extension data branches.
  const EXT_BRANCHES = {
    social: "refs/heads/gitmsg/social",
    pm: "refs/heads/gitmsg/pm",
    review: "refs/heads/gitmsg/review",
    release: "refs/heads/gitmsg/release",
    memo: "refs/heads/gitmsg/memo",
  };
  const WALK_CAP = 200;
  // DETAIL_WALK_CAP bounds the deep single-target walks: item permalinks, thread sources, a PR's merge-base search.
  const DETAIL_WALK_CAP = 2000;
  const CONCURRENCY = 6;
  // HYDRATE_CONCURRENCY bounds the body-hydration fan-out; higher than CONCURRENCY, since hydration is a flat list of small reads.
  const HYDRATE_CONCURRENCY = 16;

  // deriveBase returns the absolute directory the repo is served from: the ?base=/?repo= override, then window.__gsBase, then the page's own directory.
  function deriveBase(loc) {
    const params = new URLSearchParams(loc.search || "");
    const override = params.get("base") || params.get("repo");
    if (override) return override.endsWith("/") ? override : override + "/";
    if (root.__gsBase) return root.__gsBase;
    const path = loc.pathname || "/";
    const dir = path.slice(0, path.lastIndexOf("/") + 1);
    return (loc.origin || "") + dir;
  }

  // repoTitle names a site with no configured title: the base URL's last path segment, else its host. Mirrors sitePageDefaultTitle in site_pages.go.
  function repoTitle(base) {
    try {
      const u = new URL(base);
      const segs = u.pathname.split("/").filter(Boolean);
      return segs.length ? segs[segs.length - 1] : (u.hostname || "repository");
    } catch { return "repository"; }
  }

  // FETCH_TRIES bounds attempts per request; FETCH_HEADERS_MS bounds one attempt's wait for response headers, not its body.
  const FETCH_TRIES = 4;
  const FETCH_HEADERS_MS = 20000;

  // fetchHTTP retries 429, 5xx and header timeouts with jittered backoff; transport errors fail at once.
  async function fetchHTTP(url, opts) {
    for (let attempt = 1; ; attempt++) {
      const ac = typeof AbortController !== "undefined" ? new AbortController() : null;
      const timer = ac ? setTimeout(() => ac.abort(), FETCH_HEADERS_MS) : null;
      let res = null, err = null;
      try {
        res = await fetch(url, ac ? Object.assign({}, opts, { signal: ac.signal }) : opts);
      } catch (e) { err = e; } finally { if (timer) clearTimeout(timer); }
      if (res && res.status !== 429 && res.status < 500) return res;
      if (err && !(err.name === "AbortError")) throw err;
      if (attempt >= FETCH_TRIES) {
        if (err) throw err;
        return res;
      }
      await new Promise((r) => setTimeout(r, 250 * Math.pow(2, attempt) * (1 + Math.random())));
    }
  }

  // fetchBytes GETs a bucket key relative to base: null on 404, a forbidden-tagged throw on 401 and 403.
  async function fetchBytes(base, key) {
    // The default cache mode honors the server's Cache-Control: immutable objects come from disk, mutable keys revalidate.
    const res = await fetchHTTP(base + key);
    if (res.status === 404) return null;
    if (res.status === 401 || res.status === 403) {
      const err = new Error("GET " + key + ": " + res.status + " forbidden");
      err.forbidden = true;
      throw err;
    }
    if (!res.ok) throw new Error("GET " + key + ": " + res.status);
    return new Uint8Array(await res.arrayBuffer());
  }

  // keyExists answers whether a key is there without paying for its body; only a 404 counts as absent.
  async function keyExists(base, key) {
    const res = await fetchHTTP(base + key, { method: "HEAD" });
    return res.status !== 404;
  }

  // fetchText GETs a key and returns trimmed text; null on 404.
  async function fetchText(base, key) {
    const bytes = await fetchBytes(base, key);
    if (bytes === null) return null;
    return new TextDecoder().decode(bytes).trim();
  }

  // inflate zlib-decompresses git loose-object bytes.
  async function inflate(bytes) {
    const ds = new DecompressionStream("deflate");
    const writer = ds.writable.getWriter();
    writer.write(bytes);
    writer.close();
    return new Uint8Array(await new Response(ds.readable).arrayBuffer());
  }

  // parseLooseObject splits a decompressed object into its type header and its raw body bytes.
  function parseLooseObject(raw) {
    let nul = -1;
    for (let i = 0; i < raw.length; i++) {
      if (raw[i] === 0) { nul = i; break; }
    }
    if (nul < 0) throw new Error("loose object: missing header");
    const head = new TextDecoder().decode(raw.subarray(0, nul));
    const space = head.indexOf(" ");
    return { type: head.slice(0, space), body: raw.subarray(nul + 1) };
  }

  // objectKey maps a 40-hex sha to its bucket key.
  function objectKey(sha) {
    return "objects/" + sha.slice(0, 2) + "/" + sha.slice(2);
  }

  // getObject fetches, inflates and caches one git object; the pack listing orders the loose and packed lookups, and each falls back to the other.
  async function getObject(ctx, sha, content) {
    if (ctx.objects.has(sha)) return ctx.objects.get(sha);
    // The cache holds the in-flight promise, so concurrent readers of one sha share a single fetch.
    const pending = (async () => {
      if (await bucketIsPacked(ctx)) {
        const packed = await getPackedObject(ctx, sha, content);
        // A miss in every pack still has a loose key to try: a bucket may carry objects loose until its next seal.
        return packed !== null ? packed : getLooseObject(ctx, sha);
      }
      const loose = await getLooseObject(ctx, sha);
      return loose !== null ? loose : getPackedObject(ctx, sha, content);
    })();
    ctx.objects.set(sha, pending);
    // A rejection is not cached, so a transient failure recovers on the next call.
    pending.catch(() => { if (ctx.objects.get(sha) === pending) ctx.objects.delete(sha); });
    return pending;
  }

  // getContentObject fetches a sha known to be a tree or a blob; the pack map indexes commits and tags only, so it is skipped.
  function getContentObject(ctx, sha) {
    return getObject(ctx, sha, true);
  }

  // getStateObject fetches a sha a state ref points at (config, lists, forks).
  function getStateObject(ctx, sha) {
    return getObject(ctx, sha);
  }

  // getLooseObject fetches and inflates one loose object, or null when the bucket carries no loose key for it.
  async function getLooseObject(ctx, sha) {
    const compressed = await fetchBytes(ctx.base, objectKey(sha));
    return compressed === null ? null : parseLooseObject(await inflate(compressed));
  }

  // ---- Packfile reader (see documentation/STATIC-SITE.md) ----

  // fetchRange GETs a byte range of a bucket key and returns the bytes with the object's total size; end is exclusive, null runs to the end.
  async function fetchRange(base, key, start, end) {
    const spec = "bytes=" + start + "-" + (end === null ? "" : end - 1);
    const res = await fetchHTTP(base + key, { headers: { Range: spec } });
    if (res.status === 404) return null;
    if (!res.ok) throw new Error("GET " + key + " " + spec + ": " + res.status);
    const body = new Uint8Array(await res.arrayBuffer());
    const range = res.headers.get("Content-Range") || "";
    const total = Number(range.slice(range.indexOf("/") + 1)) || 0;
    if (res.status === 206) return { bytes: body, total };
    return { bytes: body.subarray(start, end === null ? body.length : end), total: body.length };
  }

  // packNames lists the bucket's packs from objects/info/packs, cached per context.
  async function packNames(ctx) {
    if (ctx.packs.names) return ctx.packs.names;
    const text = await fetchText(ctx.base, "objects/info/packs");
    const names = [];
    for (const line of (text || "").split("\n")) {
      const m = /^P (pack-[0-9a-f]+)\.pack$/.exec(line.trim());
      if (m) names.push(m[1]);
    }
    ctx.packs.names = names;
    return names;
  }

  // bucketIsPacked resolves once per context from the pack listing, held as a promise so concurrent readers share it.
  function bucketIsPacked(ctx) {
    if (!ctx.packs.packed) {
      const pending = packNames(ctx).then((names) => names.length > 0);
      pending.catch(() => { if (ctx.packs.packed === pending) ctx.packs.packed = null; });
      ctx.packs.packed = pending;
    }
    return ctx.packs.packed;
  }

  // packMapShard loads the pack map shard covering a sha, cached per context; null when absent or written by another schema version.
  async function packMapShard(ctx, sha) {
    const name = sha.slice(0, 2);
    if (ctx.packs.maps.has(name)) return ctx.packs.maps.get(name);
    const promise = (async () => {
      const bytes = await fetchBytes(ctx.base, ".gitsocial/packmap/" + name + ".json");
      if (bytes === null) return null;
      try {
        const doc = JSON.parse(new TextDecoder().decode(bytes));
        return doc && doc.version === 1 && doc.offsets ? doc : null;
      } catch (_) { return null; }
    })();
    ctx.packs.maps.set(name, promise);
    return promise;
  }

  // IDX_HEAD_BYTES is the leading slice a pack index is opened with, sized so a small index arrives whole in that one request.
  const IDX_HEAD_BYTES = 4096;
  // IDX_SHA_START is where a v2 index's sorted sha table begins.
  const IDX_SHA_START = 8 + 256 * 4;

  // parsePackIdxHead reads a v2 index's fanout, which bounds a sha to one slice of the sorted sha table.
  function parsePackIdxHead(bytes, total) {
    if (bytes.length < IDX_SHA_START) throw new Error("pack index: truncated head");
    const dv = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
    if (dv.getUint32(0) !== 0xff744f63 || dv.getUint32(4) !== 2) throw new Error("pack index: not a v2 index");
    const fanout = new Uint32Array(256);
    for (let i = 0; i < 256; i++) fanout[i] = dv.getUint32(8 + i * 4);
    const count = fanout[255];
    return { count, fanout, total, ofsStart: IDX_SHA_START + count * 24, shas: null, offsets: null, sorted: null, slices: new Map(), ofsSlices: new Map() };
  }

  // setPackIdxOffsets decodes the offset table and its large-offset overflow, keeping the offsets sorted ascending.
  function setPackIdxOffsets(idx, tail) {
    const dv = new DataView(tail.buffer, tail.byteOffset, tail.byteLength);
    const bigStart = idx.count * 4;
    const offsets = new Array(idx.count);
    for (let i = 0; i < idx.count; i++) {
      const raw = dv.getUint32(i * 4);
      offsets[i] = (raw & 0x80000000) ? Number(dv.getBigUint64(bigStart + (raw & 0x7fffffff) * 8)) : raw;
    }
    idx.offsets = offsets;
    idx.sorted = offsets.slice().sort((a, b) => a - b);
  }

  // packIdxOpen reads a pack index's head, cached per context; an index that fits in the head request is adopted whole.
  async function packIdxOpen(ctx, name) {
    if (ctx.packs.idx.has(name)) return ctx.packs.idx.get(name);
    const promise = (async () => {
      const got = await fetchRange(ctx.base, "objects/pack/" + name + ".idx", 0, IDX_HEAD_BYTES);
      if (got === null) return null;
      const idx = parsePackIdxHead(got.bytes, got.total || got.bytes.length);
      if (got.bytes.length >= idx.total && idx.total >= idx.ofsStart + idx.count * 4) {
        idx.shas = got.bytes.subarray(IDX_SHA_START, IDX_SHA_START + idx.count * 20);
        setPackIdxOffsets(idx, got.bytes.subarray(idx.ofsStart));
      }
      return idx;
    })();
    ctx.packs.idx.set(name, promise);
    return promise;
  }

  // packIdxOfsSlice returns the offset-table slice for one first sha byte, a small Range GET cached per byte.
  function packIdxOfsSlice(ctx, idx, name, first, lo, hi) {
    if (idx.ofsSlices.has(first)) return idx.ofsSlices.get(first);
    const promise = (async () => {
      const got = await fetchRange(ctx.base, "objects/pack/" + name + ".idx", idx.ofsStart + lo * 4, idx.ofsStart + hi * 4);
      return got === null ? null : got.bytes;
    })();
    idx.ofsSlices.set(first, promise);
    promise.catch(() => { if (idx.ofsSlices.get(first) === promise) idx.ofsSlices.delete(first); });
    return promise;
  }

  // packIdxOfsAt decodes one offset-table entry, following the large-offset overflow with its own range read.
  async function packIdxOfsAt(ctx, idx, name, ofsBytes, at) {
    const dv = new DataView(ofsBytes.buffer, ofsBytes.byteOffset, ofsBytes.byteLength);
    const raw = dv.getUint32(at * 4);
    if (!(raw & 0x80000000)) return raw;
    const bigAt = idx.ofsStart + idx.count * 4 + (raw & 0x7fffffff) * 8;
    const got = await fetchRange(ctx.base, "objects/pack/" + name + ".idx", bigAt, bigAt + 8);
    if (got === null || got.bytes.length < 8) return null;
    const bdv = new DataView(got.bytes.buffer, got.bytes.byteOffset, got.bytes.byteLength);
    return Number(bdv.getBigUint64(0));
  }

  // packIdxSlice returns the sorted-sha-table slice for one first sha byte, a small Range GET cached per byte.
  async function packIdxSlice(ctx, idx, name, first, lo, hi) {
    if (idx.shas) return idx.shas.subarray(lo * 20, hi * 20);
    if (idx.slices.has(first)) return idx.slices.get(first);
    const promise = (async () => {
      const got = await fetchRange(ctx.base, "objects/pack/" + name + ".idx", IDX_SHA_START + lo * 20, IDX_SHA_START + hi * 20);
      return got === null ? null : got.bytes;
    })();
    idx.slices.set(first, promise);
    return promise;
  }

  // packIdxFind binary-searches a 20-byte-per-entry sha table slice and returns the position within it, or -1.
  function packIdxFind(shas, count, sha) {
    const want = new Uint8Array(20);
    for (let i = 0; i < 20; i++) want[i] = parseInt(sha.slice(i * 2, i * 2 + 2), 16);
    let lo = 0, hi = count - 1;
    while (lo <= hi) {
      const mid = (lo + hi) >> 1;
      let cmp = 0;
      for (let i = 0; i < 20 && cmp === 0; i++) cmp = shas[mid * 20 + i] - want[i];
      if (cmp === 0) return mid;
      if (cmp < 0) lo = mid + 1; else hi = mid - 1;
    }
    return -1;
  }

  // packIdxLookup locates a sha in one pack and returns its byte range; end is undefined when a per-slice read cannot name it, and readPackEntry bounds the read instead.
  async function packIdxLookup(ctx, name, sha) {
    const opened = await packIdxOpen(ctx, name);
    if (!opened) return null;
    const first = parseInt(sha.slice(0, 2), 16);
    const lo = first === 0 ? 0 : opened.fanout[first - 1];
    const hi = opened.fanout[first];
    if (lo >= hi) return null;
    const ofsPending = opened.offsets ? null : packIdxOfsSlice(ctx, opened, name, first, lo, hi);
    if (ofsPending) ofsPending.catch(() => {});
    const shas = await packIdxSlice(ctx, opened, name, first, lo, hi);
    if (shas === null) return null;
    const at = packIdxFind(shas, hi - lo, sha);
    if (at < 0) return null;
    if (opened.offsets) {
      const offset = opened.offsets[lo + at];
      return { offset, end: packEntryEnd(opened, offset) };
    }
    const ofsBytes = await ofsPending.catch(() => null);
    if (ofsBytes === null || ofsBytes.length < (hi - lo) * 4) return null;
    const offset = await packIdxOfsAt(ctx, opened, name, ofsBytes, at);
    return offset === null ? null : { offset, end: undefined };
  }

  // packEntryEnd returns an entry's exclusive end, the next entry's start; null for the last entry, and answerable only with the full offset table resident.
  function packEntryEnd(idx, offset) {
    const s = idx.sorted;
    let lo = 0, hi = s.length - 1, at = -1;
    while (lo <= hi) {
      const mid = (lo + hi) >> 1;
      if (s[mid] === offset) { at = mid; break; }
      if (s[mid] < offset) lo = mid + 1; else hi = mid - 1;
    }
    if (at < 0 || at + 1 >= s.length) return null;
    return s[at + 1];
  }

  // PACK_TYPES maps a pack entry's 3-bit type code to a git object type; 6 and 7 are deltas.
  const PACK_TYPES = { 1: "commit", 2: "tree", 3: "blob", 4: "tag" };

  // PACK_WINDOW_BYTES is the aligned granule pack data is range-read in, so clustered reads share window GETs; a span over PACK_SPAN_MAX reads one exact range.
  const PACK_WINDOW_BYTES = 16384;
  const PACK_SPAN_MAX = PACK_WINDOW_BYTES * 4;

  // packWindow fetches one aligned window of a packfile, cached as an in-flight promise; null past the end of the pack.
  function packWindow(ctx, name, winStart) {
    let wins = ctx.packs.windows.get(name);
    if (!wins) { wins = new Map(); ctx.packs.windows.set(name, wins); }
    if (wins.has(winStart)) return wins.get(winStart);
    const promise = (async () => {
      const known = ctx.packs.size.get(name) || 0;
      if (known && winStart >= known) return null;
      const got = await fetchRange(ctx.base, "objects/pack/" + name + ".pack", winStart, winStart + PACK_WINDOW_BYTES);
      if (got && got.total) ctx.packs.size.set(name, got.total);
      return got;
    })();
    wins.set(winStart, promise);
    promise.catch(() => { if (wins.get(winStart) === promise) wins.delete(winStart); });
    return promise;
  }

  // packRange reads [start, end) through the window cache and returns { bytes, total }; null when the pack is absent.
  async function packRange(ctx, name, start, end) {
    if (end - start > PACK_SPAN_MAX) return fetchRange(ctx.base, "objects/pack/" + name + ".pack", start, end);
    const first = Math.floor(start / PACK_WINDOW_BYTES) * PACK_WINDOW_BYTES;
    const starts = [];
    for (let ws = first; ws < end; ws += PACK_WINDOW_BYTES) starts.push(ws);
    const wins = await Promise.all(starts.map((ws) => packWindow(ctx, name, ws)));
    if (wins[0] === null) return null;
    if (starts.length === 1) {
      const got = wins[0];
      const to = Math.max(start - first, Math.min(end - first, got.bytes.length));
      return { bytes: got.bytes.subarray(start - first, to), total: got.total || 0 };
    }
    let total = 0;
    let covered = first;
    const parts = [];
    for (let k = 0; k < wins.length; k++) {
      const got = wins[k];
      if (got === null) break;
      if (got.total) total = got.total;
      parts.push(got.bytes);
      covered = starts[k] + got.bytes.length;
      if (got.bytes.length < PACK_WINDOW_BYTES) break;
    }
    const upto = Math.min(end, covered);
    if (upto <= start) return { bytes: new Uint8Array(0), total };
    const out = new Uint8Array(upto - start);
    let at = 0;
    for (let k = 0; k < parts.length && at < out.length; k++) {
      const ws = starts[k];
      const from = Math.max(start, ws) - ws;
      const to = Math.min(upto, ws + parts[k].length) - ws;
      if (to <= from) continue;
      out.set(parts[k].subarray(from, to), at);
      at += to - from;
    }
    return { bytes: out, total };
  }

  // tryInflate inflates until expectedSize bytes are produced: the output, "small" when the stream needed more input, or "big" when trailing bytes errored it.
  async function tryInflate(bytes, expectedSize) {
    const ds = new DecompressionStream("deflate");
    const writer = ds.writable.getWriter();
    let wErr = false;
    const wp = writer.write(bytes).catch(() => { wErr = true; });
    const cp = writer.close().catch(() => {});
    const reader = ds.readable.getReader();
    const chunks = [];
    let got = 0;
    try {
      while (got < expectedSize) {
        const { done, value } = await reader.read();
        if (done) break;
        chunks.push(value);
        got += value.length;
      }
    } catch (_) { /* errored stream: junk after the end, or a truncated tail */ }
    reader.cancel().catch(() => {});
    await wp; await cp;
    if (got < expectedSize) return wErr ? "big" : "small";
    const out = new Uint8Array(got);
    let at = 0;
    for (const c of chunks) { out.set(c, at); at += c.length; }
    return out.subarray(0, expectedSize);
  }

  // inflateBounded inflates a zlib stream whose end is unknown, binary-searching the stream end when the engine rejects the trailing bytes.
  async function inflateBounded(bytes, expectedSize) {
    const whole = await tryInflate(bytes, expectedSize);
    if (whole !== "big") return whole;
    let lo = 0, hi = bytes.length;
    while (lo + 1 < hi) {
      const mid = (lo + hi) >> 1;
      const r = await tryInflate(bytes.subarray(0, mid), expectedSize);
      if (r === "small") lo = mid;
      else if (r === "big") hi = mid;
      else return r;
    }
    throw new Error("pack entry: could not locate stream end");
  }

  // parsePackEntryHead decodes a pack entry's type and size varint header plus a delta's base reference; size is the inflated length.
  function parsePackEntryHead(raw) {
    let i = 0;
    let b = raw[i++];
    const type = (b >> 4) & 7;
    let size = b & 15, shift = 4;
    while (b & 0x80) { b = raw[i++]; size += (b & 0x7f) * Math.pow(2, shift); shift += 7; }
    let back = 0, baseSha = "";
    if (type === 6) { // OFS_DELTA: base is at a negative offset from this entry
      b = raw[i++];
      back = b & 0x7f;
      while (b & 0x80) { b = raw[i++]; back = ((back + 1) << 7) | (b & 0x7f); }
    } else if (type === 7) { // REF_DELTA: base named by sha
      for (let b2 = 0; b2 < 20; b2++) baseSha += raw[i + b2].toString(16).padStart(2, "0");
      i += 20;
    }
    return { type, size, dataStart: i, back, baseSha };
  }

  // finishPackEntry resolves a parsed entry whose data is inflated, applying a delta against its base.
  async function finishPackEntry(ctx, name, offset, head, body) {
    if (head.type === 6) {
      const baseOffset = offset - head.back;
      const opened = await packIdxOpen(ctx, name);
      const baseEnd = opened && opened.offsets ? packEntryEnd(opened, baseOffset) : undefined;
      const base = await readPackEntry(ctx, name, baseOffset, baseEnd);
      if (!base) return null;
      return { type: base.type, body: applyDelta(base.body, body) };
    }
    if (head.type === 7) {
      const base = await getObject(ctx, head.baseSha);
      if (!base) return null;
      return { type: base.type, body: applyDelta(base.body, body) };
    }
    if (!PACK_TYPES[head.type]) throw new Error("pack entry: unknown type " + head.type);
    return { type: PACK_TYPES[head.type], body };
  }

  // readPackEntry reads one pack entry and resolves its delta chain; end is the exclusive end, null for the last entry, undefined when unknown.
  async function readPackEntry(ctx, name, offset, end) {
    if (end === undefined) return readPackEntryScan(ctx, name, offset);
    const known = ctx.packs.size.get(name) || 0;
    if (end === null && known) end = known - 20;
    const got = end === null
      ? await fetchRange(ctx.base, "objects/pack/" + name + ".pack", offset, null)
      : await packRange(ctx, name, offset, end);
    if (got === null) return null;
    if (got.total) ctx.packs.size.set(name, got.total);
    let raw = got.bytes;
    if (end === null) raw = raw.subarray(0, raw.length - 20); // trailing pack checksum
    const head = parsePackEntryHead(raw);
    return finishPackEntry(ctx, name, offset, head, await inflate(raw.subarray(head.dataStart)));
  }

  // readPackEntryScan reads an entry of unknown end, bounding the read from the header's inflated size.
  async function readPackEntryScan(ctx, name, offset) {
    const got = await packRange(ctx, name, offset, offset + PACK_WINDOW_BYTES);
    if (got === null || !got.bytes.length) return null;
    if (got.total) ctx.packs.size.set(name, got.total);
    let raw = got.bytes;
    const head = parsePackEntryHead(raw);
    const total = ctx.packs.size.get(name) || 0;
    const cap = total ? total - offset : Infinity;
    let want = Math.min(head.dataStart + head.size + (head.size >> 9) + 64, cap);
    for (;;) {
      while (raw.length < want) {
        const more = await packRange(ctx, name, offset + raw.length, offset + want);
        if (more === null || !more.bytes.length) break;
        const joined = new Uint8Array(raw.length + more.bytes.length);
        joined.set(raw);
        joined.set(more.bytes, raw.length);
        raw = joined;
      }
      const body = await inflateBounded(raw.subarray(head.dataStart), head.size);
      if (body !== "small") return finishPackEntry(ctx, name, offset, head, body);
      // A short read means the pack ended before the bound, so the entry is truncated.
      if (raw.length < want || raw.length >= cap) throw new Error("pack entry: truncated stream at " + offset);
      want = Math.min(Math.max(want * 2, raw.length + PACK_WINDOW_BYTES), cap);
    }
  }

  // applyDelta reconstructs an object from a base and git's delta encoding: copy-from-base and insert-literal instructions.
  function applyDelta(base, delta) {
    let i = 0;
    const varint = () => {
      let value = 0, shift = 0, b;
      do { b = delta[i++]; value |= (b & 0x7f) << shift; shift += 7; } while (b & 0x80);
      return value;
    };
    varint(); // base size, implied by the base we already hold
    const out = new Uint8Array(varint());
    let at = 0;
    while (i < delta.length) {
      const cmd = delta[i++];
      if (cmd & 0x80) {
        let offset = 0, size = 0;
        for (let bit = 0; bit < 4; bit++) if (cmd & (1 << bit)) offset |= delta[i++] << (bit * 8);
        for (let bit = 0; bit < 3; bit++) if (cmd & (1 << (4 + bit))) size |= delta[i++] << (bit * 8);
        if (size === 0) size = 0x10000;
        out.set(base.subarray(offset, offset + size), at);
        at += size;
      } else if (cmd) {
        out.set(delta.subarray(i, i + cmd), at);
        at += cmd;
        i += cmd;
      } else {
        throw new Error("pack delta: reserved instruction 0");
      }
    }
    // The reconstructed length ties back to the delta's own target-size varint.
    if (at !== out.length) throw new Error("pack delta: reconstructed " + at + " of " + out.length + " bytes");
    return out;
  }

  // getPackedObject resolves one object out of the packfiles: the pack map for commits and tags, then each pack's index for the rest.
  async function getPackedObject(ctx, sha, content) {
    const map = content ? null : await packMapShard(ctx, sha);
    const at = map && map.offsets[sha];
    if (at) {
      const name = map.packs[at[0]];
      if (name) return readPackEntry(ctx, name, at[1], at[1] + at[2]);
    }
    // Trees and blobs have no map entry; the pack that answered last is probed first, since content reads cluster.
    const names = await packNames(ctx);
    const ordered = ctx.packs.lastHit ? [ctx.packs.lastHit, ...names.filter((n) => n !== ctx.packs.lastHit)] : names;
    for (const name of ordered) {
      const found = await packIdxLookup(ctx, name, sha);
      if (found === null) continue;
      ctx.packs.lastHit = name;
      return readPackEntry(ctx, name, found.offset, found.end);
    }
    return null;
  }

  // parseCommit turns a commit object into structured fields plus its clean content and parsed gitmsg header.
  function parseCommit(sha, body) {
    const text = new TextDecoder().decode(body);
    const split = text.indexOf("\n\n");
    const headerBlock = split < 0 ? text : text.slice(0, split);
    const message = split < 0 ? "" : text.slice(split + 2);
    const parents = [];
    let tree = "", authorName = "", authorEmail = "", authorTime = 0;
    for (const line of headerBlock.split("\n")) {
      if (line.startsWith("tree ")) tree = line.slice(5).trim();
      else if (line.startsWith("parent ")) parents.push(line.slice(7).trim());
      else if (line.startsWith("author ")) {
        const m = /^author (.*) <([^>]*)> (\d+) /.exec(line);
        if (m) { authorName = m[1]; authorEmail = m[2]; authorTime = parseInt(m[3], 10); }
      }
    }
    return {
      hash: sha, short: sha.slice(0, 12), tree, parents,
      authorName, authorEmail, authorTime,
      content: cleanContent(message),
      rawMessage: message.replace(/\r/g, ""),
      gitmsg: parseGitmsg(message),
      refs: parseRefs(message),
    };
  }

  // cleanContent strips the gitmsg trailer block, leaving user text.
  function cleanContent(message) {
    let idx = -1;
    if (message.startsWith("GitMsg: ")) idx = 0;
    else { const i = message.indexOf("\nGitMsg: "); if (i !== -1) idx = i; }
    const content = idx === -1 ? message : message.slice(0, idx);
    return content.replace(/\r/g, "").trim();
  }

  // parseGitmsg extracts the GitMsg trailer into a flat key to value map, or null when the commit carries no header.
  function parseGitmsg(message) {
    let idx = -1;
    if (message.startsWith("GitMsg: ")) idx = 0;
    else { const i = message.indexOf("\nGitMsg: "); if (i !== -1) idx = i + 1; }
    if (idx === -1) return null;
    const line = message.slice(idx).split("\n")[0];
    const header = {};
    const re = /([a-zA-Z_][a-zA-Z0-9_:-]*)="([^"]*)"/g;
    let m;
    while ((m = re.exec(line)) !== null) header[m[1]] = m[2];
    if (!header.ext || !header.v) return null;
    return header;
  }

  // parseRefs extracts GitMsg-Ref trailers with their quoted origin content, the only view a reader has of a cross-repo original.
  function parseRefs(message) {
    const lines = (message || "").replace(/\r/g, "").split("\n");
    const refs = [];
    const re = /([a-zA-Z_][a-zA-Z0-9_:-]*)="([^"]*)"/g;
    for (let i = 0; i < lines.length; i++) {
      if (!lines[i].startsWith("GitMsg-Ref:")) continue;
      const fields = {};
      re.lastIndex = 0;
      let m;
      while ((m = re.exec(lines[i])) !== null) fields[m[1]] = m[2];
      const quoted = [];
      let j = i + 1;
      while (j < lines.length && /^ *>/.test(lines[j])) { quoted.push(lines[j].replace(/^ *> ?/, "")); j++; }
      fields.quoted = quoted.join("\n");
      refs.push(fields);
      i = j - 1;
    }
    return refs;
  }

  // refRepoUrl returns the repo-url prefix of a "[url]#type:value" ref, or "" for a workspace-relative one.
  function refRepoUrl(ref) {
    const s = ref || "";
    const h = s.indexOf("#");
    return h > 0 ? s.slice(0, h) : "";
  }

  // resolveRef reads a plain etag-mode ref key (content is "<40-hex>\n").
  async function resolveRef(base, refName) {
    const text = await fetchText(base, refName);
    if (!text) return null;
    return /^[0-9a-f]{40}$/.test(text) ? text : null;
  }

  // resolveHead reads the HEAD symref and returns { branch, sha } for the default branch it points at.
  async function resolveHead(base) {
    const text = await fetchText(base, "HEAD");
    if (!text) return null;
    if (text.startsWith("ref:")) {
      const branch = text.slice(4).trim();
      return { branch, sha: await resolveRef(base, branch) };
    }
    return /^[0-9a-f]{40}$/.test(text) ? { branch: null, sha: text } : null;
  }

  // headFor memoizes resolveHead per context, holding the in-flight promise so concurrent callers share one resolution.
  function headFor(ctx) {
    if (ctx.head === undefined) ctx.head = resolveHead(ctx.base);
    return ctx.head;
  }

  // startWalk seeds a resumable history walk at a tip; an empty frontier means the history is fully walked.
  function startWalk(tipSha) {
    return { visited: new Set(), frontier: [tipSha], commits: [] };
  }

  // walkStep advances a walk by up to windowCap commits, breadth-first over parents with bounded concurrency.
  async function walkStep(ctx, state, windowCap) {
    windowCap = windowCap || WALK_CAP;
    const start = state.commits.length;
    while (state.frontier.length && state.commits.length - start < windowCap) {
      const batch = [];
      while (state.frontier.length && batch.length < CONCURRENCY) {
        const h = state.frontier.shift();
        if (h && !state.visited.has(h)) { state.visited.add(h); batch.push(h); }
      }
      const objs = await Promise.all(batch.map((h) => getObject(ctx, h)));
      const nextParents = [];
      for (let i = 0; i < batch.length; i++) {
        const obj = objs[i];
        if (!obj || obj.type !== "commit") continue;
        const commit = parseCommit(batch[i], obj.body);
        state.commits.push(commit);
        for (const p of commit.parents) if (!state.visited.has(p)) nextParents.push(p);
      }
      state.frontier = nextParents.concat(state.frontier);
    }
    return state;
  }

  // walkedCommits returns a newest-first copy of a walk state's accumulated commits.
  function walkedCommits(state) {
    return state.commits.slice().sort((a, b) => b.authorTime - a.authorTime);
  }

  // walkHistory walks parent pointers from a tip in one capped window, newest-first by author time.
  async function walkHistory(ctx, tipSha, cap = WALK_CAP) {
    const state = startWalk(tipSha);
    await walkStep(ctx, state, cap);
    return walkedCommits(state).slice(0, cap);
  }

  // refHash pulls the 12-hex commit hash out of a gitmsg ref value.
  function refHash(ref) {
    const m = /commit:([0-9a-f]{7,40})/.exec(ref || "");
    return m ? m[1].slice(0, 12) : null;
  }

  // anyRefHash pulls the hash out of a ref of any type, which a relation field may carry.
  function anyRefHash(ref) {
    const m = /[#:]([0-9a-f]{7,40})(?:@|$)/.exec(ref || "");
    return m ? m[1].slice(0, 12) : refHash(ref);
  }

  // parseBranchField splits a PR base or head field into its repo url and branch name.
  function parseBranchField(field) {
    const s = field || "";
    const hash = s.indexOf("#");
    const url = hash > 0 ? s.slice(0, hash) : "";
    const rest = hash >= 0 ? s.slice(hash + 1) : s;
    const m = /^branch:(.+)$/.exec(rest);
    return { url, name: m ? m[1] : "" };
  }

  // effectiveTime returns an item's display timestamp, preferring an imported item's origin-time over the git author time.
  function effectiveTime(commit, header) {
    const ot = header && header["origin-time"];
    if (ot) { const ms = Date.parse(ot); if (!isNaN(ms)) return Math.floor(ms / 1000); }
    return (commit && commit.authorTime) || 0;
  }

  // ORIGIN_KEYS are the provenance fields fixed at import (GITMSG 1.9); an edit leaves them as the canonical set them.
  const ORIGIN_KEYS = ["origin-author-name", "origin-author-email", "origin-platform", "origin-time", "origin-url"];

  // originHandle derives an @handle from an origin author email, mirroring protocol.OriginDisplayAuthor.
  function originHandle(email) {
    if (!email) return "";
    if (email.endsWith("@users.noreply.github.com")) {
      let login = email.slice(0, -"@users.noreply.github.com".length);
      const plus = login.indexOf("+");
      if (plus >= 0) login = login.slice(plus + 1);
      return "@" + login;
    }
    const at = email.indexOf("@");
    return "@" + (at > 0 ? email.slice(0, at) : email);
  }

  // effectiveAuthor returns an item's display author, preferring the origin author over the git commit author.
  function effectiveAuthor(commit, header) {
    header = header || {};
    if (header["origin-author-name"]) return header["origin-author-name"];
    const h = originHandle(header["origin-author-email"]);
    if (h) return h;
    return (commit && (commit.authorName || commit.authorEmail)) || "unknown";
  }

  // authorLabel picks a meta row's author label: the display name, else the email, else "unknown".
  function authorLabel(name, email) {
    return name || email || "unknown";
  }

  // effectiveAuthorEmail returns the identity email an item is attributed to, origin email over git email.
  function effectiveAuthorEmail(commit, header) {
    header = header || {};
    return header["origin-author-email"] || (commit && commit.authorEmail) || "";
  }

  // eqFold compares two strings case-insensitively after trimming.
  function eqFold(a, b) { return (a || "").trim().toLowerCase() === (b || "").trim().toLowerCase(); }

  // makeVersion builds one entry of an item's version list.
  function makeVersion(commit, header, isEdit, author, editorName, effTime, content) {
    return { commit, header, content, rawMessage: commit.rawMessage, author, editorName, edited: isEdit, effectiveTime: effTime };
  }

  // buildVersions returns an item's ordered version list: the canonical first, then each edit oldest first.
  function buildVersions(canon, canonHeader, edits, author) {
    const canonEmail = effectiveAuthorEmail(canon, canonHeader);
    const mergeHeader = (own) => {
      const h = Object.assign({}, canonHeader, own || {});
      for (const k of ORIGIN_KEYS) { if (canonHeader[k] !== undefined) h[k] = canonHeader[k]; else delete h[k]; }
      return h;
    };
    const out = [makeVersion(canon, Object.assign({}, canonHeader), false, author, "", effectiveTime(canon, canonHeader), canon.content)];
    for (const e of edits) {
      const editEmail = effectiveAuthorEmail(e, e.gitmsg);
      const editorName = eqFold(editEmail, canonEmail) ? "" : effectiveAuthor(e, e.gitmsg);
      const content = e.content || canon.content;
      out.push(makeVersion(e, mergeHeader(e.gitmsg), true, author, editorName, effectiveTime(e, e.gitmsg || {}), content));
    }
    return out;
  }

  // resolveItems applies same-repo edit resolution: the latest edit per canonical wins, a retraction drops the item, an orphan edit stands alone.
  function resolveItems(commits) {
    const chron = commits.slice().reverse();
    const editsFor = new Map();
    const allEditsFor = new Map();
    const canonical = [];
    const byShort = new Set();
    for (const c of chron) byShort.add(c.short);
    for (const c of chron) {
      if (c.gitmsg && c.gitmsg.edits) {
        const t = refHash(c.gitmsg.edits);
        editsFor.set(t, c);
        if (!allEditsFor.has(t)) allEditsFor.set(t, []);
        allEditsFor.get(t).push(c);
      } else canonical.push(c);
    }
    const items = [];
    const consumed = new Set();
    for (const c of canonical) {
      const edit = editsFor.get(c.short);
      const canonHeader = c.gitmsg || {};
      const header = Object.assign({}, canonHeader);
      let content = c.content, rawMessage = c.rawMessage, edited = false, retracted = false, editorName = "", editedTime = 0;
      if (edit) {
        edited = true;
        consumed.add(c.short);
        Object.assign(header, edit.gitmsg);
        // Origin provenance is fixed at import (GITMSG 1.9): keep the canonical's origin fields.
        for (const k of ORIGIN_KEYS) { if (canonHeader[k] !== undefined) header[k] = canonHeader[k]; else delete header[k]; }
        editedTime = effectiveTime(edit, edit.gitmsg || {});
        if (edit.gitmsg.retracted === "true") retracted = true;
        if (edit.content) content = edit.content;
        // The raw view shows the commit whose content is displayed, the edit when one overrides the canonical.
        rawMessage = edit.rawMessage;
        // Show "edited by" only when the editor differs from the original author.
        if (!eqFold(effectiveAuthorEmail(edit, edit.gitmsg), effectiveAuthorEmail(c, canonHeader))) {
          editorName = effectiveAuthor(edit, edit.gitmsg);
        }
      }
      if (retracted) continue;
      const author = effectiveAuthor(c, canonHeader);
      const versions = buildVersions(c, canonHeader, allEditsFor.get(c.short) || [], author);
      items.push({ commit: c, header, content, rawMessage, edited, editorName, editedTime, author, effectiveTime: effectiveTime(c, canonHeader), versions });
    }
    for (const [target, edit] of editsFor) {
      if (byShort.has(target) || consumed.has(target)) continue;
      if (edit.gitmsg.retracted === "true") continue;
      const h = Object.assign({}, edit.gitmsg);
      const author = effectiveAuthor(edit, edit.gitmsg);
      // Orphan edits: the collected edits become the version chain, the first standing in for the missing canonical.
      const orphans = allEditsFor.get(target) || [edit];
      const versions = buildVersions(orphans[0], orphans[0].gitmsg || {}, orphans.slice(1), author);
      items.push({ commit: edit, header: h, content: edit.content, rawMessage: edit.rawMessage, edited: true, editorName: "", editedTime: effectiveTime(edit, h), author, effectiveTime: effectiveTime(edit, h), versions });
    }
    items.sort((a, b) => b.effectiveTime - a.effectiveTime);
    return items;
  }

  // readRefMode reads the bucket's ref-mode marker.
  async function readRefMode(base) {
    return await fetchText(base, ".gitsocial/ref-mode");
  }

  // MANIFEST_KEY is the push-maintained refs manifest; LEGACY_MANIFEST_KEY is its older site copy.
  const MANIFEST_KEY = ".gitsocial/refs.json";
  const LEGACY_MANIFEST_KEY = ".gitsocial/site/refs.json";

  // manifestFor memoizes the refs manifest per context, keeping the raw body beside the parsed map; null when absent or unparseable.
  function manifestFor(ctx) {
    if (ctx.manifest === undefined) {
      ctx.manifest = (async () => {
        ctx.manifestText = await fetchText(ctx.base, MANIFEST_KEY);
        if (ctx.manifestText === null) ctx.manifestText = await fetchText(ctx.base, LEGACY_MANIFEST_KEY);
        if (!ctx.manifestText) return null;
        try { return JSON.parse(ctx.manifestText); } catch { return null; }
      })();
    }
    return ctx.manifest;
  }

  // refTip resolves a ref tip. The manifest is a fast path, not proof of absence: a refname it omits is probed live once per session.
  async function refTip(ctx, refName) {
    const manifest = await manifestFor(ctx);
    const listed = manifest ? manifest[refName] : null;
    if (manifest && !listed) {
      if (ctx.refMisses.has(refName)) return null;
      // Existence first, and by HEAD: a 404 from an object store carries a full error document.
      if (!(await keyExists(ctx.base, refName))) {
        ctx.refMisses.add(refName);
        return null;
      }
    }
    const live = await resolveRef(ctx.base, refName);
    if (live) return live;
    if (manifest && !listed) ctx.refMisses.add(refName);
    return listed && /^[0-9a-f]{40}$/.test(listed) ? listed : null;
  }

  // walkStateFor returns the resumable walk cached on ctx under key, seeded at tip; a changed tip discards the stale walk.
  function walkStateFor(ctx, key, tip) {
    const prev = ctx.walks[key];
    if (prev && prev.tip === tip) return prev;
    const fresh = { tip, state: startWalk(tip) };
    ctx.walks[key] = fresh;
    return fresh;
  }

  // loadItemsIndex fetches an extension's metadata index once per context, the eager set only; null when the bucket carries none.
  async function loadItemsIndex(ctx, ext) {
    if (!ctx.itemsIndex) ctx.itemsIndex = {};
    if (ctx.itemsIndex[ext] !== undefined) return ctx.itemsIndex[ext];
    let idx = null;
    try {
      idx = await loadItemsIndexSharded(ctx, ext);
    } catch (e) { if (e && e.forbidden) throw e; }
    ctx.itemsIndex[ext] = idx;
    return idx;
  }

  // loadItemsIndexSharded reads the manifest and fetches the eager set, the newest sealed shard plus the head, newest-first; null on an unknown version.
  async function loadItemsIndexSharded(ctx, ext) {
    const dir = ".gitsocial/site/items/" + ext + "/";
    const mtext = await fetchText(ctx.base, dir + "manifest.json");
    if (!mtext) return null;
    let m;
    try { m = JSON.parse(mtext); } catch { return null; }
    if (!m || (m.version !== 4 && m.version !== 5) || !/^[0-9a-f]{40}$/.test(m.tip || "") || !Array.isArray(m.shards)) return null;
    const newest = m.shards.length ? m.shards[m.shards.length - 1] : null;
    // Assemble oldest-first (shard, then head) before reversing to the newest-first order resolveItems expects.
    const eagerKeys = [];
    if (newest) eagerKeys.push(dir + newest.key);
    eagerKeys.push(dir + "head.json");
    const texts = await Promise.all(eagerKeys.map((k) => fetchText(ctx.base, k)));
    const items = [];
    for (const t of texts) {
      if (!t) continue;
      let doc;
      try { doc = JSON.parse(t); } catch { continue; }
      if (doc && Array.isArray(doc.items)) for (const it of doc.items) items.push(it);
    }
    items.reverse();
    const residentShas = new Set(items.map((e) => e.sha));
    const olderShards = m.shards.slice(0, Math.max(0, m.shards.length - 1)).map((s) => dir + s.key).reverse();
    return {
      version: m.version, tip: m.tip, complete: m.complete !== false, bodiesBytes: m.bodiesBytes || 0,
      items, residentShas, olderShards, dir, allResident: olderShards.length === 0,
      olderBytes: m.shards.slice(0, Math.max(0, m.shards.length - 1)).reduce((n, s) => n + (s.bytes || 0), 0),
    };
  }

  // loadOlderItemShards merges every not-yet-resident older shard into the loaded index; idempotent.
  async function loadOlderItemShards(ctx, ext) {
    const idx = await loadItemsIndex(ctx, ext);
    if (!idx || idx.allResident || !idx.olderShards.length) return idx;
    const keys = idx.olderShards.slice().reverse(); // oldest→newest
    const texts = await Promise.all(keys.map((k) => fetchText(ctx.base, k)));
    const older = [];
    for (const t of texts) {
      if (!t) continue;
      let doc;
      try { doc = JSON.parse(t); } catch { continue; }
      if (doc && Array.isArray(doc.items)) for (const it of doc.items) older.push(it);
    }
    older.reverse(); // newest-first
    for (const e of older) { if (!idx.residentShas.has(e.sha)) { idx.items.push(e); idx.residentShas.add(e.sha); } }
    idx.olderShards = [];
    idx.allResident = true;
    idx.olderBytes = 0;
    return idx;
  }

  // metaCommit converts one metadata-index entry into a body-less commit record, marked hollow until hydrateItem fills it.
  function metaCommit(e) {
    const header = String(e.header || "");
    return {
      hash: e.sha, short: e.sha.slice(0, 12), tree: "", parents: [],
      authorName: e.author || "", authorEmail: e.email || "", authorTime: e.ts || 0,
      content: "", rawMessage: header, subject: String(e.subject || ""),
      gitmsg: parseGitmsg(header), refs: [], hollow: true,
    };
  }

  // indexCommit converts one full-message entry of the bodies corpus into a commit record.
  function indexCommit(e) {
    const msg = String(e.message || "");
    return {
      hash: e.sha, short: e.sha.slice(0, 12), tree: "", parents: [],
      authorName: e.author || "", authorEmail: e.email || "", authorTime: e.ts || 0,
      content: cleanContent(msg),
      rawMessage: msg.replace(/\r/g, ""),
      gitmsg: parseGitmsg(msg),
      refs: parseRefs(msg),
    };
  }

  // hydrateCommit fills a hollow commit record from its loose object; a missing object clears hollow without a refetch.
  async function hydrateCommit(ctx, commit) {
    if (!commit || !commit.hollow) return;
    const obj = await getObject(ctx, commit.hash);
    if (!obj || obj.type !== "commit") { commit.hollow = false; return; }
    const full = parseCommit(commit.hash, obj.body);
    commit.content = full.content;
    commit.rawMessage = full.rawMessage;
    commit.refs = full.refs;
    commit.tree = full.tree;
    commit.parents = full.parents;
    if (full.gitmsg) commit.gitmsg = full.gitmsg;
    if (full.authorName) commit.authorName = full.authorName;
    if (full.authorEmail) commit.authorEmail = full.authorEmail;
    if (full.authorTime) commit.authorTime = full.authorTime;
    commit.hollow = false;
  }

  // hydrateItem fetches an item's version commit bodies and recomputes its displayed content; idempotent.
  async function hydrateItem(ctx, item) {
    if (!item) return;
    const versions = item.versions || [];
    const commits = [];
    for (const v of versions) if (v.commit && commits.indexOf(v.commit) === -1) commits.push(v.commit);
    if (item.commit && commits.indexOf(item.commit) === -1) commits.push(item.commit);
    for (const c of commits) await hydrateCommit(ctx, c);
    const canon = versions.length ? versions[0].commit : item.commit;
    const canonContent = canon ? canon.content : "";
    for (let i = 0; i < versions.length; i++) {
      const vc = versions[i].commit;
      versions[i].content = i === 0 ? canonContent : ((vc && vc.content) || canonContent);
      if (vc) versions[i].rawMessage = vc.rawMessage;
    }
    if (versions.length) {
      const last = versions[versions.length - 1];
      item.content = last.content; item.rawMessage = last.rawMessage;
    } else if (item.commit) {
      item.content = item.commit.content; item.rawMessage = item.commit.rawMessage;
    }
  }

  // hydrateItems hydrates a set of items' bodies with bounded concurrency.
  async function hydrateItems(ctx, items) {
    const list = (items || []).filter(Boolean);
    let i = 0;
    const worker = async () => { while (i < list.length) { const it = list[i++]; await hydrateItem(ctx, it); } };
    await Promise.all(Array.from({ length: Math.min(HYDRATE_CONCURRENCY, list.length) }, worker));
  }

  // bridgeToIndex returns the commits between a live tip and an index's tip; null when that tip is not met within WALK_CAP.
  async function bridgeToIndex(ctx, tip, idx, known) {
    const visited = new Set();
    let frontier = [tip];
    const out = [];
    let metTip = false;
    while (frontier.length && out.length < WALK_CAP) {
      const h = frontier.shift();
      if (!h || visited.has(h)) continue;
      visited.add(h);
      if (h === idx.tip || known.has(h)) { if (h === idx.tip) metTip = true; continue; }
      const obj = await getObject(ctx, h);
      if (!obj || obj.type !== "commit") continue;
      const commit = parseCommit(h, obj.body);
      out.push(commit);
      frontier = commit.parents.concat(frontier);
    }
    return metTip && !frontier.length ? out : null;
  }

  // seedWalkFromIndex primes a fresh ext walk from the index's eager set, bridging a live tip that has moved ahead of it.
  async function seedWalkFromIndex(ctx, ext, w) {
    const idx = await loadItemsIndex(ctx, ext);
    if (!idx || w.state.commits.length) return;
    const known = new Set(idx.residentShas);
    let gap = [];
    if (w.tip !== idx.tip) {
      gap = await bridgeToIndex(ctx, w.tip, idx, known);
      if (!gap) return;
    }
    const state = w.state;
    for (const c of gap) { state.visited.add(c.hash); state.commits.push(c); }
    for (const e of idx.items) {
      if (state.visited.has(e.sha)) continue;
      state.visited.add(e.sha);
      state.commits.push(metaCommit(e));
    }
    w.older = idx.olderShards.slice();
    w.ext = ext;
    // metaCommits carry no parents, so the frontier stays empty and stepExtWalk drains older shards before a loose walk.
    state.frontier = [];
    w.indexBacked = true;
  }

  // loadNextItemShard pulls the next-older pending shard onto an index-seeded walk, consumed newest to oldest.
  async function loadNextItemShard(ctx, w) {
    if (!w.older || !w.older.length) return false;
    const key = w.older.shift();
    const text = await fetchText(ctx.base, key);
    if (!text) return true;
    let doc;
    try { doc = JSON.parse(text); } catch { return true; }
    // Shard docs store their members oldest-first; push newest-first so state.commits stays uniformly newest-first.
    const entries = (doc && Array.isArray(doc.items)) ? doc.items.slice().reverse() : [];
    for (const e of entries) {
      if (w.state.visited.has(e.sha)) continue;
      w.state.visited.add(e.sha);
      w.state.commits.push(metaCommit(e));
    }
    return true;
  }

  // stepExtWalk advances an ext walk by one window, draining an older shard before it would touch the loose walk.
  async function stepExtWalk(ctx, w, cap) {
    if (w.older && w.older.length) { await loadNextItemShard(ctx, w); return; }
    if (w.state.frontier.length) await walkStep(ctx, w.state, cap);
  }

  // extWalkExhausted reports whether an ext walk has no pending shards and an empty frontier.
  function extWalkExhausted(w) {
    return !(w.older && w.older.length) && !w.state.frontier.length;
  }

  // extSetComplete reports whether an ext's loaded walk covers its whole history; it reads the cached walk and drives no fetches.
  async function extSetComplete(ctx, ext) {
    const w = await extWalkState(ctx, ext);
    if (!w) return true;
    return !!w.indexBacked || extWalkExhausted(w);
  }

  // extWalkState returns an extension branch's resumable walk, seeded from the items index once per tip; null when the branch is absent.
  async function extWalkState(ctx, ext) {
    const tip = await refTip(ctx, EXT_BRANCHES[ext]);
    if (!tip) return null;
    const w = walkStateFor(ctx, "ext:" + ext, tip);
    // Memoize the in-flight seed, so a second caller awaits it instead of proceeding over a half-seeded state.
    if (!w.seedPromise) w.seedPromise = seedWalkFromIndex(ctx, ext, w);
    await w.seedPromise;
    return w;
  }

  // withWalkLock serializes the async operations that mutate one shared walk state.
  function withWalkLock(w, fn) {
    const prev = w.lock || Promise.resolve();
    let release;
    w.lock = new Promise((r) => { release = r; });
    return prev.then(fn).finally(release);
  }

  // loadExtItemsWindow returns a bounded, body-hydrated render window of an ext's resolved items, growing a cursor by WALK_CAP per call.
  async function loadExtItemsWindow(ctx, ext, extend) {
    const w = await extWalkState(ctx, ext);
    if (!w) return { items: [], truncated: false };
    return withWalkLock(w, async () => {
      if (w.shown === undefined) w.shown = 0;
      if (extend) { w.shown += WALK_CAP; if (!extWalkExhausted(w)) await stepExtWalk(ctx, w, WALK_CAP); }
      else { if (w.state.commits.length === 0) await stepExtWalk(ctx, w, WALK_CAP); if (w.shown < WALK_CAP) w.shown = WALK_CAP; }
      while (!extWalkExhausted(w) && w.state.commits.length < w.shown) await stepExtWalk(ctx, w, WALK_CAP);
      const items = resolveItems(walkedCommits(w.state));
      const shown = items.slice(0, w.shown);
      await hydrateItems(ctx, shown);
      return { items: shown, truncated: !extWalkExhausted(w) || items.length > w.shown };
    });
  }

  // loadExtItems returns an extension branch's resolved items, at least one window; empty when the branch is absent.
  async function loadExtItems(ctx, ext) {
    return (await loadExtItemsWindow(ctx, ext, false)).items;
  }

  // loadExtItemsUpTo deepens an ext walk to budget visited commits, then returns the resolved items.
  async function loadExtItemsUpTo(ctx, ext, budget) {
    const w = await extWalkState(ctx, ext);
    if (!w) return [];
    return withWalkLock(w, async () => {
      if (w.state.commits.length === 0) await stepExtWalk(ctx, w, WALK_CAP);
      while (!extWalkExhausted(w) && w.state.visited.size < budget) await stepExtWalk(ctx, w, WALK_CAP);
      return resolveItems(walkedCommits(w.state));
    });
  }

  // findItemDeep resolves one item by hash, deepening the walk a window at a time up to DETAIL_WALK_CAP; onProgress fires before each extra window.
  async function findItemDeep(ctx, ext, hash, onProgress) {
    const w = await extWalkState(ctx, ext);
    if (!w) return { item: null, items: [] };
    return withWalkLock(w, async () => {
      const match = (items) => items.find((i) => i.commit.hash === hash || i.commit.short === hash || i.commit.hash.startsWith(hash)) || null;
      let direct = null;
      if (/^[0-9a-f]{40}$/.test(hash) && !w.state.visited.has(hash)) {
        const obj = await getObject(ctx, hash);
        if (obj && obj.type === "commit") direct = parseCommit(hash, obj.body);
      }
      const resolved = () => {
        const commits = walkedCommits(w.state);
        if (direct && !w.state.visited.has(direct.hash)) {
          commits.push(direct);
          commits.sort((a, b) => b.authorTime - a.authorTime);
        }
        return resolveItems(commits);
      };
      if (w.state.commits.length === 0) await stepExtWalk(ctx, w, WALK_CAP);
      let items = resolved();
      let found = match(items);
      while (!found && !extWalkExhausted(w) && w.state.visited.size < DETAIL_WALK_CAP) {
        if (onProgress) onProgress(w.state.visited.size);
        await stepExtWalk(ctx, w, WALK_CAP);
        items = resolved();
        found = match(items);
      }
      if (found) await hydrateItem(ctx, found);
      return { item: found, items };
    });
  }

  // loadBranchLogWindow pages a branch log; the default branch is served from the code items index when the bucket carries one.
  async function loadBranchLogWindow(ctx, name, extend) {
    const indexed = await loadBranchLogIndexed(ctx, name, extend);
    if (indexed) return indexed;
    const tip = await refTip(ctx, "refs/heads/" + name);
    if (!tip) return { tip: null, items: [], truncated: false };
    const w = walkStateFor(ctx, "branch:" + name, tip);
    if (extend || w.state.commits.length === 0) await walkStep(ctx, w.state, WALK_CAP);
    return { tip, items: walkedCommits(w.state), truncated: w.state.frontier.length > 0 };
  }

  // loadBranchLogIndexed serves the default branch's log from the code items index, or null so the caller falls back to the loose walk.
  async function loadBranchLogIndexed(ctx, name, extend) {
    const { defaultBranch } = await listBranches(ctx);
    if (!defaultBranch || name !== defaultBranch) return null;
    const w = await codeIndexWalkState(ctx);
    if (!w) return null;
    const tip = await refTip(ctx, "refs/heads/" + name);
    const key = "branchLog:" + name;
    const b = ctx.walks[key] || (ctx.walks[key] = { shown: 0 });
    return withWalkLock(w, async () => {
      b.shown = extend ? b.shown + WALK_CAP : Math.max(b.shown, WALK_CAP);
      const filtered = () => w.items.filter((c) => (c._branch || "") === defaultBranch);
      // Drain older shards until the slice fills the cursor or no pending shard remains, guarded against a shard that adds nothing.
      let guard = (w.older || []).length, stall = 0;
      while ((w.older && w.older.length) && filtered().length < b.shown) {
        await loadNextCodeShard(ctx, w);
        const n = (w.older || []).length;
        if (n === guard) { if (++stall >= 2) break; } else stall = 0;
        guard = n;
      }
      const items = filtered();
      const shown = items.slice(0, b.shown);
      const truncated = items.length > b.shown || (w.older && w.older.length > 0) || !w.complete;
      return { tip, items: shown, truncated };
    });
  }

  // startExcludingWalk seeds a walk at tip whose visited set holds every ancestor of baseSha, yielding a compare's head-side commits.
  async function startExcludingWalk(ctx, tip, baseSha, cap) {
    cap = cap || DETAIL_WALK_CAP;
    const visited = new Set();
    let frontier = [baseSha];
    let seen = 0;
    while (frontier.length && seen < cap) {
      const batch = [];
      while (frontier.length && batch.length < CONCURRENCY && seen < cap) {
        const h = frontier.shift();
        if (h && !visited.has(h)) { visited.add(h); seen++; batch.push(h); }
      }
      if (!batch.length) break;
      const objs = await Promise.all(batch.map((h) => getObject(ctx, h)));
      for (let i = 0; i < batch.length; i++) {
        const obj = objs[i];
        if (obj && obj.type === "commit") for (const p of parseCommit(batch[i], obj.body).parents) frontier.push(p);
      }
    }
    // The base commit itself is excluded; the walk starts at the head tip with that exclusion set.
    return { visited, frontier: [tip], commits: [] };
  }

  // loadCompareCommitsWindow pages a compare's head-side commits, from the ancestry index when it covers both endpoints; cached on ctx per pair.
  async function loadCompareCommitsWindow(ctx, baseSha, headSha, extend) {
    const key = "compare:" + baseSha + ".." + headSha;
    let entry = ctx.walks[key];
    if (!entry || entry.tip !== headSha) {
      const all = await indexCompareCommits(ctx, baseSha, headSha);
      if (all) entry = ctx.walks[key] = { tip: headSha, all, shown: 0 };
      else entry = ctx.walks[key] = { tip: headSha, state: await startExcludingWalk(ctx, headSha, baseSha, DETAIL_WALK_CAP) };
    }
    if (entry.all) {
      entry.shown = extend ? entry.shown + WALK_CAP : Math.max(entry.shown, WALK_CAP);
      return { items: entry.all.slice(0, entry.shown), truncated: entry.all.length > entry.shown };
    }
    if (extend || entry.state.commits.length === 0) await walkStep(ctx, entry.state, WALK_CAP);
    return { items: walkedCommits(entry.state), truncated: entry.state.frontier.length > 0 };
  }

  // GRAPH_WINDOW is the number of commits the repository graph loads per window.
  const GRAPH_WINDOW = 150;

  // loadGraphDecorations gathers the graph's ref decorations once per context: branch tips, tags and merged-PR labels, from data the route already holds.
  async function loadGraphDecorations(ctx) {
    const key = "graphDecor";
    if (ctx.walks[key]) return ctx.walks[key];
    const load = (async () => {
      const { branches, defaultBranch } = await listBranches(ctx);
      // Branch tips, the tag list and the review index are independent reads, so they go out in one batch.
      const code = branches.filter((b) => !b.name.startsWith("gitmsg/"));
      const [tipShas, tagList, idx] = await Promise.all([
        Promise.all(code.map((b) => refTip(ctx, b.ref))),
        listTags(ctx),
        loadItemsIndex(ctx, "review").catch((e) => { if (e && e.forbidden) throw e; return null; }),
      ]);
      const tips = {};
      for (let i = 0; i < code.length; i++) {
        const sha = tipShas[i];
        if (sha) (tips[sha] = tips[sha] || []).push(code[i].name);
      }
      const tags = {};
      for (const t of tagList) (tags[t.sha] = tags[t.sha] || []).push(t.name);
      const merged = [];
      const seen = new Set();
      for (const e of (idx ? idx.items : [])) {
        const h = parseGitmsg(String(e.header || ""));
        if (!h || h.ext !== "review" || h.type !== "pull-request" || h.state !== "merged") continue;
        const name = parseBranchField(h.head).name;
        if (!name || name.startsWith("gitmsg/")) continue;
        // A merged state rides on an edit, so the canonical PR sha comes from that edit's edits field.
        const canonShort = refHash(h.edits || "");
        const canon = canonShort ? idx.items.find((x) => String(x.sha || "").startsWith(canonShort)) : e;
        for (const short of [h["merge-head"], h["head-tip"]]) {
          if (!short || !/^[0-9a-f]{6,40}$/.test(short) || seen.has(short + " " + name)) continue;
          seen.add(short + " " + name);
          merged.push({ short, name, prSha: canon ? canon.sha : "" });
        }
      }
      return { tips, tags, defaultBranch, merged };
    })();
    ctx.walks[key] = load;
    return load;
  }

  // orderGraphWindow emits resident code-index commits in the loose walk's order: topological, yet interleaved by time, which is what assignGraphLanes needs.
  function orderGraphWindow(items, cap) {
    const byHash = new Map();
    for (const c of items) if (!byHash.has(c.hash)) byHash.set(c.hash, c);
    const isParent = new Set();
    for (const c of byHash.values()) for (const p of c.parents || []) isParent.add(p);
    const frontier = [];
    for (const c of byHash.values()) if (!isParent.has(c.hash)) frontier.push(c.hash);
    const visited = new Set();
    const commits = [];
    while (frontier.length && commits.length < cap) {
      let best = 0, bestT = -Infinity;
      for (let i = 0; i < frontier.length; i++) {
        const c = byHash.get(frontier[i]);
        const t = (c && c.authorTime) || 0;
        if (t > bestT) { bestT = t; best = i; }
      }
      const h = frontier.splice(best, 1)[0];
      if (visited.has(h)) continue;
      visited.add(h);
      const c = byHash.get(h);
      if (!c) continue;
      commits.push(c);
      for (const p of c.parents || []) if (byHash.has(p) && !visited.has(p)) frontier.push(p);
    }
    return { commits, more: frontier.some((h) => !visited.has(h)) };
  }

  // loadGraphWindowIndexed serves the graph from a v5 code items index, or null so loadGraphWindow falls back to the loose walk.
  async function loadGraphWindowIndexed(ctx, extend) {
    const w = await codeIndexWalkState(ctx);
    if (!w || !w.hasParents) return null;
    const key = "graphIndexed";
    const g = ctx.walks[key] || (ctx.walks[key] = { shown: 0 });
    const decor = await loadGraphDecorations(ctx);
    return withWalkLock(w, async () => {
      g.shown = extend ? g.shown + GRAPH_WINDOW : Math.max(g.shown, GRAPH_WINDOW);
      // Drain older shards until the window fills or no pending shard remains, guarded like loadBranchLogIndexed.
      let guard = (w.older || []).length, stall = 0;
      while ((w.older && w.older.length) && w.items.length < g.shown) {
        await loadNextCodeShard(ctx, w);
        const n = (w.older || []).length;
        if (n === guard) { if (++stall >= 2) break; } else stall = 0;
        guard = n;
      }
      const ordered = orderGraphWindow(w.items, g.shown);
      const truncated = ordered.more || (w.older && w.older.length > 0) || !w.complete;
      return { commits: ordered.commits, truncated, decor };
    });
  }

  // loadGraphWindow walks the commit DAG across all branch heads, newest-first, GRAPH_WINDOW per window; returns { commits, truncated, decor }.
  async function loadGraphWindow(ctx, extend) {
    const indexed = await loadGraphWindowIndexed(ctx, extend);
    if (indexed) return indexed;
    const key = "graph";
    let entry = ctx.walks[key];
    if (!entry) {
      const { branches } = await listBranches(ctx);
      const seeds = [];
      for (const b of branches) {
        const sha = await refTip(ctx, b.ref);
        if (sha) seeds.push(sha);
      }
      entry = ctx.walks[key] = { state: { visited: new Set(), frontier: seeds.slice(), commits: [] } };
    }
    const decor = await loadGraphDecorations(ctx);
    if (extend || entry.state.commits.length === 0) await graphWalkStep(ctx, entry.state, GRAPH_WINDOW);
    return { commits: entry.state.commits.slice(), truncated: entry.state.frontier.length > 0, decor };
  }

  // graphWalkStep advances a graph walk, always expanding the newest frontier commit, so the sequence interleaves branches in time order.
  async function graphWalkStep(ctx, state, windowCap) {
    const start = state.commits.length;
    // Author times for the frontier are cached on the walk, so a re-scan across steps costs no fetch.
    state.times = state.times || new Map();
    const timeOf = async (h) => {
      if (state.times.has(h)) return state.times.get(h);
      const obj = await getObject(ctx, h);
      const t = obj && obj.type === "commit" ? parseCommit(h, obj.body).authorTime : 0;
      state.times.set(h, t);
      return t;
    };
    while (state.frontier.length && state.commits.length - start < windowCap) {
      // Drop already-visited frontier entries.
      state.frontier = state.frontier.filter((h) => h && !state.visited.has(h));
      if (!state.frontier.length) break;
      // Pick the newest frontier commit by author time.
      let best = 0, bestT = -1;
      for (let i = 0; i < state.frontier.length; i++) {
        const t = await timeOf(state.frontier[i]);
        if (t > bestT) { bestT = t; best = i; }
      }
      const h = state.frontier.splice(best, 1)[0];
      if (state.visited.has(h)) continue;
      state.visited.add(h);
      const obj = await getObject(ctx, h);
      if (!obj || obj.type !== "commit") continue;
      const c = parseCommit(h, obj.body);
      state.commits.push(c);
      // Branch attribution for the code timeline: a parent inherits the branch it was first reached from, and the default branch wins.
      if (state.reachedVia) {
        const tb = state.tipBranch && state.tipBranch[h];
        // A branch tip is attributed to that branch; the default branch wins over a feature attribution.
        const via = (tb === state.defaultBranch ? tb : (state.reachedVia[h] || tb)) || "";
        if (via) state.reachedVia[h] = via;
        const isDefault = via && via === state.defaultBranch;
        for (const p of c.parents) if (!(p in state.reachedVia) || isDefault) state.reachedVia[p] = via;
      }
      for (const p of c.parents) if (!state.visited.has(p)) state.frontier.push(p);
    }
    return state;
  }

  // assignGraphLanes assigns each commit a lane and its parent edges for the SVG DAG; a parent outside the window claims no lane.
  function assignGraphLanes(commits) {
    const index = new Map();
    commits.forEach((c, i) => index.set(c.hash, i));
    // lanes[i] = sha this lane is currently waiting to place, or null (free).
    const lanes = [];
    let laneCount = 0;
    const claim = (sha) => {
      for (let i = 0; i < lanes.length; i++) if (lanes[i] === sha) return i;
      for (let i = 0; i < lanes.length; i++) if (lanes[i] === null) { lanes[i] = sha; return i; }
      lanes.push(sha); return lanes.length - 1;
    };
    const rows = [];
    for (const c of commits) {
      // The commit's lane: a lane already waiting for it, else a fresh lane.
      let lane = -1;
      for (let i = 0; i < lanes.length; i++) if (lanes[i] === c.hash) { lane = i; break; }
      if (lane < 0) lane = claim(c.hash);
      // Free the commit's lane first, so the first parent can inherit it.
      lanes[lane] = null;
      const parents = [];
      c.parents.forEach((p, pi) => {
        // A parent is drawable only when it is emitted below this row; the rest record present=false and hold no lane.
        const present = index.has(p) && index.get(p) > rows.length;
        let pl;
        if (!present) pl = lane;
        else if (pi === 0) {
          // Merge into a lane already waiting for this parent rather than double-booking a second one.
          let waiting = -1;
          for (let i = 0; i < lanes.length; i++) if (lanes[i] === p) { waiting = i; break; }
          if (waiting >= 0) pl = waiting;
          else { lanes[lane] = p; pl = lane; }
        } else pl = claim(p);
        parents.push({ sha: p, lane: pl, present });
      });
      rows.push({ commit: c, lane, parents });
      for (let i = lanes.length; i > 0; i--) if (lanes[i - 1] !== null) { laneCount = Math.max(laneCount, i); break; }
      laneCount = Math.max(laneCount, lane + 1);
      for (const pr of parents) laneCount = Math.max(laneCount, pr.lane + 1);
    }
    return { rows, laneCount };
  }

  function newContext(base) {
    // newContext builds the per-session read context: the object cache, ref misses, tree expansion, walk states and the packfile reader state.
    return { base, objects: new Map(), refMisses: new Set(), treeExpanded: new Set(), walks: {}, packs: { names: null, packed: null, maps: new Map(), idx: new Map(), size: new Map(), windows: new Map(), lastHit: null } };
  }

  // ---- Trees, paths, branches (DOM-free, testable) ----

  // parseTree parses a git tree object body; the entry type comes from its mode.
  function parseTree(body) {
    const entries = [];
    const dec = new TextDecoder();
    let i = 0;
    while (i < body.length) {
      let sp = i;
      while (sp < body.length && body[sp] !== 0x20) sp++;
      if (sp >= body.length) break;
      const mode = dec.decode(body.subarray(i, sp));
      let nul = sp + 1;
      while (nul < body.length && body[nul] !== 0) nul++;
      const name = dec.decode(body.subarray(sp + 1, nul));
      const shaBytes = body.subarray(nul + 1, nul + 21);
      if (shaBytes.length < 20) break;
      let sha = "";
      for (let b = 0; b < 20; b++) sha += shaBytes[b].toString(16).padStart(2, "0");
      const type = mode === "40000" ? "tree" : (mode === "160000" ? "commit" : "blob");
      entries.push({ mode, name, sha, type });
      i = nul + 21;
    }
    return entries;
  }

  // getTree fetches a tree object and returns its parsed entries, or null.
  async function getTree(ctx, sha) {
    const obj = await getContentObject(ctx, sha);
    if (!obj || obj.type !== "tree") return null;
    return parseTree(obj.body);
  }

  // resolvePath walks tree entries down a "/"-separated path from a commit's root tree; null when a segment is missing.
  async function resolvePath(ctx, commitSha, path) {
    const obj = await getObject(ctx, commitSha);
    if (!obj || obj.type !== "commit") return null;
    const commit = parseCommit(commitSha, obj.body);
    let cur = { type: "tree", sha: commit.tree, mode: "40000" };
    const parts = (path || "").split("/").filter(Boolean);
    for (const part of parts) {
      if (cur.type !== "tree") return null;
      const entries = await getTree(ctx, cur.sha);
      if (!entries) return null;
      const match = entries.find((e) => e.name === part);
      if (!match) return null;
      cur = { type: match.type, sha: match.sha, mode: match.mode };
    }
    return cur;
  }

  // headBranchName strips refs/heads/ from the HEAD symref target.
  function headBranchName(head) {
    return head && head.branch ? head.branch.replace(/^refs\/heads\//, "") : null;
  }

  // listBranches enumerates branches from the manifest, else the well-known extension branches plus HEAD's branch.
  async function listBranches(ctx) {
    const head = await headFor(ctx);
    const defaultBranch = headBranchName(head);
    const manifest = await manifestFor(ctx);
    const names = new Set();
    if (manifest) {
      for (const ref of Object.keys(manifest)) {
        if (ref.startsWith("refs/heads/")) names.add(ref.slice(11));
      }
    } else {
      for (const ref of Object.values(EXT_BRANCHES)) names.add(ref.slice(11));
    }
    if (defaultBranch) names.add(defaultBranch);
    const branches = Array.from(names).sort().map((n) => ({
      name: n, ref: "refs/heads/" + n, isDefault: n === defaultBranch,
    }));
    return { defaultBranch, branches };
  }

  // peelTag resolves a ref sha to its commit, following annotated tag objects; tagger and message come from the tag object.
  async function peelTag(ctx, sha) {
    let cur = sha, tagger = "", message = "", signed = false, guard = 0;
    while (cur && guard++ < 8) {
      const obj = await getObject(ctx, cur);
      if (!obj) return { sha, commit: null, tagger, message, signed };
      if (obj.type === "commit") return { sha, commit: cur, tagger, message, signed };
      if (obj.type !== "tag") return { sha, commit: null, tagger, message, signed };
      const text = new TextDecoder().decode(obj.body);
      const split = text.indexOf("\n\n");
      const header = split < 0 ? text : text.slice(0, split);
      if (split >= 0 && !message) {
        const raw = text.slice(split + 2).replace(/\r/g, "");
        const stripped = stripSignatureBlock(raw);
        signed = stripped.signed;
        message = stripped.text.trim();
      }
      let next = "";
      for (const line of header.split("\n")) {
        if (line.startsWith("object ")) next = line.slice(7).trim();
        else if (line.startsWith("tagger ") && !tagger) tagger = line.slice(7).trim();
      }
      cur = next;
    }
    return { sha, commit: null, tagger, message, signed };
  }

  // stripSignatureBlock removes a trailing PGP or SSH signature block and reports whether one was there.
  function stripSignatureBlock(body) {
    const re = /-----BEGIN (?:PGP|SSH) SIGNATURE-----[\s\S]*?-----END (?:PGP|SSH) SIGNATURE-----\s*/g;
    const signed = re.test(body);
    return { text: signed ? body.replace(re, "").replace(/\s+$/, "") : body, signed };
  }

  // listTags enumerates the bucket's tags from the refs manifest, each with its raw ref sha, unpeeled.
  async function listTags(ctx) {
    const manifest = await manifestFor(ctx);
    const tags = [];
    if (manifest) {
      for (const ref of Object.keys(manifest)) {
        if (!ref.startsWith("refs/tags/")) continue;
        const sha = manifest[ref];
        if (!/^[0-9a-f]{40}$/.test(sha || "")) continue;
        tags.push({ name: ref.slice(10), ref, sha });
      }
    }
    tags.sort(compareTagsDesc);
    return tags;
  }

  // tagVersionKey extracts a tag name's dotted version as a number array; null when the name carries none.
  function tagVersionKey(name) {
    const m = /^v?(\d+(?:\.\d+)*)/.exec(String(name || ""));
    return m ? m[1].split(".").map(Number) : null;
  }

  // compareTagsDesc orders tags highest version first, non-version tags after them by name descending.
  function compareTagsDesc(a, b) {
    const va = tagVersionKey(a.name), vb = tagVersionKey(b.name);
    if (va && vb) {
      for (let i = 0; i < Math.max(va.length, vb.length); i++) {
        const d = (vb[i] || 0) - (va[i] || 0);
        if (d) return d;
      }
      const sa = tagVersionSuffix(a.name), sb = tagVersionSuffix(b.name);
      if (!sa && sb) return -1;
      if (sa && !sb) return 1;
      return a.name.localeCompare(b.name);
    }
    if (va) return -1;
    if (vb) return 1;
    return b.name.localeCompare(a.name);
  }

  // tagVersionSuffix returns the text after a tag name's leading numeric version.
  function tagVersionSuffix(name) {
    return String(name || "").replace(/^v?\d+(?:\.\d+)*/, "");
  }

  // resolveCompareRef resolves a compare side to a commit sha, branch first unless the manifest lists only the tag.
  async function resolveCompareRef(ctx, name) {
    if (!name) return null;
    const manifest = await manifestFor(ctx);
    const tagFirst = !!(manifest && !manifest["refs/heads/" + name] && manifest["refs/tags/" + name]);
    const tryBranch = async () => {
      const tip = await refTip(ctx, "refs/heads/" + name);
      return tip ? { sha: tip, kind: "branch", name } : null;
    };
    if (!tagFirst) {
      const branch = await tryBranch();
      if (branch) return branch;
    }
    const tagSha = await refTip(ctx, "refs/tags/" + name);
    if (tagSha) {
      const peeled = await peelTag(ctx, tagSha);
      if (peeled.commit) return { sha: peeled.commit, kind: "tag", name };
    }
    return tagFirst ? tryBranch() : null;
  }

  // ---- Markdown (GFM subset; parse step is DOM-free and testable) ----

  // VOID_HTML are the self-closing tags, treated as standalone elements rather than wrappers.
  const VOID_HTML = new Set(["br", "hr", "img", "source", "col", "input", "wbr", "area"]);

  // INLINE_HTML are the tags a line may lead with and still continue the current paragraph.
  const INLINE_HTML = new Set(["a", "b", "i", "em", "strong", "code", "kbd", "sup", "sub", "span", "img", "br", "del", "s", "strike", "mark", "small", "picture", "input"]);

  // matchDelim returns the index of the delimiter closing the one at start, counting nesting, or -1.
  function matchDelim(text, start, open, close) {
    let depth = 0;
    for (let i = start; i < text.length; i++) {
      if (text[i] === open) depth++;
      else if (text[i] === close) { depth--; if (depth === 0) return i; }
    }
    return -1;
  }

  // LINK_REF_DEF_RE matches a CommonMark link reference definition line, which renders as nothing when its label is unreferenced.
  const LINK_REF_DEF_RE = /^ {0,3}\[([^\]]{1,128})\]:\s*(\S+)(?:\s+(?:"[^"]*"|'[^']*'|\([^)]*\)))?\s*$/;

  // collectLinkRefDefs returns the label to destination map and the line indices the block parser must skip.
  function collectLinkRefDefs(lines) {
    const defs = {}, skip = new Set();
    let fenced = false, atBlockStart = true;
    for (let i = 0; i < lines.length; i++) {
      const line = lines[i];
      if (/^\s*```/.test(line)) { fenced = !fenced; atBlockStart = false; continue; }
      if (fenced) continue;
      if (line.trim() === "") { atBlockStart = true; continue; }
      const m = atBlockStart ? LINK_REF_DEF_RE.exec(line) : null;
      if (!m) { atBlockStart = false; continue; }
      const label = m[1].trim().toLowerCase();
      if (label && !(label in defs)) defs[label] = m[2].replace(/^<|>$/g, "");
      skip.add(i);
    }
    return { defs, skip };
  }

  // stripLinkRefDefs removes link reference definitions and HTML comments from raw content, mirroring site_items.go siteStripLinkRefDefs.
  function stripLinkRefDefs(content) {
    const lines = (content || "").replace(/\r/g, "").replace(HTML_COMMENT_RE, "").split("\n");
    const { skip } = collectLinkRefDefs(lines);
    const kept = skip.size ? lines.filter((_, i) => !skip.has(i)) : lines;
    return kept.join("\n").replace(/^\n+/, "");
  }

  // HTML_COMMENT_RE matches an HTML comment, the other thing that renders as nothing upstream.
  const HTML_COMMENT_RE = /<!--[\s\S]*?-->/g;

  // SUBJECT_UNWRAP reduces a subject to its text; every rule stays RE2-compatible, so site_items.go siteSubjectText mirrors it.
  const SUBJECT_UNWRAP = [
    [/^\s{0,3}(?:#{1,6}\s+|>\s?)/, ""],   // a leading heading or blockquote marker
    [/\*\*([^*]+)\*\*/g, "$1"],           // bold
    [/\*([^\s*][^*]*)\*/g, "$1"],          // italic
    // Underscore emphasis is word-bounded per CommonMark; the boundary is captured and re-emitted, which RE2 allows.
    [/(^|[\s(])__([^\s_][^_]*)__($|[\s).,;:!?])/g, "$1$2$3"],
    [/(^|[\s(])_([^\s_][^_]*)_($|[\s).,;:!?])/g, "$1$2$3"],
    [/`([^`]+)`/g, "$1"],                  // code span
    // The inline HTML a comment body may carry: the renderer shows these, a title cannot.
    [/<\/?(?:br|hr|p|div|span|b|i|em|strong|code|kbd|sup|sub|img|a|details|summary)(?:\s[^<>]*)?\/?>/gi, " "],
  ];

  // SUBJECT_LINK unwraps a link or image to its words; run repeatedly, so a nested badge reduces from the inside out.
  const SUBJECT_LINK = /!?\[([^\]]*)\]\([^)]*\)/g;

  // subjectText projects a raw first line to the text a title shows, mirroring site_items.go siteSubjectText.
  function subjectText(line) {
    let out = line || "";
    for (let pass = 0; pass < 3 && SUBJECT_LINK.test(out); pass++) { SUBJECT_LINK.lastIndex = 0; out = out.replace(SUBJECT_LINK, "$1"); }
    SUBJECT_LINK.lastIndex = 0;
    // Twice: a boundary character consumed by one match is the one the next match needs.
    for (let pass = 0; pass < 2; pass++) for (const [re, to] of SUBJECT_UNWRAP) out = out.replace(re, to);
    return out.replace(/\s+/g, " ").trim();
  }

  // linkRefTarget resolves a reference link or image against the collected definitions; null leaves the brackets as literal text.
  function linkRefTarget(text, close, label, defs) {
    if (!defs) return null;
    let end = close + 1, key = label;
    if (text[end] === "[") {
      const lclose = matchDelim(text, end, "[", "]");
      if (lclose < 0) return null;
      const explicit = text.slice(end + 1, lclose).trim();
      if (explicit) key = explicit;
      end = lclose + 1;
    }
    const href = defs[key.trim().toLowerCase()];
    return href ? { href, next: end } : null;
  }

  // parseInline tokenizes a line into inline spans; markdown-native spans render through safe DOM builders, and only rawhtml spans reach the sanitizer.
  function parseInline(text, defs) {
    const spans = [];
    let buf = "";
    const flush = () => { if (buf) { spans.push({ type: "text", value: buf }); buf = ""; } };
    for (let i = 0; i < text.length;) {
      const ch = text[i];
      if (ch === "\\" && /[!-/:-@[-`{-~]/.test(text[i + 1] || "")) { buf += text[i + 1]; i += 2; continue; }
      if (ch === "`") {
        const end = text.indexOf("`", i + 1);
        if (end > i) { flush(); spans.push({ type: "code", value: text.slice(i + 1, end) }); i = end + 1; continue; }
      }
      if (ch === "!" && text[i + 1] === "[") {
        const close = matchDelim(text, i + 1, "[", "]");
        if (close > i && text[close + 1] === "(") {
          const paren = matchDelim(text, close + 1, "(", ")");
          if (paren > close) { flush(); spans.push({ type: "image", alt: text.slice(i + 2, close), src: text.slice(close + 2, paren).trim() }); i = paren + 1; continue; }
        }
        const ref = close > i ? linkRefTarget(text, close, text.slice(i + 2, close), defs) : null;
        if (ref) { flush(); spans.push({ type: "image", alt: text.slice(i + 2, close), src: ref.href }); i = ref.next; continue; }
      }
      if (ch === "[") {
        const close = matchDelim(text, i, "[", "]");
        if (close > i && text[close + 1] === "(") {
          const paren = matchDelim(text, close + 1, "(", ")");
          if (paren > close) {
            flush();
            spans.push({ type: "link", spans: parseInline(text.slice(i + 1, close), defs), href: text.slice(close + 2, paren).trim() });
            i = paren + 1; continue;
          }
        }
        const ref = close > i ? linkRefTarget(text, close, text.slice(i + 1, close), defs) : null;
        if (ref) {
          flush();
          spans.push({ type: "link", spans: parseInline(text.slice(i + 1, close), defs), href: ref.href });
          i = ref.next; continue;
        }
      }
      if (ch === "<") {
        if (text.startsWith("<!--", i)) {
          flush();
          const end = text.indexOf("-->", i + 4);
          i = end >= 0 ? end + 3 : text.length;
          continue;
        }
        const auto = /^<((?:https?:\/\/|mailto:)[^>\s]+)>/.exec(text.slice(i));
        if (auto) { flush(); const u = auto[1]; spans.push({ type: "link", spans: [{ type: "text", value: u }], href: u }); i += auto[0].length; continue; }
        const tagM = /^<(\/?)([a-zA-Z][a-zA-Z0-9]*)((?:\s[^<>]*)?)(\/?)>/.exec(text.slice(i));
        if (tagM) {
          const tag = tagM[2].toLowerCase(), whole = tagM[0];
          if (tagM[1] === "/" || tagM[4] === "/" || VOID_HTML.has(tag)) { flush(); spans.push({ type: "rawhtml", value: whole }); i += whole.length; continue; }
          const close = "</" + tag + ">";
          const end = text.indexOf(close, i + whole.length);
          if (end >= 0) { flush(); spans.push({ type: "rawhtml", value: text.slice(i, end + close.length) }); i = end + close.length; continue; }
          flush(); spans.push({ type: "rawhtml", value: whole }); i += whole.length; continue;
        }
      }
      if (ch === "*" && text[i + 1] === "*") {
        const end = text.indexOf("**", i + 2);
        if (end > i) { flush(); spans.push({ type: "strong", spans: parseInline(text.slice(i + 2, end), defs) }); i = end + 2; continue; }
      }
      if (ch === "~" && text[i + 1] === "~") {
        const end = text.indexOf("~~", i + 2);
        if (end > i) { flush(); spans.push({ type: "strike", spans: parseInline(text.slice(i + 2, end), defs) }); i = end + 2; continue; }
      }
      if ((ch === "*" || ch === "_") && text[i + 1] !== ch && text[i + 1] !== undefined && text[i + 1] !== " ") {
        const end = text.indexOf(ch, i + 1);
        if (end > i) { flush(); spans.push({ type: "em", spans: parseInline(text.slice(i + 1, end), defs) }); i = end + 1; continue; }
      }
      if (ch === "h" && /^https?:\/\//.test(text.slice(i))) {
        const m = /^https?:\/\/[^\s<>)]+/.exec(text.slice(i));
        if (m) { const url = m[0].replace(/[.,;:!?]+$/, ""); flush(); spans.push({ type: "link", spans: [{ type: "text", value: url }], href: url }); i += url.length; continue; }
      }
      buf += ch; i++;
    }
    flush();
    return spans;
  }

  // spanHasText reports whether an inline span carries anything a reader can see.
  function spanHasText(span) {
    if (!span) return false;
    if (span.type === "text") return /\S/.test(span.value || "");
    if (span.spans) return span.spans.some(spanHasText);
    return true;
  }

  // indentWidth counts a line's leading whitespace, a tab as two, to decide list nesting.
  function indentWidth(line) {
    let n = 0;
    for (const c of line) { if (c === " ") n++; else if (c === "\t") n += 2; else break; }
    return n;
  }

  // splitTableRow splits a Markdown table row into trimmed cells, dropping the optional outer pipes.
  function splitTableRow(line) {
    let s = line.trim();
    if (s.startsWith("|")) s = s.slice(1);
    if (s.endsWith("|")) s = s.slice(0, -1);
    return s.split("|").map((c) => c.trim());
  }

  // isTableSeparator recognizes a GFM table delimiter row (---, :--, :-:, --:).
  function isTableSeparator(line) {
    if (!/\|/.test(line) && !/-/.test(line)) return false;
    const cells = splitTableRow(line);
    return cells.length > 0 && cells.every((c) => /^:?-+:?$/.test(c));
  }

  // cellAlign maps a delimiter cell to its column alignment.
  function cellAlign(cell) {
    const l = cell.startsWith(":"), r = cell.endsWith(":");
    return l && r ? "center" : r ? "right" : l ? "left" : "";
  }

  // parseList consumes an indentation-delimited list and returns { block, next }; a blank line ends it.
  function parseList(lines, start, defs) {
    const base = indentWidth(lines[start]);
    const ordered = /^\s*\d+[.)]\s+/.test(lines[start]);
    const items = [];
    let i = start;
    while (i < lines.length) {
      const line = lines[i];
      if (line.trim() === "") break;
      const m = /^(\s*)(?:[-*+]|\d+[.)])\s+(.*)$/.exec(line);
      if (!m) break;
      const ind = indentWidth(line);
      if (ind < base) break;
      if (ind > base) {
        const sub = parseList(lines, i, defs);
        if (items.length) items[items.length - 1].children.push(sub.block);
        i = sub.next;
        continue;
      }
      let content = m[2], task = null;
      const tm = /^\[([ xX])\]\s+(.*)$/.exec(content);
      if (tm) { task = tm[1].toLowerCase() === "x"; content = tm[2]; }
      items.push({ spans: parseInline(content, defs), task, children: [] });
      i++;
    }
    return { block: { type: "list", ordered, items }, next: i };
  }

  // isThematicBreak recognizes a rule line; parseMarkdown checks for a setext h2 first.
  function isThematicBreak(line) {
    return /^ {0,3}([-*_])( *\1){2,} *$/.test(line);
  }

  // breaksParagraph reports whether a trimmed <-leading line ends the current paragraph.
  function breaksParagraph(t) {
    const m = /^<(\/?)([a-zA-Z][\w-]*)/.exec(t);
    if (!m) return false;
    return m[1] === "/" || !INLINE_HTML.has(m[2].toLowerCase());
  }

  // MARKDOWN_PATH_RE matches the extensions both renderers render as prose. Mirrors siteFileDocExts in site_pages_files.go.
  const MARKDOWN_PATH_RE = /\.(md|markdown|mdown|mdx)$/i;

  // isMarkdownPath reports whether a path renders as prose rather than as source.
  function isMarkdownPath(path) { return MARKDOWN_PATH_RE.test(path || ""); }

  // isMDXPath reports whether a path carries MDX, whose imports and standalone JSX are stripped before parsing.
  function isMDXPath(path) { return /\.mdx$/i.test(path || ""); }

  // stripMDX drops the import, export and standalone JSX lines the markdown grammar has no rule for. Mirrors siteFileStripMDX in site_pages_files.go.
  function stripMDX(source) {
    const kept = [];
    for (const line of (source || "").split("\n")) {
      const t = line.trim();
      if (t.startsWith("import ") || t.startsWith("export ")) continue;
      if (t.length > 1 && t[0] === "<" && t[t.length - 1] === ">") {
        const r = t[1];
        if (r === "/" || (r >= "A" && r <= "Z")) continue;
      }
      kept.push(line);
    }
    return kept.join("\n");
  }

  // parseMarkdown parses text into a block list; markdown-native blocks are plain data, and html blocks carry verbatim source for the sanitizer.
  function parseMarkdown(text) {
    const lines = (text || "").replace(/\r/g, "").split("\n");
    // Definitions are collected in a pass of their own: a document may reference a label before defining it.
    const { defs, skip } = collectLinkRefDefs(lines);
    const blocks = [];
    let i = 0;
    while (i < lines.length) {
      const line = lines[i];
      if (/^\s*```/.test(line)) {
        const lang = line.trim().slice(3).trim();
        const body = [];
        i++;
        while (i < lines.length && !/^\s*```/.test(lines[i])) { body.push(lines[i]); i++; }
        i++;
        blocks.push({ type: "code", lang, text: body.join("\n") });
        continue;
      }
      // A collected definition renders nothing; only block-start lines are in skip, so a definition-shaped line inside a paragraph stays text.
      if (skip.has(i)) { i++; continue; }
      const t = line.trim();
      const closeM = /^<\/([a-zA-Z][\w-]*)\s*>$/.exec(t);
      if (closeM) { blocks.push({ type: "htmlclose", tag: closeM[1].toLowerCase() }); i++; continue; }
      const openM = /^<([a-zA-Z][\w-]*)((?:\s[^<>]*)?)>$/.exec(t);
      if (openM && !/\/$/.test(openM[2]) && !VOID_HTML.has(openM[1].toLowerCase())) {
        blocks.push({ type: "htmlopen", tag: openM[1].toLowerCase(), open: t }); i++; continue;
      }
      if (/^<[a-zA-Z][\w-]*(\s[^<>]*)?\/?>/.test(t)) { blocks.push({ type: "html", raw: t }); i++; continue; }
      const h = /^(#{1,6})\s+(.*)$/.exec(line);
      if (h) { blocks.push({ type: "heading", level: h[1].length, spans: parseInline(h[2].replace(/\s+#+\s*$/, "").trim(), defs) }); i++; continue; }
      if (/^\s*>/.test(line)) {
        const buf = [];
        while (i < lines.length && /^\s*>/.test(lines[i])) { buf.push(lines[i].replace(/^\s*>\s?/, "")); i++; }
        blocks.push({ type: "blockquote", blocks: parseMarkdown(buf.join("\n")) });
        continue;
      }
      if (line.includes("|") && i + 1 < lines.length && isTableSeparator(lines[i + 1])) {
        const headers = splitTableRow(line).map((c) => parseInline(c, defs));
        const aligns = splitTableRow(lines[i + 1]).map(cellAlign);
        i += 2;
        const rows = [];
        while (i < lines.length && lines[i].trim() !== "" && lines[i].includes("|")) { rows.push(splitTableRow(lines[i]).map((c) => parseInline(c, defs))); i++; }
        blocks.push({ type: "table", headers, aligns, rows });
        continue;
      }
      if (isThematicBreak(line)) { blocks.push({ type: "thematic" }); i++; continue; }
      if (/^\s*(?:[-*+]|\d+[.)])\s+/.test(line)) {
        const lst = parseList(lines, i, defs);
        blocks.push(lst.block); i = lst.next;
        continue;
      }
      if (line.trim() === "") { i++; continue; }
      const para = [];
      let setext = 0;
      while (i < lines.length && lines[i].trim() !== "" && !/^\s*```/.test(lines[i]) &&
             !/^#{1,6}\s+/.test(lines[i]) && !/^\s*(?:[-*+]|\d+[.)])\s+/.test(lines[i]) &&
             !/^\s*>/.test(lines[i]) && !breaksParagraph(lines[i].trim())) {
        const su = para.length ? /^ {0,3}(=+|-+) *$/.exec(lines[i]) : null;
        if (su) { setext = su[1][0] === "=" ? 1 : 2; i++; break; }
        if (isThematicBreak(lines[i])) break;
        para.push(lines[i]); i++;
      }
      const spans = parseInline(para.join("\n"), defs);
      if (setext) blocks.push({ type: "heading", level: setext, spans });
      // A paragraph whose spans carry no visible text is what an HTML comment leaves behind, so it is dropped.
      else if (spans.some(spanHasText)) blocks.push({ type: "paragraph", spans });
    }
    return blocks;
  }

  // ---- Diff engine (DOM-free, testable) ----

  const MAX_DIFF_LINES = 5000;
  const DIFF_BLOB_CAP = 1048576;
  // DIFF_TREE_SCAN_CAP bounds diffTrees' recursion, with enough margin over the display cap to report that more files changed.
  const DIFF_TREE_SCAN_CAP = 120;

  // splitLines splits text into lines, dropping the empty element a final newline produces.
  function splitLines(text) {
    if (text === "") return [];
    const lines = text.split("\n");
    if (lines.length && lines[lines.length - 1] === "") lines.pop();
    return lines;
  }

  // diffLines runs a Myers O(ND) diff and returns an edit script; null past MAX_DIFF_LINES unless force is set.
  function diffLines(aText, bText, force) {
    const a = splitLines(aText);
    const b = splitLines(bText);
    if (!force && a.length + b.length > MAX_DIFF_LINES) return null;
    const N = a.length, M = b.length;
    const max = N + M;
    const offset = max;
    const v = new Array(2 * max + 1).fill(0);
    const trace = [];
    let done = false;
    for (let d = 0; d <= max && !done; d++) {
      trace.push(v.slice());
      for (let k = -d; k <= d; k += 2) {
        let x;
        if (k === -d || (k !== d && v[offset + k - 1] < v[offset + k + 1])) x = v[offset + k + 1];
        else x = v[offset + k - 1] + 1;
        let y = x - k;
        while (x < N && y < M && a[x] === b[y]) { x++; y++; }
        v[offset + k] = x;
        if (x >= N && y >= M) { done = true; break; }
      }
    }
    const ops = [];
    let x = N, y = M;
    for (let d = trace.length - 1; d >= 0; d--) {
      const vv = trace[d];
      const k = x - y;
      let prevK;
      if (k === -d || (k !== d && vv[offset + k - 1] < vv[offset + k + 1])) prevK = k + 1;
      else prevK = k - 1;
      const prevX = vv[offset + prevK];
      const prevY = prevX - prevK;
      while (x > prevX && y > prevY) { ops.push({ op: "eq", line: a[x - 1] }); x--; y--; }
      if (d > 0) {
        if (x === prevX) { ops.push({ op: "add", line: b[y - 1] }); y--; }
        else { ops.push({ op: "del", line: a[x - 1] }); x--; }
      }
    }
    ops.reverse();
    return ops;
  }

  // buildHunks groups an edit script into unified-diff hunks with context lines around each change region.
  function buildHunks(ops, context) {
    context = context == null ? 3 : context;
    let oldN = 0, newN = 0;
    const ann = [];
    for (const o of ops) {
      if (o.op === "eq") { oldN++; newN++; ann.push({ op: "eq", line: o.line, oldN, newN }); }
      else if (o.op === "del") { oldN++; ann.push({ op: "del", line: o.line, oldN, newN: null }); }
      else { newN++; ann.push({ op: "add", line: o.line, oldN: null, newN }); }
    }
    const n = ann.length;
    const isCh = (idx) => ann[idx].op !== "eq";
    const hunks = [];
    let i = 0, prevStop = -1;
    while (i < n) {
      if (!isCh(i)) { i++; continue; }
      let regionEnd = i;
      let j = i + 1;
      while (j < n) {
        if (isCh(j)) { regionEnd = j; j++; continue; }
        let k = j;
        while (k < n && !isCh(k)) k++;
        if (k < n && (k - j) <= 2 * context) { j = k; continue; }
        break;
      }
      const start = Math.max(0, i - context);
      const stop = Math.min(n - 1, regionEnd + context);
      const lines = ann.slice(start, stop + 1);
      let oldStart = 0, oldCount = 0, newStart = 0, newCount = 0;
      for (const l of lines) {
        if (l.op !== "add") { oldCount++; if (!oldStart) oldStart = l.oldN; }
        if (l.op !== "del") { newCount++; if (!newStart) newStart = l.newN; }
      }
      const skipped = ann.slice(prevStop + 1, start);
      hunks.push({ oldStart: oldCount ? oldStart : 0, oldCount, newStart: newCount ? newStart : 0, newCount, lines, skipped });
      prevStop = stop;
      i = stop + 1;
    }
    return hunks;
  }

  // intraLine returns the shared prefix and suffix and each side's differing middle; null for identical or over-long lines.
  function intraLine(delLine, addLine) {
    const a = delLine == null ? "" : String(delLine);
    const b = addLine == null ? "" : String(addLine);
    if (a === b) return null;
    if (a.length > 500 || b.length > 500) return null;
    const max = Math.min(a.length, b.length);
    let p = 0;
    while (p < max && a[p] === b[p]) p++;
    let s = 0;
    while (s < max - p && a[a.length - 1 - s] === b[b.length - 1 - s]) s++;
    return {
      prefix: a.slice(0, p),
      delMid: a.slice(p, a.length - s),
      addMid: b.slice(p, b.length - s),
      suffix: a.slice(a.length - s),
    };
  }

  // diffTrees compares two git trees and returns the changed paths sorted by path; there is no rename detection.
  async function diffTrees(ctx, shaA, shaB, prefix) {
    const out = [];
    const state = { truncated: false };
    await diffCollect(ctx, shaA, shaB, prefix || "", out, state);
    out.sort((x, y) => (x.path < y.path ? -1 : x.path > y.path ? 1 : 0));
    out.truncated = state.truncated;
    return out;
  }

  // DIFF_TREE_CONCURRENCY bounds diffCollect's parallel tree fetches.
  const DIFF_TREE_CONCURRENCY = 8;

  // diffCollect is diffTrees' bounded worker; a concurrent batch can overshoot DIFF_TREE_SCAN_CAP by a few records.
  async function diffCollect(ctx, shaA, shaB, prefix, out, state) {
    const push = (rec) => { if (state.truncated) return; out.push(rec); if (out.length >= DIFF_TREE_SCAN_CAP) state.truncated = true; };
    const queue = [{ shaA, shaB, prefix }];
    const visit = async (job) => {
      if (state.truncated) return;
      const [entriesA, entriesB] = await Promise.all([
        job.shaA ? getTree(ctx, job.shaA).then((e) => e || []) : [],
        job.shaB ? getTree(ctx, job.shaB).then((e) => e || []) : [],
      ]);
      const mapA = new Map(entriesA.map((e) => [e.name, e]));
      const mapB = new Map(entriesB.map((e) => [e.name, e]));
      const names = new Set();
      for (const k of mapA.keys()) names.add(k);
      for (const k of mapB.keys()) names.add(k);
      for (const name of names) {
        if (state.truncated) return;
        const a = mapA.get(name), b = mapB.get(name);
        const path = job.prefix ? job.prefix + "/" + name : name;
        const aTree = a && a.type === "tree", bTree = b && b.type === "tree";
        if (a && b) {
          if (a.sha === b.sha && a.mode === b.mode) continue;
          if (aTree && bTree) queue.push({ shaA: a.sha, shaB: b.sha, prefix: path });
          else if (aTree) {
            queue.push({ shaA: a.sha, shaB: null, prefix: path });
            push({ path, status: "added", shaA: null, shaB: b.sha, modeA: null, modeB: b.mode });
          } else if (bTree) {
            push({ path, status: "deleted", shaA: a.sha, shaB: null, modeA: a.mode, modeB: null });
            queue.push({ shaA: null, shaB: b.sha, prefix: path });
          } else push({ path, status: "modified", shaA: a.sha, shaB: b.sha, modeA: a.mode, modeB: b.mode });
        } else if (a) {
          if (aTree) queue.push({ shaA: a.sha, shaB: null, prefix: path });
          else push({ path, status: "deleted", shaA: a.sha, shaB: null, modeA: a.mode, modeB: null });
        } else {
          if (bTree) queue.push({ shaA: null, shaB: b.sha, prefix: path });
          else push({ path, status: "added", shaA: null, shaB: b.sha, modeA: null, modeB: b.mode });
        }
      }
    };
    while (queue.length && !state.truncated) {
      await Promise.all(queue.splice(0, DIFF_TREE_CONCURRENCY).map(visit));
    }
  }

  // commitTree returns a commit's root tree sha, or null.
  async function commitTree(ctx, sha) {
    const obj = await getObject(ctx, sha);
    if (!obj || obj.type !== "commit") return null;
    return parseCommit(sha, obj.body).tree;
  }

  // mergeBase walks both ancestries under one bounded budget and returns the first common ancestor closest to base.
  async function mergeBase(ctx, headSha, baseSha, cap) {
    cap = cap || WALK_CAP;
    let visited = 0;
    const step = async (frontier, taken, onCommit) => {
      const batch = [];
      while (frontier.length && batch.length < CONCURRENCY && visited < cap) {
        const h = frontier.shift();
        if (h && !taken.has(h)) { taken.add(h); visited++; batch.push(h); }
      }
      if (!batch.length) return null;
      const objs = await Promise.all(batch.map((h) => getObject(ctx, h)));
      for (let i = 0; i < batch.length; i++) {
        const found = onCommit(batch[i]);
        if (found) return found;
        const obj = objs[i];
        if (obj && obj.type === "commit") for (const p of parseCommit(batch[i], obj.body).parents) frontier.push(p);
      }
      return null;
    };
    const headAnc = new Set();
    let frontier = [headSha];
    while (frontier.length && visited < cap) await step(frontier, headAnc, () => null);
    const seen = new Set();
    frontier = [baseSha];
    while (frontier.length && visited < cap) {
      const hit = await step(frontier, seen, (h) => (headAnc.has(h) ? h : null));
      if (hit) return hit;
    }
    return null;
  }

  // fileDiff fetches one changed entry's blob pair and produces its diff model; force bypasses both size caps.
  async function fileDiff(ctx, entry, force) {
    const aObj = entry.shaA ? await getContentObject(ctx, entry.shaA) : null;
    const bObj = entry.shaB ? await getContentObject(ctx, entry.shaB) : null;
    const aBytes = aObj ? aObj.body : new Uint8Array(0);
    const bBytes = bObj ? bObj.body : new Uint8Array(0);
    if (isBinary(aBytes) || isBinary(bBytes)) return { binary: true };
    if (!force && (aBytes.length > DIFF_BLOB_CAP || bBytes.length > DIFF_BLOB_CAP)) return { tooLarge: true };
    const ops = diffLines(new TextDecoder().decode(aBytes), new TextDecoder().decode(bBytes), force);
    if (ops === null) return { tooLarge: true };
    let adds = 0, dels = 0;
    for (const o of ops) { if (o.op === "add") adds++; else if (o.op === "del") dels++; }
    return { hunks: buildHunks(ops, 3), adds, dels };
  }

  // ---- Routing (DOM-free, testable) ----

  // An item permalink is a workspace-relative gitmsg ref, so these maps translate between a branch, its extension and its index tab.
  const COMMIT_VIEW = {
    "gitmsg/social": { ext: "social", tab: "timeline", label: "Post" },
    "gitmsg/pm": { ext: "pm", tab: "issues", label: "Issue" },
    "gitmsg/review": { ext: "review", tab: "prs", label: "Pull request" },
    "gitmsg/release": { ext: "release", tab: "releases", label: "Release" },
    "gitmsg/memo": { ext: "memo", tab: "memos", label: "Memo" },
  };
  const LEGACY_BRANCH = { issue: "gitmsg/pm", pr: "gitmsg/review", release: "gitmsg/release", commit: "" };
  const INDEX_TABS = { timeline: 1, issues: 1, prs: 1, releases: 1, memos: 1, milestones: 1, sprints: 1 };

  // commitRef builds a workspace-relative gitmsg commit ref fragment.
  function commitRef(hash, branch) {
    return "#commit:" + hash + "@" + (branch || "");
  }

  // compareRef builds a compare route fragment with each side URL-encoded.
  function compareRef(base, head) {
    return "#/compare:" + encodeURIComponent(base || "") + "..." + encodeURIComponent(head || "");
  }

  // legacyCommit resolves a legacy detail route to its #commit: target and records the canonical fragment.
  function legacyCommit(hash, branch) {
    const clean = hash.toLowerCase();
    return { type: "commit", hash: clean, branch, canonical: commitRef(clean, branch), legacy: true };
  }

  // parseRoute maps a location.hash fragment to a route descriptor; fragments carry ":" "@" "/" unencoded, so parsing is positional.
  function parseRoute(rawHash) {
    const frag = (rawHash || "").replace(/^#/, "");
    if (frag === "" || frag === "/") return { type: "home" };
    if (frag[0] === "/") {
      // The commits route carries a row anchor as a first-class suffix, so both are parsed off the full fragment before the "/"-split grammar.
      if (frag === "/commits" || frag.startsWith("/commits/") || frag.startsWith("/commits:")) {
        const cm = /^\/commits(?:\/(\d+))?(?::([A-Za-z0-9][\w.-]*))?$/.exec(frag);
        if (!cm) return { type: "notfound" };
        const route = { type: "commits", page: cm[1] ? parseInt(cm[1], 10) : 0 };
        if (cm[2]) route.anchor = cm[2];
        return route;
      }
      if (frag.startsWith("/compare:")) {
        const value = frag.slice("/compare:".length);
        const dots = value.indexOf("...");
        const dec = (s) => { try { return decodeURIComponent(s); } catch { return s; } };
        const rawBase = dots < 0 ? value : value.slice(0, dots);
        const rawHead = dots < 0 ? "" : value.slice(dots + 3);
        return { type: "compare", base: dec(rawBase), head: dec(rawHead) };
      }
      const parts = frag.slice(1).split("/");
      const head = parts[0];
      const rest = parts.slice(1).join("/");
      if (head === "" || head === "home") return { type: "home" };
      if (INDEX_TABS[head]) return { type: "index", tab: head };
      if (head === "tree" || head === "code") return { type: "code" };
      if (head === "branches") return { type: "branches" };
      if (head === "graph") return { type: "graph" };
      if (head === "tags") return { type: "tags" };
      if (head === "analytics") return { type: "analytics" };
      if (head === "board") return { type: "board" };
      if (head === "search") return { type: "search", q: rest ? decodeURIComponent(rest) : "" };
      if (head === "lists") return { type: "lists" };
      if (head === "config") return { type: "config" };
      if (rest && head in LEGACY_BRANCH) return legacyCommit(rest, LEGACY_BRANCH[head]);
      return { type: "notfound" };
    }
    // A plain fragment is an in-page anchor into the home README.
    if (/^[A-Za-z0-9][\w.-]*$/.test(frag)) return { type: "home", anchor: frag };
    const colon = frag.indexOf(":");
    if (colon <= 0) return { type: "notfound" };
    const reftype = frag.slice(0, colon);
    const value = frag.slice(colon + 1);
    if (reftype === "commit") {
      const at = value.indexOf("@");
      const hash = (at < 0 ? value : value.slice(0, at)).toLowerCase();
      const branch = at < 0 ? "" : value.slice(at + 1);
      if (!/^[0-9a-f]{7,40}$/.test(hash)) return { type: "notfound" };
      return { type: "commit", hash, branch };
    }
    if (reftype === "compare") {
      // Each compare side is URL-encoded; a missing side stays blank, so the page opens with its pickers.
      const dots = value.indexOf("...");
      const rawBase = dots < 0 ? value : value.slice(0, dots);
      const rawHead = dots < 0 ? "" : value.slice(dots + 3);
      const dec = (s) => { try { return decodeURIComponent(s); } catch { return s; } };
      return { type: "compare", base: dec(rawBase), head: dec(rawHead) };
    }
    if (reftype === "branch") return { type: "branch", name: value };
    if (reftype === "tag") return { type: "tag", name: value };
    if (reftype === "list") return { type: "list", id: value };
    if (reftype === "file") {
      const at = value.indexOf("@");
      const path = at < 0 ? value : value.slice(0, at);
      let branch = at < 0 ? "" : value.slice(at + 1);
      // Branch names cannot contain ":", so a suffix after it is a line anchor or a heading anchor.
      let line = null, lineEnd = null, anchor = "";
      const lc = branch.indexOf(":");
      if (lc >= 0) {
        const suffix = branch.slice(lc + 1);
        const lm = /^L(\d+)(?:-(\d+))?$/.exec(suffix);
        if (lm) { line = parseInt(lm[1], 10); if (lm[2]) lineEnd = parseInt(lm[2], 10); }
        else if (/^[A-Za-z0-9][\w.-]*$/.test(suffix)) anchor = suffix;
        branch = branch.slice(0, lc);
      }
      const route = { type: "file", path, branch, line, lineEnd };
      if (anchor) route.anchor = anchor;
      return route;
    }
    return { type: "notfound" };
  }

  // ---- PM aggregation (DOM-free, testable) ----

  // releaseAssets turns a release header's asset fields into entries, with external hrefs when artifact-url is set.
  function releaseAssets(header) {
    header = header || {};
    const base = (header["artifact-url"] || "").replace(/\/$/, "");
    const href = (name) => (base ? base + "/" + name : null);
    const artifacts = (header.artifacts || "").split(",").map((s) => s.trim()).filter(Boolean).map((name) => ({ name, href: href(name) }));
    const checksums = header.checksums ? { name: header.checksums, href: href(header.checksums) } : null;
    const sbom = header.sbom ? { name: header.sbom, href: href(header.sbom) } : null;
    return { artifactUrl: base, artifacts, checksums, sbom, signedBy: header["signed-by"] || "" };
  }

  // homeFilesTruncation names the root entries the front page hides: the notice sentence and the control label. Mirrors siteFrontFilesTruncation in site_pages_html.go.
  function homeFilesTruncation(total, limit) {
    if (total <= limit) return { notice: "", label: "" };
    return { notice: (total - limit) + " more not shown.", label: "Show all " + total };
  }

  // releaseAssetLabel words a release row's asset count, "" when it names none. Mirrors siteReleaseAssetLabel in site_pages_html.go.
  function releaseAssetLabel(artifacts) {
    const n = (artifacts || "").split(",").map((s) => s.trim()).filter(Boolean).length;
    if (!n) return "";
    return n === 1 ? "1 asset" : n + " assets";
  }

  // headSubject titles a card or detail head: a release leads with its tag, every other type with its first line. Mirrors siteHeadSubject in site_pages_html.go.
  function headSubject(header, ext, subject) {
    const h = header || {};
    if ((h.type || ext) !== "release") return subjectText(subject) || "(untitled)";
    if (h.tag) return h.tag;
    if (h.version) return "v" + h.version;
    return subjectText(subject) || "(release)";
  }

  // releaseVersionChip labels a release head's version chip, "" when the head already names the version. Mirrors siteReleaseVersionChip in site_pages_html.go.
  function releaseVersionChip(version, head) {
    if (!version || head === version || head === "v" + version) return "";
    return "v" + version;
  }

  // CHIP_STATE_CLASSES are the workflow states with a solid-fill chip class of their own.
  const CHIP_STATE_CLASSES = { open: 1, closed: 1, merged: 1, completed: 1, active: 1, planned: 1 };

  // chipStateClass maps a workflow state to its solid-fill chip class. Mirrors sitePageChipStateClass in site_pages_html.go.
  function chipStateClass(state) {
    if (/^cancel/.test(state || "")) return "canceled";
    return CHIP_STATE_CLASSES[state] ? state : "unknown";
  }

  // HEAD_STATE_TYPES are the item types whose head carries a state pill.
  const HEAD_STATE_TYPES = { issue: 1, milestone: 1, sprint: 1, "pull-request": 1 };

  // headChips lists a head's chip slot: the item's state pill, then a release's version. Mirrors siteHeadChips in site_pages_html.go.
  function headChips(header, ext, head, retracted) {
    const h = header || {};
    if (retracted || h.retracted === "true") return [{ class: "chip-retracted", label: "retracted" }];
    const type = h.type || EXT_DEFAULT_TYPE[ext] || ext;
    if (type === "release") {
      const chips = h.prerelease === "true" ? [{ class: "pre state", label: "prerelease" }] : [];
      const version = releaseVersionChip(h.version, head);
      return version ? chips.concat([{ class: "", label: version }]) : chips;
    }
    if (!HEAD_STATE_TYPES[type]) return [];
    if (type === "pull-request" && h.draft === "true") return [{ class: "", label: "draft" }];
    const state = h.state || "open";
    return [{ class: "state " + chipStateClass(state), label: state }];
  }

  // GLYPH_STATE_TYPES tint their glyph by state, so a row of that type drops the state pill its head would carry.
  const GLYPH_STATE_TYPES = { issue: 1, "pull-request": 1 };

  // rowChips lists a row's head chips: the head's own chips, less the state pill a tinted glyph already carries. Mirrors siteRowChips in site_pages_html.go.
  function rowChips(header, ext, head, retracted) {
    const h = header || {};
    const chips = headChips(h, ext, head, retracted);
    if (!chips.length || retracted || h.retracted === "true") return chips;
    const type = h.type || EXT_DEFAULT_TYPE[ext] || ext;
    return GLYPH_STATE_TYPES[type] && /^state /.test(chips[0].class) ? chips.slice(1) : chips;
  }

  // rowHeadChips splits a row's head chips by slot: the pill leading the subject, then the release version trailing it. Mirrors siteRowHeadChips in site_pages_html.go.
  function rowHeadChips(header, ext, head, retracted) {
    const chips = rowChips(header, ext, head, retracted);
    const lead = chips.length && chips[0].label !== releaseVersionChip((header || {}).version, head) ? chips[0] : null;
    return { lead, tail: chips.slice(lead ? 1 : 0) };
  }

  // stateCounts tallies items by header state and returns the total with a per-state map.
  function stateCounts(items) {
    const byState = {};
    for (const it of items) { const s = (it.header && it.header.state) || "open"; byState[s] = (byState[s] || 0) + 1; }
    return { total: items.length, byState };
  }

  // hashEq compares two commit hashes by prefix, tolerating either being the longer.
  function hashEq(a, b) { return !!a && !!b && (a === b || a.startsWith(b) || b.startsWith(a)); }

  // THREAD_MAX_DEPTH caps a thread's visual indent; the tree keeps its full logical depth.
  const THREAD_MAX_DEPTH = 4;

  // threadTime returns a comment's chronological sort key, origin time over git author time.
  function threadTime(item) { return item.effectiveTime || (item.commit && item.commit.authorTime) || 0; }

  // groupThread builds a comment tree under an item: original decides membership, reply-to decides nesting, per GITSOCIAL 1.3.
  function groupThread(itemShort, comments) {
    const mine = comments.filter((c) => hashEq(refHash(c.header.original), itemShort));
    const byShort = new Map();
    for (const c of mine) byShort.set(c.commit.short, { comment: c, depth: 0, replies: [] });
    const roots = [];
    for (const c of mine) {
      const node = byShort.get(c.commit.short);
      const rt = refHash(c.header["reply-to"]);
      let parent = null;
      if (rt && !hashEq(rt, itemShort)) {
        for (const [short, n] of byShort) if (n !== node && hashEq(short, rt)) { parent = n; break; }
      }
      if (parent) parent.replies.push(node);
      else roots.push(node);
    }
    const cmp = (a, b) => threadTime(a.comment) - threadTime(b.comment);
    const seen = new Set();
    const assignDepth = (nodes, depth) => {
      nodes.sort(cmp);
      for (const n of nodes) {
        if (seen.has(n)) continue;
        seen.add(n);
        n.depth = Math.min(depth, THREAD_MAX_DEPTH);
        assignDepth(n.replies, depth + 1);
      }
    };
    assignDepth(roots, 0);
    return roots;
  }

  // flattenThread walks a thread node tree depth-first into a flat [{ comment, depth }] list.
  function flattenThread(nodes, out) {
    out = out || [];
    for (const n of nodes) { out.push({ comment: n.comment, depth: n.depth }); flattenThread(n.replies, out); }
    return out;
  }

  // TIMELINE_SPECS lists the data branches the merged timeline walks, with the item-type filter each carries; memo is excluded.
  const TIMELINE_SPECS = [
    { ext: "social", branch: "gitmsg/social", type: null },
    { ext: "pm", branch: "gitmsg/pm", type: null },
    { ext: "review", branch: "gitmsg/review", type: "pull-request" },
    { ext: "release", branch: "gitmsg/release", type: "release" },
  ];

  // TIMELINE_WINDOW caps how many merged items the timeline renders and hydrates per autoscroll step.
  const TIMELINE_WINDOW = 50;

  // timelineTyped applies a spec's optional item-type filter to resolved items.
  function timelineTyped(spec, items) {
    if (!spec.type) return items;
    return items.filter((it) => ((it.header && it.header.type) || "") === spec.type);
  }

  // loadTimelineItems builds the merged timeline feed and hydrates every item; the interactive route uses loadTimelineWindow.
  async function loadTimelineItems(ctx) {
    const out = [];
    const [lanes, code] = await Promise.all([
      Promise.all(TIMELINE_SPECS.map(async (spec) => ({ spec, items: await loadExtItems(ctx, spec.ext) }))),
      resolveCodeItems(ctx, WALK_CAP),
    ]);
    for (const { spec, items } of lanes) {
      for (const it of timelineTyped(spec, items)) {
        it._ext = spec.ext; it._branch = spec.branch;
        out.push(it);
      }
    }
    for (const it of code.items) { it._ext = "code"; out.push(it); }
    out.sort((a, b) => b.effectiveTime - a.effectiveTime);
    return out;
  }

  // resolveExtItems returns an ext's resolved items un-hydrated, walking far enough to surface need items or exhaust the branch.
  async function resolveExtItems(ctx, ext, need) {
    const w = await extWalkState(ctx, ext);
    if (!w) return { items: [], more: false };
    return withWalkLock(w, async () => {
      if (w.state.commits.length === 0) await stepExtWalk(ctx, w, WALK_CAP);
      // Progress guard: a step that adds no commits and drains no shard breaks the loop rather than spinning.
      let guardN = w.state.commits.length, guardShards = (w.older || []).length, stall = 0;
      while (!extWalkExhausted(w) && w.state.commits.length < need) {
        await stepExtWalk(ctx, w, WALK_CAP);
        const n = w.state.commits.length, s = (w.older || []).length;
        if (n === guardN && s === guardShards) { if (++stall >= 2) break; } else stall = 0;
        guardN = n; guardShards = s;
      }
      return { items: resolveItems(walkedCommits(w.state)), more: !extWalkExhausted(w) };
    });
  }

  // codeCommitItem wraps a plain commit as a timeline item in the shape resolveItems produces.
  function codeCommitItem(commit, branch) {
    return {
      commit, header: {}, content: commit.content, rawMessage: commit.rawMessage,
      edited: false, editorName: "", author: effectiveAuthor(commit, null),
      effectiveTime: commit.authorTime || 0, versions: [], _code: true, _branch: branch || "",
    };
  }

  // codeMetaCommit converts one code index entry into a body-less commit record carrying its subject, branch and parents.
  function codeMetaCommit(e) {
    return {
      hash: e.sha, short: String(e.sha || "").slice(0, 12), tree: "",
      parents: Array.isArray(e.parents) ? e.parents : [],
      authorName: e.author || "", authorEmail: e.email || "", authorTime: e.ts || 0,
      content: String(e.subject || ""), rawMessage: String(e.subject || ""),
      subject: String(e.subject || ""), gitmsg: null, refs: [], _branch: e.branch || "",
    };
  }

  // codeWalkState returns the single resumable walk over every pushed code branch, seeded at all their tips; null on a data-only bucket.
  async function codeWalkState(ctx) {
    const key = "codeTimeline";
    let w = ctx.walks[key];
    if (w) return w;
    const { branches, defaultBranch } = await listBranches(ctx);
    const code = branches.filter((b) => !b.name.startsWith("gitmsg/"));
    const seeds = [];
    const tipBranch = {};
    for (const b of code) {
      const sha = await refTip(ctx, b.ref);
      if (!sha) continue;
      if (!(sha in tipBranch)) tipBranch[sha] = b.name;
      seeds.push(sha);
    }
    if (!seeds.length) return null;
    w = ctx.walks[key] = { state: { visited: new Set(), frontier: seeds.slice(), commits: [], tipBranch, reachedVia: {}, defaultBranch } };
    return w;
  }

  // resolveCodeItems returns plain commits across the code branches as timeline items, from the code index when the bucket carries one.
  async function resolveCodeItems(ctx, need) {
    const indexed = await resolveCodeItemsIndexed(ctx, need);
    if (indexed) return indexed;
    const w = await codeWalkState(ctx);
    if (!w) return { items: [], more: false };
    return withWalkLock(w, async () => {
      if (w.state.commits.length === 0) await graphWalkStep(ctx, w.state, WALK_CAP);
      while (w.state.frontier.length && w.state.commits.length < need) await graphWalkStep(ctx, w.state, WALK_CAP);
      const items = w.state.commits.filter((c) => !c.gitmsg).map((c) => codeCommitItem(c, w.state.reachedVia[c.hash] || w.state.tipBranch[c.hash] || ""));
      return { items, more: w.state.frontier.length > 0 };
    });
  }

  // resolveCodeItemsIndexed sources the timeline's code commits from the code index, or null when the bucket carries none.
  async function resolveCodeItemsIndexed(ctx, need) {
    const w = await codeIndexWalkState(ctx);
    if (!w) return null;
    return withWalkLock(w, async () => {
      let guard = (w.older || []).length, stall = 0;
      while ((w.older && w.older.length) && w.items.length < need) {
        await loadNextCodeShard(ctx, w);
        const n = (w.older || []).length;
        if (n === guard) { if (++stall >= 2) break; } else stall = 0;
        guard = n;
      }
      const items = w.items.map((c) => codeCommitItem(c, c._branch || ""));
      const more = (w.older && w.older.length > 0) || !w.complete;
      return { items, more };
    });
  }

  // codeIndexWalkState returns the resumable index-backed code walk; hasParents marks a v5 corpus, whose entries carry parent shas.
  async function codeIndexWalkState(ctx) {
    const key = "codeIndex";
    if (ctx.walks[key] !== undefined) return ctx.walks[key];
    let idx = null;
    try { idx = await loadItemsIndex(ctx, "code"); } catch (e) { if (e && e.forbidden) throw e; }
    if (!idx) { ctx.walks[key] = null; return null; }
    const seen = new Set();
    const items = [];
    for (const e of idx.items) { if (!seen.has(e.sha)) { seen.add(e.sha); items.push(codeMetaCommit(e)); } }
    const w = { items, seen, older: idx.olderShards.slice(), complete: idx.complete, hasParents: idx.version >= 5 };
    ctx.walks[key] = w;
    return w;
  }

  // loadNextCodeShard pulls the next-older pending shard onto the index-backed code walk.
  async function loadNextCodeShard(ctx, w) {
    if (!w.older || !w.older.length) return false;
    const keyName = w.older.shift();
    const text = await fetchText(ctx.base, keyName);
    if (!text) return true;
    let doc;
    try { doc = JSON.parse(text); } catch { return true; }
    const entries = (doc && Array.isArray(doc.items)) ? doc.items.slice().reverse() : [];
    for (const e of entries) {
      if (w.seen.has(e.sha)) continue;
      w.seen.add(e.sha);
      w.items.push(codeMetaCommit(e));
    }
    return true;
  }

  // codeAncestry drains the code index into a hash to commit Map for ancestry questions; null without a v5 corpus.
  function codeAncestry(ctx) {
    const key = "codeAncestry";
    if (ctx.walks[key] !== undefined) return ctx.walks[key];
    const load = (async () => {
      const w = await codeIndexWalkState(ctx);
      if (!w || !w.hasParents) return null;
      return withWalkLock(w, async () => {
        let guard = (w.older || []).length, stall = 0;
        while (w.older && w.older.length) {
          await loadNextCodeShard(ctx, w);
          const n = (w.older || []).length;
          if (n === guard) { if (++stall >= 2) break; } else stall = 0;
          guard = n;
        }
        const map = new Map();
        for (const c of w.items) map.set(c.hash, c);
        return map;
      });
    })();
    ctx.walks[key] = load;
    return load;
  }

  // indexAncestors collects every ancestor of sha present in an ancestry map; descent stops at parents outside it, so the set is a subset of the truth.
  function indexAncestors(map, sha) {
    const seen = new Set();
    const frontier = sha && map.has(sha) ? [sha] : [];
    while (frontier.length) {
      const h = frontier.pop();
      if (seen.has(h)) continue;
      seen.add(h);
      const c = map.get(h);
      if (c) for (const p of c.parents || []) if (map.has(p) && !seen.has(p)) frontier.push(p);
    }
    return seen;
  }

  // resolveMergeBase returns the merge base from the ancestry index when it covers both endpoints, else from the bounded loose walk.
  async function resolveMergeBase(ctx, headSha, baseSha, cap) {
    const map = await codeAncestry(ctx);
    if (map && map.has(headSha) && map.has(baseSha)) {
      const headAnc = indexAncestors(map, headSha);
      const seen = new Set();
      const frontier = [baseSha];
      while (frontier.length) {
        const h = frontier.shift();
        if (!h || seen.has(h)) continue;
        seen.add(h);
        if (headAnc.has(h)) return h;
        const c = map.get(h);
        if (c) for (const p of c.parents || []) frontier.push(p);
      }
      // Fall through: an incomplete corpus can hide the common ancestor even with both endpoints indexed.
    }
    return mergeBase(ctx, headSha, baseSha, cap);
  }

  // indexCompareCommits returns a compare's head-side commits from the ancestry index; null when the index cannot answer.
  async function indexCompareCommits(ctx, baseSha, headSha) {
    const map = await codeAncestry(ctx);
    if (!map || !map.has(headSha) || (baseSha && !map.has(baseSha))) return null;
    const excluded = baseSha ? indexAncestors(map, baseSha) : new Set();
    const out = [];
    const seen = new Set();
    const frontier = [headSha];
    while (frontier.length) {
      const h = frontier.pop();
      if (seen.has(h) || excluded.has(h)) continue;
      seen.add(h);
      const c = map.get(h);
      if (!c) continue;
      out.push(c);
      for (const p of c.parents || []) if (!seen.has(p) && !excluded.has(p)) frontier.push(p);
    }
    out.sort((a, b) => b.authorTime - a.authorTime);
    return out;
  }

  // resolveShortShaFromIndex resolves a short sha to its full sha from the code index; the first newest-first prefix match wins.
  async function resolveShortShaFromIndex(ctx, short) {
    if (!short) return null;
    const w = await codeIndexWalkState(ctx);
    if (!w) return null;
    return withWalkLock(w, async () => {
      const hit = () => (w.items.find((c) => c.hash.startsWith(short)) || {}).hash || null;
      let found = hit();
      let guard = (w.older || []).length, stall = 0;
      while (!found && w.older && w.older.length) {
        await loadNextCodeShard(ctx, w);
        found = hit();
        const n = (w.older || []).length;
        if (n === guard) { if (++stall >= 2) break; } else stall = 0;
        guard = n;
      }
      return found;
    });
  }

  // COMMITS_PAGE_SIZE is one commits list page's row count and must match the page layer's sitePagesListSize.
  const COMMITS_PAGE_SIZE = 100;

  // SITE_PAGES_KEY is the HTML page layer's manifest, which publishes the commits list's pagination.
  const SITE_PAGES_KEY = ".gitsocial/site/pages.json";

  // loadCommitsLayout reads the commits list's published partition from the page manifest; a fast path, with loadCommitsPage deriving it when absent.
  async function loadCommitsLayout(ctx) {
    if (ctx.commitsLayout !== undefined) return ctx.commitsLayout;
    let layout = null;
    try {
      const text = await fetchText(ctx.base, SITE_PAGES_KEY);
      const doc = text ? JSON.parse(text) : null;
      const c = doc && doc.commits;
      if (c) layout = { sealed: Math.max(0, c.sealed | 0), frontier: String(c.frontier || ""), total: Math.max(0, c.total | 0) };
    } catch (e) { layout = null; }
    ctx.commitsLayout = layout;
    return layout;
  }

  // loadCommitsPage returns one page of the default branch's commit list, page 0 being the mutable head, drained only as deep as that page needs.
  async function loadCommitsPage(ctx, page) {
    const { defaultBranch } = await listBranches(ctx);
    const size = COMMITS_PAGE_SIZE;
    const w = await codeIndexWalkState(ctx);
    if (!w) {
      // Pre-index bucket: the bounded loose walk, which is the head and nothing else.
      const r = await resolveCodeItems(ctx, size);
      const rows = r.items.filter((it) => (it._branch || "") === defaultBranch).map((it) => it.commit).slice(0, size);
      return { branch: defaultBranch, rows, page: 0, sealed: 0, total: rows.length };
    }
    const drain = (enough) => withWalkLock(w, async () => {
      const pick = () => w.items.filter((c) => (c._branch || "") === defaultBranch);
      let rows = pick();
      let guard = (w.older || []).length, stall = 0;
      while (!enough(rows) && w.older && w.older.length) {
        await loadNextCodeShard(ctx, w);
        rows = pick();
        const n = (w.older || []).length;
        if (n === guard) { if (++stall >= 2) break; } else stall = 0;
        guard = n;
      }
      return rows;
    });
    const layout = await loadCommitsLayout(ctx);
    let sealed = layout ? layout.sealed : 0;
    let total = layout ? layout.total : 0;
    const frontier = layout && /^[0-9a-f]{12}$/.test(layout.frontier) ? layout.frontier : "";
    let rows = [], head = -1;
    if (sealed > 0 && frontier) {
      rows = await drain((rs) => rs.some((c) => c.short === frontier));
      head = rows.findIndex((c) => c.short === frontier);
      if (head >= 0 && page > 0) {
        const want = head + (sealed - page + 1) * size;
        rows = await drain((rs) => rs.length >= want);
        head = rows.findIndex((c) => c.short === frontier);
      }
    }
    if (head < 0) {
      rows = await drain(() => false);
      total = rows.length;
      sealed = total > 0 ? Math.floor((total - 1) / size) : 0;
      head = total - sealed * size;
    }
    if (!layout || page > sealed) total = Math.max(total, rows.length);
    if (page > sealed) return { branch: defaultBranch, rows: [], page, sealed, total, missing: page > 0 };
    const from = page > 0 ? head + (sealed - page) * size : 0;
    const to = page > 0 ? from + size : head;
    return { branch: defaultBranch, rows: rows.slice(from, to), page, sealed, total };
  }

  // loadTimelineWindow is the autoscroll-paged merged timeline: merge every branch's metadata, take the newest shown, hydrate that slice alone.
  async function loadTimelineWindow(ctx, extend) {
    const tl = ctx.timeline || (ctx.timeline = { shown: 0 });
    tl.shown = extend ? tl.shown + TIMELINE_WINDOW : TIMELINE_WINDOW;
    const need = tl.shown;
    const merged = [];
    let more = false;
    // Every lane in parallel: each is an independent chain, and the plain-code lane joins the same batch.
    const [lanes, code] = await Promise.all([
      Promise.all(TIMELINE_SPECS.map(async (spec) => ({ spec, r: await resolveExtItems(ctx, spec.ext, need) }))),
      resolveCodeItems(ctx, need),
    ]);
    for (const { spec, r } of lanes) {
      if (r.more) more = true;
      for (const it of timelineTyped(spec, r.items)) {
        it._ext = spec.ext; it._branch = spec.branch;
        merged.push(it);
      }
    }
    if (code.more) more = true;
    for (const it of code.items) { it._ext = "code"; merged.push(it); }
    merged.sort((a, b) => b.effectiveTime - a.effectiveTime);
    const windowItems = merged.slice(0, need);
    await hydrateItems(ctx, windowItems);
    return { items: windowItems, truncated: more || merged.length > need };
  }

  // HOME_ACTIVITY_LIMIT caps the home view's recent-activity rows and mirrors the page layer's sitePagesHomeActivity.
  const HOME_ACTIVITY_LIMIT = 10;

  // HOME_ACTIVITY_NEED over-requests per branch: replies and edits are commits too, and would otherwise crowd out top-level items.
  const HOME_ACTIVITY_NEED = HOME_ACTIVITY_LIMIT * 4;

  // HOME_ACTIVITY_SPECS lists the data branches the section merges and the default item type each carries.
  const HOME_ACTIVITY_SPECS = [
    { ext: "pm", branch: "gitmsg/pm", type: "issue" },
    { ext: "review", branch: "gitmsg/review", type: "pull-request" },
    { ext: "social", branch: "gitmsg/social", type: "post" },
    { ext: "release", branch: "gitmsg/release", type: "release" },
  ];

  // homeActivityRoot mirrors the page layer's thread rule: a social comment and a review feedback are replies, so neither side lists them.
  function homeActivityRoot(ext, type) {
    if (ext === "social" && type === "comment") return false;
    if (ext === "review" && type === "feedback") return false;
    return true;
  }

  // loadHomeActivity returns the newest top-level items across the data branches, metadata only, matching buildSiteFrontActivity.
  async function loadHomeActivity(ctx) {
    const merged = [];
    // All lanes in parallel; the code lane is indexed only, so a bucket without a code index shows no code rows on either surface.
    const [lanes, code] = await Promise.all([
      Promise.all(HOME_ACTIVITY_SPECS.map(async (spec) => ({ spec, r: await resolveExtItems(ctx, spec.ext, HOME_ACTIVITY_NEED) }))),
      resolveCodeItemsIndexed(ctx, HOME_ACTIVITY_NEED),
    ]);
    for (const { spec, r } of lanes) {
      for (const it of r.items) {
        const type = (it.header && it.header.type) || spec.type;
        if (!homeActivityRoot(spec.ext, type)) continue;
        it._ext = spec.ext; it._branch = spec.branch; it._type = type;
        merged.push(it);
      }
    }
    for (const it of (code ? code.items : [])) { it._ext = "code"; it._type = "commit"; merged.push(it); }
    merged.sort((a, b) => {
      if (a.effectiveTime !== b.effectiveTime) return b.effectiveTime - a.effectiveTime;
      return a.commit.hash < b.commit.hash ? 1 : (a.commit.hash > b.commit.hash ? -1 : 0);
    });
    return merged.slice(0, HOME_ACTIVITY_LIMIT);
  }

  // embeddedRefs returns the cross-repo context an item embeds for its own original and reply-to references.
  function embeddedRefs(commit, header) {
    const out = [];
    const refs = (commit && commit.refs) || [];
    for (const key of ["original", "reply-to"]) {
      const val = header[key];
      if (!val) continue;
      const url = refRepoUrl(val);
      if (!url) continue;
      const want = refHash(val);
      const match = refs.find((r) => r.ref === val || (want && hashEq(refHash(r.ref), want)));
      out.push({ key, url, ref: val, author: (match && match.author) || "", email: (match && match.email) || "", time: (match && match.time) || "", type: (match && match.type) || "", quoted: (match && match.quoted) || "" });
    }
    return out;
  }

  // parentQuote returns the excerpt a reply carries of the thing it answers, from its own GitMsg-Ref trailer (GITMSG 1.3).
  function parentQuote(item) {
    const h = (item && item.header) || {};
    const refs = (item && item.commit && item.commit.refs) || [];
    for (const key of ["reply-to", "original"]) {
      const val = h[key];
      if (!val) continue;
      const want = refHash(val);
      const match = refs.find((r) => r.ref === val || (want && hashEq(refHash(r.ref), want)));
      if (match && match.quoted) {
        return { key, ref: val, quoted: match.quoted, author: match.author || "", email: match.email || "", time: match.time || "", type: match.type || "" };
      }
    }
    return null;
  }

  // ANCESTOR_CAP bounds the same-repo parent chain a commit permalink resolves and renders.
  const ANCESTOR_CAP = 5;

  // refBranch pulls the branch out of a gitmsg commit ref value, or "" when it carries none.
  function refBranch(ref) {
    const s = ref || "";
    const at = s.indexOf("@");
    return at < 0 ? "" : s.slice(at + 1);
  }

  // parentRef returns the same-repo ref an item names as its parent, reply-to over original (GITSOCIAL 1.3); null for a cross-repo ref.
  function parentRef(header) {
    for (const key of ["reply-to", "original"]) {
      const val = header && header[key];
      if (val && !refRepoUrl(val)) return val;
    }
    return null;
  }

  // quotedRefFor returns the commit's own GitMsg-Ref entry matching one reference value.
  function quotedRefFor(commit, ref) {
    const want = refHash(ref);
    const refs = (commit && commit.refs) || [];
    return refs.find((r) => r.ref === ref || (want && hashEq(refHash(r.ref), want))) || null;
  }

  // findRefItem resolves a same-repo commit ref to its item and data branch; the ref's branch is a hint, so resolution is hash-driven.
  async function findRefItem(ctx, ref, branch) {
    const want = refHash(ref);
    if (!want) return null;
    const hint = refBranch(ref) || branch;
    const order = COMMIT_VIEW[hint] ? [hint] : [];
    for (const br of Object.keys(COMMIT_VIEW)) if (br !== order[0]) order.push(br);
    for (const br of order) {
      const items = await loadExtItemsUpTo(ctx, COMMIT_VIEW[br].ext, DETAIL_WALK_CAP);
      const it = items.find((i) => hashEq(i.commit.short, want));
      if (it) return { item: it, branch: br };
    }
    return null;
  }

  // resolveAncestors resolves an item's same-repo parent chain root-first up to ANCESTOR_CAP, reporting the first unresolvable ref.
  async function resolveAncestors(ctx, item, branch) {
    const chain = [];
    let missing = null;
    const seen = new Set([item.commit.short]);
    let ref = parentRef(item.header);
    while (ref && chain.length < ANCESTOR_CAP) {
      const found = await findRefItem(ctx, ref, branch);
      if (!found) { missing = ref; break; }
      if (seen.has(found.item.commit.short)) break;
      seen.add(found.item.commit.short);
      chain.push(found);
      ref = parentRef(found.item.header);
    }
    chain.reverse();
    return { chain, missing };
  }

  // groupPM splits pm items by type and buckets issues under the milestone or sprint they reference, per GITPM 1.3.
  function groupPM(items) {
    const milestones = [], sprints = [], issues = [];
    for (const it of items) {
      const t = (it.header && it.header.type) || "issue";
      if (t === "milestone") milestones.push(it);
      else if (t === "sprint") sprints.push(it);
      else issues.push(it);
    }
    const bucket = (field) => {
      const map = new Map();
      for (const it of issues) {
        const h = refHash(it.header[field]);
        if (!h) continue;
        if (!map.has(h)) map.set(h, []);
        map.get(h).push(it);
      }
      return map;
    };
    return { milestones, sprints, issues, byMilestone: bucket("milestone"), bySprint: bucket("sprint") };
  }

  // itemLabels parses an item's labels header into { scope, value } entries, per GITPM 1.2.
  function itemLabels(header) {
    return ((header && header.labels) || "").split(",").map((s) => s.trim()).filter(Boolean).map((l) => {
      const i = l.indexOf("/");
      return i < 0 ? { scope: "", value: l } : { scope: l.slice(0, i), value: l.slice(i + 1) };
    });
  }

  // PM_BOARD_COLUMNS mirrors the shipped kanban framework board, the default a static reader falls back to.
  const PM_BOARD_COLUMNS = [
    { name: "Backlog", filter: "state:open", wip: 0 },
    { name: "In Progress", filter: "status:in-progress", wip: 3 },
    { name: "Review", filter: "status:review", wip: 3 },
    { name: "Done", filter: "state:closed", wip: 0 },
  ];

  // matchColumnFilter tests an issue header against a board filter expression, mirroring board.go matchFilter.
  function matchColumnFilter(header, filter) {
    for (const part of filter.split(",")) {
      const p = part.trim();
      const idx = p.indexOf(":");
      if (idx < 0) continue;
      const key = p.slice(0, idx), value = p.slice(idx + 1);
      if (key === "state") { if (((header && header.state) || "open") === value) return true; continue; }
      for (const l of itemLabels(header)) if (l.scope === key && l.value === value) return true;
    }
    return false;
  }

  // matchIssueColumn returns the best-matching column index, a label filter over a state filter; -1 when nothing matches.
  function matchIssueColumn(header, filters) {
    let stateMatch = -1;
    for (let i = 0; i < filters.length; i++) {
      if (!matchColumnFilter(header, filters[i])) continue;
      if (filters[i].indexOf("state:") !== 0) return i;
      if (stateMatch < 0) stateMatch = i;
    }
    return stateMatch;
  }

  // boardColumnsFrom normalizes a resolved-board config into the column shape buildBoard groups against, else the kanban default.
  function boardColumnsFrom(config) {
    const swimlane = (config && config.defaultSwimlane) || "";
    const cols = config && Array.isArray(config.columns) ? config.columns : null;
    if (!cols || !cols.length) return { name: "Kanban Board", columns: PM_BOARD_COLUMNS, defaultSwimlane: swimlane };
    const columns = cols
      .filter((c) => c && c.name && c.filter)
      .map((c) => ({ name: c.name, filter: c.filter, wip: (typeof c.wip === "number" && c.wip > 0) ? c.wip : 0 }));
    if (!columns.length) return { name: "Kanban Board", columns: PM_BOARD_COLUMNS, defaultSwimlane: swimlane };
    return { name: config.name || "Board", columns, defaultSwimlane: swimlane };
  }

  // buildBoard groups resolved issues into board columns; an unmatched issue falls into the first, matching board.go.
  function buildBoard(issues, config) {
    const board = boardColumnsFrom(config);
    const columns = board.columns.map((c) => ({ name: c.name, filter: c.filter, wip: c.wip || 0, issues: [] }));
    const filters = board.columns.map((c) => c.filter);
    for (const it of issues) {
      let idx = matchIssueColumn(it.header || {}, filters);
      if (idx < 0 || idx >= columns.length) idx = 0;
      columns[idx].issues.push(it);
    }
    return { name: board.name, columns, defaultSwimlane: board.defaultSwimlane || "" };
  }

  // loadSiteConfig fetches the resolved PM board config once per context; null when the bucket carries none.
  async function loadSiteConfig(ctx) {
    if (ctx.siteConfig !== undefined) return ctx.siteConfig;
    let cfg = null;
    const text = await fetchText(ctx.base, ".gitsocial/site/pm-config.json");
    if (text) { try { cfg = JSON.parse(text); } catch { cfg = null; } }
    ctx.siteConfig = cfg;
    return cfg;
  }

  // loadSiteCustomization fetches the site customization once per context; null when the bucket carries none.
  async function loadSiteCustomization(ctx) {
    if (ctx.siteCustomization !== undefined) return ctx.siteCustomization;
    let cfg = null;
    const text = await fetchText(ctx.base, ".gitsocial/site/site-config.json");
    if (text) { try { const p = JSON.parse(text); if (p && typeof p === "object") cfg = p; } catch { cfg = null; } }
    ctx.siteCustomization = cfg;
    return cfg;
  }

  // SWIMLANE_FIELDS are the board group-by options, mirroring pm.SwimlaneFields.
  const SWIMLANE_FIELDS = ["", "priority", "kind", "assignees", "author"];
  // SWIMLANE_LABELS names each field for the group-by control (none for "").
  const SWIMLANE_LABELS = { "": "none", priority: "priority", kind: "kind", assignees: "assignees", author: "author" };
  // SWIMLANE_ORDER holds the predefined lane orders for priority and kind, mirroring view_board.go getSwimlaneOrder.
  const SWIMLANE_ORDER = {
    priority: ["critical", "high", "medium", "low", ""],
    kind: ["bug", "feature", "task", "story", "spike", "chore", ""],
  };

  // swimlaneValue extracts an issue's lane value for a group-by field, mirroring view_board.go getSwimlaneValue.
  function swimlaneValue(item, field) {
    const h = item.header || {};
    if (field === "assignees") {
      const a = (h.assignees || "").split(",").map((s) => s.trim()).filter(Boolean);
      return a.length ? a[0] : "";
    }
    if (field === "author") return item.author || (item.commit && (item.commit.authorName || item.commit.authorEmail)) || "";
    if (field === "priority" || field === "kind") {
      for (const l of itemLabels(h)) if (l.scope === field) return l.value;
      return "";
    }
    return "";
  }

  // swimlaneOrder returns the ordered lane values for a field over an issue set, the ungrouped lane last.
  function swimlaneOrder(issues, field) {
    if (!field) return [];
    const present = new Set();
    for (const it of issues) present.add(swimlaneValue(it, field));
    if (SWIMLANE_ORDER[field]) {
      const out = SWIMLANE_ORDER[field].filter((v) => present.has(v));
      // A present value outside the predefined order is appended alphabetically, before the ungrouped lane.
      const extra = Array.from(present).filter((v) => v && SWIMLANE_ORDER[field].indexOf(v) === -1).sort();
      const hasBlank = out.indexOf("") !== -1;
      const base = out.filter((v) => v !== "").concat(extra);
      return hasBlank ? base.concat([""]) : base;
    }
    const vals = Array.from(present).filter((v) => v).sort();
    if (present.has("")) vals.push("");
    return vals;
  }

  // groupBySwimlane buckets a column's issues by lane value, empty lanes included so every column aligns.
  function groupBySwimlane(issues, field, lanes) {
    const map = new Map();
    for (const lane of lanes) map.set(lane, []);
    for (const it of issues) {
      const v = swimlaneValue(it, field);
      if (!map.has(v)) map.set(v, []);
      map.get(v).push(it);
    }
    return map;
  }

  // swimlaneLabel names a lane for display; the ungrouped lane reads "(none)".
  function swimlaneLabel(value) { return value === "" ? "(none)" : value; }

  // pmParentHash returns an issue's immediate-parent short hash per GITPM 1.7; null for a top-level issue.
  function pmParentHash(header) {
    return refHash((header && header.parent) || "") || refHash((header && header.root) || "") || null;
  }

  // buildIssueHierarchy indexes parent and child relationships over a resolved issue set, per GITPM 1.7.
  function buildIssueHierarchy(issues) {
    const byShort = new Map();
    for (const it of issues) byShort.set(it.commit.short, it);
    const childrenOf = new Map();
    for (const it of issues) {
      const p = pmParentHash(it.header);
      if (!p) continue;
      let key = null;
      for (const s of byShort.keys()) if (hashEq(s, p)) { key = s; break; }
      if (!key || key === it.commit.short) continue;
      if (!childrenOf.has(key)) childrenOf.set(key, []);
      childrenOf.get(key).push(it);
    }
    for (const arr of childrenOf.values()) arr.sort((a, b) => a.effectiveTime - b.effectiveTime);
    return { byShort, childrenOf };
  }

  // pmProgress counts closed against total over an item set.
  function pmProgress(items) {
    let closed = 0;
    for (const it of items) if (((it.header && it.header.state) || "open") === "closed") closed++;
    return { closed, total: items.length };
  }

  // loadInteractionCounts builds the cross-branch comment, repost, quote and review tallies keyed by a target's short hash; cached on ctx.
  async function loadInteractionCounts(ctx) {
    if (ctx.interactionCounts) return ctx.interactionCounts;
    const counts = new Map();
    const bump = (short, key) => {
      if (!short) return;
      let r = counts.get(short);
      if (!r) { r = { comments: 0, reposts: 0, quotes: 0, approved: 0, changesRequested: 0 }; counts.set(short, r); }
      r[key]++;
    };
    const [social, review] = await Promise.all([
      loadExtItemsForCounts(ctx, "social").catch(() => []),
      loadExtItemsForCounts(ctx, "review").catch(() => []),
    ]);
    for (const it of social) {
      const h = it.header || {};
      const t = it.header && it.header.type;
      const orig = anyRefHash(h.original);
      if (t === "repost") bump(orig, "reposts");
      else if (t === "quote") bump(orig, "quotes");
      else if (t === "comment" || h.original) bump(orig, "comments");
    }
    // Latest verdict per PR and reviewer, so a re-review does not double-count, mirroring reviewSummary.
    const verdicts = new Map();
    for (const it of review) {
      const h = it.header || {};
      if ((h.type || "") !== "feedback") continue;
      const pr = anyRefHash(h["pull-request"]) || anyRefHash(h.original);
      if (!pr) continue;
      bump(pr, "comments");
      const rs = h["review-state"];
      if (rs !== "approved" && rs !== "changes-requested") continue;
      const email = (effectiveAuthorEmail(it.commit, h) || "").toLowerCase();
      const t = it.effectiveTime || (it.commit && it.commit.authorTime) || 0;
      const key = pr + "\x00" + email;
      const prev = verdicts.get(key);
      if (!prev || t >= prev.time) verdicts.set(key, { pr, state: rs, time: t });
    }
    for (const v of verdicts.values()) {
      const r = counts.get(v.pr) || (counts.set(v.pr, { comments: 0, reposts: 0, quotes: 0, approved: 0, changesRequested: 0 }), counts.get(v.pr));
      if (v.state === "approved") r.approved++; else r.changesRequested++;
    }
    ctx.interactionCounts = counts;
    return counts;
  }

  // countsFor returns one item's counts from a loaded map, or null.
  function countsFor(counts, short) {
    return (counts && counts.get(short)) || null;
  }

  // ---- In-bucket item search (tier i: over already-walked items) ----

  // SEARCH_GROUPS orders and labels the search groups; the code group searches plain commits at subject level.
  const SEARCH_GROUPS = [
    { ext: "pm", label: "Issues", branch: "gitmsg/pm", type: null },
    { ext: "review", label: "Pull Requests", branch: "gitmsg/review", type: "pull-request" },
    { ext: "social", label: "Posts", branch: "gitmsg/social", type: null },
    { ext: "release", label: "Releases", branch: "gitmsg/release", type: "release" },
    { ext: "memo", label: "Memos", branch: "gitmsg/memo", type: null },
    { ext: "code", label: "Commits", branch: "", type: null },
  ];
  // SEARCH_EXTS is every gitmsg extension branch the search walks; the code lane is fed from the code items index.
  const SEARCH_EXTS = ["social", "pm", "review", "release", "memo"];
  // SEARCH_HEADER_KEYS are the header fields a query is matched against, beyond subject, content and author.
  const SEARCH_HEADER_KEYS = ["labels", "tag", "version", "type", "state", "assignees"];

  // itemSubject returns an item's display subject even before hydration, falling back to the metadata-index subject.
  function itemSubject(item) {
    const content = stripLinkRefDefs(item.content || "").trim();
    if (content) { const nl = content.indexOf("\n"); return subjectText(nl < 0 ? content : content.slice(0, nl)); }
    const versions = item.versions || [];
    for (let i = versions.length - 1; i >= 0; i--) {
      const s = versions[i].commit && versions[i].commit.subject;
      if (s) return s;
    }
    return (item.commit && item.commit.subject) || "";
  }

  // searchableText builds the lowercased haystack an item is matched against.
  function searchableText(item) {
    const parts = [item.content || itemSubject(item), item.author || ""];
    const h = item.header || {};
    for (const k of SEARCH_HEADER_KEYS) if (h[k]) parts.push(h[k]);
    return parts.join("\n").toLowerCase();
  }

  // FACET_FIELDS are the pivots the in-bucket search exposes.
  const FACET_FIELDS = ["type", "state", "author", "label"];
  // EXT_DEFAULT_TYPE names the Type-facet value for an item whose header carries no type.
  const EXT_DEFAULT_TYPE = { social: "post", pm: "issue", review: "pull-request", release: "release", memo: "memo", code: "commit" };
  // TYPE_ALIASES normalizes a typed type: token to the chip vocabulary.
  const TYPE_ALIASES = { pr: "pull-request", prs: "pull-request" };

  // facetType returns an item's Type-facet token (header type or the ext default).
  function facetType(item, ext) { return (item.header && item.header.type) || EXT_DEFAULT_TYPE[ext] || ext; }

  // BODY_ONLY_TYPES render whole, with no first line promoted to a heading: the replies, and the generated content of a repost.
  const BODY_ONLY_TYPES = { comment: 1, feedback: 1, repost: 1, quote: 1 };

  // isBodyOnly reports whether an item renders whole rather than as subject plus body, mirroring site_pages_html.go sitePageBodyOnly.
  function isBodyOnly(item, ext) { return !!BODY_ONLY_TYPES[facetType(item, ext)]; }
  // facetState returns an item's State-facet value, or "" for a stateless extension.
  function facetState(item, ext) { return (ext === "pm" || ext === "review") ? ((item.header && item.header.state) || "open") : ""; }
  // itemLabelStrings returns an item's raw label strings (Label-facet values).
  function itemLabelStrings(item) { return ((item.header && item.header.labels) || "").split(",").map((s) => s.trim()).filter(Boolean); }
  // authorBlob is the lowercased name and emails an author: substring matches against, the origin email included.
  function authorBlob(item) {
    const gitEmail = (item.commit && item.commit.authorEmail) || "";
    const effEmail = effectiveAuthorEmail(item.commit, item.header) || "";
    return ((item.author || "") + " " + effEmail + " " + gitEmail).toLowerCase();
  }

  // TYPE_GLYPH maps a gitmsg item type to the TUI's compact leading glyph; issues vary by state and resolve in typeGlyph.
  const TYPE_GLYPH = { post: "•", comment: "↩", repost: "↻", quote: "↻", milestone: "◇", sprint: "◷", "pull-request": "⑂", feedback: "↩", release: "⏏", memo: "☞", commit: "◦" };

  // typeGlyph returns an item's leading type glyph, or "" when the type is unknown.
  function typeGlyph(item, ext) {
    const h = item.header || {};
    const t = h.type || EXT_DEFAULT_TYPE[h.ext || ext] || "";
    if (t === "issue") return (h.state === "closed" || h.state === "canceled" || h.state === "completed") ? "●" : "○";
    return TYPE_GLYPH[t] || "";
  }

  // COMMIT_HASH_RE recognizes a bare commit-hash token and DATE_RE the strict after: and before: date, mirroring core/search/parse.go.
  const COMMIT_HASH_RE = /^[0-9a-fA-F]{7,40}$/;
  const DATE_RE = /^\d{4}-\d{2}-\d{2}$/;

  // dateBound parses a strict YYYY-MM-DD (UTC) to unix seconds; end returns the inclusive end of day.
  function dateBound(s, end) {
    if (!DATE_RE.test(s)) return NaN;
    const ms = Date.parse(s + "T00:00:00Z");
    if (isNaN(ms)) return NaN;
    return Math.floor(ms / 1000) + (end ? 86399 : 0);
  }

  // parseSearchFilters splits a raw query into free text, typed facet selections, hash prefixes and date bounds.
  function parseSearchFilters(query) {
    const typed = { type: [], state: [], author: [], label: [] };
    const hashes = [];
    let dateFrom = null, dateTo = null;
    let terms = String(query || "").replace(/(\w+):(\S+)/g, (m, key, val) => {
      const k = key.toLowerCase(), v = val.toLowerCase();
      if (k === "type") { typed.type.push(TYPE_ALIASES[v] || v); return " "; }
      if (k === "state") { typed.state.push(v); return " "; }
      if (k === "author") { typed.author.push(v); return " "; }
      if (k === "label" || k === "labels") { typed.label.push(v); return " "; }
      if (k === "hash" || k === "commit") { if (COMMIT_HASH_RE.test(val)) { hashes.push(v); return " "; } return m; }
      if (k === "after") { const b = dateBound(val, false); if (!isNaN(b)) { dateFrom = b; return " "; } return m; }
      if (k === "before") { const b = dateBound(val, true); if (!isNaN(b)) { dateTo = b; return " "; } return m; }
      return m;
    }).replace(/(^|\s)@(\S+)/g, (m, pre, val) => { typed.author.push(val.toLowerCase()); return pre; });
    // A bare hex token (7-40) is a hash prefix, not free text — pull it out.
    terms = terms.split(/\s+/).filter((tok) => {
      if (COMMIT_HASH_RE.test(tok)) { hashes.push(tok.toLowerCase()); return false; }
      return true;
    }).join(" ");
    return { terms: terms.trim().toLowerCase(), typed, hashes, dateFrom, dateTo };
  }

  // itemMatchesHash reports whether an item's commit hash matches any of the query's prefixes.
  function itemMatchesHash(item, hashes) {
    const full = ((item.commit && item.commit.hash) || "").toLowerCase();
    const short = ((item.commit && item.commit.short) || "").toLowerCase();
    return hashes.some((h) => full.startsWith(h) || short.startsWith(h));
  }

  // itemFacetValues returns an item's value or values for one facet field.
  function itemFacetValues(item, ext, field) {
    if (field === "label") return itemLabelStrings(item);
    if (field === "author") return [item.author || ""];
    return [field === "type" ? facetType(item, ext) : facetState(item, ext)];
  }

  // matchesFacet reports whether an item satisfies one field's selection; an empty selection matches all.
  function matchesFacet(item, ext, field, chip, typed) {
    if (!chip.size && !typed.length) return true;
    if (field === "author") {
      if (chip.has(item.author)) return true;
      const blob = authorBlob(item);
      return typed.some((a) => blob.indexOf(a) !== -1);
    }
    return itemFacetValues(item, ext, field).some((v) => chip.has(v) || typed.indexOf(String(v).toLowerCase()) !== -1);
  }

  // facetSelected marks a chip active when its value is in the clicked set or is picked out by a typed token.
  function facetSelected(field, value, chip, typed) {
    if (chip.has(value)) return true;
    if (field === "author") { const v = value.toLowerCase(); return typed.some((a) => v.indexOf(a) !== -1); }
    return typed.indexOf(String(value).toLowerCase()) !== -1;
  }

  // searchRelevance ranks a matched item: a hash-prefix hit first, then a subject match, then a body-only match.
  function searchRelevance(item, terms, hashes) {
    if (hashes.length && itemMatchesHash(item, hashes)) return 2;
    if (terms && itemSubject(item).toLowerCase().indexOf(terms) !== -1) return 1;
    return 0;
  }

  // searchItemsFaceted runs the in-bucket search with faceting: grouped hits, a flat recency lane, and per-field facet counts.
  function searchItemsFaceted(query, perExt, filters) {
    const f = filters || {};
    const chip = { type: f.type || new Set(), state: f.state || new Set(), author: f.author || new Set(), label: f.label || new Set() };
    const { terms, typed, hashes, dateFrom, dateTo } = parseSearchFilters(query);
    const active = !!terms || hashes.length > 0 || dateFrom !== null || dateTo !== null || FACET_FIELDS.some((k) => typed[k].length || chip[k].size);
    if (!active) return { query: "", terms: "", total: 0, groups: [], facets: {} };
    // Flatten the corpus once, pre-filtered by the terms, hash prefixes, date bounds and the group's own type guard.
    const pool = [];
    for (const spec of SEARCH_GROUPS) {
      for (const it of (perExt[spec.ext] || [])) {
        if (spec.type && ((it.header && it.header.type) || "") !== spec.type) continue;
        if (terms && searchableText(it).indexOf(terms) === -1) continue;
        if (hashes.length && !itemMatchesHash(it, hashes)) continue;
        if (dateFrom !== null || dateTo !== null) {
          const t = it.effectiveTime || (it.commit && it.commit.authorTime) || 0;
          if (dateFrom !== null && t < dateFrom) continue;
          if (dateTo !== null && t > dateTo) continue;
        }
        pool.push({ it, ext: spec.ext, spec });
      }
    }
    const passes = (row, skip) => FACET_FIELDS.every((fld) => fld === skip || matchesFacet(row.it, row.ext, fld, chip[fld], typed[fld]));
    const byExt = new Map();
    const flat = [];
    let total = 0;
    for (const row of pool) {
      if (!passes(row, null)) continue;
      let g = byExt.get(row.spec.ext);
      if (!g) { g = { ext: row.spec.ext, label: row.spec.label, branch: row.spec.branch, count: 0, items: [] }; byExt.set(row.spec.ext, g); }
      g.items.push(row.it); g.count++; total++;
      flat.push({ item: row.it, group: g });
    }
    // The flat lane is the default presentation: relevance rank first, then recency across all groups.
    flat.sort((a, b) => (searchRelevance(b.item, terms, hashes) - searchRelevance(a.item, terms, hashes)) || (b.item.effectiveTime - a.item.effectiveTime));
    const groups = [];
    // Within a group, rank by relevance, then recency.
    for (const spec of SEARCH_GROUPS) {
      const g = byExt.get(spec.ext);
      if (!g) continue;
      g.items.sort((a, b) => (searchRelevance(b, terms, hashes) - searchRelevance(a, terms, hashes)) || (b.effectiveTime - a.effectiveTime));
      groups.push(g);
    }
    // Per-field counts over items passing terms + every OTHER active field.
    const facets = {};
    for (const fld of FACET_FIELDS) {
      const counts = new Map();
      for (const row of pool) {
        if (!passes(row, fld)) continue;
        for (const v of itemFacetValues(row.it, row.ext, fld)) { if (v) counts.set(v, (counts.get(v) || 0) + 1); }
      }
      facets[fld] = Array.from(counts, ([value, count]) => ({ value, count, selected: facetSelected(fld, value, chip[fld], typed[fld]) }))
        .sort((a, b) => b.count - a.count || (a.value < b.value ? -1 : 1));
    }
    return { query: (query || "").trim().toLowerCase(), terms, total, groups, flat, facets };
  }

  // searchItems is the facet-free entry point the DOM-free unit tests use.
  function searchItems(query, perExt) {
    const r = searchItemsFaceted(query, perExt, null);
    return { query: r.query, total: r.total, groups: r.groups };
  }

  // loadBodyIndex fetches an extension's bodies corpus once per context; the one artifact carrying message bodies, so it loads on request.
  async function loadBodyIndex(ctx, ext) {
    if (!ctx.bodyIndex) ctx.bodyIndex = {};
    if (ctx.bodyIndex[ext] !== undefined) return ctx.bodyIndex[ext];
    let idx = null;
    try {
      idx = await loadBodyIndexSharded(ctx, ext);
    } catch (e) { if (e && e.forbidden) throw e; }
    ctx.bodyIndex[ext] = idx;
    return idx;
  }

  // loadBodyIndexSharded assembles the sharded corpus newest-first; null when the manifest is absent or an older version.
  async function loadBodyIndexSharded(ctx, ext) {
    const dir = ".gitsocial/site/bodies/" + ext + "/";
    const mtext = await fetchText(ctx.base, dir + "manifest.json");
    if (!mtext) return null;
    let m;
    try { m = JSON.parse(mtext); } catch { return null; }
    if (!m || m.version !== 4 || !/^[0-9a-f]{40}$/.test(m.tip || "") || !Array.isArray(m.shards)) return null;
    const keys = m.shards.map((s) => dir + s.key).concat([dir + "head.json"]);
    const texts = await Promise.all(keys.map((k) => fetchText(ctx.base, k)));
    const items = [];
    for (const t of texts) {
      if (!t) continue;
      let doc;
      try { doc = JSON.parse(t); } catch { continue; }
      if (doc && Array.isArray(doc.items)) for (const it of doc.items) items.push(it);
    }
    items.reverse();
    return { version: 4, tip: m.tip, items };
  }

  // olderItemBytes sums the compressed size of every corpus's not-yet-resident older metadata shards, the code corpus included.
  async function olderItemBytes(ctx) {
    let total = 0;
    const idxes = await Promise.all(SEARCH_EXTS.concat("code").map((ext) => loadItemsIndex(ctx, ext)));
    for (const idx of idxes) if (idx && !idx.allResident) total += idx.olderBytes || 0;
    return total;
  }

  // buildSearchCorpus assembles the per-extension search sets: a light metadata tier by default, full message text on request.
  async function buildSearchCorpus(ctx, extend, full, searchOlder, onLane) {
    const perExt = {};
    let truncated = false, light = false, hasOlder = false, partial = false;
    // Fallback window cursor for an ext with neither bodies nor an index-backed walk.
    if (!ctx.searchNeed) ctx.searchNeed = WALK_CAP;
    else if (extend) ctx.searchNeed += WALK_CAP;
    const laneTotal = SEARCH_EXTS.length + 1;
    let laneDone = 0;
    // note reports one lane's completion; perExt grows in place, so the caller can render lanes as they resolve.
    const note = () => { laneDone++; if (onLane) { try { onLane(perExt, laneDone, laneTotal); } catch (_) { /* render-side */ } } };
    const extLane = async (ext) => {
      if (full) {
        const bodies = await loadBodyIndex(ctx, ext);
        if (bodies) { perExt[ext] = resolveItems(bodies.items.map(indexCommit)); return; }
      }
      // Light search covers resident metadata; older shards stay a pending opt-in, and an incomplete manifest covers the bootstrapped prefix alone.
      const idx = await loadItemsIndex(ctx, ext);
      if (idx && searchOlder && !idx.allResident) await loadOlderItemShards(ctx, ext);
      const w = await extWalkState(ctx, ext);
      if (w && extWalkExhausted(w)) {
        perExt[ext] = resolveItems(walkedCommits(w.state));
        if (perExt[ext].some((it) => it.commit.hollow)) light = true;
        if (w.older && w.older.length) hasOlder = true;
        if (idx && idx.complete === false) partial = true;
        return;
      }
      // Metadata-only fallback: no body hydration here, since matching runs on the metadata tier either way.
      const r = await resolveExtItems(ctx, ext, ctx.searchNeed);
      perExt[ext] = r.items;
      if (r.items.some((it) => it.commit.hollow)) light = true;
      if (r.more) truncated = true;
    };
    // Plain code commits join at subject level from the code index; the code corpus carries no bodies, so the full tier leaves them as they are.
    const codeLane = async () => {
      const cw = await codeIndexWalkState(ctx);
      if (!cw) return;
      await withWalkLock(cw, async () => {
        if (searchOlder) {
          let guard = (cw.older || []).length, stall = 0;
          while (cw.older && cw.older.length) {
            await loadNextCodeShard(ctx, cw);
            const n = (cw.older || []).length;
            if (n === guard) { if (++stall >= 2) break; } else stall = 0;
            guard = n;
          }
        }
        perExt.code = cw.items.map((c) => codeCommitItem(c, c._branch || ""));
      });
      if (perExt.code.length && !full) light = true;
      if (cw.older && cw.older.length) hasOlder = true;
      if (!cw.complete) partial = true;
    };
    // Every lane in parallel: each is an independent per-ext chain.
    await Promise.all(SEARCH_EXTS.map((ext) => extLane(ext).then(note)).concat([codeLane().then(note)]));
    ctx.searchCorpus = { perExt, truncated, light, hasOlder, partial, full: !!full, older: !!searchOlder };
    return ctx.searchCorpus;
  }

  // loadSearchWindow returns the in-bucket search corpus; fullness and older coverage are sticky, and onLane fires as each lane resolves.
  async function loadSearchWindow(ctx, extend, full, searchOlder, onLane) {
    const cached = ctx.searchCorpus;
    const wantFull = !!full || !!(cached && cached.full);
    const wantOlder = !!searchOlder || !!(cached && cached.older);
    if (cached && cached.full === wantFull && cached.older === wantOlder && !(extend && cached.truncated)) return cached;
    return buildSearchCorpus(ctx, extend, wantFull, wantOlder, onLane);
  }

  // fullSearchBytes sums the compressed size of every extension's bodies corpus, from the loaded index manifests.
  async function fullSearchBytes(ctx) {
    let total = 0;
    const idxes = await Promise.all(SEARCH_EXTS.map((ext) => loadItemsIndex(ctx, ext)));
    for (const idx of idxes) if (idx && idx.bodiesBytes) total += idx.bodiesBytes;
    return total;
  }

  // ---- Review feedback (DOM-free, testable) ----

  // feedbackLine returns a feedback's anchor { side, line }, the new-file line preferred; null without a line ref.
  function feedbackLine(header) {
    const nl = parseInt((header && header["new-line"]) || "", 10);
    const ol = parseInt((header && header["old-line"]) || "", 10);
    if (!isNaN(nl) && nl > 0) return { side: "new", line: nl };
    if (!isNaN(ol) && ol > 0) return { side: "old", line: ol };
    return null;
  }

  // feedbackAnchorKey returns the diff-line key a feedback anchors to, or null.
  function feedbackAnchorKey(header) {
    const a = feedbackLine(header);
    return a ? (a.side === "new" ? "n" : "o") + a.line : null;
  }

  // feedbackVerdict returns a feedback's review verdict, "" for a plain comment. Mirrors sitePageFeedbackVerdict in site_pages_html.go.
  function feedbackVerdict(header) {
    const state = (header && header["review-state"]) || "";
    return state === "approved" || state === "changes-requested" ? state : "";
  }

  // feedbackAnchorLabel returns a feedback's "file:line" chip label, "" when it names no file. Mirrors sitePageFeedbackAnchor in site_pages_html.go.
  function feedbackAnchorLabel(header) {
    const h = header || {};
    if (!h.file) return "";
    let line = h["new-line"], end = h["new-line-end"];
    if (!line) { line = h["old-line"]; end = h["old-line-end"]; }
    if (!line) return h.file;
    return end && end !== line ? h.file + ":" + line + "-" + end : h.file + ":" + line;
  }

  // hunkLineKeys returns the anchor keys a rendered diff line answers to; a context line answers to both.
  function hunkLineKeys(l) {
    const ks = [];
    if (l.newN) ks.push("n" + l.newN);
    if (l.oldN) ks.push("o" + l.oldN);
    return ks;
  }

  // anchorFeedback partitions a file's feedback into the keys of rendered hunk lines and an offscreen list.
  function anchorFeedback(fbList, hunks) {
    const present = new Set();
    for (const h of hunks || []) for (const l of h.lines) for (const k of hunkLineKeys(l)) present.add(k);
    const byKey = new Map();
    const offscreen = [];
    for (const fb of fbList) {
      const key = feedbackAnchorKey(fb.header);
      if (key && present.has(key)) {
        if (!byKey.has(key)) byKey.set(key, []);
        byKey.get(key).push(fb);
      } else offscreen.push(fb);
    }
    return { byKey, offscreen };
  }

  // prFeedback selects the feedback referencing a PR, splitting the file-anchored from the rest.
  function prFeedback(reviewItems, prShort) {
    const all = reviewItems.filter((i) => i.header && i.header.type === "feedback" && hashEq(anyRefHash(i.header["pull-request"]), prShort));
    const file = all.filter((i) => i.header.file);
    const nonFile = all.filter((i) => !i.header.file);
    return { all, file, nonFile };
  }

  // reviewSummary aggregates a PR's feedback into review state, mirroring review.ComputeReviewSummary.
  function reviewSummary(feedbackItems, reviewers) {
    const latestVerdict = new Map();
    const acted = new Map();
    for (const it of feedbackItems) {
      const email = (effectiveAuthorEmail(it.commit, it.header) || "").toLowerCase();
      const name = effectiveAuthor(it.commit, it.header);
      const t = it.effectiveTime || (it.commit && it.commit.authorTime) || 0;
      const a = acted.get(email);
      if (!a || t >= a.time) acted.set(email, { name, time: t });
      const rs = it.header && it.header["review-state"];
      if (rs !== "approved" && rs !== "changes-requested") continue;
      const prev = latestVerdict.get(email);
      if (!prev || t >= prev.time) latestVerdict.set(email, { state: rs, time: t, name });
    }
    let approved = 0, changesRequested = 0;
    for (const v of latestVerdict.values()) { if (v.state === "approved") approved++; else changesRequested++; }
    const declared = (typeof reviewers === "string" ? reviewers.split(",") : (reviewers || [])).map((s) => (s || "").trim().toLowerCase()).filter(Boolean);
    let pending = 0;
    for (const em of declared) if (!latestVerdict.has(em)) pending++;
    const chips = [];
    for (const [email, a] of acted) {
      const v = latestVerdict.get(email);
      chips.push({ email, name: a.name, state: v ? v.state : "commented", time: v ? v.time : a.time });
    }
    chips.sort((x, y) => x.time - y.time);
    return { approved, changesRequested, pending, isBlocked: changesRequested > 0, isApproved: approved > 0 && changesRequested === 0 && pending === 0, reviewers: chips };
  }

  // suggestionBody extracts a suggestion feedback's replacement text, per GITREVIEW 1.4.
  function suggestionBody(content) {
    const m = /```suggestion[^\n]*\n([\s\S]*?)```/.exec(content || "");
    return m ? m[1].replace(/\n$/, "") : (content || "").trim();
  }

  // authorStats aggregates commit counts by author name over an already-walked commit set.
  function authorStats(commits) {
    const map = new Map();
    for (const c of commits) { const name = c.authorName || c.authorEmail || "unknown"; map.set(name, (map.get(name) || 0) + 1); }
    const authors = Array.from(map, ([name, count]) => ({ name, count })).sort((a, b) => b.count - a.count || (a.name < b.name ? -1 : 1));
    return { authors, total: commits.length };
  }

  // isBinary detects binary content: a NUL byte within the first 8000 bytes.
  function isBinary(bytes) {
    const n = Math.min(bytes.length, 8000);
    for (let i = 0; i < n; i++) if (bytes[i] === 0) return true;
    return false;
  }

  // LFS_POINTER_PREFIX is a Git LFS pointer's first line; mirrors lfsPointerPrefix in core/git/lfs.go.
  const LFS_POINTER_PREFIX = "version https://git-lfs.github.com/spec/v1\n";

  // isLFSPointer reports whether a blob opens with the Git LFS pointer line. Mirrors IsLFSPointer in core/git/lfs.go.
  function isLFSPointer(bytes) {
    if (!bytes || bytes.length < LFS_POINTER_PREFIX.length) return false;
    for (let i = 0; i < LFS_POINTER_PREFIX.length; i++) if (bytes[i] !== LFS_POINTER_PREFIX.charCodeAt(i)) return false;
    return true;
  }

  // ---- Lists (per-element refs; discovery/parsing is DOM-free, testable) ----

  // List refs (gitmsg/list.go): metadata at <name>/_meta, one member per ref under <name>/items/<hash>.
  const LIST_META_SUFFIX = "/_meta";
  const LIST_ITEMS_SEG = "/items/";

  // parseListRef splits a lists-namespace ref name into { ext, name, kind, hash }; null outside the namespace.
  function parseListRef(ref) {
    const m = /^refs\/gitmsg\/([^/]+)\/lists\/(.+)$/.exec(ref || "");
    if (!m) return null;
    const ext = m[1], rest = m[2];
    if (rest.endsWith(LIST_META_SUFFIX)) return { ext, name: rest.slice(0, -LIST_META_SUFFIX.length), kind: "meta", hash: "" };
    const i = rest.indexOf(LIST_ITEMS_SEG);
    if (i >= 0) return { ext, name: rest.slice(0, i), kind: "item", hash: rest.slice(i + LIST_ITEMS_SEG.length) };
    if (rest.indexOf("/") < 0) return { ext, name: rest, kind: "meta", hash: "" };
    return null;
  }

  // enumerateLists groups a manifest's list refs into per-list descriptors, sorted by ext then name.
  function enumerateLists(manifest) {
    if (!manifest) return [];
    const byKey = new Map();
    for (const ref of Object.keys(manifest)) {
      const p = parseListRef(ref);
      if (!p) continue;
      const key = p.ext + "\x00" + p.name;
      if (!byKey.has(key)) byKey.set(key, { ext: p.ext, name: p.name, id: p.ext + "/" + p.name, metaRef: "", itemRefs: [] });
      const rec = byKey.get(key);
      if (p.kind === "meta") rec.metaRef = ref;
      else rec.itemRefs.push(ref);
    }
    const out = Array.from(byKey.values());
    out.sort((a, b) => (a.ext < b.ext ? -1 : a.ext > b.ext ? 1 : a.name.localeCompare(b.name)));
    return out;
  }

  // listMemberRef classifies a member ref string as local to this bucket or foreign.
  function listMemberRef(memberRef) {
    const url = refRepoUrl(memberRef);
    return { repoUrl: url, ref: memberRef || "", local: !url };
  }

  // jsonCommitMessage JSON-parses a commit message that is pure JSON, returning the object or null.
  function jsonCommitMessage(msg) {
    try { const v = JSON.parse((msg || "").trim()); return (v && typeof v === "object") ? v : null; } catch { return null; }
  }

  // loadListMeta resolves a list's _meta commit into its metadata object; {} when the ref is absent.
  async function loadListMeta(ctx, list) {
    if (!list.metaRef) return {};
    const sha = await refTip(ctx, list.metaRef);
    if (!sha) return {};
    const obj = await getStateObject(ctx, sha);
    if (!obj || obj.type !== "commit") return {};
    return jsonCommitMessage(parseCommit(sha, obj.body).content) || {};
  }

  // loadListMembers resolves a list's item refs into member ref strings, sorted.
  async function loadListMembers(ctx, list) {
    const out = [];
    for (const ref of list.itemRefs) {
      const sha = await refTip(ctx, ref);
      if (!sha) continue;
      const obj = await getStateObject(ctx, sha);
      if (!obj || obj.type !== "commit") continue;
      const member = parseCommit(sha, obj.body).content.trim();
      if (member) out.push(member);
    }
    out.sort();
    return out;
  }

  // loadListsSummary returns every list in the bucket with its metadata and member count.
  async function loadListsSummary(ctx) {
    const lists = enumerateLists(await manifestFor(ctx));
    const out = [];
    for (const l of lists) out.push({ ext: l.ext, name: l.name, id: l.id, meta: await loadListMeta(ctx, l), count: l.itemRefs.length });
    return out;
  }

  // loadListDetail resolves one list by id with its resolved members; null when no such list exists.
  async function loadListDetail(ctx, id) {
    const l = enumerateLists(await manifestFor(ctx)).find((x) => x.id === id);
    if (!l) return null;
    return { ext: l.ext, name: l.name, id: l.id, meta: await loadListMeta(ctx, l), members: await loadListMembers(ctx, l) };
  }

  // loadExtConfig resolves an extension's config ref into its JSON object; null when the ref is absent or unparseable.
  async function loadExtConfig(ctx, ext) {
    const sha = await refTip(ctx, "refs/gitmsg/" + ext + "/config");
    if (!sha) return null;
    const obj = await getStateObject(ctx, sha);
    if (!obj || obj.type !== "commit") return null;
    return jsonCommitMessage(parseCommit(sha, obj.body).content);
  }

  // ---- Forks (refs/gitmsg/core/forks/<urlHash>, per-element refs) ----

  // FORKS_PREFIX is the ref namespace one ref per registered fork lives under; the ref name hashes the URL, which the commit message carries.
  const FORKS_PREFIX = "refs/gitmsg/core/forks/";

  // forkRefNames returns the manifest's fork ref names, sorted; the browser's only fork-ref discovery.
  function forkRefNames(manifest) {
    if (!manifest) return [];
    return Object.keys(manifest)
      .filter((r) => r.startsWith(FORKS_PREFIX) && /^[0-9a-f]{40}$/.test(manifest[r] || ""))
      .sort();
  }

  // loadForks resolves registered forks from the manifest, reading at most limit fork commits; a partial result carries a failed count, and a 403 rejects whole.
  async function loadForks(ctx, limit) {
    const manifest = await manifestFor(ctx);
    const refs = forkRefNames(manifest);
    const chosen = limit == null ? refs : refs.slice(0, limit);
    const out = [];
    let i = 0, failed = 0;
    const worker = async () => {
      while (i < chosen.length) {
        const sha = manifest[chosen[i++]];
        let obj = null;
        try { obj = await getStateObject(ctx, sha); } catch (e) { if (e && e.forbidden) throw e; failed++; continue; }
        if (!obj || obj.type !== "commit") continue;
        const c = parseCommit(sha, obj.body);
        const url = c.content.trim();
        if (url) out.push({ url, time: c.authorTime || 0 });
      }
    };
    await Promise.all(Array.from({ length: Math.min(HYDRATE_CONCURRENCY, chosen.length) }, worker));
    out.sort((a, b) => (b.time - a.time) || a.url.localeCompare(b.url));
    out.failed = failed;
    return out;
  }

  // ---- Analytics aggregation (DOM-free, testable) ----

  // commitsByMonth buckets a walked commit set into contiguous calendar months, with the peak count.
  function commitsByMonth(commits) {
    const map = new Map();
    let lo = Infinity, hi = -Infinity;
    for (const c of commits) {
      const t = c && c.authorTime;
      if (!t) continue;
      const d = new Date(t * 1000);
      const ym = d.getUTCFullYear() * 12 + d.getUTCMonth();
      map.set(ym, (map.get(ym) || 0) + 1);
      if (ym < lo) lo = ym;
      if (ym > hi) hi = ym;
    }
    if (!isFinite(lo)) return { buckets: [], max: 0 };
    const buckets = [];
    let max = 0;
    for (let ym = lo; ym <= hi; ym++) {
      const count = map.get(ym) || 0;
      const y = Math.floor(ym / 12), mo = (ym % 12) + 1;
      buckets.push({ month: y + "-" + String(mo).padStart(2, "0"), count });
      if (count > max) max = count;
    }
    return { buckets, max };
  }

  // extensionStats reduces every extension's resolved items into the per-extension counts the analytics page shows.
  function extensionStats(perExt) {
    const pm = groupPM(perExt.pm || []);
    const issue = pmProgress(pm.issues);
    const prs = (perExt.review || []).filter((i) => (i.header && i.header.type) === "pull-request");
    const prState = { open: 0, merged: 0, closed: 0 };
    for (const p of prs) {
      const s = (p.header && p.header.state) || "open";
      if (s === "merged") prState.merged++;
      else if (s === "closed") prState.closed++;
      else prState.open++;
    }
    const releases = (perExt.release || []).filter((i) => (i.header && i.header.type) === "release");
    return {
      issues: { open: issue.total - issue.closed, closed: issue.closed, total: issue.total },
      milestones: pm.milestones.length, sprints: pm.sprints.length,
      prs: { open: prState.open, merged: prState.merged, closed: prState.closed, total: prs.length },
      releases: releases.length,
      posts: (perExt.social || []).length,
      memos: (perExt.memo || []).length,
    };
  }

  // latestReleaseVersion returns the newest release's version/tag label, or "".
  function latestReleaseVersion(releaseItems) {
    const rels = (releaseItems || []).filter((i) => (i.header && i.header.type) === "release");
    if (!rels.length) return "";
    const top = rels.slice().sort((a, b) => b.effectiveTime - a.effectiveTime)[0];
    const h = top.header || {};
    return h.version ? ("v" + h.version) : (h.tag || "");
  }

  // ANALYTICS_SPECS maps each data branch to its analytics series and counted item type; ANALYTICS_KINDS is the ordered series list.
  const ANALYTICS_SPECS = [
    { ext: "social", kind: "posts", type: "" },
    { ext: "pm", kind: "issues", type: "issue" },
    { ext: "review", kind: "prs", type: "pull-request" },
    { ext: "release", kind: "releases", type: "release" },
    { ext: "memo", kind: "memos", type: "" },
  ];
  const ANALYTICS_KINDS = ANALYTICS_SPECS.map((s) => s.kind);

  // COUNTS_WALK_CAP bounds the loose-object walk one extension may do without a metadata index, so a view degrades to recent items instead of stalling.
  const COUNTS_WALK_CAP = WALK_CAP;

  // loadExtItemsAll returns an extension's resolved items un-hydrated; looseCap bounds the walk when no index backs it.
  async function loadExtItemsAll(ctx, ext, looseCap) {
    const cap = looseCap || COUNTS_WALK_CAP;
    const w = await extWalkState(ctx, ext);
    if (!w) return [];
    return withWalkLock(w, async () => {
      if (w.state.commits.length === 0) await stepExtWalk(ctx, w, WALK_CAP);
      // Progress guard, plus the loose-walk cap: an index-backed state drains its shards regardless of the cap.
      let guardN = w.state.commits.length, guardShards = (w.older || []).length, stall = 0;
      while (!extWalkExhausted(w)) {
        if (!(w.older && w.older.length) && w.state.visited.size >= cap) break;
        await stepExtWalk(ctx, w, WALK_CAP);
        const n = w.state.commits.length, s = (w.older || []).length;
        if (n === guardN && s === guardShards) { if (++stall >= 2) break; } else stall = 0;
        guardN = n; guardShards = s;
      }
      return resolveItems(walkedCommits(w.state));
    });
  }

  // loadExtItemsForCounts returns an extension's resolved items for the interaction tallies, at the default loose bound.
  async function loadExtItemsForCounts(ctx, ext) {
    return loadExtItemsAll(ctx, ext);
  }

  // loadSiteStats fetches the push-computed stats blob once per context; null when the bucket carries none.
  async function loadSiteStats(ctx) {
    if (ctx.siteStats !== undefined) return ctx.siteStats;
    let stats = null;
    const text = await fetchText(ctx.base, ".gitsocial/site/stats.json");
    if (text) { try { stats = JSON.parse(text); } catch { stats = null; } }
    ctx.siteStats = stats;
    return stats;
  }

  // loadAnalyticsData reduces every extension's item set to flat { kind, time, author, email } entries, with per-kind totals and a partial flag.
  async function loadAnalyticsData(ctx) {
    const entries = [];
    const perKind = {};
    let latestRelease = "";
    let partial = false;
    // Per-extension lanes in parallel; the reduce below keeps spec order.
    const lanes = await Promise.all(ANALYTICS_SPECS.map(async (spec) => {
      const items = await loadExtItemsAll(ctx, spec.ext);
      return { spec, items, complete: await extSetComplete(ctx, spec.ext) };
    }));
    for (const { spec, items, complete } of lanes) {
      if (!complete) partial = true;
      if (spec.ext === "release") latestRelease = latestReleaseVersion(items);
      let count = 0;
      for (const it of items) {
        if (spec.type && ((it.header && it.header.type) || "") !== spec.type) continue;
        const c = it.commit;
        entries.push({
          kind: spec.kind,
          time: it.effectiveTime || (c && c.authorTime) || 0,
          author: it.author || (c && (c.authorName || c.authorEmail)) || "unknown",
          email: effectiveAuthorEmail(c, it.header) || "",
        });
        count++;
      }
      perKind[spec.kind] = count;
    }
    return { entries, perKind, kinds: ANALYTICS_KINDS.slice(), total: entries.length, latestRelease, partial };
  }

  // activityBuckets buckets analytics entries into contiguous periods, each with a per-kind count map and the peak total; weekly buckets start Monday.
  function activityBuckets(entries, gran, kinds) {
    const pad = (n) => String(n).padStart(2, "0");
    const step = gran === "weekly" ? 7 : 1;
    const idxOf = (time) => {
      if (gran === "weekly") { const day = Math.floor(time / 86400); return day - (((day % 7) + 3) % 7); }
      const d = new Date(time * 1000);
      if (gran === "yearly") return d.getUTCFullYear();
      return d.getUTCFullYear() * 12 + d.getUTCMonth();
    };
    const labelOf = (idx) => {
      if (gran === "weekly") { const d = new Date(idx * 86400000); return d.getUTCFullYear() + "-" + pad(d.getUTCMonth() + 1) + "-" + pad(d.getUTCDate()); }
      if (gran === "yearly") return String(idx);
      return Math.floor(idx / 12) + "-" + pad((idx % 12) + 1);
    };
    // shortOf is the compact under-bar axis label; weekly collapses the ISO date to "Mon D".
    const MONTHS = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];
    const shortOf = (idx) => {
      if (gran !== "weekly") return labelOf(idx);
      const d = new Date(idx * 86400000);
      return MONTHS[d.getUTCMonth()] + " " + d.getUTCDate();
    };
    const zero = () => { const z = {}; for (const k of kinds) z[k] = 0; return z; };
    const map = new Map();
    let lo = Infinity, hi = -Infinity;
    for (const e of entries) {
      if (!e.time) continue;
      const idx = idxOf(e.time);
      if (!map.has(idx)) map.set(idx, zero());
      const counts = map.get(idx);
      if (counts[e.kind] === undefined) counts[e.kind] = 0;
      counts[e.kind]++;
      if (idx < lo) lo = idx;
      if (idx > hi) hi = idx;
    }
    if (!isFinite(lo)) return { buckets: [], max: 0 };
    const buckets = [];
    let max = 0;
    for (let idx = lo; idx <= hi; idx += step) {
      const counts = map.get(idx) || zero();
      let total = 0;
      for (const k of kinds) total += counts[k] || 0;
      buckets.push({ label: labelOf(idx), short: shortOf(idx), counts, total });
      if (total > max) max = total;
    }
    return { buckets, max };
  }

  // topItemAuthors ranks analytics entries by item count, keyed by email so an author's items merge across name spellings.
  function topItemAuthors(entries, limit) {
    const map = new Map();
    for (const e of entries) {
      const key = (e.email || e.author || "unknown").toLowerCase();
      let r = map.get(key);
      if (!r) { r = { name: e.author || e.email || "unknown", email: e.email || "", count: 0 }; map.set(key, r); }
      r.count++;
      if (!r.email && e.email) r.email = e.email;
    }
    const authors = Array.from(map.values()).sort((a, b) => b.count - a.count || (a.name < b.name ? -1 : a.name > b.name ? 1 : 0));
    return limit ? authors.slice(0, limit) : authors;
  }


  // ---- File-type icons (vendored @pierre/vscode-icons via icons.js) ----

  // Exact-filename → icon key overrides (checked before the extension map).
  const ICON_FILENAMES = {
    dockerfile: "docker", "docker-compose.yml": "docker", "docker-compose.yaml": "docker",
    makefile: "gear", gnumakefile: "gear",
    ".gitignore": "git", ".gitattributes": "git", ".gitmodules": "git", ".gitkeep": "git",
    "package.json": "npm", "package-lock.json": "npm", ".npmrc": "npm",
    "claude.md": "claude", license: "text", "license.md": "text",
    "go.mod": "go", "go.sum": "go",
  };
  // File-extension → icon key. Keys must exist in icons.js (window.GSIcons).
  const ICON_EXT = {
    go: "go", js: "js", mjs: "js", cjs: "js", jsx: "js",
    ts: "ts", mts: "ts", cts: "ts", tsx: "ts",
    json: "json", jsonc: "json", json5: "json",
    yaml: "yaml", yml: "yaml", toml: "gear", ini: "gear", cfg: "gear", conf: "gear", env: "gear",
    md: "md", markdown: "md", mdx: "md",
    html: "html", htm: "html", xhtml: "html",
    css: "css", scss: "css", sass: "css", less: "css",
    sh: "sh", bash: "sh", zsh: "sh", fish: "sh",
    py: "py", pyi: "py", pyw: "py",
    rs: "rust", rb: "ruby", erb: "ruby",
    c: "c", h: "c", cpp: "c", cc: "c", cxx: "c", hpp: "c", hh: "c",
    swift: "swift", kt: "code", kts: "code", java: "code", xml: "code",
    php: "code", cs: "code", lua: "code", proto: "code",
    sql: "sql", db: "sql", sqlite: "sql",
    svg: "svg",
    png: "image", jpg: "image", jpeg: "image", gif: "image", webp: "image", ico: "image", bmp: "image",
    zip: "zip", tar: "zip", gz: "zip", tgz: "zip", bz2: "zip", xz: "zip", "7z": "zip", rar: "zip", jar: "zip",
    txt: "text", log: "text", rst: "text",
    lock: "gear",
    ttf: "font", otf: "font", woff: "font", woff2: "font", eot: "font",
  };

  // iconName maps a filename, or a structural kind, to a GSIcons key, defaulting to "file".
  function iconName(name, kind) {
    if (kind === "tree") return "folder";
    if (kind === "tree-open") return "folder-open";
    if (kind === "commit") return "git";
    if (kind === "symlink") return "symlink";
    const base = (name || "").split("/").pop();
    const lower = base.toLowerCase();
    if (ICON_FILENAMES[lower]) return ICON_FILENAMES[lower];
    const dot = base.lastIndexOf(".");
    if (dot >= 0) {
      const ext = lower.slice(dot + 1);
      if (ICON_EXT[ext]) return ICON_EXT[ext];
    }
    return "file";
  }

  // ICON_COLOR maps an icon key to a hue class defined in index.html; a key with no entry inherits --muted.
  const ICON_COLOR = {
    go: "i-cyan", ts: "i-cyan",
    js: "i-yellow",
    html: "i-orange", swift: "i-orange", rust: "i-orange", claude: "i-orange",
    css: "i-indigo",
    sh: "i-green",
    py: "i-blue", c: "i-blue", docker: "i-blue",
    yaml: "i-red", ruby: "i-red", npm: "i-red",
    git: "i-vermilion",
  };

  // iconColorClass returns the hue class for a resolved icon key, or "".
  function iconColorClass(key) { return ICON_COLOR[key] || ""; }

  const core = {
    deriveBase, repoTitle, fetchBytes, fetchText, fetchRange, inflate, parseLooseObject, objectKey,
    getObject, getContentObject, getStateObject, getPackedObject, packNames, bucketIsPacked, packMapShard, packIdxOpen, packIdxLookup, packIdxFind, applyDelta, parseCommit, cleanContent, parseGitmsg, resolveRef, resolveHead,
    walkHistory, startWalk, walkStep, walkedCommits, walkStateFor, refHash, parseBranchField, resolveItems,
    buildVersions, effectiveTime, effectiveAuthor, effectiveAuthorEmail, authorLabel,
    feedbackLine, feedbackAnchorKey, feedbackVerdict, feedbackAnchorLabel, hunkLineKeys, anchorFeedback, prFeedback,
    reviewSummary, suggestionBody,
    loadExtItems, loadExtItemsWindow, loadExtItemsUpTo, findItemDeep, loadBranchLogWindow, loadBranchLogIndexed, loadCompareCommitsWindow, loadGraphWindow, orderGraphWindow, assignGraphLanes, GRAPH_WINDOW,
    loadItemsIndex, loadOlderItemShards, olderItemBytes, loadBodyIndex, extWalkState, indexCommit, metaCommit, hydrateItem, hydrateItems,
    loadTimelineItems, loadTimelineWindow, loadHomeActivity, HOME_ACTIVITY_LIMIT, resolveCodeItems, resolveShortShaFromIndex, readRefMode, newContext,
    loadCommitsPage, loadCommitsLayout, COMMITS_PAGE_SIZE,
    manifestFor, refTip, parseRoute, commitRef, compareRef, resolveCompareRef, COMMIT_VIEW, EXT_BRANCHES, WALK_CAP, DETAIL_WALK_CAP,
    parseTree, getTree, resolvePath, listBranches, listTags, compareTagsDesc, tagVersionKey, peelTag, stripSignatureBlock, headBranchName,
    parseInline, parseMarkdown, parseList, isTableSeparator, cellAlign, splitTableRow, isMarkdownPath, isMDXPath, stripMDX,
    splitLines, diffLines, buildHunks, diffTrees, commitTree, mergeBase, resolveMergeBase, fileDiff,
    intraLine, MAX_DIFF_LINES, DIFF_TREE_SCAN_CAP,
    headFor, parseRefs, refRepoUrl, releaseAssets, releaseAssetLabel, homeFilesTruncation, headSubject, releaseVersionChip, headChips, rowChips, rowHeadChips, chipStateClass, stateCounts, groupThread, flattenThread,
    THREAD_MAX_DEPTH, embeddedRefs, groupPM, authorStats, iconName, iconColorClass,
    ANCESTOR_CAP, refBranch, parentRef, parentQuote, quotedRefFor, resolveAncestors,
    CONCURRENCY, isBinary, isLFSPointer,
    itemLabels, isBodyOnly, facetType, stripLinkRefDefs, subjectText, buildBoard, boardColumnsFrom, loadSiteConfig, loadSiteCustomization, loadInteractionCounts, loadExtItemsForCounts, COUNTS_WALK_CAP, countsFor, matchIssueColumn, PM_BOARD_COLUMNS, pmParentHash,
    SWIMLANE_FIELDS, SWIMLANE_LABELS, swimlaneValue, swimlaneOrder, groupBySwimlane, swimlaneLabel,
    buildIssueHierarchy, pmProgress, searchItems, searchItemsFaceted, parseSearchFilters, itemMatchesHash, searchableText, itemSubject, typeGlyph, loadSearchWindow, fullSearchBytes,
    SEARCH_GROUPS, hashEq,
    parseListRef, enumerateLists, listMemberRef, jsonCommitMessage,
    loadListsSummary, loadListDetail, loadExtConfig,
    forkRefNames, loadForks,
    commitsByMonth, extensionStats, latestReleaseVersion,
    loadExtItemsAll, loadSiteStats, loadAnalyticsData, activityBuckets, topItemAuthors,
  };

  Object.assign(NS, core);
  if (typeof module !== "undefined" && module.exports) module.exports = NS;
})();

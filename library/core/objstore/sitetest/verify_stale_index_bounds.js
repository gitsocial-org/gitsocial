// verify_stale_index_bounds.js - fetch-count bounds under a mid-push, index-stale
// bucket. Reproduces the shape that hung the timeline on a real bucket: a large
// gitmsg/review data branch and a large code branch whose LIVE tips are in
// refs.json, but the review items index artifact is ABSENT (mid-push, or a plain
// git push), so review items resolve only by walking loose objects.
//
// Two contracts, self-contained (builds its own loose-object bucket in Node and
// serves it via the harness serve.js — no Go binary, no showcase fixture):
//   1. loadInteractionCounts must be BOUNDED on such a bucket: it must never walk
//      the whole data branch to exhaustion (the old loadExtItemsAll did), because
//      counts are a card-stat nicety, not first-paint content.
//   2. The timeline route must PAINT with a bounded number of loose-object GETs,
//      not sit on "Loading…" behind an unbounded walk. First paint must not wait
//      on the (bounded, backgrounded) interaction counts.
const fs = require("fs");
const os = require("os");
const path = require("path");
const zlib = require("zlib");
const crypto = require("crypto");
const { createServer } = require("./serve.js");

// Build the bucket BEFORE requiring the shim so the shim's boot location can
// point at it. The suite serves one bucket named "mid-push" under a temp root.
const tmpRoot = fs.mkdtempSync(path.join(os.tmpdir(), "gs-stale-"));
const bucket = path.join(tmpRoot, "mid-push");
const REVIEW_N = 1500; // gitmsg/review commits (no index) -> would be a huge loose walk
const CODE_N = 2000;   // master code commits -> the merged code-commit walk

function sha1(buf) { return crypto.createHash("sha1").update(buf).digest("hex"); }
function writeObject(type, body) {
  const store = Buffer.concat([Buffer.from(type + " " + body.length + "\0"), body]);
  const sha = sha1(store);
  const dir = path.join(bucket, "objects", sha.slice(0, 2));
  fs.mkdirSync(dir, { recursive: true });
  fs.writeFileSync(path.join(dir, sha.slice(2)), zlib.deflateSync(store));
  return sha;
}
const EMPTY_TREE = writeObject("tree", Buffer.alloc(0));
let ts = 1700000000;
function commit(parent, message) {
  ts += 60;
  const lines = ["tree " + EMPTY_TREE];
  if (parent) lines.push("parent " + parent);
  lines.push("author T <t@example.com> " + ts + " +0000");
  lines.push("committer T <t@example.com> " + ts + " +0000");
  return writeObject("commit", Buffer.from(lines.join("\n") + "\n\n" + message));
}
function chain(n, mk) { let p = null; for (let i = 0; i < n; i++) p = commit(p, mk(i)); return p; }

fs.mkdirSync(bucket, { recursive: true });
const reviewTip = chain(REVIEW_N, (i) =>
  "Review feedback " + i + "\n\nGitMsg: ext=\"review\" v=\"1\" type=\"feedback\" " +
  "pull-request=\"#commit:" + ("a".repeat(11) + (i % 10)) + "@gitmsg/review\" review-state=\"approved\"");
const masterTip = chain(CODE_N, (i) => "Fix bug " + i);
const pmTip = chain(3, () => "Issue\n\nGitMsg: ext=\"pm\" v=\"1\" type=\"issue\" state=\"open\"");

fs.writeFileSync(path.join(bucket, "HEAD"), "ref: refs/heads/master\n");
fs.mkdirSync(path.join(bucket, "refs", "heads", "gitmsg"), { recursive: true });
fs.writeFileSync(path.join(bucket, "refs", "heads", "master"), masterTip + "\n");
fs.writeFileSync(path.join(bucket, "refs", "heads", "gitmsg", "review"), reviewTip + "\n");
fs.writeFileSync(path.join(bucket, "refs", "heads", "gitmsg", "pm"), pmTip + "\n");
const site = path.join(bucket, ".gitsocial", "site");
fs.mkdirSync(site, { recursive: true });
fs.writeFileSync(path.join(bucket, ".gitsocial", "ref-mode"), "etag");
fs.writeFileSync(path.join(site, "refs.json"), JSON.stringify({
  "refs/heads/master": masterTip,
  "refs/heads/gitmsg/review": reviewTip,
  "refs/heads/gitmsg/pm": pmTip,
}));
// pm index present (mirrors a bucket whose earlier ext already pushed its index);
// review + social indexes ABSENT (404) -> the loose-walk path under test.
fs.mkdirSync(path.join(site, "items", "pm"), { recursive: true });
fs.writeFileSync(path.join(site, "items", "pm", "manifest.json"),
  JSON.stringify({ version: 4, tip: pmTip, complete: true, shards: [], bodiesBytes: 0 }));
fs.writeFileSync(path.join(site, "items", "pm", "head.json"), JSON.stringify({ items: [] }));

// Start the server and point the shim's boot location at it before loading it.
const server = createServer(tmpRoot);
const listening = new Promise((r) => server.listen(0, "127.0.0.1", r));

let pass = 0, fail = 0;
const ok = (n, c, e) => { (c ? pass++ : fail++); console.log((c ? "PASS " : "FAIL ") + n + (!c && e ? " :: " + e : "")); };

async function main() {
  await listening;
  const origin = "http://127.0.0.1:" + server.address().port;
  process.env.GS_SITE_ORIGIN = origin;
  process.env.GS_SITE_BUCKET = "mid-push";

  // Instrument fetch: count loose-object GETs.
  const realFetch = global.fetch;
  let looseGets = 0;
  const looseRe = /objects\/[0-9a-f]{2}\/[0-9a-f]{38}$/;
  global.fetch = async (url, opts) => { if (looseRe.test(String(url).split("?")[0])) looseGets++; return realFetch(url, opts); };

  require("./shim.js");
  require("../site/icons.js");
  const GS = require("../site/gs-app.js");
  const { viewNode, textOf, setHash } = global.__shim;
  const wait = (ms) => new Promise((r) => setTimeout(r, ms));
  function findClass(node, cls, out) { out = out || []; for (const c of (node && node._children) || []) { if (c && c.nodeType === 1) { if (c._cls && c._cls.has(cls)) out.push(c); findClass(c, cls, out); } } return out; }
  await wait(300); // drain auto-run init home route

  const base = origin + "/mid-push/";
  const CAP = GS.COUNTS_WALK_CAP;
  ok("COUNTS_WALK_CAP is a small bound", typeof CAP === "number" && CAP > 0 && CAP <= 400, "cap=" + CAP);

  // ---- 1. loadInteractionCounts is bounded on an index-absent data branch ----
  {
    const ctx = GS.newContext(base);
    looseGets = 0;
    const counts = await GS.loadInteractionCounts(ctx);
    ok("counts load settles (returns a Map)", counts && typeof counts.get === "function");
    // Bound: the review walk (no index) must stop near COUNTS_WALK_CAP, NOT walk
    // all REVIEW_N commits. Allow slack for the social attempt (404 -> 0) and a
    // window's overshoot, but assert it is a small fraction of the branch.
    ok("counts do NOT exhaust the " + REVIEW_N + "-commit review branch (bounded loose walk)",
      looseGets < REVIEW_N / 2, "looseGets=" + looseGets + " (branch=" + REVIEW_N + ")");
    ok("counts loose walk is within a few windows of COUNTS_WALK_CAP",
      looseGets <= CAP + GS.WALK_CAP + 20, "looseGets=" + looseGets + " cap=" + CAP);
  }

  // ---- 2. the bounded per-ext loader itself never exhausts the branch ----
  {
    const ctx = GS.newContext(base);
    looseGets = 0;
    const items = await GS.loadExtItemsForCounts(ctx, "review");
    ok("loadExtItemsForCounts returns some items from the bounded prefix", items.length > 0, "items=" + items.length);
    ok("loadExtItemsForCounts bounds the loose walk (< half the branch)",
      looseGets < REVIEW_N / 2, "looseGets=" + looseGets);
  }

  // ---- 3. the timeline route PAINTS with bounded GETs (not stuck on Loading…) ----
  {
    const ctx = GS.newContext(base);
    setHash("#/timeline");
    looseGets = 0;
    await GS.route(ctx);
    const painted = findClass(viewNode, "loading").length === 0 && findClass(viewNode, "card").length > 0;
    const getsAtPaint = looseGets;
    ok("timeline route paints cards (not stuck on Loading…)", painted, textOf(viewNode).slice(0, 60));
    ok("timeline paints the code commits from master", /Fix bug/.test(textOf(viewNode)), textOf(viewNode).slice(0, 60));
    // First paint must be bounded: the window walk (code + review prefix) only,
    // NOT the whole review branch. This is the core regression: before the fix,
    // the count load in the Promise.all walked all REVIEW_N commits before paint.
    ok("timeline first paint is BOUNDED (does not walk the whole review branch before painting)",
      getsAtPaint < REVIEW_N, "getsAtPaint=" + getsAtPaint + " reviewBranch=" + REVIEW_N);

    // Let the backgrounded counts settle; total loose work stays bounded (the
    // count walk is capped, so the whole route never touches all REVIEW_N).
    await wait(2000);
    ok("total loose work stays bounded after counts settle (never a full-branch walk)",
      looseGets < REVIEW_N, "totalLooseGets=" + looseGets + " reviewBranch=" + REVIEW_N);
  }

  server.close();
  try { fs.rmSync(tmpRoot, { recursive: true, force: true }); } catch (_) { /* best effort */ }
  console.log("\n" + pass + " passed, " + fail + " failed");
  process.exit(fail ? 1 : 0);
}
main().catch((e) => { console.error("FAIL", e); server.close(); process.exit(1); });

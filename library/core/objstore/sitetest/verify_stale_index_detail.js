// verify_stale_index_detail.js - first-paint bounds for the item-DETAIL routes
// under a mid-push / index-stale bucket. Companion to verify_stale_index_bounds
// (which covers the timeline): the same bucket state — large data + code branches
// whose LIVE tips are in refs.json, but an ABSENT or STALE items index — must not
// leave a detail permalink stuck on "Loading…" behind an unbounded loose walk.
//
// The class of bug: a merged PR's "Files changed" diff reconstructs the range by
// walking the base code branch for the merge-base / merge-head short shas (up to
// DETAIL_WALK_CAP). On a big branch that is a large sequential loose walk, and it
// used to be AWAITED before the detail rendered, so first paint sat behind it.
// The fix paints the base detail (subject/body/meta/header) from the ONE commit
// object, then enriches the diff / thread / review-summary in the background.
//
// Contracts (self-contained: builds its own loose-object bucket, serves it):
//   1. index-ABSENT review branch: the PR detail PAINTS (subject/body/header)
//      with a bounded number of loose GETs — it does NOT walk the whole code
//      branch before painting.
//   2. STALE-manifest review branch (manifest tip behind the live tip, never
//      bridged): same bound, same successful paint.
//   3. The background diff enrichment still lands (Files changed) after paint,
//      proving progressive enrichment, not a dropped section.
const fs = require("fs");
const os = require("os");
const path = require("path");
const zlib = require("zlib");
const crypto = require("crypto");
const { createServer } = require("./serve.js");

const tmpRoot = fs.mkdtempSync(path.join(os.tmpdir(), "gs-staledetail-"));
const CODE_N = 3000;   // base code branch: the merge-base/head sit deep in it
const REVIEW_N = 1500; // review branch (feedback + one PR near the tip)

function sha1(buf) { return crypto.createHash("sha1").update(buf).digest("hex"); }
function mkBucket(dir) {
  fs.mkdirSync(dir, { recursive: true });
  const emptyTree = writeObject(dir, "tree", Buffer.alloc(0));
  return emptyTree;
}
function writeObject(bucket, type, body) {
  const store = Buffer.concat([Buffer.from(type + " " + body.length + "\0"), body]);
  const sha = sha1(store);
  const d = path.join(bucket, "objects", sha.slice(0, 2));
  fs.mkdirSync(d, { recursive: true });
  fs.writeFileSync(path.join(d, sha.slice(2)), zlib.deflateSync(store));
  return sha;
}

// build assembles one bucket in `mode` ("absent" | "stale") and returns
// { name, prSha } so the driver can route to the PR permalink.
function build(mode) {
  const name = "detail-" + mode;
  const bucket = path.join(tmpRoot, name);
  const emptyTree = mkBucket(bucket);
  let ts = 1700000000;
  const commit = (parent, message) => {
    ts += 60;
    const lines = ["tree " + emptyTree];
    if (parent) lines.push("parent " + parent);
    lines.push("author T <t@example.com> " + ts + " +0000");
    lines.push("committer T <t@example.com> " + ts + " +0000");
    return writeObject(bucket, "commit", Buffer.from(lines.join("\n") + "\n\n" + message));
  };
  const chain = (n, mk) => { let p = null; const all = []; for (let i = 0; i < n; i++) { p = commit(p, mk(i, p)); all.push(p); } return all; };

  // Base code branch. Pick a deep merge-base / merge-head so the diff-range walk
  // is a genuinely long loose walk (the shape that hung the real bucket).
  const code = chain(CODE_N, (i) => "Fix bug " + i);
  const masterTip = code[code.length - 1];
  const mergeBaseSha = code[50];   // ~2950 commits deep from the tip
  const mergeHeadSha = code[80];

  // Review branch: feedback commits, with ONE merged PR near the tip whose head
  // branch is absent (so the diff must reconstruct from merge-base..merge-head,
  // forcing the deep base-branch walk).
  let prSha = null;
  const review = chain(REVIEW_N, (i) => {
    if (i === REVIEW_N - 4) {
      return "Add feature X\n\nGitMsg: ext=\"review\" v=\"1\" type=\"pull-request\" state=\"merged\" " +
        "base=\"#branch:master\" head=\"#branch:feature-absent\" " +
        "base-tip=\"" + masterTip.slice(0, 12) + "\" head-tip=\"" + mergeHeadSha.slice(0, 12) + "\" " +
        "merge-base=\"" + mergeBaseSha.slice(0, 12) + "\" merge-head=\"" + mergeHeadSha.slice(0, 12) + "\" " +
        "reviewers=\"r@example.com\"";
    }
    return "Review feedback " + i + "\n\nGitMsg: ext=\"review\" v=\"1\" type=\"feedback\" " +
      "pull-request=\"#commit:" + ("a".repeat(11) + (i % 10)) + "@gitmsg/review\" review-state=\"approved\"";
  });
  const reviewTip = review[review.length - 1];
  prSha = review[REVIEW_N - 4];

  fs.writeFileSync(path.join(bucket, "HEAD"), "ref: refs/heads/master\n");
  fs.mkdirSync(path.join(bucket, "refs", "heads", "gitmsg"), { recursive: true });
  fs.writeFileSync(path.join(bucket, "refs", "heads", "master"), masterTip + "\n");
  fs.writeFileSync(path.join(bucket, "refs", "heads", "gitmsg", "review"), reviewTip + "\n");
  const site = path.join(bucket, ".gitsocial", "site");
  fs.mkdirSync(site, { recursive: true });
  fs.writeFileSync(path.join(bucket, ".gitsocial", "ref-mode"), "etag");
  fs.writeFileSync(path.join(site, "refs.json"), JSON.stringify({
    "refs/heads/master": masterTip,
    "refs/heads/gitmsg/review": reviewTip,
  }));
  if (mode === "stale") {
    // Stale manifest: tip points at an OLD review sha, shards empty, complete
    // false -> bridgeToIndex from the live tip never meets it, so the walk falls
    // back to a bounded loose walk (never a whole-branch exhaustion).
    fs.mkdirSync(path.join(site, "items", "review"), { recursive: true });
    fs.writeFileSync(path.join(site, "items", "review", "manifest.json"),
      JSON.stringify({ version: 4, tip: review[0], complete: false, shards: [], bodiesBytes: 0 }));
    fs.writeFileSync(path.join(site, "items", "review", "head.json"), JSON.stringify({ items: [] }));
  }
  // social index absent in both modes (so the thread walk is a loose walk too);
  // the social branch itself is absent -> thread enrichment is a cheap no-op.
  return { name, prSha, masterTip };
}

const absent = build("absent");
const stale = build("stale");

const server = createServer(tmpRoot);
const listening = new Promise((r) => server.listen(0, "127.0.0.1", r));

let pass = 0, fail = 0;
const ok = (n, c, e) => { (c ? pass++ : fail++); console.log((c ? "PASS " : "FAIL ") + n + (!c && e ? " :: " + e : "")); };

async function main() {
  await listening;
  const origin = "http://127.0.0.1:" + server.address().port;
  process.env.GS_SITE_ORIGIN = origin;
  process.env.GS_SITE_BUCKET = absent.name;

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
  await wait(300); // drain auto init route

  async function checkBucket(b, mode) {
    const ctx = GS.newContext(origin + "/" + b.name + "/");
    setHash(GS.commitRef(b.prSha, "gitmsg/review"));
    looseGets = 0;
    // Drive the router (not itemDetail directly) so the watchdog wiring is exercised.
    const routed = GS.route(ctx);
    // First paint: the base detail must land WITHOUT the whole code-branch walk.
    // Poll briefly for the detail node (a couple of macrotasks is plenty — the
    // commit object is one loose GET).
    let painted = false, getsAtPaint = 0;
    for (let i = 0; i < 20; i++) {
      await wait(50);
      if (findClass(viewNode, "detail").length > 0) { painted = true; getsAtPaint = looseGets; break; }
    }
    ok("[" + mode + "] detail route paints (not stuck on Loading…)", painted && findClass(viewNode, "loading").length === 0, textOf(viewNode).slice(0, 60));
    // First paint costs at most the review branch's first find-window; the deep
    // merge-base..merge-head walk of the code branch (~CODE_N loose GETs) must NOT
    // run before paint. A generous few-windows bound proves the deep walk is off
    // the first-paint path without being brittle to the find-window size.
    ok("[" + mode + "] first paint is BOUNDED (does not walk the deep code branch before painting)",
      getsAtPaint < CODE_N / 2, "getsAtPaint=" + getsAtPaint + " codeBranch=" + CODE_N);
    ok("[" + mode + "] base detail shows the PR subject/body", /Add feature X/.test(textOf(viewNode)), textOf(viewNode).slice(0, 80));
    ok("[" + mode + "] base detail shows the merged state header", /merged/.test(textOf(viewNode)), textOf(viewNode).slice(0, 120));
    // Let the backgrounded diff enrichment settle: the merged-range diff (or its
    // graceful notice) appears AFTER paint. Either way the route never hangs.
    await routed;
    await wait(2500);
    ok("[" + mode + "] no watchdog / error banner after settle", findClass(viewNode, "err").length === 0, textOf(viewNode).slice(0, 120));
    ok("[" + mode + "] the Files-changed diff enriched in after paint (progressive)",
      /Files changed/.test(textOf(viewNode)), textOf(viewNode).slice(-160));
  }

  await checkBucket(absent, "index-absent");
  await checkBucket(stale, "stale-manifest");

  server.close();
  try { fs.rmSync(tmpRoot, { recursive: true, force: true }); } catch (_) { /* best effort */ }
  console.log("\n" + pass + " passed, " + fail + " failed");
  process.exit(fail ? 1 : 0);
}
main().catch((e) => { console.error("FAIL", e); server.close(); process.exit(1); });

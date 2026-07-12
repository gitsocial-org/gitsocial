// verify_stale_index_routes.js - fetch-count bounds for every EXHAUSTIVE-set route
// under an index-absent AND a stale-manifest bucket. Companion to
// verify_stale_index_bounds.js (timeline) and verify_stale_index_detail.js (item
// detail): those cover the two routes already fixed; this covers the remaining
// routes whose first paint consumed loadExtItemsAll (issues, milestones, sprints,
// board, prs) or loadAnalyticsData (analytics).
//
// The bug class: a route whose first paint awaits an exhaustive walk of a data
// branch. When that branch's items index is ABSENT (mid-push, plain git push,
// never-indexed ext) or STALE (the index tip lags the live tip by more than the
// bridge window), loadExtItemsAll degrades to one loose-object GET per commit,
// unbounded — hundreds to thousands of sequential GETs, R2 429s, watchdog hangs.
//
// Contract per route, on both fixture states:
//   1. It PAINTS (no "Loading…" left behind) with a BOUNDED number of loose GETs
//      — never the whole data branch. First paint reflects the recent bounded
//      window, not exhaustion.
// The index-PRESENT case (exact/exhaustive from cheap metadata shards) is already
// covered by the showcase suites; here we assert only the degraded-bucket bound.
const fs = require("fs");
const os = require("os");
const path = require("path");
const zlib = require("zlib");
const crypto = require("crypto");
const { createServer } = require("./serve.js");

const tmpRoot = fs.mkdtempSync(path.join(os.tmpdir(), "gs-staleroutes-"));

const PM_N = 1400;     // gitmsg/pm commits (issues/milestones/sprints/board/analytics)
const REVIEW_N = 1400; // gitmsg/review commits (prs/analytics)
const SOCIAL_N = 1400; // gitmsg/social commits (analytics + counts)
const RELEASE_N = 600; // gitmsg/release commits (analytics)
const CODE_N = 400;    // master code commits (analytics commit series is stats-only)

function sha1(buf) { return crypto.createHash("sha1").update(buf).digest("hex"); }
function mkObject(bucket, type, body) {
  const store = Buffer.concat([Buffer.from(type + " " + body.length + "\0"), body]);
  const sha = sha1(store);
  const dir = path.join(bucket, "objects", sha.slice(0, 2));
  fs.mkdirSync(dir, { recursive: true });
  fs.writeFileSync(path.join(dir, sha.slice(2)), zlib.deflateSync(store));
  return sha;
}

// buildBucket lays down one loose-object bucket with a pm/review/social/release
// data branch and a code branch, refs.json listing the LIVE tips, and — per
// `mode` — either no items index at all ("absent") or a stale pm manifest whose
// tip lags far behind the live pm tip ("stale"). Returns { bucket }.
function buildBucket(name, mode) {
  const bucket = path.join(tmpRoot, name);
  fs.mkdirSync(bucket, { recursive: true });
  const EMPTY_TREE = mkObject(bucket, "tree", Buffer.alloc(0));
  let ts = 1700000000;
  const commit = (parent, message) => {
    ts += 60;
    const lines = ["tree " + EMPTY_TREE];
    if (parent) lines.push("parent " + parent);
    lines.push("author T <t@example.com> " + ts + " +0000");
    lines.push("committer T <t@example.com> " + ts + " +0000");
    return mkObject(bucket, "commit", Buffer.from(lines.join("\n") + "\n\n" + message));
  };
  // chain returns { tip, shas } newest tracked so a stale manifest can point at
  // an OLD commit on the same branch.
  const chain = (n, mk) => { let p = null; const shas = []; for (let i = 0; i < n; i++) { p = commit(p, mk(i)); shas.push(p); } return { tip: p, shas }; };

  const pm = chain(PM_N, (i) => {
    const t = i % 8 === 0 ? "milestone" : (i % 8 === 1 ? "sprint" : "issue");
    return "PM " + t + " " + i + "\n\nGitMsg: ext=\"pm\" v=\"1\" type=\"" + t + "\" state=\"" + (i % 3 === 0 ? "closed" : "open") + "\"";
  });
  const review = chain(REVIEW_N, (i) => (i % 5 === 0)
    ? "PR " + i + "\n\nGitMsg: ext=\"review\" v=\"1\" type=\"pull-request\" state=\"" + (i % 2 ? "open" : "merged") + "\""
    : "Review feedback " + i + "\n\nGitMsg: ext=\"review\" v=\"1\" type=\"feedback\" pull-request=\"#commit:" + ("a".repeat(11) + (i % 10)) + "@gitmsg/review\" review-state=\"approved\"");
  const social = chain(SOCIAL_N, (i) => (i % 4 === 0)
    ? "Comment " + i + "\n\nGitMsg: ext=\"social\" v=\"1\" type=\"comment\" original=\"#commit:" + ("b".repeat(11) + (i % 10)) + "@gitmsg/pm\""
    : "Post " + i + "\n\nGitMsg: ext=\"social\" v=\"1\" type=\"post\"");
  const release = chain(RELEASE_N, (i) => "Release 1." + i + "\n\nGitMsg: ext=\"release\" v=\"1\" type=\"release\" version=\"1." + i + "\"");
  const master = chain(CODE_N, (i) => "Fix bug " + i);

  fs.writeFileSync(path.join(bucket, "HEAD"), "ref: refs/heads/master\n");
  fs.mkdirSync(path.join(bucket, "refs", "heads", "gitmsg"), { recursive: true });
  fs.writeFileSync(path.join(bucket, "refs", "heads", "master"), master.tip + "\n");
  for (const [ext, c] of [["pm", pm], ["review", review], ["social", social], ["release", release]]) {
    fs.writeFileSync(path.join(bucket, "refs", "heads", "gitmsg", ext), c.tip + "\n");
  }
  const site = path.join(bucket, ".gitsocial", "site");
  fs.mkdirSync(site, { recursive: true });
  fs.writeFileSync(path.join(bucket, ".gitsocial", "ref-mode"), "etag");
  fs.writeFileSync(path.join(site, "refs.json"), JSON.stringify({
    "refs/heads/master": master.tip,
    "refs/heads/gitmsg/pm": pm.tip,
    "refs/heads/gitmsg/review": review.tip,
    "refs/heads/gitmsg/social": social.tip,
    "refs/heads/gitmsg/release": release.tip,
  }));

  if (mode === "stale") {
    // A STALE pm manifest: version 4, but its tip is a FAR-OLDER pm commit than
    // the live tip in refs.json. The bridge from live tip to index tip exceeds the
    // WALK_CAP bridge window (the gap is > WALK_CAP commits), so seedWalkFromIndex
    // gives up and the walk degrades to a full loose walk — the exact stale shape
    // that must still be BOUNDED, not exhaust the branch. Older shard entries are
    // never fetched (bridge fails first), so an empty shard set is enough.
    const staleTip = pm.shas[Math.max(0, PM_N - 1 - (200 + 400))]; // ~600 behind
    fs.mkdirSync(path.join(site, "items", "pm"), { recursive: true });
    fs.writeFileSync(path.join(site, "items", "pm", "manifest.json"),
      JSON.stringify({ version: 4, tip: staleTip, complete: true, shards: [], bodiesBytes: 0 }));
    fs.writeFileSync(path.join(site, "items", "pm", "head.json"), JSON.stringify({ items: [] }));
  }
  // mode === "absent": no items/ dir at all (every ext index 404s).
  return { bucket };
}

buildBucket("absent", "absent");
buildBucket("stale", "stale");

const server = createServer(tmpRoot);
const listening = new Promise((r) => server.listen(0, "127.0.0.1", r));

let pass = 0, fail = 0;
const ok = (n, c, e) => { (c ? pass++ : fail++); console.log((c ? "PASS " : "FAIL ") + n + (!c && e ? " :: " + e : "")); };

async function main() {
  await listening;
  const origin = "http://127.0.0.1:" + server.address().port;
  process.env.GS_SITE_ORIGIN = origin;
  process.env.GS_SITE_BUCKET = "absent";

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
  await wait(300); // drain the auto-run init home route

  const CAP = GS.COUNTS_WALK_CAP;
  const WALK = GS.WALK_CAP;
  // A generous but sub-branch ceiling: the bounded exhaustive loader visits at
  // most CAP commits per data branch, plus a window's overshoot; analytics fans
  // over four branches. Any per-branch total near the branch size is the bug.
  const perBranchCeiling = CAP + WALK + 40;

  // driveRoute paints one hash under a bucket base and returns { painted,
  // looseGets, text }. It runs route() to completion (first paint), then lets any
  // backgrounded work settle so the reported total is the WHOLE route cost.
  async function driveRoute(bucket, hash, settleMs) {
    const ctx = GS.newContext(origin + "/" + bucket + "/");
    setHash(hash);
    looseGets = 0;
    await GS.route(ctx);
    const paintGets = looseGets;
    const painted = findClass(viewNode, "loading").length === 0;
    await wait(settleMs || 1500);
    return { painted, paintGets, totalGets: looseGets, text: textOf(viewNode) };
  }

  // Routes whose first paint drove loadExtItemsAll. `countsBranches` is how many
  // data branches the route ALSO fans over for interaction counts (issues and prs
  // compute them alongside their own set: pm/social/review, each loose-bounded to
  // CAP), so their bounded first-paint budget is (1 + countsBranches) cap windows,
  // still a small fraction of any single branch and never an exhaustion walk.
  const singleBranchRoutes = [
    { hash: "#/issues", branch: "pm", n: PM_N, countsBranches: 3, needle: /PM (issue|milestone|sprint)/ },
    { hash: "#/milestones", branch: "pm", n: PM_N, countsBranches: 0, needle: /(PM milestone|No milestones)/ },
    { hash: "#/sprints", branch: "pm", n: PM_N, countsBranches: 0, needle: /(PM sprint|No sprints)/ },
    { hash: "#/board", branch: "pm", n: PM_N, countsBranches: 0, needle: null },
    { hash: "#/prs", branch: "review", n: REVIEW_N, countsBranches: 3, needle: /(PR |No pull requests)/ },
  ];

  for (const mode of ["absent", "stale"]) {
    for (const rt of singleBranchRoutes) {
      const r = await driveRoute(mode, rt.hash);
      ok("[" + mode + "] " + rt.hash + " paints (not stuck on Loading…)", r.painted, r.text.slice(0, 60));
      ok("[" + mode + "] " + rt.hash + " first paint is BOUNDED (does not exhaust the " + rt.n + "-commit " + rt.branch + " branch)",
        r.paintGets < rt.n, "paintGets=" + r.paintGets + " branch=" + rt.n);
      const ceil = perBranchCeiling * (1 + (rt.countsBranches || 0));
      ok("[" + mode + "] " + rt.hash + " first paint stays within the shared cap window(s)",
        r.paintGets <= ceil, "paintGets=" + r.paintGets + " ceiling=" + ceil);
      if (rt.needle) ok("[" + mode + "] " + rt.hash + " renders recent items", rt.needle.test(r.text), r.text.slice(0, 80));
      ok("[" + mode + "] " + rt.hash + " total loose work stays bounded after settle",
        r.totalGets < rt.n * 2, "totalGets=" + r.totalGets);
    }

    // Analytics fans loadExtItemsAll over pm/review/social/release — the widest
    // exhaustion. First paint must be bounded across ALL four branches, not the
    // sum of their sizes.
    const branchSum = PM_N + REVIEW_N + SOCIAL_N + RELEASE_N;
    const a = await driveRoute(mode, "#/analytics", 2500);
    ok("[" + mode + "] #/analytics paints (not stuck on Loading…)", a.painted, a.text.slice(0, 60));
    ok("[" + mode + "] #/analytics renders the Analytics view", /Analytics/.test(a.text), a.text.slice(0, 60));
    ok("[" + mode + "] #/analytics first paint is BOUNDED (does not exhaust all four data branches)",
      a.paintGets < branchSum, "paintGets=" + a.paintGets + " branchSum=" + branchSum);
    ok("[" + mode + "] #/analytics first paint stays within the shared cap across four branches",
      a.paintGets <= perBranchCeiling * 4, "paintGets=" + a.paintGets + " ceiling=" + (perBranchCeiling * 4));
    ok("[" + mode + "] #/analytics total loose work stays bounded after settle",
      a.totalGets < branchSum, "totalGets=" + a.totalGets + " branchSum=" + branchSum);
    // The deliberate degrade: with no full index the aggregates cover recent items
    // only, and the view must SAY so (search-view voice) rather than present a
    // partial total as complete.
    ok("[" + mode + "] #/analytics notes limited (recent-only) coverage on a degraded bucket",
      /recent activity only|most recent items/i.test(a.text), a.text.slice(0, 120));
  }

  server.close();
  try { fs.rmSync(tmpRoot, { recursive: true, force: true }); } catch (_) { /* best effort */ }
  console.log("\n" + pass + " passed, " + fail + " failed");
  process.exit(fail ? 1 : 0);
}
main().catch((e) => { console.error("FAIL", e); server.close(); process.exit(1); });

// verify_branch_log_index.js - the branch-log route on the reader side: the
// DEFAULT branch's log is served from the CODE items index (ZERO per-commit
// loose-object GETs), a NON-default branch still walks loose objects, and
// autoscroll/Load-more paging over the indexed default log drains shards without
// per-commit GETs. Without a code index the default branch also falls back to the
// loose walk (old buckets keep working).
//
// Self-contained (builds its own loose-object buckets in Node and serves them via
// serve.js — no Go binary, no showcase fixture). Two buckets under one temp root:
//   - indexed: refs.json + a v4 code items index (multiple sealed shards + head +
//     manifest) over a main branch (>WALK_CAP commits, so paging needs a second
//     window) plus a feature branch off main's tip. The reader must build the
//     default-branch log from the index JSON alone, fetching NO code commit
//     objects, and a non-default branch must still walk.
//   - walk-only: the same repo shape with NO code index, so even the default
//     branch falls back to the loose walk (parity).
const fs = require("fs");
const os = require("os");
const path = require("path");
const zlib = require("zlib");
const crypto = require("crypto");
const { createServer } = require("./serve.js");

const tmpRoot = fs.mkdtempSync(path.join(os.tmpdir(), "gs-branchlog-idx-"));

// --- loose-object helpers (same shape as verify_code_index.js) ---
function sha1(buf) { return crypto.createHash("sha1").update(buf).digest("hex"); }
function writeObject(bucket, type, body) {
  const store = Buffer.concat([Buffer.from(type + " " + body.length + "\0"), body]);
  const sha = sha1(store);
  const dir = path.join(bucket, "objects", sha.slice(0, 2));
  fs.mkdirSync(dir, { recursive: true });
  fs.writeFileSync(path.join(dir, sha.slice(2)), zlib.deflateSync(store));
  return sha;
}
let ts = 1700000000;
function commit(bucket, tree, parent, message) {
  ts += 60;
  const lines = ["tree " + tree];
  if (parent) lines.push("parent " + parent);
  lines.push("author Ada <ada@example.com> " + ts + " +0000");
  lines.push("committer Ada <ada@example.com> " + ts + " +0000");
  return { sha: writeObject(bucket, "commit", Buffer.from(lines.join("\n") + "\n\n" + message)), ts, message };
}

const MAIN_N = 250; // > WALK_CAP (200), so the default log needs a second window
const FEAT_N = 3;

// buildRepo lays down a long main branch and a short feature branch off its tip.
// Returns every commit and the branch tips. Same shape for both buckets so the
// indexed and walk-only paths are compared apples to apples.
function buildRepo(bucket) {
  const EMPTY_TREE = writeObject(bucket, "tree", Buffer.alloc(0));
  const main = [];
  let parent = null;
  for (let i = 0; i < MAIN_N; i++) { const c = commit(bucket, EMPTY_TREE, parent, "main commit " + i); main.push(c); parent = c.sha; }
  const feature = [];
  parent = main[main.length - 1].sha;
  for (let i = 0; i < FEAT_N; i++) { const c = commit(bucket, EMPTY_TREE, parent, "feature commit " + i); feature.push(c); parent = c.sha; }
  fs.writeFileSync(path.join(bucket, "HEAD"), "ref: refs/heads/main\n");
  fs.mkdirSync(path.join(bucket, "refs", "heads"), { recursive: true });
  fs.writeFileSync(path.join(bucket, "refs", "heads", "main"), main[main.length - 1].sha + "\n");
  fs.mkdirSync(path.join(bucket, "refs", "heads", "feature"), { recursive: true });
  fs.writeFileSync(path.join(bucket, "refs", "heads", "feature", "x"), feature[feature.length - 1].sha + "\n");
  fs.mkdirSync(path.join(bucket, ".gitsocial", "site"), { recursive: true });
  fs.writeFileSync(path.join(bucket, ".gitsocial", "ref-mode"), "etag");
  return {
    main, feature, mainTip: main[main.length - 1].sha, featTip: feature[feature.length - 1].sha,
    refs: { "refs/heads/main": main[main.length - 1].sha, "refs/heads/feature/x": feature[feature.length - 1].sha },
  };
}

// writeCodeIndex writes a v4 code items index over the merged corpus: the shared
// main commits attribute to "main" (default wins), the feature-only commits to
// "feature/x". Sharded into small shards (SHARD_SIZE) so the default-log paging
// must drain several shards. Plain JSON (no brotli) so serve.js serves it without
// a Content-Encoding sidecar. Shard is content-hashed like the writer (first 12
// hex of sha256 over the member shas joined oldest-first).
const SHARD_SIZE = 40;
function writeCodeIndex(bucket, repo) {
  const entry = (c, branch) => ({ sha: c.sha, author: "Ada", email: "ada@example.com", ts: c.ts, header: "", subject: c.message, branch });
  // Oldest-first ingestion order across the merged corpus: main (oldest→newest)
  // then feature (oldest→newest).
  const oldestFirst = [];
  for (const c of repo.main) oldestFirst.push(entry(c, "main"));
  for (const c of repo.feature) oldestFirst.push(entry(c, "feature/x"));
  const dir = path.join(bucket, ".gitsocial", "site", "items", "code");
  fs.mkdirSync(dir, { recursive: true });
  // Seal all but the last SHARD_SIZE into sealed shards; the tail goes in head.
  const headStart = Math.max(0, oldestFirst.length - SHARD_SIZE);
  const sealed = oldestFirst.slice(0, headStart);
  const headItems = oldestFirst.slice(headStart);
  const shards = [];
  for (let i = 0; i < sealed.length; i += SHARD_SIZE) {
    const members = sealed.slice(i, i + SHARD_SIZE);
    const h = crypto.createHash("sha256");
    for (const e of members) { h.update(e.sha); h.update("\n"); }
    const shardHash = h.digest("hex").slice(0, 12);
    fs.writeFileSync(path.join(dir, "shard-" + shardHash + ".json"),
      JSON.stringify({ version: 4, tip: members[members.length - 1].sha, items: members }));
    shards.push({ key: "shard-" + shardHash + ".json", hash: shardHash, count: members.length, bytes: 200, endTip: members[members.length - 1].sha });
  }
  fs.writeFileSync(path.join(dir, "head.json"),
    JSON.stringify({ version: 4, tip: repo.featTip, items: headItems }));
  const codeTip = "0".repeat(40); // synthetic corpus tip; the reader only checks 40-hex
  fs.writeFileSync(path.join(dir, "manifest.json"), JSON.stringify({
    version: 4, tip: codeTip, totalBytes: 999, complete: true, shards,
    head: { count: headItems.length, bytes: 100 },
  }));
}

const indexed = path.join(tmpRoot, "indexed");
const walkOnly = path.join(tmpRoot, "walk-only");
fs.mkdirSync(indexed, { recursive: true });
fs.mkdirSync(walkOnly, { recursive: true });
const repoI = buildRepo(indexed);
const repoW = buildRepo(walkOnly);
fs.writeFileSync(path.join(indexed, ".gitsocial", "site", "refs.json"), JSON.stringify(repoI.refs));
fs.writeFileSync(path.join(walkOnly, ".gitsocial", "site", "refs.json"), JSON.stringify(repoW.refs));
writeCodeIndex(indexed, repoI);
// walk-only: NO items/code index at all -> the reader falls back to the loose walk.

const server = createServer(tmpRoot);
const listening = new Promise((r) => server.listen(0, "127.0.0.1", r));

let pass = 0, fail = 0;
const ok = (n, c, e) => { (c ? pass++ : fail++); console.log((c ? "PASS " : "FAIL ") + n + (!c && e ? " :: " + e : "")); };

async function main() {
  await listening;
  const origin = "http://127.0.0.1:" + server.address().port;
  process.env.GS_SITE_ORIGIN = origin;
  process.env.GS_SITE_BUCKET = "indexed";

  // Instrument fetch: count code-object (loose commit) GETs.
  const realFetch = global.fetch;
  let looseGets = 0;
  const looseRe = /objects\/[0-9a-f]{2}\/[0-9a-f]{38}$/;
  global.fetch = async (url, opts) => { if (looseRe.test(String(url).split("?")[0])) looseGets++; return realFetch(url, opts); };

  require("./shim.js");
  // The app auto-boots on import (gs-app.js init() -> route()) and renders the
  // location's route. Home walks the tree (object GETs) which would race into a
  // measured window; point the boot route at #/branches, which renders from the
  // refs manifest / HEAD only and fetches NO objects.
  global.location.hash = "#/branches";
  require("../site/icons.js");
  const GS = require("../site/gs-app.js");

  // Let the boot route settle (no measured call counts its object fetches).
  async function settle() {
    let last = -1;
    for (let i = 0; i < 50; i++) {
      if (looseGets === last) return;
      last = looseGets;
      await new Promise((r) => setTimeout(r, 10));
    }
  }
  await settle();

  // ---- 1. indexed bucket: DEFAULT branch log serves from the index, ZERO loose GETs ----
  {
    const ctx = GS.newContext(origin + "/indexed/");
    looseGets = 0;
    const r = await GS.loadBranchLogWindow(ctx, "main", false);
    ok("indexed default: tip resolved", /^[0-9a-f]{40}$/.test(r.tip || ""), "tip=" + r.tip);
    ok("indexed default: first window is WALK_CAP items", r.items.length === GS.WALK_CAP, "count=" + r.items.length);
    ok("indexed default: NO loose-object GET for the default log", looseGets === 0, "looseGets=" + looseGets);
    ok("indexed default: more history remains (truncated)", r.truncated === true);
    // Only main commits appear (feature-only commits attribute to feature/x, not default).
    const anyFeature = r.items.some((c) => /feature commit/.test(c.content || ""));
    ok("indexed default: feature-only commits excluded from default log", !anyFeature);
    // Newest-first: the newest main commit heads the list.
    ok("indexed default: newest-first (newest main commit first)", /main commit 249/.test(r.items[0].content || ""), (r.items[0] && r.items[0].content));
    // Card fields present from the index (subject/author/time/hash).
    const c0 = r.items[0];
    ok("indexed default: index fields suffice for the card", !!(c0.content && c0.authorName && c0.authorTime && c0.hash && c0.short), JSON.stringify({ s: !!c0.content, a: !!c0.authorName, t: !!c0.authorTime, h: !!c0.hash }));
  }

  // ---- 2. indexed bucket: autoscroll/Load-more paging over the indexed log ----
  {
    const ctx = GS.newContext(origin + "/indexed/");
    const first = await GS.loadBranchLogWindow(ctx, "main", false);
    looseGets = 0;
    const second = await GS.loadBranchLogWindow(ctx, "main", true);
    ok("indexed default: extend serves the full MAIN_N default commits", second.items.length === 250, "count=" + second.items.length);
    ok("indexed default: extend still fetches NO loose objects", looseGets === 0, "looseGets=" + looseGets);
    ok("indexed default: extend accumulates (grows past the first window)", second.items.length > first.items.length);
    ok("indexed default: fully paged -> not truncated", second.truncated === false, "truncated=" + second.truncated);
    // No duplicates across the paged set.
    const seen = new Set(second.items.map((c) => c.hash));
    ok("indexed default: no dup commits across pages", seen.size === second.items.length, "unique=" + seen.size);
  }

  // ---- 2b. indexed bucket: branchLogView renders index-backed cards (real DOM) ----
  {
    const ctx = GS.newContext(origin + "/indexed/");
    looseGets = 0;
    const nodes = await GS.branchLogView(ctx, "main");
    ok("indexed default: branchLogView renders without loose GETs", looseGets === 0, "looseGets=" + looseGets);
    function findClass(node, cls, out) { out = out || []; for (const c of (node && node._children) || []) { if (c && c.nodeType === 1) { if (c._cls && c._cls.has(cls)) out.push(c); findClass(c, cls, out); } } return out; }
    const textOf = global.__shim.textOf;
    const subjects = nodes.flatMap((n) => findClass(n, "subject"));
    const hashes = nodes.flatMap((n) => findClass(n, "hash"));
    ok("indexed default: view has commit-row subjects", subjects.some((s) => /main commit/.test(textOf(s) || "")), subjects.length + " subjects");
    ok("indexed default: rows link to #commit:", hashes.some((h) => (h.getAttribute("href") || "").indexOf("#commit:") === 0));
  }

  // ---- 3. indexed bucket: a NON-default branch still WALKS loose objects ----
  {
    const ctx = GS.newContext(origin + "/indexed/");
    looseGets = 0;
    const r = await GS.loadBranchLogWindow(ctx, "feature/x", false);
    ok("indexed non-default: tip resolved", r.tip === repoI.featTip, "tip=" + r.tip);
    ok("indexed non-default: the loose walk DID fetch commit objects (walk active)", looseGets > 0, "looseGets=" + looseGets);
    // The feature branch reaches its own commits AND its main ancestors.
    const hasFeat = r.items.some((c) => /feature commit 2/.test(c.content || ""));
    const hasMain = r.items.some((c) => /main commit 249/.test(c.content || ""));
    ok("indexed non-default: feature tip + main ancestors both present (walk)", hasFeat && hasMain, JSON.stringify({ hasFeat, hasMain }));
  }

  // ---- 4. walk-only bucket: no index -> even the DEFAULT branch walks ----
  {
    const ctx = GS.newContext(origin + "/walk-only/");
    looseGets = 0;
    const r = await GS.loadBranchLogWindow(ctx, "main", false);
    ok("walk-only default: the loose walk DID fetch commit objects (fallback active)", looseGets > 0, "looseGets=" + looseGets);
    ok("walk-only default: first window is WALK_CAP items", r.items.length === GS.WALK_CAP, "count=" + r.items.length);
    ok("walk-only default: newest-first (newest main commit first)", /main commit 249/.test(r.items[0].content || ""), (r.items[0] && r.items[0].content));
  }

  server.close();
  try { fs.rmSync(tmpRoot, { recursive: true, force: true }); } catch (_) { /* best effort */ }
  console.log("\n" + pass + " passed, " + fail + " failed");
  process.exit(fail ? 1 : 0);
}
main().catch((e) => { console.error(e); process.exit(1); });

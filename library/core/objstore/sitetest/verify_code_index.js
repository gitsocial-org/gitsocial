// verify_code_index.js - the CODE items index on the reader side: with the index
// present the timeline sources code commits metadata-only (ZERO per-commit
// loose-object GETs), and without it the loose graph walk still produces the same
// code commits (old buckets keep working).
//
// Self-contained (builds its own loose-object buckets in Node and serves them via
// serve.js — no Go binary, no showcase fixture). Two buckets under one temp root:
//   - indexed: refs.json + a v4 code items index (shard + head + manifest) over a
//     main + feature branch. The reader must build the code timeline from the
//     index JSON alone, fetching NO code commit objects.
//   - walk-only: the same repo shape with NO code index, so the reader falls back
//     to the loose walk and still lists the code commits (parity).
const fs = require("fs");
const os = require("os");
const path = require("path");
const zlib = require("zlib");
const crypto = require("crypto");
const { createServer } = require("./serve.js");

const tmpRoot = fs.mkdtempSync(path.join(os.tmpdir(), "gs-code-idx-"));

// --- loose-object helpers (same shape as the other self-contained suites) ---
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

// buildRepo lays down a main branch (3 commits) with a feature branch (2 commits)
// off its tip, returning every commit and the branch tips. Same shape for both
// buckets so the indexed and walk-only paths are compared apples to apples.
function buildRepo(bucket) {
  const EMPTY_TREE = writeObject(bucket, "tree", Buffer.alloc(0));
  const m0 = commit(bucket, EMPTY_TREE, null, "Initial commit: README");
  const m1 = commit(bucket, EMPTY_TREE, m0.sha, "Add notes");
  const m2 = commit(bucket, EMPTY_TREE, m1.sha, "Extend notes on main");
  const f0 = commit(bucket, EMPTY_TREE, m2.sha, "Feature: start work");
  const f1 = commit(bucket, EMPTY_TREE, f0.sha, "Feature: finish work");
  fs.writeFileSync(path.join(bucket, "HEAD"), "ref: refs/heads/main\n");
  fs.mkdirSync(path.join(bucket, "refs", "heads"), { recursive: true });
  fs.writeFileSync(path.join(bucket, "refs", "heads", "main"), m2.sha + "\n");
  fs.mkdirSync(path.join(bucket, "refs", "heads", "feature"), { recursive: true });
  fs.writeFileSync(path.join(bucket, "refs", "heads", "feature", "x"), f1.sha + "\n");
  fs.mkdirSync(path.join(bucket, ".gitsocial", "site"), { recursive: true });
  fs.writeFileSync(path.join(bucket, ".gitsocial", "ref-mode"), "etag");
  return {
    main: [m0, m1, m2], feature: [f0, f1], mainTip: m2.sha, featTip: f1.sha,
    refs: { "refs/heads/main": m2.sha, "refs/heads/feature/x": f1.sha },
  };
}

// writeCodeIndex writes a v4 code items index: one sealed shard (the 4 oldest,
// oldest-first) + a head (the newest) + a manifest, each carrying the code entry
// fields (sha/author/email/ts/header/subject/branch). Plain JSON (no brotli) so
// serve.js serves it without a Content-Encoding sidecar and the reader parses it
// straight. Shard is content-hashed like the writer (first 12 hex of sha256 over
// the member shas joined oldest-first).
function writeCodeIndex(bucket, repo) {
  const entry = (c, branch) => ({ sha: c.sha, author: "Ada", email: "ada@example.com", ts: c.ts, header: "", subject: c.message, branch });
  // Oldest-first ingestion order across the merged corpus: main(3) then feature(2).
  const oldestFirst = [
    entry(repo.main[0], "main"), entry(repo.main[1], "main"), entry(repo.main[2], "main"),
    entry(repo.feature[0], "feature/x"), entry(repo.feature[1], "feature/x"),
  ];
  const sealed = oldestFirst.slice(0, 4); // one shard of 4
  const head = oldestFirst.slice(4);      // the newest 1 in the head
  const h = crypto.createHash("sha256");
  for (const e of sealed) { h.update(e.sha); h.update("\n"); }
  const shardHash = h.digest("hex").slice(0, 12);
  const dir = path.join(bucket, ".gitsocial", "site", "items", "code");
  fs.mkdirSync(dir, { recursive: true });
  fs.writeFileSync(path.join(dir, "shard-" + shardHash + ".json"),
    JSON.stringify({ version: 4, tip: sealed[sealed.length - 1].sha, items: sealed }));
  fs.writeFileSync(path.join(dir, "head.json"),
    JSON.stringify({ version: 4, tip: repo.featTip, items: head }));
  const codeTip = "0".repeat(40); // synthetic corpus tip; the reader only checks 40-hex
  fs.writeFileSync(path.join(dir, "manifest.json"), JSON.stringify({
    version: 4, tip: codeTip, totalBytes: 999, complete: true,
    shards: [{ key: "shard-" + shardHash + ".json", hash: shardHash, count: 4, bytes: 200, endTip: sealed[sealed.length - 1].sha }],
    head: { count: head.length, bytes: 100 },
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
  require("../site/icons.js");
  const GS = require("../site/gs-app.js");

  // ---- 1. indexed bucket: code timeline sources from the index, ZERO loose GETs ----
  {
    const ctx = GS.newContext(origin + "/indexed/");
    looseGets = 0;
    const r = await GS.resolveCodeItems(ctx, 200);
    ok("indexed: code items surface from the index", r.items.length === 5, "count=" + r.items.length);
    ok("indexed: NO loose-object GET for code cards (metadata-only)", looseGets === 0, "looseGets=" + looseGets);
    const subjects = r.items.map((i) => i.content).join(" | ");
    ok("indexed: card subject comes from the index (no hydration)", /Feature: finish work/.test(subjects) && /Initial commit: README/.test(subjects), subjects.slice(0, 120));
    const byHash = {};
    for (const i of r.items) byHash[i.commit.hash] = i;
    ok("indexed: no dup across branches", Object.keys(byHash).length === r.items.length);
    const readme = r.items.find((i) => /Initial commit: README/.test(i.content));
    ok("indexed: shared/main commit attributes to main", readme && readme._branch === "main", "branch=" + (readme && readme._branch));
    const feat = r.items.find((i) => /Feature: finish work/.test(i.content));
    ok("indexed: feature commit attributes to feature/x", feat && feat._branch === "feature/x", "branch=" + (feat && feat._branch));
  }

  // ---- 2. indexed bucket: the timeline card renders subject + hash from the index ----
  {
    const ctx = GS.newContext(origin + "/indexed/");
    const r = await GS.resolveCodeItems(ctx, 200);
    const item = r.items[0]; item._ext = "code";
    const card = GS.timelineCard(item, null);
    function findClass(node, cls, out) { out = out || []; for (const c of (node && node._children) || []) { if (c && c.nodeType === 1) { if (c._cls && c._cls.has(cls)) out.push(c); findClass(c, cls, out); } } return out; }
    const textOf = global.__shim.textOf;
    ok("indexed: card renders a non-empty subject", findClass(card, "subject").length > 0 && textOf(findClass(card, "subject")[0]).length > 0);
    ok("indexed: card hash links to #commit:", findClass(card, "hash").some((h) => (h.getAttribute("href") || "").indexOf("#commit:") === 0));
  }

  // ---- 3. walk-only bucket: no index -> loose walk still lists the code commits ----
  {
    const ctx = GS.newContext(origin + "/walk-only/");
    looseGets = 0;
    const r = await GS.resolveCodeItems(ctx, 200);
    ok("walk-only: code items surface via the loose walk", r.items.length === 5, "count=" + r.items.length);
    ok("walk-only: the loose walk DID fetch commit objects (fallback active)", looseGets > 0, "looseGets=" + looseGets);
    const subjects = r.items.map((i) => i.content).join(" | ");
    ok("walk-only: same code subjects as the indexed bucket", /Feature: finish work/.test(subjects) && /Initial commit: README/.test(subjects), subjects.slice(0, 120));
    const readme = r.items.find((i) => /Initial commit: README/.test(i.content));
    ok("walk-only: shared/main commit still attributes to main", readme && readme._branch === "main", "branch=" + (readme && readme._branch));
  }

  server.close();
  try { fs.rmSync(tmpRoot, { recursive: true, force: true }); } catch (_) { /* best effort */ }
  console.log("\n" + pass + " passed, " + fail + " failed");
  process.exit(fail ? 1 : 0);
}
main().catch((e) => { console.error(e); process.exit(1); });

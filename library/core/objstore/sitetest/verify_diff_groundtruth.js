// verify_diff_groundtruth.js - diff engine grounded against real git output on
// the gitsocial bucket:
//  - diffLines unit tests (add/del/eq, bail cap, hunk headers)
//  - diffTrees vs `git show --name-status` (path set + statuses, scan-cap bound)
//  - fileDiff +/- vs `git diff --numstat`
//  - mergeBase vs `git merge-base` (grounds the PR three-dot path)
const { execSync } = require("child_process");
const GS = require("../site/gs-core.js");
const REPO = (process.env.GS_SITE_REPO || require("path").join(__dirname,"../../../.."));
const BASE = process.env.BASE || (process.env.GS_SITE_ORIGIN||"http://localhost:8000")+"/gitsocial/";
let pass = 0, fail = 0;
function ok(name, cond, extra) { (cond ? pass++ : fail++); console.log((cond ? "PASS " : "FAIL ") + name + (extra ? " :: " + extra : "")); }
function git(args) { return execSync("git -C " + REPO + " " + args, { maxBuffer: 1 << 28 }).toString(); }

// ---- Unit tests: diffLines ----
function unit() {
  const d1 = GS.diffLines("", "a\nb\n");
  ok("diffLines empty->content all add", d1.length === 2 && d1.every((o) => o.op === "add"), JSON.stringify(d1));
  const d2 = GS.diffLines("a\nb\n", "");
  ok("diffLines content->empty all del", d2.length === 2 && d2.every((o) => o.op === "del"));
  const d3 = GS.diffLines("l1\nl2\nl3\nl4\nl5\n", "l1\nl2\nXX\nl4\nl5\n");
  const adds = d3.filter((o) => o.op === "add"), dels = d3.filter((o) => o.op === "del"), eqs = d3.filter((o) => o.op === "eq");
  ok("diffLines mid change 1add 1del 4eq", adds.length === 1 && dels.length === 1 && eqs.length === 4, JSON.stringify(d3.map((o) => o.op)));
  ok("diffLines mid change add=XX del=l3", adds[0].line === "XX" && dels[0].line === "l3");
  const same = "x\ny\nz\n";
  const d4 = GS.diffLines(same, same);
  ok("diffLines identical all eq", d4.length === 3 && d4.every((o) => o.op === "eq"));
  const big = Array.from({ length: 3000 }, (_, i) => "line" + i).join("\n");
  const big2 = Array.from({ length: 3000 }, (_, i) => "LINE" + i).join("\n");
  ok("diffLines >5000 total bails to null", GS.diffLines(big, big2) === null);
  const forced = GS.diffLines(big, big2, true);
  ok("diffLines force bypasses the cap (Diff anyway)", Array.isArray(forced) && forced.length === 6000 && forced.every((o) => o.op !== "eq"));
  // hunk building sanity
  const h = GS.buildHunks(d3, 3);
  ok("buildHunks one hunk header", h.length === 1 && h[0].oldStart === 1 && h[0].newStart === 1, JSON.stringify(h[0] && { os: h[0].oldStart, oc: h[0].oldCount, ns: h[0].newStart, nc: h[0].newCount }));
}

// ---- Ground truth: diffTrees vs git name-status ----
function nameStatus(sha) {
  const out = git("show --name-status --no-renames --format= " + sha).trim();
  const map = {};
  for (const line of out.split("\n")) {
    if (!line.trim()) continue;
    const parts = line.split("\t");
    const st = parts[0][0];
    const path = parts[parts.length - 1];
    map[path] = st === "A" ? "added" : st === "D" ? "deleted" : "modified";
  }
  return map;
}

async function treesOf(ctx, sha) {
  const obj = await GS.getObject(ctx, sha);
  const c = GS.parseCommit(sha, obj.body);
  const parentTree = c.parents.length ? await GS.commitTree(ctx, c.parents[0]) : null;
  return { tree: c.tree, parentTree, parents: c.parents.length };
}

async function checkCommit(ctx, sha, label) {
  const gt = nameStatus(sha);
  const t = await treesOf(ctx, sha);
  const entries = await GS.diffTrees(ctx, t.parentTree, t.tree);
  const got = {};
  for (const e of entries) got[e.path] = e.status;
  const gtKeys = Object.keys(gt).sort();
  const gotKeys = Object.keys(got).sort();
  const SCAN_CAP = GS.DIFF_TREE_SCAN_CAP;
  if (gtKeys.length > SCAN_CAP) {
    // diffTrees is now bound (roadmap item 7): a commit changing more than
    // DIFF_TREE_SCAN_CAP paths stops descending, returns exactly the cap, and
    // flags truncated. Assert the returned paths are a subset of git's with
    // matching statuses, rather than the full (unbounded) set.
    const subset = gotKeys.every((k) => gt[k] !== undefined);
    ok(label + " bounded: returns cap + truncated (git " + gtKeys.length + " files)",
      entries.truncated === true && gotKeys.length === SCAN_CAP && subset,
      "truncated=" + entries.truncated + " got=" + gotKeys.length + " cap=" + SCAN_CAP + " subset=" + subset);
    const stMatch = gotKeys.every((k) => gt[k] === got[k]);
    ok(label + " bounded statuses match (returned subset)", stMatch,
      stMatch ? "" : JSON.stringify(gotKeys.filter((k) => gt[k] !== got[k]).map((k) => k + ":" + gt[k] + "!=" + got[k])).slice(0, 300));
    return { entries };
  }
  ok(label + " path set matches name-status (" + gtKeys.length + " files)", JSON.stringify(gtKeys) === JSON.stringify(gotKeys),
    "git=" + gtKeys.length + " got=" + gotKeys.length + (gtKeys.length !== gotKeys.length ? " diff=" + JSON.stringify(gtKeys.filter((k) => !got[k]).concat(gotKeys.filter((k) => !gt[k]))).slice(0, 300) : ""));
  let statusMatch = gtKeys.every((k) => gt[k] === got[k]);
  ok(label + " statuses match", statusMatch, statusMatch ? "" : JSON.stringify(gtKeys.filter((k) => gt[k] !== got[k]).map((k) => k + ":" + gt[k] + "!=" + got[k])).slice(0, 300));
  ok(label + " not truncated (under cap)", entries.truncated === false, "truncated=" + entries.truncated);
  return { entries };
}

// numstat for a single file path
function numstat(sha, path) {
  const out = git("diff --numstat " + sha + "^ " + sha + " -- \"" + path + "\"").trim();
  if (!out) return null;
  const [add, del] = out.split("\n")[0].split("\t");
  return { add: parseInt(add, 10), del: parseInt(del, 10) };
}

async function checkNumstat(ctx, sha, path) {
  const entries = await GS.diffTrees(ctx, (await treesOf(ctx, sha)).parentTree, (await treesOf(ctx, sha)).tree);
  const e = entries.find((x) => x.path === path);
  if (!e) { ok("numstat entry present " + path, false); return; }
  const model = await GS.fileDiff(ctx, e);
  const ns = numstat(sha, path);
  ok("numstat " + path + " +/- matches git (git +" + ns.add + "/-" + ns.del + ")", model.adds === ns.add && model.dels === ns.del,
    "got +" + model.adds + "/-" + model.dels);
}

async function main() {
  unit();
  const ctx = GS.newContext(BASE);

  // 1. small single-file change (config/doc)
  await checkCommit(ctx, "fe016ced917903d39cfa2a8da9442152bfbb882d", "single-file fe016ced");
  // 2. multi-file code change
  await checkCommit(ctx, "f084bb2d505605b9ff5cf7de2904a7f2341a6346", "multi-file f084bb2d");
  // 3. two-file change incl large app.js
  await checkCommit(ctx, "a153837a76fb731aa952c2b9dd974fc52c41caa6", "two-file a153837a");
  // 4. root-vs-parent: second commit (parent = empty root) -> all added
  const second = await checkCommit(ctx, "b5b0149ddef333b7f79075f8f341c540f18db836", "second-commit(all-added) b5b0149");
  ok("second commit all-added statuses", second.entries.every((e) => e.status === "added"), "n=" + second.entries.length);
  // 5. root commit (no parents, empty tree) -> zero changes
  const rootEntries = await GS.diffTrees(ctx, null, (await treesOf(ctx, "d1e182dc374c68abf6fc173afe58cc3e743c7081")).tree);
  ok("root commit d1e182dc empty tree -> 0 changes", rootEntries.length === 0, "n=" + rootEntries.length);

  // numstat on 2 specific files
  await checkNumstat(ctx, "fe016ced917903d39cfa2a8da9442152bfbb882d", ".github/workflows/pages.yml");
  await checkNumstat(ctx, "f084bb2d505605b9ff5cf7de2904a7f2341a6346", "library/core/objstore/site/app.js");

  // ---- merge-base engine vs git merge-base (grounds the PR three-dot path) ----
  const A = "fe016ced917903d39cfa2a8da9442152bfbb882d";
  const B = "a153837a76fb731aa952c2b9dd974fc52c41caa6";
  const mb = await GS.mergeBase(ctx, B, A, GS.WALK_CAP);
  const gitMb = git("merge-base " + A + " " + B).trim();
  ok("mergeBase(head=B,base=A) == git merge-base", mb === gitMb, "got=" + (mb || "").slice(0, 12) + " git=" + gitMb.slice(0, 12));
  // linear history: merge-base of ancestor & descendant is the ancestor
  ok("mergeBase ancestor==A (A is ancestor of B)", mb === A, mb);

  console.log("\n" + pass + " passed, " + fail + " failed");
  process.exit(fail ? 1 : 0);
}
main().catch((e) => { console.error(e); process.exit(1); });

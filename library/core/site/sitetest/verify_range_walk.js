// verify_range_walk.js - the compare list and merge base without a code index equal git, and cost reads in proportion to the range
const fs = require("fs");
const os = require("os");
const path = require("path");
const zlib = require("zlib");
const crypto = require("crypto");
const { execFileSync } = require("child_process");
const { createServer } = require("./serve.js");

const tmpRoot = fs.mkdtempSync(path.join(os.tmpdir(), "gs-rangewalk-"));
const bucket = path.join(tmpRoot, "range");
const TRUNK_N = 3000;

// writeObject stores one loose object in the bucket and returns its sha.
function writeObject(type, body) {
  const store = Buffer.concat([Buffer.from(type + " " + body.length + "\0"), body]);
  const sha = crypto.createHash("sha1").update(store).digest("hex");
  const d = path.join(bucket, "objects", sha.slice(0, 2));
  fs.mkdirSync(d, { recursive: true });
  fs.writeFileSync(path.join(d, sha.slice(2)), zlib.deflateSync(store));
  return sha;
}

const emptyTree = writeObject("tree", Buffer.alloc(0));
// commit writes a commit with the given parents; committerTs defaults to the author time.
function commit(parents, ts, message, committerTs) {
  const lines = ["tree " + emptyTree].concat(parents.map((p) => "parent " + p));
  lines.push("author T <t@example.com> " + ts + " +0000");
  lines.push("committer T <t@example.com> " + (committerTs || ts) + " +0000");
  return writeObject("commit", Buffer.from(lines.join("\n") + "\n\n" + message + "\n"));
}
// chain writes n commits on top of parent, one minute apart from start.
function chain(parent, n, start, label) {
  const out = [];
  for (let i = 0; i < n; i++) { parent = commit(parent ? [parent] : [], start + i * 60, label + " " + i); out.push(parent); }
  return out;
}

const T0 = 1700000000;
const trunk = chain(null, TRUNK_N, T0, "trunk");
const tAt = (i) => T0 + i * 60;
const side = chain(trunk[2950], 10, tAt(2950) + 30, "side");
const feat = chain(trunk[2980], 10, tAt(2980) + 30, "feat");
const merge = commit([trunk[2999], feat[9]], tAt(3000), "merge feat");
const k0 = commit([trunk[2990]], tAt(2990) + 20, "skew 0");
const k1 = commit([k0], tAt(2990) + 25, "skew 1", tAt(2000));
const k2 = commit([k1], tAt(2990) + 40, "skew 2");
fs.writeFileSync(path.join(bucket, "HEAD"), "ref: refs/heads/main\n");
fs.mkdirSync(path.join(bucket, "refs", "heads"), { recursive: true });
fs.writeFileSync(path.join(bucket, "refs", "heads", "main"), trunk[TRUNK_N - 1] + "\n");

// git runs git on the bucket, which is also a bare repository.
const git = (...args) => execFileSync("git", ["--git-dir=" + bucket].concat(args), { encoding: "utf8" }).trim();
const revList = (base, head) => git("rev-list", base + ".." + head).split("\n").filter(Boolean).sort();
const gitMergeBase = (a, b) => { try { return git("merge-base", a, b); } catch (_) { return null; } };

const server = createServer(tmpRoot);
const listening = new Promise((r) => server.listen(0, "127.0.0.1", r));

let pass = 0, fail = 0;
const ok = (n, c, e) => { (c ? pass++ : fail++); console.log((c ? "PASS " : "FAIL ") + n + (!c && e ? " :: " + e : "")); };

async function main() {
  await listening;
  const base = "http://127.0.0.1:" + server.address().port + "/range/";
  const realFetch = global.fetch;
  let looseGets = 0;
  const looseRe = /objects\/[0-9a-f]{2}\/[0-9a-f]{38}$/;
  global.fetch = async (url, opts) => { if (looseRe.test(String(url).split("?")[0])) looseGets++; return realFetch(url, opts); };
  require("../assets/gs-core.js");
  const GS = global.GS;

  // compareAll pages loadCompareCommitsWindow to the end and returns the sorted shas.
  async function compareAll(ctx, b, h) {
    let r = await GS.loadCompareCommitsWindow(ctx, b, h, false);
    while (r.truncated) r = await GS.loadCompareCommitsWindow(ctx, b, h, true);
    return r.items.map((c) => c.hash).sort();
  }

  const cases = [
    ["linear", trunk[2969], trunk[2999]],
    ["diverged", trunk[2999], side[9]],
    ["diverged, reversed", side[9], trunk[2999]],
    ["merged side branch", trunk[2990], merge],
    ["head behind base", trunk[2999], trunk[2990]],
    ["committer clock skew", trunk[2995], k2],
    ["range longer than the cap", trunk[100], trunk[2999]],
  ];
  for (const [name, b, h] of cases) {
    const got = await compareAll(GS.newContext(base), b, h);
    const want = revList(b, h);
    ok("[" + name + "] range equals git rev-list", JSON.stringify(got) === JSON.stringify(want), "got " + got.length + " want " + want.length);
    const mb = await GS.resolveMergeBase(GS.newContext(base), h, b, GS.DETAIL_WALK_CAP);
    const wantMb = want.length > GS.DETAIL_WALK_CAP ? null : gitMergeBase(h, b);
    ok("[" + name + "] merge base equals git merge-base, absent past the cap", mb === wantMb, mb + " vs " + wantMb);
  }

  looseGets = 0;
  await compareAll(GS.newContext(base), trunk[2969], trunk[2999]);
  ok("a 30-commit range under 3000 commits of history costs fewer than 60 reads", looseGets < 60, "reads=" + looseGets);
  looseGets = 0;
  await GS.resolveMergeBase(GS.newContext(base), trunk[2999], trunk[2969], GS.DETAIL_WALK_CAP);
  ok("its merge base costs fewer than 60 reads", looseGets < 60, "reads=" + looseGets);

  const deep = GS.newContext(base);
  looseGets = 0;
  const first = await GS.loadCompareCommitsWindow(deep, trunk[100], trunk[2999], false);
  const under = new Set(trunk.slice(0, 101));
  ok("a range longer than the window shows one window, truncated", first.items.length === GS.WALK_CAP && first.truncated, "items=" + first.items.length);
  ok("that window reads about one window of commits", looseGets < GS.WALK_CAP + 20, "reads=" + looseGets);
  ok("that window holds no commit from under the base", first.items.every((c) => !under.has(c.hash)));
  looseGets = 0;
  const capped = await GS.mergeBase(GS.newContext(base), trunk[2999], trunk[100], 100);
  ok("a merge base past the cap reads as absent, within the cap", capped === null && looseGets <= 100 + GS.CONCURRENCY, "mb=" + capped + " reads=" + looseGets);

  const flaky = GS.newContext(base);
  const failSha = side[4];
  let failOnce = true;
  const countingFetch = global.fetch;
  global.fetch = async (url, opts) => {
    if (failOnce && String(url).endsWith(failSha.slice(0, 2) + "/" + failSha.slice(2))) { failOnce = false; throw new TypeError("network"); }
    return countingFetch(url, opts);
  };
  const failed = await GS.loadCompareCommitsWindow(flaky, trunk[2999], side[9], false).then(() => false, () => true);
  const retried = (await compareAll(flaky, trunk[2999], side[9])).join();
  const retriedMb = await GS.resolveMergeBase(flaky, side[9], trunk[2999], GS.DETAIL_WALK_CAP);
  global.fetch = countingFetch;
  ok("a failed read fails the call", failed);
  ok("the next call on the same context walks again and equals git", retried === revList(trunk[2999], side[9]).join() && retriedMb === trunk[2950]);

  server.close();
  try { fs.rmSync(tmpRoot, { recursive: true, force: true }); } catch (_) { /* best effort */ }
  console.log("\n" + pass + " passed, " + fail + " failed");
  process.exit(fail ? 1 : 0);
}
main().catch((e) => { console.error("FAIL", e); server.close(); process.exit(1); });

// verify_fork_pulls.js - the fork area: a bucket built here with a base branch
// in loose objects, a fork head and its review commit in a pack under
// .gitsocial/forks/pack/, and a hand-written manifest. The list shows the fork
// pull request with its chip, and its detail diffs the head against the base.
require("./shim.js");
require("../assets/icons.js");
const GS = require("../assets/gs-app.js");
const { execFileSync } = require("child_process");
const fs = require("fs");
const os = require("os");
const path = require("path");
const { createServer } = require("./serve.js");
const { viewNode, textOf, setHash } = global.__shim;
const ORIGIN = process.env.GS_SITE_ORIGIN || "http://localhost:8000";
let pass = 0, fail = 0;
const ok = (n, c, e) => { (c ? pass++ : fail++); console.log((c ? "PASS " : "FAIL ") + n + (!c && e ? " :: " + e : "")); };
const wait = (ms) => new Promise((r) => setTimeout(r, ms));
function findClass(node, cls, out) { out = out || []; for (const c of (node && node._children) || []) { if (c && c.nodeType === 1) { if (c._cls && c._cls.has(cls)) out.push(c); findClass(c, cls, out); } } return out; }
const text = (node) => textOf(node).replace(/\s+/g, " ").trim();

// Every bucket fetch is recorded, so a read can be traced to the fork area or the bucket.
const realFetch = global.fetch;
const log = [];
global.fetch = async (url, opts) => { log.push(String(url)); return realFetch(url, opts); };

// settle waits until the route in flight stops fetching, or until the probe holds.
async function settle(probe) {
  let seen = -1, still = Date.now();
  for (const deadline = Date.now() + 15000; Date.now() < deadline;) {
    if (probe && probe()) return;
    if (log.length !== seen) { seen = log.length; still = Date.now(); } else if (Date.now() - still >= 400) return;
    await wait(50);
  }
}

const FORK_URL = "https://example.com/fork";
const BASE_FIELD = "https://example.com/upstream#branch:main";

// buildBucket writes a served bucket root: the base branch loose, the fork head and review commit packed under the fork area, and the manifest.
function buildBucket(root) {
  const repo = path.join(root, "repo");
  const served = path.join(root, "served");
  fs.mkdirSync(repo, { recursive: true });
  const env = Object.assign({}, process.env, {
    GIT_AUTHOR_NAME: "Ada", GIT_AUTHOR_EMAIL: "ada@example.com", GIT_COMMITTER_NAME: "Ada", GIT_COMMITTER_EMAIL: "ada@example.com",
    GIT_AUTHOR_DATE: "1750000000 +0000", GIT_COMMITTER_DATE: "1750000000 +0000",
  });
  const git = (args, input) => execFileSync("git", ["-C", repo].concat(args), { encoding: "utf8", env, input }).trim();
  git(["init", "-q", "-b", "main"]);
  fs.writeFileSync(path.join(repo, "notes.txt"), "one\ntwo\n");
  git(["add", "-A"]); git(["commit", "-qm", "base"]);
  const base = git(["rev-parse", "HEAD"]);
  git(["switch", "-qc", "feature"]);
  fs.writeFileSync(path.join(repo, "notes.txt"), "one\ntwo\nthree\n");
  git(["add", "-A"]); git(["commit", "-qm", "Add a third note"]);
  const head = git(["rev-parse", "HEAD"]);
  git(["switch", "-q", "main"]);
  const header = 'GitMsg: v="0.1.0" ext="review" type="pull-request" state="open" base="' + BASE_FIELD + '" base-tip="' + base.slice(0, 12) + '" head="#branch:feature" head-tip="' + head.slice(0, 12) + '"';
  const emptyTree = git(["hash-object", "-t", "tree", "/dev/null"]);
  const pr = git(["commit-tree", emptyTree, "-m", "Add a third note\n\nOne more line for the notes.\n\n" + header]);
  git(["update-ref", "refs/heads/gitmsg/review", pr]);
  const packDir = path.join(served, ".gitsocial", "forks", "pack");
  fs.mkdirSync(packDir, { recursive: true });
  const objects = git(["rev-list", "--objects", "feature", "gitmsg/review", "--not", "main"]);
  const packHash = git(["pack-objects", "--delta-base-offset", "-q", path.join(packDir, "pack")], objects + "\n");
  for (const line of git(["rev-list", "--objects", "main"]).split("\n")) {
    const sha = line.split(" ")[0];
    const dir = path.join(served, "objects", sha.slice(0, 2));
    fs.mkdirSync(dir, { recursive: true });
    fs.copyFileSync(path.join(repo, ".git", "objects", sha.slice(0, 2), sha.slice(2)), path.join(dir, sha.slice(2)));
  }
  fs.mkdirSync(path.join(served, "refs", "heads"), { recursive: true });
  fs.writeFileSync(path.join(served, "HEAD"), "ref: refs/heads/main\n");
  fs.writeFileSync(path.join(served, "refs", "heads", "main"), base + "\n");
  fs.writeFileSync(path.join(served, ".gitsocial", "refs.json"), JSON.stringify({ "refs/heads/main": base }));
  const packBytes = fs.statSync(path.join(packDir, "pack-" + packHash + ".pack")).size;
  const manifest = { version: 1, bytes: packBytes, items: [
    { sha: pr, author: "Ada", email: "ada@example.com", ts: 1750000000, header, subject: "Add a third note", repo: FORK_URL, head, pack: "pack-" + packHash, packBytes },
  ] };
  fs.writeFileSync(path.join(served, ".gitsocial", "forks", "manifest.json"), JSON.stringify(manifest));
  return { served, pr, head, packName: "pack-" + packHash };
}

async function main() {
  await settle();
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "gs-forkpulls-"));
  const fx = buildBucket(root);
  const server = createServer(fx.served);
  await new Promise((r) => server.listen(0, "127.0.0.1", r));
  const base = "http://127.0.0.1:" + server.address().port + "/";

  // ---- the list shows the fork pull request with its chip ----
  let ctx = GS.newContext(base);
  setHash("#/prs");
  await GS.route(ctx);
  await settle(() => findClass(viewNode, "card").length > 0);
  const cards = findClass(viewNode, "card");
  ok("the list shows the fork pull request", cards.length === 1 && text(cards[0]).indexOf("Add a third note") !== -1, text(viewNode).slice(0, 160));
  const chips = findClass(cards[0] || viewNode, "chip").map(textOf);
  ok("the row carries the fork chip", chips.indexOf("fork") !== -1, chips.join(" | "));
  ok("the row meta names the fork URL", text(findClass(cards[0] || viewNode, "meta")[0]).indexOf(FORK_URL) !== -1, text(cards[0]));
  ok("the Open filter counts it", findClass(viewNode, "filter-chip").map(textOf).indexOf("Open 1") !== -1, findClass(viewNode, "filter-chip").map(textOf).join(" | "));
  ok("the list reads no pack", !log.some((u) => u.indexOf(fx.packName) !== -1), log.filter((u) => u.indexOf(base) === 0).join(" | "));

  // ---- the fork head diffs against the base ----
  ctx = GS.newContext(base);
  setHash(GS.commitRef(fx.pr, "gitmsg/review"));
  const before = log.length;
  await GS.route(ctx);
  await settle(() => findClass(viewNode, "diff-line").length > 0 || findClass(viewNode, "notice").length > 0);
  ok("the detail renders the pull request body from the fork pack", text(viewNode).indexOf("One more line for the notes.") !== -1, text(viewNode).slice(0, 200));
  const section = findClass(viewNode, "diff-section")[0];
  ok("the detail carries a Files changed section with no notice", !!section && findClass(section, "notice").length === 0, section ? text(section) : text(viewNode).slice(0, 200));
  ok("the Files changed section lists notes.txt", findClass(section || viewNode, "diff-path").map(textOf).join() === "notes.txt");
  const lines = findClass(section || viewNode, "diff-line").map((l) => textOf(findClass(l, "dl-sign")[0]) + textOf(findClass(l, "dl-text")[0]));
  ok("the diff shows the fork's added line", lines.indexOf("+three") !== -1 && lines.indexOf("-one") === -1, lines.join(" | "));
  ok("exact tips with a merge base carry no caveat", findClass(section || viewNode, "caveat").length === 0);
  const reads = log.slice(before);
  ok("the head reads from the fork pack", reads.some((u) => u.indexOf(".gitsocial/forks/pack/" + fx.packName) !== -1), reads.join(" | "));
  ok("the base reads from the bucket, not the fork area", reads.some((u) => /\/objects\/[0-9a-f]{2}\/[0-9a-f]{38}$/.test(u)) && !reads.some((u) => u.indexOf("objects/pack/" + fx.packName) !== -1), reads.join(" | "));
  const metaChips = findClass(findClass(viewNode, "detail-meta")[0] || viewNode, "chip").map(textOf);
  ok("the detail meta carries the fork chip", metaChips.indexOf("fork") !== -1, metaChips.join(" | "));

  // ---- a bucket with no fork area lists its own pull requests, no more ----
  ctx = GS.newContext(ORIGIN + "/thread-demo/");
  setHash("#/prs");
  await GS.route(ctx);
  await settle(() => findClass(viewNode, "card").length > 0);
  const own = findClass(viewNode, "card");
  ok("a bucket with no fork area lists its own pull requests", own.length > 0 && !own.some((c) => findClass(c, "chip").map(textOf).indexOf("fork") !== -1), own.map(text).join(" | ").slice(0, 200));

  server.close();
  fs.rmSync(root, { recursive: true, force: true });
  console.log("\n" + pass + " passed, " + fail + " failed");
  process.exit(fail ? 1 : 0);
}
main().catch((e) => { console.error(e); console.log("\n" + pass + " passed, " + (fail + 1) + " failed"); process.exit(1); });

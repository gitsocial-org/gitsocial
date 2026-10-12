// unit_render_detail.js - shim-rendered units for the detail routes, over an
// object store seeded in the read context: the version picker and its diff, the
// share control, the pm relation sections, release assets, the PR Changes
// section with its notices, and the config page.
require("./shim.js");
const GS = require("../assets/gs-render.js");
const { textOf, findTag } = global.__shim;
let pass = 0, fail = 0;
function eq(a, b, msg) { if (JSON.stringify(a) === JSON.stringify(b)) { pass++; } else { fail++; console.log("FAIL", msg, "got", JSON.stringify(a), "want", JSON.stringify(b)); } }
function ok(c, msg, extra) { if (c) { pass++; } else { fail++; console.log("FAIL", msg, extra == null ? "" : extra); } }
function findClass(node, cls, out) { out = out || []; for (const c of (node && node._children) || []) { if (c && c.nodeType === 1) { if (c._cls && c._cls.has(cls)) out.push(c); findClass(c, cls, out); } } return out; }
function fire(node, ev, props) { for (const fn of (node && node._handlers && node._handlers[ev]) || []) fn(Object.assign({ preventDefault() {}, stopPropagation() {}, key: "", target: { closest() { return null; } } }, props || {})); }
const tick = (ms) => new Promise((r) => setTimeout(r, ms || 20));
const text = (node) => textOf(node).replace(/\s+/g, " ").trim();

// Nothing is served: every bucket read misses, so only the seeded objects answer.
global.fetch = async () => ({ ok: false, status: 404, headers: { get: () => null }, text: async () => "", arrayBuffer: async () => new ArrayBuffer(0) });

const enc = new TextEncoder();
const sha = (s) => (s + "0".repeat(40)).slice(0, 40);
const short = (s) => sha(s).slice(0, 12);
const ref = (s, branch) => "#commit:" + short(s) + "@gitmsg/" + branch;
const ctx = { base: "http://seeded/", objects: new Map(), refMisses: new Set(), treeExpanded: new Set(), walks: {}, packs: { names: [], packed: null, maps: new Map(), idx: new Map(), size: new Map(), windows: new Map(), hits: [] } };
// commit seeds one gitmsg commit: a message, its GitMsg header fields and a parent.
function commit(id, parent, when, message, fields) {
  const header = "GitMsg: v=\"0.1.0\" " + Object.keys(fields).map((k) => k + "=\"" + fields[k] + "\"").join(" ");
  const body = "tree " + sha("a1") + (parent ? "\nparent " + sha(parent) : "") +
    "\nauthor Ada <ada@example.com> " + when + " +0000\n\n" + message + "\n\n" + header + "\n";
  ctx.objects.set(sha(id), { type: "commit", body: enc.encode(body) });
}
commit("d1", null, 1750000000, "Cache the walk\n\nThe first version of the body.", { ext: "pm", type: "issue", state: "open", labels: "kind/bug" });
commit("d2", "d1", 1750000100, "v1.0", { ext: "pm", type: "milestone", state: "open", due: "2026-09-01" });
commit("d3", "d2", 1750000200, "Cache the walk\n\nThe second version of the body.", { ext: "pm", state: "closed", edits: ref("d1", "pm") });
commit("d4", "d3", 1750000300, "A child task", { ext: "pm", type: "issue", state: "closed", parent: ref("d1", "pm"), milestone: ref("d2", "pm") });
commit("e1", null, 1750000400, "Release notes", { ext: "release", type: "release", tag: "v1.2", version: "1.2.0", artifacts: "gitsocial-linux.tar.gz,gitsocial-mac.tar.gz", "artifact-url": "https://example.com/dl", checksums: "SHA256SUMS", sbom: "sbom.json", "signed-by": "ada@example.com", "origin-platform": "github", "origin-url": "https://example.com/releases/9", "origin-author-name": "Ada Lovelace", "origin-time": "2026-01-02T03:04:05Z" });
commit("f1", null, 1750000500, "A comment on the issue", { ext: "social", type: "comment", original: ref("d1", "pm") });
// c9 is an adopted copy, its raw message the parity fixture's, so its GitMsg-Ref names the original author and time.
const ADOPTED = require("./parity_fixtures.json").adopted;
ctx.objects.set(sha("c9"), { type: "commit", body: enc.encode("tree " + sha("a1") + "\nparent " + sha("d4") + "\nauthor " + ADOPTED.committer + " <" + ADOPTED.committerEmail + "> 1750000600 +0000\n\n" + ADOPTED.message + "\n") });
// ca edits the adopted copy, the usual case: every later change edits the copy.
commit("ca", "c9", 1750000700, "Crash on startup", { ext: "pm", state: "closed", edits: ref("c9", "pm") });
ctx.manifest = Promise.resolve({
  "refs/heads/gitmsg/pm": sha("ca"),
  "refs/heads/gitmsg/release": sha("e1"),
  "refs/heads/gitmsg/social": sha("f1"),
});

async function main() {
  console.log("=== an edited item offers its version history ===");
  const issue = (await GS.itemDetail(ctx, sha("d1"), "gitmsg/pm"))[0];
  await tick(60);
  const history = findClass(issue, "version-history")[0];
  ok(!!history, "an edited item carries a version picker");
  eq(text(findClass(history, "version-history-head")[0]), "History (2 versions)", "the picker counts the versions");
  eq(findClass(history, "version-label").map(textOf), ["current", "original"], "rows run newest first and name each version");
  ok(text(issue).indexOf("The second version of the body.") !== -1, "the detail opens on the newest version", text(issue).slice(0, 120));

  const rows = findClass(history, "version-row");
  eq(rows.length, 2, "one row per version");
  fire(rows[1], "click");
  ok(text(issue).indexOf("The first version of the body.") !== -1, "selecting the original repaints the body", text(issue).slice(0, 120));
  ok(rows[1]._cls.has("active"), "the selected row is marked active");
  ok(!rows[0]._cls.has("active"), "the other row loses the mark");
  fire(rows[0], "keydown", { key: "Enter" });
  ok(text(issue).indexOf("The second version of the body.") !== -1, "Enter on a row selects it too");
  fire(rows[1], "click", { target: { closest: (s) => (s === "button" ? {} : null) } });
  ok(!rows[1]._cls.has("active"), "a click that landed on a row button does not select");

  console.log("=== the per-version diff ===");
  const diffBtn = findClass(history, "version-diff-btn")[0];
  eq(textOf(diffBtn), "diff to previous", "the newest row offers a diff against the one before it");
  fire(diffBtn, "click");
  const pane = findClass(history, "version-diff-pane").find((p) => p._children.length);
  ok(!!pane, "clicking opens the diff pane");
  const delta = findClass(pane, "version-delta-row").map(text);
  ok(delta.some((d) => d.indexOf("state") === 0 && d.indexOf("open") !== -1 && d.indexOf("closed") !== -1), "a changed header field reads from the old value to the new", delta.join(" | "));
  ok(delta.every((d) => d.indexOf("edits") !== 0 && d.indexOf("v ") !== 0), "the version and edits fields stay out of the delta", delta.join(" | "));
  const diffText = findClass(pane, "diff-line").map((l) => textOf(findClass(l, "dl-sign")[0]) + textOf(findClass(l, "dl-text")[0]));
  ok(diffText.indexOf("-The first version of the body.") !== -1, "the body diff shows the old line", diffText.join(" | "));
  ok(diffText.indexOf("+The second version of the body.") !== -1, "the body diff shows the new line", diffText.join(" | "));
  eq(textOf(diffBtn), "hide diff", "the control names what a second click does");
  fire(diffBtn, "click");
  eq(findClass(history, "version-diff-pane").filter((p) => p._children.length).length, 0, "the second click closes the pane");
  eq(textOf(diffBtn), "diff to previous", "and restores the label");

  console.log("=== the share control ===");
  const share = findClass(issue, "share-link")[0];
  ok(!!share, "a detail offers a copy-link control");
  ctx.siteCustomization = null;
  global.__copied = null;
  fire(share, "click");
  await tick();
  eq(global.__copied, ctx.base + "#commit:" + short("d1") + "@gitmsg/pm", "with no published pages the in-app route is copied");
  eq(textOf(share), "Copied", "the control reports the copy");
  ctx.siteCustomization = { pages: "true", url: "https://example.com" };
  fire(share, "click");
  await tick();
  eq(global.__copied, "https://example.com/i/" + short("d1") + ".html", "with pages published the item page URL is copied");
  ctx.siteCustomization = { pages: "false", url: "https://example.com/" };
  fire(share, "click");
  await tick();
  eq(global.__copied, ctx.base + "#commit:" + short("d1") + "@gitmsg/pm", "pages off falls back to the in-app route");
  ctx.siteCustomization = null;

  console.log("=== the issue's relations, sub-issues and thread ===");
  const issueText = text(issue);
  ok(issueText.indexOf("Sub-issues (0 open, 1 closed)") !== -1, "the sub-issue section counts open and closed children", issueText);
  eq(findClass(issue, "pm-progress-label").map(textOf), ["1 closed of 1"], "the progress line reads closed of total");
  ok(findClass(issue, "pm-bar-fill")[0].getAttribute("style") === "width:100%", "the bar fills to the closed share");
  eq(findClass(issue, "pm-member").map((m) => textOf(findTag(m, "a")[0])), ["A child task"], "the child issue is listed by subject");
  ok(issueText.indexOf("Comments (1)") !== -1, "the thread section counts the comments", issueText);
  ok(issueText.indexOf("A comment on the issue") !== -1, "the comment body renders", issueText);

  console.log("=== a milestone lists its members ===");
  const milestone = (await GS.itemDetail(ctx, sha("d2"), "gitmsg/pm"))[0];
  await tick(60);
  const msText = text(milestone);
  ok(msText.indexOf("Linked Issues (1)") !== -1, "a milestone heads its member list with a count", msText);
  eq(findClass(milestone, "pm-member").map((m) => textOf(findTag(m, "a")[0])), ["A child task"], "the member issue is listed");
  eq(findClass(milestone, "pm-progress-label").map(textOf), ["1 closed of 1"], "the milestone carries the same progress line");

  console.log("=== a release lists its assets ===");
  const release = (await GS.itemDetail(ctx, sha("e1"), "gitmsg/release"))[0];
  await tick(60);
  const assets = findClass(release, "assets")[0];
  ok(!!assets, "a release detail carries an assets section");
  eq(findClass(assets, "asset-row").map((r) => textOf(r)),
    ["gitsocial-linux.tar.gz", "gitsocial-mac.tar.gz", "SHA256SUMSchecksums", "sbom.jsonSBOM"],
    "each artifact, the checksum file and the SBOM get a row, the last two tagged");
  eq(findClass(assets, "asset-row").map((r) => r.getAttribute("href")),
    ["https://example.com/dl/gitsocial-linux.tar.gz", "https://example.com/dl/gitsocial-mac.tar.gz", "https://example.com/dl/SHA256SUMS", "https://example.com/dl/sbom.json"],
    "a row with a resolvable URL is a link under the artifact base");
  ok(text(assets).indexOf("signed-by ada@example.com") !== -1, "the signing identity is shown", text(assets));
  const bare = GS.releaseCard({ commit: { hash: sha("e2"), short: short("e2"), authorTime: 1, refs: [] }, header: { type: "release", tag: "v9" }, content: "v9", effectiveTime: 1, author: "Ada" });
  ok(!!bare, "a release with no assets still renders its card");

  console.log("=== the tags page reads the tags artifact ===");
  // tagsCtx is a read context over the seeded objects with two tags and the given tags artifact.
  const tagsCtx = (doc) => Object.assign({}, ctx, { walks: {}, siteTags: doc, manifest: Promise.resolve({ "refs/heads/gitmsg/release": sha("e1"), "refs/tags/v1.2": sha("b2"), "refs/tags/v1.1": sha("b1") }) });
  const tagEntry = (name, id, count, prev) => ({ name, sha: sha(id), commit: sha(id), time: 1750000400, author: "Ada", email: "ada@example.com", prev, count });
  const tagRows = (nodes) => nodes.flatMap((n) => findClass(n, "card"));
  const tagsPage = await GS.tagsView(tagsCtx({ version: 1, tags: [tagEntry("v1.2", "b2", 3, "v1.1"), tagEntry("v1.1", "b1", 1)] }));
  eq(tagRows(tagsPage).map((r) => textOf(findClass(r, "subject")[0])), ["v1.2", "v1.1"], "the rows follow the artifact order");
  eq(tagRows(tagsPage).flatMap((r) => findClass(r, "chip")).length, 0, "the page paints before the release chips");
  ok(/Ada.*3 commits/.test(text(tagRows(tagsPage)[0])), "a covered tag shows its author, date and commit count", text(tagRows(tagsPage)[0]));
  await tick(60);
  const chip = findClass(tagRows(tagsPage)[0], "chip")[0];
  ok(chip && textOf(chip) === "release" && chip.getAttribute("href") === "#commit:" + sha("e1") + "@gitmsg/release", "a tag a release names links to the release", chip && chip.getAttribute("href"));
  eq(findClass(tagRows(tagsPage)[1], "chip").length, 0, "a tag no release names gets no chip");
  const brokenPage = await GS.tagsView(tagsCtx({ version: 1, tags: [tagEntry("v1.2", "b2", 3, "v1.15"), tagEntry("v1.15", "b3", 2, "v1.1"), tagEntry("v1.1", "b1", 1)] }));
  ok(/Ada/.test(text(tagRows(brokenPage)[0])) && !/commits/.test(text(tagRows(brokenPage)[0])), "a count whose previous tag is not the next row is not shown", text(tagRows(brokenPage)[0]));
  const stalePage = await GS.tagsView(tagsCtx({ version: 1, tags: [tagEntry("v1.2", "b2", 3, "v1.1"), tagEntry("v1.1", "c1", 1)] }));
  ok(/Ada/.test(text(tagRows(stalePage)[0])) && !/commits/.test(text(tagRows(stalePage)[0])), "a partial artifact keeps the fresh author and date but no count", text(tagRows(stalePage)[0]));
  ok(!/Ada/.test(text(tagRows(stalePage)[1])), "a stale tags entry falls back to the bare row", text(tagRows(stalePage)[1]));

  console.log("=== the trailer list ===");
  const keys = (node) => findTag(node, "dt").map(textOf);
  const fresh = (await GS.itemDetail(ctx, sha("d1"), "gitmsg/pm"))[0];
  await tick(60);
  eq(keys(fresh).filter((k) => ["ext", "type", "state"].indexOf(k) !== -1), [], "the route's ext and type and the head's state stay out");
  ok(keys(fresh).indexOf("labels") !== -1, "a field no other component carries stays", keys(fresh).join(","));
  eq(keys(release), ["origin"], "a release lists one row: its head, chips and assets section carry the rest, the origin fields fold into one");
  eq(findTag(findTag(release, "dd")[0], "a").map((a) => a.getAttribute("href")), ["https://example.com/releases/9"],
    "the origin row links to the imported item");

  console.log("=== an adopted copy's detail ===");
  const adopted = (await GS.itemDetail(ctx, sha("c9"), "gitmsg/pm"))[0];
  await tick(60);
  const meta = findClass(adopted, "detail-meta")[0];
  eq(findClass(meta, "author").map((n) => textOf(n) + " " + n.getAttribute("title")), [ADOPTED.expectAuthor + " alice@example.com"], "the edited copy names the original author and their email, not the editor's");
  eq(findClass(meta, "adopted").map(textOf), [ADOPTED.expectAdopted], "the detail names the repository it came from, as a row does");
  fire(findClass(adopted, "version-row")[1], "click");
  const when = findClass(meta, "reltime").map((n) => n.getAttribute("title")).join();
  ok(when.startsWith("2025-01-"), "the detail shows the original time, not the copy's June commit", when);
  eq(keys(adopted).indexOf("adopts"), -1, "the meta row carries adopts, so no field row repeats it");

  console.log("=== a pull request's Changes section ===");
  // A two-commit code history: base ab holds notes.txt, head ac adds a line to it.
  const hexBytes = (h) => { const b = new Uint8Array(20); for (let i = 0; i < 20; i++) b[i] = parseInt(h.slice(i * 2, i * 2 + 2), 16); return b; };
  const blob = (id, s) => ctx.objects.set(sha(id), { type: "blob", body: enc.encode(s) });
  // tree seeds one git tree object from [mode, name, target id] entries.
  function tree(id, entries) {
    const parts = entries.flatMap(([mode, name, target]) => [enc.encode(mode + " " + name + "\0"), hexBytes(sha(target))]);
    const body = new Uint8Array(parts.reduce((n, p) => n + p.length, 0));
    let off = 0;
    for (const p of parts) { body.set(p, off); off += p.length; }
    ctx.objects.set(sha(id), { type: "tree", body });
  }
  blob("fa", "hello\n");
  blob("fb", "hello\nworld\n");
  tree("ta", [["100644", "notes.txt", "fa"]]);
  tree("tb", [["100644", "notes.txt", "fb"]]);
  const codeCommit = (id, parent, treeId, when, msg) => ctx.objects.set(sha(id), { type: "commit", body: enc.encode("tree " + sha(treeId) + (parent ? "\nparent " + sha(parent) : "") + "\nauthor Ada <ada@example.com> " + when + " +0000\n\n" + msg + "\n") });
  codeCommit("ab", null, "ta", 1750000000, "base");
  codeCommit("ac", "ab", "tb", 1750000100, "head");
  commit("aa", null, 1750001000, "Add world", { ext: "review", type: "pull-request", state: "open", base: "#branch:main", "base-tip": short("ab"), head: "#branch:feature", "head-tip": short("ac") });
  commit("ae", "aa", 1750001100, "From a fork", { ext: "review", type: "pull-request", state: "open", base: "#branch:main", "base-tip": short("ab"), head: "https://example.com/fork#branch:feature", "head-tip": short("ac") });
  commit("af", "ae", 1750001200, "A draft with no tips", { ext: "review", type: "pull-request", state: "open", draft: "true", base: "#branch:main", head: "#branch:feature" });
  const prCtx = Object.assign({}, ctx, { walks: {}, refMisses: new Set(), manifest: Promise.resolve({
    "refs/heads/gitmsg/review": sha("af"), "refs/heads/main": sha("ab"), "refs/heads/feature": sha("ac"),
  }) });
  const pr = (await GS.itemDetail(prCtx, sha("aa"), "gitmsg/review"))[0];
  await tick(80);
  const prDiff = findClass(pr, "diff-section")[0];
  ok(!!prDiff, "a PR whose tips resolve in the bucket gains a Changes section");
  eq(findClass(prDiff, "subject").map(textOf), ["Files changed (1)"], "the section counts the changed files");
  eq(findClass(prDiff, "diff-path").map(textOf), ["notes.txt"], "the changed file is listed by path");
  const prLines = findClass(prDiff, "diff-line").map((l) => textOf(findClass(l, "dl-sign")[0]) + textOf(findClass(l, "dl-text")[0]));
  ok(prLines.indexOf("+world") !== -1, "the diff body shows the added line", prLines.join(" | "));
  eq(findClass(prDiff, "caveat").map(textOf), [], "exact tips with a merge base carry no caveat chips");
  eq(findClass(prDiff, "notice").length, 0, "a resolved diff shows no notice");

  const fpr = (await GS.itemDetail(prCtx, sha("ae"), "gitmsg/review"))[0];
  await tick(80);
  const fSec = findClass(fpr, "diff-section")[0];
  ok(!!fSec && findClass(fSec, "notice").length === 1, "a foreign head keeps the section with one notice");
  ok(text(fSec).indexOf("not present in this bucket") !== -1, "the notice names the unresolved tips", text(fSec));
  eq(findClass(fSec, "diff-file").length, 0, "no file rows render under the notice");

  const dpr = (await GS.itemDetail(prCtx, sha("af"), "gitmsg/review"))[0];
  await tick(80);
  const dSec = findClass(dpr, "diff-section")[0];
  ok(!!dSec, "a PR with no recorded tips shows the section in place of a blank");
  ok(text(dSec).indexOf("records no base or head tip") !== -1, "the notice says the tips are not recorded", text(dSec));

  commit("ad", "af", 1750001300, "Merged long ago", { ext: "review", type: "pull-request", state: "merged", base: "#branch:main", "merge-base": "0000abc", "merge-head": "0000def" });
  const mCtx = Object.assign({}, prCtx, { walks: {}, refMisses: new Set(), manifest: Promise.resolve({
    "refs/heads/gitmsg/review": sha("ad"), "refs/heads/main": sha("ab"), "refs/heads/feature": sha("ac"),
  }) });
  const mpr = (await GS.itemDetail(mCtx, sha("ad"), "gitmsg/review"))[0];
  await tick(80);
  const mSec = findClass(mpr, "diff-section")[0];
  ok(!!mSec && text(mSec).indexOf("recorded merge range does not resolve") !== -1,
    "a merged PR with an unresolvable range says so, not that nothing was recorded", text(mSec));

  console.log("=== merged range resolves from the packs ===");
  // packIdx builds a v2 pack index over full shas, each at a fake offset.
  function packIdx(shas) {
    const sorted = shas.slice().sort(), n = sorted.length;
    const buf = new Uint8Array(8 + 1024 + n * 28 + 40);
    const dv = new DataView(buf.buffer);
    dv.setUint32(0, 0xff744f63); dv.setUint32(4, 2);
    for (let b = 0, i = 0; b < 256; b++) { while (i < n && parseInt(sorted[i].slice(0, 2), 16) <= b) i++; dv.setUint32(8 + b * 4, i); }
    sorted.forEach((s, i) => { buf.set(hexBytes(s), 1032 + i * 20); dv.setUint32(1032 + n * 24 + i * 4, 12 + i * 100); });
    return buf;
  }
  // idxFetch serves one pack index by Range and hands every other key to miss.
  const idxFetch = (name, buf, miss) => async (url, opts) => {
    if (!String(url).endsWith("objects/pack/" + name + ".idx")) return miss(url, opts);
    const m = /bytes=(\d+)-(\d*)/.exec((opts && opts.headers && opts.headers.Range) || "");
    const start = m ? Number(m[1]) : 0, end = m && m[2] ? Math.min(Number(m[2]) + 1, buf.length) : buf.length;
    const body = buf.slice(start, end);
    return { ok: true, status: 206, headers: { get: (h) => (h === "Content-Range" ? "bytes " + start + "-" + (end - 1) + "/" + buf.length : null) }, arrayBuffer: async () => body.buffer };
  };
  // A squash merge: 1a is the squash commit on main, 1b the head commit that survives in a pack and on no branch.
  codeCommit("1a", "ab", "tb", 1750000200, "Add world (squash)");
  codeCommit("1b", "ab", "tb", 1750000150, "head of the squashed branch");
  commit("1c", "ad", 1750001400, "Squash merged", { ext: "review", type: "pull-request", state: "merged", base: "#branch:main", "merge-base": short("ab"), "merge-head": short("1b") });
  const freshPacks = (names) => ({ names, packed: null, maps: new Map(), idx: new Map(), size: new Map(), windows: new Map(), hits: [] });
  const packCtx = Object.assign({}, prCtx, { walks: {}, refMisses: new Set(), packs: freshPacks(["pack-1"]), manifest: Promise.resolve({
    "refs/heads/gitmsg/review": sha("1c"), "refs/heads/main": sha("1a"),
  }) });
  const miss = global.fetch;
  global.fetch = idxFetch("pack-1", packIdx([sha("1b")]), miss);
  const ppr = (await GS.itemDetail(packCtx, sha("1c"), "gitmsg/review"))[0];
  await tick(80);
  global.fetch = miss;
  const pSec = findClass(ppr, "diff-section")[0];
  ok(!!pSec && findClass(pSec, "notice").length === 0, "a merge head in a pack and on no branch resolves", pSec && text(pSec));
  eq(findClass(pSec, "diff-path").map(textOf), ["notes.txt"], "the squashed range lists its changed file");

  console.log("=== changes follow the version picker ===");
  // The head branch grows by one commit between the versions: ab, ac (+world), then 1d (+again).
  blob("fc", "hello\nworld\nagain\n");
  tree("tc", [["100644", "notes.txt", "fc"]]);
  codeCommit("1d", "ac", "tc", 1750000250, "again");
  commit("1e", "1c", 1750001500, "Add world and again", { ext: "review", type: "pull-request", state: "open", base: "#branch:main", "base-tip": short("ab"), head: "#branch:feature", "head-tip": short("ac") });
  commit("1f", "1e", 1750001600, "Add world and again", { ext: "review", "head-tip": short("1d"), edits: ref("1e", "review") });
  commit("2a", "1f", 1750001700, "Rebased since", { ext: "review", type: "pull-request", state: "open", base: "#branch:main", "base-tip": short("ab"), head: "#branch:feature", "head-tip": "0000abc" });
  commit("2b", "2a", 1750001800, "Rebased since", { ext: "review", "head-tip": short("1d"), edits: ref("2a", "review") });
  const vCtx = Object.assign({}, prCtx, { walks: {}, refMisses: new Set(), manifest: Promise.resolve({
    "refs/heads/gitmsg/review": sha("2b"), "refs/heads/main": sha("ab"), "refs/heads/feature": sha("1d"),
  }) });
  const signs = (sec) => findClass(sec, "diff-line").map((l) => textOf(findClass(l, "dl-sign")[0]) + textOf(findClass(l, "dl-text")[0]));
  const vpr = (await GS.itemDetail(vCtx, sha("1e"), "gitmsg/review"))[0];
  await tick(80);
  const vSections = () => findClass(vpr, "diff-section");
  ok(vSections().length === 1 && signs(vSections()[0]).indexOf("+again") !== -1, "the first paint shows the latest version's range", signs(vSections()[0] || GS.el("div")).join(" | "));
  const vRows = findClass(vpr, "version-row");
  fire(vRows[1], "click");
  await tick(80);
  ok(vSections().length === 1, "selecting a version replaces the section, it does not add one", vSections().length);
  ok(signs(vSections()[0]).indexOf("+world") !== -1 && signs(vSections()[0]).indexOf("+again") === -1, "the original shows its own range", signs(vSections()[0]).join(" | "));
  fire(vRows[0], "click");
  await tick(80);
  ok(signs(vSections()[0]).indexOf("+again") !== -1, "selecting the current version brings its range back", signs(vSections()[0]).join(" | "));

  console.log("=== absent version tips keep the notice ===");
  const apr = (await GS.itemDetail(vCtx, sha("2a"), "gitmsg/review"))[0];
  await tick(80);
  const aSections = () => findClass(apr, "diff-section");
  ok(aSections().length === 1 && findClass(aSections()[0], "notice").length === 0, "the latest version resolves", text(aSections()[0] || GS.el("div")));
  const aRows = findClass(apr, "version-row");
  fire(aRows[1], "click");
  await tick(80);
  ok(aSections().length === 1 && text(aSections()[0]).indexOf("not present in this bucket") !== -1, "a version whose head tip is in no branch, index or pack shows the notice", text(aSections()[0]));
  eq(findClass(aSections()[0], "diff-file").length, 0, "no file rows render under the notice");
  fire(aRows[0], "click");
  await tick(80);
  ok(findClass(aSections()[0], "notice").length === 0 && signs(aSections()[0]).indexOf("+again") !== -1, "the other version still resolves", text(aSections()[0]));

  console.log("=== merged range resolves through the merged head ref ===");
  // A squash merge of a fork head: 2c is the squash commit on main, 2d the head commit that only the merged head ref reaches.
  codeCommit("2c", "ab", "tb", 1750000300, "Add world (squash)");
  codeCommit("2d", "ab", "tb", 1750000260, "head of the fork branch");
  commit("2e", "2b", 1750001900, "Squash merged from a fork", { ext: "review", type: "pull-request", state: "merged", base: "#branch:main", "merge-base": short("ab"), "merge-head": short("2d") });
  const refCtx = (extra) => Object.assign({}, prCtx, { walks: {}, refMisses: new Set(), manifest: Promise.resolve(Object.assign({
    "refs/heads/gitmsg/review": sha("2e"), "refs/heads/main": sha("2c"),
  }, extra)) });
  const rpr = (await GS.itemDetail(refCtx({ ["refs/gitmsg/review/merged/" + short("2e")]: sha("2d") }), sha("2e"), "gitmsg/review"))[0];
  await tick(80);
  const rSec = findClass(rpr, "diff-section")[0];
  ok(!!rSec && findClass(rSec, "notice").length === 0, "a merge head that only the merged head ref reaches resolves", rSec && text(rSec));
  eq(findClass(rSec, "diff-path").map(textOf), ["notes.txt"], "the squashed fork range lists its changed file");
  const spr = (await GS.itemDetail(refCtx({ ["refs/gitmsg/review/merged/" + short("2e")]: sha("2c") }), sha("2e"), "gitmsg/review"))[0];
  await tick(80);
  ok(text(findClass(spr, "diff-section")[0]).indexOf("recorded merge range does not resolve") !== -1, "a ref whose sha is not the recorded merge head does not stand in for it", text(findClass(spr, "diff-section")[0]));

  console.log("=== merged range without the ref ===");
  // A merge commit keeps the head as its second parent, so the walk of main finds it without the ref.
  ctx.objects.set(sha("2f"), { type: "commit", body: enc.encode("tree " + sha("tb") + "\nparent " + sha("ab") + "\nparent " + sha("2d") + "\nauthor Ada <ada@example.com> 1750000300 +0000\n\nMerge the fork branch\n") });
  const wpr = (await GS.itemDetail(refCtx({ "refs/heads/main": sha("2f") }), sha("2e"), "gitmsg/review"))[0];
  await tick(80);
  const wSec = findClass(wpr, "diff-section")[0];
  ok(!!wSec && findClass(wSec, "notice").length === 0 && findClass(wSec, "diff-path").map(textOf).join() === "notes.txt", "a merged PR with no ref resolves through the walk as before", wSec && text(wSec));
  const npr = (await GS.itemDetail(refCtx({}), sha("2e"), "gitmsg/review"))[0];
  await tick(80);
  ok(text(findClass(npr, "diff-section")[0]).indexOf("recorded merge range does not resolve") !== -1, "a squash merge with no ref keeps the notice", text(findClass(npr, "diff-section")[0]));

  console.log("=== the loading spinner ===");
  const sp = GS.spinner();
  ok(sp._cls.has("spinner") && sp.getAttribute("role") === "status" && sp.getAttribute("aria-label") === "Loading", "the spinner names itself to screen readers");
  const cd = (await GS.commitDetail(ctx, sha("d3"), ""))[0];
  await tick(60);
  ok(findClass(cd, "changes-pending").length === 0, "the pending spinner goes when the changes section paints", text(cd));
  ok(findClass(cd, "diff-section").length === 1, "the changes section stands in its place");
  // boomCtx refuses the tree read, so the changes producer fails after the base detail painted.
  const boomCtx = Object.assign({}, ctx, { objects: {
    has: (k) => { if (k === sha("a1")) throw new Error("no tree reads"); return ctx.objects.has(k); },
    get: (k) => ctx.objects.get(k), set: () => {}, delete: () => {},
  } });
  const cdBoom = (await GS.commitDetail(boomCtx, sha("d3"), ""))[0];
  ok(findClass(cdBoom, "changes-pending").length === 1 && findClass(cdBoom, "spinner").length === 1, "the spinner holds the changes section's place at first paint");
  await tick(60);
  ok(findClass(cdBoom, "changes-pending").length === 0 && findClass(cdBoom, "diff-section").length === 0, "a failed changes producer removes the spinner");
  const holdRoot = GS.el("div", {}, []);
  const hold = GS.el("div", { class: "changes-pending" }, [GS.spinner()]);
  holdRoot.append(hold);
  GS.enrichDetail(holdRoot, async () => null, (n) => { if (n) hold.replaceWith(n); else hold.remove(); });
  await tick(20);
  ok(findClass(holdRoot, "changes-pending").length === 0, "a null producer removes the spinner through its place");
  const paged = GS.pagedListView({ items: [], truncated: true }, () => {}, () => new Promise(() => {}))[0];
  const more = findClass(paged, "load-more")[0];
  fire(more, "click");
  const btnSp = findClass(more, "spinner")[0];
  ok(more.disabled === true && !!btnSp && btnSp.getAttribute("role") === "status" && btnSp.getAttribute("aria-label") === "Loading", "a busy button disables behind an announced spinner");

  console.log("=== the configuration page ===");
  document.body._cls = new Set();
  const config = (await GS.configView(ctx))[0];
  const prefRows = findClass(config, "pref-row").map(text);
  eq(prefRows, ["Themelight", "Layout widthfixed", "Sidebar (desktop)expanded", "Diff viewunified"], "each reader preference shows its current value");
  const prefBtns = findClass(config, "pref-btn");
  const clicked = [];
  for (const id of ["theme-toggle", "width-toggle", "nav-collapse", "nav-handle"]) document.getElementById(id).click = () => clicked.push(id);
  fire(prefBtns[0], "click");
  fire(prefBtns[1], "click");
  fire(prefBtns[2], "click");
  eq(clicked, ["theme-toggle", "width-toggle", "nav-collapse"], "a preference button drives the header control it mirrors");
  fire(prefBtns[3], "click");
  eq(localStorage.getItem("diffview"), "split", "the diff preference writes the same key the diff toggle does");
  eq(textOf(prefBtns[3]), "split", "and the button reports the new value");
  document.body._cls = new Set(["nav-collapsed"]);
  fire(prefBtns[2], "click");
  eq(clicked[3], "nav-handle", "a collapsed sidebar toggles through the handle instead");
  ok(text(config).indexOf("Repository configuration") !== -1, "the repository configuration section renders", text(config));
  eq(findClass(config, "config-ext-head").map(textOf), ["social", "pm", "review", "release", "memo"], "every extension gets a config card");
  eq(findClass(config, "meta").map(textOf).filter((t) => t === "defaults").length, 5, "an extension with no pushed config reads as defaults");

  console.log("\n" + pass + " passed, " + fail + " failed");
  process.exit(fail ? 1 : 0);
}
main().catch((e) => { console.error(e); console.log("\n" + pass + " passed, " + (fail + 1) + " failed"); process.exit(1); });

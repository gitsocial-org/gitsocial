// unit_render_detail.js - shim-rendered units for the detail routes, over an
// object store seeded in the read context: the version picker and its diff, the
// share control, the pm relation sections, release assets and the config page.
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
const ctx = { base: "http://seeded/", objects: new Map(), refMisses: new Set(), treeExpanded: new Set(), walks: {}, packs: { names: [], packed: null, maps: new Map(), idx: new Map(), size: new Map(), windows: new Map(), lastHit: null } };
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
ctx.manifest = Promise.resolve({
  "refs/heads/gitmsg/pm": sha("d4"),
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

  console.log("=== the trailer list ===");
  const keys = (node) => findTag(node, "dt").map(textOf);
  const fresh = (await GS.itemDetail(ctx, sha("d1"), "gitmsg/pm"))[0];
  await tick(60);
  eq(keys(fresh).filter((k) => ["ext", "type", "state"].indexOf(k) !== -1), [], "the route's ext and type and the head's state stay out");
  ok(keys(fresh).indexOf("labels") !== -1, "a field no other component carries stays", keys(fresh).join(","));
  eq(keys(release), ["origin"], "a release lists one row: its head, chips and assets section carry the rest, the origin fields fold into one");
  eq(findTag(findTag(release, "dd")[0], "a").map((a) => a.getAttribute("href")), ["https://example.com/releases/9"],
    "the origin row links to the imported item");

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

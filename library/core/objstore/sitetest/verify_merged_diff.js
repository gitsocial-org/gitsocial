// verify_merged_diff.js - the merged-PR diff path (prDiffSection's
// merge-base..merge-head reconstruction). merged-demo has one PR merged with a
// real merge commit whose head branch was deleted and never published, so its
// head tip survives only as the merge commit's SECOND parent on main. The
// base-tip/head-tip path can't resolve the head here (the head branch ref is
// absent from the bucket), so a rendered "Files changed" diff — not the graceful
// "tips not present" fallback — proves the merged range was reconstructed.
require("./shim.js");
require("../site/icons.js");
const GS = require("../site/gs-app.js");
const { viewNode, textOf, setHash } = global.__shim;
let pass = 0, fail = 0;
const ok = (n, c, e) => { (c ? pass++ : fail++); console.log((c ? "PASS " : "FAIL ") + n + (!c && e ? " :: " + e : "")); };
const wait = (ms) => new Promise((r) => setTimeout(r, ms));
function findClass(node, cls, out) { out = out || []; for (const c of (node && node._children) || []) { if (c && c.nodeType === 1) { if (c._cls && c._cls.has(cls)) out.push(c); findClass(c, cls, out); } } return out; }
const origin = process.env.GS_SITE_ORIGIN || "http://localhost:8000";
function done() { console.log("\n" + pass + " passed, " + fail + " failed"); process.exit(fail ? 1 : 0); }

async function main() {
  const ctx = GS.newContext(origin + "/merged-demo/");
  // Discover the PR (fixture commit hashes are non-deterministic per build).
  const items = await GS.loadExtItemsAll(ctx, "review");
  const pr = items.find((i) => i.header && i.header.type === "pull-request");
  ok("merged PR present in review items", !!pr, "no pull-request item found");
  if (!pr) return done();

  // Route to the PR detail page and let the async diff resolve. The diff is a
  // background enrichment (paints after the base detail), so poll for it
  // instead of a fixed wait — a fixed 1.5s flakes under load.
  setHash(GS.commitRef(pr.commit.hash, "gitmsg/review"));
  await GS.route(ctx);
  for (let i = 0; i < 40 && findClass(viewNode, "diff-section").length === 0; i++) await wait(250);
  const view = viewNode;

  ok("PR renders as merged", textOf(view).toLowerCase().includes("merged"), "no merged state on page");
  const sections = findClass(view, "diff-section");
  ok("a Files-changed diff section renders", sections.length >= 1, "no diff-section");
  const subj = sections.length ? findClass(sections[0], "subject").map(textOf).join(" ") : "";
  ok("section titled 'Files changed'", subj.indexOf("Files changed") === 0, subj || "(none)");
  const secText = sections.length ? textOf(sections[0]) : "";
  ok("diff covers the added CHANGELOG.md", secText.includes("CHANGELOG.md"), secText.slice(0, 160));
  // The whole point: head branch is absent, so a fallback notice would mean the
  // merge-base..merge-head reconstruction did not fire.
  ok("did not fall back to 'tips not present'", !textOf(view).includes("not present in this bucket"), "fell back to the tips notice");

  // ---- graph decoration: the deleted, merged head branch is recovered from the
  // merged-PR header (merge-head/head-tip short shas) and badged on its row as a
  // dimmed historical chip linking to the PR detail.
  setHash("#/graph");
  await GS.route(ctx);
  await wait(500);
  const mergedChips = findClass(viewNode, "merged-branch");
  ok("graph badges the merged (deleted) head branch", mergedChips.map(textOf).includes("feature/changelog"), "chips=" + mergedChips.map(textOf).join(","));
  const chipHrefs = mergedChips.map((n) => n.getAttribute("href") || "");
  ok("merged-branch chip links to the PR detail", chipHrefs.some((h) => h.includes("@gitmsg/review")), chipHrefs.join(","));

  done();
}
main().catch((e) => { console.error(e); process.exit(1); });

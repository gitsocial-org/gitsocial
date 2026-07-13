// verify_site_features.js - fixture-backed proofs for the site's feature surfaces:
//   board honors the pushed .gitsocial/site/pm-config.json
//   issue/PR cards carry enrichment chips (labels/assignee/priority/counts/origin)
//   search deep-link (#/search/<q>) initializes AND executes the query
//   a detail page's back link returns to where the user came from
//   the Tags page lists tags; a tag route resolves to the commit (annotated peeled)
//   the board group-by control renders swimlane lanes
//   the shell ships lazy-loaded grammars/prism-<lang>.js; the old bundle is gone
//   the branch compare view (#/compare:<base>...<head>) diffs three-dot and lists commits
//   the repository graph (#/graph) renders a multi-branch commit DAG with lanes
//   the push publishes .gitsocial/site/site-config.json from refs/gitmsg/core/config
//   the reader applies title/accent/accentDark/favicon overrides (strict validation)
//   tags list is version-aware descending (v1.0 before v1.0-light, non-version after)
//   annotated tag signature blocks are stripped, a "signed" chip stands in
//   graph/branch-log/commit-detail render the git AUTHOR (origin-aware), not committer
//   the config forks section caps at 10 with a "Show all N forks" expand + filter
//   memo and release cards carry the shared enrichment chip row (labels/origin)
//   board columns collapse/hide, swimlanes collapse + lane index, per-cell item cap
//   board card glyph renders inline with the subject (one title line, not its own row)
require("./shim.js");
require("../site/icons.js");
const GS = require("../site/gs-app.js");
const { viewNode, textOf, setHash } = global.__shim;
const ORIGIN = process.env.GS_SITE_ORIGIN || "http://localhost:8000";
const BASE = ORIGIN + "/thread-demo/";
let pass = 0, fail = 0;
const ok = (n, c, e) => { (c ? pass++ : fail++); console.log((c ? "PASS " : "FAIL ") + n + (!c && e ? " :: " + e : "")); };
const wait = (ms) => new Promise((r) => setTimeout(r, ms));
function findClass(node, cls, out) { out = out || []; for (const c of (node && node._children) || []) { if (c && c.nodeType === 1) { if (c._cls && c._cls.has(cls)) out.push(c); findClass(c, cls, out); } } return out; }
function findTag(node, tag, out) { out = out || []; for (const c of (node && node._children) || []) { if (c && c.nodeType === 1) { if (c.tagName.toLowerCase() === tag) out.push(c); findTag(c, tag, out); } } return out; }
function fire(node, ev, props) { (node && node._handlers && node._handlers[ev] || []).forEach((fn) => fn(Object.assign({ preventDefault() {}, stopPropagation() {}, key: "", button: 0, target: { closest() { return null; } } }, props || {}))); }
// route drives the app router with a shared ctx (so ctx.lastHash/backFrom persist
// across navigations, as they do in a real session).
let ctx = null;
async function route(hash, fresh) { if (fresh || !ctx) ctx = GS.newContext(BASE); setHash(hash); await GS.route(ctx); await wait(900); }

async function main() {
  await wait(2000); // drain the auto-run init home route before driving our own
  // ---- board reads the pushed pm-config.json ----
  const cfg = await GS.loadSiteConfig(GS.newContext(BASE));
  ok("pm-config.json is present and parsed", !!cfg && Array.isArray(cfg.columns), "cfg=" + JSON.stringify(cfg && cfg.columns && cfg.columns.map((c) => c.name)));
  await route("#/board", true);
  const colHeads = findClass(viewNode, "board-col-name").map(textOf);
  ok("board renders columns from config (kanban default)", colHeads.some((h) => h.startsWith("Backlog")) && colHeads.some((h) => h.startsWith("Done")), colHeads.join(" | "));

  // ---- board group-by swimlane control ----
  const groupBy = findClass(viewNode, "filter-chip").map(textOf);
  ok("board has a group-by control (none/priority/kind/assignees/author)", ["none", "priority", "kind", "assignees", "author"].every((f) => groupBy.includes(f)), groupBy.join(","));
  const prio = findClass(viewNode, "filter-chip").find((c) => textOf(c) === "priority");
  fire(prio, "click"); await wait(30);
  ok("grouping by priority renders lane bands", findClass(viewNode, "board-lane").length > 0 && /grouped by priority/.test(textOf(viewNode)), "lanes=" + findClass(viewNode, "board-lane").length);

  // ---- issue cards carry enrichment chips ----
  await route("#/issues");
  const chipTexts = findClass(viewNode, "card-chips").flatMap((r) => findClass(r, "chip").map(textOf));
  ok("issue list cards carry enrichment chips (labels/priority)", chipTexts.length > 0, chipTexts.slice(0, 8).join(" | "));
  ok("a kind/* label chip renders", chipTexts.some((t) => t.indexOf("kind/") === 0), chipTexts.join(" | "));

  // ---- PR card review summary (fixture PR has an approval + a change request) ----
  await route("#/prs");
  const prChips = findClass(viewNode, "card-chips").flatMap((r) => findClass(r, "chip").map(textOf));
  ok("PR card shows a review summary (✓/✗) from cross-branch feedback", prChips.some((t) => /[✓✗]/.test(t)), prChips.join(" | "));

  // ---- history-aware back link ----
  // Come from the board, open an issue, the back link points to the board.
  await route("#/board");
  const pm = await GS.loadExtItemsUpTo(GS.newContext(BASE), "pm", 800);
  const issue = pm.find((i) => ((i.header && i.header.type) || "issue") === "issue");
  await route("#commit:" + issue.commit.hash + "@gitmsg/pm");
  const back = findClass(viewNode, "back")[0];
  ok("back link returns to the board the user came from", back && (back.getAttribute("href") || "") === "#/board", back && back.getAttribute("href"));
  // Coming from the issues list instead, back points to the issues tab.
  await route("#/issues");
  await route("#commit:" + issue.commit.hash + "@gitmsg/pm");
  const back2 = findClass(viewNode, "back")[0];
  ok("back link returns to the issues list when arriving from there", back2 && (back2.getAttribute("href") || "") === "#/issues", back2 && back2.getAttribute("href"));

  // ---- Tags page + tag detail (annotated peel) ----
  await route("#/tags");
  const tagNames = findClass(viewNode, "subject").map(textOf);
  ok("tags page lists the pushed tags (v1.0 annotated, v1.0-light lightweight)", tagNames.includes("v1.0") && tagNames.includes("v1.0-light"), tagNames.join(","));
  await route("#tag:v1.0");
  ok("annotated tag v1.0 peels to its commit (detail renders, not 'unreachable')", findClass(viewNode, "detail").length > 0 && !/unreachable|not found/i.test(textOf(viewNode)), textOf(viewNode).slice(0, 80));
  ok("annotated tag shows its annotation message", /First public release/.test(textOf(viewNode)) || findClass(viewNode, "tag-annotation").length > 0, textOf(viewNode).slice(0, 80));
  // v1.0's previous tag (version order) is v1.0-light at the same commit: the
  // commits-since section renders with its empty state and a compare link.
  ok("tag page shows a commits-since-previous-tag section", /Commits since v1\.0-light/.test(textOf(viewNode)), textOf(viewNode).slice(-120));
  ok("same-commit previous tag yields the empty state", /No commits since v1\.0-light/.test(textOf(viewNode)), textOf(viewNode).slice(-120));
  ok("tag page links a compare against the previous tag", findClass(viewNode, "action-link").some((a) => (a.getAttribute("href") || "").includes("compare")), findClass(viewNode, "action-link").map((a) => a.getAttribute("href")).join(","));
  await route("#tag:v1.0-light");
  ok("lightweight tag resolves straight to its commit", findClass(viewNode, "detail").length > 0 && !/unreachable/i.test(textOf(viewNode)));
  // v1.0-light's previous tag is v0.9, two commits back on main: the commits
  // section lists that span as milestone-style member one-liners and the page
  // diffs against the previous tag.
  const tagRows = findClass(viewNode, "pm-member");
  ok("tag page lists the commits since the previous tag as member one-liners", /Commits since v0\.9 \(\d/.test(textOf(viewNode)) && tagRows.length > 0, String(tagRows.length));
  ok("tag page diffs against the previous tag", findClass(viewNode, "diff-section").length > 0 && /Files changed since v0\.9/.test(textOf(viewNode)), textOf(viewNode).slice(-120));
  await route("#tag:v0.9");
  const oldestRows = findClass(viewNode, "pm-member");
  ok("oldest tag lists its full history in the commits section", oldestRows.length > 0, String(oldestRows.length));
  ok("oldest tag has no diff section (nothing to diff against)", findClass(viewNode, "diff-section").length === 0, String(findClass(viewNode, "diff-section").length));

  // ---- search deep-link initializes and executes ----
  await route("#/search/" + encodeURIComponent("onboarding"));
  const input = findClass(viewNode, "search-input")[0];
  ok("deep-link prefills the search input", input && input.value === "onboarding", input && input.value);
  ok("deep-link executes the query on load (results shown, no help)", findClass(viewNode, "search-group-head").length > 0 || /result/.test(textOf(findClass(viewNode, "search-status")[0] || { _children: [] })), textOf(viewNode).slice(0, 80));

  // ---- shell ships lazy-loaded grammar files; the retired bundle is gone ----
  // Non-base grammars now ride as individual grammars/prism-<lang>.js files the
  // reader lazy-loads on demand (see verify_grammars.js for the reader behavior).
  // Here we only assert the transport: the grammar files are served and the old
  // push-published prism-extra.js bundle is absent (never written; cleaned up).
  const zlib = require("zlib");
  const http = require("http");
  const get = (key) => new Promise((resolve) => {
    http.get(BASE + key, (res) => {
      const chunks = [];
      res.on("data", (c) => chunks.push(c));
      res.on("end", () => {
        let buf = Buffer.concat(chunks);
        if ((res.headers["content-encoding"] || "") === "br") { try { buf = zlib.brotliDecompressSync(buf); } catch (_) {} }
        resolve({ status: res.statusCode, text: buf.toString() });
      });
    }).on("error", () => resolve({ status: 0, text: "" }));
  });
  const py = await get("grammars/prism-python.js");
  ok("grammars/prism-python.js is served by the shell", py.status === 200 && /Prism\.languages\.python=/.test(py.text), "status=" + py.status);
  const cpp = await get("grammars/prism-cpp.js");
  ok("grammars/prism-cpp.js is served (a dependency-chained grammar)", cpp.status === 200 && /extend\("c"/.test(cpp.text), "status=" + cpp.status);
  const gone = await get(".gitsocial/site/prism-extra.js");
  ok("the retired prism-extra.js bundle is not published", gone.status === 404, "status=" + gone.status);

  // ---- branch compare view (three-dot diff + head-side commit list) ----
  await route("#/compare:" + encodeURIComponent("main") + "..." + encodeURIComponent("feature/notes-expand"), true);
  ok("compare page renders base/head pickers", findClass(viewNode, "compare-select").length === 2, "selects=" + findClass(viewNode, "compare-select").length);
  ok("compare shows a Files changed diff section", /Files changed/.test(textOf(viewNode)), textOf(viewNode).slice(0, 100));
  ok("compare lists the head-side commit (Expand and edit notes)", /Commits/.test(textOf(viewNode)) && /Expand and edit notes/.test(textOf(viewNode)), textOf(viewNode).slice(0, 160));
  const diffPaths = findClass(viewNode, "diff-path").map(textOf);
  ok("compare diff includes the changed file (notes.txt)", diffPaths.some((p) => p === "notes.txt"), diffPaths.join(","));
  // Same-ref compare is an explicit empty state.
  await route("#/compare:" + encodeURIComponent("main") + "..." + encodeURIComponent("main"), true);
  ok("same-ref compare is a clear empty state", /same commit|nothing to compare/i.test(textOf(viewNode)), textOf(viewNode).slice(0, 100));
  // A missing head ref surfaces a clear message, pickers still present.
  await route("#/compare:" + encodeURIComponent("main") + "..." + encodeURIComponent("no-such-branch"), true);
  ok("missing head ref surfaces a clear message", /not found/i.test(textOf(viewNode)) && findClass(viewNode, "compare-select").length === 2, textOf(viewNode).slice(0, 100));

  // ---- repository graph (multi-branch commit DAG) ----
  await route("#/graph", true);
  ok("graph page renders the SVG lane gutter", findClass(viewNode, "graph-gutter").length === 1, "gutters=" + findClass(viewNode, "graph-gutter").length);
  const dots = findTag(findClass(viewNode, "graph-gutter")[0] || { _children: [] }, "circle");
  ok("graph draws a commit dot per row", dots.length > 0, "dots=" + dots.length);
  const rowTexts = findClass(viewNode, "graph-subject").map(textOf);
  ok("graph rows show commit subjects", rowTexts.length > 0 && rowTexts.some((t) => /notes|README|python|rust/i.test(t)), rowTexts.slice(0, 4).join(" | "));
  ok("graph labels a branch tip", findClass(viewNode, "branch-tip").length > 0, "tips=" + findClass(viewNode, "branch-tip").length);
  const defChips = findClass(viewNode, "branch-tip").filter((n) => n._cls && n._cls.has("default"));
  ok("graph marks the default branch chip as `default`", defChips.length === 1 && textOf(defChips[0]) === "main", "default chips=" + defChips.map(textOf).join(","));
  const tagChips = findClass(viewNode, "tag-tip").map(textOf);
  ok("graph badges the lightweight tag on its row", tagChips.includes("v1.0-light"), "tags=" + tagChips.join(","));
  ok("annotated tag stays unbadged (its refs.json sha is the tag object, never a row; no peel fetch)", !tagChips.includes("v1.0"), "tags=" + tagChips.join(","));
  const hashes = findClass(viewNode, "graph-row-text").flatMap((r) => findClass(r, "hash").map((h) => h.getAttribute("href") || ""));
  ok("graph short hashes link to commit detail", hashes.some((h) => /^#commit:/.test(h)), hashes.slice(0, 3).join(","));

  // ---- search includes plain code commits (subject level, Commits group) ----
  {
    const sctx = GS.newContext(BASE);
    const corpus = await GS.loadSearchWindow(sctx, false);
    const codeOf = (res) => (res.groups || []).find((g) => g.ext === "code");
    const free = GS.searchItemsFaceted("notes", corpus.perExt, null);
    const freeCode = codeOf(free);
    ok("free text matches a code commit subject (Commits group)", !!freeCode && freeCode.count > 0,
      "groups=" + (free.groups || []).map((g) => g.ext + ":" + g.count).join(","));
    const byAuthor = GS.searchItemsFaceted("author:ada@example.com notes", corpus.perExt, null);
    const authorCode = codeOf(byAuthor);
    ok("author: facet returns code commits", !!authorCode && authorCode.count > 0,
      "groups=" + (byAuthor.groups || []).map((g) => g.ext + ":" + g.count).join(","));
    const otherAuthor = GS.searchItemsFaceted("author:nobody@example.com notes", corpus.perExt, null);
    ok("author: facet excludes non-matching code commits", !codeOf(otherAuthor), "unexpected code hits");
    const anyCode = (corpus.perExt.code || [])[0];
    ok("search corpus carries a code lane", !!anyCode, "perExt.code empty");
    if (anyCode) {
      const byHash = GS.searchItemsFaceted(anyCode.commit.hash.slice(0, 10), corpus.perExt, null);
      const hashCode = codeOf(byHash);
      ok("hash-prefix query resolves a code commit", !!hashCode && hashCode.items.some((it) => it.commit.hash === anyCode.commit.hash),
        "groups=" + (byHash.groups || []).map((g) => g.ext + ":" + g.count).join(","));
    }
    ok("code lane marks the corpus light (subject-level, body-less)", corpus.light === true, "light=" + corpus.light);
  }

  // ---- push publishes site-config.json from refs/gitmsg/core/config ----
  const cust = await GS.loadSiteCustomization(GS.newContext(BASE));
  ok("site-config.json is present and parsed", !!cust && typeof cust === "object", "cust=" + JSON.stringify(cust));
  ok("site-config carries the pushed title", cust && cust.title === "Thread Demo", cust && cust.title);
  ok("site-config carries a validated accent + accentDark", cust && cust.accent === "#0a7" && cust.accentDark === "#0dd", cust && (cust.accent + "/" + cust.accentDark));
  ok("site-config carries a favicon data URI", cust && /^data:image\/png[;,]/.test(cust.favicon || ""), cust && (cust.favicon || "").slice(0, 24));

  // ---- reader applies the overrides with strict validation ----
  await GS.applySiteCustomization(GS.newContext(BASE), "fallback");
  ok("document/header title reflects the customization", document.title === "Thread Demo", document.title);
  const accentStyle = document.getElementById("gs-site-accent");
  const accentCss = accentStyle ? accentStyle.textContent : "";
  ok("accent injects --link light + dark via a scoped style", /--link:#0a7/.test(accentCss) && /--link:#0dd/.test(accentCss), accentCss.replace(/\n/g, " "));
  const icon = document.querySelector("link[rel=icon]");
  ok("favicon href points at the validated data URI", icon && /^data:image\/png[;,]/.test(icon.getAttribute("href") || ""), icon && (icon.getAttribute("href") || "").slice(0, 24));
  // Invalid values are ignored (no crash, defaults kept).
  GS.applyAccent("javascript:alert(1)", "#zzz");
  ok("invalid accent is ignored (no injection of bad literal)", !/alert|zzz/.test(document.getElementById("gs-site-accent").textContent), document.getElementById("gs-site-accent").textContent.replace(/\n/g, " "));
  const beforeHref = document.querySelector("link[rel=icon]").getAttribute("href");
  GS.applyFavicon("data:text/html,<script>1</script>");
  ok("invalid favicon is ignored (href unchanged)", document.querySelector("link[rel=icon]").getAttribute("href") === beforeHref, document.querySelector("link[rel=icon]").getAttribute("href"));

  // ---- tags list is version-aware descending ----
  const gtags = await GS.listTags(GS.newContext(BASE));
  const gnames = gtags.map((t) => t.name);
  ok("versioned tag sorts before its pre-release-ish sibling (v1.0 < v1.0-light)", gnames.indexOf("v1.0") >= 0 && gnames.indexOf("v1.0-light") >= 0 && gnames.indexOf("v1.0") < gnames.indexOf("v1.0-light"), gnames.join(","));
  ok("compareTagsDesc orders highest version first, non-version last", JSON.stringify(["v1.2", "v1.10", "v1.0", "zeta", "alpha"].map((n) => ({ name: n })).sort(GS.compareTagsDesc).map((t) => t.name)) === JSON.stringify(["v1.10", "v1.2", "v1.0", "zeta", "alpha"]));

  // ---- annotated tag signature stripped, "signed" note stands in ----
  const strip = GS.stripSignatureBlock("Real notes\n\n-----BEGIN PGP SIGNATURE-----\nZm9v\n-----END PGP SIGNATURE-----\n");
  ok("stripSignatureBlock removes the PGP block and flags signed", strip.text === "Real notes" && strip.signed === true, JSON.stringify(strip));
  ok("stripSignatureBlock leaves an unsigned message intact", GS.stripSignatureBlock("Just notes").text === "Just notes" && GS.stripSignatureBlock("Just notes").signed === false);
  await route("#tag:v1.0", true);
  ok("tag detail shows no raw PGP armor", !/BEGIN PGP SIGNATURE/.test(textOf(viewNode)), textOf(viewNode).slice(0, 60));

  // ---- raw git commit views render the AUTHOR (origin-aware), not committer ----
  // A synthetic imported commit: git author is the importer, origin-author is Ada.
  const impRaw = "Imported change\n\nGitMsg: ext=\"social\"; type=\"post\"; origin-author-name=\"Ada Origin\"; origin-author-email=\"ada@up.stream\"; v=\"0.1.0\"";
  const impCommit = { hash: "f".repeat(40), short: "ffffffffffff", authorName: "Importer Bot", authorEmail: "bot@ci", authorTime: 1000, content: "Imported change", rawMessage: impRaw, gitmsg: GS.parseGitmsg(impRaw), refs: [], parents: [] };
  const authorText = textOf(GS.commitAuthorEl(impCommit));
  ok("commitAuthorEl prefers origin author over the git committer/importer", authorText === "Ada Origin", authorText);
  const plainCommit = { hash: "e".repeat(40), short: "eeeeeeeeeeee", authorName: "Ada Author", authorEmail: "ada@dev", authorTime: 1000, content: "Local change", rawMessage: "Local change", gitmsg: null, refs: [], parents: [] };
  ok("commitAuthorEl uses the git author for a plain commit (never committer)", textOf(GS.commitAuthorEl(plainCommit)) === "Ada Author", textOf(GS.commitAuthorEl(plainCommit)));
  await route("#/graph", true);
  const graphAuthors = findClass(viewNode, "graph-meta").map(textOf).join(" ");
  ok("graph rows render an author name (Ada authored the fixture commits)", /Ada/.test(graphAuthors), graphAuthors.slice(0, 80));

  // ---- forks section caps at 10 with a "Show all" expand + filter ----
  await route("#/config", true);
  const forkToggle = findClass(viewNode, "load-more").find((b) => /Show all \d+ forks/.test(textOf(b)));
  ok("forks section shows a 'Show all N forks' expand (fixture has >10 forks)", !!forkToggle, forkToggle && textOf(forkToggle));
  const forkRowsCollapsed = findClass(viewNode, "config-section").flatMap((s) => findClass(s, "tree-row")).filter((r) => /fork-/.test(textOf(r)));
  ok("forks section renders the top 10 by default (capped)", forkRowsCollapsed.length === 10, "rows=" + forkRowsCollapsed.length);
  if (forkToggle) { fire(forkToggle, "click"); await wait(30); }
  const filterInput = findClass(viewNode, "contrib-filter").find((i) => (i.getAttribute("placeholder") || "").indexOf("fork") !== -1 || (i.getAttribute("aria-label") || "").indexOf("fork") !== -1);
  ok("expanding reveals a filter input", !!filterInput, filterInput && (filterInput.getAttribute("aria-label")));

  // ---- memo & release cards carry the shared enrichment chip row ----
  const memoItem = { commit: { hash: "a".repeat(40), short: "aaaaaaaaaaaa" }, header: { type: "memo", labels: "area/cache,kind/policy" }, content: "Cache policy\n\nbody", author: "Ada", effectiveTime: 1, versions: [] };
  const mcard = GS.memoCard(memoItem);
  const mchips = findClass(mcard, "card-chips").flatMap((r) => findClass(r, "chip").map(textOf));
  ok("memo card renders labels via the shared enrichment chip row", mchips.includes("area/cache") && mchips.includes("kind/policy"), mchips.join(" | "));
  const relItem = { commit: { hash: "b".repeat(40), short: "bbbbbbbbbbbb" }, header: { type: "release", tag: "v2.0", version: "2.0", labels: "area/build", "origin-platform": "github", "origin-url": "https://x/y" }, content: "v2.0\n\nnotes", author: "Ada", effectiveTime: 1, versions: [] };
  const rcard = GS.releaseCard(relItem);
  const rHeadChips = findClass(findClass(rcard, "card-head")[0] || { _children: [] }, "chip").map(textOf);
  ok("release card keeps its version chip in the head", rHeadChips.some((t) => t === "v2.0"), rHeadChips.join(" | "));
  const rEnrich = findClass(rcard, "card-chips").flatMap((r) => findClass(r, "chip").map(textOf));
  ok("release card renders labels + origin via the shared enrichment chip row", rEnrich.includes("area/build") && rEnrich.some((t) => /↗/.test(t)), rEnrich.join(" | "));

  // ---- board column collapse/hide + swimlane collapse + lane index ----
  await route("#/board", true);
  // Each column head carries a collapse caret and a hide control.
  ok("each board column has a collapse toggle", findClass(viewNode, "board-col-toggle").length >= 4, "toggles=" + findClass(viewNode, "board-col-toggle").length);
  ok("each board column has a hide control", findClass(viewNode, "board-col-hide").length >= 4, "hides=" + findClass(viewNode, "board-col-hide").length);
  const colsBefore = findClass(viewNode, "board-col").length;
  const hideDone = findClass(viewNode, "board-col").map((c) => ({ c, name: textOf(findClass(c, "board-col-name")[0] || { _children: [] }) })).find((x) => x.name.startsWith("Done"));
  const doneHide = hideDone && findClass(hideDone.c, "board-col-hide")[0];
  fire(doneHide, "click"); await wait(30);
  ok("hiding the Done column removes it and offers a restore chip", findClass(viewNode, "board-col").length === colsBefore - 1 && findClass(viewNode, "board-col-restore").some((r) => /Done/.test(textOf(r))), "cols=" + findClass(viewNode, "board-col").length);
  const restore = findClass(viewNode, "board-col-restore").find((r) => /Done/.test(textOf(r)));
  fire(restore, "click"); await wait(30);
  ok("restoring brings the Done column back", findClass(viewNode, "board-col").length === colsBefore, "cols=" + findClass(viewNode, "board-col").length);
  const firstToggle = findClass(viewNode, "board-col-toggle")[0];
  fire(firstToggle, "click"); await wait(30);
  ok("collapsing a column slims it (board-col-collapsed) with no cards", findClass(viewNode, "board-col-collapsed").length === 1 && findClass(findClass(viewNode, "board-col-collapsed")[0], "board-card").length === 0, "collapsed=" + findClass(viewNode, "board-col-collapsed").length);
  // Swimlanes: group by kind (fixture has multiple kinds), lane index present.
  const kindChip = findClass(viewNode, "filter-chip").find((c) => textOf(c) === "kind");
  fire(kindChip, "click"); await wait(30);
  ok("lane index lists jump links when >1 lane", findClass(viewNode, "board-lane-index").length === 1 && findClass(viewNode, "board-lane-jump").length > 1, "jumps=" + findClass(viewNode, "board-lane-jump").length);
  const laneHead = findClass(viewNode, "board-lane-head")[0];
  fire(laneHead, "click"); await wait(30);
  ok("clicking a lane header collapses it (label + count remain, grid hidden)", findClass(viewNode, "board-lane-collapsed").length >= 1 && findClass(findClass(viewNode, "board-lane-collapsed")[0], "board").length === 0);
  // Per-cell item cap: build a synthetic column of >BOARD_ITEM_CAP issues and
  // confirm boardBody caps it with a "show N more" control.
  const many = [];
  for (let i = 0; i < 12; i++) many.push({ commit: { hash: (i + "").repeat(40).slice(0, 40), short: ("i" + i) }, header: { type: "issue", state: "open" }, content: "Issue " + i, author: "Ada", effectiveTime: i, versions: [] });
  const bb = GS.boardBody ? GS.boardBody(many, null) : null;
  if (bb) {
    const backlog = findClass(bb, "board-col").find((c) => textOf(findClass(c, "board-col-name")[0] || { _children: [] }).startsWith("Backlog"));
    const cards = findClass(backlog, "board-card").length;
    ok("a long column caps visible cards (≤7) with a 'show N more'", cards <= 7 && findClass(backlog, "board-more").length === 1, "cards=" + cards);
    const more = findClass(backlog, "board-more")[0];
    fire(more, "click"); await wait(10);
    ok("'show more' expands the capped cell", findClass(bb, "board-card").filter((c) => true).length > cards || /show less/.test(textOf(findClass(bb, "board-more")[0] || { _children: [] })), textOf(findClass(bb, "board-more")[0] || { _children: [] }));
  } else ok("boardBody exported for cap test", false, "boardBody not exported");

  // ---- board card glyph is inline with the subject (one title line) ----
  const bcard = GS.boardBody ? findClass(GS.boardBody([{ commit: { hash: "c".repeat(40), short: "cc" }, header: { type: "issue", state: "open" }, content: "A very long issue subject that would wrap in a narrow board column", author: "Ada", effectiveTime: 1, versions: [] }], null), "board-card")[0] : null;
  const titleLine = bcard && findClass(bcard, "board-card-title")[0];
  ok("board card wraps glyph + subject in one title line", !!titleLine, "titleLine=" + !!titleLine);
  ok("the glyph and subject live inside that single title line", titleLine && findClass(titleLine, "type-glyph").length === 1 && findClass(titleLine, "subject").length === 1);

  // ---- merged timeline includes plain code commits (CLI/TUI parity) ----
  // thread-demo's code branches (main + feature/notes-expand) carry plain
  // commits with no GitMsg header; they must interleave with posts/issues/PRs
  // in the merged timeline, attributed to the right branch, deduped.
  const tctx = GS.newContext(BASE);
  const tl = await GS.loadTimelineItems(tctx);
  const code = tl.filter((i) => i._ext === "code");
  ok("timeline includes plain code commits", code.length > 0, "count=" + code.length);
  ok("code commits carry no GitMsg header", code.every((i) => !i.commit.gitmsg && Object.keys(i.header).length === 0));
  // Interleaved (not all grouped at the end): a code commit sits before some
  // non-code item, and the whole feed stays newest-first by effectiveTime.
  const firstCodeIdx = tl.findIndex((i) => i._ext === "code");
  const lastNonCodeIdx = tl.map((i) => i._ext).lastIndexOf("social") >= 0 ? tl.map((i) => i._ext).lastIndexOf("social") : tl.length - 1;
  ok("code commits interleave with ext items (not appended at the end)", firstCodeIdx >= 0 && firstCodeIdx < lastNonCodeIdx, "firstCode=" + firstCodeIdx + " lastSocial=" + lastNonCodeIdx);
  let tsorted = true; for (let i = 1; i < tl.length; i++) if (tl[i].effectiveTime > tl[i - 1].effectiveTime) { tsorted = false; break; }
  ok("timeline with code commits stays newest-first by effectiveTime", tsorted);
  // Dedup: a commit reachable from two branches appears once.
  const seen = {}; let dup = 0; for (const i of code) { if (seen[i.commit.hash]) dup++; seen[i.commit.hash] = 1; }
  ok("code commits are deduped across branches", dup === 0, "dups=" + dup);
  // Branch attribution: every code commit names a real code branch, and shared
  // ancestors attribute to the default branch (main), not a feature branch.
  const codeBranches = new Set(code.map((i) => i._branch));
  ok("code commits attribute to real branches (main present)", codeBranches.has("main"), "branches=" + Array.from(codeBranches).join(","));
  const readme = code.find((i) => /Initial commit: README/.test(i.content));
  ok("a shared ancestor attributes to the default branch (main)", readme && readme._branch === "main", "branch=" + (readme && readme._branch));
  const featOnly = code.find((i) => /Expand and edit notes/.test(i.content));
  ok("a feature-only commit attributes to its feature branch", featOnly && featOnly._branch === "feature/notes-expand", "branch=" + (featOnly && featOnly._branch));

  // ---- code-commit timeline card renders subject/author/hash to commit route ----
  const cc = GS.timelineCard(code[0], null);
  ok("code timeline card renders the commit subject", findClass(cc, "subject").length > 0 && textOf(findClass(cc, "subject")[0]).length > 0, textOf(findClass(cc, "subject")[0] || { _children: [] }));
  ok("code timeline card carries a commit glyph", findClass(cc, "type-glyph").some((g) => g._cls.has("tg-commit")));
  ok("code timeline card links its hash to #commit:", findClass(cc, "hash").some((h) => (h.getAttribute("href") || "").indexOf("#commit:") === 0), findClass(cc, "hash").map((h) => h.getAttribute("href")).join(" | "));
  ok("code timeline card has no state/enrichment chip row (no header)", findClass(cc, "card-chips").length === 0);

  // ---- windowed timeline pages code commits in step (bounded, deduped) ----
  const wctx = GS.newContext(BASE);
  const w1 = await GS.loadTimelineWindow(wctx, false);
  const w2 = await GS.loadTimelineWindow(wctx, true);
  ok("windowed timeline surfaces code commits", w1.items.some((i) => i._ext === "code"));
  const wseen = {}; let wdup = 0; for (const i of w2.items) { if (wseen[i.commit.hash]) wdup++; wseen[i.commit.hash] = 1; }
  ok("windowed timeline advance keeps items deduped", wdup === 0, "dups=" + wdup);
  ok("windowed timeline code count matches un-windowed set", w2.items.filter((i) => i._ext === "code").length === code.length, "w2=" + w2.items.filter((i) => i._ext === "code").length + " full=" + code.length);

  // ---- code commits are sourced from the push-maintained code items index ----
  // thread-demo's `site push` builds .gitsocial/site/items/code/ (metadata-only,
  // no bodies), so the timeline surfaces code commits from index JSON with NO
  // per-commit loose-object GET — the ~fetches-per-first-visit win. Assert the
  // artifact is present, carries NO code bodies corpus, and that resolving the
  // code items fetches zero commit objects (only the index shard/head JSON).
  const codeManifest = await GS.fetchText(BASE, ".gitsocial/site/items/code/manifest.json");
  ok("code items index manifest is published (v5, parents)", !!codeManifest && JSON.parse(codeManifest).version === 5, (codeManifest || "").slice(0, 60));
  const codeBodies = await GS.fetchText(BASE, ".gitsocial/site/bodies/code/manifest.json");
  ok("code corpus is metadata-only (no bodies index)", !codeBodies, "bodies manifest unexpectedly present");
  {
    const realFetch = global.fetch;
    let looseGets = 0;
    const looseRe = /objects\/[0-9a-f]{2}\/[0-9a-f]{38}$/;
    global.fetch = async (u, o) => { if (looseRe.test(String(u).split("?")[0])) looseGets++; return realFetch(u, o); };
    const ictx = GS.newContext(BASE);
    const r = await GS.resolveCodeItems(ictx, 200);
    global.fetch = realFetch;
    ok("code timeline sources from the index (no per-commit loose GETs)", looseGets === 0, "looseGets=" + looseGets + " items=" + r.items.length);
    ok("index-sourced code items carry their subject + branch", r.items.length > 0 && r.items.every((i) => i.content.length > 0 && typeof i._branch === "string"), "count=" + r.items.length);
  }

  console.log("\n" + pass + " passed, " + fail + " failed");
  process.exit(fail ? 1 : 0);
}
main().catch((e) => { console.error(e); process.exit(1); });

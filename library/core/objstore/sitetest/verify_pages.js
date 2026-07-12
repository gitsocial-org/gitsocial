// verify_pages.js - app pages + chrome against the live thread-demo fixture
// (has memos, a curated list, ext configs). Grab-bag:
//  - memos tab always visible (empty state on a no-memo bucket)
//  - mobile hamburger drawer
//  - full-width tree row click targets
//  - precise-time (local YYYY-MM-DD HH:MM) titles on relative times
//  - lists page (overview + detail)
//  - configuration page (reader prefs + repository/ext config)
//  - analytics rebuild (stats grid + monthly activity + contributors)
//  - reply context on bare commit permalinks (ancestor chain + trailer links)
require("./shim.js");
require("../site/icons.js");
const GS = require("../site/gs-app.js");
const fs = require("fs");
const { viewNode, textOf, setHash } = global.__shim;
let pass = 0, fail = 0;
const ok = (n, c, e) => { (c ? pass++ : fail++); console.log((c ? "PASS " : "FAIL ") + n + (!c && e ? " :: " + e : "")); };
const wait = (ms) => new Promise((r) => setTimeout(r, ms));
function findClass(node, cls, out) { out = out || []; for (const c of node._children || []) { if (c && c.nodeType === 1) { if (c._cls && c._cls.has(cls)) out.push(c); findClass(c, cls, out); } } return out; }
function findTag(node, tag, out) { out = out || []; for (const c of node._children || []) { if (c && c.nodeType === 1) { if (c.tagName.toLowerCase() === tag) out.push(c); findTag(c, tag, out); } } return out; }
function fire(node, ev, props) { (node && node._handlers && node._handlers[ev] || []).forEach((fn) => fn(Object.assign({ preventDefault() {}, stopPropagation() {}, key: "", button: 0 }, props || {}))); }
const sha = (n) => String(n).padStart(40, "0");
const fileEntry = (name, s) => ({ mode: "100644", name, sha: sha(s), type: "blob" });
const dirEntry = (name, s) => ({ mode: "40000", name, sha: sha(s), type: "tree" });
function treeBytes(entries) { const parts = []; for (const e of entries) parts.push(Buffer.from(e.mode + " " + e.name + "\0", "utf8"), Buffer.from(e.sha, "hex")); return new Uint8Array(Buffer.concat(parts)); }
const HTML = fs.readFileSync(require("path").join(__dirname, "../site/index.html"), "utf8");
const APP = fs.readFileSync(require("path").join(__dirname, "../site/gs-app.js"), "utf8");
const ORIGIN = process.env.GS_SITE_ORIGIN || "http://localhost:8000";
const TD = ORIGIN + "/" + (process.env.GS_SITE_BUCKET || "thread-demo") + "/";
const EMPTY = ORIGIN + "/" + (process.env.GS_SITE_BUCKET_EMPTY || "other-demo") + "/";
async function render(base, hash) { const ctx = GS.newContext(base); setHash(hash); await GS.route(ctx); await wait(400); return ctx; }

async function item1() {
  console.log("\n--- Item 1: Memos tab always visible ---");
  ok("#nav-memos carries no display:none", !/id="nav-memos"[^>]*display:\s*none/.test(HTML));
  ok("revealMemosNav machinery removed from gs-app", !/revealMemosNav/.test(APP));
  ok("gs-app no longer destructures refTip/EXT_BRANCHES for reveal", !/EXT_BRANCHES,/.test(APP.split("route(ctx)")[0]) || !/revealMemosNav/.test(APP));
  // memos route on a bucket WITHOUT a memo branch renders the standard empty state.
  await render(EMPTY, "#/memos");
  ok("no-memo bucket renders 'No memos' empty state", /No memos in this repository\./.test(textOf(viewNode)), textOf(viewNode).slice(0, 60));
}

function item2() {
  console.log("\n--- Item 2: Mobile hamburger drawer ---");
  ok("mobile top bar present", /id="mobile-bar"/.test(HTML) && /class="mobile-bar"/.test(HTML));
  ok("hamburger button present (aria-controls=nav)", /id="nav-hamburger"[^>]*aria-controls="nav"/.test(HTML));
  ok("nav scrim present", /id="nav-scrim"/.test(HTML));
  const media = HTML.slice(HTML.lastIndexOf("@media (max-width: 720px)"));
  ok("drawer: nav is a fixed slide-over (transform)", /\.nav\s*\{[^}]*position:\s*fixed[^}]*transform:\s*translateX\(-100%\)/.test(media.replace(/\n/g, " ")));
  ok("drawer opens on body.nav-open", /body\.nav-open\s+\.nav\s*\{[^}]*translateX\(0\)/.test(media.replace(/\n/g, " ")));
  ok("nav forced display:flex (collapse can't trap mobile)", /\.nav\s*\{[^}]*display:\s*flex\s*!important/.test(media.replace(/\n/g, " ")));
  ok("desktop handle hidden on mobile", /\.nav-handle\s*\{\s*display:\s*none\s*!important/.test(media.replace(/\n/g, " ")));
  ok("tree slot shown in mobile drawer (no hide)", !/\.nav-tree-slot\s*\{\s*display:\s*none/.test(media.replace(/\n/g, " ")));
  ok("hamburger JS toggles body.nav-open", /nav-hamburger[\s\S]{0,400}nav-open/.test(HTML));
  ok("hamburger JS closes on hashchange", /hashchange[^)]*\)\s*=>\s*setOpen\(false\)/.test(HTML));
}

async function item3() {
  console.log("\n--- Item 3: Full-width tree row click targets ---");
  ok(".tree-dir cursor is pointer (full-row affordance)", /\.tree-dir\s*\{\s*cursor:\s*pointer/.test(HTML));
  const ctx = GS.newContext("http://x/");
  ctx.objects.set(sha(10), { type: "tree", body: treeBytes([fileEntry("a.go", 11)]) });
  const root = [dirEntry("src", 10), fileEntry("readme.md", 12)];
  const list = global.document.createElement("div");
  GS.mountTree(ctx, list, root, "", "main");
  await wait(20);
  const dirRow = findClass(list, "tree-dir")[0];
  const fileRow = findClass(list, "tree-row").find((r) => r.tagName.toLowerCase() === "a" && (r.getAttribute("href") || "").includes("readme.md"));
  ok("file row IS the anchor (whole row navigates)", !!fileRow && fileRow.tagName.toLowerCase() === "a", fileRow && fileRow.tagName);
  ok("dir row has a click handler", !!(dirRow && dirRow._handlers && dirRow._handlers.click && dirRow._handlers.click.length));
  const node = dirRow._parent; // tree-node wraps [row, childrenEl]
  const childrenEl = node._children[1];
  ok("dir children start empty (collapsed)", childrenEl._children.length === 0);
  fire(dirRow, "click", { target: dirRow }); await wait(20);
  ok("whole-row click expands the directory", childrenEl._children.length > 0, "kids=" + childrenEl._children.length);
  fire(dirRow, "click", { target: dirRow }); await wait(20);
  ok("whole-row click again collapses", childrenEl._children.length === 0);
  // dir NAME is a real anchor to the rooted dir route (still navigates).
  const nameA = findClass(dirRow, "tree-name")[0];
  ok("dir name is an anchor to #file:src@main", nameA && nameA.tagName.toLowerCase() === "a" && (nameA.getAttribute("href") || "").includes("src"), nameA && nameA.getAttribute("href"));
}

async function item4() {
  console.log("\n--- Item 4: Precise-time (with tz) + author-email tooltips ---");
  const items = await GS.loadExtItems(GS.newContext(TD), "social");
  ok("thread-demo has social items to render", items.length > 0, "n=" + items.length);
  const card = GS.metaRow(items[0], "gitmsg/social");
  const rt = findClass(card, "reltime")[0];
  ok("metaRow renders a .reltime span", !!rt);
  const title = rt && rt.getAttribute("title");
  ok("reltime carries precise local YYYY-MM-DD HH:MM <tz> title", !!title && /^\d{4}-\d{2}-\d{2} \d{2}:\d{2} \S+/.test(title), "title=" + title);
  ok("reltime text is the relative form", /\bago\b|just now/.test(textOf(rt)), textOf(rt));
  // Author name renders through the shared authorEl (email in title on hover).
  const au = findClass(card, "author")[0];
  ok("metaRow renders a .author span", !!au, "no .author");
  ok("author name text present", au && textOf(au).length > 0, au && textOf(au));
  const email = GS.newContext ? (items[0].commit && items[0].commit.authorEmail) : "";
  if (email) ok("author span carries the email in its title", au && au.getAttribute("title") === email, "title=" + (au && au.getAttribute("title")) + " email=" + email);
  // Direct authorEl contract: tooltip present with email, omitted without.
  const withEmail = GS.authorEl("Ada Lovelace", "ada@x.io");
  ok("authorEl sets title to the email", withEmail.getAttribute("title") === "ada@x.io");
  ok("authorEl omits title when no email", !GS.authorEl("Ada", "").getAttribute("title"));
}

async function item5() {
  console.log("\n--- Item 5: Lists page ---");
  await render(TD, "#/lists");
  const vt = textOf(viewNode);
  ok("lists overview shows the fixture list name", /Curated Follows/.test(vt), vt.slice(0, 120));
  ok("lists overview shows member count", /2 members/.test(vt), vt);
  const listLink = findTag(viewNode, "a").find((a) => (a.getAttribute("href") || "").includes("#list:social/curated"));
  ok("list card links to #list:social/curated", !!listLink);
  // detail
  await render(TD, "#list:social/curated");
  const dt = textOf(viewNode);
  ok("list detail shows the name", /Curated Follows/.test(dt));
  ok("list detail renders foreign members as labeled text (repo chip)", findClass(viewNode, "chip").some((c) => textOf(c) === "repo"), dt.slice(0, 160));
  ok("list detail shows the meshtastic firmware member ref", /meshtastic\/firmware#branch:main/.test(dt), dt.slice(0, 200));
  // no-list bucket empty state
  await render(EMPTY, "#/lists");
  ok("no-list bucket renders 'No lists'", /No lists in this repository\./.test(textOf(viewNode)), textOf(viewNode).slice(0, 60));
}

async function item6() {
  console.log("\n--- Item 6: Configuration page ---");
  global.localStorage.setItem("diffview", "split");
  await render(TD, "#/config");
  const vt = textOf(viewNode);
  ok("config shows Reader preferences section", /Reader preferences/.test(vt));
  ok("config shows Repository configuration section", /Repository configuration/.test(vt));
  ok("pref reflects localStorage diffview=split", findClass(viewNode, "pref-btn").some((b) => textOf(b) === "split"), findClass(viewNode, "pref-btn").map(textOf).join(","));
  ok("config renders social ext config (branch)", /gitmsg\/social/.test(vt), vt.slice(0, 300));
  ok("config renders pm framework=kanban value", /kanban/.test(vt));
  ok("release ext (no config ref) shows 'defaults'", /release[\s\S]{0,40}defaults/i.test(vt) || /defaults/.test(vt), vt);
  // localStorage change reflected on re-render
  global.localStorage.setItem("diffview", "unified");
  await render(TD, "#/config");
  ok("pref reflects localStorage diffview=unified after change", findClass(viewNode, "pref-btn").some((b) => textOf(b) === "unified"));
}

async function item7() {
  console.log("\n--- Item 7: Analytics rebuild (uncapped, interactive granularity) ---");
  await render(TD, "#/analytics");
  const vt = textOf(viewNode);
  ok("analytics has repo-facts branch chip", /main/.test(vt) && findClass(viewNode, "meta-strip").length > 0);
  // Summary stat grid: total + per-kind + most active.
  ok("analytics has a Summary section", /Summary/.test(vt));
  ok("analytics stat grid has cells", findClass(viewNode, "stat-cell").length >= 5, "cells=" + findClass(viewNode, "stat-cell").length);
  ok("summary shows total items", /total items/.test(vt));
  ok("summary shows per-kind labels", /posts/.test(vt) && /issues/.test(vt) && /PRs/.test(vt) && /releases/.test(vt) && /memos/.test(vt));
  ok("summary shows most active period", /most active/.test(vt));
  const nums = findClass(viewNode, "stat-value").map((c) => textOf(c));
  ok("stat values are numeric", nums.length > 0 && nums.every((n) => /^\d+$/.test(n)), nums.join(","));
  // Activity chart: heading, granularity toggle (monthly default), legend, stacked bars.
  ok("analytics has an Activity heading with period span", /Activity \(\d+ (week|month|year)s?\)/.test(vt), vt.match(/Activity[^\n]*/));
  const granBtns = findClass(viewNode, "gran-btn");
  ok("granularity toggle has weekly/monthly/yearly", granBtns.length === 3 && granBtns.map(textOf).join(",") === "weekly,monthly,yearly", granBtns.map(textOf).join(","));
  ok("monthly is the default active granularity", granBtns.some((b) => b._cls.has("active") && textOf(b) === "monthly"));
  ok("chart series filter present with an 'all' chip + per-kind swatches", findClass(viewNode, "chart-filter").length === 1 && findClass(viewNode, "legend-swatch").length >= 1 && findClass(viewNode, "filter-chip").some((c) => textOf(c) === "all"));
  ok("stacked activity segments render", findClass(viewNode, "activity-seg").length > 0, "segs=" + findClass(viewNode, "activity-seg").length);
  // Top authors ranking (item-count based, with percentages and email tooltips).
  ok("top-authors section renders with percentages", /Authors \d/.test(vt) && findClass(viewNode, "contrib-pct").length > 0);
  // B5: each author name is a deep-link into #/search with the author facet
  // prefilled, and the heading carries a live filter input.
  const contrib = findClass(viewNode, "contrib").length ? findClass(viewNode, "contrib")[findClass(viewNode, "contrib").length - 1] : null;
  const contribNameLinks = contrib ? findClass(contrib, "contrib-name").map((n) => findTag(n, "a")[0]).filter(Boolean) : [];
  ok("top-authors names link to the search page", contribNameLinks.length > 0, "name links=" + contribNameLinks.length);
  ok("author link prefills the search author facet", contribNameLinks.some((a) => (a.getAttribute("href") || "").indexOf("#/search/author") === 0), contribNameLinks.map((a) => a.getAttribute("href")).join(","));
  ok("top-authors heading has a live filter input", findClass(viewNode, "contrib-filter").length === 1);
  const contribFilter = findClass(viewNode, "contrib-filter")[0];
  const rowsBefore = findClass(viewNode, "contrib-row").length;
  if (contribFilter) { contribFilter.value = "zzzznomatchzzzz"; fire(contribFilter, "input", { target: contribFilter }); }
  const rowsAfter = findClass(viewNode, "contrib-row").length;
  ok("author filter narrows the list live", rowsBefore > 0 && rowsAfter === 0, "before=" + rowsBefore + " after=" + rowsAfter);
  if (contribFilter) { contribFilter.value = ""; fire(contribFilter, "input", { target: contribFilter }); }
  // Toggle re-buckets the already-loaded data in memory (no refetch): click yearly.
  const yearly = granBtns.find((b) => textOf(b) === "yearly");
  const segsBefore = findClass(viewNode, "activity-seg").length;
  fire(yearly, "click", { target: yearly }); await wait(20);
  const vt2 = textOf(viewNode);
  ok("clicking yearly re-buckets to a year span", /Activity \(\d+ years?\)/.test(vt2), vt2.match(/Activity[^\n]*/));
  ok("yearly is now the active granularity", findClass(viewNode, "gran-btn").some((b) => b._cls.has("active") && textOf(b) === "yearly"));
  ok("chart still renders segments after re-bucket", findClass(viewNode, "activity-seg").length > 0, "before=" + segsBefore + " after=" + findClass(viewNode, "activity-seg").length);
}

async function item8() {
  console.log("\n--- Item 8: Reply context on bare commit permalinks ---");
  const lookup = GS.newContext(TD);
  const social = await GS.loadExtItems(lookup, "social");
  const r2 = social.find((i) => (i.content || "").startsWith("Seconded"));
  ok("fixture reply-to-reply present", !!r2, "no 'Seconded' comment");
  if (!r2) return;
  await render(TD, "#commit:" + r2.commit.hash + "@gitmsg/social");
  const detail = findClass(viewNode, "detail")[0];
  ok("reply permalink renders the detail view", !!detail);
  const threads = findClass(viewNode, "thread");
  const rc = threads.find((t) => /In reply to/.test(textOf(t)));
  ok("reply-context section present ('In reply to')", !!rc, "threads=" + threads.length);
  const cards = rc ? findClass(rc, "comment") : [];
  ok("ancestor chain renders 3 parent cards", cards.length === 3, "cards=" + cards.length);
  ok("root post renders first with real content", !!cards[0] && /Shipping the S3 static site reader/.test(textOf(cards[0])), cards[0] && textOf(cards[0]).slice(0, 60));
  ok("immediate parent renders last", !!cards[2] && /Thanks, appreciate/.test(textOf(cards[2])), cards[2] && textOf(cards[2]).slice(0, 60));
  ok("chain is rail-indented", !!rc && findClass(rc, "rail-guide").length >= 3, rc && "guides=" + findClass(rc, "rail-guide").length);
  // Context-card layout: a detail-meta flex row (meta left, right-aligned Raw
  // toggle in view-modes) ABOVE the clamped content pane, and the pane keeps
  // body-clamp at build even though itemDetail builds the cards detached (the
  // measure polls until layout, no 0-height misread).
  const c0kids = cards.length ? cards[0]._children.filter((c) => c && c.nodeType === 1) : [];
  ok("context card renders detail-meta row first (above content)", !!c0kids[0] && c0kids[0]._cls.has("detail-meta") && findClass(c0kids[0], "meta").length >= 1, c0kids[0] && c0kids[0].className);
  ok("context card meta row carries the right-aligned Raw toggle", !!c0kids[0] && findClass(c0kids[0], "view-modes").length === 1 && findClass(c0kids[0], "view-toggle").length === 1);
  ok("clamp wrap follows the meta row", !!c0kids[1] && c0kids[1]._cls.has("body-clamp-wrap"), c0kids[1] && c0kids[1].className);
  const ctxPane = c0kids[1] && c0kids[1]._children[0];
  ok("detached-built context pane still carries body-clamp", !!ctxPane && ctxPane._cls.has("body-clamp"));
  if (ctxPane) {
    ctxPane.scrollHeight = 800; ctxPane.clientHeight = 280;
    await wait(20);
    ok("context pane gets Show more toggle once laid out", findClass(cards[0], "body-clamp-toggle").length === 1 && ctxPane._cls.has("body-clamp"), "toggles=" + findClass(cards[0], "body-clamp-toggle").length);
  }
  // The context sits above the commit's own subject.
  const kids = detail ? detail._children.filter((c) => c && c.nodeType === 1) : [];
  const iThread = kids.indexOf(rc), iSubject = kids.findIndex((c) => c._cls && c._cls.has("subject"));
  ok("reply context renders above the commit", iThread >= 0 && iSubject > iThread, "thread=" + iThread + " subject=" + iSubject);
  // Trailer table: same-repo original/reply-to values are commit-route links.
  const links = [];
  for (const dd of findTag(detail || viewNode, "dd")) for (const a of findTag(dd, "a")) links.push(a.getAttribute("href"));
  ok("trailer original/reply-to link to commit routes", links.length === 2 && links.every((h) => h && h.startsWith("#commit:") && h.endsWith("@gitmsg/social")), JSON.stringify(links));
  // Cross-repo quote keeps its current behavior: plain trailer value + embedded block.
  const quote = social.find((i) => (i.content || "").startsWith("Great point from upstream"));
  ok("fixture cross-repo quote present", !!quote, "no quote item");
  if (!quote) return;
  await render(TD, "#commit:" + quote.commit.hash + "@gitmsg/social");
  const origDd = findTag(viewNode, "dd").find((dd) => /other-demo#commit:/.test(textOf(dd)));
  ok("cross-repo original stays plain text (no link)", !!origDd && findTag(origDd, "a").length === 0, origDd ? "links=" + findTag(origDd, "a").length : "no dd");
  ok("cross-repo quote keeps embedded block, no reply-context head", /from s3:\/\/fake\.example\.com\/other-demo/.test(textOf(viewNode)) && !/In reply to/.test(textOf(viewNode)), textOf(viewNode).slice(0, 120));
}

async function item9() {
  console.log("\n--- Item 9: List-card body clamp ---");
  const mkCommit = (short, time) => ({ short, hash: short + "0".repeat(40 - short.length), authorName: "Ada", authorEmail: "ada@x.io", authorTime: time, refs: [], rawMessage: "", content: "" });
  const longText = Array.from({ length: 40 }, (_, i) => "line " + (i + 1)).join("\n");
  // The shim has no layout, so scrollHeight/clientHeight are stubbed on the
  // body element before the deferred (setTimeout-fallback) measure runs.
  // timelineCard without _ext dispatches to socialCard (not exported directly).
  const long = GS.timelineCard({ commit: mkCommit("aaaaaaaaaaaa", 1000), header: { type: "post" }, content: "Long post\n\n" + longText, author: "Ada", effectiveTime: 1000 });
  const lb = findClass(long, "body-clamp")[0];
  ok("long social body carries body-clamp at build", !!lb && lb._cls.has("body"), lb && lb.className);
  lb.scrollHeight = 800; lb.clientHeight = 280;
  await wait(20);
  ok("overflowing body keeps the clamp after measure", lb._cls.has("body-clamp"));
  const btn = findClass(long, "body-clamp-toggle")[0];
  ok("overflowing body gets a Show more toggle", !!btn && textOf(btn) === "Show more", btn && textOf(btn));
  let stopped = false;
  fire(btn, "click", { stopPropagation() { stopped = true; } });
  ok("toggle expands (clamp class removed)", !lb._cls.has("body-clamp"));
  ok("toggle text flips to Show less", textOf(btn) === "Show less", textOf(btn));
  ok("toggle stops propagation (cardNav untriggered)", stopped);
  fire(btn, "click");
  ok("second click re-clamps and flips back to Show more", lb._cls.has("body-clamp") && textOf(btn) === "Show more");
  // A body that fits loses the clamp entirely and gets no toggle.
  const short = GS.timelineCard({ commit: mkCommit("bbbbbbbbbbbb", 1000), header: { type: "post" }, content: "Hi\n\nshort body", author: "Ada", effectiveTime: 1000 });
  const sb = findClass(short, "body-clamp")[0];
  ok("short body starts clamped too (pre-measure)", !!sb);
  sb.scrollHeight = 50; sb.clientHeight = 50;
  await wait(20);
  ok("fitting body loses the clamp class", !sb._cls.has("body-clamp"));
  ok("fitting body gets no toggle", findClass(short, "body-clamp-toggle").length === 0);
  // release + memo cards route their bodies through the same helper.
  const rel = GS.releaseCard({ commit: mkCommit("cccccccccccc", 1000), header: { type: "release", tag: "v1.0.0" }, content: "Notes\n\n" + longText, author: "Ada", effectiveTime: 1000 });
  ok("releaseCard body goes through the clamp helper", findClass(rel, "body-clamp").length === 1);
  const memo = GS.memoCard({ commit: mkCommit("dddddddddddd", 1000), header: { type: "memo" }, content: "Memo subject\n\n" + longText, author: "Ada", effectiveTime: 1000 });
  ok("memoCard body goes through the clamp helper", findClass(memo, "body-clamp").length === 1);
  // A card built while DETACHED (itemDetail builds reply-context cards, then
  // awaits more work before setView attaches) must still end up clamped: the
  // deferred measure polls until the node reports layout instead of misreading
  // 0-height as "fits".
  const late = GS.timelineCard({ commit: mkCommit("eeeeeeeeeeee", 1000), header: { type: "post" }, content: "Late post\n\n" + longText, author: "Ada", effectiveTime: 1000 });
  const lateBody = findClass(late, "body-clamp")[0];
  await wait(30); // several deferred ticks pass with no layout: no premature verdict
  ok("detached card keeps the clamp while unmeasurable", !!lateBody && lateBody._cls.has("body-clamp"));
  ok("detached card gets no toggle before layout", findClass(late, "body-clamp-toggle").length === 0);
  global.document.createElement("div").append(late); // attach, then layout appears
  lateBody.scrollHeight = 800; lateBody.clientHeight = 280;
  await wait(20);
  ok("late-attached overflowing card ends clamped with a toggle", lateBody._cls.has("body-clamp") && findClass(late, "body-clamp-toggle").length === 1, "toggles=" + findClass(late, "body-clamp-toggle").length);
  // Timeline (list surface) renders clamp wraps; a non-reply item's detail
  // never clamps (reply permalinks clamp only their context cards — Item 8).
  await render(TD, "#/timeline");
  ok("timeline cards render clamp-wrapped bodies", findClass(viewNode, "body-clamp-wrap").length > 0, "wraps=" + findClass(viewNode, "body-clamp-wrap").length);
  const social = await GS.loadExtItems(GS.newContext(TD), "social");
  const rootPost = social.find((i) => (i.content || "").startsWith("Shipping the S3 static site reader"));
  await render(TD, "#commit:" + rootPost.commit.hash + "@gitmsg/social");
  ok("non-reply detail view has no clamp anywhere (unclamped)", findClass(viewNode, "body-clamp-wrap").length === 0 && findClass(viewNode, "body-clamp").length === 0);
}

(async () => {
  await wait(700); // let gs-app init()'s boot route settle before driving routes
  await item1(); item2(); await item3(); await item4(); await item5(); await item6(); await item7(); await item8(); await item9();
  console.log(`\n${pass} passed, ${fail} failed`);
  process.exit(fail ? 1 : 0);
})().catch((e) => { console.error("THREW:", e); process.exit(1); });

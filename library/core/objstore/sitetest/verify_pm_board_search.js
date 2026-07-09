// verify_pm_board_search.js - PM board + search proofs, synthetic DOM (issuesBody
// sub-chips) + live drives against thread-demo and meshtastic. Grab-bag:
//  - kanban board columns / WIP (thread-demo + 773-issue meshtastic + recount)
//  - milestone / sprint detail (members + progress bar)
//  - sub-issue hierarchy (both directions, incl. grandchild)
//  - in-bucket item search (grouping, highlight, author, '/' shortcut, Escape)
require("./shim.js");
require("../site/icons.js");
const GS = require("../site/gs-app.js");
const { viewNode, textOf, setHash, mkEl, docHandlers } = global.__shim;
let pass = 0, fail = 0;
const ok = (n, c, e) => { (c ? pass++ : fail++); console.log((c ? "PASS " : "FAIL ") + n + (!c && e ? " :: " + e : "")); };
const wait = (ms) => new Promise((r) => setTimeout(r, ms));
function findClass(node, cls, out) { out = out || []; for (const c of (node && node._children) || []) { if (c && c.nodeType === 1) { if (c._cls && c._cls.has(cls)) out.push(c); findClass(c, cls, out); } } return out; }
function findTagAll(node, tag, out) { out = out || []; for (const c of (node && node._children) || []) { if (c && c.nodeType === 1) { if (c.tagName.toLowerCase() === tag) out.push(c); findTagAll(c, tag, out); } } return out; }
function fire(node, ev, props) { (node && node._handlers && node._handlers[ev] || []).forEach((fn) => fn(Object.assign({ preventDefault() {}, stopPropagation() {}, key: "", target: { closest() { return null; } } }, props || {}))); }
async function run(bucket, hash) { global.__ctx = GS.newContext((process.env.GS_SITE_ORIGIN||"http://localhost:8000")+"/" + bucket + "/"); setHash(hash); await GS.route(global.__ctx); await wait(1600); }
const mkItem = (short, t, header, content, author) => ({ commit: { hash: (short + "0".repeat(40)).slice(0, 40), short }, header: header || {}, content: content || "", effectiveTime: t, author: author || "Tester", edited: false, editorName: "" });
const ref = (h) => "#commit:" + h + "@gitmsg/pm";

async function main() {
  await wait(2500); // drain init home route

  // ================= A: synthetic DOM (issuesBody sub-chip + groups) ==========
  const { issuesBody } = GS;
  const epic = mkItem("aaaaaaaaaaaa", 5, { type: "issue", state: "open" }, "Epic issue");
  const child = mkItem("bbbbbbbbbbbb", 4, { type: "issue", state: "closed", parent: ref("aaaaaaaaaaaa") }, "Child issue");
  const ms = mkItem("cccccccccccc", 6, { type: "milestone", state: "open", due: "2026-09-01" }, "Milestone X");
  const linked = mkItem("dddddddddddd", 3, { type: "issue", state: "open", milestone: ref("cccccccccccc") }, "Linked issue");
  const nodes = issuesBody([epic, child, ms, linked]);
  const wrap = mkEl("div"); for (const n of nodes) wrap.append(n);
  const subChips = findClass(wrap, "pm-sub-chip");
  ok("A issuesBody adds a sub-issue chip on the parent", subChips.length === 1 && textOf(subChips[0]) === "1 sub", subChips.map(textOf).join(","));
  const groupsHead = findClass(wrap, "pm-groups-head").map(textOf);
  ok("A issues page does not embed milestones/sprints (they have their own nav pages)", groupsHead.length === 0, groupsHead.join(","));

  // ================= B: live thread-demo board =================
  await run("thread-demo", "#/board");
  let board = findClass(viewNode, "board")[0];
  ok("B board route renders a .board", !!board);
  const colHeads = findClass(viewNode, "board-col-name").map(textOf);
  ok("B four kanban columns", findClass(viewNode, "board-col").length === 4, "cols=" + findClass(viewNode, "board-col").length);
  ok("B column headers carry names + counts", colHeads.some((h) => h.startsWith("Backlog")) && colHeads.some((h) => h.startsWith("In Progress")) && colHeads.some((h) => h.startsWith("Done")), colHeads.join(" | "));
  // WIP shown on In Progress/Review headers
  ok("B In Progress header shows WIP limit", colHeads.some((h) => h.indexOf("/ 3") !== -1), colHeads.join(" | "));

  // ================= B: milestone detail (members + progress) =================
  // Resolve fixture hashes dynamically from the live pm set.
  const tctx = GS.newContext((process.env.GS_SITE_ORIGIN||"http://localhost:8000")+"/thread-demo/");
  const pm = await GS.loadExtItemsUpTo(tctx, "pm", 800);
  const find = (t, sub) => pm.find((i) => ((i.header && i.header.type) || "issue") === t && i.content.split("\n")[0].indexOf(sub) !== -1);
  const msItem = find("milestone", "v1.0 Launch");
  const spItem = find("sprint", "Sprint 1");
  const epicItem = find("issue", "Epic: Reader UX");
  ok("B fixture has milestone/sprint/epic", !!msItem && !!spItem && !!epicItem);

  await run("thread-demo", "#commit:" + msItem.commit.hash + "@gitmsg/pm");
  const mHead = findClass(viewNode, "pm-members-head").map(textOf);
  ok("B milestone detail lists Linked Issues", mHead.some((h) => h.indexOf("Linked Issues (4)") !== -1), mHead.join(","));
  let prog = findClass(viewNode, "pm-progress-label").map(textOf);
  ok("B milestone progress reads '1 closed of 4'", prog.some((p) => p === "1 closed of 4"), prog.join(","));
  const barFill = findClass(viewNode, "pm-bar-fill")[0];
  ok("B milestone progress bar fill set (25%)", barFill && (barFill.getAttribute("style") || "").indexOf("width:25%") !== -1, barFill && barFill.getAttribute("style"));

  // ================= B: sprint detail =================
  await run("thread-demo", "#commit:" + spItem.commit.hash + "@gitmsg/pm");
  const sHead = findClass(viewNode, "pm-members-head").map(textOf);
  ok("B sprint detail lists Sprint Backlog", sHead.some((h) => h.indexOf("Sprint Backlog (2)") !== -1), sHead.join(","));

  // ================= B: sub-issue hierarchy (both directions) =================
  await run("thread-demo", "#commit:" + epicItem.commit.hash + "@gitmsg/pm");
  const subHead = findClass(viewNode, "pm-subissues-head").map(textOf);
  ok("B epic detail shows Sub-issues (n open, n closed)", subHead.some((h) => h.indexOf("Sub-issues (1 open, 1 closed)") !== -1), subHead.join(","));
  // the sub-issue rows link to their child issue detail (forward direction)
  const subLinks = findClass(findClass(viewNode, "pm-subissues")[0] || mkEl("div"), "pm-member");
  ok("B epic lists its two children as linked rows", subLinks.length === 2, "rows=" + subLinks.length);
  const childHashes = subLinks.map((r) => { const a = findTagAll(r, "a")[0]; return a && a.getAttribute("href"); });
  ok("B child rows link to gitmsg/pm commit routes", childHashes.every((h) => h && h.indexOf("#commit:") === 0 && h.indexOf("@gitmsg/pm") !== -1), childHashes.join(","));
  // backward direction: open a child, verify a 'parent' relationship link back to the epic
  const childItem = pm.find((i) => i.content.split("\n")[0].indexOf("Sub: dark mode polish") !== -1);
  await run("thread-demo", "#commit:" + childItem.commit.hash + "@gitmsg/pm");
  const relLabels = findClass(viewNode, "pm-rel-label").map(textOf);
  ok("B child detail shows a parent relationship link", relLabels.includes("parent"), relLabels.join(","));
  const relLink = findClass(viewNode, "pm-rel-link")[0];
  ok("B parent link targets the epic", relLink && textOf(relLink).indexOf("Epic: Reader UX") !== -1, relLink && textOf(relLink));
  // this child ("dark mode polish") is itself a parent of the sub-sub issue
  const subHead2 = findClass(viewNode, "pm-subissues-head").map(textOf);
  ok("B nested child also shows its own sub-issue (grandchild)", subHead2.some((h) => h.indexOf("Sub-issues (1 open, 0 closed)") !== -1), subHead2.join(","));

  // ================= B: search (thread-demo) =================
  await run("thread-demo", "#/search");
  let input = findClass(viewNode, "search-input")[0];
  ok("B search view renders an input", !!input);
  ok("B search help scope note present before query", findClass(viewNode, "search-help").length === 1);
  input.value = "onboarding"; fire(input, "input"); await wait(250);
  let groupHeads = findClass(viewNode, "search-group-head").map(textOf);
  ok("B search 'onboarding' finds issues, grouped", groupHeads.some((h) => h.indexOf("Issues (") === 0), groupHeads.join(" | "));
  let marks = findClass(viewNode, "search-mark").map(textOf);
  ok("B matched substring highlighted", marks.some((m) => m.toLowerCase() === "onboarding"), marks.join(","));
  // author search
  input.value = "ada"; fire(input, "input"); await wait(250);
  const statusA = findClass(viewNode, "search-status")[0];
  ok("B search by author 'ada' returns results", statusA && /[1-9]\d* results?/.test(textOf(statusA)), statusA && textOf(statusA));
  // escape clears
  fire(input, "keydown", { key: "Escape" }); await wait(50);
  ok("B Escape clears the query (help returns)", input.value === "" && findClass(viewNode, "search-help").length === 1);

  // ================= C: meshtastic board (773 issues) + recount =================
  const mctx = GS.newContext((process.env.GS_SITE_ORIGIN||"http://localhost:8000")+"/meshtastic/");
  let win = await GS.loadExtItemsWindow(mctx, "pm", false);
  const issuesOf = (its) => its.filter((i) => ((i.header && i.header.type) || "issue") === "issue");
  const boardCount = (its) => GS.buildBoard(issuesOf(its)).columns.reduce((n, c) => n + c.issues.length, 0);
  const c1 = boardCount(win.items);
  win = await GS.loadExtItemsWindow(mctx, "pm", true);
  const c2 = boardCount(win.items);
  ok("C board recounts after one deepening (grows)", c2 > c1, "window1=" + c1 + " window2=" + c2);
  // fully loaded board totals the 773 issues
  let mfull = await GS.loadExtItemsUpTo(mctx, "pm", 5000);
  const bfull = GS.buildBoard(issuesOf(mfull));
  const total = bfull.columns.reduce((n, c) => n + c.issues.length, 0);
  ok("C full meshtastic board totals all issues (773)", total === 773, "total=" + total);
  ok("C mesh board columns non-empty (Backlog+Done)", bfull.columns[0].issues.length > 0 && bfull.columns[3].issues.length > 0, bfull.columns.map((c) => c.name + "=" + c.issues.length).join(" "));

  // ================= C: search a known meshtastic subject substring =========
  const perExt = { pm: mfull, review: [], social: [], release: [], memo: [] };
  const knownSub = mfull.find((i) => ((i.header && i.header.type) || "issue") === "issue").content.split("\n")[0];
  const term = (knownSub.match(/[A-Za-z]{5,}/) || ["LoRa"])[0];
  const sr = GS.searchItems(term, perExt);
  ok("C search finds a known meshtastic issue by subject substring", sr.total >= 1 && sr.groups[0].label === "Issues", "term=" + term + " total=" + sr.total);

  // ================= D: '/' shortcut wiring =================
  setHash("#/");
  const kh = (docHandlers.keydown || []);
  ok("D a global keydown handler is registered", kh.length >= 1);
  let navigated = false;
  const origHash = global.location.hash;
  kh.forEach((fn) => fn({ key: "/", preventDefault() {}, target: { tagName: "BODY" } }));
  ok("D '/' from body navigates to #/search", global.location.hash === "#/search", "hash=" + global.location.hash);
  // '/' while typing in an input is ignored
  global.location.hash = "#/timeline";
  kh.forEach((fn) => fn({ key: "/", preventDefault() {}, target: { tagName: "INPUT" } }));
  ok("D '/' ignored while typing in an input", global.location.hash === "#/timeline", "hash=" + global.location.hash);

  console.log("\n" + pass + " passed, " + fail + " failed");
  process.exit(fail ? 1 : 0);
}
main().catch((e) => { console.error(e); process.exit(1); });

// verify_styles.js - computed-style and structure assertions against a real
// browser, the one thing the DOM shim cannot check. Skips without Chrome.
const fs = require("fs");
const path = require("path");
const { execFileSync } = require("child_process");
const chrome = require("./chrome.js");
const cdp = require("./cdp.js");

const ORIGIN = process.env.GS_SITE_ORIGIN || "http://localhost:8000";
const BASE = ORIGIN + "/thread-demo/";
const DIR = path.join(__dirname, "styles");
const FIX = JSON.parse(fs.readFileSync(path.join(__dirname, "parity_fixtures.json"), "utf8"));
const WIDTH = 1280, HEIGHT = 900, BUDGET = 9000;
const PHONE_WIDTH = 390, PHONE_HEIGHT = 844;
const THEMES = { dark: [], light: ["--blink-settings=preferredColorScheme=1"] };

let pass = 0, fail = 0;
const ok = (n, c, e) => { (c ? pass++ : fail++); console.log((c ? "PASS " : "FAIL ") + n + (!c && e ? " :: " + e : "")); };

// ROUTES are the surfaces whose styling a change can break. A detail route
// resolves by subject text, never by position or hash: the fixture rebuilds
// with new shas and its list order depends on timestamps.
const ROUTES = [
  { name: "home", hash: "#/" },
  { name: "issues", hash: "#/issues" },
  { name: "prs", hash: "#/prs" },
  { name: "timeline", hash: "#/timeline" },
  { name: "releases", hash: "#/releases" },
  { name: "memos", hash: "#/memos" },
  { name: "commits", hash: "#/commits" },
  { name: "lists", hash: "#/lists" },
  { name: "board", hash: "#/board" },
  { name: "pr-detail", subject: "Expand notes with more lines" },
  { name: "branch-missing", hash: "#branch:no-such-branch" },
];

// VERDICT_BORDER pins the C.2 state colours a feedback card's left border
// carries; both are theme-independent, so one value holds in light and dark.
const VERDICT_BORDER = { approved: "rgb(31, 157, 85)", changesRequested: "rgb(207, 34, 46)" };

// checkFeedbackCard asserts the feedback variant takes a plain card's padding
// and marks its verdict on the left border. cards is a list route's .card
// record, feedback the pull request route's .card.feedback record.
function checkFeedbackCard(theme, cards, feedback) {
  if (!cards || !feedback) {
    ok("feedback card " + theme + ": both records captured", false, "cards=" + !!cards + " feedback=" + !!feedback);
    return;
  }
  const pads = new Set(cards.map((r) => r.padding));
  const fbPads = new Set(feedback.map((r) => r.padding));
  ok("feedback card " + theme + ": padding is the card's own", pads.size === 1 && fbPads.size === 1 && pads.has([...fbPads][0]),
    "cards=" + [...pads].join("|") + " feedback=" + [...fbPads].join("|"));
  const borders = new Set(feedback.map((r) => r.borderLeftColor));
  ok("feedback card " + theme + ": the verdict rides the left border", borders.has(VERDICT_BORDER.approved) && borders.has(VERDICT_BORDER.changesRequested),
    [...borders].join("|"));
}

// DETAIL_HEAD_SIZE is --fs-h1 at the 16px root, the size the page layer's own h1 carries.
const DETAIL_HEAD_SIZE = "36px";

// DETAIL_HEAD_SHAPE is the head's one chip slot: the h1 subject, then chips.
const DETAIL_HEAD_SHAPE = /^h1\.subject( > span\.chip[a-z0-9.-]*)*$/;

// checkDetailHead asserts a detail head is a card head with one chip slot and an
// h1 subject at the page layer's own heading size. head is the detail route's
// ".detail > .card-head" record, subject its h1's.
function checkDetailHead(theme, head, subject) {
  if (!head || !subject) {
    ok("detail head " + theme + ": both records captured", false, "head=" + !!head + " subject=" + !!subject);
    return;
  }
  const shapes = head.map((r) => r.shape);
  ok("detail head " + theme + ": the h1 subject leads, the chips follow", shapes.every((s) => DETAIL_HEAD_SHAPE.test(s)), shapes.join(" | "));
  const sizes = new Set(subject.map((r) => r.fontSize));
  ok("detail head " + theme + ": the subject takes the page's h1 size", sizes.size === 1 && sizes.has(DETAIL_HEAD_SIZE), [...sizes].join("|"));
}

// TOPBAR_SHAPE is the detail top bar: the back link, then the one controls row.
const TOPBAR_SHAPE = "a.back > div.page-actions";

// ACTIONS_SHAPE is that row: the raw toggle, then the copy-link control.
const ACTIONS_SHAPE = "div.body-modes.view-modes > button.share-link";

// checkDetailActions asserts the raw toggle and the copy-link control share one
// row. topbar is the detail route's ".detail > .detail-topbar" record, actions
// its ".page-actions" child's.
function checkDetailActions(theme, topbar, actions) {
  if (!topbar || !actions) {
    ok("detail actions " + theme + ": both records captured", false, "topbar=" + !!topbar + " actions=" + !!actions);
    return;
  }
  ok("detail actions " + theme + ": the back link, then one controls row", topbar.every((r) => r.shape === TOPBAR_SHAPE),
    topbar.map((r) => r.shape).join(" | "));
  ok("detail actions " + theme + ": the raw toggle and the copy link share it", actions.every((r) => r.shape === ACTIONS_SHAPE),
    actions.map((r) => r.shape).join(" | "));
}

// META_SHAPE is the meta row skeleton the parity fixture names: the author, the
// time, then the hash link, with the edited marker as the one trailing bit.
const META_SHAPE = new RegExp("^" + FIX.metaRow.bits.map((c, i) => (i === 2 ? "a." : "span.") + c).join(" > ") + "( > span\\.edited)?$");

// checkMetaRow asserts every author-led meta row carries that skeleton. metas is
// a list route's ".meta" record; rows built from other parts are left alone.
function checkMetaRow(theme, metas) {
  if (!metas) {
    ok("meta row " + theme + ": records captured", false, "no .meta record");
    return;
  }
  const led = metas.map((r) => r.shape).filter((s) => s.startsWith("span.author"));
  ok("meta row " + theme + ": the author, the time and the hash in that order", led.length > 0 && led.every((s) => META_SHAPE.test(s)), led.join(" | "));
}

// checkEditedBit asserts the edited marker is a plain meta bit, never a chip.
// edited is a list route's ".edited" record, chips its ".chip" record.
function checkEditedBit(theme, edited, chips) {
  if (!edited || !chips) {
    ok("edited marker " + theme + ": both records captured", false, "edited=" + !!edited + " chips=" + !!chips);
    return;
  }
  const radii = new Set(chips.map((r) => r.borderRadius));
  ok("edited marker " + theme + ": no pill radius, no chip fill", edited.every((r) => !radii.has(r.borderRadius) && r.backgroundColor === "rgba(0, 0, 0, 0)"),
    edited.map((r) => r.borderRadius + " " + r.backgroundColor).join("|"));
}

// ERR_STYLE pins the C.2 tokens the error notice carries: --danger text on a
// --danger-t1 fill, --pad-panel padding at the 16px root, --r-panel corners.
const ERR_STYLE = { color: "rgb(207, 34, 46)", padding: "9.6px 14.4px", borderRadius: "10px" };

// checkErr asserts the error notice renders from those tokens. err is the
// missing-branch route's ".err" record.
function checkErr(theme, err) {
  if (!err) {
    ok("err notice " + theme + ": record captured", false, "no .err record");
    return;
  }
  const off = Object.keys(ERR_STYLE).filter((k) => err.some((r) => r[k] !== ERR_STYLE[k]));
  ok("err notice " + theme + ": danger text, panel padding and radius from the tokens", off.length === 0,
    off.map((k) => k + "=" + err.map((r) => r[k]).join("|")).join(" "));
  ok("err notice " + theme + ": the danger tint fills it", err.every((r) => r.backgroundColor !== "rgba(0, 0, 0, 0)"),
    err.map((r) => r.backgroundColor).join("|"));
}

// WIDTH_EXPR reports the document against the viewport and names the first box
// crossing the right edge. A fixed element is chrome, not content.
const WIDTH_EXPR = `(function () {
  var vw = document.documentElement.clientWidth;
  var wide = "";
  var all = document.querySelectorAll("body *");
  for (var i = 0; i < all.length && !wide; i++) {
    var el = all[i], box = el.getBoundingClientRect();
    if (!box.width && !box.height) continue;
    if (getComputedStyle(el).position === "fixed") continue;
    if (box.right > vw + 1) wide = el.tagName.toLowerCase() + "." + String(el.className || "").trim().replace(/\\s+/g, ".") + " right=" + Math.round(box.right);
  }
  return { vw: vw, scrollWidth: document.documentElement.scrollWidth, wide: wide };
})()`;

// checkPhoneWidth asserts no route runs past a phone viewport, in the served
// document and in the app it boots into.
async function checkPhoneWidth(bin, item) {
  const routes = [
    { name: "front page", url: "" },
    { name: "front page served", url: "?nojs=1" },
    { name: "list page", url: "issues/index.html" },
    { name: "list page served", url: "issues/index.html?nojs=1" },
    { name: "item page", url: item },
    { name: "item page served", url: item + "?nojs=1" },
    { name: "code route", url: "#/code" },
  ];
  const browser = cdp.launch(bin);
  try {
    for (const route of routes) {
      const page = await cdp.open(browser, PHONE_WIDTH, PHONE_HEIGHT, "light");
      await cdp.load(browser, page, BASE + route.url, BUDGET);
      const got = await cdp.evaluate(browser, page, WIDTH_EXPR);
      await cdp.close(browser, page);
      ok("phone width " + route.name + ": the content stays inside " + PHONE_WIDTH + "px",
        got.vw === PHONE_WIDTH && got.scrollWidth <= PHONE_WIDTH && !got.wide,
        "viewport=" + got.vw + " document=" + got.scrollWidth + " " + got.wide);
    }
  } finally {
    await cdp.quit(browser);
  }
}

// NAV_FOOTER_EXPR reports the sidebar tree's own bound and any nav row painted
// under the pinned brand credit. elementsFromPoint sees what is on screen, so a
// row the tree's scroll box clips is not a hit.
const NAV_FOOTER_EXPR = `(function () {
  var foot = document.querySelector(".nav-footer"), slot = document.getElementById("nav-tree-slot");
  if (!foot || !slot) return { found: false };
  var cs = getComputedStyle(slot), b = foot.getBoundingClientRect(), hits = {};
  for (var y = Math.ceil(b.top) + 1; y < b.bottom - 1; y += 4) {
    for (var x = Math.ceil(b.left) + 1; x < b.right - 1; x += 8) {
      var stack = document.elementsFromPoint(x, y);
      for (var i = 0; i < stack.length; i++) {
        var el = stack[i];
        if (el.closest(".nav-footer")) continue;
        if (el.matches("#nav a, #nav .nav-section, .tree-row")) hits[(el.textContent || "").trim().slice(0, 40)] = 1;
      }
    }
  }
  return { found: true, maxHeight: cs.maxHeight, overflowY: cs.overflowY, rows: slot.querySelectorAll(".tree-row").length,
    clientHeight: slot.clientHeight, hits: Object.keys(hits) };
})()`;

// checkNavFooter asserts the sidebar tree is bounded and no nav row runs under
// the credit pinned at the sidebar's bottom.
async function checkNavFooter(bin) {
  const browser = cdp.launch(bin);
  try {
    const page = await cdp.open(browser, WIDTH, HEIGHT, "light");
    await cdp.load(browser, page, BASE + "#/code", BUDGET);
    const got = await cdp.evaluate(browser, page, NAV_FOOTER_EXPR);
    await cdp.close(browser, page);
    if (!got.found) {
      ok("sidebar tree: the code route fills the slot", false, "no .nav-footer or #nav-tree-slot");
      return;
    }
    const bound = got.maxHeight !== "none" && parseFloat(got.maxHeight) > 0 && got.overflowY === "auto";
    ok("sidebar tree: it scrolls inside its own box", bound, "max-height=" + got.maxHeight + " overflow-y=" + got.overflowY);
    ok("sidebar tree: the box holds the cap", got.clientHeight <= parseFloat(got.maxHeight), "height=" + got.clientHeight + " cap=" + got.maxHeight);
    ok("sidebar tree: no nav row runs under the credit at " + WIDTH + "x" + HEIGHT, got.hits.length === 0, got.hits.join(" | "));
  } finally {
    await cdp.quit(browser);
  }
}

// capture runs one route in one theme and returns the probe's record.
function capture(bin, hash, flags) {
  const args = ["--headless", "--disable-gpu", "--hide-scrollbars",
    "--window-size=" + WIDTH + "," + HEIGHT, "--virtual-time-budget=" + BUDGET,
    ...flags, "--dump-dom", BASE + "?probe=1" + hash];
  const dom = execFileSync(bin, args, { encoding: "utf8", maxBuffer: 64 * 1024 * 1024, stdio: ["ignore", "pipe", "ignore"] });
  const m = /data-gs-styles="([^"]*)"/.exec(dom);
  if (!m) return null;
  const unescaped = m[1].replace(/&quot;/g, '"').replace(/&gt;/g, ">").replace(/&lt;/g, "<").replace(/&amp;/g, "&");
  return JSON.parse(unescaped);
}

// resolveShort finds a pull request's short ref by its subject, so a rebuild
// that renumbers or reorders the list still lands on the same item.
function resolveShort(subject) {
  const list = execFileSync("curl", ["-s", BASE + "prs/index.html"], { encoding: "utf8" });
  const rows = list.split('<div class="card"');
  for (const row of rows) {
    if (!row.includes(subject)) continue;
    const href = /href="\.\.\/i\/([0-9a-f]+)\.html"/.exec(row);
    if (href) return href[1];
  }
  return "";
}

async function main() {
  const bin = chrome.find();
  if (!bin) {
    console.log("SKIP: no Chrome found (set CHROME to override)");
    console.log("\n0 passed, 0 failed");
    process.exit(0);
  }
  const update = process.env.GS_STYLES_UPDATE === "1";
  if (update) fs.mkdirSync(DIR, { recursive: true });
  const listCards = {}, listMetas = {}, listChips = {}, listEdited = {}, feedbackCards = {}, detailHeads = {}, detailSubjects = {}, errNotices = {};
  const detailTopbars = {}, detailActions = {};
  let detailShort = "";
  for (const route of ROUTES) {
    let hash = route.hash;
    if (!hash) {
      detailShort = resolveShort(route.subject);
      hash = detailShort ? "#commit:" + detailShort + "@gitmsg/review" : "";
      if (!hash) { ok(route.name + ": resolved by subject", false, "no row matching " + JSON.stringify(route.subject)); continue; }
    }
    for (const [theme, flags] of Object.entries(THEMES)) {
      const got = capture(bin, hash, flags);
      if (!got) { ok(route.name + " " + theme + ": probe returned data", false, "no data-gs-styles on " + hash); continue; }
      if (route.name === "issues") { listCards[theme] = got[".card"]; listMetas[theme] = got[".meta"]; listChips[theme] = got[".chip"]; listEdited[theme] = got[".edited"]; }
      if (route.name === "branch-missing") errNotices[theme] = got[".err"];
      if (route.name === "pr-detail") {
        feedbackCards[theme] = got[".card.feedback"];
        detailHeads[theme] = got[".detail > .card-head"];
        detailSubjects[theme] = got[".detail > .card-head > h1.subject"];
        detailTopbars[theme] = got[".detail > .detail-topbar"];
        detailActions[theme] = got[".detail > .detail-topbar > .page-actions"];
      }
      const file = path.join(DIR, route.name + "." + theme + ".json");
      if (update) { fs.writeFileSync(file, JSON.stringify(got, null, 2) + "\n"); continue; }
      if (!fs.existsSync(file)) { ok(route.name + " " + theme + ": baseline exists", false, "missing " + path.basename(file) + "; capture with GS_STYLES_UPDATE=1"); continue; }
      const want = JSON.parse(fs.readFileSync(file, "utf8"));
      const diffs = [];
      for (const sel of new Set([...Object.keys(want), ...Object.keys(got)])) {
        const a = want[sel], b = got[sel];
        if (!a) { diffs.push(sel + ": appeared"); continue; }
        if (!b) { diffs.push(sel + ": vanished"); continue; }
        const av = a.map((r) => JSON.stringify(r)), bv = b.map((r) => JSON.stringify(r));
        for (const v of av) if (!bv.includes(v)) diffs.push(sel + ": variant gone " + v.slice(0, 120));
        for (const v of bv) if (!av.includes(v)) diffs.push(sel + ": variant new " + v.slice(0, 120));
      }
      ok(route.name + " " + theme + ": styles and structure match the baseline", diffs.length === 0, diffs.slice(0, 6).join("; "));
    }
  }
  if (!update) for (const theme of Object.keys(THEMES)) {
    checkFeedbackCard(theme, listCards[theme], feedbackCards[theme]);
    checkMetaRow(theme, listMetas[theme]);
    checkEditedBit(theme, listEdited[theme], listChips[theme]);
    checkErr(theme, errNotices[theme]);
    checkDetailHead(theme, detailHeads[theme], detailSubjects[theme]);
    checkDetailActions(theme, detailTopbars[theme], detailActions[theme]);
  }
  if (!update) {
    ok("phone width: the item page resolved", detailShort !== "", "no pull request row to open");
    if (detailShort) await checkPhoneWidth(bin, "i/" + detailShort + ".html");
    await checkNavFooter(bin);
  }
  if (update) console.log("baselines written to " + DIR);
  console.log("\n" + pass + " passed, " + fail + " failed");
  process.exit(fail ? 1 : 0);
}
main().catch((err) => { console.error(err); process.exit(1); });

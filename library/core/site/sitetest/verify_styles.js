// verify_styles.js - computed-style and structure assertions against a real
// browser, the one thing the DOM shim cannot check. Skips without Chrome.
const fs = require("fs");
const path = require("path");
const { execFileSync } = require("child_process");
const chrome = require("./chrome.js");

const ORIGIN = process.env.GS_SITE_ORIGIN || "http://localhost:8000";
const BASE = ORIGIN + "/thread-demo/";
const DIR = path.join(__dirname, "styles");
const WIDTH = 1280, HEIGHT = 900, BUDGET = 9000;
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

// resolveDetail finds a pull request's route by its subject, so a rebuild that
// renumbers or reorders the list still lands on the same item.
function resolveDetail(subject) {
  const list = execFileSync("curl", ["-s", BASE + "prs/index.html"], { encoding: "utf8" });
  const rows = list.split('<div class="card"');
  for (const row of rows) {
    if (!row.includes(subject)) continue;
    const href = /href="\.\.\/i\/([0-9a-f]+)\.html"/.exec(row);
    if (href) return "#commit:" + href[1] + "@gitmsg/review";
  }
  return "";
}

function main() {
  const bin = chrome.find();
  if (!bin) {
    console.log("SKIP: no Chrome found (set CHROME to override)");
    console.log("\n0 passed, 0 failed");
    process.exit(0);
  }
  const update = process.env.GS_STYLES_UPDATE === "1";
  if (update) fs.mkdirSync(DIR, { recursive: true });
  const listCards = {}, feedbackCards = {}, detailHeads = {}, detailSubjects = {};
  for (const route of ROUTES) {
    let hash = route.hash;
    if (!hash) {
      hash = resolveDetail(route.subject);
      if (!hash) { ok(route.name + ": resolved by subject", false, "no row matching " + JSON.stringify(route.subject)); continue; }
    }
    for (const [theme, flags] of Object.entries(THEMES)) {
      const got = capture(bin, hash, flags);
      if (!got) { ok(route.name + " " + theme + ": probe returned data", false, "no data-gs-styles on " + hash); continue; }
      if (route.name === "issues") listCards[theme] = got[".card"];
      if (route.name === "pr-detail") {
        feedbackCards[theme] = got[".card.feedback"];
        detailHeads[theme] = got[".detail > .card-head"];
        detailSubjects[theme] = got[".detail > .card-head > h1.subject"];
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
    checkDetailHead(theme, detailHeads[theme], detailSubjects[theme]);
  }
  if (update) console.log("baselines written to " + DIR);
  console.log("\n" + pass + " passed, " + fail + " failed");
  process.exit(fail ? 1 : 0);
}
main();

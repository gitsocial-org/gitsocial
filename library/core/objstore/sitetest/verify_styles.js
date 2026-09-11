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
  for (const route of ROUTES) {
    let hash = route.hash;
    if (!hash) {
      hash = resolveDetail(route.subject);
      if (!hash) { ok(route.name + ": resolved by subject", false, "no row matching " + JSON.stringify(route.subject)); continue; }
    }
    for (const [theme, flags] of Object.entries(THEMES)) {
      const got = capture(bin, hash, flags);
      if (!got) { ok(route.name + " " + theme + ": probe returned data", false, "no data-gs-styles on " + hash); continue; }
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
  if (update) console.log("baselines written to " + DIR);
  console.log("\n" + pass + " passed, " + fail + " failed");
  process.exit(fail ? 1 : 0);
}
main();

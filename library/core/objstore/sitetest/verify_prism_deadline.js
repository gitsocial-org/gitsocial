// verify_prism_deadline.js - a stalled prism.js must cost a late highlight,
// never the view.
//
// prism.js is an optional enhancement: every consumer renders plain text first
// and upgrades in place. But two waits sit on it — a file route awaits the
// tokenizer before building its pane, and a page entry's reveal handshake awaits
// highlightsSettled() — and with no deadline on either, a GET that stalls (a
// proxy hiccup, a half-open connection: not a 404, which was always handled)
// leaves the route hanging and the reveal never firing, until the boot watchdog
// throws away a fully rendered app over a syntax highlighter.
//
// Its own suite because ensurePrism caches its one-shot load for the lifetime of
// the module: only a process that owns the FIRST fetch of prism.js can stall it.
require("./shim.js");
require("../site/icons.js");
const GS = require("../site/gs-app.js");
const { setHash, viewNode, textOf } = global.__shim;
const ORIGIN = process.env.GS_SITE_ORIGIN || "http://localhost:8000";
const BASE = ORIGIN + "/thread-demo/";
let pass = 0, fail = 0;
const ok = (n, c, e) => { (c ? pass++ : fail++); console.log((c ? "PASS " : "FAIL ") + n + (!c && e ? " :: " + e : "")); };
const wait = (ms) => new Promise((r) => setTimeout(r, ms));
// findClass collects the rendered elements carrying a class, so "highlighted"
// is read off the DOM rather than inferred.
function findClass(node, cls, out) { out = out || []; for (const c of (node && node._children) || []) { if (c && c.nodeType === 1) { if (c._cls && c._cls.has(cls)) out.push(c); findClass(c, cls, out); } } return out; }

// STALL_MS holds prism.js well past the reader's deadline and then lets it
// through, so the same run can assert both halves: the view does not wait for
// it, and the upgrade still lands once it finally arrives.
const STALL_MS = 6000;
// DEADLINE_MS mirrors gs-render.js PRISM_DEADLINE_MS. Asserted with slack, since
// the point is that SOME bound exists well inside the stall, not its exact value.
const DEADLINE_MS = 2000;
const realFetch = global.fetch;
let prismFetches = 0;
global.fetch = async (url, opts) => {
  if (/\/prism\.js(?:\?|$)/.test(String(url))) { prismFetches++; await wait(STALL_MS); }
  return realFetch(url, opts);
};

async function main() {
  await wait(300); // the auto-run init home route: no code on it, no tokenizer

  // A file route in a language that needs highlighting. The tokenizer's fetch
  // starts with the blob's own, and the pane is built after awaiting it — with a
  // deadline, so a stall costs a plain-text first paint instead of the file.
  setHash("#file:hello.py@main");
  const routeStart = Date.now();
  await GS.route(GS.newContext(BASE));
  const routeMs = Date.now() - routeStart;
  ok("the stall landed on the path under test (prism.js fetched " + prismFetches + "x)", prismFetches === 1, "fetches=" + prismFetches);
  ok("a file route completes while prism.js is still stalled (" + routeMs + " ms of a " + STALL_MS + " ms stall)",
    routeMs < STALL_MS - 1000, "route took " + routeMs + " ms");
  ok("and renders the blob as plain text", /def main/.test(textOf(viewNode)) && findClass(viewNode, "token").length === 0,
    findClass(viewNode, "token").length + " token spans");

  // The reveal's own wait. This is the one that mattered most: it gates
  // __gsOnFirstView, so without a bound the page-entry boot sits on its loading
  // state until BOOT_MAX_MS discards a finished app.
  const settleStart = Date.now();
  await GS.highlightsSettled();
  const settleMs = Date.now() - settleStart;
  ok("the reveal's wait resolves inside the deadline (" + settleMs + " ms)", settleMs < DEADLINE_MS + 1000, "waited " + settleMs + " ms");
  ok("the whole route-plus-reveal wait finished before prism.js did",
    Date.now() - routeStart < STALL_MS, "elapsed " + (Date.now() - routeStart) + " ms of " + STALL_MS);

  // Degraded, not disabled: once the stalled fetch finally lands, the tokenizer
  // is in the page and highlights as it always did.
  for (let i = 0; i < 100 && !(global.window.Prism && global.window.Prism.tokenize); i++) await wait(100);
  ok("the tokenizer still loads once the stalled fetch lands", !!(global.window.Prism && global.window.Prism.tokenize));
  const parent = GS.el("code", {}, []);
  GS.highlightTo(parent, "const x = 1;\n", "javascript");
  ok("and a later block highlights from it", findClass(parent, "token").length > 0, findClass(parent, "token").length + " token spans");

  console.log("\n" + pass + " passed, " + fail + " failed");
  process.exit(fail ? 1 : 0);
}
main().catch((e) => { console.error(e); process.exit(1); });

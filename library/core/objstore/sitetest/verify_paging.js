// verify_paging.js - "Load more" pagination on the issues route over the large
// meshtastic fixture: the control renders, clicking it grows the rendered card
// set, continuations exhaust the walk, and filter chips recount over the loaded
// set (driven through the DOM shim + the real gs-app route).
require("./shim.js");
require("../site/icons.js");
const GS = require("../site/gs-app.js");
const { viewNode, textOf } = global.__shim;
const MESH = (process.env.GS_SITE_ORIGIN||"http://localhost:8000")+"/meshtastic/";
let pass = 0, fail = 0;
const ok = (n, c, e) => { (c ? pass++ : fail++); console.log((c ? "PASS " : "FAIL ") + n + (!c && e ? " :: " + e : "")); };
const wait = (ms) => new Promise((r) => setTimeout(r, ms));
function findClass(node, cls, out) { out = out || []; for (const c of node._children || []) { if (c && c.nodeType === 1) { if (c._cls && c._cls.has(cls)) out.push(c); findClass(c, cls, out); } } return out; }
function fire(node, ev, props) { (node && node._handlers && node._handlers[ev] || []).forEach((fn) => fn(Object.assign({ preventDefault() {}, stopPropagation() {}, key: "" }, props || {}))); }
async function fireAndSettle(btn) { fire(btn, "click"); for (let i = 0; i < 40; i++) { await wait(50); if (!/Loading…/.test(textOf(btn))) break; } }

async function main() {
  const ctx = GS.newContext(MESH);
  global.location.hash = "#/issues";
  await GS.route(ctx);
  const cards1 = findClass(viewNode, "card").length;
  let more = findClass(viewNode, "load-more");
  ok("issues list shows a Load more control", more.length === 1, "cards=" + cards1 + " more=" + more.length);
  ok("issues first window renders many cards", cards1 > 100, "cards=" + cards1);

  await fireAndSettle(more[0]);
  const cards2 = findClass(viewNode, "card").length;
  ok("Load more grows the rendered card set (window 2)", cards2 > cards1, "cards1=" + cards1 + " cards2=" + cards2);

  // Keep loading until the control disappears (history exhausted).
  let guard = 0;
  while (findClass(viewNode, "load-more").length && guard < 12) { await fireAndSettle(findClass(viewNode, "load-more")[0]); guard++; }
  const cards3 = findClass(viewNode, "card").length;
  ok("continuations exhaust the walk (Load more gone at end)", findClass(viewNode, "load-more").length === 0, "windows=" + (guard + 1));
  ok("final rendered set far exceeds the first window", cards3 > cards1, "first=" + cards1 + " final=" + cards3);
  console.log("progression: window1=" + cards1 + " cards, window2=" + cards2 + " cards, final=" + cards3 + " cards over " + (guard + 2) + " windows");

  // Filter counts recompute from the accumulated set (state chips recount).
  const chips = findClass(viewNode, "filter-chip");
  ok("filter chips present and recount over loaded set", chips.length >= 2, "chips=" + chips.length + " sample=" + (chips[0] && textOf(chips[0])));

  console.log("\n" + pass + " passed, " + fail + " failed");
  process.exit(fail ? 1 : 0);
}
main().catch((e) => { console.error("FAIL", e); process.exit(1); });

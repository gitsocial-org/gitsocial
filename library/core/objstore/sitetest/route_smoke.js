// route_smoke.js - drive the real route() through the DOM+DOMParser shim on the
// live gitsocial bucket: home README, .md blob toggle, image blob, releases order.
require("./shim.js"); // sets up global document/window/location/DOMParser
const GS = require("../site/gs-app.js");
const { viewNode, textOf, findTag, setHash } = global.__shim;

let pass = 0, fail = 0;
const ok = (n, c, e) => { (c ? pass++ : fail++); console.log((c ? "PASS " : "FAIL ") + n + (!c && e ? " :: " + e : "")); };
const wait = (ms) => new Promise((r) => setTimeout(r, ms));

async function run(hash) { setHash(hash); await GS.route(global.__ctx); await wait(500); }

async function main() {
  await wait(1500); // let gs-app init()'s boot route settle before driving routes
  global.__ctx = GS.newContext((process.env.GS_SITE_ORIGIN||"http://localhost:8000")+"/gitsocial/");

  await run("#/");
  ok("home renders README (About heading)", findTag(viewNode, "h2").some((h) => textOf(h).includes("About")));
  ok("home renders centered div", findTag(viewNode, "div").some((d) => d.getAttribute("align") === "center"));
  ok("home resolves an in-bucket image to blob URL", findTag(viewNode, "img").some((i) => /^blob:/.test(i.getAttribute("src") || "")));

  await run("#file:documentation/CLI.md@main");
  ok("md blob has Raw toggle", findTag(viewNode, "button").some((b) => textOf(b) === "Raw" && b.getAttribute("aria-pressed") === "false"));
  ok("md blob defaults to rendered markdown", findTag(viewNode, "div").some((d) => d._cls && d._cls.has("markdown")));

  await run("#file:documentation/images/gitsocial-icon.png@main");
  ok("image blob renders <img> (not binary note)", findTag(viewNode, "img").length === 1 && !textOf(viewNode).includes("Binary file"));
  ok("image blob img has blob: src", /^blob:/.test((findTag(viewNode, "img")[0] || {}).getAttribute ? findTag(viewNode, "img")[0].getAttribute("src") : ""));

  await run("#file:go.mod@main");
  ok("code blob has fullscreen button", findTag(viewNode, "button").some((b) => b._cls && b._cls.has("fs-btn")));

  await run("#/releases");
  const cards = findTag(viewNode, "div").filter((d) => d._cls && d._cls.has("card"));
  const firstText = cards.length ? textOf(cards[0]) : "";
  ok("releases newest-first: first card is v0.13.0", /0\.13\.0/.test(firstText), firstText.slice(0, 40));

  console.log("\n" + pass + " passed, " + fail + " failed");
  process.exit(fail ? 1 : 0);
}
main().catch((e) => { console.error("THREW:", e); process.exit(1); });

// unit_fetch_abort.js - a navigation aborts the previous route's in-flight transfers: the aborted read is not cached as a failure, and no error reaches the view.
require("./shim.js");
const BASE = "http://bucket.test/repo/";
const TIP = "a".repeat(40);
const served = {
  "HEAD": "ref: refs/heads/main\n",
  "refs/heads/main": TIP + "\n",
  ".gitsocial/refs.json": JSON.stringify({ "refs/heads/main": TIP }),
};
// The stubbed bucket: a loose object request hangs until its signal aborts; every other key answers from the map or is absent.
const objectSignals = [];
let aborted = 0;
global.fetch = (url, opts) => {
  const key = String(url).slice(BASE.length);
  if (/^objects\/[0-9a-f]{2}\//.test(key)) {
    return new Promise((_, reject) => {
      const signal = opts && opts.signal;
      objectSignals.push(signal || null);
      if (signal) signal.addEventListener("abort", () => { aborted++; const e = new Error("aborted"); e.name = "AbortError"; reject(e); });
    });
  }
  const body = served[key];
  const buf = Buffer.from(body === undefined ? "" : body);
  const res = { status: body === undefined ? 404 : 200, ok: body !== undefined, headers: { get: () => null }, arrayBuffer: async () => buf.buffer.slice(buf.byteOffset, buf.byteOffset + buf.byteLength) };
  return Promise.resolve(res);
};
require("../assets/icons.js");
const GS = require("../assets/gs-app.js");
const { viewNode, textOf, setHash } = global.__shim;
let pass = 0, fail = 0;
const ok = (n, c, e) => { (c ? pass++ : fail++); console.log((c ? "PASS " : "FAIL ") + n + (!c && e ? " :: " + e : "")); };
const wait = (ms) => new Promise((r) => setTimeout(r, ms));

async function main() {
  const ctx = GS.newContext(BASE);
  setHash("#branch:main");
  const first = GS.route(ctx);
  // The branch log walks from the tip, so its first object read hangs on the stub.
  for (let i = 0; i < 100 && !objectSignals.length; i++) await wait(10);
  ok("the first route started an object transfer with a signal", objectSignals.length === 1 && !!objectSignals[0], JSON.stringify(objectSignals.map((s) => !!s)));
  ok("the transfer is in flight", aborted === 0);

  setHash("#/lists");
  await GS.route(ctx);
  ok("the navigation aborted the first route's transfer", aborted === 1 && objectSignals[0].aborted, "aborted=" + aborted);
  await first;
  await wait(20);
  const text = textOf(viewNode);
  ok("the second route owns the view", text.includes("Lists") || text.includes("lists"), JSON.stringify(text.slice(0, 120)));
  ok("no error reached the view", !text.includes("Error") && !text.includes("aborted"), JSON.stringify(text.slice(0, 120)));
  ok("the aborted read is not cached as a failure", !ctx.objects.has(TIP));
  ok("the second route's signal is live", ctx.signal && !ctx.signal.aborted);

  console.log("\n" + pass + " passed, " + fail + " failed");
  process.exit(fail ? 1 : 0);
}
main().catch((e) => { console.error(e); process.exit(1); });

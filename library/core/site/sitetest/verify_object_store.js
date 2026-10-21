// verify_object_store.js - a second boot on the same bucket reads from the persistent object store: no object request for an object the first boot resolved, and the stored bytes equal the bucket's bytes.
require("./shim.js");
global.caches = global.__shim.fakeCaches();
require("../assets/icons.js");
const GS = require("../assets/gs-app.js");
const { viewNode, textOf, setHash, settle } = global.__shim;
const origin = process.env.GS_SITE_ORIGIN || "http://localhost:8000";
let pass = 0, fail = 0;
const ok = (n, c, e) => { (c ? pass++ : fail++); console.log((c ? "PASS " : "FAIL ") + n + (!c && e ? " :: " + e : "")); };
const wait = (ms) => new Promise((r) => setTimeout(r, ms));

// Every object transfer is logged: a loose object key or a pack range; the pack listing and the index artifacts are not transfers.
const OBJECT_TRANSFER = /\/objects\/(?:[0-9a-f]{2}\/[0-9a-f]{38}|pack\/)/;
const log = [];
const realFetch = global.fetch;
global.fetch = (url, opts) => { log.push(String(url)); return realFetch(url, opts); };
const transfers = (from) => log.slice(from).filter((u) => OBJECT_TRANSFER.test(u));

// boot drives one route on a fresh context, a new session on the same bucket, and waits for every fetch it started.
async function boot(base, hash) {
  const ctx = GS.newContext(base);
  await GS.objectStore(ctx);
  setHash(hash);
  await GS.route(ctx);
  await settle();
  await wait(200);
  return ctx;
}

async function main() {
  // merged-demo keeps the store on; the thread-demo config turns it off for the Chrome captures.
  const base = origin + "/merged-demo/";
  const head = await GS.headFor(GS.newContext(base));
  ok("the default branch tip is known", !!(head && head.sha), JSON.stringify(head));
  if (!head || !head.sha) { console.log("\n" + pass + " passed, " + (fail + 1) + " failed"); process.exit(1); }
  const route = GS.commitRef(head.sha, "");

  const firstFrom = log.length;
  const first = await boot(base, route);
  const firstTransfers = transfers(firstFrom);
  ok("the first boot reads objects from the bucket", firstTransfers.length > 0, "no object transfer");
  ok("the first boot renders the commit", textOf(viewNode).includes(head.sha.slice(0, 12)), textOf(viewNode).slice(0, 120));

  const secondFrom = log.length;
  await boot(base, route);
  const secondTransfers = transfers(secondFrom);
  ok("the second boot makes no object request", secondTransfers.length === 0, secondTransfers.slice(0, 5).join(" "));
  ok("the second boot renders the same commit", textOf(viewNode).includes(head.sha.slice(0, 12)), textOf(viewNode).slice(0, 120));

  const fromBucket = await GS.getObject(first, head.sha);
  const thirdFrom = log.length;
  const fresh = GS.newContext(base);
  await GS.objectStore(fresh);
  const fromStore = await GS.getObject(fresh, head.sha);
  ok("a stored object is read without a transfer", transfers(thirdFrom).length === 0);
  ok("a stored object equals the bucket object byte for byte", fromStore.type === fromBucket.type && Buffer.from(fromStore.body).equals(Buffer.from(fromBucket.body)),
    fromStore.type + " " + fromStore.body.length + " vs " + fromBucket.type + " " + fromBucket.body.length);

  console.log("\n" + pass + " passed, " + fail + " failed");
  process.exit(fail ? 1 : 0);
}
main().catch((e) => { console.error(e); process.exit(1); });

// unit_notify_seen.js - the site shows each new item once per browser: the seen mark of hashes in localStorage, the baseline, a failed load and blocked storage
require("./shim.js");
const GS = require("../assets/gs-render.js");
let pass = 0, fail = 0;
function eq(a, b, msg) { if (JSON.stringify(a) === JSON.stringify(b)) { pass++; } else { fail++; console.log("FAIL", msg, "got", JSON.stringify(a), "want", JSON.stringify(b)); } }

const BASE = "https://site.example/repo/";
const KEY = "gs-notify-seen:" + BASE;

// memoryStore is a localStorage stand-in over a map.
function memoryStore() {
  const m = new Map();
  return { getItem: (k) => (m.has(k) ? m.get(k) : null), setItem: (k, v) => { m.set(k, String(v)); }, removeItem: (k) => { m.delete(k); } };
}

// post builds a social post item at a commit time.
function post(short, time) {
  return { commit: { short, hash: (short + "0".repeat(40)).slice(0, 40), authorTime: time, authorEmail: "a@example.com" }, header: {}, content: "post " + short, author: "Alice", _ext: "social" };
}

const shorts = (items) => items.map((it) => it.commit.short);

// run runs the notifier step over items on a store and returns the bodies it showed.
async function run(store, items, base) {
  const bodies = [];
  global.window.Notification = function (title, opts) { bodies.push(opts.body); };
  await GS.notifyRun(store, base || BASE, async () => items, async (fresh, all) => {
    GS.showNotifications(GS.notifyMessages(GS.pickNotifyItems(fresh, all, ""), "GitSocial", "repo"), "");
  });
  return bodies;
}

async function main() {
  const store = memoryStore();
  const first = [post("aaaaaaaaaaa1", 100), post("aaaaaaaaaaa2", 90)];
  eq(await run(store, first), [], "the first run in a browser shows nothing");
  eq(JSON.parse(store.getItem(KEY)), ["aaaaaaaaaaa1", "aaaaaaaaaaa2"], "the first run records the hashes as the baseline");

  const second = [post("bbbbbbbbbbb1", 110), ...first];
  eq(await run(store, second), ["Alice posted · repo"], "a second run shows the new item once");
  eq(await run(store, second), [], "a third run with the same items shows nothing");

  eq(await run(store, [post("ccccccccccc1", 50), ...second]), ["Alice posted · repo"], "an item with an older date than the others is still new");

  const before = store.getItem(KEY);
  let threw = null;
  try { await GS.notifyRun(store, BASE, async () => { throw new Error("offline"); }, async () => {}); } catch (e) { threw = e; }
  eq([!!threw, store.getItem(KEY)], [true, before], "a failed load keeps the mark");
  const pending = [post("ddddddddddd1", 120), post("ddddddddddd2", 121)];
  eq(await run(store, [...pending, ...second]), ["Alice posted · repo", "Alice posted · repo"], "after a failed load the next run shows the items it missed");

  const night = memoryStore();
  night.setItem(KEY, JSON.stringify(["aaaaaaaaaaa1", "aaaaaaaaaaa2"]));
  eq(await run(night, second), ["Alice posted · repo"], "a grant with an existing mark shows the pending items");

  eq(await run(store, second, "https://other.example/"), [], "another site base has its own mark and starts with a baseline");

  store.setItem(KEY, "not json");
  eq(await run(store, second), [], "an unreadable mark is a baseline");
  eq(await run(store, [post("eeeeeeeeeee1", 130), ...second]), ["Alice posted · repo"], "and the run after it shows new items again");

  const many = Array.from({ length: 600 }, (_, i) => post(String(i).padStart(12, "f"), i));
  await run(store, many);
  eq(JSON.parse(store.getItem(KEY)).length, 500, "the mark keeps at most 500 hashes");

  const blocked = { getItem() { throw new Error("blocked"); }, setItem() { throw new Error("blocked"); }, removeItem() { throw new Error("blocked"); } };
  let loads = 0, err = null, got = null;
  try { got = await GS.notifyRun(blocked, BASE, async () => { loads++; return second; }, async () => {}); } catch (e) { err = e; }
  eq([err === null, got && got.length, loads], [true, 0, 0], "blocked storage shows nothing and loads nothing without a throw");
  eq((await GS.notifyRun(null, BASE, async () => second, async () => {})).length, 0, "no storage shows nothing");

  console.log(pass + " passed, " + fail + " failed");
  process.exit(fail ? 1 : 0);
}
main().catch((e) => { console.error(e); process.exit(1); });

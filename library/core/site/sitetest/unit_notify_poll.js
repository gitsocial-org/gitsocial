// unit_notify_poll.js - the notifier runs once on load with permission, folds a change during a run into one more run, and does not run without permission
require("./shim.js");
let pass = 0, fail = 0;
const ok = (n, c, e) => { (c ? pass++ : fail++); console.log((c ? "PASS " : "FAIL ") + n + (!c && e ? " :: " + e : "")); };
const wait = (ms) => new Promise((r) => setTimeout(r, ms));

// fakeNotification records each notification body.
function fakeNotification(permission) {
  const api = function (title, opts) { api.shown.push(opts.body); };
  api.permission = permission;
  api.shown = [];
  api.requestPermission = () => Promise.resolve(permission);
  return api;
}

// post builds a social post item.
function post(short) {
  return { commit: { short, hash: (short + "0".repeat(40)).slice(0, 40), authorTime: 100, authorEmail: "a@example.com" }, header: {}, content: "", author: "Alice", _ext: "social" };
}

async function main() {
  global.window.isSecureContext = true;
  global.document.visibilityState = "visible";
  require("../assets/icons.js");
  const GS = require("../assets/gs-app.js");

  // Load: a mark from an earlier visit, and an item that came while no tab was open.
  global.window.Notification = fakeNotification("granted");
  const base = "https://poll.example/a/";
  global.localStorage.setItem("gs-notify-seen:" + base, JSON.stringify(["aaaaaaaaaaa1"]));
  let loads = 0;
  const night = GS.startNotifier({ base }, async () => { loads++; return [post("bbbbbbbbbbb1"), post("aaaaaaaaaaa1")]; });
  GS.startFreshnessWatch({ base, manifestText: "{}" }, night);
  await wait(3300);
  ok("with permission the page runs the notifier once after the baseline load", loads === 1, "loads=" + loads);
  ok("the item from the night shows at the first open", global.window.Notification.shown.length === 1 && global.window.Notification.shown[0] === "Alice posted · a", JSON.stringify(global.window.Notification.shown));

  // Busy: changes during a run give one more run, not one per change.
  const busyBase = "https://poll.example/b/";
  global.localStorage.setItem("gs-notify-seen:" + busyBase, JSON.stringify([]));
  let release = null, busyLoads = 0;
  const busy = GS.startNotifier({ base: busyBase }, async () => { busyLoads++; if (busyLoads === 1) await new Promise((r) => { release = r; }); return []; });
  const first = busy.run();
  await wait(10);
  busy.run(); busy.run(); busy.run();
  release();
  await first;
  await wait(10);
  ok("three changes during a run give one more run", busyLoads === 2, "loads=" + busyLoads);

  // No permission: no run on load and no run on a change.
  global.window.Notification = fakeNotification("default");
  let offLoads = 0;
  const off = GS.startNotifier({ base: "https://poll.example/c/" }, async () => { offLoads++; return []; });
  GS.startFreshnessWatch({ base: "https://poll.example/c/", manifestText: "{}" }, off);
  await off.run();
  await wait(3300);
  ok("without permission the notifier never loads", offLoads === 0, "loads=" + offLoads);

  console.log("\n" + pass + " passed, " + fail + " failed");
  process.exit(fail ? 1 : 0);
}
main().catch((e) => { console.error(e); process.exit(1); });

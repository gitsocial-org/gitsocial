// unit_notify_permission.js - the site asks for notification permission only from the bell panel's button, never on load
require("./shim.js");
let pass = 0, fail = 0;
const ok = (n, c, e) => { (c ? pass++ : fail++); console.log((c ? "PASS " : "FAIL ") + n + (!c && e ? " :: " + e : "")); };
const { textOf } = global.__shim;
const wait = (ms) => new Promise((r) => setTimeout(r, ms));
function findClass(node, cls, out) { out = out || []; for (const c of (node && node._children) || []) { if (c && c.nodeType === 1) { if (c._cls && c._cls.has(cls)) out.push(c); findClass(c, cls, out); } } return out; }
function fire(node, ev) { for (const fn of (node && node._handlers && node._handlers[ev]) || []) fn({ preventDefault() {}, stopPropagation() {} }); }

// fakeNotification records each permission request and each notification; a request grants.
function fakeNotification(permission) {
  const api = function (title, opts) { api.shown.push({ title, opts }); this.close = () => {}; };
  api.permission = permission;
  api.requests = 0;
  api.shown = [];
  api.requestPermission = () => { api.requests++; api.permission = "granted"; return Promise.resolve("granted"); };
  return api;
}

// openPanel clicks the bell and returns the panel it opens, closing any panel before it.
function openPanel() {
  const bell = document.getElementById("notify-bell");
  if (bell.getAttribute("aria-expanded") === "true") fire(bell, "click");
  fire(bell, "click");
  return findClass(document.body, "notify-panel").pop();
}

const panelButton = (panel) => findClass(panel, "pref-btn")[0] || null;

async function main() {
  global.window.isSecureContext = true;
  global.window.Notification = fakeNotification("default");
  require("../assets/icons.js");
  const GS = require("../assets/gs-app.js");
  await wait(3500);
  ok("the page load asks for no permission", global.window.Notification.requests === 0, "requests=" + global.window.Notification.requests);

  const panel = openPanel();
  ok("the bell opens the panel", !!panel);
  ok("the panel says notifications are off", /are off/.test(textOf(panel)), textOf(panel));
  ok("opening the panel asks for no permission", global.window.Notification.requests === 0);
  const btn = panelButton(panel);
  ok("the panel has the turn-on action", !!btn && textOf(btn) === "Turn on notifications", btn ? textOf(btn) : "none");
  fire(btn, "click");
  await wait(10);
  ok("the turn-on action asks for permission once", global.window.Notification.requests === 1, "requests=" + global.window.Notification.requests);
  ok("after the grant the panel says notifications are on and drops the action", /are on/.test(textOf(panel)) && !panelButton(panel), textOf(panel));
  ok("the panel has the email field", findClass(panel, "notify-email").length === 1);

  const granted = openPanel();
  ok("a granted panel has no turn-on action", !panelButton(granted), textOf(granted));

  global.window.Notification = undefined;
  const none = openPanel();
  ok("a browser with no Notification API gets the reason and no action", /cannot show notifications/.test(textOf(none)) && !panelButton(none) && findClass(none, "notify-email").length === 0, textOf(none));

  global.window.Notification = fakeNotification("default");
  global.window.isSecureContext = false;
  const insecure = openPanel();
  ok("a page that is not a secure context gets the reason and no action", /https/.test(textOf(insecure)) && !panelButton(insecure), textOf(insecure));
  global.window.isSecureContext = true;

  const store = global.localStorage;
  global.localStorage = { getItem() { throw new Error("blocked"); }, setItem() { throw new Error("blocked"); }, removeItem() { throw new Error("blocked"); } };
  let nostore = null, threw = null;
  try { nostore = openPanel(); } catch (e) { threw = e; }
  ok("blocked storage gives the reason without a throw", !threw && /cannot keep/.test(textOf(nostore)) && !panelButton(nostore), threw ? String(threw) : textOf(nostore));
  ok("blocked storage gives no email", GS.readNotifyEmail() === "");
  global.localStorage = store;
  ok("no panel state asked for permission", global.window.Notification.requests === 0);

  console.log("\n" + pass + " passed, " + fail + " failed");
  process.exit(fail ? 1 : 0);
}
main().catch((e) => { console.error(e); process.exit(1); });

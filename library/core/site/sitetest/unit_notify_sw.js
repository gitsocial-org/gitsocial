// unit_notify_sw.js - a click on a worker notification focuses a tab of the site and sends it the route, else opens the site at the route
let pass = 0, fail = 0;
function eq(a, b, msg) { if (JSON.stringify(a) === JSON.stringify(b)) { pass++; } else { fail++; console.log("FAIL", msg, "got", JSON.stringify(a), "want", JSON.stringify(b)); } }

const listeners = {};
const opened = [];
let tabs = [];
global.self = {
  addEventListener: (ev, fn) => { listeners[ev] = fn; },
  skipWaiting: () => {},
  clients: { matchAll: async () => tabs, openWindow: async (url) => { opened.push(url); } },
};
require("../assets/gs-sw.js");

// tab builds a window client that records its messages and focus.
function tab(url) { return { url, posted: [], focused: false, postMessage(m) { this.posted.push(m); }, focus() { this.focused = true; return Promise.resolve(this); } }; }

// click dispatches a notificationclick with the data and resolves after its work.
async function click(data) {
  let work = Promise.resolve(), closed = false;
  listeners.notificationclick({ notification: { data, close: () => { closed = true; } }, waitUntil: (p) => { work = p; } });
  await work;
  return closed;
}

async function main() {
  const page = "https://site.example/";
  const nested = tab("https://site.example/other/#/timeline");
  const site = tab(page + "#/issues");
  tabs = [nested, site];
  const closed = await click({ page, route: "#commit:a00000000002" });
  eq([closed, site.focused, site.posted, nested.focused, nested.posted, opened], [true, true, [{ route: "#commit:a00000000002" }], false, [], []], "a click focuses the tab of the page, not a site under it, and sends it the route");
  tabs = [nested];
  await click({ page, route: "#/timeline" });
  eq(opened, [page + "#/timeline"], "with no tab of the page a click opens the page at the route");
  opened.length = 0;
  const viewer = "https://site.example/viewer/?base=https://site.example/repo/";
  const viewerTab = tab(viewer + "#/");
  tabs = [viewerTab];
  await click({ page: viewer, route: "#/timeline" });
  eq([viewerTab.posted, opened], [[{ route: "#/timeline" }], []], "a page with a base override matches its own tab");
  await click({ page, route: "javascript:alert(1)" });
  await click(null);
  eq(opened, [], "a route that is not a hash, or no data, opens nothing");
  console.log(pass + " passed, " + fail + " failed");
  process.exit(fail ? 1 : 0);
}
main();

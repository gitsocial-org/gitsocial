// gs-sw.js - the service worker of the site: the tab shows its notifications through it, and a click opens the item
self.addEventListener("install", () => self.skipWaiting());

// openRoute focuses the tab whose URL without its hash is the page and sends it the route, or opens the page at the route when no such tab is open.
async function openRoute(page, route) {
  const tabs = await self.clients.matchAll({ type: "window", includeUncontrolled: true });
  const tab = tabs.find((c) => typeof c.url === "string" && c.url.split("#")[0] === page);
  if (!tab) return self.clients.openWindow(page + route);
  tab.postMessage({ route });
  return tab.focus();
}

self.addEventListener("notificationclick", (event) => {
  event.notification.close();
  const data = event.notification.data || {};
  if (typeof data.page !== "string" || typeof data.route !== "string" || !data.route.startsWith("#")) return;
  event.waitUntil(openRoute(data.page, data.route));
});

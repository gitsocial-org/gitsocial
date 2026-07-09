// verify_403.js - Item 2: 403-vs-404. A fully-private bucket (403 on everything)
// must produce ONE clear page-level error; a 404-only bucket still renders empty
// and quiet. Drives the real app (gs-app route) through the DOM shim against two
// tiny local servers.
const http = require("http");
require("./shim.js");
require("../site/icons.js");
const GS = require("../site/gs-app.js");
const { viewNode, textOf } = global.__shim;

let pass = 0, fail = 0;
const ok = (n, c, e) => { (c ? pass++ : fail++); console.log((c ? "PASS " : "FAIL ") + n + (!c && e ? " :: " + e : "")); };

function server(status) {
  return new Promise((resolve) => {
    const s = http.createServer((req, res) => { res.statusCode = status; res.end(status === 403 ? "Forbidden" : "Not Found"); });
    s.listen(0, "127.0.0.1", () => resolve(s));
  });
}

async function main() {
  const s403 = await server(403);
  const s404 = await server(404);
  const base403 = "http://127.0.0.1:" + s403.address().port + "/";
  const base404 = "http://127.0.0.1:" + s404.address().port + "/";

  // Fully-private bucket: boot probe (HEAD/ref-mode/manifest) 403s -> error page.
  const ctx403 = GS.newContext(base403);
  global.location.hash = "#/timeline";
  await GS.route(ctx403);
  const t403 = textOf(viewNode);
  ok("403 bucket surfaces the access-disabled error page", /public access appears to be disabled/i.test(t403), t403.slice(0, 120));
  ok("403 error mentions 403 Forbidden", /403 Forbidden/i.test(t403));
  ok("403 does NOT render an empty/'not found' view", !/No activity in this repository|Not found\./i.test(t403));
  const errCls = (function find(n) { for (const c of n._children || []) { if (c && c.nodeType === 1) { if (c._cls && c._cls.has("err")) return true; if (find(c)) return true; } } return false; })(viewNode);
  ok("403 error uses the .err page styling", errCls);

  // 404-only bucket (empty/absent objects): quiet empty state, no error page.
  const ctx404 = GS.newContext(base404);
  global.location.hash = "#/timeline";
  await GS.route(ctx404);
  const t404 = textOf(viewNode);
  ok("404 bucket renders quietly (no forbidden error)", !/public access appears to be disabled/i.test(t404), t404.slice(0, 120));
  ok("404 bucket shows the empty-state text", /No activity in this repository yet/i.test(t404), t404.slice(0, 120));

  // Single missing object degrades quietly: fetchBytes returns null on 404.
  const one = await GS.fetchBytes(base404, "objects/ab/cdef");
  ok("single 404 object -> null (quiet)", one === null);
  // A single 403 object throws the tagged error (caller decides).
  let threw = null; try { await GS.fetchBytes(base403, "objects/ab/cdef"); } catch (e) { threw = e; }
  ok("single 403 object -> throws forbidden-tagged error", threw && threw.forbidden === true);

  s403.close(); s404.close();
  console.log("\n" + pass + " passed, " + fail + " failed");
  process.exit(fail ? 1 : 0);
}
main().catch((e) => { console.error("FAIL", e); process.exit(1); });

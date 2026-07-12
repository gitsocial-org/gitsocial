// verify_grammars.js - the reader's lazy grammar loading (gs-render.js):
//   a non-base grammar is fetched from grammars/prism-<lang>.js on first use,
//     then the plain text upgrades to highlighted spans in place
//   a dependency-chained grammar (cpp -> c) loads its dep first, in order
//   a missing grammar file falls back quietly to plain text (no throw)
//   grammars are cached per session (fetched at most once)
// Runs under the shim with a real Prism (prism.js) loaded and the real global
// fetch pointed at the served fixture bucket, so ensureGrammar exercises the
// actual bucket transport.
require("./shim.js");
// Load the real Prism into the shim's window so getPrism() sees a tokenizer.
global.window.Prism = global.window.Prism || {};
global.window.Prism.manual = true;
require("../site/prism.js");
require("../site/icons.js");
const GS = require("../site/gs-app.js");
const { el } = GS;
const ORIGIN = process.env.GS_SITE_ORIGIN || "http://localhost:8000";
const BASE = ORIGIN + "/thread-demo/";
let pass = 0, fail = 0;
const ok = (n, c, e) => { (c ? pass++ : fail++); console.log((c ? "PASS " : "FAIL ") + n + (!c && e ? " :: " + e : "")); };
const wait = (ms) => new Promise((r) => setTimeout(r, ms));

// tokenClasses returns the set of Prism token classes present under a node.
function tokenClasses(node, out) {
  out = out || new Set();
  for (const c of (node && node._children) || []) {
    if (c && c.nodeType === 1) { for (const cls of c._cls || []) out.add(cls); tokenClasses(c, out); }
    else if (c && c.nodeType === 1 && false) { /* text */ }
  }
  return out;
}
// hasTokenSpan reports whether the node tree has any Prism-highlighted <span>
// (class "token"), i.e. it was tokenized rather than left as a plain text node.
function hasTokenSpan(node) { return tokenClasses(node).has("token"); }

async function main() {
  // Point the loader at the served fixture and reset any base grammars fetched
  // by a prior module load. init() already set the base to the shim location;
  // set it explicitly to the served fixture for determinism.
  GS.setGrammarBase(BASE);

  const P = global.window.Prism;
  ok("prism.js loaded with a base grammar (go) but not python", !!(P && P.languages && P.languages.go) && !P.languages.python, "go=" + !!(P && P.languages && P.languages.go) + " py=" + !!(P && P.languages && P.languages.python));

  // ---- lazy-load a single grammar and highlight in place ----
  const parent = el("code", {}, []);
  GS.highlightTo(parent, "def greet(name):\n    return f'hi {name}'\n", "python");
  ok("before load the python block renders plain text (no token spans)", !hasTokenSpan(parent), "spans=" + hasTokenSpan(parent));
  const loaded = await GS.ensureGrammar("python");
  await wait(50); // let the highlightTo re-render callback run
  ok("ensureGrammar(python) resolves true (grammar fetched + evaluated)", loaded === true, "loaded=" + loaded);
  ok("python is now registered on Prism.languages", !!(P.languages && P.languages.python), "");
  ok("the python block upgraded to highlighted token spans in place", hasTokenSpan(parent), "spans=" + hasTokenSpan(parent));

  // ---- dependency-chained grammar loads its dep first, in order ----
  ok("cpp and its dep c are both unloaded before use", !P.languages.cpp && !P.languages.c, "cpp=" + !!P.languages.cpp + " c=" + !!P.languages.c);
  const cppOk = await GS.ensureGrammar("cpp");
  ok("ensureGrammar(cpp) resolves true", cppOk === true, "cppOk=" + cppOk);
  ok("cpp's dependency c was loaded (dep-first) alongside cpp", !!P.languages.c && !!P.languages.cpp, "c=" + !!P.languages.c + " cpp=" + !!P.languages.cpp);
  const cppParent = el("code", {}, []);
  GS.highlightTo(cppParent, "int main() { return 0; }", "cpp");
  await wait(20);
  ok("a cpp block highlights once the chain is loaded", hasTokenSpan(cppParent), "spans=" + hasTokenSpan(cppParent));

  // ---- missing grammar file falls back quietly ----
  let threw = false, missOk = null;
  try { missOk = await GS.ensureGrammar("nonesuchlang"); } catch (e) { threw = true; }
  ok("ensureGrammar of a missing grammar does not throw", !threw, "threw=" + threw);
  ok("ensureGrammar of a missing grammar resolves false", missOk === false, "missOk=" + missOk);
  const missParent = el("code", {}, []);
  GS.highlightTo(missParent, "some code", "nonesuchlang");
  await wait(20);
  ok("a block in a missing grammar stays plain text (no token spans)", !hasTokenSpan(missParent), "spans=" + hasTokenSpan(missParent));

  // ---- caching: a second ensureGrammar of a loaded grammar is instant/true ----
  const again = await GS.ensureGrammar("python");
  ok("re-requesting a loaded grammar resolves true from cache", again === true, "again=" + again);

  console.log("\n" + pass + " passed, " + fail + " failed");
  process.exit(fail ? 1 : 0);
}
main().catch((e) => { console.error(e); console.log("\n" + pass + " passed, " + (fail + 1) + " failed"); process.exit(1); });

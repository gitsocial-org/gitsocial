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

  // ---- dependency-chained NEW grammar: tsx loads jsx first, then highlights ----
  ok("tsx and its dep jsx are both unloaded before use", !P.languages.tsx && !P.languages.jsx, "tsx=" + !!P.languages.tsx + " jsx=" + !!P.languages.jsx);
  ok("langForPath maps .tsx to the tsx grammar", GS.langForPath("App.tsx") === "tsx", "lang=" + GS.langForPath("App.tsx"));
  const tsxOk = await GS.ensureGrammar("tsx");
  ok("ensureGrammar(tsx) resolves true", tsxOk === true, "tsxOk=" + tsxOk);
  ok("tsx's dependency jsx was loaded (dep-first) alongside tsx", !!P.languages.jsx && !!P.languages.tsx, "jsx=" + !!P.languages.jsx + " tsx=" + !!P.languages.tsx);
  const tsxParent = el("code", {}, []);
  GS.highlightTo(tsxParent, "const App = () => <div className=\"x\">{count}</div>;", GS.langForPath("App.tsx"));
  await wait(20);
  ok("a .tsx block highlights once the tsx->jsx chain is loaded", hasTokenSpan(tsxParent), "spans=" + hasTokenSpan(tsxParent));

  // ---- plain new grammars (scss, hcl): extension detection + lazy highlight ----
  for (const [path, lang, code] of [["style.scss", "scss", "$c: red;\n.a { color: $c; }\n"], ["main.tf", "hcl", "resource \"aws_s3_bucket\" \"b\" {\n  bucket = \"x\"\n}\n"]]) {
    ok("langForPath maps " + path + " to " + lang, GS.langForPath(path) === lang, "lang=" + GS.langForPath(path));
    const par = el("code", {}, []);
    GS.highlightTo(par, code, GS.langForPath(path));
    await GS.ensureGrammar(lang);
    await wait(30);
    ok("a " + lang + " block highlights after lazy load", hasTokenSpan(par), "spans=" + hasTokenSpan(par));
  }

  // ---- basename detection: extensionless Dockerfile / Makefile highlight ----
  // The Dockerfile case pins the bug fix: docker was unreachable from file views
  // when detection was extension-only.
  ok("langForPath resolves Dockerfile via the basename map (docker)", GS.langForPath("Dockerfile") === "docker", "lang=" + GS.langForPath("Dockerfile"));
  ok("langForPath resolves a nested build/Dockerfile", GS.langForPath("build/Dockerfile") === "docker", "lang=" + GS.langForPath("build/Dockerfile"));
  const dockerParent = el("code", {}, []);
  GS.highlightTo(dockerParent, "FROM alpine:3\nRUN echo hi\n", GS.langForPath("Dockerfile"));
  await GS.ensureGrammar("docker");
  await wait(30);
  ok("a Dockerfile blob highlights (basename detection reaches the docker grammar)", hasTokenSpan(dockerParent), "spans=" + hasTokenSpan(dockerParent));
  ok("langForPath resolves Makefile via the basename map (makefile)", GS.langForPath("Makefile") === "makefile", "lang=" + GS.langForPath("Makefile"));
  const makeParent = el("code", {}, []);
  GS.highlightTo(makeParent, "# build\nCC = gcc\nall:\n\techo hi\n", GS.langForPath("Makefile"));
  await GS.ensureGrammar("makefile");
  await wait(30);
  ok("a Makefile blob highlights via basename detection", hasTokenSpan(makeParent), "spans=" + hasTokenSpan(makeParent));

  // ---- vue/svelte fall back to markup (no official Prism grammar exists) ----
  ok("langForPath maps .vue to markup (base grammar, no lazy load)", GS.langForPath("App.vue") === "markup", "lang=" + GS.langForPath("App.vue"));
  ok("langForFence maps svelte to markup", GS.langForFence("svelte") === "markup", "lang=" + GS.langForFence("svelte"));
  const vueParent = el("code", {}, []);
  GS.highlightTo(vueParent, "<template><div>{{ x }}</div></template>", GS.langForPath("App.vue"));
  await wait(20);
  ok("a .vue block highlights immediately as markup (base grammar)", hasTokenSpan(vueParent), "spans=" + hasTokenSpan(vueParent));

  // ---- caching: a second ensureGrammar of a loaded grammar is instant/true ----
  const again = await GS.ensureGrammar("python");
  ok("re-requesting a loaded grammar resolves true from cache", again === true, "again=" + again);

  console.log("\n" + pass + " passed, " + fail + " failed");
  process.exit(fail ? 1 : 0);
}
main().catch((e) => { console.error(e); console.log("\n" + pass + " passed, " + (fail + 1) + " failed"); process.exit(1); });

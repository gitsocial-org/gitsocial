// verify_routes_tree_markdown.js - core reader primitives (gs-core). Grab-bag:
//  - route grammar parsing (home/index/file/branch/commit, line ranges)
//  - tree/path resolution (getTree, resolvePath) over the richbucket fixture
//  - markdown block parsing (parseMarkdown)
//  - HEAD + branch listing, blob decoding, per-extension item counts
const GS = require("../site/gs-core.js");

const DEMO = process.env.DEMO_BASE || (process.env.GS_SITE_ORIGIN||"http://localhost:8000")+"/demo-project/";
const RICH = process.env.RICH_BASE || (process.env.GS_SITE_ORIGIN||"http://localhost:8000")+"/richbucket/";
let pass = 0, fail = 0;
function ok(name, cond, extra) { (cond ? pass++ : fail++); console.log((cond ? "PASS " : "FAIL ") + name + (extra ? " :: " + extra : "")); }

function routeTests() {
  const P = GS.parseRoute.bind(GS);
  ok("route #/ -> home", P("#/").type === "home");
  ok("route empty -> home", P("").type === "home");
  ok("route #/timeline -> index", P("#/timeline").type === "index" && P("#/timeline").tab === "timeline");
  ok("route #/code -> code", P("#/code").type === "code");
  ok("route #/branches -> branches", P("#/branches").type === "branches");
  ok("route #branch:feature/x", (() => { const r = P("#branch:feature/x"); return r.type === "branch" && r.name === "feature/x"; })());
  const f1 = P("#file:src/a.js@main");
  ok("file route basic", f1.type === "file" && f1.path === "src/a.js" && f1.branch === "main" && f1.line === null);
  const f2 = P("#file:src/a.js@main:L12");
  ok("file route single line", f2.line === 12 && f2.lineEnd === null);
  const f3 = P("#file:src/a.js@main:L5-9");
  ok("file route range", f3.line === 5 && f3.lineEnd === 9);
  const f4 = P("#file:a/b/c.txt@feature/x:L3");
  ok("file route slashed branch + line", f4.branch === "feature/x" && f4.path === "a/b/c.txt" && f4.line === 3);
  const c1 = P("#commit:abc1234@custombranch");
  ok("commit route non-gitmsg branch", c1.type === "commit" && c1.branch === "custombranch");
}

async function treeParseTest(ctx, rootTreeSha, expectDirs, expectFiles) {
  const entries = await GS.getTree(ctx, rootTreeSha);
  ok("parseTree returns entries", entries && entries.length > 0, "len=" + (entries && entries.length));
  const dirs = entries.filter((e) => e.type === "tree").map((e) => e.name).sort();
  const files = entries.filter((e) => e.type === "blob").map((e) => e.name).sort();
  ok("parseTree dir names", JSON.stringify(dirs) === JSON.stringify(expectDirs), dirs.join(","));
  ok("parseTree file names", JSON.stringify(files) === JSON.stringify(expectFiles), files.join(","));
  ok("parseTree shas 40-hex", entries.every((e) => /^[0-9a-f]{40}$/.test(e.sha)));
  ok("parseTree modes octal", entries.every((e) => /^\d+$/.test(e.mode)));
  return entries;
}

function markdownTests() {
  const blocks = GS.parseMarkdown("# Title\n\npara **bold** and `code` and [l](https://x.com)\n\n- a\n- b\n\n```js\nconst x=1;\n```\n\nRaw <script>alert(1)</script> here");
  ok("md heading", blocks[0].type === "heading" && blocks[0].level === 1);
  const para = blocks.find((b) => b.type === "paragraph");
  ok("md paragraph strong span", para.spans.some((s) => s.type === "strong" && s.spans && s.spans.some((x) => x.type === "text" && x.value === "bold")));
  ok("md paragraph code span", para.spans.some((s) => s.type === "code" && s.value === "code"));
  ok("md paragraph link span", para.spans.some((s) => s.type === "link" && s.href === "https://x.com"));
  const list = blocks.find((b) => b.type === "list");
  ok("md list two items", list && list.items.length === 2);
  const code = blocks.find((b) => b.type === "code");
  ok("md fenced code lang", code && code.lang === "js" && code.text === "const x=1;");
  const rawBlock = blocks[blocks.length - 1];
  const rawSpan = rawBlock.spans.find((s) => s.type === "rawhtml");
  ok("md raw-HTML captured as inert rawhtml span", !!rawSpan && rawSpan.value.includes("<script>") && rawBlock.spans.every((s) => ["text", "code", "em", "strong", "rawhtml"].includes(s.type)), JSON.stringify(rawBlock.spans.map((s) => s.type)));
}

async function main() {
  routeTests();
  markdownTests();

  const rctx = GS.newContext(RICH);
  const rhead = await GS.resolveHead(RICH);
  ok("rich HEAD resolves", rhead && rhead.sha && rhead.branch === "refs/heads/main", JSON.stringify(rhead));
  const root = await GS.resolvePath(rctx, rhead.sha, "");
  ok("resolvePath empty -> root tree", root && root.type === "tree");
  await treeParseTest(rctx, root.sha, ["docs", "src"], ["README.md"]);
  const srcNode = await GS.resolvePath(rctx, rhead.sha, "src");
  ok("resolvePath dir 'src'", srcNode && srcNode.type === "tree");
  const srcEntries = await GS.getTree(rctx, srcNode.sha);
  ok("subtree descent lists src files", JSON.stringify(srcEntries.map((e) => e.name).sort()) === JSON.stringify(["index.js", "main.go"]), srcEntries.map((e) => e.name).join(","));
  const fileNode = await GS.resolvePath(rctx, rhead.sha, "src/index.js");
  ok("resolvePath file 'src/index.js'", fileNode && fileNode.type === "blob");
  const missing = await GS.resolvePath(rctx, rhead.sha, "src/nope.js");
  ok("resolvePath missing -> null", missing === null);
  const missingDir = await GS.resolvePath(rctx, rhead.sha, "no/such/dir");
  ok("resolvePath missing dir -> null", missingDir === null);
  const blobObj = await GS.getObject(rctx, fileNode.sha);
  ok("blob decode content", new TextDecoder().decode(blobObj.body) === "export const answer = 42;\n");
  const readmeNode = await GS.resolvePath(rctx, rhead.sha, "README.md");
  const readmeObj = await GS.getObject(rctx, readmeNode.sha);
  const rmBlocks = GS.parseMarkdown(new TextDecoder().decode(readmeObj.body));
  ok("README parses to blocks", rmBlocks.length > 3);
  const lb = await GS.listBranches(rctx);
  ok("listBranches default = main", lb.defaultBranch === "main" && lb.branches.some((b) => b.isDefault && b.name === "main"), JSON.stringify(lb.branches.map((b) => b.name)));

  const dctx = GS.newContext(DEMO);
  const dhead = await GS.resolveHead(DEMO);
  ok("demo HEAD = test3", dhead && dhead.branch === "refs/heads/test3", JSON.stringify(dhead));
  const droot = await GS.resolvePath(dctx, dhead.sha, "");
  const dentries = await GS.getTree(dctx, droot.sha);
  ok("demo root tree has test3.txt", dentries.some((e) => e.name === "test3.txt" && e.type === "blob"), dentries.map((e) => e.name).join(","));
  const dblobNode = await GS.resolvePath(dctx, dhead.sha, "test3.txt");
  const dblob = await GS.getObject(dctx, dblobNode.sha);
  ok("demo blob decode", new TextDecoder().decode(dblob.body).trim() === "hello world 3!", JSON.stringify(new TextDecoder().decode(dblob.body)));
  for (const [ext, want] of [["social", 6], ["pm", 11], ["review", 2], ["release", 1]]) {
    const items = await GS.loadExtItems(dctx, ext);
    ok("regression count " + ext + "=" + want, items.length === want, "got " + items.length);
  }
  const dlb = await GS.listBranches(dctx);
  ok("demo branches include gitmsg/social + default test3", dlb.defaultBranch === "test3" && dlb.branches.some((b) => b.name === "gitmsg/social"), JSON.stringify(dlb.branches.map((b) => b.name)));

  console.log("\n" + pass + " passed, " + fail + " failed");
  process.exit(fail ? 1 : 0);
}
main().catch((e) => { console.error(e); process.exit(1); });

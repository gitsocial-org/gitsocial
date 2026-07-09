// verify_bucket_read.js - headless read-surface smoke over a live bucket via
// gs-core: HEAD resolution, branch listing, path/tree resolution, blob decode,
// and per-extension item loads.
const GS = require("../site/gs-core.js");
const BASE = process.env.BASE || (process.env.GS_SITE_ORIGIN||"http://localhost:8000")+"/gitsocial/";
let pass = 0, fail = 0;
function ok(name, cond, extra) { (cond ? pass++ : fail++); console.log((cond ? "PASS " : "FAIL ") + name + (extra ? " :: " + extra : "")); }

async function main() {
  const ctx = GS.newContext(BASE);
  const head = await GS.resolveHead(BASE);
  ok("HEAD resolves to main", head && head.branch === "refs/heads/main" && /^[0-9a-f]{40}$/.test(head.sha), JSON.stringify(head));

  const lb = await GS.listBranches(ctx);
  const names = lb.branches.map((b) => b.name).sort();
  ok("listBranches default = main", lb.defaultBranch === "main" && lb.branches.some((b) => b.isDefault && b.name === "main"), JSON.stringify(names));
  ok("branches include gitmsg/pm", names.includes("gitmsg/pm"), names.join(","));
  ok("branches include gitmsg/release", names.includes("gitmsg/release"), names.join(","));

  const root = await GS.resolvePath(ctx, head.sha, "");
  ok("resolvePath '' -> root tree", root && root.type === "tree");
  const readme = await GS.resolvePath(ctx, head.sha, "README.md");
  ok("resolvePath README.md -> blob", readme && readme.type === "blob", JSON.stringify(readme));
  const lib = await GS.resolvePath(ctx, head.sha, "library");
  ok("resolvePath library -> tree", lib && lib.type === "tree", JSON.stringify(lib));

  const readmeObj = await GS.getObject(ctx, readme.sha);
  const text = new TextDecoder().decode(readmeObj.body);
  ok("README blob decodes", readmeObj.type === "blob" && text.length > 100, "len=" + text.length);
  console.log("     README head: " + JSON.stringify(text.slice(0, 60)));

  const libEntries = await GS.getTree(ctx, lib.sha);
  const libNames = libEntries.map((e) => e.name).sort();
  ok("library subtree lists entries", libEntries.length > 0, libNames.join(","));

  for (const ext of ["social", "pm", "review", "release"]) {
    const items = await GS.loadExtItems(ctx, ext);
    console.log("     loadExtItems " + ext + " = " + items.length);
  }
  const pm = await GS.loadExtItems(ctx, "pm");
  const rel = await GS.loadExtItems(ctx, "release");
  ok("pm items non-zero", pm.length > 0, "count=" + pm.length);
  ok("release items non-zero", rel.length > 0, "count=" + rel.length);

  console.log("\n" + pass + " passed, " + fail + " failed");
  process.exit(fail ? 1 : 0);
}
main().catch((e) => { console.error(e); process.exit(1); });

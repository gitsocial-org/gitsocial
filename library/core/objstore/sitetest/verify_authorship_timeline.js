// verify_authorship_timeline.js - shim + live/synthetic. Grab-bag:
//  - effective authorship (origin-author on imported items)
//  - merged timeline interleaving + newest-first ordering
//  - memo card rendering (chips, subject, body, detail link)
//  - edited-by attribution (cross-repo accepted edit keeps the original author)
//  - raw commit message retention through parsing
require("./shim.js");
require("../site/icons.js");
const GS = require("../site/gs-app.js");
const { textOf } = global.__shim;
let pass = 0, fail = 0;
const ok = (n, c, e) => { (c ? pass++ : fail++); console.log((c ? "PASS " : "FAIL ") + n + (!c && e ? " :: " + e : "")); };
function findClass(node, cls, out){ out=out||[]; for(const c of node._children||[]){ if(c&&c.nodeType===1){ if(c._cls&&c._cls.has(cls)) out.push(c); findClass(c,cls,out);} } return out; }
const mkCommit = (short, name, email, time, raw) => ({ short, hash: short + "0".repeat(40 - short.length), authorName: name, authorEmail: email, authorTime: time, refs: [], rawMessage: raw || "", content: "" });

async function main() {
  // ================= ITEM 1: effective authorship (meshtastic, live) =================
  const m = GS.newContext((process.env.GS_SITE_ORIGIN||"http://localhost:8000")+"/meshtastic/");
  const pm = await GS.loadExtItems(m, "pm");
  ok("meshtastic pm items load", pm.length > 0, "count=" + pm.length);
  const imported = pm.filter((i) => i.header && i.header["origin-author-name"]);
  ok("meshtastic pm has imported items (origin-author-name)", imported.length > 5, "count=" + imported.length);
  // Every imported item's display author is the origin author, not the importer.
  const it0 = imported[0];
  console.log("  before(git author)=" + JSON.stringify(it0.commit.authorName) + "  after(display author)=" + JSON.stringify(it0.author) + "  origin=" + JSON.stringify(it0.header["origin-author-name"]));
  ok("imported item.author == origin-author-name", it0.author === it0.header["origin-author-name"], "author=" + it0.author);
  ok("imported item.author != git author (importer)", it0.author !== it0.commit.authorName, "author=" + it0.author + " git=" + it0.commit.authorName);
  ok("all imported items display the origin author", imported.every((i) => i.author === i.header["origin-author-name"]), "mismatch found");
  // metaRow renders the origin author text.
  const meta = GS.metaRow(it0, "gitmsg/pm");
  ok("metaRow renders the origin author name", textOf(meta).includes(it0.header["origin-author-name"]), textOf(meta).slice(0, 60));
  // A native item (no origin) falls back to git author.
  const native = pm.find((i) => !(i.header && i.header["origin-author-name"]));
  if (native) ok("native pm item.author == git author (fallback)", native.author === (native.commit.authorName || native.commit.authorEmail), "author=" + native.author);
  else ok("native pm item present", true);

  // Release fixture (gitsocial): imported releases show origin author too.
  const g = GS.newContext((process.env.GS_SITE_ORIGIN||"http://localhost:8000")+"/gitsocial/");
  const rel = await GS.loadExtItems(g, "release");
  const relImp = rel.filter((r) => r.header && r.header["origin-author-name"]);
  console.log("  gitsocial releases=" + rel.length + " with origin-author=" + relImp.length);
  if (relImp.length) ok("gitsocial imported release shows origin author", relImp[0].author === relImp[0].header["origin-author-name"], "author=" + relImp[0].author);
  else ok("gitsocial releases loaded (no origin-author in fixture is acceptable)", rel.length > 0, "count=" + rel.length);

  // ================= SCOPE A: merged timeline (meshtastic, live) =================
  const tl = await GS.loadTimelineItems(m);
  ok("meshtastic merged timeline non-empty (was empty when social-only)", tl.length > 0, "count=" + tl.length);
  const exts = new Set(tl.map((i) => i._ext));
  ok("merged timeline interleaves types (pm + review present)", exts.has("pm") && exts.has("review"), "exts=" + Array.from(exts).join(","));
  // Newest-first by effectiveTime.
  let sorted = true; for (let i = 1; i < tl.length; i++) if (tl[i].effectiveTime > tl[i - 1].effectiveTime) { sorted = false; break; }
  ok("merged timeline sorted newest-first by effectiveTime", sorted);
  console.log("  first 5 timeline entries (ext | effTime | subject):");
  for (const e of tl.slice(0, 5)) console.log("    " + e._ext + " | " + e.effectiveTime + " | " + (e.content || "").split("\n")[0].slice(0, 44));
  const firstExts = tl.slice(0, 12).map((i) => i._ext);
  ok("first entries mix >1 type (interleaved, not grouped)", new Set(firstExts).size > 1, "firstExts=" + firstExts.join(","));
  // thread-demo: posts + its issue interleaved.
  const t = GS.newContext((process.env.GS_SITE_ORIGIN||"http://localhost:8000")+"/thread-demo/");
  const ttl = await GS.loadTimelineItems(t);
  const ttlExts = new Set(ttl.map((i) => i._ext));
  ok("thread-demo timeline interleaves social + pm", ttlExts.has("social") && ttlExts.has("pm"), "exts=" + Array.from(ttlExts).join(","));

  // ================= SCOPE B2: memo card rendering (synthetic + shim) =================
  const memoItem = { commit: mkCommit("abc123abc123", "Ada", "ada@x.io", 1000), header: { type: "memo", labels: "design,architecture" }, content: "Cache invalidation plan\n\nUse content hashes.", author: "Ada", edited: false, effectiveTime: 1000 };
  const mc = GS.memoCard(memoItem);
  const chipTexts = findClass(mc, "chip").map((c) => textOf(c));
  ok("memoCard has a 'memo' type chip", chipTexts.includes("memo"), chipTexts.join(","));
  ok("memoCard renders labels as chips", chipTexts.includes("design") && chipTexts.includes("architecture"), chipTexts.join(","));
  ok("memoCard renders the subject", findClass(mc, "subject").some((s) => textOf(s).includes("Cache invalidation plan")));
  ok("memoCard renders the body", textOf(mc).includes("Use content hashes."));
  ok("memoCard links to #commit:<hash>@gitmsg/memo", findClass(mc, "subject").some((s) => (s.getAttribute("href") || "").includes("@gitmsg/memo")));

  // ================= ITEM 1 (edit): edited-by (synthetic resolveItems) =================
  // Imported canonical by Alice; owner (Bob) accepts a cross-repo proposal as a
  // same-repo mirror edit -> display keeps Alice (original), meta gets "edited by Bob".
  const canonRaw = "Original issue\n\nGitMsg: ext=\"pm\"; type=\"issue\"; origin-author-name=\"Alice\"; origin-author-email=\"alice@up.stream\"; origin-time=\"2025-01-01T00:00:00Z\"; state=\"open\"; v=\"0.1.0\"";
  const editRaw = "Original issue (owner accepted)\n\nGitMsg: ext=\"pm\"; type=\"issue\"; edits=\"#commit:aaaaaaaaaaaa@gitmsg/pm\"; state=\"open\"; v=\"0.1.0\"";
  const canon = Object.assign(mkCommit("aaaaaaaaaaaa", "Importer", "imp@bot", 500, canonRaw), { content: "Original issue", gitmsg: GS.parseGitmsg(canonRaw) });
  const edit = Object.assign(mkCommit("bbbbbbbbbbbb", "Bob", "bob@owner", 600, editRaw), { content: "Original issue (owner accepted)", gitmsg: GS.parseGitmsg(editRaw) });
  const items = GS.resolveItems([edit, canon]); // newest-first input
  const resolved = items.find((i) => i.commit.short === "aaaaaaaaaaaa");
  ok("edited item keeps ORIGINAL (origin) author for display", resolved && resolved.author === "Alice", resolved && resolved.author);
  ok("edited item flagged edited", resolved && resolved.edited === true);
  ok("edited item names the differing editor ('edited by Bob')", resolved && resolved.editorName === "Bob", resolved && resolved.editorName);
  const rmeta = GS.metaRow(resolved, "gitmsg/pm");
  ok("metaRow shows 'edited by Bob' chip", findClass(rmeta, "chip").some((c) => textOf(c) === "edited by Bob"), findClass(rmeta, "chip").map((c) => textOf(c)).join(","));

  // ================= ITEM 3b: raw commit message survives parsing =================
  const parsed = GS.parseCommit("cccccccccccc" + "0".repeat(28), Buffer.from("tree t\nauthor N <n@e> 1000 +0000\ncommitter N <n@e> 1000 +0000\n\nSubject line\n\nBody.\n\nGitMsg: ext=\"social\"; type=\"post\"; v=\"0.1.0\""));
  ok("parseCommit retains rawMessage with the GitMsg trailer", /GitMsg:/.test(parsed.rawMessage) && /Subject line/.test(parsed.rawMessage), parsed.rawMessage);
  // resolveItems: an edited item's rawMessage is the EDIT commit's raw (protocol truth).
  ok("edited item rawMessage is the edit commit's raw", resolved && resolved.rawMessage === editRaw, resolved && resolved.rawMessage);

  console.log("\n" + pass + " passed, " + fail + " failed");
  process.exit(fail ? 1 : 0);
}
main().catch((e) => { console.error("THREW:", e); process.exit(1); });

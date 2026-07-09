// verify_versions_review.js - live proofs across two areas:
//  - version history + version diff (thread-demo 3-version issue, meshtastic
//    deep-walk orphan-edit fold-in)
//  - PR review feedback overlaid on diffs + review summary strip (thread-demo PR)
require("./shim.js");
require("../site/icons.js");
const GS = require("../site/gs-app.js");
const { viewNode, textOf, setHash } = global.__shim;
let pass = 0, fail = 0;
const ok = (n, c, e) => { (c ? pass++ : fail++); console.log((c ? "PASS " : "FAIL ") + n + (!c && e ? " :: " + e : "")); };
const wait = (ms) => new Promise((r) => setTimeout(r, ms));
function findClass(node, cls, out){ out=out||[]; for(const c of (node&&node._children)||[]){ if(c&&c.nodeType===1){ if(c._cls&&c._cls.has(cls)) out.push(c); findClass(c,cls,out);} } return out; }
function fire(node, ev, props){ (node&&node._handlers&&node._handlers[ev]||[]).forEach(fn=>fn(Object.assign({preventDefault(){},stopPropagation(){},key:"",target:{closest(){return null;}}},props||{}))); }
async function run(bucket, hash){ global.__ctx = GS.newContext((process.env.GS_SITE_ORIGIN||"http://localhost:8000")+"/"+bucket+"/"); setHash(hash); await GS.route(global.__ctx); await wait(1400); }

async function main() {
  await wait(2500); // drain init home route

  // ================= ITEM 1: version history + diff =================
  await run("thread-demo", "#commit:b661c9130d22@gitmsg/pm");
  let view = viewNode;
  const hist = findClass(view, "version-history");
  ok("1 History section present on edited issue", hist.length === 1);
  const histHead = findClass(view, "version-history-head")[0];
  ok("1 History header reads 3 versions", histHead && textOf(histHead).includes("3 versions"), histHead && textOf(histHead));
  const vrows = findClass(view, "version-row");
  ok("1 three version rows", vrows.length === 3, "got " + vrows.length);
  const labels = findClass(view, "version-label").map(textOf);
  ok("1 labels current/v2/original present", labels.includes("current") && labels.includes("original") && labels.includes("v2"), labels.join(","));
  // default shows latest (closed, edited subject)
  const subj0 = findClass(view, "subject")[0];
  ok("1 default shows latest subject", subj0 && textOf(subj0).includes("onboarding and setup"), subj0 && textOf(subj0));
  // diff-to-previous toggle exists on the non-original rows
  const diffBtns = findClass(view, "version-diff-btn");
  ok("1 diff-to-previous toggles on 2 rows", diffBtns.length === 2, "got " + diffBtns.length);
  // diffBtns[1] = the v2 row (original -> body edit, real body changes);
  // diffBtns[0] = the current row (state-only close edit, empty body diff).
  fire(diffBtns[1], "click");
  await wait(50);
  const vdiff = findClass(view, "version-diff");
  ok("1 version diff renders a unified body diff", vdiff.length >= 1);
  const diffAdds = findClass(view, "diff-line").filter(l => l._cls.has("add"));
  ok("1 version diff shows added body lines", diffAdds.length >= 1, "adds=" + diffAdds.length);
  void 0; // (diffBtns[1] = original->body-edit; diffBtns[0] = state-only close)
  // select the original version -> subject repaints to the original title
  const origRow = vrows.find(r => findClass(r, "version-label").some(l => textOf(l) === "original"));
  fire(origRow, "click");
  await wait(50);
  ok("1 selecting original repaints subject", textOf(findClass(view,"subject")[0]).includes("Improve onboarding docs") && !textOf(findClass(view,"subject")[0]).includes("setup"), textOf(findClass(view,"subject")[0]));

  // deep-walk fold-in (meshtastic): an item whose edit sits in window 1 as a
  // single-version orphan (its canonical deeper) gains its second version once
  // the canonical is walked in — the orphan-edit self-heal, now also growing the
  // version chain. In window 1 the edit stands alone (1 version, keyed by the
  // EDIT hash); fully loaded it becomes a 2-version item keyed by the CANONICAL
  // hash whose edit chain includes that window-1 edit hash.
  const mctx = GS.newContext((process.env.GS_SITE_ORIGIN||"http://localhost:8000")+"/meshtastic/");
  let w = await GS.loadExtItemsWindow(mctx, "pm", false);
  const w1single = new Set(w.items.filter(i => i.versions.length === 1).map(i => i.commit.hash));
  const w1hashes = new Set(w.items.map(i => i.commit.hash));
  let win = 1;
  while (w.truncated && win < 8) { w = await GS.loadExtItemsWindow(mctx, "pm", true); win++; }
  let grew = null;
  for (const it of w.items) {
    if (it.versions.length < 2) continue;
    const canonHash = it.versions[0].commit.hash;
    const editHashes = it.versions.slice(1).map(v => v.commit.hash);
    if (!w1hashes.has(canonHash) && editHashes.some(h => w1single.has(h))) {
      grew = { canon: canonHash.slice(0, 12), edit: editHashes.find(h => w1single.has(h)).slice(0, 12), versions: it.versions.length };
      break;
    }
  }
  ok("1 deep-walk fold-in: a window-1 orphan edit gains its canonical as a 2nd version", !!grew, grew ? JSON.stringify(grew) : "no fold-in found");
  if (grew) console.log("   orphan edit " + grew.edit + " (1 version in window 1) folded into canonical " + grew.canon + " (" + grew.versions + " versions fully loaded)");

  // ================= ITEM 2: PR feedback + summary =================
  await run("thread-demo", "#commit:6156e3fed6e0@gitmsg/review");
  await wait(600);
  view = viewNode;
  // summary strip
  const rsum = findClass(view, "review-summary")[0];
  ok("2 review summary strip present", !!rsum);
  const rline = findClass(view, "review-summary-line")[0];
  ok("2 summary counts 1 approved / 1 changes requested / 0 pending", rline && /1 approved.*1 changes requested.*0 pending/.test(textOf(rline)), rline && textOf(rline));
  ok("2 status chip 'Changes requested'", rline && textOf(rline).includes("Changes requested"), rline && textOf(rline));
  const rchips = findClass(view, "reviewer-chip").map(textOf);
  ok("2 two reviewer chips (bob approved, carol changes-requested)", rchips.length === 2 && rchips.some(t=>/approved/.test(t)) && rchips.some(t=>/changes-requested/.test(t)), rchips.join(" | "));
  // file card comment count chip
  const fbCount = findClass(view, "fb-count").map(textOf);
  ok("2 notes.txt file card shows comment-count chip (3 comments)", fbCount.some(t => t.includes("3 comment")), fbCount.join(","));
  // inline feedback cards (auto-expanded single file)
  const inlineCards = findClass(view, "diff-feedback");
  ok("2 inline feedback cards render under diff lines", inlineCards.length >= 2, "got " + inlineCards.length);
  const fbCards = findClass(view, "fb-card");
  ok("2 feedback cards carry verdict/comment content", fbCards.length >= 2, "got " + fbCards.length);
  // suggestion block
  const sugHead = findClass(view, "suggestion-head").map(textOf);
  ok("2 suggestion block with 'applies to L4' note", sugHead.some(t => t.includes("applies to L4")), sugHead.join(","));
  const sugCode = findClass(view, "suggestion-code");
  ok("2 suggestion renders code panel", sugCode.length >= 1 && textOf(sugCode[0]).includes("line four"), sugCode[0] && textOf(sugCode[0]));

  console.log(`\n${pass} passed, ${fail} failed`);
  process.exit(fail ? 1 : 0);
}
main().catch(e => { console.error("FAIL", e); process.exit(1); });

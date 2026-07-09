// verify_threads.js - live-fixture verification of thread grouping, embedded
// context, and same-repo reply-context resolution (parentRef/resolveAncestors/
// quotedRefFor). Commit hashes are resolved dynamically by content substring
// (the showcase fixture is rebuilt from scratch, so hashes are not stable),
// then the same thread/flatten/embedded-ref assertions run against the served
// bucket.
const GS = require("../site/gs-core.js");
const ORIGIN = process.env.GS_SITE_ORIGIN || "http://localhost:8000";
const BASE = ORIGIN + "/" + (process.env.GS_SITE_BUCKET || "thread-demo") + "/";
let pass = 0, fail = 0;
const ok = (n, c, e) => { (c ? pass++ : fail++); console.log((c ? "PASS " : "FAIL ") + n + (!c && e ? " :: " + e : "")); };
// shortOf finds an item whose content contains sub and returns its short hash.
const shortOf = (items, sub) => { const it = items.find((i) => (i.content || "").includes(sub)); return it && it.commit.short; };

async function main() {
  const ctx = GS.newContext(BASE);
  const social = await GS.loadExtItems(ctx, "social");
  const pm = await GS.loadExtItems(ctx, "pm");
  const comments = social.filter((i) => i.header && i.header.original);

  const P1 = shortOf(social, "Shipping the S3 static site reader");
  const C1 = shortOf(social, "Congrats");
  const C2 = shortOf(social, "What about");
  const C3 = shortOf(social, "Looking forward");
  const R1 = shortOf(social, "Thanks, appreciate");
  const R2 = shortOf(social, "Seconded");
  const QUOTE = shortOf(social, "Great point from upstream");
  const ISSUE = shortOf(pm, "thread view needs live fixture");
  ok("resolved fixture hashes", [P1, C1, C2, C3, R1, R2, QUOTE, ISSUE].every(Boolean),
    JSON.stringify({ P1, C1, C2, C3, R1, R2, QUOTE, ISSUE }));

  // ---- Same-repo post thread P1 (true-depth reply tree) ----
  const thread = GS.groupThread(P1, comments);
  const topContents = thread.map((n) => n.comment.content);
  ok("P1 thread has 3 top-level nodes (C1,C2,C3; R2 nested under R1)", thread.length === 3, "got " + thread.length);
  ok("P1 top-level chronological C1<C2<C3", topContents[0].startsWith("Congrats") && topContents[1].startsWith("What about") && topContents[2].startsWith("Looking forward"), JSON.stringify(topContents));
  const c1node = thread.find((n) => n.comment.commit.short === C1);
  ok("C1 node found with exactly 1 nested reply (R1)", c1node && c1node.replies.length === 1 && c1node.replies[0].comment.commit.short === R1, c1node ? "replies=" + c1node.replies.map((r) => r.comment.commit.short) : "no C1 node");
  ok("R1 nested content = 'Thanks, appreciate it!'", c1node && c1node.replies[0].comment.content.startsWith("Thanks"), c1node && c1node.replies[0] && c1node.replies[0].comment.content);
  const r1node = c1node && c1node.replies[0];
  ok("R2 (reply-to-a-reply) nests under R1 at depth 2", !!r1node && r1node.replies.length === 1 && r1node.replies[0].comment.commit.short === R2 && r1node.replies[0].depth === 2, r1node ? "R1 depth=" + r1node.depth + " replies=" + r1node.replies.map((r) => r.comment.commit.short) : "no R1");
  ok("R2 is not a top-level node", !thread.some((n) => n.comment.commit.short === R2), "R2 wrongly top-level");
  const flat = GS.flattenThread(thread);
  ok("P1 thread flattens to all 5 comments", flat.length === 5, "got " + flat.length);
  ok("P1 flatten depths (C1:0 R1:1 R2:2)", flat[0].depth === 0 && flat[1].comment.commit.short === R1 && flat[1].depth === 1 && flat[2].comment.commit.short === R2 && flat[2].depth === 2, JSON.stringify(flat.map((f) => ({ s: f.comment.commit.short, d: f.depth }))));
  ok("C2,C3 have no replies", thread.find((n) => n.comment.commit.short === C2).replies.length === 0 && thread.find((n) => n.comment.commit.short === C3).replies.length === 0);

  // ---- Cross-branch issue thread (issue on gitmsg/pm, comments on gitmsg/social) ----
  const issueThread = GS.groupThread(ISSUE, comments);
  let issueTotal = 0; for (const n of issueThread) issueTotal += 1 + n.replies.length;
  ok("issue thread has 2 top-level comments", issueThread.length === 2 && issueTotal === 2, "nodes=" + issueThread.length + " total=" + issueTotal);
  ok("issue comments chronological", issueThread[0].comment.content.startsWith("I can build") && issueThread[1].comment.content.startsWith("Great, assign"), JSON.stringify(issueThread.map((n) => n.comment.content)));

  // ---- Cross-repo embedded context on the quote ----
  const quote = social.find((i) => i.commit.short === QUOTE);
  ok("quote item present", !!quote, "quote not found");
  const emb = GS.embeddedRefs(quote.commit, quote.header);
  ok("quote has 1 cross-repo embedded ref", emb.length === 1, "got " + emb.length);
  ok("embedded ref url = other-demo bucket", emb[0] && emb[0].url === "s3://fake.example.com/other-demo", emb[0] && emb[0].url);
  ok("embedded ref author = Grace Hopper", emb[0] && emb[0].author === "Grace Hopper", emb[0] && emb[0].author);
  ok("embedded ref quoted excerpt present", emb[0] && emb[0].quoted.startsWith("Original upstream idea"), emb[0] && emb[0].quoted);
  ok("cross-repo quote is not grouped into P1/issue threads", !GS.flattenThread(thread.concat(issueThread)).some((f) => f.comment.commit.short === QUOTE));

  // ---- Reply context on bare commit permalinks (same-repo ancestor chain) ----
  const r2 = social.find((i) => i.commit.short === R2);
  ok("R2 parentRef names R1 (reply-to over original)", GS.hashEq(GS.refHash(GS.parentRef(r2.header)), R1), GS.parentRef(r2.header));
  const anc = await GS.resolveAncestors(ctx, r2, "gitmsg/social");
  ok("R2 ancestor chain resolves root-first P1,C1,R1", anc.chain.length === 3 && anc.chain[0].item.commit.short === P1 && anc.chain[1].item.commit.short === C1 && anc.chain[2].item.commit.short === R1, JSON.stringify(anc.chain.map((c) => c.item.commit.short)));
  ok("R2 chain resolves on gitmsg/social with nothing missing", anc.chain.every((c) => c.branch === "gitmsg/social") && anc.missing === null, JSON.stringify({ missing: anc.missing, branches: anc.chain.map((c) => c.branch) }));
  // Cross-branch: a social comment on a pm issue resolves the issue via the pm walk.
  const ic = comments.find((i) => (i.content || "").startsWith("I can build"));
  const ianc = await GS.resolveAncestors(ctx, ic, "gitmsg/social");
  ok("issue comment resolves its pm parent (cross-branch)", ianc.chain.length === 1 && ianc.chain[0].item.commit.short === ISSUE && ianc.chain[0].branch === "gitmsg/pm", JSON.stringify(ianc.chain.map((c) => [c.item.commit.short, c.branch])));
  // Cross-repo quote: original carries a repo url, so there is no same-repo parent.
  ok("cross-repo quote has no same-repo parentRef", GS.parentRef(quote.header) === null, GS.parentRef(quote.header));
  // Unresolvable parent: missing ref reported; the commit's own excerpt is the fallback.
  const ghostRef = "#commit:aaaaaaaaaaaa@gitmsg/social";
  const ghost = { commit: { short: "deadbeefdead", refs: [{ ref: ghostRef, author: "Ghost", quoted: "vanished parent text" }] }, header: { original: ghostRef } };
  const ganc = await GS.resolveAncestors(ctx, ghost, "gitmsg/social");
  ok("unresolvable parent reported as missing", ganc.chain.length === 0 && ganc.missing === ghostRef, JSON.stringify(ganc));
  const q = GS.quotedRefFor(ghost.commit, ganc.missing);
  ok("quotedRefFor returns the commit's own excerpt for the missing ref", !!q && q.quoted === "vanished parent text" && q.author === "Ghost", JSON.stringify(q));

  console.log("\n" + pass + " passed, " + fail + " failed");
  process.exit(fail ? 1 : 0);
}
main().catch((e) => { console.error("THREW:", e); process.exit(1); });

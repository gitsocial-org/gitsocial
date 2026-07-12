// unit_graph_order.js - DOM-free units for the indexed graph's window ordering
// (orderGraphWindow): a rebased linear chain with NON-monotonic author dates
// must emit in parent order and lay out as ONE lane (a plain ts sort split it
// into phantom parallel lanes), plus dedup across converging tips, the window
// cap / `more` flag, and the resident-boundary cut.
const GS = require("../site/gs-core.js");
let pass = 0, fail = 0;
function eq(a, b, msg) { if (JSON.stringify(a) === JSON.stringify(b)) { pass++; } else { fail++; console.log("FAIL", msg, "got", JSON.stringify(a), "want", JSON.stringify(b)); } }
function ok(c, msg) { if (c) { pass++; } else { fail++; console.log("FAIL", msg); } }
const sha = (s) => (s.repeat(40)).slice(0, 40);
const mk = (id, parents, t) => ({ hash: sha(id), short: sha(id).slice(0, 12), parents: parents.map(sha), authorTime: t, content: "c" + id, authorName: "Ada", authorEmail: "ada@example.com" });

console.log("=== linear chain, scrambled author dates (rebase shape) ===");
// Chain f -> e -> d -> c -> b -> a (f newest tip, a root), author times
// deliberately NON-monotonic along the chain — exactly what a rebase produces.
const chain = [
  mk("f", ["e"], 60), mk("e", ["d"], 10), mk("d", ["c"], 50),
  mk("c", ["b"], 20), mk("b", ["a"], 40), mk("a", [], 30),
];
// Resident order mimics the index (newest-first walk order), but the ordering
// must not depend on it — feed a shuffled copy.
const shuffled = [chain[2], chain[5], chain[0], chain[3], chain[1], chain[4]];
const lin = GS.orderGraphWindow(shuffled, 100);
eq(lin.commits.map((c) => c.content), ["cf", "ce", "cd", "cc", "cb", "ca"], "scrambled linear chain emits in parent order");
ok(!lin.more, "fully emitted chain reports no more");
const lanes = GS.assignGraphLanes(lin.commits);
eq(lanes.laneCount, 1, "scrambled linear chain lays out as a single lane");

console.log("=== ts-desc order (the old bug) would have fragmented it ===");
const tsSorted = chain.slice().sort((a, b) => b.authorTime - a.authorTime);
ok(GS.assignGraphLanes(tsSorted).laneCount > 1, "control: plain ts sort fragments the same chain into >1 lane");

console.log("=== converging tips dedup + real branch keeps two lanes ===");
// Two heads (t1, t2) converging on shared history s -> r.
const merged = [mk("1", ["5"], 90), mk("2", ["5"], 80), mk("5", ["6"], 70), mk("6", [], 65)];
const conv = GS.orderGraphWindow(merged, 100);
eq(conv.commits.map((c) => c.content), ["c1", "c2", "c5", "c6"], "converging tips interleave by time and dedup the shared parent");
eq(GS.assignGraphLanes(conv.commits).laneCount, 2, "two real heads still occupy two lanes");

console.log("=== window cap + more flag ===");
const capped = GS.orderGraphWindow(chain, 3);
eq(capped.commits.map((c) => c.content), ["cf", "ce", "cd"], "cap cuts the window after N emissions");
ok(capped.more, "capped window reports more resident entries remain");

console.log("=== resident boundary: absent parents are not pushed ===");
// Only the newest half is resident (older shard not drained): the walk stops
// at the boundary without inventing frontier entries for absent parents.
const partial = GS.orderGraphWindow(chain.slice(0, 3), 100);
eq(partial.commits.map((c) => c.content), ["cf", "ce", "cd"], "walk stops at the resident boundary");
ok(!partial.more, "boundary cut is not `more` (older-shard paging owns that)");

console.log("\n" + pass + " passed, " + fail + " failed");
process.exit(fail ? 1 : 0);

// unit_pm_search.js - DOM-free core units: PM board columns + matching,
// sub-issue hierarchy (parent/children, cycle-safe), milestone/sprint membership
// + progress, and in-bucket search matching/grouping/highlighting scope.
const GS = require("../site/gs-core.js");
let pass = 0, fail = 0;
function eq(a, b, msg) { if (JSON.stringify(a) === JSON.stringify(b)) { pass++; } else { fail++; console.log("FAIL", msg, "got", JSON.stringify(a), "want", JSON.stringify(b)); } }
function ok(c, msg) { if (c) { pass++; } else { fail++; console.log("FAIL", msg); } }
const H = (s) => (s.repeat(12)).slice(0, 12);
const mk = (short, t, header, content, author) => ({ commit: { hash: (short + "0".repeat(40)).slice(0, 40), short }, header: header || {}, content: content || "", effectiveTime: t, author: author || "" });
const ref = (h) => "#commit:" + h + "@gitmsg/pm";

console.log("=== board columns + matching ===");
const A = H("a"), B = H("b"), C = H("c"), D = H("d"), E = H("e");
const issues = [
  mk(A, 1, { type: "issue", state: "open", labels: "kind/bug" }, "Alpha"),
  mk(B, 2, { type: "issue", state: "open", labels: "status/in-progress" }, "Beta"),
  mk(C, 3, { type: "issue", state: "open", labels: "priority/high,status/review" }, "Gamma"),
  mk(D, 4, { type: "issue", state: "closed" }, "Delta"),
  mk(E, 5, { type: "issue", state: "closed", labels: "status/in-progress" }, "Epsilon"),
];
const board = GS.buildBoard(issues);
eq(board.columns.map((c) => c.name), ["Backlog", "In Progress", "Review", "Done"], "board column names mirror kanban framework");
// board.go matchIssueToColumn PREFERS a status: label over a state: filter, so a
// closed issue still carrying a status/in-progress label lands in In Progress
// (Epsilon), not Done. This mirrors the shipped board exactly (the spec's "closed
// -> last column" SHOULD is not enforced by the implementation).
eq(board.columns.map((c) => c.issues.length), [1, 2, 1, 1], "counts: unlabeled-open->Backlog, status labels->their col (label preferred over state)");
ok(board.columns[1].wip === 3 && board.columns[2].wip === 3, "In Progress / Review carry WIP 3");
eq(GS.matchIssueColumn({ state: "open", labels: "status/review" }, board.columns.map((c) => c.filter)), 2, "open+status/review -> Review (label preferred over state:open)");
eq(GS.matchIssueColumn({ state: "closed", labels: "status/in-progress" }, board.columns.map((c) => c.filter)), 1, "closed+status/in-progress -> In Progress (label preferred, mirrors board.go)");
eq(GS.matchIssueColumn({ state: "closed" }, board.columns.map((c) => c.filter)), 3, "plain closed -> Done");
eq(GS.matchIssueColumn({ state: "open" }, board.columns.map((c) => c.filter)), 0, "plain open -> Backlog (state:open first match)");
// itemLabels parsing
eq(GS.itemLabels({ labels: "kind/feature,status/in-progress,plain" }), [{ scope: "kind", value: "feature" }, { scope: "status", value: "in-progress" }, { scope: "", value: "plain" }], "itemLabels parses scoped + unscoped");
eq(GS.itemLabels({}), [], "itemLabels empty header");

console.log("=== sub-issue hierarchy (GITPM 1.7) ===");
const epic = mk(H("1"), 1, { type: "issue", state: "open" }, "Epic");
const directChild = mk(H("2"), 2, { type: "issue", state: "closed", root: ref(H("1")) }, "Direct child (root only)");
const nestedChild = mk(H("3"), 3, { type: "issue", state: "open", parent: ref(H("2")), root: ref(H("1")) }, "Nested child (parent+root)");
const hier = GS.buildIssueHierarchy([epic, directChild, nestedChild]);
eq((hier.childrenOf.get(H("1")) || []).map((c) => c.commit.short), [H("2")], "epic's direct child is the root-only issue (not the nested grandchild)");
eq((hier.childrenOf.get(H("2")) || []).map((c) => c.commit.short), [H("3")], "nested child attaches to its immediate parent, not root");
eq(GS.pmParentHash({ root: ref(H("1")) }), H("1"), "pmParentHash: root when no parent");
eq(GS.pmParentHash({ parent: ref(H("2")), root: ref(H("1")) }), H("2"), "pmParentHash: parent wins over root");
eq(GS.pmParentHash({}), null, "pmParentHash: null for top-level");
// children sorted chronologically
const p2 = mk(H("4"), 1, { type: "issue" }, "P");
const cA = mk(H("5"), 30, { type: "issue", parent: ref(H("4")) }, "cA-late");
const cB = mk(H("6"), 10, { type: "issue", parent: ref(H("4")) }, "cB-early");
eq(GS.buildIssueHierarchy([p2, cA, cB]).childrenOf.get(H("4")).map((c) => c.commit.short), [H("6"), H("5")], "children sorted oldest-first");
// cycle-safe: a<->b mutual parents must not loop or throw
const cyA = mk(H("7"), 1, { type: "issue", parent: ref(H("8")) }, "cyA");
const cyB = mk(H("8"), 2, { type: "issue", parent: ref(H("7")) }, "cyB");
const cyc = GS.buildIssueHierarchy([cyA, cyB]);
ok(cyc.childrenOf.get(H("7")).length === 1 && cyc.childrenOf.get(H("8")).length === 1, "mutual-parent cycle indexes both edges without looping");
// self-parent ignored
const self = mk(H("9"), 1, { type: "issue", parent: ref(H("9")) }, "self");
ok(!GS.buildIssueHierarchy([self]).childrenOf.has(H("9")), "self-parent edge dropped");

console.log("=== milestone / sprint membership + progress ===");
const ms = mk(H("a"), 1, { type: "milestone", state: "open", due: "2026-09-01" }, "v1.0");
const sp = mk(H("b"), 1, { type: "sprint", state: "active", start: "2026-07-01", end: "2026-07-14" }, "Sprint 1");
const m1 = mk(H("c"), 2, { type: "issue", state: "closed", milestone: ref(H("a")), sprint: ref(H("b")) }, "m1");
const m2 = mk(H("d"), 3, { type: "issue", state: "open", milestone: ref(H("a")) }, "m2");
const m3 = mk(H("e"), 4, { type: "issue", state: "open", sprint: ref(H("b")) }, "m3");
const g = GS.groupPM([ms, sp, m1, m2, m3]);
eq((g.byMilestone.get(H("a")) || []).map((i) => i.commit.short).sort(), [H("c"), H("d")].sort(), "milestone members via milestone field");
eq((g.bySprint.get(H("b")) || []).map((i) => i.commit.short).sort(), [H("c"), H("e")].sort(), "sprint members via sprint field");
eq(GS.pmProgress(g.byMilestone.get(H("a"))), { closed: 1, total: 2 }, "milestone progress 1 closed of 2");
eq(GS.pmProgress(g.bySprint.get(H("b"))), { closed: 1, total: 2 }, "sprint progress 1 closed of 2");
eq(GS.pmProgress([]), { closed: 0, total: 0 }, "empty progress");

console.log("=== search matching / grouping / scope ===");
const perExt = {
  pm: [
    mk(H("1"), 5, { type: "issue", state: "open", labels: "kind/bug" }, "Fix dark mode toggle", "Ada"),
    mk(H("2"), 4, { type: "milestone", state: "open" }, "v2.0 Roadmap", "Ada"),
  ],
  review: [
    mk(H("3"), 6, { type: "pull-request", state: "open" }, "Add dark mode CSS", "Bob"),
    mk(H("4"), 6, { type: "feedback" }, "dark nit here", "Bob"),
  ],
  social: [mk(H("5"), 7, { type: "post" }, "Thinking about dark themes", "Carol")],
  release: [mk(H("6"), 8, { type: "release", tag: "v1.2" }, "Release notes", "Dev")],
  memo: [],
};
const r = GS.searchItems("dark", perExt);
eq(r.total, 3, "dark matches issue+PR+post subjects (feedback excluded by pull-request type filter)");
eq(r.groups.map((x) => x.label + ":" + x.count), ["Issues:1", "Pull Requests:1", "Posts:1"], "grouped by extension, ordered, per-group counts");
ok(r.groups.every((x) => x.branch), "each group carries its data branch for the detail link");
// author search
eq(GS.searchItems("ada", perExt).total, 2, "author 'Ada' matches her two pm items");
// label search
eq(GS.searchItems("kind/bug", perExt).total, 1, "labels searchable");
// release tag search (header field)
eq(GS.searchItems("v1.2", perExt).total, 1, "release tag header searchable");
// empty query
eq(GS.searchItems("", perExt), { query: "", total: 0, groups: [] }, "empty query -> no groups");
// case-insensitive
eq(GS.searchItems("DARK", perExt).total, GS.searchItems("dark", perExt).total, "case-insensitive");
// feedback excluded from review group (type filter to pull-request)
ok(GS.searchItems("nit", perExt).total === 0, "feedback item not surfaced (type-filtered)");
// searchableText includes subject+author+labels
ok(GS.searchableText(perExt.pm[0]).indexOf("fix dark mode toggle") !== -1 && GS.searchableText(perExt.pm[0]).indexOf("ada") !== -1 && GS.searchableText(perExt.pm[0]).indexOf("kind/bug") !== -1, "searchableText spans subject+author+labels, lowercased");
// author: facet on the ORIGIN email of imported content — the Analytics page
// deep-links `author:<origin-email>`, but imported items carry the importer's
// git email on the commit and the upstream author only in origin-* header
// fields. resolveItems must build items whose author facet matches the origin
// email (else the analytics link lands on 0 results). Build via metaCommit so
// item.author/item.header are the real resolved shapes.
const importedMeta = {
  sha: (H("f") + "0".repeat(40)).slice(0, 40),
  author: "gitsocial-importer", email: "importer@localhost", ts: 9,
  header: 'GitMsg: v="0.1" ext="pm" type="issue" state="open" origin-author-name="Mitchell Hashimoto" origin-author-email="m@mitchellh.com"',
  subject: "Imported issue",
};
const importedItem = GS.resolveItems([GS.metaCommit(importedMeta)])[0];
eq(importedItem.author, "Mitchell Hashimoto", "resolved author is the origin (upstream) name, not the importer");
const importedExt = { pm: [importedItem], review: [], social: [], release: [], memo: [] };
eq(GS.searchItemsFaceted("author:m@mitchellh.com", importedExt, null).total, 1, "author:<origin-email> matches imported content (analytics deep-link)");
eq(GS.searchItemsFaceted("author:Mitchell", importedExt, null).total, 1, "author:<origin-name substring> matches too");
eq(GS.searchItemsFaceted("author:importer@localhost", importedExt, null).total, 1, "author:<git-commit-email> still matches (committer identity kept in the blob)");
eq(GS.searchItemsFaceted("author:nobody@example.com", importedExt, null).total, 0, "author: with an unrelated email matches nothing");

console.log("=== route parsing (board / search / tags) ===");
eq(GS.parseRoute("#/board"), { type: "board" }, "#/board route");
eq(GS.parseRoute("#/search"), { type: "search", q: "" }, "#/search route (empty query)");
eq(GS.parseRoute("#/search/author:alice"), { type: "search", q: "author:alice" }, "#/search/<q> carries a deep-link query");
eq(GS.parseRoute("#/search/" + encodeURIComponent("state:open dark")), { type: "search", q: "state:open dark" }, "#/search decodes an encoded query");
eq(GS.parseRoute("#/tags"), { type: "tags" }, "#/tags route");
eq(GS.parseRoute("#tag:v1.0"), { type: "tag", name: "v1.0" }, "#tag:<name> route");

console.log("=== B1 board config resolution ===");
const bcfg = GS.buildBoard(issues, { name: "Minimal", columns: [{ name: "Open", filter: "state:open" }, { name: "Closed", filter: "state:closed" }] });
eq(bcfg.columns.map((c) => c.name), ["Open", "Closed"], "buildBoard honors a configured minimal board");
eq(bcfg.name, "Minimal", "configured board name carried");
eq(bcfg.columns.map((c) => c.issues.length), [3, 2], "minimal columns: open (A,B,C) / closed (D,E)");
// malformed / empty config falls back to the kanban default
eq(GS.buildBoard(issues, null).columns.map((c) => c.name), ["Backlog", "In Progress", "Review", "Done"], "null config -> kanban default");
eq(GS.buildBoard(issues, { columns: [] }).columns.map((c) => c.name), ["Backlog", "In Progress", "Review", "Done"], "empty columns -> kanban default");
eq(GS.buildBoard(issues, { columns: [{ name: "", filter: "" }] }).columns.map((c) => c.name), ["Backlog", "In Progress", "Review", "Done"], "columns with no name/filter -> kanban default");
// WIP coercion from a configured column
eq(GS.buildBoard(issues, { columns: [{ name: "Doing", filter: "status:in-progress", wip: 2 }] }).columns[0].wip, 2, "configured WIP carried as a number");
eq(GS.boardColumnsFrom({ columns: [{ name: "X", filter: "state:open", wip: -1 }] }).columns[0].wip, 0, "non-positive WIP coerced to 0 (no limit)");

console.log("=== B4 search: hash / date filters + relevance ===");
const H40 = (h) => (h + "0".repeat(40)).slice(0, 40);
const mkH = (short, t, header, content, author, hash) => ({ commit: { hash: hash || H40(short), short }, header: header || {}, content: content || "", effectiveTime: t, author: author || "" });
const DT = (s) => Date.parse(s + "T12:00:00Z") / 1000;
const searchExt = {
  pm: [
    mkH("aaaaaaaaaaaa", DT("2026-03-01"), { type: "issue", state: "open" }, "Dark mode toggle", "Ada", "abc1234def5600000000000000000000000000ff"),
    mkH("bbbbbbbbbbbb", DT("2026-06-15"), { type: "issue", state: "open" }, "Bright theme\nsome dark text only in the body", "Bob"),
  ],
  review: [], social: [], release: [], memo: [],
};
// parseSearchFilters extraction
const pf = GS.parseSearchFilters("hash:abc1234 after:2026-01-01 before:2026-12-31 dark");
eq(pf.hashes, ["abc1234"], "hash: filter extracted");
eq(pf.terms, "dark", "free text kept after stripping typed filters");
ok(pf.dateFrom !== null && pf.dateTo !== null, "after:/before: bounds parsed");
// bare hex token becomes a hash prefix
eq(GS.parseSearchFilters("abc1234").hashes, ["abc1234"], "bare 7-40 hex token becomes a hash prefix");
eq(GS.parseSearchFilters("abc").hashes, [], "a <7-char token is NOT treated as a hash");
// strict date format: an invalid date is not a bound (stays literal)
ok(GS.parseSearchFilters("after:2026-13-40").dateFrom === null, "invalid strict date rejected (not a bound)");
// hash search surfaces the matching item prominently (first)
let hs = GS.searchItemsFaceted("hash:abc1234", searchExt);
eq(hs.total, 1, "hash:abc1234 matches exactly one item");
eq(hs.groups[0].items[0].commit.short, "aaaaaaaaaaaa", "hash match surfaces the right item");
eq(GS.searchItemsFaceted("abc1234def", searchExt).total, 1, "bare hex prefix matches by full-hash prefix");
// date filters (inclusive bounds on effectiveTime)
eq(GS.searchItemsFaceted("after:2026-05-01", searchExt).total, 1, "after: keeps only the later item");
eq(GS.searchItemsFaceted("before:2026-04-01", searchExt).total, 1, "before: keeps only the earlier item");
eq(GS.searchItemsFaceted("after:2026-03-01", searchExt).total, 2, "after: bound is inclusive of the day");
// relevance: a subject match ranks above a body-only match
const rel = GS.searchItemsFaceted("dark", searchExt);
eq(rel.groups[0].items.map((i) => i.commit.short), ["aaaaaaaaaaaa", "bbbbbbbbbbbb"], "subject match ranked above body-only match");
// itemMatchesHash direct
ok(GS.itemMatchesHash(searchExt.pm[0], ["abc1234"]) && !GS.itemMatchesHash(searchExt.pm[1], ["abc1234"]), "itemMatchesHash prefix-matches the full hash");

console.log("=== B11 board swimlanes ===");
const swIssues = [
  mk(H("1"), 1, { type: "issue", state: "open", labels: "priority/high,kind/bug" }, "A", "Ada"),
  mk(H("2"), 1, { type: "issue", state: "open", labels: "priority/low,kind/feature", assignees: "alice@x.com" }, "B", "Bob"),
  mk(H("3"), 1, { type: "issue", state: "closed", labels: "priority/high" }, "C", "Ada"),
  mk(H("4"), 1, { type: "issue", state: "open" }, "D", "Carol"),
];
eq(GS.SWIMLANE_FIELDS, ["", "priority", "kind", "assignees", "author"], "swimlane fields mirror pm.SwimlaneFields");
eq(GS.swimlaneValue(swIssues[0], "priority"), "high", "swimlaneValue: priority label");
eq(GS.swimlaneValue(swIssues[3], "priority"), "", "swimlaneValue: no priority -> ungrouped");
eq(GS.swimlaneValue(swIssues[1], "assignees"), "alice@x.com", "swimlaneValue: first assignee");
eq(GS.swimlaneValue(swIssues[0], "author"), "Ada", "swimlaneValue: author display name");
// priority order predefined, ungrouped last
eq(GS.swimlaneOrder(swIssues, "priority"), ["high", "low", ""], "priority lane order (predefined, present-only, ungrouped last)");
eq(GS.swimlaneOrder(swIssues, "kind"), ["bug", "feature", ""], "kind lane order predefined");
// author order: alphabetical, ungrouped last (none here)
eq(GS.swimlaneOrder(swIssues, "author"), ["Ada", "Bob", "Carol"], "author lanes alphabetical");
eq(GS.swimlaneOrder(swIssues, ""), [], "no field -> no lanes");
// custom label value appended before ungrouped
const custom = [mk(H("5"), 1, { type: "issue", labels: "priority/urgent" }, "X"), mk(H("6"), 1, { type: "issue" }, "Y")];
eq(GS.swimlaneOrder(custom, "priority"), ["urgent", ""], "custom priority value appended, ungrouped last");
// grouping buckets by lane
const lanes = GS.swimlaneOrder(swIssues, "priority");
const grouped = GS.groupBySwimlane(swIssues, "priority", lanes);
eq((grouped.get("high") || []).map((i) => i.commit.short), [H("1"), H("3")], "high lane holds both high-priority issues");
eq((grouped.get("") || []).map((i) => i.commit.short), [H("4")], "ungrouped lane holds the no-priority issue");
eq(GS.swimlaneLabel(""), "(none)", "ungrouped lane label");
eq(GS.swimlaneLabel("high"), "high", "value lane label verbatim");
// buildBoard carries defaultSwimlane from config
eq(GS.buildBoard(swIssues, { columns: [{ name: "O", filter: "state:open" }], defaultSwimlane: "kind" }).defaultSwimlane, "kind", "buildBoard carries defaultSwimlane");
eq(GS.buildBoard(swIssues, null).defaultSwimlane, "", "no config -> no default swimlane");

console.log("=== B3 header enrichment + interaction counts ===");
eq(GS.countsFor(new Map([[H("1"), { comments: 3 }]]), H("1")), { comments: 3 }, "countsFor returns the item's counts");
eq(GS.countsFor(new Map(), H("1")), null, "countsFor null when absent");

console.log("\n" + pass + " passed, " + fail + " failed");
process.exit(fail ? 1 : 0);

// unit_timeline_actions.js - DOM-free core units for the timeline actions: the same rules and fixtures as core/cache/actions_test.go
const GS = require("../assets/gs-core.js");
let pass = 0, fail = 0;
function eq(a, b, msg) { if (JSON.stringify(a) === JSON.stringify(b)) { pass++; } else { fail++; console.log("FAIL", msg, "got", JSON.stringify(a), "want", JSON.stringify(b)); } }
function commit(short, time, gitmsg, content, email) {
  return { hash: (short + "0".repeat(40)).slice(0, 40), short, subject: content, authorName: email || "a", authorEmail: email || "a@example.com", authorTime: time, content, rawMessage: content, gitmsg, refs: [] };
}
// actions lists a lane's action entries as "short action", oldest first; commits are given oldest first.
function actions(commits, ext) {
  const items = GS.resolveItems(commits.slice().reverse());
  return GS.itemActions(items, ext, "gitmsg/" + ext).sort((a, b) => a.effectiveTime - b.effectiveTime);
}
const names = (entries) => entries.map((e) => e.commit.short + " " + e._action);

for (const c of require("./action_fixtures.json").cases) eq(GS.headerAction(c.prev, c.cur), c.expect, "fixture: " + c.name);

const ISSUE = "a55000000001", EDITS = "#commit:" + ISSUE + "@gitmsg/pm";
const pm = (h) => Object.assign({ ext: "pm", v: "0.1.0", type: "issue" }, h);
const history = [
  commit(ISSUE, 100, pm({ state: "open" }), "Fix the build"),
  commit("ed0000000001", 110, pm({ edits: EDITS, state: "closed" }), "Fix the build"),
  commit("ed0000000002", 120, pm({ edits: EDITS, state: "closed", labels: "kind/bug" }), "Fix the build now"),
  commit("ed0000000003", 130, pm({ edits: EDITS, state: "open" }), "Fix the build now"),
  commit("ed0000000004", 140, pm({ edits: EDITS, state: "closed" }), "Fix the build now"),
];
eq(names(actions(history, "pm")), ["ed0000000001 closed", "ed0000000003 reopened", "ed0000000004 closed"], "issue history: a state change is an action, a label edit is not");
eq(actions(history, "pm")[2]._subject, "Fix the build now", "the subject is that of the version");
eq(actions(history, "pm")[0]._target.commit.short, ISSUE, "the target is the item");

eq(names(actions([
  commit(ISSUE, 100, pm({ state: "open" }), "Issue"),
  commit("ed0000000001", 110, pm({ edits: EDITS, state: "open", assignees: "a@example.com" }), "Issue"),
  commit("ed0000000002", 120, pm({ edits: EDITS }), "Issue again"),
], "pm")), [], "a text or assignee edit has no action");

eq(names(actions([
  commit(ISSUE, 100, pm({ state: "open" }), "Issue"),
  commit("c00000000001", 110, pm({ edits: EDITS, state: "closed", retracted: "true" }), "Issue"),
], "pm")), [], "a retraction has no action");

const PR = "d00000000001", PREDITS = "#commit:" + PR + "@gitmsg/review", FB = "feedbac00001";
const rv = (h) => Object.assign({ ext: "review", v: "0.1.0" }, h);
const review = [
  commit(PR, 100, rv({ type: "pull-request", state: "open", draft: "true" }), "Add the fix"),
  commit("de0000000001", 110, rv({ type: "pull-request", edits: PREDITS, state: "open" }), "Add the fix"),
  commit(FB, 120, rv({ type: "feedback", "pull-request": PREDITS, "review-state": "approved" }), "Looks right"),
  commit("feedbac00002", 130, rv({ type: "feedback", "pull-request": PREDITS, file: "a.go", "new-line": "3" }), "A comment"),
  commit("feedbac00003", 140, rv({ type: "feedback", edits: "#commit:" + FB + "@gitmsg/review", "review-state": "approved" }), "Looks right."),
  commit("feedbac00004", 150, rv({ type: "feedback", edits: "#commit:" + FB + "@gitmsg/review", "review-state": "changes-requested" }), "Not yet"),
  commit("de0000000002", 160, rv({ type: "pull-request", edits: PREDITS, state: "merged" }), "Add the fix"),
];
const reviewActions = actions(review, "review");
eq(names(reviewActions), ["de0000000001 ready", FB + " approved", "feedbac00004 changes-requested", "de0000000002 merged"], "pull request and review actions");
eq(reviewActions[1]._subject, "Add the fix", "a review names its pull request");
eq(reviewActions[1]._target.commit.short, PR, "a review targets its pull request");

const closes = "#commit:a55000000001@gitmsg/pm,#commit:a55000000002@gitmsg/pm";
const merge = actions([
  commit(PR, 3, rv({ type: "pull-request", state: "open", closes }), "Add the fix"),
  commit("de0000000001", 100, rv({ type: "pull-request", edits: PREDITS, state: "merged", closes }), "Add the fix"),
], "review");
const closeOf = (short, target, email, time) => commit(short, time, pm({ edits: "#commit:" + target + "@gitmsg/pm", state: "closed" }), "Issue", email);
const issues = actions([
  commit("a55000000001", 0, pm({ state: "open" }), "Issue"), commit("a55000000002", 1, pm({ state: "open" }), "Issue"), commit("a55000000003", 2, pm({ state: "open" }), "Issue"),
  closeOf("ed0000000001", "a55000000001", "a@example.com", 101),
  closeOf("ed0000000002", "a55000000002", "b@example.com", 102),
  closeOf("ed0000000003", "a55000000003", "a@example.com", 103),
], "pm");
eq(names(GS.dropMergeCloses(merge.concat(issues)).sort((a, b) => a.effectiveTime - b.effectiveTime)),
  ["de0000000001 merged", "ed0000000002 closed", "ed0000000003 closed"],
  "a merge hides the close it made, not one by another author, of another issue");

console.log(pass + " passed, " + fail + " failed");
process.exit(fail ? 1 : 0);

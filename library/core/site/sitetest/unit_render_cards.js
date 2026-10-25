// unit_render_cards.js - shim-rendered units for the card vocabulary:
// the chip row a header produces, the per-extension card dispatch, the avatar,
// the reply quote block, and the paged and auto-scrolling list wrappers.
require("./shim.js");
require("../assets/icons.js");
const GS = require("../assets/gs-render.js");
const { textOf, findTag } = global.__shim;
let pass = 0, fail = 0;
function eq(a, b, msg) { if (JSON.stringify(a) === JSON.stringify(b)) { pass++; } else { fail++; console.log("FAIL", msg, "got", JSON.stringify(a), "want", JSON.stringify(b)); } }
function ok(c, msg, extra) { if (c) { pass++; } else { fail++; console.log("FAIL", msg, extra == null ? "" : extra); } }
function findClass(node, cls, out) { out = out || []; for (const c of (node && node._children) || []) { if (c && c.nodeType === 1) { if (c._cls && c._cls.has(cls)) out.push(c); findClass(c, cls, out); } } return out; }
function fire(node, ev, props) { for (const fn of (node && node._handlers && node._handlers[ev]) || []) fn(Object.assign({ preventDefault() {}, stopPropagation() {}, key: "", button: 0, target: { closest() { return null; } } }, props || {})); }
const chipText = (node) => findClass(node, "chip").map(textOf);
const elems = (node) => ((node && node._children) || []).filter((c) => c && c.nodeType === 1);
const firstClass = (node) => String(node.className || "").trim().split(/\s+/)[0];
const H = (s) => (s.repeat(12)).slice(0, 12);
const sha40 = (s) => (s.repeat(40)).slice(0, 40);
function item(short, header, content, refs) {
  return {
    commit: { hash: sha40(short), short: H(short), authorName: "Ada", authorEmail: "ada@example.com", authorTime: 1750000000, refs: refs || [] },
    header: header || {}, content: content || "", author: "Ada", effectiveTime: 1750000000,
  };
}

console.log("=== header chips ===");
const rich = GS.prCard(item("a", {
  type: "pull-request", state: "open", head: "feature", base: "trunk",
  assignees: "ada@example.com, bob", labels: "priority/high,kind/bug,status/review", due: "2026-09-01",
}, "Add the reader"));
const richChips = chipText(rich);
ok(richChips.indexOf("☛ ada") !== -1, "an assignee chip shows the email local part", richChips.join(" | "));
ok(richChips.indexOf("☛ bob") !== -1, "an assignee without an @ is shown whole", richChips.join(" | "));
ok(richChips.indexOf("high") !== -1, "a priority label becomes its own chip", richChips.join(" | "));
ok(richChips.indexOf("kind/bug") !== -1, "a scoped label chip keeps its scope", richChips.join(" | "));
ok(richChips.indexOf("status/review") === -1, "a status label stays off the chip row", richChips.join(" | "));
ok(richChips.indexOf("due 2026-09-01") !== -1, "a due date becomes a chip", richChips.join(" | "));
ok(richChips.indexOf("feature → trunk") !== -1, "a pull request card names its head and base", richChips.join(" | "));
eq(findClass(rich, "chip-priority")[0].className.indexOf("prio-high") !== -1, true, "the priority chip carries its level class");
const chipHref = (cls) => findClass(rich, cls).map((c) => c.tagName + " " + textOf(c) + " " + c.getAttribute("href"));
eq(chipHref("chip-assignee"), ["A ☛ ada #/search/assignee%3Aada%40example.com", "A ☛ bob #/search/assignee%3Abob"], "an assignee chip opens the assignee search for the full email");
eq(chipHref("chip-label"), ["A kind/bug #/search/label%3Akind%2Fbug"], "a label chip opens the label search for the scoped label");
eq(chipHref("chip-priority"), ["A high #/search/label%3Apriority%2Fhigh"], "a priority chip searches its priority label");
eq(findClass(rich, "chip-due").map((c) => c.tagName), ["SPAN"], "a due chip stays text");

// markerOf reads the word that ends a card's author line, or "" when the line ends on another bit.
const markerOf = (node) => { const last = elems(findClass(node, "meta")[0]).pop(); return last && last._cls.has("marker") ? textOf(last) : ""; };
const marked = GS.memoCard(item("b", { type: "memo", retracted: "true", "origin-platform": "github", "origin-url": "https://example.com/1" }, "Memo"));
eq(markerOf(marked), "retracted", "retracted is worded at the end of the author line");
eq(chipText(findClass(marked, "meta")[0]), [], "the marker is a plain bit, not a chip");
eq(chipText(findClass(marked, "card-head")[0]), [], "the head holds the subject alone");
eq(chipText(findClass(marked, "card-chips")[0]), ["↗ github"], "and the chip row carries the origin badge");
const deadComment = GS.timelineCard(Object.assign(item("z", { type: "comment", retracted: "true" }, "Gone"), { _ext: "social" }));
eq(markerOf(deadComment), "retracted", "a body-only card ends its author line with the marker too");
const closedIssue = GS.timelineCard(Object.assign(item("y", { type: "issue", state: "closed" }, "Closed one"), { _ext: "pm" }));
eq([markerOf(closedIssue), chipText(findClass(closedIssue, "card-head")[0])], ["closed", []], "a state other than open is worded on the author line, off the head");
eq(markerOf(GS.timelineCard(Object.assign(item("y", { type: "issue", state: "open" }, "Open one"), { _ext: "pm" }))), "", "an open item words no state");
eq(markerOf(GS.prCard(item("y", { type: "pull-request", state: "open", draft: "true" }, "Drafted"))), "draft", "a draft pull request words its marker");
const editedClosed = GS.timelineCard(Object.assign(item("y", { type: "issue", state: "closed" }, "Closed one"), { _ext: "pm", edited: true }));
eq([markerOf(editedClosed), findClass(editedClosed, "edited").length], ["closed", 0], "the state word stands in for the edited marker");
eq(findClass(marked, "chip-origin")[0].getAttribute("title"), "https://example.com/1", "the origin chip links its upstream URL in the title");
eq(chipText(GS.memoCard(item("c", { type: "memo", "origin-author-email": "49699333+dependabot[bot]@users.noreply.github.com", "origin-author-name": "dependabot[bot]" }, "Bump"))).indexOf("⚙ bot") !== -1, true, "an automation author gets a bot chip");
eq(chipText(GS.memoCard(item("d", { type: "memo", "origin-author-email": "ada@example.com", "origin-author-name": "Ada" }, "Note"))).indexOf("⚙ bot"), -1, "a human origin author gets no bot chip");

console.log("=== the counts lead the chip row, as chips ===");
const counted = GS.prCard(item("e", { type: "pull-request", state: "open", head: "feature", base: "trunk", labels: "kind/bug" }, "Counted"), { approved: 2, changesRequested: 1, comments: 4, reposts: 3, quotes: 1 });
const countsOf = (node) => findClass(node, "count").map(textOf);
eq(countsOf(counted), ["↩ 4", "↻ 3", "❞ 1", "✓ 2", "✗ 1"], "comments, reposts, quotes and reviews each give an icon and a number");
eq(findClass(counted, "count").map((c) => c.className), ["chip count", "chip count", "chip count", "chip count review-ok", "chip count review-no"], "each count is a chip, the review ones marked by verdict");
eq(elems(findClass(counted, "card-chips")[0]).map(textOf), ["↩ 4", "↻ 3", "❞ 1", "✓ 2", "✗ 1", "feature → trunk", "kind/bug"], "the counts lead the chip row, then the branch pair, then the labels");
eq(findClass(counted, "card-counts").length, 0, "the counts have no row of their own");
eq(elems(counted).slice(-1).map(firstClass), ["card-chips"], "the chip row is the last part of the card");
eq(countsOf(GS.prCard(item("e", { type: "pull-request", state: "open" }, "Quiet"), { approved: 0, comments: 0 })), [], "a count of zero gives no chip");
eq(findClass(GS.timelineCard(Object.assign(item("e", { type: "issue", state: "open" }, "Uncounted"), { _ext: "pm" })), "card-chips").length, 0, "a card with no count and no chip has no row");
eq(countsOf(GS.timelineCard(Object.assign(item("e", { type: "post" }, "Counted post"), { _ext: "social" }), { comments: 2 })), ["↩ 2"], "a post carries its comment count");

console.log("=== a pull request card links its branches and its stack ===");
const anchorsOf = (node) => findTag(node, "a").map((a) => textOf(a) + "@" + a.getAttribute("href"));
const stacked = GS.prCard(item("f", { type: "pull-request", state: "open", head: "#branch:feature", base: "#branch:trunk", "depends-on": "#commit:" + H("e") + "@gitmsg/review" }, "Stacked"));
const flow = findClass(stacked, "chip").find((c) => textOf(c).indexOf("→") !== -1);
eq(textOf(flow), "feature → trunk", "the branch chip names the head and the base");
eq(anchorsOf(flow), ["feature@#branch:feature", "trunk@#branch:trunk"], "each side links to its branch log");
eq(findClass(stacked, "chip").filter((c) => textOf(c) === "stacked").map((c) => c.tagName + "@" + c.getAttribute("href")), ["A@#commit:" + H("e") + "@gitmsg/review"], "the stacked chip opens the pull request it depends on");
const forked = GS.prCard(item("g", { type: "pull-request", state: "open", head: "https://example.com/fork#branch:feature", base: "#branch:trunk" }, "From a fork"));
const forkFlow = findClass(forked, "chip").find((c) => textOf(c).indexOf("→") !== -1);
eq(anchorsOf(forkFlow), ["example.com/fork:feature@https://example.com/fork#branch:feature", "trunk@#branch:trunk"], "a fork head reads owner/repo:branch and links to its URL as written");
eq(findTag(forkFlow, "a")[0].getAttribute("title"), "https://example.com/fork#branch:feature", "the fork head carries the full field as its title");
eq(elems(findClass(stacked, "card-chips")[0]).map(textOf).slice(0, 2), ["feature → trunk", "stacked"], "the branch pair leads the chip row, the stacked chip after it");

console.log("=== a release row counts its assets after its hash ===");
const metaBits = (node) => textOf(findClass(node, "meta")[0]).split(" · ");
const shipped = GS.releaseCard(item("r", { type: "release", tag: "v1.0", version: "1.0", artifacts: "linux.tar.gz, mac.tar.gz" }, "Ship it"));
eq(metaBits(shipped).slice(0, 1).concat(metaBits(shipped).slice(2)), ["Ada", H("r").slice(0, 12), "2 assets"], "the release row ends on its hash and its asset count");
eq(chipText(shipped).indexOf("2 assets"), -1, "the count is a meta bit, not a head chip");
eq(metaBits(GS.releaseCard(item("s", { type: "release", tag: "v1.1", artifacts: "one.tar.gz" }, "Ship one"))).slice(3), ["1 asset"], "one artifact reads singular");
eq(metaBits(GS.releaseCard(item("t", { type: "release", tag: "v1.2" }, "Ship none"))).length, 3, "a release naming no artifact drops the bit");
eq(findClass(GS.memoCard(item("u", { type: "memo" }, "A memo")), "hash").length, 1, "every other row keeps its hash");

console.log("=== a row's type glyph takes its class from the item type ===");
const FIX = JSON.parse(require("fs").readFileSync(require("path").join(__dirname, "parity_fixtures.json"), "utf8"));
for (const c of FIX.rowGlyphs) {
  const row = (c.ext === "memo" ? GS.memoCard : GS.timelineCard)(Object.assign(item("g", c.header, "A row"), { _ext: c.ext, _branch: "gitmsg/" + c.ext }));
  const g = findClass(row, "type-glyph")[0];
  eq(Array.from(g._cls).filter((x) => x !== "type-glyph"), [c.expectClass], c.name + ": glyph class");
  eq(g.getAttribute("title"), c.expectTitle, c.name + ": glyph title");
}

console.log("=== a sidebar count is exact under a thousand and compact above ===");
for (const [n, want] of [[999, "999"], [1000, "1K"], [1234, "1.2K"], [12322, "12.3K"], [999999, "1M"], [1200000, "1.2M"]]) {
  eq(GS.compactCount(n), want, n + " reads " + want);
}

console.log("=== card dispatch and navigation ===");
const branchOf = (node) => (findClass(node, "subject")[0].getAttribute("href") || "").split("@")[1];
eq(branchOf(GS.timelineCard(Object.assign(item("1", { type: "issue", state: "open" }, "An issue"), { _ext: "pm" }))), "gitmsg/pm", "a pm item routes to the pm branch");
eq(branchOf(GS.timelineCard(Object.assign(item("2", { type: "pull-request", state: "open" }, "A PR"), { _ext: "review" }))), "gitmsg/review", "a review item routes to the review branch");
eq(branchOf(GS.timelineCard(Object.assign(item("3", { type: "release", tag: "v1.2" }, "v1.2"), { _ext: "release" }))), "gitmsg/release", "a release item routes to the release branch");
eq(branchOf(GS.timelineCard(Object.assign(item("4", { type: "post" }, "A post"), { _ext: "social" }))), "gitmsg/social", "a social item routes to the social branch");
const codeCard = GS.timelineCard({ _ext: "code", _branch: "trunk", commit: { hash: sha40("9"), short: H("9"), content: "Fix the walk", authorName: "Ada", authorEmail: "ada@example.com", authorTime: 1750000000 } });
eq(branchOf(codeCard), "trunk", "a code commit routes to its branch");
ok(chipText(codeCard).indexOf("trunk") !== -1, "a timeline code card carries its branch chip", chipText(codeCard).join(" | "));
eq(findClass(codeCard, "chip").map((c) => c.tagName + "@" + c.getAttribute("href")), ["A@#branch:trunk"], "the branch chip opens the branch log");
global.location.hash = "#/";
fire(codeCard, "click");
eq(global.location.hash, "#commit:" + sha40("9") + "@trunk", "clicking a card navigates to its detail route");
global.location.hash = "#/";
fire(codeCard, "click", { target: { closest: (s) => (s === "a" ? {} : null) } });
eq(global.location.hash, "#/", "a click that landed on an inner link does not navigate");
global.__selection = { isCollapsed: false };
fire(codeCard, "click");
eq(global.location.hash, "#/", "a click that ends a text selection does not navigate");
global.__selection = { isCollapsed: true };

console.log("=== every card leads with its avatar and its glyph, then its author line; a commit has no glyph ===");
const parts = (node) => elems(node).map(firstClass);
const shapes = [
  ["an issue", GS.timelineCard(Object.assign(item("1", { type: "issue", state: "open" }, "An issue"), { _ext: "pm" })), "card-head"],
  ["a pull request", GS.timelineCard(Object.assign(item("2", { type: "pull-request", state: "open" }, "A PR"), { _ext: "review" })), "card-head"],
  ["a release", GS.releaseCard(item("3", { type: "release", tag: "v1.2" }, "v1.2")), "card-head"],
  ["a memo", GS.memoCard(item("4", { type: "memo" }, "A memo")), "card-head"],
  ["a titled post", GS.timelineCard(Object.assign(item("5", { type: "post" }, "A post\n\nIts body"), { _ext: "social" })), "card-head"],
  ["a commit", GS.timelineCard({ _ext: "code", _branch: "trunk", commit: { hash: sha40("6"), short: H("6"), content: "Fix the walk", authorName: "Ada", authorEmail: "ada@example.com", authorTime: 1750000000 } }), "card-head", false],
  ["a comment", GS.timelineCard(Object.assign(item("7", { type: "comment" }, "A reply"), { _ext: "social" })), "body-clamp-wrap"],
  ["a milestone", findClass(GS.milestonesBody([item("m", { type: "milestone", state: "open", due: "2026-09-01" }, "v1.0 Launch")])[0], "card")[0], "card-head"],
  ["a sprint", findClass(GS.sprintsBody([item("n", { type: "sprint", state: "active", start: "2026-07-01", end: "2026-07-14" }, "Sprint 1")])[0], "card")[0], "card-head"],
  ["a search result", GS.searchResultCard(item("o", { type: "issue", state: "open" }, "Found it"), { ext: "pm", branch: "gitmsg/pm" }, "found"), "card-head"],
  ["a commit of the commits page", GS.commitCard({ hash: sha40("p"), short: H("p"), content: "Fix the walk", authorName: "Ada", authorEmail: "ada@example.com", authorTime: 1750000000 }, "trunk", { id: "c-" + H("p"), time: "2025-06-15", refSha: H("p") }), "card-head", false],
];
for (const [name, node, third, glyph] of shapes) {
  const want = glyph === false ? ["avatar", "meta", third] : ["avatar", "type-glyph", "meta", third];
  eq(parts(node).slice(0, want.length), want, name + ": the avatar, " + (glyph === false ? "no glyph" : "the glyph") + ", the author line, then the " + (third === "card-head" ? "head" : "body"));
}
const typeless = GS.timelineCard(Object.assign(item("8", { type: "proposal" }, "An unknown type"), { _ext: "pm" }));
eq([firstClass(elems(typeless)[1]), textOf(elems(typeless)[1])], ["type-glyph", ""], "a type with no glyph keeps the mark column, empty");
eq(GS.metaRow(item("9", { type: "post" }, "x"), "gitmsg/social")._children.filter((c) => c && c.nodeType === 1).map(firstClass)[0], "author", "the author line opens on the author");
const groupMeta = textOf(elems(shapes.find((s) => s[0] === "a milestone")[1])[2]);
eq(groupMeta.split(" · ").filter((b, i) => i !== 1), ["Ada", H("m"), "due 2026-09-01", "0 issues"], "a milestone's author line carries the due date and the issue count after the hash");

console.log("=== a timeline action is the actor line, the head and the folded text ===");
const target = item("t", { type: "issue", state: "closed" }, "Fix the parser");
const action = (header, content) => Object.assign(item("u", header, content), { _ext: "pm", _branch: "gitmsg/pm", _action: "closed", _target: target, _subject: "Fix the parser" });
const closed = GS.timelineCard(action({ edits: "#commit:" + H("t") + "@gitmsg/pm", state: "closed" }, "Fix the parser\n\nThe parser drops a line"));
eq(parts(closed), ["avatar", "type-glyph", "meta", "card-head", "excerpt"], "an action card is the avatar, the glyph, the actor line, the head and the excerpt");
ok(/^Ada closed · /.test(textOf(elems(closed)[2])), "the actor line names the actor and the verb", textOf(elems(closed)[2]));
const byBob = action({ edits: "#commit:" + H("t") + "@gitmsg/pm", state: "closed" }, "Fix the parser");
byBob.author = "Bob Stone";
byBob.commit.authorEmail = "bob@example.com";
eq(textOf(elems(GS.timelineCard(byBob))[0]), "BS", "an action card's avatar is its actor's, not the item author's");
ok(textOf(findClass(closed, "excerpt")[0]).indexOf("The parser drops a line") !== -1, "the excerpt is the body of the edit", textOf(closed));
const fold = findClass(closed, "body-clamp-wrap")[0];
const foldToggle = findClass(closed, "body-clamp-toggle")[0];
const foldState = () => [fold._cls.has("folded"), foldToggle.getAttribute("aria-label"), foldToggle.getAttribute("title"), foldToggle._cls.has("open")];
eq([foldToggle.tagName, findClass(foldToggle, "chevron").length], ["BUTTON", 1], "the fold toggle is a chevron button");
eq(foldState(), [true, "Show more", "Show more", false], "the text is folded behind Show more");
fire(foldToggle, "click");
eq(foldState(), [false, "Show less", "Show less", true], "a click opens the text and turns the chevron");
eq(findClass(GS.timelineCard(action({ state: "closed" }, "Fix the parser")), "excerpt").length, 0, "an edit with no body carries no excerpt");
const review = Object.assign(item("v", { type: "feedback", "review-state": "changes-requested" }, "Split this function"), { _ext: "review", _branch: "gitmsg/review", _action: "changes-requested", _target: target, _subject: "A PR" });
ok(textOf(findClass(GS.timelineCard(review), "excerpt")[0]).indexOf("Split this function") !== -1, "a review's excerpt is its whole text");
ok(/^Ada requested changes · /.test(textOf(findClass(GS.timelineCard(review), "meta")[0])), "a changes-requested review reads requested changes", textOf(findClass(GS.timelineCard(review), "meta")[0]));

console.log("=== a timeline entry shows its body as an excerpt ===");
const withBody = () => Object.assign(item("x", { type: "issue", state: "open" }, "Fix it\n\nThe parser drops a line"), { _ext: "pm" });
const excerpts = findClass(GS.timelineCard(withBody()), "excerpt");
eq(excerpts.length, 1, "an issue with a body carries one excerpt");
ok(textOf(excerpts[0]).indexOf("The parser drops a line") !== -1, "the excerpt holds the body", textOf(excerpts[0]));
eq(findClass(GS.timelineCard(Object.assign(item("x", { type: "issue", state: "open" }, "Fix it"), { _ext: "pm" })), "excerpt").length, 0, "an issue with no body carries no excerpt");
const issueList = GS.issuesBody([withBody()], null)[0];
eq([findClass(issueList, "card").length, findClass(issueList, "excerpt").length], [1, 0], "the issue list row carries no excerpt");

console.log("=== a comment renders whole, under the quote it answers ===");
const reply = item("f", { type: "comment", original: "#commit:" + H("1") + "@gitmsg/social" }, "First line of the reply\nand the rest",
  [{ ref: "#commit:" + H("1") + "@gitmsg/social", quoted: "> the original post", author: "Bob", email: "bob@example.com", time: "2026-01-02T03:04:05Z", type: "post" }]);
const replyCard = GS.timelineCard(Object.assign(reply, { _ext: "social" }));
eq(findClass(replyCard, "subject").length, 0, "a comment card has no subject link, so its first line stays a sentence");
const quote = findClass(replyCard, "reply-quote")[0];
ok(!!quote, "the comment carries the excerpt it answers");
ok(textOf(quote).indexOf("the original post") !== -1, "the excerpt shows the quoted text", textOf(quote));
ok(textOf(quote).indexOf("Bob") !== -1, "the excerpt names its author", textOf(quote));
ok(quote._cls.has("dimmed"), "a comment dims the excerpt it answers");
global.location.hash = "#/";
fire(quote, "click");
eq(global.location.hash, "#commit:" + H("1") + "@gitmsg/social", "clicking the excerpt opens what the comment answers");
const quoteHead = elems(quote)[0];
eq([firstClass(quoteHead), elems(quoteHead).map(firstClass).slice(0, 3)], ["reply-quote-head", ["avatar", "type-glyph", "author"]], "the quote box leads with the avatar and the glyph of the quoted item, then its author line");
eq(textOf(elems(quoteHead)[0]), "B", "the quote box's avatar is the quoted author's");
const repost = GS.timelineCard(Object.assign(item("g", { type: "repost", original: "#commit:" + H("1") + "@gitmsg/social" }, "# Bob: the original post", [{ ref: "#commit:" + H("1") + "@gitmsg/social", quoted: "> the original post", author: "Bob", type: "post" }]), { _ext: "social" }));
ok(!findClass(repost, "reply-quote")[0]._cls.has("dimmed"), "a repost shows the original undimmed");
ok(/^Ada reposted · /.test(textOf(findClass(repost, "meta")[0])), "a repost's author line reads Name reposted", textOf(findClass(repost, "meta")[0]));
eq(parts(repost), ["avatar", "type-glyph", "meta", "reply-quote"], "a repost shows the original in the quote box in place of its own text");

console.log("=== an avatar is the author's initials in one of twelve hues ===");
const avatar = (name, email) => GS.authorAvatar(name, email);
const hueOf = (name, email) => Array.from(avatar(name, email)._cls).filter((c) => c !== "avatar");
eq([textOf(avatar("Ada Lovelace", "ada@example.com")), avatar("Ada Lovelace", "ada@example.com").getAttribute("title")], ["AL", "Ada Lovelace"], "a two-word name gives two initials, the name in the title");
eq(textOf(avatar("ada augusta king", "")), "AA", "the first two words count, uppercased");
eq(textOf(avatar("Linus", "linus@example.com")), "L", "a one-word name gives one letter");
eq(textOf(avatar("Иван Петров", "")), "ИП", "a Cyrillic name gives its own letters");
eq(textOf(avatar("𝓐da 𝓑yron", "")), "𝓐𝓑", "an astral letter stays one whole code point");
eq(textOf(avatar("李小龍", "")), "李", "a name with no space gives its first character");
eq([textOf(avatar("", "grace@example.com")), avatar("", "grace@example.com").getAttribute("title")], ["G", "grace@example.com"], "an email alone gives the first letter of its local part");
eq(textOf(avatar("", "")), "?", "no name and no email give a question mark");
eq(hueOf("Ada Lovelace", "ada@example.com"), hueOf("Someone Else", "ADA@example.com"), "the hue depends on the email only, its case aside");
const hues = new Set(Array.from({ length: 200 }, (_, i) => hueOf("Ada", "author" + i + "@example.com")[0]));
ok(Array.from(hues).every((c) => /^av-(\d|1[01])$/.test(c)) && hues.size === GS.AVATAR_HUES, "every hue class is one of twelve, and two hundred authors use them all", Array.from(hues).join(" "));
const gitAuthored = GS.commitCard({ hash: sha40("q"), short: H("q"), content: "Import the tree", authorName: "Ada", authorEmail: "ada@example.com", authorTime: 1750000000, gitmsg: { "origin-author-name": "Grace Hopper", "origin-author-email": "grace@example.com" } }, "trunk", {});
eq([textOf(elems(gitAuthored)[0]), hueOf("", "grace@example.com")[0]], ["GH", Array.from(elems(gitAuthored)[0]._cls)[1]], "a commit card's avatar is the author its author line names");

console.log("=== a list member ===");
const memberOf = (ref) => findTag(GS.listMemberRow(ref), "a").map((a) => a.getAttribute("href")).concat(findClass(GS.listMemberRow(ref), "selectable").map(textOf));
eq(memberOf("#branch:main"), ["#branch:main"], "a local member links to its route");
eq(memberOf("https://example.com/repo#commit:abc123def456@gitmsg/pm"), ["https://example.com/repo#commit:abc123def456@gitmsg/pm"], "a browsable repository member links to its URL with the fragment");
eq(memberOf("s3://bucket/repo#branch:main"), ["s3://bucket/repo#branch:main"], "a member that is not browsable stays selectable text");

console.log("=== list wrappers ===");
eq(GS.renderList([], () => null, "No issues in this repository.").map(textOf), ["No issues in this repository."], "an empty list renders its sentence");
eq(textOf(GS.countHead(1, "branch", "branches")) + "|" + textOf(GS.countHead(3, "branch", "branches")), "1 branch|3 branches", "the count head picks the singular or plural noun");
eq(textOf(GS.listHeading("prs")), "Pull Requests", "a list heading is the nav label verbatim");

let served = 0;
const loadMore = async () => { served++; return { items: ["a", "b", "c"].slice(0, served + 1), truncated: served < 2 }; };
const drawn = [];
const paged = GS.pagedListView({ items: ["a"], truncated: true }, (items, box) => { drawn.push(items.slice()); box.replaceChildren(GS.el("div", {}, [items.join(",")])); }, loadMore)[0];
const moreBtn = () => findClass(paged, "load-more")[0];
ok(!!moreBtn(), "a truncated walk offers Load more");
fire(moreBtn(), "click");
setTimeout(() => {
  eq(drawn.map((d) => d.join(",")), ["a", "a,b"], "Load more redraws the body with the next window");
  ok(!!moreBtn(), "a still-truncated walk keeps the control");
  fire(moreBtn(), "click");
  setTimeout(() => {
    eq(drawn.length, 3, "the second Load more redraws again");
    eq(moreBtn(), undefined, "the control goes once the walk is complete");
    autoScroll();
  }, 5);
}, 5);

const observers = [];
global.IntersectionObserver = function (cb, opts) {
  observers.push(this);
  this.cb = cb; this.opts = opts; this.disconnected = false;
  this.observe = () => {}; this.unobserve = () => {}; this.disconnect = () => { this.disconnected = true; };
};

// autoScroll runs after the paged assertions so the two list wrappers do not interleave.
function autoScroll() {
  console.log("=== auto-scrolling list ===");
  let windows = 0;
  const seen = [];
  const auto = GS.autoScrollListView({ items: ["one"], truncated: true },
    (items, box) => { seen.push(items.join(",")); box.replaceChildren(GS.el("div", {}, [items.join(",")])); },
    async () => { windows++; return { items: ["one", "two"], truncated: windows < 2 }; })[0];
  eq(seen, ["one"], "the first window draws at once");
  eq(observers.length, 1, "a truncated walk observes its sentinel");
  eq(observers[0].opts.rootMargin, "600px", "the sentinel fires before it reaches the viewport");
  observers[0].cb([{ isIntersecting: false }, { isIntersecting: true }]);
  setTimeout(() => {
    eq(seen, ["one", "one,two"], "an intersecting sentinel loads the next window");
    auto.__loadNext();
    setTimeout(() => {
      eq(seen.length, 3, "__loadNext advances one window directly");
      ok(observers[0].disconnected, "the observer disconnects once the walk is complete");
      auto.__loadNext();
      setTimeout(() => {
        eq(seen.length, 3, "an exhausted walk advances no further");
        rest();
      }, 5);
    }, 5);
  }, 5);
}

// rest holds the assertions that need no scheduling and the branch and tag views, and closes the run.
async function rest() {
  console.log("=== a branch card and a tag card lead with their glyph ===");
  // refsCtx reads its branches and tags from the manifest and the tags artifact alone, and knows the release branch is absent, so nothing is fetched.
  const refsCtx = {
    base: "http://bucket.test/repo/", walks: {}, refMisses: new Set(["refs/heads/gitmsg/release"]),
    head: Promise.resolve({ branch: "refs/heads/main", sha: sha40("1") }),
    manifest: Promise.resolve({ "refs/heads/main": sha40("1"), "refs/heads/feature": sha40("2"), "refs/tags/v1.0": sha40("3") }),
    siteTags: { version: 1, tags: [{ name: "v1.0", sha: sha40("3"), commit: sha40("3"), time: 1750000000, author: "Ada", email: "ada@example.com", prev: "", count: 2 }] },
  };
  const branchCards = (await GS.branchesView(refsCtx)).flatMap((n) => (n._cls && n._cls.has("card") ? [n] : []));
  eq(branchCards.map((c) => parts(c)), [["type-glyph", "card-head"], ["type-glyph", "card-head"]], "a branch card is the glyph, then its head, since a branch has no author line");
  eq(branchCards.map((c) => textOf(elems(c)[0])), ["⎇", "⎇"], "the branch glyph is the sidebar's");
  const tagCards = (await GS.tagsView(refsCtx)).flatMap((n) => findClass(n, "card"));
  eq(tagCards.map((c) => parts(c)), [["avatar", "type-glyph", "meta", "card-head"]], "a tag card is the tagger's avatar, the glyph, the author line, then its head");
  eq(tagCards.map((c) => textOf(elems(c)[1])), ["⌗"], "the tag glyph is the sidebar's");
  const listsCtx = { base: "http://bucket.test/repo/", walks: {}, manifest: Promise.resolve({ "refs/gitmsg/social/lists/friends/items/aaaaaaaaaaaa": sha40("4") }) };
  const listCards = (await GS.listsView(listsCtx)).flatMap((n) => findClass(n, "card"));
  eq(listCards.map((c) => parts(c)), [["type-glyph", "card-head", "meta"]], "a list card is the glyph alone in its mark column, with no avatar");

  console.log("=== the state filter bar ===");
  const many = Array.from({ length: 150 }, (_, i) => item("h", { type: "pull-request", state: i < 40 ? "merged" : "open" }, "PR " + i));
  const filtered = GS.filteredListView(many, (it) => GS.prCard(it), "prs", GS.PR_STATES, "No pull requests in this repository.")[0];
  eq(findClass(filtered, "filter-chip").map(textOf), ["All 150", "Open 110", "Merged 40", "Closed 0"], "each state chip carries its exact count");
  ok(findClass(filtered, "filter-chip")[1]._cls.has("active"), "the list opens on the Open filter");
  eq(textOf(findClass(filtered, "load-more")[0]), "Load more (10)", "the open rows page like any other");
  fire(findClass(filtered, "filter-chip")[0], "click");
  eq(findClass(filtered, "card").length, 100, "the first page holds 100 rows");
  eq(textOf(findClass(filtered, "load-more")[0]), "Load more (50)", "the control names how many are left");
  fire(findClass(filtered, "load-more")[0], "click");
  eq(findClass(filtered, "card").length, 150, "Load more draws the rest");
  eq(findClass(filtered, "load-more").length, 0, "and the control goes");
  fire(findClass(filtered, "filter-chip")[2], "click");
  eq(findClass(filtered, "card").length, 40, "selecting a state filters the list");
  ok(findClass(filtered, "filter-chip")[2]._cls.has("active"), "the selected chip is marked active");
  fire(findClass(filtered, "filter-chip")[3], "click");
  eq(findClass(filtered, "empty").map(textOf), ["Nothing matches this filter."], "a state with no rows says the filter matched nothing");
  const none = GS.filteredListView([], (it) => GS.prCard(it), "prs", GS.PR_STATES, "No pull requests in this repository.")[0];
  eq(findClass(none, "empty").map(textOf), ["No pull requests in this repository."], "an empty list keeps the empty sentence");
  fire(findClass(filtered, "filter-chip")[0], "click");

  console.log("=== board swimlanes ===");
  const boardIssues = [
    item("i", { type: "issue", state: "open", labels: "priority/high" }, "High one"),
    item("j", { type: "issue", state: "open", labels: "priority/low" }, "Low one"),
    item("k", { type: "issue", state: "closed", labels: "priority/high" }, "High two"),
  ];
  const board = GS.boardBody(boardIssues, { columns: [{ name: "Open", filter: "state:open" }, { name: "Closed", filter: "state:closed" }], defaultSwimlane: "priority" });
  eq(findClass(board, "board-lane").length, 2, "grouping by priority splits the board into lanes");
  eq(findClass(board, "board-lane-jump").map(textOf), ["high (2)", "low (1)"], "the lane index counts each lane");
  let scrolled = null;
  findClass(board, "board-lane")[1].scrollIntoView = () => { scrolled = "low"; };
  fire(findClass(board, "board-lane-jump")[1], "click");
  eq(scrolled, "low", "a lane jump scrolls to its lane");
  fire(findClass(board, "board-lane-head")[0], "click");
  ok(findClass(board, "board-lane")[0]._cls.has("board-lane-collapsed"), "clicking a lane head collapses it");

  console.log("=== board empty-column default ===");
  const flat = GS.boardBody(boardIssues, { columns: [{ name: "Open", filter: "state:open" }, { name: "Closed", filter: "state:closed" }, { name: "Blocked", filter: "state:blocked" }] });
  const flatCols = findClass(flat, "board-col");
  ok(flatCols[2]._cls.has("board-col-collapsed"), "a column with no issues starts collapsed");
  ok(!flatCols[0]._cls.has("board-col-collapsed"), "a column with issues starts expanded");
  fire(findClass(flat, "board-col-toggle")[2], "click");
  ok(!findClass(flat, "board-col")[2]._cls.has("board-col-collapsed"), "the caret expands a default-collapsed column");
  fire(findClass(flat, "board-col-toggle")[2], "click");
  ok(findClass(flat, "board-col")[2]._cls.has("board-col-collapsed"), "the reader's own toggle outlives the empty default");

  console.log("=== icons and focus targets ===");
  const goIcon = GS.icon("main.go");
  ok(!!goIcon && goIcon._cls.has("gs-icon"), "a filename resolves to an icon span");
  ok(findTag(goIcon, "svg").length === 1, "the icon span holds one cloned glyph");
  eq(findTag(goIcon, "svg")[0].getAttribute("aria-hidden"), "true", "the glyph is hidden from assistive tech");
  ok(GS.icon("main.go", null, "tree-icon")._cls.has("tree-icon"), "an extra class rides the icon span");
  eq(GS.focusSearchInput(), false, "focusSearchInput reports false with no search box mounted");
  eq(GS.focusTreeSearch(), false, "focusTreeSearch reports false with no tree mounted");

  console.log("=== fork pull request row ===");
  // The row paints from the manifest alone: no fetch is stubbed here, so an object read would throw.
  const forkHeader = 'GitMsg: v="0.1.0" ext="review" type="pull-request" state="open" base="https://example.com/upstream#branch:main" head="#branch:feature" head-tip="' + H("7") + '"';
  const forkDoc = { version: 1, bytes: 10, items: [
    { sha: sha40("5"), author: "Eve", email: "eve@example.com", ts: 1750000000, header: forkHeader, subject: "From the fork", repo: "https://example.com/fork", head: sha40("7"), pack: "pack-abc", packBytes: 10 },
    { sha: sha40("6"), author: "Eve", email: "eve@example.com", ts: 1750000100, header: 'GitMsg: v="0.1.0" ext="review" edits="#commit:' + H("5") + '@gitmsg/review" state="open"', subject: "From the fork", repo: "https://example.com/fork" },
  ] };
  const forkItems = GS.forkPullItems(forkDoc);
  eq(forkItems.length, 1, "a manifest gives one item per canonical, its edits folded in");
  eq(forkItems[0].versions.length, 2, "the canonical and its edit build the versions");
  eq(forkItems[0].fork, { repo: "https://example.com/fork", head: sha40("7"), pack: "pack-abc", packBytes: 10, pending: false, tooLarge: false }, "the canonical entry's fork fields ride the item");
  const forkRow = GS.prCard(forkItems[0]);
  ok(chipText(forkRow).indexOf("fork") !== -1, "a fork pull request row carries the fork chip", chipText(forkRow).join(" | "));
  const forkMeta = findClass(forkRow, "meta")[0];
  ok(textOf(forkMeta).indexOf("https://example.com/fork") !== -1, "the row meta names the fork URL", textOf(forkMeta));
  ok(findTag(forkMeta, "a").some((a) => a.getAttribute("href") === "https://example.com/fork"), "the fork URL links to the fork");
  eq(textOf(findClass(forkRow, "subject")[0]), "From the fork", "the subject comes from the manifest");
  eq(chipText(GS.prCard(item("p", { type: "pull-request", state: "open" }, "Local"))).indexOf("fork"), -1, "a local pull request row carries no fork chip");
  eq(GS.forkPullItems({ version: 2, items: forkDoc.items }).length, 0, "a manifest at another version reads as no fork pull requests");
  eq(GS.forkPullItems(null).length, 0, "no manifest reads as no fork pull requests");
  console.log("\n" + pass + " passed, " + fail + " failed");
  process.exit(fail ? 1 : 0);
}

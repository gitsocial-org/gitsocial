// unit_render_cards.js - shim-rendered units for the card vocabulary:
// the chip row a header produces, the per-extension card dispatch, the reply
// quote block, and the paged and auto-scrolling list wrappers.
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

const marked = GS.memoCard(item("b", { type: "memo", retracted: "true", "origin-platform": "github", "origin-url": "https://example.com/1" }, "Memo"));
const markedChips = chipText(marked);
eq([markedChips[0], markedChips[1]], ["retracted", "↗ github"], "retracted leads the card, then the origin badge");
eq(chipText(findClass(marked, "card-head")[0]), ["retracted"], "the retracted marker rides the head's one chip slot");
eq(chipText(findClass(marked, "card-chips")[0]), ["↗ github"], "and the chip row carries the rest");
const deadComment = GS.timelineCard(Object.assign(item("z", { type: "comment", retracted: "true" }, "Gone"), { _ext: "social" }));
eq(chipText(findClass(deadComment, "meta")[0]), ["retracted"], "a body-only card has no head, so the marker leads its meta row");
eq(findClass(marked, "chip-origin")[0].getAttribute("title"), "https://example.com/1", "the origin chip links its upstream URL in the title");
eq(chipText(GS.memoCard(item("c", { type: "memo", "origin-author-email": "49699333+dependabot[bot]@users.noreply.github.com", "origin-author-name": "dependabot[bot]" }, "Bump"))).indexOf("⚙ bot") !== -1, true, "an automation author gets a bot chip");
eq(chipText(GS.memoCard(item("d", { type: "memo", "origin-author-email": "ada@example.com", "origin-author-name": "Ada" }, "Note"))).indexOf("⚙ bot"), -1, "a human origin author gets no bot chip");

const counted = GS.prCard(item("e", { type: "pull-request", state: "open" }, "Counted"), { approved: 2, changesRequested: 1, comments: 4, reposts: 3, quotes: 1 });
const countedText = chipText(counted);
ok(countedText.indexOf("✓2 ✗1") !== -1, "the review chip tallies approvals and change requests", countedText.join(" | "));
ok(countedText.indexOf("↩ 4") !== -1 && countedText.indexOf("↻ 3") !== -1 && countedText.indexOf("❞ 1") !== -1, "comment, repost and quote counts each get a chip", countedText.join(" | "));

console.log("=== a release row counts its assets where another row links its hash ===");
const metaBits = (node) => textOf(findClass(node, "meta")[0]).split(" · ");
const shipped = GS.releaseCard(item("r", { type: "release", tag: "v1.0", version: "1.0", artifacts: "linux.tar.gz, mac.tar.gz" }, "Ship it"));
eq(metaBits(shipped).slice(0, 1).concat(metaBits(shipped).slice(2)), ["Ada", "2 assets"], "the release row ends on its asset count");
eq(findClass(shipped, "hash").length, 0, "and carries no commit hash");
eq(chipText(shipped).indexOf("2 assets"), -1, "the count is a meta bit, not a head chip");
eq(metaBits(GS.releaseCard(item("s", { type: "release", tag: "v1.1", artifacts: "one.tar.gz" }, "Ship one"))).slice(2), ["1 asset"], "one artifact reads singular");
eq(metaBits(GS.releaseCard(item("t", { type: "release", tag: "v1.2" }, "Ship none"))).length, 2, "a release naming no artifact drops the bit");
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

console.log("=== a comment renders whole, under the quote it answers ===");
const reply = item("f", { type: "comment", original: "#commit:" + H("1") + "@gitmsg/social" }, "First line of the reply\nand the rest",
  [{ ref: "#commit:" + H("1") + "@gitmsg/social", quoted: "> the original post", author: "Bob", email: "bob@example.com", time: "2026-01-02T03:04:05Z" }]);
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
const repost = GS.timelineCard(Object.assign(item("g", { type: "repost", original: "#commit:" + H("1") + "@gitmsg/social" }, "", [{ ref: "#commit:" + H("1") + "@gitmsg/social", quoted: "> the original post", author: "Bob" }]), { _ext: "social" }));
ok(!findClass(repost, "reply-quote")[0]._cls.has("dimmed"), "a repost shows the original undimmed");

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

// rest holds the assertions that need no scheduling, and closes the run.
function rest() {
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

  console.log("=== icons and focus targets ===");
  const goIcon = GS.icon("main.go");
  ok(!!goIcon && goIcon._cls.has("gs-icon"), "a filename resolves to an icon span");
  ok(findTag(goIcon, "svg").length === 1, "the icon span holds one cloned glyph");
  eq(findTag(goIcon, "svg")[0].getAttribute("aria-hidden"), "true", "the glyph is hidden from assistive tech");
  ok(GS.icon("main.go", null, "tree-icon")._cls.has("tree-icon"), "an extra class rides the icon span");
  eq(GS.focusSearchInput(), false, "focusSearchInput reports false with no search box mounted");
  eq(GS.focusTreeSearch(), false, "focusTreeSearch reports false with no tree mounted");
  console.log("\n" + pass + " passed, " + fail + " failed");
  process.exit(fail ? 1 : 0);
}

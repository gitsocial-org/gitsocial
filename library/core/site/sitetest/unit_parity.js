// unit_parity.js - reader-side half of the writer/reader parity invariant: the JS
// reader's subject/header extraction (gs-core.js cleanContent + parseGitmsg,
// the same code metaCommit feeds) must agree with the Go writer's subjectOf /
// extractHeaderLine on the shared pinned fixtures (parity_fixtures.json). The Go
// half (site_parity_test.go) asserts the identical expected values, so the two
// implementations are pinned against one ground truth on the hard cases: a
// gpgsig-bearing commit, a CRLF-line-ending commit, and the empty-subject
// "body starts with GitMsg: " case.
const fs = require("fs");
const path = require("path");
require("./shim.js");
require("../assets/icons.js");
const GS = require("../assets/gs-core.js");
require("../assets/gs-render.js");
const FIX = JSON.parse(fs.readFileSync(path.join(__dirname, "parity_fixtures.json"), "utf8"));
let pass = 0, fail = 0;
// findCls collects the element nodes under node that carry the class.
function findCls(node, cls, out) { out = out || []; if (node && node._cls && node._cls.has(cls)) out.push(node); for (const c of (node && node._children) || []) if (c && c.nodeType === 1) findCls(c, cls, out); return out; }
function eq(a, b, msg) { if (a === b) { pass++; } else { fail++; console.log("FAIL", msg, "got", JSON.stringify(a), "want", JSON.stringify(b)); } }

// readerSubject mirrors the reader's subject derivation: cleanContent strips the
// trailer, the first line goes through subjectText, as itemSubject does.
function readerSubject(message) {
  const content = GS.cleanContent(message).trim();
  if (!content) return "";
  const nl = content.indexOf("\n");
  return GS.subjectText(nl < 0 ? content : content.slice(0, nl));
}

// splitCommitMessage mirrors the loose-object header/message split both the Go
// writer (parseBucketCommit) and the JS reader (parseCommit) perform: the body
// is everything after the first blank line.
function splitCommitMessage(commitText) {
  const i = commitText.indexOf("\n\n");
  return i < 0 ? "" : commitText.slice(i + 2);
}

console.log("=== parity invariant: message-level subject/header parity ===");
for (const c of FIX.messageCases) {
  eq(readerSubject(c.message), c.expectSubject, c.name + ": subject");
  // The reader parses relations from the GitMsg header line; parity means
  // parsing the Go-extracted line yields the same header map as parsing the
  // full message (extractHeaderLine picks that line, verbatim).
  const fromWhole = GS.parseGitmsg(c.message);
  const fromLine = c.expectHeader ? GS.parseGitmsg(c.expectHeader) : null;
  eq(JSON.stringify(fromWhole), JSON.stringify(fromLine), c.name + ": header line parses identically");
}

console.log("=== parity invariant: raw-object split parity (gpgsig / CRLF) ===");
for (const c of FIX.rawObjectCases) {
  const message = splitCommitMessage(c.commitText);
  eq(readerSubject(message), c.expectSubject, c.name + ": subject after header split");
  const fromWhole = GS.parseGitmsg(message);
  const fromLine = c.expectHeader ? GS.parseGitmsg(c.expectHeader) : null;
  eq(JSON.stringify(fromWhole), JSON.stringify(fromLine), c.name + ": header line parses identically");
  // The full parseCommit path (what the reader actually runs) must land the
  // same subject from the raw object body.
  const parsed = GS.parseCommit("0".repeat(40), Buffer.from(c.commitText, "utf8"));
  const nl = parsed.content.indexOf("\n");
  const subj = (nl < 0 ? parsed.content : parsed.content.slice(0, nl)).trim();
  eq(subj, c.expectSubject, c.name + ": parseCommit subject");
}

console.log("=== parity invariant: feedback card verdict + anchor chip ===");
for (const c of FIX.feedbackCards) {
  eq(GS.feedbackVerdict(c.header), c.expectVerdict, c.name + ": verdict");
  eq(GS.feedbackAnchorLabel(c.header), c.expectAnchor, c.name + ": anchor label");
}

console.log("=== parity invariant: the file route a feedback chip opens ===");
for (const c of FIX.feedbackFileRefs) {
  eq(GS.feedbackFileRef(c.header, c.pr), c.expectRef, c.name + ": file ref");
}

console.log("=== parity invariant: release head subject + version chip ===");
for (const c of FIX.releaseHeads) {
  const subject = GS.headSubject(c.header, "release", c.firstLine);
  eq(subject, c.expectSubject, c.name + ": subject");
  eq(GS.releaseVersionChip(c.header.version || "", subject), c.expectVersionChip, c.name + ": version chip");
}

console.log("=== parity invariant: detail head subject + chips ===");
const chipList = (chips) => chips.map((c) => (c.class || "") + "|" + c.label).join(",");
for (const c of FIX.detailHeads) {
  const subject = GS.headSubject(c.header, c.ext, c.firstLine);
  eq(subject, c.expectSubject, c.name + ": subject");
  eq(chipList(GS.headChips(c.header, c.ext, subject, c.retracted === true)), chipList(c.expectChips), c.name + ": head chips");
  const link = GS.headTagLink(c.header, subject, c.retracted === true);
  eq(link ? link.getAttribute("href") : "", c.expectHref || "", c.name + ": head link");
}

console.log("=== parity invariant: list and activity row head subject + chips ===");
for (const c of FIX.rowHeads) {
  const subject = GS.headSubject(c.header, c.ext, c.firstLine);
  eq(subject, c.expectSubject, c.name + ": subject");
  eq(chipList(GS.rowChips(c.header, c.ext, subject, c.retracted === true)), chipList(c.expectChips), c.name + ": row chips");
}

console.log("=== parity invariant: the asset count a release row carries ===");
for (const c of FIX.releaseRows) {
  eq(GS.releaseAssetLabel(c.artifacts), c.expectLabel, c.name + ": asset label");
}

console.log("=== parity invariant: the blocks a release body splits into ===");
// blockShape names a block the way site_parity_test.go compares it: its rows, else its lines.
const blockShape = (b) => (b.notes ? "notes:" + b.notes.map((n) => n.hash + " " + n.text).join("|") : "lines:" + b.lines.join("\n"));
for (const c of FIX.releaseNotes) {
  const got = GS.itemBodyBlocks(c.body, true).map(blockShape).join(" ~ ");
  const want = c.expect.map((b) => (b.notes ? blockShape(b) : "lines:" + b.lines.join("\n"))).join(" ~ ");
  eq(got, want, c.name + ": blocks");
}

console.log("=== parity invariant: the asset rows a release carries ===");
// rowShape names an asset row the way site_parity_test.go compares it.
const rowShape = (r) => r.name + "|" + (r.href || "") + "|" + (r.chip || "");
for (const c of FIX.releaseAssets) {
  const a = GS.releaseAssets(c.header);
  const extra = [];
  if (a.checksums) extra.push({ name: a.checksums.name, href: a.checksums.href, chip: "checksums" });
  if (a.sbom) extra.push({ name: a.sbom.name, href: a.sbom.href, chip: "SBOM" });
  eq(a.artifacts.map(rowShape).join(","), c.expectArtifacts.map(rowShape).join(","), c.name + ": artifact rows");
  eq(extra.map(rowShape).join(","), c.expectExtra.map(rowShape).join(","), c.name + ": checksum and SBOM rows");
  eq(a.signedBy, c.expectSignedBy, c.name + ": signing key");
}

console.log("=== parity invariant: the one card shape ===");
// slots names a rendered node's child classes in order, the form site_parity_test.go compares.
const slots = (node) => ((node && node._children) || []).filter((c) => c && c.nodeType === 1)
  .map((c) => String(c.className || "").trim().split(/\s+/)[0]).join(",");
for (const c of FIX.cardSkeleton.cases) {
  const it = {
    commit: { hash: "a".repeat(40), short: "a".repeat(12), authorName: "Ada", authorEmail: "ada@example.com", authorTime: 1750000000, refs: [] },
    header: c.header, content: c.firstLine, author: "Ada", effectiveTime: 1750000000,
    _ext: c.ext, _branch: "gitmsg/" + c.ext,
  };
  const row = GS.timelineCard(it);
  const head = (row._children || []).find((n) => n && n._cls && n._cls.has("card-head"));
  if (!c.expectHead.length) {
    eq(!!head, false, c.name + ": a body-only row carries no head");
    // The app clamps the body it shows whole, so only the meta row's lead is shared with the page's row.
    eq(slots(row).split(",")[0], FIX.cardSkeleton.bodyOnlyParts[0], c.name + ": the meta row leads");
    const meta = (row._children || []).find((n) => n && n._cls && n._cls.has("meta"));
    eq(slots(meta).split(",").slice(0, c.expectLead.length).join(","), c.expectLead.join(","), c.name + ": what leads the meta row");
    continue;
  }
  eq(slots(head), c.expectHead.join(","), c.name + ": head slots");
  eq(slots(row).split(",").slice(0, 2).join(","), FIX.cardSkeleton.parts.slice(0, 2).join(","), c.name + ": the head, then the meta row");
}
// The chip row is the app's own third part, so the full order shows on a card that fills it.
const chipped = GS.prCard({
  commit: { hash: "b".repeat(40), short: "b".repeat(12), authorName: "Ada", authorEmail: "ada@example.com", authorTime: 1750000000, refs: [] },
  header: { type: "pull-request", state: "open", head: "feature", base: "trunk", labels: "kind/bug" },
  content: "Rework the walk", author: "Ada", effectiveTime: 1750000000,
});
eq(slots(chipped), FIX.cardSkeleton.parts.join(","), "a card that fills every part keeps the fixture's order");

console.log("=== parity invariant: an adopted copy's row and a cross-repository edit ===");
{
  const A = FIX.adopted;
  const sha = "a".repeat(40);
  const header = A.message.split("\n").find((l) => l.startsWith("GitMsg: "));
  const fromIndex = GS.metaCommit({ sha, author: A.committer, email: A.committerEmail, ts: 1750000000, header, subject: "Crash on startup", ...A.expectEntry });
  const fromBody = GS.indexCommit({ sha, author: A.committer, email: A.committerEmail, ts: 1750000000, message: A.message });
  for (const [name, c] of [["an index entry", fromIndex], ["a full message", fromBody]]) {
    eq(GS.effectiveTime(c, c.gitmsg), A.expectTime, name + ": the row shows the original time");
    const item = { commit: c, header: c.gitmsg, content: "Crash on startup", author: GS.effectiveAuthor(c, c.gitmsg), effectiveTime: GS.effectiveTime(c, c.gitmsg) };
    const row = GS.metaRow(item, "gitmsg/pm");
    const bits = (row._children || []).filter((n) => n && n.nodeType === 1);
    eq(bits.map((n) => String(n.className || "").split(/\s+/)[0]).join(","), A.expectBits.join(","), name + ": the row's bits");
    const text = (n) => n.textContent;
    eq(text(bits[0]), A.expectAuthor, name + ": the row names the original author");
    eq(text(bits[bits.length - 1]), A.expectAdopted, name + ": the adopted bit names the fork");
    const adopted = bits[bits.length - 1];
    eq(adopted.getAttribute("href"), A.expectHref, name + ": the adopted bit links the fork");
  }
  const plain = GS.indexCommit({ sha: "c".repeat(40), author: A.committer, email: A.committerEmail, ts: A.plainIssueTime, message: "A plain issue\n\nGitMsg: ext=\"pm\"; type=\"issue\"; v=\"0.1.0\"" });
  eq(GS.resolveItems([fromBody, plain]).map((it) => it.commit.hash[0]).join(","), "c,a", "a list sorts the copy by its original time, below a plain issue created after it");
  const timeOf = (message) => { const c = GS.indexCommit({ sha, author: A.committer, email: A.committerEmail, ts: 1750000000, message }); return GS.effectiveTime(c, c.gitmsg); };
  eq(timeOf(A.messageOriginTime), A.expectOriginTime, "origin-time wins over the adopted time");
  eq(timeOf(A.messageBadTime), 1750000000, "a snapshot time that does not parse falls back to the git time");
  const bare = GS.indexCommit({ sha, author: A.committer, email: A.committerEmail, ts: 1750000000, message: A.messageNoEmail });
  eq(GS.effectiveAuthor(bare, bare.gitmsg) + " " + GS.effectiveAuthorEmail(bare, bare.gitmsg), A.committer + " " + A.committerEmail, "a GitMsg-Ref with no email is no reference, so the row falls back to the committer");
  const edit = GS.indexCommit({ sha: "b".repeat(40), author: A.committer, email: A.committerEmail, ts: 1750000001, message: A.crossRepoEdit });
  eq(GS.resolveItems([edit]).length, 0, "a cross-repository edit is no item");
}

console.log("=== parity invariant: the home section's row count ===");
eq(GS.HOME_ROWS, FIX.homeRows.limit, "the home section shows the fixture's row count");

console.log("=== parity invariant: the sidebar counts are the rows the app's lists open on ===");
{
  const live = FIX.itemCounts.items.filter((c) => !c.retracted).map((c, i) => {
    const header = {};
    if (c.type) header.type = c.type;
    if (c.state) header.state = c.state;
    if (c.origin) header["origin-url"] = c.origin;
    const hash = String(i).padStart(40, "0");
    return { ext: c.ext, commit: { hash, short: hash.slice(0, 12), authorName: "Ada", authorTime: c.ts, refs: [] }, header, content: c.subject, author: "Ada", effectiveTime: c.ts };
  });
  const of = (ext) => live.filter((it) => it.ext === ext);
  const cards = (nodes) => nodes.reduce((n, node) => n + findCls(node, "card").length, 0);
  const got = {
    issues: cards(GS.issuesBody(of("pm"), null)),
    milestones: cards(GS.milestonesBody(of("pm"))),
    sprints: cards(GS.sprintsBody(of("pm"))),
    prs: cards(GS.filteredListView(of("review").filter((it) => it.header.type === "pull-request"), (it) => GS.prCard(it), "prs", GS.PR_STATES, "")),
    releases: of("release").filter((it) => it.header.type === "release").length,
    memos: of("memo").length,
  };
  eq(JSON.stringify(got), JSON.stringify(FIX.itemCounts.expect), "the manifest's counts are the rows each list opens on");
}

console.log("=== parity invariant: which files render as prose, and the MDX strip ===");
for (const c of FIX.markdownPaths) {
  eq(GS.isMarkdownPath(c.path), c.expectProse, c.name + ": renders as prose");
}
for (const c of FIX.mdxStrip) {
  eq(GS.stripMDX(c.source), c.expect, c.name);
}
for (const c of FIX.frontMatter) {
  eq(GS.stripFrontMatter(c.source), c.expect, c.name);
}

console.log("=== parity invariant: the ::: admonition component ===");
for (const c of FIX.admonitions) {
  const root = GS.renderMarkdown(c.source);
  const boxes = findCls(root, "admonition");
  if (!c.expectClass) {
    eq(boxes.length, 0, c.name + ": a degraded opener builds no admonition");
  } else {
    eq(boxes.length, 1, c.name + ": one admonition");
    eq(boxes[0] && boxes[0].className, c.expectClass, c.name + ": the block's classes");
    const title = boxes[0] && boxes[0].children[0];
    eq(title && title.tagName.toLowerCase() + "." + title.className, "p.admonition-title", c.name + ": the title line leads");
    eq(title && title.textContent, c.expectTitle, c.name + ": the title text");
  }
  eq(root.textContent.includes(c.expectBodyHas), true, c.name + ": the body text stays visible");
}

console.log("=== parity invariant: the Files changed section of a pull request ===");
{
  const P = FIX.prDiff;
  eq(GS.DIFF_FILE_CAP, P.fileCap, "the file cap is the fixture's");
  for (const [status, letter] of Object.entries(P.statusLetters)) eq(GS.diffStatusLabel(status), letter, status + ": status letter");
  for (const c of P.hunkHeads) eq(GS.hunkHeadText(c), c.expect, c.name + ": hunk head");
  eq(JSON.stringify(GS.PR_DIFF_NOTICE), JSON.stringify(P.notices), "the notice wording is the fixture's");
  const notice = GS.prDiffNotice(P.notices.noTips);
  eq(notice.className, "diff-section", "the notice variant is a diff section");
  eq(notice._children.map((c) => c.className).join(","), P.noticeParts.join(","), "the notice variant's parts");
  eq(findCls(notice, "subject").map((n) => n.textContent).join(","), P.head, "the notice variant keeps the head");
  // Six entries keep the section from expanding its files, which would fetch.
  const entries = Array.from({ length: 6 }, (_, i) => ({ path: "f" + i + ".txt", status: i ? "modified" : "added" }));
  const section = GS.diffSection(null, entries, P.head, []);
  eq(findCls(section, "subject").map((n) => n.textContent).join(","), P.head + " (6)", "the head counts the files");
  const row = findCls(section, "diff-file-head")[0];
  const shared = (row._children || []).map((c) => String(c.className || "").split(/\s+/).find((t) => P.rowSlots.includes(t))).filter(Boolean);
  eq(shared.join(","), P.rowSlots.join(","), "a file row carries the shared slots in order");
  eq(findCls(row, "diff-status")[0].className + "|" + findCls(row, "diff-status")[0].textContent, "diff-status s-added|A", "the status slot carries its class and letter");
  for (const c of P.generated) eq(GS.isGeneratedFile(c.path, c.firstLine || ""), c.expect, c.name + ": generated");
  const gen = GS.diffSection(null, [{ path: "go.sum", status: "modified" }, { path: "main.go", status: "modified" }], P.head, []);
  const labels = findCls(gen, "diff-file-head").map((h) => findCls(h, "chip").map((n) => n.textContent).join(","));
  eq(labels.join("|"), P.generatedLabel + "|", "a generated file carries the label and a plain file none");
}

console.log("=== parity invariant: the name an unconfigured site takes ===");
for (const c of FIX.defaultTitles) {
  eq(GS.repoTitle(c.base), c.expectTitle, c.name + ": default title");
}

console.log("=== parity invariant: meta row author label ===");
for (const c of FIX.metaRow.authors) {
  eq(GS.authorLabel(c.author, c.email), c.expectLabel, c.name + ": label");
  const bit = GS.authorEl(c.author, c.email);
  eq(bit.tagName + "|" + bit.className + "|" + bit.textContent + "|" + (bit.getAttribute("title") || "") + "|" + (bit.getAttribute("href") || ""),
    (c.expectHref ? "A" : "SPAN") + "|author|" + c.expectLabel + "|" + c.expectTitle + "|" + c.expectHref, c.name + ": bit");
}

const empties = Object.entries(GS.LIST_EMPTY).sort().map((e) => e.join("=")).join(",");
const fixtureEmpties = Object.entries(FIX.listEmpty).sort().map((e) => e.join("=")).join(",");
eq(empties, fixtureEmpties, "LIST_EMPTY matches the page layer's empty sentences");
const headings = Object.entries(GS.LIST_HEADINGS).sort().map((e) => e.join("=")).join(",");
const fixtureHeadings = Object.entries(FIX.listHeadings).sort().map((e) => e.join("=")).join(",");
eq(headings, fixtureHeadings, "LIST_HEADINGS matches the page layer's nav labels");

console.log("=== parity invariant: the row text of a body-only item ===");
for (const c of FIX.rowText) {
  eq(GS.subjectBody(GS.cleanContent(c.message))[0], c.expect, c.name + ": row text");
}

console.log("=== parity invariant: the refs and mentions free text carries ===");
for (const c of FIX.refTokens) {
  const refs = findCls(GS.renderMarkdown(c.text), "hash").filter((a) => a.tagName === "A").map((a) => a.getAttribute("href"));
  eq(JSON.stringify(refs), JSON.stringify(c.expectRefs), c.name + ": refs");
  const mentions = Array.from(c.text.matchAll(GS.MENTION_RE), (m) => m[2]);
  eq(JSON.stringify(mentions), JSON.stringify(c.expectMentions), c.name + ": mentions");
}

console.log("=== parity invariant: the ref and URL anchors of a page body ===");
// anchors lists the A nodes under node in document order.
function anchors(node, out) { out = out || []; for (const c of (node && node._children) || []) if (c && c.nodeType === 1) { if (c.tagName === "A") out.push(c); anchors(c, out); } return out; }
for (const c of FIX.pageBodyRefs) {
  const all = anchors(GS.renderMarkdown(c.text));
  const refs = all.filter((a) => a._cls && a._cls.has("hash"));
  eq(JSON.stringify(refs.map((a) => a.getAttribute("href"))), JSON.stringify(c.expectRefs), c.name + ": refs");
  eq(refs.every((a) => a.textContent === a.getAttribute("href")), true, c.name + ": a ref anchor reads as its ref");
  const urls = all.filter((a) => !(a._cls && a._cls.has("hash")));
  eq(JSON.stringify(urls.map((a) => a.getAttribute("href"))), JSON.stringify(c.expectURLs), c.name + ": urls");
  eq(urls.every((a) => a.textContent === a.getAttribute("href")), true, c.name + ": a url anchor reads as its url");
}

console.log("=== parity invariant: the definition rows of an item page ===");
// rowBits names a trailer value the way site_parity_test.go compares it: each anchor with its href and rel, each text node as text, the separators dropped.
function rowBits(value) {
  const nodes = typeof value === "string" ? [{ nodeType: 3, nodeValue: value }] : value.tagName === "A" ? [value] : value._children;
  return nodes.filter((n) => !(n.nodeType === 3 && n.nodeValue === ", "))
    .map((n) => (n.nodeType === 3 ? { text: n.nodeValue } : { text: n.textContent, href: n.getAttribute("href") || "", rel: n.getAttribute("rel") || "" }));
}
const metaRowShape = (rows) => rows.map((r) => r.key + "=" + r.bits.map((b) => b.text + "@" + (b.href || "") + (b.rel ? "[" + b.rel + "]" : "")).join("|")).join(";");
for (const c of FIX.itemPageMeta) {
  const rows = GS.trailerRows(c.header, c.ext === "release" ? GS.RELEASE_DETAIL_SKIP : [], null);
  eq(metaRowShape(rows.map((r) => ({ key: r.key, bits: rowBits(r.value) }))), metaRowShape(c.expectRows), c.name + ": rows");
}

console.log("=== parity invariant: tag display order ===");
(async () => {
  for (const c of FIX.tagOrder) {
    const tags = c.tags.map((t) => ({ name: t.name, sha: "" })).sort(GS.compareTagsDesc);
    const dates = new Map(c.tags.map((t) => [t.name, { time: t.time }]));
    eq(JSON.stringify((await GS.orderTagTies(null, tags, dates)).map((t) => t.name)), JSON.stringify(c.expect), c.name + ": tag order");
  }
  console.log("\n" + pass + " passed, " + fail + " failed");
  process.exit(fail ? 1 : 0);
})();

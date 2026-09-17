// unit_parity.js - DOM-free reader-side half of the writer/reader parity invariant: the JS
// reader's subject/header extraction (gs-core.js cleanContent + parseGitmsg,
// the same code metaCommit feeds) must agree with the Go writer's subjectOf /
// extractHeaderLine on the shared pinned fixtures (parity_fixtures.json). The Go
// half (site_parity_test.go) asserts the identical expected values, so the two
// implementations are pinned against one ground truth on the hard cases: a
// gpgsig-bearing commit, a CRLF-line-ending commit, and the empty-subject
// "body starts with GitMsg: " case.
const fs = require("fs");
const path = require("path");
const GS = require("../assets/gs-core.js");
const FIX = JSON.parse(fs.readFileSync(path.join(__dirname, "parity_fixtures.json"), "utf8"));
let pass = 0, fail = 0;
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

console.log("=== parity invariant: what the front page says about the root entries it hides ===");
for (const c of FIX.frontFiles.cases) {
  const cut = GS.homeFilesTruncation(c.total, FIX.frontFiles.limit);
  eq(cut.notice, c.expectNotice, c.name + ": notice");
  eq(cut.label, c.expectLabel, c.name + ": control label");
}

console.log("=== parity invariant: which files render as prose, and the MDX strip ===");
for (const c of FIX.markdownPaths) {
  eq(GS.isMarkdownPath(c.path), c.expectProse, c.name + ": renders as prose");
}
for (const c of FIX.mdxStrip) {
  eq(GS.stripMDX(c.source), c.expect, c.name);
}

console.log("=== parity invariant: the name an unconfigured site takes ===");
for (const c of FIX.defaultTitles) {
  eq(GS.repoTitle(c.base), c.expectTitle, c.name + ": default title");
}

console.log("=== parity invariant: meta row author label ===");
for (const c of FIX.metaRow.authors) {
  eq(GS.authorLabel(c.author, c.email), c.expectLabel, c.name + ": label");
}

require("../assets/gs-render.js");
const empties = Object.entries(GS.LIST_EMPTY).sort().map((e) => e.join("=")).join(",");
const fixtureEmpties = Object.entries(FIX.listEmpty).sort().map((e) => e.join("=")).join(",");
eq(empties, fixtureEmpties, "LIST_EMPTY matches the page layer's empty sentences");
const headings = Object.entries(GS.LIST_HEADINGS).sort().map((e) => e.join("=")).join(",");
const fixtureHeadings = Object.entries(FIX.listHeadings).sort().map((e) => e.join("=")).join(",");
eq(headings, fixtureHeadings, "LIST_HEADINGS matches the page layer's nav labels");

console.log("\n" + pass + " passed, " + fail + " failed");
process.exit(fail ? 1 : 0);

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
  const row = GS.homeActivityRow(it);
  const head = (row._children || []).find((n) => n && n._cls && n._cls.has("card-head"));
  if (!c.expectHead.length) {
    eq(!!head, false, c.name + ": a body-only row carries no head");
    eq(slots(row), FIX.cardSkeleton.bodyOnlyParts.join(","), c.name + ": the meta row, then the body");
    const meta = (row._children || []).find((n) => n && n._cls && n._cls.has("meta"));
    eq(slots(meta).split(",").slice(0, c.expectLead.length).join(","), c.expectLead.join(","), c.name + ": what leads the meta row");
    continue;
  }
  eq(slots(head), c.expectHead.join(","), c.name + ": head slots");
  eq(slots(row), FIX.cardSkeleton.parts.slice(0, 2).join(","), c.name + ": the head, then the meta row");
}
// The chip row is the app's own third part, so the full order shows on a card that fills it.
const chipped = GS.prCard({
  commit: { hash: "b".repeat(40), short: "b".repeat(12), authorName: "Ada", authorEmail: "ada@example.com", authorTime: 1750000000, refs: [] },
  header: { type: "pull-request", state: "open", head: "feature", base: "trunk", labels: "kind/bug" },
  content: "Rework the walk", author: "Ada", effectiveTime: 1750000000,
});
eq(slots(chipped), FIX.cardSkeleton.parts.join(","), "a card that fills every part keeps the fixture's order");

console.log("=== parity invariant: what the front page offers for the root entries it hides ===");
for (const c of FIX.frontFiles.cases) {
  eq(GS.homeFilesMoreLabel(c.total, FIX.frontFiles.limit), c.expectLabel, c.name + ": control label");
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

const empties = Object.entries(GS.LIST_EMPTY).sort().map((e) => e.join("=")).join(",");
const fixtureEmpties = Object.entries(FIX.listEmpty).sort().map((e) => e.join("=")).join(",");
eq(empties, fixtureEmpties, "LIST_EMPTY matches the page layer's empty sentences");
const headings = Object.entries(GS.LIST_HEADINGS).sort().map((e) => e.join("=")).join(",");
const fixtureHeadings = Object.entries(FIX.listHeadings).sort().map((e) => e.join("=")).join(",");
eq(headings, fixtureHeadings, "LIST_HEADINGS matches the page layer's nav labels");

console.log("\n" + pass + " passed, " + fail + " failed");
process.exit(fail ? 1 : 0);

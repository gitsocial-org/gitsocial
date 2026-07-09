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
const GS = require("../site/gs-core.js");
const FIX = JSON.parse(fs.readFileSync(path.join(__dirname, "parity_fixtures.json"), "utf8"));
let pass = 0, fail = 0;
function eq(a, b, msg) { if (a === b) { pass++; } else { fail++; console.log("FAIL", msg, "got", JSON.stringify(a), "want", JSON.stringify(b)); } }

// readerSubject mirrors the reader's subject derivation (itemSubject /
// subjectBody): cleanContent strips the trailer, the first line trimmed is the
// subject.
function readerSubject(message) {
  const content = GS.cleanContent(message).trim();
  if (!content) return "";
  const nl = content.indexOf("\n");
  return (nl < 0 ? content : content.slice(0, nl)).trim();
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
  // whole message (extractHeaderLine picks exactly that line, verbatim).
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

console.log("\n" + pass + " passed, " + fail + " failed");
process.exit(fail ? 1 : 0);

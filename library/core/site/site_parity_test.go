// site_parity_test.go - writer/reader parity invariants: subject/header, the feedback card and the release head.
// The Go writer's subjectOf / extractHeaderLine (site_items.go) and the JS
// reader's cleanContent / parseGitmsg (site/gs-core.js) must derive the same
// subject and GitMsg header line from a commit message. This test pins the Go
// half against the SAME shared fixtures (sitetest/parity_fixtures.json) the JS
// unit half (sitetest/unit_parity.js) asserts, so the two implementations are
// checked against one ground truth on the hard cases: a gpgsig-bearing commit,
// a CRLF-line-ending commit, and the empty-subject "body starts with GitMsg: "
// case.

package site

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gitsocial-org/gitsocial/library/core/protocol"
)

// parityMessageCase pins the subject/header expected from a raw commit message.
type parityMessageCase struct {
	Name          string `json:"name"`
	Message       string `json:"message"`
	ExpectSubject string `json:"expectSubject"`
	ExpectHeader  string `json:"expectHeader"`
}

// parityRawObjectCase pins the subject/header expected from a full loose-object
// commit text (header/message split included).
type parityRawObjectCase struct {
	Name          string `json:"name"`
	CommitText    string `json:"commitText"`
	ExpectSubject string `json:"expectSubject"`
	ExpectHeader  string `json:"expectHeader"`
}

// parityFeedbackCase pins the verdict and anchor label a feedback card's header yields.
type parityFeedbackCase struct {
	Name          string            `json:"name"`
	Header        map[string]string `json:"header"`
	ExpectVerdict string            `json:"expectVerdict"`
	ExpectAnchor  string            `json:"expectAnchor"`
}

// parityReleaseHeadCase pins the subject and version chip a release head yields.
type parityReleaseHeadCase struct {
	Name              string            `json:"name"`
	Header            map[string]string `json:"header"`
	FirstLine         string            `json:"firstLine"`
	ExpectSubject     string            `json:"expectSubject"`
	ExpectVersionChip string            `json:"expectVersionChip"`
}

// parityFixtures is the shared fixture file shape.
type parityFixtures struct {
	MessageCases   []parityMessageCase     `json:"messageCases"`
	RawObjectCases []parityRawObjectCase   `json:"rawObjectCases"`
	FeedbackCards  []parityFeedbackCase    `json:"feedbackCards"`
	ReleaseHeads   []parityReleaseHeadCase `json:"releaseHeads"`
	ListHeadings   map[string]string       `json:"listHeadings"`
}

// loadParityFixtures reads the shared JSON fixtures the JS half also consumes.
func loadParityFixtures(t *testing.T) parityFixtures {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("sitetest", "parity_fixtures.json"))
	if err != nil {
		t.Fatalf("read parity fixtures: %v", err)
	}
	var f parityFixtures
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatalf("parse parity fixtures: %v", err)
	}
	return f
}

// TestParitySubjectHeader asserts subjectOf / extractHeaderLine match the pinned
// expected values from a raw commit message.
func TestParitySubjectHeader(t *testing.T) {
	f := loadParityFixtures(t)
	if len(f.MessageCases) == 0 {
		t.Fatal("no message cases in parity fixtures")
	}
	for _, c := range f.MessageCases {
		t.Run(c.Name, func(t *testing.T) {
			if got := subjectOf(c.Message); got != c.ExpectSubject {
				t.Errorf("subjectOf = %q, want %q", got, c.ExpectSubject)
			}
			if got := extractHeaderLine(c.Message); got != c.ExpectHeader {
				t.Errorf("extractHeaderLine = %q, want %q", got, c.ExpectHeader)
			}
		})
	}
}

// TestParityListHeadings pins every list page's heading to the fixture the app's
// LIST_HEADINGS is checked against (unit_parity.js); the app heads two more routes.
func TestParityListHeadings(t *testing.T) {
	f := loadParityFixtures(t)
	lists := append(append([]sitePageList(nil), sitePageLists...), siteCommitsList)
	for _, list := range lists {
		tab := strings.TrimPrefix(list.Route, "/")
		if want, ok := f.ListHeadings[tab]; !ok || want != list.NavLabel {
			t.Errorf("%s: heading %q, fixture %q", tab, list.NavLabel, want)
		}
	}
}

// TestParityFeedbackCard asserts the feedback card's verdict class and anchor
// chip label match the fixture the app's own half (unit_parity.js) asserts.
func TestParityFeedbackCard(t *testing.T) {
	f := loadParityFixtures(t)
	if len(f.FeedbackCards) == 0 {
		t.Fatal("no feedback cases in parity fixtures")
	}
	for _, c := range f.FeedbackCards {
		t.Run(c.Name, func(t *testing.T) {
			msg := &sitePageMsg{Ext: "review", Header: &protocol.Header{Ext: "review", Fields: c.Header}}
			item := &sitePageItem{Msg: msg, Resolved: msg}
			if got := sitePageFeedbackVerdict(item); got != c.ExpectVerdict {
				t.Errorf("verdict = %q, want %q", got, c.ExpectVerdict)
			}
			if got := sitePageFeedbackAnchor(item); got != c.ExpectAnchor {
				t.Errorf("anchor = %q, want %q", got, c.ExpectAnchor)
			}
		})
	}
}

// TestParityFeedbackCardVariant asserts a feedback reply renders as the card
// variant both renderers build, with the verdict on the card and on a chip.
func TestParityFeedbackCardVariant(t *testing.T) {
	fields := map[string]string{"type": "feedback", "review-state": "changes-requested", "file": "notes.txt", "new-line": "2"}
	msg := &sitePageMsg{Ext: "review", Header: &protocol.Header{Ext: "review", Fields: fields}}
	reply := buildSiteReply(&sitePageItem{Msg: msg, Resolved: msg})
	if reply.Variant != "feedback verdict-changes-requested" {
		t.Errorf("variant = %q, want %q", reply.Variant, "feedback verdict-changes-requested")
	}
	want := []sitePageChip{{Class: "verdict-changes-requested", Label: "changes requested"}, {Label: "notes.txt:2"}}
	if len(reply.Chips) != len(want) {
		t.Fatalf("chips = %v, want %v", reply.Chips, want)
	}
	for i := range want {
		if reply.Chips[i] != want[i] {
			t.Errorf("chip %d = %v, want %v", i, reply.Chips[i], want[i])
		}
	}
}

// TestParityReleaseHead asserts a release head takes the tag as its subject and
// the version as one chip, against the fixture the app's half (unit_parity.js)
// asserts, so no version string renders twice on either renderer's head.
func TestParityReleaseHead(t *testing.T) {
	f := loadParityFixtures(t)
	if len(f.ReleaseHeads) == 0 {
		t.Fatal("no release head cases in parity fixtures")
	}
	for _, c := range f.ReleaseHeads {
		t.Run(c.Name, func(t *testing.T) {
			msg := &sitePageMsg{Ext: "release", Header: &protocol.Header{Ext: "release", Fields: c.Header}}
			it := &sitePageItem{Msg: msg, Resolved: msg}
			subject := siteHeadSubject(pageItemType(it), pageItemField(it, "tag"), pageItemField(it, "version"), c.FirstLine)
			if subject != c.ExpectSubject {
				t.Errorf("subject = %q, want %q", subject, c.ExpectSubject)
			}
			chips := siteReleaseVersionChips(it, subject)
			var got string
			if len(chips) == 1 {
				got = chips[0].Label
			} else if len(chips) > 1 {
				t.Fatalf("chips = %v, want at most one", chips)
			}
			if got != c.ExpectVersionChip {
				t.Errorf("version chip = %q, want %q", got, c.ExpectVersionChip)
			}
			if got != "" && strings.Contains(subject, c.Header["version"]) {
				t.Errorf("subject %q already names version %q, so the chip %q repeats it", subject, c.Header["version"], got)
			}
		})
	}
}

// TestParityRawObjectSplit asserts parseBucketCommit's header/message split
// (gpgsig blocks, CRLF) yields the same pinned subject and header line the JS
// reader's parseCommit derives.
func TestParityRawObjectSplit(t *testing.T) {
	f := loadParityFixtures(t)
	if len(f.RawObjectCases) == 0 {
		t.Fatal("no raw-object cases in parity fixtures")
	}
	for _, c := range f.RawObjectCases {
		t.Run(c.Name, func(t *testing.T) {
			// parseBucketCommit takes the object body (everything after the
			// "commit <size>\0" loose-object header); the fixture stores that
			// body verbatim as commitText.
			bc, err := parseBucketCommit("0000000000000000000000000000000000000000", []byte(c.CommitText))
			if err != nil {
				t.Fatalf("parseBucketCommit: %v", err)
			}
			if got := subjectOf(bc.item.Message); got != c.ExpectSubject {
				t.Errorf("subjectOf(parsed message) = %q, want %q", got, c.ExpectSubject)
			}
			if bc.item.Header != c.ExpectHeader {
				t.Errorf("parsed header line = %q, want %q", bc.item.Header, c.ExpectHeader)
			}
			if got := extractHeaderLine(bc.item.Message); got != c.ExpectHeader {
				t.Errorf("extractHeaderLine(parsed message) = %q, want %q", got, c.ExpectHeader)
			}
		})
	}
}

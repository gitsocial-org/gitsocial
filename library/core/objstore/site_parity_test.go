// site_parity_test.go - writer/reader subject/header parity invariant.
// The Go writer's subjectOf / extractHeaderLine (site_items.go) and the JS
// reader's cleanContent / parseGitmsg (site/gs-core.js) must derive the same
// subject and GitMsg header line from a commit message. This test pins the Go
// half against the SAME shared fixtures (sitetest/parity_fixtures.json) the JS
// unit half (sitetest/unit_parity.js) asserts, so the two implementations are
// checked against one ground truth on the hard cases: a gpgsig-bearing commit,
// a CRLF-line-ending commit, and the empty-subject "body starts with GitMsg: "
// case.

package objstore

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
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

// parityFixtures is the shared fixture file shape.
type parityFixtures struct {
	MessageCases   []parityMessageCase   `json:"messageCases"`
	RawObjectCases []parityRawObjectCase `json:"rawObjectCases"`
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

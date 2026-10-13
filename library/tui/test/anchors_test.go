// anchors_test.go - The anchors a detail card marks are its links, in order
package test

import (
	"maps"
	"testing"

	"github.com/gitsocial-org/gitsocial/library/extensions/pm"
	"github.com/gitsocial-org/gitsocial/library/extensions/review"
	"github.com/gitsocial-org/gitsocial/library/tui/tuicore"
)

// sectionLister is implemented by the detail views that expose their section list.
type sectionLister interface {
	SectionList() *tuicore.SectionList
}

// assertAnchorsMatchLinks renders the hero card of the current detail view and compares each marked zone to the link at the same index.
func assertAnchorsMatchLinks(t *testing.T, h *Harness) {
	t.Helper()
	view, ok := h.CurrentView().(sectionLister)
	if !ok {
		t.Fatalf("current view %T does not expose SectionList", h.CurrentView())
	}
	zones, links := view.SectionList().ItemAnchors(0, 100)
	if len(zones) == 0 {
		t.Fatal("the hero card marked no anchor")
	}
	if len(zones) != len(links) {
		t.Fatalf("%d anchors marked, %d links listed: %+v", len(zones), len(links), links)
	}
	for i := range zones {
		if zones[i].Location.Path != links[i].Location.Path || !maps.Equal(zones[i].Location.Params, links[i].Location.Params) {
			t.Errorf("anchor %d is %+v, link %d is %q at %+v", i, zones[i].Location, i, links[i].Label, links[i].Location)
		}
	}
}

// TestIssueDetail_anchorsMatchLinks: an adopted issue with a body ref marks the Adopted from row and the ref as links, in order.
func TestIssueDetail_anchorsMatchLinks(t *testing.T) {
	f := SetupFixture(t)
	forked := pm.CreateIssue(f.ForkDir, "Fork issue", "Reads #commit:abc123def456 and https://example.com/doc", pm.CreateIssueOptions{})
	if !forked.Success {
		t.Fatalf("create the fork issue: %s", resultErrMsg(forked.Error))
	}
	adopted := pm.AdoptIssue(f.Workdir, forked.Data.ID)
	if !adopted.Success {
		t.Fatalf("adopt the fork issue: %s", resultErrMsg(adopted.Error))
	}
	h := New(t, f.Workdir, f.CacheDir)
	assertRendersItem(t, h, tuicore.LocPMIssueDetail(adopted.Data.ID), "Adopted from", "Fork issue")
	assertAnchorsMatchLinks(t, h)
}

// TestPRDetail_anchorsMatchLinks: a pull request with a foreign head, a closed issue and a body ref marks each as a link, in order.
func TestPRDetail_anchorsMatchLinks(t *testing.T) {
	f := SetupFixture(t)
	pr := review.CreatePR(f.Workdir, "Foreign head", "Reads #commit:abc123def456 and https://example.com/doc", review.CreatePROptions{
		Base: "main", Head: "https://github.com/carol/repo#branch:main", HeadTip: "0123456789ab", Closes: []string{f.IssueID},
	})
	if !pr.Success {
		t.Fatalf("create the pull request: %s", resultErrMsg(pr.Error))
	}
	h := New(t, f.Workdir, f.CacheDir)
	assertRendersItem(t, h, tuicore.LocReviewPRDetail(pr.Data.ID), "Foreign head", "Closes")
	assertAnchorsMatchLinks(t, h)
}

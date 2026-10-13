// anchors_test.go - The anchors a detail card marks are its links, in order
package test

import (
	"maps"
	"testing"
	"time"

	"github.com/gitsocial-org/gitsocial/library/client"
	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/extensions/memo"
	"github.com/gitsocial-org/gitsocial/library/extensions/pm"
	"github.com/gitsocial-org/gitsocial/library/extensions/release"
	"github.com/gitsocial-org/gitsocial/library/extensions/review"
	"github.com/gitsocial-org/gitsocial/library/extensions/social"
	"github.com/gitsocial-org/gitsocial/library/tui/tuicore"
)

// linkedBody is a body with one ref, one url and one mention, the three token kinds a body links.
const linkedBody = "Reads #commit:abc123def456 and https://example.com/doc with @carol@example.com"

// sectionLister is implemented by the detail views that expose their section list.
type sectionLister interface {
	SectionList() *tuicore.SectionList
}

// anchorLister is implemented by the post view, which renders its own thread.
type anchorLister interface {
	SelectedAnchors() ([]tuicore.CardLinkZone, []tuicore.CardLink)
}

// assertAnchorsMatchLinks renders the hero card of the current detail view and compares each marked zone to the link at the same index.
func assertAnchorsMatchLinks(t *testing.T, h *Harness) {
	t.Helper()
	var zones []tuicore.CardLinkZone
	var links []tuicore.CardLink
	switch view := h.CurrentView().(type) {
	case sectionLister:
		zones, links = view.SectionList().ItemAnchors(0, 100)
	case anchorLister:
		h.Rendered()
		zones, links = view.SelectedAnchors()
	default:
		t.Fatalf("current view %T exposes no anchors", h.CurrentView())
	}
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

// citeWithTrailer commits a code change on main whose Closes trailer names ref, and indexes it.
func citeWithTrailer(t *testing.T, workdir, ref string) {
	t.Helper()
	if _, err := git.CreateCommit(workdir, git.CommitOptions{Message: "Fix it\n\nCloses: " + ref, AllowEmpty: true}); err != nil {
		t.Fatalf("commit the citing change: %v", err)
	}
	if err := client.SyncWorkspace(workdir); err != nil {
		t.Fatalf("sync the workspace: %v", err)
	}
}

// TestIssueDetail_anchorsMatchLinks: an adopted issue with an assignee, a label, a citing commit and a linked body marks each as a link, in order.
func TestIssueDetail_anchorsMatchLinks(t *testing.T) {
	f := SetupFixture(t)
	forked := pm.CreateIssue(f.ForkDir, "Fork issue for @carol@example.com", linkedBody, pm.CreateIssueOptions{
		Assignees: []string{"bob@example.com"}, Labels: []pm.Label{{Scope: "kind", Value: "feature"}},
	})
	if !forked.Success {
		t.Fatalf("create the fork issue: %s", resultErrMsg(forked.Error))
	}
	adopted := pm.AdoptIssue(f.Workdir, forked.Data.ID)
	if !adopted.Success {
		t.Fatalf("adopt the fork issue: %s", resultErrMsg(adopted.Error))
	}
	citeWithTrailer(t, f.Workdir, adopted.Data.ID)
	h := New(t, f.Workdir, f.CacheDir)
	assertRendersItem(t, h, tuicore.LocPMIssueDetail(adopted.Data.ID), "Adopted from", "Fork issue", "Referenced by", "kind/feature")
	assertAnchorsMatchLinks(t, h)
}

// TestPRDetail_anchorsMatchLinks: a pull request with a foreign head, a tip, a dependency, a reviewer, a label, a closed issue and a linked body marks each as a link, in order.
func TestPRDetail_anchorsMatchLinks(t *testing.T) {
	f := SetupFixture(t)
	pr := review.CreatePR(f.Workdir, "Foreign head", linkedBody, review.CreatePROptions{
		Base: "main", Head: "https://github.com/carol/repo#branch:main", HeadTip: "0123456789ab", Closes: []string{f.IssueID},
		DependsOn: []string{f.PRID}, Reviewers: []string{"bob@example.com"}, Labels: []string{"area/ui"},
	})
	if !pr.Success {
		t.Fatalf("create the pull request: %s", resultErrMsg(pr.Error))
	}
	h := New(t, f.Workdir, f.CacheDir)
	assertRendersItem(t, h, tuicore.LocReviewPRDetail(pr.Data.ID), "Foreign head", "Closes", "Depends on", "Reviewers", "area/ui")
	assertAnchorsMatchLinks(t, h)
}

// TestMilestoneDetail_anchorsMatchLinks: a milestone with a label and a linked body marks each as a link, in order.
func TestMilestoneDetail_anchorsMatchLinks(t *testing.T) {
	f := SetupFixture(t)
	ms := pm.CreateMilestone(f.Workdir, "Ship it with @carol@example.com", linkedBody, pm.CreateMilestoneOptions{Labels: []string{"kind/release"}})
	if !ms.Success {
		t.Fatalf("create the milestone: %s", resultErrMsg(ms.Error))
	}
	h := New(t, f.Workdir, f.CacheDir)
	assertRendersItem(t, h, tuicore.LocPMMilestoneDetail(ms.Data.ID), "Ship it", "kind/release")
	assertAnchorsMatchLinks(t, h)
}

// TestSprintDetail_anchorsMatchLinks: a sprint with a label and a linked body marks each as a link, in order.
func TestSprintDetail_anchorsMatchLinks(t *testing.T) {
	f := SetupFixture(t)
	now := time.Now()
	sp := pm.CreateSprint(f.Workdir, "Sprint for @carol@example.com", linkedBody, pm.CreateSprintOptions{Start: now, End: now.Add(7 * 24 * time.Hour), Labels: []string{"team/ui"}})
	if !sp.Success {
		t.Fatalf("create the sprint: %s", resultErrMsg(sp.Error))
	}
	h := New(t, f.Workdir, f.CacheDir)
	assertRendersItem(t, h, tuicore.LocPMSprintDetail(sp.Data.ID), "Sprint for", "team/ui")
	assertAnchorsMatchLinks(t, h)
}

// TestReleaseDetail_anchorsMatchLinks: a release with a tag, a label, an artifact and a linked body marks each as a link, in order.
func TestReleaseDetail_anchorsMatchLinks(t *testing.T) {
	f := SetupFixture(t)
	rel := release.CreateRelease(f.Workdir, "Release by @carol@example.com", linkedBody, release.CreateReleaseOptions{
		Tag: "v3.0.0", Version: "3.0.0", Labels: []string{"channel/stable"}, Artifacts: []string{"app.tar.gz"},
	})
	if !rel.Success {
		t.Fatalf("create the release: %s", resultErrMsg(rel.Error))
	}
	h := New(t, f.Workdir, f.CacheDir)
	assertRendersItem(t, h, tuicore.LocReleaseDetail(rel.Data.ID), "v3.0.0", "channel/stable", "app.tar.gz")
	assertAnchorsMatchLinks(t, h)
}

// TestMemoDetail_anchorsMatchLinks: a memo with a label and a linked body marks each as a link, in order.
func TestMemoDetail_anchorsMatchLinks(t *testing.T) {
	f := SetupFixture(t)
	m := memo.CreateMemo(f.Workdir, "Memo for @carol@example.com", linkedBody, memo.CreateMemoOptions{Tier: memo.TierProject, Labels: []string{"kind/decision"}})
	if !m.Success {
		t.Fatalf("create the memo: %s", resultErrMsg(m.Error))
	}
	h := New(t, f.Workdir, f.CacheDir)
	assertRendersItem(t, h, tuicore.LocMemoDetail(m.Data.ID), "Memo for", "kind/decision")
	assertAnchorsMatchLinks(t, h)
}

// TestPostDetail_anchorsMatchLinks: a post with a linked body marks its author and each body token as a link, in order.
func TestPostDetail_anchorsMatchLinks(t *testing.T) {
	f := SetupFixture(t)
	post := social.CreatePost(f.Workdir, linkedBody, nil)
	if !post.Success {
		t.Fatalf("create the post: %s", resultErrMsg(post.Error))
	}
	h := New(t, f.Workdir, f.CacheDir)
	assertRendersItem(t, h, tuicore.LocDetail(post.Data.ID), "Reads", "@carol@example.com")
	assertAnchorsMatchLinks(t, h)
}

// run_test.go - End-to-end tests for the import pipeline, driven by a fake adapter
package importpkg

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/cache"
	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
	"github.com/gitsocial-org/gitsocial/library/extensions/pm"
	releasepkg "github.com/gitsocial-org/gitsocial/library/extensions/release"
	"github.com/gitsocial-org/gitsocial/library/extensions/review"
	"github.com/gitsocial-org/gitsocial/library/extensions/social"
	"github.com/gitsocial-org/gitsocial/library/internal/testutil"
)

var runRepoTemplate string

// TestMain builds one repo template and one cache for the whole package.
func TestMain(m *testing.M) {
	dir, err := testutil.NewRepoTemplate()
	if err != nil {
		panic(err)
	}
	runRepoTemplate = dir
	cacheDir, err := os.MkdirTemp("", "import-test-cache-*")
	if err != nil {
		panic(err)
	}
	if err := cache.Open(cacheDir); err != nil {
		panic(err)
	}
	code := m.Run()
	cache.Reset()
	os.RemoveAll(cacheDir)
	os.RemoveAll(dir)
	os.Exit(code)
}

// fakeAdapter is a SourceAdapter serving canned plans, so Run needs no platform.
type fakeAdapter struct {
	counts    ItemCounts
	pmPlan    *PMPlan
	relPlan   *ReleasePlan
	revPlan   *ReviewPlan
	socPlan   *SocialPlan
	pmErr     error
	seenSkips map[string]bool
}

// Platform identifies the fake as a GitHub import for mapping keys and origin URLs.
func (a *fakeAdapter) Platform() string { return "github" }

// CountItems returns the canned totals.
func (a *fakeAdapter) CountItems(FetchOptions) (ItemCounts, error) { return a.counts, nil }

// FetchPM records the options Run passed and returns the canned PM plan.
func (a *fakeAdapter) FetchPM(opts FetchOptions) (*PMPlan, error) {
	a.seenSkips = opts.SkipExternalIDs
	if a.pmErr != nil {
		return nil, a.pmErr
	}
	return a.pmPlan, nil
}

// FetchReleases returns the canned release plan.
func (a *fakeAdapter) FetchReleases(FetchOptions) (*ReleasePlan, error) { return a.relPlan, nil }

// FetchReview returns the canned review plan.
func (a *fakeAdapter) FetchReview(FetchOptions) (*ReviewPlan, error) { return a.revPlan, nil }

// FetchSocial returns the canned social plan.
func (a *fakeAdapter) FetchSocial(FetchOptions) (*SocialPlan, error) { return a.socPlan, nil }

var (
	fakeCreatedAt = time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC)
	fakeClosedAt  = time.Date(2024, 7, 1, 9, 30, 0, 0, time.UTC)
)

// newFakeAdapter builds an adapter carrying one item of every importable kind.
func newFakeAdapter() *fakeAdapter {
	return &fakeAdapter{
		counts: ItemCounts{Issues: 2, PRs: 1, Releases: 1, Discussions: 1},
		pmPlan: &PMPlan{
			Milestones: []ImportMilestone{{
				ExternalID: "v1.0", Number: 1, Title: "v1.0", Body: "First milestone",
				State: "open", AuthorName: "Alice", AuthorEmail: "alice@example.com",
				CreatedAt: fakeCreatedAt,
			}},
			Issues: []ImportIssue{
				{
					ExternalID: "1", Number: 1, Title: "First issue", Body: "Body one",
					State: "open", Labels: []string{"bug"}, Assignees: []string{"bob@example.com"},
					MilestoneID: "v1.0", AuthorName: "Alice", AuthorEmail: "alice@example.com",
					CreatedAt: fakeCreatedAt,
				},
				{
					ExternalID: "2", Number: 2, Title: "Second issue", Body: "Body two",
					State: "closed", AuthorName: "Bob", AuthorEmail: "bob@example.com",
					CreatedAt: fakeCreatedAt, ClosedAt: fakeClosedAt,
					ClosedByName: "Alice", ClosedByEmail: "alice@example.com",
				},
			},
			Comments: []ImportComment{{
				ExternalID: "100", PostID: "1", Content: "Me too",
				AuthorName: "Bob", AuthorEmail: "bob@example.com", CreatedAt: fakeCreatedAt,
			}},
			Filtered: 3,
		},
		relPlan: &ReleasePlan{Releases: []ImportRelease{{
			ExternalID: "v1.0.0", Name: "v1.0.0", Body: "Release notes",
			Tag: "v1.0.0", Version: "1.0.0", AuthorName: "Alice",
			AuthorEmail: "alice@example.com", CreatedAt: fakeCreatedAt,
		}}},
		revPlan: &ReviewPlan{PRs: []ImportPR{{
			ExternalID: "7", Number: 7, Title: "Add widget", Body: "Adds a widget",
			State: "open", BaseBranch: "main", HeadBranch: "feature/widget",
			Labels: []string{"enhancement"}, Reviewers: []string{"carol@example.com"},
			AuthorName: "Bob", AuthorEmail: "bob@example.com", CreatedAt: fakeCreatedAt,
		}}},
		socPlan: &SocialPlan{
			Posts: []ImportPost{{
				ExternalID: "9", Content: "Hello world", AuthorName: "Alice",
				AuthorEmail: "alice@example.com", CreatedAt: fakeCreatedAt,
			}},
			Comments: []ImportComment{{
				ExternalID: "900", PostID: "9", Content: "Nice one",
				AuthorName: "Bob", AuthorEmail: "bob@example.com", CreatedAt: fakeCreatedAt,
			}},
		},
	}
}

// newRunOptions returns Options for a fresh workspace and mapping directory.
func newRunOptions(t *testing.T) Options {
	t.Helper()
	return Options{
		WorkDir:    testutil.CopyRepo(t, runRepoTemplate),
		RepoURL:    "https://github.com/acme/widgets",
		CacheDir:   t.TempDir(),
		Extensions: []string{"all"},
	}
}

// branchCommits returns the commits on an extension branch, or nil when absent.
func branchCommits(t *testing.T, workdir, branch string) []git.Commit {
	t.Helper()
	commits, err := git.GetCommits(workdir, &git.GetCommitsOptions{Branch: branch})
	if err != nil {
		return nil
	}
	return commits
}

// findCommit returns the parsed commit on a branch whose content starts with prefix.
func findCommit(t *testing.T, workdir, branch, prefix string) *protocol.Message {
	t.Helper()
	for _, c := range branchCommits(t, workdir, branch) {
		msg := protocol.ParseMessage(c.Message)
		if msg != nil && len(msg.Content) >= len(prefix) && msg.Content[:len(prefix)] == prefix {
			return msg
		}
	}
	t.Fatalf("no commit on %s whose content starts with %q", branch, prefix)
	return nil
}

func TestRun_CreatesItems(t *testing.T) {
	opts := newRunOptions(t)
	var phases []string
	opts.OnProgress = func(e ProgressEvent) {
		if e.Phase == PhaseDone {
			phases = append(phases, e.Extension)
		}
	}
	adapter := newFakeAdapter()
	stats, err := Run(adapter, opts)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	want := Stats{Milestones: 1, Issues: 2, Releases: 1, PRs: 1, Posts: 1, Comments: 2, FilteredIssues: 3}
	if stats.Milestones != want.Milestones || stats.Issues != want.Issues ||
		stats.Releases != want.Releases || stats.PRs != want.PRs ||
		stats.Posts != want.Posts || stats.Comments != want.Comments {
		t.Errorf("stats = %+v, want %+v", stats, want)
	}
	if stats.FilteredIssues != want.FilteredIssues {
		t.Errorf("FilteredIssues = %d, want %d", stats.FilteredIssues, want.FilteredIssues)
	}
	if len(stats.Errors) != 0 {
		t.Errorf("Errors = %+v, want none", stats.Errors)
	}
	if fmt.Sprint(phases) != fmt.Sprint([]string{"pm", "release", "review", "social"}) {
		t.Errorf("done phases = %v", phases)
	}

	// Commits landed on the extension branches: 1 milestone + 2 issues + 1 close edit.
	if n := len(branchCommits(t, opts.WorkDir, "gitmsg/pm")); n != 4 {
		t.Errorf("gitmsg/pm commits = %d, want 4", n)
	}
	if n := len(branchCommits(t, opts.WorkDir, "gitmsg/release")); n != 1 {
		t.Errorf("gitmsg/release commits = %d, want 1", n)
	}
	if n := len(branchCommits(t, opts.WorkDir, "gitmsg/review")); n != 1 {
		t.Errorf("gitmsg/review commits = %d, want 1", n)
	}
	// 1 post + 1 discussion comment + 1 issue comment.
	if n := len(branchCommits(t, opts.WorkDir, "gitmsg/social")); n != 3 {
		t.Errorf("gitmsg/social commits = %d, want 3", n)
	}

	// The issue commit carries the GitMsg headers and upstream provenance.
	issueMsg := findCommit(t, opts.WorkDir, "gitmsg/pm", "First issue")
	fields := issueMsg.Header.Fields
	for key, want := range map[string]string{
		"type": "issue", "state": "open", "labels": "kind/bug",
		"assignees": "bob@example.com", "origin-platform": "github",
		"origin-author-name": "Alice", "origin-author-email": "alice@example.com",
		"origin-url":  "https://github.com/acme/widgets/issues/1",
		"origin-time": "2024-06-15T12:00:00Z",
	} {
		if fields[key] != want {
			t.Errorf("issue header %s = %q, want %q", key, fields[key], want)
		}
	}
	if fields["milestone"] == "" {
		t.Error("issue header milestone is empty, want a ref to the imported milestone")
	}

	// The closed issue got a follow-up edit commit carrying state=closed.
	closeMsg := findCommit(t, opts.WorkDir, "gitmsg/pm", "Second issue")
	var sawClose bool
	for _, c := range branchCommits(t, opts.WorkDir, "gitmsg/pm") {
		msg := protocol.ParseMessage(c.Message)
		if msg != nil && msg.Header.Fields["edits"] != "" && msg.Header.Fields["state"] == "closed" {
			sawClose = true
		}
	}
	if !sawClose {
		t.Error("no edit commit with state=closed for the closed issue")
	}
	if closeMsg.Header.Fields["type"] != "issue" {
		t.Errorf("closed issue type = %q", closeMsg.Header.Fields["type"])
	}

	// The extensions read the imported items back.
	repoURL := gitmsg.ResolveRepoURL(opts.WorkDir)
	issues := pm.GetIssues(repoURL, "gitmsg/pm", nil, "", 100)
	if !issues.Success {
		t.Fatalf("GetIssues() failed: %s", issues.Error.Message)
	}
	if len(issues.Data) != 2 {
		t.Fatalf("GetIssues() returned %d issues, want 2", len(issues.Data))
	}
	bySubject := map[string]pm.Issue{}
	for _, issue := range issues.Data {
		bySubject[issue.Subject] = issue
	}
	first, ok := bySubject["First issue"]
	if !ok {
		t.Fatalf("issues = %v", bySubject)
	}
	if first.State != pm.StateOpen {
		t.Errorf("First issue State = %q, want open", first.State)
	}
	if first.Origin == nil || first.Origin.Platform != "github" || first.Origin.AuthorName != "Alice" {
		t.Errorf("First issue Origin = %+v", first.Origin)
	}
	if len(first.Labels) != 1 || first.Labels[0].Scope != "kind" || first.Labels[0].Value != "bug" {
		t.Errorf("First issue Labels = %+v", first.Labels)
	}
	if second := bySubject["Second issue"]; second.State != pm.StateClosed {
		t.Errorf("Second issue State = %q, want closed", second.State)
	}

	releases := releasepkg.GetReleases(repoURL, "gitmsg/release", "", 100)
	if !releases.Success {
		t.Fatalf("GetReleases() failed: %s", releases.Error.Message)
	}
	if len(releases.Data) != 1 || releases.Data[0].Tag != "v1.0.0" {
		t.Errorf("releases = %+v", releases.Data)
	}

	mapping, err := ReadMapping(opts.CacheDir, opts.RepoURL, "")
	if err != nil {
		t.Fatalf("ReadMapping() error = %v", err)
	}
	for _, key := range []string{
		MappingKey("github", "milestone", "v1.0"),
		MappingKey("github", "issue", "1"),
		MappingKey("github", "issue", "2"),
		MappingKey("github", "issue-comment", "100"),
		MappingKey("github", "release", "v1.0.0"),
		MappingKey("github", "pr", "7"),
		MappingKey("github", "post", "9"),
		MappingKey("github", "comment", "900"),
	} {
		if !mapping.IsMapped(key) {
			t.Errorf("mapping is missing %s", key)
		}
	}
	if mapping.Source != "github" || mapping.RepoURL != opts.RepoURL {
		t.Errorf("mapping Source = %q, RepoURL = %q", mapping.Source, mapping.RepoURL)
	}

	prHash := mapping.GetHash(MappingKey("github", "pr", "7"))
	pr := review.GetPR(prHash)
	if !pr.Success {
		t.Fatalf("GetPR(%s) failed: %s", prHash, pr.Error.Message)
	}
	if pr.Data.Subject != "Add widget" || pr.Data.State != "open" {
		t.Errorf("PR = %+v", pr.Data)
	}
	if pr.Data.Base != "#branch:main" || pr.Data.Head != "#branch:feature/widget" {
		t.Errorf("PR base = %q, head = %q", pr.Data.Base, pr.Data.Head)
	}

	posts := social.GetPosts(opts.WorkDir, "repository:workspace", nil)
	if !posts.Success {
		t.Fatalf("GetPosts() failed: %s", posts.Error.Message)
	}
	var sawPost bool
	for _, p := range posts.Data {
		if p.Content == "Hello world" {
			sawPost = true
		}
	}
	if !sawPost {
		t.Errorf("imported post not found in %d posts", len(posts.Data))
	}
}

func TestRun_SkipsAlreadyImported(t *testing.T) {
	opts := newRunOptions(t)
	adapter := newFakeAdapter()
	if _, err := Run(adapter, opts); err != nil {
		t.Fatalf("first Run() error = %v", err)
	}
	before := map[string]int{}
	for _, branch := range []string{"gitmsg/pm", "gitmsg/release", "gitmsg/review", "gitmsg/social"} {
		before[branch] = len(branchCommits(t, opts.WorkDir, branch))
	}

	stats, err := Run(adapter, opts)
	if err != nil {
		t.Fatalf("second Run() error = %v", err)
	}
	if stats.Total() != 0 {
		t.Errorf("second run imported %d items, want 0 (%+v)", stats.Total(), stats)
	}
	if stats.Skipped != 8 {
		t.Errorf("Skipped = %d, want 8", stats.Skipped)
	}
	for branch, n := range before {
		if got := len(branchCommits(t, opts.WorkDir, branch)); got != n {
			t.Errorf("%s commits = %d, want %d (unchanged)", branch, got, n)
		}
	}
	// The mapped IDs are handed to the adapter so a real one can skip fetching them.
	for _, key := range []string{"issue:1", "issue:2", "milestone:v1.0", "pr:7", "post:9"} {
		if !adapter.seenSkips[key] {
			t.Errorf("SkipExternalIDs is missing %s: %v", key, adapter.seenSkips)
		}
	}
}

func TestRun_ResumesFromPartialMapping(t *testing.T) {
	opts := newRunOptions(t)
	seeded := &MappingFile{Items: map[string]MappedItem{
		MappingKey("github", "issue", "1"): {Hash: "abcdef123456", Branch: "gitmsg/pm", Type: "issue"},
	}}
	if err := WriteMapping(opts.CacheDir, opts.RepoURL, "", seeded); err != nil {
		t.Fatalf("WriteMapping() error = %v", err)
	}

	adapter := newFakeAdapter()
	stats, err := Run(adapter, Options{
		WorkDir: opts.WorkDir, RepoURL: opts.RepoURL, CacheDir: opts.CacheDir,
		Extensions: []string{"pm"},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if stats.Issues != 1 {
		t.Errorf("Issues = %d, want 1 (only the unmapped issue)", stats.Issues)
	}
	if stats.Milestones != 1 {
		t.Errorf("Milestones = %d, want 1", stats.Milestones)
	}
	if stats.Skipped != 1 {
		t.Errorf("Skipped = %d, want 1", stats.Skipped)
	}
	if !adapter.seenSkips["issue:1"] {
		t.Errorf("SkipExternalIDs = %v, want issue:1", adapter.seenSkips)
	}
	for _, c := range branchCommits(t, opts.WorkDir, "gitmsg/pm") {
		if msg := protocol.ParseMessage(c.Message); msg != nil && msg.Content == "First issue\n\nBody one" {
			t.Error("already-mapped issue was imported again")
		}
	}
	// The pre-existing entry survives, alongside the newly imported ones.
	mapping, err := ReadMapping(opts.CacheDir, opts.RepoURL, "")
	if err != nil {
		t.Fatalf("ReadMapping() error = %v", err)
	}
	if mapping.GetHash(MappingKey("github", "issue", "1")) != "abcdef123456" {
		t.Errorf("seeded mapping entry was overwritten: %+v", mapping.Items)
	}
	if !mapping.IsMapped(MappingKey("github", "issue", "2")) {
		t.Error("mapping is missing the newly imported issue 2")
	}
}

func TestRun_DryRunWritesNothing(t *testing.T) {
	opts := newRunOptions(t)
	opts.DryRun = true
	stats, err := Run(newFakeAdapter(), opts)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if stats.Issues != 2 || stats.Milestones != 1 || stats.Releases != 1 || stats.PRs != 1 || stats.Posts != 1 {
		t.Errorf("dry-run stats = %+v", stats)
	}
	for _, branch := range []string{"gitmsg/pm", "gitmsg/release", "gitmsg/review", "gitmsg/social"} {
		if n := len(branchCommits(t, opts.WorkDir, branch)); n != 0 {
			t.Errorf("dry run created %d commits on %s", n, branch)
		}
	}
	path := ResolveMappingPath(opts.CacheDir, opts.RepoURL, "")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("dry run wrote a mapping file at %s", path)
	}
	if _, err := os.Stat(filepath.Join(opts.CacheDir, "imports")); err == nil {
		entries, _ := os.ReadDir(filepath.Join(opts.CacheDir, "imports"))
		if len(entries) != 0 {
			t.Errorf("dry run left %d files under imports/", len(entries))
		}
	}
}

func TestRun_FetchErrorIsReportedAndOtherExtensionsContinue(t *testing.T) {
	opts := newRunOptions(t)
	adapter := newFakeAdapter()
	adapter.pmErr = fmt.Errorf("gh api: boom")
	stats, err := Run(adapter, opts)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(stats.Errors) != 1 || stats.Errors[0].Type != "pm" {
		t.Fatalf("Errors = %+v, want one pm error", stats.Errors)
	}
	if stats.Issues != 0 {
		t.Errorf("Issues = %d, want 0", stats.Issues)
	}
	if stats.Releases != 1 || stats.PRs != 1 || stats.Posts != 1 {
		t.Errorf("other extensions did not run: %+v", stats)
	}
}

func TestRun_ClosedStatesResolveOnTheCanonicalItem(t *testing.T) {
	opts := newRunOptions(t)
	adapter := newFakeAdapter()
	adapter.pmPlan.Milestones[0].State = "closed"
	adapter.revPlan.PRs[0].State = "closed"
	adapter.revPlan.PRs[0].ClosedAt = fakeClosedAt
	adapter.revPlan.PRs = append(adapter.revPlan.PRs, ImportPR{
		ExternalID: "8", Number: 8, Title: "Merged widget", State: "merged",
		BaseBranch: "main", HeadBranch: "feature/merged", MergedAt: fakeClosedAt,
		AuthorName: "Bob", AuthorEmail: "bob@example.com", CreatedAt: fakeCreatedAt,
	})
	stats, err := Run(adapter, opts)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	repoURL := gitmsg.ResolveRepoURL(opts.WorkDir)

	milestones := pm.GetMilestones(repoURL, "gitmsg/pm", nil, "", 100)
	if !milestones.Success {
		t.Fatalf("GetMilestones() failed: %s", milestones.Error.Message)
	}
	if len(milestones.Data) != 1 {
		t.Fatalf("GetMilestones() returned %d, want 1", len(milestones.Data))
	}
	if milestones.Data[0].State != pm.StateClosed {
		t.Errorf("milestone State = %q, want closed", milestones.Data[0].State)
	}

	mapping, err := ReadMapping(opts.CacheDir, opts.RepoURL, "")
	if err != nil {
		t.Fatalf("ReadMapping() error = %v", err)
	}
	closedPR := review.GetPR(mapping.GetHash(MappingKey("github", "pr", "7")))
	if !closedPR.Success {
		t.Fatalf("GetPR() failed: %s", closedPR.Error.Message)
	}
	if closedPR.Data.State != "closed" {
		t.Errorf("PR 7 State = %q, want closed", closedPR.Data.State)
	}

	// A merged PR with no merge commit cannot carry merge-base/merge-head
	// (GITREVIEW.md 1.5), so it is imported as open with a reported error.
	var sawMergeError bool
	for _, e := range stats.Errors {
		if e.Type == "pr-state" {
			sawMergeError = true
		}
	}
	if !sawMergeError {
		t.Errorf("Errors = %+v, want a pr-state error for the unresolvable merge", stats.Errors)
	}
	mergedPR := review.GetPR(mapping.GetHash(MappingKey("github", "pr", "8")))
	if !mergedPR.Success {
		t.Fatalf("GetPR() failed: %s", mergedPR.Error.Message)
	}
	if mergedPR.Data.State != "open" {
		t.Errorf("PR 8 State = %q, want open (merge state was not recordable)", mergedPR.Data.State)
	}
}

func TestRun_SelectedExtensionsOnly(t *testing.T) {
	opts := newRunOptions(t)
	opts.Extensions = []string{"release"}
	stats, err := Run(newFakeAdapter(), opts)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if stats.Releases != 1 || stats.Issues != 0 || stats.PRs != 0 || stats.Posts != 0 {
		t.Errorf("stats = %+v, want releases only", stats)
	}
	if n := len(branchCommits(t, opts.WorkDir, "gitmsg/pm")); n != 0 {
		t.Errorf("gitmsg/pm commits = %d, want 0", n)
	}
}

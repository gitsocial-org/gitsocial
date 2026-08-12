// site_pages_commits_test.go - the crawlable commits list layer against the
// in-process S3 stub: the sealed/head layout and row shape, sitemap coverage and
// cache policy, the frontier ancestry guard on BOTH branches (history still an
// ancestor → seal onward and never rewrite a sealed page; history rewritten →
// re-derive the whole chain), and the budgeted, resumable bootstrap.

package objstore

import (
	"strings"
	"testing"
)

// buildCodePages runs the code items index for the given default branch, then
// every gitmsg items index, then the page layer — the shell-free equivalent of a
// site push's tail on a repo that has code branches.
func buildCodePages(t *testing.T, client *Client) (pending bool, state string) {
	t.Helper()
	const defaultBranch = "main"
	refs := pagesRefs(client, t)
	for _, ext := range siteItemsExts {
		if tip, ok := refs["refs/heads/gitmsg/"+ext]; ok {
			if err := updateSiteItemsIndex(client, "", ext, tip, nil); err != nil {
				t.Fatalf("items index %s: %v", ext, err)
			}
		}
	}
	if err := updateSiteCodeIndex(client, "", codeBranchTips(refs, defaultBranch), defaultBranch, nil); err != nil {
		t.Fatalf("code index: %v", err)
	}
	pending, state, err := rebuildSitePages(client, "", pagesRefs(client, t), defaultBranch, nil, nil, SiteOverride{})
	if err != nil {
		t.Fatalf("rebuildSitePages: %v", err)
	}
	return pending, state
}

// seedCodeBranch uploads a linear chain of n commits (optionally onto parent),
// points refs/heads/main at its tip, and makes it the repo's HEAD.
func seedCodeBranch(t *testing.T, client *Client, parent, salt string, n int) []string {
	t.Helper()
	shas := seedChain(t, client, parent, salt, n)
	if err := client.Put("refs/heads/main", []byte(shas[len(shas)-1]+"\n")); err != nil {
		t.Fatalf("seed main: %v", err)
	}
	if err := client.Put("HEAD", []byte("ref: refs/heads/main\n")); err != nil {
		t.Fatalf("seed HEAD: %v", err)
	}
	return shas
}

// commitsState reads back the pages manifest's published commits pagination.
func commitsState(t *testing.T, client *Client) siteCommitsState {
	t.Helper()
	m, err := readSitePagesManifest(client, "")
	if err != nil || m == nil {
		t.Fatalf("pages manifest: %v (nil=%v)", err, m == nil)
	}
	if m.Commits == nil {
		t.Fatal("pages manifest carries no commits state")
	}
	return *m.Commits
}

// TestSiteCommits_SealCount pins the sealing boundary the integration tests
// straddle rather than land on: a head of exactly one page's worth seals
// nothing, which is what keeps the head non-empty and page 1 the oldest hundred.
func TestSiteCommits_SealCount(t *testing.T) {
	size := sitePagesListSize
	for _, tc := range []struct{ head, want int }{
		{0, 0}, {1, 0}, {size - 1, 0}, {size, 0}, {size + 1, 1},
		{2 * size, 1}, {2*size + 1, 2}, {230, 2}, {310, 3},
	} {
		if got := siteCommitsSealCount(tc.head); got != tc.want {
			t.Errorf("siteCommitsSealCount(%d) = %d, want %d", tc.head, got, tc.want)
		}
	}
}

// TestSiteCommits_SealedLayoutAndRows: 230 default-branch commits become two
// sealed pages (1 = oldest hundred) plus a 30-row head, each row carrying the
// citable anchor and the metadata a crawler indexes, with the whole set in the
// sitemap.
func TestSiteCommits_SealedLayoutAndRows(t *testing.T) {
	client, _ := testClient(t)
	seedPagesConfig(t, client, pagesTestSite())
	shas := seedCodeBranch(t, client, "", "", 230)
	// A feature branch off the trunk: its own commits are in the code corpus (the
	// timeline interleaves them) but the list is DEFAULT BRANCH only.
	feature := seedChain(t, client, shas[100], "feature ", 3)
	if err := client.Put("refs/heads/feature/x", []byte(feature[2]+"\n")); err != nil {
		t.Fatal(err)
	}
	if pending, _ := buildCodePages(t, client); pending {
		t.Fatal("230 commits must seal in one pass under the default budget")
	}

	head := getKey(t, client, "commits/index.html")
	if !strings.Contains(head, `href="2.html">older →`) {
		t.Error("head must chain older → 2.html")
	}
	if !strings.Contains(head, "230 commits") || !strings.Contains(head, "main") || !strings.Contains(head, "newest first") {
		t.Error("head meta line must carry the total, the branch and the order")
	}
	if !strings.Contains(head, "commit 229") || strings.Contains(head, "commit 199") {
		t.Error("head must hold exactly the unsealed newest rows (200..229)")
	}
	// The row shape IS the feature: a stable id per row, the subject linking into
	// the app's commit view, and author/date/sha as indexable text.
	newest := shas[229][:12]
	if !strings.Contains(head, `<li id="c-`+newest+`">`) {
		t.Errorf("row must carry its citable anchor id c-%s", newest)
	}
	if !strings.Contains(head, `<a href="../index.html#commit:`+newest+`@main">commit 229</a>`) {
		t.Error("row subject must link into the app's commit view on the default branch")
	}
	if !strings.Contains(head, `<span class="meta">Test User · 1970-01-01 · `+newest+`</span>`) {
		t.Errorf("row meta must be author · date · sha; head=%s", head[strings.Index(head, "<ol"):min(len(head), strings.Index(head, "<ol")+400)])
	}

	page1 := getKey(t, client, "commits/1.html")
	if !strings.Contains(page1, "commit 0") || !strings.Contains(page1, "commit 99") || strings.Contains(page1, "commit 100") {
		t.Error("page 1 must hold the oldest hundred")
	}
	if strings.Contains(page1, "older →") {
		t.Error("page 1 (oldest) must have no older link")
	}
	page2 := getKey(t, client, "commits/2.html")
	if !strings.Contains(page2, `href="index.html">← newer`) || !strings.Contains(page2, `href="1.html">older →`) {
		t.Error("page 2 must chain newer→index and older→1")
	}
	if keyExists(client, "commits/3.html") {
		t.Error("no page 3 yet")
	}
	for _, page := range []string{head, page1, page2} {
		if strings.Contains(page, "feature commit") {
			t.Error("the list is default-branch only: a feature branch's commits must not appear")
		}
	}

	st := commitsState(t, client)
	if st.Branch != "main" || st.Total != 230 || st.Sealed != 2 || st.Frontier != shas[199][:12] || st.Pending {
		t.Errorf("commits state = %+v, want branch=main total=230 sealed=2 frontier=%s", st, shas[199][:12])
	}

	// Every commits page is a crawl surface: the sitemap claims the head and both
	// sealed pages, and the nav on every generated page links the list.
	sitemap := getKey(t, client, sitePagesSitemapKey)
	for _, loc := range []string{"https://example.com/commits/index.html", "https://example.com/commits/1.html", "https://example.com/commits/2.html"} {
		if !strings.Contains(sitemap, "<loc>"+loc+"</loc>") {
			t.Errorf("sitemap must cover %s", loc)
		}
	}
	if !strings.Contains(getKey(t, client, sitePagesFrontKey), `href="./commits/index.html"`) {
		t.Error("the front page's nav must link the commits list")
	}
	// A sealed commits page is re-derivable (the default branch can be rewritten),
	// so unlike a sealed type-list page it must never be cached as immutable.
	if cacheControlForKey("commits/1.html") != cacheControlRevalidate {
		t.Error("sealed commits pages must revalidate, not cache immutably")
	}
	if cacheControlForKey("posts/1.html") != cacheControlImmutable {
		t.Error("control: a sealed type-list page still caches immutably")
	}
}

// TestSiteCommits_FrontierGuard pins both branches of the ancestry guard. New
// commits on top leave the frontier an ancestor of the tip: the layer seals
// onward and never rewrites a sealed page. History rewritten under the frontier
// takes it out of the corpus: the layer re-derives its whole chain rather than
// serving pages describing history that no longer exists.
func TestSiteCommits_FrontierGuard(t *testing.T) {
	client, bucket := testClient(t)
	seedPagesConfig(t, client, pagesTestSite())
	shas := seedCodeBranch(t, client, "", "", 230)
	if pending, _ := buildCodePages(t, client); pending {
		t.Fatal("unexpected pending")
	}
	puts1, puts2 := bucket.putCount("commits/1.html"), bucket.putCount("commits/2.html")

	// (a) ANCESTOR: 80 more commits on top. The frontier is untouched history, so
	// the sealed region carries through and only page 3 is written.
	seedCodeBranch(t, client, shas[229], "later ", 80)
	if pending, state := buildCodePages(t, client); pending || state != sitePagesStateOn {
		t.Fatalf("append pass pending=%v state=%q", pending, state)
	}
	if bucket.putCount("commits/1.html") != puts1 || bucket.putCount("commits/2.html") != puts2 {
		t.Error("an append must not rewrite a sealed commits page")
	}
	page3 := getKey(t, client, "commits/3.html")
	if !strings.Contains(page3, "later commit 0") || !strings.Contains(page3, ">commit 229<") {
		t.Error("page 3 must seal exactly the rows that overflowed the head")
	}
	if h := getKey(t, client, "commits/index.html"); !strings.Contains(h, "later commit 79") || strings.Contains(h, "later commit 69") {
		t.Error("the head must keep only the newest ten rows")
	}
	st := commitsState(t, client)
	if st.Total != 310 || st.Sealed != 3 {
		t.Errorf("after append: state = %+v, want total=310 sealed=3", st)
	}
	frontierBefore := st.Frontier

	// (b) REWRITTEN: main is force-moved onto an unrelated chain, so the recorded
	// frontier is no longer reachable. The guard must notice and re-derive.
	puts1 = bucket.putCount("commits/1.html")
	rewritten := seedCodeBranch(t, client, "", "rewritten ", 230)
	if pending, _ := buildCodePages(t, client); pending {
		t.Fatal("unexpected pending after the rewrite")
	}
	st = commitsState(t, client)
	if st.Total != 230 || st.Sealed != 2 {
		t.Errorf("after rewrite: state = %+v, want total=230 sealed=2", st)
	}
	if st.Frontier == frontierBefore || st.Frontier != rewritten[199][:12] {
		t.Errorf("frontier = %q, want the rewritten chain's %s", st.Frontier, rewritten[199][:12])
	}
	if bucket.putCount("commits/1.html") == puts1 {
		t.Error("a rewrite must re-derive the sealed pages, not leave them serving dropped history")
	}
	page1 := getKey(t, client, "commits/1.html")
	if !strings.Contains(page1, "rewritten commit 0") || strings.Contains(page1, `>commit 0<`) {
		t.Error("page 1 must list the rewritten history, never the dropped chain")
	}
	if keyExists(client, "commits/3.html") && strings.Contains(getKey(t, client, "commits/index.html"), `href="3.html"`) {
		t.Error("the head must chain to the re-derived sealed count, not the stale one")
	}
}

// TestSiteCommits_BudgetResumesSealing: a budget too small for the whole chain
// seals what it can, leaves a valid (larger) head rather than a half-written
// chain, reports the layer as pending, and finishes on the next pass.
func TestSiteCommits_BudgetResumesSealing(t *testing.T) {
	client, _ := testClient(t)
	seedPagesConfig(t, client, pagesTestSite())
	prev := sitePagesBudget
	defer func() { sitePagesBudget = prev }()
	sitePagesBudget = 1
	shas := seedCodeBranch(t, client, "", "", 230)

	pending, state := buildCodePages(t, client)
	if !pending || state != "" {
		t.Fatalf("first pass pending=%v state=%q, want pending with no stampable state", pending, state)
	}
	st := commitsState(t, client)
	if st.Sealed != 1 || !st.Pending || st.Frontier != shas[99][:12] {
		t.Errorf("first pass state = %+v, want sealed=1 pending frontier=%s", st, shas[99][:12])
	}
	if keyExists(client, "commits/2.html") {
		t.Error("a budget-cut pass must not write a page it cannot record")
	}
	// The rows the budget could not seal are still listed — in the head.
	if !strings.Contains(getKey(t, client, "commits/index.html"), "commit 100") {
		t.Error("unsealed rows must stay in the head, not vanish")
	}

	if pending, state = buildCodePages(t, client); pending || state != sitePagesStateOn {
		t.Fatalf("resume pass pending=%v state=%q", pending, state)
	}
	st = commitsState(t, client)
	if st.Sealed != 2 || st.Pending || st.Frontier != shas[199][:12] {
		t.Errorf("resume state = %+v, want sealed=2 not pending frontier=%s", st, shas[199][:12])
	}
	if !strings.Contains(getKey(t, client, "commits/2.html"), "commit 100") {
		t.Error("the resumed pass must seal the rows the budget deferred")
	}
}

// TestSiteCommits_NoSealingWhileTheCodeIndexBootstraps: while the code corpus is
// still walking older history, today's oldest row is not the oldest row, so
// nothing may seal — page 1 must be the oldest hundred forever.
func TestSiteCommits_NoSealingWhileTheCodeIndexBootstraps(t *testing.T) {
	client, _ := testClient(t)
	seedPagesConfig(t, client, pagesTestSite())
	prev := siteItemsWalkBudget
	defer func() { siteItemsWalkBudget = prev }()
	siteItemsWalkBudget = 120
	seedCodeBranch(t, client, "", "", 230)
	buildCodePages(t, client)
	m, err := readItemsManifest(client, "", siteCodeExt)
	if err != nil || m == nil || m.Complete {
		t.Fatalf("test setup: the code index must still be bootstrapping (manifest=%v)", m)
	}
	st := commitsState(t, client)
	if st.Sealed != 0 || st.Frontier != "" {
		t.Errorf("nothing may seal off an incomplete corpus: state = %+v", st)
	}
	if keyExists(client, "commits/1.html") {
		t.Error("no sealed page may exist while the code index is incomplete")
	}
	if !strings.Contains(getKey(t, client, "commits/index.html"), "commit 229") {
		t.Error("the head must still list what the corpus knows")
	}
}

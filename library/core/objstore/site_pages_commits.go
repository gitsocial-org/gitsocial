// site_pages_commits.go - the crawlable commits list layer:
//
//	commits/index.html      the mutable head (the newest unsealed rows)
//	commits/<n>.html        sealed pages of 100 rows, 1 = oldest, chained "older →"
//
// One row per DEFAULT-BRANCH commit — subject, author, date, sha, and nothing
// else — so code commits have indexable content and a stable citable URL
// (`commits/7.html#c-<sha12>`) instead of existing only as a hash route into the
// app. Metadata only: a diff per commit is what would blow the size up, and a
// page per commit is what would make object count scale with history rather than
// with items (100k commits become ~1,000 pages of 100 rows, which read as a
// changelog rather than as mass-produced thin pages).
//
// The data source is the push's own CODE ITEMS INDEX
// (.gitsocial/site/items/code/), never a second git walk — the same corpus the
// reader's timeline and the front page's activity rows are drawn from, so both
// surfaces list the same commits under the same branch attribution. "Default
// branch" therefore means "attributed to the default branch by the code index"
// (the default branch when the commit was reachable from it at index time, else
// the first code branch that reached it), which is the definition the whole site
// already uses.
//
// THE FRONTIER ANCESTRY GUARD is the one thing that differs from the gitmsg type
// lists. Those seal safely because `gitmsg/*` branches are append-only by
// protocol; the default branch can be rebased or force-pushed, so a sealed page
// can silently describe history that no longer exists. Every pass therefore
// re-locates the recorded frontier (the sha12 of the newest sealed row) in the
// current list before sealing onward: still there, with the same number of rows
// below it, means the sealed region is intact; anything else means history moved
// under the pages and the layer re-derives its whole sealed chain from the
// current corpus. Costs one scan of a list the pass has already read — no extra
// bucket request.
//
// Two contracts deliberately differ from the type lists, both for the same
// reason (a commits page is re-derivable content, not immutable-by-protocol):
// the commits dir gets no Atom feed (feed.xml is items only — code commits carry
// no body corpus to syndicate), and `commits/<n>.html` is NOT in
// cacheControlForKey's sealed-page class, so a page a rewrite forced the layer to
// re-derive is actually re-fetched rather than served from a year-long immutable
// cache.

package objstore

import (
	"fmt"
	"strconv"
)

const (
	// siteCommitsDir is the commits list's bucket directory.
	siteCommitsDir = "commits"
	// siteCommitsRoute is the shell route a commits page boots into
	// (gs-core.js parseRoute); a sealed page appends "/<n>".
	siteCommitsRoute = "/commits"
)

// siteCommitsList describes the commits directory the way sitePageList describes
// a gitmsg type dir, so the shared list template, nav and page builders serve it
// unchanged. It is deliberately NOT a member of sitePageLists: its rows are code
// commits rather than gitmsg roots, so every loop keyed on an extension's roots
// would have to special-case it, and the feed/cache contracts above differ.
var siteCommitsList = sitePageList{Ext: siteCodeExt, Dir: siteCommitsDir, Label: "commits", Route: siteCommitsRoute}

// siteCommitsState is the commits layer's published pagination, recorded in the
// pages manifest and read back by BOTH sides: the next push (to seal onward
// without re-deriving the layout, and to run the ancestry guard) and the booted
// app, whose /commits route must render exactly the rows the generated page
// shows or the page-entry upgrade visibly jumps. Publishing the layout rather
// than having each side derive it is what makes that parity structural: there is
// one partition, decided by the writer, and the reader reads it.
type siteCommitsState struct {
	Branch   string `json:"branch"`            // the default branch these pages were derived from
	Total    int    `json:"total"`             // default-branch commits listed across head + sealed
	Sealed   int    `json:"sealed"`            // sealed page count (1 = oldest)
	Frontier string `json:"frontier"`          // sha12 of the NEWEST sealed row: the sealing boundary
	Lastmod  string `json:"lastmod,omitempty"` // newest listed commit's date, for the sitemap
	Pending  bool   `json:"pending,omitempty"` // the page budget cut sealing short: resume next push
}

// readSiteCommitEntries returns the code index entries attributed to the default
// branch, NEWEST-FIRST — the commits list's whole data source. complete mirrors
// the code corpus's own manifest: while that index is still bootstrapping its
// older history, the oldest rows are not known yet and nothing may seal (a page
// 1 that later stops being the oldest hundred is exactly the immutability
// promise the sealed pages make). Empty (no error) when the bucket carries no
// code index or no default branch, which is the same condition under which the
// app's own commits route has nothing to render.
func readSiteCommitEntries(client *Client, prefix, defaultBranch string) (entries []siteMetaEntry, complete bool, err error) {
	if defaultBranch == "" {
		return nil, false, nil
	}
	m, err := readItemsManifest(client, prefix, siteCodeExt)
	if err != nil || m == nil {
		return nil, false, err
	}
	all, err := readAllShardEntries(client, prefix, siteCodeExt, itemsCorpus, m)
	if err != nil {
		return nil, false, fmt.Errorf("read code index: %w", err)
	}
	entries = make([]siteMetaEntry, 0, len(all))
	for i := len(all) - 1; i >= 0; i-- { // stored oldest-first; the list reads newest-first
		if all[i].Branch == defaultBranch && len(all[i].SHA) >= 12 {
			entries = append(entries, all[i])
		}
	}
	return entries, m.Complete, nil
}

// siteCommitsFrontierIndex returns the newest-first position of the sha12
// frontier in the entry list, or -1 when it is absent.
func siteCommitsFrontierIndex(entries []siteMetaEntry, frontier string) int {
	for i, e := range entries {
		if e.SHA[:12] == frontier {
			return i
		}
	}
	return -1
}

// siteCommitsSealedRegion resolves how much of the newest-first entry list the
// prior pass already sealed, running the frontier ancestry guard. It returns the
// carried-over sealed page count, its frontier, and the index at which the
// sealed region starts (= the head's length).
//
// The guard is two questions on one already-read list. Is the frontier commit
// still in the default-branch history? A git sha commits to its entire ancestry,
// so a frontier that is still present proves every commit below it is unchanged,
// and a rebase or force-push that dropped it removes it from the corpus the code
// index repairs to. And does the same number of rows still sit below it? That
// catches the one way the region can move while the sha survives — the code
// index rebuilding and re-attributing older commits onto the default branch (a
// long-lived branch merged in, say), which would insert rows underneath a
// frontier that is itself untouched. Either answer wrong means history moved
// under the pages: the layer forgets its chain and re-derives it from the current
// corpus, which is the same recovery a sitePagesVersion bump forces.
func siteCommitsSealedRegion(entries []siteMetaEntry, prior *siteCommitsState, defaultBranch string) (sealed int, frontier string, idx int) {
	if prior == nil || prior.Branch != defaultBranch || prior.Frontier == "" || prior.Sealed <= 0 {
		return 0, "", len(entries)
	}
	idx = siteCommitsFrontierIndex(entries, prior.Frontier)
	if idx < 0 || len(entries)-idx != prior.Sealed*sitePagesListSize {
		return 0, "", len(entries)
	}
	return prior.Sealed, prior.Frontier, idx
}

// siteCommitsSealCount returns how many full pages seal off a head of n rows,
// matching writeSiteTypeLists's "seal the oldest hundred while the head is
// LONGER than a page" rule exactly. The strictness is the whole boundary: a head
// of exactly one page's worth stays the head, so a repo at 200 commits serves one
// sealed page and a 100-row head, not two sealed pages and an empty head.
func siteCommitsSealCount(n int) int {
	if n <= sitePagesListSize {
		return 0
	}
	return (n - 1) / sitePagesListSize
}

// writeSiteCommitPages maintains the commits directory from the newest-first
// default-branch entry list and returns the layer's new published state.
//
// seal is false while either corpus is still bootstrapping (the code index, or
// the item page set): the head then carries every known row and nothing is
// sealed, exactly as the type lists behave mid-bootstrap. Sealing is oldest-first
// and the frontier advances only over pages actually written, so a budget that
// runs out mid-seal leaves a valid — merely larger — head rather than a
// half-written chain, and the returned state's Pending flag brings the next push
// straight back here.
func writeSiteCommitPages(client *Client, prefix string, entries []siteMetaEntry, defaultBranch string, seal bool, site sitePageSite, prior *siteCommitsState, budget int) (*siteCommitsState, error) {
	sealed, frontier, idx := siteCommitsSealedRegion(entries, prior, defaultBranch)
	pages := 0
	if seal {
		pages = siteCommitsSealCount(idx)
	}
	pending := false
	if pages > budget {
		pages, pending = budget, true
	}
	chunk := sitePagesChunk()
	uploads := make([]sitePageUpload, 0, min(pages, chunk))
	for j := 0; j < pages; j++ {
		segment := entries[idx-(j+1)*sitePagesListSize : idx-j*sitePagesListSize]
		page, err := renderSitePage("list", buildSiteCommitsSealedPage(site, segment, defaultBranch, sealed+j+1, sealed+pages))
		if err != nil {
			return nil, err
		}
		uploads = append(uploads, sitePageUpload{key: prefix + siteCommitsDir + "/" + strconv.Itoa(sealed+j+1) + ".html", page: page})
		if len(uploads) < chunk {
			continue
		}
		if err := putSitePages(client, uploads, nil, "", 0, 0); err != nil {
			return nil, err
		}
		uploads = uploads[:0]
	}
	if err := putSitePages(client, uploads, nil, "", 0, 0); err != nil {
		return nil, err
	}
	if pages > 0 {
		frontier = entries[idx-pages*sitePagesListSize].SHA[:12]
		sealed += pages
	}
	head := entries[:idx-pages*sitePagesListSize]
	page, err := renderSitePage("list", buildSiteCommitsHeadPage(site, head, defaultBranch, len(entries), sealed))
	if err != nil {
		return nil, err
	}
	if err := putSitePage(client, prefix+siteCommitsDir+"/index.html", page); err != nil {
		return nil, err
	}
	state := &siteCommitsState{Branch: defaultBranch, Total: len(entries), Sealed: sealed, Frontier: frontier, Pending: pending}
	if len(entries) > 0 {
		state.Lastmod = sitePageDate(entries[0].TS)
	}
	return state, nil
}

// buildSiteCommitEntry renders one commit row: the subject linking into the
// app's commit view (the commit's rich surface, with diffs and file trees), the
// author/date/sha meta line, and the `c-<sha12>` id that gives the row its
// stable citable URL. The crawl value comes from the row's text living on a real
// page, not from where the link points — a crawler indexes subject, author, date
// and sha here and treats the href as a link to index.html, which it already
// knows.
func buildSiteCommitEntry(e siteMetaEntry, base, branch string) sitePageListEntry {
	short := e.SHA[:12]
	return sitePageListEntry{
		ID:    "c-" + short,
		Href:  base + "index.html#commit:" + short + "@" + branch,
		Title: e.Subject,
		Meta:  []string{e.Author, sitePageDate(e.TS), short},
	}
}

// siteCommitsChrome assembles a commits page's head. No TypeFeed: the commits
// dir has no Atom feed of its own (the code corpus carries no bodies to
// syndicate), so its pages advertise the site feed alone.
func siteCommitsChrome(site sitePageSite, title, canonical, route string) sitePageChrome {
	return sitePageChrome{
		Title:       title,
		AccentCSS:   site.AccentCSS,
		Description: sitePageDescription(sitePageListDescription(siteCommitsList, site), ""),
		OGTitle:     title,
		SiteTitle:   site.Title,
		Canonical:   canonical,
		Route:       route,
		Base:        "../",
		Image:       site.Image,
		Icon:        site.Icon,
		Feed:        site.URL + sitePagesFeedKey,
	}
}

// buildSiteCommitsHeadPage assembles the mutable commits head page.
func buildSiteCommitsHeadPage(site sitePageSite, head []siteMetaEntry, branch string, total, sealed int) siteListPageData {
	entries := make([]sitePageListEntry, 0, len(head))
	for _, e := range head {
		entries = append(entries, buildSiteCommitEntry(e, "../", branch))
	}
	metaBits := []string{fmt.Sprintf("%d %s", total, siteCommitsList.Label), branch, "newest first"}
	d := siteChainedListPage(siteCommitsDir, siteCommitsList.Label, entries, metaBits, 0, sealed)
	d.Chrome = siteCommitsChrome(site, siteCommitsList.Label+" · "+site.Title, site.URL+siteCommitsDir+"/index.html", siteCommitsRoute)
	return d
}

// buildSiteCommitsSealedPage assembles one sealed commits page (n = 1 is the
// oldest); siteChainedListPage owns the newer/older chain, so the commits list
// and the type lists cannot chain differently.
func buildSiteCommitsSealedPage(site sitePageSite, segment []siteMetaEntry, branch string, n, sealed int) siteListPageData {
	entries := make([]sitePageListEntry, 0, len(segment))
	for _, e := range segment {
		entries = append(entries, buildSiteCommitEntry(e, "../", branch))
	}
	metaBits := []string{fmt.Sprintf("%d %s", len(entries), siteCommitsList.Label), branch, fmt.Sprintf("older page %d", n)}
	d := siteChainedListPage(siteCommitsDir, siteCommitsList.Label, entries, metaBits, n, sealed)
	title := fmt.Sprintf("%s · page %d · %s", siteCommitsList.Label, n, site.Title)
	d.Chrome = siteCommitsChrome(site, title, site.URL+siteCommitsDir+"/"+strconv.Itoa(n)+".html", siteCommitsRoute+"/"+strconv.Itoa(n))
	return d
}

// buildSiteCommitsSitemapEntries collects the commits pages' sitemap URLs: the
// head (with the newest listed commit's date) and every sealed page. Sealed
// pages carry no <lastmod> on purpose — their content is fixed at seal time, so
// there is no recrawl hint to give, and deriving one would mean re-reading the
// whole corpus on passes where the commits layer did no work at all.
func buildSiteCommitsSitemapEntries(state *siteCommitsState, site sitePageSite) []siteSitemapEntry {
	if state == nil {
		return nil
	}
	entries := []siteSitemapEntry{{loc: site.URL + siteCommitsDir + "/index.html", lastmod: state.Lastmod}}
	for n := 1; n <= state.Sealed; n++ {
		entries = append(entries, siteSitemapEntry{loc: site.URL + siteCommitsDir + "/" + strconv.Itoa(n) + ".html"})
	}
	return entries
}

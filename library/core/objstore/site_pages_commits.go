// site_pages_commits.go - the crawlable commits list layer, its sealing chain and its frontier guard

package objstore

import (
	"fmt"
	"strconv"
)

const (
	// siteCommitsDir is the commits list's bucket directory.
	siteCommitsDir = "commits"
	// siteCommitsRoute is the shell route a commits page boots into; a sealed page appends "/<n>".
	siteCommitsRoute = "/commits"
)

// siteCommitsList describes the commits directory the way sitePageList describes a type dir; it is not a member of sitePageLists, whose loops are keyed on gitmsg roots.
var siteCommitsList = sitePageList{Ext: siteCodeExt, Dir: siteCommitsDir, Label: "commits", Route: siteCommitsRoute, NavLabel: "Commits", Glyph: "≡", Section: "Repository"}

// siteCommitsState is the commits layer's published pagination, read back by the next push and by the app's own /commits route, so one partition serves both.
type siteCommitsState struct {
	Branch   string `json:"branch"`            // the default branch these pages were derived from
	Total    int    `json:"total"`             // default-branch commits listed across head + sealed
	Sealed   int    `json:"sealed"`            // sealed page count (1 = oldest)
	Frontier string `json:"frontier"`          // sha12 of the NEWEST sealed row: the sealing boundary
	Lastmod  string `json:"lastmod,omitempty"` // newest listed commit's date, for the sitemap
	Pending  bool   `json:"pending,omitempty"` // the page budget cut sealing short: resume next push
}

// readSiteCommitEntries returns the code index entries attributed to the default branch, newest-first; complete mirrors the code corpus's own manifest.
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

// siteCommitsFrontierIndex returns the newest-first position of the frontier in the entry list, or -1.
func siteCommitsFrontierIndex(entries []siteMetaEntry, frontier string) int {
	for i, e := range entries {
		if e.SHA[:12] == frontier {
			return i
		}
	}
	return -1
}

// siteCommitsSealedRegion resolves how much of the entry list the prior pass sealed, running the frontier guard: the frontier sha must still be present, with the same row count below it.
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

// siteCommitsSealCount returns how many full pages seal off a head of n rows; a head of one page's worth stays the head, as in writeSiteTypeLists.
func siteCommitsSealCount(n int) int {
	if n <= sitePagesListSize {
		return 0
	}
	return (n - 1) / sitePagesListSize
}

// writeSiteCommitPages maintains the commits directory and returns its new state; the frontier advances only over pages written, so an exhausted budget leaves a larger head.
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

// buildSiteCommitEntry renders one commit row: its subject linking into the app's commit view, its meta line, and the id that makes the row citable.
func buildSiteCommitEntry(e siteMetaEntry, base, branch string) sitePageListEntry {
	short := e.SHA[:12]
	glyph, glyphClass := sitePageGlyph("commit", "commit", "")
	return sitePageListEntry{
		ID:         "c-" + short,
		Glyph:      glyph,
		GlyphClass: glyphClass,
		GlyphTitle: "commit",
		Href:       base + "index.html#commit:" + short + "@" + branch,
		Title:      e.Subject,
		Meta:       []string{e.Author, sitePageDate(e.TS), short},
	}
}

// siteCommitsChrome assembles a commits page's head; the dir has no feed of its own, so its pages advertise the site feed alone.
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
		Nav:         sitePageSidebar("../", siteCommitsDir, site.Files),
	}
}

// buildSiteCommitsHeadPage assembles the mutable commits head page.
func buildSiteCommitsHeadPage(site sitePageSite, head []siteMetaEntry, branch string, total, sealed int) siteListPageData {
	entries := make([]sitePageListEntry, 0, len(head))
	for _, e := range head {
		entries = append(entries, buildSiteCommitEntry(e, "../", branch))
	}
	metaBits := []string{fmt.Sprintf("%d %s", total, siteCommitsList.Label), branch, "newest first"}
	d := siteChainedListPage(siteCommitsList.NavLabel, entries, metaBits, 0, sealed)
	d.Chrome = siteCommitsChrome(site, siteCommitsList.NavLabel+" · "+site.Title, site.URL+siteCommitsDir+"/index.html", siteCommitsRoute)
	return d
}

// buildSiteCommitsSealedPage assembles one sealed commits page; siteChainedListPage owns the chain, so the two list kinds cannot differ.
func buildSiteCommitsSealedPage(site sitePageSite, segment []siteMetaEntry, branch string, n, sealed int) siteListPageData {
	entries := make([]sitePageListEntry, 0, len(segment))
	for _, e := range segment {
		entries = append(entries, buildSiteCommitEntry(e, "../", branch))
	}
	metaBits := []string{fmt.Sprintf("%d %s", len(entries), siteCommitsList.Label), branch, fmt.Sprintf("older page %d", n)}
	d := siteChainedListPage(siteCommitsList.NavLabel, entries, metaBits, n, sealed)
	title := fmt.Sprintf("%s · page %d · %s", siteCommitsList.NavLabel, n, site.Title)
	d.Chrome = siteCommitsChrome(site, title, site.URL+siteCommitsDir+"/"+strconv.Itoa(n)+".html", siteCommitsRoute+"/"+strconv.Itoa(n))
	return d
}

// buildSiteCommitsSitemapEntries collects the commits pages' sitemap URLs; a sealed page carries no lastmod, since its content is fixed at seal time.
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

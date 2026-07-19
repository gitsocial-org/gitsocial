// site_pages.go - push-maintained static HTML page layer (the website's
// human-readable pages), generated at the prefix root next to the shell:
//
//   i/<shorthash>.html      one page per top-level gitmsg item, thread inlined
//   issues/ prs/ posts/ releases/ memos/
//                           per-type list pages (mutable index.html head +
//                           immutable sealed <n>.html, chained "older →")
//   pages.css               the pages' shared stylesheet (their only subresource)
//   index.html              the generated front page (README + timeline + PE
//                           hooks + gs-upgrade.js) — the M8 entry flip: when the
//                           page layer is effective the pages maintainer OWNS
//                           index.html; uploadSiteFiles owns the embedded shell
//                           index.html only when it is not (dual-mode ownership).
//   sitemap.xml robots.txt  crawl surface (sitemap-<n>.xml parts past ~40K URLs)
//   feed.xml                Atom 1.0 feed of the newest top-level items
//   <dir>/feed.xml          per-type Atom feeds mirroring each list page
//
// Pages are a projection of the push's own artifacts (the items metadata index
// + bodies corpus, never a second git walk), enabled by the pushed guards
// (site.publish + site.pages + a valid site.url), and tracked by the pages
// manifest at .gitsocial/site/pages.json. This file owns the manifest, the
// per-push page budget, the bootstrap/full-regen path (missing/foreign-version
// manifest → budgeted regeneration with a cursor resume), the incremental path
// (complete manifest + moved items tips → classify the delta, regenerate only
// the affected threads/lists), and the disable path (guards off while the
// bucket carries pages → best-effort deletion, manifest last).
//
// The page root keys are reserved alongside the repo data layout (HEAD,
// objects/, refs/, .gitsocial/) and the shell files (index.html, gs-*.js,
// icons.js, prism.js, grammars/) — all disjoint by construction.

package objstore

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
)

const (
	// sitePagesManifestKey tracks the generated page set: schema version, the
	// per-extension items-manifest tips consumed, the bootstrap cursor while
	// incomplete, and the list pagination state.
	sitePagesManifestKey = ".gitsocial/site/pages.json"
	// sitePagesVersion is the page layer's schema version; a manifest at any
	// other version is treated as absent (full regen under budget). v2: the
	// Atom feed (feed.xml) plus the absolute autodiscovery link in every head.
	sitePagesVersion = 2
	// sitePagesListSize is one list page's entry count.
	sitePagesListSize = 100
	// sitePagesFrontSize is the front page's entry count.
	sitePagesFrontSize = 50
	// sitePagesReadmeMax caps the front page's inlined README bytes.
	sitePagesReadmeMax = 8 * 1024
)

// sitePagesBudget bounds one push's item-page writes. A page set larger than
// the budget bootstraps over several pushes, resuming from the manifest's
// cursor. A var so tests can lower it; GITSOCIAL_SITE_PAGES_BUDGET overrides.
var sitePagesBudget = sitePagesBudgetFromEnv()

// sitePagesBudgetFromEnv returns the per-push page budget, honoring a positive
// GITSOCIAL_SITE_PAGES_BUDGET override, else the 5000 default.
func sitePagesBudgetFromEnv() int {
	if v := os.Getenv("GITSOCIAL_SITE_PAGES_BUDGET"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return 5000
}

// sitePagesManifest is the .gitsocial/site/pages.json document.
type sitePagesManifest struct {
	Version  int               `json:"version"`
	Ext      map[string]string `json:"ext"`                // per-extension items-manifest tip consumed ("code" included: the front page interleaves code commits)
	Cursor   *sitePagesCursor  `json:"cursor,omitempty"`   // present while the bootstrap is incomplete
	Counts   map[string]int    `json:"counts,omitempty"`   // sealed list pages per type dir
	Frontier map[string]string `json:"frontier,omitempty"` // per type dir: sha12 of the newest sealed list entry (sealing boundary)
	SiteHash string            `json:"siteHash,omitempty"` // hash of the site identity (title/url/description) stamped into every page
}

// sitePagesCursor records an in-progress page bootstrap: per-extension counts
// of item pages already generated (newest-first prefix of that extension's
// top-level items).
type sitePagesCursor struct {
	Done map[string]int `json:"done"`
}

// readSitePagesManifest fetches the pages manifest; nil (no error) when it is
// absent, at an unknown version, or unparseable — all of which mean a full
// (re)generation.
func readSitePagesManifest(client *Client, prefix string) (*sitePagesManifest, error) {
	var m sitePagesManifest
	found, err := readCompressedJSON(client, prefix+sitePagesManifestKey, &m)
	if err != nil {
		return nil, err
	}
	if !found || m.Version != sitePagesVersion {
		return nil, nil
	}
	return &m, nil
}

// putSitePagesManifest writes the pages manifest (the layer's commit point,
// always last in the write order).
func putSitePagesManifest(client *Client, prefix string, m *sitePagesManifest) error {
	comp, err := compressJSON(m, brotliQualityFull)
	if err != nil {
		return err
	}
	return putCompressed(client, prefix+sitePagesManifestKey, comp)
}

// putSiteText uploads one plain (uncompressed) page-layer document with its
// Content-Type; crawl and unfurl scrapers are the least capable clients, so
// nothing here carries a Content-Encoding.
func putSiteText(client *Client, key, contentType string, body []byte) error {
	resp, err := client.do(http.MethodPut, key, nil, body, map[string]string{"Content-Type": contentType})
	if err != nil {
		return fmt.Errorf("upload %s: %w", key, err)
	}
	resp.Body.Close()
	return nil
}

// putSitePage uploads one rendered HTML page.
func putSitePage(client *Client, key string, page []byte) error {
	return putSiteText(client, key, "text/html; charset=utf-8", page)
}

// putSitePagesCSS uploads the shared stylesheet, written before any page so no
// page ever references a missing subresource.
func putSitePagesCSS(client *Client, prefix string) error {
	return putSiteText(client, prefix+sitePagesCSSKey, "text/css; charset=utf-8", []byte(sitePagesCSS))
}

// sitePagesEffective resolves the HTML page layer's enablement from the
// bucket's pushed site config: both guards on plus a valid site.url
// (canonicals, OG and the sitemap need the absolute base). Returns the
// normalized base URL.
func sitePagesEffective(cfg siteCustomization, ok bool) (string, bool) {
	if !ok || cfg.Publish != "true" || cfg.Pages != "true" {
		return "", false
	}
	return NormalizeSiteURL(cfg.URL)
}

// sitePageSiteFor assembles the site identity every page stamps.
func sitePageSiteFor(prefix string, cfg siteCustomization, url string) sitePageSite {
	site := sitePageSite{Title: cfg.Title, URL: url, Description: cfg.Description}
	if site.Title == "" {
		site.Title = sitePageDefaultTitle(prefix)
	}
	return site
}

// sitePageSiteHash fingerprints the site identity baked into every rendered
// page (title, canonical base, description); a change regenerates everything.
func sitePageSiteHash(site sitePageSite) string {
	h := sha256.Sum256([]byte(site.Title + "\x00" + site.URL + "\x00" + site.Description))
	return hex.EncodeToString(h[:])[:12]
}

// sitePagesState reports the pages-state component the push-state marker
// records plus whether the page layer still has work that only a site pass runs
// (a pending bootstrap, consumed tips lagging the items indexes, a stale site
// identity, or a page set awaiting the disable-path deletion). The helper's
// post-push maintenance consults it before stamping the push-state marker — a
// stamped marker would make the next site push skip work no ref move signals.
// Best-effort: any read error reports pending with no stampable state, costing
// at worst an extra full pass, never a wrong skip.
func sitePagesState(client *Client, prefix string, refs map[string]string, ov SiteOverride) (state string, pending bool) {
	cfg, ok, err := readSiteCustomization(client, prefix, refs, ov)
	if err != nil {
		return "", true
	}
	url, on := sitePagesEffective(cfg, ok)
	if !on {
		_, exists, err := objectSize(client, prefix+sitePagesManifestKey)
		if err != nil || exists {
			return "", true
		}
		return sitePagesStateOff, false
	}
	manifest, err := readSitePagesManifest(client, prefix)
	if err != nil || manifest == nil || manifest.Cursor != nil {
		return "", true
	}
	if manifest.SiteHash != sitePageSiteHash(sitePageSiteFor(prefix, cfg, url)) {
		return "", true
	}
	_, tips, err := readSitePagesManifests(client, prefix, refs)
	if err != nil || !sitePagesTipsCurrent(manifest, tips) {
		return "", true
	}
	return sitePagesStateOn, false
}

// rebuildSitePages maintains the static HTML page layer after the item
// artifacts. Guards off (or no valid site.url) → any existing page set is
// deleted; pushes are otherwise byte-identical to a pages-less binary. Returns
// pending=true while the layer still has work (an incomplete bootstrap, or an
// incomplete deletion) so the caller leaves the push-state marker unstamped and
// the next push resumes; state is the marker's pages-state component ("" while
// pending).
func rebuildSitePages(client *Client, prefix string, refs map[string]string, defaultBranch string, src *localCommitSource, progress Progress, ov SiteOverride) (pending bool, state string, err error) {
	cfg, ok, err := readSiteCustomization(client, prefix, refs, ov)
	if err != nil {
		return false, "", err
	}
	url, on := sitePagesEffective(cfg, ok)
	if !on {
		complete, err := deleteSitePages(client, prefix)
		if err != nil {
			return false, "", err
		}
		if !complete {
			return true, "", nil
		}
		return false, sitePagesStateOff, nil
	}
	site := sitePageSiteFor(prefix, cfg, url)
	manifest, err := readSitePagesManifest(client, prefix)
	if err != nil {
		return false, "", err
	}
	if manifest != nil && manifest.SiteHash != sitePageSiteHash(site) {
		manifest = nil // the site identity is stamped into every page: full regen
	}
	manifests, tips, err := readSitePagesManifests(client, prefix, refs)
	if err != nil {
		return false, "", err
	}
	readme := readSiteFrontReadme(src, defaultBranch)
	switch {
	case manifest != nil && manifest.Cursor == nil && sitePagesTipsCurrent(manifest, tips):
		// Nothing any page derives from moved: the page set is current. But the
		// front page IS index.html since the M8 flip, and this same push's
		// uploadSiteFiles/ensureSiteShell may have just (re)uploaded the embedded
		// shell over it (a shell-version bump, or any non-pages ref move that
		// un-skips maintenance). Reclaim index.html deterministically — the pages
		// maintainer owns it whenever the layer is effective — from the cheap
		// metadata index (no bodies read).
		err = reclaimSiteFrontPage(client, prefix, site, manifests, readme)
	case manifest != nil && manifest.Cursor == nil:
		pending, err = incrementalSitePages(client, prefix, site, manifest, manifests, tips, readme, progress)
	default:
		pending, err = generateSitePages(client, prefix, site, manifest, manifests, tips, readme, progress)
	}
	if err != nil || pending {
		return pending, "", err
	}
	// The legacy pre-flip front page (timeline.html) is retired; sweep it best-
	// effort on every effective push so a bucket first pushed by a pre-M8 binary
	// stops serving a stale duplicate front page.
	_ = client.Delete(prefix + sitePagesLegacyFrontKey)
	return false, sitePagesStateOn, nil
}

// reclaimSiteFrontPage re-renders and PUTs index.html (the generated front page)
// from the metadata index without reading any bodies — the cheap no-op-push path
// that reclaims index.html after uploadSiteFiles/ensureSiteShell may have written
// the embedded shell over it. The front page needs only subjects/authors/times
// (metadata) plus the code interleave and README, so no thread bodies are read.
func reclaimSiteFrontPage(client *Client, prefix string, site sitePageSite, manifests map[string]*siteShardManifest, readme *siteFrontReadme) error {
	metas := map[string][]sitePageMsg{}
	for ext, m := range manifests {
		entries, err := readSitePagesMeta(client, prefix, ext, m)
		if err != nil {
			return fmt.Errorf("read pages index %s: %w", ext, err)
		}
		metas[ext] = entries
	}
	roots := buildSitePageThreads(metas)
	done := map[string]int{}
	for _, list := range sitePageLists {
		done[list.Ext] = len(roots[list.Ext])
	}
	return writeSiteFrontPage(client, prefix, roots, done, site, readme)
}

// sitePageDefaultTitle derives a fallback site title from the repo's key
// prefix (its last path segment) when no title is configured.
func sitePageDefaultTitle(prefix string) string {
	trimmed := strings.TrimSuffix(prefix, "/")
	if i := strings.LastIndex(trimmed, "/"); i >= 0 {
		trimmed = trimmed[i+1:]
	}
	if trimmed == "" {
		return "repository"
	}
	return trimmed
}

// readSitePagesManifests reads every present extension's items manifest plus
// the code index manifest, returning the gitmsg manifests (the pages read those
// corpora) and the consumed-tip map the pages manifest diffs against (the code
// tip included, so a code-only push refreshes the front page's interleave).
func readSitePagesManifests(client *Client, prefix string, refs map[string]string) (map[string]*siteShardManifest, map[string]string, error) {
	manifests := map[string]*siteShardManifest{}
	tips := map[string]string{}
	for _, ext := range siteItemsExts {
		if _, exists := refs["refs/heads/gitmsg/"+ext]; !exists {
			continue
		}
		m, err := readItemsManifest(client, prefix, ext)
		if err != nil {
			return nil, nil, err
		}
		if m == nil {
			continue
		}
		manifests[ext] = m
		tips[ext] = m.Tip
	}
	code, err := readItemsManifest(client, prefix, siteCodeExt)
	if err != nil {
		return nil, nil, err
	}
	if code != nil {
		tips[siteCodeExt] = code.Tip
	}
	return manifests, tips, nil
}

// sitePagesTipsCurrent reports whether the pages manifest consumed exactly the
// current items-manifest tips (any drift — a moved, added, or removed corpus —
// triggers the incremental pass).
func sitePagesTipsCurrent(m *sitePagesManifest, tips map[string]string) bool {
	if len(m.Ext) != len(tips) {
		return false
	}
	for ext, tip := range tips {
		if m.Ext[ext] != tip {
			return false
		}
	}
	return true
}

// readSiteFrontReadme reads the default branch's README.md through the local
// commit source the push already has (never a bucket GET — a pusher without a
// local repo simply gets no README section), rendered as escaped plain text
// capped at sitePagesReadmeMax with a truncation marker.
func readSiteFrontReadme(src *localCommitSource, defaultBranch string) *siteFrontReadme {
	if defaultBranch == "" {
		return nil
	}
	body, ok := src.object("refs/heads/"+defaultBranch+":README.md", "blob")
	if !ok {
		return nil
	}
	text, truncated := string(body), false
	if len(text) > sitePagesReadmeMax {
		text, truncated = strings.ToValidUTF8(text[:sitePagesReadmeMax], ""), true
	}
	paras := sitePageParas(text)
	if paras == nil {
		return nil
	}
	return &siteFrontReadme{Paras: paras, Truncated: truncated}
}

// generateSitePages runs one budgeted full-regen pass: read back the item
// corpora, assemble threads, then write in the pinned order — pages.css, item
// pages (budgeted, newest-first per extension, cursor resume), list pages,
// front page, sitemap + robots, manifest last (the commit point; every earlier
// write is an idempotent overwrite, so an interrupted pass just redoes the tail).
func generateSitePages(client *Client, prefix string, site sitePageSite, prior *sitePagesManifest, manifests map[string]*siteShardManifest, tips map[string]string, readme *siteFrontReadme, progress Progress) (bool, error) {
	msgs := map[string][]sitePageMsg{}
	for ext, m := range manifests {
		entries, err := readSitePagesCorpus(client, prefix, ext, m)
		if err != nil {
			return false, fmt.Errorf("read pages corpus %s: %w", ext, err)
		}
		msgs[ext] = entries
	}
	roots := buildSitePageThreads(msgs)
	if err := putSitePagesCSS(client, prefix); err != nil {
		return false, err
	}
	done, complete, err := writeSiteItemPages(client, prefix, roots, tips, site, prior, progress)
	if err != nil {
		return false, err
	}
	counts, frontier, err := writeSiteTypeLists(client, prefix, roots, done, complete, site, prior, nil)
	if err != nil {
		return false, err
	}
	if err := writeSiteFrontPage(client, prefix, roots, done, site, readme); err != nil {
		return false, err
	}
	if err := writeSiteSitemap(client, prefix, roots, done, site); err != nil {
		return false, err
	}
	if err := writeSiteFeed(client, prefix, roots, done, site); err != nil {
		return false, err
	}
	if err := writeSiteTypeFeeds(client, prefix, roots, done, site, nil); err != nil {
		return false, err
	}
	if err := writeSiteRobots(client, prefix, site); err != nil {
		return false, err
	}
	manifest := &sitePagesManifest{Version: sitePagesVersion, Ext: tips, SiteHash: sitePageSiteHash(site)}
	if complete {
		manifest.Counts, manifest.Frontier = counts, frontier
	} else {
		manifest.Cursor = &sitePagesCursor{Done: done}
	}
	if err := putSitePagesManifest(client, prefix, manifest); err != nil {
		return false, err
	}
	return !complete, nil
}

// incrementalSitePages processes one push's delta on a complete page set: the
// messages appended past the consumed tips are classified through the same
// thread machinery as the full build — a new top-level item gets its page, a
// reply/edit/retract resolves to its root, whose page is regenerated with
// thread bodies read back only for the affected threads. Only the affected type
// lists (plus the front page, sitemap head and manifest) are rewritten; sealed
// list pages stay immutable. The delta is deliberately unbudgeted — it is
// push-sized by construction, and a corpus whose consumed tip vanished
// (repair/history rewrite) falls back to the budgeted full regeneration.
func incrementalSitePages(client *Client, prefix string, site sitePageSite, prior *sitePagesManifest, manifests map[string]*siteShardManifest, tips map[string]string, readme *siteFrontReadme, progress Progress) (bool, error) {
	metas := map[string][]sitePageMsg{}
	delta := map[string]bool{}
	for ext, m := range manifests {
		entries, err := readSitePagesMeta(client, prefix, ext, m)
		if err != nil {
			return false, fmt.Errorf("read pages index %s: %w", ext, err)
		}
		metas[ext] = entries
		if prior.Ext[ext] == tips[ext] {
			continue
		}
		newer, found := sitePageEntriesSince(entries, prior.Ext[ext])
		if !found {
			return generateSitePages(client, prefix, site, nil, manifests, tips, readme, progress)
		}
		for i := range newer {
			delta[newer[i].Short] = true
		}
	}
	roots := buildSitePageThreads(metas)
	done := map[string]int{}
	for _, list := range sitePageLists {
		done[list.Ext] = len(roots[list.Ext])
	}
	affected := affectedSitePageRoots(roots, delta)
	if err := attachThreadBodies(client, prefix, affected); err != nil {
		return false, err
	}
	listByExt := map[string]sitePageList{}
	for _, l := range sitePageLists {
		listByExt[l.Ext] = l
	}
	affectedDirs := map[string]bool{}
	for i, r := range affected {
		page, err := renderSitePage("item", buildSiteItemPage(r, listByExt[r.Msg.Ext], site))
		if err != nil {
			return false, err
		}
		if err := putSitePage(client, prefix+"i/"+r.Msg.Short+".html", page); err != nil {
			return false, err
		}
		affectedDirs[listByExt[r.Msg.Ext].Dir] = true
		progress.call("site pages", i+1, len(affected))
	}
	counts, frontier, err := writeSiteTypeLists(client, prefix, roots, done, true, site, prior, affectedDirs)
	if err != nil {
		return false, err
	}
	if err := writeSiteFrontPage(client, prefix, roots, done, site, readme); err != nil {
		return false, err
	}
	if len(affected) > 0 {
		if err := writeSiteSitemap(client, prefix, roots, done, site); err != nil {
			return false, err
		}
		if err := writeSiteFeed(client, prefix, roots, done, site); err != nil {
			return false, err
		}
		if err := writeSiteTypeFeeds(client, prefix, roots, done, site, affectedDirs); err != nil {
			return false, err
		}
	}
	manifest := &sitePagesManifest{Version: sitePagesVersion, Ext: tips, Counts: counts, Frontier: frontier, SiteHash: sitePageSiteHash(site)}
	if err := putSitePagesManifest(client, prefix, manifest); err != nil {
		return false, err
	}
	return false, nil
}

// sitePageEntriesSince returns the entries appended after the given consumed
// branch tip (the newest entry of the corpus at consume time; the corpus is
// ingestion-ordered, so everything past it is new). A "" tip means the corpus
// is new since the last pass: everything is new. found is false when the tip is
// no longer in the corpus (repaired or rewritten), making the delta unknowable.
func sitePageEntriesSince(entries []sitePageMsg, tip string) ([]sitePageMsg, bool) {
	if tip == "" {
		return entries, true
	}
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].SHA == tip {
			return entries[i+1:], true
		}
	}
	return nil, false
}

// affectedSitePageRoots maps a delta (message shorts) onto the top-level items
// whose pages must be (re)generated: a delta member may be a root itself, one
// of its replies, or the resolved latest version of either. Delta members with
// no owning root (a reply to a foreign/never-fetched root, a dropped cross-repo
// proposal, a superseded stale edit) change no rendered page and are skipped.
// Returned newest-first for stable write order.
func affectedSitePageRoots(roots map[string][]*sitePageItem, delta map[string]bool) []*sitePageItem {
	owner := map[string]*sitePageItem{}
	for _, rs := range roots {
		for _, r := range rs {
			owner[r.Msg.Short], owner[r.Resolved.Short] = r, r
			for _, rep := range r.Replies {
				owner[rep.Msg.Short], owner[rep.Resolved.Short] = r, r
			}
		}
	}
	var affected []*sitePageItem
	seen := map[*sitePageItem]bool{}
	for sha := range delta {
		if r := owner[sha]; r != nil && !seen[r] {
			seen[r] = true
			affected = append(affected, r)
		}
	}
	sort.Slice(affected, func(i, j int) bool {
		ti, tj := pageEffectiveTime(affected[i].Msg), pageEffectiveTime(affected[j].Msg)
		if ti != tj {
			return ti > tj
		}
		return affected[i].Msg.SHA > affected[j].Msg.SHA
	})
	return affected
}

// deleteSitePages removes the whole HTML page layer — the disable path: pages
// turned off (or site.url removed) while the bucket carries a page set. List +
// Delete over the page namespaces, best-effort per key (failures log and
// continue); the manifest is deleted last and only after a clean sweep, so an
// interrupted deletion retries on the next push. Returns complete=false while
// anything (including the manifest) survives.
//
// index.html is NOT deleted — it is dual-owned: the generated front page while
// the layer is effective, the embedded shell otherwise. On disable this restores
// the embedded shell index.html itself (rather than relying on the ordering with
// uploadSiteFiles/ensureSiteShell, which may skip on a matching shell-version
// marker even though index.html currently holds the generated front page), so
// the flip back to the shell is deterministic. The retired timeline.html front
// key is swept alongside the page namespaces.
func deleteSitePages(client *Client, prefix string) (bool, error) {
	_, exists, err := objectSize(client, prefix+sitePagesManifestKey)
	if err != nil {
		return false, err
	}
	if !exists {
		return true, nil
	}
	clean := true
	remove := func(key string) {
		if err := client.Delete(key); err != nil {
			clean = false
			fmt.Fprintf(os.Stderr, "gitsocial s3: delete %s: %v\n", key, err)
		}
	}
	namespaces := []string{"i/"}
	for _, l := range sitePageLists {
		namespaces = append(namespaces, l.Dir+"/")
	}
	namespaces = append(namespaces, "sitemap-") // sealed parts + the head part
	for _, ns := range namespaces {
		keys, err := client.List(prefix + ns)
		if err != nil {
			clean = false
			fmt.Fprintf(os.Stderr, "gitsocial s3: list %s: %v\n", ns, err)
			continue
		}
		for _, key := range keys {
			remove(key)
		}
	}
	for _, key := range []string{sitePagesLegacyFrontKey, sitePagesCSSKey, sitePagesSitemapKey, sitePagesRobotsKey, sitePagesFeedKey} {
		remove(prefix + key)
	}
	// Restore the embedded shell as index.html (the flip back). Best-effort: a
	// failure keeps the sweep incomplete so the next push retries.
	if err := uploadShellIndexHTML(client, prefix); err != nil {
		clean = false
		fmt.Fprintf(os.Stderr, "gitsocial s3: restore shell index.html: %v\n", err)
	}
	if !clean {
		return false, nil
	}
	if err := client.Delete(prefix + sitePagesManifestKey); err != nil {
		fmt.Fprintf(os.Stderr, "gitsocial s3: delete %s: %v\n", sitePagesManifestKey, err)
		return false, nil
	}
	return true, nil
}

// writeSiteItemPages writes item pages newest-first per extension under the
// per-push budget, resuming from the prior cursor when that extension's
// consumed tip is unchanged (a moved tip resets the extension: the sorted root
// list may have shifted, and page PUTs are overwrite-idempotent).
func writeSiteItemPages(client *Client, prefix string, roots map[string][]*sitePageItem, tips map[string]string, site sitePageSite, prior *sitePagesManifest, progress Progress) (map[string]int, bool, error) {
	done := map[string]int{}
	if prior != nil && prior.Cursor != nil {
		for _, list := range sitePageLists {
			if prior.Ext[list.Ext] == tips[list.Ext] {
				done[list.Ext] = min(prior.Cursor.Done[list.Ext], len(roots[list.Ext]))
			}
		}
	}
	budget := sitePagesBudget
	complete := true
	for _, list := range sitePageLists {
		rs := roots[list.Ext]
		for done[list.Ext] < len(rs) {
			if budget <= 0 {
				complete = false
				break
			}
			it := rs[done[list.Ext]]
			page, err := renderSitePage("item", buildSiteItemPage(it, list, site))
			if err != nil {
				return nil, false, err
			}
			if err := putSitePage(client, prefix+"i/"+it.Msg.Short+".html", page); err != nil {
				return nil, false, err
			}
			done[list.Ext]++
			budget--
			progress.call("site pages "+list.Ext, done[list.Ext], len(rs))
		}
	}
	return done, complete, nil
}

// writeSiteTypeLists writes the type directories' list pages from the roots
// generated so far (retracted roots are hidden per GITMSG §1.5). While the
// bootstrap is incomplete only the mutable head is written (the true oldest
// entries are not known yet, so nothing seals). On a complete set the sealing
// frontier (the manifest's per-dir newest-sealed sha) partitions the roots:
// everything at or below it is already in immutable <n>.html pages (1 = oldest)
// and is never rewritten; the head above it seals its oldest full hundreds into
// new pages as it overflows, advancing the frontier. affected (nil = every dir)
// limits the incremental pass to the dirs whose entries changed; skipped dirs
// carry their prior sealing state through to the returned counts/frontier.
func writeSiteTypeLists(client *Client, prefix string, roots map[string][]*sitePageItem, done map[string]int, complete bool, site sitePageSite, prior *sitePagesManifest, affected map[string]bool) (map[string]int, map[string]string, error) {
	counts := map[string]int{}
	frontier := map[string]string{}
	for _, list := range sitePageLists {
		sealed, sealedSha := 0, ""
		if prior != nil {
			sealed, sealedSha = prior.Counts[list.Dir], prior.Frontier[list.Dir]
		}
		if sealedSha == "" {
			sealed = 0
		}
		if affected != nil && !affected[list.Dir] {
			counts[list.Dir], frontier[list.Dir] = sealed, sealedSha
			continue
		}
		rs := roots[list.Ext][:done[list.Ext]]
		idx := len(rs) // start of the sealed region in the newest-first root list
		if sealedSha != "" {
			idx = -1
			for i, it := range rs {
				if it.Msg.Short == sealedSha {
					idx = i
					break
				}
			}
			if idx < 0 { // frontier vanished (corpus rewrite): recompute the layout
				sealed, sealedSha, idx = 0, "", len(rs)
			}
		}
		head := make([]*sitePageItem, 0, idx)
		for _, it := range rs[:idx] {
			if !it.Retracted {
				head = append(head, it)
			}
		}
		totalVisible := len(head)
		for _, it := range rs[idx:] {
			if !it.Retracted {
				totalVisible++
			}
		}
		var newPages [][]*sitePageItem // oldest-first segments to seal
		if complete {
			for len(head) > sitePagesListSize {
				segment := head[len(head)-sitePagesListSize:]
				newPages = append(newPages, segment)
				sealedSha = segment[0].Msg.Short // the newest entry sealed
				head = head[:len(head)-sitePagesListSize]
			}
		}
		finalSealed := sealed + len(newPages)
		for i, segment := range newPages {
			page, err := renderSitePage("list", buildSiteSealedListPage(list, site, segment, sealed+i+1, finalSealed))
			if err != nil {
				return nil, nil, err
			}
			if err := putSitePage(client, prefix+list.Dir+"/"+strconv.Itoa(sealed+i+1)+".html", page); err != nil {
				return nil, nil, err
			}
		}
		sealed = finalSealed
		page, err := renderSitePage("list", buildSiteListHeadPage(list, site, head, totalVisible, sealed))
		if err != nil {
			return nil, nil, err
		}
		if err := putSitePage(client, prefix+list.Dir+"/index.html", page); err != nil {
			return nil, nil, err
		}
		counts[list.Dir], frontier[list.Dir] = sealed, sealedSha
	}
	return counts, frontier, nil
}

// buildSiteListHeadPage assembles a type's mutable head list page.
func buildSiteListHeadPage(list sitePageList, site sitePageSite, head []*sitePageItem, total, sealed int) siteListPageData {
	entries := make([]sitePageListEntry, 0, len(head))
	openCount := 0
	for _, it := range head {
		entries = append(entries, buildSiteListEntry(it, "../", sitePageDefaultTypes[list.Ext]))
	}
	metaBits := []string{fmt.Sprintf("%d %s", total, list.Label)}
	if list.Ext == "pm" || list.Ext == "review" {
		for _, it := range head {
			if state := pageItemField(it, "state"); state == "" || state == "open" {
				openCount++
			}
		}
		metaBits = append(metaBits, fmt.Sprintf("%d open", openCount))
	}
	metaBits = append(metaBits, "newest first")
	d := siteListPageData{
		Nav:      sitePageNav("../", list.Dir),
		Heading:  list.Label,
		MetaBits: metaBits,
		Entries:  entries,
	}
	if sealed > 0 {
		d.OlderHref = strconv.Itoa(sealed) + ".html"
	}
	d.Chrome = sitePageChrome{
		Title:         list.Label + " · " + site.Title,
		Description:   sitePageDescription(sitePageListDescription(list, site), ""),
		OGTitle:       list.Label + " · " + site.Title,
		SiteTitle:     site.Title,
		Canonical:     site.URL + list.Dir + "/index.html",
		Route:         list.Route,
		Base:          "../",
		Feed:          site.URL + sitePagesFeedKey,
		TypeFeed:      site.URL + siteTypeFeedKey(list),
		TypeFeedTitle: siteTypeFeedTitle(list, site),
	}
	return d
}

// buildSiteSealedListPage assembles one immutable older list page (n = 1 is the
// oldest). The newest sealed page AT SEAL TIME links "← newer" to the head; a
// page that later stops being the newest keeps that link (it still navigates —
// sealed pages are immutable by contract), while the older→ chain from the head
// always covers every page.
func buildSiteSealedListPage(list sitePageList, site sitePageSite, pageEntries []*sitePageItem, n, sealed int) siteListPageData {
	entries := make([]sitePageListEntry, 0, len(pageEntries))
	for _, it := range pageEntries {
		entries = append(entries, buildSiteListEntry(it, "../", sitePageDefaultTypes[list.Ext]))
	}
	d := siteListPageData{
		Nav:      sitePageNav("../", list.Dir),
		Heading:  list.Label,
		MetaBits: []string{fmt.Sprintf("%d %s", len(entries), list.Label), fmt.Sprintf("older page %d", n)},
		Entries:  entries,
	}
	if n == sealed {
		d.NewerHref = "index.html"
	} else {
		d.NewerHref = strconv.Itoa(n+1) + ".html"
	}
	if n > 1 {
		d.OlderHref = strconv.Itoa(n-1) + ".html"
	}
	d.Chrome = sitePageChrome{
		Title:         fmt.Sprintf("%s · page %d · %s", list.Label, n, site.Title),
		Description:   sitePageDescription(sitePageListDescription(list, site), ""),
		OGTitle:       fmt.Sprintf("%s · page %d · %s", list.Label, n, site.Title),
		SiteTitle:     site.Title,
		Canonical:     site.URL + list.Dir + "/" + strconv.Itoa(n) + ".html",
		Route:         list.Route,
		Base:          "../",
		Feed:          site.URL + sitePagesFeedKey,
		TypeFeed:      site.URL + siteTypeFeedKey(list),
		TypeFeedTitle: siteTypeFeedTitle(list, site),
	}
	return d
}

// sitePageListDescription words a type list's meta description.
func sitePageListDescription(list sitePageList, site sitePageSite) string {
	return strings.ToUpper(list.Label[:1]) + list.Label[1:] + " of " + site.Title + ", newest first."
}

// siteFrontEntry pairs a rendered front-page row with its sort key.
type siteFrontEntry struct {
	entry sitePageListEntry
	ts    int64
	sha   string
}

// writeSiteFrontPage writes the timeline front page: the newest entries across
// the generated item roots (memo excluded, mirroring the shell's timeline) and
// the newest code commits from the code items index, interleaved by time, then
// the default branch's README as escaped text (feed first; indexing is
// position-independent). Code entries link into the app — they get no pages.
func writeSiteFrontPage(client *Client, prefix string, roots map[string][]*sitePageItem, done map[string]int, site sitePageSite, readme *siteFrontReadme) error {
	var merged []siteFrontEntry
	totalItems := 0
	for _, list := range sitePageLists {
		for _, it := range roots[list.Ext][:done[list.Ext]] {
			if it.Retracted {
				continue
			}
			totalItems++
			if list.Ext == "memo" {
				continue
			}
			merged = append(merged, siteFrontEntry{entry: buildSiteListEntry(it, "./", ""), ts: pageEffectiveTime(it.Msg), sha: it.Msg.SHA})
		}
	}
	code, err := readSiteFrontCodeEntries(client, prefix, sitePagesFrontSize)
	if err != nil {
		return err
	}
	for _, e := range code {
		merged = append(merged, siteFrontEntry{entry: buildSiteCodeEntry(e, site), ts: e.TS, sha: e.SHA})
	}
	sort.Slice(merged, func(i, j int) bool {
		if merged[i].ts != merged[j].ts {
			return merged[i].ts > merged[j].ts
		}
		return merged[i].sha > merged[j].sha
	})
	if len(merged) > sitePagesFrontSize {
		merged = merged[:sitePagesFrontSize]
	}
	entries := make([]sitePageListEntry, 0, len(merged))
	var newest int64
	for _, m := range merged {
		entries = append(entries, m.entry)
		if m.ts > newest {
			newest = m.ts
		}
	}
	var metaBits []string
	if site.Description != "" {
		metaBits = append(metaBits, site.Description)
	}
	metaBits = append(metaBits, fmt.Sprintf("%d items", totalItems))
	if newest > 0 {
		metaBits = append(metaBits, "updated "+sitePageDate(newest))
	}
	description := site.Description
	if description == "" {
		description = "Timeline of " + site.Title + ": issues, pull requests, posts, releases."
	}
	d := siteListPageData{
		Nav:      sitePageNav("./", ""),
		Heading:  site.Title,
		MetaBits: metaBits,
		Entries:  entries,
		Readme:   readme,
	}
	d.Chrome = sitePageChrome{
		Title:       site.Title,
		Description: sitePageDescription(description, site.Title),
		OGTitle:     site.Title,
		SiteTitle:   site.Title,
		// Post-flip the front page IS index.html, and its canonical/clean URL is
		// the site root itself (matching the sitemap's root entry), not the
		// index.html filename.
		Canonical: site.URL,
		// The front page is the README home view (parseRoute maps "/" to home);
		// stamping /timeline here would boot the upgraded app into the feed over
		// the README the static page shows.
		Route: "/",
		Base:  "./",
		Feed:  site.URL + sitePagesFeedKey,
	}
	page, err := renderSitePage("front", d)
	if err != nil {
		return err
	}
	return putSitePage(client, prefix+sitePagesFrontKey, page)
}

// readSiteFrontCodeEntries returns the newest code items (newest-first, up to
// limit) from the code items index: the head, then newest sealed shards. Empty
// (no error) when the code index is absent.
func readSiteFrontCodeEntries(client *Client, prefix string, limit int) ([]siteMetaEntry, error) {
	m, err := readItemsManifest(client, prefix, siteCodeExt)
	if err != nil || m == nil {
		return nil, err
	}
	head, err := readItemsHeadEntries(client, prefix+siteItemsHeadKey(siteCodeExt))
	if err != nil {
		return nil, err
	}
	out := reverseGeneric(head)
	for i := len(m.Shards) - 1; i >= 0 && len(out) < limit; i-- {
		entries, err := readItemsHeadEntries(client, prefix+siteItemsDir(siteCodeExt)+m.Shards[i].Key)
		if err != nil {
			return nil, err
		}
		out = append(out, reverseGeneric(entries)...)
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// site_pages.go - push-maintained static HTML page layer (the website's
// human-readable pages), generated at the prefix root next to the shell:
//
//   i/<shorthash>.html      one page per top-level gitmsg item, thread inlined
//   issues/ prs/ posts/ releases/ memos/
//                           per-type list pages (mutable index.html head +
//                           immutable sealed <n>.html, chained "older →")
//   pages.css               the pages' shared stylesheet (their only subresource,
//                           plus the EB Garamond woff2 it pulls from the shell)
//   index.html              the generated front page (the app's home landing +
//                           PE hooks + gs-upgrade.js) — the entry flip: when the
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
// objects/, refs/, .gitsocial/), the shell files (index.html, gs-*.js,
// icons.js, prism.js, grammars/), and the release artifact objects
// (artifacts/, owned by `release artifacts push` — see artifacts.go) — all
// disjoint by construction.

package objstore

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"html/template"
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
	// v3: script-src gains 'unsafe-eval' in every page's CSP meta (without it
	// the booted app's lazy grammar loader is blocked and only base-bundle
	// languages highlight).
	// v4: CSP gains media-src 'self' blob: so the booted app's file view can
	// play in-bucket mp4/webm blobs as inline <video> (without it <video src=blob:>
	// falls back to default-src 'self' and the browser rejects the format).
	// v5: the front page became the app's home landing (branch strip, root file
	// listing, README, recent activity) and pages.css grew the rules those
	// elements need. A one-time heal for buckets stamped by a v4 binary, which
	// shipped that CSS only on a full regen; the stylesheet now goes out on every
	// pass (rebuildSitePages), so a later CSS edit needs no bump.
	// v6: every head declares a favicon. Without one the browser asks the ORIGIN
	// root for /favicon.ico, which under a bucket prefix is a key outside the
	// site that no push can ever make resolve — a 404 (a ~27 KB error body on
	// R2) on every page view. Buckets stamped by a v5 binary need the regen.
	// v7: every head carries the boot script and the `.gs-boot` rules that let
	// the upgrade show a loading state instead of static content it is about to
	// replace. Both live in the inlined head, so a v6 bucket keeps swapping the
	// page under the reader until it is regenerated.
	// v8: a cancel state counts as closed in both its spellings
	// (sitePageStateClass), so a milestone or sprint carrying the doubled-l form
	// GITPM.md words it in stops rendering its chip open-green. The front page is
	// rewritten on every pass, but item pages and SEALED list pages are not — a
	// sealed page is immutable by contract, so a v7 bucket would keep serving the
	// wrong chip there forever.
	// v9: the commits list layer (commits/index.html + sealed commits/<n>.html,
	// site_pages_commits.go). The bump is not for the new directory — a v8 bucket
	// would grow it on its next pass anyway — but for the nav: EVERY generated
	// page's nav now carries the commits link, and item pages and sealed list
	// pages are never rewritten outside a full regen, so without it a v8 bucket's
	// existing pages would link a five-way nav forever and the new dir would be
	// reachable only from the pages written after it.
	// v10: the pages adopt the app's layout metrics (the 1012px shell, the 220px
	// nav column, 20px/1.4 type, the 720px breakpoint) in the INLINED head CSS,
	// so the front page, which now stays on screen while the shell downloads,
	// is swapped for the app's render without the content column moving or the
	// text re-wrapping. The geometry is inlined rather than left to pages.css so
	// a failed stylesheet fetch cannot reintroduce the shift, which puts it in
	// every page's head: item pages and sealed list pages are never rewritten
	// outside a full regen, so without the bump a bucket would serve two
	// different layouts depending on when each page was written.
	// v11: a retracted item's chip class is its own `retracted` (the app's
	// danger-tinted treatment) instead of reusing `code`, which read as a muted
	// grey commit chip. Item pages and sealed list pages are never rewritten
	// outside a full regen, so a v10 bucket would keep serving grey retracted
	// chips forever without the bump.
	// v12: the pages adopt the app's visual vocabulary in the frozen per-page
	// layer: the boot script stamps the app's stored theme choice (localStorage
	// "theme") on <html> so pages.css can hold a chosen theme with no flip at
	// boot; list rows (type lists + commits) render the app's .card markup
	// instead of bare ol.items li; chips carry the app's classes (.chip.state
	// fills, .chip.reviewer-chip verdicts, .chip.chip-retracted, "prerelease");
	// and the thread truncation marker takes the shared "… truncated" wording.
	// All of it lives in heads and rows that item pages and sealed list pages
	// keep forever without the bump, serving pre-app markup a pages.css that no
	// longer styles it.
	sitePagesVersion = 12
	// sitePagesListSize is one list page's entry count.
	sitePagesListSize = 100
	// sitePagesFeedSize is the Atom feeds' entry count.
	sitePagesFeedSize = 50
	// sitePagesReadmeMax caps the front page's inlined README bytes.
	sitePagesReadmeMax = 8 * 1024
	// sitePagesHomeFiles caps the front page's root file listing, mirroring the
	// app's HOME_FILE_LIMIT (gs-render.js homeFileList) so the static rows and
	// the upgraded render are the same rows.
	sitePagesHomeFiles = 3
	// sitePagesHomeActivity caps the front page's recent-activity rows, mirroring
	// the app's HOME_ACTIVITY_LIMIT (gs-core.js loadHomeActivity). Ten: a round
	// number short enough that the section reads as a summary below the README
	// rather than a scrolling log. There is no per-type quota — the newest ten
	// entries are whatever they are, so a repo that commits daily can legitimately
	// show ten code commits and no item rows.
	sitePagesHomeActivity = 10
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
	Ext      map[string]string `json:"ext"`                // per-extension items-manifest tip consumed ("code" included: the front page shows the default branch's tip)
	Cursor   *sitePagesCursor  `json:"cursor,omitempty"`   // present while the bootstrap is incomplete
	Counts   map[string]int    `json:"counts,omitempty"`   // sealed list pages per type dir
	Frontier map[string]string `json:"frontier,omitempty"` // per type dir: sha12 of the newest sealed list entry (sealing boundary)
	Commits  *siteCommitsState `json:"commits,omitempty"`  // the commits list's published pagination (site_pages_commits.go)
	SiteHash string            `json:"siteHash,omitempty"` // hash of the site identity (title/url/description) stamped into every page
}

// sitePagesCommitsPending reports whether the commits layer still owes sealing
// work a later push must finish (the per-push page budget cut it short).
func sitePagesCommitsPending(m *sitePagesManifest) bool {
	return m != nil && m.Commits != nil && m.Commits.Pending
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
// nothing here carries a Content-Encoding. These are the documents fetched
// directly by whatever asked for the URL — the HTML pages, the sitemap, robots,
// the feed — and a bucket serves a stored encoding to every client regardless of
// its Accept-Encoding. The page layer exists to be legible to those clients, so
// it trades their bytes for their comprehension; the stylesheet they never fetch
// goes through putSiteAsset instead.
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

// putSitePagesCSS uploads the shared stylesheet — rendered with the pushed
// config's accent baked in (sitePagesCSSFor) — written before any page so no
// page ever references a missing subresource. Brotli-stored like the shell's own
// stylesheet (putSiteAsset): a <link> subresource is fetched only by a browser
// rendering the page, never by the scrapers the pages themselves must stay
// plain for. An accent change needs no page regen: this PUT happens on every
// effective pass and pages.css is a no-cache key.
func putSitePagesCSS(client *Client, prefix string, cfg siteCustomization) error {
	return putSiteAsset(client, prefix+sitePagesCSSKey, sitePagesCSSKey, sitePagesCSSFor(cfg))
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

// sitePageSiteFor assembles the site identity every page stamps. A relative
// site.image key resolves against the effective base URL here, so every
// consumer sees the absolute og:image URL.
func sitePageSiteFor(prefix string, cfg siteCustomization, url string) sitePageSite {
	site := sitePageSite{Title: cfg.Title, URL: url, Description: cfg.Description, Image: cfg.Image, Icon: sitePageIcon(cfg.Favicon)}
	if site.Image != "" && !strings.Contains(site.Image, "://") {
		site.Image = url + site.Image
	}
	if site.Title == "" {
		site.Title = sitePageDefaultTitle(prefix)
	}
	return site
}

// sitePageSiteHash fingerprints the site identity baked into every rendered
// page (title, canonical base, description, og:image, favicon); a change
// regenerates everything.
func sitePageSiteHash(site sitePageSite) string {
	h := sha256.Sum256([]byte(site.Title + "\x00" + site.URL + "\x00" + site.Description + "\x00" + site.Image + "\x00" + string(site.Icon)))
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
func sitePagesState(client *Client, prefix string, refs map[string]string, ov SiteOverride, src *localCommitSource) (state string, pending bool) {
	cfg, ok, err := readSiteCustomization(client, prefix, refs, ov, src)
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
	if err != nil || manifest == nil || manifest.Cursor != nil || sitePagesCommitsPending(manifest) {
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
	cfg, ok, err := readSiteCustomization(client, prefix, refs, ov, src)
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
	// The stylesheet ships on EVERY effective pass, not just the full regen. It is
	// one small idempotent PUT next to the index.html this pass writes anyway, and
	// gating it behind the full-regen path made an edit to it invisible: a bucket
	// whose page set was already current kept serving the previous binary's CSS
	// indefinitely, so a rule added for a new page element never arrived and its
	// markup rendered unstyled. Cheap and unconditional beats correct-in-theory.
	if err := putSitePagesCSS(client, prefix, cfg); err != nil {
		return false, "", err
	}
	home := readSiteFrontHome(src, site, refs, defaultBranch)
	switch {
	case manifest != nil && manifest.Cursor == nil && sitePagesTipsCurrent(manifest, tips) && !sitePagesCommitsPending(manifest):
		// Nothing any page derives from moved: the page set is current. But the
		// front page IS index.html since the entry flip, and this same push's
		// uploadSiteFiles/ensureSiteShell may have just (re)uploaded the embedded
		// shell over it (a shell-version bump, or any non-pages ref move that
		// un-skips maintenance). Reclaim index.html deterministically — the pages
		// maintainer owns it whenever the layer is effective — from the cheap
		// metadata index (no bodies read).
		err = reclaimSiteFrontPage(client, prefix, site, manifests, home)
	case manifest != nil && manifest.Cursor == nil:
		pending, err = incrementalSitePages(client, prefix, site, manifest, manifests, tips, defaultBranch, home, progress)
	default:
		pending, err = generateSitePages(client, prefix, site, manifest, manifests, tips, defaultBranch, home, progress)
	}
	if err != nil || pending {
		return pending, "", err
	}
	// The legacy pre-flip front page (timeline.html) is retired; sweep it best-
	// effort on every effective push so a bucket first pushed by an older binary
	// stops serving a stale duplicate front page.
	_ = client.Delete(prefix + sitePagesLegacyFrontKey)
	return false, sitePagesStateOn, nil
}

// reclaimSiteFrontPage re-renders and PUTs index.html (the generated front page)
// from the metadata index without reading any bodies — the cheap no-op-push path
// that reclaims index.html after uploadSiteFiles/ensureSiteShell may have written
// the embedded shell over it. The recent-activity section needs only
// subjects/authors/times (metadata), so no thread bodies are read.
func reclaimSiteFrontPage(client *Client, prefix string, site sitePageSite, manifests map[string]*siteShardManifest, home *siteFrontHome) error {
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
	return writeSiteFrontPage(client, prefix, roots, done, site, home)
}

// readSiteFrontCodeEntries returns the newest code items (newest-first, up to
// limit) from the code items index: the head, then newest sealed shards. Empty
// (no error) when the code index is absent — the same condition under which the
// app's own activity section shows no code rows, so the two stay in agreement.
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

// readSiteFrontHome reads the front page's body through the local commit source
// the push already has (never a bucket GET — a pusher without a local repo
// simply gets a thinner front page): the default branch strip, its root file
// listing and its README. This mirrors what the booted app renders on the home
// route (gs-render.js homeView), so the page-entry upgrade replaces the body
// with the same content rather than a different page.
//
// Every part of the body reads from ONE source of truth: the BUCKET's tip for
// the default branch. The local ref of the same name is not it — `gitsocial site
// push` pushes no data of its own, so a local branch ahead of the bucket would
// list files the bucket cannot serve behind app links that resolve to nothing,
// and a bucket HEAD naming a branch this checkout lacks would drop the listing
// even though the bucket carries it. The local odb is only the reader for that
// sha; a miss thins the page (no files, no README) instead of misreporting it.
func readSiteFrontHome(src *localCommitSource, site sitePageSite, refs map[string]string, defaultBranch string) *siteFrontHome {
	if defaultBranch == "" {
		return nil
	}
	// The count the app's chip shows: every refs/heads/* the bucket carries, plus
	// the default branch when the listing has not caught up with it (listBranches
	// adds it to the same set).
	branches := 0
	for ref := range refs {
		if strings.HasPrefix(ref, "refs/heads/") {
			branches++
		}
	}
	if _, ok := refs[localBranchRef(defaultBranch)]; !ok {
		branches++
	}
	label := " branches"
	if branches == 1 {
		label = " branch"
	}
	tip := refs[localBranchRef(defaultBranch)]
	if len(tip) < 12 { // no usable bucket tip: the strip stands alone
		tip = ""
	}
	home := &siteFrontHome{
		Branch:       defaultBranch,
		Branches:     strconv.Itoa(branches) + label,
		BranchesHref: sitePageAppURL(site, "/branches"),
		Latest:       readSiteFrontLatest(src, site, tip, defaultBranch),
	}
	entries := readSiteRootTree(src, tip)
	home.Files, home.MoreHref, home.MoreLabel = buildSiteFrontFiles(entries, site, defaultBranch)
	home.Readme = readSiteFrontReadme(src, tip, siteReadmeName(entries), defaultBranch, site)
	return home
}

// localBranchRef names a branch's full ref.
func localBranchRef(branch string) string { return "refs/heads/" + branch }

// readSiteFrontLatest reads the default branch's tip commit for the front page's
// meta strip. nil when the sha is unknown or the commit is not readable locally.
func readSiteFrontLatest(src *localCommitSource, site sitePageSite, sha, branch string) *siteFrontCommit {
	if len(sha) < 12 {
		return nil
	}
	body, ok := src.commit(sha)
	if !ok {
		return nil
	}
	c, err := parseBucketCommit(sha, body)
	if err != nil {
		return nil
	}
	short := sha[:12]
	return &siteFrontCommit{
		Subject: subjectOf(c.item.Message),
		Date:    sitePageDate(c.item.TS),
		Short:   short,
		Href:    sitePageAppURL(site, "commit:"+short+"@"+branch),
	}
}

// siteTreeEntry is one parsed root-tree row (the front page needs no shas).
type siteTreeEntry struct {
	Name  string
	IsDir bool
}

// readSiteRootTree reads and parses the root tree of the bucket tip's commit
// (tip = "" when the bucket has no usable tip for the default branch). Empty
// (never an error) when the odb has no local copy of that commit, which just
// thins the front page.
func readSiteRootTree(src *localCommitSource, tip string) []siteTreeEntry {
	if tip == "" {
		return nil
	}
	body, ok := src.object(tip+"^{tree}", "tree")
	if !ok {
		return nil
	}
	var entries []siteTreeEntry
	for i := 0; i < len(body); {
		sep := bytes.IndexByte(body[i:], 0)
		if sep < 0 || i+sep+21 > len(body) {
			break
		}
		mode, name, found := strings.Cut(string(body[i:i+sep]), " ")
		if !found {
			break
		}
		entries = append(entries, siteTreeEntry{Name: name, IsDir: mode == "40000"})
		i += sep + 21
	}
	return entries
}

// siteReadmeName picks the root README the app's findReadme would pick (same
// candidate names, same precedence, case-insensitive), or "" when there is none.
func siteReadmeName(entries []siteTreeEntry) string {
	for _, want := range []string{"readme.md", "readme", "readme.markdown", "readme.txt"} {
		for _, e := range entries {
			if !e.IsDir && strings.EqualFold(e.Name, want) {
				return e.Name
			}
		}
	}
	return ""
}

// readSiteFrontReadme reads one root README blob from the bucket tip's tree and
// RENDERS it (site_markdown.go), capped at sitePagesReadmeMax with a truncation
// marker. Rendering is what makes the served document say what the project is:
// a README is the one file reliably full of markdown and raw HTML, so as escaped
// plain text the front page's first indexable content was its own markup.
//
// The cap is applied to the SOURCE and pulled back to the last line boundary, so
// the renderer is never handed a half-written line; the renderer itself closes
// whatever the cut left open, so the page is well-formed either way.
func readSiteFrontReadme(src *localCommitSource, tip, name, branch string, site sitePageSite) *siteFrontReadme {
	if tip == "" || name == "" {
		return nil
	}
	body, ok := src.object(tip+":"+name, "blob")
	if !ok {
		return nil
	}
	text, truncated := siteMDTruncateSource(string(body), sitePagesReadmeMax)
	rendered := renderSiteMarkdown(text, siteMarkdownContext{AppBase: sitePageAppURL(site, ""), Branch: branch})
	if rendered == "" {
		return nil
	}
	return &siteFrontReadme{HTML: template.HTML(rendered), Truncated: truncated}
}

// buildSiteFrontFiles renders the root listing the app's homeFileList shows:
// directories first then files, each group ordered case-insensitively, capped at
// sitePagesHomeFiles rows behind the same "Show all N" control (a link into the
// app's code browser, since the rows themselves are app routes — files get no
// pages of their own).
func buildSiteFrontFiles(entries []siteTreeEntry, site sitePageSite, branch string) (files []siteFrontFile, moreHref, moreLabel string) {
	ordered := append([]siteTreeEntry(nil), entries...)
	sort.SliceStable(ordered, func(i, j int) bool {
		a, b := ordered[i], ordered[j]
		if a.IsDir != b.IsDir {
			return a.IsDir
		}
		la, lb := strings.ToLower(a.Name), strings.ToLower(b.Name)
		if la != lb {
			return la < lb
		}
		return a.Name > b.Name // the app's localeCompare orders lowercase first
	})
	shown := ordered
	if len(shown) > sitePagesHomeFiles {
		shown = shown[:sitePagesHomeFiles]
		moreHref = sitePageAppURL(site, "/code")
		moreLabel = "Show all " + strconv.Itoa(len(ordered))
	}
	for _, e := range shown {
		files = append(files, siteFrontFile{Name: e.Name, Href: sitePageAppURL(site, "file:"+e.Name+"@"+branch)})
	}
	return files, moreHref, moreLabel
}

// generateSitePages runs one budgeted full-regen pass: read back the item
// corpora, assemble threads, then write in the pinned order — item pages
// (budgeted, newest-first per extension, cursor resume), list pages, front page,
// sitemap + robots, manifest last (the commit point; every earlier write is an
// idempotent overwrite, so an interrupted pass just redoes the tail). The
// stylesheet is already on the bucket: rebuildSitePages ships it on every pass,
// before any page can reference it.
func generateSitePages(client *Client, prefix string, site sitePageSite, prior *sitePagesManifest, manifests map[string]*siteShardManifest, tips map[string]string, defaultBranch string, home *siteFrontHome, progress Progress) (bool, error) {
	msgs := map[string][]sitePageMsg{}
	for ext, m := range manifests {
		entries, err := readSitePagesCorpus(client, prefix, ext, m)
		if err != nil {
			return false, fmt.Errorf("read pages corpus %s: %w", ext, err)
		}
		msgs[ext] = entries
	}
	roots := buildSitePageThreads(msgs)
	done, complete, budget, err := writeSiteItemPages(client, prefix, roots, tips, site, prior, progress)
	if err != nil {
		return false, err
	}
	counts, frontier, err := writeSiteTypeLists(client, prefix, roots, done, complete, site, prior, nil)
	if err != nil {
		return false, err
	}
	// The commits layer reads the code corpus, not the gitmsg ones, so a full
	// regen re-derives it from scratch (prior = nil): a version bump or a site
	// identity change is exactly when the sealed chain must be rebuilt anyway.
	commits, err := maintainSiteCommitPages(client, prefix, site, nil, defaultBranch, complete, budget)
	if err != nil {
		return false, err
	}
	if err := writeSiteFrontPage(client, prefix, roots, done, site, home); err != nil {
		return false, err
	}
	if err := writeSiteSitemap(client, prefix, roots, done, site, commits); err != nil {
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
	manifest := &sitePagesManifest{Version: sitePagesVersion, Ext: tips, Commits: commits, SiteHash: sitePageSiteHash(site)}
	if complete {
		manifest.Counts, manifest.Frontier = counts, frontier
	} else {
		manifest.Cursor = &sitePagesCursor{Done: done}
	}
	if err := putSitePagesManifest(client, prefix, manifest); err != nil {
		return false, err
	}
	return !complete || commits.Pending, nil
}

// maintainSiteCommitPages runs the commits list layer for one pass: read the
// default-branch slice of the code index, then write the head and whatever the
// budget lets it seal. Sealing additionally waits on the CODE index being
// complete — while that corpus is still bootstrapping its older history, today's
// oldest row is not the oldest row, and page 1 must be the oldest hundred
// forever.
func maintainSiteCommitPages(client *Client, prefix string, site sitePageSite, prior *siteCommitsState, defaultBranch string, itemsComplete bool, budget int) (*siteCommitsState, error) {
	entries, codeComplete, err := readSiteCommitEntries(client, prefix, defaultBranch)
	if err != nil {
		return nil, err
	}
	return writeSiteCommitPages(client, prefix, entries, defaultBranch, itemsComplete && codeComplete, site, prior, budget)
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
func incrementalSitePages(client *Client, prefix string, site sitePageSite, prior *sitePagesManifest, manifests map[string]*siteShardManifest, tips map[string]string, defaultBranch string, home *siteFrontHome, progress Progress) (bool, error) {
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
			return generateSitePages(client, prefix, site, nil, manifests, tips, defaultBranch, home, progress)
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
	// The commits layer works off the code corpus, so it is dirty on exactly three
	// signals: the code tip moved, the default branch changed under it, or the
	// budget left it mid-seal. Clean means the pass reads no code shard at all and
	// carries the published state through unchanged.
	commits := prior.Commits
	if commits == nil || commits.Pending || commits.Branch != defaultBranch || prior.Ext[siteCodeExt] != tips[siteCodeExt] {
		budget := max(0, sitePagesBudget-len(affected))
		commits, err = maintainSiteCommitPages(client, prefix, site, prior.Commits, defaultBranch, true, budget)
		if err != nil {
			return false, err
		}
	}
	if err := writeSiteFrontPage(client, prefix, roots, done, site, home); err != nil {
		return false, err
	}
	commitsMoved := prior.Commits == nil || *prior.Commits != *commits
	if len(affected) > 0 || commitsMoved {
		if err := writeSiteSitemap(client, prefix, roots, done, site, commits); err != nil {
			return false, err
		}
	}
	if len(affected) > 0 {
		if err := writeSiteFeed(client, prefix, roots, done, site); err != nil {
			return false, err
		}
		if err := writeSiteTypeFeeds(client, prefix, roots, done, site, affectedDirs); err != nil {
			return false, err
		}
	}
	manifest := &sitePagesManifest{Version: sitePagesVersion, Ext: tips, Counts: counts, Frontier: frontier, Commits: commits, SiteHash: sitePageSiteHash(site)}
	if err := putSitePagesManifest(client, prefix, manifest); err != nil {
		return false, err
	}
	return commits.Pending, nil
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
	namespaces := []string{"i/", siteCommitsDir + "/"}
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
// list may have shifted, and page PUTs are overwrite-idempotent). It returns
// what is LEFT of the budget, which the commits layer seals against — item pages
// come first because a bucket without them has no crawlable items at all.
func writeSiteItemPages(client *Client, prefix string, roots map[string][]*sitePageItem, tips map[string]string, site sitePageSite, prior *sitePagesManifest, progress Progress) (map[string]int, bool, int, error) {
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
				return nil, false, 0, err
			}
			if err := putSitePage(client, prefix+"i/"+it.Msg.Short+".html", page); err != nil {
				return nil, false, 0, err
			}
			done[list.Ext]++
			budget--
			progress.call("site pages "+list.Ext, done[list.Ext], len(rs))
		}
	}
	return done, complete, budget, nil
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

// siteChainedListPage assembles the shape every list page shares: the nav, the
// heading, the rows, and the chain that links a mutable head to its sealed
// pages. n = 0 is the head; n >= 1 is a sealed page (1 = oldest).
//
// The chain is the reason this has one owner rather than one per list kind. The
// newest sealed page AT SEAL TIME links "← newer" to the head and keeps that
// link once it stops being newest (sealed pages are immutable by contract, and
// the link still navigates), while the older→ chain from the head always covers
// every page. Two copies of that rule drift silently, and a drifted chain is a
// crawler walking into a page that links nowhere. Callers own what actually
// differs between the kinds: the row builder, the meta line, and the chrome.
func siteChainedListPage(dir, label string, entries []sitePageListEntry, metaBits []string, n, sealed int) siteListPageData {
	d := siteListPageData{
		Nav:      sitePageNav("../", dir),
		Heading:  label,
		MetaBits: metaBits,
		Entries:  entries,
	}
	if n == 0 {
		if sealed > 0 {
			d.OlderHref = strconv.Itoa(sealed) + ".html"
		}
		return d
	}
	if n == sealed {
		d.NewerHref = "index.html"
	} else {
		d.NewerHref = strconv.Itoa(n+1) + ".html"
	}
	if n > 1 {
		d.OlderHref = strconv.Itoa(n-1) + ".html"
	}
	return d
}

// buildSiteListHeadPage assembles a type's mutable head list page.
func buildSiteListHeadPage(list sitePageList, site sitePageSite, head []*sitePageItem, total, sealed int) siteListPageData {
	entries := make([]sitePageListEntry, 0, len(head))
	for _, it := range head {
		entries = append(entries, buildSiteListEntry(it, "../", sitePageDefaultTypes[list.Ext]))
	}
	metaBits := []string{fmt.Sprintf("%d %s", total, list.Label)}
	if list.Ext == "pm" || list.Ext == "review" {
		openCount := 0
		for _, it := range head {
			if state := pageItemField(it, "state"); state == "" || state == "open" {
				openCount++
			}
		}
		metaBits = append(metaBits, fmt.Sprintf("%d open", openCount))
	}
	metaBits = append(metaBits, "newest first")
	d := siteChainedListPage(list.Dir, list.Label, entries, metaBits, 0, sealed)
	d.Chrome = sitePageChrome{
		Title:         list.Label + " · " + site.Title,
		Description:   sitePageDescription(sitePageListDescription(list, site), ""),
		OGTitle:       list.Label + " · " + site.Title,
		SiteTitle:     site.Title,
		Canonical:     site.URL + list.Dir + "/index.html",
		Route:         list.Route,
		Base:          "../",
		Image:         site.Image,
		Icon:          site.Icon,
		Feed:          site.URL + sitePagesFeedKey,
		TypeFeed:      site.URL + siteTypeFeedKey(list),
		TypeFeedTitle: siteTypeFeedTitle(list, site),
	}
	return d
}

// buildSiteSealedListPage assembles one immutable older list page (n = 1 is the
// oldest); siteChainedListPage owns the newer/older chain.
func buildSiteSealedListPage(list sitePageList, site sitePageSite, pageEntries []*sitePageItem, n, sealed int) siteListPageData {
	entries := make([]sitePageListEntry, 0, len(pageEntries))
	for _, it := range pageEntries {
		entries = append(entries, buildSiteListEntry(it, "../", sitePageDefaultTypes[list.Ext]))
	}
	metaBits := []string{fmt.Sprintf("%d %s", len(entries), list.Label), fmt.Sprintf("older page %d", n)}
	d := siteChainedListPage(list.Dir, list.Label, entries, metaBits, n, sealed)
	d.Chrome = sitePageChrome{
		Title:         fmt.Sprintf("%s · page %d · %s", list.Label, n, site.Title),
		Description:   sitePageDescription(sitePageListDescription(list, site), ""),
		OGTitle:       fmt.Sprintf("%s · page %d · %s", list.Label, n, site.Title),
		SiteTitle:     site.Title,
		Canonical:     site.URL + list.Dir + "/" + strconv.Itoa(n) + ".html",
		Route:         list.Route,
		Base:          "../",
		Image:         site.Image,
		Icon:          site.Icon,
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

// writeSiteFrontPage writes the front page: the repo landing the booted app
// renders on the home route (branch strip, root file listing, README, then the
// recent-activity rows), from readSiteFrontHome's local-odb read plus the item
// roots. index.html is dual-owned and the page-entry upgrade replaces this body
// with the app's own home render, so the two carry the same content in the same
// order — the upgrade swaps nothing in.
func writeSiteFrontPage(client *Client, prefix string, roots map[string][]*sitePageItem, done map[string]int, site sitePageSite, home *siteFrontHome) error {
	code, err := readSiteFrontCodeEntries(client, prefix, sitePagesHomeActivity)
	if err != nil {
		return err
	}
	var metaBits []string
	if site.Description != "" {
		metaBits = append(metaBits, site.Description)
	}
	description := site.Description
	if description == "" {
		description = site.Title + ": code, issues, pull requests, posts and releases."
	}
	d := siteFrontPageData{
		Nav:      sitePageNav("./", ""),
		Heading:  site.Title,
		MetaBits: metaBits,
		Home:     home,
		Activity: buildSiteFrontActivity(roots, done, code, site),
	}
	if len(d.Activity) > 0 {
		d.ActivityMoreHref, d.ActivityMoreLabel = siteActivityMoreKey, siteActivityMoreLabel
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
		// The front page IS the app's home view (parseRoute maps "/" to home);
		// stamping /timeline here would boot the upgraded app into the feed over
		// the landing the static page shows.
		Route: "/",
		Base:  "./",
		Image: site.Image,
		Icon:  site.Icon,
		Feed:  site.URL + sitePagesFeedKey,
	}
	page, err := renderSitePage("front", d)
	if err != nil {
		return err
	}
	return putSitePage(client, prefix+sitePagesFrontKey, page)
}

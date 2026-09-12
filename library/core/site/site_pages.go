// site_pages.go - the static HTML page layer: its manifest, budget, and the regen, incremental and disable passes

package site

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"html/template"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/gitsocial-org/gitsocial/library/core/objstore"
)

const (
	// sitePagesManifestKey tracks the generated page set: schema version, the
	// per-extension items-manifest tips consumed, the bootstrap cursor while
	// incomplete, and the list pagination state.
	sitePagesManifestKey = ".gitsocial/site/pages.json"
	// sitePagesVersion is the page layer's schema version; bump it when a page
	// head or its sealed markup changes.
	sitePagesVersion = 21
	// sitePagesListSize is one list page's entry count.
	sitePagesListSize = 100
	// sitePagesFeedSize is the Atom feeds' entry count.
	sitePagesFeedSize = 50
	// sitePagesReadmeMax caps the front page's inlined README bytes.
	sitePagesReadmeMax = 8 * 1024
	// sitePagesHomeFiles caps the front page's root file listing, mirroring the app's HOME_FILE_LIMIT.
	sitePagesHomeFiles = 3
	// sitePagesHomeActivity caps the front page's recent-activity rows, mirroring the app's HOME_ACTIVITY_LIMIT.
	sitePagesHomeActivity = 10
)

// sitePagesBudget bounds one push's item-page writes; unbounded unless GITSOCIAL_SITE_PAGES_BUDGET caps it. A var so tests can lower it.
var sitePagesBudget = sitePagesBudgetFromEnv()

// sitePagesBudgetFromEnv returns the per-push page budget, honoring GITSOCIAL_SITE_PAGES_BUDGET.
func sitePagesBudgetFromEnv() int {
	if v := os.Getenv("GITSOCIAL_SITE_PAGES_BUDGET"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return math.MaxInt32
}

// sitePagesManifest is the .gitsocial/site/pages.json document.
type sitePagesManifest struct {
	Version  int               `json:"version"`
	Ext      map[string]string `json:"ext"`                // per-extension items-manifest tip consumed ("code" included: the front page shows the default branch's tip)
	Cursor   *sitePagesCursor  `json:"cursor,omitempty"`   // present while the bootstrap is incomplete
	Counts   map[string]int    `json:"counts,omitempty"`   // sealed list pages per type dir
	Frontier map[string]string `json:"frontier,omitempty"` // per type dir: sha12 of the newest sealed list entry (sealing boundary)
	Commits  *siteCommitsState `json:"commits,omitempty"`  // the commits list's published pagination (site_pages_commits.go)
	Files    *siteFilesState   `json:"files,omitempty"`    // the file layer's published document set (site_pages_files.go)
	SiteHash string            `json:"siteHash,omitempty"` // hash of the site identity (title/url/description) stamped into every page
}

// sitePagesCommitsPending reports whether the commits layer still owes sealing work a later push must finish.
func sitePagesCommitsPending(m *sitePagesManifest) bool {
	return m != nil && m.Commits != nil && m.Commits.Pending
}

// sitePagesCursor records an in-progress page bootstrap: per-extension counts of item pages already generated.
type sitePagesCursor struct {
	Done map[string]int `json:"done"`
}

// readSitePagesManifest fetches the pages manifest; nil when absent, at another version, or unreadable, each meaning a full regen.
func readSitePagesManifest(client *objstore.Client, prefix string) (*sitePagesManifest, error) {
	var m sitePagesManifest
	found, err := objstore.ReadCompressedJSON(client, prefix+sitePagesManifestKey, &m)
	if err != nil {
		return nil, err
	}
	if !found || m.Version != sitePagesVersion {
		return nil, nil
	}
	return &m, nil
}

// putSitePagesManifest writes the pages manifest, the layer's commit point and last in the write order.
func putSitePagesManifest(client *objstore.Client, prefix string, m *sitePagesManifest) error {
	comp, err := objstore.CompressJSON(m, objstore.BrotliQualityFull)
	if err != nil {
		return err
	}
	return objstore.PutCompressed(client, prefix+sitePagesManifestKey, comp, "")
}

// putSiteText uploads one page-layer document uncompressed, since a bucket serves a stored encoding whatever the client accepts.
func putSiteText(client *objstore.Client, key, contentType string, body []byte) error {
	headers := map[string]string{"Content-Type": contentType}
	if class := siteCacheControl(key); class != "" {
		headers["Cache-Control"] = class
	}
	if err := client.PutWithHeadersRetry(key, body, headers); err != nil {
		return fmt.Errorf("upload %s: %w", key, err)
	}
	return nil
}

// putSitePage uploads one rendered HTML page.
func putSitePage(client *objstore.Client, key string, page []byte) error {
	return putSiteText(client, key, "text/html; charset=utf-8", page)
}

// sitePageUpload is one rendered page and the key it lands at.
type sitePageUpload struct {
	key  string
	page []byte
}

// sitePagesChunk sizes one bootstrap upload batch: deep enough to keep the pool fed, short enough that an interruption repeats one batch.
func sitePagesChunk() int {
	return max(1, 4*objstore.UploadConcurrency())
}

// putSitePages uploads a batch of rendered pages through a worker pool; progress counts up from base under the mutex the Progress contract needs.
func putSitePages(client *objstore.Client, uploads []sitePageUpload, progress objstore.Progress, phase string, base, total int) error {
	if len(uploads) == 0 {
		return nil
	}
	concurrency := max(1, min(objstore.UploadConcurrency(), len(uploads)))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	work := make(chan sitePageUpload)
	var mu sync.Mutex
	var firstErr error
	var done int64
	var wg sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for u := range work {
				if err := putSitePage(client, u.key, u.page); err != nil {
					mu.Lock()
					if firstErr == nil {
						firstErr = err
						cancel()
					}
					mu.Unlock()
					continue
				}
				mu.Lock()
				progress.Call(phase, base+int(atomic.AddInt64(&done, 1)), total)
				mu.Unlock()
			}
		}()
	}
	for _, u := range uploads {
		select {
		case work <- u:
		case <-ctx.Done():
		}
	}
	close(work)
	wg.Wait()
	return firstErr
}

// sitePagesEffective resolves the page layer's enablement from the pushed site config and returns the normalized base URL.
func sitePagesEffective(cfg siteCustomization, ok bool) (string, bool) {
	if !ok || cfg.Publish != "true" || cfg.Pages != "true" {
		return "", false
	}
	return NormalizeSiteURL(cfg.URL)
}

// sitePageSiteFor assembles the site identity every page stamps, resolving a relative site.image against the base URL.
func sitePageSiteFor(prefix string, cfg siteCustomization, url string) sitePageSite {
	site := sitePageSite{Title: cfg.Title, URL: url, Description: cfg.Description, Image: cfg.Image, Icon: sitePageIcon(cfg.Favicon), AccentCSS: sitePagesAccentCSS(cfg)}
	if site.Image != "" && !strings.Contains(site.Image, "://") {
		site.Image = url + site.Image
	}
	if site.Title == "" {
		site.Title = sitePageDefaultTitle(prefix)
	}
	return site
}

// sitePageSiteHash fingerprints the site identity baked into every page; a change regenerates the whole layer.
func sitePageSiteHash(site sitePageSite) string {
	h := sha256.Sum256([]byte(site.Title + "\x00" + site.URL + "\x00" + site.Description + "\x00" + site.Image + "\x00" + string(site.Icon) + "\x00" + string(site.AccentCSS)))
	return hex.EncodeToString(h[:])[:12]
}

// sitePagesState reports the push-state marker's pages component, and whether the layer still has work only a site pass runs; a read error counts as pending.
func sitePagesState(client *objstore.Client, prefix string, refs map[string]string, ov objstore.SiteOverride, src *objstore.LocalCommitSource) (state string, pending bool) {
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
	if err != nil || manifest == nil || manifest.Cursor != nil || sitePagesCommitsPending(manifest) || sitePagesFilesPending(manifest) {
		return "", true
	}
	// The file layer's flag comes from the recorded set, since this helper has no default branch to walk a tree from.
	site := sitePageSiteFor(prefix, cfg, url)
	site.Files = sitePagesHasFiles(manifest)
	if manifest.SiteHash != sitePageSiteHash(site) {
		return "", true
	}
	_, tips, err := readSitePagesManifests(client, prefix, refs)
	if err != nil || !sitePagesTipsCurrent(manifest, tips) {
		return "", true
	}
	return sitePagesStateOn, false
}

// rebuildSitePages maintains the page layer after the item artifacts, deleting the page set when the guards are off; pending leaves the marker unstamped.
func rebuildSitePages(client *objstore.Client, prefix string, refs map[string]string, defaultBranch string, src *objstore.LocalCommitSource, progress objstore.Progress, ov objstore.SiteOverride) (pending bool, state string, err error) {
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
	// Discovery runs before the identity is hashed, since the Files sidebar entry is stamped into every page. An unreadable tree carries the published set forward.
	tip := siteBucketBranchTip(refs, defaultBranch)
	docs, known := discoverSiteFileDocs(src, tip, cfg)
	if !known {
		docs = siteFileDocsFromState(sitePagesFilesState(manifest))
	}
	site.Files = len(docs) > 0
	if manifest != nil && manifest.SiteHash != sitePageSiteHash(site) {
		manifest = nil // the site identity is stamped into every page: full regen
	}
	manifests, tips, err := readSitePagesManifests(client, prefix, refs)
	if err != nil {
		return false, "", err
	}
	home := readSiteFrontHome(src, site, refs, defaultBranch)
	files := siteFilePass{docs: docs, branch: defaultBranch, tip: tip, src: src}
	switch {
	case manifest != nil && manifest.Cursor == nil && sitePagesTipsCurrent(manifest, tips) && !sitePagesCommitsPending(manifest) &&
		!sitePagesFilesPending(manifest) && !siteFileDocsChanged(manifest.Files, docs, defaultBranch):
		// Nothing a page derives from moved, but this push's shell upload may have written over index.html, so reclaim it.
		err = reclaimSiteFrontPage(client, prefix, site, manifests, home)
	case manifest != nil && manifest.Cursor == nil:
		pending, err = incrementalSitePages(client, prefix, site, manifest, manifests, tips, defaultBranch, home, files, progress)
	default:
		pending, err = generateSitePages(client, prefix, site, manifest, manifests, tips, defaultBranch, home, files, progress)
	}
	if err != nil || pending {
		return pending, "", err
	}
	// Sweep the retired pre-flip front page so an older binary's bucket stops serving a duplicate.
	_ = client.Delete(prefix + sitePagesLegacyFrontKey)
	return false, sitePagesStateOn, nil
}

// reclaimSiteFrontPage re-renders index.html from the metadata index alone, reading no bodies.
func reclaimSiteFrontPage(client *objstore.Client, prefix string, site sitePageSite, manifests map[string]*siteShardManifest, home *siteFrontHome) error {
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

// readSiteFrontCodeEntries returns the newest code items from the code index: the head, then the newest sealed shards.
func readSiteFrontCodeEntries(client *objstore.Client, prefix string, limit int) ([]siteMetaEntry, error) {
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

// sitePageDefaultTitle derives a fallback site title from the key prefix's last segment.
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

// readSitePagesManifests reads every present items manifest plus the code index, returning the gitmsg manifests and the consumed-tip map.
func readSitePagesManifests(client *objstore.Client, prefix string, refs map[string]string) (map[string]*siteShardManifest, map[string]string, error) {
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

// sitePagesTipsCurrent reports whether the pages manifest consumed the current items-manifest tips; any drift triggers the incremental pass.
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

// readSiteFrontHome reads the front page's body from the local odb at the bucket's own tip, so the page cannot describe content the bucket is unable to serve.
func readSiteFrontHome(src *objstore.LocalCommitSource, site sitePageSite, refs map[string]string, defaultBranch string) *siteFrontHome {
	if defaultBranch == "" {
		return nil
	}
	// The app's chip counts every refs/heads/* the bucket carries, plus the default branch when the listing lags it.
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
	tip := siteBucketBranchTip(refs, defaultBranch)
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

// siteBucketBranchTip returns the bucket's usable tip for a branch, the one sha the front page and the file layer read a tree from.
func siteBucketBranchTip(refs map[string]string, branch string) string {
	if tip := refs[localBranchRef(branch)]; len(tip) >= 12 {
		return tip
	}
	return ""
}

// readSiteFrontLatest reads the default branch's tip commit for the front page's meta strip.
func readSiteFrontLatest(src *objstore.LocalCommitSource, site sitePageSite, sha, branch string) *siteFrontCommit {
	if len(sha) < 12 {
		return nil
	}
	body, ok := src.Commit(sha)
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

// siteTreeRow is one raw tree entry: its mode, name and object sha.
type siteTreeRow struct {
	Mode string
	Name string
	SHA  string
}

const (
	// siteTreeDirMode marks a subtree entry.
	siteTreeDirMode = "40000"
	// siteTreeFileMode and siteTreeExecMode mark the regular blobs; any other mode is a submodule or a symlink.
	siteTreeFileMode = "100644"
	siteTreeExecMode = "100755"
)

// parseSiteTreeRows parses a raw git tree object into its entries.
func parseSiteTreeRows(body []byte) []siteTreeRow {
	var rows []siteTreeRow
	for i := 0; i < len(body); {
		sep := bytes.IndexByte(body[i:], 0)
		if sep < 0 || i+sep+21 > len(body) {
			break
		}
		mode, name, found := strings.Cut(string(body[i:i+sep]), " ")
		if !found {
			break
		}
		rows = append(rows, siteTreeRow{Mode: mode, Name: name, SHA: hex.EncodeToString(body[i+sep+1 : i+sep+21])})
		i += sep + 21
	}
	return rows
}

// readSiteRootTree reads and parses the root tree of the bucket tip's commit; empty when the odb has no local copy.
func readSiteRootTree(src *objstore.LocalCommitSource, tip string) []siteTreeEntry {
	if tip == "" {
		return nil
	}
	body, ok := src.Object(tip+"^{tree}", "tree")
	if !ok {
		return nil
	}
	var entries []siteTreeEntry
	for _, row := range parseSiteTreeRows(body) {
		entries = append(entries, siteTreeEntry{Name: row.Name, IsDir: row.Mode == siteTreeDirMode})
	}
	return entries
}

// siteReadmeName picks the root README the app's findReadme would pick, by the same names and precedence.
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

// readSiteFrontReadme reads and renders one root README, capping the source at a line boundary so the renderer takes no half line.
func readSiteFrontReadme(src *objstore.LocalCommitSource, tip, name, branch string, site sitePageSite) *siteFrontReadme {
	if tip == "" || name == "" {
		return nil
	}
	body, ok := src.Object(tip+":"+name, "blob")
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

// buildSiteFrontFiles renders the root listing the app's homeFileList shows: directories first, then files, capped behind a "Show all N" link.
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

// generateSitePages runs one budgeted full-regen pass, writing in the pinned order with the manifest last.
func generateSitePages(client *objstore.Client, prefix string, site sitePageSite, prior *sitePagesManifest, manifests map[string]*siteShardManifest, tips map[string]string, defaultBranch string, home *siteFrontHome, files siteFilePass, progress objstore.Progress) (bool, error) {
	msgs := map[string][]sitePageMsg{}
	for ext, m := range manifests {
		entries, err := readSitePagesCorpus(client, prefix, ext, m)
		if err != nil {
			return false, fmt.Errorf("read pages corpus %s: %w", ext, err)
		}
		msgs[ext] = entries
	}
	roots := buildSitePageThreads(msgs)
	titles := siteItemPageTitles(roots, siteFileTitleSet(sitePagesFilesState(prior)))
	done, complete, budget, err := writeSiteItemPages(client, prefix, roots, tips, site, prior, titles, progress)
	if err != nil {
		return false, err
	}
	// File pages go out after the item pages, which a bucket needs before it has any crawlable items.
	filesState, budget, err := maintainSiteFilePages(client, prefix, site, sitePagesFilesState(prior), files, siteTitleSet(titles), prior == nil, budget, progress)
	if err != nil {
		return false, err
	}
	counts, frontier, err := writeSiteTypeLists(client, prefix, roots, done, complete, site, prior, nil)
	if err != nil {
		return false, err
	}
	// A full regen re-derives the commits layer from scratch, which is what a version or identity change calls for.
	commits, err := maintainSiteCommitPages(client, prefix, site, nil, defaultBranch, complete, budget)
	if err != nil {
		return false, err
	}
	if err := writeSiteFrontPage(client, prefix, roots, done, site, home); err != nil {
		return false, err
	}
	if err := writeSiteSitemap(client, prefix, roots, done, site, commits, filesState); err != nil {
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
	manifest := &sitePagesManifest{Version: sitePagesVersion, Ext: tips, Commits: commits, Files: filesState, SiteHash: sitePageSiteHash(site)}
	if complete {
		manifest.Counts, manifest.Frontier = counts, frontier
	} else {
		manifest.Cursor = &sitePagesCursor{Done: done}
	}
	if err := putSitePagesManifest(client, prefix, manifest); err != nil {
		return false, err
	}
	return !complete || commits.Pending || sitePagesFilesPending(manifest), nil
}

// maintainSiteCommitPages runs the commits list layer for one pass; sealing waits on a complete code index, since page 1 must hold the oldest hundred.
func maintainSiteCommitPages(client *objstore.Client, prefix string, site sitePageSite, prior *siteCommitsState, defaultBranch string, itemsComplete bool, budget int) (*siteCommitsState, error) {
	entries, codeComplete, err := readSiteCommitEntries(client, prefix, defaultBranch)
	if err != nil {
		return nil, err
	}
	return writeSiteCommitPages(client, prefix, entries, defaultBranch, itemsComplete && codeComplete, site, prior, budget)
}

// incrementalSitePages regenerates only the threads and lists one push's delta touched; a vanished consumed tip falls back to the full regen.
func incrementalSitePages(client *objstore.Client, prefix string, site sitePageSite, prior *sitePagesManifest, manifests map[string]*siteShardManifest, tips map[string]string, defaultBranch string, home *siteFrontHome, files siteFilePass, progress objstore.Progress) (bool, error) {
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
			return generateSitePages(client, prefix, site, nil, manifests, tips, defaultBranch, home, files, progress)
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
	uploads := make([]sitePageUpload, 0, len(affected))
	titles := siteItemPageTitles(roots, siteFileTitleSet(prior.Files))
	for _, r := range affected {
		page, err := renderSitePage("item", buildSiteItemPage(r, listByExt[r.Msg.Ext], site, titles[r.Msg.Short]))
		if err != nil {
			return false, err
		}
		uploads = append(uploads, sitePageUpload{key: prefix + "i/" + r.Msg.Short + ".html", page: page})
		affectedDirs[listByExt[r.Msg.Ext].Dir] = true
	}
	if err := putSitePages(client, uploads, progress, "site pages", 0, len(affected)); err != nil {
		return false, err
	}
	counts, frontier, err := writeSiteTypeLists(client, prefix, roots, done, true, site, prior, affectedDirs)
	if err != nil {
		return false, err
	}
	budget := max(0, sitePagesBudget-len(affected))
	filesState, budget, err := maintainSiteFilePages(client, prefix, site, prior.Files, files, siteTitleSet(titles), false, budget, progress)
	if err != nil {
		return false, err
	}
	// The commits layer is dirty on three signals: a moved code tip, a changed default branch, or a seal the budget cut short.
	commits := prior.Commits
	if commits == nil || commits.Pending || commits.Branch != defaultBranch || prior.Ext[siteCodeExt] != tips[siteCodeExt] {
		commits, err = maintainSiteCommitPages(client, prefix, site, prior.Commits, defaultBranch, true, budget)
		if err != nil {
			return false, err
		}
	}
	if err := writeSiteFrontPage(client, prefix, roots, done, site, home); err != nil {
		return false, err
	}
	commitsMoved := prior.Commits == nil || *prior.Commits != *commits
	if len(affected) > 0 || commitsMoved || siteFilesMoved(prior.Files, filesState) {
		if err := writeSiteSitemap(client, prefix, roots, done, site, commits, filesState); err != nil {
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
	manifest := &sitePagesManifest{Version: sitePagesVersion, Ext: tips, Counts: counts, Frontier: frontier, Commits: commits, Files: filesState, SiteHash: sitePageSiteHash(site)}
	if err := putSitePagesManifest(client, prefix, manifest); err != nil {
		return false, err
	}
	return commits.Pending || sitePagesFilesPending(manifest), nil
}

// sitePageEntriesSince returns the entries appended after a consumed tip; found is false when the tip has left the corpus.
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

// affectedSitePageRoots maps a delta onto the top-level items whose pages must be regenerated, newest-first; a member with no owning root is skipped.
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

// deleteSitePages removes the page layer on disable, best-effort per key with the manifest deleted last; index.html is restored to the shell rather than deleted.
func deleteSitePages(client *objstore.Client, prefix string) (bool, error) {
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
	namespaces := []string{"i/", siteCommitsDir + "/", sitePagesFilesDir + "/"}
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
	for _, key := range []string{sitePagesLegacyFrontKey, sitePagesLegacyCSSKey, sitePagesSitemapKey, sitePagesRobotsKey, sitePagesFeedKey} {
		remove(prefix + key)
	}
	// Restore the embedded shell as index.html; a failure keeps the sweep incomplete so the next push retries.
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

// writeSiteItemPages writes item pages newest-first per extension under the per-push budget and returns what is left of it; a moved tip resets that extension.
func writeSiteItemPages(client *objstore.Client, prefix string, roots map[string][]*sitePageItem, tips map[string]string, site sitePageSite, prior *sitePagesManifest, titles map[string]string, progress objstore.Progress) (map[string]int, bool, int, error) {
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
	chunk := sitePagesChunk()
	for _, list := range sitePageLists {
		rs := roots[list.Ext]
		for done[list.Ext] < len(rs) {
			if budget <= 0 {
				complete = false
				break
			}
			batch := min(chunk, len(rs)-done[list.Ext], budget)
			uploads := make([]sitePageUpload, 0, batch)
			for _, it := range rs[done[list.Ext] : done[list.Ext]+batch] {
				page, err := renderSitePage("item", buildSiteItemPage(it, list, site, titles[it.Msg.Short]))
				if err != nil {
					return nil, false, 0, err
				}
				uploads = append(uploads, sitePageUpload{key: prefix + "i/" + it.Msg.Short + ".html", page: page})
			}
			if err := putSitePages(client, uploads, progress, "site pages "+list.Ext, done[list.Ext], len(rs)); err != nil {
				return nil, false, 0, err
			}
			done[list.Ext] += batch
			budget -= batch
		}
	}
	return done, complete, budget, nil
}

// writeSiteTypeLists writes the type list pages: the head, plus the sealed pages the frontier leaves to seal; affected (nil = every dir) limits the pass.
func writeSiteTypeLists(client *objstore.Client, prefix string, roots map[string][]*sitePageItem, done map[string]int, complete bool, site sitePageSite, prior *sitePagesManifest, affected map[string]bool) (map[string]int, map[string]string, error) {
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
		chunk := sitePagesChunk()
		uploads := make([]sitePageUpload, 0, min(len(newPages), chunk))
		for i, segment := range newPages {
			page, err := renderSitePage("list", buildSiteSealedListPage(list, site, segment, sealed+i+1, finalSealed))
			if err != nil {
				return nil, nil, err
			}
			uploads = append(uploads, sitePageUpload{key: prefix + list.Dir + "/" + strconv.Itoa(sealed+i+1) + ".html", page: page})
			if len(uploads) < chunk {
				continue
			}
			if err := putSitePages(client, uploads, nil, "", 0, 0); err != nil {
				return nil, nil, err
			}
			uploads = uploads[:0]
		}
		if err := putSitePages(client, uploads, nil, "", 0, 0); err != nil {
			return nil, nil, err
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

// siteChainedListPage assembles the heading, rows and newer/older chain every list page shares; n = 0 is the head, n >= 1 a sealed page with 1 the oldest.
func siteChainedListPage(label string, entries []sitePageListEntry, metaBits []string, n, sealed int) siteListPageData {
	d := siteListPageData{
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
	d := siteChainedListPage(list.NavLabel, entries, metaBits, 0, sealed)
	d.Chrome = sitePageChrome{
		Title:         list.NavLabel + " · " + site.Title,
		AccentCSS:     site.AccentCSS,
		Description:   sitePageDescription(sitePageListDescription(list, site), ""),
		OGTitle:       list.NavLabel + " · " + site.Title,
		SiteTitle:     site.Title,
		Canonical:     site.URL + list.Dir + "/index.html",
		Route:         list.Route,
		Base:          "../",
		Image:         site.Image,
		Icon:          site.Icon,
		Feed:          site.URL + sitePagesFeedKey,
		TypeFeed:      site.URL + siteTypeFeedKey(list),
		TypeFeedTitle: siteTypeFeedTitle(list, site),
		Nav:           sitePageSidebar("../", list.Dir, site.Files),
	}
	return d
}

// buildSiteSealedListPage assembles one sealed older list page; siteChainedListPage owns its chain.
func buildSiteSealedListPage(list sitePageList, site sitePageSite, pageEntries []*sitePageItem, n, sealed int) siteListPageData {
	entries := make([]sitePageListEntry, 0, len(pageEntries))
	for _, it := range pageEntries {
		entries = append(entries, buildSiteListEntry(it, "../", sitePageDefaultTypes[list.Ext]))
	}
	metaBits := []string{fmt.Sprintf("%d %s", len(entries), list.Label), fmt.Sprintf("older page %d", n)}
	d := siteChainedListPage(list.NavLabel, entries, metaBits, n, sealed)
	d.Chrome = sitePageChrome{
		Title:         fmt.Sprintf("%s · page %d · %s", list.NavLabel, n, site.Title),
		AccentCSS:     site.AccentCSS,
		Description:   sitePageDescription(sitePageListDescription(list, site), ""),
		OGTitle:       fmt.Sprintf("%s · page %d · %s", list.NavLabel, n, site.Title),
		SiteTitle:     site.Title,
		Canonical:     site.URL + list.Dir + "/" + strconv.Itoa(n) + ".html",
		Route:         list.Route,
		Base:          "../",
		Image:         site.Image,
		Icon:          site.Icon,
		Feed:          site.URL + sitePagesFeedKey,
		TypeFeed:      site.URL + siteTypeFeedKey(list),
		TypeFeedTitle: siteTypeFeedTitle(list, site),
		Nav:           sitePageSidebar("../", list.Dir, site.Files),
	}
	return d
}

// sitePageListDescription words a type list's meta description.
func sitePageListDescription(list sitePageList, site sitePageSite) string {
	return list.NavLabel + " of " + site.Title + ", newest first."
}

// writeSiteFrontPage writes the front page: the branch strip, root file listing, README and recent-activity rows the app's home route also renders.
func writeSiteFrontPage(client *objstore.Client, prefix string, roots map[string][]*sitePageItem, done map[string]int, site sitePageSite, home *siteFrontHome) error {
	code, err := readSiteFrontCodeEntries(client, prefix, sitePagesHomeActivity)
	if err != nil {
		return err
	}
	description := site.Description
	if description == "" {
		description = site.Title + ": code, issues, pull requests, posts and releases."
	}
	d := siteFrontPageData{
		Description: site.Description,
		Home:        home,
		Activity:    buildSiteFrontActivity(roots, done, code, site),
	}
	if len(d.Activity) > 0 {
		d.ActivityMoreHref, d.ActivityMoreLabel = siteActivityMoreKey, siteActivityMoreLabel
	}
	d.Chrome = sitePageChrome{
		Title:       site.Title,
		AccentCSS:   site.AccentCSS,
		Description: sitePageDescription(description, site.Title),
		OGTitle:     site.Title,
		SiteTitle:   site.Title,
		// The front page's canonical URL is the site root, matching the sitemap's root entry.
		Canonical: site.URL,
		// The front page is the app's home view, so a /timeline route here would boot past the landing it shows.
		Route: "/",
		Base:  "./",
		Image: site.Image,
		Icon:  site.Icon,
		Feed:  site.URL + sitePagesFeedKey,
		Nav:   sitePageSidebar("./", "", site.Files),
	}
	page, err := renderSitePage("front", d)
	if err != nil {
		return err
	}
	return putSitePage(client, prefix+sitePagesFrontKey, page)
}

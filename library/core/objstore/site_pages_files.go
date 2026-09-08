// site_pages_files.go - the crawlable file-page layer:
//
//	f/<repo path>.html      one page per prose document on the default branch
//	f/index.html            the list of published documents
//
// The item layers project protocol items; a repo's own files get no page at
// all, so a young repo's whole indexable site is a handful of near-duplicate
// item pages. These pages put the repo's prose where a crawler can read it.
//
// Keys mirror the repo path (the only collision-free key for an arbitrary tree,
// and a 1:1 map onto the app's `file:<path>@<branch>` route every page stamps as
// its boot hook, so the upgrade opens the file viewer on the document the
// crawler just read). The document extension is swapped for .html, with a
// fallback for the rare two-documents-one-key case.
//
// Discovery is the one place the page layer reads a git tree rather than the
// push's own index artifacts (documentation/STATIC-SITE.md): those indexes carry
// commits, not files. It walks the whole default-branch tree from the pusher's
// local odb and selects by extension and convention, minus the directories no
// site publishes from and minus the root README (the front page owns it), with
// config globs as the escape hatch. Nothing here names a path of any particular
// repo: the layer ships to every repo that pushes a site.
//
// Markdown renders through the shared renderer (site_markdown.go) with no 8 KB
// cap — that cap is the front page's README EXCERPT and a file page is the whole
// document — under a 256 KB ceiling cut at a line boundary. Other prose renders
// preformatted: there is no .rst or .adoc grammar here, and feeding one to the
// markdown renderer would invent structure the source does not have.
//
// Pages are no-cache (a file is mutable in a way a protocol item is not) and
// incremental: the manifest records each published path's blob sha, so a pass
// over an unchanged tree writes nothing.

package objstore

import (
	"fmt"
	"html/template"
	"os/exec"
	"path"
	"sort"
	"strconv"
	"strings"
)

const (
	// sitePagesFilesDir is the file pages' bucket directory.
	sitePagesFilesDir = "f"
	// sitePagesFileMax caps one published document's source bytes.
	sitePagesFileMax = 256 * 1024
	// sitePagesFileMinWords is the floor below which a document is published but
	// not submitted: thin pages are what this layer exists to stop shipping.
	sitePagesFileMinWords = 100
	// sitePagesFileHeadMax bounds the source parsed for a title and description.
	sitePagesFileHeadMax = 8 * 1024
)

// siteFilesList describes the file directory the way sitePageList describes a
// gitmsg type dir, so the shared list template, chrome and sidebar serve it
// unchanged. Its route is the app's code browser — the closest surface the app
// has to a list of the repo's own files.
var siteFilesList = sitePageList{Dir: sitePagesFilesDir, Label: "files", Route: "/code", NavLabel: "Files", Glyph: "❯", Section: "Repository"}

// siteFileDocExts maps a prose extension to whether it renders as markdown.
// Markdown only: .txt and the other prose extensions were measured across
// unseen repos as overwhelmingly fixtures, generated data and build files, and
// a format this package has no grammar for renders worse than not at all.
var siteFileDocExts = map[string]bool{
	".md": true, ".markdown": true, ".mdown": true, ".mdx": true,
}

// siteFileDocNames are the extensionless basenames convention makes documents.
// A license file is matched by the siteFileLicensePrefix instead, so both
// spellings count.
var siteFileDocNames = map[string]bool{
	"authors": true, "changelog": true, "changes": true, "contributing": true,
	"contributors": true, "copying": true, "install": true, "maintainers": true,
	"notice": true, "readme": true, "security": true, "todo": true,
}

// siteFileLicensePrefix matches a license file in either spelling by prefix,
// because the repo's misspell linter rewrites the British literal on sight (the
// same reason sitePageStateClass matches a cancel state by prefix).
const siteFileLicensePrefix = "licen"

// siteFileSkipDirs are the directories no site publishes documents from: files
// that are someone else's, or fixtures that are nobody's reading material.
var siteFileSkipDirs = map[string]bool{
	"node_modules": true, "vendor": true, "testdata": true, "third_party": true,
	"fixtures": true, "golden": true, "corpus": true, "snapshots": true, "__snapshots__": true,
}

// siteFileRecord is one published document's state: the blob it was rendered
// from, the key it landed at, its resolved title and its last-commit date.
type siteFileRecord struct {
	Blob    string `json:"blob"`
	Key     string `json:"key"`
	Title   string `json:"title,omitempty"`
	Lastmod string `json:"lastmod,omitempty"`
	Thin    bool   `json:"thin,omitempty"`
}

// siteFilesState is the file layer's published set, recorded in the pages
// manifest: the branch the documents were read from, one record per published
// repo path, and whether the page budget cut the pass short.
type siteFilesState struct {
	Branch  string                    `json:"branch"`
	Files   map[string]siteFileRecord `json:"files,omitempty"`
	Pending bool                      `json:"pending,omitempty"`
}

// siteFilePass is what one pass needs to maintain the layer: the documents the
// default branch carries and the odb they are read from.
type siteFilePass struct {
	docs   []siteFileDoc
	branch string
	tip    string
	src    *localCommitSource
}

// siteFileDoc is one discovered document: its repo path, the blob behind it,
// the bucket key it publishes at, and whether it renders as markdown.
type siteFileDoc struct {
	Path     string
	Blob     string
	Key      string
	Markdown bool
}

// siteFilePageData feeds the "file" template.
type siteFilePageData struct {
	Chrome    sitePageChrome
	Heading   string
	MetaBits  []string
	HTML      template.HTML
	Pre       string
	Truncated bool
}

// sitePagesFilesPending reports whether the file layer still owes work a later
// push must finish (the per-push page budget cut it short).
func sitePagesFilesPending(m *sitePagesManifest) bool {
	return m != nil && m.Files != nil && m.Files.Pending
}

// sitePagesFilesState returns the manifest's recorded file-layer state (nil
// when the manifest is absent or carried none).
func sitePagesFilesState(m *sitePagesManifest) *siteFilesState {
	if m == nil {
		return nil
	}
	return m.Files
}

// siteFileDocsChanged reports whether the discovered document set differs from
// the published one. It is the layer's own "something moved" signal: a config
// glob, a default-branch change or an edited document can move the file pages
// without moving any tip the other layers derive from.
func siteFileDocsChanged(state *siteFilesState, docs []siteFileDoc, branch string) bool {
	if state == nil {
		return len(docs) > 0
	}
	if state.Branch != branch || len(state.Files) != len(docs) {
		return true
	}
	for _, d := range docs {
		if r, ok := state.Files[d.Path]; !ok || r.Blob != d.Blob || r.Key != d.Key {
			return true
		}
	}
	return false
}

// siteFilesMoved reports whether a pass changed the published document set,
// which is what puts the file entries in a rewritten sitemap.
func siteFilesMoved(prior, next *siteFilesState) bool {
	if prior == nil || next == nil {
		return prior != next
	}
	if len(prior.Files) != len(next.Files) {
		return true
	}
	for p, r := range prior.Files {
		if next.Files[p] != r {
			return true
		}
	}
	return false
}

// sitePagesHasFiles reports whether the recorded page set carries file pages,
// which is what puts the Files entry in every page's sidebar.
func sitePagesHasFiles(m *sitePagesManifest) bool {
	return m != nil && m.Files != nil && len(m.Files.Files) > 0
}

// siteFileTitleSet returns the titles the published file pages already hold, so
// the item pages' <title> disambiguation can avoid them.
func siteFileTitleSet(state *siteFilesState) map[string]bool {
	taken := map[string]bool{}
	if state == nil {
		return taken
	}
	for _, r := range state.Files {
		if r.Title != "" {
			taken[r.Title] = true
		}
	}
	return taken
}

// siteFileExt returns a path's lowercased extension ("" when it has none).
func siteFileExt(p string) string { return strings.ToLower(path.Ext(p)) }

// siteFileIsDocument reports whether a repo path is prose this layer publishes,
// and whether it renders as markdown.
func siteFileIsDocument(p string) (markdown, ok bool) {
	name := path.Base(p)
	if ext := siteFileExt(name); ext != "" {
		markdown, ok = siteFileDocExts[ext]
		return markdown, ok
	}
	lower := strings.ToLower(name)
	return false, siteFileDocNames[lower] || strings.HasPrefix(lower, siteFileLicensePrefix)
}

// siteFileGlobMatch reports whether a path matches a glob, segment by segment,
// where a "**" segment stands for any number of segments (including none).
func siteFileGlobMatch(pattern, p string) bool {
	pat, seg := strings.Split(pattern, "/"), strings.Split(p, "/")
	var match func(i, j int) bool
	match = func(i, j int) bool {
		for i < len(pat) {
			if pat[i] == "**" {
				for k := j; k <= len(seg); k++ {
					if match(i+1, k) {
						return true
					}
				}
				return false
			}
			if j >= len(seg) {
				return false
			}
			if ok, err := path.Match(pat[i], seg[j]); err != nil || !ok {
				return false
			}
			i, j = i+1, j+1
		}
		return j == len(seg)
	}
	return match(0, 0)
}

// siteFileGlobAny reports whether any of the globs matches the path.
func siteFileGlobAny(globs []string, p string) bool {
	for _, g := range globs {
		if siteFileGlobMatch(g, p) {
			return true
		}
	}
	return false
}

// siteFileGlobs splits a configured comma-separated glob list.
func siteFileGlobs(v string) []string {
	var globs []string
	for _, g := range strings.Split(v, ",") {
		if g = strings.TrimSpace(g); g != "" {
			globs = append(globs, g)
		}
	}
	return globs
}

// siteFileWalk collects the default-branch tree's blobs (repo path + sha),
// pruning the directories and dotdirs no site publishes from. ok is false when
// the tree cannot be read at all (no tip, or the odb has no local copy), which
// is never "this repo has no documents".
func siteFileWalk(src *localCommitSource, tip string) (files []siteFileDoc, ok bool) {
	if tip == "" {
		return nil, false
	}
	body, ok := src.object(tip+"^{tree}", "tree")
	if !ok {
		return nil, false
	}
	var walk func(rows []siteTreeRow, dir string)
	walk = func(rows []siteTreeRow, dir string) {
		for _, row := range rows {
			if strings.HasPrefix(row.Name, ".") {
				continue
			}
			p := row.Name
			if dir != "" {
				p = dir + "/" + row.Name
			}
			switch row.Mode {
			case siteTreeDirMode:
				if siteFileSkipDirs[strings.ToLower(row.Name)] {
					continue
				}
				sub, found := src.object(row.SHA, "tree")
				if !found {
					continue
				}
				walk(parseSiteTreeRows(sub), p)
			case siteTreeFileMode, siteTreeExecMode:
				files = append(files, siteFileDoc{Path: p, Blob: row.SHA})
			}
		}
	}
	walk(parseSiteTreeRows(body), "")
	return files, true
}

// selectSiteFileDocs applies the publication rules to a walked tree: an exclude
// glob is the owner's explicit no, an include glob publishes what the extension
// rule would miss, the root README belongs to the front page, and everything
// else is published when it is prose. Returns the documents path-ordered, each
// with the bucket key it owns.
func selectSiteFileDocs(files []siteFileDoc, rootReadme string, include, exclude []string) []siteFileDoc {
	var docs []siteFileDoc
	for _, f := range files {
		if siteFileGlobAny(exclude, f.Path) || !siteFileRoutablePath(f.Path) {
			continue
		}
		markdown, ok := siteFileIsDocument(f.Path)
		if !ok && !siteFileGlobAny(include, f.Path) {
			continue
		}
		if f.Path == rootReadme {
			continue
		}
		f.Markdown = markdown
		docs = append(docs, f)
	}
	sort.Slice(docs, func(i, j int) bool { return docs[i].Path < docs[j].Path })
	// index.html is the layer's own list page, so a root index.md takes the
	// fallback key rather than overwriting it.
	taken := map[string]bool{"index.html": true}
	kept := docs[:0]
	for _, d := range docs {
		key := siteFilePageKey(d.Path)
		if taken[key] {
			key = d.Path + ".html"
		}
		if taken[key] {
			continue
		}
		taken[key] = true
		d.Key = sitePagesFilesDir + "/" + key
		kept = append(kept, d)
	}
	return kept
}

// siteFileRoutablePath reports whether a repo path can be both a URL and a
// `file:` route: the ref grammar splits on "@" and ":", and a URL cannot carry a
// space or the reserved characters, so a path holding any of them would publish
// a page whose own boot hook points somewhere else.
func siteFileRoutablePath(p string) bool {
	return !strings.ContainsAny(p, " @:#?%\\\"<>") && strings.IndexFunc(p, func(r rune) bool { return r < 0x20 }) < 0
}

// siteFilePageKey maps a repo path to its page key under the file directory: a
// document extension is swapped for .html, an extensionless name takes it.
func siteFilePageKey(p string) string {
	if ext := siteFileExt(p); ext != "" {
		return strings.TrimSuffix(p, path.Ext(p)) + ".html"
	}
	return p + ".html"
}

// discoverSiteFileDocs resolves the documents the default branch publishes.
// known is false when the tree is unreadable — the pusher has no local odb, or
// the bucket tip is not in it — in which case the caller carries the published
// set forward rather than reading a missing tree as an empty repo.
func discoverSiteFileDocs(src *localCommitSource, tip string, cfg siteCustomization) (docs []siteFileDoc, known bool) {
	files, ok := siteFileWalk(src, tip)
	if !ok {
		return nil, false
	}
	var root []siteTreeEntry
	for _, f := range files {
		if !strings.Contains(f.Path, "/") {
			root = append(root, siteTreeEntry{Name: f.Path})
		}
	}
	return selectSiteFileDocs(files, siteReadmeName(root), siteFileGlobs(cfg.FilesInclude), siteFileGlobs(cfg.FilesExclude)), true
}

// siteFileDocsFromState rebuilds the discovered set from the published state,
// which is what a pass with an unreadable tree works from: every document is
// unchanged, so the layer writes nothing and deletes nothing.
func siteFileDocsFromState(state *siteFilesState) []siteFileDoc {
	if state == nil {
		return nil
	}
	docs := make([]siteFileDoc, 0, len(state.Files))
	for p, r := range state.Files {
		markdown, _ := siteFileIsDocument(p)
		docs = append(docs, siteFileDoc{Path: p, Blob: r.Blob, Key: r.Key, Markdown: markdown})
	}
	sort.Slice(docs, func(i, j int) bool { return docs[i].Path < docs[j].Path })
	return docs
}

// siteFileStripMDX drops the ES import/export lines and standalone JSX element
// lines an .mdx document carries, which the markdown grammar has no rule for.
func siteFileStripMDX(source string) string {
	lines := strings.Split(source, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "import ") || strings.HasPrefix(t, "export ") {
			continue
		}
		if len(t) > 1 && t[0] == '<' && t[len(t)-1] == '>' {
			if r := t[1]; r == '/' || (r >= 'A' && r <= 'Z') {
				continue
			}
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

// siteFileWordCount counts the prose words a document publishes.
func siteFileWordCount(source string, markdown bool) int {
	if markdown {
		return len(strings.Fields(siteMarkdownPlainText(source)))
	}
	return len(strings.Fields(source))
}

// siteFileLastCommitTimes maps every path to its newest commit time in ONE
// history walk. Per-path `git log` was measured 44x slower on a real repo, and
// the first pass is the one that saturates the page budget.
func siteFileLastCommitTimes(src *localCommitSource, tip string) map[string]int64 {
	times := map[string]int64{}
	if src == nil || tip == "" {
		return times
	}
	args := []string{}
	if src.workdir != "" {
		args = append(args, "-C", src.workdir)
	}
	args = append(args, "log", "--format=%ct", "--name-only", "--no-renames", tip)
	cmd := exec.Command("git", args...)
	cmd.Env = append(cmd.Environ(), "GIT_NO_LAZY_FETCH=1")
	if src.gitDir != "" {
		cmd.Env = append(cmd.Env, "GIT_DIR="+src.gitDir)
	}
	out, err := cmd.Output()
	if err != nil {
		return times
	}
	var ts int64
	for _, line := range strings.Split(string(out), "\n") {
		if line == "" {
			continue
		}
		if n, err := strconv.ParseInt(line, 10, 64); err == nil && !strings.Contains(line, "/") {
			ts = n
			continue
		}
		if _, seen := times[line]; !seen {
			times[line] = ts
		}
	}
	return times
}

// siteFileMeta extracts a document's page title and description source. A
// LEADING `# H1` titles the page and is left to the render, which shows it in
// place; anything else is titled by its path. The description is the first
// paragraph.
func siteFileMeta(body string, markdown bool) (title, summary string, leadHeading bool) {
	if !markdown {
		for _, block := range strings.Split(strings.ReplaceAll(body, "\r", ""), "\n\n") {
			if block = strings.TrimSpace(block); block != "" {
				return "", block, false
			}
		}
		return "", "", false
	}
	blocks := parseSiteMarkdown(body)
	for i, block := range blocks {
		if i == 0 && block.Kind == siteMDHeading && block.Level == 1 {
			title, leadHeading = strings.TrimSpace(siteMDSpanPlainText(block.Spans)), true
			continue
		}
		if block.Kind == siteMDParagraph {
			return title, siteMDSpanPlainText(block.Spans), leadHeading
		}
	}
	return title, "", leadHeading
}

// buildSiteFilePage assembles one document's page: the rendered markdown (or
// the preformatted source), the path/branch/date meta line, and the file route
// the boot hook stamps.
func buildSiteFilePage(doc siteFileDoc, source string, truncated bool, site sitePageSite, branch, title, lastmod string, thin bool) siteFilePageData {
	_, summary, leadHeading := siteFileMeta(siteFileHead(source), doc.Markdown)
	d := siteFilePageData{Truncated: truncated}
	if doc.Markdown {
		d.HTML = template.HTML(renderSiteMarkdown(source, siteMarkdownContext{AppBase: sitePageAppURL(site, ""), Branch: branch}))
	} else {
		d.Pre = source
	}
	if !leadHeading {
		d.Heading = doc.Path
	}
	d.MetaBits = []string{doc.Path, branch}
	if lastmod != "" {
		d.MetaBits = append(d.MetaBits, lastmod)
	}
	base := strings.Repeat("../", strings.Count(doc.Key, "/"))
	robots := ""
	if thin {
		robots = sitePageRobotsNoIndex
	}
	d.Chrome = sitePageChrome{
		Title:       title + " · " + site.Title,
		Robots:      robots,
		AccentCSS:   site.AccentCSS,
		Description: sitePageDescription(summary, doc.Path),
		OGTitle:     title,
		SiteTitle:   site.Title,
		Canonical:   site.URL + doc.Key,
		Route:       "file:" + doc.Path + "@" + branch,
		Base:        base,
		Image:       site.Image,
		Icon:        site.Icon,
		Feed:        site.URL + sitePagesFeedKey,
		Nav:         sitePageSidebar(base, sitePagesFilesDir, site.Files),
	}
	return d
}

// siteFileHead returns the leading source a title and description are read
// from, cut at a line boundary so no block rule sees half a line.
func siteFileHead(source string) string {
	head, _ := siteMDTruncateSource(source, sitePagesFileHeadMax)
	return head
}

// siteFilePageTitle resolves a document's site-unique <title>: its leading H1
// when nothing else on the site already carries that title, else its path,
// which is unique by construction.
func siteFilePageTitle(doc siteFileDoc, source string, taken map[string]bool) string {
	title, _, _ := siteFileMeta(siteFileHead(source), doc.Markdown)
	if title == "" || taken[title] {
		return doc.Path
	}
	return title
}

// maintainSiteFilePages writes the file layer for one pass and returns its new
// published state plus what is left of the page budget. Only documents whose
// blob or key moved are re-read and re-rendered (regen forces every page, for
// the head changes a version or site-identity change carries), documents that
// left the tree have their pages swept, and a pass where nothing moved writes
// nothing at all. Budget exhaustion leaves a valid published prefix and the
// state's Pending flag brings the next push back here.
func maintainSiteFilePages(client *Client, prefix string, site sitePageSite, prior *siteFilesState, pass siteFilePass, taken map[string]bool, regen bool, budget int, progress Progress) (*siteFilesState, int, error) {
	docs, branch, tip, src := pass.docs, pass.branch, pass.tip, pass.src
	priorFiles := map[string]siteFileRecord{}
	if prior != nil {
		priorFiles = prior.Files
	}
	commitTimes := siteFileLastCommitTimes(src, tip)
	if len(docs) == 0 && len(priorFiles) == 0 {
		return nil, budget, nil
	}
	state := &siteFilesState{Branch: branch, Files: map[string]siteFileRecord{}}
	// A branch change moves every page's route and meta line, so it rewrites the
	// whole set exactly as a version or identity change does.
	regen = regen || prior == nil || prior.Branch != branch
	changed := regen
	titles := map[string]bool{}
	for t := range taken {
		titles[t] = true
	}
	var uploads []sitePageUpload
	for _, doc := range docs {
		old, published := priorFiles[doc.Path]
		carry := func() {
			if published {
				state.Files[doc.Path] = old
				titles[old.Title] = true
			}
		}
		if published && old.Blob == doc.Blob && old.Key == doc.Key && !regen {
			carry()
			continue
		}
		changed = true
		if budget <= 0 {
			state.Pending = true
			carry()
			continue
		}
		body, ok := src.object(doc.Blob, "blob")
		if !ok {
			carry()
			continue
		}
		source, truncated := siteMDTruncateSource(string(body), sitePagesFileMax)
		if siteFileExt(doc.Path) == ".mdx" {
			source = siteFileStripMDX(source)
		}
		title := siteFilePageTitle(doc, source, titles)
		lastmod := sitePageDate(commitTimes[doc.Path])
		thin := siteFileWordCount(source, doc.Markdown) < sitePagesFileMinWords
		page, err := renderSitePage("file", buildSiteFilePage(doc, source, truncated, site, branch, title, lastmod, thin))
		if err != nil {
			return nil, budget, err
		}
		uploads = append(uploads, sitePageUpload{key: prefix + doc.Key, page: page})
		state.Files[doc.Path] = siteFileRecord{Blob: doc.Blob, Key: doc.Key, Title: title, Lastmod: lastmod, Thin: thin}
		titles[title] = true
		budget--
	}
	if err := putSitePages(client, uploads, progress, "site file pages", 0, len(uploads)); err != nil {
		return nil, budget, err
	}
	for p, old := range priorFiles {
		if r, ok := state.Files[p]; ok && r.Key == old.Key {
			continue
		}
		changed = true
		if err := client.Delete(prefix + old.Key); err != nil {
			return nil, budget, fmt.Errorf("delete file page %s: %w", old.Key, err)
		}
	}
	if !changed {
		return state, budget, nil
	}
	if len(state.Files) == 0 {
		if err := client.Delete(prefix + sitePagesFilesDir + "/index.html"); err != nil {
			return nil, budget, fmt.Errorf("delete file index page: %w", err)
		}
		return nil, budget, nil
	}
	page, err := renderSitePage("list", buildSiteFilesIndexPage(site, state, branch))
	if err != nil {
		return nil, budget, err
	}
	if err := putSitePage(client, prefix+sitePagesFilesDir+"/index.html", page); err != nil {
		return nil, budget, err
	}
	return state, budget, nil
}

// siteFileRows returns the published documents path-ordered, for the list page
// and the sitemap.
func siteFileRows(state *siteFilesState) []string {
	if state == nil {
		return nil
	}
	paths := make([]string, 0, len(state.Files))
	for p := range state.Files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	return paths
}

// buildSiteFilesIndexPage assembles f/index.html, the list of published
// documents; it takes the shared list template and the chained-page shape with
// nothing to chain (the set is a tree, not a timeline).
func buildSiteFilesIndexPage(site sitePageSite, state *siteFilesState, branch string) siteListPageData {
	paths := siteFileRows(state)
	entries := make([]sitePageListEntry, 0, len(paths))
	for _, p := range paths {
		r := state.Files[p]
		meta := []string{}
		if r.Title != p {
			meta = append(meta, p)
		}
		if r.Lastmod != "" {
			meta = append(meta, r.Lastmod)
		}
		entries = append(entries, sitePageListEntry{
			Glyph:      siteFilesList.Glyph,
			GlyphClass: "tg-file",
			GlyphTitle: "file",
			Href:       strings.TrimPrefix(r.Key, sitePagesFilesDir+"/"),
			Title:      r.Title,
			Meta:       meta,
		})
	}
	metaBits := []string{fmt.Sprintf("%d %s", len(entries), siteFilesList.Label)}
	if branch != "" {
		metaBits = append(metaBits, branch)
	}
	d := siteChainedListPage(siteFilesList.NavLabel, entries, append(metaBits, "by path"), 0, 0)
	d.Chrome = sitePageChrome{
		Title:       siteFilesList.NavLabel + " · " + site.Title,
		AccentCSS:   site.AccentCSS,
		Description: sitePageDescription(siteFilesList.NavLabel+" of "+site.Title+", by path.", ""),
		OGTitle:     siteFilesList.NavLabel + " · " + site.Title,
		SiteTitle:   site.Title,
		Canonical:   site.URL + sitePagesFilesDir + "/index.html",
		Route:       siteFilesList.Route,
		Base:        "../",
		Image:       site.Image,
		Icon:        site.Icon,
		Feed:        site.URL + sitePagesFeedKey,
		Nav:         sitePageSidebar("../", sitePagesFilesDir, site.Files),
	}
	return d
}

// buildSiteFilesSitemapEntries collects the file layer's sitemap URLs: the list
// page and every published document, each dated by the last commit that touched
// it. An empty layer submits nothing, like a type list with nothing to list.
func buildSiteFilesSitemapEntries(state *siteFilesState, site sitePageSite) []siteSitemapEntry {
	paths := siteFileRows(state)
	if len(paths) == 0 {
		return nil
	}
	newest := ""
	entries := make([]siteSitemapEntry, 0, len(paths)+1)
	for _, p := range paths {
		r := state.Files[p]
		if r.Thin {
			continue
		}
		if r.Lastmod > newest {
			newest = r.Lastmod
		}
		entries = append(entries, siteSitemapEntry{loc: site.URL + r.Key, lastmod: r.Lastmod})
	}
	return append([]siteSitemapEntry{{loc: site.URL + sitePagesFilesDir + "/index.html", lastmod: newest}}, entries...)
}

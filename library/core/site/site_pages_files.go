// site_pages_files.go - the crawlable file-page layer: one page per prose document, plus its list

package site

import (
	"fmt"
	"html/template"
	"os/exec"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/gitsocial-org/gitsocial/library/core/objstore"
)

const (
	// sitePagesFilesDir is the file pages' bucket directory.
	sitePagesFilesDir = "f"
	// sitePagesFileMax caps one published document's source bytes.
	sitePagesFileMax = 256 * 1024
	// sitePagesFileMinWords is the floor below which a document is published but not submitted to the sitemap.
	sitePagesFileMinWords = 100
	// sitePagesFileHeadMax bounds the source parsed for a title and description.
	sitePagesFileHeadMax = 8 * 1024
)

// siteFilesList describes the file directory the way sitePageList describes a type dir, so the shared template and sidebar serve it.
var siteFilesList = sitePageList{Dir: sitePagesFilesDir, Label: "files", Route: "/code", NavLabel: "Files", Glyph: "❯", Section: "Repository"}

// siteFileDocExts maps a prose extension to whether it renders as markdown.
var siteFileDocExts = map[string]bool{
	".md": true, ".markdown": true, ".mdown": true, ".mdx": true,
}

// siteFileDocNames are the extensionless basenames convention makes documents.
var siteFileDocNames = map[string]bool{
	"authors": true, "changelog": true, "changes": true, "contributing": true,
	"contributors": true, "copying": true, "install": true, "maintainers": true,
	"notice": true, "readme": true, "security": true, "todo": true,
}

// siteFileLicensePrefix matches a license file in either spelling, since the misspell linter rewrites one of them.
const siteFileLicensePrefix = "licen"

// siteFileSkipDirs are the directories no site publishes documents from.
var siteFileSkipDirs = map[string]bool{
	"node_modules": true, "vendor": true, "testdata": true, "third_party": true,
	"fixtures": true, "golden": true, "corpus": true, "snapshots": true, "__snapshots__": true,
}

// siteFileRecord is one published document's state: its blob, key, resolved title and last-commit date.
type siteFileRecord struct {
	Blob    string `json:"blob"`
	Key     string `json:"key"`
	Title   string `json:"title,omitempty"`
	Lastmod string `json:"lastmod,omitempty"`
	Thin    bool   `json:"thin,omitempty"`
}

// siteFilesState is the file layer's published set: the branch, one record per path, and whether the budget cut the pass short.
type siteFilesState struct {
	Branch  string                    `json:"branch"`
	Files   map[string]siteFileRecord `json:"files,omitempty"`
	Pending bool                      `json:"pending,omitempty"`
}

// siteFilePass is what one pass needs: the documents the default branch carries and the odb they are read from.
type siteFilePass struct {
	docs   []siteFileDoc
	branch string
	tip    string
	src    *objstore.LocalCommitSource
}

// siteFileDoc is one discovered document: its path, blob, bucket key, and whether it renders as markdown.
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

// sitePagesFilesPending reports whether the file layer still owes work a later push must finish.
func sitePagesFilesPending(m *sitePagesManifest) bool {
	return m != nil && m.Files != nil && m.Files.Pending
}

// sitePagesFilesState returns the manifest's recorded file-layer state.
func sitePagesFilesState(m *sitePagesManifest) *siteFilesState {
	if m == nil {
		return nil
	}
	return m.Files
}

// siteFileDocsChanged reports whether the discovered set differs from the published one; it is the layer's own moved signal, since no tip need have moved.
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

// siteFilesMoved reports whether a pass changed the published set, which is what rewrites the sitemap.
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

// sitePagesHasFiles reports whether the page set carries file pages, which puts the Files entry in every sidebar.
func sitePagesHasFiles(m *sitePagesManifest) bool {
	return m != nil && m.Files != nil && len(m.Files.Files) > 0
}

// siteFileTitleSet returns the titles the published file pages hold, so item-page titles can avoid them.
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

// siteFileIsDocument reports whether a repo path is prose this layer publishes, and whether it renders as markdown.
func siteFileIsDocument(p string) (markdown, ok bool) {
	name := path.Base(p)
	if ext := siteFileExt(name); ext != "" {
		markdown, ok = siteFileDocExts[ext]
		return markdown, ok
	}
	lower := strings.ToLower(name)
	return false, siteFileDocNames[lower] || strings.HasPrefix(lower, siteFileLicensePrefix)
}

// siteFileGlobMatch matches a path against a glob segment by segment, where "**" spans any number of segments.
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

// siteFileWalk collects the default-branch tree's blobs, pruning what no site publishes from; ok is false when the tree cannot be read at all.
func siteFileWalk(src *objstore.LocalCommitSource, tip string) (files []siteFileDoc, ok bool) {
	if tip == "" {
		return nil, false
	}
	body, ok := src.Object(tip+"^{tree}", "tree")
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
				sub, found := src.Object(row.SHA, "tree")
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

// selectSiteFileDocs applies the publication rules to a walked tree and returns the documents path-ordered, each with the key it owns.
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
	// index.html is the layer's own list page, so a root index.md takes the fallback key.
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

// siteFileRoutablePath reports whether a repo path can be both a URL and a file route; a path with a reserved character would boot somewhere else.
func siteFileRoutablePath(p string) bool {
	return !strings.ContainsAny(p, " @:#?%\\\"<>") && strings.IndexFunc(p, func(r rune) bool { return r < 0x20 }) < 0
}

// siteFilePageKey maps a repo path to its page key: the extension swapped for .html, or .html appended.
func siteFilePageKey(p string) string {
	if ext := siteFileExt(p); ext != "" {
		return strings.TrimSuffix(p, path.Ext(p)) + ".html"
	}
	return p + ".html"
}

// discoverSiteFileDocs resolves the documents the default branch publishes; known is false when the tree is unreadable, so the caller carries its published set forward.
func discoverSiteFileDocs(src *objstore.LocalCommitSource, tip string, cfg siteCustomization) (docs []siteFileDoc, known bool) {
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

// siteFileDocsFromState rebuilds the discovered set from the published state, which a pass with an unreadable tree works from.
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

// siteFileStripMDX drops the import, export and standalone JSX lines the markdown grammar has no rule for.
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

// siteFileLastCommitTimes maps every path to its newest commit time in one history walk, not a git log per path.
func siteFileLastCommitTimes(src *objstore.LocalCommitSource, tip string) map[string]int64 {
	times := map[string]int64{}
	if src == nil || tip == "" {
		return times
	}
	args := []string{}
	if workdir := src.Workdir(); workdir != "" {
		args = append(args, "-C", workdir)
	}
	args = append(args, "log", "--format=%ct", "--name-only", "--no-renames", tip)
	cmd := exec.Command("git", args...)
	cmd.Env = append(cmd.Environ(), "GIT_NO_LAZY_FETCH=1")
	if gitDir := src.GitDir(); gitDir != "" {
		cmd.Env = append(cmd.Env, "GIT_DIR="+gitDir)
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

// siteFileMeta extracts a document's title and description source: a leading H1 titles the page, anything else is titled by its path.
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

// buildSiteFilePage assembles one document's page: its rendered or preformatted body, its meta line, and the file route the boot hook stamps.
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

// siteFileHead returns the leading source a title and description are read from, cut at a line boundary.
func siteFileHead(source string) string {
	head, _ := siteMDTruncateSource(source, sitePagesFileHeadMax)
	return head
}

// siteFilePageTitle resolves a document's site-unique title: its leading H1 when nothing else holds it, else its path.
func siteFilePageTitle(doc siteFileDoc, source string, taken map[string]bool) string {
	title, _, _ := siteFileMeta(siteFileHead(source), doc.Markdown)
	if title == "" || taken[title] {
		return doc.Path
	}
	return title
}

// maintainSiteFilePages writes the file layer for one pass and returns its new state plus what is left of the budget; only a moved blob or key is re-rendered.
func maintainSiteFilePages(client *objstore.Client, prefix string, site sitePageSite, prior *siteFilesState, pass siteFilePass, taken map[string]bool, regen bool, budget int, progress objstore.Progress) (*siteFilesState, int, error) {
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
	// A branch change moves every page's route and meta line, so it rewrites the whole set.
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
		body, ok := src.Object(doc.Blob, "blob")
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

// siteFileRows returns the published documents path-ordered, for the list page and the sitemap.
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

// buildSiteFilesIndexPage assembles f/index.html from the shared list template, with nothing to chain.
func buildSiteFilesIndexPage(site sitePageSite, state *siteFilesState, branch string) siteListPageData {
	paths := siteFileRows(state)
	entries := make([]sitePageListEntry, 0, len(paths))
	for _, p := range paths {
		r := state.Files[p]
		meta := []sitePageBit{}
		if r.Title != p {
			meta = append(meta, sitePageTextBit(p))
		}
		if r.Lastmod != "" {
			meta = append(meta, sitePageTextBit(r.Lastmod))
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
	d := siteChainedListPage(siteFilesList, entries, append(metaBits, "by path"), 0, 0)
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

// buildSiteFilesSitemapEntries collects the file layer's sitemap URLs, each dated by the last commit that touched it.
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

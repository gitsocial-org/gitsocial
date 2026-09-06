// site_pages_files_test.go - the file-page layer against the in-process S3 stub:
// discovery over SYNTHETIC trees (the rule ships to every repo, so nothing here
// may pass only because it was written against this one), key mapping and its
// collision fallback, the glob escape hatch, markdown vs preformatted rendering,
// the index page and sitemap coverage, the incremental no-op and sweep, the
// budget resume, and the disable-path deletion.

package objstore

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// pagesFileRepo builds a one-commit repo carrying the given files and returns
// its directory with the sha of that commit.
func pagesFileRepo(t *testing.T, files map[string]string) (dir, head string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir = t.TempDir()
	writeRepoFiles(t, dir, files)
	gitRun(t, dir, "init", "-q", "-b", "main")
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-q", "-m", "seed")
	return dir, gitRun(t, dir, "rev-parse", "HEAD")
}

// writeRepoFiles writes a path→content map into a working tree.
func writeRepoFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for p, body := range files {
		full := filepath.Join(dir, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// commitRepoFiles writes more files (or rewrites some) and commits, returning
// the new head.
func commitRepoFiles(t *testing.T, dir string, files map[string]string, removed ...string) string {
	t.Helper()
	writeRepoFiles(t, dir, files)
	for _, p := range removed {
		if err := os.Remove(filepath.Join(dir, filepath.FromSlash(p))); err != nil {
			t.Fatal(err)
		}
	}
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-q", "-m", "edit")
	return gitRun(t, dir, "rev-parse", "HEAD")
}

// buildFilePages points the bucket's default branch at head and runs one page
// pass against the repo's odb, returning whether the layer still owes work.
func buildFilePages(t *testing.T, client *Client, dir, head string, site map[string]any) bool {
	t.Helper()
	seedPagesConfig(t, client, site)
	if err := client.Put("refs/heads/main", []byte(head+"\n")); err != nil {
		t.Fatal(err)
	}
	refs := pagesRefs(client, t)
	for _, ext := range siteItemsExts {
		if tip, ok := refs["refs/heads/gitmsg/"+ext]; ok {
			if err := updateSiteItemsIndex(client, "", ext, tip, nil); err != nil {
				t.Fatalf("items index %s: %v", ext, err)
			}
		}
	}
	src := newLocalCommitSource("", dir)
	defer src.close()
	// The code index is what a real push maintains before the pages (rebuildSiteItems),
	// and its tip is what tells the pages a branch moved.
	if err := updateSiteCodeIndex(client, "", codeBranchTips(refs, "main"), "main", &siteProgress{ext: siteCodeExt, src: src}); err != nil {
		t.Fatalf("code index: %v", err)
	}
	pending, _, err := rebuildSitePages(client, "", pagesRefs(client, t), "main", src, nil, SiteOverride{})
	if err != nil {
		t.Fatalf("rebuildSitePages: %v", err)
	}
	return pending
}

func TestSiteFilePages_Discovery(t *testing.T) {
	dir, _ := pagesFileRepo(t, map[string]string{
		"README.md":              "# Root Readme\n\nthe front page owns this.\n",
		"handbook/Guide.md":      "# Handbook Guide\n\nhow the thing works. the handbook explains each step in enough detail to be a real page rather than a stub. the handbook explains each step in enough detail to be a real page rather than a stub. the handbook explains each step in enough detail to be a real page rather than a stub. the handbook explains each step in enough detail to be a real page rather than a stub. the handbook explains each step in enough detail to be a real page rather than a stub. the handbook explains each step in enough detail to be a real page rather than a stub. the handbook explains each step in enough detail to be a real page rather than a stub. the handbook explains each step in enough detail to be a real page rather than a stub. the handbook explains each step in enough detail to be a real page rather than a stub. the handbook explains each step in enough detail to be a real page rather than a stub. the handbook explains each step in enough detail to be a real page rather than a stub. the handbook explains each step in enough detail to be a real page rather than a stub. \n",
		"handbook/deep/Guide.md": "# Deep Guide\n\na different guide, same basename.\n",
		"notes.txt":              "plain text notes\nsecond line\n",
		"handbook/Mdx.mdx":       "import Tabs from '@site/Tabs'\n\n# Mdx Guide\n\n<Tabs>\n\nreal prose the jsx wraps.\n",
		"fixtures/sample.md":     "# Fixture\n",
		"LICENSE":                "All rights reserved.\n",
		"handbook/app.py":        "print('hi')\n",
		".github/WORKFLOW.md":    "# Workflow\n\nnot a document.\n",
		"node_modules/a/dep.md":  "# Dep\n\nsomeone else's.\n",
		"vendor/v.md":            "# Vendored\n",
		"testdata/fixture.md":    "# Fixture\n",
		"handbook/a name.md":     "# Unroutable\n\na path no file route can carry.\n",
	})
	if err := os.Symlink("README.md", filepath.Join(dir, "LINK.md")); err != nil {
		t.Fatal(err)
	}
	head := commitRepoFiles(t, dir, nil)
	client, _ := testClient(t)
	if buildFilePages(t, client, dir, head, pagesTestSite()) {
		t.Fatal("the pass must complete")
	}
	if keyExists(client, "f/LINK.html") {
		t.Error("a symlink's blob is a target path, not a document")
	}

	for _, key := range []string{"f/handbook/Guide.html", "f/handbook/deep/Guide.html", "f/handbook/Mdx.html", "f/LICENSE.html", "f/index.html"} {
		if !keyExists(client, key) {
			t.Errorf("%s must be published", key)
		}
	}
	for _, key := range []string{"f/README.html", "f/handbook/app.html", "f/.github/WORKFLOW.html", "f/node_modules/a/dep.html", "f/vendor/v.html", "f/testdata/fixture.html", "f/handbook/a name.html", "f/notes.html", "f/fixtures/sample.html"} {
		if keyExists(client, key) {
			t.Errorf("%s must not be published", key)
		}
	}

	page := getKey(t, client, "f/handbook/Guide.html")
	if !strings.Contains(page, `<meta name="gs-route" content="file:handbook/Guide.md@main">`) {
		t.Error("a file page must boot the app's file route on its own path and branch")
	}
	if !strings.Contains(page, `<link rel="canonical" href="https://example.com/f/handbook/Guide.html">`) {
		t.Error("a file page must self-canonicalize at its own key")
	}
	if !strings.Contains(page, "<title>Handbook Guide · Pages Test</title>") {
		t.Error("the leading H1 must title the page")
	}
	if !strings.Contains(page, `<h1 id="md-handbook-guide">Handbook Guide</h1>`) || !strings.Contains(page, "<p>how the thing works.") {
		t.Error("markdown must be rendered, not escaped")
	}
	if strings.Count(page, "Handbook Guide</h1>") != 1 {
		t.Error("the rendered H1 must not be repeated by a page heading of its own")
	}
	if !strings.Contains(page, `<div id="gs-page" data-base="../../">`) {
		t.Error("a nested file page's base must climb back to the site root")
	}
	if !strings.Contains(page, `<meta name="description" content="how the thing works.`) {
		t.Error("the first paragraph must be the description")
	}

	text := getKey(t, client, "f/LICENSE.html")
	if !strings.Contains(text, "<pre>All rights reserved.\n</pre>") {
		t.Error("a non-markdown document must render preformatted")
	}
	if !strings.Contains(text, "<h1>LICENSE</h1>") {
		t.Error("a document with no leading H1 is headed by its path")
	}

	mdx := getKey(t, client, "f/handbook/Mdx.html")
	if strings.Contains(mdx, "import Tabs") || strings.Contains(mdx, "&lt;Tabs&gt;") {
		t.Error("mdx imports and standalone jsx must not reach the page")
	}
	if !strings.Contains(mdx, "real prose the jsx wraps.") {
		t.Error("mdx prose must survive the strip")
	}

	index := getKey(t, client, "f/index.html")
	for _, want := range []string{"4 files", `href="handbook/Guide.html"`, `href="handbook/deep/Guide.html"`, "Handbook Guide", "Deep Guide", "LICENSE"} {
		if !strings.Contains(index, want) {
			t.Errorf("the file index must carry %q", want)
		}
	}
	sitemap := getKey(t, client, sitePagesSitemapKey)
	for _, want := range []string{"https://example.com/f/index.html", "https://example.com/f/handbook/Guide.html"} {
		if !strings.Contains(sitemap, "<loc>"+want+"</loc>") {
			t.Errorf("the sitemap must submit %q", want)
		}
	}
	if strings.Contains(sitemap, "<loc>https://example.com/f/LICENSE.html</loc>") {
		t.Error("a document under the thin floor must not be submitted")
	}
	if !strings.Contains(text, `<meta name="robots" content="noindex,follow">`) {
		t.Error("a thin document is published but asks not to be indexed")
	}
	if strings.Contains(getKey(t, client, "f/handbook/Guide.html"), `name="robots"`) {
		t.Error("a document over the floor carries no robots tag")
	}
	if !strings.Contains(sitemap, "<loc>https://example.com/f/handbook/Guide.html</loc><lastmod>") {
		t.Error("a file page must carry the last commit that touched it as its lastmod")
	}
	if !strings.Contains(index, `href="../index.html"`) || !strings.Contains(getKey(t, client, sitePagesFrontKey), `href="./f/index.html"`) {
		t.Error("the sidebar must link the file index once the layer has pages")
	}
	if got := cacheControlForKey("f/handbook/Guide.html"); got != cacheControlRevalidate {
		t.Errorf("file pages must revalidate, got %q", got)
	}
}

// TestSiteFilePages_NoDocuments: a repo whose only prose is its root README
// publishes no file layer at all — no pages, no index, and no sidebar entry
// pointing at a page that does not exist.
func TestSiteFilePages_NoDocuments(t *testing.T) {
	dir, head := pagesFileRepo(t, map[string]string{
		"README.md": "# Only Readme\n\nnothing else here.\n",
		"main.go":   "package main\n",
	})
	client, _ := testClient(t)
	if buildFilePages(t, client, dir, head, pagesTestSite()) {
		t.Fatal("the pass must complete")
	}
	if keyExists(client, "f/index.html") || keyExists(client, "f/README.html") {
		t.Error("a repo with no documents must publish no file pages")
	}
	front := getKey(t, client, sitePagesFrontKey)
	if strings.Contains(front, "f/index.html") {
		t.Error("the sidebar must not carry a Files entry with nothing behind it")
	}
	if strings.Contains(getKey(t, client, sitePagesSitemapKey), "/f/") {
		t.Error("the sitemap must not submit an absent file layer")
	}
	manifest, err := readSitePagesManifest(client, "")
	if err != nil || manifest == nil {
		t.Fatalf("pages manifest: %v", err)
	}
	if manifest.Files != nil {
		t.Error("the manifest must record no file state")
	}
}

// TestSiteFilePages_ConfigGlobs: the escape hatch decides both ways — an exclude
// glob withholds a document the convention took, an include glob publishes a
// file the extension rule does not know.
func TestSiteFilePages_ConfigGlobs(t *testing.T) {
	dir, head := pagesFileRepo(t, map[string]string{
		"README.md":          "# Readme\n",
		"docs/keep.md":       "# Keep\n\nkeep this one.\n",
		"ui/embedded.md":     "# Embedded\n\nstrings the program prints.\n",
		"handbook/page.text": "# Recovered\n\nan extension the rule misses.\n",
	})
	client, _ := testClient(t)
	if buildFilePages(t, client, dir, head, pagesTestSite()) {
		t.Fatal("the pass must complete")
	}
	if !keyExists(client, "f/ui/embedded.html") {
		t.Fatal("without a glob the convention publishes every document it finds")
	}
	// Config alone, with no branch or corpus tip moving: the layer must still act
	// on it, since the globs move the document set and nothing else signals that.
	site := pagesTestSite()
	site["filesExclude"] = "ui/**"
	site["filesInclude"] = "handbook/**/*.text"
	if buildFilePages(t, client, dir, head, site) {
		t.Fatal("the pass must complete")
	}
	if !keyExists(client, "f/docs/keep.html") {
		t.Error("an ordinary document must still be published")
	}
	if keyExists(client, "f/ui/embedded.html") {
		t.Error("an excluded document must not be published")
	}
	if !keyExists(client, "f/handbook/page.html") {
		t.Error("an included file must be published even though the extension rule misses it")
	}
}

// TestSiteFilePages_Incremental: only what moved is rewritten. An unchanged tree
// costs zero writes, an edited document rewrites its page alone, and a deleted
// one has its page swept.
func TestSiteFilePages_Incremental(t *testing.T) {
	dir, head := pagesFileRepo(t, map[string]string{
		"README.md":      "# Readme\n",
		"docs/one.md":    "# One\n\nfirst.\n",
		"docs/two.md":    "# Two\n\nsecond.\n",
		"docs/gone.md":   "# Gone\n\nleaving.\n",
		"docs/notes.txt": "notes\n",
	})
	client, bucket := testClient(t)
	buildFilePages(t, client, dir, head, pagesTestSite())
	one, two, index := bucket.putCount("f/docs/one.html"), bucket.putCount("f/docs/two.html"), bucket.putCount("f/index.html")
	if one != 1 || index != 1 {
		t.Fatalf("first pass wrote one=%d index=%d, want 1/1", one, index)
	}

	// A pass the gitmsg side made incremental, so the layer runs and decides for
	// itself that the tree it walked is the tree it published.
	seedSocialMessages(t, client, "", []pageMsgSpec{{msg: "a post that moves the gitmsg tip"}})
	if buildFilePages(t, client, dir, head, pagesTestSite()) {
		t.Fatal("the second pass must complete")
	}
	if bucket.putCount("f/docs/one.html") != one || bucket.putCount("f/index.html") != index {
		t.Error("a pass over an unchanged tree must write nothing")
	}

	next := commitRepoFiles(t, dir, map[string]string{"docs/one.md": "# One\n\nfirst, revised.\n"}, "docs/gone.md")
	if buildFilePages(t, client, dir, next, pagesTestSite()) {
		t.Fatal("the third pass must complete")
	}
	if bucket.putCount("f/docs/one.html") != one+1 {
		t.Error("the edited document's page must be rewritten")
	}
	if bucket.putCount("f/docs/two.html") != two {
		t.Error("an untouched document's page must not be rewritten")
	}
	if !strings.Contains(getKey(t, client, "f/docs/one.html"), "first, revised.") {
		t.Error("the rewritten page must carry the new content")
	}
	if keyExists(client, "f/docs/gone.html") {
		t.Error("a document that left the tree must have its page swept")
	}
	if strings.Contains(getKey(t, client, sitePagesSitemapKey), "f/docs/gone.html") {
		t.Error("a swept page must leave the sitemap")
	}
}

// TestSiteFilePages_UnreadableTree: a pusher whose odb cannot serve the tree
// (no local repo, a shallow clone) must change nothing. An unreadable tree is
// not an empty one, and reading it as one would sweep the whole layer and drop
// the Files entry out of every page's sidebar.
func TestSiteFilePages_UnreadableTree(t *testing.T) {
	dir, head := pagesFileRepo(t, map[string]string{"README.md": "# Readme\n", "docs/one.md": "# One\n\nkeep me.\n"})
	client, _ := testClient(t)
	buildFilePages(t, client, dir, head, pagesTestSite())

	seedSocialMessages(t, client, "", []pageMsgSpec{{msg: "a post from a machine with no checkout"}})
	refs := pagesRefs(client, t)
	if err := updateSiteItemsIndex(client, "", "social", refs["refs/heads/gitmsg/social"], nil); err != nil {
		t.Fatal(err)
	}
	if pending, _, err := rebuildSitePages(client, "", pagesRefs(client, t), "main", nil, nil, SiteOverride{}); err != nil || pending {
		t.Fatalf("rebuildSitePages: pending=%v err=%v", pending, err)
	}
	if !keyExists(client, "f/docs/one.html") || !keyExists(client, "f/index.html") {
		t.Error("an unreadable tree must leave the published file pages alone")
	}
	if !strings.Contains(getKey(t, client, sitePagesFrontKey), `href="./f/index.html"`) {
		t.Error("the sidebar must keep its Files entry")
	}
}

// TestSiteFilePages_BudgetResume: a budget too small for the document set leaves
// a valid published prefix, reports the layer as pending, and the next pass
// finishes it.
func TestSiteFilePages_BudgetResume(t *testing.T) {
	prev := sitePagesBudget
	defer func() { sitePagesBudget = prev }()
	sitePagesBudget = 1
	dir, head := pagesFileRepo(t, map[string]string{
		"README.md":   "# Readme\n",
		"docs/a.md":   "# A\n",
		"docs/b.md":   "# B\n",
		"docs/c.md":   "# C\n",
		"CHANGELOG":   "changes\n",
		"docs/d.md":   "# D\n",
		"docs/e.md":   "# E\n",
		"docs/f/g.md": "# G\n",
	})
	client, _ := testClient(t)
	if !buildFilePages(t, client, dir, head, pagesTestSite()) {
		t.Fatal("a budget-limited pass must report pending")
	}
	if !keyExists(client, "f/CHANGELOG.html") || keyExists(client, "f/docs/e.html") {
		t.Error("the first pass must publish a path-ordered prefix, not an arbitrary subset")
	}
	if !strings.Contains(getKey(t, client, "f/index.html"), "1 files") {
		t.Error("the index must claim only what exists")
	}
	sitePagesBudget = prev
	if buildFilePages(t, client, dir, head, pagesTestSite()) {
		t.Fatal("the resuming pass must complete")
	}
	for _, key := range []string{"f/CHANGELOG.html", "f/docs/a.html", "f/docs/e.html", "f/docs/f/g.html"} {
		if !keyExists(client, key) {
			t.Errorf("%s must be published once the budget allows", key)
		}
	}
	if !strings.Contains(getKey(t, client, "f/index.html"), "7 files") {
		t.Error("the resumed index must list the whole set")
	}
}

// TestSiteFilePages_Disable: turning the page layer off sweeps the file pages
// with everything else.
func TestSiteFilePages_Disable(t *testing.T) {
	dir, head := pagesFileRepo(t, map[string]string{"README.md": "# Readme\n", "docs/one.md": "# One\n"})
	client, _ := testClient(t)
	buildFilePages(t, client, dir, head, pagesTestSite())
	if !keyExists(client, "f/docs/one.html") {
		t.Fatal("the document must be published first")
	}
	off := pagesTestSite()
	off["pages"] = "false"
	buildFilePages(t, client, dir, head, off)
	for _, key := range []string{"f/docs/one.html", "f/index.html", sitePagesManifestKey} {
		if keyExists(client, key) {
			t.Errorf("%s must be swept when the page layer is disabled", key)
		}
	}
}

// TestSiteFilePages_TitlesStayUnique: two documents whose H1 is the same word,
// and an item whose subject is that word too, must not share a <title>.
func TestSiteFilePages_TitlesStayUnique(t *testing.T) {
	dir, head := pagesFileRepo(t, map[string]string{
		"README.md":   "# Readme\n",
		"docs/a.md":   "# Roadmap\n\nthe plan.\n",
		"other/b.md":  "# Roadmap\n\nanother plan.\n",
		"docs/own.md": "# Own Title\n\nunshared.\n",
	})
	client, _ := testClient(t)
	seedSocialMessages(t, client, "", []pageMsgSpec{{msg: "Own Title\n\nan item that wants the same name"}})
	buildFilePages(t, client, dir, head, pagesTestSite())

	titles := map[string]string{}
	for _, key := range []string{"f/docs/a.html", "f/other/b.html", "f/docs/own.html"} {
		page := getKey(t, client, key)
		start := strings.Index(page, "<title>")
		title := page[start+len("<title>") : start+strings.Index(page[start:], "</title>")]
		if other, dup := titles[title]; dup {
			t.Errorf("%s and %s share the title %q", other, key, title)
		}
		titles[title] = key
	}
	if titles["Own Title · Pages Test"] != "" {
		t.Error("a file must not take a title an item page already published")
	}
}

func TestSiteFilePageKey(t *testing.T) {
	cases := map[string]string{
		"specs/GITMSG.md":   "specs/GITMSG.html",
		"docs/api/index.md": "docs/api/index.html",
		"LICENSE":           "LICENSE.html",
		"notes.txt":         "notes.html",
	}
	for in, want := range cases {
		if got := siteFilePageKey(in); got != want {
			t.Errorf("siteFilePageKey(%q) = %q, want %q", in, got, want)
		}
	}
	// Two documents whose keys collide: the first keeps the swapped key, the
	// second keeps its own extension so neither page is silently overwritten.
	docs := selectSiteFileDocs([]siteFileDoc{{Path: "notes.md"}, {Path: "notes.markdown"}, {Path: "index.md"}}, "", nil, nil)
	got := map[string]string{}
	for _, d := range docs {
		if other, dup := got[d.Key]; dup {
			t.Fatalf("%s and %s collide at %s", other, d.Path, d.Key)
		}
		got[d.Key] = d.Path
	}
	if got["f/notes.html"] != "notes.markdown" || got["f/notes.md.html"] != "notes.md" {
		t.Errorf("collision fallback = %v", got)
	}
	if got["f/index.md.html"] != "index.md" {
		t.Errorf("a root index.md must not claim the layer's own index page: %v", got)
	}
}

func TestSiteFileGlobMatch(t *testing.T) {
	cases := []struct {
		pattern, path string
		want          bool
	}{
		{"docs/*.md", "docs/a.md", true},
		{"docs/*.md", "docs/deep/a.md", false},
		{"docs/**/*.md", "docs/a.md", true},
		{"docs/**/*.md", "docs/deep/down/a.md", true},
		{"**/help.md", "library/ui/help.md", true},
		{"**/help.md", "help.md", true},
		{"ui/**", "ui/a/b.md", true},
		{"ui/**", "uix/a.md", false},
		{"*.md", "a.md", true},
		{"*.md", "docs/a.md", false},
	}
	for _, c := range cases {
		if got := siteFileGlobMatch(c.pattern, c.path); got != c.want {
			t.Errorf("siteFileGlobMatch(%q, %q) = %v, want %v", c.pattern, c.path, got, c.want)
		}
	}
}

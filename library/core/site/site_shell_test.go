// site_shell_test.go - the versioned shell: reference substitution, page regeneration and the revision sweep.
package site

import (
	"strings"
	"testing"
)

// TestSiteShellReferencesAreVersioned: the flipped index.html references every
// asset through the shell directory and announces it; no root reference stays.
func TestSiteShellReferencesAreVersioned(t *testing.T) {
	version, err := siteVersion()
	if err != nil {
		t.Fatalf("siteVersion: %v", err)
	}
	shellDir := shellDirFor(version)
	html, err := siteIndexHTML(shellDir)
	if err != nil {
		t.Fatalf("siteIndexHTML: %v", err)
	}
	page := string(html)
	for _, want := range []string{
		`href="` + shellDir + `pages-core.css"`,
		`href="` + shellDir + `pages-full.css"`,
		`src="` + shellDir + `icons.js"`,
		`src="` + shellDir + `gs-core.js"`,
		`src="` + shellDir + `gs-render.js"`,
		`src="` + shellDir + `gs-app.js"`,
		`window.GS_SHELL="` + shellDir + `"`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("index.html lacks %s", want)
		}
	}
	for _, stale := range []string{`href="pages-`, `src="gs-`, `src="icons.js"`} {
		if strings.Contains(page, stale) {
			t.Errorf("index.html keeps a root-level reference %q", stale)
		}
	}
}

// TestSitePageSiteHash_shellVersion: a shell bump regenerates the pages, since the hash folds the shell dir in.
func TestSitePageSiteHash_shellVersion(t *testing.T) {
	site := sitePageSite{Title: "t", URL: "https://example.com/"}
	if sitePageSiteHashWith(site, shellDirFor("aaaaaaaaaaaa")) == sitePageSiteHashWith(site, shellDirFor("bbbbbbbbbbbb")) {
		t.Error("site hash ignores the shell revision; a shell bump would not regenerate the pages")
	}
}

// TestSiteShellSweep_firstTransitionKeepsRoots: the first versioned push over
// a pre-versioning bucket keeps the root-level copies, since the old pages
// reference them until the budgeted regeneration finishes.
func TestSiteShellSweep_firstTransitionKeepsRoots(t *testing.T) {
	client, bucket := testClient(t)
	bucket.Seed("repo/"+siteVersionKey, []byte("feedfeedfeedfeedfeedfeedfeedfeedfeedfeedfeedfeedfeedfeedfeedfeed\n"))
	bucket.Seed("repo/gs-app.js", []byte("root-level, still referenced by old pages"))

	if err := uploadSiteFiles(client, "repo/"); err != nil {
		t.Fatalf("uploadSiteFiles: %v", err)
	}
	if _, ok := bucket.Object("repo/gs-app.js"); !ok {
		t.Error("the first transition must keep the root-level copies as grace")
	}
	prev, ok := bucket.Object("repo/" + siteShellPrevKey)
	if !ok || !strings.Contains(prev, "feedfeedfeed") {
		t.Errorf("shellprev marker = %q ok=%v, want the outgoing revision recorded", prev, ok)
	}
}

// TestSiteShellSweep_retriesFromPrevMarker: the sweep runs from the durable
// prev marker on every pass, so an interrupted sweep collects later, with no
// version change needed.
func TestSiteShellSweep_retriesFromPrevMarker(t *testing.T) {
	client, bucket := testClient(t)
	version, err := siteVersion()
	if err != nil {
		t.Fatalf("siteVersion: %v", err)
	}
	bucket.Seed("repo/"+siteVersionKey, []byte(version+"\n"))
	bucket.Seed("repo/"+siteShellPrevKey, []byte(siteShellPrefix+"feedfeedfeed/\n"))
	bucket.Seed("repo/"+siteShellPrefix+"feedfeedfeed/gs-app.js", []byte("previous"))
	bucket.Seed("repo/"+siteShellPrefix+"cafecafecafe/gs-app.js", []byte("ancient, missed by a crashed sweep"))
	bucket.Seed("repo/gs-app.js", []byte("root-level legacy"))

	if err := uploadSiteFiles(client, "repo/"); err != nil {
		t.Fatalf("uploadSiteFiles: %v", err)
	}
	if _, ok := bucket.Object("repo/" + siteShellPrefix + "cafecafecafe/gs-app.js"); ok {
		t.Error("a later pass must collect what a crashed sweep missed")
	}
	if _, ok := bucket.Object("repo/" + siteShellPrefix + "feedfeedfeed/gs-app.js"); !ok {
		t.Error("the recorded previous revision must survive")
	}
	if _, ok := bucket.Object("repo/gs-app.js"); ok {
		t.Error("with the previous revision versioned, the root copies must sweep")
	}
}

// TestSiteShellUpload_sweepsOldRevisions: a shell change keeps the previous
// revision, sweeps the ones before it and removes the root-level copies.
func TestSiteShellUpload_sweepsOldRevisions(t *testing.T) {
	client, bucket := testClient(t)
	bucket.Seed("repo/"+siteVersionKey, []byte("feedfeedfeedfeedfeedfeedfeedfeedfeedfeedfeedfeedfeedfeedfeedfeed\n"))
	bucket.Seed("repo/"+siteShellPrefix+"feedfeedfeed/gs-app.js", []byte("previous"))
	bucket.Seed("repo/"+siteShellPrefix+"cafecafecafe/gs-app.js", []byte("ancient"))
	bucket.Seed("repo/gs-app.js", []byte("root-level legacy"))

	if err := uploadSiteFiles(client, "repo/"); err != nil {
		t.Fatalf("uploadSiteFiles: %v", err)
	}
	version, err := siteVersion()
	if err != nil {
		t.Fatalf("siteVersion: %v", err)
	}
	if _, ok := bucket.Object("repo/" + shellDirFor(version) + "gs-app.js"); !ok {
		t.Error("current shell revision missing after upload")
	}
	if _, ok := bucket.Object("repo/" + siteShellPrefix + "feedfeedfeed/gs-app.js"); !ok {
		t.Error("the previous revision must survive one shell change")
	}
	if _, ok := bucket.Object("repo/" + siteShellPrefix + "cafecafecafe/gs-app.js"); ok {
		t.Error("a revision older than the previous one must be swept")
	}
	if _, ok := bucket.Object("repo/gs-app.js"); ok {
		t.Error("the root-level legacy copy must be swept")
	}
	if _, ok := bucket.Object("repo/index.html"); !ok {
		t.Error("index.html must stay at the root")
	}
}

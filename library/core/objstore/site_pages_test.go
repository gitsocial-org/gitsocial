// site_pages_test.go - the static HTML page layer against the in-process S3
// stub (memBucket): guards + disable-path deletion, sealed-list-page overflow
// on both the bootstrap and incremental paths, hostile-string escaping,
// thread/edit/retract resolution (incl. the ts+hash tiebreak), thread cap
// truncation, manifest diff/partition (new root vs reply→root regeneration),
// bootstrap cursor resume under a small budget, sitemap/robots coverage and
// index mode, the front-page README, and the pages-aware push-state marker.

package objstore

import (
	"crypto/sha1"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// makePageCommit builds a loose commit with a controllable author identity (the
// pages render author names, so escaping tests need hostile ones).
func makePageCommit(t *testing.T, parent, author, email, message string, ts int64) (string, []byte) {
	t.Helper()
	body := "tree " + emptyTree + "\n"
	if parent != "" {
		body += "parent " + parent + "\n"
	}
	body += fmt.Sprintf("author %s <%s> %d +0000\n", author, email, ts)
	body += fmt.Sprintf("committer %s <%s> %d +0000\n\n", author, email, ts)
	body += message
	sha := fmt.Sprintf("%x", sha1.Sum([]byte(fmt.Sprintf("commit %d\x00%s", len(body), body))))
	loose, err := encodeLooseObject("commit", []byte(body))
	if err != nil {
		t.Fatalf("encodeLooseObject: %v", err)
	}
	return sha, loose
}

// pageMsgSpec is one seeded social commit: its message and author identity.
type pageMsgSpec struct {
	msg    string
	author string
	email  string
	ts     int64
}

// seedExtMessages appends the given messages as a linear chain on top of
// parent, uploads the objects, points refs/heads/gitmsg/<ext> at the new tip,
// and returns the shas oldest-first.
func seedExtMessages(t *testing.T, client *Client, ext, parent string, specs []pageMsgSpec) []string {
	t.Helper()
	shas := make([]string, 0, len(specs))
	for i, s := range specs {
		author, email, ts := s.author, s.email, s.ts
		if author == "" {
			author, email = "Ada", "ada@example.com"
		}
		if ts == 0 {
			ts = int64(1000 + len(shas) + i)
		}
		sha, loose := makePageCommit(t, parent, author, email, s.msg, ts)
		if err := client.Put("objects/"+sha[:2]+"/"+sha[2:], loose); err != nil {
			t.Fatalf("seed commit: %v", err)
		}
		shas = append(shas, sha)
		parent = sha
	}
	if err := client.Put("refs/heads/gitmsg/"+ext, []byte(parent+"\n")); err != nil {
		t.Fatalf("seed %s ref: %v", ext, err)
	}
	return shas
}

// seedSocialMessages is seedExtMessages on the social data branch.
func seedSocialMessages(t *testing.T, client *Client, parent string, specs []pageMsgSpec) []string {
	t.Helper()
	return seedExtMessages(t, client, "social", parent, specs)
}

// seedPagesConfig writes a core config commit carrying the given site
// sub-object and points refs/gitmsg/core/config at it, returning the sha.
func seedPagesConfig(t *testing.T, client *Client, site map[string]any) string {
	t.Helper()
	cfg, err := json.Marshal(map[string]any{"site": site})
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	sha, loose := makePageCommit(t, "", "Ada", "ada@example.com", string(cfg), 900)
	if err := client.Put("objects/"+sha[:2]+"/"+sha[2:], loose); err != nil {
		t.Fatalf("seed config object: %v", err)
	}
	if err := client.Put("refs/gitmsg/core/config", []byte(sha+"\n")); err != nil {
		t.Fatalf("seed config ref: %v", err)
	}
	return sha
}

// pagesTestSite is the standard guard-enabled site config for these tests.
func pagesTestSite() map[string]any {
	return map[string]any{"publish": "true", "pages": "true", "url": "https://example.com/", "title": "Pages Test"}
}

// pagesRefs assembles the refs map rebuildSitePages consumes.
func pagesRefs(client *Client, t *testing.T) map[string]string {
	t.Helper()
	refs, err := readRemoteRefs(client, "")
	if err != nil {
		t.Fatalf("read refs: %v", err)
	}
	return refs
}

// buildPages runs the items index for every present gitmsg data branch and the
// page layer (the direct, shell-free equivalent of a site push's tail).
func buildPages(t *testing.T, client *Client) (pending bool, state string) {
	t.Helper()
	refs := pagesRefs(client, t)
	for _, ext := range siteItemsExts {
		if tip, ok := refs["refs/heads/gitmsg/"+ext]; ok {
			if err := updateSiteItemsIndex(client, "", ext, tip, nil); err != nil {
				t.Fatalf("items index %s: %v", ext, err)
			}
		}
	}
	pending, state, err := rebuildSitePages(client, "", pagesRefs(client, t), "", nil, nil, SiteOverride{})
	if err != nil {
		t.Fatalf("rebuildSitePages: %v", err)
	}
	return pending, state
}

// getKey fetches a bucket key as a string, failing the test when absent.
func getKey(t *testing.T, client *Client, key string) string {
	t.Helper()
	data, err := client.Get(key)
	if err != nil {
		t.Fatalf("get %s: %v", key, err)
	}
	return string(data)
}

// keyExists reports whether a bucket key is present.
func keyExists(client *Client, key string) bool {
	_, err := client.Get(key)
	return err == nil
}

// socialComment builds a comment message on the given root/parent refs.
func socialComment(text, original string) string {
	return text + "\n\nGitMsg: ext=\"social\"; type=\"comment\"; original=\"#commit:" + original[:12] + "@gitmsg/social\"; v=\"0.1.0\""
}

func TestSitePages_GuardsAndDisable(t *testing.T) {
	client, bucket := testClient(t)
	seedSocialMessages(t, client, "", []pageMsgSpec{{msg: "first post\n\nhello"}, {msg: "second post"}})
	if err := client.Put("HEAD", []byte("ref: refs/heads/main\n")); err != nil {
		t.Fatal(err)
	}

	// generatedFront reports whether index.html currently holds the GENERATED
	// front page (plain HTML carrying the PE mount) rather than the embedded shell
	// (brotli-compressed on upload, so the literal mount bytes are absent).
	generatedFront := func() bool {
		body, err := client.Get("index.html")
		return err == nil && strings.Contains(string(body), `id="gs-page"`)
	}

	// No config at all: publish is off — a full pushSite must move repo-data
	// artifacts only and stamp the marker with pages "off". index.html is the
	// embedded shell (uploadSiteFiles always ships it), never the generated front
	// page; the retired timeline.html key must be absent.
	if err := pushSite(client, "", nil, SiteOverride{}, nil); err != nil {
		t.Fatalf("pushSite: %v", err)
	}
	if !keyExists(client, "index.html") || generatedFront() {
		t.Error("guards off: index.html must be the embedded shell, not the generated front page")
	}
	for _, key := range []string{sitePagesLegacyFrontKey, sitePagesManifestKey, sitePagesCSSKey, sitePagesSitemapKey, sitePagesRobotsKey, "posts/index.html"} {
		if keyExists(client, key) {
			t.Errorf("guards off: %s must not exist", key)
		}
	}
	if state, ok := readSitePushState(client, ""); !ok || state.Pages != sitePagesStateOff {
		t.Errorf("marker pages state = %q ok=%v, want %q", state.Pages, ok, sitePagesStateOff)
	}

	// publish on, pages off: site artifacts yes, page layer no. index.html stays
	// the shell.
	seedPagesConfig(t, client, map[string]any{"publish": "true", "title": "Pages Test"})
	if err := pushSite(client, "", nil, SiteOverride{}, nil); err != nil {
		t.Fatalf("pushSite publish-only: %v", err)
	}
	if !keyExists(client, siteCustomizationKey) {
		t.Error("publish on: site-config.json must exist")
	}
	if generatedFront() || keyExists(client, sitePagesManifestKey) {
		t.Error("pages off: no page keys expected (index.html stays the shell)")
	}

	// Both guards + url: the page layer appears and index.html BECOMES the
	// generated front page (the M8 entry flip), overwriting the embedded shell.
	seedPagesConfig(t, client, pagesTestSite())
	if err := pushSite(client, "", nil, SiteOverride{}, nil); err != nil {
		t.Fatalf("pushSite pages-on: %v", err)
	}
	if !generatedFront() {
		t.Error("pages on: index.html must be the generated front page")
	}
	front := getKey(t, client, "index.html")
	if !strings.Contains(front, `src="./gs-upgrade.js"`) {
		t.Error("pages on: front page must reference gs-upgrade.js")
	}
	if strings.Contains(front, "open in app") {
		t.Error("pages on: front page must not carry an 'open in app' link")
	}
	if keyExists(client, sitePagesLegacyFrontKey) {
		t.Error("pages on: retired timeline.html must not exist")
	}
	for _, key := range []string{sitePagesManifestKey, sitePagesCSSKey, sitePagesSitemapKey, sitePagesRobotsKey, sitePagesFeedKey, sitePagesUpgradeKey, "posts/index.html", "issues/index.html", "posts/feed.xml", "issues/feed.xml"} {
		if !keyExists(client, key) {
			t.Errorf("pages on: %s must exist", key)
		}
	}
	if state, ok := readSitePushState(client, ""); !ok || state.Pages != sitePagesStateOn {
		t.Errorf("marker pages state = %q ok=%v, want %q", state.Pages, ok, sitePagesStateOn)
	}
	robots := getKey(t, client, sitePagesRobotsKey)
	if !strings.Contains(robots, "User-agent: *") || !strings.Contains(robots, "Sitemap: https://example.com/sitemap.xml") {
		t.Errorf("robots.txt content wrong:\n%s", robots)
	}

	// Count the item pages so the disable sweep can be checked exhaustively.
	itemKeys, err := client.List("i/")
	if err != nil || len(itemKeys) != 2 {
		t.Fatalf("item pages = %v (err %v), want 2", itemKeys, err)
	}

	// pages=false on a bucket that has pages: the disable path deletes every page
	// key (manifest last) and RESTORES the embedded shell at index.html (the flip
	// back — index.html is never deleted, it is dual-owned), and the marker
	// returns to "off".
	seedPagesConfig(t, client, map[string]any{"publish": "true", "pages": "false", "url": "https://example.com/", "title": "Pages Test"})
	if err := pushSite(client, "", nil, SiteOverride{}, nil); err != nil {
		t.Fatalf("pushSite disable: %v", err)
	}
	gone := append([]string{sitePagesLegacyFrontKey, sitePagesCSSKey, sitePagesSitemapKey, sitePagesRobotsKey, sitePagesFeedKey, "posts/index.html", "issues/index.html", "posts/feed.xml", "issues/feed.xml", sitePagesManifestKey}, itemKeys...)
	for _, key := range gone {
		if keyExists(client, key) {
			t.Errorf("disable: %s must be deleted", key)
		}
	}
	if !keyExists(client, "index.html") || generatedFront() {
		t.Error("disable: index.html must be restored to the embedded shell")
	}
	if state, ok := readSitePushState(client, ""); !ok || state.Pages != sitePagesStateOff {
		t.Errorf("marker after disable = %q ok=%v, want %q", state.Pages, ok, sitePagesStateOff)
	}
	_ = bucket
}

// TestSitePages_IndexHTMLReclaim pins the dual-mode index.html ownership: while
// the page layer is effective, a shell (re)upload over index.html must be
// reclaimed by the pages pass so index.html serves the generated front page, not
// the embedded shell — the deterministic path that keeps a shell-version bump
// from stranding index.html as the shell after a no-op-tips push.
func TestSitePages_IndexHTMLReclaim(t *testing.T) {
	client, _ := testClient(t)
	seedPagesConfig(t, client, pagesTestSite())
	seedSocialMessages(t, client, "", []pageMsgSpec{{msg: "first post\n\nhello"}})
	if err := client.Put("HEAD", []byte("ref: refs/heads/main\n")); err != nil {
		t.Fatal(err)
	}
	// A full pushSite: uploadSiteFiles ships the shell at index.html, then
	// rebuildSitePages overwrites it with the generated front page (the flip).
	if err := pushSite(client, "", nil, SiteOverride{}, nil); err != nil {
		t.Fatalf("pushSite: %v", err)
	}
	isFront := func() bool {
		body, err := client.Get("index.html")
		return err == nil && strings.Contains(string(body), `id="gs-page"`)
	}
	if !isFront() {
		t.Fatal("after pages-on push, index.html must be the generated front page")
	}
	// Simulate a shell (re)upload clobbering index.html with the embedded shell
	// (brotli-compressed, so the plain mount bytes vanish). Move a non-pages ref
	// (a fork ref) so the push-state digest changes and the pass is NOT skipped,
	// while the pages tips stay put — so rebuildSitePages takes its no-op-tips
	// branch, which must RECLAIM index.html (reclaimSiteFrontPage) rather than
	// leave the shell the upload just wrote.
	if err := uploadShellIndexHTML(client, ""); err != nil {
		t.Fatalf("clobber index.html: %v", err)
	}
	if isFront() {
		t.Fatal("test setup: the shell upload should have clobbered the front page")
	}
	socialTip := strings.TrimSpace(getKey(t, client, "refs/heads/gitmsg/social"))
	if err := client.Put("refs/tags/v0.1", []byte(socialTip+"\n")); err != nil {
		t.Fatal(err)
	}
	if err := pushSite(client, "", nil, SiteOverride{}, nil); err != nil {
		t.Fatalf("pushSite reclaim: %v", err)
	}
	if !isFront() {
		t.Error("a no-op-tips push must reclaim index.html to the generated front page")
	}
}

func TestSitePages_SealedListOverflow(t *testing.T) {
	client, bucket := testClient(t)
	seedPagesConfig(t, client, pagesTestSite())
	specs := make([]pageMsgSpec, 0, 230)
	for i := 0; i < 230; i++ {
		specs = append(specs, pageMsgSpec{msg: fmt.Sprintf("post number %03d", i), ts: int64(1000 + i)})
	}
	shas := seedSocialMessages(t, client, "", specs)
	if pending, _ := buildPages(t, client); pending {
		t.Fatal("bootstrap of 230 posts must complete in one pass")
	}

	// 230 visible → two sealed pages (1 = oldest) + a 30-entry head.
	head := getKey(t, client, "posts/index.html")
	if !strings.Contains(head, `href="2.html">older →`) {
		t.Error("head must link older → 2.html")
	}
	page2 := getKey(t, client, "posts/2.html")
	if !strings.Contains(page2, `href="index.html">← newer`) || !strings.Contains(page2, `href="1.html">older →`) {
		t.Error("page 2 must chain newer→index and older→1")
	}
	page1 := getKey(t, client, "posts/1.html")
	if strings.Contains(page1, "older →") {
		t.Error("page 1 (oldest) must have no older link")
	}
	// Sealed AND head list pages carry both autodiscovery links: the global feed
	// and their own type feed.
	globalLink := `<link rel="alternate" type="application/atom+xml" title="Pages Test" href="https://example.com/feed.xml">`
	typeLink := `<link rel="alternate" type="application/atom+xml" title="posts · Pages Test" href="https://example.com/posts/feed.xml">`
	for name, page := range map[string]string{"posts/index.html": head, "posts/1.html": page1} {
		if !strings.Contains(page, globalLink) || !strings.Contains(page, typeLink) {
			t.Errorf("%s must carry both the global and type feed autodiscovery links", name)
		}
	}
	if !strings.Contains(page1, "post number 000") || !strings.Contains(page1, "post number 099") {
		t.Error("page 1 must hold the oldest hundred")
	}
	if keyExists(client, "posts/3.html") {
		t.Error("no page 3 yet")
	}
	manifest, err := readSitePagesManifest(client, "")
	if err != nil || manifest == nil {
		t.Fatalf("pages manifest: %v", err)
	}
	if manifest.Counts["posts"] != 2 || manifest.Frontier["posts"] == "" {
		t.Errorf("manifest counts/frontier = %d/%q, want 2/non-empty", manifest.Counts["posts"], manifest.Frontier["posts"])
	}

	// +80 posts: the incremental pass must seal page 3 and never rewrite 1/2.
	puts1, puts2 := bucket.putCount("posts/1.html"), bucket.putCount("posts/2.html")
	more := make([]pageMsgSpec, 0, 80)
	for i := 230; i < 310; i++ {
		more = append(more, pageMsgSpec{msg: fmt.Sprintf("post number %03d", i), ts: int64(1000 + i)})
	}
	seedSocialMessages(t, client, shas[len(shas)-1], more)
	if pending, state := buildPages(t, client); pending || state != sitePagesStateOn {
		t.Fatalf("incremental pass pending=%v state=%q", pending, state)
	}
	if bucket.putCount("posts/1.html") != puts1 || bucket.putCount("posts/2.html") != puts2 {
		t.Error("sealed pages 1/2 must not be rewritten by the incremental pass")
	}
	page3 := getKey(t, client, "posts/3.html")
	if !strings.Contains(page3, "post number 200") || !strings.Contains(page3, "post number 299") {
		t.Error("page 3 must hold entries 200..299 (sealed off the old head)")
	}
	head = getKey(t, client, "posts/index.html")
	if !strings.Contains(head, `href="3.html">older →`) || !strings.Contains(head, "310 posts") {
		t.Error("head must link older → 3.html and count 310")
	}
	if !strings.Contains(head, "post number 309") || strings.Contains(head, "post number 299") {
		t.Error("head must hold only the unsealed newest entries (300..309)")
	}
	manifest, _ = readSitePagesManifest(client, "")
	if manifest.Counts["posts"] != 3 {
		t.Errorf("manifest counts = %d, want 3", manifest.Counts["posts"])
	}
}

func TestSitePages_HostileEscaping(t *testing.T) {
	client, _ := testClient(t)
	seedPagesConfig(t, client, pagesTestSite())
	hostile := "</title><script>alert(1)</script>\n\n\"><script>body()</script> with `backticks` and 'quotes'"
	shas := seedSocialMessages(t, client, "", []pageMsgSpec{{
		msg: hostile, author: "\"><script>evil()</script>", email: "eve@example.com\"><script>",
	}})
	if pending, _ := buildPages(t, client); pending {
		t.Fatal("unexpected pending")
	}
	for _, key := range []string{"i/" + shas[0][:12] + ".html", "posts/index.html", sitePagesFrontKey} {
		page := getKey(t, client, key)
		if strings.Contains(page, "<script>") || strings.Contains(page, "</title><") {
			t.Errorf("%s carries unescaped hostile input", key)
		}
		if !strings.Contains(page, "&lt;script&gt;") {
			t.Errorf("%s must carry the escaped form", key)
		}
	}
	item := getKey(t, client, "i/"+shas[0][:12]+".html")
	if !strings.Contains(item, "&#34;&gt;&lt;script&gt;evil()&lt;/script&gt;") && !strings.Contains(item, "&quot;&gt;&lt;script&gt;evil()&lt;/script&gt;") {
		t.Error("author name must render escaped")
	}
}

func TestSitePages_ThreadEditRetractResolution(t *testing.T) {
	client, _ := testClient(t)
	seedPagesConfig(t, client, pagesTestSite())
	post := "Original subject\n\noriginal body"
	shas := seedSocialMessages(t, client, "", []pageMsgSpec{{msg: post, ts: 1000}})
	root := shas[0]
	c1 := socialComment("First reply text", root)
	shas = seedSocialMessages(t, client, root, []pageMsgSpec{
		{msg: c1, author: "Bob", email: "bob@example.com", ts: 1010},
	})
	c1sha := shas[0]
	// A nested reply that names only its parent comment: root resolution must
	// walk the reply-to chain up to the post.
	nested := "Nested reply text\n\nGitMsg: ext=\"social\"; type=\"comment\"; reply-to=\"#commit:" + c1sha[:12] + "@gitmsg/social\"; v=\"0.1.0\""
	// Two same-timestamp edits of the root (git timestamps are second-granular):
	// the LATER commit on the data branch wins, matching the shell's chain-order
	// resolution — never the hash lottery.
	editA := "Edited subject A\n\nedited body A\n\nGitMsg: ext=\"social\"; type=\"post\"; edits=\"#commit:" + root[:12] + "@gitmsg/social\"; v=\"0.1.0\""
	editB := "Edited subject B\n\nedited body B\n\nGitMsg: ext=\"social\"; type=\"post\"; edits=\"#commit:" + root[:12] + "@gitmsg/social\"; v=\"0.1.0\""
	retract := "GitMsg: ext=\"social\"; edits=\"#commit:" + c1sha[:12] + "@gitmsg/social\"; retracted=\"true\"; v=\"0.1.0\""
	seedSocialMessages(t, client, c1sha, []pageMsgSpec{
		{msg: nested, author: "Cee", email: "cee@example.com", ts: 1020},
		{msg: editA, ts: 1500},
		{msg: editB, ts: 1500},
		{msg: retract, author: "Bob", email: "bob@example.com", ts: 1600},
	})
	if pending, _ := buildPages(t, client); pending {
		t.Fatal("unexpected pending")
	}

	page := getKey(t, client, "i/"+root[:12]+".html")
	if !strings.Contains(page, "Edited subject B") || strings.Contains(page, "Edited subject A") {
		t.Error("same-second edits: the later branch commit must win the tiebreak")
	}
	if !strings.Contains(page, "edited") {
		t.Error("edited marker missing")
	}
	if !strings.Contains(page, "was retracted by its author") || strings.Contains(page, "First reply text") {
		t.Error("retracted reply must render as a tombstone line")
	}
	if !strings.Contains(page, "Nested reply text") {
		t.Error("nested reply must resolve to the root's thread")
	}
	// Replies get no pages of their own.
	if keyExists(client, "i/"+c1sha[:12]+".html") {
		t.Error("a reply must not get its own page")
	}
	// The lists show the resolved subject.
	if head := getKey(t, client, "posts/index.html"); !strings.Contains(head, "Edited subject B") {
		t.Error("list must show the resolved subject")
	}
}

func TestSitePages_ThreadCapTruncation(t *testing.T) {
	client, _ := testClient(t)
	seedPagesConfig(t, client, pagesTestSite())
	prevReplies, prevBytes := sitePageMaxReplies, sitePageMaxThreadBytes
	defer func() { sitePageMaxReplies, sitePageMaxThreadBytes = prevReplies, prevBytes }()
	sitePageMaxReplies = 3

	shas := seedSocialMessages(t, client, "", []pageMsgSpec{{msg: "Capped thread\n\nbody", ts: 1000}})
	root := shas[0]
	parent := root
	for i := 0; i < 5; i++ {
		out := seedSocialMessages(t, client, parent, []pageMsgSpec{{msg: socialComment(fmt.Sprintf("reply %d", i), root), ts: int64(1010 + i)}})
		parent = out[0]
	}
	if pending, _ := buildPages(t, client); pending {
		t.Fatal("unexpected pending")
	}
	page := getKey(t, client, "i/"+root[:12]+".html")
	if !strings.Contains(page, "2 more replies") {
		t.Error("reply cap: expected a '2 more replies' marker")
	}
	if !strings.Contains(page, "reply 2") || strings.Contains(page, "reply 3") {
		t.Error("reply cap: only the first three replies inline")
	}

	// Byte cap: with a tiny thread budget the tail truncates even below the
	// reply-count cap.
	sitePageMaxReplies = prevReplies
	sitePageMaxThreadBytes = 8
	seedPagesConfig(t, client, map[string]any{"publish": "true", "pages": "true", "url": "https://example.com/", "title": "Regen"})
	if pending, _ := buildPages(t, client); pending {
		t.Fatal("unexpected pending")
	}
	page = getKey(t, client, "i/"+root[:12]+".html")
	if !strings.Contains(page, "more replies") {
		t.Error("byte cap: expected a truncation marker")
	}
}

func TestSitePages_IncrementalDeltaPartition(t *testing.T) {
	client, bucket := testClient(t)
	seedPagesConfig(t, client, pagesTestSite())
	shas := seedSocialMessages(t, client, "", []pageMsgSpec{
		{msg: "Thread root\n\nroot body", ts: 1000},
		{msg: "Bystander post\n\nuntouched", ts: 1001},
	})
	rootSha, bystander := shas[0], shas[1]
	if pending, _ := buildPages(t, client); pending {
		t.Fatal("unexpected pending")
	}
	rootKey, bystanderKey := "i/"+rootSha[:12]+".html", "i/"+bystander[:12]+".html"
	rootPuts, byPuts := bucket.putCount(rootKey), bucket.putCount(bystanderKey)
	frontPuts := bucket.putCount(sitePagesFrontKey)

	// Delta: one reply to the root + one new top-level post.
	out := seedSocialMessages(t, client, bystander, []pageMsgSpec{
		{msg: socialComment("Fresh reply body", rootSha), author: "Bob", email: "bob@example.com", ts: 1010},
		{msg: "New root post\n\nnew body", ts: 1011},
	})
	newRoot := out[1]
	if pending, state := buildPages(t, client); pending || state != sitePagesStateOn {
		t.Fatalf("incremental pending=%v state=%q", pending, state)
	}
	if bucket.putCount(rootKey) != rootPuts+1 {
		t.Error("the replied-to root's page must be regenerated exactly once")
	}
	if bucket.putCount(bystanderKey) != byPuts {
		t.Error("an unaffected item's page must not be rewritten")
	}
	if bucket.putCount(sitePagesFrontKey) != frontPuts+1 {
		t.Error("the front page must be rewritten once")
	}
	if !strings.Contains(getKey(t, client, rootKey), "Fresh reply body") {
		t.Error("the regenerated root page must inline the new reply (bodies from the corpus)")
	}
	if !keyExists(client, "i/"+newRoot[:12]+".html") {
		t.Error("the new top-level post must get its own page")
	}
	if !strings.Contains(getKey(t, client, sitePagesFrontKey), "New root post") {
		t.Error("the front page must show the new root")
	}
	manifest, _ := readSitePagesManifest(client, "")
	tip := strings.TrimSpace(getKey(t, client, "refs/heads/gitmsg/social"))
	if manifest.Ext["social"] != tip {
		t.Error("the manifest must record the consumed tip")
	}
}

func TestSitePages_BootstrapCursorResume(t *testing.T) {
	client, _ := testClient(t)
	seedPagesConfig(t, client, pagesTestSite())
	prev := sitePagesBudget
	defer func() { sitePagesBudget = prev }()
	sitePagesBudget = 3

	specs := make([]pageMsgSpec, 0, 8)
	for i := 0; i < 8; i++ {
		specs = append(specs, pageMsgSpec{msg: fmt.Sprintf("budgeted post %d", i), ts: int64(1000 + i)})
	}
	seedSocialMessages(t, client, "", specs)
	pending, state := buildPages(t, client)
	if !pending || state != "" {
		t.Fatalf("first budgeted pass: pending=%v state=%q, want true/\"\"", pending, state)
	}
	manifest, _ := readSitePagesManifest(client, "")
	if manifest == nil || manifest.Cursor == nil || manifest.Cursor.Done["social"] != 3 {
		t.Fatalf("cursor = %+v, want done social=3", manifest.Cursor)
	}
	// The head list claims only what exists: the newest three.
	head := getKey(t, client, "posts/index.html")
	if !strings.Contains(head, "budgeted post 7") || strings.Contains(head, "budgeted post 0") {
		t.Error("partial bootstrap: list must claim only the generated newest prefix")
	}
	// Two more passes finish the set.
	for i := 0; i < 2; i++ {
		pending, state = buildPages(t, client)
	}
	if pending || state != sitePagesStateOn {
		t.Fatalf("bootstrap must complete after three passes, pending=%v state=%q", pending, state)
	}
	manifest, _ = readSitePagesManifest(client, "")
	if manifest.Cursor != nil {
		t.Error("completed bootstrap must clear the cursor")
	}
	for i := 0; i < 8; i++ {
		if !strings.Contains(getKey(t, client, "posts/index.html"), fmt.Sprintf("budgeted post %d", i)) {
			t.Errorf("post %d missing after completion", i)
		}
	}
}

func TestSitePages_SitemapCoverageAndIndexMode(t *testing.T) {
	client, _ := testClient(t)
	// A path with XML-special characters pins loc escaping.
	seedPagesConfig(t, client, map[string]any{"publish": "true", "pages": "true", "url": "https://example.com/a&b/", "title": "Sitemap"})
	specs := make([]pageMsgSpec, 0, 5)
	for i := 0; i < 5; i++ {
		specs = append(specs, pageMsgSpec{msg: fmt.Sprintf("sitemap post %d", i), ts: int64(1000 + i)})
	}
	shas := seedSocialMessages(t, client, "", specs)
	if pending, _ := buildPages(t, client); pending {
		t.Fatal("unexpected pending")
	}
	sitemap := getKey(t, client, sitePagesSitemapKey)
	if strings.Contains(sitemap, "a&b") || !strings.Contains(sitemap, "a&amp;b") {
		t.Error("sitemap locs must be XML-escaped")
	}
	if got := strings.Count(sitemap, "<url>"); got != 6 {
		t.Errorf("sitemap has %d urls, want 6 (root + 5 pages)", got)
	}
	for _, sha := range shas {
		if !strings.Contains(sitemap, "/i/"+sha[:12]+".html</loc>") {
			t.Errorf("sitemap must cover i/%s.html", sha[:12])
		}
	}
	if !strings.Contains(sitemap, "<lastmod>1970-01-01</lastmod>") {
		t.Error("lastmod must be W3C dates")
	}

	// Index mode under a lowered part size: sealed parts + head + index.
	prev := siteSitemapPartSize
	defer func() { siteSitemapPartSize = prev }()
	siteSitemapPartSize = 2
	seedPagesConfig(t, client, map[string]any{"publish": "true", "pages": "true", "url": "https://example.com/a&b/", "title": "Sitemap2"})
	if pending, _ := buildPages(t, client); pending {
		t.Fatal("unexpected pending")
	}
	index := getKey(t, client, sitePagesSitemapKey)
	if !strings.Contains(index, "<sitemapindex") {
		t.Fatal("sitemap.xml must become an index past the part size")
	}
	for _, part := range []string{"sitemap-1.xml", "sitemap-2.xml", sitePagesSitemapHeadKey} {
		if !strings.Contains(index, part+"</loc>") {
			t.Errorf("index must reference %s", part)
		}
	}
	urls := 0
	for _, part := range []string{"sitemap-1.xml", "sitemap-2.xml", sitePagesSitemapHeadKey} {
		urls += strings.Count(getKey(t, client, part), "<url>")
	}
	if urls != 6 {
		t.Errorf("parts cover %d urls, want 6", urls)
	}
	if cacheControlForKey("sitemap-1.xml") != cacheControlImmutable {
		t.Error("sealed sitemap parts must be immutable-cached")
	}
	if cacheControlForKey(sitePagesSitemapHeadKey) != cacheControlRevalidate || cacheControlForKey(sitePagesSitemapKey) != cacheControlRevalidate {
		t.Error("sitemap head/index must revalidate")
	}
}

func TestSitePages_OldMarkerDoesNotMaskPagesBootstrap(t *testing.T) {
	client, _ := testClient(t)
	seedPagesConfig(t, client, pagesTestSite())
	seedSocialMessages(t, client, "", []pageMsgSpec{{msg: "marker post"}})
	if err := client.Put("HEAD", []byte("ref: refs/heads/main\n")); err != nil {
		t.Fatal(err)
	}
	if err := pushSite(client, "", nil, SiteOverride{}, nil); err != nil {
		t.Fatalf("pushSite: %v", err)
	}
	// Simulate an older binary's pass: pages wiped, marker stamped WITHOUT a
	// pages field at the current shell version and digest.
	if err := client.Delete(sitePagesManifestKey); err != nil {
		t.Fatal(err)
	}
	digest, err := refsHeadDigest(client, "")
	if err != nil {
		t.Fatal(err)
	}
	old, _ := json.Marshal(map[string]any{"version": sitePushStateVersion, "shellVersion": mustSiteVersion(t), "refsDigest": digest})
	if err := client.Put(sitePushStateKey, old); err != nil {
		t.Fatal(err)
	}
	if up, _ := siteMaintenanceUpToDate(client, "", mustSiteVersion(t), SiteOverride{}); up {
		t.Fatal("a pages-unaware marker must not report up-to-date")
	}
	if err := pushSite(client, "", nil, SiteOverride{}, nil); err != nil {
		t.Fatalf("recovery pushSite: %v", err)
	}
	if !keyExists(client, sitePagesManifestKey) {
		t.Error("the pass after an old-format marker must regenerate the pages")
	}
	if state, ok := readSitePushState(client, ""); !ok || state.Pages != sitePagesStateOn {
		t.Errorf("recovered marker pages state = %q ok=%v, want on", state.Pages, ok)
	}
}

func TestSitePages_FrontReadme(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "HOME="+dir)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	run("config", "user.name", "T")
	run("config", "user.email", "t@example.com")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Readme Title\n\nreadme paragraph text\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "README.md")
	run("commit", "-q", "-m", "readme")

	client, _ := testClient(t)
	seedPagesConfig(t, client, pagesTestSite())
	seedSocialMessages(t, client, "", []pageMsgSpec{{msg: "readme fixture post"}})
	refs := pagesRefs(client, t)
	if err := updateSiteItemsIndex(client, "", "social", refs["refs/heads/gitmsg/social"], nil); err != nil {
		t.Fatal(err)
	}
	src := newLocalCommitSource("", dir)
	defer src.close()
	if pending, _, err := rebuildSitePages(client, "", pagesRefs(client, t), "main", src, nil, SiteOverride{}); err != nil || pending {
		t.Fatalf("rebuildSitePages: pending=%v err=%v", pending, err)
	}
	front := getKey(t, client, sitePagesFrontKey)
	if !strings.Contains(front, "README") || !strings.Contains(front, "readme paragraph text") {
		t.Error("front page must carry the README section after the entries")
	}
	if strings.Contains(front, "truncated") {
		t.Error("a small README must not be marked truncated")
	}
	if i, j := strings.Index(front, "readme fixture post"), strings.Index(front, "readme paragraph text"); i < 0 || j < 0 || j < i {
		t.Error("README must render after the timeline entries")
	}
}

// atomFeedDoc mirrors the Atom feed shape for round-trip parsing in the tests.
type atomFeedDoc struct {
	XMLName  xml.Name `xml:"feed"`
	ID       string   `xml:"id"`
	Title    string   `xml:"title"`
	Subtitle string   `xml:"subtitle"`
	Updated  string   `xml:"updated"`
	Links    []struct {
		Rel  string `xml:"rel,attr"`
		Href string `xml:"href,attr"`
	} `xml:"link"`
	Entries []atomFeedEntry `xml:"entry"`
}

// atomFeedEntry mirrors one Atom entry for round-trip parsing in the tests.
type atomFeedEntry struct {
	Title     string `xml:"title"`
	ID        string `xml:"id"`
	Updated   string `xml:"updated"`
	Published string `xml:"published"`
	Author    struct {
		Name string `xml:"name"`
	} `xml:"author"`
	Category struct {
		Term string `xml:"term,attr"`
	} `xml:"category"`
	Link struct {
		Rel  string `xml:"rel,attr"`
		Href string `xml:"href,attr"`
	} `xml:"link"`
	Content struct {
		Type  string `xml:"type,attr"`
		Value string `xml:",chardata"`
	} `xml:"content"`
}

// feedLink returns the href of the feed-level link with the given rel.
func feedLink(f atomFeedDoc, rel string) string {
	for _, l := range f.Links {
		if l.Rel == rel {
			return l.Href
		}
	}
	return ""
}

// feedEntryByTitle finds a parsed feed entry by its title.
func feedEntryByTitle(t *testing.T, f atomFeedDoc, title string) atomFeedEntry {
	t.Helper()
	for _, e := range f.Entries {
		if e.Title == title {
			return e
		}
	}
	t.Fatalf("feed entry %q not found", title)
	return atomFeedEntry{}
}

func TestSitePages_AtomFeed(t *testing.T) {
	client, bucket := testClient(t)
	seedPagesConfig(t, client, map[string]any{"publish": "true", "pages": "true", "url": "https://example.com/", "title": "Feed Test", "description": "a & b feed"})
	hostile := "Bad <subject> & \"quotes\""
	shas := seedSocialMessages(t, client, "", []pageMsgSpec{
		{msg: "Apple post\n\nbody", ts: 1000},
		{msg: hostile + "\n\n<b>bold</b> & stuff", author: "Eve <x>", ts: 1010},
		{msg: "Cherry post\n\nbody", ts: 1020},
		{msg: "Date post\n\nbody line one\nline two\n\npara two", ts: 1030},
		{msg: "Subject only post", ts: 1035},
		{msg: "Long post\n\n" + strings.Repeat("x", siteFeedBodyMax+1000), ts: 1036},
	})
	// Retract Cherry so it is excluded from the feed.
	retract := "GitMsg: ext=\"social\"; edits=\"#commit:" + shas[2][:12] + "@gitmsg/social\"; retracted=\"true\"; v=\"0.1.0\""
	seedSocialMessages(t, client, shas[5], []pageMsgSpec{{msg: retract, ts: 1040}})

	// A memo item (newest of all) must be excluded from the feed.
	msha, mloose := makePageCommit(t, "", "Mem", "mem@example.com", "Memo entry\n\nmemo body", 1050)
	if err := client.Put("objects/"+msha[:2]+"/"+msha[2:], mloose); err != nil {
		t.Fatal(err)
	}
	if err := client.Put("refs/heads/gitmsg/memo", []byte(msha+"\n")); err != nil {
		t.Fatal(err)
	}
	if err := updateSiteItemsIndex(client, "", "memo", msha, nil); err != nil {
		t.Fatal(err)
	}

	if pending, _ := buildPages(t, client); pending {
		t.Fatal("unexpected pending")
	}

	raw := getKey(t, client, sitePagesFeedKey)
	if strings.Contains(raw, "<subject>") || strings.Contains(raw, "Memo entry") || strings.Contains(raw, "Cherry post") {
		t.Errorf("feed carries excluded/unescaped content:\n%s", raw)
	}
	if !strings.Contains(raw, "Bad &lt;subject&gt; &amp; &quot;quotes&quot;") {
		t.Error("hostile subject must be XML-escaped in the feed")
	}
	var f atomFeedDoc
	if err := xml.Unmarshal([]byte(raw), &f); err != nil {
		t.Fatalf("feed is not well-formed XML: %v", err)
	}
	if f.ID != "https://example.com/" || f.Title != "Feed Test" || f.Subtitle != "a & b feed" {
		t.Errorf("feed metadata wrong: id=%q title=%q subtitle=%q", f.ID, f.Title, f.Subtitle)
	}
	if feedLink(f, "self") != "https://example.com/feed.xml" || feedLink(f, "alternate") != "https://example.com/" {
		t.Errorf("feed self/alternate links wrong: %+v", f.Links)
	}
	if len(f.Entries) != 5 {
		t.Fatalf("feed has %d entries, want 5 (Cherry retracted, memo excluded)", len(f.Entries))
	}
	// Newest-first: Long (1036), Subject only (1035), Date (1030), hostile
	// (1010), Apple (1000).
	wantTitles := []string{"Long post", "Subject only post", "Date post", hostile, "Apple post"}
	for i, want := range wantTitles {
		if f.Entries[i].Title != want {
			t.Errorf("feed entry %d title = %q, want %q", i, f.Entries[i].Title, want)
		}
	}
	e := f.Entries[2]
	if e.ID != "https://example.com/i/"+shas[3][:12]+".html" || e.ID != e.Link.Href || e.Link.Rel != "alternate" {
		t.Errorf("entry id/link wrong: id=%q link=%+v", e.ID, e.Link)
	}
	if e.Category.Term != "post" || e.Author.Name == "" {
		t.Errorf("entry category/author wrong: term=%q author=%q", e.Category.Term, e.Author.Name)
	}
	if _, err := time.Parse(time.RFC3339, e.Updated); err != nil {
		t.Errorf("entry updated %q not RFC3339: %v", e.Updated, err)
	}
	if _, err := time.Parse(time.RFC3339, f.Updated); err != nil {
		t.Errorf("feed updated %q not RFC3339: %v", f.Updated, err)
	}
	// The feed's <updated> is the newest ENTRY's activity (Long, 1036), not the
	// memo's newer timestamp.
	if want := time.Unix(1036, 0).UTC().Format(time.RFC3339); f.Updated != want {
		t.Errorf("feed updated = %q, want the newest entry's activity %q", f.Updated, want)
	}

	// Entry content: the item's own body as escaped <p>/<br> HTML (subject
	// stripped), double-escaped in the raw XML.
	if e.Content.Type != "html" || e.Content.Value != "<p>body line one<br>line two</p><p>para two</p>" {
		t.Errorf("content wrong: type=%q value=%q", e.Content.Type, e.Content.Value)
	}
	if strings.Contains(raw, "<p>body") || strings.Contains(raw, "<b>bold") {
		t.Error("content must not carry literal HTML in the raw XML")
	}
	// An XML/HTML-special body is escaped INSIDE the content HTML (one XML
	// unescape by the parser leaves escaped-HTML text, never markup).
	if got := feedEntryByTitle(t, f, hostile).Content.Value; got != "<p>&lt;b&gt;bold&lt;/b&gt; &amp; stuff</p>" {
		t.Errorf("hostile body content = %q, want the inner-escaped form", got)
	}
	// A subject-only item carries no content element.
	if got := feedEntryByTitle(t, f, "Subject only post").Content.Value; got != "" {
		t.Errorf("subject-only entry must omit content, got %q", got)
	}
	// An over-cap body truncates with a marker paragraph.
	long := feedEntryByTitle(t, f, "Long post").Content.Value
	if !strings.HasPrefix(long, "<p>xxxx") || !strings.HasSuffix(long, "<p>… truncated</p>") {
		t.Errorf("long body must truncate with a marker, got %d bytes ending %q", len(long), long[max(0, len(long)-40):])
	}
	if len(long) > siteFeedBodyMax+100 {
		t.Errorf("truncated content is %d bytes, want ~%d", len(long), siteFeedBodyMax)
	}

	// Autodiscovery: every generated page carries the alternate feed link with
	// an ABSOLUTE href (a relative one resolves to garbage once gs-upgrade.js
	// hash-rewrites the location).
	discovery := `<link rel="alternate" type="application/atom+xml" title="Feed Test" href="https://example.com/feed.xml">`
	front := getKey(t, client, sitePagesFrontKey)
	if !strings.Contains(front, discovery) {
		t.Error("front page missing the absolute Atom autodiscovery link")
	}
	item := getKey(t, client, "i/"+shas[0][:12]+".html")
	if !strings.Contains(item, discovery) {
		t.Error("item page missing the absolute Atom autodiscovery link")
	}
	if head := getKey(t, client, "posts/index.html"); !strings.Contains(head, discovery) {
		t.Error("list head page missing the absolute Atom autodiscovery link")
	}

	// No-op reclaim pass (no tips moved) must not rewrite the feed.
	feedPuts := bucket.putCount(sitePagesFeedKey)
	if pending, _ := buildPages(t, client); pending {
		t.Fatal("unexpected pending on reclaim pass")
	}
	if bucket.putCount(sitePagesFeedKey) != feedPuts {
		t.Error("a no-op pass must not rewrite feed.xml")
	}

	// Incremental append: a new top-level post chained off the current social tip
	// must add a feed entry and rewrite feed.xml.
	socialTip := strings.TrimSpace(getKey(t, client, "refs/heads/gitmsg/social"))
	seedSocialMessages(t, client, socialTip, []pageMsgSpec{{msg: "Elder post\n\nelder body", ts: 1070}})
	if pending, state := buildPages(t, client); pending || state != sitePagesStateOn {
		t.Fatalf("incremental pending=%v state=%q", pending, state)
	}
	if bucket.putCount(sitePagesFeedKey) <= feedPuts {
		t.Error("an item-appending push must rewrite feed.xml")
	}
	f = atomFeedDoc{}
	if err := xml.Unmarshal([]byte(getKey(t, client, sitePagesFeedKey)), &f); err != nil {
		t.Fatalf("incremental feed not well-formed: %v", err)
	}
	if got := feedEntryByTitle(t, f, "Elder post").Content.Value; got != "<p>elder body</p>" {
		t.Errorf("appended post content = %q, want its body", got)
	}
	// The incremental pass builds from metadata: an entry OUTSIDE the delta must
	// still carry content (its body fetched back from the corpus).
	if got := feedEntryByTitle(t, f, "Apple post").Content.Value; got != "<p>body</p>" {
		t.Errorf("non-delta entry content = %q, want its corpus body", got)
	}
}

func TestSitePages_AtomFeedCap(t *testing.T) {
	client, _ := testClient(t)
	seedPagesConfig(t, client, pagesTestSite())
	specs := make([]pageMsgSpec, 0, sitePagesFrontSize+5)
	for i := 0; i < sitePagesFrontSize+5; i++ {
		specs = append(specs, pageMsgSpec{msg: fmt.Sprintf("feed post %03d", i), ts: int64(1000 + i)})
	}
	seedSocialMessages(t, client, "", specs)
	if pending, _ := buildPages(t, client); pending {
		t.Fatal("unexpected pending")
	}
	var f atomFeedDoc
	if err := xml.Unmarshal([]byte(getKey(t, client, sitePagesFeedKey)), &f); err != nil {
		t.Fatalf("feed not well-formed: %v", err)
	}
	if len(f.Entries) != sitePagesFrontSize {
		t.Fatalf("feed has %d entries, want the cap %d", len(f.Entries), sitePagesFrontSize)
	}
	// Newest-first: the cap keeps the newest, drops the oldest five.
	newest := fmt.Sprintf("feed post %03d", sitePagesFrontSize+4)
	if f.Entries[0].Title != newest {
		t.Errorf("first entry = %q, want newest %q", f.Entries[0].Title, newest)
	}
	for _, e := range f.Entries {
		if e.Title == "feed post 000" {
			t.Error("the oldest post must be dropped by the cap")
		}
	}
}

func TestSitePages_TypeFeeds(t *testing.T) {
	client, bucket := testClient(t)
	seedPagesConfig(t, client, pagesTestSite())
	seedSocialMessages(t, client, "", []pageMsgSpec{{msg: "Social one\n\npost body", ts: 1000}})
	issues := seedExtMessages(t, client, "pm", "", []pageMsgSpec{{msg: "Issue one\n\nissue body", ts: 1010}})
	seedExtMessages(t, client, "memo", "", []pageMsgSpec{{msg: "Memo one\n\nmemo body", ts: 1020}})
	if pending, _ := buildPages(t, client); pending {
		t.Fatal("unexpected pending")
	}

	// Isolation: each type feed carries only its own extension's items.
	posts := getKey(t, client, "posts/feed.xml")
	if !strings.Contains(posts, "Social one") || strings.Contains(posts, "Issue one") || strings.Contains(posts, "Memo one") {
		t.Errorf("posts feed must carry only posts:\n%s", posts)
	}
	issuesFeed := getKey(t, client, "issues/feed.xml")
	if !strings.Contains(issuesFeed, "Issue one") || strings.Contains(issuesFeed, "Social one") {
		t.Errorf("issues feed must carry only issues:\n%s", issuesFeed)
	}
	// Memos get a type feed even though the main feed excludes them.
	if memos := getKey(t, client, "memos/feed.xml"); !strings.Contains(memos, "Memo one") {
		t.Error("memos feed must carry the memo")
	}
	main := getKey(t, client, sitePagesFeedKey)
	if strings.Contains(main, "Memo one") || !strings.Contains(main, "Issue one") || !strings.Contains(main, "Social one") {
		t.Error("main feed must interleave posts+issues and still exclude memos")
	}

	// A type feed's identity block and body content.
	var f atomFeedDoc
	if err := xml.Unmarshal([]byte(issuesFeed), &f); err != nil {
		t.Fatalf("issues feed not well-formed: %v", err)
	}
	if f.ID != "https://example.com/issues/feed.xml" || f.Title != "issues · Pages Test" {
		t.Errorf("issues feed identity wrong: id=%q title=%q", f.ID, f.Title)
	}
	if feedLink(f, "self") != "https://example.com/issues/feed.xml" || feedLink(f, "alternate") != "https://example.com/issues/index.html" {
		t.Errorf("issues feed self/alternate links wrong: %+v", f.Links)
	}
	entry := feedEntryByTitle(t, f, "Issue one")
	if entry.Category.Term != "issue" || entry.Content.Value != "<p>issue body</p>" {
		t.Errorf("issues feed entry wrong: term=%q content=%q", entry.Category.Term, entry.Content.Value)
	}

	// A type with no items still gets a well-formed empty feed at epoch.
	f = atomFeedDoc{}
	if err := xml.Unmarshal([]byte(getKey(t, client, "prs/feed.xml")), &f); err != nil {
		t.Fatalf("prs feed not well-formed: %v", err)
	}
	if len(f.Entries) != 0 || f.Updated != "1970-01-01T00:00:00Z" {
		t.Errorf("empty prs feed wrong: %d entries, updated %q", len(f.Entries), f.Updated)
	}

	// Item pages and the front page carry only the global autodiscovery link.
	typeLink := `title="issues · Pages Test" href="https://example.com/issues/feed.xml"`
	if front := getKey(t, client, sitePagesFrontKey); strings.Contains(front, "issues/feed.xml") {
		t.Error("the front page must carry only the global feed link")
	}
	itemPage := getKey(t, client, "i/"+issues[0][:12]+".html")
	if strings.Contains(itemPage, "issues/feed.xml") || !strings.Contains(itemPage, `href="https://example.com/feed.xml"`) {
		t.Error("an item page must carry only the global feed link")
	}
	if head := getKey(t, client, "issues/index.html"); !strings.Contains(head, typeLink) {
		t.Error("the issues list head must advertise its type feed")
	}

	// Incremental: an issues-only push rewrites issues/feed.xml and the main
	// feed, never the other type feeds.
	mainPuts, issuesPuts := bucket.putCount(sitePagesFeedKey), bucket.putCount("issues/feed.xml")
	postsPuts, memosPuts := bucket.putCount("posts/feed.xml"), bucket.putCount("memos/feed.xml")
	seedExtMessages(t, client, "pm", issues[0], []pageMsgSpec{{msg: "Issue two\n\nsecond body", ts: 1030}})
	if pending, state := buildPages(t, client); pending || state != sitePagesStateOn {
		t.Fatalf("incremental pending=%v state=%q", pending, state)
	}
	if bucket.putCount("issues/feed.xml") != issuesPuts+1 || bucket.putCount(sitePagesFeedKey) != mainPuts+1 {
		t.Error("an issues push must rewrite the issues feed and the main feed once")
	}
	if bucket.putCount("posts/feed.xml") != postsPuts || bucket.putCount("memos/feed.xml") != memosPuts {
		t.Error("an issues push must not rewrite unaffected type feeds")
	}
	f = atomFeedDoc{}
	if err := xml.Unmarshal([]byte(getKey(t, client, "issues/feed.xml")), &f); err != nil {
		t.Fatalf("incremental issues feed not well-formed: %v", err)
	}
	if got := feedEntryByTitle(t, f, "Issue two").Content.Value; got != "<p>second body</p>" {
		t.Errorf("incremental issues entry content = %q, want its body", got)
	}
}

func TestPostPushMaintenance_PublishGuard(t *testing.T) {
	// No pushed guard: a plain git push must leave the bucket clean (no shell,
	// no artifacts) even though refs moved.
	client, _ := testClient(t)
	shas := seedSocialMessages(t, client, "", []pageMsgSpec{{msg: "helper post"}})
	h := &remoteHelper{client: client, prefix: ""}
	h.postPushMaintenance("", true, map[string]string{"social": shas[0]})
	for _, key := range []string{siteManifestKey, siteVersionKey, "index.html", siteItemsManifestKey("social")} {
		if keyExists(client, key) {
			t.Errorf("guard off: helper must not write %s", key)
		}
	}

	// Guard pushed: the same plain push creates the shell and maintains the
	// artifacts.
	client2, _ := testClient(t)
	shas2 := seedSocialMessages(t, client2, "", []pageMsgSpec{{msg: "helper post"}})
	seedPagesConfig(t, client2, map[string]any{"publish": "true"})
	h2 := &remoteHelper{client: client2, prefix: ""}
	h2.postPushMaintenance("", true, map[string]string{"social": shas2[0]})
	for _, key := range []string{siteManifestKey, siteVersionKey, "index.html", siteItemsManifestKey("social")} {
		if !keyExists(client2, key) {
			t.Errorf("guard on: helper must write %s", key)
		}
	}
}

// site_pages_diff_test.go - the Files changed section of a pull request page against a local repository

package site

import (
	"fmt"
	"html"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gitsocial-org/gitsocial/library/core/objstore"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
)

// prDiffRepo builds a repository whose feature branch edits a.txt, adds b.txt, changes bin.dat and deletes c.txt against main; it returns the directory, a git runner and a file writer.
func prDiffRepo(t *testing.T) (dir string, run func(...string) string, write func(name, content string)) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir = t.TempDir()
	run = func(args ...string) string {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "HOME="+dir)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	write = func(name, content string) {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run("init", "-q", "-b", "main")
	run("config", "user.name", "T")
	run("config", "user.email", "t@example.com")
	write("a.txt", "one\ntwo\nthree\n")
	write("bin.dat", "\x00\x01\x02")
	write("c.txt", "gone\n")
	run("add", ".")
	run("commit", "-q", "-m", "base")
	run("switch", "-q", "-c", "feature")
	write("a.txt", "one\n2\nthree\nfour\n")
	write("b.txt", "new\n")
	write("bin.dat", "\x00\x01\x03")
	if err := os.Remove(filepath.Join(dir, "c.txt")); err != nil {
		t.Fatal(err)
	}
	run("add", "-A")
	run("commit", "-q", "-m", "change")
	run("switch", "-q", "main")
	return dir, run, write
}

// prMessage builds a pull request message whose header carries fields; the branch pair is main and feature unless fields name a head.
func prMessage(subject string, fields map[string]string) string {
	head := fields["head"]
	if head == "" {
		head = "#branch:feature"
	}
	header := `GitMsg: ext="review"; type="pull-request"; base="#branch:main"; head="` + head + `"`
	for _, key := range []string{"edits", "state", "base-tip", "head-tip", "merge-base", "merge-head"} {
		if v, ok := fields[key]; ok {
			header += fmt.Sprintf(`; %s="%s"`, key, v)
		}
	}
	return subject + "\n\nbody\n\n" + header + `; v="0.1.0"`
}

// buildPRPages indexes the review branch and runs the page layer over the local repository at dir.
func buildPRPages(t *testing.T, client *objstore.Client, dir string) {
	t.Helper()
	refs := pagesRefs(client, t)
	if err := updateSiteItemsIndex(client, "", "review", refs["refs/heads/gitmsg/review"], nil); err != nil {
		t.Fatalf("items index: %v", err)
	}
	src := objstore.NewLocalCommitSource("", dir)
	defer src.Close()
	if pending, _, err := rebuildSitePages(client, "", refs, "main", src, nil, objstore.SiteOverride{}); err != nil || pending {
		t.Fatalf("rebuildSitePages: pending=%v err=%v", pending, err)
	}
}

// prDiffSection cuts the Files changed section out of a rendered page, with the entities the template wrote for its signs decoded.
func prDiffSection(t *testing.T, page string) string {
	t.Helper()
	start := strings.Index(page, `<div class="diff-section">`)
	end := strings.Index(page, `<footer>`)
	if start < 0 || end < start {
		t.Fatalf("no Files changed section:\n%s", page)
	}
	return html.UnescapeString(page[start:end])
}

// TestSitePages_prDiff asserts a pull request page lists its files with counts and hunks, that a merged edit rewrites the page, and that a squash merge resolves through the merged head ref.
func TestSitePages_prDiff(t *testing.T) {
	dir, run, _ := prDiffRepo(t)
	mainTip, featureTip := run("rev-parse", "main"), run("rev-parse", "feature")
	client, bucket := testClient(t)
	seedPagesConfig(t, client, pagesTestSite())
	pr := seedExtMessages(t, client, "review", "", []pageMsgSpec{{msg: prMessage("Add b", map[string]string{"base-tip": mainTip[:12], "head-tip": featureTip[:12]}), ts: 1000}})
	buildPRPages(t, client, dir)
	key := "i/" + pr[0][:12] + ".html"
	section := prDiffSection(t, getKey(t, client, key))
	for _, want := range []string{
		`<div class="diff-head"><span class="subject">Files changed (4)</span></div>`,
		`<div class="diff-file-head"><span class="diff-status s-modified">M</span><span class="mono diff-path">a.txt</span><span class="diff-counts mono"><span class="cnt-add">+2</span><span class="cnt-del">-1</span></span></div>`,
		`<span class="diff-status s-added">A</span><span class="mono diff-path">b.txt</span><span class="diff-counts mono"><span class="cnt-add">+1</span><span class="cnt-del">-0</span></span>`,
		`<span class="diff-status s-deleted">D</span><span class="mono diff-path">c.txt</span>`,
		`<span class="mono diff-path">bin.dat</span><span class="diff-counts mono">binary</span></div>` + "\n</div>",
		`<div class="diff-file-body"><div class="diff-body unified"><div class="diff-hunk-head mono">@@ -1,3 +1,4 @@</div>`,
		`<div class="diff-line ctx"><span class="dl-num mono">1</span><span class="dl-num mono">1</span><span class="dl-sign mono"> </span><span class="dl-text mono">one</span></div>`,
		`<div class="diff-line del"><span class="dl-num mono">2</span><span class="dl-num mono"></span><span class="dl-sign mono">-</span><span class="dl-text mono">two</span></div>`,
		`<div class="diff-line add"><span class="dl-num mono"></span><span class="dl-num mono">2</span><span class="dl-sign mono">+</span><span class="dl-text mono">2</span></div>`,
		`<div class="diff-hunk-head mono">@@ -0,0 +1,1 @@</div>`,
	} {
		if !strings.Contains(section, want) {
			t.Errorf("section lacks %s:\n%s", want, section)
		}
	}
	if strings.Contains(section, `class="notice"`) {
		t.Errorf("a resolved diff carries a notice:\n%s", section)
	}
	if i, j := strings.Index(section, "a.txt"), strings.Index(section, "b.txt"); i > j {
		t.Error("files are not in tree order")
	}

	// A squash merge deletes the head branch; the merge head survives only at the merged head ref, locally and in the bucket.
	run("merge", "-q", "--squash", "feature")
	run("commit", "-q", "-m", "squash")
	run("branch", "-q", "-D", "feature")
	run("update-ref", siteMergedHeadRefPrefix+pr[0][:12], featureTip)
	run("reflog", "expire", "--expire=now", "--all")
	run("gc", "-q", "--prune=now")
	if err := client.Put(siteMergedHeadRefPrefix+pr[0][:12], []byte(featureTip+"\n")); err != nil {
		t.Fatal(err)
	}
	merged := map[string]string{"edits": "#commit:" + pr[0][:12] + "@gitmsg/review", "state": "merged", "merge-base": mainTip[:12], "merge-head": featureTip[:12]}
	seedExtMessages(t, client, "review", pr[0], []pageMsgSpec{{msg: prMessage("Add b", merged), ts: 2000}})
	puts := bucket.PutCount(key)
	buildPRPages(t, client, dir)
	if bucket.PutCount(key) != puts+1 {
		t.Errorf("the merged edit rewrote the page %d times, want once", bucket.PutCount(key)-puts)
	}
	page := getKey(t, client, key)
	section = prDiffSection(t, page)
	if !strings.Contains(page, `<span class="chip state merged">merged</span>`) {
		t.Error("the page does not show the merged state")
	}
	if !strings.Contains(section, "Files changed (4)") || !strings.Contains(section, `<span class="diff-status s-added">A</span><span class="mono diff-path">b.txt</span>`) || strings.Contains(section, `class="notice"`) {
		t.Errorf("the merged range did not resolve through the merged head ref:\n%s", section)
	}
}

// TestSitePages_prDiffCap asserts a page past the caps lists the first files, carries the first rows, and ends with the omitted count and the app link.
func TestSitePages_prDiffCap(t *testing.T) {
	dir, run, write := prDiffRepo(t)
	run("switch", "-q", "-c", "big", "main")
	write("long.txt", strings.Repeat("line\n", sitePRDiffLineCap+100))
	for i := 0; i < sitePRDiffFileCap+5; i++ {
		write(fmt.Sprintf("many/%03d.txt", i), "x\n")
	}
	run("add", "-A")
	run("commit", "-q", "-m", "big")
	mainTip, bigTip := run("rev-parse", "main"), run("rev-parse", "big")
	client, _ := testClient(t)
	seedPagesConfig(t, client, pagesTestSite())
	pr := seedExtMessages(t, client, "review", "", []pageMsgSpec{{msg: prMessage("Big change", map[string]string{"head": "#branch:big", "base-tip": mainTip[:12], "head-tip": bigTip[:12]}), ts: 1000}})
	buildPRPages(t, client, dir)
	section := prDiffSection(t, getKey(t, client, "i/"+pr[0][:12]+".html"))
	for _, want := range []string{
		fmt.Sprintf(`<span class="subject">Files changed (%d)</span>`, sitePRDiffFileCap+6),
		`<span class="mono diff-path">long.txt</span>`,
		`<span class="mono diff-path">many/000.txt</span>`,
		`<div class="notice">6 more not shown.</div>`,
		fmt.Sprintf(`<div class="notice">The page shows the first %d changed lines. <a href="https://example.com/index.html#commit:%s@gitmsg/review">The app shows the rest.</a></div>`, sitePRDiffLineCap, pr[0][:12]),
	} {
		if !strings.Contains(section, want) {
			t.Errorf("section lacks %s", want)
		}
	}
	if strings.Contains(section, fmt.Sprintf("many/%03d.txt", sitePRDiffFileCap+4)) {
		t.Error("a file past the file cap is listed")
	}
	if got := strings.Count(section, `<div class="diff-line `); got != sitePRDiffLineCap {
		t.Errorf("%d rows, want the cap %d", got, sitePRDiffLineCap)
	}
	if strings.Count(section, `<div class="diff-file">`) != sitePRDiffFileCap {
		t.Errorf("%d files listed, want the cap %d", strings.Count(section, `<div class="diff-file">`), sitePRDiffFileCap)
	}
}

// TestSitePages_prDiffUnresolved asserts a pull request whose range does not resolve carries the notice for its case and its page is still written.
func TestSitePages_prDiffUnresolved(t *testing.T) {
	dir, run, _ := prDiffRepo(t)
	mainTip, featureTip := run("rev-parse", "main"), run("rev-parse", "feature")
	tips := map[string]string{"base-tip": mainTip[:12], "head-tip": featureTip[:12]}
	bogus := strings.Repeat("0", 12)
	cases := []struct {
		name   string
		fields map[string]string
		notice string
	}{
		{"a foreign head", map[string]string{"head": "https://example.org/fork#branch:feature", "base-tip": tips["base-tip"], "head-tip": tips["head-tip"]}, sitePRDiffNoticeTipsUnresolved},
		{"an absent head tip", map[string]string{"base-tip": tips["base-tip"], "head-tip": bogus}, sitePRDiffNoticeTipsUnresolved},
		{"no tips", map[string]string{}, sitePRDiffNoticeNoTips},
		{"a merge range that does not resolve", map[string]string{"state": "merged", "merge-base": bogus, "merge-head": bogus}, sitePRDiffNoticeMergeUnresolved},
		{"a merge range that does not resolve over tips that do", map[string]string{"state": "merged", "merge-base": bogus, "merge-head": bogus, "base-tip": tips["base-tip"], "head-tip": tips["head-tip"]}, ""},
	}
	client, _ := testClient(t)
	seedPagesConfig(t, client, pagesTestSite())
	specs := make([]pageMsgSpec, 0, len(cases))
	for i, c := range cases {
		specs = append(specs, pageMsgSpec{msg: prMessage(c.name, c.fields), ts: int64(1000 + i)})
	}
	prs := seedExtMessages(t, client, "review", "", specs)
	buildPRPages(t, client, dir)
	for i, c := range cases {
		section := prDiffSection(t, getKey(t, client, "i/"+prs[i][:12]+".html"))
		if c.notice == "" {
			if !strings.Contains(section, "Files changed (4)") || strings.Contains(section, `class="notice"`) {
				t.Errorf("%s: the tips did not stand in:\n%s", c.name, section)
			}
			continue
		}
		want := `<div class="diff-head"><span class="subject">Files changed</span></div>` + "\n" + `<div class="notice">` + c.notice + `</div>`
		if !strings.Contains(section, want) || strings.Contains(section, `class="diff-file"`) {
			t.Errorf("%s: want the notice alone:\n%s", c.name, section)
		}
	}
	// With no local source, a pull request with tips carries the notice too.
	msg := &sitePageMsg{Ext: "review", SHA: strings.Repeat("a", 40), Short: strings.Repeat("a", 12), Header: protocol.ParseHeader(extractHeaderLine(prMessage("x", tips)))}
	if d := buildSitePRDiff(&sitePageItem{Msg: msg, Resolved: msg}, sitePageSite{URL: "https://example.com/"}, nil, nil); d == nil || d.Notice != sitePRDiffNoticeTipsUnresolved {
		t.Errorf("no local source: diff = %+v, want the tips notice", d)
	}
}

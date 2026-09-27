// site_tags_test.go - the tags artifact: order, counts since the previous tag, omitted entries, shallow repos, reuse and range documents

package site

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/gitsocial-org/gitsocial/library/core/objstore"
)

// tagsFixture builds a repo of five commits with a lightweight tag on the first and third and an annotated tag on the fifth.
func tagsFixture(t *testing.T) (dir string, refs map[string]string) {
	t.Helper()
	dir = packTestRepo(t, 5)
	gitRun(t, dir, "tag", "v0.9", "main~4")
	gitRun(t, dir, "tag", "v1.0", "main~2")
	gitRun(t, dir, "tag", "-a", "v1.1", "-m", "release", "main")
	refs = map[string]string{}
	for _, name := range []string{"v0.9", "v1.0", "v1.1"} {
		refs["refs/tags/"+name] = gitRun(t, dir, "rev-parse", "refs/tags/"+name)
	}
	return dir, refs
}

// writeAndReadTags runs writeSiteTags against a fresh bucket, or the given one, and returns the artifact.
func writeAndReadTags(t *testing.T, client *objstore.Client, dir string, refs map[string]string) siteTags {
	t.Helper()
	src := objstore.NewLocalCommitSource(filepath.Join(dir, ".git"), "")
	defer src.Close()
	if err := writeSiteTags(client, "", refs, src); err != nil {
		t.Fatalf("writeSiteTags: %v", err)
	}
	var doc siteTags
	if found, err := objstore.ReadCompressedJSON(client, siteTagsKey, &doc); err != nil || !found {
		t.Fatalf("read tags artifact: found=%v err=%v", found, err)
	}
	return doc
}

// tagCount returns an entry's count, -1 when it has none.
func tagCount(e siteTagEntry) int {
	if e.Count == nil {
		return -1
	}
	return *e.Count
}

func TestSiteTags_CountSincePrev(t *testing.T) {
	dir, refs := tagsFixture(t)
	client, _ := testClient(t)
	doc := writeAndReadTags(t, client, dir, refs)
	want := []struct {
		name, prev string
		count      int
	}{{"v1.1", "v1.0", 2}, {"v1.0", "v0.9", 2}, {"v0.9", "", 1}}
	if len(doc.Tags) != len(want) {
		t.Fatalf("got %d entries, want %d: %+v", len(doc.Tags), len(want), doc.Tags)
	}
	for i, w := range want {
		e := doc.Tags[i]
		if e.Name != w.name || e.Prev != w.prev || tagCount(e) != w.count {
			t.Errorf("entry %d = %s prev=%q count=%d, want %s prev=%q count=%d", i, e.Name, e.Prev, tagCount(e), w.name, w.prev, w.count)
		}
	}
	annotated := doc.Tags[0]
	if annotated.SHA == annotated.Commit || annotated.Commit != gitRun(t, dir, "rev-parse", "main") || annotated.Author != "T" || annotated.Time == 0 {
		t.Errorf("annotated entry not peeled to its commit with its tagger: %+v", annotated)
	}
}

func TestSiteTags_MissingObjectOmitsEntry(t *testing.T) {
	dir, refs := tagsFixture(t)
	refs["refs/tags/v2.0"] = "0123456789abcdef0123456789abcdef01234567"
	refs["refs/tags/v1.0"] = "89abcdef0123456789abcdef0123456789abcdef"
	client, _ := testClient(t)
	doc := writeAndReadTags(t, client, dir, refs)
	names := make([]string, 0, len(doc.Tags))
	for _, e := range doc.Tags {
		names = append(names, e.Name)
	}
	if len(names) != 2 || names[0] != "v1.1" || names[1] != "v0.9" {
		t.Errorf("entries = %v, want the two tags the local odb holds at the bucket's sha", names)
	}
}

func TestSiteTags_CarriesTagMissingLocally(t *testing.T) {
	dir, refs := tagsFixture(t)
	client, _ := testClient(t)
	doc := writeAndReadTags(t, client, dir, refs)
	gitRun(t, dir, "tag", "-d", "v1.0")
	again := writeAndReadTags(t, client, dir, refs)
	if len(again.Tags) != 3 || again.Tags[1].Name != "v1.0" || again.Tags[1].Commit != doc.Tags[1].Commit || tagCount(again.Tags[1]) != 2 {
		t.Errorf("entries = %+v, want v1.0 carried from the prior artifact with its count", again.Tags)
	}
	refs["refs/tags/v1.0"] = "89abcdef0123456789abcdef0123456789abcdef"
	moved := writeAndReadTags(t, client, dir, refs)
	if len(moved.Tags) != 2 {
		t.Errorf("entries = %+v, want the moved tag left out", moved.Tags)
	}
}

func TestSiteTags_ShallowOmitsCounts(t *testing.T) {
	dir, _ := tagsFixture(t)
	shallow := t.TempDir()
	gitRun(t, shallow, "clone", "-q", "--depth", "1", "--no-single-branch", "file://"+dir, ".")
	gitRun(t, shallow, "fetch", "-q", "--depth", "1", "origin", "refs/tags/v1.1:refs/tags/v1.1")
	refs := map[string]string{"refs/tags/v1.1": gitRun(t, shallow, "rev-parse", "refs/tags/v1.1")}
	client, _ := testClient(t)
	doc := writeAndReadTags(t, client, shallow, refs)
	if !doc.Shallow || len(doc.Tags) != 1 || doc.Tags[0].Count != nil {
		t.Errorf("shallow artifact = %+v, want shallow with an entry and no count", doc)
	}
}

func TestSiteTags_ReusesUnchangedEntries(t *testing.T) {
	dir, refs := tagsFixture(t)
	client, _ := testClient(t)
	doc := writeAndReadTags(t, client, dir, refs)
	planted := 99
	doc.Tags[1].Count = &planted
	doc.Tags[2].Count = &planted
	doc.Tags[2].PrevCommit = "0123456789abcdef0123456789abcdef01234567"
	data, err := objstore.CompressJSON(doc, objstore.BrotliQualityFull)
	if err != nil {
		t.Fatal(err)
	}
	if err := objstore.PutCompressed(client, siteTagsKey, data, ""); err != nil {
		t.Fatal(err)
	}
	again := writeAndReadTags(t, client, dir, refs)
	if tagCount(again.Tags[1]) != 99 {
		t.Errorf("unchanged entry count = %d, want the copied 99", tagCount(again.Tags[1]))
	}
	if tagCount(again.Tags[2]) != 1 {
		t.Errorf("entry whose previous commit changed = %d, want a fresh count of 1", tagCount(again.Tags[2]))
	}
}

func TestSiteTags_NoTagsDeletesArtifact(t *testing.T) {
	dir, refs := tagsFixture(t)
	client, _ := testClient(t)
	writeAndReadTags(t, client, dir, refs)
	if err := writeSiteTags(client, "", map[string]string{"refs/heads/main": refs["refs/tags/v1.0"]}, nil); err != nil {
		t.Fatalf("writeSiteTags: %v", err)
	}
	var doc siteTags
	if found, _ := objstore.ReadCompressedJSON(client, siteTagsKey, &doc); found {
		t.Errorf("artifact still present after the last tag went: %+v", doc)
	}
}

// readRange reads the range document of a pair; found is false when the bucket has none.
func readRange(t *testing.T, client *objstore.Client, prevCommit, commit string) (siteRange, bool) {
	t.Helper()
	var doc siteRange
	found, err := objstore.ReadCompressedJSON(client, siteRangeKey("", prevCommit, commit), &doc)
	if err != nil {
		t.Fatalf("read range: %v", err)
	}
	return doc, found
}

func TestSiteTags_RangeMatchesGit(t *testing.T) {
	dir, refs := tagsFixture(t)
	client, _ := testClient(t)
	doc := writeAndReadTags(t, client, dir, refs)
	top, oldest := doc.Tags[0], doc.Tags[2]
	if top.Range != siteRangeVersion || oldest.Range != 0 {
		t.Fatalf("range flags = %d, %d, want %d on v1.1 and none on the oldest tag", top.Range, oldest.Range, siteRangeVersion)
	}
	r, found := readRange(t, client, top.PrevCommit, top.Commit)
	if !found || r.MergeBase != top.PrevCommit {
		t.Fatalf("range = %+v found=%v, want merge base %s", r, found, top.PrevCommit)
	}
	want := strings.Fields(gitRun(t, dir, "rev-list", top.Commit, "^"+top.PrevCommit))
	if len(r.Commits) != len(want) || r.Commits[0][0] != want[0] || r.Commits[0][2] != "commit 4" {
		t.Errorf("commits = %v, want %v with subject \"commit 4\" first", r.Commits, want)
	}
	wantFiles := []siteRangeFile{
		{Path: "file-03.txt", Status: "added", ShaB: gitRun(t, dir, "rev-parse", "main:file-03.txt"), ModeB: "100644"},
		{Path: "file-04.txt", Status: "added", ShaB: gitRun(t, dir, "rev-parse", "main:file-04.txt"), ModeB: "100644"},
	}
	if !reflect.DeepEqual(r.Files, wantFiles) || r.Truncated {
		t.Errorf("files = %+v, want %+v", r.Files, wantFiles)
	}
}

func TestSiteTags_RangesReused(t *testing.T) {
	dir, refs := tagsFixture(t)
	client, bucket := testClient(t)
	doc := writeAndReadTags(t, client, dir, refs)
	key := siteRangeKey("", doc.Tags[0].PrevCommit, doc.Tags[0].Commit)
	again := writeAndReadTags(t, client, dir, refs)
	if n := bucket.PutCount(key); n != 1 || again.Tags[0].Range != siteRangeVersion {
		t.Errorf("range document written %d times, flag %d; want one write and the flag kept", n, again.Tags[0].Range)
	}
}

func TestSiteTags_RangeFailureOmitsFlag(t *testing.T) {
	dir, refs := tagsFixture(t)
	client, bucket := testClient(t)
	v11, v10 := gitRun(t, dir, "rev-parse", "main"), gitRun(t, dir, "rev-parse", "main~2")
	bucket.FailPut(siteRangeKey("", v10, v11))
	doc := writeAndReadTags(t, client, dir, refs)
	if doc.Tags[0].Range != 0 || doc.Tags[1].Range != siteRangeVersion {
		t.Errorf("range flags = %d, %d; want none where the document failed and the flag where it was written", doc.Tags[0].Range, doc.Tags[1].Range)
	}
}

func TestSiteTags_ShallowKeepsOnlyMatchingRangeFlags(t *testing.T) {
	dir, refs := tagsFixture(t)
	client, _ := testClient(t)
	doc := writeAndReadTags(t, client, dir, refs)
	doc.Tags[1].PrevCommit = "0123456789abcdef0123456789abcdef01234567"
	data, err := objstore.CompressJSON(doc, objstore.BrotliQualityFull)
	if err != nil {
		t.Fatal(err)
	}
	if err := objstore.PutCompressed(client, siteTagsKey, data, ""); err != nil {
		t.Fatal(err)
	}
	shallow := t.TempDir()
	gitRun(t, shallow, "clone", "-q", "--depth", "1", "--no-single-branch", "file://"+dir, ".")
	gitRun(t, shallow, "fetch", "-q", "--depth", "1", "origin", "refs/tags/v1.1:refs/tags/v1.1")
	gitRun(t, shallow, "tag", "-d", "v1.0")
	again := writeAndReadTags(t, client, shallow, refs)
	if len(again.Tags) != 3 || again.Tags[0].Range != siteRangeVersion || again.Tags[1].Range != 0 {
		t.Errorf("entries = %+v; want v1.1 keeping its flag and the carried v1.0, whose pair changed, without one", again.Tags)
	}
}

// TestSiteTagOrderParity pins the Go tag order to the shared fixture the app's order is checked against (unit_parity.js).
func TestSiteTagOrderParity(t *testing.T) {
	f := loadParityFixtures(t)
	if len(f.TagOrder) == 0 {
		t.Fatal("no tagOrder cases in parity fixtures")
	}
	for _, c := range f.TagOrder {
		t.Run(c.Name, func(t *testing.T) {
			entries := make([]siteTagEntry, len(c.Tags))
			for i, tg := range c.Tags {
				entries[i] = siteTagEntry{Name: tg.Name, Time: tg.Time}
			}
			got := []string{}
			for _, e := range orderSiteTags(entries) {
				got = append(got, e.Name)
			}
			if len(got) != len(c.Expect) {
				t.Fatalf("order = %v, want %v", got, c.Expect)
			}
			for i := range got {
				if got[i] != c.Expect[i] {
					t.Fatalf("order = %v, want %v", got, c.Expect)
				}
			}
		})
	}
}

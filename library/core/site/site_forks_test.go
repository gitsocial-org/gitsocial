// site_forks_test.go - the fork pull request area: pack contents, the cap, the write order, the sweep, pack reuse, isolation and pending heads
package site

import (
	"crypto/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/gitsocial-org/gitsocial/library/core/objstore"
	"github.com/gitsocial-org/gitsocial/library/core/objstore/membucket"
)

// forkFixture pushes a three-commit workspace to a bucket and clones it bare as the fork, whose feature branch adds one commit and whose review branch holds a pull request with one edit.
func forkFixture(t *testing.T) (client *objstore.Client, bucket *membucket.Bucket, refs map[string]string, ws string, pull ForkPull) {
	t.Helper()
	ws = packTestRepo(t, 3)
	client, bucket = pushPackedBucketWith(t, ws, "refs/heads/main")
	var err error
	if refs, err = objstore.ReadRemoteRefs(client, ""); err != nil {
		t.Fatalf("read bucket refs: %v", err)
	}
	forkDir := t.TempDir()
	gitRun(t, forkDir, "clone", "-q", "--bare", ws, ".")
	work := t.TempDir()
	gitRun(t, work, "clone", "-q", forkDir, ".")
	if err := os.WriteFile(filepath.Join(work, "feature.txt"), []byte("feature\n"), 0644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, work, "checkout", "-q", "-b", "feature")
	gitRun(t, work, "add", "feature.txt")
	gitRun(t, work, "commit", "-qm", "feature work")
	gitRun(t, work, "push", "-q", "origin", "feature")
	head := gitRun(t, forkDir, "rev-parse", "refs/heads/feature")
	tree := gitRun(t, forkDir, "mktree", "--missing")
	canonical := gitRun(t, forkDir, "commit-tree", tree, "-m", "Fix the bug\n\nGitMsg: ext=\"review\"; type=\"pull-request\"; state=\"open\"; base=\"#branch:main\"; head=\"#branch:feature\"; v=\"0.1.0\"")
	edit := gitRun(t, forkDir, "commit-tree", tree, "-m", "Fix the bug, again\n\nGitMsg: ext=\"review\"; type=\"pull-request\"; edits=\"#commit:"+canonical+"\"; v=\"0.1.0\"")
	gitRun(t, forkDir, "update-ref", "refs/forks/x/gitmsg/review", edit)
	// The cache names a commit by its 12-character hash, and the area resolves it to the full sha.
	pull = ForkPull{Repo: "https://example.com/bob/repo", Dir: forkDir, Head: head, Commits: []string{canonical[:12], edit[:12]}}
	return client, bucket, refs, ws, pull
}

// sourceOf returns a source over the given pull requests.
func sourceOf(pulls ...ForkPull) ForkPullSource {
	return func() ([]ForkPull, error) { return pulls, nil }
}

// readForkManifest returns the area manifest, failing when it is absent.
func readForkManifest(t *testing.T, client *objstore.Client) siteForkManifest {
	t.Helper()
	var m siteForkManifest
	found, err := objstore.ReadCompressedJSON(client, siteForksManifestKey, &m)
	if err != nil || !found {
		t.Fatalf("read fork manifest: found=%v err=%v", found, err)
	}
	return m
}

// bucketSnapshot returns every key outside the fork area with its body.
func bucketSnapshot(t *testing.T, client *objstore.Client) map[string]string {
	t.Helper()
	keys, err := client.List("")
	if err != nil {
		t.Fatalf("list bucket: %v", err)
	}
	out := map[string]string{}
	for _, key := range keys {
		if strings.HasPrefix(key, siteForksPrefix) {
			continue
		}
		body, err := client.Get(key)
		if err != nil {
			t.Fatalf("get %s: %v", key, err)
		}
		out[key] = string(body)
	}
	return out
}

func TestSiteForks_packExcludesBucketObjects(t *testing.T) {
	client, _, refs, ws, pull := forkFixture(t)
	area, err := writeSiteForks(client, "", refs, sourceOf(pull))
	if err != nil {
		t.Fatalf("writeSiteForks: %v", err)
	}
	m := readForkManifest(t, client)
	if len(m.Items) != 2 || len(m.Items[0].SHA) != 40 || !strings.HasPrefix(m.Items[0].SHA, pull.Commits[0]) || !strings.HasPrefix(m.Items[1].SHA, pull.Commits[1]) || m.Items[0].Repo != pull.Repo {
		t.Fatalf("manifest items = %+v, want the canonical then its edit for %s under full shas", m.Items, pull.Repo)
	}
	entry := m.Items[0]
	if entry.Head != pull.Head || entry.Pack == "" || entry.PackBytes == 0 || m.Bytes != entry.PackBytes || area.Count != 1 || area.Bytes != m.Bytes {
		t.Fatalf("canonical entry = %+v, area = %+v, want the head, its pack and the bytes", entry, area)
	}
	if m.Items[1].Pack != "" || m.Items[1].Head != "" || m.Items[0].Subject != "Fix the bug" || !strings.Contains(m.Items[1].Header, "edits=") {
		t.Errorf("entries carry the wrong fields: %+v", m.Items)
	}
	dir := t.TempDir()
	for _, ext := range []string{".pack", ".idx"} {
		key := siteForksPackPrefix + entry.Pack + ext
		if siteCacheControl(key) != objstore.CacheControlImmutable {
			t.Errorf("%s is not immutable", key)
		}
		data, err := client.Get(key)
		if err != nil {
			t.Fatalf("get %s: %v", key, err)
		}
		if err := os.WriteFile(filepath.Join(dir, entry.Pack+ext), data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	if siteCacheControl(siteForksManifestKey) != "" {
		t.Error("the manifest must stay no-cache")
	}
	packed := map[string]bool{}
	for _, line := range strings.Split(gitRun(t, pull.Dir, "verify-pack", "-v", filepath.Join(dir, entry.Pack+".pack")), "\n") {
		if f := strings.Fields(line); len(f) >= 5 && len(f[0]) == 40 {
			packed[f[0]] = true
		}
	}
	for _, sha := range strings.Fields(gitRun(t, ws, "rev-list", "--objects", "main")) {
		if len(sha) == 40 && packed[sha] {
			t.Errorf("pack holds %s, which a bucket ref reaches", sha)
		}
	}
	for _, sha := range []string{pull.Head, m.Items[0].SHA, m.Items[1].SHA} {
		if !packed[sha] {
			t.Errorf("pack lacks %s", sha)
		}
	}
	// An empty repository resolves no delta base, so index-pack fails on a delta that reaches outside the pack.
	empty := t.TempDir()
	gitRun(t, empty, "init", "-q", "--bare")
	gitRun(t, empty, "index-pack", filepath.Join(dir, entry.Pack+".pack"))
}

func TestSiteForks_packCap(t *testing.T) {
	client, _, refs, _, pull := forkFixture(t)
	work := t.TempDir()
	gitRun(t, work, "clone", "-q", "-b", "feature", pull.Dir, ".")
	big := make([]byte, siteForkPackCap+1<<20)
	if _, err := rand.Read(big); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, "big.bin"), big, 0644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, work, "add", "big.bin")
	gitRun(t, work, "commit", "-qm", "a large head")
	gitRun(t, work, "push", "-q", "origin", "feature")
	pull.Head = gitRun(t, pull.Dir, "rev-parse", "refs/heads/feature")
	area, err := writeSiteForks(client, "", refs, sourceOf(pull))
	if err != nil {
		t.Fatalf("writeSiteForks: %v", err)
	}
	m := readForkManifest(t, client)
	if len(m.Items) != 2 || !m.Items[0].TooLarge || m.Items[0].Pack != "" || m.Items[0].Head != pull.Head || m.Bytes != 0 || area.Bytes != 0 || area.Count != 1 {
		t.Errorf("manifest = %+v, area = %+v, want a tooLarge entry with no pack", m, area)
	}
	if keys, _ := client.List(siteForksPackPrefix); len(keys) != 0 {
		t.Errorf("a pack over the cap was written: %v", keys)
	}
}

func TestSiteForks_manifestLast(t *testing.T) {
	client, bucket, refs, _, pull := forkFixture(t)
	bucket.FailPut(siteForksManifestKey)
	if _, err := writeSiteForks(client, "", refs, sourceOf(pull)); err == nil {
		t.Fatal("a refused manifest write must fail the step")
	}
	keys, _ := client.List(siteForksPackPrefix)
	if len(keys) != 2 {
		t.Fatalf("packs before the manifest: got %v, want the pack and its index", keys)
	}
	if _, _, err := client.HeadObject(siteForksManifestKey); err == nil {
		t.Fatal("the manifest was written although its write was refused")
	}
}

func TestSiteForks_sweep(t *testing.T) {
	client, bucket, refs, _, pull := forkFixture(t)
	for _, key := range []string{siteForksPackPrefix + "pack-stale.pack", siteForksPackPrefix + "pack-stale.idx"} {
		if err := client.Put(key, []byte("old")); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := writeSiteForks(client, "", refs, sourceOf(pull)); err != nil {
		t.Fatalf("writeSiteForks: %v", err)
	}
	m := readForkManifest(t, client)
	keys, _ := client.List(siteForksPackPrefix)
	sort.Strings(keys)
	want := []string{siteForksPackPrefix + m.Items[0].Pack + ".idx", siteForksPackPrefix + m.Items[0].Pack + ".pack"}
	if strings.Join(keys, " ") != strings.Join(want, " ") {
		t.Errorf("packs after the sweep = %v, want %v", keys, want)
	}
	// A second pass finds the pack in the bucket and writes it no second time.
	before := bucket.PutCount(want[1])
	if _, err := writeSiteForks(client, "", refs, sourceOf(pull)); err != nil {
		t.Fatalf("second writeSiteForks: %v", err)
	}
	if bucket.PutCount(want[1]) != before {
		t.Error("a pack the bucket has was uploaded again")
	}
}

func TestSiteForks_reusesPriorPack(t *testing.T) {
	client, bucket, refs, _, pull := forkFixture(t)
	if _, err := writeSiteForks(client, "", refs, sourceOf(pull)); err != nil {
		t.Fatalf("writeSiteForks: %v", err)
	}
	first := readForkManifest(t, client).Items[0].Pack
	packKey := siteForksPackPrefix + first + ".pack"
	// With no tip to exclude, a rebuild would pack every commit under another name; the unchanged head keeps its pack.
	before := bucket.PutCount(packKey)
	if _, err := writeSiteForks(client, "", map[string]string{}, sourceOf(pull)); err != nil {
		t.Fatalf("second writeSiteForks: %v", err)
	}
	if got := readForkManifest(t, client).Items[0].Pack; got != first || bucket.PutCount(packKey) != before {
		t.Errorf("second pass pack = %s (puts %d), want %s reused (puts %d)", got, bucket.PutCount(packKey), first, before)
	}
	// A pack gone from the bucket is built again.
	if err := client.Delete(packKey); err != nil {
		t.Fatal(err)
	}
	if _, err := writeSiteForks(client, "", map[string]string{}, sourceOf(pull)); err != nil {
		t.Fatalf("third writeSiteForks: %v", err)
	}
	got := readForkManifest(t, client).Items[0].Pack
	if got == first || !bucketHasForkPack(client, "", got) {
		t.Errorf("third pass pack = %s, want a new pack the bucket has", got)
	}
}

func TestSiteForks_isolation(t *testing.T) {
	client, _, refs, _, pull := forkFixture(t)
	before := bucketSnapshot(t, client)
	if _, err := writeSiteForks(client, "", refs, sourceOf(pull)); err != nil {
		t.Fatalf("writeSiteForks: %v", err)
	}
	after := bucketSnapshot(t, client)
	if len(before) != len(after) {
		t.Fatalf("keys outside the area changed: %d before, %d after", len(before), len(after))
	}
	for key, body := range before {
		if after[key] != body {
			t.Errorf("%s changed", key)
		}
	}
	for key := range after {
		if _, ok := before[key]; !ok {
			t.Errorf("%s was written outside the area", key)
		}
	}
}

func TestSiteForks_pendingHead(t *testing.T) {
	client, _, refs, _, pull := forkFixture(t)
	fetched := pull.Head
	pull.Head = ""
	area, err := writeSiteForks(client, "", refs, sourceOf(pull))
	if err != nil {
		t.Fatalf("writeSiteForks: %v", err)
	}
	m := readForkManifest(t, client)
	if len(m.Items) != 2 || !m.Items[0].Pending || m.Items[0].Pack != "" || m.Items[0].Head != "" || area.Count != 1 || area.Bytes != 0 {
		t.Errorf("manifest = %+v, area = %+v, want a pending entry", m, area)
	}
	if keys, _ := client.List(siteForksPackPrefix); len(keys) != 0 {
		t.Errorf("a pending head wrote packs: %v", keys)
	}
	pull.Head = fetched
	if _, err := writeSiteForks(client, "", refs, sourceOf(pull)); err != nil {
		t.Fatalf("second writeSiteForks: %v", err)
	}
	if m := readForkManifest(t, client); m.Items[0].Pending || m.Items[0].Pack == "" {
		t.Errorf("the fetched head was not written: %+v", m.Items[0])
	}
	if _, err := writeSiteForks(client, "", refs, nil); err != nil {
		t.Fatalf("writeSiteForks without a source: %v", err)
	}
	if m := readForkManifest(t, client); len(m.Items) != 0 || m.Bytes != 0 || m.Version != siteForksVersion {
		t.Errorf("manifest without a source = %+v, want no items", m)
	}
}

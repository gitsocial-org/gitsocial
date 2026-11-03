// site_avatars_test.go - Tests for the repository slug and the sync of the local avatar store to the bucket
package site

import (
	"bytes"
	"image"
	"image/png"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/gitsocial-org/gitsocial/library/core/objstore"
	"github.com/gitsocial-org/gitsocial/library/core/objstore/membucket"
)

// avatarTestPNG encodes a 1x1 PNG.
func avatarTestPNG(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// avatarTestClient is testClient with each PUT recorded in order as key, Content-Type and Cache-Control.
func avatarTestClient(t *testing.T) (*objstore.Client, *membucket.Bucket, func() []string) {
	t.Helper()
	bucket := membucket.New()
	var mu sync.Mutex
	var puts []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			mu.Lock()
			puts = append(puts, membucket.KeyOf(r.URL.Path)+"|"+r.Header.Get("Content-Type")+"|"+r.Header.Get("Cache-Control"))
			mu.Unlock()
		}
		bucket.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	client, err := objstore.NewClient(objstore.Config{Endpoint: srv.URL, Bucket: "b", Region: "us-east-1", AccessKey: "k", SecretKey: "s", PathStyle: true})
	if err != nil {
		t.Fatal(err)
	}
	return client, bucket, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(puts)
	}
}

// writeAvatarStoreFile puts one file into a store directory.
func writeAvatarStoreFile(t *testing.T, dir, key string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, key), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

// avatarPuts keeps the recorded PUTs under avatars/ and the manifest.
func avatarPuts(puts []string) []string {
	var out []string
	for _, p := range puts {
		if strings.HasPrefix(p, AvatarsDir) || strings.HasPrefix(p, AvatarManifestKey+"|") {
			out = append(out, p)
		}
	}
	return out
}

// manifestKeys reads the keys of the manifest in the bucket.
func manifestKeys(t *testing.T, client *objstore.Client) []string {
	t.Helper()
	keys, _, err := readAvatarManifest(client, "")
	if err != nil {
		t.Fatal(err)
	}
	return slices.Sorted(maps.Keys(keys))
}

func TestRepoSlug(t *testing.T) {
	cases := []struct{ input, want string }{
		{"https://github.com/user/repo", "github.com-user-repo"},
		{"http://github.com/user/repo", "github.com-user-repo"},
		{"https://github.com/user/repo.git", "github.com-user-repo"},
		{"https://gitlab.example.com:8080/group/project", "gitlab.example.com-8080-group-project"},
	}
	for _, c := range cases {
		if got := RepoSlug(c.input); got != c.want {
			t.Errorf("RepoSlug(%q) = %q, want %q", c.input, got, c.want)
		}
	}
	long := "https://github.com/very-long-organization-name/very-long-repository-name-that-goes-on-and-on"
	if got := RepoSlug(long); len(got) > 50 {
		t.Errorf("RepoSlug of a long URL has %d characters, want at most 50", len(got))
	}
}

// TestRebuild_avatarsFromStore: the push uploads each store picture the manifest lacks with the import headers, then writes the manifest with the union.
func TestRebuild_avatarsFromStore(t *testing.T) {
	client, bucket, puts := avatarTestClient(t)
	img := avatarTestPNG(t)
	listed, stored := AvatarKey("listed@example.com"), AvatarKey("stored@example.com")
	bucket.Seed(AvatarsDir+listed, img)
	if err := WriteAvatarManifest(client, "", []string{listed}); err != nil {
		t.Fatal(err)
	}
	store := filepath.Join(t.TempDir(), "store")
	writeAvatarStoreFile(t, store, listed, img)
	writeAvatarStoreFile(t, store, stored, img)
	before := len(puts())
	if _, err := rebuildSite(client, "", nil, objstore.SiteOverride{}, nil, store); err != nil {
		t.Fatal(err)
	}
	got := avatarPuts(puts()[before:])
	want := []string{AvatarsDir + stored + "|image/png|public, max-age=86400", AvatarManifestKey + "|application/json|no-cache"}
	if !slices.Equal(got, want) {
		t.Errorf("avatar puts = %v, want %v", got, want)
	}
	if body, ok := bucket.Object(AvatarsDir + stored); !ok || body != string(img) {
		t.Error("the store picture is not in the bucket as stored")
	}
	if keys := manifestKeys(t, client); !slices.Equal(keys, slices.Sorted(slices.Values([]string{listed, stored}))) {
		t.Errorf("manifest keys = %v, want the listed and the stored key", keys)
	}
}

// TestRebuild_avatarsCurrentNoWrite: a manifest that holds every store key gets no picture write and no manifest write.
func TestRebuild_avatarsCurrentNoWrite(t *testing.T) {
	client, _, puts := avatarTestClient(t)
	key := AvatarKey("ada@example.com")
	if err := WriteAvatarManifest(client, "", []string{key}); err != nil {
		t.Fatal(err)
	}
	store := filepath.Join(t.TempDir(), "store")
	writeAvatarStoreFile(t, store, key, avatarTestPNG(t))
	before := len(puts())
	if _, err := rebuildSite(client, "", nil, objstore.SiteOverride{}, nil, store); err != nil {
		t.Fatal(err)
	}
	if got := avatarPuts(puts()[before:]); len(got) != 0 {
		t.Errorf("avatar puts = %v, want none", got)
	}
}

// TestSyncAvatars_absentStore: an absent or empty store, or a workspace with no origin, reads no bucket key.
func TestSyncAvatars_absentStore(t *testing.T) {
	client, bucket := testClient(t)
	empty := t.TempDir()
	noOrigin := t.TempDir()
	gitRun(t, noOrigin, "init", "-q", "-b", "main")
	if dir := workspaceAvatarStore(noOrigin, t.TempDir()); dir != "" {
		t.Errorf("store of a workspace with no origin = %q, want none", dir)
	}
	for _, dir := range []string{"", filepath.Join(empty, "absent"), empty, workspaceAvatarStore(noOrigin, t.TempDir())} {
		if err := syncAvatarStore(client, "", dir, nil); err != nil {
			t.Fatalf("sync %q: %v", dir, err)
		}
	}
	if bucket.GetCount(AvatarManifestKey) != 0 || bucket.ListCount() != 0 || bucket.TotalPuts() != 0 {
		t.Errorf("bucket gets %d, lists %d, puts %d, want none", bucket.GetCount(AvatarManifestKey), bucket.ListCount(), bucket.TotalPuts())
	}
}

// TestSyncAvatars_perRepository: the push reads the store of the origin of the workspace and no other.
func TestSyncAvatars_perRepository(t *testing.T) {
	client, bucket := testClient(t)
	cacheDir := t.TempDir()
	key := AvatarKey("ada@example.com")
	writeAvatarStoreFile(t, AvatarStoreDir(cacheDir, "https://github.com/o/a"), key, avatarTestPNG(t))
	work := t.TempDir()
	gitRun(t, work, "init", "-q", "-b", "main")
	gitRun(t, work, "remote", "add", "origin", "https://github.com/o/b.git")
	if err := syncAvatarStore(client, "", workspaceAvatarStore(work, cacheDir), nil); err != nil {
		t.Fatal(err)
	}
	if _, ok := bucket.Object(AvatarsDir + key); ok {
		t.Fatal("the push of one repository uploaded the store of another")
	}
	gitRun(t, work, "remote", "set-url", "origin", "https://github.com/o/a.git")
	if err := syncAvatarStore(client, "", workspaceAvatarStore(work, cacheDir), nil); err != nil {
		t.Fatal(err)
	}
	if _, ok := bucket.Object(AvatarsDir + key); !ok {
		t.Error("the push did not upload the store of its own origin")
	}
}

// TestRebuild_avatarsMoveMarker: a picture the push uploads moves the skip marker, so the pass runs once, then the marker holds.
func TestRebuild_avatarsMoveMarker(t *testing.T) {
	client, bucket := testClient(t)
	if err := client.Put("HEAD", []byte("ref: refs/heads/main\n")); err != nil {
		t.Fatal(err)
	}
	var phases []string
	progress := objstore.Progress(func(phase string, done, total int) { phases = append(phases, phase) })
	store := filepath.Join(t.TempDir(), "store")
	rebuild := func() bool {
		t.Helper()
		phases = nil
		if _, err := rebuildSite(client, "", nil, objstore.SiteOverride{}, progress, store); err != nil {
			t.Fatal(err)
		}
		return slices.Contains(phases, "site up to date")
	}
	if rebuild() || !rebuild() {
		t.Fatal("the marker does not hold with an empty store")
	}
	writeAvatarStoreFile(t, store, AvatarKey("ada@example.com"), avatarTestPNG(t))
	stamps := bucket.PutCount(sitePushStateKey)
	if rebuild() {
		t.Error("an uploaded picture left the marker in place")
	}
	if bucket.PutCount(sitePushStateKey) != stamps+1 {
		t.Error("the pass after the upload did not stamp the marker")
	}
	if !rebuild() {
		t.Error("the marker does not hold after the pass")
	}
}

// TestSyncAvatars_skipsBadFile: a store file that is not a PNG, JPEG or GIF under the cap is skipped with a note and stays.
func TestSyncAvatars_skipsBadFile(t *testing.T) {
	client, bucket := testClient(t)
	store := t.TempDir()
	good, html, big := AvatarKey("good@example.com"), AvatarKey("html@example.com"), AvatarKey("big@example.com")
	writeAvatarStoreFile(t, store, good, avatarTestPNG(t))
	writeAvatarStoreFile(t, store, html, []byte("<html>no</html>"))
	writeAvatarStoreFile(t, store, big, append(avatarTestPNG(t), make([]byte, AvatarMaxBytes)...))
	var notes []string
	progress := objstore.Progress(func(phase string, done, total int) { notes = append(notes, phase) })
	if err := syncAvatarStore(client, "", store, progress); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{html, big} {
		if _, ok := bucket.Object(AvatarsDir + key); ok {
			t.Errorf("bad file %s was uploaded", key)
		}
		if !slices.ContainsFunc(notes, func(n string) bool { return strings.Contains(n, key) && strings.Contains(n, "skipped") }) {
			t.Errorf("no note for bad file %s in %v", key, notes)
		}
		if _, err := os.Stat(filepath.Join(store, key)); err != nil {
			t.Errorf("bad file %s left the store: %v", key, err)
		}
	}
	if keys := manifestKeys(t, client); !slices.Equal(keys, []string{good}) {
		t.Errorf("manifest keys = %v, want only the good picture", keys)
	}
}

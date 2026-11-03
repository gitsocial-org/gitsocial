// avatars_test.go - Tests for the avatar run: write order, skips, the rate limit and the download check
package importpkg

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/objstore/membucket"
	"github.com/gitsocial-org/gitsocial/library/core/site"
)

// avatarPNG encodes a 1x1 PNG.
func avatarPNG(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// avatarGit runs git in dir as the given author.
func avatarGit(t *testing.T, dir, name, email string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=" + name, "-c", "user.email=" + email}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "HOME="+t.TempDir())
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// avatarRepo builds a workspace with two code authors and a gitmsg/pm branch carrying one imported issue and its edit.
func avatarRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	avatarGit(t, dir, "T", "t@example.com", "init", "-q", "-b", "main")
	avatarGit(t, dir, "Ada", "ada@example.com", "commit", "-q", "--allow-empty", "-m", "first")
	avatarGit(t, dir, "Bob", "Bob@Example.com", "commit", "-q", "--allow-empty", "-m", "second")
	avatarGit(t, dir, "T", "t@example.com", "switch", "-q", "--orphan", "gitmsg/pm")
	avatarGit(t, dir, "Importer", "importer@example.com", "commit", "-q", "--allow-empty", "-m",
		"An issue\n\nGitMsg: ext=\"pm\"; type=\"issue\"; state=\"open\"; origin-author-email=\"cy@example.com\"; origin-url=\"https://github.com/o/r/issues/1\"; v=\"0.1.0\"")
	avatarGit(t, dir, "Importer", "importer@example.com", "commit", "-q", "--allow-empty", "-m",
		"An issue\n\nGitMsg: ext=\"pm\"; type=\"issue\"; state=\"closed\"; edits=\"#commit:aaaaaaaaaaaa@gitmsg/pm\"; origin-author-email=\"dee@example.com\"; origin-url=\"https://github.com/o/r/issues/1\"; v=\"0.1.0\"")
	avatarGit(t, dir, "T", "t@example.com", "switch", "-q", "main")
	return dir
}

// avatarBucket serves a membucket and records each PUT key and its headers in order.
type avatarBucket struct {
	mu      sync.Mutex
	puts    []string
	headers map[string]http.Header
	onPut   func(key string)
	bucket  *membucket.Bucket
}

// ServeHTTP records a PUT and delegates to the membucket.
func (b *avatarBucket) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPut {
		key := strings.TrimPrefix(r.URL.Path, "/b/")
		b.mu.Lock()
		b.puts = append(b.puts, key)
		b.headers[key] = r.Header.Clone()
		b.mu.Unlock()
		if b.onPut != nil {
			b.onPut(key)
		}
	}
	b.bucket.ServeHTTP(w, r)
}

// avatarBucketServer starts a recorded bucket and returns it with the s3 remote URL that reaches it.
func avatarBucketServer(t *testing.T) (*avatarBucket, string) {
	t.Helper()
	t.Setenv("GITSOCIAL_S3_ACCESS_KEY", "k")
	t.Setenv("GITSOCIAL_S3_SECRET_KEY", "s")
	b := &avatarBucket{headers: map[string]http.Header{}, bucket: membucket.New()}
	srv := httptest.NewServer(b)
	t.Cleanup(srv.Close)
	return b, "s3://" + strings.TrimPrefix(srv.URL, "http://") + "/b/"
}

// avatarImageServer serves body at every path and counts the downloads.
func avatarImageServer(t *testing.T, body []byte, contentType string) (*httptest.Server, *int) {
	t.Helper()
	var mu sync.Mutex
	count := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		count++
		mu.Unlock()
		w.Header().Set("Content-Type", contentType)
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv, &count
}

// readManifestKeys reads the manifest keys a membucket holds.
func readManifestKeys(t *testing.T, b *membucket.Bucket) []string {
	t.Helper()
	raw, ok := b.Object(site.AvatarManifestKey)
	if !ok {
		t.Fatal("no avatar manifest")
	}
	var m site.AvatarManifest
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatal(err)
	}
	return m.Keys
}

// TestImportAvatars_writeOrder: every picture is uploaded before the manifest, and the manifest before the memo.
func TestImportAvatars_writeOrder(t *testing.T) {
	dir := avatarRepo(t)
	bucket, remote := avatarBucketServer(t)
	img, _ := avatarImageServer(t, avatarPNG(t), "application/octet-stream")
	cacheDir := t.TempDir()
	memoPath := avatarMemoPath(cacheDir, "https://github.com/o/r")
	memoAtManifest := true
	bucket.onPut = func(key string) {
		if key == site.AvatarManifestKey {
			_, err := os.Stat(memoPath)
			memoAtManifest = err == nil
		}
	}
	asked := map[string]string{}
	resolve := func(email string, originURLs []string, sha string) (string, error) {
		asked[email] = strings.Join(originURLs, ",") + "|" + fmt.Sprint(len(sha))
		return img.URL + "/" + email, nil
	}
	stats, err := RunAvatars(AvatarOptions{WorkDir: dir, RepoURL: "https://github.com/o/r", RemoteURL: remote, CacheDir: cacheDir, Source: "github", Resolve: resolve}, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"ada@example.com": "|40", "bob@example.com": "|40", "cy@example.com": "https://github.com/o/r/issues/1|0"}
	if fmt.Sprint(asked) != fmt.Sprint(want) {
		t.Errorf("resolved %v, want %v: an edit author and a gitmsg git author have nothing to look up", asked, want)
	}
	if stats.Uploaded != 3 {
		t.Fatalf("stats = %+v, want 3 uploads", stats)
	}
	last := len(bucket.puts) - 1
	if last < 0 || bucket.puts[last] != site.AvatarManifestKey {
		t.Fatalf("puts = %v, want the manifest last", bucket.puts)
	}
	for _, key := range bucket.puts[:last] {
		if !strings.HasPrefix(key, site.AvatarsDir) {
			t.Errorf("put %s before the manifest, want only pictures", key)
		}
		if h := bucket.headers[key]; h.Get("Content-Type") != "image/png" || h.Get("Cache-Control") != "public, max-age=86400" {
			t.Errorf("picture %s headers = %v", key, h)
		}
	}
	if memoAtManifest {
		t.Error("the memo was written before the manifest")
	}
	if _, err := os.Stat(memoPath); err != nil {
		t.Errorf("no memo after the run: %v", err)
	}
	if got := readManifestKeys(t, bucket.bucket); len(got) != 3 || got[0] > got[1] || got[1] > got[2] {
		t.Errorf("manifest keys = %v, want three sorted", got)
	}
}

// TestImportAvatars_skipsKnown: an author the bucket holds or a fresh miss costs no call and no download.
func TestImportAvatars_skipsKnown(t *testing.T) {
	dir := avatarRepo(t)
	bucket, remote := avatarBucketServer(t)
	bucket.bucket.Seed(site.AvatarsDir+site.AvatarKey("ada@example.com"), avatarPNG(t))
	img, downloads := avatarImageServer(t, avatarPNG(t), "image/png")
	cacheDir := t.TempDir()
	memo := map[string]avatarMemo{
		"bob@example.com": {Status: "miss", At: time.Now().Add(-24 * time.Hour), Source: "github"},
		"cy@example.com":  {Status: "miss", At: time.Now().Add(-40 * 24 * time.Hour), Source: "github"},
	}
	if err := writeAvatarMemo(avatarMemoPath(cacheDir, "https://github.com/o/r"), memo); err != nil {
		t.Fatal(err)
	}
	var asked []string
	resolve := func(email string, originURLs []string, sha string) (string, error) {
		asked = append(asked, email)
		return img.URL, nil
	}
	stats, err := RunAvatars(AvatarOptions{WorkDir: dir, RepoURL: "https://github.com/o/r", RemoteURL: remote, CacheDir: cacheDir, Resolve: resolve}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(asked, ",") != "cy@example.com" || *downloads != 1 {
		t.Errorf("resolved %v with %d downloads, want only the stale miss", asked, *downloads)
	}
	if stats.Done != 1 || stats.FreshMisses != 1 || stats.Uploaded != 1 {
		t.Errorf("stats = %+v", stats)
	}
	if got := readManifestKeys(t, bucket.bucket); len(got) != 2 {
		t.Errorf("manifest keys = %v, want the listed picture and the new one", got)
	}
}

// TestImportAvatars_stopsOnRateLimit: a rate-limited run stops, writes the manifest and the memo, and reports the remainder.
func TestImportAvatars_stopsOnRateLimit(t *testing.T) {
	dir := avatarRepo(t)
	bucket, remote := avatarBucketServer(t)
	img, _ := avatarImageServer(t, avatarPNG(t), "image/png")
	cacheDir := t.TempDir()
	calls := 0
	resolve := func(email string, originURLs []string, sha string) (string, error) {
		calls++
		if calls > 1 {
			return "", fmt.Errorf("%w: gh api", ErrRateLimited)
		}
		return img.URL, nil
	}
	stats, err := RunAvatars(AvatarOptions{WorkDir: dir, RepoURL: "https://github.com/o/r", RemoteURL: remote, CacheDir: cacheDir, Resolve: resolve}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !stats.RateLimited || stats.Uploaded != 1 || stats.Remaining != 2 || calls != 2 {
		t.Errorf("stats = %+v after %d calls, want one upload, two remaining", stats, calls)
	}
	if got := readManifestKeys(t, bucket.bucket); len(got) != 1 {
		t.Errorf("manifest keys = %v, want the one upload", got)
	}
	memo, err := readAvatarMemo(avatarMemoPath(cacheDir, "https://github.com/o/r"))
	if err != nil || memo["ada@example.com"].Status != "hit" || len(memo) != 1 {
		t.Errorf("memo = %v %v, want the one hit and nothing for the remainder", memo, err)
	}
}

// TestFetchAvatar_rejects: a download that is not a PNG, JPEG or GIF, is over the cap or fails is an error.
func TestFetchAvatar_rejects(t *testing.T) {
	html, _ := avatarImageServer(t, []byte("<html>no</html>"), "image/png")
	big, _ := avatarImageServer(t, append(avatarPNG(t), make([]byte, site.AvatarMaxBytes)...), "image/png")
	missing := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(missing.Close)
	for name, srv := range map[string]*httptest.Server{"html served as a png": html, "over the cap": big, "not found": missing} {
		if _, _, err := fetchAvatarImage(srv.URL); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

// TestFetchAvatar_keepsBytes: the bytes are kept as served and the type is the decoded format, not the response header.
func TestFetchAvatar_keepsBytes(t *testing.T) {
	body := avatarPNG(t)
	srv, _ := avatarImageServer(t, body, "text/plain")
	data, contentType, err := fetchAvatarImage(srv.URL)
	if err != nil || !bytes.Equal(data, body) || contentType != "image/png" {
		t.Errorf("fetch = %d bytes, %q, %v", len(data), contentType, err)
	}
}

// TestImportAvatars_failureNotMemoed: a 5xx download is a failure the next run retries, and a 4xx is a miss in the memo.
func TestImportAvatars_failureNotMemoed(t *testing.T) {
	dir := avatarRepo(t)
	_, remote := avatarBucketServer(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/gone") {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusBadGateway)
	}))
	t.Cleanup(srv.Close)
	cacheDir := t.TempDir()
	resolve := func(email string, originURLs []string, sha string) (string, error) {
		if email == "ada@example.com" {
			return srv.URL + "/gone", nil
		}
		return srv.URL + "/busy", nil
	}
	stats, err := RunAvatars(AvatarOptions{WorkDir: dir, RepoURL: "https://github.com/o/r", RemoteURL: remote, CacheDir: cacheDir, Resolve: resolve}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Missed != 1 || stats.Failed != 2 {
		t.Errorf("stats = %+v, want one miss and two failures", stats)
	}
	memo, err := readAvatarMemo(avatarMemoPath(cacheDir, "https://github.com/o/r"))
	if err != nil || len(memo) != 1 || memo["ada@example.com"].Status != "miss" {
		t.Errorf("memo = %v %v, want only the 4xx miss", memo, err)
	}
}

// TestImportAvatars_writesStore: a downloaded picture and a --set-file picture are in the store before the bucket upload.
func TestImportAvatars_writesStore(t *testing.T) {
	dir := avatarRepo(t)
	bucket, remote := avatarBucketServer(t)
	body := avatarPNG(t)
	img, _ := avatarImageServer(t, body, "image/png")
	cacheDir := t.TempDir()
	storeDir := site.AvatarStoreDir(cacheDir, "https://github.com/o/r")
	var missing []string
	bucket.onPut = func(key string) {
		if name, ok := strings.CutPrefix(key, site.AvatarsDir); ok {
			if _, err := os.Stat(filepath.Join(storeDir, name)); err != nil {
				missing = append(missing, name)
			}
		}
	}
	resolve := func(email string, originURLs []string, sha string) (string, error) { return img.URL, nil }
	opts := AvatarOptions{WorkDir: dir, RepoURL: "https://github.com/o/r", RemoteURL: remote, CacheDir: cacheDir, Resolve: resolve}
	if _, err := RunAvatars(opts, nil); err != nil {
		t.Fatal(err)
	}
	setFile := filepath.Join(t.TempDir(), "set.png")
	if err := os.WriteFile(setFile, body, 0o600); err != nil {
		t.Fatal(err)
	}
	opts.SetEmail, opts.SetFile = "eve@example.com", setFile
	if _, err := RunAvatars(opts, nil); err != nil {
		t.Fatal(err)
	}
	if len(missing) > 0 {
		t.Errorf("uploaded %v before the store held them", missing)
	}
	for _, email := range []string{"ada@example.com", "bob@example.com", "cy@example.com", "eve@example.com"} {
		if got, err := os.ReadFile(filepath.Join(storeDir, site.AvatarKey(email))); err != nil || !bytes.Equal(got, body) {
			t.Errorf("store picture of %s = %d bytes, %v", email, len(got), err)
		}
	}
	if keys, err := site.ListAvatarStore(storeDir); err != nil || len(keys) != 4 {
		t.Errorf("store keys = %v %v, want four and no temp file", keys, err)
	}
}

// TestImportAvatars_storeIsDone: a store key counts as done, so a run against an empty bucket calls no forge for it.
func TestImportAvatars_storeIsDone(t *testing.T) {
	dir := avatarRepo(t)
	img, downloads := avatarImageServer(t, avatarPNG(t), "image/png")
	cacheDir := t.TempDir()
	var asked []string
	resolve := func(email string, originURLs []string, sha string) (string, error) {
		asked = append(asked, email)
		return img.URL, nil
	}
	opts := AvatarOptions{WorkDir: dir, RepoURL: "https://github.com/o/r", CacheDir: cacheDir, Resolve: resolve}
	first, err := RunAvatars(opts, nil)
	if err != nil || first.Uploaded != 3 {
		t.Fatalf("first run = %+v %v, want three pictures in the store", first, err)
	}
	asked, *downloads = nil, 0
	bucket, remote := avatarBucketServer(t)
	opts.RemoteURL = remote
	second, err := RunAvatars(opts, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(asked) != 0 || *downloads != 0 || second.Done != 3 {
		t.Errorf("second run asked %v with %d downloads, stats %+v, want every author done", asked, *downloads, second)
	}
	if len(bucket.puts) != 0 {
		t.Errorf("second run put %v, want nothing: the push publishes the store", bucket.puts)
	}
}

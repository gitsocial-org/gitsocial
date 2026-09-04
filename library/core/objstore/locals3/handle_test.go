// handle_test.go - HTTP surface tests for locals3's single handler: the
// GET/HEAD/PUT/DELETE + ListObjectsV2 subset the git remote helper and the
// browser reader depend on.
//
// The contract under test is documentation/S3.md: conditional writes are what
// ref updates use for compare-and-swap (etag mode) and for create-only chains
// (generation mode); conditional GETs are what the no-cache mutable keys rely
// on to revalidate cheaply; Range GETs are how the browser reads one object out
// of a packfile; the listing's per-object ETag is what the push-state marker
// fingerprints. Requests are driven through httptest, with the package-level
// bucket root pointed at a temp dir.
package main

import (
	"bufio"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newBucketRoot points the package-level root at a fresh temp dir for one test.
func newBucketRoot(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	withRoot(t, dir)
	return dir
}

// request drives one request through handle and returns the recorded response.
func request(t *testing.T, method, target string, body []byte, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var payload io.Reader
	if body != nil {
		payload = bytes.NewReader(body)
	}
	req := httptest.NewRequest(method, target, payload)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	recorder := httptest.NewRecorder()
	handle(recorder, req)
	return recorder
}

// putObject stores a key and fails the test unless the write is accepted.
func putObject(t *testing.T, key, body string) {
	t.Helper()
	res := request(t, http.MethodPut, "/"+key, []byte(body), nil)
	if res.Code != 200 {
		t.Fatalf("PUT %s = %d, want 200", key, res.Code)
	}
}

// TestPutGetRoundTrip covers the base write/read path: the body comes back
// byte-identical with the ETag, Cache-Control and Content-Type a real bucket
// serves, and the object lands under the bucket directory on disk.
func TestPutGetRoundTrip(t *testing.T) {
	dir := newBucketRoot(t)
	const body = "9b8c1f2e3d4a5b6c7d8e9f0a1b2c3d4e5f60718293a4b5c6\n"
	put := request(t, http.MethodPut, "/showcase/refs/heads/main", []byte(body), nil)
	if put.Code != 200 {
		t.Fatalf("PUT = %d, want 200", put.Code)
	}
	if got, want := put.Header().Get("ETag"), etagOf([]byte(body)); got != want {
		t.Errorf("PUT ETag = %s, want %s", got, want)
	}
	onDisk, err := os.ReadFile(filepath.Join(dir, "showcase", "refs", "heads", "main"))
	if err != nil {
		t.Fatalf("object not stored under the bucket directory: %v", err)
	}
	if string(onDisk) != body {
		t.Errorf("stored bytes = %q, want %q", onDisk, body)
	}
	get := request(t, http.MethodGet, "/showcase/refs/heads/main", nil, nil)
	if get.Code != 200 {
		t.Fatalf("GET = %d, want 200", get.Code)
	}
	if get.Body.String() != body {
		t.Errorf("GET body = %q, want %q", get.Body.String(), body)
	}
	if got, want := get.Header().Get("ETag"), etagOf([]byte(body)); got != want {
		t.Errorf("GET ETag = %s, want %s", got, want)
	}
	if got := get.Header().Get("Cache-Control"); got != "no-cache" {
		t.Errorf("GET Cache-Control for a ref key = %q, want %q", got, "no-cache")
	}
	if got := get.Header().Get("Content-Type"); got != "application/octet-stream" {
		t.Errorf("GET Content-Type for a ref key = %q, want %q", got, "application/octet-stream")
	}
}

// TestGetMissingKeyIs404 checks an absent key reads as 404 with no body, which
// is how every caller detects a key it has not written yet.
func TestGetMissingKeyIs404(t *testing.T) {
	newBucketRoot(t)
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		res := request(t, method, "/showcase/refs/heads/absent", nil, nil)
		if res.Code != 404 {
			t.Errorf("%s missing key = %d, want 404", method, res.Code)
		}
		if res.Body.Len() != 0 {
			t.Errorf("%s missing key wrote a body: %q", method, res.Body.String())
		}
	}
}

// TestHeadReportsSizeWithoutBody checks HEAD answers the metadata the pusher's
// skip-existing check reads (ETag, Content-Length, Cache-Control,
// Content-Type) and sends no body.
func TestHeadReportsSizeWithoutBody(t *testing.T) {
	newBucketRoot(t)
	const body = `{"schema":4,"items":[]}`
	putObject(t, "showcase/.gitsocial/site/items/pm/shard-abc123.json", body)
	res := request(t, http.MethodHead, "/showcase/.gitsocial/site/items/pm/shard-abc123.json", nil, nil)
	if res.Code != 200 {
		t.Fatalf("HEAD = %d, want 200", res.Code)
	}
	if res.Body.Len() != 0 {
		t.Errorf("HEAD wrote a body: %q", res.Body.String())
	}
	if got, want := res.Header().Get("Content-Length"), fmt.Sprintf("%d", len(body)); got != want {
		t.Errorf("HEAD Content-Length = %q, want %q", got, want)
	}
	if got, want := res.Header().Get("ETag"), etagOf([]byte(body)); got != want {
		t.Errorf("HEAD ETag = %s, want %s", got, want)
	}
	if got := res.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Errorf("HEAD Cache-Control for a sealed shard = %q, want the immutable policy", got)
	}
	if got := res.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("HEAD Content-Type = %q, want %q", got, "application/json")
	}
}

// TestConditionalGetRevalidates checks the round-trip every no-cache mutable
// key depends on: an unchanged object answers 304 with no body, a changed one
// answers 200 with the new bytes and the new ETag.
func TestConditionalGetRevalidates(t *testing.T) {
	newBucketRoot(t)
	putObject(t, "showcase/HEAD", "ref: refs/heads/main\n")
	etag := etagOf([]byte("ref: refs/heads/main\n"))

	unchanged := request(t, http.MethodGet, "/showcase/HEAD", nil, map[string]string{"If-None-Match": etag})
	if unchanged.Code != 304 {
		t.Fatalf("conditional GET of an unchanged object = %d, want 304", unchanged.Code)
	}
	if unchanged.Body.Len() != 0 {
		t.Errorf("304 carried a body: %q", unchanged.Body.String())
	}
	if got := unchanged.Header().Get("ETag"); got != etag {
		t.Errorf("304 ETag = %s, want %s", got, etag)
	}

	putObject(t, "showcase/HEAD", "ref: refs/heads/next\n")
	changed := request(t, http.MethodGet, "/showcase/HEAD", nil, map[string]string{"If-None-Match": etag})
	if changed.Code != 200 {
		t.Fatalf("conditional GET after a change = %d, want 200", changed.Code)
	}
	if changed.Body.String() != "ref: refs/heads/next\n" {
		t.Errorf("body = %q, want the new content", changed.Body.String())
	}
	if got, want := changed.Header().Get("ETag"), etagOf([]byte("ref: refs/heads/next\n")); got != want {
		t.Errorf("ETag = %s, want %s (the new content's)", got, want)
	}
}

// TestPutIfNoneMatchStarCreatesOnce checks create-only compare-and-swap: the
// first write of a key succeeds, a second is refused with 412 and leaves the
// stored bytes alone. Generation-mode ref chains (<ref>/.gen/<counter>) are
// built entirely out of this, and the pusher probes it before trusting a
// bucket at all.
func TestPutIfNoneMatchStarCreatesOnce(t *testing.T) {
	newBucketRoot(t)
	const key = "/showcase/refs/heads/main/.gen/1"
	create := request(t, http.MethodPut, key, []byte("first"), map[string]string{"If-None-Match": "*"})
	if create.Code != 200 {
		t.Fatalf("create = %d, want 200", create.Code)
	}
	second := request(t, http.MethodPut, key, []byte("second"), map[string]string{"If-None-Match": "*"})
	if second.Code != 412 {
		t.Fatalf("create over an existing key = %d, want 412", second.Code)
	}
	get := request(t, http.MethodGet, key, nil, nil)
	if get.Body.String() != "first" {
		t.Errorf("refused create still wrote: body = %q, want %q", get.Body.String(), "first")
	}
}

// TestPutIfMatchCompareAndSwap checks update compare-and-swap, the primitive
// under etag-mode ref updates: a write carrying the current ETag lands, one
// carrying a stale ETag is refused with 412 without touching the object, and
// one naming any ETag on an absent key is refused too.
func TestPutIfMatchCompareAndSwap(t *testing.T) {
	newBucketRoot(t)
	const key = "/showcase/refs/heads/main"
	stale := etagOf([]byte("old"))

	absent := request(t, http.MethodPut, key, []byte("new"), map[string]string{"If-Match": stale})
	if absent.Code != 412 {
		t.Fatalf("If-Match on an absent key = %d, want 412", absent.Code)
	}
	if get := request(t, http.MethodGet, key, nil, nil); get.Code != 404 {
		t.Errorf("refused If-Match created the key: GET = %d, want 404", get.Code)
	}

	putObject(t, "showcase/refs/heads/main", "old")
	ok := request(t, http.MethodPut, key, []byte("new"), map[string]string{"If-Match": stale})
	if ok.Code != 200 {
		t.Fatalf("If-Match with the current ETag = %d, want 200", ok.Code)
	}
	if got, want := ok.Header().Get("ETag"), etagOf([]byte("new")); got != want {
		t.Errorf("ETag after a swap = %s, want %s (the new content's)", got, want)
	}

	// A second writer that read the object before the swap now holds a stale
	// ETag: its write must lose, which is what makes a concurrent
	// non-fast-forward push fail instead of clobbering the winner.
	loser := request(t, http.MethodPut, key, []byte("clobber"), map[string]string{"If-Match": stale})
	if loser.Code != 412 {
		t.Fatalf("If-Match with a stale ETag = %d, want 412", loser.Code)
	}
	get := request(t, http.MethodGet, key, nil, nil)
	if get.Body.String() != "new" {
		t.Errorf("loser overwrote the winner: body = %q, want %q", get.Body.String(), "new")
	}
}

// TestPutContentEncodingSidecar checks a stored Content-Encoding is replayed on
// GET and HEAD (brotli site artifacts decode transparently) and that rewriting
// the key without one clears it, so an identity body is never served as brotli.
func TestPutContentEncodingSidecar(t *testing.T) {
	dir := newBucketRoot(t)
	const key = "/showcase/.gitsocial/site/items/pm/head.json"
	if res := request(t, http.MethodPut, key, []byte("compressed"), map[string]string{"Content-Encoding": "br"}); res.Code != 200 {
		t.Fatalf("PUT = %d, want 200", res.Code)
	}
	sidecar := filepath.Join(dir, "showcase/.gitsocial/site/items/pm/head.json"+encSuffix)
	if _, err := os.Stat(sidecar); err != nil {
		t.Fatalf("Content-Encoding sidecar not written: %v", err)
	}
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		if got := request(t, method, key, nil, nil).Header().Get("Content-Encoding"); got != "br" {
			t.Errorf("%s Content-Encoding = %q, want %q", method, got, "br")
		}
	}
	if res := request(t, http.MethodPut, key, []byte("plain"), nil); res.Code != 200 {
		t.Fatalf("rewrite = %d, want 200", res.Code)
	}
	if _, err := os.Stat(sidecar); !os.IsNotExist(err) {
		t.Errorf("sidecar survived an identity rewrite (err = %v), so the body would be served as brotli", err)
	}
	if got := request(t, http.MethodGet, key, nil, nil).Header().Get("Content-Encoding"); got != "" {
		t.Errorf("Content-Encoding after an identity rewrite = %q, want none", got)
	}
}

// TestDeleteRemovesObjectAndSidecar checks DELETE answers 204, the key reads as
// absent afterwards, the encoding sidecar goes with it, and deleting an absent
// key is a no-op rather than an error (as on real S3).
func TestDeleteRemovesObjectAndSidecar(t *testing.T) {
	dir := newBucketRoot(t)
	const key = "/showcase/.gitsocial/site/items/pm/head.json"
	if res := request(t, http.MethodPut, key, []byte("compressed"), map[string]string{"Content-Encoding": "br"}); res.Code != 200 {
		t.Fatalf("PUT = %d, want 200", res.Code)
	}
	if res := request(t, http.MethodDelete, key, nil, nil); res.Code != 204 {
		t.Fatalf("DELETE = %d, want 204", res.Code)
	}
	if res := request(t, http.MethodGet, key, nil, nil); res.Code != 404 {
		t.Errorf("GET after DELETE = %d, want 404", res.Code)
	}
	if _, err := os.Stat(filepath.Join(dir, "showcase/.gitsocial/site/items/pm/head.json"+encSuffix)); !os.IsNotExist(err) {
		t.Errorf("sidecar survived DELETE (err = %v)", err)
	}
	if res := request(t, http.MethodDelete, "/showcase/never-written", nil, nil); res.Code != 204 {
		t.Errorf("DELETE of an absent key = %d, want 204", res.Code)
	}
}

// TestUnsupportedMethodIs405 checks a verb outside the served subset is
// refused rather than silently treated as one of them.
func TestUnsupportedMethodIs405(t *testing.T) {
	newBucketRoot(t)
	for _, method := range []string{http.MethodPost, http.MethodPatch, http.MethodOptions} {
		if res := request(t, method, "/showcase/refs/heads/main", []byte("x"), nil); res.Code != 405 {
			t.Errorf("%s = %d, want 405", method, res.Code)
		}
	}
}

// TestCacheControlByKeyClass checks the headers actually served per key class
// against S3.md's cache policy table: immutable for keys that can never change
// once written, no-cache for every mutable key.
func TestCacheControlByKeyClass(t *testing.T) {
	newBucketRoot(t)
	const immutable = "public, max-age=31536000, immutable"
	cases := []struct {
		key  string
		want string
	}{
		{"showcase/objects/ab/" + strings.Repeat("c", 38), immutable},
		{"showcase/objects/pack/pack-abc123.pack", immutable},
		{"showcase/objects/pack/pack-abc123.idx", immutable},
		{"showcase/.gitsocial/site/bodies/social/shard-abc123.json", immutable},
		{"showcase/issues/2.html", immutable},
		{"showcase/sitemap-2.xml", immutable},
		{"showcase/artifacts/v1.2.0/gitsocial.tar.gz", immutable},
		{"showcase/refs/heads/main", "no-cache"},
		{"showcase/HEAD", "no-cache"},
		{"showcase/.gitsocial/ref-mode", "no-cache"},
		{"showcase/.gitsocial/packmap/ab.json", "no-cache"},
		{"showcase/objects/info/packs", "no-cache"},
		{"showcase/info/refs", "no-cache"},
		{"showcase/artifacts/latest.txt", "no-cache"},
		{"showcase/sitemap.xml", "no-cache"},
		{"showcase/index.html", "no-cache"},
		{"showcase/commits/2.html", "no-cache"},
	}
	for _, c := range cases {
		putObject(t, c.key, "x")
		if got := request(t, http.MethodGet, "/"+c.key, nil, nil).Header().Get("Cache-Control"); got != c.want {
			t.Errorf("GET %s Cache-Control = %q, want %q", c.key, got, c.want)
		}
	}
}

// TestRangeGetServesPartialContent checks the 206 path the browser uses to read
// one object out of a packfile: exact bytes, Content-Range and Content-Length,
// for a closed range, an open-ended one, the final byte, and an end past the
// object's size (clamped, as a real bucket clamps).
func TestRangeGetServesPartialContent(t *testing.T) {
	newBucketRoot(t)
	const body = "0123456789"
	putObject(t, "showcase/objects/pack/pack-abc123.pack", body)
	cases := []struct {
		header string
		want   string
		rang   string
	}{
		{"bytes=0-3", "0123", "bytes 0-3/10"},
		{"bytes=4-6", "456", "bytes 4-6/10"},
		{"bytes=7-", "789", "bytes 7-9/10"},
		{"bytes=9-9", "9", "bytes 9-9/10"},
		{"bytes=0-9", body, "bytes 0-9/10"},
		{"bytes=8-99", "89", "bytes 8-9/10"},
	}
	for _, c := range cases {
		res := request(t, http.MethodGet, "/showcase/objects/pack/pack-abc123.pack", nil, map[string]string{"Range": c.header})
		if res.Code != 206 {
			t.Errorf("GET %s = %d, want 206", c.header, res.Code)
			continue
		}
		if res.Body.String() != c.want {
			t.Errorf("GET %s body = %q, want %q", c.header, res.Body.String(), c.want)
		}
		if got := res.Header().Get("Content-Range"); got != c.rang {
			t.Errorf("GET %s Content-Range = %q, want %q", c.header, got, c.rang)
		}
		if got, want := res.Header().Get("Content-Length"), fmt.Sprintf("%d", len(c.want)); got != want {
			t.Errorf("GET %s Content-Length = %q, want %q", c.header, got, want)
		}
	}
}

// TestRangeGetFallsBackToWholeBody checks the ranges locals3 does not serve as
// 206 come back as a whole-body 200. Client.GetRange and the browser's
// fetchRange both slice a 200 locally, so this is a safe answer; note that a
// real bucket answers 416 for a start at or past the object's size, and
// supports the suffix form, so those two rows are divergences (see the QA
// report), not behavior to rely on.
func TestRangeGetFallsBackToWholeBody(t *testing.T) {
	newBucketRoot(t)
	const body = "0123456789"
	putObject(t, "showcase/objects/pack/pack-abc123.pack", body)
	for _, header := range []string{"bytes=10-", "bytes=99-", "bytes=5-3", "bytes=-5", "bytes=0-1,4-5", "items=0-1", "bytes=x-1", "bytes=5"} {
		res := request(t, http.MethodGet, "/showcase/objects/pack/pack-abc123.pack", nil, map[string]string{"Range": header})
		if res.Code != 200 {
			t.Errorf("GET %s = %d, want 200 (whole body)", header, res.Code)
			continue
		}
		if res.Body.String() != body {
			t.Errorf("GET %s body = %q, want the whole object", header, res.Body.String())
		}
		if got := res.Header().Get("Content-Range"); got != "" {
			t.Errorf("GET %s set Content-Range = %q on a 200", header, got)
		}
	}
}

// TestConditionalGetWinsOverRange checks a matching If-None-Match answers 304
// even when a Range is present, the precedence RFC 7233 requires (an unchanged
// object is never re-sent in pieces).
func TestConditionalGetWinsOverRange(t *testing.T) {
	newBucketRoot(t)
	const body = "0123456789"
	putObject(t, "showcase/objects/pack/pack-abc123.pack", body)
	res := request(t, http.MethodGet, "/showcase/objects/pack/pack-abc123.pack", nil, map[string]string{
		"If-None-Match": etagOf([]byte(body)),
		"Range":         "bytes=0-3",
	})
	if res.Code != 304 {
		t.Fatalf("conditional range GET = %d, want 304", res.Code)
	}
	if res.Body.Len() != 0 {
		t.Errorf("304 carried a body: %q", res.Body.String())
	}
}

// listBucketResult mirrors the ListObjectsV2 shape objstore's Client decodes,
// so the listing is asserted as parsed XML rather than as a substring.
type listBucketResult struct {
	XMLName     xml.Name `xml:"ListBucketResult"`
	IsTruncated bool     `xml:"IsTruncated"`
	Contents    []struct {
		Key  string `xml:"Key"`
		ETag string `xml:"ETag"`
	} `xml:"Contents"`
}

// listBucket issues a ListObjectsV2 request and decodes the response.
func listBucket(t *testing.T, target string) listBucketResult {
	t.Helper()
	res := request(t, http.MethodGet, target, nil, nil)
	if res.Code != 200 {
		t.Fatalf("list %s = %d, want 200", target, res.Code)
	}
	var parsed listBucketResult
	if err := xml.Unmarshal(res.Body.Bytes(), &parsed); err != nil {
		t.Fatalf("list %s: %v (body %q)", target, err, res.Body.String())
	}
	return parsed
}

// listedKeys returns the keys of a decoded listing, in the order listed.
func listedKeys(result listBucketResult) []string {
	keys := make([]string, 0, len(result.Contents))
	for _, c := range result.Contents {
		keys = append(keys, c.Key)
	}
	return keys
}

// TestListObjectsV2 checks the listing contract: bucket-relative keys in sorted
// order, scoped to one bucket, filtered by prefix, never truncated, with the
// encoding sidecars hidden.
func TestListObjectsV2(t *testing.T) {
	newBucketRoot(t)
	putObject(t, "showcase/refs/heads/main", "a")
	putObject(t, "showcase/refs/heads/feature", "b")
	putObject(t, "showcase/HEAD", "c")
	if res := request(t, http.MethodPut, "/showcase/.gitsocial/site/items/pm/head.json", []byte("d"), map[string]string{"Content-Encoding": "br"}); res.Code != 200 {
		t.Fatalf("PUT = %d, want 200", res.Code)
	}
	putObject(t, "other/refs/heads/main", "e")

	all := listBucket(t, "/showcase?list-type=2")
	if all.IsTruncated {
		t.Error("IsTruncated = true, want false (locals3 never paginates)")
	}
	keys := listedKeys(all)
	want := []string{".gitsocial/site/items/pm/head.json", "HEAD", "refs/heads/feature", "refs/heads/main"}
	if strings.Join(keys, ",") != strings.Join(want, ",") {
		t.Errorf("listed keys = %v, want %v (sorted, bucket-relative, no %s sidecars, no other bucket)", keys, want, encSuffix)
	}

	keys = listedKeys(listBucket(t, "/showcase?list-type=2&prefix=refs/heads/"))
	if strings.Join(keys, ",") != "refs/heads/feature,refs/heads/main" {
		t.Errorf("prefixed listing = %v, want only the two ref keys", keys)
	}

	if empty := listBucket(t, "/never-pushed?list-type=2"); len(empty.Contents) != 0 || empty.IsTruncated {
		t.Errorf("listing an unwritten bucket = %+v, want an empty, untruncated result", empty)
	}
}

// TestListObjectsV2ETagTracksValue checks each listed ETag is the object's
// content md5, and changes when the content does: the site push-state marker
// fingerprints a listing by (key, ETag), so an ETag that only tracked a key's
// presence would make it miss rewrites.
func TestListObjectsV2ETagTracksValue(t *testing.T) {
	newBucketRoot(t)
	putObject(t, "showcase/refs/heads/main", "first")
	before := listBucket(t, "/showcase?list-type=2")
	if len(before.Contents) != 1 {
		t.Fatalf("listed %d objects, want 1", len(before.Contents))
	}
	if got, want := before.Contents[0].ETag, etagOf([]byte("first")); got != want {
		t.Errorf("listed ETag = %s, want %s (the content md5)", got, want)
	}
	putObject(t, "showcase/refs/heads/main", "second")
	after := listBucket(t, "/showcase?list-type=2")
	if got, want := after.Contents[0].ETag, etagOf([]byte("second")); got != want {
		t.Errorf("listed ETag after a rewrite = %s, want %s", got, want)
	}
}

// TestListObjectsV2DoesNotEscapeKeys reproduces a DEFECT, not intended
// behavior: the listing writes each key straight into the XML, so a key holding
// a character XML reserves breaks the whole document. "&" is legal in a git ref
// name (git check-ref-format accepts refs/heads/foo&bar), and a real bucket
// escapes it, so a locally pushed repo carrying such a branch makes every
// ListObjectsV2 consumer (ref listing, push state, the thin-fork walk) fail to
// parse the entire listing, not just that one entry.
func TestListObjectsV2DoesNotEscapeKeys(t *testing.T) {
	newBucketRoot(t)
	putObject(t, "showcase/refs/heads/foo&bar", "a")
	res := request(t, http.MethodGet, "/showcase?list-type=2", nil, nil)
	if !strings.Contains(res.Body.String(), "<Key>refs/heads/foo&bar</Key>") {
		t.Fatalf("listing = %q, want the unescaped key this test documents", res.Body.String())
	}
	var parsed listBucketResult
	if err := xml.Unmarshal(res.Body.Bytes(), &parsed); err == nil {
		t.Errorf("listing parsed as XML; the escaping defect is fixed, so replace this test with a round-trip assertion")
	}
}

// TestTrailingSlashServesDirectoryIndex checks the browsable read surface: a
// trailing-slash GET or HEAD answers with that directory's index.html and its
// text/html type, while a ListObjectsV2 request on the same path still lists
// (the query guard keeps the S3 API and the website on one port).
func TestTrailingSlashServesDirectoryIndex(t *testing.T) {
	newBucketRoot(t)
	putObject(t, "showcase/issues/index.html", "<h1>issues</h1>")
	get := request(t, http.MethodGet, "/showcase/issues/", nil, nil)
	if get.Code != 200 {
		t.Fatalf("GET of a directory = %d, want 200", get.Code)
	}
	if get.Body.String() != "<h1>issues</h1>" {
		t.Errorf("directory body = %q, want the index.html", get.Body.String())
	}
	if got := get.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Errorf("directory Content-Type = %q, want %q", got, "text/html; charset=utf-8")
	}
	head := request(t, http.MethodHead, "/showcase/issues/", nil, nil)
	if head.Code != 200 || head.Body.Len() != 0 {
		t.Errorf("HEAD of a directory = %d with %d body bytes, want 200 and none", head.Code, head.Body.Len())
	}
	if listed := listBucket(t, "/showcase/?list-type=2"); len(listed.Contents) != 1 || listed.Contents[0].Key != "issues/index.html" {
		t.Errorf("list on a trailing-slash path = %+v, want the one stored key", listed.Contents)
	}
}

// TestHandleServesFileOutsideRootViaEncodedTraversal reproduces a DEFECT, not
// intended behavior: request keys are joined onto the bucket root without
// confinement (see TestDiskPathEscapesRootWithDotDot), and ServeMux only
// rewrites a literal "..", so a percent-encoded traversal reaches handle intact
// and reads a file outside -root. PUT and DELETE map keys the same way, so they
// write and delete outside it too. The server binds 127.0.0.1 and is
// development-only, which bounds the blast radius but does not close the hole.
// The test drives a raw request line, since an http.Client would normalize it.
func TestHandleServesFileOutsideRootViaEncodedTraversal(t *testing.T) {
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(secret, []byte("not bucket content"), 0o644); err != nil {
		t.Fatal(err)
	}
	bucketRoot := filepath.Join(outside, "root")
	if err := os.MkdirAll(bucketRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	withRoot(t, bucketRoot)

	// main() registers handle on a ServeMux, so the reproduction goes through
	// one too rather than calling handle directly.
	mux := http.NewServeMux()
	mux.HandleFunc("/", handle)
	server := httptest.NewServer(mux)
	defer server.Close()

	conn, err := net.Dial("tcp", strings.TrimPrefix(server.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := fmt.Fprint(conn, "GET /showcase/%2e%2e/%2e%2e/secret.txt HTTP/1.1\r\nHost: locals3\r\nConnection: close\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	res, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body := make([]byte, 64)
	n, _ := res.Body.Read(body)
	if res.StatusCode != 200 || string(body[:n]) != "not bucket content" {
		t.Errorf("encoded traversal = %d %q; a confined server answers 404 or 403, so the defect is fixed and this test should be replaced", res.StatusCode, body[:n])
	}
}

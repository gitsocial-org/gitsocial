// helpers_test.go - unit tests for locals3's pure helpers: ETag shape, disk
// path mapping, the Content-Encoding sidecar, the cache policy classifier, the
// Content-Type table, the digit/hex predicates and Range parsing.
//
// The cache policy expectations come from documentation/S3.md ("Cache policy"),
// which is the specification both dev servers (locals3 and sitetest/serve.js)
// and the uploaders (objstore/cache_control.go, site/site_cache_control.go) must agree on; the Range
// expectations come from RFC 7233 and from Client.GetRange's contract.
package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestEtagOf pins the S3 ETag shape: the lowercase hex md5 of the body, quoted.
func TestEtagOf(t *testing.T) {
	cases := []struct {
		body string
		want string
	}{
		{"", `"d41d8cd98f00b204e9800998ecf8427e"`},
		{"hello", `"5d41402abc4b2a76b9719d911017c592"`},
		{"ref: refs/heads/main\n", `"cf7dd3ce51958c5f13fece957cc417fb"`},
	}
	for _, c := range cases {
		if got := etagOf([]byte(c.body)); got != c.want {
			t.Errorf("etagOf(%q) = %s, want %s", c.body, got, c.want)
		}
	}
}

// TestDiskPathJoinsUnderRoot checks the ordinary mapping of a request key onto
// a file under the bucket root.
func TestDiskPathJoinsUnderRoot(t *testing.T) {
	withRoot(t, "/srv/buckets")
	cases := []struct {
		key  string
		want string
	}{
		{"showcase/refs/heads/main", filepath.Join("/srv/buckets", "showcase", "refs", "heads", "main")},
		{"showcase/objects/ab/cdef", filepath.Join("/srv/buckets", "showcase", "objects", "ab", "cdef")},
		{"showcase", filepath.Join("/srv/buckets", "showcase")},
	}
	for _, c := range cases {
		if got := diskPath(c.key); got != c.want {
			t.Errorf("diskPath(%q) = %q, want %q", c.key, got, c.want)
		}
	}
}

// TestWithinRoot covers the confinement guard. diskPath still resolves ".."
// (filepath.Join's behavior), so containment is decided separately: withinRoot
// is what keeps a traversing key from addressing a file outside the bucket
// root, and handle refuses one with a 403.
func TestWithinRoot(t *testing.T) {
	withRoot(t, "/srv/buckets")
	cases := []struct {
		key  string
		want bool
	}{
		{"showcase/refs/heads/main", true},
		{"showcase", true},
		{"showcase/a/../b", true},
		{"showcase/../showcase/x", true},
		{"showcase/../../etc/passwd", false},
		{"..", false},
		{"../sibling", false},
		{"showcase/../..", false},
	}
	for _, c := range cases {
		if got := withinRoot(diskPath(c.key)); got != c.want {
			t.Errorf("withinRoot(diskPath(%q)) = %v, want %v (resolved %q)", c.key, got, c.want, diskPath(c.key))
		}
	}
}

// TestReadEnc covers the Content-Encoding sidecar: present (trailing newline
// trimmed), absent, and empty.
func TestReadEnc(t *testing.T) {
	dir := t.TempDir()
	object := filepath.Join(dir, "shard.json")
	if err := os.WriteFile(object, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := readEnc(object); got != "" {
		t.Errorf("readEnc without a sidecar = %q, want empty", got)
	}
	if err := os.WriteFile(object+encSuffix, []byte("br\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := readEnc(object); got != "br" {
		t.Errorf("readEnc = %q, want %q (trailing newline trimmed)", got, "br")
	}
	if err := os.WriteFile(object+encSuffix, []byte("  \n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := readEnc(object); got != "" {
		t.Errorf("readEnc of a blank sidecar = %q, want empty", got)
	}
}

// TestCacheControlFor checks the classifier against the key classes S3.md's
// cache policy table names: only keys that can never change once written are
// immutable, every mutable key revalidates.
func TestCacheControlFor(t *testing.T) {
	const immutable = "public, max-age=31536000, immutable"
	const revalidate = "no-cache"
	cases := []struct {
		key  string
		want string
	}{
		// Loose objects: objects/<xx>/<38-hex>, at a path boundary.
		{"objects/ab/" + strings.Repeat("c", 38), immutable},
		{"repo/objects/AB/" + strings.Repeat("F", 38), immutable},
		{"objects/ab/" + strings.Repeat("c", 37), revalidate},
		{"objects/ab/" + strings.Repeat("z", 38), revalidate},
		{"objects/info/packs", revalidate},
		{"myobjects/ab/" + strings.Repeat("c", 38), revalidate},
		// Packfiles and their indexes.
		{"objects/pack/pack-abc123.pack", immutable},
		{"repo/objects/pack/pack-abc123.idx", immutable},
		{"objects/pack/tmp-abc123.pack", revalidate},
		{"objects/pack/pack-abc123.keep", revalidate},
		// Sealed site shards of either corpus.
		{"repo/.gitsocial/site/items/pm/shard-deadbeef.json", immutable},
		{"repo/.gitsocial/site/bodies/social/shard-deadbeef.json", immutable},
		{"repo/.gitsocial/site/items/pm/head.json", revalidate},
		{"repo/.gitsocial/site/items/pm/manifest.json", revalidate},
		// Sealed HTML list pages, one per page-type directory.
		{"repo/issues/2.html", immutable},
		{"repo/prs/2.html", immutable},
		{"repo/posts/2.html", immutable},
		{"repo/releases/2.html", immutable},
		{"repo/memos/2.html", immutable},
		{"issues/2.html", immutable},
		{"repo/issues/index.html", revalidate},
		{"repo/issue/2.html", revalidate},
		// commits/<n>.html is sealed the same way but stays mutable on purpose:
		// the default branch can be rebased, so those pages get re-derived
		// (site/site_cache_control.go, isSealedListPageKey).
		{"repo/commits/2.html", revalidate},
		// Sealed sitemap parts; the index and the head part stay mutable.
		{"repo/sitemap-2.xml", immutable},
		{"repo/sitemap.xml", revalidate},
		{"repo/sitemap-head.xml", revalidate},
		// Release artifact objects; the sibling latest.txt is mutable, and a ref
		// key for a branch named artifacts/… is a ref, not an artifact.
		{"repo/artifacts/v1.2.0/gitsocial-darwin-arm64.tar.gz", immutable},
		{"repo/artifacts/latest.txt", revalidate},
		{"repo/refs/heads/artifacts/v1/x", revalidate},
		// Ordinary mutable keys.
		{"repo/refs/heads/main", revalidate},
		{"repo/HEAD", revalidate},
		{"repo/.gitsocial/ref-mode", revalidate},
		{"repo/.gitsocial/pack-state.json", revalidate},
		{"repo/.gitsocial/packmap/ab.json", revalidate},
		{"repo/info/refs", revalidate},
		{"repo/index.html", revalidate},
		{"repo/robots.txt", revalidate},
		{"", revalidate},
	}
	for _, c := range cases {
		if got := cacheControlFor(c.key); got != c.want {
			t.Errorf("cacheControlFor(%q) = %q, want %q", c.key, got, c.want)
		}
	}
}

// TestContentTypeFor checks extension-derived types, including the ones
// browsers refuse to sniff (CSS, JS) and the octet-stream fallback for keys
// with no or an unknown extension.
func TestContentTypeFor(t *testing.T) {
	cases := []struct {
		key  string
		want string
	}{
		{"repo/index.html", "text/html; charset=utf-8"},
		{"repo/site/gs-core.js", "text/javascript; charset=utf-8"},
		{"repo/site/pages-core.css", "text/css; charset=utf-8"},
		{"repo/.gitsocial/site/items/pm/head.json", "application/json"},
		{"repo/sitemap.xml", "application/xml"},
		{"repo/robots.txt", "text/plain; charset=utf-8"},
		{"repo/README.md", "text/markdown; charset=utf-8"},
		{"repo/site/icons.svg", "image/svg+xml"},
		{"repo/site/font.woff2", "font/woff2"},
		{"repo/site/IMAGE.PNG", "image/png"},
		{"repo/refs/heads/main", "application/octet-stream"},
		{"repo/objects/pack/pack-abc.pack", "application/octet-stream"},
		{"repo/archive.tar.gz", "application/octet-stream"},
	}
	for _, c := range cases {
		if got := contentTypeFor(c.key); got != c.want {
			t.Errorf("contentTypeFor(%q) = %q, want %q", c.key, got, c.want)
		}
	}
}

// TestContentTypesMatchServeJS keeps locals3's table identical to the site
// battery's server (sitetest/serve.js): the two serve the same bucket, so a
// type only one of them knows would make a local run and a suite run disagree.
func TestContentTypesMatchServeJS(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("..", "..", "site", "sitetest", "serve.js"))
	if err != nil {
		t.Fatal(err)
	}
	block := regexp.MustCompile(`(?s)const TYPES = \{(.*?)\n\};`).FindSubmatch(source)
	if block == nil {
		t.Fatal("serve.js: TYPES table not found, update this test alongside it")
	}
	entries := regexp.MustCompile(`"(\.[a-z0-9]+)":\s*"([^"]+)"`).FindAllStringSubmatch(string(block[1]), -1)
	if len(entries) == 0 {
		t.Fatal("serve.js: TYPES table parsed empty")
	}
	fromJS := map[string]string{}
	for _, e := range entries {
		fromJS[e[1]] = e[2]
	}
	for ext, want := range fromJS {
		if got := contentTypes[ext]; got != want {
			t.Errorf("contentTypes[%q] = %q, serve.js has %q", ext, got, want)
		}
	}
	for ext, got := range contentTypes {
		if _, ok := fromJS[ext]; !ok {
			t.Errorf("contentTypes has %q = %q, serve.js has no entry", ext, got)
		}
	}
}

// TestIsDigits covers the decimal predicate used to spot sealed page and
// sitemap part numbers.
func TestIsDigits(t *testing.T) {
	cases := map[string]bool{"0": true, "12345": true, "": false, "1a": false, " 1": false, "1.2": false, "-1": false, "१२": false}
	for in, want := range cases {
		if got := isDigits(in); got != want {
			t.Errorf("isDigits(%q) = %v, want %v", in, got, want)
		}
	}
}

// TestIsHex covers the hex predicate used to spot loose object key shapes; it
// accepts both cases, as git shas and S3 keys may carry either.
func TestIsHex(t *testing.T) {
	cases := map[string]bool{"abc123": true, "ABCDEF": true, "0": true, "": false, "g": false, "ab cd": false, "0x1f": false}
	for in, want := range cases {
		if got := isHex(in); got != want {
			t.Errorf("isHex(%q) = %v, want %v", in, got, want)
		}
	}
}

// TestParseRange covers single-range parsing against RFC 7233 semantics: a
// satisfiable range is served as a 206, a range starting past the object is
// refused with a 416, and a malformed or unsupported header is ignored and the
// object served whole as a 200.
func TestParseRange(t *testing.T) {
	cases := []struct {
		name   string
		header string
		size   int
		start  int
		end    int
		status int
	}{
		{"closed range", "bytes=0-4", 10, 0, 5, 206},
		{"interior range", "bytes=3-6", 10, 3, 7, 206},
		{"single byte", "bytes=2-2", 10, 2, 3, 206},
		{"final byte", "bytes=9-9", 10, 9, 10, 206},
		{"open ended", "bytes=5-", 10, 5, 10, 206},
		{"open ended from zero", "bytes=0-", 10, 0, 10, 206},
		{"whole object", "bytes=0-9", 10, 0, 10, 206},
		{"end past size is clamped", "bytes=8-99", 10, 8, 10, 206},
		{"surrounding space tolerated", " bytes=1-2 ", 10, 1, 3, 206},
		{"suffix range", "bytes=-5", 10, 5, 10, 206},
		{"suffix of one byte", "bytes=-1", 10, 9, 10, 206},
		{"suffix past size is clamped", "bytes=-99", 10, 0, 10, 206},
		// Unsatisfiable: the range starts at or past the object's last byte.
		{"start at size", "bytes=10-", 10, 0, 0, 416},
		{"start past size", "bytes=99-", 10, 0, 0, 416},
		{"empty object", "bytes=0-", 0, 0, 0, 416},
		{"empty suffix", "bytes=-0", 10, 0, 0, 416},
		{"suffix of an empty object", "bytes=-5", 0, 0, 0, 416},
		// Malformed or unsupported: the header is ignored and the body served whole.
		{"start after end", "bytes=5-3", 10, 0, 0, 200},
		{"multi range", "bytes=0-1,4-5", 10, 0, 0, 200},
		{"non-bytes unit", "items=0-1", 10, 0, 0, 200},
		{"missing header", "", 10, 0, 0, 200},
		{"no dash", "bytes=5", 10, 0, 0, 200},
		{"non-numeric start", "bytes=a-4", 10, 0, 0, 200},
		{"non-numeric end", "bytes=0-z", 10, 0, 0, 200},
		{"overflowing start", "bytes=99999999999999999999-", 10, 0, 0, 200},
		{"negative start", "bytes=-1-5", 10, 0, 0, 200},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			start, end, status := parseRange(c.header, c.size)
			if status != c.status {
				t.Fatalf("parseRange(%q, %d) status = %d, want %d", c.header, c.size, status, c.status)
			}
			// The bounds only carry meaning for a 206; the caller ignores them
			// otherwise and serves the whole body or a 416.
			if status == 206 && (start != c.start || end != c.end) {
				t.Errorf("parseRange(%q, %d) = [%d, %d), want [%d, %d)", c.header, c.size, start, end, c.start, c.end)
			}
		})
	}
}

// TestNormalizeETag checks the spellings an If-Match compare has to see
// through: the quotes a client may drop and the weak marker a compressing CDN
// adds.
func TestNormalizeETag(t *testing.T) {
	const want = "5d41402abc4b2a76b9719d911017c592"
	for _, in := range []string{`"5d41402abc4b2a76b9719d911017c592"`, "5d41402abc4b2a76b9719d911017c592", `W/"5d41402abc4b2a76b9719d911017c592"`, ` "5d41402abc4b2a76b9719d911017c592" `} {
		if got := normalizeETag(in); got != want {
			t.Errorf("normalizeETag(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestListLimit checks the page size a listing honors: max-keys when it is
// usable, the S3 maximum otherwise.
func TestListLimit(t *testing.T) {
	cases := map[string]int{"": 1000, "2": 2, "1000": 1000, "1001": 1000, "0": 1000, "-3": 1000, "many": 1000}
	for raw, want := range cases {
		if got := listLimit(raw); got != want {
			t.Errorf("listLimit(%q) = %d, want %d", raw, got, want)
		}
	}
}

// TestContinuationTokenRoundTrip checks a token carries the key a listing
// resumes after, and that an undecodable one starts from the beginning.
func TestContinuationTokenRoundTrip(t *testing.T) {
	for _, key := range []string{"refs/heads/main", "refs/heads/foo&bar", ""} {
		if got := continuationKey(continuationToken(key)); got != key {
			t.Errorf("continuationKey(continuationToken(%q)) = %q", key, got)
		}
	}
	if got := continuationKey("not base64!!"); got != "" {
		t.Errorf("continuationKey of a malformed token = %q, want empty", got)
	}
}

// withRoot points the package-level bucket root at dir for one test and
// restores the previous value afterwards.
func withRoot(t *testing.T, dir string) {
	t.Helper()
	previous := root
	root = dir
	t.Cleanup(func() { root = previous })
}

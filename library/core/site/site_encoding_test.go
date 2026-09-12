// site_encoding_test.go - the stored-Content-Encoding boundary.
//
// A bucket never negotiates: what a push stores is served to every client
// whatever its Accept-Encoding said. So the boundary has to be exact. The
// browser shell is stored brotli, because a bare bucket with no compressing CDN
// in front of it is the baseline deployment and half a megabyte of raw JS per
// cold visit is the alternative. Git's dumb-HTTP transport surface is stored
// plain, because git's walker is not a browser: it sends no Accept-Encoding and
// inflates what it gets as zlib, so a br-encoded object would break `git clone`
// from the bucket.

package site

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"

	"github.com/gitsocial-org/gitsocial/library/core/objstore/membucket"

	"github.com/gitsocial-org/gitsocial/library/core/objstore"
)

// assertStoredBrotli checks a key was stored brotli-encoded, smaller than raw,
// and decodes back to exactly the raw bytes.
func assertStoredBrotli(t *testing.T, client *objstore.Client, bucket *membucket.Bucket, key string, raw []byte) {
	t.Helper()
	if enc := bucket.EncOf(key); enc != "br" {
		t.Errorf("%s: stored Content-Encoding = %q, want %q", key, enc, "br")
		return
	}
	stored, err := client.Get(key)
	if err != nil {
		t.Fatalf("get %s: %v", key, err)
	}
	if len(stored) >= len(raw) {
		t.Errorf("%s: stored %d bytes, raw %d — compression bought nothing", key, len(stored), len(raw))
	}
	decoded, err := objstore.BrotliDecompress(stored)
	if err != nil {
		t.Fatalf("%s: decode: %v", key, err)
	}
	if string(decoded) != string(raw) {
		t.Errorf("%s: decoded bytes differ from the raw asset", key)
	}
}

// TestShellAssetsStoredBrotli: every embedded shell asset (the two
// stylesheets included) lands with `Content-Encoding: br` and round-trips to
// its raw bytes. The push-state version marker beside them stays plain and
// keeps naming the RAW content, so a compression change can never masquerade
// as a content change (nor mask one).
func TestShellAssetsStoredBrotli(t *testing.T) {
	client, bucket := testClient(t)
	if err := uploadSiteFiles(client, "repo/"); err != nil {
		t.Fatalf("uploadSiteFiles: %v", err)
	}
	names, err := siteFileNames()
	if err != nil {
		t.Fatalf("siteFileNames: %v", err)
	}
	if len(names) == 0 {
		t.Fatal("no embedded site files: the shell assertion would be vacuous")
	}
	for _, name := range names {
		raw, err := siteFiles.ReadFile("assets/" + name)
		if err != nil {
			t.Fatalf("read embedded %s: %v", name, err)
		}
		key := "repo/" + name
		if !siteCompressible(name) {
			if enc := bucket.EncOf(key); enc != "" {
				t.Errorf("%s: non-text asset stored with Content-Encoding %q", key, enc)
			}
			continue
		}
		assertStoredBrotli(t, client, bucket, key, raw)
	}
	markerKey := "repo/" + siteVersionKey
	if enc := bucket.EncOf(markerKey); enc != "" {
		t.Errorf("%s: version marker stored with Content-Encoding %q, want none", markerKey, enc)
	}
	h := sha256.New()
	for _, name := range names {
		raw, err := siteFiles.ReadFile("assets/" + name)
		if err != nil {
			t.Fatalf("read embedded %s: %v", name, err)
		}
		fmt.Fprintf(h, "%s %d\n", name, len(raw))
		h.Write(raw)
	}
	wantVersion := fmt.Sprintf("%x", h.Sum(nil))
	if got := strings.TrimSpace(getKey(t, client, markerKey)); got != wantVersion {
		t.Errorf("site version = %s, want the hash of the RAW assets %s", got, wantVersion)
	}
}

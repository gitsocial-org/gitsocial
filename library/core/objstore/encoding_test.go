// encoding_test.go - the stored-Content-Encoding boundary on the transport side.

package objstore

import (
	"strings"
	"testing"
)

// TestTransportKeysNeverEncoded: a bucket never negotiates, and git's walker
// sends no Accept-Encoding, so on a bucket that also carries a brotli asset
// nothing the dumb walker reads carries a Content-Encoding.
func TestTransportKeysNeverEncoded(t *testing.T) {
	dir := gitInfoRefsRepo(t)
	client, bucket := testClient(t)
	refs := uploadRepoObjectsAndRefs(t, client, "repo/", dir)
	produce, _ := feedObjects(4)
	if err := uploadEncodedObjects(client, "repo/", 2, 4, nil, produce); err != nil {
		t.Fatalf("uploadEncodedObjects: %v", err)
	}
	if err := writeDumbTransportInfo(client, "repo/", nil, refs, false); err != nil {
		t.Fatalf("writeDumbTransportInfo: %v", err)
	}
	// A browser-facing asset on the same bucket, stored the way the site layer stores one.
	compressed, err := BrotliCompress([]byte(strings.Repeat("gs-core stand-in\n", 200)), BrotliQualityShard)
	if err != nil {
		t.Fatalf("BrotliCompress: %v", err)
	}
	if err := client.PutWithHeaders("repo/gs-core.js", compressed, map[string]string{"Content-Type": "text/javascript; charset=utf-8", "Content-Encoding": "br"}); err != nil {
		t.Fatalf("upload asset: %v", err)
	}
	keys, err := client.List("repo/")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	transport := 0
	for _, key := range keys {
		rel := strings.TrimPrefix(key, "repo/")
		if !strings.HasPrefix(rel, "objects/") && !strings.HasPrefix(rel, "refs/") &&
			rel != "HEAD" && rel != infoRefsKey && rel != packsKey {
			continue
		}
		transport++
		if enc := bucket.EncOf(key); enc != "" {
			t.Errorf("%s: transport key stored with Content-Encoding %q — git's dumb walker cannot decode it", key, enc)
		}
	}
	// The asset landed on the same bucket, so the two classes really are being
	// distinguished rather than nothing having been written at all.
	if transport == 0 {
		t.Fatal("no transport keys written: the assertion would be vacuous")
	}
	if enc := bucket.EncOf("repo/gs-core.js"); enc != "br" {
		t.Fatalf("browser asset on the same bucket = %q, want br", enc)
	}
}

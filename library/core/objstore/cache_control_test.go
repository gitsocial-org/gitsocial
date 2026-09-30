// cache_control_test.go - Tests for the per-key cache policy classifier.
package objstore

import "testing"

// TestCacheControlForKey_shellRevision: a versioned shell directory is immutable, the root shell names are not.
func TestCacheControlForKey_shellRevision(t *testing.T) {
	for key, want := range map[string]string{
		"repo/.gitsocial/site/shell/d829ec79861d/gs-app.js":     CacheControlImmutable,
		"repo/.gitsocial/site/shell/d829ec79861d/fonts/a.woff2": CacheControlImmutable,
		".gitsocial/site/shell/d829ec79861d/pages-full.css":     CacheControlImmutable,
		"repo/gs-app.js":  cacheControlRevalidate,
		"repo/index.html": cacheControlRevalidate,
		"repo/.gitsocial/site/shell/notahexdir12/gs-app.js":  cacheControlRevalidate,
		"repo/.gitsocial/site/shell/d829ec79861d":            cacheControlRevalidate,
		"repo/x.gitsocial/site/shell/d829ec79861d/gs-app.js": cacheControlRevalidate,
	} {
		if got := cacheControlForKey(key); got != want {
			t.Errorf("cacheControlForKey(%q) = %q, want %q", key, got, want)
		}
	}
}

// cache_control.go - HTTP cache policy for uploaded bucket objects.
//
// Loose git objects are content-addressed by sha, so a written object can never
// change: browsers may cache it forever without revalidating. Everything else
// (ref keys, HEAD, the ref-mode marker, the site shell, and its index
// artifacts) changes on push, so it is stored cacheable but with no-cache
// ("store, but always revalidate"): a conditional GET yields 304 when unchanged
// and never a stale body. This keeps the reader's ref-tip freshness check
// correct (ref keys are never served stale) while letting immutable loose
// objects skip the network entirely on reload.
package objstore

import "strings"

const (
	// cacheControlImmutable marks content-addressed loose objects.
	cacheControlImmutable = "public, max-age=31536000, immutable"
	// cacheControlRevalidate marks every mutable key: cache but always revalidate.
	cacheControlRevalidate = "no-cache"
)

// cacheControlForKey classifies a bucket key by mutability and returns the
// Cache-Control value it must be stored (and served) with.
func cacheControlForKey(key string) string {
	if isLooseObjectKey(key) || isSealedShardKey(key) {
		return cacheControlImmutable
	}
	return cacheControlRevalidate
}

// isSealedShardKey reports whether a key is a sealed shard of either corpus
// (`.gitsocial/site/{bodies,items}/<ext>/shard-<hash>.json`), which is
// content-hashed and written exactly once — the `shard-` basename prefix
// distinguishes it from the sibling no-cache head.json and manifest.json.
func isSealedShardKey(key string) bool {
	if !strings.Contains(key, siteBodiesKeyPrefix) && !strings.Contains(key, siteItemsKeyPrefix) {
		return false
	}
	slash := strings.LastIndex(key, "/")
	file := key[slash+1:]
	return strings.HasPrefix(file, "shard-") && strings.HasSuffix(file, ".json")
}

// isLooseObjectKey reports whether a key is a content-addressed loose object
// (`objects/<xx>/<38-hex>`), matched at a path boundary so a ref or state key
// that merely contains "objects" elsewhere is never misclassified.
func isLooseObjectKey(key string) bool {
	i := strings.Index(key, "objects/")
	if i < 0 || (i > 0 && key[i-1] != '/') {
		return false
	}
	xx, rest, ok := strings.Cut(key[i+len("objects/"):], "/")
	return ok && len(xx) == 2 && len(rest) == 38 && isHexString(xx) && isHexString(rest)
}

// isHexString reports whether s is non-empty and all hex digits.
func isHexString(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}
	return true
}

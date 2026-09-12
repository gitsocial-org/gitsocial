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
	// CacheControlImmutable marks a key whose bytes are sealed, for the writer that seals one.
	CacheControlImmutable = "public, max-age=31536000, immutable"
	// CacheControlRevalidate marks every mutable key: cache but always revalidate.
	CacheControlRevalidate = "no-cache"
)

// cacheControlForKey classifies a transport key by mutability and returns the
// Cache-Control value it must be stored (and served) with. A writer that seals
// a key of its own stamps CacheControlImmutable on the upload instead.
func cacheControlForKey(key string) string {
	if isLooseObjectKey(key) || isPackKey(key) || isArtifactVersionKey(key) {
		return CacheControlImmutable
	}
	return CacheControlRevalidate
}

// isPackKey reports whether a key is a packfile or its index
// (`objects/pack/pack-<hash>.{pack,idx}`), matched at a path boundary. Packs are
// named after their content and never rewritten, so they cache like loose
// objects; the sibling `objects/info/packs` listing stays mutable.
func isPackKey(key string) bool {
	i := strings.Index(key, packKeyPrefix)
	if i < 0 || (i > 0 && key[i-1] != '/') {
		return false
	}
	name := key[i+len(packKeyPrefix):]
	return strings.HasPrefix(name, "pack-") && (strings.HasSuffix(name, ".pack") || strings.HasSuffix(name, ".idx")) && !strings.Contains(name, "/")
}

// isArtifactVersionKey reports whether a key is a release artifact object
// (`artifacts/<version>/<file>`) at a path boundary — a version's artifacts are
// written once (a re-push of the same version re-uploads identical bytes), so
// they cache as immutable. The sibling `artifacts/latest.txt` sits directly
// under artifacts/ (no version directory) and stays no-cache, as does any ref
// key for a branch that happens to be named artifacts/… (`refs/` precedes it).
func isArtifactVersionKey(key string) bool {
	i := strings.Index(key, ArtifactsPrefix)
	if i < 0 || (i > 0 && key[i-1] != '/') {
		return false
	}
	if j := strings.Index(key, "refs/"); j >= 0 && j < i {
		return false
	}
	return strings.Contains(key[i+len(ArtifactsPrefix):], "/")
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

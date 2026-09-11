// site_cache_control.go - cache classification of the keys the site layer writes.
package objstore

import "strings"

// isSealedSitemapPartKey reports whether a key is a sealed (full, immutable)
// sitemap part — `sitemap-<n>.xml` at a path boundary. The index (sitemap.xml)
// and the mutable newest part (sitemap-head.xml) stay no-cache.
func isSealedSitemapPartKey(key string) bool {
	name := key[strings.LastIndex(key, "/")+1:]
	rest, ok := strings.CutPrefix(name, "sitemap-")
	if !ok {
		return false
	}
	n, ok := strings.CutSuffix(rest, ".xml")
	return ok && isDigitString(n)
}

// isSealedListPageKey reports whether a key is a sealed (immutable) HTML list
// page — `<type>/<n>.html` under one of the five page type directories at a
// path boundary. Everything else the page layer writes (item pages, the
// mutable index.html type-list heads, the generated front page index.html)
// stays no-cache: those keys rewrite in place on later pushes.
//
// `commits/<n>.html` is deliberately NOT in this class even though it is sealed
// the same way. A gitmsg data branch is append-only by protocol, so a sealed
// item list can never stop being true; the default branch can be rebased or
// force-pushed, and the commits layer's ancestry guard exists precisely to
// re-derive the pages when it is. A year-long immutable copy in a visitor's
// browser would outlive that repair, so those pages revalidate.
//
// Neither is a file page under `f/`, whose key mirrors a repo path and can
// therefore end in `<type dir>/<n>.html` by coincidence: a document is mutable
// and is never sealed.
func isSealedListPageKey(key string) bool {
	if isSiteFilePageKey(key) {
		return false
	}
	slash := strings.LastIndex(key, "/")
	if slash < 0 {
		return false
	}
	name, ok := strings.CutSuffix(key[slash+1:], ".html")
	if !ok || !isDigitString(name) {
		return false
	}
	dir := key[:slash]
	for _, l := range sitePageLists {
		if dir == l.Dir || strings.HasSuffix(dir, "/"+l.Dir) {
			return true
		}
	}
	return false
}

// isSiteFilePageKey reports whether a key is a file page (`f/<repo path>.html`),
// matched at a path boundary so a repo directory merely named f/ elsewhere is
// never misread.
func isSiteFilePageKey(key string) bool {
	i := strings.Index(key, sitePagesFilesDir+"/")
	return i >= 0 && (i == 0 || key[i-1] == '/')
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

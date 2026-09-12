// main.go - locals3, a disk-backed local S3 server for development and the site-test fixture builder
//
// It serves the subset of the S3 API the helper needs, plus the pushed site
// browsably, so one port is the whole local provider. Stdlib only, with no repo
// deps, so it stays standalone.
package main

import (
	"crypto/md5"
	"encoding/xml"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
)

var (
	root string
	mu   sync.Mutex
)

// etagOf returns the quoted md5 hex of the given bytes, matching S3 ETag shape.
func etagOf(b []byte) string { return fmt.Sprintf("%q", fmt.Sprintf("%x", md5.Sum(b))) }

// diskPath maps a request key ("<bucket>/<key>") to an absolute file path.
func diskPath(key string) string { return filepath.Join(root, filepath.FromSlash(key)) }

// withinRoot reports whether a path resolved from a request key stays under the served root; a percent-encoded traversal reaches the handler already decoded.
func withinRoot(path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// encSuffix names the sidecar file recording an object's Content-Encoding; no git ref or object key ends in it.
const encSuffix = ".gsenc"

// readEnc returns the stored Content-Encoding for a disk path ("" when none).
func readEnc(path string) string {
	b, err := os.ReadFile(path + encSuffix)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// cacheControlFor classifies a request key the way a real bucket is stamped on upload, derived from the key so locals3 keeps no per-object metadata. See documentation/S3.md.
func cacheControlFor(key string) string {
	i := strings.Index(key, "objects/")
	if i >= 0 && (i == 0 || key[i-1] == '/') {
		tail := key[i+len("objects/"):]
		if slash := strings.IndexByte(tail, '/'); slash == 2 && isHex(tail[:2]) && len(tail) == 41 && isHex(tail[3:]) {
			return "public, max-age=31536000, immutable"
		}
	}
	file := key[strings.LastIndexByte(key, '/')+1:]
	if j := strings.Index(key, "objects/pack/"); j >= 0 && (j == 0 || key[j-1] == '/') {
		if strings.HasPrefix(file, "pack-") && (strings.HasSuffix(file, ".pack") || strings.HasSuffix(file, ".idx")) {
			return "public, max-age=31536000, immutable"
		}
	}
	if strings.Contains(key, ".gitsocial/site/bodies/") || strings.Contains(key, ".gitsocial/site/items/") {
		if strings.HasPrefix(file, "shard-") && strings.HasSuffix(file, ".json") {
			return "public, max-age=31536000, immutable"
		}
	}
	if isDigits(strings.TrimSuffix(file, ".html")) && strings.HasSuffix(file, ".html") {
		dir := key[:strings.LastIndexByte(key, '/')+1]
		for _, d := range []string{"issues/", "prs/", "posts/", "releases/", "memos/"} {
			if strings.HasSuffix(dir, "/"+d) || dir == d {
				return "public, max-age=31536000, immutable"
			}
		}
	}
	if rest, ok := strings.CutPrefix(file, "sitemap-"); ok {
		if n, ok := strings.CutSuffix(rest, ".xml"); ok && isDigits(n) {
			return "public, max-age=31536000, immutable"
		}
	}
	if j := strings.Index(key, "artifacts/"); j >= 0 && (j == 0 || key[j-1] == '/') {
		if r := strings.Index(key, "refs/"); (r < 0 || r > j) && strings.Contains(key[j+len("artifacts/"):], "/") {
			return "public, max-age=31536000, immutable"
		}
	}
	return "no-cache"
}

// contentTypes maps served file extensions to Content-Type, mirroring sitetest/serve.js; Go's own sniffing calls CSS and JS text/plain, which a browser refuses.
var contentTypes = map[string]string{
	".html":  "text/html; charset=utf-8",
	".js":    "text/javascript; charset=utf-8",
	".json":  "application/json",
	".css":   "text/css; charset=utf-8",
	".xml":   "application/xml",
	".txt":   "text/plain; charset=utf-8",
	".md":    "text/markdown; charset=utf-8",
	".png":   "image/png",
	".gif":   "image/gif",
	".jpg":   "image/jpeg",
	".svg":   "image/svg+xml",
	".woff2": "font/woff2",
}

// contentTypeFor returns the Content-Type for a key, octet-stream when the extension is unknown.
func contentTypeFor(key string) string {
	if t, ok := contentTypes[strings.ToLower(filepath.Ext(key))]; ok {
		return t
	}
	return "application/octet-stream"
}

// isDigits reports whether s is non-empty and all decimal digits.
func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// parseRange parses a single byte-range header into a half-open range; ok is false for an absent, multi-range or unsatisfiable one, which is served whole.
func parseRange(header string, size int) (start, end int, ok bool) {
	spec, found := strings.CutPrefix(strings.TrimSpace(header), "bytes=")
	if !found || strings.Contains(spec, ",") {
		return 0, 0, false
	}
	from, to, found := strings.Cut(spec, "-")
	if !found || from == "" {
		return 0, 0, false // suffix ranges ("-500") are not used by the reader
	}
	start, err := strconv.Atoi(from)
	if err != nil || start >= size {
		return 0, 0, false
	}
	end = size
	if to != "" {
		last, err := strconv.Atoi(to)
		if err != nil {
			return 0, 0, false
		}
		if end = last + 1; end > size {
			end = size
		}
	}
	return start, end, end > start
}

// isHex reports whether s is non-empty and all hex digits.
func isHex(s string) bool {
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

// handle implements the GET/PUT/DELETE + ListObjectsV2 surface under a lock.
func handle(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimPrefix(r.URL.Path, "/")
	// A trailing-slash read answers with index.html, so the pushed site is browsable off this same port.
	if (r.Method == http.MethodGet || r.Method == http.MethodHead) && strings.HasSuffix(key, "/") && r.URL.RawQuery == "" {
		key += "index.html"
	}
	// Refuse a key that escapes the bucket root, on writes and deletes as much as reads.
	if !withinRoot(diskPath(key)) {
		w.WriteHeader(403)
		return
	}
	mu.Lock()
	defer mu.Unlock()
	switch r.Method {
	case http.MethodGet:
		if r.URL.Query().Get("list-type") == "2" {
			bucket := strings.SplitN(key, "/", 2)[0]
			prefix := r.URL.Query().Get("prefix")
			base := filepath.Join(root, bucket)
			var keys []string
			// A walk error surfaces per entry and a missing base lists empty, as an empty bucket does.
			_ = filepath.Walk(base, func(p string, info os.FileInfo, err error) error {
				if err != nil || info.IsDir() {
					return nil
				}
				rel, rerr := filepath.Rel(base, p)
				if rerr != nil {
					return nil
				}
				rel = filepath.ToSlash(rel)
				if strings.HasSuffix(rel, encSuffix) {
					return nil
				}
				if strings.HasPrefix(rel, prefix) {
					keys = append(keys, rel)
				}
				return nil
			})
			sort.Strings(keys)
			fmt.Fprint(w, `<?xml version="1.0"?><ListBucketResult><IsTruncated>false</IsTruncated>`)
			for _, k := range keys {
				// Emit the content md5 as the ETag: a caller that fingerprints a listing needs it to track an object's value, not its key's presence.
				etag := ""
				if body, err := os.ReadFile(filepath.Join(base, filepath.FromSlash(k))); err == nil {
					etag = etagOf(body)
				}
				// Escape the key, since "&" is legal in a ref name and would make the whole document unparseable.
				fmt.Fprint(w, "<Contents><Key>")
				_ = xml.EscapeText(w, []byte(k))
				fmt.Fprintf(w, "</Key><ETag>%s</ETag></Contents>", etag)
			}
			fmt.Fprint(w, `</ListBucketResult>`)
			return
		}
		path := diskPath(key)
		body, err := os.ReadFile(path)
		if err != nil {
			w.WriteHeader(404)
			return
		}
		etag := etagOf(body)
		w.Header().Set("ETag", etag)
		w.Header().Set("Cache-Control", cacheControlFor(key))
		w.Header().Set("Content-Type", contentTypeFor(key))
		if enc := readEnc(path); enc != "" {
			w.Header().Set("Content-Encoding", enc)
		}
		// A conditional GET revalidates an unchanged object to 304, the cheap round trip the no-cache keys rely on.
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(304)
			return
		}
		// The browser reads a packed object as one byte range, so a bucket must answer 206 with Content-Range.
		if start, end, ok := parseRange(r.Header.Get("Range"), len(body)); ok {
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end-1, len(body)))
			w.Header().Set("Content-Length", fmt.Sprintf("%d", end-start))
			w.WriteHeader(206)
			_, _ = w.Write(body[start:end])
			return
		}
		_, _ = w.Write(body)
	case http.MethodHead:
		path := diskPath(key)
		body, err := os.ReadFile(path)
		if err != nil {
			w.WriteHeader(404)
			return
		}
		w.Header().Set("ETag", etagOf(body))
		w.Header().Set("Cache-Control", cacheControlFor(key))
		w.Header().Set("Content-Type", contentTypeFor(key))
		// Content-Length lets the pusher's skip-existing check read a sealed shard's size from a HEAD alone.
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(body)))
		if enc := readEnc(path); enc != "" {
			w.Header().Set("Content-Encoding", enc)
		}
		w.WriteHeader(200)
	case http.MethodPut:
		body, _ := io.ReadAll(r.Body)
		path := diskPath(key)
		existing, err := os.ReadFile(path)
		exists := err == nil
		if r.Header.Get("If-None-Match") == "*" && exists {
			w.WriteHeader(412)
			return
		}
		if match := r.Header.Get("If-Match"); match != "" && (!exists || etagOf(existing) != match) {
			w.WriteHeader(412)
			return
		}
		if mkErr := os.MkdirAll(filepath.Dir(path), 0o755); mkErr != nil {
			w.WriteHeader(500)
			return
		}
		if wErr := os.WriteFile(path, body, 0o644); wErr != nil {
			w.WriteHeader(500)
			return
		}
		if enc := r.Header.Get("Content-Encoding"); enc != "" {
			_ = os.WriteFile(path+encSuffix, []byte(enc), 0o644)
		} else {
			os.Remove(path + encSuffix)
		}
		w.Header().Set("ETag", etagOf(body))
		w.WriteHeader(200)
	case http.MethodDelete:
		path := diskPath(key)
		os.Remove(path)
		os.Remove(path + encSuffix)
		w.WriteHeader(204)
	default:
		w.WriteHeader(405)
	}
}

// main binds the listener (ephemeral by default) and serves until killed.
func main() {
	// 9000 is the S3-ecosystem convention, and gives a stable port for a persisted loopback remote URL.
	addr := flag.String("addr", "127.0.0.1:9000", "listen address")
	flag.StringVar(&root, "root", "", "bucket root directory")
	flag.Parse()
	if root == "" {
		fmt.Fprintln(os.Stderr, "missing -root")
		os.Exit(1)
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	http.HandleFunc("/", handle)
	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("listening %s root=%s\n", ln.Addr().String(), root)
	if err := http.Serve(ln, nil); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// main.go - locals3, a disk-backed local S3 server for development and the
// site-test fixture builder.
//
// Serves the subset of the S3 API the git remote helper needs: GET/PUT/DELETE
// with If-Match / If-None-Match conditional writes plus ListObjectsV2. Keys are
// stored as files under <root>/<key>; path-style requests carry the bucket name
// as the first path segment, so each first path segment = bucket = directory
// under -root (bucket "showcase" lands at <root>/showcase/). ETags are the md5
// of the on-disk content. Standalone: stdlib only, no repo deps, so it compiles
// under `go build ./...` without affecting anything else.
package main

import (
	"crypto/md5"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
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

// encSuffix names the sidecar file that records an object's Content-Encoding
// (git refs and object keys never end in it, so a walk can skip it cleanly).
const encSuffix = ".gsenc"

// readEnc returns the stored Content-Encoding for a disk path ("" when none).
func readEnc(path string) string {
	b, err := os.ReadFile(path + encSuffix)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// cacheControlFor classifies a request key the way real buckets are stamped on
// upload (see objstore/cache_control.go): content-addressed loose objects
// (`objects/<xx>/<38-hex>`) and sealed shards of either corpus
// (`.gitsocial/site/{bodies,items}/<ext>/shard-<hash>.json`, content-hashed and
// written once) are immutable, everything else revalidates. Derived from the key
// rather than persisted, since both are pattern-identifiable and locals3 stays
// dependency-free.
func cacheControlFor(key string) string {
	i := strings.Index(key, "objects/")
	if i >= 0 && (i == 0 || key[i-1] == '/') {
		tail := key[i+len("objects/"):]
		if slash := strings.IndexByte(tail, '/'); slash == 2 && isHex(tail[:2]) && len(tail) == 41 && isHex(tail[3:]) {
			return "public, max-age=31536000, immutable"
		}
	}
	if strings.Contains(key, ".gitsocial/site/bodies/") || strings.Contains(key, ".gitsocial/site/items/") {
		file := key[strings.LastIndexByte(key, '/')+1:]
		if strings.HasPrefix(file, "shard-") && strings.HasSuffix(file, ".json") {
			return "public, max-age=31536000, immutable"
		}
	}
	return "no-cache"
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
	mu.Lock()
	defer mu.Unlock()
	switch r.Method {
	case http.MethodGet:
		if r.URL.Query().Get("list-type") == "2" {
			bucket := strings.SplitN(key, "/", 2)[0]
			prefix := r.URL.Query().Get("prefix")
			base := filepath.Join(root, bucket)
			var keys []string
			// Walk errors surface as per-entry err (skipped below); a missing base
			// yields an empty listing, matching an empty bucket.
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
				// Emit the content md5 as the ETag, matching real S3 (whose listings
				// carry per-object ETags). Callers that fingerprint a listing by
				// (key, ETag) — e.g. the site push-state marker — depend on the ETag
				// tracking an object's VALUE, not just its key's presence.
				etag := ""
				if body, err := os.ReadFile(filepath.Join(base, filepath.FromSlash(k))); err == nil {
					etag = etagOf(body)
				}
				fmt.Fprintf(w, "<Contents><Key>%s</Key><ETag>%s</ETag></Contents>", k, etag)
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
		if enc := readEnc(path); enc != "" {
			w.Header().Set("Content-Encoding", enc)
		}
		// Conditional GET: an unchanged object revalidates to 304 (no body), the
		// cheap round-trip the reader's no-cache mutable keys rely on.
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(304)
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
		// Content-Length lets the pusher's skip-existing check read a sealed
		// shard's stored size from a HEAD alone (real buckets set it too).
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
	addr := flag.String("addr", "127.0.0.1:0", "listen address")
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

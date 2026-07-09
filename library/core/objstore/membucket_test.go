// membucket_test.go - an in-process, in-memory S3 stub for the objstore tests.
//
// Implements the GET/HEAD/PUT/DELETE + ListObjectsV2 surface the Client uses
// (path-style: the first path segment is the bucket), including If-None-Match /
// If-Match conditional writes, a stored Content-Encoding replayed on read, and a
// per-key PUT counter so skip-existing behavior is directly assertable. Mirrors
// locals3/main.go's semantics but stays in-process (no port, no disk) and
// ignores SigV4 (the Client signs, the stub does not verify — the tests exercise
// artifact logic, not auth).

package objstore

import (
	"crypto/md5"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// memObject is one stored object: its bytes and Content-Encoding.
type memObject struct {
	body []byte
	enc  string
}

// memBucket is a threadsafe in-memory object store implementing http.Handler.
type memBucket struct {
	mu   sync.Mutex
	objs map[string]memObject
	puts map[string]int
}

// newMemBucket returns an empty in-memory bucket.
func newMemBucket() *memBucket {
	return &memBucket{objs: map[string]memObject{}, puts: map[string]int{}}
}

// putCount returns how many times a key (bucket-relative) was PUT.
func (m *memBucket) putCount(key string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.puts[key]
}

// etag returns the quoted md5 hex of bytes, matching S3 ETag shape.
func etag(b []byte) string { return fmt.Sprintf("%q", fmt.Sprintf("%x", md5.Sum(b))) }

// keyOf strips the leading "/<bucket>/" so stored keys are bucket-relative.
func keyOf(path string) string {
	p := strings.TrimPrefix(path, "/")
	if i := strings.IndexByte(p, '/'); i >= 0 {
		return p[i+1:]
	}
	return ""
}

// ServeHTTP dispatches the S3 subset under a single lock.
func (m *memBucket) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	key := keyOf(r.URL.Path)
	m.mu.Lock()
	defer m.mu.Unlock()
	switch r.Method {
	case http.MethodGet:
		if r.URL.Query().Get("list-type") == "2" {
			m.list(w, r)
			return
		}
		obj, ok := m.objs[key]
		if !ok {
			w.WriteHeader(404)
			return
		}
		tag := etag(obj.body)
		w.Header().Set("ETag", tag)
		if obj.enc != "" {
			w.Header().Set("Content-Encoding", obj.enc)
		}
		if r.Header.Get("If-None-Match") == tag {
			w.WriteHeader(304)
			return
		}
		w.Write(obj.body)
	case http.MethodHead:
		obj, ok := m.objs[key]
		if !ok {
			w.WriteHeader(404)
			return
		}
		w.Header().Set("ETag", etag(obj.body))
		w.Header().Set("Content-Length", strconv.Itoa(len(obj.body)))
		if obj.enc != "" {
			w.Header().Set("Content-Encoding", obj.enc)
		}
		w.WriteHeader(200)
	case http.MethodPut:
		body, _ := io.ReadAll(r.Body)
		existing, exists := m.objs[key]
		if r.Header.Get("If-None-Match") == "*" && exists {
			w.WriteHeader(412)
			return
		}
		if match := r.Header.Get("If-Match"); match != "" && (!exists || etag(existing.body) != match) {
			w.WriteHeader(412)
			return
		}
		m.objs[key] = memObject{body: body, enc: r.Header.Get("Content-Encoding")}
		m.puts[key]++
		w.Header().Set("ETag", etag(body))
		w.WriteHeader(200)
	case http.MethodDelete:
		delete(m.objs, key)
		w.WriteHeader(204)
	default:
		w.WriteHeader(405)
	}
}

// list answers a ListObjectsV2 request over the in-memory keys under prefix.
func (m *memBucket) list(w http.ResponseWriter, r *http.Request) {
	prefix := r.URL.Query().Get("prefix")
	var keys []string
	for k := range m.objs {
		if strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	fmt.Fprint(w, `<?xml version="1.0"?><ListBucketResult><IsTruncated>false</IsTruncated>`)
	for _, k := range keys {
		fmt.Fprintf(w, "<Contents><Key>%s</Key></Contents>", k)
	}
	fmt.Fprint(w, `</ListBucketResult>`)
}

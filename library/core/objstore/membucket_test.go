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
	mu        sync.Mutex
	objs      map[string]memObject
	puts      map[string]int
	gets      map[string]int  // per-key non-list GET count (skip-path assertions)
	lists     int             // ListObjectsV2 request count
	failPuts  map[string]bool // keys whose PUT returns 500 (simulated hard error)
	flakyPuts map[string]int  // keys whose next N PUTs return 500, then succeed
	failGets  map[string]int  // keys whose GETs return 500 forever (>0 = armed)
	flakyGets map[string]int  // keys whose next N GETs return 500, then succeed
}

// newMemBucket returns an empty in-memory bucket.
func newMemBucket() *memBucket {
	return &memBucket{objs: map[string]memObject{}, puts: map[string]int{}, gets: map[string]int{}, failPuts: map[string]bool{}, flakyPuts: map[string]int{}, failGets: map[string]int{}, flakyGets: map[string]int{}}
}

// failPut marks a bucket-relative key so its next PUTs return HTTP 500.
func (m *memBucket) failPut(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failPuts[key] = true
}

// flakyPut marks a bucket-relative key so its next n PUTs return HTTP 500,
// after which PUTs succeed (simulated transient fault).
func (m *memBucket) flakyPut(key string, n int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.flakyPuts[key] = n
}

// failGet marks a bucket-relative key so every GET returns HTTP 500 (a
// fault that never clears — the read-retry must give up and surface the error).
func (m *memBucket) failGet(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failGets[key] = 1
}

// flakyGet marks a bucket-relative key so its next n GETs return HTTP 500,
// after which GETs succeed (a transient read fault the retry must absorb).
func (m *memBucket) flakyGet(key string, n int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.flakyGets[key] = n
}

// putCount returns how many times a key (bucket-relative) was PUT.
func (m *memBucket) putCount(key string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.puts[key]
}

// getCount returns how many non-list GETs a key (bucket-relative) received.
func (m *memBucket) getCount(key string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.gets[key]
}

// listCount returns how many ListObjectsV2 requests the bucket received.
func (m *memBucket) listCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lists
}

// totalPuts returns the total number of successful PUTs across all keys.
func (m *memBucket) totalPuts() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	total := 0
	for _, n := range m.puts {
		total += n
	}
	return total
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
			m.lists++
			m.list(w, r)
			return
		}
		m.gets[key]++
		if m.failGets[key] > 0 {
			w.WriteHeader(500)
			return
		}
		if m.flakyGets[key] > 0 {
			m.flakyGets[key]--
			w.WriteHeader(500)
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
		if m.failPuts[key] {
			w.WriteHeader(500)
			return
		}
		if m.flakyPuts[key] > 0 {
			m.flakyPuts[key]--
			w.WriteHeader(500)
			return
		}
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
		fmt.Fprintf(w, "<Contents><Key>%s</Key><ETag>%s</ETag></Contents>", k, etag(m.objs[k].body))
	}
	fmt.Fprint(w, `</ListBucketResult>`)
}

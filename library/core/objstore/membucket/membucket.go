// membucket.go - an in-process, in-memory S3 stub for the objstore tests.
//
// Implements the GET/HEAD/PUT/DELETE + ListObjectsV2 surface the Client uses
// (path-style: the first path segment is the bucket), including If-None-Match /
// If-Match conditional writes, a stored Content-Encoding replayed on read, and a
// per-key PUT counter so skip-existing behavior is directly assertable. Mirrors
// locals3/main.go's semantics but stays in-process (no port, no disk) and
// ignores SigV4 (the Client signs, the stub does not verify — the tests exercise
// artifact logic, not auth).

package membucket

import (
	"crypto/md5"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// object is one stored object: its bytes and Content-Encoding.
type object struct {
	body []byte
	enc  string
}

// Bucket is a threadsafe in-memory object store implementing http.Handler.
type Bucket struct {
	mu          sync.Mutex
	objs        map[string]object
	puts        map[string]int
	putAttempts map[string]int  // per-key PUT request count, refused attempts included
	gets        map[string]int  // per-key non-list GET count (skip-path assertions)
	lists       int             // ListObjectsV2 request count
	failPuts    map[string]bool // keys whose PUT returns 500 (simulated hard error)
	flakyPuts   map[string]int  // keys whose next N PUTs fail, then succeed
	putStatus   map[string]int  // status a failing PUT answers with (0 = 500)

	// rejectIfMatch models a create-only provider (Ceph RGW, DO Spaces): every
	// If-Match is refused with a 412 even when the ETag matches, while
	// If-None-Match: * creates are still enforced. ifMatchTries counts every
	// If-Match write the bucket saw, honored or not.
	rejectIfMatch bool
	ifMatchTries  int
	failGets      map[string]int // keys whose GETs return 500 forever (>0 = armed)
	flakyGets     map[string]int // keys whose next N GETs fail, then succeed
	getStatus     map[string]int // status a failing GET answers with (0 = 500)
}

// New returns an empty in-memory bucket.
func New() *Bucket {
	return &Bucket{
		objs: map[string]object{}, puts: map[string]int{}, putAttempts: map[string]int{}, gets: map[string]int{},
		failPuts: map[string]bool{}, flakyPuts: map[string]int{}, putStatus: map[string]int{},
		failGets: map[string]int{}, flakyGets: map[string]int{}, getStatus: map[string]int{},
	}
}

// FailPut marks a bucket-relative key so its next PUTs return HTTP 500.
func (m *Bucket) FailPut(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failPuts[key] = true
}

// ClearFailPut lets a key's PUTs succeed again, so a test can assert what the pass after a failure does.
func (m *Bucket) ClearFailPut(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.failPuts, key)
}

// FlakyPut marks a bucket-relative key so its next n PUTs return HTTP 500, after which PUTs succeed.
func (m *Bucket) FlakyPut(key string, n int) { m.FlakyPutStatus(key, n, 500) }

// FlakyPutStatus is FlakyPut with the status the refused PUTs answer with.
func (m *Bucket) FlakyPutStatus(key string, n, status int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.flakyPuts[key] = n
	m.putStatus[key] = status
}

// FailGet marks a bucket-relative key so every GET returns HTTP 500, a fault that does not clear.
func (m *Bucket) FailGet(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failGets[key] = 1
}

// FlakyGet marks a bucket-relative key so its next n GETs return HTTP 500, after which GETs succeed.
func (m *Bucket) FlakyGet(key string, n int) { m.FlakyGetStatus(key, n, 500) }

// FlakyGetStatus is FlakyGet with the status the refused GETs answer with.
func (m *Bucket) FlakyGetStatus(key string, n, status int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.flakyGets[key] = n
	m.getStatus[key] = status
}

// RejectIfMatchWrites makes the bucket behave like a create-only provider.
func (m *Bucket) RejectIfMatchWrites() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rejectIfMatch = true
}

// IfMatchCount returns how many If-Match writes the bucket saw.
func (m *Bucket) IfMatchCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.ifMatchTries
}

// PutCount returns how many times a key (bucket-relative) was stored.
func (m *Bucket) PutCount(key string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.puts[key]
}

// PutAttempts returns how many PUT requests a key received, refused attempts included.
func (m *Bucket) PutAttempts(key string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.putAttempts[key]
}

// Seed stores one object's bytes directly, for a fixture the Client cannot write.
func (m *Bucket) Seed(key string, body []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.objs[key] = object{body: body}
}

// Object returns a stored object's bytes; ok is false when the key is absent.
func (m *Bucket) Object(key string) (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	obj, ok := m.objs[key]
	return string(obj.body), ok
}

// EncOf returns the Content-Encoding a key (bucket-relative) was stored with, "" when it carries none.
func (m *Bucket) EncOf(key string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.objs[key].enc
}

// GetCount returns how many non-list GETs a key (bucket-relative) received.
func (m *Bucket) GetCount(key string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.gets[key]
}

// ListCount returns how many ListObjectsV2 requests the bucket received.
func (m *Bucket) ListCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lists
}

// TotalPuts returns the total number of successful PUTs across all keys.
func (m *Bucket) TotalPuts() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	total := 0
	for _, n := range m.puts {
		total += n
	}
	return total
}

// failStatus returns the status an armed failure answers with, 500 when none was named.
func failStatus(status int) int {
	if status == 0 {
		return 500
	}
	return status
}

// ETag returns the quoted md5 hex of bytes, matching S3 ETag shape.
func ETag(b []byte) string { return fmt.Sprintf("%q", fmt.Sprintf("%x", md5.Sum(b))) }

// KeyOf strips the leading "/<bucket>/" so stored keys are bucket-relative.
func KeyOf(path string) string {
	p := strings.TrimPrefix(path, "/")
	if i := strings.IndexByte(p, '/'); i >= 0 {
		return p[i+1:]
	}
	return ""
}

// ServeHTTP dispatches the S3 subset under a single lock.
func (m *Bucket) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	key := KeyOf(r.URL.Path)
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
			w.WriteHeader(failStatus(m.getStatus[key]))
			return
		}
		if m.flakyGets[key] > 0 {
			m.flakyGets[key]--
			w.WriteHeader(failStatus(m.getStatus[key]))
			return
		}
		obj, ok := m.objs[key]
		if !ok {
			w.WriteHeader(404)
			return
		}
		tag := ETag(obj.body)
		w.Header().Set("ETag", tag)
		if obj.enc != "" {
			w.Header().Set("Content-Encoding", obj.enc)
		}
		if r.Header.Get("If-None-Match") == tag {
			w.WriteHeader(304)
			return
		}
		// A test client that hung up needs no report from the stub.
		_, _ = w.Write(obj.body)
	case http.MethodHead:
		obj, ok := m.objs[key]
		if !ok {
			w.WriteHeader(404)
			return
		}
		w.Header().Set("ETag", ETag(obj.body))
		w.Header().Set("Content-Length", strconv.Itoa(len(obj.body)))
		if obj.enc != "" {
			w.Header().Set("Content-Encoding", obj.enc)
		}
		w.WriteHeader(200)
	case http.MethodPut:
		body, _ := io.ReadAll(r.Body)
		m.putAttempts[key]++
		if m.failPuts[key] {
			w.WriteHeader(failStatus(m.putStatus[key]))
			return
		}
		if m.flakyPuts[key] > 0 {
			m.flakyPuts[key]--
			w.WriteHeader(failStatus(m.putStatus[key]))
			return
		}
		existing, exists := m.objs[key]
		if r.Header.Get("If-None-Match") == "*" && exists {
			w.WriteHeader(412)
			return
		}
		if match := r.Header.Get("If-Match"); match != "" {
			m.ifMatchTries++
			if m.rejectIfMatch || !exists || ETag(existing.body) != match {
				w.WriteHeader(412)
				return
			}
		}
		m.objs[key] = object{body: body, enc: r.Header.Get("Content-Encoding")}
		m.puts[key]++
		w.Header().Set("ETag", ETag(body))
		w.WriteHeader(200)
	case http.MethodDelete:
		delete(m.objs, key)
		w.WriteHeader(204)
	default:
		w.WriteHeader(405)
	}
}

// list answers a ListObjectsV2 request over the in-memory keys under prefix.
func (m *Bucket) list(w http.ResponseWriter, r *http.Request) {
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
		// Escape the key, as a real bucket and locals3 both do: "&" is legal in a
		// git ref name and writing it raw makes the whole document unparseable.
		fmt.Fprint(w, "<Contents><Key>")
		_ = xml.EscapeText(w, []byte(k))
		fmt.Fprintf(w, "</Key><ETag>%s</ETag></Contents>", ETag(m.objs[k].body))
	}
	fmt.Fprint(w, `</ListBucketResult>`)
}

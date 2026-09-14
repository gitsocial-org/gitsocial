// server_test.go - A fake GitLab API over httptest for the adapter tests
package gitlab

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// glRequest is one request the adapter sent to the fake GitLab.
type glRequest struct {
	method string
	path   string
	query  url.Values
	token  string
	body   string
}

// glServer is an httptest server that records every request the adapter makes.
type glServer struct {
	server   *httptest.Server
	mu       sync.Mutex
	recorded []glRequest
}

// newGLServer starts a fake GitLab API serving handler and recording each request.
func newGLServer(t *testing.T, handler http.HandlerFunc) *glServer {
	t.Helper()
	s := &glServer{}
	s.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		s.mu.Lock()
		s.recorded = append(s.recorded, glRequest{
			method: r.Method,
			path:   r.URL.EscapedPath(),
			query:  r.URL.Query(),
			token:  r.Header.Get("PRIVATE-TOKEN"),
			body:   string(body),
		})
		s.mu.Unlock()
		handler(w, r)
	}))
	t.Cleanup(s.server.Close)
	return s
}

// requests returns a copy of everything recorded so far.
func (s *glServer) requests() []glRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]glRequest(nil), s.recorded...)
}

// count returns how many recorded requests ended with the given path suffix.
func (s *glServer) count(suffix string) int {
	n := 0
	for _, r := range s.requests() {
		if strings.HasSuffix(r.path, suffix) {
			n++
		}
	}
	return n
}

// first returns the first recorded request ending with the given path suffix.
func (s *glServer) first(t *testing.T, suffix string) glRequest {
	t.Helper()
	for _, r := range s.requests() {
		if strings.HasSuffix(r.path, suffix) {
			return r
		}
	}
	t.Fatalf("no request to %q", suffix)
	return glRequest{}
}

// all returns every recorded request ending with the given path suffix.
func (s *glServer) all(suffix string) []glRequest {
	var out []glRequest
	for _, r := range s.requests() {
		if strings.HasSuffix(r.path, suffix) {
			out = append(out, r)
		}
	}
	return out
}

// host returns the fake server's host, the domain the adapter derives emails from.
func (s *glServer) host() string {
	parsed, _ := url.Parse(s.server.URL)
	return parsed.Host
}

// newTestAdapter builds an adapter for acme/widgets pointed at the fake server.
func newTestAdapter(s *glServer) *Adapter {
	return New("acme", "widgets", AdapterOptions{BaseURL: s.server.URL, Token: "test-token"})
}

// glRoute pairs a request path suffix with the handler that answers it.
type glRoute struct {
	suffix  string
	handler http.HandlerFunc
}

// routed serves the first route matching the request path and fails the test on anything else.
func routed(t *testing.T, routes ...glRoute) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		for _, route := range routes {
			if strings.HasSuffix(r.URL.EscapedPath(), route.suffix) {
				route.handler(w, r)
				return
			}
		}
		t.Errorf("unrouted request: %s %s", r.Method, r.URL)
		http.Error(w, "unrouted", http.StatusNotFound)
	}
}

// jsonRoute answers with a fixed 200 JSON body.
func jsonRoute(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	}
}

// statusRoute answers with a fixed status code and body.
func statusRoute(code int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(code)
		_, _ = io.WriteString(w, body)
	}
}

// pagedRoute serves the page named by the page query parameter and sets X-Next-Page for the rest.
func pagedRoute(pages ...string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		index := 1
		if p := r.URL.Query().Get("page"); p != "" {
			parsed, err := strconv.Atoi(p)
			if err != nil {
				http.Error(w, "bad page", http.StatusBadRequest)
				return
			}
			index = parsed
		}
		if index < 1 || index > len(pages) {
			http.Error(w, "no such page", http.StatusNotFound)
			return
		}
		if index < len(pages) {
			w.Header().Set("X-Next-Page", strconv.Itoa(index+1))
		}
		_, _ = io.WriteString(w, pages[index-1])
	}
}

// failingPageRoute serves one page announcing a second, then fails every page after it.
func failingPageRoute(first string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") != "" {
			http.Error(w, `{"message":"404 Not Found"}`, http.StatusNotFound)
			return
		}
		w.Header().Set("X-Next-Page", "2")
		_, _ = io.WriteString(w, first)
	}
}

// testProfiles are the user lookups the fixtures rely on.
var testProfiles = map[string]string{
	"alice": `{"name":"Alice Example","public_email":"alice@example.com"}`,
	"bob":   `{"name":"Bob Example","commit_email":"bob@commit.example.com"}`,
	"carol": `{"name":"Carol Example","email":"carol@private.example.com"}`,
	"dave":  `{"name":"Dave Example"}`,
}

// usersRoute answers a username lookup from testProfiles; an unknown username yields an empty array.
func usersRoute() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		profile, ok := testProfiles[r.URL.Query().Get("username")]
		if !ok {
			_, _ = io.WriteString(w, `[]`)
			return
		}
		_, _ = io.WriteString(w, "["+profile+"]")
	}
}

// graphqlUnavailableRoute fails the blocking-link query so the REST fallback runs.
func graphqlUnavailableRoute() http.HandlerFunc {
	return statusRoute(http.StatusNotFound, `{"message":"404 Not Found"}`)
}

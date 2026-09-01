// resolve_http_test.go - Tests for well-known identity resolution against a local HTTPS server
package identity

import (
	"context"
	"database/sql"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/cache"
)

// withWellKnownServer routes the package http client at a TLS test server for
// every host, using the existing package-level httpClient seam. The httptest
// certificate covers example.com, so real-looking domains resolve to the server.
func withWellKnownServer(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	srv := httptest.NewTLSServer(handler)
	client := srv.Client()
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("httptest client transport = %T, want *http.Transport", client.Transport)
	}
	transport.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, srv.Listener.Addr().String())
	}
	prev := httpClient
	httpClient = client
	t.Cleanup(func() {
		httpClient = prev
		srv.Close()
	})
}

// wellKnownJSON is a minimal document declaring one identity.
const wellKnownJSON = `{"identities":{"alice":{"key":"ABCDEF1234567890","repo":"https://github.com/alice/alice"}}}`

func TestResolveIdentity_success(t *testing.T) {
	setupTestCache(t)
	var requests atomic.Int32
	withWellKnownServer(t, func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Path != "/.well-known/gitmsg-id.json" {
			t.Errorf("path = %q, want /.well-known/gitmsg-id.json", r.URL.Path)
		}
		w.Write([]byte(wellKnownJSON))
	})

	got, err := ResolveIdentity("Alice@Example.com")
	if err != nil {
		t.Fatalf("ResolveIdentity() error = %v", err)
	}
	if got.Key != "ABCDEF1234567890" {
		t.Errorf("Key = %q, want ABCDEF1234567890", got.Key)
	}
	if got.Email != "alice@example.com" {
		t.Errorf("Email = %q, want the normalized address", got.Email)
	}
	if got.Repo != "https://github.com/alice/alice" {
		t.Errorf("Repo = %q", got.Repo)
	}
	if !got.Resolved || got.Cached {
		t.Errorf("Resolved = %v, Cached = %v, want true/false on a network hit", got.Resolved, got.Cached)
	}

	// The second lookup is served from the DNS cache, not the network.
	cached, err := ResolveIdentity("alice@example.com")
	if err != nil {
		t.Fatalf("second ResolveIdentity() error = %v", err)
	}
	if !cached.Cached {
		t.Error("second lookup should be served from the cache")
	}
	if requests.Load() != 1 {
		t.Errorf("server requests = %d, want 1", requests.Load())
	}
}

func TestResolveIdentity_expiredCacheRefetches(t *testing.T) {
	setupTestCache(t)
	var requests atomic.Int32
	withWellKnownServer(t, func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.Write([]byte(wellKnownJSON))
	})

	stale := time.Now().Add(-2 * dnsCacheTTL).UTC().Format(time.RFC3339)
	if err := cache.ExecLocked(func(db *sql.DB) error {
		_, err := db.Exec(`INSERT INTO core_identity_dns (email, key, repo, resolved_at) VALUES (?, ?, ?, ?)`,
			"alice@example.com", "STALEKEY", "", stale)
		return err
	}); err != nil {
		t.Fatalf("seed stale row: %v", err)
	}

	got, err := ResolveIdentity("alice@example.com")
	if err != nil {
		t.Fatalf("ResolveIdentity() error = %v", err)
	}
	if got.Key != "ABCDEF1234567890" {
		t.Errorf("Key = %q, want the refetched key", got.Key)
	}
	if requests.Load() != 1 {
		t.Errorf("server requests = %d, want 1 (expired entries refetch)", requests.Load())
	}
}

func TestResolveIdentity_mailSubdomainFallback(t *testing.T) {
	setupTestCache(t)
	var hosts []string
	withWellKnownServer(t, func(w http.ResponseWriter, r *http.Request) {
		hosts = append(hosts, r.Host)
		if r.Host != "example.com" {
			http.Error(w, "not here", http.StatusNotFound)
			return
		}
		w.Write([]byte(wellKnownJSON))
	})

	got, err := ResolveIdentity("alice@mail.example.com")
	if err != nil {
		t.Fatalf("ResolveIdentity() error = %v", err)
	}
	if got.Key != "ABCDEF1234567890" {
		t.Errorf("Key = %q, want the parent-domain key", got.Key)
	}
	if len(hosts) != 2 || hosts[0] != "mail.example.com" || hosts[1] != "example.com" {
		t.Errorf("hosts = %v, want [mail.example.com example.com]", hosts)
	}
}

func TestResolveIdentity_errors(t *testing.T) {
	tests := []struct {
		name    string
		email   string
		status  int
		body    string
		wantErr string
	}{
		{name: "http error", email: "alice@example.com", status: http.StatusNotFound, body: "nope", wantErr: "HTTP 404"},
		{name: "malformed json", email: "alice@example.com", status: http.StatusOK, body: "{not json", wantErr: "parse"},
		{name: "identity absent", email: "bob@example.com", status: http.StatusOK, body: wellKnownJSON, wantErr: `no identity for "bob"`},
		{name: "identity without key", email: "carol@example.com", status: http.StatusOK, body: `{"identities":{"carol":{"repo":"x"}}}`, wantErr: "has no key"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setupTestCache(t)
			withWellKnownServer(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				w.Write([]byte(tt.body))
			})
			got, err := ResolveIdentity(tt.email)
			if err == nil {
				t.Fatalf("ResolveIdentity() = %+v, want an error", got)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want it to mention %q", err, tt.wantErr)
			}
		})
	}
}

func TestResolveIdentity_invalidEmail(t *testing.T) {
	for _, email := range []string{"", "not-an-email", "@example.com", "alice@"} {
		if _, err := ResolveIdentity(email); err == nil {
			t.Errorf("ResolveIdentity(%q) should error", email)
		}
	}
}

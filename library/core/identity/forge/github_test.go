// github_test.go - Tests for the GitHub forge adapter against a local HTTP server
package forge

import (
	"bytes"
	"context"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
)

// newTestGitHub points a GitHub adapter at a local server standing in for both
// the web and API hosts, using the adapter's own unexported endpoint fields.
func newTestGitHub(t *testing.T, handler http.HandlerFunc) *gitHubForge {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &gitHubForge{host: "github.com", api: srv.URL, web: srv.URL, client: srv.Client()}
}

// newTestKeyring builds a throwaway OpenPGP key and returns it armored, in
// binary form, and its uppercase fingerprint.
func newTestKeyring(t *testing.T, name, email string) (armored, binary []byte, fingerprint string) {
	t.Helper()
	entity, err := openpgp.NewEntity(name, "", email, nil)
	if err != nil {
		t.Fatalf("NewEntity: %v", err)
	}
	var raw bytes.Buffer
	if err := entity.Serialize(&raw); err != nil {
		t.Fatalf("serialize public key: %v", err)
	}
	var armoredBuf bytes.Buffer
	w, err := armor.Encode(&armoredBuf, openpgp.PublicKeyType, nil)
	if err != nil {
		t.Fatalf("armor encode: %v", err)
	}
	if _, err := w.Write(raw.Bytes()); err != nil {
		t.Fatalf("armor write: %v", err)
	}
	w.Close()
	return armoredBuf.Bytes(), raw.Bytes(), strings.ToUpper(hex.EncodeToString(entity.PrimaryKey.Fingerprint))
}

func TestGitHubFetchGPGKeys(t *testing.T) {
	armored, binary, fingerprint := newTestKeyring(t, "Alice", "alice@example.com")

	t.Run("armored", func(t *testing.T) {
		g := newTestGitHub(t, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/alice.gpg" {
				t.Errorf("path = %q, want /alice.gpg", r.URL.Path)
			}
			w.Write(armored)
		})
		keys, err := g.FetchGPGKeys(context.Background(), "alice")
		if err != nil {
			t.Fatalf("FetchGPGKeys() error = %v", err)
		}
		if len(keys) != 1 {
			t.Fatalf("FetchGPGKeys() = %d keys, want 1", len(keys))
		}
		if keys[0].Fingerprint != fingerprint {
			t.Errorf("Fingerprint = %q, want %q", keys[0].Fingerprint, fingerprint)
		}
		if len(keys[0].Emails) != 1 || keys[0].Emails[0] != "alice@example.com" {
			t.Errorf("Emails = %v, want [alice@example.com]", keys[0].Emails)
		}
		if len(keys[0].Subkeys) != 1 {
			t.Errorf("Subkeys = %v, want the encryption subkey", keys[0].Subkeys)
		}
	})

	t.Run("binary", func(t *testing.T) {
		g := newTestGitHub(t, func(w http.ResponseWriter, _ *http.Request) { w.Write(binary) })
		keys, err := g.FetchGPGKeys(context.Background(), "alice")
		if err != nil {
			t.Fatalf("FetchGPGKeys() error = %v", err)
		}
		if len(keys) != 1 || keys[0].Fingerprint != fingerprint {
			t.Errorf("FetchGPGKeys() = %+v, want the same key as the armored form", keys)
		}
	})

	t.Run("http error", func(t *testing.T) {
		g := newTestGitHub(t, func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "no such user", http.StatusNotFound)
		})
		if _, err := g.FetchGPGKeys(context.Background(), "ghost"); err == nil || !strings.Contains(err.Error(), "HTTP 404") {
			t.Errorf("FetchGPGKeys() error = %v, want an HTTP 404 error", err)
		}
	})

	t.Run("garbage body", func(t *testing.T) {
		g := newTestGitHub(t, func(w http.ResponseWriter, _ *http.Request) {
			w.Write([]byte("not a key"))
		})
		if _, err := g.FetchGPGKeys(context.Background(), "alice"); err == nil || !strings.Contains(err.Error(), "parse keyring") {
			t.Errorf("FetchGPGKeys() error = %v, want a keyring parse error", err)
		}
	})

	t.Run("empty account", func(t *testing.T) {
		g := newTestGitHub(t, func(w http.ResponseWriter, _ *http.Request) {})
		keys, err := g.FetchGPGKeys(context.Background(), "nokeys")
		if err != nil {
			t.Fatalf("FetchGPGKeys() error = %v", err)
		}
		if len(keys) != 0 {
			t.Errorf("FetchGPGKeys() = %v, want no keys for an empty body", keys)
		}
	})
}

const commitVerificationJSON = `{
  "commit": {"author": {"email": "Alice@Example.COM"}, "verification": {"verified": true, "reason": "valid"}},
  "author": {"login": "alice", "id": 4242}
}`

func TestGitHubFetchCommitVerification(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "test-token")

	var gotPath, gotAccept, gotVersion, gotAuth string
	g := newTestGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAccept = r.Header.Get("Accept")
		gotVersion = r.Header.Get("X-GitHub-Api-Version")
		gotAuth = r.Header.Get("Authorization")
		w.Write([]byte(commitVerificationJSON))
	})

	v, err := g.FetchCommitVerification(context.Background(), "owner", "repo", "abc123456789")
	if err != nil {
		t.Fatalf("FetchCommitVerification() error = %v", err)
	}
	if gotPath != "/repos/owner/repo/commits/abc123456789" {
		t.Errorf("path = %q", gotPath)
	}
	if gotAccept != "application/vnd.github+json" || gotVersion != "2022-11-28" {
		t.Errorf("headers: Accept = %q, X-GitHub-Api-Version = %q", gotAccept, gotVersion)
	}
	if gotAuth != "Bearer test-token" {
		t.Errorf("Authorization = %q, want the env token", gotAuth)
	}
	if !v.Verified {
		t.Error("Verified = false, want true")
	}
	if v.AuthorEmail != "alice@example.com" {
		t.Errorf("AuthorEmail = %q, want it lowercased", v.AuthorEmail)
	}
	if v.AccountLogin != "alice" || v.AccountID != "4242" {
		t.Errorf("account = %q/%q, want alice/4242", v.AccountLogin, v.AccountID)
	}
}

func TestGitHubFetchCommitVerification_errors(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "test-token")

	tests := []struct {
		name    string
		handler http.HandlerFunc
		wantErr string
	}{
		{
			name: "rate limited",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("X-RateLimit-Remaining", "0")
				w.WriteHeader(http.StatusForbidden)
			},
			wantErr: "rate-limited",
		},
		{
			name: "forbidden but not rate limited",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("X-RateLimit-Remaining", "37")
				w.WriteHeader(http.StatusForbidden)
			},
			wantErr: "HTTP 403",
		},
		{
			name:    "not found",
			handler: func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) },
			wantErr: "HTTP 404",
		},
		{
			name:    "malformed json",
			handler: func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("{oops")) },
			wantErr: "decode response",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := newTestGitHub(t, tt.handler)
			_, err := g.FetchCommitVerification(context.Background(), "owner", "repo", "abc")
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("FetchCommitVerification() error = %v, want it to mention %q", err, tt.wantErr)
			}
		})
	}
}

func TestGitHubFetchCommitVerification_anonymousAuthor(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "test-token")
	g := newTestGitHub(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"commit": {"author": {"email": "bob@example.com"}, "verification": {"verified": false}}, "author": null}`))
	})
	v, err := g.FetchCommitVerification(context.Background(), "owner", "repo", "abc")
	if err != nil {
		t.Fatalf("FetchCommitVerification() error = %v", err)
	}
	if v.Verified || v.AccountLogin != "" || v.AccountID != "" {
		t.Errorf("got %+v, want an unverified result with no account", v)
	}
}

func TestResolveGitHubToken(t *testing.T) {
	t.Run("GITHUB_TOKEN wins", func(t *testing.T) {
		t.Setenv("GITHUB_TOKEN", "primary")
		t.Setenv("GH_TOKEN", "secondary")
		if got := resolveGitHubToken(); got != "primary" {
			t.Errorf("resolveGitHubToken() = %q, want primary", got)
		}
	})
	t.Run("GH_TOKEN fallback", func(t *testing.T) {
		t.Setenv("GITHUB_TOKEN", "")
		t.Setenv("GH_TOKEN", "secondary")
		if got := resolveGitHubToken(); got != "secondary" {
			t.Errorf("resolveGitHubToken() = %q, want secondary", got)
		}
	})
}

func TestNewGitHubDefaults(t *testing.T) {
	g, ok := NewGitHub().(*gitHubForge)
	if !ok {
		t.Fatal("NewGitHub() did not return a *gitHubForge")
	}
	if g.Host() != "github.com" || g.api != "https://api.github.com" || g.web != "https://github.com" {
		t.Errorf("NewGitHub() = %+v, want the github.com endpoints", g)
	}
}

func TestUIDEmail(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, uid, want string
	}{
		{"angle bracket form", "Alice <Alice@Example.com>", "alice@example.com"},
		{"with comment", "Alice (work) <alice@example.com>", "alice@example.com"},
		{"no email", "Alice", ""},
		{"unterminated", "Alice <alice@example.com", ""},
	}
	for _, tt := range tests {
		if got := uidEmail(tt.uid, nil); got != tt.want {
			t.Errorf("%s: uidEmail(%q) = %q, want %q", tt.name, tt.uid, got, tt.want)
		}
	}
}

// resolve.go - DNS-based identity resolution via https://domain/.well-known/gitmsg-id.json
package identity

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gitsocial-org/gitsocial/core/cache"
)

const (
	dnsTimeout  = 10 * time.Second
	dnsCacheTTL = 24 * time.Hour
)

var httpClient = &http.Client{Timeout: dnsTimeout}

// DNSIdentity represents a resolved identity from a well-known endpoint.
type DNSIdentity struct {
	Key  string `json:"key"`
	Repo string `json:"repo,omitempty"`
}

// dnsResponse is the JSON structure returned by /.well-known/gitmsg-id.json.
type dnsResponse struct {
	Identities map[string]DNSIdentity `json:"identities"`
}

// ResolvedIdentity is the result of DNS resolution with metadata.
type ResolvedIdentity struct {
	Identity
	Repo     string `json:"repo,omitempty"`
	Resolved bool   `json:"resolved"`
	Cached   bool   `json:"cached"`
}

// mailSubdomainPrefixes recognizes common mail-host subdomain prefixes for the
// fallback heuristic that walks one level up to the parent domain.
var mailSubdomainPrefixes = []string{"mail.", "email.", "smtp.", "imap.", "pop.", "mx."}

// ResolveIdentity resolves an email via DNS well-known endpoint.
// Checks the DNS cache first; fetches from the network if expired or missing.
// The mail-subdomain fallback is defined by the protocol (specs/GITMSG.md §3.2):
// for an email at <prefix>.<parent> where <prefix> is one of mail/email/smtp/
// imap/pop/mx, the document at <parent> MAY also attest the binding. The walk
// is bounded to one step.
func ResolveIdentity(email string) (*ResolvedIdentity, error) {
	email = NormalizeEmail(email)
	parts := strings.SplitN(email, "@", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return nil, fmt.Errorf("invalid email format: %s", email)
	}
	localPart := parts[0]
	domain := parts[1]

	if cached, ok := getCachedDNSIdentity(email); ok {
		return cached, nil
	}

	resolved, err := fetchWellKnown(email, localPart, domain)
	if err != nil {
		if parent := mailParentDomain(domain); parent != "" {
			resolved, err = fetchWellKnown(email, localPart, parent)
		}
	}
	if err != nil {
		return nil, err
	}

	cacheDNSIdentity(email, resolved)
	return resolved, nil
}

// fetchWellKnown attempts to fetch and parse a gitmsg-id document at the given
// host, returning the entry for localPart.
func fetchWellKnown(email, localPart, host string) (*ResolvedIdentity, error) {
	url := fmt.Sprintf("https://%s/.well-known/gitmsg-id.json", host)
	resp, err := httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch %s: HTTP %d", url, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	var dnsResp dnsResponse
	if err := json.Unmarshal(body, &dnsResp); err != nil {
		return nil, fmt.Errorf("parse %s: %w", url, err)
	}
	dnsID, ok := dnsResp.Identities[localPart]
	if !ok {
		return nil, fmt.Errorf("no identity for %q at %s", localPart, url)
	}
	if dnsID.Key == "" {
		return nil, fmt.Errorf("identity for %q has no key", email)
	}
	return &ResolvedIdentity{
		Identity: Identity{Key: dnsID.Key, Email: email},
		Repo:     dnsID.Repo,
		Resolved: true,
	}, nil
}

// mailParentDomain returns the parent domain when the input host begins with a
// recognizable mail prefix; otherwise "". Only one level up — never walks
// further. The result still contains at least one dot (so we never produce a
// bare TLD or registered domain like "co.uk" by stripping prefixes).
func mailParentDomain(host string) string {
	for _, prefix := range mailSubdomainPrefixes {
		if strings.HasPrefix(host, prefix) {
			parent := strings.TrimPrefix(host, prefix)
			if strings.Contains(parent, ".") {
				return parent
			}
			return ""
		}
	}
	return ""
}

func init() {
	cache.RegisterSchema("identity_dns", dnsCacheSchema)
}

const dnsCacheSchema = `
CREATE TABLE IF NOT EXISTS core_identity_dns (
    email TEXT NOT NULL PRIMARY KEY,
    key TEXT NOT NULL,
    repo TEXT,
    resolved_at TEXT NOT NULL
);
`

// getCachedDNSIdentity returns a cached DNS identity if it exists and hasn't expired.
func getCachedDNSIdentity(email string) (*ResolvedIdentity, bool) {
	results, err := cache.QueryLocked(func(db *sql.DB) ([]ResolvedIdentity, error) {
		var key, resolvedAt string
		var repo sql.NullString
		err := db.QueryRow(
			"SELECT key, repo, resolved_at FROM core_identity_dns WHERE email = ?", email,
		).Scan(&key, &repo, &resolvedAt)
		if err != nil {
			return nil, err
		}
		t, err := time.Parse(time.RFC3339, resolvedAt)
		if err != nil || time.Since(t) > dnsCacheTTL {
			return nil, fmt.Errorf("expired")
		}
		r := ResolvedIdentity{
			Identity: Identity{Key: key, Email: email},
			Resolved: true,
			Cached:   true,
		}
		if repo.Valid {
			r.Repo = repo.String
		}
		return []ResolvedIdentity{r}, nil
	})
	if err != nil || len(results) == 0 {
		return nil, false
	}
	return &results[0], true
}

// cacheDNSIdentity stores a resolved DNS identity in the cache.
func cacheDNSIdentity(email string, resolved *ResolvedIdentity) {
	_ = cache.ExecLocked(func(db *sql.DB) error {
		_, err := db.Exec(
			`INSERT OR REPLACE INTO core_identity_dns (email, key, repo, resolved_at) VALUES (?, ?, ?, ?)`,
			email, resolved.Key, resolved.Repo, time.Now().UTC().Format(time.RFC3339),
		)
		return err
	})
}

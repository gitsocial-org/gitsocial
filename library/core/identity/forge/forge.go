// forge.go - Forge adapter interface for identity verification
package forge

import (
	"context"
	"fmt"
	"net/url"
	"strings"
)

// Forge is a per-host adapter that exposes the two endpoints needed for
// identity verification: a static GPG-keys endpoint and a per-commit
// verification API.
type Forge interface {
	Host() string
	FetchGPGKeys(ctx context.Context, user string) ([]GPGKey, error)
	FetchCommitVerification(ctx context.Context, owner, repo, sha string) (*CommitVerification, error)
}

// GPGKey is a single OpenPGP public key extracted from a forge endpoint.
type GPGKey struct {
	// Fingerprint is the uppercase hex fingerprint of the primary key (40 chars).
	Fingerprint string
	// Emails is the set of UID emails attached to the key (lowercased).
	// Forges only publish UIDs whose emails were verified on the account.
	Emails []string
	// Subkeys are fingerprints of any subkeys (signatures may use a subkey).
	Subkeys []string
}

// CommitVerification is the response shape from a forge's commit verification API.
type CommitVerification struct {
	// Verified is true when the forge confirms the signature is valid AND the
	// signing key belongs to the named account.
	Verified bool
	// KeyFingerprint is the fingerprint of the signing key (uppercase hex for GPG,
	// "SHA256:base64" for SSH).
	KeyFingerprint string
	// AuthorEmail is the commit's author email as reported by the forge.
	AuthorEmail string
	// AccountID is the stable numeric account identifier (preferred over login).
	AccountID string
	// AccountLogin is the human-readable username; may be renamed/recycled.
	AccountLogin string
}

// ParseRepoURL splits a repo URL into host, owner, and repo components.
// Supports HTTPS (https://host/owner/repo[.git]) and SSH (git@host:owner/repo[.git]).
// Nested paths beyond owner/repo are ignored — owner and repo are always the first two segments.
func ParseRepoURL(repoURL string) (host, owner, repo string, err error) {
	if repoURL == "" {
		return "", "", "", fmt.Errorf("empty repo URL")
	}
	if strings.HasPrefix(repoURL, "git@") {
		rest := strings.TrimPrefix(repoURL, "git@")
		parts := strings.SplitN(rest, ":", 2)
		if len(parts) != 2 {
			return "", "", "", fmt.Errorf("invalid SSH URL: %s", repoURL)
		}
		host = parts[0]
		path := strings.TrimSuffix(parts[1], ".git")
		segments := strings.Split(path, "/")
		if len(segments) < 2 {
			return "", "", "", fmt.Errorf("SSH URL missing owner/repo: %s", repoURL)
		}
		return host, segments[0], segments[1], nil
	}
	u, parseErr := url.Parse(repoURL)
	if parseErr != nil {
		return "", "", "", fmt.Errorf("parse URL: %w", parseErr)
	}
	host = u.Host
	path := strings.TrimPrefix(u.Path, "/")
	path = strings.TrimSuffix(path, ".git")
	segments := strings.Split(path, "/")
	if len(segments) < 2 {
		return "", "", "", fmt.Errorf("URL missing owner/repo: %s", repoURL)
	}
	return host, segments[0], segments[1], nil
}

// github.go - GitHub forge adapter (.gpg endpoint + commits REST API)
package forge

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
)

// gitHubForge implements Forge against api.github.com and github.com endpoints.
type gitHubForge struct {
	host   string
	api    string
	web    string
	client *http.Client
}

// NewGitHub builds a GitHub adapter rooted at github.com.
func NewGitHub() Forge {
	return &gitHubForge{
		host:   "github.com",
		api:    "https://api.github.com",
		web:    "https://github.com",
		client: &http.Client{Timeout: 15 * time.Second},
	}
}

func (g *gitHubForge) Host() string { return g.host }

// FetchGPGKeys downloads <user>.gpg and parses the OpenPGP key block.
// Each primary key contributes one GPGKey, carrying every UID email.
// GitHub serves an ASCII-armored block by default; binary is also accepted.
func (g *gitHubForge) FetchGPGKeys(ctx context.Context, user string) ([]GPGKey, error) {
	url := g.web + "/" + user + ".gpg"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	resp, err := g.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch %s: HTTP %d", url, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	return parseGPGKeyring(body)
}

// FetchCommitVerification calls GET /repos/{owner}/{repo}/commits/{sha} and
// returns the verification block + author identity.
func (g *gitHubForge) FetchCommitVerification(ctx context.Context, owner, repo, sha string) (*CommitVerification, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/commits/%s", g.api, owner, repo, sha)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if token := resolveGitHubToken(); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := g.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusForbidden && resp.Header.Get("X-RateLimit-Remaining") == "0" {
		return nil, fmt.Errorf("rate-limited: %s", url)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch %s: HTTP %d", url, resp.StatusCode)
	}
	var payload struct {
		Commit struct {
			Author struct {
				Email string `json:"email"`
			} `json:"author"`
			Verification struct {
				Verified bool   `json:"verified"`
				Reason   string `json:"reason"`
			} `json:"verification"`
		} `json:"commit"`
		Author *struct {
			Login string `json:"login"`
			ID    int64  `json:"id"`
		} `json:"author"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	v := &CommitVerification{
		Verified:    payload.Commit.Verification.Verified,
		AuthorEmail: strings.ToLower(payload.Commit.Author.Email),
	}
	if payload.Author != nil {
		v.AccountLogin = payload.Author.Login
		v.AccountID = fmt.Sprintf("%d", payload.Author.ID)
	}
	return v, nil
}

// resolveGitHubToken returns a GitHub API token from environment or gh CLI.
func resolveGitHubToken() string {
	if t := os.Getenv("GITHUB_TOKEN"); t != "" {
		return t
	}
	if t := os.Getenv("GH_TOKEN"); t != "" {
		return t
	}
	cmd := exec.Command("gh", "auth", "token")
	out, err := cmd.Output()
	if err == nil {
		return strings.TrimSpace(string(out))
	}
	return ""
}

// parseGPGKeyring accepts armored or binary OpenPGP key data and returns one
// GPGKey per primary key, carrying every UID email.
func parseGPGKeyring(body []byte) ([]GPGKey, error) {
	var entities openpgp.EntityList
	var err error
	trimmed := bytes.TrimSpace(body)
	if bytes.HasPrefix(trimmed, []byte("-----BEGIN")) {
		entities, err = openpgp.ReadArmoredKeyRing(bytes.NewReader(body))
	} else {
		entities, err = openpgp.ReadKeyRing(bytes.NewReader(body))
	}
	if err != nil {
		return nil, fmt.Errorf("parse keyring: %w", err)
	}
	out := make([]GPGKey, 0, len(entities))
	for _, entity := range entities {
		if entity.PrimaryKey == nil {
			continue
		}
		fp := strings.ToUpper(hex.EncodeToString(entity.PrimaryKey.Fingerprint))
		gk := GPGKey{Fingerprint: fp}
		seen := make(map[string]bool)
		for name, ident := range entity.Identities {
			email := uidEmail(name, ident)
			if email == "" || seen[email] {
				continue
			}
			seen[email] = true
			gk.Emails = append(gk.Emails, email)
		}
		for _, sub := range entity.Subkeys {
			if sub.PublicKey == nil {
				continue
			}
			gk.Subkeys = append(gk.Subkeys, strings.ToUpper(hex.EncodeToString(sub.PublicKey.Fingerprint)))
		}
		out = append(out, gk)
	}
	return out, nil
}

// uidEmail extracts the lowercased email from an OpenPGP UID, falling back to
// parsing the canonical "Name (comment) <email>" string when the structured
// UserId record lacks an Email.
func uidEmail(uidKey string, ident *openpgp.Identity) string {
	if ident != nil && ident.UserId != nil {
		if e := strings.TrimSpace(ident.UserId.Email); e != "" {
			return strings.ToLower(e)
		}
	}
	if i := strings.Index(uidKey, "<"); i >= 0 {
		if j := strings.Index(uidKey[i:], ">"); j > 0 {
			return strings.ToLower(strings.TrimSpace(uidKey[i+1 : i+j]))
		}
	}
	return ""
}

func init() {
	Register(NewGitHub())
}

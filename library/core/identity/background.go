// background.go - Background resolution of verified bindings during fetch
package identity

import (
	"database/sql"
	"sync"

	"github.com/gitsocial-org/gitsocial/core/cache"
	"github.com/gitsocial-org/gitsocial/core/git"
	"github.com/gitsocial-org/gitsocial/core/log"
)

// backfillBatchLimit caps how many NULL-signer_key commits we backfill per
// fetch call. Keeps first-fetch-after-upgrade latency bounded; subsequent
// fetches finish the rest.
const backfillBatchLimit = 5000

// BackfillSignerKeys populates signer_key for commits in this repo that were
// fetched before signer extraction shipped. Returns (rows updated, candidates
// for re-verification). Bounded by backfillBatchLimit per call.
func BackfillSignerKeys(storageDir, repoURL string) (int, []VerifyCandidate) {
	if storageDir == "" || repoURL == "" {
		return 0, nil
	}
	type row struct {
		hash, branch, email string
	}
	rows, err := cache.QueryLocked(func(db *sql.DB) ([]row, error) {
		r, err := db.Query(`
			SELECT hash, branch, author_email FROM core_commits
			WHERE repo_url = ? AND signer_key IS NULL
			LIMIT ?`, repoURL, backfillBatchLimit)
		if err != nil {
			return nil, err
		}
		defer r.Close()
		var out []row
		for r.Next() {
			var h, b, e string
			if err := r.Scan(&h, &b, &e); err != nil {
				continue
			}
			out = append(out, row{h, b, e})
		}
		return out, r.Err()
	})
	if err != nil || len(rows) == 0 {
		return 0, nil
	}
	hashes := make([]string, 0, len(rows))
	for _, r := range rows {
		hashes = append(hashes, r.hash)
	}
	keys, err := git.GetCommitSignerKeys(storageDir, hashes)
	if err != nil {
		log.Debug("backfill signer keys", "repo", repoURL, "error", err)
		return 0, nil
	}
	updated := 0
	candidates := make([]VerifyCandidate, 0, len(rows))
	_ = cache.ExecLocked(func(db *sql.DB) error {
		stmt, err := db.Prepare(`UPDATE core_commits SET signer_key = ? WHERE repo_url = ? AND hash = ? AND branch = ?`)
		if err != nil {
			return err
		}
		defer stmt.Close()
		for _, r := range rows {
			raw, ok := keys[r.hash]
			if !ok || raw == "" {
				// Unsigned: write empty string so we don't re-scan next fetch.
				if _, err := stmt.Exec("", repoURL, r.hash, r.branch); err == nil {
					updated++
				}
				continue
			}
			signer := NormalizeSignerKey(raw)
			if _, err := stmt.Exec(signer, repoURL, r.hash, r.branch); err != nil {
				continue
			}
			updated++
			if r.email != "" {
				candidates = append(candidates, VerifyCandidate{
					RepoURL: repoURL, Hash: r.hash, SignerKey: signer, Email: r.email,
				})
			}
		}
		return nil
	})
	return updated, candidates
}

// VerifyCandidate is one (commit, signer key, email) tuple submitted for
// background verification.
type VerifyCandidate struct {
	RepoURL   string
	Hash      string
	SignerKey string
	Email     string
}

// VerifyCandidates resolves bindings for the given candidates in parallel,
// skipping anything already in the cache. Path failures are non-fatal — they
// produce negative cache entries that the next fetch will retry.
func VerifyCandidates(candidates []VerifyCandidate, parallel int) {
	if len(candidates) == 0 {
		return
	}
	if parallel <= 0 {
		parallel = 4
	}

	seen := make(map[string]bool, len(candidates))
	work := make([]VerifyCandidate, 0, len(candidates))
	for _, c := range candidates {
		if c.SignerKey == "" || c.Email == "" {
			continue
		}
		key := NormalizeSignerKey(c.SignerKey) + "\x00" + NormalizeEmail(c.Email)
		if seen[key] {
			continue
		}
		seen[key] = true
		if existing := LookupBinding(c.SignerKey, c.Email); existing != nil {
			continue
		}
		work = append(work, c)
	}
	if len(work) == 0 {
		return
	}

	log.Debug("identity: resolving bindings", "count", len(work))
	sem := make(chan struct{}, parallel)
	var wg sync.WaitGroup
	for _, c := range work {
		wg.Add(1)
		sem <- struct{}{}
		go func(c VerifyCandidate) {
			defer wg.Done()
			defer func() { <-sem }()
			if _, err := VerifyBinding(c.SignerKey, c.Email, c.RepoURL, c.Hash); err != nil {
				log.Debug("identity: verify binding", "email", c.Email, "error", err)
			}
		}(c)
	}
	wg.Wait()
}

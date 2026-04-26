// verifier.go - Resolves (key, email) bindings against forge or DNS attestations
package identity

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gitsocial-org/gitsocial/core/cache"
	"github.com/gitsocial-org/gitsocial/core/identity/forge"
	"github.com/gitsocial-org/gitsocial/core/log"
)

const (
	bindingPositiveTTL = 24 * time.Hour
	bindingNegativeTTL = 1 * time.Hour
	bindingFetchTime   = 12 * time.Second
)

// Source labels the path that produced a verified binding.
type Source string

const (
	SourceForgeGPG Source = "forge_gpg"
	SourceForgeAPI Source = "forge_api"
	SourceDNS      Source = "dns"
)

// Binding is a verified link between a signing key and an author email.
type Binding struct {
	KeyFingerprint string
	Email          string
	Source         Source
	ForgeHost      string
	ForgeAccount   string
	ResolvedAt     time.Time
	Verified       bool
}

func init() {
	cache.RegisterSchema("identity_bindings", bindingsSchema)
	cache.RegisterMigration(migrateBindingsToPerSource)
}

const bindingsSchema = `
CREATE TABLE IF NOT EXISTS core_verified_bindings (
    key_fingerprint TEXT NOT NULL,
    email TEXT NOT NULL,
    source TEXT NOT NULL,
    forge_host TEXT NOT NULL DEFAULT '',
    forge_account TEXT,
    verified INTEGER NOT NULL,
    resolved_at TEXT NOT NULL,
    PRIMARY KEY (key_fingerprint, email, source, forge_host)
);
CREATE INDEX IF NOT EXISTS idx_core_verified_bindings_email ON core_verified_bindings(email);
`

// migrateBindingsToPerSource drops the table if its PK doesn't include 'source'
// (legacy single-row-per-(key,email) shape) so the new schema can recreate it.
// The cache rebuilds on next fetch — no data preserved.
func migrateBindingsToPerSource(db *sql.DB) {
	var pkHasSource int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('core_verified_bindings') WHERE pk > 0 AND name = 'source'`).Scan(&pkHasSource); err != nil {
		return
	}
	if pkHasSource > 0 {
		return
	}
	_, _ = db.Exec(`DROP TABLE IF EXISTS core_verified_bindings`)
	_, _ = db.Exec(bindingsSchema)
}

// inflight de-duplicates concurrent verification attempts for the same binding.
var inflight sync.Map

// dnsEnabled controls whether the DNS well-known source is consulted and
// whether cached DNS bindings count toward IsVerified/IsVerifiedCommit lookups.
// Default is false — domain attestation is opt-in. Toggled via
// SetDNSVerificationEnabled (called from the settings load path).
var dnsEnabled atomic.Bool

// SetDNSVerificationEnabled toggles whether the DNS well-known source is
// consulted and whether cached DNS bindings satisfy IsVerified lookups.
// The default is false; callers (CLI startup, settings save) call this to
// reflect user preference.
func SetDNSVerificationEnabled(enabled bool) { dnsEnabled.Store(enabled) }

// IsDNSVerificationEnabled returns the current DNS verification policy.
func IsDNSVerificationEnabled() bool { return dnsEnabled.Load() }

// dnsSourceFilter returns the SQL fragment and argument extension used to
// suppress DNS-source rows from binding lookups when DNS verification is
// disabled. Returns ("", nil) when no filter is needed.
func dnsSourceFilter() (string, []interface{}) {
	if dnsEnabled.Load() {
		return "", nil
	}
	return " AND source != ?", []interface{}{string(SourceDNS)}
}

// NormalizeSignerKey canonicalizes a key string: GPG hex IDs are uppercased,
// SSH "SHA256:<base64>" forms are preserved (base64 is case-sensitive).
func NormalizeSignerKey(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if strings.HasPrefix(s, "SHA256:") {
		return s
	}
	return strings.ToUpper(s)
}

// IsVerified returns true when a positive verified binding exists in the cache
// for the given (signer key, email) pair. Empty inputs return false.
func IsVerified(signerKey, email string) bool {
	signerKey = NormalizeSignerKey(signerKey)
	email = NormalizeEmail(email)
	if signerKey == "" || email == "" {
		return false
	}
	dnsFilter, dnsArgs := dnsSourceFilter()
	found, err := cache.QueryLocked(func(db *sql.DB) (bool, error) {
		var verified int
		args := []interface{}{email, signerKey, signerKey, signerKey}
		args = append(args, dnsArgs...)
		err := db.QueryRow(`
			SELECT verified FROM core_verified_bindings
			WHERE email = ? AND verified = 1
			  AND (key_fingerprint = ?
			       OR ? LIKE '%' || key_fingerprint
			       OR key_fingerprint LIKE '%' || ?)`+dnsFilter+`
			LIMIT 1`, args...).Scan(&verified)
		if err != nil {
			return false, err
		}
		return verified == 1, nil
	})
	return err == nil && found
}

// FindUserSignerKey returns the signer_key of the most recent signed commit
// authored by the given email in the given repo, or "" if none. Used by the
// TUI Identity view to look up the user's own binding without guessing the
// fingerprint format from raw git config.
func FindUserSignerKey(repoURL, email string) string {
	if repoURL == "" || email == "" {
		return ""
	}
	email = NormalizeEmail(email)
	key, err := cache.QueryLocked(func(db *sql.DB) (string, error) {
		var sk sql.NullString
		err := db.QueryRow(`
			SELECT signer_key FROM core_commits
			WHERE repo_url = ? AND author_email = ?
			  AND signer_key IS NOT NULL AND signer_key != ''
			ORDER BY timestamp DESC LIMIT 1`, repoURL, email).Scan(&sk)
		if err != nil {
			return "", err
		}
		if !sk.Valid {
			return "", nil
		}
		return sk.String, nil
	})
	if err != nil {
		return ""
	}
	return key
}

// IsVerifiedCommit returns true when the commit's stored signer key is bound
// to the author email via a positive verified record. The single SQL roundtrip
// is suitable for per-card render lookups.
func IsVerifiedCommit(repoURL, hash, email string) bool {
	if repoURL == "" || hash == "" || email == "" {
		return false
	}
	email = NormalizeEmail(email)
	dnsFilter, dnsArgs := dnsSourceFilter()
	// dnsSourceFilter returns "AND source != ?" but here we need to qualify the
	// column with the table alias `b` since core_commits is also in the join.
	dnsFilter = strings.Replace(dnsFilter, " AND source", " AND b.source", 1)
	found, err := cache.QueryLocked(func(db *sql.DB) (bool, error) {
		var n int
		args := []interface{}{email, repoURL, hash}
		// dnsArgs append after the WHERE positions; the filter applies inside the JOIN ON
		// so we need to inject before the WHERE. Restructure:
		query := `
			SELECT 1 FROM core_commits c
			JOIN core_verified_bindings b
			  ON b.email = ?
			 AND b.verified = 1
			 AND (c.signer_key = b.key_fingerprint
			      OR c.signer_key LIKE '%' || b.key_fingerprint
			      OR b.key_fingerprint LIKE '%' || c.signer_key)` + dnsFilter + `
			WHERE c.repo_url = ? AND c.hash = ? AND c.signer_key IS NOT NULL
			LIMIT 1`
		// dnsArgs slot in between b.email and c.repo_url
		if len(dnsArgs) > 0 {
			args = append([]interface{}{email}, dnsArgs...)
			args = append(args, repoURL, hash)
		}
		err := db.QueryRow(query, args...).Scan(&n)
		if err != nil {
			return false, err
		}
		return n == 1, nil
	})
	return err == nil && found
}

// LookupBinding returns the best cached binding for display: any positive
// (preferring forge_api, then forge_gpg, then dns) or the most recent negative
// if no positive exists. Returns nil when no fresh row matches.
func LookupBinding(signerKey, email string) *Binding {
	signerKey = NormalizeSignerKey(signerKey)
	email = NormalizeEmail(email)
	if signerKey == "" || email == "" {
		return nil
	}
	rows := loadFreshBindings(email, signerKey)
	if len(rows) == 0 {
		return nil
	}
	sourceRank := map[Source]int{SourceForgeAPI: 3, SourceForgeGPG: 2, SourceDNS: 1}
	var best *Binding
	for i := range rows {
		r := &rows[i]
		if best == nil {
			best = r
			continue
		}
		if r.Verified && !best.Verified {
			best = r
			continue
		}
		if r.Verified == best.Verified {
			if r.Verified && sourceRank[r.Source] > sourceRank[best.Source] {
				best = r
			} else if !r.Verified && r.ResolvedAt.After(best.ResolvedAt) {
				best = r
			}
		}
	}
	return best
}

// lookupPerSource returns the cached binding for an exact (key, email, source, host)
// tuple if present and fresh, nil otherwise.
func lookupPerSource(signerKey, email string, src Source, host string) *Binding {
	rows := loadFreshBindings(email, signerKey)
	for i := range rows {
		r := &rows[i]
		if r.Source == src && r.ForgeHost == host {
			return r
		}
	}
	return nil
}

// loadFreshBindings returns all non-expired binding rows for an (email, key)
// pair. Match on key uses suffix logic (commit signer keys may be 16-char IDs;
// stored fingerprints may be 40-char) — the caller filters further if needed.
// DNS-source rows are excluded when DNS verification is disabled.
func loadFreshBindings(email, signerKey string) []Binding {
	dnsFilter, dnsArgs := dnsSourceFilter()
	args := make([]interface{}, 0, 4+len(dnsArgs))
	args = append(args, email, signerKey, signerKey, signerKey)
	args = append(args, dnsArgs...)
	rows, err := cache.QueryLocked(func(db *sql.DB) ([]Binding, error) {
		r, err := db.Query(`
			SELECT key_fingerprint, email, source, forge_host, forge_account, verified, resolved_at
			FROM core_verified_bindings
			WHERE email = ?
			  AND (key_fingerprint = ?
			       OR ? LIKE '%' || key_fingerprint
			       OR key_fingerprint LIKE '%' || ?)`+dnsFilter,
			args...)
		if err != nil {
			return nil, err
		}
		defer r.Close()
		var out []Binding
		for r.Next() {
			var fp, em, src, host string
			var account sql.NullString
			var verified int
			var resolvedAt string
			if err := r.Scan(&fp, &em, &src, &host, &account, &verified, &resolvedAt); err != nil {
				continue
			}
			t, _ := time.Parse(time.RFC3339, resolvedAt)
			ttl := bindingPositiveTTL
			if verified == 0 {
				ttl = bindingNegativeTTL
			}
			if !t.IsZero() && time.Since(t) > ttl {
				continue
			}
			b := Binding{
				KeyFingerprint: fp,
				Email:          em,
				Source:         Source(src),
				ForgeHost:      host,
				ResolvedAt:     t,
				Verified:       verified == 1,
			}
			if account.Valid {
				b.ForgeAccount = account.String
			}
			out = append(out, b)
		}
		return out, r.Err()
	})
	if err != nil {
		return nil
	}
	return rows
}

// VerifyBinding resolves a (signerKey, email) pair against each external source
// independently and caches per-source outcomes. Optional repoURL/sha enable the
// forge commits API source. The returned Binding is the strongest result: any
// source affirming the binding wins; if none affirm, a negative summary is
// returned. Negative cache entries from one source never short-circuit attempts
// against other sources.
func VerifyBinding(signerKey, email, repoURL, sha string) (*Binding, error) {
	signerKey = strings.TrimSpace(signerKey)
	email = NormalizeEmail(email)
	if signerKey == "" || email == "" {
		return nil, fmt.Errorf("signer key and email are required")
	}

	if existing := LookupBinding(signerKey, email); existing != nil && existing.Verified {
		return existing, nil
	}

	flightKey := NormalizeSignerKey(signerKey) + "\x00" + email
	if _, loaded := inflight.LoadOrStore(flightKey, struct{}{}); loaded {
		time.Sleep(50 * time.Millisecond)
		if existing := LookupBinding(signerKey, email); existing != nil && existing.Verified {
			return existing, nil
		}
	}
	defer inflight.Delete(flightKey)

	ctx, cancel := context.WithTimeout(context.Background(), bindingFetchTime)
	defer cancel()

	host := hostOf(repoURL)
	type sourceAttempt struct {
		src  Source
		host string
		fn   func() *Binding
	}
	attempts := []sourceAttempt{
		{SourceForgeGPG, host, func() *Binding { return tryForgeGPG(ctx, repoURL, signerKey, email) }},
		{SourceForgeAPI, host, func() *Binding { return tryForgeAPI(ctx, repoURL, sha, signerKey, email) }},
	}
	if dnsEnabled.Load() {
		attempts = append(attempts, sourceAttempt{SourceDNS, "", func() *Binding { return tryDNS(signerKey, email) }})
	}

	var firstPositive *Binding
	for _, a := range attempts {
		if cached := lookupPerSource(signerKey, email, a.src, a.host); cached != nil {
			if cached.Verified && firstPositive == nil {
				firstPositive = cached
			}
			continue
		}
		if b := a.fn(); b != nil {
			storeBinding(b)
			if b.Verified && firstPositive == nil {
				firstPositive = b
			}
			continue
		}
		// Source declined — record per-source negative so we don't re-attempt
		// every render, but other sources are still tried this call.
		storeBinding(&Binding{
			KeyFingerprint: NormalizeSignerKey(signerKey),
			Email:          email,
			Source:         a.src,
			ForgeHost:      a.host,
			ResolvedAt:     time.Now().UTC(),
			Verified:       false,
		})
	}

	if firstPositive != nil {
		return firstPositive, nil
	}
	return &Binding{
		KeyFingerprint: NormalizeSignerKey(signerKey),
		Email:          email,
		ResolvedAt:     time.Now().UTC(),
		Verified:       false,
	}, nil
}

// hostOf extracts the lowercase host from a repo URL, returning "" on failure.
func hostOf(repoURL string) string {
	if repoURL == "" {
		return ""
	}
	host, _, _, err := forge.ParseRepoURL(repoURL)
	if err != nil {
		return ""
	}
	return strings.ToLower(host)
}

// tryForgeGPG attempts the forge GPG endpoint — fetch <user>.gpg, match key+email UID.
func tryForgeGPG(ctx context.Context, repoURL, signerKey, email string) *Binding {
	if repoURL == "" {
		return nil
	}
	f, owner, _, err := forge.LookupForRepo(repoURL)
	if err != nil || f == nil || owner == "" {
		return nil
	}
	keys, err := f.FetchGPGKeys(ctx, owner)
	if err != nil {
		log.Debug("forge GPG fetch", "host", f.Host(), "user", owner, "error", err)
		return nil
	}
	signerNorm := NormalizeSignerKey(signerKey)
	for _, k := range keys {
		if !keyMatchesFingerprint(signerNorm, k) {
			continue
		}
		if isNoreplyEmail(email) || containsEmail(k.Emails, email) {
			return &Binding{
				KeyFingerprint: k.Fingerprint,
				Email:          email,
				Source:         SourceForgeGPG,
				ForgeHost:      f.Host(),
				ForgeAccount:   owner,
				ResolvedAt:     time.Now().UTC(),
				Verified:       true,
			}
		}
	}
	return nil
}

// tryForgeAPI attempts the forge commits API — call the per-commit verification endpoint.
func tryForgeAPI(ctx context.Context, repoURL, sha, signerKey, email string) *Binding {
	if repoURL == "" || sha == "" {
		return nil
	}
	f, owner, repo, err := forge.LookupForRepo(repoURL)
	if err != nil || f == nil || owner == "" || repo == "" {
		return nil
	}
	cv, err := f.FetchCommitVerification(ctx, owner, repo, sha)
	if err != nil {
		log.Debug("forge commits API", "host", f.Host(), "owner", owner, "sha", sha, "error", err)
		return nil
	}
	if !cv.Verified {
		return nil
	}
	if cv.AuthorEmail != "" && cv.AuthorEmail != email {
		return nil
	}
	return &Binding{
		KeyFingerprint: NormalizeSignerKey(signerKey),
		Email:          email,
		Source:         SourceForgeAPI,
		ForgeHost:      f.Host(),
		ForgeAccount:   cv.AccountID,
		ResolvedAt:     time.Now().UTC(),
		Verified:       true,
	}
}

// tryDNS attempts the domain-owner source — fetch the well-known endpoint and match the key.
func tryDNS(signerKey, email string) *Binding {
	resolved, err := ResolveIdentity(email)
	if err != nil {
		return nil
	}
	if !sshKeyMatches(resolved.Key, signerKey) {
		return nil
	}
	return &Binding{
		KeyFingerprint: NormalizeSignerKey(signerKey),
		Email:          email,
		Source:         SourceDNS,
		ResolvedAt:     time.Now().UTC(),
		Verified:       true,
	}
}

// keyMatchesFingerprint returns true when signerKey refers to the GPGKey's
// primary key or any subkey. Commits sign with subkeys, so all are checked.
func keyMatchesFingerprint(signerKeyUpper string, k forge.GPGKey) bool {
	if signerKeyUpper == "" {
		return false
	}
	if matchesFingerprint(signerKeyUpper, k.Fingerprint) {
		return true
	}
	for _, sub := range k.Subkeys {
		if matchesFingerprint(signerKeyUpper, sub) {
			return true
		}
	}
	return false
}

func matchesFingerprint(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	if a == b {
		return true
	}
	if strings.HasSuffix(a, b) || strings.HasSuffix(b, a) {
		return true
	}
	return false
}

// sshKeyMatches compares an SSH public key (algorithm + base64) against a
// commit signer key (typically "SHA256:<base64>"). Falls back to GPG suffix
// match when the DNS document declares a "gpg:<fingerprint>" key.
func sshKeyMatches(declaredKey, signerKey string) bool {
	if declaredKey == "" || signerKey == "" {
		return false
	}
	if strings.HasPrefix(declaredKey, "gpg:") {
		fp := strings.ToUpper(strings.TrimPrefix(declaredKey, "gpg:"))
		return matchesFingerprint(NormalizeSignerKey(signerKey), fp)
	}
	parts := strings.Fields(declaredKey)
	if len(parts) < 2 {
		return false
	}
	raw, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		return false
	}
	digest := sha256.Sum256(raw)
	expected := "SHA256:" + strings.TrimRight(base64.StdEncoding.EncodeToString(digest[:]), "=")
	return signerKey == expected
}

func storeBinding(b *Binding) {
	verified := 0
	if b.Verified {
		verified = 1
	}
	_ = cache.ExecLocked(func(db *sql.DB) error {
		_, err := db.Exec(
			`INSERT OR REPLACE INTO core_verified_bindings (key_fingerprint, email, source, forge_host, forge_account, verified, resolved_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			b.KeyFingerprint, b.Email, string(b.Source),
			b.ForgeHost, nullableString(b.ForgeAccount),
			verified, b.ResolvedAt.UTC().Format(time.RFC3339),
		)
		return err
	})
}

func nullableString(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

// isNoreplyEmail returns true for forge-issued noreply addresses where the
// email is structurally bound to the account namespace.
func isNoreplyEmail(email string) bool {
	return strings.HasSuffix(email, "@users.noreply.github.com")
}

func containsEmail(haystack []string, needle string) bool {
	for _, e := range haystack {
		if e == needle {
			return true
		}
	}
	return false
}

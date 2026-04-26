// verifier_test.go - Tests for verifier helpers and per-source cache semantics
package identity

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"testing"
	"time"

	"github.com/gitsocial-org/gitsocial/core/cache"
	"github.com/gitsocial-org/gitsocial/core/identity/forge"
)

// --- Pure helpers ---

func TestNormalizeSignerKey(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in, want string
	}{
		{"abcdef", "ABCDEF"},
		{"ABCDEF", "ABCDEF"},
		{"  abc  ", "ABC"},
		{"SHA256:base64+/=", "SHA256:base64+/="},
		{"", ""},
	}
	for _, tt := range tests {
		if got := NormalizeSignerKey(tt.in); got != tt.want {
			t.Errorf("NormalizeSignerKey(%q) = %q; want %q", tt.in, got, tt.want)
		}
	}
}

func TestMatchesFingerprint(t *testing.T) {
	t.Parallel()
	tests := []struct {
		a, b string
		want bool
	}{
		{"ABCDEF1234567890", "ABCDEF1234567890", true},
		{"1234567890", "ABCDEF1234567890", true},
		{"ABCDEF1234567890", "1234567890", true},
		{"DEADBEEF", "ABCDEF1234567890", false},
		{"", "ABCDEF", false},
		{"ABCDEF", "", false},
	}
	for _, tt := range tests {
		if got := matchesFingerprint(tt.a, tt.b); got != tt.want {
			t.Errorf("matchesFingerprint(%q, %q) = %v; want %v", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestKeyMatchesFingerprint(t *testing.T) {
	t.Parallel()
	k := forge.GPGKey{
		Fingerprint: "AAAAFFFFFINGERPRINTPRIMARY",
		Subkeys:     []string{"BBBBFFFFFINGERPRINTSUB1", "CCCCFFFFFINGERPRINTSUB2"},
	}
	tests := []struct {
		signer string
		want   bool
	}{
		{"AAAAFFFFFINGERPRINTPRIMARY", true},
		{"FINGERPRINTPRIMARY", true},
		{"BBBBFFFFFINGERPRINTSUB1", true},
		{"FINGERPRINTSUB2", true},
		{"DEADBEEF", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := keyMatchesFingerprint(tt.signer, k); got != tt.want {
			t.Errorf("keyMatchesFingerprint(%q) = %v; want %v", tt.signer, got, tt.want)
		}
	}
}

func TestSshKeyMatches(t *testing.T) {
	t.Parallel()
	// Compute expected SHA256 fingerprint of a known base64 blob
	rawBlob := []byte("test-ssh-key-bytes")
	declared := "ssh-ed25519 " + base64.StdEncoding.EncodeToString(rawBlob)
	digest := sha256.Sum256(rawBlob)
	signerSHA := "SHA256:" + base64.StdEncoding.EncodeToString(digest[:])
	// Trim trailing '=' padding (matches sshKeyMatches behavior)
	for len(signerSHA) > 0 && signerSHA[len(signerSHA)-1] == '=' {
		signerSHA = signerSHA[:len(signerSHA)-1]
	}

	t.Run("matching SSH key", func(t *testing.T) {
		if !sshKeyMatches(declared, signerSHA) {
			t.Errorf("expected match for SSH key + its SHA256")
		}
	})
	t.Run("mismatched SSH key", func(t *testing.T) {
		if sshKeyMatches(declared, "SHA256:wronghash") {
			t.Error("expected no match for unrelated SHA256")
		}
	})
	t.Run("GPG declared key suffix match", func(t *testing.T) {
		if !sshKeyMatches("gpg:1234567890ABCDEF", "ABCDEF") {
			t.Error("expected suffix match against gpg:<fingerprint>")
		}
	})
	t.Run("malformed declared key", func(t *testing.T) {
		if sshKeyMatches("only-one-field", "SHA256:anything") {
			t.Error("expected no match for malformed declared key")
		}
	})
	t.Run("empty inputs", func(t *testing.T) {
		if sshKeyMatches("", "SHA256:x") || sshKeyMatches("ssh-ed25519 abc", "") {
			t.Error("empty inputs should not match")
		}
	})
}

func TestIsNoreplyEmail(t *testing.T) {
	t.Parallel()
	if !isNoreplyEmail("12345+alice@users.noreply.github.com") {
		t.Error("github noreply should match")
	}
	if isNoreplyEmail("alice@example.com") {
		t.Error("regular email should not match")
	}
}

func TestContainsEmail(t *testing.T) {
	t.Parallel()
	hay := []string{"a@x.com", "b@x.com", "c@x.com"}
	if !containsEmail(hay, "b@x.com") {
		t.Error("expected match")
	}
	if containsEmail(hay, "z@x.com") {
		t.Error("unexpected match")
	}
	if containsEmail(nil, "anything") {
		t.Error("nil haystack should not match")
	}
}

func TestHostOf(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in, want string
	}{
		{"https://github.com/alice/foo", "github.com"},
		{"https://Example.COM/alice/foo", "example.com"},
		{"git@gitlab.com:alice/foo", "gitlab.com"},
		{"", ""},
		{"not a url", ""},
	}
	for _, tt := range tests {
		if got := hostOf(tt.in); got != tt.want {
			t.Errorf("hostOf(%q) = %q; want %q", tt.in, got, tt.want)
		}
	}
}

// --- Cache-backed lookups ---

func setupTestCache(t *testing.T) {
	t.Helper()
	cache.Reset()
	if err := cache.Open(t.TempDir()); err != nil {
		t.Fatalf("cache.Open: %v", err)
	}
	t.Cleanup(func() { cache.Reset() })
}

// withDNSEnabled flips the DNS verification flag on for the duration of a test
// and restores the prior value at cleanup. Use when a test needs DNS sources to
// participate in lookups.
func withDNSEnabled(t *testing.T) {
	t.Helper()
	prev := IsDNSVerificationEnabled()
	SetDNSVerificationEnabled(true)
	t.Cleanup(func() { SetDNSVerificationEnabled(prev) })
}

// insertTestBinding writes a binding row directly, bypassing storeBinding for
// precise control over resolved_at (used to test TTL expiry).
func insertTestBinding(t *testing.T, b *Binding) {
	t.Helper()
	verified := 0
	if b.Verified {
		verified = 1
	}
	if err := cache.ExecLocked(func(db *sql.DB) error {
		_, err := db.Exec(
			`INSERT OR REPLACE INTO core_verified_bindings (key_fingerprint, email, source, forge_host, forge_account, verified, resolved_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			b.KeyFingerprint, b.Email, string(b.Source), b.ForgeHost,
			nullableString(b.ForgeAccount), verified, b.ResolvedAt.UTC().Format(time.RFC3339))
		return err
	}); err != nil {
		t.Fatalf("insert binding: %v", err)
	}
}

func TestIsVerified(t *testing.T) {
	setupTestCache(t)
	insertTestBinding(t, &Binding{
		KeyFingerprint: "ABCDEF1234567890",
		Email:          "alice@example.com",
		Source:         SourceForgeGPG,
		ForgeHost:      "github.com",
		ResolvedAt:     time.Now(),
		Verified:       true,
	})

	if !IsVerified("ABCDEF1234567890", "alice@example.com") {
		t.Error("expected verified for full fingerprint match")
	}
	if !IsVerified("1234567890", "alice@example.com") {
		t.Error("expected verified for short suffix match (16-char vs full)")
	}
	if !IsVerified("ABCDEF1234567890", "ALICE@EXAMPLE.COM") {
		t.Error("expected case-insensitive email match")
	}
	if IsVerified("DEADBEEF", "alice@example.com") {
		t.Error("expected not verified for different key")
	}
	if IsVerified("ABCDEF1234567890", "bob@example.com") {
		t.Error("expected not verified for different email")
	}
	if IsVerified("", "alice@example.com") || IsVerified("ABC", "") {
		t.Error("empty inputs should not verify")
	}
}

func TestIsVerified_negativeBindingNotCounted(t *testing.T) {
	setupTestCache(t)
	insertTestBinding(t, &Binding{
		KeyFingerprint: "DEADBEEF",
		Email:          "alice@example.com",
		Source:         SourceForgeGPG,
		ForgeHost:      "github.com",
		ResolvedAt:     time.Now(),
		Verified:       false,
	})
	if IsVerified("DEADBEEF", "alice@example.com") {
		t.Error("negative binding should not satisfy IsVerified")
	}
}

func TestIsVerifiedCommit(t *testing.T) {
	setupTestCache(t)
	const repoURL = "https://github.com/alice/foo"
	const hash = "abc123def456"
	const email = "alice@example.com"
	const fp = "ABCDEFFINGERPRINT"

	// Insert a commit row with a signer key
	if err := cache.InsertCommits([]cache.Commit{{
		Hash: hash, RepoURL: repoURL, Branch: "main",
		AuthorName: "Alice", AuthorEmail: email,
		Message: "test", Timestamp: time.Now(), SignerKey: fp,
	}}); err != nil {
		t.Fatalf("InsertCommits: %v", err)
	}

	// Without a binding: not verified
	if IsVerifiedCommit(repoURL, hash, email) {
		t.Error("expected not verified before binding stored")
	}

	// Add positive binding
	insertTestBinding(t, &Binding{
		KeyFingerprint: fp, Email: email,
		Source: SourceForgeGPG, ForgeHost: "github.com",
		ResolvedAt: time.Now(), Verified: true,
	})

	if !IsVerifiedCommit(repoURL, hash, email) {
		t.Error("expected verified after binding stored")
	}
	if IsVerifiedCommit(repoURL, "wronghash000", email) {
		t.Error("wrong hash should not verify")
	}
	if IsVerifiedCommit(repoURL, hash, "wrong@example.com") {
		t.Error("wrong email should not verify")
	}
}

func TestLookupBinding_PrefersPositiveOverNegative(t *testing.T) {
	setupTestCache(t)
	withDNSEnabled(t)
	const fp = "FINGERPRINT123"
	const email = "alice@example.com"
	insertTestBinding(t, &Binding{
		KeyFingerprint: fp, Email: email,
		Source: SourceForgeGPG, ForgeHost: "github.com",
		ResolvedAt: time.Now(), Verified: false,
	})
	insertTestBinding(t, &Binding{
		KeyFingerprint: fp, Email: email,
		Source: SourceDNS, ForgeHost: "",
		ResolvedAt: time.Now(), Verified: true,
	})
	got := LookupBinding(fp, email)
	if got == nil {
		t.Fatal("expected non-nil binding")
	}
	if !got.Verified {
		t.Errorf("expected positive binding; got source=%s verified=%v", got.Source, got.Verified)
	}
	if got.Source != SourceDNS {
		t.Errorf("expected DNS source for positive; got %s", got.Source)
	}
}

func TestLookupBinding_RanksForgeAPIOverGPGOverDNS(t *testing.T) {
	setupTestCache(t)
	withDNSEnabled(t)
	const fp = "FINGERPRINT456"
	const email = "alice@example.com"
	now := time.Now()
	insertTestBinding(t, &Binding{
		KeyFingerprint: fp, Email: email,
		Source: SourceDNS, ForgeHost: "", ResolvedAt: now, Verified: true,
	})
	insertTestBinding(t, &Binding{
		KeyFingerprint: fp, Email: email,
		Source: SourceForgeGPG, ForgeHost: "github.com", ResolvedAt: now, Verified: true,
	})
	insertTestBinding(t, &Binding{
		KeyFingerprint: fp, Email: email,
		Source: SourceForgeAPI, ForgeHost: "github.com", ResolvedAt: now, Verified: true,
	})
	got := LookupBinding(fp, email)
	if got == nil || got.Source != SourceForgeAPI {
		t.Errorf("expected forge_api; got %+v", got)
	}
}

func TestDNSVerification_DisabledByDefault_ExcludesDNSBindings(t *testing.T) {
	setupTestCache(t)
	// DNS NOT enabled (default false)
	const fp = "DNSGATEFP"
	const email = "alice@example.com"
	insertTestBinding(t, &Binding{
		KeyFingerprint: fp, Email: email,
		Source: SourceDNS, ForgeHost: "",
		ResolvedAt: time.Now(), Verified: true,
	})
	if IsVerified(fp, email) {
		t.Error("DNS-source positive should NOT count when DNS verification is disabled")
	}
	if got := LookupBinding(fp, email); got != nil {
		t.Errorf("LookupBinding should ignore DNS rows when disabled; got %+v", got)
	}
}

func TestDNSVerification_Enabled_IncludesDNSBindings(t *testing.T) {
	setupTestCache(t)
	withDNSEnabled(t)
	const fp = "DNSENABLEDFP"
	const email = "alice@example.com"
	insertTestBinding(t, &Binding{
		KeyFingerprint: fp, Email: email,
		Source: SourceDNS, ForgeHost: "",
		ResolvedAt: time.Now(), Verified: true,
	})
	if !IsVerified(fp, email) {
		t.Error("DNS-source positive should count when DNS verification is enabled")
	}
}

func TestDNSVerification_Disabled_ForgeBindingStillCounts(t *testing.T) {
	setupTestCache(t)
	const fp = "FORGEONLYFP"
	const email = "alice@example.com"
	insertTestBinding(t, &Binding{
		KeyFingerprint: fp, Email: email,
		Source: SourceForgeGPG, ForgeHost: "github.com",
		ResolvedAt: time.Now(), Verified: true,
	})
	if !IsVerified(fp, email) {
		t.Error("forge binding should count regardless of DNS toggle")
	}
}

func TestLookupBinding_TTLExpiry(t *testing.T) {
	setupTestCache(t)
	const fp = "FRESHFINGERPRINT"
	const email = "alice@example.com"
	// Stale positive (older than 24h)
	insertTestBinding(t, &Binding{
		KeyFingerprint: fp, Email: email,
		Source: SourceForgeGPG, ForgeHost: "github.com",
		ResolvedAt: time.Now().Add(-25 * time.Hour), Verified: true,
	})
	if LookupBinding(fp, email) != nil {
		t.Error("expected nil for expired positive binding")
	}

	// Stale negative (older than 1h)
	insertTestBinding(t, &Binding{
		KeyFingerprint: fp, Email: email,
		Source: SourceForgeAPI, ForgeHost: "github.com",
		ResolvedAt: time.Now().Add(-2 * time.Hour), Verified: false,
	})
	if LookupBinding(fp, email) != nil {
		t.Error("expected nil when only expired bindings exist")
	}
}

func TestLookupPerSource_ExactSourceAndHost(t *testing.T) {
	setupTestCache(t)
	const fp = "PERSOURCEFP"
	const email = "alice@example.com"
	insertTestBinding(t, &Binding{
		KeyFingerprint: fp, Email: email,
		Source: SourceForgeGPG, ForgeHost: "github.com",
		ResolvedAt: time.Now(), Verified: true,
	})
	if got := lookupPerSource(fp, email, SourceForgeGPG, "github.com"); got == nil {
		t.Error("expected hit for matching source+host")
	}
	if got := lookupPerSource(fp, email, SourceForgeAPI, "github.com"); got != nil {
		t.Error("different source should not hit")
	}
	if got := lookupPerSource(fp, email, SourceForgeGPG, "gitlab.com"); got != nil {
		t.Error("different host should not hit")
	}
}

// --- Multi-source verification semantics ---

// recordingForge tracks calls and returns canned responses, for testing
// VerifyBinding without real network.
type recordingForge struct {
	host      string
	gpgKeys   []forge.GPGKey
	gpgErr    error
	gpgCalls  int
	verResult *forge.CommitVerification
	verErr    error
	verCalls  int
}

func (r *recordingForge) Host() string { return r.host }
func (r *recordingForge) FetchGPGKeys(ctx context.Context, user string) ([]forge.GPGKey, error) {
	r.gpgCalls++
	return r.gpgKeys, r.gpgErr
}
func (r *recordingForge) FetchCommitVerification(ctx context.Context, owner, repo, sha string) (*forge.CommitVerification, error) {
	r.verCalls++
	return r.verResult, r.verErr
}

func TestVerifyBinding_AnyPositiveSourceWins_FastPath(t *testing.T) {
	setupTestCache(t)
	const fp = "ANYWINSFP"
	const email = "alice@example.com"
	// Pre-cache a positive on forge_api; fast path should return without
	// touching adapters or DNS.
	insertTestBinding(t, &Binding{
		KeyFingerprint: fp, Email: email,
		Source: SourceForgeAPI, ForgeHost: "github.com",
		ResolvedAt: time.Now(), Verified: true,
	})

	b, err := VerifyBinding(fp, email, "https://github.com/alice/foo", "abc123")
	if err != nil {
		t.Fatalf("VerifyBinding: %v", err)
	}
	if !b.Verified || b.Source != SourceForgeAPI {
		t.Errorf("expected forge_api positive; got %+v", b)
	}
}

func TestVerifyBinding_NegativeOnOneSourceDoesNotPoisonOthers(t *testing.T) {
	setupTestCache(t)
	const fp = "NOPOISONFP"
	const email = "alice@example.com"
	const host = "no-poison-test.example.com"

	// Cache a fresh negative on forge_gpg.
	insertTestBinding(t, &Binding{
		KeyFingerprint: fp, Email: email,
		Source: SourceForgeGPG, ForgeHost: host,
		ResolvedAt: time.Now(), Verified: false,
	})

	// Register a forge whose commits API affirms the binding.
	rf := &recordingForge{
		host: host,
		verResult: &forge.CommitVerification{
			Verified:    true,
			AuthorEmail: email,
			AccountID:   "12345",
		},
	}
	forge.Register(rf)

	b, err := VerifyBinding(fp, email, "https://"+host+"/alice/foo", "abc123def456")
	if err != nil {
		t.Fatalf("VerifyBinding: %v", err)
	}
	if !b.Verified {
		t.Errorf("expected verified via forge_api despite forge_gpg negative cache; got %+v", b)
	}
	if b.Source != SourceForgeAPI {
		t.Errorf("expected source=forge_api; got %s", b.Source)
	}

	// Confirm the verifier did NOT call FetchGPGKeys (cached negative honored)
	if rf.gpgCalls != 0 {
		t.Errorf("expected no GPG fetch (cache hit on negative); got %d calls", rf.gpgCalls)
	}
	// Confirm it DID call commits API (no cache for that source yet)
	if rf.verCalls != 1 {
		t.Errorf("expected 1 commits-API call; got %d", rf.verCalls)
	}
}

func TestVerifyBinding_AllSourcesNegative_StoresPerSource(t *testing.T) {
	setupTestCache(t)
	const fp = "ALLNEGFP"
	const email = "alice@example.com"
	const host = "all-neg-test.example.com"

	// Forge that declines on both endpoints.
	rf := &recordingForge{
		host:      host,
		gpgErr:    nil,
		gpgKeys:   nil, // empty → no match → nil binding from tryForgeGPG
		verResult: &forge.CommitVerification{Verified: false},
	}
	forge.Register(rf)

	b, err := VerifyBinding(fp, email, "https://"+host+"/alice/foo", "abc123def456")
	if err != nil {
		t.Fatalf("VerifyBinding: %v", err)
	}
	if b.Verified {
		t.Errorf("expected not verified; got %+v", b)
	}

	// Verify per-source negative bindings were recorded
	if got := lookupPerSource(fp, email, SourceForgeGPG, host); got == nil || got.Verified {
		t.Errorf("expected forge_gpg negative recorded; got %+v", got)
	}
	if got := lookupPerSource(fp, email, SourceForgeAPI, host); got == nil || got.Verified {
		t.Errorf("expected forge_api negative recorded; got %+v", got)
	}
	// DNS attempt may or may not have populated a row depending on whether
	// ResolveIdentity errored; either is acceptable.
}

func TestVerifyBinding_EmptyInputsErr(t *testing.T) {
	setupTestCache(t)
	if _, err := VerifyBinding("", "alice@example.com", "", ""); err == nil {
		t.Error("expected error for empty signer key")
	}
	if _, err := VerifyBinding("ABC", "", "", ""); err == nil {
		t.Error("expected error for empty email")
	}
}

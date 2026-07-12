// refs_verify_test.go - ETag-verified refs read: a plain ref whose refs.json
// claim's "<sha>\n" MD5 matches the listing ETag is resolved without a per-ref
// GET; mismatches, absent manifest entries, and multipart-shaped ETags fall back
// to the GET.

package objstore

import (
	"encoding/json"
	"fmt"
	"testing"
)

// writeManifest publishes a refs.json (refname → sha) exactly as putSiteManifest
// does (plain JSON, no compression).
func writeManifest(t *testing.T, client *Client, claims map[string]string) {
	t.Helper()
	data, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	if err := client.Put(siteManifestKey, data); err != nil {
		t.Fatalf("put manifest: %v", err)
	}
}

// TestRefsVerify_NoPerRefGETOnConsistentManifest: on a bucket whose refs.json
// matches every plain ref, the read does exactly one GET (refs.json) and zero
// per-ref GETs, yet returns the correct refname→sha map.
func TestRefsVerify_NoPerRefGETOnConsistentManifest(t *testing.T) {
	client, bucket := testClient(t)
	want := seedPlainRefs(t, client, 200)
	writeManifest(t, client, want)

	got, err := readRemoteRefs(client, "")
	if err != nil {
		t.Fatalf("readRemoteRefs: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("got %d refs, want %d", len(got), len(want))
	}
	for ref, sha := range want {
		if got[ref] != sha {
			t.Errorf("ref %s: got %q want %q", ref, got[ref], sha)
		}
	}
	if n := bucket.getCount(siteManifestKey); n != 1 {
		t.Errorf("refs.json GETs = %d, want exactly 1", n)
	}
	for ref := range want {
		if n := bucket.getCount(ref); n != 0 {
			t.Errorf("ref %s got %d per-ref GETs; a verified manifest claim needs none", ref, n)
		}
	}
}

// TestRefsVerify_FallsBackOnMismatch: a ref whose manifest claim is wrong (its
// MD5 won't match the real ETag) is resolved by a GET, and the returned value is
// the TRUE bucket value — a wrong manifest never poisons a ref.
func TestRefsVerify_FallsBackOnMismatch(t *testing.T) {
	client, bucket := testClient(t)
	want := seedPlainRefs(t, client, 20)
	claims := map[string]string{}
	for ref, sha := range want {
		claims[ref] = sha
	}
	// Corrupt one claim so it won't verify.
	var bad string
	for ref := range want {
		bad = ref
		break
	}
	claims[bad] = fmt.Sprintf("%040x", 0xdead)
	writeManifest(t, client, claims)

	got, err := readRemoteRefs(client, "")
	if err != nil {
		t.Fatalf("readRemoteRefs: %v", err)
	}
	if got[bad] != want[bad] {
		t.Errorf("ref %s resolved to %q, want the true bucket value %q (manifest must never poison)", bad, got[bad], want[bad])
	}
	if n := bucket.getCount(bad); n != 1 {
		t.Errorf("mismatched ref %s got %d GETs, want 1 (fell back)", bad, n)
	}
	// A ref whose claim was correct still needed no GET.
	for ref := range want {
		if ref == bad {
			continue
		}
		if n := bucket.getCount(ref); n != 0 {
			t.Errorf("verified ref %s got %d GETs, want 0", ref, n)
		}
	}
}

// TestRefsVerify_FallsBackOnMultipartETag: an object whose ETag is multipart-
// shaped ("-<parts>", never a plain MD5) is never treated as verified even if the
// manifest claim is right — the read GETs it.
func TestRefsVerify_FallsBackOnMultipartETag(t *testing.T) {
	client, bucket := testClient(t)
	sha := fmt.Sprintf("%040x", 1)
	// A multipart-style ETag never verifies, regardless of the claimed sha.
	if etagMatchesRef(`"abc123-3"`, sha) {
		t.Fatal("a multipart-shaped ETag must never verify")
	}
	// A non-32-hex ETag never verifies.
	if etagMatchesRef(`"tooShort"`, sha) {
		t.Fatal("a non-32-hex ETag must never verify")
	}
	// And end-to-end: a correct MD5 ETag DOES verify (proving the happy path is
	// really the MD5 check, not an always-false function).
	ref := "refs/heads/main"
	if err := client.Put(ref, []byte(sha+"\n")); err != nil {
		t.Fatalf("put ref: %v", err)
	}
	writeManifest(t, client, map[string]string{ref: sha})
	got, err := readRemoteRefs(client, "")
	if err != nil {
		t.Fatalf("readRemoteRefs: %v", err)
	}
	if got[ref] != sha {
		t.Fatalf("ref %s = %q, want %q", ref, got[ref], sha)
	}
	if n := bucket.getCount(ref); n != 0 {
		t.Errorf("verified ref got %d GETs, want 0", n)
	}
}

// TestRefsVerify_NoManifestFullGET: a bucket with no refs.json (a plain git
// remote) skips the optimization entirely — every plain ref is GET.
func TestRefsVerify_NoManifestFullGET(t *testing.T) {
	client, bucket := testClient(t)
	want := seedPlainRefs(t, client, 30)
	got, err := readRemoteRefs(client, "")
	if err != nil {
		t.Fatalf("readRemoteRefs: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("got %d refs, want %d", len(got), len(want))
	}
	for ref := range want {
		if n := bucket.getCount(ref); n != 1 {
			t.Errorf("ref %s got %d GETs, want 1 (no manifest ⇒ full GET path)", ref, n)
		}
	}
}

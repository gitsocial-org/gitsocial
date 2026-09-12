// list_truncation_test.go - a listing that claims truncation without a token is an error, not a short result.
package objstore

import (
	"strings"
	"testing"
)

// TestList_truncatedWithoutToken: a provider that truncates and offers no continuation token fails the listing.
func TestList_truncatedWithoutToken(t *testing.T) {
	client, bucket := testClient(t)
	if err := client.Put("refs/heads/main", []byte(strings.Repeat("a", 40)+"\n")); err != nil {
		t.Fatalf("seed ref: %v", err)
	}
	bucket.TruncateListings()

	_, err := client.listWithETags("refs/")
	if err == nil {
		t.Fatal("a truncated listing with no continuation token must be an error, not a short result")
	}
	if !strings.Contains(err.Error(), "truncated") {
		t.Errorf("error = %v, want it to name the truncation", err)
	}
}

// TestReadRemoteRefs_surfacesTruncatedListing: the ref read does not quietly drop the refs the short listing omitted.
func TestReadRemoteRefs_surfacesTruncatedListing(t *testing.T) {
	client, bucket := testClient(t)
	if err := client.Put("refs/heads/main", []byte(strings.Repeat("a", 40)+"\n")); err != nil {
		t.Fatalf("seed ref: %v", err)
	}
	bucket.TruncateListings()

	refs, err := ReadRemoteRefs(client, "")
	if err == nil {
		t.Fatalf("ReadRemoteRefs over a truncated listing returned %v, want an error", refs)
	}
	if !strings.Contains(err.Error(), "truncated") {
		t.Errorf("error = %v, want it to name the truncation", err)
	}
}

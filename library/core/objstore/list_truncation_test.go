// list_truncation_test.go - the continuation loop: every page of a long listing is read, and a truncation without a token is an error.
package objstore

import (
	"fmt"
	"strings"
	"testing"
)

// TestList_followsContinuationPages: a listing longer than one page returns every key once, so the continuation loop runs.
func TestList_followsContinuationPages(t *testing.T) {
	client, bucket := testClient(t)
	const total = 1003 // more than the fixture's page size
	want := map[string]bool{}
	for i := 0; i < total; i++ {
		key := fmt.Sprintf("refs/gitmsg/core/forks/%08x", i)
		bucket.Seed(key, []byte(fmt.Sprintf("%040x\n", i+1)))
		want[key] = true
	}

	objs, err := client.listWithETags("refs/")
	if err != nil {
		t.Fatalf("listWithETags: %v", err)
	}
	if len(objs) != total {
		t.Fatalf("listed %d keys, want %d", len(objs), total)
	}
	seen := map[string]bool{}
	for _, obj := range objs {
		if !want[obj.Key] {
			t.Fatalf("listed %s, which was never written", obj.Key)
		}
		if seen[obj.Key] {
			t.Fatalf("key %s listed twice", obj.Key)
		}
		seen[obj.Key] = true
		if obj.ETag == "" {
			t.Fatalf("key %s listed with no ETag", obj.Key)
		}
	}
	if bucket.ListCount() < 2 {
		t.Errorf("the listing took %d requests, want the continuation loop to run", bucket.ListCount())
	}
}

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

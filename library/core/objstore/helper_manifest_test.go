// helper_manifest_test.go - the refs manifest is the browser's only listing of
// the bucket, so a push that fails to write it must not record success.

package objstore

import "testing"

// TestPostPushMaintenance_FailedManifestRetriesNextPush: the skip marker is
// keyed on the refs digest, so stamping it after a failed manifest write makes
// every later push against the same refs skip the whole block, leaving the
// site's only listing describing an older ref set until refs happen to move
// again (indefinitely, on a quiet repo, and silently: a refname the manifest
// omits reads as an empty extension branch). The failed write must therefore
// withhold the marker, which is observable as the next push actually rewriting
// the manifest instead of skipping it.
func TestPostPushMaintenance_FailedManifestRetriesNextPush(t *testing.T) {
	client, bucket := testClient(t)
	seedSiteBucket(t, client)
	// Bring the bucket to the steady state this is about: a fully maintained
	// site whose marker records the current refs. Without this the pass below
	// would be a bootstrap, which withholds the marker for its own reasons and
	// would make the retry prove nothing.
	if err := pushSite(client, "", nil, SiteOverride{}, nil); err != nil {
		t.Fatalf("seed pushSite: %v", err)
	}
	if _, ok := readSitePushState(client, ""); !ok {
		t.Fatal("the seeded bucket must be fully maintained, or a withheld marker is unattributable")
	}

	h := pushHelper(t, client, packTestRepo(t, 2))
	h.override = SiteOverride{Publish: "true", URL: "https://demo.example/"}
	// Move a ref so this push has work: an unchanged digest would skip on the
	// marker alone and never reach the manifest write.
	tip, err := client.Get("refs/heads/main")
	if err != nil {
		t.Fatalf("read seeded tip: %v", err)
	}
	if err := client.Put("refs/heads/feature", tip); err != nil {
		t.Fatalf("move a ref: %v", err)
	}

	bucket.failPut(siteManifestKey)
	h.postPushMaintenance("refs/heads/main", true, nil)
	if bucket.putCount(siteManifestKey) != 1 {
		t.Fatalf("the failing manifest PUT must not have been stored (stored %d, want just the seed's)", bucket.putCount(siteManifestKey))
	}

	// The retry, against the SAME refs. If the failed write had stamped the
	// marker, this pass would report itself up to date and write nothing.
	bucket.clearFailPut(siteManifestKey)
	h.postPushMaintenance("refs/heads/main", true, nil)
	if bucket.putCount(siteManifestKey) < 2 {
		t.Error("the next push must retry the manifest write it lost, not skip on a marker stamped over the failure")
	}
}

// push_state_test.go - the site push-state skip marker: a second push against an
// unchanged bucket skips the whole maintenance pass (no refs GETs, no artifact
// writes); a ref change invalidates the marker; a corrupt marker falls back to a
// full pass.

package objstore

import (
	"testing"
)

// seedSiteBucket lays down a minimal site-enabled bucket: a linear social branch
// (its loose commits + the ref key), a HEAD symref, and the site version marker
// so siteEnabled reports true.
func seedSiteBucket(t *testing.T, client *Client) {
	t.Helper()
	shas := seedChain(t, client, "", "soc", 5)
	tip := shas[len(shas)-1]
	if err := client.Put("refs/heads/gitmsg/social", []byte(tip+"\n")); err != nil {
		t.Fatalf("seed social ref: %v", err)
	}
	if err := client.Put("refs/heads/main", []byte(tip+"\n")); err != nil {
		t.Fatalf("seed main ref: %v", err)
	}
	if err := client.Put("HEAD", []byte("ref: refs/heads/main\n")); err != nil {
		t.Fatalf("seed HEAD: %v", err)
	}
	// The version marker makes siteEnabled true without a full shell upload.
	if err := client.Put(siteVersionKey, []byte("stale-version\n")); err != nil {
		t.Fatalf("seed version marker: %v", err)
	}
}

// TestPushSite_SkipMarker: first push runs the full pass and stamps the marker;
// the second push against the unchanged bucket skips entirely (no refs/ GETs, no
// artifact writes); changing a ref then invalidates the marker so the third push
// runs the full pass again.
func TestPushSite_SkipMarker(t *testing.T) {
	client, bucket := testClient(t)
	seedSiteBucket(t, client)

	// First push: full pass. It writes the refs manifest, configs, items, and the
	// marker last.
	if err := pushSite(client, "", nil, nil); err != nil {
		t.Fatalf("first pushSite: %v", err)
	}
	if _, ok := readSitePushState(client, ""); !ok {
		t.Fatal("first push must leave a push-state marker")
	}
	manifestPuts := bucket.putCount(siteManifestKey)
	if manifestPuts == 0 {
		t.Fatal("first push must have written the refs manifest")
	}

	// Second push against the unchanged bucket: it must skip. Capture the write and
	// per-ref GET counters before, and assert nothing moves.
	putsBefore := bucket.totalPuts()
	socRefGetsBefore := bucket.getCount("refs/heads/gitmsg/social")
	manifestPutsBefore := bucket.putCount(siteManifestKey)
	listsBefore := bucket.listCount()

	if err := pushSite(client, "", nil, nil); err != nil {
		t.Fatalf("second pushSite: %v", err)
	}
	if got := bucket.totalPuts(); got != putsBefore {
		t.Errorf("second push wrote %d objects, want 0 (skip)", got-putsBefore)
	}
	// The skip costs only the cheap digest listing (one refs/ ListObjectsV2 page),
	// never the full per-ref maintenance.
	if got := bucket.listCount() - listsBefore; got > 1 {
		t.Errorf("second push issued %d listings, want at most 1 (digest only)", got)
	}
	if got := bucket.getCount("refs/heads/gitmsg/social"); got != socRefGetsBefore {
		t.Errorf("second push issued %d per-ref GETs, want 0 (skip)", got-socRefGetsBefore)
	}
	if got := bucket.putCount(siteManifestKey); got != manifestPutsBefore {
		t.Errorf("second push rewrote the refs manifest, want a skip")
	}

	// A ref change must invalidate the marker: the third push runs the full pass.
	newShas := seedChain(t, client, "", "soc2", 3)
	newTip := newShas[len(newShas)-1]
	if err := client.Put("refs/heads/gitmsg/social", []byte(newTip+"\n")); err != nil {
		t.Fatalf("advance social ref: %v", err)
	}
	manifestBefore := bucket.putCount(siteManifestKey)
	if err := pushSite(client, "", nil, nil); err != nil {
		t.Fatalf("third pushSite: %v", err)
	}
	if got := bucket.putCount(siteManifestKey); got == manifestBefore {
		t.Error("third push after a ref change must rewrite the refs manifest (marker invalidated)")
	}
}

// TestPushSite_HeadChangeInvalidates: a HEAD symref change alone (the default
// branch switched, no ref/ key moved) invalidates the marker, because the digest
// folds HEAD's etag in (the code view depends on it).
func TestPushSite_HeadChangeInvalidates(t *testing.T) {
	client, _ := testClient(t)
	seedSiteBucket(t, client)
	if err := pushSite(client, "", nil, nil); err != nil {
		t.Fatalf("first pushSite: %v", err)
	}
	if up, _ := siteMaintenanceUpToDate(client, "", mustSiteVersion(t)); !up {
		t.Fatal("unchanged bucket must report up-to-date after a push")
	}
	// Repoint HEAD only; the refs/ listing is untouched.
	if err := client.Put("HEAD", []byte("ref: refs/heads/gitmsg/social\n")); err != nil {
		t.Fatalf("repoint HEAD: %v", err)
	}
	if up, _ := siteMaintenanceUpToDate(client, "", mustSiteVersion(t)); up {
		t.Fatal("a HEAD change must invalidate the marker (digest folds HEAD etag)")
	}
}

// TestPushSite_CorruptMarkerFallsBack: a marker with garbage bytes is treated as
// absent — the next push runs the full pass rather than skipping on a marker it
// cannot parse.
func TestPushSite_CorruptMarkerFallsBack(t *testing.T) {
	client, bucket := testClient(t)
	seedSiteBucket(t, client)
	if err := pushSite(client, "", nil, nil); err != nil {
		t.Fatalf("first pushSite: %v", err)
	}
	// Corrupt the marker.
	if err := client.Put(sitePushStateKey, []byte("{not json")); err != nil {
		t.Fatalf("corrupt marker: %v", err)
	}
	if _, ok := readSitePushState(client, ""); ok {
		t.Fatal("a corrupt marker must read as absent")
	}
	upToDate, _ := siteMaintenanceUpToDate(client, "", mustSiteVersion(t))
	if upToDate {
		t.Fatal("a corrupt marker must never report up-to-date (would skip work wrongly)")
	}
	// The full pass must run and rewrite the marker to a valid one.
	manifestBefore := bucket.putCount(siteManifestKey)
	if err := pushSite(client, "", nil, nil); err != nil {
		t.Fatalf("second pushSite after corruption: %v", err)
	}
	if got := bucket.putCount(siteManifestKey); got == manifestBefore {
		t.Error("push after a corrupt marker must rewrite the refs manifest (full pass)")
	}
	if _, ok := readSitePushState(client, ""); !ok {
		t.Fatal("the recovery push must rewrite a valid marker")
	}
}

// TestSitePushState_RoundTrip: the marker marshals and reads back at the current
// version; a version bump reads as absent.
func TestSitePushState_RoundTrip(t *testing.T) {
	client, _ := testClient(t)
	writeSitePushState(client, "", "shellv1", "digest-abc")
	state, ok := readSitePushState(client, "")
	if !ok {
		t.Fatal("marker must round-trip")
	}
	if state.ShellVersion != "shellv1" || state.RefsDigest != "digest-abc" {
		t.Errorf("marker = %+v, want shellv1/digest-abc", state)
	}
	// An empty digest is never written (can never match a real digest).
	client2, bucket2 := testClient(t)
	writeSitePushState(client2, "", "shellv1", "")
	if bucket2.putCount(sitePushStateKey) != 0 {
		t.Error("an empty digest must not write a marker")
	}
}

// TestSiteMaintenanceUpToDate_ShellVersionBump: a marker at a different shell
// version never reports up-to-date, so a new binary always refreshes.
func TestSiteMaintenanceUpToDate_ShellVersionBump(t *testing.T) {
	client, _ := testClient(t)
	seedSiteBucket(t, client)
	digest, err := refsHeadDigest(client, "")
	if err != nil {
		t.Fatalf("refsHeadDigest: %v", err)
	}
	writeSitePushState(client, "", "old-shell", digest)
	// Same digest but a newer shell version: not up to date.
	if up, _ := siteMaintenanceUpToDate(client, "", "new-shell"); up {
		t.Error("a shell-version mismatch must force a full pass")
	}
	// Matching shell version + digest: up to date.
	if up, _ := siteMaintenanceUpToDate(client, "", "old-shell"); !up {
		t.Error("matching shell version + digest must report up-to-date")
	}
}

// mustSiteVersion returns the embedded shell version or fails the test.
func mustSiteVersion(t *testing.T) string {
	t.Helper()
	v, err := siteVersion()
	if err != nil {
		t.Fatalf("siteVersion: %v", err)
	}
	return v
}

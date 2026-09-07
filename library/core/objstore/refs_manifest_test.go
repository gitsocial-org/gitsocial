// refs_manifest_test.go - the ref manifest follows every ref-moving transfer
// and describes exactly the refs the bucket carries.

package objstore

import (
	"io"
	"maps"
	"testing"

	"github.com/gitsocial-org/gitsocial/library/core/git"
)

// TestPostPushMaintenance_ManifestOnDeferredTransfer: a transfer that defers
// the rest of the maintenance still publishes the manifest from the push's own
// ref view, so a ref the stored document names but the bucket lost is pruned.
func TestPostPushMaintenance_ManifestOnDeferredTransfer(t *testing.T) {
	client, _ := testClient(t)
	want := seedPlainRefs(t, client, 3)
	stale := map[string]string{"refs/heads/gone": shaB}
	for ref, sha := range want {
		stale[ref] = sha
	}
	writeManifest(t, client, stale)
	if err := client.Put(siteVersionKey, []byte("stale-version\n")); err != nil {
		t.Fatal(err)
	}
	t.Setenv(git.DeferMaintenanceEnv, "1")
	h := &remoteHelper{client: client, prefix: ""}
	if err := h.list(io.Discard, true); err != nil {
		t.Fatal(err)
	}
	h.postPushMaintenance("", map[string]string{"refs/heads/main": shaA}, nil)

	got, found := readClaimsDoc(client, bucketRefsKey)
	if !found {
		t.Fatal("deferred transfer must still publish the manifest")
	}
	if _, ok := got["refs/heads/gone"]; ok || len(got) != len(want) {
		t.Errorf("manifest = %v, want exactly the bucket's refs %v", got, want)
	}
	if v, err := client.Get(siteVersionKey); err != nil || string(v) != "stale-version\n" {
		t.Error("deferred transfer must leave the site maintenance to the last one")
	}
}

// TestList_SyncsManifestBeforePush: `list for-push` writes an absent manifest
// and rewrites a stale one from its own listing, so a push that moves nothing
// still leaves an exact one; a fetch-side `list` writes nothing.
func TestList_SyncsManifestBeforePush(t *testing.T) {
	client, _ := testClient(t)
	want := seedPlainRefs(t, client, 2)
	h := &remoteHelper{client: client, prefix: ""}
	if err := h.list(io.Discard, false); err != nil {
		t.Fatal(err)
	}
	if keyExists(client, bucketRefsKey) {
		t.Fatal("a fetch-side list must not write the manifest")
	}
	if err := h.list(io.Discard, true); err != nil {
		t.Fatal(err)
	}
	got, found := readClaimsDoc(client, bucketRefsKey)
	if !found || len(got) != len(want) || h.manifestETag == "" {
		t.Errorf("manifest = %v (found %v, etag %q), want %v", got, found, h.manifestETag, want)
	}
	writeManifest(t, client, map[string]string{"refs/heads/gone": shaB})
	if err := h.list(io.Discard, true); err != nil {
		t.Fatal(err)
	}
	if got, _ := readClaimsDoc(client, bucketRefsKey); len(got) != len(want) || got["refs/heads/gone"] != "" {
		t.Errorf("stale manifest not reconciled: %v", got)
	}
}

// TestPublishRefManifest_ReListsWhenTheDocumentMoved: a manifest another pusher
// wrote after this pusher listed fails the conditional write; the listing that
// follows carries that pusher's ref, and this batch's own updates stay on top,
// even for a pusher whose only listing is the other pusher's manifest.
func TestPublishRefManifest_ReListsWhenTheDocumentMoved(t *testing.T) {
	url := publicNoListBucket(t, map[string]string{
		bucketRefsKey:     `{"refs/heads/main":"` + shaA + `"}`,
		"refs/heads/main": shaA + "\n",
	})
	client, prefix, _, err := clientForRemote(url, HelperEnv{})
	if err != nil {
		t.Fatal(err)
	}
	h := &remoteHelper{client: client, prefix: prefix}
	if err := h.list(io.Discard, true); err != nil {
		t.Fatal(err)
	}
	// This push creates feature; another pusher lands late and its manifest first.
	for key, body := range map[string]string{
		"refs/heads/feature": shaB + "\n",
		"refs/heads/late":    shaStale + "\n",
		bucketRefsKey:        `{"refs/heads/main":"` + shaA + `","refs/heads/late":"` + shaStale + `"}`,
	} {
		if err := putObject(client, prefix, key, []byte(body), ""); err != nil {
			t.Fatal(err)
		}
	}
	h.remoteRefs["refs/heads/feature"] = shaB
	if err := h.publishRefManifest(map[string]string{"refs/heads/feature": shaB}); err != nil {
		t.Fatal(err)
	}
	got, _ := readClaimsDoc(client, prefix+bucketRefsKey)
	want := map[string]string{"refs/heads/main": shaA, "refs/heads/late": shaStale, "refs/heads/feature": shaB}
	if !maps.Equal(got, want) {
		t.Errorf("manifest = %v, want %v", got, want)
	}
}

// TestRebuildRefManifest_ReplacesUnparseableDocument: a manifest a foreign tool
// mangled is overwritten, not mistaken for contention.
func TestRebuildRefManifest_ReplacesUnparseableDocument(t *testing.T) {
	client, bucket := testClient(t)
	want := seedPlainRefs(t, client, 2)
	if err := client.Put(bucketRefsKey, []byte("{not json")); err != nil {
		t.Fatal(err)
	}
	if _, err := rebuildRefManifest(client, "", nil); err != nil {
		t.Fatal(err)
	}
	if got, found := readClaimsDoc(client, bucketRefsKey); !found || len(got) != len(want) {
		t.Errorf("manifest = %v, want %v", got, want)
	}
	if n := bucket.listCount(); n != 1 {
		t.Errorf("listed %d times, want once", n)
	}
}

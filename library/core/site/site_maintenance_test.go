// site_maintenance_test.go - the post-push hook's marker discipline: a pass whose
// transport half lost the ref manifest must not stamp the skip marker.

package site

import (
	"testing"

	"github.com/gitsocial-org/gitsocial/library/core/objstore"
)

// TestPostPushMaintenance_FailedManifestWithholdsMarker: the skip marker is
// keyed on the refs digest, so stamping it after a failed manifest write makes
// every later push against the same refs skip the whole block, leaving the
// site's only listing describing an older ref set until refs happen to move
// again (indefinitely, on a quiet repo: a refname the manifest omits reads as
// an empty extension branch). A pass told the manifest was lost must therefore
// leave the marker alone, so the next pass runs in full.
func TestPostPushMaintenance_FailedManifestWithholdsMarker(t *testing.T) {
	client, _ := testClient(t)
	seedSiteBucket(t, client)
	refs, err := objstore.ReadRemoteRefs(client, "")
	if err != nil {
		t.Fatalf("read seeded refs: %v", err)
	}
	out := objstore.PushOutcome{
		Client:   client,
		Refs:     refs,
		Updates:  map[string]string{"refs/heads/gitmsg/social": refs["refs/heads/gitmsg/social"]},
		Override: objstore.SiteOverride{Publish: "true"},
	}

	PostPushMaintenance(out)
	if _, ok := readSitePushState(client, ""); ok {
		t.Fatal("a pass whose manifest write failed must not stamp the skip marker")
	}

	// The retry, against the same refs, with the manifest written this time.
	out.ManifestOK = true
	PostPushMaintenance(out)
	if _, ok := readSitePushState(client, ""); !ok {
		t.Error("a complete pass must stamp the marker it withheld over the failure")
	}
}

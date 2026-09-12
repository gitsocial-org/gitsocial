// site_read_retry_test.go - the site items walk survives a transient 503 mid-walk and still fails on a fault that never clears.

package site

import (
	"strings"
	"testing"
)

// TestWalk_AbsorbsTransient500MidWalk: the site items walk over a chain survives
// a transient 503 on one commit's object (the production failure mode: a walk
// died at commit 5,697 on a Cloudflare 503). Every commit is still indexed.
func TestWalk_AbsorbsTransient500MidWalk(t *testing.T) {
	client, bucket := testClient(t)
	const n = 8
	shas := seedChain(t, client, "", "", n)
	tip := shas[n-1]
	// Arm a transient fault on a mid-chain commit's object key.
	mid := shas[3]
	bucket.FlakyGet("objects/"+mid[:2]+"/"+mid[2:], 2)

	withTestShardCount(func() {
		withTestWalkBudget(50000, func() {
			sp := &siteProgress{ext: "social"}
			if err := bootstrapItems(client, "", "social", tip, sp); err != nil {
				t.Fatalf("bootstrapItems over a transient mid-walk 503: %v", err)
			}
		})
	})
	assertLockstepState(t, client, "social", shas, tip)
}

// TestWalk_FailsOnPersistentFault: a walk over a commit whose object 500s forever
// still fails (the retry is bounded, not infinite).
func TestWalk_FailsOnPersistentFault(t *testing.T) {
	client, bucket := testClient(t)
	const n = 6
	shas := seedChain(t, client, "", "", n)
	tip := shas[n-1]
	mid := shas[2]
	bucket.FailGet("objects/" + mid[:2] + "/" + mid[2:])

	withTestShardCount(func() {
		withTestWalkBudget(50000, func() {
			sp := &siteProgress{ext: "social"}
			err := bootstrapItems(client, "", "social", tip, sp)
			if err == nil {
				t.Fatal("expected the walk to fail on a fault that never clears")
			}
			if !strings.Contains(err.Error(), mid) {
				t.Errorf("error = %v, want it to name the failing object %s", err, mid)
			}
		})
	})
}

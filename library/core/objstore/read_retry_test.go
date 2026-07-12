// read_retry_test.go - the transient-fault read retry (client.GetRetry /
// withReadRetry) absorbs a flaky 5xx and still fails on a fault that never
// clears, and the site items walk survives a 503 mid-walk.

package objstore

import (
	"errors"
	"strings"
	"testing"
)

// TestGetRetry_AbsorbsTransient500: a key whose first two GETs 500 (then
// succeeds) is read successfully — the retry hides the transient fault.
func TestGetRetry_AbsorbsTransient500(t *testing.T) {
	client, bucket := testClient(t)
	if err := client.Put("k", []byte("value")); err != nil {
		t.Fatalf("put: %v", err)
	}
	bucket.flakyGet("k", 2)
	got, err := client.GetRetry("k")
	if err != nil {
		t.Fatalf("GetRetry over a transient fault: %v", err)
	}
	if string(got) != "value" {
		t.Errorf("GetRetry = %q, want %q", got, "value")
	}
	// One initial attempt + two retries that failed + one that succeeded = 3 GETs
	// reached the bucket (the two 500s plus the success; the very first is one of
	// the two flaky ones).
	if n := bucket.getCount("k"); n != 3 {
		t.Errorf("bucket saw %d GETs, want 3 (2 transient 500s + 1 success)", n)
	}
}

// TestGetRetry_GivesUpOnPersistentFault: a fault that never clears surfaces the
// error after the bounded attempts (it does not retry forever).
func TestGetRetry_GivesUpOnPersistentFault(t *testing.T) {
	client, bucket := testClient(t)
	if err := client.Put("k", []byte("value")); err != nil {
		t.Fatalf("put: %v", err)
	}
	bucket.failGet("k")
	_, err := client.GetRetry("k")
	if err == nil {
		t.Fatal("expected an error from a fault that never clears")
	}
	// 1 initial + len(retryBackoff) retries = total attempts, all reaching the bucket.
	if n := bucket.getCount("k"); n != len(retryBackoff)+1 {
		t.Errorf("bucket saw %d GETs, want %d (initial + %d retries)", n, len(retryBackoff)+1, len(retryBackoff))
	}
}

// TestGetRetry_NoRetryOn404: a definite answer (404) is not retried — one GET.
func TestGetRetry_NoRetryOn404(t *testing.T) {
	client, bucket := testClient(t)
	_, err := client.GetRetry("absent")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetRetry on absent key = %v, want ErrNotFound", err)
	}
	if n := bucket.getCount("absent"); n != 1 {
		t.Errorf("bucket saw %d GETs for a 404, want 1 (a 404 is a definite answer, never retried)", n)
	}
}

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
	bucket.flakyGet("objects/"+mid[:2]+"/"+mid[2:], 2)

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
	bucket.failGet("objects/" + mid[:2] + "/" + mid[2:])

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

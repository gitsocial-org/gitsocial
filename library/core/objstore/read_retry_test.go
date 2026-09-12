// read_retry_test.go - the transient-fault read retry (client.GetRetry and
// withReadRetry) absorbs a flaky 5xx and still fails on a fault that never clears.

package objstore

import (
	"errors"
	"testing"
)

// TestGetRetry_AbsorbsTransient500: a key whose first two GETs 500 (then
// succeeds) is read successfully — the retry hides the transient fault.
func TestGetRetry_AbsorbsTransient500(t *testing.T) {
	client, bucket := testClient(t)
	if err := client.Put("k", []byte("value")); err != nil {
		t.Fatalf("put: %v", err)
	}
	bucket.FlakyGet("k", 2)
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
	if n := bucket.GetCount("k"); n != 3 {
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
	bucket.FailGet("k")
	_, err := client.GetRetry("k")
	if err == nil {
		t.Fatal("expected an error from a fault that never clears")
	}
	// 1 initial + len(retryBackoff) retries = total attempts, all reaching the bucket.
	if n := bucket.GetCount("k"); n != len(retryBackoff)+1 {
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
	if n := bucket.GetCount("absent"); n != 1 {
		t.Errorf("bucket saw %d GETs for a 404, want 1 (a 404 is a definite answer, never retried)", n)
	}
}

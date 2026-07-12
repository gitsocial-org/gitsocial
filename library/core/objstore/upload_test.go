// upload_test.go - concurrent object-upload pool tests (uploadEncodedObjects).
package objstore

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Shrink the retry backoff so the pool tests exercise retries without real waits
// (shared by the PUT pool and the read-retry paths).
func init() {
	retryBackoff = []time.Duration{time.Millisecond, time.Millisecond}
}

// feedObjects returns a producer that emits n synthetic objects (distinct
// shas 0001…, distinct bytes), honoring ctx cancellation.
func feedObjects(n int) (func(context.Context, chan<- encodedObject) error, []string) {
	shas := make([]string, n)
	for i := 0; i < n; i++ {
		shas[i] = fmt.Sprintf("%040x", i+1)
	}
	produce := func(ctx context.Context, out chan<- encodedObject) error {
		for i, sha := range shas {
			obj := encodedObject{sha: sha, compressed: []byte(fmt.Sprintf("body-%d", i))}
			select {
			case out <- obj:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	}
	return produce, shas
}

// TestUploadEncodedObjects_AllLand: a pool wider than one uploads every object
// exactly once to its content-addressed key.
func TestUploadEncodedObjects_AllLand(t *testing.T) {
	client, bucket := testClient(t)
	const n = 300
	produce, shas := feedObjects(n)
	if err := uploadEncodedObjects(client, "repo/", 8, len(shas), nil, produce); err != nil {
		t.Fatalf("uploadEncodedObjects: %v", err)
	}
	for _, sha := range shas {
		key := "repo/objects/" + sha[:2] + "/" + sha[2:]
		if got := bucket.putCount(key); got != 1 {
			t.Errorf("object %s: put count = %d, want 1", sha, got)
		}
	}
}

// TestUploadEncodedObjects_TransientErrorRetries: a PUT that fails twice and
// then succeeds must not fail the push — the retry absorbs it and every
// object still lands.
func TestUploadEncodedObjects_TransientErrorRetries(t *testing.T) {
	client, bucket := testClient(t)
	const n = 100
	produce, shas := feedObjects(n)
	flaky := shas[41]
	flakyKey := "repo/objects/" + flaky[:2] + "/" + flaky[2:]
	bucket.flakyPut(flakyKey, 2)

	if err := uploadEncodedObjects(client, "repo/", 8, len(shas), nil, produce); err != nil {
		t.Fatalf("uploadEncodedObjects with transient failure: %v", err)
	}
	for _, sha := range shas {
		key := "repo/objects/" + sha[:2] + "/" + sha[2:]
		want := 1
		if key == flakyKey {
			want = 1 // successful store count; the two 500s never stored
		}
		if got := bucket.putCount(key); got != want {
			t.Errorf("object %s: put count = %d, want %d", sha, got, want)
		}
	}
}

// TestUploadEncodedObjects_MidTransferError: a failing PUT partway through must
// surface as a wrapped error and cancel the pool so the producer stops.
func TestUploadEncodedObjects_MidTransferError(t *testing.T) {
	client, bucket := testClient(t)
	// Poison one key so its PUT 500s; the pool must fail the whole push.
	bad := fmt.Sprintf("%040x", 42)
	badKey := "repo/objects/" + bad[:2] + "/" + bad[2:]
	bucket.failPut(badKey)

	var produced int64
	produce := func(ctx context.Context, out chan<- encodedObject) error {
		for i := 0; i < 1000; i++ {
			sha := fmt.Sprintf("%040x", i+1)
			select {
			case out <- encodedObject{sha: sha, compressed: []byte("x")}:
				atomic.AddInt64(&produced, 1)
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	}
	err := uploadEncodedObjects(client, "repo/", 4, 1000, nil, produce)
	if err == nil {
		t.Fatal("expected a wrapped upload error, got nil")
	}
	if !strings.Contains(err.Error(), "upload object "+bad) {
		t.Errorf("error = %v, want it to name the failed object %s", err, bad)
	}
	// Cancellation must stop the producer well before all 1000 objects, proving
	// one failed PUT doesn't let the rest keep streaming.
	if p := atomic.LoadInt64(&produced); p >= 1000 {
		t.Errorf("producer emitted all %d objects despite mid-transfer failure; cancellation didn't propagate", p)
	}
}

// TestUploadEncodedObjects_ProducerError: an error from the producer (e.g. a
// cat-file read failure) fails the push and is returned verbatim.
func TestUploadEncodedObjects_ProducerError(t *testing.T) {
	client, _ := testClient(t)
	sentinel := fmt.Errorf("cat-file boom")
	produce := func(ctx context.Context, out chan<- encodedObject) error {
		return sentinel
	}
	if err := uploadEncodedObjects(client, "repo/", 8, 0, nil, produce); err == nil || !strings.Contains(err.Error(), "cat-file boom") {
		t.Fatalf("uploadEncodedObjects err = %v, want the producer error", err)
	}
}

// TestResolveUploadConcurrency_EnvWins: the env var overrides any setting/default.
func TestResolveUploadConcurrency_EnvWins(t *testing.T) {
	t.Setenv("GITSOCIAL_S3_CONCURRENCY", "7")
	if got := resolveUploadConcurrency(); got != 7 {
		t.Errorf("resolveUploadConcurrency() = %d, want 7 (env)", got)
	}
	// A garbage env value falls through to the default (no personal repo here).
	t.Setenv("GITSOCIAL_S3_CONCURRENCY", "nope")
	t.Setenv("GITSOCIAL_PERSONAL_REPO", t.TempDir()) // empty dir, no config
	if got := resolveUploadConcurrency(); got != defaultUploadConcurrency {
		t.Errorf("resolveUploadConcurrency() = %d, want default %d", got, defaultUploadConcurrency)
	}
}

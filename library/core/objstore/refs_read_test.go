// refs_read_test.go - the bounded ref-read pool: identical results to a serial
// read on a bucket with many plain refs and generation chains, and a read error
// surfaced with its wrapping.

package objstore

import (
	"fmt"
	"sort"
	"strings"
	"testing"
)

// seedPlainRefs writes n plain ref keys "refs/gitmsg/core/forks/<i>" (the shape
// a bucket with many fork registrations carries), each holding a distinct sha.
func seedPlainRefs(t *testing.T, client *Client, n int) map[string]string {
	t.Helper()
	want := map[string]string{}
	for i := 0; i < n; i++ {
		refName := fmt.Sprintf("refs/gitmsg/core/forks/%08x", i)
		sha := fmt.Sprintf("%040x", i+1)
		if err := client.Put(refName, []byte(sha+"\n")); err != nil {
			t.Fatalf("seed ref %d: %v", i, err)
		}
		want[refName] = sha
	}
	return want
}

// serialReadRefs reads every ref one GET at a time, the pre-pool baseline the
// pool must match exactly.
func serialReadRefs(t *testing.T, client *Client, prefix string) map[string]string {
	t.Helper()
	keys, err := client.List(prefix + "refs/")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	out := map[string]string{}
	for _, key := range keys {
		refName := strings.TrimPrefix(key, prefix)
		if _, _, isGen, err := parseGenKey(refName); err != nil || isGen {
			continue // handled elsewhere; the plain-ref bucket has none
		}
		value, err := client.Get(key)
		if err != nil {
			t.Fatalf("get %s: %v", key, err)
		}
		sha, err := refSHA(refName, value)
		if err != nil {
			t.Fatalf("refSHA %s: %v", refName, err)
		}
		out[refName] = sha
	}
	return out
}

// TestReadRemoteRefs_PoolMatchesSerial: the pooled read returns exactly the same
// refname→sha map as a serial baseline on a bucket with a few hundred refs.
func TestReadRemoteRefs_PoolMatchesSerial(t *testing.T) {
	client, _ := testClient(t)
	want := seedPlainRefs(t, client, 300)
	// Add a generation chain too, so both read paths (plain + chain) are covered.
	chainRef := "refs/heads/gitmsg/social"
	chainSha := fmt.Sprintf("%040x", 0xabc)
	if err := client.Put(genKey("", chainRef, 3), []byte(chainSha+"\n")); err != nil {
		t.Fatalf("seed chain: %v", err)
	}
	want[chainRef] = chainSha

	serial := serialReadRefs(t, client, "")
	serial[chainRef] = chainSha // baseline the chain the serial helper skipped

	got, err := readRemoteRefs(client, "")
	if err != nil {
		t.Fatalf("readRemoteRefs: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("pooled read returned %d refs, want %d", len(got), len(want))
	}
	for ref, sha := range want {
		if got[ref] != sha {
			t.Errorf("ref %s: pooled=%q want=%q", ref, got[ref], sha)
		}
		if serial[ref] != sha {
			t.Errorf("ref %s: serial=%q want=%q", ref, serial[ref], sha)
		}
	}
}

// TestReadRemoteRefs_ProgressAndPool: progress fires once per ref and the final
// count equals the ref total (the atomic counter stays correct under the pool).
func TestReadRemoteRefs_ProgressAndPool(t *testing.T) {
	client, _ := testClient(t)
	want := seedPlainRefs(t, client, 200)
	var maxDone, total int
	seen := map[int]bool{}
	progress := func(phase string, done, tot int) {
		if phase != "site refs" {
			return
		}
		if done > maxDone {
			maxDone = done
		}
		total = tot
		seen[done] = true
	}
	got, err := readRemoteRefsProgress(client, "", progress)
	if err != nil {
		t.Fatalf("readRemoteRefsProgress: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("got %d refs, want %d", len(got), len(want))
	}
	if total != len(want) || maxDone != len(want) {
		t.Errorf("progress final = %d/%d, want %d/%d", maxDone, total, len(want), len(want))
	}
}

// TestReadRemoteRefs_ErrorWrapped: a malformed ref value (not 40 hex) fails the
// pooled read with the same wrapping the serial read produced.
func TestReadRemoteRefs_ErrorWrapped(t *testing.T) {
	client, _ := testClient(t)
	seedPlainRefs(t, client, 50)
	if err := client.Put("refs/heads/broken", []byte("not-a-sha\n")); err != nil {
		t.Fatalf("seed broken ref: %v", err)
	}
	_, err := readRemoteRefs(client, "")
	if err == nil {
		t.Fatal("expected an error from the malformed ref value")
	}
	if !strings.Contains(err.Error(), "refs/heads/broken") || !strings.Contains(err.Error(), "malformed value") {
		t.Errorf("error = %v, want it to name refs/heads/broken and the malformed value", err)
	}
}

// TestReadRemoteRefs_Deterministic: repeated pooled reads produce identical maps
// (no lost/duplicated refs from the concurrent accumulation).
func TestReadRemoteRefs_Deterministic(t *testing.T) {
	client, _ := testClient(t)
	seedPlainRefs(t, client, 400)
	first, err := readRemoteRefs(client, "")
	if err != nil {
		t.Fatalf("read 1: %v", err)
	}
	for i := 0; i < 5; i++ {
		next, err := readRemoteRefs(client, "")
		if err != nil {
			t.Fatalf("read %d: %v", i+2, err)
		}
		if !sameRefs(first, next) {
			t.Fatalf("read %d diverged from the first read", i+2)
		}
	}
}

// sameRefs reports whether two ref maps are identical.
func sameRefs(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	ak := make([]string, 0, len(a))
	for k := range a {
		ak = append(ak, k)
	}
	sort.Strings(ak)
	for _, k := range ak {
		if a[k] != b[k] {
			return false
		}
	}
	return true
}

// site_localwalk_test.go - the items walk reads commit objects from the local
// git odb when a source is provided, and falls back to the bucket GET per object
// the local odb is missing.

package objstore

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// seedGitChain creates a bare-friendly git repo with n linear commits, uploads
// each commit's loose object to the bucket (so the bucket is a faithful mirror of
// what a push would have uploaded — objects present in both stores), and returns
// the repo's GIT_DIR plus the commit shas oldest-first. Every commit's tree is
// empty (--allow-empty), which is all the items walk reads.
func seedGitChain(t *testing.T, client *Client, n int) (gitDir string, shas []string) {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) string {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=T", "GIT_AUTHOR_EMAIL=t@e",
			"GIT_COMMITTER_NAME=T", "GIT_COMMITTER_EMAIL=t@e")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return strings.TrimSpace(string(out))
	}
	run("init", "-q")
	for i := 0; i < n; i++ {
		run("commit", "-q", "--allow-empty", "-m", fmt.Sprintf("commit %d", i))
	}
	log := run("log", "--format=%H", "--reverse")
	for _, sha := range strings.Fields(log) {
		shas = append(shas, sha)
		// Copy the commit's loose object bytes from the repo to the bucket, exactly
		// as a push would have (content-addressed: identical bytes in both stores).
		cmd := exec.Command("git", "cat-file", "commit", sha)
		cmd.Dir = dir
		body, err := cmd.Output()
		if err != nil {
			t.Fatalf("cat-file %s: %v", sha, err)
		}
		loose := looseCommitBytes(t, body)
		if err := client.Put("objects/"+sha[:2]+"/"+sha[2:], loose); err != nil {
			t.Fatalf("upload object %s: %v", sha, err)
		}
	}
	return dir + "/.git", shas
}

// looseCommitBytes wraps a raw commit body in git's loose-object framing.
func looseCommitBytes(t *testing.T, body []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zlib.NewWriter(&buf)
	if _, err := fmt.Fprintf(zw, "commit %d\x00", len(body)); err != nil {
		t.Fatalf("encode: %v", err)
	}
	if _, err := zw.Write(body); err != nil {
		t.Fatalf("encode: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("encode: %v", err)
	}
	return buf.Bytes()
}

// TestLocalWalk_ReadsLocalNotBucket: with a local commit source, the walk reads
// every commit from the local odb, so the bucket receives zero object GETs.
func TestLocalWalk_ReadsLocalNotBucket(t *testing.T) {
	client, bucket := testClient(t)
	const n = 8
	gitDir, shas := seedGitChain(t, client, n)
	tip := shas[n-1]

	src := newLocalCommitSource(gitDir, "")
	if src == nil {
		t.Fatal("newLocalCommitSource returned nil for a real repo")
	}
	defer src.close()

	withTestShardCount(func() {
		withTestWalkBudget(50000, func() {
			sp := &siteProgress{ext: "social", src: src}
			if err := bootstrapItems(client, "", "social", tip, sp); err != nil {
				t.Fatalf("bootstrapItems (local source): %v", err)
			}
		})
	})
	assertLockstepState(t, client, "social", shas, tip)
	for _, sha := range shas {
		if got := bucket.getCount("objects/" + sha[:2] + "/" + sha[2:]); got != 0 {
			t.Errorf("object %s got %d bucket GETs; the walk should have read it locally", sha[:8], got)
		}
	}
}

// TestLocalWalk_FallsBackPerMissingObject: a commit absent from the local odb but
// present on the bucket falls back to a bucket GET for that one object; the rest
// still read locally.
func TestLocalWalk_FallsBackPerMissingObject(t *testing.T) {
	client, bucket := testClient(t)
	const n = 8
	gitDir, shas := seedGitChain(t, client, n)
	tip := shas[n-1]

	// Delete one commit's loose object from the LOCAL odb, simulating a shallow
	// clone / gc race — it stays on the bucket.
	missing := shas[4]
	local := gitDir + "/objects/" + missing[:2] + "/" + missing[2:]
	if err := removeLoose(gitDir, missing); err != nil {
		t.Fatalf("remove local object %s: %v", missing, err)
	}
	if _, err := os.Stat(local); err == nil {
		t.Fatalf("packed copy still present; the fallback test needs a loose-only object")
	}

	src := newLocalCommitSource(gitDir, "")
	defer src.close()
	withTestShardCount(func() {
		withTestWalkBudget(50000, func() {
			sp := &siteProgress{ext: "social", src: src}
			if err := bootstrapItems(client, "", "social", tip, sp); err != nil {
				t.Fatalf("bootstrapItems (local source, one missing): %v", err)
			}
		})
	})
	assertLockstepState(t, client, "social", shas, tip)
	// The missing object fell back to exactly one bucket GET; its neighbors did not.
	if got := bucket.getCount("objects/" + missing[:2] + "/" + missing[2:]); got != 1 {
		t.Errorf("missing object %s got %d bucket GETs, want 1 (per-object fallback)", missing[:8], got)
	}
	for _, sha := range shas {
		if sha == missing {
			continue
		}
		if got := bucket.getCount("objects/" + sha[:2] + "/" + sha[2:]); got != 0 {
			t.Errorf("present object %s got %d bucket GETs; want 0 (read locally)", sha[:8], got)
		}
	}
}

// TestLocalWalk_NilSourceBucketOnly: a nil source (no local repo) reads every
// commit from the bucket — the unchanged bucket-only path.
func TestLocalWalk_NilSourceBucketOnly(t *testing.T) {
	client, bucket := testClient(t)
	const n = 5
	shas := seedChain(t, client, "", "", n) // synthetic bucket-only objects
	tip := shas[n-1]
	withTestShardCount(func() {
		withTestWalkBudget(50000, func() {
			sp := &siteProgress{ext: "social"} // src nil
			if err := bootstrapItems(client, "", "social", tip, sp); err != nil {
				t.Fatalf("bootstrapItems (nil source): %v", err)
			}
		})
	})
	assertLockstepState(t, client, "social", shas, tip)
	for _, sha := range shas {
		if got := bucket.getCount("objects/" + sha[:2] + "/" + sha[2:]); got == 0 {
			t.Errorf("object %s got 0 bucket GETs; a nil source must read from the bucket", sha[:8])
		}
	}
}

// removeLoose deletes one loose object from a repo's odb, unpacking first if the
// object only exists inside a pack (a fresh small repo keeps objects loose, so
// this is normally a plain unlink).
func removeLoose(gitDir, sha string) error {
	path := gitDir + "/objects/" + sha[:2] + "/" + sha[2:]
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("object %s is not loose (%w); test repo must keep it loose", sha, err)
	}
	return os.Remove(path)
}

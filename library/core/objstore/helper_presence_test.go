// helper_presence_test.go - the fetch walk's presence semantics, the rule the
// thin-fork read overlay rests on: an object a BUCKET pack carries is never
// "present" (the pack was pulled whole and need not close over its references,
// so the walk must descend through it), while an object the local odb holds
// that no bucket pack carries IS present (it arrived through a real git
// transport, which guarantees closure, so the walk stops).
package objstore

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// packedTestRepo builds a repo whose single commit (commit + tree + blob) lives
// entirely in a packfile, and returns its GIT_DIR plus the three object shas.
func packedTestRepo(t *testing.T) (gitDir string, commit, tree, blob string) {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) string {
		t.Helper()
		out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
		if err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
		return strings.TrimSpace(string(out))
	}
	run("init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("presence\n"), 0644); err != nil {
		t.Fatal(err)
	}
	run("add", "a.txt")
	run("-c", "user.email=t@test", "-c", "user.name=T", "commit", "-qm", "one")
	// Pack everything and drop the loose copies, so only the odb probe can see them.
	run("repack", "-a", "-d", "-q")
	commit = run("rev-parse", "HEAD")
	tree = run("rev-parse", "HEAD^{tree}")
	blob = run("rev-parse", "HEAD:a.txt")
	return filepath.Join(dir, ".git"), commit, tree, blob
}

// TestEnsureObject_bucketPackDescends: an object listed in the bucket's packs
// comes back with a body and present=false, so walkObject descends into its
// children — the closure rule a blanket "in the odb ⇒ present" would break.
func TestEnsureObject_bucketPackDescends(t *testing.T) {
	gitDir, commit, tree, blob := packedTestRepo(t)
	h := &remoteHelper{
		gitDir:      gitDir,
		fetched:     map[string]bool{},
		packObjects: map[string]bool{commit: true, tree: true, blob: true},
	}
	defer h.local.close()

	objType, body, present, err := h.ensureObject(commit)
	if err != nil {
		t.Fatalf("ensureObject: %v", err)
	}
	if present {
		t.Error("a bucket-pack object must NOT be present (the walk has to descend through it)")
	}
	if objType != "commit" || len(body) == 0 {
		t.Errorf("bucket-pack object = (%q, %d bytes), want a commit body", objType, len(body))
	}
	if err := h.walkObject(commit); err != nil {
		t.Fatalf("walkObject: %v", err)
	}
	for _, sha := range []string{tree, blob} {
		if !h.fetched[sha] {
			t.Errorf("walk stopped at the bucket-pack commit; %s was never visited", sha[:8])
		}
	}
}

// TestEnsureObject_emptyTreeNotPresent: git synthesizes the empty tree in every
// repo, so the odb probe must skip it — otherwise the walk never downloads the
// bucket's copy and a clone's `git fsck` reports it missing (every gitmsg data
// commit carries it).
func TestEnsureObject_emptyTreeNotPresent(t *testing.T) {
	gitDir, _, _, _ := packedTestRepo(t)
	client, bucket := testClient(t)
	encoded, err := encodeLooseObject("tree", nil)
	if err != nil {
		t.Fatal(err)
	}
	bucket.objs["objects/"+emptyTreeSHA[:2]+"/"+emptyTreeSHA[2:]] = memObject{body: encoded}

	h := &remoteHelper{gitDir: gitDir, client: client, fetched: map[string]bool{}}
	defer h.local.close()
	objType, _, present, err := h.ensureObject(emptyTreeSHA)
	if err != nil {
		t.Fatalf("ensureObject(empty tree): %v", err)
	}
	if present {
		t.Error("the empty tree must never read as present: git answers for it without holding a copy")
	}
	if objType != "tree" {
		t.Errorf("empty tree downloaded as %q, want tree", objType)
	}
	if _, statErr := os.Stat(filepath.Join(gitDir, "objects", emptyTreeSHA[:2], emptyTreeSHA[2:])); statErr != nil {
		t.Errorf("empty tree not written to the odb: %v", statErr)
	}
}

// TestEnsureObject_localOdbPresent: an object the local odb holds that no bucket
// pack carries (it arrived via a real transport — the upstream overlay) counts as
// present, so the walk stops there and never GETs the bucket for it.
func TestEnsureObject_localOdbPresent(t *testing.T) {
	gitDir, commit, tree, blob := packedTestRepo(t)
	h := &remoteHelper{gitDir: gitDir, fetched: map[string]bool{}}
	defer h.local.close()

	objType, body, present, err := h.ensureObject(commit)
	if err != nil {
		t.Fatalf("ensureObject: %v", err)
	}
	if !present {
		t.Fatal("an object in the local odb but in no bucket pack must be present (real transports close over their graph)")
	}
	if objType != "" || body != nil {
		t.Errorf("present object returned (%q, %d bytes), want no body", objType, len(body))
	}
	if err := h.walkObject(commit); err != nil {
		t.Fatalf("walkObject: %v", err)
	}
	for _, sha := range []string{tree, blob} {
		if h.fetched[sha] {
			t.Errorf("walk descended past a present object into %s; it must stop", sha[:8])
		}
	}
}

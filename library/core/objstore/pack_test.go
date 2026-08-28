// pack_test.go - the packed push path: the commit/content type split, the
// threshold that keeps small pushes loose, the objects/info/packs listing, the
// pack map's byte ranges, the fetch walk against a pack-only bucket, the sealing
// round that packs an all-loose bucket and deletes the loose copies after its
// grace period, and the multi-writer hazards of a bucket several clones push to.

package objstore

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// gitRun runs a git command in dir with a hermetic environment (no user config,
// fixed identity) so commits are reproducible across machines. GIT_DIR is
// stripped: the helper under test exports it for its own git invocations, and
// it would otherwise redirect these commands away from dir.
func gitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var env []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "GIT_DIR=") && !strings.HasPrefix(kv, "GIT_WORK_TREE=") {
			env = append(env, kv)
		}
	}
	cmd.Env = append(env,
		"GIT_CONFIG_NOSYSTEM=1", "HOME="+t.TempDir(),
		"GIT_AUTHOR_NAME=T", "GIT_AUTHOR_EMAIL=t@t.com",
		"GIT_COMMITTER_NAME=T", "GIT_COMMITTER_EMAIL=t@t.com",
		"GIT_AUTHOR_DATE=2026-01-01T00:00:00Z", "GIT_COMMITTER_DATE=2026-01-01T00:00:00Z")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// packTestRepo builds a repo with n commits, each adding a distinct file, and
// returns its directory. Every commit therefore contributes a commit, a tree,
// and a blob, so the type split has all three to sort.
func packTestRepo(t *testing.T, n int) string {
	t.Helper()
	dir := t.TempDir()
	gitRun(t, dir, "init", "-q", "-b", "main")
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("file-%02d.txt", i)
		if err := os.WriteFile(filepath.Join(dir, name), []byte(fmt.Sprintf("contents %d\n", i)), 0644); err != nil {
			t.Fatal(err)
		}
		gitRun(t, dir, "add", name)
		gitRun(t, dir, "commit", "-qm", fmt.Sprintf("commit %d", i))
	}
	return dir
}

// pushHelper returns a helper bound to a repo's GIT_DIR and the given bucket,
// with GIT_DIR exported so the helper's own git invocations find the repo.
func pushHelper(t *testing.T, client *Client, dir string) *remoteHelper {
	t.Helper()
	gitDir := filepath.Join(dir, ".git")
	t.Setenv("GIT_DIR", gitDir)
	return &remoteHelper{client: client, gitDir: gitDir, fetched: map[string]bool{}}
}

// pushCmds turns refnames into the push batch a plain `git push <refs>` sends
// (src and dst are the same ref, which is what git generates for a matching
// refspec).
func pushCmds(refs ...string) []pushCommand {
	cmds := make([]pushCommand, 0, len(refs))
	for _, ref := range refs {
		cmds = append(cmds, pushCommand{src: ref, dst: ref})
	}
	return cmds
}

// bucketPackNames returns the pack names the bucket carries.
func bucketPackNames(t *testing.T, client *Client) []string {
	t.Helper()
	names, err := listBucketPacks(client, "")
	if err != nil {
		t.Fatalf("listBucketPacks: %v", err)
	}
	return names
}

// packedShas returns the object shas a bucket pack's index lists.
func packedShas(t *testing.T, client *Client, name string) []string {
	t.Helper()
	idx, err := client.Get("objects/pack/" + name + ".idx")
	if err != nil {
		t.Fatalf("read %s.idx: %v", name, err)
	}
	entries, err := parsePackIdx(idx)
	if err != nil {
		t.Fatalf("parse %s.idx: %v", name, err)
	}
	shas := make([]string, len(entries))
	for i, e := range entries {
		shas[i] = e.sha
	}
	return shas
}

// looseObjectKeys returns the loose object keys the bucket carries.
func looseObjectKeys(t *testing.T, client *Client) []string {
	t.Helper()
	shas, err := listLooseObjects(client, "")
	if err != nil {
		t.Fatalf("listLooseObjects: %v", err)
	}
	return shas
}

// backdatePendingRounds rewrites the bucket's sealing state so its pending
// rounds were sealed long enough ago to clear the wall-clock half of the grace,
// leaving the push counter to decide on its own.
func backdatePendingRounds(t *testing.T, client *Client) {
	t.Helper()
	state, err := readPackState(client, "")
	if err != nil {
		t.Fatalf("readPackState: %v", err)
	}
	for i := range state.Pending {
		state.Pending[i].SealedAt = time.Now().Add(-2 * packDeleteGraceWindow).Unix()
	}
	if err := writePackState(client, "", state); err != nil {
		t.Fatalf("writePackState: %v", err)
	}
}

// orphanStateObject creates a commit in the repo under test and uploads its
// loose object to the bucket WITHOUT giving it a local ref: the shape a shared
// bucket normally has, where a state ref another clone published names a
// refname this clone does not carry (which used to abort sealing) over an
// object it does (whose tip therefore still resolves and seals).
func orphanStateObject(t *testing.T, client *Client, dir, message string) string {
	t.Helper()
	sha := gitRun(t, dir, "commit-tree", gitRun(t, dir, "mktree"), "-m", message)
	body, err := os.ReadFile(filepath.Join(dir, ".git", "objects", sha[:2], sha[2:]))
	if err != nil {
		t.Fatalf("read state object: %v", err)
	}
	if err := client.Put("objects/"+sha[:2]+"/"+sha[2:], body); err != nil {
		t.Fatalf("upload state object: %v", err)
	}
	return sha
}

// packedSet returns every sha the bucket's packs carry.
func packedSet(t *testing.T, client *Client) map[string]bool {
	t.Helper()
	packed := map[string]bool{}
	for _, name := range bucketPackNames(t, client) {
		for _, sha := range packedShas(t, client, name) {
			packed[sha] = true
		}
	}
	return packed
}

// readPackRange decodes one non-delta pack entry straight out of a packfile's
// bytes, exactly the way the browser reader does: parse the type/size varint
// header, then inflate the rest of the recorded range.
func readPackRange(t *testing.T, pack []byte, offset, size int64) (objType string, body []byte) {
	t.Helper()
	raw := pack[offset : offset+size]
	i := 0
	b := raw[i]
	i++
	code := (b >> 4) & 7
	for b&0x80 != 0 {
		b = raw[i]
		i++
	}
	types := map[byte]string{1: "commit", 2: "tree", 3: "blob", 4: "tag"}
	name, ok := types[code]
	if !ok {
		t.Fatalf("pack entry at %d: type %d is a delta, but this pack must have none", offset, code)
	}
	zr, err := zlib.NewReader(bytes.NewReader(raw[i:]))
	if err != nil {
		t.Fatalf("pack entry at %d: inflate: %v", offset, err)
	}
	defer zr.Close()
	content, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("pack entry at %d: inflate: %v", offset, err)
	}
	return name, content
}

// TestUploadPacked_TypeSplitAndPackMap: a push above the threshold writes two
// packs — commits and tags in one, trees and blobs in the other — publishes
// every commit's byte range in the pack map, and writes no loose object.
func TestUploadPacked_TypeSplitAndPackMap(t *testing.T) {
	client, _ := testClient(t)
	dir := packTestRepo(t, 6)
	gitRun(t, dir, "tag", "-a", "v1", "-m", "release")
	h := pushHelper(t, client, dir)
	t.Setenv("GITSOCIAL_S3_PACK_THRESHOLD", "1")

	if err := h.uploadMissingObjects(pushCmds("refs/heads/main", "refs/tags/v1")); err != nil {
		t.Fatalf("uploadMissingObjects: %v", err)
	}

	if got := looseObjectKeys(t, client); len(got) != 0 {
		t.Errorf("packed push wrote %d loose objects; packed and loose copies must never coexist", len(got))
	}
	names := bucketPackNames(t, client)
	if len(names) != 2 {
		t.Fatalf("bucket carries %d packs (%v), want 2 (commits + content)", len(names), names)
	}
	// Classify the two packs by what they hold, then check the split is exactly
	// the type boundary the reader depends on.
	kinds := map[string]string{}
	for _, sha := range strings.Fields(gitRun(t, dir, "rev-list", "--objects", "--all")) {
		if len(sha) == 40 {
			kinds[sha] = gitRun(t, dir, "cat-file", "-t", sha)
		}
	}
	kinds[gitRun(t, dir, "rev-parse", "refs/tags/v1")] = "tag"
	var commitPack, contentPack string
	for _, name := range names {
		for _, sha := range packedShas(t, client, name) {
			switch kinds[sha] {
			case "commit", "tag":
				if contentPack == name {
					t.Fatalf("pack %s mixes commit-like and content objects", name)
				}
				commitPack = name
			case "tree", "blob":
				if commitPack == name {
					t.Fatalf("pack %s mixes commit-like and content objects", name)
				}
				contentPack = name
			default:
				t.Fatalf("object %s in pack %s has unexpected type %q", sha, name, kinds[sha])
			}
		}
	}
	if commitPack == "" || contentPack == "" {
		t.Fatalf("expected one commit-like and one content pack, got %v", names)
	}

	// Every commit and the tag must resolve from the pack map alone: one range,
	// no index, no delta chain.
	pack, err := client.Get("objects/pack/" + commitPack + ".pack")
	if err != nil {
		t.Fatal(err)
	}
	for sha, kind := range kinds {
		if kind != "commit" && kind != "tag" {
			continue
		}
		doc, err := readPackMapShard(client, "", packMapShardName(sha))
		if err != nil {
			t.Fatalf("read pack map shard for %s: %v", sha, err)
		}
		at, ok := doc.Offsets[sha]
		if !ok || len(at) != 3 {
			t.Fatalf("pack map has no byte range for %s %s", kind, sha)
		}
		if doc.Packs[at[0]] != commitPack {
			t.Errorf("%s maps to pack %q, want %q", sha, doc.Packs[at[0]], commitPack)
		}
		gotType, body := readPackRange(t, pack, at[1], at[2])
		if gotType != kind {
			t.Errorf("%s: pack entry type %q, want %q", sha, gotType, kind)
		}
		want := gitRun(t, dir, "cat-file", kind, sha)
		if strings.TrimSpace(string(body)) != want {
			t.Errorf("%s: pack map range decoded to %q, want %q", sha, body, want)
		}
	}
	// No object may be in more than one pack, or the walker's loose-first rule
	// would pick a duplicate.
	seen := map[string]bool{}
	for _, name := range names {
		for _, sha := range packedShas(t, client, name) {
			if seen[sha] {
				t.Errorf("object %s appears in two packs", sha)
			}
			seen[sha] = true
		}
	}
}

// TestUploadDelta_ThresholdKeepsSmallPushesLoose: below the threshold every
// object goes up loose and no pack is written; at the threshold the same push
// packs instead.
func TestUploadDelta_ThresholdKeepsSmallPushesLoose(t *testing.T) {
	for _, tc := range []struct {
		name      string
		threshold string
		wantPacks int
	}{
		{"below the threshold stays loose", "1000", 0},
		{"at the threshold packs", "1", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, _ := testClient(t)
			dir := packTestRepo(t, 3)
			h := pushHelper(t, client, dir)
			t.Setenv("GITSOCIAL_S3_PACK_THRESHOLD", tc.threshold)

			if err := h.uploadMissingObjects(pushCmds("refs/heads/main")); err != nil {
				t.Fatalf("uploadMissingObjects: %v", err)
			}
			if got := len(bucketPackNames(t, client)); got != tc.wantPacks {
				t.Errorf("bucket carries %d packs, want %d", got, tc.wantPacks)
			}
			loose := len(looseObjectKeys(t, client))
			if tc.wantPacks == 0 && loose == 0 {
				t.Error("below the threshold every object must be uploaded loose, found none")
			}
			if tc.wantPacks > 0 && loose != 0 {
				t.Errorf("packed push also wrote %d loose objects", loose)
			}
		})
	}
}

// TestUploadPacked_StateRefObjectsPack: refs/gitmsg/* objects join the packs —
// every loose-key reader has a pack fallback now (getBucketCommit reads the
// pack map, the browser probes both shapes) — so a packed push writes no loose
// keys at all and the state commit resolves through the pack map.
func TestUploadPacked_StateRefObjectsPack(t *testing.T) {
	client, _ := testClient(t)
	dir := packTestRepo(t, 4)
	stateSha := gitRun(t, dir, "commit-tree", gitRun(t, dir, "rev-parse", "HEAD^{tree}"), "-m", "config")
	gitRun(t, dir, "update-ref", "refs/gitmsg/core/config", stateSha)
	h := pushHelper(t, client, dir)
	t.Setenv("GITSOCIAL_S3_PACK_THRESHOLD", "1")

	if err := h.uploadMissingObjects(pushCmds("refs/heads/main", "refs/gitmsg/core/config")); err != nil {
		t.Fatalf("uploadMissingObjects: %v", err)
	}

	if got := looseObjectKeys(t, client); len(got) != 0 {
		t.Errorf("packed push wrote %d loose objects; the whole delta, state refs included, must pack", len(got))
	}
	if !packedSet(t, client)[stateSha] {
		t.Errorf("state-ref commit %s is in no pack", stateSha)
	}
	// The push-time config readers resolve the packed-only commit off the pack
	// map: one range read, no loose key, no local source.
	c, err := getBucketCommit(client, "", stateSha)
	if err != nil {
		t.Fatalf("getBucketCommit over a packed-only state commit: %v", err)
	}
	if strings.TrimSpace(c.item.Message) != "config" {
		t.Errorf("packed state commit message = %q, want %q", c.item.Message, "config")
	}
}

// TestWriteDumbTransportInfo_ListsPacks: objects/info/packs carries a "P
// <name>.pack" line per pack (and the trailing blank line git writes), so stock
// git's dumb walker discovers them.
func TestWriteDumbTransportInfo_ListsPacks(t *testing.T) {
	client, _ := testClient(t)
	dir := packTestRepo(t, 4)
	h := pushHelper(t, client, dir)
	t.Setenv("GITSOCIAL_S3_PACK_THRESHOLD", "1")
	if err := h.uploadMissingObjects(pushCmds("refs/heads/main")); err != nil {
		t.Fatalf("uploadMissingObjects: %v", err)
	}
	refs := map[string]string{"refs/heads/main": gitRun(t, dir, "rev-parse", "HEAD")}
	if err := writeDumbTransportInfo(client, "", nil, refs, false); err != nil {
		t.Fatalf("writeDumbTransportInfo: %v", err)
	}

	body, err := client.Get(packsKey)
	if err != nil {
		t.Fatal(err)
	}
	var want strings.Builder
	for _, name := range bucketPackNames(t, client) {
		fmt.Fprintf(&want, "P %s.pack\n", name)
	}
	want.WriteString("\n")
	if string(body) != want.String() {
		t.Errorf("objects/info/packs = %q, want %q", body, want.String())
	}
	if got := parseInfoPacks(body); len(got) != 2 {
		t.Errorf("parseInfoPacks round trip = %v, want the 2 packs", got)
	}
	for _, name := range bucketPackNames(t, client) {
		for _, key := range []string{"objects/pack/" + name + ".pack", "objects/pack/" + name + ".idx"} {
			if got := cacheControlForKey(key); got != cacheControlImmutable {
				t.Errorf("cacheControlForKey(%q) = %q, want %q", key, got, cacheControlImmutable)
			}
		}
	}
	if got := cacheControlForKey(packsKey); got != cacheControlRevalidate {
		t.Errorf("cacheControlForKey(%q) = %q, want %q", packsKey, got, cacheControlRevalidate)
	}
}

// TestFetch_PackOnlyBucket: the fetch walk against a bucket whose objects exist
// only inside packfiles pulls the packs, indexes them locally, and lands a
// complete, fsck-clean object graph.
func TestFetch_PackOnlyBucket(t *testing.T) {
	client, _ := testClient(t)
	source := packTestRepo(t, 5)
	tip := gitRun(t, source, "rev-parse", "HEAD")
	h := pushHelper(t, client, source)
	t.Setenv("GITSOCIAL_S3_PACK_THRESHOLD", "1")
	if err := h.uploadMissingObjects(pushCmds("refs/heads/main")); err != nil {
		t.Fatalf("uploadMissingObjects: %v", err)
	}
	refs := map[string]string{"refs/heads/main": tip}
	if err := writeDumbTransportInfo(client, "", nil, refs, false); err != nil {
		t.Fatalf("writeDumbTransportInfo: %v", err)
	}

	dest := t.TempDir()
	gitRun(t, dest, "init", "-q", "-b", "main")
	destGit := filepath.Join(dest, ".git")
	t.Setenv("GIT_DIR", destGit)
	fetcher := &remoteHelper{client: client, gitDir: destGit, fetched: map[string]bool{}}
	defer fetcher.local.close()
	if err := fetcher.fetch([]string{"fetch " + tip + " refs/heads/main"}); err != nil {
		t.Fatalf("fetch from a pack-only bucket: %v", err)
	}

	gitRun(t, dest, "update-ref", "refs/heads/main", tip)
	if got := gitRun(t, dest, "rev-list", "--count", "main"); got != "5" {
		t.Errorf("fetched history has %s commits, want 5", got)
	}
	gitRun(t, dest, "fsck", "--no-dangling")
	for _, sha := range strings.Fields(gitRun(t, source, "rev-list", "--objects", "--all")) {
		if len(sha) == 40 {
			gitRun(t, dest, "cat-file", "-e", sha)
		}
	}
}

// TestSealLooseObjects_PacksThenDeletesAfterGrace: an all-loose bucket is sealed
// into packs, its loose copies survive the grace period (so an in-flight clone
// finishes), and are deleted only once it expires.
func TestSealLooseObjects_PacksThenDeletesAfterGrace(t *testing.T) {
	client, _ := testClient(t)
	dir := packTestRepo(t, 6)
	h := pushHelper(t, client, dir)
	t.Setenv("GITSOCIAL_S3_PACK_THRESHOLD", "1000") // push loose
	if err := h.uploadMissingObjects(pushCmds("refs/heads/main")); err != nil {
		t.Fatalf("uploadMissingObjects: %v", err)
	}
	looseBefore := len(looseObjectKeys(t, client))
	if looseBefore == 0 {
		t.Fatal("expected an all-loose bucket to seal")
	}
	refs := map[string]string{"refs/heads/main": gitRun(t, dir, "rev-parse", "HEAD")}

	// First maintenance pass seals; the loose copies must still be there.
	t.Setenv("GITSOCIAL_S3_PACK_THRESHOLD", "1")
	h.maintainPacks(refs)
	if len(bucketPackNames(t, client)) != 2 {
		t.Fatalf("sealing wrote %d packs, want 2", len(bucketPackNames(t, client)))
	}
	if got := len(looseObjectKeys(t, client)); got != looseBefore {
		t.Errorf("sealing deleted %d loose objects immediately; the grace period must keep them", looseBefore-got)
	}
	state, err := readPackState(client, "")
	if err != nil || len(state.Pending) != 1 {
		t.Fatalf("pack state after sealing: %+v (err %v), want one pending round", state, err)
	}

	// Further pushes run the grace down; deletion happens on the pass that
	// reaches deleteAfter.
	for i := 0; i < packDeleteGrace-1; i++ {
		h.maintainPacks(refs)
		if got := len(looseObjectKeys(t, client)); got != looseBefore {
			t.Fatalf("loose objects deleted %d push(es) into a %d-push grace", i+1, packDeleteGrace)
		}
	}
	// The push counter has now run out, but the wall-clock half of the grace has
	// not: a burst of pushes must not shorten the window a clone walks in.
	h.maintainPacks(refs)
	if got := len(looseObjectKeys(t, client)); got != looseBefore {
		t.Fatalf("loose objects deleted %d push(es) into the grace with the wall-clock window still open", packDeleteGrace)
	}
	backdatePendingRounds(t, client)
	h.maintainPacks(refs)
	if got := len(looseObjectKeys(t, client)); got != 0 {
		t.Errorf("%d loose objects survive the grace period, want them deleted", got)
	}
	state, err = readPackState(client, "")
	if err != nil || len(state.Pending) != 0 {
		t.Fatalf("pack state after deletion: %+v (err %v), want no pending round", state, err)
	}
	// The sealed history must still be readable: every commit keeps a pack map
	// byte range even though its loose key is gone.
	for _, sha := range strings.Fields(gitRun(t, dir, "rev-list", "main")) {
		doc, err := readPackMapShard(client, "", packMapShardName(sha))
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := doc.Offsets[sha]; !ok {
			t.Errorf("sealed commit %s has no pack map entry, so the browser can no longer read it", sha)
		}
	}
}

// TestSealLooseObjects_MidSizeCohortSeals: the seal minimum is the drift
// trigger (packSealLooseThreshold), not the push path's 1,000-object litter
// threshold, so a cohort between the two — the shape one tag-heavy push leaves
// behind — is sealed instead of declined. Before the fix such a cohort was
// declined forever and every stock clone paid one GET per loose object.
func TestSealLooseObjects_MidSizeCohortSeals(t *testing.T) {
	client, _ := testClient(t)
	dir := packTestRepo(t, 25) // 75 objects: above the seal minimum, far below the push threshold
	h := pushHelper(t, client, dir)
	if err := h.uploadMissingObjects(pushCmds("refs/heads/main")); err != nil {
		t.Fatalf("uploadMissingObjects: %v", err)
	}
	loose := len(looseObjectKeys(t, client))
	if loose < packSealLooseThreshold || loose >= defaultPackThreshold {
		t.Fatalf("fixture has %d loose objects, want between %d and %d", loose, packSealLooseThreshold, defaultPackThreshold)
	}
	refs := map[string]string{"refs/heads/main": gitRun(t, dir, "rev-parse", "HEAD")}

	h.maintainPacks(refs)

	if got := len(bucketPackNames(t, client)); got != 2 {
		t.Fatalf("sealing a %d-object cohort wrote %d packs, want 2", loose, got)
	}
	state, err := readPackState(client, "")
	if err != nil {
		t.Fatalf("readPackState: %v", err)
	}
	if state.LastSeal != state.Generation {
		t.Errorf("lastSeal %d, generation %d: the seal must advance the counter", state.LastSeal, state.Generation)
	}
	if state.LooseSinceSeal != 0 {
		t.Errorf("looseSinceSeal %d after a full seal, want 0", state.LooseSinceSeal)
	}
	if state.LastError != "" {
		t.Errorf("sealing recorded failure %q", state.LastError)
	}
}

// TestMaintainPacks_DeclinedSealAdvancesLastSealAndMeasuresLoose: an attempt
// whose sealable set is below the seal minimum must be recorded as what it is —
// a decline, not a success. LastSeal still advances (the decline paid the
// bucket LIST the counter rate-limits), but LooseSinceSeal keeps the measured
// sealable count instead of being zeroed: zeroing it recorded the decline as a
// completed seal, so a mid-size cohort re-tripped the trigger, re-declined, and
// was re-zeroed on every push forever (the production pack-state showed
// looseSinceSeal:7 beside ~620 loose objects). And with the counter advanced,
// a push that uploads nothing loose must not re-attempt (no objects/ LIST)
// until the interval expires.
func TestMaintainPacks_DeclinedSealAdvancesLastSealAndMeasuresLoose(t *testing.T) {
	client, bucket := testClient(t)
	dir := packTestRepo(t, 6) // ~19 objects, below the seal minimum
	h := pushHelper(t, client, dir)
	if err := h.uploadMissingObjects(pushCmds("refs/heads/main")); err != nil {
		t.Fatalf("uploadMissingObjects: %v", err)
	}
	looseBefore := len(looseObjectKeys(t, client))
	if looseBefore == 0 || looseBefore >= packSealLooseThreshold {
		t.Fatalf("fixture has %d loose objects, want a non-empty cohort below %d", looseBefore, packSealLooseThreshold)
	}
	refs := map[string]string{"refs/heads/main": gitRun(t, dir, "rev-parse", "HEAD")}

	h.maintainPacks(refs) // never sealed, so the attempt is due; it declines

	if got := len(bucketPackNames(t, client)); got != 0 {
		t.Fatalf("a declined seal published %d packs, want none", got)
	}
	state, err := readPackState(client, "")
	if err != nil {
		t.Fatalf("readPackState: %v", err)
	}
	if state.LastSeal != state.Generation {
		t.Errorf("lastSeal %d, generation %d: a declined attempt must advance the counter, it paid the LIST", state.LastSeal, state.Generation)
	}
	if state.LooseSinceSeal != looseBefore {
		t.Errorf("looseSinceSeal %d after the decline, want the measured %d: zeroing it records the decline as a seal", state.LooseSinceSeal, looseBefore)
	}
	if state.LastError != "" {
		t.Errorf("a decline is not a failure, but %q was recorded", state.LastError)
	}
	if len(state.Pending) != 0 {
		t.Errorf("pack state has %d pending rounds after a decline, want none", len(state.Pending))
	}

	// The next push moves a ref without uploading anything loose: the measured
	// count is below the trigger and the interval has not expired, so the pass
	// must not pay another objects/ listing.
	h.looseUploaded = 0
	listsBefore := bucket.listCount()
	h.maintainPacks(refs)
	if got := bucket.listCount(); got != listsBefore {
		t.Errorf("the push after a decline listed the bucket (%d listings, was %d); the interval must rate-limit the retry", got, listsBefore)
	}
	state, err = readPackState(client, "")
	if err != nil {
		t.Fatalf("readPackState: %v", err)
	}
	if state.LooseSinceSeal != looseBefore {
		t.Errorf("looseSinceSeal %d after a no-upload push, want it kept at %d", state.LooseSinceSeal, looseBefore)
	}
}

// TestParsePackIdx_MatchesGitShowIndex: the index parser agrees with git's own
// reading of the same file, including the large-offset indirection's layout.
func TestParsePackIdx_MatchesGitShowIndex(t *testing.T) {
	dir := packTestRepo(t, 4)
	t.Setenv("GIT_DIR", filepath.Join(dir, ".git"))
	shas := strings.Fields(gitRun(t, dir, "rev-list", "--objects", "--all"))
	var objects []string
	for _, sha := range shas {
		if len(sha) == 40 {
			objects = append(objects, sha)
		}
	}
	built, err := buildPack(objects, false, true)
	if err != nil {
		t.Fatalf("buildPack: %v", err)
	}

	show := exec.Command("git", "show-index")
	show.Stdin = bytes.NewReader(built.idx)
	shown, err := show.Output()
	if err != nil {
		t.Fatalf("git show-index: %v", err)
	}
	want := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(shown)), "\n") {
		if fields := strings.Fields(line); len(fields) >= 2 {
			want[fields[1]] = fields[0]
		}
	}
	entries, err := parsePackIdx(built.idx)
	if err != nil {
		t.Fatalf("parsePackIdx: %v", err)
	}
	if len(entries) != len(want) {
		t.Fatalf("parsePackIdx found %d objects, git show-index %d", len(entries), len(want))
	}
	for _, e := range entries {
		if got := want[e.sha]; got != fmt.Sprintf("%d", e.offset) {
			t.Errorf("object %s: parsed offset %d, git says %s", e.sha, e.offset, got)
		}
	}
	// The published ranges must tile the pack exactly, with nothing overlapping
	// and nothing left but the 20-byte trailer.
	var covered int64
	for _, e := range built.entries {
		if e.size <= 0 {
			t.Errorf("object %s has a non-positive pack range size %d", e.sha, e.size)
		}
		covered += e.size
	}
	if want := int64(len(built.pack)) - 12 - 20; covered != want {
		t.Errorf("pack ranges cover %d bytes, want %d (pack minus header and trailer)", covered, want)
	}
}

// TestSealLooseObjects_BucketStateRefMissingLocally reproduces the production
// condition that left a bucket permanently unsealed: the bucket carries
// refs/gitmsg/* state refs (a release's artifact refs, say) whose REFNAMES the
// pushing clone does not have. Resolving those refnames against the local repo
// made `git rev-list` exit 128, which aborted the pass before it packed
// anything, on every push forever.
func TestSealLooseObjects_BucketStateRefMissingLocally(t *testing.T) {
	client, _ := testClient(t)
	dir := packTestRepo(t, 6)
	h := pushHelper(t, client, dir)
	t.Setenv("GITSOCIAL_S3_PACK_THRESHOLD", "1000") // push loose
	if err := h.uploadMissingObjects(pushCmds("refs/heads/main")); err != nil {
		t.Fatalf("uploadMissingObjects: %v", err)
	}
	if len(looseObjectKeys(t, client)) == 0 {
		t.Fatal("expected an all-loose bucket to seal")
	}
	// A state ref only the bucket has: this clone carries no such refname.
	orphan := orphanStateObject(t, client, dir, "artifacts")
	refs := map[string]string{
		"refs/heads/main":                     gitRun(t, dir, "rev-parse", "HEAD"),
		"refs/gitmsg/release/1.0.0/artifacts": orphan,
	}

	t.Setenv("GITSOCIAL_S3_PACK_THRESHOLD", "1")
	h.maintainPacks(refs)

	if got := len(bucketPackNames(t, client)); got != 2 {
		t.Fatalf("sealing wrote %d packs, want 2: a bucket ref this clone lacks must not abort the pass", got)
	}
	state, err := readPackState(client, "")
	if err != nil {
		t.Fatalf("readPackState: %v", err)
	}
	if state.LastError != "" {
		t.Errorf("sealing recorded failure %q", state.LastError)
	}
	if len(state.Pending) != 1 {
		t.Errorf("pack state has %d pending rounds, want 1", len(state.Pending))
	}
	if state.LastSeal != state.Generation {
		t.Errorf("lastSeal %d, generation %d: a successful seal must advance the counter", state.LastSeal, state.Generation)
	}
}

// TestSealLooseObjects_StateRefObjectsPackAndDelete: objects a state ref
// reaches pack and seal like everything else now (every loose-key reader has a
// pack fallback), so sealing packs them and deletes their loose copies once the
// grace runs out — whether the state ref's refname resolves in the pushing
// clone or, as on a shared bucket, does not. A tip whose OBJECT this clone does
// not carry still contributes nothing: it stays loose, untouched.
func TestSealLooseObjects_StateRefObjectsPackAndDelete(t *testing.T) {
	client, _ := testClient(t)
	dir := packTestRepo(t, 6)
	stateSha := gitRun(t, dir, "commit-tree", gitRun(t, dir, "mktree"), "-m", "config")
	gitRun(t, dir, "update-ref", "refs/gitmsg/core/config", stateSha)
	h := pushHelper(t, client, dir)
	t.Setenv("GITSOCIAL_S3_PACK_THRESHOLD", "1000") // push loose
	if err := h.uploadMissingObjects(pushCmds("refs/heads/main", "refs/gitmsg/core/config")); err != nil {
		t.Fatalf("uploadMissingObjects: %v", err)
	}
	// A state object present locally whose refname only the bucket has: packs.
	orphan := orphanStateObject(t, client, dir, "artifacts")
	// An object this clone does NOT carry: an unresolvable tip contributes
	// nothing, so its loose key must survive the whole pass.
	foreignSha, foreignLoose := makeLooseCommit(t, "", "a foreign clone's config", 1000)
	if err := client.Put("objects/"+foreignSha[:2]+"/"+foreignSha[2:], foreignLoose); err != nil {
		t.Fatalf("upload foreign object: %v", err)
	}
	refs := map[string]string{
		"refs/heads/main":                     gitRun(t, dir, "rev-parse", "HEAD"),
		"refs/gitmsg/core/config":             stateSha,
		"refs/gitmsg/release/2.0.0/artifacts": orphan,
		"refs/gitmsg/social/lists/team/_meta": foreignSha,
	}

	t.Setenv("GITSOCIAL_S3_PACK_THRESHOLD", "1")
	h.maintainPacks(refs)
	packed := packedSet(t, client)
	for _, sha := range []string{stateSha, orphan} {
		if !packed[sha] {
			t.Errorf("state-ref object %s was not packed", sha)
		}
	}
	if packed[foreignSha] {
		t.Errorf("locally-absent object %s was packed", foreignSha)
	}

	// Run the whole grace out; the sealed state objects' loose copies go with
	// everything else, the unresolvable tip's object stays.
	for i := 0; i < packDeleteGrace; i++ {
		backdatePendingRounds(t, client)
		h.maintainPacks(refs)
	}
	loose := map[string]bool{}
	for _, sha := range looseObjectKeys(t, client) {
		loose[sha] = true
	}
	for _, sha := range []string{stateSha, orphan} {
		if loose[sha] {
			t.Errorf("sealed state-ref object %s still has its loose copy after the grace", sha)
		}
	}
	if !loose[foreignSha] {
		t.Error("locally-absent object was deleted; an unresolvable tip must contribute nothing")
	}
	if len(packed) == 0 {
		t.Error("sealing packed nothing, so the deletion above never ran")
	}
	// The sealed state commits stay readable through the pack fallback the
	// push-time config readers use.
	for _, sha := range []string{stateSha, orphan} {
		c, err := getBucketCommit(client, "", sha)
		if err != nil {
			t.Errorf("getBucketCommit(%s) after seal + delete: %v", sha, err)
		} else if c.item.SHA != sha {
			t.Errorf("getBucketCommit(%s) returned sha %s", sha, c.item.SHA)
		}
	}
}

// TestReadSiteConfigs_PackedOnlyAndLocal: the push-time config readers succeed
// against a bucket whose config commits exist ONLY inside packs (the pack-map
// fallback), and resolve purely from the local odb when a source is available
// (asserted against an empty bucket, where only the local path can answer).
func TestReadSiteConfigs_PackedOnlyAndLocal(t *testing.T) {
	client, _ := testClient(t)
	dir := packTestRepo(t, 4)
	pmSha := gitRun(t, dir, "commit-tree", gitRun(t, dir, "mktree"), "-m", `{"framework":"scrum"}`)
	gitRun(t, dir, "update-ref", "refs/gitmsg/pm/config", pmSha)
	coreSha := gitRun(t, dir, "commit-tree", gitRun(t, dir, "mktree"), "-m", `{"version":1,"site":{"title":"Packed"}}`)
	gitRun(t, dir, "update-ref", "refs/gitmsg/core/config", coreSha)
	h := pushHelper(t, client, dir)
	t.Setenv("GITSOCIAL_S3_PACK_THRESHOLD", "1")
	if err := h.uploadMissingObjects(pushCmds("refs/heads/main", "refs/gitmsg/pm/config", "refs/gitmsg/core/config")); err != nil {
		t.Fatalf("uploadMissingObjects: %v", err)
	}
	if got := looseObjectKeys(t, client); len(got) != 0 {
		t.Fatalf("fixture is not packed-only: %d loose keys", len(got))
	}
	refs := map[string]string{"refs/gitmsg/pm/config": pmSha, "refs/gitmsg/core/config": coreSha}

	cfg, ok, err := readSitePMConfig(client, "", refs, nil)
	if err != nil || !ok || cfg.Framework != "scrum" {
		t.Errorf("readSitePMConfig over a packed-only bucket = %+v ok=%v err=%v", cfg, ok, err)
	}
	custom, ok, err := readSiteBaseCustomization(client, "", refs, nil)
	if err != nil || !ok || custom.Title != "Packed" {
		t.Errorf("readSiteBaseCustomization over a packed-only bucket = %+v ok=%v err=%v", custom, ok, err)
	}

	src := newLocalCommitSource(filepath.Join(dir, ".git"), "")
	defer src.close()
	emptyClient, _ := testClient(t)
	cfg, ok, err = readSitePMConfig(emptyClient, "", refs, src)
	if err != nil || !ok || cfg.Framework != "scrum" {
		t.Errorf("readSitePMConfig via the local odb = %+v ok=%v err=%v", cfg, ok, err)
	}
	custom, ok, err = readSiteBaseCustomization(emptyClient, "", refs, src)
	if err != nil || !ok || custom.Title != "Packed" {
		t.Errorf("readSiteBaseCustomization via the local odb = %+v ok=%v err=%v", custom, ok, err)
	}
}

// hookedClient serves an in-memory bucket whose requests run `after` once the
// bucket has answered them, so a test can land a competing write INSIDE another
// writer's compare-and-swap window: between the read that fed the merge and the
// conditional write that read authorizes. Sequential commits only prove the
// merge function; a plain PUT passes them too. This is what forces the 412
// replay. The hook is handed the same client, and must guard its own re-entry
// (its own requests run through the same handler).
func hookedClient(t *testing.T, after func(*Client, *http.Request)) *Client {
	t.Helper()
	bucket := newMemBucket()
	var mu sync.Mutex
	var client *Client
	current := func() *Client {
		mu.Lock()
		defer mu.Unlock()
		return client
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bucket.ServeHTTP(w, r)
		if c := current(); c != nil && after != nil {
			after(c, r)
		}
	}))
	t.Cleanup(srv.Close)
	c, err := NewClient(Config{
		Endpoint: srv.URL, Bucket: "b", Region: "us-east-1",
		AccessKey: "k", SecretKey: "s", PathStyle: true,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	mu.Lock()
	client = c
	mu.Unlock()
	return c
}

// readOf reports whether a request is a plain GET of the given bucket key (not
// a listing), i.e. the read half of a compare-and-swap cycle.
func readOf(r *http.Request, key string) bool {
	return r.Method == http.MethodGet && r.URL.Query().Get("list-type") != "2" && keyOf(r.URL.Path) == key
}

// TestCommitPackState_ConcurrentPassesMerge: two pushers that both read the
// state at the same generation must both land. A plain last-writer-wins write
// dropped the loser's pending round, and a dropped round is a set of loose
// copies nothing ever deletes, kept alongside the packs that replaced them.
func TestCommitPackState_ConcurrentPassesMerge(t *testing.T) {
	first := packStateUpdate{deleted: map[string]bool{}, sealed: &packRound{Packs: []string{"pack-aaa"}}, sealAttempted: true}
	second := packStateUpdate{deleted: map[string]bool{}, sealed: &packRound{Packs: []string{"pack-bbb"}}, sealAttempted: true}

	// The second pusher's whole pass runs inside the first's window: after the
	// read it merges onto, before the write that read authorizes.
	var raced atomic.Bool
	client := hookedClient(t, func(c *Client, r *http.Request) {
		if !readOf(r, packStateKey) || !raced.CompareAndSwap(false, true) {
			return
		}
		if err := commitPackState(c, CapabilityFull, "", second); err != nil {
			t.Errorf("competing commitPackState: %v", err)
		}
	})
	if err := commitPackState(client, CapabilityFull, "", first); err != nil {
		t.Fatalf("commitPackState: %v", err)
	}
	if !raced.Load() {
		t.Fatal("the competing write never ran, so no compare-and-swap window was exercised")
	}

	state, err := readPackState(client, "")
	if err != nil {
		t.Fatalf("readPackState: %v", err)
	}
	if state.Generation != 2 {
		t.Errorf("generation %d after two pushes, want 2", state.Generation)
	}
	got := map[string]bool{}
	for _, round := range state.Pending {
		got[roundKey(round)] = true
	}
	for _, want := range []string{"pack-aaa", "pack-bbb"} {
		if !got[want] {
			t.Errorf("round %s was lost; both pushers' rounds must survive (pending: %+v)", want, state.Pending)
		}
	}
}

// TestCommitPackState_DeletionMergesOntoAnotherPushersState: a pass that
// deleted a round's loose copies must drop exactly that round from whatever the
// state says now, keeping a round another pusher queued in the meantime — even
// when that other pusher queued it inside this pass's compare-and-swap window.
func TestCommitPackState_DeletionMergesOntoAnotherPushersState(t *testing.T) {
	var armed, raced atomic.Bool
	seal := packStateUpdate{deleted: map[string]bool{}, sealed: &packRound{Packs: []string{"pack-new"}}}
	client := hookedClient(t, func(c *Client, r *http.Request) {
		if !armed.Load() || !readOf(r, packStateKey) || !raced.CompareAndSwap(false, true) {
			return
		}
		if err := commitPackState(c, CapabilityFull, "", seal); err != nil {
			t.Errorf("competing commitPackState: %v", err)
		}
	})
	if err := commitPackState(client, CapabilityFull, "", packStateUpdate{deleted: map[string]bool{}, sealed: &packRound{Packs: []string{"pack-old"}}}); err != nil {
		t.Fatalf("commitPackState: %v", err)
	}
	// Armed only now, so the seeding pass above is not the one that races.
	armed.Store(true)
	if err := commitPackState(client, CapabilityFull, "", packStateUpdate{deleted: map[string]bool{"pack-old": true}}); err != nil {
		t.Fatalf("commitPackState: %v", err)
	}
	if !raced.Load() {
		t.Fatal("the competing seal never ran, so no compare-and-swap window was exercised")
	}

	state, err := readPackState(client, "")
	if err != nil {
		t.Fatalf("readPackState: %v", err)
	}
	if len(state.Pending) != 1 || roundKey(state.Pending[0]) != "pack-new" {
		t.Errorf("pending rounds %+v, want only pack-new", state.Pending)
	}
}

// TestPutCompressedIfMatch_RetriesATransientFault: a 5xx on the conditional
// write must cost a retried request — not a compare-and-swap attempt, and above
// all not the push. Before the retry one provider hiccup on the state document
// or a pack map shard reported `error <ref>` for every ref in the batch, with
// both packfiles already durable on the bucket.
func TestPutCompressedIfMatch_RetriesATransientFault(t *testing.T) {
	client, bucket := testClient(t)
	bucket.flakyPut(packStateKey, 1) // one 5xx, then through

	if err := commitPackState(client, CapabilityFull, "", packStateUpdate{deleted: map[string]bool{}, sealed: &packRound{Packs: []string{"pack-aaa"}}}); err != nil {
		t.Fatalf("commitPackState: %v", err)
	}
	// Counted before reading the state back, which is a read of its own. One read
	// and one stored write means the fault was absorbed where it happened: a
	// re-read would mean a spent compare-and-swap attempt or the unconditional
	// fallback, both of which turn a hiccup into last-writer-wins.
	reads, writes := bucket.getCount(packStateKey), bucket.putCount(packStateKey)
	if reads != 1 {
		t.Errorf("%d reads of %s, want 1: a transient write fault must not cost a re-read", reads, packStateKey)
	}
	if writes != 1 {
		t.Errorf("%d stored writes of %s, want 1", writes, packStateKey)
	}

	state, err := readPackState(client, "")
	if err != nil {
		t.Fatalf("readPackState: %v", err)
	}
	if len(state.Pending) != 1 || roundKey(state.Pending[0]) != "pack-aaa" {
		t.Errorf("pending rounds %+v, want the round the push sealed", state.Pending)
	}
}

// TestReadPackState_PresentButUnreadableIsNeverOverwritten: a document that IS
// there but cannot be read — unparseable bytes, or a schema version this binary
// does not write — must fail the pass rather than be replaced by a zeroed one
// under a passing compare-and-swap. Overwriting drops every pending round, and a
// dropped round is loose copies nothing ever collects, kept forever beside the
// packs that replaced them.
func TestReadPackState_PresentButUnreadableIsNeverOverwritten(t *testing.T) {
	foreign, err := compressJSON(map[string]any{"version": packStateVersion + 99, "generation": 7}, brotliQualityFull)
	if err != nil {
		t.Fatalf("compressJSON: %v", err)
	}
	for _, tc := range []struct {
		name string
		seed func(*Client) error
	}{
		{"bytes that do not parse", func(c *Client) error { return c.Put(packStateKey, []byte("{ half a document")) }},
		{"another schema version", func(c *Client) error { return putCompressed(c, packStateKey, foreign) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, _ := testClient(t)
			if err := tc.seed(client); err != nil {
				t.Fatalf("seed: %v", err)
			}
			before, err := client.Get(packStateKey)
			if err != nil {
				t.Fatalf("read back the seed: %v", err)
			}
			if _, err := readPackState(client, ""); err == nil {
				t.Error("readPackState returned a state for a document it cannot read; the pass would re-seal the whole bucket on every push")
			}
			if err := commitPackState(client, CapabilityFull, "", packStateUpdate{deleted: map[string]bool{}, sealed: &packRound{Packs: []string{"pack-aaa"}}}); err == nil {
				t.Error("commitPackState wrote over a document it cannot read")
			}
			after, err := client.Get(packStateKey)
			if err != nil {
				t.Fatalf("read the document after the pass: %v", err)
			}
			if !bytes.Equal(before, after) {
				t.Errorf("the document changed under the pass (%d bytes -> %d)", len(before), len(after))
			}
		})
	}
}

// TestUnsealableClone: sealing walks the bucket's history through the LOCAL odb,
// so a clone that cannot answer for it must skip and leave the pass to one that
// can — a shallow clone cannot see that history at all, and a partial clone
// would drag the whole repo over its promisor in the middle of someone's push.
// Untested in either direction, an inverted condition makes every clone skip
// forever behind one stderr line, and the bucket simply never packs.
func TestUnsealableClone(t *testing.T) {
	full := packTestRepo(t, 3)
	gitRun(t, full, "config", "uploadpack.allowFilter", "true")
	clones := t.TempDir()
	gitRun(t, clones, "clone", "-q", "--depth", "1", "file://"+full, "shallow")
	gitRun(t, clones, "clone", "-q", "--filter=blob:none", "file://"+full, "partial")

	for _, tc := range []struct {
		name string
		dir  string
		want string
	}{
		{"a full clone seals", full, ""},
		{"a shallow clone does not", filepath.Join(clones, "shallow"), "shallow clone"},
		{"a partial clone does not", filepath.Join(clones, "partial"), "partial clone"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("GIT_DIR", filepath.Join(tc.dir, ".git"))
			if got := unsealableClone(); got != tc.want {
				t.Errorf("unsealableClone() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestMaintainPacks_RoundNotAdvertisedKeepsItsLooseObjects: objects/info/packs
// is the only way either reader — stock git's dumb walker and the browser —
// discovers a pack, and concurrent pushers rewrite it from whole listing
// snapshots, so one pusher's snapshot can drop the line another just published.
// A round whose packs the listing does not name must therefore keep its loose
// copies however far its grace has run: durable is not discoverable, and
// deleting them turns objects that exist only inside an unadvertised pack into a
// 404 for every reader.
func TestMaintainPacks_RoundNotAdvertisedKeepsItsLooseObjects(t *testing.T) {
	client, _ := testClient(t)
	dir := packTestRepo(t, 6)
	h := pushHelper(t, client, dir)
	t.Setenv("GITSOCIAL_S3_PACK_THRESHOLD", "1000") // push loose
	if err := h.uploadMissingObjects(pushCmds("refs/heads/main")); err != nil {
		t.Fatalf("uploadMissingObjects: %v", err)
	}
	refs := map[string]string{"refs/heads/main": gitRun(t, dir, "rev-parse", "HEAD")}
	t.Setenv("GITSOCIAL_S3_PACK_THRESHOLD", "1")
	h.maintainPacks(refs)
	packed := packedSet(t, client)
	if len(packed) == 0 {
		t.Fatal("sealing packed nothing, so there is no round to withhold")
	}

	// Another pusher's stale snapshot lands, dropping this round's packs from the
	// listing while the packs themselves stay on the bucket.
	if err := client.Put(packsKey, []byte("\n")); err != nil {
		t.Fatal(err)
	}
	for i := 0; i <= packDeleteGrace; i++ {
		backdatePendingRounds(t, client)
		h.maintainPacks(refs)
	}
	loose := map[string]bool{}
	for _, sha := range looseObjectKeys(t, client) {
		loose[sha] = true
	}
	for sha := range packed {
		if !loose[sha] {
			t.Errorf("loose copy of %s was deleted while %s did not list the pack carrying it", sha, packsKey)
		}
	}
	state, err := readPackState(client, "")
	if err != nil {
		t.Fatalf("readPackState: %v", err)
	}
	if len(state.Pending) != 1 {
		t.Errorf("pack state has %d pending rounds, want the withheld round still pending and retried", len(state.Pending))
	}

	// Positive control: restore the listing and the very next pass collects, so
	// the round was withheld over discoverability and nothing else.
	if err := putText(client, packsKey, buildInfoPacks(bucketPackNames(t, client))); err != nil {
		t.Fatalf("restore %s: %v", packsKey, err)
	}
	backdatePendingRounds(t, client)
	h.maintainPacks(refs)
	loose = map[string]bool{}
	for _, sha := range looseObjectKeys(t, client) {
		loose[sha] = true
	}
	for sha := range packed {
		if loose[sha] {
			t.Errorf("loose copy of %s survived a pass whose packs the listing names", sha)
		}
	}
}

// TestSealLooseObjects_StampsSealedAtOnEveryPublishedRound: SealedAt is what the
// wall-clock half of the grace is measured from, so a round must carry the time
// the pass RETURNED, not the time it started. The first seal of a large all-loose
// history runs for a long time (a full-history pack-objects, a multi-GB PUT, up
// to 256 pack map shards, then the listing), and a round stamped at the start
// hands back a window it has already spent — the loose keys then go while a clone
// that read the pre-seal listing is still walking them. Both return paths that
// carry packs are checked, the failed one included: those packs are on the
// bucket, so their round is recorded and must carry a real window with it.
func TestSealLooseObjects_StampsSealedAtOnEveryPublishedRound(t *testing.T) {
	for _, tc := range []struct {
		name        string
		failListing bool
	}{
		{"the listing published", false},
		{"the listing write failed after the packs landed", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, bucket := testClient(t)
			dir := packTestRepo(t, 6)
			h := pushHelper(t, client, dir)
			t.Setenv("GITSOCIAL_S3_PACK_THRESHOLD", "1000") // push loose
			if err := h.uploadMissingObjects(pushCmds("refs/heads/main")); err != nil {
				t.Fatalf("uploadMissingObjects: %v", err)
			}
			refs := map[string]string{"refs/heads/main": gitRun(t, dir, "rev-parse", "HEAD")}
			if tc.failListing {
				bucket.failPut(packsKey)
			}

			t.Setenv("GITSOCIAL_S3_PACK_THRESHOLD", "1")
			start := time.Now().Unix()
			round, _, err := h.sealLooseObjects(refs)
			if tc.failListing == (err == nil) {
				t.Fatalf("sealLooseObjects error = %v, wanted an error: %v", err, tc.failListing)
			}
			if len(round.Packs) == 0 {
				t.Fatal("the pass published no pack, so there is no round to stamp")
			}
			if round.SealedAt < start || round.SealedAt > time.Now().Unix() {
				t.Errorf("SealedAt %d outside the pass [%d, %d]: the grace window is measured from this stamp",
					round.SealedAt, start, time.Now().Unix())
			}
			// The stamp is what roundDeletable reads, so a round fresh out of the
			// pass must not already be past its wall-clock floor.
			if roundDeletable(packRound{Packs: round.Packs, DeleteAfter: 0, SealedAt: round.SealedAt}, 1) {
				t.Error("a round sealed just now is already deletable; its wall-clock grace was spent before the pass returned")
			}
		})
	}
}

// TestMaintainPacks_FailedSealRetriesNextPush: a seal that fails must not
// advance the counter, or the failure both hides itself and defers the retry by
// a whole packSealInterval — which is how a production bucket stayed unpacked.
func TestMaintainPacks_FailedSealRetriesNextPush(t *testing.T) {
	client, bucket := testClient(t)
	dir := packTestRepo(t, 6)
	h := pushHelper(t, client, dir)
	t.Setenv("GITSOCIAL_S3_PACK_THRESHOLD", "1000") // push loose
	if err := h.uploadMissingObjects(pushCmds("refs/heads/main")); err != nil {
		t.Fatalf("uploadMissingObjects: %v", err)
	}
	refs := map[string]string{"refs/heads/main": gitRun(t, dir, "rev-parse", "HEAD")}
	bucket.failPut(packsKey) // the pack listing every reader discovers packs through

	t.Setenv("GITSOCIAL_S3_PACK_THRESHOLD", "1")
	h.maintainPacks(refs)
	state, err := readPackState(client, "")
	if err != nil {
		t.Fatalf("readPackState: %v", err)
	}
	if state.LastSeal != 0 {
		t.Errorf("lastSeal advanced to %d after a failed seal; the next push would skip the retry", state.LastSeal)
	}
	if state.LastError == "" {
		t.Error("a failed seal must be recorded on the bucket, not only on stderr")
	}
	// The packs that did land are still collected, so their loose copies are not
	// stranded alongside them.
	if len(state.Pending) != 1 {
		t.Errorf("pack state has %d pending rounds, want the round whose packs landed", len(state.Pending))
	}

	listsBefore := bucket.listCount()
	h.maintainPacks(refs)
	if bucket.listCount() == listsBefore {
		t.Error("the next push did not re-attempt the seal (no objects/ listing)")
	}
}

// TestWritePackMapShard_MergesConcurrentPacks: two packs' entries landing in one
// shard must both survive. An object is packed once, so a shard entry lost to a
// last-writer-wins overwrite is never rewritten, and every reader of those
// commits pays the pack-index fallback from then on.
func TestWritePackMapShard_MergesConcurrentPacks(t *testing.T) {
	client, _ := testClient(t)
	shard := "ab"
	mine := []packMapEntry{{sha: shard + strings.Repeat("1", 38), offset: 12, size: 40}}
	theirs := []packMapEntry{{sha: shard + strings.Repeat("2", 38), offset: 52, size: 40}}
	// The competing write lands between this writer's read and its write, which
	// is the whole window a plain read-merge-write leaves open.
	if err := updateCompressedJSON(client, CapabilityFull, packMapKeyPrefix+shard+".json", func(doc *packMapDoc, found bool) error {
		if !found {
			*doc = packMapDoc{Version: packMapVersion, Offsets: map[string][]int64{}}
			if err := writePackMapShard(client, CapabilityFull, "", shard, "pack-theirs", theirs); err != nil {
				return err
			}
		}
		doc.Packs = append(doc.Packs, "pack-mine")
		for _, e := range mine {
			doc.Offsets[e.sha] = []int64{int64(len(doc.Packs) - 1), e.offset, e.size}
		}
		return nil
	}); err != nil {
		t.Fatalf("updateCompressedJSON: %v", err)
	}

	doc, err := readPackMapShard(client, "", shard)
	if err != nil {
		t.Fatalf("readPackMapShard: %v", err)
	}
	for _, want := range []packMapEntry{mine[0], theirs[0]} {
		at, ok := doc.Offsets[want.sha]
		if !ok {
			t.Fatalf("shard lost the entry for %s (offsets: %v)", want.sha, doc.Offsets)
		}
		if at[1] != want.offset || at[2] != want.size {
			t.Errorf("%s maps to offset %d size %d, want %d/%d", want.sha, at[1], at[2], want.offset, want.size)
		}
		if int(at[0]) >= len(doc.Packs) {
			t.Errorf("%s names pack index %d, but the shard lists %d packs", want.sha, at[0], len(doc.Packs))
		}
	}
}

// TestUpdateCompressedJSON_CreateOnlyProviderSkipsTheDoomedUpdate: a provider
// that refuses If-Match even on a matching ETag can never win a conditional
// update, so declaring it must cost zero doomed attempts (a cold push rewrites
// all 256 pack map shards, and maxCASRetries wasted round trips on each is the
// whole reason to consult the capability). Creation keeps its compare-and-swap,
// which those providers do enforce, and no update is lost either way.
func TestUpdateCompressedJSON_CreateOnlyProviderSkipsTheDoomedUpdate(t *testing.T) {
	client, bucket := testClient(t)
	bucket.rejectIfMatchWrites()
	key := packMapKeyPrefix + "aa.json"
	record := func(capability Capability, packName string) {
		t.Helper()
		if err := updateCompressedJSON(client, capability, key, func(doc *packMapDoc, found bool) error {
			if !found {
				*doc = packMapDoc{Offsets: map[string][]int64{}}
			}
			doc.Version = packMapVersion
			doc.Packs = append(doc.Packs, packName)
			return nil
		}); err != nil {
			t.Fatalf("updateCompressedJSON(%v, %s): %v", capability, packName, err)
		}
	}

	record(CapabilityCreateOnly, "pack-created")
	if got := bucket.ifMatchCount(); got != 0 {
		t.Errorf("creating the document attempted %d If-Match writes, want 0 (an absent key takes If-None-Match: *)", got)
	}
	record(CapabilityCreateOnly, "pack-updated")
	if got := bucket.ifMatchCount(); got != 0 {
		t.Errorf("updating under CapabilityCreateOnly attempted %d If-Match writes, want 0: every one of them can only 412", got)
	}

	// Positive control: the same bucket, declared full-capability, still pays the
	// retries before falling back. That cost is what the declaration removes.
	record(CapabilityFull, "pack-probed")
	if got := bucket.ifMatchCount(); got != maxCASRetries {
		t.Errorf("updating under CapabilityFull attempted %d If-Match writes, want %d", got, maxCASRetries)
	}

	doc, err := readPackMapShard(client, "", "aa")
	if err != nil {
		t.Fatalf("readPackMapShard: %v", err)
	}
	if len(doc.Packs) != 3 {
		t.Errorf("shard lists %v, want all three packs: skipping the doomed attempt must not drop an update", doc.Packs)
	}
}

// TestBuildPack_PinsIndexVersionAndPackSize: the pusher's git config must not
// change the shape of an artifact every other reader depends on.
// pack.indexVersion=1 writes an index neither parsePackIdx nor the browser can
// read, and pack.packSizeLimit splits the output into several packfiles.
func TestBuildPack_PinsIndexVersionAndPackSize(t *testing.T) {
	dir := packTestRepo(t, 2)
	// Incompressible content well past the 1 MiB minimum pack size limit, so a
	// split would actually happen.
	blob := make([]byte, 1<<21)
	for i := range blob {
		binary.LittleEndian.PutUint32(blob[i&^3:], uint32(i)*2654435761)
	}
	if err := os.WriteFile(filepath.Join(dir, "big.bin"), blob, 0644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "big.bin")
	gitRun(t, dir, "commit", "-qm", "big")
	gitRun(t, dir, "config", "pack.indexVersion", "1")
	gitRun(t, dir, "config", "pack.packSizeLimit", "1m")
	t.Setenv("GIT_DIR", filepath.Join(dir, ".git"))
	var objects []string
	for _, sha := range strings.Fields(gitRun(t, dir, "rev-list", "--objects", "--all")) {
		if len(sha) == 40 {
			objects = append(objects, sha)
		}
	}

	built, err := buildPack(objects, false, true)
	if err != nil {
		t.Fatalf("buildPack under a hostile pack config: %v", err)
	}
	if _, err := parsePackIdx(built.idx); err != nil {
		t.Errorf("pack index is not readable: %v", err)
	}
	if len(built.entries) != len(objects) {
		t.Errorf("pack carries %d of %d objects, so the output was split", len(built.entries), len(objects))
	}
}

// TestRepairDanglingHEAD_RepointsToACarriedRef covers a first push made from a
// feature branch: HEAD is published as that branch, the branch itself is never
// pushed, and the symref dangles. ensureRemoteHEAD keeps it (it is not a gitmsg
// branch), so before the repair the bucket stayed broken permanently and the
// browser reported no default branch.
func TestRepairDanglingHEAD_RepointsToACarriedRef(t *testing.T) {
	client, _ := testClient(t)
	dir := packTestRepo(t, 2)
	h := pushHelper(t, client, dir)
	if err := client.Put("HEAD", []byte("ref: refs/heads/feature/x\n")); err != nil {
		t.Fatal(err)
	}
	refs := map[string]string{"refs/heads/main": gitRun(t, dir, "rev-parse", "HEAD")}

	h.repairDanglingHEAD(refs, "refs/heads/feature/x", "refs/heads/main")

	got, err := client.Get("HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "ref: refs/heads/main\n" {
		t.Fatalf("HEAD = %q, want it repointed to the carried refs/heads/main", got)
	}
}

// TestRepairDanglingHEAD_KeepsACarriedHEAD makes sure the repair only fires on a
// dangling symref: a HEAD the bucket can resolve is never rewritten, so a repo
// whose default branch is not "main" keeps its own.
func TestRepairDanglingHEAD_KeepsACarriedHEAD(t *testing.T) {
	client, _ := testClient(t)
	dir := packTestRepo(t, 2)
	h := pushHelper(t, client, dir)
	if err := client.Put("HEAD", []byte("ref: refs/heads/trunk\n")); err != nil {
		t.Fatal(err)
	}
	refs := map[string]string{
		"refs/heads/trunk": gitRun(t, dir, "rev-parse", "HEAD"),
		"refs/heads/main":  gitRun(t, dir, "rev-parse", "HEAD"),
	}

	h.repairDanglingHEAD(refs, "refs/heads/main")

	got, err := client.Get("HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "ref: refs/heads/trunk\n" {
		t.Fatalf("HEAD = %q, want the carried refs/heads/trunk left alone", got)
	}
}

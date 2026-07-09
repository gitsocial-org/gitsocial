// site_repair_test.go - the repair state machine: classifyItemsState over every
// row of the state table (pure, no I/O), then integration against the in-process
// S3 stub (memBucket) seeding real loose commit objects: each interruption
// boundary (items manifest absent, bodies manifest ahead, head-count mismatch,
// tip unreachable via a rewritten branch) is simulated by deleting/rewinding one
// key, one more commit is pushed, and updateSiteItemsIndex must repair to a
// valid lockstepped state without error. Plus two concurrent-writer interleaves
// that must self-heal on the following push.

package objstore

import (
	"crypto/sha1"
	"fmt"
	"testing"
)

// mf builds a COMPLETE steady-state manifest carrying only the fields the
// classifier reads (a finished index has complete=true).
func mf(tip string, headCount int) *siteShardManifest {
	return &siteShardManifest{Version: siteItemsVersion, Tip: tip, Complete: true, Head: siteShardHead{Count: headCount}}
}

// mfIncomplete builds an in-flight (bootstrap not yet at root) manifest:
// complete=false, which the classifier reads as authoritative "bootstrap
// pending" even without a cursor (the incomplete-manifest-no-cursor torn-state signal).
func mfIncomplete(tip string, headCount int) *siteShardManifest {
	m := mf(tip, headCount)
	m.Complete = false
	return m
}

func TestClassifyItemsState_Table(t *testing.T) {
	const t1, t2 = "aaaa", "bbbb"
	pending := &siteItemsCursor{Version: siteItemsVersion, Tip: t1, OldestIndexed: "cccc", Complete: false}
	done := &siteItemsCursor{Version: siteItemsVersion, Tip: t1, OldestIndexed: "cccc", Complete: true}
	cases := []struct {
		name                  string
		items, bodies         *siteShardManifest
		cursor                *siteItemsCursor
		itemsHead, bodiesHead int
		newTip                string
		want                  itemsAction
	}{
		{"fresh bucket: no manifests", nil, nil, nil, 0, 0, t1, actionBootstrap},
		{"items manifest absent, bodies present", nil, mf(t1, 2), nil, 0, 2, t1, actionBootstrap},
		{"bodies manifest absent, items present", mf(t1, 2), nil, nil, 2, 0, t1, actionRepair},
		{"steady state at newTip: no-op", mf(t1, 2), mf(t1, 2), nil, 2, 2, t1, actionNoOp},
		{"lockstepped behind newTip: append", mf(t1, 2), mf(t1, 2), nil, 2, 2, t2, actionAppend},
		{"bodies tip ahead of items", mf(t1, 2), mf(t2, 3), nil, 2, 3, t2, actionRepair},
		{"items tip ahead of bodies", mf(t2, 3), mf(t1, 2), nil, 3, 2, t2, actionRepair},
		{"items head count mismatch", mf(t1, 2), mf(t1, 2), nil, 1, 2, t2, actionRepair},
		{"bodies head count mismatch", mf(t1, 2), mf(t1, 2), nil, 2, 1, t2, actionRepair},
		{"at newTip but heads mismatch: still repair", mf(t1, 2), mf(t1, 2), nil, 2, 1, t1, actionRepair},
		{"tips lockstepped on a stale (possibly unreachable) tip", mf(t1, 2), mf(t1, 2), nil, 2, 2, t2, actionAppend},
		{"cursor pending, newest end at newTip: backfill", mfIncomplete(t1, 2), mfIncomplete(t1, 2), pending, 2, 2, t1, actionBackfill},
		{"cursor pending, newTip advanced: append newest end first", mfIncomplete(t1, 2), mfIncomplete(t1, 2), pending, 2, 2, t2, actionAppend},
		{"cursor pending but corpus torn: repair first", mfIncomplete(t1, 2), mfIncomplete(t2, 3), pending, 2, 3, t2, actionRepair},
		{"cursor pending, head count off: repair first", mfIncomplete(t1, 2), mfIncomplete(t1, 2), pending, 1, 2, t1, actionRepair},
		{"cursor complete: treated as no cursor (no-op at tip)", mf(t1, 2), mf(t1, 2), done, 2, 2, t1, actionNoOp},
		// Torn state: incomplete manifest, NO cursor. Classifies as backfill so
		// the bootstrap resumes rather than freezing.
		{"incomplete manifest, no cursor, at tip: backfill", mfIncomplete(t1, 2), mfIncomplete(t1, 2), nil, 2, 2, t1, actionBackfill},
		{"incomplete manifest, no cursor, newTip advanced: append", mfIncomplete(t1, 2), mfIncomplete(t1, 2), nil, 2, 2, t2, actionAppend},
		{"incomplete manifest, no cursor, torn corpus: repair", mfIncomplete(t1, 2), mfIncomplete(t2, 3), nil, 2, 3, t2, actionRepair},
	}
	for _, tc := range cases {
		if got := classifyItemsState(tc.items, tc.bodies, tc.cursor, tc.itemsHead, tc.bodiesHead, tc.newTip); got != tc.want {
			t.Errorf("%s: classifyItemsState = %d, want %d", tc.name, got, tc.want)
		}
	}
}

// --- integration scaffolding: a real loose-object commit chain in memBucket ---

// emptyTree is git's well-known empty tree sha.
const emptyTree = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

// makeLooseCommit builds a git commit object (sha1-addressed, zlib loose format)
// with the given parent ("" for the root) and message.
func makeLooseCommit(t *testing.T, parent, message string, ts int64) (string, []byte) {
	t.Helper()
	body := "tree " + emptyTree + "\n"
	if parent != "" {
		body += "parent " + parent + "\n"
	}
	body += fmt.Sprintf("author Test User <test@example.com> %d +0000\n", ts)
	body += fmt.Sprintf("committer Test User <test@example.com> %d +0000\n\n", ts)
	body += message
	sha := fmt.Sprintf("%x", sha1.Sum([]byte(fmt.Sprintf("commit %d\x00%s", len(body), body))))
	loose, err := encodeLooseObject("commit", []byte(body))
	if err != nil {
		t.Fatalf("encodeLooseObject: %v", err)
	}
	return sha, loose
}

// seedChain uploads a linear chain of n commits (optionally on top of parent)
// and returns their shas oldest-first. The salt keeps divergent chains' shas
// distinct.
func seedChain(t *testing.T, client *Client, parent, salt string, n int) []string {
	t.Helper()
	shas := make([]string, 0, n)
	for i := 0; i < n; i++ {
		sha, loose := makeLooseCommit(t, parent, fmt.Sprintf("%scommit %d", salt, i), int64(1000+i))
		if err := client.Put("objects/"+sha[:2]+"/"+sha[2:], loose); err != nil {
			t.Fatalf("seed commit %d: %v", i, err)
		}
		shas = append(shas, sha)
		parent = sha
	}
	return shas
}

// corpusShas reads a corpus's full sha sequence (every sealed shard oldest-first,
// then the head) via the given doc reader.
func corpusShas(t *testing.T, client *Client, m *siteShardManifest, dir func(string) string, headKey func(string) string, ext string) []string {
	t.Helper()
	var shas []string
	for _, s := range m.Shards {
		entries, err := readItemsHeadEntries(client, dir(ext)+s.Key)
		if err != nil {
			t.Fatalf("read shard %s: %v", s.Key, err)
		}
		if len(entries) != s.Count {
			t.Errorf("shard %s: %d entries, manifest says %d", s.Key, len(entries), s.Count)
		}
		for _, e := range entries {
			shas = append(shas, e.SHA)
		}
	}
	head, err := readItemsHeadEntries(client, headKey(ext))
	if err != nil {
		t.Fatalf("read head: %v", err)
	}
	for _, e := range head {
		shas = append(shas, e.SHA)
	}
	return shas
}

// assertLockstepState asserts both corpora are present, at wantTip, with head
// counts matching their manifests, identical shard boundaries, and exactly the
// wanted oldest-first sha coverage.
func assertLockstepState(t *testing.T, client *Client, ext string, wantShas []string, wantTip string) {
	t.Helper()
	items, err := readItemsManifest(client, "", ext)
	if err != nil || items == nil {
		t.Fatalf("items manifest: %v (nil=%v)", err, items == nil)
	}
	bodies, err := readBodiesManifest(client, "", ext)
	if err != nil || bodies == nil {
		t.Fatalf("bodies manifest: %v (nil=%v)", err, bodies == nil)
	}
	if items.Tip != wantTip || bodies.Tip != wantTip {
		t.Fatalf("tips not at %s: items=%s bodies=%s", wantTip[:8], items.Tip[:8], bodies.Tip[:8])
	}
	itemsHead, err := readItemsHeadEntries(client, siteItemsHeadKey(ext))
	if err != nil {
		t.Fatalf("items head: %v", err)
	}
	bodiesHead, err := readBodyDocItems(client, bodiesHeadKey(ext))
	if err != nil {
		t.Fatalf("bodies head: %v", err)
	}
	if len(itemsHead) != items.Head.Count || len(bodiesHead) != bodies.Head.Count {
		t.Fatalf("head counts off: items %d/%d bodies %d/%d", len(itemsHead), items.Head.Count, len(bodiesHead), bodies.Head.Count)
	}
	if len(items.Shards) != len(bodies.Shards) {
		t.Fatalf("shard counts differ: items=%d bodies=%d", len(items.Shards), len(bodies.Shards))
	}
	for i := range items.Shards {
		if items.Shards[i].Count != bodies.Shards[i].Count || items.Shards[i].EndTip != bodies.Shards[i].EndTip {
			t.Errorf("shard %d boundary skew: items(%d,%s) bodies(%d,%s)", i, items.Shards[i].Count, items.Shards[i].EndTip[:8], bodies.Shards[i].Count, bodies.Shards[i].EndTip[:8])
		}
	}
	// Both docs carry items:[{sha,...}], so the same sha reader covers bodies.
	for corpus, got := range map[string][]string{
		"items":  corpusShas(t, client, items, siteItemsDir, siteItemsHeadKey, ext),
		"bodies": corpusShas(t, client, bodies, siteBodiesDir, bodiesHeadKey, ext),
	} {
		if len(got) != len(wantShas) {
			t.Fatalf("%s corpus covers %d commits, want %d", corpus, len(got), len(wantShas))
		}
		for i, sha := range wantShas {
			if got[i] != sha {
				t.Fatalf("%s corpus sha %d = %s, want %s", corpus, i, got[i][:8], sha[:8])
			}
		}
	}
}

// mustUpdate runs updateSiteItemsIndex and fails the test on any error (the M2
// contract: with a manifest present no state may error).
func mustUpdate(t *testing.T, client *Client, ext, tip string) {
	t.Helper()
	if err := updateSiteItemsIndex(client, "", ext, tip); err != nil {
		t.Fatalf("updateSiteItemsIndex(%s): %v", tip[:8], err)
	}
}

// rawDoc snapshots a key's stored bytes for later restore (simulating a rewind).
func rawDoc(t *testing.T, client *Client, key string) []byte {
	t.Helper()
	data, err := client.Get(key)
	if err != nil {
		t.Fatalf("get %s: %v", key, err)
	}
	return data
}

func TestRepair_ItemsManifestAbsent(t *testing.T) {
	withTestShardCount(func() {
		client, _ := testClient(t)
		shas := seedChain(t, client, "", "", 6)
		mustUpdate(t, client, "social", shas[5])
		// Interruption before the first items manifest (or the manifest was lost).
		if err := client.Delete(siteItemsManifestKey("social")); err != nil {
			t.Fatalf("delete items manifest: %v", err)
		}
		shas = append(shas, seedChain(t, client, shas[5], "", 1)...)
		mustUpdate(t, client, "social", shas[6])
		assertLockstepState(t, client, "social", shas, shas[6])
	})
}

func TestRepair_BodiesManifestAbsent(t *testing.T) {
	withTestShardCount(func() {
		client, _ := testClient(t)
		shas := seedChain(t, client, "", "", 6)
		mustUpdate(t, client, "pm", shas[5])
		if err := client.Delete(bodiesManifestKey("pm")); err != nil {
			t.Fatalf("delete bodies manifest: %v", err)
		}
		shas = append(shas, seedChain(t, client, shas[5], "", 1)...)
		mustUpdate(t, client, "pm", shas[6])
		assertLockstepState(t, client, "pm", shas, shas[6])
	})
}

func TestRepair_BodiesManifestAhead(t *testing.T) {
	withTestShardCount(func() {
		client, _ := testClient(t)
		shas := seedChain(t, client, "", "", 5)
		mustUpdate(t, client, "social", shas[4])
		// Snapshot the items state at tip A, advance to tip B, then rewind the
		// items manifest + head to A: exactly the state an interruption between
		// the bodies manifest write and the items manifest write leaves.
		oldManifest := rawDoc(t, client, siteItemsManifestKey("social"))
		oldHead := rawDoc(t, client, siteItemsHeadKey("social"))
		shas = append(shas, seedChain(t, client, shas[4], "", 2)...)
		mustUpdate(t, client, "social", shas[6])
		if err := putCompressed(client, siteItemsManifestKey("social"), oldManifest); err != nil {
			t.Fatalf("rewind items manifest: %v", err)
		}
		if err := putCompressed(client, siteItemsHeadKey("social"), oldHead); err != nil {
			t.Fatalf("rewind items head: %v", err)
		}
		shas = append(shas, seedChain(t, client, shas[6], "", 1)...)
		mustUpdate(t, client, "social", shas[7])
		assertLockstepState(t, client, "social", shas, shas[7])
	})
}

func TestRepair_ItemsManifestAhead(t *testing.T) {
	withTestShardCount(func() {
		client, _ := testClient(t)
		shas := seedChain(t, client, "", "", 5)
		mustUpdate(t, client, "social", shas[4])
		// The order forbids this state, but a foreign writer could leave it:
		// rewind the BODIES manifest + head while items stay current.
		oldManifest := rawDoc(t, client, bodiesManifestKey("social"))
		oldHead := rawDoc(t, client, bodiesHeadKey("social"))
		shas = append(shas, seedChain(t, client, shas[4], "", 2)...)
		mustUpdate(t, client, "social", shas[6])
		if err := putCompressed(client, bodiesManifestKey("social"), oldManifest); err != nil {
			t.Fatalf("rewind bodies manifest: %v", err)
		}
		if err := putCompressed(client, bodiesHeadKey("social"), oldHead); err != nil {
			t.Fatalf("rewind bodies head: %v", err)
		}
		shas = append(shas, seedChain(t, client, shas[6], "", 1)...)
		mustUpdate(t, client, "social", shas[7])
		assertLockstepState(t, client, "social", shas, shas[7])
	})
}

func TestRepair_HeadCountMismatch(t *testing.T) {
	withTestShardCount(func() {
		for _, corrupt := range []struct {
			name string
			key  string
			doc  func(tip string) any
		}{
			{"items head truncated", siteItemsHeadKey("social"), func(tip string) any {
				return &siteItemsDoc{Version: siteItemsVersion, Tip: tip, Items: nil}
			}},
			{"bodies head truncated", bodiesHeadKey("social"), func(tip string) any {
				return &siteBodyIndex{Version: siteItemsVersion, Tip: tip, Items: nil}
			}},
		} {
			client, _ := testClient(t)
			shas := seedChain(t, client, "", "", 6)
			mustUpdate(t, client, "social", shas[5])
			// Interruption between a head put and its manifest put: the live head
			// no longer matches the manifest's head count.
			comp, err := compressJSON(corrupt.doc(shas[5]), brotliQualityFull)
			if err != nil {
				t.Fatalf("%s: compress: %v", corrupt.name, err)
			}
			if err := putCompressed(client, corrupt.key, comp); err != nil {
				t.Fatalf("%s: corrupt head: %v", corrupt.name, err)
			}
			shas = append(shas, seedChain(t, client, shas[5], "", 1)...)
			mustUpdate(t, client, "social", shas[6])
			assertLockstepState(t, client, "social", shas, shas[6])
		}
	})
}

func TestRepair_TipUnreachableAfterRewrite(t *testing.T) {
	withTestShardCount(func() {
		client, _ := testClient(t)
		oldShas := seedChain(t, client, "", "old-", 6)
		mustUpdate(t, client, "social", oldShas[5])
		oldManifest, err := readItemsManifest(client, "", "social")
		if err != nil || oldManifest == nil {
			t.Fatalf("read old manifest: %v", err)
		}
		// History rewrite: a force-push replaces the branch with a divergent
		// chain that shares no commit with the artifacts.
		newShas := seedChain(t, client, "", "new-", 7)
		mustUpdate(t, client, "social", newShas[6])
		assertLockstepState(t, client, "social", newShas, newShas[6])
		// The only path that discards shards: the stale ones are dropped from
		// the manifest.
		rebuilt, _ := readItemsManifest(client, "", "social")
		for _, s := range rebuilt.Shards {
			for _, old := range oldManifest.Shards {
				if s.Key == old.Key {
					t.Errorf("stale shard %s survived the rewrite reset", s.Key)
				}
			}
		}
	})
}

func TestRepair_ConcurrentAppends_LastWriterStale(t *testing.T) {
	withTestShardCount(func() {
		client, _ := testClient(t)
		shas := seedChain(t, client, "", "", 6)
		mustUpdate(t, client, "social", shas[5])
		items0, _ := readItemsManifest(client, "", "social")
		bodies0, _ := readBodiesManifest(client, "", "social")
		itemsHead0, _ := readItemsHeadEntries(client, siteItemsHeadKey("social"))
		bodiesHead0, _ := readBodyDocItems(client, bodiesHeadKey("social"))
		shas = append(shas, seedChain(t, client, shas[5], "", 2)...)
		// Writer 1 (fast) appends the whole gap to tip B; writer 2 (slow, read
		// the same pre-push state) appends only to tip A and lands LAST,
		// clobbering the manifests back to A while the branch is at B.
		mustUpdate(t, client, "social", shas[7])
		gapA, _, _, err := walkBucketItems(client, "", shas[6], map[string]bool{items0.Tip: true}, siteItemsWalkBudget)
		if err != nil {
			t.Fatalf("walk writer-2 gap: %v", err)
		}
		if err := putGapArtifacts(client, "", "social", shas[6], gapA, items0, bodies0, itemsHead0, bodiesHead0, true); err != nil {
			t.Fatalf("writer-2 stale append: %v", err)
		}
		// The following push self-heals to the branch tip.
		mustUpdate(t, client, "social", shas[7])
		assertLockstepState(t, client, "social", shas, shas[7])
	})
}

func TestRepair_ConcurrentAppends_TornHeads(t *testing.T) {
	withTestShardCount(func() {
		client, _ := testClient(t)
		shas := seedChain(t, client, "", "", 6)
		mustUpdate(t, client, "social", shas[5])
		items0, _ := readItemsManifest(client, "", "social")
		bodies0, _ := readBodiesManifest(client, "", "social")
		itemsHead0, _ := readItemsHeadEntries(client, siteItemsHeadKey("social"))
		bodiesHead0, _ := readBodyDocItems(client, bodiesHeadKey("social"))
		shas = append(shas, seedChain(t, client, shas[5], "", 2)...)
		// Writer 1 walks its gap to tip B (8 commits total = two full test
		// shards, head empty) and gets through its head writes; writer 2's whole
		// sequence for tip A (head of 3) lands in between; writer 1 then writes
		// its manifests. Result: manifests at B, heads at A (a torn state whose
		// head counts mismatch both manifests).
		gapB, _, _, err := walkBucketItems(client, "", shas[7], map[string]bool{items0.Tip: true}, siteItemsWalkBudget)
		if err != nil {
			t.Fatalf("walk writer-1 gap: %v", err)
		}
		gapBMeta := make([]siteMetaEntry, 0, len(gapB))
		gapBBodies := make([]siteBodyEntry, 0, len(gapB))
		for _, w := range gapB {
			gapBMeta = append(gapBMeta, metaOf(w))
			gapBBodies = append(gapBBodies, bodyOf(w))
		}
		bodiesPlan, err := planBodiesAppend(client, "", "social", gapBBodies, bodiesHead0, bodies0)
		if err != nil {
			t.Fatalf("writer-1 bodies plan: %v", err)
		}
		itemsPlan, err := planItemsAppend(client, "", "social", gapBMeta, itemsHead0, items0)
		if err != nil {
			t.Fatalf("writer-1 items plan: %v", err)
		}
		if err := putBodiesHead(client, "", "social", shas[7], &bodiesPlan); err != nil {
			t.Fatalf("writer-1 bodies head: %v", err)
		}
		if err := putItemsHead(client, "", "social", shas[7], &itemsPlan); err != nil {
			t.Fatalf("writer-1 items head: %v", err)
		}
		// Writer 2's full sequence for tip A interleaves here.
		gapA, _, _, err := walkBucketItems(client, "", shas[6], map[string]bool{items0.Tip: true}, siteItemsWalkBudget)
		if err != nil {
			t.Fatalf("walk writer-2 gap: %v", err)
		}
		if err := putGapArtifacts(client, "", "social", shas[6], gapA, items0, bodies0, itemsHead0, bodiesHead0, true); err != nil {
			t.Fatalf("writer-2 append: %v", err)
		}
		// Writer 1 finishes with its manifest writes.
		total, err := putBodiesManifest(client, "", "social", shas[7], bodiesPlan, true)
		if err != nil {
			t.Fatalf("writer-1 bodies manifest: %v", err)
		}
		if err := putItemsManifest(client, "", "social", shas[7], itemsPlan, total, true); err != nil {
			t.Fatalf("writer-1 items manifest: %v", err)
		}
		// The following push self-heals the torn state.
		shas = append(shas, seedChain(t, client, shas[7], "", 1)...)
		mustUpdate(t, client, "social", shas[8])
		assertLockstepState(t, client, "social", shas, shas[8])
	})
}

func TestRepair_NoOpAndAppendStillWork(t *testing.T) {
	withTestShardCount(func() {
		client, bucket := testClient(t)
		shas := seedChain(t, client, "", "", 5)
		mustUpdate(t, client, "social", shas[4])
		assertLockstepState(t, client, "social", shas, shas[4])
		// No-op: a re-push at the same tip rewrites nothing.
		manifestPuts := bucket.putCount(siteItemsManifestKey("social"))
		mustUpdate(t, client, "social", shas[4])
		if got := bucket.putCount(siteItemsManifestKey("social")); got != manifestPuts {
			t.Errorf("no-op re-push rewrote the items manifest (%d -> %d PUTs)", manifestPuts, got)
		}
		// Append: one more commit advances both corpora and reuses the sealed shard.
		items0, _ := readItemsManifest(client, "", "social")
		shardKey := siteItemsDir("social") + items0.Shards[0].Key
		shardPuts := bucket.putCount(shardKey)
		shas = append(shas, seedChain(t, client, shas[4], "", 1)...)
		mustUpdate(t, client, "social", shas[5])
		assertLockstepState(t, client, "social", shas, shas[5])
		if got := bucket.putCount(shardKey); got != shardPuts {
			t.Errorf("append re-PUT a sealed shard (%d -> %d PUTs)", shardPuts, got)
		}
	})
}

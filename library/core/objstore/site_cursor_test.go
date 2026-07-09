// site_cursor_test.go - the resumable budget bootstrap: with a test-lowered walk
// budget, a branch larger than the budget bootstraps over many pushes. Push 1
// yields a servable newest prefix plus a cursor; each later push prepends the
// next older segment; the final push reaches the root, clears the cursor, and
// marks the manifests complete. Asserts every intermediate manifest is a valid
// newest-first prefix (contiguous shards ending at the head, tip = branch tip,
// newest window resolvable), that the completed index is byte-identical to (and
// re-PUTs nothing on) a subsequent rebuild, the tip-advanced-mid-bootstrap row
// (APPEND then BACKFILL resumes), and idempotent recovery from an interruption at
// each write boundary within a backfill segment.

package objstore

import "testing"

// withTestWalkBudget runs fn with siteItemsWalkBudget lowered to budget,
// restoring it after — so the multi-push bootstrap is exercised on tiny corpora.
func withTestWalkBudget(budget int, fn func()) {
	prev := siteItemsWalkBudget
	siteItemsWalkBudget = budget
	defer func() { siteItemsWalkBudget = prev }()
	fn()
}

// assertNewestFirstPrefix asserts a partial (or complete) manifest of the
// "social" ext is a valid servable newest-first prefix: both corpora lockstepped
// at wantTip, their shard boundaries identical, the covered shas are exactly the
// newest len(coverage) of the branch (contiguous, ending at the tip via the
// head), and complete matches.
func assertNewestFirstPrefix(t *testing.T, client *Client, allShas []string, wantCovered int, wantTip string, wantComplete bool) {
	t.Helper()
	const ext = "social"
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
	if items.Complete != wantComplete || bodies.Complete != wantComplete {
		t.Fatalf("complete flags: items=%v bodies=%v, want %v", items.Complete, bodies.Complete, wantComplete)
	}
	if len(items.Shards) != len(bodies.Shards) {
		t.Fatalf("shard counts differ: items=%d bodies=%d", len(items.Shards), len(bodies.Shards))
	}
	for i := range items.Shards {
		if items.Shards[i].Count != bodies.Shards[i].Count || items.Shards[i].EndTip != bodies.Shards[i].EndTip {
			t.Errorf("shard %d boundary skew: items(%d,%s) bodies(%d,%s)", i, items.Shards[i].Count, items.Shards[i].EndTip[:8], bodies.Shards[i].Count, bodies.Shards[i].EndTip[:8])
		}
	}
	got := corpusShas(t, client, items, siteItemsDir, siteItemsHeadKey, ext)
	// The covered range is the newest wantCovered commits of the branch, oldest
	// first: allShas is oldest-first, so it is the tail slice.
	want := allShas[len(allShas)-wantCovered:]
	if len(got) != len(want) {
		t.Fatalf("covered %d commits, want %d (of %d)", len(got), len(want), len(allShas))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("covered sha %d = %s, want %s", i, got[i][:8], want[i][:8])
		}
	}
	// Newest-first prefix ⇒ the head carries the tip and the resolvable newest
	// window; the last covered sha is the branch tip.
	if got[len(got)-1] != wantTip {
		t.Fatalf("newest covered sha %s is not the tip %s", got[len(got)-1][:8], wantTip[:8])
	}
}

// shardKeySet reads the "social" corpus's ordered shard keys from its items
// manifest.
func shardKeySet(t *testing.T, client *Client) []string {
	t.Helper()
	m, err := readItemsManifest(client, "", "social")
	if err != nil || m == nil {
		t.Fatalf("items manifest: %v (nil=%v)", err, m == nil)
	}
	keys := make([]string, len(m.Shards))
	for i, s := range m.Shards {
		keys[i] = s.Key
	}
	return keys
}

func TestBootstrap_ResumesAcrossPushesToRoot(t *testing.T) {
	withTestShardCount(func() {
		withTestWalkBudget(5, func() {
			client, bucket := testClient(t)
			shas := seedChain(t, client, "", "", 13) // > budget: needs several pushes
			tip := shas[12]

			// Push 1: BOOTSTRAP seals the newest budget prefix + a cursor.
			mustUpdate(t, client, "social", tip)
			c1, err := readItemsCursor(client, "", "social")
			if err != nil || c1 == nil {
				t.Fatalf("push 1 left no cursor: %v (nil=%v)", err, c1 == nil)
			}
			if c1.Complete {
				t.Fatalf("push 1 cursor already complete")
			}
			assertNewestFirstPrefix(t, client, shas, 5, tip, false)
			if c1.OldestIndexed != shas[8] {
				t.Fatalf("push 1 oldestIndexed = %s, want %s (commit 8)", c1.OldestIndexed[:8], shas[8][:8])
			}

			// Push 2: BACKFILL prepends the next older budget segment.
			mustUpdate(t, client, "social", tip)
			assertNewestFirstPrefix(t, client, shas, 10, tip, false)
			c2, _ := readItemsCursor(client, "", "social")
			if c2 == nil || c2.OldestIndexed != shas[3] {
				t.Fatalf("push 2 oldestIndexed wrong: %v", c2)
			}

			// Push 3: BACKFILL reaches the root, clears the cursor, marks complete.
			mustUpdate(t, client, "social", tip)
			assertNewestFirstPrefix(t, client, shas, 13, tip, true)
			assertLockstepState(t, client, "social", shas, tip)
			if c3, _ := readItemsCursor(client, "", "social"); c3 != nil {
				t.Fatalf("push 3 did not clear the cursor: %+v", c3)
			}

			// A subsequent rebuild re-PUTs nothing (skip-existing) and leaves every
			// shard key byte-identical.
			before := shardKeySet(t, client)
			puts := map[string]int{}
			for _, k := range before {
				puts[k] = bucket.putCount(siteItemsDir("social") + k)
			}
			if err := rebuildSiteItems(client, "", map[string]string{"refs/heads/gitmsg/social": tip}); err != nil {
				t.Fatalf("rebuild after complete: %v", err)
			}
			after := shardKeySet(t, client)
			if len(after) != len(before) {
				t.Fatalf("rebuild changed shard count: %d -> %d", len(before), len(after))
			}
			for i := range before {
				if after[i] != before[i] {
					t.Errorf("rebuild changed shard %d key: %s -> %s", i, before[i], after[i])
				}
				if got := bucket.putCount(siteItemsDir("social") + before[i]); got != puts[before[i]] {
					t.Errorf("rebuild re-PUT sealed shard %s (%d -> %d)", before[i], puts[before[i]], got)
				}
			}
		})
	})
}

func TestBootstrap_TipAdvancedMidBootstrap(t *testing.T) {
	withTestShardCount(func() {
		withTestWalkBudget(5, func() {
			client, _ := testClient(t)
			shas := seedChain(t, client, "", "", 12)
			// Push 1 bootstraps the newest prefix of the 12-commit branch.
			mustUpdate(t, client, "social", shas[11])
			assertNewestFirstPrefix(t, client, shas, 5, shas[11], false)
			c1, _ := readItemsCursor(client, "", "social")

			// The branch advances before the backfill finishes: APPEND owns the
			// newest end, keeps the manifest incomplete, and bumps cursor.tip while
			// leaving oldestIndexed for BACKFILL to resume from.
			shas = append(shas, seedChain(t, client, shas[11], "", 2)...)
			newTip := shas[13]
			mustUpdate(t, client, "social", newTip)
			assertNewestFirstPrefix(t, client, shas, 7, newTip, false)
			c2, _ := readItemsCursor(client, "", "social")
			if c2 == nil || c2.Tip != newTip || c2.OldestIndexed != c1.OldestIndexed {
				t.Fatalf("append did not bump only cursor.tip: c1=%+v c2=%+v", c1, c2)
			}

			// BACKFILL resumes from the unchanged oldestIndexed and drives to root.
			for i := 0; i < 5; i++ {
				mustUpdate(t, client, "social", newTip)
				if c, _ := readItemsCursor(client, "", "social"); c == nil {
					break
				}
			}
			if c, _ := readItemsCursor(client, "", "social"); c != nil {
				t.Fatalf("bootstrap never completed after tip advance: %+v", c)
			}
			assertNewestFirstPrefix(t, client, shas, len(shas), newTip, true)
			assertLockstepState(t, client, "social", shas, newTip)
		})
	})
}

// TestBootstrap_LostCursorResumes covers the lost-cursor torn state: a BOOTSTRAP wrote
// both manifests (complete:false) but its cursor PUT was lost, so the bucket has
// incomplete manifests and NO cursor. manifest.Complete is authoritative: the push
// reconstructs the cursor from the manifest's oldest sealed shard and resumes
// backfill until the root is reached and complete flips true.
func TestBootstrap_LostCursorResumes(t *testing.T) {
	withTestShardCount(func() {
		withTestWalkBudget(4, func() {
			client, _ := testClient(t)
			shas := seedChain(t, client, "", "", 13) // > budget: bootstrap is multi-push
			tip := shas[12]

			// Push 1 bootstraps the newest prefix and leaves a cursor.
			mustUpdate(t, client, "social", tip)
			assertNewestFirstPrefix(t, client, shas, 4, tip, false)
			if c, _ := readItemsCursor(client, "", "social"); c == nil {
				t.Fatalf("push 1 left no cursor")
			}

			// Simulate the torn state: the cursor PUT was lost, leaving the
			// incomplete manifests behind with no cursor to resume from.
			if err := client.Delete(siteItemsCursorKey("social")); err != nil {
				t.Fatalf("delete cursor: %v", err)
			}
			if c, _ := readItemsCursor(client, "", "social"); c != nil {
				t.Fatalf("cursor still present after delete")
			}

			// The very next push must NOT classify NO-OP: it reconstructs the cursor
			// and resumes backfill. Assert coverage strictly grows on this push.
			mustUpdate(t, client, "social", tip)
			assertNewestFirstPrefix(t, client, shas, 8, tip, false)

			// Drive to the root; the manifests reach complete:true and the cursor
			// clears — the freeze is gone.
			for i := 0; i < 6; i++ {
				mustUpdate(t, client, "social", tip)
				if c, _ := readItemsCursor(client, "", "social"); c == nil {
					break
				}
			}
			if c, _ := readItemsCursor(client, "", "social"); c != nil {
				t.Fatalf("bootstrap never completed after cursor loss: %+v", c)
			}
			assertLockstepState(t, client, "social", shas, tip)
			assertNewestFirstPrefix(t, client, shas, 13, tip, true)
		})
	})
}

func TestBootstrap_BackfillInterruptionRecovers(t *testing.T) {
	// Each interruption point within a backfill segment must recover idempotently
	// on the next push. The pinned write order is: bodies shards, items shards,
	// bodies manifest, items manifest, cursor. A boundary is modeled by running a
	// full backfill push (seals + advances everything) then rewinding the keys the
	// interruption would not yet have written:
	//   - "shards only": rewind both manifests + cursor (sealed shards linger
	//     unreferenced and are re-created byte-identically on retry).
	//   - "bodies manifest only": rewind the items manifest + cursor (a torn,
	//     bodies-ahead state).
	//   - "both manifests, no cursor": rewind only the cursor (the next push
	//     re-walks the same skip-existing segment and re-writes the same manifests).
	withTestShardCount(func() {
		withTestWalkBudget(5, func() {
			for _, tc := range []struct {
				name           string
				rewindItemsMF  bool
				rewindBodiesMF bool
			}{
				{"shards only", true, true},
				{"bodies manifest only", true, false},
				{"both manifests, no cursor", false, false},
			} {
				t.Run(tc.name, func(t *testing.T) {
					client, _ := testClient(t)
					shas := seedChain(t, client, "", "", 13)
					tip := shas[12]
					mustUpdate(t, client, "social", tip) // push 1: bootstrap prefix
					// Snapshot the pre-segment state, then run one backfill push.
					preItems := rawDoc(t, client, siteItemsManifestKey("social"))
					preBodies := rawDoc(t, client, bodiesManifestKey("social"))
					preCursor := rawDoc(t, client, siteItemsCursorKey("social"))
					mustUpdate(t, client, "social", tip) // push 2: one backfill segment
					if tc.rewindItemsMF {
						if err := putCompressed(client, siteItemsManifestKey("social"), preItems); err != nil {
							t.Fatalf("rewind items manifest: %v", err)
						}
					}
					if tc.rewindBodiesMF {
						if err := putCompressed(client, bodiesManifestKey("social"), preBodies); err != nil {
							t.Fatalf("rewind bodies manifest: %v", err)
						}
					}
					if err := putCompressed(client, siteItemsCursorKey("social"), preCursor); err != nil {
						t.Fatalf("rewind cursor: %v", err)
					}
					// Drive to the root; recovery may take a few more pushes.
					for i := 0; i < 6; i++ {
						mustUpdate(t, client, "social", tip)
						if c, _ := readItemsCursor(client, "", "social"); c == nil {
							break
						}
					}
					if c, _ := readItemsCursor(client, "", "social"); c != nil {
						t.Fatalf("did not reach root after interruption: %+v", c)
					}
					assertLockstepState(t, client, "social", shas, tip)
					assertNewestFirstPrefix(t, client, shas, 13, tip, true)
				})
			}
		})
	})
}

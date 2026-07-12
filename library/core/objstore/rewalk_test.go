// rewalk_test.go - the rewalk verdict: a run that completes an items index does
// NOT get re-walked from scratch by a later run (NO-OP / bounded APPEND); a run
// that dies mid-bootstrap before its manifest lands legitimately restarts the
// bootstrap (which is why the GET retry — not a state-machine change — is the
// real fix for the production "run B rewalked review from scratch" report).

package objstore

import "testing"

// countWalkedCommits runs a "social" update and returns how many commits the
// walk read (peak "site index social" done).
func countWalkedCommits(t *testing.T, client *Client, tip string) int {
	t.Helper()
	const ext = "social"
	var peak int
	sp := &siteProgress{ext: ext, progress: func(phase string, done, total int) {
		if phase == "site index "+ext && done > peak {
			peak = done
		}
	}}
	if err := updateSiteItemsIndex(client, "", ext, tip, sp); err != nil {
		t.Fatalf("updateSiteItemsIndex: %v", err)
	}
	return peak
}

// TestRewalk_CompletedIndexIsNotRewalked: once a single-push bootstrap completes
// (a branch smaller than the budget, which reaches the root and writes a complete
// manifest in one run), a later run at the same tip classifies NO-OP and walks
// ZERO commits — the completed shard work is reused, never re-walked. This is the
// invariant the production report seemed to violate; it does not.
func TestRewalk_CompletedIndexIsNotRewalked(t *testing.T) {
	withTestShardCount(func() {
		withTestWalkBudget(50000, func() { // budget >> branch: one-push bootstrap
			client, _ := testClient(t)
			const n = 30
			shas := seedChain(t, client, "", "", n)
			tip := shas[n-1]

			// Run A: bootstrap the whole branch to a complete manifest.
			if walked := countWalkedCommits(t, client, tip); walked != n {
				t.Fatalf("run A walked %d, want the full %d (initial bootstrap)", walked, n)
			}
			m, err := readItemsManifest(client, "", "social")
			if err != nil || m == nil || !m.Complete {
				t.Fatalf("run A did not leave a complete manifest: %v (nil=%v)", err, m == nil)
			}

			// Run B: same tip. It must NOT re-walk — the completed index is reused.
			if walked := countWalkedCommits(t, client, tip); walked != 0 {
				t.Errorf("run B re-walked %d commits at an unchanged, completed tip; want 0 (NO-OP)", walked)
			}
		})
	})
}

// TestRewalk_AdvancedTipAppendsBoundedGap: a later run whose tip advanced walks
// only the bounded gap (the new commits), never the whole branch from scratch.
func TestRewalk_AdvancedTipAppendsBoundedGap(t *testing.T) {
	withTestShardCount(func() {
		withTestWalkBudget(50000, func() {
			client, _ := testClient(t)
			base := seedChain(t, client, "", "", 20)
			baseTip := base[len(base)-1]
			if walked := countWalkedCommits(t, client, baseTip); walked != 20 {
				t.Fatalf("initial bootstrap walked %d, want 20", walked)
			}
			// Advance the branch by 3 commits.
			ext := seedChain(t, client, baseTip, "more", 3)
			newTip := ext[len(ext)-1]
			if walked := countWalkedCommits(t, client, newTip); walked != 3 {
				t.Errorf("advanced-tip run walked %d, want only the 3-commit gap (bounded APPEND, not a rewalk)", walked)
			}
		})
	})
}

// TestRewalk_DiedBeforeManifestRestartsBootstrap: a bootstrap that never wrote
// its manifest (the production failure: the walk died on a 503 before the manifest
// landed) leaves items==nil, so the next run legitimately restarts BOOTSTRAP. This
// documents that the restart is CORRECT given a lost walk — the fix for the
// production report is the GET retry that keeps the walk alive, not a change here.
func TestRewalk_DiedBeforeManifestRestartsBootstrap(t *testing.T) {
	withTestShardCount(func() {
		withTestWalkBudget(50000, func() {
			client, _ := testClient(t)
			const n = 15
			shas := seedChain(t, client, "", "", n)
			tip := shas[n-1]
			// No prior push: items manifest is absent (the state a pre-manifest crash
			// leaves — the manifest is the only commit point and lands last).
			m, err := readItemsManifest(client, "", "social")
			if err != nil || m != nil {
				t.Fatalf("precondition: expected no manifest, got %v (err %v)", m, err)
			}
			// The next run bootstraps from scratch — expected, since the walk was lost.
			if walked := countWalkedCommits(t, client, tip); walked != n {
				t.Errorf("restart walked %d, want the full %d (a lost pre-manifest walk correctly restarts)", walked, n)
			}
		})
	})
}

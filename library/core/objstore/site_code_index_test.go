// site_code_index_test.go - the CODE items index (one merged corpus across every
// code branch, metadata-only, no bodies). Mirrors the ext site_items tests over
// an in-process memBucket seeding real loose commit objects: bootstrap across
// pushes with a small budget, incremental append, multi-branch dedup +
// attribution, and force-push repair (a rewrite dropping a commit from one branch
// while it survives on another rebuilds the affected shards).

package objstore

import "testing"

// codeTipsOf builds the code-tip list from a refname→sha map with the given
// default branch (the same shape codeBranchTips takes).
func codeTipsOf(refs map[string]string, defaultBranch string) []codeTip {
	return codeBranchTips(refs, defaultBranch)
}

// mustUpdateCode runs updateSiteCodeIndex over the current code tips (default
// branch = "main") and fails on any error (the code corpus's M2 contract: with a
// manifest present no state may error).
func mustUpdateCode(t *testing.T, client *Client, refs map[string]string, defaultBranch string) {
	t.Helper()
	// defaultBranch is threaded from the caller so the attribution tests can vary
	// it; it is validated against the refs so an absent default is a no-op (every
	// commit then attributes to the first branch that reached it).
	if _, ok := refs["refs/heads/"+defaultBranch]; !ok && defaultBranch != "" {
		defaultBranch = ""
	}
	if err := updateSiteCodeIndex(client, "", codeTipsOf(refs, defaultBranch), defaultBranch, nil); err != nil {
		t.Fatalf("updateSiteCodeIndex: %v", err)
	}
}

// codeCorpusShas reads the code corpus's full sha sequence oldest-first (every
// sealed shard then the head).
func codeCorpusShas(t *testing.T, client *Client) []string {
	t.Helper()
	m, err := readItemsManifest(client, "", siteCodeExt)
	if err != nil || m == nil {
		t.Fatalf("code manifest: %v (nil=%v)", err, m == nil)
	}
	return corpusShas(t, client, m, siteItemsDir, siteItemsHeadKey, siteCodeExt)
}

// codeCorpusEntries reads every code entry (sealed shards then head) as metadata
// entries, oldest-first, so a test can assert per-commit Branch attribution.
func codeCorpusEntries(t *testing.T, client *Client) []siteMetaEntry {
	t.Helper()
	m, err := readItemsManifest(client, "", siteCodeExt)
	if err != nil || m == nil {
		t.Fatalf("code manifest: %v (nil=%v)", err, m == nil)
	}
	out := make([]siteMetaEntry, 0, len(m.Shards))
	for _, s := range m.Shards {
		entries, err := readItemsHeadEntries(client, siteItemsDir(siteCodeExt)+s.Key)
		if err != nil {
			t.Fatalf("read shard %s: %v", s.Key, err)
		}
		out = append(out, entries...)
	}
	head, err := readItemsHeadEntries(client, siteItemsHeadKey(siteCodeExt))
	if err != nil {
		t.Fatalf("read head: %v", err)
	}
	return append(out, head...)
}

// branchOf returns the attributed branch of a sha in a code entry list ("" when
// absent).
func branchOf(entries []siteMetaEntry, sha string) string {
	for _, e := range entries {
		if e.SHA == sha {
			return e.Branch
		}
	}
	return ""
}

// TestCode_BootstrapSingleBranch: a linear main branch indexes in one push, and
// every commit is attributed to the default branch.
func TestCode_BootstrapSingleBranch(t *testing.T) {
	withTestShardCount(func() {
		client, _ := testClient(t)
		shas := seedChain(t, client, "", "", 6)
		refs := map[string]string{"refs/heads/main": shas[5]}
		mustUpdateCode(t, client, refs, "main")
		m, err := readItemsManifest(client, "", siteCodeExt)
		if err != nil || m == nil {
			t.Fatalf("manifest: %v (nil=%v)", err, m == nil)
		}
		if !m.Complete {
			t.Fatalf("small branch should complete in one push")
		}
		if m.BodiesBytes != 0 {
			t.Fatalf("code corpus must carry NO bodies (bodiesBytes=%d)", m.BodiesBytes)
		}
		// No bodies corpus artifacts written.
		if bm, _ := readBodiesManifest(client, "", siteCodeExt); bm != nil {
			t.Fatalf("code corpus wrote a bodies manifest, must be metadata-only")
		}
		got := codeCorpusShas(t, client)
		if len(got) != 6 {
			t.Fatalf("covered %d commits, want 6", len(got))
		}
		entries := codeCorpusEntries(t, client)
		for _, sha := range shas {
			if b := branchOf(entries, sha); b != "main" {
				t.Fatalf("commit %s attributed %q, want main", sha[:8], b)
			}
		}
	})
}

// TestCode_MultiBranchDedupAndAttribution: a feature branch off main; the shared
// base is attributed to main (default wins), the feature-only commits to the
// feature branch, and every commit appears exactly once.
func TestCode_MultiBranchDedupAndAttribution(t *testing.T) {
	withTestShardCount(func() {
		client, _ := testClient(t)
		base := seedChain(t, client, "", "", 3) // shared main history
		mainTip := base[2]
		mainExtra := seedChain(t, client, mainTip, "main-", 2) // main advances
		featTip := seedChain(t, client, mainTip, "feat-", 2)   // feature off the base
		refs := map[string]string{
			"refs/heads/main":      mainExtra[1],
			"refs/heads/feature/x": featTip[1],
		}
		mustUpdateCode(t, client, refs, "main")
		got := codeCorpusShas(t, client)
		// base(3) + mainExtra(2) + featTip(2) = 7 distinct commits, no dup.
		if len(got) != 7 {
			t.Fatalf("covered %d commits, want 7 (dedup failed?)", len(got))
		}
		entries := codeCorpusEntries(t, client)
		for _, sha := range append(append([]string{}, base...), mainExtra...) {
			if b := branchOf(entries, sha); b != "main" {
				t.Fatalf("main commit %s attributed %q, want main", sha[:8], b)
			}
		}
		for _, sha := range featTip {
			if b := branchOf(entries, sha); b != "feature/x" {
				t.Fatalf("feature commit %s attributed %q, want feature/x", sha[:8], b)
			}
		}
	})
}

// TestCode_BootstrapResumesAcrossPushes: a branch larger than the walk budget
// bootstraps over several pushes, each prepending an older segment, until the
// root is reached and the manifest is complete with full coverage.
func TestCode_BootstrapResumesAcrossPushes(t *testing.T) {
	withTestShardCount(func() {
		withTestWalkBudget(5, func() {
			client, _ := testClient(t)
			shas := seedChain(t, client, "", "", 13)
			refs := map[string]string{"refs/heads/main": shas[12]}

			mustUpdateCode(t, client, refs, "main")
			c1, err := readItemsCursor(client, "", siteCodeExt)
			if err != nil || c1 == nil {
				t.Fatalf("push 1 left no cursor: %v (nil=%v)", err, c1 == nil)
			}
			m1, _ := readItemsManifest(client, "", siteCodeExt)
			if m1 == nil || m1.Complete {
				t.Fatalf("push 1 manifest must be incomplete")
			}

			// Backfill pushes until complete (bounded).
			for i := 0; i < 10; i++ {
				m, _ := readItemsManifest(client, "", siteCodeExt)
				if m != nil && m.Complete {
					break
				}
				mustUpdateCode(t, client, refs, "main")
			}
			m, err := readItemsManifest(client, "", siteCodeExt)
			if err != nil || m == nil || !m.Complete {
				t.Fatalf("bootstrap never completed: %v (nil=%v)", err, m == nil)
			}
			if c, _ := readItemsCursor(client, "", siteCodeExt); c != nil {
				t.Fatalf("completed bootstrap left a cursor")
			}
			got := codeCorpusShas(t, client)
			if len(got) != 13 {
				t.Fatalf("covered %d commits, want 13", len(got))
			}
			for i, sha := range shas {
				if got[i] != sha {
					t.Fatalf("covered sha %d = %s, want %s", i, got[i][:8], sha[:8])
				}
			}
		})
	})
}

// TestCode_AppendNewCommits: a second push with the branch advanced appends only
// the new commits, keeping the sealed prefix.
func TestCode_AppendNewCommits(t *testing.T) {
	withTestShardCount(func() {
		client, _ := testClient(t)
		shas := seedChain(t, client, "", "", 6)
		refs := map[string]string{"refs/heads/main": shas[5]}
		mustUpdateCode(t, client, refs, "main")
		shas = append(shas, seedChain(t, client, shas[5], "", 3)...)
		refs["refs/heads/main"] = shas[8]
		mustUpdateCode(t, client, refs, "main")
		got := codeCorpusShas(t, client)
		if len(got) != 9 {
			t.Fatalf("covered %d commits, want 9", len(got))
		}
		for i, sha := range shas {
			if got[i] != sha {
				t.Fatalf("covered sha %d = %s, want %s", i, got[i][:8], sha[:8])
			}
		}
	})
}

// TestCode_NoOpWhenUnchanged: a re-push with identical tips is a NO-OP (the code
// manifest is unchanged).
func TestCode_NoOpWhenUnchanged(t *testing.T) {
	withTestShardCount(func() {
		client, bucket := testClient(t)
		shas := seedChain(t, client, "", "", 6)
		refs := map[string]string{"refs/heads/main": shas[5]}
		mustUpdateCode(t, client, refs, "main")
		before := bucket.putCount(siteItemsManifestKey(siteCodeExt))
		mustUpdateCode(t, client, refs, "main")
		after := bucket.putCount(siteItemsManifestKey(siteCodeExt))
		if after != before {
			t.Fatalf("NO-OP re-wrote the manifest: %d -> %d PUTs", before, after)
		}
	})
}

// TestCode_ForcePushRebaseRepairsShards: a feature branch's commit is force-moved
// (rebased) so its old sha is dropped from that branch. The corpus is defined as
// "reachable from any current code tip", so if the commit is no longer reachable
// from ANY tip its shard membership changed and the repair path rebuilds the
// affected (head) shards. When it remains reachable from another branch it stays.
func TestCode_ForcePushRebaseRepairsShards(t *testing.T) {
	withTestShardCount(func() {
		client, _ := testClient(t)
		// main: linear base. feature: two commits off the base tip.
		base := seedChain(t, client, "", "", 3)
		mainTip := base[2]
		feat := seedChain(t, client, mainTip, "feat-", 2)
		refs := map[string]string{
			"refs/heads/main":      mainTip,
			"refs/heads/feature/x": feat[1],
		}
		mustUpdateCode(t, client, refs, "main")
		if got := len(codeCorpusShas(t, client)); got != 5 {
			t.Fatalf("initial coverage %d, want 5", got)
		}

		// Force-push: rebase the feature branch onto main with DIFFERENT shas
		// (rewrite), dropping the old feat commits from every tip.
		feat2 := seedChain(t, client, mainTip, "feat2-", 2)
		refs["refs/heads/feature/x"] = feat2[1]
		mustUpdateCode(t, client, refs, "main")

		got := codeCorpusShas(t, client)
		set := map[string]bool{}
		for _, s := range got {
			set[s] = true
		}
		// The rewritten feature shas must now be indexed; the old ones gone.
		for _, s := range feat2 {
			if !set[s] {
				t.Fatalf("rewritten feature commit %s missing after force-push", s[:8])
			}
		}
		for _, s := range feat {
			if set[s] {
				t.Fatalf("dropped feature commit %s still indexed after force-push", s[:8])
			}
		}
		// base + main tip already counted in base; total distinct = 3 + 2 = 5.
		if len(got) != 5 {
			t.Fatalf("post-force-push coverage %d, want 5", len(got))
		}
		m, _ := readItemsManifest(client, "", siteCodeExt)
		if m == nil || !m.Complete {
			t.Fatalf("post-force-push manifest not complete")
		}
	})
}

// TestCode_ForcePushKeepsCommitReachableFromOtherBranch: a commit dropped from one
// branch but still reachable from another stays in the corpus (the multi-branch
// membership subtlety) — with default attribution winning once main reaches it.
func TestCode_ForcePushKeepsCommitReachableFromOtherBranch(t *testing.T) {
	withTestShardCount(func() {
		client, _ := testClient(t)
		base := seedChain(t, client, "", "", 2)
		shared := seedChain(t, client, base[1], "shared-", 2) // built on base
		// feature points at the shared tip; main points at base only, for now.
		refs := map[string]string{
			"refs/heads/main":      base[1],
			"refs/heads/feature/x": shared[1],
		}
		mustUpdateCode(t, client, refs, "main")
		entries := codeCorpusEntries(t, client)
		if branchOf(entries, shared[1]) != "feature/x" {
			t.Fatalf("shared commit should be feature-attributed initially, got %q", branchOf(entries, shared[1]))
		}

		// main advances to include the shared commits (merge-forward), then the
		// feature branch is deleted. The shared commits remain reachable from main
		// and must now be attributed to main (default wins).
		refs["refs/heads/main"] = shared[1]
		delete(refs, "refs/heads/feature/x")
		mustUpdateCode(t, client, refs, "main")

		got := codeCorpusShas(t, client)
		set := map[string]bool{}
		for _, s := range got {
			set[s] = true
		}
		for _, s := range shared {
			if !set[s] {
				t.Fatalf("shared commit %s dropped though reachable from main", s[:8])
			}
		}
		entries = codeCorpusEntries(t, client)
		for _, s := range shared {
			if b := branchOf(entries, s); b != "main" {
				t.Fatalf("shared commit %s attributed %q after main reached it, want main", s[:8], b)
			}
		}
	})
}

// TestCode_DeleteWhenNoBranches: dropping every code branch deletes the corpus.
func TestCode_DeleteWhenNoBranches(t *testing.T) {
	withTestShardCount(func() {
		client, _ := testClient(t)
		shas := seedChain(t, client, "", "", 4)
		refs := map[string]string{"refs/heads/main": shas[3]}
		mustUpdateCode(t, client, refs, "main")
		if m, _ := readItemsManifest(client, "", siteCodeExt); m == nil {
			t.Fatalf("expected a code manifest after bootstrap")
		}
		mustUpdateCode(t, client, map[string]string{}, "main")
		if m, _ := readItemsManifest(client, "", siteCodeExt); m != nil {
			t.Fatalf("code manifest survived dropping every code branch")
		}
	})
}

// TestCode_NoDefaultBranchAttributesToReacher: with no default branch present,
// every commit attributes to the (sole) branch that reached it — the code walk
// must not require a default to produce a valid index.
func TestCode_NoDefaultBranchAttributesToReacher(t *testing.T) {
	withTestShardCount(func() {
		client, _ := testClient(t)
		shas := seedChain(t, client, "", "topic-", 4)
		refs := map[string]string{"refs/heads/topic": shas[3]}
		// Default branch "master" is NOT among the refs, so mustUpdateCode clears it.
		mustUpdateCode(t, client, refs, "master")
		entries := codeCorpusEntries(t, client)
		if len(codeCorpusShas(t, client)) != 4 {
			t.Fatalf("coverage %d, want 4", len(codeCorpusShas(t, client)))
		}
		for _, s := range shas {
			if b := branchOf(entries, s); b != "topic" {
				t.Fatalf("commit %s attributed %q, want topic", s[:8], b)
			}
		}
	})
}

// TestCode_GitmsgBranchesExcluded: gitmsg/* data branches never enter the code
// corpus even when passed among the refs.
func TestCode_GitmsgBranchesExcluded(t *testing.T) {
	withTestShardCount(func() {
		client, _ := testClient(t)
		code := seedChain(t, client, "", "", 4)
		data := seedChain(t, client, "", "gitmsg-", 3)
		refs := map[string]string{
			"refs/heads/main":          code[3],
			"refs/heads/gitmsg/social": data[2],
		}
		mustUpdateCode(t, client, refs, "main")
		got := codeCorpusShas(t, client)
		set := map[string]bool{}
		for _, s := range got {
			set[s] = true
		}
		for _, s := range data {
			if set[s] {
				t.Fatalf("gitmsg data commit %s leaked into the code corpus", s[:8])
			}
		}
		if len(got) != 4 {
			t.Fatalf("code coverage %d, want 4 (data branch excluded)", len(got))
		}
	})
}

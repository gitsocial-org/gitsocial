// site_code_index.go - push-maintained static-site items index for CODE branches.
//
// The static site's timeline interleaves plain (non-gitmsg) commits from every
// pushed code branch, deduped by hash, newest-first, each attributed to a real
// branch (the default branch when reachable from it, else the first code branch
// that reached it). Without an index the reader walks loose commit objects — one
// serial bucket GET per commit — so a first visit to a large branch costs
// hundreds of GETs. This file extends the per-extension items index machinery
// (site_items.go / site_shards.go) to a single "code" corpus so the reader loads
// the newest shard + head instead of walking.
//
// Differences from a gitmsg-extension index (why this is its own state machine
// rather than a call into updateSiteItemsIndex):
//
//   - ONE corpus, not per-branch: the corpus is "every commit reachable from any
//     current code tip", merged and deduped, so the timeline's cross-branch feed
//     is a single newest-first index. The synthetic tip (codeCorpusTip) is a
//     digest over the sorted code-branch tips, so any tip move/add/remove changes
//     it and drives the same APPEND/REPAIR/BACKFILL classification.
//   - NO bodies corpus: a code commit card renders subject + author/time/hash
//     only; the full message body is needed solely on the detail view, which
//     hydrates the loose object. A bodies corpus would bloat the bucket (code
//     history is far larger than collaboration history) for content the timeline
//     never shows — so code is metadata-only. This also means the reader never
//     needs a per-code-card loose-object hydration.
//   - MULTI-TIP attributed walk: walkCodeItems seeds every code tip at once,
//     dedups by sha, and records each commit's attributed branch (mirroring the
//     reader's reachedVia: the default branch always wins, else the first branch
//     that reached the commit), stored in each entry's Branch field.
//   - PARENTS in every entry (schema v5, siteCodeItemsVersion): the repository
//     graph renders its DAG from the index instead of a per-commit loose-object
//     walk. The gitmsg-extension corpora stay at v4, byte-identical.
//
// Everything else — the sealed brotli shards, the head, the manifest/complete
// semantics, the bootstrap cursor, the budgeted multi-push walk, the local-source
// (cat-file) walk with bucket fallback, and honest progress — is reused verbatim
// from the extension machinery via the shared generic shard layer.

package objstore

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

// siteCodeExt is the corpus name the code index lives under
// (.gitsocial/site/items/code/), slotting into the same ext-name-driven key
// funcs and reader loaders the gitmsg extensions use.
const siteCodeExt = "code"

// codeBranchTips returns the pushed code branches' tips as (branch, sha) pairs,
// skipping the gitmsg/* data branches (the extension indexes own those) and any
// non-branch ref. Sorted by branch name so codeCorpusTip is deterministic. The
// default branch, when present, is returned FIRST so the attributed walk seeds it
// ahead of feature branches (default-branch attribution wins).
func codeBranchTips(refs map[string]string, defaultBranch string) []codeTip {
	tips := make([]codeTip, 0, len(refs))
	for ref, sha := range refs {
		name, ok := strings.CutPrefix(ref, "refs/heads/")
		if !ok || strings.HasPrefix(name, "gitmsg/") || len(sha) != 40 {
			continue
		}
		tips = append(tips, codeTip{branch: name, sha: sha})
	}
	sort.Slice(tips, func(i, j int) bool {
		di, dj := tips[i].branch == defaultBranch, tips[j].branch == defaultBranch
		if di != dj {
			return di // default branch first
		}
		return tips[i].branch < tips[j].branch
	})
	return tips
}

// codeDefaultTip returns the default branch's tip sha among the code tips (empty
// when the default branch is absent from the pushed refs). Used to seed the
// default-reachability pass so default-branch attribution wins.
func codeDefaultTip(tips []codeTip, defaultBranch string) string {
	for _, t := range tips {
		if t.branch == defaultBranch {
			return t.sha
		}
	}
	return ""
}

// codeTip is one code branch's tip.
type codeTip struct {
	branch string
	sha    string
}

// codeCorpusTip is the synthetic 40-hex tip identifier for the code corpus: a
// sha256 (truncated to 40 hex, matching git's sha length so the shared
// manifest/cursor readers accept it) over the sorted "branch sha" lines. Any code
// branch move, add, or delete changes it, so the classifier sees the same
// tip-changed signal a single-branch push gives the extension machinery.
func codeCorpusTip(tips []codeTip) string {
	if len(tips) == 0 {
		return ""
	}
	ordered := append([]codeTip{}, tips...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].branch != ordered[j].branch {
			return ordered[i].branch < ordered[j].branch
		}
		return ordered[i].sha < ordered[j].sha
	})
	h := sha256.New()
	for _, t := range ordered {
		fmt.Fprintf(h, "%s %s\n", t.branch, t.sha)
	}
	return hex.EncodeToString(h.Sum(nil))[:40]
}

// walkCodeItems walks parent pointers from EVERY code tip at once (dedup by sha),
// collecting AT MOST budget commits newest-first over the merged frontier, and
// attributes each collected commit to a branch: the default branch when the
// commit is reachable from the default branch tip, else the first tip-branch
// (branch-name order, default-first per codeBranchTips) whose walk reached it.
// stopAt shas are not descended into (the already-indexed set for APPEND/BACKFILL
// tails). It returns the collected commits (each carrying its attributed Branch),
// the set of stopAt shas met, and budgetHit — true when the budget was reached
// with the frontier non-empty (the corpus is larger than one push can index).
//
// Attribution is order-independent (so a commit's Branch is identical whichever
// budget segment seals it): a first pass over the local odb (bucket fallback)
// marks every sha reachable from the default tip, which then unconditionally wins;
// every other commit keeps the first branch that reached it (default-first tip
// order makes that deterministic). This is the writer form of the reader's
// gs-core.js reachedVia rule ("the default branch always wins"), so switching the
// timeline to the index is invisible. Commits carrying a `GitMsg:` header (an ext
// data branch merged into a code line) are walked for reachability but excluded
// from the corpus, exactly as the reader's resolveCodeItems filters them.
func walkCodeItems(client *Client, prefix string, tips []codeTip, defaultBranch string, stopAt map[string]bool, budget int, sp *siteProgress) ([]walkedItem, map[string]bool, bool, error) {
	defaultReach, err := codeDefaultReachable(client, prefix, tips, defaultBranch, stopAt, sp)
	if err != nil {
		return nil, nil, false, err
	}
	visited := map[string]bool{}
	met := map[string]bool{}
	via := map[string]string{} // sha -> attributed branch (first non-default reacher)
	frontier := make([]string, 0, len(tips))
	for _, t := range tips {
		if _, seen := via[t.sha]; !seen {
			via[t.sha] = t.branch
		}
		frontier = append(frontier, t.sha)
	}
	items := []walkedItem{}
	for len(frontier) > 0 {
		sha := frontier[0]
		frontier = frontier[1:]
		if visited[sha] {
			continue
		}
		visited[sha] = true
		if stopAt[sha] {
			met[sha] = true
			continue
		}
		if len(items) >= budget {
			return items, met, true, nil
		}
		c, err := getCommit(sp.commitSource(), client, prefix, sha)
		if err != nil {
			return nil, nil, false, err
		}
		branch := via[sha]
		if defaultReach[sha] {
			branch = defaultBranch
		}
		// A code commit (no GitMsg header) joins the corpus, attributed to `branch`
		// and carrying its parent shas (the graph's DAG edges). A gitmsg-carrying
		// commit is walked for reachability but not listed.
		if c.item.Header == "" {
			w := c.item
			w.Branch = branch
			w.Parents = c.parents
			items = append(items, w)
			sp.walk(len(items), 0)
		}
		for _, p := range c.parents {
			if _, ok := via[p]; !ok {
				via[p] = branch
			}
			frontier = append(append([]string{}, p), frontier...)
		}
	}
	return items, met, false, nil
}

// codeDefaultReachable returns the set of shas reachable from the default
// branch's tip, honoring the same stopAt frontier as the main walk (an already-
// indexed backfill boundary need not be re-walked to know it is default-reachable
// — a sha at or below a sealed default-attributed frontier already carries its
// branch in the index). Empty when there is no default branch among the tips. The
// walk reads commits from the same local/bucket source as the main walk.
func codeDefaultReachable(client *Client, prefix string, tips []codeTip, defaultBranch string, stopAt map[string]bool, sp *siteProgress) (map[string]bool, error) {
	tip := codeDefaultTip(tips, defaultBranch)
	if tip == "" {
		return map[string]bool{}, nil
	}
	reach := map[string]bool{}
	visited := map[string]bool{}
	frontier := []string{tip}
	for len(frontier) > 0 {
		sha := frontier[0]
		frontier = frontier[1:]
		if visited[sha] {
			continue
		}
		visited[sha] = true
		reach[sha] = true
		if stopAt[sha] {
			continue
		}
		c, err := getCommit(sp.commitSource(), client, prefix, sha)
		if err != nil {
			return nil, err
		}
		for _, p := range c.parents {
			if !visited[p] {
				frontier = append(frontier, p)
			}
		}
	}
	return reach, nil
}

// codeMetaOf projects a walked (already branch-attributed) code commit into a
// metadata-index entry, carrying the attributed branch and parent shas (v5)
// alongside the shared metadata fields metaOf produces.
func codeMetaOf(w walkedItem) siteMetaEntry {
	e := metaOf(w)
	e.Branch = w.Branch
	e.Parents = w.Parents
	return e
}

// codeMetaSlice projects a walked segment into code metadata entries.
func codeMetaSlice(items []walkedItem) []siteMetaEntry {
	meta := make([]siteMetaEntry, len(items))
	for i, w := range items {
		meta[i] = codeMetaOf(w)
	}
	return meta
}

// updateSiteCodeIndex brings the single code items corpus
// (.gitsocial/site/items/code/) to the current code-branch tips. It is the
// code-corpus counterpart of updateSiteItemsIndex, sharing the same manifest,
// cursor, shard, and complete/bootstrap semantics but over ONE metadata corpus
// (no bodies) fed by the multi-tip attributed walk. tips are the current pushed
// code-branch tips (default-first); the synthetic corpus tip is their digest.
//
// State machine (mirrors site_repair.go's, minus the bodies corpus):
//   - No code branches at all: delete the corpus (a repo that dropped every code
//     branch, or a data-only bucket).
//   - No manifest: BOOTSTRAP the newest budget segment (leaving a cursor if the
//     merged history exceeds the budget).
//   - Manifest complete and corpus tip already current: NO-OP.
//   - Bootstrap in flight (cursor pending or manifest incomplete): APPEND the new
//     commits when the corpus tip changed, else BACKFILL the next older segment.
//   - Corpus tip changed with a complete manifest: APPEND the bounded gap; a walk
//     that can't reach the sealed frontier (a code branch force-moved under the
//     index, dropping indexed commits) falls through to REPAIR, which rebuilds
//     from the reachable sealed shards plus a bounded tail — or, when the frontier
//     is unreachable from any current tip, resets to a fresh bootstrap.
func updateSiteCodeIndex(client *Client, prefix string, tips []codeTip, defaultBranch string, sp *siteProgress) error {
	if len(tips) == 0 {
		return deleteCodeArtifacts(client, prefix)
	}
	newTip := codeCorpusTip(tips)
	manifest, err := readItemsManifest(client, prefix, siteCodeExt)
	if err != nil {
		return err
	}
	cursor, err := readItemsCursor(client, prefix, siteCodeExt)
	if err != nil {
		return err
	}
	if cursor == nil && manifest != nil && !manifest.Complete {
		if cursor, err = reconstructCursor(client, prefix, siteCodeExt, manifest, newTip); err != nil {
			return err
		}
	}
	head, err := readItemsHeadEntries(client, prefix+siteItemsHeadKey(siteCodeExt))
	if err != nil {
		return err
	}
	switch classifyCodeState(manifest, cursor, len(head), newTip) {
	case actionNoOp:
		return nil
	case actionBackfill:
		return backfillCode(client, prefix, cursor, manifest, head, sp)
	case actionRepair:
		return repairCodeState(client, prefix, tips, defaultBranch, newTip, manifest, cursor, sp)
	default: // actionBootstrap
		return bootstrapCode(client, prefix, tips, defaultBranch, newTip, sp)
	}
}

// classifyCodeState decides the code corpus's action from its manifest, the
// bootstrap cursor, and the live head count. It differs from classifyItemsState
// in ONE way: a changed corpus tip is always a REPAIR (tail rebuild), never a
// cheap gap-APPEND. The code corpus's membership is "reachable from ANY current
// tip", so a code-branch push can SHRINK it (a force-push/rebase drops commits
// that a suffix-only append would leave stale in the head) as well as grow it —
// only the tail rebuild, which re-walks every commit above the sealed frontier
// from all current tips and rebuilds the head from scratch, gets both cases (and
// re-attribution) right. The tail is bounded by the shard size, and sealed shards
// below the frontier are immutable and skip-existing, so this stays cheap.
//
//   - No manifest: BOOTSTRAP.
//   - Bootstrap in flight (cursor pending or manifest incomplete), heads match,
//     corpus tip already current: BACKFILL the next older segment.
//   - Corpus tip current and complete with matching heads: NO-OP.
//   - Anything else (tip changed, or a torn write with a head-count mismatch):
//     REPAIR. REPAIR keeps a pending cursor incomplete so an in-flight bootstrap's
//     older backfill still resumes on later pushes.
func classifyCodeState(manifest *siteShardManifest, cursor *siteItemsCursor, headCount int, newTip string) itemsAction {
	if manifest == nil {
		return actionBootstrap
	}
	headsMatch := headCount == manifest.Head.Count
	inFlight := (cursor != nil && !cursor.Complete) || !manifest.Complete
	if inFlight && headsMatch && manifest.Tip == newTip {
		return actionBackfill
	}
	if !inFlight && headsMatch && manifest.Tip == newTip {
		return actionNoOp
	}
	return actionRepair
}

// bootstrapCode seals the first budget segment of a fresh/wiped code corpus
// (newest-first across all tips), leaving a cursor when the merged history
// exceeds one push's budget.
func bootstrapCode(client *Client, prefix string, tips []codeTip, defaultBranch, newTip string, sp *siteProgress) error {
	walked, _, budgetHit, err := walkCodeItems(client, prefix, tips, defaultBranch, nil, siteItemsWalkBudget, sp)
	if err != nil {
		return err
	}
	plan, err := planItems(client, prefix, siteCodeExt, codeMetaSlice(walked), sp)
	if err != nil {
		return err
	}
	if err := putItemsHead(client, prefix, siteCodeExt, newTip, &plan); err != nil {
		return err
	}
	var pending *siteItemsCursor
	if budgetHit {
		pending = &siteItemsCursor{Tip: newTip, OldestIndexed: walked[len(walked)-1].SHA}
	}
	if err := putItemsManifest(client, prefix, siteCodeExt, newTip, plan, 0, pending == nil); err != nil {
		return err
	}
	return finalizeCursor(client, prefix, siteCodeExt, pending)
}

// codeIndexedShas returns the set of already-indexed boundary shas a backfill
// walk must stop at: the manifest tip, every sealed shard's endTip, and every
// head sha. Seeding all of them (not just the frontier) makes the walk halt at any
// indexed boundary even across a rare multi-branch merge (mirrors backfillItems'
// stopAt).
func codeIndexedShas(manifest *siteShardManifest, head []siteMetaEntry) map[string]bool {
	known := map[string]bool{}
	if manifest != nil {
		if len(manifest.Tip) == 40 {
			known[manifest.Tip] = true
		}
		for _, s := range manifest.Shards {
			known[s.EndTip] = true
		}
	}
	for _, e := range head {
		known[e.SHA] = true
	}
	return known
}

// backfillCode seals the next older budget segment of an in-progress code
// bootstrap: it walks older history from the manifest's oldest sealed frontier's
// parents toward the roots (stopAt = the already-indexed set), prepends the sealed
// segment to the manifest, and advances (or clears) the cursor. The head is never
// rewritten (the newest end is owned by BOOTSTRAP/REPAIR). No current tips are
// needed: the walk descends only from the sealed frontier's parents, attributing
// each older commit to the frontier's own branch. This is backfillItems
// specialized to the code corpus.
func backfillCode(client *Client, prefix string, cursor *siteItemsCursor, manifest *siteShardManifest, head []siteMetaEntry, sp *siteProgress) error {
	frontier, err := manifestOldestSha(client, prefix, siteCodeExt, manifest, head)
	if err != nil {
		return err
	}
	if frontier == "" {
		return completeCodeBackfill(client, prefix)
	}
	oldest, err := getCommit(sp.commitSource(), client, prefix, frontier)
	if err != nil {
		return err
	}
	if len(oldest.parents) == 0 {
		return completeCodeBackfill(client, prefix)
	}
	stop := codeIndexedShas(manifest, head)
	stop[frontier] = true
	// The older segment is walked from the frontier's parents as fresh pseudo-tips,
	// each inheriting the frontier commit's own attributed branch. An ancestor of a
	// default-attributed frontier is itself default-reachable, so this inheritance
	// reproduces the newest segments' attribution without re-walking the default
	// branch's whole (already-sealed) history; the reader reads Branch straight from
	// the shard, so the older commits link under the same branch route.
	frontierBranch := codeFrontierBranch(client, prefix, manifest, head, frontier)
	parentTips := make([]codeTip, 0, len(oldest.parents))
	for _, p := range oldest.parents {
		parentTips = append(parentTips, codeTip{branch: frontierBranch, sha: p})
	}
	segment, budgetHit, err := walkCodeSegment(client, prefix, parentTips, frontierBranch, stop, sp)
	if err != nil {
		return err
	}
	if len(segment) == 0 {
		return completeCodeBackfill(client, prefix)
	}
	var pending *siteItemsCursor
	if budgetHit {
		pending = &siteItemsCursor{Tip: cursor.Tip, OldestIndexed: segment[len(segment)-1].SHA}
	}
	if err := prependCodeSegment(client, prefix, segment, pending == nil, sp); err != nil {
		return err
	}
	return finalizeCursor(client, prefix, siteCodeExt, pending)
}

// walkCodeSegment walks an older backfill segment from the parent pseudo-tips
// toward the roots, honoring the walk budget across all parents (like
// backfillItems' per-parent loop). Attribution inherits frontierBranch (passed as
// the default so codeDefaultReachable stays inert here — the sealed frontier is in
// `stop`, so the default branch's history is never re-walked); an ancestor of a
// default-attributed frontier thus keeps the default branch, matching the newest
// segments' attribution.
func walkCodeSegment(client *Client, prefix string, parentTips []codeTip, frontierBranch string, stop map[string]bool, sp *siteProgress) ([]walkedItem, bool, error) {
	segment := []walkedItem{}
	budgetHit := false
	for _, pt := range parentTips {
		seg, _, hit, err := walkCodeItems(client, prefix, []codeTip{pt}, frontierBranch, stop, siteItemsWalkBudget-len(segment), sp)
		if err != nil {
			return nil, false, err
		}
		for _, w := range seg {
			stop[w.SHA] = true
		}
		segment = append(segment, seg...)
		if hit {
			budgetHit = true
			break
		}
	}
	return segment, budgetHit, nil
}

// codeFrontierBranch returns the attributed branch of the oldest indexed commit
// (the backfill frontier), read from the manifest's oldest sealed shard or the
// head. Backfilled parents inherit it as their first-reacher attribution (default
// reachability still overrides). Empty when unknown.
func codeFrontierBranch(client *Client, prefix string, manifest *siteShardManifest, head []siteMetaEntry, frontier string) string {
	if manifest != nil && len(manifest.Shards) > 0 {
		entries, err := readItemsHeadEntries(client, prefix+siteItemsDir(siteCodeExt)+manifest.Shards[0].Key)
		if err == nil {
			for _, e := range entries {
				if e.SHA == frontier {
					return e.Branch
				}
			}
		}
	}
	for _, e := range head {
		if e.SHA == frontier {
			return e.Branch
		}
	}
	return ""
}

// prependCodeSegment seals one backfilled older code segment and prepends it to
// the manifest (re-reading it immediately before the write so a concurrent APPEND
// head/tip survives). The head is untouched. complete is true only when this
// segment reached the roots.
func prependCodeSegment(client *Client, prefix string, segment []walkedItem, complete bool, sp *siteProgress) error {
	head, err := readItemsHeadEntries(client, prefix+siteItemsHeadKey(siteCodeExt))
	if err != nil {
		return err
	}
	plan, tip, err := prependSegmentPlan(client, itemsCorpus, prefix, siteCodeExt, codeMetaSlice(segment), head, sp)
	if err != nil {
		return err
	}
	_, err = putManifest(client, itemsCorpus, prefix, siteCodeExt, tip, plan, 0, complete)
	return err
}

// completeCodeBackfill marks the code manifest complete and clears the cursor
// when a backfill finds no older history remains.
func completeCodeBackfill(client *Client, prefix string) error {
	if err := markManifestComplete(client, itemsCorpus, prefix, siteCodeExt); err != nil {
		return err
	}
	return deleteItemsCursor(client, prefix, siteCodeExt)
}

// repairCodeState rebuilds the code corpus after the corpus tip changed (or a
// torn write). Unlike the ext REPAIR it does NOT keep sealed shards by
// endTip-reachability: the code corpus's membership can SHRINK inside a sealed
// shard (a force-push/rebase drops a commit whose shard's newest member — the
// endTip, often a shared base commit — is still reachable), so a kept-by-frontier
// shard would strand the dropped commit. Instead:
//
//   - Complete corpus (no bootstrap in flight): a FULL re-walk over all current
//     tips + a full reseal (planItems). Content-hash keying makes every unchanged
//     shard skip-existing (no recompress, no re-PUT), so only shards whose
//     membership actually changed get new keys, and stale shards simply drop out
//     of the rebuilt manifest — the "discard affected sealed shards" repair the
//     multi-branch force-push needs, at the cost of one budgeted walk.
//   - Bootstrap in flight (cursor pending): the corpus is larger than one push, so
//     a full re-walk can't reach the root. Rebuild only the tail above the sealed
//     frontier when it is still reachable (commits added/reattributed on top),
//     else reset; the older segments self-heal as the bootstrap's later backfills
//     re-seal them. A force-push mid-bootstrap is rare and converges once complete.
func repairCodeState(client *Client, prefix string, tips []codeTip, defaultBranch, newTip string, manifest *siteShardManifest, cursor *siteItemsCursor, sp *siteProgress) error {
	if cursor == nil {
		return rebuildCodeFull(client, prefix, tips, defaultBranch, newTip, sp)
	}
	return repairCodeTail(client, prefix, tips, defaultBranch, newTip, manifest, cursor, sp)
}

// rebuildCodeFull re-walks the whole merged corpus over all tips and reseals it
// (full rebuild; skip-existing keeps unchanged shards free), marking it complete
// only when the walk reached every root within the budget. A budget hit leaves a
// cursor so the remainder backfills — the same servable-newest-prefix guarantee
// bootstrap gives.
func rebuildCodeFull(client *Client, prefix string, tips []codeTip, defaultBranch, newTip string, sp *siteProgress) error {
	walked, _, budgetHit, err := walkCodeItems(client, prefix, tips, defaultBranch, nil, siteItemsWalkBudget, sp)
	if err != nil {
		return err
	}
	plan, err := planItems(client, prefix, siteCodeExt, codeMetaSlice(walked), sp)
	if err != nil {
		return err
	}
	if err := putItemsHead(client, prefix, siteCodeExt, newTip, &plan); err != nil {
		return err
	}
	var pending *siteItemsCursor
	if budgetHit && len(walked) > 0 {
		pending = &siteItemsCursor{Tip: newTip, OldestIndexed: walked[len(walked)-1].SHA}
	}
	if err := putItemsManifest(client, prefix, siteCodeExt, newTip, plan, 0, pending == nil); err != nil {
		return err
	}
	return finalizeCursor(client, prefix, siteCodeExt, pending)
}

// repairCodeTail rebuilds only the code corpus's head/tail above its sealed
// frontier (the in-flight-bootstrap REPAIR): keep the sealed shards when the
// frontier is still reachable, else reset. The pending cursor is kept so the
// bootstrap's older backfills still run.
func repairCodeTail(client *Client, prefix string, tips []codeTip, defaultBranch, newTip string, manifest *siteShardManifest, cursor *siteItemsCursor, sp *siteProgress) error {
	var tail []walkedItem
	var kept []siteShardEntry
	frontier, has := manifest.sealedFrontier()
	if !has {
		walked, _, _, err := walkCodeItems(client, prefix, tips, defaultBranch, nil, siteItemsWalkBudget, sp)
		if err != nil {
			return err
		}
		tail = walked
	} else {
		walked, met, _, err := walkCodeItems(client, prefix, tips, defaultBranch, map[string]bool{frontier: true}, siteItemsWalkBudget, sp)
		if err != nil {
			return err
		}
		tail = walked
		if met[frontier] {
			kept = manifest.Shards
		}
	}
	plan, err := planItemsTail(client, prefix, siteCodeExt, kept, codeMetaSlice(tail), sp)
	if err != nil {
		return err
	}
	if err := putItemsHead(client, prefix, siteCodeExt, newTip, &plan); err != nil {
		return err
	}
	if err := putItemsManifest(client, prefix, siteCodeExt, newTip, plan, 0, false); err != nil {
		return err
	}
	pending := &siteItemsCursor{Tip: newTip, OldestIndexed: cursor.OldestIndexed}
	if len(plan.shards) == 0 {
		pending = nil
	}
	return finalizeCursor(client, prefix, siteCodeExt, pending)
}

// deleteCodeArtifacts removes the whole code items corpus (every sealed shard from
// the manifest, then head, manifest, and cursor) — a repo that dropped every code
// branch. There is no bodies corpus to delete.
func deleteCodeArtifacts(client *Client, prefix string) error {
	manifest, err := readItemsManifest(client, prefix, siteCodeExt)
	if err != nil {
		return err
	}
	if manifest == nil {
		// Nothing (or only a stray head/cursor) — clean those best-effort.
		_ = client.Delete(prefix + siteItemsHeadKey(siteCodeExt))
		_ = client.Delete(prefix + siteItemsManifestKey(siteCodeExt))
		return deleteItemsCursor(client, prefix, siteCodeExt)
	}
	for _, s := range manifest.Shards {
		if err := client.Delete(prefix + siteItemsDir(siteCodeExt) + s.Key); err != nil {
			return err
		}
	}
	if err := client.Delete(prefix + siteItemsHeadKey(siteCodeExt)); err != nil {
		return err
	}
	if err := client.Delete(prefix + siteItemsManifestKey(siteCodeExt)); err != nil {
		return err
	}
	return deleteItemsCursor(client, prefix, siteCodeExt)
}

// codeIndexBootstrapPending reports whether the code corpus is still an incomplete
// bootstrap (its manifest is incomplete), so the caller declines to stamp the
// push-state marker until every older segment is backfilled. Best-effort: a read
// error reports pending, at worst leaving the marker unstamped for one more pass.
func codeIndexBootstrapPending(client *Client, prefix string) bool {
	manifest, err := readItemsManifest(client, prefix, siteCodeExt)
	if err != nil {
		return true
	}
	return manifest != nil && !manifest.Complete
}

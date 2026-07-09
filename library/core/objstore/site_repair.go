// site_repair.go - the static-site item-artifact repair state machine.
//
// updateSiteItemsIndex reads four bucket-derived inputs (both corpora's
// manifests + both live head counts) and classifies the state with the pure
// classifyItemsState below, then dispatches. The design invariant: once an items
// manifest exists on the bucket there is NO path back to a from-scratch capped
// walk. Every observable mismatch (a partial or interrupted or raced write) is
// repaired by rebuilding each corpus from its own reachable immutable sealed
// shards plus a BOUNDED tail re-walk (the commits newer than the newest sealed
// shard). The single exception is a manifest tip that is genuinely unreachable
// from newTip (a history rewrite / force-push under the artifacts): that corpus's
// old shards are stale, so it resets to a fresh bootstrap walk. A branch past
// the walk budget can no longer wedge the incremental path, because the
// incremental path never re-walks sealed history.

package objstore

// itemsAction is the repair state machine's decision for one extension push.
type itemsAction int

const (
	// actionNoOp: both corpora already at newTip with matching head counts.
	actionNoOp itemsAction = iota
	// actionAppend: both corpora present, lockstepped at a common tip, head
	// counts match; advance by the bounded gap from newTip down to that tip. When
	// a cursor is pending, APPEND owns the newest end (it bumps cursor.tip and
	// leaves oldestIndexed for BACKFILL) so new commits and backfill never
	// overlap.
	actionAppend
	// actionRepair: an items manifest is present but the state is mismatched
	// (tips skewed, a head count off, or a partial write); rebuild each corpus
	// from its reachable sealed shards plus a bounded tail re-walk.
	actionRepair
	// actionBootstrap: no items manifest (fresh / wiped); seal the first budget
	// segment (newest-first) and, if the branch exceeds the budget, leave a cursor.
	actionBootstrap
	// actionBackfill: a cursor is pending and the newest end is already at newTip;
	// seal the next older budget segment from oldestIndexed toward the root and
	// prepend it to both manifests, advancing (or clearing) the cursor.
	actionBackfill
)

// classifyItemsState decides the repair action purely from the read inputs (no
// I/O), so it is directly table-testable. newTip is the pushed ref sha; cursor
// is the in-progress bootstrap cursor (nil when none is pending).
//
// A bootstrap is "in flight" when EITHER a cursor is pending OR the items
// manifest is marked incomplete — the two are treated identically. This is the
// authoritative fix for the torn state where a BOOTSTRAP wrote both manifests
// (complete:false) but its cursor PUT was lost: the missing cursor no longer
// reads as a finished small branch (NO-OP freeze). BACKFILL reconstructs the
// cursor from the manifest's oldest sealed shard when it is absent.
//
//   - No items manifest: BOOTSTRAP (fresh/wiped, or the items manifest was lost
//     while bodies survive; the bootstrap walk reseals both idempotently).
//   - Bootstrap in flight (cursor pending, or the items manifest is incomplete)
//     AND both corpora lockstepped with matching head counts: BACKFILL the next
//     older segment when the newest end is already at newTip; else APPEND the new
//     commits first (APPEND owns the newest end and bumps cursor.tip, BACKFILL
//     resumes from the unchanged oldestIndexed on a later push). A torn corpus
//     falls through to REPAIR, which heals both ends before the next backfill.
//   - Not in flight, both manifests present, lockstepped (items.Tip ==
//     bodies.Tip), and each head count matches its manifest: NO-OP when both tips
//     already equal newTip, else APPEND the bounded gap.
//   - Anything else with an items manifest present: REPAIR (bodies ahead of
//     items, items ahead of bodies, a head count mismatch on either corpus, a
//     missing bodies manifest, or a tip that will prove unreachable). REPAIR's
//     per-corpus tail rebuild handles every one; reachability is resolved there.
func classifyItemsState(items, bodies *siteShardManifest, cursor *siteItemsCursor, itemsHeadCount, bodiesHeadCount int, newTip string) itemsAction {
	if items == nil {
		return actionBootstrap
	}
	if bodies == nil {
		return actionRepair
	}
	headsMatch := itemsHeadCount == items.Head.Count && bodiesHeadCount == bodies.Head.Count
	lockstepped := items.Tip == bodies.Tip && headsMatch
	inFlight := (cursor != nil && !cursor.Complete) || !items.Complete
	if inFlight {
		if !lockstepped {
			return actionRepair
		}
		if items.Tip == newTip {
			return actionBackfill
		}
		return actionAppend
	}
	if lockstepped {
		if items.Tip == newTip {
			return actionNoOp
		}
		return actionAppend
	}
	return actionRepair
}

// repairItemsState brings both corpora to newTip without a from-scratch capped
// walk over reachable history. Each corpus is rebuilt independently from its own
// reachable sealed shards plus a bounded tail re-walk (walkCorpusTail); the two
// plans are then written in the pinned order (bodies shards + items shards
// already sealed by planning; then bodies head, items head, bodies manifest,
// items manifest) so the manifests land last and an interruption leaves at worst
// "bodies ahead of items", which a later REPAIR fixes. When a cursor is pending
// (bootstrap in flight) the rebuilt manifests stay incomplete so the next push
// still backfills the older history; REPAIR heals only the newest (head) end.
func repairItemsState(client *Client, prefix, ext, newTip string, items, bodies *siteShardManifest, cursor *siteItemsCursor) error {
	complete := cursor == nil
	bodiesTail, bodiesKept, err := walkCorpusTail(client, prefix, newTip, bodies)
	if err != nil {
		return err
	}
	itemsTail, itemsKept, err := walkCorpusTail(client, prefix, newTip, items)
	if err != nil {
		return err
	}
	bodiesEntries := make([]siteBodyEntry, len(bodiesTail))
	itemsEntries := make([]siteMetaEntry, len(itemsTail))
	for i, w := range bodiesTail {
		bodiesEntries[i] = bodyOf(w)
	}
	for i, w := range itemsTail {
		itemsEntries[i] = metaOf(w)
	}
	bodiesPlan, err := planBodiesTail(client, prefix, ext, bodiesKept, bodiesEntries)
	if err != nil {
		return err
	}
	itemsPlan, err := planItemsTail(client, prefix, ext, itemsKept, itemsEntries)
	if err != nil {
		return err
	}
	if err := putBodiesHead(client, prefix, ext, newTip, &bodiesPlan); err != nil {
		return err
	}
	if err := putItemsHead(client, prefix, ext, newTip, &itemsPlan); err != nil {
		return err
	}
	total, err := putBodiesManifest(client, prefix, ext, newTip, bodiesPlan, complete)
	if err != nil {
		return err
	}
	return putItemsManifest(client, prefix, ext, newTip, itemsPlan, total, complete)
}

// walkCorpusTail walks the commits newer than one corpus's sealed frontier (its
// newest sealed shard's endTip) and returns that tail (newest-first) plus the
// sealed shards REPAIR keeps. Three cases:
//   - No sealed shards: the whole branch from newTip is the tail (bootstrap /
//     items-manifest-absent), keeping nothing, bounded by the walk budget.
//   - Frontier reachable: the tail is exactly the commits above the immutable
//     sealed prefix; keep every sealed shard (their boundaries are stable, so the
//     rebuilt head/shards are byte-identical and skip-existing).
//   - Frontier unreachable (history rewrite under the artifacts): the old sealed
//     shards are stale, so reset by walking the whole branch from newTip
//     (budgeted) and keeping nothing.
func walkCorpusTail(client *Client, prefix, newTip string, manifest *siteShardManifest) ([]walkedItem, []siteShardEntry, error) {
	frontier, has := manifest.sealedFrontier()
	if !has {
		walked, _, _, err := walkBucketItems(client, prefix, newTip, nil, siteItemsWalkBudget)
		return walked, nil, err
	}
	tail, met, _, err := walkBucketItems(client, prefix, newTip, map[string]bool{frontier: true}, siteItemsWalkBudget)
	if err != nil {
		return nil, nil, err
	}
	if met[frontier] {
		return tail, manifest.Shards, nil
	}
	// Frontier never encountered: the walk already covered the whole branch (a
	// stopAt that never matches walks exactly as far as no stopAt), so the tail
	// IS the reset walk. Keep no shards.
	return tail, nil, nil
}

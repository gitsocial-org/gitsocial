// site_repair.go - the item-artifact repair state machine

package site

import "github.com/gitsocial-org/gitsocial/library/core/objstore"

// itemsAction is the repair state machine's decision for one extension push.
type itemsAction int

const (
	// actionNoOp: both corpora already at newTip with matching head counts.
	actionNoOp itemsAction = iota
	// actionAppend: both corpora lockstepped below newTip; advance by the bounded gap.
	actionAppend
	// actionRepair: an items manifest is present and the state mismatches.
	actionRepair
	// actionBootstrap: no items manifest; seal the first budget segment.
	actionBootstrap
	// actionBackfill: a cursor is pending and the newest end is already at newTip.
	actionBackfill
)

// classifyItemsState decides the repair action from the read inputs alone, with no I/O, so it is table-testable.
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

// repairItemsState brings both corpora to newTip by rebuilding each from its own sealed shards plus a bounded tail re-walk.
func repairItemsState(client *objstore.Client, prefix, ext, newTip string, items, bodies *siteShardManifest, cursor *siteItemsCursor, sp *siteProgress) error {
	complete := cursor == nil
	bodiesTail, bodiesKept, err := walkCorpusTail(client, prefix, newTip, bodies, sp)
	if err != nil {
		return err
	}
	itemsTail, itemsKept, err := walkCorpusTail(client, prefix, newTip, items, sp)
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
	bodiesPlan, err := planBodiesTail(client, prefix, ext, bodiesKept, bodiesEntries, sp)
	if err != nil {
		return err
	}
	itemsPlan, err := planItemsTail(client, prefix, ext, itemsKept, itemsEntries, sp)
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

// walkCorpusTail walks the commits newer than one corpus's sealed frontier and returns that tail plus the sealed shards repair keeps.
func walkCorpusTail(client *objstore.Client, prefix, newTip string, manifest *siteShardManifest, sp *siteProgress) ([]walkedItem, []siteShardEntry, error) {
	frontier, has := manifest.sealedFrontier()
	if !has {
		walked, _, _, err := walkBucketItems(client, prefix, newTip, nil, siteItemsWalkBudget, sp)
		return walked, nil, err
	}
	tail, met, _, err := walkBucketItems(client, prefix, newTip, map[string]bool{frontier: true}, siteItemsWalkBudget, sp)
	if err != nil {
		return nil, nil, err
	}
	if met[frontier] {
		return tail, manifest.Shards, nil
	}
	// An unmet frontier means the walk already covered the whole branch, so the tail is the reset walk.
	return tail, nil, nil
}

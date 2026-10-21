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
	// An unmet frontier means the walk already covered the branch, so the tail is the reset walk.
	return tail, nil, nil
}

// readStaleItemsManifest fetches one extension's items manifest when it is at an older schema version; nil when absent, current or unreadable.
func readStaleItemsManifest(client *objstore.Client, prefix, ext string) (*siteShardManifest, error) {
	var m siteShardManifest
	found, err := objstore.ReadCompressedJSON(client, prefix+siteItemsManifestKey(ext), &m)
	if err != nil || !found || m.Version == itemsDocVersion(ext) || len(m.Tip) != 40 {
		return nil, err
	}
	return &m, nil
}

// upgradeItemsCorpus rewrites a complete items corpus at an older schema version from its own entries and the bodies corpus, shard by shard, and returns the new manifest; nil when the two corpora do not cover each other, which leaves the bootstrap to rebuild both (TestRepair_itemsBumpKeepsBodies).
func upgradeItemsCorpus(client *objstore.Client, prefix, ext string, bodies *siteShardManifest, sp *siteProgress) (*siteShardManifest, error) {
	stale, err := readStaleItemsManifest(client, prefix, ext)
	if err != nil || stale == nil || !stale.Complete || !bodies.Complete || stale.Tip != bodies.Tip {
		return nil, err
	}
	entries, err := readAllShardEntries(client, prefix, ext, bodiesCorpus, bodies)
	if err != nil {
		return nil, err
	}
	messages := make(map[string]string, len(entries))
	for _, e := range entries {
		messages[e.SHA] = e.Message
	}
	project := func(old []siteMetaEntry) ([]siteMetaEntry, bool) {
		out := make([]siteMetaEntry, 0, len(old))
		for _, e := range old {
			message, ok := messages[e.SHA]
			if !ok {
				return nil, false
			}
			out = append(out, metaOf(walkedItem{SHA: e.SHA, Author: e.Author, Email: e.Email, TS: e.TS, Header: e.Header, Message: message}))
		}
		return out, true
	}
	var plan shardPlan[siteMetaEntry]
	for i, s := range stale.Shards {
		old, err := readItemsHeadEntries(client, prefix+siteItemsDir(ext)+s.Key)
		if err != nil {
			return nil, err
		}
		group, ok := project(old)
		if !ok || len(group) == 0 {
			return nil, nil
		}
		shard, size, err := sealShardGeneric(client, itemsCorpus, prefix, ext, group, nil)
		if err != nil {
			return nil, err
		}
		plan.shards = append(plan.shards, shard)
		plan.sealedBytes += size
		sp.shards(itemsCorpus.label, i+1, len(stale.Shards))
	}
	head, err := readItemsHeadEntries(client, prefix+siteItemsHeadKey(ext))
	if err != nil {
		return nil, err
	}
	if plan.head, _ = project(head); plan.head == nil && len(head) > 0 {
		return nil, nil
	}
	if err := putItemsHead(client, prefix, ext, stale.Tip, &plan); err != nil {
		return nil, err
	}
	if err := putItemsManifest(client, prefix, ext, stale.Tip, plan, bodies.TotalBytes, true); err != nil {
		return nil, err
	}
	return readItemsManifest(client, prefix, ext)
}

// site_code_index.go - the single metadata index across every code branch

package site

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/gitsocial-org/gitsocial/library/core/objstore"
)

// siteCodeExt is the corpus name the code index lives under, feeding the same key funcs the extensions use.
const siteCodeExt = "code"

// codeBranchTips returns the pushed code branches' tips, sorted by name with the default branch first, so the attributed walk seeds it ahead of the rest.
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

// codeDefaultTip returns the default branch's tip sha among the code tips, which seeds the default-reachability pass.
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

// codeCorpusTip is the corpus's synthetic tip: a digest over the sorted branch tips, truncated to git's sha length so the shared readers accept it.
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

// walkCodeItems walks parents from every code tip at once, deduped, and attributes each commit to a branch. Attribution is order-independent, so a commit's branch is the same whichever budget segment seals it.
func walkCodeItems(client *objstore.Client, prefix string, tips []codeTip, defaultBranch string, stopAt map[string]bool, budget int, sp *siteProgress) ([]walkedItem, map[string]bool, bool, error) {
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
		// A gitmsg-carrying commit is walked for reachability but left out of the corpus.
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

// codeDefaultReachable returns the shas reachable from the default branch's tip, honoring the same stopAt frontier as the main walk.
func codeDefaultReachable(client *objstore.Client, prefix string, tips []codeTip, defaultBranch string, stopAt map[string]bool, sp *siteProgress) (map[string]bool, error) {
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

// codeMetaOf projects a walked code commit into a metadata-index entry, carrying its attributed branch and parent shas.
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

// updateSiteCodeIndex brings the single code corpus to the current code-branch tips, over the same manifest, cursor and shard machinery minus the bodies.
func updateSiteCodeIndex(client *objstore.Client, prefix string, tips []codeTip, defaultBranch string, sp *siteProgress) error {
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

// classifyCodeState decides the code corpus's action; a changed tip takes the repair path, since the corpus can shrink as well as grow under a force-push.
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

// bootstrapCode seals the first budget segment of a fresh code corpus, leaving a cursor when the merged history exceeds one push's budget.
func bootstrapCode(client *objstore.Client, prefix string, tips []codeTip, defaultBranch, newTip string, sp *siteProgress) error {
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

// codeIndexedShas returns the indexed boundary shas a backfill walk stops at, so it halts at any of them and not the frontier alone.
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

// backfillCode seals the next older budget segment of an in-progress code bootstrap and prepends it to the manifest, leaving the head alone.
func backfillCode(client *objstore.Client, prefix string, cursor *siteItemsCursor, manifest *siteShardManifest, head []siteMetaEntry, sp *siteProgress) error {
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
	// Parents are walked as fresh tips inheriting the frontier's branch, which reproduces the newest segments' attribution without re-walking sealed history.
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

// walkCodeSegment walks an older backfill segment from the parent tips toward the roots, sharing one budget; attribution inherits frontierBranch.
func walkCodeSegment(client *objstore.Client, prefix string, parentTips []codeTip, frontierBranch string, stop map[string]bool, sp *siteProgress) ([]walkedItem, bool, error) {
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

// codeFrontierBranch returns the backfill frontier commit's attributed branch, read from the oldest sealed shard or the head.
func codeFrontierBranch(client *objstore.Client, prefix string, manifest *siteShardManifest, head []siteMetaEntry, frontier string) string {
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

// prependCodeSegment seals one backfilled older code segment and prepends it to the manifest, leaving the head untouched.
func prependCodeSegment(client *objstore.Client, prefix string, segment []walkedItem, complete bool, sp *siteProgress) error {
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

// completeCodeBackfill marks the code manifest complete and clears the cursor when no older history remains.
func completeCodeBackfill(client *objstore.Client, prefix string) error {
	if err := markManifestComplete(client, itemsCorpus, prefix, siteCodeExt); err != nil {
		return err
	}
	return deleteItemsCursor(client, prefix, siteCodeExt)
}

// repairCodeState rebuilds the code corpus: a full re-walk and reseal when it is complete, the tail alone while a bootstrap is in flight. It keeps no shard by frontier reachability, since membership can shrink inside a sealed one.
func repairCodeState(client *objstore.Client, prefix string, tips []codeTip, defaultBranch, newTip string, manifest *siteShardManifest, cursor *siteItemsCursor, sp *siteProgress) error {
	if cursor == nil {
		return rebuildCodeFull(client, prefix, tips, defaultBranch, newTip, sp)
	}
	return repairCodeTail(client, prefix, tips, defaultBranch, newTip, manifest, cursor, sp)
}

// rebuildCodeFull re-walks the merged corpus over all tips and reseals it, leaving a cursor when the budget stopped it short of the roots.
func rebuildCodeFull(client *objstore.Client, prefix string, tips []codeTip, defaultBranch, newTip string, sp *siteProgress) error {
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

// repairCodeTail rebuilds the code corpus above its sealed frontier, keeping the sealed shards when the frontier is still reachable.
func repairCodeTail(client *objstore.Client, prefix string, tips []codeTip, defaultBranch, newTip string, manifest *siteShardManifest, cursor *siteItemsCursor, sp *siteProgress) error {
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

// deleteCodeArtifacts removes the whole code corpus, for a repo that dropped every code branch.
func deleteCodeArtifacts(client *objstore.Client, prefix string) error {
	manifest, err := readItemsManifest(client, prefix, siteCodeExt)
	if err != nil {
		return err
	}
	if manifest == nil {
		// Nothing, or only a stray head and cursor; clean those best-effort.
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

// codeIndexBootstrapPending reports whether the code corpus is still an incomplete bootstrap; a read error counts as pending.
func codeIndexBootstrapPending(client *objstore.Client, prefix string) bool {
	manifest, err := readItemsManifest(client, prefix, siteCodeExt)
	if err != nil {
		return true
	}
	return manifest != nil && !manifest.Complete
}

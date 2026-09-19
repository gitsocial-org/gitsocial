// sync.go - PM extension sync and commit processing
package pm

import (
	"github.com/gitsocial-org/gitsocial/library/core/cache"
	"github.com/gitsocial-org/gitsocial/library/core/fetch"
	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/log"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
)

// SyncWorkspaceBatch ingests pre-fetched workspace commits from the pm branch.
func SyncWorkspaceBatch(commits []git.Commit, workdir, repoURL, _ string) {
	ProcessWorkspaceBatch(commits, repoURL, gitmsg.GetExtBranch(workdir, "pm"))
}

// ProcessWorkspaceBatch processes pre-fetched commits for PM extension items.
// Used by the unified workspace sync to avoid redundant git log calls.
func ProcessWorkspaceBatch(commits []git.Commit, repoURL, branch string) {
	var pmItems []PMItem
	var links []pmLinkEntry
	for _, gc := range commits {
		if fetch.CleanRefname(gc.Refname) != branch {
			continue
		}
		msg := protocol.ParseMessage(gc.Message)
		if msg == nil || msg.Header.Ext != "pm" {
			continue
		}
		item, lnk := buildPMItem(gc, msg, repoURL, branch)
		if item != nil {
			pmItems = append(pmItems, *item)
		}
		if lnk != nil {
			links = append(links, *lnk)
		}
	}
	if err := InsertPMItems(pmItems); err != nil {
		log.Debug("batch insert pm items failed", "error", err)
	}
	syncEditFields(pmItems)
	for _, lnk := range links {
		if err := InsertLinks(repoURL, lnk.hash, branch, lnk.blocks, lnk.blockedBy, lnk.related); err != nil {
			log.Debug("insert pm links failed", "hash", lnk.hash, "error", err)
		}
	}
}

// syncEditFields propagates extension fields from edit items to their canonicals.
func syncEditFields(items []PMItem) {
	edits := make([]cache.EditKey, 0, len(items))
	for _, item := range items {
		edits = append(edits, cache.EditKey{RepoURL: item.RepoURL, Hash: item.Hash, Branch: item.Branch})
	}
	cache.SyncEditExtensionFields(edits)
}

// pmLinkEntry holds link data for batch processing.
type pmLinkEntry struct {
	hash                       string
	blocks, blockedBy, related []IssueRef
}

// buildPMItem builds a PMItem from a commit and message without inserting.
// Returns nil item if the commit is not a PM item.
func buildPMItem(gc git.Commit, msg *protocol.Message, repoURL, branch string) (*PMItem, *pmLinkEntry) {
	itemType := msg.Header.Fields["type"]
	if itemType == "" {
		return nil, nil
	}

	state := msg.Header.Fields["state"]
	if state == "" {
		state = string(StateOpen)
	}

	cache.ProcessVersionFromHeader(msg, gc.Hash, repoURL, branch)

	item := PMItem{
		RepoURL:   repoURL,
		Hash:      gc.Hash,
		Branch:    branch,
		Type:      itemType,
		State:     state,
		Assignees: cache.ToNullString(msg.Header.Fields["assignees"]),
		Due:       cache.ToNullString(msg.Header.Fields["due"]),
		StartDate: cache.ToNullString(msg.Header.Fields["start"]),
		EndDate:   cache.ToNullString(msg.Header.Fields["end"]),
		Labels:    cache.ToNullString(msg.Header.Fields["labels"]),
	}

	if milestone := msg.Header.Fields["milestone"]; milestone != "" {
		ref := protocol.ResolveRefWithDefaults(milestone, repoURL, branch)
		if ref.Hash != "" {
			item.MilestoneRepoURL = cache.ToNullString(ref.RepoURL)
			item.MilestoneHash = cache.ToNullString(ref.Hash)
			item.MilestoneBranch = cache.ToNullString(ref.Branch)
		}
	}

	if sprint := msg.Header.Fields["sprint"]; sprint != "" {
		ref := protocol.ResolveRefWithDefaults(sprint, repoURL, branch)
		if ref.Hash != "" {
			item.SprintRepoURL = cache.ToNullString(ref.RepoURL)
			item.SprintHash = cache.ToNullString(ref.Hash)
			item.SprintBranch = cache.ToNullString(ref.Branch)
		}
	}

	if parent := msg.Header.Fields["parent"]; parent != "" {
		ref := protocol.ResolveRefWithDefaults(parent, repoURL, branch)
		if ref.Hash != "" {
			item.ParentRepoURL = cache.ToNullString(ref.RepoURL)
			item.ParentHash = cache.ToNullString(ref.Hash)
			item.ParentBranch = cache.ToNullString(ref.Branch)
		}
	}

	if root := msg.Header.Fields["root"]; root != "" {
		ref := protocol.ResolveRefWithDefaults(root, repoURL, branch)
		if ref.Hash != "" {
			item.RootRepoURL = cache.ToNullString(ref.RepoURL)
			item.RootHash = cache.ToNullString(ref.Hash)
			item.RootBranch = cache.ToNullString(ref.Branch)
		}
	}

	blocks := ParseRefList(msg.Header.Fields["blocks"], repoURL, branch)
	blockedBy := ParseRefList(msg.Header.Fields["blocked-by"], repoURL, branch)
	related := ParseRefList(msg.Header.Fields["related"], repoURL, branch)

	var lnk *pmLinkEntry
	if len(blocks) > 0 || len(blockedBy) > 0 || len(related) > 0 {
		lnk = &pmLinkEntry{hash: gc.Hash, blocks: blocks, blockedBy: blockedBy, related: related}
	}

	return &item, lnk
}

// processPMCommit handles a single commit for PM extension processing.
// Matches fetch.CommitProcessor signature for use as a core fetch callback.
// Runs SyncEditExtensionFields after InsertPMItem so the canonical's
// pm_items columns (state, assignees, …) catch up — buildPMItem already
// inserted the version row via ProcessVersionFromHeader, but the
// applyEditToCanonical inside InsertVersion fires BEFORE the edit's
// pm_items row exists and skips the column propagation. Memo's
// processor follows the same pattern.
func processPMCommit(gc git.Commit, msg *protocol.Message, repoURL, branch string) {
	if msg == nil || msg.Header.Ext != "pm" {
		return
	}
	item, lnk := buildPMItem(gc, msg, repoURL, branch)
	if item == nil {
		return
	}
	if err := InsertPMItem(*item); err != nil {
		log.Debug("insert pm item failed", "hash", gc.Hash, "error", err)
		return
	}
	if lnk != nil {
		if err := InsertLinks(repoURL, lnk.hash, branch, lnk.blocks, lnk.blockedBy, lnk.related); err != nil {
			log.Debug("insert pm links failed", "hash", gc.Hash, "error", err)
		}
	}
	cache.SyncEditExtensionFields([]cache.EditKey{{RepoURL: repoURL, Hash: gc.Hash, Branch: branch}})
}

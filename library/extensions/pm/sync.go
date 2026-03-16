// sync.go - PM extension sync and commit processing
package pm

import (
	"sync"

	"github.com/gitsocial-org/gitsocial/core/cache"
	"github.com/gitsocial-org/gitsocial/core/git"
	"github.com/gitsocial-org/gitsocial/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/core/log"
	"github.com/gitsocial-org/gitsocial/core/protocol"
)

var lastSyncedTip sync.Map

// SyncWorkspaceToCache synchronizes PM commits from the workspace to the cache.
func SyncWorkspaceToCache(workdir string) error {
	branch := gitmsg.GetExtBranch(workdir, "pm")
	repoURL := gitmsg.ResolveRepoURL(workdir)

	tip, err := git.ReadRef(workdir, branch)
	if err != nil {
		return nil
	}
	key := workdir + "\x00" + branch
	if prev, ok := lastSyncedTip.Load(key); ok && prev.(string) == tip {
		return nil
	}

	commits, err := git.GetCommits(workdir, &git.GetCommitsOptions{
		Branch: branch,
	})
	if err != nil {
		return err
	}

	// First ensure commits are in core_commits
	cacheCommits := make([]cache.Commit, 0, len(commits))
	for _, c := range commits {
		cacheCommits = append(cacheCommits, cache.Commit{
			Hash:        c.Hash,
			RepoURL:     repoURL,
			Branch:      branch,
			AuthorName:  c.Author,
			AuthorEmail: c.Email,
			Message:     c.Message,
			Timestamp:   c.Timestamp,
		})
	}
	if err := cache.InsertCommits(cacheCommits); err != nil {
		return err
	}
	liveHashes := make(map[string]bool, len(commits))
	for _, c := range commits {
		liveHashes[c.Hash] = true
	}
	if _, err := cache.MarkCommitsStale(repoURL, branch, liveHashes); err != nil {
		log.Debug("mark stale pm commits failed", "repo", repoURL, "error", err)
	}

	// Process PM-specific items (batch collect then insert)
	var pmItems []PMItem
	var links []pmLinkEntry
	for _, gc := range commits {
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
	for _, lnk := range links {
		if err := InsertLinks(repoURL, lnk.hash, branch, lnk.blocks, lnk.blockedBy, lnk.related); err != nil {
			log.Debug("insert pm links failed", "hash", lnk.hash, "error", err)
		}
	}
	lastSyncedTip.Store(key, tip)
	return nil
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

	blocks := parseRefList(msg.Header.Fields["blocks"], repoURL, branch)
	blockedBy := parseRefList(msg.Header.Fields["blocked-by"], repoURL, branch)
	related := parseRefList(msg.Header.Fields["related"], repoURL, branch)

	var lnk *pmLinkEntry
	if len(blocks) > 0 || len(blockedBy) > 0 || len(related) > 0 {
		lnk = &pmLinkEntry{hash: gc.Hash, blocks: blocks, blockedBy: blockedBy, related: related}
	}

	return &item, lnk
}

// processPMCommit handles a single commit for PM extension processing.
// Matches fetch.CommitProcessor signature for use as a core fetch callback.
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
}

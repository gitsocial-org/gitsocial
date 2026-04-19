// sync.go - Review extension sync and commit processing
package review

import (
	"sync"

	"github.com/gitsocial-org/gitsocial/core/cache"
	"github.com/gitsocial-org/gitsocial/core/fetch"
	"github.com/gitsocial-org/gitsocial/core/git"
	"github.com/gitsocial-org/gitsocial/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/core/log"
	"github.com/gitsocial-org/gitsocial/core/protocol"
)

var lastSyncedTip sync.Map // "workdir\x00branch" → tip hash

func init() {
	fetch.RegisterProcessor("review", func(commits []git.Commit, _, repoURL, extBranch, _ string) {
		ProcessWorkspaceBatch(commits, repoURL, extBranch)
	})
}

// SyncWorkspaceToCache synchronizes review commits from the workspace to the cache.
func SyncWorkspaceToCache(workdir string) error {
	branch := gitmsg.GetExtBranch(workdir, "review")
	repoURL := gitmsg.ResolveRepoURL(workdir)

	tip, err := git.ReadRef(workdir, branch)
	if err != nil {
		return nil // branch doesn't exist yet
	}
	key := workdir + "\x00" + branch
	if prev, ok := lastSyncedTip.Load(key); ok && prev.(string) == tip {
		return nil
	}
	if persisted, err := cache.GetSyncTip(key); err == nil && persisted == tip {
		lastSyncedTip.Store(key, tip)
		return nil
	}

	commits, err := git.GetCommits(workdir, &git.GetCommitsOptions{
		Branch: branch,
	})
	if err != nil {
		return err
	}

	// Ensure all commits are in core_commits
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
	_, _ = cache.MarkCommitsStale(repoURL, branch, liveHashes)

	// Batch collect and insert review items
	var reviewItems []ReviewItem
	for _, gc := range commits {
		msg := protocol.ParseMessage(gc.Message)
		if item := buildReviewItem(gc, msg, repoURL, branch); item != nil {
			reviewItems = append(reviewItems, *item)
		}
	}
	if err := InsertReviewItems(reviewItems); err != nil {
		log.Debug("batch insert review items failed", "error", err)
	}
	syncEditFields(reviewItems)

	lastSyncedTip.Store(key, tip)
	_ = cache.SetSyncTip(key, tip)
	return nil
}

// ProcessWorkspaceBatch processes pre-fetched commits for review extension items.
// Used by the unified workspace sync to avoid redundant git log calls.
func ProcessWorkspaceBatch(commits []git.Commit, repoURL, branch string) {
	var reviewItems []ReviewItem
	for _, gc := range commits {
		if fetch.CleanRefname(gc.Refname) != branch {
			continue
		}
		msg := protocol.ParseMessage(gc.Message)
		if item := buildReviewItem(gc, msg, repoURL, branch); item != nil {
			reviewItems = append(reviewItems, *item)
		}
	}
	if err := InsertReviewItems(reviewItems); err != nil {
		log.Debug("batch insert review items failed", "error", err)
	}
	syncEditFields(reviewItems)
}

// syncEditFields propagates extension fields from edit items to their canonicals.
func syncEditFields(items []ReviewItem) {
	edits := make([]cache.EditKey, 0, len(items))
	for _, item := range items {
		edits = append(edits, cache.EditKey{RepoURL: item.RepoURL, Hash: item.Hash, Branch: item.Branch})
	}
	cache.SyncEditExtensionFields(edits)
}

// buildReviewItem builds a ReviewItem from a commit and message without inserting.
// Returns nil if the commit is not a review item.
func buildReviewItem(gc git.Commit, msg *protocol.Message, repoURL, branch string) *ReviewItem {
	if msg == nil || msg.Header.Ext != "review" {
		return nil
	}

	itemType := msg.Header.Fields["type"]
	if itemType == "" {
		return nil
	}

	cache.ProcessVersionFromHeader(msg, gc.Hash, repoURL, branch)

	base := msg.Header.Fields["base"]
	head := msg.Header.Fields["head"]
	if head != "" {
		headParsed := protocol.ParseRef(head)
		if headParsed.Repository == "" {
			baseParsed := protocol.ParseRef(base)
			if baseParsed.Repository != "" && baseParsed.Repository != repoURL {
				head = protocol.CreateRef(headParsed.Type, headParsed.Value, repoURL, headParsed.Branch)
			}
		}
	}

	item := ReviewItem{
		RepoURL:          repoURL,
		Hash:             gc.Hash,
		Branch:           branch,
		Type:             itemType,
		State:            cache.ToNullString(msg.Header.Fields["state"]),
		Draft:            boolToInt(msg.Header.Fields["draft"] == "true"),
		Base:             cache.ToNullString(base),
		BaseTip:          cache.ToNullString(msg.Header.Fields["base-tip"]),
		Head:             cache.ToNullString(head),
		HeadTip:          cache.ToNullString(msg.Header.Fields["head-tip"]),
		DependsOn:        cache.ToNullString(msg.Header.Fields["depends-on"]),
		Closes:           cache.ToNullString(msg.Header.Fields["closes"]),
		Reviewers:        cache.ToNullString(msg.Header.Fields["reviewers"]),
		CommitRef:        cache.ToNullString(msg.Header.Fields["commit"]),
		File:             cache.ToNullString(msg.Header.Fields["file"]),
		ReviewStateField: cache.ToNullString(msg.Header.Fields["review-state"]),
		Suggestion:       boolToInt(msg.Header.Fields["suggestion"] == "true"),
	}

	if s := msg.Header.Fields["old-line"]; s != "" {
		if v := parseInt(s); v > 0 {
			item.OldLine = cache.ToNullInt64(v)
		}
	}
	if s := msg.Header.Fields["new-line"]; s != "" {
		if v := parseInt(s); v > 0 {
			item.NewLine = cache.ToNullInt64(v)
		}
	}
	if s := msg.Header.Fields["old-line-end"]; s != "" {
		if v := parseInt(s); v > 0 {
			item.OldLineEnd = cache.ToNullInt64(v)
		}
	}
	if s := msg.Header.Fields["new-line-end"]; s != "" {
		if v := parseInt(s); v > 0 {
			item.NewLineEnd = cache.ToNullInt64(v)
		}
	}

	if prRef := msg.Header.Fields["pull-request"]; prRef != "" {
		ref := protocol.ResolveRefWithDefaults(prRef, repoURL, branch)
		if ref.Hash != "" {
			item.PullRequestRepoURL = cache.ToNullString(ref.RepoURL)
			item.PullRequestHash = cache.ToNullString(ref.Hash)
			item.PullRequestBranch = cache.ToNullString(ref.Branch)
		}
	}

	return &item
}

// processReviewCommit handles a single commit for review extension processing.
// Matches fetch.CommitProcessor signature for use as a core fetch callback.
func processReviewCommit(gc git.Commit, msg *protocol.Message, repoURL, branch string) {
	if item := buildReviewItem(gc, msg, repoURL, branch); item != nil {
		if err := InsertReviewItem(*item); err != nil {
			log.Debug("insert review item failed", "hash", gc.Hash, "error", err)
		}
	}
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func parseInt(s string) int {
	var n int
	for _, c := range s {
		if c >= '0' && c <= '9' {
			n = n*10 + int(c-'0')
		} else {
			return 0
		}
	}
	return n
}

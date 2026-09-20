// sync.go - Review extension sync and commit processing
package review

import (
	"strconv"

	"github.com/gitsocial-org/gitsocial/library/core/cache"
	"github.com/gitsocial-org/gitsocial/library/core/fetch"
	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/log"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
)

// SyncWorkspaceBatch ingests pre-fetched workspace commits from the review branch.
func SyncWorkspaceBatch(commits []git.Commit, workdir, repoURL, _ string) {
	processWorkspaceBatch(commits, repoURL, gitmsg.GetExtBranch(workdir, "review"))
}

// processWorkspaceBatch processes pre-fetched commits into review items.
func processWorkspaceBatch(commits []git.Commit, repoURL, branch string) {
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
	if err := insertReviewItems(reviewItems); err != nil {
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

// buildReviewItem builds a ReviewItem from a commit, nil when the commit is not one.
func buildReviewItem(gc git.Commit, msg *protocol.Message, repoURL, branch string) *ReviewItem {
	if msg == nil || msg.Header.Ext != "review" {
		return nil
	}

	if msg.Header.Fields["type"] == "" {
		return nil
	}

	cache.ProcessVersionFromHeader(msg, gc.Hash, repoURL, branch)

	item := MessageToReviewItem(msg, repoURL, gc.Hash, branch)
	return &item
}

// MessageToReviewItem builds a ReviewItem from a parsed review message and its coordinates.
func MessageToReviewItem(msg *protocol.Message, repoURL, hash, branch string) ReviewItem {
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
		Hash:             hash,
		Branch:           branch,
		Type:             msg.Header.Fields["type"],
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

	if v, err := strconv.Atoi(msg.Header.Fields["old-line"]); err == nil && v > 0 {
		item.OldLine = cache.ToNullInt64(v)
	}
	if v, err := strconv.Atoi(msg.Header.Fields["new-line"]); err == nil && v > 0 {
		item.NewLine = cache.ToNullInt64(v)
	}
	if v, err := strconv.Atoi(msg.Header.Fields["old-line-end"]); err == nil && v > 0 {
		item.OldLineEnd = cache.ToNullInt64(v)
	}
	if v, err := strconv.Atoi(msg.Header.Fields["new-line-end"]); err == nil && v > 0 {
		item.NewLineEnd = cache.ToNullInt64(v)
	}

	if prRef := msg.Header.Fields["pull-request"]; prRef != "" {
		ref := protocol.ResolveRefWithDefaults(prRef, repoURL, branch)
		if ref.Hash != "" {
			item.PullRequestRepoURL = cache.ToNullString(ref.RepoURL)
			item.PullRequestHash = cache.ToNullString(ref.Hash)
			item.PullRequestBranch = cache.ToNullString(ref.Branch)
		}
	}

	return item
}

// processReviewCommit ingests one commit, matching the fetch.CommitProcessor signature.
func processReviewCommit(gc git.Commit, msg *protocol.Message, repoURL, branch string) {
	// GITMSG.md 3.4: gitmsg/review is the only branch scanned for review messages.
	if branch != ReviewBranch {
		return
	}
	if item := buildReviewItem(gc, msg, repoURL, branch); item != nil {
		if err := InsertReviewItem(*item); err != nil {
			log.Debug("insert review item failed", "hash", gc.Hash, "error", err)
		}
		cache.SyncEditExtensionFields([]cache.EditKey{{RepoURL: repoURL, Hash: gc.Hash, Branch: branch}})
	}
}

// boolToInt maps a bool to the 0 or 1 the cache stores.
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

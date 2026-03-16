// interaction.go - Interaction counts (comments, reposts) storage and refresh
package social

import (
	"database/sql"
	"strings"
	"time"

	"github.com/gitsocial-org/gitsocial/core/cache"
	"github.com/gitsocial-org/gitsocial/core/git"
	"github.com/gitsocial-org/gitsocial/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/core/log"
	"github.com/gitsocial-org/gitsocial/core/protocol"
)

// resolveItem looks up a social item by ref from cache or workspace.
func resolveItem(workdir, itemID string) *SocialItem {
	item, _ := GetSocialItemByRef(itemID, "")
	if item != nil {
		return item
	}
	parsed := protocol.ParseRef(itemID)
	if parsed.Value == "" {
		return nil
	}
	branch := gitmsg.GetExtBranch(workdir, "social")
	commit, err := git.GetCommit(workdir, parsed.Value)
	if err == nil && commit != nil {
		msg := protocol.ParseMessage(commit.Message)
		postType := "post"
		if msg != nil {
			postType = string(GetPostType(msg))
		}
		wsRepoURL := gitmsg.ResolveRepoURL(workdir)
		item := &SocialItem{
			RepoURL:     wsRepoURL,
			Hash:        commit.Hash,
			Branch:      branch,
			AuthorName:  commit.Author,
			AuthorEmail: commit.Email,
			Content:     protocol.ExtractCleanContent(commit.Message),
			Type:        postType,
			Timestamp:   commit.Timestamp,
		}
		if msg != nil {
			item.HeaderExt = msg.Header.Ext
			item.HeaderType = msg.Header.Fields["type"]
			item.HeaderState = msg.Header.Fields["state"]
		}
		if msg != nil && msg.Header.Fields["original"] != "" {
			normalizedRef := protocol.NormalizeRefWithContext(msg.Header.Fields["original"], wsRepoURL, branch)
			origParsed := protocol.ParseRef(normalizedRef)
			if origParsed.Value != "" {
				item.OriginalRepoURL = sql.NullString{String: origParsed.Repository, Valid: true}
				item.OriginalHash = sql.NullString{String: origParsed.Value, Valid: true}
			}
		}
		return item
	}
	cacheBranch := parsed.Branch
	if cacheBranch == "" {
		cacheBranch = branch
	}
	item, _ = GetCachedCommit(parsed.Repository, parsed.Value, cacheBranch)
	return item
}

// CreateCommentOptions configures comment creation.
type CreateCommentOptions struct {
	Origin *protocol.Origin
}

// CreateComment creates a comment on an existing post.
func CreateComment(workdir, targetPostID, content string, opts *CreateCommentOptions) Result[Post] {
	if strings.TrimSpace(content) == "" {
		return Failure[Post]("EMPTY_CONTENT", "Comment content cannot be empty")
	}
	var origin *protocol.Origin
	if opts != nil {
		origin = opts.Origin
	}
	return createInteraction(workdir, PostTypeComment, targetPostID, content, origin)
}

// CreateRepost creates a repost of an existing post.
func CreateRepost(workdir, targetPostID string) Result[Post] {
	return createInteraction(workdir, PostTypeRepost, targetPostID, "", nil)
}

// CreateQuote creates a quote post with commentary on an existing post.
func CreateQuote(workdir, targetPostID, content string) Result[Post] {
	if strings.TrimSpace(content) == "" {
		return Failure[Post]("EMPTY_CONTENT", "Quote content cannot be empty")
	}
	return createInteraction(workdir, PostTypeQuote, targetPostID, content, nil)
}

// createInteraction creates a comment, repost, or quote interaction.
func createInteraction(workdir string, interactionType PostType, targetPostID, content string, origin *protocol.Origin) Result[Post] {
	targetItem := resolveItem(workdir, targetPostID)
	if targetItem == nil {
		return Failure[Post]("NOT_FOUND", "Target post not found: "+targetPostID)
	}

	// Validation: reposts MUST reference original posts only (no repost chains per GITSOCIAL 1.3)
	if interactionType == PostTypeRepost && targetItem.Type == "repost" {
		return Failure[Post]("INVALID_TARGET", "Cannot repost a repost; reposts must reference original posts")
	}

	// Validation: quotes MUST reference original posts only (no quote chains)
	if interactionType == PostTypeQuote && targetItem.Type == "repost" {
		return Failure[Post]("INVALID_TARGET", "Cannot quote a repost; quotes must reference original posts")
	}

	branch := gitmsg.GetExtBranch(workdir, "social")
	repoURL := gitmsg.ResolveRepoURL(workdir)

	// For refs: use item's branch if remote or cross-extension, workspace branch if local same-extension
	getRefBranch := func(item *SocialItem) string {
		if item.RepoURL != "" && item.RepoURL != repoURL {
			return item.Branch
		}
		if item.Branch != "" && item.Branch != branch {
			return item.Branch
		}
		return branch
	}

	fields := map[string]string{
		"type": string(interactionType),
	}

	var refs []protocol.Ref
	isNested := interactionType == PostTypeComment && targetItem.Type == "comment"

	if isNested {
		// Nested comment: must find the root post (original) for this thread
		// Per GITSOCIAL 1.3: original field MUST reference the thread's first post
		if !targetItem.OriginalRepoURL.Valid || !targetItem.OriginalHash.Valid {
			return Failure[Post]("INVALID_TARGET", "Cannot comment on a comment without a valid root post reference")
		}
		origBranch := ""
		if targetItem.OriginalBranch.Valid {
			origBranch = targetItem.OriginalBranch.String
		}
		originalID := protocol.CreateRef(protocol.RefTypeCommit, targetItem.OriginalHash.String, targetItem.OriginalRepoURL.String, origBranch)
		originalItem := resolveItem(workdir, originalID)
		if originalItem == nil {
			return Failure[Post]("NOT_FOUND", "Root post not found for comment thread")
		}
		// Validate the original is not a comment (prevent circular threading)
		if originalItem.Type == "comment" {
			return Failure[Post]("INVALID_TARGET", "Comment thread root cannot be another comment")
		}
		fields["reply-to"] = protocol.CreateRef(protocol.RefTypeCommit, targetItem.Hash, targetItem.RepoURL, getRefBranch(targetItem))
		fields["original"] = protocol.CreateRef(protocol.RefTypeCommit, originalItem.Hash, originalItem.RepoURL, getRefBranch(originalItem))
		refs = append(refs, buildRefFromItem(targetItem))
		refs = append(refs, buildRefFromItem(originalItem))
	} else {
		fields["original"] = protocol.CreateRef(protocol.RefTypeCommit, targetItem.Hash, targetItem.RepoURL, getRefBranch(targetItem))
		refs = append(refs, buildRefFromItem(targetItem))
	}

	if interactionType == PostTypeRepost && content == "" {
		content = generateRepostContentFromItem(targetItem)
	}

	// Strip workdir from refs for git commit, but preserve URLs for remote repositories
	gitFields := make(map[string]string)
	for k, v := range fields {
		if k == "original" || k == "reply-to" {
			gitFields[k] = protocol.LocalizeRef(v, repoURL)
		} else {
			gitFields[k] = v
		}
	}
	protocol.ApplyOrigin(gitFields, origin)
	gitRefs := make([]protocol.Ref, len(refs))
	for i, r := range refs {
		gitRefs[i] = r
		gitRefs[i].Ref = protocol.LocalizeRef(r.Ref, repoURL)
	}

	header := protocol.Header{
		Ext:        "social",
		V:          "0.1.0",
		Fields:     gitFields,
		FieldOrder: socialFieldOrder,
	}

	message := protocol.FormatMessage(content, header, gitRefs)

	hash, err := git.CreateCommitOnBranch(workdir, branch, message)
	if err != nil {
		return FailureWithDetails[Post]("COMMIT_ERROR", "Failed to create commit", err)
	}

	commit, err := git.GetCommit(workdir, hash)
	if err != nil {
		return FailureWithDetails[Post]("COMMIT_ERROR", "Failed to get commit", err)
	}
	authorName := ""
	authorEmail := ""
	if commit != nil {
		authorName = commit.Author
		authorEmail = commit.Email
	}

	unpushed, _ := git.GetUnpushedCommits(workdir, branch)
	_, isUnpushed := unpushed[hash[:12]]

	originalID := fields["original"]
	replyToID := fields["reply-to"]
	now := time.Now()

	post := Post{
		ID:         protocol.CreateRef(protocol.RefTypeCommit, hash, repoURL, branch),
		Repository: repoURL,
		Branch:     branch,
		Author: Author{
			Name:  authorName,
			Email: authorEmail,
		},
		Timestamp:       now,
		Content:         content,
		Type:            interactionType,
		Source:          PostSourceExplicit,
		CleanContent:    content,
		OriginalPostID:  originalID,
		ParentCommentID: replyToID,
		IsWorkspacePost: true,
		Display: Display{
			CommitHash:      hash,
			IsWorkspacePost: true,
			IsUnpushed:      isUnpushed,
		},
	}

	// Insert into commits and social_items
	if err := cache.InsertCommits([]cache.Commit{{
		Hash:        hash,
		RepoURL:     repoURL,
		Branch:      branch,
		AuthorName:  authorName,
		AuthorEmail: authorEmail,
		Message:     message,
		Timestamp:   now,
	}}); err != nil {
		log.Warn("insert commit failed", "hash", hash, "error", err)
	}

	// Parse refs to get repo_url, hash, and branch components
	originalRepoURL, originalHash, originalBranch := "", "", ""
	if originalID != "" {
		parsed := protocol.ParseRef(originalID)
		if parsed.Value != "" {
			originalRepoURL = parsed.Repository
			originalHash = parsed.Value
			originalBranch = parsed.Branch
			if originalBranch == "" {
				originalBranch = branch // default to current branch
			}
		}
	}
	replyToRepoURL, replyToHash, replyToBranch := "", "", ""
	if replyToID != "" {
		parsed := protocol.ParseRef(replyToID)
		if parsed.Value != "" {
			replyToRepoURL = parsed.Repository
			replyToHash = parsed.Value
			replyToBranch = parsed.Branch
			if replyToBranch == "" {
				replyToBranch = branch // default to current branch
			}
		}
	}
	if err := InsertSocialItem(SocialItem{
		RepoURL:         repoURL,
		Hash:            hash,
		Branch:          branch,
		Type:            string(interactionType),
		OriginalRepoURL: sql.NullString{String: originalRepoURL, Valid: originalRepoURL != ""},
		OriginalHash:    sql.NullString{String: originalHash, Valid: originalHash != ""},
		OriginalBranch:  sql.NullString{String: originalBranch, Valid: originalBranch != ""},
		ReplyToRepoURL:  sql.NullString{String: replyToRepoURL, Valid: replyToRepoURL != ""},
		ReplyToHash:     sql.NullString{String: replyToHash, Valid: replyToHash != ""},
		ReplyToBranch:   sql.NullString{String: replyToBranch, Valid: replyToBranch != ""},
	}); err != nil {
		log.Warn("insert social item failed", "hash", hash, "error", err)
	}

	return Success(post)
}

// buildRefFromItem constructs a protocol reference from a social item.
func buildRefFromItem(item *SocialItem) protocol.Ref {
	itemID := protocol.CreateRef(protocol.RefTypeCommit, item.Hash, item.RepoURL, item.Branch)
	ext := item.HeaderExt
	if ext == "" {
		ext = "social"
	}
	itemType := item.HeaderType
	if itemType == "" {
		itemType = item.Type
	}
	if itemType == "" {
		itemType = "post"
	}
	fields := map[string]string{"type": itemType}
	if item.HeaderState != "" {
		fields["state"] = item.HeaderState
	}
	return protocol.Ref{
		Ext:      ext,
		Author:   item.AuthorName,
		Email:    item.AuthorEmail,
		Time:     item.Timestamp.Format(time.RFC3339),
		Ref:      itemID,
		V:        "0.1.0",
		Fields:   fields,
		Metadata: protocol.QuoteContent(item.Content),
	}
}

// generateRepostContentFromItem creates repost content from the original item.
func generateRepostContentFromItem(item *SocialItem) string {
	author := item.AuthorName
	if author == "" {
		author = "Unknown"
	}

	repoName := protocol.GetFullDisplayName(item.RepoURL)
	firstLine := strings.Split(item.Content, "\n")[0]
	if len(firstLine) > 50 {
		firstLine = firstLine[:47] + "..."
	}

	return "# " + author + " @ " + repoName + ": " + firstLine
}

// EditPost creates a new version of an existing post with updated content.
func EditPost(workdir, targetPostID, newContent string) Result[Post] {
	if strings.TrimSpace(newContent) == "" {
		return Failure[Post]("EMPTY_CONTENT", "Post content cannot be empty")
	}

	targetItem := resolveItem(workdir, targetPostID)
	if targetItem == nil {
		return Failure[Post]("NOT_FOUND", "Target post not found: "+targetPostID)
	}

	branch := gitmsg.GetExtBranch(workdir, "social")
	repoURL := gitmsg.ResolveRepoURL(workdir)

	// Resolve to canonical ID via core_commits_version
	targetRepoURL := targetItem.RepoURL
	if targetRepoURL == "" {
		targetRepoURL = repoURL
	}
	targetBranch := targetItem.Branch
	if targetBranch == "" {
		targetBranch = branch
	}
	canonicalRepoURL, canonicalHash, canonicalBranch, _ := cache.ResolveToCanonical(targetRepoURL, targetItem.Hash, targetBranch)
	canonicalID := protocol.CreateRef(protocol.RefTypeCommit, canonicalHash, canonicalRepoURL, canonicalBranch)

	editsRef := protocol.LocalizeRef(canonicalID, repoURL)

	fields := map[string]string{
		"type":  targetItem.Type,
		"edits": editsRef,
	}

	header := protocol.Header{
		Ext:        "social",
		V:          "0.1.0",
		Fields:     fields,
		FieldOrder: socialFieldOrder,
	}

	message := protocol.FormatMessage(newContent, header, nil)

	hash, err := git.CreateCommitOnBranch(workdir, branch, message)
	if err != nil {
		return FailureWithDetails[Post]("COMMIT_ERROR", "Failed to create commit", err)
	}

	commit, err := git.GetCommit(workdir, hash)
	if err != nil {
		return FailureWithDetails[Post]("COMMIT_ERROR", "Failed to get commit", err)
	}
	authorName := ""
	authorEmail := ""
	if commit != nil {
		authorName = commit.Author
		authorEmail = commit.Email
	}

	unpushed, _ := git.GetUnpushedCommits(workdir, branch)
	_, isUnpushed := unpushed[hash[:12]]

	now := time.Now()
	post := Post{
		ID:         protocol.CreateRef(protocol.RefTypeCommit, hash, repoURL, branch),
		Repository: repoURL,
		Branch:     branch,
		Author: Author{
			Name:  authorName,
			Email: authorEmail,
		},
		Timestamp:       now,
		Content:         newContent,
		Type:            PostType(targetItem.Type),
		Source:          PostSourceExplicit,
		CleanContent:    newContent,
		EditOf:          canonicalID,
		IsWorkspacePost: true,
		Display: Display{
			CommitHash:      hash,
			IsWorkspacePost: true,
			IsUnpushed:      isUnpushed,
		},
	}

	// Insert into commits and social_items
	if err := cache.InsertCommits([]cache.Commit{{
		Hash:        hash,
		RepoURL:     repoURL,
		Branch:      branch,
		AuthorName:  authorName,
		AuthorEmail: authorEmail,
		Message:     message,
		Timestamp:   now,
	}}); err != nil {
		log.Warn("insert commit failed", "hash", hash, "error", err)
	}
	if err := InsertSocialItem(SocialItem{
		RepoURL:         repoURL,
		Hash:            hash,
		Branch:          branch,
		Type:            targetItem.Type,
		OriginalRepoURL: targetItem.OriginalRepoURL,
		OriginalHash:    targetItem.OriginalHash,
		OriginalBranch:  targetItem.OriginalBranch,
		ReplyToRepoURL:  targetItem.ReplyToRepoURL,
		ReplyToHash:     targetItem.ReplyToHash,
		ReplyToBranch:   targetItem.ReplyToBranch,
	}); err != nil {
		log.Warn("insert social item failed", "hash", hash, "error", err)
	}

	return Success(post)
}

// RetractPost marks a post as retracted (soft delete).
func RetractPost(workdir, targetPostID string) Result[bool] {
	targetItem := resolveItem(workdir, targetPostID)
	if targetItem == nil {
		return Failure[bool]("NOT_FOUND", "Target post not found: "+targetPostID)
	}

	branch := gitmsg.GetExtBranch(workdir, "social")
	repoURL := gitmsg.ResolveRepoURL(workdir)

	// Resolve to canonical ID via core_commits_version
	targetRepoURL := targetItem.RepoURL
	if targetRepoURL == "" {
		targetRepoURL = repoURL
	}
	targetBranch := targetItem.Branch
	if targetBranch == "" {
		targetBranch = branch
	}
	canonicalRepoURL, canonicalHash, canonicalBranch, _ := cache.ResolveToCanonical(targetRepoURL, targetItem.Hash, targetBranch)
	canonicalID := protocol.CreateRef(protocol.RefTypeCommit, canonicalHash, canonicalRepoURL, canonicalBranch)

	editsRef := protocol.LocalizeRef(canonicalID, repoURL)

	fields := map[string]string{
		"edits":     editsRef,
		"retracted": "true",
	}

	header := protocol.Header{
		Ext:        "social",
		V:          "0.1.0",
		Fields:     fields,
		FieldOrder: socialFieldOrder,
	}

	// Empty content for retraction
	message := protocol.FormatMessage("", header, nil)

	hash, err := git.CreateCommitOnBranch(workdir, branch, message)
	if err != nil {
		return FailureWithDetails[bool]("COMMIT_ERROR", "Failed to create commit", err)
	}

	commit, err := git.GetCommit(workdir, hash)
	if err != nil {
		return FailureWithDetails[bool]("COMMIT_ERROR", "Failed to get commit", err)
	}
	authorName := ""
	authorEmail := ""
	if commit != nil {
		authorName = commit.Author
		authorEmail = commit.Email
	}

	now := time.Now()

	// Insert into commits and social_items
	if err := cache.InsertCommits([]cache.Commit{{
		Hash:        hash,
		RepoURL:     repoURL,
		Branch:      branch,
		AuthorName:  authorName,
		AuthorEmail: authorEmail,
		Message:     message,
		Timestamp:   now,
	}}); err != nil {
		log.Warn("insert commit failed", "hash", hash, "error", err)
	}
	if err := InsertSocialItem(SocialItem{
		RepoURL:         repoURL,
		Hash:            hash,
		Branch:          branch,
		Type:            targetItem.Type,
		OriginalRepoURL: targetItem.OriginalRepoURL,
		OriginalHash:    targetItem.OriginalHash,
		OriginalBranch:  targetItem.OriginalBranch,
		ReplyToRepoURL:  targetItem.ReplyToRepoURL,
		ReplyToHash:     targetItem.ReplyToHash,
		ReplyToBranch:   targetItem.ReplyToBranch,
	}); err != nil {
		log.Warn("insert social item failed", "hash", hash, "error", err)
	}

	return Success(true)
}

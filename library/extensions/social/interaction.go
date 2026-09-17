// interaction.go - Interaction counts (comments, reposts) storage and refresh
package social

import (
	"database/sql"
	"strings"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/cache"
	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
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
			postType = string(getPostType(msg))
		}
		wsRepoURL := gitmsg.ResolveRepoURL(workdir)
		// The item is attributed to its origin author and time, not to the importer git recorded.
		var header *protocol.Header
		if msg != nil {
			header = &msg.Header
		}
		authorName, authorEmail := protocol.EffectiveAuthor(header, commit.Author, commit.Email)
		item := &SocialItem{
			RepoURL:     wsRepoURL,
			Hash:        commit.Hash,
			Branch:      branch,
			AuthorName:  authorName,
			AuthorEmail: authorEmail,
			Content:     protocol.ExtractCleanContent(commit.Message),
			Type:        postType,
			Timestamp:   protocol.EffectiveTime(header, commit.Timestamp),
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
	item, _ = getCachedCommit(parsed.Repository, parsed.Value, cacheBranch)
	return item
}

// CreateCommentOptions configures comment creation.
type CreateCommentOptions struct {
	Origin *protocol.Origin
	Labels []string
}

// CreateQuoteOptions configures quote creation.
type CreateQuoteOptions struct {
	Origin *protocol.Origin
	Labels []string
}

// CreateRepostOptions configures repost creation.
type CreateRepostOptions struct {
	Labels []string
}

// CreateComment creates a comment on an existing post.
func CreateComment(workdir, targetPostID, content string, opts *CreateCommentOptions) Result[Post] {
	if strings.TrimSpace(content) == "" {
		return failure[Post]("EMPTY_CONTENT", "comment content is empty")
	}
	var origin *protocol.Origin
	var labels []string
	if opts != nil {
		origin = opts.Origin
		labels = opts.Labels
	}
	return createInteraction(workdir, PostTypeComment, targetPostID, content, origin, labels)
}

// CreateRepost creates a repost of an existing post.
func CreateRepost(workdir, targetPostID string, opts *CreateRepostOptions) Result[Post] {
	var labels []string
	if opts != nil {
		labels = opts.Labels
	}
	return createInteraction(workdir, PostTypeRepost, targetPostID, "", nil, labels)
}

// CreateQuote creates a quote post with commentary on an existing post.
func CreateQuote(workdir, targetPostID, content string, opts *CreateQuoteOptions) Result[Post] {
	if strings.TrimSpace(content) == "" {
		return failure[Post]("EMPTY_CONTENT", "quote content is empty")
	}
	var origin *protocol.Origin
	var labels []string
	if opts != nil {
		origin = opts.Origin
		labels = opts.Labels
	}
	return createInteraction(workdir, PostTypeQuote, targetPostID, content, origin, labels)
}

// createInteraction creates a comment, repost, or quote interaction.
func createInteraction(workdir string, interactionType PostType, targetPostID, content string, origin *protocol.Origin, labels []string) Result[Post] {
	targetItem := resolveItem(workdir, targetPostID)
	if targetItem == nil {
		return failure[Post]("NOT_FOUND", "target post not found: "+targetPostID)
	}

	// GITSOCIAL.md 1.3: a repost or quote references an original post, never another repost.
	if interactionType == PostTypeRepost && targetItem.Type == "repost" {
		return failure[Post]("INVALID_TARGET", "cannot repost a repost: repost the original post")
	}

	if interactionType == PostTypeQuote && targetItem.Type == "repost" {
		return failure[Post]("INVALID_TARGET", "cannot quote a repost: quote the original post")
	}

	branch := gitmsg.GetExtBranch(workdir, "social")
	repoURL := gitmsg.ResolveRepoURL(workdir)

	// A ref keeps the item's own branch when it is remote or on another branch.
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
		// GITSOCIAL.md 1.3: original names the thread's first post, not the parent comment.
		if !targetItem.OriginalRepoURL.Valid || !targetItem.OriginalHash.Valid {
			return failure[Post]("INVALID_TARGET", "the target comment has no root post reference")
		}
		origBranch := ""
		if targetItem.OriginalBranch.Valid {
			origBranch = targetItem.OriginalBranch.String
		}
		originalID := protocol.CreateRef(protocol.RefTypeCommit, targetItem.OriginalHash.String, targetItem.OriginalRepoURL.String, origBranch)
		originalItem := resolveItem(workdir, originalID)
		if originalItem == nil {
			return failure[Post]("NOT_FOUND", "root post of the comment thread not found")
		}
		if originalItem.Type == "comment" {
			return failure[Post]("INVALID_TARGET", "the comment thread root is another comment")
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
		content = generateRepostContentFromItem(targetItem, repoURL)
	}

	if labelStr := joinSocialLabels(labels); labelStr != "" {
		fields["labels"] = labelStr
	}

	// The written message carries a local ref bare and a remote ref with its URL.
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

	hash, author, isUnpushed, err := commitSocialMessage(workdir, branch, message)
	if err != nil {
		return failureWithDetails[Post]("COMMIT_ERROR", "create commit", err)
	}

	originalID := fields["original"]
	replyToID := fields["reply-to"]
	now := time.Now()
	originalRepoURL, originalHash, originalBranch := parseSocialRefField(originalID, "", branch)
	replyToRepoURL, replyToHash, replyToBranch := parseSocialRefField(replyToID, "", branch)

	recordSocialCommit(SocialItem{
		RepoURL:         repoURL,
		Hash:            hash,
		Branch:          branch,
		Type:            string(interactionType),
		OriginalRepoURL: cache.ToNullString(originalRepoURL),
		OriginalHash:    cache.ToNullString(originalHash),
		OriginalBranch:  cache.ToNullString(originalBranch),
		ReplyToRepoURL:  cache.ToNullString(replyToRepoURL),
		ReplyToHash:     cache.ToNullString(replyToHash),
		ReplyToBranch:   cache.ToNullString(replyToBranch),
	}, message, author, now)

	return success(Post{
		ID:              protocol.CreateRef(protocol.RefTypeCommit, hash, repoURL, branch),
		Repository:      repoURL,
		Branch:          branch,
		Author:          author,
		Timestamp:       now,
		Content:         content,
		Type:            interactionType,
		CleanContent:    content,
		OriginalPostID:  originalID,
		ParentCommentID: replyToID,
		IsWorkspacePost: true,
		Display: Display{
			CommitHash:      hash,
			IsWorkspacePost: true,
			IsUnpushed:      isUnpushed,
		},
	})
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

// generateRepostContentFromItem writes the repost subject of GITSOCIAL.md 1.2, local or remote form.
func generateRepostContentFromItem(item *SocialItem, workspaceURL string) string {
	author := item.AuthorName
	if author == "" {
		author = "Unknown"
	}

	firstLine := strings.Split(item.Content, "\n")[0]
	// Runes, not bytes: a byte cut through a multi-byte character writes invalid UTF-8.
	if runes := []rune(firstLine); len(runes) > 50 {
		firstLine = string(runes[:47]) + "..."
	}

	if item.RepoURL == "" || item.RepoURL == workspaceURL {
		return "# " + author + ": " + firstLine
	}
	return "# " + author + " @ " + protocol.GetFullDisplayName(item.RepoURL) + ": " + firstLine
}

// addThreadFields carries an item's original and reply-to onto the edit or retraction that replaces it.
func addThreadFields(fields map[string]string, item *SocialItem, workspaceURL string) {
	if ref := localItemRef(item.OriginalRepoURL, item.OriginalHash, item.OriginalBranch, workspaceURL); ref != "" {
		fields["original"] = ref
	}
	if ref := localItemRef(item.ReplyToRepoURL, item.ReplyToHash, item.ReplyToBranch, workspaceURL); ref != "" {
		fields["reply-to"] = ref
	}
}

// localItemRef builds a ref from an item's stored key, relative to the workspace.
func localItemRef(repoURL, hash, branch sql.NullString, workspaceURL string) string {
	if !repoURL.Valid || !hash.Valid || hash.String == "" {
		return ""
	}
	ref := protocol.CreateRef(protocol.RefTypeCommit, hash.String, repoURL.String, branch.String)
	return protocol.LocalizeRef(ref, workspaceURL)
}

// EditPostOptions configures post edits; a nil Labels keeps the canonical's labels.
type EditPostOptions struct {
	Labels *[]string
}

// EditPost creates a new version of an existing post with updated content.
func EditPost(workdir, targetPostID, newContent string, opts *EditPostOptions) Result[Post] {
	if strings.TrimSpace(newContent) == "" {
		return failure[Post]("EMPTY_CONTENT", "post content is empty")
	}

	targetItem := resolveItem(workdir, targetPostID)
	if targetItem == nil {
		return failure[Post]("NOT_FOUND", "target post not found: "+targetPostID)
	}
	if !strings.HasPrefix(targetItem.Branch, "gitmsg/") {
		return failure[Post]("INVALID_TARGET", "cannot edit a post on a code branch: reply with a comment")
	}

	branch := gitmsg.GetExtBranch(workdir, "social")
	repoURL := gitmsg.ResolveRepoURL(workdir)

	if targetItem.RepoURL != "" && targetItem.RepoURL != repoURL {
		return failure[Post]("INVALID_TARGET", "cannot edit a post owned by another repository")
	}

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
	addThreadFields(fields, targetItem, repoURL)
	if opts != nil && opts.Labels != nil {
		fields["labels"] = joinSocialLabels(*opts.Labels)
	}

	header := protocol.Header{
		Ext:        "social",
		V:          "0.1.0",
		Fields:     fields,
		FieldOrder: socialFieldOrder,
	}

	message := protocol.FormatMessage(newContent, header, nil)

	hash, author, isUnpushed, err := commitSocialMessage(workdir, branch, message)
	if err != nil {
		return failureWithDetails[Post]("COMMIT_ERROR", "create commit", err)
	}

	now := time.Now()
	recordSocialCommit(SocialItem{
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
	}, message, author, now)

	return success(Post{
		ID:              protocol.CreateRef(protocol.RefTypeCommit, hash, repoURL, branch),
		Repository:      repoURL,
		Branch:          branch,
		Author:          author,
		Timestamp:       now,
		Content:         newContent,
		Type:            PostType(targetItem.Type),
		CleanContent:    newContent,
		EditOf:          canonicalID,
		IsWorkspacePost: true,
		Display: Display{
			CommitHash:      hash,
			IsWorkspacePost: true,
			IsUnpushed:      isUnpushed,
		},
	})
}

// RetractPost marks a post as retracted (soft delete).
func RetractPost(workdir, targetPostID string) Result[bool] {
	targetItem := resolveItem(workdir, targetPostID)
	if targetItem == nil {
		return failure[bool]("NOT_FOUND", "target post not found: "+targetPostID)
	}
	if !strings.HasPrefix(targetItem.Branch, "gitmsg/") {
		return failure[bool]("INVALID_TARGET", "cannot retract a post on a code branch")
	}

	branch := gitmsg.GetExtBranch(workdir, "social")
	repoURL := gitmsg.ResolveRepoURL(workdir)

	if targetItem.RepoURL != "" && targetItem.RepoURL != repoURL {
		return failure[bool]("INVALID_TARGET", "cannot retract a post owned by another repository")
	}

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
	addThreadFields(fields, targetItem, repoURL)

	header := protocol.Header{
		Ext:        "social",
		V:          "0.1.0",
		Fields:     fields,
		FieldOrder: socialFieldOrder,
	}

	// A retraction carries no content.
	message := protocol.FormatMessage("", header, nil)

	hash, author, _, err := commitSocialMessage(workdir, branch, message)
	if err != nil {
		return failureWithDetails[bool]("COMMIT_ERROR", "create commit", err)
	}

	recordSocialCommit(SocialItem{
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
	}, message, author, time.Now())

	return success(true)
}

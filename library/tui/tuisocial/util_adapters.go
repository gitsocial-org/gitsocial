// util_adapters.go - Adapters bridging social types to tuicore interfaces
package tuisocial

import (
	"fmt"
	"strings"
	"time"

	"github.com/gitsocial-org/gitsocial/core/notifications"
	"github.com/gitsocial-org/gitsocial/core/protocol"
	"github.com/gitsocial-org/gitsocial/core/search"
	"github.com/gitsocial-org/gitsocial/extensions/pm"
	"github.com/gitsocial-org/gitsocial/extensions/release"
	"github.com/gitsocial-org/gitsocial/extensions/review"
	"github.com/gitsocial-org/gitsocial/extensions/social"
	"github.com/gitsocial-org/gitsocial/tui/tuicore"
)

// PostResolver resolves a post ID to a Post
type PostResolver func(postID string) (social.Post, bool)

// PostToCardOptions configures how a Post is converted to a Card
type PostToCardOptions struct {
	FullTime   bool   // Use full timestamp instead of relative
	SkipNested bool   // Don't include nested cards (for nested cards themselves)
	ShowEmail  bool   // Show <email> after name in header
	UserEmail  string // Current user's email for own post detection
}

// PostToCard converts a social.Post to a Card with optional nested post resolution.
// This is the social extension's adapter - it converts social.Post to the generic Card format.
func PostToCard(post social.Post, resolver PostResolver) tuicore.Card {
	return PostToCardWithOptions(post, resolver, PostToCardOptions{})
}

// PostToCardWithOptions converts a social.Post to a Card with configuration options.
func PostToCardWithOptions(post social.Post, resolver PostResolver, cardOpts PostToCardOptions) tuicore.Card {
	name := post.Author.Name
	if name == "" {
		name = "Anonymous"
	}
	if cardOpts.ShowEmail && post.Author.Email != "" {
		name += " <" + post.Author.Email + ">"
	}
	if post.Origin != nil {
		if a := tuicore.FormatOriginAuthorDisplay(post.Origin, cardOpts.ShowEmail); a != "" {
			name = a
		}
	}

	var subtitleParts []tuicore.HeaderPart
	if post.Origin != nil {
		if b := tuicore.FormatOriginBadge(post.Origin); b != "" {
			subtitleParts = append(subtitleParts, tuicore.HeaderPart{Text: b})
		}
	}
	if post.Origin != nil && post.Origin.Time != "" {
		subtitleParts = append(subtitleParts, tuicore.HeaderPart{Text: tuicore.FormatOriginTime(post.Origin.Time)})
	} else if cardOpts.FullTime {
		subtitleParts = append(subtitleParts, tuicore.HeaderPart{Text: tuicore.FormatFullTime(post.Timestamp)})
	} else {
		subtitleParts = append(subtitleParts, tuicore.HeaderPart{Text: tuicore.FormatTime(post.Timestamp)})
	}
	// Show state for cross-extension items (PRs, issues) right after time
	if post.HeaderState != "" && post.HeaderExt != "" && post.HeaderExt != "social" {
		subtitleParts = append(subtitleParts, tuicore.HeaderPart{Text: post.HeaderState})
	}
	if post.Interactions.Comments > 0 {
		loc := tuicore.LocDetail(post.ID)
		subtitleParts = append(subtitleParts, tuicore.HeaderPart{
			Text: fmt.Sprintf("↩ %d", post.Interactions.Comments),
			Link: &loc,
		})
	}
	totalReposts := post.Interactions.Reposts + post.Interactions.Quotes
	if totalReposts > 0 {
		subtitleParts = append(subtitleParts, tuicore.HeaderPart{Text: fmt.Sprintf("↻ %d", totalReposts)})
	}
	originURL := ""
	if post.Origin != nil {
		originURL = post.Origin.URL
	}
	if repoRef := tuicore.BuildRef(post.ID, post.Repository, post.Branch, post.Display.IsWorkspacePost); repoRef != "" {
		var repoLoc *tuicore.Location
		if !post.Display.IsWorkspacePost && post.Repository != "" {
			loc := tuicore.LocRepository(post.Repository, post.Branch)
			repoLoc = &loc
		}
		subtitleParts = append(subtitleParts, tuicore.HeaderPart{Text: repoRef, Link: repoLoc})
	}

	// Title link: search by author across all repos
	var titleLink *tuicore.Location
	authorEmail := post.Author.Email
	if post.Origin != nil && post.Origin.AuthorEmail != "" {
		authorEmail = post.Origin.AuthorEmail
	}
	if authorEmail != "" {
		loc := tuicore.LocSearchQuery("author:" + authorEmail)
		titleLink = &loc
	}

	// Resolve relative URLs for non-workspace posts (workspace posts open files locally)
	content := post.Content
	repoForLinks := ""
	if !post.Display.IsWorkspacePost {
		repoForLinks = post.Repository
		content = tuicore.ResolveContentURLs(content, repoForLinks, "")
	}

	if originURL != "" {
		loc := tuicore.Location{Path: originURL}
		subtitleParts = append(subtitleParts, tuicore.HeaderPart{Text: "↗", Link: &loc})
	}

	card := tuicore.Card{
		Header: tuicore.CardHeader{
			Title:       name,
			TitleLink:   titleLink,
			Subtitle:    subtitleParts,
			Icon:        "•",
			IsEdited:    post.IsEdited,
			IsRetracted: post.IsRetracted,
			IsStale:     post.IsStale,
		},
		Content: tuicore.CardContent{
			Text: content,
		},
		ContentLinks: tuicore.ExtractContentLinks(content, repoForLinks, ""),
	}

	// Handle post type-specific icons and nested content
	switch post.Type {
	case social.PostTypeRepost:
		card.Header.Icon = "↻"
		card.Content.Text = "" // Reposts have no content of their own
		if !cardOpts.SkipNested && resolver != nil && post.OriginalPostID != "" {
			if original, ok := resolver(post.OriginalPostID); ok {
				originalCard := PostToCardWithOptions(original, nil, PostToCardOptions{UserEmail: cardOpts.UserEmail, ShowEmail: cardOpts.ShowEmail})
				card.Nested = append(card.Nested, tuicore.NestedCard{
					Card:     originalCard,
					Position: "after",
					Dimmed:   false,
				})
			}
		}

	case social.PostTypeQuote:
		card.Header.Icon = "↻"
		if !cardOpts.SkipNested && resolver != nil && post.OriginalPostID != "" {
			if original, ok := resolver(post.OriginalPostID); ok {
				originalCard := PostToCardWithOptions(original, nil, PostToCardOptions{UserEmail: cardOpts.UserEmail, ShowEmail: cardOpts.ShowEmail})
				card.Nested = append(card.Nested, tuicore.NestedCard{
					Card:     originalCard,
					Position: "after",
					Dimmed:   true,
					MaxLines: 5,
				})
			}
		}

	case social.PostTypeComment:
		card.Header.Icon = "↩"
		if !cardOpts.SkipNested && resolver != nil {
			parentID := post.ParentCommentID
			if parentID == "" {
				parentID = post.OriginalPostID
			}
			if parentID != "" {
				if parent, ok := resolver(parentID); ok {
					parentCard := PostToCardWithOptions(parent, nil, PostToCardOptions{UserEmail: cardOpts.UserEmail, ShowEmail: cardOpts.ShowEmail})
					card.Nested = append(card.Nested, tuicore.NestedCard{
						Card:     parentCard,
						Position: "after",
						Dimmed:   true,
						MaxLines: 5,
					})
				}
			}
		}
	}

	// Set icon for cross-extension items in social cache
	if post.HeaderExt == "pm" {
		switch post.HeaderType {
		case "issue":
			if post.HeaderState == "closed" || post.HeaderState == "canceled" {
				card.Header.Icon = "●"
			} else {
				card.Header.Icon = "○"
			}
		case "milestone":
			card.Header.Icon = "◇"
		case "sprint":
			card.Header.Icon = "◷"
		}
	}
	if post.HeaderExt == "review" {
		switch post.HeaderType {
		case "pull-request":
			card.Header.Icon = "⑂"
		case "feedback":
			card.Header.Icon = "↩"
		}
	}
	if post.HeaderExt == "release" {
		card.Header.Icon = "⏏"
	}

	// Custom badge from Display (e.g., email for follow notifications)
	if post.Display.Badge != "" && card.Header.Badge == "" {
		card.Header.Badge = post.Display.Badge
	}

	// Unpushed indicator
	if post.Display.IsUnpushed {
		if card.Header.Badge != "" {
			card.Header.Badge += " · ⇡"
		} else {
			card.Header.Badge = "⇡"
		}
	}

	// Mutual follow: they follow you AND you follow them (timeline posts are from repos you follow)
	if post.Display.FollowsYou && !post.Display.IsWorkspacePost {
		card.Header.IsMutualFollow = true
	}

	// Me: author email matches current user's email
	if cardOpts.UserEmail != "" && strings.EqualFold(authorEmail, cardOpts.UserEmail) {
		card.Header.IsMe = true
	}

	// Own repo: post is from the workspace repository
	if post.Display.IsWorkspacePost {
		card.Header.IsOwnRepo = true
	}

	// Verified: precomputed at post-load time so render never blocks on the cache.
	card.Header.IsVerified = post.Display.IsVerified

	return card
}

// RenderCommentCard renders a social comment as an indented card with depth-based nesting.
func RenderCommentCard(comment social.Post, width int, selected bool, searchQuery, userEmail string, showEmail bool, anchors *tuicore.AnchorCollector) []string {
	card := PostToCardWithOptions(comment, nil, PostToCardOptions{
		SkipNested: true,
		UserEmail:  userEmail,
		ShowEmail:  showEmail,
	})
	depth := comment.Depth
	if depth > 8 {
		depth = 8
	}
	indent := ""
	if depth > 0 {
		indent = strings.Repeat("    ", depth)
	}
	cardWidth := width - len(indent)
	opts := tuicore.CardOptions{
		MaxLines:      -1,
		ShowStats:     true,
		Selected:      selected,
		Width:         cardWidth,
		Indent:        indent,
		Markdown:      true,
		WrapWidth:     cardWidth - 2,
		HighlightText: searchQuery,
		Anchors:       anchors,
	}
	rendered := tuicore.RenderCard(card, opts)
	return strings.Split(rendered, "\n")
}

// PostsToItems converts []social.Post to []tuicore.DisplayItem using universal Item
func PostsToItems(posts []social.Post, userEmail string, showEmail bool) []tuicore.DisplayItem {
	items := make([]tuicore.DisplayItem, len(posts))
	for i, p := range posts {
		// Store rendering options in the post's Display field
		p.Display.UserEmail = userEmail
		p.Display.ShowEmail = showEmail
		item := tuicore.NewItem(p.ID, "social", string(p.Type), p.Timestamp, p)
		// Handle cross-extension routing (comments on PM items, etc.)
		// OriginalExt/OriginalType affect navigation routing via ItemType()
		// OriginalID ensures the target detail view receives the referenced item's ID
		if p.Type == social.PostTypeComment && p.OriginalExtension != "" {
			item.OriginalExt = p.OriginalExtension
			item.OriginalType = p.OriginalType
			item.OriginalID = p.OriginalPostID
		} else if p.HeaderExt != "" && p.HeaderExt != "social" {
			// Non-social items (issues, milestones, sprints) stored in social cache:
			// Use OriginalExt/Type for navigation routing, keep Ext/Type as "social"
			// for correct card rendering (data is social.Post, not pm.Issue)
			item.OriginalExt = p.HeaderExt
			item.OriginalType = p.HeaderType
		}
		items[i] = item
	}
	return items
}

// ItemToPost extracts social.Post from a DisplayItem
func ItemToPost(item tuicore.DisplayItem) (social.Post, bool) {
	if ui, ok := item.(tuicore.Item); ok {
		if p, ok := ui.Data.(social.Post); ok {
			return p, true
		}
	}
	return social.Post{}, false
}

// MakeSearchFunc creates a SearchFunc that wraps core/search.Search.
func MakeSearchFunc(userEmail string, showEmailFn func() bool) tuicore.SearchFunc {
	return func(workdir, query, scope string, limit, offset int) (tuicore.SearchResult, error) {
		result, err := search.Search(workdir, search.Params{
			Query:  query,
			Scope:  scope,
			Limit:  limit,
			Offset: offset,
		})
		if err != nil {
			return tuicore.SearchResult{}, err
		}
		showEmail := showEmailFn()
		items := make([]tuicore.DisplayItem, len(result.Results))
		for i, si := range result.Results {
			items[i] = searchItemToDisplayItem(si.Item, userEmail, showEmail)
		}
		return tuicore.SearchResult{
			Items:         items,
			Total:         result.Total,
			TotalSearched: result.TotalSearched,
			HasMore:       result.HasMore,
		}, nil
	}
}

// searchItemToDisplayItem converts a search.Item to the appropriate extension-specific DisplayItem.
func searchItemToDisplayItem(item search.Item, userEmail string, showEmail bool) tuicore.DisplayItem {
	id := protocol.CreateRef(protocol.RefTypeCommit, item.Hash, item.RepoURL, item.Branch)
	subject, body := protocol.SplitSubjectBody(item.Content)

	switch item.Extension {
	case "pm":
		return searchItemToPMDisplayItem(item, id, subject, body)
	case "review":
		if item.Type == "pull-request" {
			return searchItemToReviewDisplayItem(item, id, subject, body)
		}
	case "release":
		return searchItemToReleaseDisplayItem(item, id, subject, body)
	}

	// Default: social post
	post := searchItemToPost(item)
	post.Display.UserEmail = userEmail
	post.Display.ShowEmail = showEmail
	di := tuicore.NewItem(post.ID, "social", string(post.Type), post.Timestamp, post)
	if post.HeaderExt != "" && post.HeaderExt != "social" {
		di.OriginalExt = post.HeaderExt
		di.OriginalType = post.HeaderType
	}
	return di
}

// searchItemToPMDisplayItem creates a PM issue DisplayItem from a search result.
func searchItemToPMDisplayItem(item search.Item, id, subject, body string) tuicore.DisplayItem {
	issue := pm.Issue{
		ID:         id,
		Repository: item.RepoURL,
		Branch:     item.Branch,
		Author:     pm.Author{Name: item.AuthorName, Email: item.AuthorEmail},
		Timestamp:  item.Timestamp,
		Subject:    subject,
		Body:       body,
		State:      pm.State(item.State),
		Assignees:  splitNonEmpty(item.Assignees, ","),
		Labels:     parseSearchLabels(item.Labels),
		Comments:   item.Comments,
	}
	if item.Due != "" {
		if t, err := time.Parse(time.RFC3339, item.Due); err == nil {
			issue.Due = &t
		} else if t, err := time.Parse("2006-01-02", item.Due); err == nil {
			issue.Due = &t
		}
	}
	return tuicore.NewItem(id, "pm", item.Type, item.Timestamp, issue)
}

// searchItemToReviewDisplayItem creates a review PR DisplayItem from a search result.
func searchItemToReviewDisplayItem(item search.Item, id, subject, body string) tuicore.DisplayItem {
	pr := review.PullRequest{
		ID:         id,
		Repository: item.RepoURL,
		Branch:     item.Branch,
		Author:     review.Author{Name: item.AuthorName, Email: item.AuthorEmail},
		Timestamp:  item.Timestamp,
		Subject:    subject,
		Body:       body,
		State:      review.PRState(item.State),
		IsDraft:    item.Draft,
		Base:       item.Base,
		Head:       item.Head,
		Reviewers:  splitNonEmpty(item.Reviewers, ","),
		Labels:     splitNonEmpty(item.Labels, ","),
		Comments:   item.Comments,
	}
	return tuicore.NewItem(id, "review", item.Type, item.Timestamp, pr)
}

// searchItemToReleaseDisplayItem creates a release DisplayItem from a search result.
func searchItemToReleaseDisplayItem(item search.Item, id, subject, body string) tuicore.DisplayItem {
	rel := release.Release{
		ID:         id,
		Repository: item.RepoURL,
		Branch:     item.Branch,
		Author:     release.Author{Name: item.AuthorName, Email: item.AuthorEmail},
		Timestamp:  item.Timestamp,
		Subject:    subject,
		Body:       body,
		Tag:        item.Tag,
		Version:    item.Version,
		Prerelease: item.Prerelease,
		Comments:   item.Comments,
	}
	return tuicore.NewItem(id, "release", "release", item.Timestamp, rel)
}

// searchItemToPost converts a search.Item to a social.Post for card rendering.
func searchItemToPost(item search.Item) social.Post {
	postType := social.PostType(item.Type)
	if postType == "" {
		postType = social.PostTypePost
	}
	id := protocol.CreateRef(protocol.RefTypeCommit, item.Hash, item.RepoURL, item.Branch)
	post := social.Post{
		ID:           id,
		Repository:   item.RepoURL,
		Branch:       item.Branch,
		Author:       social.Author{Name: item.AuthorName, Email: item.AuthorEmail},
		Timestamp:    item.Timestamp,
		Content:      item.Content,
		CleanContent: item.Content,
		Type:         postType,
		IsVirtual:    item.IsVirtual,
		IsStale:      item.IsStale,
		Display: social.Display{
			CommitHash: item.Hash,
		},
	}
	// Preserve actual extension/type/state for correct icons and navigation routing
	if item.Extension != "" && item.Extension != "unknown" {
		post.HeaderExt = item.Extension
		post.HeaderType = item.Type
		post.HeaderState = item.State
	}
	return post
}

// splitNonEmpty splits a string by sep and returns non-empty trimmed parts.
func splitNonEmpty(s, sep string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, sep)
	var result []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

// parseSearchLabels parses comma-separated scoped labels into pm.Label structs.
func parseSearchLabels(labelsStr string) []pm.Label {
	if labelsStr == "" {
		return nil
	}
	parts := strings.Split(labelsStr, ",")
	var labels []pm.Label
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if idx := strings.Index(p, "/"); idx > 0 {
			labels = append(labels, pm.Label{Scope: p[:idx], Value: p[idx+1:]})
		} else {
			labels = append(labels, pm.Label{Value: p})
		}
	}
	return labels
}

// MakeGetNotificationsFunc creates a GetNotificationsFunc that wraps core notifications.GetAll.
func MakeGetNotificationsFunc(userEmail string, showEmailFn func() bool) tuicore.GetNotificationsFunc {
	return func(workdir string, unreadOnly bool) (tuicore.NotificationsResult, error) {
		filter := notifications.Filter{UnreadOnly: unreadOnly}
		all, err := notifications.GetAll(workdir, filter)
		if err != nil {
			return tuicore.NotificationsResult{}, err
		}
		showEmail := showEmailFn()
		items := make([]tuicore.DisplayItem, 0, len(all))
		meta := make([]tuicore.NotificationMeta, 0, len(all))
		for _, n := range all {
			switch n.Source {
			case "social":
				sn, ok := n.Item.(social.Notification)
				if !ok {
					continue
				}
				if sn.Item == nil {
					item := tuicore.NewItem(sn.ID, "social", "follow", n.Timestamp, sn)
					items = append(items, item)
					meta = append(meta, tuicore.NotificationMeta{
						RepoURL:   n.ActorRepo,
						Hash:      sn.CommitHash,
						Branch:    sn.Branch,
						Type:      n.Type,
						ActorRepo: n.ActorRepo,
						IsRead:    n.IsRead,
					})
				} else {
					post := *sn.Item
					post.Display.IsNotificationRead = n.IsRead
					post.Display.UserEmail = userEmail
					post.Display.ShowEmail = showEmail
					item := tuicore.NewItem(post.ID, "social", string(post.Type), post.Timestamp, post)
					items = append(items, item)
					hash := protocol.ParseRef(sn.Item.ID).Value
					meta = append(meta, tuicore.NotificationMeta{
						RepoURL:   sn.Item.Repository,
						Hash:      hash,
						Branch:    sn.Item.Branch,
						Type:      n.Type,
						ActorRepo: n.ActorRepo,
						IsRead:    n.IsRead,
					})
				}
			case "review":
				rn, ok := n.Item.(review.ReviewNotification)
				if !ok {
					continue
				}
				item := tuicore.NewItem(rn.ID, "review", rn.Type, rn.Timestamp, rn)
				items = append(items, item)
				meta = append(meta, tuicore.NotificationMeta{
					RepoURL:   rn.RepoURL,
					Hash:      rn.Hash,
					Branch:    rn.Branch,
					Type:      rn.Type,
					ActorRepo: n.ActorRepo,
					IsRead:    n.IsRead,
				})
			case "pm":
				pn, ok := n.Item.(pm.PMNotification)
				if !ok {
					continue
				}
				item := tuicore.NewItem(pn.ID, "pm", pn.Type, pn.Timestamp, pn)
				items = append(items, item)
				meta = append(meta, tuicore.NotificationMeta{
					RepoURL:   pn.RepoURL,
					Hash:      pn.Hash,
					Branch:    pn.Branch,
					Type:      pn.Type,
					ActorRepo: n.ActorRepo,
					IsRead:    n.IsRead,
				})
			case "release":
				rn, ok := n.Item.(release.ReleaseNotification)
				if !ok {
					continue
				}
				item := tuicore.NewItem(rn.ID, "release", rn.Type, rn.Timestamp, rn)
				items = append(items, item)
				meta = append(meta, tuicore.NotificationMeta{
					RepoURL:   rn.RepoURL,
					Hash:      rn.Hash,
					Branch:    rn.Branch,
					Type:      rn.Type,
					ActorRepo: n.ActorRepo,
					IsRead:    n.IsRead,
				})
			default:
				// Core mentions and other providers
				id := n.RepoURL + "#commit:" + n.Hash
				item := tuicore.NewItem(id, n.Source, n.Type, n.Timestamp, n)
				items = append(items, item)
				meta = append(meta, tuicore.NotificationMeta{
					RepoURL:   n.RepoURL,
					Hash:      n.Hash,
					Branch:    n.Branch,
					Type:      n.Type,
					ActorRepo: n.ActorRepo,
					IsRead:    n.IsRead,
				})
			}
		}
		return tuicore.NotificationsResult{Items: items, Meta: meta}, nil
	}
}

// MakeMarkReadFunc creates a MarkReadFunc that wraps notifications.MarkAsRead.
func MakeMarkReadFunc() tuicore.MarkReadFunc {
	return notifications.MarkAsRead
}

// MakeMarkUnreadFunc creates a MarkUnreadFunc that wraps notifications.MarkAsUnread.
func MakeMarkUnreadFunc() tuicore.MarkUnreadFunc {
	return notifications.MarkAsUnread
}

// MakeMarkAllReadFunc creates a MarkAllReadFunc that wraps notifications.MarkAllAsRead.
func MakeMarkAllReadFunc() tuicore.MarkAllReadFunc {
	return notifications.MarkAllAsRead
}

// MakeMarkAllUnreadFunc creates a MarkAllUnreadFunc that wraps notifications.MarkAllAsUnread.
func MakeMarkAllUnreadFunc() tuicore.MarkAllUnreadFunc {
	return notifications.MarkAllAsUnread
}

// MakeResolveItemFunc creates a ResolveItemFunc that wraps social.GetPosts.
func MakeResolveItemFunc(userEmail string) tuicore.ResolveItemFunc {
	return func(workdir, itemID string) (tuicore.DisplayItem, bool) {
		result := social.GetPosts(workdir, "post:"+itemID, nil)
		if result.Success && len(result.Data) > 0 {
			post := result.Data[0]
			post.Display.UserEmail = userEmail
			return tuicore.NewItem(post.ID, "social", string(post.Type), post.Timestamp, post), true
		}
		return nil, false
	}
}

// ExtractSearchTerms delegates to tuicore.ExtractSearchTerms.
func ExtractSearchTerms(query string) string {
	return tuicore.ExtractSearchTerms(query)
}

// thread.go - Comment reading, thread building and comment tree sorting
package social

import (
	"database/sql"
	"sort"

	"github.com/gitsocial-org/gitsocial/library/core/cache"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
)

// commentsQuery joins social_items directly, so idx_social_original drives the plan; the original matches by repository and hash.
var commentsQuery = baseDirectSelect + `
		WHERE s.type = 'comment' AND s.original_repo_url = ? AND s.original_hash = ?
		  AND c.is_edit_commit = 0
		  AND c.is_retracted = 0
		  AND (c.stale_since IS NULL OR c.is_virtual = 1)
		ORDER BY COALESCE(c.origin_time, c.timestamp) DESC`

// GetComments reads the live comments on an item, on any branch of its reference, sorted into the reply tree under rootRef.
func GetComments(repoURL, hash, rootRef string) ([]Post, error) {
	items, err := cache.QueryLocked(func(db *sql.DB) ([]SocialItem, error) {
		// The empty workspace URL leaves the FollowsYou mark unset, as each comment reader already does.
		rows, err := db.Query(commentsQuery, "", repoURL, hash)
		if err != nil {
			return nil, err
		}
		defer rows.Close()

		return scanResolvedRows(rows)
	})
	if err != nil {
		return nil, err
	}
	posts := make([]Post, len(items))
	for i, item := range items {
		posts[i] = SocialItemToPost(item)
	}
	if len(posts) > 0 {
		posts = sortThreadTree(rootRef, posts)
	}
	return posts, nil
}

// normalizedKey keys a post ID by repository and hash, so each branch value of a reference names one post.
func normalizedKey(id string) string {
	parsed := protocol.ParseRef(id)
	if parsed.Value == "" {
		return id
	}
	return parsed.Repository + "|" + parsed.Value
}

// SamePost reports whether two post IDs name one commit, whatever branch each names.
func SamePost(a, b string) bool {
	return normalizedKey(a) == normalizedKey(b)
}

// oneRowPerPost keeps one post per key, the live fetched row over a stale or virtual one.
func oneRowPerPost(posts []Post) []Post {
	index := make(map[string]int, len(posts))
	out := make([]Post, 0, len(posts))
	for _, p := range posts {
		key := normalizedKey(p.ID)
		i, seen := index[key]
		if !seen {
			index[key] = len(out)
			out = append(out, p)
			continue
		}
		if (out[i].IsStale || out[i].IsVirtual) && !p.IsStale && !p.IsVirtual {
			out[i] = p
		}
	}
	return out
}

// sortThreadTree organizes posts into a depth-first tree structure.
func sortThreadTree(rootID string, posts []Post) []Post {
	normalizedRootID := normalizedKey(rootID)
	childrenMap := make(map[string][]Post)
	for _, p := range oneRowPerPost(posts) {
		if normalizedKey(p.ID) == normalizedRootID {
			continue
		}
		parentID := ""
		if p.ParentCommentID != "" {
			parentID = p.ParentCommentID
		} else if p.OriginalPostID != "" && p.Type != PostTypeRepost {
			parentID = p.OriginalPostID
		}
		if parentID != "" {
			key := normalizedKey(parentID)
			childrenMap[key] = append(childrenMap[key], p)
		}
	}
	var walk func(id string, depth int, seen map[string]bool) []Post
	walk = func(id string, depth int, seen map[string]bool) []Post {
		directChildren := childrenMap[normalizedKey(id)]
		if directChildren == nil {
			return nil
		}
		if depth == 1 {
			sort.Slice(directChildren, func(i, j int) bool {
				if directChildren[i].Interactions.Comments != directChildren[j].Interactions.Comments {
					return directChildren[i].Interactions.Comments > directChildren[j].Interactions.Comments
				}
				return directChildren[i].Timestamp.Before(directChildren[j].Timestamp)
			})
		} else {
			sort.Slice(directChildren, func(i, j int) bool {
				return directChildren[i].Timestamp.Before(directChildren[j].Timestamp)
			})
		}
		var result []Post
		for _, child := range directChildren {
			childKey := normalizedKey(child.ID)
			if seen[childKey] {
				continue
			}
			seen[childKey] = true
			child.Depth = depth
			result = append(result, child)
			result = append(result, walk(child.ID, depth+1, seen)...)
		}
		return result
	}
	seen := make(map[string]bool)
	return walk(rootID, 1, seen)
}

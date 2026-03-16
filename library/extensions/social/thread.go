// thread.go - Thread building and comment tree sorting
package social

import (
	"sort"

	"github.com/gitsocial-org/gitsocial/core/protocol"
)

// normalizedKey creates a unique key from a post ID for map lookups.
func normalizedKey(id string) string {
	parsed := protocol.ParseRef(id)
	if parsed.Value == "" {
		return id
	}
	return parsed.Repository + "|" + parsed.Value + "|" + parsed.Branch
}

// SortThreadTree organizes posts into a depth-first tree structure.
func SortThreadTree(rootID string, posts []Post) []Post {
	normalizedRootID := normalizedKey(rootID)
	childrenMap := make(map[string][]Post)
	for _, p := range posts {
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

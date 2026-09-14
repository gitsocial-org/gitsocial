// notes.go - Fetch GitLab issue and merge request notes as conversation comments
package gitlab

import (
	"fmt"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/log"
	importpkg "github.com/gitsocial-org/gitsocial/library/import"
)

// glNote is one note on an issue or a merge request.
type glNote struct {
	ID        int    `json:"id"`
	Body      string `json:"body"`
	System    bool   `json:"system"`
	Author    glUser `json:"author"`
	CreatedAt string `json:"created_at"`
}

// fetchItemNotes converts the notes of every given issue or merge request into comments.
// resource is the API collection, "issues" or "merge_requests"; keyType is the mapping key type.
// A failed lookup is logged and its item skipped, so one unreadable thread does not fail the import.
func (a *Adapter) fetchItemNotes(resource, keyType string, iids []int, opts importpkg.FetchOptions) []importpkg.ImportComment {
	var out []importpkg.ImportComment
	for _, iid := range iids {
		notes, err := a.fetchNotes(resource, iid)
		if err != nil {
			log.Warn("failed to fetch notes", "resource", resource, "iid", iid, "error", err)
			continue
		}
		for _, note := range notes {
			// A system note is GitLab's own activity log, not a comment anyone wrote.
			if note.System {
				continue
			}
			externalID := fmt.Sprintf("%d", note.ID)
			if opts.SkipExternalIDs[keyType+":"+externalID] {
				continue
			}
			if opts.SkipBots && isBot(note.Author.Username, note.Author.Bot) {
				continue
			}
			createdAt, _ := time.Parse(time.RFC3339, note.CreatedAt)
			author := a.resolveUser(note.Author.Username)
			out = append(out, importpkg.ImportComment{
				ExternalID:  externalID,
				PostID:      fmt.Sprintf("%d", iid),
				Content:     note.Body,
				AuthorName:  author.name,
				AuthorEmail: author.email,
				CreatedAt:   createdAt,
			})
		}
	}
	return out
}

// fetchNotes reads every page of one item's notes, oldest first.
func (a *Adapter) fetchNotes(resource string, iid int) ([]glNote, error) {
	path := fmt.Sprintf("projects/%s/%s/%d/notes?per_page=100&order_by=created_at&sort=asc",
		a.projectPath(), resource, iid)
	var all []glNote
	for path != "" {
		var page []glNote
		next, err := a.apiGetPage(path, &page)
		if err != nil {
			return nil, err
		}
		all = append(all, page...)
		path = next
	}
	return all, nil
}

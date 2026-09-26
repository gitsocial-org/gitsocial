// adopt.go - Adopting a registered fork's issue as a workspace copy (GITMSG.md 1.5)
package pm

import (
	"database/sql"
	"slices"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/cache"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
	"github.com/gitsocial-org/gitsocial/library/core/result"
)

// AdoptIssue adopts a registered fork's issue as a workspace copy without changing it; an issue adopted before returns its copy.
func AdoptIssue(workdir, issueRef string) Result[Issue] {
	repoURL := gitmsg.ResolveRepoURL(workdir)
	existing, err := GetPMItemByRef(issueRef, repoURL)
	if err != nil {
		return result.Err[Issue]("NOT_FOUND", "issue not found")
	}
	if existing.RepoURL == repoURL {
		return result.Err[Issue]("NOT_FOREIGN", "the issue is already in this repository")
	}
	if copied := findAdoptedCopy(repoURL, existing.RepoURL, existing.Hash); copied != nil {
		return result.Ok(PMItemToIssue(*copied))
	}
	if !IsRegisteredFork(workdir, existing.RepoURL) {
		return result.Err[Issue]("NOT_A_FORK", "only a registered fork's issue can be adopted: add the fork with gitsocial fork add")
	}
	return UpdateIssue(workdir, issueRef, UpdateIssueOptions{})
}

// adoptedCopies returns the workspace's issues that adopt another repository's issue.
func adoptedCopies(workspaceURL string) ([]PMItem, error) {
	return cache.QueryLocked(func(db *sql.DB) ([]PMItem, error) {
		rows, err := db.Query(baseSelectFromView+`
			WHERE v.repo_url = ? AND v.type = ? AND v.raw_message LIKE ? ESCAPE '\'
			  AND NOT v.is_edit_commit AND NOT v.is_retracted`,
			workspaceURL, string(ItemTypeIssue), "%"+cache.EscapeLike(`adopts="`)+"%")
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var out []PMItem
		for rows.Next() {
			item, err := scanResolvedRow(rows)
			if err != nil {
				return nil, err
			}
			if item.Adopts != "" {
				out = append(out, *item)
			}
		}
		return out, rows.Err()
	})
}

// adoptedKey names an adopted original by repository identity and short hash, the form a copy's adopts ref and a cache row compare in.
func adoptedKey(repoURL, hash string) string {
	if len(hash) > 12 {
		hash = hash[:12]
	}
	return protocol.NormalizeURL(repoURL) + "#" + hash
}

// adoptedOriginals returns the key of every original the workspace has adopted.
func adoptedOriginals(workspaceURL string) map[string]bool {
	copies, err := adoptedCopies(workspaceURL)
	if err != nil {
		return nil
	}
	out := make(map[string]bool, len(copies))
	for _, c := range copies {
		if ref := protocol.ParseRef(c.Adopts); ref.Type == protocol.RefTypeCommit && ref.Value != "" {
			out[adoptedKey(ref.Repository, ref.Value)] = true
		}
	}
	return out
}

// findAdoptedCopy returns the workspace copy adopting the given original, or nil.
func findAdoptedCopy(workspaceURL, repoURL, hash string) *PMItem {
	copies, err := adoptedCopies(workspaceURL)
	if err != nil {
		return nil
	}
	want := adoptedKey(repoURL, hash)
	for i := range copies {
		ref := protocol.ParseRef(copies[i].Adopts)
		if ref.Type == protocol.RefTypeCommit && adoptedKey(ref.Repository, ref.Value) == want {
			return &copies[i]
		}
	}
	return nil
}

// IsRegisteredFork reports whether the workspace registers the repository as a fork, whose issues a change adopts.
func IsRegisteredFork(workdir, repoURL string) bool {
	return slices.Contains(gitmsg.GetForks(workdir), protocol.NormalizeURL(repoURL))
}

// adoptedRefSection snapshots the adopted original's author and content for the copy's GitMsg-Ref trailer.
func adoptedRefSection(existing *PMItem, subject, body, forkRef string) protocol.Ref {
	content := subject
	if body != "" {
		content += "\n\n" + body
	}
	return protocol.Ref{
		Ext:      "pm",
		Author:   existing.AuthorName,
		Email:    existing.AuthorEmail,
		Time:     existing.Timestamp.Format(time.RFC3339),
		Ref:      forkRef,
		V:        "0.1.0",
		Fields:   map[string]string{"type": string(ItemTypeIssue)},
		Metadata: protocol.QuoteContent(content),
	}
}

// adoptedAuthor reads the adopted original's author and time from the copy's GitMsg-Ref trailer.
func adoptedAuthor(adopts string, refs []protocol.Ref) (*Author, time.Time) {
	for _, ref := range refs {
		if ref.Ref != adopts {
			continue
		}
		t, _ := time.Parse(time.RFC3339, ref.Time)
		return &Author{Name: ref.Author, Email: ref.Email}, t
	}
	return nil, time.Time{}
}

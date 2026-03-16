// history.go - Message edit history retrieval and formatting
package gitmsg

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/gitsocial-org/gitsocial/core/cache"
	"github.com/gitsocial-org/gitsocial/core/protocol"
)

type MessageVersion struct {
	ID          string
	CommitHash  string
	Branch      string
	RepoURL     string
	AuthorName  string
	AuthorEmail string
	Timestamp   time.Time
	Extension   string
	Type        string
	Content     string
	EditOf      string
	IsRetracted bool
}

// GetHistory retrieves all versions of a message by its ref.
func GetHistory(ref string, workspaceURL string) ([]MessageVersion, error) {
	parsed := protocol.ParseRef(ref)
	if parsed.Value == "" {
		return nil, nil
	}

	repoURL := parsed.Repository
	if repoURL == "" {
		repoURL = workspaceURL
	}
	commitHash := parsed.Value
	branch := parsed.Branch
	if branch == "" {
		branch = "main"
	}

	// Resolve to canonical if this is an edit
	canonicalRepoURL, canonicalHash, canonicalBranch, err := cache.ResolveToCanonical(repoURL, commitHash, branch)
	if err != nil {
		return nil, err
	}

	return cache.QueryLocked(func(db *sql.DB) ([]MessageVersion, error) {
		// Get canonical commit and all edits via core_commits_version
		query := `
			SELECT repo_url, hash, branch, author_name, author_email, message, timestamp, edits
			FROM core_commits
			WHERE repo_url = ? AND hash = ? AND branch = ?
			UNION ALL
			SELECT c.repo_url, c.hash, c.branch, c.author_name, c.author_email, c.message, c.timestamp, c.edits
			FROM core_commits c
			JOIN core_commits_version v ON v.edit_repo_url = c.repo_url AND v.edit_hash = c.hash AND v.edit_branch = c.branch
			WHERE v.canonical_repo_url = ? AND v.canonical_hash = ? AND v.canonical_branch = ?
			ORDER BY timestamp DESC`

		rows, err := db.Query(query, canonicalRepoURL, canonicalHash, canonicalBranch, canonicalRepoURL, canonicalHash, canonicalBranch)
		if err != nil {
			return nil, err
		}
		defer rows.Close()

		var versions []MessageVersion
		for rows.Next() {
			var c struct {
				RepoURL     string
				Hash        string
				Branch      string
				AuthorName  string
				AuthorEmail string
				Message     string
				Timestamp   string
				Edits       sql.NullString
			}
			if err := rows.Scan(&c.RepoURL, &c.Hash, &c.Branch, &c.AuthorName, &c.AuthorEmail, &c.Message, &c.Timestamp, &c.Edits); err != nil {
				return nil, err
			}

			ext := ""
			msgType := ""
			isRetracted := false
			if msg := protocol.ParseMessage(c.Message); msg != nil {
				ext = msg.Header.Ext
				if t := msg.Header.Fields["type"]; t != "" {
					msgType = t
				}
				if msg.Header.Fields["retracted"] == "true" {
					isRetracted = true
				}
			}

			ts, _ := time.Parse(time.RFC3339, c.Timestamp) // zero-time fallback acceptable for display
			vBranch := c.Branch
			if vBranch == "" {
				vBranch = branch
			}

			v := MessageVersion{
				ID:          protocol.CreateRef(protocol.RefTypeCommit, c.Hash, c.RepoURL, vBranch),
				CommitHash:  c.Hash,
				Branch:      vBranch,
				RepoURL:     c.RepoURL,
				AuthorName:  c.AuthorName,
				AuthorEmail: c.AuthorEmail,
				Timestamp:   ts,
				Extension:   ext,
				Type:        msgType,
				Content:     protocol.ExtractCleanContent(c.Message),
				IsRetracted: isRetracted,
			}

			if c.Edits.Valid && c.Edits.String != "" {
				v.EditOf = c.Edits.String
			}

			versions = append(versions, v)
		}
		return versions, rows.Err()
	})
}

// FormatHistory formats version history for display.
func FormatHistory(versions []MessageVersion) string {
	if len(versions) == 0 {
		return "No versions found."
	}

	var parts []string
	for i, v := range versions {
		var label string
		if i == 0 {
			label = "latest"
		} else if i == len(versions)-1 && v.EditOf == "" {
			label = "original"
		} else {
			label = fmt.Sprintf("v%d", len(versions)-i-1)
		}

		commitID := v.CommitHash
		if len(commitID) > 12 {
			commitID = commitID[:12]
		}
		ref := fmt.Sprintf("#commit:%s@%s", commitID, v.Branch)
		header := fmt.Sprintf("Version %d (%s) - %s - %s", len(versions)-i-1, label, ref, v.Timestamp.Local().Format("Jan 2, 2006 15:04 MST"))
		if v.IsRetracted {
			parts = append(parts, header+"\n  [deleted]")
		} else {
			content := strings.TrimSpace(v.Content)
			indented := "  " + strings.ReplaceAll(content, "\n", "\n  ")
			parts = append(parts, header+"\n"+indented)
		}
	}

	return strings.Join(parts, "\n\n")
}

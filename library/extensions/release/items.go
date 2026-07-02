// items.go - Release item queries and cache operations
package release

import (
	"database/sql"
	"strings"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/cache"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
	"github.com/gitsocial-org/gitsocial/library/core/result"
	"github.com/gitsocial-org/gitsocial/library/core/text"
)

type ReleaseItem struct {
	RepoURL          string
	Hash             string
	Branch           string
	Tag              sql.NullString
	Version          sql.NullString
	Prerelease       bool
	Artifacts        sql.NullString
	ArtifactURL      sql.NullString
	Checksums        sql.NullString
	SignedBy         sql.NullString
	SBOM             sql.NullString
	Labels           sql.NullString
	Origin           *protocol.Origin
	Content          string
	AuthorName       string
	AuthorEmail      string
	Timestamp        time.Time
	EditOf           sql.NullString
	IsRetracted      bool
	IsEdited         bool
	HasProposedEdits bool
	IsVirtual        bool
	// Derived from social_interactions
	Comments int
}

var baseSelectFromView = cache.ResolvedSelect("release_items_resolved", `v.tag, v.version, v.prerelease, v.artifacts, v.artifact_url,
       v.checksums, v.signed_by, v.sbom, v.labels`)

// InsertReleaseItem inserts or updates a release item in the cache database.
func InsertReleaseItem(item ReleaseItem) error {
	return cache.ExecLocked(func(db *sql.DB) error {
		_, err := db.Exec(`
			INSERT INTO release_items
			(repo_url, hash, branch, tag, version, prerelease, artifacts, artifact_url, checksums, signed_by, sbom)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(repo_url, hash, branch) DO UPDATE SET
				tag = excluded.tag,
				version = excluded.version,
				prerelease = excluded.prerelease,
				artifacts = excluded.artifacts,
				artifact_url = excluded.artifact_url,
				checksums = excluded.checksums,
				signed_by = excluded.signed_by,
				sbom = excluded.sbom`,
			item.RepoURL,
			item.Hash,
			item.Branch,
			item.Tag,
			item.Version,
			item.Prerelease,
			item.Artifacts,
			item.ArtifactURL,
			item.Checksums,
			item.SignedBy,
			item.SBOM,
		)
		return err
	})
}

// GetReleaseItem retrieves a single release item by its composite key.
func GetReleaseItem(repoURL, hash, branch string) (*ReleaseItem, error) {
	return cache.QueryLocked(func(db *sql.DB) (*ReleaseItem, error) {
		query := baseSelectFromView + `
			WHERE v.repo_url = ? AND v.hash = ? AND v.branch = ?
			  AND NOT v.is_edit_commit AND NOT v.is_retracted`
		row := db.QueryRow(query, repoURL, hash, branch)
		return scanResolvedRow(row)
	})
}

// GetReleaseItemByRef looks up a release item by its ref string.
func GetReleaseItemByRef(refStr string, defaultRepoURL string) (*ReleaseItem, error) {
	ref := protocol.ResolveRefWithDefaults(refStr, defaultRepoURL, "gitmsg/release")
	if ref.Hash == "" {
		return nil, sql.ErrNoRows
	}
	return GetReleaseItem(ref.RepoURL, ref.Hash, ref.Branch)
}

// GetReleaseItems queries release items with optional filtering.
func GetReleaseItems(repoURL, branch, cursor string, limit int) ([]ReleaseItem, error) {
	return cache.QueryLocked(func(db *sql.DB) ([]ReleaseItem, error) {
		var args []interface{}
		var where []string

		if repoURL != "" {
			where = append(where, "v.repo_url = ?")
			args = append(args, repoURL)
		}
		if branch != "" {
			where = append(where, "v.branch = ?")
			args = append(args, branch)
		}
		if cursor != "" {
			where = append(where, "v.timestamp < ?")
			args = append(args, cursor)
		}

		where = append(where, "NOT v.is_edit_commit")
		where = append(where, "NOT v.is_retracted")

		sqlQuery := baseSelectFromView
		if len(where) > 0 {
			sqlQuery += " WHERE " + strings.Join(where, " AND ")
		}
		sqlQuery += " ORDER BY v.timestamp DESC"

		if limit > 0 {
			sqlQuery += " LIMIT ?"
			args = append(args, limit)
		}

		rows, err := db.Query(sqlQuery, args...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()

		var items []ReleaseItem
		for rows.Next() {
			item, err := scanResolvedRow(rows)
			if err != nil {
				return nil, err
			}
			items = append(items, *item)
		}
		return items, rows.Err()
	})
}

// CountReleases returns the total number of releases for the given repo/branch.
func CountReleases(repoURL, branch string) (int, error) {
	return cache.QueryLocked(func(db *sql.DB) (int, error) {
		var args []interface{}
		var where []string
		if repoURL != "" {
			where = append(where, "v.repo_url = ?")
			args = append(args, repoURL)
		}
		if branch != "" {
			where = append(where, "v.branch = ?")
			args = append(args, branch)
		}
		where = append(where, "NOT v.is_edit_commit")
		where = append(where, "NOT v.is_retracted")
		query := "SELECT COUNT(*) FROM release_items_resolved v"
		if len(where) > 0 {
			query += " WHERE " + strings.Join(where, " AND ")
		}
		var count int
		err := db.QueryRow(query, args...).Scan(&count)
		return count, err
	})
}

// GetReleases retrieves releases with optional filtering.
func GetReleases(repoURL, branch, cursor string, limit int) Result[[]Release] {
	items, err := GetReleaseItems(repoURL, branch, cursor, limit)
	if err != nil {
		return result.Err[[]Release]("QUERY_FAILED", err.Error())
	}
	releases := make([]Release, len(items))
	for i, item := range items {
		releases[i] = ReleaseItemToRelease(item)
	}
	return result.Ok(releases)
}

// ReleaseItemToRelease converts a ReleaseItem to a Release.
func ReleaseItemToRelease(item ReleaseItem) Release {
	subject, body := protocol.SplitSubjectBody(item.Content)
	id := protocol.CreateRef(protocol.RefTypeCommit, item.Hash, item.RepoURL, item.Branch)

	artifacts := text.SplitCSV(item.Artifacts.String)
	labels := text.SplitCSV(item.Labels.String)

	return Release{
		ID:               id,
		Repository:       item.RepoURL,
		Branch:           item.Branch,
		Author:           Author{Name: item.AuthorName, Email: item.AuthorEmail},
		Timestamp:        item.Timestamp,
		Subject:          subject,
		Body:             body,
		Version:          item.Version.String,
		Tag:              item.Tag.String,
		Prerelease:       item.Prerelease,
		Artifacts:        artifacts,
		ArtifactURL:      item.ArtifactURL.String,
		Checksums:        item.Checksums.String,
		SignedBy:         item.SignedBy.String,
		SBOM:             item.SBOM.String,
		Labels:           labels,
		IsEdited:         item.IsEdited,
		HasProposedEdits: item.HasProposedEdits,
		IsRetracted:      item.IsRetracted,
		Comments:         item.Comments,
		Origin:           item.Origin,
	}
}

// GetArtifactURL returns the full URL for an artifact given a release and filename.
func GetArtifactURL(rel Release, filename string) string {
	if rel.ArtifactURL == "" {
		return ""
	}
	base := strings.TrimRight(rel.ArtifactURL, "/")
	return base + "/" + filename
}

// GetReleaseItemByHashPrefix retrieves a release item by hash prefix using direct SQL.
func GetReleaseItemByHashPrefix(hashPrefix string) (*ReleaseItem, error) {
	return cache.QueryLocked(func(db *sql.DB) (*ReleaseItem, error) {
		query := baseSelectFromView + `
			WHERE v.hash LIKE ? AND NOT v.is_edit_commit AND NOT v.is_retracted
			ORDER BY v.timestamp DESC LIMIT 1`
		row := db.QueryRow(query, cache.EscapeLike(hashPrefix)+"%")
		return scanResolvedRow(row)
	})
}

// GetReleaseItemByTagOrVersion retrieves a release item by tag or version string.
func GetReleaseItemByTagOrVersion(value string) (*ReleaseItem, error) {
	return cache.QueryLocked(func(db *sql.DB) (*ReleaseItem, error) {
		query := baseSelectFromView + `
			WHERE (v.tag = ? OR v.version = ?) AND NOT v.is_edit_commit AND NOT v.is_retracted
			ORDER BY v.timestamp DESC LIMIT 1`
		row := db.QueryRow(query, value, value)
		return scanResolvedRow(row)
	})
}

// GetReleaseItemByFullRef retrieves a release item matching a full ref string or prefix.
// This handles cases where the ref includes repo_url#commit:hash@branch.
func GetReleaseItemByFullRef(refPrefix string) (*ReleaseItem, error) {
	return cache.QueryLocked(func(db *sql.DB) (*ReleaseItem, error) {
		// Match items whose constructed full ref starts with the given prefix
		query := baseSelectFromView + `
			WHERE (v.repo_url || '#commit:' || v.hash || '@' || v.branch) LIKE ? ESCAPE '\'
			  AND NOT v.is_edit_commit AND NOT v.is_retracted
			ORDER BY v.timestamp DESC LIMIT 1`
		row := db.QueryRow(query, cache.EscapeLike(refPrefix)+"%")
		return scanResolvedRow(row)
	})
}

// scanResolvedRow scans a baseSelectFromView row (single- or multi-row query).
func scanResolvedRow(s cache.RowScanner) (*ReleaseItem, error) {
	var item ReleaseItem
	var prerelease int
	meta, err := cache.ScanResolved(s,
		&item.Tag, &item.Version, &prerelease, &item.Artifacts, &item.ArtifactURL,
		&item.Checksums, &item.SignedBy, &item.SBOM, &item.Labels,
	)
	if err != nil {
		return nil, err
	}
	item.RepoURL, item.Hash, item.Branch = meta.RepoURL, meta.Hash, meta.Branch
	item.AuthorName, item.AuthorEmail = meta.AuthorName, meta.AuthorEmail
	item.Content, item.Origin, item.Timestamp = meta.Content, meta.Origin, meta.Timestamp
	item.EditOf, item.Comments = meta.EditOf, meta.Comments
	item.Prerelease = prerelease == 1
	item.IsVirtual, item.IsRetracted = meta.IsVirtual, meta.IsRetracted
	item.IsEdited, item.HasProposedEdits = meta.IsEdited, meta.HasProposed
	return &item, nil
}

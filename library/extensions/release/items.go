// items.go - Release item queries and cache operations
package release

import (
	"database/sql"
	"errors"
	"fmt"
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

// GetReleaseItemByRef looks up a release item by a full or workspace-relative ref, or by a bare hash prefix.
func GetReleaseItemByRef(refStr string, defaultRepoURL string) (*ReleaseItem, error) {
	if refStr == "" {
		return nil, sql.ErrNoRows
	}
	if !strings.Contains(refStr, "#") && !strings.Contains(refStr, "://") {
		if item, err := GetReleaseItem(defaultRepoURL, refStr, ReleaseBranch); err == nil {
			return item, nil
		}
		return GetReleaseItemByHashPrefix(refStr)
	}
	ref := protocol.ResolveRefWithDefaults(refStr, defaultRepoURL, ReleaseBranch)
	if ref.Hash == "" {
		return nil, sql.ErrNoRows
	}
	return GetReleaseItem(ref.RepoURL, ref.Hash, ref.Branch)
}

// notFoundMessage names an ambiguous hash prefix, and the ref itself otherwise.
func notFoundMessage(ref string, err error) string {
	if errors.Is(err, sql.ErrNoRows) {
		return "release not found: " + ref
	}
	return err.Error()
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

		where = append(where, cache.LiveItemFilter)

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
		where = append(where, cache.LiveItemFilter)
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

// MessageToReleaseItem builds a ReleaseItem from a parsed release message and its
// coordinates (pure: reads header fields only, no cache access).
func MessageToReleaseItem(msg *protocol.Message, repoURL, hash, branch string) ReleaseItem {
	return ReleaseItem{
		RepoURL:     repoURL,
		Hash:        hash,
		Branch:      branch,
		Tag:         cache.ToNullString(msg.Header.Fields["tag"]),
		Version:     cache.ToNullString(msg.Header.Fields["version"]),
		Prerelease:  msg.Header.Fields["prerelease"] == "true",
		Artifacts:   cache.ToNullString(msg.Header.Fields["artifacts"]),
		ArtifactURL: cache.ToNullString(msg.Header.Fields["artifact-url"]),
		Checksums:   cache.ToNullString(msg.Header.Fields["checksums"]),
		SignedBy:    cache.ToNullString(msg.Header.Fields["signed-by"]),
		SBOM:        cache.ToNullString(msg.Header.Fields["sbom"]),
	}
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

// GetReleaseItemByHashPrefix retrieves a release item by hash prefix, its live row first, refusing a prefix that several items share.
func GetReleaseItemByHashPrefix(hashPrefix string) (*ReleaseItem, error) {
	hashes, err := cache.QueryLocked(func(db *sql.DB) ([]string, error) {
		rows, err := db.Query(`SELECT DISTINCT hash FROM release_items_resolved
			WHERE hash LIKE ? ESCAPE '\' AND NOT is_edit_commit AND NOT is_retracted
			LIMIT 2`, cache.EscapeLike(hashPrefix)+"%")
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var out []string
		for rows.Next() {
			var hash string
			if err := rows.Scan(&hash); err != nil {
				return nil, err
			}
			out = append(out, hash)
		}
		return out, rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("get release item by hash prefix: %w", err)
	}
	if len(hashes) == 0 {
		return nil, sql.ErrNoRows
	}
	if len(hashes) > 1 {
		return nil, fmt.Errorf("hash %q is ambiguous between %s and %s: use a longer prefix", hashPrefix, hashes[0], hashes[1])
	}
	return cache.QueryLocked(func(db *sql.DB) (*ReleaseItem, error) {
		query := baseSelectFromView + `
			WHERE v.hash = ? AND NOT v.is_edit_commit AND NOT v.is_retracted
			ORDER BY ` + cache.LiveFirstOrder("v") + `, v.timestamp DESC LIMIT 1`
		return scanResolvedRow(db.QueryRow(query, hashes[0]))
	})
}

// GetReleaseItemByTagOrVersion retrieves a release item by tag or version string.
func GetReleaseItemByTagOrVersion(value string) (*ReleaseItem, error) {
	return cache.QueryLocked(func(db *sql.DB) (*ReleaseItem, error) {
		query := baseSelectFromView + `
			WHERE (v.tag = ? OR v.version = ?) AND NOT v.is_edit_commit AND NOT v.is_retracted
			ORDER BY ` + cache.LiveFirstOrder("v") + `, v.timestamp DESC LIMIT 1`
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
			ORDER BY ` + cache.LiveFirstOrder("v") + `, v.timestamp DESC LIMIT 1`
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

// query.go - SQL query building for cross-extension search
package search

import (
	"database/sql"
	"strings"
	"time"
)

// searchQuery holds the resolved search parameters for SQL building.
type searchQuery struct {
	RepoURL      string
	RepoURLs     []string
	Branch       string
	ListID       string
	ListIDs      []string
	WorkspaceURL string
	Types        []string   // social types (post, comment, etc.)
	ExtFilter    *ExtFilter // extension table filter
	Since        *time.Time
	Until        *time.Time
}

// extFilterFromType maps user-facing type names to extension table filters.
func extFilterFromType(typ string) *ExtFilter {
	switch strings.ToLower(typ) {
	case "pr", "pull-request", "pullrequest":
		return &ExtFilter{Table: "review_items", Type: "pull-request"}
	case "issue":
		return &ExtFilter{Table: "pm_items", Type: "issue"}
	case "milestone":
		return &ExtFilter{Table: "pm_items", Type: "milestone"}
	case "sprint":
		return &ExtFilter{Table: "pm_items", Type: "sprint"}
	case "release":
		return &ExtFilter{Table: "release_items"}
	case "feedback", "review":
		return &ExtFilter{Table: "review_items", Type: "feedback"}
	}
	return nil
}

// extensionTable describes an extension table that search can LEFT JOIN.
type extensionTable struct {
	alias   string // SQL alias (si, pi, ri, rli)
	table   string // table name
	typeCol string // column for item type (type or tag)
	extName string // extension name for CASE expression
}

var allExtensionTables = []extensionTable{
	{alias: "si", table: "social_items", typeCol: "type", extName: "social"},
	{alias: "pi", table: "pm_items", typeCol: "type", extName: "pm"},
	{alias: "ri", table: "review_items", typeCol: "type", extName: "review"},
	{alias: "rli", table: "release_items", typeCol: "tag", extName: "release"},
}

// tableExists checks if a table exists in the database.
func tableExists(db *sql.DB, name string) bool {
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", name).Scan(&count)
	return err == nil && count > 0
}

// availableTables returns extension tables that exist in the database.
func availableTables(db *sql.DB) []extensionTable {
	var tables []extensionTable
	for _, t := range allExtensionTables {
		if tableExists(db, t.table) {
			tables = append(tables, t)
		}
	}
	return tables
}

// buildSelect constructs the SELECT with LEFT JOINs for available extension tables.
func buildSelect(tables []extensionTable) string {
	var socialTypeExpr, extCaseExpr, itemTypeExpr string
	joins := make([]string, 0, len(tables))

	// Build COALESCE for social type
	socialTypeExpr = "'unknown'"
	for _, t := range tables {
		if t.extName == "social" {
			socialTypeExpr = "COALESCE(si.type, 'unknown')"
			break
		}
	}

	// Build CASE for extension detection
	caseParts := make([]string, 0, len(tables))
	for _, t := range tables {
		caseParts = append(caseParts, "WHEN "+t.alias+".repo_url IS NOT NULL THEN '"+t.extName+"'")
	}
	if len(caseParts) > 0 {
		extCaseExpr = "CASE " + strings.Join(caseParts, " ") + " ELSE 'unknown' END"
	} else {
		extCaseExpr = "'unknown'"
	}

	// Build COALESCE for item type
	typeParts := make([]string, 0, len(tables))
	for _, t := range tables {
		typeParts = append(typeParts, t.alias+"."+t.typeCol)
	}
	if len(typeParts) > 0 {
		itemTypeExpr = "COALESCE(" + strings.Join(typeParts, ", ") + ", 'unknown')"
	} else {
		itemTypeExpr = "'unknown'"
	}

	// Build LEFT JOINs
	for _, t := range tables {
		joins = append(joins, "LEFT JOIN "+t.table+" "+t.alias+" ON r.repo_url = "+t.alias+".repo_url AND r.hash = "+t.alias+".hash")
	}

	query := `SELECT r.repo_url, r.hash, r.branch,
	       r.author_name, r.author_email, r.resolved_message, r.timestamp,
	       r.is_virtual, r.stale_since,
	       ` + socialTypeExpr + ` as social_type,
	       ` + extCaseExpr + ` as extension,
	       ` + itemTypeExpr + ` as item_type
	FROM core_commits_resolved r
	` + strings.Join(joins, "\n\t")

	return query
}

// buildWhere constructs WHERE clause and args for search queries.
func buildWhere(q searchQuery) (string, []interface{}) {
	var args []interface{}
	var where []string

	// Exclude refs/gitmsg/ config branches
	where = append(where, "r.branch NOT LIKE 'refs/gitmsg/%'")

	if q.RepoURL != "" {
		where = append(where, "r.repo_url = ?")
		args = append(args, q.RepoURL)
	}

	if q.Branch != "" {
		where = append(where, "r.branch = ?")
		args = append(args, q.Branch)
	}

	if len(q.RepoURLs) > 0 {
		ph := strings.Repeat("?,", len(q.RepoURLs))
		ph = ph[:len(ph)-1]
		where = append(where, "r.repo_url IN ("+ph+")")
		for _, url := range q.RepoURLs {
			args = append(args, url)
		}
	}

	if q.ListID != "" {
		where = append(where, "r.repo_url IN (SELECT repo_url FROM core_list_repositories WHERE list_id = ?)")
		args = append(args, q.ListID)
	}

	if len(q.ListIDs) > 0 || q.WorkspaceURL != "" {
		var orClauses []string
		if len(q.ListIDs) > 0 {
			ph := strings.Repeat("?,", len(q.ListIDs))
			ph = ph[:len(ph)-1]
			orClauses = append(orClauses, "r.repo_url IN (SELECT repo_url FROM core_list_repositories WHERE list_id IN ("+ph+"))")
			for _, id := range q.ListIDs {
				args = append(args, id)
			}
		}
		if q.WorkspaceURL != "" {
			orClauses = append(orClauses, "r.repo_url = ?")
			args = append(args, q.WorkspaceURL)
		}
		where = append(where, "("+strings.Join(orClauses, " OR ")+")")
	}

	if q.Since != nil {
		where = append(where, "r.timestamp >= ?")
		args = append(args, q.Since.Format(time.RFC3339))
	}

	if q.Until != nil {
		where = append(where, "r.timestamp <= ?")
		args = append(args, q.Until.Format(time.RFC3339))
	}

	// Social-level type filter (post, comment, repost, quote)
	if len(q.Types) > 0 {
		ph := strings.Repeat("?,", len(q.Types))
		ph = ph[:len(ph)-1]
		where = append(where, "si.type IN ("+ph+")")
		for _, t := range q.Types {
			args = append(args, t)
		}
	}

	// Cross-extension type filter (issue, pr, release, etc.)
	if q.ExtFilter != nil {
		subq := "EXISTS (SELECT 1 FROM " + q.ExtFilter.Table + " e WHERE e.repo_url = r.repo_url AND e.hash = r.hash AND e.branch = r.branch"
		if q.ExtFilter.Type != "" {
			subq += " AND e.type = ?"
			args = append(args, q.ExtFilter.Type)
		}
		subq += ")"
		where = append(where, subq)
	}

	// Standard exclusions
	where = append(where, "NOT r.is_edit_commit")
	where = append(where, "NOT r.is_retracted")
	where = append(where, "(r.stale_since IS NULL OR r.is_virtual = 1)")

	return " WHERE " + strings.Join(where, " AND "), args
}

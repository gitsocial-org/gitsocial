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

	// Text search filters (pushed to SQL for performance)
	TextSearch   string // text to search in message and author fields
	AuthorFilter string // author name/email substring
	HashPrefix   string // hash prefix match

	// Extension-specific SQL filters
	State      string // pm_items.state or review_items.state
	Labels     string // comma-separated labels to match (pm/review)
	Assignee   string // assignee email (pm)
	Reviewer   string // reviewer email (review)
	Draft      bool   // review_items.draft = 1
	Prerelease bool   // release_items.prerelease = 1
	Tag        string // release_items.tag
	Base       string // review_items.base
	Milestone  string // pm milestone name (subquery through core_commits)
	Sprint     string // pm sprint name (subquery through core_commits)
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

	// Text search: filter by message content or author in SQL
	if q.TextSearch != "" {
		where = append(where, "(r.resolved_message LIKE '%' || ? || '%' COLLATE NOCASE OR r.author_name LIKE '%' || ? || '%' COLLATE NOCASE OR r.author_email LIKE '%' || ? || '%' COLLATE NOCASE)")
		args = append(args, q.TextSearch, q.TextSearch, q.TextSearch)
	}
	if q.AuthorFilter != "" {
		where = append(where, "(r.author_name LIKE '%' || ? || '%' COLLATE NOCASE OR r.author_email LIKE '%' || ? || '%' COLLATE NOCASE)")
		args = append(args, q.AuthorFilter, q.AuthorFilter)
	}
	if q.HashPrefix != "" {
		where = append(where, "r.hash LIKE ? || '%'")
		args = append(args, q.HashPrefix)
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

	// Extension-specific filters use resolved views to get current state after edits.
	// Raw tables only reflect creation-time values; resolved views COALESCE edit values.
	if q.State != "" {
		var stateClauses []string
		// pm: open, closed, canceled
		if q.State == "open" || q.State == "closed" || q.State == "canceled" {
			stateClauses = append(stateClauses, "EXISTS (SELECT 1 FROM pm_items_resolved pir WHERE pir.repo_url = r.repo_url AND pir.hash = r.hash AND pir.branch = r.branch AND pir.state = ?)")
			args = append(args, q.State)
		}
		// review: open, merged, closed
		if q.State == "open" || q.State == "merged" || q.State == "closed" {
			stateClauses = append(stateClauses, "EXISTS (SELECT 1 FROM review_items_resolved rir WHERE rir.repo_url = r.repo_url AND rir.hash = r.hash AND rir.branch = r.branch AND rir.state = ?)")
			args = append(args, q.State)
		}
		if len(stateClauses) == 1 {
			where = append(where, stateClauses[0])
		} else if len(stateClauses) > 1 {
			where = append(where, "("+strings.Join(stateClauses, " OR ")+")")
		}
	}
	if q.Draft {
		where = append(where, "EXISTS (SELECT 1 FROM review_items_resolved rir2 WHERE rir2.repo_url = r.repo_url AND rir2.hash = r.hash AND rir2.branch = r.branch AND rir2.draft = 1)")
	}
	if q.Prerelease {
		where = append(where, "EXISTS (SELECT 1 FROM release_items rli2 WHERE rli2.repo_url = r.repo_url AND rli2.hash = r.hash AND rli2.branch = r.branch AND rli2.prerelease = 1)")
	}
	if q.Tag != "" {
		where = append(where, "EXISTS (SELECT 1 FROM release_items rli3 WHERE rli3.repo_url = r.repo_url AND rli3.hash = r.hash AND rli3.branch = r.branch AND rli3.tag = ?)")
		args = append(args, q.Tag)
	}
	if q.Base != "" {
		where = append(where, "EXISTS (SELECT 1 FROM review_items_resolved rir3 WHERE rir3.repo_url = r.repo_url AND rir3.hash = r.hash AND rir3.branch = r.branch AND rir3.base = ?)")
		args = append(args, q.Base)
	}
	if q.Milestone != "" {
		where = append(where, `EXISTS (SELECT 1 FROM pm_items_resolved pir2 WHERE pir2.repo_url = r.repo_url AND pir2.hash = r.hash AND pir2.branch = r.branch
			AND pir2.milestone_hash IS NOT NULL
			AND EXISTS (SELECT 1 FROM core_commits mc WHERE mc.repo_url = pir2.milestone_repo_url AND mc.hash = pir2.milestone_hash AND mc.branch = pir2.milestone_branch AND mc.message LIKE ? || '%'))`)
		args = append(args, q.Milestone)
	}
	if q.Sprint != "" {
		where = append(where, `EXISTS (SELECT 1 FROM pm_items_resolved pir3 WHERE pir3.repo_url = r.repo_url AND pir3.hash = r.hash AND pir3.branch = r.branch
			AND pir3.sprint_hash IS NOT NULL
			AND EXISTS (SELECT 1 FROM core_commits sc WHERE sc.repo_url = pir3.sprint_repo_url AND sc.hash = pir3.sprint_hash AND sc.branch = pir3.sprint_branch AND sc.message LIKE ? || '%'))`)
		args = append(args, q.Sprint)
	}
	if q.Labels != "" {
		// Match any of the provided labels (OR logic). Labels are comma-separated in DB.
		labelList := splitCSV(q.Labels)
		labelClauses := make([]string, 0, 2*len(labelList))
		for _, label := range labelList {
			labelClauses = append(labelClauses,
				"EXISTS (SELECT 1 FROM pm_items_resolved pir4 WHERE pir4.repo_url = r.repo_url AND pir4.hash = r.hash AND pir4.branch = r.branch AND pir4.labels LIKE '%' || ? || '%')")
			args = append(args, label)
			labelClauses = append(labelClauses,
				"EXISTS (SELECT 1 FROM review_items_resolved rir4 WHERE rir4.repo_url = r.repo_url AND rir4.hash = r.hash AND rir4.branch = r.branch AND rir4.labels LIKE '%' || ? || '%')")
			args = append(args, label)
		}
		where = append(where, "("+strings.Join(labelClauses, " OR ")+")")
	}
	if q.Assignee != "" {
		where = append(where, "EXISTS (SELECT 1 FROM pm_items_resolved pir5 WHERE pir5.repo_url = r.repo_url AND pir5.hash = r.hash AND pir5.branch = r.branch AND (',' || REPLACE(pir5.assignees, ' ', '') || ',') LIKE '%,' || ? || ',%')")
		args = append(args, q.Assignee)
	}
	if q.Reviewer != "" {
		where = append(where, "EXISTS (SELECT 1 FROM review_items_resolved rir5 WHERE rir5.repo_url = r.repo_url AND rir5.hash = r.hash AND rir5.branch = r.branch AND (',' || REPLACE(rir5.reviewers, ' ', '') || ',') LIKE '%,' || ? || ',%')")
		args = append(args, q.Reviewer)
	}

	// Standard exclusions
	where = append(where, "NOT r.is_edit_commit")
	where = append(where, "NOT r.is_retracted")
	where = append(where, "(r.stale_since IS NULL OR r.is_virtual = 1)")

	return " WHERE " + strings.Join(where, " AND "), args
}

// splitCSV splits a comma-separated string and trims whitespace from each element.
func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

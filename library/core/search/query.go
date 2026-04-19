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
	SQLLimit     int // pushed to SQL LIMIT; 0 = no limit

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
func buildSelect(tables []extensionTable, hasInteractions bool) string {
	var socialTypeExpr, extCaseExpr, itemTypeExpr string
	joins := make([]string, 0, len(tables)+1)

	// Build COALESCE for social type
	socialTypeExpr = "'unknown'"
	for _, t := range tables {
		if t.extName == "social" {
			socialTypeExpr = "COALESCE(si.type, 'unknown')"
			break
		}
	}

	// Build CASE for extension detection (prefer specific extensions over social)
	caseParts := make([]string, 0, len(tables))
	for _, t := range tables {
		if t.extName != "social" {
			caseParts = append(caseParts, "WHEN "+t.alias+".repo_url IS NOT NULL THEN '"+t.extName+"'")
		}
	}
	for _, t := range tables {
		if t.extName == "social" {
			caseParts = append(caseParts, "WHEN "+t.alias+".repo_url IS NOT NULL THEN '"+t.extName+"'")
		}
	}
	if len(caseParts) > 0 {
		extCaseExpr = "CASE " + strings.Join(caseParts, " ") + " ELSE 'unknown' END"
	} else {
		extCaseExpr = "'unknown'"
	}

	// Build COALESCE for item type (prefer specific extensions over social)
	typeParts := make([]string, 0, len(tables))
	for _, t := range tables {
		if t.extName != "social" {
			typeParts = append(typeParts, t.alias+"."+t.typeCol)
		}
	}
	for _, t := range tables {
		if t.extName == "social" {
			typeParts = append(typeParts, t.alias+"."+t.typeCol)
		}
	}
	if len(typeParts) > 0 {
		itemTypeExpr = "COALESCE(" + strings.Join(typeParts, ", ") + ", 'unknown')"
	} else {
		itemTypeExpr = "'unknown'"
	}

	// Track available aliases for extension-specific columns
	has := map[string]bool{}
	for _, t := range tables {
		has[t.alias] = true
	}

	// Build extension-specific column expressions
	// State resolves through version chain: latest edit's state, then canonical's raw state.
	// This handles cases where syncResolvedVersion hasn't propagated to the canonical's raw row.
	stateExpr := buildResolvedStateExpr(has)
	assigneesExpr := coalesceStr(has, [][2]string{{"pi", "assignees"}})
	dueExpr := coalesceStr(has, [][2]string{{"pi", "due"}})
	draftExpr := coalesceInt(has, [][2]string{{"ri", "draft"}})
	baseExpr := coalesceStr(has, [][2]string{{"ri", "base"}})
	headExpr := coalesceStr(has, [][2]string{{"ri", "head"}})
	reviewersExpr := coalesceStr(has, [][2]string{{"ri", "reviewers"}})
	tagExpr := coalesceStr(has, [][2]string{{"rli", "tag"}})
	versionExpr := coalesceStr(has, [][2]string{{"rli", "version"}})
	prereleaseExpr := coalesceInt(has, [][2]string{{"rli", "prerelease"}})

	commentsExpr := "0"
	if hasInteractions {
		joins = append(joins, "LEFT JOIN social_interactions sic ON r.repo_url = sic.repo_url AND r.hash = sic.hash AND r.branch = sic.branch")
		commentsExpr = "COALESCE(sic.comments, 0)"
	}

	// Build LEFT JOINs for extension tables
	for _, t := range tables {
		joins = append(joins, "LEFT JOIN "+t.table+" "+t.alias+" ON r.repo_url = "+t.alias+".repo_url AND r.hash = "+t.alias+".hash")
	}

	query := `SELECT r.repo_url, r.hash, r.branch,
	       r.author_name, r.author_email, r.resolved_message, r.timestamp,
	       r.is_virtual, r.stale_since,
	       ` + socialTypeExpr + ` as social_type,
	       ` + extCaseExpr + ` as extension,
	       ` + itemTypeExpr + ` as item_type,
	       ` + stateExpr + ` as item_state,
	       COALESCE(r.labels, '') as item_labels,
	       ` + assigneesExpr + ` as item_assignees,
	       ` + dueExpr + ` as item_due,
	       ` + draftExpr + ` as item_draft,
	       ` + baseExpr + ` as item_base,
	       ` + headExpr + ` as item_head,
	       ` + reviewersExpr + ` as item_reviewers,
	       ` + tagExpr + ` as item_tag,
	       ` + versionExpr + ` as item_version,
	       ` + prereleaseExpr + ` as item_prerelease,
	       ` + commentsExpr + ` as item_comments
	FROM core_commits_resolved r
	` + strings.Join(joins, "\n\t")

	return query
}

// buildResolvedStateExpr builds a state expression that resolves through the version chain.
// Prefers the latest edit's state, falls back to the canonical's raw extension state.
func buildResolvedStateExpr(has map[string]bool) string {
	// Build subquery that gets state from the latest edit's extension row
	var editJoins []string
	var editCols []string
	if has["ri"] {
		editJoins = append(editJoins, "LEFT JOIN review_items _re ON v.edit_repo_url = _re.repo_url AND v.edit_hash = _re.hash AND v.edit_branch = _re.branch")
		editCols = append(editCols, "_re.state")
	}
	if has["pi"] {
		editJoins = append(editJoins, "LEFT JOIN pm_items _pe ON v.edit_repo_url = _pe.repo_url AND v.edit_hash = _pe.hash AND v.edit_branch = _pe.branch")
		editCols = append(editCols, "_pe.state")
	}

	// Fallback: canonical's raw state
	var rawFallbacks []string
	if has["ri"] {
		rawFallbacks = append(rawFallbacks, "ri.state")
	}
	if has["pi"] {
		rawFallbacks = append(rawFallbacks, "pi.state")
	}

	if len(editJoins) == 0 {
		if len(rawFallbacks) == 0 {
			return "''"
		}
		return "COALESCE(" + strings.Join(rawFallbacks, ", ") + ", '')"
	}

	editStateExpr := "COALESCE(" + strings.Join(editCols, ", ") + ")"
	subquery := "(SELECT " + editStateExpr +
		" FROM core_commits_version v " + strings.Join(editJoins, " ") +
		" WHERE v.canonical_repo_url = r.repo_url AND v.canonical_hash = r.hash AND v.canonical_branch = r.branch" +
		" AND v.is_retracted = 0" +
		" ORDER BY v.rowid DESC LIMIT 1)"

	parts := make([]string, 0, 1+len(rawFallbacks))
	parts = append(parts, subquery)
	parts = append(parts, rawFallbacks...)
	return "COALESCE(" + strings.Join(parts, ", ") + ", '')"
}

// stateExistsClause builds a WHERE clause that checks state on both the canonical's
// raw extension row AND the latest edit's extension row via the version chain.
func stateExistsClause(table, state string, args *[]interface{}) string {
	*args = append(*args, state, state)
	return `(EXISTS (SELECT 1 FROM ` + table + ` _sr WHERE _sr.repo_url = r.repo_url AND _sr.hash = r.hash AND _sr.branch = r.branch AND _sr.state = ?)` +
		` OR EXISTS (SELECT 1 FROM core_commits_version _sv JOIN ` + table + ` _se ON _sv.edit_repo_url = _se.repo_url AND _sv.edit_hash = _se.hash AND _sv.edit_branch = _se.branch` +
		` WHERE _sv.canonical_repo_url = r.repo_url AND _sv.canonical_hash = r.hash AND _sv.canonical_branch = r.branch AND _sv.is_retracted = 0 AND _se.state = ?))`
}

// coalesceStr builds a COALESCE expression for text columns from available aliases.
func coalesceStr(has map[string]bool, aliasCols [][2]string) string {
	var parts []string
	for _, ac := range aliasCols {
		if has[ac[0]] {
			parts = append(parts, ac[0]+"."+ac[1])
		}
	}
	if len(parts) == 0 {
		return "''"
	}
	return "COALESCE(" + strings.Join(parts, ", ") + ", '')"
}

// coalesceInt builds a COALESCE expression for integer columns from available aliases.
func coalesceInt(has map[string]bool, aliasCols [][2]string) string {
	var parts []string
	for _, ac := range aliasCols {
		if has[ac[0]] {
			parts = append(parts, ac[0]+"."+ac[1])
		}
	}
	if len(parts) == 0 {
		return "0"
	}
	return "COALESCE(" + strings.Join(parts, ", ") + ", 0)"
}

// buildWhere constructs WHERE clause and args for search queries.
func buildWhere(q searchQuery, db *sql.DB) (string, []interface{}) {
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

	// Text search: use FTS5 index when available, fall back to LIKE
	if q.TextSearch != "" {
		if ftsAvailable(db) {
			where = append(where, "r.hash IN (SELECT hash FROM core_fts WHERE core_fts MATCH ?)")
			args = append(args, ftsQuery(q.TextSearch))
		} else {
			where = append(where, "(r.resolved_message LIKE '%' || ? || '%' COLLATE NOCASE OR r.author_name LIKE '%' || ? || '%' COLLATE NOCASE OR r.author_email LIKE '%' || ? || '%' COLLATE NOCASE)")
			args = append(args, q.TextSearch, q.TextSearch, q.TextSearch)
		}
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

	// Extension-specific filters check both the canonical's raw row and the latest
	// edit's row via the version chain, so stale canonical rows are still matched.
	if q.State != "" {
		var stateClauses []string
		if q.State == "open" || q.State == "closed" || q.State == "canceled" {
			stateClauses = append(stateClauses, stateExistsClause("pm_items", q.State, &args))
		}
		if q.State == "open" || q.State == "merged" || q.State == "closed" {
			stateClauses = append(stateClauses, stateExistsClause("review_items", q.State, &args))
		}
		if len(stateClauses) == 1 {
			where = append(where, stateClauses[0])
		} else if len(stateClauses) > 1 {
			where = append(where, "("+strings.Join(stateClauses, " OR ")+")")
		}
	}
	if q.Draft {
		where = append(where, "EXISTS (SELECT 1 FROM review_items rir2 WHERE rir2.repo_url = r.repo_url AND rir2.hash = r.hash AND rir2.branch = r.branch AND rir2.draft = 1)")
	}
	if q.Prerelease {
		where = append(where, "EXISTS (SELECT 1 FROM release_items rli2 WHERE rli2.repo_url = r.repo_url AND rli2.hash = r.hash AND rli2.branch = r.branch AND rli2.prerelease = 1)")
	}
	if q.Tag != "" {
		where = append(where, "EXISTS (SELECT 1 FROM release_items rli3 WHERE rli3.repo_url = r.repo_url AND rli3.hash = r.hash AND rli3.branch = r.branch AND rli3.tag = ?)")
		args = append(args, q.Tag)
	}
	if q.Base != "" {
		where = append(where, "EXISTS (SELECT 1 FROM review_items rir3 WHERE rir3.repo_url = r.repo_url AND rir3.hash = r.hash AND rir3.branch = r.branch AND rir3.base = ?)")
		args = append(args, q.Base)
	}
	if q.Milestone != "" {
		where = append(where, `EXISTS (SELECT 1 FROM pm_items pir2 WHERE pir2.repo_url = r.repo_url AND pir2.hash = r.hash AND pir2.branch = r.branch
			AND pir2.milestone_hash IS NOT NULL
			AND EXISTS (SELECT 1 FROM core_commits mc WHERE mc.repo_url = pir2.milestone_repo_url AND mc.hash = pir2.milestone_hash AND mc.branch = pir2.milestone_branch AND mc.message LIKE ? || '%'))`)
		args = append(args, q.Milestone)
	}
	if q.Sprint != "" {
		where = append(where, `EXISTS (SELECT 1 FROM pm_items pir3 WHERE pir3.repo_url = r.repo_url AND pir3.hash = r.hash AND pir3.branch = r.branch
			AND pir3.sprint_hash IS NOT NULL
			AND EXISTS (SELECT 1 FROM core_commits sc WHERE sc.repo_url = pir3.sprint_repo_url AND sc.hash = pir3.sprint_hash AND sc.branch = pir3.sprint_branch AND sc.message LIKE ? || '%'))`)
		args = append(args, q.Sprint)
	}
	if q.Labels != "" {
		labelList := splitCSV(q.Labels)
		labelClauses := make([]string, 0, 2*len(labelList))
		for _, label := range labelList {
			labelClauses = append(labelClauses,
				"EXISTS (SELECT 1 FROM pm_items pir4 WHERE pir4.repo_url = r.repo_url AND pir4.hash = r.hash AND pir4.branch = r.branch AND pir4.labels LIKE '%' || ? || '%')")
			args = append(args, label)
			labelClauses = append(labelClauses,
				"EXISTS (SELECT 1 FROM review_items rir4 WHERE rir4.repo_url = r.repo_url AND rir4.hash = r.hash AND rir4.branch = r.branch AND rir4.labels LIKE '%' || ? || '%')")
			args = append(args, label)
		}
		where = append(where, "("+strings.Join(labelClauses, " OR ")+")")
	}
	if q.Assignee != "" {
		where = append(where, "EXISTS (SELECT 1 FROM pm_items pir5 WHERE pir5.repo_url = r.repo_url AND pir5.hash = r.hash AND pir5.branch = r.branch AND (',' || REPLACE(pir5.assignees, ' ', '') || ',') LIKE '%,' || ? || ',%')")
		args = append(args, q.Assignee)
	}
	if q.Reviewer != "" {
		where = append(where, "EXISTS (SELECT 1 FROM review_items rir5 WHERE rir5.repo_url = r.repo_url AND rir5.hash = r.hash AND rir5.branch = r.branch AND (',' || REPLACE(rir5.reviewers, ' ', '') || ',') LIKE '%,' || ? || ',%')")
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

// ftsAvailable checks whether the core_fts FTS5 table exists.
func ftsAvailable(db *sql.DB) bool {
	if db == nil {
		return false
	}
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='core_fts'").Scan(&count)
	return err == nil && count > 0
}

// ftsQuery converts a text search string to an FTS5 MATCH query.
// Each word becomes a quoted term; multiple words are ANDed (FTS5 default).
func ftsQuery(text string) string {
	words := strings.Fields(text)
	if len(words) == 0 {
		return text
	}
	quoted := make([]string, len(words))
	for i, w := range words {
		quoted[i] = `"` + strings.ReplaceAll(w, `"`, `""`) + `"*`
	}
	return strings.Join(quoted, " ")
}

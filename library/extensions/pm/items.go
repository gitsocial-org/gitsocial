// items.go - PM item queries and cache operations
package pm

import (
	"database/sql"
	"strings"
	"time"

	"github.com/gitsocial-org/gitsocial/core/cache"
	"github.com/gitsocial-org/gitsocial/core/protocol"
	"github.com/gitsocial-org/gitsocial/core/result"
)

type PMItem struct {
	RepoURL          string
	Hash             string
	Branch           string
	Type             string
	State            string
	Assignees        sql.NullString
	Due              sql.NullString
	StartDate        sql.NullString
	EndDate          sql.NullString
	MilestoneRepoURL sql.NullString
	MilestoneHash    sql.NullString
	MilestoneBranch  sql.NullString
	SprintRepoURL    sql.NullString
	SprintHash       sql.NullString
	SprintBranch     sql.NullString
	ParentRepoURL    sql.NullString
	ParentHash       sql.NullString
	ParentBranch     sql.NullString
	RootRepoURL      sql.NullString
	RootHash         sql.NullString
	RootBranch       sql.NullString
	Labels           sql.NullString
	// Derived from core_commits via JOIN
	Origin      *protocol.Origin
	Content     string
	AuthorName  string
	AuthorEmail string
	Timestamp   time.Time
	EditOf      sql.NullString
	IsRetracted bool
	IsEdited    bool
	IsVirtual   bool
	// Derived from social_interactions
	Comments int
}

const baseSelectFromView = `
	SELECT v.repo_url, v.hash, v.branch,
	       v.author_name, v.author_email, v.resolved_message, v.original_message, v.timestamp,
	       v.type, v.state,
	       v.assignees, v.due, v.start_date, v.end_date,
	       v.milestone_repo_url, v.milestone_hash, v.milestone_branch,
	       v.sprint_repo_url, v.sprint_hash, v.sprint_branch,
	       v.parent_repo_url, v.parent_hash, v.parent_branch,
	       v.root_repo_url, v.root_hash, v.root_branch,
	       v.labels,
	       v.edits, v.is_virtual, v.is_retracted, v.has_edits,
	       v.comments
	FROM pm_items_resolved v
`

// InsertPMItem inserts or updates a PM item in the cache database.
// Wrapped in a single transaction with the pm_assignees rebuild so search
// can never observe a stale linking-table state.
func InsertPMItem(item PMItem) error {
	return cache.ExecLocked(func(db *sql.DB) error {
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback() }()
		if _, err := tx.Exec(`
			INSERT INTO pm_items
			(repo_url, hash, branch, type, state, assignees, due, start_date, end_date, milestone_repo_url, milestone_hash, milestone_branch, sprint_repo_url, sprint_hash, sprint_branch, parent_repo_url, parent_hash, parent_branch, root_repo_url, root_hash, root_branch, labels)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(repo_url, hash, branch) DO UPDATE SET
				type = excluded.type,
				state = excluded.state,
				assignees = excluded.assignees,
				due = excluded.due,
				start_date = excluded.start_date,
				end_date = excluded.end_date,
				milestone_repo_url = excluded.milestone_repo_url,
				milestone_hash = excluded.milestone_hash,
				milestone_branch = excluded.milestone_branch,
				sprint_repo_url = excluded.sprint_repo_url,
				sprint_hash = excluded.sprint_hash,
				sprint_branch = excluded.sprint_branch,
				parent_repo_url = excluded.parent_repo_url,
				parent_hash = excluded.parent_hash,
				parent_branch = excluded.parent_branch,
				root_repo_url = excluded.root_repo_url,
				root_hash = excluded.root_hash,
				root_branch = excluded.root_branch,
				labels = excluded.labels`,
			item.RepoURL, item.Hash, item.Branch,
			item.Type, item.State, item.Assignees,
			item.Due, item.StartDate, item.EndDate,
			item.MilestoneRepoURL, item.MilestoneHash, item.MilestoneBranch,
			item.SprintRepoURL, item.SprintHash, item.SprintBranch,
			item.ParentRepoURL, item.ParentHash, item.ParentBranch,
			item.RootRepoURL, item.RootHash, item.RootBranch,
			item.Labels,
		); err != nil {
			return err
		}
		if err := cache.RebuildCSVLinkingTable(tx, "pm_assignees", "email",
			item.RepoURL, item.Hash, item.Branch, nullStrPM(item.Assignees)); err != nil {
			return err
		}
		return tx.Commit()
	})
}

// nullStrPM unwraps a sql.NullString to its underlying value (empty when invalid).
func nullStrPM(ns sql.NullString) string {
	if ns.Valid {
		return ns.String
	}
	return ""
}

// InsertPMItems batch-inserts multiple PM items in a single transaction.
func InsertPMItems(items []PMItem) error {
	if len(items) == 0 {
		return nil
	}
	return cache.ExecLocked(func(db *sql.DB) error {
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback() }()
		stmt, err := tx.Prepare(`
			INSERT INTO pm_items
			(repo_url, hash, branch, type, state, assignees, due, start_date, end_date, milestone_repo_url, milestone_hash, milestone_branch, sprint_repo_url, sprint_hash, sprint_branch, parent_repo_url, parent_hash, parent_branch, root_repo_url, root_hash, root_branch, labels)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(repo_url, hash, branch) DO UPDATE SET
				type = excluded.type,
				state = excluded.state,
				assignees = excluded.assignees,
				due = excluded.due,
				start_date = excluded.start_date,
				end_date = excluded.end_date,
				milestone_repo_url = excluded.milestone_repo_url,
				milestone_hash = excluded.milestone_hash,
				milestone_branch = excluded.milestone_branch,
				sprint_repo_url = excluded.sprint_repo_url,
				sprint_hash = excluded.sprint_hash,
				sprint_branch = excluded.sprint_branch,
				parent_repo_url = excluded.parent_repo_url,
				parent_hash = excluded.parent_hash,
				parent_branch = excluded.parent_branch,
				root_repo_url = excluded.root_repo_url,
				root_hash = excluded.root_hash,
				root_branch = excluded.root_branch,
				labels = excluded.labels`)
		if err != nil {
			return err
		}
		defer stmt.Close()
		for _, item := range items {
			if _, err := stmt.Exec(
				item.RepoURL, item.Hash, item.Branch,
				item.Type, item.State,
				item.Assignees, item.Due, item.StartDate, item.EndDate,
				item.MilestoneRepoURL, item.MilestoneHash, item.MilestoneBranch,
				item.SprintRepoURL, item.SprintHash, item.SprintBranch,
				item.ParentRepoURL, item.ParentHash, item.ParentBranch,
				item.RootRepoURL, item.RootHash, item.RootBranch,
				item.Labels,
			); err != nil {
				return err
			}
			if err := cache.RebuildCSVLinkingTable(tx, "pm_assignees", "email",
				item.RepoURL, item.Hash, item.Branch, nullStrPM(item.Assignees)); err != nil {
				return err
			}
		}
		return tx.Commit()
	})
}

// GetPMItem retrieves a single PM item by its composite key.
func GetPMItem(repoURL, hash, branch string) (*PMItem, error) {
	return cache.QueryLocked(func(db *sql.DB) (*PMItem, error) {
		query := baseSelectFromView + `
			WHERE v.repo_url = ? AND v.hash = ? AND v.branch = ?
			  AND NOT v.is_edit_commit AND NOT v.is_retracted`
		row := db.QueryRow(query, repoURL, hash, branch)
		return scanResolvedRow(row)
	})
}

// GetPMItemByRef looks up a PM item by its ref string.
func GetPMItemByRef(refStr string, defaultRepoURL string) (*PMItem, error) {
	ref := protocol.ResolveRefWithDefaults(refStr, defaultRepoURL, "gitmsg/pm")
	if ref.Hash == "" {
		return nil, sql.ErrNoRows
	}
	return GetPMItem(ref.RepoURL, ref.Hash, ref.Branch)
}

// GetPMItemByHashPrefix finds a PM item by hash prefix and type using direct SQL.
func GetPMItemByHashPrefix(hashPrefix, itemType string) (*PMItem, error) {
	return cache.QueryLocked(func(db *sql.DB) (*PMItem, error) {
		query := baseSelectFromView + `
			WHERE v.hash LIKE ? AND v.type = ?
			  AND NOT v.is_edit_commit AND NOT v.is_retracted
			ORDER BY v.timestamp DESC LIMIT 1`
		row := db.QueryRow(query, cache.EscapeLike(hashPrefix)+"%", itemType)
		return scanResolvedRow(row)
	})
}

type PMQuery struct {
	Types     []string
	States    []string
	RepoURL   string
	Branch    string
	Labels    []string
	Assignee  string
	Since     *time.Time
	Until     *time.Time
	Limit     int
	Offset    int
	Cursor    string // RFC3339 timestamp — items older than this (keyset pagination)
	FilterStr string // Query filter string (e.g., "state:open priority:high")
	SortField string // Sort field (priority, created, due)
	SortOrder string // Sort order (asc, desc)
}

// GetPMItems queries PM items with filtering and pagination.
func GetPMItems(q PMQuery) ([]PMItem, error) {
	return cache.QueryLocked(func(db *sql.DB) ([]PMItem, error) {
		var args []interface{}
		var where []string

		// Parse filter string if provided
		var parsed Query
		if q.FilterStr != "" {
			parsed = ParseQuery(q.FilterStr)
			filterWhere, filterArgs := parsed.BuildWhereClause()
			if filterWhere != "" {
				where = append(where, filterWhere)
				args = append(args, filterArgs...)
			}
		}

		// Apply sort from query params or parsed filter
		sortField := q.SortField
		sortOrder := q.SortOrder
		if sortField == "" && parsed.SortField != "" {
			sortField = parsed.SortField
		}
		if sortOrder == "" && parsed.SortOrder != "" {
			sortOrder = parsed.SortOrder
		}

		if len(q.Types) > 0 {
			ph := strings.Repeat("?,", len(q.Types))
			ph = ph[:len(ph)-1]
			where = append(where, "v.type IN ("+ph+")")
			for _, t := range q.Types {
				args = append(args, t)
			}
		}

		if len(q.States) > 0 {
			ph := strings.Repeat("?,", len(q.States))
			ph = ph[:len(ph)-1]
			where = append(where, "v.state IN ("+ph+")")
			for _, s := range q.States {
				args = append(args, s)
			}
		}

		if q.RepoURL != "" {
			where = append(where, "v.repo_url = ?")
			args = append(args, q.RepoURL)
		}

		if q.Branch != "" {
			where = append(where, "v.branch = ?")
			args = append(args, q.Branch)
		}

		for _, label := range q.Labels {
			where = append(where, `v.labels LIKE ? ESCAPE '\'`)
			args = append(args, "%"+escapeLike(label)+"%")
		}

		if q.Assignee != "" {
			where = append(where, `v.assignees LIKE ? ESCAPE '\'`)
			args = append(args, "%"+escapeLike(q.Assignee)+"%")
		}

		if q.Since != nil {
			where = append(where, "v.timestamp >= ?")
			args = append(args, q.Since.Format(time.RFC3339))
		}

		if q.Until != nil {
			where = append(where, "v.timestamp <= ?")
			args = append(args, q.Until.Format(time.RFC3339))
		}

		if q.Cursor != "" {
			where = append(where, "v.timestamp < ?")
			args = append(args, q.Cursor)
		}

		where = append(where, "NOT v.is_edit_commit")
		where = append(where, "NOT v.is_retracted")

		sqlQuery := baseSelectFromView
		if len(where) > 0 {
			sqlQuery += " WHERE " + strings.Join(where, " AND ")
		}

		// Apply sorting
		orderClause := buildOrderClause(sortField, sortOrder)
		sqlQuery += " ORDER BY " + orderClause

		if q.Limit > 0 {
			sqlQuery += " LIMIT ?"
			args = append(args, q.Limit)
		}
		if q.Offset > 0 && q.Cursor == "" {
			sqlQuery += " OFFSET ?"
			args = append(args, q.Offset)
		}

		rows, err := db.Query(sqlQuery, args...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()

		var items []PMItem
		for rows.Next() {
			item, err := scanResolvedRows(rows)
			if err != nil {
				return nil, err
			}
			items = append(items, *item)
		}
		return items, rows.Err()
	})
}

// GetPMItemsCount returns the total count of items matching the query (ignoring Limit/Cursor/Offset).
func GetPMItemsCount(q PMQuery) (int, error) {
	q.Limit = 0
	q.Cursor = ""
	q.Offset = 0
	return cache.QueryLocked(func(db *sql.DB) (int, error) {
		var args []interface{}
		var where []string
		if q.FilterStr != "" {
			parsed := ParseQuery(q.FilterStr)
			filterWhere, filterArgs := parsed.BuildWhereClause()
			if filterWhere != "" {
				where = append(where, filterWhere)
				args = append(args, filterArgs...)
			}
		}
		if len(q.Types) > 0 {
			ph := strings.Repeat("?,", len(q.Types))
			ph = ph[:len(ph)-1]
			where = append(where, "v.type IN ("+ph+")")
			for _, t := range q.Types {
				args = append(args, t)
			}
		}
		if len(q.States) > 0 {
			ph := strings.Repeat("?,", len(q.States))
			ph = ph[:len(ph)-1]
			where = append(where, "v.state IN ("+ph+")")
			for _, s := range q.States {
				args = append(args, s)
			}
		}
		if q.RepoURL != "" {
			where = append(where, "v.repo_url = ?")
			args = append(args, q.RepoURL)
		}
		if q.Branch != "" {
			where = append(where, "v.branch = ?")
			args = append(args, q.Branch)
		}
		where = append(where, "NOT v.is_edit_commit")
		where = append(where, "NOT v.is_retracted")
		query := "SELECT COUNT(*) FROM pm_items_resolved v"
		if len(where) > 0 {
			query += " WHERE " + strings.Join(where, " AND ")
		}
		var count int
		err := db.QueryRow(query, args...).Scan(&count)
		return count, err
	})
}

// CountIssues returns the number of issues matching the given states.
func CountIssues(states []string) (int, error) {
	return GetPMItemsCount(PMQuery{Types: []string{string(ItemTypeIssue)}, States: states})
}

// CountMilestones returns the number of milestones matching the given states.
func CountMilestones(repoURL, branch string, states []string) (int, error) {
	return GetPMItemsCount(PMQuery{Types: []string{string(ItemTypeMilestone)}, States: states, RepoURL: repoURL, Branch: branch})
}

// CountSprints returns the number of sprints matching the given states.
func CountSprints(repoURL, branch string, states []string) (int, error) {
	return GetPMItemsCount(PMQuery{Types: []string{string(ItemTypeSprint)}, States: states, RepoURL: repoURL, Branch: branch})
}

// GetIssues retrieves issues with optional filtering.
func GetIssues(repoURL, branch string, states []string, cursor string, limit int) Result[[]Issue] {
	q := PMQuery{
		Types:   []string{string(ItemTypeIssue)},
		States:  states,
		RepoURL: repoURL,
		Branch:  branch,
		Cursor:  cursor,
		Limit:   limit,
	}
	items, err := GetPMItems(q)
	if err != nil {
		return result.Err[[]Issue]("QUERY_FAILED", err.Error())
	}
	issues := make([]Issue, len(items))
	for i, item := range items {
		issues[i] = PMItemToIssue(item)
	}
	return result.Ok(issues)
}

// GetIssuesWithForks retrieves issues from the workspace and registered forks.
// Unlike PRs, fork issues don't need base-ref filtering — all issues from registered forks are included.
func GetIssuesWithForks(workspaceURL, workspaceBranch string, forkURLs, states []string, cursor string, limit int) Result[[]Issue] {
	if len(forkURLs) == 0 {
		return GetIssues(workspaceURL, workspaceBranch, states, cursor, limit)
	}
	repoURLs := append([]string{workspaceURL}, forkURLs...)
	items, err := cache.QueryLocked(func(db *sql.DB) ([]PMItem, error) {
		ph := strings.Repeat("?,", len(repoURLs))
		ph = ph[:len(ph)-1]
		var args []interface{}
		var where []string
		where = append(where, "v.type = ?")
		args = append(args, string(ItemTypeIssue))
		where = append(where, "v.repo_url IN ("+ph+")")
		for _, u := range repoURLs {
			args = append(args, u)
		}
		if len(states) > 0 {
			sph := strings.Repeat("?,", len(states))
			sph = sph[:len(sph)-1]
			where = append(where, "v.state IN ("+sph+")")
			for _, s := range states {
				args = append(args, s)
			}
		}
		if cursor != "" {
			where = append(where, "v.timestamp < ?")
			args = append(args, cursor)
		}
		where = append(where, "NOT v.is_edit_commit")
		where = append(where, "NOT v.is_retracted")
		sqlQuery := baseSelectFromView + " WHERE " + strings.Join(where, " AND ") + " ORDER BY v.timestamp DESC"
		if limit > 0 {
			sqlQuery += " LIMIT ?"
			args = append(args, limit)
		}
		rows, err := db.Query(sqlQuery, args...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var result []PMItem
		for rows.Next() {
			item, err := scanResolvedRows(rows)
			if err != nil {
				return nil, err
			}
			result = append(result, *item)
		}
		return result, rows.Err()
	})
	if err != nil {
		return result.Err[[]Issue]("QUERY_FAILED", err.Error())
	}
	// Deduplicate by hash: workspace items take priority over fork duplicates
	// (can happen when forks are created via git clone --mirror)
	seen := make(map[string]bool, len(items))
	issues := make([]Issue, 0, len(items))
	for _, item := range items {
		if seen[item.Hash] {
			continue
		}
		seen[item.Hash] = true
		issues = append(issues, PMItemToIssue(item))
	}
	return result.Ok(issues)
}

// CountIssuesWithForks counts issues from workspace and forks.
func CountIssuesWithForks(workspaceURL, workspaceBranch string, forkURLs, states []string) int {
	if len(forkURLs) == 0 {
		count, _ := CountIssues(states)
		return count
	}
	res := GetIssuesWithForks(workspaceURL, workspaceBranch, forkURLs, states, "", 0)
	if !res.Success {
		return 0
	}
	return len(res.Data)
}

// GetMilestones retrieves milestones with optional filtering.
func GetMilestones(repoURL, branch string, states []string, cursor string, limit int) Result[[]Milestone] {
	q := PMQuery{
		Types:   []string{string(ItemTypeMilestone)},
		States:  states,
		RepoURL: repoURL,
		Branch:  branch,
		Cursor:  cursor,
		Limit:   limit,
	}
	items, err := GetPMItems(q)
	if err != nil {
		return result.Err[[]Milestone]("QUERY_FAILED", err.Error())
	}
	milestones := make([]Milestone, len(items))
	for i, item := range items {
		milestones[i] = PMItemToMilestone(item)
	}
	return result.Ok(milestones)
}

// GetSprints retrieves sprints with optional filtering, ordered by start date descending.
func GetSprints(repoURL, branch string, states []string, cursor string, limit int) Result[[]Sprint] {
	q := PMQuery{
		Types:     []string{string(ItemTypeSprint)},
		States:    states,
		RepoURL:   repoURL,
		Branch:    branch,
		Cursor:    cursor,
		Limit:     limit,
		SortField: "start",
		SortOrder: "desc",
	}
	items, err := GetPMItems(q)
	if err != nil {
		return result.Err[[]Sprint]("QUERY_FAILED", err.Error())
	}
	sprints := make([]Sprint, len(items))
	for i, item := range items {
		sprints[i] = PMItemToSprint(item)
	}
	return result.Ok(sprints)
}

func scanResolvedRow(row *sql.Row) (*PMItem, error) {
	var item PMItem
	var ts, message, originalMessage sql.NullString
	var isVirtual, isRetracted, hasEdits int
	err := row.Scan(
		&item.RepoURL, &item.Hash, &item.Branch,
		&item.AuthorName, &item.AuthorEmail, &message, &originalMessage, &ts,
		&item.Type, &item.State,
		&item.Assignees, &item.Due, &item.StartDate, &item.EndDate,
		&item.MilestoneRepoURL, &item.MilestoneHash, &item.MilestoneBranch,
		&item.SprintRepoURL, &item.SprintHash, &item.SprintBranch,
		&item.ParentRepoURL, &item.ParentHash, &item.ParentBranch,
		&item.RootRepoURL, &item.RootHash, &item.RootBranch,
		&item.Labels,
		&item.EditOf, &isVirtual, &isRetracted, &hasEdits,
		&item.Comments,
	)
	if err != nil {
		return nil, err
	}
	if message.Valid {
		item.Content = protocol.ExtractCleanContent(message.String)
	}
	if originalMessage.Valid {
		if msg := protocol.ParseMessage(originalMessage.String); msg != nil {
			item.Origin = protocol.ExtractOrigin(&msg.Header)
		}
	}
	if ts.Valid {
		item.Timestamp, _ = time.Parse(time.RFC3339, ts.String)
	}
	item.IsVirtual = isVirtual == 1
	item.IsRetracted = isRetracted == 1
	item.IsEdited = hasEdits == 1
	return &item, nil
}

func scanResolvedRows(rows *sql.Rows) (*PMItem, error) {
	var item PMItem
	var ts, message, originalMessage sql.NullString
	var isVirtual, isRetracted, hasEdits int
	err := rows.Scan(
		&item.RepoURL, &item.Hash, &item.Branch,
		&item.AuthorName, &item.AuthorEmail, &message, &originalMessage, &ts,
		&item.Type, &item.State,
		&item.Assignees, &item.Due, &item.StartDate, &item.EndDate,
		&item.MilestoneRepoURL, &item.MilestoneHash, &item.MilestoneBranch,
		&item.SprintRepoURL, &item.SprintHash, &item.SprintBranch,
		&item.ParentRepoURL, &item.ParentHash, &item.ParentBranch,
		&item.RootRepoURL, &item.RootHash, &item.RootBranch,
		&item.Labels,
		&item.EditOf, &isVirtual, &isRetracted, &hasEdits,
		&item.Comments,
	)
	if err != nil {
		return nil, err
	}
	if message.Valid {
		item.Content = protocol.ExtractCleanContent(message.String)
	}
	if originalMessage.Valid {
		if msg := protocol.ParseMessage(originalMessage.String); msg != nil {
			item.Origin = protocol.ExtractOrigin(&msg.Header)
		}
	}
	if ts.Valid {
		item.Timestamp, _ = time.Parse(time.RFC3339, ts.String)
	}
	item.IsVirtual = isVirtual == 1
	item.IsRetracted = isRetracted == 1
	item.IsEdited = hasEdits == 1
	return &item, nil
}

// parseLabels parses comma-separated scoped labels into Label structs.
func parseLabels(labelsStr string) []Label {
	if labelsStr == "" {
		return nil
	}
	parts := strings.Split(labelsStr, ",")
	labels := make([]Label, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if idx := strings.Index(p, "/"); idx > 0 {
			labels = append(labels, Label{Scope: p[:idx], Value: p[idx+1:]})
		} else {
			labels = append(labels, Label{Value: p})
		}
	}
	return labels
}

// parseAssignees parses comma-separated emails into a string slice.
func parseAssignees(assigneesStr string) []string {
	if assigneesStr == "" {
		return nil
	}
	parts := strings.Split(assigneesStr, ",")
	assignees := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			assignees = append(assignees, p)
		}
	}
	return assignees
}

// PMItemToIssue converts a PMItem to an Issue.
func PMItemToIssue(item PMItem) Issue {
	var due *time.Time
	if item.Due.Valid {
		if t, err := time.Parse("2006-01-02", item.Due.String); err == nil {
			due = &t
		}
	}

	var milestone *IssueRef
	if item.MilestoneRepoURL.Valid && item.MilestoneHash.Valid {
		milestone = &IssueRef{
			RepoURL: item.MilestoneRepoURL.String,
			Hash:    item.MilestoneHash.String,
			Branch:  item.MilestoneBranch.String,
		}
	}

	var sprint *IssueRef
	if item.SprintRepoURL.Valid && item.SprintHash.Valid {
		sprint = &IssueRef{
			RepoURL: item.SprintRepoURL.String,
			Hash:    item.SprintHash.String,
			Branch:  item.SprintBranch.String,
		}
	}

	var parent *IssueRef
	if item.ParentRepoURL.Valid && item.ParentHash.Valid {
		parent = &IssueRef{
			RepoURL: item.ParentRepoURL.String,
			Hash:    item.ParentHash.String,
			Branch:  item.ParentBranch.String,
		}
	}

	var root *IssueRef
	if item.RootRepoURL.Valid && item.RootHash.Valid {
		root = &IssueRef{
			RepoURL: item.RootRepoURL.String,
			Hash:    item.RootHash.String,
			Branch:  item.RootBranch.String,
		}
	}

	// Load links from pm_links table
	var blocks, blockedBy, related []IssueRef
	if links, err := GetLinks(item.RepoURL, item.Hash, item.Branch); err == nil {
		for _, l := range links {
			switch l.Type {
			case LinkTypeBlocks:
				blocks = append(blocks, l.To)
			case LinkTypeBlockedBy:
				blockedBy = append(blockedBy, l.To)
			case LinkTypeRelated:
				related = append(related, l.To)
			}
		}
	}

	subject, body := protocol.SplitSubjectBody(item.Content)
	id := protocol.CreateRef(protocol.RefTypeCommit, item.Hash, item.RepoURL, item.Branch)

	return Issue{
		ID:         id,
		Repository: item.RepoURL,
		Branch:     item.Branch,
		Author: Author{
			Name:  item.AuthorName,
			Email: item.AuthorEmail,
		},
		Timestamp:   item.Timestamp,
		Subject:     subject,
		Body:        body,
		State:       State(item.State),
		Assignees:   parseAssignees(item.Assignees.String),
		Due:         due,
		Milestone:   milestone,
		Sprint:      sprint,
		Parent:      parent,
		Root:        root,
		Blocks:      blocks,
		BlockedBy:   blockedBy,
		Related:     related,
		Labels:      parseLabels(item.Labels.String),
		IsEdited:    item.IsEdited,
		IsRetracted: item.IsRetracted,
		Comments:    item.Comments,
		Origin:      item.Origin,
	}
}

// PMItemToMilestone converts a PMItem to a Milestone.
func PMItemToMilestone(item PMItem) Milestone {
	var due *time.Time
	if item.Due.Valid {
		if t, err := time.Parse("2006-01-02", item.Due.String); err == nil {
			due = &t
		}
	}

	title, body := protocol.SplitSubjectBody(item.Content)
	id := protocol.CreateRef(protocol.RefTypeCommit, item.Hash, item.RepoURL, item.Branch)

	return Milestone{
		ID:         id,
		Repository: item.RepoURL,
		Branch:     item.Branch,
		Author: Author{
			Name:  item.AuthorName,
			Email: item.AuthorEmail,
		},
		Timestamp:   item.Timestamp,
		Title:       title,
		Body:        body,
		State:       State(item.State),
		Due:         due,
		IsEdited:    item.IsEdited,
		IsRetracted: item.IsRetracted,
		Origin:      item.Origin,
	}
}

// PMItemToSprint converts a PMItem to a Sprint.
func PMItemToSprint(item PMItem) Sprint {
	var start, end time.Time
	if item.StartDate.Valid {
		start, _ = time.Parse("2006-01-02", item.StartDate.String)
	}
	if item.EndDate.Valid {
		end, _ = time.Parse("2006-01-02", item.EndDate.String)
	}

	title, body := protocol.SplitSubjectBody(item.Content)
	id := protocol.CreateRef(protocol.RefTypeCommit, item.Hash, item.RepoURL, item.Branch)

	return Sprint{
		ID:         id,
		Repository: item.RepoURL,
		Branch:     item.Branch,
		Author: Author{
			Name:  item.AuthorName,
			Email: item.AuthorEmail,
		},
		Timestamp:   item.Timestamp,
		Title:       title,
		Body:        body,
		State:       SprintState(item.State),
		Start:       start,
		End:         end,
		IsEdited:    item.IsEdited,
		IsRetracted: item.IsRetracted,
		Origin:      item.Origin,
	}
}

// buildOrderClause creates SQL ORDER BY clause from sort parameters.
func buildOrderClause(sortField, sortOrder string) string {
	col := "v.timestamp"
	switch sortField {
	case "created":
		col = "v.timestamp"
	case "updated":
		col = "v.timestamp" // TODO: track updated separately
	case "due":
		col = "v.due"
	case "start":
		col = "v.start_date"
	case "priority":
		col = "CASE " +
			"WHEN v.labels LIKE '%priority/critical%' THEN 1 " +
			"WHEN v.labels LIKE '%priority/high%' THEN 2 " +
			"WHEN v.labels LIKE '%priority/medium%' THEN 3 " +
			"WHEN v.labels LIKE '%priority/low%' THEN 4 " +
			"ELSE 5 END"
	}

	order := "DESC"
	if sortOrder == "asc" {
		order = "ASC"
	}

	// For date fields, put NULLs last
	if sortField == "due" || sortField == "start" {
		return "CASE WHEN " + col + " IS NULL THEN 1 ELSE 0 END, " + col + " " + order
	}

	return col + " " + order
}

// buildRetractContent creates a retraction commit message using protocol functions.
func buildRetractContent(editsRef string) string {
	header := protocol.Header{
		Ext: "pm",
		V:   "0.1.0",
		Fields: map[string]string{
			"edits":     editsRef,
			"retracted": "true",
		},
	}
	return protocol.FormatMessage("", header, nil)
}

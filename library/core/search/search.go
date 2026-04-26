// search.go - Cross-extension full-text search with filters and scoring
package search

import (
	"database/sql"
	"sort"
	"strings"
	"time"

	"github.com/gitsocial-org/gitsocial/core/cache"
	"github.com/gitsocial-org/gitsocial/core/git"
	"github.com/gitsocial-org/gitsocial/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/core/protocol"
)

// Search performs full-text search across all extensions with filters and scoring.
func Search(workdir string, params Params) (Result, error) {
	startTime := time.Now()

	parsed := parseSearchQuery(params.Query)
	if params.Limit == 0 {
		params.Limit = 20
	}

	// Merge: explicit params override parsed query filters
	if params.Author == "" {
		params.Author = parsed.Author
	}
	if params.Repo == "" {
		params.Repo = parsed.Repo
	}
	if params.Repo != "" {
		params.Repo = protocol.NormalizeURL(params.Repo)
	}
	if params.Type == "" {
		params.Type = parsed.Type
	}
	if params.Hash == "" {
		params.Hash = parsed.Hash
	}
	if params.After == nil {
		params.After = parsed.After
	}
	if params.Before == nil {
		params.Before = parsed.Before
	}

	gitRoot, err := git.GetRootDir(workdir)
	if err != nil {
		gitRoot = workdir
	}

	listIDs, _ := cache.GetListIDs(gitRoot)
	workspaceURL := gitmsg.ResolveRepoURL(gitRoot)

	q := searchQuery{
		WorkspaceURL: workspaceURL,
	}

	// Apply scope
	switch {
	case params.Scope == "" || params.Scope == "timeline":
		q.ListIDs = listIDs
	case strings.HasPrefix(params.Scope, "list:"):
		q.ListID = strings.TrimPrefix(params.Scope, "list:")
		q.WorkspaceURL = ""
	case strings.HasPrefix(params.Scope, "repository:"):
		repoURL := strings.TrimPrefix(params.Scope, "repository:")
		if repoURL == "my" || repoURL == "workspace" {
			q.RepoURL = workspaceURL
		} else {
			q.RepoURL = repoURL
		}
	case strings.HasPrefix(params.Scope, "repos:"):
		urls := strings.Split(strings.TrimPrefix(params.Scope, "repos:"), ",")
		q.RepoURLs = urls
		q.WorkspaceURL = ""
		q.ListIDs = nil
	}

	// Apply list filter from query text (overrides scope, except for repos: scope)
	if parsed.List != "" && len(q.RepoURLs) == 0 {
		q.ListID = resolveListID(parsed.List)
		q.ListIDs = nil
		q.WorkspaceURL = ""
	}

	// Apply repo filter (from param or parsed) — overrides timeline scope
	if params.Repo != "" {
		if params.Repo == "my" || params.Repo == "workspace" {
			q.RepoURL = workspaceURL
		} else {
			q.RepoURL = params.Repo
		}
		q.ListIDs = nil
		q.WorkspaceURL = ""
	}
	// Type inference: extension-specific filters imply their type
	if params.Type == "" {
		params.Type = inferTypeFromFilters(params)
	}
	if params.Type != "" {
		if ext := extFilterFromType(params.Type); ext != nil {
			q.ExtFilter = ext
		} else {
			q.Types = []string{params.Type}
		}
	}
	if params.After != nil {
		q.Since = params.After
	}
	if params.Before != nil {
		q.Until = params.Before
	}

	// Push text search into SQL for performance
	searchTerms := strings.ToLower(parsed.Terms)
	for _, term := range strings.Fields(searchTerms) {
		if !hashPattern.MatchString(term) {
			if q.TextSearch != "" {
				q.TextSearch += " "
			}
			q.TextSearch += term
		}
	}
	q.AuthorFilter = params.Author
	q.HashPrefix = strings.ToLower(params.Hash)

	// Pass extension-specific filters to query
	q.State = params.State
	q.Labels = params.Labels
	q.Assignee = params.Assignee
	q.Reviewer = params.Reviewer
	q.Draft = params.Draft
	q.Prerelease = params.Prerelease
	q.Tag = params.Tag
	q.Base = params.Base
	q.Milestone = params.Milestone
	q.Sprint = params.Sprint

	// Push LIMIT into SQL when possible. For group-by, we need all rows.
	// For text queries, FTS5 provides relevance so SQL LIMIT is safe.
	if params.GroupBy == "" && params.Limit > 0 {
		q.SQLLimit = params.Limit + params.Offset
	}

	// When SQL LIMIT is active, get the true total count for pagination
	var trueTotal int
	if q.SQLLimit > 0 {
		trueTotal, _ = queryCount(q)
	}

	items, err := queryItems(q)
	if err != nil {
		return Result{}, err
	}

	results := scoreAndFilter(items, params, parsed)

	if params.Sort == "date" {
		sort.Slice(results, func(i, j int) bool {
			return results[i].Timestamp.After(results[j].Timestamp)
		})
	} else {
		sort.Slice(results, func(i, j int) bool {
			if results[i].Score != results[j].Score {
				return results[i].Score > results[j].Score
			}
			return results[i].Timestamp.After(results[j].Timestamp)
		})
	}

	// Grouped output path
	if params.GroupBy != "" {
		enrichForGrouping(results, params.GroupBy)
		groups := groupBy(results, params.GroupBy, params.Top, params.CountOnly)
		return Result{
			Query:           params.Query,
			GroupBy:         params.GroupBy,
			Groups:          groups,
			Total:           len(results),
			TotalSearched:   len(items),
			ExecutionTimeMs: time.Since(startTime).Milliseconds(),
		}, nil
	}

	total := len(results)
	if trueTotal > total {
		total = trueTotal
	}
	if params.Offset > 0 {
		if params.Offset >= len(results) {
			results = nil
		} else {
			results = results[params.Offset:]
		}
	}
	hasMore := len(results) > params.Limit || (trueTotal > 0 && params.Offset+params.Limit < trueTotal)
	if len(results) > params.Limit {
		results = results[:params.Limit]
	}

	return Result{
		Query:           params.Query,
		Results:         results,
		Total:           total,
		TotalSearched:   total,
		HasMore:         hasMore,
		ExecutionTimeMs: time.Since(startTime).Milliseconds(),
	}, nil
}

// inferTypeFromFilters returns an implied type when extension-specific filters are used without --type.
func inferTypeFromFilters(params Params) string {
	switch {
	case params.Assignee != "" || params.Milestone != "" || params.Sprint != "":
		return "issue"
	case params.Reviewer != "" || params.Draft || params.Base != "":
		return "pr"
	case params.Prerelease || params.Tag != "":
		return "release"
	}
	return ""
}

// resolveListID resolves a list filter value to an ID.
// Accepts either an ID directly or a case-insensitive name match.
// Searches all lists (not just current workdir) since lists are global in the cache.
func resolveListID(value string) string {
	lists, err := cache.GetLists("")
	if err != nil {
		return value
	}
	for _, l := range lists {
		if l.ID == value {
			return value
		}
	}
	lower := strings.ToLower(value)
	for _, l := range lists {
		if strings.ToLower(l.Name) == lower {
			return l.ID
		}
	}
	return value
}

// queryCount returns the total count of items matching the search filters (no LIMIT).
func queryCount(q searchQuery) (int, error) {
	return cache.QueryLocked(func(db *sql.DB) (int, error) {
		whereClause, args := buildWhere(q, db)
		query := "SELECT COUNT(*) FROM core_commits r" + whereClause
		var count int
		err := db.QueryRow(query, args...).Scan(&count)
		return count, err
	})
}

// queryItems fetches items from the database matching scope and filters.
func queryItems(q searchQuery) ([]Item, error) {
	return cache.QueryLocked(func(db *sql.DB) ([]Item, error) {
		tables := availableTables(db)
		hasInteractions := tableExists(db, "social_interactions")
		selectClause := buildSelect(tables, hasInteractions)
		whereClause, args := buildWhere(q, db)
		query := selectClause + whereClause + " ORDER BY r.effective_timestamp DESC"
		if q.SQLLimit > 0 {
			query += " LIMIT ?"
			args = append(args, q.SQLLimit)
		}

		rows, err := db.Query(query, args...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()

		var items []Item
		for rows.Next() {
			item, err := scanItem(rows)
			if err != nil {
				return nil, err
			}
			items = append(items, item)
		}
		return items, rows.Err()
	})
}

// scanItem scans a row from the search query into an Item.
func scanItem(rows *sql.Rows) (Item, error) {
	var item Item
	var ts, staleSince, message sql.NullString
	var isVirtual int
	var socialType, extension, itemType string
	var state, labels, assignees, due sql.NullString
	var base, head, reviewers sql.NullString
	var tag, version sql.NullString
	var draft, prerelease, comments int

	err := rows.Scan(
		&item.RepoURL, &item.Hash, &item.Branch,
		&item.AuthorName, &item.AuthorEmail, &message, &ts,
		&isVirtual, &staleSince,
		&socialType, &extension, &itemType,
		&state, &labels, &assignees, &due,
		&draft, &base, &head, &reviewers,
		&tag, &version, &prerelease, &comments,
	)
	if err != nil {
		return Item{}, err
	}

	if message.Valid {
		item.Content = protocol.ExtractCleanContent(message.String)
	}
	if ts.Valid {
		item.Timestamp, _ = time.Parse(time.RFC3339, ts.String)
	}

	item.IsVirtual = isVirtual == 1
	item.IsStale = staleSince.Valid
	item.Extension = extension

	// Determine the item type: prefer social type for social items, otherwise use extension type
	if extension == "social" && socialType != "unknown" {
		item.Type = socialType
	} else {
		item.Type = itemType
	}

	// Extension-specific fields
	item.State = state.String
	item.Labels = labels.String
	item.Assignees = assignees.String
	item.Due = due.String
	item.Draft = draft == 1
	item.Base = base.String
	item.Head = head.String
	item.Reviewers = reviewers.String
	item.Tag = tag.String
	item.Version = version.String
	item.Prerelease = prerelease == 1
	item.Comments = comments

	return item, nil
}

// scoreAndFilter applies client-side text matching, author/hash filtering, and scoring.
func scoreAndFilter(items []Item, params Params, parsed parsedQuery) []ScoredItem {
	searchTerms := strings.ToLower(parsed.Terms)

	// Auto-detect hash-like terms (7+ hex chars)
	var hashTerms []string
	var textTerms []string
	for _, term := range strings.Fields(searchTerms) {
		if hashPattern.MatchString(term) {
			hashTerms = append(hashTerms, term)
		} else {
			textTerms = append(textTerms, term)
		}
	}
	textSearch := strings.Join(textTerms, " ")

	var results []ScoredItem
	for _, item := range items {
		if params.Author != "" {
			authorLower := strings.ToLower(params.Author)
			nameMatch := strings.Contains(strings.ToLower(item.AuthorName), authorLower)
			emailMatch := strings.Contains(strings.ToLower(item.AuthorEmail), authorLower)
			if !nameMatch && !emailMatch {
				continue
			}
		}
		if params.Hash != "" && !strings.HasPrefix(strings.ToLower(item.Hash), strings.ToLower(params.Hash)) {
			continue
		}

		// Filter by auto-detected hash terms
		hashMatch := len(hashTerms) == 0
		for _, ht := range hashTerms {
			if strings.HasPrefix(strings.ToLower(item.Hash), ht) {
				hashMatch = true
				break
			}
		}
		if !hashMatch {
			continue
		}

		score := 0.0
		if textSearch != "" {
			contentLower := strings.ToLower(item.Content)
			authorLower := strings.ToLower(item.AuthorName + " " + item.AuthorEmail)

			if strings.Contains(contentLower, textSearch) {
				score += 10.0
			}
			if strings.Contains(authorLower, textSearch) {
				score += 5.0
			}

			if score == 0 {
				score = 1.0 // SQL already filtered via FTS/LIKE; keep for ranking
			}
		} else if len(hashTerms) > 0 {
			score = 20.0
		} else {
			score = 1.0
		}

		results = append(results, ScoredItem{
			Item:  item,
			Score: score,
		})
	}

	return results
}

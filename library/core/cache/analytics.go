// analytics.go - Analytics queries for commit activity, repositories, and contributors
package cache

import (
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// logScanErr logs a non-nil error from row.Scan or rows.Err at debug level.
func logScanErr(err error) {
	if err != nil {
		slog.Debug("analytics query", "error", err)
	}
}

// resolvedFilter excludes edit commits, retracted commits, virtual commits, and stale commits.
const resolvedFilter = "NOT v.is_edit_commit AND NOT v.is_retracted AND v.is_virtual = 0 AND v.stale_since IS NULL"

type DayStat struct {
	Date  string
	Count int
}

type RepoStat struct {
	URL       string
	Count     int
	PrevCount int
	LastSeen  time.Time
}

type ContribStat struct {
	Name  string
	Email string
	Count int
}

type SocialAnalytics struct {
	PostsPerDay    []DayStat
	CommentsPerDay []DayStat
	RepostsPerDay  []DayStat
	TotalPosts     int
	TotalComments  int
	TotalReposts   int
	PrevPosts      int
	PrevComments   int
	PrevReposts    int
}

type PMAnalytics struct {
	OpenIssues   int
	ClosedIssues int
	OpenedPerDay []DayStat
	ClosedPerDay []DayStat
	AvgAgeDays   int
	Milestones   []MilestoneStat
}

type MilestoneStat struct {
	Name   string
	Total  int
	Closed int
	Due    string
}

type ReleaseAnalytics struct {
	Recent     []ReleaseStat
	FreqPerDay []DayStat
	Total      int
	PerMonth   float64
}

type ReleaseStat struct {
	Version   string
	Timestamp time.Time
}

type ReviewAnalytics struct {
	OpenPRs       int
	MergedPRs     int
	ClosedPRs     int
	PRsPerDay     []DayStat
	TotalFeedback int
}

type AnalyticsData struct {
	CommitsPerDay     []DayStat
	RepoActivity      []RepoStat
	Contributors      []ContribStat
	DayOfWeek         [7]int
	TotalCommits      int
	PrevTotalCommits  int
	TrackedRepos      int
	ActiveRepos       int
	TotalContributors int
	Social            *SocialAnalytics
	PM                *PMAnalytics
	Release           *ReleaseAnalytics
	Review            *ReviewAnalytics
	RepoURL           string // non-empty when scoped to a single repo
}

// repoFilter builds a SQL clause and args for optional repo_url filtering.
// The table alias is prepended to "repo_url" (e.g. "v" → "v.repo_url = ?").
func repoFilter(alias, repoURL string) (string, []any) {
	if repoURL == "" {
		return "", nil
	}
	col := "repo_url"
	if alias != "" {
		col = alias + ".repo_url"
	}
	return " AND " + col + " = ?", []any{repoURL}
}

// GetAnalytics collects analytics data from core_commits and extension tables.
// When repoURL is non-empty, results are scoped to that single repository.
func GetAnalytics(repoURL string) (*AnalyticsData, error) {
	mu.RLock()
	defer mu.RUnlock()

	if db == nil {
		return &AnalyticsData{}, nil
	}

	now := time.Now().UTC()
	thirtyDaysAgo := now.AddDate(0, 0, -30).Format(time.RFC3339)
	sixtyDaysAgo := now.AddDate(0, 0, -60).Format(time.RFC3339)

	rfv, rfvArgs := repoFilter("v", repoURL)

	data := &AnalyticsData{RepoURL: repoURL}

	// Commits per day (30 days)
	q := `SELECT DATE(v.effective_timestamp) d, COUNT(*) c FROM core_commits v
		WHERE ` + resolvedFilter + ` AND v.effective_timestamp > ?` + rfv + ` GROUP BY d ORDER BY d`
	args := append([]any{thirtyDaysAgo}, rfvArgs...)
	rows, err := db.Query(q, args...)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var ds DayStat
			if rows.Scan(&ds.Date, &ds.Count) == nil {
				data.CommitsPerDay = append(data.CommitsPerDay, ds)
				data.TotalCommits += ds.Count
			}
		}
		logScanErr(rows.Err())
	}

	// Previous 30 days total (for trend)
	q = `SELECT COUNT(*) FROM core_commits v
		WHERE ` + resolvedFilter + ` AND v.effective_timestamp BETWEEN ? AND ?` + rfv
	args = append([]any{sixtyDaysAgo, thirtyDaysAgo}, rfvArgs...)
	row := db.QueryRow(q, args...)
	logScanErr(row.Scan(&data.PrevTotalCommits))

	// Top repos by commit count (30d) — skip when scoped to single repo
	if repoURL == "" {
		rows, err = db.Query(`SELECT v.repo_url, COUNT(*) cnt,
			MAX(v.effective_timestamp) last_ts
			FROM core_commits v
			WHERE `+resolvedFilter+` AND v.effective_timestamp > ?
			GROUP BY v.repo_url ORDER BY cnt DESC LIMIT 8`, thirtyDaysAgo)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var rs RepoStat
				var lastTS string
				if rows.Scan(&rs.URL, &rs.Count, &lastTS) == nil {
					if t, err := time.Parse(time.RFC3339, lastTS); err == nil {
						rs.LastSeen = t
					}
					data.RepoActivity = append(data.RepoActivity, rs)
				}
			}
			logScanErr(rows.Err())
		}
		for i := range data.RepoActivity {
			row := db.QueryRow(`SELECT COUNT(*) FROM core_commits v
				WHERE `+resolvedFilter+` AND v.repo_url = ? AND v.effective_timestamp BETWEEN ? AND ?`,
				data.RepoActivity[i].URL, sixtyDaysAgo, thirtyDaysAgo)
			logScanErr(row.Scan(&data.RepoActivity[i].PrevCount))
		}
	}

	// Contributors (30d) — author_name/email are COALESCE'd with origin in the view
	q = `SELECT v.effective_author_name, v.effective_author_email, COUNT(*) c FROM core_commits v
		WHERE ` + resolvedFilter + ` AND v.effective_timestamp > ?` + rfv + `
		GROUP BY v.effective_author_email ORDER BY c DESC LIMIT 8`
	args = append([]any{thirtyDaysAgo}, rfvArgs...)
	rows, err = db.Query(q, args...)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var cs ContribStat
			if rows.Scan(&cs.Name, &cs.Email, &cs.Count) == nil {
				data.Contributors = append(data.Contributors, cs)
			}
		}
		logScanErr(rows.Err())
	}

	// Total unique contributors
	q = `SELECT COUNT(DISTINCT v.effective_author_email) FROM core_commits v
		WHERE ` + resolvedFilter + ` AND v.effective_timestamp > ?` + rfv
	args = append([]any{thirtyDaysAgo}, rfvArgs...)
	row = db.QueryRow(q, args...)
	logScanErr(row.Scan(&data.TotalContributors))

	// Day of week distribution
	q = `SELECT CAST(strftime('%w', v.effective_timestamp) AS INTEGER) dow, COUNT(*) c
		FROM core_commits v WHERE ` + resolvedFilter + ` AND v.effective_timestamp > ?` + rfv + ` GROUP BY dow`
	args = append([]any{thirtyDaysAgo}, rfvArgs...)
	rows, err = db.Query(q, args...)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var dow, count int
			if rows.Scan(&dow, &count) == nil {
				idx := (dow + 6) % 7
				data.DayOfWeek[idx] = count
			}
		}
		logScanErr(rows.Err())
	}

	// Tracked repos
	row = db.QueryRow(`SELECT COUNT(*) FROM core_repositories`)
	logScanErr(row.Scan(&data.TrackedRepos))

	// Active repos this month
	q = `SELECT COUNT(DISTINCT v.repo_url) FROM core_commits v
		WHERE ` + resolvedFilter + ` AND v.effective_timestamp > ?` + rfv
	args = append([]any{thirtyDaysAgo}, rfvArgs...)
	row = db.QueryRow(q, args...)
	logScanErr(row.Scan(&data.ActiveRepos))

	// Extension: Social
	if repoHasExtRows("social_items", repoURL) {
		data.Social = querySocialAnalytics(thirtyDaysAgo, sixtyDaysAgo, repoURL)
	}

	// Extension: PM
	if repoHasExtRows("pm_items", repoURL) {
		data.PM = queryPMAnalytics(thirtyDaysAgo, repoURL)
	}

	// Extension: Release
	if repoHasExtRows("release_items", repoURL) {
		data.Release = queryReleaseAnalytics(thirtyDaysAgo, repoURL)
	}

	// Extension: Review
	if repoHasExtRows("review_items", repoURL) {
		data.Review = queryReviewAnalytics(thirtyDaysAgo, repoURL)
	}

	return data, nil
}

// NetworkAnalytics holds cross-repo analytics for the Network section.
type NetworkAnalytics struct {
	Repos        []RepoStat
	TrackedRepos int
	ActiveRepos  int
	Social       *NetworkSocial
	PM           *NetworkPM
	Release      *NetworkRelease
	Review       *NetworkReview
}

// NetworkSocial holds cross-repo social activity rankings.
type NetworkSocial struct {
	RepoActivity  []NetworkSocialRepo
	TotalPosts    int
	TotalComments int
	TotalReposts  int
}

// NetworkSocialRepo holds social stats for a single repo.
type NetworkSocialRepo struct {
	URL       string
	Posts     int
	PrevPosts int
}

// NetworkPM holds cross-repo issue rankings.
type NetworkPM struct {
	RepoActivity []NetworkPMRepo
	TotalOpen    int
	TotalClosed  int
}

// NetworkPMRepo holds issue counts for a single repo.
type NetworkPMRepo struct {
	URL    string
	Open   int
	Closed int
}

// NetworkRelease holds cross-repo release activity.
type NetworkRelease struct {
	Recent     []NetworkReleaseItem
	TotalMonth int
	RepoCount  int
}

// NetworkReleaseItem holds a single release entry.
type NetworkReleaseItem struct {
	URL       string
	Version   string
	Timestamp time.Time
}

// NetworkReview holds cross-repo review activity.
type NetworkReview struct {
	RepoActivity []NetworkReviewRepo
	TotalOpen    int
	TotalMerged  int
}

// NetworkReviewRepo holds PR counts for a single repo.
type NetworkReviewRepo struct {
	URL    string
	Open   int
	Merged int
}

// getListRepoURLs returns all repo URLs in the workspace's lists.
func getListRepoURLs(workdir string) []string {
	if db == nil {
		return nil
	}
	rows, err := db.Query(`SELECT DISTINCT lr.repo_url
		FROM core_list_repositories lr
		JOIN core_lists l ON lr.list_id = l.id
		WHERE l.workdir = ?`, workdir)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var urls []string
	for rows.Next() {
		var url string
		if rows.Scan(&url) == nil {
			urls = append(urls, url)
		}
	}
	return urls
}

// listFilter builds a SQL IN clause and args for filtering by list repo URLs.
// alias is prepended to "repo_url" (e.g. "v" → "v.repo_url IN (...)").
func listFilter(alias string, urls []string) (string, []any) {
	if len(urls) == 0 {
		return "", nil
	}
	col := "repo_url"
	if alias != "" {
		col = alias + ".repo_url"
	}
	placeholders := strings.Repeat("?,", len(urls))
	placeholders = placeholders[:len(placeholders)-1]
	args := make([]any, len(urls))
	for i, u := range urls {
		args[i] = u
	}
	return " AND " + col + " IN (" + placeholders + ")", args
}

// GetNetworkAnalytics returns cross-repo analytics scoped to repos in the workspace's lists.
func GetNetworkAnalytics(workdir string) *NetworkAnalytics {
	mu.RLock()
	defer mu.RUnlock()

	na := &NetworkAnalytics{}
	if db == nil {
		return na
	}

	listRepos := getListRepoURLs(workdir)
	if len(listRepos) == 0 {
		return na
	}

	now := time.Now().UTC()
	thirtyDaysAgo := now.AddDate(0, 0, -30).Format(time.RFC3339)
	sixtyDaysAgo := now.AddDate(0, 0, -60).Format(time.RFC3339)

	inClause, inArgs := listFilter("v", listRepos)

	// Top repos by commit count (30d)
	q := `SELECT v.repo_url, COUNT(*) cnt, MAX(v.effective_timestamp) last_ts
		FROM core_commits v
		WHERE ` + resolvedFilter + ` AND v.effective_timestamp > ?` + inClause + `
		GROUP BY v.repo_url ORDER BY cnt DESC LIMIT 8`
	rows, err := db.Query(q, append([]any{thirtyDaysAgo}, inArgs...)...)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var rs RepoStat
			var lastTS string
			if rows.Scan(&rs.URL, &rs.Count, &lastTS) == nil {
				if t, err := time.Parse(time.RFC3339, lastTS); err == nil {
					rs.LastSeen = t
				}
				na.Repos = append(na.Repos, rs)
			}
		}
		logScanErr(rows.Err())
	}
	for i := range na.Repos {
		row := db.QueryRow(`SELECT COUNT(*) FROM core_commits v
			WHERE `+resolvedFilter+` AND v.repo_url = ? AND v.effective_timestamp BETWEEN ? AND ?`,
			na.Repos[i].URL, sixtyDaysAgo, thirtyDaysAgo)
		logScanErr(row.Scan(&na.Repos[i].PrevCount))
	}

	na.TrackedRepos = len(listRepos)
	q = `SELECT COUNT(DISTINCT v.repo_url) FROM core_commits v
		WHERE ` + resolvedFilter + ` AND v.effective_timestamp > ?` + inClause
	row := db.QueryRow(q, append([]any{thirtyDaysAgo}, inArgs...)...)
	logScanErr(row.Scan(&na.ActiveRepos))

	// Social: repos by post count
	if repoHasExtRows("social_items", "") {
		na.Social = queryNetworkSocial(thirtyDaysAgo, sixtyDaysAgo, listRepos)
	}

	// PM: repos by open issue count
	if repoHasExtRows("pm_items", "") {
		na.PM = queryNetworkPM(listRepos)
	}

	// Release: recent releases across network
	if repoHasExtRows("release_items", "") {
		na.Release = queryNetworkRelease(thirtyDaysAgo, listRepos)
	}

	// Review: PRs across network
	if repoHasExtRows("review_items", "") {
		na.Review = queryNetworkReview(listRepos)
	}

	return na
}

// queryNetworkSocial gathers cross-repo social rankings scoped to list repos.
func queryNetworkSocial(thirtyDaysAgo, sixtyDaysAgo string, listRepos []string) *NetworkSocial {
	ns := &NetworkSocial{}
	inClause, inArgs := listFilter("v", listRepos)

	// Top repos by post count (30d)
	q := `SELECT v.repo_url, COUNT(*) cnt
		FROM social_items s JOIN core_commits v ON s.repo_url=v.repo_url AND s.hash=v.hash AND s.branch=v.branch
		WHERE s.type='post' AND ` + resolvedFilter + ` AND v.effective_timestamp > ?` + inClause + `
		GROUP BY v.repo_url ORDER BY cnt DESC LIMIT 8`
	rows, err := db.Query(q, append([]any{thirtyDaysAgo}, inArgs...)...)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var r NetworkSocialRepo
			if rows.Scan(&r.URL, &r.Posts) == nil {
				ns.RepoActivity = append(ns.RepoActivity, r)
			}
		}
		logScanErr(rows.Err())
	}
	// Previous period for trends
	for i := range ns.RepoActivity {
		row := db.QueryRow(`SELECT COUNT(*) FROM social_items s
			JOIN core_commits v ON s.repo_url=v.repo_url AND s.hash=v.hash AND s.branch=v.branch
			WHERE s.type='post' AND `+resolvedFilter+` AND v.repo_url=? AND v.effective_timestamp BETWEEN ? AND ?`,
			ns.RepoActivity[i].URL, sixtyDaysAgo, thirtyDaysAgo)
		logScanErr(row.Scan(&ns.RepoActivity[i].PrevPosts))
	}

	// Totals (30d)
	for _, typ := range []struct {
		name  string
		total *int
	}{
		{"post", &ns.TotalPosts},
		{"comment", &ns.TotalComments},
		{"repost", &ns.TotalReposts},
	} {
		q = `SELECT COUNT(*) FROM social_items s
			JOIN core_commits v ON s.repo_url=v.repo_url AND s.hash=v.hash AND s.branch=v.branch
			WHERE s.type=? AND ` + resolvedFilter + ` AND v.effective_timestamp > ?` + inClause
		row := db.QueryRow(q, append([]any{typ.name, thirtyDaysAgo}, inArgs...)...)
		logScanErr(row.Scan(typ.total))
	}

	return ns
}

// queryNetworkPM gathers cross-repo issue rankings scoped to list repos.
func queryNetworkPM(listRepos []string) *NetworkPM {
	np := &NetworkPM{}
	inClause, inArgs := listFilter("v", listRepos)

	q := `SELECT v.repo_url,
		SUM(CASE WHEN p.state='open' THEN 1 ELSE 0 END) open_cnt,
		SUM(CASE WHEN p.state='closed' THEN 1 ELSE 0 END) closed_cnt
		FROM pm_items p JOIN core_commits v ON p.repo_url=v.repo_url AND p.hash=v.hash AND p.branch=v.branch
		WHERE p.type='issue' AND ` + resolvedFilter + inClause + `
		GROUP BY v.repo_url ORDER BY open_cnt DESC LIMIT 8`
	rows, err := db.Query(q, inArgs...)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var r NetworkPMRepo
			if rows.Scan(&r.URL, &r.Open, &r.Closed) == nil {
				np.TotalOpen += r.Open
				np.TotalClosed += r.Closed
				np.RepoActivity = append(np.RepoActivity, r)
			}
		}
		logScanErr(rows.Err())
	}

	return np
}

// queryNetworkRelease gathers cross-repo release activity scoped to list repos.
func queryNetworkRelease(thirtyDaysAgo string, listRepos []string) *NetworkRelease {
	nr := &NetworkRelease{}
	inClause, inArgs := listFilter("v", listRepos)

	// 5 most recent releases
	q := `SELECT v.repo_url, COALESCE(r.version, r.tag, 'unknown'), v.effective_timestamp
		FROM release_items r JOIN core_commits v ON r.repo_url=v.repo_url AND r.hash=v.hash AND r.branch=v.branch
		WHERE ` + resolvedFilter + inClause + `
		ORDER BY v.effective_timestamp DESC LIMIT 5`
	rows, err := db.Query(q, inArgs...)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var ri NetworkReleaseItem
			var ts string
			if rows.Scan(&ri.URL, &ri.Version, &ts) == nil {
				if t, err := time.Parse(time.RFC3339, ts); err == nil {
					ri.Timestamp = t
				}
				nr.Recent = append(nr.Recent, ri)
			}
		}
		logScanErr(rows.Err())
	}

	// This month: total releases and distinct repos
	q = `SELECT COUNT(*), COUNT(DISTINCT v.repo_url)
		FROM release_items r JOIN core_commits v ON r.repo_url=v.repo_url AND r.hash=v.hash AND r.branch=v.branch
		WHERE ` + resolvedFilter + ` AND v.effective_timestamp > ?` + inClause
	row := db.QueryRow(q, append([]any{thirtyDaysAgo}, inArgs...)...)
	logScanErr(row.Scan(&nr.TotalMonth, &nr.RepoCount))

	return nr
}

// queryNetworkReview gathers cross-repo PR rankings scoped to list repos.
func queryNetworkReview(listRepos []string) *NetworkReview {
	nrv := &NetworkReview{}
	inClause, inArgs := listFilter("v", listRepos)

	q := `SELECT v.repo_url,
		SUM(CASE WHEN rv.state='open' THEN 1 ELSE 0 END) open_cnt,
		SUM(CASE WHEN rv.state='merged' THEN 1 ELSE 0 END) merged_cnt
		FROM review_items rv JOIN core_commits v ON rv.repo_url=v.repo_url AND rv.hash=v.hash AND rv.branch=v.branch
		WHERE rv.type='pull-request' AND ` + resolvedFilter + inClause + `
		GROUP BY v.repo_url ORDER BY open_cnt DESC LIMIT 8`
	rows, err := db.Query(q, inArgs...)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var r NetworkReviewRepo
			if rows.Scan(&r.URL, &r.Open, &r.Merged) == nil {
				nrv.TotalOpen += r.Open
				nrv.TotalMerged += r.Merged
				nrv.RepoActivity = append(nrv.RepoActivity, r)
			}
		}
		logScanErr(rows.Err())
	}

	return nrv
}

// repoHasExtRows checks if an extension table has rows, optionally filtered by repo.
func repoHasExtRows(table, repoURL string) bool {
	if db == nil {
		return false
	}
	if repoURL == "" {
		var count int
		row := db.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %s", table))
		logScanErr(row.Scan(&count))
		return count > 0
	}
	var count int
	row := db.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE repo_url = ?", table), repoURL)
	logScanErr(row.Scan(&count))
	return count > 0
}

// querySocialAnalytics gathers social extension metrics. Must be called with mu held.
func querySocialAnalytics(thirtyDaysAgo, sixtyDaysAgo, repoURL string) *SocialAnalytics {
	sa := &SocialAnalytics{}
	rfv, rfvArgs := repoFilter("v", repoURL)
	for _, item := range []struct {
		typ    string
		perDay *[]DayStat
		total  *int
		prev   *int
	}{
		{"post", &sa.PostsPerDay, &sa.TotalPosts, &sa.PrevPosts},
		{"comment", &sa.CommentsPerDay, &sa.TotalComments, &sa.PrevComments},
		{"repost", &sa.RepostsPerDay, &sa.TotalReposts, &sa.PrevReposts},
	} {
		q := `SELECT DATE(v.effective_timestamp) d, COUNT(*) cnt
			FROM social_items s JOIN core_commits v ON s.repo_url=v.repo_url AND s.hash=v.hash AND s.branch=v.branch
			WHERE s.type=? AND ` + resolvedFilter + ` AND v.effective_timestamp > ?` + rfv + `
			GROUP BY d ORDER BY d`
		args := append([]any{item.typ, thirtyDaysAgo}, rfvArgs...)
		rows, err := db.Query(q, args...)
		if err == nil {
			for rows.Next() {
				var ds DayStat
				if rows.Scan(&ds.Date, &ds.Count) == nil {
					*item.perDay = append(*item.perDay, ds)
					*item.total += ds.Count
				}
			}
			rows.Close()
		}
		q = `SELECT COUNT(*) FROM social_items s
			JOIN core_commits v ON s.repo_url=v.repo_url AND s.hash=v.hash AND s.branch=v.branch
			WHERE s.type=? AND ` + resolvedFilter + ` AND v.effective_timestamp BETWEEN ? AND ?` + rfv
		args = append([]any{item.typ, sixtyDaysAgo, thirtyDaysAgo}, rfvArgs...)
		row := db.QueryRow(q, args...)
		logScanErr(row.Scan(item.prev))
	}
	return sa
}

// queryPMAnalytics gathers PM extension metrics. Must be called with mu held.
func queryPMAnalytics(thirtyDaysAgo, repoURL string) *PMAnalytics {
	pa := &PMAnalytics{}
	rfv, rfvArgs := repoFilter("v", repoURL)

	// Open/closed counts — join with resolved view for correctness
	q := `SELECT COUNT(*) FROM pm_items p
		JOIN core_commits v ON p.repo_url=v.repo_url AND p.hash=v.hash AND p.branch=v.branch
		WHERE p.type='issue' AND p.state='open' AND ` + resolvedFilter + rfv
	row := db.QueryRow(q, rfvArgs...)
	logScanErr(row.Scan(&pa.OpenIssues))

	q = `SELECT COUNT(*) FROM pm_items p
		JOIN core_commits v ON p.repo_url=v.repo_url AND p.hash=v.hash AND p.branch=v.branch
		WHERE p.type='issue' AND p.state='closed' AND ` + resolvedFilter + rfv
	row = db.QueryRow(q, rfvArgs...)
	logScanErr(row.Scan(&pa.ClosedIssues))

	// Opened per day
	q = `SELECT DATE(v.effective_timestamp) d, COUNT(*) cnt
		FROM pm_items p JOIN core_commits v ON p.repo_url=v.repo_url AND p.hash=v.hash AND p.branch=v.branch
		WHERE p.type='issue' AND ` + resolvedFilter + ` AND v.effective_timestamp > ?` + rfv + `
		GROUP BY d ORDER BY d`
	args := append([]any{thirtyDaysAgo}, rfvArgs...)
	rows, err := db.Query(q, args...)
	if err == nil {
		for rows.Next() {
			var ds DayStat
			if rows.Scan(&ds.Date, &ds.Count) == nil {
				pa.OpenedPerDay = append(pa.OpenedPerDay, ds)
			}
		}
		rows.Close()
	}

	// Closed per day
	q = `SELECT DATE(v.effective_timestamp) d, COUNT(*) cnt
		FROM pm_items p JOIN core_commits v ON p.repo_url=v.repo_url AND p.hash=v.hash AND p.branch=v.branch
		WHERE p.type='issue' AND p.state='closed' AND ` + resolvedFilter + ` AND v.effective_timestamp > ?` + rfv + `
		GROUP BY d ORDER BY d`
	args = append([]any{thirtyDaysAgo}, rfvArgs...)
	rows, err = db.Query(q, args...)
	if err == nil {
		for rows.Next() {
			var ds DayStat
			if rows.Scan(&ds.Date, &ds.Count) == nil {
				pa.ClosedPerDay = append(pa.ClosedPerDay, ds)
			}
		}
		rows.Close()
	}

	// Average age of open issues
	q = `SELECT CAST(AVG(julianday('now') - julianday(v.effective_timestamp)) AS INTEGER)
		FROM pm_items p JOIN core_commits v ON p.repo_url=v.repo_url AND p.hash=v.hash AND p.branch=v.branch
		WHERE p.type='issue' AND p.state='open' AND ` + resolvedFilter + rfv
	row = db.QueryRow(q, rfvArgs...)
	logScanErr(row.Scan(&pa.AvgAgeDays))

	// Milestones
	q = `SELECT v.effective_message, p.due,
		(SELECT COUNT(*) FROM pm_items i WHERE i.type='issue'
			AND i.milestone_repo_url=p.repo_url AND i.milestone_hash=p.hash AND i.milestone_branch=p.branch) total,
		(SELECT COUNT(*) FROM pm_items i WHERE i.type='issue' AND i.state='closed'
			AND i.milestone_repo_url=p.repo_url AND i.milestone_hash=p.hash AND i.milestone_branch=p.branch) closed
		FROM pm_items p JOIN core_commits v ON p.repo_url=v.repo_url AND p.hash=v.hash AND p.branch=v.branch
		WHERE p.type='milestone' AND p.state='open' AND ` + resolvedFilter + rfv + `
		ORDER BY p.due ASC LIMIT 5`
	rows, err = db.Query(q, rfvArgs...)
	if err == nil {
		for rows.Next() {
			var ms MilestoneStat
			var nameRaw, dueRaw *string
			if rows.Scan(&nameRaw, &dueRaw, &ms.Total, &ms.Closed) == nil {
				if nameRaw != nil {
					ms.Name = *nameRaw
				}
				if dueRaw != nil {
					ms.Due = *dueRaw
				}
				pa.Milestones = append(pa.Milestones, ms)
			}
		}
		rows.Close()
	}

	return pa
}

// queryReleaseAnalytics gathers release extension metrics. Must be called with mu held.
func queryReleaseAnalytics(thirtyDaysAgo, repoURL string) *ReleaseAnalytics {
	ra := &ReleaseAnalytics{}
	rfv, rfvArgs := repoFilter("v", repoURL)

	q := `SELECT COALESCE(r.version, r.tag, 'unknown'), v.effective_timestamp
		FROM release_items r JOIN core_commits v ON r.repo_url=v.repo_url AND r.hash=v.hash AND r.branch=v.branch
		WHERE ` + resolvedFilter + rfv + `
		ORDER BY v.effective_timestamp DESC LIMIT 3`
	rows, err := db.Query(q, rfvArgs...)
	if err == nil {
		for rows.Next() {
			var rs ReleaseStat
			var ts string
			if rows.Scan(&rs.Version, &ts) == nil {
				if t, err := time.Parse(time.RFC3339, ts); err == nil {
					rs.Timestamp = t
				}
				ra.Recent = append(ra.Recent, rs)
			}
		}
		rows.Close()
	}

	q = `SELECT DATE(v.effective_timestamp) d, COUNT(*) cnt
		FROM release_items r JOIN core_commits v ON r.repo_url=v.repo_url AND r.hash=v.hash AND r.branch=v.branch
		WHERE ` + resolvedFilter + ` AND v.effective_timestamp > ?` + rfv + `
		GROUP BY d ORDER BY d`
	args := append([]any{thirtyDaysAgo}, rfvArgs...)
	rows, err = db.Query(q, args...)
	if err == nil {
		for rows.Next() {
			var ds DayStat
			if rows.Scan(&ds.Date, &ds.Count) == nil {
				ra.FreqPerDay = append(ra.FreqPerDay, ds)
				ra.Total++
			}
		}
		rows.Close()
	}

	var total int
	var firstTS, lastTS *string
	q = `SELECT COUNT(*), MIN(v.effective_timestamp), MAX(v.effective_timestamp)
		FROM release_items r JOIN core_commits v ON r.repo_url=v.repo_url AND r.hash=v.hash AND r.branch=v.branch
		WHERE ` + resolvedFilter + rfv
	row := db.QueryRow(q, rfvArgs...)
	if row.Scan(&total, &firstTS, &lastTS) == nil {
		ra.Total = total
		if firstTS != nil && lastTS != nil {
			first, err1 := time.Parse(time.RFC3339, *firstTS)
			last, err2 := time.Parse(time.RFC3339, *lastTS)
			if err1 != nil || err2 != nil {
				slog.Debug("analytics release timestamp parse", "firstErr", err1, "lastErr", err2)
			}
			months := last.Sub(first).Hours() / (24 * 30)
			if months > 0 {
				ra.PerMonth = float64(total) / months
			}
		}
	}

	return ra
}

// queryReviewAnalytics gathers review extension metrics. Must be called with mu held.
func queryReviewAnalytics(thirtyDaysAgo, repoURL string) *ReviewAnalytics {
	rva := &ReviewAnalytics{}
	rfv, rfvArgs := repoFilter("v", repoURL)

	// PR counts by state
	for _, item := range []struct {
		state string
		count *int
	}{
		{"open", &rva.OpenPRs},
		{"merged", &rva.MergedPRs},
		{"closed", &rva.ClosedPRs},
	} {
		q := `SELECT COUNT(*) FROM review_items rv
			JOIN core_commits v ON rv.repo_url=v.repo_url AND rv.hash=v.hash AND rv.branch=v.branch
			WHERE rv.type='pull-request' AND rv.state=? AND ` + resolvedFilter + rfv
		row := db.QueryRow(q, append([]any{item.state}, rfvArgs...)...)
		logScanErr(row.Scan(item.count))
	}

	// PRs per day (30d)
	q := `SELECT DATE(v.effective_timestamp) d, COUNT(*) cnt
		FROM review_items rv JOIN core_commits v ON rv.repo_url=v.repo_url AND rv.hash=v.hash AND rv.branch=v.branch
		WHERE rv.type='pull-request' AND ` + resolvedFilter + ` AND v.effective_timestamp > ?` + rfv + `
		GROUP BY d ORDER BY d`
	args := append([]any{thirtyDaysAgo}, rfvArgs...)
	rows, err := db.Query(q, args...)
	if err == nil {
		for rows.Next() {
			var ds DayStat
			if rows.Scan(&ds.Date, &ds.Count) == nil {
				rva.PRsPerDay = append(rva.PRsPerDay, ds)
			}
		}
		rows.Close()
	}

	// Total feedback count
	q = `SELECT COUNT(*) FROM review_items rv
		JOIN core_commits v ON rv.repo_url=v.repo_url AND rv.hash=v.hash AND rv.branch=v.branch
		WHERE rv.type='feedback' AND ` + resolvedFilter + rfv
	row := db.QueryRow(q, rfvArgs...)
	logScanErr(row.Scan(&rva.TotalFeedback))

	return rva
}

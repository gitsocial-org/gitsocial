// activity.go - The activity events of the repositories that the user set to the notification level all
package social

import (
	"database/sql"
	"slices"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/cache"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/settings"
)

// getActivityNotifications returns one activity event for each timeline row of the workspace, a registered fork or a list repository at the level all, newer than the set time and not by the user.
func getActivityNotifications(workdir, workspaceURL, userEmail string, unreadOnly bool) ([]Notification, error) {
	levels := settings.NotificationLevels()
	if len(levels) == 0 {
		return nil, nil
	}
	forks := gitmsg.GetForks(workdir)
	listRepos, err := listRepositories(workdir)
	if err != nil {
		return nil, err
	}
	var out []Notification
	seen := make(map[string]bool)
	for _, repoURL := range slices.Concat([]string{workspaceURL}, forks, listRepos) {
		if seen[repoURL] {
			continue
		}
		seen[repoURL] = true
		level, setAt := settings.NotificationLevel(levels, repoURL, workspaceURL, forks)
		if level != settings.LevelAll {
			continue
		}
		events, err := activityEvents(repoURL, workspaceURL, slices.Contains(forks, repoURL), setAt, userEmail, unreadOnly)
		if err != nil {
			return nil, err
		}
		out = append(out, events...)
	}
	return out, nil
}

// listRepositories returns the repositories of the lists of a workdir.
func listRepositories(workdir string) ([]string, error) {
	return cache.QueryLocked(func(db *sql.DB) ([]string, error) {
		rows, err := db.Query(`SELECT DISTINCT lr.repo_url FROM core_list_repositories lr
			JOIN core_lists l ON lr.list_id = l.id WHERE l.workdir = ?`, workdir)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var urls []string
		for rows.Next() {
			var url string
			if err := rows.Scan(&url); err != nil {
				return nil, err
			}
			urls = append(urls, url)
		}
		return urls, rows.Err()
	})
}

// activityEvents reads the timeline rows of one repository with their read state: the source of a fork drops a copy of a workspace commit.
func activityEvents(repoURL, workspaceURL string, isFork bool, setAt time.Time, userEmail string, unreadOnly bool) ([]Notification, error) {
	wheres, whereArgs := timelineWheres(nil, repoURL, nil, "")
	if isFork {
		wheres, whereArgs = timelineWheres(nil, workspaceURL, []string{repoURL}, "")
	}
	where, sourceArgs := wheres[len(wheres)-1], whereArgs[len(whereArgs)-1]
	query := `SELECT t.*, r.hash IS NOT NULL FROM (` + baseSelectFromView + where +
		` AND v.timestamp > ? AND v.author_email != ?) t ` + cache.ReadStateJoin("r", "t.repo_url", "t.hash")
	if unreadOnly {
		query += " WHERE r.hash IS NULL"
	}
	args := slices.Concat([]interface{}{workspaceURL}, sourceArgs, []interface{}{setAt.UTC().Format(time.RFC3339), userEmail})
	return cache.QueryLocked(func(db *sql.DB) ([]Notification, error) {
		rows, err := db.Query(query, args...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var events []Notification
		for rows.Next() {
			var isRead bool
			item, err := scanResolvedRow(rows, &isRead)
			if err != nil {
				return nil, err
			}
			post := timelinePost(*item)
			post.Display.IsWorkspacePost = item.RepoURL == workspaceURL
			events = append(events, Notification{
				ID:        post.ID,
				Type:      NotificationTypeActivity,
				Item:      &post,
				Actor:     post.Author,
				ActorRepo: item.RepoURL,
				Timestamp: item.Timestamp,
				IsRead:    isRead,
			})
		}
		return events, rows.Err()
	})
}

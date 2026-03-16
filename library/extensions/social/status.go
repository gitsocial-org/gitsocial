// status.go - Social extension status and time formatting utilities
package social

import (
	"fmt"
	"time"

	"github.com/gitsocial-org/gitsocial/core/cache"
	"github.com/gitsocial-org/gitsocial/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/core/log"
)

// FormatRelativeTime formats a time as a human-readable relative string.
func FormatRelativeTime(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		m := int(d.Minutes())
		if m == 1 {
			return "1 minute ago"
		}
		return fmt.Sprintf("%d minutes ago", m)
	case d < 24*time.Hour:
		h := int(d.Hours())
		if h == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", h)
	default:
		days := int(d.Hours() / 24)
		if days == 1 {
			return "1 day ago"
		}
		return fmt.Sprintf("%d days ago", days)
	}
}

type StatusData struct {
	Branch        string          `json:"branch"`
	Unpushed      *UnpushedCounts `json:"unpushed,omitempty"`
	LastFetch     time.Time       `json:"lastFetch,omitempty"`
	Lists         []ListInfo      `json:"lists"`
	Items         int             `json:"items"`
	FromLists     int             `json:"fromLists"`
	FromWorkspace int             `json:"fromWorkspace"`
}

type UnpushedCounts struct {
	Posts int `json:"posts"`
	Lists int `json:"lists"`
}

type ListInfo struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Repos int    `json:"repos"`
}

// Status returns the current social extension status for a workspace.
func Status(workdir, cacheDir string) Result[StatusData] {
	if !gitmsg.IsExtInitialized(workdir, "social") {
		return Failure[StatusData]("NOT_INITIALIZED", "Social extension not initialized. Run 'gitsocial social init' to initialize.")
	}
	branch := gitmsg.GetExtBranch(workdir, "social")

	data := StatusData{
		Branch: branch,
		Lists:  []ListInfo{},
	}

	if unpushed, err := gitmsg.GetUnpushedCounts(workdir, branch); err != nil {
		log.Debug("get unpushed counts failed", "error", err)
	} else if unpushed != nil && (unpushed.Posts > 0 || unpushed.Lists > 0) {
		data.Unpushed = &UnpushedCounts{
			Posts: unpushed.Posts,
			Lists: unpushed.Lists,
		}
	}

	if lastFetch, err := cache.GetLastFetch(); err != nil {
		log.Debug("get last fetch time failed", "error", err)
	} else if !lastFetch.IsZero() {
		data.LastFetch = lastFetch
	}

	names, err := gitmsg.EnumerateLists(workdir, "social")
	if err == nil {
		for _, name := range names {
			listData, err := gitmsg.ReadList(workdir, "social", name)
			if err != nil || listData == nil {
				continue
			}
			data.Lists = append(data.Lists, ListInfo{
				ID:    listData.ID,
				Name:  listData.Name,
				Repos: len(listData.Repositories),
			})
		}
	}

	if extStats, err := cache.GetExtensionStats("social"); err != nil {
		log.Debug("get extension stats failed", "error", err)
	} else if extStats != nil {
		data.Items = extStats.Items
		data.FromLists = extStats.BySource["list"]
		data.FromWorkspace = extStats.BySource["workspace"]
	}

	return Success(data)
}

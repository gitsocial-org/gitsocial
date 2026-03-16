// utils.go - Git utility functions for user config and repository detection
package git

import (
	"sort"
	"time"
)

// MergeCommitsChronologically combines and sorts commits by timestamp descending.
func MergeCommitsChronologically(myCommits, externalCommits []Commit) []Commit {
	all := make([]Commit, 0, len(myCommits)+len(externalCommits))
	all = append(all, myCommits...)
	all = append(all, externalCommits...)

	sort.Slice(all, func(i, j int) bool {
		return all[i].Timestamp.After(all[j].Timestamp)
	})

	return all
}

// GetFetchStartDate returns the start of the current week as YYYY-MM-DD.
func GetFetchStartDate() string {
	now := time.Now()
	weekday := int(now.Weekday())
	if weekday == 0 {
		weekday = 7
	}
	daysToMonday := weekday - 1
	monday := now.AddDate(0, 0, -daysToMonday)
	return monday.Format("2006-01-02")
}

// ranges.go - Fetch range tracking for incremental data loading
package cache

import (
	"database/sql"
	"log/slog"
	"sort"
	"time"
)

type FetchRange struct {
	ID           int64
	RepoURL      string
	RangeStart   string
	RangeEnd     string
	Status       string
	FetchedAt    time.Time
	CommitCount  int
	ErrorMessage sql.NullString
}

type DateGap struct {
	Start string
	End   string
}

// InsertFetchRange creates a new fetch range record for tracking.
func InsertFetchRange(repoURL, rangeStart, rangeEnd string) (int64, error) {
	mu.Lock()
	defer mu.Unlock()
	if db == nil {
		return 0, ErrNotOpen
	}
	result, err := db.Exec(`
		INSERT INTO core_fetch_ranges (repo_url, range_start, range_end, status)
		VALUES (?, ?, ?, 'partial')`,
		repoURL, rangeStart, rangeEnd)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

// UpdateFetchRangeStatus updates the completion status of a fetch range.
func UpdateFetchRangeStatus(id int64, status string, commitCount int, errMsg string) error {
	mu.Lock()
	defer mu.Unlock()
	if db == nil {
		return ErrNotOpen
	}
	var errMsgVal sql.NullString
	if errMsg != "" {
		errMsgVal = sql.NullString{String: errMsg, Valid: true}
	}

	_, err := db.Exec(`
		UPDATE core_fetch_ranges
		SET status = ?, commit_count = ?, error_message = ?
		WHERE id = ?`,
		status, commitCount, errMsgVal, id)
	return err
}

// GetFetchRanges returns all fetch ranges for a repository.
func GetFetchRanges(repoURL string) ([]FetchRange, error) {
	mu.RLock()
	defer mu.RUnlock()
	if db == nil {
		return nil, ErrNotOpen
	}
	rows, err := db.Query(`
		SELECT id, repo_url, range_start, range_end, status, fetched_at, commit_count, error_message
		FROM core_fetch_ranges
		WHERE repo_url = ?
		ORDER BY range_start`, repoURL)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanFetchRanges(rows)
}

// FindMissingRanges finds date gaps that haven't been fetched yet.
func FindMissingRanges(repoURL, desiredStart, desiredEnd string) ([]DateGap, error) {
	mu.RLock()
	defer mu.RUnlock()
	if db == nil {
		return nil, ErrNotOpen
	}
	rows, err := db.Query(`
		SELECT id, repo_url, range_start, range_end, status, fetched_at, commit_count, error_message
		FROM core_fetch_ranges
		WHERE repo_url = ? AND status = 'complete'
		  AND range_end >= ? AND range_start <= ?
		ORDER BY range_start`,
		repoURL, desiredStart, desiredEnd)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ranges, err := scanFetchRanges(rows)
	if err != nil {
		return nil, err
	}

	return computeGaps(ranges, desiredStart, desiredEnd), nil
}

// computeGaps computes unfetched date gaps from a list of ranges.
func computeGaps(ranges []FetchRange, desiredStart, desiredEnd string) []DateGap {
	if len(ranges) == 0 {
		return []DateGap{{Start: desiredStart, End: desiredEnd}}
	}

	sort.Slice(ranges, func(i, j int) bool {
		return ranges[i].RangeStart < ranges[j].RangeStart
	})

	var gaps []DateGap
	cursor := desiredStart

	for _, r := range ranges {
		if r.RangeStart > cursor {
			gapEnd := r.RangeStart
			if gapEnd > desiredEnd {
				gapEnd = desiredEnd
			}
			if cursor < gapEnd {
				gaps = append(gaps, DateGap{Start: cursor, End: gapEnd})
			}
		}
		if r.RangeEnd > cursor {
			cursor = r.RangeEnd
		}
	}

	if cursor < desiredEnd {
		gaps = append(gaps, DateGap{Start: cursor, End: desiredEnd})
	}

	return gaps
}

// scanFetchRanges scans database rows into FetchRange structs.
func scanFetchRanges(rows *sql.Rows) ([]FetchRange, error) {
	var ranges []FetchRange
	for rows.Next() {
		var fr FetchRange
		var fetchedAt string

		err := rows.Scan(
			&fr.ID,
			&fr.RepoURL,
			&fr.RangeStart,
			&fr.RangeEnd,
			&fr.Status,
			&fetchedAt,
			&fr.CommitCount,
			&fr.ErrorMessage,
		)
		if err != nil {
			return nil, err
		}

		if t, err := time.Parse(time.RFC3339, fetchedAt); err == nil {
			fr.FetchedAt = t
		}
		ranges = append(ranges, fr)
	}
	return ranges, rows.Err()
}

// HasFetchRanges returns true if the repo has any completed fetch ranges.
func HasFetchRanges(repoURL string) bool {
	mu.RLock()
	defer mu.RUnlock()
	if db == nil {
		return false
	}
	var count int
	err := db.QueryRow(`
		SELECT COUNT(*) FROM core_fetch_ranges
		WHERE repo_url = ? AND status = 'complete'
	`, repoURL).Scan(&count)
	if err != nil {
		slog.Debug("has fetch ranges", "error", err, "repo", repoURL)
		return false
	}
	return count > 0
}

// GetFetchedMonths returns distinct months that have been fetched for a repo.
// Returns sorted list like ["2026-01", "2025-12", "2025-11"] (newest first).
func GetFetchedMonths(repoURL string) ([]string, error) {
	mu.RLock()
	defer mu.RUnlock()
	if db == nil {
		return nil, ErrNotOpen
	}
	rows, err := db.Query(`
		SELECT DISTINCT strftime('%Y-%m', range_start) as month
		FROM core_fetch_ranges
		WHERE repo_url = ? AND status = 'complete'
		ORDER BY month DESC
	`, repoURL)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var months []string
	for rows.Next() {
		var m string
		if err := rows.Scan(&m); err != nil {
			return nil, err
		}
		months = append(months, m)
	}
	return months, rows.Err()
}

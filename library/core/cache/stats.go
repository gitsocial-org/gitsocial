// stats.go - Cache statistics and extension-specific metrics
package cache

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"time"
)

type CacheStats struct {
	Location      string           `json:"location"`
	DbSizeBytes   int64            `json:"dbSizeBytes"`
	RepoSizeBytes int64            `json:"repoSizeBytes"`
	ForkSizeBytes int64            `json:"forkSizeBytes"`
	TotalBytes    int64            `json:"totalBytes"`
	MemoryBytes   int64            `json:"memoryBytes"`
	Items         int              `json:"items"`
	Repositories  int              `json:"repositories"`
	ForkCount     int              `json:"forkCount"`
	TopRepos      []RepositoryInfo `json:"topRepos,omitempty"`
	TopForks      []RepositoryInfo `json:"topForks,omitempty"`
}

type RepositoryInfo struct {
	URL       string    `json:"url"`
	RepoURL   string    `json:"repoUrl,omitempty"`
	Path      string    `json:"path,omitempty"`
	Size      int64     `json:"size"`
	Commits   int       `json:"commits"`
	LastFetch time.Time `json:"lastFetch,omitempty"`
}

type ExtensionStats struct {
	Extension string         `json:"extension"`
	Items     int            `json:"items"`
	ByType    map[string]int `json:"byType"`
	BySource  map[string]int `json:"bySource"`
}

// GetStatsLite returns cache stats without filesystem walks. Uses os.Stat for
// file sizes and os.ReadDir for directory counts — no recursive traversal.
func GetStatsLite(cacheDir string) (*CacheStats, error) {
	stats := &CacheStats{
		Location: cacheDir,
	}

	dbPath := filepath.Join(cacheDir, "cache.db")
	if info, err := os.Stat(dbPath); err == nil {
		stats.DbSizeBytes = info.Size()
	}
	stats.TotalBytes = stats.DbSizeBytes

	if entries, err := os.ReadDir(filepath.Join(cacheDir, "repositories")); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				stats.Repositories++
			}
		}
	}
	if entries, err := os.ReadDir(filepath.Join(cacheDir, "forks")); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				stats.ForkCount++
			}
		}
	}

	mu.RLock()
	if db != nil {
		row := db.QueryRow("SELECT COUNT(*) FROM core_commits")
		if err := row.Scan(&stats.Items); err != nil {
			slog.Debug("stats lite scan commit count", "error", err)
		}
	}
	mu.RUnlock()

	return stats, nil
}

// GetStats collects overall cache statistics including size and counts.
func GetStats(cacheDir string) (*CacheStats, error) {
	stats := &CacheStats{
		Location: cacheDir,
	}

	dbPath := filepath.Join(cacheDir, "cache.db")
	if info, err := os.Stat(dbPath); err == nil {
		stats.DbSizeBytes = info.Size()
	}

	repoSize, repoCount := getRepositoriesSize(cacheDir)
	stats.RepoSizeBytes = repoSize
	stats.Repositories = repoCount
	forkSize, forkCount := getForkSize(cacheDir)
	stats.ForkSizeBytes = forkSize
	stats.ForkCount = forkCount
	stats.TotalBytes = stats.DbSizeBytes + stats.RepoSizeBytes + stats.ForkSizeBytes

	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)
	stats.MemoryBytes = int64(memStats.Alloc)

	mu.RLock()
	if db != nil {
		row := db.QueryRow("SELECT COUNT(*) FROM core_commits")
		if err := row.Scan(&stats.Items); err != nil {
			slog.Debug("stats scan commit count", "error", err)
		}
	}
	mu.RUnlock()

	// Get all repos by size (acquires its own lock)
	stats.TopRepos = getTopRepositoriesBySize(cacheDir, 0)
	stats.TopForks = getTopForksBySize(cacheDir)

	return stats, nil
}

// getRepositoriesSize calculates total size and count of cached repositories.
func getRepositoriesSize(cacheDir string) (int64, int) {
	reposDir := filepath.Join(cacheDir, "repositories")
	var totalSize int64
	var count int
	entries, err := os.ReadDir(reposDir)
	if err != nil {
		return 0, 0
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		count++
		dirPath := filepath.Join(reposDir, entry.Name())
		if walkErr := filepath.Walk(dirPath, func(_ string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			totalSize += info.Size()
			return nil
		}); walkErr != nil {
			slog.Debug("walk repo dir", "error", walkErr, "path", dirPath)
		}
	}
	return totalSize, count
}

// getForkSize calculates total size and count of fork bare repos.
func getForkSize(cacheDir string) (int64, int) {
	forksDir := filepath.Join(cacheDir, "forks")
	var totalSize int64
	var count int
	entries, err := os.ReadDir(forksDir)
	if err != nil {
		return 0, 0
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		count++
		dirPath := filepath.Join(forksDir, entry.Name())
		if walkErr := filepath.Walk(dirPath, func(_ string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			totalSize += info.Size()
			return nil
		}); walkErr != nil {
			slog.Debug("walk fork dir", "error", walkErr, "path", dirPath)
		}
	}
	return totalSize, count
}

// getTopForksBySize returns fork repos sorted by size descending.
func getTopForksBySize(cacheDir string) []RepositoryInfo {
	forksDir := filepath.Join(cacheDir, "forks")
	entries, err := os.ReadDir(forksDir)
	if err != nil {
		return nil
	}
	forks := make([]RepositoryInfo, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dirPath := filepath.Join(forksDir, entry.Name())
		var size int64
		if walkErr := filepath.Walk(dirPath, func(_ string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			size += info.Size()
			return nil
		}); walkErr != nil {
			slog.Debug("walk fork dir", "error", walkErr, "path", dirPath)
		}
		forks = append(forks, RepositoryInfo{
			URL:  entry.Name(),
			Path: dirPath,
			Size: size,
		})
	}
	sort.Slice(forks, func(i, j int) bool {
		return forks[i].Size > forks[j].Size
	})
	return forks
}

// getTopRepositoriesBySize returns repositories sorted by size descending.
func getTopRepositoriesBySize(cacheDir string, limit int) []RepositoryInfo {
	reposDir := filepath.Join(cacheDir, "repositories")
	entries, err := os.ReadDir(reposDir)
	if err != nil {
		return nil
	}

	// Build maps of storage paths to last_fetch times and repo URLs from database
	lastFetchMap := make(map[string]time.Time)
	repoURLMap := make(map[string]string)
	mu.RLock()
	if db != nil {
		rows, err := db.Query("SELECT url, storage_path, last_fetch FROM core_repositories")
		if err == nil {
			for rows.Next() {
				var url, storagePath string
				var lastFetch *string
				if rows.Scan(&url, &storagePath, &lastFetch) == nil {
					repoURLMap[storagePath] = url
					if lastFetch != nil {
						if t, err := time.Parse(time.RFC3339, *lastFetch); err == nil {
							lastFetchMap[storagePath] = t
						}
					}
				}
			}
			rows.Close()
		}
	}
	mu.RUnlock()

	repos := make([]RepositoryInfo, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dirPath := filepath.Join(reposDir, entry.Name())

		// Get size from filesystem
		var size int64
		if walkErr := filepath.Walk(dirPath, func(_ string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			size += info.Size()
			return nil
		}); walkErr != nil {
			slog.Debug("walk repo dir", "error", walkErr, "path", dirPath)
		}

		// Count commits directly from the bare git repo
		commits := countCommitsInBareRepo(dirPath)

		info := RepositoryInfo{
			URL:     entry.Name(),
			Path:    dirPath,
			Size:    size,
			Commits: commits,
		}
		if url, ok := repoURLMap[dirPath]; ok {
			info.RepoURL = url
		}
		if t, ok := lastFetchMap[dirPath]; ok {
			info.LastFetch = t
		}
		repos = append(repos, info)
	}

	sort.Slice(repos, func(i, j int) bool {
		return repos[i].Size > repos[j].Size
	})

	// Return top N (0 means all)
	if limit > 0 && len(repos) > limit {
		repos = repos[:limit]
	}
	return repos
}

// countCommitsInBareRepo estimates commit count from git objects directory.
func countCommitsInBareRepo(repoPath string) int {
	// Count objects in the git objects directory as a proxy for commits
	// This is faster than running git commands
	objectsDir := filepath.Join(repoPath, "objects")
	count := 0

	// Count loose objects (in subdirectories like objects/ab/...)
	entries, err := os.ReadDir(objectsDir)
	if err != nil {
		return 0
	}

	for _, entry := range entries {
		if !entry.IsDir() || len(entry.Name()) != 2 {
			continue
		}
		subdir := filepath.Join(objectsDir, entry.Name())
		files, err := os.ReadDir(subdir)
		if err != nil {
			continue
		}
		count += len(files)
	}

	// Also check pack files for packed objects (estimate from file count)
	packDir := filepath.Join(objectsDir, "pack")
	if packs, err := os.ReadDir(packDir); err == nil {
		for _, p := range packs {
			if filepath.Ext(p.Name()) == ".idx" {
				// Each pack file typically contains many objects
				// Use file size as rough estimate (idx files are ~24 bytes per object)
				if info, err := p.Info(); err == nil {
					count += int(info.Size() / 24)
				}
			}
		}
	}

	return count
}

// FormatBytes formats bytes as a human-readable string (KB, MB, etc).
func FormatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return "0B"
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%cB", float64(b)/float64(div), "KMGTPE"[exp])
}

// FormatBytesMB formats bytes as megabytes with fixed width.
func FormatBytesMB(b int64) string {
	mb := float64(b) / (1024 * 1024)
	return fmt.Sprintf("%8.2fMB", mb)
}

// GetExtensionStats returns statistics for a specific extension.
func GetExtensionStats(extension string) (*ExtensionStats, error) {
	mu.RLock()
	defer mu.RUnlock()

	stats := &ExtensionStats{
		Extension: extension,
		ByType:    make(map[string]int),
		BySource:  make(map[string]int),
	}

	if db == nil {
		return stats, nil
	}

	// For "social" extension, query social_items
	if extension == "social" {
		row := db.QueryRow("SELECT COUNT(*) FROM social_items")
		if err := row.Scan(&stats.Items); err != nil {
			slog.Debug("stats scan social count", "error", err)
		}

		rows, err := db.Query("SELECT type, COUNT(*) FROM social_items GROUP BY type")
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var t string
				var c int
				if err := rows.Scan(&t, &c); err != nil {
					slog.Debug("stats scan social type", "error", err)
					continue
				}
				stats.ByType[t] = c
			}
		}

		rows, err = db.Query("SELECT s.repo_url, COUNT(*) FROM social_items s GROUP BY s.repo_url")
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var s string
				var c int
				if err := rows.Scan(&s, &c); err != nil {
					slog.Debug("stats scan social source", "error", err)
					continue
				}
				stats.BySource[s] = c
			}
		}
	}

	return stats, nil
}

// GetLastFetch returns the most recent fetch timestamp across all repos.
func GetLastFetch() (time.Time, error) {
	mu.RLock()
	defer mu.RUnlock()

	if db == nil {
		return time.Time{}, nil
	}

	var lastFetch string
	row := db.QueryRow("SELECT MAX(last_fetch) FROM core_repositories")
	if err := row.Scan(&lastFetch); err != nil || lastFetch == "" {
		return time.Time{}, nil
	}

	return time.Parse(time.RFC3339, lastFetch)
}

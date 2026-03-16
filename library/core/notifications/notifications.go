// notifications.go - Core notification aggregation, provider registry, and read state management
package notifications

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gitsocial-org/gitsocial/core/cache"
)

// Notification represents a unified notification from any extension.
type Notification struct {
	RepoURL   string
	Hash      string
	Branch    string
	Type      string // "comment", "repost", "quote", "follow", "mention", "reference"
	Source    string // "social", "core", "pm", "review"
	Item      any
	Actor     Actor
	ActorRepo string
	Timestamp time.Time
	IsRead    bool
}

// Actor identifies who triggered the notification.
type Actor struct {
	Name  string
	Email string
}

// Filter controls which notifications are returned.
type Filter struct {
	UnreadOnly bool
	Types      []string
	Limit      int
}

// Provider supplies notifications from a specific source.
type Provider interface {
	GetNotifications(workdir string, filter Filter) ([]Notification, error)
	GetUnreadCount(workdir string) (int, error)
}

var (
	providers []namedProvider
	mu        sync.RWMutex
)

type namedProvider struct {
	name     string
	provider Provider
}

// RegisterProvider registers a notification provider by name.
func RegisterProvider(name string, p Provider) {
	mu.Lock()
	defer mu.Unlock()
	providers = append(providers, namedProvider{name: name, provider: p})
}

// GetAll aggregates notifications from all registered providers, sorted by timestamp descending.
func GetAll(workdir string, filter Filter) ([]Notification, error) {
	mu.RLock()
	snapshot := make([]namedProvider, len(providers))
	copy(snapshot, providers)
	mu.RUnlock()

	var all []Notification
	for _, np := range snapshot {
		items, err := np.provider.GetNotifications(workdir, filter)
		if err != nil {
			continue
		}
		all = append(all, items...)
	}

	if len(filter.Types) > 0 {
		typeSet := make(map[string]bool, len(filter.Types))
		for _, t := range filter.Types {
			typeSet[strings.TrimSpace(t)] = true
		}
		filtered := all[:0]
		for _, n := range all {
			if typeSet[n.Type] {
				filtered = append(filtered, n)
			}
		}
		all = filtered
	}

	sort.Slice(all, func(i, j int) bool {
		return all[i].Timestamp.After(all[j].Timestamp)
	})

	if filter.Limit > 0 && len(all) > filter.Limit {
		all = all[:filter.Limit]
	}

	return all, nil
}

// GetUnreadCount returns the total unread count across all providers.
func GetUnreadCount(workdir string) (int, error) {
	mu.RLock()
	snapshot := make([]namedProvider, len(providers))
	copy(snapshot, providers)
	mu.RUnlock()

	total := 0
	for _, np := range snapshot {
		count, err := np.provider.GetUnreadCount(workdir)
		if err != nil {
			continue
		}
		total += count
	}
	return total, nil
}

// MarkAsRead marks a single notification as read in core_notification_reads.
func MarkAsRead(repoURL, hash, branch string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	return cache.ExecLocked(func(db *sql.DB) error {
		_, err := db.Exec(`
			INSERT INTO core_notification_reads (repo_url, hash, branch, read_at) VALUES (?, ?, ?, ?)
			ON CONFLICT(repo_url, hash, branch) DO NOTHING
		`, repoURL, hash, branch, now)
		return err
	})
}

// MarkAsUnread marks a single notification as unread by removing from core_notification_reads.
func MarkAsUnread(repoURL, hash, branch string) error {
	return cache.ExecLocked(func(db *sql.DB) error {
		_, err := db.Exec(`
			DELETE FROM core_notification_reads WHERE repo_url = ? AND hash = ? AND branch = ?
		`, repoURL, hash, branch)
		return err
	})
}

// MarkAllAsRead marks all current notifications as read.
func MarkAllAsRead(workdir string) error {
	items, _ := GetAll(workdir, Filter{UnreadOnly: true})
	if len(items) == 0 {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	return cache.ExecLocked(func(db *sql.DB) error {
		const batchSize = 100
		for i := 0; i < len(items); i += batchSize {
			end := i + batchSize
			if end > len(items) {
				end = len(items)
			}
			batch := items[i:end]
			placeholders := make([]string, len(batch))
			args := make([]interface{}, 0, len(batch)*4)
			for j, n := range batch {
				placeholders[j] = "(?, ?, ?, ?)"
				args = append(args, n.RepoURL, n.Hash, n.Branch, now)
			}
			query := "INSERT INTO core_notification_reads (repo_url, hash, branch, read_at) VALUES " +
				strings.Join(placeholders, ",") +
				" ON CONFLICT(repo_url, hash, branch) DO NOTHING"
			if _, err := db.Exec(query, args...); err != nil {
				return fmt.Errorf("mark read batch: %w", err)
			}
		}
		return nil
	})
}

// MarkAllAsUnread marks all current notifications as unread.
func MarkAllAsUnread(workdir string) error {
	items, _ := GetAll(workdir, Filter{})
	if len(items) == 0 {
		return nil
	}
	return cache.ExecLocked(func(db *sql.DB) error {
		const batchSize = 100
		for i := 0; i < len(items); i += batchSize {
			end := i + batchSize
			if end > len(items) {
				end = len(items)
			}
			batch := items[i:end]
			placeholders := make([]string, len(batch))
			args := make([]interface{}, 0, len(batch)*3)
			for j, n := range batch {
				placeholders[j] = "(?, ?, ?)"
				args = append(args, n.RepoURL, n.Hash, n.Branch)
			}
			query := "DELETE FROM core_notification_reads WHERE (repo_url, hash, branch) IN (" +
				strings.Join(placeholders, ",") + ")"
			if _, err := db.Exec(query, args...); err != nil {
				return fmt.Errorf("mark unread batch: %w", err)
			}
		}
		return nil
	})
}

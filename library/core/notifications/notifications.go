// notifications.go - Core notification aggregation, provider registry, and read state management
package notifications

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/cache"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
)

// Notification represents a unified notification from any extension.
type Notification struct {
	RepoURL   string
	Hash      string
	Branch    string
	Type      string // a type of the table in NOTIFICATIONS.md
	Source    string // "core", "gitmsg-divergence", or the name of an extension
	Item      any
	Actor     Actor
	ActorRepo string
	Timestamp time.Time
	IsRead    bool
	Condition bool // true until someone repairs it: no read state, no message
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

// GetAll aggregates notifications from all registered providers, sorted by timestamp descending; a failing provider is skipped.
func GetAll(workdir string, filter Filter) ([]Notification, error) {
	all, _ := gather(workdir, filter)
	return all, nil
}

// GetAllStrict aggregates like GetAll and also returns the errors of the providers that failed.
func GetAllStrict(workdir string, filter Filter) ([]Notification, error) {
	return gather(workdir, filter)
}

// gather reads each provider with no limit, applies the type filter, keeps one event per commit, sorts by time and cuts to the limit; it joins the provider errors.
func gather(workdir string, filter Filter) ([]Notification, error) {
	mu.RLock()
	snapshot := make([]namedProvider, len(providers))
	copy(snapshot, providers)
	mu.RUnlock()

	var all []Notification
	var failures []error
	for _, np := range snapshot {
		items, err := np.provider.GetNotifications(workdir, Filter{UnreadOnly: filter.UnreadOnly})
		if err != nil {
			failures = append(failures, fmt.Errorf("provider %s: %w", np.name, err))
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
	all = onePerKey(all, gitmsg.ResolveRepoURL(workdir))

	sort.SliceStable(all, func(i, j int) bool {
		return all[i].Timestamp.After(all[j].Timestamp)
	})

	if filter.Limit > 0 && len(all) > filter.Limit {
		all = all[:filter.Limit]
	}

	return all, errors.Join(failures...)
}

// onePerKey keeps one event per commit hash, and per repository for a follow: each other type before an activity row, then the row of the workspace, then the lowest rank, then the first; a condition has no key and stays.
func onePerKey(items []Notification, workspaceURL string) []Notification {
	type key struct{ repoURL, hash string }
	// order puts the row of the workspace before a copy of the commit in a fork, and an activity row after each other type of either.
	order := func(n Notification) int {
		rank := typeRank(n)
		if n.Type == "activity" {
			rank += 20
		}
		if n.RepoURL != workspaceURL {
			rank += 10
		}
		return rank
	}
	kept := make(map[key]int, len(items))
	out := items[:0]
	for _, n := range items {
		k := key{hash: n.Hash}
		if n.Hash == "follow" {
			k.repoURL = n.RepoURL
		}
		i, seen := kept[k]
		switch {
		case n.Condition || !seen:
			if !n.Condition {
				kept[k] = len(out)
			}
			out = append(out, n)
		case order(n) < order(out[i]):
			out[i] = n
		}
	}
	return out
}

// typeRank orders the types that one commit can have: an extension type, then a comment of social, then a mention, a reference, an edit and an activity event.
func typeRank(n Notification) int {
	switch {
	case n.Type == "activity":
		return 5
	case n.Source == "core" && n.Type == "edit":
		return 4
	case n.Source == "core" && n.Type == "reference":
		return 3
	case n.Source == "core":
		return 2
	case n.Source == "social":
		return 1
	}
	return 0
}

// GetUnreadCount returns the number of unread events and conditions, the same rows as the unread feed.
func GetUnreadCount(workdir string) (int, error) {
	items, _ := gather(workdir, Filter{UnreadOnly: true}) // a failing provider is skipped, as in GetAll
	return len(items), nil
}

// MarkAsRead marks one event as read; an event with no state row gets one, so it is never sent after.
func MarkAsRead(repoURL, hash string) error {
	return markRead([]Notification{{RepoURL: repoURL, Hash: hash}})
}

// MarkAsUnread marks one event as unread; the state row stays, so the event is not sent again.
func MarkAsUnread(repoURL, hash string) error {
	return markUnread([]Notification{{RepoURL: repoURL, Hash: hash}})
}

// MarkAllAsRead marks each unread event as read; a condition has no read state.
func MarkAllAsRead(workdir string) error {
	items, _ := GetAll(workdir, Filter{UnreadOnly: true}) // GetAll returns no error
	return markRead(events(items))
}

// MarkAllAsUnread marks each event as unread.
func MarkAllAsUnread(workdir string) error {
	items, _ := GetAll(workdir, Filter{}) // GetAll returns no error
	return markUnread(events(items))
}

// Claim writes a state row for each event that has none and returns the events whose row this call wrote, so one process sends each event.
func Claim(items []Notification) ([]Notification, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	var claimed []Notification
	err := cache.ExecLocked(func(db *sql.DB) error {
		for _, n := range events(items) {
			res, err := db.Exec(`INSERT INTO core_notification_state (repo_url, hash, sent_at)
				SELECT ?1, ?2, ?3 WHERE NOT EXISTS (SELECT 1 FROM core_notification_state WHERE `+cache.EventStateMatch("?1", "?2")+`)
				ON CONFLICT(repo_url, hash) DO NOTHING`, n.RepoURL, n.Hash, now)
			if err != nil {
				return fmt.Errorf("claim %s: %w", n.Hash, err)
			}
			if rows, _ := res.RowsAffected(); rows == 1 {
				claimed = append(claimed, n)
			}
		}
		return nil
	})
	return claimed, err
}

// events returns the notifications that are not conditions.
func events(items []Notification) []Notification {
	out := make([]Notification, 0, len(items))
	for _, n := range items {
		if !n.Condition {
			out = append(out, n)
		}
	}
	return out
}

// markRead writes the read time of each event, with a state row for an event that has none.
func markRead(items []Notification) error {
	if len(items) == 0 {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	return cache.ExecLocked(func(db *sql.DB) error {
		for _, n := range items {
			if _, err := db.Exec(`INSERT INTO core_notification_state (repo_url, hash, sent_at, read_at) VALUES (?, ?, ?, ?)
				ON CONFLICT(repo_url, hash) DO UPDATE SET read_at = excluded.read_at WHERE read_at IS NULL`,
				n.RepoURL, n.Hash, now, now); err != nil {
				return fmt.Errorf("mark read %s: %w", n.Hash, err)
			}
		}
		return nil
	})
}

// markUnread clears the read time of each event and keeps its state row.
func markUnread(items []Notification) error {
	if len(items) == 0 {
		return nil
	}
	return cache.ExecLocked(func(db *sql.DB) error {
		for _, n := range items {
			if _, err := db.Exec(`UPDATE core_notification_state SET read_at = NULL WHERE `+cache.EventStateMatch("?1", "?2"),
				n.RepoURL, n.Hash); err != nil {
				return fmt.Errorf("mark unread %s: %w", n.Hash, err)
			}
		}
		return nil
	})
}

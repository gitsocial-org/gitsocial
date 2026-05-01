// Package divergence provides a notification source for diverged
// gitmsg/<ext> branches. Lives in a sub-package because the registration
// path (core/gitmsg → core/notifications → core/fetch → core/gitmsg)
// would otherwise close an import cycle.
//
// Fires a notification when any local gitmsg/<ext> branch has unpushed
// commits AND a divergent remote tip — i.e., `gitsocial push` would be
// rejected as non-fast-forward unless the user runs the rebase flow
// first. Self-clearing: once the rebase + push lands and the branch
// converges with origin, the notification stops appearing on the next
// poll. No read tracking, by design — this is a state mirror, not an
// event feed.
package divergence

import (
	"errors"
	"time"

	"github.com/gitsocial-org/gitsocial/core/git"
	"github.com/gitsocial-org/gitsocial/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/core/notifications"
)

type provider struct{}

func init() {
	notifications.RegisterProvider("gitmsg-divergence", &provider{})
}

// Notification carries the branch name as the Item payload for a
// "branch-diverged" notification.
type Notification struct {
	Branch string
}

// GetNotifications returns one entry per diverged gitmsg branch.
func (p *provider) GetNotifications(workdir string,
	_ notifications.Filter) ([]notifications.Notification, error) {
	if workdir == "" {
		return nil, nil
	}
	repoURL := gitmsg.ResolveRepoURL(workdir)
	if repoURL == "" {
		return nil, nil
	}
	now := time.Now()
	var out []notifications.Notification
	for _, branch := range gitmsg.GetExtBranches(workdir) {
		err := git.ValidatePushPreconditions(workdir, "origin", branch)
		if err == nil || !errors.Is(err, git.ErrDiverged) {
			continue
		}
		out = append(out, notifications.Notification{
			RepoURL:   repoURL,
			Branch:    branch,
			Type:      "branch-diverged",
			Source:    "gitmsg-divergence",
			Item:      Notification{Branch: branch},
			Timestamp: now,
			IsRead:    false,
		})
	}
	return out, nil
}

// GetUnreadCount returns the count of diverged branches.
func (p *provider) GetUnreadCount(workdir string) (int, error) {
	notifs, err := p.GetNotifications(workdir, notifications.Filter{})
	if err != nil {
		return 0, err
	}
	return len(notifs), nil
}

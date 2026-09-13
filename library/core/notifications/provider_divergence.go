// provider_divergence.go - Notification provider for diverged gitmsg/<ext> branches
package notifications

import (
	"errors"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
)

type divergenceProvider struct{}

func init() {
	// Registering here gives the CLI, the TUI and RPC the same notification set.
	RegisterProvider("gitmsg-divergence", &divergenceProvider{})
}

// DivergenceNotification carries the branch name as the Item payload of a "branch-diverged" notification.
type DivergenceNotification struct {
	Branch string
}

// GetNotifications returns one entry per diverged gitmsg branch.
func (p *divergenceProvider) GetNotifications(workdir string, _ Filter) ([]Notification, error) {
	if workdir == "" {
		return nil, nil
	}
	repoURL := gitmsg.ResolveRepoURL(workdir)
	if repoURL == "" {
		return nil, nil
	}
	now := time.Now()
	remote := git.PushRemote(workdir)
	var out []Notification
	for _, branch := range gitmsg.GetExtBranches(workdir) {
		err := git.ValidatePushPreconditions(workdir, remote, branch)
		if err == nil || !errors.Is(err, git.ErrDiverged) {
			continue
		}
		// The entry clears itself once the branch converges, so it carries no read state.
		out = append(out, Notification{
			RepoURL:   repoURL,
			Branch:    branch,
			Type:      "branch-diverged",
			Source:    "gitmsg-divergence",
			Item:      DivergenceNotification{Branch: branch},
			Timestamp: now,
			IsRead:    false,
		})
	}
	return out, nil
}

// GetUnreadCount returns the count of diverged branches.
func (p *divergenceProvider) GetUnreadCount(workdir string) (int, error) {
	notifs, err := p.GetNotifications(workdir, Filter{})
	if err != nil {
		return 0, err
	}
	return len(notifs), nil
}

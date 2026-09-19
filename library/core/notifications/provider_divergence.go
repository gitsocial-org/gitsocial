// provider_divergence.go - Notification provider for diverged gitmsg/<ext> branches
package notifications

import (
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
)

// gitmsgBranchPrefix is the refs/heads prefix every extension branch carries.
const gitmsgBranchPrefix = "gitmsg/"

type divergenceProvider struct{}

// init registers the branch divergence notification provider.
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
	local, remotes, err := git.ReadBranchTips(workdir, gitmsgBranchPrefix)
	if err != nil {
		return nil, fmt.Errorf("read gitmsg branch tips: %w", err)
	}
	if !anyRemoteTipDiffers(local, remotes) {
		return nil, nil
	}
	remote := git.PushRemote(workdir)
	now := time.Now()
	var out []Notification
	for _, branch := range slices.Sorted(maps.Keys(local)) {
		tip, ok := remotes[remote][branch]
		if !ok || tip == local[branch] {
			continue
		}
		diverged, err := git.BranchDiverged(workdir, remote, branch)
		if err != nil {
			slog.Debug("divergence check", "error", err, "branch", branch, "remote", remote)
			continue
		}
		if !diverged {
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

// anyRemoteTipDiffers reports whether a remote holds a tracked branch at a hash its local branch does not carry.
func anyRemoteTipDiffers(local map[string]string, remotes map[string]map[string]string) bool {
	for _, tips := range remotes {
		for branch, tip := range tips {
			if localTip, ok := local[branch]; ok && localTip != tip {
				return true
			}
		}
	}
	return false
}

// GetUnreadCount returns the count of diverged branches.
func (p *divergenceProvider) GetUnreadCount(workdir string) (int, error) {
	notifs, err := p.GetNotifications(workdir, Filter{})
	if err != nil {
		return 0, err
	}
	return len(notifs), nil
}

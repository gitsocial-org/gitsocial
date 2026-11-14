// notifications.go - CLI commands for viewing and managing notifications
package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/gitsocial-org/gitsocial/library/core/notifications"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
	"github.com/gitsocial-org/gitsocial/library/core/text"
	"github.com/gitsocial-org/gitsocial/library/extensions/review"
	"github.com/gitsocial-org/gitsocial/library/extensions/social"
)

// newNotificationsCmd creates the parent command for viewing and managing notifications.
func newNotificationsCmd() *cobra.Command {
	var all bool
	var limit int
	var typeFilter string

	cmd := &cobra.Command{
		Use:   "notifications",
		Short: "View and manage notifications",
		Long: `View notifications about activity on your work: comments, reposts, and
quotes on your posts, mentions, edits to your items, follows, and PR, issue,
and release activity from other users.

By default shows unread notifications only. Use --all to see all notifications.
Use --type to filter by notification type (comma-separated).

Valid types: mention, comment, repost, quote, follow, fork-pr, feedback,
approved, changes-requested, issue-assigned, new-release, edit, activity`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !EnsureGitRepo(cmd) {
				return exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			filter := notifications.Filter{
				UnreadOnly: !all,
				Limit:      limit,
			}
			if typeFilter != "" {
				filter.Types = strings.Split(typeFilter, ",")
			}

			items, err := notifications.GetAll(cfg.WorkDir, filter)
			if err != nil {
				PrintError(cmd, "read notifications: "+err.Error())
				return exit(ExitError)
			}

			if cfg.JSONOutput {
				return PrintJSON(cmd, items)
			}

			if len(items) == 0 {
				if all {
					fmt.Fprintln(cmd.OutOrStdout(), "No notifications.")
				} else {
					fmt.Fprintln(cmd.OutOrStdout(), "No unread notifications.")
				}
				return nil
			}

			fmt.Fprintln(cmd.OutOrStdout(), formatNotifications(items))
			return nil
		},
	}

	cmd.Flags().BoolVarP(&all, "all", "a", false, "Show read notifications too")
	cmd.Flags().IntVarP(&limit, "limit", "n", 20, "Maximum number of notifications")
	cmd.Flags().StringVarP(&typeFilter, "type", "t", "", "Filter by comma-separated types")

	cmd.AddCommand(
		newNotificationsCountCmd(),
		newNotificationsReadCmd(),
		newNotificationsReadAllCmd(),
		newNotificationsUnreadCmd(),
		newNotificationsUnreadAllCmd(),
		newNotificationsLevelCmd(),
	)

	return cmd
}

// newNotificationsCountCmd creates the command to show unread notification count.
func newNotificationsCountCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "count",
		Short: "Show unread notification count",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !EnsureGitRepo(cmd) {
				return exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			count, err := notifications.GetUnreadCount(cfg.WorkDir)
			if err != nil {
				PrintError(cmd, "read the unread count: "+err.Error())
				return exit(ExitError)
			}

			if cfg.JSONOutput {
				return PrintJSON(cmd, map[string]int{"unread": count})
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "%d unread\n", count)
			}
			return nil
		},
	}
}

// notificationKey parses a notification id, <repo>#commit:<hash> or <repo>#follow, into the key of its event.
func notificationKey(id string) (repoURL, hash string, ok bool) {
	if repo, found := strings.CutSuffix(id, "#follow"); found {
		return repo, "follow", repo != ""
	}
	ref := protocol.ParseRef(id)
	return ref.Repository, ref.Value, ref.Type == "commit" && ref.Repository != "" && ref.Value != ""
}

// notificationID returns the id that read and unread take for an event.
func notificationID(n notifications.Notification) string {
	if n.Hash == "follow" {
		return n.RepoURL + "#follow"
	}
	return n.RepoURL + "#commit:" + n.Hash
}

// newNotificationsReadCmd creates the command to mark a notification as read.
func newNotificationsReadCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "read <notification-id>",
		Short: "Mark a notification as read",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !EnsureGitRepo(cmd) {
				return exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			id := args[0]

			repoURL, hash, ok := notificationKey(id)
			if !ok {
				PrintError(cmd, "invalid notification ID")
				return exit(ExitInvalidArgs)
			}

			if err := notifications.MarkAsRead(repoURL, hash); err != nil {
				PrintError(cmd, "mark as read: "+err.Error())
				return exit(ExitError)
			}

			if cfg.JSONOutput {
				return PrintJSON(cmd, map[string]string{"status": "read", "id": id})
			} else {
				PrintSuccess(cmd, "Marked as read")
			}
			return nil
		},
	}
}

// newNotificationsReadAllCmd creates the command to mark all notifications as read.
func newNotificationsReadAllCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "read-all",
		Short: "Mark all notifications as read",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !EnsureGitRepo(cmd) {
				return exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			if err := notifications.MarkAllAsRead(cfg.WorkDir); err != nil {
				PrintError(cmd, "mark all as read: "+err.Error())
				return exit(ExitError)
			}

			if cfg.JSONOutput {
				return PrintJSON(cmd, map[string]string{"status": "all_read"})
			} else {
				PrintSuccess(cmd, "All notifications marked as read")
			}
			return nil
		},
	}
}

// newNotificationsUnreadCmd creates the command to mark a notification as unread.
func newNotificationsUnreadCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "unread <notification-id>",
		Short: "Mark a notification as unread",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !EnsureGitRepo(cmd) {
				return exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			id := args[0]

			repoURL, hash, ok := notificationKey(id)
			if !ok {
				PrintError(cmd, "invalid notification ID")
				return exit(ExitInvalidArgs)
			}

			if err := notifications.MarkAsUnread(repoURL, hash); err != nil {
				PrintError(cmd, "mark as unread: "+err.Error())
				return exit(ExitError)
			}

			if cfg.JSONOutput {
				return PrintJSON(cmd, map[string]string{"status": "unread", "id": id})
			} else {
				PrintSuccess(cmd, "Marked as unread")
			}
			return nil
		},
	}
}

// newNotificationsUnreadAllCmd creates the command to mark all notifications as unread.
func newNotificationsUnreadAllCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "unread-all",
		Short: "Mark all notifications as unread",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !EnsureGitRepo(cmd) {
				return exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			if err := notifications.MarkAllAsUnread(cfg.WorkDir); err != nil {
				PrintError(cmd, "mark all as unread: "+err.Error())
				return exit(ExitError)
			}

			if cfg.JSONOutput {
				return PrintJSON(cmd, map[string]string{"status": "all_unread"})
			} else {
				PrintSuccess(cmd, "All notifications marked as unread")
			}
			return nil
		},
	}
}

// formatNotifications formats a list of notifications for display.
func formatNotifications(items []notifications.Notification) string {
	parts := make([]string, 0, len(items))
	for _, n := range items {
		parts = append(parts, formatNotification(n))
	}
	return strings.Join(parts, "\n\n---\n\n")
}

// formatNotification formats one notification: the type, the read state, the title and the time, then the body of its message and the detail of its type.
func formatNotification(n notifications.Notification) string {
	readStatus := ""
	if !n.IsRead {
		readStatus = " [unread]"
	}
	title, body := notifications.MessageText(n)
	lines := []string{fmt.Sprintf("[%s]%s %s · %s", n.Type, readStatus, title, social.FormatRelativeTime(n.Timestamp))}
	for _, line := range strings.Split(body, "\n") {
		if line != "" {
			lines = append(lines, "  "+line)
		}
	}
	if en, ok := n.Item.(notifications.EditNotification); ok {
		lines = append(lines, "  item: "+en.CanonicalRepoURL+"#commit:"+en.CanonicalHash)
	}
	if rn, ok := n.Item.(review.ReviewNotification); ok && n.Condition {
		lines = append(lines, "  "+text.SafeLine(rn.Content, 0))
	}
	// A condition has no read state, so it has no id for read and unread.
	if !n.Condition {
		lines = append(lines, "  id: "+notificationID(n))
	}
	return strings.Join(lines, "\n")
}

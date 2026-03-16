// notifications.go - CLI commands for viewing and managing notifications
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/gitsocial-org/gitsocial/core/notifications"
	"github.com/gitsocial-org/gitsocial/core/protocol"
	"github.com/gitsocial-org/gitsocial/extensions/pm"
	"github.com/gitsocial-org/gitsocial/extensions/release"
	"github.com/gitsocial-org/gitsocial/extensions/review"
	"github.com/gitsocial-org/gitsocial/extensions/social"
)

// newNotificationsCmd creates the parent command for viewing and managing notifications.
func newNotificationsCmd() *cobra.Command {
	var all bool
	var limit int
	var typeFilter string

	cmd := &cobra.Command{
		Use:   "notifications",
		Short: "View and manage notifications",
		Long: `View interactions (comments, reposts, quotes) on your posts from other users.

By default shows unread notifications only. Use --all to see all notifications.
Use --type to filter by notification type (comma-separated).

Valid types: mention, comment, repost, quote, follow, fork-pr, feedback,
approved, changes-requested, issue-assigned, new-release`,
		Run: func(cmd *cobra.Command, args []string) {
			if !EnsureGitRepo(cmd) {
				os.Exit(ExitNotRepo)
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
				PrintError(cmd, "failed to get notifications: "+err.Error())
				os.Exit(ExitError)
			}

			if cfg.JSONOutput {
				PrintJSON(items)
				return
			}

			if len(items) == 0 {
				if all {
					fmt.Println("No notifications.")
				} else {
					fmt.Println("No unread notifications.")
				}
				return
			}

			fmt.Println(formatNotifications(items))
		},
	}

	cmd.Flags().BoolVarP(&all, "all", "a", false, "Show all notifications (not just unread)")
	cmd.Flags().IntVarP(&limit, "limit", "n", 20, "Maximum number of notifications")
	cmd.Flags().StringVarP(&typeFilter, "type", "t", "", "Filter by type (comma-separated)")

	cmd.AddCommand(
		newNotificationsCountCmd(),
		newNotificationsReadCmd(),
		newNotificationsReadAllCmd(),
		newNotificationsUnreadCmd(),
		newNotificationsUnreadAllCmd(),
	)

	return cmd
}

// newNotificationsCountCmd creates the command to show unread notification count.
func newNotificationsCountCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "count",
		Short: "Show unread notification count",
		Run: func(cmd *cobra.Command, args []string) {
			if !EnsureGitRepo(cmd) {
				os.Exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			count, err := notifications.GetUnreadCount(cfg.WorkDir)
			if err != nil {
				PrintError(cmd, "failed to get count: "+err.Error())
				os.Exit(ExitError)
			}

			if cfg.JSONOutput {
				PrintJSON(map[string]int{"unread": count})
			} else {
				fmt.Printf("%d unread\n", count)
			}
		},
	}
}

// newNotificationsReadCmd creates the command to mark a notification as read.
func newNotificationsReadCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "read <notification-id>",
		Short: "Mark a notification as read",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			if !EnsureGitRepo(cmd) {
				os.Exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			id := args[0]

			var repoURL, hash, branch string
			if strings.HasSuffix(id, "#follow") {
				repoURL = strings.TrimSuffix(id, "#follow")
				hash = "follow"
				branch = ""
			} else {
				ref := protocol.ParseRef(id)
				if ref.Type != "commit" {
					PrintError(cmd, "invalid notification ID")
					os.Exit(ExitInvalidArgs)
				}
				repoURL = ref.Repository
				hash = ref.Value
				branch = ref.Branch
				if branch == "" {
					branch = "main"
				}
			}

			if err := notifications.MarkAsRead(repoURL, hash, branch); err != nil {
				PrintError(cmd, "failed to mark as read: "+err.Error())
				os.Exit(ExitError)
			}

			if cfg.JSONOutput {
				PrintJSON(map[string]string{"status": "read", "id": id})
			} else {
				PrintSuccess(cmd, "Marked as read")
			}
		},
	}
}

// newNotificationsReadAllCmd creates the command to mark all notifications as read.
func newNotificationsReadAllCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "read-all",
		Short: "Mark all notifications as read",
		Run: func(cmd *cobra.Command, args []string) {
			if !EnsureGitRepo(cmd) {
				os.Exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			if err := notifications.MarkAllAsRead(cfg.WorkDir); err != nil {
				PrintError(cmd, "failed to mark all as read: "+err.Error())
				os.Exit(ExitError)
			}

			if cfg.JSONOutput {
				PrintJSON(map[string]string{"status": "all_read"})
			} else {
				PrintSuccess(cmd, "All notifications marked as read")
			}
		},
	}
}

// newNotificationsUnreadCmd creates the command to mark a notification as unread.
func newNotificationsUnreadCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "unread <notification-id>",
		Short: "Mark a notification as unread",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			if !EnsureGitRepo(cmd) {
				os.Exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			id := args[0]

			var repoURL, hash, branch string
			if strings.HasSuffix(id, "#follow") {
				repoURL = strings.TrimSuffix(id, "#follow")
				hash = "follow"
				branch = ""
			} else {
				ref := protocol.ParseRef(id)
				if ref.Type != "commit" {
					PrintError(cmd, "invalid notification ID")
					os.Exit(ExitInvalidArgs)
				}
				repoURL = ref.Repository
				hash = ref.Value
				branch = ref.Branch
				if branch == "" {
					branch = "main"
				}
			}

			if err := notifications.MarkAsUnread(repoURL, hash, branch); err != nil {
				PrintError(cmd, "failed to mark as unread: "+err.Error())
				os.Exit(ExitError)
			}

			if cfg.JSONOutput {
				PrintJSON(map[string]string{"status": "unread", "id": id})
			} else {
				PrintSuccess(cmd, "Marked as unread")
			}
		},
	}
}

// newNotificationsUnreadAllCmd creates the command to mark all notifications as unread.
func newNotificationsUnreadAllCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "unread-all",
		Short: "Mark all notifications as unread",
		Run: func(cmd *cobra.Command, args []string) {
			if !EnsureGitRepo(cmd) {
				os.Exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			if err := notifications.MarkAllAsUnread(cfg.WorkDir); err != nil {
				PrintError(cmd, "failed to mark all as unread: "+err.Error())
				os.Exit(ExitError)
			}

			if cfg.JSONOutput {
				PrintJSON(map[string]string{"status": "all_unread"})
			} else {
				PrintSuccess(cmd, "All notifications marked as unread")
			}
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

// formatNotification formats a single notification for display.
func formatNotification(n notifications.Notification) string {
	var lines []string

	typeLabel := n.Type
	readStatus := ""
	if !n.IsRead {
		readStatus = " [unread]"
	}

	header := fmt.Sprintf("[%s]%s %s · %s", typeLabel, readStatus, n.Actor.Name, social.FormatRelativeTime(n.Timestamp))
	lines = append(lines, header)

	switch n.Type {
	case "follow":
		lines = append(lines, fmt.Sprintf("  %s added you to their list", n.ActorRepo))
	case "mention":
		lines = append(lines, fmt.Sprintf("  mentioned you in %s", n.ActorRepo))
		id := n.RepoURL + "#commit:" + n.Hash
		lines = append(lines, fmt.Sprintf("  %s", id))
	case "fork-pr":
		if rn, ok := n.Item.(review.ReviewNotification); ok {
			if rn.PRSubject != "" {
				lines = append(lines, fmt.Sprintf("  PR: %s", rn.PRSubject))
			}
			lines = append(lines, fmt.Sprintf("  %s", rn.ID))
		}
	case "feedback", "approved", "changes-requested":
		if rn, ok := n.Item.(review.ReviewNotification); ok {
			if rn.Content != "" {
				content := strings.TrimSpace(rn.Content)
				if len(content) > 150 {
					content = content[:150] + "..."
				}
				lines = append(lines, content)
			}
			lines = append(lines, fmt.Sprintf("  %s", rn.ID))
		}
	case "issue-assigned":
		if pn, ok := n.Item.(pm.PMNotification); ok {
			if pn.Subject != "" {
				lines = append(lines, fmt.Sprintf("  Issue: %s [%s]", pn.Subject, pn.State))
			}
			lines = append(lines, fmt.Sprintf("  %s", pn.ID))
		}
	case "new-release":
		if rn, ok := n.Item.(release.ReleaseNotification); ok {
			label := rn.Version
			if label == "" {
				label = rn.Tag
			}
			if rn.Prerelease {
				label += " (pre-release)"
			}
			if label != "" {
				lines = append(lines, fmt.Sprintf("  Release: %s", label))
			}
			if rn.Subject != "" {
				subject := rn.Subject
				if len(subject) > 150 {
					subject = subject[:150] + "..."
				}
				lines = append(lines, fmt.Sprintf("  %s", subject))
			}
			lines = append(lines, fmt.Sprintf("  %s", rn.ID))
		}
	default:
		if sn, ok := n.Item.(social.Notification); ok && sn.Item != nil {
			content := strings.TrimSpace(sn.Item.Content)
			if len(content) > 150 {
				content = content[:150] + "..."
			}
			if content != "" {
				lines = append(lines, content)
			}
			lines = append(lines, fmt.Sprintf("  %s", sn.ID))
		}
	}

	return strings.Join(lines, "\n")
}

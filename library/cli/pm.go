// pm.go - CLI commands for the PM extension
package main

import (
	"bufio"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/gitsocial-org/gitsocial/core/cache"
	"github.com/gitsocial-org/gitsocial/core/protocol"
	"github.com/gitsocial-org/gitsocial/extensions/pm"
)

const pmExt = "pm"

// warnIfFeatureHidden prints a warning if the feature is hidden by the current framework.
func warnIfFeatureHidden(workdir string, feature string) {
	hasMilestones, hasSprints := pm.FrameworkFeatures(workdir)
	config := pm.GetPMConfig(workdir)
	switch feature {
	case "milestone":
		if !hasMilestones {
			fmt.Fprintf(os.Stderr, "warning: milestones are not part of the '%s' framework; consider switching to 'kanban' or 'scrum'\n", config.Framework)
		}
	case "sprint":
		if !hasSprints {
			fmt.Fprintf(os.Stderr, "warning: sprints are not part of the '%s' framework; consider switching to 'scrum'\n", config.Framework)
		}
	}
}

func init() {
	RegisterExtension(ExtensionRegistration{
		Use:   "pm",
		Short: "Project management (issues, milestones, sprints)",
		Register: func(cmd *cobra.Command) {
			cmd.AddCommand(
				newPMStatusCmd(),
				newPMInitCmd(),
				NewExtConfigCmd(pmExt),
				newPMIssueCmd(),
				newPMMilestoneCmd(),
				newPMSprintCmd(),
				newPMBoardCmd(),
			)
		},
	})
}

// newPMStatusCmd creates the command to show PM extension status.
func newPMStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show PM extension status",
		Run: func(cmd *cobra.Command, args []string) {
			if !EnsureGitRepo(cmd) {
				os.Exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			if err := pm.SyncWorkspaceToCache(cfg.WorkDir); err != nil {
				slog.Debug("sync workspace", "ext", "pm", "error", err)
			}
			pmConfig := pm.GetPMConfig(cfg.WorkDir)

			branch := pmConfig.Branch
			if branch == "" {
				branch = "(not configured)"
			}
			framework := pmConfig.Framework
			if framework == "" {
				framework = "(not configured)"
			}

			// Get issue counts
			openCount, _ := pm.CountIssues([]string{"open"})
			closedCount, _ := pm.CountIssues([]string{"closed"})

			if cfg.JSONOutput {
				PrintJSON(map[string]interface{}{
					"branch":        branch,
					"framework":     framework,
					"open_issues":   openCount,
					"closed_issues": closedCount,
				})
			} else {
				fmt.Println("PM:")
				fmt.Printf("  Branch: %s\n", branch)
				fmt.Printf("  Framework: %s\n", framework)
				fmt.Printf("  Issues: %d open, %d closed\n", openCount, closedCount)
			}
		},
	}
}

// newPMInitCmd creates the command to initialize GitPM in a repository.
func newPMInitCmd() *cobra.Command {
	var branch string
	var framework string

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize GitPM in the current repository",
		Run: func(cmd *cobra.Command, args []string) {
			if !EnsureGitRepo(cmd) {
				os.Exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)

			if branch == "" {
				branch = "gitmsg/pm"
			}
			if framework == "" {
				framework = "kanban"
			}

			// Validate framework
			if pm.GetFramework(framework) == nil {
				PrintError(cmd, fmt.Sprintf("unknown framework: %s (available: %s)", framework, strings.Join(pm.ListFrameworks(), ", ")))
				os.Exit(ExitInvalidArgs)
			}

			// Save PM config with framework
			pmConfig := pm.PMConfig{
				Version:   "0.1.0",
				Branch:    branch,
				Framework: framework,
			}
			if err := pm.SavePMConfig(cfg.WorkDir, pmConfig); err != nil {
				PrintError(cmd, "failed to initialize: "+err.Error())
				os.Exit(ExitError)
			}

			if cfg.JSONOutput {
				PrintJSON(map[string]string{
					"status":    "initialized",
					"branch":    branch,
					"framework": framework,
				})
			} else {
				PrintSuccess(cmd, fmt.Sprintf("GitPM initialized (branch: %s, framework: %s)", branch, framework))
			}
		},
	}

	cmd.Flags().StringVarP(&branch, "branch", "b", "gitmsg/pm", "Branch to use for PM content")
	cmd.Flags().StringVarP(&framework, "framework", "f", "kanban", "Framework to use (minimal, kanban, scrum)")

	return cmd
}

// newPMIssueCmd creates the parent command for issue management.
func newPMIssueCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "issue",
		Short: "Manage issues",
	}

	cmd.AddCommand(
		newPMIssueListCmd(),
		newPMIssueShowCmd(),
		newPMIssueCreateCmd(),
		newPMIssueCloseCmd(),
		newPMIssueReopenCmd(),
		newPMIssueCommentCmd(),
		newPMIssueCommentsCmd(),
	)

	return cmd
}

func newPMIssueListCmd() *cobra.Command {
	var state string
	var limit int
	var labels string
	var filter string
	var sort string
	var repoURL string
	var branch string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List issues",
		Long: `List issues with optional filtering and sorting.

Filter syntax:
  state:open              - Filter by state
  assignees:alice@x.com   - Filter by assignee
  status:backlog          - Filter by label
  priority:high           - Filter by label
  -kind:chore             - Exclude label
  due:today               - Due today
  due:overdue             - Past due
  due:week                - Due within 7 days
  "search text"           - Text search

Sort options: created, due, priority`,
		Run: func(cmd *cobra.Command, args []string) {
			cfg := GetConfig(cmd)
			if repoURL != "" {
				fetchResult := pm.FetchRepository(cfg.CacheDir, repoURL, branch)
				if !fetchResult.Success {
					PrintError(cmd, fetchResult.Error.Message)
					os.Exit(ExitError)
				}
			} else {
				if !EnsureGitRepo(cmd) {
					os.Exit(ExitNotRepo)
				}
				if err := pm.SyncWorkspaceToCache(cfg.WorkDir); err != nil {
					slog.Debug("sync workspace", "ext", "pm", "error", err)
				}
			}

			q := pm.PMQuery{
				Types:   []string{string(pm.ItemTypeIssue)},
				RepoURL: repoURL,
				Branch:  branch,
				Limit:   limit,
			}

			// Build filter from flags - filter string takes precedence, flags add to it
			if filter != "" {
				q.FilterStr = filter
			}
			if state != "" && state != "all" {
				q.States = []string{state}
			} else if state == "" && filter == "" {
				q.States = []string{"open"}
			}
			if labels != "" {
				q.Labels = strings.Split(labels, ",")
			}

			// Apply sort
			if sort != "" {
				parts := strings.Split(sort, ":")
				q.SortField = parts[0]
				if len(parts) > 1 {
					q.SortOrder = parts[1]
				}
			}

			items, err := pm.GetPMItems(q)
			if err != nil {
				PrintError(cmd, err.Error())
				os.Exit(ExitError)
			}

			if cfg.JSONOutput {
				issues := make([]pm.Issue, len(items))
				for i, item := range items {
					issues[i] = pm.PMItemToIssue(item)
				}
				PrintJSON(issues)
			} else {
				if len(items) == 0 {
					fmt.Println("No issues found")
					return
				}
				for _, item := range items {
					issue := pm.PMItemToIssue(item)
					printIssueLine(issue)
				}
			}
		},
	}

	cmd.Flags().StringVarP(&state, "state", "s", "", "Filter by state (open, closed, all)")
	cmd.Flags().IntVarP(&limit, "limit", "n", 20, "Maximum number of issues")
	cmd.Flags().StringVarP(&labels, "labels", "l", "", "Filter by labels (comma-separated)")
	cmd.Flags().StringVarP(&filter, "filter", "f", "", "Filter query (e.g., 'state:open priority:high')")
	cmd.Flags().StringVar(&sort, "sort", "", "Sort by field (created, due, priority) with optional :asc/:desc")
	cmd.Flags().StringVarP(&repoURL, "repo", "r", "", "Repository URL (default: current workspace)")
	cmd.Flags().StringVarP(&branch, "branch", "b", "", "Branch name (default: configured PM branch)")

	return cmd
}

func newPMIssueShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <issue-id>",
		Short: "Show issue details",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			if !EnsureGitRepo(cmd) {
				os.Exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			issueRef := args[0]

			item, err := pm.GetPMItemByRef(issueRef, "")
			if err != nil {
				PrintError(cmd, "issue not found")
				os.Exit(ExitError)
			}

			issue := pm.PMItemToIssue(*item)

			if cfg.JSONOutput {
				PrintJSON(issue)
			} else {
				printIssueDetails(issue)
			}
		},
	}
}

func newPMIssueCreateCmd() *cobra.Command {
	var labelsStr string
	var assigneesStr string
	var dueDateStr string
	var milestoneRef string
	var sprintRef string
	var blocksStr string
	var blockedByStr string
	var relatedStr string

	cmd := &cobra.Command{
		Use:   "create <subject>",
		Short: "Create a new issue",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			if !EnsureGitRepo(cmd) {
				os.Exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			subject := args[0]
			body := ""

			if subject == "-" {
				scanner := bufio.NewScanner(os.Stdin)
				var lines []string
				for scanner.Scan() {
					lines = append(lines, scanner.Text())
				}
				if err := scanner.Err(); err != nil {
					PrintError(cmd, "failed to read from stdin: "+err.Error())
					os.Exit(ExitError)
				}
				content := strings.Join(lines, "\n")
				parts := strings.SplitN(content, "\n\n", 2)
				subject = strings.TrimSpace(parts[0])
				if len(parts) > 1 {
					body = strings.TrimSpace(parts[1])
				}
			}

			if strings.TrimSpace(subject) == "" {
				PrintError(cmd, "issue subject cannot be empty")
				os.Exit(ExitInvalidArgs)
			}

			opts := pm.CreateIssueOptions{
				State: pm.StateOpen,
			}

			if labelsStr != "" {
				for _, l := range strings.Split(labelsStr, ",") {
					l = strings.TrimSpace(l)
					if idx := strings.Index(l, "/"); idx > 0 {
						opts.Labels = append(opts.Labels, pm.Label{Scope: l[:idx], Value: l[idx+1:]})
					} else {
						opts.Labels = append(opts.Labels, pm.Label{Value: l})
					}
				}
			}

			if assigneesStr != "" {
				opts.Assignees = strings.Split(assigneesStr, ",")
				for i := range opts.Assignees {
					opts.Assignees[i] = strings.TrimSpace(opts.Assignees[i])
				}
			}

			if dueDateStr != "" {
				t, err := time.Parse("2006-01-02", dueDateStr)
				if err != nil {
					PrintError(cmd, "invalid due date format (use YYYY-MM-DD)")
					os.Exit(ExitInvalidArgs)
				}
				opts.Due = &t
			}

			if milestoneRef != "" {
				opts.Milestone = "#commit:" + milestoneRef
			}

			if sprintRef != "" {
				opts.Sprint = "#commit:" + sprintRef
			}

			if blocksStr != "" {
				for _, r := range strings.Split(blocksStr, ",") {
					r = strings.TrimSpace(r)
					if r != "" {
						opts.Blocks = append(opts.Blocks, "#commit:"+r)
					}
				}
			}
			if blockedByStr != "" {
				for _, r := range strings.Split(blockedByStr, ",") {
					r = strings.TrimSpace(r)
					if r != "" {
						opts.BlockedBy = append(opts.BlockedBy, "#commit:"+r)
					}
				}
			}
			if relatedStr != "" {
				for _, r := range strings.Split(relatedStr, ",") {
					r = strings.TrimSpace(r)
					if r != "" {
						opts.Related = append(opts.Related, "#commit:"+r)
					}
				}
			}

			result := pm.CreateIssue(cfg.WorkDir, subject, body, opts)

			if !result.Success {
				PrintError(cmd, result.Error.Message)
				os.Exit(ExitError)
			}

			if cfg.JSONOutput {
				PrintJSON(result.Data)
			} else {
				PrintSuccess(cmd, "Issue created")
				fmt.Println()
				printIssueDetails(result.Data)
			}
		},
	}

	cmd.Flags().StringVarP(&labelsStr, "labels", "l", "", "Labels (comma-separated, e.g., kind/bug,priority/high)")
	cmd.Flags().StringVarP(&assigneesStr, "assignees", "a", "", "Assignees (comma-separated emails)")
	cmd.Flags().StringVarP(&dueDateStr, "due", "d", "", "Due date (YYYY-MM-DD)")
	cmd.Flags().StringVarP(&milestoneRef, "milestone", "m", "", "Milestone reference (commit hash)")
	cmd.Flags().StringVarP(&sprintRef, "sprint", "s", "", "Sprint reference (commit hash)")
	cmd.Flags().StringVar(&blocksStr, "blocks", "", "Issues this blocks (comma-separated hashes)")
	cmd.Flags().StringVar(&blockedByStr, "blocked-by", "", "Issues blocking this (comma-separated hashes)")
	cmd.Flags().StringVar(&relatedStr, "related", "", "Related issues (comma-separated hashes)")

	return cmd
}

func newPMIssueCloseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "close <issue-id>",
		Short: "Close an issue",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			if !EnsureGitRepo(cmd) {
				os.Exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			issueRef := args[0]

			result := pm.CloseIssue(cfg.WorkDir, issueRef)

			if !result.Success {
				PrintError(cmd, result.Error.Message)
				os.Exit(ExitError)
			}

			if cfg.JSONOutput {
				PrintJSON(result.Data)
			} else {
				PrintSuccess(cmd, "Issue closed")
			}
		},
	}
}

func newPMIssueReopenCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "reopen <issue-id>",
		Short: "Reopen an issue",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			if !EnsureGitRepo(cmd) {
				os.Exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			issueRef := args[0]

			result := pm.ReopenIssue(cfg.WorkDir, issueRef)

			if !result.Success {
				PrintError(cmd, result.Error.Message)
				os.Exit(ExitError)
			}

			if cfg.JSONOutput {
				PrintJSON(result.Data)
			} else {
				PrintSuccess(cmd, "Issue reopened")
			}
		},
	}
}

func newPMIssueCommentCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "comment <issue-id> <message>",
		Short: "Add a comment to an issue",
		Args:  cobra.MinimumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			if !EnsureGitRepo(cmd) {
				os.Exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			issueRef := args[0]

			var content string
			if len(args) > 1 {
				content = strings.Join(args[1:], " ")
			} else {
				scanner := bufio.NewScanner(os.Stdin)
				var lines []string
				for scanner.Scan() {
					lines = append(lines, scanner.Text())
				}
				if err := scanner.Err(); err != nil {
					PrintError(cmd, "failed to read from stdin: "+err.Error())
					os.Exit(ExitError)
				}
				content = strings.Join(lines, "\n")
			}

			if strings.TrimSpace(content) == "" {
				PrintError(cmd, "comment content cannot be empty")
				os.Exit(ExitInvalidArgs)
			}

			result := pm.CommentOnItem(cfg.WorkDir, issueRef, content)

			if !result.Success {
				PrintError(cmd, result.Error.Message)
				os.Exit(ExitError)
			}

			if cfg.JSONOutput {
				PrintJSON(result.Data)
			} else {
				PrintSuccess(cmd, "Comment added")
			}
		},
	}
}

func newPMIssueCommentsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "comments <issue-id>",
		Short: "List comments on an issue",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			if !EnsureGitRepo(cmd) {
				os.Exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			issueRef := args[0]

			result := pm.GetItemComments(issueRef, "")

			if !result.Success {
				PrintError(cmd, result.Error.Message)
				os.Exit(ExitError)
			}

			if cfg.JSONOutput {
				PrintJSON(result.Data)
			} else {
				if len(result.Data) == 0 {
					fmt.Println("No comments")
					return
				}
				for _, comment := range result.Data {
					fmt.Printf("%s %s <%s>\n", comment.Timestamp.Format("2006-01-02 15:04"), comment.Author.Name, comment.Author.Email)
					fmt.Println(comment.Content)
					fmt.Println()
				}
			}
		},
	}
}

// newPMMilestoneCmd creates the parent command for milestone management.
func newPMMilestoneCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "milestone",
		Short: "Manage milestones",
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			cfg := GetConfig(cmd)
			if cfg != nil {
				warnIfFeatureHidden(cfg.WorkDir, "milestone")
			}
		},
	}

	cmd.AddCommand(
		newPMMilestoneListCmd(),
		newPMMilestoneShowCmd(),
		newPMMilestoneCreateCmd(),
		newPMMilestoneCloseCmd(),
		newPMMilestoneReopenCmd(),
		newPMMilestoneCancelCmd(),
		newPMMilestoneDeleteCmd(),
	)

	return cmd
}

func newPMMilestoneListCmd() *cobra.Command {
	var state string
	var limit int
	var repoURL string
	var branch string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List milestones",
		Run: func(cmd *cobra.Command, args []string) {
			cfg := GetConfig(cmd)
			if repoURL != "" {
				fetchResult := pm.FetchRepository(cfg.CacheDir, repoURL, branch)
				if !fetchResult.Success {
					PrintError(cmd, fetchResult.Error.Message)
					os.Exit(ExitError)
				}
			} else {
				if !EnsureGitRepo(cmd) {
					os.Exit(ExitNotRepo)
				}
				if err := pm.SyncWorkspaceToCache(cfg.WorkDir); err != nil {
					slog.Debug("sync workspace", "ext", "pm", "error", err)
				}
			}

			var states []string
			if state == "all" {
				states = []string{string(pm.StateOpen), string(pm.StateClosed), string(pm.StateCancelled)}
			} else if state != "" {
				states = []string{state}
			} else {
				states = []string{string(pm.StateOpen)}
			}

			result := pm.GetMilestones(repoURL, branch, states, "", limit)
			if !result.Success {
				PrintError(cmd, result.Error.Message)
				os.Exit(ExitError)
			}

			if cfg.JSONOutput {
				PrintJSON(result.Data)
			} else {
				if len(result.Data) == 0 {
					fmt.Println("No milestones found")
					return
				}
				for _, m := range result.Data {
					printMilestoneLine(m)
				}
			}
		},
	}

	cmd.Flags().StringVarP(&state, "state", "s", "", "Filter by state (open, closed, canceled, all)")
	cmd.Flags().IntVarP(&limit, "limit", "n", 20, "Maximum number of milestones")
	cmd.Flags().StringVarP(&repoURL, "repo", "r", "", "Repository URL (default: current workspace)")
	cmd.Flags().StringVarP(&branch, "branch", "b", "", "Branch name (default: configured PM branch)")

	return cmd
}

func newPMMilestoneShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <milestone-id>",
		Short: "Show milestone details",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			if !EnsureGitRepo(cmd) {
				os.Exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			milestoneRef := args[0]

			result := pm.GetMilestone(milestoneRef)
			if !result.Success {
				PrintError(cmd, result.Error.Message)
				os.Exit(ExitError)
			}

			milestone := result.Data

			if cfg.JSONOutput {
				PrintJSON(milestone)
			} else {
				printMilestoneDetails(milestone)

				// Show linked issues
				issueResult := pm.GetMilestoneIssues(milestone.ID, []string{string(pm.StateOpen), string(pm.StateClosed)})
				if issueResult.Success && len(issueResult.Data) > 0 {
					fmt.Println("\nLinked Issues:")
					for _, issue := range issueResult.Data {
						printIssueLine(issue)
					}
				}
			}
		},
	}
}

func newPMMilestoneCreateCmd() *cobra.Command {
	var dueDateStr string

	cmd := &cobra.Command{
		Use:   "create <title>",
		Short: "Create a new milestone",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			if !EnsureGitRepo(cmd) {
				os.Exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			title := args[0]
			body := ""

			if title == "-" {
				scanner := bufio.NewScanner(os.Stdin)
				var lines []string
				for scanner.Scan() {
					lines = append(lines, scanner.Text())
				}
				if err := scanner.Err(); err != nil {
					PrintError(cmd, "failed to read from stdin: "+err.Error())
					os.Exit(ExitError)
				}
				content := strings.Join(lines, "\n")
				parts := strings.SplitN(content, "\n\n", 2)
				title = strings.TrimSpace(parts[0])
				if len(parts) > 1 {
					body = strings.TrimSpace(parts[1])
				}
			}

			if strings.TrimSpace(title) == "" {
				PrintError(cmd, "milestone title cannot be empty")
				os.Exit(ExitInvalidArgs)
			}

			opts := pm.CreateMilestoneOptions{}

			if dueDateStr != "" {
				t, err := time.Parse("2006-01-02", dueDateStr)
				if err != nil {
					PrintError(cmd, "invalid due date format (use YYYY-MM-DD)")
					os.Exit(ExitInvalidArgs)
				}
				opts.Due = &t
			}

			result := pm.CreateMilestone(cfg.WorkDir, title, body, opts)

			if !result.Success {
				PrintError(cmd, result.Error.Message)
				os.Exit(ExitError)
			}

			if cfg.JSONOutput {
				PrintJSON(result.Data)
			} else {
				PrintSuccess(cmd, "Milestone created")
				fmt.Println()
				printMilestoneDetails(result.Data)
			}
		},
	}

	cmd.Flags().StringVarP(&dueDateStr, "due", "d", "", "Due date (YYYY-MM-DD)")

	return cmd
}

func newPMMilestoneCloseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "close <milestone-id>",
		Short: "Close a milestone",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			if !EnsureGitRepo(cmd) {
				os.Exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			milestoneRef := args[0]

			result := pm.CloseMilestone(cfg.WorkDir, milestoneRef)

			if !result.Success {
				PrintError(cmd, result.Error.Message)
				os.Exit(ExitError)
			}

			if cfg.JSONOutput {
				PrintJSON(result.Data)
			} else {
				PrintSuccess(cmd, "Milestone closed")
			}
		},
	}
}

func newPMMilestoneReopenCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "reopen <milestone-id>",
		Short: "Reopen a milestone",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			if !EnsureGitRepo(cmd) {
				os.Exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			milestoneRef := args[0]

			result := pm.ReopenMilestone(cfg.WorkDir, milestoneRef)

			if !result.Success {
				PrintError(cmd, result.Error.Message)
				os.Exit(ExitError)
			}

			if cfg.JSONOutput {
				PrintJSON(result.Data)
			} else {
				PrintSuccess(cmd, "Milestone reopened")
			}
		},
	}
}

func newPMMilestoneCancelCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "cancel <milestone-id>",
		Short: "Cancel a milestone",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			if !EnsureGitRepo(cmd) {
				os.Exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			milestoneRef := args[0]

			result := pm.CancelMilestone(cfg.WorkDir, milestoneRef)

			if !result.Success {
				PrintError(cmd, result.Error.Message)
				os.Exit(ExitError)
			}

			if cfg.JSONOutput {
				PrintJSON(result.Data)
			} else {
				PrintSuccess(cmd, "Milestone canceled")
			}
		},
	}
}

func newPMMilestoneDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <milestone-id>",
		Short: "Delete (retract) a milestone",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			if !EnsureGitRepo(cmd) {
				os.Exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			milestoneRef := args[0]

			result := pm.RetractMilestone(cfg.WorkDir, milestoneRef)

			if !result.Success {
				PrintError(cmd, result.Error.Message)
				os.Exit(ExitError)
			}

			if cfg.JSONOutput {
				PrintJSON(map[string]bool{"deleted": true})
			} else {
				PrintSuccess(cmd, "Milestone deleted")
			}
		},
	}
}

// newPMSprintCmd creates the parent command for sprint management.
func newPMSprintCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sprint",
		Short: "Manage sprints",
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			cfg := GetConfig(cmd)
			if cfg != nil {
				warnIfFeatureHidden(cfg.WorkDir, "sprint")
			}
		},
	}

	cmd.AddCommand(
		newPMSprintListCmd(),
		newPMSprintShowCmd(),
		newPMSprintCreateCmd(),
		newPMSprintStartCmd(),
		newPMSprintCompleteCmd(),
		newPMSprintCancelCmd(),
		newPMSprintDeleteCmd(),
	)

	return cmd
}

func newPMSprintListCmd() *cobra.Command {
	var state string
	var limit int
	var repoURL string
	var branch string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List sprints",
		Run: func(cmd *cobra.Command, args []string) {
			cfg := GetConfig(cmd)
			if repoURL != "" {
				fetchResult := pm.FetchRepository(cfg.CacheDir, repoURL, branch)
				if !fetchResult.Success {
					PrintError(cmd, fetchResult.Error.Message)
					os.Exit(ExitError)
				}
			} else {
				if !EnsureGitRepo(cmd) {
					os.Exit(ExitNotRepo)
				}
				if err := pm.SyncWorkspaceToCache(cfg.WorkDir); err != nil {
					slog.Debug("sync workspace", "ext", "pm", "error", err)
				}
			}

			var states []string
			if state == "all" {
				states = []string{
					string(pm.SprintStatePlanned),
					string(pm.SprintStateActive),
					string(pm.SprintStateCompleted),
					string(pm.SprintStateCancelled),
				}
			} else if state != "" {
				states = []string{state}
			} else {
				states = []string{string(pm.SprintStatePlanned), string(pm.SprintStateActive)}
			}

			result := pm.GetSprints(repoURL, branch, states, "", limit)
			if !result.Success {
				PrintError(cmd, result.Error.Message)
				os.Exit(ExitError)
			}

			if cfg.JSONOutput {
				PrintJSON(result.Data)
			} else {
				if len(result.Data) == 0 {
					fmt.Println("No sprints found")
					return
				}
				for _, s := range result.Data {
					printSprintLine(s)
				}
			}
		},
	}

	cmd.Flags().StringVarP(&state, "state", "s", "", "Filter by state (planned, active, completed, canceled, all)")
	cmd.Flags().IntVarP(&limit, "limit", "n", 20, "Maximum number of sprints")
	cmd.Flags().StringVarP(&repoURL, "repo", "r", "", "Repository URL (default: current workspace)")
	cmd.Flags().StringVarP(&branch, "branch", "b", "", "Branch name (default: configured PM branch)")

	return cmd
}

func newPMSprintShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <sprint-id>",
		Short: "Show sprint details",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			if !EnsureGitRepo(cmd) {
				os.Exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			sprintRef := args[0]

			result := pm.GetSprint(sprintRef)
			if !result.Success {
				PrintError(cmd, result.Error.Message)
				os.Exit(ExitError)
			}

			sprint := result.Data

			if cfg.JSONOutput {
				PrintJSON(sprint)
			} else {
				printSprintDetails(sprint)

				// Show linked issues
				issueResult := pm.GetSprintIssues(sprint.ID, []string{string(pm.StateOpen), string(pm.StateClosed)})
				if issueResult.Success && len(issueResult.Data) > 0 {
					fmt.Println("\nLinked Issues:")
					for _, issue := range issueResult.Data {
						printIssueLine(issue)
					}
				}
			}
		},
	}
}

func newPMSprintCreateCmd() *cobra.Command {
	var startDateStr string
	var endDateStr string

	cmd := &cobra.Command{
		Use:   "create <title>",
		Short: "Create a new sprint",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			if !EnsureGitRepo(cmd) {
				os.Exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			title := args[0]
			body := ""

			if title == "-" {
				scanner := bufio.NewScanner(os.Stdin)
				var lines []string
				for scanner.Scan() {
					lines = append(lines, scanner.Text())
				}
				if err := scanner.Err(); err != nil {
					PrintError(cmd, "failed to read from stdin: "+err.Error())
					os.Exit(ExitError)
				}
				content := strings.Join(lines, "\n")
				parts := strings.SplitN(content, "\n\n", 2)
				title = strings.TrimSpace(parts[0])
				if len(parts) > 1 {
					body = strings.TrimSpace(parts[1])
				}
			}

			if strings.TrimSpace(title) == "" {
				PrintError(cmd, "sprint title cannot be empty")
				os.Exit(ExitInvalidArgs)
			}

			if startDateStr == "" || endDateStr == "" {
				PrintError(cmd, "start and end dates are required (use --start and --end)")
				os.Exit(ExitInvalidArgs)
			}

			start, err := time.Parse("2006-01-02", startDateStr)
			if err != nil {
				PrintError(cmd, "invalid start date format (use YYYY-MM-DD)")
				os.Exit(ExitInvalidArgs)
			}

			end, err := time.Parse("2006-01-02", endDateStr)
			if err != nil {
				PrintError(cmd, "invalid end date format (use YYYY-MM-DD)")
				os.Exit(ExitInvalidArgs)
			}

			opts := pm.CreateSprintOptions{
				Start: start,
				End:   end,
			}

			result := pm.CreateSprint(cfg.WorkDir, title, body, opts)

			if !result.Success {
				PrintError(cmd, result.Error.Message)
				os.Exit(ExitError)
			}

			if cfg.JSONOutput {
				PrintJSON(result.Data)
			} else {
				PrintSuccess(cmd, "Sprint created")
				fmt.Println()
				printSprintDetails(result.Data)
			}
		},
	}

	cmd.Flags().StringVar(&startDateStr, "start", "", "Start date (YYYY-MM-DD, required)")
	cmd.Flags().StringVar(&endDateStr, "end", "", "End date (YYYY-MM-DD, required)")

	return cmd
}

func newPMSprintStartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "start <sprint-id>",
		Short: "Start (activate) a sprint",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			if !EnsureGitRepo(cmd) {
				os.Exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			sprintRef := args[0]

			result := pm.ActivateSprint(cfg.WorkDir, sprintRef)

			if !result.Success {
				PrintError(cmd, result.Error.Message)
				os.Exit(ExitError)
			}

			if cfg.JSONOutput {
				PrintJSON(result.Data)
			} else {
				PrintSuccess(cmd, "Sprint started")
			}
		},
	}
}

func newPMSprintCompleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "complete <sprint-id>",
		Short: "Complete a sprint",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			if !EnsureGitRepo(cmd) {
				os.Exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			sprintRef := args[0]

			result := pm.CompleteSprint(cfg.WorkDir, sprintRef)

			if !result.Success {
				PrintError(cmd, result.Error.Message)
				os.Exit(ExitError)
			}

			if cfg.JSONOutput {
				PrintJSON(result.Data)
			} else {
				PrintSuccess(cmd, "Sprint completed")
			}
		},
	}
}

func newPMSprintCancelCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "cancel <sprint-id>",
		Short: "Cancel a sprint",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			if !EnsureGitRepo(cmd) {
				os.Exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			sprintRef := args[0]

			result := pm.CancelSprint(cfg.WorkDir, sprintRef)

			if !result.Success {
				PrintError(cmd, result.Error.Message)
				os.Exit(ExitError)
			}

			if cfg.JSONOutput {
				PrintJSON(result.Data)
			} else {
				PrintSuccess(cmd, "Sprint canceled")
			}
		},
	}
}

func newPMSprintDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <sprint-id>",
		Short: "Delete (retract) a sprint",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			if !EnsureGitRepo(cmd) {
				os.Exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			sprintRef := args[0]

			result := pm.RetractSprint(cfg.WorkDir, sprintRef)

			if !result.Success {
				PrintError(cmd, result.Error.Message)
				os.Exit(ExitError)
			}

			if cfg.JSONOutput {
				PrintJSON(map[string]bool{"deleted": true})
			} else {
				PrintSuccess(cmd, "Sprint deleted")
			}
		},
	}
}

func printMilestoneLine(m pm.Milestone) {
	stateIcon := "◇"
	switch m.State {
	case pm.StateClosed:
		stateIcon = "◆"
	case pm.StateCancelled:
		stateIcon = "◈"
	}

	dueStr := ""
	if m.Due != nil {
		dueStr = fmt.Sprintf(" (due: %s)", m.Due.Format("2006-01-02"))
	}

	id := m.ID
	if len(id) > 12 {
		id = id[:12]
	}

	fmt.Printf("%s %s %s%s\n", stateIcon, id, m.Title, dueStr)
}

func printMilestoneDetails(m pm.Milestone) {
	fmt.Printf("Milestone: %s\n", m.ID)
	fmt.Printf("State: %s\n", m.State)
	fmt.Printf("Title: %s\n", m.Title)
	fmt.Printf("Author: %s <%s>\n", m.Author.Name, m.Author.Email)
	fmt.Printf("Created: %s\n", m.Timestamp.Format(time.RFC3339))

	if m.Due != nil {
		fmt.Printf("Due: %s\n", m.Due.Format("2006-01-02"))
	}

	if m.Body != "" {
		fmt.Println()
		fmt.Println(m.Body)
	}
}

func printSprintLine(s pm.Sprint) {
	stateIcon := "◷"
	switch s.State {
	case pm.SprintStateActive:
		stateIcon = "▶"
	case pm.SprintStateCompleted:
		stateIcon = "■"
	case pm.SprintStateCancelled:
		stateIcon = "□"
	}

	dateRange := fmt.Sprintf("%s - %s", s.Start.Format("Jan 2"), s.End.Format("Jan 2"))

	id := s.ID
	if len(id) > 12 {
		id = id[:12]
	}

	fmt.Printf("%s %s %s (%s)\n", stateIcon, id, s.Title, dateRange)
}

func printSprintDetails(s pm.Sprint) {
	fmt.Printf("Sprint: %s\n", s.ID)
	fmt.Printf("State: %s\n", s.State)
	fmt.Printf("Title: %s\n", s.Title)
	fmt.Printf("Author: %s <%s>\n", s.Author.Name, s.Author.Email)
	fmt.Printf("Created: %s\n", s.Timestamp.Format(time.RFC3339))
	fmt.Printf("Start: %s\n", s.Start.Format("2006-01-02"))
	fmt.Printf("End: %s\n", s.End.Format("2006-01-02"))

	if s.Body != "" {
		fmt.Println()
		fmt.Println(s.Body)
	}
}

func printIssueLine(issue pm.Issue) {
	stateIcon := "○"
	if issue.State == pm.StateClosed {
		stateIcon = "●"
	}

	var labelStrs []string
	for _, l := range issue.Labels {
		if l.Scope != "" {
			labelStrs = append(labelStrs, l.Scope+"/"+l.Value)
		} else {
			labelStrs = append(labelStrs, l.Value)
		}
	}

	labelsDisplay := ""
	if len(labelStrs) > 0 {
		labelsDisplay = " [" + strings.Join(labelStrs, ", ") + "]"
	}

	fmt.Printf("%s %s %s%s\n", stateIcon, issue.ID, issue.Subject, labelsDisplay)
}

func printIssueDetails(issue pm.Issue) {
	stateDisplay := "open"
	if issue.State == pm.StateClosed {
		stateDisplay = "closed"
	}

	fmt.Printf("Issue: %s\n", issue.ID)
	fmt.Printf("State: %s\n", stateDisplay)
	fmt.Printf("Subject: %s\n", issue.Subject)
	fmt.Printf("Author: %s\n", FormatAuthorWithVerification(issue.Author.Name, issue.Author.Email, issue.Repository, protocol.ParseRef(issue.ID).Value))
	fmt.Printf("Created: %s\n", issue.Timestamp.Format(time.RFC3339))

	if len(issue.Labels) > 0 {
		var labelStrs []string
		for _, l := range issue.Labels {
			if l.Scope != "" {
				labelStrs = append(labelStrs, l.Scope+"/"+l.Value)
			} else {
				labelStrs = append(labelStrs, l.Value)
			}
		}
		fmt.Printf("Labels: %s\n", strings.Join(labelStrs, ", "))
	}

	if len(issue.Assignees) > 0 {
		fmt.Printf("Assignees: %s\n", strings.Join(issue.Assignees, ", "))
	}

	if issue.Due != nil {
		fmt.Printf("Due: %s\n", issue.Due.Format("2006-01-02"))
	}

	if len(issue.Blocks) > 0 {
		fmt.Printf("Blocks: %s\n", formatIssueRefList(issue.Blocks))
	}
	if len(issue.BlockedBy) > 0 {
		fmt.Printf("Blocked by: %s\n", formatIssueRefList(issue.BlockedBy))
	}
	if len(issue.Related) > 0 {
		fmt.Printf("Related: %s\n", formatIssueRefList(issue.Related))
	}

	ref := protocol.ParseRef(issue.ID)
	if refs, err := cache.GetTrailerRefsTo(ref.Repository, ref.Value, ref.Branch); err == nil && len(refs) > 0 {
		fmt.Printf("\nReferenced by:\n")
		for _, r := range refs {
			subject, _ := protocol.SplitSubjectBody(r.Message)
			fmt.Printf("  %s %s (%s)  %s\n", r.Hash[:12], subject, r.AuthorName, r.TrailerKey)
		}
	}

	if issue.Body != "" {
		fmt.Println()
		fmt.Println(issue.Body)
	}
}

func formatIssueRefList(refs []pm.IssueRef) string {
	parts := make([]string, len(refs))
	for i, ref := range refs {
		parts[i] = protocol.FormatShortRef(protocol.CreateRef(protocol.RefTypeCommit, ref.Hash, ref.RepoURL, ref.Branch), "")
	}
	return strings.Join(parts, ", ")
}

func newPMBoardCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "board",
		Short: "Show kanban board view of issues",
		Run: func(cmd *cobra.Command, args []string) {
			if !EnsureGitRepo(cmd) {
				os.Exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			if err := pm.SyncWorkspaceToCache(cfg.WorkDir); err != nil {
				slog.Debug("sync workspace", "ext", "pm", "error", err)
			}

			result := pm.GetBoardView(cfg.WorkDir)
			if !result.Success {
				PrintError(cmd, result.Error.Message)
				os.Exit(ExitError)
			}

			board := result.Data

			if cfg.JSONOutput {
				PrintJSON(board)
			} else {
				printBoard(board)
			}
		},
	}
}

func printBoard(board pm.BoardView) {
	// Calculate column widths
	colWidth := 30
	separator := strings.Repeat("─", colWidth)

	// Print column headers
	headers := make([]string, 0, len(board.Columns))
	for _, col := range board.Columns {
		header := fmt.Sprintf(" %s (%d)", col.Name, len(col.Issues))
		if len(header) > colWidth {
			header = header[:colWidth-1] + "…"
		}
		headers = append(headers, padRight(header, colWidth))
	}
	fmt.Println(strings.Join(headers, " │ "))
	fmt.Println(strings.Repeat(separator+" ┼ ", len(board.Columns)-1) + separator)

	// Find max issues in any column
	maxIssues := 0
	for _, col := range board.Columns {
		if len(col.Issues) > maxIssues {
			maxIssues = len(col.Issues)
		}
	}

	// Print issues row by row
	for i := 0; i < maxIssues; i++ {
		var cells []string
		for _, col := range board.Columns {
			if i < len(col.Issues) {
				issue := col.Issues[i]
				stateIcon := "○"
				if issue.State == pm.StateClosed {
					stateIcon = "●"
				}
				cell := fmt.Sprintf(" %s %s", stateIcon, issue.Subject)
				if len(cell) > colWidth {
					cell = cell[:colWidth-1] + "…"
				}
				cells = append(cells, padRight(cell, colWidth))
			} else {
				cells = append(cells, strings.Repeat(" ", colWidth))
			}
		}
		fmt.Println(strings.Join(cells, " │ "))
	}

	if maxIssues == 0 {
		fmt.Println("  (no issues)")
	}
}

func padRight(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(s))
}

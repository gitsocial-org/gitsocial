// pm.go - CLI commands for the PM extension
package main

import (
	"bufio"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/gitsocial-org/gitsocial/library/client"
	"github.com/gitsocial-org/gitsocial/library/core/cache"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
	"github.com/gitsocial-org/gitsocial/library/core/text"
	"github.com/gitsocial-org/gitsocial/library/extensions/pm"
)

const pmExt = "pm"

// warnIfFeatureHidden prints a warning if the feature is hidden by the current framework.
func warnIfFeatureHidden(out io.Writer, workdir string, feature string) {
	hasMilestones, hasSprints := pm.FrameworkFeatures(workdir)
	config := pm.GetPMConfig(workdir)
	switch feature {
	case "milestone":
		if !hasMilestones {
			fmt.Fprintf(out, "warning: milestones are not part of the '%s' framework; consider switching to 'kanban' or 'scrum'\n", config.Framework)
		}
	case "sprint":
		if !hasSprints {
			fmt.Fprintf(out, "warning: sprints are not part of the '%s' framework; consider switching to 'scrum'\n", config.Framework)
		}
	}
}

// runRootThenWarn chains the root command's persistent setup (config injection
// and cache.Open) before emitting the framework-feature warning. Cobra runs only
// the nearest PersistentPreRun, so a nested hook on the milestone/sprint groups
// would otherwise shadow the root's setup and leave the config uninitialized.
func runRootThenWarn(cmd *cobra.Command, args []string, feature string) error {
	if root := cmd.Root(); root.PersistentPreRunE != nil {
		if err := root.PersistentPreRunE(cmd, args); err != nil {
			return err
		}
	}
	if cfg := GetConfig(cmd); cfg != nil {
		warnIfFeatureHidden(cmd.ErrOrStderr(), cfg.WorkDir, feature)
	}
	return nil
}

// init registers the pm command tree.
func init() {
	RegisterExtension(ExtensionRegistration{
		Use:   "pm",
		Short: "Manage issues, milestones and sprints",
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
		RunE: func(cmd *cobra.Command, args []string) error {
			if !EnsureGitRepo(cmd) {
				return exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			if _, err := client.SyncWorkspaceLocal(cfg.WorkDir); err != nil {
				slog.Debug("sync workspace", "error", err)
			}
			pmConfig := pm.GetPMConfig(cfg.WorkDir)

			branch := pm.PMBranch
			framework := pmConfig.Framework
			if framework == "" {
				framework = "(not configured)"
			}

			// Get issue counts
			openCount, _ := pm.CountIssues([]string{"open"})
			closedCount, _ := pm.CountIssues([]string{"closed"})

			if cfg.JSONOutput {
				return PrintJSON(cmd, map[string]interface{}{
					"branch":        branch,
					"framework":     framework,
					"open_issues":   openCount,
					"closed_issues": closedCount,
				})
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), "PM:")
				fmt.Fprintf(cmd.OutOrStdout(), "  Branch: %s\n", branch)
				fmt.Fprintf(cmd.OutOrStdout(), "  Framework: %s\n", framework)
				fmt.Fprintf(cmd.OutOrStdout(), "  Issues: %d open, %d closed\n", openCount, closedCount)
			}
			return nil
		},
	}
}

// newPMInitCmd creates the command to initialize GitPM in a repository.
func newPMInitCmd() *cobra.Command {
	var framework string

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize GitPM in this repository",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !EnsureGitRepo(cmd) {
				return exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)

			branch := pm.PMBranch
			if framework == "" {
				framework = "kanban"
			}

			// Validate framework
			if pm.GetFramework(framework) == nil {
				PrintError(cmd, fmt.Sprintf("unknown framework %q: use %s", framework, strings.Join(pm.ListFrameworks(), ", ")))
				return exit(ExitInvalidArgs)
			}

			// Save PM config with framework
			pmConfig := pm.PMConfig{
				Version:   "0.1.0",
				Framework: framework,
			}
			if err := pm.SavePMConfig(cfg.WorkDir, pmConfig); err != nil {
				PrintError(cmd, "save pm config: "+err.Error())
				return exit(ExitError)
			}

			if cfg.JSONOutput {
				return PrintJSON(cmd, map[string]string{
					"status":    "initialized",
					"branch":    branch,
					"framework": framework,
				})
			} else {
				PrintSuccess(cmd, fmt.Sprintf("GitPM initialized (branch: %s, framework: %s)", branch, framework))
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&framework, "framework", "f", "kanban", "Framework to use: minimal, kanban, scrum")

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
		newPMIssueEditCmd(),
		newPMIssueCloseCmd(),
		newPMIssueReopenCmd(),
		newPMIssueAdoptCmd(),
		newPMIssueCommentCmd(),
		newPMIssueCommentsCmd(),
	)

	return cmd
}

// newPMIssueListCmd builds the command that lists issues.
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
  state:open              filter by state
  assignees:alice@x.com   filter by assignee
  status:backlog          filter by label
  priority:high           filter by label
  -kind:chore             exclude a label
  due:today               due today
  due:overdue             past due
  due:week                due within 7 days
  "search text"           text search

Sort by created, due or priority, each with :asc or :desc.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := GetConfig(cmd)
			if repoURL != "" {
				fetchResult := pm.FetchRepository(cfg.CacheDir, repoURL, branch)
				if !fetchResult.Success {
					PrintError(cmd, fetchResult.Error.Text())
					return exit(ExitError)
				}
			} else {
				if !EnsureGitRepo(cmd) {
					return exit(ExitNotRepo)
				}
				if _, err := client.SyncWorkspaceLocal(cfg.WorkDir); err != nil {
					slog.Debug("sync workspace", "error", err)
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
				return exit(ExitError)
			}

			if cfg.JSONOutput {
				issues := make([]pm.Issue, len(items))
				for i, item := range items {
					issues[i] = pm.PMItemToIssue(item)
				}
				return PrintJSON(cmd, issues)
			} else {
				if len(items) == 0 {
					fmt.Fprintln(cmd.OutOrStdout(), "No issues found")
					return nil
				}
				for _, item := range items {
					issue := pm.PMItemToIssue(item)
					printIssueLine(cmd.OutOrStdout(), issue)
				}
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&state, "state", "s", "", "Filter by state: open, closed, all")
	cmd.Flags().IntVarP(&limit, "limit", "n", 20, "Maximum number of issues")
	cmd.Flags().StringVarP(&labels, "labels", "l", "", "Filter by comma-separated labels")
	cmd.Flags().StringVarP(&filter, "filter", "f", "", "Filter query, such as state:open priority:high")
	cmd.Flags().StringVar(&sort, "sort", "", "Sort by created, due or priority, with :asc or :desc")
	cmd.Flags().StringVarP(&repoURL, "repo", "r", "", "Repository URL, default the current workspace")
	cmd.Flags().StringVarP(&branch, "branch", "b", "", "Branch name, default the configured PM branch")

	return cmd
}

// newPMIssueShowCmd builds the command that shows one issue.
func newPMIssueShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <issue-id>",
		Short: "Show issue details",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !EnsureGitRepo(cmd) {
				return exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			issueRef := args[0]

			item, err := pm.GetPMItemByRef(issueRef, "")
			if err != nil {
				PrintError(cmd, "issue not found")
				return exit(ExitError)
			}

			issue := pm.PMItemToIssue(*item)

			if cfg.JSONOutput {
				return PrintJSON(cmd, issue)
			} else {
				printIssueDetails(cmd.OutOrStdout(), issue)
			}
			return nil
		},
	}
}

// newPMIssueCreateCmd builds the command that creates an issue.
func newPMIssueCreateCmd() *cobra.Command {
	var labelsStr string
	var assigneesStr string
	var dueDateStr string
	var milestoneRef string
	var sprintRef string
	var parentRef string
	var blocksStr string
	var blockedByStr string
	var relatedStr string

	cmd := &cobra.Command{
		Use:   "create <subject>",
		Short: "Create a new issue",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !EnsureGitRepo(cmd) {
				return exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			subject := args[0]
			body := ""

			if subject == "-" {
				scanner := bufio.NewScanner(cmd.InOrStdin())
				var lines []string
				for scanner.Scan() {
					lines = append(lines, scanner.Text())
				}
				if err := scanner.Err(); err != nil {
					PrintError(cmd, "read stdin: "+err.Error())
					return exit(ExitError)
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
				return exit(ExitInvalidArgs)
			}

			opts := pm.CreateIssueOptions{
				State:  pm.StateOpen,
				Labels: parseIssueLabels(labelsStr),
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
					PrintError(cmd, "invalid --due date: use YYYY-MM-DD")
					return exit(ExitInvalidArgs)
				}
				opts.Due = &t
			}

			if milestoneRef != "" {
				opts.Milestone = "#commit:" + milestoneRef
			}

			if sprintRef != "" {
				opts.Sprint = "#commit:" + sprintRef
			}

			if parentRef != "" {
				repoURL := gitmsg.ResolveRepoURL(cfg.WorkDir)
				parent, root, err := pm.DeriveHierarchy(commitRefOrEmpty(parentRef), repoURL, "")
				if err != nil {
					PrintError(cmd, err.Error())
					return exit(ExitInvalidArgs)
				}
				opts.Parent = parent
				opts.Root = root
			}

			opts.Blocks = commitRefList(blocksStr)
			opts.BlockedBy = commitRefList(blockedByStr)
			opts.Related = commitRefList(relatedStr)

			result := pm.CreateIssue(cfg.WorkDir, subject, body, opts)

			if !result.Success {
				PrintError(cmd, result.Error.Text())
				return exit(ExitError)
			}

			if cfg.JSONOutput {
				return PrintJSON(cmd, result.Data)
			} else {
				PrintSuccess(cmd, "Issue created")
				fmt.Fprintln(cmd.OutOrStdout())
				printIssueDetails(cmd.OutOrStdout(), result.Data)
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&labelsStr, "labels", "l", "", "Comma-separated labels, such as kind/bug")
	cmd.Flags().StringVarP(&assigneesStr, "assignees", "a", "", "Comma-separated assignee emails")
	cmd.Flags().StringVarP(&dueDateStr, "due", "d", "", "Due date, YYYY-MM-DD")
	cmd.Flags().StringVarP(&milestoneRef, "milestone", "m", "", "Milestone reference, a commit hash")
	cmd.Flags().StringVarP(&sprintRef, "sprint", "s", "", "Sprint reference, a commit hash")
	cmd.Flags().StringVar(&parentRef, "parent", "", "Parent issue commit hash; makes this a sub-issue")
	cmd.Flags().StringVar(&blocksStr, "blocks", "", "Comma-separated hashes of issues this blocks")
	cmd.Flags().StringVar(&blockedByStr, "blocked-by", "", "Comma-separated hashes of blocking issues")
	cmd.Flags().StringVar(&relatedStr, "related", "", "Comma-separated hashes of related issues")

	return cmd
}

// newPMIssueEditCmd creates the command to edit an issue's metadata. Unset
// flags preserve existing values via changed-flag detection.
func newPMIssueEditCmd() *cobra.Command {
	var subject, body, state, assigneesStr, dueDateStr, labelsStr, milestoneRef, sprintRef, parentRef, blocksStr, blockedByStr, relatedStr string

	cmd := &cobra.Command{
		Use:   "edit <issue-id>",
		Short: "Edit an issue's metadata",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !EnsureGitRepo(cmd) {
				return exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			if _, err := client.SyncWorkspaceLocal(cfg.WorkDir); err != nil {
				slog.Debug("sync workspace", "error", err)
			}

			opts := pm.UpdateIssueOptions{}
			if cmd.Flags().Changed("subject") {
				opts.Subject = &subject
			}
			if cmd.Flags().Changed("body") {
				opts.Body = &body
			}
			if cmd.Flags().Changed("state") {
				s := pm.State(state)
				opts.State = &s
			}
			if cmd.Flags().Changed("assignees") {
				a := text.SplitCSV(assigneesStr)
				opts.Assignees = &a
			}
			if cmd.Flags().Changed("due") {
				if strings.TrimSpace(dueDateStr) == "" {
					PrintError(cmd, "--due cannot be cleared: pass a YYYY-MM-DD date")
					return exit(ExitInvalidArgs)
				}
				t, err := time.Parse("2006-01-02", dueDateStr)
				if err != nil {
					PrintError(cmd, "invalid --due date: use YYYY-MM-DD")
					return exit(ExitInvalidArgs)
				}
				opts.Due = &t
			}
			if cmd.Flags().Changed("labels") {
				l := parseIssueLabels(labelsStr)
				opts.Labels = &l
			}
			if cmd.Flags().Changed("milestone") {
				ref := commitRefOrEmpty(milestoneRef)
				opts.Milestone = &ref
			}
			if cmd.Flags().Changed("sprint") {
				ref := commitRefOrEmpty(sprintRef)
				opts.Sprint = &ref
			}
			if cmd.Flags().Changed("parent") {
				if strings.TrimSpace(parentRef) == "" {
					empty := ""
					opts.Parent = &empty
					opts.Root = &empty
				} else {
					repoURL := gitmsg.ResolveRepoURL(cfg.WorkDir)
					parent, root, err := pm.DeriveHierarchy(commitRefOrEmpty(parentRef), repoURL, args[0])
					if err != nil {
						PrintError(cmd, err.Error())
						return exit(ExitInvalidArgs)
					}
					opts.Parent = &parent
					opts.Root = &root
				}
			}
			if cmd.Flags().Changed("blocks") {
				r := commitRefList(blocksStr)
				opts.Blocks = &r
			}
			if cmd.Flags().Changed("blocked-by") {
				r := commitRefList(blockedByStr)
				opts.BlockedBy = &r
			}
			if cmd.Flags().Changed("related") {
				r := commitRefList(relatedStr)
				opts.Related = &r
			}

			result := pm.UpdateIssue(cfg.WorkDir, args[0], opts)
			if !result.Success {
				PrintError(cmd, result.Error.Text())
				return exit(ExitError)
			}

			if cfg.JSONOutput {
				return PrintJSON(cmd, result.Data)
			} else {
				PrintSuccess(cmd, "Issue updated")
				fmt.Fprintln(cmd.OutOrStdout())
				printIssueDetails(cmd.OutOrStdout(), result.Data)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&subject, "subject", "", "Updated subject/title")
	cmd.Flags().StringVar(&body, "body", "", "Updated body/description")
	cmd.Flags().StringVar(&state, "state", "", "State: open, closed, canceled")
	cmd.Flags().StringVarP(&assigneesStr, "assignees", "a", "", "Comma-separated assignee emails, replacing any set")
	cmd.Flags().StringVarP(&dueDateStr, "due", "d", "", "Due date, YYYY-MM-DD")
	cmd.Flags().StringVarP(&labelsStr, "labels", "l", "", "Comma-separated labels, replacing any set")
	cmd.Flags().StringVarP(&milestoneRef, "milestone", "m", "", "Milestone commit hash, empty to clear")
	cmd.Flags().StringVarP(&sprintRef, "sprint", "s", "", "Sprint commit hash, empty to clear")
	cmd.Flags().StringVar(&parentRef, "parent", "", "Parent issue commit hash, empty to clear")
	cmd.Flags().StringVar(&blocksStr, "blocks", "", "Hashes of issues this blocks, replacing any set")
	cmd.Flags().StringVar(&blockedByStr, "blocked-by", "", "Hashes of blocking issues, replacing any set")
	cmd.Flags().StringVar(&relatedStr, "related", "", "Hashes of related issues, replacing any set")

	return cmd
}

// parseIssueLabels parses a comma-separated "scope/value" (or bare "value") string into pm.Label slice.
func parseIssueLabels(labelsStr string) []pm.Label {
	var labels []pm.Label
	for _, l := range strings.Split(labelsStr, ",") {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		if idx := strings.Index(l, "/"); idx > 0 {
			labels = append(labels, pm.Label{Scope: l[:idx], Value: l[idx+1:]})
		} else {
			labels = append(labels, pm.Label{Value: l})
		}
	}
	return labels
}

// commitRefOrEmpty normalizes a bare hash into a "#commit:" ref, passing through
// existing refs and empty strings (empty clears the field on edit).
func commitRefOrEmpty(ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" || strings.Contains(ref, "#") {
		return ref
	}
	return "#commit:" + ref
}

// commitRefList parses a comma-separated hash list into "#commit:" refs.
func commitRefList(refsStr string) []string {
	var refs []string
	for _, r := range strings.Split(refsStr, ",") {
		if norm := commitRefOrEmpty(r); norm != "" {
			refs = append(refs, norm)
		}
	}
	return refs
}

// newPMIssueCloseCmd builds the command that closes an issue.
func newPMIssueCloseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "close <issue-id>",
		Short: "Close an issue",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !EnsureGitRepo(cmd) {
				return exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			issueRef := args[0]

			result := pm.CloseIssue(cfg.WorkDir, issueRef)

			if !result.Success {
				PrintError(cmd, result.Error.Text())
				return exit(ExitError)
			}

			if cfg.JSONOutput {
				return PrintJSON(cmd, result.Data)
			} else {
				PrintSuccess(cmd, "Issue closed")
			}
			return nil
		},
	}
}

// newPMIssueAdoptCmd builds the command that adopts a registered fork's issue into this repository.
func newPMIssueAdoptCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "adopt <issue-id>",
		Short: "Adopt a registered fork's issue",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !EnsureGitRepo(cmd) {
				return exit(ExitNotRepo)
			}
			cfg := GetConfig(cmd)
			result := pm.AdoptIssue(cfg.WorkDir, args[0])
			if !result.Success {
				PrintError(cmd, result.Error.Text())
				return exit(ExitError)
			}
			if cfg.JSONOutput {
				return PrintJSON(cmd, result.Data)
			}
			PrintSuccess(cmd, "Issue adopted: "+result.Data.ID)
			return nil
		},
	}
}

// newPMIssueReopenCmd builds the command that reopens an issue.
func newPMIssueReopenCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "reopen <issue-id>",
		Short: "Reopen an issue",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !EnsureGitRepo(cmd) {
				return exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			issueRef := args[0]

			result := pm.ReopenIssue(cfg.WorkDir, issueRef)

			if !result.Success {
				PrintError(cmd, result.Error.Text())
				return exit(ExitError)
			}

			if cfg.JSONOutput {
				return PrintJSON(cmd, result.Data)
			} else {
				PrintSuccess(cmd, "Issue reopened")
			}
			return nil
		},
	}
}

// newPMIssueCommentCmd builds the command that comments on an issue.
func newPMIssueCommentCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "comment <issue-id> <message>",
		Short: "Add a comment to an issue",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !EnsureGitRepo(cmd) {
				return exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			issueRef := args[0]

			var content string
			if len(args) > 1 {
				content = strings.Join(args[1:], " ")
			} else {
				scanner := bufio.NewScanner(cmd.InOrStdin())
				var lines []string
				for scanner.Scan() {
					lines = append(lines, scanner.Text())
				}
				if err := scanner.Err(); err != nil {
					PrintError(cmd, "read stdin: "+err.Error())
					return exit(ExitError)
				}
				content = strings.Join(lines, "\n")
			}

			if strings.TrimSpace(content) == "" {
				PrintError(cmd, "comment content cannot be empty")
				return exit(ExitInvalidArgs)
			}

			result := pm.CommentOnItem(cfg.WorkDir, issueRef, content)

			if !result.Success {
				PrintError(cmd, result.Error.Text())
				return exit(ExitError)
			}

			if cfg.JSONOutput {
				return PrintJSON(cmd, result.Data)
			} else {
				PrintSuccess(cmd, "Comment added")
			}
			return nil
		},
	}
}

// newPMIssueCommentsCmd builds the command that lists an issue's comments.
func newPMIssueCommentsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "comments <issue-id>",
		Short: "List comments on an issue",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !EnsureGitRepo(cmd) {
				return exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			issueRef := args[0]

			result := pm.GetItemComments(issueRef, "")

			if !result.Success {
				PrintError(cmd, result.Error.Text())
				return exit(ExitError)
			}

			if cfg.JSONOutput {
				return PrintJSON(cmd, result.Data)
			} else {
				if len(result.Data) == 0 {
					fmt.Fprintln(cmd.OutOrStdout(), "No comments")
					return nil
				}
				for _, comment := range result.Data {
					fmt.Fprintf(cmd.OutOrStdout(), "%s %s <%s>\n", comment.Timestamp.Format("2006-01-02 15:04"), comment.Author.Name, comment.Author.Email)
					fmt.Fprintln(cmd.OutOrStdout(), comment.Content)
					fmt.Fprintln(cmd.OutOrStdout())
				}
			}
			return nil
		},
	}
}

// newPMMilestoneCmd creates the parent command for milestone management.
func newPMMilestoneCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "milestone",
		Short: "Manage milestones",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return runRootThenWarn(cmd, args, "milestone")
		},
	}

	cmd.AddCommand(
		newPMMilestoneListCmd(),
		newPMMilestoneShowCmd(),
		newPMMilestoneCreateCmd(),
		newPMMilestoneEditCmd(),
		newPMMilestoneCloseCmd(),
		newPMMilestoneReopenCmd(),
		newPMMilestoneCancelCmd(),
		newPMMilestoneDeleteCmd(),
	)

	return cmd
}

// newPMMilestoneListCmd builds the command that lists milestones.
func newPMMilestoneListCmd() *cobra.Command {
	var state string
	var limit int
	var repoURL string
	var branch string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List milestones",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := GetConfig(cmd)
			if repoURL != "" {
				fetchResult := pm.FetchRepository(cfg.CacheDir, repoURL, branch)
				if !fetchResult.Success {
					PrintError(cmd, fetchResult.Error.Text())
					return exit(ExitError)
				}
			} else {
				if !EnsureGitRepo(cmd) {
					return exit(ExitNotRepo)
				}
				if _, err := client.SyncWorkspaceLocal(cfg.WorkDir); err != nil {
					slog.Debug("sync workspace", "error", err)
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
				PrintError(cmd, result.Error.Text())
				return exit(ExitError)
			}

			if cfg.JSONOutput {
				return PrintJSON(cmd, result.Data)
			} else {
				if len(result.Data) == 0 {
					fmt.Fprintln(cmd.OutOrStdout(), "No milestones found")
					return nil
				}
				for _, m := range result.Data {
					printMilestoneLine(cmd.OutOrStdout(), m)
				}
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&state, "state", "s", "", "Filter by state: open, closed, canceled, all")
	cmd.Flags().IntVarP(&limit, "limit", "n", 20, "Maximum number of milestones")
	cmd.Flags().StringVarP(&repoURL, "repo", "r", "", "Repository URL, default the current workspace")
	cmd.Flags().StringVarP(&branch, "branch", "b", "", "Branch name, default the configured PM branch")

	return cmd
}

// newPMMilestoneShowCmd builds the command that shows one milestone.
func newPMMilestoneShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <milestone-id>",
		Short: "Show milestone details",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !EnsureGitRepo(cmd) {
				return exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			milestoneRef := args[0]

			result := pm.GetMilestone(milestoneRef)
			if !result.Success {
				PrintError(cmd, result.Error.Text())
				return exit(ExitError)
			}

			milestone := result.Data

			if cfg.JSONOutput {
				return PrintJSON(cmd, milestone)
			} else {
				printMilestoneDetails(cmd.OutOrStdout(), milestone)

				// Show linked issues
				issueResult := pm.GetMilestoneIssues(milestone.ID, []string{string(pm.StateOpen), string(pm.StateClosed)})
				if issueResult.Success && len(issueResult.Data) > 0 {
					fmt.Fprintln(cmd.OutOrStdout(), "\nLinked Issues:")
					for _, issue := range issueResult.Data {
						printIssueLine(cmd.OutOrStdout(), issue)
					}
				}
			}
			return nil
		},
	}
}

// newPMMilestoneCreateCmd builds the command that creates a milestone.
func newPMMilestoneCreateCmd() *cobra.Command {
	var dueDateStr string
	var labelsStr string
	var allowDuplicate bool

	cmd := &cobra.Command{
		Use:   "create <title>",
		Short: "Create a new milestone",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !EnsureGitRepo(cmd) {
				return exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			title := args[0]
			body := ""

			if title == "-" {
				scanner := bufio.NewScanner(cmd.InOrStdin())
				var lines []string
				for scanner.Scan() {
					lines = append(lines, scanner.Text())
				}
				if err := scanner.Err(); err != nil {
					PrintError(cmd, "read stdin: "+err.Error())
					return exit(ExitError)
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
				return exit(ExitInvalidArgs)
			}

			opts := pm.CreateMilestoneOptions{AllowDuplicate: allowDuplicate, Labels: text.SplitCSV(labelsStr)}

			if dueDateStr != "" {
				t, err := time.Parse("2006-01-02", dueDateStr)
				if err != nil {
					PrintError(cmd, "invalid --due date: use YYYY-MM-DD")
					return exit(ExitInvalidArgs)
				}
				opts.Due = &t
			}

			result := pm.CreateMilestone(cfg.WorkDir, title, body, opts)

			if !result.Success {
				PrintError(cmd, result.Error.Text())
				return exit(ExitError)
			}

			if cfg.JSONOutput {
				return PrintJSON(cmd, result.Data)
			} else {
				PrintSuccess(cmd, "Milestone created")
				fmt.Fprintln(cmd.OutOrStdout())
				printMilestoneDetails(cmd.OutOrStdout(), result.Data)
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&dueDateStr, "due", "d", "", "Due date, YYYY-MM-DD")
	cmd.Flags().StringVarP(&labelsStr, "labels", "l", "", "Comma-separated labels, such as area/tui")
	cmd.Flags().BoolVar(&allowDuplicate, "allow-duplicate", false, "Allow creating a milestone with a title that already exists")

	return cmd
}

// newPMMilestoneEditCmd creates the command to edit a milestone's metadata.
func newPMMilestoneEditCmd() *cobra.Command {
	var title, body, state, dueDateStr, labelsStr string

	cmd := &cobra.Command{
		Use:   "edit <milestone-id>",
		Short: "Edit a milestone's metadata",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !EnsureGitRepo(cmd) {
				return exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			if _, err := client.SyncWorkspaceLocal(cfg.WorkDir); err != nil {
				slog.Debug("sync workspace", "error", err)
			}

			opts := pm.UpdateMilestoneOptions{}
			if cmd.Flags().Changed("title") {
				opts.Title = &title
			}
			if cmd.Flags().Changed("body") {
				opts.Body = &body
			}
			if cmd.Flags().Changed("state") {
				s := pm.State(state)
				opts.State = &s
			}
			if cmd.Flags().Changed("due") {
				if strings.TrimSpace(dueDateStr) == "" {
					PrintError(cmd, "--due cannot be cleared: pass a YYYY-MM-DD date")
					return exit(ExitInvalidArgs)
				}
				t, err := time.Parse("2006-01-02", dueDateStr)
				if err != nil {
					PrintError(cmd, "invalid --due date: use YYYY-MM-DD")
					return exit(ExitInvalidArgs)
				}
				opts.Due = &t
			}
			if cmd.Flags().Changed("labels") {
				l := text.SplitCSV(labelsStr)
				opts.Labels = &l
			}

			result := pm.UpdateMilestone(cfg.WorkDir, args[0], opts)
			if !result.Success {
				PrintError(cmd, result.Error.Text())
				return exit(ExitError)
			}

			if cfg.JSONOutput {
				return PrintJSON(cmd, result.Data)
			} else {
				PrintSuccess(cmd, "Milestone updated")
				fmt.Fprintln(cmd.OutOrStdout())
				printMilestoneDetails(cmd.OutOrStdout(), result.Data)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&title, "title", "", "Updated title")
	cmd.Flags().StringVar(&body, "body", "", "Updated description")
	cmd.Flags().StringVar(&state, "state", "", "State: open, closed, canceled")
	cmd.Flags().StringVarP(&dueDateStr, "due", "d", "", "Due date, YYYY-MM-DD")
	cmd.Flags().StringVarP(&labelsStr, "labels", "l", "", "Comma-separated labels, replacing any set")

	return cmd
}

// newPMMilestoneCloseCmd builds the command that closes a milestone.
func newPMMilestoneCloseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "close <milestone-id>",
		Short: "Close a milestone",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !EnsureGitRepo(cmd) {
				return exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			milestoneRef := args[0]

			result := pm.CloseMilestone(cfg.WorkDir, milestoneRef)

			if !result.Success {
				PrintError(cmd, result.Error.Text())
				return exit(ExitError)
			}

			if cfg.JSONOutput {
				return PrintJSON(cmd, result.Data)
			} else {
				PrintSuccess(cmd, "Milestone closed")
			}
			return nil
		},
	}
}

// newPMMilestoneReopenCmd builds the command that reopens a milestone.
func newPMMilestoneReopenCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "reopen <milestone-id>",
		Short: "Reopen a milestone",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !EnsureGitRepo(cmd) {
				return exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			milestoneRef := args[0]

			result := pm.ReopenMilestone(cfg.WorkDir, milestoneRef)

			if !result.Success {
				PrintError(cmd, result.Error.Text())
				return exit(ExitError)
			}

			if cfg.JSONOutput {
				return PrintJSON(cmd, result.Data)
			} else {
				PrintSuccess(cmd, "Milestone reopened")
			}
			return nil
		},
	}
}

// newPMMilestoneCancelCmd builds the command that cancels a milestone.
func newPMMilestoneCancelCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "cancel <milestone-id>",
		Short: "Cancel a milestone",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !EnsureGitRepo(cmd) {
				return exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			milestoneRef := args[0]

			result := pm.CancelMilestone(cfg.WorkDir, milestoneRef)

			if !result.Success {
				PrintError(cmd, result.Error.Text())
				return exit(ExitError)
			}

			if cfg.JSONOutput {
				return PrintJSON(cmd, result.Data)
			} else {
				PrintSuccess(cmd, "Milestone canceled")
			}
			return nil
		},
	}
}

// newPMMilestoneDeleteCmd builds the command that retracts a milestone.
func newPMMilestoneDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <milestone-id>",
		Short: "Delete (retract) a milestone",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !EnsureGitRepo(cmd) {
				return exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			milestoneRef := args[0]

			result := pm.RetractMilestone(cfg.WorkDir, milestoneRef)

			if !result.Success {
				PrintError(cmd, result.Error.Text())
				return exit(ExitError)
			}

			if cfg.JSONOutput {
				return PrintJSON(cmd, map[string]bool{"deleted": true})
			} else {
				PrintSuccess(cmd, "Milestone deleted")
			}
			return nil
		},
	}
}

// newPMSprintCmd creates the parent command for sprint management.
func newPMSprintCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sprint",
		Short: "Manage sprints",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return runRootThenWarn(cmd, args, "sprint")
		},
	}

	cmd.AddCommand(
		newPMSprintListCmd(),
		newPMSprintShowCmd(),
		newPMSprintCreateCmd(),
		newPMSprintEditCmd(),
		newPMSprintStartCmd(),
		newPMSprintCompleteCmd(),
		newPMSprintCancelCmd(),
		newPMSprintDeleteCmd(),
	)

	return cmd
}

// newPMSprintListCmd builds the command that lists sprints.
func newPMSprintListCmd() *cobra.Command {
	var state string
	var limit int
	var repoURL string
	var branch string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List sprints",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := GetConfig(cmd)
			if repoURL != "" {
				fetchResult := pm.FetchRepository(cfg.CacheDir, repoURL, branch)
				if !fetchResult.Success {
					PrintError(cmd, fetchResult.Error.Text())
					return exit(ExitError)
				}
			} else {
				if !EnsureGitRepo(cmd) {
					return exit(ExitNotRepo)
				}
				if _, err := client.SyncWorkspaceLocal(cfg.WorkDir); err != nil {
					slog.Debug("sync workspace", "error", err)
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
				PrintError(cmd, result.Error.Text())
				return exit(ExitError)
			}

			if cfg.JSONOutput {
				return PrintJSON(cmd, result.Data)
			} else {
				if len(result.Data) == 0 {
					fmt.Fprintln(cmd.OutOrStdout(), "No sprints found")
					return nil
				}
				for _, s := range result.Data {
					printSprintLine(cmd.OutOrStdout(), s)
				}
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&state, "state", "s", "", "Filter by state: planned, active, completed, all")
	cmd.Flags().IntVarP(&limit, "limit", "n", 20, "Maximum number of sprints")
	cmd.Flags().StringVarP(&repoURL, "repo", "r", "", "Repository URL, default the current workspace")
	cmd.Flags().StringVarP(&branch, "branch", "b", "", "Branch name, default the configured PM branch")

	return cmd
}

// newPMSprintShowCmd builds the command that shows one sprint.
func newPMSprintShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <sprint-id>",
		Short: "Show sprint details",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !EnsureGitRepo(cmd) {
				return exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			sprintRef := args[0]

			result := pm.GetSprint(sprintRef)
			if !result.Success {
				PrintError(cmd, result.Error.Text())
				return exit(ExitError)
			}

			sprint := result.Data

			if cfg.JSONOutput {
				return PrintJSON(cmd, sprint)
			} else {
				printSprintDetails(cmd.OutOrStdout(), sprint)

				// Show linked issues
				issueResult := pm.GetSprintIssues(sprint.ID, []string{string(pm.StateOpen), string(pm.StateClosed)})
				if issueResult.Success && len(issueResult.Data) > 0 {
					fmt.Fprintln(cmd.OutOrStdout(), "\nLinked Issues:")
					for _, issue := range issueResult.Data {
						printIssueLine(cmd.OutOrStdout(), issue)
					}
				}
			}
			return nil
		},
	}
}

// newPMSprintCreateCmd builds the command that creates a sprint.
func newPMSprintCreateCmd() *cobra.Command {
	var startDateStr string
	var endDateStr string
	var labelsStr string

	cmd := &cobra.Command{
		Use:   "create <title>",
		Short: "Create a new sprint",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !EnsureGitRepo(cmd) {
				return exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			title := args[0]
			body := ""

			if title == "-" {
				scanner := bufio.NewScanner(cmd.InOrStdin())
				var lines []string
				for scanner.Scan() {
					lines = append(lines, scanner.Text())
				}
				if err := scanner.Err(); err != nil {
					PrintError(cmd, "read stdin: "+err.Error())
					return exit(ExitError)
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
				return exit(ExitInvalidArgs)
			}

			if startDateStr == "" || endDateStr == "" {
				PrintError(cmd, "start and end dates are required: pass --start and --end")
				return exit(ExitInvalidArgs)
			}

			start, err := time.Parse("2006-01-02", startDateStr)
			if err != nil {
				PrintError(cmd, "invalid --start date: use YYYY-MM-DD")
				return exit(ExitInvalidArgs)
			}

			end, err := time.Parse("2006-01-02", endDateStr)
			if err != nil {
				PrintError(cmd, "invalid --end date: use YYYY-MM-DD")
				return exit(ExitInvalidArgs)
			}

			opts := pm.CreateSprintOptions{
				Start:  start,
				End:    end,
				Labels: text.SplitCSV(labelsStr),
			}

			result := pm.CreateSprint(cfg.WorkDir, title, body, opts)

			if !result.Success {
				PrintError(cmd, result.Error.Text())
				return exit(ExitError)
			}

			if cfg.JSONOutput {
				return PrintJSON(cmd, result.Data)
			} else {
				PrintSuccess(cmd, "Sprint created")
				fmt.Fprintln(cmd.OutOrStdout())
				printSprintDetails(cmd.OutOrStdout(), result.Data)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&startDateStr, "start", "", "Start date, YYYY-MM-DD, required")
	cmd.Flags().StringVar(&endDateStr, "end", "", "End date, YYYY-MM-DD, required")
	cmd.Flags().StringVarP(&labelsStr, "labels", "l", "", "Comma-separated labels, such as area/tui")

	return cmd
}

// newPMSprintEditCmd creates the command to edit a sprint's metadata.
func newPMSprintEditCmd() *cobra.Command {
	var title, body, state, startDateStr, endDateStr, labelsStr string

	cmd := &cobra.Command{
		Use:   "edit <sprint-id>",
		Short: "Edit a sprint's metadata",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !EnsureGitRepo(cmd) {
				return exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			if _, err := client.SyncWorkspaceLocal(cfg.WorkDir); err != nil {
				slog.Debug("sync workspace", "error", err)
			}

			opts := pm.UpdateSprintOptions{}
			if cmd.Flags().Changed("title") {
				opts.Title = &title
			}
			if cmd.Flags().Changed("body") {
				opts.Body = &body
			}
			if cmd.Flags().Changed("state") {
				s := pm.SprintState(state)
				opts.State = &s
			}
			if cmd.Flags().Changed("start") {
				t, err := time.Parse("2006-01-02", startDateStr)
				if err != nil {
					PrintError(cmd, "invalid --start date: use YYYY-MM-DD")
					return exit(ExitInvalidArgs)
				}
				opts.Start = &t
			}
			if cmd.Flags().Changed("end") {
				t, err := time.Parse("2006-01-02", endDateStr)
				if err != nil {
					PrintError(cmd, "invalid --end date: use YYYY-MM-DD")
					return exit(ExitInvalidArgs)
				}
				opts.End = &t
			}
			if cmd.Flags().Changed("labels") {
				l := text.SplitCSV(labelsStr)
				opts.Labels = &l
			}

			result := pm.UpdateSprint(cfg.WorkDir, args[0], opts)
			if !result.Success {
				PrintError(cmd, result.Error.Text())
				return exit(ExitError)
			}

			if cfg.JSONOutput {
				return PrintJSON(cmd, result.Data)
			} else {
				PrintSuccess(cmd, "Sprint updated")
				fmt.Fprintln(cmd.OutOrStdout())
				printSprintDetails(cmd.OutOrStdout(), result.Data)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&title, "title", "", "Updated title")
	cmd.Flags().StringVar(&body, "body", "", "Updated description")
	cmd.Flags().StringVar(&state, "state", "", "State: planned, active, completed, canceled")
	cmd.Flags().StringVar(&startDateStr, "start", "", "Start date, YYYY-MM-DD")
	cmd.Flags().StringVar(&endDateStr, "end", "", "End date, YYYY-MM-DD")
	cmd.Flags().StringVarP(&labelsStr, "labels", "l", "", "Comma-separated labels, replacing any set")

	return cmd
}

// newPMSprintStartCmd builds the command that activates a sprint.
func newPMSprintStartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "start <sprint-id>",
		Short: "Start (activate) a sprint",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !EnsureGitRepo(cmd) {
				return exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			sprintRef := args[0]

			result := pm.ActivateSprint(cfg.WorkDir, sprintRef)

			if !result.Success {
				PrintError(cmd, result.Error.Text())
				return exit(ExitError)
			}

			if cfg.JSONOutput {
				return PrintJSON(cmd, result.Data)
			} else {
				PrintSuccess(cmd, "Sprint started")
			}
			return nil
		},
	}
}

// newPMSprintCompleteCmd builds the command that completes a sprint.
func newPMSprintCompleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "complete <sprint-id>",
		Short: "Complete a sprint",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !EnsureGitRepo(cmd) {
				return exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			sprintRef := args[0]

			result := pm.CompleteSprint(cfg.WorkDir, sprintRef)

			if !result.Success {
				PrintError(cmd, result.Error.Text())
				return exit(ExitError)
			}

			if cfg.JSONOutput {
				return PrintJSON(cmd, result.Data)
			} else {
				PrintSuccess(cmd, "Sprint completed")
			}
			return nil
		},
	}
}

// newPMSprintCancelCmd builds the command that cancels a sprint.
func newPMSprintCancelCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "cancel <sprint-id>",
		Short: "Cancel a sprint",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !EnsureGitRepo(cmd) {
				return exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			sprintRef := args[0]

			result := pm.CancelSprint(cfg.WorkDir, sprintRef)

			if !result.Success {
				PrintError(cmd, result.Error.Text())
				return exit(ExitError)
			}

			if cfg.JSONOutput {
				return PrintJSON(cmd, result.Data)
			} else {
				PrintSuccess(cmd, "Sprint canceled")
			}
			return nil
		},
	}
}

// newPMSprintDeleteCmd builds the command that retracts a sprint.
func newPMSprintDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <sprint-id>",
		Short: "Delete (retract) a sprint",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !EnsureGitRepo(cmd) {
				return exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			sprintRef := args[0]

			result := pm.RetractSprint(cfg.WorkDir, sprintRef)

			if !result.Success {
				PrintError(cmd, result.Error.Text())
				return exit(ExitError)
			}

			if cfg.JSONOutput {
				return PrintJSON(cmd, map[string]bool{"deleted": true})
			} else {
				PrintSuccess(cmd, "Sprint deleted")
			}
			return nil
		},
	}
}

// printMilestoneLine prints one milestone as a list row.
func printMilestoneLine(out io.Writer, m pm.Milestone) {
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

	fmt.Fprintf(out, "%s %s %s%s\n", stateIcon, id, m.Title, dueStr)
}

// printMilestoneDetails prints a milestone's fields and body.
func printMilestoneDetails(out io.Writer, m pm.Milestone) {
	authorName, authorEmail, created := ResolveDisplayIdentity(m.Author.Name, m.Author.Email, m.Timestamp, m.Origin)
	fmt.Fprintf(out, "Milestone: %s\n", m.ID)
	fmt.Fprintf(out, "State: %s\n", m.State)
	fmt.Fprintf(out, "Title: %s\n", m.Title)
	fmt.Fprintf(out, "Author: %s <%s>\n", authorName, authorEmail)
	fmt.Fprintf(out, "Created: %s\n", created.Format(time.RFC3339))

	if len(m.Labels) > 0 {
		fmt.Fprintf(out, "Labels: %s\n", strings.Join(m.Labels, ", "))
	}

	if m.Due != nil {
		fmt.Fprintf(out, "Due: %s\n", m.Due.Format("2006-01-02"))
	}

	if m.Body != "" {
		fmt.Fprintln(out)
		fmt.Fprintln(out, m.Body)
	}
}

// printSprintLine prints one sprint as a list row.
func printSprintLine(out io.Writer, s pm.Sprint) {
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

	fmt.Fprintf(out, "%s %s %s (%s)\n", stateIcon, id, s.Title, dateRange)
}

// printSprintDetails prints a sprint's fields and body.
func printSprintDetails(out io.Writer, s pm.Sprint) {
	authorName, authorEmail, created := ResolveDisplayIdentity(s.Author.Name, s.Author.Email, s.Timestamp, s.Origin)
	fmt.Fprintf(out, "Sprint: %s\n", s.ID)
	fmt.Fprintf(out, "State: %s\n", s.State)
	fmt.Fprintf(out, "Title: %s\n", s.Title)
	fmt.Fprintf(out, "Author: %s <%s>\n", authorName, authorEmail)
	fmt.Fprintf(out, "Created: %s\n", created.Format(time.RFC3339))
	fmt.Fprintf(out, "Start: %s\n", s.Start.Format("2006-01-02"))
	fmt.Fprintf(out, "End: %s\n", s.End.Format("2006-01-02"))

	if len(s.Labels) > 0 {
		fmt.Fprintf(out, "Labels: %s\n", strings.Join(s.Labels, ", "))
	}

	if s.Body != "" {
		fmt.Fprintln(out)
		fmt.Fprintln(out, s.Body)
	}
}

// printIssueLine prints one issue as a list row.
func printIssueLine(out io.Writer, issue pm.Issue) {
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

	fmt.Fprintf(out, "%s %s %s%s\n", stateIcon, issue.ID, issue.Subject, labelsDisplay)
}

// printIssueDetails prints an issue's fields and body.
func printIssueDetails(out io.Writer, issue pm.Issue) {
	stateDisplay := "open"
	if issue.State == pm.StateClosed {
		stateDisplay = "closed"
	}

	// An adopted copy names its original author, verified against the original's own commit.
	author, when, verifyRepo, verifyHash := issue.Author, issue.Timestamp, issue.Repository, protocol.ParseRef(issue.ID).Value
	if issue.OriginalAuthor != nil {
		adopted := protocol.ParseRef(issue.Adopts)
		author, verifyRepo, verifyHash = *issue.OriginalAuthor, adopted.Repository, adopted.Value
		if !issue.OriginalTime.IsZero() {
			when = issue.OriginalTime
		}
	}
	authorName, authorEmail, created := ResolveDisplayIdentity(author.Name, author.Email, when, issue.Origin)
	fmt.Fprintf(out, "Issue: %s\n", issue.ID)
	fmt.Fprintf(out, "State: %s\n", stateDisplay)
	fmt.Fprintf(out, "Subject: %s\n", issue.Subject)
	fmt.Fprintf(out, "Author: %s\n", FormatAuthorWithVerification(authorName, authorEmail, verifyRepo, verifyHash))
	fmt.Fprintf(out, "Created: %s\n", created.Format(time.RFC3339))
	if issue.Adopts != "" {
		fmt.Fprintf(out, "Adopted from: %s\n", issue.Adopts)
	}

	if len(issue.Labels) > 0 {
		var labelStrs []string
		for _, l := range issue.Labels {
			if l.Scope != "" {
				labelStrs = append(labelStrs, l.Scope+"/"+l.Value)
			} else {
				labelStrs = append(labelStrs, l.Value)
			}
		}
		fmt.Fprintf(out, "Labels: %s\n", strings.Join(labelStrs, ", "))
	}

	if len(issue.Assignees) > 0 {
		fmt.Fprintf(out, "Assignees: %s\n", strings.Join(issue.Assignees, ", "))
	}

	if issue.Due != nil {
		fmt.Fprintf(out, "Due: %s\n", issue.Due.Format("2006-01-02"))
	}

	// Parent — a direct child's root IS its parent (GITPM.md §1.7).
	if parentRef := issue.Parent; parentRef != nil {
		fmt.Fprintf(out, "Parent: %s\n", formatParentDisplay(*parentRef))
	} else if issue.Root != nil {
		fmt.Fprintf(out, "Parent: %s\n", formatParentDisplay(*issue.Root))
	}

	if len(issue.Blocks) > 0 {
		fmt.Fprintf(out, "Blocks: %s\n", formatIssueRefList(issue.Blocks))
	}
	if len(issue.BlockedBy) > 0 {
		fmt.Fprintf(out, "Blocked by: %s\n", formatIssueRefList(issue.BlockedBy))
	}
	if len(issue.Related) > 0 {
		fmt.Fprintf(out, "Related: %s\n", formatIssueRefList(issue.Related))
	}

	ref := protocol.ParseRef(issue.ID)
	if refs, err := cache.GetTrailerRefsTo(ref.Repository, ref.Value, ref.Branch); err == nil && len(refs) > 0 {
		fmt.Fprintf(out, "\nReferenced by:\n")
		for _, r := range refs {
			subject, _ := protocol.SplitSubjectBody(r.Message)
			fmt.Fprintf(out, "  %s %s (%s)  %s\n", r.Hash[:12], subject, r.AuthorName, r.TrailerKey)
		}
	}

	if childRes := pm.GetChildIssues(ref.Repository, ref.Value, issue.Branch); childRes.Success && len(childRes.Data) > 0 {
		open, closed := 0, 0
		for _, c := range childRes.Data {
			if c.State == pm.StateClosed {
				closed++
			} else {
				open++
			}
		}
		fmt.Fprintf(out, "\nSub-issues (%d open, %d closed):\n", open, closed)
		for _, c := range childRes.Data {
			icon := "○"
			if c.State == pm.StateClosed {
				icon = "●"
			}
			fmt.Fprintf(out, "  %s %s  %s\n", icon, c.Subject, protocol.FormatShortRef(c.ID, ""))
		}
	}

	if issue.Body != "" {
		fmt.Fprintln(out)
		fmt.Fprintln(out, issue.Body)
	}
}

// formatParentDisplay resolves the current subject of a parent/root issue ref,
// falling back to its short ref when unresolvable.
func formatParentDisplay(ref pm.IssueRef) string {
	refID := protocol.CreateRef(protocol.RefTypeCommit, ref.Hash, ref.RepoURL, ref.Branch)
	shortRef := protocol.FormatShortRef(refID, "")
	if item, err := pm.GetPMItem(ref.RepoURL, ref.Hash, ref.Branch); err == nil {
		if subject, _ := protocol.SplitSubjectBody(protocol.ExtractCleanContent(item.Content)); subject != "" {
			return subject + "  " + shortRef
		}
	}
	return shortRef
}

// formatIssueRefList joins issue refs into a comma-separated list of short refs.
func formatIssueRefList(refs []pm.IssueRef) string {
	parts := make([]string, len(refs))
	for i, ref := range refs {
		parts[i] = protocol.FormatShortRef(protocol.CreateRef(protocol.RefTypeCommit, ref.Hash, ref.RepoURL, ref.Branch), "")
	}
	return strings.Join(parts, ", ")
}

// newPMBoardCmd builds the command that shows the kanban board.
func newPMBoardCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "board",
		Short: "Show kanban board view of issues",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !EnsureGitRepo(cmd) {
				return exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			if _, err := client.SyncWorkspaceLocal(cfg.WorkDir); err != nil {
				slog.Debug("sync workspace", "error", err)
			}

			result := pm.GetBoardView(cfg.WorkDir)
			if !result.Success {
				PrintError(cmd, result.Error.Text())
				return exit(ExitError)
			}

			board := result.Data

			if cfg.JSONOutput {
				return PrintJSON(cmd, board)
			} else {
				printBoard(cmd.OutOrStdout(), board)
			}
			return nil
		},
	}
}

// printBoard writes the kanban board columns to out.
func printBoard(out io.Writer, board pm.BoardView) {
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
	fmt.Fprintln(out, strings.Join(headers, " │ "))
	fmt.Fprintln(out, strings.Repeat(separator+" ┼ ", len(board.Columns)-1)+separator)

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
		fmt.Fprintln(out, strings.Join(cells, " │ "))
	}

	if maxIssues == 0 {
		fmt.Fprintln(out, "  (no issues)")
	}
}

// padRight pads a string with spaces to the given width.
func padRight(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(s))
}

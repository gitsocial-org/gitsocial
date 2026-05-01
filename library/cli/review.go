// review.go - CLI commands for the review extension
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
	"github.com/gitsocial-org/gitsocial/core/git"
	"github.com/gitsocial-org/gitsocial/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/core/protocol"
	"github.com/gitsocial-org/gitsocial/extensions/review"
)

const reviewExt = "review"

func init() {
	RegisterExtension(ExtensionRegistration{
		Use:   "review",
		Short: "Code review (pull requests, feedback)",
		Register: func(cmd *cobra.Command) {
			cmd.AddCommand(
				newReviewStatusCmd(),
				newReviewInitCmd(),
				NewExtConfigCmd(reviewExt),
				newReviewPRCmd(),
				newReviewFeedbackCmd(),
				newReviewForkCmd(),
			)
		},
	})
}

// --- status ---

func newReviewStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show review extension status",
		Run: func(cmd *cobra.Command, args []string) {
			if !EnsureGitRepo(cmd) {
				os.Exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			if err := review.SyncWorkspaceToCache(cfg.WorkDir); err != nil {
				slog.Debug("sync workspace", "ext", "review", "error", err)
			}
			revConfig := review.GetReviewConfig(cfg.WorkDir)

			branch := revConfig.Branch
			if branch == "" {
				branch = "(not configured)"
			}

			count, _ := review.CountPullRequests(nil)
			openCount, _ := review.CountPullRequests([]string{"open"})

			forks := review.GetForks(cfg.WorkDir)
			if cfg.JSONOutput {
				PrintJSON(map[string]interface{}{
					"branch":        branch,
					"pull_requests": count,
					"open":          openCount,
					"forks":         len(forks),
				})
			} else {
				fmt.Println("Review:")
				fmt.Printf("  Branch: %s\n", branch)
				fmt.Printf("  Pull Requests: %d (%d open)\n", count, openCount)
				if len(forks) > 0 {
					fmt.Printf("  Forks: %d\n", len(forks))
				}
			}
		},
	}
}

// --- init ---

func newReviewInitCmd() *cobra.Command {
	var branch string

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize GitReview in the current repository",
		Run: func(cmd *cobra.Command, args []string) {
			if !EnsureGitRepo(cmd) {
				os.Exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			if branch == "" {
				branch = "gitmsg/review"
			}

			revConfig := review.ReviewConfig{
				Version: "0.1.0",
				Branch:  branch,
			}
			if err := review.SaveReviewConfig(cfg.WorkDir, revConfig); err != nil {
				PrintError(cmd, "failed to initialize: "+err.Error())
				os.Exit(ExitError)
			}

			if cfg.JSONOutput {
				PrintJSON(map[string]string{
					"status": "initialized",
					"branch": branch,
				})
			} else {
				PrintSuccess(cmd, fmt.Sprintf("GitReview initialized (branch: %s)", branch))
			}
		},
	}

	cmd.Flags().StringVarP(&branch, "branch", "b", "gitmsg/review", "Branch to use for review content")
	return cmd
}

// --- pr ---

func newReviewPRCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pr",
		Short: "Pull request management",
	}
	cmd.AddCommand(
		newReviewPRCreateCmd(),
		newReviewPRListCmd(),
		newReviewPRShowCmd(),
		newReviewPRUpdateCmd(),
		newReviewPRMergeCmd(),
		newReviewPRCloseCmd(),
		newReviewPRRetractCmd(),
		newReviewPRDiffCmd(),
		newReviewPRSyncCmd(),
		newReviewPRReadyCmd(),
		newReviewPRDraftCmd(),
		newReviewPRStackCmd(),
		newReviewPRRebaseStackCmd(),
		newReviewPRSyncStackCmd(),
	)
	return cmd
}

func newReviewPRCreateCmd() *cobra.Command {
	var base, head, dependsOnStr, closesStr, reviewersStr string
	var draft, stack, allowUnpublished bool

	cmd := &cobra.Command{
		Use:   "create <subject>",
		Short: "Create a new pull request",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			if !EnsureGitRepo(cmd) {
				os.Exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			subject := args[0]
			body := ""

			if subject == "-" {
				subject, body = readStdinSubjectBody()
			}
			if strings.TrimSpace(subject) == "" {
				PrintError(cmd, "pull request subject cannot be empty")
				os.Exit(ExitInvalidArgs)
			}

			// Auto-detect stack relationship from base branch matching
			if stack && dependsOnStr == "" && base != "" {
				if err := review.SyncWorkspaceToCache(cfg.WorkDir); err != nil {
					slog.Debug("sync workspace", "ext", "review", "error", err)
				}
				normalizedBase := protocol.EnsureBranchRef(base)
				normalizedBase = protocol.LocalizeRef(normalizedBase, gitmsg.ResolveRepoURL(cfg.WorkDir))
				matches := review.FindPRByHead(normalizedBase)
				switch len(matches) {
				case 1:
					dependsOnStr = matches[0].ID
				case 0:
					// No match — not an error, base may target trunk
				default:
					PrintError(cmd, fmt.Sprintf("--stack: %d open PRs have head=%s, use --depends-on to specify which", len(matches), base))
					os.Exit(ExitInvalidArgs)
				}
			}

			// Warn when local head has unpushed commits — those won't be in the PR.
			if head != "" && !cfg.JSONOutput {
				headParsed := protocol.ParseRef(protocol.LocalizeRef(protocol.EnsureBranchRef(head), gitmsg.ResolveRepoURL(cfg.WorkDir)))
				if headParsed.Repository == "" && headParsed.Value != "" {
					if unpushed, err := git.GetUnpushedCommits(cfg.WorkDir, headParsed.Value); err == nil && len(unpushed) > 0 {
						fmt.Printf("warning: local %s is %d commit(s) ahead of origin — those commits won't be in the PR until you push.\n",
							headParsed.Value, len(unpushed))
					}
				}
			}

			opts := review.CreatePROptions{
				Base:                 base,
				Head:                 head,
				Draft:                draft,
				AllowUnpublishedHead: allowUnpublished,
			}
			if dependsOnStr != "" {
				opts.DependsOn = splitCSV(dependsOnStr)
			}
			if closesStr != "" {
				opts.Closes = splitCSV(closesStr)
			}
			if reviewersStr != "" {
				opts.Reviewers = splitCSV(reviewersStr)
			}

			result := review.CreatePR(cfg.WorkDir, subject, body, opts)
			if !result.Success {
				PrintError(cmd, result.Error.Message)
				os.Exit(ExitError)
			}

			if cfg.JSONOutput {
				PrintJSON(result.Data)
			} else {
				PrintSuccess(cmd, "Pull request created")
				fmt.Println()
				printPRDetails(cfg.WorkDir, result.Data)
			}
		},
	}

	cmd.Flags().StringVar(&base, "base", "", "Target branch ref (e.g., #branch:main)")
	cmd.Flags().StringVar(&head, "head", "", "Source branch ref (e.g., #branch:feature)")
	cmd.Flags().StringVar(&dependsOnStr, "depends-on", "", "PR refs this depends on (comma-separated)")
	cmd.Flags().BoolVar(&stack, "stack", false, "Auto-detect depends-on from base branch matching")
	cmd.Flags().StringVar(&closesStr, "closes", "", "PM issue refs to close on merge (comma-separated)")
	cmd.Flags().StringVar(&reviewersStr, "reviewers", "", "Reviewer email addresses (comma-separated)")
	cmd.Flags().BoolVar(&draft, "draft", false, "Create as a draft pull request")
	cmd.Flags().BoolVar(&allowUnpublished, "allow-unpublished-head", false, "Allow creation when head branch is not resolvable on origin")

	return cmd
}

func newReviewPRListCmd() *cobra.Command {
	var state string
	var limit int
	var repoURL, branch string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List pull requests",
		Run: func(cmd *cobra.Command, args []string) {
			cfg := GetConfig(cmd)
			if repoURL != "" {
				fetchResult := review.FetchRepository(cfg.CacheDir, repoURL, branch)
				if !fetchResult.Success {
					PrintError(cmd, fetchResult.Error.Message)
					os.Exit(ExitError)
				}
			} else {
				if !EnsureGitRepo(cmd) {
					os.Exit(ExitNotRepo)
				}
				if err := review.SyncWorkspaceToCache(cfg.WorkDir); err != nil {
					slog.Debug("sync workspace", "ext", "review", "error", err)
				}
			}

			var states []string
			if state != "" {
				states = splitCSV(state)
			}
			var result review.Result[[]review.PullRequest]
			if repoURL == "" {
				forks := review.GetForks(cfg.WorkDir)
				workspaceURL := gitmsg.ResolveRepoURL(cfg.WorkDir)
				workspaceBranch := gitmsg.GetExtBranch(cfg.WorkDir, "review")
				result = review.GetPullRequestsWithForks(workspaceURL, workspaceBranch, forks, states, "", limit)
			} else {
				result = review.GetPullRequests(repoURL, branch, states, "", limit)
			}
			if !result.Success {
				PrintError(cmd, result.Error.Message)
				os.Exit(ExitError)
			}

			if cfg.JSONOutput {
				PrintJSON(result.Data)
			} else {
				if len(result.Data) == 0 {
					fmt.Println("No pull requests found")
					return
				}
				for _, pr := range result.Data {
					printPRLine(pr)
				}
			}
		},
	}

	cmd.Flags().StringVarP(&state, "state", "s", "open", "Filter by state (open, merged, closed)")
	cmd.Flags().IntVarP(&limit, "limit", "n", 50, "Maximum number of results")
	cmd.Flags().StringVarP(&repoURL, "repo", "r", "", "Repository URL")
	cmd.Flags().StringVarP(&branch, "branch", "b", "", "Branch name")

	return cmd
}

func newReviewPRShowCmd() *cobra.Command {
	var showVersions bool

	cmd := &cobra.Command{
		Use:   "show <pr-ref>",
		Short: "Show pull request details",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			if !EnsureGitRepo(cmd) {
				os.Exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			if err := review.SyncWorkspaceToCache(cfg.WorkDir); err != nil {
				slog.Debug("sync workspace", "ext", "review", "error", err)
			}

			result := review.GetPR(args[0])
			if !result.Success {
				PrintError(cmd, result.Error.Message)
				os.Exit(ExitError)
			}

			pr := result.Data
			pr.ReviewSummary = review.GetReviewSummary(pr.Repository, extractHash(pr.ID), pr.Branch, pr.Reviewers)

			if cfg.JSONOutput {
				data := map[string]interface{}{"pull_request": pr}
				if showVersions {
					vRes := review.GetPRVersions(pr.ID, gitmsg.ResolveRepoURL(cfg.WorkDir))
					if vRes.Success {
						data["versions"] = vRes.Data
					}
				}
				vaRes := review.GetVersionAwareReviews(cfg.WorkDir, args[0])
				if vaRes.Success && len(vaRes.Data) > 0 {
					data["version_aware_reviews"] = vaRes.Data
				}
				PrintJSON(data)
			} else {
				printPRDetails(cfg.WorkDir, pr)

				// Version-aware reviews
				vaRes := review.GetVersionAwareReviews(cfg.WorkDir, args[0])
				if vaRes.Success && len(vaRes.Data) > 0 {
					fmt.Println()
					fmt.Println("Reviews:")
					for _, r := range vaRes.Data {
						printVersionAwareReview(r)
					}
				}

				// Show feedback
				feedbackResult := review.GetFeedbackForPR(pr.Repository, extractHash(pr.ID), pr.Branch)
				if feedbackResult.Success && len(feedbackResult.Data) > 0 {
					fmt.Println()
					fmt.Println("Feedback:")
					for _, r := range feedbackResult.Data {
						printFeedbackLine(r)
					}
				}

				// Show versions
				if showVersions {
					vRes := review.GetPRVersions(pr.ID, gitmsg.ResolveRepoURL(cfg.WorkDir))
					if vRes.Success && len(vRes.Data) > 0 {
						fmt.Println()
						fmt.Println("Versions:")
						fmt.Printf("  %-4s %-10s %-14s %-14s %-12s %s\n", "#", "Label", "Base-Tip", "Head-Tip", "Author", "Date")
						for _, v := range vRes.Data {
							baseTip := v.BaseTip
							if baseTip == "" {
								baseTip = "-"
							}
							headTip := v.HeadTip
							if headTip == "" {
								headTip = "-"
							}
							fmt.Printf("  %-4d %-10s %-14s %-14s %-12s %s\n",
								v.Number, v.Label, baseTip, headTip, v.AuthorName, v.Timestamp.Format("2006-01-02"))
						}
					}
				}
			}
		},
	}

	cmd.Flags().BoolVar(&showVersions, "versions", false, "Show version history")
	return cmd
}

func newReviewPRUpdateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "update <pr-ref>",
		Short: "Update PR with current branch tips (signals new code ready for review)",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			if !EnsureGitRepo(cmd) {
				os.Exit(ExitNotRepo)
			}
			cfg := GetConfig(cmd)
			if err := review.SyncWorkspaceToCache(cfg.WorkDir); err != nil {
				slog.Debug("sync workspace", "ext", "review", "error", err)
			}
			result := review.UpdatePRTips(cfg.WorkDir, args[0])
			if !result.Success {
				PrintError(cmd, result.Error.Message)
				os.Exit(ExitError)
			}
			if cfg.JSONOutput {
				PrintJSON(result.Data)
			} else {
				PrintSuccess(cmd, "Pull request updated with current branch tips")
			}
		},
	}
}

func newReviewPRMergeCmd() *cobra.Command {
	var strategy string

	cmd := &cobra.Command{
		Use:   "merge <pr-ref>",
		Short: "Merge a pull request",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			if !EnsureGitRepo(cmd) {
				os.Exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			if err := review.SyncWorkspaceToCache(cfg.WorkDir); err != nil {
				slog.Debug("sync workspace", "ext", "review", "error", err)
			}

			result := review.MergePR(cfg.WorkDir, args[0], review.MergeStrategy(strategy))
			if !result.Success {
				PrintError(cmd, result.Error.Message)
				os.Exit(ExitError)
			}

			if cfg.JSONOutput {
				PrintJSON(result.Data)
			} else {
				PrintSuccess(cmd, "Pull request merged")
			}
		},
	}

	cmd.Flags().StringVar(&strategy, "strategy", "ff", "Merge strategy: ff, squash, rebase, merge")
	return cmd
}

func newReviewPRCloseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "close <pr-ref>",
		Short: "Close a pull request",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			if !EnsureGitRepo(cmd) {
				os.Exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			if err := review.SyncWorkspaceToCache(cfg.WorkDir); err != nil {
				slog.Debug("sync workspace", "ext", "review", "error", err)
			}

			result := review.ClosePR(cfg.WorkDir, args[0])
			if !result.Success {
				PrintError(cmd, result.Error.Message)
				os.Exit(ExitError)
			}

			if cfg.JSONOutput {
				PrintJSON(result.Data)
			} else {
				PrintSuccess(cmd, "Pull request closed")
			}
		},
	}
}

func newReviewPRRetractCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "retract <pr-ref>",
		Short: "Retract a pull request",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			if !EnsureGitRepo(cmd) {
				os.Exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			if err := review.SyncWorkspaceToCache(cfg.WorkDir); err != nil {
				slog.Debug("sync workspace", "ext", "review", "error", err)
			}

			result := review.RetractPR(cfg.WorkDir, args[0])
			if !result.Success {
				PrintError(cmd, result.Error.Message)
				os.Exit(ExitError)
			}

			if cfg.JSONOutput {
				PrintJSON(map[string]bool{"retracted": true})
			} else {
				PrintSuccess(cmd, "Pull request retracted")
			}
		},
	}
}

func newReviewPRDiffCmd() *cobra.Command {
	var from, to int

	cmd := &cobra.Command{
		Use:   "diff <pr-ref>",
		Short: "Show range-diff between PR versions",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			if !EnsureGitRepo(cmd) {
				os.Exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			if err := review.SyncWorkspaceToCache(cfg.WorkDir); err != nil {
				slog.Debug("sync workspace", "ext", "review", "error", err)
			}

			// Resolve default from/to if not set
			if !cmd.Flags().Changed("from") || !cmd.Flags().Changed("to") {
				vRes := review.GetPRVersions(args[0], gitmsg.ResolveRepoURL(cfg.WorkDir))
				if !vRes.Success {
					PrintError(cmd, vRes.Error.Message)
					os.Exit(ExitError)
				}
				n := len(vRes.Data)
				if n < 2 {
					PrintError(cmd, "need at least 2 versions for range-diff")
					os.Exit(ExitError)
				}
				if !cmd.Flags().Changed("from") {
					from = n - 2
				}
				if !cmd.Flags().Changed("to") {
					to = n - 1
				}
			}

			result := review.ComparePRVersions(cfg.WorkDir, args[0], from, to)
			if !result.Success {
				PrintError(cmd, result.Error.Message)
				os.Exit(ExitError)
			}

			if cfg.JSONOutput {
				PrintJSON(map[string]interface{}{
					"from":       from,
					"to":         to,
					"range_diff": result.Data,
				})
			} else {
				fmt.Print(result.Data)
			}
		},
	}

	cmd.Flags().IntVar(&from, "from", 0, "Source version number")
	cmd.Flags().IntVar(&to, "to", 0, "Target version number")
	return cmd
}

func newReviewPRSyncCmd() *cobra.Command {
	var strategy string

	cmd := &cobra.Command{
		Use:   "sync <pr-ref>",
		Short: "Update PR head branch with base branch changes",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			if !EnsureGitRepo(cmd) {
				os.Exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			if err := review.SyncWorkspaceToCache(cfg.WorkDir); err != nil {
				slog.Debug("sync workspace", "ext", "review", "error", err)
			}

			result := review.SyncPRBranch(cfg.WorkDir, args[0], strategy)
			if !result.Success {
				PrintError(cmd, result.Error.Message)
				os.Exit(ExitError)
			}

			if cfg.JSONOutput {
				PrintJSON(result.Data)
			} else {
				PrintSuccess(cmd, "PR branch synced with base")
			}
		},
	}

	cmd.Flags().StringVar(&strategy, "strategy", "rebase", "Sync strategy: rebase, merge")
	return cmd
}

func newReviewPRReadyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ready <pr-ref>",
		Short: "Mark a draft pull request as ready for review",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			if !EnsureGitRepo(cmd) {
				os.Exit(ExitNotRepo)
			}
			cfg := GetConfig(cmd)
			if err := review.SyncWorkspaceToCache(cfg.WorkDir); err != nil {
				slog.Debug("sync workspace", "ext", "review", "error", err)
			}
			result := review.MarkReady(cfg.WorkDir, args[0])
			if !result.Success {
				PrintError(cmd, result.Error.Message)
				os.Exit(ExitError)
			}
			if cfg.JSONOutput {
				PrintJSON(result.Data)
			} else {
				PrintSuccess(cmd, "Pull request marked as ready for review")
			}
		},
	}
}

func newReviewPRDraftCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "draft <pr-ref>",
		Short: "Convert an open pull request to draft",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			if !EnsureGitRepo(cmd) {
				os.Exit(ExitNotRepo)
			}
			cfg := GetConfig(cmd)
			if err := review.SyncWorkspaceToCache(cfg.WorkDir); err != nil {
				slog.Debug("sync workspace", "ext", "review", "error", err)
			}
			result := review.ConvertToDraft(cfg.WorkDir, args[0])
			if !result.Success {
				PrintError(cmd, result.Error.Message)
				os.Exit(ExitError)
			}
			if cfg.JSONOutput {
				PrintJSON(result.Data)
			} else {
				PrintSuccess(cmd, "Pull request converted to draft")
			}
		},
	}
}

func newReviewPRStackCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stack <pr-ref>",
		Short: "Show the full stack for a pull request",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			if !EnsureGitRepo(cmd) {
				os.Exit(ExitNotRepo)
			}
			cfg := GetConfig(cmd)
			if err := review.SyncWorkspaceToCache(cfg.WorkDir); err != nil {
				slog.Debug("sync workspace", "ext", "review", "error", err)
			}
			result := review.GetStack(args[0])
			if !result.Success {
				PrintError(cmd, result.Error.Message)
				os.Exit(ExitError)
			}
			if cfg.JSONOutput {
				PrintJSON(result.Data)
			} else {
				fmt.Printf("Stack (%d PRs):\n", len(result.Data))
				for _, entry := range result.Data {
					pr := entry.PullRequest
					icon := "  "
					switch pr.State {
					case review.PRStateMerged:
						icon = "✓ "
					case review.PRStateClosed:
						icon = "✗ "
					case review.PRStateOpen:
						if pr.IsDraft {
							icon = "◌ "
						} else {
							icon = "● "
						}
					}
					baseShort := shortenBranchRef(pr.Base)
					headShort := shortenBranchRef(pr.Head)
					fmt.Printf("  %s#%-2d %s  %s ← %s  [%s]\n",
						icon, entry.Position+1, pr.Subject, baseShort, headShort, pr.State)
				}
			}
		},
	}
}

func newReviewPRRebaseStackCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rebase-stack <pr-ref>",
		Short: "Cascade rebase all PRs above this one in the stack",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			if !EnsureGitRepo(cmd) {
				os.Exit(ExitNotRepo)
			}
			cfg := GetConfig(cmd)
			if err := review.SyncWorkspaceToCache(cfg.WorkDir); err != nil {
				slog.Debug("sync workspace", "ext", "review", "error", err)
			}
			result := review.RebaseStack(cfg.WorkDir, args[0])
			if !result.Success {
				PrintError(cmd, result.Error.Message)
				os.Exit(ExitError)
			}
			if cfg.JSONOutput {
				PrintJSON(result.Data)
			} else {
				for _, pr := range result.Data {
					fmt.Printf("  Rebased: %s (%s ← %s)\n", pr.Subject, shortenBranchRef(pr.Base), shortenBranchRef(pr.Head))
				}
				PrintSuccess(cmd, fmt.Sprintf("Rebased %d PR(s) in the stack", len(result.Data)))
			}
		},
	}
}

func newReviewPRSyncStackCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "sync-stack <pr-ref>",
		Short: "Update branch tips for all open PRs in the stack",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			if !EnsureGitRepo(cmd) {
				os.Exit(ExitNotRepo)
			}
			cfg := GetConfig(cmd)
			if err := review.SyncWorkspaceToCache(cfg.WorkDir); err != nil {
				slog.Debug("sync workspace", "ext", "review", "error", err)
			}
			result := review.SyncStackTips(cfg.WorkDir, args[0])
			if !result.Success {
				PrintError(cmd, result.Error.Message)
				os.Exit(ExitError)
			}
			if cfg.JSONOutput {
				PrintJSON(result.Data)
			} else {
				PrintSuccess(cmd, fmt.Sprintf("Updated tips for %d PR(s)", len(result.Data)))
			}
		},
	}
}

// --- feedback ---

func newReviewFeedbackCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "feedback",
		Short: "Code review feedback (approve, request changes, inline comments)",
	}
	cmd.AddCommand(
		newFeedbackApproveCmd(),
		newFeedbackRequestChangesCmd(),
		newFeedbackCommentCmd(),
	)
	return cmd
}

func newFeedbackApproveCmd() *cobra.Command {
	var message string

	cmd := &cobra.Command{
		Use:   "approve <pr-ref>",
		Short: "Approve a pull request",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			if !EnsureGitRepo(cmd) {
				os.Exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			if err := review.SyncWorkspaceToCache(cfg.WorkDir); err != nil {
				slog.Debug("sync workspace", "ext", "review", "error", err)
			}

			if message == "" {
				message = "LGTM!"
			}
			opts := review.CreateFeedbackOptions{
				PullRequest: args[0],
				ReviewState: review.ReviewStateApproved,
			}
			result := review.CreateFeedback(cfg.WorkDir, message, opts)
			if !result.Success {
				PrintError(cmd, result.Error.Message)
				os.Exit(ExitError)
			}

			if cfg.JSONOutput {
				PrintJSON(result.Data)
			} else {
				PrintSuccess(cmd, "Pull request approved")
			}
		},
	}

	cmd.Flags().StringVarP(&message, "message", "m", "", "Feedback message (default: LGTM!)")
	return cmd
}

func newFeedbackRequestChangesCmd() *cobra.Command {
	var message string

	cmd := &cobra.Command{
		Use:   "request-changes <pr-ref>",
		Short: "Request changes on a pull request",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			if !EnsureGitRepo(cmd) {
				os.Exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			if err := review.SyncWorkspaceToCache(cfg.WorkDir); err != nil {
				slog.Debug("sync workspace", "ext", "review", "error", err)
			}

			if message == "" {
				PrintError(cmd, "feedback message is required for requesting changes (use -m)")
				os.Exit(ExitInvalidArgs)
			}
			opts := review.CreateFeedbackOptions{
				PullRequest: args[0],
				ReviewState: review.ReviewStateChangesRequested,
			}
			result := review.CreateFeedback(cfg.WorkDir, message, opts)
			if !result.Success {
				PrintError(cmd, result.Error.Message)
				os.Exit(ExitError)
			}

			if cfg.JSONOutput {
				PrintJSON(result.Data)
			} else {
				PrintSuccess(cmd, "Changes requested")
			}
		},
	}

	cmd.Flags().StringVarP(&message, "message", "m", "", "Feedback message (required)")
	return cmd
}

func newFeedbackCommentCmd() *cobra.Command {
	var prRef, file, commitHash string
	var oldLine, newLine, oldLineEnd, newLineEnd int
	var suggest bool

	cmd := &cobra.Command{
		Use:   "comment <message>",
		Short: "Create an inline feedback comment",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			if !EnsureGitRepo(cmd) {
				os.Exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			if err := review.SyncWorkspaceToCache(cfg.WorkDir); err != nil {
				slog.Debug("sync workspace", "ext", "review", "error", err)
			}

			content := args[0]
			if content == "-" {
				content = readStdin()
			}
			if strings.TrimSpace(content) == "" {
				PrintError(cmd, "feedback comment cannot be empty")
				os.Exit(ExitInvalidArgs)
			}

			opts := review.CreateFeedbackOptions{
				PullRequest: prRef,
				Commit:      commitHash,
				File:        file,
				OldLine:     oldLine,
				NewLine:     newLine,
				OldLineEnd:  oldLineEnd,
				NewLineEnd:  newLineEnd,
				Suggestion:  suggest,
			}

			result := review.CreateFeedback(cfg.WorkDir, content, opts)
			if !result.Success {
				PrintError(cmd, result.Error.Message)
				os.Exit(ExitError)
			}

			if cfg.JSONOutput {
				PrintJSON(result.Data)
			} else {
				PrintSuccess(cmd, "Feedback comment created")
			}
		},
	}

	cmd.Flags().StringVar(&prRef, "pr", "", "Pull request ref (required)")
	cmd.Flags().StringVar(&file, "file", "", "File path (required for inline)")
	cmd.Flags().StringVar(&commitHash, "commit", "", "Commit hash, 12 chars (required for inline)")
	cmd.Flags().IntVar(&oldLine, "old-line", 0, "Line in old file version, 1-indexed")
	cmd.Flags().IntVar(&newLine, "new-line", 0, "Line in new file version, 1-indexed")
	cmd.Flags().IntVar(&oldLineEnd, "old-line-end", 0, "End line in old file version")
	cmd.Flags().IntVar(&newLineEnd, "new-line-end", 0, "End line in new file version")
	cmd.Flags().BoolVar(&suggest, "suggest", false, "Mark as code suggestion")

	_ = cmd.MarkFlagRequired("pr")

	return cmd
}

// --- fork ---

func newReviewForkCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "fork",
		Short: "Manage registered forks for PR discovery",
	}
	cmd.AddCommand(
		newReviewForkAddCmd(),
		newReviewForkRemoveCmd(),
		newReviewForkListCmd(),
	)
	return cmd
}

func newReviewForkAddCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "add <url>",
		Short: "Register a fork for PR discovery",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			if !EnsureGitRepo(cmd) {
				os.Exit(ExitNotRepo)
			}
			cfg := GetConfig(cmd)
			if err := review.AddFork(cfg.WorkDir, args[0]); err != nil {
				PrintError(cmd, err.Error())
				os.Exit(ExitError)
			}
			if cfg.JSONOutput {
				PrintJSON(map[string]string{"added": args[0]})
			} else {
				PrintSuccess(cmd, fmt.Sprintf("Fork added: %s", args[0]))
			}
		},
	}
}

func newReviewForkRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove <url>",
		Short: "Remove a registered fork",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			if !EnsureGitRepo(cmd) {
				os.Exit(ExitNotRepo)
			}
			cfg := GetConfig(cmd)
			if err := review.RemoveFork(cfg.WorkDir, args[0]); err != nil {
				PrintError(cmd, err.Error())
				os.Exit(ExitError)
			}
			if cfg.JSONOutput {
				PrintJSON(map[string]string{"removed": args[0]})
			} else {
				PrintSuccess(cmd, fmt.Sprintf("Fork removed: %s", args[0]))
			}
		},
	}
}

func newReviewForkListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List registered forks",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			if !EnsureGitRepo(cmd) {
				os.Exit(ExitNotRepo)
			}
			cfg := GetConfig(cmd)
			forks := review.GetForks(cfg.WorkDir)
			if cfg.JSONOutput {
				PrintJSON(forks)
			} else {
				if len(forks) == 0 {
					fmt.Println("No forks registered")
					return
				}
				for _, f := range forks {
					fmt.Println(f)
				}
			}
		},
	}
}

// --- helpers ---

func printPRLine(pr review.PullRequest) {
	icon := "⑂"
	stateStr := string(pr.State)
	if pr.IsDraft {
		stateStr = "draft"
	}
	dateStr := pr.Timestamp.Format("2006-01-02")
	baseShort := shortenBranchRef(pr.Base)
	headShort := shortenBranchRef(pr.Head)
	fmt.Printf("%s %s  %s ← %s  [%s]  %s\n", icon, pr.Subject, baseShort, headShort, stateStr, dateStr)
}

func printPRDetails(workdir string, pr review.PullRequest) {
	fmt.Printf("Pull Request: %s\n", pr.ID)
	if pr.IsDraft {
		fmt.Printf("State: %s (draft)\n", pr.State)
	} else {
		fmt.Printf("State: %s\n", pr.State)
	}
	fmt.Printf("Author: %s\n", FormatAuthorWithVerification(pr.Author.Name, pr.Author.Email, pr.Repository, protocol.ParseRef(pr.ID).Value))
	fmt.Printf("Created: %s\n", pr.Timestamp.Format(time.RFC3339))
	var observation *review.PRObservation
	if pr.State == review.PRStateOpen {
		observation = review.ObserveLivePR(workdir, pr)
	}
	if pr.Base != "" {
		fmt.Printf("Base: %s%s\n", pr.Base, formatTipStaleMarker("base", pr.BaseTip, observation))
	}
	if pr.Head != "" {
		fmt.Printf("Head: %s%s\n", pr.Head, formatTipStaleMarker("head", pr.HeadTip, observation))
	}
	// Fork info
	headParsed := protocol.ParseRef(pr.Head)
	wsURL := gitmsg.ResolveRepoURL(workdir)
	if headParsed.Repository != "" && headParsed.Repository != wsURL {
		fmt.Printf("Fork: %s\n", headParsed.Repository)
	}
	// Behind count
	if pr.State == review.PRStateOpen {
		baseName := protocol.ParseRef(pr.Base).Value
		headName := headParsed.Value
		if baseName != "" && headName != "" && (headParsed.Repository == "" || headParsed.Repository == wsURL) {
			if behind, err := git.GetBehindCount(workdir, baseName, headName); err == nil && behind > 0 {
				fmt.Printf("Behind: %d commits behind %s\n", behind, baseName)
			}
		}
	}
	if len(pr.Reviewers) > 0 {
		fmt.Printf("Reviewers: %s\n", strings.Join(pr.Reviewers, ", "))
	}
	if len(pr.DependsOn) > 0 {
		fmt.Printf("Depends on: %s\n", strings.Join(pr.DependsOn, ", "))
	}
	if len(pr.Closes) > 0 {
		fmt.Printf("Closes: %s\n", strings.Join(pr.Closes, ", "))
	}
	if pr.IsEdited {
		fmt.Println("(edited)")
	}

	// Merge/close metadata
	hash := extractHash(pr.ID)
	switch pr.State {
	case review.PRStateMerged:
		if info, err := review.GetStateChangeInfo(pr.Repository, hash, pr.Branch, review.PRStateMerged); err == nil {
			fmt.Printf("Merged by: %s <%s> on %s\n", info.AuthorName, info.AuthorEmail, info.Timestamp.Format(time.RFC3339))
		}
	case review.PRStateClosed:
		if info, err := review.GetStateChangeInfo(pr.Repository, hash, pr.Branch, review.PRStateClosed); err == nil {
			fmt.Printf("Closed by: %s <%s> on %s\n", info.AuthorName, info.AuthorEmail, info.Timestamp.Format(time.RFC3339))
		}
	}

	summary := pr.ReviewSummary
	if summary.Approved > 0 || summary.ChangesRequested > 0 || summary.Pending > 0 {
		fmt.Printf("Review: %d approved, %d changes requested, %d pending\n",
			summary.Approved, summary.ChangesRequested, summary.Pending)
	}

	ref := protocol.ParseRef(pr.ID)
	if refs, err := cache.GetTrailerRefsTo(ref.Repository, ref.Value, ref.Branch); err == nil && len(refs) > 0 {
		fmt.Printf("\nReferenced by:\n")
		for _, r := range refs {
			subject, _ := protocol.SplitSubjectBody(r.Message)
			fmt.Printf("  %s %s (%s)  %s\n", r.Hash[:12], subject, r.AuthorName, r.TrailerKey)
		}
	}

	if pr.Body != "" {
		fmt.Println()
		fmt.Println(pr.Body)
	}
}

// formatTipStaleMarker returns " ⚠ ..." text when origin's observed tip on
// the given side ("head"/"base") differs from the PR's stored tip, or when
// the branch is missing on origin. Returns empty when in sync or no
// observation has been recorded.
func formatTipStaleMarker(side, storedTip string, obs *review.PRObservation) string {
	if obs == nil {
		return ""
	}
	var exists bool
	var observedTip string
	switch side {
	case "head":
		exists, observedTip = obs.HeadExists, obs.HeadTip
	case "base":
		exists, observedTip = obs.BaseExists, obs.BaseTip
	default:
		return ""
	}
	if !exists {
		return "  ⚠ deleted on origin"
	}
	if observedTip == "" || observedTip == storedTip {
		return ""
	}
	return "  ⚠ updated to #" + observedTip + " (run `pr update`)"
}

func printVersionAwareReview(r review.VersionAwareReview) {
	icon := "  "
	switch r.State {
	case review.ReviewStateApproved:
		icon = "✓ "
	case review.ReviewStateChangesRequested:
		icon = "✗ "
	}
	reviewedLabel := r.ReviewedLabel
	if reviewedLabel == "" {
		reviewedLabel = fmt.Sprintf("v%d", r.ReviewedVersion)
	}
	currentLabel := r.CurrentLabel
	if currentLabel == "" {
		currentLabel = fmt.Sprintf("v%d", r.CurrentVersion)
	}
	status := fmt.Sprintf("%s (reviewed %s, current is %s", r.State, reviewedLabel, currentLabel)
	if r.CodeChanged {
		status += ", code changed"
	} else {
		status += ", no code changes"
	}
	status += ")"
	stale := ""
	if r.Stale {
		stale = " [stale]"
	}
	fmt.Printf("  %s%-25s %s%s\n", icon, r.ReviewerEmail, status, stale)
}

func printFeedbackLine(r review.Feedback) {
	icon := "  "
	switch r.ReviewState {
	case review.ReviewStateApproved:
		icon = "✓ "
	case review.ReviewStateChangesRequested:
		icon = "✗ "
	}

	location := ""
	if r.File != "" {
		location = fmt.Sprintf(" on %s", r.File)
		if r.NewLine > 0 {
			location += fmt.Sprintf(":%d", r.NewLine)
		} else if r.OldLine > 0 {
			location += fmt.Sprintf(":%d", r.OldLine)
		}
	}

	dateStr := r.Timestamp.Format("2006-01-02 15:04")
	fmt.Printf("%s%s%s  %s  %s\n", icon, r.Author.Name, location, dateStr, truncate(r.Content, 60))
}

func readStdinSubjectBody() (string, string) {
	scanner := bufio.NewScanner(os.Stdin)
	var lines []string
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	content := strings.Join(lines, "\n")
	parts := strings.SplitN(content, "\n\n", 2)
	subject := strings.TrimSpace(parts[0])
	body := ""
	if len(parts) > 1 {
		body = strings.TrimSpace(parts[1])
	}
	return subject, body
}

func readStdin() string {
	scanner := bufio.NewScanner(os.Stdin)
	var lines []string
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	return strings.Join(lines, "\n")
}

func splitCSV(s string) []string {
	var result []string
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

func shortenBranchRef(ref string) string {
	// Extract branch name from "#branch:name" or full URL refs
	if idx := strings.LastIndex(ref, "#branch:"); idx >= 0 {
		return ref[idx+len("#branch:"):]
	}
	return ref
}

func extractHash(id string) string {
	// Extract hash from ref like "#commit:abc123@branch" or "url#commit:abc123@branch"
	if idx := strings.Index(id, "#commit:"); idx >= 0 {
		rest := id[idx+len("#commit:"):]
		if atIdx := strings.Index(rest, "@"); atIdx > 0 {
			return rest[:atIdx]
		}
		return rest
	}
	return id
}

func truncate(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > maxLen {
		return s[:maxLen-3] + "..."
	}
	return s
}

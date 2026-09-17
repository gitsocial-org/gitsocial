// social.go - CLI commands for the social extension
package main

import (
	"bufio"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/spf13/cobra"

	"github.com/gitsocial-org/gitsocial/library/client"
	"github.com/gitsocial-org/gitsocial/library/core/cache"
	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
	"github.com/gitsocial-org/gitsocial/library/core/text"
	"github.com/gitsocial-org/gitsocial/library/extensions/social"
)

const socialExt = "social"

func init() {
	RegisterExtension(ExtensionRegistration{
		Use:   "social",
		Short: "Social features (posts, timeline, lists)",
		Register: func(cmd *cobra.Command) {
			cmd.AddCommand(
				newSocialStatusCmd(),
				newSocialInitCmd(),
				NewExtConfigCmd(socialExt),
				newSocialTimelineCmd(),
				newSocialPostCmd(),
				newSocialEditCmd(),
				newSocialRetractCmd(),
				newSocialCommentCmd(),
				newSocialRepostCmd(),
				newSocialQuoteCmd(),
				newSocialListCmd(),
				newSocialFetchCmd(),
				newSocialFollowersCmd(),
			)
		},
	})
}

// newSocialStatusCmd creates the command to show social extension status.
func newSocialStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show social extension status",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !EnsureGitRepo(cmd) {
				return exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			result := social.Status(cfg.WorkDir, cfg.CacheDir)
			if !result.Success {
				PrintError(cmd, result.Error.Text())
				return exit(ExitError)
			}

			if cfg.JSONOutput {
				return PrintJSON(cmd, result.Data)
			} else {
				printSocialStatus(cmd.OutOrStdout(), &result.Data)
			}
			return nil
		},
	}
}

// printSocialStatus writes the social extension status to out.
func printSocialStatus(out io.Writer, s *social.StatusData) {
	fmt.Fprintln(out, "Social:")
	fmt.Fprintf(out, "  Branch: %s\n", s.Branch)

	if s.Unpushed != nil && (s.Unpushed.Posts > 0 || s.Unpushed.Lists > 0) {
		var parts []string
		if s.Unpushed.Posts > 0 {
			parts = append(parts, fmt.Sprintf("%d posts", s.Unpushed.Posts))
		}
		if s.Unpushed.Lists > 0 {
			parts = append(parts, fmt.Sprintf("%d lists", s.Unpushed.Lists))
		}
		fmt.Fprintf(out, "  ⇡ Unpushed: %s\n", strings.Join(parts, ", "))
	}

	if !s.LastFetch.IsZero() {
		fmt.Fprintf(out, "  Fetched: %s\n", social.FormatRelativeTime(s.LastFetch))
	}

	if len(s.Lists) > 0 {
		fmt.Fprintf(out, "  Lists (%d):\n", len(s.Lists))
		for _, list := range s.Lists {
			fmt.Fprintf(out, "    - %s (%d repos)\n", list.ID, list.Repos)
		}
	} else {
		fmt.Fprintln(out, "  Lists: none")
	}

	fmt.Fprintf(out, "  Items: %d (%d list, %d workspace)\n", s.Items, s.FromLists, s.FromWorkspace)
}

// newSocialInitCmd creates the command to initialize GitSocial in a repository.
func newSocialInitCmd() *cobra.Command {
	var branch string

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize GitSocial in this repository",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !EnsureGitRepo(cmd) {
				return exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)

			if branch == "" {
				branch = "gitmsg/social"
			}

			if err := gitmsg.SetExtConfigValue(cfg.WorkDir, "social", "branch", branch); err != nil {
				PrintError(cmd, "save social config: "+err.Error())
				return exit(ExitError)
			}

			if cfg.JSONOutput {
				return PrintJSON(cmd, map[string]string{
					"status": "initialized",
					"branch": branch,
				})
			} else {
				PrintSuccess(cmd, "GitSocial initialized on branch: "+branch)
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&branch, "branch", "b", "", "Branch to use for social content")

	return cmd
}

// newSocialConfigCmd creates the parent command for social extension configuration.
// newSocialTimelineCmd creates the command to view posts from the timeline.
func newSocialTimelineCmd() *cobra.Command {
	var listName string
	var repoURL string
	var limit int

	cmd := &cobra.Command{
		Use:   "timeline",
		Short: "View posts from your timeline",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !EnsureGitRepo(cmd) {
				return exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			if _, err := client.SyncWorkspaceLocal(cfg.WorkDir); err != nil {
				slog.Debug("sync workspace", "error", err)
			}
			scope := "timeline"
			if repoURL != "" {
				if repoURL == "workspace" || repoURL == "my" {
					scope = "repository:workspace"
				} else {
					scope = "repository:" + repoURL
				}
			} else if listName != "" {
				scope = "list:" + listName
			}

			result := social.GetPosts(cfg.WorkDir, scope, &social.GetPostsOptions{
				Limit: limit,
			})

			if !result.Success {
				PrintError(cmd, result.Error.Text())
				return exit(ExitCode(result.Error.Code))
			}

			posts := result.Data

			if cfg.JSONOutput {
				return PrintJSON(cmd, posts)
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), social.FormatTimeline(posts))
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&repoURL, "repo", "r", "", "Filter by repository URL, or workspace")
	cmd.Flags().StringVarP(&listName, "list", "l", "", "Filter by list name")
	cmd.Flags().IntVarP(&limit, "limit", "n", 20, "Maximum number of posts")

	return cmd
}

// newSocialPostCmd creates the command to create a new post.
func newSocialPostCmd() *cobra.Command {
	var labelsStr string
	cmd := &cobra.Command{
		Use:   "post <text>",
		Short: "Create a new post",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !EnsureGitRepo(cmd) {
				return exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			content := args[0]

			if content == "-" {
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
				PrintError(cmd, "post content cannot be empty")
				return exit(ExitInvalidArgs)
			}

			result := social.CreatePost(cfg.WorkDir, content, &social.CreatePostOptions{Labels: text.SplitCSV(labelsStr)})

			if !result.Success {
				PrintError(cmd, result.Error.Text())
				return exit(ExitCode(result.Error.Code))
			}

			if cfg.JSONOutput {
				return PrintJSON(cmd, result.Data)
			} else {
				PrintSuccess(cmd, "Post created")
				fmt.Fprintln(cmd.OutOrStdout())
				fmt.Fprintln(cmd.OutOrStdout(), social.FormatPost(result.Data))
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&labelsStr, "labels", "l", "", "Comma-separated labels, such as topic/release")

	return cmd
}

// newSocialEditCmd creates the command to edit an existing post.
func newSocialEditCmd() *cobra.Command {
	var labelsStr string
	cmd := &cobra.Command{
		Use:   "edit <post-id> <new-text>",
		Short: "Edit an existing post",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !EnsureGitRepo(cmd) {
				return exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			postID := args[0]
			content := args[1]

			if content == "-" {
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
				PrintError(cmd, "post content cannot be empty")
				return exit(ExitInvalidArgs)
			}

			var opts *social.EditPostOptions
			if cmd.Flags().Changed("labels") {
				labels := text.SplitCSV(labelsStr)
				opts = &social.EditPostOptions{Labels: &labels}
			}

			result := social.EditPost(cfg.WorkDir, postID, content, opts)

			if !result.Success {
				PrintError(cmd, result.Error.Text())
				return exit(ExitCode(result.Error.Code))
			}

			if cfg.JSONOutput {
				return PrintJSON(cmd, result.Data)
			} else {
				PrintSuccess(cmd, "Post edited")
				fmt.Fprintln(cmd.OutOrStdout())
				fmt.Fprintln(cmd.OutOrStdout(), social.FormatPost(result.Data))
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&labelsStr, "labels", "l", "", "Comma-separated labels, replacing any set")
	return cmd
}

// newSocialRetractCmd creates the command to retract a post.
func newSocialRetractCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "retract <post-id>",
		Short: "Retract a post",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !EnsureGitRepo(cmd) {
				return exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			postID := args[0]

			result := social.RetractPost(cfg.WorkDir, postID)

			if !result.Success {
				PrintError(cmd, result.Error.Text())
				return exit(ExitCode(result.Error.Code))
			}

			if cfg.JSONOutput {
				return PrintJSON(cmd, map[string]string{"status": "retracted", "post": postID})
			} else {
				PrintSuccess(cmd, "Post retracted")
			}
			return nil
		},
	}

	return cmd
}

// newSocialCommentCmd creates the command to comment on a post.
func newSocialCommentCmd() *cobra.Command {
	var labelsStr string
	cmd := &cobra.Command{
		Use:   "comment <post-id> <text>",
		Short: "Comment on a post",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !EnsureGitRepo(cmd) {
				return exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			postID := args[0]
			content := args[1]

			if content == "-" {
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

			result := social.CreateComment(cfg.WorkDir, postID, content, &social.CreateCommentOptions{Labels: text.SplitCSV(labelsStr)})

			if !result.Success {
				PrintError(cmd, result.Error.Text())
				return exit(ExitCode(result.Error.Code))
			}

			if cfg.JSONOutput {
				return PrintJSON(cmd, result.Data)
			} else {
				PrintSuccess(cmd, "Comment created")
				fmt.Fprintln(cmd.OutOrStdout())
				fmt.Fprintln(cmd.OutOrStdout(), social.FormatPost(result.Data))
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&labelsStr, "labels", "l", "", "Comma-separated labels, such as topic/release")

	return cmd
}

// newSocialRepostCmd creates the command to repost a post.
func newSocialRepostCmd() *cobra.Command {
	var labelsStr string
	cmd := &cobra.Command{
		Use:   "repost <post-id>",
		Short: "Repost a post",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !EnsureGitRepo(cmd) {
				return exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			postID := args[0]

			result := social.CreateRepost(cfg.WorkDir, postID, &social.CreateRepostOptions{Labels: text.SplitCSV(labelsStr)})

			if !result.Success {
				PrintError(cmd, result.Error.Text())
				return exit(ExitCode(result.Error.Code))
			}

			if cfg.JSONOutput {
				return PrintJSON(cmd, result.Data)
			} else {
				PrintSuccess(cmd, "Reposted")
				fmt.Fprintln(cmd.OutOrStdout())
				fmt.Fprintln(cmd.OutOrStdout(), social.FormatPost(result.Data))
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&labelsStr, "labels", "l", "", "Comma-separated labels, such as topic/release")

	return cmd
}

// newSocialQuoteCmd creates the command to quote a post with commentary.
func newSocialQuoteCmd() *cobra.Command {
	var labelsStr string
	cmd := &cobra.Command{
		Use:   "quote <post-id> <text>",
		Short: "Quote a post with your own commentary",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !EnsureGitRepo(cmd) {
				return exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			postID := args[0]
			content := args[1]

			if content == "-" {
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

			result := social.CreateQuote(cfg.WorkDir, postID, content, &social.CreateQuoteOptions{Labels: text.SplitCSV(labelsStr)})

			if !result.Success {
				PrintError(cmd, result.Error.Text())
				return exit(ExitCode(result.Error.Code))
			}

			if cfg.JSONOutput {
				return PrintJSON(cmd, result.Data)
			} else {
				PrintSuccess(cmd, "Quote created")
				fmt.Fprintln(cmd.OutOrStdout())
				fmt.Fprintln(cmd.OutOrStdout(), social.FormatPost(result.Data))
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&labelsStr, "labels", "l", "", "Comma-separated labels, such as topic/release")

	return cmd
}

// newSocialListCmd creates the parent command for managing repository lists.
func newSocialListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "Manage repository lists",
	}

	cmd.AddCommand(
		newSocialListShowCmd(),
		newSocialListLsCmd(),
		newSocialListCreateCmd(),
		newSocialListDeleteCmd(),
		newSocialListAddCmd(),
		newSocialListRemoveCmd(),
		newSocialListRepoCmd(),
	)

	return cmd
}

// newSocialListShowCmd creates the command to show lists or a specific list.
func newSocialListShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show [name]",
		Short: "Show lists or a specific list",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !EnsureGitRepo(cmd) {
				return exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)

			if len(args) == 0 {
				result := social.GetLists(cfg.WorkDir)
				if !result.Success {
					PrintError(cmd, result.Error.Text())
					return exit(ExitCode(result.Error.Code))
				}

				if cfg.JSONOutput {
					if err := PrintJSON(cmd, result.Data); err != nil {
						return err
					}
				} else {
					fmt.Fprintln(cmd.OutOrStdout(), social.FormatLists(result.Data))
				}
				return nil
			}

			listID := args[0]
			result := social.GetList(cfg.WorkDir, listID)
			if !result.Success {
				PrintError(cmd, result.Error.Text())
				return exit(ExitCode(result.Error.Code))
			}

			if result.Data == nil {
				PrintError(cmd, "list not found: "+listID)
				return exit(ExitError)
			}

			if cfg.JSONOutput {
				return PrintJSON(cmd, result.Data)
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), social.FormatList(*result.Data))
				if len(result.Data.Repositories) > 0 {
					fmt.Fprintln(cmd.OutOrStdout(), "\nRepositories:")
					for _, repo := range result.Data.Repositories {
						fmt.Fprintf(cmd.OutOrStdout(), "  - %s\n", repo)
					}
				}
			}
			return nil
		},
	}
}

// newSocialListLsCmd creates the command to list all lists.
func newSocialListLsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ls",
		Short: "List all lists (alias for 'show')",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !EnsureGitRepo(cmd) {
				return exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			result := social.GetLists(cfg.WorkDir)
			if !result.Success {
				PrintError(cmd, result.Error.Text())
				return exit(ExitCode(result.Error.Code))
			}

			if cfg.JSONOutput {
				return PrintJSON(cmd, result.Data)
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), social.FormatLists(result.Data))
			}
			return nil
		},
	}
}

// newSocialListCreateCmd creates the command to create a new list.
func newSocialListCreateCmd() *cobra.Command {
	var name string

	cmd := &cobra.Command{
		Use:   "create <id>",
		Short: "Create a new list",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !EnsureGitRepo(cmd) {
				return exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			listID := args[0]
			result := social.CreateList(cfg.WorkDir, listID, name)

			if !result.Success {
				PrintError(cmd, result.Error.Text())
				return exit(ExitCode(result.Error.Code))
			}

			if cfg.JSONOutput {
				return PrintJSON(cmd, result.Data)
			} else {
				PrintSuccess(cmd, "List created: "+listID)
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&name, "name", "n", "", "Display name for the list")

	return cmd
}

// newSocialListDeleteCmd creates the command to delete a list.
func newSocialListDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete a list",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !EnsureGitRepo(cmd) {
				return exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			listID := args[0]
			result := social.DeleteList(cfg.WorkDir, listID)

			if !result.Success {
				PrintError(cmd, result.Error.Text())
				return exit(ExitCode(result.Error.Code))
			}

			if cfg.JSONOutput {
				return PrintJSON(cmd, map[string]string{"status": "deleted", "list": listID})
			} else {
				PrintSuccess(cmd, "List deleted: "+listID)
			}
			return nil
		},
	}
}

// newSocialListAddCmd creates the command to add a repository to a list.
func newSocialListAddCmd() *cobra.Command {
	var branch string
	var allBranches bool

	cmd := &cobra.Command{
		Use:   "add <list-id> <repository-url>",
		Short: "Add a repository to a list",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !EnsureGitRepo(cmd) {
				return exit(ExitNotRepo)
			}

			if allBranches && branch != "" {
				PrintError(cmd, "--all-branches and --branch are mutually exclusive")
				return exit(ExitInvalidArgs)
			}

			cfg := GetConfig(cmd)
			listID := args[0]
			repoURL := args[1]

			result := social.AddRepositoryToList(cfg.WorkDir, listID, repoURL, branch, allBranches)

			if !result.Success {
				PrintError(cmd, result.Error.Text())
				return exit(ExitCode(result.Error.Code))
			}

			if cfg.JSONOutput {
				return PrintJSON(cmd, map[string]string{
					"status":     "added",
					"list":       listID,
					"repository": result.Data,
				})
			} else {
				PrintSuccess(cmd, fmt.Sprintf("Added %s to list %s", result.Data, listID))
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&branch, "branch", "b", "", "Branch to track")
	cmd.Flags().BoolVar(&allBranches, "all-branches", false, "Follow all branches")

	return cmd
}

// newSocialListRemoveCmd creates the command to remove a repository from a list.
func newSocialListRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove <list-id> <repository-url>",
		Short: "Remove a repository from a list",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !EnsureGitRepo(cmd) {
				return exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			listID := args[0]
			repoURL := args[1]

			result := social.RemoveRepositoryFromList(cfg.WorkDir, listID, repoURL)

			if !result.Success {
				PrintError(cmd, result.Error.Text())
				return exit(ExitCode(result.Error.Code))
			}

			if cfg.JSONOutput {
				return PrintJSON(cmd, map[string]string{
					"status":     "removed",
					"list":       listID,
					"repository": repoURL,
				})
			} else {
				PrintSuccess(cmd, fmt.Sprintf("Removed %s from list %s", repoURL, listID))
			}
			return nil
		},
	}
}

// newSocialFetchCmd creates the command to fetch social updates from repositories.
func newSocialFetchCmd() *cobra.Command {
	var listID string
	var parallel int

	cmd := &cobra.Command{
		Use:   "fetch [url]",
		Short: "Fetch social updates",
		Long: `Fetch social updates from subscribed repositories and populate the cache.

Examples:
  gitsocial social fetch                     # Fetch all subscribed repos
  gitsocial social fetch --list reading      # Fetch only repos in 'reading' list
  gitsocial social fetch https://github.com/user/repo  # Fetch specific repo`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !EnsureGitRepo(cmd) {
				return exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)

			if len(args) == 1 {
				repoURL := args[0]
				workspaceURL := gitmsg.ResolveRepoURL(cfg.WorkDir)
				result := client.FetchRepository(cfg.CacheDir, repoURL, "", workspaceURL)
				if !result.Success {
					PrintError(cmd, result.Error.Text())
					return exit(ExitCode(result.Error.Code))
				}

				if cfg.JSONOutput {
					if err := PrintJSON(cmd, result.Data); err != nil {
						return err
					}
				} else {
					fmt.Fprintf(cmd.OutOrStdout(), "✓ %s (%d posts)\n", repoURL, result.Data.Items)
				}
				return nil
			}

			opts := client.FetchOptions{
				ListID:   listID,
				Parallel: parallel,
			}

			if !cfg.JSONOutput {
				if listID != "" {
					fmt.Fprintf(cmd.OutOrStdout(), "Fetching repositories from list '%s'...\n", listID)
				} else {
					fmt.Fprintln(cmd.OutOrStdout(), "Fetching all subscribed repositories...")
				}
			}

			result, _ := client.Fetch(cfg.WorkDir, cfg.CacheDir, opts)
			if !result.Success {
				PrintError(cmd, result.Error.Text())
				return exit(ExitCode(result.Error.Code))
			}

			stats := result.Data

			if cfg.JSONOutput {
				return PrintJSON(cmd, stats)
			} else {
				for _, e := range stats.Errors {
					fmt.Fprintf(cmd.OutOrStdout(), "  ✗ %s (%s)\n", e.Repository, e.Error)
				}

				if stats.Repositories > 0 || len(stats.Errors) == 0 {
					fmt.Fprintf(cmd.OutOrStdout(), "\nFetched %d items from %d repositories\n", stats.Items, stats.Repositories)
				}

				if len(stats.Errors) > 0 {
					fmt.Fprintf(cmd.OutOrStdout(), "Failed: %d repositories\n", len(stats.Errors))
				}
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&listID, "list", "l", "", "Fetch only repos from this list")
	cmd.Flags().IntVarP(&parallel, "parallel", "p", 4, "Number of concurrent fetches")

	return cmd
}

// newSocialFollowersCmd creates the command to list repositories following the workspace.
func newSocialFollowersCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "followers",
		Short: "List repositories that follow you",
		Long: `List repositories that follow your workspace.

A repository "follows" you if they have your repository URL in one of their lists.
This is detected during fetch when parsing remote repository lists.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !EnsureGitRepo(cmd) {
				return exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			workspaceURL := protocol.NormalizeURL(git.GetOriginURL(cfg.WorkDir))

			if workspaceURL == "" {
				PrintError(cmd, "no origin remote: add one with git remote add origin <url>")
				return exit(ExitError)
			}

			followers, err := social.GetFollowers(workspaceURL)
			if err != nil {
				PrintError(cmd, "read followers: "+err.Error())
				return exit(ExitError)
			}

			if cfg.JSONOutput {
				return PrintJSON(cmd, map[string]interface{}{
					"workspace": workspaceURL,
					"followers": followers,
					"count":     len(followers),
				})
			} else {
				if len(followers) == 0 {
					fmt.Fprintln(cmd.OutOrStdout(), "No followers detected yet.")
					fmt.Fprintln(cmd.OutOrStdout(), "Run 'gitsocial fetch' to detect followers from remote repositories.")
				} else {
					fmt.Fprintf(cmd.OutOrStdout(), "Repositories following %s:\n\n", workspaceURL)
					for _, f := range followers {
						fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", f)
					}
					fmt.Fprintf(cmd.OutOrStdout(), "\nTotal: %d\n", len(followers))
				}
			}
			return nil
		},
	}
}

// newSocialListRepoCmd creates the command to show lists defined by a repository.
func newSocialListRepoCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "repo <url>",
		Short: "Show lists defined by a repository",
		Long: `Show the social lists defined by a repository.

For the current workspace, lists are read from git refs directly.
For external repositories, lists are read from the cache (populated during fetch).

Examples:
  gitsocial social list repo https://github.com/user/repo`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !EnsureGitRepo(cmd) {
				return exit(ExitNotRepo)
			}

			cfg := GetConfig(cmd)
			repoURL := protocol.NormalizeURL(args[0])

			originURL := git.GetOriginURL(cfg.WorkDir)
			isWorkspace := originURL != "" && protocol.NormalizeURL(originURL) == repoURL

			type listView struct {
				ID        string `json:"id"`
				Name      string `json:"name"`
				Version   string `json:"version"`
				RepoCount int    `json:"repo_count"`
			}

			var lists []listView

			if isWorkspace {
				listIDs, err := gitmsg.EnumerateLists(cfg.WorkDir, socialExt)
				if err != nil {
					PrintError(cmd, "enumerate lists: "+err.Error())
					return exit(ExitError)
				}
				for _, id := range listIDs {
					data, _ := gitmsg.ReadList(cfg.WorkDir, socialExt, id)
					if data == nil {
						continue
					}
					lists = append(lists, listView{
						ID:        data.ID,
						Name:      data.Name,
						Version:   data.Version,
						RepoCount: len(data.Repositories),
					})
				}
			} else {
				cachedLists, err := cache.GetExternalRepoLists(repoURL)
				if err != nil {
					PrintError(cmd, "read lists from cache: "+err.Error())
					return exit(ExitError)
				}
				for _, list := range cachedLists {
					lists = append(lists, listView{
						ID:        list.ListID,
						Name:      list.Name,
						Version:   list.Version,
						RepoCount: len(list.Repositories),
					})
				}
			}

			if cfg.JSONOutput {
				return PrintJSON(cmd, lists)
			} else {
				if len(lists) == 0 {
					fmt.Fprintln(cmd.OutOrStdout(), "No lists found for this repository")
					if !isWorkspace {
						fmt.Fprintln(cmd.OutOrStdout(), "(Try running 'gitsocial fetch' first to cache remote data)")
					}
				} else {
					fmt.Fprintf(cmd.OutOrStdout(), "Lists from %s:\n\n", repoURL)
					for _, list := range lists {
						fmt.Fprintf(cmd.OutOrStdout(), "  %s (%d repos)\n", list.Name, list.RepoCount)
					}
				}
			}
			return nil
		},
	}
}

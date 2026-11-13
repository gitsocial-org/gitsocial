// notifications_level.go - The command that sets or prints the notification level of a repository
package main

import (
	"fmt"
	"slices"
	"time"

	"github.com/spf13/cobra"

	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
	"github.com/gitsocial-org/gitsocial/library/core/settings"
)

// newNotificationsLevelCmd creates the command that sets or prints the notification level of a repository.
func newNotificationsLevelCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "level [<repository>] [all|default|off]",
		Short: "Set or show the notification level of a repository",
		Long: `Set the notification level of a repository, or print the levels. The level
is in your personal settings, not in the repository.

  default  the notifications of each type, the same as with no level
  all      also one activity notification for each new timeline item
           that is not yours, from the time that you set the level
  off      the notifications stay in the list and go to the webhook;
           no desktop message

With no argument, the command prints the levels. One argument that is a
level sets the level of the workspace. One other argument is a repository,
and the command prints its level. A registered fork with no level takes the
level of the workspace. The level all applies to the workspace, its
registered forks and the repositories of its lists.

Examples:
  gitsocial notifications level                                # the levels
  gitsocial notifications level all                            # the workspace
  gitsocial notifications level https://github.com/org/repo off
  gitsocial notifications level https://github.com/org/repo    # one level`,
		Args: cobra.MaximumNArgs(2),
		RunE: runNotificationsLevel,
	}
}

// runNotificationsLevel prints the levels, prints the level of one repository, or writes a level.
func runNotificationsLevel(cmd *cobra.Command, args []string) error {
	if !EnsureGitRepo(cmd) {
		return exit(ExitNotRepo)
	}
	cfg := GetConfig(cmd)
	workspaceURL := gitmsg.ResolveRepoURL(cfg.WorkDir)
	levels := settings.NotificationLevels()
	if len(args) == 0 {
		shown := make(map[string]string, len(levels))
		for repo := range levels {
			shown[repo] = levelText(settings.NotificationLevel(levels, repo, "", nil))
		}
		return printNotificationLevels(cmd, cfg.JSONOutput, shown)
	}
	repoURL, level := args[0], ""
	if len(args) == 2 {
		level = args[1]
	} else if isLevel(args[0]) {
		repoURL, level = workspaceURL, args[0]
	}
	identity := protocol.NormalizeURL(repoURL)
	if identity == "" {
		PrintError(cmd, "no repository: give its URL or add an origin remote")
		return exit(ExitInvalidArgs)
	}
	if level == "" {
		current := levelText(settings.NotificationLevel(levels, identity, workspaceURL, gitmsg.GetForks(cfg.WorkDir)))
		return printNotificationLevels(cmd, cfg.JSONOutput, map[string]string{identity: current})
	}
	if !isLevel(level) {
		PrintError(cmd, fmt.Sprintf("invalid level %q: use all, default or off", level))
		return exit(ExitInvalidArgs)
	}
	// A second set of all keeps the first set time, so the events from that time stay in the list.
	if own, setAt := settings.NotificationLevel(levels, identity, "", nil); level == settings.LevelAll && own == settings.LevelAll {
		return printNotificationLevels(cmd, cfg.JSONOutput, map[string]string{identity: levelText(own, setAt)})
	}
	if err := settings.WriteNotificationLevel(identity, level, time.Now()); err != nil {
		PrintError(cmd, "write the level: "+err.Error())
		return exit(ExitError)
	}
	if cfg.JSONOutput {
		return PrintJSON(cmd, map[string]string{"repository": identity, "level": level})
	}
	PrintSuccess(cmd, fmt.Sprintf("Level of %s: %s", identity, level))
	return nil
}

// isLevel reports whether a word is a notification level.
func isLevel(word string) bool {
	return word == settings.LevelAll || word == settings.LevelDefault || word == settings.LevelOff
}

// levelText returns the text of a level: the word, and for all its set time.
func levelText(level string, setAt time.Time) string {
	if level == settings.LevelAll {
		return level + " since " + setAt.UTC().Format(time.RFC3339)
	}
	return level
}

// printNotificationLevels prints each repository with the text of its level, sorted by repository.
func printNotificationLevels(cmd *cobra.Command, jsonOutput bool, levels map[string]string) error {
	repos := make([]string, 0, len(levels))
	for repo := range levels {
		repos = append(repos, repo)
	}
	slices.Sort(repos)
	rows := make([]map[string]string, 0, len(repos))
	for _, repo := range repos {
		rows = append(rows, map[string]string{"repository": repo, "level": levels[repo]})
	}
	if jsonOutput {
		return PrintJSON(cmd, rows)
	}
	if len(rows) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No notification levels.")
		return nil
	}
	for _, row := range rows {
		fmt.Fprintf(cmd.OutOrStdout(), "%s  %s\n", row["repository"], row["level"])
	}
	return nil
}

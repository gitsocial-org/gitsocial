// memo.go - CLI commands for the memo extension
package main

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/settings"
	"github.com/gitsocial-org/gitsocial/library/core/text"
	"github.com/gitsocial-org/gitsocial/library/extensions/memo"
)

const memoExt = "memo"

func init() {
	RegisterExtension(ExtensionRegistration{
		Use:   "memo",
		Short: "Memos: knowledge stored as commits across tiers",
		Register: func(cmd *cobra.Command) {
			cmd.AddCommand(
				newMemoStatusCmd(),
				NewExtConfigCmd(memoExt),
				newMemoProjectCmd(),
				newMemoPersonalCmd(),
				newMemoSessionCmd(),
				newMemoInheritCmd(),
				newMemoCreateCmd(),
				newMemoEditCmd(),
				newMemoRetractCmd(),
				newMemoPromoteCmd(),
				newMemoListCmd(),
				newMemoShowCmd(),
			)
		},
	})
}

// newMemoStatusCmd creates the command to show memo extension status.
func newMemoStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show memo extension status",
		Run: func(cmd *cobra.Command, args []string) {
			if !EnsureGitRepo(cmd) {
				os.Exit(ExitNotRepo)
			}
			cfg := GetConfig(cmd)
			if err := memo.SyncAllTierReposToCache(cfg.WorkDir); err != nil {
				slog.Debug("memo sync", "error", err)
			}
			projectInit := memo.IsProjectInitialized(cfg.WorkDir)
			personalPath, _ := settings.PersonalRepoPath()
			sessionDir, _ := memo.SessionDir()
			sessions := memo.ListSessions(gitmsg.ResolveRepoURL(cfg.WorkDir))
			inherits := memo.InheritsStatus(cfg.WorkDir)

			counts := map[string]int{}
			tiers := []memo.Tier{memo.TierSession, memo.TierPersonal, memo.TierProject}
			for _, t := range tiers {
				res := memo.ListMemos(cfg.WorkDir, memo.ListOptions{Tier: t})
				if res.Success {
					counts[string(t)] = len(res.Data)
				}
			}

			if cfg.JSONOutput {
				PrintJSON(map[string]interface{}{
					"project_initialized": projectInit,
					"personal_repo":       personalPath,
					"session_dir":         sessionDir,
					"counts":              counts,
					"sessions":            sessions.Data,
					"inherits":            inherits,
				})
				return
			}
			fmt.Println("Memo:")
			fmt.Printf("  Project initialized: %v\n", projectInit)
			fmt.Printf("  Personal repo: %s\n", personalPath)
			fmt.Printf("  Session dir: %s\n", sessionDir)
			fmt.Printf("  Counts: session=%d personal=%d project=%d\n",
				counts["session"], counts["personal"], counts["project"])
			if sessions.Success {
				fmt.Printf("  Active sessions: %d\n", len(sessions.Data))
			}
			if len(inherits) > 0 {
				fmt.Printf("  Inherited: %s\n", formatInheritsLine(inherits))
			}
		},
	}
}

// formatInheritsLine summarizes inherited sources for memo status: count plus
// oldest-fetch age, with an explicit warning if any URL isn't followed (so the
// user knows fetch won't pick it up).
func formatInheritsLine(statuses []memo.InheritStatus) string {
	notFollowed := 0
	neverFetched := 0
	var oldest time.Time
	for _, s := range statuses {
		if !s.Followed {
			notFollowed++
			continue
		}
		if s.LastFetch.IsZero() {
			neverFetched++
			continue
		}
		if oldest.IsZero() || s.LastFetch.Before(oldest) {
			oldest = s.LastFetch
		}
	}
	line := fmt.Sprintf("%d source(s)", len(statuses))
	switch {
	case notFollowed > 0:
		line += fmt.Sprintf(", %d not followed — run `gitsocial fetch` to subscribe", notFollowed)
	case neverFetched > 0:
		line += fmt.Sprintf(", %d not yet fetched — run `gitsocial fetch`", neverFetched)
	case !oldest.IsZero():
		line += fmt.Sprintf(", oldest fetch %s ago", memo.FormatAge(oldest))
	}
	return line
}

// newMemoProjectCmd creates the parent command for project-tier subcommands.
func newMemoProjectCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "project",
		Short: "Project-tier subcommands",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "init",
		Short: "Initialize the project memo branch (idempotent)",
		Run: func(cmd *cobra.Command, args []string) {
			if !EnsureGitRepo(cmd) {
				os.Exit(ExitNotRepo)
			}
			cfg := GetConfig(cmd)
			res := memo.InitProject(cfg.WorkDir)
			if !res.Success {
				PrintError(cmd, res.Error.Message)
				os.Exit(1)
			}
			PrintSuccess(cmd, "project tier initialized")
		},
	})
	return cmd
}

// newMemoPersonalCmd creates the parent command for personal-tier subcommands.
func newMemoPersonalCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "personal",
		Short: "Personal-tier subcommands",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "init",
		Short: "Initialize the personal bare repo (idempotent)",
		Run: func(cmd *cobra.Command, args []string) {
			res := memo.InitPersonal()
			if !res.Success {
				PrintError(cmd, res.Error.Message)
				os.Exit(1)
			}
			PrintSuccess(cmd, "personal tier initialized at "+res.Data)
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "push",
		Short: "Push personal memos to the configured remote",
		Run: func(cmd *cobra.Command, args []string) {
			res := memo.PushPersonal()
			if !res.Success {
				PrintError(cmd, res.Error.Message)
				os.Exit(1)
			}
			PrintSuccess(cmd, "pushed personal memos")
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "fetch",
		Short: "Fetch personal memos from the configured remote",
		Run: func(cmd *cobra.Command, args []string) {
			res := memo.FetchPersonal()
			if !res.Success {
				PrintError(cmd, res.Error.Message)
				os.Exit(1)
			}
			PrintSuccess(cmd, "fetched personal memos")
		},
	})
	return cmd
}

// newMemoSessionCmd creates the parent command for session-tier subcommands.
func newMemoSessionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "session",
		Short: "Session-tier subcommands",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "init [id]",
		Short: "Create or resume a session (prints resolved id)",
		Args:  cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			id := ""
			if len(args) == 1 {
				id = args[0]
			}
			cfg := GetConfig(cmd)
			workspaceURL := ""
			if cfg != nil {
				workspaceURL = gitmsg.ResolveRepoURL(cfg.WorkDir)
			}
			res := memo.InitSession(id, workspaceURL)
			if !res.Success {
				PrintError(cmd, res.Error.Message)
				os.Exit(1)
			}
			if cfg != nil && cfg.JSONOutput {
				PrintJSON(map[string]string{"session_id": res.Data})
			} else {
				fmt.Println(res.Data)
			}
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List active sessions with ages",
		Run: func(cmd *cobra.Command, args []string) {
			cfg := GetConfig(cmd)
			workspaceURL := ""
			if cfg != nil {
				workspaceURL = gitmsg.ResolveRepoURL(cfg.WorkDir)
			}
			res := memo.ListSessions(workspaceURL)
			if !res.Success {
				PrintError(cmd, res.Error.Message)
				os.Exit(1)
			}
			if cfg != nil && cfg.JSONOutput {
				PrintJSON(res.Data)
				return
			}
			if len(res.Data) == 0 {
				fmt.Println("(no sessions)")
				return
			}
			for _, s := range res.Data {
				remote := ""
				if s.HasRemote {
					remote = " (remote)"
				}
				fmt.Printf("%s  %s%s\n", s.ID, memo.FormatAge(s.LastUsed), remote)
			}
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "push <id>",
		Short: "Push a session's memos to its remote (if configured)",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			res := memo.PushSession(args[0])
			if !res.Success {
				PrintError(cmd, res.Error.Message)
				os.Exit(1)
			}
			PrintSuccess(cmd, "pushed session "+args[0])
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "fetch <id>",
		Short: "Fetch a session's memos from its remote (if configured)",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			res := memo.FetchSession(args[0])
			if !res.Success {
				PrintError(cmd, res.Error.Message)
				os.Exit(1)
			}
			PrintSuccess(cmd, "fetched session "+args[0])
		},
	})
	gcCmd := &cobra.Command{
		Use:   "gc [id]",
		Short: "Delete a session, or sessions inactive past --older-than",
		Args:  cobra.MaximumNArgs(1),
	}
	var olderThan string
	gcCmd.Flags().StringVar(&olderThan, "older-than", "", "delete sessions inactive past this duration (e.g. 30d, 7d, 24h)")
	gcCmd.Run = func(cmd *cobra.Command, args []string) {
		if olderThan != "" {
			d, err := parseDurationFriendly(olderThan)
			if err != nil {
				PrintError(cmd, err.Error())
				os.Exit(1)
			}
			res := memo.GCSessionsOlderThan(d)
			if !res.Success {
				PrintError(cmd, res.Error.Message)
				os.Exit(1)
			}
			cfg := GetConfig(cmd)
			if cfg != nil && cfg.JSONOutput {
				PrintJSON(map[string]interface{}{"deleted": res.Data})
				return
			}
			fmt.Printf("deleted %d session(s)\n", len(res.Data))
			return
		}
		if len(args) != 1 {
			PrintError(cmd, "session id required (or pass --older-than)")
			os.Exit(1)
		}
		res := memo.GCSession(args[0])
		if !res.Success {
			PrintError(cmd, res.Error.Message)
			os.Exit(1)
		}
		PrintSuccess(cmd, "deleted session "+args[0])
	}
	cmd.AddCommand(gcCmd)
	return cmd
}

// newMemoInheritCmd creates the parent command for managing inherited memo source repos.
func newMemoInheritCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "inherit",
		Short: "Manage repos this project inherits memos from (binding policy sources)",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "add <url>",
		Short: "Register a memo source repo (auto-follows via the memo-inherits list)",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			if !EnsureGitRepo(cmd) {
				os.Exit(ExitNotRepo)
			}
			cfg := GetConfig(cmd)
			res := memo.AddInherit(cfg.WorkDir, args[0])
			if !res.Success {
				PrintError(cmd, res.Error.Message)
				os.Exit(1)
			}
			if res.Data {
				PrintSuccess(cmd, "added inherit: "+args[0]+" (followed via '"+memo.InheritsListID+"' list — run `gitsocial fetch` to pull memos)")
			} else {
				PrintSuccess(cmd, "already inherited: "+args[0])
			}
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List inherited memo source repos",
		Run: func(cmd *cobra.Command, args []string) {
			if !EnsureGitRepo(cmd) {
				os.Exit(ExitNotRepo)
			}
			cfg := GetConfig(cmd)
			urls := memo.ListInherits(cfg.WorkDir)
			if cfg.JSONOutput {
				PrintJSON(urls)
				return
			}
			if len(urls) == 0 {
				fmt.Println("(no inherited sources)")
				return
			}
			for _, u := range urls {
				fmt.Println(u)
			}
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "remove <url>",
		Short: "Remove a memo source repo (also removes from the memo-inherits list)",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			if !EnsureGitRepo(cmd) {
				os.Exit(ExitNotRepo)
			}
			cfg := GetConfig(cmd)
			res := memo.RemoveInherit(cfg.WorkDir, args[0])
			if !res.Success {
				PrintError(cmd, res.Error.Message)
				os.Exit(1)
			}
			PrintSuccess(cmd, "removed inherit: "+args[0])
		},
	})
	return cmd
}

// newMemoCreateCmd creates the command to create a new memo (defaults to session tier).
func newMemoCreateCmd() *cobra.Command {
	var labels, scope, body string
	cmd := &cobra.Command{
		Use:   "create <subject>",
		Short: "Create a new memo (defaults to session tier)",
		Long: `Create a new memo. The body comes from --body, stdin (with --body -), or
the editor ($GITSOCIAL_EDITOR > $EDITOR > $VISUAL > vi) when --body is omitted
and the command runs interactively.`,
		Args: cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			cfg := GetConfig(cmd)
			tier := memo.Tier(strings.TrimSpace(scope))
			if tier == "" {
				tier = memo.TierSession
			}
			if tier == memo.TierProject && !EnsureGitRepo(cmd) {
				os.Exit(ExitNotRepo)
			}
			resolvedBody, err := resolveMemoBody(cmd, body, args[0], tier)
			if err != nil {
				PrintError(cmd, err.Error())
				os.Exit(1)
			}
			res := memo.CreateMemo(cfg.WorkDir, args[0], resolvedBody, memo.CreateMemoOptions{
				Tier:   tier,
				Labels: text.SplitCSV(labels),
			})
			if !res.Success {
				PrintError(cmd, res.Error.Message)
				os.Exit(1)
			}
			if cfg.JSONOutput {
				PrintJSON(res.Data)
				return
			}
			fmt.Printf("memo created: %s\n", res.Data.ID)
		},
	}
	cmd.Flags().StringVar(&labels, "labels", "", "comma-separated labels (e.g. kind/policy,priority/high)")
	cmd.Flags().StringVar(&scope, "scope", "", "tier: session | personal | project (default: session)")
	cmd.Flags().StringVar(&body, "body", "", "memo body text; `-` to read from stdin; omit to open editor")
	return cmd
}

// resolveMemoBody decides how to source the memo body:
//   - `--body <text>` is taken verbatim
//   - `--body -` reads from stdin until EOF
//   - omitted: opens $EDITOR with a template (only when interactive)
//   - omitted + non-interactive: returns empty (script-friendly default)
func resolveMemoBody(cmd *cobra.Command, body, subject string, tier memo.Tier) (string, error) {
	if cmd.Flags().Changed("body") {
		if body == "-" {
			data, err := io.ReadAll(os.Stdin)
			if err != nil {
				return "", fmt.Errorf("read stdin: %w", err)
			}
			return strings.TrimRight(string(data), "\n"), nil
		}
		return body, nil
	}
	if !IsInteractive() {
		return "", nil
	}
	template := fmt.Sprintf(`
# Body for memo: %s
# Tier: %s
#
# Lines starting with `+"`#`"+` are stripped before saving.
# Save and quit to commit the memo; quit without saving to abort.
`, subject, tier)
	return OpenInEditor(template, ".md")
}

// newMemoEditCmd creates the command to edit a memo on its source tier (creates a new version).
func newMemoEditCmd() *cobra.Command {
	var subject, body, labels string
	var setSubject, setBody, setLabels bool
	cmd := &cobra.Command{
		Use:   "edit <ref>",
		Short: "Edit a memo on its source tier (creates a new version)",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			cfg := GetConfig(cmd)
			opts := memo.EditMemoOptions{}
			if setSubject {
				opts.Subject = &subject
			}
			if setBody {
				opts.Body = &body
			}
			if setLabels {
				ls := text.SplitCSV(labels)
				opts.Labels = &ls
			}
			res := memo.EditMemo(cfg.WorkDir, args[0], opts)
			if !res.Success {
				PrintError(cmd, res.Error.Message)
				os.Exit(1)
			}
			PrintSuccess(cmd, "memo edited")
		},
	}
	cmd.Flags().StringVar(&subject, "subject", "", "new subject")
	cmd.Flags().StringVar(&body, "body", "", "new body")
	cmd.Flags().StringVar(&labels, "labels", "", "new labels (comma-separated)")
	cmd.PreRun = func(cmd *cobra.Command, args []string) {
		setSubject = cmd.Flags().Changed("subject")
		setBody = cmd.Flags().Changed("body")
		setLabels = cmd.Flags().Changed("labels")
	}
	return cmd
}

// newMemoRetractCmd creates the command to retract a memo via the edit chain.
func newMemoRetractCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "retract <ref>",
		Short: "Retract a memo (marks as removed via the edit chain)",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			cfg := GetConfig(cmd)
			res := memo.RetractMemo(cfg.WorkDir, args[0])
			if !res.Success {
				PrintError(cmd, res.Error.Message)
				os.Exit(1)
			}
			PrintSuccess(cmd, "memo retracted")
		},
	}
}

// newMemoPromoteCmd creates the command to promote a memo to a higher tier as a fresh commit.
func newMemoPromoteCmd() *cobra.Command {
	var to string
	cmd := &cobra.Command{
		Use:   "promote <ref> --to <tier>",
		Short: "Promote a memo to a higher tier (copy as fresh commit; source untouched)",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			cfg := GetConfig(cmd)
			tier := memo.Tier(strings.TrimSpace(to))
			if tier == "" {
				PrintError(cmd, "--to <tier> is required")
				os.Exit(1)
			}
			res := memo.PromoteMemo(cfg.WorkDir, args[0], tier)
			if !res.Success {
				PrintError(cmd, res.Error.Message)
				os.Exit(1)
			}
			if cfg.JSONOutput {
				PrintJSON(res.Data)
				return
			}
			fmt.Printf("promoted to %s: %s\n", tier, res.Data.ID)
		},
	}
	cmd.Flags().StringVar(&to, "to", "", "target tier: project | personal | session")
	return cmd
}

// newMemoListCmd creates the command to list memos across tiers (external hidden by default).
func newMemoListCmd() *cobra.Command {
	var tier, includeSessions, labels string
	var includeExpired, onlyExpired, includeExternal bool
	var limit int
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List memos (session + personal + project + inherited by default; external hidden)",
		Run: func(cmd *cobra.Command, args []string) {
			cfg := GetConfig(cmd)
			if err := memo.SyncAllTierReposToCache(cfg.WorkDir); err != nil {
				slog.Debug("memo sync", "error", err)
			}
			res := memo.ListMemos(cfg.WorkDir, memo.ListOptions{
				Tier:            memo.Tier(tier),
				IncludeSessions: includeSessions,
				IncludeExpired:  includeExpired,
				OnlyExpired:     onlyExpired,
				IncludeExternal: includeExternal,
				Labels:          text.SplitCSV(labels),
				Limit:           limit,
			})
			if !res.Success {
				PrintError(cmd, res.Error.Message)
				os.Exit(1)
			}
			if cfg.JSONOutput {
				PrintJSON(res.Data)
				return
			}
			if len(res.Data) == 0 {
				fmt.Println("(no memos)")
				return
			}
			for _, m := range res.Data {
				labelStr := ""
				if len(m.Labels) > 0 {
					labelStr = " [" + strings.Join(m.Labels, ",") + "]"
				}
				fmt.Printf("%-9s %s  %s%s\n", m.Tier, shortHash(m.ID), m.Subject, labelStr)
			}
		},
	}
	cmd.Flags().StringVar(&tier, "tier", "", "restrict to one tier (session | personal | project | inherited | external)")
	cmd.Flags().StringVar(&includeSessions, "include-sessions", "", `widen session visibility ("all" or a specific id)`)
	cmd.Flags().BoolVar(&includeExpired, "include-expired", false, "include expired memos")
	cmd.Flags().BoolVar(&onlyExpired, "expired", false, "show only expired memos")
	cmd.Flags().BoolVar(&includeExternal, "include-external", false, "include memos from incidentally-followed repos (default merge plus external)")
	cmd.Flags().StringVar(&labels, "labels", "", "filter by labels (comma-separated, AND semantics)")
	cmd.Flags().IntVar(&limit, "limit", 0, "max number of memos to return")
	return cmd
}

// newMemoShowCmd creates the command to show a single memo.
func newMemoShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <ref>",
		Short: "Show a single memo",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			cfg := GetConfig(cmd)
			workspaceURL := gitmsg.ResolveRepoURL(cfg.WorkDir)
			res := memo.GetSingleMemo(args[0], workspaceURL, memo.ListInherits(cfg.WorkDir))
			if !res.Success {
				PrintError(cmd, res.Error.Message)
				os.Exit(1)
			}
			if cfg.JSONOutput {
				PrintJSON(res.Data)
				return
			}
			m := res.Data
			fmt.Printf("ID:     %s\n", m.ID)
			fmt.Printf("Tier:   %s\n", m.Tier)
			fmt.Printf("Author: %s <%s>\n", m.Author.Name, m.Author.Email)
			fmt.Printf("Time:   %s\n", m.Timestamp.Format(time.RFC3339))
			if len(m.Labels) > 0 {
				fmt.Printf("Labels: %s\n", strings.Join(m.Labels, ","))
			}
			fmt.Println()
			fmt.Println(m.Subject)
			if m.Body != "" {
				fmt.Println()
				fmt.Println(m.Body)
			}
		},
	}
}

// shortHash returns the first 12 hex chars of the commit hash in a protocol ref.
func shortHash(id string) string {
	// id is `[repo_url]#commit:<hash>` per protocol.CreateRef
	if i := strings.LastIndex(id, ":"); i >= 0 && i+1 < len(id) {
		h := id[i+1:]
		if len(h) > 12 {
			return h[:12]
		}
		return h
	}
	return id
}

// parseDurationFriendly accepts go's time.Duration syntax plus the suffix `d`
// (days), so users can pass `30d` directly.
func parseDurationFriendly(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if strings.HasSuffix(s, "d") {
		days := strings.TrimSuffix(s, "d")
		var n int
		if _, err := fmt.Sscanf(days, "%d", &n); err != nil || n < 0 {
			return 0, fmt.Errorf("invalid duration: %s", s)
		}
		return time.Duration(n) * 24 * time.Hour, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("invalid duration: %s", s)
	}
	return d, nil
}

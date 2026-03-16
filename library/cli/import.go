// import.go - CLI commands for importing data from external platforms
package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"

	"github.com/gitsocial-org/gitsocial/core/git"
	"github.com/gitsocial-org/gitsocial/core/protocol"
	importpkg "github.com/gitsocial-org/gitsocial/import"
	ghimport "github.com/gitsocial-org/gitsocial/import/github"
	glimport "github.com/gitsocial-org/gitsocial/import/gitlab"
)

type importFlags struct {
	limit      int
	since      string
	dryRun     bool
	update     bool
	yes        bool
	mapFile    string
	labels     string
	skipBots   bool
	host       string
	apiURL     string
	token      string
	verbose    bool
	state      string
	categories string
	emailMap   string
}

// addImportFlags registers all import flags on a command.
func addImportFlags(cmd *cobra.Command, f *importFlags, hasSocial bool, defaultLimit int) {
	limitHelp := "Max items per type (0 = unlimited)"
	cmd.Flags().IntVarP(&f.limit, "limit", "n", defaultLimit, limitHelp)
	cmd.Flags().StringVar(&f.since, "since", "", "Only import items created after date (YYYY-MM-DD)")
	cmd.Flags().BoolVar(&f.dryRun, "dry-run", false, "Print what would be imported without creating commits")
	cmd.Flags().BoolVar(&f.update, "update", false, "Sync changes from platform for already-imported items")
	cmd.Flags().BoolVarP(&f.yes, "yes", "y", false, "Skip confirmation prompt")
	cmd.Flags().StringVar(&f.mapFile, "map-file", "", "Path to ID mapping file (default: ~/.cache/gitsocial/import/<repo>.json)")
	cmd.Flags().StringVar(&f.labels, "labels", "auto", "Label mapping: auto, raw, skip")
	cmd.Flags().BoolVar(&f.skipBots, "skip-bots", true, "Skip items authored by bots")
	cmd.Flags().StringVar(&f.host, "host", "", "Force host type: github, gitlab, gitea, bitbucket")
	cmd.Flags().StringVar(&f.apiURL, "api-url", "", "Custom API base URL for self-hosted instances")
	cmd.Flags().StringVar(&f.token, "token", "", "API token (default: read from platform CLI or env)")
	cmd.Flags().BoolVarP(&f.verbose, "verbose", "v", false, "Print each item as it's imported")
	cmd.Flags().StringVar(&f.state, "state", "all", "Filter by state: open, closed, merged, all")
	cmd.Flags().StringVar(&f.emailMap, "email-map", "", "Path to username=email mapping file for author email overrides")
	if hasSocial {
		cmd.Flags().StringVar(&f.categories, "categories", "announcements,feature-requests,q-a", "Discussion category slugs to import (comma-separated)")
	}
}

// newImportCmd creates the top-level import command with subcommands.
func newImportCmd() *cobra.Command {
	var f importFlags
	allExtensions := []string{"pm", "release", "review", "social"}
	cmd := &cobra.Command{
		Use:   "import [url]",
		Short: "Import data from external platforms",
		Long: `Import issues, releases, PRs, and discussions from GitHub, GitLab, Gitea, and other platforms.

When no URL is provided, the origin remote of the current repository is used.
When no subcommand is given, imports everything (same as "import all").

Examples:
  gitsocial import                                      # import all, uses origin remote
  gitsocial import https://github.com/org/repo          # import all from URL
  gitsocial import all https://github.com/org/repo
  gitsocial import pm https://github.com/org/repo
  gitsocial import release https://gitlab.com/org/repo
  gitsocial import review https://codeberg.org/org/repo`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runImport(cmd, args, "all", allExtensions, &f)
		},
	}
	addImportFlags(cmd, &f, true, 0)
	cmd.AddCommand(
		newImportSubCmd("all", "Import everything in dependency order", allExtensions, 0),
		newImportSubCmd("pm", "Import milestones and issues", []string{"pm"}, 50),
		newImportSubCmd("release", "Import releases", []string{"release"}, 50),
		newImportSubCmd("review", "Import forks and pull requests", []string{"review"}, 50),
		newImportSubCmd("social", "Import discussions as posts", []string{"social"}, 50),
	)
	return cmd
}

func newImportSubCmd(name, short string, extensions []string, defaultLimit int) *cobra.Command {
	var f importFlags
	hasSocial := false
	for _, e := range extensions {
		if e == "social" {
			hasSocial = true
			break
		}
	}
	cmd := &cobra.Command{
		Use:   name + " [url]",
		Short: short,
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runImport(cmd, args, name, extensions, &f)
		},
	}
	addImportFlags(cmd, &f, hasSocial, defaultLimit)
	return cmd
}

// runImport is the shared import logic for both the parent command and subcommands.
func runImport(cmd *cobra.Command, args []string, label string, extensions []string, f *importFlags) error {
	if !EnsureGitRepo(cmd) {
		os.Exit(ExitNotRepo)
	}
	cfg := GetConfig(cmd)
	var rawURL string
	if len(args) > 0 {
		rawURL = args[0]
	} else {
		rawURL = git.GetOriginURL(cfg.WorkDir)
		if rawURL == "" {
			return fmt.Errorf("no URL provided and no origin remote found — specify a URL or run from a repo with a remote")
		}
	}
	if !strings.Contains(rawURL, "://") && !strings.HasPrefix(rawURL, "git@") {
		rawURL = "https://" + rawURL
	}
	repoURL := protocol.NormalizeURL(rawURL)
	repoInfo := protocol.ParseRepo(repoURL)
	if repoInfo == nil {
		return fmt.Errorf("could not parse owner/repo from URL: %s", rawURL)
	}
	hostType, err := importpkg.ResolveHost(repoURL, f.host)
	if err != nil {
		return err
	}
	adapter, err := createAdapter(hostType, repoInfo.Owner, repoInfo.Repo, f.apiURL, f.token)
	if err != nil {
		return err
	}
	if f.emailMap != "" {
		emails, err := importpkg.ParseEmailMap(f.emailMap)
		if err != nil {
			return err
		}
		if len(emails) > 0 {
			switch a := adapter.(type) {
			case *ghimport.Adapter:
				a.SetUserEmails(emails)
			case *glimport.Adapter:
				a.SetUserEmails(emails)
			}
		}
	}
	isTTY := isatty.IsTerminal(os.Stderr.Fd())
	if !cfg.JSONOutput {
		fmt.Printf("Importing %s from %s\n", label, repoURL)
		fmt.Printf("Host: %s\n\n", hostType)
	}
	fetchOpts := importpkg.FetchOptions{
		RepoURL:  repoURL,
		Owner:    repoInfo.Owner,
		Repo:     repoInfo.Repo,
		Limit:    f.limit,
		SkipBots: f.skipBots,
		Token:    f.token,
		State:    f.state,
	}
	if f.since != "" {
		t, err := parseDate(f.since)
		if err != nil {
			return fmt.Errorf("invalid --since date: %w", err)
		}
		fetchOpts.Since = &t
	}
	hasSocial := false
	for _, e := range extensions {
		if e == "social" {
			hasSocial = true
			break
		}
	}
	if hasSocial && f.categories != "" {
		fetchOpts.Categories = strings.Split(f.categories, ",")
	}
	// Count items and show confirmation prompt
	if !cfg.JSONOutput {
		fmt.Printf("Counting items...\n")
	}
	counts, _ := adapter.CountItems(fetchOpts)
	mapping := importpkg.ReadMapping(cfg.CacheDir, repoURL, f.mapFile)
	mapped := importpkg.CountMapped(mapping, counts)
	if !cfg.JSONOutput {
		summary := importpkg.FormatItemCounts(counts)
		if summary != "" {
			printStatusTable(counts, mapped, importpkg.Stats{}, f.limit)
		}
	}
	if !f.yes && !f.dryRun && !cfg.JSONOutput && isTTY {
		fmt.Printf("\nProceed with import? [Y/n] ")
		reader := bufio.NewReader(os.Stdin)
		answer, _ := reader.ReadString('\n')
		answer = strings.TrimSpace(strings.ToLower(answer))
		if answer != "" && answer != "y" && answer != "yes" {
			fmt.Println("Import canceled.")
			return nil
		}
		fmt.Println()
	}
	var spinner *importSpinner
	if isTTY && !cfg.JSONOutput {
		spinner = newImportSpinner()
		spinner.Start()
	}
	opts := importpkg.Options{
		WorkDir:    cfg.WorkDir,
		RepoURL:    repoURL,
		CacheDir:   cfg.CacheDir,
		Extensions: extensions,
		MapFile:    f.mapFile,
		LabelMode:  f.labels,
		DryRun:     f.dryRun,
		Update:     f.update,
		Verbose:    f.verbose,
		FetchOpts:  fetchOpts,
		Counts:     &counts,
		OnProgress: func(ev importpkg.ProgressEvent) {
			if cfg.JSONOutput {
				return
			}
			if spinner != nil {
				spinner.Update(ev)
			} else {
				printProgressLine(ev)
			}
		},
	}
	stats, err := importpkg.Run(adapter, opts)
	if spinner != nil {
		spinner.Stop()
	}
	if err != nil {
		return err
	}
	mapPath := importpkg.ResolveMappingPath(cfg.CacheDir, repoURL, f.mapFile)
	if cfg.JSONOutput {
		PrintJSON(stats)
	} else {
		for _, e := range stats.Errors {
			fmt.Printf("  error  %s %s: %s\n", e.Type, e.ExternalID, e.Message)
		}
		imported := importpkg.ItemCounts{
			Issues:      addImported(mapped.Issues, stats.Issues),
			PRs:         addImported(mapped.PRs, stats.PRs),
			Releases:    addImported(mapped.Releases, stats.Releases),
			Discussions: addImported(mapped.Discussions, stats.Posts),
		}
		fmt.Println()
		printStatusTable(counts, imported, stats, 0)
		if !f.dryRun {
			fmt.Printf("Map file: %s\n", mapPath)
		}
	}
	return nil
}

var extLabels = map[string]string{
	"pm":      "milestones & issues",
	"release": "releases",
	"review":  "pull requests",
	"social":  "posts & comments",
}

var phaseVerbs = map[importpkg.ProgressPhase]string{
	importpkg.PhaseCount:  "Counting",
	importpkg.PhaseFetch:  "Fetching",
	importpkg.PhaseCommit: "Importing",
}

// printProgressLine prints a simple non-TTY progress line.
func printProgressLine(ev importpkg.ProgressEvent) {
	if ev.Phase == importpkg.PhaseCount {
		if ev.Detail != "" {
			fmt.Printf("  %s\n", ev.Detail)
		}
		return
	}
	if ev.Phase != importpkg.PhaseDone {
		return
	}
	desc := extLabels[ev.Extension]
	line := formatExtStats(ev.Extension, ev.Stats)
	if line != "" {
		fmt.Printf("  %s %s\n", desc, line)
	}
}

// importSpinner shows animated progress for TTY output.
type importSpinner struct {
	mu      sync.Mutex
	stop    chan struct{}
	done    chan struct{}
	message string
	frames  []string
	frame   int
}

func newImportSpinner() *importSpinner {
	return &importSpinner{
		frames: []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"},
		stop:   make(chan struct{}),
		done:   make(chan struct{}),
	}
}

func (s *importSpinner) Update(ev importpkg.ProgressEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ev.Phase == importpkg.PhaseCount {
		if ev.Detail != "" {
			s.clearLine()
			fmt.Printf("  %s\n", ev.Detail)
		}
		return
	}
	if ev.Phase == importpkg.PhaseDone {
		s.clearLine()
		desc := extLabels[ev.Extension]
		line := formatExtStats(ev.Extension, ev.Stats)
		if line != "" {
			fmt.Printf("  ✓ %s %s\n", desc, line)
		} else {
			fmt.Printf("  ✓ %s (nothing to import)\n", desc)
		}
		return
	}
	verb := phaseVerbs[ev.Phase]
	desc := extLabels[ev.Extension]
	suffix := ""
	if ev.ItemCount > 0 {
		if ev.ItemTotal > 0 {
			suffix = fmt.Sprintf(" (%s/%s)", formatCount(ev.ItemCount), formatCount(ev.ItemTotal))
		} else {
			suffix = fmt.Sprintf(" (%s so far)", formatCount(ev.ItemCount))
		}
	}
	s.message = fmt.Sprintf("%s %s...%s", verb, desc, suffix)
}

func (s *importSpinner) Start() {
	go func() {
		defer close(s.done)
		ticker := time.NewTicker(80 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-s.stop:
				s.mu.Lock()
				s.clearLine()
				s.mu.Unlock()
				return
			case <-ticker.C:
				s.mu.Lock()
				if s.message != "" {
					s.clearLine()
					fmt.Fprintf(os.Stderr, "  %s %s", s.frames[s.frame], s.message)
					s.frame = (s.frame + 1) % len(s.frames)
				}
				s.mu.Unlock()
			}
		}
	}()
}

func (s *importSpinner) Stop() {
	close(s.stop)
	<-s.done
}

func (s *importSpinner) clearLine() {
	fmt.Fprintf(os.Stderr, "\r\033[K")
}

// printStatusTable displays the import status table with columns: Found, Imported, [Updated], Remaining, Skipped.
func printStatusTable(found, imported importpkg.ItemCounts, stats importpkg.Stats, limit int) {
	totalUpdated := stats.UpdatedIssues + stats.UpdatedPRs + stats.UpdatedMilestones + stats.UpdatedReleases + stats.UpdatedPosts
	showUpdated := totalUpdated > 0
	type entry struct {
		label    string
		found    int
		imported int
		updated  int
		skipped  int
	}
	var entries []entry
	add := func(label string, f, imp, upd, skip int) {
		if f < 0 {
			return
		}
		if imp < 0 {
			imp = 0
		}
		entries = append(entries, entry{label, f, imp, upd, skip})
	}
	add("Issues", found.Issues, imported.Issues, stats.UpdatedIssues+stats.UpdatedMilestones, stats.FilteredIssues)
	add("PRs", found.PRs, imported.PRs, stats.UpdatedPRs, stats.FilteredPRs)
	add("Releases", found.Releases, imported.Releases, stats.UpdatedReleases, stats.FilteredReleases)
	add("Discussions", found.Discussions, imported.Discussions, stats.UpdatedPosts, stats.FilteredDiscussions)
	if len(entries) == 0 {
		return
	}
	var totalFound, totalImported, totalSkipped int
	for _, e := range entries {
		totalFound += e.found
		totalImported += e.imported
		totalSkipped += e.skipped
	}
	type frow struct{ cols []string }
	buildRow := func(label string, f, imp, upd, skip int) frow {
		rem := f - imp - skip
		if rem < 0 {
			rem = 0
		}
		if showUpdated {
			return frow{cols: []string{label, formatCount(f), formatCount(imp), formatCount(upd), formatCount(rem), formatCount(skip)}}
		}
		return frow{cols: []string{label, formatCount(f), formatCount(imp), formatCount(rem), formatCount(skip)}}
	}
	var header frow
	if showUpdated {
		header = frow{cols: []string{"", "Found", "Imported", "Updated", "Remaining", "Skipped"}}
	} else {
		header = frow{cols: []string{"", "Found", "Imported", "Remaining", "Skipped"}}
	}
	var rows []frow
	rows = append(rows, header)
	for _, e := range entries {
		rows = append(rows, buildRow(e.label, e.found, e.imported, e.updated, e.skipped))
	}
	totalRow := buildRow("Total", totalFound, totalImported, totalUpdated, totalSkipped)
	ncols := len(header.cols)
	widths := make([]int, ncols)
	for _, r := range append(append([]frow{}, rows...), totalRow) {
		for i, c := range r.cols {
			if len(c) > widths[i] {
				widths[i] = len(c)
			}
		}
	}
	printRow := func(r frow) {
		fmt.Printf("  %-*s", widths[0], r.cols[0])
		for i := 1; i < len(r.cols); i++ {
			fmt.Printf("  %*s", widths[i], r.cols[i])
		}
		fmt.Println()
	}
	for _, r := range rows {
		printRow(r)
	}
	fmt.Printf("  %s", strings.Repeat("─", widths[0]))
	for i := 1; i < ncols; i++ {
		fmt.Printf("  %s", strings.Repeat("─", widths[i]))
	}
	fmt.Println()
	printRow(totalRow)
	if limit > 0 {
		fmt.Printf("  (limited to %d per type)\n", limit)
	}
}

// addImported adds newly imported count to mapped count, preserving -1 (unknown).
func addImported(mapped, newCount int) int {
	if mapped < 0 {
		return -1
	}
	return mapped + newCount
}

// formatExtStats returns a short summary for a completed extension.
func formatExtStats(ext string, stats importpkg.Stats) string {
	var parts []string
	switch ext {
	case "pm":
		if stats.Milestones > 0 {
			parts = append(parts, fmt.Sprintf("%d milestones", stats.Milestones))
		}
		if stats.Issues > 0 {
			parts = append(parts, fmt.Sprintf("%d issues", stats.Issues))
		}
		updated := stats.UpdatedIssues + stats.UpdatedMilestones
		if updated > 0 {
			parts = append(parts, fmt.Sprintf("%d updated", updated))
		}
	case "release":
		if stats.Releases > 0 {
			parts = append(parts, fmt.Sprintf("%d releases", stats.Releases))
		}
		if stats.UpdatedReleases > 0 {
			parts = append(parts, fmt.Sprintf("%d updated", stats.UpdatedReleases))
		}
	case "review":
		if stats.Forks > 0 {
			parts = append(parts, fmt.Sprintf("%d forks", stats.Forks))
		}
		if stats.PRs > 0 {
			parts = append(parts, fmt.Sprintf("%d pull requests", stats.PRs))
		}
		if stats.UpdatedPRs > 0 {
			parts = append(parts, fmt.Sprintf("%d updated", stats.UpdatedPRs))
		}
	case "social":
		if stats.Posts > 0 {
			parts = append(parts, fmt.Sprintf("%d posts", stats.Posts))
		}
		if stats.Comments > 0 {
			parts = append(parts, fmt.Sprintf("%d comments", stats.Comments))
		}
		if stats.UpdatedPosts > 0 {
			parts = append(parts, fmt.Sprintf("%d updated", stats.UpdatedPosts))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return "— " + strings.Join(parts, ", ")
}

// formatCount formats an integer with comma separators for display.
func formatCount(n int) string {
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	var result []byte
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			result = append(result, ',')
		}
		result = append(result, byte(c))
	}
	return string(result)
}

func createAdapter(host protocol.HostingService, owner, repo, apiURL, token string) (importpkg.SourceAdapter, error) {
	switch host {
	case protocol.HostGitHub:
		if err := ghimport.CheckGH(); err != nil {
			return nil, err
		}
		return ghimport.New(owner, repo), nil
	case protocol.HostGitLab:
		return glimport.New(owner, repo, glimport.AdapterOptions{BaseURL: apiURL, Token: token}), nil
	case protocol.HostGitea:
		return nil, fmt.Errorf("gitea import not yet implemented — coming soon")
	case protocol.HostBitbucket:
		return nil, fmt.Errorf("bitbucket import not yet supported")
	default:
		return nil, fmt.Errorf("unsupported platform — use --host to specify")
	}
}

func parseDate(s string) (time.Time, error) {
	return time.Parse("2006-01-02", s)
}

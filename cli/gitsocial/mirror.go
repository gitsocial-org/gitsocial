// mirror.go - gitsocial mirror: the forge→local→bucket sync loop (clone/fetch,
// import, push) as one idempotent, cron-able command
package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"

	"github.com/gitsocial-org/gitsocial/library/client"
	"github.com/gitsocial-org/gitsocial/library/core/fetch"
	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/objstore"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
	importpkg "github.com/gitsocial-org/gitsocial/library/import"
)

// mirrorSitePassCap bounds the site page drain loop so it terminates.
const mirrorSitePassCap = 16

type mirrorFlags struct {
	dir               string
	url               string
	noCode            bool
	defaultBranchOnly bool
	noImport          bool
	limit             int
	yes               bool
	noSite            bool
	dryRun            bool
	fullFetch         bool
}

// mirrorTarget is one bucket a mirror run pushes to: the git remote name and
// its canonical s3:// URL.
type mirrorTarget struct {
	name   string
	url    string
	exists bool
}

// newMirrorCmd creates the mirror command: forge → local workspace → bucket.
func newMirrorCmd() *cobra.Command {
	var f mirrorFlags
	cmd := &cobra.Command{
		Use:   "mirror [forge-url] [s3-url]",
		Short: "Mirror a forge project into a bucket",
		Long: `Mirror a forge-hosted project into an S3 bucket as a browsable site.
mirror fetches from the forge, imports issues, pull requests, releases
and discussions, then pushes data and code, and rebuilds the site.
Re-running refreshes, and a crashed run resumes.

The two URLs are told apart by scheme, so their order is free:
  gitsocial mirror <forge-url> <s3-url>  cold start: clone, import, push
  gitsocial mirror <s3-url>              attach the bucket, import, push
  gitsocial mirror                       refresh a mirrored workspace

Origin stays the forge URL and the bucket is a second remote. Every
upstream branch is mirrored unless --default-branch-only.

Credentials resolve in order:
  1. GITSOCIAL_S3_ACCESS_KEY and GITSOCIAL_S3_SECRET_KEY
  2. ~/.config/gitsocial/credentials.json, per endpoint host
  3. AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY
On a TTY without -y mirror prompts for them; otherwise it fails naming
the gitsocial config credentials set command.

Creating the bucket, allowing public reads and attaching a domain are
provider dashboard steps. --dry-run prints that checklist and the plan.

Examples:
  gitsocial mirror https://github.com/org/repo s3://<host>/<bucket>/repo
  gitsocial mirror --url https://project.example.org/
  gitsocial mirror -n 50 --dry-run`,
		Args: cobra.MaximumNArgs(2),
		// A long-running composite command: a mid-run failure (gh auth, S3
		// credentials, push) is not a usage error, so keep the output to the
		// one message main prints instead of usage + a duplicated error.
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMirror(cmd, args, &f)
		},
	}
	cmd.Flags().StringVar(&f.dir, "dir", "", "Workspace directory for the cold start")
	cmd.Flags().StringVar(&f.url, "url", "", "Public URL the bucket is served at")
	cmd.Flags().BoolVar(&f.noCode, "no-code", false, "Skip pushing code branches")
	cmd.Flags().BoolVar(&f.defaultBranchOnly, "default-branch-only", false, "Mirror only the default branch")
	cmd.Flags().BoolVar(&f.noImport, "no-import", false, "Skip the forge import step")
	cmd.Flags().IntVarP(&f.limit, "limit", "n", 0, "Max items per type to import")
	cmd.Flags().BoolVarP(&f.yes, "yes", "y", false, "Do not prompt")
	cmd.Flags().BoolVar(&f.noSite, "no-site", false, "Skip the site rebuild")
	cmd.Flags().BoolVar(&f.fullFetch, "full-fetch", false, "Also fetch forks, followed repos and identity bindings")
	cmd.Flags().BoolVar(&f.dryRun, "dry-run", false, "Print the provider checklist and the plan")
	return cmd
}

// runMirror resolves the plan from the arguments and the repo, then executes
// the mirror steps: workspace, credentials, target remote, import, site
// config, push, report.
func runMirror(cmd *cobra.Command, args []string, f *mirrorFlags) error {
	forgeURL, s3URL, err := classifyMirrorArgs(args)
	if err != nil {
		return err
	}
	cfg := GetConfig(cmd)
	if f.dir != "" && forgeURL == "" {
		return fmt.Errorf("--dir applies only with a forge URL: gitsocial mirror <forge-url> --dir <path>")
	}

	// Step 1 (resolve): the workspace directory and what to do with it.
	var wsDir, wsAction string
	if forgeURL != "" {
		wsDir, wsAction, err = resolveMirrorWorkspace(cfg.WorkDir, f.dir, forgeURL)
	} else {
		wsDir, forgeURL, err = resolveMirroredWorkspace(cfg.WorkDir)
		wsAction = "fetch"
	}
	if err != nil {
		return err
	}
	if wsAction == "clone" && s3URL == "" {
		return fmt.Errorf("a cold start needs the bucket too: gitsocial mirror %s s3://<endpoint-host>/<bucket>/<prefix>", forgeURL)
	}
	wsCfg := &Config{WorkDir: wsDir, CacheDir: cfg.CacheDir, JSONOutput: cfg.JSONOutput}

	// Step 2 (resolve): the target remotes, reusing any remote whose canonical
	// URL already matches so re-runs never accumulate remotes.
	targets, err := resolveMirrorTargets(wsDir, s3URL, wsAction == "clone")
	if err != nil {
		return err
	}

	if f.dryRun {
		printMirrorPlan(cmd.OutOrStdout(), forgeURL, wsDir, wsAction, targets, f)
		return nil
	}

	// Step 3: credentials, then a live bucket probe — both BEFORE the clone.
	// Every later step is expensive and none of it is usable if the bucket is
	// wrong, so the cheap check that can reject the run goes first.
	for _, host := range mirrorEndpointHosts(targets) {
		if err := ensureMirrorCredentials(cmd, wsCfg, host, !f.yes); err != nil {
			return err
		}
	}
	if err := probeMirrorTargets(cmd, wsCfg, targets); err != nil {
		return err
	}

	// Step 4: clone or fetch the workspace, then hold the advisory lock for
	// everything that mutates it.
	if wsAction == "clone" {
		if !wsCfg.JSONOutput {
			fmt.Fprintf(cmd.OutOrStdout(), "Cloning %s into %s ...\n", forgeURL, wsDir)
		}
		ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Minute)
		defer cancel()
		if _, err := git.ExecGitContext(ctx, cfg.WorkDir, []string{"clone", forgeURL, wsDir}); err != nil {
			return fmt.Errorf("clone %s: %w", forgeURL, err)
		}
	}
	release, err := acquireMirrorLock(wsDir)
	if err != nil {
		return err
	}
	defer release()

	if !wsCfg.JSONOutput {
		fmt.Fprintln(cmd.OutOrStdout(), "Fetching latest updates...")
	}
	mirrorFetch(cmd, wsCfg, f, true)
	if !wsCfg.JSONOutput {
		if wsAction == "clone" {
			fmt.Fprintf(cmd.OutOrStdout(), "Workspace: cloned into %s\n", wsDir)
		} else {
			fmt.Fprintf(cmd.OutOrStdout(), "Workspace: %s (fetched)\n", wsDir)
		}
	}

	// Step 5: attach the bucket (remote + push default + site.publish), all
	// set-if-absent.
	if err := ensureMirrorTargets(cmd, wsCfg, targets, f.noSite); err != nil {
		return err
	}

	// Step 6: import — resumable and idempotent via the mapping file.
	var importStats importpkg.Stats
	importRan := false
	if !f.noImport {
		importStats, err = runMirrorImport(cmd, wsCfg, forgeURL, f)
		if err != nil {
			return err
		}
		importRan = true
		// Post-import fetch so the cache reflects the freshly imported items
		// (same tail the import command runs).
		if !wsCfg.JSONOutput {
			fmt.Fprintln(cmd.OutOrStdout(), "\nIngesting imported items...")
		}
		mirrorFetch(cmd, wsCfg, f, false)
	} else if !wsCfg.JSONOutput {
		fmt.Fprintln(cmd.OutOrStdout(), "Import: skipped (--no-import)")
	}

	// Step 7: site config — read-before-write lives inside WriteExtConfig, so
	// repeated sets are commit-free no-ops.
	publicURL, err := applyMirrorSiteConfig(cmd, wsCfg, f)
	if err != nil {
		return err
	}

	// Step 8: push data and code, then rebuild the site.
	if err := syncMirrorBranches(cmd, wsCfg, f.defaultBranchOnly); err != nil {
		return err
	}
	pushErr := runMirrorPush(cmd, wsCfg, targets, f)

	// Step 9: report.
	if wsCfg.JSONOutput {
		names := make([]string, len(targets))
		for i, t := range targets {
			names[i] = t.name
		}
		summary := map[string]any{"workspace": wsDir, "forge": forgeURL, "remotes": names}
		if importRan {
			summary["import"] = importStats
		}
		if publicURL != "" {
			summary["publicURL"] = publicURL
		}
		if err := PrintJSON(cmd, summary); err != nil {
			return err
		}
	} else {
		printMirrorReport(cmd.OutOrStdout(), wsDir, publicURL, f.noSite)
	}
	if pushErr != nil {
		return pushErr
	}
	if len(importStats.Errors) > 0 {
		return fmt.Errorf("import reported %d errors: re-run to retry the affected items", len(importStats.Errors))
	}
	return nil
}

// classifyMirrorArgs assigns the positionals to forge and s3 roles by scheme,
// in either order. Anything that is neither is refused: the failure mode to
// design against is a mistyped bucket target force-pushing imported gitmsg
// branches at the upstream forge repo.
func classifyMirrorArgs(args []string) (forgeURL, s3URL string, err error) {
	for _, arg := range args {
		kind, value, err := classifyMirrorTarget(arg)
		if err != nil {
			return "", "", err
		}
		switch kind {
		case "forge":
			if forgeURL != "" {
				return "", "", fmt.Errorf("two forge URLs given, %s and %s: pass at most one forge URL and one s3 URL", forgeURL, value)
			}
			forgeURL = value
		case "s3":
			if s3URL != "" {
				return "", "", fmt.Errorf("two s3 URLs given, %s and %s: pass at most one forge URL and one s3 URL", s3URL, value)
			}
			s3URL = value
		}
	}
	return forgeURL, s3URL, nil
}

// classifyMirrorTarget identifies one positional as a forge URL (https, with
// an owner/repo path) or an s3 bucket URL (canonicalized), refusing anything
// else with a message naming both accepted shapes.
func classifyMirrorTarget(arg string) (kind, value string, err error) {
	canonical, isS3, err := protocol.ResolveS3URL(arg)
	if err != nil {
		return "", "", err
	}
	if isS3 {
		return "s3", canonical, nil
	}
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(arg)), "https://") {
		if normalized := protocol.NormalizeURL(arg); protocol.ParseRepo(normalized) != nil {
			return "forge", normalized, nil
		}
	}
	return "", "", fmt.Errorf("%q is neither an https forge URL nor an s3 bucket URL: gitsocial mirror <forge-url> <s3-url>", arg)
}

// normalizedRepoURLEqual compares two repo URLs for identity, tolerating the
// .git suffix, host/path case, and a trailing slash.
func normalizedRepoURLEqual(a, b string) bool {
	na := strings.TrimSuffix(protocol.NormalizeURL(a), "/")
	nb := strings.TrimSuffix(protocol.NormalizeURL(b), "/")
	return na != "" && strings.EqualFold(na, nb)
}

// resolveMirrorWorkspace decides what to do with the cold-start workspace
// directory: clone when absent, fetch when it is already this forge repo, and
// a hard error otherwise — mirror never adopts a foreign directory and never
// clones over one.
func resolveMirrorWorkspace(baseDir, dirFlag, forgeURL string) (wsDir, action string, err error) {
	dir := dirFlag
	if dir == "" {
		if dir, err = cloneDir(forgeURL); err != nil {
			return "", "", err
		}
	}
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(baseDir, dir)
	}
	info, err := os.Stat(dir)
	if os.IsNotExist(err) {
		return dir, "clone", nil
	}
	if err != nil {
		return "", "", fmt.Errorf("stat %s: %w", dir, err)
	}
	if !info.IsDir() || !git.IsRepository(dir) {
		return "", "", fmt.Errorf("%s is not a git repository: pick another --dir", dir)
	}
	origin := git.GetOriginURL(dir)
	if !normalizedRepoURLEqual(origin, forgeURL) {
		return "", "", fmt.Errorf("%s has origin %q, not %q: pick another --dir", dir, origin, forgeURL)
	}
	return dir, "fetch", nil
}

// resolveMirroredWorkspace validates the current directory as a mirrored
// workspace (a git repo whose origin is an https forge URL) and returns its
// forge URL, for the s3-only and no-argument forms.
func resolveMirroredWorkspace(workdir string) (wsDir, forgeURL string, err error) {
	if !git.IsRepository(workdir) {
		return "", "", fmt.Errorf("not a git repository: run inside a mirrored workspace or pass the forge URL")
	}
	origin := git.GetOriginURL(workdir)
	if origin == "" {
		return "", "", fmt.Errorf("no origin remote: add one for the forge repository")
	}
	kind, value, err := classifyMirrorTarget(origin)
	if err != nil || kind != "forge" {
		return "", "", fmt.Errorf("origin %q is not an https forge URL: point origin at the forge repository", origin)
	}
	return workdir, value, nil
}

// resolveMirrorTargets returns the buckets this run pushes to. With an s3 URL
// it is that bucket: an existing remote with the same canonical URL is reused,
// otherwise a free name is picked to add. Without one (the refresh form) it is
// every s3 remote among the configured push remotes.
func resolveMirrorTargets(wsDir, s3URL string, freshClone bool) ([]mirrorTarget, error) {
	if s3URL != "" {
		if freshClone {
			return []mirrorTarget{{name: "s3", url: s3URL}}, nil
		}
		if name, found := findMatchingS3Remote(wsDir, s3URL); found {
			return []mirrorTarget{{name: name, url: s3URL, exists: true}}, nil
		}
		return []mirrorTarget{{name: pickFreeRemoteName(wsDir), url: s3URL}}, nil
	}
	targets := s3PushRemotes(wsDir)
	if len(targets) == 0 {
		return nil, fmt.Errorf("no s3 push remote configured: run gitsocial mirror s3://<endpoint-host>/<bucket>/<prefix>")
	}
	return targets, nil
}

// findMatchingS3Remote scans the configured remotes for one whose canonical
// s3 URL equals the target, so re-runs reuse it instead of adding another.
func findMatchingS3Remote(wsDir, canonical string) (string, bool) {
	remotes, err := git.ListRemotes(wsDir)
	if err != nil {
		return "", false
	}
	for _, r := range remotes {
		if c, isS3, err := protocol.ResolveS3URL(r.URL); err == nil && isS3 && c == canonical {
			return r.Name, true
		}
	}
	return "", false
}

// pickFreeRemoteName returns "s3", or the first numbered variant no existing
// remote claims.
func pickFreeRemoteName(wsDir string) string {
	taken := map[string]bool{}
	if remotes, err := git.ListRemotes(wsDir); err == nil {
		for _, r := range remotes {
			taken[r.Name] = true
		}
	}
	if !taken["s3"] {
		return "s3"
	}
	for i := 2; ; i++ {
		name := fmt.Sprintf("s3-%d", i)
		if !taken[name] {
			return name
		}
	}
}

// s3PushRemotes returns the s3 remotes among the resolved push remotes as
// mirror targets (the refresh form's "where to" — gitsocial.pushRemote).
func s3PushRemotes(wsDir string) []mirrorTarget {
	var targets []mirrorTarget
	for _, name := range git.PushRemotes(wsDir) {
		url := git.RemoteURL(wsDir, name)
		if strings.HasPrefix(url, "s3://") {
			targets = append(targets, mirrorTarget{name: name, url: url, exists: true})
		}
	}
	return targets
}

// mirrorEndpointHosts returns the distinct endpoint hosts of the targets, in
// order.
func mirrorEndpointHosts(targets []mirrorTarget) []string {
	seen := map[string]bool{}
	var hosts []string
	for _, t := range targets {
		host, _, _, err := objstore.ParseS3URL(t.url)
		if err != nil || seen[host] {
			continue
		}
		seen[host] = true
		hosts = append(hosts, host)
	}
	return hosts
}

// ensureMirrorCredentials verifies a key pair resolves for the endpoint host.
// When nothing resolves it prompts (TTY only, and only when allowed) and
// stores the pair; otherwise it fails printing the exact setup command.
// Resolving credentials are used as-is — never re-prompted, never rewritten.
func ensureMirrorCredentials(cmd *cobra.Command, cfg *Config, host string, allowPrompt bool) error {
	if objstore.HasCredentials(host) {
		return nil
	}
	if !allowPrompt || !isatty.IsTerminal(os.Stdin.Fd()) {
		return fmt.Errorf("no S3 credentials for %s: run gitsocial config credentials set %s", host, host)
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "No S3 credentials for %s.\n", host)
	reader := bufio.NewReader(cmd.InOrStdin())
	readLine := func(prompt string) string {
		fmt.Fprint(cmd.ErrOrStderr(), prompt)
		line, _ := reader.ReadString('\n')
		return strings.TrimSpace(line)
	}
	access := readLine("Access key: ")
	secret := readLine("Secret key: ")
	if access == "" || secret == "" {
		return fmt.Errorf("both the access key and the secret key are required")
	}
	creds, err := objstore.ReadCredentialsFile()
	if err != nil {
		return err
	}
	creds[host] = objstore.Credential{AccessKey: access, SecretKey: secret}
	if err := objstore.WriteCredentialsFile(creds); err != nil {
		return err
	}
	if !cfg.JSONOutput {
		fmt.Fprintf(cmd.OutOrStdout(), "Credentials: stored for %s\n", host)
	}
	return nil
}

// probeMirrorTargets verifies every bucket answers with the resolved credentials
// BEFORE the run does anything expensive. One refs listing per target is enough
// to separate the failures worth failing fast on — unreachable endpoint, wrong
// or revoked key, missing bucket — from the work that would otherwise surface
// them only at the push, after a clone, a fetch and a full import have already
// run. Read-only: it lists, it never writes.
func probeMirrorTargets(cmd *cobra.Command, cfg *Config, targets []mirrorTarget) error {
	for _, t := range targets {
		if _, err := objstore.ListRemoteRefs(t.url, objstore.HelperEnvFromOS()); err != nil {
			// Name the endpoint host, not the remote: on a cold start the remote
			// does not exist yet, so it is not something the user can act on.
			host, _, _, hostErr := objstore.ParseS3URL(t.url)
			hint := "verify the URL and that the bucket exists"
			if hostErr == nil {
				hint = fmt.Sprintf("verify the URL and the bucket, or set credentials with gitsocial config credentials set %s", host)
			}
			return fmt.Errorf("bucket %s is not usable: %w, %s", t.url, err, hint)
		}
	}
	if !cfg.JSONOutput {
		fmt.Fprintf(cmd.OutOrStdout(), "Bucket: reachable (%d target%s)\n", len(targets), plural(len(targets)))
	}
	return nil
}

// plural returns "s" for any count that is not one.
func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// mirrorFetch refreshes the workspace: origin when fetchOrigin, and forks and lists under --full-fetch.
func mirrorFetch(cmd *cobra.Command, cfg *Config, f *mirrorFlags, fetchOrigin bool) {
	if f.fullFetch {
		runFullFetch(cmd, cfg, client.FetchOptions{}, true, !f.defaultBranchOnly)
		return
	}
	if fetchOrigin {
		opts := &fetch.Options{FetchAllBranches: !f.defaultBranchOnly}
		client.SyncWorkspaceOrigin(cfg.WorkDir, opts)
	}
	if _, err := client.SyncWorkspaceLocal(cfg.WorkDir); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: workspace sync: %v\n", err)
	}
}

// ensureMirrorTargets attaches each bucket: adds the remote when no canonical
// match exists, records the s3 helper alias, appends the remote to the push
// defaults, and (unless the site is skipped) enables site.publish. Every part
// is set-if-absent, so re-runs change nothing.
func ensureMirrorTargets(cmd *cobra.Command, cfg *Config, targets []mirrorTarget, noSite bool) error {
	for i := range targets {
		t := &targets[i]
		if !t.exists {
			if _, err := git.ExecGit(cfg.WorkDir, []string{"remote", "add", t.name, t.url}); err != nil {
				return fmt.Errorf("add remote %q: %w", t.name, err)
			}
			if err := ensureLocalS3Alias(cfg.WorkDir); err != nil {
				return err
			}
		}
		if err := git.AppendConfiguredPushRemote(cfg.WorkDir, t.name); err != nil {
			return err
		}
		if !cfg.JSONOutput {
			if t.exists {
				fmt.Fprintf(cmd.OutOrStdout(), "Remote: reusing %q → %s\n", t.name, t.url)
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "Remote: added %q → %s (default push target)\n", t.name, t.url)
			}
		}
	}
	if noSite {
		return nil
	}
	alreadyOn := readSiteConfigMap(cfg.WorkDir)["publish"] == "true"
	if err := writeSiteConfigValue(cfg.WorkDir, "publish", "true"); err != nil {
		return err
	}
	if !cfg.JSONOutput && !alreadyOn {
		fmt.Fprintln(cmd.OutOrStdout(), "Site rebuild enabled (site.publish = true)")
	}
	return nil
}

// runMirrorImport imports the forge's items with `import -y` semantics through
// the import package. Per-item failures are reported and returned on Stats —
// they do not abort the run, so the push still happens and a re-run retries
// only the missing items (the mapping file skips what already imported).
func runMirrorImport(cmd *cobra.Command, cfg *Config, forgeURL string, f *mirrorFlags) (importpkg.Stats, error) {
	repoURL := protocol.NormalizeURL(forgeURL)
	repoInfo := protocol.ParseRepo(repoURL)
	if repoInfo == nil {
		return importpkg.Stats{}, fmt.Errorf("no owner/repo in %s", forgeURL)
	}
	hostType, err := importpkg.ResolveHost(repoURL, "")
	if err != nil {
		return importpkg.Stats{}, err
	}
	adapter, err := createAdapter(hostType, repoURL, repoInfo.Owner, repoInfo.Repo, "", "")
	if err != nil {
		return importpkg.Stats{}, err
	}
	if !cfg.JSONOutput {
		fmt.Fprintf(cmd.OutOrStdout(), "Importing all from %s\n", repoURL)
	}
	fetchOpts := importpkg.FetchOptions{
		RepoURL:  repoURL,
		Owner:    repoInfo.Owner,
		Repo:     repoInfo.Repo,
		Limit:    f.limit,
		SkipBots: true,
		State:    "all",
	}
	counts, _ := adapter.CountItems(fetchOpts)
	mapping, err := importpkg.ReadMapping(cfg.CacheDir, repoURL, "")
	if err != nil {
		return importpkg.Stats{}, err
	}
	if len(mapping.Items) == 0 {
		importpkg.RebuildMapping(cfg.WorkDir, mapping)
	}
	mapped := importpkg.CountMapped(mapping, counts)
	if !cfg.JSONOutput {
		printStatusTable(cmd.OutOrStdout(), counts, mapped, importpkg.Stats{}, f.limit)
		fmt.Fprintln(cmd.OutOrStdout(), mirrorImportPlanLine(counts, mapped))
	}
	var spinner *importSpinner
	if isatty.IsTerminal(os.Stderr.Fd()) && !cfg.JSONOutput {
		spinner = newImportSpinner(cmd.OutOrStdout(), cmd.ErrOrStderr())
		spinner.Start()
	}
	stats, err := importpkg.Run(adapter, importpkg.Options{
		WorkDir:    cfg.WorkDir,
		RepoURL:    repoURL,
		CacheDir:   cfg.CacheDir,
		Extensions: []string{"pm", "release", "review", "social"},
		LabelMode:  "auto",
		FetchOpts:  fetchOpts,
		Counts:     &counts,
		Mapping:    mapping,
		OnProgress: func(ev importpkg.ProgressEvent) {
			if cfg.JSONOutput {
				return
			}
			if spinner != nil {
				spinner.Update(ev)
			} else {
				printProgressLine(cmd.OutOrStdout(), ev)
			}
		},
	})
	if spinner != nil {
		spinner.Stop()
	}
	if err != nil {
		return stats, fmt.Errorf("import: %w", err)
	}
	for _, e := range stats.Errors {
		fmt.Fprintf(cmd.ErrOrStderr(), "  error  %s %s: %s\n", e.Type, e.ExternalID, e.Message)
	}
	return stats, nil
}

// mirrorImportPlanLine names the forge read that is about to happen, so the
// pause it causes reads as work rather than a hang. The adapters list every
// item and filter the already-imported ones after they arrive, so a repo with
// nothing new still pays the full read — the line says exactly that instead of
// leaving a silent gap between the status table and the result.
func mirrorImportPlanLine(found, mapped importpkg.ItemCounts) string {
	pairs := [][2]int{
		{found.Issues, mapped.Issues},
		{found.PRs, mapped.PRs},
		{found.Releases, mapped.Releases},
		{found.Discussions, mapped.Discussions},
	}
	outstanding, known := 0, false
	for _, p := range pairs {
		if p[0] < 0 || p[1] < 0 {
			continue
		}
		known = true
		if p[0] > p[1] {
			outstanding += p[0] - p[1]
		}
	}
	const tail = "the forge's full item set is re-read every run, so this is the slow step"
	switch {
	case known && outstanding == 0:
		return fmt.Sprintf("Reading the forge to confirm nothing is new (%s) ...", tail)
	case known:
		return fmt.Sprintf("Reading the forge for %s new item%s (%s) ...", formatCount(outstanding), plural(outstanding), tail)
	}
	return fmt.Sprintf("Reading the forge (%s) ...", tail)
}

// applyMirrorSiteConfig sets site.url and site.pages when --url is given (the
// crawlable layer and OG cards come up in the same run) and returns the public
// URL to report — the flag's value, or the already-configured one.
func applyMirrorSiteConfig(cmd *cobra.Command, cfg *Config, f *mirrorFlags) (string, error) {
	site := readSiteConfigMap(cfg.WorkDir)
	configured, _ := site["url"].(string)
	if f.noSite || f.url == "" {
		return configured, nil
	}
	norm, err := resolveSiteConfigValue("url", f.url)
	if err != nil {
		return "", err
	}
	current := configured == norm && site["pages"] == "true"
	if err := writeSiteConfigValue(cfg.WorkDir, "url", norm); err != nil {
		return "", err
	}
	if err := writeSiteConfigValue(cfg.WorkDir, "pages", "true"); err != nil {
		return "", err
	}
	if !cfg.JSONOutput {
		if current {
			fmt.Fprintf(cmd.OutOrStdout(), "Site config: url = %s, pages = true (already current)\n", norm)
		} else {
			fmt.Fprintf(cmd.OutOrStdout(), "Site config: url = %s, pages = true\n", norm)
		}
	}
	return norm, nil
}

// syncMirrorBranches materializes the fetched upstream branches as local
// branches so the all-branches push can send them: the push enumerates
// refs/heads/*, while the fetch lands upstream branches in
// refs/remotes/origin/*. Creation and fast-forward only — a diverged local
// branch is skipped with a warning, never overwritten. gitmsg/* branches are
// excluded (the fetch already fast-forwards them with merge handling).
func syncMirrorBranches(cmd *cobra.Command, cfg *Config, defaultOnly bool) error {
	out, err := git.ExecGit(cfg.WorkDir, []string{"for-each-ref", "--format=%(refname:strip=3)", "refs/remotes/origin"})
	if err != nil {
		return fmt.Errorf("list remote-tracking branches: %w", err)
	}
	current := ""
	if r, err := git.ExecGit(cfg.WorkDir, []string{"rev-parse", "--abbrev-ref", "HEAD"}); err == nil {
		current = strings.TrimSpace(r.Stdout)
	}
	defaultBranch, _ := git.GetDefaultBranch(cfg.WorkDir)
	created, updated := 0, 0
	for _, branch := range strings.Fields(out.Stdout) {
		if branch == "HEAD" || strings.HasPrefix(branch, "gitmsg/") {
			continue
		}
		if defaultOnly && branch != defaultBranch {
			continue
		}
		remoteTip, err := git.ReadRef(cfg.WorkDir, "refs/remotes/origin/"+branch)
		if err != nil {
			continue
		}
		localTip, err := git.ReadRef(cfg.WorkDir, "refs/heads/"+branch)
		if err != nil {
			// The checked-out branch always has a local ref, so this is safe.
			if _, err := git.ExecGit(cfg.WorkDir, []string{"branch", branch, "origin/" + branch}); err == nil {
				created++
			}
			continue
		}
		if localTip == remoteTip {
			continue
		}
		if branch == current {
			// Fast-forwarding the checked-out branch must move the worktree
			// with it; --ff-only aborts safely on divergence or dirty overlap.
			if _, err := git.ExecGit(cfg.WorkDir, []string{"merge", "--ff-only", "origin/" + branch}); err == nil {
				updated++
			} else {
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: could not fast-forward checked-out branch %s; the bucket keeps its previous tip\n", branch)
			}
			continue
		}
		if _, err := git.ExecGit(cfg.WorkDir, []string{"merge-base", "--is-ancestor", localTip, remoteTip}); err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "warning: branch %s has local commits not on origin; skipped (not overwritten)\n", branch)
			continue
		}
		if _, err := git.ExecGit(cfg.WorkDir, []string{"update-ref", "refs/heads/" + branch, remoteTip, localTip}); err == nil {
			updated++
		}
	}
	if !cfg.JSONOutput && (created > 0 || updated > 0) {
		fmt.Fprintf(cmd.OutOrStdout(), "Branches: %d created, %d fast-forwarded from origin\n", created, updated)
	}
	return nil
}

// runMirrorPush pushes to every target, then drains the site pages cursor.
func runMirrorPush(cmd *cobra.Command, cfg *Config, targets []mirrorTarget, f *mirrorFlags) error {
	var siteProgress objstore.Progress
	siteDone := func() {}
	if !cfg.JSONOutput {
		siteProgress, siteDone = objstore.StderrProgress()
	}
	defer siteDone()
	failed := false
	for _, t := range targets {
		var onBranch func(branch string, done, total int)
		if siteProgress != nil {
			onBranch = func(branch string, done, total int) { siteProgress(branch, done, total) }
		}
		opts := client.Options{
			NoCode:      f.noCode,
			NoSite:      f.noSite,
			AllBranches: !f.defaultBranchOnly,
		}
		if !cfg.JSONOutput {
			fmt.Fprintf(cmd.OutOrStdout(), "Pushing to %s (%s) ...\n", t.name, t.url)
		}
		result, err := client.Publish(cfg.WorkDir, t.name, opts, onBranch, siteProgress)
		if err != nil {
			failed = true
			fmt.Fprintf(cmd.ErrOrStderr(), "error: push to %s: %v\n", t.name, err)
			continue
		}
		// Drain BEFORE reporting: the push's own site line describes the state
		// at the end of that one pass, and mirror is about to change it. Printing
		// first would tell the user to push again in the same breath as finishing
		// the job for them.
		if result.Site.Published && !result.Site.Complete {
			complete, err := drainMirrorSitePages(cfg, t.name, siteProgress)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: site refresh on %s: %v\n", t.name, err)
			}
			result.Site.Complete = complete
		}
		if !cfg.JSONOutput {
			printPushResult(cmd.OutOrStdout(), result, false)
		}
	}
	if failed {
		return fmt.Errorf("push failed for one or more remotes")
	}
	return nil
}

// drainMirrorSitePages runs site-only passes until one reports the site
// complete (each pass advances the pages cursor by one budget) or the pass cap
// is hit, and reports which. Completion is the publish result's own signal, not
// a progress phase read in passing, so the loop cannot be fooled by wording;
// the caller folds the answer into the result it prints, so the reported site
// state is the one the run actually left behind.
func drainMirrorSitePages(cfg *Config, remote string, progress objstore.Progress) (bool, error) {
	for pass := 0; pass < mirrorSitePassCap; pass++ {
		result, err := client.Publish(cfg.WorkDir, remote, client.Options{SiteOnly: true}, nil, progress)
		if err != nil {
			return false, err
		}
		if result.Site.Complete {
			return true, nil
		}
	}
	return false, nil
}

// printMirrorPlan renders the --dry-run output: the resolved plan plus the
// provider checklist mirror cannot automate. Nothing is written.
func printMirrorPlan(out io.Writer, forgeURL, wsDir, wsAction string, targets []mirrorTarget, f *mirrorFlags) {
	fmt.Fprintln(out, "Mirror plan (dry run — nothing written):")
	fmt.Fprintf(out, "  Forge:      %s\n", forgeURL)
	if wsAction == "clone" {
		fmt.Fprintf(out, "  Workspace:  %s (will clone)\n", wsDir)
	} else {
		fmt.Fprintf(out, "  Workspace:  %s (exists; will fetch)\n", wsDir)
	}
	for _, t := range targets {
		if t.exists {
			fmt.Fprintf(out, "  Bucket:     %s (reusing remote %q)\n", t.url, t.name)
		} else {
			fmt.Fprintf(out, "  Bucket:     %s (will add remote %q as default push target)\n", t.url, t.name)
		}
	}
	for _, host := range mirrorEndpointHosts(targets) {
		if objstore.HasCredentials(host) {
			fmt.Fprintf(out, "  Credentials: found for %s\n", host)
		} else {
			fmt.Fprintf(out, "  Credentials: MISSING for %s — run: gitsocial config credentials set %s\n", host, host)
		}
	}
	switch {
	case f.noImport:
		fmt.Fprintln(out, "  Import:     skipped (--no-import)")
	case f.limit > 0:
		fmt.Fprintf(out, "  Import:     issues, PRs, releases, discussions (capped at %d per type)\n", f.limit)
	default:
		fmt.Fprintln(out, "  Import:     issues, PRs, releases, discussions (unlimited)")
	}
	switch {
	case f.noSite:
		fmt.Fprintln(out, "  Site:       skipped (--no-site)")
	case f.url != "":
		fmt.Fprintf(out, "  Site:       rebuild + crawlable pages at %s\n", f.url)
	default:
		fmt.Fprintln(out, "  Site:       rebuild (no public URL yet; pass --url to enable crawlable pages)")
	}
	scope := "all branches"
	if f.defaultBranchOnly {
		scope = "default branch only"
	}
	if f.noCode {
		scope += ", no code branches"
	}
	fmt.Fprintf(out, "  Push:       data + code, %s\n", scope)
	fmt.Fprintln(out, "\nProvider checklist (dashboard steps mirror cannot automate):")
	fmt.Fprintln(out, "  1. Create the bucket and grant the credentials write access")
	fmt.Fprintln(out, "  2. Enable public read on the bucket so browsers and git can fetch it")
	fmt.Fprintln(out, "  3. Attach a public domain, then re-run with --url https://<domain>/")
}

// printMirrorReport prints the closing report: where the workspace is, and the
// public URL when known, else the exact remaining manual steps.
func printMirrorReport(out io.Writer, wsDir, publicURL string, noSite bool) {
	fmt.Fprintf(out, "\nWorkspace: %s\n", wsDir)
	if noSite {
		return
	}
	if publicURL != "" {
		fmt.Fprintf(out, "Site: %s\n", publicURL)
		return
	}
	fmt.Fprintln(out, "To serve the site publicly:")
	fmt.Fprintln(out, "  1. Enable public read on the bucket (provider dashboard)")
	fmt.Fprintln(out, "  2. Attach a public domain (e.g. an r2.dev subdomain or a custom domain)")
	fmt.Fprintln(out, "  3. Re-run with --url https://<domain>/ to enable crawlable pages and canonical links")
}

// acquireMirrorLock takes the workspace's advisory mirror lock (a PID file in
// the git dir, created O_EXCL) so overlapping cron ticks cannot collide. A
// lock whose PID is no longer alive is taken over; a live one is a clear
// error. The returned release removes the lock.
func acquireMirrorLock(wsDir string) (func(), error) {
	out, err := git.ExecGit(wsDir, []string{"rev-parse", "--absolute-git-dir"})
	if err != nil {
		return nil, fmt.Errorf("resolve git dir: %w", err)
	}
	lockPath := filepath.Join(strings.TrimSpace(out.Stdout), "gitsocial-mirror.lock")
	for attempt := 0; attempt < 2; attempt++ {
		file, err := os.OpenFile(lockPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err == nil {
			fmt.Fprintf(file, "%d\n", os.Getpid())
			file.Close()
			return func() { os.Remove(lockPath) }, nil
		}
		if !os.IsExist(err) {
			return nil, fmt.Errorf("create lock %s: %w", lockPath, err)
		}
		data, readErr := os.ReadFile(lockPath)
		pid, _ := strconv.Atoi(strings.TrimSpace(string(data)))
		if readErr == nil && pidAlive(pid) {
			return nil, fmt.Errorf("another mirror run is active as pid %d: wait for it, or remove %s if it is stale", pid, lockPath)
		}
		os.Remove(lockPath)
	}
	return nil, fmt.Errorf("lock %s is still taken, retry the command", lockPath)
}

// pidAlive reports whether a process with the given PID exists (signal 0).
func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return process.Signal(syscall.Signal(0)) == nil
}

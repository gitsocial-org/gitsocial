// fork_create.go - gitsocial fork create: create the fork on its destination
// (through the forge's fork API where we can), clone upstream blobless, and
// wire up origin (yours) / upstream (the source).
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
	ghimport "github.com/gitsocial-org/gitsocial/library/import/github"
)

// errForkNotCreated ends the flow after the manual instructions were printed:
// there is no destination to clone against yet.
var errForkNotCreated = errors.New("the fork was not created — follow the instructions above, then re-run with --to")

type forkCreateFlags struct {
	to       string
	noFilter bool
	full     bool
}

// newForkCreateCmd creates the `fork create` command.
func newForkCreateCmd() *cobra.Command {
	var f forkCreateFlags
	cmd := &cobra.Command{
		Use:   "create <upstream-url> [directory]",
		Short: "Fork a repository: create it on the destination, clone it, and wire up remotes",
		Long: `Fork <upstream-url> and set up a local clone to work in.

The destination is a fork on the same forge as upstream (created through the
forge's fork API, which copies nothing server-side) or your own bucket:

  gitsocial fork create https://github.com/org/repo
  gitsocial fork create https://github.com/org/repo --to https://github.com/me/repo
  gitsocial fork create https://github.com/org/repo --to s3://<endpoint-host>/<bucket>/<prefix>

The clone is blobless (--filter=blob:none): blobs are fetched on demand, so a
fork costs a fraction of a full clone. Servers that cannot honor the filter
ignore it and send everything. Pass --no-filter for a complete clone.

Afterwards origin is your fork and upstream is the source, matching the
convention of forge CLIs. Nothing is pushed: commit your work, then run
gitsocial push.`,
		Args: cobra.RangeArgs(1, 2),
		// A composite command (forge API, clone, git config): a mid-run
		// failure is not a usage error, so keep the output to the one message
		// main prints.
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runForkCreate(cmd, args, &f)
		},
	}
	cmd.Flags().StringVar(&f.to, "to", "", "Destination for the fork (a forge URL, or s3://<endpoint-host>/<bucket>/<prefix>)")
	cmd.Flags().BoolVar(&f.noFilter, "no-filter", false, "Clone every blob instead of fetching them on demand")
	cmd.Flags().BoolVar(&f.full, "full", false, "Pushes to the fork upload everything (no upstream exclusion); default for an s3 destination is thin")
	return cmd
}

// runForkCreate resolves the destination (creating the fork on its forge when
// it can), clones upstream, wires the remotes, records the push relationship,
// and registers upstream as a fork.
func runForkCreate(cmd *cobra.Command, args []string, f *forkCreateFlags) error {
	cfg := GetConfig(cmd)
	out := cmd.OutOrStdout()
	upstreamURL, err := canonicalForkURL(args[0])
	if err != nil {
		return err
	}
	destURL, err := resolveForkDestination(cmd, upstreamURL, f.to)
	if err != nil {
		return err
	}

	dir := ""
	if len(args) == 2 {
		dir = args[1]
	} else if dir, err = cloneDir(upstreamURL); err != nil {
		return err
	}

	cloneArgs := []string{"--filter=blob:none"}
	if f.noFilter {
		cloneArgs = nil
	}
	ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Minute)
	defer cancel()
	repoDir, err := cloneRepo(ctx, cfg.WorkDir, upstreamURL, dir, cloneArgs...)
	if err != nil {
		return fmt.Errorf("clone %s: %w", upstreamURL, err)
	}

	if err := wireForkRemotes(repoDir, destURL); err != nil {
		return err
	}
	destIsS3 := strings.HasPrefix(destURL, "s3://")
	thin := destIsS3 && !f.full
	if destIsS3 {
		if err := recordThinRelationship(repoDir, upstreamURL, thin); err != nil {
			return err
		}
	} else if f.full && !cfg.JSONOutput {
		// Thinness is moot on a forge: its fork already shares upstream's
		// objects server-side, so a push only ever transfers new commits.
		fmt.Fprintf(out, "--full has no effect on %s: a forge fork already shares upstream's objects.\n", destURL)
	}
	if err := gitmsg.AddFork(repoDir, upstreamURL); err != nil {
		return fmt.Errorf("register %s as a fork: %w", upstreamURL, err)
	}

	if cfg.JSONOutput {
		PrintJSON(map[string]interface{}{
			"upstream":  upstreamURL,
			"origin":    destURL,
			"directory": dir,
			"blobless":  !f.noFilter,
			"thin":      thin,
		})
		return nil
	}
	blobless := ""
	if !f.noFilter {
		blobless = " (blobless: blobs fetch on demand)"
	}
	fmt.Fprintf(out, "Forked %s\n", upstreamURL)
	fmt.Fprintf(out, "  cloned into '%s'%s\n", dir, blobless)
	fmt.Fprintf(out, "  origin   %s\n", destURL)
	fmt.Fprintf(out, "  upstream %s\n", upstreamURL)
	if thin {
		fmt.Fprintln(out, "  pushes to origin are thin against upstream")
	}
	fmt.Fprintln(out, "Commit your work, then: gitsocial push")
	return nil
}

// canonicalForkURL normalizes a user-supplied URL, resolving s3 aliases (an
// AWS console URL, a bare endpoint) to the canonical s3:// form.
func canonicalForkURL(raw string) (string, error) {
	canonical, isS3, err := protocol.ResolveS3URL(raw)
	if err != nil {
		return "", err
	}
	if isS3 {
		return canonical, nil
	}
	return raw, nil
}

// resolveForkDestination returns the URL the fork lives at, calling the
// destination forge's fork API when it has one. With no --to the destination
// is upstream's own forge.
func resolveForkDestination(cmd *cobra.Command, upstreamURL, to string) (string, error) {
	upstreamHost := protocol.DetectHost(upstreamURL)
	if to == "" {
		if !canForkOnForge(upstreamHost) {
			return "", fmt.Errorf("cannot fork %s automatically: pass --to <dest-url> (a fork URL on a forge, or s3://<endpoint-host>/<bucket>/<prefix>)", upstreamURL)
		}
		return forkOnForge(cmd, upstreamURL, "")
	}
	destURL, err := canonicalForkURL(to)
	if err != nil {
		return "", err
	}
	if strings.HasPrefix(destURL, "s3://") {
		return destURL, nil
	}
	destHost := protocol.DetectHost(destURL)
	out := cmd.OutOrStdout()
	switch {
	case canForkOnForge(destHost) && destHost == upstreamHost:
		return forkOnForge(cmd, upstreamURL, destURL)
	case destHost != upstreamHost:
		// Cross-forge: no shared object pool and no fork API that reaches
		// across, so say it before the clone rather than mid-upload.
		fmt.Fprintf(out, "%s and %s are not on the same host: no fork API applies and the first push transfers the full history.\n", upstreamURL, destURL)
		fmt.Fprintf(out, "Create %s first if it does not exist yet.\n", destURL)
	default:
		fmt.Fprintf(out, "Creating a fork on %s is not automated: create %s there first if it does not exist yet.\n", protocol.ExtractDomain(destURL), destURL)
	}
	return destURL, nil
}

// canForkOnForge reports whether a forge's fork API is wired up here.
func canForkOnForge(host protocol.HostingService) bool {
	return host == protocol.HostGitHub || host == protocol.HostGitLab
}

// forkOnForge calls the forge's fork API and returns the new fork's URL.
// Missing credentials print manual instructions rather than a bare failure.
func forkOnForge(cmd *cobra.Command, upstreamURL, destURL string) (string, error) {
	switch protocol.DetectHost(upstreamURL) {
	case protocol.HostGitHub:
		return forkOnGitHub(cmd, upstreamURL, destURL)
	case protocol.HostGitLab:
		return forkOnGitLab(cmd, upstreamURL, destURL)
	default:
		printManualForkInstructions(cmd, upstreamURL, "no fork API is wired up for "+protocol.ExtractDomain(upstreamURL))
		return "", errForkNotCreated
	}
}

// forkOnGitHub forks through the gh CLI (`gh repo fork`), which shares
// upstream's object pool server-side and transfers nothing.
func forkOnGitHub(cmd *cobra.Command, upstreamURL, destURL string) (string, error) {
	if err := ghimport.CheckGH(); err != nil {
		printManualForkInstructions(cmd, upstreamURL, err.Error())
		return "", errForkNotCreated
	}
	args := []string{"repo", "fork", upstreamURL, "--clone=false", "--remote=false"}
	dest := protocol.ParseRepo(destURL)
	if dest != nil {
		if login := githubLogin(); login != "" && !strings.EqualFold(login, dest.Owner) {
			args = append(args, "--org", dest.Owner)
		}
		if up := protocol.ParseRepo(upstreamURL); up != nil && !strings.EqualFold(up.Repo, dest.Repo) {
			args = append(args, "--fork-name", dest.Repo)
		}
	}
	output, err := runForgeCommand("gh", args...)
	if err != nil {
		printManualForkInstructions(cmd, upstreamURL, err.Error())
		return "", errForkNotCreated
	}
	if slug := parseGitHubForkSlug(output); slug != "" {
		return "https://" + protocol.ExtractDomain(upstreamURL) + "/" + slug, nil
	}
	if destURL != "" {
		return destURL, nil
	}
	return "", fmt.Errorf("gh repo fork did not report the new fork: %s", strings.TrimSpace(output))
}

// githubLogin returns the authenticated gh account, empty when unavailable.
func githubLogin() string {
	out, err := runForgeCommand("gh", "api", "user", "--jq", ".login")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// parseGitHubForkSlug reads the fork's owner/repo out of gh's report
// ("Created fork owner/repo", "owner/repo already exists").
func parseGitHubForkSlug(output string) string {
	for _, line := range strings.Split(output, "\n") {
		if !strings.Contains(line, "Created fork") && !strings.Contains(line, "already exists") {
			continue
		}
		fields := strings.Fields(line)
		for i := len(fields) - 1; i >= 0; i-- {
			if strings.Count(fields[i], "/") == 1 && !strings.Contains(fields[i], ":") {
				return strings.TrimSuffix(fields[i], ".git")
			}
		}
	}
	return ""
}

// runForgeCommand runs a forge CLI, returning its combined output so a
// failure keeps the tool's own diagnostics.
func runForgeCommand(name string, args ...string) (string, error) {
	var buf bytes.Buffer
	c := exec.Command(name, args...)
	c.Stdout = &buf
	c.Stderr = &buf
	if err := c.Run(); err != nil {
		msg := strings.TrimSpace(buf.String())
		if msg == "" {
			msg = err.Error()
		}
		return buf.String(), fmt.Errorf("%s %s: %s", name, strings.Join(args, " "), msg)
	}
	return buf.String(), nil
}

// forkOnGitLab forks through the REST API (POST /projects/:id/fork), which
// shares upstream's objects server-side.
func forkOnGitLab(cmd *cobra.Command, upstreamURL, destURL string) (string, error) {
	token := gitlabToken()
	if token == "" {
		printManualForkInstructions(cmd, upstreamURL, "no GitLab token — set GITLAB_TOKEN")
		return "", errForkNotCreated
	}
	up := protocol.ParseRepo(upstreamURL)
	if up == nil {
		return "", fmt.Errorf("cannot read owner/repo from %s", upstreamURL)
	}
	body := map[string]string{}
	if dest := protocol.ParseRepo(destURL); dest != nil {
		body["namespace_path"] = dest.Owner
		body["path"] = dest.Repo
		body["name"] = dest.Repo
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("encode fork request: %w", err)
	}
	apiURL := "https://" + protocol.ExtractDomain(upstreamURL) + "/api/v4/projects/" + url.PathEscape(up.Owner+"/"+up.Repo) + "/fork"
	req, err := http.NewRequestWithContext(cmd.Context(), http.MethodPost, apiURL, bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("create fork request: %w", err)
	}
	req.Header.Set("PRIVATE-TOKEN", token)
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		printManualForkInstructions(cmd, upstreamURL, err.Error())
		return "", errForkNotCreated
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusConflict && destURL != "" {
		return destURL, nil
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		printManualForkInstructions(cmd, upstreamURL, fmt.Sprintf("POST %s: %d %s", apiURL, resp.StatusCode, strings.TrimSpace(string(raw))))
		return "", errForkNotCreated
	}
	var created struct {
		WebURL string `json:"web_url"`
	}
	if err := json.Unmarshal(raw, &created); err != nil || created.WebURL == "" {
		if destURL != "" {
			return destURL, nil
		}
		return "", fmt.Errorf("GitLab fork response has no web_url: %s", strings.TrimSpace(string(raw)))
	}
	return created.WebURL, nil
}

// gitlabToken reads the GitLab API token from the environment, matching the
// import adapter's variables.
func gitlabToken() string {
	if t := os.Getenv("GITLAB_TOKEN"); t != "" {
		return t
	}
	return os.Getenv("GITLAB_PRIVATE_TOKEN")
}

// printManualForkInstructions names why the fork API was unavailable, where to
// create the fork by hand, and how to re-run once it exists.
func printManualForkInstructions(cmd *cobra.Command, upstreamURL, reason string) {
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Cannot create the fork automatically: %s\n", reason)
	fmt.Fprintf(out, "  create it here: %s\n", forkWebURL(upstreamURL))
	fmt.Fprintf(out, "  then re-run:    gitsocial fork create %s --to <fork-url>\n", upstreamURL)
}

// forkWebURL returns the forge's own "create a fork" page for a repo URL.
func forkWebURL(repoURL string) string {
	base := protocol.NormalizeURL(repoURL)
	switch protocol.DetectHost(repoURL) {
	case protocol.HostGitHub:
		return base + "/fork"
	case protocol.HostGitLab:
		return base + "/-/forks/new"
	case protocol.HostGitea:
		return base + "/fork"
	default:
		return base
	}
}

// wireForkRemotes renames the clone's origin to upstream and points origin at
// the fork, so origin is yours and upstream is the source.
func wireForkRemotes(repoDir, destURL string) error {
	if _, err := git.ExecGit(repoDir, []string{"remote", "rename", "origin", "upstream"}); err != nil {
		return fmt.Errorf("rename origin to upstream: %w", err)
	}
	// A filtered clone records its promisor remote by name; the rename does
	// not follow it, and origin is about to be a different repository.
	if promisor := git.GetGitConfig(repoDir, "extensions.partialClone"); promisor == "origin" {
		if _, err := git.ExecGit(repoDir, []string{"config", "--local", "extensions.partialClone", "upstream"}); err != nil {
			return fmt.Errorf("repoint the promisor remote: %w", err)
		}
	}
	if _, err := git.ExecGit(repoDir, []string{"remote", "add", "origin", destURL}); err != nil {
		return fmt.Errorf("add origin %s: %w", destURL, err)
	}
	if strings.HasPrefix(destURL, "s3://") {
		if err := ensureLocalS3Alias(repoDir); err != nil {
			return err
		}
	}
	return nil
}

// recordThinRelationship records the push relationship for an s3 destination:
// which upstream it is thin against, and whether pushes exclude upstream's
// objects at all (--full opts out).
func recordThinRelationship(repoDir, upstreamURL string, thin bool) error {
	if thin {
		if _, err := git.ExecGit(repoDir, []string{"config", "--local", "remote.origin.gitsocial-thin", "true"}); err != nil {
			return fmt.Errorf("record the thin push relationship: %w", err)
		}
	}
	if _, err := git.ExecGit(repoDir, []string{"config", "--local", "remote.origin.gitsocial-upstream", upstreamURL}); err != nil {
		return fmt.Errorf("record the fork's upstream: %w", err)
	}
	return nil
}

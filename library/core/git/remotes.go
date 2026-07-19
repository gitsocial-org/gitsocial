// remotes.go - Remote repository management and fetching
package git

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// FetchRemote fetches updates from a remote repository.
func FetchRemote(workdir, remoteName string, opts *FetchOptions) error {
	args := []string{"fetch", remoteName}

	if opts != nil {
		if opts.ShallowSince != "" {
			args = append(args, "--shallow-since="+opts.ShallowSince)
		}
		if opts.Depth > 0 {
			args = append(args, fmt.Sprintf("--depth=%d", opts.Depth))
		}
		if opts.Branch != "" {
			args = append(args, opts.Branch)
		}
		if opts.Jobs > 0 {
			args = append(args, fmt.Sprintf("--jobs=%d", opts.Jobs))
		}
	}

	_, err := ExecGit(workdir, args)
	return err
}

// ListRemotes returns all configured remotes with their URLs.
func ListRemotes(workdir string) ([]Remote, error) {
	result, err := ExecGit(workdir, []string{"remote", "-v"})
	if err != nil {
		return nil, err
	}

	if result.Stdout == "" {
		return []Remote{}, nil
	}

	remotes := make(map[string]string)
	lines := strings.Split(result.Stdout, "\n")
	pattern := regexp.MustCompile(`^([^\t\s]+)\s+([^\t\s]+)\s+\(fetch\)`)

	for _, line := range lines {
		matches := pattern.FindStringSubmatch(line)
		if len(matches) >= 3 {
			remotes[matches[1]] = matches[2]
		}
	}

	result2 := make([]Remote, 0, len(remotes))
	for name, url := range remotes {
		result2 = append(result2, Remote{Name: name, URL: url})
	}
	return result2, nil
}

// GetRemoteDefaultBranch detects the default branch of a remote repository.
func GetRemoteDefaultBranch(workdir, remoteURL string) string {
	result, err := ExecGit(workdir, []string{"ls-remote", "--symref", remoteURL, "HEAD"})
	if err != nil {
		return "main"
	}

	for _, line := range strings.Split(result.Stdout, "\n") {
		if strings.HasPrefix(line, "ref: refs/heads/") {
			parts := strings.Split(line, "\t")
			if len(parts) > 0 {
				branch := strings.TrimPrefix(parts[0], "ref: refs/heads/")
				if branch != "" {
					return branch
				}
			}
		}
	}

	return "main"
}

// FetchRefspec fetches a specific refspec from a remote.
func FetchRefspec(workdir, remoteName, refspec string) error {
	_, err := ExecGit(workdir, []string{"fetch", remoteName, refspec, "--no-tags"})
	return err
}

// ListRemoteBranches returns the branch names available on a remote.
func ListRemoteBranches(workdir, remoteName string) ([]string, error) {
	result, err := ExecGit(workdir, []string{"ls-remote", "--heads", remoteName})
	if err != nil {
		return nil, fmt.Errorf("ls-remote: %w", err)
	}
	if result.Stdout == "" {
		return nil, nil
	}
	var branches []string
	for _, line := range strings.Split(result.Stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			ref := parts[1]
			branch := strings.TrimPrefix(ref, "refs/heads/")
			if branch != ref {
				branches = append(branches, branch)
			}
		}
	}
	return branches, nil
}

// ReadRemoteRef reads a branch tip hash from a remote URL using ls-remote.
func ReadRemoteRef(workdir, remoteURL, branch string) (string, error) {
	result, err := ExecGit(workdir, []string{"ls-remote", remoteURL, "refs/heads/" + branch})
	if err != nil {
		return "", fmt.Errorf("ls-remote %s: %w", branch, err)
	}
	line := strings.TrimSpace(result.Stdout)
	if line == "" {
		return "", fmt.Errorf("branch %s not found on remote", branch)
	}
	parts := strings.Fields(line)
	if len(parts) < 1 {
		return "", fmt.Errorf("unexpected ls-remote output")
	}
	return parts[0], nil
}

// ambiguityWarnOnce ensures the heuristic-ambiguity warning fires at most once
// per process, not once per branch/ref during a push.
var ambiguityWarnOnce sync.Once

// misconfiguredRemoteWarnOnce ensures the configured-remote-missing warning
// fires at most once per process, not once per PushRemote resolution.
var misconfiguredRemoteWarnOnce sync.Once

// PushRemote returns the remote gitsocial publishes to. Resolution order:
// git config gitsocial.pushRemote (a configured name that doesn't exist as a
// remote falls through to the heuristic with a stderr warning), then the
// heuristic: "origin" normally, or the configured s3 remote when one exists
// ("origin" still wins when it is itself s3; ties between multiple s3 remotes
// break alphabetically). Warns once per process when the heuristic must pick
// among 2+ s3 remotes with nothing configured.
func PushRemote(workdir string) string {
	remotes, err := ListRemotes(workdir)
	if err != nil {
		return "origin"
	}

	if configured := configuredPushRemote(workdir); configured != "" {
		for _, r := range remotes {
			if r.Name == configured {
				return configured
			}
		}
		misconfiguredRemoteWarnOnce.Do(func() {
			fmt.Fprintf(os.Stderr, "gitsocial: configured push remote %q (gitsocial.pushRemote) does not exist; falling back to heuristic\n", configured)
		})
	}

	s3Name := ""
	s3Count := 0
	for _, r := range remotes {
		if !strings.HasPrefix(r.URL, "s3://") {
			continue
		}
		s3Count++
		if r.Name == "origin" {
			return "origin"
		}
		if s3Name == "" || r.Name < s3Name {
			s3Name = r.Name
		}
	}
	if s3Name != "" {
		if s3Count >= 2 {
			ambiguityWarnOnce.Do(func() {
				fmt.Fprintf(os.Stderr, "gitsocial: multiple s3 remotes; pushing to %q — set `git config gitsocial.pushRemote <name>` to choose\n", s3Name)
			})
		}
		return s3Name
	}
	return "origin"
}

// ConfiguredPushRemote returns the first value of git config
// gitsocial.pushRemote, or "" when unset. Exposed for the `remote default`
// command's no-arg report.
func ConfiguredPushRemote(workdir string) string {
	return configuredPushRemote(workdir)
}

// ConfiguredPushRemotes returns every configured gitsocial.pushRemote value
// (git config --get-all), in config order, or nil when unset. The key is
// multi-valued so a publish can fan out to several buckets.
func ConfiguredPushRemotes(workdir string) []string {
	result, err := ExecGit(workdir, []string{"config", "--get-all", "gitsocial.pushRemote"})
	if err != nil {
		return nil
	}
	var names []string
	for _, line := range strings.Split(result.Stdout, "\n") {
		if name := strings.TrimSpace(line); name != "" {
			names = append(names, name)
		}
	}
	return names
}

// PushRemotes returns the remotes gitsocial publishes to by default: every
// configured gitsocial.pushRemote that exists as a remote, or a single-element
// slice from the PushRemote heuristic when none are configured/valid. Callers
// that push to one remote still use PushRemote; multi-remote push uses this.
func PushRemotes(workdir string) []string {
	remotes, err := ListRemotes(workdir)
	if err != nil {
		return []string{PushRemote(workdir)}
	}
	exists := map[string]bool{}
	for _, r := range remotes {
		exists[r.Name] = true
	}
	var valid []string
	for _, name := range ConfiguredPushRemotes(workdir) {
		if exists[name] {
			valid = append(valid, name)
		}
	}
	if len(valid) == 0 {
		return []string{PushRemote(workdir)}
	}
	return valid
}

// SetConfiguredPushRemotes replaces the multi-valued gitsocial.pushRemote config
// with the given names (per-clone, like remotes themselves). An empty list
// unsets the key entirely, reverting to the PushRemote heuristic.
func SetConfiguredPushRemotes(workdir string, names []string) error {
	// Clear the key first so a shorter list doesn't leave stale trailing values;
	// --unset-all on an unset key is a benign exit-5, tolerated.
	_, _ = ExecGit(workdir, []string{"config", "--unset-all", "gitsocial.pushRemote"})
	for _, name := range names {
		if _, err := ExecGit(workdir, []string{"config", "--add", "gitsocial.pushRemote", name}); err != nil {
			return fmt.Errorf("set gitsocial.pushRemote: %w", err)
		}
	}
	return nil
}

// SetConfiguredPushRemote persists a single default push remote (per-clone, like
// remotes themselves), replacing any previously configured set. Used by the TUI
// push picker's "persist this choice" action.
func SetConfiguredPushRemote(workdir, name string) error {
	return SetConfiguredPushRemotes(workdir, []string{name})
}

// S3Remotes returns the names of all s3-scheme remotes, sorted alphabetically.
// The TUI push picker uses this to decide whether to prompt for a target: 2+
// candidates with nothing configured is the ambiguous case.
func S3Remotes(workdir string) []string {
	remotes, err := ListRemotes(workdir)
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(remotes))
	for _, r := range remotes {
		if strings.HasPrefix(r.URL, "s3://") {
			names = append(names, r.Name)
		}
	}
	sort.Strings(names)
	return names
}

// configuredPushRemote returns the first value of git config
// gitsocial.pushRemote, or "" when unset. Reads via --get-all (the key is
// multi-valued; a plain --get errors when several values are set).
func configuredPushRemote(workdir string) string {
	names := ConfiguredPushRemotes(workdir)
	if len(names) == 0 {
		return ""
	}
	return names[0]
}

// PushSiteEnabled reports whether this machine allows the site step of a
// gitsocial push at all. Reads git config gitsocial.pushSite through git's own
// --bool normalization (false/no/off/0 ⇒ off; unset or any other value ⇒ on).
// This is a per-machine force-off, never an enabler: the site itself is gated
// on the repo's site.publish config guard (default off).
func PushSiteEnabled(workdir string) bool {
	result, err := ExecGit(workdir, []string{"config", "--bool", "--get", "gitsocial.pushSite"})
	if err != nil {
		return true // unset (or unreadable) ⇒ default on
	}
	return strings.TrimSpace(result.Stdout) != "false"
}

// PushRemoteURL returns the push remote's URL, or "" when it isn't configured.
func PushRemoteURL(workdir string) string {
	return RemoteURL(workdir, PushRemote(workdir))
}

// RemoteURL returns the URL of the named remote, or "" when it isn't configured.
func RemoteURL(workdir, name string) string {
	remotes, err := ListRemotes(workdir)
	if err != nil {
		return ""
	}
	for _, r := range remotes {
		if r.Name == name {
			return r.URL
		}
	}
	return ""
}

// GetOriginURL returns the URL of the origin remote.
func GetOriginURL(workdir string) string {
	remotes, err := ListRemotes(workdir)
	if err != nil || len(remotes) == 0 {
		return ""
	}

	for _, r := range remotes {
		if r.Name == "origin" {
			return r.URL
		}
	}

	// Fallback to first remote if origin not found
	return remotes[0].URL
}

// remotes.go - Remote repository management and fetching
package git

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
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

// ReadBranchTips reads the local and remote-tracking tips of every branch under prefix in one for-each-ref.
func ReadBranchTips(workdir, prefix string) (map[string]string, map[string]map[string]string, error) {
	result, err := ExecGit(workdir, []string{
		"for-each-ref", "--format=%(refname) %(objectname)",
		"refs/heads/" + prefix, "refs/remotes/",
	})
	if err != nil {
		return nil, nil, fmt.Errorf("for-each-ref %s: %w", prefix, err)
	}
	local := make(map[string]string)
	remote := make(map[string]map[string]string)
	for _, line := range strings.Split(result.Stdout, "\n") {
		refname, hash, ok := strings.Cut(strings.TrimSpace(line), " ")
		if !ok {
			continue
		}
		if branch, isLocal := strings.CutPrefix(refname, "refs/heads/"); isLocal {
			local[branch] = hash
			continue
		}
		rest, isRemote := strings.CutPrefix(refname, "refs/remotes/")
		if !isRemote {
			continue
		}
		at := strings.Index(rest, "/"+prefix)
		if at < 0 {
			continue
		}
		name, branch := rest[:at], rest[at+1:]
		if remote[name] == nil {
			remote[name] = make(map[string]string)
		}
		remote[name][branch] = hash
	}
	return local, remote, nil
}

// BranchDiverged reports whether a branch and its remote-tracking branch each hold commits the other lacks.
func BranchDiverged(workdir, remoteName, branch string) (bool, error) {
	result, err := ExecGit(workdir, []string{
		"rev-list", "--left-right", "--count", remoteName + "/" + branch + "..." + branch,
	})
	if err != nil {
		return false, fmt.Errorf("rev-list %s: %w", branch, err)
	}
	counts := strings.Fields(result.Stdout)
	if len(counts) != 2 {
		return false, fmt.Errorf("rev-list %s: unexpected output %q", branch, result.Stdout)
	}
	behind, err := strconv.Atoi(counts[0])
	if err != nil {
		return false, fmt.Errorf("rev-list %s: read behind count: %w", branch, err)
	}
	ahead, err := strconv.Atoi(counts[1])
	if err != nil {
		return false, fmt.Errorf("rev-list %s: read ahead count: %w", branch, err)
	}
	return behind > 0 && ahead > 0, nil
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

// PushResolution names how the default push remotes were resolved.
type PushResolution string

const (
	// PushConfigured: the user named the remotes, as arguments or in git config.
	PushConfigured PushResolution = "configured"
	// PushOrigin: nothing configured, no s3 remote to prefer.
	PushOrigin PushResolution = "origin"
	// PushS3: nothing configured, one s3 remote.
	PushS3 PushResolution = "s3"
	// PushAmbiguous: nothing configured, several s3 remotes, the first alphabetically.
	PushAmbiguous PushResolution = "ambiguous"
	// PushStale: every configured name is missing, the heuristic took over.
	PushStale PushResolution = "stale"
)

// ResolvePushRemotes returns the remotes a default push reaches and why.
func ResolvePushRemotes(workdir string) ([]string, PushResolution) {
	remotes, err := ListRemotes(workdir)
	if err != nil {
		return []string{"origin"}, PushOrigin
	}
	exists := make(map[string]bool, len(remotes))
	for _, r := range remotes {
		exists[r.Name] = true
	}
	configured := ConfiguredPushRemotes(workdir)
	var valid []string
	for _, name := range configured {
		if exists[name] {
			valid = append(valid, name)
		}
	}
	if len(valid) > 0 {
		return valid, PushConfigured
	}
	name, reason := heuristicPushRemote(remotes)
	if len(configured) > 0 {
		reason = PushStale
	}
	return []string{name}, reason
}

// heuristicPushRemote picks origin, or the first s3 remote alphabetically when origin is not one.
func heuristicPushRemote(remotes []Remote) (string, PushResolution) {
	s3Name := ""
	s3Count := 0
	for _, r := range remotes {
		if !strings.HasPrefix(r.URL, "s3://") {
			continue
		}
		s3Count++
		if r.Name == "origin" {
			return "origin", PushOrigin
		}
		if s3Name == "" || r.Name < s3Name {
			s3Name = r.Name
		}
	}
	switch {
	case s3Name == "":
		return "origin", PushOrigin
	case s3Count >= 2:
		return s3Name, PushAmbiguous
	default:
		return s3Name, PushS3
	}
}

// PushRemote returns the single remote the single-remote paths publish to.
func PushRemote(workdir string) string {
	names, _ := ResolvePushRemotes(workdir)
	return names[0]
}

// PushRemotes returns every remote a default push reaches.
func PushRemotes(workdir string) []string {
	names, _ := ResolvePushRemotes(workdir)
	return names
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

// AppendConfiguredPushRemote appends a remote to the gitsocial.pushRemote defaults, once.
func AppendConfiguredPushRemote(workdir, name string) error {
	configured := ConfiguredPushRemotes(workdir)
	for _, n := range configured {
		if n == name {
			return nil
		}
	}
	return SetConfiguredPushRemotes(workdir, append(configured, name))
}

// S3Remotes returns the names of all s3-scheme remotes, sorted alphabetically.
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

// EnsureRemote points the named remote at url: it adds a missing remote, re-points one whose URL differs and leaves a matching one alone.
func EnsureRemote(workdir, name, url string) error {
	current, err := ExecGit(workdir, []string{"remote", "get-url", name})
	if err != nil {
		if _, err := ExecGit(workdir, []string{"remote", "add", name, url}); err != nil {
			return fmt.Errorf("add remote %s: %w", name, err)
		}
		return nil
	}
	if strings.TrimSpace(current.Stdout) == url {
		return nil
	}
	if _, err := ExecGit(workdir, []string{"remote", "set-url", name, url}); err != nil {
		return fmt.Errorf("set remote %s url: %w", name, err)
	}
	return nil
}

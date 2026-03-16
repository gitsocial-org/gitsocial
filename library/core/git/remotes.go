// remotes.go - Remote repository management and fetching
package git

import (
	"fmt"
	"regexp"
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

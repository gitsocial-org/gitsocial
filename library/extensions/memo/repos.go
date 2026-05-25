// repos.go - Memo tier resolution and bare-repo lifecycle. Personal-tier
// storage delegates to core/settings (single bare repo at
// ~/.config/gitsocial/personal); sessions stay memo-local under
// ~/.cache/gitsocial/memo/session.
package memo

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/settings"
)

// Tier identifies which storage tier a memo lives on.
type Tier string

const (
	TierSession   Tier = "session"
	TierPersonal  Tier = "personal"
	TierProject   Tier = "project"
	TierInherited Tier = "inherited"
	TierExternal  Tier = "external"
)

// MemoBranch is the canonical branch name memos live on within their tier repo.
const MemoBranch = "gitmsg/memo"

// TierPriority returns the retrieval rank for a tier (lower = surfaces first).
// Authority is carried by priority/* labels, not by tier rank.
func TierPriority(t Tier) int {
	switch t {
	case TierSession:
		return 0
	case TierPersonal:
		return 1
	case TierProject:
		return 2
	case TierInherited:
		return 3
	case TierExternal:
		return 4
	}
	return 5
}

// LocalRepoURL returns the cache.repo_url representation of a local bare repo.
// Format: `local:<absolute-path>`.
func LocalRepoURL(path string) string {
	if path == "" {
		return ""
	}
	return "local:" + path
}

// PathFromLocalURL extracts the bare-repo path from a `local:<path>` URL.
func PathFromLocalURL(repoURL string) string {
	if !strings.HasPrefix(repoURL, "local:") {
		return ""
	}
	return strings.TrimPrefix(repoURL, "local:")
}

// DefaultSessionDir returns the default parent directory for per-session bare repos.
func DefaultSessionDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".cache", "gitsocial", "memo", "session"), nil
}

// SessionDir returns the session parent directory. Honors MEMO_SESSION_DIR env
// override; otherwise uses the default under ~/.cache/gitsocial/memo/session.
func SessionDir() (string, error) {
	if v := strings.TrimSpace(os.Getenv("MEMO_SESSION_DIR")); v != "" {
		return v, nil
	}
	return DefaultSessionDir()
}

// SessionRepoPath returns the bare-repo path for a specific session id.
func SessionRepoPath(sessionID string) (string, error) {
	dir, err := SessionDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, sessionID), nil
}

// EnsureSessionRepo creates the bare repo for the given session id if missing.
// The id is resolved if empty. workspaceURL, when non-empty, is recorded under
// `memo.workspace-url` on first creation so list/filter calls can scope
// sessions to the workspace that created them. Resumed repos keep their
// originally-recorded URL.
func EnsureSessionRepo(sessionID, workspaceURL string) (string, string, error) {
	if sessionID == "" {
		sessionID = ResolveSessionID()
	}
	path, err := SessionRepoPath(sessionID)
	if err != nil {
		return "", "", err
	}
	fresh := !git.BareRepoExists(path)
	if err := git.EnsureBareRepo(path); err != nil {
		return "", "", err
	}
	if fresh && workspaceURL != "" {
		_, _ = git.ExecGit(path, []string{"config", "memo.workspace-url", workspaceURL})
	}
	return path, sessionID, nil
}

// SessionWorkspaceURL returns the workspace URL recorded on the session bare
// repo at creation time, or "" when the repo predates the tag (legacy) or
// the call fails.
func SessionWorkspaceURL(path string) string {
	if path == "" {
		return ""
	}
	out, err := git.ExecGit(path, []string{"config", "--get", "memo.workspace-url"})
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out.Stdout)
}

// ResolveSessionID returns the active session id using the documented priority:
// MEMO_SESSION_ID env var > generated.
func ResolveSessionID() string {
	if v := strings.TrimSpace(os.Getenv("MEMO_SESSION_ID")); v != "" {
		return v
	}
	return generateSessionID()
}

// ResolveTierTarget returns the on-disk repo path, cache repo_url, and branch
// that a write at the given tier should land on. For TierProject the workdir
// is used; personal/session resolve to a bare repo under the user's cache/config.
// Sessions are always created on demand.
func ResolveTierTarget(tier Tier, workdir, workspaceURL string) (repoPath, repoURL, branch string, err error) {
	switch tier {
	case TierProject, "":
		return workdir, workspaceURL, MemoBranch, nil
	case TierPersonal:
		path, perr := settings.EnsurePersonalRepo()
		if perr != nil {
			return "", "", "", perr
		}
		return path, LocalRepoURL(path), MemoBranch, nil
	case TierSession:
		path, _, perr := EnsureSessionRepo("", workspaceURL)
		if perr != nil {
			return "", "", "", perr
		}
		return path, LocalRepoURL(path), MemoBranch, nil
	case TierInherited:
		return "", "", "", fmt.Errorf("inherited tier is read-only")
	case TierExternal:
		return "", "", "", fmt.Errorf("external tier is read-only")
	}
	return "", "", "", fmt.Errorf("unknown tier: %s", tier)
}

// TierForRepoURL maps a (repo_url, branch) pair back to its conceptual tier.
// Sessions are detected by repo_url path layout (under the session dir);
// personal by matching the configured personal repo; project by matching
// the workspace URL; inherited by membership in the supplied URL set;
// everything else is external.
func TierForRepoURL(repoURL, workspaceURL string, inheritedURLs []string) Tier {
	if repoURL == "" {
		return TierProject
	}
	if workspaceURL != "" && repoURL == workspaceURL {
		return TierProject
	}
	if strings.HasPrefix(repoURL, "local:") {
		path := PathFromLocalURL(repoURL)
		if dir, err := SessionDir(); err == nil && samePath(filepath.Dir(path), dir) {
			return TierSession
		}
		if p, err := settings.PersonalRepoPath(); err == nil && samePath(path, p) {
			return TierPersonal
		}
		return TierExternal
	}
	for _, u := range inheritedURLs {
		if u == repoURL {
			return TierInherited
		}
	}
	return TierExternal
}

// SessionIDForRepoURL extracts the session id from a session-tier repo_url.
// Returns "" if the URL doesn't point inside the session dir.
func SessionIDForRepoURL(repoURL string) string {
	path := PathFromLocalURL(repoURL)
	if path == "" {
		return ""
	}
	dir, err := SessionDir()
	if err != nil {
		return ""
	}
	if !samePath(filepath.Dir(path), dir) {
		return ""
	}
	return filepath.Base(path)
}

// AllTierRepoURLs returns the repo_url strings spanning every initialized
// tier reachable from the workspace, in priority order: workspace, personal,
// each session repo whose recorded workspace-url matches (or is unset for
// legacy repos). Used as the IN-clause for tier-merging queries.
func AllTierRepoURLs(workspaceURL string) []string {
	urls := make([]string, 0, 4)
	if workspaceURL != "" {
		urls = append(urls, workspaceURL)
	}
	if p, err := settings.PersonalRepoPath(); err == nil && git.BareRepoExists(p) {
		urls = append(urls, LocalRepoURL(p))
	}
	for _, p := range listSessionReposForWorkspace(workspaceURL) {
		urls = append(urls, LocalRepoURL(p))
	}
	return urls
}

// listSessionRepos returns every session bare-repo path under the session dir.
func listSessionRepos() []string {
	dir, err := SessionDir()
	if err != nil {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var paths []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		p := filepath.Join(dir, e.Name())
		if git.BareRepoExists(p) {
			paths = append(paths, p)
		}
	}
	return paths
}

// listSessionReposForWorkspace returns session bare-repo paths whose recorded
// workspace-url matches workspaceURL, plus untagged (legacy) repos. Passing
// an empty workspaceURL disables filtering — every session is returned.
func listSessionReposForWorkspace(workspaceURL string) []string {
	all := listSessionRepos()
	if workspaceURL == "" {
		return all
	}
	out := make([]string, 0, len(all))
	for _, p := range all {
		tag := SessionWorkspaceURL(p)
		if tag == "" || tag == workspaceURL {
			out = append(out, p)
		}
	}
	return out
}

// --- internal helpers ---

func samePath(a, b string) bool {
	ar, err := filepath.Abs(a)
	if err != nil {
		ar = a
	}
	br, err := filepath.Abs(b)
	if err != nil {
		br = b
	}
	return ar == br
}

func generateSessionID() string {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return time.Now().UTC().Format("20060102") + "-" + fmt.Sprintf("%08x", time.Now().UnixNano()&0xFFFFFFFF)
	}
	return time.Now().UTC().Format("20060102") + "-" + hex.EncodeToString(b[:])
}

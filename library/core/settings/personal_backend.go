// personal_backend.go - Backend reading/writing refs/gitmsg/core/config in
// the personal bare repo. The repo path is resolved via GITSOCIAL_PERSONAL_REPO
// or the default ~/.config/gitsocial/personal/. When the path is unset or the
// repo is missing, Get returns ("", false) and Set returns a clear error;
// resolution then falls through to local/default.
package settings

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
)

// PersonalConfigExt is the extension name used under refs/gitmsg/<ext>/config.
const PersonalConfigExt = "core"

// PersonalRepoPath returns the configured (or default) path to the personal
// bare repo. The path may not exist yet; callers should check via
// PersonalRepoExists or by attempting an operation.
func PersonalRepoPath() (string, error) {
	if v := strings.TrimSpace(os.Getenv("GITSOCIAL_PERSONAL_REPO")); v != "" {
		return expandHome(v), nil
	}
	dir, err := UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "gitsocial", "personal"), nil
}

// PersonalRepoExists reports whether a bare repo is initialized at the
// resolved personal-repo path.
func PersonalRepoExists() bool {
	path, err := PersonalRepoPath()
	if err != nil {
		return false
	}
	return bareRepoExists(path)
}

// EnsurePersonalRepo creates the personal bare repo at the resolved path if
// missing, configuring it with the global git user.name / user.email so
// commits written by the backend are attributable. Returns the resolved path.
func EnsurePersonalRepo() (string, error) {
	path, err := PersonalRepoPath()
	if err != nil {
		return "", err
	}
	if bareRepoExists(path) {
		return path, nil
	}
	if err := os.MkdirAll(path, 0o700); err != nil {
		return "", fmt.Errorf("create personal repo dir: %w", err)
	}
	if _, err := git.ExecGit(path, []string{"init", "--bare", path}); err != nil {
		return "", fmt.Errorf("git init --bare %s: %w", path, err)
	}
	if name := globalGitConfig("user.name"); name != "" {
		_, _ = git.ExecGit(path, []string{"config", "user.name", name})
	} else {
		_, _ = git.ExecGit(path, []string{"config", "user.name", "GitSocial Personal"})
	}
	if email := globalGitConfig("user.email"); email != "" {
		_, _ = git.ExecGit(path, []string{"config", "user.email", email})
	} else {
		_, _ = git.ExecGit(path, []string{"config", "user.email", "personal@local"})
	}
	return path, nil
}

// personalConfigBackend stores ScopePersonalConfig values in
// refs/gitmsg/core/config of the personal bare repo. The path is resolved
// lazily on each call so a freshly-initialized repo (created between Manager
// construction and a Set) is picked up without re-creating the Manager.
type personalConfigBackend struct{}

// NewPersonalConfigBackend returns the personal-config-ref-backed source.
func NewPersonalConfigBackend() *personalConfigBackend { return &personalConfigBackend{} }

func (b *personalConfigBackend) Scope() Scope { return ScopePersonalConfig }

// Get returns the value of key from refs/gitmsg/core/config. Returns ("", false)
// when the personal repo isn't initialized, the ref doesn't exist yet, or the
// key isn't present. The personal repo is a soft dependency — its absence
// must not break Resolve for ScopeLocal keys.
func (b *personalConfigBackend) Get(key string) (string, bool) {
	path, err := PersonalRepoPath()
	if err != nil || !bareRepoExists(path) {
		return "", false
	}
	config, err := gitmsg.ReadExtConfig(path, PersonalConfigExt)
	if err != nil || config == nil {
		return "", false
	}
	v, ok := config[key]
	if !ok {
		return "", false
	}
	return jsonValueToString(v), true
}

// Set writes value for key into refs/gitmsg/core/config, auto-initializing
// the bare repo at PersonalRepoPath() if it doesn't exist. This keeps the
// "first write just works" UX: a user setting a pref via the CLI or TUI
// doesn't need to know about `gitsocial personal init` — that command is only
// required when they want to attach a remote and sync.
func (b *personalConfigBackend) Set(key, value string) error {
	if err := Validate(key, value); err != nil {
		return err
	}
	path, err := EnsurePersonalRepo()
	if err != nil {
		return fmt.Errorf("ensure personal repo: %w", err)
	}
	config, err := gitmsg.ReadExtConfig(path, PersonalConfigExt)
	if err != nil {
		return fmt.Errorf("read personal config: %w", err)
	}
	if config == nil {
		config = make(map[string]interface{})
	}
	config[key] = value
	if err := gitmsg.WriteExtConfig(path, PersonalConfigExt, config); err != nil {
		return fmt.Errorf("write personal config: %w", err)
	}
	return nil
}

// GetWorkspaceMode returns the fetch mode recorded for a specific repo URL,
// or "" when nothing has been set. Workspace modes live in personal config
// under the "fetch.workspace_modes" key as a JSON object (repoURL → mode).
func GetWorkspaceMode(repoURL string) string {
	if repoURL == "" {
		return ""
	}
	path, err := PersonalRepoPath()
	if err != nil || !bareRepoExists(path) {
		return ""
	}
	config, err := gitmsg.ReadExtConfig(path, PersonalConfigExt)
	if err != nil || config == nil {
		return ""
	}
	raw, ok := config[workspaceModesKey].(map[string]interface{})
	if !ok {
		return ""
	}
	if v, ok := raw[repoURL].(string); ok {
		return v
	}
	return ""
}

// WriteWorkspaceMode persists the fetch mode for a repo URL, auto-initializing
// the personal repo as Set does. Pass mode="" to remove the entry.
func WriteWorkspaceMode(repoURL, mode string) error {
	if repoURL == "" {
		return fmt.Errorf("repo URL required")
	}
	path, err := EnsurePersonalRepo()
	if err != nil {
		return fmt.Errorf("ensure personal repo: %w", err)
	}
	config, err := gitmsg.ReadExtConfig(path, PersonalConfigExt)
	if err != nil {
		return fmt.Errorf("read personal config: %w", err)
	}
	if config == nil {
		config = make(map[string]interface{})
	}
	modes, _ := config[workspaceModesKey].(map[string]interface{})
	if modes == nil {
		modes = make(map[string]interface{})
	}
	if mode == "" {
		delete(modes, repoURL)
	} else {
		modes[repoURL] = mode
	}
	if len(modes) == 0 {
		delete(config, workspaceModesKey)
	} else {
		config[workspaceModesKey] = modes
	}
	if err := gitmsg.WriteExtConfig(path, PersonalConfigExt, config); err != nil {
		return fmt.Errorf("write personal config: %w", err)
	}
	return nil
}

// workspaceModesKey is the personal-config key under which the per-repo mode
// map is stored. Kept private — callers use Get/WriteWorkspaceMode.
const workspaceModesKey = "fetch.workspace_modes"

// LoadWorkspaceModes reads the entire per-repo mode map. Used by overlay to
// populate Settings.Fetch.WorkspaceModes so legacy readers see synced values.
func LoadWorkspaceModes() map[string]string {
	path, err := PersonalRepoPath()
	if err != nil || !bareRepoExists(path) {
		return nil
	}
	config, err := gitmsg.ReadExtConfig(path, PersonalConfigExt)
	if err != nil || config == nil {
		return nil
	}
	raw, ok := config[workspaceModesKey].(map[string]interface{})
	if !ok {
		return nil
	}
	out := make(map[string]string, len(raw))
	for k, v := range raw {
		if s, ok := v.(string); ok {
			out[k] = s
		}
	}
	return out
}

// --- helpers ---

// bareRepoExists checks whether path contains a bare git repo (HEAD file at
// the root, not a directory). Mirrors the heuristic memo/repos.go uses; when
// memo rebases on top, the two definitions should be unified.
func bareRepoExists(path string) bool {
	if path == "" {
		return false
	}
	st, err := os.Stat(filepath.Join(path, "HEAD"))
	if err != nil {
		return false
	}
	return !st.IsDir()
}

// globalGitConfig reads a key from the global git config; "" on failure.
func globalGitConfig(key string) string {
	out, err := git.ExecGit("", []string{"config", "--global", "--get", key})
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out.Stdout)
}

// expandHome resolves a leading "~/" or bare "~" against $HOME.
func expandHome(p string) string {
	if !strings.HasPrefix(p, "~/") && p != "~" {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	if p == "~" {
		return home
	}
	return filepath.Join(home, p[2:])
}

// jsonValueToString renders a JSON-decoded value (string/bool/number) back to
// the canonical string form the legacy Get returns, so callers see consistent
// strings regardless of which backend serviced the read.
func jsonValueToString(v interface{}) string {
	switch x := v.(type) {
	case string:
		return x
	case bool:
		if x {
			return "true"
		}
		return "false"
	case float64:
		// json.Unmarshal decodes numbers as float64; render ints without ".0".
		if x == float64(int64(x)) {
			return fmt.Sprintf("%d", int64(x))
		}
		return fmt.Sprintf("%v", x)
	}
	return fmt.Sprintf("%v", v)
}

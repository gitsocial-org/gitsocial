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
	"slices"
	"strings"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
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
	return git.BareRepoExists(path)
}

// EnsurePersonalRepo creates the personal bare repo at the resolved path if
// missing. Returns the resolved path.
func EnsurePersonalRepo() (string, error) {
	path, err := PersonalRepoPath()
	if err != nil {
		return "", err
	}
	if err := git.EnsureBareRepo(path); err != nil {
		return "", err
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

// Scope returns the personal config scope.
func (b *personalConfigBackend) Scope() Scope { return ScopePersonalConfig }

// Get returns the value of key from refs/gitmsg/core/config. Returns ("", false)
// when the personal repo isn't initialized, the ref doesn't exist yet, or the
// key isn't present. The personal repo is a soft dependency — its absence
// must not break Resolve for ScopeLocal keys.
func (b *personalConfigBackend) Get(key string) (string, bool) {
	path, err := PersonalRepoPath()
	if err != nil || !git.BareRepoExists(path) {
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
	return personalObject(workspaceModesKey)[repoURL]
}

// WriteWorkspaceMode persists the fetch mode for a repo URL, auto-initializing
// the personal repo as Set does. Pass mode="" to remove the entry.
func WriteWorkspaceMode(repoURL, mode string) error {
	if repoURL == "" {
		return fmt.Errorf("repo URL required")
	}
	return writePersonalObjectEntry(workspaceModesKey, repoURL, mode)
}

// workspaceModesKey is the personal-config key under which the per-repo mode
// map is stored. Kept private — callers use Get/WriteWorkspaceMode.
const workspaceModesKey = "fetch.workspace_modes"

// LoadWorkspaceModes reads the entire per-repo mode map. Used by overlay to
// populate Settings.Fetch.WorkspaceModes so legacy readers see synced values.
func LoadWorkspaceModes() map[string]string {
	return personalObject(workspaceModesKey)
}

// notificationLevelsKey is the personal-config key of the object from repository identity to notification level.
const notificationLevelsKey = "notifications.levels"

// The notification levels of a repository; no entry is the level default.
const (
	LevelAll     = "all"
	LevelDefault = "default"
	LevelOff     = "off"
)

// NotificationLevels reads the stored level of each repository, after a clear of the config memo so a level that another process wrote is seen.
func NotificationLevels() map[string]string {
	if path, err := PersonalRepoPath(); err == nil {
		gitmsg.InvalidateExtConfig(path, PersonalConfigExt)
	}
	return personalObject(notificationLevelsKey)
}

// WriteNotificationLevel stores the level of a repository under its identity, all with its set time; default removes the entry.
func WriteNotificationLevel(repoURL, level string, setAt time.Time) error {
	identity := protocol.NormalizeURL(repoURL)
	if identity == "" {
		return fmt.Errorf("repository required")
	}
	switch level {
	case LevelAll:
		return writePersonalObjectEntry(notificationLevelsKey, identity, LevelAll+" "+setAt.UTC().Format(time.RFC3339))
	case LevelOff:
		return writePersonalObjectEntry(notificationLevelsKey, identity, LevelOff)
	case LevelDefault:
		return writePersonalObjectEntry(notificationLevelsKey, identity, "")
	}
	return fmt.Errorf("invalid level %q: use all, default or off", level)
}

// NotificationLevel returns the level of a repository and the set time of all: its own entry, else the entry of the workspace for a registered fork, else default.
func NotificationLevel(levels map[string]string, repoURL, workspaceURL string, forks []string) (string, time.Time) {
	identity := protocol.NormalizeURL(repoURL)
	value, ok := levels[identity]
	if !ok && slices.Contains(forks, identity) {
		value = levels[protocol.NormalizeURL(workspaceURL)]
	}
	word, setTime, _ := strings.Cut(value, " ")
	switch word {
	case LevelOff:
		return LevelOff, time.Time{}
	case LevelAll:
		if at, err := time.Parse(time.RFC3339, setTime); err == nil {
			return LevelAll, at
		}
	}
	return LevelDefault, time.Time{}
}

// personalObject returns the string entries of the object under key in the personal config, or nil.
func personalObject(key string) map[string]string {
	path, err := PersonalRepoPath()
	if err != nil || !git.BareRepoExists(path) {
		return nil
	}
	config, err := gitmsg.ReadExtConfig(path, PersonalConfigExt)
	if err != nil || config == nil {
		return nil
	}
	raw, ok := config[key].(map[string]interface{})
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

// writePersonalObjectEntry sets one entry of the object under key in the personal config, or removes it for an empty value; an empty object is removed.
func writePersonalObjectEntry(key, entry, value string) error {
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
	object, _ := config[key].(map[string]interface{})
	if object == nil {
		object = make(map[string]interface{})
	}
	if value == "" {
		delete(object, entry)
	} else {
		object[entry] = value
	}
	if len(object) == 0 {
		delete(config, key)
	} else {
		config[key] = object
	}
	if err := gitmsg.WriteExtConfig(path, PersonalConfigExt, config); err != nil {
		return fmt.Errorf("write personal config: %w", err)
	}
	return nil
}

// --- helpers ---

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

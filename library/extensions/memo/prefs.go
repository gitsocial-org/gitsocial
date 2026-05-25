// prefs.go - Memo extension config (refs/gitmsg/memo/config) and tier init
package memo

import (
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/result"
	"github.com/gitsocial-org/gitsocial/library/core/settings"
)

// Config holds memo extension configuration as stored in refs/gitmsg/memo/config.
// The branch is fixed at `gitmsg/memo` across every tier — no field exposes it
// so a stale config can't desync writes from reads.
type Config struct {
	Version string `json:"version"`
}

// DefaultConfig returns the default memo configuration.
func DefaultConfig() Config {
	return Config{Version: "0.1.0"}
}

// GetConfig reads the project-tier memo extension configuration.
func GetConfig(workdir string) Config {
	configMap, err := gitmsg.ReadExtConfig(workdir, "memo")
	if err != nil || configMap == nil {
		return DefaultConfig()
	}
	cfg := DefaultConfig()
	if v, ok := configMap["version"].(string); ok && v != "" {
		cfg.Version = v
	}
	return cfg
}

// SaveConfig writes the project-tier memo extension configuration. The branch
// is always `gitmsg/memo` so `gitmsg.IsExtInitialized` and the rest of the
// core protocol layer can locate the memo branch without reading this config.
func SaveConfig(workdir string, cfg Config) error {
	if cfg.Version == "" {
		cfg.Version = "0.1.0"
	}
	return gitmsg.WriteExtConfig(workdir, "memo", map[string]interface{}{
		"version": cfg.Version,
		"branch":  MemoBranch,
	})
}

// IsProjectInitialized returns true when the workspace has the memo extension
// branch configured.
func IsProjectInitialized(workdir string) bool {
	return gitmsg.IsExtInitialized(workdir, "memo")
}

// InitProject sets up the project-tier memo branch on the workspace.
// Idempotent: re-running on an already-initialized workspace is a no-op.
func InitProject(workdir string) Result[bool] {
	if err := SaveConfig(workdir, DefaultConfig()); err != nil {
		return result.Err[bool]("PROJECT_INIT_FAILED", err.Error())
	}
	return result.Ok(true)
}

// InitPersonal sets up the personal-tier bare repo (shared with core
// settings). Idempotent.
func InitPersonal() Result[string] {
	path, err := settings.EnsurePersonalRepo()
	if err != nil {
		return result.Err[string]("PERSONAL_INIT_FAILED", err.Error())
	}
	return result.Ok(path)
}

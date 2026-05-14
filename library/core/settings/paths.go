// paths.go - User-config directory resolution shared across the settings
// package and any extension that stores per-user state under ~/.config.
package settings

import (
	"os"
	"path/filepath"
)

// UserConfigDir returns the directory that holds per-user gitsocial config:
// $XDG_CONFIG_HOME when set, otherwise $HOME/.config. Mirrors the convention
// used by git, gh, kubectl, etc. — CLI tools that prefer ~/.config over the
// platform-native location on macOS, so config travels with dotfiles.
func UserConfigDir() (string, error) {
	if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
		return v, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config"), nil
}

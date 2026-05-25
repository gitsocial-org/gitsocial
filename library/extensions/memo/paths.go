// paths.go - Home-relative path collapsing for identity-leak avoidance.
// Memo tier repos live under $HOME (`local:/Users/alice/.cache/...`); the
// absolute path is the right cache key but a poor public identifier — it
// leaks the OS username into any JSON export. Helpers here keep the cache
// representation absolute while presenting tilde-form (`local:~/.cache/...`)
// at the JSON boundary, and translate user-supplied tilde-form refs back to
// absolute before cache lookup.
package memo

import (
	"os"
	"strings"
)

// collapseHomePath replaces a leading $HOME with `~`. Inputs that don't start
// with $HOME (or when $HOME is empty) are returned unchanged.
func collapseHomePath(p string) string {
	if p == "" {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return p
	}
	if p == home {
		return "~"
	}
	if strings.HasPrefix(p, home+string(os.PathSeparator)) {
		return "~" + p[len(home):]
	}
	return p
}

// expandHomePath replaces a leading `~` with $HOME. Inputs that don't start
// with `~` (or when $HOME is empty) are returned unchanged.
func expandHomePath(p string) string {
	if p == "" || (p[0] != '~') {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return p
	}
	if p == "~" {
		return home
	}
	if strings.HasPrefix(p, "~"+string(os.PathSeparator)) {
		return home + p[1:]
	}
	return p
}

// CollapseLocalURL maps `local:<abs-path>` → `local:<home-relative-path>` when
// the path lives under $HOME. Non-local URLs and paths outside $HOME are
// returned unchanged. Used at every JSON-boundary serialization site.
func CollapseLocalURL(repoURL string) string {
	path := PathFromLocalURL(repoURL)
	if path == "" {
		return repoURL
	}
	return "local:" + collapseHomePath(path)
}

// ExpandLocalURL maps `local:<home-relative-path>` → `local:<abs-path>`. Used
// by GetMemoItemByRef and friends to translate user-supplied tilde-form refs
// back to the absolute form the cache stores.
func ExpandLocalURL(repoURL string) string {
	path := PathFromLocalURL(repoURL)
	if path == "" {
		return repoURL
	}
	return "local:" + expandHomePath(path)
}

// forks.go - Core fork registry stored in refs/gitmsg/config
package gitmsg

import (
	"github.com/gitsocial-org/gitsocial/core/protocol"
)

// GetForks returns the list of registered fork URLs for the workspace.
// Migrates legacy forks from review config on first access.
func GetForks(workdir string) []string {
	config, _ := ReadExtConfig(workdir, "core")
	forks := getForksList(configOrEmpty(config))
	if len(forks) > 0 {
		return forks
	}
	// Migrate legacy forks from review config
	reviewConfig, _ := ReadExtConfig(workdir, "review")
	legacyForks := getForksList(configOrEmpty(reviewConfig))
	if len(legacyForks) > 0 {
		if config == nil {
			config = make(map[string]interface{})
		}
		config["forks"] = legacyForks
		_ = WriteExtConfig(workdir, "core", config)
		return legacyForks
	}
	return nil
}

func configOrEmpty(config map[string]interface{}) map[string]interface{} {
	if config == nil {
		return map[string]interface{}{}
	}
	return config
}

// AddFork registers a fork URL in the core config.
func AddFork(workdir, forkURL string) error {
	forkURL = protocol.NormalizeURL(forkURL)
	config, _ := ReadExtConfig(workdir, "core")
	if config == nil {
		config = make(map[string]interface{})
	}
	existing := getForksList(config)
	for _, f := range existing {
		if f == forkURL {
			return nil
		}
	}
	existing = append(existing, forkURL)
	config["forks"] = existing
	return WriteExtConfig(workdir, "core", config)
}

// AddForks registers multiple fork URLs in a single config save.
func AddForks(workdir string, forkURLs []string) (int, error) {
	config, _ := ReadExtConfig(workdir, "core")
	if config == nil {
		config = make(map[string]interface{})
	}
	existing := getForksList(config)
	set := make(map[string]bool, len(existing))
	for _, f := range existing {
		set[f] = true
	}
	added := 0
	for _, u := range forkURLs {
		u = protocol.NormalizeURL(u)
		if set[u] {
			continue
		}
		set[u] = true
		existing = append(existing, u)
		added++
	}
	if added == 0 {
		return 0, nil
	}
	config["forks"] = existing
	return added, WriteExtConfig(workdir, "core", config)
}

// RemoveFork removes a fork URL from the core config.
func RemoveFork(workdir, forkURL string) error {
	forkURL = protocol.NormalizeURL(forkURL)
	config, _ := ReadExtConfig(workdir, "core")
	if config == nil {
		return nil
	}
	existing := getForksList(config)
	filtered := make([]string, 0, len(existing))
	for _, f := range existing {
		if f != forkURL {
			filtered = append(filtered, f)
		}
	}
	config["forks"] = filtered
	return WriteExtConfig(workdir, "core", config)
}

func getForksList(config map[string]interface{}) []string {
	forks, ok := config["forks"].([]interface{})
	if !ok {
		return nil
	}
	result := make([]string, 0, len(forks))
	for _, item := range forks {
		if s, ok := item.(string); ok {
			result = append(result, s)
		}
	}
	return result
}

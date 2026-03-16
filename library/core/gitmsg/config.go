// config.go - Extension configuration stored in git refs
package gitmsg

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/gitsocial-org/gitsocial/core/git"
)

const configVersion = "0.1.0"

var extConfigCache sync.Map // "workdir\x00ext" → map[string]interface{}

// extConfigRef returns the git ref path for an extension's config.
func extConfigRef(ext string) string {
	return fmt.Sprintf("refs/gitmsg/%s/config", ext)
}

// ReadExtConfig reads an extension's configuration from git ref.
func ReadExtConfig(workdir, ext string) (map[string]interface{}, error) {
	key := workdir + "\x00" + ext
	if v, ok := extConfigCache.Load(key); ok {
		if v == nil {
			return nil, nil
		}
		return v.(map[string]interface{}), nil
	}
	config := readExtConfigUncached(workdir, ext)
	extConfigCache.Store(key, config)
	return config, nil
}

func readExtConfigUncached(workdir, ext string) map[string]interface{} {
	ref := extConfigRef(ext)
	hash, err := git.ReadRef(workdir, ref)
	if err != nil {
		return nil
	}
	msg, err := git.GetCommitMessage(workdir, hash)
	if err != nil {
		slog.Debug("read ext config message", "error", err, "ext", ext)
		return nil
	}
	var config map[string]interface{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(msg)), &config); err != nil {
		slog.Warn("ext config JSON parse", "error", err, "ext", ext)
		return nil
	}
	return config
}

// InvalidateExtConfig clears the cached config for an extension.
func InvalidateExtConfig(workdir, ext string) {
	extConfigCache.Delete(workdir + "\x00" + ext)
}

// WriteExtConfig writes an extension's configuration to git ref.
func WriteExtConfig(workdir, ext string, config map[string]interface{}) error {
	if config["version"] == nil {
		config["version"] = configVersion
	}

	content, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	ref := extConfigRef(ext)
	var parent string
	if existingHash, err := git.ReadRef(workdir, ref); err == nil {
		parent = existingHash
	}

	commitHash, err := git.CreateCommitTree(workdir, string(content), parent)
	if err != nil {
		return fmt.Errorf("failed to create config commit: %w", err)
	}

	if err := git.WriteRef(workdir, ref, commitHash); err != nil {
		return fmt.Errorf("failed to write config ref: %w", err)
	}

	InvalidateExtConfig(workdir, ext)
	return nil
}

// GetExtConfigValue retrieves a single config value for an extension.
func GetExtConfigValue(workdir, ext, key string) (string, bool) {
	config, _ := ReadExtConfig(workdir, ext)
	if config == nil {
		return "", false
	}

	val, ok := config[key]
	if !ok {
		return "", false
	}

	switch v := val.(type) {
	case string:
		return v, v != ""
	case float64:
		return fmt.Sprintf("%v", v), true
	case bool:
		return fmt.Sprintf("%v", v), true
	default:
		return fmt.Sprintf("%v", v), true
	}
}

// SetExtConfigValue sets a single config value for an extension.
func SetExtConfigValue(workdir, ext, key, value string) error {
	config, _ := ReadExtConfig(workdir, ext)
	if config == nil {
		config = make(map[string]interface{})
	}

	config[key] = value

	return WriteExtConfig(workdir, ext, config)
}

// DeleteExtConfigKey removes a config key from an extension.
func DeleteExtConfigKey(workdir, ext, key string) error {
	config, _ := ReadExtConfig(workdir, ext)
	if config == nil {
		return nil
	}

	delete(config, key)

	return WriteExtConfig(workdir, ext, config)
}

type ConfigKeyValue struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// ListExtConfig returns all config key-value pairs for an extension.
func ListExtConfig(workdir, ext string) []ConfigKeyValue {
	config, _ := ReadExtConfig(workdir, ext)
	if config == nil {
		return nil
	}

	result := make([]ConfigKeyValue, 0, len(config))
	for key, val := range config {
		var value string
		switch v := val.(type) {
		case string:
			value = v
		default:
			value = fmt.Sprintf("%v", v)
		}
		result = append(result, ConfigKeyValue{Key: key, Value: value})
	}
	return result
}

// GetExtBranch returns the configured branch for an extension, defaulting to gitmsg/<ext>.
func GetExtBranch(workdir, ext string) string {
	if val, ok := GetExtConfigValue(workdir, ext, "branch"); ok {
		return val
	}
	return "gitmsg/" + ext
}

// IsExtInitialized checks if an extension has been initialized (has a branch configured).
func IsExtInitialized(workdir, ext string) bool {
	_, ok := GetExtConfigValue(workdir, ext, "branch")
	return ok
}

// GetExtBranches returns all local gitmsg/* branch names.
func GetExtBranches(workdir string) []string {
	result, err := git.ExecGit(workdir, []string{
		"for-each-ref", "--format=%(refname:short)", "refs/heads/gitmsg/",
	})
	if err != nil || result.Stdout == "" {
		return nil
	}
	var branches []string
	for _, line := range strings.Split(strings.TrimSpace(result.Stdout), "\n") {
		if line != "" {
			branches = append(branches, line)
		}
	}
	return branches
}

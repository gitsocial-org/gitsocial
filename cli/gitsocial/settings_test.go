// settings_test.go - Tests for the settings commands' text and JSON output
package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/gitsocial-org/gitsocial/library/core/settings"
)

// settingsListRepo returns a repo and cache dir with the personal repo pointed at a temp path.
func settingsListRepo(t *testing.T) (string, string) {
	t.Helper()
	t.Setenv("GITSOCIAL_PERSONAL_REPO", t.TempDir()+"/personal")
	return initCLITestRepo(t), t.TempDir()
}

func TestSettingsList_printsDescriptionUnderEachKey(t *testing.T) {
	dir, cacheDir := settingsListRepo(t)
	stdout, stderr, code := runInProcess(t, dir, cacheDir, "settings", "list")
	if code != 0 {
		t.Fatalf("settings list: exit %d\n%s%s", code, stdout, stderr)
	}
	spec, ok := settings.Lookup("fetch.parallel")
	if !ok {
		t.Fatal("Lookup(fetch.parallel) found no registry entry")
	}
	if !strings.Contains(stdout, "fetch.parallel = 4\n  "+spec.Desc+"\n") {
		t.Errorf("settings list does not print fetch.parallel with its description:\n%s", stdout)
	}
	if strings.Contains(stdout, "fetch.workspace_mode = (per-repo)\n\n") {
		t.Errorf("a key with no description printed a blank line:\n%s", stdout)
	}
}

func TestSettingsList_jsonCarriesDescription(t *testing.T) {
	dir, cacheDir := settingsListRepo(t)
	stdout, stderr, code := runInProcess(t, dir, cacheDir, "--json", "settings", "list")
	if code != 0 {
		t.Fatalf("settings list --json: exit %d\n%s%s", code, stdout, stderr)
	}
	var items []struct {
		Key         string `json:"key"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal([]byte(stdout), &items); err != nil {
		t.Fatalf("unmarshal settings list --json: %v\n%s", err, stdout)
	}
	byKey := map[string]string{}
	for _, item := range items {
		byKey[item.Key] = item.Description
	}
	spec, _ := settings.Lookup("fetch.parallel")
	if byKey["fetch.parallel"] != spec.Desc {
		t.Errorf("fetch.parallel description = %q, want %q", byKey["fetch.parallel"], spec.Desc)
	}
	if byKey["fetch.workspace_mode"] != "" {
		t.Errorf("fetch.workspace_mode is outside the registry, description = %q, want empty", byKey["fetch.workspace_mode"])
	}
}

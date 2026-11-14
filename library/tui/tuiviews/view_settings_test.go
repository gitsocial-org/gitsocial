// view_settings_test.go - The settings view renders, cursors and edits one row list
package tuiviews

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	zone "github.com/lrstanley/bubblezone/v2"

	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/settings"
	"github.com/gitsocial-org/gitsocial/library/tui/tuicore"
)

// TestMain installs the zone manager the rendered rows mark themselves with.
func TestMain(m *testing.M) {
	zone.NewGlobal()
	os.Exit(m.Run())
}

// ansiEscape matches the styling sequences the renderer wraps every row in.
var ansiEscape = regexp.MustCompile("\x1b\\[[0-9;]*m")

// isolatePersonalRepo points the personal config at a fresh temp path for the test.
func isolatePersonalRepo(t *testing.T) {
	t.Helper()
	t.Setenv("GITSOCIAL_PERSONAL_REPO", filepath.Join(t.TempDir(), "personal"))
}

// loadedSettingsView returns a settings view holding the current settings, sized so every row renders.
func loadedSettingsView(t *testing.T) (*SettingsView, *tuicore.State) {
	t.Helper()
	data, err := settings.Load("")
	if err != nil {
		t.Fatalf("settings.Load: %v", err)
	}
	v := NewSettingsView()
	v.HandleLoaded(SettingsViewLoadedMsg{Settings: data, Keys: settings.ListAll(data)})
	return v, &tuicore.State{Width: 120, Height: 80, Registry: tuicore.NewRegistry()}
}

// renderedSettingKeys returns the setting key shown on each rendered row, top to bottom.
func renderedSettingKeys(t *testing.T, v *SettingsView, state *tuicore.State) []string {
	t.Helper()
	var keys []string
	for _, line := range strings.Split(ansiEscape.ReplaceAllString(v.Render(state), ""), "\n") {
		found := ""
		for _, key := range settings.ListKeys() {
			if strings.Contains(line, key) {
				if found != "" {
					t.Fatalf("line %q carries both %s and %s", line, found, key)
				}
				found = key
			}
		}
		if found != "" {
			keys = append(keys, found)
		}
	}
	if len(keys) == 0 {
		t.Fatal("no settings rows rendered")
	}
	return keys
}

// writtenSettingKeys lists the keys the personal config holds a value for.
func writtenSettingKeys(t *testing.T) []string {
	t.Helper()
	backend := settings.NewPersonalConfigBackend()
	var keys []string
	for _, key := range settings.ListKeys() {
		if value, ok := backend.Get(key); ok && value != "" {
			keys = append(keys, key)
		}
	}
	return keys
}

// TestSettingsEditActsOnRowUnderCursor checks that every row edits the key it renders.
func TestSettingsEditActsOnRowUnderCursor(t *testing.T) {
	isolatePersonalRepo(t)
	base, baseState := loadedSettingsView(t)
	rendered := renderedSettingKeys(t, base, baseState)

	for row, key := range rendered {
		t.Run(key, func(t *testing.T) {
			isolatePersonalRepo(t)
			v, _ := loadedSettingsView(t)
			v.cursor = row
			v.editOrCycleSetting()
			if v.IsInputActive() {
				if v.input.Value() == "" {
					v.input.SetValue("https://example.com/hook") // a key with an empty default writes nothing until it has a value
				}
				v.saveCurrentSetting()
			}
			if v.err != "" {
				t.Fatalf("row %d (%s): %s", row, key, v.err)
			}
			written := writtenSettingKeys(t)
			// The two per-repo keys live outside the registry, so they write no key here.
			if key == "fetch.workspace_mode" || key == "notifications.level" {
				if len(written) != 0 {
					t.Fatalf("row %d (%s) wrote %v; want nothing", row, key, written)
				}
				return
			}
			if len(written) != 1 || written[0] != key {
				t.Fatalf("row %d renders %s but wrote %v", row, key, written)
			}
		})
	}
}

// TestSettingsRowsShowEveryRegisteredKey checks that every key reaches a row unless another view declares it.
func TestSettingsRowsShowEveryRegisteredKey(t *testing.T) {
	isolatePersonalRepo(t)
	v, state := loadedSettingsView(t)
	rendered := renderedSettingKeys(t, v, state)
	shown := make(map[string]bool, len(rendered))
	for _, key := range rendered {
		shown[key] = true
	}
	for _, key := range settings.ListKeys() {
		if !shown[key] && !keysOwnedElsewhere[key] {
			t.Errorf("settings view shows no row for %s", key)
		}
	}
}

// TestSettingsCursorStopsAtLastRow checks that end lands on the last rendered row.
func TestSettingsCursorStopsAtLastRow(t *testing.T) {
	isolatePersonalRepo(t)
	v, state := loadedSettingsView(t)
	rendered := renderedSettingKeys(t, v, state)
	v.Update(tea.KeyPressMsg{Code: tea.KeyEnd}, state)
	if v.cursor != len(rendered)-1 {
		t.Errorf("end put the cursor on row %d; the last rendered row is %d", v.cursor, len(rendered)-1)
	}
}

// TestSettingsLevelRowCyclesWorkspaceLevel checks that the level row shows the level of the workspace and writes the next one on each edit.
func TestSettingsLevelRowCyclesWorkspaceLevel(t *testing.T) {
	isolatePersonalRepo(t)
	workdir := t.TempDir()
	for _, args := range [][]string{{"init", "-q"}, {"remote", "add", "origin", "https://example.com/owner/repo"}} {
		if _, err := git.ExecGit(workdir, args); err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
	}
	v, state := loadedSettingsView(t)
	v.workdir, state.Workdir = workdir, workdir
	for i, row := range v.rows {
		if row.key == "notifications.level" {
			v.cursor = i
		}
	}
	for _, want := range []string{"all", "off", "default"} {
		v.editOrCycleSetting()
		if v.err != "" {
			t.Fatalf("set %s: %s", want, v.err)
		}
		if got := v.resolveNotificationLevel(state); got != want {
			t.Errorf("level after an edit = %q, want %q", got, want)
		}
	}
	if levels := settings.NotificationLevels(); len(levels) != 0 {
		t.Errorf("levels after default = %v, want no entry", levels)
	}
	if out := ansiEscape.ReplaceAllString(v.Render(state), ""); !strings.Contains(out, "notifications.level") || !strings.Contains(out, "this repository") {
		t.Errorf("the view does not show the level row with its scope:\n%s", out)
	}
}

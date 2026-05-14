// personal_backend_test.go - Tests for personalConfigBackend, auto-init,
// workspace-mode direct API, and Load() overlay.
package settings

import (
	"path/filepath"
	"testing"
)

// freshPersonalRepo points GITSOCIAL_PERSONAL_REPO at a temp path for the
// test's duration. The repo is not pre-created — auto-init handles that on
// first write.
func freshPersonalRepo(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("GITSOCIAL_PERSONAL_REPO", filepath.Join(dir, "personal"))
}

func TestPersonalRepoPath_EnvOverride(t *testing.T) {
	t.Setenv("GITSOCIAL_PERSONAL_REPO", "/tmp/gitsocial-test-personal-path-xyz")
	got, err := PersonalRepoPath()
	if err != nil {
		t.Fatalf("PersonalRepoPath: %v", err)
	}
	if got != "/tmp/gitsocial-test-personal-path-xyz" {
		t.Errorf("PersonalRepoPath = %q; want env override", got)
	}
}

func TestPersonalRepoPath_DefaultUnderHome(t *testing.T) {
	t.Setenv("GITSOCIAL_PERSONAL_REPO", "")
	got, err := PersonalRepoPath()
	if err != nil {
		t.Fatalf("PersonalRepoPath: %v", err)
	}
	if got == "" {
		t.Errorf("PersonalRepoPath returned empty path with no env override")
	}
}

func TestPersonalConfigBackend_GetMissingRepo(t *testing.T) {
	t.Setenv("GITSOCIAL_PERSONAL_REPO", "/tmp/does-not-exist-gitsocial-test")
	b := NewPersonalConfigBackend()
	if v, ok := b.Get("output.color"); ok || v != "" {
		t.Errorf("Get on missing repo = (%q, %v); want (\"\", false)", v, ok)
	}
}

// TestPersonalConfigBackend_SetAutoInits verifies the simplification: a first
// write to a missing personal repo creates the bare repo at PersonalRepoPath()
// and lands the value, no explicit `gitsocial personal init` required.
func TestPersonalConfigBackend_SetAutoInits(t *testing.T) {
	freshPersonalRepo(t)
	b := NewPersonalConfigBackend()
	if PersonalRepoExists() {
		t.Fatalf("personal repo should not exist before Set")
	}
	if err := b.Set("output.color", "never"); err != nil {
		t.Fatalf("Set should auto-init: %v", err)
	}
	if !PersonalRepoExists() {
		t.Errorf("personal repo should exist after Set")
	}
	got, ok := b.Get("output.color")
	if !ok || got != "never" {
		t.Errorf("Get after auto-init Set = (%q, %v); want (\"never\", true)", got, ok)
	}
}

func TestPersonalConfigBackend_RoundTrip(t *testing.T) {
	freshPersonalRepo(t)
	b := NewPersonalConfigBackend()

	cases := []struct {
		key, val string
	}{
		{"output.color", "never"},
		{"display.show_email", "true"},
		{"log.level", "debug"},
		{"identity.dns_verification", "true"},
		{"fetch.parallel", "8"},
	}
	for _, c := range cases {
		if err := b.Set(c.key, c.val); err != nil {
			t.Fatalf("Set(%s, %s): %v", c.key, c.val, err)
		}
	}
	for _, c := range cases {
		got, ok := b.Get(c.key)
		if !ok {
			t.Errorf("Get(%s) returned !ok after Set", c.key)
			continue
		}
		if got != c.val {
			t.Errorf("Get(%s) = %q after Set(%q); want %q", c.key, got, c.val, c.val)
		}
	}
}

func TestPersonalConfigBackend_SetRejectsInvalidValue(t *testing.T) {
	freshPersonalRepo(t)
	b := NewPersonalConfigBackend()

	if err := b.Set("output.color", "purple"); err == nil {
		t.Errorf("Set(output.color, purple) should fail enum validation")
	}
	if err := b.Set("fetch.parallel", "not-a-number"); err == nil {
		t.Errorf("Set(fetch.parallel, not-a-number) should fail int validation")
	}
	if err := b.Set("display.show_email", "maybe"); err == nil {
		t.Errorf("Set(display.show_email, maybe) should fail bool validation")
	}
	if err := b.Set("does.not.exist", "x"); err == nil {
		t.Errorf("Set on unknown key should fail")
	}
}

func TestManager_SourceOf_Personal(t *testing.T) {
	freshPersonalRepo(t)
	mgr := NewManager()
	if err := mgr.Write("output.color", "never"); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if got := mgr.sourceOf("output.color", ScopePersonalConfig); got != SourcePersonalConfig {
		t.Errorf("sourceOf(output.color) with personal value set = %v; want SourcePersonalConfig", got)
	}
}

// TestLoad_OverlaysPersonalConfig: write a ScopePersonalConfig key via
// Manager, then load *Settings via Load() and verify the in-memory struct
// surfaces the synced value through Get(s, key). This is what makes legacy
// readers (TUI app, fetch CLI, etc.) transparently pick up the value.
func TestLoad_OverlaysPersonalConfig(t *testing.T) {
	freshPersonalRepo(t)
	mgr := NewManager()
	if err := mgr.Write("output.color", "never"); err != nil {
		t.Fatalf("Write output.color: %v", err)
	}
	if err := mgr.Write("display.show_email", "true"); err != nil {
		t.Fatalf("Write display.show_email: %v", err)
	}

	s, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got, _ := Get(s, "output.color"); got != "never" {
		t.Errorf("Get(output.color) after overlay = %q, want \"never\"", got)
	}
	if !s.Display.ShowEmail {
		t.Errorf("Display.ShowEmail after overlay = false, want true")
	}
}

// TestWorkspaceMode_RoundTrip verifies the per-repo-URL workspace_modes API
// stores and retrieves entries via the personal config.
func TestWorkspaceMode_RoundTrip(t *testing.T) {
	freshPersonalRepo(t)

	const (
		repoA = "https://example.com/a.git"
		repoB = "https://example.com/b.git"
	)
	if got := GetWorkspaceMode(repoA); got != "" {
		t.Errorf("GetWorkspaceMode pre-write = %q; want \"\"", got)
	}
	if err := WriteWorkspaceMode(repoA, "*"); err != nil {
		t.Fatalf("WriteWorkspaceMode(A, *): %v", err)
	}
	if err := WriteWorkspaceMode(repoB, "default"); err != nil {
		t.Fatalf("WriteWorkspaceMode(B, default): %v", err)
	}
	if got := GetWorkspaceMode(repoA); got != "*" {
		t.Errorf("GetWorkspaceMode(A) = %q; want \"*\"", got)
	}
	if got := GetWorkspaceMode(repoB); got != "default" {
		t.Errorf("GetWorkspaceMode(B) = %q; want \"default\"", got)
	}

	modes := LoadWorkspaceModes()
	if len(modes) != 2 {
		t.Errorf("LoadWorkspaceModes len = %d; want 2", len(modes))
	}

	// Empty mode deletes the entry.
	if err := WriteWorkspaceMode(repoA, ""); err != nil {
		t.Fatalf("WriteWorkspaceMode(A, \"\"): %v", err)
	}
	if got := GetWorkspaceMode(repoA); got != "" {
		t.Errorf("GetWorkspaceMode(A) after delete = %q; want \"\"", got)
	}
}

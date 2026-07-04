// main_test.go - Shared test fixture setup via TestMain
package test

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/gitsocial-org/gitsocial/library/core/cache"
)

var sharedFixture *Fixture

func TestMain(m *testing.M) {
	flag.Parse()
	// When generating fixture, skip shared fixture setup (tarball doesn't exist yet)
	if *generateFlag {
		os.Exit(m.Run())
	}

	// Isolate from host settings so the settings view loads DefaultSettings()
	// (produces deterministic output regardless of ~/.config/gitmsg/settings.json)
	origHome := os.Getenv("HOME")
	tmpHome, err := os.MkdirTemp("", "tui-test-home-")
	if err != nil {
		panic("create temp home: " + err.Error())
	}
	os.Setenv("HOME", tmpHome)
	// HOME alone doesn't isolate config paths: UserConfigDir prefers
	// XDG_CONFIG_HOME (set on Linux CI runners).
	origXDGConfig := os.Getenv("XDG_CONFIG_HOME")
	os.Setenv("XDG_CONFIG_HOME", filepath.Join(tmpHome, ".config"))
	os.Unsetenv("GITSOCIAL_PERSONAL_REPO")

	// Create a shared fixture for all tests. This avoids repeating expensive
	// git init + seed + sync operations per test.
	sharedFixture = setupFixtureForMain()

	code := m.Run()

	// Cleanup
	os.Setenv("HOME", origHome)
	if origXDGConfig == "" {
		os.Unsetenv("XDG_CONFIG_HOME")
	} else {
		os.Setenv("XDG_CONFIG_HOME", origXDGConfig)
	}
	os.RemoveAll(tmpHome)
	cache.Reset()
	if sharedFixture != nil {
		os.RemoveAll(sharedFixture.Workdir)
		os.RemoveAll(sharedFixture.CacheDir)
	}
	os.Exit(code)
}

// getFixture returns the shared fixture for read-only tests.
// Tests that modify state should use SetupFixture(t) for isolation.
func getFixture(t *testing.T) *Fixture {
	t.Helper()
	if sharedFixture == nil {
		t.Fatal("shared fixture not initialized")
	}
	return sharedFixture
}

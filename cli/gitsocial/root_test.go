// root_test.go - the cache directory resolution of the root command
package main

import (
	"path/filepath"
	"testing"
)

func TestResolveCacheDir(t *testing.T) {
	t.Setenv(cacheDirEnv, "")
	if got, err := resolveCacheDir("/flag"); err != nil || got != "/flag" {
		t.Errorf("flag: got %q, %v", got, err)
	}
	t.Setenv(cacheDirEnv, "/env")
	if got, err := resolveCacheDir(""); err != nil || got != "/env" {
		t.Errorf("env: got %q, %v", got, err)
	}
	if got, err := resolveCacheDir("/flag"); err != nil || got != "/flag" {
		t.Errorf("flag over env: got %q, %v", got, err)
	}
	t.Setenv(cacheDirEnv, "")
	got, err := resolveCacheDir("")
	if err != nil || filepath.Base(got) != "gitsocial" || filepath.Base(filepath.Dir(got)) != ".cache" {
		t.Errorf("default: got %q, %v", got, err)
	}
}

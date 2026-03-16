// util_test.go - Tests for CLI utility functions
package main

import (
	"context"
	"testing"

	"github.com/spf13/cobra"
)

func TestExitCode(t *testing.T) {
	tests := []struct {
		code string
		want int
	}{
		{"NOT_A_REPOSITORY", ExitNotRepo},
		{"INVALID_ARGUMENT", ExitInvalidArgs},
		{"PERMISSION_DENIED", ExitPermission},
		{"NETWORK_ERROR", ExitNetwork},
		{"UNKNOWN_ERROR", ExitError},
		{"", ExitError},
	}

	for _, tt := range tests {
		t.Run(tt.code, func(t *testing.T) {
			got := ExitCode(tt.code)
			if got != tt.want {
				t.Errorf("ExitCode(%q) = %d, want %d", tt.code, got, tt.want)
			}
		})
	}
}

func TestWithConfig_GetConfig(t *testing.T) {
	cfg := &Config{WorkDir: "/test", CacheDir: "/cache", JSONOutput: true}
	ctx := WithConfig(context.Background(), cfg)

	cmd := &cobra.Command{}
	cmd.SetContext(ctx)

	got := GetConfig(cmd)
	if got == nil {
		t.Fatal("GetConfig() returned nil")
	}
	if got.WorkDir != "/test" {
		t.Errorf("WorkDir = %q, want %q", got.WorkDir, "/test")
	}
	if got.CacheDir != "/cache" {
		t.Errorf("CacheDir = %q, want %q", got.CacheDir, "/cache")
	}
	if !got.JSONOutput {
		t.Error("JSONOutput should be true")
	}
}

func TestGetConfig_nilContext(t *testing.T) {
	cmd := &cobra.Command{}
	got := GetConfig(cmd)
	if got != nil {
		t.Errorf("GetConfig(nil context) = %v, want nil", got)
	}
}

func TestGetConfig_noConfig(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	got := GetConfig(cmd)
	if got != nil {
		t.Errorf("GetConfig(no config) = %v, want nil", got)
	}
}

func TestExitConstants(t *testing.T) {
	if ExitSuccess != 0 {
		t.Errorf("ExitSuccess = %d, want 0", ExitSuccess)
	}
	if ExitError != 1 {
		t.Errorf("ExitError = %d, want 1", ExitError)
	}
	if ExitInvalidArgs != 2 {
		t.Errorf("ExitInvalidArgs = %d, want 2", ExitInvalidArgs)
	}
}

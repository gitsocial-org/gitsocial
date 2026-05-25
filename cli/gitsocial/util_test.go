// util_test.go - Tests for CLI utility functions
package main

import (
	"context"
	"os"
	"path/filepath"
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

// TestStripCommentLines locks the §3.8 editor convention: `#`-prefixed lines
// in the editor's saved content are dropped before the body is sent to memo.
func TestStripCommentLines(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"no comments", "hello\nworld", "hello\nworld"},
		{"strip leading comments", "# top\nreal\nbody", "real\nbody"},
		{"strip interleaved comments", "real\n# midway\nmore", "real\nmore"},
		{"strip indented comments", "real\n  # indented\nmore", "real\nmore"},
		{"only comments → empty", "# a\n# b\n", ""},
		{"trim trailing newline", "real body\n\n\n", "real body"},
		{"hash inside text preserved", "this # is not a comment\nokay", "this # is not a comment\nokay"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := stripCommentLines(c.in); got != c.want {
				t.Errorf("stripCommentLines(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

// TestOpenInEditor exercises the editor end-to-end via a mock editor that
// rewrites the temp file's contents — verifies the saved content comes back
// and comment-stripping fires on the round trip.
func TestOpenInEditor(t *testing.T) {
	dir := t.TempDir()
	mockEditor := filepath.Join(dir, "mock-editor.sh")
	mockBody := "# this line is a comment\nactual body content\n"
	script := "#!/bin/sh\ncat <<'EOF' > \"$1\"\n" + mockBody + "EOF\n"
	if err := os.WriteFile(mockEditor, []byte(script), 0o755); err != nil {
		t.Fatalf("write mock editor: %v", err)
	}

	origEditor := os.Getenv("GITSOCIAL_EDITOR")
	os.Setenv("GITSOCIAL_EDITOR", mockEditor)
	t.Cleanup(func() {
		if origEditor == "" {
			os.Unsetenv("GITSOCIAL_EDITOR")
		} else {
			os.Setenv("GITSOCIAL_EDITOR", origEditor)
		}
	})

	got, err := OpenInEditor("# template\n", ".md")
	if err != nil {
		t.Fatalf("OpenInEditor: %v", err)
	}
	if got != "actual body content" {
		t.Errorf("got %q, want \"actual body content\" (comment stripped)", got)
	}
}

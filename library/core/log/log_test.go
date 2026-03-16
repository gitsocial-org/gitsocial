// log_test.go - Tests for log level parsing and conversion
package log

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestParseLevel(t *testing.T) {
	tests := []struct {
		input string
		want  Level
	}{
		{"debug", LevelDebug},
		{"info", LevelInfo},
		{"warn", LevelWarn},
		{"error", LevelError},
		{"", LevelInfo},
		{"unknown", LevelInfo},
		{"DEBUG", LevelInfo},
		{"Info", LevelInfo},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := ParseLevel(tt.input)
			if got != tt.want {
				t.Errorf("ParseLevel(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

func TestToSlogLevel(t *testing.T) {
	tests := []struct {
		input Level
		want  slog.Level
	}{
		{LevelDebug, slog.LevelDebug},
		{LevelInfo, slog.LevelInfo},
		{LevelWarn, slog.LevelWarn},
		{LevelError, slog.LevelError},
		{Level(99), slog.LevelInfo},
	}
	for _, tt := range tests {
		t.Run(tt.want.String(), func(t *testing.T) {
			got := toSlogLevel(tt.input)
			if got != tt.want {
				t.Errorf("toSlogLevel(%d) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestLevelConstants(t *testing.T) {
	if LevelDebug >= LevelInfo {
		t.Error("LevelDebug should be less than LevelInfo")
	}
	if LevelInfo >= LevelWarn {
		t.Error("LevelInfo should be less than LevelWarn")
	}
	if LevelWarn >= LevelError {
		t.Error("LevelWarn should be less than LevelError")
	}
}

func TestModeConstants(t *testing.T) {
	if ModeText != 0 {
		t.Errorf("ModeText = %d, want 0", ModeText)
	}
	if ModeJSON != 1 {
		t.Errorf("ModeJSON = %d, want 1", ModeJSON)
	}
	if ModeSilent != 2 {
		t.Errorf("ModeSilent = %d, want 2", ModeSilent)
	}
}

func TestInit_silent(t *testing.T) {
	Init(Config{Mode: ModeSilent})
	Debug("should not panic")
	Info("should not panic")
	Warn("should not panic")
	Error("should not panic")
}

func TestInit_text(t *testing.T) {
	var buf bytes.Buffer
	Init(Config{Mode: ModeText, Level: LevelDebug, Output: &buf})
	Info("hello", "key", "val")
	out := buf.String()
	if !strings.Contains(out, "hello") {
		t.Errorf("expected text output to contain 'hello', got %q", out)
	}
	if !strings.Contains(out, "key=val") {
		t.Errorf("expected text output to contain 'key=val', got %q", out)
	}
}

func TestInit_json(t *testing.T) {
	var buf bytes.Buffer
	Init(Config{Mode: ModeJSON, Level: LevelDebug, Output: &buf})
	Info("hello", "key", "val")
	out := buf.String()
	if !strings.Contains(out, `"msg":"hello"`) {
		t.Errorf("expected JSON output to contain '\"msg\":\"hello\"', got %q", out)
	}
	if !strings.Contains(out, `"key":"val"`) {
		t.Errorf("expected JSON output to contain '\"key\":\"val\"', got %q", out)
	}
}

func TestInit_nilOutput(t *testing.T) {
	Init(Config{Mode: ModeText, Level: LevelInfo})
	Info("should not panic with nil output")
}

func TestWith(t *testing.T) {
	Init(Config{Mode: ModeSilent})
	l := With("key", "value")
	if l == nil {
		t.Fatal("With() returned nil")
	}
	l.Debug("test")
	l.Info("test")
	l.Warn("test")
	l.Error("test")
}

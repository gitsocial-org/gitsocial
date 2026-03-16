// settings_test.go - Tests for user settings management
package settings

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDefaultSettings(t *testing.T) {
	s := DefaultSettings()
	if s.Fetch.Parallel != 4 {
		t.Errorf("Fetch.Parallel = %d, want 4", s.Fetch.Parallel)
	}
	if s.Fetch.Timeout != 30 {
		t.Errorf("Fetch.Timeout = %d, want 30", s.Fetch.Timeout)
	}
	if s.Output.Color != "auto" {
		t.Errorf("Output.Color = %q, want %q", s.Output.Color, "auto")
	}
	if s.Log.Level != "info" {
		t.Errorf("Log.Level = %q, want %q", s.Log.Level, "info")
	}
	if s.Display.ShowEmail != false {
		t.Error("Display.ShowEmail should be false")
	}
	if !s.Extensions.Social {
		t.Error("Extensions.Social should be true")
	}
	if !s.Extensions.PM {
		t.Error("Extensions.PM should be true")
	}
	if !s.Extensions.Release {
		t.Error("Extensions.Release should be true")
	}
	if !s.Extensions.Review {
		t.Error("Extensions.Review should be true")
	}
}

func TestGet(t *testing.T) {
	s := DefaultSettings()

	tests := []struct {
		key    string
		want   string
		wantOk bool
	}{
		{"fetch.parallel", "4", true},
		{"fetch.timeout", "30", true},
		{"output.color", "auto", true},
		{"log.level", "info", true},
		{"display.show_email", "false", true},
		{"extensions.social", "true", true},
		{"extensions.pm", "true", true},
		{"extensions.release", "true", true},
		{"extensions.review", "true", true},
		{"unknown.key", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			got, ok := Get(s, tt.key)
			if ok != tt.wantOk {
				t.Errorf("Get(%q) ok = %v, want %v", tt.key, ok, tt.wantOk)
			}
			if got != tt.want {
				t.Errorf("Get(%q) = %q, want %q", tt.key, got, tt.want)
			}
		})
	}
}

func TestSet(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		value   string
		wantErr bool
		check   func(t *testing.T, s *Settings)
	}{
		{
			name:  "fetch.parallel valid",
			key:   "fetch.parallel",
			value: "8",
			check: func(t *testing.T, s *Settings) {
				if s.Fetch.Parallel != 8 {
					t.Errorf("Fetch.Parallel = %d, want 8", s.Fetch.Parallel)
				}
			},
		},
		{
			name:    "fetch.parallel zero",
			key:     "fetch.parallel",
			value:   "0",
			wantErr: true,
		},
		{
			name:    "fetch.parallel negative",
			key:     "fetch.parallel",
			value:   "-1",
			wantErr: true,
		},
		{
			name:    "fetch.parallel non-integer",
			key:     "fetch.parallel",
			value:   "abc",
			wantErr: true,
		},
		{
			name:  "fetch.timeout valid",
			key:   "fetch.timeout",
			value: "60",
			check: func(t *testing.T, s *Settings) {
				if s.Fetch.Timeout != 60 {
					t.Errorf("Fetch.Timeout = %d, want 60", s.Fetch.Timeout)
				}
			},
		},
		{
			name:  "output.color always",
			key:   "output.color",
			value: "always",
			check: func(t *testing.T, s *Settings) {
				if s.Output.Color != "always" {
					t.Errorf("Output.Color = %q, want %q", s.Output.Color, "always")
				}
			},
		},
		{
			name:    "output.color invalid",
			key:     "output.color",
			value:   "bright",
			wantErr: true,
		},
		{
			name:  "log.level debug",
			key:   "log.level",
			value: "debug",
			check: func(t *testing.T, s *Settings) {
				if s.Log.Level != "debug" {
					t.Errorf("Log.Level = %q, want %q", s.Log.Level, "debug")
				}
			},
		},
		{
			name:    "log.level invalid",
			key:     "log.level",
			value:   "verbose",
			wantErr: true,
		},
		{
			name:  "display.show_email true",
			key:   "display.show_email",
			value: "true",
			check: func(t *testing.T, s *Settings) {
				if !s.Display.ShowEmail {
					t.Error("Display.ShowEmail should be true")
				}
			},
		},
		{
			name:    "display.show_email invalid",
			key:     "display.show_email",
			value:   "yes",
			wantErr: true,
		},
		{
			name:  "extensions.social false",
			key:   "extensions.social",
			value: "false",
			check: func(t *testing.T, s *Settings) {
				if s.Extensions.Social {
					t.Error("Extensions.Social should be false")
				}
			},
		},
		{
			name:    "unknown key",
			key:     "unknown.key",
			value:   "value",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := DefaultSettings()
			err := Set(s, tt.key, tt.value)
			if tt.wantErr {
				if err == nil {
					t.Error("Set() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("Set() error = %v", err)
			}
			if tt.check != nil {
				tt.check(t, s)
			}
		})
	}
}

func TestSaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	s := DefaultSettings()
	s.Fetch.Parallel = 8
	s.Log.Level = "debug"

	if err := Save(path, s); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if loaded.Fetch.Parallel != 8 {
		t.Errorf("Fetch.Parallel = %d, want 8", loaded.Fetch.Parallel)
	}
	if loaded.Log.Level != "debug" {
		t.Errorf("Log.Level = %q, want %q", loaded.Log.Level, "debug")
	}
}

func TestLoad_nonExistentFile(t *testing.T) {
	s, err := Load("/nonexistent/path/settings.json")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if s.Fetch.Parallel != 4 {
		t.Errorf("Should return defaults, got Fetch.Parallel = %d", s.Fetch.Parallel)
	}
}

func TestLoad_invalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	os.WriteFile(path, []byte("not json"), 0644)

	_, err := Load(path)
	if err == nil {
		t.Error("Load() should fail for invalid JSON")
	}
}

func TestSave_createsDirectory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "subdir", "settings.json")

	if err := Save(path, DefaultSettings()); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("File should exist after Save")
	}
}

func TestListKeys(t *testing.T) {
	keys := ListKeys()
	if len(keys) == 0 {
		t.Fatal("ListKeys() returned empty slice")
	}
	expected := map[string]bool{
		"fetch.parallel":     true,
		"fetch.timeout":      true,
		"output.color":       true,
		"log.level":          true,
		"display.show_email": true,
	}
	for key := range expected {
		found := false
		for _, k := range keys {
			if k == key {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("ListKeys() missing %q", key)
		}
	}
}

func TestListAll(t *testing.T) {
	s := DefaultSettings()
	all := ListAll(s)
	if len(all) == 0 {
		t.Fatal("ListAll() returned empty slice")
	}
	found := false
	for _, kv := range all {
		if kv.Key == "fetch.parallel" && kv.Value == "4" {
			found = true
		}
	}
	if !found {
		t.Error("ListAll() missing fetch.parallel=4")
	}
}

func TestParseKey(t *testing.T) {
	tests := []struct {
		key         string
		wantSection string
		wantName    string
		wantOk      bool
	}{
		{"fetch.parallel", "fetch", "parallel", true},
		{"log.level", "log", "level", true},
		{"nodot", "", "", false},
		{"a.b.c", "a", "b.c", true},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			section, name, ok := ParseKey(tt.key)
			if ok != tt.wantOk {
				t.Errorf("ParseKey(%q) ok = %v, want %v", tt.key, ok, tt.wantOk)
			}
			if section != tt.wantSection {
				t.Errorf("section = %q, want %q", section, tt.wantSection)
			}
			if name != tt.wantName {
				t.Errorf("name = %q, want %q", name, tt.wantName)
			}
		})
	}
}

func TestIsEnum(t *testing.T) {
	if !IsEnum("log.level") {
		t.Error("log.level should be an enum")
	}
	if !IsEnum("output.color") {
		t.Error("output.color should be an enum")
	}
	if IsEnum("fetch.parallel") {
		t.Error("fetch.parallel should not be an enum")
	}
}

func TestNextEnumValue(t *testing.T) {
	tests := []struct {
		key     string
		current string
		want    string
	}{
		{"log.level", "info", "warn"},
		{"log.level", "error", "debug"},
		{"log.level", "unknown", "debug"},
		{"output.color", "auto", "always"},
		{"output.color", "never", "auto"},
		{"fetch.parallel", "4", "4"},
	}

	for _, tt := range tests {
		t.Run(tt.key+"/"+tt.current, func(t *testing.T) {
			got := NextEnumValue(tt.key, tt.current)
			if got != tt.want {
				t.Errorf("NextEnumValue(%q, %q) = %q, want %q", tt.key, tt.current, got, tt.want)
			}
		})
	}
}

func TestDefaultPath(t *testing.T) {
	p, err := DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath() error = %v", err)
	}
	suffix := filepath.Join(".config", "gitmsg", "settings.json")
	if !strings.HasSuffix(p, suffix) {
		t.Errorf("DefaultPath() = %q, want suffix %q", p, suffix)
	}
}

func TestSave_andLoad_roundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "roundtrip.json")

	s := DefaultSettings()
	s.Extensions.Social = false
	s.Extensions.PM = false
	s.Extensions.Release = false
	s.Extensions.Review = false
	s.Display.ShowEmail = true

	if err := Save(path, s); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if loaded.Extensions.Social != false {
		t.Errorf("Extensions.Social = %v, want false", loaded.Extensions.Social)
	}
	if loaded.Extensions.PM != false {
		t.Errorf("Extensions.PM = %v, want false", loaded.Extensions.PM)
	}
	if loaded.Extensions.Release != false {
		t.Errorf("Extensions.Release = %v, want false", loaded.Extensions.Release)
	}
	if loaded.Extensions.Review != false {
		t.Errorf("Extensions.Review = %v, want false", loaded.Extensions.Review)
	}
	if loaded.Display.ShowEmail != true {
		t.Errorf("Display.ShowEmail = %v, want true", loaded.Display.ShowEmail)
	}
}

func TestLoad_unreadableFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod not supported on Windows")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "unreadable.json")

	if err := os.WriteFile(path, []byte(`{"fetch":{}}`), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := os.Chmod(path, 0000); err != nil {
		t.Fatalf("Chmod() error = %v", err)
	}

	_, err := Load(path)
	if err == nil {
		t.Error("Load() should fail for unreadable file")
	}
}

func TestSet_fetchTimeout_zero(t *testing.T) {
	s := DefaultSettings()
	err := Set(s, "fetch.timeout", "0")
	if err == nil {
		t.Error("Set(fetch.timeout, 0) should return error")
	}
}

func TestSet_fetchTimeout_negative(t *testing.T) {
	s := DefaultSettings()
	err := Set(s, "fetch.timeout", "-5")
	if err == nil {
		t.Error("Set(fetch.timeout, -5) should return error")
	}
}

func TestSet_fetchTimeout_nonInteger(t *testing.T) {
	s := DefaultSettings()
	err := Set(s, "fetch.timeout", "abc")
	if err == nil {
		t.Error("Set(fetch.timeout, abc) should return error")
	}
}

func TestSet_extensionsPM_invalid(t *testing.T) {
	s := DefaultSettings()
	err := Set(s, "extensions.pm", "yes")
	if err == nil {
		t.Error("Set(extensions.pm, yes) should return error")
	}
}

func TestSet_extensionsRelease_true(t *testing.T) {
	s := DefaultSettings()
	s.Extensions.Release = false
	err := Set(s, "extensions.release", "true")
	if err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	if s.Extensions.Release != true {
		t.Errorf("Extensions.Release = %v, want true", s.Extensions.Release)
	}
}

func TestSet_extensionsRelease_false(t *testing.T) {
	s := DefaultSettings()
	err := Set(s, "extensions.release", "false")
	if err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	if s.Extensions.Release != false {
		t.Errorf("Extensions.Release = %v, want false", s.Extensions.Release)
	}
}

func TestSet_extensionsReview_invalid(t *testing.T) {
	s := DefaultSettings()
	err := Set(s, "extensions.review", "1")
	if err == nil {
		t.Error("Set(extensions.review, 1) should return error")
	}
}

func TestSet_extensionsSocial_invalid(t *testing.T) {
	s := DefaultSettings()
	err := Set(s, "extensions.social", "yes")
	if err == nil {
		t.Error("Set(extensions.social, yes) should return error")
	}
}

func TestSet_extensionsPM_valid(t *testing.T) {
	s := DefaultSettings()
	if err := Set(s, "extensions.pm", "false"); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	if s.Extensions.PM != false {
		t.Errorf("Extensions.PM = %v, want false", s.Extensions.PM)
	}
}

func TestSet_extensionsRelease_invalid(t *testing.T) {
	s := DefaultSettings()
	err := Set(s, "extensions.release", "yes")
	if err == nil {
		t.Error("Set(extensions.release, yes) should return error")
	}
}

func TestSet_extensionsReview_valid(t *testing.T) {
	s := DefaultSettings()
	if err := Set(s, "extensions.review", "false"); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	if s.Extensions.Review != false {
		t.Errorf("Extensions.Review = %v, want false", s.Extensions.Review)
	}
}

func TestSave_mkdirFails(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocked")
	os.WriteFile(blocker, []byte("x"), 0644)
	path := filepath.Join(blocker, "sub", "settings.json")

	err := Save(path, DefaultSettings())
	if err == nil {
		t.Error("Save() should fail when MkdirAll fails")
	}
	if !strings.Contains(err.Error(), "failed to create settings directory") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestSave_writeFileFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod not supported on Windows")
	}
	dir := t.TempDir()
	os.Chmod(dir, 0555)
	t.Cleanup(func() { os.Chmod(dir, 0755) })
	path := filepath.Join(dir, "settings.json")

	err := Save(path, DefaultSettings())
	if err == nil {
		t.Error("Save() should fail when directory is read-only")
	}
	if !strings.Contains(err.Error(), "failed to write settings") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDefaultPath_noHome(t *testing.T) {
	t.Setenv("HOME", "")
	_, err := DefaultPath()
	// On some systems this still succeeds via user database lookup;
	// we just ensure it doesn't panic
	_ = err
}

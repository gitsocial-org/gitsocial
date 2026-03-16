// settings.go - User settings management with file persistence
package settings

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Settings struct {
	Fetch      FetchSettings      `json:"fetch"`
	Output     OutputSettings     `json:"output"`
	Log        LogSettings        `json:"log"`
	Display    DisplaySettings    `json:"display"`
	Extensions ExtensionsSettings `json:"extensions"`
}

type ExtensionsSettings struct {
	Social  bool `json:"social"`
	PM      bool `json:"pm"`
	Release bool `json:"release"`
	Review  bool `json:"review"`
}

type FetchSettings struct {
	Parallel       int               `json:"parallel"`
	Timeout        int               `json:"timeout"`
	WorkspaceModes map[string]string `json:"workspace_modes,omitempty"`
}

type OutputSettings struct {
	Color string `json:"color"`
}

type LogSettings struct {
	Level string `json:"level"`
}

type DisplaySettings struct {
	ShowEmail bool `json:"show_email"`
}

// DefaultSettings returns settings with default values.
func DefaultSettings() *Settings {
	return &Settings{
		Fetch: FetchSettings{
			Parallel: 4,
			Timeout:  30,
		},
		Output: OutputSettings{
			Color: "auto",
		},
		Log: LogSettings{
			Level: "info",
		},
		Display: DisplaySettings{
			ShowEmail: false,
		},
		Extensions: ExtensionsSettings{
			Social:  true,
			PM:      true,
			Release: true,
			Review:  true,
		},
	}
}

// DefaultPath returns the default settings file path.
func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "gitmsg", "settings.json"), nil
}

// Load reads settings from a file, returning defaults if not found.
func Load(path string) (*Settings, error) {
	s := DefaultSettings()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, err
	}

	if err := json.Unmarshal(data, s); err != nil {
		return nil, fmt.Errorf("failed to parse settings: %w", err)
	}

	return s, nil
}

// Save writes settings to a file.
func Save(path string, s *Settings) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create settings directory: %w", err)
	}

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal settings: %w", err)
	}

	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("failed to write settings: %w", err)
	}

	return nil
}

// Get retrieves a setting value by key.
func Get(s *Settings, key string) (string, bool) {
	switch key {
	case "fetch.parallel":
		return strconv.Itoa(s.Fetch.Parallel), true
	case "fetch.timeout":
		return strconv.Itoa(s.Fetch.Timeout), true
	case "output.color":
		return s.Output.Color, s.Output.Color != ""
	case "log.level":
		return s.Log.Level, s.Log.Level != ""
	case "display.show_email":
		return strconv.FormatBool(s.Display.ShowEmail), true
	case "extensions.social":
		return strconv.FormatBool(s.Extensions.Social), true
	case "extensions.pm":
		return strconv.FormatBool(s.Extensions.PM), true
	case "extensions.release":
		return strconv.FormatBool(s.Extensions.Release), true
	case "extensions.review":
		return strconv.FormatBool(s.Extensions.Review), true
	case "fetch.workspace_mode":
		return "(per-repo)", true
	default:
		return "", false
	}
}

// Set updates a setting value by key with validation.
func Set(s *Settings, key, value string) error {
	switch key {
	case "fetch.parallel":
		n, err := strconv.Atoi(value)
		if err != nil || n < 1 {
			return fmt.Errorf("fetch.parallel must be a positive integer")
		}
		s.Fetch.Parallel = n
	case "fetch.timeout":
		n, err := strconv.Atoi(value)
		if err != nil || n < 1 {
			return fmt.Errorf("fetch.timeout must be a positive integer")
		}
		s.Fetch.Timeout = n
	case "output.color":
		if value != "auto" && value != "always" && value != "never" {
			return fmt.Errorf("output.color must be auto, always, or never")
		}
		s.Output.Color = value
	case "log.level":
		if value != "debug" && value != "info" && value != "warn" && value != "error" {
			return fmt.Errorf("log.level must be debug, info, warn, or error")
		}
		s.Log.Level = value
	case "display.show_email":
		if value != "true" && value != "false" {
			return fmt.Errorf("display.show_email must be true or false")
		}
		s.Display.ShowEmail = value == "true"
	case "extensions.social":
		if value != "true" && value != "false" {
			return fmt.Errorf("extensions.social must be true or false")
		}
		s.Extensions.Social = value == "true"
	case "extensions.pm":
		if value != "true" && value != "false" {
			return fmt.Errorf("extensions.pm must be true or false")
		}
		s.Extensions.PM = value == "true"
	case "extensions.release":
		if value != "true" && value != "false" {
			return fmt.Errorf("extensions.release must be true or false")
		}
		s.Extensions.Release = value == "true"
	case "extensions.review":
		if value != "true" && value != "false" {
			return fmt.Errorf("extensions.review must be true or false")
		}
		s.Extensions.Review = value == "true"
	case "fetch.workspace_mode":
		return fmt.Errorf("use settings view to change workspace mode (per-repo setting)")
	default:
		return fmt.Errorf("unknown settings key: %s", key)
	}
	return nil
}

// ListKeys returns all available setting keys.
func ListKeys() []string {
	return []string{
		"fetch.parallel",
		"fetch.timeout",
		"fetch.workspace_mode",
		"output.color",
		"log.level",
		"display.show_email",
		"extensions.social",
		"extensions.pm",
		"extensions.review",
		"extensions.release",
	}
}

// ListAll returns all settings as key-value pairs.
func ListAll(s *Settings) []KeyValue {
	keys := ListKeys()
	result := make([]KeyValue, 0, len(keys))
	for _, key := range keys {
		value, _ := Get(s, key)
		result = append(result, KeyValue{Key: key, Value: value})
	}
	return result
}

type KeyValue struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// ParseKey splits a setting key into section and name.
func ParseKey(key string) (section, name string, ok bool) {
	parts := strings.SplitN(key, ".", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	return parts[0], parts[1], true
}

var EnumOptions = map[string][]string{
	"log.level":            {"debug", "info", "warn", "error"},
	"output.color":         {"auto", "always", "never"},
	"display.show_email":   {"false", "true"},
	"fetch.workspace_mode": {"default", "*"},
	"extensions.social":    {"true", "false"},
	"extensions.pm":        {"true", "false"},
	"extensions.release":   {"true", "false"},
	"extensions.review":    {"true", "false"},
}

// GetWorkspaceMode returns the workspace fetch mode for a given repo URL.
// Returns "" if not set (first time), "default", or "*".
func GetWorkspaceMode(s *Settings, repoURL string) string {
	if s.Fetch.WorkspaceModes == nil {
		return ""
	}
	return s.Fetch.WorkspaceModes[repoURL]
}

// SetWorkspaceMode saves the workspace fetch mode for a given repo URL.
func SetWorkspaceMode(s *Settings, repoURL, mode string) {
	if s.Fetch.WorkspaceModes == nil {
		s.Fetch.WorkspaceModes = make(map[string]string)
	}
	s.Fetch.WorkspaceModes[repoURL] = mode
}

// IsEnum checks if a setting has enumerated options.
func IsEnum(key string) bool {
	_, ok := EnumOptions[key]
	return ok
}

// NextEnumValue returns the next value in an enum cycle.
func NextEnumValue(key, current string) string {
	opts, ok := EnumOptions[key]
	if !ok {
		return current
	}
	for i, v := range opts {
		if v == current {
			return opts[(i+1)%len(opts)]
		}
	}
	return opts[0]
}

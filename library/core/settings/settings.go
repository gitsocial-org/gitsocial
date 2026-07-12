// settings.go - In-memory Settings struct + legacy Get/Set used by the
// overlay path. All persistence lives in personal_backend.go (refs/gitmsg/
// core/config in the personal bare repo); no file is written to disk.
package settings

import (
	"fmt"
	"strconv"
	"strings"
)

// Settings is the runtime view of all configurable values. It's populated by
// Load() from defaults + personal-config overlay; nothing writes it to disk.
type Settings struct {
	Fetch      FetchSettings
	Output     OutputSettings
	Log        LogSettings
	Display    DisplaySettings
	Extensions ExtensionsSettings
	Identity   IdentitySettings
	S3         S3Settings
}

// S3Settings controls the s3:// remote backend.
type S3Settings struct {
	// Concurrency is the number of concurrent object uploads per push.
	Concurrency int
}

// IdentitySettings controls per-user identity verification policy.
type IdentitySettings struct {
	// DNSVerification enables fetching .well-known/gitmsg-id.json for verification.
	// Defaults to false: domain-owner attestation is a weaker trust path than
	// forge attestation, and is opt-in.
	DNSVerification bool
}

type ExtensionsSettings struct {
	Social  bool
	PM      bool
	Release bool
	Review  bool
	Memo    bool
}

type FetchSettings struct {
	Parallel       int
	Timeout        int
	AutoEnabled    bool
	AutoInterval   int
	AutoBackoff    bool
	WorkspaceModes map[string]string
}

type OutputSettings struct {
	Color string
}

type LogSettings struct {
	Level string
}

type DisplaySettings struct {
	ShowEmail bool
	Theme     string
}

// DefaultSettings returns settings with default values matching the Registry.
func DefaultSettings() *Settings {
	return &Settings{
		Fetch: FetchSettings{
			Parallel:     4,
			Timeout:      30,
			AutoEnabled:  false,
			AutoInterval: 300,
			AutoBackoff:  true,
		},
		Output: OutputSettings{
			Color: "auto",
		},
		Log: LogSettings{
			Level: "info",
		},
		Display: DisplaySettings{
			ShowEmail: false,
			Theme:     "auto",
		},
		Extensions: ExtensionsSettings{
			Social:  true,
			PM:      true,
			Release: true,
			Review:  true,
			Memo:    true,
		},
		Identity: IdentitySettings{
			DNSVerification: false,
		},
		S3: S3Settings{
			Concurrency: 16,
		},
	}
}

// Load returns the runtime Settings view: defaults overlaid with values from
// the personal-config ref. The path argument is ignored (kept for compatibility
// with callers that still pass DefaultPath()).
func Load(_ string) (*Settings, error) {
	s := DefaultSettings()
	overlayPersonalConfig(s)
	s.Fetch.WorkspaceModes = LoadWorkspaceModes()
	return s, nil
}

// DefaultPath returns a vestigial path under the user-config directory. The
// file isn't actually read or written anymore (personal repo is the store),
// but callers passing this to Load() still work.
func DefaultPath() (string, error) {
	dir, err := UserConfigDir()
	if err != nil {
		return "", err
	}
	return dir + "/gitsocial/settings.json", nil
}

// overlayPersonalConfig applies values from the personal-config ref onto the
// in-memory *Settings for every Registry key declared at ScopePersonalConfig.
// Errors from validation or a missing personal repo are silently ignored —
// the overlay is best-effort, since a missing repo must not stop the CLI.
func overlayPersonalConfig(s *Settings) {
	pb := NewPersonalConfigBackend()
	for _, spec := range Registry {
		if spec.Scope != ScopePersonalConfig {
			continue
		}
		if v, ok := pb.Get(spec.Key); ok && v != "" {
			_ = Set(s, spec.Key, v)
		}
	}
}

// Get retrieves a setting value by key from the in-memory *Settings.
func Get(s *Settings, key string) (string, bool) {
	switch key {
	case "fetch.parallel":
		return strconv.Itoa(s.Fetch.Parallel), true
	case "fetch.timeout":
		return strconv.Itoa(s.Fetch.Timeout), true
	case "fetch.auto.enabled":
		return strconv.FormatBool(s.Fetch.AutoEnabled), true
	case "fetch.auto.interval":
		return strconv.Itoa(s.Fetch.AutoInterval), true
	case "fetch.auto.backoff":
		return strconv.FormatBool(s.Fetch.AutoBackoff), true
	case "output.color":
		return s.Output.Color, s.Output.Color != ""
	case "log.level":
		return s.Log.Level, s.Log.Level != ""
	case "display.show_email":
		return strconv.FormatBool(s.Display.ShowEmail), true
	case "display.theme":
		return s.Display.Theme, s.Display.Theme != ""
	case "extensions.social":
		return strconv.FormatBool(s.Extensions.Social), true
	case "extensions.pm":
		return strconv.FormatBool(s.Extensions.PM), true
	case "extensions.release":
		return strconv.FormatBool(s.Extensions.Release), true
	case "extensions.review":
		return strconv.FormatBool(s.Extensions.Review), true
	case "extensions.memo":
		return strconv.FormatBool(s.Extensions.Memo), true
	case "identity.dns_verification":
		return strconv.FormatBool(s.Identity.DNSVerification), true
	case "s3.concurrency":
		return strconv.Itoa(s.S3.Concurrency), true
	case "fetch.workspace_mode":
		return "(per-repo)", true
	default:
		return "", false
	}
}

// Set updates a setting value by key with validation, mutating the in-memory
// *Settings. Used by the overlay path to apply personal-config values onto
// the struct.
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
	case "fetch.auto.enabled":
		if value != "true" && value != "false" {
			return fmt.Errorf("fetch.auto.enabled must be true or false")
		}
		s.Fetch.AutoEnabled = value == "true"
	case "fetch.auto.interval":
		n, err := strconv.Atoi(value)
		if err != nil || n < 60 {
			return fmt.Errorf("fetch.auto.interval must be at least 60 seconds")
		}
		s.Fetch.AutoInterval = n
	case "fetch.auto.backoff":
		if value != "true" && value != "false" {
			return fmt.Errorf("fetch.auto.backoff must be true or false")
		}
		s.Fetch.AutoBackoff = value == "true"
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
	case "display.theme":
		if value != "auto" && value != "light" && value != "dark" {
			return fmt.Errorf("display.theme must be auto, light, or dark")
		}
		s.Display.Theme = value
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
	case "extensions.memo":
		if value != "true" && value != "false" {
			return fmt.Errorf("extensions.memo must be true or false")
		}
		s.Extensions.Memo = value == "true"
	case "identity.dns_verification":
		if value != "true" && value != "false" {
			return fmt.Errorf("identity.dns_verification must be true or false")
		}
		s.Identity.DNSVerification = value == "true"
	case "s3.concurrency":
		n, err := strconv.Atoi(value)
		if err != nil || n < 1 {
			return fmt.Errorf("s3.concurrency must be a positive integer")
		}
		s.S3.Concurrency = n
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
		"fetch.auto.enabled",
		"fetch.auto.interval",
		"fetch.auto.backoff",
		"fetch.workspace_mode",
		"s3.concurrency",
		"output.color",
		"log.level",
		"display.show_email",
		"display.theme",
		"extensions.social",
		"extensions.pm",
		"extensions.review",
		"extensions.release",
		"extensions.memo",
		"identity.dns_verification",
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
	"log.level":                 {"debug", "info", "warn", "error"},
	"output.color":              {"auto", "always", "never"},
	"display.show_email":        {"false", "true"},
	"display.theme":             {"auto", "light", "dark"},
	"fetch.auto.enabled":        {"false", "true"},
	"fetch.auto.backoff":        {"false", "true"},
	"fetch.workspace_mode":      {"default", "*"},
	"extensions.social":         {"true", "false"},
	"extensions.pm":             {"true", "false"},
	"extensions.release":        {"true", "false"},
	"extensions.review":         {"true", "false"},
	"extensions.memo":           {"true", "false"},
	"identity.dns_verification": {"false", "true"},
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

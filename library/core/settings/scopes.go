// scopes.go - Typed allow-list of every settings key, with declared scope and type.
// Adding a key requires a code change here; callers cannot write to keys absent
// from Registry. Editable keys are scoped to the personal config ref
// (refs/gitmsg/core/config in the personal bare repo, synced across hosts); the
// remaining keys are read-only process environment.
package settings

import (
	"fmt"
	"strconv"
)

// Scope identifies which backend a key reads from and writes to.
type Scope int

const (
	// ScopePersonalConfig — refs/gitmsg/core/config in the personal bare repo
	// (synced across hosts). The default scope for editable keys.
	ScopePersonalConfig Scope = iota
	// ScopeEnv — process environment; read-only.
	ScopeEnv
)

// String returns the human-readable scope label used in UI listings.
func (s Scope) String() string {
	switch s {
	case ScopePersonalConfig:
		return "personal"
	case ScopeEnv:
		return "env"
	}
	return "unknown"
}

// KeyType classifies the value shape for validation and UI rendering.
type KeyType int

const (
	KeyString KeyType = iota
	KeyInt
	KeyBool
	KeyEnum
)

// KeySpec describes a single registered settings key.
type KeySpec struct {
	Key     string
	Scope   Scope
	Type    KeyType
	Enum    []string // populated when Type == KeyEnum
	Default string   // string form, parsed by callers
	Desc    string
}

// Registry is the authoritative list of every settings key the system knows
// about. Writes to unregistered keys are rejected at the Manager layer.
var Registry = []KeySpec{
	// User-scoped — synced via refs/gitmsg/core/config in the personal repo.
	{Key: "identity.dns_verification", Scope: ScopePersonalConfig, Type: KeyBool, Default: "false",
		Desc: "Enable DNS .well-known identity attestation (weaker than forge attestation)."},
	{Key: "output.color", Scope: ScopePersonalConfig, Type: KeyEnum, Enum: []string{"auto", "always", "never"}, Default: "auto",
		Desc: "Terminal color policy."},
	{Key: "display.show_email", Scope: ScopePersonalConfig, Type: KeyBool, Default: "false",
		Desc: "Show author email alongside name on cards."},
	{Key: "display.theme", Scope: ScopePersonalConfig, Type: KeyEnum, Enum: []string{"auto", "light", "dark"}, Default: "auto",
		Desc: "TUI color theme: auto detects terminal background, or force light/dark."},
	{Key: "log.level", Scope: ScopePersonalConfig, Type: KeyEnum, Enum: []string{"debug", "info", "warn", "error"}, Default: "info",
		Desc: "Logging verbosity."},
	{Key: "extensions.social", Scope: ScopePersonalConfig, Type: KeyBool, Default: "true",
		Desc: "Show the Social extension in nav."},
	{Key: "extensions.pm", Scope: ScopePersonalConfig, Type: KeyBool, Default: "true",
		Desc: "Show the PM extension in nav."},
	{Key: "extensions.release", Scope: ScopePersonalConfig, Type: KeyBool, Default: "true",
		Desc: "Show the Release extension in nav."},
	{Key: "extensions.review", Scope: ScopePersonalConfig, Type: KeyBool, Default: "true",
		Desc: "Show the Review extension in nav."},
	{Key: "extensions.memo", Scope: ScopePersonalConfig, Type: KeyBool, Default: "true",
		Desc: "Show the Memo extension in nav."},

	{Key: "fetch.parallel", Scope: ScopePersonalConfig, Type: KeyInt, Default: "4",
		Desc: "Concurrent fetch workers."},
	{Key: "fetch.timeout", Scope: ScopePersonalConfig, Type: KeyInt, Default: "30",
		Desc: "Per-repo fetch timeout in seconds."},
	{Key: "fetch.auto.enabled", Scope: ScopePersonalConfig, Type: KeyBool, Default: "false",
		Desc: "Periodically fetch in the TUI while it's open."},
	{Key: "fetch.auto.interval", Scope: ScopePersonalConfig, Type: KeyInt, Default: "300",
		Desc: "Auto-fetch interval in seconds (minimum 60)."},
	{Key: "fetch.auto.backoff", Scope: ScopePersonalConfig, Type: KeyBool, Default: "true",
		Desc: "Slow auto-fetch when idle; reset to the base interval when new items arrive."},

	// fetch.workspace_mode is a per-repo-URL map; handled outside the Registry
	// via Get/WriteWorkspaceMode (the personal config also stores the map, but
	// the shape doesn't fit Manager.Write's scalar contract).

	{Key: "GITSOCIAL_PPROF", Scope: ScopeEnv, Type: KeyEnum, Enum: []string{"cpu", "mem", "trace"},
		Desc: "Capture a profile for the current run."},
	{Key: "GITSOCIAL_PERSONAL_REPO", Scope: ScopeEnv, Type: KeyString,
		Desc: "Override the personal bare repo path (default: ~/.config/gitsocial/personal)."},
	{Key: "MEMO_SESSION_ID", Scope: ScopeEnv, Type: KeyString,
		Desc: "Pin the active memo session id (default: auto-generated per process)."},
	{Key: "MEMO_SESSION_DIR", Scope: ScopeEnv, Type: KeyString,
		Desc: "Override the memo session-repos parent directory (default: ~/.cache/gitsocial/memo/session)."},
}

// Lookup returns the KeySpec for a key, or (zero, false) if unregistered.
func Lookup(key string) (KeySpec, bool) {
	for _, k := range Registry {
		if k.Key == key {
			return k, true
		}
	}
	return KeySpec{}, false
}

// Validate checks that value conforms to the key's declared type (bool/int/enum).
// Unregistered keys return an error. KeyString accepts anything.
func Validate(key, value string) error {
	spec, ok := Lookup(key)
	if !ok {
		return fmt.Errorf("unknown settings key: %s", key)
	}
	switch spec.Type {
	case KeyBool:
		if value != "true" && value != "false" {
			return fmt.Errorf("%s must be true or false", key)
		}
	case KeyInt:
		n, err := strconv.Atoi(value)
		if err != nil || n < 0 {
			return fmt.Errorf("%s must be a non-negative integer", key)
		}
		if key == "fetch.auto.interval" && n < 60 {
			return fmt.Errorf("fetch.auto.interval must be at least 60 seconds")
		}
	case KeyEnum:
		for _, opt := range spec.Enum {
			if opt == value {
				return nil
			}
		}
		return fmt.Errorf("%s must be one of %v", key, spec.Enum)
	}
	return nil
}

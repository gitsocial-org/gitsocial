// backend.go - Backend interface + Manager facade for scope-routed reads/writes.
// Two backends today: personalConfigBackend (refs/gitmsg/core/config in the
// personal bare repo) and envBackend (process environment, read-only).
package settings

import (
	"fmt"
	"os"
)

// Backend is one concrete source of settings values.
type Backend interface {
	// Get returns the stored value for key, if any.
	Get(key string) (string, bool)
	// Set persists value for key. Returns an error if the backend is read-only.
	Set(key, value string) error
	// Scope identifies which scope this backend serves.
	Scope() Scope
}

// envBackend exposes process environment variables as a read-only settings
// source. Set always returns an error; the variable must be exported by the
// shell or parent process.
type envBackend struct{}

// NewEnvBackend returns the singleton env-backed source.
func NewEnvBackend() *envBackend { return &envBackend{} }

func (b *envBackend) Get(key string) (string, bool) {
	v := os.Getenv(key)
	return v, v != ""
}

func (b *envBackend) Set(key, _ string) error {
	return fmt.Errorf("settings key %q is environment-scoped and read-only", key)
}

func (b *envBackend) Scope() Scope { return ScopeEnv }

// Manager routes reads through the resolution chain and writes to the backend
// declared by the registry. Construct it once per process via NewManager;
// subsequent Resolve/Write calls are cheap.
type Manager struct {
	env      *envBackend
	personal *personalConfigBackend
}

// NewManager constructs a Manager. The personal-config backend is always
// attached; it tolerates a missing personal bare repo by yielding empty reads,
// and auto-initializes the repo on first Set.
func NewManager() *Manager {
	return &Manager{
		env:      NewEnvBackend(),
		personal: NewPersonalConfigBackend(),
	}
}

// Resolve returns the effective value for key using the resolution chain:
// env (when the key is env-scoped) → personal-config ref → registry default.
// Returns ("", false) for unregistered keys.
func (m *Manager) Resolve(key string) (string, bool) {
	spec, ok := Lookup(key)
	if !ok {
		return "", false
	}
	if spec.Scope == ScopeEnv {
		return m.env.Get(key)
	}
	if v, ok := m.personal.Get(key); ok && v != "" {
		return v, true
	}
	if spec.Default != "" {
		return spec.Default, true
	}
	return "", false
}

// Write persists value for key, dispatched to the backend named by the
// registry. Unregistered keys and env-scoped keys return errors — callers
// can't sneak writes past the allow-list.
func (m *Manager) Write(key, value string) error {
	spec, ok := Lookup(key)
	if !ok {
		return fmt.Errorf("unknown settings key: %s", key)
	}
	switch spec.Scope {
	case ScopePersonalConfig:
		return m.personal.Set(key, value)
	case ScopeEnv:
		return m.env.Set(key, value)
	}
	return fmt.Errorf("unhandled scope %v for key %q", spec.Scope, key)
}

// List returns every registered key with its currently-resolved value and the
// scope label that determines where writes for that key land. Used by the
// settings UI to render scope-grouped rows.
func (m *Manager) List() []ResolvedKey {
	out := make([]ResolvedKey, 0, len(Registry))
	for _, spec := range Registry {
		v, _ := m.Resolve(spec.Key)
		out = append(out, ResolvedKey{
			Key:    spec.Key,
			Value:  v,
			Scope:  spec.Scope,
			Source: m.sourceOf(spec.Key, spec.Scope),
		})
	}
	return out
}

// ResolvedKey is one row of Manager.List.
type ResolvedKey struct {
	Key    string
	Value  string
	Scope  Scope  // scope that owns writes for this key
	Source Source // where the resolved value actually came from
}

// Source identifies the origin of a resolved value — distinct from the key's
// declared Scope, since a key with no value set resolves from SourceDefault.
type Source int

const (
	SourceDefault Source = iota
	SourcePersonalConfig
	SourceEnv
)

// String returns the label shown in `settings list` output.
func (s Source) String() string {
	switch s {
	case SourceDefault:
		return "default"
	case SourcePersonalConfig:
		return "personal"
	case SourceEnv:
		return "env"
	}
	return "unknown"
}

// sourceOf reports the actual provenance of a resolved value, matching the
// precedence order in Resolve.
func (m *Manager) sourceOf(key string, scope Scope) Source {
	if scope == ScopeEnv {
		if _, ok := m.env.Get(key); ok {
			return SourceEnv
		}
		return SourceDefault
	}
	if v, ok := m.personal.Get(key); ok && v != "" {
		return SourcePersonalConfig
	}
	return SourceDefault
}

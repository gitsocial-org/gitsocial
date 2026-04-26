// registry.go - Per-host forge registry and lookup
package forge

import (
	"strings"
	"sync"
)

var (
	registry   = make(map[string]Forge)
	registryMu sync.RWMutex
)

// Register adds a forge adapter to the registry, keyed by lowercase host.
// Replaces an existing entry for the same host.
//
// SECURITY: registered adapters are trusted to honestly attest commit
// verification. Only first-party adapters should be registered. If user-defined
// adapters are ever exposed (e.g. via config), gate registration on an explicit
// trust list — a malicious adapter can affirm any (key, email) binding for its
// host and pollute downstream UI.
func Register(f Forge) {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry[strings.ToLower(f.Host())] = f
}

// Lookup returns the forge adapter for a host, or nil if none is registered.
func Lookup(host string) Forge {
	registryMu.RLock()
	defer registryMu.RUnlock()
	return registry[strings.ToLower(host)]
}

// LookupForRepo resolves the forge adapter for a repo URL.
func LookupForRepo(repoURL string) (Forge, string, string, error) {
	host, owner, repo, err := ParseRepoURL(repoURL)
	if err != nil {
		return nil, "", "", err
	}
	f := Lookup(host)
	if f == nil {
		return nil, owner, repo, nil
	}
	return f, owner, repo, nil
}

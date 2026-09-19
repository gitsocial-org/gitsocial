// forks.go - Fork registry stored as one ref per fork URL.
//
// Each registered fork lives at refs/gitmsg/core/forks/<urlHash>, where
// urlHash is the first 12 hex chars of SHA-256(identity). The ref's commit
// message is the address, the spelling the user gave, which is what git is
// handed. This layout means concurrent `fork add` calls on different clones
// either land on different refs (no collision) or on the same ref with
// identical content (idempotent push).
package gitmsg

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
)

const forksRefPrefix = "refs/gitmsg/core/forks/"

// ErrForkNotRegistered reports a URL with no ref under refs/gitmsg/core/forks/.
var ErrForkNotRegistered = errors.New("fork not registered")

var legacyForksMigrated sync.Map // workdir → bool

// GetForks returns the identity of every fork registered in the workspace.
// Migrates legacy forks (stored as a JSON array in core config) into the
// per-element ref layout on first call per workdir, then reads from the
// new layout exclusively.
func GetForks(workdir string) []string {
	addresses := forkAddressList(workdir)
	out := make([]string, 0, len(addresses))
	seen := make(map[string]bool, len(addresses))
	for _, address := range addresses {
		if identity := protocol.NormalizeURL(address); identity != "" && !seen[identity] {
			seen[identity] = true
			out = append(out, identity)
		}
	}
	return out
}

// ForkAddresses maps each registered fork's identity to the address it is fetched from.
func ForkAddresses(workdir string) map[string]string {
	addresses := forkAddressList(workdir)
	out := make(map[string]string, len(addresses))
	for _, address := range addresses {
		if identity := protocol.NormalizeURL(address); identity != "" && out[identity] == "" {
			out[identity] = address
		}
	}
	return out
}

// forkAddressList reads every fork ref's address in one `for-each-ref`, the
// subprocess budget hot interactive paths allow.
func forkAddressList(workdir string) []string {
	migrateLegacyForks(workdir)
	result, err := git.ExecGit(workdir, []string{
		"for-each-ref",
		"--format=%(contents:subject)",
		forksRefPrefix,
	})
	if err != nil || result.Stdout == "" {
		return nil
	}
	lines := strings.Split(result.Stdout, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if address := strings.TrimSpace(line); address != "" {
			out = append(out, address)
		}
	}
	return out
}

// AddFork registers a fork by its address, keyed by identity: re-adding any
// spelling of a registered fork is a no-op. Two clones adding the same
// spelling produce identical refs and don't conflict on push.
func AddFork(workdir, forkURL string) error {
	address := strings.TrimSpace(forkURL)
	identity := protocol.NormalizeURL(address)
	if identity == "" {
		return fmt.Errorf("invalid fork URL: %q", forkURL)
	}
	migrateLegacyForks(workdir)
	ref := forkRefPath(identity)
	if _, err := git.ReadRef(workdir, ref); err == nil {
		return nil
	}
	hash, err := git.CreateCommitTree(workdir, address+"\n", "")
	if err != nil {
		return fmt.Errorf("create fork ref commit: %w", err)
	}
	return git.WriteRef(workdir, ref, hash)
}

// AddForks registers multiple fork URLs and returns the count of new
// additions. Existing forks are skipped (idempotent).
func AddForks(workdir string, forkURLs []string) (int, error) {
	added := 0
	for _, u := range forkURLs {
		identity := protocol.NormalizeURL(u)
		if identity == "" {
			continue
		}
		ref := forkRefPath(identity)
		if _, err := git.ReadRef(workdir, ref); err == nil {
			continue
		}
		if err := AddFork(workdir, u); err != nil {
			return added, err
		}
		added++
	}
	return added, nil
}

// RemoveFork removes a fork by deleting every ref whose address is a spelling of its identity.
func RemoveFork(workdir, forkURL string) error {
	identity := protocol.NormalizeURL(forkURL)
	if identity == "" {
		return fmt.Errorf("invalid fork URL: %q", forkURL)
	}
	migrateLegacyForks(workdir)
	refs := forkRefsFor(workdir, identity)
	if len(refs) == 0 {
		return fmt.Errorf("%w: %s", ErrForkNotRegistered, forkURL)
	}
	for _, ref := range refs {
		if err := git.DeleteRef(workdir, ref); err != nil {
			return fmt.Errorf("delete fork ref: %w", err)
		}
	}
	return nil
}

// forkRefsFor returns every fork ref whose address normalizes to the identity, a ref keyed under an older identity rule included.
func forkRefsFor(workdir, identity string) []string {
	result, err := git.ExecGit(workdir, []string{
		"for-each-ref",
		"--format=%(refname) %(contents:subject)",
		forksRefPrefix,
	})
	if err != nil {
		return nil
	}
	var refs []string
	for _, line := range strings.Split(result.Stdout, "\n") {
		ref, address, _ := strings.Cut(strings.TrimSpace(line), " ")
		if ref != "" && protocol.NormalizeURL(strings.TrimSpace(address)) == identity {
			refs = append(refs, ref)
		}
	}
	return refs
}

// forkRefPath returns the per-fork ref name for a fork's identity. Hash
// length (12 hex chars = 48 bits) gives ~16M ref-name space, comfortable
// for thousands of forks per workspace.
func forkRefPath(identity string) string {
	h := sha256.Sum256([]byte(identity))
	return forksRefPrefix + hex.EncodeToString(h[:6])
}

// migrateLegacyForks reads the old config-embedded forks array (if any)
// and converts it to per-element refs, then clears the legacy key. Runs
// at most once per workdir per process; subsequent calls are no-ops via
// the sync.Map gate.
//
// Hot-path constraint: `GetForks` is called from interactive paths
// (e.g., the TUI board view) which the test harness gives a 50ms budget.
// The migration cheap-checks for legacy config refs before parsing — a
// `ReadRef` on a missing ref is one fast `git rev-parse` returning a
// nonzero exit, much cheaper than reading and JSON-parsing the config
// commit. Most workspaces have no legacy forks, so the cheap-check
// short-circuits before any expensive work.
func migrateLegacyForks(workdir string) {
	if _, done := legacyForksMigrated.Load(workdir); done {
		return
	}
	defer legacyForksMigrated.Store(workdir, true)
	for _, ext := range []string{"core", "review"} {
		// Fast path: no legacy config ref exists for this extension.
		if _, err := git.ReadRef(workdir, "refs/gitmsg/"+ext+"/config"); err != nil {
			continue
		}
		config, _ := ReadExtConfig(workdir, ext)
		legacy := getLegacyForksList(configOrEmpty(config))
		if len(legacy) == 0 {
			continue
		}
		for _, url := range legacy {
			address := strings.TrimSpace(url)
			identity := protocol.NormalizeURL(address)
			if identity == "" {
				continue
			}
			ref := forkRefPath(identity)
			if _, err := git.ReadRef(workdir, ref); err == nil {
				continue
			}
			hash, err := git.CreateCommitTree(workdir, address+"\n", "")
			if err != nil {
				continue
			}
			_ = git.WriteRef(workdir, ref, hash)
		}
		if config != nil {
			delete(config, "forks")
			_ = WriteExtConfig(workdir, ext, config)
		}
	}
}

// configOrEmpty returns the config, or an empty map when it is nil.
func configOrEmpty(config map[string]interface{}) map[string]interface{} {
	if config == nil {
		return map[string]interface{}{}
	}
	return config
}

// getLegacyForksList returns the fork URLs a legacy config holds.
func getLegacyForksList(config map[string]interface{}) []string {
	forks, ok := config["forks"].([]interface{})
	if !ok {
		return nil
	}
	result := make([]string, 0, len(forks))
	for _, item := range forks {
		if s, ok := item.(string); ok {
			result = append(result, s)
		}
	}
	return result
}

// forks.go - Fork registry stored as one ref per fork URL.
//
// Each registered fork lives at refs/gitmsg/core/forks/<urlHash>, where
// urlHash is the first 12 hex chars of SHA-256(normalizedURL). The ref's
// commit message is the normalized URL. This layout means concurrent
// `fork add` calls on different clones either land on different refs (no
// collision) or on the same ref with identical content (idempotent push).
// Eliminates the silent-data-loss path where a single shared config ref
// rejected the second push and dropped the user's edit.
package gitmsg

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"

	"github.com/gitsocial-org/gitsocial/core/git"
	"github.com/gitsocial-org/gitsocial/core/protocol"
)

const forksRefPrefix = "refs/gitmsg/core/forks/"

var legacyForksMigrated sync.Map // workdir → bool

// GetForks returns the list of registered fork URLs for the workspace.
// Migrates legacy forks (stored as a JSON array in core config) into the
// per-element ref layout on first call per workdir, then reads from the
// new layout exclusively.
func GetForks(workdir string) []string {
	migrateLegacyForks(workdir)
	refs, err := git.ListRefs(workdir, "core/forks/")
	if err != nil || len(refs) == 0 {
		return nil
	}
	out := make([]string, 0, len(refs))
	for _, ref := range refs {
		hash, err := git.ReadRef(workdir, "refs/gitmsg/"+ref)
		if err != nil {
			continue
		}
		msg, err := git.GetCommitMessage(workdir, hash)
		if err != nil {
			continue
		}
		url := strings.TrimSpace(msg)
		if url != "" {
			out = append(out, url)
		}
	}
	return out
}

// AddFork registers a fork URL. Idempotent: re-adding an existing fork
// is a no-op. Two clones adding the same URL produce identical refs and
// don't conflict on push.
func AddFork(workdir, forkURL string) error {
	normalized := protocol.NormalizeURL(forkURL)
	if normalized == "" {
		return fmt.Errorf("invalid fork URL: %q", forkURL)
	}
	migrateLegacyForks(workdir)
	ref := forkRefPath(normalized)
	if _, err := git.ReadRef(workdir, ref); err == nil {
		return nil
	}
	hash, err := git.CreateCommitTree(workdir, normalized+"\n", "")
	if err != nil {
		return fmt.Errorf("create fork ref commit: %w", err)
	}
	return git.WriteRef(workdir, ref, hash)
}

// AddForks registers multiple fork URLs and returns the count of new
// additions. Existing forks are skipped silently (idempotent).
func AddForks(workdir string, forkURLs []string) (int, error) {
	added := 0
	for _, u := range forkURLs {
		normalized := protocol.NormalizeURL(u)
		if normalized == "" {
			continue
		}
		ref := forkRefPath(normalized)
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

// RemoveFork removes a fork URL by deleting its ref. Idempotent: removing
// a fork that's not registered is a no-op.
func RemoveFork(workdir, forkURL string) error {
	normalized := protocol.NormalizeURL(forkURL)
	if normalized == "" {
		return nil
	}
	migrateLegacyForks(workdir)
	ref := forkRefPath(normalized)
	if _, err := git.ReadRef(workdir, ref); err != nil {
		return nil
	}
	return git.DeleteRef(workdir, ref)
}

// forkRefPath returns the per-fork ref name for a normalized URL. Hash
// length (12 hex chars = 48 bits) gives ~16M ref-name space, comfortable
// for thousands of forks per workspace.
func forkRefPath(normalizedURL string) string {
	h := sha256.Sum256([]byte(normalizedURL))
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
			normalized := protocol.NormalizeURL(url)
			if normalized == "" {
				continue
			}
			ref := forkRefPath(normalized)
			if _, err := git.ReadRef(workdir, ref); err == nil {
				continue
			}
			hash, err := git.CreateCommitTree(workdir, normalized+"\n", "")
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

func configOrEmpty(config map[string]interface{}) map[string]interface{} {
	if config == nil {
		return map[string]interface{}{}
	}
	return config
}

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

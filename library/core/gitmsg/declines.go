// declines.go - Published decline markers, one ref per declined proposal at
// refs/gitmsg/core/declines/<hash> (subject = the proposal's ref). The marker
// rides the bulk refs/gitmsg/* push, so on fetch a proposer learns the owner
// declined its cross-repo proposal and the owner's choice survives a re-clone
// (the decline is otherwise local). Accept takes precedence (GITMSG.md §1.5);
// acceptance itself needs no marker, since the owner's mirror edit carries it.
package gitmsg

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/gitsocial-org/gitsocial/library/core/git"
)

// DeclinesRefPrefix is the ref namespace for published decline markers.
const DeclinesRefPrefix = "refs/gitmsg/core/declines/"

// AddDecline publishes that proposalRef has been declined by this workspace.
// Idempotent: re-publishing the same proposal is a no-op.
func AddDecline(workdir, proposalRef string) error {
	if proposalRef == "" {
		return fmt.Errorf("empty proposal ref")
	}
	ref := declineRefPath(proposalRef)
	if _, err := git.ReadRef(workdir, ref); err == nil {
		return nil
	}
	hash, err := git.CreateCommitTree(workdir, proposalRef+"\n", "")
	if err != nil {
		return fmt.Errorf("create decline ref commit: %w", err)
	}
	return git.WriteRef(workdir, ref, hash)
}

// declineRefPath returns the per-proposal marker ref for a proposal ref.
func declineRefPath(proposalRef string) string {
	h := sha256.Sum256([]byte(proposalRef))
	return DeclinesRefPrefix + hex.EncodeToString(h[:6])
}

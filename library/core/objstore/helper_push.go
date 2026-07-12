// helper_push.go - push side of the s3:// remote helper
//
// Objects are uploaded loose (not packed): it keeps the bucket uniform with
// the read path, uploads stay verbatim content-addressed copies, and gitmsg
// pushes are small. Pack strategy is future work alongside repack.
//
// Git invocations here use os/exec directly rather than core/git: the helper
// runs as a child of git with GIT_DIR in its environment (no worktree path to
// hand to ExecGit), and keeping objstore free of a core/git dependency leaves
// core/git free to reference objstore for helper setup without a cycle.
package objstore

import (
	"bufio"
	"bytes"
	"compress/zlib"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// pushCommand is one parsed "push [+]<src>:<dst>" line.
type pushCommand struct {
	src    string // empty = delete dst
	dst    string
	forced bool
}

// parsePushCommand parses a remote-helper push line.
func parsePushCommand(line string) (pushCommand, error) {
	spec, ok := strings.CutPrefix(line, "push ")
	if !ok {
		return pushCommand{}, fmt.Errorf("malformed push command: %q", line)
	}
	var cmd pushCommand
	spec, cmd.forced = strings.CutPrefix(spec, "+")
	src, dst, ok := strings.Cut(spec, ":")
	if !ok || dst == "" {
		return pushCommand{}, fmt.Errorf("malformed push refspec: %q", spec)
	}
	cmd.src, cmd.dst = src, dst
	return cmd, nil
}

// push executes a push batch: upload missing objects, then write/delete the
// refs, reporting per-ref status lines followed by a blank line.
func (h *remoteHelper) push(batch []string, w io.Writer) error {
	cmds := make([]pushCommand, 0, len(batch))
	for _, line := range batch {
		cmd, err := parsePushCommand(line)
		if err != nil {
			return err
		}
		cmds = append(cmds, cmd)
	}

	failAll := func(err error) {
		for _, cmd := range cmds {
			fmt.Fprintf(w, "error %s %s\n", cmd.dst, oneLine(err))
		}
		fmt.Fprint(w, "\n")
	}
	// Resolve how this bucket stores refs before any write — a bucket that
	// can't CAS at all is rejected loudly rather than racing silently.
	if err := h.resolveRefMode(); err != nil {
		failAll(err)
		return nil
	}
	var srcs []string
	for _, cmd := range cmds {
		if cmd.src != "" {
			srcs = append(srcs, cmd.src)
		}
	}
	if len(srcs) > 0 {
		if err := h.uploadMissingObjects(srcs); err != nil {
			// Object transfer failed: no ref moved; fail every dst.
			failAll(err)
			return nil
		}
	}

	branchPushed := ""
	refsMoved := false
	extPushed := map[string]string{} // ext -> new tip ("" = branch deleted)
	for _, cmd := range cmds {
		sha, err := h.applyRefUpdate(cmd)
		if err != nil {
			fmt.Fprintf(w, "error %s %s\n", cmd.dst, oneLine(err))
			continue
		}
		fmt.Fprintf(w, "ok %s\n", cmd.dst)
		refsMoved = true
		if h.remoteRefs != nil {
			if cmd.src == "" {
				delete(h.remoteRefs, cmd.dst)
			} else {
				h.remoteRefs[cmd.dst] = sha
			}
		}
		if ext := siteItemsExt(cmd.dst); ext != "" {
			extPushed[ext] = sha
		}
		if cmd.src != "" && strings.HasPrefix(cmd.dst, "refs/heads/") {
			if branchPushed == "" || cmd.dst == "refs/heads/main" {
				branchPushed = cmd.dst
			}
		}
	}
	// Leases apply to this batch only; a later batch gets fresh option cas lines.
	h.leases = nil
	// Report is complete: emit the terminating blank line and flush it out to git
	// NOW, before any post-push bucket maintenance. The gitremote-helpers(7)
	// protocol has git block reading our stdout for the per-ref status + blank
	// line, so anything slow between the ref writes and this flush leaves git idle
	// waiting on a report that already landed — on a large multi-ref push over a
	// high-latency bucket the maintenance below runs long enough to look like a
	// hang (git and the helper both at 0% CPU), and refs that already updated
	// would appear to fail. Maintenance must therefore run strictly after git has
	// its report; it is best-effort and its outcome never affects the push.
	fmt.Fprint(w, "\n")
	if f, ok := w.(interface{ Flush() error }); ok {
		if err := f.Flush(); err != nil {
			return err
		}
	}
	h.postPushMaintenance(branchPushed, refsMoved, extPushed)
	return nil
}

// postPushMaintenance runs the best-effort bucket upkeep that follows a
// successful push — advertising the default branch as HEAD and refreshing the
// static read surface's artifacts. It runs only AFTER the push report has been
// flushed to git (see push): none of this is part of git's ref-update contract,
// so a slow or failed maintenance pass must never delay or fail the push itself.
func (h *remoteHelper) postPushMaintenance(branchPushed string, refsMoved bool, extPushed map[string]string) {
	// Advertise the repo's real default branch (its local HEAD symref) as the
	// bucket HEAD — never assume "main". Fall back to a pushed branch only when
	// HEAD can't be read (detached, or a non-repo caller).
	head := localDefaultBranchRef()
	if head == "" {
		head = branchPushed
	}
	if head != "" {
		h.ensureRemoteHEAD(head)
	}
	if !refsMoved {
		return
	}
	// Site artifacts (refs manifest + per-extension item/body corpora) serve
	// ONLY the static read surface, so a plain s3:// git remote with no site
	// stays clean: gate them on the same site-enabled probe the shell refresh
	// uses. Best-effort throughout — a probe failure only skips this push's
	// maintenance, never the git push itself.
	enabled, _, err := siteEnabled(h.client, h.prefix)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gitsocial s3: site probe: %v\n", err)
		return
	}
	if !enabled {
		return
	}
	// The refs-derived artifacts (refs manifest, pm/site config, prism grammars)
	// depend ONLY on the refs/ listing + HEAD, so skip re-deriving them when a
	// prior pass already covered this exact ref state at this shell version —
	// detected in ~2-3 round trips. The helper just moved refs, so its OWN push
	// almost always changes the digest and runs the full block; the win is a
	// concurrent/duplicate push whose ref state another pusher already handled.
	// The per-branch site-items append below is NOT gated on the marker: those
	// branches just moved, and updateSiteItemsIndex already does its own cheap
	// tip comparison, so the marker must never mask that append.
	shellVersion, verErr := siteVersion()
	upToDate, digest := false, ""
	if verErr == nil {
		upToDate, digest = siteMaintenanceUpToDate(h.client, h.prefix, shellVersion)
	}
	if !upToDate {
		h.writeSiteManifest()
		h.writeSitePMConfig()
		h.writeSiteCustomization()
	}
	h.updateSiteItems(extPushed)
	// Site self-refresh: buckets carrying the static read surface pick up
	// this binary's embedded version on any push. Best-effort, like the
	// manifest: a failure only leaves the site stale until the next push.
	if err := refreshSiteIfEnabled(h.client, h.prefix); err != nil {
		fmt.Fprintf(os.Stderr, "gitsocial s3: site refresh: %v\n", err)
	}
	// Stamp the marker after the refs-derived writes so a later push against this
	// same ref state can skip them. Best-effort: a wrong/missing marker only costs
	// extra work next time, never a wrong skip. Skipped when we didn't run the
	// block (upToDate), couldn't trust the digest (""), or a bootstrap is still
	// backfilling — same rule as pushSite: a stamped marker would make the next
	// site pass skip the backfill that no ref move signals.
	if !upToDate {
		refs := h.remoteRefs
		if refs == nil {
			refs, _ = readRemoteRefs(h.client, h.prefix)
		}
		if refs != nil && !siteItemsBootstrapPending(h.client, h.prefix, refs) {
			writeSitePushState(h.client, h.prefix, shellVersion, digest)
		}
	}
}

// writeSiteManifest publishes refname → sha as the site refs manifest after
// every push, so the static read surface discovers refs without bucket
// listing (public domains don't expose it) and resolves generation-mode refs.
// Best-effort: a stale manifest only degrades the site until the next push.
func (h *remoteHelper) writeSiteManifest() {
	refs := h.remoteRefs
	if refs == nil {
		var err error
		if refs, err = readRemoteRefs(h.client, h.prefix); err != nil {
			fmt.Fprintf(os.Stderr, "gitsocial s3: site manifest: %v\n", err)
			return
		}
	}
	if err := putSiteManifest(h.client, h.prefix, refs); err != nil {
		fmt.Fprintf(os.Stderr, "gitsocial s3: site manifest: %v\n", err)
	}
}

// writeSitePMConfig publishes the resolved PM board config after every push, so
// the static site's board honors the repo's refs/gitmsg/pm/config. Best-effort,
// same contract as the manifest: a failure only leaves the board on the kanban
// default until the next push.
func (h *remoteHelper) writeSitePMConfig() {
	refs := h.remoteRefs
	if refs == nil {
		var err error
		if refs, err = readRemoteRefs(h.client, h.prefix); err != nil {
			fmt.Fprintf(os.Stderr, "gitsocial s3: site pm config: %v\n", err)
			return
		}
	}
	if err := writeSitePMConfig(h.client, h.prefix, refs); err != nil {
		fmt.Fprintf(os.Stderr, "gitsocial s3: site pm config: %v\n", err)
	}
}

// writeSiteCustomization publishes the validated site customization after every
// push, so the static site honors the repo's refs/gitmsg/core/config `site`
// sub-object. Best-effort, same contract as the manifest: a failure only leaves
// the site on its built-in defaults until the next push.
func (h *remoteHelper) writeSiteCustomization() {
	refs := h.remoteRefs
	if refs == nil {
		var err error
		if refs, err = readRemoteRefs(h.client, h.prefix); err != nil {
			fmt.Fprintf(os.Stderr, "gitsocial s3: site customization: %v\n", err)
			return
		}
	}
	if err := writeSiteCustomization(h.client, h.prefix, refs); err != nil {
		fmt.Fprintf(os.Stderr, "gitsocial s3: site customization: %v\n", err)
	}
}

// updateSiteItems maintains the per-extension site artifacts (metadata index +
// search corpus) for every pushed gitmsg data branch, plus the single code items
// index across every pushed code branch, alongside the refs manifest. Best-effort,
// same contract: a stale artifact only degrades the site until the next push. An
// error here can now only be transient (a network / bucket failure): the artifact
// state is always repairable on the next push (the repair state machine rebuilds
// any mismatch from the immutable sealed shards + a bounded walk), so a failed
// maintenance pass never wedges the index.
func (h *remoteHelper) updateSiteItems(extPushed map[string]string) {
	// The helper runs as a git child with GIT_DIR set, so every commit the walk
	// visits (an ancestor of a just-pushed tip) is present in the local odb —
	// read it there instead of a per-commit bucket GET. One source serves the
	// whole pass across extensions and the code corpus.
	src := newLocalCommitSource(h.gitDir, "")
	defer src.close()
	for ext, sha := range extPushed {
		var err error
		if sha == "" {
			err = deleteSiteArtifacts(h.client, h.prefix, ext)
		} else {
			err = updateSiteItemsIndex(h.client, h.prefix, ext, sha, &siteProgress{progress: h.progress, ext: ext, src: src})
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "gitsocial s3: items index %s: %v\n", ext, err)
		}
	}
	h.updateSiteCodeItems(src)
}

// updateSiteCodeItems maintains the single code items index across every pushed
// code branch. Unlike the per-extension indexes it is keyed on ALL current code
// tips (a synthetic digest tip), so any code-branch push runs it and a push that
// touched only ext branches sees a NO-OP after a cheap manifest read. The current
// code tips and the default branch come from the bucket's refs (the authoritative
// post-push state) and the pushing repo's HEAD. Best-effort, same contract.
func (h *remoteHelper) updateSiteCodeItems(src *localCommitSource) {
	refs := h.remoteRefs
	if refs == nil {
		var err error
		if refs, err = readRemoteRefs(h.client, h.prefix); err != nil {
			fmt.Fprintf(os.Stderr, "gitsocial s3: code index refs: %v\n", err)
			return
		}
	}
	defaultBranch := strings.TrimPrefix(localDefaultBranchRef(), "refs/heads/")
	tips := codeBranchTips(refs, defaultBranch)
	sp := &siteProgress{progress: h.progress, ext: siteCodeExt, src: src}
	if err := updateSiteCodeIndex(h.client, h.prefix, tips, defaultBranch, sp); err != nil {
		fmt.Fprintf(os.Stderr, "gitsocial s3: code index: %v\n", err)
	}
}

// maxCASRetries bounds the read-check-write loop; contention on GitSocial's
// per-element refs is rare, so hitting this means something is spinning.
const maxCASRetries = 5

// zeroOID is git's null object id (in a cas lease: the ref must not exist).
const zeroOID = "0000000000000000000000000000000000000000"

// checkLease enforces a --force-with-lease expectation (recorded by `option
// cas`, see helper.go) at write time: the remote's current value ("" = absent)
// must equal the expected one ("" = must not exist). A match authorizes the
// update even when it is not a fast-forward; a mismatch rejects with git's
// conventional "stale info" phrasing so porcelain output reads like a native
// lease failure. Both ref-mode CAS loops re-check on every attempt, so a
// concurrent pusher landing between read and write is caught on the retry's
// re-read, never slipped past. leased=false means no lease applies and the
// normal fast-forward rules decide.
func (h *remoteHelper) checkLease(dst, current string) (leased bool, err error) {
	expected, ok := h.leases[dst]
	if !ok {
		return false, nil
	}
	if current != expected {
		return true, fmt.Errorf("stale info")
	}
	return true, nil
}

// applyRefUpdate writes or deletes one ref, dispatching on the bucket's ref
// mode, and returns the written sha ("" for a deletion). The stored value is
// the object src names — for an annotated tag that is the tag object itself,
// exactly as git stores it.
func (h *remoteHelper) applyRefUpdate(cmd pushCommand) (string, error) {
	if h.refMode == refModeGeneration {
		return h.applyRefUpdateGeneration(cmd)
	}
	return h.applyRefUpdateETag(cmd)
}

// applyRefUpdateETag writes or deletes one plain ref key with ETag
// compare-and-swap: read current value + ETag, verify the update is allowed
// (fast-forward unless forced), write with If-Match / If-None-Match: *, and
// re-read on precondition failure. This is git's "old value must match"
// ref-update contract expressed in S3.
func (h *remoteHelper) applyRefUpdateETag(cmd pushCommand) (string, error) {
	key := h.prefix + cmd.dst
	if cmd.src == "" {
		// Deletion stays unconditional: S3 has no conditional DELETE, and git
		// itself only guards deletes client-side.
		return "", h.client.Delete(key)
	}
	sha, err := gitOutput("rev-parse", "--verify", cmd.src)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", cmd.src, err)
	}
	value := []byte(sha + "\n")
	var lastErr error
	for attempt := 0; attempt < maxCASRetries; attempt++ {
		currentRaw, etag, err := h.client.GetWithETag(key)
		switch {
		case errors.Is(err, ErrNotFound):
			if _, leaseErr := h.checkLease(cmd.dst, ""); leaseErr != nil {
				return "", leaseErr
			}
			err = h.client.PutIfAbsent(key, value)
		case err != nil:
			return "", fmt.Errorf("read ref %s: %w", cmd.dst, err)
		default:
			current := strings.TrimSpace(string(currentRaw))
			if current == sha {
				return sha, nil // already up to date
			}
			leased, leaseErr := h.checkLease(cmd.dst, current)
			if leaseErr != nil {
				return "", leaseErr
			}
			if !leased && !cmd.forced {
				if err := checkFastForward(current, sha, cmd.dst); err != nil {
					return "", err
				}
			}
			err = h.client.PutIfMatch(key, value, etag)
		}
		if errors.Is(err, ErrPreconditionFailed) {
			lastErr = err
			continue // ref moved underneath us — re-read and re-verify
		}
		if err != nil {
			return "", err
		}
		return sha, nil
	}
	return "", fmt.Errorf("ref %s: too much contention (gave up after %d CAS attempts): %w", cmd.dst, maxCASRetries, lastErr)
}

// applyRefUpdateGeneration writes or deletes one ref as a generation chain:
// every update atomically creates the next generation key with
// If-None-Match: * (the only CAS create-only providers enforce), the highest
// generation is the current value, and superseded generations are cleaned up
// after a successful write.
func (h *remoteHelper) applyRefUpdateGeneration(cmd pushCommand) (string, error) {
	if cmd.src == "" {
		return "", h.deleteRefGenerations(cmd.dst)
	}
	sha, err := gitOutput("rev-parse", "--verify", cmd.src)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", cmd.src, err)
	}
	var lastErr error
	for attempt := 0; attempt < maxCASRetries; attempt++ {
		maxGen, err := maxGeneration(h.client, h.prefix, cmd.dst)
		if err != nil {
			return "", err
		}
		if maxGen > 0 {
			current, err := h.client.Get(genKey(h.prefix, cmd.dst, maxGen))
			if errors.Is(err, ErrNotFound) {
				lastErr = err
				continue // chain advanced and got GC'd underneath us — re-list
			}
			if err != nil {
				return "", fmt.Errorf("read ref %s: %w", cmd.dst, err)
			}
			currentSHA, err := refSHA(cmd.dst, current)
			if err != nil {
				return "", err
			}
			if currentSHA == sha {
				return sha, nil // already up to date
			}
			leased, leaseErr := h.checkLease(cmd.dst, currentSHA)
			if leaseErr != nil {
				return "", leaseErr
			}
			if !leased && !cmd.forced {
				if err := checkFastForward(currentSHA, sha, cmd.dst); err != nil {
					return "", err
				}
			}
		} else if _, leaseErr := h.checkLease(cmd.dst, ""); leaseErr != nil {
			return "", leaseErr
		}
		err = h.client.PutIfAbsent(genKey(h.prefix, cmd.dst, maxGen+1), []byte(sha+"\n"))
		if errors.Is(err, ErrPreconditionFailed) {
			lastErr = err
			continue // another writer took this generation — re-list and re-verify
		}
		if err != nil {
			return "", err
		}
		h.gcGenerations(cmd.dst, maxGen)
		return sha, nil
	}
	return "", fmt.Errorf("ref %s: too much contention (gave up after %d CAS attempts): %w", cmd.dst, maxCASRetries, lastErr)
}

// gcGenerations best-effort deletes generations older than the written one's
// immediate predecessor, so a concurrent reader's list→read window survives
// one more update. Failures only log: the next successful write re-collects.
func (h *remoteHelper) gcGenerations(refName string, previousGen uint64) {
	keys, err := h.client.List(h.prefix + refName + genDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gitsocial s3: gc ref %s: %v\n", refName, err)
		return
	}
	for _, key := range keys {
		_, gen, isGen, err := parseGenKey(strings.TrimPrefix(key, h.prefix))
		if err != nil || !isGen || gen >= previousGen {
			continue
		}
		if err := h.client.Delete(key); err != nil {
			fmt.Fprintf(os.Stderr, "gitsocial s3: gc %s generation %d: %v\n", refName, gen, err)
		}
	}
}

// deleteRefGenerations removes a ref's whole chain, oldest first so readers
// keep resolving the current value until the end. Deletion is unconditional,
// matching the etag mode and git's client-side-only delete guard.
func (h *remoteHelper) deleteRefGenerations(refName string) error {
	keys, err := h.client.List(h.prefix + refName + genDir)
	if err != nil {
		return fmt.Errorf("list ref %s generations: %w", refName, err)
	}
	for _, key := range keys {
		if err := h.client.Delete(key); err != nil {
			return fmt.Errorf("delete ref %s: %w", refName, err)
		}
	}
	return nil
}

// checkFastForward enforces the non-force update rule: the remote's current
// value must be an ancestor of what we push. A current value we don't have
// locally means someone pushed history we haven't fetched.
func checkFastForward(current, next, dst string) error {
	if _, err := gitOutput("cat-file", "-e", current); err != nil {
		return fmt.Errorf("remote %s is at %s which is not known locally; fetch first", dst, current[:12])
	}
	ancestor, err := isAncestor(current, next)
	if err != nil {
		return err
	}
	if !ancestor {
		return fmt.Errorf("non-fast-forward: remote %s is at %s; fetch and merge first, or force-push", dst, current[:12])
	}
	return nil
}

// isAncestor reports whether a is an ancestor of b (exit 1 means "no", not an error).
func isAncestor(a, b string) (bool, error) {
	cmd := exec.Command("git", "merge-base", "--is-ancestor", a, b)
	err := cmd.Run()
	if err == nil {
		return true, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return false, nil
	}
	return false, fmt.Errorf("git merge-base --is-ancestor: %w", err)
}

// refModeKey / casProbeKey live under a dot-prefixed namespace no git ref
// can collide with (refnames can't start with a dot).
const (
	refModeKey  = ".gitsocial/ref-mode"
	casProbeKey = ".gitsocial/cas-probe"
)

// Ref modes: how this bucket stores ref updates.
const (
	refModeETag       = "etag"       // plain keys, If-Match update CAS
	refModeGeneration = "generation" // chain keys, If-None-Match: * create CAS
)

// resolveRefMode determines the bucket's ref mode and caches it in h.refMode.
// The bucket's marker wins over provider capability so every writer stays
// consistent; without a marker the capability (probed when unknown) decides,
// and the winning first pusher records it with a create-CAS write.
func (h *remoteHelper) resolveRefMode() error {
	if h.refMode != "" {
		return nil
	}
	mode, err := h.readRefModeMarker()
	if err != nil {
		return err
	}
	if mode == "" {
		if mode, err = h.refModeFromCapability(); err != nil {
			return err
		}
		if mode, err = h.publishRefMode(mode); err != nil {
			return err
		}
	}
	if mode == refModeETag && h.capability == CapabilityCreateOnly {
		return fmt.Errorf("bucket uses etag ref mode but this provider cannot update refs conditionally (no If-Match support); push from a full-capability provider (aws, r2) or use a fresh prefix")
	}
	h.refMode = mode
	return nil
}

// readRefModeMarker fetches the bucket's recorded ref mode ("" when absent).
func (h *remoteHelper) readRefModeMarker() (string, error) {
	value, err := h.client.Get(h.prefix + refModeKey)
	if errors.Is(err, ErrNotFound) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read ref-mode marker: %w", err)
	}
	mode := strings.TrimSpace(string(value))
	if mode != refModeETag && mode != refModeGeneration {
		return "", fmt.Errorf("unrecognized ref mode %q in bucket marker (written by a newer gitsocial or a foreign tool?)", mode)
	}
	return mode, nil
}

// refModeFromCapability picks the ref mode for a fresh bucket, probing the
// endpoint's conditional-write behavior when no preset declares it.
func (h *remoteHelper) refModeFromCapability() (string, error) {
	capability := h.capability
	if capability == CapabilityUnknown {
		var err error
		if capability, err = h.probeCapability(); err != nil {
			return "", err
		}
	} else if err := h.probeCreateCAS(); err != nil {
		// Declared capabilities still get the cheap create-CAS sanity check:
		// a bucket that ignores conditional headers must be rejected loudly.
		return "", err
	}
	if capability == CapabilityFull {
		return refModeETag, nil
	}
	return refModeGeneration, nil
}

// probeCreateCAS verifies the bucket enforces If-None-Match: * creates:
// create a probe key, then require a duplicate create to fail.
func (h *remoteHelper) probeCreateCAS() error {
	probe := h.prefix + casProbeKey
	// A leftover probe key from a crashed run would fail the first create.
	_ = h.client.Delete(probe)
	if err := h.client.PutIfAbsent(probe, []byte("probe\n")); err != nil {
		return fmt.Errorf("conditional-write probe (create): %w", err)
	}
	defer func() { _ = h.client.Delete(probe) }()
	err := h.client.PutIfAbsent(probe, []byte("probe2\n"))
	if err == nil {
		return fmt.Errorf("bucket does not enforce conditional writes (If-None-Match), so ref updates would race silently; use a provider with conditional-write support (aws, r2, do)")
	}
	if !errors.Is(err, ErrPreconditionFailed) {
		return fmt.Errorf("conditional-write probe: %w", err)
	}
	return nil
}

// probeCapability classifies an unknown endpoint's conditional-write support:
// create-CAS must be enforced (else the bucket is rejected), then If-Match
// overwrite behavior decides full vs create-only.
func (h *remoteHelper) probeCapability() (Capability, error) {
	if err := h.probeCreateCAS(); err != nil {
		return CapabilityUnknown, err
	}
	probe := h.prefix + casProbeKey
	if err := h.client.Put(probe, []byte("probe\n")); err != nil {
		return CapabilityUnknown, fmt.Errorf("conditional-write probe (overwrite): %w", err)
	}
	defer func() { _ = h.client.Delete(probe) }()
	// A deliberately wrong (but well-formed) ETag: success means If-Match is
	// silently ignored, so only create-CAS can be trusted.
	err := h.client.PutIfMatch(probe, []byte("probe3\n"), `"d41d8cd98f00b204e9800998ecf8427e"`)
	if err == nil {
		return CapabilityCreateOnly, nil
	}
	if !errors.Is(err, ErrPreconditionFailed) {
		return CapabilityUnknown, fmt.Errorf("conditional-write probe (overwrite): %w", err)
	}
	_, etag, err := h.client.GetWithETag(probe)
	if err != nil {
		return CapabilityUnknown, fmt.Errorf("conditional-write probe (overwrite): %w", err)
	}
	err = h.client.PutIfMatch(probe, []byte("probe4\n"), etag)
	switch {
	case err == nil:
		return CapabilityFull, nil
	case errors.Is(err, ErrPreconditionFailed):
		// Ceph RGW shape: a matching If-Match still 412s — overwrites unsupported.
		return CapabilityCreateOnly, nil
	default:
		return CapabilityUnknown, fmt.Errorf("conditional-write probe (overwrite): %w", err)
	}
}

// publishRefMode records the bucket's ref mode with a create-CAS write so
// concurrent first pushers converge on a single mode.
func (h *remoteHelper) publishRefMode(mode string) (string, error) {
	err := h.client.PutIfAbsent(h.prefix+refModeKey, []byte(mode+"\n"))
	if errors.Is(err, ErrPreconditionFailed) {
		existing, err := h.readRefModeMarker()
		if err != nil {
			return "", err
		}
		if existing == "" {
			return "", fmt.Errorf("ref-mode marker contention; retry the push")
		}
		return existing, nil
	}
	if err != nil {
		return "", fmt.Errorf("write ref-mode marker: %w", err)
	}
	return mode, nil
}

// ensureRemoteHEAD writes HEAD on first push so later clones get a default branch.
func (h *remoteHelper) ensureRemoteHEAD(branch string) {
	if cur, err := h.client.Get(h.prefix + "HEAD"); err == nil {
		// Keep an existing HEAD unless it points at a gitmsg data branch — never a
		// valid default (a symptom of the earlier first-pushed-branch heuristic).
		if !strings.Contains(string(cur), "refs/heads/gitmsg/") {
			return
		}
	}
	// Best effort: a missing/wrong HEAD only degrades clone ergonomics.
	_ = h.client.Put(h.prefix+"HEAD", []byte("ref: "+branch+"\n"))
}

// localDefaultBranchRef returns the pushing repo's default branch ref (its HEAD
// symref, e.g. "refs/heads/master") so the bucket advertises the real default —
// not an assumed "main". Empty when HEAD is detached or unreadable.
func localDefaultBranchRef() string {
	ref, err := gitOutput("symbolic-ref", "HEAD")
	if err != nil || !strings.HasPrefix(ref, "refs/heads/") {
		return ""
	}
	return ref
}

// uploadMissingObjects uploads every object reachable from srcs that the
// remote doesn't already have, computed as `rev-list --objects <srcs> --not
// <remote tips we have locally>`.
func (h *remoteHelper) uploadMissingObjects(srcs []string) error {
	tips, err := h.remoteTipsPresentLocally()
	if err != nil {
		return err
	}
	args := append([]string{"rev-list", "--objects"}, srcs...)
	if len(tips) > 0 {
		args = append(args, "--not")
		args = append(args, tips...)
	}
	out, err := gitOutput(args...)
	if err != nil {
		return fmt.Errorf("rev-list: %w", err)
	}
	seen := map[string]bool{}
	var shas []string
	add := func(sha string) {
		if !seen[sha] {
			seen[sha] = true
			shas = append(shas, sha)
		}
	}
	// rev-list --objects peels annotated tags; upload tag objects explicitly.
	for _, src := range srcs {
		sha, err := gitOutput("rev-parse", "--verify", src)
		if err != nil {
			return fmt.Errorf("resolve %s: %w", src, err)
		}
		if objType, err := gitOutput("cat-file", "-t", sha); err == nil && objType == "tag" {
			add(sha)
		}
	}
	for _, line := range strings.Split(out, "\n") {
		if len(line) >= 40 {
			add(line[:40])
		}
	}
	return h.uploadObjects(shas)
}

// remoteTipsPresentLocally resolves the remote's ref values and keeps those
// whose objects exist locally — safe negative ends for the rev-list frontier.
func (h *remoteHelper) remoteTipsPresentLocally() ([]string, error) {
	refs, err := readRemoteRefs(h.client, h.prefix)
	if err != nil {
		return nil, err
	}
	var tips []string
	for _, sha := range refs {
		if _, err := gitOutput("cat-file", "-e", sha); err == nil {
			tips = append(tips, sha)
		}
	}
	return tips, nil
}

// encodedObject is one loose object ready to upload: its sha and zlib bytes.
type encodedObject struct {
	sha        string
	compressed []byte
}

// listResumeThreshold is the git-computed delta size at or above which
// uploadObjects first LISTs the bucket's objects/ prefix to skip objects already
// present (an interrupted initial push then resumes where it stopped instead of
// re-PUTting everything). The LIST costs ~1 round trip per 1,000 keys, so it only
// pays off once the delta is large: below this, the handful of redundant PUTs an
// interrupted small push would repeat is cheaper than the extra listing. 2,000 is
// ~2 LIST round trips against the delta's thousands of PUTs — a rounding error on
// a large push, pure overhead on a small one.
const listResumeThreshold = 2000

// filterPresentObjects removes shas already on the bucket from a large upload
// delta, so an interrupted initial push resumes instead of re-PUTting every
// object. It only LISTs (and only pays that cost) when the delta is at least
// listResumeThreshold; below that it returns the input unchanged. Keys are
// content-addressed, so a present key IS the finished object — no ETag/size
// comparison is needed. A LIST error is non-fatal: fall back to uploading the
// full delta (the PUTs are idempotent), never fail the push over the
// optimization.
func filterPresentObjects(client *Client, prefix string, shas []string) []string {
	if len(shas) < listResumeThreshold {
		return shas
	}
	objs, err := client.ListWithETags(prefix + "objects/")
	if err != nil {
		fmt.Fprintf(os.Stderr, "gitsocial s3: list objects for resume: %v\n", err)
		return shas
	}
	present := make(map[string]bool, len(objs))
	for _, o := range objs {
		// objects/<xx>/<38-hex> -> reassemble the 40-hex sha.
		rel := strings.TrimPrefix(o.Key, prefix+"objects/")
		rel = strings.Replace(rel, "/", "", 1)
		if len(rel) == 40 {
			present[rel] = true
		}
	}
	kept := shas[:0:0]
	for _, sha := range shas {
		if !present[sha] {
			kept = append(kept, sha)
		}
	}
	return kept
}

// uploadObjects streams objects out of the local odb via `git cat-file
// --batch`, re-encodes each as a loose object, and uploads them through a
// bounded worker pool. Content addressing makes each object immutable and
// re-uploads idempotent, so upload order is free: no ordering constraint holds
// across objects, and the ref-update phase runs only after this returns nil.
// Uploads are one HTTP round trip each, so serial transfer is pure round-trip
// latency; the pool overlaps that latency across resolveUploadConcurrency
// workers. The cat-file read stays sequential (one git process) and feeds the
// pool as a producer.
func (h *remoteHelper) uploadObjects(shas []string) error {
	if len(shas) == 0 {
		return nil
	}
	// On a large delta, drop objects already on the bucket so an interrupted
	// initial push resumes instead of re-uploading everything (below the
	// threshold this is a no-op — the LIST would cost more than it saves).
	shas = filterPresentObjects(h.client, h.prefix, shas)
	if len(shas) == 0 {
		return nil
	}
	cmd := exec.Command("git", "cat-file", "--batch")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("git cat-file --batch: %w", err)
	}
	defer func() {
		stdin.Close()
		_ = cmd.Wait()
	}()

	produce := func(ctx context.Context, out chan<- encodedObject) error {
		reader := bufio.NewReaderSize(stdout, 1<<20)
		for _, sha := range shas {
			if _, err := io.WriteString(stdin, sha+"\n"); err != nil {
				return err
			}
			header, err := reader.ReadString('\n')
			if err != nil {
				return fmt.Errorf("cat-file %s: %w", sha, err)
			}
			fields := strings.Fields(strings.TrimSpace(header))
			if len(fields) != 3 || fields[1] == "missing" {
				return fmt.Errorf("cat-file %s: unexpected response %q", sha, strings.TrimSpace(header))
			}
			objType := fields[1]
			var size int64
			if _, err := fmt.Sscanf(fields[2], "%d", &size); err != nil {
				return fmt.Errorf("cat-file %s: bad size in %q", sha, header)
			}
			content := make([]byte, size)
			if _, err := io.ReadFull(reader, content); err != nil {
				return fmt.Errorf("cat-file %s: read content: %w", sha, err)
			}
			if _, err := reader.Discard(1); err != nil { // trailing newline
				return fmt.Errorf("cat-file %s: %w", sha, err)
			}
			compressed, err := encodeLooseObject(objType, content)
			if err != nil {
				return err
			}
			h.fetched[sha] = true
			select {
			case out <- encodedObject{sha: sha, compressed: compressed}:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	}
	return uploadEncodedObjects(h.client, h.prefix, resolveUploadConcurrency(), len(shas), h.progress, produce)
}

// uploadEncodedObjects runs a bounded worker pool that PUTs each object the
// producer emits. The first hard error (from the producer or any worker)
// cancels the context so peers stop promptly and the producer unblocks, then
// the wrapped error is returned. Objects are content-addressed and immutable,
// so worker order is irrelevant; refs move only after this returns nil.
//
// total is the object count (for progress); progress (nil = silent) is called
// as each object lands, throttled by the caller-provided hook.
func uploadEncodedObjects(client *Client, prefix string, concurrency, total int, progress Progress, produce func(context.Context, chan<- encodedObject) error) error {
	if concurrency < 1 {
		concurrency = 1
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	work := make(chan encodedObject)
	var firstErr error
	var errMu sync.Mutex
	setErr := func(err error) {
		errMu.Lock()
		if firstErr == nil {
			firstErr = err
			cancel() // stop peers and unblock the producer
		}
		errMu.Unlock()
	}

	var done int64
	var wg sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for obj := range work {
				key := prefix + "objects/" + obj.sha[:2] + "/" + obj.sha[2:]
				if err := putObjectWithRetry(ctx, client, key, obj.compressed); err != nil {
					setErr(fmt.Errorf("upload object %s: %w", obj.sha, err))
					continue
				}
				progress.call("objects", int(atomic.AddInt64(&done, 1)), total)
			}
		}()
	}

	if err := produce(ctx, work); err != nil {
		setErr(err)
	}
	close(work)
	wg.Wait()
	return firstErr
}

// putObjectWithRetry retries a failed object PUT so a transient fault (a
// killed connection, a throttle) costs one retried object instead of failing
// the whole push — objects are content-addressed, so re-PUTs are idempotent.
// Persistent failures still surface after the attempts run out, and the pool
// context aborts the wait when a peer has already failed the push. It shares
// retryBackoff (client.go) with the read retries.
func putObjectWithRetry(ctx context.Context, client *Client, key string, body []byte) error {
	var err error
	for attempt := 0; ; attempt++ {
		if err = client.Put(key, body); err == nil || attempt >= len(retryBackoff) {
			return err
		}
		select {
		case <-ctx.Done():
			return err
		case <-time.After(retryBackoff[attempt]):
		}
	}
}

// encodeLooseObject builds git's loose-object format: zlib("<type> <size>\0" + content).
func encodeLooseObject(objType string, content []byte) ([]byte, error) {
	var buf bytes.Buffer
	zw := zlib.NewWriter(&buf)
	if _, err := fmt.Fprintf(zw, "%s %d\x00", objType, len(content)); err != nil {
		return nil, err
	}
	if _, err := zw.Write(content); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// gitOutput runs a git command (repo located via the GIT_DIR env git gave the
// helper) and returns trimmed stdout.
func gitOutput(args ...string) (string, error) {
	out, err := exec.Command("git", args...).Output()
	if err != nil {
		var stderr string
		if exitErr, ok := err.(*exec.ExitError); ok {
			stderr = strings.TrimSpace(string(exitErr.Stderr))
		}
		return "", fmt.Errorf("git %s: %v %s", strings.Join(args, " "), err, stderr)
	}
	return strings.TrimSpace(string(out)), nil
}

// oneLine collapses an error message to a single line for status reporting.
func oneLine(err error) string {
	return strings.ReplaceAll(err.Error(), "\n", " ")
}

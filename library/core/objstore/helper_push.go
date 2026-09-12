// helper_push.go - push side of the s3:// remote helper: the object transfer, the ref CAS and the post-push maintenance
//
// Git runs here through os/exec rather than core/git: the helper is a child of
// git with GIT_DIR set, and objstore stays free of a core/git import.
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

	"github.com/gitsocial-org/gitsocial/library/core/git"
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
	// No credentials, so every write below would be refused one object at a time.
	if h.client.Anonymous() {
		failAll(ErrCredentialsRequired)
		return nil
	}
	// Resolve the bucket's ref mode before any write, so a bucket that cannot CAS is rejected up front.
	if err := h.resolveRefMode(); err != nil {
		failAll(err)
		return nil
	}
	if err := h.uploadMissingObjects(cmds); err != nil {
		// Object transfer failed: no ref moved; fail every dst.
		failAll(err)
		return nil
	}

	branchPushed := ""
	updates := map[string]string{}   // dst -> new sha ("" = deleted)
	extPushed := map[string]string{} // ext -> new tip ("" = branch deleted)
	// No two commands in a batch share a write target, so the CAS contract stays per-ref while the round trips overlap; the report keeps command order.
	shas := make([]string, len(cmds))
	errs := runParallel(len(cmds), func(i int) error {
		sha, err := h.applyRefUpdate(cmds[i])
		shas[i] = sha
		return err
	})
	for i, cmd := range cmds {
		if errs[i] != nil {
			fmt.Fprintf(w, "error %s %s\n", cmd.dst, oneLine(errs[i]))
			continue
		}
		sha := shas[i]
		fmt.Fprintf(w, "ok %s\n", cmd.dst)
		updates[cmd.dst] = sha
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
	// Flush the report before any maintenance: git blocks reading it, so work ahead of this line reads as a hang.
	fmt.Fprint(w, "\n")
	if f, ok := w.(interface{ Flush() error }); ok {
		if err := f.Flush(); err != nil {
			return err
		}
	}
	h.postPushMaintenance(branchPushed, updates, extPushed)
	return nil
}

// postPushMaintenance runs the best-effort bucket upkeep after a push, strictly after the report is flushed to git.
func (h *remoteHelper) postPushMaintenance(branchPushed string, updates, extPushed map[string]string) {
	// The manifest follows every ref-moving transfer, deferred or not, since a later one may move nothing.
	refsMoved := len(updates) > 0
	manifestOK := true
	if refsMoved {
		if err := h.publishRefManifest(updates); err != nil {
			fmt.Fprintf(os.Stderr, "gitsocial s3: ref manifest: %v\n", err)
			manifestOK = false
		}
	}
	// One gitsocial push is several git pushes; the caller defers all but the last, which runs the pass once against the final state.
	if os.Getenv(git.DeferMaintenanceEnv) == "1" {
		return
	}
	// Advertise the repo's own HEAD symref as the bucket HEAD, falling back to a pushed branch when it cannot be read.
	head := localDefaultBranchRef()
	if head == "" {
		head = branchPushed
	}
	if head != "" {
		h.ensureRemoteHEAD(head)
	}
	// A thin push records the frontier it excluded against, so readers can resolve the missing objects.
	thin, upstreamURL := h.thinPush()
	if thin {
		h.publishThinUpstream(upstreamURL)
	}
	if !refsMoved {
		return
	}
	// Every step of the pass reads this one view of the refs the bucket now carries.
	if h.remoteRefs == nil {
		refs, err := readRemoteRefs(h.client, h.prefix)
		if err != nil {
			fmt.Fprintf(os.Stderr, "gitsocial s3: post-push maintenance: %v\n", err)
			return
		}
		h.remoteRefs = refs
	}
	refs := h.remoteRefs
	// One local commit source serves the whole pass; the helper runs as a git child, so the pushed objects are already here.
	src := newLocalCommitSource(h.gitDir, "")
	defer src.close()
	// Refresh the dumb-HTTP surface on every ref-moving push, ahead of the site gate, so stock git keeps cloning.
	h.progress.call("maintenance: ref advertisement", 0, 0)
	logDumbTransportInfo(h.client, h.prefix, src, refs, thin)
	// With the bucket's refs known, a HEAD pointing at a ref it does not carry can be repaired.
	h.progress.call("maintenance: HEAD", 0, 0)
	h.repairDanglingHEAD(refs, head, branchPushed)
	// Sealing packs loose history and, after a grace period, deletes the loose copies.
	h.progress.call("maintenance: packs", 0, 0)
	h.maintainPacks(refs)
	// A thin bucket publishes no site, since the site reads a history it does not carry.
	if thin {
		if enabled, _, probeErr := siteEnabled(h.client, h.prefix); probeErr == nil && enabled {
			fmt.Fprintf(os.Stderr, "gitsocial s3: thin fork bucket; its existing site is no longer maintained (detach with `gitsocial push --full`)\n")
		}
		return
	}
	// The pushed site.publish guard is the only enabler, so a plain s3:// remote stays clean.
	h.progress.call("maintenance: site artifacts", 0, 0)
	cfg, cfgOK, err := readSiteCustomization(h.client, h.prefix, refs, h.override, src)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gitsocial s3: site config: %v\n", err)
		return
	}
	if !cfgOK || cfg.Publish != "true" {
		if enabled, _, probeErr := siteEnabled(h.client, h.prefix); probeErr == nil && enabled {
			fmt.Fprintf(os.Stderr, "gitsocial s3: bucket has a site; set `gitsocial config site set publish true` to keep maintaining it\n")
		}
		return
	}
	// The refs-derived artifacts depend only on the refs listing and HEAD, so the marker can skip them. The items append below is not gated on it.
	shellVersion, verErr := siteVersion()
	upToDate, digest := false, ""
	if verErr == nil {
		upToDate, digest = siteMaintenanceUpToDate(h.client, h.prefix, shellVersion, h.override)
	}
	shellUploaded := false
	if !upToDate {
		// Shell first, so no reader sees data artifacts without an entry page.
		var err error
		if shellUploaded, err = ensureSiteShell(h.client, h.prefix); err != nil {
			fmt.Fprintf(os.Stderr, "gitsocial s3: site refresh: %v\n", err)
		}
		h.writeSitePMConfig(src)
		h.writeSiteCustomization(src)
	}
	h.updateSiteItems(extPushed)
	// A shell upload overwrites the dual-owned index.html, so reclaim the front page in the one case the marker below would still be stamped.
	reclaimOK := true
	if shellUploaded {
		reclaimOK = h.reclaimSitePagesFront(refs, src)
	}
	// Stamp the marker only when this pass left nothing a later site pass must still do; a withheld marker costs one extra pass, a wrong one costs a skip.
	if !upToDate && reclaimOK && manifestOK {
		pagesState, pagesPending := sitePagesState(h.client, h.prefix, refs, h.override, src)
		if !siteItemsBootstrapPending(h.client, h.prefix, refs) && !pagesPending {
			writeSitePushState(h.client, h.prefix, shellVersion, digest, pagesState)
		}
	}
}

// writeSitePMConfig publishes the resolved PM board config after every push.
func (h *remoteHelper) writeSitePMConfig(src *localCommitSource) {
	refs := h.remoteRefs
	if err := writeSitePMConfig(h.client, h.prefix, refs, src); err != nil {
		fmt.Fprintf(os.Stderr, "gitsocial s3: site pm config: %v\n", err)
	}
}

// writeSiteCustomization publishes the validated site customization after every push.
func (h *remoteHelper) writeSiteCustomization(src *localCommitSource) {
	refs := h.remoteRefs
	if err := writeSiteCustomization(h.client, h.prefix, refs, h.override, src); err != nil {
		fmt.Fprintf(os.Stderr, "gitsocial s3: site customization: %v\n", err)
	}
}

// updateSiteItems maintains the site artifacts for every pushed data branch, plus the code index; the repair machine heals whatever a failure leaves.
func (h *remoteHelper) updateSiteItems(extPushed map[string]string) {
	// Every commit the walk visits is an ancestor of a just-pushed tip, so read it from the local odb rather than the bucket.
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

// reclaimSitePagesFront re-writes index.html after a shell upload clobbered it, but only when the page set is complete and current; ok=false only on a failed reclaim.
func (h *remoteHelper) reclaimSitePagesFront(refs map[string]string, src *localCommitSource) (ok bool) {
	cfg, cfgOK, err := readSiteCustomization(h.client, h.prefix, refs, h.override, src)
	if err != nil {
		return false // can't tell if a reclaim was needed: withhold the marker
	}
	url, on := sitePagesEffective(cfg, cfgOK)
	if !on {
		return true // layer off: index.html is legitimately the shell
	}
	site := sitePageSiteFor(h.prefix, cfg, url)
	manifest, err := readSitePagesManifest(h.client, h.prefix)
	if err != nil {
		return false
	}
	if manifest == nil || manifest.Cursor != nil || manifest.SiteHash != sitePageSiteHash(site) {
		return true // page set pending/stale: a site push rebuilds+reclaims
	}
	manifests, tips, err := readSitePagesManifests(h.client, h.prefix, refs)
	if err != nil {
		return false
	}
	if !sitePagesTipsCurrent(manifest, tips) {
		return true // tips moved: pending, a site push rebuilds+reclaims
	}
	home := readSiteFrontHome(src, site, refs, readSiteDefaultBranch(h.client, h.prefix))
	if err := reclaimSiteFrontPage(h.client, h.prefix, site, manifests, home); err != nil {
		fmt.Fprintf(os.Stderr, "gitsocial s3: reclaim front page: %v\n", err)
		return false
	}
	return true
}

// updateSiteCodeItems maintains the single code items index across every code branch the bucket carries.
func (h *remoteHelper) updateSiteCodeItems(src *localCommitSource) {
	refs := h.remoteRefs
	defaultBranch := strings.TrimPrefix(localDefaultBranchRef(), "refs/heads/")
	tips := codeBranchTips(refs, defaultBranch)
	sp := &siteProgress{progress: h.progress, ext: siteCodeExt, src: src}
	if err := updateSiteCodeIndex(h.client, h.prefix, tips, defaultBranch, sp); err != nil {
		fmt.Fprintf(os.Stderr, "gitsocial s3: code index: %v\n", err)
	}
}

// publishRefManifest writes the helper's ref view as the manifest after a push, re-deriving it from a listing when the document moved.
func (h *remoteHelper) publishRefManifest(updates map[string]string) error {
	for attempt := 0; attempt < maxCASRetries; attempt++ {
		etag, err := publishRefManifest(h.client, h.prefix, h.refMode, h.remoteRefs, h.manifestETag)
		if err == nil {
			h.manifestETag = etag
			return nil
		}
		if !errors.Is(err, ErrPreconditionFailed) {
			return err
		}
		if _, h.manifestETag, err = readClaimsWithETag(h.client, h.prefix+bucketRefsKey); err != nil && !errors.Is(err, ErrNotFound) {
			return err
		}
		refs, err := readRemoteRefs(h.client, h.prefix)
		if err != nil {
			return fmt.Errorf("read refs: %w", err)
		}
		for ref, sha := range updates {
			if sha == "" {
				delete(refs, ref)
			} else {
				refs[ref] = sha
			}
		}
		h.remoteRefs = refs
	}
	return fmt.Errorf("upload %s: too much contention (gave up after %d attempts)", bucketRefsKey, maxCASRetries)
}

// maxCASRetries bounds the read-check-write loop; per-element refs rarely contend, so hitting it means something is spinning.
const maxCASRetries = 5

// zeroOID is git's null object id (in a cas lease: the ref must not exist).
const zeroOID = "0000000000000000000000000000000000000000"

// checkLease enforces a --force-with-lease expectation at write time; both CAS loops re-check it on every attempt, so a racing pusher is caught on the re-read.
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

// applyRefUpdate writes or deletes one ref by the bucket's ref mode and returns the written sha; an annotated tag stores the tag object, as git does.
func (h *remoteHelper) applyRefUpdate(cmd pushCommand) (string, error) {
	if h.refMode == refModeGeneration {
		return h.applyRefUpdateGeneration(cmd)
	}
	return h.applyRefUpdateETag(cmd)
}

// applyRefUpdateETag writes or deletes one plain ref key with ETag compare-and-swap, re-reading on a precondition failure.
func (h *remoteHelper) applyRefUpdateETag(cmd pushCommand) (string, error) {
	key := h.prefix + cmd.dst
	if cmd.src == "" {
		// Deletion stays unconditional: S3 has no conditional DELETE, and git guards deletes client-side.
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

// applyRefUpdateGeneration writes or deletes one ref as a generation chain, creating the next key with the only CAS a create-only provider enforces.
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

// gcGenerations deletes generations older than the written one's predecessor, so a concurrent reader's list-then-read window survives one more update.
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

// deleteRefGenerations removes a ref's whole chain, oldest first, so readers keep resolving the current value until the end.
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

// checkFastForward enforces the non-force rule: the remote's current value must be an ancestor of the pushed one.
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

// refModeKey and casProbeKey live under a dot-prefixed namespace no refname can reach.
const (
	refModeKey  = ".gitsocial/ref-mode"
	casProbeKey = ".gitsocial/cas-probe"
)

// Ref modes: how this bucket stores ref updates.
const (
	refModeETag       = "etag"       // plain keys, If-Match update CAS
	refModeGeneration = "generation" // chain keys, If-None-Match: * create CAS
)

// resolveRefMode determines the bucket's ref mode and caches it; the bucket's marker wins over provider capability, so every writer agrees.
func (h *remoteHelper) resolveRefMode() error {
	if h.refMode != "" {
		return nil
	}
	mode, err := readRefModeMarker(h.client, h.prefix)
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

// refModeFromCapability picks the ref mode for a fresh bucket, probing conditional writes when no preset declares them.
func (h *remoteHelper) refModeFromCapability() (string, error) {
	capability := h.capability
	if capability == CapabilityUnknown {
		var err error
		if capability, err = h.probeCapability(); err != nil {
			return "", err
		}
		// Keep what the probe learned: this push's document rewrites read it.
		h.capability = capability
	} else if err := h.probeCreateCAS(); err != nil {
		// A declared capability still gets the create-CAS check, so a bucket that ignores conditional headers is rejected.
		return "", err
	}
	if capability == CapabilityFull {
		return refModeETag, nil
	}
	return refModeGeneration, nil
}

// probeCreateCAS verifies the bucket enforces If-None-Match creates: write a probe key, then require a duplicate create to fail.
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

// probeCapability classifies an unknown endpoint: create-CAS must hold, then If-Match behavior decides full against create-only.
func (h *remoteHelper) probeCapability() (Capability, error) {
	if err := h.probeCreateCAS(); err != nil {
		return CapabilityUnknown, err
	}
	probe := h.prefix + casProbeKey
	if err := h.client.Put(probe, []byte("probe\n")); err != nil {
		return CapabilityUnknown, fmt.Errorf("conditional-write probe (overwrite): %w", err)
	}
	defer func() { _ = h.client.Delete(probe) }()
	// A wrong but well-formed ETag: success means If-Match is ignored, so only create-CAS can be trusted.
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

// publishRefMode records the bucket's ref mode with a create-CAS write, so concurrent first pushers converge.
func (h *remoteHelper) publishRefMode(mode string) (string, error) {
	err := h.client.PutIfAbsent(h.prefix+refModeKey, []byte(mode+"\n"))
	if errors.Is(err, ErrPreconditionFailed) {
		existing, err := readRefModeMarker(h.client, h.prefix)
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
		// Keep an existing HEAD unless it points at a gitmsg data branch, which is not a valid default.
		if !strings.Contains(string(cur), "refs/heads/gitmsg/") {
			return
		}
	}
	_ = h.client.Put(h.prefix+"HEAD", []byte("ref: "+branch+"\n"))
}

// repairDanglingHEAD repoints a bucket HEAD whose target ref the bucket does not carry, to the first candidate it does.
func (h *remoteHelper) repairDanglingHEAD(refs map[string]string, candidates ...string) {
	cur, err := h.client.Get(h.prefix + "HEAD")
	if err != nil {
		return
	}
	target := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(cur)), "ref:"))
	if target == "" || refs[target] != "" {
		return
	}
	for _, candidate := range candidates {
		// Never trade one dangling symref for another.
		if candidate == "" || refs[candidate] == "" {
			continue
		}
		if err := h.client.Put(h.prefix+"HEAD", []byte("ref: "+candidate+"\n")); err != nil {
			fmt.Fprintf(os.Stderr, "gitsocial s3: repair HEAD: %v\n", err)
			return
		}
		fmt.Fprintf(os.Stderr, "gitsocial s3: HEAD pointed at %s, which this bucket does not carry; repointed to %s\n", target, candidate)
		return
	}
}

// localDefaultBranchRef returns the pushing repo's HEAD symref, so the bucket advertises its real default branch.
func localDefaultBranchRef() string {
	ref, err := gitOutput("symbolic-ref", "HEAD")
	if err != nil || !strings.HasPrefix(ref, "refs/heads/") {
		return ""
	}
	return ref
}

// uploadMissingObjects uploads every object the pushed refs reach that the bucket lacks; a thin push widens the negative end for code refs alone, so gitmsg stays whole.
func (h *remoteHelper) uploadMissingObjects(cmds []pushCommand) error {
	tips, err := h.remoteTipsPresentLocally()
	if err != nil {
		return err
	}
	var srcs, gitmsgSrcs, codeSrcs []string
	for _, cmd := range cmds {
		if cmd.src == "" {
			continue // deletion: nothing to upload
		}
		srcs = append(srcs, cmd.src)
		if isGitmsgRef(cmd.src) || isGitmsgRef(cmd.dst) {
			gitmsgSrcs = append(gitmsgSrcs, cmd.src)
		} else {
			codeSrcs = append(codeSrcs, cmd.src)
		}
	}
	if len(srcs) == 0 {
		return nil
	}
	seen := map[string]bool{}
	var shas []string
	add := func(sha string) {
		if !seen[sha] {
			seen[sha] = true
			shas = append(shas, sha)
		}
	}
	// rev-list --objects peels annotated tags, so upload the tag objects explicitly.
	for _, src := range srcs {
		sha, err := gitOutput("rev-parse", "--verify", src)
		if err != nil {
			return fmt.Errorf("resolve %s: %w", src, err)
		}
		if objType, err := gitOutput("cat-file", "-t", sha); err == nil && objType == "tag" {
			add(sha)
		}
	}
	thin, upstreamURL := h.thinPush()
	if !thin {
		if err := revListObjects(h.workdir, srcs, tips, add); err != nil {
			return err
		}
		return h.uploadDelta(shas)
	}
	frontier, pins := h.verifyUpstreamFrontier(upstreamURL)
	h.thinPins = pins
	if err := revListObjects(h.workdir, gitmsgSrcs, tips, add); err != nil {
		return err
	}
	if err := revListObjects(h.workdir, codeSrcs, append(append([]string{}, tips...), frontier...), add); err != nil {
		return err
	}
	return h.uploadDelta(shas)
}

// revListObjects feeds add every object reachable from srcs but not from the excluded tips; an empty source list contributes nothing.
func revListObjects(dir string, srcs, excluded []string, add func(string)) error {
	if len(srcs) == 0 {
		return nil
	}
	args := append([]string{"rev-list", "--objects"}, srcs...)
	if len(excluded) > 0 {
		args = append(args, "--not")
		args = append(args, excluded...)
	}
	out, err := gitOutputIn(dir, args...)
	if err != nil {
		return fmt.Errorf("rev-list: %w", err)
	}
	for _, line := range strings.Split(out, "\n") {
		if len(line) >= 40 {
			add(line[:40])
		}
	}
	return nil
}

// uploadDelta uploads a push's object delta: packed once it is large enough to pay for a pack, loose below that.
func (h *remoteHelper) uploadDelta(shas []string) error {
	if len(shas) < resolvePackThreshold() {
		h.looseUploaded += len(shas)
		return h.uploadObjects(shas)
	}
	return h.uploadPacked(shas)
}

// uploadPacked builds and uploads the two packs and publishes the commit byte ranges as the pack map; a pack past the single-PUT ceiling falls back to loose objects.
func (h *remoteHelper) uploadPacked(shas []string) error {
	if len(shas) == 0 {
		return nil
	}
	h.progress.call("packing", 0, len(shas))
	packs, err := buildDeltaPacks(shas)
	if err != nil {
		return err
	}
	// Size-check every pack before uploading any, so the fallback cannot leave half the delta packed.
	for _, built := range packs {
		if len(built.pack) > maxPackUploadBytes {
			fmt.Fprintf(os.Stderr, "gitsocial s3: %s is %d bytes, past the single-PUT ceiling; uploading loose objects instead\n", built.name, len(built.pack))
			h.looseUploaded += len(shas)
			return h.uploadObjects(shas)
		}
	}
	for i, built := range packs {
		if err := publishPack(h.client, h.capability, h.prefix, built, resolveUploadConcurrency()); err != nil {
			return err
		}
		h.progress.call("packs", i+1, len(packs))
	}
	for _, sha := range shas {
		h.fetched[sha] = true
	}
	return nil
}

// remoteTipsPresentLocally resolves the remote's ref values and keeps those whose objects exist locally.
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

// listResumeThreshold is the delta size at which uploadObjects first lists objects/ to skip what is already present.
const listResumeThreshold = 2000

// filterPresentObjects drops shas the bucket already holds from a large delta; keys are content-addressed, so a present key is the finished object.
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
		// objects/<xx>/<38-hex>: reassemble the 40-hex sha.
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

// uploadObjects streams objects out of the local odb, re-encodes each as a loose object and uploads them through a bounded worker pool.
func (h *remoteHelper) uploadObjects(shas []string) error {
	if len(shas) == 0 {
		return nil
	}
	// On a large delta, drop what the bucket already holds so an interrupted first push resumes.
	shas = filterPresentObjects(h.client, h.prefix, shas)
	if len(shas) == 0 {
		return nil
	}
	cmd := gitCmdIn(h.workdir, "cat-file", "--batch")
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

// uploadEncodedObjects runs a bounded worker pool that PUTs each object the producer emits; the first error cancels the rest. Refs move only after it returns nil.
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

// putObjectWithRetry retries an object PUT past a transient fault; a re-PUT is idempotent, and a refusal surfaces at once.
func putObjectWithRetry(ctx context.Context, client *Client, key string, body []byte) error {
	var err error
	for attempt := 0; ; attempt++ {
		if err = client.Put(key, body); err == nil || attempt >= len(retryBackoff) || !isTransientFault(err) {
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

// gitOutput runs a git command against the GIT_DIR git handed the helper and returns trimmed stdout.
func gitOutput(args ...string) (string, error) {
	return gitOutputIn("", args...)
}

// oneLine collapses an error message to a single line for status reporting.
func oneLine(err error) string {
	return strings.ReplaceAll(err.Error(), "\n", " ")
}

// SiteOverride carries one remote's deployment-key overrides, applied over readSiteCustomization so every consumer sees effective values.
type SiteOverride struct {
	URL     string
	Publish string
	Pages   string
}

// readRemoteSiteOverride reads a remote's site deployment overrides from git config; an empty name yields none.
func readRemoteSiteOverride(remoteName string) SiteOverride {
	if remoteName == "" {
		return SiteOverride{}
	}
	get := func(suffix string) string {
		v, err := gitOutput("config", "--get", "remote."+remoteName+"."+suffix)
		if err != nil {
			return ""
		}
		return strings.TrimSpace(v)
	}
	return SiteOverride{
		URL:     get(SiteOverrideURLKey),
		Publish: get(SiteOverridePublishKey),
		Pages:   get(SiteOverridePagesKey),
	}
}

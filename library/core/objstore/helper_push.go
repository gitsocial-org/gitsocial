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
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
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
	if refsMoved {
		// Site artifacts (refs manifest + per-extension item/body corpora) serve
		// ONLY the static read surface, so a plain s3:// git remote with no site
		// stays clean: gate them on the same site-enabled probe the shell refresh
		// uses. Best-effort throughout — a probe failure only skips this push's
		// maintenance, never the git push itself.
		enabled, _, err := siteEnabled(h.client, h.prefix)
		if err != nil {
			fmt.Fprintf(os.Stderr, "gitsocial s3: site probe: %v\n", err)
		} else if enabled {
			h.writeSiteManifest()
			h.writeSitePMConfig()
			h.writeSiteCustomization()
			h.writeSitePrismExtra()
			h.updateSiteItems(extPushed)
			// Site self-refresh: buckets carrying the static read surface pick up
			// this binary's embedded version on any push. Best-effort, like the
			// manifest: a failure only leaves the site stale until the next push.
			if err := refreshSiteIfEnabled(h.client, h.prefix); err != nil {
				fmt.Fprintf(os.Stderr, "gitsocial s3: site refresh: %v\n", err)
			}
		}
	}
	fmt.Fprint(w, "\n")
	return nil
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

// writeSitePrismExtra publishes the repo-specific extra Prism grammars bundle
// after every push (scanning the default branch tree for the languages it uses),
// so the static site highlights the long tail of languages without bloating the
// base shell. Best-effort, same contract as the manifest: a failure only leaves
// the site on the base grammars until the next push.
func (h *remoteHelper) writeSitePrismExtra() {
	refs := h.remoteRefs
	if refs == nil {
		var err error
		if refs, err = readRemoteRefs(h.client, h.prefix); err != nil {
			fmt.Fprintf(os.Stderr, "gitsocial s3: site prism extra: %v\n", err)
			return
		}
	}
	if err := writeSitePrismExtra(h.client, h.prefix, refs); err != nil {
		fmt.Fprintf(os.Stderr, "gitsocial s3: site prism extra: %v\n", err)
	}
}

// updateSiteItems maintains the per-extension site artifacts (metadata index +
// search corpus) for every pushed gitmsg data branch, alongside the refs
// manifest. Best-effort, same contract: a stale artifact only degrades the site
// until the next push. An error here can now only be transient (a network /
// bucket failure): the artifact state is always repairable on the next push (the
// repair state machine rebuilds any mismatch from the immutable sealed shards +
// a bounded walk), so a failed maintenance pass never wedges the index.
func (h *remoteHelper) updateSiteItems(extPushed map[string]string) {
	for ext, sha := range extPushed {
		var err error
		if sha == "" {
			err = deleteSiteArtifacts(h.client, h.prefix, ext)
		} else {
			err = updateSiteItemsIndex(h.client, h.prefix, ext, sha)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "gitsocial s3: items index %s: %v\n", ext, err)
		}
	}
}

// maxCASRetries bounds the read-check-write loop; contention on GitSocial's
// per-element refs is rare, so hitting this means something is spinning.
const maxCASRetries = 5

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
			err = h.client.PutIfAbsent(key, value)
		case err != nil:
			return "", fmt.Errorf("read ref %s: %w", cmd.dst, err)
		default:
			current := strings.TrimSpace(string(currentRaw))
			if current == sha {
				return sha, nil // already up to date
			}
			if !cmd.forced {
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
			if !cmd.forced {
				if err := checkFastForward(currentSHA, sha, cmd.dst); err != nil {
					return "", err
				}
			}
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

// uploadObjects streams objects out of the local odb via `git cat-file
// --batch`, re-encodes each as a loose object, and uploads it. Content
// addressing makes re-uploads idempotent, so no per-object existence check.
func (h *remoteHelper) uploadObjects(shas []string) error {
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

	// The batch read stays sequential (one git process); uploads fan out with
	// bounded parallelism so push latency isn't one HTTP round trip per object.
	const uploadParallelism = 8
	sem := make(chan struct{}, uploadParallelism)
	var wg sync.WaitGroup
	var errMu sync.Mutex
	var uploadErr error
	setErr := func(err error) {
		errMu.Lock()
		if uploadErr == nil {
			uploadErr = err
		}
		errMu.Unlock()
	}
	failed := func() bool {
		errMu.Lock()
		defer errMu.Unlock()
		return uploadErr != nil
	}

	reader := bufio.NewReaderSize(stdout, 1<<20)
	for _, sha := range shas {
		if failed() {
			break
		}
		if _, err := io.WriteString(stdin, sha+"\n"); err != nil {
			setErr(err)
			break
		}
		header, err := reader.ReadString('\n')
		if err != nil {
			setErr(fmt.Errorf("cat-file %s: %w", sha, err))
			break
		}
		fields := strings.Fields(strings.TrimSpace(header))
		if len(fields) != 3 || fields[1] == "missing" {
			setErr(fmt.Errorf("cat-file %s: unexpected response %q", sha, strings.TrimSpace(header)))
			break
		}
		objType := fields[1]
		var size int64
		if _, err := fmt.Sscanf(fields[2], "%d", &size); err != nil {
			setErr(fmt.Errorf("cat-file %s: bad size in %q", sha, header))
			break
		}
		content := make([]byte, size)
		if _, err := io.ReadFull(reader, content); err != nil {
			setErr(fmt.Errorf("cat-file %s: read content: %w", sha, err))
			break
		}
		if _, err := reader.Discard(1); err != nil { // trailing newline
			setErr(fmt.Errorf("cat-file %s: %w", sha, err))
			break
		}
		compressed, err := encodeLooseObject(objType, content)
		if err != nil {
			setErr(err)
			break
		}
		h.fetched[sha] = true
		wg.Add(1)
		sem <- struct{}{}
		go func(sha string, compressed []byte) {
			defer wg.Done()
			defer func() { <-sem }()
			key := h.prefix + "objects/" + sha[:2] + "/" + sha[2:]
			if err := h.client.Put(key, compressed); err != nil {
				setErr(fmt.Errorf("upload object %s: %w", sha, err))
			}
		}(sha, compressed)
	}
	wg.Wait()
	return uploadErr
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

// thin.go - thin fork buckets: the push-side frontier and the .gitsocial/upstream key.
//
// A thin bucket carries only the fork's own objects: code history it shares with
// upstream is excluded from the upload and resolved by the reader from upstream
// instead (see the overlay in helper.go). Two rules make that safe:
//
//   - gitmsg is never thinned. Objects reachable from refs/heads/gitmsg/* and
//     refs/gitmsg/* are always pushed complete, so fork discovery, issues, PR
//     metadata and comments read from a thin bucket with no upstream at all.
//   - the frontier is verified by FETCHING upstream, never by trusting local
//     tracking refs: excluding an object the reader cannot get back produces a
//     bucket nobody can clone.
//
// Thinness is a property of the push RELATIONSHIP, recorded in local git config
// next to the remote it describes (remote.<name>.gitsocial-thin /
// -gitsocial-upstream), exactly like the per-remote site overrides.
package objstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// thinUpstreamKey records, on the bucket, which upstream a thin fork is thin
// against and the frontier its last push excluded. Mutable, so it classifies as
// no-cache under the existing cacheControlForKey rules.
const thinUpstreamKey = ".gitsocial/upstream"

// thinUpstreamVersion is the schema version of the .gitsocial/upstream document.
const thinUpstreamVersion = 1

// upstreamRemoteName is the git remote (and remote-tracking namespace) both
// sides of a thin relationship use for upstream: the push-side frontier fetch
// and the read-side overlay land in refs/remotes/upstream/*.
const upstreamRemoteName = "upstream"

// Config keys recording a thin push relationship, read from the local git config
// of the repo the helper was spawned in.
const (
	ThinConfigKey     = "gitsocial-thin"
	UpstreamConfigKey = "gitsocial-upstream"
)

// ThinPin is one frontier tip a thin push excluded against: the upstream branch
// and the sha it pointed at. The reader needs every pin fetchable from upstream.
type ThinPin struct {
	Ref string `json:"ref"`
	SHA string `json:"sha"`
}

// thinUpstreamDoc is the .gitsocial/upstream document: the URL a reader fetches
// and the frontier the last push asserted about it.
type thinUpstreamDoc struct {
	Version int       `json:"version"`
	URL     string    `json:"url"`
	Pins    []ThinPin `json:"pins"`
}

// allowedUpstreamSchemes are the only transports a thin upstream URL may name.
// .gitsocial/upstream is bucket content, so a hostile bucket must not get to
// pick an arbitrary `git fetch` transport (ext:: is command execution, file://
// reads local paths). ssh is deliberately excluded: a thin bucket must record
// the URL its readers can fetch anonymously, not the contributor's push
// transport. Object integrity needs no defense here — everything fetched
// verifies against the shas the fork's own refs demand.
var allowedUpstreamSchemes = []string{"https://", "http://", "s3://"}

// checkUpstreamURL rejects an upstream URL whose transport is not allowlisted,
// naming the key and the offending URL so the refusal is actionable.
func checkUpstreamURL(url string) error {
	for _, scheme := range allowedUpstreamSchemes {
		if strings.HasPrefix(url, scheme) {
			return nil
		}
	}
	return fmt.Errorf("%s names upstream %q, whose transport is not allowed (only https://, http:// and s3:// may be fetched)", thinUpstreamKey, url)
}

// isGitmsgRef reports whether a refname belongs to the gitmsg classes that are
// never thinned: the data branches (refs/heads/gitmsg/*) and the state refs
// (refs/gitmsg/*).
func isGitmsgRef(name string) bool {
	return strings.HasPrefix(name, "refs/heads/gitmsg/") || strings.HasPrefix(name, "refs/gitmsg/")
}

// thinRelationship reads the push relationship for a remote from git config:
// thin=true only when remote.<name>.gitsocial-thin is a true git boolean AND
// remote.<name>.gitsocial-upstream names an allowed transport. A thin flag with
// an unusable upstream degrades to a normal (full) push with a stderr note,
// never to a silently incomplete bucket.
func thinRelationship(remoteName string) (thin bool, upstreamURL string) {
	if remoteName == "" {
		return false, "" // anonymous-URL invocation: no per-remote config to read
	}
	get := func(suffix string, args ...string) string {
		v, err := gitOutput(append(append([]string{"config"}, args...), "--get", "remote."+remoteName+"."+suffix)...)
		if err != nil {
			return ""
		}
		return strings.TrimSpace(v)
	}
	if get(ThinConfigKey, "--bool") != "true" {
		return false, ""
	}
	url := get(UpstreamConfigKey)
	if url == "" {
		fmt.Fprintf(os.Stderr, "gitsocial s3: remote %s is marked thin but remote.%s.%s is unset; pushing the full history\n", remoteName, remoteName, UpstreamConfigKey)
		return false, ""
	}
	if err := checkUpstreamURL(url); err != nil {
		fmt.Fprintf(os.Stderr, "gitsocial s3: %v; pushing the full history\n", err)
		return false, ""
	}
	return true, url
}

// thinPush resolves this session's push relationship once and caches it.
func (h *remoteHelper) thinPush() (thin bool, upstreamURL string) {
	if !h.thinResolved {
		h.thinResolved = true
		h.thin, h.upstreamURL = thinRelationship(h.remoteName)
	}
	return h.thin, h.upstreamURL
}

// verifyUpstreamFrontier returns the shas a thin push may exclude against, plus
// the pins to record for them. Three tiers, in order:
//
//  1. fetch upstream: the post-fetch remote-tracking tips are advertised-now and
//     locally present by construction, so rev-list can use them directly, and
//     --prune drops branches upstream deleted.
//  2. the fetch failed (offline, private, gone): fall back to the pins the
//     bucket already records, keeping those whose sha is present locally. They
//     were verified when written and are already baked into the bucket.
//  3. neither: no exclusion at all — a full push, with a one-line stderr note.
//
// Objects a dropped tip covered simply upload on this push: the fork fattens
// exactly as much as upstream drift requires, with no special case.
func (h *remoteHelper) verifyUpstreamFrontier(upstreamURL string) (frontier []string, pins []ThinPin) {
	refspec := "+refs/heads/*:refs/remotes/" + upstreamRemoteName + "/*"
	if _, err := h.git("fetch", "--prune", "--no-tags", upstreamURL, refspec); err == nil {
		out, listErr := h.git("for-each-ref", "--format=%(refname) %(objectname)", "refs/remotes/"+upstreamRemoteName+"/")
		if listErr == nil {
			for _, line := range strings.Split(out, "\n") {
				name, sha, ok := strings.Cut(strings.TrimSpace(line), " ")
				if !ok || len(sha) != 40 {
					continue
				}
				branch := strings.TrimPrefix(name, "refs/remotes/"+upstreamRemoteName+"/")
				frontier = append(frontier, sha)
				pins = append(pins, ThinPin{Ref: "refs/heads/" + branch, SHA: sha})
			}
			return frontier, pins
		}
	}
	doc, err := readThinUpstream(h.client, h.prefix)
	if err == nil && doc != nil {
		for _, pin := range doc.Pins {
			if _, err := h.git("cat-file", "-e", pin.SHA); err == nil {
				frontier = append(frontier, pin.SHA)
				pins = append(pins, pin)
			}
		}
	}
	if len(pins) > 0 {
		fmt.Fprintf(os.Stderr, "gitsocial s3: upstream %s unreachable; excluding against the %d recorded pin(s)\n", upstreamURL, len(pins))
		return frontier, pins
	}
	fmt.Fprintf(os.Stderr, "gitsocial s3: upstream %s unreachable and no usable pins recorded; pushing the full history\n", upstreamURL)
	return nil, nil
}

// publishThinUpstream records the frontier this push used on the bucket, so a
// reader knows where to resolve the excluded objects and the next push can fall
// back to it when upstream is unreachable. A push that computed no frontier
// (a deletion-only batch) keeps whatever the bucket already records.
func (h *remoteHelper) publishThinUpstream(upstreamURL string) {
	if h.thinPins == nil {
		if doc, err := readThinUpstream(h.client, h.prefix); err == nil && doc != nil && doc.URL == upstreamURL {
			return
		}
	}
	if err := writeThinUpstream(h.client, h.prefix, thinUpstreamDoc{Version: thinUpstreamVersion, URL: upstreamURL, Pins: h.thinPins}); err != nil {
		fmt.Fprintf(os.Stderr, "gitsocial s3: record %s: %v\n", thinUpstreamKey, err)
	}
}

// ensureUpstreamLocal is the read overlay: it fetches the upstream a thin bucket
// names into this repo, once per session, so walkObject's presence
// short-circuit stops at the boundary between the fork's objects and upstream's.
// There is no per-object resolver — the pre-fetch IS the overlay.
//
// ran=false means the bucket is not a thin fork and there was nothing to do.
// Upstream refs land in refs/remotes/upstream/* with `upstream` registered as a
// real remote: a clone of a thin fork genuinely depends on upstream, and keeping
// the borrowed objects reachable is what stops a later `git gc` from corrupting
// the clone.
func (h *remoteHelper) ensureUpstreamLocal() (ran bool, err error) {
	if h.upstreamPulled {
		return false, nil
	}
	h.upstreamPulled = true
	doc, err := readThinUpstream(h.client, h.prefix)
	if err != nil || doc == nil {
		return false, err
	}
	// (1) The URL is bucket content, so validate the transport BEFORE any fetch:
	// a hostile bucket must not get to pick what git executes (ext:: is command
	// execution, file:// reads local paths).
	if err := checkUpstreamURL(doc.URL); err != nil {
		return false, err
	}
	h.upstreamURL = doc.URL
	// (2) One branch fetch covers every pin still reachable from a current tip,
	// which is the normal case.
	refspec := "+refs/heads/*:refs/remotes/" + upstreamRemoteName + "/*"
	if _, err := h.git("fetch", "--no-tags", doc.URL, refspec); err != nil {
		fmt.Fprintf(os.Stderr, "gitsocial s3: thin fork: fetching upstream %s failed: %v\n", doc.URL, err)
	}
	// (3) Any pin the branch fetch did not cover: ask for it by sha. That only
	// works where upstream allows it, so a failure is reported naming the commit
	// and the URL rather than surfacing later as a bare missing object.
	for _, pin := range doc.Pins {
		if _, err := h.git("cat-file", "-e", pin.SHA); err == nil {
			continue
		}
		if _, err := h.git("fetch", "--no-tags", doc.URL, pin.SHA); err != nil {
			fmt.Fprintf(os.Stderr, "gitsocial s3: thin fork: commit %s (%s) is not available from upstream %s\n", pin.SHA, pin.Ref, doc.URL)
		}
	}
	// (4) Record the dependency as a visible remote (idempotent; failure ignored).
	if _, err := h.git("config", "--get", "remote."+upstreamRemoteName+".url"); err != nil {
		_, _ = h.git("config", "remote."+upstreamRemoteName+".url", doc.URL)
	}
	h.refreshLocalOdb(doc.Pins)
	return true, nil
}

// refreshLocalOdb restarts the long-running `cat-file --batch` when it cannot
// see an object the overlay just fetched. Modern git re-scans the pack directory
// on a lookup miss (verified on git 2.50), so this is normally a no-op; an older
// git needs the restart or the presence probe would miss every borrowed object.
func (h *remoteHelper) refreshLocalOdb(pins []ThinPin) {
	for _, pin := range pins {
		if _, err := h.git("cat-file", "-e", pin.SHA); err != nil {
			continue // not fetched: tells us nothing about freshness
		}
		if _, _, ok := h.localOdb().typed(pin.SHA); !ok {
			h.local.close()
			h.local = nil
		}
		return
	}
}

// readThinUpstream reads a bucket's thin-fork marker, returning nil (no error)
// when the bucket is not a thin fork.
func readThinUpstream(client *Client, prefix string) (*thinUpstreamDoc, error) {
	body, err := client.Get(prefix + thinUpstreamKey)
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", thinUpstreamKey, err)
	}
	var doc thinUpstreamDoc
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("parse %s: %w", thinUpstreamKey, err)
	}
	return &doc, nil
}

// writeThinUpstream publishes the thin-fork marker.
func writeThinUpstream(client *Client, prefix string, doc thinUpstreamDoc) error {
	body, err := json.Marshal(doc)
	if err != nil {
		return fmt.Errorf("marshal %s: %w", thinUpstreamKey, err)
	}
	if err := client.Put(prefix+thinUpstreamKey, body); err != nil {
		return fmt.Errorf("write %s: %w", thinUpstreamKey, err)
	}
	return nil
}

// ErrThinBucket is what the site surface refuses with on a thin fork bucket: it
// is helper-only, so it publishes no static site.
var ErrThinBucket = errors.New("thin fork bucket: its history is incomplete without upstream, so no static site is published; detach it with `gitsocial push --full`")

// PushFull detaches a bucket from its thin relationship: it uploads every object
// the bucket's own refs reach but the bucket does not carry, restores the
// dumb-HTTP ref advertisement, and deletes the thin marker. That turns "upstream
// is going away" from a crisis into one command, and afterwards the bucket is an
// ordinary, self-contained remote again. A no-op on a bucket that is not thin.
//
// workdir is the local repo the objects come from; its refs must cover the
// bucket's tips (the usual case for the clone that pushed them).
func PushFull(remoteURL string, env HelperEnv, workdir string, progress Progress) error {
	client, prefix, capability, err := clientForRemote(remoteURL, env)
	if err != nil {
		return err
	}
	doc, err := readThinUpstream(client, prefix)
	if err != nil || doc == nil {
		return err
	}
	refs, err := readRemoteRefs(client, prefix)
	if err != nil {
		return fmt.Errorf("read refs: %w", err)
	}
	h := &remoteHelper{client: client, prefix: prefix, capability: capability, workdir: workdir, fetched: map[string]bool{}, progress: progress}
	missing, err := h.missingBucketObjects(refs)
	if err != nil {
		return err
	}
	// Loose, never packed: this is a one-time repair, the dumb walker prefers
	// loose keys anyway, and the next sealing round packs them.
	if err := h.uploadObjects(missing); err != nil {
		return fmt.Errorf("upload missing objects: %w", err)
	}
	src := newLocalCommitSource("", workdir)
	defer src.close()
	if err := writeDumbTransportInfo(client, prefix, src, refs, false); err != nil {
		return err
	}
	if err := client.Delete(prefix + thinUpstreamKey); err != nil {
		return fmt.Errorf("delete %s: %w", thinUpstreamKey, err)
	}
	return nil
}

// missingBucketObjects lists every object the bucket's own ref tips reach that
// the bucket does not already carry — the objects a thin push left to upstream.
func (h *remoteHelper) missingBucketObjects(refs map[string]string) ([]string, error) {
	inventory, err := bucketObjectInventory(h.client, h.prefix)
	if err != nil {
		return nil, err
	}
	var tips []string
	for _, sha := range refs {
		if _, err := h.git("cat-file", "-e", sha); err == nil {
			tips = append(tips, sha)
		}
	}
	if len(tips) == 0 {
		return nil, nil
	}
	seen := map[string]bool{}
	var missing []string
	add := func(sha string) {
		if !inventory[sha] && !seen[sha] {
			seen[sha] = true
			missing = append(missing, sha)
		}
	}
	// rev-list --objects peels annotated tags; carry the tag objects themselves.
	for _, sha := range tips {
		if objType, err := h.git("cat-file", "-t", sha); err == nil && objType == "tag" {
			add(sha)
		}
	}
	if err := revListObjects(h.workdir, tips, nil, add); err != nil {
		return nil, err
	}
	return missing, nil
}

// bucketObjectInventory is the set of object shas a bucket carries: its loose
// keys plus every object its published packs index.
func bucketObjectInventory(client *Client, prefix string) (map[string]bool, error) {
	objs, err := client.ListWithETags(prefix + "objects/")
	if err != nil {
		return nil, fmt.Errorf("list bucket objects: %w", err)
	}
	inventory := make(map[string]bool, len(objs))
	for _, obj := range objs {
		rel := strings.TrimPrefix(obj.Key, prefix+"objects/")
		rel = strings.Replace(rel, "/", "", 1)
		if len(rel) == 40 {
			inventory[rel] = true
		}
	}
	names, err := listBucketPacks(client, prefix)
	if err != nil {
		return nil, fmt.Errorf("list bucket packs: %w", err)
	}
	for _, name := range names {
		idx, err := client.GetRetry(prefix + packKeyPrefix + name + ".idx")
		if err != nil {
			return nil, fmt.Errorf("read pack index %s: %w", name, err)
		}
		entries, err := parsePackIdx(idx)
		if err != nil {
			return nil, fmt.Errorf("pack %s: %w", name, err)
		}
		for _, entry := range entries {
			inventory[entry.sha] = true
		}
	}
	return inventory, nil
}

// ThinUpstream reports the upstream a bucket is thin against ("" when the bucket
// is not a thin fork) and how many tips its last push pinned. Shared by the
// clone/status surfaces and the site refusal, so all three read the SAME bucket
// key rather than trusting per-clone config.
func ThinUpstream(remoteURL string, env HelperEnv) (url string, pins int, err error) {
	client, prefix, _, err := clientForRemote(remoteURL, env)
	if err != nil {
		return "", 0, err
	}
	doc, err := readThinUpstream(client, prefix)
	if err != nil || doc == nil {
		return "", 0, err
	}
	return doc.URL, len(doc.Pins), nil
}

// gitOutputIn runs a git command in dir ("" = the repo the helper inherits from
// git via GIT_DIR) and returns trimmed stdout.
func gitOutputIn(dir string, args ...string) (string, error) {
	cmd := gitCmdIn(dir, args...)
	out, err := cmd.Output()
	if err != nil {
		var stderr string
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			stderr = strings.TrimSpace(string(exitErr.Stderr))
		}
		return "", fmt.Errorf("git %s: %v %s", strings.Join(args, " "), err, stderr)
	}
	return strings.TrimSpace(string(out)), nil
}

// gitCmdIn builds a git command bound to dir ("" = the ambient repo).
func gitCmdIn(dir string, args ...string) *exec.Cmd {
	if dir != "" {
		args = append([]string{"-C", dir}, args...)
	}
	return exec.Command("git", args...)
}

// git runs a git command in the helper's repo — the GIT_DIR git handed it, or
// the explicit workdir a CLI-side entry point (PushFull) was built with.
func (h *remoteHelper) git(args ...string) (string, error) {
	return gitOutputIn(h.workdir, args...)
}

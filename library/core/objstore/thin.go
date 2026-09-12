// thin.go - thin fork buckets: the push-side frontier, the read overlay and the .gitsocial/upstream marker
//
// Two rules make thinning safe: gitmsg refs are pushed complete, and the
// frontier is verified by fetching upstream rather than by trusting local
// tracking refs.
package objstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// thinUpstreamKey records which upstream a thin fork is thin against, and the frontier its last push excluded.
const thinUpstreamKey = ".gitsocial/upstream"

// thinUpstreamVersion is the schema version of the .gitsocial/upstream document.
const thinUpstreamVersion = 1

// upstreamRemoteName is the remote and tracking namespace both sides of a thin relationship use for upstream.
const upstreamRemoteName = "upstream"

// Config keys recording a thin push relationship, read from the local git config.
const (
	ThinConfigKey     = "gitsocial-thin"
	UpstreamConfigKey = "gitsocial-upstream"
)

// ThinPin is one frontier tip a thin push excluded against; a reader needs every pin fetchable from upstream.
type ThinPin struct {
	Ref string `json:"ref"`
	SHA string `json:"sha"`
}

// thinUpstreamDoc is the .gitsocial/upstream document: the URL a reader fetches and the frontier the last push asserted.
type thinUpstreamDoc struct {
	Version int       `json:"version"`
	URL     string    `json:"url"`
	Pins    []ThinPin `json:"pins"`
}

// allowedUpstreamSchemes are the only transports a thin upstream URL may name; the URL is bucket content, so a bucket cannot pick what git executes.
var allowedUpstreamSchemes = []string{"https://", "http://", "s3://"}

// checkUpstreamURL rejects an upstream URL whose transport is not allowlisted.
func checkUpstreamURL(url string) error {
	for _, scheme := range allowedUpstreamSchemes {
		if strings.HasPrefix(url, scheme) {
			return nil
		}
	}
	return fmt.Errorf("%s names upstream %q, whose transport is not allowed (only https://, http:// and s3:// may be fetched)", thinUpstreamKey, url)
}

// isGitmsgRef reports whether a refname is one of the gitmsg classes a thin push leaves whole.
func isGitmsgRef(name string) bool {
	return strings.HasPrefix(name, "refs/heads/gitmsg/") || strings.HasPrefix(name, "refs/gitmsg/")
}

// thinRelationship reads a remote's push relationship from git config; a thin flag with an unusable upstream degrades to a full push with a note.
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

// verifyUpstreamFrontier returns the shas a thin push may exclude against, in three tiers: a fresh upstream fetch, the pins the bucket records, then no exclusion at all.
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

// publishThinUpstream records the frontier this push used; a push that computed none keeps what the bucket already records.
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

// ensureUpstreamLocal is the read overlay: one upstream fetch per session, with upstream registered as a real remote so the borrowed objects stay reachable.
func (h *remoteHelper) ensureUpstreamLocal() (ran bool, err error) {
	if h.upstreamPulled {
		return false, nil
	}
	h.upstreamPulled = true
	doc, err := readThinUpstream(h.client, h.prefix)
	if err != nil || doc == nil {
		return false, err
	}
	// The URL is bucket content, so validate its transport before any fetch runs.
	if err := checkUpstreamURL(doc.URL); err != nil {
		return false, err
	}
	h.upstreamURL = doc.URL
	// One branch fetch covers every pin still reachable from a current tip.
	refspec := "+refs/heads/*:refs/remotes/" + upstreamRemoteName + "/*"
	if _, err := h.git("fetch", "--no-tags", doc.URL, refspec); err != nil {
		fmt.Fprintf(os.Stderr, "gitsocial s3: thin fork: fetching upstream %s failed: %v\n", doc.URL, err)
	}
	// Ask for any uncovered pin by sha, and name the commit on failure rather than surfacing a bare missing object later.
	for _, pin := range doc.Pins {
		if _, err := h.git("cat-file", "-e", pin.SHA); err == nil {
			continue
		}
		if _, err := h.git("fetch", "--no-tags", doc.URL, pin.SHA); err != nil {
			fmt.Fprintf(os.Stderr, "gitsocial s3: thin fork: commit %s (%s) is not available from upstream %s\n", pin.SHA, pin.Ref, doc.URL)
		}
	}
	// Record the dependency as a visible remote; idempotent, and a failure is ignored.
	if _, err := h.git("config", "--get", "remote."+upstreamRemoteName+".url"); err != nil {
		_, _ = h.git("config", "remote."+upstreamRemoteName+".url", doc.URL)
	}
	h.refreshLocalOdb(doc.Pins)
	return true, nil
}

// refreshLocalOdb restarts the long-running cat-file batch when it cannot see an object the overlay just fetched.
func (h *remoteHelper) refreshLocalOdb(pins []ThinPin) {
	for _, pin := range pins {
		if _, err := h.git("cat-file", "-e", pin.SHA); err != nil {
			continue // not fetched: tells us nothing about freshness
		}
		if _, _, ok := h.localOdb().typed(pin.SHA); !ok {
			h.local.Close()
			h.local = nil
		}
		return
	}
}

// readThinUpstream reads a bucket's thin-fork marker; nil when the bucket is not a thin fork.
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

// ThinUpstreamURL returns the upstream a bucket is thin against, "" when the bucket carries no marker.
func ThinUpstreamURL(client *Client, prefix string) (string, error) {
	doc, err := readThinUpstream(client, prefix)
	if err != nil || doc == nil {
		return "", err
	}
	return doc.URL, nil
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

// ErrThinBucket is what the site surface refuses a thin fork bucket with.
var ErrThinBucket = errors.New("thin fork bucket: its history is incomplete without upstream, so no static site is published; detach it with `gitsocial push --full`")

// PushFull detaches a bucket from its thin relationship: upload what the bucket lacks, restore the ref advertisement, delete the marker. workdir's refs must cover the bucket's tips.
func PushFull(remoteURL string, env HelperEnv, workdir string, progress Progress) error {
	client, prefix, capability, err := ClientForRemote(remoteURL, env)
	if err != nil {
		return err
	}
	doc, err := readThinUpstream(client, prefix)
	if err != nil || doc == nil {
		return err
	}
	refs, err := ReadRemoteRefs(client, prefix)
	if err != nil {
		return fmt.Errorf("read refs: %w", err)
	}
	h := &remoteHelper{client: client, prefix: prefix, capability: capability, workdir: workdir, fetched: map[string]bool{}, progress: progress}
	missing, err := h.missingBucketObjects(refs)
	if err != nil {
		return err
	}
	// Loose, not packed: a one-time repair the next sealing round packs.
	if err := h.uploadObjects(missing); err != nil {
		return fmt.Errorf("upload missing objects: %w", err)
	}
	src := NewLocalCommitSource("", workdir)
	defer src.Close()
	if err := writeDumbTransportInfo(client, prefix, src, refs, false); err != nil {
		return err
	}
	if err := client.Delete(prefix + thinUpstreamKey); err != nil {
		return fmt.Errorf("delete %s: %w", thinUpstreamKey, err)
	}
	return nil
}

// missingBucketObjects lists the objects the bucket's own ref tips reach but the bucket does not carry.
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
	// rev-list --objects peels annotated tags, so carry the tag objects themselves.
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

// bucketObjectInventory is the set of object shas a bucket carries, loose keys plus every object its packs index.
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

// ThinUpstream reports the upstream a bucket is thin against and how many tips its last push pinned, from the bucket key rather than per-clone config.
func ThinUpstream(remoteURL string, env HelperEnv) (url string, pins int, err error) {
	client, prefix, _, err := ClientForRemote(remoteURL, env)
	if err != nil {
		return "", 0, err
	}
	doc, err := readThinUpstream(client, prefix)
	if err != nil || doc == nil {
		return "", 0, err
	}
	return doc.URL, len(doc.Pins), nil
}

// gitOutputIn runs a git command in dir ("" is the repo GIT_DIR names) and returns trimmed stdout.
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

// git runs a git command in the helper's repo: the GIT_DIR git handed it, or an explicit workdir.
func (h *remoteHelper) git(args ...string) (string, error) {
	return gitOutputIn(h.workdir, args...)
}

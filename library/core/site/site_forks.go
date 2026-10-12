// site_forks.go - the fork pull request area: one pack per open fork head and the manifest that lists them
package site

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/gitsocial-org/gitsocial/library/core/objstore"
)

// ForkPull is one open pull request of a registered fork: the fork URL, the fork bare repository, the head sha or "", the review commit shas.
type ForkPull struct {
	Repo, Dir, Head string
	Commits         []string
}

// ForkPullSource returns the open fork pull requests of the workspace; a nil source has none.
type ForkPullSource func() ([]ForkPull, error)

// ForkPullArea reports the fork pull request area after a pass: the pull requests it lists and the bytes of the packs the manifest names.
type ForkPullArea struct {
	Count int   `json:"count"`
	Bytes int64 `json:"bytes"`
}

const (
	// siteForksPrefix holds everything of the fork pull requests, and nothing else in the bucket changes for them.
	siteForksPrefix = ".gitsocial/forks/"
	// siteForksManifestKey lists the open fork pull requests and the pack of each head; written last.
	siteForksManifestKey = siteForksPrefix + "manifest.json"
	// siteForksPackPrefix holds one immutable pack per open head, named as git names it.
	siteForksPackPrefix = siteForksPrefix + "pack/"
	// siteForksVersion is the manifest schema version; another version reads as absent.
	siteForksVersion = 1
	// siteForkPackCap is the pack size over which a head is not written and its entry says tooLarge.
	siteForkPackCap = 32 << 20
)

// errForkPackTooLarge reports a pack over siteForkPackCap.
var errForkPackTooLarge = errors.New("pack over the cap")

// siteForkManifest is the area's listing: the entries of each open fork pull request and the bytes of the packs it names.
type siteForkManifest struct {
	Version int             `json:"version"`
	Bytes   int64           `json:"bytes"`
	Items   []siteForkEntry `json:"items"`
}

// siteForkEntry is a shard entry plus the fork URL; the canonical alone carries the head with its pack, or pending, or tooLarge.
type siteForkEntry struct {
	siteMetaEntry
	Repo      string `json:"repo"`
	Head      string `json:"head,omitempty"`
	Pack      string `json:"pack,omitempty"`
	PackBytes int64  `json:"packBytes,omitempty"`
	Pending   bool   `json:"pending,omitempty"`
	TooLarge  bool   `json:"tooLarge,omitempty"`
}

// maintainSiteForks runs the fork pull request step and reports a failure as one note, since the area never fails a push.
func maintainSiteForks(client *objstore.Client, prefix string, refs map[string]string, forks ForkPullSource, note func(string)) *ForkPullArea {
	area, err := writeSiteForks(client, prefix, refs, forks)
	if err != nil {
		note("fork pull requests: " + err.Error())
		return nil
	}
	return area
}

// writeSiteForks writes the pack of each fetched head the manifest does not already name, then the manifest, then sweeps the packs the manifest does not name.
func writeSiteForks(client *objstore.Client, prefix string, refs map[string]string, forks ForkPullSource) (*ForkPullArea, error) {
	var pulls []ForkPull
	if forks != nil {
		var err error
		if pulls, err = forks(); err != nil {
			return nil, err
		}
	}
	sources := map[string]*objstore.LocalCommitSource{}
	defer func() {
		for _, src := range sources {
			src.Close()
		}
	}()
	manifest := siteForkManifest{Version: siteForksVersion, Items: []siteForkEntry{}}
	named := map[string]bool{}
	area := &ForkPullArea{}
	prior := priorForkPacks(client, prefix)
	for _, pull := range pulls {
		src, ok := sources[pull.Dir]
		if !ok {
			src = objstore.NewLocalCommitSource(pull.Dir, "")
			sources[pull.Dir] = src
		}
		entries := forkPullEntries(src, pull)
		if len(entries) == 0 {
			continue
		}
		pull.Commits = make([]string, len(entries))
		for i, e := range entries {
			pull.Commits[i] = e.SHA
		}
		canonical := &entries[0]
		if pull.Head == "" {
			canonical.Pending = true
		} else {
			canonical.Head = pull.Head
			name, size := reuseForkPack(client, prefix, prior, pull.Head)
			var err error
			if name == "" {
				name, size, err = putForkPack(client, prefix, src, refs, pull)
			}
			switch {
			case errors.Is(err, errForkPackTooLarge):
				canonical.TooLarge = true
			case err != nil:
				return nil, err
			default:
				canonical.Pack, canonical.PackBytes = name, size
				if !named[name] {
					manifest.Bytes += size
				}
				named[name] = true
			}
		}
		manifest.Items = append(manifest.Items, entries...)
		area.Count++
	}
	data, err := objstore.CompressJSON(manifest, objstore.BrotliQualityFull)
	if err != nil {
		return nil, err
	}
	if err := objstore.PutCompressed(client, prefix+siteForksManifestKey, data, ""); err != nil {
		return nil, err
	}
	area.Bytes = manifest.Bytes
	return area, sweepForkPacks(client, prefix, named)
}

// priorForkPacks maps each head the current manifest lists to its pack entry, so an unchanged head is not packed again.
func priorForkPacks(client *objstore.Client, prefix string) map[string]siteForkEntry {
	var m siteForkManifest
	found, err := objstore.ReadCompressedJSON(client, prefix+siteForksManifestKey, &m)
	out := map[string]siteForkEntry{}
	if err != nil || !found || m.Version != siteForksVersion {
		return out
	}
	for _, e := range m.Items {
		if e.Head != "" && e.Pack != "" {
			out[e.Head] = e
		}
	}
	return out
}

// reuseForkPack returns the prior pack of a head when the bucket still has it, else "".
func reuseForkPack(client *objstore.Client, prefix string, prior map[string]siteForkEntry, head string) (string, int64) {
	e, ok := prior[head]
	if !ok || !bucketHasForkPack(client, prefix, e.Pack) {
		return "", 0
	}
	return e.Pack, e.PackBytes
}

// bucketHasForkPack reports whether the area holds the named pack.
func bucketHasForkPack(client *objstore.Client, prefix, name string) bool {
	_, _, err := client.HeadObject(prefix + siteForksPackPrefix + name + ".pack")
	return err == nil
}

// forkPullEntries reads the review commits of one pull request from the fork repository under their full shas; a canonical it cannot read drops the pull request.
func forkPullEntries(src *objstore.LocalCommitSource, pull ForkPull) []siteForkEntry {
	entries := make([]siteForkEntry, 0, len(pull.Commits))
	for _, name := range pull.Commits {
		sha, objType, ok := src.Resolve(name)
		body, found := src.Commit(sha)
		if !ok || objType != "commit" || !found {
			if len(entries) == 0 {
				return nil
			}
			continue
		}
		c, err := parseBucketCommit(sha, body)
		if err != nil {
			continue
		}
		entries = append(entries, siteForkEntry{siteMetaEntry: metaOf(c.item), Repo: pull.Repo})
	}
	return entries
}

// gitError adds the first stderr line of a failed git command to err, which names what git refused.
func gitError(err error) error {
	var exit *exec.ExitError
	if errors.As(err, &exit) && len(exit.Stderr) > 0 {
		line, _, _ := strings.Cut(strings.TrimSpace(string(exit.Stderr)), "\n")
		return fmt.Errorf("%w: %s", err, line)
	}
	return err
}

// putForkPack builds the pack of one head and uploads it unless the bucket has it; it returns the pack name and size.
func putForkPack(client *objstore.Client, prefix string, src *objstore.LocalCommitSource, refs map[string]string, pull ForkPull) (string, int64, error) {
	name, pack, idx, err := buildForkPack(src, refs, pull)
	if err != nil {
		return "", 0, err
	}
	if len(pack) > siteForkPackCap {
		return "", 0, errForkPackTooLarge
	}
	packKey := prefix + siteForksPackPrefix + name + ".pack"
	if bucketHasForkPack(client, prefix, name) {
		return name, int64(len(pack)), nil
	}
	// The index goes first, so a pack the bucket has is a pack it can read.
	for _, part := range []struct {
		key  string
		data []byte
	}{{prefix + siteForksPackPrefix + name + ".idx", idx}, {packKey, pack}} {
		headers := map[string]string{"Content-Type": "application/octet-stream", "Cache-Control": siteCacheControl(part.key)}
		if err := client.PutWithHeaders(part.key, part.data, headers); err != nil {
			return "", 0, fmt.Errorf("upload %s: %w", part.key, err)
		}
	}
	return name, int64(len(pack)), nil
}

// buildForkPack packs the objects reachable from the head and from no bucket tip, plus the review commits, with offset deltas inside the pack only.
func buildForkPack(src *objstore.LocalCommitSource, refs map[string]string, pull ForkPull) (name string, pack, idx []byte, err error) {
	args := []string{"rev-list", "--objects", pull.Head, "--not"}
	out, err := siteGitCommand(src, append(args, localTips(src, refs)...)...).Output()
	if err != nil {
		return "", nil, nil, fmt.Errorf("rev-list %s: %w", pull.Head, gitError(err))
	}
	objects := strings.TrimSpace(string(out)) + "\n" + strings.Join(pull.Commits, "\n") + "\n"
	dir, err := os.MkdirTemp("", "gitsocial-fork-pack-")
	if err != nil {
		return "", nil, nil, fmt.Errorf("pack temp dir: %w", err)
	}
	defer os.RemoveAll(dir)
	cmd := siteGitCommand(src, "-c", "pack.indexVersion=2", "-c", "pack.packSizeLimit=0", "pack-objects", "-q", "--delta-base-offset", filepath.Join(dir, "pack"))
	cmd.Stdin = strings.NewReader(objects)
	if _, err := cmd.Output(); err != nil {
		return "", nil, nil, fmt.Errorf("pack-objects %s: %w", pull.Head, gitError(err))
	}
	written, err := filepath.Glob(filepath.Join(dir, "pack-*.pack"))
	if err != nil || len(written) != 1 {
		return "", nil, nil, fmt.Errorf("pack-objects %s: %d packfiles written, want one", pull.Head, len(written))
	}
	name = strings.TrimSuffix(filepath.Base(written[0]), ".pack")
	if pack, err = os.ReadFile(written[0]); err != nil {
		return "", nil, nil, fmt.Errorf("read packfile: %w", err)
	}
	if idx, err = os.ReadFile(filepath.Join(dir, name+".idx")); err != nil {
		return "", nil, nil, fmt.Errorf("read pack index: %w", err)
	}
	return name, pack, idx, nil
}

// localTips returns the bucket ref tips the fork repository can read, so a tip another clone pushed does not fail the pack.
func localTips(src *objstore.LocalCommitSource, refs map[string]string) []string {
	var tips []string
	for _, sha := range refs {
		if _, _, ok := src.Resolve(sha); ok && len(sha) == 40 {
			tips = append(tips, sha)
		}
	}
	return tips
}

// sweepForkPacks deletes every pack under the area that the manifest just written does not name.
func sweepForkPacks(client *objstore.Client, prefix string, named map[string]bool) error {
	keys, err := client.List(prefix + siteForksPackPrefix)
	if err != nil {
		return fmt.Errorf("list fork packs: %w", err)
	}
	for _, key := range keys {
		base := key[strings.LastIndex(key, "/")+1:]
		name := strings.TrimSuffix(strings.TrimSuffix(base, ".pack"), ".idx")
		if named[name] {
			continue
		}
		if err := client.Delete(key); err != nil {
			return fmt.Errorf("delete %s: %w", key, err)
		}
	}
	return nil
}

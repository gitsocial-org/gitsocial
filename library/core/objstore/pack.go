// pack.go - packfiles on the push path: the two packs, the .idx parser, and the pack map a reader resolves a commit through
//
// A push packs its whole delta or leaves it all loose: git's dumb walker falls
// back to a pack only on a loose 404, so no object may exist as both.
package objstore

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

const (
	// packKeyPrefix is the bucket namespace of packfiles and their indexes, the
	// layout stock git's dumb walker expects next to objects/info/packs.
	packKeyPrefix = "objects/pack/"
	// packMapKeyPrefix is the pack map's namespace: one shard per two-hex sha prefix, next to the ref-mode marker rather than the site artifacts.
	packMapKeyPrefix = ".gitsocial/packmap/"
	// packMapVersion is the pack map shard schema version; another version reads as absent.
	packMapVersion = 1
	// defaultPackThreshold is the delta size at which a push uploads packfiles instead of loose objects.
	defaultPackThreshold = 1000
	// maxPackUploadBytes caps a single pack upload, since the client PUTs a pack in one request.
	maxPackUploadBytes = 4 << 30
)

// resolvePackThreshold returns the object count at which a push packs, honoring GITSOCIAL_S3_PACK_THRESHOLD.
func resolvePackThreshold() int {
	return envInt("GITSOCIAL_S3_PACK_THRESHOLD", defaultPackThreshold)
}

// packMapEntry locates one object inside a pack: its sha and the byte range of its entry.
type packMapEntry struct {
	sha    string
	offset int64
	size   int64
}

// builtPack is one packfile ready to upload: its name, bytes, object count, and the ranges the pack map publishes.
type builtPack struct {
	name    string
	pack    []byte
	idx     []byte
	objects int
	entries []packMapEntry
}

// packMapDoc is one pack map shard: its packs, and sha to [packIndex, offset, size] for each commit or tag under its prefix.
type packMapDoc struct {
	Version int                `json:"version"`
	Packs   []string           `json:"packs"`
	Offsets map[string][]int64 `json:"offsets"`
}

// classifyObjects splits a sha list in one cat-file pass: commits and tags, trees and blobs, and what the local odb lacks.
func classifyObjects(shas []string) (commitLike, content, missing []string, err error) {
	cmd := exec.Command("git", "cat-file", "--batch-check")
	cmd.Stdin = strings.NewReader(strings.Join(shas, "\n") + "\n")
	out, err := cmd.Output()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("git cat-file --batch-check: %w", err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		fields := strings.Fields(line)
		switch {
		case len(fields) == 2 && fields[1] == "missing":
			missing = append(missing, fields[0])
		case len(fields) != 3:
			return nil, nil, nil, fmt.Errorf("git cat-file --batch-check: unexpected response %q", line)
		case fields[1] == "commit" || fields[1] == "tag":
			commitLike = append(commitLike, fields[0])
		default:
			content = append(content, fields[0])
		}
	}
	return commitLike, content, missing, nil
}

// buildDeltaPacks builds the two packs a packed write produces; a sha the local odb lacks is an error, since dropping one would publish a broken pack.
func buildDeltaPacks(shas []string) ([]*builtPack, error) {
	commitLike, content, missing, err := classifyObjects(shas)
	if err != nil {
		return nil, err
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("git cat-file --batch-check: %d object(s) missing locally, starting with %s", len(missing), missing[0])
	}
	commitPack, err := buildPack(commitLike, true, true)
	if err != nil {
		return nil, err
	}
	contentPack, err := buildPack(content, false, false)
	if err != nil {
		return nil, err
	}
	var packs []*builtPack
	for _, built := range []*builtPack{commitPack, contentPack} {
		if built != nil {
			packs = append(packs, built)
		}
	}
	return packs, nil
}

// buildPack runs git pack-objects over a sha list and reads the pack, its index and its byte ranges back; noDelta is what makes a commit one self-contained stream.
func buildPack(shas []string, noDelta, withEntries bool) (*builtPack, error) {
	if len(shas) == 0 {
		return nil, nil
	}
	dir, err := os.MkdirTemp("", "gitsocial-pack-")
	if err != nil {
		return nil, fmt.Errorf("pack temp dir: %w", err)
	}
	defer os.RemoveAll(dir)
	// Pin the two settings that change an artifact's shape, since the command line outranks every config file.
	args := []string{"-c", "pack.indexVersion=2", "-c", "pack.packSizeLimit=0", "pack-objects", "-q", "--delta-base-offset"}
	if noDelta {
		args = append(args, "--depth=0")
	}
	cmd := exec.Command("git", append(args, filepath.Join(dir, "pack"))...)
	cmd.Stdin = strings.NewReader(strings.Join(shas, "\n") + "\n")
	cmd.Stderr = os.Stderr
	if _, err := cmd.Output(); err != nil {
		return nil, fmt.Errorf("git pack-objects: %w", err)
	}
	// Take the name from the file git wrote, so the uploaded key and the objects/info/packs line agree.
	written, err := filepath.Glob(filepath.Join(dir, "pack-*.pack"))
	if err != nil || len(written) != 1 {
		return nil, fmt.Errorf("git pack-objects: expected one packfile in %s, found %d", dir, len(written))
	}
	name := strings.TrimSuffix(filepath.Base(written[0]), ".pack")
	pack, err := os.ReadFile(written[0])
	if err != nil {
		return nil, fmt.Errorf("read packfile: %w", err)
	}
	idx, err := os.ReadFile(filepath.Join(dir, name+".idx"))
	if err != nil {
		return nil, fmt.Errorf("read pack index: %w", err)
	}
	built := &builtPack{name: name, pack: pack, idx: idx, objects: len(shas)}
	if withEntries {
		if built.entries, err = packEntryRanges(idx, int64(len(pack))); err != nil {
			return nil, err
		}
	}
	return built, nil
}

// packIdxEntry is one object's position in a pack index: its sha and offset.
type packIdxEntry struct {
	sha    string
	offset int64
}

// parsePackIdx reads a v2 pack index and returns every entry in sha order.
func parsePackIdx(idx []byte) ([]packIdxEntry, error) {
	const header = 8 + 256*4
	if len(idx) < header+40 || string(idx[:4]) != "\xfftOc" || binary.BigEndian.Uint32(idx[4:8]) != 2 {
		return nil, fmt.Errorf("pack index: not a v2 index")
	}
	count := int(binary.BigEndian.Uint32(idx[header-4 : header]))
	shaStart := header
	offStart := shaStart + count*20 + count*4
	bigStart := offStart + count*4
	if len(idx) < bigStart+40 {
		return nil, fmt.Errorf("pack index: truncated (%d objects, %d bytes)", count, len(idx))
	}
	entries := make([]packIdxEntry, count)
	for i := 0; i < count; i++ {
		off := int64(binary.BigEndian.Uint32(idx[offStart+i*4 : offStart+i*4+4]))
		if off&0x80000000 != 0 {
			big := bigStart + int(off&0x7fffffff)*8
			if len(idx) < big+8 {
				return nil, fmt.Errorf("pack index: large-offset table truncated")
			}
			off = int64(binary.BigEndian.Uint64(idx[big : big+8]))
		}
		entries[i] = packIdxEntry{sha: hex.EncodeToString(idx[shaStart+i*20 : shaStart+i*20+20]), offset: off}
	}
	return entries, nil
}

// packEntryRanges turns a pack index into per-object byte ranges; entries are contiguous, so each ends where the next begins.
func packEntryRanges(idx []byte, packSize int64) ([]packMapEntry, error) {
	entries, err := parsePackIdx(idx)
	if err != nil {
		return nil, err
	}
	byOffset := append([]packIdxEntry{}, entries...)
	sort.Slice(byOffset, func(i, j int) bool { return byOffset[i].offset < byOffset[j].offset })
	ranges := make([]packMapEntry, len(byOffset))
	for i, e := range byOffset {
		end := packSize - 20
		if i+1 < len(byOffset) {
			end = byOffset[i+1].offset
		}
		ranges[i] = packMapEntry{sha: e.sha, offset: e.offset, size: end - e.offset}
	}
	return ranges, nil
}

// publishPack PUTs a pack's index and then the pack, then its map entries; the index lands first, so a reader that finds the pack can index it.
func publishPack(client *Client, capability Capability, prefix string, built *builtPack, concurrency int) error {
	for _, part := range []struct {
		suffix string
		body   []byte
	}{{".idx", built.idx}, {".pack", built.pack}} {
		key := prefix + packKeyPrefix + built.name + part.suffix
		if err := client.Put(key, part.body); err != nil {
			return fmt.Errorf("upload %s: %w", built.name+part.suffix, err)
		}
	}
	if len(built.entries) == 0 {
		return nil
	}
	if err := writePackMap(client, capability, prefix, built.name, built.entries, concurrency); err != nil {
		fmt.Fprintf(os.Stderr, "gitsocial s3: pack map for %s: %v (readers fall back to the pack index)\n", built.name, err)
	}
	return nil
}

// packMapShardName is the pack map shard a sha belongs to (its two-hex prefix).
func packMapShardName(sha string) string { return sha[:2] }

// writePackMap merges a pack's object ranges into the sha-prefixed pack map, shard by shard and concurrently.
func writePackMap(client *Client, capability Capability, prefix, packName string, entries []packMapEntry, concurrency int) error {
	byShard := map[string][]packMapEntry{}
	for _, e := range entries {
		name := packMapShardName(e.sha)
		byShard[name] = append(byShard[name], e)
	}
	names := make([]string, 0, len(byShard))
	for name := range byShard {
		names = append(names, name)
	}
	sort.Strings(names)
	return forEachBounded(len(names), concurrency, func(i int) error {
		return writePackMapShard(client, capability, prefix, names[i], packName, byShard[names[i]])
	})
}

// writePackMapShard merges one pack's entries into a shard under compare-and-swap; an object is packed once, so nothing rewrites a lost entry.
func writePackMapShard(client *Client, capability Capability, prefix, shard, packName string, entries []packMapEntry) error {
	return updateCompressedJSON(client, capability, prefix+packMapKeyPrefix+shard+".json", func(doc *packMapDoc, found bool) error {
		if !found || doc.Version != packMapVersion || doc.Offsets == nil {
			*doc = packMapDoc{Offsets: map[string][]int64{}}
		}
		doc.Version = packMapVersion
		packIndex := -1
		for i, name := range doc.Packs {
			if name == packName {
				packIndex = i
			}
		}
		if packIndex < 0 {
			packIndex = len(doc.Packs)
			doc.Packs = append(doc.Packs, packName)
		}
		for _, e := range entries {
			doc.Offsets[e.sha] = []int64{int64(packIndex), e.offset, e.size}
		}
		return nil
	})
}

// readPackMapShard fetches one pack map shard, empty when absent, unreadable, or at another schema version.
func readPackMapShard(client *Client, prefix, shard string) (*packMapDoc, error) {
	var doc packMapDoc
	found, err := ReadCompressedJSON(client, prefix+packMapKeyPrefix+shard+".json", &doc)
	if err != nil {
		return nil, fmt.Errorf("read pack map shard %s: %w", shard, err)
	}
	if !found || doc.Version != packMapVersion || doc.Offsets == nil {
		return &packMapDoc{Version: packMapVersion, Offsets: map[string][]int64{}}, nil
	}
	doc.Version = packMapVersion
	return &doc, nil
}

// ReadPackedObject resolves one object out of the bucket's packfiles through its pack map shard; ok is false when the map has no usable entry.
func ReadPackedObject(client *Client, prefix, sha string) (objType string, body []byte, ok bool, err error) {
	doc, err := readPackMapShard(client, prefix, packMapShardName(sha))
	if err != nil {
		return "", nil, false, err
	}
	at, found := doc.Offsets[sha]
	if !found || len(at) != 3 || at[0] < 0 || at[0] >= int64(len(doc.Packs)) {
		return "", nil, false, nil
	}
	raw, err := client.GetRange(prefix+packKeyPrefix+doc.Packs[at[0]]+".pack", at[1], at[1]+at[2])
	if errors.Is(err, ErrNotFound) {
		return "", nil, false, nil
	}
	if err != nil {
		return "", nil, false, fmt.Errorf("read packed object %s: %w", sha, err)
	}
	objType, body, err = inflatePackEntry(raw)
	if err != nil {
		return "", nil, false, fmt.Errorf("packed object %s: %w", sha, err)
	}
	return objType, body, true, nil
}

// packObjectTypes maps a pack entry's 3-bit type code to the git object type name; 6 and 7 are the delta codes.
var packObjectTypes = map[byte]string{1: "commit", 2: "tree", 3: "blob", 4: "tag"}

// inflatePackEntry decodes one non-delta pack entry from its byte range; a delta entry is an error, since the map indexes only the commits pack.
func inflatePackEntry(raw []byte) (objType string, body []byte, err error) {
	if len(raw) == 0 {
		return "", nil, fmt.Errorf("pack entry: empty range")
	}
	i := 0
	b := raw[i]
	i++
	code := (b >> 4) & 7
	for b&0x80 != 0 {
		if i >= len(raw) {
			return "", nil, fmt.Errorf("pack entry: truncated header")
		}
		b = raw[i]
		i++
	}
	objType, ok := packObjectTypes[code]
	if !ok {
		return "", nil, fmt.Errorf("pack entry: type %d is a delta, but a mapped entry is always whole", code)
	}
	zr, err := zlib.NewReader(bytes.NewReader(raw[i:]))
	if err != nil {
		return "", nil, fmt.Errorf("pack entry: inflate: %w", err)
	}
	defer zr.Close()
	if body, err = io.ReadAll(zr); err != nil {
		return "", nil, fmt.Errorf("pack entry: inflate: %w", err)
	}
	return objType, body, nil
}

// listBucketPacks returns the pack names the bucket carries, from the objects/pack/ listing, so another clone's packs are listed too.
func listBucketPacks(client *Client, prefix string) ([]string, error) {
	keys, err := client.List(prefix + packKeyPrefix)
	if err != nil {
		return nil, fmt.Errorf("list packs: %w", err)
	}
	var names []string
	for _, key := range keys {
		name := strings.TrimPrefix(key, prefix+packKeyPrefix)
		if base, ok := strings.CutSuffix(name, ".pack"); ok && strings.HasPrefix(base, "pack-") {
			names = append(names, base)
		}
	}
	sort.Strings(names)
	return names, nil
}

// parseInfoPacks reads the pack names out of an objects/info/packs body.
func parseInfoPacks(body []byte) []string {
	var names []string
	for _, line := range strings.Split(string(body), "\n") {
		rest, ok := strings.CutPrefix(strings.TrimSpace(line), "P ")
		if !ok {
			continue
		}
		if base, ok := strings.CutSuffix(rest, ".pack"); ok && base != "" {
			names = append(names, base)
		}
	}
	return names
}

// forEachBounded runs fn for indexes 0 to n-1 over a bounded worker pool and returns the first error.
func forEachBounded(n, concurrency int, fn func(i int) error) error {
	if n == 0 {
		return nil
	}
	if concurrency < 1 {
		concurrency = 1
	}
	work := make(chan int)
	var firstErr error
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range work {
				if err := fn(idx); err != nil {
					mu.Lock()
					if firstErr == nil {
						firstErr = err
					}
					mu.Unlock()
				}
			}
		}()
	}
	for i := 0; i < n; i++ {
		work <- i
	}
	close(work)
	wg.Wait()
	return firstErr
}

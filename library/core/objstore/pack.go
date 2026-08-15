// pack.go - packfiles on the push path: two packs split by object type, the
// pack .idx parser, and the pack map the browser reads packed commit bodies
// through.
//
// Git's dumb walker prefers loose objects and only falls back to
// objects/info/packs when a loose fetch 404s, so the same object must never
// exist both loose and packed: a push either packs its whole delta or leaves it
// all loose. Commits and tags pack with --depth=0 (measured 0.5% larger, zero
// deltas) so a reader resolves any commit body from one byte range and never a
// delta chain; trees and blobs pack at git's default depth, where delta
// compression is the whole point.
//
// Git is invoked with os/exec directly for the reason helper_push.go documents:
// the helper runs as a child of git with GIT_DIR in its environment, and
// objstore stays free of a core/git dependency.
package objstore

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
)

const (
	// packKeyPrefix is the bucket namespace of packfiles and their indexes, the
	// layout stock git's dumb walker expects next to objects/info/packs.
	packKeyPrefix = "objects/pack/"
	// packMapKeyPrefix is the bucket namespace of the pack map: one shard per
	// two-hex sha prefix (git's own loose fan-out), mapping a packed commit or
	// tag to the exact byte range of its pack entry. It describes packs rather
	// than the site, so it sits beside the ref-mode marker under .gitsocial/
	// instead of under the site artifacts — a plain s3:// remote with no site
	// still packs, and its reader still needs the offsets.
	packMapKeyPrefix = ".gitsocial/packmap/"
	// packMapVersion is the pack map shard schema version; a reader treats any
	// other version as absent and falls back to the loose object.
	packMapVersion = 1
	// defaultPackThreshold is the delta size at or above which a push uploads
	// packfiles instead of loose objects. Packing a handful of objects would
	// litter the bucket with tiny packs, each costing the dumb walker an extra
	// .idx fetch and a manifest line, while the PUT saving is a rounding error;
	// the win only starts once a push is thousands of round trips long.
	defaultPackThreshold = 1000
	// maxPackUploadBytes caps a single pack upload. The client PUTs a pack as
	// one request (no multipart), and providers reject a single PUT past ~5 GB,
	// so a delta whose pack would exceed this falls back to loose objects rather
	// than failing the push.
	maxPackUploadBytes = 4 << 30
)

// resolvePackThreshold returns the object count at or above which a push packs,
// honoring a positive GITSOCIAL_S3_PACK_THRESHOLD override (the site-test
// fixture lowers it; unset in production).
func resolvePackThreshold() int {
	if v := os.Getenv("GITSOCIAL_S3_PACK_THRESHOLD"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 {
			return n
		}
	}
	return defaultPackThreshold
}

// packMapEntry locates one object inside a pack: its sha and the exact byte
// range of its pack entry, so a reader needs one Range GET and no .idx.
type packMapEntry struct {
	sha    string
	offset int64
	size   int64
}

// builtPack is one packfile ready to upload: git's pack name ("pack-<hash>"),
// the .pack and .idx bytes, the object count it carries (the sealing pass's
// leftover accounting reads it), and the per-object byte ranges the pack map
// publishes (empty for the content pack, whose readers use the .idx).
type builtPack struct {
	name    string
	pack    []byte
	idx     []byte
	objects int
	entries []packMapEntry
}

// packMapDoc is one pack map shard: the packs its entries live in, and
// sha → [packIndex, offset, size] for every packed commit or tag whose sha
// starts with the shard's two-hex prefix.
type packMapDoc struct {
	Version int                `json:"version"`
	Packs   []string           `json:"packs"`
	Offsets map[string][]int64 `json:"offsets"`
}

// classifyObjects splits a sha list with one `git cat-file --batch-check` pass:
// commits and tags (packed without deltas, published in the pack map), trees
// and blobs (packed at default depth), and the shas the local odb does not
// carry. A push errors on any miss; the sealing pass, which classifies whatever
// a bucket happens to hold, simply skips them.
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

// buildDeltaPacks builds the two packs a packed write produces from one sha
// list: commits and tags without deltas (their byte ranges recorded for the
// pack map), trees and blobs at git's default depth. Either half may be empty,
// so the result carries one or two packs. A sha the local odb lacks is an error
// here — silently dropping an object from a pack would publish a broken pack.
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

// buildPack runs `git pack-objects` over a sha list and reads the resulting
// pack, index, and (when withEntries) every object's byte range back. noDelta
// forbids delta compression, which is what makes a commit read one self-
// contained zlib stream at a fixed offset. A nil pack (no error) means the sha
// list was empty.
func buildPack(shas []string, noDelta, withEntries bool) (*builtPack, error) {
	if len(shas) == 0 {
		return nil, nil
	}
	dir, err := os.MkdirTemp("", "gitsocial-pack-")
	if err != nil {
		return nil, fmt.Errorf("pack temp dir: %w", err)
	}
	defer os.RemoveAll(dir)
	// The pusher's own git config must not change the shape of a bucket artifact
	// every other reader depends on: pack.indexVersion=1 writes a v1 index that
	// neither parsePackIdx nor the browser reads, and any pack.packSizeLimit
	// splits the output into several packfiles the single-pack contract below
	// rejects. Both are pinned on the command line, which outranks every config
	// file. The rest (compression, window, depth for the content pack) only
	// changes how well the pack packs, so it is left to the pusher.
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
	// Take the name from the file git actually wrote rather than its stdout, so
	// the uploaded key and the objects/info/packs line can never disagree.
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

// parsePackIdx reads a v2 pack index (magic, 256-entry fanout, sorted shas,
// CRCs, 4-byte offsets with an 8-byte large-offset table) and returns every
// entry in sha order.
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

// packEntryRanges turns a pack index into per-object byte ranges. Pack entries
// are contiguous, so an entry ends where the next one (in offset order) begins,
// and the last ends at the pack's 20-byte trailing checksum — no inflation and
// no `git verify-pack` pass needed to learn a size.
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

// publishPack PUTs a pack's index and then the pack itself under objects/pack/,
// followed by the pack map entries a commits pack carries. Both objects are
// immutable (content-named), so re-uploads are idempotent and the retry is
// free; the index lands first so a reader that discovers the pack can always
// index it.
//
// Only the two object uploads can fail this: the pack map is a read
// optimization over them (a reader with no entry range-reads the .idx instead),
// so a shard that will not write is logged and nothing more. Failing here would
// reject a push whose packfiles are already durable on the bucket, over an index
// that costs a reader nothing but an extra round trip and that the next pack
// published into the same shard rewrites anyway.
func publishPack(client *Client, capability Capability, prefix string, built *builtPack, concurrency int) error {
	for _, part := range []struct {
		suffix string
		body   []byte
	}{{".idx", built.idx}, {".pack", built.pack}} {
		key := prefix + packKeyPrefix + built.name + part.suffix
		if err := putObjectWithRetry(context.TODO(), client, key, part.body); err != nil {
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

// writePackMap records a pack's object ranges into the sha-prefixed pack map,
// merging into whatever earlier packs already published. Shards are read,
// merged, and written concurrently: a cold push touches all 256 of them, and
// serial round trips would dominate the pack upload it follows.
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

// writePackMapShard merges one pack's entries into a single pack map shard,
// under compare-and-swap. A plain write loses a concurrent pusher's entries for
// good: an object is packed once, so nothing ever rewrites the shard it went
// missing from, and every reader of those commits pays the fallback (fetch a
// whole pack index and binary-search it) forever.
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

// readPackMapShard fetches one pack map shard, returning an empty document when
// it is absent, unparseable, or written by another schema version.
func readPackMapShard(client *Client, prefix, shard string) (*packMapDoc, error) {
	var doc packMapDoc
	found, err := readCompressedJSON(client, prefix+packMapKeyPrefix+shard+".json", &doc)
	if err != nil {
		return nil, fmt.Errorf("read pack map shard %s: %w", shard, err)
	}
	if !found || doc.Version != packMapVersion || doc.Offsets == nil {
		return &packMapDoc{Version: packMapVersion, Offsets: map[string][]int64{}}, nil
	}
	doc.Version = packMapVersion
	return &doc, nil
}

// packObjectTypes maps a pack entry's 3-bit type code to the git object type
// name (whole objects only; 6/7 are the delta codes).
var packObjectTypes = map[byte]string{1: "commit", 2: "tree", 3: "blob", 4: "tag"}

// inflatePackEntry decodes one NON-DELTA pack entry out of its exact byte range
// (a pack map range): the type/size varint header, then one self-contained zlib
// stream — the shape the commits pack guarantees, since it is written with
// --depth=0. A delta entry is an error here, not a fallback: the pack map only
// indexes the commits pack.
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

// listBucketPacks returns the pack names ("pack-<hash>") the bucket carries,
// sorted, derived from the objects/pack/ listing rather than push-side state so
// packs another clone uploaded are listed too.
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

// parseInfoPacks reads the pack names out of an objects/info/packs body
// ("P <name>.pack" lines, git's update-server-info format).
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

// forEachBounded runs fn for indexes 0..n-1 over a bounded worker pool and
// returns the first error. Used by the pack map's per-shard read-merge-write,
// where each shard is an independent pair of round trips.
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

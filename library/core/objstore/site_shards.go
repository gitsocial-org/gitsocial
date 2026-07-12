// site_shards.go - generic append-only, ingestion-order shard/manifest layer
// shared by the two static-site corpora (the bodies search corpus and the
// metadata items index).
//
// Both corpora split a per-extension commit list into fixed oldest-first groups
// of shardBodyCount: full groups seal into immutable content-hash-keyed shards
// (compressed once, then served immutable), the trailing partial group is the
// head (no-cache, recompressed on every push), and a manifest lists the ordered
// shards plus the head. Because older commits never change under append, a
// sealed shard's membership — and thus its content hash / key — is stable across
// incremental updates and full rebuilds, so old shards are byte-identical and
// never rewritten (the content hash is derivable WITHOUT compressing, so an
// already-present shard is skipped before any expensive encode).
//
// The layer is parameterized over an entry type (a siteBodyEntry or a
// siteMetaEntry) via shardEntry.entrySHA and a small shardCorpus descriptor
// (dir/key funcs + the per-doc marshal closure), so the boundary math, content
// hash, seal/skip-existing, and manifest struct are written once for both.

package objstore

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
)

// errNoManifestForBackfill signals a BACKFILL that raced a manifest reset (the
// manifest vanished between classify and the re-read): the caller drops to a
// fresh bootstrap on the next push rather than prepend onto nothing.
var errNoManifestForBackfill = errors.New("backfill: manifest absent at write time")

// shardEntry is one indexed commit in either corpus; entrySHA returns the sha
// the content hash and the shard's newest-member (endTip) are computed over.
type shardEntry interface {
	entrySHA() string
}

// siteShardManifest is the .gitsocial/site/<corpus>/<ext>/manifest.json
// document shared by both corpora: the ordered sealed shards, the unsealed
// head, the branch tip at write time, the total compressed bytes (the reader's
// full-search download size for bodies, threaded into the items manifest as
// bodiesBytes), a `complete` flag (true once the walk reached the branch root,
// false while a bootstrap is still backfilling older history), and `bodiesBytes`
// (set on the items manifest to the bodies corpus's TotalBytes; 0 on the bodies
// manifest itself).
type siteShardManifest struct {
	Version     int              `json:"version"`
	Tip         string           `json:"tip"`
	TotalBytes  int              `json:"totalBytes"`
	Complete    bool             `json:"complete"`
	BodiesBytes int              `json:"bodiesBytes,omitempty"`
	Shards      []siteShardEntry `json:"shards"`
	Head        siteShardHead    `json:"head"`
}

// siteShardEntry describes one sealed shard: its object-key basename, content
// hash, member count, compressed size, and its newest member sha (endTip).
type siteShardEntry struct {
	Key    string `json:"key"`
	Hash   string `json:"hash"`
	Count  int    `json:"count"`
	Bytes  int    `json:"bytes"`
	EndTip string `json:"endTip"`
}

// siteShardHead describes the unsealed head: its member count and compressed size.
type siteShardHead struct {
	Count int `json:"count"`
	Bytes int `json:"bytes"`
}

// shardCorpus describes one corpus to the generic layer: how to name its keys
// and how to marshal a shard/head document from a tip + entries. label
// ("items"/"bodies") tags this corpus's shard-upload progress so the two
// corpora's identical shard counts don't read as duplicate work.
type shardCorpus[E shardEntry] struct {
	label       string
	manifestKey func(ext string) string
	headKey     func(ext string) string
	shardName   func(hash string) string
	shardKey    func(ext, hash string) string
	dir         func(ext string) string
	version     func(ext string) int
	marshalDoc  func(ext, tip string, entries []E) any
}

// sealedFrontier returns a manifest's newest sealed member sha (the newest
// sealed shard's endTip) and whether any sealed shard exists. It is the boundary
// a REPAIR tail-walk stops at: everything newer than it is the (re-derivable)
// head; everything at or below it is already in immutable sealed shards.
func (m *siteShardManifest) sealedFrontier() (string, bool) {
	if m == nil || len(m.Shards) == 0 {
		return "", false
	}
	return m.Shards[len(m.Shards)-1].EndTip, true
}

// shardObjectName is a sealed shard's object-key basename for a content hash.
// Both corpora share it (the basename is identical; only the parent dir differs).
func shardObjectName(hash string) string {
	return "shard-" + hash + ".json"
}

// shardContentHash returns the first 12 hex of sha256 over the member commit
// shas joined oldest-first — the stable, compression-free key of a sealed shard.
// A non-v4 schema version salts the hash so a schema tick (e.g. the code
// corpus's v5 parents) yields NEW shard keys and the skip-existing seal
// re-uploads rather than trusting a stale-schema object; v4 stays unsalted so
// every already-sealed v4 shard's key (gitmsg items, bodies) is unchanged.
func shardContentHash[E shardEntry](version int, group []E) string {
	h := sha256.New()
	if version != siteItemsVersion {
		fmt.Fprintf(h, "v%d\n", version)
	}
	for _, e := range group {
		h.Write([]byte(e.entrySHA()))
		h.Write([]byte{'\n'})
	}
	return hex.EncodeToString(h.Sum(nil))[:12]
}

// reverseGeneric returns a reversed copy (newest-first walk order <-> oldest-first
// ingestion order).
func reverseGeneric[E shardEntry](in []E) []E {
	out := make([]E, len(in))
	for i, e := range in {
		out[len(in)-1-i] = e
	}
	return out
}

// putShardDoc uploads one corpus document (a sealed shard or the head) and
// returns its compressed size.
func putShardDoc[E shardEntry](client *Client, corpus shardCorpus[E], ext, key, tip string, entries []E, quality int) (int, error) {
	comp, err := compressJSON(corpus.marshalDoc(ext, tip, entries), quality)
	if err != nil {
		return 0, err
	}
	if err := putCompressed(client, key, comp); err != nil {
		return 0, err
	}
	return len(comp), nil
}

// sealShardGeneric writes one sealed shard when it is not already present on the
// bucket (its content hash makes an existing object byte-identical), returning
// the shard's manifest entry and its compressed size. sizeByHash carries the
// sizes the current manifest already recorded (hash → bytes): when a shard is
// already present but the endpoint's HEAD omits Content-Length, the recorded
// size is used rather than re-downloading the immutable shard. A nil map or a
// genuinely-unknown shard yields 0 bytes (harmless: the manifest's TotalBytes,
// the reader's full-search size hint, is then a slight under-count until the next
// push resees the size, and no shard is ever fetched to learn its size).
func sealShardGeneric[E shardEntry](client *Client, corpus shardCorpus[E], prefix, ext string, group []E, sizeByHash map[string]int) (siteShardEntry, int, error) {
	hash := shardContentHash(corpus.version(ext), group)
	key := prefix + corpus.shardKey(ext, hash)
	endTip := group[len(group)-1].entrySHA()
	size, exists, err := objectSize(client, key)
	if err != nil {
		return siteShardEntry{}, 0, err
	}
	if !exists {
		if size, err = putShardDoc(client, corpus, ext, key, endTip, group, brotliQualityShard); err != nil {
			return siteShardEntry{}, 0, err
		}
	} else if size == 0 {
		size = sizeByHash[hash]
	}
	return siteShardEntry{Key: corpus.shardName(hash), Hash: hash, Count: len(group), Bytes: size, EndTip: endTip}, size, nil
}

// shardSizes builds the hash → recorded-bytes map from a manifest's sealed
// shards, so a re-seal of an already-present shard can reuse its size instead of
// probing (or downloading) the immutable object. nil-safe.
func shardSizes(m *siteShardManifest) map[string]int {
	if m == nil {
		return nil
	}
	sizes := make(map[string]int, len(m.Shards))
	for _, s := range m.Shards {
		sizes[s.Hash] = s.Bytes
	}
	return sizes
}

// putShardManifest uploads one corpus's manifest.
func putShardManifest[E shardEntry](client *Client, corpus shardCorpus[E], prefix, ext string, m *siteShardManifest) error {
	comp, err := compressJSON(m, brotliQualityFull)
	if err != nil {
		return err
	}
	return putCompressed(client, prefix+corpus.manifestKey(ext), comp)
}

// shardPlan is a corpus's sealed shard set plus the resulting unsealed head,
// computed (and the shards sealed) BEFORE any head/manifest is written. Splitting
// the write into plan → head → manifest lets the caller interleave the two
// corpora and land the manifests last (see putSiteArtifacts / putGapArtifacts's
// pinned write order), so an interruption never leaves a manifest ahead of its
// shards.
type shardPlan[E shardEntry] struct {
	shards      []siteShardEntry // sealed, immutable, already on the bucket
	head        []E              // the trailing unsealed group (oldest-first)
	sealedBytes int              // total compressed bytes across the sealed shards
	headBytes   int              // the head document's compressed size (set by putHead)
}

// planSharded seals every full oldest-first group of a full (re)build and
// returns the plan (sealed shards + trailing head). Shards already present are
// skipped (content-hash keyed), so a rebuild recompresses nothing.
func planSharded[E shardEntry](client *Client, corpus shardCorpus[E], prefix, ext string, entries []E, sizeByHash map[string]int, sp *siteProgress) (shardPlan[E], error) {
	oldest := reverseGeneric(entries)
	numSealed := len(oldest) / shardBodyCount
	plan := shardPlan[E]{shards: make([]siteShardEntry, 0, numSealed)}
	for i := 0; i < numSealed; i++ {
		shard, size, err := sealShardGeneric(client, corpus, prefix, ext, oldest[i*shardBodyCount:(i+1)*shardBodyCount], sizeByHash)
		if err != nil {
			return shardPlan[E]{}, err
		}
		plan.shards = append(plan.shards, shard)
		plan.sealedBytes += size
		sp.shards(corpus.label, i+1, numSealed)
	}
	plan.head = oldest[numSealed*shardBodyCount:]
	return plan, nil
}

// planAppend seals any groups a newest-first gap fills on top of the existing
// head (prior sealed shards untouched) and returns the plan carrying the prior +
// new sealed shards and the trailing head.
func planAppend[E shardEntry](client *Client, corpus shardCorpus[E], prefix, ext string, gap, headItems []E, manifest *siteShardManifest, sp *siteProgress) (shardPlan[E], error) {
	newHead := append(append([]E{}, headItems...), reverseGeneric(gap)...)
	sizeByHash := shardSizes(manifest)
	plan := shardPlan[E]{shards: append([]siteShardEntry{}, manifest.Shards...)}
	for _, s := range plan.shards {
		plan.sealedBytes += s.Bytes
	}
	toSeal := len(newHead) / shardBodyCount
	for done := 0; len(newHead) >= shardBodyCount; done++ {
		shard, size, err := sealShardGeneric(client, corpus, prefix, ext, newHead[:shardBodyCount], sizeByHash)
		if err != nil {
			return shardPlan[E]{}, err
		}
		plan.shards = append(plan.shards, shard)
		plan.sealedBytes += size
		newHead = newHead[shardBodyCount:]
		sp.shards(corpus.label, done+1, toSeal)
	}
	plan.head = newHead
	return plan, nil
}

// planTail rebuilds a corpus that keeps a prefix of already-sealed shards and
// re-derives everything newer than the sealed frontier from a freshly-walked
// tail (the commits above the newest sealed shard, newest-first). Used by REPAIR:
// the kept shards are content-hash keyed so their boundaries are stable, and the
// tail seals any newly-full groups on those same boundaries (skip-existing), so a
// head-count mismatch or a partial/skewed corpus is rebuilt without re-walking or
// recompressing the sealed history. keptShards is the frontier prefix (empty for
// a bootstrap-from-root); tail is oldest-first-above-the-frontier reversed to
// newest-first by the caller's walk, so it is reversed back here.
func planTail[E shardEntry](client *Client, corpus shardCorpus[E], prefix, ext string, keptShards []siteShardEntry, tail []E, sp *siteProgress) (shardPlan[E], error) {
	newHead := reverseGeneric(tail)
	sizeByHash := shardSizes(&siteShardManifest{Shards: keptShards})
	plan := shardPlan[E]{shards: append([]siteShardEntry{}, keptShards...)}
	for _, s := range plan.shards {
		plan.sealedBytes += s.Bytes
	}
	toSeal := len(newHead) / shardBodyCount
	for done := 0; len(newHead) >= shardBodyCount; done++ {
		shard, size, err := sealShardGeneric(client, corpus, prefix, ext, newHead[:shardBodyCount], sizeByHash)
		if err != nil {
			return shardPlan[E]{}, err
		}
		plan.shards = append(plan.shards, shard)
		plan.sealedBytes += size
		newHead = newHead[shardBodyCount:]
		sp.shards(corpus.label, done+1, toSeal)
	}
	plan.head = newHead
	return plan, nil
}

// sealSegment seals a walked OLDER segment (newest-first, up to one push's
// budget) fully into shards and returns them oldest-first plus their total
// compressed bytes. A backfill segment carries no head — the head stays owned by
// the newest prefix (the BOOTSTRAP/APPEND end) — so the whole segment seals: full
// oldest-first shardBodyCount groups, then the trailing remainder as its own
// (shorter) shard. Sizes may therefore be uneven (a segment length need not
// divide shardBodyCount), which the reader treats opaquely; content-hash keying
// makes every seal skip-existing on retry.
func sealSegment[E shardEntry](client *Client, corpus shardCorpus[E], prefix, ext string, segment []E, sizeByHash map[string]int, sp *siteProgress) ([]siteShardEntry, int, error) {
	oldest := reverseGeneric(segment)
	toSeal := (len(oldest) + shardBodyCount - 1) / shardBodyCount
	var shards []siteShardEntry
	var total int
	for len(oldest) > 0 {
		n := shardBodyCount
		if n > len(oldest) {
			n = len(oldest)
		}
		shard, size, err := sealShardGeneric(client, corpus, prefix, ext, oldest[:n], sizeByHash)
		if err != nil {
			return nil, 0, err
		}
		shards = append(shards, shard)
		total += size
		oldest = oldest[n:]
		sp.shards(corpus.label, len(shards), toSeal)
	}
	return shards, total, nil
}

// prependSegmentPlan re-reads the current manifest (last-writer-wins on a shared
// key: APPEND and BACKFILL both edit disjoint ends of the same manifest, so
// re-reading immediately before writing keeps the newest APPEND head/tip while
// this BACKFILL prepends its older shards), seals the older segment, and returns
// a plan whose shards are [segment shards ++ current shards] and whose head is
// the current head. It is idempotent against a torn state where one corpus's
// manifest already lists the segment (an interruption between the two manifest
// writes): segment shards already present in current (content-hash keyed) are
// dropped from current before the prepend, so re-running never duplicates them.
// The manifest write that follows records the prepended-older prefix; a clobber
// self-heals on the next push.
func prependSegmentPlan[E shardEntry](client *Client, corpus shardCorpus[E], prefix, ext string, segment, head []E, sp *siteProgress) (shardPlan[E], string, error) {
	current, err := readShardManifest(client, corpus, prefix, ext)
	if err != nil {
		return shardPlan[E]{}, "", err
	}
	if current == nil {
		return shardPlan[E]{}, "", errNoManifestForBackfill
	}
	older, olderBytes, err := sealSegment(client, corpus, prefix, ext, segment, shardSizes(current), sp)
	if err != nil {
		return shardPlan[E]{}, "", err
	}
	olderKeys := map[string]bool{}
	for _, s := range older {
		olderKeys[s.Key] = true
	}
	plan := shardPlan[E]{shards: append([]siteShardEntry{}, older...), head: head, sealedBytes: olderBytes, headBytes: current.Head.Bytes}
	for _, s := range current.Shards {
		if olderKeys[s.Key] {
			continue // already prepended by an interrupted prior run
		}
		plan.shards = append(plan.shards, s)
		plan.sealedBytes += s.Bytes
	}
	return plan, current.Tip, nil
}

// putHead writes a plan's head document and records its compressed size on the
// plan (so the manifest write that follows totals shards + head).
func putHead[E shardEntry](client *Client, corpus shardCorpus[E], prefix, ext, tip string, plan *shardPlan[E]) error {
	headBytes, err := putShardDoc(client, corpus, ext, prefix+corpus.headKey(ext), tip, plan.head, brotliQualityFull)
	if err != nil {
		return err
	}
	plan.headBytes = headBytes
	return nil
}

// putManifest assembles and writes a plan's manifest (the corpus's only commit
// point). complete is false while a bootstrap is still backfilling older history
// (a cursor is pending); the reader reads it to know whether "search older items"
// can cover all history or only the bootstrapped prefix. Returns the corpus's new
// total compressed bytes (shards + head).
func putManifest[E shardEntry](client *Client, corpus shardCorpus[E], prefix, ext, tip string, plan shardPlan[E], bodiesBytes int, complete bool) (int, error) {
	total := plan.sealedBytes + plan.headBytes
	m := &siteShardManifest{Version: corpus.version(ext), Tip: tip, TotalBytes: total, Complete: complete, BodiesBytes: bodiesBytes, Shards: plan.shards, Head: siteShardHead{Count: len(plan.head), Bytes: plan.headBytes}}
	if err := putShardManifest(client, corpus, prefix, ext, m); err != nil {
		return 0, err
	}
	return total, nil
}

// readDocItems fetches a shard/head document's items into the corpus's entry
// type (both docs share the {version,tip,items:[...]} shape, so one reader
// serves both). nil (no error) when the key is absent or unparseable.
func readDocItems[E shardEntry](client *Client, key string) ([]E, error) {
	var doc struct {
		Items []E `json:"items"`
	}
	found, err := readCompressedJSON(client, key, &doc)
	if err != nil || !found {
		return nil, err
	}
	return doc.Items, nil
}

// readShardManifest fetches one corpus's manifest; nil (no error) when absent,
// not this corpus's version (an old-schema bucket, treated as absent so the
// classifier re-bootstraps), or unparseable.
func readShardManifest[E shardEntry](client *Client, corpus shardCorpus[E], prefix, ext string) (*siteShardManifest, error) {
	var m siteShardManifest
	found, err := readCompressedJSON(client, prefix+corpus.manifestKey(ext), &m)
	if err != nil {
		return nil, err
	}
	if !found || m.Version != corpus.version(ext) || len(m.Tip) != 40 {
		return nil, nil
	}
	return &m, nil
}

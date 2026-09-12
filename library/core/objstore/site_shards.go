// site_shards.go - the append-only shard and manifest layer both site corpora share

package objstore

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
)

// errNoManifestForBackfill signals a backfill that raced a manifest reset, so the next push bootstraps instead.
var errNoManifestForBackfill = errors.New("backfill: manifest absent at write time")

// shardEntry is one indexed commit in either corpus; entrySHA returns the sha the content hash is computed over.
type shardEntry interface {
	entrySHA() string
}

// siteShardManifest is the manifest document both corpora share: the ordered sealed shards, the head, the tip and the sizes.
type siteShardManifest struct {
	Version     int              `json:"version"`
	Tip         string           `json:"tip"`
	TotalBytes  int              `json:"totalBytes"`
	Complete    bool             `json:"complete"`
	BodiesBytes int              `json:"bodiesBytes,omitempty"`
	Shards      []siteShardEntry `json:"shards"`
	Head        siteShardHead    `json:"head"`
}

// siteShardEntry describes one sealed shard: key basename, content hash, count, compressed size and newest member.
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

// shardCorpus describes one corpus to the generic layer: its key names, its doc marshaler and its progress label.
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

// sealedFrontier returns a manifest's newest sealed member sha, the boundary a repair tail-walk stops at.
func (m *siteShardManifest) sealedFrontier() (string, bool) {
	if m == nil || len(m.Shards) == 0 {
		return "", false
	}
	return m.Shards[len(m.Shards)-1].EndTip, true
}

// shardObjectName is a sealed shard's object-key basename for a content hash.
func shardObjectName(hash string) string {
	return "shard-" + hash + ".json"
}

// shardContentHash keys a sealed shard by its member shas; a version past siteItemsVersion salts it, so a schema tick yields new keys.
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

// reverseGeneric returns a reversed copy, swapping newest-first walk order for oldest-first ingestion order.
func reverseGeneric[E shardEntry](in []E) []E {
	out := make([]E, len(in))
	for i, e := range in {
		out[len(in)-1-i] = e
	}
	return out
}

// putShardDoc uploads one corpus document and returns its compressed size.
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

// sealShardGeneric writes one sealed shard unless the bucket already holds it, falling back to sizeByHash when the endpoint omits Content-Length.
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

// shardSizes builds the hash to recorded-bytes map from a manifest's sealed shards; nil-safe.
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

// shardPlan is a corpus's sealed shards plus the unsealed head, both computed before any head or manifest is written.
type shardPlan[E shardEntry] struct {
	shards      []siteShardEntry // sealed, immutable, already on the bucket
	head        []E              // the trailing unsealed group (oldest-first)
	sealedBytes int              // total compressed bytes across the sealed shards
	headBytes   int              // the head document's compressed size (set by putHead)
}

// planSharded seals every full oldest-first group of a rebuild; shards already present are skipped.
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

// planAppend seals any groups a newest-first gap fills on top of the existing head, leaving prior shards untouched.
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

// planTail rebuilds a corpus from a kept prefix of sealed shards plus a freshly-walked newest-first tail.
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

// sealSegment seals a walked older segment whole, since a backfill carries no head; the trailing group becomes its own shorter shard.
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

// prependSegmentPlan re-reads the manifest immediately before sealing, so a concurrent append's head and tip survive the prepend.
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

// putHead writes a plan's head document and records its compressed size on the plan.
func putHead[E shardEntry](client *Client, corpus shardCorpus[E], prefix, ext, tip string, plan *shardPlan[E]) error {
	headBytes, err := putShardDoc(client, corpus, ext, prefix+corpus.headKey(ext), tip, plan.head, brotliQualityFull)
	if err != nil {
		return err
	}
	plan.headBytes = headBytes
	return nil
}

// putManifest writes a plan's manifest, the corpus's only commit point, and returns its total compressed bytes.
func putManifest[E shardEntry](client *Client, corpus shardCorpus[E], prefix, ext, tip string, plan shardPlan[E], bodiesBytes int, complete bool) (int, error) {
	total := plan.sealedBytes + plan.headBytes
	m := &siteShardManifest{Version: corpus.version(ext), Tip: tip, TotalBytes: total, Complete: complete, BodiesBytes: bodiesBytes, Shards: plan.shards, Head: siteShardHead{Count: len(plan.head), Bytes: plan.headBytes}}
	if err := putShardManifest(client, corpus, prefix, ext, m); err != nil {
		return 0, err
	}
	return total, nil
}

// readDocItems fetches a shard or head document's items; nil when the key is absent or unreadable.
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

// readShardManifest fetches one corpus's manifest; nil when absent, at another version, or unreadable.
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

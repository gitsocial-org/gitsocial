// site_bodies.go - the static-site search-body corpus, projected onto the
// generic shard/manifest layer (site_shards.go).
//
// The bodies corpus (full raw message per commit, loaded only on the reader's
// explicit full-text request) is split per extension under
// `.gitsocial/site/bodies/<ext>/` into immutable sealed shards + an unsealed
// head + a manifest — the append-only, content-hash-keyed shape site_shards.go
// implements for both corpora. This file supplies only the bodies projection:
// key names and the per-doc marshal closure.

package objstore

import (
	"errors"
	"net/http"
	"os"
	"strconv"
)

const (
	// brotliQualityShard compresses a sealed shard once and then serves it
	// immutable, so max quality (11) is worth the one-time wall time — no later
	// push recompresses a sealed shard.
	brotliQualityShard = 11
)

// shardBodyCount is the number of commits in a sealed shard (bodies and items
// alike). Fixed so shard boundaries are stable under append (older shards never
// change). A var (not a const) so tests can lower it to exercise multi-shard
// boundaries on tiny corpora; GITSOCIAL_SITE_SHARD_COUNT overrides it for the
// site-test fixture (unset in production, so the value stays 4000).
var shardBodyCount = shardBodyCountFromEnv()

// shardBodyCountFromEnv returns the sealed-shard size, honoring a positive
// GITSOCIAL_SITE_SHARD_COUNT override, else the 4000 default.
func shardBodyCountFromEnv() int {
	if v := os.Getenv("GITSOCIAL_SITE_SHARD_COUNT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return 4000
}

// entrySHA implements shardEntry for the bodies corpus.
func (e siteBodyEntry) entrySHA() string { return e.SHA }

// siteBodiesDir is the per-extension bodies namespace.
func siteBodiesDir(ext string) string {
	return siteBodiesKeyPrefix + ext + "/"
}

// bodiesManifestKey returns one extension's bodies manifest key.
func bodiesManifestKey(ext string) string {
	return siteBodiesDir(ext) + "manifest.json"
}

// bodiesHeadKey returns one extension's bodies head key.
func bodiesHeadKey(ext string) string {
	return siteBodiesDir(ext) + "head.json"
}

// bodiesShardKey returns a sealed shard's full key under one extension's bodies dir.
func bodiesShardKey(ext, hash string) string {
	return siteBodiesDir(ext) + shardObjectName(hash)
}

// bodiesCorpus wires the bodies key names and doc marshaling into the generic
// shard layer.
var bodiesCorpus = shardCorpus[siteBodyEntry]{
	manifestKey: bodiesManifestKey,
	headKey:     bodiesHeadKey,
	shardName:   shardObjectName,
	shardKey:    bodiesShardKey,
	dir:         siteBodiesDir,
	marshalDoc: func(tip string, entries []siteBodyEntry) any {
		return &siteBodyIndex{Version: siteItemsVersion, Tip: tip, Items: entries}
	},
}

// objectSize reports whether a key exists and, when the endpoint returns it on
// HEAD, its stored (compressed) byte size. It NEVER downloads the body: an
// immutable sealed shard must never be fetched just to learn its size, or
// skip-existing would silently re-download every shard each push on any
// endpoint/CDN that strips Content-Length from HEAD. A present-but-sizeless HEAD
// returns (0, true): the caller carries the size the manifest already recorded
// for that content hash (see sealShardGeneric's sizeByHash).
func objectSize(client *Client, key string) (int, bool, error) {
	resp, err := client.do(http.MethodHead, key, nil, nil, nil)
	if errors.Is(err, ErrNotFound) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	resp.Body.Close()
	if n, e := strconv.Atoi(resp.Header.Get("Content-Length")); e == nil && n > 0 {
		return n, true, nil
	}
	return 0, true, nil
}

// planBodies seals a full bodies (re)build's shards, returning the plan (staged
// so the two corpora can interleave shard/head/manifest writes).
func planBodies(client *Client, prefix, ext string, bodies []siteBodyEntry) (shardPlan[siteBodyEntry], error) {
	return planSharded(client, bodiesCorpus, prefix, ext, bodies, nil)
}

// planBodiesAppend seals any shards a bodies gap fills, returning the plan.
func planBodiesAppend(client *Client, prefix, ext string, gap, headItems []siteBodyEntry, manifest *siteShardManifest) (shardPlan[siteBodyEntry], error) {
	return planAppend(client, bodiesCorpus, prefix, ext, gap, headItems, manifest)
}

// planBodiesTail rebuilds a bodies corpus from its kept sealed shards plus a
// freshly-walked tail (REPAIR).
func planBodiesTail(client *Client, prefix, ext string, keptShards []siteShardEntry, tail []siteBodyEntry) (shardPlan[siteBodyEntry], error) {
	return planTail(client, bodiesCorpus, prefix, ext, keptShards, tail)
}

// putBodiesHead writes a bodies plan's head document.
func putBodiesHead(client *Client, prefix, ext, tip string, plan *shardPlan[siteBodyEntry]) error {
	return putHead(client, bodiesCorpus, prefix, ext, tip, plan)
}

// putBodiesManifest assembles and writes a bodies plan's manifest, returning its
// new total compressed bytes (threaded into the items manifest as bodiesBytes).
// complete is false while a bootstrap is still backfilling older history.
func putBodiesManifest(client *Client, prefix, ext, tip string, plan shardPlan[siteBodyEntry], complete bool) (int, error) {
	return putManifest(client, bodiesCorpus, prefix, ext, tip, plan, 0, complete)
}

// readBodiesManifest fetches one extension's bodies manifest; nil (no error)
// when absent, an older version, or unparseable.
func readBodiesManifest(client *Client, prefix, ext string) (*siteShardManifest, error) {
	return readShardManifest(client, bodiesCorpus, prefix, ext)
}

// readBodyDocItems fetches a bodies document's items (nil when the key is absent
// or unparseable).
func readBodyDocItems(client *Client, key string) ([]siteBodyEntry, error) {
	return readDocItems[siteBodyEntry](client, key)
}

// deleteBodiesSharded removes one extension's whole sharded bodies set (every
// sealed shard enumerated from the manifest, then head and manifest).
func deleteBodiesSharded(client *Client, prefix, ext string) error {
	manifest, err := readBodiesManifest(client, prefix, ext)
	if err != nil {
		return err
	}
	if manifest != nil {
		for _, s := range manifest.Shards {
			if err := client.Delete(prefix + siteBodiesDir(ext) + s.Key); err != nil {
				return err
			}
		}
	}
	if err := client.Delete(prefix + bodiesHeadKey(ext)); err != nil {
		return err
	}
	return client.Delete(prefix + bodiesManifestKey(ext))
}

// site_bodies.go - the search-body corpus's projection onto the shared shard layer: its key names and doc marshaler

package objstore

import (
	"errors"
	"net/http"
	"os"
	"strconv"
)

const (
	// brotliQualityShard compresses a sealed shard once, so max quality is worth the one-time wall time.
	brotliQualityShard = 11
)

// shardBodyCount is the commit count of a sealed shard, fixed so shard boundaries stay stable under append. A var so tests can lower it.
var shardBodyCount = shardBodyCountFromEnv()

// shardBodyCountFromEnv returns the sealed-shard size, honoring GITSOCIAL_SITE_SHARD_COUNT.
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

// bodiesCorpus wires the bodies key names and doc marshaling into the shard layer.
var bodiesCorpus = shardCorpus[siteBodyEntry]{
	label:       "bodies",
	manifestKey: bodiesManifestKey,
	headKey:     bodiesHeadKey,
	shardName:   shardObjectName,
	shardKey:    bodiesShardKey,
	dir:         siteBodiesDir,
	version:     func(string) int { return siteItemsVersion },
	marshalDoc: func(_, tip string, entries []siteBodyEntry) any {
		return &siteBodyIndex{Version: siteItemsVersion, Tip: tip, Items: entries}
	},
}

// objectSize reports whether a key exists and its stored size when HEAD carries one; it downloads no body, so a sizeless HEAD returns (0, true) for the caller to fill from the manifest.
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

// planBodies seals a full bodies rebuild's shards and returns the plan.
func planBodies(client *Client, prefix, ext string, bodies []siteBodyEntry, sp *siteProgress) (shardPlan[siteBodyEntry], error) {
	return planSharded(client, bodiesCorpus, prefix, ext, bodies, nil, sp)
}

// planBodiesAppend seals any shards a bodies gap fills, returning the plan.
func planBodiesAppend(client *Client, prefix, ext string, gap, headItems []siteBodyEntry, manifest *siteShardManifest, sp *siteProgress) (shardPlan[siteBodyEntry], error) {
	return planAppend(client, bodiesCorpus, prefix, ext, gap, headItems, manifest, sp)
}

// planBodiesTail rebuilds a bodies corpus from its kept sealed shards plus a freshly-walked tail.
func planBodiesTail(client *Client, prefix, ext string, keptShards []siteShardEntry, tail []siteBodyEntry, sp *siteProgress) (shardPlan[siteBodyEntry], error) {
	return planTail(client, bodiesCorpus, prefix, ext, keptShards, tail, sp)
}

// putBodiesHead writes a bodies plan's head document.
func putBodiesHead(client *Client, prefix, ext, tip string, plan *shardPlan[siteBodyEntry]) error {
	return putHead(client, bodiesCorpus, prefix, ext, tip, plan)
}

// putBodiesManifest writes a bodies plan's manifest and returns its total compressed bytes, which thread into the items manifest.
func putBodiesManifest(client *Client, prefix, ext, tip string, plan shardPlan[siteBodyEntry], complete bool) (int, error) {
	return putManifest(client, bodiesCorpus, prefix, ext, tip, plan, 0, complete)
}

// readBodiesManifest fetches one extension's bodies manifest; nil when absent or unreadable.
func readBodiesManifest(client *Client, prefix, ext string) (*siteShardManifest, error) {
	return readShardManifest(client, bodiesCorpus, prefix, ext)
}

// readBodyDocItems fetches a bodies document's items.
func readBodyDocItems(client *Client, key string) ([]siteBodyEntry, error) {
	return readDocItems[siteBodyEntry](client, key)
}

// deleteBodiesSharded removes one extension's whole bodies set: every sealed shard, then the head and manifest.
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

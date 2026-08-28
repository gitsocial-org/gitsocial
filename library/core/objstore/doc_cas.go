// doc_cas.go - compare-and-swap rewrites of the mutable brotli-JSON documents
// two pushers can touch at once.
//
// The bucket is a shared backend: several clones push to it, and the pack map
// shards and the sealing state are read-modify-write documents on a single key.
// A plain PUT makes that last-writer-wins, which silently drops the loser's
// update. Every rewrite here re-reads under the key's ETag and writes only if
// nothing moved underneath, replaying the merge on contention.
package objstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"
)

// readCompressedJSONWithETag is readCompressedJSON plus the stored object's
// ETag, which a later conditional write compares against. found reports whether
// a document was parsed; an empty ETag means the key is absent, so the write
// side conditions on creation instead. Retried on a transient fault like every
// other read a long operation depends on: this one sits inside the push path,
// where a single provider hiccup would otherwise fail the push.
//
// A key that IS there but does not parse is an error, never found=false: pairing
// "absent" with the live ETag of something else let the write side put a zeroed
// document over it under a passing compare-and-swap, which on a mixed-version
// bucket dropped every pending sealing round (their packed objects then keep
// their loose copies for good). The caller logs and leaves the key alone.
func readCompressedJSONWithETag(client *Client, key string, v any) (found bool, etag string, err error) {
	data, err := withReadRetry(context.TODO(), func() ([]byte, error) {
		body, tag, err := client.GetWithETag(key)
		etag = tag
		return body, err
	})
	if errors.Is(err, ErrNotFound) {
		return false, "", nil
	}
	if err != nil {
		return false, "", fmt.Errorf("read %s: %w", key, err)
	}
	raw, err := brotliDecompress(data)
	if err != nil || !json.Valid(raw) {
		raw = data
	}
	if json.Unmarshal(raw, v) != nil {
		return false, etag, fmt.Errorf("read %s: %d bytes present that do not parse as JSON; delete the key to have it rebuilt", key, len(data))
	}
	return true, etag, nil
}

// putCompressedIfMatch uploads pre-compressed JSON only while the key still
// carries etag (or, for an empty etag, only while the key is still absent),
// keeping the Content-Encoding metadata putCompressed sets so readers decode
// transparently. A transient fault (a 5xx, a throttle, a dropped connection) is
// retried here like every object PUT on the same push path: this write sits
// inside a push, so one provider hiccup must cost a retried request rather than
// a compare-and-swap attempt or the push itself. The classifier is shared with
// the read retries, so a 412 — the contention signal the caller replays on — is
// never mistaken for a fault.
func putCompressedIfMatch(client *Client, key string, compressed []byte, etag string) error {
	headers := map[string]string{"Content-Type": "application/json", "Content-Encoding": "br"}
	if etag == "" {
		headers["If-None-Match"] = "*"
	} else {
		headers["If-Match"] = etag
	}
	for attempt := 0; ; attempt++ {
		resp, err := client.do(http.MethodPut, key, nil, compressed, headers)
		if err == nil {
			resp.Body.Close()
			return nil
		}
		if attempt >= len(retryBackoff) || !isTransientFault(err) {
			return err
		}
		time.Sleep(retryBackoff[attempt])
	}
}

// updateCompressedJSON rewrites one mutable brotli-JSON document under
// compare-and-swap: read it with its ETag, hand it to merge, write it back only
// if nothing changed underneath, and replay the whole cycle on contention so the
// loser merges onto the winner instead of overwriting it. merge is handed a zero
// document with found=false only when the key is ABSENT: a present document that
// does not parse fails the read instead, so nothing writes over it.
//
// Every way the conditional write can be unusable ends in the same fallback to
// an unconditional write: one that rejects the header outright, or a fault that
// outlived putCompressedIfMatch's own retries, takes it immediately rather than
// aborting. That is exactly the last-writer-wins behavior this replaces —
// degraded, and never a failed push.
//
// A provider declared CapabilityCreateOnly rejects If-Match even when the ETag
// matches, so an update against it can only ever 412. Discovering that per
// document costs maxCASRetries wasted round trips each, which on a cold push
// lands on all 256 pack map shards; those buckets take the fallback on the first
// attempt instead. Creation still goes through If-None-Match: *, which they do
// enforce, so a fresh key keeps its compare-and-swap and a loser replays onto
// the winner's document.
func updateCompressedJSON[T any](client *Client, capability Capability, key string, merge func(doc *T, found bool) error) error {
	for attempt := 0; attempt < maxCASRetries; attempt++ {
		compressed, etag, err := mergeCompressedJSON(client, key, merge)
		if err != nil {
			return err
		}
		if capability == CapabilityCreateOnly && etag != "" {
			return putCompressed(client, key, compressed)
		}
		err = putCompressedIfMatch(client, key, compressed, etag)
		if err == nil {
			return nil
		}
		if !errors.Is(err, ErrPreconditionFailed) {
			// Not contention, so re-reading would only reproduce it: take the
			// fallback below instead of failing the caller.
			fmt.Fprintf(os.Stderr, "gitsocial s3: conditional write %s: %v (falling back to an unconditional write)\n", key, err)
			break
		}
	}
	compressed, _, err := mergeCompressedJSON(client, key, merge)
	if err != nil {
		return err
	}
	return putCompressed(client, key, compressed)
}

// mergeCompressedJSON runs one read-merge-compress cycle of updateCompressedJSON,
// returning the bytes to write and the ETag they must be written against.
func mergeCompressedJSON[T any](client *Client, key string, merge func(doc *T, found bool) error) ([]byte, string, error) {
	var doc T
	found, etag, err := readCompressedJSONWithETag(client, key, &doc)
	if err != nil {
		return nil, "", err
	}
	if err := merge(&doc, found); err != nil {
		return nil, "", err
	}
	compressed, err := compressJSON(&doc, brotliQualityFull)
	if err != nil {
		return nil, "", err
	}
	return compressed, etag, nil
}

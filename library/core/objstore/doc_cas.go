// doc_cas.go - compare-and-swap rewrites of the mutable brotli-JSON documents two pushers can touch at once
package objstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

// readCompressedJSONWithETag is ReadCompressedJSON plus the stored ETag a later conditional write compares against. A key that is present but does not parse is an error, not found=false, so nothing writes a zeroed document over it.
func readCompressedJSONWithETag(client *Client, key string, v any) (found bool, etag string, err error) {
	data, etag, err := client.getWithETag(key)
	if errors.Is(err, ErrNotFound) {
		return false, "", nil
	}
	if err != nil {
		return false, "", fmt.Errorf("read %s: %w", key, err)
	}
	raw, err := BrotliDecompress(data)
	if err != nil || !json.Valid(raw) {
		raw = data
	}
	if json.Unmarshal(raw, v) != nil {
		return false, etag, fmt.Errorf("read %s: %d bytes present that do not parse as JSON; delete the key to have it rebuilt", key, len(data))
	}
	return true, etag, nil
}

// putCompressedIfMatch uploads pre-compressed JSON only while the key still carries etag, or only while it is absent for an empty one; a 412 is contention, not a fault, so it surfaces to the caller.
func putCompressedIfMatch(client *Client, key string, compressed []byte, etag string) error {
	headers := map[string]string{"Content-Type": "application/json", "Content-Encoding": "br"}
	if etag == "" {
		headers["If-None-Match"] = "*"
	} else {
		headers["If-Match"] = etag
	}
	return client.PutWithHeaders(key, compressed, headers)
}

// updateCompressedJSON rewrites one mutable document under compare-and-swap, replaying the merge on contention. Any unusable conditional write falls back to an unconditional one, and a create-only provider takes that fallback on the first attempt rather than burning retries on a 412 it returns either way.
func updateCompressedJSON[T any](client *Client, capability writeCapability, key string, merge func(doc *T, found bool) error) error {
	for attempt := 0; attempt < maxCASRetries; attempt++ {
		compressed, etag, err := mergeCompressedJSON(client, key, merge)
		if err != nil {
			return err
		}
		if capability == capabilityCreateOnly && etag != "" {
			return PutCompressed(client, key, compressed, "")
		}
		err = putCompressedIfMatch(client, key, compressed, etag)
		if err == nil {
			return nil
		}
		if !errors.Is(err, errPreconditionFailed) {
			// Not contention, so re-reading would reproduce it; take the fallback instead.
			fmt.Fprintf(os.Stderr, "gitsocial s3: conditional write %s: %v (falling back to an unconditional write)\n", key, err)
			break
		}
	}
	compressed, _, err := mergeCompressedJSON(client, key, merge)
	if err != nil {
		return err
	}
	return PutCompressed(client, key, compressed, "")
}

// mergeCompressedJSON runs one read-merge-compress cycle and returns the bytes to write with the ETag to write them against.
func mergeCompressedJSON[T any](client *Client, key string, merge func(doc *T, found bool) error) ([]byte, string, error) {
	var doc T
	found, etag, err := readCompressedJSONWithETag(client, key, &doc)
	if err != nil {
		return nil, "", err
	}
	if err := merge(&doc, found); err != nil {
		return nil, "", err
	}
	compressed, err := CompressJSON(&doc, BrotliQualityFull)
	if err != nil {
		return nil, "", err
	}
	return compressed, etag, nil
}

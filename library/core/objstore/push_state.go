// push_state.go - bucket reads that fingerprint a push's ref state.
package objstore

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
)

// refsHeadDigest fingerprints every source a data-derived site artifact reads: the sorted refs/ listing etags plus HEAD's.
func refsHeadDigest(client *Client, prefix string) (string, error) {
	objs, err := client.ListWithETags(prefix + "refs/")
	if err != nil {
		return "", fmt.Errorf("list refs for push-state: %w", err)
	}
	pairs := make([]string, 0, len(objs)+1)
	for _, o := range objs {
		pairs = append(pairs, strings.TrimPrefix(o.Key, prefix)+"\x00"+o.ETag)
	}
	headETag, err := headObjectETag(client, prefix+"HEAD")
	if err != nil {
		return "", err
	}
	pairs = append(pairs, "HEAD\x00"+headETag)
	sort.Strings(pairs)
	h := sha256.New()
	for _, p := range pairs {
		fmt.Fprintln(h, p)
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

// headObjectETag returns a key's ETag, or "" when the key is absent.
func headObjectETag(client *Client, key string) (string, error) {
	resp, err := client.do(http.MethodHead, key, nil, nil, nil)
	if errors.Is(err, ErrNotFound) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("head %s: %w", key, err)
	}
	etag := resp.Header.Get("ETag")
	resp.Body.Close()
	return etag, nil
}

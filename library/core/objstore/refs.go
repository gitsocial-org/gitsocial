// refs.go - Remote ref storage shared by both ref modes: plain keys (etag
// CAS) and generation chains (create-only CAS), resolved structurally on read
// so fetch/clone never need mode negotiation.
package objstore

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Generation chains live under "<refname>/.gen/<counter>". A dot-prefixed
// path component is illegal in a git refname, so chain keys can never collide
// with a real ref, and they sort inside the one refs/ listing reads already do.
const (
	genDir   = "/.gen/"
	genWidth = 10 // zero-padded decimal; ~10 updates/s for 30 years before overflow
)

// genKey builds the bucket key for one generation of a ref.
func genKey(prefix, refName string, gen uint64) string {
	return fmt.Sprintf("%s%s%s%0*d", prefix, refName, genDir, genWidth, gen)
}

// parseGenKey splits a prefix-stripped key into refname and generation;
// isGen=false means a plain ref key (returned as refName unchanged).
// Malformed counters are an error — only a foreign writer produces keys
// under /.gen/ that this code didn't format.
func parseGenKey(key string) (refName string, gen uint64, isGen bool, err error) {
	idx := strings.LastIndex(key, genDir)
	if idx < 0 {
		return key, 0, false, nil
	}
	counter := key[idx+len(genDir):]
	parsed, convErr := strconv.ParseUint(counter, 10, 64)
	if len(counter) != genWidth || convErr != nil {
		return "", 0, false, fmt.Errorf("malformed generation key %q — was the bucket written by a non-gitsocial tool?", key)
	}
	return key[:idx], parsed, true, nil
}

// refSHA validates a ref key's content as a 40-hex sha line.
func refSHA(refName string, value []byte) (string, error) {
	sha := strings.TrimSpace(string(value))
	if len(sha) != 40 {
		return "", fmt.Errorf("ref %s: malformed value %q", refName, sha)
	}
	return sha, nil
}

// readRemoteRefs returns refname → sha for every remote ref, resolving
// generation chains (highest generation wins) and plain keys. A chain takes
// precedence over a plain key of the same name.
func readRemoteRefs(client *Client, prefix string) (map[string]string, error) {
	keys, err := client.List(prefix + "refs/")
	if err != nil {
		return nil, fmt.Errorf("list remote refs: %w", err)
	}
	plain := map[string]bool{}
	chains := map[string]uint64{}
	for _, key := range keys {
		refName, gen, isGen, err := parseGenKey(strings.TrimPrefix(key, prefix))
		if err != nil {
			return nil, err
		}
		if !isGen {
			plain[refName] = true
		} else if gen > chains[refName] {
			chains[refName] = gen
		}
	}
	refs := map[string]string{}
	for refName := range plain {
		if _, hasChain := chains[refName]; hasChain {
			continue
		}
		value, err := client.Get(prefix + refName)
		if err != nil {
			return nil, fmt.Errorf("read ref %s: %w", refName, err)
		}
		sha, err := refSHA(refName, value)
		if err != nil {
			return nil, err
		}
		refs[refName] = sha
	}
	for refName, gen := range chains {
		sha, err := readChainTip(client, prefix, refName, gen)
		if err != nil {
			return nil, err
		}
		refs[refName] = sha
	}
	return refs, nil
}

// readChainTip reads the ref value at the given generation, re-listing the
// chain when the key was garbage-collected between list and read.
func readChainTip(client *Client, prefix, refName string, gen uint64) (string, error) {
	for attempt := 0; attempt < 3; attempt++ {
		value, err := client.Get(genKey(prefix, refName, gen))
		if errors.Is(err, ErrNotFound) {
			gen, err = maxGeneration(client, prefix, refName)
			if err != nil {
				return "", err
			}
			if gen == 0 {
				return "", fmt.Errorf("ref %s: generation chain vanished (deleted concurrently?)", refName)
			}
			continue
		}
		if err != nil {
			return "", fmt.Errorf("read ref %s: %w", refName, err)
		}
		return refSHA(refName, value)
	}
	return "", fmt.Errorf("ref %s: generation chain kept moving; retry", refName)
}

// maxGeneration lists one ref's chain and returns its highest generation (0 = none).
func maxGeneration(client *Client, prefix, refName string) (uint64, error) {
	keys, err := client.List(prefix + refName + genDir)
	if err != nil {
		return 0, fmt.Errorf("list ref %s generations: %w", refName, err)
	}
	max := uint64(0)
	for _, key := range keys {
		_, gen, isGen, err := parseGenKey(strings.TrimPrefix(key, prefix))
		if err != nil {
			return 0, err
		}
		if isGen && gen > max {
			max = gen
		}
	}
	return max, nil
}

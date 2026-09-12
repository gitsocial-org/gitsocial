// info_refs.go - the two listing keys git's dumb-HTTP walker cannot synthesize
package objstore

import (
	"bytes"
	"compress/zlib"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
)

// Dumb-HTTP transport keys, regenerated whenever refs move, so both stay no-cache.
const (
	infoRefsKey = "info/refs"
	packsKey    = "objects/info/packs"
)

// alternatesKeys are published as empty objects, not left absent: git's dumb walker probes them from a completion callback, where a 404 can livelock it.
var alternatesKeys = []string{"objects/info/http-alternates", "objects/info/alternates"}

// maxTagPeelDepth bounds tag-of-tag dereferencing, so a cyclic tag chain cannot spin.
const maxTagPeelDepth = 10

// writeDumbTransportInfo refreshes info/refs and objects/info/packs from what the bucket carries; thin drops the ref advertisement, since a thin bucket's history is incomplete.
func writeDumbTransportInfo(client *Client, prefix string, src *localCommitSource, refs map[string]string, thin bool) error {
	if thin {
		if err := client.Delete(prefix + infoRefsKey); err != nil {
			return fmt.Errorf("delete %s: %w", infoRefsKey, err)
		}
	} else {
		body := buildInfoRefs(refs, func(sha string) (string, bool) {
			return peelBucketTag(client, prefix, src, sha)
		})
		if err := putText(client, prefix+infoRefsKey, body); err != nil {
			return fmt.Errorf("write %s: %w", infoRefsKey, err)
		}
	}
	packs, err := listBucketPacks(client, prefix)
	if err != nil {
		return fmt.Errorf("write %s: %w", packsKey, err)
	}
	if err := putText(client, prefix+packsKey, buildInfoPacks(packs)); err != nil {
		return fmt.Errorf("write %s: %w", packsKey, err)
	}
	for _, key := range alternatesKeys {
		if err := putText(client, prefix+key, nil); err != nil {
			return fmt.Errorf("write %s: %w", key, err)
		}
	}
	return nil
}

// buildInfoPacks renders the objects/info/packs body: one "P <name>.pack" line per pack, then a blank line.
func buildInfoPacks(names []string) []byte {
	var buf bytes.Buffer
	for _, name := range names {
		fmt.Fprintf(&buf, "P %s.pack\n", name)
	}
	buf.WriteByte('\n')
	return buf.Bytes()
}

// buildInfoRefs renders the info/refs body: one line per ref sorted by name, plus a peel line for each annotated tag.
func buildInfoRefs(refs map[string]string, peel func(sha string) (string, bool)) []byte {
	names := make([]string, 0, len(refs))
	for name := range refs {
		names = append(names, name)
	}
	sort.Strings(names)
	var buf bytes.Buffer
	for _, name := range names {
		sha := refs[name]
		fmt.Fprintf(&buf, "%s\t%s\n", sha, name)
		// Only tags carry annotated objects, so peeling those alone costs one GET per tag.
		if strings.HasPrefix(name, "refs/tags/") {
			if target, ok := peel(sha); ok {
				fmt.Fprintf(&buf, "%s\t%s^{}\n", target, name)
			}
		}
	}
	return buf.Bytes()
}

// peelBucketTag dereferences a ref value to its non-tag object, preferring the bucket's loose copy and falling back to the local odb.
func peelBucketTag(client *Client, prefix string, src *localCommitSource, sha string) (string, bool) {
	cur := sha
	for depth := 0; depth < maxTagPeelDepth; depth++ {
		target, isTag, err := bucketTagTarget(client, prefix, src, cur)
		if err != nil || !isTag {
			if cur == sha {
				return "", false // the ref's own object isn't a tag: no peel line
			}
			return cur, true // followed a tag to a non-tag object
		}
		cur = target
	}
	return "", false
}

// bucketTagTarget reads an object from the bucket, or the local odb when the bucket copy is packed, and returns an annotated tag's target.
func bucketTagTarget(client *Client, prefix string, src *localCommitSource, sha string) (target string, isTag bool, err error) {
	if len(sha) != 40 {
		return "", false, fmt.Errorf("malformed object id %q", sha)
	}
	compressed, err := client.Get(prefix + "objects/" + sha[:2] + "/" + sha[2:])
	if errors.Is(err, ErrNotFound) {
		body, ok := src.object(sha, "tag")
		if !ok {
			return "", false, err
		}
		children := tagChildren(body)
		if len(children) == 0 {
			return "", false, fmt.Errorf("tag %s: no target object", sha)
		}
		return children[0], true, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("read object %s: %w", sha, err)
	}
	zr, err := zlib.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return "", false, fmt.Errorf("object %s: inflate: %w", sha, err)
	}
	raw, err := io.ReadAll(zr)
	zr.Close()
	if err != nil {
		return "", false, fmt.Errorf("object %s: inflate: %w", sha, err)
	}
	nul := bytes.IndexByte(raw, 0)
	if nul < 0 {
		return "", false, fmt.Errorf("object %s: missing header", sha)
	}
	objType, _, _ := strings.Cut(string(raw[:nul]), " ")
	if objType != "tag" {
		return "", false, nil
	}
	children := tagChildren(raw[nul+1:])
	if len(children) == 0 {
		return "", false, fmt.Errorf("tag %s: no target object", sha)
	}
	return children[0], true, nil
}

// putText uploads a mutable text/plain transport key; cacheControlForKey makes it no-cache, so a reader revalidates its ref state.
func putText(client *Client, key string, body []byte) error {
	resp, err := client.do(http.MethodPut, key, nil, body, map[string]string{"Content-Type": "text/plain; charset=utf-8"})
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// logDumbTransportInfo runs writeDumbTransportInfo and reports a failure to stderr; the surface self-heals on the next ref-moving push.
func logDumbTransportInfo(client *Client, prefix string, src *localCommitSource, refs map[string]string, thin bool) {
	if err := writeDumbTransportInfo(client, prefix, src, refs, thin); err != nil {
		fmt.Fprintf(os.Stderr, "gitsocial s3: dumb-http info: %v\n", err)
	}
}

// readInfoRefsClaims reads the ref advertisement as refname to sha, the last listing-free ref source; peel lines are skipped.
func readInfoRefsClaims(client *Client, prefix string) (map[string]string, bool) {
	body, err := client.GetRetry(prefix + infoRefsKey)
	if err != nil {
		return nil, false
	}
	claims := map[string]string{}
	for _, line := range strings.Split(string(body), "\n") {
		sha, name, ok := strings.Cut(strings.TrimSpace(line), "\t")
		if !ok || len(sha) != 40 || strings.HasSuffix(name, "^{}") {
			continue
		}
		claims[name] = sha
	}
	return claims, true
}

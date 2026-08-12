// info_refs.go - git dumb-HTTP transport surface.
//
// The bucket layout already IS git's dumb-HTTP protocol: content-addressed
// objects under objects/<xx>/<38-hex> (or packfiles under objects/pack/, see
// pack.go), and HEAD / refs/* as plain keys, all served publicly over HTTPS.
// Stock git's dumb walker needs only two more keys it cannot synthesize (it
// can't list directories): info/refs (the ref listing in `git
// update-server-info` format) and objects/info/packs (the pack list, a lone
// newline while the bucket is all-loose). With both present, `git clone
// https://<host>/` works with stock git and no helper.
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

// Dumb-HTTP transport keys, mutable (regenerated whenever refs move) so both stay
// no-cache (the default cacheControlForKey classification) and text/plain.
const (
	infoRefsKey = "info/refs"
	packsKey    = "objects/info/packs"
)

// maxTagPeelDepth bounds tag-of-tag dereferencing so a malformed or cyclic tag
// chain can never spin.
const maxTagPeelDepth = 10

// writeDumbTransportInfo refreshes info/refs and objects/info/packs from the
// refs and packs the bucket actually carries, so a bucket-served repo clones and
// fetches with stock git over plain HTTPS. src (may be nil) is the pushing
// repo's odb, used to peel a tag whose object is packed rather than loose.
// Best-effort: it runs after the push report is flushed (like the rest of
// post-push maintenance) and a failure only leaves the dumb surface stale until
// the next push, never affecting the git push itself.
func writeDumbTransportInfo(client *Client, prefix string, src *localCommitSource, refs map[string]string) error {
	body := buildInfoRefs(refs, func(sha string) (string, bool) {
		return peelBucketTag(client, prefix, src, sha)
	})
	if err := putText(client, prefix+infoRefsKey, body); err != nil {
		return fmt.Errorf("write %s: %w", infoRefsKey, err)
	}
	packs, err := listBucketPacks(client, prefix)
	if err != nil {
		return fmt.Errorf("write %s: %w", packsKey, err)
	}
	if err := putText(client, prefix+packsKey, buildInfoPacks(packs)); err != nil {
		return fmt.Errorf("write %s: %w", packsKey, err)
	}
	return nil
}

// buildInfoPacks renders the objects/info/packs body in `git
// update-server-info` format: one "P <name>.pack" line per pack, then a blank
// line. An all-loose bucket has no packs and gets the lone newline git itself
// writes.
func buildInfoPacks(names []string) []byte {
	var buf bytes.Buffer
	for _, name := range names {
		fmt.Fprintf(&buf, "P %s.pack\n", name)
	}
	buf.WriteByte('\n')
	return buf.Bytes()
}

// buildInfoRefs renders the info/refs body in `git update-server-info` format:
// "<sha>\t<refname>\n" for every ref sorted by refname, and — for a ref pointing
// at an annotated tag — an immediate "<peeled-sha>\t<refname>^{}\n" line with the
// tag's ultimate non-tag target. peel(sha) resolves a tag under refs/tags to that
// target (ok=false for a non-tag or an unresolvable object, so no peel line is
// emitted, matching a lightweight tag).
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
		// Only tags carry annotated (tag) objects by git convention; peeling just
		// those keeps the cost to one GET per tag while matching update-server-info.
		if strings.HasPrefix(name, "refs/tags/") {
			if target, ok := peel(sha); ok {
				fmt.Fprintf(&buf, "%s\t%s^{}\n", target, name)
			}
		}
	}
	return buf.Bytes()
}

// peelBucketTag dereferences a ref value to its ultimate non-tag object,
// preferring the bucket's loose objects (self-contained, so a tag pushed by
// another clone peels correctly) and falling back to the pushing repo's odb
// when the tag object is packed. Returns ok=false when sha is not a tag object
// or can't be read, so buildInfoRefs emits no peel line — the same output as a
// lightweight tag pointing directly at a commit.
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

// bucketTagTarget reads an object from the bucket (falling back to the local
// odb when the bucket copy is packed rather than loose); when it is an
// annotated tag it returns the sha its `object` header names. isTag=false (with
// no error) for any non-tag object.
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

// putText uploads a mutable text/plain transport key (info/refs, packs). The
// key's mutability makes it no-cache under cacheControlForKey, so stock git and
// the browser reader always revalidate its ref state rather than serve it stale.
func putText(client *Client, key string, body []byte) error {
	resp, err := client.do(http.MethodPut, key, nil, body, map[string]string{"Content-Type": "text/plain; charset=utf-8"})
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// logDumbTransportInfo runs writeDumbTransportInfo and reports any failure to
// stderr without disturbing the caller — the shared best-effort maintenance
// contract (a stale dumb surface self-heals on the next ref-moving push).
func logDumbTransportInfo(client *Client, prefix string, src *localCommitSource, refs map[string]string) {
	if err := writeDumbTransportInfo(client, prefix, src, refs); err != nil {
		fmt.Fprintf(os.Stderr, "gitsocial s3: dumb-http info: %v\n", err)
	}
}

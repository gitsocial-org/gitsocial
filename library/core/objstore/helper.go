// helper.go - git remote helper for s3:// remotes (read side)
//
// Implements the gitremote-helpers(7) line protocol with the `fetch`
// capability (object-graph level, dumb-transport shape): `list` reads one key
// per ref plus HEAD, `fetch` walks the commit graph from the wanted tips and
// downloads missing loose objects straight into GIT_DIR/objects. Objects are
// stored in the bucket exactly as git loose objects (zlib, same 2/38 fan-out),
// so downloads are verbatim copies and SHAs are never rewritten.
package objstore

import (
	"bufio"
	"bytes"
	"compress/zlib"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gitsocial-org/gitsocial/library/core/protocol"
)

// HelperEnv carries the environment the helper runs in (injected for tests).
type HelperEnv struct {
	GitDir    string // $GIT_DIR, set by git when it execs the helper
	Endpoint  string // endpoint override for dev/self-hosted servers (scheme + addressing)
	Region    string // SigV4 region for endpoints no preset recognizes
	PathStyle bool
}

// HelperEnvFromOS reads helper configuration from the process environment:
// standard AWS credential env vars plus GITSOCIAL_S3_ENDPOINT /
// GITSOCIAL_S3_REGION / GITSOCIAL_S3_PATH_STYLE.
func HelperEnvFromOS() HelperEnv {
	return HelperEnv{
		GitDir:    os.Getenv("GIT_DIR"),
		Endpoint:  os.Getenv("GITSOCIAL_S3_ENDPOINT"),
		Region:    os.Getenv("GITSOCIAL_S3_REGION"),
		PathStyle: os.Getenv("GITSOCIAL_S3_PATH_STYLE") == "1",
	}
}

// ParseS3URL splits a canonical s3 URL (s3://<endpoint-host>/<bucket>/<prefix>)
// into endpoint host, bucket, and key prefix (prefix is "" or ends with "/").
// A known provider's virtual-host spelling (s3://<bucket>.<endpoint-host>/…)
// folds to the same result. Bucket-only authorities and query parameters are
// rejected.
func ParseS3URL(raw string) (endpointHost, bucket, prefix string, err error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", "", "", fmt.Errorf("parse remote URL: %w", err)
	}
	if u.Scheme != "s3" {
		return "", "", "", fmt.Errorf("not an s3 URL: %s", raw)
	}
	if u.RawQuery != "" {
		return "", "", "", fmt.Errorf("s3 URLs take no parameters (configure endpoint/path-style via GITSOCIAL_S3_* env): %s", raw)
	}
	authority := strings.ToLower(u.Host)
	if !strings.Contains(authority, ".") {
		return "", "", "", fmt.Errorf("s3 URLs must name the endpoint host: s3://<endpoint-host>/<bucket>/<prefix> (got %s)", raw)
	}
	trail := strings.Trim(u.Path, "/")
	if first, remainder, _ := strings.Cut(authority, "."); remainder != "" {
		if _, _, known := protocol.S3HostInfo(remainder); known {
			endpointHost, bucket = remainder, first // virtual-host spelling
		}
	}
	if endpointHost == "" {
		bucket, trail, _ = strings.Cut(trail, "/")
		if bucket == "" {
			return "", "", "", fmt.Errorf("missing bucket in URL: %s", raw)
		}
		endpointHost = authority
	}
	if trail != "" {
		trail += "/"
	}
	return endpointHost, bucket, trail, nil
}

// remoteHelper holds the state for one helper invocation.
type remoteHelper struct {
	client     *Client
	prefix     string
	gitDir     string
	fetched    map[string]bool // object SHAs confirmed present this session
	capability Capability      // provider's declared conditional-write support
	refMode    string          // resolved lazily on first push (refModeETag/refModeGeneration)
}

// RunHelper speaks the git remote-helper protocol on in/out for the given
// s3:// remote URL until EOF or an empty command line.
func RunHelper(remoteURL string, env HelperEnv, in io.Reader, out io.Writer) error {
	endpointHost, bucket, prefix, err := ParseS3URL(remoteURL)
	if err != nil {
		return err
	}
	if env.GitDir == "" {
		return fmt.Errorf("GIT_DIR not set (helper must be invoked by git)")
	}
	// The URL's endpoint host is authoritative; the env endpoint override
	// exists for dev/self-hosted servers (http scheme, path-style addressing).
	endpoint := env.Endpoint
	if endpoint == "" {
		endpoint = "https://" + endpointHost
	}
	region := env.Region
	capability := CapabilityUnknown
	if provider, hostRegion, ok := protocol.S3HostInfo(endpointHost); ok {
		region = hostRegion
		capability = hostCapability(provider)
	}
	if region == "" {
		region = "us-east-1"
	}
	client, err := NewClient(Config{
		Endpoint:  endpoint,
		Region:    region,
		Bucket:    bucket,
		PathStyle: env.PathStyle,
	})
	if err != nil {
		return err
	}
	h := &remoteHelper{client: client, prefix: prefix, gitDir: env.GitDir, fetched: map[string]bool{}, capability: capability}

	w := bufio.NewWriter(out)
	defer w.Flush()
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 64*1024), 64*1024)
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case line == "capabilities":
			fmt.Fprint(w, "fetch\npush\n\n")
		case line == "list", line == "list for-push":
			if err := h.list(w); err != nil {
				return err
			}
		case strings.HasPrefix(line, "push "):
			batch := []string{line}
			for scanner.Scan() {
				next := scanner.Text()
				if next == "" {
					break
				}
				batch = append(batch, next)
			}
			if err := h.push(batch, w); err != nil {
				return err
			}
		case strings.HasPrefix(line, "fetch "):
			batch := []string{line}
			for scanner.Scan() {
				next := scanner.Text()
				if next == "" {
					break
				}
				batch = append(batch, next)
			}
			if err := h.fetch(batch); err != nil {
				return err
			}
			fmt.Fprint(w, "\n")
		case line == "":
			return w.Flush()
		default:
			return fmt.Errorf("unsupported remote-helper command: %q", line)
		}
		if err := w.Flush(); err != nil {
			return err
		}
	}
	return scanner.Err()
}

// list prints every ref (resolving generation chains) and the HEAD symref.
func (h *remoteHelper) list(w io.Writer) error {
	refs, err := readRemoteRefs(h.client, h.prefix)
	if err != nil {
		return err
	}
	names := make([]string, 0, len(refs))
	for name := range refs {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		fmt.Fprintf(w, "%s %s\n", refs[name], name)
	}
	if head, err := h.client.Get(h.prefix + "HEAD"); err == nil {
		target := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(head)), "ref:"))
		if target != "" {
			fmt.Fprintf(w, "@%s HEAD\n", target)
		}
	} else if !errors.Is(err, ErrNotFound) {
		return fmt.Errorf("read HEAD: %w", err)
	}
	fmt.Fprint(w, "\n")
	return nil
}

// fetch downloads the object graphs reachable from each requested tip.
// Batch lines look like "fetch <sha> <refname>".
func (h *remoteHelper) fetch(batch []string) error {
	for _, line := range batch {
		parts := strings.Fields(line)
		if len(parts) < 2 {
			return fmt.Errorf("malformed fetch command: %q", line)
		}
		if err := h.walkObject(parts[1]); err != nil {
			return err
		}
	}
	return nil
}

// walkObject ensures the object and everything it references exist locally,
// downloading missing loose objects from the bucket. Iterative DFS.
func (h *remoteHelper) walkObject(sha string) error {
	stack := []string{sha}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if h.fetched[cur] {
			continue
		}
		h.fetched[cur] = true
		compressed, present, err := h.ensureObject(cur)
		if err != nil {
			return err
		}
		if present {
			// Already in the local odb from a previous fetch; its graph is
			// assumed complete (same assumption git makes for haves).
			continue
		}
		children, err := objectChildren(compressed, cur)
		if err != nil {
			return err
		}
		stack = append(stack, children...)
	}
	return nil
}

// ensureObject makes the loose object available in GIT_DIR/objects. Returns
// the compressed bytes when it was downloaded, or present=true when the
// object already existed locally.
func (h *remoteHelper) ensureObject(sha string) (compressed []byte, present bool, err error) {
	if len(sha) != 40 {
		return nil, false, fmt.Errorf("malformed object id %q", sha)
	}
	rel := filepath.Join("objects", sha[:2], sha[2:])
	local := filepath.Join(h.gitDir, rel)
	if _, statErr := os.Stat(local); statErr == nil {
		return nil, true, nil
	}
	key := h.prefix + "objects/" + sha[:2] + "/" + sha[2:]
	data, err := h.client.Get(key)
	if errors.Is(err, ErrNotFound) {
		// The bucket is loose-object-only by design (the helper is its only
		// writer); a missing object means a foreign or packed write.
		return nil, false, fmt.Errorf("object %s missing from bucket: the s3 backend stores loose objects only — was the bucket written or repacked by a non-gitsocial tool?", sha)
	}
	if err != nil {
		return nil, false, fmt.Errorf("download object %s: %w", sha, err)
	}
	if err := os.MkdirAll(filepath.Dir(local), 0755); err != nil {
		return nil, false, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(local), "obj-*")
	if err != nil {
		return nil, false, err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return nil, false, err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return nil, false, err
	}
	if err := os.Rename(tmp.Name(), local); err != nil {
		os.Remove(tmp.Name())
		return nil, false, err
	}
	return data, false, nil
}

// objectChildren inflates a loose object and returns the SHAs it references
// (commit → tree + parents, tree → entries, tag → object, blob → none).
func objectChildren(compressed []byte, sha string) ([]string, error) {
	zr, err := zlib.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return nil, fmt.Errorf("object %s: inflate: %w", sha, err)
	}
	raw, err := io.ReadAll(zr)
	zr.Close()
	if err != nil {
		return nil, fmt.Errorf("object %s: inflate: %w", sha, err)
	}
	nul := bytes.IndexByte(raw, 0)
	if nul < 0 {
		return nil, fmt.Errorf("object %s: missing header", sha)
	}
	objType, _, _ := strings.Cut(string(raw[:nul]), " ")
	body := raw[nul+1:]
	switch objType {
	case "blob":
		return nil, nil
	case "commit":
		return commitChildren(body), nil
	case "tag":
		return tagChildren(body), nil
	case "tree":
		return treeChildren(body, sha)
	default:
		return nil, fmt.Errorf("object %s: unknown type %q", sha, objType)
	}
}

// commitChildren extracts tree and parent SHAs from a commit body.
func commitChildren(body []byte) []string {
	var out []string
	for _, line := range strings.Split(string(body), "\n") {
		if line == "" {
			break
		}
		if rest, ok := strings.CutPrefix(line, "tree "); ok && len(rest) >= 40 {
			out = append(out, rest[:40])
		}
		if rest, ok := strings.CutPrefix(line, "parent "); ok && len(rest) >= 40 {
			out = append(out, rest[:40])
		}
	}
	return out
}

// tagChildren extracts the target object SHA from an annotated tag body.
func tagChildren(body []byte) []string {
	for _, line := range strings.Split(string(body), "\n") {
		if line == "" {
			break
		}
		if rest, ok := strings.CutPrefix(line, "object "); ok && len(rest) >= 40 {
			return []string{rest[:40]}
		}
	}
	return nil
}

// treeChildren parses the binary tree format: "<mode> <name>\0" + 20-byte SHA.
func treeChildren(body []byte, sha string) ([]string, error) {
	var out []string
	for len(body) > 0 {
		nul := bytes.IndexByte(body, 0)
		if nul < 0 || len(body) < nul+21 {
			return nil, fmt.Errorf("tree %s: truncated entry", sha)
		}
		out = append(out, fmt.Sprintf("%x", body[nul+1:nul+21]))
		body = body[nul+21:]
	}
	return out, nil
}

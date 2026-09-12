// helper.go - the read side of the s3:// remote helper: the line protocol, the ref listing and the object-graph fetch
package objstore

import (
	"bufio"
	"bytes"
	"compress/zlib"
	"errors"
	"fmt"
	"io"
	"maps"
	"net"
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

// HelperEnvFromOS reads helper configuration from the process environment; credentials are not carried here, but resolved per endpoint host.
func HelperEnvFromOS() HelperEnv {
	return HelperEnv{
		GitDir:    os.Getenv("GIT_DIR"),
		Endpoint:  os.Getenv("GITSOCIAL_S3_ENDPOINT"),
		Region:    os.Getenv("GITSOCIAL_S3_REGION"),
		PathStyle: os.Getenv("GITSOCIAL_S3_PATH_STYLE") == "1",
	}
}

// ParseS3URL splits a canonical s3 URL into endpoint host, bucket and key prefix; a known provider's virtual-host spelling folds to the same result.
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
	// A dot or a port marks a real endpoint host, which a bare bucket name has neither of.
	if !strings.Contains(authority, ".") && !strings.Contains(authority, ":") {
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

// hostAddressKind classifies an endpoint authority: ipLiteral for any IP address, loopback for localhost and the loopback range.
func hostAddressKind(authority string) (ipLiteral, loopback bool) {
	host := authority
	if h, _, err := net.SplitHostPort(authority); err == nil {
		host = h
	}
	if host == "localhost" {
		return false, true
	}
	ip := net.ParseIP(host)
	return ip != nil, ip != nil && ip.IsLoopback()
}

// remoteHelper holds the state for one helper invocation.
type remoteHelper struct {
	client         *Client
	prefix         string
	gitDir         string
	remoteName     string             // the git remote git invoked the helper for ("" = anonymous URL)
	workdir        string             // explicit repo for CLI-side entry points ("" = the GIT_DIR git handed us)
	fetched        map[string]bool    // object SHAs confirmed present this session
	capability     Capability         // provider's declared conditional-write support
	refMode        string             // resolved lazily on first push (refModeETag/refModeGeneration)
	remoteRefs     map[string]string  // ref state from list, kept current by push for the maintenance pass
	manifestETag   string             // the ref manifest as list for-push observed it ("" = absent); bounds remoteRefs' freshness
	leases         map[string]string  // refname → expected oid ("" = must not exist), recorded by `option cas` (--force-with-lease)
	progress       Progress           // stderr progress hook (nil = silent)
	override       SiteOverride       // per-remote site deployment overrides (read from git config)
	packsPulled    bool               // the bucket's packfiles were pulled into GIT_DIR this session
	packObjects    map[string]bool    // object SHAs the pulled packfiles carry
	looseUploaded  int                // objects THIS push uploaded loose (drives the seal trigger)
	local          *localCommitSource // lazily-started local odb reader (packed-object bodies)
	upstreamPulled bool               // the thin-fork read overlay ran this session
	thinResolved   bool               // the push relationship (thin.go) was read from git config this session
	thin           bool               // pushes to this remote exclude upstream objects
	upstreamURL    string             // the upstream this relationship is thin against
	thinPins       []ThinPin          // frontier this push excluded against (nil = none computed)
}

// clientForRemote builds the client, key prefix and provider capability for a canonical s3 remote URL.
func clientForRemote(remoteURL string, env HelperEnv) (*Client, string, Capability, error) {
	endpointHost, bucket, prefix, err := ParseS3URL(remoteURL)
	if err != nil {
		return nil, "", CapabilityUnknown, err
	}
	// The URL's endpoint host is authoritative. Absent an override, an IP-literal or loopback host takes path-style addressing, which is the only shape that resolves there.
	endpoint := env.Endpoint
	pathStyle := env.PathStyle
	if endpoint == "" {
		scheme := "https"
		if ip, loopback := hostAddressKind(endpointHost); ip || loopback {
			pathStyle = true
			if loopback {
				scheme = "http"
			}
		}
		endpoint = scheme + "://" + endpointHost
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
	// Credentials resolve per endpoint host, so a multi-remote push signs each provider with its own keys.
	access, secret := resolveCredentials(endpointHost)
	client, err := NewClient(Config{
		Endpoint:  endpoint,
		Region:    region,
		Bucket:    bucket,
		AccessKey: access,
		SecretKey: secret,
		PathStyle: pathStyle,
	})
	if err != nil {
		return nil, "", CapabilityUnknown, err
	}
	return client, prefix, capability, nil
}

// RunHelper speaks the git remote-helper protocol on in and out until EOF or an empty command line; remoteName supplies the per-remote site overrides, and "" means none.
func RunHelper(remoteName, remoteURL string, env HelperEnv, in io.Reader, out io.Writer) error {
	if env.GitDir == "" {
		return fmt.Errorf("GIT_DIR not set (helper must be invoked by git)")
	}
	client, prefix, capability, err := clientForRemote(remoteURL, env)
	if err != nil {
		return err
	}
	// Progress goes to stderr, since out is the git protocol stream; GIT_QUIET silences it.
	var pw *progressWriter
	if os.Getenv("GIT_QUIET") == "" {
		pw = newProgressWriter(os.Stderr, stderrIsTTY())
	}
	h := &remoteHelper{client: client, prefix: prefix, gitDir: env.GitDir, remoteName: remoteName, fetched: map[string]bool{}, capability: capability, progress: pw.Progress(), override: readRemoteSiteOverride(remoteName)}
	defer pw.finish()
	defer func() { h.local.close() }()

	w := bufio.NewWriter(out)
	defer w.Flush()
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 64*1024), 64*1024)
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case line == "capabilities":
			fmt.Fprint(w, "option\nfetch\npush\n\n")
		case strings.HasPrefix(line, "option "):
			fmt.Fprintf(w, "%s\n", h.option(strings.TrimPrefix(line, "option ")))
		case line == "list", line == "list for-push":
			if err := h.list(w, line == "list for-push"); err != nil {
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

// option handles one "option" command; only "cas", the per-ref force-with-lease expectation, is supported, and every other option answers unsupported, which git takes as a clean decline.
func (h *remoteHelper) option(spec string) string {
	name, value, _ := strings.Cut(spec, " ")
	if name != "cas" {
		return "unsupported"
	}
	refName, oid, ok := strings.Cut(value, ":")
	if !ok || refName == "" || len(oid) != 40 {
		return "error malformed cas value"
	}
	if oid == zeroOID {
		oid = "" // the lease asserts the ref must not exist yet
	}
	if h.leases == nil {
		h.leases = map[string]string{}
	}
	h.leases[refName] = oid
	return "ok"
}

// list prints every ref and the HEAD symref; before a push it also brings a missing or stale ref manifest up to this listing.
func (h *remoteHelper) list(w io.Writer, forPush bool) error {
	refs, err := readRemoteRefs(h.client, h.prefix)
	if err != nil {
		return err
	}
	h.remoteRefs = refs
	if forPush && !h.client.Anonymous() {
		stored, etag, err := readClaimsWithETag(h.client, h.prefix+bucketRefsKey)
		if err != nil && !errors.Is(err, ErrNotFound) {
			return fmt.Errorf("read ref manifest: %w", err)
		}
		h.manifestETag = etag
		if stored == nil || !maps.Equal(stored, refs) {
			if newETag, err := publishRefManifest(h.client, h.prefix, "", refs, etag); err != nil {
				fmt.Fprintf(os.Stderr, "gitsocial s3: ref manifest: %v\n", err)
			} else {
				h.manifestETag = newETag
			}
		}
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
func (h *remoteHelper) fetch(batch []string) error {
	// Pull a packed bucket's packfiles once up front, so the walk resolves those objects from the local odb.
	if err := h.ensurePacksLocal(); err != nil {
		return err
	}
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

// ensurePacksLocal downloads every packfile GIT_DIR lacks, indexes it with git index-pack, and records what each carries. It runs once per session.
func (h *remoteHelper) ensurePacksLocal() error {
	if h.packsPulled {
		return nil
	}
	h.packsPulled = true
	body, err := h.client.Get(h.prefix + packsKey)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read %s: %w", packsKey, err)
	}
	names := parseInfoPacks(body)
	if len(names) == 0 {
		return nil
	}
	dir := filepath.Join(h.gitDir, "objects", "pack")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	h.packObjects = map[string]bool{}
	for _, name := range names {
		idxPath := filepath.Join(dir, name+".idx")
		if _, statErr := os.Stat(idxPath); statErr != nil {
			if err := h.downloadPack(dir, name); err != nil {
				return err
			}
		}
		idx, err := os.ReadFile(idxPath)
		if err != nil {
			return fmt.Errorf("read pack index %s: %w", name, err)
		}
		entries, err := parsePackIdx(idx)
		if err != nil {
			return fmt.Errorf("pack %s: %w", name, err)
		}
		for _, entry := range entries {
			h.packObjects[entry.sha] = true
		}
	}
	return nil
}

// packSidecarSuffixes are the files git index-pack produces beside a pack.
var packSidecarSuffixes = []string{".pack", ".idx", ".rev"}

// downloadPack fetches one packfile and indexes it locally, landing it under a temporary name and renaming it in only once indexed.
func (h *remoteHelper) downloadPack(dir, name string) error {
	data, err := h.client.GetRetry(h.prefix + packKeyPrefix + name + ".pack")
	if err != nil {
		return fmt.Errorf("download %s: %w", name, err)
	}
	tmp := filepath.Join(dir, "tmp-gitsocial-"+name)
	if err := os.WriteFile(tmp+".pack", data, 0644); err != nil {
		return err
	}
	defer func() {
		for _, suffix := range packSidecarSuffixes {
			os.Remove(tmp + suffix)
		}
	}()
	if _, err := gitOutput("index-pack", tmp+".pack"); err != nil {
		return fmt.Errorf("index %s: %w", name, err)
	}
	for _, suffix := range packSidecarSuffixes {
		if _, statErr := os.Stat(tmp + suffix); statErr != nil {
			continue // .rev only exists on git versions that write one
		}
		if err := os.Rename(tmp+suffix, filepath.Join(dir, name+suffix)); err != nil {
			return err
		}
	}
	return nil
}

// walkObject ensures the object and everything it references exist locally, downloading what is missing.
func (h *remoteHelper) walkObject(sha string) error {
	stack := []string{sha}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if h.fetched[cur] {
			continue
		}
		h.fetched[cur] = true
		objType, body, present, err := h.ensureObject(cur)
		if err != nil {
			return err
		}
		if present {
			// Already in the local odb, so its graph is assumed complete, as git assumes for haves.
			continue
		}
		children, err := objectChildren(objType, body, cur)
		if err != nil {
			return err
		}
		stack = append(stack, children...)
	}
	return nil
}

// emptyTreeSHA is the object git synthesizes in every repository, so the presence probe must skip it or the walk does not download the bucket's copy.
const emptyTreeSHA = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

// ensureObject makes the object available in GIT_DIR/objects and returns its type and body when this session brought it in. An object from a pulled pack is not present, since a pack need not close over its references; one the local odb already held is, since it arrived through a real transport.
func (h *remoteHelper) ensureObject(sha string) (objType string, body []byte, present bool, err error) {
	if len(sha) != 40 {
		return "", nil, false, fmt.Errorf("malformed object id %q", sha)
	}
	rel := filepath.Join("objects", sha[:2], sha[2:])
	local := filepath.Join(h.gitDir, rel)
	if _, statErr := os.Stat(local); statErr == nil {
		return "", nil, true, nil
	}
	if h.packObjects[sha] {
		if packedType, packedBody, ok := h.localOdb().typed(sha); ok {
			return packedType, packedBody, false, nil
		}
	} else if sha != emptyTreeSHA {
		if _, _, ok := h.localOdb().typed(sha); ok {
			return "", nil, true, nil
		}
	}
	if h.client == nil {
		return "", nil, false, fmt.Errorf("object %s is not in the local odb and this helper has no bucket client to fall back to", sha)
	}
	key := h.prefix + "objects/" + sha[:2] + "/" + sha[2:]
	data, err := h.client.Get(key)
	if errors.Is(err, ErrNotFound) {
		// A thin fork bucket omits what it shares with upstream, so the first miss triggers the overlay and one retry.
		ran, upErr := h.ensureUpstreamLocal()
		if upErr != nil {
			return "", nil, false, upErr
		}
		if ran {
			if _, _, ok := h.localOdb().typed(sha); ok {
				return "", nil, true, nil
			}
			return "", nil, false, fmt.Errorf("object %s is missing from this thin fork bucket AND from the upstream it names (%s): the fork excluded it as upstream's, and upstream no longer serves it", sha, h.upstreamURL)
		}
		return "", nil, false, fmt.Errorf("object %s missing from bucket: neither a loose key nor any packfile in objects/info/packs carries it — was the bucket written or repacked by a non-gitsocial tool?", sha)
	}
	if err != nil {
		return "", nil, false, fmt.Errorf("download object %s: %w", sha, err)
	}
	if err := os.MkdirAll(filepath.Dir(local), 0755); err != nil {
		return "", nil, false, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(local), "obj-*")
	if err != nil {
		return "", nil, false, err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return "", nil, false, err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return "", nil, false, err
	}
	if err := os.Rename(tmp.Name(), local); err != nil {
		os.Remove(tmp.Name())
		return "", nil, false, err
	}
	objType, body, err = inflateLooseObject(data, sha)
	return objType, body, false, err
}

// localOdb returns the helper's lazily-started cat-file reader on GIT_DIR.
func (h *remoteHelper) localOdb() *localCommitSource {
	if h.local == nil {
		h.local = newLocalCommitSource(h.gitDir, "")
	}
	return h.local
}

// inflateLooseObject inflates a loose object and splits its header from the raw body.
func inflateLooseObject(compressed []byte, sha string) (objType string, body []byte, err error) {
	zr, err := zlib.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return "", nil, fmt.Errorf("object %s: inflate: %w", sha, err)
	}
	raw, err := io.ReadAll(zr)
	zr.Close()
	if err != nil {
		return "", nil, fmt.Errorf("object %s: inflate: %w", sha, err)
	}
	nul := bytes.IndexByte(raw, 0)
	if nul < 0 {
		return "", nil, fmt.Errorf("object %s: missing header", sha)
	}
	objType, _, _ = strings.Cut(string(raw[:nul]), " ")
	return objType, raw[nul+1:], nil
}

// objectChildren returns the shas an object references.
func objectChildren(objType string, body []byte, sha string) ([]string, error) {
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

// treeChildren parses the binary tree format, skipping gitlink entries, whose objects live in the submodule's repository.
func treeChildren(body []byte, sha string) ([]string, error) {
	var out []string
	for len(body) > 0 {
		nul := bytes.IndexByte(body, 0)
		if nul < 0 || len(body) < nul+21 {
			return nil, fmt.Errorf("tree %s: truncated entry", sha)
		}
		mode, _, _ := strings.Cut(string(body[:nul]), " ")
		if mode != "160000" {
			out = append(out, fmt.Sprintf("%x", body[nul+1:nul+21]))
		}
		body = body[nul+21:]
	}
	return out, nil
}

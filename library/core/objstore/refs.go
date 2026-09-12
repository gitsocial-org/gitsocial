// refs.go - Remote ref storage shared by both ref modes: plain keys (etag
// CAS) and generation chains (create-only CAS), resolved structurally on read
// so fetch/clone never need mode negotiation.
package objstore

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

// Generation chains live under "<refname>/.gen/<counter>"; a dot-prefixed component is illegal in a refname, so a chain key cannot collide with a real ref.
const (
	genDir   = "/.gen/"
	genWidth = 10 // zero-padded decimal; ~10 updates/s for 30 years before overflow
)

// bucketRefsKey holds refname to sha for the whole bucket; legacySiteManifestKey is its pre-manifest site copy, read as a fallback and not written.
const (
	bucketRefsKey         = ".gitsocial/refs.json"
	legacySiteManifestKey = ".gitsocial/site/refs.json"
)

// publishRefManifest writes refs as the ref manifest and returns the new ETag; errPreconditionFailed means the document moved, so the caller re-derives and retries.
func publishRefManifest(client *Client, prefix, mode string, refs map[string]string, etag string) (string, error) {
	if mode == "" {
		var err error
		if mode, err = readRefModeMarker(client, prefix); err != nil {
			return "", err
		}
	}
	data, err := json.Marshal(refs)
	if err != nil {
		return "", fmt.Errorf("marshal ref manifest: %w", err)
	}
	if mode == refModeGeneration {
		return "", putObject(client, prefix, bucketRefsKey, data, "application/json")
	}
	newETag, err := putRefManifestConditional(client, prefix+bucketRefsKey, data, etag)
	if err != nil && !errors.Is(err, errPreconditionFailed) {
		return "", fmt.Errorf("upload %s: %w", bucketRefsKey, err)
	}
	return newETag, err
}

// casRefManifest publishes the manifest under compare-and-swap, calling derive once per attempt for the document as stored, the refs to write and the ETag to write them against; a stored document already equal to the refs is left alone.
func casRefManifest(client *Client, prefix, mode string, derive func(attempt int) (stored, refs map[string]string, etag string, err error)) (string, error) {
	for attempt := 0; attempt < maxCASRetries; attempt++ {
		stored, refs, etag, err := derive(attempt)
		if err != nil {
			return "", err
		}
		if stored != nil && maps.Equal(stored, refs) {
			return etag, nil
		}
		newETag, err := publishRefManifest(client, prefix, mode, refs, etag)
		if err == nil {
			return newETag, nil
		}
		if !errors.Is(err, errPreconditionFailed) {
			return "", err
		}
	}
	return "", fmt.Errorf("upload %s: too much contention (gave up after %d attempts)", bucketRefsKey, maxCASRetries)
}

// RebuildRefManifest republishes the manifest from a fresh listing; the ETag is read before the listing, so a manifest written between the two fails the write.
func RebuildRefManifest(client *Client, prefix string, progress Progress) (map[string]string, error) {
	var current map[string]string
	_, err := casRefManifest(client, prefix, "", func(int) (map[string]string, map[string]string, string, error) {
		stored, etag, err := readClaimsWithETag(client, prefix+bucketRefsKey)
		if err != nil && !errors.Is(err, ErrNotFound) {
			return nil, nil, "", err
		}
		if current, err = readRemoteRefsProgress(client, prefix, progress); err != nil {
			return nil, nil, "", fmt.Errorf("read refs: %w", err)
		}
		return stored, current, etag, nil
	})
	if err != nil {
		return nil, err
	}
	return current, nil
}

// putRefManifestConditional writes the manifest only while the key still carries etag, or only while it is absent for an empty one.
func putRefManifestConditional(client *Client, key string, data []byte, etag string) (string, error) {
	headers := map[string]string{"Content-Type": "application/json", "If-Match": etag}
	if etag == "" {
		headers = map[string]string{"Content-Type": "application/json", "If-None-Match": "*"}
	}
	_, respHeaders, err := client.do(context.Background(), http.MethodPut, key, nil, data, headers)
	if err != nil {
		return "", err
	}
	return respHeaders.Get("ETag"), nil
}

// genKey builds the bucket key for one generation of a ref.
func genKey(prefix, refName string, gen uint64) string {
	return fmt.Sprintf("%s%s%s%0*d", prefix, refName, genDir, genWidth, gen)
}

// parseGenKey splits a prefix-stripped key into refname and generation; a malformed counter is an error, since only a foreign writer formats one.
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

// ReadRemoteRefs returns refname to sha for every remote ref; the highest generation wins, and a chain outranks a plain key of the same name.
func ReadRemoteRefs(client *Client, prefix string) (map[string]string, error) {
	return readRemoteRefsProgress(client, prefix, nil)
}

// ListRemoteRefs returns refname to sha for every ref in the bucket behind a canonical s3 remote URL.
func ListRemoteRefs(remoteURL string, env HelperEnv) (map[string]string, error) {
	client, prefix, err := ClientForRemote(remoteURL, env)
	if err != nil {
		return nil, err
	}
	return ReadRemoteRefs(client, prefix)
}

// readRemoteRefsProgress is ReadRemoteRefs with a progress hook, reading the per-ref GETs through a bounded pool. A manifest claim whose MD5 matches the listing's ETag proves a ref's value with no GET; anything else falls back to the read.
func readRemoteRefsProgress(client *Client, prefix string, progress Progress) (map[string]string, error) {
	listed, err := client.listWithETags(prefix + "refs/")
	// A public web domain in front of a bucket answers a list request with 404.
	if errors.Is(err, errAccessDenied) || (client.anonymous && errors.Is(err, ErrNotFound)) {
		return readRefsWithoutListing(client, prefix, progress)
	}
	if err != nil {
		return nil, fmt.Errorf("list remote refs: %w", err)
	}
	plain := map[string]string{} // refName -> listing ETag
	chains := map[string]uint64{}
	for _, obj := range listed {
		refName, gen, isGen, err := parseGenKey(strings.TrimPrefix(obj.Key, prefix))
		if err != nil {
			return nil, err
		}
		if !isGen {
			plain[refName] = obj.ETag
		} else if gen > chains[refName] {
			chains[refName] = gen
		}
	}
	manifest, found := readClaimsDoc(client, prefix+bucketRefsKey)
	if !found {
		manifest, _ = readClaimsDoc(client, prefix+legacySiteManifestKey)
	}
	// Resolve the plain refs the manifest and ETag prove up front; only the rest become GET jobs.
	out := map[string]string{}
	type refJob struct {
		refName string
		gen     uint64
		isChain bool
	}
	var jobs []refJob
	for refName, etag := range plain {
		if _, hasChain := chains[refName]; hasChain {
			continue // a chain of the same name wins; resolved via the chain job
		}
		if sha, ok := manifest[refName]; ok && etagMatchesRef(etag, sha) {
			out[refName] = sha
			continue
		}
		jobs = append(jobs, refJob{refName: refName})
	}
	for refName, gen := range chains {
		jobs = append(jobs, refJob{refName: refName, gen: gen, isChain: true})
	}
	total := len(jobs)
	refs := readRefJobs(total, progress, func(ctx context.Context, j refJob) (string, string, error) {
		if j.isChain {
			sha, err := readChainTip(client, prefix, j.refName, j.gen)
			return j.refName, sha, err
		}
		value, err := client.getContext(ctx, prefix+j.refName)
		if err != nil {
			return "", "", fmt.Errorf("read ref %s: %w", j.refName, err)
		}
		sha, err := refSHA(j.refName, value)
		return j.refName, sha, err
	}, jobs)
	if refs.err != nil {
		return nil, refs.err
	}
	for refName, sha := range refs.out {
		out[refName] = sha
	}
	return out, nil
}

// readRefsWithoutListing discovers refs for a reader that cannot list: a document supplies the names, and in etag mode each name's own key its value.
func readRefsWithoutListing(client *Client, prefix string, progress Progress) (map[string]string, error) {
	claims, found := readRefClaims(client, prefix)
	if !found {
		return nil, noRefSourceError(client, prefix)
	}
	mode, err := readRefModeMarker(client, prefix)
	if err != nil {
		return nil, err
	}
	claimWins := mode == refModeGeneration
	names := make([]string, 0, len(claims))
	for refName := range claims {
		names = append(names, refName)
	}
	refs := readRefJobs(len(names), progress, func(ctx context.Context, refName string) (string, string, error) {
		if claimWins {
			return refName, claims[refName], nil
		}
		value, err := client.getContext(ctx, prefix+refName)
		if errors.Is(err, ErrNotFound) {
			return refName, "", nil // deleted since the document was written
		}
		if err != nil {
			return "", "", fmt.Errorf("read ref %s: %w", refName, err)
		}
		sha, err := refSHA(refName, value)
		return refName, sha, err
	}, names)
	if refs.err != nil {
		return nil, refs.err
	}
	for refName, sha := range refs.out {
		if sha == "" {
			delete(refs.out, refName)
		}
	}
	return refs.out, nil
}

// readRefClaims returns the first ref document the bucket publishes, freshest source first; a present and empty document is found, with no refs.
func readRefClaims(client *Client, prefix string) (map[string]string, bool) {
	if claims, found := readClaimsDoc(client, prefix+bucketRefsKey); found {
		return claims, true
	}
	if claims, found := readInfoRefsClaims(client, prefix); found {
		return claims, true
	}
	return readClaimsDoc(client, prefix+legacySiteManifestKey)
}

// noRefSourceError diagnoses a bucket publishing no ref document, by whether its ref-mode marker or HEAD is readable.
func noRefSourceError(client *Client, prefix string) error {
	for _, key := range []string{refModeKey, "HEAD"} {
		_, err := client.Get(prefix + key)
		if err == nil {
			return fmt.Errorf("bucket denies listing and publishes no ref manifest: ask its owner to push once with a current gitsocial, or use credentials carrying s3:ListBucket (`gitsocial config credentials set <remote>`)")
		}
		if !errors.Is(err, ErrNotFound) {
			return fmt.Errorf("read %s: %w", key, err)
		}
	}
	if client.anonymous {
		return fmt.Errorf("%w: every read was denied, so the bucket is private or nothing has been pushed to it yet", errCredentialsRequired)
	}
	return fmt.Errorf("the credentials for this remote can neither list the bucket nor read its refs: the bucket is empty, or the key lacks s3:GetObject and s3:ListBucket")
}

// readRefModeMarker returns the bucket's recorded ref mode, "" when no push has pinned one.
func readRefModeMarker(client *Client, prefix string) (string, error) {
	value, err := client.Get(prefix + refModeKey)
	if errors.Is(err, ErrNotFound) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read ref-mode marker: %w", err)
	}
	mode := strings.TrimSpace(string(value))
	if mode != refModeETag && mode != refModeGeneration {
		return "", fmt.Errorf("unrecognized ref mode %q in bucket marker (written by a newer gitsocial or a foreign tool?)", mode)
	}
	return mode, nil
}

// readClaimsDoc reads one refname-to-sha JSON document; a present empty document is found, with zero refs.
func readClaimsDoc(client *Client, key string) (map[string]string, bool) {
	claims, _, err := readClaimsWithETag(client, key)
	return claims, err == nil && claims != nil
}

// readClaimsWithETag is readClaimsDoc plus the stored ETag; an unparseable document yields nil claims with its ETag, so a rewrite can replace it.
func readClaimsWithETag(client *Client, key string) (map[string]string, string, error) {
	data, etag, err := client.getWithETag(key)
	if err != nil {
		return nil, "", err
	}
	var claims map[string]string
	if json.Unmarshal(data, &claims) != nil {
		return nil, etag, nil
	}
	return claims, etag, nil
}

// etagMatchesRef reports whether a listing ETag proves a plain ref holds sha; only a true MD5 of the "<sha>\n" body verifies.
func etagMatchesRef(etag, sha string) bool {
	e := strings.Trim(etag, `"`)
	if len(e) != 32 || strings.Contains(e, "-") {
		return false
	}
	sum := md5.Sum([]byte(sha + "\n"))
	return hex.EncodeToString(sum[:]) == strings.ToLower(e)
}

// refReadResult carries the accumulated refs and the first error from the ref-read pool.
type refReadResult struct {
	out map[string]string
	err error
}

// readRefJobs runs read over each job through a bounded worker pool; the first error cancels it, and progress is serialized behind the result mutex.
func readRefJobs[J any](total int, progress Progress, read func(context.Context, J) (string, string, error), jobs []J) refReadResult {
	concurrency := UploadConcurrency()
	if concurrency < 1 {
		concurrency = 1
	}
	if concurrency > len(jobs) {
		concurrency = len(jobs)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	work := make(chan J)
	out := map[string]string{}
	var mu sync.Mutex
	var firstErr error
	var done int64
	setErr := func(err error) {
		mu.Lock()
		if firstErr == nil {
			firstErr = err
			cancel()
		}
		mu.Unlock()
	}

	var wg sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range work {
				refName, sha, err := read(ctx, job)
				if err != nil {
					setErr(err)
					continue
				}
				mu.Lock()
				out[refName] = sha
				progress.Call("refs", int(atomic.AddInt64(&done, 1)), total)
				mu.Unlock()
			}
		}()
	}
	for _, job := range jobs {
		select {
		case work <- job:
		case <-ctx.Done():
		}
	}
	close(work)
	wg.Wait()
	return refReadResult{out: out, err: firstErr}
}

// readChainTip reads the ref value at a generation, re-listing the chain when the key was collected between list and read.
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

// maxGeneration lists one ref's chain and returns its highest generation, 0 for none.
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

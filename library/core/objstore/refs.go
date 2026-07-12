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
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
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
	return readRemoteRefsProgress(client, prefix, nil)
}

// readRemoteRefsProgress is readRemoteRefs with a progress hook: reading each
// ref is one GET, so a bucket with many refs (fork registrations) is a long
// silent phase without it. The per-ref GETs run through a bounded worker pool
// (same size as the upload pool) so ~1,000 refs cost latency of ~1,000/N round
// trips instead of 1,000 serial ones; the first error cancels the pool.
//
// ETag verification: a plain ref key's body is exactly "<sha>\n", and a simple-
// PUT S3/R2 ETag is the MD5 of the body — so when the site refs.json manifest
// (1 GET) claims a refname→sha and that sha's "<sha>\n" MD5 matches the ETag the
// listing already carried, the ref value is proven WITHOUT a GET. Only a
// verified match is trusted, so a wrong manifest can never poison a ref value;
// any mismatch, an absent manifest entry, a multipart-shaped ETag (contains a
// dash → not a plain MD5), or a generation chain falls back to the GET below.
// Buckets with no refs.json (a plain git remote) skip the optimization entirely.
func readRemoteRefsProgress(client *Client, prefix string, progress Progress) (map[string]string, error) {
	listed, err := client.ListWithETags(prefix + "refs/")
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
	manifest := readManifestClaims(client, prefix)
	// Resolve plain refs that the manifest+ETag prove up front; only the
	// unverified remainder becomes GET jobs.
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
		value, err := withReadRetry(ctx, func() ([]byte, error) { return client.Get(prefix + j.refName) })
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

// readManifestClaims fetches the site refs.json manifest (refname → sha) so the
// caller can ETag-verify plain refs without a per-ref GET. It returns nil (never
// an error) when the manifest is absent (a plain git remote, no site) or
// unreadable: the manifest is only an optimization input, and every claim is
// still ETag-verified before use, so a bad manifest degrades to a full GET, never
// a wrong value.
func readManifestClaims(client *Client, prefix string) map[string]string {
	data, err := client.GetRetry(prefix + siteManifestKey)
	if err != nil {
		return nil
	}
	var claims map[string]string
	if json.Unmarshal(data, &claims) != nil {
		return nil
	}
	return claims
}

// etagMatchesRef reports whether a listing ETag proves a plain ref holds sha:
// the ref body is exactly "<sha>\n", so on a simple (non-multipart) PUT the ETag
// is the MD5 hex of that body. A multipart-upload ETag carries a "-<parts>"
// suffix (never a plain MD5) and any non-32-hex ETag is rejected, so only a true
// MD5 match verifies.
func etagMatchesRef(etag, sha string) bool {
	e := strings.Trim(etag, `"`)
	if len(e) != 32 || strings.Contains(e, "-") {
		return false
	}
	sum := md5.Sum([]byte(sha + "\n"))
	return hex.EncodeToString(sum[:]) == strings.ToLower(e)
}

// refReadResult carries the accumulated refs and the first error (if any) from
// the bounded ref-read pool.
type refReadResult struct {
	out map[string]string
	err error
}

// readRefJobs runs read over each job through a bounded worker pool sized like
// the upload pool. The first error cancels the pool and is returned; progress
// (nil = silent) is reported as each ref lands, serialized behind the result
// mutex so the single-goroutine-per-phase Progress contract holds under the pool.
func readRefJobs[J any](total int, progress Progress, read func(context.Context, J) (string, string, error), jobs []J) refReadResult {
	concurrency := resolveUploadConcurrency()
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
				progress.call("site refs", int(atomic.AddInt64(&done, 1)), total)
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

// readChainTip reads the ref value at the given generation, re-listing the
// chain when the key was garbage-collected between list and read.
func readChainTip(client *Client, prefix, refName string, gen uint64) (string, error) {
	for attempt := 0; attempt < 3; attempt++ {
		value, err := client.GetRetry(genKey(prefix, refName, gen))
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

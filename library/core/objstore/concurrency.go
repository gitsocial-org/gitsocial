// concurrency.go - resolves the push object-upload worker-pool size.
//
// Precedence: GITSOCIAL_S3_CONCURRENCY env (the reliable channel for the
// helper subprocess git spawns) > the s3.concurrency personal setting >
// defaultUploadConcurrency. The setting is reachable from the helper because
// it reads the personal bare repo (GITSOCIAL_PERSONAL_REPO / default path),
// not a workdir — so the same value the user set via the CLI applies to the
// helper too.
package objstore

import (
	"os"
	"strconv"
	"sync"

	"github.com/gitsocial-org/gitsocial/library/core/settings"
)

// defaultUploadConcurrency is the fallback pool size: enough to turn ~2
// serial round trips per object into an order-of-magnitude speedup without
// overwhelming a single endpoint or the connection pool (the client's
// transport is sized to match). The env/setting knob covers the long tail.
const defaultUploadConcurrency = 16

// resolveUploadConcurrency picks the object-upload pool size, env first, then
// the personal setting, then the default. A non-positive or unparsable value
// at any layer falls through to the next.
func resolveUploadConcurrency() int {
	if v := os.Getenv("GITSOCIAL_S3_CONCURRENCY"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 {
			return n
		}
	}
	if s, err := settings.Load(""); err == nil {
		if v, ok := settings.Get(s, "s3.concurrency"); ok {
			if n, err := strconv.Atoi(v); err == nil && n >= 1 {
				return n
			}
		}
	}
	return defaultUploadConcurrency
}

// runParallel runs fn over indices [0,n) through the resolved upload pool and
// returns each call's error, index-aligned with the input. Every index runs even
// when a peer fails: a push's per-ref report needs an outcome for every command,
// and callers that only care about the first failure just scan the result. fn
// must confine its writes to its own index (or synchronize them itself).
func runParallel(n int, fn func(i int) error) []error {
	errs := make([]error, n)
	if n == 0 {
		return errs
	}
	concurrency := max(1, min(resolveUploadConcurrency(), n))
	work := make(chan int)
	var wg sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range work {
				errs[idx] = fn(idx)
			}
		}()
	}
	for i := 0; i < n; i++ {
		work <- i
	}
	close(work)
	wg.Wait()
	return errs
}

// firstError returns the first non-nil error, or nil.
func firstError(errs []error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

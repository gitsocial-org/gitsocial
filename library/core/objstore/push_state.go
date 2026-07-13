// push_state.go - skip-if-unchanged marker for site-artifact maintenance.
//
// A site push (or a helper post-push maintenance pass) rewrites every
// data-derived site artifact from the bucket: the refs manifest, the resolved
// pm/site config, and the per-extension item/body corpora. Every one of those
// sources is reachable from refs/: branch tips (refs/heads/*), the gitmsg config
// refs (refs/gitmsg/{pm,core}/config), the extension data branches
// (refs/heads/gitmsg/*), and the fork refs (refs/gitmsg/core/forks/*). The one
// non-ref input is HEAD (the symref that picks the default branch for the code
// view), so the digest folds HEAD's etag in too. Everything else the maintenance
// writes is either embedded in this binary (the site shell, keyed by its own
// version) or written by the CLI from the local workdir (stats.json / HEAD),
// which are cheap and always written outside this skip.
//
// So: an unchanged (refs/ listing + HEAD) etag digest, at the same shell
// version, means no data-derived artifact can have changed — the whole expensive
// pass (per-ref GETs, the config resolves, the items index no-op checks) can be
// skipped in ~2-3 round trips (one ListObjectsV2 over refs/, one HEAD GET, one
// marker GET).
//
// INVARIANT: the marker is a pure optimization. A stale, missing, or corrupt
// marker must only ever cause EXTRA work (a full pass), never SKIPPED work. So
// the marker is written only at the END of a successful full pass, every marker
// read/write error is swallowed (never fails the push), and a digest mismatch
// (or any doubt) runs the full pass.

package objstore

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
)

// sitePushStateKey holds the last successful maintenance pass's fingerprint
// (no-cache: it must revalidate, and it is not content-addressed).
const sitePushStateKey = ".gitsocial/site/push-state"

// sitePushState is the marker written after a full maintenance pass: the digest
// of the (refs/ listing + HEAD) etags that pass observed, plus the embedded site
// shell version, so a newer binary (different shell) still runs a full pass.
// Pages records the HTML page layer's state that pass left behind
// (sitePagesStateOff / sitePagesStateOn): a marker stamped by an older binary
// (no pages field) or under a different pages schema never matches, so a pending
// pages bootstrap can't be masked by a pre-pages marker.
type sitePushState struct {
	Version      int    `json:"version"`
	ShellVersion string `json:"shellVersion"`
	RefsDigest   string `json:"refsDigest"`
	Pages        string `json:"pages,omitempty"`
}

// Pages-state vocabulary the marker records. sitePagesStateOff means the page
// layer was off (and any stale page set deleted) when the pass finished;
// sitePagesStateOn means it was fully generated at the current pages schema. A
// config change that would flip the state also moves refs/gitmsg/core/config,
// which the digest covers — so the marker check only needs to recognize its own
// binary's vocabulary, never re-resolve the config.
const sitePagesStateOff = "off"

var sitePagesStateOn = fmt.Sprintf("v%d", sitePagesVersion)

// sitePushStateVersion is the marker schema version; a marker at any other
// version is treated as absent (runs a full pass).
const sitePushStateVersion = 1

// refsHeadDigest fingerprints everything a data-derived site artifact depends
// on with a cheap listing: the sorted (key, etag) pairs of the refs/ listing
// plus HEAD's etag. Two calls returning the same digest guarantee no branch tip,
// config ref, fork ref, or default-branch selection moved between them.
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

// headObjectETag returns a key's ETag via a HEAD request, or "" (no error) when
// the key is absent (a bucket with no HEAD symref yet).
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

// readSitePushState fetches the push-state marker; ok=false (no error) when it
// is absent, at a foreign version, or unparseable — every one of which means
// "no trustworthy prior pass", so the caller runs a full pass.
func readSitePushState(client *Client, prefix string) (sitePushState, bool) {
	data, err := client.Get(prefix + sitePushStateKey)
	if err != nil {
		return sitePushState{}, false
	}
	var s sitePushState
	if json.Unmarshal(data, &s) != nil || s.Version != sitePushStateVersion {
		return sitePushState{}, false
	}
	return s, true
}

// siteMaintenanceUpToDate reports whether a full maintenance pass can be skipped:
// a marker at the current shell version whose recorded refs+HEAD digest still
// matches the bucket. It returns the freshly-computed digest so a caller that
// proceeds with a full pass can stamp the marker without re-listing. Any error
// (list/HEAD/marker) yields upToDate=false with an empty digest — the caller
// then runs the full pass and skips the marker write, so a transient fault only
// costs extra work, never a wrong skip.
func siteMaintenanceUpToDate(client *Client, prefix, shellVersion string) (upToDate bool, digest string) {
	state, ok := readSitePushState(client, prefix)
	digest, err := refsHeadDigest(client, prefix)
	if err != nil {
		return false, ""
	}
	if !ok || state.ShellVersion != shellVersion {
		return false, digest
	}
	// A marker without a recognizable pages state was stamped by a pages-unaware
	// binary (or a different pages schema): it must not skip the pass that would
	// generate (or clean up) the HTML page layer.
	if state.Pages != sitePagesStateOff && state.Pages != sitePagesStateOn {
		return false, digest
	}
	return digest == state.RefsDigest, digest
}

// writeSitePushState stamps the marker at the end of a successful full pass.
// Best-effort: a write failure only means the NEXT push cannot skip (extra
// work), never a wrong skip, so the error is swallowed by every caller. digest
// may be "" when the pre-pass digest computation failed, and pagesState "" when
// the page layer's state couldn't be trusted; either way the marker is left
// untouched (it can never validate, so writing it would be noise).
func writeSitePushState(client *Client, prefix, shellVersion, digest, pagesState string) {
	if digest == "" || pagesState == "" {
		return
	}
	data, err := json.Marshal(sitePushState{Version: sitePushStateVersion, ShellVersion: shellVersion, RefsDigest: digest, Pages: pagesState})
	if err != nil {
		return
	}
	_ = client.Put(prefix+sitePushStateKey, data)
}

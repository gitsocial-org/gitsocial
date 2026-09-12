// site_push_state.go - the skip-if-unchanged marker for site-artifact maintenance
//
// The marker is an optimization: a stale, missing or unreadable one costs a full
// pass rather than a skipped one.

package objstore

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
)

// sitePushStateKey holds the last successful maintenance pass's fingerprint.
const sitePushStateKey = ".gitsocial/site/push-state"

// sitePushState is the marker a full pass writes: the refs and HEAD etag digest it observed, the shell version, and the page layer's state.
type sitePushState struct {
	Version      int    `json:"version"`
	ShellVersion string `json:"shellVersion"`
	RefsDigest   string `json:"refsDigest"`
	Pages        string `json:"pages,omitempty"`
}

// Pages-state vocabulary the marker records; a config change that flips it also moves a ref the digest covers.
const sitePagesStateOff = "off"

var sitePagesStateOn = fmt.Sprintf("v%d", sitePagesVersion)

// sitePushStateVersion is the marker schema version; another version reads as absent.
const sitePushStateVersion = 1

// foldSiteOverride folds a per-remote override into the digest, since an override moves no bucket ref; an empty one leaves the digest unchanged.
func foldSiteOverride(digest string, ov SiteOverride) string {
	if ov == (SiteOverride{}) {
		return digest
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(digest+"\x00"+ov.URL+"\x00"+ov.Publish+"\x00"+ov.Pages)))
}

// readSitePushState fetches the push-state marker; ok is false when absent, at another version, or unreadable.
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

// siteMaintenanceUpToDate reports whether a full pass can be skipped, and returns the fresh digest so a full pass can stamp it without re-listing.
func siteMaintenanceUpToDate(client *Client, prefix, shellVersion string, ov SiteOverride) (upToDate bool, digest string) {
	state, ok := readSitePushState(client, prefix)
	digest, err := refsHeadDigest(client, prefix)
	if err != nil {
		return false, ""
	}
	// Fold in the per-remote override, which is invisible to the bucket refs.
	digest = foldSiteOverride(digest, ov)
	if !ok || state.ShellVersion != shellVersion {
		return false, digest
	}
	// A marker with no recognizable pages state came from another schema, so it cannot skip the page layer's pass.
	if state.Pages != sitePagesStateOff && state.Pages != sitePagesStateOn {
		return false, digest
	}
	return digest == state.RefsDigest, digest
}

// writeSitePushState stamps the marker at the end of a successful full pass; an untrusted digest or pages state leaves it untouched.
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

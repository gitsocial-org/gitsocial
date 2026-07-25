// artifacts.go - release artifact objects under the reserved artifacts/ root.
//
// `gitsocial release artifacts push` uploads a release's files as plain bucket
// objects at artifacts/<version>/<name> (GITRELEASE.md §3.2 external storage)
// and maintains artifacts/latest.txt, the newest non-prerelease version pushed
// — a stable, forge-free resolution endpoint for install scripts. The
// artifacts/ prefix is part of the reserved root namespace: site maintenance
// never touches keys under it.
package objstore

import (
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
)

const (
	// ArtifactsPrefix is the reserved root prefix for release artifact objects.
	ArtifactsPrefix = "artifacts/"
	// ArtifactsLatestKey holds the newest non-prerelease version pushed (bare
	// semver, trailing newline).
	ArtifactsLatestKey = ArtifactsPrefix + "latest.txt"
)

// PushArtifactObjects uploads each named file as a plain object at
// artifacts/<version>/<name> under the remote's prefix, silently overwriting a
// previous push of the same version, then maintains artifacts/latest.txt:
// advance receives its current content ("" when absent) and decides whether
// the pushed version becomes the new latest. Returns whether latest.txt was
// rewritten.
func PushArtifactObjects(remoteURL string, env HelperEnv, version string, files map[string][]byte, advance func(current string) bool) (bool, error) {
	client, prefix, _, err := clientForRemote(remoteURL, env)
	if err != nil {
		return false, err
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		key := prefix + ArtifactsPrefix + version + "/" + name
		resp, err := client.do(http.MethodPut, key, nil, files[name], map[string]string{"Content-Type": "application/octet-stream"})
		if err != nil {
			return false, fmt.Errorf("upload %s: %w", key, err)
		}
		resp.Body.Close()
	}
	current, err := client.Get(prefix + ArtifactsLatestKey)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return false, fmt.Errorf("read %s: %w", ArtifactsLatestKey, err)
	}
	if !advance(strings.TrimSpace(string(current))) {
		return false, nil
	}
	resp, err := client.do(http.MethodPut, prefix+ArtifactsLatestKey, nil, []byte(version+"\n"), map[string]string{"Content-Type": "text/plain; charset=utf-8"})
	if err != nil {
		return false, fmt.Errorf("upload %s: %w", ArtifactsLatestKey, err)
	}
	resp.Body.Close()
	return true, nil
}

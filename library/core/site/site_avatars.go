// site_avatars.go - the avatar key of an email, the local avatar store and the manifest of the pictures the bucket holds

package site

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"image"
	_ "image/gif"  // registers GIF with image.DecodeConfig
	_ "image/jpeg" // registers JPEG with image.DecodeConfig
	_ "image/png"  // registers PNG with image.DecodeConfig
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/objstore"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
)

const (
	// AvatarsDir is the bucket directory of the pictures, one object per avatar key.
	AvatarsDir = ".gitsocial/site/avatars/"
	// AvatarManifestKey lists the avatar keys under AvatarsDir; its writer puts it after the pictures.
	AvatarManifestKey = ".gitsocial/site/avatars.json"
	// avatarManifestVersion is the manifest schema; another version reads as empty.
	avatarManifestVersion = 1
	// AvatarMaxBytes is the size cap of one picture.
	AvatarMaxBytes     = 256 * 1024
	avatarCacheControl = "public, max-age=86400"
)

// ErrAvatarNotImage marks bytes that are not a PNG, JPEG or GIF within the size cap.
var ErrAvatarNotImage = errors.New("avatar is not a png, jpeg or gif")

// AvatarManifest is the avatars.json document.
type AvatarManifest struct {
	V    int      `json:"v"`
	Keys []string `json:"keys"`
}

// AvatarKey returns FNV-1a 64 of the lowercase trimmed email as 16 hex characters. Mirrors avatarKey in gs-core.js.
func AvatarKey(email string) string {
	hash := fnv.New64a()
	_, _ = hash.Write([]byte(strings.ToLower(strings.TrimSpace(email)))) // a hash write never fails
	return fmt.Sprintf("%016x", hash.Sum64())
}

// RepoSlug converts a repository URL to a filesystem-safe name.
func RepoSlug(url string) string {
	url = strings.TrimPrefix(url, "https://")
	url = strings.TrimPrefix(url, "http://")
	url = strings.TrimSuffix(url, ".git")
	url = strings.ReplaceAll(url, "/", "-")
	url = strings.ReplaceAll(url, ":", "-")
	if len(url) > 50 {
		url = url[:50]
	}
	return url
}

// AvatarStoreDir is the local avatar store of a repository under the cache directory.
func AvatarStoreDir(cacheDir, repoURL string) string {
	return filepath.Join(cacheDir, "imports", RepoSlug(repoURL)+".avatars")
}

// workspaceAvatarStore is the avatar store of the origin of the workspace, "" with no cache directory or no origin.
func workspaceAvatarStore(workdir, cacheDir string) string {
	origin := git.GetOriginURL(workdir)
	if cacheDir == "" || origin == "" {
		return ""
	}
	return AvatarStoreDir(cacheDir, protocol.NormalizeURL(origin))
}

// ListAvatarStore lists the avatar keys the store holds a file for; an absent store is empty.
func ListAvatarStore(dir string) ([]string, error) {
	if dir == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read avatar store: %w", err)
	}
	var keys []string
	for _, e := range entries {
		if e.Type().IsRegular() && validAvatarKey(e.Name()) {
			keys = append(keys, e.Name())
		}
	}
	return keys, nil
}

// validAvatarKey reports a name of 16 lowercase hex characters.
func validAvatarKey(name string) bool {
	return len(name) == 16 && strings.Trim(name, "0123456789abcdef") == ""
}

// CheckAvatarImage returns the bytes and the image/<format> type of a PNG, JPEG or GIF of at most AvatarMaxBytes.
func CheckAvatarImage(data []byte) ([]byte, string, error) {
	if len(data) > AvatarMaxBytes {
		return nil, "", fmt.Errorf("%w: over %d bytes", ErrAvatarNotImage, AvatarMaxBytes)
	}
	_, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, "", fmt.Errorf("%w: %v", ErrAvatarNotImage, err)
	}
	return data, "image/" + format, nil
}

// PutAvatar uploads one picture under its avatar key with its type and the picture cache policy.
func PutAvatar(client *objstore.Client, prefix, key string, data []byte, contentType string) error {
	if err := client.PutWithHeaders(prefix+AvatarsDir+key, data, map[string]string{"Content-Type": contentType, "Cache-Control": avatarCacheControl}); err != nil {
		return fmt.Errorf("upload avatar: %w", err)
	}
	return nil
}

// syncAvatarStore uploads each store picture the manifest lacks, then writes the manifest with the union; an empty store reads no bucket key.
func syncAvatarStore(client *objstore.Client, prefix, dir string, progress objstore.Progress) error {
	stored, err := ListAvatarStore(dir)
	if err != nil || len(stored) == 0 {
		return err
	}
	listed, _, err := readAvatarManifest(client, prefix)
	if err != nil {
		return err
	}
	var uploaded []string
	for _, key := range stored {
		if listed[key] {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, key))
		if err != nil {
			return fmt.Errorf("read avatar store: %w", err)
		}
		data, contentType, err := CheckAvatarImage(raw)
		if err != nil {
			progress.Call(fmt.Sprintf("avatar %s skipped: %v", key, err), 0, 0)
			continue
		}
		if err := PutAvatar(client, prefix, key, data, contentType); err != nil {
			return err
		}
		uploaded = append(uploaded, key)
	}
	if len(uploaded) == 0 {
		return nil
	}
	progress.Call(fmt.Sprintf("avatars: %d uploaded from the store", len(uploaded)), 1, 1)
	for key := range listed {
		uploaded = append(uploaded, key)
	}
	return WriteAvatarManifest(client, prefix, uploaded)
}

// WriteAvatarManifest writes the sorted, deduplicated keys as the avatar manifest.
func WriteAvatarManifest(client *objstore.Client, prefix string, keys []string) error {
	sorted := slices.Compact(slices.Sorted(slices.Values(keys)))
	data, err := json.Marshal(AvatarManifest{V: avatarManifestVersion, Keys: append([]string{}, sorted...)})
	if err != nil {
		return fmt.Errorf("marshal avatar manifest: %w", err)
	}
	if err := client.PutWithHeaders(prefix+AvatarManifestKey, data, map[string]string{"Content-Type": "application/json", "Cache-Control": "no-cache"}); err != nil {
		return fmt.Errorf("upload %s: %w", AvatarManifestKey, err)
	}
	return nil
}

// readAvatarManifest reads the manifest keys and the first 12 hex of the sha256 of its bytes; an absent manifest or another version is empty with no digest.
func readAvatarManifest(client *objstore.Client, prefix string) (keys map[string]bool, digest string, err error) {
	data, err := client.Get(prefix + AvatarManifestKey)
	if errors.Is(err, objstore.ErrNotFound) {
		return map[string]bool{}, "", nil
	}
	if err != nil {
		return nil, "", fmt.Errorf("read %s: %w", AvatarManifestKey, err)
	}
	var m AvatarManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, "", fmt.Errorf("parse %s: %w", AvatarManifestKey, err)
	}
	if m.V != avatarManifestVersion {
		return map[string]bool{}, "", nil
	}
	keys = make(map[string]bool, len(m.Keys))
	for _, k := range m.Keys {
		keys[k] = true
	}
	sum := sha256.Sum256(data)
	return keys, hex.EncodeToString(sum[:])[:12], nil
}

// sitePageSiteWithAvatars is sitePageSiteFor with the picture of each manifest key resolved against the base and the manifest digest set.
func sitePageSiteWithAvatars(client *objstore.Client, prefix string, cfg SiteCustomization, base string) (sitePageSite, error) {
	site := sitePageSiteFor(cfg, base)
	keys, digest, err := readAvatarManifest(client, prefix)
	if err != nil {
		return sitePageSite{}, err
	}
	site.Avatars, site.AvatarDigest = make(map[string]string, len(keys)), digest
	for k := range keys {
		site.Avatars[k] = sitePageResolve(AvatarsDir+k, base)
	}
	return site, nil
}

// site.go - the embedded static read-surface shell for bucket-hosted repos

package site

import (
	"crypto/sha256"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/gitsocial-org/gitsocial/library/core/objstore"
)

// siteFiles holds the browser-only read surface uploaded alongside a bucket-hosted repo.
//
//go:embed assets
var siteFiles embed.FS

// Site state keys under the dot-prefixed namespace no git ref can collide with.
const (
	// siteVersionKey records the hash of the shipped site files, so a push can skip a current bucket.
	siteVersionKey = ".gitsocial/site/version"
	// siteStatsKey holds push-computed counts the analytics page reads.
	siteStatsKey = ".gitsocial/site/stats.json"
)

// siteFileNames lists the embedded site files in upload order, relative to site/, walking subdirectories.
func siteFileNames() ([]string, error) {
	var names []string
	err := fs.WalkDir(siteFiles, "assets", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			names = append(names, strings.TrimPrefix(path, "assets/"))
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("read embedded assets dir: %w", err)
	}
	sort.Strings(names)
	return names, nil
}

// siteContentType maps a site file to the Content-Type it must be served with.
func siteContentType(name string) string {
	switch {
	case strings.HasSuffix(name, ".html"):
		return "text/html; charset=utf-8"
	case strings.HasSuffix(name, ".js"):
		return "text/javascript; charset=utf-8"
	case strings.HasSuffix(name, ".json"):
		return "application/json"
	case strings.HasSuffix(name, ".css"):
		return "text/css; charset=utf-8"
	case strings.HasSuffix(name, ".woff2"):
		return "font/woff2"
	default:
		return "application/octet-stream"
	}
}

// siteCompressible reports whether a site file is a text asset worth brotli-compressing; a bucket does not compress on the fly.
func siteCompressible(name string) bool {
	return strings.HasSuffix(name, ".js") || strings.HasSuffix(name, ".html") ||
		strings.HasSuffix(name, ".json") || strings.HasSuffix(name, ".css")
}

// siteVersion hashes the names and raw bytes of every embedded site file, so a change in compression settings cannot read as a change in content.
func siteVersion() (string, error) {
	names, err := siteFileNames()
	if err != nil {
		return "", err
	}
	h := sha256.New()
	for _, name := range names {
		data, err := siteFiles.ReadFile("assets/" + name)
		if err != nil {
			return "", fmt.Errorf("read embedded %s: %w", name, err)
		}
		fmt.Fprintf(h, "%s %d\n", name, len(data))
		h.Write(data)
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

// putSiteAsset puts one browser-facing site asset, brotli-storing the text ones. A bucket does not negotiate, so only assets no other client fetches belong here.
func putSiteAsset(client *objstore.Client, key, name string, data []byte) error {
	headers := map[string]string{"Content-Type": siteContentType(name)}
	// Assets ship once per shell version, so pay full-quality brotli once.
	if siteCompressible(name) {
		compressed, err := objstore.BrotliCompress(data, objstore.BrotliQualityShard)
		if err != nil {
			return fmt.Errorf("compress %s: %w", name, err)
		}
		data = compressed
		headers["Content-Encoding"] = "br"
	}
	if err := client.PutWithHeaders(key, data, headers); err != nil {
		return fmt.Errorf("upload %s: %w", key, err)
	}
	return nil
}

// uploadShellFile puts one embedded site file (by its site/-relative name).
func uploadShellFile(client *objstore.Client, prefix, name string) error {
	data, err := siteFiles.ReadFile("assets/" + name)
	if err != nil {
		return fmt.Errorf("read embedded %s: %w", name, err)
	}
	return putSiteAsset(client, prefix+name, name, data)
}

// uploadShellIndexHTML puts the embedded shell index.html, the flip back on the pages-disable path.
func uploadShellIndexHTML(client *objstore.Client, prefix string) error {
	return uploadShellFile(client, prefix, "index.html")
}

// uploadSiteFiles puts every embedded site file plus the version marker.
func uploadSiteFiles(client *objstore.Client, prefix string) error {
	names, err := siteFileNames()
	if err != nil {
		return err
	}
	// The shell is dozens of small files, each a round trip, so they upload through the pool.
	if err := objstore.FirstError(objstore.RunParallel(len(names), func(i int) error {
		return uploadShellFile(client, prefix, names[i])
	})); err != nil {
		return err
	}
	// Sweep the retired grammar bundle so a bucket pushed by an earlier binary stays tidy.
	_ = client.Delete(prefix + obsoletePrismExtraKey)
	version, err := siteVersion()
	if err != nil {
		return err
	}
	if err := client.Put(prefix+siteVersionKey, []byte(version+"\n")); err != nil {
		return fmt.Errorf("write site version: %w", err)
	}
	return nil
}

// obsoletePrismExtraKey is the retired extra-grammars bundle, swept on every shell upload.
const obsoletePrismExtraKey = ".gitsocial/site/prism-extra.js"

// siteEnabled reports whether the bucket carries the static read surface, by its version marker, or by index.html when that marker is absent.
func siteEnabled(client *objstore.Client, prefix string) (enabled bool, markerVersion string, err error) {
	current, err := client.Get(prefix + siteVersionKey)
	switch {
	case err == nil:
		return true, strings.TrimSpace(string(current)), nil
	case errors.Is(err, objstore.ErrNotFound):
		_, _, headErr := client.HeadObject(prefix + "index.html")
		if errors.Is(headErr, objstore.ErrNotFound) {
			return false, "", nil
		}
		if headErr != nil {
			return false, "", fmt.Errorf("probe site shell: %w", headErr)
		}
		return true, "", nil
	default:
		return false, "", fmt.Errorf("read site version: %w", err)
	}
}

// ensureSiteShell uploads the embedded site files when the bucket's version marker differs from this binary's; uploaded=true lets a caller reclaim index.html.
func ensureSiteShell(client *objstore.Client, prefix string) (uploaded bool, err error) {
	version, err := siteVersion()
	if err != nil {
		return false, err
	}
	current, err := client.Get(prefix + siteVersionKey)
	if err == nil && strings.TrimSpace(string(current)) == version {
		return false, nil
	}
	if err != nil && !errors.Is(err, objstore.ErrNotFound) {
		return false, fmt.Errorf("read site version: %w", err)
	}
	return true, uploadSiteFiles(client, prefix)
}

// readSiteDefaultBranch returns the repo's default branch name from the bucket's HEAD symref; empty when HEAD is absent or not a symref.
func readSiteDefaultBranch(client *objstore.Client, prefix string) string {
	body, err := client.Get(prefix + "HEAD")
	if err != nil {
		return ""
	}
	target := strings.TrimSpace(string(body))
	ref, ok := strings.CutPrefix(target, "ref:")
	if !ok {
		return ""
	}
	return strings.TrimPrefix(strings.TrimSpace(ref), "refs/heads/")
}

// SetRemoteHead points the bucket's HEAD symref at a branch, written authoritatively on `gitsocial push --site-only`.
func SetRemoteHead(remoteURL string, env objstore.HelperEnv, branch string) error {
	if branch == "" {
		return nil
	}
	client, prefix, _, err := objstore.ClientForRemote(remoteURL, env)
	if err != nil {
		return err
	}
	body := []byte("ref: refs/heads/" + branch + "\n")
	if err := client.PutWithHeaders(prefix+"HEAD", body, map[string]string{"Content-Type": "text/plain"}); err != nil {
		return fmt.Errorf("write HEAD: %w", err)
	}
	return nil
}

// WriteSiteStats publishes the small stats blob the browser reads in one fetch, refreshed on `gitsocial push --site-only`.
func WriteSiteStats(remoteURL string, env objstore.HelperEnv, stats map[string]any) error {
	client, prefix, _, err := objstore.ClientForRemote(remoteURL, env)
	if err != nil {
		return err
	}
	data, err := json.Marshal(stats)
	if err != nil {
		return fmt.Errorf("marshal site stats: %w", err)
	}
	// Brotli-compressed like the item corpora; the commit times can be large.
	comp, err := objstore.BrotliCompress(data, objstore.BrotliQualityFull)
	if err != nil {
		return fmt.Errorf("compress site stats: %w", err)
	}
	if err := client.PutWithHeaders(prefix+siteStatsKey, comp, map[string]string{"Content-Type": "application/json", "Content-Encoding": "br"}); err != nil {
		return fmt.Errorf("upload %s: %w", siteStatsKey, err)
	}
	return nil
}

// Push uploads the shell, seeds the refs manifest and runs the item-artifact state machine over every data branch. The workspace's site.publish guard is the only enabler.
func Push(remoteURL string, env objstore.HelperEnv, workdir string, ov objstore.SiteOverride, progress objstore.Progress) (published, complete bool, err error) {
	client, prefix, _, err := objstore.ClientForRemote(remoteURL, env)
	if err != nil {
		return false, false, err
	}
	// The thin marker is read from the bucket, not per-clone config, so the refusal holds from any clone.
	if upstream, thinErr := objstore.ThinUpstreamURL(client, prefix); thinErr == nil && upstream != "" {
		return false, false, fmt.Errorf("%w (upstream %s)", objstore.ErrThinBucket, upstream)
	}
	// The per-remote override wins over the workspace value, so one remote can carry data with no site.
	cfg, cfgErr := ReadWorkspaceSiteCustomization(workdir)
	eff, effOK := applySiteOverride(siteCustomization(cfg), cfg != SiteCustomization{}, ov)
	if cfgErr != nil || !effOK || eff.Publish != "true" {
		if enabled, _, probeErr := siteEnabled(client, prefix); probeErr == nil && enabled {
			progress.Call("bucket has a site; set `gitsocial config site set publish true` to keep maintaining it", 1, 1)
		}
		return false, false, nil
	}
	src := objstore.NewLocalCommitSource(env.GitDir, workdir)
	defer src.Close()
	complete, err = pushSite(client, prefix, src, ov, progress)
	return true, complete, err
}

// pushSite is Push over a resolved client and prefix; complete is false when a bootstrap still owes work a later push must finish.
func pushSite(client *objstore.Client, prefix string, src *objstore.LocalCommitSource, ov objstore.SiteOverride, progress objstore.Progress) (complete bool, err error) {
	// Skip the pass when nothing a site artifact derives from has moved since the last one at this shell version.
	shellVersion, err := siteVersion()
	if err != nil {
		return false, err
	}
	upToDate, skipDigest := siteMaintenanceUpToDate(client, prefix, shellVersion, ov)
	if upToDate {
		progress.Call("site up to date", 1, 1)
		return true, nil
	}
	if err := uploadSiteFiles(client, prefix); err != nil {
		return false, err
	}
	// The manifest is the site's listing of the bucket, and publishing it heals one an interrupted push left behind.
	refs, err := objstore.RebuildRefManifest(client, prefix, progress)
	if err != nil {
		return false, fmt.Errorf("site manifest: %w", err)
	}
	// Keep the dumb-HTTP surface in step, so a site-only push also heals a stale listing.
	objstore.LogDumbTransportInfo(client, prefix, src, refs, false)
	if err := writeSitePMConfig(client, prefix, refs, src); err != nil {
		return false, err
	}
	if err := writeSiteCustomization(client, prefix, refs, ov, src); err != nil {
		return false, err
	}
	defaultBranch := readSiteDefaultBranch(client, prefix)
	if err := rebuildSiteItems(client, prefix, refs, defaultBranch, src, progress); err != nil {
		return false, err
	}
	// The page layer projects the item artifacts, and only a complete index: pages from a partial one would claim a wrong prefix.
	itemsPending := siteItemsBootstrapPending(client, prefix, refs)
	pagesPending, pagesState := itemsPending, ""
	if !itemsPending {
		var err error
		if pagesPending, pagesState, err = rebuildSitePages(client, prefix, refs, defaultBranch, src, progress, ov); err != nil {
			return false, err
		}
	} else if cfg, ok, err := readSiteCustomization(client, prefix, refs, ov, src); err == nil {
		if _, on := sitePagesEffective(cfg, ok); on {
			progress.Call("site pages: deferred (items index bootstrap in progress; push again or run `gitsocial push --site-only`)", 1, 1)
		}
	}
	// Stamp the marker only after a pass that finished; a bootstrap still in progress has work no ref move signals.
	if !itemsPending && !pagesPending {
		writeSitePushState(client, prefix, shellVersion, skipDigest, pagesState)
	}
	return !itemsPending && !pagesPending, nil
}

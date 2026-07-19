// site.go - embedded static read-surface shell for bucket-hosted repos

package objstore

import (
	"crypto/sha256"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"sort"
	"strings"
)

// SiteFiles holds the browser-only read surface uploaded alongside a bucket-hosted repo.
//
//go:embed site
var SiteFiles embed.FS

// Site state keys under the dot-prefixed namespace no git ref can collide with.
const (
	// siteManifestKey is the push-maintained refs manifest (refname → sha) the
	// static site reads instead of listing the bucket (public domains don't
	// expose listing, and generation-mode refs can't be resolved without it).
	siteManifestKey = ".gitsocial/site/refs.json"
	// siteVersionKey records the hash of the shipped site files so pushes can
	// skip the refresh when the bucket's copy is already current.
	siteVersionKey = ".gitsocial/site/version"
	// siteStatsKey holds push-computed counts the browser cannot cheaply derive
	// (regular git commits have no metadata index), read by the analytics page.
	siteStatsKey = ".gitsocial/site/stats.json"
)

// siteFileNames lists the embedded site files in upload order, walking
// subdirectories (e.g. site/grammars/) so nested assets ship too. Names are
// returned relative to site/ (e.g. "grammars/prism-python.js"), the same shape
// SiteFiles.ReadFile and the upload key expect.
func siteFileNames() ([]string, error) {
	var names []string
	err := fs.WalkDir(SiteFiles, "site", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			names = append(names, strings.TrimPrefix(path, "site/"))
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("read embedded site dir: %w", err)
	}
	sort.Strings(names)
	return names, nil
}

// siteContentType maps a site file to the Content-Type it must be served with
// (browsers won't render/execute S3's default octet-stream).
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
	default:
		return "application/octet-stream"
	}
}

// siteCompressible reports whether a site file is a text asset worth
// brotli-compressing before upload. Buckets never compress on the fly, so the
// shell's largest assets (the JS bundles, the HTML shell) must be stored
// pre-compressed to arrive small on the wire.
func siteCompressible(name string) bool {
	return strings.HasSuffix(name, ".js") || strings.HasSuffix(name, ".html") ||
		strings.HasSuffix(name, ".json") || strings.HasSuffix(name, ".css")
}

// siteVersion hashes the embedded site files (names + content) so a bucket's
// copy can be compared against what this binary ships.
func siteVersion() (string, error) {
	names, err := siteFileNames()
	if err != nil {
		return "", err
	}
	h := sha256.New()
	for _, name := range names {
		data, err := SiteFiles.ReadFile("site/" + name)
		if err != nil {
			return "", fmt.Errorf("read embedded %s: %w", name, err)
		}
		fmt.Fprintf(h, "%s %d\n", name, len(data))
		h.Write(data)
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

// uploadShellFile puts one embedded site file (by its site/-relative name) with
// its Content-Type, brotli-compressing the text assets like uploadSiteFiles.
func uploadShellFile(client *Client, prefix, name string) error {
	data, err := SiteFiles.ReadFile("site/" + name)
	if err != nil {
		return fmt.Errorf("read embedded %s: %w", name, err)
	}
	headers := map[string]string{"Content-Type": siteContentType(name)}
	// Shell assets ship once per version, so pay full-quality brotli once.
	if siteCompressible(name) {
		compressed, err := brotliCompress(data, brotliQualityShard)
		if err != nil {
			return fmt.Errorf("compress %s: %w", name, err)
		}
		data = compressed
		headers["Content-Encoding"] = "br"
	}
	resp, err := client.do(http.MethodPut, prefix+name, nil, data, headers)
	if err != nil {
		return fmt.Errorf("upload %s: %w", name, err)
	}
	resp.Body.Close()
	return nil
}

// uploadShellIndexHTML puts just the embedded shell index.html — the flip back
// to the shell entry on the pages-disable path (see deleteSitePages).
func uploadShellIndexHTML(client *Client, prefix string) error {
	return uploadShellFile(client, prefix, "index.html")
}

// uploadSiteFiles puts every embedded site file plus the version marker.
func uploadSiteFiles(client *Client, prefix string) error {
	names, err := siteFileNames()
	if err != nil {
		return err
	}
	for _, name := range names {
		if err := uploadShellFile(client, prefix, name); err != nil {
			return err
		}
	}
	// The shell now ships every Prism grammar under grammars/ and lazy-loads
	// them, so the old push-published prism-extra.js bundle is obsolete. Delete
	// it best-effort whenever the shell is (re)uploaded so buckets pushed by an
	// earlier binary stay tidy; a delete failure never fails the shell upload.
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

// obsoletePrismExtraKey is the retired push-published extra-grammars bundle
// (replaced by the shell's lazy-loaded grammars/ files). Deleted on shell upload
// so it doesn't linger on buckets first pushed by an older binary.
const obsoletePrismExtraKey = ".gitsocial/site/prism-extra.js"

// siteEnabled reports whether the bucket carries the static read surface — the
// signal behind the "bucket has a site but site.publish is off" hint. The
// version marker is the cheap check (present ⇒ enabled, and its value is
// returned); when it is absent the shell's index.html HEAD decides (a
// pre-version-marker bucket).
func siteEnabled(client *Client, prefix string) (enabled bool, markerVersion string, err error) {
	current, err := client.Get(prefix + siteVersionKey)
	switch {
	case err == nil:
		return true, strings.TrimSpace(string(current)), nil
	case errors.Is(err, ErrNotFound):
		resp, headErr := client.do(http.MethodHead, prefix+"index.html", nil, nil, nil)
		if errors.Is(headErr, ErrNotFound) {
			return false, "", nil
		}
		if headErr != nil {
			return false, "", fmt.Errorf("probe site shell: %w", headErr)
		}
		resp.Body.Close()
		return true, "", nil
	default:
		return false, "", fmt.Errorf("read site version: %w", err)
	}
}

// ensureSiteShell uploads the embedded site files when the bucket's version
// marker is absent or differs from this binary's embedded copy (last writer
// wins across mixed binary versions). With the site.publish guard on, this is
// what creates the shell on a bucket that has none — the guard travels with
// the repo, so a plain `git push` carrying it bootstraps the site too. Returns
// uploaded=true when it re-shipped the assets, so a caller can reclaim any
// dual-owned key (index.html) the fresh upload just overwrote.
func ensureSiteShell(client *Client, prefix string) (uploaded bool, err error) {
	version, err := siteVersion()
	if err != nil {
		return false, err
	}
	current, err := client.Get(prefix + siteVersionKey)
	if err == nil && strings.TrimSpace(string(current)) == version {
		return false, nil
	}
	if err != nil && !errors.Is(err, ErrNotFound) {
		return false, fmt.Errorf("read site version: %w", err)
	}
	return true, uploadSiteFiles(client, prefix)
}

// readSiteDefaultBranch returns the repo's default branch name from the bucket's
// HEAD symref key (`ref: refs/heads/<branch>`), used by the code items index for
// default-branch attribution. Empty (best-effort) when HEAD is absent or not a
// symref — the code walk then attributes every commit to the first branch that
// reached it, which the reader tolerates.
func readSiteDefaultBranch(client *Client, prefix string) string {
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

// putSiteManifest writes the refname → sha map as the site refs manifest.
func putSiteManifest(client *Client, prefix string, refs map[string]string) error {
	data, err := json.Marshal(refs)
	if err != nil {
		return fmt.Errorf("marshal site manifest: %w", err)
	}
	resp, err := client.do(http.MethodPut, prefix+siteManifestKey, nil, data, map[string]string{"Content-Type": "application/json"})
	if err != nil {
		return fmt.Errorf("upload %s: %w", siteManifestKey, err)
	}
	resp.Body.Close()
	return nil
}

// SetRemoteHead points the bucket's HEAD symref at the given branch, so git
// clone and the browser code view use the repo's real default branch (e.g.
// "master") rather than an assumed "main" or whatever branch happened to be
// pushed first. Written authoritatively on `gitsocial site push`.
func SetRemoteHead(remoteURL string, env HelperEnv, branch string) error {
	if branch == "" {
		return nil
	}
	client, prefix, _, err := clientForRemote(remoteURL, env)
	if err != nil {
		return err
	}
	body := []byte("ref: refs/heads/" + branch + "\n")
	resp, err := client.do(http.MethodPut, prefix+"HEAD", nil, body, map[string]string{"Content-Type": "text/plain"})
	if err != nil {
		return fmt.Errorf("write HEAD: %w", err)
	}
	resp.Body.Close()
	return nil
}

// WriteSiteStats publishes a small stats blob at .gitsocial/site/stats.json for
// the browser read surface. It carries counts with no metadata index — the
// default branch's regular commit count — that the pusher (which has the git
// repo) computes cheaply and the browser reads in one fetch. Refreshed on
// `gitsocial site push`; a plain git push leaves it until the next site push.
func WriteSiteStats(remoteURL string, env HelperEnv, stats map[string]any) error {
	client, prefix, _, err := clientForRemote(remoteURL, env)
	if err != nil {
		return err
	}
	data, err := json.Marshal(stats)
	if err != nil {
		return fmt.Errorf("marshal site stats: %w", err)
	}
	// Brotli-compressed (Content-Encoding: br) like the item corpora — the commit
	// times can be large; the browser's fetch decodes it transparently.
	comp, err := brotliCompress(data, brotliQualityFull)
	if err != nil {
		return fmt.Errorf("compress site stats: %w", err)
	}
	resp, err := client.do(http.MethodPut, prefix+siteStatsKey, nil, comp, map[string]string{"Content-Type": "application/json", "Content-Encoding": "br"})
	if err != nil {
		return fmt.Errorf("upload %s: %w", siteStatsKey, err)
	}
	resp.Body.Close()
	return nil
}

// PushSite uploads the embedded site files to the bucket behind a canonical
// s3 remote URL, at the repo's key prefix, seeds the refs manifest, and runs
// the item-artifact state machine over every extension data branch (appending,
// repairing, or advancing a bootstrap as the current state demands) so buckets
// pushed by older gitsocial versions render fully right away.
//
// The workspace's site.publish guard (the `site` sub-object of the local
// refs/gitmsg/core/config — the same value the data push publishes) is the only
// enabler: unset/false returns published=false and touches nothing, printing a
// one-line hint via progress when the bucket already carries a site.
//
// workdir is the local checkout the site push runs from (env.GitDir is used when
// set instead): the items walk reads commits from that repo's odb rather than a
// per-commit bucket GET, since every commit it visits is an ancestor of a bucket
// ref tip and so present locally too. Empty (and no GIT_DIR) ⇒ bucket-only walk.
func PushSite(remoteURL string, env HelperEnv, workdir string, ov SiteOverride, progress Progress) (published bool, err error) {
	client, prefix, _, err := clientForRemote(remoteURL, env)
	if err != nil {
		return false, err
	}
	// The publish guard is effective (the per-remote override wins over the
	// workspace value) so a remote configured with publish=false carries data
	// but no site, and publish=true can render a bucket the repo hasn't opted in.
	cfg, cfgErr := ReadWorkspaceSiteCustomization(workdir)
	eff, effOK := applySiteOverride(siteCustomization(cfg), cfg != SiteCustomization{}, ov)
	if cfgErr != nil || !effOK || eff.Publish != "true" {
		if enabled, _, probeErr := siteEnabled(client, prefix); probeErr == nil && enabled {
			progress.call("bucket has a site; set `gitsocial config site set publish true` to keep maintaining it", 1, 1)
		}
		return false, nil
	}
	src := newLocalCommitSource(env.GitDir, workdir)
	defer src.close()
	return true, pushSite(client, prefix, src, ov, progress)
}

// pushSite is PushSite over a resolved client/prefix (the unit-testable core).
// src (may be nil) is the local commit source for the items walk; ov carries
// the per-remote deployment overrides applied at the site-config boundary.
func pushSite(client *Client, prefix string, src *localCommitSource, ov SiteOverride, progress Progress) error {
	// Skip the whole expensive pass when nothing a site artifact derives from has
	// changed since the last successful pass at this shell version — detected in
	// ~2-3 round trips (refs/ list + HEAD + marker GET). The marker is only an
	// optimization: any error or mismatch below falls through to the full pass,
	// and skipDigest is "" when it couldn't be trusted (never a wrong skip).
	shellVersion, err := siteVersion()
	if err != nil {
		return err
	}
	upToDate, skipDigest := siteMaintenanceUpToDate(client, prefix, shellVersion, ov)
	if upToDate {
		progress.call("site up to date", 1, 1)
		return nil
	}
	if err := uploadSiteFiles(client, prefix); err != nil {
		return err
	}
	refs, err := readRemoteRefsProgress(client, prefix, progress)
	if err != nil {
		return fmt.Errorf("read refs for site manifest: %w", err)
	}
	if err := putSiteManifest(client, prefix, refs); err != nil {
		return err
	}
	if err := writeSitePMConfig(client, prefix, refs); err != nil {
		return err
	}
	if err := writeSiteCustomization(client, prefix, refs, ov); err != nil {
		return err
	}
	defaultBranch := readSiteDefaultBranch(client, prefix)
	if err := rebuildSiteItems(client, prefix, refs, defaultBranch, src, progress); err != nil {
		return err
	}
	// The HTML page layer projects the item artifacts written above, so it runs
	// after them — but only once the items index is complete: one bootstrap at a
	// time, and pages generated from a partial index would claim a wrong prefix.
	itemsPending := siteItemsBootstrapPending(client, prefix, refs)
	pagesPending, pagesState := itemsPending, ""
	if !itemsPending {
		var err error
		if pagesPending, pagesState, err = rebuildSitePages(client, prefix, refs, defaultBranch, src, progress, ov); err != nil {
			return err
		}
	} else if cfg, ok, err := readSiteCustomization(client, prefix, refs, ov); err == nil {
		if _, on := sitePagesEffective(cfg, ok); on {
			progress.call("site pages: deferred (items index bootstrap in progress; push again or run `gitsocial site push`)", 1, 1)
		}
	}
	// Stamp the marker LAST, only after a fully successful pass, so an interrupted
	// pass leaves the marker stale-or-absent and the next push redoes the work. An
	// in-progress bootstrap (an incomplete items index, or an incomplete page set
	// still under its budget cursor) is NOT a finished pass: it has more work no
	// ref move signals, so leave the marker unstamped and let the next push
	// advance it rather than skip.
	if !itemsPending && !pagesPending {
		writeSitePushState(client, prefix, shellVersion, skipDigest, pagesState)
	}
	return nil
}

// site_maintenance.go - the site's share of the post-push pass, run as the remote helper's hook

package objstore

import (
	"fmt"
	"os"
)

// PostPushMaintenance maintains the site after a push: the shell, the config artifacts, the item indexes, the front page and the skip marker.
func PostPushMaintenance(out PushOutcome) {
	client, prefix := out.Client, out.Prefix
	// A thin bucket publishes no site, since the site reads a history it does not carry.
	if out.Thin {
		if enabled, _, probeErr := siteEnabled(client, prefix); probeErr == nil && enabled {
			fmt.Fprintf(os.Stderr, "gitsocial s3: thin fork bucket; its existing site is no longer maintained (detach with `gitsocial push --full`)\n")
		}
		return
	}
	// One local commit source serves the whole pass; the helper runs as a git child, so the pushed objects are already here.
	src := NewLocalCommitSource(out.GitDir, "")
	defer src.Close()
	// The pushed site.publish guard is the only enabler, so a plain s3:// remote stays clean.
	out.Progress.Call("maintenance: site artifacts", 0, 0)
	cfg, cfgOK, err := readSiteCustomization(client, prefix, out.Refs, out.Override, src)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gitsocial s3: site config: %v\n", err)
		return
	}
	if !cfgOK || cfg.Publish != "true" {
		if enabled, _, probeErr := siteEnabled(client, prefix); probeErr == nil && enabled {
			fmt.Fprintf(os.Stderr, "gitsocial s3: bucket has a site; set `gitsocial config site set publish true` to keep maintaining it\n")
		}
		return
	}
	// The refs-derived artifacts depend only on the refs listing and HEAD, so the marker can skip them. The items append below is not gated on it.
	shellVersion, verErr := siteVersion()
	upToDate, digest := false, ""
	if verErr == nil {
		upToDate, digest = siteMaintenanceUpToDate(client, prefix, shellVersion, out.Override)
	}
	shellUploaded := false
	if !upToDate {
		// Shell first, so no reader sees data artifacts without an entry page.
		var err error
		if shellUploaded, err = ensureSiteShell(client, prefix); err != nil {
			fmt.Fprintf(os.Stderr, "gitsocial s3: site refresh: %v\n", err)
		}
		if err := writeSitePMConfig(client, prefix, out.Refs, src); err != nil {
			fmt.Fprintf(os.Stderr, "gitsocial s3: site pm config: %v\n", err)
		}
		if err := writeSiteCustomization(client, prefix, out.Refs, out.Override, src); err != nil {
			fmt.Fprintf(os.Stderr, "gitsocial s3: site customization: %v\n", err)
		}
	}
	updatePushedSiteItems(out, src)
	// A shell upload overwrites the dual-owned index.html, so reclaim the front page in the one case the marker below would still be stamped.
	reclaimOK := true
	if shellUploaded {
		reclaimOK = reclaimSitePagesFront(client, prefix, out.Refs, out.Override, src)
	}
	// Stamp the marker only when this pass left nothing a later site pass must still do; a withheld marker costs one extra pass, a wrong one costs a skip.
	if !upToDate && reclaimOK && out.ManifestOK {
		pagesState, pagesPending := sitePagesState(client, prefix, out.Refs, out.Override, src)
		if !siteItemsBootstrapPending(client, prefix, out.Refs) && !pagesPending {
			writeSitePushState(client, prefix, shellVersion, digest, pagesState)
		}
	}
}

// updatePushedSiteItems maintains the artifacts of every pushed data branch, plus the code index; the repair machine heals whatever a failure leaves.
func updatePushedSiteItems(out PushOutcome, src *LocalCommitSource) {
	client, prefix := out.Client, out.Prefix
	for dst, sha := range out.Updates {
		ext := siteItemsExt(dst)
		if ext == "" {
			continue
		}
		var err error
		if sha == "" {
			err = deleteSiteArtifacts(client, prefix, ext)
		} else {
			err = updateSiteItemsIndex(client, prefix, ext, sha, &siteProgress{progress: out.Progress, ext: ext, src: src})
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "gitsocial s3: items index %s: %v\n", ext, err)
		}
	}
	tips := codeBranchTips(out.Refs, out.DefaultBranch)
	sp := &siteProgress{progress: out.Progress, ext: siteCodeExt, src: src}
	if err := updateSiteCodeIndex(client, prefix, tips, out.DefaultBranch, sp); err != nil {
		fmt.Fprintf(os.Stderr, "gitsocial s3: code index: %v\n", err)
	}
}

// reclaimSitePagesFront re-writes index.html after a shell upload clobbered it, but only when the page set is complete and current; ok=false only on a failed reclaim.
func reclaimSitePagesFront(client *Client, prefix string, refs map[string]string, ov SiteOverride, src *LocalCommitSource) (ok bool) {
	cfg, cfgOK, err := readSiteCustomization(client, prefix, refs, ov, src)
	if err != nil {
		return false // can't tell if a reclaim was needed: withhold the marker
	}
	url, on := sitePagesEffective(cfg, cfgOK)
	if !on {
		return true // layer off: index.html is legitimately the shell
	}
	site := sitePageSiteFor(prefix, cfg, url)
	manifest, err := readSitePagesManifest(client, prefix)
	if err != nil {
		return false
	}
	if manifest == nil || manifest.Cursor != nil || manifest.SiteHash != sitePageSiteHash(site) {
		return true // page set pending/stale: a site push rebuilds+reclaims
	}
	manifests, tips, err := readSitePagesManifests(client, prefix, refs)
	if err != nil {
		return false
	}
	if !sitePagesTipsCurrent(manifest, tips) {
		return true // tips moved: pending, a site push rebuilds+reclaims
	}
	home := readSiteFrontHome(src, site, refs, readSiteDefaultBranch(client, prefix))
	if err := reclaimSiteFrontPage(client, prefix, site, manifests, home); err != nil {
		fmt.Fprintf(os.Stderr, "gitsocial s3: reclaim front page: %v\n", err)
		return false
	}
	return true
}

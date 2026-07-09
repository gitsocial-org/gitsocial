// site_customization.go - push-maintained static-site customization artifact.
//
// The repo's site customization lives at refs/gitmsg/core/config as a commit
// whose message is the core config JSON with a `site` sub-object (title, accent,
// accentDark, favicon). This resolves that sub-object at push time, validates it
// strictly, and emits it as .gitsocial/site/site-config.json alongside the other
// mutable site artifacts. The reader loads it (no-cache, refreshed on every push)
// and applies the overrides; an absent or malformed config deletes the artifact
// (the reader falls back to its built-in defaults), mirroring pm-config.json.

package objstore

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
)

// siteCustomizationKey is the site customization the static site reads.
const siteCustomizationKey = ".gitsocial/site/site-config.json"

// SiteConfigMaxTitle bounds a customization title (defensive: the reader only
// uses textContent, but a runaway title serves no one).
const SiteConfigMaxTitle = 200

// SiteFaviconMaxBytes caps the favicon data URI so the no-cache artifact stays
// small (the whole reason it is a data URI: no extra bucket object).
const SiteFaviconMaxBytes = 32 * 1024

// siteHexRe matches a strict CSS hex color (#rgb or #rrggbb), the only accent
// shape the writer emits and the reader applies.
var siteHexRe = regexp.MustCompile(`^#(?:[0-9a-fA-F]{3}|[0-9a-fA-F]{6})$`)

// siteFaviconRe matches an allowed favicon data URI prefix (png/webp/svg+xml).
// Kept in sync with the reader's favicon guard in gs-app.js.
var siteFaviconRe = regexp.MustCompile(`^data:image/(png|webp|svg\+xml)[;,]`)

// siteCustomization is the validated customization the reader consumes: a title,
// an accent (light) and optional accentDark, and an optional favicon data URI.
// Only the fields that survive validation are emitted; empties are omitted.
type siteCustomization struct {
	Title      string `json:"title,omitempty"`
	Accent     string `json:"accent,omitempty"`
	AccentDark string `json:"accentDark,omitempty"`
	Favicon    string `json:"favicon,omitempty"`
}

// ValidSiteAccent reports whether v is a strict #rgb/#rrggbb hex color.
func ValidSiteAccent(v string) bool { return siteHexRe.MatchString(v) }

// ValidSiteFavicon reports whether v is an allowed favicon data URI (png/webp/
// svg+xml) within the size cap.
func ValidSiteFavicon(v string) bool {
	return len(v) <= SiteFaviconMaxBytes && siteFaviconRe.MatchString(v)
}

// validateSiteCustomization keeps only the fields that pass strict validation,
// dropping anything malformed field-by-field (a bad accent never poisons a good
// title). Returns ok=false when nothing survives, so the caller deletes the
// artifact rather than emit an empty object.
func validateSiteCustomization(raw map[string]interface{}) (siteCustomization, bool) {
	var c siteCustomization
	if s, ok := raw["title"].(string); ok {
		s = strings.TrimSpace(s)
		if len(s) > SiteConfigMaxTitle {
			s = s[:SiteConfigMaxTitle]
		}
		c.Title = s
	}
	if s, ok := raw["accent"].(string); ok && ValidSiteAccent(s) {
		c.Accent = s
	}
	if s, ok := raw["accentDark"].(string); ok && ValidSiteAccent(s) {
		c.AccentDark = s
	}
	if s, ok := raw["favicon"].(string); ok && ValidSiteFavicon(s) {
		c.Favicon = s
	}
	if c.Title == "" && c.Accent == "" && c.AccentDark == "" && c.Favicon == "" {
		return siteCustomization{}, false
	}
	return c, true
}

// readSiteCustomization resolves refs/gitmsg/core/config from the bucket's
// objects and extracts its validated `site` sub-object. Returns ok=false (no
// error) when the ref is absent, the object is missing/not a commit, the message
// is not valid config JSON, or nothing in `site` survives validation — the
// caller then deletes the artifact (reader falls back to its defaults).
func readSiteCustomization(client *Client, prefix string, refs map[string]string) (siteCustomization, bool, error) {
	sha, present := refs["refs/gitmsg/core/config"]
	if !present || len(sha) != 40 {
		return siteCustomization{}, false, nil
	}
	c, err := getBucketCommit(client, prefix, sha)
	if err != nil {
		return siteCustomization{}, false, err
	}
	var cfg map[string]interface{}
	if json.Unmarshal([]byte(strings.TrimSpace(c.item.Message)), &cfg) != nil {
		return siteCustomization{}, false, nil
	}
	site, ok := cfg["site"].(map[string]interface{})
	if !ok {
		return siteCustomization{}, false, nil
	}
	valid, ok := validateSiteCustomization(site)
	return valid, ok, nil
}

// writeSiteCustomization publishes the validated site customization at
// .gitsocial/site/site-config.json after every push, so the static site honors
// the repo's refs/gitmsg/core/config `site` sub-object. Absent/malformed config
// deletes the artifact (reader falls back to its defaults). Best-effort by
// contract; written on the same refs-moved path that maintains refs.json.
func writeSiteCustomization(client *Client, prefix string, refs map[string]string) error {
	cfg, ok, err := readSiteCustomization(client, prefix, refs)
	if err != nil {
		return err
	}
	if !ok {
		return client.Delete(prefix + siteCustomizationKey)
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal site customization: %w", err)
	}
	resp, err := client.do(http.MethodPut, prefix+siteCustomizationKey, nil, data, map[string]string{"Content-Type": "application/json"})
	if err != nil {
		return fmt.Errorf("upload %s: %w", siteCustomizationKey, err)
	}
	resp.Body.Close()
	return nil
}

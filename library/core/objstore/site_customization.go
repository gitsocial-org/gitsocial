// site_customization.go - push-maintained static-site customization artifact.
//
// The repo's site customization lives at refs/gitmsg/core/config as a commit
// whose message is the core config JSON with a `site` sub-object (title, accent,
// accentDark, favicon, url, description, publish, pages). This resolves that sub-object at push time, validates it
// strictly, and emits it as .gitsocial/site/site-config.json alongside the other
// mutable site artifacts. The reader loads it (no-cache, refreshed on every push)
// and applies the overrides; an absent or malformed config deletes the artifact
// (the reader falls back to its built-in defaults), mirroring pm-config.json.

package objstore

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
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

// SiteConfigMaxURL bounds the site base URL (site.url), after normalization.
const SiteConfigMaxURL = 500

// SiteConfigMaxDescription bounds the site description (site.description).
const SiteConfigMaxDescription = 300

// siteHexRe matches a strict CSS hex color (#rgb or #rrggbb), the only accent
// shape the writer emits and the reader applies.
var siteHexRe = regexp.MustCompile(`^#(?:[0-9a-fA-F]{3}|[0-9a-fA-F]{6})$`)

// siteFaviconRe matches an allowed favicon data URI prefix (png/webp/svg+xml).
// Kept in sync with the reader's favicon guard in gs-app.js.
var siteFaviconRe = regexp.MustCompile(`^data:image/(png|webp|svg\+xml)[;,]`)

// Per-remote site-override git config keys, stored on the remote definition
// (remote.<name>.<suffix>) as machine-local deployment state. Only the
// deployment keys (url, publish, pages) are overridable; identity keys stay
// shared in the repo's config ref. See documentation/STATIC-SITE.md.
const (
	SiteOverrideURLKey     = "gitsocial-site-url"
	SiteOverridePublishKey = "gitsocial-site-publish"
	SiteOverridePagesKey   = "gitsocial-site-pages"
)

// SiteOverride carries a remote's per-remote deployment-key overrides; each ""
// field means "not overridden" (the repo config value stands). Applied over
// readSiteCustomization's result at that single boundary so every consumer
// (guards, canonical/OG URL, siteHash, site-config.json) sees effective values.
type SiteOverride struct {
	URL     string
	Publish string
	Pages   string
}

// applySiteOverride overlays a remote's deployment overrides onto a resolved
// customization, normalizing each override the same way the shared keys are
// (url through NormalizeSiteURL, publish/pages through siteBoolString). An
// override can turn a previously-empty customization into a publishable one
// (ok=true), matching the single-boundary contract.
func applySiteOverride(c siteCustomization, ok bool, ov SiteOverride) (siteCustomization, bool) {
	if ov == (SiteOverride{}) {
		return c, ok
	}
	if ov.URL != "" {
		if norm, valid := NormalizeSiteURL(ov.URL); valid {
			c.URL = norm
		}
	}
	if ov.Publish != "" {
		if b := siteBoolString(ov.Publish); b != "" {
			c.Publish = b
		}
	}
	if ov.Pages != "" {
		if b := siteBoolString(ov.Pages); b != "" {
			c.Pages = b
		}
	}
	if c == (siteCustomization{}) {
		return siteCustomization{}, false
	}
	return c, true
}

// siteCustomization is the validated customization the reader consumes: a title,
// an accent (light) and optional accentDark, an optional favicon data URI, the
// site's canonical base URL, a description, and the two publish guards. Only the
// fields that survive validation are emitted; empties are omitted.
type siteCustomization struct {
	Title       string `json:"title,omitempty"`
	Accent      string `json:"accent,omitempty"`
	AccentDark  string `json:"accentDark,omitempty"`
	Favicon     string `json:"favicon,omitempty"`
	URL         string `json:"url,omitempty"`
	Description string `json:"description,omitempty"`
	Publish     string `json:"publish,omitempty"` // "true" enables the static site (default off)
	Pages       string `json:"pages,omitempty"`   // "true" enables the HTML page layer (needs publish + url)
}

// siteBoolString normalizes a raw guard value to "true"/"false" ("" when it is
// neither — the guard is then treated as unset, i.e. off).
func siteBoolString(v interface{}) string {
	switch t := v.(type) {
	case bool:
		if t {
			return "true"
		}
		return "false"
	case string:
		if s := strings.TrimSpace(t); s == "true" || s == "false" {
			return s
		}
	}
	return ""
}

// ValidSiteAccent reports whether v is a strict #rgb/#rrggbb hex color.
func ValidSiteAccent(v string) bool { return siteHexRe.MatchString(v) }

// ValidSiteFavicon reports whether v is an allowed favicon data URI (png/webp/
// svg+xml) within the size cap.
func ValidSiteFavicon(v string) bool {
	return len(v) <= SiteFaviconMaxBytes && siteFaviconRe.MatchString(v)
}

// NormalizeSiteURL validates and normalizes a site base URL: absolute https
// (http only for localhost/127.0.0.1, the locals3 dev loop), no query or
// fragment, normalized to a trailing slash, within the length cap. Returns
// ok=false when invalid.
func NormalizeSiteURL(v string) (string, bool) {
	v = strings.TrimSpace(v)
	u, err := url.Parse(v)
	if err != nil || u.Host == "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return "", false
	}
	switch u.Scheme {
	case "https":
	case "http":
		if h := u.Hostname(); h != "localhost" && h != "127.0.0.1" {
			return "", false
		}
	default:
		return "", false
	}
	if !strings.HasSuffix(v, "/") {
		v += "/"
	}
	if len(v) > SiteConfigMaxURL {
		return "", false
	}
	return v, true
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
	if s, ok := raw["url"].(string); ok {
		if norm, valid := NormalizeSiteURL(s); valid {
			c.URL = norm
		}
	}
	if s, ok := raw["description"].(string); ok {
		s = strings.TrimSpace(s)
		if len(s) > SiteConfigMaxDescription {
			s = s[:SiteConfigMaxDescription]
		}
		c.Description = s
	}
	if v, ok := raw["publish"]; ok {
		c.Publish = siteBoolString(v)
	}
	if v, ok := raw["pages"]; ok {
		c.Pages = siteBoolString(v)
	}
	if c == (siteCustomization{}) {
		return siteCustomization{}, false
	}
	return c, true
}

// readSiteCustomization resolves the bucket's site customization and overlays
// the per-remote deployment overrides (ov) at this single boundary, so every
// consumer sees effective values. See readSiteBaseCustomization for the base
// resolution and applySiteOverride for the override rules.
func readSiteCustomization(client *Client, prefix string, refs map[string]string, ov SiteOverride) (siteCustomization, bool, error) {
	base, ok, err := readSiteBaseCustomization(client, prefix, refs)
	if err != nil {
		return siteCustomization{}, false, err
	}
	c, ok := applySiteOverride(base, ok, ov)
	return c, ok, nil
}

// readSiteBaseCustomization resolves refs/gitmsg/core/config from the bucket's
// objects and extracts its validated `site` sub-object (no per-remote overrides
// applied). Returns ok=false (no error) when the ref is absent, the object is
// missing/not a commit, the message is not valid config JSON, or nothing in
// `site` survives validation — the caller then deletes the artifact (reader
// falls back to its defaults).
func readSiteBaseCustomization(client *Client, prefix string, refs map[string]string) (siteCustomization, bool, error) {
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
func writeSiteCustomization(client *Client, prefix string, refs map[string]string, ov SiteOverride) error {
	cfg, ok, err := readSiteCustomization(client, prefix, refs, ov)
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

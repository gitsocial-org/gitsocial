// site_customization.go - resolving the repo's `site` config sub-object at push time and publishing it as a site artifact

package site

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/gitsocial-org/gitsocial/library/core/objstore"
)

// siteCustomizationKey is the site customization the static site reads.
const siteCustomizationKey = ".gitsocial/site/site-config.json"

// siteConfigMaxTitle bounds a customization title.
const siteConfigMaxTitle = 200

// SiteFaviconMaxBytes caps the favicon data URI, so the no-cache artifact stays small.
const SiteFaviconMaxBytes = 32 * 1024

// siteConfigMaxURL bounds the site base URL (site.url), after normalization.
const siteConfigMaxURL = 500

// SiteConfigMaxDescription bounds the site description (site.description).
const SiteConfigMaxDescription = 300

// siteHexRe matches a strict CSS hex color, the only accent shape the writer emits.
var siteHexRe = regexp.MustCompile(`^#(?:[0-9a-fA-F]{3}|[0-9a-fA-F]{6})$`)

// siteFaviconRe matches an allowed favicon data URI prefix, in step with the reader's guard in gs-app.js.
var siteFaviconRe = regexp.MustCompile(`^data:image/(png|webp|svg\+xml)[;,]`)

// siteImageKeyRe matches a relative bucket key for site.image: plain segments, no scheme, no leading slash, no traversal.
var siteImageKeyRe = regexp.MustCompile(`^[A-Za-z0-9._-]+(?:/[A-Za-z0-9._-]+)*$`)

// applySiteOverride overlays a remote's deployment overrides onto a resolved customization, normalizing each the way the shared keys are.
func applySiteOverride(c siteCustomization, ok bool, ov objstore.SiteOverride) (siteCustomization, bool) {
	if ov == (objstore.SiteOverride{}) {
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

// siteCustomization is the validated customization the reader consumes; only the fields that survive validation are emitted.
type siteCustomization struct {
	Title       string `json:"title,omitempty"`
	Accent      string `json:"accent,omitempty"`
	AccentDark  string `json:"accentDark,omitempty"`
	Favicon     string `json:"favicon,omitempty"`
	Image       string `json:"image,omitempty"`
	URL         string `json:"url,omitempty"`
	Description string `json:"description,omitempty"`
	Publish     string `json:"publish,omitempty"` // "true" enables the static site (default off)
	Pages       string `json:"pages,omitempty"`   // "true" enables the HTML page layer (needs publish + url)
	// The file layer's globs, comma-separated strings so the type stays comparable.
	FilesInclude string `json:"filesInclude,omitempty"`
	FilesExclude string `json:"filesExclude,omitempty"`
}

// siteBoolString normalizes a raw guard value to "true" or "false"; "" means unset, so the guard is off.
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

// NormalizeSiteGlobs keeps the well-formed globs of a comma-separated list, so a glob can only select inside the published tree.
func NormalizeSiteGlobs(v string) string {
	var kept []string
	for _, g := range strings.Split(v, ",") {
		g = strings.TrimSpace(g)
		if g == "" || strings.HasPrefix(g, "/") || g == ".." || strings.HasPrefix(g, "../") || strings.Contains(g, "/../") {
			continue
		}
		kept = append(kept, g)
	}
	return strings.Join(kept, ",")
}

// ValidSiteAccent reports whether v is a strict #rgb/#rrggbb hex color.
func ValidSiteAccent(v string) bool { return siteHexRe.MatchString(v) }

// ValidSiteFavicon reports whether v is an allowed favicon data URI within the size cap.
func ValidSiteFavicon(v string) bool {
	return len(v) <= SiteFaviconMaxBytes && siteFaviconRe.MatchString(v)
}

// NormalizeSiteImage validates a site.image value: an absolute URL under site.url's scheme rules, or a relative bucket key.
func NormalizeSiteImage(v string) (string, bool) {
	v = strings.TrimSpace(v)
	if v == "" || len(v) > siteConfigMaxURL {
		return "", false
	}
	if strings.Contains(v, "://") {
		u, err := url.Parse(v)
		if err != nil || u.Host == "" {
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
		return v, true
	}
	if !siteImageKeyRe.MatchString(v) {
		return "", false
	}
	return v, true
}

// NormalizeSiteURL validates a site base URL: absolute https, or http for a loopback host, with no query or fragment and a trailing slash.
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
	if len(v) > siteConfigMaxURL {
		return "", false
	}
	return v, true
}

// validateSiteCustomization keeps only the fields that validate, dropping the rest one by one; ok is false when nothing survives.
func validateSiteCustomization(raw map[string]interface{}) (siteCustomization, bool) {
	var c siteCustomization
	if s, ok := raw["title"].(string); ok {
		s = strings.TrimSpace(s)
		if len(s) > siteConfigMaxTitle {
			s = s[:siteConfigMaxTitle]
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
	if s, ok := raw["image"].(string); ok {
		if norm, valid := NormalizeSiteImage(s); valid {
			c.Image = norm
		}
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
	if s, ok := raw["filesInclude"].(string); ok {
		c.FilesInclude = NormalizeSiteGlobs(s)
	}
	if s, ok := raw["filesExclude"].(string); ok {
		c.FilesExclude = NormalizeSiteGlobs(s)
	}
	if c == (siteCustomization{}) {
		return siteCustomization{}, false
	}
	return c, true
}

// readSiteCustomization resolves the bucket's site customization and overlays the per-remote overrides at this one boundary, so every consumer sees effective values.
func readSiteCustomization(client *objstore.Client, prefix string, refs map[string]string, ov objstore.SiteOverride, src *objstore.LocalCommitSource) (siteCustomization, bool, error) {
	base, ok, err := readSiteBaseCustomization(client, prefix, refs, src)
	if err != nil {
		return siteCustomization{}, false, err
	}
	c, ok := applySiteOverride(base, ok, ov)
	return c, ok, nil
}

// readSiteBaseCustomization resolves refs/gitmsg/core/config and extracts its validated `site` sub-object, with no overrides applied; ok is false when nothing survives.
func readSiteBaseCustomization(client *objstore.Client, prefix string, refs map[string]string, src *objstore.LocalCommitSource) (siteCustomization, bool, error) {
	sha, present := refs["refs/gitmsg/core/config"]
	if !present || len(sha) != 40 {
		return siteCustomization{}, false, nil
	}
	c, err := getCommit(src, client, prefix, sha)
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

// writeSiteCustomization publishes the validated site customization after every push; an absent or malformed config deletes the artifact instead.
func writeSiteCustomization(client *objstore.Client, prefix string, refs map[string]string, ov objstore.SiteOverride, src *objstore.LocalCommitSource) error {
	cfg, ok, err := readSiteCustomization(client, prefix, refs, ov, src)
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
	if err := client.PutWithHeaders(prefix+siteCustomizationKey, data, map[string]string{"Content-Type": "application/json"}); err != nil {
		return fmt.Errorf("upload %s: %w", siteCustomizationKey, err)
	}
	return nil
}

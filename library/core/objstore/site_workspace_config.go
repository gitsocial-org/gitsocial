// site_workspace_config.go - read/write the workspace's site customization.
//
// The site customization the static site serves lives in the `site` sub-object
// of the core config commit at refs/gitmsg/core/config (see site_customization.go
// for the push-time reader/writer that resolves it from a bucket). These helpers
// are the workspace-side source of truth: `gitsocial site push` publishes whatever
// they store, so the TUI reads and writes the same place. Fields are validated
// with the same strict rules the push writer and the browser reader apply.

package objstore

import (
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
)

// coreConfigSiteKey is the sub-object under the core config that holds the site
// customization.
const coreConfigSiteKey = "site"

// SiteCustomization is the workspace-editable site customization: a title, a
// light accent and optional dark accent (both #rgb/#rrggbb hex), and an optional
// favicon data URI. Empty fields are omitted on write (and drop the artifact when
// all are empty), mirroring the push-time validation.
type SiteCustomization struct {
	Title      string `json:"title,omitempty"`
	Accent     string `json:"accent,omitempty"`
	AccentDark string `json:"accentDark,omitempty"`
	Favicon    string `json:"favicon,omitempty"`
}

// ReadWorkspaceSiteCustomization returns the `site` sub-object of the workspace's
// core config, keeping only fields that pass validation. Missing config or a
// missing/empty site sub-object returns a zero-value customization (no error).
func ReadWorkspaceSiteCustomization(workdir string) (SiteCustomization, error) {
	config, err := gitmsg.ReadExtConfig(workdir, "core")
	if err != nil {
		return SiteCustomization{}, err
	}
	site, _ := config[coreConfigSiteKey].(map[string]interface{})
	valid, _ := validateSiteCustomization(site)
	return SiteCustomization(valid), nil
}

// WriteWorkspaceSiteCustomization stores the site customization into the `site`
// sub-object of the workspace's core config. Empty fields are dropped; when every
// field is empty the sub-object is removed entirely, so the next push deletes the
// artifact and the site falls back to its defaults.
func WriteWorkspaceSiteCustomization(workdir string, c SiteCustomization) error {
	config, err := gitmsg.ReadExtConfig(workdir, "core")
	if err != nil {
		return err
	}
	if config == nil {
		config = map[string]interface{}{}
	}
	site := map[string]interface{}{}
	if c.Title != "" {
		site["title"] = c.Title
	}
	if c.Accent != "" {
		site["accent"] = c.Accent
	}
	if c.AccentDark != "" {
		site["accentDark"] = c.AccentDark
	}
	if c.Favicon != "" {
		site["favicon"] = c.Favicon
	}
	if len(site) == 0 {
		delete(config, coreConfigSiteKey)
	} else {
		config[coreConfigSiteKey] = site
	}
	return gitmsg.WriteExtConfig(workdir, "core", config)
}

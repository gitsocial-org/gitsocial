// site_workspace_config.go - read/write the workspace's site customization.
//
// The site customization the static site serves lives in the `site` sub-object
// of the core config commit at refs/gitmsg/core/config (see site_customization.go
// for the push-time reader/writer that resolves it from a bucket). These helpers
// are the workspace-side source of truth: `gitsocial push --site-only` publishes whatever
// they store, so the TUI reads and writes the same place. Fields are validated
// with the same strict rules the push writer and the browser reader apply.

package site

import (
	"encoding/json"
	"fmt"

	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
)

// coreConfigSiteKey is the sub-object under the core config that holds the site
// customization.
const coreConfigSiteKey = "site"

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
	return valid, nil
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
	data, err := json.Marshal(c)
	if err != nil {
		return fmt.Errorf("marshal site customization: %w", err)
	}
	var site map[string]interface{}
	if err := json.Unmarshal(data, &site); err != nil {
		return fmt.Errorf("decode site customization: %w", err)
	}
	if len(site) == 0 {
		delete(config, coreConfigSiteKey)
	} else {
		config[coreConfigSiteKey] = site
	}
	return gitmsg.WriteExtConfig(workdir, "core", config)
}

// config.go - CLI commands for managing extension configuration
package main

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/objstore"

	"github.com/gitsocial-org/gitsocial/library/core/site"
)

const coreExt = "core"

// NewExtConfigCmd creates a config command with get/set/list subcommands for the given extension.
func NewExtConfigCmd(ext string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: fmt.Sprintf("Manage %s extension configuration", ext),
	}
	cmd.AddCommand(
		newExtConfigGetCmd(ext),
		newExtConfigSetCmd(ext),
		newExtConfigListCmd(ext),
	)
	// The core config carries the static-site customization (title/accent/
	// favicon) under a `site` sub-object; expose it as `config site ...`.
	// Per-endpoint S3 credentials live beside it as `config credentials ...`.
	if ext == coreExt {
		cmd.AddCommand(newSiteConfigCmd())
		cmd.AddCommand(newCredentialsConfigCmd())
	}
	return cmd
}

// newExtConfigGetCmd builds the command that prints one extension config value.
func newExtConfigGetCmd(ext string) *cobra.Command {
	return &cobra.Command{
		Use:   "get <key>",
		Short: "Get a config value",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !EnsureGitRepo(cmd) {
				return exit(ExitNotRepo)
			}
			cfg := GetConfig(cmd)
			key := args[0]
			value, ok := gitmsg.GetExtConfigValue(cfg.WorkDir, ext, key)
			if !ok {
				PrintError(cmd, fmt.Sprintf("key not found: %s", key))
				return exit(ExitError)
			}
			if cfg.JSONOutput {
				return PrintJSON(cmd, map[string]string{"key": key, "value": value})
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), value)
			}
			return nil
		},
	}
}

// newExtConfigSetCmd builds the command that sets one extension config value.
func newExtConfigSetCmd(ext string) *cobra.Command {
	return &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a config value",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !EnsureGitRepo(cmd) {
				return exit(ExitNotRepo)
			}
			cfg := GetConfig(cmd)
			key := args[0]
			value := args[1]
			if err := gitmsg.SetExtConfigValue(cfg.WorkDir, ext, key, value); err != nil {
				PrintError(cmd, err.Error())
				return exit(ExitError)
			}
			if cfg.JSONOutput {
				return PrintJSON(cmd, map[string]string{"key": key, "value": value})
			} else {
				PrintSuccess(cmd, fmt.Sprintf("%s = %s", key, value))
			}
			return nil
		},
	}
}

// newExtConfigListCmd builds the command that lists an extension's config values.
func newExtConfigListCmd(ext string) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all config values",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !EnsureGitRepo(cmd) {
				return exit(ExitNotRepo)
			}
			cfg := GetConfig(cmd)
			items := gitmsg.ListExtConfig(cfg.WorkDir, ext)
			if cfg.JSONOutput {
				return PrintJSON(cmd, items)
			} else {
				if len(items) == 0 {
					fmt.Fprintln(cmd.OutOrStdout(), "No config set")
					return nil
				}
				for _, item := range items {
					fmt.Fprintf(cmd.OutOrStdout(), "%s = %s\n", item.Key, item.Value)
				}
			}
			return nil
		},
	}
}

// siteConfigKeys are the customization fields settable under the core config's
// `site` sub-object, published as the static site's site-config.json artifact.
var siteConfigKeys = map[string]bool{"title": true, "accent": true, "accentDark": true, "favicon": true, "image": true, "url": true, "description": true, "publish": true, "pages": true, "filesInclude": true, "filesExclude": true}

// siteOverrideKeys maps the per-remote-overridable deployment keys to their git
// config suffix (remote.<name>.<suffix>). Only these three deployment keys are
// overridable per remote; identity keys stay shared in the config ref.
var siteOverrideKeys = map[string]string{
	"url":     objstore.SiteOverrideURLKey,
	"publish": objstore.SiteOverridePublishKey,
	"pages":   objstore.SiteOverridePagesKey,
}

// readRemoteSiteOverrideValue returns a remote's override for a deployment key
// from git config (remote.<name>.<suffix>), or "" when unset.
func readRemoteSiteOverrideValue(workdir, remote, key string) string {
	suffix, ok := siteOverrideKeys[key]
	if !ok {
		return ""
	}
	out, err := git.ExecGit(workdir, []string{"config", "--get", "remote." + remote + "." + suffix})
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out.Stdout)
}

// effectiveSiteConfigMap returns the site customization for a remote: the config
// ref's `site` sub-object overlaid by that remote's per-remote deployment
// overrides (url/publish/pages). remote "" returns the config ref map unchanged.
func effectiveSiteConfigMap(workdir, remote string) map[string]interface{} {
	site := readSiteConfigMap(workdir)
	if remote == "" {
		return site
	}
	merged := map[string]interface{}{}
	for k, v := range site {
		merged[k] = v
	}
	for key := range siteOverrideKeys {
		if v := readRemoteSiteOverrideValue(workdir, remote, key); v != "" {
			merged[key] = v
		}
	}
	return merged
}

// newSiteConfigCmd creates the `config site` group for the static-site
// customization stored under the `site` sub-object of refs/gitmsg/core/config.
func newSiteConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "site",
		Short: "Manage static site customization",
		Long: `Set the browser site's title, accent color, favicon and the other site
keys. Values live in the site object of the core config ref and reach the
bucket as .gitsocial/site/site-config.json on the next push.

Keys:
  title         plain text for the tab title and the header
  description   plain text, up to 300 characters
  accent        accent color, #rgb or #rrggbb
  accentDark    accent color for dark mode, same form
  favicon       @path/to/icon.png or a data: URI; png, webp or svg, 32KB
  image         og:image for every page: a bucket key or an https:// URL
  url           the public base URL, absolute https://, trailing slash
  publish       true or false, default false: the site master switch
  pages         true or false, default false: the crawlable HTML pages
  filesInclude  comma-separated globs the file pages also publish
  filesExclude  comma-separated globs the file pages skip

Validation is in documentation/STATIC-SITE.md.`,
	}
	cmd.AddCommand(newSiteConfigGetCmd(), newSiteConfigSetCmd(), newSiteConfigListCmd())
	return cmd
}

// writeSiteConfigValue stores one already-validated site customization value
// under the core config's `site` sub-object. WriteExtConfig skips the commit
// when the stored content is unchanged, so repeated sets are no-ops.
func writeSiteConfigValue(workdir, key, resolved string) error {
	full, _ := gitmsg.ReadExtConfig(workdir, coreExt)
	if full == nil {
		full = map[string]interface{}{}
	}
	site, _ := full["site"].(map[string]interface{})
	if site == nil {
		site = map[string]interface{}{}
	}
	site[key] = resolved
	full["site"] = site
	return gitmsg.WriteExtConfig(workdir, coreExt, full)
}

// readSiteConfigMap returns the core config's `site` sub-object (never nil).
func readSiteConfigMap(workdir string) map[string]interface{} {
	cfg, _ := gitmsg.ReadExtConfig(workdir, coreExt)
	if cfg != nil {
		if site, ok := cfg["site"].(map[string]interface{}); ok {
			return site
		}
	}
	return map[string]interface{}{}
}

// newSiteConfigGetCmd builds the command that prints one site customization value.
func newSiteConfigGetCmd() *cobra.Command {
	var remote string
	cmd := &cobra.Command{
		Use:   "get <key>",
		Short: "Get a site customization value",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !EnsureGitRepo(cmd) {
				return exit(ExitNotRepo)
			}
			cfg := GetConfig(cmd)
			key := args[0]
			site := effectiveSiteConfigMap(cfg.WorkDir, remote)
			val, ok := site[key].(string)
			if !ok || val == "" {
				PrintError(cmd, fmt.Sprintf("key not found: %s", key))
				return exit(ExitError)
			}
			if cfg.JSONOutput {
				return PrintJSON(cmd, map[string]string{"key": key, "value": val})
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), val)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&remote, "remote", "", "Show the effective value for this remote")
	return cmd
}

// newSiteConfigListCmd builds the command that lists site customization values.
func newSiteConfigListCmd() *cobra.Command {
	var remote string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List site customization values",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !EnsureGitRepo(cmd) {
				return exit(ExitNotRepo)
			}
			cfg := GetConfig(cmd)
			site := effectiveSiteConfigMap(cfg.WorkDir, remote)
			if cfg.JSONOutput {
				return PrintJSON(cmd, site)
			}
			if len(site) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No site customization set")
				return nil
			}
			for _, k := range []string{"title", "accent", "accentDark", "favicon", "image", "url", "description", "publish", "pages", "filesInclude", "filesExclude"} {
				if v, ok := site[k].(string); ok && v != "" {
					fmt.Fprintf(cmd.OutOrStdout(), "%s = %s\n", k, siteConfigDisplay(k, v))
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&remote, "remote", "", "Show the effective values for this remote")
	return cmd
}

// siteConfigDisplay truncates a long favicon data URI for readable listing.
func siteConfigDisplay(key, value string) string {
	if key == "favicon" && len(value) > 48 {
		return value[:45] + fmt.Sprintf("... (%d bytes)", len(value))
	}
	return value
}

// newSiteConfigSetCmd builds the command that sets a site customization value.
func newSiteConfigSetCmd() *cobra.Command {
	var remote string
	cmd := &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a site customization value",
		Long: `Set a site customization value. Valid keys: title, description, accent,
accentDark, favicon, image, url, publish, pages, filesInclude,
filesExclude.

  accent, accentDark  #rgb or #rrggbb, such as #0a7 or #00dddd
  favicon             @path/to/icon.png to read and encode a png, webp
                      or svg image, or a data: URI; 32KB max
  image               a bucket key relative to the site root, uploaded
                      with gitsocial remote put, or an https:// URL
  url                 absolute https://, http:// for localhost only, no
                      query or fragment, normalized to a trailing slash
  description         plain text, 300 characters max
  publish, pages      true or false, both default false; pages needs
                      publish true and a valid url

With --remote <name> the value is stored in git config as
remote.<name>.gitsocial-site-<key> and applies only to pushes to that
remote. Only url, publish and pages are overridable per remote.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !EnsureGitRepo(cmd) {
				return exit(ExitNotRepo)
			}
			cfg := GetConfig(cmd)
			key, value := args[0], args[1]
			if remote != "" {
				return setRemoteSiteOverride(cmd, cfg, remote, key, value)
			}
			if !siteConfigKeys[key] {
				PrintError(cmd, fmt.Sprintf("unknown site key %q: run gitsocial config site set --help", key))
				return exit(ExitError)
			}
			resolved, err := resolveSiteConfigValue(key, value)
			if err != nil {
				PrintError(cmd, err.Error())
				return exit(ExitError)
			}
			if err := writeSiteConfigValue(cfg.WorkDir, key, resolved); err != nil {
				PrintError(cmd, err.Error())
				return exit(ExitError)
			}
			if cfg.JSONOutput {
				return PrintJSON(cmd, map[string]string{"key": key, "value": siteConfigDisplay(key, resolved)})
			} else {
				PrintSuccess(cmd, fmt.Sprintf("%s = %s", key, siteConfigDisplay(key, resolved)))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&remote, "remote", "", "Store the value per remote in git config")
	return cmd
}

// setRemoteSiteOverride writes a per-remote deployment override to git config
// (remote.<name>.gitsocial-site-<key>). Only the deployment keys url/publish/
// pages are overridable, and each is validated with the same rules as the shared
// config key before it is stored.
func setRemoteSiteOverride(cmd *cobra.Command, cfg *Config, remote, key, value string) error {
	suffix, ok := siteOverrideKeys[key]
	if !ok {
		PrintError(cmd, fmt.Sprintf("%q is not overridable per remote: use url, publish or pages", key))
		return exit(ExitError)
	}
	if _, err := git.ExecGit(cfg.WorkDir, []string{"remote", "get-url", remote}); err != nil {
		PrintError(cmd, fmt.Sprintf("remote %q does not exist", remote))
		return exit(ExitError)
	}
	resolved, err := resolveSiteConfigValue(key, value)
	if err != nil {
		PrintError(cmd, err.Error())
		return exit(ExitError)
	}
	if _, err := git.ExecGit(cfg.WorkDir, []string{"config", "remote." + remote + "." + suffix, resolved}); err != nil {
		PrintError(cmd, fmt.Sprintf("set remote.%s.%s: %v", remote, suffix, err))
		return exit(ExitError)
	}
	if cfg.JSONOutput {
		return PrintJSON(cmd, map[string]string{"remote": remote, "key": key, "value": resolved})
	} else {
		PrintSuccess(cmd, fmt.Sprintf("remote %q: %s = %s", remote, key, resolved))
	}
	return nil
}

// resolveSiteConfigValue validates (and for a favicon, loads/encodes) a raw CLI
// value into what is stored: accent colors must be strict hex; a favicon may be
// an @path to a raw image (base64-encoded into a data URI) or a data URI, of an
// allowed type (png/webp/svg+xml) within the 32KB cap; a url is normalized to a
// trailing slash; a description is trimmed and length-checked.
func resolveSiteConfigValue(key, value string) (string, error) {
	switch key {
	case "accent", "accentDark":
		if !site.ValidSiteAccent(value) {
			return "", fmt.Errorf("%s must be a #rgb or #rrggbb hex color, got %q", key, value)
		}
		return value, nil
	case "favicon":
		return resolveFaviconValue(value)
	case "image":
		norm, ok := site.NormalizeSiteImage(value)
		if !ok {
			return "", fmt.Errorf("image must be a bucket key like og-card.png or an absolute https:// URL, got %q", value)
		}
		return norm, nil
	case "url":
		norm, ok := site.NormalizeSiteURL(value)
		if !ok {
			return "", fmt.Errorf("url must be an absolute https:// URL with no query or fragment, got %q", value)
		}
		return norm, nil
	case "description":
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return "", fmt.Errorf("description must not be empty")
		}
		if len(trimmed) > site.SiteConfigMaxDescription {
			return "", fmt.Errorf("description is %d chars, over the %d-char cap", len(trimmed), site.SiteConfigMaxDescription)
		}
		return trimmed, nil
	case "publish", "pages":
		v := strings.ToLower(strings.TrimSpace(value))
		if v != "true" && v != "false" {
			return "", fmt.Errorf("%s must be true or false, got %q", key, value)
		}
		return v, nil
	case "filesInclude", "filesExclude":
		globs := site.NormalizeSiteGlobs(value)
		if globs == "" {
			return "", fmt.Errorf("%s must be comma-separated repo-relative path globs, got %q", key, value)
		}
		return globs, nil
	default:
		return value, nil
	}
}

// resolveFaviconValue turns an @path image or a data: URI into a validated
// favicon data URI. A raw file is read, its type detected from its bytes, and
// base64-encoded; either form must be an allowed image type within the cap.
func resolveFaviconValue(value string) (string, error) {
	dataURI := value
	if strings.HasPrefix(value, "@") {
		data, err := os.ReadFile(strings.TrimPrefix(value, "@"))
		if err != nil {
			return "", fmt.Errorf("read favicon file: %w", err)
		}
		mime := faviconMIME(strings.TrimPrefix(value, "@"), data)
		if mime == "" {
			return "", fmt.Errorf("unsupported favicon type: only png, webp, and svg are allowed")
		}
		dataURI = "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data)
	}
	if !site.ValidSiteFavicon(dataURI) {
		if len(dataURI) > site.SiteFaviconMaxBytes {
			return "", fmt.Errorf("favicon is %d bytes, over the %d-byte cap", len(dataURI), site.SiteFaviconMaxBytes)
		}
		return "", fmt.Errorf("favicon must be a data: URI of type png, webp, or svg+xml")
	}
	return dataURI, nil
}

// faviconMIME detects an allowed favicon MIME type from a file's bytes (and its
// name for SVG, which http.DetectContentType reports as text/xml), or "" when
// the type is not an allowed image.
func faviconMIME(name string, data []byte) string {
	if strings.HasSuffix(strings.ToLower(name), ".svg") || strings.Contains(string(firstBytes(data, 512)), "<svg") {
		return "image/svg+xml"
	}
	switch http.DetectContentType(data) {
	case "image/png":
		return "image/png"
	case "image/webp":
		return "image/webp"
	default:
		return ""
	}
}

// firstBytes returns up to n leading bytes of b.
func firstBytes(b []byte, n int) []byte {
	if len(b) < n {
		return b
	}
	return b[:n]
}

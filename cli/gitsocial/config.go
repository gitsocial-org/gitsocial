// config.go - CLI commands for managing extension configuration
package main

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/objstore"
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
	if ext == coreExt {
		cmd.AddCommand(newSiteConfigCmd())
	}
	return cmd
}

func newExtConfigGetCmd(ext string) *cobra.Command {
	return &cobra.Command{
		Use:   "get <key>",
		Short: "Get a config value",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			if !EnsureGitRepo(cmd) {
				os.Exit(ExitNotRepo)
			}
			cfg := GetConfig(cmd)
			key := args[0]
			value, ok := gitmsg.GetExtConfigValue(cfg.WorkDir, ext, key)
			if !ok {
				PrintError(cmd, fmt.Sprintf("key not found: %s", key))
				os.Exit(ExitError)
			}
			if cfg.JSONOutput {
				PrintJSON(map[string]string{"key": key, "value": value})
			} else {
				fmt.Println(value)
			}
		},
	}
}

func newExtConfigSetCmd(ext string) *cobra.Command {
	return &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a config value",
		Args:  cobra.ExactArgs(2),
		Run: func(cmd *cobra.Command, args []string) {
			if !EnsureGitRepo(cmd) {
				os.Exit(ExitNotRepo)
			}
			cfg := GetConfig(cmd)
			key := args[0]
			value := args[1]
			if err := gitmsg.SetExtConfigValue(cfg.WorkDir, ext, key, value); err != nil {
				PrintError(cmd, err.Error())
				os.Exit(ExitError)
			}
			if cfg.JSONOutput {
				PrintJSON(map[string]string{"key": key, "value": value})
			} else {
				PrintSuccess(cmd, fmt.Sprintf("%s = %s", key, value))
			}
		},
	}
}

func newExtConfigListCmd(ext string) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all config values",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			if !EnsureGitRepo(cmd) {
				os.Exit(ExitNotRepo)
			}
			cfg := GetConfig(cmd)
			items := gitmsg.ListExtConfig(cfg.WorkDir, ext)
			if cfg.JSONOutput {
				PrintJSON(items)
			} else {
				if len(items) == 0 {
					fmt.Println("No config set")
					return
				}
				for _, item := range items {
					fmt.Printf("%s = %s\n", item.Key, item.Value)
				}
			}
		},
	}
}

// siteConfigKeys are the customization fields settable under the core config's
// `site` sub-object, published as the static site's site-config.json artifact.
var siteConfigKeys = map[string]bool{"title": true, "accent": true, "accentDark": true, "favicon": true, "url": true, "description": true, "publish": true, "pages": true}

// newSiteConfigCmd creates the `config site` group for the static-site
// customization stored under the `site` sub-object of refs/gitmsg/core/config.
func newSiteConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "site",
		Short: "Manage static-site customization (title, accent, favicon)",
		Long: `Set the static browser site's title, accent color, and favicon. Values are
stored under the "site" sub-object of the core config (refs/gitmsg/core/config)
and published to the bucket as .gitsocial/site/site-config.json on the next
` + "`gitsocial site push`" + ` (or any push to a site-enabled bucket).

Keys:
  title        plain text shown in the tab title and header
  accent       accent color, strict #rgb or #rrggbb hex (e.g. #0a7)
  accentDark   optional accent color for dark mode (same hex form)
  favicon      an image path (@path/to/icon.png) or a data: URI; png/webp/svg,
               32KB max
  url          the site's public base URL, absolute https:// (http:// only for
               localhost), no query/fragment; normalized to a trailing slash
  description  plain text description of the site, 300 chars max
  publish      true/false (default false): master switch for the static site;
               unset or false, pushes move repo data only
  pages        true/false (default false): the crawlable HTML page layer;
               effective only with publish=true and a valid url`,
	}
	cmd.AddCommand(newSiteConfigGetCmd(), newSiteConfigSetCmd(), newSiteConfigListCmd())
	return cmd
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

func newSiteConfigGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <key>",
		Short: "Get a site customization value",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			if !EnsureGitRepo(cmd) {
				os.Exit(ExitNotRepo)
			}
			cfg := GetConfig(cmd)
			key := args[0]
			site := readSiteConfigMap(cfg.WorkDir)
			val, ok := site[key].(string)
			if !ok || val == "" {
				PrintError(cmd, fmt.Sprintf("key not found: %s", key))
				os.Exit(ExitError)
			}
			if cfg.JSONOutput {
				PrintJSON(map[string]string{"key": key, "value": val})
			} else {
				fmt.Println(val)
			}
		},
	}
}

func newSiteConfigListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all site customization values",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			if !EnsureGitRepo(cmd) {
				os.Exit(ExitNotRepo)
			}
			cfg := GetConfig(cmd)
			site := readSiteConfigMap(cfg.WorkDir)
			if cfg.JSONOutput {
				PrintJSON(site)
				return
			}
			if len(site) == 0 {
				fmt.Println("No site customization set")
				return
			}
			for _, k := range []string{"title", "accent", "accentDark", "favicon", "url", "description", "publish", "pages"} {
				if v, ok := site[k].(string); ok && v != "" {
					fmt.Printf("%s = %s\n", k, siteConfigDisplay(k, v))
				}
			}
		},
	}
}

// siteConfigDisplay truncates a long favicon data URI for readable listing.
func siteConfigDisplay(key, value string) string {
	if key == "favicon" && len(value) > 48 {
		return value[:45] + fmt.Sprintf("... (%d bytes)", len(value))
	}
	return value
}

func newSiteConfigSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a site customization value",
		Long: `Set a site customization value. Valid keys: title, accent, accentDark, favicon,
url, description, publish, pages.

  accent / accentDark  strict #rgb or #rrggbb hex (e.g. #0a7 or #00dddd)
  favicon              @path/to/icon.png to read+encode a raw image (png/webp/
                       svg), or a data: URI directly; 32KB max
  url                  absolute https:// URL (http:// only for localhost), no
                       query/fragment; normalized to a trailing slash
  description          plain text, 300 chars max
  publish / pages      true or false (both default false): publish enables the
                       static site; pages enables the crawlable HTML page layer
                       (effective only with publish=true and a valid url)`,
		Args: cobra.ExactArgs(2),
		Run: func(cmd *cobra.Command, args []string) {
			if !EnsureGitRepo(cmd) {
				os.Exit(ExitNotRepo)
			}
			cfg := GetConfig(cmd)
			key, value := args[0], args[1]
			if !siteConfigKeys[key] {
				PrintError(cmd, fmt.Sprintf("unknown key %q (valid: title, accent, accentDark, favicon, url, description, publish, pages)", key))
				os.Exit(ExitError)
			}
			resolved, err := resolveSiteConfigValue(key, value)
			if err != nil {
				PrintError(cmd, err.Error())
				os.Exit(ExitError)
			}
			full, _ := gitmsg.ReadExtConfig(cfg.WorkDir, coreExt)
			if full == nil {
				full = map[string]interface{}{}
			}
			site, _ := full["site"].(map[string]interface{})
			if site == nil {
				site = map[string]interface{}{}
			}
			site[key] = resolved
			full["site"] = site
			if err := gitmsg.WriteExtConfig(cfg.WorkDir, coreExt, full); err != nil {
				PrintError(cmd, err.Error())
				os.Exit(ExitError)
			}
			if cfg.JSONOutput {
				PrintJSON(map[string]string{"key": key, "value": siteConfigDisplay(key, resolved)})
			} else {
				PrintSuccess(cmd, fmt.Sprintf("%s = %s", key, siteConfigDisplay(key, resolved)))
			}
		},
	}
}

// resolveSiteConfigValue validates (and for a favicon, loads/encodes) a raw CLI
// value into what is stored: accent colors must be strict hex; a favicon may be
// an @path to a raw image (base64-encoded into a data URI) or a data URI, of an
// allowed type (png/webp/svg+xml) within the 32KB cap; a url is normalized to a
// trailing slash; a description is trimmed and length-checked.
func resolveSiteConfigValue(key, value string) (string, error) {
	switch key {
	case "accent", "accentDark":
		if !objstore.ValidSiteAccent(value) {
			return "", fmt.Errorf("%s must be a #rgb or #rrggbb hex color, got %q", key, value)
		}
		return value, nil
	case "favicon":
		return resolveFaviconValue(value)
	case "url":
		norm, ok := objstore.NormalizeSiteURL(value)
		if !ok {
			return "", fmt.Errorf("url must be an absolute https:// URL with no query or fragment (http:// only for localhost), got %q", value)
		}
		return norm, nil
	case "description":
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return "", fmt.Errorf("description must not be empty")
		}
		if len(trimmed) > objstore.SiteConfigMaxDescription {
			return "", fmt.Errorf("description too long: %d chars (max %d)", len(trimmed), objstore.SiteConfigMaxDescription)
		}
		return trimmed, nil
	case "publish", "pages":
		v := strings.ToLower(strings.TrimSpace(value))
		if v != "true" && v != "false" {
			return "", fmt.Errorf("%s must be true or false, got %q", key, value)
		}
		return v, nil
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
	if !objstore.ValidSiteFavicon(dataURI) {
		if len(dataURI) > objstore.SiteFaviconMaxBytes {
			return "", fmt.Errorf("favicon is %d bytes, over the %d-byte cap", len(dataURI), objstore.SiteFaviconMaxBytes)
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

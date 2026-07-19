// site_customization_test.go - the site customization artifact: strict
// per-field validation of the core config's `site` sub-object, and the
// push-time writer that publishes (or deletes) .gitsocial/site/site-config.json.

package objstore

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestValidateSiteCustomization(t *testing.T) {
	t.Run("all valid fields survive", func(t *testing.T) {
		c, ok := validateSiteCustomization(map[string]interface{}{
			"title":      "My Project",
			"accent":     "#0a7",
			"accentDark": "#00dddd",
			"favicon":    "data:image/png;base64,AAAA",
		})
		if !ok {
			t.Fatal("valid config rejected")
		}
		if c.Title != "My Project" || c.Accent != "#0a7" || c.AccentDark != "#00dddd" || c.Favicon != "data:image/png;base64,AAAA" {
			t.Fatalf("valid config = %+v", c)
		}
	})

	t.Run("bad accent dropped, good title kept", func(t *testing.T) {
		c, ok := validateSiteCustomization(map[string]interface{}{"title": "Keep", "accent": "red"})
		if !ok || c.Title != "Keep" || c.Accent != "" {
			t.Fatalf("field-by-field drop failed: %+v ok=%v", c, ok)
		}
	})

	t.Run("all invalid yields ok=false (artifact deleted)", func(t *testing.T) {
		if _, ok := validateSiteCustomization(map[string]interface{}{"accent": "notahex", "favicon": "data:text/html,x"}); ok {
			t.Fatal("all-invalid config reported valid")
		}
	})

	t.Run("empty object yields ok=false", func(t *testing.T) {
		if _, ok := validateSiteCustomization(map[string]interface{}{}); ok {
			t.Fatal("empty config reported valid")
		}
	})

	t.Run("oversized favicon rejected", func(t *testing.T) {
		big := "data:image/png;base64," + strings.Repeat("A", SiteFaviconMaxBytes)
		if _, ok := validateSiteCustomization(map[string]interface{}{"favicon": big}); ok {
			t.Fatal("oversized favicon accepted")
		}
	})

	t.Run("disallowed favicon type rejected", func(t *testing.T) {
		if _, ok := validateSiteCustomization(map[string]interface{}{"favicon": "data:image/gif;base64,AAAA"}); ok {
			t.Fatal("gif favicon accepted")
		}
	})

	t.Run("long title truncated", func(t *testing.T) {
		c, ok := validateSiteCustomization(map[string]interface{}{"title": strings.Repeat("x", SiteConfigMaxTitle+50)})
		if !ok || len(c.Title) != SiteConfigMaxTitle {
			t.Fatalf("title truncation failed: len=%d ok=%v", len(c.Title), ok)
		}
	})
}

func TestValidSiteAccentAndFavicon(t *testing.T) {
	for _, v := range []string{"#0a7", "#0A7", "#00dddd", "#FFFFFF"} {
		if !ValidSiteAccent(v) {
			t.Errorf("valid accent %q rejected", v)
		}
	}
	for _, v := range []string{"0a7", "#00d7", "#gggggg", "red", "", "#0a7;x"} {
		if ValidSiteAccent(v) {
			t.Errorf("invalid accent %q accepted", v)
		}
	}
	for _, v := range []string{"data:image/png;base64,AA", "data:image/webp,AA", "data:image/svg+xml,<svg/>"} {
		if !ValidSiteFavicon(v) {
			t.Errorf("valid favicon %q rejected", v)
		}
	}
	for _, v := range []string{"data:image/gif;base64,AA", "http://x/i.png", "data:text/html,x", ""} {
		if ValidSiteFavicon(v) {
			t.Errorf("invalid favicon %q accepted", v)
		}
	}
}

// seedCoreConfigCommit uploads a refs/gitmsg/core/config commit whose message is
// the given JSON and returns its sha (as the refs map would carry it).
func seedCoreConfigCommit(t *testing.T, client *Client, jsonMsg string) string {
	t.Helper()
	sha, loose := makeLooseCommit(t, "", jsonMsg, 1000)
	if err := client.Put("objects/"+sha[:2]+"/"+sha[2:], loose); err != nil {
		t.Fatalf("seed core config commit: %v", err)
	}
	return sha
}

func TestWriteSiteCustomization(t *testing.T) {
	t.Run("valid site config publishes the artifact", func(t *testing.T) {
		client, bucket := testClient(t)
		sha := seedCoreConfigCommit(t, client, `{"version":1,"site":{"title":"Demo","accent":"#0a7","accentDark":"#0dd"}}`)
		refs := map[string]string{"refs/gitmsg/core/config": sha}
		if err := writeSiteCustomization(client, "", refs, SiteOverride{}); err != nil {
			t.Fatalf("writeSiteCustomization: %v", err)
		}
		data, err := client.Get(siteCustomizationKey)
		if err != nil {
			t.Fatalf("read artifact: %v", err)
		}
		var got siteCustomization
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatalf("parse artifact: %v", err)
		}
		if got.Title != "Demo" || got.Accent != "#0a7" || got.AccentDark != "#0dd" {
			t.Fatalf("artifact = %+v", got)
		}
		if bucket.putCount(siteCustomizationKey) != 1 {
			t.Fatalf("expected 1 PUT, got %d", bucket.putCount(siteCustomizationKey))
		}
	})

	t.Run("absent ref deletes/skips the artifact", func(t *testing.T) {
		client, _ := testClient(t)
		if err := writeSiteCustomization(client, "", map[string]string{}, SiteOverride{}); err != nil {
			t.Fatalf("writeSiteCustomization (absent): %v", err)
		}
		if _, err := client.Get(siteCustomizationKey); err == nil {
			t.Fatal("artifact present after absent-ref write")
		}
	})

	t.Run("malformed config JSON deletes the artifact", func(t *testing.T) {
		client, _ := testClient(t)
		if err := client.Put(siteCustomizationKey, []byte(`{"stale":true}`)); err != nil {
			t.Fatalf("seed stale artifact: %v", err)
		}
		sha := seedCoreConfigCommit(t, client, `{not valid json`)
		refs := map[string]string{"refs/gitmsg/core/config": sha}
		if err := writeSiteCustomization(client, "", refs, SiteOverride{}); err != nil {
			t.Fatalf("writeSiteCustomization (malformed): %v", err)
		}
		if _, err := client.Get(siteCustomizationKey); err == nil {
			t.Fatal("stale artifact survived a malformed config")
		}
	})

	t.Run("no site sub-object deletes the artifact", func(t *testing.T) {
		client, _ := testClient(t)
		if err := client.Put(siteCustomizationKey, []byte(`{"stale":true}`)); err != nil {
			t.Fatalf("seed stale artifact: %v", err)
		}
		sha := seedCoreConfigCommit(t, client, `{"version":1,"branch":"gitmsg/core"}`)
		refs := map[string]string{"refs/gitmsg/core/config": sha}
		if err := writeSiteCustomization(client, "", refs, SiteOverride{}); err != nil {
			t.Fatalf("writeSiteCustomization (no site): %v", err)
		}
		if _, err := client.Get(siteCustomizationKey); err == nil {
			t.Fatal("stale artifact survived a config without a site sub-object")
		}
	})
}

// TestSiteCustomizationCacheControl confirms the artifact falls through to the
// no-cache policy (mutable key), so a customization change is picked up next load.
func TestSiteCustomizationCacheControl(t *testing.T) {
	if got := cacheControlForKey(siteCustomizationKey); got != cacheControlRevalidate {
		t.Fatalf("site-config.json cache-control = %q, want %q", got, cacheControlRevalidate)
	}
}

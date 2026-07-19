// site_override_test.go - per-remote site deployment overrides: precedence and
// normalization of applySiteOverride, the readSiteCustomization boundary, and
// the override stamped into the site-config.json artifact written to a bucket.

package objstore

import (
	"encoding/json"
	"encoding/xml"
	"strings"
	"testing"
)

func TestApplySiteOverride(t *testing.T) {
	base := siteCustomization{Title: "Demo", URL: "https://primary.example/", Publish: "true", Pages: "true"}

	t.Run("empty override is a passthrough", func(t *testing.T) {
		got, ok := applySiteOverride(base, true, SiteOverride{})
		if !ok || got != base {
			t.Fatalf("passthrough failed: %+v ok=%v", got, ok)
		}
	})

	t.Run("url override normalizes and wins", func(t *testing.T) {
		got, ok := applySiteOverride(base, true, SiteOverride{URL: "https://r2.example"})
		if !ok || got.URL != "https://r2.example/" {
			t.Fatalf("url override = %q ok=%v, want https://r2.example/", got.URL, ok)
		}
		if got.Title != "Demo" {
			t.Errorf("identity leaked: title = %q, want Demo", got.Title)
		}
	})

	t.Run("publish override off disables", func(t *testing.T) {
		got, ok := applySiteOverride(base, true, SiteOverride{Publish: "false"})
		if !ok || got.Publish != "false" {
			t.Fatalf("publish override = %q ok=%v, want false", got.Publish, ok)
		}
	})

	t.Run("pages override off", func(t *testing.T) {
		got, _ := applySiteOverride(base, true, SiteOverride{Pages: "false"})
		if got.Pages != "false" {
			t.Errorf("pages override = %q, want false", got.Pages)
		}
	})

	t.Run("invalid url override is ignored", func(t *testing.T) {
		got, _ := applySiteOverride(base, true, SiteOverride{URL: "ftp://nope"})
		if got.URL != "https://primary.example/" {
			t.Errorf("invalid override should keep base url, got %q", got.URL)
		}
	})

	t.Run("override makes an empty base publishable", func(t *testing.T) {
		got, ok := applySiteOverride(siteCustomization{}, false, SiteOverride{Publish: "true", URL: "https://only.example"})
		if !ok || got.Publish != "true" || got.URL != "https://only.example/" {
			t.Fatalf("empty-base override = %+v ok=%v", got, ok)
		}
	})
}

func TestReadSiteCustomization_override(t *testing.T) {
	client, _ := testClient(t)
	sha := seedCoreConfigCommit(t, client, `{"version":1,"site":{"title":"Demo","url":"https://primary.example/","publish":"true"}}`)
	refs := map[string]string{"refs/gitmsg/core/config": sha}

	got, ok, err := readSiteCustomization(client, "", refs, SiteOverride{URL: "https://r2.example"})
	if err != nil || !ok {
		t.Fatalf("readSiteCustomization: ok=%v err=%v", ok, err)
	}
	if got.URL != "https://r2.example/" {
		t.Errorf("effective url = %q, want the override https://r2.example/", got.URL)
	}
	if got.Title != "Demo" {
		t.Errorf("title = %q, want Demo (identity is not overridable)", got.Title)
	}
}

func TestWriteSiteCustomization_overrideStamped(t *testing.T) {
	client, bucket := testClient(t)
	sha := seedCoreConfigCommit(t, client, `{"version":1,"site":{"title":"Demo","url":"https://primary.example/","publish":"true"}}`)
	refs := map[string]string{"refs/gitmsg/core/config": sha}

	if err := writeSiteCustomization(client, "", refs, SiteOverride{URL: "https://r2.example"}); err != nil {
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
	if got.URL != "https://r2.example/" {
		t.Fatalf("site-config.json url = %q, want the override so the SPA agrees with the pages", got.URL)
	}
	if bucket.putCount(siteCustomizationKey) != 1 {
		t.Errorf("expected 1 PUT, got %d", bucket.putCount(siteCustomizationKey))
	}
}

// TestSitePush_URLOverrideStamped confirms a full site push with a URL override
// stamps that bucket's own URL into the crawlable page layer's absolute-URL
// artifacts (robots sitemap line, the Atom feed) and site-config.json, so the
// pages and the SPA agree on the bucket's identity.
func TestSitePush_URLOverrideStamped(t *testing.T) {
	client, _ := testClient(t)
	seedPagesConfig(t, client, map[string]any{"publish": "true", "pages": "true", "url": "https://primary.example/", "title": "Override"})
	seedSocialMessages(t, client, "", []pageMsgSpec{{msg: "hello post\n\nbody"}})
	if err := client.Put("HEAD", []byte("ref: refs/heads/main\n")); err != nil {
		t.Fatal(err)
	}
	if err := pushSite(client, "", nil, SiteOverride{URL: "https://r2.example"}, nil); err != nil {
		t.Fatalf("pushSite: %v", err)
	}

	robots := getKey(t, client, sitePagesRobotsKey)
	if !strings.Contains(robots, "Sitemap: https://r2.example/sitemap.xml") {
		t.Errorf("robots.txt used the config url, not the override:\n%s", robots)
	}
	var f atomFeedDoc
	if err := xml.Unmarshal([]byte(getKey(t, client, sitePagesFeedKey)), &f); err != nil {
		t.Fatalf("feed xml: %v", err)
	}
	if f.ID != "https://r2.example/" {
		t.Errorf("feed id = %q, want the override https://r2.example/", f.ID)
	}
	var sc siteCustomization
	if err := json.Unmarshal([]byte(getKey(t, client, siteCustomizationKey)), &sc); err != nil {
		t.Fatalf("site-config: %v", err)
	}
	if sc.URL != "https://r2.example/" {
		t.Errorf("site-config.json url = %q, want the override", sc.URL)
	}
}

// TestSitePush_PublishOverrideDisablesPagesOnly confirms a publish=false override
// (no config-ref change) deletes that bucket's page set on the next push while
// leaving the data artifacts (the refs manifest) intact — the override must
// invalidate the skip marker even though no bucket ref moved.
func TestSitePush_PublishOverrideDisablesPagesOnly(t *testing.T) {
	client, _ := testClient(t)
	seedPagesConfig(t, client, pagesTestSite())
	seedSocialMessages(t, client, "", []pageMsgSpec{{msg: "a post\n\nbody"}})
	if err := client.Put("HEAD", []byte("ref: refs/heads/main\n")); err != nil {
		t.Fatal(err)
	}
	// First push (no override) builds the page set and stamps the skip marker.
	if err := pushSite(client, "", nil, SiteOverride{}, nil); err != nil {
		t.Fatalf("pushSite build: %v", err)
	}
	if !keyExists(client, sitePagesManifestKey) {
		t.Fatal("expected a page set after the first push")
	}
	// A publish=false override, with the config ref and all branch tips unchanged,
	// must still run a full pass (the folded digest differs) and delete the pages.
	if err := pushSite(client, "", nil, SiteOverride{Publish: "false"}, nil); err != nil {
		t.Fatalf("pushSite disable: %v", err)
	}
	for _, key := range []string{sitePagesManifestKey, sitePagesRobotsKey, sitePagesFeedKey} {
		if keyExists(client, key) {
			t.Errorf("publish-override off must delete the page set: %s survived", key)
		}
	}
	if !keyExists(client, siteManifestKey) {
		t.Error("the data refs manifest must survive a page-set-only disable")
	}
}

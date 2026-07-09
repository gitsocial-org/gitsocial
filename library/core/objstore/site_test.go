// site_test.go - the site-enabled probe that gates site-only artifacts, so a
// plain s3:// git remote with no static site never accumulates the refs manifest
// or the per-extension item/body corpora (finding L1).

package objstore

import "testing"

func TestSiteEnabled(t *testing.T) {
	t.Run("fresh non-site bucket: not enabled", func(t *testing.T) {
		client, _ := testClient(t)
		enabled, marker, err := siteEnabled(client, "")
		if err != nil {
			t.Fatalf("siteEnabled: %v", err)
		}
		if enabled || marker != "" {
			t.Fatalf("empty bucket reported enabled=%v marker=%q, want false/\"\"", enabled, marker)
		}
	})

	t.Run("version marker present: enabled with value", func(t *testing.T) {
		client, _ := testClient(t)
		if err := client.Put(siteVersionKey, []byte("v123\n")); err != nil {
			t.Fatalf("seed version marker: %v", err)
		}
		enabled, marker, err := siteEnabled(client, "")
		if err != nil {
			t.Fatalf("siteEnabled: %v", err)
		}
		if !enabled || marker != "v123" {
			t.Fatalf("marker bucket reported enabled=%v marker=%q, want true/v123", enabled, marker)
		}
	})

	t.Run("shell present, no version marker: enabled", func(t *testing.T) {
		client, _ := testClient(t)
		if err := client.Put("index.html", []byte("<html></html>")); err != nil {
			t.Fatalf("seed shell: %v", err)
		}
		enabled, marker, err := siteEnabled(client, "")
		if err != nil {
			t.Fatalf("siteEnabled: %v", err)
		}
		if !enabled || marker != "" {
			t.Fatalf("pre-marker shell bucket reported enabled=%v marker=%q, want true/\"\"", enabled, marker)
		}
	})
}

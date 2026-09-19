// site_workspace_config_test.go - The workspace site config round trip: every json key survives the writer and the reader.
package site

import (
	"encoding/json"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/internal/testutil"
)

// siteConfigJSONKeys lists the json tag names SiteCustomization declares.
func siteConfigJSONKeys() []string {
	t := reflect.TypeOf(SiteCustomization{})
	keys := make([]string, 0, t.NumField())
	for i := range t.NumField() {
		tag, _, _ := strings.Cut(t.Field(i).Tag.Get("json"), ",")
		keys = append(keys, tag)
	}
	sort.Strings(keys)
	return keys
}

// sortedKeys returns a map's keys in order.
func sortedKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// TestWorkspaceSiteCustomizationRoundTrip checks that a full customization survives the writer and the reader with every key intact.
func TestWorkspaceSiteCustomizationRoundTrip(t *testing.T) {
	workdir, err := testutil.NewRepoTemplate()
	if err != nil {
		t.Fatalf("NewRepoTemplate() error = %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(workdir) })

	want := SiteCustomization{
		Title:        "Thread Demo",
		Accent:       "#0a7",
		AccentDark:   "#0dd",
		Favicon:      "data:image/png;base64,iVBORw0KGgo=",
		Image:        "og-card.png",
		URL:          "https://example.com/",
		Description:  "A demo site",
		Publish:      "true",
		Pages:        "true",
		FilesInclude: "*.txt",
		FilesExclude: "secret/*",
	}
	if err := WriteWorkspaceSiteCustomization(workdir, want); err != nil {
		t.Fatalf("WriteWorkspaceSiteCustomization() error = %v", err)
	}
	got, err := ReadWorkspaceSiteCustomization(workdir)
	if err != nil {
		t.Fatalf("ReadWorkspaceSiteCustomization() error = %v", err)
	}
	if got != want {
		t.Errorf("round trip = %+v, want %+v", got, want)
	}

	keys := siteConfigJSONKeys()
	config, err := gitmsg.ReadExtConfig(workdir, "core")
	if err != nil {
		t.Fatalf("ReadExtConfig() error = %v", err)
	}
	stored, _ := config[coreConfigSiteKey].(map[string]interface{})
	if diff := sortedKeys(stored); !reflect.DeepEqual(diff, keys) {
		t.Errorf("stored site sub-object keys = %v, want %v", diff, keys)
	}

	// The published site-config.json artifact carries the same key set.
	data, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	var artifact map[string]interface{}
	if err := json.Unmarshal(data, &artifact); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if diff := sortedKeys(artifact); !reflect.DeepEqual(diff, keys) {
		t.Errorf("%s keys = %v, want %v", siteCustomizationKey, diff, keys)
	}
}

// TestWorkspaceSiteCustomizationDropsEmpty checks that an empty customization removes the sub-object.
func TestWorkspaceSiteCustomizationDropsEmpty(t *testing.T) {
	workdir, err := testutil.NewRepoTemplate()
	if err != nil {
		t.Fatalf("NewRepoTemplate() error = %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(workdir) })

	if err := WriteWorkspaceSiteCustomization(workdir, SiteCustomization{Title: "Gone"}); err != nil {
		t.Fatalf("WriteWorkspaceSiteCustomization() error = %v", err)
	}
	if err := WriteWorkspaceSiteCustomization(workdir, SiteCustomization{}); err != nil {
		t.Fatalf("WriteWorkspaceSiteCustomization(empty) error = %v", err)
	}
	config, err := gitmsg.ReadExtConfig(workdir, "core")
	if err != nil {
		t.Fatalf("ReadExtConfig() error = %v", err)
	}
	if _, present := config[coreConfigSiteKey]; present {
		t.Error("empty customization left the site sub-object in place")
	}
}

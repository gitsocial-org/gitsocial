// sbom_test.go - Tests and a fuzz target for the SPDX, CycloneDX and Syft SBOM parsers
package release

import (
	"strings"
	"testing"
)

const spdxSBOM = `{
  "spdxVersion": "SPDX-2.3",
  "creationInfo": {
    "created": "2026-01-02T03:04:05Z",
    "creators": ["Organization: Example", "Tool: syft-1.2.3"]
  },
  "packages": [
    {"name": "libfoo", "versionInfo": "1.0.0", "licenseConcluded": "MIT"},
    {"name": "libbar", "versionInfo": "2.0.0", "licenseConcluded": "MIT"},
    {"name": "libbaz", "versionInfo": "3.0.0", "licenseConcluded": "NOASSERTION"}
  ]
}`

const cycloneDXSBOM = `{
  "bomFormat": "CycloneDX",
  "specVersion": "1.5",
  "metadata": {
    "timestamp": "2026-01-02T03:04:05Z",
    "tools": {"components": [{"name": "cdxgen", "version": "10.0.0"}]}
  },
  "components": [
    {"name": "libfoo", "version": "1.0.0", "licenses": [{"license": {"id": "Apache-2.0"}}]},
    {"name": "libbar", "version": "2.0.0", "licenses": [{"license": {"name": "Custom Public License"}}]},
    {"name": "libbaz", "version": "3.0.0"}
  ]
}`

const syftSBOM = `{
  "descriptor": {"name": "syft", "version": "1.2.3"},
  "artifacts": [
    {"name": "libfoo", "version": "1.0.0", "licenses": [{"value": "MIT"}]},
    {"name": "libbar", "version": "2.0.0", "licenses": []}
  ]
}`

func TestParseSBOM_validDocuments(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		data          string
		wantFormat    string
		wantPackages  int
		wantGenerator string
		wantGenerated string
		wantLicenses  map[string]int
		wantFirst     SBOMPackage
	}{
		{
			name:          "spdx",
			data:          spdxSBOM,
			wantFormat:    "SPDX-2.3",
			wantPackages:  3,
			wantGenerator: "syft-1.2.3",
			wantGenerated: "2026-01-02T03:04:05Z",
			wantLicenses:  map[string]int{"MIT": 2},
			wantFirst:     SBOMPackage{Name: "libfoo", Version: "1.0.0", License: "MIT"},
		},
		{
			name:          "cyclonedx",
			data:          cycloneDXSBOM,
			wantFormat:    "CycloneDX-1.5",
			wantPackages:  3,
			wantGenerator: "cdxgen 10.0.0",
			wantGenerated: "2026-01-02T03:04:05Z",
			wantLicenses:  map[string]int{"Apache-2.0": 1, "Custom Public License": 1},
			wantFirst:     SBOMPackage{Name: "libfoo", Version: "1.0.0", License: "Apache-2.0"},
		},
		{
			name:          "syft native",
			data:          syftSBOM,
			wantFormat:    "Syft",
			wantPackages:  2,
			wantGenerator: "syft 1.2.3",
			wantLicenses:  map[string]int{"MIT": 1},
			wantFirst:     SBOMPackage{Name: "libfoo", Version: "1.0.0", License: "MIT"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseSBOM([]byte(tt.data))
			if err != nil {
				t.Fatalf("ParseSBOM() error = %v", err)
			}
			if got.Format != tt.wantFormat {
				t.Errorf("Format = %q, want %q", got.Format, tt.wantFormat)
			}
			if got.Packages != tt.wantPackages || len(got.Items) != tt.wantPackages {
				t.Errorf("Packages = %d, items = %d, want %d", got.Packages, len(got.Items), tt.wantPackages)
			}
			if got.Generator != tt.wantGenerator {
				t.Errorf("Generator = %q, want %q", got.Generator, tt.wantGenerator)
			}
			if got.Generated != tt.wantGenerated {
				t.Errorf("Generated = %q, want %q", got.Generated, tt.wantGenerated)
			}
			if len(got.Licenses) != len(tt.wantLicenses) {
				t.Errorf("Licenses = %v, want %v", got.Licenses, tt.wantLicenses)
			}
			for name, count := range tt.wantLicenses {
				if got.Licenses[name] != count {
					t.Errorf("Licenses[%q] = %d, want %d", name, got.Licenses[name], count)
				}
			}
			if got.Items[0] != tt.wantFirst {
				t.Errorf("Items[0] = %+v, want %+v", got.Items[0], tt.wantFirst)
			}
		})
	}
}

func TestParseSBOM_malformed(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		data    string
		wantErr string
	}{
		{name: "not json", data: "this is not json at all", wantErr: "parse sbom json"},
		{name: "truncated cyclonedx", data: `{"bomFormat": "CycloneDX", "components": [`, wantErr: "parse sbom json"},
		{name: "unknown format", data: `{"someOtherFormat": true, "packages": []}`, wantErr: "unrecognized SBOM format"},
		{name: "syft without descriptor", data: `{"artifacts": []}`, wantErr: "unrecognized SBOM format"},
		{name: "spdx packages wrong type", data: `{"spdxVersion": "SPDX-2.3", "packages": "not an array"}`, wantErr: "parse spdx"},
		{name: "cyclonedx components wrong type", data: `{"bomFormat": "CycloneDX", "components": 42}`, wantErr: "parse cyclonedx"},
		{name: "syft artifacts wrong type", data: `{"descriptor": {}, "artifacts": "nope"}`, wantErr: "parse syft native"},
		{name: "empty input", data: "", wantErr: "parse sbom json"},
		{name: "json array at top level", data: `[{"spdxVersion": "SPDX-2.3"}]`, wantErr: "parse sbom json"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseSBOM([]byte(tt.data))
			if err == nil {
				t.Fatalf("ParseSBOM() = %+v, want an error", got)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want it to mention %q", err, tt.wantErr)
			}
		})
	}
}

func TestParseSBOM_emptyDocuments(t *testing.T) {
	t.Parallel()
	tests := []struct{ name, data string }{
		{"spdx", `{"spdxVersion": "SPDX-2.3"}`},
		{"cyclonedx", `{"bomFormat": "CycloneDX", "specVersion": "1.4"}`},
		{"syft", `{"descriptor": {"name": "syft"}, "artifacts": []}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseSBOM([]byte(tt.data))
			if err != nil {
				t.Fatalf("ParseSBOM() error = %v", err)
			}
			if got.Packages != 0 || len(got.Items) != 0 || len(got.Licenses) != 0 {
				t.Errorf("ParseSBOM() = %+v, want an empty summary", got)
			}
		})
	}
}

func TestParseCDXTools(t *testing.T) {
	t.Parallel()
	tests := []struct{ name, raw, want string }{
		{"empty", "", ""},
		{"1.5 object form", `{"components": [{"name": "cdxgen", "version": "10.0.0"}]}`, "cdxgen 10.0.0"},
		{"1.5 object without version", `{"components": [{"name": "cdxgen"}]}`, "cdxgen"},
		{"legacy array form", `[{"name": "syft", "version": "1.0"}]`, "syft 1.0"},
		{"legacy array without version", `[{"name": "syft"}]`, "syft"},
		{"empty object", `{}`, ""},
		{"empty array", `[]`, ""},
		{"garbage", `"a string"`, ""},
	}
	for _, tt := range tests {
		if got := parseCDXTools([]byte(tt.raw)); got != tt.want {
			t.Errorf("%s: parseCDXTools(%s) = %q, want %q", tt.name, tt.raw, got, tt.want)
		}
	}
}

func TestSortedLicenses(t *testing.T) {
	t.Parallel()
	entries := SortedLicenses(map[string]int{"MIT": 5, "Apache-2.0": 9, "BSD-3": 1})
	if len(entries) != 3 {
		t.Fatalf("SortedLicenses() = %d entries, want 3", len(entries))
	}
	if entries[0].Name != "Apache-2.0" || entries[0].Count != 9 {
		t.Errorf("entries[0] = %+v, want Apache-2.0 at 9", entries[0])
	}
	if entries[2].Name != "BSD-3" {
		t.Errorf("entries[2] = %+v, want the least common license last", entries[2])
	}
	if len(SortedLicenses(nil)) != 0 {
		t.Error("SortedLicenses(nil) should be empty")
	}
}

// FuzzParseSBOM checks that no third-party document shape makes the parser panic
// and that a successful parse always describes its own item list consistently.
func FuzzParseSBOM(f *testing.F) {
	for _, seed := range []string{spdxSBOM, cycloneDXSBOM, syftSBOM, "{}", "", "null", `{"spdxVersion":1}`} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		got, err := ParseSBOM(data)
		if err != nil {
			return
		}
		if got == nil {
			t.Fatal("ParseSBOM() returned no summary and no error")
		}
		if got.Packages != len(got.Items) {
			t.Errorf("Packages = %d but %d items were returned", got.Packages, len(got.Items))
		}
		for name, count := range got.Licenses {
			if name == "" {
				t.Error("a blank license name was counted")
			}
			if count <= 0 {
				t.Errorf("license %q counted %d times", name, count)
			}
		}
	})
}

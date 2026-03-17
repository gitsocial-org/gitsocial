// sbom.go - SBOM parsing, caching, and public API
package release

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gitsocial-org/gitsocial/core/cache"
	"github.com/gitsocial-org/gitsocial/core/git"
	"github.com/gitsocial-org/gitsocial/core/log"
	"github.com/gitsocial-org/gitsocial/core/result"
)

// SBOMSummary holds parsed SBOM metadata.
type SBOMSummary struct {
	Format    string         `json:"format"`
	Packages  int            `json:"packages"`
	Generator string         `json:"generator"`
	Licenses  map[string]int `json:"licenses"`
	Generated string         `json:"generated"`
	Items     []SBOMPackage  `json:"items"`
}

// SBOMPackage represents a single package entry in an SBOM.
type SBOMPackage struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	License string `json:"license"`
}

// ParseSBOM detects the SBOM format and parses it into a summary.
func ParseSBOM(data []byte) (*SBOMSummary, error) {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(data, &probe); err != nil {
		return nil, fmt.Errorf("parse sbom json: %w", err)
	}
	if _, ok := probe["spdxVersion"]; ok {
		return parseSPDX(data)
	}
	if _, ok := probe["bomFormat"]; ok {
		return parseCycloneDX(data)
	}
	if _, ok := probe["artifacts"]; ok {
		if _, ok2 := probe["descriptor"]; ok2 {
			return parseSyftNative(data)
		}
	}
	return nil, fmt.Errorf("unrecognized SBOM format")
}

// parseSPDX parses an SPDX JSON document.
func parseSPDX(data []byte) (*SBOMSummary, error) {
	var doc struct {
		SPDXVersion  string `json:"spdxVersion"`
		CreationInfo struct {
			Created  string   `json:"created"`
			Creators []string `json:"creators"`
		} `json:"creationInfo"`
		Packages []struct {
			Name             string `json:"name"`
			VersionInfo      string `json:"versionInfo"`
			LicenseConcluded string `json:"licenseConcluded"`
		} `json:"packages"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse spdx: %w", err)
	}
	var generator string
	for _, c := range doc.CreationInfo.Creators {
		if strings.HasPrefix(c, "Tool:") {
			generator = strings.TrimSpace(strings.TrimPrefix(c, "Tool:"))
			break
		}
	}
	licenses := make(map[string]int)
	items := make([]SBOMPackage, 0, len(doc.Packages))
	for _, p := range doc.Packages {
		lic := p.LicenseConcluded
		if lic != "" && lic != "NOASSERTION" {
			licenses[lic]++
		}
		items = append(items, SBOMPackage{
			Name:    p.Name,
			Version: p.VersionInfo,
			License: lic,
		})
	}
	return &SBOMSummary{
		Format:    doc.SPDXVersion,
		Packages:  len(doc.Packages),
		Generator: generator,
		Licenses:  licenses,
		Generated: doc.CreationInfo.Created,
		Items:     items,
	}, nil
}

// parseCycloneDX parses a CycloneDX JSON document.
func parseCycloneDX(data []byte) (*SBOMSummary, error) {
	var doc struct {
		BomFormat   string `json:"bomFormat"`
		SpecVersion string `json:"specVersion"`
		Metadata    struct {
			Timestamp string          `json:"timestamp"`
			Tools     json.RawMessage `json:"tools"`
		} `json:"metadata"`
		Components []struct {
			Name     string `json:"name"`
			Version  string `json:"version"`
			Licenses []struct {
				License struct {
					ID   string `json:"id"`
					Name string `json:"name"`
				} `json:"license"`
			} `json:"licenses"`
		} `json:"components"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse cyclonedx: %w", err)
	}
	generator := parseCDXTools(doc.Metadata.Tools)
	licenses := make(map[string]int)
	items := make([]SBOMPackage, 0, len(doc.Components))
	for _, c := range doc.Components {
		var lic string
		if len(c.Licenses) > 0 {
			lic = c.Licenses[0].License.ID
			if lic == "" {
				lic = c.Licenses[0].License.Name
			}
		}
		if lic != "" {
			licenses[lic]++
		}
		items = append(items, SBOMPackage{
			Name:    c.Name,
			Version: c.Version,
			License: lic,
		})
	}
	return &SBOMSummary{
		Format:    "CycloneDX-" + doc.SpecVersion,
		Packages:  len(doc.Components),
		Generator: generator,
		Licenses:  licenses,
		Generated: doc.Metadata.Timestamp,
		Items:     items,
	}, nil
}

// parseCDXTools extracts a tool string from CycloneDX tools field (array or object form).
func parseCDXTools(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	// CycloneDX 1.5+: tools is an object with "components" or "services"
	var toolsObj struct {
		Components []struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"components"`
	}
	if err := json.Unmarshal(raw, &toolsObj); err == nil && len(toolsObj.Components) > 0 {
		t := toolsObj.Components[0]
		if t.Version != "" {
			return t.Name + " " + t.Version
		}
		return t.Name
	}
	// CycloneDX <1.5: tools is an array of {name, version}
	var toolsArr []struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal(raw, &toolsArr); err == nil && len(toolsArr) > 0 {
		t := toolsArr[0]
		if t.Version != "" {
			return t.Name + " " + t.Version
		}
		return t.Name
	}
	return ""
}

// parseSyftNative parses Syft's native JSON format.
func parseSyftNative(data []byte) (*SBOMSummary, error) {
	var doc struct {
		Descriptor struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"descriptor"`
		Artifacts []struct {
			Name     string `json:"name"`
			Version  string `json:"version"`
			Licenses []struct {
				Value string `json:"value"`
			} `json:"licenses"`
		} `json:"artifacts"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse syft native: %w", err)
	}
	generator := doc.Descriptor.Name
	if doc.Descriptor.Version != "" {
		generator += " " + doc.Descriptor.Version
	}
	licenses := make(map[string]int)
	items := make([]SBOMPackage, 0, len(doc.Artifacts))
	for _, a := range doc.Artifacts {
		var lic string
		if len(a.Licenses) > 0 {
			lic = a.Licenses[0].Value
		}
		if lic != "" {
			licenses[lic]++
		}
		items = append(items, SBOMPackage{
			Name:    a.Name,
			Version: a.Version,
			License: lic,
		})
	}
	return &SBOMSummary{
		Format:    "Syft",
		Packages:  len(doc.Artifacts),
		Generator: generator,
		Licenses:  licenses,
		Items:     items,
	}, nil
}

// SortedLicenses returns license entries sorted by count descending.
func SortedLicenses(licenses map[string]int) []LicenseEntry {
	entries := make([]LicenseEntry, 0, len(licenses))
	for name, count := range licenses {
		entries = append(entries, LicenseEntry{Name: name, Count: count})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Count > entries[j].Count
	})
	return entries
}

// LicenseEntry is a license name and count pair.
type LicenseEntry struct {
	Name  string
	Count int
}

// cacheSBOMSummary stores a parsed SBOM summary in the cache.
func cacheSBOMSummary(repoURL, version string, s *SBOMSummary) {
	licensesJSON, err := json.Marshal(s.Licenses)
	if err != nil {
		log.Warn("marshal sbom licenses failed", "error", err)
	}
	itemsJSON, err := json.Marshal(s.Items)
	if err != nil {
		log.Warn("marshal sbom items failed", "error", err)
	}
	if err := cache.ExecLocked(func(db *sql.DB) error {
		_, err := db.Exec(`
			INSERT OR REPLACE INTO release_sbom_cache
			(repo_url, version, format, packages, generator, licenses_json, generated, items_json, cached_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			repoURL, version, s.Format, s.Packages, s.Generator,
			string(licensesJSON), s.Generated, string(itemsJSON),
			time.Now().UTC().Format(time.RFC3339),
		)
		return err
	}); err != nil {
		log.Warn("cache sbom summary failed", "repo", repoURL, "version", version, "error", err)
	}
}

// getCachedSBOMSummary retrieves a cached SBOM summary.
func getCachedSBOMSummary(repoURL, version string) (*SBOMSummary, error) {
	return cache.QueryLocked(func(db *sql.DB) (*SBOMSummary, error) {
		var s SBOMSummary
		var licensesJSON, itemsJSON string
		err := db.QueryRow(`
			SELECT format, packages, generator, licenses_json, generated, items_json
			FROM release_sbom_cache
			WHERE repo_url = ? AND version = ?`,
			repoURL, version,
		).Scan(&s.Format, &s.Packages, &s.Generator, &licensesJSON, &s.Generated, &itemsJSON)
		if err != nil {
			return nil, err
		}
		s.Licenses = make(map[string]int)
		if err := json.Unmarshal([]byte(licensesJSON), &s.Licenses); err != nil {
			log.Debug("unmarshal cached sbom licenses failed", "error", err)
		}
		if err := json.Unmarshal([]byte(itemsJSON), &s.Items); err != nil {
			log.Debug("unmarshal cached sbom items failed", "error", err)
		}
		return &s, nil
	})
}

// GetSBOMSummary returns a parsed SBOM summary, using cache when available.
// Falls back to HTTP fetch from artifactURL if the artifact ref doesn't exist.
func GetSBOMSummary(workdir, repoURL, version, sbomFilename, artifactURL string) (*SBOMSummary, error) {
	if cached, err := getCachedSBOMSummary(repoURL, version); err == nil {
		return cached, nil
	}
	var data []byte
	ref := "refs/gitmsg/release/" + version + "/artifacts"
	if content, err := git.GetFileContent(workdir, ref, sbomFilename); err == nil {
		data = []byte(content)
		if oid, _, ok := git.ParseLFSPointer(data); ok {
			if lfsData, lfsErr := git.ReadLFSObject(workdir, oid); lfsErr == nil {
				data = lfsData
			}
		}
	} else if artifactURL != "" {
		fetched, fetchErr := fetchSBOMHTTP(artifactURL, sbomFilename)
		if fetchErr != nil {
			return nil, fmt.Errorf("read sbom: artifact ref failed, http fetch failed: %w", fetchErr)
		}
		data = fetched
	} else {
		return nil, fmt.Errorf("read sbom artifact: %w", err)
	}
	summary, err := ParseSBOM(data)
	if err != nil {
		return nil, fmt.Errorf("parse sbom: %w", err)
	}
	cacheSBOMSummary(repoURL, version, summary)
	return summary, nil
}

// fetchSBOMHTTP downloads an SBOM file from artifactURL/filename.
func fetchSBOMHTTP(artifactURL, filename string) ([]byte, error) {
	url := strings.TrimRight(artifactURL, "/") + "/" + filename
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch %s: status %d", url, resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", url, err)
	}
	return data, nil
}

// GetSBOMDetails resolves a release and returns its SBOM summary.
func GetSBOMDetails(workdir, releaseRef string) Result[SBOMSummary] {
	res := GetSingleRelease(releaseRef)
	if !res.Success {
		return result.Err[SBOMSummary](res.Error.Code, res.Error.Message)
	}
	rel := res.Data
	if rel.SBOM == "" {
		return result.Err[SBOMSummary]("NO_SBOM", "release has no SBOM")
	}
	if rel.Version == "" {
		return result.Err[SBOMSummary]("NO_VERSION", "release has no version (required for artifact ref)")
	}
	repoURL := rel.Repository
	if repoURL == "" {
		repoURL = "."
	}
	summary, err := GetSBOMSummary(workdir, repoURL, rel.Version, rel.SBOM, rel.ArtifactURL)
	if err != nil {
		return result.Err[SBOMSummary]("SBOM_FAILED", err.Error())
	}
	return result.Ok(*summary)
}

// GetSBOMRaw returns the raw SBOM file content from the artifact ref.
func GetSBOMRaw(workdir, version, sbomFilename string) Result[string] {
	ref := "refs/gitmsg/release/" + version + "/artifacts"
	content, err := git.GetFileContent(workdir, ref, sbomFilename)
	if err != nil {
		return result.Err[string]("READ_FAILED", err.Error())
	}
	if oid, _, ok := git.ParseLFSPointer([]byte(content)); ok {
		if lfsData, lfsErr := git.ReadLFSObject(workdir, oid); lfsErr == nil {
			return result.Ok(string(lfsData))
		}
	}
	return result.Ok(content)
}

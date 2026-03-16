// releases.go - Fetch GitLab releases via REST API
package gitlab

import (
	"fmt"
	"strings"
	"time"

	importpkg "github.com/gitsocial-org/gitsocial/import"
)

type glRelease struct {
	TagName     string   `json:"tag_name"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	ReleasedAt  string   `json:"released_at"`
	Author      glUser   `json:"author"`
	Assets      glAssets `json:"assets"`
}

type glAssets struct {
	Links []glAssetLink `json:"links"`
}

type glAssetLink struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// FetchReleases fetches releases from GitLab.
func (a *Adapter) FetchReleases(opts importpkg.FetchOptions) (*importpkg.ReleasePlan, error) {
	unlimited := opts.Limit == 0
	limit := opts.Limit
	if limit <= 0 {
		limit = 999999
	}
	perPage := 100
	path := fmt.Sprintf("projects/%s/releases?per_page=%d&order_by=released_at&sort=desc",
		a.projectPath(), perPage)
	var raw []glRelease
	nextPage, err := a.apiGetPage(path, &raw)
	if err != nil {
		return nil, fmt.Errorf("fetch releases: %w", err)
	}
	for nextPage != "" && (unlimited || len(raw) < limit) {
		var page []glRelease
		pagePath := path + "&page=" + nextPage
		nextPage, err = a.apiGetPage(pagePath, &page)
		if err != nil {
			break
		}
		raw = append(raw, page...)
		if opts.OnFetchProgress != nil {
			opts.OnFetchProgress(len(raw))
		}
	}
	if !unlimited && len(raw) > limit {
		raw = raw[:limit]
	}
	releases := make([]importpkg.ImportRelease, 0, len(raw))
	var filtered int
	for _, r := range raw {
		if opts.SkipExternalIDs["release:"+r.TagName] {
			continue
		}
		releasedAt, _ := time.Parse(time.RFC3339, r.ReleasedAt)
		if opts.Since != nil && releasedAt.Before(*opts.Since) {
			filtered++
			continue
		}
		var artifacts []string
		var sbom, checksums string
		for _, link := range r.Assets.Links {
			switch {
			case isSBOMAsset(link.Name):
				sbom = link.Name
			case isChecksumAsset(link.Name):
				checksums = link.Name
			}
			artifacts = append(artifacts, link.Name)
		}
		name := r.Name
		if name == "" {
			name = r.TagName
		}
		author := a.resolveUser(r.Author.Username)
		releases = append(releases, importpkg.ImportRelease{
			ExternalID:  r.TagName,
			Name:        name,
			Body:        r.Description,
			Tag:         r.TagName,
			Version:     versionFromTag(r.TagName),
			Artifacts:   artifacts,
			ArtifactURL: buildArtifactURL(a.baseURL, a.owner, a.repo, r.TagName),
			Checksums:   checksums,
			SBOM:        sbom,
			AuthorName:  author.name,
			AuthorEmail: author.email,
			CreatedAt:   releasedAt,
		})
	}
	return &importpkg.ReleasePlan{Releases: releases, Filtered: filtered}, nil
}

func buildArtifactURL(baseURL, owner, repo, tag string) string {
	return fmt.Sprintf("%s/%s/%s/-/releases/%s",
		baseURL, owner, repo, strings.ReplaceAll(tag, " ", "%20"))
}

// isSBOMAsset returns true if the filename looks like an SBOM file.
func isSBOMAsset(name string) bool {
	lower := strings.ToLower(name)
	return strings.HasSuffix(lower, ".spdx.json") ||
		strings.HasSuffix(lower, ".spdx") ||
		strings.HasSuffix(lower, ".cdx.json") ||
		strings.HasSuffix(lower, ".cdx.xml") ||
		strings.Contains(lower, "sbom")
}

// isChecksumAsset returns true if the filename looks like a checksums file.
func isChecksumAsset(name string) bool {
	lower := strings.ToLower(name)
	return lower == "sha256sums" || lower == "sha256sums.txt" ||
		lower == "sha512sums" || lower == "sha512sums.txt" ||
		lower == "checksums.txt" || strings.HasSuffix(lower, ".sha256")
}

// versionFromTag strips a leading "v" prefix to derive a semver version.
func versionFromTag(tag string) string {
	if strings.HasPrefix(tag, "v") {
		return tag[1:]
	}
	return ""
}

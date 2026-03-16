// releases.go - Fetch GitHub releases via gh API
package github

import (
	"fmt"
	"strings"
	"time"

	importpkg "github.com/gitsocial-org/gitsocial/import"
)

type ghRelease struct {
	TagName    string    `json:"tag_name"`
	Name       string    `json:"name"`
	Body       string    `json:"body"`
	Prerelease bool      `json:"prerelease"`
	Draft      bool      `json:"draft"`
	CreatedAt  time.Time `json:"created_at"`
	Author     struct {
		Login string `json:"login"`
	} `json:"author"`
	Assets []ghAsset `json:"assets"`
}

type ghAsset struct {
	Name string `json:"name"`
}

// FetchReleases fetches releases from GitHub using the REST API.
func (a *Adapter) FetchReleases(opts importpkg.FetchOptions) (*importpkg.ReleasePlan, error) {
	unlimited := opts.Limit == 0
	limit := opts.Limit
	if limit <= 0 {
		limit = 999999
	}
	perPage := 100
	var raw []ghRelease
	endpoint := fmt.Sprintf("repos/%s/releases?per_page=%d", a.repoSlug(), perPage)
	if err := ghJSON(&raw, "api", "--paginate", endpoint); err != nil {
		return nil, fmt.Errorf("fetch releases: %w", err)
	}
	if !unlimited && len(raw) > limit {
		raw = raw[:limit]
	}
	if opts.OnFetchProgress != nil {
		opts.OnFetchProgress(len(raw))
	}
	releases := make([]importpkg.ImportRelease, 0, len(raw))
	var filtered int
	for _, r := range raw {
		if r.Draft {
			continue
		}
		if opts.SkipExternalIDs["release:"+r.TagName] {
			continue
		}
		if opts.Since != nil && r.CreatedAt.Before(*opts.Since) {
			filtered++
			continue
		}
		var artifacts []string
		var sbom, checksums string
		for _, asset := range r.Assets {
			switch {
			case isSBOMAsset(asset.Name):
				sbom = asset.Name
			case isChecksumAsset(asset.Name):
				checksums = asset.Name
			}
			artifacts = append(artifacts, asset.Name)
		}
		name := r.Name
		if name == "" {
			name = r.TagName
		}
		artifactURL := buildArtifactURL(a.owner, a.repo, r.TagName)
		author := a.resolveUser(r.Author.Login)
		releases = append(releases, importpkg.ImportRelease{
			ExternalID:  r.TagName,
			Name:        name,
			Body:        r.Body,
			Tag:         r.TagName,
			Version:     versionFromTag(r.TagName),
			Prerelease:  r.Prerelease,
			Artifacts:   artifacts,
			ArtifactURL: artifactURL,
			Checksums:   checksums,
			SBOM:        sbom,
			AuthorName:  author.name,
			AuthorEmail: author.email,
			CreatedAt:   r.CreatedAt,
		})
	}
	return &importpkg.ReleasePlan{Releases: releases, Filtered: filtered}, nil
}

func buildArtifactURL(owner, repo, tag string) string {
	return fmt.Sprintf("https://github.com/%s/%s/releases/download/%s",
		owner, repo, strings.ReplaceAll(tag, " ", "%20"))
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

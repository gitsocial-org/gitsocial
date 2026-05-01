// release.go - Release public API
package release

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/gitsocial-org/gitsocial/core/cache"
	"github.com/gitsocial-org/gitsocial/core/git"
	"github.com/gitsocial-org/gitsocial/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/core/protocol"
	"github.com/gitsocial-org/gitsocial/core/result"
)

// releaseFieldOrder declares the spec-defined field ordering for release headers (GITRELEASE.md 1.2).
var releaseFieldOrder = []string{"artifact-url", "artifacts", "checksums", "prerelease", "sbom", "signed-by", "tag", "version"}

type CreateReleaseOptions struct {
	Tag         string
	Version     string
	Prerelease  bool
	Artifacts   []string
	ArtifactURL string
	Checksums   string
	SignedBy    string
	SBOM        string
	// AllowDuplicate skips the tag-uniqueness check. Set when the
	// caller has accepted that an existing release with the same tag
	// is OK (e.g., re-creating after a retraction).
	AllowDuplicate bool
	Origin         *protocol.Origin
}

type EditReleaseOptions struct {
	Subject     *string
	Body        *string
	Tag         *string
	Version     *string
	Prerelease  *bool
	Artifacts   *[]string
	ArtifactURL *string
	Checksums   *string
	SignedBy    *string
	SBOM        *string
}

// CreateRelease creates a new release on the release branch. Refuses
// with `DUPLICATE` when a non-retracted release with the same tag
// exists, unless opts.AllowDuplicate is set. Tag uniqueness matters for
// CI/CD lookups by tag.
func CreateRelease(workdir, subject, body string, opts CreateReleaseOptions) Result[Release] {
	if !opts.AllowDuplicate && opts.Tag != "" {
		if existing, err := GetReleaseItemByTagOrVersion(opts.Tag); err == nil && existing != nil {
			return result.Err[Release]("DUPLICATE",
				fmt.Sprintf("release with tag %q already exists (pass --allow-duplicate to override)", opts.Tag))
		}
	}
	branch := gitmsg.GetExtBranch(workdir, "release")
	content := buildReleaseContent(subject, body, opts, "")
	hash, err := git.CreateCommitOnBranch(workdir, branch, content)
	if err != nil {
		return result.Err[Release]("COMMIT_FAILED", err.Error())
	}

	repoURL := gitmsg.ResolveRepoURL(workdir)
	if err := cacheReleaseFromCommit(workdir, repoURL, hash, branch); err != nil {
		return result.Err[Release]("CACHE_FAILED", err.Error())
	}

	item, err := GetReleaseItem(repoURL, hash, branch)
	if err != nil {
		return result.Err[Release]("GET_FAILED", err.Error())
	}
	return result.Ok(ReleaseItemToRelease(*item))
}

// EditRelease edits an existing release using core versioning.
func EditRelease(workdir, releaseRef string, opts EditReleaseOptions) Result[Release] {
	repoURL := gitmsg.ResolveRepoURL(workdir)
	existing, err := GetReleaseItemByRef(releaseRef, repoURL)
	if err != nil {
		return result.Err[Release]("NOT_FOUND", "release not found")
	}

	branch := gitmsg.GetExtBranch(workdir, "release")

	rel := ReleaseItemToRelease(*existing)
	createOpts := CreateReleaseOptions{
		Tag:         rel.Tag,
		Version:     rel.Version,
		Prerelease:  rel.Prerelease,
		Artifacts:   rel.Artifacts,
		ArtifactURL: rel.ArtifactURL,
		Checksums:   rel.Checksums,
		SignedBy:    rel.SignedBy,
		SBOM:        rel.SBOM,
	}
	createOpts.Origin = existing.Origin

	subject := rel.Subject
	body := rel.Body

	if opts.Subject != nil {
		subject = *opts.Subject
	}
	if opts.Body != nil {
		body = *opts.Body
	}
	if opts.Tag != nil {
		createOpts.Tag = *opts.Tag
	}
	if opts.Version != nil {
		createOpts.Version = *opts.Version
	}
	if opts.Prerelease != nil {
		createOpts.Prerelease = *opts.Prerelease
	}
	if opts.Artifacts != nil {
		createOpts.Artifacts = *opts.Artifacts
	}
	if opts.ArtifactURL != nil {
		createOpts.ArtifactURL = *opts.ArtifactURL
	}
	if opts.Checksums != nil {
		createOpts.Checksums = *opts.Checksums
	}
	if opts.SignedBy != nil {
		createOpts.SignedBy = *opts.SignedBy
	}
	if opts.SBOM != nil {
		createOpts.SBOM = *opts.SBOM
	}

	canonicalRef := protocol.LocalizeRef(
		protocol.CreateRef(protocol.RefTypeCommit, existing.Hash, existing.RepoURL, existing.Branch),
		repoURL,
	)
	content := buildReleaseContent(subject, body, createOpts, canonicalRef)

	hash, err := git.CreateCommitOnBranch(workdir, branch, content)
	if err != nil {
		return result.Err[Release]("COMMIT_FAILED", err.Error())
	}

	if err := cacheReleaseFromCommit(workdir, repoURL, hash, branch); err != nil {
		return result.Err[Release]("CACHE_FAILED", err.Error())
	}

	item, err := GetReleaseItem(existing.RepoURL, existing.Hash, existing.Branch)
	if err != nil {
		return result.Err[Release]("GET_FAILED", err.Error())
	}
	return result.Ok(ReleaseItemToRelease(*item))
}

// RetractRelease marks a release as retracted.
func RetractRelease(workdir, releaseRef string) Result[bool] {
	repoURL := gitmsg.ResolveRepoURL(workdir)
	existing, err := GetReleaseItemByRef(releaseRef, repoURL)
	if err != nil {
		return result.Err[bool]("NOT_FOUND", "release not found")
	}

	branch := gitmsg.GetExtBranch(workdir, "release")

	canonicalRef := protocol.LocalizeRef(
		protocol.CreateRef(protocol.RefTypeCommit, existing.Hash, existing.RepoURL, existing.Branch),
		repoURL,
	)

	header := protocol.Header{
		Ext: "release",
		V:   "0.1.0",
		Fields: map[string]string{
			"edits":     canonicalRef,
			"retracted": "true",
		},
	}
	content := protocol.FormatMessage("", header, nil)

	_, err = git.CreateCommitOnBranch(workdir, branch, content)
	if err != nil {
		return result.Err[bool]("COMMIT_FAILED", err.Error())
	}
	return result.Ok(true)
}

// GetSingleRelease retrieves a single release by reference (full ref, hash prefix, tag, or version).
func GetSingleRelease(releaseRef string) Result[Release] {
	parsed := protocol.ParseRef(releaseRef)
	// Try hash prefix match (fast path for raw hashes)
	hash := parsed.Value
	if hash == "" {
		hash = releaseRef
	}
	if item, err := GetReleaseItemByHashPrefix(hash); err == nil {
		return result.Ok(ReleaseItemToRelease(*item))
	}
	// Try full ref match (handles full refs like repo#commit:hash@branch and prefixes thereof)
	if strings.Contains(releaseRef, "#") || strings.Contains(releaseRef, "://") {
		if item, err := GetReleaseItemByFullRef(releaseRef); err == nil {
			return result.Ok(ReleaseItemToRelease(*item))
		}
	}
	// Fall back to tag or version match
	if item, err := GetReleaseItemByTagOrVersion(releaseRef); err == nil {
		return result.Ok(ReleaseItemToRelease(*item))
	}
	return result.Err[Release]("NOT_FOUND", "release not found: "+releaseRef)
}

func buildReleaseContent(subject, body string, opts CreateReleaseOptions, editsRef string) string {
	content := subject
	if body != "" {
		content += "\n\n" + body
	}

	fields := map[string]string{
		"type": "release",
	}
	if editsRef != "" {
		fields["edits"] = editsRef
	}
	if opts.ArtifactURL != "" {
		fields["artifact-url"] = opts.ArtifactURL
	}
	if len(opts.Artifacts) > 0 {
		fields["artifacts"] = strings.Join(opts.Artifacts, ",")
	}
	if opts.Checksums != "" {
		fields["checksums"] = opts.Checksums
	}
	if opts.Prerelease {
		fields["prerelease"] = "true"
	}
	if opts.SBOM != "" {
		fields["sbom"] = opts.SBOM
	}
	if opts.SignedBy != "" {
		fields["signed-by"] = opts.SignedBy
	}
	if opts.Tag != "" {
		fields["tag"] = opts.Tag
	}
	if opts.Version != "" {
		fields["version"] = opts.Version
	}
	protocol.ApplyOrigin(fields, opts.Origin)

	header := protocol.Header{
		Ext:        "release",
		V:          "0.1.0",
		Fields:     fields,
		FieldOrder: releaseFieldOrder,
	}
	return protocol.FormatMessage(content, header, nil)
}

func cacheReleaseFromCommit(workdir, repoURL, hash, branch string) error {
	commit, err := git.GetCommit(workdir, hash)
	if err != nil {
		return err
	}

	if err := cache.InsertCommits([]cache.Commit{{
		Hash:        hash,
		RepoURL:     repoURL,
		Branch:      branch,
		AuthorName:  commit.Author,
		AuthorEmail: commit.Email,
		Message:     commit.Message,
		Timestamp:   commit.Timestamp,
	}}); err != nil {
		return fmt.Errorf("insert commit: %w", err)
	}

	msg := protocol.ParseMessage(commit.Message)
	if msg == nil || msg.Header.Ext != "release" {
		return nil
	}

	editsRef := msg.Header.Fields["edits"]
	isRetracted := msg.Header.Fields["retracted"] == "true"
	if editsRef != "" {
		parsed := protocol.ParseRef(editsRef)
		if parsed.Value != "" {
			canonicalRepoURL := repoURL
			canonicalHash := parsed.Value
			canonicalBranch := branch
			if parsed.Repository != "" {
				canonicalRepoURL = parsed.Repository
			}
			if parsed.Branch != "" {
				canonicalBranch = parsed.Branch
			}
			_ = cache.InsertVersion(repoURL, hash, branch, canonicalRepoURL, canonicalHash, canonicalBranch, isRetracted)
		}
	}

	prerelease := msg.Header.Fields["prerelease"] == "true"

	item := ReleaseItem{
		RepoURL:     repoURL,
		Hash:        hash,
		Branch:      branch,
		Tag:         cache.ToNullString(msg.Header.Fields["tag"]),
		Version:     cache.ToNullString(msg.Header.Fields["version"]),
		Prerelease:  prerelease,
		Artifacts:   cache.ToNullString(msg.Header.Fields["artifacts"]),
		ArtifactURL: cache.ToNullString(msg.Header.Fields["artifact-url"]),
		Checksums:   cache.ToNullString(msg.Header.Fields["checksums"]),
		SignedBy:    cache.ToNullString(msg.Header.Fields["signed-by"]),
		SBOM:        cache.ToNullString(msg.Header.Fields["sbom"]),
	}

	return InsertReleaseItem(item)
}

// ReleaseConfig holds release extension configuration.
type ReleaseConfig struct {
	Version           string `json:"version"`
	Branch            string `json:"branch,omitempty"`
	RequireSignature  bool   `json:"require-signature,omitempty"`
	ChecksumAlgorithm string `json:"checksum-algorithm,omitempty"`
}

// SaveReleaseConfig saves the release extension configuration.
func SaveReleaseConfig(workdir string, config ReleaseConfig) error {
	if config.Version == "" {
		config.Version = "0.1.0"
	}
	data, err := json.Marshal(config)
	if err != nil {
		return err
	}
	ref := "refs/gitmsg/release/config"
	var parent string
	if existing, err := git.ReadRef(workdir, ref); err == nil {
		parent = existing
	}
	hash, err := git.CreateCommitTree(workdir, string(data), parent)
	if err != nil {
		return err
	}
	return git.WriteRef(workdir, ref, hash)
}

// GetReleaseConfig reads the release extension configuration.
func GetReleaseConfig(workdir string) ReleaseConfig {
	configMap, err := gitmsg.ReadExtConfig(workdir, "release")
	if err != nil || configMap == nil {
		return ReleaseConfig{Branch: "gitmsg/release"}
	}
	var config ReleaseConfig
	if v, ok := configMap["version"].(string); ok {
		config.Version = v
	}
	if v, ok := configMap["branch"].(string); ok {
		config.Branch = v
	}
	if v, ok := configMap["require-signature"].(bool); ok {
		config.RequireSignature = v
	}
	if v, ok := configMap["checksum-algorithm"].(string); ok {
		config.ChecksumAlgorithm = v
	}
	if config.Branch == "" {
		config.Branch = "gitmsg/release"
	}
	return config
}

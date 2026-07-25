// artifacts_push.go - Push release artifacts to an s3 remote's bucket
// (GITRELEASE.md §3.2 external storage): plain objects at
// artifacts/<version>/<basename>, an artifacts/latest.txt marker for the
// newest non-prerelease version, and an artifact-url catch-up on the release
// record.
package release

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/objstore"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
	"github.com/gitsocial-org/gitsocial/library/core/result"
)

// ArtifactPushResult reports what an artifact push did: the uploaded files,
// the derived artifact base URL, and whether latest.txt / the release record
// were updated.
type ArtifactPushResult struct {
	Version        string
	Remote         string
	BaseURL        string
	Files          []ArtifactInfo
	LatestAdvanced bool
	RecordUpdated  bool
}

// PushArtifacts uploads a release's artifact files to the s3 remote's bucket
// at artifacts/<version>/<basename> (remote "" resolves like `gitsocial push`),
// advances artifacts/latest.txt when the version is a newer non-prerelease,
// and sets artifact-url on the version's release record when the record exists
// without one. Re-pushing a version silently overwrites its objects.
func PushArtifacts(workdir, version string, filePaths []string, remote string) Result[ArtifactPushResult] {
	if len(filePaths) == 0 {
		return result.Err[ArtifactPushResult]("INVALID_ARGS", "no artifact files given")
	}
	files := make(map[string][]byte, len(filePaths))
	infos := make([]ArtifactInfo, 0, len(filePaths))
	for _, fp := range filePaths {
		data, err := os.ReadFile(fp)
		if err != nil {
			return result.Err[ArtifactPushResult]("READ_FAILED", fmt.Sprintf("read %s: %s", fp, err))
		}
		name := filepath.Base(fp)
		if _, exists := files[name]; exists {
			return result.Err[ArtifactPushResult]("INVALID_ARGS", fmt.Sprintf("duplicate artifact filename %q", name))
		}
		files[name] = data
		infos = append(infos, ArtifactInfo{Filename: name, Size: int64(len(data)), SHA256: fmt.Sprintf("%x", sha256.Sum256(data))})
	}
	if remote == "" {
		remote = git.PushRemote(workdir)
	}
	remoteURL := git.RemoteURL(workdir, remote)
	if remoteURL == "" {
		return result.Err[ArtifactPushResult]("NOT_FOUND", fmt.Sprintf("remote %q is not configured", remote))
	}
	if !strings.HasPrefix(remoteURL, "s3://") {
		return result.Err[ArtifactPushResult]("NOT_S3", fmt.Sprintf("remote %q (%s) is not an s3 remote", remote, remoteURL))
	}
	siteURL := effectiveSiteURL(workdir, remote)
	if siteURL == "" {
		return result.Err[ArtifactPushResult]("NO_SITE_URL", fmt.Sprintf("no site url configured for remote %q: set it with `gitsocial config site set url https://...` (or a per-remote override)", remote))
	}
	baseURL := siteURL + objstore.ArtifactsPrefix + version
	advanced, err := objstore.PushArtifactObjects(remoteURL, objstore.HelperEnvFromOS(), version, files, func(current string) bool {
		return shouldAdvanceLatest(current, version)
	})
	if err != nil {
		return result.Err[ArtifactPushResult]("UPLOAD_FAILED", err.Error())
	}
	updated, err := setRecordArtifactURL(workdir, version, baseURL)
	if err != nil {
		return result.Err[ArtifactPushResult]("RECORD_EDIT_FAILED", fmt.Sprintf("artifacts uploaded, but setting artifact-url on the release record failed (re-run to retry): %s", err))
	}
	return result.Ok(ArtifactPushResult{
		Version:        version,
		Remote:         remote,
		BaseURL:        baseURL,
		Files:          infos,
		LatestAdvanced: advanced,
		RecordUpdated:  updated,
	})
}

// effectiveSiteURL resolves the remote's effective site base URL: the
// per-remote override (remote.<name>.gitsocial-site-url) when valid, else the
// workspace site config's url — the same precedence the site push applies.
func effectiveSiteURL(workdir, remote string) string {
	cfg, _ := objstore.ReadWorkspaceSiteCustomization(workdir)
	siteURL := cfg.URL
	if out, err := git.ExecGit(workdir, []string{"config", "--get", "remote." + remote + "." + objstore.SiteOverrideURLKey}); err == nil {
		if norm, ok := objstore.NormalizeSiteURL(strings.TrimSpace(out.Stdout)); ok {
			siteURL = norm
		}
	}
	return siteURL
}

// setRecordArtifactURL sets artifact-url on the workspace's release record for
// the version when the record exists without one (the catch-up path); a
// missing record, a foreign-repo record, or an already-set URL is a silent
// no-op. Returns whether the record was edited.
func setRecordArtifactURL(workdir, version, baseURL string) (bool, error) {
	item := findReleaseRecord(version)
	if item == nil || item.RepoURL != gitmsg.ResolveRepoURL(workdir) {
		return false, nil
	}
	if item.ArtifactURL.String != "" {
		return false, nil
	}
	ref := protocol.CreateRef(protocol.RefTypeCommit, item.Hash, item.RepoURL, item.Branch)
	res := EditRelease(workdir, ref, EditReleaseOptions{ArtifactURL: &baseURL})
	if !res.Success {
		return false, fmt.Errorf("edit release %s: %s", version, res.Error.Message)
	}
	return true, nil
}

// findReleaseRecord looks up the release record for a bare version, also
// trying the conventional v-prefixed tag; nil when no record is cached.
func findReleaseRecord(version string) *ReleaseItem {
	if item, err := GetReleaseItemByTagOrVersion(version); err == nil {
		return item
	}
	if item, err := GetReleaseItemByTagOrVersion("v" + version); err == nil {
		return item
	}
	return nil
}

// shouldAdvanceLatest reports whether pushing version should rewrite
// artifacts/latest.txt (current is its existing content, "" when absent):
// prereleases never advance; a missing or unparsable current always does;
// otherwise only a strictly newer version wins.
func shouldAdvanceLatest(current, version string) bool {
	parts, pre, ok := parseSemver(version)
	if !ok || pre != "" {
		return false
	}
	currentParts, _, currentOK := parseSemver(strings.TrimSpace(current))
	if !currentOK {
		return true
	}
	for i := range parts {
		if parts[i] != currentParts[i] {
			return parts[i] > currentParts[i]
		}
	}
	return false
}

// parseSemver splits a bare semver string into its numeric parts and
// prerelease suffix; ok is false when the shape is not
// `major.minor.patch[-suffix]`.
func parseSemver(v string) (parts [3]int, pre string, ok bool) {
	base, pre, _ := strings.Cut(v, "-")
	fields := strings.Split(base, ".")
	if len(fields) != 3 {
		return parts, "", false
	}
	for i, f := range fields {
		n, err := strconv.Atoi(f)
		if err != nil || n < 0 || f != strconv.Itoa(n) {
			return parts, "", false
		}
		parts[i] = n
	}
	return parts, pre, true
}

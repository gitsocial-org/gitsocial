// artifacts.go - Artifact storage on git refs
package release

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gitsocial-org/gitsocial/core/git"
	"github.com/gitsocial-org/gitsocial/core/log"
	"github.com/gitsocial-org/gitsocial/core/result"
)

const lfsThreshold = 1 << 20 // 1MB

// AddArtifacts commits files to refs/gitmsg/release/<version>/artifacts.
func AddArtifacts(workdir, version string, filePaths []string) Result[ArtifactResult] {
	files := make(map[string][]byte, len(filePaths)+1)
	infos := make([]ArtifactInfo, 0, len(filePaths))
	var checksumLines strings.Builder
	for _, fp := range filePaths {
		data, err := os.ReadFile(fp)
		if err != nil {
			return result.Err[ArtifactResult]("READ_FAILED", fmt.Sprintf("read %s: %s", fp, err))
		}
		name := filepath.Base(fp)
		hash := sha256.Sum256(data)
		hexHash := fmt.Sprintf("%x", hash)
		if len(data) > lfsThreshold {
			if err := git.StoreLFSObject(workdir, hexHash, data); err != nil {
				log.Warn("lfs store failed, using raw blob", "file", name, "error", err)
				files[name] = data
			} else {
				files[name] = git.FormatLFSPointer(hexHash, int64(len(data)))
				log.Info("storing artifact via LFS", "file", name, "size", len(data))
				if !git.IsLFSAvailable() {
					log.Warn("git-lfs not installed; LFS objects may not push correctly")
				}
			}
		} else {
			files[name] = data
		}
		infos = append(infos, ArtifactInfo{Filename: name, Size: int64(len(data)), SHA256: hexHash})
		fmt.Fprintf(&checksumLines, "%s  %s  %d\n", hexHash, name, len(data))
	}
	files["SHA256SUMS"] = []byte(checksumLines.String())
	ref := "refs/gitmsg/release/" + version + "/artifacts"
	msg := fmt.Sprintf("Add artifacts for %s", version)
	_, err := git.CommitFiles(workdir, ref, msg, files)
	if err != nil {
		return result.Err[ArtifactResult]("COMMIT_FAILED", err.Error())
	}
	return result.Ok(ArtifactResult{Version: version, Files: infos})
}

// ListArtifacts returns artifact info from the artifact ref.
func ListArtifacts(workdir, version string) Result[[]ArtifactInfo] {
	ref := "refs/gitmsg/release/" + version + "/artifacts"
	res, err := git.ExecGit(workdir, []string{"ls-tree", ref})
	if err != nil {
		return result.Err[[]ArtifactInfo]("NOT_FOUND", fmt.Sprintf("no artifacts for version %s", version))
	}
	checksums := readChecksums(workdir, ref)
	var infos []ArtifactInfo
	for _, line := range strings.Split(res.Stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			continue
		}
		name := parts[1]
		if name == "SHA256SUMS" {
			continue
		}
		info := ArtifactInfo{Filename: name}
		if entry, ok := checksums[name]; ok {
			info.SHA256 = entry.sha256
			info.Size = entry.size
		}
		infos = append(infos, info)
	}
	return result.Ok(infos)
}

type checksumEntry struct {
	sha256 string
	size   int64
}

// ExportArtifact reads an artifact from the release ref and writes it to destPath.
// It resolves LFS pointers locally first, then falls back to remote fetch via the Batch API.
func ExportArtifact(repoDir, repoURL, version, filename, destPath string) Result[string] {
	ref := "refs/gitmsg/release/" + version + "/artifacts"
	content, err := git.GetFileContent(repoDir, ref, filename)
	if err != nil {
		return result.Err[string]("NOT_FOUND", fmt.Sprintf("artifact %s not found in %s: %s", filename, version, err))
	}
	data := []byte(content)
	if oid, size, ok := git.ParseLFSPointer(data); ok {
		if lfsData, lfsErr := git.ReadLFSObject(repoDir, oid); lfsErr == nil {
			data = lfsData
		} else if repoURL != "" {
			lfsData, fetchErr := git.FetchLFSObject(repoURL, oid, size)
			if fetchErr != nil {
				return result.Err[string]("LFS_UNAVAILABLE", fmt.Sprintf("artifact %s is stored in LFS but could not be downloaded: %s", filename, fetchErr))
			}
			data = lfsData
			_ = git.StoreLFSObject(repoDir, oid, data)
		} else {
			return result.Err[string]("LFS_UNAVAILABLE", fmt.Sprintf("artifact %s is stored in LFS but not available locally", filename))
		}
	}
	destPath = uniquePath(destPath)
	if err := os.WriteFile(destPath, data, 0o644); err != nil {
		return result.Err[string]("WRITE_FAILED", fmt.Sprintf("write artifact: %s", err))
	}
	return result.Ok(destPath)
}

// uniquePath appends (1), (2), etc. if the path already exists.
func uniquePath(path string) string {
	if _, err := os.Stat(path); err != nil {
		return path
	}
	ext := filepath.Ext(path)
	base := strings.TrimSuffix(path, ext)
	for i := 1; ; i++ {
		candidate := fmt.Sprintf("%s (%d)%s", base, i, ext)
		if _, err := os.Stat(candidate); err != nil {
			return candidate
		}
	}
}

// FormatSize formats a byte count as a human-readable string.
func FormatSize(bytes int64) string {
	switch {
	case bytes >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(bytes)/float64(1<<30))
	case bytes >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(bytes)/float64(1<<20))
	case bytes >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(bytes)/float64(1<<10))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

// readChecksums reads and parses the SHA256SUMS blob from the artifact ref.
func readChecksums(workdir, ref string) map[string]checksumEntry {
	out, err := git.ExecGit(workdir, []string{"show", ref + ":SHA256SUMS"})
	if err != nil {
		return nil
	}
	entries := make(map[string]checksumEntry)
	for _, line := range strings.Split(out.Stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		hash := fields[0]
		name := fields[1]
		var size int64
		if len(fields) >= 3 {
			size, _ = strconv.ParseInt(fields[2], 10, 64)
		}
		entries[name] = checksumEntry{sha256: hash, size: size}
	}
	return entries
}

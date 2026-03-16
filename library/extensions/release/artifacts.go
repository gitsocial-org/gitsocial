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
	"github.com/gitsocial-org/gitsocial/core/result"
)

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
		files[name] = data
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

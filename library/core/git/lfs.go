// lfs.go - Git LFS pointer generation and object storage
package git

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const lfsPointerPrefix = "version https://git-lfs.github.com/spec/v1\n"

// IsLFSAvailable checks if git-lfs is installed.
func IsLFSAvailable() bool {
	_, err := ExecGit(".", []string{"lfs", "version"})
	return err == nil
}

// FormatLFSPointer generates LFS pointer content for a given oid and size.
func FormatLFSPointer(oid string, size int64) []byte {
	return []byte(fmt.Sprintf("%soid sha256:%s\nsize %d\n", lfsPointerPrefix, oid, size))
}

// StoreLFSObject writes content to the local LFS object store.
func StoreLFSObject(workdir, oid string, data []byte) error {
	gitDir, err := execGitSimple(workdir, []string{"rev-parse", "--git-dir"})
	if err != nil {
		return fmt.Errorf("resolve git dir: %w", err)
	}
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(workdir, gitDir)
	}
	dir := filepath.Join(gitDir, "lfs", "objects", oid[:2], oid[2:4])
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create lfs object dir: %w", err)
	}
	objPath := filepath.Join(dir, oid)
	if _, err := os.Stat(objPath); err == nil {
		return nil // already exists
	}
	if err := os.WriteFile(objPath, data, 0o444); err != nil {
		return fmt.Errorf("write lfs object: %w", err)
	}
	return nil
}

// ReadLFSObject reads content from the local LFS object store.
func ReadLFSObject(workdir, oid string) ([]byte, error) {
	gitDir, err := execGitSimple(workdir, []string{"rev-parse", "--git-dir"})
	if err != nil {
		return nil, fmt.Errorf("resolve git dir: %w", err)
	}
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(workdir, gitDir)
	}
	objPath := filepath.Join(gitDir, "lfs", "objects", oid[:2], oid[2:4], oid)
	data, err := os.ReadFile(objPath)
	if err != nil {
		return nil, fmt.Errorf("read lfs object: %w", err)
	}
	return data, nil
}

// IsLFSPointer checks whether content is a Git LFS pointer.
func IsLFSPointer(data []byte) bool {
	return bytes.HasPrefix(data, []byte(lfsPointerPrefix))
}

// GetUnpushedLFSCount returns the number of LFS objects not yet pushed to origin.
func GetUnpushedLFSCount(workdir string) int {
	if !IsLFSAvailable() {
		return 0
	}
	result, err := ExecGit(workdir, []string{"lfs", "push", "--dry-run", "origin", "--all"})
	if err != nil {
		return 0
	}
	count := 0
	for _, line := range strings.Split(result.Stdout, "\n") {
		if strings.HasPrefix(line, "push ") {
			count++
		}
	}
	return count
}

// PushLFS pushes all LFS objects to origin, including those on gitmsg refs.
func PushLFS(workdir string) (int, error) {
	if !IsLFSAvailable() {
		return 0, fmt.Errorf("git-lfs not installed")
	}
	count := GetUnpushedLFSCount(workdir)
	if count == 0 {
		return 0, nil
	}
	if _, err := ExecGit(workdir, []string{"lfs", "push", "origin", "--all"}); err != nil {
		return 0, fmt.Errorf("lfs push --all: %w", err)
	}
	refsResult, err := ExecGit(workdir, []string{"for-each-ref", "--format=%(refname)", "refs/gitmsg/"})
	if err == nil {
		for _, ref := range strings.Split(refsResult.Stdout, "\n") {
			ref = strings.TrimSpace(ref)
			if ref != "" {
				_, _ = ExecGit(workdir, []string{"lfs", "push", "origin", ref})
			}
		}
	}
	return count, nil
}

// ParseLFSPointer extracts the oid and size from LFS pointer content.
func ParseLFSPointer(data []byte) (oid string, size int64, ok bool) {
	if !IsLFSPointer(data) {
		return "", 0, false
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "oid sha256:") {
			oid = strings.TrimPrefix(line, "oid sha256:")
		} else if strings.HasPrefix(line, "size ") {
			size, _ = strconv.ParseInt(strings.TrimPrefix(line, "size "), 10, 64)
		}
	}
	return oid, size, oid != "" && size > 0
}

// FetchLFSObject downloads an LFS object from the remote using the Batch API.
func FetchLFSObject(repoURL, oid string, size int64) ([]byte, error) {
	lfsURL := buildLFSBatchURL(repoURL)
	reqBody, _ := json.Marshal(map[string]interface{}{
		"operation": "download",
		"transfers": []string{"basic"},
		"objects":   []map[string]interface{}{{"oid": oid, "size": size}},
	})
	req, err := http.NewRequest("POST", lfsURL, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("create lfs request: %w", err)
	}
	req.Header.Set("Content-Type", "application/vnd.git-lfs+json")
	req.Header.Set("Accept", "application/vnd.git-lfs+json")
	if user, pass, ok := getGitCredentials(repoURL); ok {
		req.SetBasicAuth(user, pass)
	}
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("lfs batch request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("lfs batch returned %d", resp.StatusCode)
	}
	var batchResp lfsBatchResponse
	if err := json.NewDecoder(resp.Body).Decode(&batchResp); err != nil {
		return nil, fmt.Errorf("decode lfs batch response: %w", err)
	}
	if len(batchResp.Objects) == 0 {
		return nil, fmt.Errorf("lfs batch returned no objects")
	}
	obj := batchResp.Objects[0]
	if obj.Error != nil {
		return nil, fmt.Errorf("lfs object error: %s", obj.Error.Message)
	}
	dl, ok := obj.Actions["download"]
	if !ok {
		return nil, fmt.Errorf("lfs object has no download action")
	}
	return downloadLFSObject(dl, client)
}

type lfsBatchResponse struct {
	Objects []lfsObject `json:"objects"`
}

type lfsObject struct {
	OID     string               `json:"oid"`
	Size    int64                `json:"size"`
	Actions map[string]lfsAction `json:"actions"`
	Error   *lfsObjectError      `json:"error"`
}

type lfsAction struct {
	Href   string            `json:"href"`
	Header map[string]string `json:"header"`
}

type lfsObjectError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// downloadLFSObject fetches the actual object data from the download URL.
func downloadLFSObject(action lfsAction, client *http.Client) ([]byte, error) {
	req, err := http.NewRequest("GET", action.Href, nil)
	if err != nil {
		return nil, fmt.Errorf("create download request: %w", err)
	}
	for k, v := range action.Header {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download lfs object: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("lfs download returned %d", resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read lfs object: %w", err)
	}
	return data, nil
}

// buildLFSBatchURL constructs the LFS Batch API URL from a repo URL.
func buildLFSBatchURL(repoURL string) string {
	base := strings.TrimSuffix(repoURL, "/")
	if !strings.HasSuffix(base, ".git") {
		base += ".git"
	}
	return base + "/info/lfs/objects/batch"
}

// getGitCredentials retrieves credentials for a URL via git credential fill.
func getGitCredentials(repoURL string) (user, pass string, ok bool) {
	parsed, err := url.Parse(repoURL)
	if err != nil {
		return "", "", false
	}
	input := fmt.Sprintf("protocol=%s\nhost=%s\n", parsed.Scheme, parsed.Host)
	if parsed.Path != "" {
		input += fmt.Sprintf("path=%s\n", strings.TrimPrefix(parsed.Path, "/"))
	}
	output, err := execGitWithStdin(".", []string{"credential", "fill"}, input)
	if err != nil {
		return "", "", false
	}
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, "username=") {
			user = strings.TrimPrefix(line, "username=")
		} else if strings.HasPrefix(line, "password=") {
			pass = strings.TrimPrefix(line, "password=")
		}
	}
	return user, pass, user != "" && pass != ""
}

// DownloadsDir returns the user's downloads directory.
func DownloadsDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	dir := filepath.Join(home, "Downloads")
	if info, err := os.Stat(dir); err == nil && info.IsDir() {
		return dir
	}
	return home
}

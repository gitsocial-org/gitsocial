// loose_count_test.go - looseUploaded counts the objects one push uploaded, and starts each batch at zero.
package objstore

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// writeBlobs writes n distinct blobs into the repo's odb through one git call and returns their shas.
func writeBlobs(t *testing.T, dir string, n int) []string {
	t.Helper()
	blobDir := filepath.Join(dir, "blobs")
	if err := os.MkdirAll(blobDir, 0755); err != nil {
		t.Fatal(err)
	}
	var paths bytes.Buffer
	for i := 0; i < n; i++ {
		path := filepath.Join(blobDir, fmt.Sprintf("b-%05d", i))
		if err := os.WriteFile(path, []byte(fmt.Sprintf("blob %d\n", i)), 0644); err != nil {
			t.Fatal(err)
		}
		fmt.Fprintln(&paths, path)
	}
	cmd := exec.Command("git", "hash-object", "-w", "--stdin-paths")
	cmd.Dir = dir
	cmd.Stdin = &paths
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git hash-object: %v", err)
	}
	shas := strings.Fields(string(out))
	if len(shas) != n {
		t.Fatalf("wrote %d blobs, want %d", len(shas), n)
	}
	return shas
}

// TestLooseUploaded_zeroWhenEveryObjectIsPresent: a delta the bucket already holds is credited to nothing, so the seal trigger does not fire early.
func TestLooseUploaded_zeroWhenEveryObjectIsPresent(t *testing.T) {
	dir := packTestRepo(t, 1)
	client, _ := testClient(t)
	h := pushHelper(t, client, dir)
	shas := writeBlobs(t, dir, listResumeThreshold)
	putObjectKeys(t, client, shas)

	if err := h.uploadObjects(shas); err != nil {
		t.Fatalf("uploadObjects: %v", err)
	}
	if h.looseUploaded != 0 {
		t.Errorf("looseUploaded = %d after a push whose objects are all present, want 0", h.looseUploaded)
	}
}

// TestLooseUploaded_countsTheFilteredDelta: only the objects past the presence filter are credited.
func TestLooseUploaded_countsTheFilteredDelta(t *testing.T) {
	dir := packTestRepo(t, 1)
	client, _ := testClient(t)
	h := pushHelper(t, client, dir)
	shas := writeBlobs(t, dir, listResumeThreshold)
	half := len(shas) / 2
	putObjectKeys(t, client, shas[:half])

	if err := h.uploadObjects(shas); err != nil {
		t.Fatalf("uploadObjects: %v", err)
	}
	if want := len(shas) - half; h.looseUploaded != want {
		t.Errorf("looseUploaded = %d, want %d (the delta the presence filter kept)", h.looseUploaded, want)
	}
}

// TestLooseUploaded_batchesDoNotAccumulate: a second push batch in one helper session counts its own uploads alone.
func TestLooseUploaded_batchesDoNotAccumulate(t *testing.T) {
	dir := packTestRepo(t, 2)
	gitRun(t, dir, "branch", "second", "HEAD~1")
	client, _ := testClient(t)
	h := pushHelper(t, client, dir)
	h.refMode = refModeETag

	var out bytes.Buffer
	if err := h.push([]string{"push refs/heads/main:refs/heads/main"}, &out); err != nil {
		t.Fatalf("first push: %v", err)
	}
	first := h.looseUploaded
	if first == 0 {
		t.Fatal("the first push uploaded nothing; the fixture is not exercising the counter")
	}
	if err := h.push([]string{"push refs/heads/second:refs/heads/second"}, &out); err != nil {
		t.Fatalf("second push: %v", err)
	}
	// The second batch uploads nothing new, so a counter that accumulated would still hold the first batch's count.
	if h.looseUploaded != 0 {
		t.Errorf("looseUploaded = %d after a second batch that uploaded nothing, want 0 (the first batch counted %d)", h.looseUploaded, first)
	}
}

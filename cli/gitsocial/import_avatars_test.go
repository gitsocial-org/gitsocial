// import_avatars_test.go - tests for gitsocial import avatars with no s3 remote
package main

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gitsocial-org/gitsocial/library/core/protocol"
	"github.com/gitsocial-org/gitsocial/library/core/site"
)

// avatarTestPNG writes a 1x1 PNG into a temp directory and returns its path and bytes.
func avatarTestPNG(t *testing.T) (string, []byte) {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "picture.png")
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return path, buf.Bytes()
}

// TestImportAvatars_noRemote: with no s3 remote the import fills the store of the origin and touches no bucket.
func TestImportAvatars_noRemote(t *testing.T) {
	t.Parallel()
	dir, cacheDir := initCLITestRepo(t), t.TempDir()
	path, body := avatarTestPNG(t)
	stdout, stderr, code := runCLI(t, dir, cacheDir, "import", "avatars", "--set-email", "ada@example.com", "--set-file", path)
	if code != 0 {
		t.Fatalf("import avatars: exit %d\n%s", code, stderr)
	}
	if !strings.Contains(stdout, "Stored 1 picture") || !strings.Contains(stderr, "next push publishes") {
		t.Errorf("output = %q, stderr = %q, want the stored count and the push hint", stdout, stderr)
	}
	storeDir := site.AvatarStoreDir(cacheDir, protocol.NormalizeURL("https://github.com/test/cli-repo.git"))
	if got, err := os.ReadFile(filepath.Join(storeDir, site.AvatarKey("ada@example.com"))); err != nil || !bytes.Equal(got, body) {
		t.Errorf("store picture = %d bytes, %v", len(got), err)
	}
}

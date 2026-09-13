// error_codes_test.go - Error codes the release artifact, push and SBOM paths return at the Result boundary
package release

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/objstore"
	"github.com/gitsocial-org/gitsocial/library/internal/testutil"
)

// lockExtBranch leaves a ref lock on an extension's data branch, the way a crashed writer would.
func lockExtBranch(t *testing.T, dir, ext string) {
	t.Helper()
	lock := filepath.Join(dir, ".git", "refs", "heads", filepath.FromSlash(gitmsg.GetExtBranch(dir, ext))+".lock")
	if err := os.MkdirAll(filepath.Dir(lock), 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(lock), err)
	}
	if err := os.WriteFile(lock, nil, 0o600); err != nil {
		t.Fatalf("write %s: %v", lock, err)
	}
}

// TestArtifacts_readFailed asserts READ_FAILED on an artifact file that is not there.
func TestArtifacts_readFailed(t *testing.T) {
	setupTestDB(t)
	workdir := initTestRepo(t)
	missing := filepath.Join(t.TempDir(), "never-built.tar.gz")

	if res := AddArtifacts(workdir, "1.0.0", []string{missing}); res.Success || res.Error.Code != "READ_FAILED" {
		t.Errorf("AddArtifacts() over a missing file = %+v, want READ_FAILED", res)
	}
	if res := PushArtifacts(workdir, "1.0.0", []string{missing}, ""); res.Success || res.Error.Code != "READ_FAILED" {
		t.Errorf("PushArtifacts() over a missing file = %+v, want READ_FAILED", res)
	}
	if res := GetSBOMRaw(workdir, "1.0.0", "sbom.json"); res.Success || res.Error.Code != "READ_FAILED" {
		t.Errorf("GetSBOMRaw() without an artifact ref = %+v, want READ_FAILED", res)
	}
}

// TestExportArtifact_lfsUnavailable asserts LFS_UNAVAILABLE for a pointer whose object is not in the repository.
func TestExportArtifact_lfsUnavailable(t *testing.T) {
	setupTestDB(t)
	workdir := initTestRepo(t)

	// A pointer whose object was never stored, the state a clone without LFS leaves.
	oid := fmt.Sprintf("%x", sha256.Sum256([]byte("app bytes")))
	pointer := git.FormatLFSPointer(oid, 9)
	path := filepath.Join(t.TempDir(), "app.tar.gz")
	if err := os.WriteFile(path, pointer, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	if res := AddArtifacts(workdir, "1.0.0", []string{path}); !res.Success {
		t.Fatalf("AddArtifacts: %s", res.Error.Message)
	}

	dest := filepath.Join(t.TempDir(), "app.tar.gz")
	res := ExportArtifact(workdir, "", "1.0.0", "app.tar.gz", dest)
	if res.Success || res.Error.Code != "LFS_UNAVAILABLE" {
		t.Errorf("ExportArtifact() of an unresolvable pointer = %+v, want LFS_UNAVAILABLE", res)
	}
}

// TestExportArtifact_writeFailed asserts WRITE_FAILED when the destination directory is not there.
func TestExportArtifact_writeFailed(t *testing.T) {
	setupTestDB(t)
	workdir := initTestRepo(t)
	path := writeArtifactFile(t, t.TempDir(), "app.tar.gz", "binary v1")
	if res := AddArtifacts(workdir, "1.0.0", []string{path}); !res.Success {
		t.Fatalf("AddArtifacts: %s", res.Error.Message)
	}

	dest := filepath.Join(t.TempDir(), "no-such-dir", "app.tar.gz")
	res := ExportArtifact(workdir, "", "1.0.0", "app.tar.gz", dest)
	if res.Success || res.Error.Code != "WRITE_FAILED" {
		t.Errorf("ExportArtifact() into a missing directory = %+v, want WRITE_FAILED", res)
	}
}

// TestCreateRelease_duplicate asserts DUPLICATE on a second release with the same tag.
func TestCreateRelease_duplicate(t *testing.T) {
	setupTestDB(t)
	workdir := initTestRepo(t)

	if res := CreateRelease(workdir, "v1.0.0", "", CreateReleaseOptions{Tag: "v1.0.0", Version: "1.0.0"}); !res.Success {
		t.Fatalf("CreateRelease: %s", res.Error.Message)
	}
	res := CreateRelease(workdir, "v1.0.0 again", "", CreateReleaseOptions{Tag: "v1.0.0", Version: "1.0.1"})
	if res.Success || res.Error.Code != "DUPLICATE" {
		t.Errorf("CreateRelease() with a taken tag = %+v, want DUPLICATE", res)
	}

	allowed := CreateRelease(workdir, "v1.0.0 again", "", CreateReleaseOptions{Tag: "v1.0.0", Version: "1.0.1", AllowDuplicate: true})
	if !allowed.Success {
		t.Errorf("CreateRelease() with AllowDuplicate = %+v, want success", allowed)
	}
}

// TestGetSBOMDetails_errorCodes asserts NO_SBOM, NO_VERSION and SBOM_FAILED on the SBOM lookup path.
func TestGetSBOMDetails_errorCodes(t *testing.T) {
	setupTestDB(t)
	workdir := initTestRepo(t)

	plain := CreateRelease(workdir, "v1.0.0", "", CreateReleaseOptions{Tag: "v1.0.0", Version: "1.0.0"})
	if !plain.Success {
		t.Fatalf("CreateRelease: %s", plain.Error.Message)
	}
	if res := GetSBOMDetails(workdir, plain.Data.ID); res.Success || res.Error.Code != "NO_SBOM" {
		t.Errorf("GetSBOMDetails() on a release with no SBOM = %+v, want NO_SBOM", res)
	}

	unversioned := CreateRelease(workdir, "Nightly", "", CreateReleaseOptions{Tag: "nightly", SBOM: "sbom.json"})
	if !unversioned.Success {
		t.Fatalf("CreateRelease: %s", unversioned.Error.Message)
	}
	if res := GetSBOMDetails(workdir, unversioned.Data.ID); res.Success || res.Error.Code != "NO_VERSION" {
		t.Errorf("GetSBOMDetails() on a release with no version = %+v, want NO_VERSION", res)
	}

	declared := CreateRelease(workdir, "v2.0.0", "", CreateReleaseOptions{Tag: "v2.0.0", Version: "2.0.0", SBOM: "sbom.json"})
	if !declared.Success {
		t.Fatalf("CreateRelease: %s", declared.Error.Message)
	}
	if res := GetSBOMDetails(workdir, declared.Data.ID); res.Success || res.Error.Code != "SBOM_FAILED" {
		t.Errorf("GetSBOMDetails() without the declared SBOM file = %+v, want SBOM_FAILED", res)
	}
}

// TestPushArtifacts_uploadFailed asserts UPLOAD_FAILED when the bucket refuses the write.
func TestPushArtifacts_uploadFailed(t *testing.T) {
	setupTestDB(t)
	workdir := initTestRepo(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("GITSOCIAL_S3_ACCESS_KEY", "k")
	t.Setenv("GITSOCIAL_S3_SECRET_KEY", "s")

	remoteURL := "s3://" + strings.TrimPrefix(srv.URL, "http://") + "/bucket/repo"
	git.ExecGit(workdir, []string{"remote", "add", "site", remoteURL})
	git.ExecGit(workdir, []string{"config", "remote.site." + objstore.SiteOverrideURLKey, "http://localhost:8080/"})

	path := writeArtifactFile(t, t.TempDir(), "app.tar.gz", "binary v1")
	res := PushArtifacts(workdir, "1.0.0", []string{path}, "site")
	if res.Success || res.Error.Code != "UPLOAD_FAILED" {
		t.Errorf("PushArtifacts() to a bucket that refuses writes = %+v, want UPLOAD_FAILED", res)
	}
}

// TestPushArtifacts_recordEditFailed asserts RECORD_EDIT_FAILED when the artifact-url catch-up cannot commit.
func TestPushArtifacts_recordEditFailed(t *testing.T) {
	setupTestDB(t)
	workdir := initTestRepo(t)
	testutil.SkipWithoutFilesRefBackend(t, workdir)
	stub := &artifactStub{objs: map[string][]byte{}}
	srv := httptest.NewServer(stub)
	t.Cleanup(srv.Close)
	t.Setenv("GITSOCIAL_S3_ACCESS_KEY", "k")
	t.Setenv("GITSOCIAL_S3_SECRET_KEY", "s")

	remoteURL := "s3://" + strings.TrimPrefix(srv.URL, "http://") + "/bucket/repo"
	git.ExecGit(workdir, []string{"remote", "add", "site", remoteURL})
	git.ExecGit(workdir, []string{"config", "remote.site." + objstore.SiteOverrideURLKey, "http://localhost:8080/"})

	if res := CreateRelease(workdir, "v1.0.0", "", CreateReleaseOptions{Tag: "v1.0.0", Version: "1.0.0"}); !res.Success {
		t.Fatalf("CreateRelease: %s", res.Error.Message)
	}
	lockExtBranch(t, workdir, "release")

	path := writeArtifactFile(t, t.TempDir(), "app.tar.gz", "binary v1")
	res := PushArtifacts(workdir, "1.0.0", []string{path}, "site")
	if res.Success || res.Error.Code != "RECORD_EDIT_FAILED" {
		t.Errorf("PushArtifacts() with the release branch locked = %+v, want RECORD_EDIT_FAILED", res)
	}
	if body, ok := stub.object("bucket/repo/artifacts/1.0.0/app.tar.gz"); !ok || body != "binary v1" {
		t.Errorf("artifact object = %q ok=%v, want the upload to stand", body, ok)
	}
}
